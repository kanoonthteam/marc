//go:build unit

package client_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caffeaun/marc/internal/config"
	"github.com/caffeaun/marc/internal/configure/client"
	"github.com/caffeaun/marc/internal/minioclient"
)

// fakePingOK returns a Fake whose Ping always succeeds.
func fakePingOK(_ minioclient.Config) (minioclient.Client, error) {
	return minioclient.NewFake(), nil
}

// fakePingErr returns a Fake whose Ping returns the given error.
func fakePingErr(err error) func(minioclient.Config) (minioclient.Client, error) {
	return func(_ minioclient.Config) (minioclient.Client, error) {
		f := minioclient.NewFake()
		f.PingErr = err
		return f, nil
	}
}

// tmpConfigPath returns a path inside a fresh temp directory.
func tmpConfigPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, ".marc", "config.toml")
}

// allFlagsOpts builds a fully non-interactive Options pointing at a fake MinIO.
// The Stdin, Stdout, Stderr and NewClient fields are set for test use.
// We use http://127.0.0.1:9999 so that:
//   - DNS resolves (loopback always resolves)
//   - TLS step is skipped (http:// scheme)
//   - Auth and Bucket are fully controlled by the fake newClient
func allFlagsOpts(cfgPath string, newClient func(minioclient.Config) (minioclient.Client, error)) client.Options {
	return client.Options{
		ConfigPath:     cfgPath,
		NonInteractive: true,
		MachineName:    "test-machine",
		MinIOEndpoint:  "http://127.0.0.1:9999",
		AccessKey:      "TESTKEY",
		SecretKey:      "TESTSECRET",
		Bucket:         "marc",
		NewClient:      newClient,
		Stdin:          strings.NewReader(""),
		Stdout:         io.Discard,
		Stderr:         io.Discard,
	}
}

// TestPrintDefault checks that --print-default writes valid TOML parseable by LoadClient.
func TestPrintDefault(t *testing.T) {
	var out bytes.Buffer
	opts := client.Options{
		PrintDefault: true,
		Stdout:       &out,
		Stderr:       io.Discard,
		Stdin:        strings.NewReader(""),
	}
	if err := client.Run(context.Background(), opts); err != nil {
		t.Fatalf("Run(PrintDefault): %v", err)
	}

	// The output must be non-empty TOML.
	tomlStr := out.String()
	if !strings.Contains(tomlStr, "machine_name") {
		t.Fatalf("expected machine_name in output, got:\n%s", tomlStr)
	}
	if !strings.Contains(tomlStr, "minio") {
		t.Fatalf("expected [minio] section in output, got:\n%s", tomlStr)
	}

	// Write to a temp file to verify it is parseable by LoadClient.
	tmp, err := os.CreateTemp("", "print-default-*.toml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(tomlStr); err != nil {
		t.Fatal(err)
	}
	tmp.Close()

	// Must set mode 0600 before LoadClient will accept it.
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		t.Fatal(err)
	}

	// LoadClient validates required fields; placeholder values like "AKIA..." are
	// non-empty so validation will pass.
	if _, err := config.LoadClient(tmp.Name()); err != nil {
		t.Fatalf("LoadClient on PrintDefault output: %v", err)
	}
}

// TestNonInteractiveWriteAndValidate ensures a fully-flagged call writes 0600 file
// and all four validation steps pass when the fake Ping returns nil.
func TestNonInteractiveWriteAndValidate(t *testing.T) {
	cfgPath := tmpConfigPath(t)
	opts := allFlagsOpts(cfgPath, fakePingOK)

	var out bytes.Buffer
	opts.Stdout = &out

	if err := client.Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// File must exist.
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("config file not created: %v", err)
	}
	// Mode 0600.
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected mode 0600 got %04o", info.Mode().Perm())
	}

	// All four steps must show PASS.
	output := out.String()
	for _, step := range []string{"DNS", "TLS", "Auth", "Bucket"} {
		if !strings.Contains(output, "[PASS] "+step) {
			t.Errorf("expected [PASS] %s in output; got:\n%s", step, output)
		}
	}
}

// TestValidationFailureDoesNotWrite verifies the file is not written when Ping
// returns ErrAuthFailed.
func TestValidationFailureDoesNotWrite(t *testing.T) {
	cfgPath := tmpConfigPath(t)
	opts := allFlagsOpts(cfgPath, fakePingErr(minioclient.ErrAuthFailed))

	var errOut bytes.Buffer
	opts.Stdout = &errOut

	err := client.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error when auth fails, got nil")
	}
	if !strings.Contains(err.Error(), "authentication failed") && !strings.Contains(errOut.String(), "authentication failed") {
		// Check the stdout output as well since printAndCheckResults writes there.
		combined := err.Error() + errOut.String()
		if !strings.Contains(combined, "Auth") {
			t.Errorf("expected 'authentication failed' or 'Auth' in output; err=%v out=%s", err, errOut.String())
		}
	}

	// File must NOT have been created.
	if _, statErr := os.Stat(cfgPath); statErr == nil {
		t.Errorf("config file should not exist after auth failure")
	}
}

// TestCheckMode writes a valid config, then checks it with --check.
func TestCheckMode(t *testing.T) {
	cfgPath := tmpConfigPath(t)

	// Write the file first via non-interactive path.
	opts := allFlagsOpts(cfgPath, fakePingOK)
	opts.Stdout = io.Discard
	if err := client.Run(context.Background(), opts); err != nil {
		t.Fatalf("setup: write config: %v", err)
	}

	// --check with good config must return nil.
	checkOpts := client.Options{
		ConfigPath: cfgPath,
		Check:      true,
		NewClient:  fakePingOK,
		Stdin:      strings.NewReader(""),
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	}
	if err := client.Run(context.Background(), checkOpts); err != nil {
		t.Fatalf("--check on valid config: %v", err)
	}

	// Corrupt the endpoint to break DNS.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	// Replace the loopback address with a hostname that cannot resolve.
	broken := strings.ReplaceAll(string(data), "127.0.0.1:9999", "this-host-does-not-exist.invalid")
	if err := os.WriteFile(cfgPath, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}

	// --check with broken config must return non-nil.
	checkOpts.NewClient = fakePingOK // DNS will fail before NewClient is called
	if err := client.Run(context.Background(), checkOpts); err == nil {
		t.Fatal("expected error for bad endpoint, got nil")
	}
}

// TestModeMismatch checks that --check refuses a file with permissions != 0600.
func TestModeMismatch(t *testing.T) {
	cfgPath := tmpConfigPath(t)

	// Write the file via non-interactive path.
	opts := allFlagsOpts(cfgPath, fakePingOK)
	opts.Stdout = io.Discard
	if err := client.Run(context.Background(), opts); err != nil {
		t.Fatalf("setup: write config: %v", err)
	}

	// Widen the permissions.
	if err := os.Chmod(cfgPath, 0o644); err != nil {
		t.Fatal(err)
	}

	checkOpts := client.Options{
		ConfigPath: cfgPath,
		Check:      true,
		NewClient:  fakePingOK,
		Stdin:      strings.NewReader(""),
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	}
	err := client.Run(context.Background(), checkOpts)
	if err == nil {
		t.Fatal("expected error for mode 0644, got nil")
	}
	if !strings.Contains(err.Error(), "0600") {
		t.Errorf("expected '0600' in error message, got: %v", err)
	}
}

// TestResetMode writes an initial config, then resets with stdin feeding "y\n"
// plus non-interactive flags via a new machine name.
func TestResetMode(t *testing.T) {
	cfgPath := tmpConfigPath(t)

	// Write initial config.
	opts := allFlagsOpts(cfgPath, fakePingOK)
	opts.Stdout = io.Discard
	if err := client.Run(context.Background(), opts); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Reset: confirm with "y" then feed new non-interactive flags.
	// Because Reset mode still runs runInteractive, we feed all prompt answers
	// through stdin: y\n then all 12 field values.
	// Use http://127.0.0.1:9999 so DNS resolves and TLS is skipped.
	stdinData := strings.Join([]string{
		"y",                         // confirm overwrite
		"new-machine",               // machine_name
		"http://127.0.0.1:9999",    // minio.endpoint
		"NEWKEY",                    // minio.access_key
		"NEWSECRET",                 // minio.secret_key
		"newbucket",                 // minio.bucket
		"~/.marc/capture.jsonl",     // paths.capture_file
		"~/.marc/marc.log",          // paths.log_file
		"127.0.0.1:8082",            // proxy.listen_addr
		"https://api.anthropic.com", // proxy.upstream_url
		"5",                         // shipper.rotate_size_mb
		"30",                        // shipper.ship_interval_seconds
		"false",                     // minio.verify_tls (http endpoint)
		"",                          // EOF sentinel
	}, "\n")

	var out bytes.Buffer
	resetOpts := client.Options{
		ConfigPath: cfgPath,
		Reset:      true,
		NewClient:  fakePingOK,
		Stdin:      strings.NewReader(stdinData),
		Stdout:     &out,
		Stderr:     io.Discard,
	}

	if err := client.Run(context.Background(), resetOpts); err != nil {
		t.Fatalf("Reset: %v\noutput: %s", err, out.String())
	}

	// Verify the config now has the new machine name.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "new-machine") {
		t.Errorf("expected new-machine in config; got:\n%s", string(data))
	}
}

// TestInteractiveStdinSimulation feeds all prompts via Stdin and asserts the result.
// Uses http://127.0.0.1:9999 as the endpoint so DNS resolves and TLS is skipped.
func TestInteractiveStdinSimulation(t *testing.T) {
	cfgPath := tmpConfigPath(t)

	stdinData := strings.Join([]string{
		"my-laptop",                 // machine_name
		"http://127.0.0.1:9999",    // minio.endpoint (loopback so DNS resolves, http so TLS skipped)
		"ACCESSKEY",                 // minio.access_key
		"SECRETKEY",                 // minio.secret_key
		"testbucket",                // minio.bucket
		"~/.marc/capture.jsonl",     // paths.capture_file
		"~/.marc/marc.log",          // paths.log_file
		"127.0.0.1:8082",            // proxy.listen_addr
		"https://api.anthropic.com", // proxy.upstream_url
		"10",                        // shipper.rotate_size_mb
		"60",                        // shipper.ship_interval_seconds
		"false",                     // minio.verify_tls (http endpoint)
		"",
	}, "\n")

	var out bytes.Buffer
	opts := client.Options{
		ConfigPath: cfgPath,
		NewClient:  fakePingOK,
		Stdin:      strings.NewReader(stdinData),
		Stdout:     &out,
		Stderr:     io.Discard,
	}

	if err := client.Run(context.Background(), opts); err != nil {
		t.Fatalf("interactive run: %v\noutput: %s", err, out.String())
	}

	// Read back and verify key fields.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	for _, want := range []string{"my-laptop", "127.0.0.1:9999", "ACCESSKEY", "testbucket"} {
		if !strings.Contains(content, want) {
			t.Errorf("expected %q in config; got:\n%s", want, content)
		}
	}
	if !strings.Contains(content, "rotate_size_mb = 10") {
		t.Errorf("expected rotate_size_mb = 10; got:\n%s", content)
	}
}

// TestValidationStepsDNS exercises the DNS failure path directly.
func TestValidationStepsDNS(t *testing.T) {
	ctx := context.Background()
	cfg := minioclient.Config{
		Endpoint:  "https://this-host-will-never-resolve.invalid",
		Bucket:    "marc",
		AccessKey: "k",
		SecretKey: "s",
		VerifyTLS: true,
	}
	results := client.ValidateMinIO(ctx, cfg, fakePingOK)
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}
	if results[0].Passed {
		t.Error("expected DNS step to fail for .invalid domain")
	}
	if !strings.Contains(results[0].Message, "DNS resolution failed") {
		t.Errorf("unexpected DNS message: %s", results[0].Message)
	}
	// Remaining steps should be skipped.
	for _, r := range results[1:] {
		if r.Passed {
			t.Errorf("step %s should be skipped/failed after DNS failure", r.Step)
		}
	}
}

// TestValidationBucketNotFound checks that ErrBucketNotFound maps correctly.
func TestValidationBucketNotFound(t *testing.T) {
	ctx := context.Background()

	// Use a valid-looking endpoint so DNS/TLS steps pass (they are mocked via newClient).
	// We short-circuit DNS by using localhost which always resolves.
	cfg := minioclient.Config{
		Endpoint:  "http://127.0.0.1:9999",
		Bucket:    "no-such-bucket",
		AccessKey: "k",
		SecretKey: "s",
		VerifyTLS: false,
	}

	newClient := fakePingErr(fmt.Errorf("wrapped: %w", minioclient.ErrBucketNotFound))

	results := client.ValidateMinIO(ctx, cfg, newClient)
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	// DNS must pass (127.0.0.1 always resolves).
	if !results[0].Passed {
		t.Errorf("DNS should pass for 127.0.0.1; got: %s", results[0].Message)
	}
	// Auth should pass (bucket not found means server responded, credentials OK).
	if !results[2].Passed {
		t.Errorf("Auth should pass for ErrBucketNotFound; got: %s", results[2].Message)
	}
	// Bucket step should fail.
	if results[3].Passed {
		t.Errorf("Bucket should fail for ErrBucketNotFound; got: %s", results[3].Message)
	}
	if !strings.Contains(results[3].Message, "not found") {
		t.Errorf("expected 'not found' in bucket message; got: %s", results[3].Message)
	}
}
