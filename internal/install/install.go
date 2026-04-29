package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/user"
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

	// SkipSelfTest, when true, omits the post-install `marc proxy --self-test`
	// gate. Set by tests and by --skip-load (no daemon to test against).
	SkipSelfTest bool

	// Stdout and Stderr receive informational and error output.
	Stdout io.Writer
	Stderr io.Writer

	// geteuid is injected in tests to simulate non-root on Linux.
	// Leave nil for production (uses os.Geteuid).
	geteuid func() int

	// verifyHook overrides the post-install verification step. When nil,
	// runSelfTestSubprocess is used. Tests inject a fake to assert the gate
	// without spawning a real binary.
	verifyHook func(ctx context.Context, opts Options) error
}

// templateData holds the values interpolated into service unit templates.
type templateData struct {
	BinaryPath string
	ConfigPath string
	LogPath    string
	User       string // unix user the daemon runs as (Linux only)
	Group      string // unix group the daemon runs as (Linux only)
	HomeDir    string // home directory of User (used in ReadWritePaths)
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

// resolveTargetUser returns the user the daemon should run as on Linux.
// Priority:
//
//	1. SUDO_USER env var (set when invoked via sudo) — preserves the calling
//	   user so capture.jsonl in their ~/.marc/ stays owned by them.
//	2. USER env var.
//	3. The euid's name from /etc/passwd.
//
// Also returns the user's primary group and home directory, used by the
// systemd template for ReadWritePaths (so ProtectSystem=strict can grant
// the daemon write access to ~/.marc/ without granting the rest of the FS).
func resolveTargetUser() (username, group, home string) {
	candidate := os.Getenv("SUDO_USER")
	if candidate == "" {
		candidate = os.Getenv("USER")
	}
	if candidate != "" {
		if u, err := user.Lookup(candidate); err == nil {
			grp := u.Gid
			if g, err := user.LookupGroupId(u.Gid); err == nil {
				grp = g.Name
			}
			return u.Username, grp, u.HomeDir
		}
	}
	// Fallback: current effective uid.
	if u, err := user.Current(); err == nil {
		grp := u.Gid
		if g, err := user.LookupGroupId(u.Gid); err == nil {
			grp = g.Name
		}
		return u.Username, grp, u.HomeDir
	}
	return "", "", ""
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
