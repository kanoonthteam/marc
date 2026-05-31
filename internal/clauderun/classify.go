package clauderun

import (
	"os"
	"syscall"
)

// Terminal-reason names for a finished claude invocation.
const (
	reasonOK         = "ok"
	reasonOOMKilled  = "oom_killed"
	reasonTerminated = "terminated"
	reasonErrorExit  = "error_exit"
	reasonLaunch     = "launch_error"
)

// exitInfo is the classified outcome of one claude invocation.
type exitInfo struct {
	Code   int    // process exit code; -1 when killed by a signal
	Signal int    // terminating signal number, or 0
	Reason string // one of the reason* constants
	Advice string // one-line operator guidance for abnormal endings
}

// classifyExit maps a finished process state to a named terminal reason and
// operator advice — the "explainer" pattern adapted from hermes-agent's
// turn-completion classifier. ps may be nil when the process never started.
func classifyExit(ps *os.ProcessState) exitInfo {
	if ps == nil {
		return exitInfo{Code: -1, Reason: reasonLaunch}
	}
	code := ps.ExitCode()
	sig := 0
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		sig = int(ws.Signal())
	}

	switch {
	case sig == int(syscall.SIGKILL) || code == 137:
		return exitInfo{Code: code, Signal: sig, Reason: reasonOOMKilled,
			Advice: "claude was OS-killed (exit 137 / SIGKILL — typically the OOM killer). " +
				"`--continue` resumes the SAME session and will re-trigger it. " +
				"Narrow the task, reduce memory use, or start a fresh session."}
	case sig == int(syscall.SIGTERM) || code == 143:
		return exitInfo{Code: code, Signal: sig, Reason: reasonTerminated,
			Advice: "claude was terminated (SIGTERM)."}
	case code == 0:
		return exitInfo{Code: 0, Reason: reasonOK}
	default:
		return exitInfo{Code: code, Signal: sig, Reason: reasonErrorExit,
			Advice: "claude exited with a non-zero status."}
	}
}

// abnormal reports whether the reason represents a non-clean ending.
func (e exitInfo) abnormal() bool {
	return e.Reason != reasonOK && e.Reason != ""
}
