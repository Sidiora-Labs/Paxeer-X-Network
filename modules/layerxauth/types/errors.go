package types

import (
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

var (
	ErrAuthScope         = sdkerrors.Register("layerxauth", 2, "LXP_AUTH_SCOPE")
	ErrAuthRevoked       = sdkerrors.Register("layerxauth", 3, "LXP_AUTH_REVOKED")
	ErrAuthExpired       = sdkerrors.Register("layerxauth", 4, "LXP_AUTH_EXPIRED")
	ErrAuthNotMature     = sdkerrors.Register("layerxauth", 5, "LXP_AUTH_NOT_MATURE")
	ErrNotYetValid       = sdkerrors.Register("layerxauth", 6, "LXP_NOT_YET_VALID")
	ErrSequenceReused    = sdkerrors.Register("layerxauth", 7, "LXP_SEQUENCE_REUSED")
	ErrNonCanonical      = sdkerrors.Register("layerxauth", 8, "LXP_NON_CANONICAL")
	ErrUnauthorizedDebit = sdkerrors.Register("layerxauth", 9, "LXP_UNAUTHORIZED_DEBIT")
	ErrIdempotentReplay  = sdkerrors.Register("layerxauth", 10, "LXP_IDEMPOTENT_REPLAY")
	ErrPausedScope       = sdkerrors.Register("layerxauth", 11, "LXP_PAUSED_SCOPE")
)
