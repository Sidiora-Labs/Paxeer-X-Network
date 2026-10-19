// Package tasks — gov-param-change handler.
//
// This handler signs ParameterChangeProposals as the validator's
// operator account. API authentication is controlled by
// SEI_SIDECAR_AUTHN_MODE; see sidecar/server/auth.go.
//
// REHYDRATION — sei-protocol/seictl#174
//
// MsgSubmitProposal is NOT chain-idempotent, and param-change has no "applies
// once" safety net (unlike gov-software-upgrade). Crash-idempotency is provided
// by the pre-broadcast TxMarker + rehydrate-adopt in SignAndBroadcast: a re-run
// adopts the in-flight tx rather than re-signing, so a crash no longer produces
// a duplicate proposal.

package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	proposal "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types/proposal"

	"github.com/sei-protocol/seictl/sidecar/engine"
	"github.com/sei-protocol/seictl/sidecar/wire"
	"github.com/sei-protocol/seilog"
)

var govParamChangeLog = seilog.NewLogger("seictl", "task", "gov-param-change")

// paramChange is one (subspace, key, value) entry. Value is raw JSON of
// whatever shape the param's registered type expects (scalar, string,
// bool, or object). It is stringified exactly ONCE — see buildParamChangeMsg.
//
// Integer-valued params MUST be passed as JSON strings (e.g. "100"), not
// bare numbers: the sidecar request decode routes values through a
// map[string]any (float64 numbers), which silently loses precision above
// 2^53. Sei's large-integer params (durations, windows) are string-encoded
// by convention, so this is the natural form anyway.
type paramChange struct {
	Subspace string          `json:"subspace"`
	Key      string          `json:"key"`
	Value    json.RawMessage `json:"value"`
}

// GovParamChangeRequest holds gov-param-change params. Idempotency is
// NOT handled — see the REHYDRATION WARNING at the top of this file.
type GovParamChangeRequest struct {
	ChainID string `json:"chainId"`
	KeyName string `json:"keyName"`

	Title       string `json:"title"`
	Description string `json:"description"`

	Changes []paramChange `json:"changes"`

	InitialDeposit string `json:"initialDeposit"`

	Memo string `json:"memo,omitempty"`
	Fees string `json:"fees"`
	Gas  uint64 `json:"gas"`
}

// GovParamChanger captures cfg by value at construction; engine.Config
// is documented read-only after startup, so the copy is safe.
type GovParamChanger struct {
	cfg engine.ExecutionConfig
}

func NewGovParamChanger(cfg engine.ExecutionConfig) *GovParamChanger {
	return &GovParamChanger{cfg: cfg}
}

// Handler delegates to SignAndBroadcast (which owns the crash-idempotency
// marker — see the REHYDRATION note at the top of this file) and classifies
// the outcome via classifyGovResult.
func (g *GovParamChanger) Handler() engine.TaskHandler {
	return engine.TypedHandlerWithResult(func(ctx context.Context, params GovParamChangeRequest) (*wire.GovTxResult, error) {
		msg, err := buildParamChangeMsg(g.cfg, params)
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
		out, cerr := classifyGovResult(engine.TaskGovParamChange, result)
		cerr = requireProposalID(out, cerr)
		keys := make([]string, 0, len(params.Changes))
		for _, c := range params.Changes {
			keys = append(keys, c.Subspace+"/"+c.Key)
		}
		govParamChangeLog.Info("proposal broadcast",
			"taskId", engine.TaskIDFromContext(ctx),
			"chainId", params.ChainID,
			"changes", keys,
			"txHash", out.TxHash,
			"height", out.Height,
			"proposalId", out.ProposalID,
			"inclusionStatus", out.InclusionStatus)
		return out, cerr
	})
}

func buildParamChangeMsg(cfg engine.ExecutionConfig, params GovParamChangeRequest) (*govtypes.MsgSubmitProposal, error) {
	if cfg.Keyring == nil {
		return nil, Terminal(errors.New("keyring not configured: set SEI_KEYRING_BACKEND/SEI_KEYRING_PASSPHRASE on the sidecar"))
	}
	if params.KeyName == "" {
		return nil, Terminal(errors.New("keyName required"))
	}
	if params.Title == "" {
		return nil, Terminal(errors.New("title required"))
	}
	if params.Description == "" {
		return nil, Terminal(errors.New("description required"))
	}
	if len(params.Changes) == 0 {
		return nil, Terminal(errors.New("at least one change required"))
	}
	changes := make([]proposal.ParamChange, 0, len(params.Changes))
	for i, c := range params.Changes {
		if c.Subspace == "" {
			return nil, Terminal(fmt.Errorf("changes[%d].subspace required", i))
		}
		if c.Key == "" {
			return nil, Terminal(fmt.Errorf("changes[%d].key required", i))
		}
		if len(c.Value) == 0 {
			return nil, Terminal(fmt.Errorf("changes[%d].value required", i))
		}
		// The ONLY string() conversion: c.Value is the raw JSON of the
		// param's registered type (scalar/string/bool/object). The chain
		// runs UnmarshalAsJSON(value, registeredType) at apply time, so
		// the bytes must be valid JSON for that type — NOT a re-escaped
		// string (which would double-encode and fail at apply).
		changes = append(changes, proposal.NewParamChange(c.Subspace, c.Key, string(c.Value)))
	}
	info, err := cfg.Keyring.Key(params.KeyName)
	if err != nil {
		return nil, Terminal(fmt.Errorf("keyring entry %q: %w", params.KeyName, err))
	}
	deposit, err := sdk.ParseCoinsNormalized(params.InitialDeposit)
	if err != nil {
		return nil, Terminal(fmt.Errorf("parse initialDeposit %q: %w", params.InitialDeposit, err))
	}
	if len(deposit) == 0 {
		return nil, Terminal(fmt.Errorf("initialDeposit %q resolves to zero coins", params.InitialDeposit))
	}
	if !deposit.IsAllPositive() {
		return nil, Terminal(fmt.Errorf("initialDeposit %q contains non-positive amounts", params.InitialDeposit))
	}
	// Symmetric with checkFeesDenom: deposit denom is fixed by gov params
	// on Sei (usei); a wrong denom would CheckTx-reject anyway, but
	// rejecting here saves the sign + broadcast roundtrip.
	for _, c := range deposit {
		if c.Denom != feeDenom {
			return nil, Terminal(fmt.Errorf("initialDeposit %q: denom %q not permitted (only %q)", params.InitialDeposit, c.Denom, feeDenom))
		}
	}
	// isExpedited=false: expedited is deferred (it is honored only via
	// NewMsgSubmitProposalWithExpedite, not the content field). See LLD.
	content := proposal.NewParameterChangeProposal(params.Title, params.Description, changes, false)
	msg, err := govtypes.NewMsgSubmitProposal(content, deposit, info.GetAddress())
	if err != nil {
		return nil, Terminal(fmt.Errorf("build MsgSubmitProposal: %w", err))
	}
	return msg, nil
}
