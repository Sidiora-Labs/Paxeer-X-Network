package tasks

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type mockHTTPDoer struct {
	responses map[string]*http.Response
}

func (m *mockHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	resp, ok := m.responses[req.URL.String()]
	if !ok {
		return nil, fmt.Errorf("unexpected request: %s", req.URL.String())
	}
	// Read and restore the body so repeated requests to the same URL (the
	// witness reachability probe, then the trust-point query) both succeed.
	body, _ := io.ReadAll(resp.Body)
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return &http.Response{StatusCode: resp.StatusCode, Body: io.NopCloser(bytes.NewReader(body))}, nil
}

func generateBlockHash() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%X", b)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// wrapResult wraps inner JSON in the CometBFT JSON-RPC envelope.
func wrapResult(inner string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":-1,"result":%s}`, inner)
}

func setupPeersInConfig(t *testing.T, homeDir string, peers []string) {
	t.Helper()
	configDir := filepath.Join(homeDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}
	content := fmt.Sprintf("[p2p]\npersistent-peers = %q\n", strings.Join(peers, ","))
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing config.toml: %v", err)
	}
}

func TestStateSyncConfigurer_Success(t *testing.T) {
	homeDir := t.TempDir()
	setupPeersInConfig(t, homeDir, []string{"nodeId1@1.2.3.4:26656", "nodeId2@5.6.7.8:26656"})

	hash := generateBlockHash()
	mock := &mockHTTPDoer{
		responses: map[string]*http.Response{
			"http://1.2.3.4:26657/status": jsonResponse(wrapResult(`{
				"sync_info": {"latest_block_height": "10000"}
			}`)),
			"http://5.6.7.8:26657/status": jsonResponse(wrapResult(`{
				"sync_info": {"latest_block_height": "10000"}
			}`)),
			"http://1.2.3.4:26657/block?height=8000": jsonResponse(wrapResult(fmt.Sprintf(`{
				"block_id": {"hash": %q}
			}`, hash))),
		},
	}

	configurer := NewStateSyncConfigurer(homeDir, mock)
	if err := configurer.Configure(context.Background(), StateSyncRequest{}); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	if !markerExists(homeDir, stateSyncMarkerFile) {
		t.Fatal("marker file should exist after successful configure")
	}

	configDoc := readTOML(t, filepath.Join(homeDir, "config", "config.toml"))
	ss := configDoc["statesync"].(map[string]any)
	if ss["enable"] != true {
		t.Error("expected statesync.enable = true in config.toml")
	}
	if ss["trust-height"] != int64(8000) {
		t.Errorf("expected statesync.trust-height = 8000, got %v", ss["trust-height"])
	}
	if ss["trust-hash"] != hash {
		t.Errorf("expected trust-hash = %s, got %v", hash, ss["trust-hash"])
	}
	if ss["rpc-servers"] != "1.2.3.4:26657,5.6.7.8:26657" {
		t.Errorf("expected rpc-servers, got %v", ss["rpc-servers"])
	}
	if ss["use-local-snapshot"] != false {
		t.Errorf("expected use-local-snapshot = false, got %v", ss["use-local-snapshot"])
	}
}

func TestStateSyncConfigurer_MarkerSkips(t *testing.T) {
	homeDir := t.TempDir()

	if err := writeMarker(homeDir, stateSyncMarkerFile); err != nil {
		t.Fatalf("writing marker: %v", err)
	}

	configurer := NewStateSyncConfigurer(homeDir, &mockHTTPDoer{})
	if err := configurer.Configure(context.Background(), StateSyncRequest{}); err != nil {
		t.Fatalf("expected nil error when marker exists, got: %v", err)
	}
}

func TestStateSyncConfigurer_NoPeers(t *testing.T) {
	homeDir := t.TempDir()
	setupPeersInConfig(t, homeDir, nil)

	configurer := NewStateSyncConfigurer(homeDir, &mockHTTPDoer{})
	err := configurer.Configure(context.Background(), StateSyncRequest{})
	if err == nil {
		t.Fatal("expected error when no peers in config")
	}
}

func TestStateSyncConfigurer_LowHeight(t *testing.T) {
	homeDir := t.TempDir()
	setupPeersInConfig(t, homeDir, []string{"nodeId1@10.0.0.1:26656"})

	hash := generateBlockHash()
	mock := &mockHTTPDoer{
		responses: map[string]*http.Response{
			"http://10.0.0.1:26657/status": jsonResponse(wrapResult(`{
				"sync_info": {"latest_block_height": "500"}
			}`)),
			"http://10.0.0.1:26657/block?height=1": jsonResponse(wrapResult(fmt.Sprintf(`{
				"block_id": {"hash": %q}
			}`, hash))),
		},
	}

	configurer := NewStateSyncConfigurer(homeDir, mock)
	if err := configurer.Configure(context.Background(), StateSyncRequest{}); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	configDoc := readTOML(t, filepath.Join(homeDir, "config", "config.toml"))
	ss := configDoc["statesync"].(map[string]any)
	if ss["trust-height"] != int64(1) {
		t.Errorf("expected trustHeight clamped to 1, got %v", ss["trust-height"])
	}
	if ss["trust-hash"] != hash {
		t.Errorf("expected trustHash %s, got %v", hash, ss["trust-hash"])
	}
	// Single peer should be duplicated to satisfy Tendermint requirement.
	if ss["rpc-servers"] != "10.0.0.1:26657,10.0.0.1:26657" {
		t.Errorf("expected duplicated rpc-servers, got %v", ss["rpc-servers"])
	}
}

func TestStateSyncConfigurer_InvalidBlockHash(t *testing.T) {
	tests := []struct {
		name    string
		hash    string
		wantErr string
	}{
		{"empty hash", "", "empty block hash"},
		{"short hash", "ABCDEF", "unexpected block hash length"},
		{"long hash", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA00", "unexpected block hash length"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := t.TempDir()
			setupPeersInConfig(t, homeDir, []string{"nodeId1@1.2.3.4:26656"})

			mock := &mockHTTPDoer{
				responses: map[string]*http.Response{
					"http://1.2.3.4:26657/status": jsonResponse(wrapResult(`{
						"sync_info": {"latest_block_height": "10000"}
					}`)),
					"http://1.2.3.4:26657/block?height=8000": jsonResponse(wrapResult(fmt.Sprintf(`{
						"block_id": {"hash": %q}
					}`, tt.hash))),
				},
			}

			configurer := NewStateSyncConfigurer(homeDir, mock)
			err := configurer.Configure(context.Background(), StateSyncRequest{})
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestExtractRPCHosts(t *testing.T) {
	tests := []struct {
		name     string
		peers    []string
		maxHosts int
		want     []string
	}{
		{
			name:     "standard peers",
			peers:    []string{"nodeId1@1.2.3.4:26656", "nodeId2@5.6.7.8:26656"},
			maxHosts: 2,
			want:     []string{"1.2.3.4", "5.6.7.8"},
		},
		{
			name:     "max hosts limits output",
			peers:    []string{"a@1.1.1.1:26656", "b@2.2.2.2:26656", "c@3.3.3.3:26656"},
			maxHosts: 2,
			want:     []string{"1.1.1.1", "2.2.2.2"},
		},
		{
			name:     "invalid format skipped",
			peers:    []string{"no-at-sign", "valid@10.0.0.1:26656"},
			maxHosts: 2,
			want:     []string{"10.0.0.1"},
		},
		{
			name:     "empty peers",
			peers:    []string{},
			maxHosts: 2,
			want:     nil,
		},
		{
			name:     "host without port",
			peers:    []string{"nodeId@myhost"},
			maxHosts: 1,
			want:     []string{"myhost"},
		},
		{
			name:     "IPv6-style host with port",
			peers:    []string{"nodeId@[::1]:26656"},
			maxHosts: 1,
			want:     []string{"[::1]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRPCHosts(tt.peers, tt.maxHosts)
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d hosts, got %d: %v", len(tt.want), len(got), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("host[%d]: expected %q, got %q", i, tt.want[i], got[i])
				}
			}
		})
	}
}

func TestStateSyncConfigurer_Handler(t *testing.T) {
	homeDir := t.TempDir()
	setupPeersInConfig(t, homeDir, []string{"nodeId1@1.2.3.4:26656"})

	hash := generateBlockHash()
	mock := &mockHTTPDoer{
		responses: map[string]*http.Response{
			"http://1.2.3.4:26657/status": jsonResponse(wrapResult(`{
				"sync_info": {"latest_block_height": "5000"}
			}`)),
			"http://1.2.3.4:26657/block?height=3000": jsonResponse(wrapResult(fmt.Sprintf(`{
				"block_id": {"hash": %q}
			}`, hash))),
		},
	}

	configurer := NewStateSyncConfigurer(homeDir, mock)
	handler := configurer.Handler()

	if _, err := handler(context.Background(), nil); err != nil {
		t.Fatalf("Handler failed: %v", err)
	}

	configDoc := readTOML(t, filepath.Join(homeDir, "config", "config.toml"))
	ss := configDoc["statesync"].(map[string]any)
	if ss["trust-height"] != int64(3000) {
		t.Errorf("expected trustHeight 3000, got %v", ss["trust-height"])
	}
}

func TestStateSyncConfigurer_NetworkWithBackfill(t *testing.T) {
	homeDir := t.TempDir()
	setupPeersInConfig(t, homeDir, []string{"nodeId1@1.2.3.4:26656", "nodeId2@5.6.7.8:26656"})

	hash := generateBlockHash()
	mock := &mockHTTPDoer{
		responses: map[string]*http.Response{
			"http://1.2.3.4:26657/status": jsonResponse(wrapResult(`{
				"sync_info": {"latest_block_height": "10000"}
			}`)),
			"http://5.6.7.8:26657/status": jsonResponse(wrapResult(`{
				"sync_info": {"latest_block_height": "10000"}
			}`)),
			"http://1.2.3.4:26657/block?height=8000": jsonResponse(wrapResult(fmt.Sprintf(`{
				"block_id": {"hash": %q}
			}`, hash))),
		},
	}

	configurer := NewStateSyncConfigurer(homeDir, mock)
	err := configurer.Configure(context.Background(), StateSyncRequest{
		TrustPeriod:    "168h0m0s",
		BackfillBlocks: 6000,
	})
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	configDoc := readTOML(t, filepath.Join(homeDir, "config", "config.toml"))
	ss := configDoc["statesync"].(map[string]any)
	if ss["trust-period"] != "168h0m0s" {
		t.Errorf("expected trust-period = 168h0m0s, got %v", ss["trust-period"])
	}
	if ss["backfill-blocks"] != int64(6000) {
		t.Errorf("expected backfill-blocks = 6000, got %v", ss["backfill-blocks"])
	}
	if ss["use-local-snapshot"] != false {
		t.Errorf("expected use-local-snapshot = false, got %v", ss["use-local-snapshot"])
	}
}

func TestStateSyncConfigurer_LocalSnapshot(t *testing.T) {
	homeDir := t.TempDir()
	setupPeersInConfig(t, homeDir, []string{"nodeId1@1.2.3.4:26656", "nodeId2@5.6.7.8:26656"})

	snapshotHeight := int64(198030000)
	snapshotDir := filepath.Join(homeDir, "data", "snapshots", "198030000", "1")
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatalf("creating snapshot dir: %v", err)
	}

	hash := generateBlockHash()
	mock := &mockHTTPDoer{
		responses: map[string]*http.Response{
			"http://1.2.3.4:26657/status": jsonResponse(wrapResult(`{
				"sync_info": {"latest_block_height": "198030000"}
			}`)),
			"http://5.6.7.8:26657/status": jsonResponse(wrapResult(`{
				"sync_info": {"latest_block_height": "198030000"}
			}`)),
			"http://1.2.3.4:26657/block?height=198030000": jsonResponse(wrapResult(fmt.Sprintf(`{
				"block_id": {"hash": %q}
			}`, hash))),
		},
	}

	configurer := NewStateSyncConfigurer(homeDir, mock)
	err := configurer.Configure(context.Background(), StateSyncRequest{
		UseLocalSnapshot: true,
		TrustPeriod:      "9999h0m0s",
		BackfillBlocks:   0,
	})
	if err != nil {
		t.Fatalf("Configure with local snapshot failed: %v", err)
	}

	configDoc := readTOML(t, filepath.Join(homeDir, "config", "config.toml"))
	ss := configDoc["statesync"].(map[string]any)

	if ss["trust-height"] != snapshotHeight {
		t.Errorf("expected trust-height = %d (snapshot height), got %v", snapshotHeight, ss["trust-height"])
	}
	if ss["trust-hash"] != hash {
		t.Errorf("expected trust-hash = %s, got %v", hash, ss["trust-hash"])
	}
	if ss["use-local-snapshot"] != true {
		t.Errorf("expected use-local-snapshot = true, got %v", ss["use-local-snapshot"])
	}
	if ss["trust-period"] != "9999h0m0s" {
		t.Errorf("expected trust-period = 9999h0m0s, got %v", ss["trust-period"])
	}
	if ss["enable"] != true {
		t.Error("expected statesync.enable = true")
	}
}

func TestStateSyncConfigurer_LocalSnapshotNoDir(t *testing.T) {
	homeDir := t.TempDir()
	setupPeersInConfig(t, homeDir, []string{"nodeId1@1.2.3.4:26656"})

	mock := &mockHTTPDoer{
		responses: map[string]*http.Response{
			"http://1.2.3.4:26657/status": jsonResponse(wrapResult(`{
				"sync_info": {"latest_block_height": "100"}
			}`)),
		},
	}
	configurer := NewStateSyncConfigurer(homeDir, mock)
	err := configurer.Configure(context.Background(), StateSyncRequest{UseLocalSnapshot: true})
	if err == nil {
		t.Fatal("expected error when no snapshot directory exists")
	}
	if !strings.Contains(err.Error(), "discovering local snapshot height") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDiscoverLocalSnapshotHeight(t *testing.T) {
	homeDir := t.TempDir()

	snapshotBase := filepath.Join(homeDir, "data", "snapshots")
	for _, dir := range []string{"198030000/1", "198020000/1", "notaheight"} {
		if err := os.MkdirAll(filepath.Join(snapshotBase, dir), 0o755); err != nil {
			t.Fatalf("creating dir: %v", err)
		}
	}

	h, err := discoverLocalSnapshotHeight(homeDir)
	if err != nil {
		t.Fatalf("discoverLocalSnapshotHeight failed: %v", err)
	}
	if h != 198030000 {
		t.Errorf("expected height 198030000, got %d", h)
	}
}

func TestDiscoverLocalSnapshotHeight_Empty(t *testing.T) {
	homeDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(homeDir, "data", "snapshots"), 0o755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}

	_, err := discoverLocalSnapshotHeight(homeDir)
	if err == nil {
		t.Fatal("expected error for empty snapshots directory")
	}
}

func TestReadPeersFromConfig(t *testing.T) {
	homeDir := t.TempDir()
	setupPeersInConfig(t, homeDir, []string{"abc@1.2.3.4:26656", "def@5.6.7.8:26656"})

	peers, err := readPeersFromConfig(homeDir)
	if err != nil {
		t.Fatalf("readPeersFromConfig failed: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}
	if peers[0] != "abc@1.2.3.4:26656" {
		t.Errorf("peer[0] = %q, want abc@1.2.3.4:26656", peers[0])
	}
}

func TestReadPeersFromConfig_Empty(t *testing.T) {
	homeDir := t.TempDir()
	setupPeersInConfig(t, homeDir, nil)

	peers, err := readPeersFromConfig(homeDir)
	if err != nil {
		t.Fatalf("readPeersFromConfig failed: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers, got %d", len(peers))
	}
}

func TestReadPeersFromConfig_NoConfigFile(t *testing.T) {
	homeDir := t.TempDir()
	peers, err := readPeersFromConfig(homeDir)
	if err != nil {
		t.Fatalf("readPeersFromConfig failed: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers for missing config, got %d", len(peers))
	}
}

// Caller-provided RpcServers are used verbatim, independent of persistent-peers
// (here there are none).
func TestStateSyncConfigurer_ExplicitRpcServers(t *testing.T) {
	homeDir := t.TempDir()
	setupPeersInConfig(t, homeDir, nil)

	const witness = "syncer-0-0-0.syncer-0-0.arctic-1.svc.cluster.local:26657"
	hash := generateBlockHash()
	mock := &mockHTTPDoer{
		responses: map[string]*http.Response{
			"http://" + witness + "/status": jsonResponse(wrapResult(`{
				"sync_info": {"latest_block_height": "10000"}
			}`)),
			"http://" + witness + "/block?height=8000": jsonResponse(wrapResult(fmt.Sprintf(`{
				"block_id": {"hash": %q}
			}`, hash))),
		},
	}

	configurer := NewStateSyncConfigurer(homeDir, mock)
	if err := configurer.Configure(context.Background(), StateSyncRequest{
		RpcServers: []string{witness},
	}); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	ss := readTOML(t, filepath.Join(homeDir, "config", "config.toml"))["statesync"].(map[string]any)
	if ss["rpc-servers"] != witness+","+witness {
		t.Errorf("expected explicit witness padded to two, got %v", ss["rpc-servers"])
	}
	if ss["trust-height"] != int64(8000) {
		t.Errorf("expected trust-height 8000, got %v", ss["trust-height"])
	}
}

// The regression case: a peer-derived witness whose host serves P2P but not RPC
// (an external NLB hostname). The reachable witness is kept; the dead one is
// dropped instead of being written and crashlooping seid.
func TestStateSyncConfigurer_DropsUnreachableWitness(t *testing.T) {
	homeDir := t.TempDir()
	setupPeersInConfig(t, homeDir, []string{
		"nodeId1@1.2.3.4:26656",
		"nodeId2@syncer-0-0-p2p.arctic-1.prod.platform.sei.io:26656",
	})

	hash := generateBlockHash()
	mock := &mockHTTPDoer{
		responses: map[string]*http.Response{
			// Only 1.2.3.4 serves RPC; the p2p NLB host has no /status entry.
			"http://1.2.3.4:26657/status": jsonResponse(wrapResult(`{
				"sync_info": {"latest_block_height": "10000"}
			}`)),
			"http://1.2.3.4:26657/block?height=8000": jsonResponse(wrapResult(fmt.Sprintf(`{
				"block_id": {"hash": %q}
			}`, hash))),
		},
	}

	configurer := NewStateSyncConfigurer(homeDir, mock)
	if err := configurer.Configure(context.Background(), StateSyncRequest{}); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	ss := readTOML(t, filepath.Join(homeDir, "config", "config.toml"))["statesync"].(map[string]any)
	if ss["rpc-servers"] != "1.2.3.4:26657,1.2.3.4:26657" {
		t.Errorf("expected unreachable witness dropped, got %v", ss["rpc-servers"])
	}
}

// The production-regression ordering: the first candidate is unreachable (a
// P2P-only NLB host), so the trust query must fall through to the reachable
// second one — reachable[0] is the post-probe slice, not the raw peer list.
func TestStateSyncConfigurer_PrimaryUnreachableFallsThrough(t *testing.T) {
	homeDir := t.TempDir()
	setupPeersInConfig(t, homeDir, []string{
		"nodeId1@syncer-0-0-p2p.arctic-1.prod.platform.sei.io:26656", // P2P-only, no RPC
		"nodeId2@1.2.3.4:26656", // serves RPC
	})

	hash := generateBlockHash()
	mock := &mockHTTPDoer{
		responses: map[string]*http.Response{
			"http://1.2.3.4:26657/status": jsonResponse(wrapResult(`{
				"sync_info": {"latest_block_height": "10000"}
			}`)),
			"http://1.2.3.4:26657/block?height=8000": jsonResponse(wrapResult(fmt.Sprintf(`{
				"block_id": {"hash": %q}
			}`, hash))),
		},
	}

	configurer := NewStateSyncConfigurer(homeDir, mock)
	if err := configurer.Configure(context.Background(), StateSyncRequest{}); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	ss := readTOML(t, filepath.Join(homeDir, "config", "config.toml"))["statesync"].(map[string]any)
	if ss["trust-height"] != int64(8000) {
		t.Errorf("expected trust query to fall through to the reachable witness, trust-height = %v", ss["trust-height"])
	}
	if ss["trust-hash"] != hash {
		t.Errorf("expected trust-hash %s, got %v", hash, ss["trust-hash"])
	}
	if ss["rpc-servers"] != "1.2.3.4:26657,1.2.3.4:26657" {
		t.Errorf("expected only the reachable witness, got %v", ss["rpc-servers"])
	}
}

func TestWitnessScheme(t *testing.T) {
	cases := []struct {
		name, endpoint, want string
	}{
		{"tls gateway on 443", "archive-0-rpc.arctic-1.platform.sei.io:443", "https"},
		{"in-cluster rpc on 26657", "syncer-0-internal.arctic-1.svc.cluster.local:26657", "http"},
		{"bare ip rpc", "1.2.3.4:26657", "http"},
		{"no port defaults to http", "some-host", "http"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := witnessScheme(tc.endpoint); got != tc.want {
				t.Errorf("witnessScheme(%q) = %q, want %q", tc.endpoint, got, tc.want)
			}
		})
	}
}

// Regression guard for the state-sync witness scheme/port mismatch that blocked
// every new K8s state-syncing node: a canonical syncer resolved to the public
// Istio HTTPRoute hostname on :443 must be probed and queried over https, not
// the previously-hardcoded http (which EOFed against the TLS listener). The mock
// only answers the https URL — an http probe would fall through as an
// "unexpected request" and the configure would fail.
func TestStateSyncConfigurer_TLSWitnessUsesHTTPS(t *testing.T) {
	homeDir := t.TempDir()
	setupPeersInConfig(t, homeDir, nil)

	const witness = "archive-0-rpc.arctic-1.platform.sei.io:443"
	hash := generateBlockHash()
	mock := &mockHTTPDoer{
		responses: map[string]*http.Response{
			"https://" + witness + "/status": jsonResponse(wrapResult(`{
				"sync_info": {"latest_block_height": "10000"}
			}`)),
			"https://" + witness + "/block?height=8000": jsonResponse(wrapResult(fmt.Sprintf(`{
				"block_id": {"hash": %q}
			}`, hash))),
		},
	}

	configurer := NewStateSyncConfigurer(homeDir, mock)
	if err := configurer.Configure(context.Background(), StateSyncRequest{
		RpcServers: []string{witness},
	}); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	ss := readTOML(t, filepath.Join(homeDir, "config", "config.toml"))["statesync"].(map[string]any)
	// rpc-servers is written as the bare host:port (seid attaches the scheme).
	if ss["rpc-servers"] != witness+","+witness {
		t.Errorf("expected TLS witness padded to two, got %v", ss["rpc-servers"])
	}
	if ss["trust-height"] != int64(8000) {
		t.Errorf("expected trust-height 8000, got %v", ss["trust-height"])
	}
}

// The in-cluster plaintext path (a syncer's internal Service on :26657) must
// stay on http — the forward-compatible counterpart to the TLS guard above.
func TestStateSyncConfigurer_InternalWitnessUsesHTTP(t *testing.T) {
	homeDir := t.TempDir()
	setupPeersInConfig(t, homeDir, nil)

	const witness = "syncer-0-internal.arctic-1.svc.cluster.local:26657"
	hash := generateBlockHash()
	mock := &mockHTTPDoer{
		responses: map[string]*http.Response{
			"http://" + witness + "/status": jsonResponse(wrapResult(`{
				"sync_info": {"latest_block_height": "10000"}
			}`)),
			"http://" + witness + "/block?height=8000": jsonResponse(wrapResult(fmt.Sprintf(`{
				"block_id": {"hash": %q}
			}`, hash))),
		},
	}

	configurer := NewStateSyncConfigurer(homeDir, mock)
	if err := configurer.Configure(context.Background(), StateSyncRequest{
		RpcServers: []string{witness},
	}); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	ss := readTOML(t, filepath.Join(homeDir, "config", "config.toml"))["statesync"].(map[string]any)
	if ss["trust-height"] != int64(8000) {
		t.Errorf("expected trust-height 8000, got %v", ss["trust-height"])
	}
}

// When no candidate witness is reachable, fail at configure time with a clear
// error rather than writing a config that makes seid exit on "no witnesses".
func TestStateSyncConfigurer_NoReachableWitness(t *testing.T) {
	homeDir := t.TempDir()
	setupPeersInConfig(t, homeDir, []string{"nodeId1@1.2.3.4:26656"})

	configurer := NewStateSyncConfigurer(homeDir, &mockHTTPDoer{})
	err := configurer.Configure(context.Background(), StateSyncRequest{})
	if err == nil {
		t.Fatal("expected error when no witness is reachable")
	}
	if !strings.Contains(err.Error(), "no reachable RPC witness") {
		t.Errorf("unexpected error: %v", err)
	}
}
