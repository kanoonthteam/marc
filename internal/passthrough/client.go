// Package passthrough provides a denoiser that extracts structured fields from
// a raw capture event deterministically, without calling any LLM.
//
// It exists so the marc-process pipeline can keep ingesting captures into
// ClickHouse when the hosted denoise backend (MiniMax) is intentionally turned
// off for cost reasons. Instead of asking a model to summarise each exchange,
// it pulls the user's last message text and the assistant's reply straight out
// of the Anthropic request/response JSON, and flags "has_decision" with a cheap
// heuristic. Question generation (which runs on `claude -p`) then does the real
// quality filtering downstream, so a permissive, LLM-free extraction here is
// enough to keep the corpus fed at zero MiniMax spend.
//
// It implements ollama.Client so it drops into the same provider switch as the
// Ollama and MiniMax denoisers.
package passthrough

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/caffeaun/marc/internal/ollama"
)

// maxTextBytes caps each extracted field. A single tool_result (a big file read
// or long bash output) can be hundreds of KB; storing that verbatim would bloat
// ClickHouse rows and, worse, blow up the `claude -p` generation prompt (30
// events per cycle). The denoiser only needs enough text to convey what the
// user asked and what the assistant did, so we keep a generous head slice.
const maxTextBytes = 8192

// substantiveTextThreshold is the assistant_text length (bytes) above which an
// exchange is considered decision-bearing even when no tool was used — e.g. a
// substantive explanation or design rationale. Short replies with no tool use
// (acknowledgements, clarifying questions) are treated as non-decisions.
const substantiveTextThreshold = 160

type client struct{}

// New returns a passthrough denoiser. It holds no resources and never fails to
// construct, so callers need no configuration.
func New() ollama.Client { return &client{} }

// Denoise extracts UserText/AssistantText/HasDecision from a raw capture event
// without any network call. The model argument is ignored. It returns
// ollama.ErrUnparseableModelOutput when the event JSON itself cannot be parsed,
// so the process daemon treats it as a poison pill and advances the cursor
// rather than halting the batch.
func (c *client) Denoise(_ context.Context, _ string, rawEvent string) (*ollama.DenoiseResult, error) {
	var ev struct {
		Request  json.RawMessage `json:"request"`
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal([]byte(rawEvent), &ev); err != nil {
		return nil, ollama.ErrUnparseableModelOutput
	}

	userText := extractLastUserText(ev.Request)
	assistantText, tookAction := extractAssistantText(ev.Response)

	hasDecision := tookAction || len(strings.TrimSpace(assistantText)) > substantiveTextThreshold

	return &ollama.DenoiseResult{
		UserText:      clip(userText, maxTextBytes),
		AssistantText: clip(assistantText, maxTextBytes),
		HasDecision:   hasDecision,
	}, nil
}

// Ping always succeeds — there is no backend to reach.
func (c *client) Ping(_ context.Context) error { return nil }

// Close is a no-op; the passthrough client holds no resources.
func (c *client) Close() error { return nil }

// extractLastUserText returns the concatenated text of the last user-role
// message in the request. Anthropic message content is either a plain string
// or an array of content blocks (text, tool_result, ...); both are handled.
func extractLastUserText(reqRaw json.RawMessage) string {
	if len(reqRaw) == 0 {
		return ""
	}
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(reqRaw, &req); err != nil {
		return ""
	}
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			return contentToText(req.Messages[i].Content)
		}
	}
	// No explicit user role found — fall back to the last message of any role.
	if n := len(req.Messages); n > 0 {
		return contentToText(req.Messages[n-1].Content)
	}
	return ""
}

// extractAssistantText returns the assistant reply text from a response body
// and whether the assistant took an action (used a tool). tookAction is true
// when the response contains a tool_use block or its stop_reason is "tool_use".
func extractAssistantText(respRaw json.RawMessage) (text string, tookAction bool) {
	if len(respRaw) == 0 {
		return "", false
	}
	var resp struct {
		StopReason string          `json:"stop_reason"`
		Content    json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(respRaw, &resp); err != nil {
		return "", false
	}
	if resp.StopReason == "tool_use" {
		tookAction = true
	}
	text, usedTool := blocksToText(resp.Content)
	return text, tookAction || usedTool
}

// contentToText renders Anthropic message content (string or block array) to a
// plain-text string. tool_use blocks are ignored here (their presence is
// tracked separately for the decision heuristic on the assistant side).
func contentToText(raw json.RawMessage) string {
	text, _ := blocksToText(raw)
	return text
}

// blocksToText renders content that may be a JSON string or an array of content
// blocks. It concatenates text from "text" and "tool_result" blocks and reports
// whether any "tool_use" block was present.
func blocksToText(raw json.RawMessage) (text string, usedTool bool) {
	if len(raw) == 0 {
		return "", false
	}
	// Plain string content.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, false
	}

	var blocks []struct {
		Type    string          `json:"type"`
		Text    string          `json:"text"`
		Content json.RawMessage `json:"content"` // tool_result nests string or blocks
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", false
	}

	var b strings.Builder
	for _, blk := range blocks {
		switch blk.Type {
		case "text":
			if blk.Text != "" {
				appendLine(&b, blk.Text)
			}
		case "tool_result":
			if nested, _ := blocksToText(blk.Content); nested != "" {
				appendLine(&b, nested)
			}
		case "tool_use":
			usedTool = true
		}
		// Stop growing the buffer once we have plenty; the caller clips anyway.
		if b.Len() >= maxTextBytes {
			break
		}
	}
	return strings.TrimSpace(b.String()), usedTool
}

func appendLine(b *strings.Builder, s string) {
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
	b.WriteString(s)
}

// clip truncates s to at most n bytes without splitting a UTF-8 rune.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Back off to a rune boundary.
	for n > 0 && !utf8RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// utf8RuneStart reports whether byte b is the first byte of a UTF-8 rune
// (i.e. not a continuation byte 0b10xxxxxx).
func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }
