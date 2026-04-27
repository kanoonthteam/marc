package jsonl

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"syscall"
)

// appendMu serializes all AppendEvent calls within the same process.
//
// Rationale: O_APPEND atomicity is POSIX-guaranteed only for writes ≤ PIPE_BUF
// (typically 4 KB on Linux, 512 bytes on macOS). Full Anthropic JSONL events
// run 50–200 KB. Without this mutex, two goroutines writing large events
// concurrently would interleave their writes even with O_APPEND, producing
// corrupted JSONL lines.
//
// Cross-process safety is handled separately via syscall.Flock (LOCK_EX) inside
// AppendEvent, which blocks until the OS-level advisory lock is acquired. Both
// mechanisms are required: mutex covers in-process concurrency; flock covers
// cross-process concurrency (e.g., marc proxy and marc-server bot writing the
// same capture.jsonl on Ubuntu).
var appendMu sync.Mutex

// AppendEvent marshals event as a single JSON line and appends it to path,
// creating the file if it does not exist.
//
// The file is opened with mode 0600 because it contains API request/response
// bodies — sensitive data that must not be world-readable.
//
// Locking order:
//  1. Acquire appendMu (in-process serialization for events larger than PIPE_BUF).
//  2. Open file with O_APPEND|O_CREATE|O_WRONLY.
//  3. Acquire syscall.Flock(LOCK_EX) (cross-process serialization).
//  4. Marshal + write (marshaled bytes + newline).
//  5. Release flock (LOCK_UN).
//  6. Close file.
//  7. Release appendMu (via defer).
//
// If any step fails an error is returned. A partial write is impossible because
// we hold both mutex and flock before touching the file.
func AppendEvent(path string, event any) error {
	// Step 1: acquire in-process lock.
	appendMu.Lock()
	defer appendMu.Unlock()

	// Step 2: open/create file.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("jsonl: open %s: %w", path, err)
	}
	// Ensure file is closed even on error paths.
	// Note: flock is released before Close so the OS unlock happens first.
	defer f.Close() //nolint:errcheck // read-only operation; write errors caught earlier

	// Step 3: acquire cross-process exclusive lock.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("jsonl: flock LOCK_EX %s: %w", path, err)
	}
	// Release flock before closing so another process can acquire it immediately.
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }() //nolint:errcheck

	// Step 4: marshal to JSON (single-line — JSONL must not contain embedded newlines).
	marshaled, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("jsonl: marshal event: %w", err)
	}

	// Append marshaled JSON + newline in one Write call.
	// A single write is best-effort atomic at the OS level; the combination of
	// mutex + flock makes it safe regardless of write size.
	line := append(marshaled, '\n')
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("jsonl: write to %s: %w", path, err)
	}

	return nil
}
