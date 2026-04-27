package minioclient_test

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // MD5 used for ETag matching per S3 protocol
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/caffeaun/marc/internal/minioclient"
)

// helpers

func md5hex(data []byte) string {
	//nolint:gosec
	sum := md5.Sum(data)
	return fmt.Sprintf("%x", sum)
}

func mustPut(t *testing.T, f *minioclient.Fake, key string, body []byte) {
	t.Helper()
	err := f.PutObject(context.Background(), key, bytes.NewReader(body), int64(len(body)), md5hex(body))
	if err != nil {
		t.Fatalf("PutObject(%q): %v", key, err)
	}
}

// TestFakePutGetRoundTrip verifies that bytes written via PutObject are returned
// byte-for-byte by GetObject.
func TestFakePutGetRoundTrip(t *testing.T) {
	f := minioclient.NewFake()
	payload := []byte("hello, world")

	mustPut(t, f, "some/key.jsonl", payload)

	rc, err := f.GetObject(context.Background(), "some/key.jsonl")
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("round-trip mismatch: got %q want %q", got, payload)
	}
}

// TestFakePutMismatchedMD5 verifies that a wrong md5hex causes ErrETagMismatch.
func TestFakePutMismatchedMD5(t *testing.T) {
	f := minioclient.NewFake()
	body := []byte("data")
	wrongMD5 := "00000000000000000000000000000000"

	err := f.PutObject(context.Background(), "k", bytes.NewReader(body), int64(len(body)), wrongMD5)
	if err == nil {
		t.Fatal("expected ErrETagMismatch, got nil")
	}
	if !errors.Is(err, minioclient.ErrETagMismatch) {
		t.Errorf("expected ErrETagMismatch in chain, got: %v", err)
	}
}

// TestFakePutErrHook verifies that PutErr is returned by PutObject.
func TestFakePutErrHook(t *testing.T) {
	f := minioclient.NewFake()
	f.PutErr = errors.New("simulated put failure")
	body := []byte("x")

	err := f.PutObject(context.Background(), "k", bytes.NewReader(body), 1, md5hex(body))
	if err == nil {
		t.Fatal("expected error from PutErr hook, got nil")
	}
}

// TestFakeMoveObjectSuccess verifies that MoveObject moves the key.
func TestFakeMoveObjectSuccess(t *testing.T) {
	f := minioclient.NewFake()
	mustPut(t, f, "src/obj", []byte("payload"))

	if err := f.MoveObject(context.Background(), "src/obj", "dst/obj"); err != nil {
		t.Fatalf("MoveObject: %v", err)
	}

	// Source must be gone.
	keys := f.Keys()
	for _, k := range keys {
		if k == "src/obj" {
			t.Error("source key still present after move")
		}
	}

	// Destination must be present with original content.
	data := f.Get("dst/obj")
	if !bytes.Equal(data, []byte("payload")) {
		t.Errorf("dst content = %q, want %q", data, "payload")
	}
}

// TestFakeMoveObjectCopyError verifies that when MoveErr is set, the source is preserved.
func TestFakeMoveObjectCopyError(t *testing.T) {
	f := minioclient.NewFake()
	mustPut(t, f, "src/obj", []byte("safe"))
	f.MoveErr = errors.New("copy failed")

	err := f.MoveObject(context.Background(), "src/obj", "dst/obj")
	if err == nil {
		t.Fatal("expected error from MoveErr hook, got nil")
	}

	// Source must still exist.
	data := f.Get("src/obj")
	if !bytes.Equal(data, []byte("safe")) {
		t.Error("source was modified or deleted despite copy failure")
	}

	// Destination must not exist.
	data2 := f.Get("dst/obj")
	if data2 != nil {
		t.Error("destination was written despite copy failure")
	}
}

// TestFakeListObjectsRespectsCursorAndPrefix verifies filtering and cursor semantics.
func TestFakeListObjectsRespectsCursorAndPrefix(t *testing.T) {
	f := minioclient.NewFake()
	keys := []string{
		"raw/machine/2024/01/01/00/a.jsonl",
		"raw/machine/2024/01/01/00/b.jsonl",
		"raw/machine/2024/01/01/00/c.jsonl",
		"other/prefix/x.jsonl",
	}
	for _, k := range keys {
		mustPut(t, f, k, []byte("x"))
	}

	// List with prefix and afterKey cursor.
	got, err := f.ListObjects(context.Background(), "raw/machine/", "raw/machine/2024/01/01/00/a.jsonl")
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}

	want := []string{
		"raw/machine/2024/01/01/00/b.jsonl",
		"raw/machine/2024/01/01/00/c.jsonl",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

// TestFakeListObjectsLexicographicOrder verifies lexicographic ordering of returned keys.
func TestFakeListObjectsLexicographicOrder(t *testing.T) {
	f := minioclient.NewFake()
	// Insert in non-sorted order.
	for _, k := range []string{"p/c", "p/a", "p/b"} {
		mustPut(t, f, k, []byte("v"))
	}

	got, err := f.ListObjects(context.Background(), "p/", "")
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}

	want := []string{"p/a", "p/b", "p/c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

// TestFakePingNilByDefault verifies Ping returns nil when no error is injected.
func TestFakePingNilByDefault(t *testing.T) {
	f := minioclient.NewFake()
	if err := f.Ping(context.Background()); err != nil {
		t.Errorf("Ping() = %v, want nil", err)
	}
}

// TestFakePingErrInjection verifies that PingErr is returned by Ping.
func TestFakePingErrInjection(t *testing.T) {
	f := minioclient.NewFake()
	f.PingErr = minioclient.ErrDNSResolution
	if err := f.Ping(context.Background()); !errors.Is(err, minioclient.ErrDNSResolution) {
		t.Errorf("Ping() = %v, want ErrDNSResolution", err)
	}
}

// TestFakeGetNonExistentKey verifies GetObject returns an error for unknown keys.
func TestFakeGetNonExistentKey(t *testing.T) {
	f := minioclient.NewFake()
	_, err := f.GetObject(context.Background(), "does/not/exist")
	if err == nil {
		t.Fatal("expected error for non-existent key, got nil")
	}
}

// TestFakeConcurrentPuts verifies that concurrent PutObject calls are safe under
// the race detector. Each goroutine writes a unique key with matching MD5.
func TestFakeConcurrentPuts(t *testing.T) {
	f := minioclient.NewFake()
	const numGoroutines = 50
	const bodySize = 1024 // 1 KB per put

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := range numGoroutines {
		go func() {
			defer wg.Done()
			// Build a unique body for each goroutine.
			body := []byte(strings.Repeat(fmt.Sprintf("%04d", i), bodySize/4))
			key := fmt.Sprintf("concurrent/%04d", i)
			h := md5hex(body)

			ctx := context.Background()
			if err := f.PutObject(ctx, key, bytes.NewReader(body), int64(len(body)), h); err != nil {
				// Report with t.Errorf (not Fatalf) so other goroutines can finish.
				t.Errorf("goroutine %d: PutObject: %v", i, err)
			}
		}()
	}
	wg.Wait()

	// Verify all keys are present.
	keys := f.Keys()
	if len(keys) != numGoroutines {
		t.Errorf("expected %d keys after concurrent puts, got %d", numGoroutines, len(keys))
	}
}

// TestSentinelErrors verifies that sentinel error values are exported and usable
// with errors.Is.
func TestSentinelErrors(t *testing.T) {
	sentinels := []error{
		minioclient.ErrETagMismatch,
		minioclient.ErrBucketNotFound,
		minioclient.ErrAuthFailed,
		minioclient.ErrDNSResolution,
		minioclient.ErrTLSVerification,
	}
	for _, s := range sentinels {
		wrapped := fmt.Errorf("outer: %w", s)
		if !errors.Is(wrapped, s) {
			t.Errorf("errors.Is(wrapped, %v) = false, want true", s)
		}
	}
}
