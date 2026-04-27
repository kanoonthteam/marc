package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caffeaun/marc/internal/configure/client"
	"github.com/spf13/cobra"
)

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Interactive setup wizard for marc client configuration",
	Long: `configure writes ~/.marc/config.toml (mode 0600) with your machine name,
MinIO credentials, and proxy settings.  When no flags are supplied it runs an
interactive prompt sequence.  Supply all flags to run non-interactively.

After writing the file it validates the configuration by:
  1. Resolving the MinIO endpoint hostname (DNS)
  2. Verifying the TLS certificate (or skipping when verify_tls = false)
  3. Authenticating the credentials
  4. Performing a test PUT + DELETE on a _marc-config-test/ key`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := expandHome(configFile)

		machineName, _ := cmd.Flags().GetString("machine-name")
		minioEndpoint, _ := cmd.Flags().GetString("minio-endpoint")
		accessKey, _ := cmd.Flags().GetString("minio-access-key")
		secretKey, _ := cmd.Flags().GetString("minio-secret-key")
		bucket, _ := cmd.Flags().GetString("bucket")

		// NonInteractive is true when the caller explicitly sets any value flag.
		nonInteractive := cmd.Flags().Changed("machine-name") ||
			cmd.Flags().Changed("minio-endpoint") ||
			cmd.Flags().Changed("minio-access-key") ||
			cmd.Flags().Changed("minio-secret-key") ||
			cmd.Flags().Changed("bucket")

		check, _ := cmd.Flags().GetBool("check")
		reset, _ := cmd.Flags().GetBool("reset")
		printDefault, _ := cmd.Flags().GetBool("print-default")

		opts := client.Options{
			ConfigPath:     cfgPath,
			Check:          check,
			Reset:          reset,
			PrintDefault:   printDefault,
			NonInteractive: nonInteractive,
			MachineName:    machineName,
			MinIOEndpoint:  minioEndpoint,
			AccessKey:      accessKey,
			SecretKey:      secretKey,
			Bucket:         bucket,
			// Stdin/Stdout/Stderr/NewClient left nil — Run() applies production defaults.
		}

		if err := client.Run(cmd.Context(), opts); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			return err
		}
		return nil
	},
}

// expandHome replaces a leading "~/" with the user's home directory.
func expandHome(p string) string {
	if !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[2:])
}

func init() {
	// Operational mode flags.
	configureCmd.Flags().Bool(
		"check",
		false,
		"validate the existing configuration without making changes (exit 1 on error)",
	)
	configureCmd.Flags().Bool(
		"reset",
		false,
		"wipe the existing configuration and start the interactive wizard from scratch",
	)
	configureCmd.Flags().Bool(
		"print-default",
		false,
		"print the default configuration template to stdout without writing any file",
	)

	// Non-interactive value flags (bypass prompts when any are provided).
	configureCmd.Flags().String(
		"machine-name",
		"",
		"unique name for this machine (e.g. macbook-kanoon)",
	)
	configureCmd.Flags().String(
		"minio-endpoint",
		"",
		"MinIO endpoint URL (e.g. https://artifacts.kanolab.io)",
	)
	configureCmd.Flags().String(
		"minio-access-key",
		"",
		"MinIO access key ID",
	)
	configureCmd.Flags().String(
		"minio-secret-key",
		"",
		"MinIO secret access key",
	)
	configureCmd.Flags().String(
		"bucket",
		"marc",
		"MinIO bucket name",
	)
}
