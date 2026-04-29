package main

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/caffeaun/marc/internal/update"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Replace this marc binary with the latest published release",
	Long: `update queries https://github.com/kanoonthteam/marc/releases/latest, compares
the published tag to this binary's compile-time version, downloads the
asset matching $GOOS/$GOARCH if a newer one exists, verifies its
SHA-256 against the release's checksums.txt, and atomically replaces
this binary on disk.

If the destination directory is not writable (typical /usr/local/bin
case), the verified binary is staged to a temp path and the command
prints a one-liner you can run with sudo to install it.

After replacing, restart the daemons so they pick up the new binary
(the command prints the platform-appropriate restart commands).`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		check, _ := cmd.Flags().GetBool("check")

		res, err := update.Run(ctx, update.Options{
			CurrentVersion: version,
			CheckOnly:      check,
			Stdout:         cmd.OutOrStdout(),
		})
		if err != nil {
			return err
		}
		if check && !res.UpToDate {
			// Make `marc update --check` exit 1 when an update is available so
			// scripts can detect "needs update" without parsing output.
			os.Exit(1)
		}
		return nil
	},
}

func init() {
	updateCmd.Flags().Bool("check", false, "report whether an update is available; do not download or replace (exit 1 if behind)")
	rootCmd.AddCommand(updateCmd)
}