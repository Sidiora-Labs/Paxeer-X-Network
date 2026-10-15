package typesv610

import (
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

func LaunchpadCode(name string) string {
	return "LXP_LAUNCHPAD_" + name
}

var (
	ErrInvalidParams           = sdkerrors.New(ModuleName, 2, LaunchpadCode("INVALID_PARAMS"))
	ErrOverflow                = sdkerrors.New(ModuleName, 3, LaunchpadCode("OVERFLOW"))
	ErrDivisionByZero          = sdkerrors.New(ModuleName, 4, LaunchpadCode("DIVISION_BY_ZERO"))
	ErrInsufficientInput       = sdkerrors.New(ModuleName, 5, LaunchpadCode("INSUFFICIENT_INPUT"))
	ErrInsufficientLiquidity   = sdkerrors.New(ModuleName, 6, LaunchpadCode("INSUFFICIENT_LIQUIDITY"))
	ErrVirtualFloorBreached    = sdkerrors.New(ModuleName, 7, LaunchpadCode("VIRTUAL_FLOOR_BREACHED"))
	ErrSlippageExceeded        = sdkerrors.New(ModuleName, 8, LaunchpadCode("SLIPPAGE_EXCEEDED"))
	ErrDeadlineExpired         = sdkerrors.New(ModuleName, 9, LaunchpadCode("DEADLINE_EXPIRED"))
	ErrZeroAddress             = sdkerrors.New(ModuleName, 10, LaunchpadCode("ZERO_ADDRESS"))
	ErrZeroAmount              = sdkerrors.New(ModuleName, 11, LaunchpadCode("ZERO_AMOUNT"))
	ErrUnknownMarket           = sdkerrors.New(ModuleName, 12, LaunchpadCode("UNKNOWN_MARKET"))
	ErrPaused                  = sdkerrors.New(ModuleName, 13, LaunchpadCode("PAUSED"))
	ErrNotPaused               = sdkerrors.New(ModuleName, 14, LaunchpadCode("NOT_PAUSED"))
	ErrNotGuardian             = sdkerrors.New(ModuleName, 15, LaunchpadCode("NOT_GUARDIAN"))
	ErrNotFeeRightsHolder      = sdkerrors.New(ModuleName, 16, LaunchpadCode("NOT_FEE_RIGHTS_HOLDER"))
	ErrWrongStrategy           = sdkerrors.New(ModuleName, 17, LaunchpadCode("WRONG_STRATEGY"))
	ErrInvalidStrategy         = sdkerrors.New(ModuleName, 18, LaunchpadCode("INVALID_STRATEGY"))
	ErrNoFeesAccumulated       = sdkerrors.New(ModuleName, 19, LaunchpadCode("NO_FEES_ACCUMULATED"))
	ErrAirdropNotTriggered     = sdkerrors.New(ModuleName, 20, LaunchpadCode("AIRDROP_NOT_TRIGGERED"))
	ErrAlreadyClaimed          = sdkerrors.New(ModuleName, 21, LaunchpadCode("ALREADY_CLAIMED"))
	ErrInvalidMarket           = sdkerrors.New(ModuleName, 22, LaunchpadCode("INVALID_MARKET"))
	ErrUnauthorized            = sdkerrors.New(ModuleName, 23, LaunchpadCode("UNAUTHORIZED"))
	ErrInvalidGenesis          = sdkerrors.New(ModuleName, 24, LaunchpadCode("INVALID_GENESIS"))
	ErrHoldingHistory          = sdkerrors.New(ModuleName, 25, LaunchpadCode("HOLDING_HISTORY"))
	ErrUnsupportedHoldingEpoch = sdkerrors.New(ModuleName, 26, LaunchpadCode("UNSUPPORTED_HOLDING_EPOCH"))
	ErrInvalidAirdropEpoch     = sdkerrors.New(ModuleName, 27, LaunchpadCode("INVALID_AIRDROP_EPOCH"))
	ErrLegacyAirdropEpoch      = sdkerrors.New(ModuleName, 28, LaunchpadCode("LEGACY_AIRDROP_EPOCH"))
	ErrAirdropBasis            = sdkerrors.New(ModuleName, 29, LaunchpadCode("AIRDROP_BASIS"))
	ErrInvalidRange            = sdkerrors.New(ModuleName, 30, LaunchpadCode("INVALID_RANGE"))
)
