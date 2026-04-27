package install

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"text/template"
)

// serverFS embeds the four server-mode systemd unit templates.
// Templates live in internal/install/systemd/ (separate from the client templates
// in internal/install/templates/ to keep the two sets cleanly partitioned).

//go:embed systemd/marc-process.service.tmpl systemd/marc-generate.service.tmpl systemd/marc-generate.timer.tmpl
var serverFS embed.FS

// serverUnit describes one server-mode systemd unit file.
type serverUnit struct {
	tmplName string // path inside serverFS, e.g. "systemd/marc-process.service.tmpl"
	fileName string // written as this name under TargetDir
}

// serverUnits lists the units in installation order.
// marc-generate.service is NOT enabled directly; it is activated by the timer.
// The Telegram bot does not run under marc-server: question-delivery and
// callback handling are implemented in ~/kanoonth/scripts/telegram-commands.py
// (the existing openclaw Telegram proxy) which reads marc's SQLite directly.
var serverUnits = []serverUnit{
	{tmplName: "systemd/marc-process.service.tmpl", fileName: "marc-process.service"},
	{tmplName: "systemd/marc-generate.service.tmpl", fileName: "marc-generate.service"},
	{tmplName: "systemd/marc-generate.timer.tmpl", fileName: "marc-generate.timer"},
}

// serverTemplateData holds the values interpolated into server unit templates.
type serverTemplateData struct {
	BinaryPath string
}

// ServerOptions controls the behaviour of RunServer.
type ServerOptions struct {
	// BinaryPath is the absolute path to the marc-server binary written into
	// ExecStart= directives. Defaults to /usr/local/bin/marc-server.
	BinaryPath string

	// TargetDir overrides /etc/systemd/system for tests.
	// Empty string means the production path.
	TargetDir string

	// Uninstall stops and removes all four installed units.
	Uninstall bool

	// DryRun renders templates and prints them to Out without writing files or
	// running systemctl. The root gate is skipped in dry-run mode.
	DryRun bool

	// Out receives all informational output. Defaults to os.Stdout.
	Out io.Writer

	// skipSystemctl suppresses all systemctl calls. Used in tests.
	skipSystemctl bool

	// geteuid is injected in tests to simulate non-root. Leave nil for production.
	geteuid func() int
}

// RunServer is the main entry point for marc-server install.
//
// Enforces:
//   - Linux-only gate: exit with "marc-server install: Linux only (detected: <goos>)".
//   - Root gate unless --dry-run: exit with "marc-server install: requires root (try sudo)".
//   - Renders four systemd unit files with the required hardening directives.
//   - --dry-run prints all four rendered units to Out without writing or running systemctl.
//   - --uninstall disables and removes all four units then reloads systemd.
//   - Re-running (idempotent): systemctl enable is idempotent; daemon-reload is safe.
func RunServer(ctx context.Context, opts ServerOptions) error {
	// Linux-only gate (AC #6).
	if runtime.GOOS != "linux" {
		return fmt.Errorf("marc-server install: Linux only (detected: %s)", runtime.GOOS)
	}

	opts = applyServerDefaults(opts)

	// Root gate — skipped for --dry-run (AC #4, #7).
	if !opts.DryRun && opts.geteuid() != 0 {
		return fmt.Errorf("marc-server install: requires root (try sudo)")
	}

	targetDir := opts.TargetDir
	if targetDir == "" {
		targetDir = "/etc/systemd/system"
	}

	data := serverTemplateData{BinaryPath: opts.BinaryPath}

	if opts.Uninstall {
		return runServerUninstall(ctx, opts, targetDir)
	}

	// Render all four unit files before touching the filesystem.
	rendered := make(map[string]string, len(serverUnits))
	for _, u := range serverUnits {
		content, err := renderServerUnit(u, data)
		if err != nil {
			return err
		}
		rendered[u.fileName] = content
	}

	// --dry-run: print all rendered units and return without writing (AC #2, #7).
	if opts.DryRun {
		for _, u := range serverUnits {
			printSection(opts.Out, u.fileName, rendered[u.fileName])
		}
		return nil
	}

	// Write unit files to TargetDir (AC #1, #4).
	for _, u := range serverUnits {
		path := filepath.Join(targetDir, u.fileName)
		if err := writeFile(path, rendered[u.fileName]); err != nil {
			return err
		}
		fmt.Fprintf(opts.Out, "Wrote %s\n", path)
	}

	if opts.skipSystemctl {
		return nil
	}

	// Reload systemd manager configuration.
	if err := runServerCmd(ctx, "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("marc-server install: systemctl daemon-reload: %w", err)
	}

	// Enable and start the long-running service and the timer.
	// marc-generate.service is NOT enabled here — it runs only via the timer.
	if err := runServerCmd(ctx, "systemctl", "enable", "--now",
		"marc-process.service",
		"marc-generate.timer",
	); err != nil {
		return fmt.Errorf("marc-server install: systemctl enable: %w", err)
	}

	fmt.Fprintf(opts.Out, "marc-server services installed and started\n")
	return nil
}

// runServerUninstall disables and removes all four server units, then reloads
// the systemd daemon (AC #3).
func runServerUninstall(ctx context.Context, opts ServerOptions, targetDir string) error {
	if !opts.skipSystemctl {
		// Disable and stop the enabled units.
		_ = runServerCmd(ctx, "systemctl", "disable", "--now",
			"marc-process.service",
			"marc-generate.timer",
		)
		// Stop marc-generate.service in case it is currently running.
		_ = runServerCmd(ctx, "systemctl", "stop", "marc-generate.service")
	}

	for _, u := range serverUnits {
		path := filepath.Join(targetDir, u.fileName)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("marc-server install: remove %s: %w", path, err)
		}
		fmt.Fprintf(opts.Out, "Removed %s\n", path)
	}

	if !opts.skipSystemctl {
		_ = runServerCmd(ctx, "systemctl", "daemon-reload")
	}

	fmt.Fprintf(opts.Out, "marc-server units uninstalled\n")
	return nil
}

// renderServerUnit renders a single server unit template and returns its content.
func renderServerUnit(u serverUnit, data serverTemplateData) (string, error) {
	raw, err := serverFS.ReadFile(u.tmplName)
	if err != nil {
		return "", fmt.Errorf("marc-server install: read template %s: %w", u.tmplName, err)
	}
	t, err := template.New(u.fileName).Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("marc-server install: parse template %s: %w", u.fileName, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("marc-server install: render template %s: %w", u.fileName, err)
	}
	return buf.String(), nil
}

// applyServerDefaults fills zero-value ServerOptions fields with production defaults.
func applyServerDefaults(opts ServerOptions) ServerOptions {
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.BinaryPath == "" {
		opts.BinaryPath = "/usr/local/bin/marc-server"
	}
	if opts.geteuid == nil {
		opts.geteuid = os.Geteuid
	}
	return opts
}

// runServerCmd runs an external command and returns a combined error on failure.
// It is separate from runCmd (the client helper) to keep the two independent.
func runServerCmd(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w\n%s", name, args, err, string(out))
	}
	return nil
}
