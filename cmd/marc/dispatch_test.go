package main

import "testing"

func TestIsClaudePassthrough(t *testing.T) {
	cases := []struct {
		args []string
		want bool
		why  string
	}{
		// Empty args → cobra shows root help.
		{[]string{}, false, "no args"},

		// Marc subcommands.
		{[]string{"proxy"}, false, "marc subcommand"},
		{[]string{"proxy", "--self-test"}, false, "marc subcommand with flag"},
		{[]string{"ship"}, false, "marc subcommand"},
		{[]string{"configure"}, false, "marc subcommand"},
		{[]string{"install", "--user"}, false, "marc subcommand with flag"},
		{[]string{"doctor"}, false, "marc subcommand"},
		{[]string{"version"}, false, "marc subcommand"},
		{[]string{"update", "--check"}, false, "marc subcommand with flag"},
		{[]string{"help"}, false, "cobra builtin"},
		{[]string{"completion", "zsh"}, false, "cobra builtin"},

		// Marc-level help/version flags.
		{[]string{"--help"}, false, "marc help flag"},
		{[]string{"-h"}, false, "marc help flag"},
		{[]string{"--version"}, false, "marc version flag"},

		// Marc's persistent --config flag.
		{[]string{"--config", "/x/config.toml", "doctor"}, false, "marc persistent flag"},

		// Claude-only flags should pass through.
		{[]string{"--continue"}, true, "claude --continue"},
		{[]string{"--resume"}, true, "claude --resume"},
		{[]string{"-p", "say hi"}, true, "claude -p"},
		{[]string{"--model", "haiku-4-5"}, true, "claude --model"},
		{[]string{"--print"}, true, "claude --print"},

		// Bare prompt (claude is given a positional message; rare but handled).
		{[]string{"hello"}, true, "claude positional"},
	}

	for _, tc := range cases {
		got := isClaudePassthrough(tc.args)
		if got != tc.want {
			t.Errorf("isClaudePassthrough(%v) = %v, want %v (%s)", tc.args, got, tc.want, tc.why)
		}
	}
}
