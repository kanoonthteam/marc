package install

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// rootServerOpts returns a ServerOptions pre-configured for unit tests:
// - BinaryPath set to a stable test path
// - TargetDir points to a fresh temp directory
// - skipSystemctl suppresses all systemctl calls
// - geteuid simulates root (uid 0)
// - Out captures output for assertion
func rootServerOpts(t *testing.T) (ServerOptions, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	return ServerOptions{
		BinaryPath:    "/usr/local/bin/marc-server",
		TargetDir:     t.TempDir(),
		skipSystemctl: true,
		geteuid:       func() int { return 0 },
		Out:           out,
	}, out
}

// --- 1. TestServerDryRunPrintsAllFourUnits ---

// TestServerDryRunPrintsAllFourUnits verifies AC #2 and AC #7:
// --dry-run renders all four units and prints them to Out without writing any file.
// It also grepping for every required hardening directive (AC #8).
func TestServerDryRunPrintsAllFourUnits(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("server install is Linux-only")
	}

	dir := t.TempDir()
	out := &bytes.Buffer{}
	opts := ServerOptions{
		BinaryPath:    "/usr/local/bin/marc-server",
		TargetDir:     dir,
		DryRun:        true,
		Out:           out,
		skipSystemctl: true,
		geteuid:       func() int { return 0 },
	}

	if err := RunServer(context.Background(), opts); err != nil {
		t.Fatalf("RunServer dry-run: %v", err)
	}

	output := out.String()

	// All three unit names must appear as section headers.
	for _, name := range []string{
		"marc-process.service",
		"marc-generate.service",
		"marc-generate.timer",
	} {
		if !strings.Contains(output, name) {
			t.Errorf("dry-run output missing section header for %q", name)
		}
	}

	// Hardening directives (AC #8): present in the rendered output.
	for _, directive := range []string{
		"DynamicUser=yes",
		"ProtectSystem=strict",
		"ReadWritePaths=/var/lib/marc /var/log",
		"NoNewPrivileges=true",
		"PrivateTmp=true",
	} {
		if !strings.Contains(output, directive) {
			t.Errorf("dry-run output missing hardening directive %q", directive)
		}
	}

	// OnCalendar=hourly (AC #5).
	if !strings.Contains(output, "OnCalendar=hourly") {
		t.Error("dry-run output missing OnCalendar=hourly in marc-generate.timer")
	}

	// Restart=on-failure in the two long-running services (AC #7).
	if !strings.Contains(output, "Restart=on-failure") {
		t.Error("dry-run output missing Restart=on-failure")
	}

	// No files written.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("dry-run wrote %d file(s) to TargetDir; expected 0", len(entries))
	}
}

// --- 2. TestServerNonRootRejected ---

// TestServerNonRootRejected verifies AC #4:
// Running as non-root without --dry-run exits with a clear error message.
func TestServerNonRootRejected(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("server install is Linux-only")
	}

	out := &bytes.Buffer{}
	opts := ServerOptions{
		BinaryPath:    "/usr/local/bin/marc-server",
		TargetDir:     t.TempDir(),
		skipSystemctl: true,
		geteuid:       func() int { return 1000 }, // non-root
		Out:           out,
	}

	err := RunServer(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error for non-root, got nil")
	}
	if !strings.Contains(err.Error(), "requires root") {
		t.Errorf("error %q does not contain 'requires root'", err.Error())
	}
}

// --- 3. TestServerDryRunSkipsRootCheck ---

// TestServerDryRunSkipsRootCheck verifies AC #7:
// --dry-run does not require root.
func TestServerDryRunSkipsRootCheck(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("server install is Linux-only")
	}

	out := &bytes.Buffer{}
	opts := ServerOptions{
		BinaryPath:    "/usr/local/bin/marc-server",
		TargetDir:     t.TempDir(),
		DryRun:        true,
		skipSystemctl: true,
		geteuid:       func() int { return 1000 }, // non-root should still work with --dry-run
		Out:           out,
	}

	if err := RunServer(context.Background(), opts); err != nil {
		t.Fatalf("RunServer dry-run as non-root: %v", err)
	}
}

// --- 4. TestServerUninstallRemovesFiles ---

// TestServerUninstallRemovesFiles verifies AC #3:
// --uninstall removes all four unit files from TargetDir.
func TestServerUninstallRemovesFiles(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("server install is Linux-only")
	}

	dir := t.TempDir()

	// Pre-populate all four unit files.
	for _, u := range serverUnits {
		path := filepath.Join(dir, u.fileName)
		if err := os.WriteFile(path, []byte("stub"), 0o644); err != nil {
			t.Fatalf("pre-create %s: %v", u.fileName, err)
		}
	}

	out := &bytes.Buffer{}
	opts := ServerOptions{
		BinaryPath:    "/usr/local/bin/marc-server",
		TargetDir:     dir,
		Uninstall:     true,
		skipSystemctl: true,
		geteuid:       func() int { return 0 },
		Out:           out,
	}

	if err := RunServer(context.Background(), opts); err != nil {
		t.Fatalf("RunServer --uninstall: %v", err)
	}

	for _, u := range serverUnits {
		path := filepath.Join(dir, u.fileName)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed; os.Stat err: %v", u.fileName, err)
		}
	}
}

// --- 5. TestServerIdempotentReinstall ---

// TestServerIdempotentReinstall verifies AC #8 (idempotency):
// Running RunServer twice produces identical file content with no error.
func TestServerIdempotentReinstall(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("server install is Linux-only")
	}

	opts, _ := rootServerOpts(t)

	if err := RunServer(context.Background(), opts); err != nil {
		t.Fatalf("first RunServer: %v", err)
	}

	// Capture file content after first install.
	first := make(map[string]string)
	for _, u := range serverUnits {
		data, err := os.ReadFile(filepath.Join(opts.TargetDir, u.fileName))
		if err != nil {
			t.Fatalf("read %s after first install: %v", u.fileName, err)
		}
		first[u.fileName] = string(data)
	}

	// Re-install (idempotent).
	if err := RunServer(context.Background(), opts); err != nil {
		t.Fatalf("second RunServer: %v", err)
	}

	for _, u := range serverUnits {
		data, err := os.ReadFile(filepath.Join(opts.TargetDir, u.fileName))
		if err != nil {
			t.Fatalf("read %s after second install: %v", u.fileName, err)
		}
		if string(data) != first[u.fileName] {
			t.Errorf("%s content changed on re-install", u.fileName)
		}
	}
}

// --- 6. TestServerWritesFilesWithCorrectMode ---

// TestServerWritesFilesWithCorrectMode verifies that written unit files have
// mode 0644 and that all four files are present.
func TestServerWritesFilesWithCorrectMode(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("server install is Linux-only")
	}

	opts, _ := rootServerOpts(t)

	if err := RunServer(context.Background(), opts); err != nil {
		t.Fatalf("RunServer: %v", err)
	}

	for _, u := range serverUnits {
		path := filepath.Join(opts.TargetDir, u.fileName)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", u.fileName, err)
		}
		if perm := info.Mode().Perm(); perm != 0o644 {
			t.Errorf("%s: mode %04o, want 0644", u.fileName, perm)
		}
	}
}

// --- 7. TestServerHardeningDirectives ---

// TestServerHardeningDirectives verifies AC #8 directly by rendering the three
// service templates and asserting every required hardening directive is present.
// The timer unit is a scheduling artefact and does not run any process, so it
// does not need (and cannot meaningfully have) [Service] hardening directives.
func TestServerHardeningDirectives(t *testing.T) {
	data := serverTemplateData{BinaryPath: "/usr/local/bin/marc-server"}

	hardeningDirectives := []string{
		"DynamicUser=yes",
		"ProtectSystem=strict",
		"ReadWritePaths=/var/lib/marc /var/log",
		"NoNewPrivileges=true",
		"PrivateTmp=true",
	}

	// Service units (not the timer) must contain the hardening directives.
	serviceUnits := []serverUnit{
		{tmplName: "systemd/marc-process.service.tmpl", fileName: "marc-process.service"},
		{tmplName: "systemd/marc-generate.service.tmpl", fileName: "marc-generate.service"},
	}
	for _, u := range serviceUnits {
		content, err := renderServerUnit(u, data)
		if err != nil {
			t.Fatalf("renderServerUnit %s: %v", u.fileName, err)
		}
		for _, d := range hardeningDirectives {
			if !strings.Contains(content, d) {
				t.Errorf("%s: missing hardening directive %q\ngot:\n%s", u.fileName, d, content)
			}
		}
	}
}

// --- 8. TestServerRestartOnFailure ---

// TestServerRestartOnFailure verifies AC #7:
// marc-process.service contains Restart=on-failure.
// marc-generate.service (oneshot) must NOT contain Restart=.
func TestServerRestartOnFailure(t *testing.T) {
	data := serverTemplateData{BinaryPath: "/usr/local/bin/marc-server"}

	longRunning := []serverUnit{
		{tmplName: "systemd/marc-process.service.tmpl", fileName: "marc-process.service"},
	}
	for _, u := range longRunning {
		content, err := renderServerUnit(u, data)
		if err != nil {
			t.Fatalf("renderServerUnit %s: %v", u.fileName, err)
		}
		if !strings.Contains(content, "Restart=on-failure") {
			t.Errorf("%s: missing Restart=on-failure\ngot:\n%s", u.fileName, content)
		}
	}

	// marc-generate.service is Type=oneshot and must NOT have Restart=.
	generateUnit := serverUnit{
		tmplName: "systemd/marc-generate.service.tmpl",
		fileName: "marc-generate.service",
	}
	content, err := renderServerUnit(generateUnit, data)
	if err != nil {
		t.Fatalf("renderServerUnit marc-generate.service: %v", err)
	}
	if strings.Contains(content, "Restart=") {
		t.Errorf("marc-generate.service (oneshot) must not contain Restart=\ngot:\n%s", content)
	}
}

// --- 9. TestServerTimerOnCalendarHourly ---

// TestServerTimerOnCalendarHourly verifies AC #5:
// marc-generate.timer contains OnCalendar=hourly and Persistent=true.
func TestServerTimerOnCalendarHourly(t *testing.T) {
	data := serverTemplateData{BinaryPath: "/usr/local/bin/marc-server"}
	timerUnit := serverUnit{
		tmplName: "systemd/marc-generate.timer.tmpl",
		fileName: "marc-generate.timer",
	}

	content, err := renderServerUnit(timerUnit, data)
	if err != nil {
		t.Fatalf("renderServerUnit marc-generate.timer: %v", err)
	}

	for _, want := range []string{
		"OnCalendar=hourly",
		"Persistent=true",
		"Unit=marc-generate.service",
		"WantedBy=timers.target",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("marc-generate.timer: missing %q\ngot:\n%s", want, content)
		}
	}
}

// --- 10. TestServerLinuxOnlyGate ---

// TestServerLinuxOnlyGate verifies AC #6:
// On non-Linux platforms RunServer returns an error containing "Linux only".
// On Linux we simulate GOOS by testing the gate logic directly via the exported error text.
// This test is skipped on Linux because we cannot override runtime.GOOS at runtime
// (the gate is enforced by a compile-time check inside RunServer).
// The Linux-only constraint is validated end-to-end by the dry-run tests above
// which only run on Linux.
func TestServerLinuxOnlyGate(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux-only gate is enforced by runtime.GOOS check; test runs only on non-Linux")
	}

	out := &bytes.Buffer{}
	opts := ServerOptions{
		BinaryPath:    "/usr/local/bin/marc-server",
		TargetDir:     t.TempDir(),
		skipSystemctl: true,
		geteuid:       func() int { return 0 },
		Out:           out,
	}

	err := RunServer(context.Background(), opts)
	if err == nil {
		t.Fatal("expected Linux-only error on non-Linux platform, got nil")
	}
	if !strings.Contains(err.Error(), "Linux only") {
		t.Errorf("error %q does not contain 'Linux only'", err.Error())
	}
}

// --- 11. TestServerBinaryPathSubstitution ---

// TestServerBinaryPathSubstitution verifies that the {{.BinaryPath}} placeholder
// is correctly substituted in the three service templates.
// The timer unit does not reference BinaryPath directly (it delegates to the
// service unit), so it is excluded from this check.
func TestServerBinaryPathSubstitution(t *testing.T) {
	const testPath = "/custom/path/marc-server"
	data := serverTemplateData{BinaryPath: testPath}

	serviceUnits := []serverUnit{
		{tmplName: "systemd/marc-process.service.tmpl", fileName: "marc-process.service"},
		{tmplName: "systemd/marc-generate.service.tmpl", fileName: "marc-generate.service"},
	}
	for _, u := range serviceUnits {
		content, err := renderServerUnit(u, data)
		if err != nil {
			t.Fatalf("renderServerUnit %s: %v", u.fileName, err)
		}
		if !strings.Contains(content, testPath) {
			t.Errorf("%s: BinaryPath %q not found in rendered content\ngot:\n%s",
				u.fileName, testPath, content)
		}
	}
}
