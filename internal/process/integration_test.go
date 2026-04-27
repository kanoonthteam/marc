//go:build integration

package process

import (
	"context"
	"crypto/md5" //nolint:gosec // MD5 used for MinIO ETag matching per S3 protocol
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	miniogo "github.com/minio/minio-go/v7"
	miniocreds "github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/google/uuid"

	"github.com/caffeaun/marc/internal/clickhouse"
	"github.com/caffeaun/marc/internal/config"
	"github.com/caffeaun/marc/internal/minioclient"
	"github.com/caffeaun/marc/internal/ollama"
	"github.com/caffeaun/marc/internal/sqlitedb"
)

// Integration test service addresses — match the real services on this host.
const (
	integrationMinIOEndpoint = "http://127.0.0.1:9000"
	integrationMinIOBucket   = "marc"
	integrationCHAddr        = "127.0.0.1:19000"
	integrationCHDatabase    = "marc"
	integrationOllamaURL     = "http://127.0.0.1:11434"
	integrationOllamaModel   = "qwen3:8b"
	integrationMachine       = "test"
)

// minioCredentials returns the MinIO access/secret key.
// Priority: MINIO_ACCESS_KEY / MINIO_SECRET_KEY env vars, then known local values.
func minioCredentials() (access, secret string) {
	access = os.Getenv("MINIO_ACCESS_KEY")
	secret = os.Getenv("MINIO_SECRET_KEY")
	if access != "" && secret != "" {
		return access, secret
	}
	// Fallback to the local mc alias "local" credentials found on this host.
	return "admin", "Iv3GtjVlsiwwRX0uhj4kEA2Wd0Is5rOObL9Pogfj39c="
}

// ensureMinIOBucket creates bucket in MinIO if it does not already exist.
// It uses minio-go directly so it can call MakeBucket, which is not part of
// the minioclient.Client interface (that interface is intentionally minimal).
func ensureMinIOBucket(ctx context.Context, endpoint, bucket, accessKey, secretKey string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("parse endpoint: %w", err)
	}
	secure := u.Scheme == "https"
	host := u.Host
	if host == "" {
		host = endpoint
	}
	raw, err := miniogo.New(host, &miniogo.Options{
		Creds:  miniocreds.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
	})
	if err != nil {
		return fmt.Errorf("minio-go new: %w", err)
	}
	exists, err := raw.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("bucket exists check: %w", err)
	}
	if exists {
		return nil
	}
	return raw.MakeBucket(ctx, bucket, miniogo.MakeBucketOptions{})
}

// checkHTTPReachable returns true if a GET to url succeeds within 2 seconds with
// a non-5xx status.
func checkHTTPReachable(rawURL string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode < 500
}

// skipIfUnreachable calls t.Skip if any required service is unreachable.
func skipIfUnreachable(t *testing.T) {
	t.Helper()
	if !checkHTTPReachable(integrationMinIOEndpoint + "/minio/health/live") {
		t.Skip("MinIO not reachable at " + integrationMinIOEndpoint)
	}
	if !checkHTTPReachable("http://127.0.0.1:8123/ping") {
		t.Skip("ClickHouse not reachable at 127.0.0.1:8123")
	}
	if !checkHTTPReachable(integrationOllamaURL + "/api/tags") {
		t.Skip("Ollama not reachable at " + integrationOllamaURL)
	}
}

// md5Hex returns the lower-case hex-encoded MD5 of data.
func md5Hex(data []byte) string {
	//nolint:gosec // MD5 is used for ETag matching per S3 protocol, not security
	h := md5.New()
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// integrationEvent returns a realistic CaptureEvent as a JSON-serializable map.
func integrationEvent(eventID string, isInternal bool) map[string]any {
	return map[string]any{
		"event_id":     eventID,
		"machine":      integrationMachine,
		"captured_at":  time.Now().UTC().Format(time.RFC3339),
		"source":       "anthropic_api",
		"is_internal":  isInternal,
		"request":      json.RawMessage(`{"model":"claude-3-5-sonnet-20241022","system":"You are a helpful assistant for software project management.","messages":[{"role":"user","content":"How should I prioritize my backlog?"}]}`),
		"response":     json.RawMessage(`{"id":"msg_test","type":"message","role":"assistant","content":[{"type":"text","text":"Consider impact vs effort scoring."}],"model":"claude-3-5-sonnet-20241022","stop_reason":"end_turn","usage":{"input_tokens":25,"output_tokens":12}}`),
		"error":        nil,
		"session_hint": nil,
	}
}

// TestIntegrationProcessTick is the full end-to-end integration test.
//
// It drops a JSONL with one non-internal CaptureEvent and one is_internal=true
// event into marc/raw/test/YYYY/MM/DD/HH/test-1.jsonl, runs a single tick, and
// asserts:
//  1. One row in marc.events with the correct project_id stub.
//  2. Source object moved to marc/processed/raw-archive/...
//  3. Cursor advanced in SQLite.
func TestIntegrationProcessTick(t *testing.T) {
	skipIfUnreachable(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Open a temporary SQLite database for this test run.
	db, err := sqlitedb.Open(t.TempDir() + "/integration-state.db")
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	defer db.Close()

	// Construct the MinIO client.
	accessKey, secretKey := minioCredentials()
	mc, err := minioclient.New(minioclient.Config{
		Endpoint:  integrationMinIOEndpoint,
		Bucket:    integrationMinIOBucket,
		AccessKey: accessKey,
		SecretKey: secretKey,
		VerifyTLS: false,
	})
	if err != nil {
		t.Fatalf("minioclient.New: %v", err)
	}

	// Verify we can reach and authenticate to MinIO.
	// If the bucket does not exist, attempt to create it — this allows the test
	// to self-provision on a fresh MinIO instance without requiring manual setup.
	pingCtx, pingCancel := context.WithTimeout(ctx, 10*time.Second)
	pingErr := mc.Ping(pingCtx)
	pingCancel()
	if pingErr != nil {
		if !errors.Is(pingErr, minioclient.ErrBucketNotFound) {
			t.Skipf("MinIO ping failed (unreachable or wrong credentials): %v", pingErr)
		}
		// Bucket not found — create it using the minio-go client directly.
		t.Logf("bucket %q not found; attempting to create it...", integrationMinIOBucket)
		if err := ensureMinIOBucket(ctx, integrationMinIOEndpoint, integrationMinIOBucket, accessKey, secretKey); err != nil {
			t.Skipf("could not create MinIO bucket %q: %v", integrationMinIOBucket, err)
		}
		t.Logf("created bucket %q", integrationMinIOBucket)
	}

	// Construct the ClickHouse client.
	chClient, err := clickhouse.Connect(config.ClickHouseConfig{
		Addr:     integrationCHAddr,
		Database: integrationCHDatabase,
		User:     "default",
		Password: "",
	})
	if err != nil {
		t.Fatalf("clickhouse.Connect: %v", err)
	}
	defer chClient.Close()

	// Construct the Ollama client.
	ollamaClient := ollama.New(config.OllamaConfig{
		Endpoint:     integrationOllamaURL,
		DenoiseModel: integrationOllamaModel,
	})
	defer ollamaClient.Close()

	// Unique test key to avoid collisions with repeated runs.
	ts := time.Now().UTC()
	rawKey := fmt.Sprintf("raw/%s/%04d/%02d/%02d/%02d/test-1.jsonl",
		integrationMachine, ts.Year(), int(ts.Month()), ts.Day(), ts.Hour())

	// Use valid UUIDs so that buildEvent can parse them and the ClickHouse
	// event_id column value matches what we query for.
	externalEventID := uuid.New().String()
	internalEventID := uuid.New().String()

	events := []map[string]any{
		integrationEvent(externalEventID, false),
		integrationEvent(internalEventID, true),
	}

	// Build JSONL body and compute MD5 for the PUT ETag.
	var sb strings.Builder
	for _, ev := range events {
		b, _ := json.Marshal(ev)
		sb.Write(b)
		sb.WriteByte('\n')
	}
	body := []byte(sb.String())
	etag := md5Hex(body)

	putCtx, putCancel := context.WithTimeout(ctx, 15*time.Second)
	if err := mc.PutObject(putCtx, rawKey, strings.NewReader(string(body)), int64(len(body)), etag); err != nil {
		putCancel()
		t.Fatalf("put test JSONL to %s: %v", rawKey, err)
	}
	putCancel()
	t.Logf("uploaded test JSONL to %s (%d bytes)", rawKey, len(body))

	// Construct and run one daemon tick.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := &daemon{
		cfg:          &config.ServerConfig{MachineName: integrationMachine},
		db:           db,
		mc:           mc,
		ch:           chClient,
		ollama:       ollamaClient,
		stagingDir:   t.TempDir(),
		denoiseModel: integrationOllamaModel,
		machine:      integrationMachine,
		logger:       logger,
	}

	t.Log("running single tick (Ollama denoise may take 30–120s)...")
	d.tick(ctx)

	// --- Assert 1: one row in ClickHouse for the non-internal event ---
	queryCtx, queryCancel := context.WithTimeout(ctx, 15*time.Second)
	defer queryCancel()

	rows, err := chClient.QueryEvents(queryCtx,
		"SELECT event_id, project_id FROM marc.events WHERE machine = ? AND toString(event_id) = ?",
		integrationMachine, externalEventID)
	if err != nil {
		t.Fatalf("query clickhouse: %v", err)
	}
	t.Logf("rows inserted into ClickHouse: %d", len(rows))
	if len(rows) == 0 {
		t.Fatalf("expected >= 1 row in marc.events for event_id=%s, got 0", externalEventID)
	}

	projectID := fmt.Sprintf("%v", rows[0]["project_id"])
	t.Logf("project_id in ClickHouse row: %s", projectID)

	// Verify project_id matches the stub heuristic.
	expectedProjectID := projectIDFromRawRequest(
		`{"model":"claude-3-5-sonnet-20241022","system":"You are a helpful assistant for software project management.","messages":[{"role":"user","content":"How should I prioritize my backlog?"}]}`,
	)
	if projectID != expectedProjectID {
		t.Errorf("project_id = %q, want %q", projectID, expectedProjectID)
	}

	// Verify the internal event was NOT inserted into ClickHouse.
	internalRows, err := chClient.QueryEvents(queryCtx,
		"SELECT event_id FROM marc.events WHERE machine = ? AND toString(event_id) = ?",
		integrationMachine, internalEventID)
	if err != nil {
		t.Fatalf("query clickhouse for internal event: %v", err)
	}
	if len(internalRows) > 0 {
		t.Errorf("internal event was inserted into ClickHouse (must be skipped)")
	}

	// --- Assert 2: source object moved to archive ---
	archKey := archiveKey(rawKey)
	t.Logf("expected archive key: %s", archKey)

	listCtx, listCancel := context.WithTimeout(ctx, 10*time.Second)
	defer listCancel()

	archivePrefix := "processed/raw-archive/" + integrationMachine + "/"
	archiveKeys, err := mc.ListObjects(listCtx, archivePrefix, "")
	if err != nil {
		t.Fatalf("list archive prefix: %v", err)
	}
	foundArchive := false
	for _, k := range archiveKeys {
		if k == archKey {
			foundArchive = true
			break
		}
	}
	if !foundArchive {
		t.Errorf("archive key %q not found; archive keys: %v", archKey, archiveKeys)
	}
	t.Logf("object moved to archive: %s", archKey)

	// --- Assert 3: cursor advanced in SQLite ---
	cursor, err := db.GetCursor(ctx, integrationMachine)
	if err != nil {
		t.Fatalf("GetCursor: %v", err)
	}
	if cursor != rawKey {
		t.Errorf("cursor = %q, want %q", cursor, rawKey)
	}
	t.Logf("cursor advanced in SQLite: %s", cursor)

	t.Logf("=== integration test summary ===")
	t.Logf("  rows inserted: %d", len(rows))
	t.Logf("  archive key:   %s", archKey)
	t.Logf("  cursor value:  %s", cursor)
}
