package jsonl

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestAppendSingleEvent writes one event and reads it back as raw bytes.
func TestAppendSingleEvent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test.jsonl")
	ev := CaptureEvent{
		EventID:    "00000000-0000-4000-8000-000000000001",
		Machine:    "testhost",
		CapturedAt: time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC),
		Source:     "anthropic_api",
		IsInternal: false,
		Request:    json.RawMessage(`{"model":"test"}`),
		Response:   json.RawMessage(`{"status":200}`),
	}

	if err := AppendEvent(path, ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	lr, err := NewLineReader(path)
	if err != nil {
		t.Fatalf("NewLineReader: %v", err)
	}
	defer lr.Close()

	raw, err := lr.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}

	// Verify round-trip: the marshaled form matches what we get back.
	want, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if string(raw) != string(want) {
		t.Errorf("round-trip mismatch\ngot:  %s\nwant: %s", raw, want)
	}

	// Ensure EOF on second call.
	_, err = lr.Next()
	if err != io.EOF {
		t.Errorf("expected io.EOF after last line, got %v", err)
	}
}

// TestRoundTripCaptureEvent checks all fields survive a marshal→append→read→unmarshal cycle.
func TestRoundTripCaptureEvent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "capture.jsonl")

	hint1 := "sess-abc"
	hint2 := "proj-xyz"
	orig := CaptureEvent{
		EventID:     "11111111-1111-4111-8111-111111111111",
		Machine:     "macbook-pro",
		CapturedAt:  time.Date(2026, 4, 26, 12, 30, 0, 123000000, time.UTC),
		Source:      "anthropic_api",
		RequestID:   "req_aabbcc",
		Method:      "POST",
		Path:        "/v1/messages",
		IsInternal:  true,
		Request:     json.RawMessage(`{"model":"claude-opus","stream":true}`),
		Response:    json.RawMessage(`{"status":200,"stop_reason":"end_turn"}`),
		StreamMeta:  &StreamMeta{WasStreamed: true, FirstChunkMs: 234, TotalMs: 4521, ChunkCount: 142},
		Error:       nil,
		SessionHint: &hint1,
		ProjectHint: &hint2,
	}

	if err := AppendEvent(path, orig); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	lr, err := NewLineReader(path)
	if err != nil {
		t.Fatalf("NewLineReader: %v", err)
	}
	defer lr.Close()

	raw, err := lr.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}

	var got CaptureEvent
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.EventID != orig.EventID {
		t.Errorf("EventID: got %q, want %q", got.EventID, orig.EventID)
	}
	if got.Machine != orig.Machine {
		t.Errorf("Machine: got %q, want %q", got.Machine, orig.Machine)
	}
	if !got.CapturedAt.Equal(orig.CapturedAt) {
		t.Errorf("CapturedAt: got %v, want %v", got.CapturedAt, orig.CapturedAt)
	}
	if got.Source != orig.Source {
		t.Errorf("Source: got %q, want %q", got.Source, orig.Source)
	}
	if got.RequestID != orig.RequestID {
		t.Errorf("RequestID: got %q, want %q", got.RequestID, orig.RequestID)
	}
	if got.Method != orig.Method {
		t.Errorf("Method: got %q, want %q", got.Method, orig.Method)
	}
	if got.Path != orig.Path {
		t.Errorf("Path: got %q, want %q", got.Path, orig.Path)
	}
	if got.IsInternal != orig.IsInternal {
		t.Errorf("IsInternal: got %v, want %v", got.IsInternal, orig.IsInternal)
	}
	if string(got.Request) != string(orig.Request) {
		t.Errorf("Request: got %s, want %s", got.Request, orig.Request)
	}
	if string(got.Response) != string(orig.Response) {
		t.Errorf("Response: got %s, want %s", got.Response, orig.Response)
	}
	if got.StreamMeta == nil {
		t.Fatal("StreamMeta: got nil, want non-nil")
	}
	if *got.StreamMeta != *orig.StreamMeta {
		t.Errorf("StreamMeta: got %+v, want %+v", *got.StreamMeta, *orig.StreamMeta)
	}
	if got.SessionHint == nil || *got.SessionHint != hint1 {
		t.Errorf("SessionHint: got %v, want %q", got.SessionHint, hint1)
	}
	if got.ProjectHint == nil || *got.ProjectHint != hint2 {
		t.Errorf("ProjectHint: got %v, want %q", got.ProjectHint, hint2)
	}
}

// TestRoundTripAnswerEvent checks all AnswerEvent fields survive a round trip.
func TestRoundTripAnswerEvent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "answer.jsonl")

	sess := "marc-question-117"
	proj := "sliplotto"
	orig := AnswerEvent{
		EventID:    "22222222-2222-4222-8222-222222222222",
		Machine:    "telegram",
		CapturedAt: time.Date(2026, 4, 26, 10, 53, 12, 456000000, time.UTC),
		Source:     "telegram_answer",
		IsInternal: false,
		Request: json.RawMessage(`{
			"model":"marc-question",
			"messages":[
				{"role":"system","content":"principle_tested: validate at boundary vs validate at use-site"},
				{"role":"user","content":"Situation: ...\nQuestion: ...\nA) opt-a\nB) opt-b"}
			]
		}`),
		Response: json.RawMessage(`{
			"status":200,
			"stop_reason":"user_choice",
			"content":[{"type":"text","text":"B"}],
			"id":"marc-question-117"
		}`),
		SessionHint: &sess,
		ProjectHint: &proj,
	}

	if err := AppendEvent(path, orig); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	lr, err := NewLineReader(path)
	if err != nil {
		t.Fatalf("NewLineReader: %v", err)
	}
	defer lr.Close()

	raw, err := lr.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}

	var got AnswerEvent
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.EventID != orig.EventID {
		t.Errorf("EventID: got %q, want %q", got.EventID, orig.EventID)
	}
	if got.Machine != orig.Machine {
		t.Errorf("Machine: got %q, want %q", got.Machine, orig.Machine)
	}
	if got.Source != orig.Source {
		t.Errorf("Source: got %q, want %q", got.Source, orig.Source)
	}
	if got.SessionHint == nil || *got.SessionHint != sess {
		t.Errorf("SessionHint: got %v, want %q", got.SessionHint, sess)
	}
	if got.ProjectHint == nil || *got.ProjectHint != proj {
		t.Errorf("ProjectHint: got %v, want %q", got.ProjectHint, proj)
	}
}

// makeLargeEvent returns a CaptureEvent with a Request payload >= minBytes bytes.
func makeLargeEvent(id string, minBytes int) CaptureEvent {
	// Build a JSON object with a "padding" field large enough to meet minBytes.
	padding := strings.Repeat("x", minBytes)
	req := json.RawMessage(`{"model":"claude-opus","padding":"` + padding + `"}`)
	return CaptureEvent{
		EventID:    id,
		Machine:    "testhost",
		CapturedAt: time.Now().UTC(),
		Source:     "anthropic_api",
		IsInternal: false,
		Request:    req,
		Response:   json.RawMessage(`{"status":200}`),
	}
}

// TestConcurrentAppendLargeEvents spawns 100 goroutines each writing 100 events
// of ≥100 KB and verifies every line parses as valid JSON with no truncation.
func TestConcurrentAppendLargeEvents(t *testing.T) {
	t.Parallel()

	const (
		goroutines = 100
		eventsEach = 100
		minBytes   = 100 * 1024 // 100 KB per event
	)

	path := filepath.Join(t.TempDir(), "concurrent.jsonl")
	var wg sync.WaitGroup

	for g := range goroutines {
		wg.Add(1)
		go func(gID int) {
			defer wg.Done()
			for e := range eventsEach {
				id, err := NewUUIDv4()
				if err != nil {
					// Cannot call t.Fatal from a goroutine; use panic.
					panic("NewUUIDv4: " + err.Error())
				}
				ev := makeLargeEvent(id, minBytes)
				if err := AppendEvent(path, ev); err != nil {
					panic("AppendEvent g=" + string(rune('0'+gID)) + " e=" + string(rune('0'+e)) + ": " + err.Error())
				}
			}
		}(g)
	}

	wg.Wait()

	// Read all lines and verify each is valid JSON.
	lr, err := NewLineReader(path)
	if err != nil {
		t.Fatalf("NewLineReader: %v", err)
	}
	defer lr.Close()

	count := 0
	for {
		raw, err := lr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next at line %d: %v", count+1, err)
		}
		if !json.Valid(raw) {
			t.Errorf("line %d is not valid JSON (len=%d)", count+1, len(raw))
		}
		count++
	}

	want := goroutines * eventsEach
	if count != want {
		t.Errorf("line count: got %d, want %d", count, want)
	}
}

// TestCrossProcessAppend is the entry point for the cross-process test.
// When env var MARC_APPEND_CHILD=1 it runs in child mode (writing events);
// otherwise it acts as the parent that spawns a child and verifies results.
func TestCrossProcessAppend(t *testing.T) {
	if os.Getenv("MARC_APPEND_CHILD") == "1" {
		// Child mode: write 50 large events to the path provided by the parent.
		path := os.Getenv("MARC_APPEND_PATH")
		if path == "" {
			t.Fatal("MARC_APPEND_PATH not set in child mode")
		}
		for range 50 {
			id, err := NewUUIDv4()
			if err != nil {
				t.Fatalf("NewUUIDv4: %v", err)
			}
			ev := makeLargeEvent(id, 50*1024) // 50 KB events
			if err := AppendEvent(path, ev); err != nil {
				t.Fatalf("AppendEvent child: %v", err)
			}
		}
		return
	}

	// Parent mode.
	path := filepath.Join(t.TempDir(), "cross_process.jsonl")

	// Spawn child process.
	child := exec.Command(os.Args[0], "-test.run=TestCrossProcessAppend", "-test.v")
	child.Env = append(os.Environ(),
		"MARC_APPEND_CHILD=1",
		"MARC_APPEND_PATH="+path,
	)
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr

	// Parent writes 50 events concurrently with the child starting up.
	var parentWg sync.WaitGroup
	parentWg.Add(1)
	go func() {
		defer parentWg.Done()
		for range 50 {
			id, err := NewUUIDv4()
			if err != nil {
				t.Errorf("parent NewUUIDv4: %v", err)
				return
			}
			ev := makeLargeEvent(id, 50*1024)
			if err := AppendEvent(path, ev); err != nil {
				t.Errorf("parent AppendEvent: %v", err)
				return
			}
		}
	}()

	if err := child.Start(); err != nil {
		t.Fatalf("child.Start: %v", err)
	}

	parentWg.Wait()

	if err := child.Wait(); err != nil {
		t.Fatalf("child.Wait: %v", err)
	}

	// Read all lines from the shared file.
	lr, err := NewLineReader(path)
	if err != nil {
		t.Fatalf("NewLineReader: %v", err)
	}
	defer lr.Close()

	count := 0
	for {
		raw, err := lr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next at line %d: %v", count+1, err)
		}
		if !json.Valid(raw) {
			t.Errorf("line %d is not valid JSON (len=%d)", count+1, len(raw))
		}
		count++
	}

	// Parent wrote 50, child wrote 50 → 100 total.
	if count != 100 {
		t.Errorf("cross-process line count: got %d, want 100", count)
	}
}

// uuidV4Re matches the canonical UUID v4 format.
var uuidV4Re = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// TestNewUUIDv4Format generates 1000 UUIDs and verifies format and uniqueness.
func TestNewUUIDv4Format(t *testing.T) {
	t.Parallel()

	const count = 1000
	seen := make(map[string]struct{}, count)

	for range count {
		id, err := NewUUIDv4()
		if err != nil {
			t.Fatalf("NewUUIDv4: %v", err)
		}
		if !uuidV4Re.MatchString(id) {
			t.Errorf("UUID %q does not match format regex", id)
		}
		if _, dup := seen[id]; dup {
			t.Errorf("duplicate UUID: %q", id)
		}
		seen[id] = struct{}{}
	}
}

// TestFilePermissions verifies that AppendEvent creates files with mode 0600.
func TestFilePermissions(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "perms.jsonl")
	ev := CaptureEvent{
		EventID:    "33333333-3333-4333-8333-333333333333",
		Machine:    "test",
		CapturedAt: time.Now().UTC(),
		Source:     "anthropic_api",
		Request:    json.RawMessage(`{}`),
		Response:   json.RawMessage(`{}`),
	}

	if err := AppendEvent(path, ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("file mode: got %o, want 0600", perm)
	}
}

// TestLineReaderEOFEmpty verifies that an empty JSONL file returns io.EOF immediately.
func TestLineReaderEOFEmpty(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(path, []byte{}, 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	lr, err := NewLineReader(path)
	if err != nil {
		t.Fatalf("NewLineReader: %v", err)
	}
	defer lr.Close()

	_, err = lr.Next()
	if err != io.EOF {
		t.Errorf("expected io.EOF for empty file, got %v", err)
	}
}

// TestLineReaderMultipleEvents verifies sequential reading of multiple events.
func TestLineReaderMultipleEvents(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "multi.jsonl")
	events := []CaptureEvent{
		{
			EventID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Machine: "m1",
			CapturedAt: time.Now().UTC(), Source: "anthropic_api",
			Request: json.RawMessage(`{"n":1}`), Response: json.RawMessage(`{}`),
		},
		{
			EventID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Machine: "m2",
			CapturedAt: time.Now().UTC(), Source: "anthropic_api",
			Request: json.RawMessage(`{"n":2}`), Response: json.RawMessage(`{}`),
		},
		{
			EventID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", Machine: "m3",
			CapturedAt: time.Now().UTC(), Source: "anthropic_api",
			Request: json.RawMessage(`{"n":3}`), Response: json.RawMessage(`{}`),
		},
	}

	for _, ev := range events {
		if err := AppendEvent(path, ev); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}

	lr, err := NewLineReader(path)
	if err != nil {
		t.Fatalf("NewLineReader: %v", err)
	}
	defer lr.Close()

	for i, want := range events {
		raw, err := lr.Next()
		if err != nil {
			t.Fatalf("Next[%d]: %v", i, err)
		}
		var got CaptureEvent
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("Unmarshal[%d]: %v", i, err)
		}
		if got.EventID != want.EventID {
			t.Errorf("event[%d] EventID: got %q, want %q", i, got.EventID, want.EventID)
		}
		if got.Machine != want.Machine {
			t.Errorf("event[%d] Machine: got %q, want %q", i, got.Machine, want.Machine)
		}
	}

	_, err = lr.Next()
	if err != io.EOF {
		t.Errorf("expected io.EOF after last event, got %v", err)
	}
}

// TestLineReaderHandlesLargeLines verifies that JSONL lines up to several
// MB are read without bufio.Scanner: token-too-long errors. Reproduces a
// production failure where one event's response field exceeded 1 MB and
// halted marc-process indefinitely.
func TestLineReaderHandlesLargeLines(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "huge.jsonl")

	// Build a CaptureEvent whose response body is ~3 MB. AppendEvent uses
	// json.Marshal so the line on disk is at least that big, comfortably
	// over the old 1 MB limit but well under the new 16 MB ceiling.
	huge := make([]byte, 0, 3*1024*1024+64)
	huge = append(huge, `{"big":"`...)
	for i := 0; i < 3*1024*1024; i++ {
		huge = append(huge, 'x')
	}
	huge = append(huge, `"}`...)

	ev := CaptureEvent{
		EventID:    "12345678-1234-4567-8901-123456789012",
		Machine:    "huge-test",
		CapturedAt: time.Now().UTC(),
		Source:     "anthropic_api",
		Request:    json.RawMessage(`{"n":1}`),
		Response:   json.RawMessage(huge),
	}
	if err := AppendEvent(path, ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	lr, err := NewLineReader(path)
	if err != nil {
		t.Fatalf("NewLineReader: %v", err)
	}
	defer lr.Close()

	raw, err := lr.Next()
	if err != nil {
		t.Fatalf("Next on >3 MB line: %v (must not be 'token too long')", err)
	}
	if len(raw) < 3*1024*1024 {
		t.Errorf("read line is only %d bytes; expected >3 MB", len(raw))
	}

	if _, err := lr.Next(); err != io.EOF {
		t.Errorf("expected io.EOF after the single huge line, got %v", err)
	}
}
