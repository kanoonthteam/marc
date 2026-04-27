package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTemp writes content to a file in dir with the given mode and returns the path.
func writeTemp(t *testing.T, dir, name, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("writeTemp: %v", err)
	}
	return path
}

// ---- ClientConfig tests -------------------------------------------------------

const validClientTOML = `
machine_name = "test-machine"

[paths]
capture_file = "/tmp/capture.jsonl"
log_file = "/tmp/marc.log"

[proxy]
listen_addr = "127.0.0.1:8082"
upstream_url = "https://api.anthropic.com"
stripped_headers = ["authorization", "x-api-key"]

[shipper]
rotate_size_mb = 5
ship_interval_seconds = 30

[minio]
endpoint = "https://s3.example.com"
bucket = "marc"
access_key = "AKIAIOSFODNN7"
secret_key = "wJalrXUtnFEMI"
verify_tls = true
`

func TestLoadClient_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "config.toml", validClientTOML, 0o600)

	cfg, err := LoadClient(path)
	if err != nil {
		t.Fatalf("LoadClient returned error: %v", err)
	}

	if cfg.MachineName != "test-machine" {
		t.Errorf("MachineName = %q, want %q", cfg.MachineName, "test-machine")
	}
	if cfg.Paths.CaptureFile != "/tmp/capture.jsonl" {
		t.Errorf("Paths.CaptureFile = %q, want %q", cfg.Paths.CaptureFile, "/tmp/capture.jsonl")
	}
	if cfg.Proxy.ListenAddr != "127.0.0.1:8082" {
		t.Errorf("Proxy.ListenAddr = %q, want %q", cfg.Proxy.ListenAddr, "127.0.0.1:8082")
	}
	if cfg.Shipper.RotateSizeMB != 5 {
		t.Errorf("Shipper.RotateSizeMB = %d, want %d", cfg.Shipper.RotateSizeMB, 5)
	}
	if cfg.MinIO.Endpoint != "https://s3.example.com" {
		t.Errorf("MinIO.Endpoint = %q, want %q", cfg.MinIO.Endpoint, "https://s3.example.com")
	}
	if !cfg.MinIO.VerifyTLS {
		t.Errorf("MinIO.VerifyTLS = false, want true")
	}
	if len(cfg.Proxy.StrippedHeaders) != 2 {
		t.Errorf("Proxy.StrippedHeaders len = %d, want 2", len(cfg.Proxy.StrippedHeaders))
	}
}

func TestLoadClient_WrongMode(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "config.toml", validClientTOML, 0o644)

	_, err := LoadClient(path)
	if err == nil {
		t.Fatal("expected error for wrong permissions, got nil")
	}
	if !strings.Contains(err.Error(), "0600") {
		t.Errorf("error should mention 0600, got: %v", err)
	}
}

func TestLoadClient_MalformedTOML(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "config.toml", "machine_name = [not valid toml", 0o600)

	_, err := LoadClient(path)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

func TestLoadClient_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		toml    string
		missing string
	}{
		{
			name: "missing machine_name",
			toml: `
[minio]
endpoint = "https://s3.example.com"
bucket = "marc"
access_key = "key"
secret_key = "secret"
`,
			missing: "machine_name",
		},
		{
			name: "missing minio.endpoint",
			toml: `
machine_name = "box"
[minio]
bucket = "marc"
access_key = "key"
secret_key = "secret"
`,
			missing: "minio.endpoint",
		},
		{
			name: "missing minio.bucket",
			toml: `
machine_name = "box"
[minio]
endpoint = "https://s3.example.com"
access_key = "key"
secret_key = "secret"
`,
			missing: "minio.bucket",
		},
		{
			name: "missing minio.access_key",
			toml: `
machine_name = "box"
[minio]
endpoint = "https://s3.example.com"
bucket = "marc"
secret_key = "secret"
`,
			missing: "minio.access_key",
		},
		{
			name: "missing minio.secret_key",
			toml: `
machine_name = "box"
[minio]
endpoint = "https://s3.example.com"
bucket = "marc"
access_key = "key"
`,
			missing: "minio.secret_key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeTemp(t, dir, "config.toml", tt.toml, 0o600)

			_, err := LoadClient(path)
			if err == nil {
				t.Fatalf("expected error mentioning %q, got nil", tt.missing)
			}
			if !strings.Contains(err.Error(), tt.missing) {
				t.Errorf("error should mention %q, got: %v", tt.missing, err)
			}
		})
	}
}

func TestLoadClient_HomeTildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir:", err)
	}

	tomlContent := `
machine_name = "box"

[paths]
capture_file = "~/.marc/capture.jsonl"
log_file = "~/.marc/marc.log"

[minio]
endpoint = "https://s3.example.com"
bucket = "marc"
access_key = "key"
secret_key = "secret"
`
	dir := t.TempDir()
	path := writeTemp(t, dir, "config.toml", tomlContent, 0o600)

	cfg, err := LoadClient(path)
	if err != nil {
		t.Fatalf("LoadClient: %v", err)
	}
	want := filepath.Join(home, ".marc/capture.jsonl")
	if cfg.Paths.CaptureFile != want {
		t.Errorf("CaptureFile = %q, want %q", cfg.Paths.CaptureFile, want)
	}
}

func TestPrintDefaultClient_Parseable(t *testing.T) {
	var buf bytes.Buffer
	if err := PrintDefaultClient(&buf); err != nil {
		t.Fatalf("PrintDefaultClient: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	// The template has placeholder values for access_key/secret_key but they are
	// non-empty strings ("AKIA..." / "MIGHTY..."), so validation passes.
	cfg, err := LoadClient(path)
	if err != nil {
		t.Fatalf("default template is not parseable: %v", err)
	}
	if cfg.MachineName == "" {
		t.Error("machine_name should not be empty in default template")
	}
}

// ---- ServerConfig tests -------------------------------------------------------

const validServerTOML = `
machine_name = "test-server"

[paths]
capture_file = "/var/lib/marc/capture.jsonl"
log_file = "/var/log/marc.log"

[minio]
endpoint = "https://s3.example.com"
bucket = "marc"
access_key = "AKIAIOSFODNN7"
secret_key = "wJalrXUtnFEMI"
verify_tls = true
staging_dir = "/var/lib/marc/staging"

[clickhouse]
addr = "127.0.0.1:9000"
database = "marc"
user = "default"
password = ""

[sqlite]
path = "/var/lib/marc/state/state.db"

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
bot_token = "bot12345:ABCDE"
chat_id = 123456789

[filtering]
min_durability = 7
max_obviousness = 7

[projects]
"abc123" = "myproject"
`

func TestLoadServer_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "server.toml", validServerTOML, 0o600)

	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer returned error: %v", err)
	}

	if cfg.MachineName != "test-server" {
		t.Errorf("MachineName = %q, want %q", cfg.MachineName, "test-server")
	}
	if cfg.ClickHouse.Addr != "127.0.0.1:9000" {
		t.Errorf("ClickHouse.Addr = %q, want %q", cfg.ClickHouse.Addr, "127.0.0.1:9000")
	}
	if cfg.SQLite.Path != "/var/lib/marc/state/state.db" {
		t.Errorf("SQLite.Path = %q", cfg.SQLite.Path)
	}
	if cfg.Ollama.DenoiseModel != "qwen3:8b" {
		t.Errorf("Ollama.DenoiseModel = %q", cfg.Ollama.DenoiseModel)
	}
	if cfg.Scheduler.Timezone != "Asia/Bangkok" {
		t.Errorf("Scheduler.Timezone = %q", cfg.Scheduler.Timezone)
	}
	if cfg.Scheduler.EventsPerGeneration != 30 {
		t.Errorf("Scheduler.EventsPerGeneration = %d, want 30", cfg.Scheduler.EventsPerGeneration)
	}
	if cfg.Telegram.ChatID != 123456789 {
		t.Errorf("Telegram.ChatID = %d, want 123456789", cfg.Telegram.ChatID)
	}
	if cfg.Filtering.MinDurability != 7 {
		t.Errorf("Filtering.MinDurability = %d, want 7", cfg.Filtering.MinDurability)
	}
	if cfg.Projects["abc123"] != "myproject" {
		t.Errorf("Projects[abc123] = %q, want %q", cfg.Projects["abc123"], "myproject")
	}
	if cfg.MinIO.StagingDir != "/var/lib/marc/staging" {
		t.Errorf("MinIO.StagingDir = %q", cfg.MinIO.StagingDir)
	}
}

func TestLoadServer_WrongMode(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "server.toml", validServerTOML, 0o644)

	_, err := LoadServer(path)
	if err == nil {
		t.Fatal("expected error for wrong permissions, got nil")
	}
	if !strings.Contains(err.Error(), "0600") {
		t.Errorf("error should mention 0600, got: %v", err)
	}
}

func TestLoadServer_MalformedTOML(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "server.toml", "machine_name = [bad toml", 0o600)

	_, err := LoadServer(path)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

func TestLoadServer_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		toml    string
		missing string
	}{
		{
			name: "missing machine_name",
			toml: func() string {
				return strings.ReplaceAll(validServerTOML, `machine_name = "test-server"`, "")
			}(),
			missing: "machine_name",
		},
		{
			name: "missing minio.endpoint",
			toml: func() string {
				return strings.ReplaceAll(validServerTOML, `endpoint = "https://s3.example.com"`, "")
			}(),
			missing: "minio.endpoint",
		},
		{
			name: "missing clickhouse.addr",
			toml: func() string {
				return strings.ReplaceAll(validServerTOML, `addr = "127.0.0.1:9000"`, "")
			}(),
			missing: "clickhouse.addr",
		},
		{
			name: "missing sqlite.path",
			toml: func() string {
				return strings.ReplaceAll(validServerTOML, `path = "/var/lib/marc/state/state.db"`, "")
			}(),
			missing: "sqlite.path",
		},
		{
			name: "missing ollama.endpoint",
			toml: func() string {
				return strings.ReplaceAll(validServerTOML, `endpoint = "http://127.0.0.1:11434"`, "")
			}(),
			missing: "ollama.endpoint",
		},
		{
			name: "missing telegram.bot_token",
			toml: func() string {
				return strings.ReplaceAll(validServerTOML, `bot_token = "bot12345:ABCDE"`, "")
			}(),
			missing: "telegram.bot_token",
		},
		{
			name: "missing telegram.chat_id",
			toml: func() string {
				return strings.ReplaceAll(validServerTOML, "chat_id = 123456789", "")
			}(),
			missing: "telegram.chat_id",
		},
		{
			name: "missing scheduler.timezone",
			toml: func() string {
				return strings.ReplaceAll(validServerTOML, `timezone = "Asia/Bangkok"`, "")
			}(),
			missing: "scheduler.timezone",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeTemp(t, dir, "server.toml", tt.toml, 0o600)

			_, err := LoadServer(path)
			if err == nil {
				t.Fatalf("expected error mentioning %q, got nil", tt.missing)
			}
			if !strings.Contains(err.Error(), tt.missing) {
				t.Errorf("error should mention %q, got: %v", tt.missing, err)
			}
		})
	}
}

func TestPrintDefaultServer_Parseable(t *testing.T) {
	var buf bytes.Buffer
	if err := PrintDefaultServer(&buf); err != nil {
		t.Fatalf("PrintDefaultServer: %v", err)
	}

	// The default template has a comment-only [projects] section. That parses fine.
	// bot_token placeholder "..." is non-empty; chat_id is 123456789.
	dir := t.TempDir()
	path := filepath.Join(dir, "server.toml")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("default server template is not parseable: %v", err)
	}
	if cfg.MachineName == "" {
		t.Error("machine_name should not be empty in default template")
	}
}
