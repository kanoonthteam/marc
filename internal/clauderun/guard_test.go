package clauderun

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/caffeaun/marc/internal/config"
)

func mustAppend(t *testing.T, rec RunRecord) {
	t.Helper()
	if err := appendRun(rec); err != nil {
		t.Fatalf("appendRun: %v", err)
	}
}

func TestResolveGuardDefaults(t *testing.T) {
	s := resolveGuard(config.ClientGuard{})
	if !s.Enabled || s.Threshold != 2 || s.Mode != "warn" {
		t.Fatalf("defaults wrong: %+v", s)
	}
	if s.BackoffBase != 5*time.Second || s.BackoffCap != 120*time.Second || s.BackoffJit != 0.5 {
		t.Fatalf("backoff defaults wrong: %+v", s)
	}
	if resolveGuard(config.ClientGuard{CrashLoopThreshold: -1}).Enabled {
		t.Error("negative threshold should disable the guard")
	}
	if m := resolveGuard(config.ClientGuard{OnCrashLoop: "bogus"}).Mode; m != "warn" {
		t.Errorf("bogus mode should fall back to warn, got %q", m)
	}
	if th := resolveGuard(config.ClientGuard{CrashLoopThreshold: 3}).Threshold; th != 3 {
		t.Errorf("explicit threshold not honored, got %d", th)
	}
}

func TestWantsResume(t *testing.T) {
	yes := [][]string{{"--continue"}, {"-c"}, {"--resume"}, {"--resume=abc"}, {"-p", "--continue"}}
	for _, a := range yes {
		if !wantsResume(a) {
			t.Errorf("wantsResume(%v) = false, want true", a)
		}
	}
	no := [][]string{{}, {"-p"}, {"--print"}, {"--model", "x"}}
	for _, a := range no {
		if wantsResume(a) {
			t.Errorf("wantsResume(%v) = true, want false", a)
		}
	}
}

func TestStripResumeFlags(t *testing.T) {
	cases := []struct{ in, want []string }{
		{[]string{"-p", "--continue", "--model", "x"}, []string{"-p", "--model", "x"}},
		{[]string{"--resume", "sess123", "--model", "x"}, []string{"--model", "x"}},
		{[]string{"--resume=sess", "-c"}, []string{}},
		{[]string{"-p"}, []string{"-p"}},
	}
	for _, c := range cases {
		got := stripResumeFlags(c.in)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("stripResumeFlags(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseForceContinue(t *testing.T) {
	f, rest := parseForceContinue([]string{"--force-continue", "--continue"})
	if !f || strings.Join(rest, ",") != "--continue" {
		t.Errorf("got force=%v rest=%v", f, rest)
	}
	if f, _ := parseForceContinue([]string{"--continue"}); f {
		t.Error("force should be false when flag absent")
	}
}

func TestAbnormalStreak(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := "/work/proj"
	mustAppend(t, RunRecord{CWD: cwd, Reason: reasonOK})
	mustAppend(t, RunRecord{CWD: cwd, Reason: reasonOOMKilled})
	mustAppend(t, RunRecord{CWD: cwd, Reason: reasonOOMKilled})
	if got := abnormalStreak(cwd); got != 2 {
		t.Errorf("streak = %d, want 2", got)
	}
	if got := abnormalStreak("/other"); got != 0 {
		t.Errorf("other-cwd streak = %d, want 0", got)
	}
	mustAppend(t, RunRecord{CWD: cwd, Reason: reasonOK}) // clean run resets
	if got := abnormalStreak(cwd); got != 0 {
		t.Errorf("post-clean streak = %d, want 0", got)
	}
}

func seedCrashes(t *testing.T, cwd string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		mustAppend(t, RunRecord{CWD: cwd, Reason: reasonOOMKilled})
	}
}

func TestGuardResumeBelowThresholdSilentProceed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := "/w"
	seedCrashes(t, cwd, 1) // 1 < threshold 2
	var buf bytes.Buffer
	proceed, _ := guardResume(resolveGuard(config.ClientGuard{}), cwd, []string{"--continue"}, strings.NewReader(""), &buf)
	if !proceed {
		t.Fatal("below threshold should proceed")
	}
	if buf.Len() != 0 {
		t.Errorf("should be silent below threshold, got %q", buf.String())
	}
}

func TestGuardResumeWarnProceedsWithBanner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := "/w"
	seedCrashes(t, cwd, 2)
	var buf bytes.Buffer
	proceed, _ := guardResume(resolveGuard(config.ClientGuard{}), cwd, []string{"--continue"}, strings.NewReader(""), &buf)
	if !proceed {
		t.Fatal("warn mode should proceed")
	}
	if !strings.Contains(buf.String(), "crash-loop guard") {
		t.Errorf("expected banner, got %q", buf.String())
	}
}

func TestGuardResumeBlockAborts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := "/w"
	seedCrashes(t, cwd, 2)
	var buf bytes.Buffer
	proceed, _ := guardResume(resolveGuard(config.ClientGuard{OnCrashLoop: "block"}), cwd, []string{"--continue"}, strings.NewReader(""), &buf)
	if proceed {
		t.Fatal("block mode should NOT proceed")
	}
}

func TestGuardResumeFreshStripsResume(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := "/w"
	seedCrashes(t, cwd, 2)
	var buf bytes.Buffer
	proceed, args := guardResume(resolveGuard(config.ClientGuard{OnCrashLoop: "fresh"}), cwd, []string{"--continue", "-p"}, strings.NewReader(""), &buf)
	if !proceed {
		t.Fatal("fresh mode should proceed")
	}
	if wantsResume(args) {
		t.Errorf("fresh mode must strip resume flags, got %v", args)
	}
}

func TestGuardResumePromptNonTTYAborts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := "/w"
	seedCrashes(t, cwd, 2)
	var buf bytes.Buffer
	// strings.Reader is not an *os.File → not a TTY → must abort even on "y".
	proceed, _ := guardResume(resolveGuard(config.ClientGuard{OnCrashLoop: "prompt"}), cwd, []string{"--continue"}, strings.NewReader("y\n"), &buf)
	if proceed {
		t.Fatal("prompt mode must abort on non-interactive input")
	}
}

func TestClassifyExit(t *testing.T) {
	run := func(sh string) *os.ProcessState {
		c := exec.Command("sh", "-c", sh)
		_ = c.Run()
		return c.ProcessState
	}
	if got := classifyExit(run("exit 0")); got.Reason != reasonOK {
		t.Errorf("exit 0 → %q", got.Reason)
	}
	if got := classifyExit(run("exit 7")); got.Reason != reasonErrorExit || got.Code != 7 {
		t.Errorf("exit 7 → %+v", got)
	}
	if got := classifyExit(run("kill -9 $$")); got.Reason != reasonOOMKilled {
		t.Errorf("SIGKILL → %q, want oom_killed", got.Reason)
	}
	if got := classifyExit(run("kill -15 $$")); got.Reason != reasonTerminated {
		t.Errorf("SIGTERM → %q, want terminated", got.Reason)
	}
	if got := classifyExit(nil); got.Reason != reasonLaunch {
		t.Errorf("nil ProcessState → %q, want launch_error", got.Reason)
	}
}

func TestJitteredBackoff(t *testing.T) {
	base, max := 5*time.Second, 120*time.Second
	upper := max + time.Duration(0.5*float64(max)) + time.Second
	for attempt := 1; attempt <= 40; attempt++ {
		d := jitteredBackoff(attempt, base, max, 0.5)
		if d < 0 || d > upper {
			t.Errorf("attempt %d: delay %v out of [0,%v]", attempt, d, upper)
		}
	}
	if d := jitteredBackoff(1, base, max, 0); d != base {
		t.Errorf("attempt 1 no-jitter = %v, want %v", d, base)
	}
}
