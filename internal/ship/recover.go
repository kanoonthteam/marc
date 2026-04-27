package ship

import (
	"context"
	"log/slog"
	"os"

	"github.com/caffeaun/marc/internal/minioclient"
)

// recoverShipping checks whether a .shipping file exists and, if so, uploads
// it before the normal poll loop begins.
//
// This handles the crash-recovery case: if the shipper was killed after the
// atomic rename but before the upload completed (or before the local file was
// removed), the .shipping file contains data that must be uploaded.
//
// recoverShipping is idempotent and safe to call on every startup.
func recoverShipping(ctx context.Context, client minioclient.Client, machine, shippingPath string) {
	if _, err := os.Stat(shippingPath); os.IsNotExist(err) {
		return // nothing to recover
	}

	slog.Info("ship: crash recovery — uploading existing .shipping file",
		slog.String("path", shippingPath),
	)

	if err := uploadAndRemove(ctx, client, machine, shippingPath); err != nil {
		slog.Warn("ship: crash recovery upload failed; will retry next cycle",
			slog.String("path", shippingPath),
			slog.Any("error", err),
		)
	}
}
