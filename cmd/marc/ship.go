package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caffeaun/marc/internal/config"
	"github.com/caffeaun/marc/internal/minioclient"
	"github.com/caffeaun/marc/internal/ship"
	"github.com/spf13/cobra"
)

var shipCmd = &cobra.Command{
	Use:   "ship",
	Short: "Run the upload daemon (rotates capture.jsonl and ships to MinIO)",
	Long: `ship polls every 30 seconds. When capture.jsonl reaches 5 MB it atomically
renames the file to capture.jsonl.shipping, uploads it to MinIO, verifies the
ETag, and removes the local copy on success.

On startup it first checks for an existing capture.jsonl.shipping file left
by a previous crash and uploads it before entering the normal poll loop.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadClient(configFile)
		if err != nil {
			return fmt.Errorf("marc ship: load config: %w", err)
		}

		client, err := minioclient.New(minioclient.Config{
			Endpoint:  cfg.MinIO.Endpoint,
			Bucket:    cfg.MinIO.Bucket,
			AccessKey: cfg.MinIO.AccessKey,
			SecretKey: cfg.MinIO.SecretKey,
			VerifyTLS: cfg.MinIO.VerifyTLS,
		})
		if err != nil {
			return fmt.Errorf("marc ship: create minio client: %w", err)
		}

		shipCfg := ship.Config{
			Machine:      cfg.MachineName,
			CapturePath:  cfg.Paths.CaptureFile,
			RotateSizeMB: cfg.Shipper.RotateSizeMB,
			PollInterval: time.Duration(cfg.Shipper.ShipIntervalSeconds) * time.Second,
			Client:       client,
		}

		// Wire SIGINT/SIGTERM to context cancellation for graceful shutdown.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		slog.Info("marc ship: starting",
			slog.String("capture", shipCfg.CapturePath),
			slog.Int("rotate_mb", shipCfg.RotateSizeMB),
			slog.Duration("interval", shipCfg.PollInterval),
		)

		return ship.Run(ctx, shipCfg)
	},
}
