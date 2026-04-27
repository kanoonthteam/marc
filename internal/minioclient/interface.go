package minioclient

import (
	"context"
	"io"
)

// Client is the surface other packages depend on. Defined as an interface so
// tests can substitute a Fake without spinning up a real MinIO instance.
type Client interface {
	// PutObject uploads r (of size bytes) at key and verifies the returned
	// ETag matches md5hex. Returns ErrETagMismatch on a digest mismatch.
	PutObject(ctx context.Context, key string, r io.Reader, size int64, md5hex string) error

	// GetObject returns an io.ReadCloser for the object at key. The caller
	// must close the returned reader.
	GetObject(ctx context.Context, key string) (io.ReadCloser, error)

	// MoveObject copies srcKey to dstKey within the same bucket, then removes
	// srcKey. If the copy fails, srcKey is preserved and the error is returned
	// without attempting the delete.
	MoveObject(ctx context.Context, srcKey, dstKey string) error

	// ListObjects returns all object keys under prefix that sort after afterKey
	// in lexicographic (S3) order.
	ListObjects(ctx context.Context, prefix, afterKey string) ([]string, error)

	// Ping performs a test PUT + DELETE on a _marc-config-test/ key and returns
	// a descriptive error distinguishing DNS failure, TLS failure, auth failure,
	// and bucket-not-found from other errors.
	Ping(ctx context.Context) error
}

// Config holds the parameters required to construct a production Client.
type Config struct {
	// Endpoint is the full URL of the MinIO/S3 server, e.g. "https://s3.example.com".
	Endpoint string

	// Bucket is the bucket name that all operations target.
	Bucket string

	// AccessKey is the S3 access key ID.
	AccessKey string

	// SecretKey is the S3 secret access key.
	SecretKey string

	// VerifyTLS controls whether the server's TLS certificate is verified.
	// Set to false only in development or when using self-signed certificates.
	VerifyTLS bool
}
