// Package telemetry serves the Prometheus metrics of one perps data service.
// Origin: Launch-x shared/telemetry, registered on the prometheus client
// instead of the hand-written exposition.
package telemetry

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/config"
)

const maxRouteLen = 128

var textFormat = expfmt.NewFormat(expfmt.TypeTextPlain)

// Metrics holds the registry of one service and the request counter every
// service exports.
type Metrics struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
}

// New returns the Metrics of the named service, or an error when the service
// is outside the inventory.
func New(service config.Service) (*Metrics, error) {
	switch service {
	case config.Indexer, config.Gateway, config.Explorer, config.Portal, config.Replay, config.Loadtest:
	default:
		return nil, errors.New("telemetry_identity_outside_inventory")
	}
	requests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "perps_http_requests_total",
		Help: "HTTP requests served, by route and status.",
	}, []string{"route", "status"})
	registry := prometheus.NewRegistry()
	if err := registry.Register(requests); err != nil {
		return nil, err
	}
	return &Metrics{registry: registry, requests: requests}, nil
}

// Register adds a service-specific collector to the registry.
func (m *Metrics) Register(c prometheus.Collector) error {
	if m == nil {
		return errors.New("telemetry_not_initialized")
	}
	return m.registry.Register(c)
}

// RecordRead counts one served request under its route pattern and status.
func (m *Metrics) RecordRead(route string, status int) error {
	if m == nil || route == "" || len(route) > maxRouteLen || !utf8.ValidString(route) || status < 100 || status > 599 {
		return errors.New("telemetry_invalid_read")
	}
	m.requests.WithLabelValues(route, strconv.Itoa(status)).Inc()
	return nil
}

// WritePrometheus writes the text exposition of every registered collector.
func (m *Metrics) WritePrometheus(w io.Writer) error {
	if m == nil {
		return errors.New("telemetry_not_initialized")
	}
	families, err := m.registry.Gather()
	if err != nil {
		return err
	}
	enc := expfmt.NewEncoder(w, textFormat)
	for _, family := range families {
		if err := enc.Encode(family); err != nil {
			return err
		}
	}
	return nil
}

// ServeHTTP answers GET with the text exposition.
func (m *Metrics) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if m == nil {
		http.Error(w, "telemetry unavailable", http.StatusServiceUnavailable)
		return
	}
	var body bytes.Buffer
	if err := m.WritePrometheus(&body); err != nil {
		http.Error(w, "telemetry unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", string(textFormat))
	_, _ = w.Write(body.Bytes())
}
