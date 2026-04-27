package config

import (
	"fmt"
	"io"
	"strings"

	"github.com/BurntSushi/toml"
)

// ServerPaths holds filesystem paths used by the marc-server daemons.
type ServerPaths struct {
	CaptureFile string `toml:"capture_file"`
	LogFile     string `toml:"log_file"`
}

// ServerMinIO holds MinIO connection settings for the server, including
// an additional staging directory for downloaded objects.
type ServerMinIO struct {
	Endpoint   string `toml:"endpoint"`
	Bucket     string `toml:"bucket"`
	AccessKey  string `toml:"access_key"`
	SecretKey  string `toml:"secret_key"`
	VerifyTLS  bool   `toml:"verify_tls"`
	StagingDir string `toml:"staging_dir"`
}

// ClickHouseConfig holds ClickHouse connection settings.
type ClickHouseConfig struct {
	Addr     string `toml:"addr"`
	Database string `toml:"database"`
	User     string `toml:"user"`
	Password string `toml:"password"`
}

// SQLiteConfig holds the SQLite database path.
type SQLiteConfig struct {
	Path string `toml:"path"`
}

// OllamaConfig holds Ollama inference settings.
type OllamaConfig struct {
	Endpoint     string `toml:"endpoint"`
	DenoiseModel string `toml:"denoise_model"`
}

// ClaudeConfig holds settings for the claude CLI subprocess.
type ClaudeConfig struct {
	Binary         string `toml:"binary"`
	InternalHeader string `toml:"internal_header"`
}

// SchedulerConfig holds cron expressions and scheduler tuning parameters.
type SchedulerConfig struct {
	QuestionGenCron    string `toml:"question_gen_cron"`
	TelegramSendCron   string `toml:"telegram_send_cron"`
	Timezone           string `toml:"timezone"`
	EventsPerGeneration int   `toml:"events_per_generation"`
}

// TelegramConfig holds Telegram bot credentials.
type TelegramConfig struct {
	BotToken string `toml:"bot_token"`
	ChatID   int64  `toml:"chat_id"`
}

// FilteringConfig controls which denoised events qualify for question generation.
type FilteringConfig struct {
	MinDurability  int `toml:"min_durability"`
	MaxObviousness int `toml:"max_obviousness"`
}

// ServerConfig is the top-level configuration for the marc-server binary.
// It is loaded from /etc/marc/server.toml (mode 0600).
type ServerConfig struct {
	MachineName string            `toml:"machine_name"`
	Paths       ServerPaths       `toml:"paths"`
	MinIO       ServerMinIO       `toml:"minio"`
	ClickHouse  ClickHouseConfig  `toml:"clickhouse"`
	SQLite      SQLiteConfig      `toml:"sqlite"`
	Ollama      OllamaConfig      `toml:"ollama"`
	Claude      ClaudeConfig      `toml:"claude"`
	Scheduler   SchedulerConfig   `toml:"scheduler"`
	Telegram    TelegramConfig    `toml:"telegram"`
	Filtering   FilteringConfig   `toml:"filtering"`
	Projects    map[string]string `toml:"projects"`
}

// LoadServer reads and parses the server config at path.
// It returns an error if the file permissions are not exactly 0600,
// if the TOML cannot be parsed, or if required fields are absent.
func LoadServer(path string) (*ServerConfig, error) {
	if err := checkMode(path); err != nil {
		return nil, err
	}

	var cfg ServerConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	if err := validateServer(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// PrintDefaultServer writes the spec's template server TOML to w.
// The output is parseable by LoadServer after the user fills in placeholder
// values and sets file mode 0600.
func PrintDefaultServer(w io.Writer) error {
	_, err := fmt.Fprint(w, defaultServerTOML)
	return err
}

// defaultServerTOML is the verbatim template from spec §"Server config file format".
const defaultServerTOML = `machine_name = "ubuntu-server"

[paths]
capture_file = "/var/lib/marc/capture.jsonl"
log_file = "/var/log/marc.log"

[minio]
endpoint = "https://artifacts.kanolab.io"
bucket = "marc"
access_key = "AKIA..."
secret_key = "MIGHTY..."
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
bot_token = "..."
chat_id = 123456789

[filtering]
min_durability = 7
max_obviousness = 7

[projects]
# Map directory hashes to friendly names. Edit after first ingest.
# "abc123hash" = "sliplotto"
# "def456hash" = "flowrent"
`

// validateServer checks required fields and returns a descriptive error.
func validateServer(cfg *ServerConfig) error {
	stringRequired := []struct {
		value string
		name  string
	}{
		{cfg.MachineName, "machine_name"},
		{cfg.MinIO.Endpoint, "minio.endpoint"},
		{cfg.ClickHouse.Addr, "clickhouse.addr"},
		{cfg.SQLite.Path, "sqlite.path"},
		{cfg.Ollama.Endpoint, "ollama.endpoint"},
		{cfg.Telegram.BotToken, "telegram.bot_token"},
		{cfg.Scheduler.Timezone, "scheduler.timezone"},
	}
	for _, r := range stringRequired {
		if strings.TrimSpace(r.value) == "" {
			return fmt.Errorf("config: required field %q is missing or empty", r.name)
		}
	}
	if cfg.Telegram.ChatID == 0 {
		return fmt.Errorf("config: required field %q is missing or zero", "telegram.chat_id")
	}
	return nil
}
