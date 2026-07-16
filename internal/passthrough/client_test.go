package passthrough

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/caffeaun/marc/internal/ollama"
)

func TestDenoise_ExtractsTextAndToolUseDecision(t *testing.T) {
	raw := `{
		"request": {
			"messages": [
				{"role": "user", "content": "help me refactor auth"},
				{"role": "assistant", "content": [{"type": "text", "text": "sure"}]},
				{"role": "user", "content": [
					{"type": "tool_result", "content": [{"type": "text", "text": "file contents here"}]}
				]}
			]
		},
		"response": {
			"stop_reason": "tool_use",
			"content": [
				{"type": "text", "text": "I'll edit the file."},
				{"type": "tool_use", "name": "Edit", "input": {}}
			]
		}
	}`

	c := New()
	dr, err := c.Denoise(context.Background(), "ignored", raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dr.UserText != "file contents here" {
		t.Errorf("UserText = %q, want the last user message text", dr.UserText)
	}
	if !strings.Contains(dr.AssistantText, "I'll edit the file.") {
		t.Errorf("AssistantText = %q, want assistant text block", dr.AssistantText)
	}
	if !dr.HasDecision {
		t.Error("HasDecision = false, want true (tool_use present)")
	}
}

func TestDenoise_PlainStringContent(t *testing.T) {
	raw := `{
		"request": {"messages": [{"role": "user", "content": "what is a mutex?"}]},
		"response": {"stop_reason": "end_turn", "content": [{"type": "text", "text": "A mutex is a mutual-exclusion primitive that guards a critical section so that only one goroutine holds it at a time; prefer sync.RWMutex when reads dominate, and always unlock via defer to avoid leaking the lock on an early return."}]}
	}`

	dr, err := New().Denoise(context.Background(), "", raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dr.UserText != "what is a mutex?" {
		t.Errorf("UserText = %q", dr.UserText)
	}
	if !dr.HasDecision {
		t.Error("HasDecision = false, want true (substantive assistant text over threshold)")
	}
}

func TestDenoise_ShortReplyNoToolIsNotDecision(t *testing.T) {
	raw := `{
		"request": {"messages": [{"role": "user", "content": "thanks"}]},
		"response": {"stop_reason": "end_turn", "content": [{"type": "text", "text": "You're welcome!"}]}
	}`

	dr, err := New().Denoise(context.Background(), "", raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dr.HasDecision {
		t.Error("HasDecision = true, want false (short reply, no tool use)")
	}
}

func TestDenoise_UnparseableIsPoisonPill(t *testing.T) {
	_, err := New().Denoise(context.Background(), "", "not json at all")
	if !errors.Is(err, ollama.ErrUnparseableModelOutput) {
		t.Fatalf("err = %v, want ErrUnparseableModelOutput so the batch advances", err)
	}
}

func TestDenoise_ClipsLargeText(t *testing.T) {
	big := strings.Repeat("x", 50_000)
	raw := `{"request":{"messages":[{"role":"user","content":"` + big + `"}]},"response":{"content":[]}}`

	dr, err := New().Denoise(context.Background(), "", raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dr.UserText) > maxTextBytes {
		t.Errorf("UserText length = %d, want <= %d (clipped)", len(dr.UserText), maxTextBytes)
	}
}

func TestPingAndClose(t *testing.T) {
	c := New()
	if err := c.Ping(context.Background()); err != nil {
		t.Errorf("Ping error: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close error: %v", err)
	}
}
