package proxy

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/caffeaun/marc/internal/jsonl"
)

// buildCaptureEvent constructs a CaptureEvent from the fields accumulated
// during a single proxy request.
//
// requestBody  – raw bytes the client sent to the proxy.
// responseBody – raw bytes received from the upstream (SSE or JSON body).
// statusCode   – HTTP status code from the upstream response.
// req          – the original incoming http.Request (for method, path, headers).
// machine      – from proxy Config.
// meta         – streaming metadata; nil for non-streaming requests.
// upstreamErr  – non-nil when the upstream returned a non-2xx response.
func buildCaptureEvent(
	requestBody []byte,
	responseBody []byte,
	statusCode int,
	req *http.Request,
	machine string,
	meta *jsonl.StreamMeta,
	upstreamErr *jsonl.EventError,
) (jsonl.CaptureEvent, error) {
	eventID, err := jsonl.NewUUIDv4()
	if err != nil {
		return jsonl.CaptureEvent{}, err
	}

	// Validate request body as JSON; if not valid JSON (e.g. empty), use null.
	reqJSON := makeRawMessage(requestBody)
	respJSON := makeRawMessage(responseBody)

	ev := jsonl.CaptureEvent{
		EventID:     eventID,
		Machine:     machine,
		CapturedAt:  time.Now().UTC(),
		Source:      "anthropic_api",
		RequestID:   req.Header.Get("request-id"),
		Method:      req.Method,
		Path:        req.URL.Path,
		IsInternal:  req.Header.Get("X-Marc-Internal") == "true",
		Request:     reqJSON,
		Response:    respJSON,
		StreamMeta:  meta,
		Error:       upstreamErr,
		SessionHint: nil, // populated by T015
		ProjectHint: nil, // populated by T015
	}

	return ev, nil
}

// makeRawMessage returns b as a json.RawMessage.
// If b is nil or not valid JSON it returns json.RawMessage("null") so the
// event always contains valid JSON in the request/response fields.
func makeRawMessage(b []byte) json.RawMessage {
	if len(b) == 0 || !json.Valid(b) {
		return json.RawMessage("null")
	}
	out := make(json.RawMessage, len(b))
	copy(out, b)
	return out
}

// errorFromStatus returns a non-nil *EventError when status is not 2xx.
// The error message is extracted from the response body if it is valid JSON;
// otherwise the raw bytes are used as the message.
func errorFromStatus(status int, body []byte) *jsonl.EventError {
	if status >= 200 && status < 300 {
		return nil
	}
	msg := http.StatusText(status)
	// Try to extract a meaningful message from a JSON error body.
	var parsed struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Valid(body) {
		if jsonErr := json.Unmarshal(body, &parsed); jsonErr == nil && parsed.Error.Message != "" {
			msg = parsed.Error.Message
		}
	}
	errType := parsed.Error.Type
	if errType == "" {
		errType = "upstream_error"
	}
	return &jsonl.EventError{
		Type:    errType,
		Message: msg,
	}
}
