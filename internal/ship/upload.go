package ship

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // MD5 used for ETag matching per S3 protocol
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/caffeaun/marc/internal/minioclient"
)

// uploadAndRemove performs the full upload sequence for a .shipping file:
//
//  1. Open the file and read its contents into memory.
//  2. Compute the MD5 hex digest.
//  3. Build the MinIO object key from the machine name and current UTC time.
//  4. PutObject to MinIO (which verifies ETag on success).
//  5. On success: remove the local .shipping file and log success.
//  6. On failure: leave the .shipping file in place and return the error.
//
// The caller is responsible for deciding what to do with the error.
func uploadAndRemove(ctx context.Context, client minioclient.Client, machine, shippingPath string) error {
	// Step 1: read the whole file into memory.
	// Files are 5-50 MB (RotateSizeMB default 5 MB, plus any traffic burst).
	// Reading into memory is fine for this size range and lets us compute MD5
	// and feed bytes.NewReader to PutObject without a second open/seek.
	data, err := os.ReadFile(shippingPath)
	if err != nil {
		return fmt.Errorf("ship: read %s: %w", shippingPath, err)
	}

	// Step 2: compute MD5 (lowercase hex) for ETag verification.
	//nolint:gosec // MD5 is mandated by the S3/MinIO ETag protocol
	sum := md5.Sum(data)
	md5hex := fmt.Sprintf("%x", sum)

	// Step 3: build the object key.
	key, err := formatKey(machine, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("ship: build key: %w", err)
	}

	// Step 4: PUT to MinIO. PutObject verifies ETag on its own and returns
	// ErrETagMismatch if the server's ETag does not match md5hex.
	if err := client.PutObject(ctx, key, bytes.NewReader(data), int64(len(data)), md5hex); err != nil {
		// Leave shippingPath in place so the next cycle retries.
		return fmt.Errorf("ship: put %s -> %s: %w", shippingPath, key, err)
	}

	// Step 5: upload verified — remove the local copy.
	if err := os.Remove(shippingPath); err != nil {
		// Log but do not treat this as fatal; the file will be re-uploaded on
		// the next cycle if it still exists (duplicate upload is harmless
		// because ReplacingMergeTree deduplicates on event_id).
		slog.Warn("ship: remove shipping file after upload",
			slog.String("path", shippingPath),
			slog.Any("error", err),
		)
		return fmt.Errorf("ship: remove %s after successful upload: %w", shippingPath, err)
	}

	slog.Info("ship: uploaded",
		slog.String("key", key),
		slog.Int("bytes", len(data)),
		slog.String("md5", md5hex),
	)
	return nil
}
