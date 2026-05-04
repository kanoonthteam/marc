package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/caffeaun/marc/internal/config"
	"github.com/caffeaun/marc/internal/proxy"
	"github.com/caffeaun/marc/internal/selftest"
)

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Run the HTTPS proxy daemon (forwards to api.anthropic.com)",
	Long: `proxy listens on the configured address and forwards all /v1/* requests
to https://api.anthropic.com, streaming responses back to the caller and
appending a capture event to ~/.marc/capture.jsonl on completion.

Wire it into your shell with:
  export ANTHROPIC_BASE_URL=http://localhost:8082`,
	SilenceUsage: true,
	RunE:         runProxy,
}

func init() {
	proxyCmd.Flags().String(
		"listen-addr",
		"",
		"address and port for the proxy to listen on (overrides config)",
	)
	proxyCmd.Flags().Bool(
		"self-test",
		false,
		"start the proxy on an ephemeral port, send one Anthropic request through it, "+
			"verify the response and capture, then exit (0=pass, 1=fail)",
	)
	proxyCmd.Flags().String(
		"self-test-upstream-url",
		"",
		"override the upstream URL for --self-test only (used by CI to point at a fake server)",
	)
}

func runProxy(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	selfTest, _ := cmd.Flags().GetBool("self-test")
	if selfTest {
		return runProxySelfTest(ctx, cmd)
	}

	// JSON-structured logs to stderr so each request lifecycle line is one
	// machine-readable record. This is what `marc doctor` and journalctl
	// consumers parse. Set MARC_PROXY_DEBUG=1 to drop to debug level —
	// useful when investigating slow-streaming or stalled requests.
	logLevel := slog.LevelInfo
	if os.Getenv("MARC_PROXY_DEBUG") == "1" {
		logLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	})))

	cfg, err := loadProxyConfig(cmd)
	if err != nil {
		return err
	}

	slog.Info("marc proxy starting",
		slog.String("listen", cfg.ListenAddr),
		slog.String("upstream", cfg.UpstreamURL),
		slog.String("machine", cfg.Machine),
		slog.String("capture", cfg.CapturePath),
	)

	if err := proxy.Run(ctx, cfg); err != nil {
		return fmt.Errorf("proxy: %w", err)
	}
	return nil
}

// runProxySelfTest stands up the proxy on an ephemeral port, sends one real
// request through it, validates the response and capture, and prints a
// check-mark report. Returns a non-nil error iff the self-test failed,
// which makes Cobra exit 1.
func runProxySelfTest(ctx context.Context, cmd *cobra.Command) error {
	// Debug-only escape hatch: force self-test to fail. Used by the install
	// rollback demo and integration tests. Never set in normal operation.
	if os.Getenv("MARC_FORCE_SELF_TEST_FAIL") == "1" {
		fmt.Fprintln(cmd.OutOrStdout(), "✗ self-test forced to fail (MARC_FORCE_SELF_TEST_FAIL=1)")
		return fmt.Errorf("self-test forced to fail by MARC_FORCE_SELF_TEST_FAIL")
	}

	// Suppress the proxy's own JSON request-lifecycle logs so the test
	// report stays readable. They go to /dev/null for the self-test only.
	slog.SetDefault(slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{
		Level: slog.LevelError,
	})))

	cfg, clientCfg, err := loadProxyConfigWithClientCfg(cmd)
	if err != nil {
		return err
	}
	upstreamOverride, _ := cmd.Flags().GetString("self-test-upstream-url")

	res := selftest.Run(ctx, selftest.Options{
		Config:           cfg,
		APIKey:           selftest.LoadAPIKey(clientCfg),
		UpstreamOverride: upstreamOverride,
		Stdout:           cmd.OutOrStdout(),
	})

	if !res.Success {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"\nSelf-test FAILED at step: %s\n  Reason: %s\n  Hint:   %s\n",
			res.FailedStep, res.FailedReason, res.Hint)
		return fmt.Errorf("self-test failed at %q", res.FailedStep)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "\n✓ marc proxy self-test passed.")
	return nil
}

// loadProxyConfig builds a proxy.Config by:
//  1. Attempting to load ~/.marc/config.toml.
//  2. Applying --listen-addr flag override if provided.
//  3. Falling back to sensible defaults when config is absent.
func loadProxyConfig(cmd *cobra.Command) (proxy.Config, error) {
	cfg, _, err := loadProxyConfigWithClientCfg(cmd)
	return cfg, err
}

// loadProxyConfigWithClientCfg is loadProxyConfig but also returns the parsed
// ClientConfig (or nil) so callers like --self-test can read sections that
// don't map onto proxy.Config (e.g. [anthropic].api_key).
func loadProxyConfigWithClientCfg(cmd *cobra.Command) (proxy.Config, *config.ClientConfig, error) {
	listenFlag, _ := cmd.Flags().GetString("listen-addr")

	// Expand the config file path.
	cfgPath := configFile
	if strings.HasPrefix(cfgPath, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			cfgPath = filepath.Join(home, cfgPath[2:])
		}
	}

	// Try to load the client config. If it doesn't exist or has wrong perms,
	// fall back to defaults rather than failing hard — the user may not have
	// run `marc configure` yet.
	var clientCfg *config.ClientConfig
	if _, statErr := os.Stat(cfgPath); statErr == nil {
		loaded, loadErr := config.LoadClient(cfgPath)
		if loadErr != nil {
			slog.Warn("proxy: could not load config, using defaults",
				slog.String("path", cfgPath),
				slog.Any("error", loadErr),
			)
		} else {
			clientCfg = loaded
		}
	} else {
		slog.Warn("proxy: config not found, using defaults", slog.String("path", cfgPath))
	}

	// Build proxy.Config with defaults.
	proxyCfg := proxy.Config{
		ListenAddr:      "127.0.0.1:8082",
		UpstreamURL:     "https://api.anthropic.com",
		CapturePath:     defaultCapturePath(),
		StrippedHeaders: []string{"authorization", "x-api-key", "cookie"},
		EventChanCap:    256,
		Version:         version,
	}

	// Machine name: hostname as fallback.
	hostname, _ := os.Hostname()
	proxyCfg.Machine = hostname

	// Apply values from config file when available.
	if clientCfg != nil {
		if clientCfg.Proxy.ListenAddr != "" {
			proxyCfg.ListenAddr = clientCfg.Proxy.ListenAddr
		}
		if clientCfg.Proxy.UpstreamURL != "" {
			proxyCfg.UpstreamURL = clientCfg.Proxy.UpstreamURL
		}
		if len(clientCfg.Proxy.StrippedHeaders) > 0 {
			proxyCfg.StrippedHeaders = clientCfg.Proxy.StrippedHeaders
		}
		if clientCfg.Paths.CaptureFile != "" {
			proxyCfg.CapturePath = clientCfg.Paths.CaptureFile
		}
		if clientCfg.MachineName != "" {
			proxyCfg.Machine = clientCfg.MachineName
		}
		// Map client profiles → proxy profiles. Resolve each profile's API
		// key from its env var at startup time (so the proxy doesn't need to
		// hold the env name; the request handler can do a direct lookup).
		if len(clientCfg.Profiles) > 0 {
			proxyCfg.Profiles = make(map[string]proxy.ProxyProfile, len(clientCfg.Profiles))
			for name, p := range clientCfg.Profiles {
				key := p.APIKey
				if key == "" && p.APIKeyEnv != "" {
					key = os.Getenv(p.APIKeyEnv)
				}
				proxyCfg.Profiles[name] = proxy.ProxyProfile{
					Name:            name,
					BaseURL:         p.BaseURL,
					AuthStyle:       p.AuthStyle,
					APIKey:          key,
					HeaderOverrides: p.HeaderOverrides,
				}
			}
			proxyCfg.DefaultProfile = clientCfg.DefaultProfile
		}
	}

	// --listen-addr flag takes highest priority.
	if listenFlag != "" {
		proxyCfg.ListenAddr = listenFlag
	}

	// Ensure the capture directory exists.
	if err := os.MkdirAll(filepath.Dir(proxyCfg.CapturePath), 0o700); err != nil {
		return proxy.Config{}, nil, fmt.Errorf("proxy: create capture directory: %w", err)
	}

	return proxyCfg, clientCfg, nil
}

// defaultCapturePath returns ~/.marc/capture.jsonl.
func defaultCapturePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "capture.jsonl"
	}
	return filepath.Join(home, ".marc", "capture.jsonl")
}
