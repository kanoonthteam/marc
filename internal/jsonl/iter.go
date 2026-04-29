package jsonl

import (
	"bufio"
	"fmt"
	"io"
	"os"
)

// LineReader reads a JSONL file one raw line at a time.
// Callers are responsible for unmarshaling each line into the appropriate type
// (CaptureEvent or AnswerEvent) based on the "source" field.
type LineReader struct {
	f       *os.File
	scanner *bufio.Scanner
}

// NewLineReader opens path for reading and returns a LineReader.
// The caller must not call Next after an error; the file is not automatically
// closed — call Close when done.
func NewLineReader(path string) (*LineReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("jsonl: open %s: %w", path, err)
	}

	scanner := bufio.NewScanner(f)
	// Large buffer: a captured Anthropic event holds the full request and the
	// full SSE-aggregated response. Long tool-use streams or code-heavy
	// completions easily exceed 1 MB — observed 1.7 MB on the Ubuntu host
	// while debugging the marc-process backlog. Match the proxy's own SSE
	// scan ceiling (8 MB in internal/proxy/sse.go) plus headroom so the two
	// can't drift; if the proxy ever lifts its cap, raise this together.
	scanner.Buffer(make([]byte, 1024*256), 16*1024*1024)

	return &LineReader{f: f, scanner: scanner}, nil
}

// Next returns the raw bytes of the next JSONL line (without the trailing newline).
// It returns (nil, io.EOF) when the file is exhausted.
// Empty lines are skipped.
func (lr *LineReader) Next() ([]byte, error) {
	for lr.scanner.Scan() {
		line := lr.scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		// Return a copy so the caller's slice is not overwritten on the next Scan call.
		out := make([]byte, len(line))
		copy(out, line)
		return out, nil
	}
	if err := lr.scanner.Err(); err != nil {
		return nil, fmt.Errorf("jsonl: scan: %w", err)
	}
	return nil, io.EOF
}

// Close closes the underlying file.
func (lr *LineReader) Close() error {
	return lr.f.Close()
}
