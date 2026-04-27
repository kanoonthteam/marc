package minioclient

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // MD5 is used here for ETag matching, not security
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

// Fake is an in-memory implementation of Client for use in tests.
// It stores objects in a map[string][]byte guarded by a sync.Mutex so it is
// safe for concurrent callers (verified by go test -race).
//
// Error injection is supported via the exported hook fields. Setting any hook
// field to a non-nil error causes that method to return the error immediately,
// without performing any state mutation.
type Fake struct {
	mu   sync.Mutex
	objs map[string][]byte

	// PutErr, if non-nil, is returned by PutObject (after body is read and MD5 checked).
	PutErr error

	// GetErr, if non-nil, is returned by GetObject.
	GetErr error

	// MoveErr, if non-nil, is returned by MoveObject.
	MoveErr error

	// ListErr, if non-nil, is returned by ListObjects.
	ListErr error

	// PingErr, if non-nil, is returned by Ping.
	PingErr error
}

// NewFake returns an empty, ready-to-use Fake.
func NewFake() *Fake {
	return &Fake{
		objs: make(map[string][]byte),
	}
}

// PutObject stores the body at key and verifies the MD5 of the received bytes
// against md5hex. If PutErr is set, the body is still read but the error is
// returned. If md5hex is non-empty and does not match the received data, an
// ErrETagMismatch-wrapped error is returned.
func (f *Fake) PutObject(_ context.Context, key string, r io.Reader, _ int64, md5hex string) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("fake minioclient: read body for %s: %w", key, err)
	}

	if f.PutErr != nil {
		return f.PutErr
	}

	if md5hex != "" {
		//nolint:gosec // MD5 is used for ETag matching, consistent with S3 protocol
		computed := fmt.Sprintf("%x", md5.Sum(data))
		if !strings.EqualFold(computed, md5hex) {
			return fmt.Errorf("%w: expected %s got %s", ErrETagMismatch, md5hex, computed)
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.objs[key] = data
	return nil
}

// GetObject returns the bytes stored at key wrapped in an io.ReadCloser.
// Returns an error if the key does not exist or if GetErr is set.
func (f *Fake) GetObject(_ context.Context, key string) (io.ReadCloser, error) {
	if f.GetErr != nil {
		return nil, f.GetErr
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	data, ok := f.objs[key]
	if !ok {
		return nil, fmt.Errorf("fake minioclient: key not found: %s", key)
	}
	// Return a copy so callers cannot mutate internal state.
	cp := make([]byte, len(data))
	copy(cp, data)
	return io.NopCloser(bytes.NewReader(cp)), nil
}

// MoveObject copies srcKey to dstKey and removes srcKey. Returns MoveErr if set.
// If srcKey does not exist, an error is returned without modifying dstKey.
func (f *Fake) MoveObject(_ context.Context, srcKey, dstKey string) error {
	if f.MoveErr != nil {
		// Simulate copy failure: source must be preserved, so do nothing.
		return f.MoveErr
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	data, ok := f.objs[srcKey]
	if !ok {
		return fmt.Errorf("fake minioclient: src key not found for move: %s", srcKey)
	}

	cp := make([]byte, len(data))
	copy(cp, data)
	f.objs[dstKey] = cp
	delete(f.objs, srcKey)
	return nil
}

// ListObjects returns all keys with the given prefix that sort after afterKey,
// in lexicographic order. Returns ListErr if set.
func (f *Fake) ListObjects(_ context.Context, prefix, afterKey string) ([]string, error) {
	if f.ListErr != nil {
		return nil, f.ListErr
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	var keys []string
	for k := range f.objs {
		if strings.HasPrefix(k, prefix) && k > afterKey {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// Ping returns PingErr if set, otherwise nil.
func (f *Fake) Ping(_ context.Context) error {
	return f.PingErr
}

// Keys returns a sorted snapshot of all keys currently stored in the fake.
// Useful for test assertions.
func (f *Fake) Keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	keys := make([]string, 0, len(f.objs))
	for k := range f.objs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Get returns the raw bytes stored at key, or nil if the key does not exist.
// Useful for test assertions without using GetObject.
func (f *Fake) Get(key string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.objs[key]
	if !ok {
		return nil
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	return cp
}
