package tasks

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/hd"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keyring"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	txtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/tx"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	rpctypes "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/jsonrpc/types"

	"github.com/sei-protocol/seictl/sidecar/engine"
	"github.com/sei-protocol/seictl/sidecar/rpc"
)

// fakeTxClient is the in-memory txClient used by tests.
type fakeTxClient struct {
	mu sync.Mutex

	accountNumber uint64
	sequence      uint64
	accountErr    error

	broadcastResp *sdk.TxResponse
	broadcastErr  error
	broadcasts    int
	lastTxBytes   []byte

	// queryDefault returns canned data for any hash (mirroring production
	// QueryTx). Nil returns not-found.
	queryDefault *sdk.TxResponse
	queryErr     error
	queryCalls   int

	// queryFoundAfter makes the first N QueryTx calls return not-found;
	// calls after that return queryDefault (found). Zero preserves the
	// default behavior. Used to model a committed-but-unindexed tx that
	// the /tx index only surfaces on a later re-query.
	queryFoundAfter int
}

func (f *fakeTxClient) AccountNumberSequence(_ context.Context, _ sdk.AccAddress) (uint64, uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.accountNumber, f.sequence, f.accountErr
}

func (f *fakeTxClient) BroadcastSync(_ context.Context, txBytes []byte) (*sdk.TxResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.broadcasts++
	f.lastTxBytes = append(f.lastTxBytes[:0], txBytes...)
	if f.broadcastErr != nil {
		return nil, f.broadcastErr
	}
	return f.broadcastResp, nil
}

func (f *fakeTxClient) QueryTx(_ context.Context, hash string) (*sdk.TxResponse, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queryCalls++
	if f.queryErr != nil {
		return nil, false, f.queryErr
	}
	if f.queryCalls <= f.queryFoundAfter {
		return nil, false, nil
	}
	if f.queryDefault != nil {
		resp := *f.queryDefault
		resp.TxHash = hash
		return &resp, true, nil
	}
	return nil, false, nil
}

// fakeCheckpointer is an in-memory engine.Checkpointer test double.
type fakeCheckpointer struct {
	mu      sync.Mutex
	markers map[string]*engine.TxMarker
	saveErr error
	saves   int

	// tc lets SaveTxMarker record whether a broadcast had already happened
	// at save time, so tests can assert the marker is persisted BEFORE the
	// broadcast side effect. saveBroadcasts is tc.broadcasts captured on the
	// most recent SaveTxMarker call.
	tc             *fakeTxClient
	saveBroadcasts int
}

func newFakeCheckpointer(tc *fakeTxClient) *fakeCheckpointer {
	return &fakeCheckpointer{markers: map[string]*engine.TxMarker{}, tc: tc}
}

func (f *fakeCheckpointer) SaveTxMarker(m *engine.TxMarker) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves++
	if f.tc != nil {
		f.tc.mu.Lock()
		f.saveBroadcasts = f.tc.broadcasts
		f.tc.mu.Unlock()
	}
	if f.saveErr != nil {
		return f.saveErr
	}
	cp := *m
	f.markers[m.TaskID] = &cp
	return nil
}

func (f *fakeCheckpointer) GetTxMarker(taskID string) (*engine.TxMarker, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.markers[taskID]
	if !ok {
		return nil, nil
	}
	return m, nil
}

// testKeyring returns a memory keyring with one entry under "node_admin".
func testKeyring(t *testing.T) (keyring.Keyring, sdk.AccAddress) {
	t.Helper()
	ensureBech32()
	kb, err := keyring.New("seictl-tests", keyring.BackendMemory, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	info, _, err := kb.NewMnemonic("node_admin", keyring.English, sdk.GetConfig().GetFullBIP44Path(), "", hd.Secp256k1)
	if err != nil {
		t.Fatalf("NewMnemonic: %v", err)
	}
	return kb, info.GetAddress()
}

func fakeStatusServer(t *testing.T, network string, respCode int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			http.NotFound(w, r)
			return
		}
		if respCode != 0 {
			w.WriteHeader(respCode)
			return
		}
		// Seid's flat response shape (no JSON-RPC envelope).
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"node_info":{"id":"abc","network":"` + network + `"},"sync_info":{"latest_block_height":"100","catching_up":false}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func makeMsgVote(t *testing.T, voter sdk.AccAddress) sdk.Msg {
	t.Helper()
	return govtypes.NewMsgVote(voter, 7, govtypes.OptionYes)
}

func newGuardCfg(t *testing.T, chainID string) (engine.ExecutionConfig, sdk.AccAddress) {
	t.Helper()
	t.Setenv("SEI_CHAIN_ID", chainID)
	srv := fakeStatusServer(t, chainID, 0)
	kb, addr := testKeyring(t)
	return engine.ExecutionConfig{
		Keyring: kb,
		RPC:     rpc.NewClient(srv.URL, nil),
	}, addr
}

// --- Tests -----------------------------------------------------------

func TestChainConfusionGuard_EnvMismatch(t *testing.T) {
	cfg, addr := newGuardCfg(t, "pacific-1")
	tc := &fakeTxClient{}

	_, err := signAndBroadcast(context.Background(), cfg, tc, SignAndBroadcastInput{
		ChainID: "wrong-chain",
		KeyName: "node_admin",
		Msg:     makeMsgVote(t, addr),
		Fees:    "4000usei",
		Gas:     200_000,
		TaskID:  "00000000-0000-0000-0000-000000000001",
	}, addr)
	if !IsTerminal(err) {
		t.Fatalf("want Terminal env-mismatch error, got %v", err)
	}
	if !strings.Contains(err.Error(), "SEI_CHAIN_ID") {
		t.Fatalf("error should reference SEI_CHAIN_ID, got %q", err.Error())
	}
	if tc.broadcasts != 0 {
		t.Fatalf("broadcast must not run on guard failure; saw %d", tc.broadcasts)
	}
}

func TestChainConfusionGuard_StatusMismatch(t *testing.T) {
	t.Setenv("SEI_CHAIN_ID", "pacific-1")
	srv := fakeStatusServer(t, "atlantic-2", 0)
	kb, addr := testKeyring(t)
	cfg := engine.ExecutionConfig{
		Keyring: kb,
		RPC:     rpc.NewClient(srv.URL, nil),
	}

	_, err := signAndBroadcast(context.Background(), cfg, &fakeTxClient{}, SignAndBroadcastInput{
		ChainID: "pacific-1",
		KeyName: "node_admin",
		Msg:     makeMsgVote(t, addr),
		Fees:    "4000usei",
		Gas:     200_000,
		TaskID:  "00000000-0000-0000-0000-000000000002",
	}, addr)
	if !IsTerminal(err) {
		t.Fatalf("want Terminal status-mismatch error, got %v", err)
	}
	if !strings.Contains(err.Error(), "node.network") {
		t.Fatalf("error should reference node.network, got %q", err.Error())
	}
}

func TestChainConfusionGuard_MissingEnv(t *testing.T) {
	// t.Setenv("","") auto-restores after the test, unlike os.Unsetenv.
	t.Setenv("SEI_CHAIN_ID", "")
	srv := fakeStatusServer(t, "pacific-1", 0)
	kb, addr := testKeyring(t)
	cfg := engine.ExecutionConfig{
		Keyring: kb,
		RPC:     rpc.NewClient(srv.URL, nil),
	}

	_, err := signAndBroadcast(context.Background(), cfg, &fakeTxClient{}, SignAndBroadcastInput{
		ChainID: "pacific-1",
		KeyName: "node_admin",
		Msg:     makeMsgVote(t, addr),
		Fees:    "4000usei",
		Gas:     200_000,
		TaskID:  "00000000-0000-0000-0000-000000000003",
	}, addr)
	if !IsTerminal(err) {
		t.Fatalf("want Terminal env-not-set error, got %v", err)
	}
}

func TestFeesDenomGuard_RejectsNonUSei(t *testing.T) {
	cfg, addr := newGuardCfg(t, "pacific-1")

	_, err := signAndBroadcast(context.Background(), cfg, &fakeTxClient{}, SignAndBroadcastInput{
		ChainID: "pacific-1",
		KeyName: "node_admin",
		Msg:     makeMsgVote(t, addr),
		Fees:    "20sei", // the seienv vote.go:15 latent bug
		Gas:     200_000,
		TaskID:  "00000000-0000-0000-0000-000000000004",
	}, addr)
	if !IsTerminal(err) {
		t.Fatalf("want Terminal denom error, got %v", err)
	}
	if !strings.Contains(err.Error(), "usei") {
		t.Fatalf("error should mention usei, got %q", err.Error())
	}
}

func TestMissingKeyring_ReturnsTerminal(t *testing.T) {
	t.Setenv("SEI_CHAIN_ID", "pacific-1")
	srv := fakeStatusServer(t, "pacific-1", 0)
	cfg := engine.ExecutionConfig{
		Keyring: nil, // sidecar started without SEI_KEYRING_BACKEND
		RPC:     rpc.NewClient(srv.URL, nil),
	}

	_, err := SignAndBroadcast(context.Background(), cfg, SignAndBroadcastInput{
		ChainID: "pacific-1",
		KeyName: "node_admin",
		Msg:     govtypes.NewMsgVote(makeAddr(t), 7, govtypes.OptionYes),
		Fees:    "4000usei",
		Gas:     200_000,
		TaskID:  "00000000-0000-0000-0000-000000000005",
	})
	if !IsTerminal(err) {
		t.Fatalf("want Terminal missing-keyring error, got %v", err)
	}
}

func TestMissingKeyEntry_ReturnsTerminal(t *testing.T) {
	cfg, _ := newGuardCfg(t, "pacific-1")

	_, err := SignAndBroadcast(context.Background(), cfg, SignAndBroadcastInput{
		ChainID: "pacific-1",
		KeyName: "ghost", // not in the keyring
		Msg:     govtypes.NewMsgVote(makeAddr(t), 7, govtypes.OptionYes),
		Fees:    "4000usei",
		Gas:     200_000,
		TaskID:  "00000000-0000-0000-0000-000000000006",
	})
	if !IsTerminal(err) {
		t.Fatalf("want Terminal missing-key error, got %v", err)
	}
}

func TestAccountRetrieverFailure_Propagates(t *testing.T) {
	// Account-retrieve failure is NOT Terminal — may be a transient seid restart.
	cfg, addr := newGuardCfg(t, "pacific-1")
	tc := &fakeTxClient{accountErr: errors.New("rpc dial: connection refused")}

	_, err := signAndBroadcast(context.Background(), cfg, tc, SignAndBroadcastInput{
		ChainID: "pacific-1",
		KeyName: "node_admin",
		Msg:     makeMsgVote(t, addr),
		Fees:    "4000usei",
		Gas:     200_000,
		TaskID:  "00000000-0000-0000-0000-000000000007",
	}, addr)
	if err == nil {
		t.Fatal("expected error from account retrieve")
	}
	if IsTerminal(err) {
		t.Fatalf("account-retrieve transport errors should be retryable; got Terminal %v", err)
	}
	if !strings.Contains(err.Error(), "account retrieve") {
		t.Fatalf("error should mention account retrieve, got %q", err.Error())
	}
}

// TestSignedTxHasNoFeePayerOrGranter locks the signer-pays-its-own-fees
// invariant at the signed-bytes level. Without it a future contributor
// wiring WithFeePayer slips past the denom whitelist — the whitelist
// proves what denom is used, not who's paying.
func TestSignedTxHasNoFeePayerOrGranter(t *testing.T) {
	cfg, addr := newGuardCfg(t, "pacific-1")
	tc := &fakeTxClient{
		accountNumber: 17,
		sequence:      42,
		broadcastResp: &sdk.TxResponse{Code: 0, TxHash: "h", Height: 0},
		// Return inclusion on first poll so the test doesn't wait
		// the full inclusionPollTimeout.
		queryDefault: &sdk.TxResponse{Code: 0, Height: 7},
	}

	_, err := signAndBroadcast(context.Background(), cfg, tc, SignAndBroadcastInput{
		ChainID: "pacific-1",
		KeyName: "node_admin",
		Msg:     makeMsgVote(t, addr),
		Fees:    "4000usei",
		Gas:     200_000,
		TaskID:  "00000000-0000-0000-0000-0000000000fe",
	}, addr)
	if err != nil {
		t.Fatalf("signAndBroadcast: %v", err)
	}
	if len(tc.lastTxBytes) == 0 {
		t.Fatal("no tx bytes captured")
	}

	// Assert raw proto fields, not sdk.FeeTx.FeePayer() — the interface
	// method falls back to GetSigners()[0] when Fee.Payer is empty, so
	// a future WithFeePayer(signer) plumbing would silently pass.
	var pbTx txtypes.Tx
	if err := pbTx.Unmarshal(tc.lastTxBytes); err != nil {
		t.Fatalf("proto unmarshal tx: %v", err)
	}
	if pbTx.AuthInfo == nil || pbTx.AuthInfo.Fee == nil {
		t.Fatalf("decoded tx missing AuthInfo.Fee")
	}
	if pbTx.AuthInfo.Fee.Payer != "" {
		t.Errorf("AuthInfo.Fee.Payer = %q, want empty", pbTx.AuthInfo.Fee.Payer)
	}
	if pbTx.AuthInfo.Fee.Granter != "" {
		t.Errorf("AuthInfo.Fee.Granter = %q, want empty", pbTx.AuthInfo.Fee.Granter)
	}
}

func TestBroadcastCheckTxFailure_ReturnsTerminal(t *testing.T) {
	cfg, addr := newGuardCfg(t, "pacific-1")
	tc := &fakeTxClient{
		accountNumber: 17,
		sequence:      42,
		broadcastResp: &sdk.TxResponse{
			Code:      11,
			Codespace: "sdk",
			RawLog:    "insufficient fees",
		},
	}

	_, err := signAndBroadcast(context.Background(), cfg, tc, SignAndBroadcastInput{
		ChainID: "pacific-1",
		KeyName: "node_admin",
		Msg:     makeMsgVote(t, addr),
		Fees:    "4000usei",
		Gas:     200_000,
		TaskID:  "00000000-0000-0000-0000-000000000008",
	}, addr)
	if !IsTerminal(err) {
		t.Fatalf("want Terminal checkTx error, got %v", err)
	}
	if !strings.Contains(err.Error(), "checkTx") {
		t.Fatalf("error should mention checkTx, got %q", err.Error())
	}
	if tc.broadcasts != 1 {
		t.Fatalf("expected 1 broadcast, got %d", tc.broadcasts)
	}
}

func TestInclusionPollingCtxCancel_ReturnsCtxErr(t *testing.T) {
	// ctx cancellation must surface so the engine records the task failed
	// (rather than synthesizing "completed" on a truncated run).
	cfg, addr := newGuardCfg(t, "pacific-1")
	tc := &fakeTxClient{
		accountNumber: 17,
		sequence:      42,
		broadcastResp: &sdk.TxResponse{Code: 0, TxHash: "h", Height: 0},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := signAndBroadcast(ctx, cfg, tc, SignAndBroadcastInput{
		ChainID: "pacific-1",
		KeyName: "node_admin",
		Msg:     makeMsgVote(t, addr),
		Fees:    "4000usei",
		Gas:     200_000,
		TaskID:  "00000000-0000-0000-0000-00000000000c",
	}, addr)
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected ctx-cancellation error, got: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("test ran too long: %v (polling should bail on ctx.Done)", time.Since(start))
	}
}

func TestInclusionPollingDeadline_ReturnsNonTerminalWithNilIncludedAt(t *testing.T) {
	// Poll deadline (no ctx cancel) is "broadcast OK, inclusion deferred":
	// non-Terminal success with IncludedAt=nil.
	cfg, addr := newGuardCfg(t, "pacific-1")
	tc := &fakeTxClient{
		accountNumber: 17,
		sequence:      42,
		broadcastResp: &sdk.TxResponse{Code: 0, TxHash: "h", Height: 0},
	}

	prevTimeout, prevInterval := inclusionPollTimeout, inclusionPollInterval
	inclusionPollTimeout = 50 * time.Millisecond
	inclusionPollInterval = 10 * time.Millisecond
	t.Cleanup(func() {
		inclusionPollTimeout = prevTimeout
		inclusionPollInterval = prevInterval
	})

	res, err := signAndBroadcast(context.Background(), cfg, tc, SignAndBroadcastInput{
		ChainID: "pacific-1",
		KeyName: "node_admin",
		Msg:     makeMsgVote(t, addr),
		Fees:    "4000usei",
		Gas:     200_000,
		TaskID:  "00000000-0000-0000-0000-00000000000d",
	}, addr)
	if err != nil {
		t.Fatalf("poll-deadline (no ctx cancel) should NOT be an error: %v", err)
	}
	if res.IncludedAt != nil {
		t.Fatal("IncludedAt must be nil when poll deadline elapsed without inclusion")
	}
}

func TestAppendTaskIDToMemo(t *testing.T) {
	const id = "00000000-0000-0000-0000-000000000001"
	cases := []struct {
		base string
		want string
	}{
		{"", "taskID=" + id},
		{"vote-rationale", "vote-rationale taskID=" + id},
	}
	for _, c := range cases {
		if got := appendTaskIDToMemo(c.base, id); got != c.want {
			t.Fatalf("appendTaskIDToMemo(%q,_) = %q, want %q", c.base, got, c.want)
		}
	}
	// Empty taskID leaves base unchanged.
	if got := appendTaskIDToMemo("base", ""); got != "base" {
		t.Fatalf("empty taskID changed memo: %q", got)
	}
}

func TestCallerSuppliedTaskIDInMemoRejected(t *testing.T) {
	// Caller cannot smuggle a "taskID=" tag into the memo — the audit
	// trail must contain exactly the engine-appended tag.
	cfg, addr := newGuardCfg(t, "pacific-1")
	tc := &fakeTxClient{accountNumber: 17, sequence: 42}

	_, err := signAndBroadcast(context.Background(), cfg, tc, SignAndBroadcastInput{
		ChainID: "pacific-1",
		KeyName: "node_admin",
		Msg:     makeMsgVote(t, addr),
		Fees:    "4000usei",
		Gas:     200_000,
		Memo:    "rationale taskID=forged-uuid",
		TaskID:  "00000000-0000-0000-0000-0000000000aa",
	}, addr)
	if !IsTerminal(err) || !strings.Contains(err.Error(), "taskID=") {
		t.Fatalf("want Terminal taskID-prefix rejection, got %v", err)
	}
}

func TestMemoCapEnforcedAfterTaskIDAppend(t *testing.T) {
	// Caller's base is under 256 bytes but base + taskID exceeds the cap.
	cfg, addr := newGuardCfg(t, "pacific-1")
	tc := &fakeTxClient{accountNumber: 17, sequence: 42}

	base := strings.Repeat("a", 240) // leaves 16 bytes; "taskID=<uuid>" is 7+36=43
	_, err := signAndBroadcast(context.Background(), cfg, tc, SignAndBroadcastInput{
		ChainID: "pacific-1",
		KeyName: "node_admin",
		Msg:     makeMsgVote(t, addr),
		Fees:    "4000usei",
		Gas:     200_000,
		Memo:    base,
		TaskID:  "00000000-0000-0000-0000-000000000099",
	}, addr)
	if !IsTerminal(err) || !strings.Contains(err.Error(), "memo length") {
		t.Fatalf("want Terminal memo-length error, got %v", err)
	}
}

// --- Idempotency marker / adopt tests --------------------------------

// shortenPolls shrinks the poll/re-query timeouts so marker tests that
// exercise the not-found paths stay fast, restoring them after the test.
func shortenPolls(t *testing.T) {
	t.Helper()
	pt, pi, aq := inclusionPollTimeout, inclusionPollInterval, adoptReQueryTimeout
	inclusionPollTimeout = 50 * time.Millisecond
	inclusionPollInterval = 5 * time.Millisecond
	adoptReQueryTimeout = 50 * time.Millisecond
	t.Cleanup(func() {
		inclusionPollTimeout = pt
		inclusionPollInterval = pi
		adoptReQueryTimeout = aq
	})
}

// markerCfg wires a checkpointer-backed ExecutionConfig around the given fake.
func markerCfg(t *testing.T, chainID string, tc *fakeTxClient) (engine.ExecutionConfig, sdk.AccAddress, *fakeCheckpointer) {
	t.Helper()
	cfg, addr := newGuardCfg(t, chainID)
	cp := newFakeCheckpointer(tc)
	cfg.Checkpointer = cp
	return cfg, addr, cp
}

func markerInput(t *testing.T, addr sdk.AccAddress) SignAndBroadcastInput {
	t.Helper()
	return SignAndBroadcastInput{
		ChainID: "pacific-1",
		KeyName: "node_admin",
		Msg:     makeMsgVote(t, addr),
		Fees:    "4000usei",
		Gas:     200_000,
		TaskID:  "task-1",
	}
}

// Test 1: adopt with the tx already on chain — build the result from the
// chain, never re-broadcast, never re-sign (broadcasts==0 is the proxy).
func TestAdopt_FoundOnChain_NoRebroadcast(t *testing.T) {
	shortenPolls(t)
	tc := &fakeTxClient{queryDefault: &sdk.TxResponse{Code: 0, Height: 9, TxHash: "seed"}}
	cfg, addr, cp := markerCfg(t, "pacific-1", tc)
	cp.markers["task-1"] = &engine.TxMarker{
		TaskID: "task-1", TxHash: "ABCD", TxBytes: []byte{1, 2, 3}, ChainID: "pacific-1",
	}

	res, err := signAndBroadcast(context.Background(), cfg, tc, markerInput(t, addr), addr)
	if err != nil {
		t.Fatalf("adopt-found should succeed: %v", err)
	}
	if res.Height != 9 {
		t.Fatalf("result should be built from chain (Height 9), got %d", res.Height)
	}
	if tc.broadcasts != 0 {
		t.Fatalf("adopt-found must not re-broadcast; saw %d", tc.broadcasts)
	}
	if tc.queryCalls < 1 {
		t.Fatalf("expected at least one QueryTx, got %d", tc.queryCalls)
	}
}

// Test 2: adopt not-found → re-broadcast the identical marker bytes.
func TestAdopt_NotFound_RebroadcastsIdenticalBytes(t *testing.T) {
	shortenPolls(t)
	tc := &fakeTxClient{
		queryDefault:  nil, // not found
		broadcastResp: &sdk.TxResponse{Code: 0, TxHash: "h"},
	}
	cfg, addr, cp := markerCfg(t, "pacific-1", tc)
	want := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02}
	cp.markers["task-1"] = &engine.TxMarker{
		TaskID: "task-1", TxHash: "ABCD", TxBytes: want,
		AccountNumber: 17, Sequence: 42, ChainID: "pacific-1",
	}

	_, err := signAndBroadcast(context.Background(), cfg, tc, markerInput(t, addr), addr)
	if err != nil {
		t.Fatalf("adopt not-found re-broadcast should succeed: %v", err)
	}
	if tc.broadcasts != 1 {
		t.Fatalf("expected exactly 1 re-broadcast, got %d", tc.broadcasts)
	}
	if string(tc.lastTxBytes) != string(want) {
		t.Fatalf("re-broadcast must use the marker's exact bytes; got %x want %x", tc.lastTxBytes, want)
	}
}

// Test 3 (M1/B1): adopt with a QueryTx transport error — report pending
// (inclusion-undetermined), never re-broadcast into uncertainty, never a bare
// error the controller can't distinguish from terminal.
func TestAdopt_QueryTransportError_ReportsPending_NoRebroadcast(t *testing.T) {
	shortenPolls(t)
	tc := &fakeTxClient{queryErr: errors.New("rpc down")}
	cfg, addr, cp := markerCfg(t, "pacific-1", tc)
	cp.markers["task-1"] = &engine.TxMarker{
		TaskID: "task-1", TxHash: "ABCD", TxBytes: []byte{9, 9, 9}, ChainID: "pacific-1",
	}

	res, err := signAndBroadcast(context.Background(), cfg, tc, markerInput(t, addr), addr)
	if err != nil {
		t.Fatalf("transport error should report pending, not error: %v", err)
	}
	if res == nil || res.IncludedAt != nil {
		t.Fatalf("expected an undetermined (pending) result, got %+v", res)
	}
	if res.TxHash != "ABCD" {
		t.Fatalf("pending result must carry the marker txHash, got %q", res.TxHash)
	}
	if tc.broadcasts != 0 {
		t.Fatalf("must NOT re-broadcast on unknown state; saw %d", tc.broadcasts)
	}
}

// txIndexDisabledErr mirrors the node's real /tx response when tx_index is off
// (tx_index.indexer = "null"): a JSON-RPC internal error whose Data names the
// missing kvEventSink. Observed on a validator with transaction indexing off.
func txIndexDisabledErr() *rpctypes.RPCError {
	return &rpctypes.RPCError{
		Code:    -32603,
		Message: "Internal error",
		Data:    "transaction querying is disabled due to no kvEventSink",
	}
}

// TestIsTxIndexingDisabled pins the match against sei-tendermint's real
// disabled-index payloads — a reword would otherwise silently revert the fix to
// the retry-until-timeout bug. Both event-sink spellings must classify as
// disabled (terminal); crucially a message that says "not found" AND names the
// sink must too, since QueryTx checks this before isTxNotFound. A genuine miss
// on an indexed node and a transport error must not.
func TestIsTxIndexingDisabled(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"pre-lookup guard (kvEventSink)", txIndexDisabledErr(), true},
		{"fallback guard (KV event sink)", &rpctypes.RPCError{Message: "Internal error", Data: "transaction querying is disabled on this node due to the KV event sink being disabled"}, true},
		{"not-found that also names the sink", &rpctypes.RPCError{Message: "Internal error", Data: "tx (ABCD) not found, err: no kvEventSink"}, true},
		{"genuine not-found on an indexed node", &rpctypes.RPCError{Message: "Internal error", Data: "tx (ABCD) not found, err: x"}, false},
		{"transport error", errors.New("connection refused"), false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isTxIndexingDisabled(c.err); got != c.want {
				t.Fatalf("isTxIndexingDisabled(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

// Adopt path: a marker exists but the node's tx index is off, so QueryTx yields
// errTxIndexingDisabled. signAndBroadcast must return an UNVERIFIABLE result —
// not a retryable pending one — without re-broadcasting. The pre-fix behavior
// reported inclusion-undetermined and retried to the task deadline, masking the
// outcome as a bare Timeout. (classifyGovResult turns the unverifiable result
// terminal — see gov_result_test.go.)
func TestAdopt_TxIndexingDisabled_Unverifiable(t *testing.T) {
	shortenPolls(t)
	tc := &fakeTxClient{queryErr: errTxIndexingDisabled}
	cfg, addr, cp := markerCfg(t, "pacific-1", tc)
	cp.markers["task-1"] = &engine.TxMarker{
		TaskID: "task-1", TxHash: "ABCD", TxBytes: []byte{9, 9, 9}, ChainID: "pacific-1",
	}

	res, err := signAndBroadcast(context.Background(), cfg, tc, markerInput(t, addr), addr)
	if err != nil {
		t.Fatalf("unverifiable is carried on the result, not returned as an error: %v", err)
	}
	if res == nil || !res.Unverifiable || res.IncludedAt != nil {
		t.Fatalf("expected an unverifiable result with IncludedAt nil, got %+v", res)
	}
	if res.TxHash != "ABCD" {
		t.Fatalf("unverifiable result must carry the marker txHash, got %q", res.TxHash)
	}
	if tc.broadcasts != 0 {
		t.Fatalf("must not re-broadcast when inclusion is unobservable; saw %d", tc.broadcasts)
	}
}

// Fresh path: no marker; broadcast accepted (CheckTx code 0) but the node's tx
// index is off, so inclusion can never be polled. Same unverifiable result —
// not pending-until-deadline.
func TestFresh_TxIndexingDisabled_Unverifiable(t *testing.T) {
	shortenPolls(t)
	tc := &fakeTxClient{
		broadcastResp: &sdk.TxResponse{Code: 0, TxHash: "h"},
		queryErr:      errTxIndexingDisabled,
	}
	cfg, addr, _ := markerCfg(t, "pacific-1", tc)

	res, err := signAndBroadcast(context.Background(), cfg, tc, markerInput(t, addr), addr)
	if err != nil {
		t.Fatalf("unverifiable is carried on the result, not an error: %v", err)
	}
	if res == nil || !res.Unverifiable || res.IncludedAt != nil {
		t.Fatalf("expected an unverifiable result, got %+v", res)
	}
}

// B1: a broadcast transport error on the fresh path reports pending (the marker
// is durable, the tx may be in flight), not a bare error.
func TestBroadcastTransportError_ReportsPending(t *testing.T) {
	shortenPolls(t)
	tc := &fakeTxClient{broadcastErr: errors.New("rpc down")}
	cfg, addr, _ := markerCfg(t, "pacific-1", tc)

	res, err := signAndBroadcast(context.Background(), cfg, tc, markerInput(t, addr), addr)
	if err != nil {
		t.Fatalf("broadcast transport error should report pending, not error: %v", err)
	}
	if res == nil || res.IncludedAt != nil || res.TxHash == "" {
		t.Fatalf("expected an undetermined (pending) result with a txHash, got %+v", res)
	}
}

// Test 4 (H1): adopt not-found → re-broadcast is CheckTx-rejected (seq
// mismatch → Terminal), but the re-query guard finds the tx actually
// landed → the guard rescues it into a success.
func TestAdopt_RebroadcastRejectedButLanded_RequeryGuardRescues(t *testing.T) {
	shortenPolls(t)
	tc := &fakeTxClient{
		queryFoundAfter: 1, // first QueryTx not-found, then found
		queryDefault:    &sdk.TxResponse{Code: 0, Height: 11, TxHash: "seed"},
		broadcastResp:   &sdk.TxResponse{Code: 32}, // sequence mismatch → Terminal
	}
	cfg, addr, cp := markerCfg(t, "pacific-1", tc)
	cp.markers["task-1"] = &engine.TxMarker{
		TaskID: "task-1", TxHash: "ABCD", TxBytes: []byte{1}, ChainID: "pacific-1",
	}

	res, err := signAndBroadcast(context.Background(), cfg, tc, markerInput(t, addr), addr)
	if err != nil {
		t.Fatalf("re-query guard should rescue a landed tx from a false Terminal fail: %v", err)
	}
	if res == nil || res.Height != 11 {
		t.Fatalf("result should be built from the re-queried chain tx (Height 11), got %+v", res)
	}
	if tc.broadcasts != 1 {
		t.Fatalf("expected exactly 1 re-broadcast, got %d", tc.broadcasts)
	}
}

// Test 5: fresh path persists the marker BEFORE broadcasting.
func TestFresh_MarkerPersistedBeforeBroadcast(t *testing.T) {
	shortenPolls(t)
	tc := &fakeTxClient{
		accountNumber: 17,
		sequence:      42,
		broadcastResp: &sdk.TxResponse{Code: 0, TxHash: "h"},
		queryDefault:  &sdk.TxResponse{Code: 0, Height: 7}, // inclusion on first poll
	}
	cfg, addr, cp := markerCfg(t, "pacific-1", tc)

	_, err := signAndBroadcast(context.Background(), cfg, tc, markerInput(t, addr), addr)
	if err != nil {
		t.Fatalf("fresh broadcast should succeed: %v", err)
	}
	if cp.saves != 1 {
		t.Fatalf("expected exactly 1 SaveTxMarker, got %d", cp.saves)
	}
	if cp.saveBroadcasts != 0 {
		t.Fatalf("marker must be saved before broadcast; broadcasts at save time = %d", cp.saveBroadcasts)
	}
	if tc.broadcasts != 1 {
		t.Fatalf("expected 1 broadcast after save, got %d", tc.broadcasts)
	}
	if _, ok := cp.markers["task-1"]; !ok {
		t.Fatal("marker for task-1 must exist in the checkpointer after broadcast")
	}
}

// Test 6: SaveTxMarker failure aborts before any broadcast.
func TestFresh_SaveMarkerFails_NoBroadcast(t *testing.T) {
	shortenPolls(t)
	tc := &fakeTxClient{
		accountNumber: 17,
		sequence:      42,
		broadcastResp: &sdk.TxResponse{Code: 0, TxHash: "h"},
	}
	cfg, addr, cp := markerCfg(t, "pacific-1", tc)
	cp.saveErr = errors.New("disk full")

	_, err := signAndBroadcast(context.Background(), cfg, tc, markerInput(t, addr), addr)
	if err == nil {
		t.Fatal("expected error when SaveTxMarker fails")
	}
	if tc.broadcasts != 0 {
		t.Fatalf("must never broadcast without a durable marker; saw %d", tc.broadcasts)
	}
}

// Test 7: no checkpointer configured (back-compat) — broadcasts once, no panic.
func TestNoCheckpointer_BroadcastsOnce(t *testing.T) {
	shortenPolls(t)
	cfg, addr := newGuardCfg(t, "pacific-1") // no Checkpointer
	tc := &fakeTxClient{
		accountNumber: 17,
		sequence:      42,
		broadcastResp: &sdk.TxResponse{Code: 0, TxHash: "h"},
		queryDefault:  &sdk.TxResponse{Code: 0, Height: 7},
	}

	res, err := signAndBroadcast(context.Background(), cfg, tc, markerInput(t, addr), addr)
	if err != nil {
		t.Fatalf("back-compat path should succeed: %v", err)
	}
	if res == nil {
		t.Fatal("expected a result")
	}
	if tc.broadcasts != 1 {
		t.Fatalf("expected exactly 1 broadcast, got %d", tc.broadcasts)
	}
}

// Test 8 (L1): the persisted marker's TxHash equals sha256 of the exact
// bytes that were broadcast.
func TestFresh_MarkerHashMatchesBroadcastBytes(t *testing.T) {
	shortenPolls(t)
	tc := &fakeTxClient{
		accountNumber: 17,
		sequence:      42,
		broadcastResp: &sdk.TxResponse{Code: 0, TxHash: "h"},
		queryDefault:  &sdk.TxResponse{Code: 0, Height: 7},
	}
	cfg, addr, cp := markerCfg(t, "pacific-1", tc)

	_, err := signAndBroadcast(context.Background(), cfg, tc, markerInput(t, addr), addr)
	if err != nil {
		t.Fatalf("fresh broadcast should succeed: %v", err)
	}
	m, ok := cp.markers["task-1"]
	if !ok {
		t.Fatal("marker for task-1 missing")
	}
	want := fmt.Sprintf("%X", sha256.Sum256(tc.lastTxBytes))
	if m.TxHash != want {
		t.Fatalf("marker TxHash %q != sha256 of broadcast bytes %q", m.TxHash, want)
	}
}

// makeAddr returns a syntactically valid sei-bech32 AccAddress without
// constructing a keyring entry. Used by guards that fail before any
// keyring lookup.
func makeAddr(t *testing.T) sdk.AccAddress {
	t.Helper()
	ensureBech32()
	return sdk.AccAddress([]byte("aaaaaaaaaaaaaaaaaaaa"))
}
