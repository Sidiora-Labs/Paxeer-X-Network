// Package tasks — gov completion contract. Gov sign-tx handlers surface a
// structured GovTxResult (committed_ok / committed_failed / pending) so the
// controller isn't left inferring success from a bare "task Complete".
package tasks

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"

	"github.com/sei-protocol/seictl/sidecar/engine"
	"github.com/sei-protocol/seictl/sidecar/wire"
	"github.com/sei-protocol/seilog"
)

var govResultLog = seilog.NewLogger("seictl", "task", "gov-result")

// submitProposalMsgType is the message type URL baseapp stamps on a
// MsgSubmitProposal's TxMsgData entry — guards parseProposalID against
// decoding a different message's response (e.g. a vote's) as a
// proposal-submit result.
var submitProposalMsgType = sdk.MsgTypeURL(&govtypes.MsgSubmitProposal{})

var txBroadcastTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "seictl_tx_broadcast_total",
		Help: "Gov sign-tx broadcasts by task type and inclusion outcome.",
	},
	[]string{"type", "outcome"},
)

func init() { prometheus.MustRegister(txBroadcastTotal) }

// classifyGovResult maps a broadcast result to the gov completion contract and
// records the outcome metric. Returns (result, err):
//   - committed_ok  (included, code 0) → (result, nil)         → task Completed
//   - committed_failed (included, code≠0) → (result, Terminal) → task Failed, terminal
//   - pending (inclusion undetermined) → (result, non-terminal) → task Failed;
//     the controller re-submits (same task ID → re-run → marker re-check)
func classifyGovResult(taskType engine.TaskType, r *SignAndBroadcastResult) (*wire.GovTxResult, error) {
	out := &wire.GovTxResult{
		TxHash:     r.TxHash,
		Height:     r.Height,
		ProposalID: r.ProposalID,
		Code:       r.Code,
		Codespace:  r.Codespace,
		RawLog:     r.RawLog,
	}
	switch {
	case r.Unverifiable:
		// Broadcast accepted but the node's tx index is off, so the outcome is
		// unobservable. Terminal (retrying this node is futile) but NOT
		// committed_failed — the operator must verify via an indexed RPC.
		out.InclusionStatus = wire.InclusionUnverifiable
		txBroadcastTotal.WithLabelValues(string(taskType), wire.InclusionUnverifiable).Inc()
		return out, Terminal(fmt.Errorf("tx %s inclusion unverifiable: %w", r.TxHash, errTxIndexingDisabled))
	case r.IncludedAt == nil:
		out.InclusionStatus = wire.InclusionPending
		txBroadcastTotal.WithLabelValues(string(taskType), wire.InclusionPending).Inc()
		return out, fmt.Errorf("tx %s inclusion undetermined; re-check pending", r.TxHash)
	case r.Code != 0:
		out.InclusionStatus = wire.InclusionCommittedFailed
		txBroadcastTotal.WithLabelValues(string(taskType), wire.InclusionCommittedFailed).Inc()
		return out, Terminal(fmt.Errorf("tx %s committed but failed: code=%d codespace=%q log=%s",
			r.TxHash, r.Code, r.Codespace, r.RawLog))
	default:
		out.InclusionStatus = wire.InclusionCommittedOK
		txBroadcastTotal.WithLabelValues(string(taskType), wire.InclusionCommittedOK).Inc()
		return out, nil
	}
}

// parseProposalID extracts the minted proposal ID from a committed
// MsgSubmitProposal tx's result data, or 0 if none is present: a
// not-yet-included tx (empty result data), a non-submit-proposal task such
// as a vote (index 0 is a different message's response), or a malformed
// result. SignAndBroadcastInput carries exactly one Msg per tx (never
// batched), so a MsgSubmitProposal response is always index 0 of the
// baseapp-encoded TxMsgData when present, regardless of the proposal's
// Content (software-upgrade or param-change) — the MsgType check guards
// that assumption rather than trusting the index blindly.
// This fork's gov keeper emits the proposal ID on the proposal_deposit
// event, not submit_proposal, so the tx's own result data is the one place
// the ID is unconditionally present.
func parseProposalID(resp *sdk.TxResponse) uint64 {
	var txMsgData sdk.TxMsgData
	if err := txMsgData.Unmarshal([]byte(resp.Data)); err != nil {
		govResultLog.Warn("decode tx result data", "txHash", resp.TxHash, "height", resp.Height, "err", err)
		return 0
	}
	if len(txMsgData.Data) == 0 {
		return 0 // not-yet-included (CheckTx-only response) or a non-Msg-service tx
	}
	if txMsgData.Data[0].MsgType != submitProposalMsgType {
		return 0 // a different message's response (e.g. a vote) — not ours to parse
	}
	var msgResp govtypes.MsgSubmitProposalResponse
	if err := msgResp.Unmarshal(txMsgData.Data[0].Data); err != nil {
		govResultLog.Warn("decode MsgSubmitProposalResponse", "txHash", resp.TxHash, "height", resp.Height, "err", err)
		return 0
	}
	return msgResp.ProposalId
}

// requireProposalID escalates a committed-ok gov result with no minted
// proposal ID to a Terminal error. A committed MsgSubmitProposal tx always
// mints an ID >= 1 (DefaultStartingProposalID); a 0 there is parseProposalID
// failing to decode it, not a legitimate outcome, and must not silently
// latch the task Complete. Callers whose result never carries a proposal ID
// (gov-vote) must not call this — cerr is returned unchanged for any
// inclusion status other than committed-ok, so it composes safely with the
// pending/committed-failed paths classifyGovResult already produces.
func requireProposalID(out *wire.GovTxResult, cerr error) error {
	if cerr == nil && out.InclusionStatus == wire.InclusionCommittedOK && out.ProposalID == 0 {
		return Terminal(fmt.Errorf("tx %s committed but minted no proposal ID (parse failure)", out.TxHash))
	}
	return cerr
}
