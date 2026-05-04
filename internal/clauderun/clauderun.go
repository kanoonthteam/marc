// Package clauderun implements `marc <claude-args>`: spawn the Claude CLI
// with ANTHROPIC_BASE_URL pointed at the marc proxy for one invocation.
//
// The motivation is opt-in capture: rather than exporting
// ANTHROPIC_BASE_URL globally in ~/.zshrc (which captures every Claude
// session, including throwaways), the operator can run `marc --continue`
// when they want a session captured and `claude --continue` otherwise.
package clauderun

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/caffeaun/marc/internal/config"
)

// Options controls a single passthrough invocation.
type Options struct {
	// Args are the arguments forwarded to claude (everything after the
	// `marc` binary name when no marc subcommand matched).
	Args []string

	// ConfigPath is ~/.marc/config.toml. Empty → expanded from os.UserHomeDir.
	ConfigPath string

	// ClaudeBinary names the CLI to spawn. Defaults to "claude" (LookPath).
	ClaudeBinary string

	// Stdin / Stdout / Stderr default to os.Stdin/Stdout/Stderr when nil.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// dial is an injection point for tests. nil → net.DialTimeout.
	dial func(network, addr string, timeout time.Duration) (net.Conn, error)
}

// Run looks up the configured proxy address, verifies a TCP connection can
// be opened to it, then exec's claude with ANTHROPIC_BASE_URL set to that
// address. Claude's exit code is propagated via the returned error: callers
// should check for *exec.ExitError and exit with .ExitCode().
//
// Profile selection (AWS-style precedence):
//
//	--profile <name> in opts.Args  >  MARC_PROFILE env  >  cfg.DefaultProfile  >  "anthropic"
//
// The resolved profile name becomes the URL prefix passed to claude:
//
//	ANTHROPIC_BASE_URL=http://<listen_addr>/<profile>
//
// The proxy reads the prefix and routes the request to the matching upstream.
func Run(opts Options) error {
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	cfg, listenAddr, err := loadConfigAndAddr(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("marc: %w", err)
	}

	// Strip --profile from args and resolve to a real profile.
	profileFlag, claudeArgs := ParseProfileFlag(opts.Args)
	profileName := ResolveProfileName(profileFlag, cfg)
	resolvedName, _, profErr := cfg.ResolveProfile(profileName)
	if profErr != nil {
		fmt.Fprintf(stderr, "marc: %s\n", profErr.Error())
		return profErr
	}

	dial := opts.dial
	if dial == nil {
		dial = net.DialTimeout
	}
	conn, dialErr := dial("tcp", listenAddr, time.Second)
	if dialErr != nil {
		fmt.Fprintf(stderr,
			"marc: proxy not reachable at %s — start it before running marc:\n%s\n",
			listenAddr, restartHint())
		return fmt.Errorf("marc: dial proxy %s: %w", listenAddr, dialErr)
	}
	_ = conn.Close()

	claudeBin := opts.ClaudeBinary
	if claudeBin == "" {
		claudeBin = "claude"
	}
	resolved, err := exec.LookPath(claudeBin)
	if err != nil {
		return fmt.Errorf("marc: %s not found on PATH (install Claude Code from https://claude.com/claude-code)", claudeBin)
	}

	baseURL := "http://" + listenAddr + "/" + resolvedName

	cmd := exec.Command(resolved, claudeArgs...)
	cmd.Env = withEnv(os.Environ(), "ANTHROPIC_BASE_URL", baseURL)
	cmd.Stdin = opts.Stdin
	if cmd.Stdin == nil {
		cmd.Stdin = os.Stdin
	}
	cmd.Stdout = opts.Stdout
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = stderr

	return cmd.Run()
}

// loadConfigAndAddr loads the client config and returns the resolved proxy
// listen address. Falls back to a synthetic default-only config and 127.0.0.1:8082
// when no config file exists (or it can't be parsed) — same forgiving behaviour
// as before, just with the config object surfaced for profile resolution.
func loadConfigAndAddr(cfgPath string) (*config.ClientConfig, string, error) {
	defaultCfg := func() *config.ClientConfig {
		c := &config.ClientConfig{}
		// Trigger the same auto-migration that LoadClient runs so callers
		// always see a valid Profiles map and DefaultProfile.
		setProfilesViaPublicAPI(c, "https://api.anthropic.com")
		return c
	}

	if cfgPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return defaultCfg(), "127.0.0.1:8082", nil
		}
		cfgPath = filepath.Join(home, ".marc", "config.toml")
	}
	if _, err := os.Stat(cfgPath); err != nil {
		return defaultCfg(), "127.0.0.1:8082", nil
	}
	cfg, err := config.LoadClient(cfgPath)
	if err != nil {
		return defaultCfg(), "127.0.0.1:8082", nil
	}
	addr := strings.TrimSpace(cfg.Proxy.ListenAddr)
	if addr == "" {
		addr = "127.0.0.1:8082"
	}
	return cfg, addr, nil
}

// setProfilesViaPublicAPI configures a synthetic default profile when no
// config file is on disk. We can't call cfg.migrateProfiles directly (it's
// unexported); instead we set the legacy Proxy.UpstreamURL and call
// LoadClient's hook by serializing to disk then re-reading — too heavy.
// Simpler: build the map by hand here (mirrors migrateProfiles minimally).
func setProfilesViaPublicAPI(cfg *config.ClientConfig, baseURL string) {
	cfg.DefaultProfile = "anthropic"
	cfg.Profiles = map[string]config.ClientProfile{
		"anthropic": {
			BaseURL:   baseURL,
			APIKeyEnv: "ANTHROPIC_API_KEY",
			AuthStyle: "x-api-key",
		},
	}
}

// withEnv returns a copy of env with the given key set to value, replacing
// any existing definition. Used to inject ANTHROPIC_BASE_URL into the env
// of the spawned claude process without mutating the parent shell.
func withEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return append(out, prefix+value)
}

// restartHint returns the platform-appropriate command for restarting the
// marc-proxy daemon when it's down.
func restartHint() string {
	switch runtime.GOOS {
	case "linux":
		return "  sudo systemctl start marc-proxy"
	case "darwin":
		return `  launchctl kickstart -k "gui/$(id -u)/io.marc.proxy"`
	default:
		return "  (no restart command for " + runtime.GOOS + ")"
	}
}
