package jsonl

import (
	"encoding/json"
	"time"
)

// StreamMeta holds timing metadata for streamed API responses.
type StreamMeta struct {
	WasStreamed  bool   `json:"was_streamed"`
	FirstChunkMs uint32 `json:"first_chunk_ms"`
	TotalMs      uint32 `json:"total_ms"`
	ChunkCount   int    `json:"chunk_count"`
}

// EventError holds error details captured during a proxy request.
type EventError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// CaptureEvent is Variant A: written by marc proxy for Anthropic API captures.
// Source values: "anthropic_api" | "telegram_answer"
type CaptureEvent struct {
	EventID     string          `json:"event_id"`
	Machine     string          `json:"machine"`
	CapturedAt  time.Time       `json:"captured_at"`
	Source      string          `json:"source"`
	RequestID   string          `json:"request_id,omitempty"`
	Method      string          `json:"method,omitempty"`
	Path        string          `json:"path,omitempty"`
	IsInternal  bool            `json:"is_internal"`
	Request     json.RawMessage `json:"request"`
	Response    json.RawMessage `json:"response"`
	StreamMeta  *StreamMeta     `json:"stream_meta,omitempty"`
	Error       *EventError     `json:"error"`
	SessionHint *string         `json:"session_hint"`
	ProjectHint *string         `json:"project_hint"`
}

// AnswerEvent is Variant B: written by marc-server bot for Telegram answers.
// It shares the top-level schema with CaptureEvent but carries different content:
// request.model = "marc-question", response.stop_reason = "user_choice".
// session_hint and project_hint are typically populated for answer events.
type AnswerEvent struct {
	EventID     string          `json:"event_id"`
	Machine     string          `json:"machine"`
	CapturedAt  time.Time       `json:"captured_at"`
	Source      string          `json:"source"`
	IsInternal  bool            `json:"is_internal"`
	Request     json.RawMessage `json:"request"`
	Response    json.RawMessage `json:"response"`
	SessionHint *string         `json:"session_hint"`
	ProjectHint *string         `json:"project_hint"`
}
