package install

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPostInstallGate_PassPrintsSuccess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	var rollbackCalled bool

	opts := Options{
		BinaryPath: "/usr/local/bin/marc",
		Stdout:     &stdout,
		Stderr:     &stderr,
		verifyHook: func(_ context.Context, _ Options) error { return nil },
	}

	err := runPostInstallGate(context.Background(), opts, func() { rollbackCalled = true })
	if err != nil {
		t.Fatalf("gate returned error on pass: %v", err)
	}
	if rollbackCalled {
		t.Error("rollback was called on success — must not happen")
	}
	if !strings.Contains(stdout.String(), "✓ Install complete") {
		t.Errorf("stdout should print success message, got: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "marc proxy is forwarding correctly") {
		t.Errorf("stdout should mention forwarding, got: %q", stdout.String())
	}
}

func TestPostInstallGate_FailRollsBackAndReturnsError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	var rollbackCalled bool

	opts := Options{
		BinaryPath: "/usr/local/bin/marc",
		Stdout:     &stdout,
		Stderr:     &stderr,
		verifyHook: func(_ context.Context, _ Options) error { return errors.New("self-test exit 1") },
	}

	err := runPostInstallGate(context.Background(), opts, func() { rollbackCalled = true })
	if err == nil {
		t.Fatal("gate should have returned an error on self-test failure")
	}
	if !rollbackCalled {
		t.Error("rollback was NOT called on failure — must be called")
	}
	if !strings.Contains(stderr.String(), "self-test failed") {
		t.Errorf("stderr should mention self-test failed, got: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Install rolled back") {
		t.Errorf("stderr should mention rollback, got: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "See output above for diagnosis") {
		t.Errorf("stderr should mention diagnosis pointer, got: %q", stderr.String())
	}
	if strings.Contains(stdout.String(), "✓ Install complete") {
		t.Errorf("stdout must NOT print success on failure, got: %q", stdout.String())
	}
}

func TestPostInstallGate_SkipSelfTestBypassesGate(t *testing.T) {
	var verifyCalled, rollbackCalled bool

	opts := Options{
		BinaryPath:   "/usr/local/bin/marc",
		Stdout:       &bytes.Buffer{},
		Stderr:       &bytes.Buffer{},
		SkipSelfTest: true,
		verifyHook:   func(_ context.Context, _ Options) error { verifyCalled = true; return nil },
	}

	if err := runPostInstallGate(context.Background(), opts, func() { rollbackCalled = true }); err != nil {
		t.Fatalf("SkipSelfTest path returned error: %v", err)
	}
	if verifyCalled {
		t.Error("SkipSelfTest=true should not invoke verifyHook")
	}
	if rollbackCalled {
		t.Error("SkipSelfTest=true should not invoke rollback")
	}
}
