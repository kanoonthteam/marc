package client

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"

	"github.com/caffeaun/marc/internal/minioclient"
)

// ValidationResult records the outcome of one of the four MinIO validation steps.
type ValidationResult struct {
	Step    string // "DNS", "TLS", "Auth", "Bucket"
	Passed  bool
	Message string // human-readable outcome
}

// ValidateMinIO runs all four validation steps against the given minioclient.Config.
// It always returns a slice of length 4 (one entry per step). Steps that cannot
// run because a preceding step failed are marked as Skipped (Passed=false,
// Message starting with "skipped:").
//
// The newClient argument is the injection point for tests: pass minioclient.New
// in production or a stub-returning function in unit tests.
func ValidateMinIO(
	ctx context.Context,
	cfg minioclient.Config,
	newClient func(minioclient.Config) (minioclient.Client, error),
) []ValidationResult {
	results := make([]ValidationResult, 4)

	// Step 1 — DNS resolution.
	results[0] = validateDNS(ctx, cfg.Endpoint)

	// Step 2 — TLS handshake (only meaningful when step 1 passed).
	if !results[0].Passed {
		results[1] = ValidationResult{
			Step:    "TLS",
			Passed:  false,
			Message: "skipped: DNS resolution failed",
		}
	} else {
		results[1] = validateTLS(cfg.Endpoint, cfg.VerifyTLS)
	}

	// Steps 3 and 4 share the same Ping() call. Run only when DNS passed;
	// if TLS failed we still attempt auth (the user may have verify_tls=false).
	if !results[0].Passed {
		results[2] = ValidationResult{
			Step:    "Auth",
			Passed:  false,
			Message: "skipped: DNS resolution failed",
		}
		results[3] = ValidationResult{
			Step:    "Bucket",
			Passed:  false,
			Message: "skipped: DNS resolution failed",
		}
		return results
	}

	results[2], results[3] = validateAuthAndBucket(ctx, cfg, newClient)
	return results
}

// validateDNS resolves the hostname embedded in endpointURL.
func validateDNS(ctx context.Context, endpointURL string) ValidationResult {
	host, err := extractHost(endpointURL)
	if err != nil {
		return ValidationResult{
			Step:    "DNS",
			Passed:  false,
			Message: fmt.Sprintf("DNS resolution failed: could not parse endpoint URL: %v", err),
		}
	}

	// LookupHost returns at least one address on success.
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil || len(addrs) == 0 {
		msg := fmt.Sprintf("DNS resolution failed for %s", host)
		if err != nil {
			msg = fmt.Sprintf("DNS resolution failed for %s: %v", host, err)
		}
		return ValidationResult{Step: "DNS", Passed: false, Message: msg}
	}

	return ValidationResult{
		Step:    "DNS",
		Passed:  true,
		Message: fmt.Sprintf("DNS resolved %s -> %s", host, addrs[0]),
	}
}

// validateTLS performs a TLS handshake against the endpoint.
// When the scheme is http or verify_tls is false, the step is skipped.
func validateTLS(endpointURL string, verifyTLS bool) ValidationResult {
	u, err := url.Parse(endpointURL)
	if err != nil {
		return ValidationResult{
			Step:    "TLS",
			Passed:  false,
			Message: fmt.Sprintf("TLS verification failed: cannot parse endpoint: %v", err),
		}
	}

	if u.Scheme == "http" {
		return ValidationResult{
			Step:    "TLS",
			Passed:  true,
			Message: "skipped: http endpoint, no TLS to verify",
		}
	}

	if !verifyTLS {
		return ValidationResult{
			Step:    "TLS",
			Passed:  true,
			Message: "skipped: verify_tls=false; skipped",
		}
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "443"
	}
	addr := net.JoinHostPort(host, port)

	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host}) //nolint:gosec // purposefully strict here
	if err != nil {
		return ValidationResult{
			Step:    "TLS",
			Passed:  false,
			Message: fmt.Sprintf("TLS verification failed: %v", err),
		}
	}
	_ = conn.Close()

	return ValidationResult{
		Step:    "TLS",
		Passed:  true,
		Message: fmt.Sprintf("TLS verified for %s", host),
	}
}

// validateAuthAndBucket calls Ping once and maps the result to two steps.
// Step 3 tests authentication; step 4 tests bucket writability.
func validateAuthAndBucket(
	ctx context.Context,
	cfg minioclient.Config,
	newClient func(minioclient.Config) (minioclient.Client, error),
) (authResult, bucketResult ValidationResult) {
	authResult = ValidationResult{Step: "Auth"}
	bucketResult = ValidationResult{Step: "Bucket"}

	client, err := newClient(cfg)
	if err != nil {
		// Construction failure is treated as an auth-level failure because
		// it typically means bad credentials or an unreachable endpoint.
		authResult.Passed = false
		authResult.Message = fmt.Sprintf("authentication failed: could not create client: %v", err)
		bucketResult.Passed = false
		bucketResult.Message = "skipped: auth client creation failed"
		return
	}

	pingErr := client.Ping(ctx)
	if pingErr == nil {
		authResult.Passed = true
		authResult.Message = "credentials accepted"
		bucketResult.Passed = true
		bucketResult.Message = "bucket is writable"
		return
	}

	// Map the sentinel error to the appropriate step.
	switch {
	case errors.Is(pingErr, minioclient.ErrDNSResolution):
		// Unlikely at this point (DNS already passed), but handle defensively.
		authResult.Passed = false
		authResult.Message = "skipped: DNS resolution error (already detected)"
		bucketResult.Passed = false
		bucketResult.Message = "skipped: DNS resolution error"

	case errors.Is(pingErr, minioclient.ErrAuthFailed):
		authResult.Passed = false
		authResult.Message = fmt.Sprintf("authentication failed: %v", pingErr)
		bucketResult.Passed = false
		bucketResult.Message = "skipped: authentication failed"

	case errors.Is(pingErr, minioclient.ErrBucketNotFound):
		// Auth succeeded (server responded with a recognisable error), bucket is missing.
		authResult.Passed = true
		authResult.Message = "credentials accepted"
		bucketResult.Passed = false
		bucketResult.Message = fmt.Sprintf("bucket %s not found", cfg.Bucket)

	default:
		// Generic error after auth was apparently OK.
		authResult.Passed = true
		authResult.Message = "credentials accepted (inferred from non-auth error)"
		bucketResult.Passed = false
		bucketResult.Message = fmt.Sprintf("bucket not writable: %v", pingErr)
	}

	return
}

// extractHost parses an endpoint URL and returns the hostname (no port).
// For bare hostnames without a scheme, it returns the string as-is.
func extractHost(endpointURL string) (string, error) {
	u, err := url.Parse(endpointURL)
	if err != nil {
		return "", err
	}
	host := u.Hostname()
	if host == "" {
		// No scheme — treat the whole string as a hostname.
		return endpointURL, nil
	}
	return host, nil
}
