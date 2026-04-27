// Package server provides the interactive setup wizard for marc-server.
//
// It prompts for MinIO, ClickHouse, SQLite, Ollama, Claude, Scheduler,
// Telegram, and Filtering settings, writes /etc/marc/server.toml with mode
// 0600, and validates all four backing services via their Ping methods.
//
// This package is imported only by cmd/marc-server. The separation from
// internal/configure/client ensures cmd/marc does not transitively depend on
// clickhouse, sqlitedb, ollama, or telegram packages.
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/caffeaun/marc/internal/clickhouse"
	"github.com/caffeaun/marc/internal/config"
	"github.com/caffeaun/marc/internal/minioclient"
	"github.com/caffeaun/marc/internal/ollama"
)

// Options controls the behaviour of Run.
// Zero values produce safe production defaults.
type Options struct {
	// Path is the destination file; defaults to /etc/marc/server.toml.
	Path string

	// Check loads an existing config, pings all services, prints pass/fail per
	// service, and exits non-zero if any service fails. No prompts.
	Check bool

	// PrintDefault writes defaultServerTOML to Writer without creating any file.
	PrintDefault bool

	// Reader is the source for interactive prompts; defaults to os.Stdin.
	Reader io.Reader

	// Writer is the output destination; defaults to os.Stdout.
	Writer io.Writer

	// NowFn is used for testable timestamps. Currently unused but present per spec.
	NowFn func() time.Time

	// Injection points — set by tests to replace live service calls.
	// Production code leaves these nil; applyDefaults fills in the real constructors.
	NewMinioClient    func(minioclient.Config) (minioclient.Client, error)
	NewClickHouseConn func(config.ClickHouseConfig) (clickhouse.Client, error)
	NewOllamaClient   func(config.OllamaConfig) ollama.Client
	TelegramGetMe     func(ctx context.Context, token string) error
}

// Run is the main entry point. It dispatches to the appropriate operating mode.
func Run(ctx context.Context, opts Options) error {
	opts = applyDefaults(opts)

	switch {
	case opts.PrintDefault:
		return config.PrintDefaultServer(opts.Writer)
	case opts.Check:
		return runCheck(ctx, opts)
	default:
		return runInteractive(ctx, opts)
	}
}

// applyDefaults fills in nil/zero fields with production values.
func applyDefaults(opts Options) Options {
	if opts.Path == "" {
		opts.Path = "/etc/marc/server.toml"
	}
	if opts.Reader == nil {
		opts.Reader = os.Stdin
	}
	if opts.Writer == nil {
		opts.Writer = os.Stdout
	}
	if opts.NowFn == nil {
		opts.NowFn = time.Now
	}
	if opts.NewMinioClient == nil {
		opts.NewMinioClient = minioclient.New
	}
	if opts.NewClickHouseConn == nil {
		opts.NewClickHouseConn = clickhouse.Connect
	}
	if opts.NewOllamaClient == nil {
		opts.NewOllamaClient = ollama.New
	}
	if opts.TelegramGetMe == nil {
		opts.TelegramGetMe = telegramPing
	}
	return opts
}

// runCheck loads the existing server config and pings all four services.
// It prints a pass/fail line per service and exits non-zero if any fail.
// The written TOML is not deleted on failure.
func runCheck(ctx context.Context, opts Options) error {
	info, err := os.Stat(opts.Path)
	if err != nil {
		return fmt.Errorf("configure --check: cannot stat %s: %w", opts.Path, err)
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf(
			"configure --check: %s has permissions %04o; must be 0600 (fix with: chmod 0600 %s)",
			opts.Path, info.Mode().Perm(), opts.Path,
		)
	}

	cfg, err := config.LoadServer(opts.Path)
	if err != nil {
		return fmt.Errorf("configure --check: %w", err)
	}

	results := pingAllServices(ctx, cfg, opts)
	return printAndCheckServiceResults(opts.Writer, results)
}

// runInteractive prompts the user for every field, writes the TOML file, and then
// runs a validation pass. Re-running prompts for confirmation when the file already exists.
func runInteractive(ctx context.Context, opts Options) error {
	r := bufio.NewReader(opts.Reader)

	// Overwrite confirmation.
	if _, err := os.Stat(opts.Path); err == nil {
		fmt.Fprintf(opts.Writer, "%s already exists. Overwrite? [y/N]: ", opts.Path)
		answer, rerr := r.ReadString('\n')
		if rerr != nil && !errors.Is(rerr, io.EOF) {
			return fmt.Errorf("configure: read confirmation: %w", rerr)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(opts.Writer, "Aborted; original config unchanged.")
			return nil
		}
	}

	cfg, err := collectFields(r, opts.Writer)
	if err != nil {
		return err
	}

	if err := writeServerConfig(opts.Path, cfg); err != nil {
		return err
	}
	fmt.Fprintf(opts.Writer, "Configuration written to %s\n", opts.Path)

	// Validation pass — print results but do not delete the written TOML on failure.
	fmt.Fprintln(opts.Writer, "Validating services...")
	results := pingAllServices(ctx, cfg, opts)
	return printAndCheckServiceResults(opts.Writer, results)
}

// collectFields prompts the user for every server config field and returns a
// populated ServerConfig. Defaults are shown in brackets.
func collectFields(r *bufio.Reader, w io.Writer) (*config.ServerConfig, error) {
	hostname, _ := os.Hostname()

	machineName, err := prompt(r, w, "machine_name", hostname)
	if err != nil {
		return nil, promptErr("machine_name", err)
	}

	// MinIO section.
	minioEndpoint, err := prompt(r, w, "minio.endpoint", "https://artifacts.kanolab.io")
	if err != nil {
		return nil, promptErr("minio.endpoint", err)
	}
	minioBucket, err := prompt(r, w, "minio.bucket", "marc")
	if err != nil {
		return nil, promptErr("minio.bucket", err)
	}
	minioAccessKey, err := prompt(r, w, "minio.access_key", "")
	if err != nil {
		return nil, promptErr("minio.access_key", err)
	}
	minioSecretKey, err := prompt(r, w, "minio.secret_key", "")
	if err != nil {
		return nil, promptErr("minio.secret_key", err)
	}

	defaultVerifyTLS := "true"
	if strings.HasPrefix(strings.ToLower(minioEndpoint), "http://") {
		defaultVerifyTLS = "false"
		fmt.Fprintln(w, "  Warning: http:// endpoint — defaulting verify_tls to false")
	}
	verifyTLSStr, err := prompt(r, w, "minio.verify_tls (y/n)", defaultVerifyTLS)
	if err != nil {
		return nil, promptErr("minio.verify_tls", err)
	}
	verifyTLS := parseBool(verifyTLSStr, true)

	minioStagingDir, err := prompt(r, w, "minio.staging_dir", "/var/lib/marc/staging")
	if err != nil {
		return nil, promptErr("minio.staging_dir", err)
	}

	// ClickHouse section.
	chAddr, err := prompt(r, w, "clickhouse.addr", "127.0.0.1:9000")
	if err != nil {
		return nil, promptErr("clickhouse.addr", err)
	}
	chDatabase, err := prompt(r, w, "clickhouse.database", "marc")
	if err != nil {
		return nil, promptErr("clickhouse.database", err)
	}
	chUser, err := prompt(r, w, "clickhouse.user", "default")
	if err != nil {
		return nil, promptErr("clickhouse.user", err)
	}
	chPassword, err := prompt(r, w, "clickhouse.password", "")
	if err != nil {
		return nil, promptErr("clickhouse.password", err)
	}

	// SQLite section.
	sqlitePath, err := prompt(r, w, "sqlite.path", "/var/lib/marc/state/state.db")
	if err != nil {
		return nil, promptErr("sqlite.path", err)
	}

	// Ollama section.
	ollamaEndpoint, err := prompt(r, w, "ollama.endpoint", "http://127.0.0.1:11434")
	if err != nil {
		return nil, promptErr("ollama.endpoint", err)
	}
	ollamaDenoiseModel, err := prompt(r, w, "ollama.denoise_model", "qwen3:8b")
	if err != nil {
		return nil, promptErr("ollama.denoise_model", err)
	}

	// Claude section.
	claudeBinary, err := prompt(r, w, "claude.binary", "claude")
	if err != nil {
		return nil, promptErr("claude.binary", err)
	}
	claudeInternalHeader, err := prompt(r, w, "claude.internal_header", "X-Marc-Internal")
	if err != nil {
		return nil, promptErr("claude.internal_header", err)
	}

	// Scheduler section.
	schedulerQuestionGenCron, err := prompt(r, w, "scheduler.question_gen_cron", "0 * * * *")
	if err != nil {
		return nil, promptErr("scheduler.question_gen_cron", err)
	}
	schedulerTelegramSendCron, err := prompt(r, w, "scheduler.telegram_send_cron", "*/30 9-18 * * 1-5")
	if err != nil {
		return nil, promptErr("scheduler.telegram_send_cron", err)
	}
	schedulerTimezone, err := prompt(r, w, "scheduler.timezone", "Asia/Bangkok")
	if err != nil {
		return nil, promptErr("scheduler.timezone", err)
	}
	eventsPerGenerationStr, err := prompt(r, w, "scheduler.events_per_generation", "30")
	if err != nil {
		return nil, promptErr("scheduler.events_per_generation", err)
	}
	eventsPerGeneration, convErr := strconv.Atoi(eventsPerGenerationStr)
	if convErr != nil {
		eventsPerGeneration = 30
	}

	// Telegram section.
	telegramBotToken, err := prompt(r, w, "telegram.bot_token", "")
	if err != nil {
		return nil, promptErr("telegram.bot_token", err)
	}
	telegramChatIDStr, err := prompt(r, w, "telegram.chat_id", "")
	if err != nil {
		return nil, promptErr("telegram.chat_id", err)
	}
	var telegramChatID int64
	if v, parseErr := strconv.ParseInt(telegramChatIDStr, 10, 64); parseErr == nil {
		telegramChatID = v
	}

	// Filtering section.
	minDurabilityStr, err := prompt(r, w, "filtering.min_durability", "7")
	if err != nil {
		return nil, promptErr("filtering.min_durability", err)
	}
	minDurability, convErr := strconv.Atoi(minDurabilityStr)
	if convErr != nil {
		minDurability = 7
	}
	maxObviousnessStr, err := prompt(r, w, "filtering.max_obviousness", "7")
	if err != nil {
		return nil, promptErr("filtering.max_obviousness", err)
	}
	maxObviousness, convErr := strconv.Atoi(maxObviousnessStr)
	if convErr != nil {
		maxObviousness = 7
	}

	cfg := &config.ServerConfig{
		MachineName: machineName,
		MinIO: config.ServerMinIO{
			Endpoint:   minioEndpoint,
			Bucket:     minioBucket,
			AccessKey:  minioAccessKey,
			SecretKey:  minioSecretKey,
			VerifyTLS:  verifyTLS,
			StagingDir: minioStagingDir,
		},
		ClickHouse: config.ClickHouseConfig{
			Addr:     chAddr,
			Database: chDatabase,
			User:     chUser,
			Password: chPassword,
		},
		SQLite: config.SQLiteConfig{
			Path: sqlitePath,
		},
		Ollama: config.OllamaConfig{
			Endpoint:     ollamaEndpoint,
			DenoiseModel: ollamaDenoiseModel,
		},
		Claude: config.ClaudeConfig{
			Binary:         claudeBinary,
			InternalHeader: claudeInternalHeader,
		},
		Scheduler: config.SchedulerConfig{
			QuestionGenCron:     schedulerQuestionGenCron,
			TelegramSendCron:    schedulerTelegramSendCron,
			Timezone:            schedulerTimezone,
			EventsPerGeneration: eventsPerGeneration,
		},
		Telegram: config.TelegramConfig{
			BotToken: telegramBotToken,
			ChatID:   telegramChatID,
		},
		Filtering: config.FilteringConfig{
			MinDurability:  minDurability,
			MaxObviousness: maxObviousness,
		},
		// Projects: leave empty; operator edits TOML later.
	}

	return cfg, nil
}

// serviceResult records the outcome of pinging one backing service.
type serviceResult struct {
	Name    string // "minio", "clickhouse", "ollama", "telegram"
	Passed  bool
	Message string
}

// pingAllServices pings each of the four services and returns results.
func pingAllServices(ctx context.Context, cfg *config.ServerConfig, opts Options) []serviceResult {
	results := make([]serviceResult, 0, 4)

	// MinIO ping.
	minioCfg := minioclient.Config{
		Endpoint:  cfg.MinIO.Endpoint,
		Bucket:    cfg.MinIO.Bucket,
		AccessKey: cfg.MinIO.AccessKey,
		SecretKey: cfg.MinIO.SecretKey,
		VerifyTLS: cfg.MinIO.VerifyTLS,
	}
	results = append(results, pingMinio(ctx, minioCfg, opts.NewMinioClient))

	// ClickHouse ping.
	results = append(results, pingClickHouse(ctx, cfg.ClickHouse, opts.NewClickHouseConn))

	// Ollama ping.
	results = append(results, pingOllama(ctx, cfg.Ollama, opts.NewOllamaClient))

	// Telegram ping.
	results = append(results, pingTelegram(ctx, cfg.Telegram.BotToken, opts.TelegramGetMe))

	return results
}

// pingMinio constructs a MinIO client and calls Ping.
func pingMinio(
	ctx context.Context,
	cfg minioclient.Config,
	newClient func(minioclient.Config) (minioclient.Client, error),
) serviceResult {
	name := "minio"
	mc, err := newClient(cfg)
	if err != nil {
		return serviceResult{
			Name:    name,
			Passed:  false,
			Message: fmt.Sprintf("client construction failed: %v", err),
		}
	}
	if err := mc.Ping(ctx); err != nil {
		return serviceResult{Name: name, Passed: false, Message: err.Error()}
	}
	return serviceResult{Name: name, Passed: true, Message: "reachable and writable"}
}

// pingClickHouse opens a ClickHouse connection and pings it.
func pingClickHouse(
	ctx context.Context,
	cfg config.ClickHouseConfig,
	newConn func(config.ClickHouseConfig) (clickhouse.Client, error),
) serviceResult {
	name := "clickhouse"
	conn, err := newConn(cfg)
	if err != nil {
		return serviceResult{
			Name:    name,
			Passed:  false,
			Message: fmt.Sprintf("connect failed: %v", err),
		}
	}
	defer conn.Close() //nolint:errcheck // close-on-check path; error not critical
	if err := conn.Ping(ctx); err != nil {
		return serviceResult{Name: name, Passed: false, Message: err.Error()}
	}
	return serviceResult{Name: name, Passed: true, Message: "reachable"}
}

// pingOllama constructs an Ollama client and pings it (verifying model is loaded).
func pingOllama(
	ctx context.Context,
	cfg config.OllamaConfig,
	newClient func(config.OllamaConfig) ollama.Client,
) serviceResult {
	name := "ollama"
	oc := newClient(cfg)
	defer oc.Close() //nolint:errcheck // close-on-check path; error not critical
	if err := oc.Ping(ctx); err != nil {
		return serviceResult{Name: name, Passed: false, Message: err.Error()}
	}
	return serviceResult{Name: name, Passed: true, Message: "reachable and model loaded"}
}

// pingTelegram checks whether a Telegram bot token is valid by calling getMe.
func pingTelegram(
	ctx context.Context,
	token string,
	getMe func(ctx context.Context, token string) error,
) serviceResult {
	name := "telegram"
	if err := getMe(ctx, token); err != nil {
		return serviceResult{Name: name, Passed: false, Message: err.Error()}
	}
	return serviceResult{Name: name, Passed: true, Message: "bot token valid"}
}

// telegramPing calls GET https://api.telegram.org/bot<token>/getMe and asserts ok:true.
func telegramPing(ctx context.Context, token string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getMe", token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("telegram: getMe build request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: getMe failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body; close error irrelevant

	var body struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("telegram: getMe decode response: %w", err)
	}
	if !body.OK {
		return fmt.Errorf("telegram: getMe failed: %s", body.Description)
	}
	return nil
}

// printAndCheckServiceResults writes a pass/fail line per service and returns
// a non-nil error if any service failed.
func printAndCheckServiceResults(w io.Writer, results []serviceResult) error {
	var failed bool
	for _, r := range results {
		tag := "ok"
		if !r.Passed {
			tag = "fail"
			failed = true
		}
		fmt.Fprintf(w, "[%s] %s", tag, r.Name)
		if r.Message != "" {
			fmt.Fprintf(w, ": %s", r.Message)
		}
		fmt.Fprintln(w)
	}
	if failed {
		return errors.New("one or more services failed validation; TOML left in place for editing")
	}
	return nil
}

// writeServerConfig serialises cfg to path with mode 0600.
// The file is set to mode 0600 before any data is written via os.OpenFile.
// Parent directories are created with mode 0700 if absent.
func writeServerConfig(path string, cfg *config.ServerConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("configure: create config directory: %w", err)
	}

	// Open (or create) the file with mode 0600 immediately so that the file
	// is never world-readable, even if the process is interrupted during write.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // 0600 is the required mode
	if err != nil {
		return fmt.Errorf("configure: open %s: %w", path, err)
	}
	defer f.Close() //nolint:errcheck // write errors surfaced via Encode below

	enc := toml.NewEncoder(f)
	if err := enc.Encode(cfg); err != nil {
		return fmt.Errorf("configure: encode TOML: %w", err)
	}

	// Explicit chmod in case the process umask widened the permissions.
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("configure: chmod %s: %w", path, err)
	}
	return nil
}

// prompt writes "<question> [<default>]: " to w and reads a line from r.
// Returns defaultValue when the user presses Enter without typing anything.
func prompt(r *bufio.Reader, w io.Writer, question, defaultValue string) (string, error) {
	if defaultValue != "" {
		fmt.Fprintf(w, "%s [%s]: ", question, defaultValue)
	} else {
		fmt.Fprintf(w, "%s: ", question)
	}
	line, err := r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultValue, nil
	}
	return line, nil
}

// parseBool converts y/yes/true/1 to true, n/no/false/0 to false.
// Falls back to def for unrecognised input.
func parseBool(s string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "y", "yes", "true", "1":
		return true
	case "n", "no", "false", "0":
		return false
	default:
		return def
	}
}

// promptErr wraps a prompt read error with field context.
func promptErr(field string, err error) error {
	return fmt.Errorf("configure: reading %s: %w", field, err)
}
