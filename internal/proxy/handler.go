package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/caffeaun/marc/internal/jsonl"
)

// rewriteModel returns a body with the top-level JSON "model" field replaced
// by `model`. Returns the original body and ok=false if:
//   - body isn't valid JSON (e.g. multipart, non-POST without body),
//   - the JSON root isn't an object,
//   - the object has no "model" field at all (some endpoints like /v1/models
//     don't carry one — leave them alone).
//
// Only the top-level field is rewritten; nested occurrences (e.g. inside
// tool descriptions) are untouched.
func rewriteModel(body []byte, model string) ([]byte, bool) {
	if len(body) == 0 || model == "" {
		return body, false
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return body, false
	}
	if _, ok := obj["model"]; !ok {
		return body, false
	}
	obj["model"] = model
	out, err := json.Marshal(obj)
	if err != nil {
		return body, false
	}
	return out, true
}

// handler is the http.Handler for all proxy requests.
type handler struct {
	cfg       Config
	upstreams map[string]*url.URL // pre-parsed per-profile base URLs
	eventCh   chan<- jsonl.CaptureEvent
	transport http.RoundTripper
	health    *healthState
}

// newHandler constructs a handler from the given config and event channel.
// transport may be nil; if so http.DefaultTransport is used (allows tests to
// inject a custom transport).
//
// When cfg.Profiles is empty, synthesizes the default "anthropic" profile from
// cfg.UpstreamURL. This lets tests and pre-profiles callers continue to use the
// flat Config-with-UpstreamURL form without manually setting up Profiles.
func newHandler(cfg Config, eventCh chan<- jsonl.CaptureEvent) *handler {
	synthesizeDefaultProfiles(&cfg)
	upstreams := make(map[string]*url.URL, len(cfg.Profiles))
	for name, p := range cfg.Profiles {
		if u, err := url.Parse(p.BaseURL); err == nil {
			upstreams[name] = u
		}
	}
	return &handler{
		cfg:       cfg,
		upstreams: upstreams,
		eventCh:   eventCh,
		health:    newHealthState(),
	}
}

// routeRequest parses the incoming URL path into (profile, restPath, ok).
//
// Accepted shapes:
//
//	/<profile>/v1/...   → profile = first segment, restPath = "/v1/..."
//	/v1/...             → profile = cfg.DefaultProfile  (legacy / direct caller)
//
// Anything else returns ok=false; the caller should 404.
func (h *handler) routeRequest(rawPath string) (profile, restPath string, ok bool) {
	if !strings.HasPrefix(rawPath, "/") {
		return "", "", false
	}
	trimmed := strings.TrimPrefix(rawPath, "/")
	if trimmed == "" {
		return "", "", false
	}
	first, rest, _ := strings.Cut(trimmed, "/")

	// Legacy: /v1/... routes to default profile.
	if first == "v1" {
		return h.cfg.DefaultProfile, rawPath, true
	}

	// /<profile>/v1/... — the profile must exist and rest must start with v1.
	if _, exists := h.upstreams[first]; !exists {
		return "", "", false
	}
	if !strings.HasPrefix(rest, "v1/") && rest != "v1" {
		return "", "", false
	}
	return first, "/" + rest, true
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

	// Resolve the path → (profile, upstream rest-path).
	profileName, restPath, ok := h.routeRequest(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	profile := h.cfg.Profiles[profileName]
	upstreamRoot := h.upstreams[profileName]
	if upstreamRoot == nil {
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
	log := slog.With(
		slog.String("request_id", reqID),
		slog.String("profile", profileName),
	)
	log.Info("request received",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.String("upstream_path", restPath),
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

	// Build the upstream request URL using the per-profile base + the
	// rest-path stripped from the routing prefix. base_url may itself
	// contain a path (e.g. "https://api.minimax.io/anthropic") — JOIN
	// it with restPath rather than overwriting, so /v1/messages becomes
	// /anthropic/v1/messages on minimax. When base_url has no path the
	// behaviour is unchanged from a single-segment join.
	upURL := *upstreamRoot
	basePath := strings.TrimRight(upstreamRoot.Path, "/")
	upURL.Path = basePath + restPath
	upURL.RawQuery = r.URL.RawQuery

	// Per-profile model rewrite: if profile.Model is set, replace the
	// request body's "model" field before forwarding. The captured body
	// (reqBody, used in buildCaptureEvent below) keeps the ORIGINAL model
	// so the corpus reflects what the caller actually requested.
	upstreamBody := reqBody
	if profile.Model != "" {
		if rewritten, ok := rewriteModel(reqBody, profile.Model); ok {
			upstreamBody = rewritten
			log.Debug("proxy: model rewrite applied",
				slog.String("to", profile.Model),
				slog.Int("body_bytes_before", len(reqBody)),
				slog.Int("body_bytes_after", len(upstreamBody)),
			)
		}
	}

	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, upURL.String(), bytes.NewReader(upstreamBody))
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

	// Per-profile auth-style adjustment.
	switch profile.AuthStyle {
	case "bearer":
		// Convert Anthropic-native x-api-key to OpenAI-style bearer token.
		// Use the request's incoming key if no profile-level key configured;
		// otherwise the operator's stored key wins (lets a single Claude
		// session route to a non-Anthropic provider with a different key).
		key := profile.APIKey
		if key == "" {
			key = upReq.Header.Get("x-api-key")
		}
		upReq.Header.Del("x-api-key")
		if key != "" {
			upReq.Header.Set("Authorization", "Bearer "+key)
		}
	case "x-api-key", "":
		// Default Anthropic style — pass through unchanged.
	}

	// Per-profile header overrides (applied last so they always win).
	for k, v := range profile.HeaderOverrides {
		upReq.Header.Set(k, v)
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
