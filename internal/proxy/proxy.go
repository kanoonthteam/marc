// Package proxy implements the marc HTTPS proxy daemon.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/caffeaun/marc/internal/jsonl"
)

// Config holds all configuration values for the proxy daemon.
type Config struct {
	// ListenAddr is the address the proxy listens on, e.g. "127.0.0.1:8082".
	ListenAddr string

	// UpstreamURL is the upstream API base URL, e.g. "https://api.anthropic.com".
	UpstreamURL string

	// CapturePath is the fully expanded path to capture.jsonl.
	CapturePath string

	// Machine is the machine name written into captured events.
	Machine string

	// StrippedHeaders are header names whose values are redacted in logs.
	// The check is case-insensitive.
	StrippedHeaders []string

	// EventChanCap is the capacity of the event channel between request goroutines
	// and the single-writer goroutine. Defaults to 256.
	EventChanCap int

	// Version is the marc binary version, surfaced via /_marc/health.
	Version string

	// Listener, when non-nil, takes precedence over ListenAddr. Self-tests
	// pre-bind to an ephemeral port and pass the listener so they can read
	// the kernel-assigned address before the server starts serving.
	Listener net.Listener
}

// overflowDrops counts events dropped because the channel was full.
// Incremented atomically by request goroutines; read in tests via OverflowDrops.
var overflowDrops atomic.Uint64

// OverflowDrops returns the current count of dropped events.
// Exported for tests.
func OverflowDrops() uint64 {
	return overflowDrops.Load()
}

// resetOverflowDrops zeroes the counter. Called in tests.
func resetOverflowDrops() {
	overflowDrops.Store(0)
}

// Run starts the proxy daemon and blocks until ctx is cancelled.
//
// Architecture: request goroutines construct a CaptureEvent and perform a
// non-blocking send onto eventCh. A single writer goroutine (started by Run)
// owns the capture file and is the ONLY caller of jsonl.AppendEvent from this
// package. This centralises file-descriptor ownership and avoids the
// rename-vs-write race during shipper rotation.
func Run(ctx context.Context, cfg Config) error {
	if cfg.EventChanCap <= 0 {
		cfg.EventChanCap = 256
	}
	if cfg.UpstreamURL == "" {
		cfg.UpstreamURL = "https://api.anthropic.com"
	}

	eventCh := make(chan jsonl.CaptureEvent, cfg.EventChanCap)

	// Start the single writer goroutine. It exits when eventCh is closed.
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		runWriter(ctx, cfg.CapturePath, eventCh)
	}()

	h := newHandler(cfg, eventCh)

	srv := &http.Server{
		Handler:      h,
		ReadTimeout: 30 * time.Second,
		// http.Server.WriteTimeout is the deadline for writing the *entire*
		// response, streaming bodies included — so a single long Claude
		// request (sub-agent invocations, deep reasoning + tool use loops)
		// gets a TCP RST when this fires mid-stream. Observed live: a PM
		// sub-agent run that took ~7 min produced "socket connection was
		// closed unexpectedly" on the client. 30 min covers virtually every
		// realistic single-request agent flow; Anthropic ends the stream
		// long before then for healthy connections.
		WriteTimeout: 30 * time.Minute,
		IdleTimeout:  60 * time.Second,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	serverErr := make(chan error, 1)
	go func() {
		var serveErr error
		if cfg.Listener != nil {
			slog.Info("proxy listening",
				slog.String("addr", cfg.Listener.Addr().String()),
				slog.String("upstream", cfg.UpstreamURL),
			)
			serveErr = srv.Serve(cfg.Listener)
		} else {
			srv.Addr = cfg.ListenAddr
			slog.Info("proxy listening",
				slog.String("addr", cfg.ListenAddr),
				slog.String("upstream", cfg.UpstreamURL),
			)
			serveErr = srv.ListenAndServe()
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			serverErr <- serveErr
		}
		close(serverErr)
	}()

	select {
	case <-ctx.Done():
		slog.Info("proxy: shutdown signal received")
	case err, ok := <-serverErr:
		close(eventCh)
		<-writerDone
		if ok && err != nil {
			return fmt.Errorf("proxy: listen: %w", err)
		}
		return nil
	}

	// Graceful shutdown: give in-flight requests 30 seconds.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("proxy: shutdown error", slog.Any("error", err))
	}

	// Close the event channel so the writer goroutine drains and exits.
	close(eventCh)
	<-writerDone

	slog.Info("proxy: stopped")
	return nil
}

// sendEvent tries a non-blocking send on eventCh.
// If the channel is full the event is dropped and the overflow counter
// is incremented. The request is never blocked.
func sendEvent(eventCh chan<- jsonl.CaptureEvent, ev jsonl.CaptureEvent) {
	select {
	case eventCh <- ev:
	default:
		overflowDrops.Add(1)
		slog.Warn("proxy: event channel full, dropping capture event",
			slog.String("event_id", ev.EventID),
			slog.Uint64("total_dropped", overflowDrops.Load()),
		)
	}
}

// redactHeaders returns a copy of h with sensitive header values replaced by
// "<redacted>". Comparison is case-insensitive. This ensures auth header
// values are never written to log output.
func redactHeaders(h http.Header, stripped []string) http.Header {
	set := make(map[string]bool, len(stripped))
	for _, s := range stripped {
		set[lowercaseASCII(s)] = true
	}
	redacted := make(http.Header, len(h))
	for k, v := range h {
		if set[lowercaseASCII(k)] {
			redacted[k] = []string{"<redacted>"}
		} else {
			redacted[k] = v
		}
	}
	return redacted
}

// lowercaseASCII returns s lowercased for ASCII characters only.
func lowercaseASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
