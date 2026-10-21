package app

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethclient/simulated"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

func networkReadsServer(t *testing.T, rpcURL string) *httptest.Server {
	t.Helper()
	d, err := newDevelopmentNetworkReads(context.Background(), rpcURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := rest.NewAdmission(100, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	d.register(router, admission)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv
}

func netCall(t *testing.T, method, url string) (int, []byte, http.Header) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body, resp.Header
}

// Every path the adapter mounts is a frozen v2 OpenAPI operation with the same
// method; nothing outside the contract is invented.
func TestDevelopmentNetworkReadsMountOnlyFrozenOperations(t *testing.T) {
	d, err := newDevelopmentNetworkReads(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	d.register(router, nil)
	raw, err := os.ReadFile("../../../../spec/production-system/integration/contracts/v2/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err = json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	count := 0
	err = chi.Walk(router, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		count++
		if _, ok := document.Paths[path][strings.ToLower(method)]; !ok {
			t.Errorf("mounted operation absent from frozen v2 OpenAPI: %s %s", method, path)
		}
		return nil
	})
	if err != nil || count != 6+len(developmentUnservedOperations) {
		t.Fatalf("walk: %v, %d operations", err, count)
	}
}

func TestDevelopmentDeploymentFromReviewedManifest(t *testing.T) {
	srv := networkReadsServer(t, "")
	status, body, header := netCall(t, http.MethodGet, srv.URL+"/v2/network/deployment")
	if status != 200 {
		t.Fatalf("status %d: %s", status, body)
	}
	if err := interfaces.ValidateJSON("DeploymentResponse", body); err != nil {
		t.Fatalf("not a DeploymentResponse: %v\n%s", err, body)
	}
	if header.Get("X-Schema-Version") != "2" || header.Get("Content-Type") != "application/json" {
		t.Fatalf("headers %v", header)
	}
	var got struct {
		Data struct {
			ChainID           string            `json:"chainId"`
			NativeSymbol      string            `json:"nativeSymbol"`
			ManifestHash      string            `json:"manifestHash"`
			ContractAddresses map[string]string `json:"contractAddresses"`
			Capabilities      map[string]bool   `json:"capabilities"`
		} `json:"data"`
		Quality string `json:"quality"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	m := deployment.Current()
	if got.Data.ChainID != "125" || got.Data.NativeSymbol != m.Native.Symbol || got.Data.ManifestHash != "0x"+m.SourceManifestSHA256 || got.Quality != "partial" {
		t.Fatalf("deployment data %s", body)
	}
	for key, role := range map[string]string{"factory": "SidioraFactory", "router": "Router", "quoter": "Quoter", "protocolConfig": "ProtocolConfig", "usdl": "USDL", "feeAccumulator": "FeeAccumulator", "nft": "SidioraNFT", "eventEmitter": "EventEmitter", "poolRegistry": "PoolRegistry"} {
		if !strings.EqualFold(got.Data.ContractAddresses[key], m.Roles[role].Address) {
			t.Errorf("%s = %s, manifest %s = %s", key, got.Data.ContractAddresses[key], role, m.Roles[role].Address)
		}
	}
	// The web pins the same manifest digest (core/network/deployment.ts).
	web, err := os.ReadFile("../../../../client/apps/web/src/core/network/deployment.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(web), `"manifestSha256": "`+m.SourceManifestSHA256+`"`) {
		t.Fatal("web CORE_DEPLOYMENT pins a different manifest")
	}
	active := m.Status == "active"
	if got.Data.Capabilities["creation"] != active || got.Data.Capabilities["swap"] != active || got.Data.Capabilities["conditionalOrders"] || got.Data.Capabilities["voice"] {
		t.Fatalf("capabilities %v", got.Data.Capabilities)
	}
}

func TestDevelopmentCupsAndUnservedOperations(t *testing.T) {
	srv := networkReadsServer(t, "")
	status, body, _ := netCall(t, http.MethodGet, srv.URL+"/v2/cups/current")
	if status != 200 || interfaces.ValidateJSON("CupCurrentResponse", body) != nil || !strings.Contains(string(body), `"cup":null`) {
		t.Fatalf("cups/current %d: %s", status, body)
	}
	for _, path := range []string{"/v2/cups/c1", "/v2/cups/c1/standings", "/v2/cups/c1/results"} {
		status, body, _ = netCall(t, http.MethodGet, srv.URL+path)
		if status != 404 || interfaces.ValidateJSON("Error", body) != nil {
			t.Fatalf("GET %s = %d: %s", path, status, body)
		}
	}
	replacer := strings.NewReplacer("{orderId}", "0f6b6c52-3f0e-4c38-9a59-2b2b3f6f8c11", "{alertId}", "a1", "{notificationId}", "n1", "{id}", "n1", "{narrativeId}", "x1")
	for _, op := range developmentUnservedOperations {
		path := replacer.Replace(op.path)
		status, body, header := netCall(t, op.method, srv.URL+path+"?limit=10")
		if status != 503 || interfaces.ValidateJSON("Error", body) != nil {
			t.Fatalf("%s %s = %d: %s", op.method, path, status, body)
		}
		var envelope struct {
			Code         string `json:"code"`
			Retryable    bool   `json:"retryable"`
			RetryAfterMs *int   `json:"retryAfterMs"`
		}
		if json.Unmarshal(body, &envelope) != nil || envelope.Code != "DEPENDENCY_UNAVAILABLE" || envelope.Retryable || envelope.RetryAfterMs != nil || header.Get("Retry-After") != "" {
			t.Fatalf("%s %s envelope %s", op.method, path, body)
		}
	}
	// Without NETWORK_RPC_URL the transaction read is not configured, not 404.
	status, body, _ = netCall(t, http.MethodGet, srv.URL+"/v2/transactions/0x"+strings.Repeat("ab", 32))
	if status != 503 || !strings.Contains(string(body), `"retryable":false`) {
		t.Fatalf("tx without RPC %d: %s", status, body)
	}
	status, _, _ = netCall(t, http.MethodGet, srv.URL+"/v2/transactions/0x1234")
	if status != 400 {
		t.Fatalf("malformed hash %d", status)
	}
}

// chain125 starts an in-process go-ethereum node (simulated beacon backend)
// with chain id 125 and JSON-RPC over HTTP, funding key.
func chain125(t *testing.T, key *ecdsa.PrivateKey) (*simulated.Backend, string) {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()
	chainConfig := *params.AllDevChainProtocolChanges
	chainConfig.ChainID = big.NewInt(125)
	alloc := types.GenesisAlloc{crypto.PubkeyToAddress(key.PublicKey): {Balance: new(big.Int).Mul(big.NewInt(1000), big.NewInt(params.Ether))}}
	backend := simulated.NewBackend(alloc, func(nodeConf *node.Config, ethConf *ethconfig.Config) {
		nodeConf.HTTPHost = "127.0.0.1"
		nodeConf.HTTPPort = port
		nodeConf.HTTPModules = []string{"eth", "net", "web3"}
		ethConf.Genesis.Config = &chainConfig
		ethConf.NetworkId = 125
	})
	t.Cleanup(func() { _ = backend.Close() })
	backend.Commit()
	return backend, fmt.Sprintf("http://127.0.0.1:%d", port)
}

func TestDevelopmentTransactionStatusFromChainRPC(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	backend, rpcURL := chain125(t, key)
	client := backend.Client()
	ctx := context.Background()
	from := crypto.PubkeyToAddress(key.PublicKey)
	signer := types.LatestSignerForChainID(big.NewInt(125))
	tip, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	head, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	feeCap := new(big.Int).Add(tip, new(big.Int).Mul(head.BaseFee, big.NewInt(2)))
	nonce := uint64(0)
	send := func(to *common.Address, data []byte, value *big.Int) common.Hash {
		tx, e := types.SignNewTx(key, signer, &types.DynamicFeeTx{ChainID: big.NewInt(125), Nonce: nonce, GasTipCap: tip, GasFeeCap: feeCap, Gas: 100000, To: to, Value: value, Data: data})
		if e != nil {
			t.Fatal(e)
		}
		if e = client.SendTransaction(ctx, tx); e != nil {
			t.Fatal(e)
		}
		nonce++
		return tx.Hash()
	}
	recipient := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	transfer := send(&recipient, nil, big.NewInt(1))
	// Contract creation whose init code reverts: PUSH1 0 PUSH1 0 REVERT.
	reverted := send(nil, common.FromHex("0x60006000fd"), big.NewInt(0))
	backend.Commit()
	pending := send(&recipient, nil, big.NewInt(2))

	srv := networkReadsServer(t, rpcURL)
	read := func(hash common.Hash) (string, map[string]any) {
		status, body, header := netCall(t, http.MethodGet, srv.URL+"/v2/transactions/"+hash.Hex())
		if status != 200 {
			t.Fatalf("tx %s = %d: %s", hash.Hex(), status, body)
		}
		if err := interfaces.ValidateJSON("TxStatusResponse", body); err != nil {
			t.Fatalf("not a TxStatusResponse: %v\n%s", err, body)
		}
		if header.Get("Cache-Control") != "no-store" {
			t.Fatalf("cache-control %q", header.Get("Cache-Control"))
		}
		var envelope struct {
			Data struct {
				Status  string         `json:"status"`
				Receipt map[string]any `json:"receipt"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data.Status, envelope.Data.Receipt
	}

	status, receipt := read(transfer)
	chainReceipt, err := client.TransactionReceipt(ctx, transfer)
	if err != nil {
		t.Fatal(err)
	}
	if status != "included_success" || receipt["success"] != true || !strings.EqualFold(receipt["from"].(string), from.Hex()) || !strings.EqualFold(receipt["to"].(string), recipient.Hex()) || receipt["height"] != chainReceipt.BlockNumber.String() || receipt["blockHash"] != chainReceipt.BlockHash.Hex() || receipt["gasUsedRaw"] != fmt.Sprint(chainReceipt.GasUsed) || receipt["effectiveGasPriceRaw"] != chainReceipt.EffectiveGasPrice.String() {
		t.Fatalf("transfer: %s %v (chain %+v)", status, receipt, chainReceipt)
	}
	status, receipt = read(reverted)
	if status != "included_reverted" || receipt["success"] != false || receipt["to"] != nil {
		t.Fatalf("reverted: %s %v", status, receipt)
	}
	position := receipt["position"].(map[string]any)
	if position["transactionIndex"] != "1" || position["chainId"] != "125" {
		t.Fatalf("position %v", position)
	}
	if status, receipt = read(pending); status != "submitted" || receipt != nil {
		t.Fatalf("pending: %s %v", status, receipt)
	}
	if status, receipt = read(common.HexToHash("0x" + strings.Repeat("cd", 32))); status != "unknown" || receipt != nil {
		t.Fatalf("unknown: %s %v", status, receipt)
	}
	backend.Commit()
	if status, _ = read(pending); status != "included_success" {
		t.Fatalf("pending after commit: %s", status)
	}
}
