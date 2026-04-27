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
	RunE: func(cmd *cobra.Command, args []string) error {
		uninstall, _ := cmd.Flags().GetBool("uninstall")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

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
			Stdout:     cmd.OutOrStdout(),
			Stderr:     cmd.ErrOrStderr(),
		}

		if err := install.Run(cmd.Context(), opts); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), "Error:", err)
			return err
		}
		return nil
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
}
