//go:build integration

package ollama_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/caffeaun/marc/internal/config"
	"github.com/caffeaun/marc/internal/ollama"
)

const (
	integrationEndpoint = "http://127.0.0.1:11434"
	integrationModel    = "qwen3:8b"
)

// isOllamaReachable performs a lightweight /api/tags check. Returns false if
// the server is unreachable, which causes the integration tests to be skipped.
func isOllamaReachable(t *testing.T) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, integrationEndpoint+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// TestIntegration_DenoiseRealOllama calls the real qwen3:8b model and asserts
// that a non-empty, parseable DenoiseResult is returned.
//
// Run with: go test -tags integration ./internal/ollama/...
func TestIntegration_DenoiseRealOllama(t *testing.T) {
	if !isOllamaReachable(t) {
		t.Skip("Ollama not reachable at 127.0.0.1:11434 — skipping integration test")
	}

	cfg := config.OllamaConfig{
		Endpoint:     integrationEndpoint,
		DenoiseModel: integrationModel,
	}
	c := ollama.New(cfg)
	defer c.Close()

	// Minimal raw event that gives the model enough context to fill the fields.
	rawEvent := `{"user": "Should I use a map or a struct here?", "assistant": "A struct is better when fields are known at compile time."}`

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	result, err := c.Denoise(ctx, integrationModel, rawEvent)
	if err != nil {
		t.Fatalf("Denoise: %v", err)
	}

	t.Logf("DenoiseResult from %s: user_text=%q assistant_text=%q summary=%q has_decision=%v skip_reason=%q",
		integrationModel,
		result.UserText,
		result.AssistantText,
		result.Summary,
		result.HasDecision,
		result.SkipReason,
	)

	// With the production prompt (T019), the model must produce at least one
	// non-empty text field given a real conversation exchange.
	if result.UserText == "" && result.AssistantText == "" && result.Summary == "" {
		t.Error("all text fields are empty; the model is not following the denoise prompt")
	}
}

// TestIntegration_PingRealOllama calls /api/tags on the real Ollama and
// verifies that qwen3:8b is listed.
func TestIntegration_PingRealOllama(t *testing.T) {
	if !isOllamaReachable(t) {
		t.Skip("Ollama not reachable at 127.0.0.1:11434 — skipping integration test")
	}

	cfg := config.OllamaConfig{
		Endpoint:     integrationEndpoint,
		DenoiseModel: integrationModel,
	}
	c := ollama.New(cfg)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := c.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

// TestIntegration_StreamFalseProducesValidJSON directly calls /api/generate
// with stream:false and verifies the response is a single JSON object (not
// newline-delimited streaming chunks). This is a belt-and-suspenders check
// that the Ollama server behaviour matches our assumption.
func TestIntegration_StreamFalseProducesValidJSON(t *testing.T) {
	if !isOllamaReachable(t) {
		t.Skip("Ollama not reachable at 127.0.0.1:11434 — skipping integration test")
	}

	body := strings.NewReader(`{"model":"qwen3:8b","prompt":"Return JSON: {\"foo\":\"bar\"}","stream":false,"format":"json"}`)
	req, err := http.NewRequest(http.MethodPost, integrationEndpoint+"/api/generate", body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	t.Logf("raw Ollama /api/generate response (stream:false): %s", string(raw))

	// A single JSON object must decode cleanly into a map.
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("response is not a single JSON object: %v\nbody: %s", err, string(raw))
	}

	if _, ok := obj["response"]; !ok {
		t.Errorf("JSON object missing 'response' field; got keys: %v", mapKeys(obj))
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
