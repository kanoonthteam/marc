package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/caffeaun/marc/internal/config"
	"github.com/caffeaun/marc/internal/proxy"
)

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Run the HTTPS proxy daemon (forwards to api.anthropic.com)",
	Long: `proxy listens on the configured address and forwards all /v1/* requests
to https://api.anthropic.com, streaming responses back to the caller and
appending a capture event to ~/.marc/capture.jsonl on completion.

Wire it into your shell with:
  export ANTHROPIC_BASE_URL=http://localhost:8082`,
	RunE: runProxy,
}

func init() {
	proxyCmd.Flags().String(
		"listen-addr",
		"",
		"address and port for the proxy to listen on (overrides config)",
	)
}

func runProxy(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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

// loadProxyConfig builds a proxy.Config by:
//  1. Attempting to load ~/.marc/config.toml.
//  2. Applying --listen-addr flag override if provided.
//  3. Falling back to sensible defaults when config is absent.
func loadProxyConfig(cmd *cobra.Command) (proxy.Config, error) {
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
	}

	// --listen-addr flag takes highest priority.
	if listenFlag != "" {
		proxyCfg.ListenAddr = listenFlag
	}

	// Ensure the capture directory exists.
	if err := os.MkdirAll(filepath.Dir(proxyCfg.CapturePath), 0o700); err != nil {
		return proxy.Config{}, fmt.Errorf("proxy: create capture directory: %w", err)
	}

	return proxyCfg, nil
}

// defaultCapturePath returns ~/.marc/capture.jsonl.
func defaultCapturePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "capture.jsonl"
	}
	return filepath.Join(home, ".marc", "capture.jsonl")
}
