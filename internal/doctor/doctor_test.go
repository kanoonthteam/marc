package doctor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caffeaun/marc/internal/minioclient"
)

// writeConfig writes a TOML client config to dir/config.toml with mode 0600
// and returns the path.
func writeConfig(t *testing.T, dir string, contents string) string {
	t.Helper()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

const validConfigTOML = `machine_name = "test-host"

[paths]
capture_file = "%s"
log_file = "%s"

[proxy]
listen_addr = "127.0.0.1:8082"
upstream_url = "https://api.anthropic.com"

[shipper]
rotate_size_mb = 5
ship_interval_seconds = 30

[minio]
endpoint = "http://127.0.0.1:9000"
bucket = "marc"
access_key = "key"
secret_key = "secret"
verify_tls = false
`

// fakeProxy returns an httptest.Server whose /_marc/health returns the given
// JSON body (caller controls status field, last_successful_forward_at, etc).
func fakeProxy(t *testing.T, healthJSON string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_marc/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(healthJSON))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// extractHostPort returns the host:port of a fake server URL.
func extractHostPort(t *testing.T, srvURL string) string {
	t.Helper()
	// httptest URL is "http://127.0.0.1:NNNNN"
	return strings.TrimPrefix(srvURL, "http://")
}

// findCheck returns the first check whose Name equals or contains the
// substring. Tests use this rather than fixed indexes so insert-order changes
// don't make tests brittle.
func findCheck(t *testing.T, res Result, sub string) Check {
	t.Helper()
	for _, c := range res.Checks {
		if strings.Contains(c.Name, sub) {
			return c
		}
	}
	t.Fatalf("no check matched %q\nchecks: %+v", sub, res.Checks)
	return Check{}
}

// healthyOpts builds an Options that simulates a fully-healthy host: every
// systemd unit active, every MinIO call succeeds, env var set to the right
// thing, and the fake proxy reporting a recent successful forward.
func healthyOpts(t *testing.T) (Options, *fakeMinIO) {
	t.Helper()
	dir := t.TempDir()
	capturePath := filepath.Join(dir, "capture.jsonl")
	logPath := filepath.Join(dir, "marc.log")

	cfgPath := writeConfig(t, dir, fmt.Sprintf(validConfigTOML, capturePath, logPath))

	healthJSON := fmt.Sprintf(`{
		"status": "ok",
		"last_successful_forward_at": "%s",
		"last_error_at": null,
		"last_error_message": null,
		"requests_forwarded_total": 1,
		"requests_failed_total": 0,
		"upstream_url": "https://api.anthropic.com",
		"listen_addr": "127.0.0.1:8082",
		"version": "test-1.0"
	}`, time.Now().Add(-30*time.Second).Format(time.RFC3339Nano))
	srv := fakeProxy(t, healthJSON, http.StatusOK)
	srvAddr := extractHostPort(t, srv.URL)

	// Override the listen address in the config to match the fake server.
	cfgRewritten := fmt.Sprintf(strings.ReplaceAll(validConfigTOML, "127.0.0.1:8082", srvAddr), capturePath, logPath)
	if err := os.WriteFile(cfgPath, []byte(cfgRewritten), 0o600); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}

	fm := &fakeMinIO{}
	opts := Options{
		ConfigPath: cfgPath,
		HTTPClient: &http.Client{Timeout: time.Second},
		NewMinIOClient: func(_ minioclient.Config) (minioclient.Client, error) {
			return fm, nil
		},
		SystemctlIsActive: func(_ context.Context, _ string) (string, error) {
			return "active", nil
		},
		DialPort: func(_, addr string, _ time.Duration) error {
			if addr == srvAddr {
				return nil // listening
			}
			return errors.New("not listening")
		},
		Getenv: func(k string) string {
			if k == "ANTHROPIC_BASE_URL" {
				return "http://" + srvAddr
			}
			return ""
		},
		Now: time.Now,
	}
	return opts, fm
}

// fakeMinIO implements minioclient.Client and lets tests inject Ping errors.
type fakeMinIO struct {
	pingErr error
}

func (f *fakeMinIO) Ping(_ context.Context) error { return f.pingErr }
func (f *fakeMinIO) PutObject(_ context.Context, _ string, _ io.Reader, _ int64, _ string) error {
	return nil
}
func (f *fakeMinIO) GetObject(_ context.Context, _ string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeMinIO) MoveObject(_ context.Context, _, _ string) error {
	return errors.New("not implemented")
}
func (f *fakeMinIO) ListObjects(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}

// --- Tests --------------------------------------------------------------

func TestDoctor_Healthy_AllPass(t *testing.T) {
	opts, _ := healthyOpts(t)

	res := Run(context.Background(), opts)

	if got := res.ExitCode(); got != 0 {
		t.Errorf("ExitCode: want 0 (all pass-or-warn), got %d", got)
	}
	for _, c := range res.Checks {
		if c.Severity == SevFail {
			t.Errorf("unexpected FAIL: %s — %s", c.Name, c.Detail)
		}
	}

	// Spot-check that all 13 logical checks are represented.
	want := []string{
		"config file exists",
		"config file mode",
		"config schema parses",
		"ANTHROPIC_BASE_URL",
		"marc-proxy",
		"marc-ship",
		"proxy port listening",
		"/_marc/health",
		"last successful forward",
		"capture file appendable",
		"MinIO endpoint reachable",
		"MinIO authentication works",
		"MinIO bucket writable",
	}
	for _, sub := range want {
		findCheck(t, res, sub) // fails the test if missing
	}
}

func TestDoctor_ProxyDown_FailsCorrectChecks(t *testing.T) {
	dir := t.TempDir()
	capturePath := filepath.Join(dir, "capture.jsonl")
	logPath := filepath.Join(dir, "marc.log")
	cfgPath := writeConfig(t, dir, fmt.Sprintf(validConfigTOML, capturePath, logPath))

	opts := Options{
		ConfigPath: cfgPath,
		HTTPClient: &http.Client{Timeout: 200 * time.Millisecond},
		NewMinIOClient: func(_ minioclient.Config) (minioclient.Client, error) {
			return &fakeMinIO{}, nil
		},
		SystemctlIsActive: func(_ context.Context, _ string) (string, error) {
			return "failed", nil
		},
		DialPort: func(_, _ string, _ time.Duration) error {
			// Simulate "nothing listening on 8082".
			return &net.OpError{Op: "dial", Err: errors.New("connection refused")}
		},
		Getenv: func(k string) string {
			if k == "ANTHROPIC_BASE_URL" {
				return "http://127.0.0.1:8082"
			}
			return ""
		},
		Now: time.Now,
	}

	res := Run(context.Background(), opts)

	if got := res.ExitCode(); got != 1 {
		t.Errorf("ExitCode: want 1 (proxy down -> fail), got %d", got)
	}

	if c := findCheck(t, res, "marc-proxy"); c.Severity != SevFail {
		t.Errorf("marc-proxy: want FAIL (state=failed), got %v — %s", c.Severity, c.Detail)
	}
	if c := findCheck(t, res, "proxy port listening"); c.Severity != SevFail {
		t.Errorf("port listening: want FAIL (refused), got %v — %s", c.Severity, c.Detail)
	}
	if c := findCheck(t, res, "/_marc/health"); c.Severity != SevFail {
		t.Errorf("/_marc/health: want FAIL (unreachable), got %v — %s", c.Severity, c.Detail)
	}
}

func TestDoctor_ConfigMissing_StillReportsCleanly(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.toml")

	opts := Options{
		ConfigPath: missing,
		Stdout:     &bytes.Buffer{},
		HTTPClient: &http.Client{Timeout: 200 * time.Millisecond},
		Getenv: func(k string) string {
			return os.Getenv(k) // pass-through; doesn't matter for this case
		},
		SystemctlIsActive: func(_ context.Context, _ string) (string, error) {
			return "inactive", nil
		},
		DialPort: func(_, _ string, _ time.Duration) error { return errors.New("refused") },
		Now:      time.Now,
	}

	res := Run(context.Background(), opts)

	if got := res.ExitCode(); got != 1 {
		t.Errorf("ExitCode: want 1 (config missing -> fail), got %d", got)
	}

	first := findCheck(t, res, "config file exists")
	if first.Severity != SevFail {
		t.Errorf("'config file exists' should FAIL when missing, got %v", first.Severity)
	}
	// All MinIO checks should be skipped (warn) since we have no config.
	for _, sub := range []string{"MinIO endpoint reachable", "MinIO authentication works", "MinIO bucket writable"} {
		c := findCheck(t, res, sub)
		if c.Severity != SevWarn {
			t.Errorf("%s: want WARN (skipped), got %v — %s", sub, c.Severity, c.Detail)
		}
	}
}

func TestDoctor_ConfigBadMode_FailsModeCheckButContinues(t *testing.T) {
	dir := t.TempDir()
	capturePath := filepath.Join(dir, "capture.jsonl")
	logPath := filepath.Join(dir, "marc.log")
	cfgPath := writeConfig(t, dir, fmt.Sprintf(validConfigTOML, capturePath, logPath))
	if err := os.Chmod(cfgPath, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	opts := Options{
		ConfigPath: cfgPath,
		HTTPClient: &http.Client{Timeout: 200 * time.Millisecond},
		NewMinIOClient: func(_ minioclient.Config) (minioclient.Client, error) {
			return &fakeMinIO{}, nil
		},
		SystemctlIsActive: func(_ context.Context, _ string) (string, error) {
			return "active", nil
		},
		DialPort: func(_, _ string, _ time.Duration) error { return errors.New("refused") },
		Getenv:   func(k string) string { return "" },
		Now:      time.Now,
	}

	res := Run(context.Background(), opts)

	mode := findCheck(t, res, "config file mode")
	if mode.Severity != SevFail {
		t.Errorf("config file mode: want FAIL on 0644, got %v", mode.Severity)
	}
	if !strings.Contains(mode.Detail, "0644") {
		t.Errorf("mode detail should mention 0644, got %q", mode.Detail)
	}
}

func TestDoctor_AnthropicBaseURLUnset_Warn(t *testing.T) {
	opts, _ := healthyOpts(t)
	opts.Getenv = func(k string) string { return "" } // ANTHROPIC_BASE_URL not set

	res := Run(context.Background(), opts)
	c := findCheck(t, res, "ANTHROPIC_BASE_URL")
	if c.Severity != SevWarn {
		t.Errorf("ANTHROPIC_BASE_URL unset: want WARN, got %v", c.Severity)
	}
	if !strings.Contains(c.Detail, "not set") {
		t.Errorf("detail should mention 'not set', got %q", c.Detail)
	}
}

func TestDoctor_AnthropicBaseURLMismatch_Warn(t *testing.T) {
	opts, _ := healthyOpts(t)
	opts.Getenv = func(k string) string {
		if k == "ANTHROPIC_BASE_URL" {
			return "http://wrong.example:9999"
		}
		return ""
	}

	res := Run(context.Background(), opts)
	c := findCheck(t, res, "ANTHROPIC_BASE_URL")
	if c.Severity != SevWarn {
		t.Errorf("ANTHROPIC_BASE_URL mismatch: want WARN, got %v", c.Severity)
	}
	if !strings.Contains(c.Detail, "wrong.example") {
		t.Errorf("detail should mention the wrong value, got %q", c.Detail)
	}
}

func TestDoctor_LastForwardOlderThanHour_Warn(t *testing.T) {
	dir := t.TempDir()
	capturePath := filepath.Join(dir, "capture.jsonl")
	logPath := filepath.Join(dir, "marc.log")
	cfgPath := writeConfig(t, dir, fmt.Sprintf(validConfigTOML, capturePath, logPath))

	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	healthJSON := fmt.Sprintf(`{
		"status": "ok",
		"last_successful_forward_at": "%s",
		"last_error_at": null,
		"last_error_message": null,
		"requests_forwarded_total": 1,
		"requests_failed_total": 0,
		"upstream_url": "https://api.anthropic.com",
		"listen_addr": "127.0.0.1:8082",
		"version": "test-1.0"
	}`, twoHoursAgo.Format(time.RFC3339Nano))
	srv := fakeProxy(t, healthJSON, http.StatusOK)
	srvAddr := extractHostPort(t, srv.URL)

	cfgRewritten := fmt.Sprintf(strings.ReplaceAll(validConfigTOML, "127.0.0.1:8082", srvAddr), capturePath, logPath)
	_ = os.WriteFile(cfgPath, []byte(cfgRewritten), 0o600)

	opts := Options{
		ConfigPath: cfgPath,
		HTTPClient: &http.Client{Timeout: time.Second},
		NewMinIOClient: func(_ minioclient.Config) (minioclient.Client, error) {
			return &fakeMinIO{}, nil
		},
		SystemctlIsActive: func(_ context.Context, _ string) (string, error) { return "active", nil },
		DialPort:          func(_, _ string, _ time.Duration) error { return nil },
		Getenv: func(k string) string {
			if k == "ANTHROPIC_BASE_URL" {
				return "http://" + srvAddr
			}
			return ""
		},
		Now: time.Now,
	}

	res := Run(context.Background(), opts)
	c := findCheck(t, res, "last successful forward")
	if c.Severity != SevWarn {
		t.Errorf("last forward 2h ago: want WARN, got %v", c.Severity)
	}
	if !strings.Contains(c.Detail, ">1h") && !strings.Contains(c.Detail, "h ago") {
		t.Errorf("detail should mention >1h, got %q", c.Detail)
	}
}

func TestDoctor_MinIOAuthFails(t *testing.T) {
	opts, fm := healthyOpts(t)
	fm.pingErr = minioclient.ErrAuthFailed

	res := Run(context.Background(), opts)
	if c := findCheck(t, res, "MinIO endpoint reachable"); c.Severity != SevPass {
		t.Errorf("auth fail should still pass reachability, got %v", c.Severity)
	}
	if c := findCheck(t, res, "MinIO authentication"); c.Severity != SevFail {
		t.Errorf("auth fail should FAIL auth check, got %v", c.Severity)
	}
	if c := findCheck(t, res, "MinIO bucket writable"); c.Severity != SevWarn {
		t.Errorf("auth fail should skip bucket check, got %v", c.Severity)
	}
}

func TestDoctor_MinIOBucketMissing(t *testing.T) {
	opts, fm := healthyOpts(t)
	fm.pingErr = minioclient.ErrBucketNotFound

	res := Run(context.Background(), opts)
	if c := findCheck(t, res, "MinIO endpoint reachable"); c.Severity != SevPass {
		t.Errorf("bucket missing should still pass reachability, got %v", c.Severity)
	}
	if c := findCheck(t, res, "MinIO authentication"); c.Severity != SevPass {
		t.Errorf("bucket missing should still pass auth, got %v", c.Severity)
	}
	if c := findCheck(t, res, "MinIO bucket writable"); c.Severity != SevFail {
		t.Errorf("bucket missing should FAIL bucket writable, got %v", c.Severity)
	}
}

// TestCheckLaunchdAgent_AllStates covers the three launchctl states the
// doctor distinguishes: running, loaded-but-stopped, and not-loaded.
func TestCheckLaunchdAgent_AllStates(t *testing.T) {
	cases := []struct {
		name     string
		state    string
		wantSev  Severity
		wantHint string // substring expected in Detail
	}{
		{"running", "running", SevPass, "loaded and running"},
		{"loaded-stopped", "loaded-stopped", SevFail, "kickstart"},
		{"not-loaded", "not-loaded", SevFail, "marc install"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := Options{
				LaunchctlList: func(_ context.Context, _ string) (string, error) {
					return tc.state, nil
				},
			}
			c := checkLaunchdAgent(context.Background(), opts, "io.marc.proxy")
			if c.Severity != tc.wantSev {
				t.Errorf("severity: want %v, got %v", tc.wantSev, c.Severity)
			}
			if !strings.Contains(c.Detail, tc.wantHint) {
				t.Errorf("detail %q should contain %q", c.Detail, tc.wantHint)
			}
		})
	}
}

// TestDoctor_DarwinUsesLaunchdNotSystemd verifies the platform switch:
// when Goos = "darwin" the run includes io.marc.proxy / io.marc.ship checks
// (not marc-proxy.service / marc-ship.service).
func TestDoctor_DarwinUsesLaunchdNotSystemd(t *testing.T) {
	opts, _ := healthyOpts(t)
	opts.Goos = "darwin"
	opts.LaunchctlList = func(_ context.Context, _ string) (string, error) {
		return "running", nil
	}

	res := Run(context.Background(), opts)

	if got := res.ExitCode(); got != 0 {
		for _, c := range res.Checks {
			if c.Severity == SevFail {
				t.Logf("FAIL: %s — %s", c.Name, c.Detail)
			}
		}
		t.Errorf("ExitCode: want 0 on healthy darwin host, got %d", got)
	}

	// Must include the launchd labels.
	findCheck(t, res, "io.marc.proxy")
	findCheck(t, res, "io.marc.ship")

	// Must NOT include the linux systemd unit names.
	for _, c := range res.Checks {
		if strings.Contains(c.Name, "marc-proxy.service") || strings.Contains(c.Name, "marc-ship.service") {
			t.Errorf("darwin run should not include systemd check %q", c.Name)
		}
	}
}

func TestDoctor_PrintFormat(t *testing.T) {
	res := Result{Checks: []Check{
		{"alpha", SevPass, ""},
		{"beta", SevWarn, "details here"},
		{"gamma", SevFail, "boom"},
	}}
	var buf bytes.Buffer
	Print(&buf, res)

	out := buf.String()
	if !strings.Contains(out, "✓ alpha") {
		t.Errorf("missing ✓ alpha line\n%s", out)
	}
	if !strings.Contains(out, "⚠ beta — details here") {
		t.Errorf("missing ⚠ beta line\n%s", out)
	}
	if !strings.Contains(out, "✗ gamma — boom") {
		t.Errorf("missing ✗ gamma line\n%s", out)
	}
	if !strings.Contains(out, "1 passed, 1 warned, 1 failed") {
		t.Errorf("missing summary\n%s", out)
	}
}
