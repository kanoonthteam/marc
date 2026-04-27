package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/caffeaun/marc/internal/config"
	"github.com/caffeaun/marc/internal/minioclient"
)


// Options controls the behaviour of Run.
// Callers set the fields they need; zero values produce safe defaults.
type Options struct {
	// ConfigPath is the destination file (default expands to ~/.marc/config.toml).
	// The caller is responsible for expanding "~" before passing the value.
	ConfigPath string

	// Reset, Check, PrintDefault select the operating mode.
	// At most one should be true at a time.
	Reset        bool
	Check        bool
	PrintDefault bool

	// NonInteractive is set true when any non-interactive flag below is provided.
	// Run will bypass prompts and use those values directly.
	NonInteractive bool

	// Non-interactive flag values. Only consulted when NonInteractive is true.
	MachineName   string
	MinIOEndpoint string
	AccessKey     string
	SecretKey     string
	Bucket        string

	// Injection points. If nil, the production defaults are used.
	// NewClient defaults to minioclient.New.
	// Stdin defaults to os.Stdin; Stdout to os.Stdout; Stderr to os.Stderr.
	NewClient func(cfg minioclient.Config) (minioclient.Client, error)
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
}

// Run is the main entry point. It dispatches based on the operating mode flags.
func Run(ctx context.Context, opts Options) error {
	opts = applyDefaults(opts)

	switch {
	case opts.PrintDefault:
		return config.PrintDefaultClient(opts.Stdout)

	case opts.Check:
		return runCheck(ctx, opts)

	case opts.Reset:
		return runReset(ctx, opts)

	case opts.NonInteractive:
		return runNonInteractive(ctx, opts)

	default:
		return runInteractive(ctx, opts)
	}
}

// applyDefaults fills in nil/zero fields with their production values.
func applyDefaults(opts Options) Options {
	if opts.NewClient == nil {
		opts.NewClient = defaultNewClient()
	}
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.ConfigPath == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			opts.ConfigPath = filepath.Join(home, ".marc", "config.toml")
		}
	}
	return opts
}

// runCheck loads the existing config, runs the four validation steps,
// and prints per-step results. Exits with a non-nil error if any step fails.
func runCheck(ctx context.Context, opts Options) error {
	// Verify file permissions first.
	info, err := os.Stat(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("configure --check: cannot stat %s: %w\n  Run 'marc configure' to create it.", opts.ConfigPath, err)
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf(
			"configure --check: %s has permissions %04o; must be 0600\n  Fix with: chmod 0600 %s",
			opts.ConfigPath, info.Mode().Perm(), opts.ConfigPath,
		)
	}

	cfg, err := config.LoadClient(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("configure --check: %w", err)
	}

	minioCfg := minioclient.Config{
		Endpoint:  cfg.MinIO.Endpoint,
		Bucket:    cfg.MinIO.Bucket,
		AccessKey: cfg.MinIO.AccessKey,
		SecretKey: cfg.MinIO.SecretKey,
		VerifyTLS: cfg.MinIO.VerifyTLS,
	}

	results := ValidateMinIO(ctx, minioCfg, opts.NewClient)
	return printAndCheckResults(opts.Stdout, results)
}

// runReset prompts for confirmation, then runs the full interactive wizard.
// A single bufio.Reader wraps opts.Stdin so the buffered offset is shared.
func runReset(ctx context.Context, opts Options) error {
	r := bufio.NewReader(opts.Stdin)
	if _, err := os.Stat(opts.ConfigPath); err == nil {
		// File exists — ask for confirmation.
		fmt.Fprintf(opts.Stdout, "Reset will overwrite %s. Continue? [y/N]: ", opts.ConfigPath)
		answer, err := r.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("configure --reset: read confirmation: %w", err)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(opts.Stdout, "Reset cancelled; original config unchanged.")
			return nil
		}
	}
	// Proceed with a fresh interactive session using the same reader.
	return runInteractiveWithReader(ctx, opts, r)
}

// runNonInteractive builds a config from the flag values, validates, and writes.
func runNonInteractive(ctx context.Context, opts Options) error {
	cfg := buildConfigFromFlags(opts)
	if err := validateAndWrite(ctx, opts, cfg); err != nil {
		return err
	}
	fmt.Fprintf(opts.Stdout, "Configuration written to %s\n", opts.ConfigPath)
	return nil
}

// runInteractive prompts the user for every field, validates, and writes.
func runInteractive(ctx context.Context, opts Options) error {
	return runInteractiveWithReader(ctx, opts, bufio.NewReader(opts.Stdin))
}

// runInteractiveWithReader is the implementation that accepts an existing
// bufio.Reader so that runReset can share it without creating a new buffer.
func runInteractiveWithReader(ctx context.Context, opts Options, r *bufio.Reader) error {
	// Check whether an existing config should be overwritten.
	if _, err := os.Stat(opts.ConfigPath); err == nil && !opts.Reset {
		fmt.Fprintf(opts.Stdout, "%s already exists. Overwrite? [y/N]: ", opts.ConfigPath)
		answer, rerr := r.ReadString('\n')
		if rerr != nil && !errors.Is(rerr, io.EOF) {
			return fmt.Errorf("configure: read confirmation: %w", rerr)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(opts.Stdout, "Aborted; original config unchanged.")
			return nil
		}
	}

	hostname, _ := os.Hostname()

	// Prompt order matches spec §"marc configure (client)".
	machineName, err := prompt(r, opts.Stdout, "machine_name", hostname)
	if err != nil {
		return promptErr("machine_name", err)
	}

	endpoint, err := prompt(r, opts.Stdout, "minio.endpoint", "")
	if err != nil {
		return promptErr("minio.endpoint", err)
	}

	accessKey, err := prompt(r, opts.Stdout, "minio.access_key", "")
	if err != nil {
		return promptErr("minio.access_key", err)
	}

	// TODO: use golang.org/x/term.ReadPassword for hidden input on darwin/linux.
	// Currently reads plaintext for simplicity; functional but not ideal UX.
	secretKey, err := prompt(r, opts.Stdout, "minio.secret_key", "")
	if err != nil {
		return promptErr("minio.secret_key", err)
	}

	bucket, err := prompt(r, opts.Stdout, "minio.bucket", "marc")
	if err != nil {
		return promptErr("minio.bucket", err)
	}

	captureFile, err := prompt(r, opts.Stdout, "paths.capture_file", "~/.marc/capture.jsonl")
	if err != nil {
		return promptErr("paths.capture_file", err)
	}

	logFile, err := prompt(r, opts.Stdout, "paths.log_file", "~/.marc/marc.log")
	if err != nil {
		return promptErr("paths.log_file", err)
	}

	listenAddr, err := prompt(r, opts.Stdout, "proxy.listen_addr", "127.0.0.1:8082")
	if err != nil {
		return promptErr("proxy.listen_addr", err)
	}

	upstreamURL, err := prompt(r, opts.Stdout, "proxy.upstream_url", "https://api.anthropic.com")
	if err != nil {
		return promptErr("proxy.upstream_url", err)
	}

	rotateSizeMBStr, err := prompt(r, opts.Stdout, "shipper.rotate_size_mb", "5")
	if err != nil {
		return promptErr("shipper.rotate_size_mb", err)
	}
	rotateSizeMB, convErr := strconv.Atoi(rotateSizeMBStr)
	if convErr != nil {
		rotateSizeMB = 5
	}

	shipIntervalStr, err := prompt(r, opts.Stdout, "shipper.ship_interval_seconds", "30")
	if err != nil {
		return promptErr("shipper.ship_interval_seconds", err)
	}
	shipInterval, convErr := strconv.Atoi(shipIntervalStr)
	if convErr != nil {
		shipInterval = 30
	}

	// Default verify_tls based on scheme.
	defaultVerifyTLS := "true"
	if strings.HasPrefix(strings.ToLower(endpoint), "http://") {
		defaultVerifyTLS = "false"
		fmt.Fprintln(opts.Stdout, "  Warning: http:// endpoint — defaulting verify_tls to false")
	}

	verifyTLSStr, err := prompt(r, opts.Stdout, "minio.verify_tls (y/n)", defaultVerifyTLS)
	if err != nil {
		return promptErr("minio.verify_tls", err)
	}
	verifyTLS := parseBool(verifyTLSStr, true)

	cfg := &config.ClientConfig{
		MachineName: machineName,
		Paths: config.ClientPaths{
			CaptureFile: captureFile,
			LogFile:     logFile,
		},
		Proxy: config.ClientProxy{
			ListenAddr:      listenAddr,
			UpstreamURL:     upstreamURL,
			StrippedHeaders: []string{"authorization", "x-api-key", "cookie"},
		},
		Shipper: config.ClientShipper{
			RotateSizeMB:        rotateSizeMB,
			ShipIntervalSeconds: shipInterval,
		},
		MinIO: config.ClientMinIO{
			Endpoint:  endpoint,
			Bucket:    bucket,
			AccessKey: accessKey,
			SecretKey: secretKey,
			VerifyTLS: verifyTLS,
		},
	}

	if err := validateAndWrite(ctx, opts, cfg); err != nil {
		return err
	}

	fmt.Fprintf(opts.Stdout, "Configuration written to %s\n", opts.ConfigPath)
	return nil
}

// buildConfigFromFlags assembles a ClientConfig entirely from flag values.
func buildConfigFromFlags(opts Options) *config.ClientConfig {
	return &config.ClientConfig{
		MachineName: opts.MachineName,
		Paths: config.ClientPaths{
			CaptureFile: "~/.marc/capture.jsonl",
			LogFile:     "~/.marc/marc.log",
		},
		Proxy: config.ClientProxy{
			ListenAddr:      "127.0.0.1:8082",
			UpstreamURL:     "https://api.anthropic.com",
			StrippedHeaders: []string{"authorization", "x-api-key", "cookie"},
		},
		Shipper: config.ClientShipper{
			RotateSizeMB:        5,
			ShipIntervalSeconds: 30,
		},
		MinIO: config.ClientMinIO{
			Endpoint:  opts.MinIOEndpoint,
			Bucket:    opts.Bucket,
			AccessKey: opts.AccessKey,
			SecretKey: opts.SecretKey,
			VerifyTLS: !strings.HasPrefix(strings.ToLower(opts.MinIOEndpoint), "http://"),
		},
	}
}

// validateAndWrite runs all four validation steps; writes the file only on full success.
func validateAndWrite(ctx context.Context, opts Options, cfg *config.ClientConfig) error {
	minioCfg := minioclient.Config{
		Endpoint:  cfg.MinIO.Endpoint,
		Bucket:    cfg.MinIO.Bucket,
		AccessKey: cfg.MinIO.AccessKey,
		SecretKey: cfg.MinIO.SecretKey,
		VerifyTLS: cfg.MinIO.VerifyTLS,
	}

	fmt.Fprintln(opts.Stdout, "Validating MinIO connectivity...")
	results := ValidateMinIO(ctx, minioCfg, opts.NewClient)

	if err := printAndCheckResults(opts.Stdout, results); err != nil {
		return err
	}

	return writeConfig(opts.ConfigPath, cfg)
}

// printAndCheckResults prints validation step results and returns an error if any step failed.
func printAndCheckResults(w io.Writer, results []ValidationResult) error {
	var failed bool
	for _, r := range results {
		status := "PASS"
		if !r.Passed {
			status = "FAIL"
			failed = true
		}
		fmt.Fprintf(w, "  [%s] %s: %s\n", status, r.Step, r.Message)
	}
	if failed {
		return errors.New("one or more validation steps failed; configuration not written")
	}
	return nil
}

// writeConfig serialises cfg to path with mode 0600.
// It creates parent directories if they do not exist.
func writeConfig(path string, cfg *config.ClientConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("configure: create config directory: %w", err)
	}

	// Build TOML output that exactly mirrors the spec's template format.
	// We use BurntSushi/toml's Encoder to produce field names that match the
	// struct tags (snake_case), which matches PrintDefaultClient output.
	var sb strings.Builder
	enc := toml.NewEncoder(&sb)
	if err := enc.Encode(cfg); err != nil {
		return fmt.Errorf("configure: encode TOML: %w", err)
	}

	data := []byte(sb.String())
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("configure: write %s: %w", path, err)
	}
	// Ensure mode 0600 even if umask is permissive.
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("configure: chmod %s: %w", path, err)
	}
	return nil
}

// prompt writes "question [defaultValue]: " to w and reads a line from r.
// If the user enters an empty string, defaultValue is returned.
func prompt(r *bufio.Reader, w io.Writer, question, defaultValue string) (string, error) {
	if defaultValue != "" {
		fmt.Fprintf(w, "%s [%s]: ", question, defaultValue)
	} else {
		fmt.Fprintf(w, "%s: ", question)
	}
	line, err := r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultValue, nil
	}
	return line, nil
}

// parseBool converts a y/yes/true/1 string to true, n/no/false/0 to false.
// Falls back to def on unrecognised input.
func parseBool(s string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "y", "yes", "true", "1":
		return true
	case "n", "no", "false", "0":
		return false
	default:
		return def
	}
}

// promptErr wraps a prompt read error with context.
func promptErr(field string, err error) error {
	return fmt.Errorf("configure: reading %s: %w", field, err)
}
