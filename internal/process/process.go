// Package process implements the marc-server MinIO poll and denoise loop.
package process

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/caffeaun/marc/internal/clickhouse"
	"github.com/caffeaun/marc/internal/config"
	"github.com/caffeaun/marc/internal/jsonl"
	"github.com/caffeaun/marc/internal/minioclient"
	"github.com/caffeaun/marc/internal/ollama"
	"github.com/caffeaun/marc/internal/sqlitedb"
)

const defaultPollInterval = 60 * time.Second

// Options configures the process daemon. All service constructor fields default
// to the production implementations when nil.
type Options struct {
	// Config is the loaded server configuration. Required.
	Config *config.ServerConfig

	// PollInterval controls how often the daemon polls MinIO.
	// Defaults to 60 seconds when zero.
	PollInterval time.Duration

	// Out receives structured-log output. Defaults to os.Stdout when nil.
	Out io.Writer

	// Test injection points. When nil the production constructors are used.

	// NewMinioClient, when non-nil, is called instead of minioclient.New.
	NewMinioClient func(minioclient.Config) (minioclient.Client, error)

	// NewClickHouseConn, when non-nil, is called instead of clickhouse.Connect.
	NewClickHouseConn func(config.ClickHouseConfig) (clickhouse.Client, error)

	// NewOllamaClient, when non-nil, is called instead of ollama.New.
	NewOllamaClient func(config.OllamaConfig) ollama.Client

	// SQLiteDB, when non-nil, is used directly instead of opening cfg.SQLite.Path.
	SQLiteDB *sqlitedb.DB
}

func (o Options) pollInterval() time.Duration {
	if o.PollInterval <= 0 {
		return defaultPollInterval
	}
	return o.PollInterval
}

// Run blocks until ctx is cancelled. It executes one tick immediately on entry,
// then ticks every PollInterval.
//
// Returns nil when ctx is cancelled.
func Run(ctx context.Context, opts Options) error {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}

	logger := slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Construct or reuse the SQLite DB.
	var db *sqlitedb.DB
	if opts.SQLiteDB != nil {
		db = opts.SQLiteDB
	} else {
		var err error
		db, err = sqlitedb.Open(opts.Config.SQLite.Path)
		if err != nil {
			return fmt.Errorf("process: open sqlite: %w", err)
		}
		defer db.Close()
	}

	// Construct the MinIO client.
	var mc minioclient.Client
	if opts.NewMinioClient != nil {
		var err error
		mc, err = opts.NewMinioClient(minioclient.Config{
			Endpoint:  opts.Config.MinIO.Endpoint,
			Bucket:    opts.Config.MinIO.Bucket,
			AccessKey: opts.Config.MinIO.AccessKey,
			SecretKey: opts.Config.MinIO.SecretKey,
			VerifyTLS: opts.Config.MinIO.VerifyTLS,
		})
		if err != nil {
			return fmt.Errorf("process: construct minio client: %w", err)
		}
	} else {
		var err error
		mc, err = minioclient.New(minioclient.Config{
			Endpoint:  opts.Config.MinIO.Endpoint,
			Bucket:    opts.Config.MinIO.Bucket,
			AccessKey: opts.Config.MinIO.AccessKey,
			SecretKey: opts.Config.MinIO.SecretKey,
			VerifyTLS: opts.Config.MinIO.VerifyTLS,
		})
		if err != nil {
			return fmt.Errorf("process: construct minio client: %w", err)
		}
	}

	// Construct the ClickHouse client.
	var chClient clickhouse.Client
	if opts.NewClickHouseConn != nil {
		var err error
		chClient, err = opts.NewClickHouseConn(opts.Config.ClickHouse)
		if err != nil {
			return fmt.Errorf("process: construct clickhouse client: %w", err)
		}
	} else {
		var err error
		chClient, err = clickhouse.Connect(opts.Config.ClickHouse)
		if err != nil {
			return fmt.Errorf("process: construct clickhouse client: %w", err)
		}
	}
	defer chClient.Close()

	// Construct the Ollama client.
	var ollamaClient ollama.Client
	if opts.NewOllamaClient != nil {
		ollamaClient = opts.NewOllamaClient(opts.Config.Ollama)
	} else {
		ollamaClient = ollama.New(opts.Config.Ollama)
	}
	defer ollamaClient.Close()

	d := &daemon{
		cfg:          opts.Config,
		db:           db,
		mc:           mc,
		ch:           chClient,
		ollama:       ollamaClient,
		logger:       logger,
		stagingDir:   opts.Config.MinIO.StagingDir,
		denoiseModel: opts.Config.Ollama.DenoiseModel,
		machine:      opts.Config.MachineName,
	}

	ticker := time.NewTicker(opts.pollInterval())
	defer ticker.Stop()

	// Run one tick immediately on entry.
	d.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			d.tick(ctx)
		}
	}
}

// daemon holds shared state across ticks.
type daemon struct {
	cfg          *config.ServerConfig
	db           *sqlitedb.DB
	mc           minioclient.Client
	ch           clickhouse.Client
	ollama       ollama.Client
	logger       *slog.Logger
	stagingDir   string
	denoiseModel string
	machine      string
}

// tick runs one full poll cycle: crash recovery, then process new objects
// from every machine that has ever shipped to the bucket. marc is designed
// for multi-machine capture (one server consumes from every client's prefix);
// scoping to d.machine alone would silently drop everyone else's events.
func (d *daemon) tick(ctx context.Context) {
	// Step 1: crash recovery — clean stale staging files older than this cycle.
	d.cleanStagingFiles(ctx)

	// Step 2: discover every machine that has shipped under raw/, then process
	// each one in turn. processMachine has its own per-machine cursor, so two
	// clients never race on the same prefix.
	machines, err := d.listMachines(ctx)
	if err != nil {
		d.logger.Error("process: discover machines", slog.Any("error", err))
		return
	}
	for _, m := range machines {
		if ctx.Err() != nil {
			return
		}
		d.processMachine(ctx, m)
	}
}

// listMachines lists distinct machine names that have at least one object
// under raw/<machine>/ in MinIO. Returns names in stable lexicographic order.
func (d *daemon) listMachines(ctx context.Context) ([]string, error) {
	keys, err := d.mc.ListObjects(ctx, "raw/", "")
	if err != nil {
		return nil, fmt.Errorf("list raw/: %w", err)
	}
	seen := make(map[string]struct{}, 4)
	machines := make([]string, 0, 4)
	for _, k := range keys {
		// raw/<machine>/<rest>
		rest := strings.TrimPrefix(k, "raw/")
		i := strings.IndexByte(rest, '/')
		if i <= 0 {
			continue
		}
		m := rest[:i]
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		machines = append(machines, m)
	}
	sort.Strings(machines)
	return machines, nil
}

// cleanStagingFiles removes any leftover staging files from a prior crashed cycle.
func (d *daemon) cleanStagingFiles(_ context.Context) {
	entries, err := os.ReadDir(d.stagingDir)
	if err != nil {
		if !os.IsNotExist(err) {
			d.logger.Warn("process: read staging dir", slog.String("dir", d.stagingDir), slog.Any("error", err))
		}
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(d.stagingDir, e.Name())
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			d.logger.Warn("process: remove stale staging file",
				slog.String("path", path),
				slog.Any("error", err),
			)
		} else {
			d.logger.Debug("process: removed stale staging file", slog.String("path", path))
		}
	}
}

// rawPrefix returns the MinIO prefix for a machine's raw objects.
func rawPrefix(machine string) string {
	return "raw/" + machine + "/"
}

// archiveKey converts a raw object key to its archive destination.
// raw/<machine>/<rest> → processed/raw-archive/<machine>/<rest>
func archiveKey(key string) string {
	// key looks like: raw/<machine>/<YYYY>/<MM>/<DD>/<HH>/filename.jsonl
	// Replace leading "raw/" with "processed/raw-archive/"
	trimmed := strings.TrimPrefix(key, "raw/")
	return "processed/raw-archive/" + trimmed
}

// processMachine lists all new MinIO objects for machine and processes them in order.
func (d *daemon) processMachine(ctx context.Context, machine string) {
	cursor, err := d.db.GetCursor(ctx, machine)
	if err != nil {
		d.logger.Error("process: get cursor",
			slog.String("machine", machine),
			slog.Any("error", err),
		)
		return
	}

	prefix := rawPrefix(machine)
	keys, err := d.mc.ListObjects(ctx, prefix, cursor)
	if err != nil {
		// AC#6: MinIO list failure → log, cursor unchanged, retry next cycle, daemon does not panic.
		d.logger.Error("process: list minio objects",
			slog.String("machine", machine),
			slog.String("prefix", prefix),
			slog.Any("error", err),
		)
		return
	}

	if len(keys) == 0 {
		d.logger.Debug("process: no new objects", slog.String("machine", machine))
		return
	}

	d.logger.Info("process: new objects found",
		slog.String("machine", machine),
		slog.Int("count", len(keys)),
	)

	for _, key := range keys {
		if ctx.Err() != nil {
			return
		}
		if err := d.processObject(ctx, machine, key); err != nil {
			// Halt batch on any object error; cursor not advanced.
			d.logger.Error("process: object processing failed; halting batch for this cycle",
				slog.String("machine", machine),
				slog.String("key", key),
				slog.Any("error", err),
			)
			return
		}
	}
}

// processObject downloads, parses, denoises, inserts, archives, and advances the
// cursor for a single MinIO object.
func (d *daemon) processObject(ctx context.Context, machine, key string) error {
	// Download to staging.
	stagingPath := filepath.Join(d.stagingDir, filepath.Base(key))
	if err := d.downloadToStaging(ctx, key, stagingPath); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	// Always clean up staging file at the end (success or failure after staging).
	defer func() {
		if err := os.Remove(stagingPath); err != nil && !os.IsNotExist(err) {
			d.logger.Warn("process: remove staging file",
				slog.String("path", stagingPath),
				slog.Any("error", err),
			)
		}
	}()

	// Parse JSONL and process each event.
	skippedInternal := 0
	processed := 0
	if err := d.processJSONL(ctx, stagingPath, &skippedInternal, &processed); err != nil {
		return err
	}

	d.logger.Info("process: object events processed",
		slog.String("key", key),
		slog.Int("processed", processed),
		slog.Int("skipped_internal", skippedInternal),
	)

	// Move source object to archive.
	dst := archiveKey(key)
	if err := d.mc.MoveObject(ctx, key, dst); err != nil {
		// AC failure mode: MoveObject failure → halt batch, do not advance cursor.
		return fmt.Errorf("move to archive: %w", err)
	}

	// Advance cursor after successful archive.
	if err := d.db.UpsertCursor(ctx, machine, key); err != nil {
		return fmt.Errorf("upsert cursor: %w", err)
	}

	d.logger.Info("process: object complete",
		slog.String("key", key),
		slog.String("archive", dst),
	)

	return nil
}

// downloadToStaging downloads an object from MinIO into the local staging directory.
// It creates the staging directory if it does not exist.
func (d *daemon) downloadToStaging(ctx context.Context, key, stagingPath string) error {
	if err := os.MkdirAll(d.stagingDir, 0o755); err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}

	rc, err := d.mc.GetObject(ctx, key)
	if err != nil {
		return fmt.Errorf("get object %s: %w", key, err)
	}
	defer rc.Close()

	f, err := os.Create(stagingPath)
	if err != nil {
		return fmt.Errorf("create staging file %s: %w", stagingPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, rc); err != nil {
		return fmt.Errorf("write staging file: %w", err)
	}
	return nil
}

// processJSONL reads a JSONL file event by event, skipping internal events and
// denoising/inserting non-internal ones.
func (d *daemon) processJSONL(ctx context.Context, path string, skippedInternal, processed *int) error {
	reader, err := jsonl.NewLineReader(path)
	if err != nil {
		return fmt.Errorf("open jsonl %s: %w", path, err)
	}
	defer reader.Close()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		line, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read jsonl: %w", err)
		}

		var ev jsonl.CaptureEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			// Skip malformed events with a log; do not halt the batch.
			d.logger.Warn("process: skip malformed event",
				slog.String("path", path),
				slog.Any("error", err),
			)
			continue
		}

		// AC#2: is_internal == true → skip (do NOT call ollama, do NOT write to ClickHouse).
		if ev.IsInternal {
			*skippedInternal++
			d.logger.Debug("process: skip internal event", slog.String("event_id", ev.EventID))
			continue
		}

		// Denoise via Ollama.
		dr, err := d.ollama.Denoise(ctx, d.denoiseModel, string(line))
		if err != nil {
			// Poison-pill: model produced a response that doesn't unmarshal as
			// DenoiseResult. Retrying will deterministically reproduce the
			// failure and would block the entire pipeline. Skip the event,
			// log loudly, and let the rest of the batch drain so the cursor
			// can advance past this object.
			if errors.Is(err, ollama.ErrUnparseableModelOutput) {
				d.logger.Error("process: skipping event with unparseable model output (cursor will advance)",
					slog.String("event_id", ev.EventID),
					slog.String("denoise_model", d.denoiseModel),
					slog.Any("error", err),
				)
				continue
			}
			// AC#4: any other Ollama failure (network, timeout, non-200 status)
			// is potentially transient → halt batch, cursor does not advance,
			// retry next cycle.
			return fmt.Errorf("denoise event %s: %w", ev.EventID, err)
		}

		// Build ClickHouse event. Machine is read from ev.Machine (set by
		// the originating proxy) — not d.machine — so MacBook events stay
		// tagged "macbook-..." instead of being relabelled with the server's
		// hostname.
		chEvent := buildEvent(ev, dr, d.denoiseModel)

		// ClickHouse ReplacingMergeTree dedupes by sort key during background merges,
		// not on INSERT. If this processor crashes mid-batch (some inserts succeeded,
		// cursor not yet advanced), re-processing produces transient duplicate rows
		// until the next merge. Run `OPTIMIZE TABLE marc.events FINAL` if exact
		// counts matter immediately after a recovery; otherwise duplicates clear on
		// the next scheduled merge. Acceptable for v1 analytics.
		if err := d.ch.InsertEvent(ctx, chEvent); err != nil {
			// AC#5: ClickHouse insert failure → halt batch, log, do not advance cursor.
			return fmt.Errorf("insert event %s: %w", ev.EventID, err)
		}

		*processed++
	}

	return nil
}
