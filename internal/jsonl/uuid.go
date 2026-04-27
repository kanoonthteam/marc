package jsonl

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// NewUUIDv4 generates a random UUID v4 using crypto/rand.
// Format: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
// where y is one of 8, 9, a, or b (variant bits = 10xx).
// No external dependencies — pure stdlib.
func NewUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating random bytes for UUID: %w", err)
	}

	// Set version to 4 (bits 12-15 of time_hi_and_version = 0100).
	b[6] = (b[6] & 0x0f) | 0x40

	// Set variant to 10xx (bits 6-7 of clock_seq_hi_and_reserved).
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	), nil
}
