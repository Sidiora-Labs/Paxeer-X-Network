package types

import (
	"errors"

	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

const (
	CodeInvalidParams   = "LXP_EXCHANGE_INVALID_PARAMS"
	CodeInvalidMarket   = "LXP_EXCHANGE_INVALID_MARKET"
	CodeUnknownMarket   = "LXP_EXCHANGE_UNKNOWN_MARKET"
	CodeNotMarginAsset  = "LXP_EXCHANGE_NOT_MARGIN_ASSET"
	CodeInvalidIntent   = "LXP_EXCHANGE_INVALID_INTENT"
	CodeIntentExists    = "LXP_EXCHANGE_INTENT_EXISTS"
	CodeNotFinalized    = "LXP_EXCHANGE_NOT_FINALIZED"
	CodeInvalidProof    = "LXP_EXCHANGE_INVALID_PROOF"
	CodeStateMismatch   = "LXP_EXCHANGE_STATE_MISMATCH"
	CodeInvalidGenesis  = "LXP_EXCHANGE_INVALID_GENESIS"
	CodeKeeperMissing   = "LXP_EXCHANGE_KEEPER_MISSING"
	CodeEvidenceTooLong = "LXP_EXCHANGE_EVIDENCE_TOO_LONG"
)

var (
	ErrInvalidParams   = sdkerrors.Register(ModuleName, 2, "invalid params")
	ErrInvalidMarket   = sdkerrors.Register(ModuleName, 3, "invalid market")
	ErrUnknownMarket   = sdkerrors.Register(ModuleName, 4, "market is not listed or is disabled")
	ErrNotMarginAsset  = sdkerrors.Register(ModuleName, 5, "asset is not the margin asset of an enabled market")
	ErrInvalidIntent   = sdkerrors.Register(ModuleName, 6, "invalid exchange intent")
	ErrIntentExists    = sdkerrors.Register(ModuleName, 7, "intent already recorded")
	ErrNotFinalized    = sdkerrors.Register(ModuleName, 8, "batch is not a finalized checkpoint")
	ErrInvalidProof    = sdkerrors.Register(ModuleName, 9, "LayerX state proof refused")
	ErrStateMismatch   = sdkerrors.Register(ModuleName, 10, "state proof does not prove the requested entry")
	ErrInvalidGenesis  = sdkerrors.Register(ModuleName, 11, "invalid genesis")
	ErrKeeperMissing   = sdkerrors.Register(ModuleName, 12, "layerxexchange keeper is not wired")
	ErrEvidenceTooLong = sdkerrors.Register(ModuleName, 13, "state proof exceeds the exchange bound")
)

func ErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidParams):
		return CodeInvalidParams
	case errors.Is(err, ErrInvalidMarket):
		return CodeInvalidMarket
	case errors.Is(err, ErrUnknownMarket):
		return CodeUnknownMarket
	case errors.Is(err, ErrNotMarginAsset):
		return CodeNotMarginAsset
	case errors.Is(err, ErrInvalidIntent):
		return CodeInvalidIntent
	case errors.Is(err, ErrIntentExists):
		return CodeIntentExists
	case errors.Is(err, ErrNotFinalized):
		return CodeNotFinalized
	case errors.Is(err, ErrInvalidProof):
		return CodeInvalidProof
	case errors.Is(err, ErrStateMismatch):
		return CodeStateMismatch
	case errors.Is(err, ErrInvalidGenesis):
		return CodeInvalidGenesis
	case errors.Is(err, ErrKeeperMissing):
		return CodeKeeperMissing
	case errors.Is(err, ErrEvidenceTooLong):
		return CodeEvidenceTooLong
	default:
		return ""
	}
}
