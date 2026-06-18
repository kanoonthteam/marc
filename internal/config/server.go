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

// DenoiseConfig selects which backend denoises raw captures.
type DenoiseConfig struct {
	// Provider is "ollama" (local, default) or "minimax" (hosted
	// Anthropic-Messages-compatible API). Unset defaults to "ollama".
	Provider string `toml:"provider"`
	// MaxEventBytes caps the size of a single trimmed event sent to the
	// denoiser; larger events are skipped. The 80KB default suited local
	// Ollama (which timed out on big inputs); a hosted provider like MiniMax
	// handles ~150KB events in seconds, so operators can raise this to avoid
	// dropping substantive coding sessions. Defaults to 81920 (80KB) if unset.
	MaxEventBytes int `toml:"max_event_bytes"`
	// MaxCallsPerMinute caps how many denoise requests are issued per minute,
	// spaced evenly, to keep a hosted provider (MiniMax) under its overload
	// threshold during large backlog drains — without it a catch-up burst hit
	// ~120/min and triggered 529s. 0 = unlimited.
	MaxCallsPerMinute int `toml:"max_calls_per_minute"`
}

// MiniMaxConfig holds connection settings + the denoise model for the MiniMax
// backend (used when denoise.provider = "minimax"). MiniMax speaks the
// Anthropic Messages protocol at base_url + /v1/messages with bearer auth. The
// same base_url/api_key are reused for MiniMax question generation; only the
// model differs (see GenerationConfig.MinimaxModel).
type MiniMaxConfig struct {
	BaseURL string `toml:"base_url"`
	APIKey  string `toml:"api_key"`
	Model   string `toml:"model"`
}

// GenerationConfig controls the question-generation backend. Generation
// normally shells out to `claude -p` (Opus). When RandomizeBackend is true the
// generator flips a coin each cycle between Claude and MiniMax (MinimaxModel),
// so the corpus isn't shaped by a single model's biases.
type GenerationConfig struct {
	RandomizeBackend bool   `toml:"randomize_backend"`
	MinimaxModel     string `toml:"minimax_model"`
}

// SchedulerConfig holds cron expressions and scheduler tuning parameters.
type SchedulerConfig struct {
	QuestionGenCron     string `toml:"question_gen_cron"`
	TelegramSendCron    string `toml:"telegram_send_cron"`
	Timezone            string `toml:"timezone"`
	EventsPerGeneration int    `toml:"events_per_generation"`
	// MaxReadyQueue caps how many 'ready' questions may sit in the queue.
	// Generation runs hourly but delivery is throttled by telegram_send_cron
	// (~20/weekday), so without a cap the queue grows unbounded and the FIFO
	// head falls weeks behind current work. When ready >= MaxReadyQueue the
	// generator skips the cycle (advancing its cursor to stay current) so the
	// queue stays a small, fresh rolling buffer. Defaults to 40 if unset.
	MaxReadyQueue int `toml:"max_ready_queue"`
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
	Denoise     DenoiseConfig     `toml:"denoise"`
	MiniMax     MiniMaxConfig     `toml:"minimax"`
	Generation  GenerationConfig  `toml:"generation"`
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

	// Apply defaults for optional tuning fields so existing deployed configs
	// (which predate the field) keep working without an edit.
	if cfg.Scheduler.MaxReadyQueue <= 0 {
		cfg.Scheduler.MaxReadyQueue = 40
	}
	if strings.TrimSpace(cfg.Denoise.Provider) == "" {
		cfg.Denoise.Provider = "ollama"
	}
	if cfg.Denoise.MaxEventBytes <= 0 {
		cfg.Denoise.MaxEventBytes = 80 * 1024
	}
	if strings.TrimSpace(cfg.MiniMax.BaseURL) == "" {
		cfg.MiniMax.BaseURL = "https://api.minimax.io/anthropic"
	}
	if strings.TrimSpace(cfg.Generation.MinimaxModel) == "" {
		cfg.Generation.MinimaxModel = "MiniMax-M3"
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

[denoise]
# Denoise backend: "ollama" (local, default) or "minimax" (hosted API).
provider = "ollama"
# Skip events whose trimmed last message exceeds this many bytes. 80KB suits
# local Ollama; hosted providers (minimax) handle ~150KB+ — raise to keep
# substantive sessions. Defaults to 81920.
max_event_bytes = 81920
# Cap denoise requests per minute (evenly spaced) to stay under a hosted
# provider's overload threshold during backlog drains. 0 = unlimited.
max_calls_per_minute = 0

[minimax]
# Used when denoise.provider = "minimax". Anthropic-Messages-compatible API.
# model here is the DENOISE model — a small/fast one is plenty for extraction.
base_url = "https://api.minimax.io/anthropic"
api_key = "..."
model = "MiniMax-M2.5-highspeed"

[generation]
# When true, each generation cycle randomly picks Claude (claude -p) or MiniMax
# (minimax_model) so the corpus isn't shaped by one model's biases.
randomize_backend = false
minimax_model = "MiniMax-M3"

[claude]
binary = "claude"
internal_header = "X-Marc-Internal"

[scheduler]
question_gen_cron = "0 * * * *"
telegram_send_cron = "*/30 9-18 * * 1-5"
timezone = "Asia/Bangkok"
events_per_generation = 30
# Cap the ready-question backlog so it stays a small, fresh rolling buffer
# instead of growing faster than delivery drains it. Defaults to 40 if unset.
max_ready_queue = 40

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
	// When the MiniMax denoiser is selected, its key and model are required.
	if strings.TrimSpace(cfg.Denoise.Provider) == "minimax" {
		if strings.TrimSpace(cfg.MiniMax.APIKey) == "" {
			return fmt.Errorf("config: required field %q is missing or empty (denoise.provider = minimax)", "minimax.api_key")
		}
		if strings.TrimSpace(cfg.MiniMax.Model) == "" {
			return fmt.Errorf("config: required field %q is missing or empty (denoise.provider = minimax)", "minimax.model")
		}
	}
	return nil
}
