package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/caffeaun/marc/internal/install"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install marc-proxy and marc-ship as system services",
	Long: `install generates platform-appropriate service unit files for marc proxy
and marc ship, then loads and starts them.

On Linux it writes systemd units to /etc/systemd/system/ and runs
  systemctl daemon-reload && systemctl enable --now.

On macOS it writes launchd plists to ~/Library/LaunchAgents/ and runs
  launchctl load -w.

The command is idempotent: re-running it on an already-installed system
restarts the services without error.

Examples:
  marc install              # install and start services
  marc install --dry-run    # print unit file contents without writing
  marc install --uninstall  # stop and remove all installed units`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		uninstall, _ := cmd.Flags().GetBool("uninstall")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		targetDir, _ := cmd.Flags().GetString("target-dir")
		userMode, _ := cmd.Flags().GetBool("user")
		skipLoad, _ := cmd.Flags().GetBool("skip-load")

		// --user is a convenience: maps to ~/.config/systemd/user/ and skips
		// the systemctl invocation by default (the operator runs systemctl
		// --user enable --now afterwards). Override with --target-dir.
		if userMode && targetDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("install --user: resolve home dir: %w", err)
			}
			targetDir = filepath.Join(home, ".config", "systemd", "user")
			if !cmd.Flags().Changed("skip-load") {
				skipLoad = true
			}
		}

		// Resolve the binary path: real path of the running executable.
		binaryPath, err := os.Executable()
		if err == nil {
			if resolved, err2 := filepath.EvalSymlinks(binaryPath); err2 == nil {
				binaryPath = resolved
			}
		}

		cfgPath := expandHome(configFile)

		opts := install.Options{
			BinaryPath: binaryPath,
			ConfigPath: cfgPath,
			Uninstall:  uninstall,
			DryRun:     dryRun,
			TargetDir:  targetDir,
			SkipLoad:   skipLoad,
			Stdout:     cmd.OutOrStdout(),
			Stderr:     cmd.ErrOrStderr(),
		}

		return install.Run(cmd.Context(), opts)
	},
}

func init() {
	installCmd.Flags().Bool(
		"uninstall",
		false,
		"stop and remove all installed marc service units",
	)
	installCmd.Flags().Bool(
		"dry-run",
		false,
		"print the unit file contents without writing or starting anything",
	)
	installCmd.Flags().String(
		"target-dir",
		"",
		"override the systemd unit directory (default: /etc/systemd/system on Linux)",
	)
	installCmd.Flags().Bool(
		"user",
		false,
		"install under ~/.config/systemd/user/ (no sudo); operator runs `systemctl --user enable --now marc-proxy.service marc-ship.service` afterwards",
	)
	installCmd.Flags().Bool(
		"skip-load",
		false,
		"write unit files but do not run systemctl daemon-reload/enable/start (used by --user, where the dbus path may differ)",
	)
}
