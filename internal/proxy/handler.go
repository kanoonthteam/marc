package proxy

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/caffeaun/marc/internal/jsonl"
)

// handler is the http.Handler for all proxy requests.
type handler struct {
	cfg       Config
	upstream  *url.URL
	eventCh   chan<- jsonl.CaptureEvent
	transport http.RoundTripper
	health    *healthState
}

// newHandler constructs a handler from the given config and event channel.
// transport may be nil; if so http.DefaultTransport is used (allows tests to
// inject a custom transport).
func newHandler(cfg Config, eventCh chan<- jsonl.CaptureEvent) *handler {
	u, _ := url.Parse(cfg.UpstreamURL)
	return &handler{
		cfg:      cfg,
		upstream: u,
		eventCh:  eventCh,
		health:   newHealthState(),
	}
}

// countingResponseWriter wraps an http.ResponseWriter and tracks total bytes
// written to the client. Flush is forwarded to the underlying writer so SSE
// streaming continues to flush eagerly.
type countingResponseWriter struct {
	http.ResponseWriter
	bytesWritten atomic.Int64
}

func (c *countingResponseWriter) Write(b []byte) (int, error) {
	n, err := c.ResponseWriter.Write(b)
	if n > 0 {
		c.bytesWritten.Add(int64(n))
	}
	return n, err
}

func (c *countingResponseWriter) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (c *countingResponseWriter) BytesWritten() int64 {
	return c.bytesWritten.Load()
}

// ServeHTTP satisfies http.Handler.
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// /_marc/* is marc's own surface — short-circuit before any /v1 logic so it
	// never forwards upstream and never touches the capture file.
	if r.URL.Path == "/_marc/health" {
		h.serveHealth(w, r)
		return
	}
	// Only proxy /v1/* paths. Return 404 for anything else.
	if !strings.HasPrefix(r.URL.Path, "/v1/") {
		http.NotFound(w, r)
		return
	}

	reqStart := time.Now()
	reqID, idErr := jsonl.NewUUIDv4()
	if idErr != nil {
		// crypto/rand failures are essentially impossible on a healthy host;
		// fall through with a sentinel rather than 500'ing the request.
		reqID = "no-id"
	}
	log := slog.With(slog.String("request_id", reqID))
	log.Info("request received",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)

	cw := &countingResponseWriter{ResponseWriter: w}

	// Read and buffer the request body so we can (a) forward it and (b) capture it.
	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		h.health.recordFailure("read request body: " + err.Error())
		log.Error("proxy: failed to read request body", slog.Any("error", err))
		http.Error(cw, "failed to read request body", http.StatusBadGateway)
		return
	}
	_ = r.Body.Close()

	// Build the upstream request URL.
	upURL := *h.upstream
	upURL.Path = r.URL.Path
	upURL.RawQuery = r.URL.RawQuery

	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, upURL.String(), bytes.NewReader(reqBody))
	if err != nil {
		h.health.recordFailure("build upstream request: " + err.Error())
		log.Error("proxy: failed to build upstream request", slog.Any("error", err))
		http.Error(cw, "upstream request error", http.StatusBadGateway)
		return
	}

	// Copy all incoming headers to the upstream request.
	// Authorization / x-api-key flow through unchanged (required for auth).
	for k, vals := range r.Header {
		for _, v := range vals {
			upReq.Header.Add(k, v)
		}
	}
	// Strip Accept-Encoding so the upstream returns uncompressed SSE.
	// We need to read the SSE stream as plain text to aggregate it into the
	// JSONL event's response field; with gzip enabled, the proxy would have
	// to inflate before scanning, and the saved bytes would be opaque to the
	// downstream denoise step. Bandwidth cost is negligible for our scale.
	upReq.Header.Del("Accept-Encoding")
	// Detailed header dump at debug level with sensitive values redacted.
	log.Debug("proxy: forwarding request headers",
		slog.Any("headers", redactHeaders(r.Header, h.cfg.StrippedHeaders)),
	)

	transport := h.transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	log.Info("forwarding upstream", slog.String("upstream_url", upURL.String()))

	requestSent := time.Now()
	resp, err := transport.RoundTrip(upReq)
	if err != nil {
		h.health.recordFailure("upstream transport: " + err.Error())
		log.Error("proxy: upstream request failed",
			slog.String("path", r.URL.Path),
			slog.Any("error", err),
		)
		http.Error(cw, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close() //nolint:errcheck

	upstreamMs := time.Since(requestSent).Milliseconds()
	log.Info("upstream responded",
		slog.Int("status", resp.StatusCode),
		slog.Int64("duration_ms", upstreamMs),
	)

	// Copy upstream response headers to the client.
	for k, vals := range resp.Header {
		for _, v := range vals {
			cw.Header().Add(k, v)
		}
	}
	cw.WriteHeader(resp.StatusCode)

	// Detect whether this is a streaming (SSE) response.
	contentType := resp.Header.Get("Content-Type")
	isSSE := strings.Contains(contentType, "text/event-stream")

	var (
		responseBody []byte
		streamMeta   *jsonl.StreamMeta
	)

	if isSSE {
		res := streamSSE(cw, resp.Body, requestSent)
		// Reconstruct the non-streaming-shaped response JSON object from the
		// raw SSE event stream so downstream (denoise / generate) can read
		// assistant text uniformly. Falls back to the raw bytes (which fail
		// json.Valid and produce response: null) when the stream is malformed.
		if agg := aggregateSSE(res.rawBody); len(agg) > 0 {
			responseBody = agg
		} else {
			responseBody = res.rawBody
			log.Warn("proxy: SSE aggregation produced empty result; response will be null",
				slog.Int("raw_body_bytes", len(res.rawBody)),
				slog.Int("chunk_count", res.chunkCount),
				slog.Bool("saw_stop", res.sawStop),
				slog.String("first_500", string(res.rawBody[:min(500, len(res.rawBody))])),
			)
		}
		streamMeta = &jsonl.StreamMeta{
			WasStreamed:  true,
			FirstChunkMs: res.firstChunkMs,
			TotalMs:      res.totalMs,
			ChunkCount:   res.chunkCount,
		}
		// Per-request SSE diagnostic. Lets the operator tell apart
		// "marc is buffering" from "Anthropic is slow-streaming thinking".
		// Compare a slow direct-to-Anthropic baseline against the same
		// prompt through marc — if first_text_delta_ms and event_type_counts
		// match, the proxy is innocent.
		summary := []any{
			slog.Int("chunk_count", res.chunkCount),
			slog.Int("first_chunk_ms", int(res.firstChunkMs)),
			slog.Int("max_inter_chunk_gap_ms", int(res.maxInterChunkGapMs)),
			slog.Int("total_ms", int(res.totalMs)),
			slog.Bool("saw_stop", res.sawStop),
			// keepalive_pings surfaces the "Anthropic is silently reasoning"
			// pattern without making the operator know that ping events are
			// what fills the gap before text_delta starts.
			slog.Int("keepalive_pings", res.eventTypeCounts["ping"]),
			slog.Any("event_type_counts", res.eventTypeCounts),
		}
		// Only emit first_text_delta_ms / first_thinking_ms when the
		// corresponding event actually arrived — otherwise the value 0 is
		// ambiguous (no event vs. event at t=0).
		if res.sawTextDelta {
			summary = append(summary, slog.Int("first_text_delta_ms", int(res.firstTextDeltaMs)))
		}
		if res.sawThinking {
			summary = append(summary, slog.Int("first_thinking_ms", int(res.firstThinkingMs)))
		}
		log.Info("sse stream summary", summary...)
		if !res.sawStop {
			log.Debug("proxy: SSE stream ended without message_stop", slog.String("path", r.URL.Path))
		}
	} else {
		var firstChunkMs, totalMs uint32
		responseBody, firstChunkMs, totalMs, err = copyNonStreaming(cw, resp.Body, requestSent)
		if err != nil {
			log.Error("proxy: error copying non-streaming response", slog.Any("error", err))
		}
		// Non-streaming: we still populate StreamMeta with timing so callers
		// get consistent data. WasStreamed = false distinguishes them.
		streamMeta = &jsonl.StreamMeta{
			WasStreamed:  false,
			FirstChunkMs: firstChunkMs,
			TotalMs:      totalMs,
			ChunkCount:   0,
		}
	}

	totalMs := time.Since(reqStart).Milliseconds()
	log.Info("response sent",
		slog.Int("status", resp.StatusCode),
		slog.Int64("duration_ms", totalMs),
		slog.Int64("bytes_written", cw.BytesWritten()),
	)

	// Build and dispatch the capture event AFTER the client has received the
	// full response. The request goroutine sends to the channel; the writer
	// goroutine is the only one that calls AppendEvent.
	upstreamErr := errorFromStatus(resp.StatusCode, responseBody)
	if upstreamErr != nil {
		h.health.recordFailure(upstreamErr.Message)
	} else {
		h.health.recordSuccess()
	}
	ev, buildErr := buildCaptureEvent(
		reqBody,
		responseBody,
		resp.StatusCode,
		r,
		h.cfg.Machine,
		streamMeta,
		upstreamErr,
	)
	if buildErr != nil {
		log.Error("proxy: failed to build capture event",
			slog.String("path", r.URL.Path),
			slog.Any("error", buildErr),
		)
		return
	}

	// Override RequestID from the response header if available.
	if rid := resp.Header.Get("request-id"); rid != "" {
		ev.RequestID = rid
	}

	sendEvent(h.eventCh, ev)
}
