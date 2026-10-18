package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/capabilities"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// The production network reads run the production construction path: a
// NETWORK_AUTHORITY_CONFIG_FILE document bound by its SHA-256 (the reviewed
// manifest, BeaconProxy runtime, interface digest, expected preflight revision
// and the manifest addresses), newNetworkAuthority over the live chain 125
// HTTPS RPC (NETWORK_RPC_URL), and RegisterNetworkReads on a router. The
// transaction read is checked against the real swap of pool
// 0xce20da50f92d21d8bee497910c782e9aa25b6263 at block 33963749; the pruned
// public RPC keeps ~100k blocks, so a run after that window reports the
// receipt as unknown and the test says so instead of passing silently.

const (
	productionPoolSwapTx     = "0xf5f9ed1136e71f03ffd44ae7f80ee192998b80fccd96ad3f75824fa9ef3c2845"
	productionPoolSwapHeight = "33963749"
	productionPoolSwapBlock  = "0x1cd6b450aa87745be16a8760a928a2e7ddd1097e872bdcd99b31ff66f77c2945"
)

func productionNetworkRPC(t *testing.T) string {
	t.Helper()
	endpoint := os.Getenv("NETWORK_RPC_URL")
	if !strings.HasPrefix(endpoint, "https://") {
		t.Skip("NETWORK_RPC_URL (https, chain 125) is required for the production network reads")
	}
	return endpoint
}

// writeNetworkAuthorityDocument writes the bound production document and
// returns its path and SHA-256. The expected preflight revision is derived
// from the same reviewed inputs over the live chain.
func writeNetworkAuthorityDocument(t *testing.T, endpoint string) (string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	manifest := deployment.Current()
	client, err := dialNetworkRPC(ctx, endpoint, false)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	reader, err := capabilities.NewPinnedRPCReader(client)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := capabilities.NewPreflightEngine(reader, manifest, developmentPoolRuntime, developmentSerialInterface, time.Minute, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	document := networkAuthorityConfig{
		SchemaVersion:          2,
		Manifest:               manifest,
		ReviewedPoolRuntime:    developmentPoolRuntime,
		SerialInterfaceSHA256:  developmentSerialInterface,
		ExpectedRevisionSHA256: engine.Revision().RevisionSHA256,
		SourceSchema:           "indexer",
		Endpoints:              deployment.PublicEndpoints{RPC: endpoint, Explorer: developmentExplorer, AllowedRPCs: []string{endpoint}, AllowedExplorers: []string{developmentExplorer}},
		GoAddresses:            manifest.Addresses(),
		FrontendAddresses:      manifest.Addresses(),
	}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("network authority document: expectedRevisionSha256=%s documentSha256=%x", document.ExpectedRevisionSHA256, sha256.Sum256(raw))
	path := filepath.Join(t.TempDir(), "network-authority.json")
	if err = os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, fmt.Sprintf("%x", sha256.Sum256(raw))
}

func productionNetworkReadsServer(t *testing.T, cfg *config.Config) *httptest.Server {
	t.Helper()
	authority, err := newNetworkAuthority(context.Background(), nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(authority.Close)
	admission, err := rest.NewAdmission(100, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	authority.RegisterNetworkReads(router, admission)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv
}

func TestProductionNetworkReadsFromBoundAuthorityDocument(t *testing.T) {
	endpoint := productionNetworkRPC(t)
	path, digest := writeNetworkAuthorityDocument(t, endpoint)
	srv := productionNetworkReadsServer(t, &config.Config{NodeEnv: "production", NetworkAuthorityFile: path, NetworkAuthoritySHA256: digest, NetworkRPCURL: endpoint})

	status, body, header := netCall(t, "GET", srv.URL+"/v2/network/deployment")
	if status != 200 || interfaces.ValidateJSON("DeploymentResponse", body) != nil || header.Get("X-Schema-Version") != "2" {
		t.Fatalf("deployment %d %s", status, body)
	}
	var deploymentBody struct {
		Data struct {
			ChainID           string            `json:"chainId"`
			ManifestHash      string            `json:"manifestHash"`
			ContractAddresses map[string]string `json:"contractAddresses"`
			Capabilities      map[string]bool   `json:"capabilities"`
		} `json:"data"`
		Quality string `json:"quality"`
	}
	if err := json.Unmarshal(body, &deploymentBody); err != nil {
		t.Fatal(err)
	}
	manifest := deployment.Current()
	if deploymentBody.Data.ChainID != "125" || deploymentBody.Data.ManifestHash != "0x"+manifest.SourceManifestSHA256 || deploymentBody.Quality != "partial" || !strings.EqualFold(deploymentBody.Data.ContractAddresses["usdl"], manifest.Roles["USDL"].Address) || deploymentBody.Data.Capabilities["swap"] != (manifest.Status == "active") {
		t.Fatalf("deployment data %+v", deploymentBody)
	}

	status, body, _ = netCall(t, "GET", srv.URL+"/v2/transactions/"+productionPoolSwapTx)
	if status != 200 || interfaces.ValidateJSON("TxStatusResponse", body) != nil {
		t.Fatalf("transaction %d %s", status, body)
	}
	var tx struct {
		Data struct {
			Status  string `json:"status"`
			Receipt *struct {
				Height    string `json:"height"`
				BlockHash string `json:"blockHash"`
				Success   bool   `json:"success"`
				Position  struct {
					TransactionIndex string `json:"transactionIndex"`
				} `json:"position"`
			} `json:"receipt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &tx); err != nil {
		t.Fatal(err)
	}
	if tx.Data.Status == "unknown" {
		t.Fatalf("the pruned RPC no longer serves block %s; rerun against an archive NETWORK_RPC_URL: %s", productionPoolSwapHeight, body)
	}
	if tx.Data.Status != "included_success" || tx.Data.Receipt == nil || tx.Data.Receipt.Height != productionPoolSwapHeight || !strings.EqualFold(tx.Data.Receipt.BlockHash, productionPoolSwapBlock) || !tx.Data.Receipt.Success || tx.Data.Receipt.Position.TransactionIndex != "0" {
		t.Fatalf("transaction status %s", body)
	}

	status, body, _ = netCall(t, "GET", srv.URL+"/v2/transactions/0x"+strings.Repeat("0", 63)+"1")
	if status != 200 || !strings.Contains(string(body), `"status":"unknown"`) {
		t.Fatalf("unknown transaction %d %s", status, body)
	}
	if status, _, _ = netCall(t, "GET", srv.URL+"/v2/transactions/0x1234"); status != 400 {
		t.Fatalf("malformed hash answered %d", status)
	}
}

// A document whose bytes differ from the bound digest fails construction, so
// production never serves a manifest other than the reviewed one.
func TestProductionNetworkAuthorityRejectsUnboundDocument(t *testing.T) {
	endpoint := productionNetworkRPC(t)
	path, digest := writeNetworkAuthorityDocument(t, endpoint)
	wrong := strings.Repeat("0", 64)
	if digest == wrong {
		t.Fatal("digest collision")
	}
	if _, err := newNetworkAuthority(context.Background(), nil, &config.Config{NodeEnv: "production", NetworkAuthorityFile: path, NetworkAuthoritySHA256: wrong, NetworkRPCURL: endpoint}); err == nil {
		t.Fatal("unbound network authority document accepted")
	}
}

// Without the reviewed document both reads answer the frozen non-retryable
// 503 (never a plain-text 404).
func TestProductionNetworkReadsWithoutAuthorityDocument(t *testing.T) {
	srv := productionNetworkReadsServer(t, &config.Config{NodeEnv: "production"})
	for _, path := range []string{"/v2/network/deployment", "/v2/transactions/" + productionPoolSwapTx} {
		status, body, _ := netCall(t, "GET", srv.URL+path)
		if status != 503 || !strings.Contains(string(body), `"retryable":false`) {
			t.Fatalf("%s answered %d %s", path, status, body)
		}
	}
}
