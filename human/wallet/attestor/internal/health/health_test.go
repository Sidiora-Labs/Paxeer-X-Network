package health

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func reporter(t *testing.T, peers map[string]PeerState, readiness error) *Reporter {
	t.Helper()
	head := sha256.Sum256([]byte("audit-head"))
	r, err := NewReporter("attestor-3", "region-c", Providers{
		Shares:    func() (uint64, uint64, error) { return 42, 7, nil },
		AuditHead: func() (uint64, [32]byte) { return 1024, head },
		Peers:     func(context.Context) map[string]PeerState { return peers },
		Readiness: func() error { return readiness },
	})
	if err != nil {
		t.Fatalf("NewReporter: %v", err)
	}
	return r
}

func fourPeers(unreachable ...string) map[string]PeerState {
	peers := map[string]PeerState{
		"attestor-1": {Reachable: true, RTT: 12 * time.Millisecond},
		"attestor-2": {Reachable: true, RTT: 48 * time.Millisecond},
		"attestor-4": {Reachable: true, RTT: 95 * time.Millisecond},
		"attestor-5": {Reachable: true, RTT: 140 * time.Millisecond},
	}
	for _, id := range unreachable {
		peers[id] = PeerState{}
	}
	return peers
}

func TestReportOneUnreachablePeerStillReady(t *testing.T) {
	rep := reporter(t, fourPeers("attestor-2"), nil).Report(context.Background())
	head := sha256.Sum256([]byte("audit-head"))
	if rep.NodeID != "attestor-3" || rep.Region != "region-c" {
		t.Fatalf("identity = %q %q", rep.NodeID, rep.Region)
	}
	if rep.ShareCount != 42 || rep.RefreshEpoch != 7 {
		t.Fatalf("shares = %d epoch %d", rep.ShareCount, rep.RefreshEpoch)
	}
	if rep.AuditSequence != 1024 || rep.AuditHead != hex.EncodeToString(head[:]) {
		t.Fatalf("audit head = %d %s", rep.AuditSequence, rep.AuditHead)
	}
	if len(rep.Peers) != 4 {
		t.Fatalf("peers = %d, want 4", len(rep.Peers))
	}
	if rep.Peers["attestor-2"].Reachable {
		t.Fatalf("attestor-2 reported reachable")
	}
	if !rep.Peers["attestor-5"].Reachable || rep.Peers["attestor-5"].RTT != 140*time.Millisecond {
		t.Fatalf("attestor-5 state = %+v", rep.Peers["attestor-5"])
	}
	if rep.ReachablePeers != 3 {
		t.Fatalf("reachable peers = %d, want 3", rep.ReachablePeers)
	}
	if !rep.Ready {
		t.Fatalf("report not ready with three reachable peers")
	}
}

func TestReportThreeUnreachablePeersNotReady(t *testing.T) {
	rep := reporter(t, fourPeers("attestor-1", "attestor-2", "attestor-4"), nil).Report(context.Background())
	if rep.ReachablePeers != 1 {
		t.Fatalf("reachable peers = %d, want 1", rep.ReachablePeers)
	}
	if rep.Ready {
		t.Fatalf("report ready with one reachable peer")
	}
}

func TestReportReadinessErrorNotReady(t *testing.T) {
	rep := reporter(t, fourPeers(), errors.New("share store locked")).Report(context.Background())
	if rep.ReachablePeers != 4 {
		t.Fatalf("reachable peers = %d, want 4", rep.ReachablePeers)
	}
	if rep.Ready {
		t.Fatalf("report ready despite readiness error")
	}
	if rep.ReadinessError != "share store locked" {
		t.Fatalf("readiness error = %q", rep.ReadinessError)
	}
}

func TestReportShareErrorNotReady(t *testing.T) {
	r, err := NewReporter("attestor-3", "region-c", Providers{
		Shares:    func() (uint64, uint64, error) { return 0, 0, errors.New("share store unreadable") },
		AuditHead: func() (uint64, [32]byte) { return 0, [32]byte{} },
		Peers:     func(context.Context) map[string]PeerState { return fourPeers() },
		Readiness: func() error { return nil },
	})
	if err != nil {
		t.Fatalf("NewReporter: %v", err)
	}
	rep := r.Report(context.Background())
	if rep.Ready || rep.ShareError != "share store unreadable" {
		t.Fatalf("ready %v share error %q", rep.Ready, rep.ShareError)
	}
}

func TestNewReporterRequiresEveryProvider(t *testing.T) {
	if _, err := NewReporter("attestor-3", "region-c", Providers{}); !errors.Is(err, ErrMissingProvider) {
		t.Fatalf("NewReporter with no providers = %v, want ErrMissingProvider", err)
	}
}

func TestMetricsHandlerServesRegisteredSeries(t *testing.T) {
	registry := prometheus.NewRegistry()
	m, err := NewMetrics(registry)
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	m.SessionStarted("cggmp_sign")
	m.SessionCompleted("cggmp_sign")
	m.Refused("policy_denied")
	m.ObserveSigning("cggmp_sign", 180*time.Millisecond)
	m.ObservePeerRTT("attestor-1", 12*time.Millisecond)
	server := httptest.NewServer(m.Handler())
	defer server.Close()
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("GET metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	for _, series := range []string{
		`attestor_sessions_started_total{protocol="cggmp_sign"} 1`,
		`attestor_sessions_completed_total{protocol="cggmp_sign"} 1`,
		`attestor_refusals_total{reason="policy_denied"} 1`,
		`attestor_signing_latency_seconds_count{protocol="cggmp_sign"} 1`,
		`attestor_peer_rtt_seconds_count{peer="attestor-1"} 1`,
	} {
		if !strings.Contains(text, series) {
			t.Fatalf("metrics output missing %q:\n%s", series, text)
		}
	}
	if _, err := NewMetrics(registry); err == nil {
		t.Fatalf("second NewMetrics on the same registry succeeded")
	}
}
