package clauderun

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/caffeaun/marc/internal/config"
)

// guardSettings is the resolved (defaults-applied) crash-loop configuration.
type guardSettings struct {
	Enabled     bool
	Threshold   int
	Mode        string // warn | backoff | prompt | fresh | block
	BackoffBase time.Duration
	BackoffCap  time.Duration
	BackoffJit  float64
}

// resolveGuard applies built-in defaults to a config.ClientGuard:
// threshold 2, mode "warn", backoff base 5s / cap 120s / jitter 0.5. A
// negative threshold disables the guard entirely.
func resolveGuard(g config.ClientGuard) guardSettings {
	s := guardSettings{
		Enabled:     true,
		Threshold:   g.CrashLoopThreshold,
		Mode:        strings.ToLower(strings.TrimSpace(g.OnCrashLoop)),
		BackoffBase: time.Duration(g.BackoffBaseSeconds * float64(time.Second)),
		BackoffCap:  time.Duration(g.BackoffCapSeconds * float64(time.Second)),
		BackoffJit:  g.BackoffJitter,
	}
	if s.Threshold < 0 {
		s.Enabled = false
	}
	if s.Threshold == 0 {
		s.Threshold = 2
	}
	switch s.Mode {
	case "warn", "backoff", "prompt", "fresh", "block":
	default:
		s.Mode = "warn"
	}
	if s.BackoffBase <= 0 {
		s.BackoffBase = 5 * time.Second
	}
	if s.BackoffCap <= 0 {
		s.BackoffCap = 120 * time.Second
	}
	if s.BackoffJit <= 0 {
		s.BackoffJit = 0.5
	}
	return s
}

// wantsResume reports whether the claude args request resuming a prior session
// (--continue/-c or --resume[=id]).
func wantsResume(args []string) bool {
	for _, a := range args {
		if a == "--continue" || a == "-c" || a == "--resume" ||
			strings.HasPrefix(a, "--resume=") {
			return true
		}
	}
	return false
}

// stripResumeFlags removes resume flags (and a --resume session-id value) so a
// fresh session is started instead.
func stripResumeFlags(args []string) []string {
	out := make([]string, 0, len(args))
	skipNext := false
	for i := 0; i < len(args); i++ {
		if skipNext {
			skipNext = false
			continue
		}
		a := args[i]
		switch {
		case a == "--continue" || a == "-c":
			continue
		case a == "--resume":
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				skipNext = true // drop the trailing session-id token too
			}
			continue
		case strings.HasPrefix(a, "--resume="):
			continue
		}
		out = append(out, a)
	}
	return out
}

// parseForceContinue extracts the marc-level --force-continue flag (which is
// NOT forwarded to claude) and returns the remaining args.
func parseForceContinue(args []string) (force bool, rest []string) {
	rest = make([]string, 0, len(args))
	for _, a := range args {
		if a == "--force-continue" {
			force = true
			continue
		}
		rest = append(rest, a)
	}
	return force, rest
}

// isTTY reports whether r is an interactive terminal (for prompt mode).
func isTTY(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// guardResume applies the crash-loop guard before a resume launch. It returns
// (proceed, claudeArgs); when proceed is false the caller must NOT launch
// claude. claudeArgs may be rewritten (e.g. "fresh" strips the resume flags).
func guardResume(s guardSettings, cwd string, claudeArgs []string, stdin io.Reader, stderr io.Writer) (bool, []string) {
	if !s.Enabled {
		return true, claudeArgs
	}
	streak := abnormalStreak(cwd)
	if streak < s.Threshold {
		return true, claudeArgs
	}

	lastReason := "abnormal exit"
	if recs := recentForCWD(cwd); len(recs) > 0 {
		lastReason = recs[len(recs)-1].Reason
	}
	banner := fmt.Sprintf(
		"marc: crash-loop guard — the last %d session(s) in %s ended abnormally (most recent: %s).\n"+
			"      Resuming with --continue will likely re-trigger the same failure.",
		streak, cwd, lastReason)

	switch s.Mode {
	case "backoff":
		d := jitteredBackoff(streak, s.BackoffBase, s.BackoffCap, s.BackoffJit)
		fmt.Fprintf(stderr, "%s\n      Cooling down %s before resuming (Ctrl-C to stop; --force-continue to skip).\n",
			banner, d.Round(time.Second))
		time.Sleep(d)
		return true, claudeArgs
	case "prompt":
		fmt.Fprintln(stderr, banner)
		if !isTTY(stdin) {
			fmt.Fprintln(stderr, "      Non-interactive input; not resuming. Use --force-continue to override.")
			return false, claudeArgs
		}
		fmt.Fprint(stderr, "      Resume anyway? [y/N]: ")
		ans, _ := bufio.NewReader(stdin).ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(ans)) {
		case "y", "yes":
			return true, claudeArgs
		}
		fmt.Fprintln(stderr, "      Aborted (start a fresh session, or --force-continue to resume).")
		return false, claudeArgs
	case "fresh":
		fmt.Fprintf(stderr, "%s\n      Starting a FRESH session instead (dropping resume flags). Use --force-continue to resume.\n", banner)
		return true, stripResumeFlags(claudeArgs)
	case "block":
		fmt.Fprintf(stderr, "%s\n      Blocked. Fix the cause, then re-run with --force-continue to resume.\n", banner)
		return false, claudeArgs
	default: // warn
		fmt.Fprintf(stderr, "%s\n      Proceeding anyway (on_crash_loop=warn).\n", banner)
		return true, claudeArgs
	}
}
