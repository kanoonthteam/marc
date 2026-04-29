package install

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

//go:embed templates/io.marc.proxy.plist.tmpl templates/io.marc.ship.plist.tmpl
var launchdFS embed.FS

// launchdAgent describes a single LaunchAgent plist to install.
type launchdAgent struct {
	tmplName string // e.g. "templates/io.marc.proxy.plist.tmpl"
	fileName string // e.g. "io.marc.proxy.plist"
}

var launchdAgents = []launchdAgent{
	{tmplName: "templates/io.marc.proxy.plist.tmpl", fileName: "io.marc.proxy.plist"},
	{tmplName: "templates/io.marc.ship.plist.tmpl", fileName: "io.marc.ship.plist"},
}

// renderLaunchd renders a single launchd plist template and returns the content.
func renderLaunchd(tmplName string, data templateData) (string, error) {
	raw, err := launchdFS.ReadFile(tmplName)
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

// launchAgentsDir returns ~/Library/LaunchAgents.
func launchAgentsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("install: cannot determine home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

// installLaunchd handles macOS launchd install/uninstall/dry-run.
func installLaunchd(ctx context.Context, opts Options) error {
	targetDir := opts.TargetDir
	if targetDir == "" {
		var err error
		targetDir, err = launchAgentsDir()
		if err != nil {
			return err
		}
	}

	data := templateData{
		BinaryPath: opts.BinaryPath,
		ConfigPath: opts.ConfigPath,
		LogPath:    logPath(),
	}

	if opts.Uninstall {
		return uninstallLaunchd(ctx, opts, targetDir)
	}

	// Render all templates first to catch errors before writing.
	rendered := make(map[string]string, len(launchdAgents))
	for _, a := range launchdAgents {
		content, err := renderLaunchd(a.tmplName, data)
		if err != nil {
			return err
		}
		rendered[a.fileName] = content
	}

	if opts.DryRun {
		for _, a := range launchdAgents {
			printSection(opts.Stdout, a.fileName, rendered[a.fileName])
		}
		return nil
	}

	// Idempotency: unload any existing agent before re-writing.
	if !opts.SkipLoad {
		for _, a := range launchdAgents {
			path := filepath.Join(targetDir, a.fileName)
			if _, err := os.Stat(path); err == nil {
				// Ignore errors — plist may not be loaded yet.
				_ = runCmd(ctx, "launchctl", "unload", path)
			}
		}
	}

	// Write plist files.
	for _, a := range launchdAgents {
		path := filepath.Join(targetDir, a.fileName)
		if err := writeFile(path, rendered[a.fileName]); err != nil {
			return err
		}
		fmt.Fprintf(opts.Stdout, "Wrote %s\n", path)
	}

	if opts.SkipLoad {
		return nil
	}

	// Load each agent.
	for _, a := range launchdAgents {
		path := filepath.Join(targetDir, a.fileName)
		if err := runCmd(ctx, "launchctl", "load", "-w", path); err != nil {
			return fmt.Errorf("install: launchctl load %s: %w", path, err)
		}
	}

	fmt.Fprintf(opts.Stdout, "io.marc.proxy and io.marc.ship LaunchAgents installed and loaded\n")

	// Mirror the systemd post-install gate: run --self-test, roll back on
	// failure.
	return runPostInstallGate(ctx, opts, func() {
		for _, a := range launchdAgents {
			path := filepath.Join(targetDir, a.fileName)
			if uerr := runCmd(ctx, "launchctl", "unload", path); uerr != nil {
				fmt.Fprintf(opts.Stderr, "rollback: launchctl unload %s failed: %v\n", path, uerr)
			}
		}
	})
}

// uninstallLaunchd unloads and removes launchd plists.
func uninstallLaunchd(ctx context.Context, opts Options, targetDir string) error {
	for _, a := range launchdAgents {
		path := filepath.Join(targetDir, a.fileName)
		if !opts.SkipLoad {
			_ = runCmd(ctx, "launchctl", "unload", path)
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("install: remove %s: %w", path, err)
		}
		fmt.Fprintf(opts.Stdout, "Removed %s\n", path)
	}
	fmt.Fprintf(opts.Stdout, "io.marc.proxy and io.marc.ship LaunchAgents uninstalled\n")
	return nil
}
