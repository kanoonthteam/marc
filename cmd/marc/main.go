package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/caffeaun/marc/internal/clauderun"
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
	// Pre-dispatch: when args[1] isn't a recognized marc subcommand or
	// help/version flag, treat the whole tail as `claude` arguments and
	// exec claude with ANTHROPIC_BASE_URL pointed at the proxy. This lets
	// `marc --continue` mean "open a captured Claude session" without
	// requiring a permanently-exported env var in ~/.zshrc.
	if isClaudePassthrough(os.Args[1:]) {
		err := clauderun.Run(clauderun.Options{Args: os.Args[1:]})
		if err == nil {
			return
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// isClaudePassthrough returns true when args should be forwarded to the
// claude CLI rather than handled as marc subcommands. Anything matching a
// marc command name or marc-level help/version flag is consumed by cobra;
// every other first token (notably `--continue`, `--resume`, `-p`) is
// claude's.
func isClaudePassthrough(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "proxy", "ship", "configure", "install", "doctor", "version", "update",
		"help", "completion",
		"--help", "-h", "--version":
		return false
	}
	// Marc's persistent --config flag with a value: `marc --config /x doctor`.
	// We let cobra handle it.
	if args[0] == "--config" {
		return false
	}
	return true
}

var rootCmd = &cobra.Command{
	Use:   "marc",
	Short: "Capture and ship Claude Code sessions",
	Long: `marc is the client-side binary for the marc system.
It proxies Claude Code API calls, captures conversations, and ships
them to MinIO for server-side processing.

Running marc with arguments that aren't a marc subcommand spawns
` + "`claude`" + ` with ANTHROPIC_BASE_URL pointed at the proxy, so
` + "`marc --continue`" + ` opens a captured Claude Code session
without requiring a permanently-exported env var. Use plain
` + "`claude`" + ` (no marc) when you don't want the session captured.`,
}

func init() {
	// Set Version so `marc --version` prints the binary version (cobra
	// auto-wires a --version flag when Version is set). Without this,
	// our isClaudePassthrough whitelisting --version would still fail
	// at the cobra layer.
	rootCmd.Version = fmt.Sprintf("%s (commit %s)", version, commit)
	rootCmd.SetVersionTemplate("marc version {{.Version}}\n")

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
