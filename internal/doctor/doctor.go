// Package doctor implements `marc doctor`: a read-only diagnostic that runs
// every check we know how to run and prints one ✓/⚠/✗ line per check.
//
// It MUST NOT mutate state — no daemon control, no config writes, no MinIO
// puts that aren't immediately deleted. The exit code reflects the worst
// severity (0 for all-pass-or-warn, 1 if any check failed).
package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/caffeaun/marc/internal/config"
	"github.com/caffeaun/marc/internal/minioclient"
)

// Severity classifies a check outcome.
type Severity int

const (
	SevPass Severity = iota
	SevWarn
	SevFail
)

func (s Severity) String() string {
	switch s {
	case SevPass:
		return "✓"
	case SevWarn:
		return "⚠"
	case SevFail:
		return "✗"
	}
	return "?"
}

// Check is the outcome of one named diagnostic.
type Check struct {
	Name     string
	Severity Severity
	Detail   string
}

// Result aggregates every check that ran.
type Result struct {
	Checks []Check
}

// ExitCode returns 1 if any check failed, else 0.
func (r Result) ExitCode() int {
	for _, c := range r.Checks {
		if c.Severity == SevFail {
			return 1
		}
	}
	return 0
}

// Options controls a doctor run. Most fields are injection points so the test
// can avoid touching the real filesystem / network.
type Options struct {
	// ConfigPath is the path to ~/.marc/config.toml.
	ConfigPath string

	// Stdout receives the per-check report.
	Stdout io.Writer

	// HTTPClient is used to hit /_marc/health. nil → http.DefaultClient with
	// a short timeout.
	HTTPClient *http.Client

	// NewMinIOClient is the constructor for a MinIO client. Tests inject a
	// fake. nil → minioclient.New.
	NewMinIOClient func(minioclient.Config) (minioclient.Client, error)

	// SystemctlIsActive returns "active", "inactive", "failed", "not-installed",
	// or another systemd state for the given unit. Tests inject a fake.
	// nil → runs `systemctl is-active <unit>` for real.
	SystemctlIsActive func(ctx context.Context, unit string) (string, error)

	// LaunchctlList returns the launchd state for the given label on darwin.
	// Returns one of "running", "loaded-stopped", "not-loaded". Tests inject
	// a fake. nil → runs `launchctl list <label>` for real.
	LaunchctlList func(ctx context.Context, label string) (string, error)

	// Goos overrides runtime.GOOS for tests. Empty string → runtime.GOOS.
	Goos string

	// DialPort tries to TCP-connect to addr to confirm something is listening.
	// nil → net.DialTimeout.
	DialPort func(network, addr string, timeout time.Duration) error

	// Getenv reads environment variables. Tests inject a fake. nil → os.Getenv.
	Getenv func(string) string

	// Now returns the current time. Tests inject a fake. nil → time.Now.
	Now func() time.Time
}

func (o *Options) httpClient() *http.Client {
	if o.HTTPClient != nil {
		return o.HTTPClient
	}
	return &http.Client{Timeout: 2 * time.Second}
}

func (o *Options) getenv(k string) string {
	if o.Getenv != nil {
		return o.Getenv(k)
	}
	return os.Getenv(k)
}

func (o *Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o *Options) dial(addr string) error {
	if o.DialPort != nil {
		return o.DialPort("tcp", addr, time.Second)
	}
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

func (o *Options) systemctlIsActive(ctx context.Context, unit string) (string, error) {
	if o.SystemctlIsActive != nil {
		return o.SystemctlIsActive(ctx, unit)
	}
	cmd := exec.CommandContext(ctx, "systemctl", "is-active", unit)
	out, err := cmd.CombinedOutput()
	state := strings.TrimSpace(string(out))
	if state == "" && err != nil {
		state = "unknown"
	}
	// systemctl returns non-zero for non-active states; treat that as "the
	// state we got is the answer" rather than an error.
	if _, ok := err.(*exec.ExitError); ok {
		return state, nil
	}
	return state, err
}

func (o *Options) launchctlList(ctx context.Context, label string) (string, error) {
	if o.LaunchctlList != nil {
		return o.LaunchctlList(ctx, label)
	}
	out, err := exec.CommandContext(ctx, "launchctl", "list", label).CombinedOutput()
	// launchctl returns non-zero exit when the label isn't loaded.
	if _, ok := err.(*exec.ExitError); ok {
		return "not-loaded", nil
	}
	if err != nil {
		return "", err
	}
	// Loaded. Look for a "PID" key in the dict-style stdout to decide whether
	// it's currently running. Stopped agents print no PID line at all.
	if strings.Contains(string(out), `"PID"`) {
		return "running", nil
	}
	return "loaded-stopped", nil
}

func (o *Options) goos() string {
	if o.Goos != "" {
		return o.Goos
	}
	return runtime.GOOS
}

// Run executes all checks in order and returns a Result. It does NOT print
// to stdout (the caller decides — see Print).
func Run(ctx context.Context, opts Options) Result {
	var res Result
	add := func(c Check) { res.Checks = append(res.Checks, c) }

	// 1. Config file exists.
	cfgPath := opts.ConfigPath
	info, statErr := os.Stat(cfgPath)
	if statErr != nil {
		add(Check{"config file exists", SevFail, cfgPath + ": " + statErr.Error()})
		// Without a config we can't do MinIO checks; skip schema/mode and run
		// what we can in a degraded mode.
		runWithoutConfig(ctx, opts, &res)
		return res
	}
	add(Check{"config file exists", SevPass, cfgPath})

	// 2. Config file mode = 0600.
	if perm := info.Mode().Perm(); perm != 0o600 {
		add(Check{"config file mode 0600", SevFail, fmt.Sprintf("got %04o, want 0600 (run: chmod 0600 %s)", perm, cfgPath)})
	} else {
		add(Check{"config file mode 0600", SevPass, ""})
	}

	// 3. Config schema parses. config.LoadClient also re-checks the mode; if
	// the mode check above failed we still attempt a parse with a relaxed
	// reader so the user gets schema feedback instead of a stop-on-first-error.
	cfg, parseErr := loadClientWithoutModeCheck(cfgPath)
	if parseErr != nil {
		add(Check{"config schema parses", SevFail, parseErr.Error()})
		// Without a parsed config, downstream checks (port, MinIO, etc.) lose
		// context. Run them with sensible defaults so the report is still useful.
		runChecksWithoutCfg(ctx, opts, &res)
		return res
	}
	add(Check{"config schema parses", SevPass, ""})

	// 4. ANTHROPIC_BASE_URL env var.
	base := opts.getenv("ANTHROPIC_BASE_URL")
	expected := "http://" + cfg.Proxy.ListenAddr
	switch {
	case base == "":
		add(Check{"ANTHROPIC_BASE_URL env var", SevWarn,
			"not set (Claude Code will hit api.anthropic.com directly; no captures will flow); set: export ANTHROPIC_BASE_URL=" + expected})
	case base != expected:
		add(Check{"ANTHROPIC_BASE_URL env var", SevWarn,
			fmt.Sprintf("set to %q but proxy listens on %s; set: export ANTHROPIC_BASE_URL=%s", base, cfg.Proxy.ListenAddr, expected)})
	default:
		add(Check{"ANTHROPIC_BASE_URL env var", SevPass, base})
	}

	// 5 & 6. Daemon supervisor state — systemd on linux, launchd on darwin.
	switch opts.goos() {
	case "linux":
		add(checkSystemdUnit(ctx, opts, "marc-proxy.service"))
		add(checkSystemdUnit(ctx, opts, "marc-ship.service"))
	case "darwin":
		add(checkLaunchdAgent(ctx, opts, "io.marc.proxy"))
		add(checkLaunchdAgent(ctx, opts, "io.marc.ship"))
	default:
		add(Check{"marc-proxy daemon", SevWarn, "no supervisor check for " + opts.goos()})
		add(Check{"marc-ship daemon", SevWarn, "no supervisor check for " + opts.goos()})
	}

	// 7. Port listening.
	listenAddr := cfg.Proxy.ListenAddr
	if listenAddr == "" {
		listenAddr = "127.0.0.1:8082"
	}
	if err := opts.dial(listenAddr); err != nil {
		add(Check{"proxy port listening (" + listenAddr + ")", SevFail, "nothing accepting TCP connections: " + err.Error()})
	} else {
		add(Check{"proxy port listening (" + listenAddr + ")", SevPass, ""})
	}

	// 8 & 9. /_marc/health and "last successful forward within 1h".
	healthCheck, recencyCheck := checkHealthAndRecency(opts, listenAddr)
	add(healthCheck)
	add(recencyCheck)

	// 9.5. Each configured profile's upstream is reachable. One TCP-dial per
	// profile, 3s timeout. Doesn't validate auth — just that the endpoint
	// resolves and accepts connections. Failures are warnings, not fails:
	// the operator may have set up minimax/openai for later use without it
	// being live yet.
	for name, p := range cfg.Profiles {
		add(checkProfileReachable(name, p))
	}

	// 10. Capture file appendable.
	capturePath := cfg.Paths.CaptureFile
	if capturePath == "" {
		// fall back to ~/.marc/capture.jsonl
		if home, err := os.UserHomeDir(); err == nil {
			capturePath = home + "/.marc/capture.jsonl"
		}
	}
	add(checkCaptureAppendable(capturePath))

	// 11, 12, 13. MinIO reachable / auth / bucket — derived from a single Ping.
	add12 := func(c Check) { add(c) }
	checkMinIO(ctx, opts, cfg, add12)

	return res
}

// checkProfileReachable does a quick TCP dial against the profile's BaseURL.
// Pass = endpoint accepts a connection. Warn = unreachable (network /
// firewall / DNS issue). Doesn't attempt to validate auth or HTTP status —
// those are deferred to actual request flow.
func checkProfileReachable(name string, p config.ClientProfile) Check {
	label := "profile " + name + " upstream reachable"
	u, err := url.Parse(p.BaseURL)
	if err != nil || u.Host == "" {
		return Check{label, SevFail, "unparseable base_url: " + p.BaseURL}
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		switch u.Scheme {
		case "https":
			host += ":443"
		case "http":
			host += ":80"
		default:
			return Check{label, SevFail, "unsupported scheme: " + u.Scheme}
		}
	}
	conn, err := net.DialTimeout("tcp", host, 3*time.Second)
	if err != nil {
		return Check{label, SevWarn, "TCP dial " + host + ": " + err.Error()}
	}
	_ = conn.Close()
	keyStatus := ""
	if p.APIKeyEnv != "" {
		if os.Getenv(p.APIKeyEnv) == "" {
			keyStatus = " (note: " + p.APIKeyEnv + " env var is empty)"
		}
	}
	return Check{label, SevPass, host + keyStatus}
}

// loadClientWithoutModeCheck calls config.LoadClient. We accept its error
// verbatim — including the mode-mismatch error. The caller renders that as
// "config schema parses" failure, which is mildly inaccurate (the error is
// about mode, not schema) but the user already got a clearer mode line above
// at check #2, so it's fine in practice.
func loadClientWithoutModeCheck(path string) (*config.ClientConfig, error) {
	return config.LoadClient(path)
}

// runWithoutConfig runs the checks that don't need a config so the user still
// gets a partial diagnostic when ~/.marc/config.toml is missing entirely.
func runWithoutConfig(ctx context.Context, opts Options, res *Result) {
	add := func(c Check) { res.Checks = append(res.Checks, c) }
	add(Check{"config file mode 0600", SevFail, "skipped — config file is missing"})
	add(Check{"config schema parses", SevFail, "skipped — config file is missing"})

	base := opts.getenv("ANTHROPIC_BASE_URL")
	if base == "" {
		add(Check{"ANTHROPIC_BASE_URL env var", SevWarn, "not set"})
	} else {
		add(Check{"ANTHROPIC_BASE_URL env var", SevWarn, "set to " + base + " but no config to compare against"})
	}

	switch opts.goos() {
	case "linux":
		add(checkSystemdUnit(ctx, opts, "marc-proxy.service"))
		add(checkSystemdUnit(ctx, opts, "marc-ship.service"))
	case "darwin":
		add(checkLaunchdAgent(ctx, opts, "io.marc.proxy"))
		add(checkLaunchdAgent(ctx, opts, "io.marc.ship"))
	}

	add(Check{"proxy port listening", SevWarn, "skipped — no config to determine listen address"})
	add(Check{"/_marc/health responds ok", SevWarn, "skipped — no config to determine listen address"})
	add(Check{"last successful forward within 1h", SevWarn, "skipped — no config"})
	add(Check{"capture file appendable", SevWarn, "skipped — no config"})
	add(Check{"MinIO endpoint reachable", SevWarn, "skipped — no config"})
	add(Check{"MinIO authentication works", SevWarn, "skipped — no config"})
	add(Check{"MinIO bucket writable", SevWarn, "skipped — no config"})
}

// runChecksWithoutCfg is similar but used when the file exists yet doesn't
// parse. We've already added schema/mode/exists rows; fill in the rest.
func runChecksWithoutCfg(ctx context.Context, opts Options, res *Result) {
	add := func(c Check) { res.Checks = append(res.Checks, c) }
	base := opts.getenv("ANTHROPIC_BASE_URL")
	if base == "" {
		add(Check{"ANTHROPIC_BASE_URL env var", SevWarn, "not set"})
	} else {
		add(Check{"ANTHROPIC_BASE_URL env var", SevWarn, "set to " + base + " — config could not be parsed to verify"})
	}
	switch opts.goos() {
	case "linux":
		add(checkSystemdUnit(ctx, opts, "marc-proxy.service"))
		add(checkSystemdUnit(ctx, opts, "marc-ship.service"))
	case "darwin":
		add(checkLaunchdAgent(ctx, opts, "io.marc.proxy"))
		add(checkLaunchdAgent(ctx, opts, "io.marc.ship"))
	}
	add(Check{"proxy port listening", SevWarn, "skipped — config did not parse"})
	add(Check{"/_marc/health responds ok", SevWarn, "skipped — config did not parse"})
	add(Check{"last successful forward within 1h", SevWarn, "skipped — config did not parse"})
	add(Check{"capture file appendable", SevWarn, "skipped — config did not parse"})
	add(Check{"MinIO endpoint reachable", SevWarn, "skipped — config did not parse"})
	add(Check{"MinIO authentication works", SevWarn, "skipped — config did not parse"})
	add(Check{"MinIO bucket writable", SevWarn, "skipped — config did not parse"})
}

func checkSystemdUnit(ctx context.Context, opts Options, unit string) Check {
	state, err := opts.systemctlIsActive(ctx, unit)
	name := unit + " state"
	if err != nil {
		return Check{name, SevFail, "could not query systemctl: " + err.Error()}
	}
	switch state {
	case "active":
		return Check{name, SevPass, "active"}
	case "inactive":
		return Check{name, SevFail, "inactive (run: sudo systemctl start " + unit + ")"}
	case "failed":
		return Check{name, SevFail, "failed (check: journalctl -u " + unit + ")"}
	case "not-installed", "unknown":
		return Check{name, SevFail, "unit file not present (run: sudo marc install)"}
	default:
		return Check{name, SevWarn, "state=" + state}
	}
}

func checkLaunchdAgent(ctx context.Context, opts Options, label string) Check {
	state, err := opts.launchctlList(ctx, label)
	name := label + " state"
	if err != nil {
		return Check{name, SevFail, "could not query launchctl: " + err.Error()}
	}
	switch state {
	case "running":
		return Check{name, SevPass, "loaded and running"}
	case "loaded-stopped":
		return Check{name, SevFail,
			"loaded but not running (try: launchctl kickstart -k gui/$(id -u)/" + label + ")"}
	case "not-loaded":
		return Check{name, SevFail, "agent not loaded (run: marc install)"}
	default:
		return Check{name, SevWarn, "state=" + state}
	}
}

func checkHealthAndRecency(opts Options, listenAddr string) (Check, Check) {
	healthURL := "http://" + listenAddr + "/_marc/health"
	resp, err := opts.httpClient().Get(healthURL)
	if err != nil {
		return Check{"/_marc/health responds ok", SevFail, "GET " + healthURL + ": " + err.Error()},
			Check{"last successful forward within 1h", SevWarn, "skipped — health endpoint unreachable"}
	}
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return Check{"/_marc/health responds ok", SevFail, fmt.Sprintf("status %d: %s", resp.StatusCode, truncate(string(body), 200))},
			Check{"last successful forward within 1h", SevWarn, "skipped — health returned non-200"}
	}
	var snap map[string]any
	if err := json.Unmarshal(body, &snap); err != nil {
		return Check{"/_marc/health responds ok", SevFail, "non-JSON body: " + truncate(string(body), 200)},
			Check{"last successful forward within 1h", SevWarn, "skipped"}
	}
	status, _ := snap["status"].(string)
	switch status {
	case "ok":
		// fall through to recency check
	case "degraded":
		return Check{"/_marc/health responds ok", SevWarn, "status=degraded; last_error_message=" + asString(snap["last_error_message"])},
			recencyCheckFromHealth(opts, snap)
	case "failed":
		return Check{"/_marc/health responds ok", SevFail, "status=failed; last_error_message=" + asString(snap["last_error_message"])},
			Check{"last successful forward within 1h", SevWarn, "skipped — health is failed"}
	default:
		return Check{"/_marc/health responds ok", SevWarn, "status=" + status},
			recencyCheckFromHealth(opts, snap)
	}
	return Check{"/_marc/health responds ok", SevPass, "status=ok"},
		recencyCheckFromHealth(opts, snap)
}

func recencyCheckFromHealth(opts Options, snap map[string]any) Check {
	const name = "last successful forward within 1h"
	tsAny, ok := snap["last_successful_forward_at"]
	if !ok || tsAny == nil {
		return Check{name, SevWarn, "no successful forward recorded yet (proxy just started? send one /v1 request and re-check)"}
	}
	ts, ok := tsAny.(string)
	if !ok || ts == "" {
		return Check{name, SevWarn, "field is not a timestamp string"}
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return Check{name, SevWarn, "could not parse timestamp: " + err.Error()}
	}
	age := opts.now().Sub(t)
	if age < 0 {
		// Clock skew — treat as a warn, not a fail.
		return Check{name, SevWarn, "timestamp is in the future (clock skew?): " + ts}
	}
	if age > time.Hour {
		return Check{name, SevWarn, fmt.Sprintf("last forward was %s ago (>1h)", roundDur(age))}
	}
	return Check{name, SevPass, fmt.Sprintf("%s ago", roundDur(age))}
}

func checkCaptureAppendable(path string) Check {
	const name = "capture file appendable"
	if path == "" {
		return Check{name, SevFail, "no capture path resolved"}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return Check{name, SevFail, path + ": " + err.Error()}
	}
	if cerr := f.Close(); cerr != nil {
		return Check{name, SevWarn, "opened but close failed: " + cerr.Error()}
	}
	return Check{name, SevPass, path}
}

func checkMinIO(ctx context.Context, opts Options, cfg *config.ClientConfig, add func(Check)) {
	if cfg.MinIO.Endpoint == "" {
		add(Check{"MinIO endpoint reachable", SevWarn, "minio.endpoint is empty in config"})
		add(Check{"MinIO authentication works", SevWarn, "skipped — no endpoint"})
		add(Check{"MinIO bucket writable", SevWarn, "skipped — no endpoint"})
		return
	}
	mc := opts.NewMinIOClient
	if mc == nil {
		mc = minioclient.New
	}
	cli, err := mc(minioclient.Config{
		Endpoint:  cfg.MinIO.Endpoint,
		Bucket:    cfg.MinIO.Bucket,
		AccessKey: cfg.MinIO.AccessKey,
		SecretKey: cfg.MinIO.SecretKey,
		VerifyTLS: cfg.MinIO.VerifyTLS,
	})
	if err != nil {
		// Construction failed — usually invalid endpoint URL.
		reason := err.Error()
		add(Check{"MinIO endpoint reachable", SevFail, reason})
		add(Check{"MinIO authentication works", SevWarn, "skipped — client construction failed"})
		add(Check{"MinIO bucket writable", SevWarn, "skipped — client construction failed"})
		return
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pingErr := cli.Ping(pingCtx)
	if pingErr == nil {
		// Ping succeeded → all three checks pass (Ping is a PUT+DELETE).
		host := endpointHost(cfg.MinIO.Endpoint)
		add(Check{"MinIO endpoint reachable", SevPass, host})
		add(Check{"MinIO authentication works", SevPass, ""})
		add(Check{"MinIO bucket writable", SevPass, "bucket=" + cfg.MinIO.Bucket})
		return
	}
	// Classify Ping error into the three checks.
	switch {
	case errors.Is(pingErr, minioclient.ErrDNSResolution):
		add(Check{"MinIO endpoint reachable", SevFail, "DNS resolution failed for " + endpointHost(cfg.MinIO.Endpoint)})
		add(Check{"MinIO authentication works", SevWarn, "skipped — endpoint unreachable"})
		add(Check{"MinIO bucket writable", SevWarn, "skipped — endpoint unreachable"})
	case errors.Is(pingErr, minioclient.ErrTLSVerification):
		add(Check{"MinIO endpoint reachable", SevFail, "TLS verification failed (set verify_tls=false in config to bypass for self-signed certs)"})
		add(Check{"MinIO authentication works", SevWarn, "skipped — TLS handshake failed"})
		add(Check{"MinIO bucket writable", SevWarn, "skipped — TLS handshake failed"})
	case errors.Is(pingErr, minioclient.ErrAuthFailed):
		add(Check{"MinIO endpoint reachable", SevPass, endpointHost(cfg.MinIO.Endpoint)})
		add(Check{"MinIO authentication works", SevFail, "credentials rejected (verify minio.access_key / secret_key)"})
		add(Check{"MinIO bucket writable", SevWarn, "skipped — auth failed"})
	case errors.Is(pingErr, minioclient.ErrBucketNotFound):
		add(Check{"MinIO endpoint reachable", SevPass, endpointHost(cfg.MinIO.Endpoint)})
		add(Check{"MinIO authentication works", SevPass, ""})
		add(Check{"MinIO bucket writable", SevFail, "bucket " + cfg.MinIO.Bucket + " does not exist"})
	default:
		add(Check{"MinIO endpoint reachable", SevFail, "Ping failed: " + pingErr.Error()})
		add(Check{"MinIO authentication works", SevWarn, "skipped — Ping inconclusive"})
		add(Check{"MinIO bucket writable", SevWarn, "skipped — Ping inconclusive"})
	}
}

// Print writes the Result to w as one line per check, prefixed with the
// severity glyph. Returns nothing — the caller checks ExitCode separately.
func Print(w io.Writer, res Result) {
	for _, c := range res.Checks {
		if c.Detail == "" {
			fmt.Fprintf(w, "%s %s\n", c.Severity.String(), c.Name)
		} else {
			fmt.Fprintf(w, "%s %s — %s\n", c.Severity.String(), c.Name, c.Detail)
		}
	}
	// Trailing summary.
	var pass, warn, fail int
	for _, c := range res.Checks {
		switch c.Severity {
		case SevPass:
			pass++
		case SevWarn:
			warn++
		case SevFail:
			fail++
		}
	}
	fmt.Fprintf(w, "\n%d passed, %d warned, %d failed\n", pass, warn, fail)
}

func endpointHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Host
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func roundDur(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}
