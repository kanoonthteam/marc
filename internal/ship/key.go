package ship

import (
	"fmt"
	"time"

	"github.com/caffeaun/marc/internal/jsonl"
)

// formatKey returns the MinIO object key for a shipping file.
//
// Format: raw/<machine>/<YYYY>/<MM>/<DD>/<HH>/<machine>-<unix_ts>-<uuid>.jsonl
//
// The key begins with "raw/" (not "marc/raw/") because the bucket name "marc"
// is the bucket parameter passed to PutObject; the key is the object path
// within the bucket.
//
// Keys are lexicographically sortable by date then timestamp, which allows the
// server-side processor to use a simple cursor.
func formatKey(machine string, t time.Time) (string, error) {
	uuid, err := jsonl.NewUUIDv4()
	if err != nil {
		return "", fmt.Errorf("ship: generate uuid for key: %w", err)
	}
	return fmt.Sprintf(
		"raw/%s/%04d/%02d/%02d/%02d/%s-%d-%s.jsonl",
		machine,
		t.Year(), t.Month(), t.Day(), t.Hour(),
		machine, t.Unix(), uuid,
	), nil
}
