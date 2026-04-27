package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Options controls the behaviour of Run.
type Options struct {
	// BinaryPath is the absolute path to the marc binary. Defaults to
	// os.Executable() resolved through filepath.EvalSymlinks.
	BinaryPath string

	// ConfigPath is the path to ~/.marc/config.toml passed to --config flag.
	ConfigPath string

	// Uninstall stops and removes all installed service units.
	Uninstall bool

	// DryRun renders templates and prints them to Stdout without writing or
	// invoking any service manager.
	DryRun bool

	// TargetDir overrides the default installation directory for tests.
	// Empty string means production path:
	//   Linux:  /etc/systemd/system/
	//   darwin: ~/Library/LaunchAgents/
	TargetDir string

	// SkipLoad suppresses calls to launchctl / systemctl. Used in tests.
	SkipLoad bool

	// Stdout and Stderr receive informational and error output.
	Stdout io.Writer
	Stderr io.Writer

	// geteuid is injected in tests to simulate non-root on Linux.
	// Leave nil for production (uses os.Geteuid).
	geteuid func() int
}

// templateData holds the values interpolated into service unit templates.
type templateData struct {
	BinaryPath string
	ConfigPath string
	LogPath    string
}

// Run is the main entry point. It dispatches based on runtime.GOOS.
func Run(ctx context.Context, opts Options) error {
	opts = applyDefaults(opts)

	switch runtime.GOOS {
	case "darwin":
		return installLaunchd(ctx, opts)
	case "linux":
		return installSystemd(ctx, opts)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// applyDefaults fills nil/zero fields with production values.
func applyDefaults(opts Options) Options {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.BinaryPath == "" {
		exe, err := os.Executable()
		if err == nil {
			if resolved, err2 := filepath.EvalSymlinks(exe); err2 == nil {
				exe = resolved
			}
		}
		opts.BinaryPath = exe
	}
	if opts.ConfigPath == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			opts.ConfigPath = filepath.Join(home, ".marc", "config.toml")
		}
	}
	if opts.geteuid == nil {
		opts.geteuid = os.Geteuid
	}
	return opts
}

// logPath returns the default log path: ~/.marc/marc.log, expanded.
func logPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.marc/marc.log"
	}
	return filepath.Join(home, ".marc", "marc.log")
}

// writeFile writes content to path with mode 0644, creating parent dirs.
func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("install: create directory %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("install: write %s: %w", path, err)
	}
	// Ensure mode 0644 regardless of umask.
	if err := os.Chmod(path, 0o644); err != nil {
		return fmt.Errorf("install: chmod %s: %w", path, err)
	}
	return nil
}

// printSection prints a clearly-labelled section to stdout.
func printSection(w io.Writer, label, content string) {
	fmt.Fprintf(w, "=== %s ===\n%s\n", label, strings.TrimRight(content, "\n"))
}
