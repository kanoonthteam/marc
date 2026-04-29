package install

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// runSelfTestSubprocess invokes `<binary> proxy --self-test` and forwards its
// stdout/stderr to opts.Stdout/Stderr. Returns nil iff the subprocess exits 0.
//
// We shell out (rather than calling the selftest package directly) so the
// install gate exercises the exact same code path the operator would run
// later if they re-ran the self-test by hand. If the binary is broken, this
// is the moment to learn it.
func runSelfTestSubprocess(ctx context.Context, opts Options) error {
	if opts.BinaryPath == "" {
		return errors.New("install: cannot run self-test, binary path is empty")
	}
	cmd := exec.CommandContext(ctx, opts.BinaryPath, "proxy", "--self-test")
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	// Give the daemons a moment to settle. systemd reports ActiveState=active
	// before the binary has finished its own startup.
	select {
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
		return ctx.Err()
	}
	return cmd.Run()
}

// rollbackServices stops the proxy and ship units. Used when the post-install
// self-test fails. Errors are logged and ignored — once we're rolling back
// we want to leave the machine in the cleanest possible state, not bail out
// halfway through.
func rollbackServicesSystemd(ctx context.Context, opts Options) {
	for _, u := range systemdUnits {
		if err := runCmd(ctx, "systemctl", "stop", u.fileName); err != nil {
			fmt.Fprintf(opts.Stderr, "rollback: systemctl stop %s failed: %v\n", u.fileName, err)
		}
	}
}

// runPostInstallGate is the platform-agnostic step at the tail of every
// successful install: invoke the self-test, and on failure call rollback and
// fail the command. opts.SkipSelfTest skips the gate entirely (used by
// --user mode and tests that don't exercise the gate).
func runPostInstallGate(ctx context.Context, opts Options, rollback func()) error {
	if opts.SkipSelfTest {
		return nil
	}
	verify := opts.verifyHook
	if verify == nil {
		verify = runSelfTestSubprocess
	}
	fmt.Fprintln(opts.Stdout, "\nRunning marc proxy --self-test ...")
	if err := verify(ctx, opts); err != nil {
		fmt.Fprintf(opts.Stderr, "\nself-test failed: %v\n", err)
		if rollback != nil {
			rollback()
		}
		fmt.Fprintln(opts.Stderr,
			"\nInstall rolled back. The proxy is not forwarding requests correctly. "+
				"See output above for diagnosis.")
		return fmt.Errorf("install: self-test failed")
	}
	fmt.Fprintln(opts.Stdout, "\n✓ Install complete. marc proxy is forwarding correctly.")
	return nil
}
