package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version and commit are set at build time via
// -ldflags="-X main.version=<value> -X main.commit=<value>".
var (
	version = "dev"
	commit  = "none"
)

// configFile holds the value of the persistent --config flag.
var configFile string

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "marc",
	Short: "Capture and ship Claude Code sessions",
	Long: `marc is the client-side binary for the marc system.
It proxies Claude Code API calls, captures conversations, and ships
them to MinIO for server-side processing.`,
}

func init() {
	// Persistent flag available to every subcommand.
	rootCmd.PersistentFlags().StringVar(
		&configFile,
		"config",
		"~/.marc/config.toml",
		"path to the marc client configuration file",
	)

	rootCmd.AddCommand(
		proxyCmd,
		shipCmd,
		configureCmd,
		installCmd,
		versionCmd,
	)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version string",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("marc version %s (commit %s)\n", version, commit)
	},
}
