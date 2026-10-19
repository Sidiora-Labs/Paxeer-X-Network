// Package tasks — gov-vote handler.
//
// This handler signs votes as the validator's operator account.
// API authentication is controlled by SEI_SIDECAR_AUTHN_MODE; see
// sidecar/server/auth.go for the deployment guidance.

package tasks

import (
	"context"
	"errors"
	"fmt"

	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"

	"github.com/sei-protocol/seictl/sidecar/engine"
	"github.com/sei-protocol/seictl/sidecar/wire"
	"github.com/sei-protocol/seilog"
)

var govVoteLog = seilog.NewLogger("seictl", "task", "gov-vote")

// GovVoteRequest holds gov-vote params. Idempotency is handled by the
// chain — last-write-wins on (proposalID, voter); no pre-broadcast
// chain query.
type GovVoteRequest struct {
	ChainID    string `json:"chainId"`
	KeyName    string `json:"keyName"`
	ProposalID uint64 `json:"proposalId"`
	Option     string `json:"option"` // yes | no | abstain | no_with_veto
	Memo       string `json:"memo,omitempty"`
	Fees       string `json:"fees"`
	Gas        uint64 `json:"gas"`
}

// GovVoter captures cfg by value at construction; engine.Config is
// documented read-only after startup, so the copy is safe.
type GovVoter struct {
	cfg engine.ExecutionConfig
}

func NewGovVoter(cfg engine.ExecutionConfig) *GovVoter {
	return &GovVoter{cfg: cfg}
}

// Handler delegates to SignAndBroadcast after MsgVote construction.
//
// Rehydration: a crash after BroadcastSync but before result persist
// re-runs this handler. The rehydrated run signs at sequence+1 and
// broadcasts a second tx; chain last-write-wins on (proposalID, voter)
// keeps governance state correct and the operator pays fees twice.
// Safe ONLY because MsgVote is chain-idempotent — non-idempotent Msg
// types (MsgSend, MsgWithdraw…) would double-spend. Future sign-tx
// handlers must evaluate per-Msg idempotency before reusing this shape.
//
// Stale proposals are rejected by CheckTx and surface as Terminal. We
// do not pre-check via chain query — that opens a TOCTOU window.
func (g *GovVoter) Handler() engine.TaskHandler {
	return engine.TypedHandlerWithResult(func(ctx context.Context, params GovVoteRequest) (*wire.GovTxResult, error) {
		msg, err := buildVoteMsg(g.cfg, params)
		if err != nil {
			return nil, err
		}
		result, err := SignAndBroadcast(ctx, g.cfg, SignAndBroadcastInput{
			ChainID: params.ChainID,
			KeyName: params.KeyName,
			Msg:     msg,
			Fees:    params.Fees,
			Gas:     params.Gas,
			Memo:    params.Memo,
			TaskID:  engine.TaskIDFromContext(ctx),
		})
		if err != nil {
			return nil, err
		}
		out, cerr := classifyGovResult(engine.TaskGovVote, result)
		govVoteLog.Info("vote broadcast",
			"taskId", engine.TaskIDFromContext(ctx),
			"chainId", params.ChainID,
			"proposalId", params.ProposalID,
			"option", params.Option,
			"txHash", out.TxHash,
			"height", out.Height,
			"inclusionStatus", out.InclusionStatus)
		return out, cerr
	})
}

func buildVoteMsg(cfg engine.ExecutionConfig, params GovVoteRequest) (*govtypes.MsgVote, error) {
	if params.ProposalID == 0 {
		return nil, Terminal(errors.New("proposalId required (must be > 0)"))
	}
	option, err := wire.ParseVoteOption(params.Option)
	if err != nil {
		return nil, Terminal(err)
	}
	if cfg.Keyring == nil {
		return nil, Terminal(errors.New("keyring not configured: set SEI_KEYRING_BACKEND/SEI_KEYRING_PASSPHRASE on the sidecar"))
	}
	if params.KeyName == "" {
		return nil, Terminal(errors.New("keyName required"))
	}
	info, err := cfg.Keyring.Key(params.KeyName)
	if err != nil {
		return nil, Terminal(fmt.Errorf("keyring entry %q: %w", params.KeyName, err))
	}
	return govtypes.NewMsgVote(info.GetAddress(), params.ProposalID, govtypes.VoteOption(option)), nil
}
