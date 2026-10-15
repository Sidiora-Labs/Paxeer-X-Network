package types

import (
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

const ModuleName = "perps"

func PerpsCode(name string) string {
	return "LXP_PERPS_" + name
}

var (
	ErrInvalidParams            = sdkerrors.Register(ModuleName, 2, PerpsCode("INVALID_PARAMS"))
	ErrUnknownMarket            = sdkerrors.Register(ModuleName, 3, PerpsCode("UNKNOWN_MARKET"))
	ErrMarketDisabled           = sdkerrors.Register(ModuleName, 4, PerpsCode("MARKET_DISABLED"))
	ErrPriceStale               = sdkerrors.Register(ModuleName, 5, PerpsCode("PRICE_STALE"))
	ErrZeroAmount               = sdkerrors.Register(ModuleName, 6, PerpsCode("ZERO_AMOUNT"))
	ErrUnsupportedCollateral    = sdkerrors.Register(ModuleName, 7, PerpsCode("UNSUPPORTED_COLLATERAL"))
	ErrInsufficientCollateral   = sdkerrors.Register(ModuleName, 8, PerpsCode("INSUFFICIENT_COLLATERAL"))
	ErrInsufficientMargin       = sdkerrors.Register(ModuleName, 9, PerpsCode("INSUFFICIENT_MARGIN"))
	ErrLeverageTooHigh          = sdkerrors.Register(ModuleName, 10, PerpsCode("LEVERAGE_TOO_HIGH"))
	ErrTickSize                 = sdkerrors.Register(ModuleName, 11, PerpsCode("TICK_SIZE"))
	ErrLotSize                  = sdkerrors.Register(ModuleName, 12, PerpsCode("LOT_SIZE"))
	ErrMinOrderValue            = sdkerrors.Register(ModuleName, 13, PerpsCode("MIN_ORDER_VALUE"))
	ErrMaxOrderValue            = sdkerrors.Register(ModuleName, 14, PerpsCode("MAX_ORDER_VALUE"))
	ErrOpenInterestCap          = sdkerrors.Register(ModuleName, 15, PerpsCode("OPEN_INTEREST_CAP"))
	ErrInvalidTimeInForce       = sdkerrors.Register(ModuleName, 16, PerpsCode("INVALID_TIME_IN_FORCE"))
	ErrInvalidOrderType         = sdkerrors.Register(ModuleName, 17, PerpsCode("INVALID_ORDER_TYPE"))
	ErrPostOnlyWouldCross       = sdkerrors.Register(ModuleName, 18, PerpsCode("POST_ONLY_WOULD_CROSS"))
	ErrFokNotFilled             = sdkerrors.Register(ModuleName, 19, PerpsCode("FOK_NOT_FILLED"))
	ErrNoLiquidity              = sdkerrors.Register(ModuleName, 20, PerpsCode("NO_LIQUIDITY"))
	ErrSlippageExceeded         = sdkerrors.Register(ModuleName, 21, PerpsCode("SLIPPAGE_EXCEEDED"))
	ErrDeadlineExpired          = sdkerrors.Register(ModuleName, 22, PerpsCode("DEADLINE_EXPIRED"))
	ErrReduceOnlyViolation      = sdkerrors.Register(ModuleName, 23, PerpsCode("REDUCE_ONLY_VIOLATION"))
	ErrDuplicateCloid           = sdkerrors.Register(ModuleName, 24, PerpsCode("DUPLICATE_CLOID"))
	ErrUnknownOrder             = sdkerrors.Register(ModuleName, 25, PerpsCode("UNKNOWN_ORDER"))
	ErrNotOrderHolder           = sdkerrors.Register(ModuleName, 26, PerpsCode("NOT_ORDER_HOLDER"))
	ErrUnknownPosition          = sdkerrors.Register(ModuleName, 27, PerpsCode("UNKNOWN_POSITION"))
	ErrNotPositionHolder        = sdkerrors.Register(ModuleName, 28, PerpsCode("NOT_POSITION_HOLDER"))
	ErrInvalidTriggerPrice      = sdkerrors.Register(ModuleName, 29, PerpsCode("INVALID_TRIGGER_PRICE"))
	ErrTriggerNotReached        = sdkerrors.Register(ModuleName, 30, PerpsCode("TRIGGER_NOT_REACHED"))
	ErrTooManyOpenOrders        = sdkerrors.Register(ModuleName, 31, PerpsCode("TOO_MANY_OPEN_ORDERS"))
	ErrCrossMarginOnly          = sdkerrors.Register(ModuleName, 32, PerpsCode("CROSS_MARGIN_ONLY"))
	ErrNotLiquidatable          = sdkerrors.Register(ModuleName, 33, PerpsCode("NOT_LIQUIDATABLE"))
	ErrLiquidationCooldown      = sdkerrors.Register(ModuleName, 34, PerpsCode("LIQUIDATION_COOLDOWN"))
	ErrUnauthorized             = sdkerrors.Register(ModuleName, 35, PerpsCode("UNAUTHORIZED"))
	ErrStaticCall               = sdkerrors.Register(ModuleName, 36, PerpsCode("STATIC_CALL"))
	ErrDelegatecall             = sdkerrors.Register(ModuleName, 37, PerpsCode("DELEGATECALL"))
	ErrOverflow                 = sdkerrors.Register(ModuleName, 38, PerpsCode("OVERFLOW"))
	ErrInvalidGenesis           = sdkerrors.Register(ModuleName, 39, PerpsCode("INVALID_GENESIS"))
	ErrCloseSizeExceedsPosition = sdkerrors.Register(ModuleName, 40, PerpsCode("CLOSE_SIZE_EXCEEDS_POSITION"))
	ErrSelfTransfer             = sdkerrors.Register(ModuleName, 41, PerpsCode("SELF_TRANSFER"))
	ErrZeroAddress              = sdkerrors.Register(ModuleName, 42, PerpsCode("ZERO_ADDRESS"))
	ErrNoAccount                = sdkerrors.Register(ModuleName, 43, PerpsCode("NO_ACCOUNT"))
	ErrTokenTransferFailed      = sdkerrors.Register(ModuleName, 44, PerpsCode("TOKEN_TRANSFER_FAILED"))
	ErrPaused                   = sdkerrors.Register(ModuleName, 45, PerpsCode("PAUSED"))
)
