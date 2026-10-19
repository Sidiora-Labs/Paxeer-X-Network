// Package tasks — sign-tx family helper.
//
// SignAndBroadcast signs txs as the validator's operator account.
// API-level authentication is controlled by SEI_SIDECAR_AUTHN_MODE:
// trusted-header mode pairs the sidecar with a kube-rbac-proxy
// container that gates every request via TokenReview + SAR. When the
// env var is unset, the API is unauthenticated and any actor with
// network reach to the listen port can reach this code path.

package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client/tx"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"

	signingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"

	"github.com/sei-protocol/seictl/sidecar/engine"
	"github.com/sei-protocol/seictl/sidecar/rpc"
	"github.com/sei-protocol/seilog"
)

var signTxLog = seilog.NewLogger("seictl", "task", "sign-tx")

// feeDenom is the only fee denom the sidecar will sign for. seienv
// historically used "sei" (e.g., `--fees 20sei` in vote.go:15) which
// seid rejects at parse time — we make the rejection explicit and
// chain-confusion-proof. 1 SEI = 1_000_000 usei.
const feeDenom = "usei"

// Var (not const) so tests can shorten without a deadline context.
var (
	inclusionPollTimeout  = 60 * time.Second
	inclusionPollInterval = 500 * time.Millisecond
)

// SignAndBroadcastInput is the shared input contract for every sign-tx
// handler. Msg stays as sdk.Msg so the helper never grows a per-msg switch.
type SignAndBroadcastInput struct {
	ChainID string
	KeyName string
	Msg     sdk.Msg

	// Fees is a coin-string in usei. Non-usei denoms are rejected Terminal.
	Fees string

	Gas  uint64
	Memo string

	// TaskID is appended to the on-chain memo so operators can grep the
	// chain by task. See appendTaskIDToMemo.
	TaskID string
}

// SignAndBroadcastResult is the shared output contract. Sign-tx handlers
// extend it with type-specific fields after this function returns.
type SignAndBroadcastResult struct {
	TxHash        string    `json:"txHash"`
	Height        int64     `json:"height"`
	Code          uint32    `json:"code"`
	Codespace     string    `json:"codespace,omitempty"`
	RawLog        string    `json:"rawLog,omitempty"`
	GasWanted     int64     `json:"gasWanted"`
	GasUsed       int64     `json:"gasUsed"`
	Sequence      uint64    `json:"sequence"`
	AccountNumber uint64    `json:"accountNumber"`
	ChainID       string    `json:"chainId"`
	BroadcastedAt time.Time `json:"broadcastedAt"`
	// ProposalID is decoded from the committed tx's result data (see
	// parseProposalID); 0 for votes, non-gov txs, or a not-yet-included tx.
	ProposalID uint64 `json:"proposalId,omitempty"`
	// IncludedAt is nil when inclusion polling timed out after a
	// successful broadcast. nil means UNDETERMINED — the tx may still
	// land later. It does NOT mean "not included". Callers must
	// re-query the chain to determine final state.
	IncludedAt *time.Time `json:"includedAt,omitempty"`
	// Unverifiable is set when the tx was broadcast but its outcome cannot be
	// observed because the target node's tx index is off (errTxIndexingDisabled).
	// classifyGovResult turns this into a terminal InclusionUnverifiable — it is
	// distinct from IncludedAt==nil (pending/retryable): retrying is futile.
	Unverifiable bool `json:"unverifiable,omitempty"`
}

// TerminalError marks a sign-tx error as non-retryable (malformed input,
// chain-confusion, CheckTx rejection, missing key). The engine has no
// retry policy yet, but callers should not implement ad-hoc retry on top.
type TerminalError struct {
	Err error
}

func (e *TerminalError) Error() string { return e.Err.Error() }
func (e *TerminalError) Unwrap() error { return e.Err }

// Terminal wraps err as TerminalError, or returns nil when err is nil.
func Terminal(err error) error {
	if err == nil {
		return nil
	}
	return &TerminalError{Err: err}
}

// IsTerminal reports whether err (or any wrapped error) is a TerminalError.
func IsTerminal(err error) bool {
	var t *TerminalError
	return errors.As(err, &t)
}

// txClient is the narrow seam for everything not covered by local files
// or rpc.Client: account fetch, broadcast, tx-by-hash query. Kept local
// to sign-tx rather than on engine.ExecutionConfig.
type txClient interface {
	// AccountNumberSequence always refreshes — a stale sequence is the
	// most common CheckTx rejection and could mask a prior broadcast.
	AccountNumberSequence(ctx context.Context, fromAddr sdk.AccAddress) (uint64, uint64, error)

	// BroadcastSync returns (resp, err). resp.Code != 0 is a CheckTx
	// rejection and is treated as Terminal.
	BroadcastSync(ctx context.Context, txBytes []byte) (*sdk.TxResponse, error)

	// QueryTx returns (resp, found, err). found=false / err=nil means
	// the node has no record (distinct from a transport error). A node with
	// tx indexing disabled returns errTxIndexingDisabled (match via
	// errors.Is), since /tx cannot answer there.
	QueryTx(ctx context.Context, txHashHex string) (*sdk.TxResponse, bool, error)
}

// SignAndBroadcast is the entry point each sign-tx handler calls. It
// resolves the signer from the in-memory keyring, wires the production
// txClient, and delegates to signAndBroadcast for the full validate +
// guard + sign + broadcast + poll cycle.
func SignAndBroadcast(ctx context.Context, cfg engine.ExecutionConfig, in SignAndBroadcastInput) (*SignAndBroadcastResult, error) {
	if cfg.Keyring == nil {
		return nil, Terminal(errors.New("keyring not configured: set SEI_KEYRING_BACKEND/SEI_KEYRING_PASSPHRASE on the sidecar"))
	}
	info, err := cfg.Keyring.Key(in.KeyName)
	if err != nil {
		return nil, Terminal(fmt.Errorf("keyring entry %q: %w", in.KeyName, err))
	}
	fromAddr := info.GetAddress()

	tc, err := newSDKTxClient(cfg, in, fromAddr)
	if err != nil {
		return nil, err
	}
	return signAndBroadcast(ctx, cfg, tc, in, fromAddr)
}

// signAndBroadcast does the full sign+broadcast+poll cycle. Public callers
// reach this through SignAndBroadcast which wires the production txClient;
// tests call it directly with a fake.
func signAndBroadcast(ctx context.Context, cfg engine.ExecutionConfig, tc txClient, in SignAndBroadcastInput, fromAddr sdk.AccAddress) (*SignAndBroadcastResult, error) {
	// Reject caller-supplied "taskID=" before appending so on-chain
	// audit greps never see a forged tag preceding the genuine one.
	if strings.Contains(in.Memo, taskIDMemoPrefix) {
		return nil, Terminal(fmt.Errorf("memo must not contain %q (reserved for engine-supplied task tag)", taskIDMemoPrefix))
	}
	// Append taskID to memo so the byte cap is enforced against the
	// effective on-chain memo.
	in.Memo = appendTaskIDToMemo(in.Memo, in.TaskID)
	if err := validateInput(in); err != nil {
		return nil, Terminal(err)
	}
	if err := checkFeesDenom(in.Fees); err != nil {
		return nil, Terminal(err)
	}
	if err := guardChainID(ctx, cfg.RPC, in.ChainID); err != nil {
		return nil, err
	}

	// Adopt an in-flight tx from a prior crashed run instead of signing anew.
	if cfg.Checkpointer != nil && in.TaskID != "" {
		marker, err := cfg.Checkpointer.GetTxMarker(in.TaskID)
		if err != nil {
			return nil, fmt.Errorf("read tx marker: %w", err)
		}
		if marker != nil {
			signTxLog.Info("adopting in-flight tx from marker", "taskId", in.TaskID, "txHash", marker.TxHash)
			return adoptMarker(ctx, tc, marker)
		}
	}

	accNum, seq, err := tc.AccountNumberSequence(ctx, fromAddr)
	if err != nil {
		return nil, fmt.Errorf("account retrieve %s: %w", fromAddr.String(), err)
	}

	// WithFees panics on parse error; checkFeesDenom already ran.
	_, _, txCfg := makeSignTxCodec()
	factory := tx.Factory{}.
		WithChainID(in.ChainID).
		WithKeybase(cfg.Keyring).
		WithTxConfig(txCfg).
		WithAccountRetriever(authtypes.AccountRetriever{}).
		WithAccountNumber(accNum).
		WithSequence(seq).
		WithGas(in.Gas).
		WithFees(in.Fees).
		WithMemo(in.Memo).
		WithSignMode(signingtypes.SignMode_SIGN_MODE_DIRECT)

	builder, err := tx.BuildUnsignedTx(factory, in.Msg)
	if err != nil {
		return nil, Terminal(fmt.Errorf("build unsigned tx: %w", err))
	}
	if err := tx.Sign(factory, in.KeyName, builder, true); err != nil {
		return nil, fmt.Errorf("sign tx: %w", err)
	}
	txBytes, err := txCfg.TxEncoder()(builder.GetTx())
	if err != nil {
		return nil, fmt.Errorf("encode tx: %w", err)
	}

	txHash := fmt.Sprintf("%X", sha256.Sum256(txBytes))

	// Marker must be durable before broadcast so a crash re-adopts this tx.
	if cfg.Checkpointer != nil && in.TaskID != "" {
		if err := cfg.Checkpointer.SaveTxMarker(&engine.TxMarker{
			TaskID:        in.TaskID,
			TxHash:        txHash,
			TxBytes:       txBytes,
			AccountNumber: accNum,
			Sequence:      seq,
			ChainID:       in.ChainID,
		}); err != nil {
			return nil, fmt.Errorf("persist tx marker: %w", err)
		}
	} else {
		signTxLog.Warn("no checkpointer configured; broadcasting without crash-idempotency marker",
			"taskId", in.TaskID)
	}

	signTxLog.Info("broadcasting tx",
		"taskId", in.TaskID, "chainId", in.ChainID,
		"sequence", seq, "accountNumber", accNum, "gas", in.Gas, "fees", in.Fees)
	return broadcastAndPoll(ctx, tc, txBytes, txHash, accNum, seq, in.ChainID)
}

// adoptReQueryTimeout bounds the re-query after an adopt re-broadcast is
// CheckTx-rejected. Var so tests can shorten it.
var adoptReQueryTimeout = 5 * time.Second

// adoptMarker resumes a prior run's tx: already on chain → build from it; query
// error → report undetermined (retry, don't re-broadcast blind); not indexed →
// re-broadcast the identical bytes (never re-sign; CometBFT dedups by hash).
// The /tx index lags commit, so a CheckTx rejection on that re-broadcast likely
// means the tx already landed — re-query before failing.
func adoptMarker(ctx context.Context, tc txClient, m *engine.TxMarker) (*SignAndBroadcastResult, error) {
	resp, found, err := tc.QueryTx(ctx, m.TxHash)
	if err != nil {
		if errors.Is(err, errTxIndexingDisabled) {
			// Inclusion is unobservable on this node — retrying is futile.
			// Return an unverifiable result (classifyGovResult makes it
			// terminal), carrying the txHash for the operator's manual check.
			return unverifiableResult(m.TxHash, m.AccountNumber, m.Sequence, m.ChainID), nil
		}
		// Unknown state — report undetermined so the caller re-checks; the
		// durable marker makes a later re-broadcast safe.
		signTxLog.Warn("adopt query transport error; reporting inclusion-undetermined",
			"txHash", m.TxHash, "err", err)
		return &SignAndBroadcastResult{
			TxHash: m.TxHash, AccountNumber: m.AccountNumber, Sequence: m.Sequence,
			ChainID: m.ChainID, BroadcastedAt: time.Now().UTC(), IncludedAt: nil,
		}, nil
	}
	if found {
		now := time.Now().UTC()
		return resultFromTxResponse(resp, m.AccountNumber, m.Sequence, m.ChainID, now, &now), nil
	}

	out, berr := broadcastAndPoll(ctx, tc, m.TxBytes, m.TxHash, m.AccountNumber, m.Sequence, m.ChainID)
	if berr != nil && IsTerminal(berr) {
		if included, qerr := pollForInclusion(ctx, tc, m.TxHash, adoptReQueryTimeout, inclusionPollInterval); qerr == nil && included != nil {
			now := time.Now().UTC()
			return resultFromTxResponse(included, m.AccountNumber, m.Sequence, m.ChainID, now, &now), nil
		}
	}
	return out, berr
}

// broadcastAndPoll broadcasts signed tx bytes and polls for inclusion; shared
// by the fresh-sign and re-broadcast paths. A CheckTx rejection is terminal; a
// transport error or poll timeout returns an undetermined result (IncludedAt
// nil) so the caller classifies it as pending and re-checks — a bare error
// would look terminal and strand a possibly-live tx.
func broadcastAndPoll(ctx context.Context, tc txClient, txBytes []byte, txHash string, accNum, seq uint64, chainID string) (*SignAndBroadcastResult, error) {
	broadcastedAt := time.Now().UTC()
	undetermined := &SignAndBroadcastResult{
		TxHash: txHash, Sequence: seq, AccountNumber: accNum,
		ChainID: chainID, BroadcastedAt: broadcastedAt, IncludedAt: nil,
	}

	resp, err := tc.BroadcastSync(ctx, txBytes)
	if err != nil {
		signTxLog.Warn("broadcast transport error; reporting inclusion-undetermined",
			"txHash", txHash, "err", err)
		return undetermined, nil
	}
	if resp.Code != 0 {
		return nil, Terminal(fmt.Errorf("checkTx rejected: code=%d codespace=%q log=%s",
			resp.Code, resp.Codespace, resp.RawLog))
	}

	included, perr := pollForInclusion(ctx, tc, resp.TxHash, inclusionPollTimeout, inclusionPollInterval)
	if perr != nil {
		if errors.Is(perr, errTxIndexingDisabled) {
			// Inclusion is unobservable on this node — polling can never
			// confirm it. Return an unverifiable result (terminal), not
			// pending-until-deadline.
			return unverifiableResult(resp.TxHash, accNum, seq, chainID), nil
		}
		// ctx cancellation (shutdown) — propagate; a poll timeout is (nil,nil).
		return nil, perr
	}
	if included != nil {
		now := time.Now().UTC()
		return resultFromTxResponse(included, accNum, seq, chainID, broadcastedAt, &now), nil
	}
	return resultFromTxResponse(resp, accNum, seq, chainID, broadcastedAt, nil), nil
}

// taskIDMemoPrefix is the literal tag prefix written into the on-chain
// memo by appendTaskIDToMemo. Caller-supplied memos containing this
// substring are rejected so audit greps see exactly one tag per tx.
const taskIDMemoPrefix = "taskID="

// appendTaskIDToMemo tags the memo with the task ID so operators can grep
// the chain by memo. taskID="" leaves base unchanged.
func appendTaskIDToMemo(base, taskID string) string {
	if taskID == "" {
		return base
	}
	tag := taskIDMemoPrefix + taskID
	if base == "" {
		return tag
	}
	return base + " " + tag
}

func validateInput(in SignAndBroadcastInput) error {
	if in.ChainID == "" {
		return errors.New("chainId required")
	}
	if in.KeyName == "" {
		return errors.New("keyName required")
	}
	if in.Msg == nil {
		return errors.New("msg required")
	}
	if in.TaskID == "" {
		return errors.New("taskId required (engine should always populate this)")
	}
	if in.Gas == 0 {
		return errors.New("gas required (must be > 0)")
	}
	if in.Fees == "" {
		return errors.New("fees required")
	}
	if err := validateMemo(in.Memo); err != nil {
		return err
	}
	if err := in.Msg.ValidateBasic(); err != nil {
		return fmt.Errorf("msg.ValidateBasic: %w", err)
	}
	return nil
}

// maxMemoBytes matches Cosmos SDK's MaxMemoCharacters consensus cap.
const maxMemoBytes = 256

// validateMemo rejects oversize or non-printable memos. Memos surface
// in on-chain events and audit pipelines; control chars (tab, newline,
// ANSI) are known log/CSV injection vectors.
func validateMemo(memo string) error {
	if len(memo) > maxMemoBytes {
		return fmt.Errorf("memo length %d exceeds %d bytes", len(memo), maxMemoBytes)
	}
	for _, r := range memo {
		if !unicode.IsPrint(r) {
			return fmt.Errorf("memo contains non-printable character %U", r)
		}
	}
	return nil
}

// checkFeesDenom rejects any coin string whose denom is not "usei".
// Guards the seienv `--fees 20sei` latent bug before the SDK's
// Factory.WithFees panics deep inside.
func checkFeesDenom(fees string) error {
	coins, err := sdk.ParseCoinsNormalized(fees)
	if err != nil {
		return fmt.Errorf("parse fees %q: %w", fees, err)
	}
	// ParseCoinsNormalized filters zero-amount entries; "0usei" and
	// non-positive sums normalize to empty Coins.
	if len(coins) == 0 {
		return fmt.Errorf("fees %q resolves to zero or empty coins", fees)
	}
	if !coins.IsAllPositive() {
		return fmt.Errorf("fees %q contains non-positive amounts", fees)
	}
	for _, c := range coins {
		if c.Denom != feeDenom {
			return fmt.Errorf("fees %q: denom %q not permitted (only %q)", fees, c.Denom, feeDenom)
		}
	}
	return nil
}

// guardChainID rejects sign requests whose ChainID does not match BOTH
// SEI_CHAIN_ID AND the chain the local seid reports via /status.
func guardChainID(ctx context.Context, rpcClient *rpc.Client, chainID string) error {
	envChain := os.Getenv("SEI_CHAIN_ID")
	if envChain == "" {
		return Terminal(errors.New("SEI_CHAIN_ID not set on sidecar; refusing to sign"))
	}
	if chainID != envChain {
		return Terminal(fmt.Errorf("chain mismatch: params.chainId=%q sidecar.SEI_CHAIN_ID=%q", chainID, envChain))
	}
	if rpcClient == nil {
		return Terminal(errors.New("RPC client not configured; cannot verify chain identity via /status"))
	}
	raw, err := rpcClient.Get(ctx, "/status")
	if err != nil {
		// Transport failure — refuse to sign since we cannot prove chain identity.
		return fmt.Errorf("query local seid /status: %w", err)
	}
	var status rpc.StatusResult
	if err := json.Unmarshal(raw, &status); err != nil {
		return fmt.Errorf("decode /status: %w", err)
	}
	if status.NodeInfo.Network == "" {
		return Terminal(errors.New("/status.node_info.network is empty; refusing to sign"))
	}
	if status.NodeInfo.Network != chainID {
		return Terminal(fmt.Errorf("chain mismatch: params.chainId=%q node.network=%q", chainID, status.NodeInfo.Network))
	}
	return nil
}

// pollForInclusion polls /tx?hash=... until inclusion, ctx cancel, or
// timeout. Returns (resp, nil) on inclusion, (nil, nil) on timeout
// (caller treats as "broadcast OK, confirmation deferred"), and
// (nil, ctx.Err()) on cancellation.
func pollForInclusion(ctx context.Context, tc txClient, txHash string, timeout, interval time.Duration) (*sdk.TxResponse, error) {
	deadline := time.Now().Add(timeout)
	for {
		// Check cancellation + deadline before each network call so a
		// hung QueryTx transport does not blow past the bound.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, nil
		}
		resp, found, err := tc.QueryTx(ctx, txHash)
		if err == nil && found {
			return resp, nil
		}
		if errors.Is(err, errTxIndexingDisabled) {
			// Unobservable on this node — polling can never confirm inclusion,
			// so stop and surface it for the caller rather than spin until the
			// deadline.
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// unverifiableResult builds the result for a broadcast tx whose on-chain
// outcome cannot be observed because the target node's tx index is off.
// IncludedAt stays nil and Unverifiable is set, so classifyGovResult reports a
// terminal InclusionUnverifiable — distinct from the retryable pending case.
func unverifiableResult(txHash string, accNum, seq uint64, chainID string) *SignAndBroadcastResult {
	return &SignAndBroadcastResult{
		TxHash:        txHash,
		AccountNumber: accNum,
		Sequence:      seq,
		ChainID:       chainID,
		BroadcastedAt: time.Now().UTC(),
		IncludedAt:    nil,
		Unverifiable:  true,
	}
}

func resultFromTxResponse(resp *sdk.TxResponse, accNum, seq uint64, chainID string, broadcastedAt time.Time, includedAt *time.Time) *SignAndBroadcastResult {
	return &SignAndBroadcastResult{
		TxHash:        resp.TxHash,
		Height:        resp.Height,
		Code:          resp.Code,
		Codespace:     resp.Codespace,
		RawLog:        resp.RawLog,
		GasWanted:     resp.GasWanted,
		GasUsed:       resp.GasUsed,
		Sequence:      seq,
		AccountNumber: accNum,
		ChainID:       chainID,
		BroadcastedAt: broadcastedAt,
		IncludedAt:    includedAt,
		ProposalID:    parseProposalID(resp),
	}
}
