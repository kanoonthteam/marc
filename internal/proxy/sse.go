package proxy

import (
	"bufio"
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// sseResult holds what the SSE tee-reader accumulated.
type sseResult struct {
	rawBody      []byte
	firstChunkMs uint32
	totalMs      uint32
	chunkCount   int
	sawStop      bool

	// Diagnostics for "is the upstream actually streaming, or is the proxy
	// adding latency?" The handler logs these as part of the per-request
	// SSE summary so the operator can compare a direct-to-Anthropic baseline
	// against the same prompt through marc.
	eventTypeCounts    map[string]int
	firstTextDeltaMs   uint32 // ms from streamSSE start to the first content_block_delta of type=text_delta
	firstThinkingMs    uint32 // ms to the first content_block_delta of type=thinking_delta
	maxInterChunkGapMs uint32 // longest gap observed between two consecutive scanner.Scan iterations

	// sawTextDelta / sawThinking distinguish "no event of that type ever
	// arrived" from "first one arrived at t=0ms" (which can happen on tests
	// running in <1ms or on very fast loopback). The Ms fields above mean
	// nothing without these.
	sawTextDelta bool
	sawThinking  bool
}

// streamSSE reads an SSE stream from upstream, writes each chunk to w
// (flushing after each write for real-time streaming), and accumulates the
// raw bytes. It returns once it has seen an "event: message_stop" line or
// the body is exhausted.
//
// startTime is the moment the upstream response headers were received; it is
// used to compute first_chunk_ms and total_ms.
func streamSSE(w http.ResponseWriter, body io.Reader, startTime time.Time) sseResult {
	flusher, canFlush := w.(http.Flusher)

	var acc bytes.Buffer
	var result sseResult
	result.eventTypeCounts = make(map[string]int, 8)
	var firstChunk bool
	var pendingEvent string
	lastChunkTime := startTime

	scanner := bufio.NewScanner(body)
	// SSE events can be very large (full message content). 8 MB should be
	// generous enough for any realistic Anthropic response.
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		acc.Write(line)
		acc.WriteByte('\n')

		now := time.Now()
		if !firstChunk {
			firstChunk = true
			result.firstChunkMs = clampMs(now.Sub(startTime))
		} else if gap := clampMs(now.Sub(lastChunkTime)); gap > result.maxInterChunkGapMs {
			result.maxInterChunkGapMs = gap
		}
		lastChunkTime = now
		result.chunkCount++

		// Forward line to the client immediately.
		if _, err := w.Write(line); err != nil {
			slog.Debug("proxy: client write error during SSE stream", slog.Any("error", err))
			break
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			break
		}
		if canFlush {
			flusher.Flush()
		}

		// --- Diagnostics: classify the line so the per-request summary can
		// distinguish "upstream is sending thinking deltas slowly" from
		// "upstream sent nothing for 4 minutes". Cheap byte-prefix checks;
		// no JSON unmarshal on the hot path.
		switch {
		case bytes.HasPrefix(line, []byte("event: ")):
			pendingEvent = strings.TrimSpace(string(line[len("event: "):]))
			result.eventTypeCounts[pendingEvent]++
			if pendingEvent == "message_stop" {
				result.sawStop = true
			}
		case bytes.HasPrefix(line, []byte("data: ")) && pendingEvent == "content_block_delta":
			data := line[len("data: "):]
			if bytes.Contains(data, []byte(`"type":"text_delta"`)) {
				if !result.sawTextDelta {
					result.firstTextDeltaMs = clampMs(now.Sub(startTime))
					result.sawTextDelta = true
				}
				result.eventTypeCounts["text_delta"]++
			} else if bytes.Contains(data, []byte(`"type":"thinking_delta"`)) {
				if !result.sawThinking {
					result.firstThinkingMs = clampMs(now.Sub(startTime))
					result.sawThinking = true
				}
				result.eventTypeCounts["thinking_delta"]++
			}
		}
	}

	result.totalMs = clampMs(time.Since(startTime))
	result.rawBody = acc.Bytes()
	return result
}

// clampMs converts d to a uint32 millisecond value, saturating at math.MaxUint32.
func clampMs(d time.Duration) uint32 {
	ms := uint64(d.Milliseconds())
	if ms > uint64(^uint32(0)) {
		ms = uint64(^uint32(0))
	}
	return uint32(ms)
}

// copyNonStreaming reads the full upstream body into a buffer, writes it to w,
// and returns the accumulated bytes plus timing.
func copyNonStreaming(w http.ResponseWriter, body io.Reader, startTime time.Time) ([]byte, uint32, uint32, error) {
	flusher, canFlush := w.(http.Flusher)

	var acc bytes.Buffer
	buf := make([]byte, 32*1024)

	var firstChunkMs uint32
	var firstChunk bool

	for {
		n, err := body.Read(buf)
		if n > 0 {
			if !firstChunk {
				firstChunk = true
				elapsed := time.Since(startTime)
				ms := uint64(elapsed.Milliseconds())
				if ms > uint64(^uint32(0)) {
					ms = uint64(^uint32(0))
				}
				firstChunkMs = uint32(ms)
			}
			chunk := buf[:n]
			acc.Write(chunk)
			if _, werr := w.Write(chunk); werr != nil {
				return acc.Bytes(), firstChunkMs, 0, werr
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return acc.Bytes(), firstChunkMs, 0, err
		}
	}

	elapsed := time.Since(startTime)
	ms := uint64(elapsed.Milliseconds())
	if ms > uint64(^uint32(0)) {
		ms = uint64(^uint32(0))
	}
	return acc.Bytes(), firstChunkMs, uint32(ms), nil
}
