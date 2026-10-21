package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testNodeID = "0123456789abcdef0123456789abcdef01234567"

func newTestServer(t *testing.T, dir string) *server {
	t.Helper()
	reg, err := openRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &server{cfg: config{ChainID: "hyperpax_125-1", RegisterTok: "operator-token"}, reg: reg}
}

func register(s *server, remote string, headers map[string]string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/register", strings.NewReader(body))
	req.RemoteAddr = remote
	req.Header.Set("X-HPX-Token", "operator-token")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	s.handleRegister(rec, req)
	return rec
}

func registered(t *testing.T, s *server) []*Node {
	t.Helper()
	return s.reg.list()
}

func TestRegisterRecordsProxyObservedAddress(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	body := `{"node_id":"` + testNodeID + `","ip":"8.8.8.8","p2p_port":26656,"moniker":"hpx-node","type":"fullnode"}`
	rec := register(s, "127.0.0.1:41000", map[string]string{"X-Real-IP": "203.0.113.7", "X-Forwarded-For": "8.8.4.4"}, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	nodes := registered(t, s)
	if len(nodes) != 1 || nodes[0].IP != "203.0.113.7" || nodes[0].Peer() != testNodeID+"@203.0.113.7:26656" {
		t.Fatalf("unexpected registry contents %+v", nodes)
	}
}

func TestSpoofedForwardingHeadersIgnoredOffLoopback(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	body := `{"node_id":"` + testNodeID + `","p2p_port":26656}`
	rec := register(s, "198.51.100.9:41000", map[string]string{"X-Real-IP": "203.0.113.50", "X-Forwarded-For": "203.0.113.51"}, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	nodes := registered(t, s)
	if len(nodes) != 1 || nodes[0].IP != "198.51.100.9" {
		t.Fatalf("spoofed address accepted: %+v", nodes)
	}
}

func TestMalformedRegistrationsRejectedWithoutPersistence(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(t, dir)
	public := map[string]string{"X-Real-IP": "203.0.113.7"}
	cases := []struct {
		name    string
		remote  string
		headers map[string]string
		body    string
		want    int
	}{
		{"short node id", "127.0.0.1:1", public, `{"node_id":"abc"}`, http.StatusBadRequest},
		{"non-hex node id", "127.0.0.1:1", public, `{"node_id":"` + strings.Repeat("z", 40) + `"}`, http.StatusBadRequest},
		{"private source", "127.0.0.1:1", map[string]string{"X-Real-IP": "10.1.2.3"}, `{"node_id":"` + testNodeID + `"}`, http.StatusBadRequest},
		{"loopback source", "127.0.0.1:1", nil, `{"node_id":"` + testNodeID + `"}`, http.StatusBadRequest},
		{"malformed source", "127.0.0.1:1", map[string]string{"X-Real-IP": "203.0.113.999"}, `{"node_id":"` + testNodeID + `"}`, http.StatusBadRequest},
		{"port out of range", "127.0.0.1:1", public, `{"node_id":"` + testNodeID + `","p2p_port":70000}`, http.StatusBadRequest},
		{"negative port", "127.0.0.1:1", public, `{"node_id":"` + testNodeID + `","p2p_port":-1}`, http.StatusBadRequest},
		{"control moniker", "127.0.0.1:1", public, `{"node_id":"` + testNodeID + `","moniker":"a\u0007b"}`, http.StatusBadRequest},
		{"oversized version", "127.0.0.1:1", public, `{"node_id":"` + testNodeID + `","version":"` + strings.Repeat("v", 65) + `"}`, http.StatusBadRequest},
		{"bad json", "127.0.0.1:1", public, `{"node_id":`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		if rec := register(s, tc.remote, tc.headers, tc.body); rec.Code != tc.want {
			t.Errorf("%s: status %d want %d", tc.name, rec.Code, tc.want)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/register", strings.NewReader(`{"node_id":"`+testNodeID+`"}`))
	req.RemoteAddr = "127.0.0.1:1"
	req.Header.Set("X-Real-IP", "203.0.113.7")
	req.Header.Set("X-HPX-Token", "wrong-token")
	rec := httptest.NewRecorder()
	s.handleRegister(rec, req)
	if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "operator-token") {
		t.Errorf("wrong token: status %d body %q", rec.Code, rec.Body)
	}
	get := httptest.NewRecorder()
	s.handleRegister(get, httptest.NewRequest(http.MethodGet, "/api/register", nil))
	if get.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET register status %d", get.Code)
	}
	if nodes := registered(t, s); len(nodes) != 0 {
		t.Fatalf("rejected registrations persisted: %+v", nodes)
	}
	if _, err := os.Stat(filepath.Join(dir, "registry.json")); !os.IsNotExist(err) {
		t.Fatalf("registry file written for rejected registrations: %v", err)
	}
}

func TestRegistrationRateLimited(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	body := `{"node_id":"` + testNodeID + `"}`
	headers := map[string]string{"X-Real-IP": "203.0.113.7"}
	for i := 0; i < registerBurst; i++ {
		if rec := register(s, "127.0.0.1:1", headers, body); rec.Code != http.StatusOK {
			t.Fatalf("registration %d status %d", i, rec.Code)
		}
	}
	if rec := register(s, "127.0.0.1:1", headers, body); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over-limit registration status %d", rec.Code)
	}
	if rec := register(s, "127.0.0.1:1", map[string]string{"X-Real-IP": "203.0.113.8"}, body); rec.Code != http.StatusOK {
		t.Fatalf("independent source limited: status %d", rec.Code)
	}
}

func TestRegistryRecoversPeersAfterRestart(t *testing.T) {
	dir := t.TempDir()
	first := newTestServer(t, dir)
	other := "fedcba9876543210fedcba9876543210fedcba98"
	for ip, id := range map[string]string{"203.0.113.7": testNodeID, "203.0.113.8": other} {
		if rec := register(first, "127.0.0.1:1", map[string]string{"X-Real-IP": ip}, `{"node_id":"`+id+`","p2p_port":26656}`); rec.Code != http.StatusOK {
			t.Fatalf("register %s: %d", id, rec.Code)
		}
	}
	restarted := newTestServer(t, dir)
	rec := httptest.NewRecorder()
	restarted.handlePeers(rec, httptest.NewRequest(http.MethodGet, "/api/peers?self="+other, nil))
	var peers struct {
		ChainID string   `json:"chain_id"`
		Peers   []string `json:"peers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &peers); err != nil {
		t.Fatal(err)
	}
	if peers.ChainID != "hyperpax_125-1" || len(peers.Peers) != 1 || peers.Peers[0] != testNodeID+"@203.0.113.7:26656" {
		t.Fatalf("restart lost peers: %+v", peers)
	}
	if nodes := registered(t, restarted); len(nodes) != 2 {
		t.Fatalf("restart lost nodes: %+v", nodes)
	}
}

func TestArtifactSurfaceServesOnlyDeclaredFiles(t *testing.T) {
	root := t.TempDir()
	for rel, content := range map[string]string{
		"paxd":                     "binary",
		"checksums.txt":            "manifest",
		"lib/libwasmvm.x86_64.so":  "library",
		"config/fullnode/app.toml": "app",
		"undeclared.txt":           "private",
		"lib/undeclared.so":        "private",
	} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	handler := artifactHandler(root)
	fetch := func(method, target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
		return rec
	}
	for target, want := range map[string]string{"/paxd": "binary", "/lib/libwasmvm.x86_64.so": "library", "/config/fullnode/app.toml": "app"} {
		rec := fetch(http.MethodGet, target)
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), []byte(want)) || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: status %d body %q", target, rec.Code, rec.Body)
		}
	}
	if rec := fetch(http.MethodGet, "/checksums.txt"); rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("manifest cacheable: %q", rec.Header().Get("Cache-Control"))
	}
	for _, target := range []string{"/", "/lib/", "/lib", "/config/", "/undeclared.txt", "/lib/undeclared.so", "/../undeclared.txt", "/lib/../undeclared.txt", "/genesis.json"} {
		if rec := fetch(http.MethodGet, target); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d", target, rec.Code)
		}
	}
	if rec := fetch(http.MethodPost, "/paxd"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /paxd: status %d", rec.Code)
	}
}

func TestRegistryListensOnlyOnLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:8099": true, "[::1]:8099": true, ":8099": false, "0.0.0.0:8099": false,
		"[::]:8099": false, "203.0.113.1:8099": false, "localhost:8099": false, "127.0.0.1": false,
	} {
		if got := loopbackListen(addr); got != want {
			t.Errorf("loopbackListen(%q) = %v want %v", addr, got, want)
		}
	}
	t.Setenv("HPX_ADDR", "")
	if cfg := loadConfig(); cfg.Addr != "127.0.0.1:8099" {
		t.Fatalf("default listen address %q", cfg.Addr)
	}
}
