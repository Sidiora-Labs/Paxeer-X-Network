package tasks

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	rpchttp "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/http"
	rpctypes "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/jsonrpc/types"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	cryptocodec "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/codec"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtx "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/tx"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	proposal "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types/proposal"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"

	"github.com/sei-protocol/seictl/sidecar/engine"
	"github.com/sei-protocol/seictl/sidecar/rpc"
)

// tmRPCTimeout bounds the underlying HTTP client so a wedged seid does
// not pin goroutines/conns indefinitely (the ctx-cancel select in
// AccountNumberSequence returns the task but leaves the inflight call
// behind until transport completes).
const tmRPCTimeout = 30 * time.Second

// newSDKTxClient wires the production txClient against the local seid RPC.
func newSDKTxClient(cfg engine.ExecutionConfig, in SignAndBroadcastInput, fromAddr sdk.AccAddress) (txClient, error) {
	rpcURL := rpc.DefaultEndpoint
	if cfg.RPC != nil && cfg.RPC.Endpoint() != "" {
		rpcURL = cfg.RPC.Endpoint()
	}
	tmClient, err := rpchttp.NewWithTimeout(rpcURL, tmRPCTimeout)
	if err != nil {
		return nil, fmt.Errorf("tendermint RPC client at %s: %w", rpcURL, err)
	}
	registry, cdc, txCfg := makeSignTxCodec()
	clientCtx := client.Context{}.
		WithChainID(in.ChainID).
		WithCodec(cdc).
		WithInterfaceRegistry(registry).
		WithTxConfig(txCfg).
		WithKeyring(cfg.Keyring).
		WithClient(tmClient).
		WithFromAddress(fromAddr).
		WithFromName(in.KeyName).
		WithAccountRetriever(authtypes.AccountRetriever{}).
		WithBroadcastMode("sync")
	return &sdkTxClient{clientCtx: clientCtx}, nil
}

type sdkTxClient struct {
	clientCtx client.Context
}

func (c *sdkTxClient) AccountNumberSequence(ctx context.Context, fromAddr sdk.AccAddress) (uint64, uint64, error) {
	// AccountRetriever ignores context.Context; wrap in a goroutine so
	// a hung seid does not outlive the engine's cancellation.
	type res struct {
		accNum, seq uint64
		err         error
	}
	ch := make(chan res, 1)
	go func() {
		a, s, err := authtypes.AccountRetriever{}.GetAccountNumberSequence(c.clientCtx, fromAddr)
		ch <- res{a, s, err}
	}()
	select {
	case <-ctx.Done():
		return 0, 0, ctx.Err()
	case r := <-ch:
		return r.accNum, r.seq, r.err
	}
}

func (c *sdkTxClient) BroadcastSync(ctx context.Context, txBytes []byte) (*sdk.TxResponse, error) {
	// Bypass clientCtx.BroadcastTxSync (hardcodes context.Background) so
	// the engine's cancellation propagates into the broadcast HTTP call.
	node, err := c.clientCtx.GetNode()
	if err != nil {
		return nil, err
	}
	resp, err := node.BroadcastTxSync(ctx, txBytes)
	if err != nil {
		return nil, err
	}
	return sdk.NewResponseFormatBroadcastTx(resp), nil
}

func (c *sdkTxClient) QueryTx(ctx context.Context, txHashHex string) (*sdk.TxResponse, bool, error) {
	if c.clientCtx.Client == nil {
		return nil, false, errors.New("tx client has no Tendermint RPC client")
	}
	hashBytes, err := hex.DecodeString(txHashHex)
	if err != nil {
		return nil, false, fmt.Errorf("decode tx hash %q: %w", txHashHex, err)
	}
	// ctx is plumbed through to the underlying http.Request via
	// NewRequestWithContext in sei-tendermint's jsonrpc HTTP client
	// (sei-tendermint/rpc/jsonrpc/client/http_json_client.go) —
	// cancellation aborts the in-flight call, so the 30s tmRPCTimeout
	// is a redundant upper bound, not the only guardrail.
	res, err := c.clientCtx.Client.Tx(ctx, hashBytes, false)
	if err != nil {
		// Order matters: the indexing-disabled check runs first. sei-tendermint's
		// disabled-index guard fires before any lookup, but a message that names
		// the event sink AND says "not found" must classify as the terminal
		// disabled case, not the retryable not-found one. Normalize to the
		// sentinel here (the one layer that owns /tx semantics) so callers branch
		// on errors.Is rather than re-matching the message text.
		if isTxIndexingDisabled(err) {
			return nil, false, fmt.Errorf("query /tx?hash=%s: %w", txHashHex, errTxIndexingDisabled)
		}
		if isTxNotFound(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("query /tx?hash=%s: %w", txHashHex, err)
	}
	resp := &sdk.TxResponse{
		Height:    res.Height,
		TxHash:    txHashHex,
		Code:      res.TxResult.Code,
		Codespace: res.TxResult.Codespace,
		Data:      string(res.TxResult.Data),
		RawLog:    res.TxResult.Log,
		Info:      res.TxResult.Info,
		GasWanted: res.TxResult.GasWanted,
		GasUsed:   res.TxResult.GasUsed,
	}
	return resp, true, nil
}

// isTxNotFound discriminates "the node has no record of this tx" from
// every other failure mode of /tx?hash=. Server-side, all handler
// errors come back as JSON-RPC CodeInternalError (sei-tendermint's
// MakeError, types.go:171); the "tx not found" message lands in the
// Data field. Transport errors (DNS, connection-refused, timeout) are
// NOT *rpctypes.RPCError, so the type assertion fences them out — a
// DNS "host not found" cannot be confused with a missing tx.
func isTxNotFound(err error) bool {
	var rpcErr *rpctypes.RPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	return strings.Contains(rpcErr.Data, "not found")
}

// errTxIndexingDisabled is the sentinel QueryTx returns when the target node
// has transaction indexing turned off (tx_index.indexer = "null"), so /tx?hash=
// cannot answer. The tx may still have committed — this reports that inclusion
// is unobservable from this node, NOT that the tx failed. Retrying the same
// node is futile (unlike a transient transport error), so callers fail
// terminally; the message tells the operator how to unblock.
var errTxIndexingDisabled = errors.New(
	"node transaction indexing is disabled (no kvEventSink): the tx may already be on chain — " +
		"verify via an indexed RPC before re-running, or enable tx_index on the target node")

// isTxIndexingDisabled reports whether err is the node's indexing-disabled
// response. Same *rpctypes.RPCError fence as isTxNotFound (a transport error is
// not an RPCError, so it stays transient); like there, the detail lands in
// Data. sei-tendermint's /tx handler names the missing sink in two spellings —
// the pre-lookup guard ("...disabled due to no kvEventSink") and the fallback
// ("...KV event sink being disabled") — so match both, case-insensitively. A
// genuine miss on an index-ENABLED node is "tx (…) not found" carrying neither
// token, and stays retryable via isTxNotFound.
func isTxIndexingDisabled(err error) bool {
	var rpcErr *rpctypes.RPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	d := strings.ToLower(rpcErr.Data)
	return strings.Contains(d, "kveventsink") || strings.Contains(d, "kv event sink")
}

// newSignTxInterfaceRegistry registers only the proto interfaces sign-tx
// needs. Adding more pulls in transitive deps (notably x/wasm via x/evm)
// that break CGO_ENABLED=0 builds.
func newSignTxInterfaceRegistry() codectypes.InterfaceRegistry {
	registry := codectypes.NewInterfaceRegistry()
	cryptocodec.RegisterInterfaces(registry)
	authtypes.RegisterInterfaces(registry)
	banktypes.RegisterInterfaces(registry)
	stakingtypes.RegisterInterfaces(registry)
	govtypes.RegisterInterfaces(registry)
	upgradetypes.RegisterInterfaces(registry)
	// x/params ParameterChangeProposal as a gov Content impl (gov-param-change
	// task). proposal does not pull x/wasm, so CGO_ENABLED=0 builds stay clean.
	proposal.RegisterInterfaces(registry)
	return registry
}

// makeSignTxCodec is the single source of truth for the proto registry,
// codec, and TxConfig used by both production sdkTxClient wiring and the
// sign path. If interfaces ever diverge between the two, sign/encode
// breaks in confusing ways; sharing here is load-bearing.
func makeSignTxCodec() (codectypes.InterfaceRegistry, codec.Codec, client.TxConfig) {
	registry := newSignTxInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)
	txCfg := authtx.NewTxConfig(cdc, authtx.DefaultSignModes)
	return registry, cdc, txCfg
}
