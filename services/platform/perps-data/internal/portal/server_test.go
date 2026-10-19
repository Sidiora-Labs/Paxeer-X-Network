package portal

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/gateway"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/httpx"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/telemetry"
)

const (
	serverTestChainID    = 125
	serverTestAuthSecret = "portal-server-test-secret-of-32-bytes-or-more"
	seededSymbol         = "BTC"
	seededAccount        = "0x00000000000000000000000000000000000004b1"
)

// serverDB opens PERPS_DATA_TEST_DATABASE_URL with a fresh perps schema; the
// migrations seed the market records, so BTC exists and DOGE does not.
func serverDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("PERPS_DATA_TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("PERPS_DATA_TEST_DATABASE_URL is unset")
	}
	db, err := store.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS perps CASCADE`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

// gatewayServer serves a gateway built from gateway.NewServer over db, an
// in-process JetStream server and the node of PERPS_DATA_TEST_RPC. It is never
// run, so no consumer is attached and its readiness answers 503.
func gatewayServer(t *testing.T, db *sql.DB) *httptest.Server {
	t.Helper()
	rpcURL := os.Getenv("PERPS_DATA_TEST_RPC")
	if rpcURL == "" {
		t.Fatal("PERPS_DATA_TEST_RPC is unset")
	}
	node, err := ethclient.DialContext(t.Context(), rpcURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(node.Close)
	ns, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: server.RANDOM_PORT, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go ns.Start()
	t.Cleanup(ns.Shutdown)
	if !ns.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	metrics, err := telemetry.New(config.Gateway)
	if err != nil {
		t.Fatal(err)
	}
	gw, err := gateway.NewServer(t.Context(), gateway.Options{DB: db, NATS: nc, Chain: serverTestChainID, AuthSecret: serverTestAuthSecret, Node: node, Metrics: metrics})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(gw.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// portalServer serves a portal whose pages read gatewayURL.
func portalServer(t *testing.T, gatewayURL string) *httptest.Server {
	t.Helper()
	metrics, err := telemetry.New(config.Portal)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	pages, err := NewPages(gatewayURL, "https://explorer.invalid/tx/", client)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(pages, gatewayURL, metrics, client)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts
}

type answer struct {
	status int
	header http.Header
	body   string
}

func get(t *testing.T, url string) answer {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return answer{status: resp.StatusCode, header: resp.Header, body: string(body)}
}

func readiness(t *testing.T, url string) (int, map[string]string) {
	t.Helper()
	a := get(t, url+"/healthz/ready")
	var doc struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.Unmarshal([]byte(a.body), &doc); err != nil {
		t.Fatalf("readiness body %q: %v", a.body, err)
	}
	return a.status, doc.Checks
}

func renderCount(t *testing.T, portalURL, page string) string {
	t.Helper()
	a := get(t, portalURL+"/metrics")
	if a.status != http.StatusOK {
		t.Fatalf("/metrics answered %d", a.status)
	}
	prefix := `perps_portal_render_seconds_count{page="` + page + `"} `
	for _, line := range strings.Split(a.body, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	return "absent"
}

func TestPortalServer(t *testing.T) {
	db := serverDB(t)
	gw := gatewayServer(t, db)

	t.Run("pages_mounted", func(t *testing.T) {
		portal := portalServer(t, gw.URL)
		for _, path := range []string{"/", "/markets/" + seededSymbol, "/accounts/" + seededAccount, "/leaderboard", "/stats"} {
			a := get(t, portal.URL+path)
			if a.status != http.StatusOK {
				t.Fatalf("%s answered %d: %s", path, a.status, a.body)
			}
			if got := a.header.Get("Content-Type"); got != "text/html; charset=utf-8" {
				t.Fatalf("%s Content-Type %q", path, got)
			}
		}
		for page, want := range map[string]string{"markets": "1", "market": "1", "account": "1", "leaderboard": "1", "stats": "1"} {
			if got := renderCount(t, portal.URL, page); got != want {
				t.Fatalf("render count of %s = %s, want %s", page, got, want)
			}
		}
		first := get(t, portal.URL+"/stats")
		second := get(t, portal.URL+"/stats")
		if first.body != second.body {
			t.Fatal("cached /stats changed within the cache window")
		}
		if got := renderCount(t, portal.URL, "stats"); got != "1" {
			t.Fatalf("render count of stats after cached reads = %s, want 1", got)
		}
		get(t, portal.URL+"/stats?window=7d")
		if got := renderCount(t, portal.URL, "stats"); got != "2" {
			t.Fatalf("render count of stats after a new query = %s, want 2", got)
		}
	})

	t.Run("readiness_follows_gateway", func(t *testing.T) {
		code, checks := readiness(t, gw.URL)
		if code != http.StatusServiceUnavailable {
			t.Fatalf("gateway readiness answered %d, want 503", code)
		}
		portal := portalServer(t, gw.URL)
		code, checks = readiness(t, portal.URL)
		if code != http.StatusServiceUnavailable || checks["gateway"] != "failed" {
			t.Fatalf("portal readiness behind a 503 gateway = %d %v, want 503 with gateway failed", code, checks)
		}

		readyMux := http.NewServeMux()
		httpx.RegisterHealth(readyMux, httpx.HealthDeps{Postgres: db.PingContext})
		ready := httptest.NewServer(readyMux)
		defer ready.Close()
		portal = portalServer(t, ready.URL)
		code, checks = readiness(t, portal.URL)
		if code != http.StatusOK || checks["gateway"] != "ok" {
			t.Fatalf("portal readiness behind a ready service = %d %v, want 200 with gateway ok", code, checks)
		}

		ready.Close()
		code, checks = readiness(t, portal.URL)
		if code != http.StatusServiceUnavailable || checks["gateway"] != "failed" {
			t.Fatalf("portal readiness behind an unreachable gateway = %d %v, want 503 with gateway failed", code, checks)
		}
		if live := get(t, portal.URL+"/healthz/live"); live.status != http.StatusOK {
			t.Fatalf("liveness answered %d, want 200", live.status)
		}
	})

	t.Run("not_found_page", func(t *testing.T) {
		portal := portalServer(t, gw.URL)
		for path, message := range map[string]string{
			"/markets/doge":                        "Unknown symbol",
			"/markets/do-ge":                       "Unknown symbol",
			"/accounts/0x1234":                     "Malformed address",
			"/accounts/" + strings.Repeat("z", 42): "Malformed address",
			"/no-such-page":                        "Page not found",
		} {
			a := get(t, portal.URL+path)
			if a.status != http.StatusNotFound {
				t.Fatalf("%s answered %d, want 404", path, a.status)
			}
			if !strings.HasPrefix(strings.ToLower(a.body), "<!doctype html>") || !strings.Contains(a.body, "</html>") {
				t.Fatalf("%s 404 is not inside the page layout: %s", path, a.body)
			}
			if !strings.Contains(a.body, message) {
				t.Fatalf("%s 404 lacks %q: %s", path, message, a.body)
			}
		}
	})

	t.Run("csp_header", func(t *testing.T) {
		portal := portalServer(t, gw.URL)
		for _, path := range []string{"/", "/markets/" + seededSymbol, "/accounts/" + seededAccount, "/leaderboard", "/stats", "/markets/doge", "/no-such-page", "/healthz/live", "/healthz/ready", "/metrics"} {
			a := get(t, portal.URL+path)
			csp := a.header.Get("Content-Security-Policy")
			if !strings.HasPrefix(csp, "default-src 'self'") {
				t.Fatalf("%s Content-Security-Policy %q does not start with default-src 'self'", path, csp)
			}
			if !strings.Contains(csp, "script-src 'self';") {
				t.Fatalf("%s Content-Security-Policy %q allows a script source other than self", path, csp)
			}
			if csp != ContentSecurityPolicy {
				t.Fatalf("%s Content-Security-Policy %q, want %q", path, csp, ContentSecurityPolicy)
			}
		}
	})
}
