package l18

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

var swapABI = parseABI(`[{"type":"function","name":"swap","inputs":[{"name":"amountIn","type":"uint256"},{"name":"minOut","type":"uint256"},{"name":"isBuy","type":"bool"},{"name":"recipient","type":"address"},{"name":"deadline","type":"uint256"}],"outputs":[{"name":"out","type":"uint256"}]}]`)
var mintABI = parseABI(`[{"type":"function","name":"mint","inputs":[{"name":"to","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[]}]`)

func send(t *testing.T, r *fixtureRPC, from, to string, a abi.ABI, method string, args ...any) string {
	t.Helper()
	data, e := a.Pack(method, args...)
	if e != nil {
		t.Fatal(e)
	}
	var h string
	e = r.CallContext(context.Background(), &h, "eth_sendTransaction", map[string]any{"from": from, "to": to, "data": hexutil.Encode(data), "gas": "0x1e8480"})
	if e != nil {
		t.Fatal(e)
	}
	if !hash.MatchString(h) {
		t.Fatal(h)
	}
	return h
}
func receiptStatus(t *testing.T, r *fixtureRPC, h string) uint64 {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var rec *struct {
			Status hexutil.Uint64 `json:"status"`
		}
		if e := r.CallContext(context.Background(), &rec, "eth_getTransactionReceipt", h); e != nil {
			t.Fatal(e)
		}
		if rec != nil {
			return uint64(rec.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("actual receipt remained unavailable")
	return 0
}

func mustSend(t *testing.T, r *fixtureRPC, from, to string, a abi.ABI, method string, args ...any) string {
	t.Helper()
	h := send(t, r, from, to, a, method, args...)
	if receiptStatus(t, r, h) != 1 {
		t.Fatalf("reverted %s %s", method, h)
	}
	return h
}
func TestActualAnvilCreatorAndEpochStorage(t *testing.T) {
	path := os.Getenv("L18_CHAIN_FIXTURE")
	if path == "" {
		t.Fatal("L18_CHAIN_FIXTURE required: actual owned Anvil contract fixture")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var f struct{ RPC, Pool, Token, Admin, Alice, Bob string }
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	if f.RPC != "http://127.0.0.1:21800" {
		t.Fatal("fixture endpoint outside lease")
	}
	r, e := newFixtureRPC(f.RPC)
	if e != nil {
		t.Fatal(e)
	}

	ctx := context.Background()
	c := NewCreator(r, time.Now)
	l := storage(t)
	p, e := c.Preview(ctx, "1", f.Alice)
	if e != nil || p.Owner != strings.ToLower(f.Alice) || p.ClaimableRaw != "10000000" {
		t.Fatalf("preview %+v %v", p, e)
	}
	call, e := c.PrepareClaim(ctx, "1", f.Alice, f.Alice)
	if e != nil || call.ChainID != 125 || call.To != FeesRouter || call.Value != "0" {
		t.Fatalf("call %+v %v", call, e)
	}
	// Refresh ownership after a real NFT transfer. Metadata display stays Alice.
	mustSend(t, r, f.Alice, NFT, nft, "transferFrom", common.HexToAddress(f.Alice), common.HexToAddress(f.Bob), big.NewInt(1))
	if _, e = c.PrepareClaim(ctx, "1", f.Alice, f.Alice); !errors.Is(e, ErrUnauthorized) {
		t.Fatal(e)
	}
	p, e = c.Preview(ctx, "1", f.Alice)
	if e != nil || p.Owner != strings.ToLower(f.Bob) || p.HistoricalCreator != f.Alice {
		t.Fatalf("transfer %+v %v", p, e)
	}
	failed := send(t, r, f.Alice, FeesRouter, router, "claimFees", big.NewInt(1))
	if receiptStatus(t, r, failed) != 0 {
		t.Fatal("old owner claim succeeded")
	}
	if _, e = l.RecordCreatorClaim(ctx, c, failed, f.Pool, f.Alice); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	p, e = c.Preview(ctx, "1", f.Alice)
	if e != nil || p.ClaimableRaw != "10000000" {
		t.Fatalf("failed receipt changed liability %+v %v", p, e)
	}
	h := mustSend(t, r, f.Bob, FeesRouter, router, "claimFees", big.NewInt(1))
	ev, e := l.RecordCreatorClaim(ctx, c, h, f.Pool, f.Bob)
	if e != nil || ev.Raw != "10000000" {
		t.Fatalf("claim %+v %v", ev, e)
	}
	if _, e = l.RecordCreatorClaim(ctx, c, h, f.Pool, f.Bob); e != nil {
		t.Fatal(e)
	}
	if got := query(t, l, "SELECT count(*)::text FROM l18_claims"); got != "1" {
		t.Fatal(got)
	}
	p, e = c.Preview(ctx, "1", f.Alice)
	if e != nil || p.ClaimableRaw != "0" {
		t.Fatalf("post claim %+v %v", p, e)
	}
	t.Logf("actual fee receipt %s, raw %s, canonical %s", ev.TxHash, ev.Raw, ev.Block.Hash)
	// EOA pool has no checkpoint authority: unsupported strategy never prepares.
	p, e = c.Preview(ctx, "2", f.Alice)
	if e != nil {
		t.Fatal(e)
	}
	if StrategyCapability(2, false).Available {
		t.Fatal("unsupported snapshot")
	}
	bad := send(t, r, f.Alice, FeesRouter, router, "setFeeStrategy", big.NewInt(2), uint8(2))
	if receiptStatus(t, r, bad) != 0 {
		t.Fatal("unsupported AIRDROP activated")
	}
	mustSend(t, r, f.Bob, FeesRouter, router, "setFeeStrategy", big.NewInt(1), uint8(2))
	createEpoch := func(raw int64) string {
		mustSend(t, r, f.Admin, USDL, mintABI, "mint", common.HexToAddress(f.Pool), big.NewInt(raw*100))
		mustSend(t, r, f.Admin, f.Pool, swapABI, "swap", big.NewInt(raw*100), big.NewInt(0), true, common.HexToAddress("0x000000000000000000000000000000000000dead"), big.NewInt(2000000000))
		h := mustSend(t, r, f.Bob, FeesRouter, router, "executeAirdrop", big.NewInt(1))
		ep, e := l.RegisterEpoch(ctx, c, h, f.Pool)
		if e != nil || ep.FundedRaw != big.NewInt(raw).String() {
			t.Fatalf("epoch %+v %v", ep, e)
		}
		t.Logf("actual epoch %+v", ep)
		return ep.ID
	}
	ep1 := createEpoch(10)
	ep2 := createEpoch(7)
	if ep1 != "1" || ep2 != "2" {
		t.Fatal(ep1, ep2)
	}
	holderCall, e := l.PrepareEpochClaim(ctx, c, "1", "1", f.Alice)
	if e != nil || holderCall.Entitlement.ClaimableRaw != "6" || holderCall.ChainID != 125 || holderCall.To != FeesRouter {
		t.Fatalf("non-NFT holder call %+v %v", holderCall, e)
	}
	ent, e := l.EpochEntitlement(ctx, c, f.Pool, "1", f.Alice)
	if e != nil || ent.ClaimableRaw != "6" || ent.SnapshotBlock == 0 {
		t.Fatalf("entitlement %+v %v", ent, e)
	}
	h = mustSend(t, r, f.Alice, FeesRouter, router, "claimAirdropEpoch", big.NewInt(1), big.NewInt(1))
	ec, e := l.RecordEpochClaim(ctx, c, h, f.Pool, f.Alice, "1")
	if e != nil || ec.Raw != "6" {
		t.Fatalf("holder epoch %+v %v", ec, e)
	}
	if _, e = l.RecordEpochClaim(ctx, c, h, f.Pool, f.Alice, "1"); e != nil {
		t.Fatal(e)
	}
	ent, e = l.EpochEntitlement(ctx, c, f.Pool, "1", f.Bob)
	if e != nil || ent.ClaimableRaw != "4" {
		t.Fatalf("residue entitlement %+v %v", ent, e)
	}
	h = mustSend(t, r, f.Bob, FeesRouter, router, "claimAirdropEpoch", big.NewInt(1), big.NewInt(1))
	ec, e = l.RecordEpochClaim(ctx, c, h, f.Pool, f.Bob, "1")
	if e != nil || ec.Raw != "4" {
		t.Fatalf("residue %+v %v", ec, e)
	}
	ent, e = l.EpochEntitlement(ctx, c, f.Pool, "1", f.Bob)
	if e != nil || ent.ClaimableRaw != "0" || !ent.AlreadyClaimed || ent.ClaimedRaw != "10" {
		t.Fatalf("post-claim entitlement %+v %v", ent, e)
	}
	repeat := send(t, r, f.Bob, FeesRouter, router, "claimAirdropEpoch", big.NewInt(1), big.NewInt(1))
	if receiptStatus(t, r, repeat) != 0 {
		t.Fatal("epoch double claim")
	}
	if _, e = l.RecordEpochClaim(ctx, c, repeat, f.Pool, f.Bob, "1"); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	if got := query(t, l, "SELECT claimed::text FROM l18_epochs WHERE epoch=1"); got != "10" {
		t.Fatal(got)
	}
	if got := query(t, l, "SELECT claimed::text FROM l18_epochs WHERE epoch=2"); got != "0" {
		t.Fatal(got)
	}
	t.Logf("actual PostgreSQL epoch1 claimed=10 epoch2 claimed=0; outbox=%s", query(t, l, "SELECT jsonb_agg(payload)::text FROM l18_outbox"))
	// Canceled reads cannot produce prepared calls or settlement evidence.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = c.PrepareClaim(canceled, "1", f.Bob, f.Alice); e == nil {
		t.Fatal("ignored cancellation")
	}
}

// HTTP fixture transport avoids a new websocket dependency in the shared module.
type fixtureRPC struct {
	url    string
	client *http.Client
}

func newFixtureRPC(url string) (*fixtureRPC, error) {
	return &fixtureRPC{url, &http.Client{Timeout: 10 * time.Second}}, nil
}
func (r *fixtureRPC) CallContext(ctx context.Context, result any, method string, args ...any) error {
	if method == "eth_sendTransaction" || method == "eth_sendRawTransaction" || method == "evm_snapshot" || method == "evm_revert" || strings.HasPrefix(method, "anvil_") {
		if e := assertFixtureOwner(); e != nil {
			return e
		}
	}
	body, e := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": args})
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", r.url, bytes.NewReader(body))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := r.client.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	var v struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if e = json.NewDecoder(resp.Body).Decode(&v); e != nil {
		return e
	}
	if v.Error != nil {
		return fmt.Errorf("rpc %d: %s", v.Error.Code, v.Error.Message)
	}
	return json.Unmarshal(v.Result, result)
}

// Guard each actual fixture mutation against child death, PID reuse or a peer
// taking over the port. This belongs only to the private chain test transport.
func assertFixtureOwner() error {
	var id struct {
		PID   int    `json:"pid"`
		Start string `json:"starttime"`
	}
	if e := json.Unmarshal([]byte(os.Getenv("L18_ANVIL_IDENTITY")), &id); e != nil || id.PID <= 0 {
		return errors.New("owned Anvil identity absent")
	}
	base := "/proc/" + strconv.Itoa(id.PID)
	raw, e := os.ReadFile(base + "/stat")
	if e != nil {
		return e
	}
	end := strings.LastIndex(string(raw), ")")
	if end < 0 {
		return errors.New("invalid process identity")
	}
	fields := strings.Fields(string(raw)[end+1:])
	if len(fields) <= 19 || fields[0] == "Z" || fields[19] != id.Start {
		return errors.New("owned Anvil identity changed")
	}
	fds, e := os.ReadDir(base + "/fd")
	if e != nil {
		return e
	}
	inodes := map[string]bool{}
	for _, fd := range fds {
		target, e := os.Readlink(filepath.Join(base, "fd", fd.Name()))
		if e == nil && strings.HasPrefix(target, "socket:[") {
			inodes[strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")] = true
		}
	}
	tcp, e := os.ReadFile("/proc/net/tcp")
	if e != nil {
		return e
	}
	for _, line := range strings.Split(string(tcp), "\n") {
		f := strings.Fields(line)
		if len(f) > 9 && f[1] == "0100007F:5528" && f[3] == "0A" && inodes[f[9]] {
			return nil
		}
	}
	return errors.New("owned Anvil no longer owns endpoint")
}

func TestPhase3ActualReceiptReplayAndNodeDisagreement(t *testing.T) {
	path := os.Getenv("L18_CHAIN_FIXTURE")
	if path == "" {
		t.Fatal("owned real Anvil fixture required")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var f struct{ RPC, Pool, Token, Admin, Alice, Bob string }
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	if f.RPC != "http://127.0.0.1:21800" {
		t.Fatal("outside owned fixture")
	}
	r, e := newFixtureRPC(f.RPC)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	var initial string
	if e = r.CallContext(ctx, &initial, "evm_snapshot"); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		var reverted bool
		if err := r.CallContext(context.Background(), &reverted, "evm_revert", initial); err != nil || !reverted {
			t.Errorf("fixture restoration=%t error=%v", reverted, err)
		}
	})
	c := NewCreator(r, time.Now)
	preview, e := c.Preview(ctx, "1", f.Alice)
	if e != nil {
		t.Fatal(e)
	}
	owner := preview.Owner
	mustSend(t, r, owner, FeesRouter, router, "setFeeStrategy", big.NewInt(1), uint8(0))
	mustSend(t, r, f.Admin, USDL, mintABI, "mint", common.HexToAddress(f.Pool), big.NewInt(100000000))
	mustSend(t, r, f.Admin, f.Pool, swapABI, "swap", big.NewInt(100000000), big.NewInt(0), true, common.HexToAddress("0x000000000000000000000000000000000000dead"), big.NewInt(2000000000))
	preview, e = c.Preview(ctx, "1", f.Alice)
	if e != nil {
		t.Fatal(e)
	}
	before := preview.ClaimableRaw
	if before == "0" {
		t.Fatal("real accumulation missing")
	}
	old, e := c.checkpoint(ctx)
	if e != nil {
		t.Fatal(e)
	}
	paid := mustSend(t, r, owner, FeesRouter, router, "claimFees", big.NewInt(1))
	l := phase2Storage(t)
	first, e := l.RecordCreatorClaim(ctx, c, paid, f.Pool, owner)
	if e != nil || first.Raw != before {
		t.Fatalf("paid=%+v error=%v", first, e)
	}
	restarted := NewCreator(r, time.Now)
	second, e := l.RecordCreatorClaim(ctx, restarted, paid, f.Pool, owner)
	if e != nil || second != first {
		t.Fatalf("replayed=%+v error=%v", second, e)
	}
	if query(t, l, "SELECT count(*)::text FROM l18_claims") != "1" || query(t, l, "SELECT count(*)::text FROM l18_outbox WHERE topic='fee.claimed'") != "1" {
		t.Fatal("receipt replay double credited or published")
	}
	older := *c
	older.committed = func(context.Context) (Checkpoint, error) { return old, nil }
	if _, e = older.ClaimReceipt(ctx, paid, f.Pool, owner); e == nil {
		t.Fatal("uncommitted receipt accepted")
	}
	var branch string
	if e = r.CallContext(ctx, &branch, "evm_snapshot"); e != nil {
		t.Fatal(e)
	}
	mustSend(t, r, f.Admin, USDL, mintABI, "mint", common.HexToAddress(f.Admin), big.NewInt(1))
	committed, e := c.checkpoint(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var reverted bool
	if e = r.CallContext(ctx, &reverted, "evm_revert", branch); e != nil || !reverted {
		t.Fatalf("branch=%t error=%v", reverted, e)
	}
	mustSend(t, r, f.Admin, USDL, mintABI, "mint", common.HexToAddress(f.Admin), big.NewInt(2))
	conflicting := *c
	conflicting.committed = func(context.Context) (Checkpoint, error) { return committed, nil }
	if _, e = conflicting.PrepareClaim(ctx, "1", owner, f.Alice); !errors.Is(e, ErrUnavailable) {
		t.Fatal("node disagreed with committed hash but preparation continued", e)
	}
	if query(t, l, "SELECT count(*)::text FROM l18_claims") != "1" {
		t.Fatal("node disagreement changed persisted settlement")
	}
	t.Logf("receipt=%s amount=%s block=%s outbox=%s conflicting_committed_hash=%s", first.TxHash, first.Raw, first.Block.Hash, query(t, l, "SELECT payload::text FROM l18_outbox WHERE topic='fee.claimed'"), committed.Hash)
}
