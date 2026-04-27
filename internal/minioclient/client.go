//go:build !unit

// client.go contains the production minio-go-backed implementation of Client.
// It is excluded from unit test builds (via the "unit" build tag) so that
// the minio-go assembly dependencies (md5-simd, sha256-simd, klauspost/cpuid)
// do not cause issues on darwin/arm64 during local development.
// Unit tests use Fake exclusively; integration tests run on the Ubuntu server.

package minioclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// productionClient is the minio-go-backed implementation of Client.
type productionClient struct {
	mc     *minio.Client
	bucket string
	host   string // hostname extracted from Endpoint, used for error messages
}

// New constructs the production Client backed by minio-go.
//
// Endpoint is parsed to extract the host and whether TLS is in use. When
// VerifyTLS is false and the scheme is https, a custom transport with
// InsecureSkipVerify is used; otherwise the default transport is used.
func New(cfg Config) (Client, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("minioclient: parse endpoint %q: %w", cfg.Endpoint, err)
	}

	secure := u.Scheme == "https"
	host := u.Host
	if host == "" {
		// Bare host without scheme falls through to minio.New as-is.
		host = cfg.Endpoint
	}

	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
	}

	if secure && !cfg.VerifyTLS {
		opts.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // intentional per config
		}
	}

	mc, err := minio.New(host, opts)
	if err != nil {
		return nil, fmt.Errorf("minioclient: new client: %w", err)
	}

	return &productionClient{
		mc:     mc,
		bucket: cfg.Bucket,
		host:   host,
	}, nil
}

// PutObject uploads r to key and verifies the ETag against md5hex.
func (c *productionClient) PutObject(ctx context.Context, key string, r io.Reader, size int64, md5hex string) error {
	info, err := c.mc.PutObject(ctx, c.bucket, key, r, size, minio.PutObjectOptions{
		ContentType: "application/x-ndjson",
	})
	if err != nil {
		return fmt.Errorf("minioclient: put %s: %w", key, err)
	}

	// minio-go strips surrounding quotes from the ETag before returning it.
	// Compare case-insensitively to be safe.
	if !strings.EqualFold(info.ETag, md5hex) {
		return fmt.Errorf("%w: expected %s got %s", ErrETagMismatch, md5hex, info.ETag)
	}
	return nil
}

// GetObject returns an io.ReadCloser for the object at key.
func (c *productionClient) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("minioclient: get %s: %w", key, err)
	}
	return obj, nil
}

// MoveObject copies srcKey to dstKey, then removes srcKey on success.
// If the copy fails, srcKey is left intact and the error is returned.
func (c *productionClient) MoveObject(ctx context.Context, srcKey, dstKey string) error {
	_, err := c.mc.CopyObject(ctx,
		minio.CopyDestOptions{Bucket: c.bucket, Object: dstKey},
		minio.CopySrcOptions{Bucket: c.bucket, Object: srcKey},
	)
	if err != nil {
		// Do NOT delete the source — preserve the original on copy failure.
		return fmt.Errorf("minioclient: copy %s -> %s: %w", srcKey, dstKey, err)
	}

	if err := c.mc.RemoveObject(ctx, c.bucket, srcKey, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("minioclient: remove source %s after copy: %w", srcKey, err)
	}
	return nil
}

// ListObjects lists all keys under prefix that sort after afterKey.
// Keys are returned in lexicographic order (S3/MinIO contract).
func (c *productionClient) ListObjects(ctx context.Context, prefix, afterKey string) ([]string, error) {
	opts := minio.ListObjectsOptions{
		Prefix:     prefix,
		StartAfter: afterKey,
		Recursive:  true,
	}

	var keys []string
	for obj := range c.mc.ListObjects(ctx, c.bucket, opts) {
		if obj.Err != nil {
			return nil, fmt.Errorf("minioclient: list %s: %w", prefix, obj.Err)
		}
		keys = append(keys, obj.Key)
	}
	return keys, nil
}

// Ping performs a test PUT + DELETE on a _marc-config-test/ key and returns a
// descriptive error for each class of failure.
func (c *productionClient) Ping(ctx context.Context) error {
	// Generate a unique key to avoid collisions with concurrent pings.
	key := fmt.Sprintf("_marc-config-test/ping-%d-%d",
		time.Now().UnixNano(),
		rand.Int64(), //nolint:gosec // non-crypto random is fine for a ping key
	)

	payload := strings.NewReader("1")
	_, err := c.mc.PutObject(ctx, c.bucket, key, payload, 1, minio.PutObjectOptions{
		ContentType: "text/plain",
	})
	if err != nil {
		return c.classifyError(err)
	}

	// Best-effort delete; failure here is non-critical.
	_ = c.mc.RemoveObject(ctx, c.bucket, key, minio.RemoveObjectOptions{})
	return nil
}

// classifyError maps minio-go errors to the package-level sentinel errors.
func (c *productionClient) classifyError(err error) error {
	msg := err.Error()

	// DNS resolution failure — no such host.
	if strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "Name or service not known") ||
		strings.Contains(msg, "dial tcp: lookup") {
		return fmt.Errorf("%w for %s: %w", ErrDNSResolution, c.host, err)
	}

	// TLS verification failure — Go 1.20+ wraps as *tls.CertificateVerificationError;
	// fall back to string matching for older environments.
	var tlsErr *tls.CertificateVerificationError
	if errors.As(err, &tlsErr) || strings.Contains(msg, "x509:") {
		return fmt.Errorf("%w: %w", ErrTLSVerification, err)
	}

	// Auth failure — minio-go surfaces HTTP 401/403 as ErrorResponse with codes
	// such as "AccessDenied" or "SignatureDoesNotMatch".
	var minioErr minio.ErrorResponse
	if errors.As(err, &minioErr) {
		switch minioErr.Code {
		case "AccessDenied", "SignatureDoesNotMatch", "InvalidAccessKeyId",
			"InvalidSecretAccessKey":
			return fmt.Errorf("%w: %w", ErrAuthFailed, err)
		case "NoSuchBucket":
			return fmt.Errorf("%w: %s: %w", ErrBucketNotFound, c.bucket, err)
		}
	}

	// Fallback: wrap the original error with context.
	return fmt.Errorf("minioclient: ping: %w", err)
}
