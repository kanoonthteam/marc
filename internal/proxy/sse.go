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
	var firstChunk bool

	scanner := bufio.NewScanner(body)
	// SSE events can be very large (full message content). 8 MB should be
	// generous enough for any realistic Anthropic response.
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		acc.Write(line)
		acc.WriteByte('\n')

		if !firstChunk {
			firstChunk = true
			elapsed := time.Since(startTime)
			ms := uint64(elapsed.Milliseconds())
			if ms > uint64(^uint32(0)) {
				ms = uint64(^uint32(0))
			}
			result.firstChunkMs = uint32(ms)
		}
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

		// Detect "event: message_stop" — the canonical end-of-conversation marker.
		trimmed := strings.TrimSpace(string(line))
		if trimmed == "event: message_stop" {
			result.sawStop = true
		}
	}

	elapsed := time.Since(startTime)
	ms := uint64(elapsed.Milliseconds())
	if ms > uint64(^uint32(0)) {
		ms = uint64(^uint32(0))
	}
	result.totalMs = uint32(ms)
	result.rawBody = acc.Bytes()
	return result
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
