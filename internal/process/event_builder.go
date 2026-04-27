package process

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/caffeaun/marc/internal/clickhouse"
	"github.com/caffeaun/marc/internal/jsonl"
	"github.com/caffeaun/marc/internal/ollama"
)

// rawRequest is the subset of the Anthropic request body we inspect for
// the project_id stub heuristic. Only the "system" field is needed.
type rawRequest struct {
	System json.RawMessage `json:"system"`
}

// projectIDFromRawRequest derives a project_id from the system field of the
// raw request body using the v1 stub heuristic.
//
// project_id: v1 stub per spec open question Q3 — iterates after first real ingest.
//
// Algorithm:
//  1. Parse the raw request body and extract the "system" field.
//  2. If "system" is absent, null, empty string, or an empty JSON array, return "unknown".
//  3. Convert the system value to its string representation (unwrap a JSON string literal
//     or use the raw bytes for objects/arrays).
//  4. SHA-256 of the first 64 bytes of that string; take the first 8 hex chars.
func projectIDFromRawRequest(rawRequestBody string) string {
	if rawRequestBody == "" {
		return "unknown"
	}

	var req rawRequest
	if err := json.Unmarshal([]byte(rawRequestBody), &req); err != nil {
		return "unknown"
	}

	if len(req.System) == 0 {
		return "unknown"
	}

	// system may be a JSON string, array, or object. Try to unwrap a plain string first.
	var systemStr string
	if err := json.Unmarshal(req.System, &systemStr); err == nil {
		// It was a JSON string literal.
		if systemStr == "" {
			return "unknown"
		}
		return hashFirst64(systemStr)
	}

	// Not a plain string — use raw JSON bytes (e.g., array of content blocks).
	raw := string(req.System)
	if raw == "null" || raw == "" || raw == "[]" {
		return "unknown"
	}
	return hashFirst64(raw)
}

// hashFirst64 SHA-256s the first 64 bytes of s and returns the first 8 hex chars.
func hashFirst64(s string) string {
	input := []byte(s)
	if len(input) > 64 {
		input = input[:64]
	}
	sum := sha256.Sum256(input)
	return fmt.Sprintf("%x", sum[:4]) // 4 bytes = 8 hex chars
}

// buildEvent converts a raw CaptureEvent and its DenoiseResult into a
// clickhouse.Event ready for insertion.
func buildEvent(raw jsonl.CaptureEvent, dr *ollama.DenoiseResult, machine, denoiseModel string) clickhouse.Event {
	eventID, err := uuid.Parse(raw.EventID)
	if err != nil {
		// Generate a new UUID v4 if the stored one is unparseable.
		eventID = uuid.New()
	}

	// Extract fields from the response body if present.
	var (
		responseStatus     uint16
		responseStopReason string
		requestModel       string
		inputTokens        uint32
		outputTokens       uint32
		cacheReadTokens    uint32
		cacheWriteTokens   uint32
		firstChunkMs       uint32
		totalMs            uint32
		errType            string
		errMsg             string
		sessionHint        string
	)

	if raw.SessionHint != nil {
		sessionHint = *raw.SessionHint
	}

	if raw.StreamMeta != nil {
		firstChunkMs = raw.StreamMeta.FirstChunkMs
		totalMs = raw.StreamMeta.TotalMs
	}

	if raw.Error != nil {
		errType = raw.Error.Type
		errMsg = raw.Error.Message
	}

	// Extract token usage and model from response body when possible.
	if len(raw.Response) > 0 {
		var resp struct {
			Model      string `json:"model"`
			StopReason string `json:"stop_reason"`
			Usage      struct {
				InputTokens              uint32 `json:"input_tokens"`
				OutputTokens             uint32 `json:"output_tokens"`
				CacheCreationInputTokens uint32 `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     uint32 `json:"cache_read_input_tokens"`
			} `json:"usage"`
		}
		if jsonErr := json.Unmarshal(raw.Response, &resp); jsonErr == nil {
			responseStopReason = resp.StopReason
			requestModel = resp.Model
			inputTokens = resp.Usage.InputTokens
			outputTokens = resp.Usage.OutputTokens
			cacheWriteTokens = resp.Usage.CacheCreationInputTokens
			cacheReadTokens = resp.Usage.CacheReadInputTokens
		}
	}

	// Extract model from request body as fallback.
	if requestModel == "" && len(raw.Request) > 0 {
		var reqBody struct {
			Model string `json:"model"`
		}
		if jsonErr := json.Unmarshal(raw.Request, &reqBody); jsonErr == nil {
			requestModel = reqBody.Model
		}
	}

	projectID := projectIDFromRawRequest(string(raw.Request))

	source := raw.Source
	if source == "" {
		source = "anthropic_api"
	}

	return clickhouse.Event{
		EventID:    eventID,
		Machine:    machine,
		ProjectID:  projectID,
		CapturedAt: raw.CapturedAt,
		Source:     source,
		IsInternal: raw.IsInternal,
		SessionHint: sessionHint,

		RawRequestBody:     string(raw.Request),
		RawResponseBody:    string(raw.Response),
		ResponseStatus:     responseStatus,
		ResponseStopReason: responseStopReason,
		RequestModel:       requestModel,

		InputTokens:      inputTokens,
		OutputTokens:     outputTokens,
		CacheReadTokens:  cacheReadTokens,
		CacheWriteTokens: cacheWriteTokens,
		FirstChunkMs:     firstChunkMs,
		TotalMs:          totalMs,
		ErrorType:        errType,
		ErrorMessage:     errMsg,

		UserText:      dr.UserText,
		AssistantText: dr.AssistantText,
		Summary:       dr.Summary,
		HasDecision:   dr.HasDecision,
		SkipReason:    dr.SkipReason,
		DenoisedAt:    time.Now().UTC(),
		DenoiseModel:  denoiseModel,
	}
}
