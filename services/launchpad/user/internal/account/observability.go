package account

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Observation struct {
	Count      uint64 `json:"count"`
	TotalNanos uint64 `json:"totalNanos"`
	MaxNanos   uint64 `json:"maxNanos"`
}
type Metrics struct {
	mu     sync.Mutex
	values map[string]Observation
}

func (m *Metrics) Observe(operation, outcome string, duration time.Duration) {
	if m == nil {
		return
	}
	switch operation {
	case "profile", "preferences", "watchlist", "consent", "endpoint", "export", "deletion", "backfill", "projection", "inbox":
	default:
		return
	}
	switch outcome {
	case "success", "conflict", "invalid", "denied", "failed", "stale", "expired", "quarantined", "overdue":
	default:
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.values == nil {
		m.values = make(map[string]Observation)
	}
	key := operation + "." + outcome
	v := m.values[key]
	v.Count++
	if duration > 0 {
		n := uint64(duration)
		v.TotalNanos += n
		if n > v.MaxNanos {
			v.MaxNanos = n
		}
	}
	m.values[key] = v
}
func (m *Metrics) Snapshot() map[string]Observation {
	out := map[string]Observation{}
	if m == nil {
		return out
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range m.values {
		out[k] = v
	}
	return out
}

type measuredWriter struct {
	http.ResponseWriter
	status int
}

func (w *measuredWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *measuredWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}
func operationName(path string) string {
	switch {
	case strings.Contains(path, "/profiles/"):
		return "profile"
	case strings.Contains(path, "/preferences"):
		return "preferences"
	case strings.Contains(path, "/watchlist"):
		return "watchlist"
	case strings.Contains(path, "/devices/"):
		return "endpoint"
	case strings.Contains(path, "/exports"):
		return "export"
	case strings.Contains(path, "/deletion-requests"):
		return "deletion"
	}
	return ""
}
func (m *Metrics) ObserveHTTP(path string, status int, duration time.Duration) {
	outcome := "success"
	switch {
	case status == http.StatusPreconditionFailed || status == http.StatusConflict:
		outcome = "conflict"
	case status == http.StatusUnprocessableEntity || status == http.StatusBadRequest:
		outcome = "invalid"
	case status == http.StatusForbidden || status == http.StatusUnauthorized:
		outcome = "denied"
	case status >= 400:
		outcome = "failed"
	}
	m.Observe(operationName(path), outcome, duration)
}

type ServiceStats struct {
	PendingExports        int64 `json:"pendingExports"`
	PendingDeletions      int64 `json:"pendingDeletions"`
	OldestExportSeconds   int64 `json:"oldestExportSeconds"`
	OldestDeletionSeconds int64 `json:"oldestDeletionSeconds"`
	OverdueDeletions      int64 `json:"overdueDeletions"`
	ExpiredEndpoints      int64 `json:"expiredEndpoints"`
	QuarantinedBackfills  int64 `json:"quarantinedBackfills"`
	WatchlistMax          int64 `json:"watchlistMax"`
}

func (s Store) Stats(ctx context.Context) (ServiceStats, error) {
	var v ServiceStats
	e := s.DB.QueryRow(ctx, `SELECT count(*) FILTER(WHERE kind='export'),count(*) FILTER(WHERE kind='deletion'),coalesce(extract(epoch FROM $1-min(requested_at) FILTER(WHERE kind='export'))::bigint,0),coalesce(extract(epoch FROM $1-min(requested_at) FILTER(WHERE kind='deletion'))::bigint,0),count(*) FILTER(WHERE kind='deletion' AND requested_at<$1-interval '30 days') FROM account_job WHERE state NOT IN('completed','expired','ready')`, s.now()).Scan(&v.PendingExports, &v.PendingDeletions, &v.OldestExportSeconds, &v.OldestDeletionSeconds, &v.OverdueDeletions)
	if e != nil {
		return v, e
	}
	e = s.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM account_endpoint WHERE validated_at<$1-interval '30 days'),(SELECT count(*) FROM account_backfill WHERE state='quarantined'),coalesce((SELECT max(n) FROM(SELECT count(*) n FROM account_watch GROUP BY account_id) sizes),0)`, s.now()).Scan(&v.ExpiredEndpoints, &v.QuarantinedBackfills, &v.WatchlistMax)
	return v, e
}
