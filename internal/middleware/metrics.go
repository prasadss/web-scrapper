package middleware

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"
)

// Metrics tracks application-level counters.
type Metrics struct {
	TotalRequests      atomic.Int64
	SuccessfulAnalyses atomic.Int64
	FailedAnalyses     atomic.Int64
	CacheHits          atomic.Int64
	startTime          time.Time
}

// NewMetrics creates a new Metrics instance.
func NewMetrics() *Metrics {
	return &Metrics{
		startTime: time.Now(),
	}
}

// RecordRequest increments the total request counter.
func (m *Metrics) RecordRequest() {
	m.TotalRequests.Add(1)
}

// RecordSuccess increments the successful analysis counter.
func (m *Metrics) RecordSuccess() {
	m.SuccessfulAnalyses.Add(1)
}

// RecordFailure increments the failed analysis counter.
func (m *Metrics) RecordFailure() {
	m.FailedAnalyses.Add(1)
}

// RecordCacheHit increments the cache-hit counter.
func (m *Metrics) RecordCacheHit() {
	m.CacheHits.Add(1)
}

// metricsResponse is the JSON structure for the /metrics endpoint.
type metricsResponse struct {
	Uptime             string `json:"uptime"`
	TotalRequests      int64  `json:"total_requests"`
	SuccessfulAnalyses int64  `json:"successful_analyses"`
	FailedAnalyses     int64  `json:"failed_analyses"`
	CacheHits          int64  `json:"cache_hits"`
}

// Handler returns an HTTP handler that exposes metrics as JSON.
func (m *Metrics) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := metricsResponse{
			Uptime:             time.Since(m.startTime).Round(time.Second).String(),
			TotalRequests:      m.TotalRequests.Load(),
			SuccessfulAnalyses: m.SuccessfulAnalyses.Load(),
			FailedAnalyses:     m.FailedAnalyses.Load(),
			CacheHits:          m.CacheHits.Load(),
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, "encode metrics", http.StatusInternalServerError)
		}
	}
}
