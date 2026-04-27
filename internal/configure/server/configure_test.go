package server_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/caffeaun/marc/internal/clickhouse"
	"github.com/caffeaun/marc/internal/config"
	"github.com/caffeaun/marc/internal/configure/server"
	"github.com/caffeaun/marc/internal/minioclient"
	"github.com/caffeaun/marc/internal/ollama"
)

// ---- test doubles --------------------------------------------------------

// fakeMinioOK is a minioclient.Client whose Ping always succeeds.
type fakeMinioOK struct{ minioclient.Client }

func (f *fakeMinioOK) Ping(_ context.Context) error       { return nil }
func (f *fakeMinioOK) PutObject(_ context.Context, _ string, _ io.Reader, _ int64, _ string) error {
	return nil
}
func (f *fakeMinioOK) GetObject(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (f *fakeMinioOK) MoveObject(_ context.Context, _, _ string) error { return nil }
func (f *fakeMinioOK) ListObjects(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}

// fakeMinioFail is a minioclient.Client whose Ping always fails.
type fakeMinioFail struct {
	fakeMinioOK
	err error
}

func (f *fakeMinioFail) Ping(_ context.Context) error { return f.err }

// fakeClickHouseOK is a clickhouse.Client whose Ping always succeeds.
type fakeClickHouseOK struct{}

func (f *fakeClickHouseOK) InsertEvent(_ context.Context, _ clickhouse.Event) error { return nil }
func (f *fakeClickHouseOK) QueryEvents(_ context.Context, _ string, _ ...any) ([]map[string]any, error) {
	return nil, nil
}
func (f *fakeClickHouseOK) Exec(_ context.Context, _ string, _ ...any) error { return nil }
func (f *fakeClickHouseOK) Ping(_ context.Context) error                     { return nil }
func (f *fakeClickHouseOK) Close() error                                     { return nil }

// fakeClickHouseFail is a clickhouse.Client whose Ping always fails.
type fakeClickHouseFail struct {
	fakeClickHouseOK
	err error
}

func (f *fakeClickHouseFail) Ping(_ context.Context) error { return f.err }

// fakeOllamaOK is an ollama.Client whose Ping always succeeds.
type fakeOllamaOK struct{}

func (f *fakeOllamaOK) Denoise(_ context.Context, _, _ string) (*ollama.DenoiseResult, error) {
	return &ollama.DenoiseResult{}, nil
}
func (f *fakeOllamaOK) Ping(_ context.Context) error { return nil }
func (f *fakeOllamaOK) Close() error                  { return nil }

// fakeOllamaFail is an ollama.Client whose Ping always fails.
type fakeOllamaFail struct {
	fakeOllamaOK
	err error
}

func (f *fakeOllamaFail) Ping(_ context.Context) error { return f.err }

// ---- constructor helpers --------------------------------------------------

func newMinioOK(_ minioclient.Config) (minioclient.Client, error) {
	return &fakeMinioOK{}, nil
}

func newMinioFail(err error) func(minioclient.Config) (minioclient.Client, error) {
	return func(_ minioclient.Config) (minioclient.Client, error) {
		return &fakeMinioFail{err: err}, nil
	}
}

func newCHOK(_ config.ClickHouseConfig) (clickhouse.Client, error) {
	return &fakeClickHouseOK{}, nil
}

func newCHFail(err error) func(config.ClickHouseConfig) (clickhouse.Client, error) {
	return func(_ config.ClickHouseConfig) (clickhouse.Client, error) {
		return &fakeClickHouseFail{err: err}, nil
	}
}

func newOllamaOK(_ config.OllamaConfig) ollama.Client { return &fakeOllamaOK{} }

func newOllamaFail(err error) func(config.OllamaConfig) ollama.Client {
	return func(_ config.OllamaConfig) ollama.Client { return &fakeOllamaFail{err: err} }
}

func telegramOK(_ context.Context, _ string) error { return nil }

func telegramFail(err error) func(_ context.Context, _ string) error {
	return func(_ context.Context, _ string) error { return err }
}

// ---- helpers for building test Options ------------------------------------

// allServicesOK returns Options with all service fakes set to succeed.
func allServicesOK(path string) server.Options {
	return server.Options{
		Path:              path,
		Reader:            strings.NewReader(""),
		Writer:            io.Discard,
		NewMinioClient:    newMinioOK,
		NewClickHouseConn: newCHOK,
		NewOllamaClient:   newOllamaOK,
		TelegramGetMe:     telegramOK,
	}
}

// tmpPath returns a file path inside a fresh temp directory.
func tmpPath(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("%s/server.toml", t.TempDir())
}

// minimalStdin returns a strings.Reader that provides enough input to answer
// all interactive prompts using default values (empty lines).
func minimalStdin() io.Reader {
	// One blank line per prompt — there are ~20 prompts; 30 gives headroom.
	return strings.NewReader(strings.Repeat("\n", 30))
}

// fullStdin returns input that fills in required fields (telegram token + chat_id)
// while leaving everything else as defaults.
func fullStdin() io.Reader {
	// Order must match collectFields prompt order.
	fields := []string{
		"test-server",       // machine_name
		"",                  // minio.endpoint (default)
		"",                  // minio.bucket (default)
		"ACCESSKEY",         // minio.access_key
		"SECRETKEY",         // minio.secret_key
		"",                  // minio.verify_tls (default)
		"",                  // minio.staging_dir (default)
		"127.0.0.1:9000",   // clickhouse.addr
		"",                  // clickhouse.database (default)
		"",                  // clickhouse.user (default)
		"",                  // clickhouse.password (default)
		"",                  // sqlite.path (default)
		"",                  // ollama.endpoint (default)
		"",                  // ollama.denoise_model (default)
		"",                  // claude.binary (default)
		"",                  // claude.internal_header (default)
		"",                  // scheduler.question_gen_cron (default)
		"",                  // scheduler.telegram_send_cron (default)
		"",                  // scheduler.timezone (default)
		"",                  // scheduler.events_per_generation (default)
		"BOTTOKEN",          // telegram.bot_token
		"123456789",         // telegram.chat_id
		"",                  // filtering.min_durability (default)
		"",                  // filtering.max_obviousness (default)
	}
	return strings.NewReader(strings.Join(fields, "\n") + "\n")
}

// ---- tests ----------------------------------------------------------------

// TestPrintDefault verifies that --print-default emits the full server TOML template.
func TestPrintDefault(t *testing.T) {
	var out bytes.Buffer
	opts := server.Options{
		PrintDefault: true,
		Writer:       &out,
		Reader:       strings.NewReader(""),
	}
	if err := server.Run(context.Background(), opts); err != nil {
		t.Fatalf("Run(PrintDefault): %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"machine_name",
		"[minio]",
		"[clickhouse]",
		"[sqlite]",
		"[ollama]",
		"[claude]",
		"[scheduler]",
		"[telegram]",
		"[filtering]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in --print-default output; got:\n%s", want, got)
		}
	}
}

// TestFileModeIs0600 verifies that the written config file has mode 0600.
func TestFileModeIs0600(t *testing.T) {
	path := tmpPath(t)
	opts := allServicesOK(path)
	opts.Reader = fullStdin()
	var out bytes.Buffer
	opts.Writer = &out

	if err := server.Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v\noutput: %s", err, out.String())
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected mode 0600, got %04o", info.Mode().Perm())
	}
}

// TestOverwriteConfirmation verifies that re-running prompts for confirmation
// when the file already exists, and aborts on "n".
func TestOverwriteConfirmation(t *testing.T) {
	path := tmpPath(t)

	// Write the file first.
	opts := allServicesOK(path)
	opts.Reader = fullStdin()
	opts.Writer = io.Discard
	if err := server.Run(context.Background(), opts); err != nil {
		t.Fatalf("initial write: %v", err)
	}

	// Re-run with "n" — must NOT overwrite.
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	opts2 := allServicesOK(path)
	opts2.Reader = strings.NewReader("n\n")
	opts2.Writer = &out
	if err := server.Run(context.Background(), opts2); err != nil {
		t.Fatalf("Run (abort): %v", err)
	}

	// File must be unchanged.
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != string(original) {
		t.Error("file was modified despite answering 'n' to overwrite prompt")
	}
	if !strings.Contains(out.String(), "Aborted") {
		t.Errorf("expected 'Aborted' in output; got: %s", out.String())
	}
}

// TestCheckModePassesAllServices verifies --check prints [ok] per service when all pass.
func TestCheckModePassesAllServices(t *testing.T) {
	path := tmpPath(t)

	// Write a minimal valid config file with mode 0600.
	tomlContent := `machine_name = "test-server"
[minio]
endpoint = "http://127.0.0.1:9000"
bucket = "marc"
access_key = "KEY"
secret_key = "SECRET"
verify_tls = false
staging_dir = "/tmp/staging"
[clickhouse]
addr = "127.0.0.1:9000"
database = "marc"
user = "default"
password = ""
[sqlite]
path = "/tmp/state.db"
[ollama]
endpoint = "http://127.0.0.1:11434"
denoise_model = "qwen3:8b"
[claude]
binary = "claude"
internal_header = "X-Marc-Internal"
[scheduler]
question_gen_cron = "0 * * * *"
telegram_send_cron = "*/30 9-18 * * 1-5"
timezone = "Asia/Bangkok"
events_per_generation = 30
[telegram]
bot_token = "TESTTOKEN"
chat_id = 123456789
[filtering]
min_durability = 7
max_obviousness = 7
`
	if err := os.WriteFile(path, []byte(tomlContent), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	opts := server.Options{
		Path:              path,
		Check:             true,
		Writer:            &out,
		Reader:            strings.NewReader(""),
		NewMinioClient:    newMinioOK,
		NewClickHouseConn: newCHOK,
		NewOllamaClient:   newOllamaOK,
		TelegramGetMe:     telegramOK,
	}
	if err := server.Run(context.Background(), opts); err != nil {
		t.Fatalf("--check with all ok: %v\noutput: %s", err, out.String())
	}

	output := out.String()
	for _, svc := range []string{"minio", "clickhouse", "ollama", "telegram"} {
		if !strings.Contains(output, "[ok] "+svc) {
			t.Errorf("expected '[ok] %s' in output; got:\n%s", svc, output)
		}
	}
}

// TestCheckModeNamedFailure verifies that --check names the failing service.
func TestCheckModeNamedFailure(t *testing.T) {
	path := tmpPath(t)

	tomlContent := `machine_name = "test-server"
[minio]
endpoint = "http://127.0.0.1:9000"
bucket = "marc"
access_key = "KEY"
secret_key = "SECRET"
verify_tls = false
staging_dir = "/tmp/staging"
[clickhouse]
addr = "127.0.0.1:9000"
database = "marc"
user = "default"
password = ""
[sqlite]
path = "/tmp/state.db"
[ollama]
endpoint = "http://127.0.0.1:11434"
denoise_model = "qwen3:8b"
[claude]
binary = "claude"
internal_header = "X-Marc-Internal"
[scheduler]
question_gen_cron = "0 * * * *"
telegram_send_cron = "*/30 9-18 * * 1-5"
timezone = "Asia/Bangkok"
events_per_generation = 30
[telegram]
bot_token = "BADTOKEN"
chat_id = 123456789
[filtering]
min_durability = 7
max_obviousness = 7
`
	if err := os.WriteFile(path, []byte(tomlContent), 0o600); err != nil {
		t.Fatal(err)
	}

	telegramErr := errors.New("Unauthorized")
	var out bytes.Buffer
	opts := server.Options{
		Path:              path,
		Check:             true,
		Writer:            &out,
		Reader:            strings.NewReader(""),
		NewMinioClient:    newMinioOK,
		NewClickHouseConn: newCHOK,
		NewOllamaClient:   newOllamaOK,
		TelegramGetMe:     telegramFail(fmt.Errorf("telegram: getMe failed: %w", telegramErr)),
	}
	err := server.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected non-nil error when telegram fails")
	}

	output := out.String()
	// Telegram must be named in the output.
	if !strings.Contains(output, "[fail] telegram") {
		t.Errorf("expected '[fail] telegram' in output; got:\n%s", output)
	}
	// Other services must pass.
	for _, svc := range []string{"minio", "clickhouse", "ollama"} {
		if !strings.Contains(output, "[ok] "+svc) {
			t.Errorf("expected '[ok] %s' in output; got:\n%s", svc, output)
		}
	}
	// Error must name "telegram".
	if !strings.Contains(err.Error(), "telegram") && !strings.Contains(output, "telegram") {
		t.Errorf("expected 'telegram' to be named in failure output; err=%v out=%s", err, output)
	}
}

// TestValidationFanOut verifies that each service name appears in output
// independently, and a per-service failure is surfaced correctly.
func TestValidationFanOut(t *testing.T) {
	tests := []struct {
		name           string
		minioClient    func(minioclient.Config) (minioclient.Client, error)
		chConn         func(config.ClickHouseConfig) (clickhouse.Client, error)
		ollamaClient   func(config.OllamaConfig) ollama.Client
		telegramGetMe  func(context.Context, string) error
		failedService  string
		expectErr      bool
	}{
		{
			name:          "all pass",
			minioClient:   newMinioOK,
			chConn:        newCHOK,
			ollamaClient:  newOllamaOK,
			telegramGetMe: telegramOK,
			expectErr:     false,
		},
		{
			name:          "minio fails",
			minioClient:   newMinioFail(errors.New("minio: bucket not found")),
			chConn:        newCHOK,
			ollamaClient:  newOllamaOK,
			telegramGetMe: telegramOK,
			failedService: "minio",
			expectErr:     true,
		},
		{
			name:          "clickhouse fails",
			minioClient:   newMinioOK,
			chConn:        newCHFail(errors.New("clickhouse: connection refused")),
			ollamaClient:  newOllamaOK,
			telegramGetMe: telegramOK,
			failedService: "clickhouse",
			expectErr:     true,
		},
		{
			name:          "ollama fails",
			minioClient:   newMinioOK,
			chConn:        newCHOK,
			ollamaClient:  newOllamaFail(errors.New("ollama: model not loaded")),
			telegramGetMe: telegramOK,
			failedService: "ollama",
			expectErr:     true,
		},
		{
			name:          "telegram fails",
			minioClient:   newMinioOK,
			chConn:        newCHOK,
			ollamaClient:  newOllamaOK,
			telegramGetMe: telegramFail(errors.New("telegram: getMe failed: Unauthorized")),
			failedService: "telegram",
			expectErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tmpPath(t)
			var out bytes.Buffer

			opts := server.Options{
				Path:              path,
				Reader:            fullStdin(),
				Writer:            &out,
				NewMinioClient:    tt.minioClient,
				NewClickHouseConn: tt.chConn,
				NewOllamaClient:   tt.ollamaClient,
				TelegramGetMe:     tt.telegramGetMe,
			}

			err := server.Run(context.Background(), opts)
			output := out.String()

			if tt.expectErr && err == nil {
				t.Fatalf("expected error for %q, got nil; output:\n%s", tt.name, output)
			}
			if !tt.expectErr && err != nil {
				t.Fatalf("unexpected error for %q: %v; output:\n%s", tt.name, err, output)
			}

			if tt.failedService != "" {
				if !strings.Contains(output, "[fail] "+tt.failedService) {
					t.Errorf("expected '[fail] %s' in output; got:\n%s", tt.failedService, output)
				}
			}

			// The TOML must be written even when validation fails.
			if tt.expectErr {
				if _, statErr := os.Stat(path); statErr != nil {
					t.Errorf("TOML should exist even after validation failure; stat err: %v", statErr)
				}
			}
		})
	}
}

// TestErrorMessageNamesFailingService ensures the returned error mentions the
// failing service name.
func TestErrorMessageNamesFailingService(t *testing.T) {
	path := tmpPath(t)
	var out bytes.Buffer

	opts := server.Options{
		Path:              path,
		Reader:            fullStdin(),
		Writer:            &out,
		NewMinioClient:    newMinioFail(errors.New("bucket missing")),
		NewClickHouseConn: newCHOK,
		NewOllamaClient:   newOllamaOK,
		TelegramGetMe:     telegramOK,
	}

	err := server.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected non-nil error when minio fails")
	}

	output := out.String()
	if !strings.Contains(output, "minio") {
		t.Errorf("expected 'minio' named in failure output; got:\n%s", output)
	}
}
