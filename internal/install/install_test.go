package install

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// --- helpers ---

func rootOpts(t *testing.T) Options {
	t.Helper()
	return Options{
		BinaryPath: "/usr/local/bin/marc",
		ConfigPath: "/home/user/.marc/config.toml",
		TargetDir:  t.TempDir(),
		SkipLoad:   true,
		geteuid:    func() int { return 0 }, // simulate root
	}
}

func captureOutput(opts *Options) (*bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	opts.Stdout = stdout
	opts.Stderr = stderr
	return stdout, stderr
}

// --- 1. TestRenderSystemdProxy ---

func TestRenderSystemdProxy(t *testing.T) {
	data := templateData{
		BinaryPath: "/foo/marc",
		ConfigPath: "/bar/cfg.toml",
	}
	content, err := renderSystemd("templates/marc-proxy.service.tmpl", data)
	if err != nil {
		t.Fatalf("renderSystemd proxy: %v", err)
	}
	for _, want := range []string{
		"[Unit]",
		"[Service]",
		"[Install]",
		"Restart=on-failure",
		"ExecStart=/foo/marc proxy --config /bar/cfg.toml",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("proxy systemd template missing %q\ngot:\n%s", want, content)
		}
	}
}

// --- 2. TestRenderSystemdShip ---

func TestRenderSystemdShip(t *testing.T) {
	data := templateData{
		BinaryPath: "/foo/marc",
		ConfigPath: "/bar/cfg.toml",
	}
	content, err := renderSystemd("templates/marc-ship.service.tmpl", data)
	if err != nil {
		t.Fatalf("renderSystemd ship: %v", err)
	}
	for _, want := range []string{
		"[Unit]",
		"[Service]",
		"[Install]",
		"Restart=on-failure",
		"ExecStart=/foo/marc ship --config /bar/cfg.toml",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("ship systemd template missing %q\ngot:\n%s", want, content)
		}
	}
}

// --- 3. TestRenderLaunchdProxy ---

func TestRenderLaunchdProxy(t *testing.T) {
	data := templateData{
		BinaryPath: "/usr/local/bin/marc",
		ConfigPath: "/Users/user/.marc/config.toml",
		LogPath:    "/Users/user/.marc/marc.log",
	}
	content, err := renderLaunchd("templates/io.marc.proxy.plist.tmpl", data)
	if err != nil {
		t.Fatalf("renderLaunchd proxy: %v", err)
	}

	// KeepAlive key followed (eventually) by <true/>
	keepAlive := regexp.MustCompile(`(?s)<key>KeepAlive</key>\s*<true/>`)
	if !keepAlive.MatchString(content) {
		t.Errorf("proxy plist missing KeepAlive=true\ngot:\n%s", content)
	}
	// RunAtLoad
	runAtLoad := regexp.MustCompile(`(?s)<key>RunAtLoad</key>\s*<true/>`)
	if !runAtLoad.MatchString(content) {
		t.Errorf("proxy plist missing RunAtLoad=true\ngot:\n%s", content)
	}
	if !strings.Contains(content, data.BinaryPath) {
		t.Errorf("proxy plist missing BinaryPath %q", data.BinaryPath)
	}
	if !strings.Contains(content, "<string>proxy</string>") {
		t.Errorf("proxy plist missing proxy subcommand string")
	}
	if !strings.Contains(content, data.ConfigPath) {
		t.Errorf("proxy plist missing ConfigPath %q", data.ConfigPath)
	}
}

// --- 4. TestRenderLaunchdShip ---

func TestRenderLaunchdShip(t *testing.T) {
	data := templateData{
		BinaryPath: "/usr/local/bin/marc",
		ConfigPath: "/Users/user/.marc/config.toml",
		LogPath:    "/Users/user/.marc/marc.log",
	}
	content, err := renderLaunchd("templates/io.marc.ship.plist.tmpl", data)
	if err != nil {
		t.Fatalf("renderLaunchd ship: %v", err)
	}

	keepAlive := regexp.MustCompile(`(?s)<key>KeepAlive</key>\s*<true/>`)
	if !keepAlive.MatchString(content) {
		t.Errorf("ship plist missing KeepAlive=true\ngot:\n%s", content)
	}
	if !strings.Contains(content, "<string>ship</string>") {
		t.Errorf("ship plist missing ship subcommand string")
	}
	if !strings.Contains(content, data.ConfigPath) {
		t.Errorf("ship plist missing ConfigPath %q", data.ConfigPath)
	}
}

// --- 5. TestDryRunDarwin ---

func TestDryRunDarwin(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		BinaryPath: "/usr/local/bin/marc",
		ConfigPath: "/Users/user/.marc/config.toml",
		TargetDir:  dir,
		SkipLoad:   true,
		DryRun:     true,
	}
	stdout, _ := captureOutput(&opts)

	if err := installLaunchd(context.Background(), opts); err != nil {
		t.Fatalf("installLaunchd dry-run: %v", err)
	}

	out := stdout.String()

	// Both plist labels must appear in output.
	if !strings.Contains(out, "io.marc.proxy.plist") {
		t.Error("dry-run output missing io.marc.proxy.plist header")
	}
	if !strings.Contains(out, "io.marc.ship.plist") {
		t.Error("dry-run output missing io.marc.ship.plist header")
	}

	// No files written.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("dry-run wrote %d file(s) to TargetDir; expected 0", len(entries))
	}
}

// --- 6. TestDryRunLinux ---

func TestDryRunLinux(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		BinaryPath: "/usr/local/bin/marc",
		ConfigPath: "/home/user/.marc/config.toml",
		TargetDir:  dir,
		SkipLoad:   true,
		DryRun:     true,
		geteuid:    func() int { return 0 },
	}
	stdout, _ := captureOutput(&opts)

	if err := installSystemd(context.Background(), opts); err != nil {
		t.Fatalf("installSystemd dry-run: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "marc-proxy.service") {
		t.Error("dry-run output missing marc-proxy.service header")
	}
	if !strings.Contains(out, "marc-ship.service") {
		t.Error("dry-run output missing marc-ship.service header")
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("dry-run wrote %d file(s) to TargetDir; expected 0", len(entries))
	}
}

// --- 7. TestWriteSystemdToTargetDir ---

func TestWriteSystemdToTargetDir(t *testing.T) {
	opts := rootOpts(t)
	captureOutput(&opts)

	if err := installSystemd(context.Background(), opts); err != nil {
		t.Fatalf("installSystemd: %v", err)
	}

	for _, u := range systemdUnits {
		path := filepath.Join(opts.TargetDir, u.fileName)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
		if perm := info.Mode().Perm(); perm != 0o644 {
			t.Errorf("%s: mode %04o, want 0644", u.fileName, perm)
		}
	}
}

// --- 8. TestWriteLaunchdToTargetDir ---

func TestWriteLaunchdToTargetDir(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("launchd test only runs on darwin")
	}
	dir := t.TempDir()
	opts := Options{
		BinaryPath: "/usr/local/bin/marc",
		ConfigPath: "/Users/user/.marc/config.toml",
		TargetDir:  dir,
		SkipLoad:   true,
	}
	captureOutput(&opts)

	if err := installLaunchd(context.Background(), opts); err != nil {
		t.Fatalf("installLaunchd: %v", err)
	}

	for _, a := range launchdAgents {
		path := filepath.Join(dir, a.fileName)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
		if perm := info.Mode().Perm(); perm != 0o644 {
			t.Errorf("%s: mode %04o, want 0644", a.fileName, perm)
		}
	}
}

// --- 9. TestUninstallRemovesFiles ---

func TestUninstallRemovesFiles(t *testing.T) {
	dir := t.TempDir()

	// Pre-create unit files.
	for _, u := range systemdUnits {
		path := filepath.Join(dir, u.fileName)
		if err := os.WriteFile(path, []byte("stub"), 0o644); err != nil {
			t.Fatalf("pre-create %s: %v", path, err)
		}
	}

	opts := Options{
		BinaryPath: "/usr/local/bin/marc",
		ConfigPath: "/home/user/.marc/config.toml",
		TargetDir:  dir,
		SkipLoad:   true,
		Uninstall:  true,
		geteuid:    func() int { return 0 },
	}
	captureOutput(&opts)

	if err := installSystemd(context.Background(), opts); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	for _, u := range systemdUnits {
		path := filepath.Join(dir, u.fileName)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed, got err: %v", path, err)
		}
	}
}

// --- 10. TestIdempotentReinstall ---

func TestIdempotentReinstall(t *testing.T) {
	opts := rootOpts(t)
	captureOutput(&opts)

	// First install.
	if err := installSystemd(context.Background(), opts); err != nil {
		t.Fatalf("first install: %v", err)
	}

	// Read contents after first install.
	firstContents := make(map[string]string)
	for _, u := range systemdUnits {
		data, err := os.ReadFile(filepath.Join(opts.TargetDir, u.fileName))
		if err != nil {
			t.Fatalf("read after first install: %v", err)
		}
		firstContents[u.fileName] = string(data)
	}

	// Second install (idempotent).
	if err := installSystemd(context.Background(), opts); err != nil {
		t.Fatalf("second install: %v", err)
	}

	// Content must be identical.
	for _, u := range systemdUnits {
		data, err := os.ReadFile(filepath.Join(opts.TargetDir, u.fileName))
		if err != nil {
			t.Fatalf("read after second install: %v", err)
		}
		if got := string(data); got != firstContents[u.fileName] {
			t.Errorf("%s content changed on re-install", u.fileName)
		}
	}
}

// --- 11. TestLinuxNonRootRejected ---

func TestLinuxNonRootRejected(t *testing.T) {
	// The root gate fires only when writing to the default /etc/systemd/system
	// path. Custom TargetDir (used for --user installs and tests) bypasses it
	// because the operator already picked a path they can write to.
	opts := Options{
		BinaryPath: "/usr/local/bin/marc",
		ConfigPath: "/home/user/.marc/config.toml",
		SkipLoad:   true,
		geteuid:    func() int { return 1000 }, // non-root
	}
	captureOutput(&opts)

	err := installSystemd(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error for non-root with default target dir, got nil")
	}
	if !strings.Contains(err.Error(), "requires root") {
		t.Errorf("error %q missing 'requires root'", err.Error())
	}
}

// TestLinuxNonRootBypassedByTargetDir confirms that a custom TargetDir lets
// non-root users install — needed for --user mode (~/.config/systemd/user).
func TestLinuxNonRootBypassedByTargetDir(t *testing.T) {
	opts := Options{
		BinaryPath: "/usr/local/bin/marc",
		ConfigPath: "/home/user/.marc/config.toml",
		TargetDir:  t.TempDir(),
		SkipLoad:   true,
		geteuid:    func() int { return 1000 }, // non-root
	}
	captureOutput(&opts)

	if err := installSystemd(context.Background(), opts); err != nil {
		t.Fatalf("non-root with custom TargetDir: unexpected error: %v", err)
	}
}

// TestLinuxNonRootDryRunBypassed confirms that --dry-run skips the root gate
// — printing templates should never need privileges.
func TestLinuxNonRootDryRunBypassed(t *testing.T) {
	opts := Options{
		BinaryPath: "/usr/local/bin/marc",
		ConfigPath: "/home/user/.marc/config.toml",
		DryRun:     true,
		geteuid:    func() int { return 1000 }, // non-root
	}
	captureOutput(&opts)

	if err := installSystemd(context.Background(), opts); err != nil {
		t.Fatalf("non-root with --dry-run: unexpected error: %v", err)
	}
}

// --- 12. TestRestartOnFailureGrep ---

func TestRestartOnFailureGrep(t *testing.T) {
	data := templateData{
		BinaryPath: "/bin/marc",
		ConfigPath: "/etc/marc/config.toml",
	}
	restartLine := regexp.MustCompile(`(?m)^Restart=on-failure$`)

	for _, tmpl := range []string{
		"templates/marc-proxy.service.tmpl",
		"templates/marc-ship.service.tmpl",
	} {
		content, err := renderSystemd(tmpl, data)
		if err != nil {
			t.Fatalf("renderSystemd %s: %v", tmpl, err)
		}
		if !restartLine.MatchString(content) {
			t.Errorf("%s: Restart=on-failure not found on its own line\ngot:\n%s", tmpl, content)
		}
	}
}

// --- 13. TestKeepAliveGrep ---

func TestKeepAliveGrep(t *testing.T) {
	data := templateData{
		BinaryPath: "/bin/marc",
		ConfigPath: "/etc/marc/config.toml",
		LogPath:    "/tmp/marc.log",
	}
	keepAlive := regexp.MustCompile(`(?s)<key>KeepAlive</key>\s*<true/>`)

	for _, tmpl := range []string{
		"templates/io.marc.proxy.plist.tmpl",
		"templates/io.marc.ship.plist.tmpl",
	} {
		content, err := renderLaunchd(tmpl, data)
		if err != nil {
			t.Fatalf("renderLaunchd %s: %v", tmpl, err)
		}
		if !keepAlive.MatchString(content) {
			t.Errorf("%s: KeepAlive key not present or not set to true\ngot:\n%s", tmpl, content)
		}
	}
}
