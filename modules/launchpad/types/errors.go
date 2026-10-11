package types

import (
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

func LaunchpadCode(name string) string {
	return "LXP_LAUNCHPAD_" + name
}

var (
	ErrInvalidParams           = sdkerrors.Register(ModuleName, 2, LaunchpadCode("INVALID_PARAMS"))
	ErrOverflow                = sdkerrors.Register(ModuleName, 3, LaunchpadCode("OVERFLOW"))
	ErrDivisionByZero          = sdkerrors.Register(ModuleName, 4, LaunchpadCode("DIVISION_BY_ZERO"))
	ErrInsufficientInput       = sdkerrors.Register(ModuleName, 5, LaunchpadCode("INSUFFICIENT_INPUT"))
	ErrInsufficientLiquidity   = sdkerrors.Register(ModuleName, 6, LaunchpadCode("INSUFFICIENT_LIQUIDITY"))
	ErrVirtualFloorBreached    = sdkerrors.Register(ModuleName, 7, LaunchpadCode("VIRTUAL_FLOOR_BREACHED"))
	ErrSlippageExceeded        = sdkerrors.Register(ModuleName, 8, LaunchpadCode("SLIPPAGE_EXCEEDED"))
	ErrDeadlineExpired         = sdkerrors.Register(ModuleName, 9, LaunchpadCode("DEADLINE_EXPIRED"))
	ErrZeroAddress             = sdkerrors.Register(ModuleName, 10, LaunchpadCode("ZERO_ADDRESS"))
	ErrZeroAmount              = sdkerrors.Register(ModuleName, 11, LaunchpadCode("ZERO_AMOUNT"))
	ErrUnknownMarket           = sdkerrors.Register(ModuleName, 12, LaunchpadCode("UNKNOWN_MARKET"))
	ErrPaused                  = sdkerrors.Register(ModuleName, 13, LaunchpadCode("PAUSED"))
	ErrNotPaused               = sdkerrors.Register(ModuleName, 14, LaunchpadCode("NOT_PAUSED"))
	ErrNotGuardian             = sdkerrors.Register(ModuleName, 15, LaunchpadCode("NOT_GUARDIAN"))
	ErrNotFeeRightsHolder      = sdkerrors.Register(ModuleName, 16, LaunchpadCode("NOT_FEE_RIGHTS_HOLDER"))
	ErrWrongStrategy           = sdkerrors.Register(ModuleName, 17, LaunchpadCode("WRONG_STRATEGY"))
	ErrInvalidStrategy         = sdkerrors.Register(ModuleName, 18, LaunchpadCode("INVALID_STRATEGY"))
	ErrNoFeesAccumulated       = sdkerrors.Register(ModuleName, 19, LaunchpadCode("NO_FEES_ACCUMULATED"))
	ErrAirdropNotTriggered     = sdkerrors.Register(ModuleName, 20, LaunchpadCode("AIRDROP_NOT_TRIGGERED"))
	ErrAlreadyClaimed          = sdkerrors.Register(ModuleName, 21, LaunchpadCode("ALREADY_CLAIMED"))
	ErrInvalidMarket           = sdkerrors.Register(ModuleName, 22, LaunchpadCode("INVALID_MARKET"))
	ErrUnauthorized            = sdkerrors.Register(ModuleName, 23, LaunchpadCode("UNAUTHORIZED"))
	ErrInvalidGenesis          = sdkerrors.Register(ModuleName, 24, LaunchpadCode("INVALID_GENESIS"))
	ErrHoldingHistory          = sdkerrors.Register(ModuleName, 25, LaunchpadCode("HOLDING_HISTORY"))
	ErrUnsupportedHoldingEpoch = sdkerrors.Register(ModuleName, 26, LaunchpadCode("UNSUPPORTED_HOLDING_EPOCH"))
	ErrInvalidAirdropEpoch     = sdkerrors.Register(ModuleName, 27, LaunchpadCode("INVALID_AIRDROP_EPOCH"))
	ErrLegacyAirdropEpoch      = sdkerrors.Register(ModuleName, 28, LaunchpadCode("LEGACY_AIRDROP_EPOCH"))
	ErrAirdropBasis            = sdkerrors.Register(ModuleName, 29, LaunchpadCode("AIRDROP_BASIS"))
	ErrInvalidRange            = sdkerrors.Register(ModuleName, 30, LaunchpadCode("INVALID_RANGE"))
)
