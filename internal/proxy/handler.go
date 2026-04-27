package proxy

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/caffeaun/marc/internal/jsonl"
)

// handler is the http.Handler for all proxy requests.
type handler struct {
	cfg       Config
	upstream  *url.URL
	eventCh   chan<- jsonl.CaptureEvent
	transport http.RoundTripper
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
	}
}

// ServeHTTP satisfies http.Handler.
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only proxy /v1/* paths. Return 404 for anything else.
	if !strings.HasPrefix(r.URL.Path, "/v1/") {
		http.NotFound(w, r)
		return
	}

	// Read and buffer the request body so we can (a) forward it and (b) capture it.
	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("proxy: failed to read request body", slog.Any("error", err))
		http.Error(w, "failed to read request body", http.StatusBadGateway)
		return
	}
	_ = r.Body.Close()

	// Build the upstream request URL.
	upURL := *h.upstream
	upURL.Path = r.URL.Path
	upURL.RawQuery = r.URL.RawQuery

	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, upURL.String(), bytes.NewReader(reqBody))
	if err != nil {
		slog.Error("proxy: failed to build upstream request", slog.Any("error", err))
		http.Error(w, "upstream request error", http.StatusBadGateway)
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
	// Log headers at debug level with sensitive values redacted.
	slog.Debug("proxy: forwarding request",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Any("headers", redactHeaders(r.Header, h.cfg.StrippedHeaders)),
	)

	transport := h.transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	requestSent := time.Now()
	resp, err := transport.RoundTrip(upReq)
	if err != nil {
		slog.Error("proxy: upstream request failed",
			slog.String("path", r.URL.Path),
			slog.Any("error", err),
		)
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close() //nolint:errcheck

	// Copy upstream response headers to the client.
	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// Detect whether this is a streaming (SSE) response.
	contentType := resp.Header.Get("Content-Type")
	isSSE := strings.Contains(contentType, "text/event-stream")

	var (
		responseBody []byte
		streamMeta   *jsonl.StreamMeta
	)

	if isSSE {
		res := streamSSE(w, resp.Body, requestSent)
		// Reconstruct the non-streaming-shaped response JSON object from the
		// raw SSE event stream so downstream (denoise / generate) can read
		// assistant text uniformly. Falls back to the raw bytes (which fail
		// json.Valid and produce response: null) when the stream is malformed.
		if agg := aggregateSSE(res.rawBody); len(agg) > 0 {
			responseBody = agg
		} else {
			responseBody = res.rawBody
			slog.Warn("proxy: SSE aggregation produced empty result; response will be null",
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
		if !res.sawStop {
			slog.Debug("proxy: SSE stream ended without message_stop", slog.String("path", r.URL.Path))
		}
	} else {
		var firstChunkMs, totalMs uint32
		responseBody, firstChunkMs, totalMs, err = copyNonStreaming(w, resp.Body, requestSent)
		if err != nil {
			slog.Error("proxy: error copying non-streaming response", slog.Any("error", err))
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

	// Build and dispatch the capture event AFTER the client has received the
	// full response. The request goroutine sends to the channel; the writer
	// goroutine is the only one that calls AppendEvent.
	upstreamErr := errorFromStatus(resp.StatusCode, responseBody)
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
		slog.Error("proxy: failed to build capture event",
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
