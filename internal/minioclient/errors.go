package minioclient

import "errors"

// Sentinel errors returned by the minioclient package.
// Callers may use errors.Is and errors.As to distinguish failure modes.
var (
	// ErrETagMismatch is returned when the ETag returned by MinIO after a PUT
	// does not match the locally computed MD5 hex digest.
	ErrETagMismatch = errors.New("etag mismatch")

	// ErrBucketNotFound is returned when the configured bucket does not exist.
	ErrBucketNotFound = errors.New("bucket not found")

	// ErrAuthFailed is returned when the supplied credentials are rejected by
	// MinIO (HTTP 401/403, AccessDenied, SignatureDoesNotMatch, etc.).
	ErrAuthFailed = errors.New("authentication failed")

	// ErrDNSResolution is returned when the MinIO endpoint hostname cannot be
	// resolved via DNS.
	ErrDNSResolution = errors.New("dns resolution failed")

	// ErrTLSVerification is returned when the TLS certificate presented by the
	// MinIO server cannot be verified against the system trust store.
	ErrTLSVerification = errors.New("tls verification failed")
)
