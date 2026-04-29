package main

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/caffeaun/marc/internal/doctor"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run a read-only diagnostic of the marc client setup",
	Long: `doctor inspects every part of the marc setup and prints one ✓/⚠/✗
line per check.

It only reads — no daemon control, no config writes. Run this when something
feels wrong; it pinpoints which subsystem (config, systemd unit, port, MinIO,
capture file) is unhealthy.

Exit code: 0 if all checks pass-or-warn, 1 if any check failed.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		opts := doctor.Options{
			ConfigPath: expandHome(configFile),
			Stdout:     cmd.OutOrStdout(),
		}

		res := doctor.Run(ctx, opts)
		doctor.Print(cmd.OutOrStdout(), res)

		if res.ExitCode() != 0 {
			// Returning a Cobra error makes the process exit 1, but we don't
			// want it to print "Error: ..." since the report itself is the
			// message. Use os.Exit directly.
			os.Exit(1)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
