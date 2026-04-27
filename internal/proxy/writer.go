package proxy

import (
	"log/slog"

	"github.com/caffeaun/marc/internal/jsonl"
)

// runWriter is the single writer goroutine. It is the ONLY caller of
// jsonl.AppendEvent from this package. Request goroutines are explicitly
// prohibited from calling AppendEvent directly.
//
// Design: jsonl.AppendEvent opens, flocks, writes, and closes the file on
// every call. This means the writer goroutine does NOT need to maintain a
// long-lived file descriptor. When the shipper renames capture.jsonl to
// capture.jsonl.shipping, the very next AppendEvent call sees that the path
// no longer exists and creates a fresh capture.jsonl via O_CREATE. The
// "inode detection" requirement from the spec is therefore satisfied
// implicitly: AppendEvent re-opens every call so it always writes to whatever
// file currently lives at the path.
//
// If a future version of T003 changes AppendEvent to use a long-lived fd,
// add explicit inode detection here (stat the path, compare Ino, reopen on
// change). For now, the open-per-call design in internal/jsonl/append.go
// makes that unnecessary.
func runWriter(ctx interface{ Done() <-chan struct{} }, capturePath string, eventCh <-chan jsonl.CaptureEvent) {
	for {
		select {
		case ev, ok := <-eventCh:
			if !ok {
				// Channel closed — server is shutting down. Drain complete.
				return
			}
			if err := jsonl.AppendEvent(capturePath, ev); err != nil {
				// Log and continue. A capture failure must never crash the
				// writer goroutine or block request handling.
				slog.Error("proxy: failed to append capture event",
					slog.String("event_id", ev.EventID),
					slog.Any("error", err),
				)
			}
		case <-ctx.Done():
			// Context cancelled but we still drain the channel until it is
			// closed by Run() after Shutdown completes.
		}
	}
}
