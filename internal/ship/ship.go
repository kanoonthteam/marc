// Package ship implements the marc capture-file upload daemon.
package ship

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/caffeaun/marc/internal/minioclient"
)

// Config holds all the parameters needed to run the shipper daemon.
// Callers construct this from config.ClientConfig; tests inject a Fake client.
type Config struct {
	// Machine is the machine name used in the MinIO object key.
	Machine string

	// CapturePath is the fully-expanded path to the active capture file
	// (e.g. "/Users/me/.marc/capture.jsonl"). Must not end with ".shipping".
	CapturePath string

	// RotateSizeMB is the minimum file size in megabytes that triggers a
	// rotation. Defaults to 5 if zero.
	RotateSizeMB int

	// PollInterval is how long to sleep between shipper iterations.
	// Defaults to 30 seconds if zero.
	PollInterval time.Duration

	// Client is the MinIO client used for object uploads.
	// Tests pass minioclient.NewFake(); production passes minioclient.New(...).
	Client minioclient.Client
}

const (
	defaultRotateSizeMB = 5
	defaultPollInterval = 30 * time.Second
)

func (c *Config) rotateSizeBytes() int64 {
	if c.RotateSizeMB <= 0 {
		return int64(defaultRotateSizeMB) * 1024 * 1024
	}
	return int64(c.RotateSizeMB) * 1024 * 1024
}

func (c *Config) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return defaultPollInterval
	}
	return c.PollInterval
}

// shippingPath returns the path of the in-transit file (CapturePath + ".shipping").
func (c *Config) shippingPath() string {
	return c.CapturePath + ".shipping"
}

// Run blocks until ctx is cancelled, executing the shipping loop on every tick.
//
// Each iteration:
//  1. Crash recovery: if <CapturePath>.shipping exists, upload it now.
//  2. Stat CapturePath; if it does not exist or is smaller than RotateSizeMB, sleep.
//  3. Atomic os.Rename to <CapturePath>.shipping.
//  4. Run the upload sequence on the .shipping file.
//  5. Sleep until the next tick.
//
// Run returns nil when ctx is cancelled.
func Run(ctx context.Context, cfg Config) error {
	ticker := time.NewTicker(cfg.pollInterval())
	defer ticker.Stop()

	shippingPath := cfg.shippingPath()
	rotateSizeBytes := cfg.rotateSizeBytes()

	for {
		// Run one iteration immediately, then wait for the next tick.
		runOnce(ctx, cfg, shippingPath, rotateSizeBytes)

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// next iteration
		}
	}
}

// runOnce executes a single shipper iteration.
func runOnce(ctx context.Context, cfg Config, shippingPath string, rotateSizeBytes int64) {
	// Step 1: crash recovery.
	recoverShipping(ctx, cfg.Client, cfg.Machine, shippingPath)

	// Step 2: stat the capture file.
	info, err := os.Stat(cfg.CapturePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// No capture file yet — nothing to ship.
			return
		}
		slog.Warn("ship: stat capture file",
			slog.String("path", cfg.CapturePath),
			slog.Any("error", err),
		)
		return
	}

	if info.Size() < rotateSizeBytes {
		// File too small — wait for more data.
		return
	}

	// Step 3: atomic rename.
	if err := os.Rename(cfg.CapturePath, shippingPath); err != nil {
		slog.Warn("ship: rename to .shipping",
			slog.String("from", cfg.CapturePath),
			slog.String("to", shippingPath),
			slog.Any("error", err),
		)
		return
	}

	// Step 4: upload the .shipping file.
	// On failure the file is left in place; the next iteration's crash recovery
	// will pick it up and retry.
	if err := uploadAndRemove(ctx, cfg.Client, cfg.Machine, shippingPath); err != nil {
		slog.Warn("ship: upload failed; will retry next cycle",
			slog.String("path", shippingPath),
			slog.Any("error", err),
		)
	}
}
