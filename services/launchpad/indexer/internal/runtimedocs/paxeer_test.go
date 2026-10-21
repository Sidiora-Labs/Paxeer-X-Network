package runtimedocs

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/internaltest/chainfixture"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/receipt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/source"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// The real chain-125 market used by these tests: created at block 33963716 by
// the factory through the Router, bought at 33963733, sold at 33963749, and
// fees claimed at 33963761. The fixture is the exact JSON-RPC traffic of the
// production code paths against https://api5.mainnet-beta.paxeer.network,
// recorded with INDEXER_RECORD_PAXEER_RPC=<url> and replayed byte-for-byte.
var (
	realPool        = common.HexToAddress("0xCE20Da50F92D21d8BeE497910c782E9Aa25b6263")
	realToken       = common.HexToAddress("0x0F92FDD087c96dD3413eF98646b94D0d986192FA")
	realDeployer    = "0x1dae78b53c9f39930a2ff2b6b7aac12ec75fb154"
	realAnchor      = receipt.BlockAnchor{Height: 33963715, Hash: common.HexToHash("0x82477110c5fc791f6bef5a7d96636dc457e83daf59c8b802e264bada02f13395")}
	realPoolRuntime = common.HexToHash("0xa9b304d6d0aa562f283b45978b84ced8840a9331f8ac946742670b1b02a2872d")
	realCreation    = uint64(33963716)
	realBuy         = uint64(33963733)
	realSell        = uint64(33963749)
	realClaim       = uint64(33963761)
	realBuyHash     = "0x50f4e42d7247aa1665d1aaebc33039c8fc8e493727a864085ba77910dda56f6b"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "..")
}

func realInputs(t *testing.T) Inputs {
	t.Helper()
	root := repoRoot(t)
	reviewed, err := os.ReadFile(filepath.Join(root, "sdk", "launchpad", "protocol", "catalog", "reviewed.json"))
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := os.ReadFile(filepath.Join(root, "contracts", "launchpad", "artifacts", "contracts", "core", "SidioraERC20.sol", "SidioraERC20.json"))
	if err != nil {
		t.Fatal(err)
	}
	var compiled struct {
		DeployedBytecode string `json:"deployedBytecode"`
	}
	if err = json.Unmarshal(artifact, &compiled); err != nil {
		t.Fatal(err)
	}
	code, err := hex.DecodeString(strings.TrimPrefix(compiled.DeployedBytecode, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	return Inputs{Manifest: deployment.Current(), ReviewedCatalog: reviewed, ERC20RuntimeCode: code, PoolRuntimeHash: realPoolRuntime, Anchor: realAnchor}
}

func realAuthority(t *testing.T, ctx context.Context, url string, mutate func(*receipt.RPCOptions)) (*receipt.RPCQualification, error) {
	t.Helper()
	options, _, err := ReceiptOptions(realInputs(t))
	if err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(&options)
	}
	client, closeClient, err := receipt.NewBoundedRPCClient(ctx, url, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeClient)
	return receipt.NewRPCQualification(ctx, client, options)
}

func qualifyRange(t *testing.T, ctx context.Context, authority *receipt.RPCQualification, from, to uint64) (store.CanonicalBatch, []*receipt.QualifiedReceipt) {
	t.Helper()
	batch, receipts, err := authority.FetchQualifiedBatch(ctx, from, to)
	if err != nil {
		t.Fatalf("qualify %d..%d: %v", from, to, err)
	}
	return batch, receipts
}

func TestPaxeerReportedIdentityQualifiesRealMarket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := chainfixture.New(t)
	url := fixture.Serve(t)
	authority, err := realAuthority(t, ctx, url, nil)
	if err != nil {
		t.Fatalf("production authority against chain 125: %v", err)
	}

	// Creation: the factory MarketCreated discovers the token (immutable
	// masked ERC20 template) and the pool (beacon proxy template).
	batch, receipts := qualifyRange(t, ctx, authority, realCreation, realCreation)
	if len(batch.Blocks) != 1 || batch.Blocks[0].Hash != "0x81a18d822e327fbcbfe2be91b7f666bfa043016cb0e3f094c5bbabe91f9d192b" {
		t.Fatalf("creation block identity is not the node-reported hash: %+v", batch.Blocks)
	}
	if len(receipts) != 1 {
		t.Fatalf("creation receipts %d", len(receipts))
	}
	events, err := receipts[0].QualifiedEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range events {
		names[e.EmitterRole+"."+e.EventName] = true
	}
	for _, want := range []string{"SidioraFactory.MarketCreated", "EventEmitter.MarketCreated", "EventEmitter.TokenDeployed"} {
		if !names[want] {
			t.Fatalf("creation lacks qualified %s: %v", want, names)
		}
	}
	discovered := authority.DiscoveredEpochs()
	if len(discovered) != 2 {
		t.Fatalf("discovered epochs %+v", discovered)
	}
	for _, e := range discovered {
		if e.Deployment.Address != realPool && e.Deployment.Address != realToken || e.Deployment.From.Block != realCreation {
			t.Fatalf("unexpected discovery %+v", e)
		}
	}
	tokenLogs := 0
	for _, r := range batch.Records {
		if r.Emitter == strings.ToLower(realToken.Hex()) {
			tokenLogs++
		}
		if r.Emitter == "0xbb82d74e071e03d8e68f1d78fae9efe8b6b7a723" {
			t.Fatal("treasury log recorded as a qualified emitter")
		}
	}
	if tokenLogs != 2 {
		t.Fatalf("token transfer logs in creation batch %d", tokenLogs)
	}

	// Buy: one qualified accounting fill bound to the reported block hash.
	_, receipts = qualifyRange(t, ctx, authority, realBuy, realBuy)
	fills, err := receipts[0].Fills(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(fills) != 1 || fills[0].Side != "buy" || fills[0].PoolAddress != strings.ToLower(realPool.Hex()) || fills[0].Position.BlockHash != realBuyHash || fills[0].Token.Address != strings.ToLower(realToken.Hex()) || fills[0].Settlement.Decimals != 6 {
		t.Fatalf("buy fill %+v", fills)
	}
	if fills[0].Attribution.TransactionSender != realDeployer {
		t.Fatalf("buy sender %s", fills[0].Attribution.TransactionSender)
	}
	_, receipts = qualifyRange(t, ctx, authority, realSell, realSell)
	fills, err = receipts[0].Fills(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(fills) != 1 || fills[0].Side != "sell" {
		t.Fatalf("sell fill %+v", fills)
	}
	_, receipts = qualifyRange(t, ctx, authority, realClaim, realClaim)
	if fills, err = receipts[0].Fills(ctx); err != nil || len(fills) != 0 {
		t.Fatalf("fee claim fills %v %+v", err, fills)
	}

	// Reserves pinned to the swap blocks (the pool never emits PoolStateUpdated).
	buyReserves, err := authority.PoolReserves(ctx, realPool, common.HexToHash(realBuyHash))
	if err != nil {
		t.Fatal(err)
	}
	sellBlock, _, err := blockAt(ctx, url, realSell)
	if err != nil {
		t.Fatal(err)
	}
	sellReserves, err := authority.PoolReserves(ctx, realPool, sellBlock)
	if err != nil {
		t.Fatal(err)
	}
	if buyReserves.VirtualUSDL.Sign() <= 0 || buyReserves.Token.Sign() <= 0 || buyReserves.Price.Sign() <= 0 || buyReserves.RealUSDL.Cmp(sellReserves.RealUSDL) == 0 || buyReserves.Token.Cmp(sellReserves.Token) >= 0 {
		t.Fatalf("pinned reserves buy=%+v sell=%+v", buyReserves, sellReserves)
	}
}

func blockAt(ctx context.Context, url string, height uint64) (common.Hash, common.Hash, error) {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "eth_getBlockByNumber", "params": []any{"0x" + new(big.Int).SetUint64(height).Text(16), false}})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return common.Hash{}, common.Hash{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return common.Hash{}, common.Hash{}, err
	}
	defer response.Body.Close()
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	if err = json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return common.Hash{}, common.Hash{}, err
	}
	var header types.Header
	if err = json.Unmarshal(envelope.Result, &header); err != nil {
		return common.Hash{}, common.Hash{}, err
	}
	var reported struct {
		Hash common.Hash `json:"hash"`
	}
	if err = json.Unmarshal(envelope.Result, &reported); err != nil {
		return common.Hash{}, common.Hash{}, err
	}
	return reported.Hash, header.Hash(), nil
}

func TestPaxeerKeccakIdentityCannotQualifyChain125(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	url := chainfixture.New(t).Serve(t)
	reported, recomputed, err := blockAt(ctx, url, realAnchor.Height)
	if err != nil {
		t.Fatal(err)
	}
	if reported != realAnchor.Hash || recomputed == reported {
		t.Fatalf("anchor reported=%s recomputed=%s", reported.Hex(), recomputed.Hex())
	}
	if _, err = realAuthority(t, ctx, url, func(o *receipt.RPCOptions) { o.BlockIdentity = receipt.BlockIdentityKeccak }); err == nil {
		t.Fatal("go-ethereum header hashing accepted a CometBFT block identity")
	}
	if _, err = realAuthority(t, ctx, url, func(o *receipt.RPCOptions) {
		o.Anchor = &receipt.BlockAnchor{Height: realAnchor.Height, Hash: recomputed}
	}); err == nil {
		t.Fatal("anchor accepted with a hash the node does not report")
	}
}

func TestPaxeerRehydratedAuthorityQualifiesTradesOfEarlierMarket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	url := chainfixture.New(t).Serve(t)
	fresh, err := realAuthority(t, ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = fresh.FetchQualifiedBatch(ctx, realSell, realSell); err == nil {
		t.Fatal("trade of an undiscovered market qualified (token transfers from an unadmitted emitter)")
	}
	restarted, err := realAuthority(t, ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.Rehydrate(ctx, []receipt.DiscoveredMarket{{Token: realToken, Pool: realPool, Height: realCreation, TransactionIndex: 0}}); err != nil {
		t.Fatal(err)
	}
	_, receipts := qualifyRange(t, ctx, restarted, realSell, realSell)
	fills, err := receipts[0].Fills(ctx)
	if err != nil || len(fills) != 1 || fills[0].Side != "sell" {
		t.Fatalf("rehydrated sell %v %+v", err, fills)
	}
	forged, err := realAuthority(t, ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = forged.Rehydrate(ctx, []receipt.DiscoveredMarket{{Token: common.HexToAddress(deployment.Current().Roles["USDL"].Address), Pool: realPool, Height: realCreation}}); err == nil {
		t.Fatal("rehydration admitted a market the PoolRegistry does not bind")
	}
}

func TestPaxeerTamperedReceiptIsRejected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	fixture := chainfixture.New(t)
	if fixture.Recording() {
		t.Skip("tamper replay runs against the recorded fixture only")
	}
	tampered := false
	fixture.Mutate = func(key string, result json.RawMessage) json.RawMessage {
		if !strings.HasPrefix(key, "eth_getBlockReceipts ") || !strings.Contains(key, realBuyHash) {
			return result
		}
		// Alter the first log's data by one bit.
		altered := strings.Replace(string(result), `"data":"0x0000`, `"data":"0x0001`, 1)
		if altered != string(result) {
			tampered = true
		}
		return json.RawMessage(altered)
	}
	url := fixture.Serve(t)
	authority, err := realAuthority(t, ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = authority.Rehydrate(ctx, []receipt.DiscoveredMarket{{Token: realToken, Pool: realPool, Height: realCreation}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = authority.FetchQualifiedBatch(ctx, realBuy, realBuy); err == nil {
		t.Fatal("altered receipt log data qualified")
	}
	if !tampered {
		t.Fatal("tamper hook did not alter the recorded receipts")
	}
}

// The committed source and the live qualified source together: independent
// eth_getLogs coverage including discovered token and pool emitters, bound to
// the receipt authority, extending the durable anchor.
func TestPaxeerLiveQualifiedSourceCoversDiscoveredEmitters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	url := chainfixture.New(t).Serve(t)
	authority, err := realAuthority(t, ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	options, _, err := ReceiptOptions(realInputs(t))
	if err != nil {
		t.Fatal(err)
	}
	committed, err := CommittedOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	committed.Endpoints[PrimaryEndpointID] = url
	rpcSource, err := source.NewCommittedSource(ctx, committed)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rpcSource.Close() })
	durable := realAnchor.Height
	hashes := map[uint64]common.Hash{realAnchor.Height: realAnchor.Hash}
	live := &source.LiveQualifiedSource{RPC: rpcSource, Authority: authority, DurableHeight: func() uint64 { return durable }, PriorHash: func(_ context.Context, h uint64) (common.Hash, error) {
		hash, ok := hashes[h]
		if !ok {
			return common.Hash{}, errors.New("no durable hash")
		}
		return hash, nil
	}}
	batch, receipts, err := live.FetchQualifiedBatch(ctx, realCreation, realCreation)
	if err != nil {
		t.Fatalf("live creation batch: %v", err)
	}
	if len(receipts) != 1 || batch.Blocks[0].RawCount != len(batch.Records) || len(batch.Records) < 12 {
		t.Fatalf("live creation batch %d receipts %d records", len(receipts), len(batch.Records))
	}
	hashes[realCreation] = common.HexToHash(batch.Blocks[0].Hash)
	durable = realCreation
	// A batch that does not extend the durable checkpoint is refused.
	hashes[realCreation] = common.HexToHash(realBuyHash)
	if _, _, err = live.FetchQualifiedBatch(ctx, realCreation+1, realCreation+1); err == nil {
		t.Fatal("batch accepted although its parent is not the durable block")
	}
}
