package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

const sampleSSEMessage = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-4-7","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":12}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" marc"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}

event: message_stop
data: {"type":"message_stop"}

`

func TestAggregateSSE_TextStream(t *testing.T) {
	out := aggregateSSE([]byte(sampleSSEMessage))
	if len(out) == 0 {
		t.Fatal("aggregateSSE returned empty for a valid stream")
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("aggregated response is not valid JSON: %v\nbody: %s", err, string(out))
	}
	if got := resp["model"]; got != "claude-opus-4-7" {
		t.Errorf("model = %v, want claude-opus-4-7", got)
	}
	if got := resp["stop_reason"]; got != "end_turn" {
		t.Errorf("stop_reason = %v, want end_turn", got)
	}
	content, _ := resp["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content = %d blocks, want 1; full=%s", len(content), string(out))
	}
	block, _ := content[0].(map[string]any)
	if block["type"] != "text" {
		t.Errorf("block.type = %v, want text", block["type"])
	}
	if got, _ := block["text"].(string); got != "hello marc" {
		t.Errorf("block.text = %q, want %q", got, "hello marc")
	}
	usage, _ := resp["usage"].(map[string]any)
	if got, _ := usage["input_tokens"].(float64); got != 12 {
		t.Errorf("usage.input_tokens = %v, want 12", got)
	}
	if got, _ := usage["output_tokens"].(float64); got != 2 {
		t.Errorf("usage.output_tokens = %v, want 2", got)
	}
}

// TestAggregateSSE_NoTrailingBlankLine — simulates the format streamSSE
// produces when the upstream doesn't emit a final blank line. The aggregator
// must still flush the final event on EOF.
func TestAggregateSSE_NoTrailingBlankLine(t *testing.T) {
	stream := strings.TrimRight(sampleSSEMessage, "\n")
	out := aggregateSSE([]byte(stream))
	if len(out) == 0 {
		t.Fatal("no trailing blank line: aggregateSSE returned empty")
	}
}

// TestAggregateSSE_StreamSSEFormat — replicate exactly what streamSSE writes
// (every line gets a single \n appended, blank lines included). This is the
// canonical input the aggregator sees in production.
func TestAggregateSSE_StreamSSEFormat(t *testing.T) {
	// streamSSE strips the upstream's "\r" by virtue of bufio.Scanner using
	// ScanLines, so blank lines appear as "" between events with single \n.
	// Build the exact byte stream streamSSE would accumulate.
	stream := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"opus\",\"content\":[],\"usage\":{\"input_tokens\":1}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	out := aggregateSSE([]byte(stream))
	if len(out) == 0 {
		t.Fatalf("streamSSE-format input produced empty aggregation\ninput=%q", stream)
	}
	var resp map[string]any
	_ = json.Unmarshal(out, &resp)
	content, _ := resp["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("no content blocks: %s", string(out))
	}
	block := content[0].(map[string]any)
	if block["text"] != "hi" {
		t.Errorf("block.text = %v, want hi", block["text"])
	}
}
