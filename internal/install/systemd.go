package install

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

//go:embed templates/marc-proxy.service.tmpl templates/marc-ship.service.tmpl
var systemdFS embed.FS

// systemdUnit describes a single systemd unit to install.
type systemdUnit struct {
	tmplName string // e.g. "templates/marc-proxy.service.tmpl"
	fileName string // e.g. "marc-proxy.service"
}

var systemdUnits = []systemdUnit{
	{tmplName: "templates/marc-proxy.service.tmpl", fileName: "marc-proxy.service"},
	{tmplName: "templates/marc-ship.service.tmpl", fileName: "marc-ship.service"},
}

// renderSystemd renders a single systemd template and returns the content.
func renderSystemd(tmplName string, data templateData) (string, error) {
	raw, err := systemdFS.ReadFile(tmplName)
	if err != nil {
		return "", fmt.Errorf("install: read template %s: %w", tmplName, err)
	}
	t, err := template.New(tmplName).Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("install: parse template %s: %w", tmplName, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("install: render template %s: %w", tmplName, err)
	}
	return buf.String(), nil
}

// installSystemd handles Linux systemd install/uninstall/dry-run.
func installSystemd(ctx context.Context, opts Options) error {
	// Linux requires root only when actually writing to /etc/systemd/system/.
	// --dry-run just renders and prints templates and never touches the disk
	// or systemctl, so the root gate is skipped in that mode. A custom
	// TargetDir (used by tests and by user-mode systemd installs under
	// ~/.config/systemd/user/) also bypasses the gate since the operator
	// already chose a path they can write to.
	if !opts.DryRun && opts.TargetDir == "" && opts.geteuid() != 0 {
		return fmt.Errorf(
			"marc install on Linux requires root; re-run with sudo\n" +
				"  Remediation: sudo marc install",
		)
	}

	targetDir := opts.TargetDir
	if targetDir == "" {
		targetDir = "/etc/systemd/system"
	}

	user, group, home := resolveTargetUser()
	data := templateData{
		BinaryPath: opts.BinaryPath,
		ConfigPath: opts.ConfigPath,
		User:       user,
		Group:      group,
		HomeDir:    home,
	}

	if opts.Uninstall {
		return uninstallSystemd(ctx, opts, targetDir)
	}

	// Render all templates first to catch errors before writing.
	rendered := make(map[string]string, len(systemdUnits))
	for _, u := range systemdUnits {
		content, err := renderSystemd(u.tmplName, data)
		if err != nil {
			return err
		}
		rendered[u.fileName] = content
	}

	if opts.DryRun {
		for _, u := range systemdUnits {
			printSection(opts.Stdout, u.fileName, rendered[u.fileName])
		}
		return nil
	}

	// Idempotency: disable existing units before re-writing.
	if !opts.SkipLoad {
		for _, u := range systemdUnits {
			path := filepath.Join(targetDir, u.fileName)
			if _, err := os.Stat(path); err == nil {
				// Unit already installed — disable before re-installing.
				_ = runCmd(ctx, "systemctl", "disable", "--now", u.fileName)
			}
		}
	}

	// Write unit files.
	for _, u := range systemdUnits {
		path := filepath.Join(targetDir, u.fileName)
		if err := writeFile(path, rendered[u.fileName]); err != nil {
			return err
		}
		fmt.Fprintf(opts.Stdout, "Wrote %s\n", path)
	}

	if opts.SkipLoad {
		return nil
	}

	// Reload and enable units.
	if err := runCmd(ctx, "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("install: systemctl daemon-reload: %w", err)
	}

	unitNames := make([]string, len(systemdUnits))
	for i, u := range systemdUnits {
		unitNames[i] = u.fileName
	}

	args := append([]string{"enable", "--now"}, unitNames...)
	if err := runCmd(ctx, "systemctl", args...); err != nil {
		return fmt.Errorf("install: systemctl enable: %w", err)
	}

	fmt.Fprintf(opts.Stdout, "marc-proxy and marc-ship services installed and started\n")
	return nil
}

// uninstallSystemd stops and removes systemd units.
func uninstallSystemd(ctx context.Context, opts Options, targetDir string) error {
	for _, u := range systemdUnits {
		if !opts.SkipLoad {
			_ = runCmd(ctx, "systemctl", "disable", "--now", u.fileName)
		}
		path := filepath.Join(targetDir, u.fileName)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("install: remove %s: %w", path, err)
		}
		fmt.Fprintf(opts.Stdout, "Removed %s\n", path)
	}
	if !opts.SkipLoad {
		_ = runCmd(ctx, "systemctl", "daemon-reload")
	}
	fmt.Fprintf(opts.Stdout, "marc-proxy and marc-ship services uninstalled\n")
	return nil
}

// runCmd runs an external command and returns a combined error on failure.
func runCmd(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w\n%s", name, args, err, string(out))
	}
	return nil
}
