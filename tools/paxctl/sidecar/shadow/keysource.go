package shadow

import (
	"bytes"
	"context"
	"fmt"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

// TraceKeySource derives a block's touched accounts and storage slots from a
// diff-mode prestate trace (debug_traceBlockByNumber, prestateTracer with
// diffMode) on an EVM JSON-RPC endpoint. diffMode reports both the pre-state
// (slots read) and post-state (slots written), so the union covers keys the
// block read OR modified — not just those read. Requires the debug_ namespace
// enabled on the endpoint (a non-public, operator-owned node).
//
// Coverage boundary: this is the per-block TOUCHED set. Keys migrated but never
// touched by any transaction (cold state), and non-EVM Cosmos-module state, are
// not covered here — that breadth is the corpus harness's job (Arm A) plus a
// periodic StaticKeySource sweep. Layer 2 over a trace source is a hot-state
// sampling oracle, not a full-keyspace check.
type TraceKeySource struct {
	client *gethrpc.Client
}

// NewTraceKeySource dials the EVM JSON-RPC endpoint used to fetch prestate
// traces. The same touched set applies to both chains (identical transactions),
// so a single endpoint (typically the canonical node) is sufficient.
func NewTraceKeySource(evmRPC string) (*TraceKeySource, error) {
	c, err := gethrpc.Dial(evmRPC)
	if err != nil {
		return nil, fmt.Errorf("dialing EVM RPC %q: %w", evmRPC, err)
	}
	return &TraceKeySource{client: c}, nil
}

// Close releases the underlying RPC connection. No return value, matching
// go-ethereum's *ethclient.Client / *rpc.Client Close() so Comparator.Close can
// treat all closeables uniformly.
func (t *TraceKeySource) Close() {
	if t.client != nil {
		t.client.Close()
	}
}

// prestateAccount is the subset of the prestateTracer's per-account output the
// key source needs: which slots were accessed, and whether the account carries
// code (a contract).
type prestateAccount struct {
	Storage map[common.Hash]common.Hash `json:"storage"`
	Code    hexutil.Bytes               `json:"code"`
}

// prestateResult is one transaction's diff-mode prestate output: pre-state
// (read) and post-state (written) per account.
type prestateResult struct {
	Pre  map[common.Address]prestateAccount `json:"pre"`
	Post map[common.Address]prestateAccount `json:"post"`
}

// prestateTxTrace wraps one transaction's tracer result.
type prestateTxTrace struct {
	Result prestateResult `json:"result"`
}

func (t *TraceKeySource) TouchedAccounts(ctx context.Context, height int64) ([]TouchedAccount, error) {
	var traces []prestateTxTrace
	blockArg := hexutil.EncodeUint64(uint64(height))
	cfg := map[string]any{"tracer": "prestateTracer", "tracerConfig": map[string]any{"diffMode": true}}
	if err := t.client.CallContext(ctx, &traces, "debug_traceBlockByNumber", blockArg, cfg); err != nil {
		return nil, fmt.Errorf("debug_traceBlockByNumber at height %d: %w", height, err)
	}
	return mergePrestateTraces(traces), nil
}

// mergePrestateTraces unions the per-transaction diff-mode results into one
// touched-account set per address: slots unioned across pre and post (reads and
// writes), code checked when either side shows code, balance and nonce checked
// for every touched account. Output is sorted (accounts by address, slots by
// hash) so the same block yields a byte-reproducible report.
func mergePrestateTraces(traces []prestateTxTrace) []TouchedAccount {
	slots := map[common.Address]map[common.Hash]struct{}{}
	hasCode := map[common.Address]bool{}

	absorb := func(accts map[common.Address]prestateAccount) {
		for addr, acct := range accts {
			if slots[addr] == nil {
				slots[addr] = map[common.Hash]struct{}{}
			}
			for slot := range acct.Storage {
				slots[addr][slot] = struct{}{}
			}
			if len(acct.Code) > 0 {
				hasCode[addr] = true
			}
		}
	}
	for _, tx := range traces {
		absorb(tx.Result.Pre)
		absorb(tx.Result.Post)
	}

	out := make([]TouchedAccount, 0, len(slots))
	for addr, slotSet := range slots {
		ta := TouchedAccount{Addr: addr, CheckCode: hasCode[addr], CheckNonce: true, CheckBalance: true}
		for slot := range slotSet {
			ta.Slots = append(ta.Slots, slot)
		}
		sort.Slice(ta.Slots, func(i, j int) bool { return bytes.Compare(ta.Slots[i][:], ta.Slots[j][:]) < 0 })
		out = append(out, ta)
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i].Addr[:], out[j].Addr[:]) < 0 })
	return out
}

// StaticKeySource compares a fixed, curated set of accounts on every block — the
// fallback when prestate tracing (debug_) is unavailable, and the hook for a
// periodic cold-key / hot-contract sweep that the trace source does not cover.
type StaticKeySource struct {
	Accounts []TouchedAccount
}

func (s StaticKeySource) TouchedAccounts(_ context.Context, _ int64) ([]TouchedAccount, error) {
	return s.Accounts, nil
}
