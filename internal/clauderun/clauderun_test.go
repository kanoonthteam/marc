package clauderun

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, dir, listenAddr string) string {
	t.Helper()
	path := filepath.Join(dir, "config.toml")
	body := fmt.Sprintf(`machine_name = "test"

[paths]
capture_file = "/tmp/capture.jsonl"
log_file = "/tmp/marc.log"

[proxy]
listen_addr = %q
upstream_url = "https://api.anthropic.com"

[shipper]
rotate_size_mb = 5
ship_interval_seconds = 30

[minio]
endpoint = "http://127.0.0.1:9000"
bucket = "marc"
access_key = "k"
secret_key = "s"
verify_tls = false
`, listenAddr)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// fakeClaude builds a tiny Go program that prints its ANTHROPIC_BASE_URL
// env var + its argv and exits 0. The test invokes it instead of the real
// claude binary by setting Options.ClaudeBinary to its path.
func fakeClaude(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "fake_claude.go")
	bin := filepath.Join(dir, "fake_claude")
	if err := os.WriteFile(src, []byte(`package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Printf("BASE=%s\n", os.Getenv("ANTHROPIC_BASE_URL"))
	for _, a := range os.Args[1:] {
		fmt.Printf("ARG=%s\n", a)
	}
}
`), 0o644); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", bin, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake claude: %v\n%s", err, out)
	}
	return bin
}

func TestRun_PassesArgsAndOverridesBaseURL(t *testing.T) {
	cfgPath := writeConfig(t, t.TempDir(), "127.0.0.1:18099")

	// A listener on the configured port so the dial check passes.
	ln, err := net.Listen("tcp", "127.0.0.1:18099")
	if err != nil {
		t.Skipf("port 18099 already in use: %v", err)
	}
	defer ln.Close() //nolint:errcheck

	bin := fakeClaude(t)

	var stdout, stderr bytes.Buffer
	t.Setenv("ANTHROPIC_BASE_URL", "http://wrong.example.com:1234")
	err = Run(Options{
		Args:         []string{"--continue", "--something", "else"},
		ConfigPath:   cfgPath,
		ClaudeBinary: bin,
		Stdout:       &stdout,
		Stderr:       &stderr,
		Stdin:        bytes.NewReader(nil),
	})
	if err != nil {
		t.Fatalf("Run: %v\nstderr=%s", err, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "BASE=http://127.0.0.1:18099") {
		t.Errorf("ANTHROPIC_BASE_URL not overridden in spawned process: %s", out)
	}
	if !strings.Contains(out, "ARG=--continue") {
		t.Errorf("first arg not forwarded: %s", out)
	}
	if !strings.Contains(out, "ARG=--something") || !strings.Contains(out, "ARG=else") {
		t.Errorf("subsequent args not forwarded: %s", out)
	}
}

func TestRun_ProxyDown_ErrorsWithRestartHint(t *testing.T) {
	cfgPath := writeConfig(t, t.TempDir(), "127.0.0.1:1") // port 1 = nothing listening

	var stderr bytes.Buffer
	err := Run(Options{
		Args:         []string{"--continue"},
		ConfigPath:   cfgPath,
		ClaudeBinary: "/bin/echo", // never reached
		Stderr:       &stderr,
	})
	if err == nil {
		t.Fatal("Run should have failed when proxy is unreachable")
	}
	if !strings.Contains(err.Error(), "dial proxy") {
		t.Errorf("error should mention dial failure, got: %v", err)
	}
	hint := stderr.String()
	switch runtime.GOOS {
	case "linux":
		if !strings.Contains(hint, "systemctl start marc-proxy") {
			t.Errorf("linux hint should suggest systemctl, got: %s", hint)
		}
	case "darwin":
		if !strings.Contains(hint, "launchctl kickstart") {
			t.Errorf("darwin hint should suggest launchctl, got: %s", hint)
		}
	}
}

func TestRun_ClaudeNotOnPath(t *testing.T) {
	cfgPath := writeConfig(t, t.TempDir(), "127.0.0.1:18098")
	ln, err := net.Listen("tcp", "127.0.0.1:18098")
	if err != nil {
		t.Skipf("port 18098 in use: %v", err)
	}
	defer ln.Close() //nolint:errcheck

	err = Run(Options{
		Args:         []string{"--continue"},
		ConfigPath:   cfgPath,
		ClaudeBinary: "definitely-no-such-binary-marc-test",
	})
	if err == nil {
		t.Fatal("Run should have failed when claude binary is missing")
	}
	if !strings.Contains(err.Error(), "not found on PATH") {
		t.Errorf("error should mention PATH lookup, got: %v", err)
	}
}

func TestRun_PropagatesNonZeroExit(t *testing.T) {
	cfgPath := writeConfig(t, t.TempDir(), "127.0.0.1:18097")
	ln, err := net.Listen("tcp", "127.0.0.1:18097")
	if err != nil {
		t.Skipf("port 18097 in use: %v", err)
	}
	defer ln.Close() //nolint:errcheck

	// /usr/bin/false exits 1 on every Unix.
	err = Run(Options{
		Args:         []string{},
		ConfigPath:   cfgPath,
		ClaudeBinary: "/usr/bin/false",
		Stdout:       &bytes.Buffer{},
		Stderr:       &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("Run should have surfaced the non-zero exit")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("err should be *exec.ExitError so caller can propagate code, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != 1 {
		t.Errorf("exit code = %d, want 1", exitErr.ExitCode())
	}
}

// quick non-test sanity: make sure injection points work with a custom dial
// that always succeeds, so the test can run without binding a real port.
func TestRun_DialInjection(t *testing.T) {
	cfgPath := writeConfig(t, t.TempDir(), "10.255.255.1:9999")
	bin := fakeClaude(t)

	var stdout bytes.Buffer
	err := Run(Options{
		Args:         []string{"hello"},
		ConfigPath:   cfgPath,
		ClaudeBinary: bin,
		Stdout:       &stdout,
		Stderr:       &bytes.Buffer{},
		dial: func(_, addr string, _ time.Duration) (net.Conn, error) {
			// Pretend dial succeeded by returning a closed pipe.
			c1, c2 := net.Pipe()
			_ = c2.Close()
			return c1, nil
		},
	})
	if err != nil {
		t.Fatalf("Run with mocked dial: %v", err)
	}
	if !strings.Contains(stdout.String(), "BASE=http://10.255.255.1:9999") {
		t.Errorf("dial-injection path: env var not set right: %s", stdout.String())
	}
}
