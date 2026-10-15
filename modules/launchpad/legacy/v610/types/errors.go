package typesv610

import (
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

const Codespace = ModuleName + "_legacy_v610"

func LaunchpadCode(name string) string {
	return "LXP_LAUNCHPAD_" + name
}

var (
	ErrInvalidParams           = sdkerrors.Register(Codespace, 2, LaunchpadCode("INVALID_PARAMS"))
	ErrOverflow                = sdkerrors.Register(Codespace, 3, LaunchpadCode("OVERFLOW"))
	ErrDivisionByZero          = sdkerrors.Register(Codespace, 4, LaunchpadCode("DIVISION_BY_ZERO"))
	ErrInsufficientInput       = sdkerrors.Register(Codespace, 5, LaunchpadCode("INSUFFICIENT_INPUT"))
	ErrInsufficientLiquidity   = sdkerrors.Register(Codespace, 6, LaunchpadCode("INSUFFICIENT_LIQUIDITY"))
	ErrVirtualFloorBreached    = sdkerrors.Register(Codespace, 7, LaunchpadCode("VIRTUAL_FLOOR_BREACHED"))
	ErrSlippageExceeded        = sdkerrors.Register(Codespace, 8, LaunchpadCode("SLIPPAGE_EXCEEDED"))
	ErrDeadlineExpired         = sdkerrors.Register(Codespace, 9, LaunchpadCode("DEADLINE_EXPIRED"))
	ErrZeroAddress             = sdkerrors.Register(Codespace, 10, LaunchpadCode("ZERO_ADDRESS"))
	ErrZeroAmount              = sdkerrors.Register(Codespace, 11, LaunchpadCode("ZERO_AMOUNT"))
	ErrUnknownMarket           = sdkerrors.Register(Codespace, 12, LaunchpadCode("UNKNOWN_MARKET"))
	ErrPaused                  = sdkerrors.Register(Codespace, 13, LaunchpadCode("PAUSED"))
	ErrNotPaused               = sdkerrors.Register(Codespace, 14, LaunchpadCode("NOT_PAUSED"))
	ErrNotGuardian             = sdkerrors.Register(Codespace, 15, LaunchpadCode("NOT_GUARDIAN"))
	ErrNotFeeRightsHolder      = sdkerrors.Register(Codespace, 16, LaunchpadCode("NOT_FEE_RIGHTS_HOLDER"))
	ErrWrongStrategy           = sdkerrors.Register(Codespace, 17, LaunchpadCode("WRONG_STRATEGY"))
	ErrInvalidStrategy         = sdkerrors.Register(Codespace, 18, LaunchpadCode("INVALID_STRATEGY"))
	ErrNoFeesAccumulated       = sdkerrors.Register(Codespace, 19, LaunchpadCode("NO_FEES_ACCUMULATED"))
	ErrAirdropNotTriggered     = sdkerrors.Register(Codespace, 20, LaunchpadCode("AIRDROP_NOT_TRIGGERED"))
	ErrAlreadyClaimed          = sdkerrors.Register(Codespace, 21, LaunchpadCode("ALREADY_CLAIMED"))
	ErrInvalidMarket           = sdkerrors.Register(Codespace, 22, LaunchpadCode("INVALID_MARKET"))
	ErrUnauthorized            = sdkerrors.Register(Codespace, 23, LaunchpadCode("UNAUTHORIZED"))
	ErrInvalidGenesis          = sdkerrors.Register(Codespace, 24, LaunchpadCode("INVALID_GENESIS"))
	ErrHoldingHistory          = sdkerrors.Register(Codespace, 25, LaunchpadCode("HOLDING_HISTORY"))
	ErrUnsupportedHoldingEpoch = sdkerrors.Register(Codespace, 26, LaunchpadCode("UNSUPPORTED_HOLDING_EPOCH"))
	ErrInvalidAirdropEpoch     = sdkerrors.Register(Codespace, 27, LaunchpadCode("INVALID_AIRDROP_EPOCH"))
	ErrLegacyAirdropEpoch      = sdkerrors.Register(Codespace, 28, LaunchpadCode("LEGACY_AIRDROP_EPOCH"))
	ErrAirdropBasis            = sdkerrors.Register(Codespace, 29, LaunchpadCode("AIRDROP_BASIS"))
	ErrInvalidRange            = sdkerrors.Register(Codespace, 30, LaunchpadCode("INVALID_RANGE"))
)
