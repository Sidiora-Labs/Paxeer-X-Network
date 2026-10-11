package types

import (
	"errors"

	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

var (
	ErrUnauthorized     = sdkerrors.Register(ModuleName, 2, "unauthorized")
	ErrInvalidGenesis   = sdkerrors.Register(ModuleName, 3, "invalid genesis")
	ErrInvalidParams    = sdkerrors.Register(ModuleName, 4, "invalid params")
	ErrInvalidAttestors = sdkerrors.Register(ModuleName, 5, "invalid attestor set")
	ErrInvalidThreshold = sdkerrors.Register(ModuleName, 6, "threshold is not a majority of the attestor set")
	ErrPaused           = sdkerrors.Register(ModuleName, 7, "xweb paused")
	ErrUnknownKind      = sdkerrors.Register(ModuleName, 8, "unknown request kind")
	ErrPayloadSize      = sdkerrors.Register(ModuleName, 9, "payload empty or over the payload cap")
	ErrCallbackGas      = sdkerrors.Register(ModuleName, 10, "callback gas zero or over the callback cap")
	ErrWrongFee         = sdkerrors.Register(ModuleName, 11, "paid amount is not the current fee")
	ErrUnknownRequest   = sdkerrors.Register(ModuleName, 12, "request not found")
	ErrAlreadyFulfilled = sdkerrors.Register(ModuleName, 13, "request already fulfilled")
	ErrRefunded         = sdkerrors.Register(ModuleName, 14, "request already refunded")
	ErrResponseTooLarge = sdkerrors.Register(ModuleName, 15, "response over the stored response bound")
	ErrInvalidLength    = sdkerrors.Register(ModuleName, 16, "full length shorter than the response")
	ErrBadSignature     = sdkerrors.Register(ModuleName, 17, "invalid attestor signature")
	ErrBelowThreshold   = sdkerrors.Register(ModuleName, 18, "attestor signatures below threshold")
	ErrNotExpired       = sdkerrors.Register(ModuleName, 19, "request not yet past its timeout")
	ErrUnknownAttestor  = sdkerrors.Register(ModuleName, 20, "attestor not registered")
	ErrCallbackRecorded = sdkerrors.Register(ModuleName, 21, "callback outcome already recorded")
	ErrInvalidRequest   = sdkerrors.Register(ModuleName, 22, "invalid request")
	ErrInvalidApi       = sdkerrors.Register(ModuleName, 23, "invalid api payload")
	ErrInvalidEnvelope  = sdkerrors.Register(ModuleName, 24, "invalid credential envelope")
	ErrInvalidLevel     = sdkerrors.Register(ModuleName, 25, "invalid attestation level")
)

func LXPName(err error) (string, bool) {
	var registered *sdkerrors.Error
	if !errors.As(err, &registered) || registered == nil || registered.Codespace() != ModuleName {
		return "", false
	}
	switch registered.ABCICode() {
	case 2:
		return "LXP_XWEB_UNAUTHORIZED", true
	case 3:
		return "LXP_XWEB_INVALID_GENESIS", true
	case 4:
		return "LXP_XWEB_INVALID_PARAMS", true
	case 5:
		return "LXP_XWEB_INVALID_ATTESTORS", true
	case 6:
		return "LXP_XWEB_INVALID_THRESHOLD", true
	case 7:
		return "LXP_XWEB_PAUSED", true
	case 8:
		return "LXP_XWEB_UNKNOWN_KIND", true
	case 9:
		return "LXP_XWEB_PAYLOAD_SIZE", true
	case 10:
		return "LXP_XWEB_CALLBACK_GAS", true
	case 11:
		return "LXP_XWEB_WRONG_FEE", true
	case 12:
		return "LXP_XWEB_UNKNOWN_REQUEST", true
	case 13:
		return "LXP_XWEB_ALREADY_FULFILLED", true
	case 14:
		return "LXP_XWEB_REFUNDED", true
	case 15:
		return "LXP_XWEB_RESPONSE_TOO_LARGE", true
	case 16:
		return "LXP_XWEB_INVALID_LENGTH", true
	case 17:
		return "LXP_XWEB_BAD_SIGNATURE", true
	case 18:
		return "LXP_XWEB_BELOW_THRESHOLD", true
	case 19:
		return "LXP_XWEB_NOT_EXPIRED", true
	case 20:
		return "LXP_XWEB_UNKNOWN_ATTESTOR", true
	case 21:
		return "LXP_XWEB_CALLBACK_RECORDED", true
	case 22:
		return "LXP_XWEB_INVALID_REQUEST", true
	case 23:
		return "LXP_XWEB_INVALID_API", true
	case 24:
		return "LXP_XWEB_INVALID_ENVELOPE", true
	case 25:
		return "LXP_XWEB_INVALID_LEVEL", true
	default:
		return "", false
	}
}
