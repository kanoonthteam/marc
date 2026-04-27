package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
)

// aggregateSSE walks an Anthropic SSE stream and assembles a synthesized
// non-streaming-shaped response JSON object: {type:"message", id, role, model,
// content:[...], stop_reason, stop_sequence, usage:{...}}. This is the value
// the proxy places into the JSONL event's `response` field so downstream
// (denoise, generate) can read assistant text uniformly regardless of whether
// the original request was streamed.
//
// It returns nil if the stream never emitted a message_start (i.e. nothing to
// aggregate); the caller falls back to the raw bytes.
//
// Block-type coverage:
//   - text         — content_block_delta.delta.text appended to .text
//   - thinking     — content_block_delta.delta.thinking appended to .thinking
//   - tool_use     — input_json_delta.partial_json concatenated; on
//                    content_block_stop it is parsed back into .input
//
// Anything unknown is preserved as-is from content_block_start.
func aggregateSSE(rawSSE []byte) []byte {
	if len(rawSSE) == 0 {
		return nil
	}

	scanner := bufio.NewScanner(bytes.NewReader(rawSSE))
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)

	var (
		response       map[string]any
		blocks         []map[string]any
		toolPartials   map[int]*bytes.Buffer
		currentEvent   string
		dataBuf        bytes.Buffer
		started        bool
	)
	toolPartials = make(map[int]*bytes.Buffer)

	flush := func() {
		if !started {
			currentEvent = ""
			dataBuf.Reset()
			return
		}
		processSSEEvent(currentEvent, dataBuf.Bytes(), &response, &blocks, toolPartials)
		started = false
		currentEvent = ""
		dataBuf.Reset()
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			flush()
			continue
		}
		// SSE separator inside multi-line data fields: lines beginning with "data:"
		// concatenate; "event:" sets the type. Anything else (e.g. ":" comments)
		// is ignored.
		switch {
		case bytes.HasPrefix(line, []byte("event: ")):
			currentEvent = string(bytes.TrimPrefix(line, []byte("event: ")))
			started = true
		case bytes.HasPrefix(line, []byte("data: ")):
			data := bytes.TrimPrefix(line, []byte("data: "))
			if dataBuf.Len() > 0 {
				dataBuf.WriteByte('\n')
			}
			dataBuf.Write(data)
			started = true
		}
	}
	// Flush trailing event without blank-line terminator.
	flush()

	if response == nil {
		return nil
	}

	// Finalize tool_use partial_json into structured input.
	for i, buf := range toolPartials {
		if i >= len(blocks) {
			continue
		}
		block := blocks[i]
		if block == nil {
			continue
		}
		var parsed any
		if err := json.Unmarshal(buf.Bytes(), &parsed); err == nil {
			block["input"] = parsed
		}
	}

	// Attach the assembled blocks as the response content array.
	contentSlice := make([]any, 0, len(blocks))
	for _, b := range blocks {
		if b != nil {
			contentSlice = append(contentSlice, b)
		}
	}
	response["content"] = contentSlice

	// Mirror stop_reason / stop_sequence at top level (already set by message_delta).
	out, err := json.Marshal(response)
	if err != nil {
		return nil
	}
	return out
}

// processSSEEvent dispatches a single SSE event to the response/blocks state.
// It tolerates malformed JSON in a single event without aborting the aggregation.
func processSSEEvent(
	eventType string,
	data []byte,
	response *map[string]any,
	blocks *[]map[string]any,
	toolPartials map[int]*bytes.Buffer,
) {
	if len(data) == 0 {
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return
	}

	switch eventType {
	case "message_start":
		msg, ok := payload["message"].(map[string]any)
		if !ok {
			return
		}
		// Copy the message envelope; we'll overwrite content + usage as we go.
		*response = msg

	case "content_block_start":
		idx, _ := payload["index"].(float64)
		block, _ := payload["content_block"].(map[string]any)
		ensureBlocks(blocks, int(idx)+1)
		(*blocks)[int(idx)] = block
		// Tool-use blocks accumulate input_json_delta strings.
		if block != nil && block["type"] == "tool_use" {
			toolPartials[int(idx)] = &bytes.Buffer{}
		}

	case "content_block_delta":
		idx, _ := payload["index"].(float64)
		delta, ok := payload["delta"].(map[string]any)
		if !ok {
			return
		}
		ensureBlocks(blocks, int(idx)+1)
		block := (*blocks)[int(idx)]
		if block == nil {
			block = map[string]any{}
			(*blocks)[int(idx)] = block
		}
		switch delta["type"] {
		case "text_delta":
			if t, ok := delta["text"].(string); ok {
				prev, _ := block["text"].(string)
				block["text"] = prev + t
			}
		case "thinking_delta":
			if t, ok := delta["thinking"].(string); ok {
				prev, _ := block["thinking"].(string)
				block["thinking"] = prev + t
			}
		case "input_json_delta":
			if pj, ok := delta["partial_json"].(string); ok {
				if buf, exists := toolPartials[int(idx)]; exists {
					buf.WriteString(pj)
				}
			}
		}

	case "message_delta":
		if *response == nil {
			return
		}
		if delta, ok := payload["delta"].(map[string]any); ok {
			if v, ok := delta["stop_reason"]; ok {
				(*response)["stop_reason"] = v
			}
			if v, ok := delta["stop_sequence"]; ok {
				(*response)["stop_sequence"] = v
			}
		}
		if usage, ok := payload["usage"].(map[string]any); ok {
			merged, _ := (*response)["usage"].(map[string]any)
			if merged == nil {
				merged = map[string]any{}
			}
			for k, v := range usage {
				merged[k] = v
			}
			(*response)["usage"] = merged
		}

	case "message_stop":
		// no-op — the aggregator finalizes after the loop.
	}
}

func ensureBlocks(blocks *[]map[string]any, n int) {
	for len(*blocks) < n {
		*blocks = append(*blocks, nil)
	}
}
