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
	Use:   "marc-server",
	Short: "Server-side daemons for the marc system",
	Long: `marc-server is the server-side binary for the marc system.
It polls MinIO for captured conversations, denoises them via Ollama,
stores results in ClickHouse, generates questions, and delivers them
via Telegram.`,
}

func init() {
	// Persistent flag available to every subcommand.
	rootCmd.PersistentFlags().StringVar(
		&configFile,
		"config",
		"/etc/marc/server.toml",
		"path to the marc server configuration file",
	)

	rootCmd.AddCommand(
		processCmd,
		generateCmd,
		botCmd,
		configureCmd,
		installCmd,
		initCmd,
		versionCmd,
	)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version string",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("marc-server version %s (commit %s)\n", version, commit)
	},
}

var processCmd = &cobra.Command{
	Use:   "process",
	Short: "Poll MinIO for raw captures, denoise via Ollama, store in ClickHouse",
	Long: `process is the server ingest daemon.
It polls MinIO every 60 seconds for new raw capture objects, denoises
each event via Ollama, and inserts the results into ClickHouse.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(os.Stderr, "marc-server process: not implemented")
		os.Exit(0)
	},
}

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate candidate questions from recent ClickHouse events",
	Long: `generate queries ClickHouse for recent decision-bearing events,
invokes claude -p to produce candidate questions, filters by quality scores,
and inserts survivors into the SQLite pending_questions table.
Intended to run hourly via systemd timer.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(os.Stderr, "marc-server generate: not implemented")
		os.Exit(0)
	},
}

var botCmd = &cobra.Command{
	Use:   "bot",
	Short: "Run the Telegram bot and question-delivery scheduler",
	Long: `bot starts the Telegram long-poll loop and a gocron scheduler
that sends the oldest ready question to the configured Telegram chat
every 30 minutes during working hours (Asia/Bangkok timezone).`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(os.Stderr, "marc-server bot: not implemented")
		os.Exit(0)
	},
}

// configureCheck and configurePrintDefault are flags for the configure subcommand.
var (
	configureCheck        bool
	configurePrintDefault bool
)

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Interactive setup wizard for /etc/marc/server.toml",
	Long: `configure runs an interactive wizard that prompts for MinIO,
ClickHouse, Ollama, Telegram, and scheduler settings, writes
/etc/marc/server.toml with mode 0600, and validates all four services
via their Ping() methods.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(os.Stderr, "marc-server configure: not implemented")
		os.Exit(0)
	},
}

func init() {
	configureCmd.Flags().BoolVar(
		&configureCheck,
		"check",
		false,
		"validate existing /etc/marc/server.toml without writing; exit 1 on invalid",
	)
	configureCmd.Flags().BoolVar(
		&configurePrintDefault,
		"print-default",
		false,
		"print the default server TOML template to stdout without writing any file",
	)
}

// installUninstall and installDryRun are flags for the install subcommand.
var (
	installUninstall bool
	installDryRun    bool
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install marc-server systemd service units (Linux only)",
	Long: `install writes marc-process.service, marc-bot.service,
marc-generate.service, and marc-generate.timer to /etc/systemd/system/,
runs daemon-reload, then enables and starts each unit.
Requires root. Linux only.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(os.Stderr, "marc-server install: not implemented")
		os.Exit(0)
	},
}

func init() {
	installCmd.Flags().BoolVar(
		&installUninstall,
		"uninstall",
		false,
		"stop and remove all installed marc-server systemd units",
	)
	installCmd.Flags().BoolVar(
		&installDryRun,
		"dry-run",
		false,
		"print unit file contents without writing or starting anything",
	)
}

// initCheck is the flag for the init subcommand.
var initCheck bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize ClickHouse schema and SQLite state database",
	Long: `init applies the ClickHouse DDL (CREATE DATABASE marc, CREATE TABLE
marc.events) and creates the SQLite state.db with all required tables,
pragmas, and seed rows. Safe to re-run — all statements use IF NOT EXISTS.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(os.Stderr, "marc-server init: not implemented")
		os.Exit(0)
	},
}

func init() {
	initCmd.Flags().BoolVar(
		&initCheck,
		"check",
		false,
		"compare current schema against expected; exit 1 with drift description on mismatch",
	)
}
