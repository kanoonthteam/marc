package proxy

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// healthState tracks proxy observability metrics for /_marc/health.
// Counters are updated via sync/atomic; timestamp/message fields are guarded
// by mu so the snapshot read sees a consistent (time, message) pair.
type healthState struct {
	forwardedTotal atomic.Uint64
	failedTotal    atomic.Uint64

	mu            sync.RWMutex
	lastSuccessAt time.Time
	lastErrorAt   time.Time
	lastErrorMsg  string
}

func newHealthState() *healthState {
	return &healthState{}
}

func (s *healthState) recordSuccess() {
	s.forwardedTotal.Add(1)
	s.mu.Lock()
	s.lastSuccessAt = time.Now().UTC()
	s.mu.Unlock()
}

func (s *healthState) recordFailure(msg string) {
	s.failedTotal.Add(1)
	s.mu.Lock()
	s.lastErrorAt = time.Now().UTC()
	s.lastErrorMsg = msg
	s.mu.Unlock()
}

// healthSnapshot is the JSON shape returned by /_marc/health.
// Pointer-string fields render as JSON null when nil.
type healthSnapshot struct {
	Status                  string  `json:"status"`
	LastSuccessfulForwardAt *string `json:"last_successful_forward_at"`
	LastErrorAt             *string `json:"last_error_at"`
	LastErrorMessage        *string `json:"last_error_message"`
	RequestsForwardedTotal  uint64  `json:"requests_forwarded_total"`
	RequestsFailedTotal     uint64  `json:"requests_failed_total"`
	UpstreamURL             string  `json:"upstream_url"`
	ListenAddr              string  `json:"listen_addr"`
	Version                 string  `json:"version"`
}

func (s *healthState) snapshot(cfg Config) healthSnapshot {
	forwarded := s.forwardedTotal.Load()
	failed := s.failedTotal.Load()

	s.mu.RLock()
	lastSuccess := s.lastSuccessAt
	lastError := s.lastErrorAt
	lastErrMsg := s.lastErrorMsg
	s.mu.RUnlock()

	snap := healthSnapshot{
		Status:                 computeStatus(forwarded, failed, lastSuccess, lastError),
		RequestsForwardedTotal: forwarded,
		RequestsFailedTotal:    failed,
		UpstreamURL:            cfg.UpstreamURL,
		ListenAddr:             cfg.ListenAddr,
		Version:                cfg.Version,
	}
	if !lastSuccess.IsZero() {
		v := lastSuccess.Format(time.RFC3339Nano)
		snap.LastSuccessfulForwardAt = &v
	}
	if !lastError.IsZero() {
		v := lastError.Format(time.RFC3339Nano)
		snap.LastErrorAt = &v
		m := lastErrMsg
		snap.LastErrorMessage = &m
	}
	return snap
}

// computeStatus returns "ok" | "degraded" | "failed".
//
//   - no traffic at all: "ok" (nothing has gone wrong yet).
//   - every request has failed: "failed".
//   - the most recent event is an error and there has been at least one
//     success before it: "degraded".
//   - otherwise: "ok".
func computeStatus(forwarded, failed uint64, lastSuccess, lastError time.Time) string {
	if forwarded == 0 && failed == 0 {
		return "ok"
	}
	if forwarded == 0 {
		return "failed"
	}
	if !lastError.IsZero() && lastError.After(lastSuccess) {
		return "degraded"
	}
	return "ok"
}

// serveHealth handles GET /_marc/health. It does not forward upstream and
// does not touch the capture file — purely observability.
func (h *handler) serveHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snap := h.health.snapshot(h.cfg)
	b, err := json.Marshal(snap)
	if err != nil {
		slog.Error("health: marshal failed", slog.Any("error", err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}
