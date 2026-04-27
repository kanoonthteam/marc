// Package clickhouse provides a thin wrapper around the native
// ClickHouse driver (clickhouse-go/v2) for marc-server.
//
// This package is server-binary-only: it must not be imported from
// internal/proxy, internal/ship, or any other package used by the client
// binary cmd/marc. Verify with:
//
//	go list -deps ./cmd/marc | grep clickhouse   # must be empty
package clickhouse

import (
	"time"

	"github.com/google/uuid"
)

// Event mirrors every column in the marc.events ClickHouse table.
// The table uses a ReplacingMergeTree engine with ORDER BY (project_id, captured_at, event_id).
// During background merges, rows with the same ORDER BY key are deduplicated — only the
// most-recently inserted version is kept. This means re-inserting a row with the same
// (project_id, captured_at, event_id) is safe: after an OPTIMIZE TABLE marc.events FINAL
// (or after a background merge), duplicates are removed. Callers should not rely on
// immediate deduplication — duplicates may be visible briefly between insert and merge.
type Event struct {
	// Identity & metadata
	EventID   uuid.UUID `ch:"event_id"`
	Machine   string    `ch:"machine"`
	ProjectID string    `ch:"project_id"`
	// CapturedAt uses millisecond precision (DateTime64(3)).
	CapturedAt  time.Time `ch:"captured_at"`
	Source      string    `ch:"source"`
	IsInternal  bool      `ch:"is_internal"`
	SessionHint string    `ch:"session_hint"`

	// Raw capture from proxy (JSON blobs stored as strings)
	RawRequestBody     string `ch:"raw_request_body"`
	RawResponseBody    string `ch:"raw_response_body"`
	ResponseStatus     uint16 `ch:"response_status"`
	ResponseStopReason string `ch:"response_stop_reason"`
	RequestModel       string `ch:"request_model"`

	// Token & timing metrics
	InputTokens      uint32 `ch:"input_tokens"`
	OutputTokens     uint32 `ch:"output_tokens"`
	CacheReadTokens  uint32 `ch:"cache_read_tokens"`
	CacheWriteTokens uint32 `ch:"cache_write_tokens"`
	FirstChunkMs     uint32 `ch:"first_chunk_ms"`
	TotalMs          uint32 `ch:"total_ms"`
	ErrorType        string `ch:"error_type"`
	ErrorMessage     string `ch:"error_message"`

	// Denoised by processor
	UserText      string    `ch:"user_text"`
	AssistantText string    `ch:"assistant_text"`
	Summary       string    `ch:"summary"`
	HasDecision   bool      `ch:"has_decision"`
	SkipReason    string    `ch:"skip_reason"`
	DenoisedAt    time.Time `ch:"denoised_at"`
	DenoiseModel  string    `ch:"denoise_model"`
}
