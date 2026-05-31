package clauderun

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// RunRecord is one line in the run ledger (~/.marc/runs.jsonl). The ledger
// gives marc the memory it otherwise lacks: it runs claude once and forgets,
// so a `--continue` session that keeps crashing (e.g. OOM) would be blindly
// re-resumed. Keyed by CWD because `claude --continue` resumes the most
// recent session in the current directory.
type RunRecord struct {
	TS         string `json:"ts"` // RFC3339 (UTC)
	CWD        string `json:"cwd"`
	Resume     bool   `json:"resume"` // args requested --continue/-c/--resume
	ExitCode   int    `json:"exit_code"`
	Signal     int    `json:"signal"`
	Reason     string `json:"reason"`
	DurationMS int64  `json:"duration_ms"`
}

// ledgerMaxLines bounds the ledger; it's pruned to this when it grows past 2x.
const ledgerMaxLines = 500

func ledgerPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".marc", "runs.jsonl"), nil
}

// appendRun appends a record and best-effort prunes the file. Callers treat
// errors as non-fatal (the guard degrades to "no history").
func appendRun(rec RunRecord) error {
	path, err := ledgerPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, werr := f.Write(append(line, '\n')); werr != nil {
		f.Close()
		return werr
	}
	if cerr := f.Close(); cerr != nil {
		return cerr
	}
	pruneLedger(path)
	return nil
}

// pruneLedger rewrites the file keeping only the last ledgerMaxLines once it
// exceeds 2x that. Best-effort; ignores errors.
func pruneLedger(path string) {
	all, err := readLines(path)
	if err != nil || len(all) <= ledgerMaxLines*2 {
		return
	}
	keep := all[len(all)-ledgerMaxLines:]
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	for _, l := range keep {
		if _, werr := f.WriteString(l + "\n"); werr != nil {
			f.Close()
			return
		}
	}
	if cerr := f.Close(); cerr != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if t := sc.Text(); t != "" {
			out = append(out, t)
		}
	}
	return out, sc.Err()
}

// recentForCWD returns ledger records for cwd, oldest→newest.
func recentForCWD(cwd string) []RunRecord {
	path, err := ledgerPath()
	if err != nil {
		return nil
	}
	lines, err := readLines(path)
	if err != nil {
		return nil
	}
	var out []RunRecord
	for _, l := range lines {
		var r RunRecord
		if json.Unmarshal([]byte(l), &r) == nil && r.CWD == cwd {
			out = append(out, r)
		}
	}
	return out
}

// abnormalStreak counts consecutive trailing abnormal runs for cwd (newest
// backward), stopping at the first clean (reason "ok") run.
func abnormalStreak(cwd string) int {
	recs := recentForCWD(cwd)
	streak := 0
	for i := len(recs) - 1; i >= 0; i-- {
		if !(exitInfo{Reason: recs[i].Reason}).abnormal() {
			break
		}
		streak++
	}
	return streak
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }
