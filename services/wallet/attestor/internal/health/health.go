package health

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const MinReachablePeers = 2

var ErrMissingProvider = errors.New("health: every provider must be set")

type PeerState struct {
	Reachable bool          `json:"reachable"`
	RTT       time.Duration `json:"rtt_ns"`
}

type ReplicaState struct {
	LastShipped time.Time
	ErrorClass  string
}

type ReplicaReport struct {
	LastShippedAgeSeconds *int64 `json:"last_shipped_age_seconds"`
	ErrorClass            string `json:"error_class,omitempty"`
}

type SnapshotState struct {
	LastWritten time.Time
	LastError   string
	Failures    uint64
}

type SnapshotReport struct {
	LastWrittenAgeSeconds *int64 `json:"last_written_age_seconds"`
	LastError             string `json:"last_error,omitempty"`
	Failures              uint64 `json:"failures"`
}

func SnapshotAge(last, now time.Time) *int64 {
	if last.IsZero() {
		return nil
	}
	age := int64(now.Sub(last) / time.Second)
	return &age
}

type Providers struct {
	Shares    func() (count uint64, epoch uint64, err error)
	AuditHead func() (sequence uint64, hash [32]byte)
	Peers     func(ctx context.Context) map[string]PeerState
	Readiness func() error
	Replica   func() ReplicaState
	Snapshot  func() SnapshotState
	Clock     func() time.Time
}

type Report struct {
	NodeID         string               `json:"node_id"`
	Region         string               `json:"region"`
	ShareCount     uint64               `json:"share_count"`
	RefreshEpoch   uint64               `json:"refresh_epoch"`
	ShareError     string               `json:"share_error,omitempty"`
	AuditSequence  uint64               `json:"audit_sequence"`
	AuditHead      string               `json:"audit_head"`
	Peers          map[string]PeerState `json:"peers"`
	ReachablePeers int                  `json:"reachable_peers"`
	ReadinessError string               `json:"readiness_error,omitempty"`
	Replica        *ReplicaReport       `json:"replica,omitempty"`
	Snapshot       *SnapshotReport      `json:"snapshot,omitempty"`
	Ready          bool                 `json:"ready"`
}

type Reporter struct {
	nodeID    string
	region    string
	providers Providers
}

func NewReporter(nodeID, region string, providers Providers) (*Reporter, error) {
	if providers.Shares == nil || providers.AuditHead == nil || providers.Peers == nil || providers.Readiness == nil {
		return nil, ErrMissingProvider
	}
	if providers.Clock == nil {
		providers.Clock = time.Now
	}
	return &Reporter{nodeID: nodeID, region: region, providers: providers}, nil
}

func (r *Reporter) Report(ctx context.Context) Report {
	out := Report{NodeID: r.nodeID, Region: r.region, Peers: map[string]PeerState{}}
	count, epoch, shareErr := r.providers.Shares()
	if shareErr != nil {
		out.ShareError = shareErr.Error()
	} else {
		out.ShareCount = count
		out.RefreshEpoch = epoch
	}
	sequence, head := r.providers.AuditHead()
	out.AuditSequence = sequence
	out.AuditHead = hex.EncodeToString(head[:])
	for id, state := range r.providers.Peers(ctx) {
		out.Peers[id] = state
		if state.Reachable {
			out.ReachablePeers++
		}
	}
	if r.providers.Replica != nil {
		state := r.providers.Replica()
		replica := &ReplicaReport{ErrorClass: state.ErrorClass}
		if !state.LastShipped.IsZero() {
			age := int64(r.providers.Clock().Sub(state.LastShipped) / time.Second)
			replica.LastShippedAgeSeconds = &age
		}
		out.Replica = replica
	}
	if r.providers.Snapshot != nil {
		state := r.providers.Snapshot()
		out.Snapshot = &SnapshotReport{
			LastWrittenAgeSeconds: SnapshotAge(state.LastWritten, r.providers.Clock()),
			LastError:             state.LastError,
			Failures:              state.Failures,
		}
	}
	readyErr := r.providers.Readiness()
	if readyErr != nil {
		out.ReadinessError = readyErr.Error()
	}
	out.Ready = readyErr == nil && shareErr == nil && out.ReachablePeers >= MinReachablePeers
	return out
}

type Metrics struct {
	registry          *prometheus.Registry
	sessionsStarted   *prometheus.CounterVec
	sessionsCompleted *prometheus.CounterVec
	refusals          *prometheus.CounterVec
	signingLatency    *prometheus.HistogramVec
	peerRTT           *prometheus.HistogramVec
}

func NewMetrics(registry *prometheus.Registry) (*Metrics, error) {
	m := &Metrics{
		registry: registry,
		sessionsStarted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "attestor",
			Name:      "sessions_started_total",
			Help:      "Threshold protocol sessions started, by protocol.",
		}, []string{"protocol"}),
		sessionsCompleted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "attestor",
			Name:      "sessions_completed_total",
			Help:      "Threshold protocol sessions completed, by protocol.",
		}, []string{"protocol"}),
		refusals: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "attestor",
			Name:      "refusals_total",
			Help:      "Requests refused, by reason code.",
		}, []string{"reason"}),
		signingLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "attestor",
			Name:      "signing_latency_seconds",
			Help:      "Signing session latency from request to signature, by protocol.",
			Buckets:   prometheus.ExponentialBuckets(0.005, 2, 12),
		}, []string{"protocol"}),
		peerRTT: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "attestor",
			Name:      "peer_rtt_seconds",
			Help:      "Round-trip time to each peer.",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 12),
		}, []string{"peer"}),
	}
	for _, c := range []prometheus.Collector{m.sessionsStarted, m.sessionsCompleted, m.refusals, m.signingLatency, m.peerRTT} {
		if err := registry.Register(c); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (m *Metrics) SessionStarted(protocol string) {
	m.sessionsStarted.WithLabelValues(protocol).Inc()
}

func (m *Metrics) SessionCompleted(protocol string) {
	m.sessionsCompleted.WithLabelValues(protocol).Inc()
}

func (m *Metrics) Refused(reason string) {
	m.refusals.WithLabelValues(reason).Inc()
}

func (m *Metrics) ObserveSigning(protocol string, d time.Duration) {
	m.signingLatency.WithLabelValues(protocol).Observe(d.Seconds())
}

func (m *Metrics) ObservePeerRTT(peer string, d time.Duration) {
	m.peerRTT.WithLabelValues(peer).Observe(d.Seconds())
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{Registry: m.registry})
}
