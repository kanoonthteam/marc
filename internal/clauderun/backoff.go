package clauderun

import (
	"math/rand"
	"time"
)

// jitteredBackoff returns a decorrelated backoff delay for a 1-based attempt:
//
//	min(base * 2^(attempt-1), cap) + jitter
//
// where jitter is uniform in [0, jitterRatio*delay). The jitter decorrelates
// multiple marc sessions hitting the same crash so they don't all retry at the
// same instant. Ported from NousResearch/hermes-agent's agent.retry_utils
// jittered_backoff (base 5s, cap 120s, jitter_ratio 0.5).
func jitteredBackoff(attempt int, base, maxDelay time.Duration, jitterRatio float64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if base <= 0 {
		base = 5 * time.Second
	}
	if maxDelay <= 0 {
		maxDelay = 120 * time.Second
	}
	if jitterRatio < 0 {
		jitterRatio = 0
	}

	exponent := attempt - 1
	var delay time.Duration
	// Overflow guard: 2^exponent overflows int64 around exponent 63.
	if exponent >= 62 {
		delay = maxDelay
	} else {
		scaled := base * time.Duration(int64(1)<<uint(exponent))
		if scaled <= 0 || scaled > maxDelay { // <=0 catches multiplication overflow
			delay = maxDelay
		} else {
			delay = scaled
		}
	}

	if jitterRatio > 0 {
		// Fresh source each call; this is cooldown timing, not crypto.
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		delay += time.Duration(r.Float64() * jitterRatio * float64(delay))
	}
	return delay
}
