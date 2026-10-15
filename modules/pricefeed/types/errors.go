package types

import (
	"errors"

	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

const ModuleName = "pricefeed"

var (
	ErrUnauthorized        = sdkerrors.Register(ModuleName, 2, "unauthorized")
	ErrInvalidParams       = sdkerrors.Register(ModuleName, 3, "invalid params")
	ErrInvalidGenesis      = sdkerrors.Register(ModuleName, 4, "invalid genesis")
	ErrNotValidator        = sdkerrors.Register(ModuleName, 5, "not a bonded validator")
	ErrInvalidKey          = sdkerrors.Register(ModuleName, 6, "feed key must be a 33-byte compressed secp256k1 public key")
	ErrKeyInUse            = sdkerrors.Register(ModuleName, 7, "feed account is registered to another validator")
	ErrNoKey               = sdkerrors.Register(ModuleName, 8, "validator has no feed key")
	ErrUnknownMarket       = sdkerrors.Register(ModuleName, 9, "market is not listed")
	ErrUnregisteredFeeder  = sdkerrors.Register(ModuleName, 10, "feeder is not a registered feed account")
	ErrRateLimited         = sdkerrors.Register(ModuleName, 11, "the validator has already submitted prices at this height")
	ErrTooManyObservations = sdkerrors.Register(ModuleName, 12, "too many observations in one submission")
	ErrSubmissionTooLarge  = sdkerrors.Register(ModuleName, 13, "submission is over the byte bound")
	ErrDuplicateMarket     = sdkerrors.Register(ModuleName, 14, "a submission carries one observation per market")
	ErrSubmissionAlone     = sdkerrors.Register(ModuleName, 15, "price submissions cannot be in the same tx as other messages")
	ErrInvalidObservation  = sdkerrors.Register(ModuleName, 16, "invalid observation")
)

func LXPName(err error) (string, bool) {
	var registered *sdkerrors.Error
	if !errors.As(err, &registered) || registered == nil || registered.Codespace() != ModuleName {
		return "", false
	}
	switch registered.ABCICode() {
	case 2:
		return "LXP_PRICEFEED_UNAUTHORIZED", true
	case 3:
		return "LXP_PRICEFEED_INVALID_PARAMS", true
	case 4:
		return "LXP_PRICEFEED_INVALID_GENESIS", true
	case 5:
		return "LXP_PRICEFEED_NOT_VALIDATOR", true
	case 6:
		return "LXP_PRICEFEED_INVALID_KEY", true
	case 7:
		return "LXP_PRICEFEED_KEY_IN_USE", true
	case 8:
		return "LXP_PRICEFEED_NO_KEY", true
	case 9:
		return "LXP_PRICEFEED_UNKNOWN_MARKET", true
	case 10:
		return "LXP_PRICEFEED_UNREGISTERED_FEEDER", true
	case 11:
		return "LXP_PRICEFEED_RATE_LIMITED", true
	case 12:
		return "LXP_PRICEFEED_TOO_MANY_OBSERVATIONS", true
	case 13:
		return "LXP_PRICEFEED_SUBMISSION_TOO_LARGE", true
	case 14:
		return "LXP_PRICEFEED_DUPLICATE_MARKET", true
	case 15:
		return "LXP_PRICEFEED_SUBMISSION_ALONE", true
	case 16:
		return "LXP_PRICEFEED_INVALID_OBSERVATION", true
	default:
		return "", false
	}
}
