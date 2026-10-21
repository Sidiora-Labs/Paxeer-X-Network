package telemetry

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

var services = map[string]bool{"apid_rest": true, "apid_sockets": true, "indexerd": true, "chartsd": true, "statsd": true, "pnld": true, "rankingd": true, "mediagatewayd": true, "userd": true, "sociald": true, "metadatad": true, "livestreamd": true}
var routes = map[string]bool{"browse": true, "quote": true, "wallet_challenge": true, "metadata": true, "websocket_replay": true, "account_mutation": true, "other": true}
var metricNames = []string{"sli_read_eligible_requests_total", "sli_read_good_requests_total", "sli_read_fast150_requests_total", "sli_read_fast400_requests_total", "sli_mutation_eligible_requests_total", "sli_mutation_good_requests_total", "durable_transaction_commits_total", "durable_transaction_rollbacks_total"}

type Metrics struct {
	mu                         sync.Mutex
	service, environment, zone string
	counts                     map[string]map[string]uint64
}

func New(service, environment, zone string) (*Metrics, error) {
	if !services[service] || (environment != "qualification" && environment != "production") || (zone != "zone-a" && zone != "zone-b" && zone != "zone-c") {
		return nil, errors.New("telemetry_identity_outside_inventory")
	}
	m := &Metrics{service: service, environment: environment, zone: zone, counts: make(map[string]map[string]uint64)}
	for _, name := range metricNames {
		m.counts[name] = make(map[string]uint64)
		for route := range routes {
			m.counts[name][route] = 0
		}
	}
	return m, nil
}

func (m *Metrics) RecordRead(route string, status int, duration time.Duration, exclusion string) error {
	if m == nil || !routes[route] || status < 100 || status > 599 || duration < 0 {
		return errors.New("telemetry_invalid_read")
	}
	switch exclusion {
	case "synthetic", "health", "client_cancel", "invalid_request":
		return nil
	case "":
	default:
		return errors.New("telemetry_invalid_exclusion")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counts["sli_read_eligible_requests_total"][route]++
	if status < 500 {
		m.counts["sli_read_good_requests_total"][route]++
	}
	if duration <= 150*time.Millisecond {
		m.counts["sli_read_fast150_requests_total"][route]++
	}
	if duration <= 400*time.Millisecond {
		m.counts["sli_read_fast400_requests_total"][route]++
	}
	return nil
}

func (m *Metrics) RecordMutation(route string, validated, authoritativeCommit, outboxCommit, acknowledged bool) error {
	if m == nil || !routes[route] {
		return errors.New("telemetry_invalid_mutation")
	}
	if !validated {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counts["sli_mutation_eligible_requests_total"][route]++
	if authoritativeCommit && outboxCommit && acknowledged {
		m.counts["sli_mutation_good_requests_total"][route]++
	}
	return nil
}

func (m *Metrics) RecordCommit(committed bool) error {
	if m == nil {
		return errors.New("telemetry_not_initialized")
	}
	name := "durable_transaction_rollbacks_total"
	if committed {
		name = "durable_transaction_commits_total"
	}
	m.mu.Lock()
	m.counts[name]["other"]++
	m.mu.Unlock()
	return nil
}

func (m *Metrics) WritePrometheus(w io.Writer) error {
	if m == nil {
		return errors.New("telemetry_not_initialized")
	}
	m.mu.Lock()
	copy := make(map[string]map[string]uint64, len(m.counts))
	for name, values := range m.counts {
		copy[name] = make(map[string]uint64, len(values))
		for route, count := range values {
			copy[name][route] = count
		}
	}
	m.mu.Unlock()
	ordered := make([]string, 0, len(routes))
	for route := range routes {
		ordered = append(ordered, route)
	}
	sort.Strings(ordered)
	for _, name := range metricNames {
		if _, err := fmt.Fprintf(w, "# TYPE %s counter\n", name); err != nil {
			return err
		}
		for _, route := range ordered {
			if _, err := fmt.Fprintf(w, "%s{service=%q,environment=%q,zone=%q,route=%q} %d\n", name, m.service, m.environment, m.zone, route, copy[name][route]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Metrics) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	if m == nil {
		http.Error(w, "telemetry unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = m.WritePrometheus(w)
}
