package types_test

import (
	"strings"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/stretchr/testify/require"
)

func TestErrorsRegistry(t *testing.T) {
	cases := []struct {
		err    *sdkerrors.Error
		code   uint32
		reason string
	}{
		{types.ErrInvalidParams, 2, "LXP_PERPS_INVALID_PARAMS"},
		{types.ErrUnknownMarket, 3, "LXP_PERPS_UNKNOWN_MARKET"},
		{types.ErrMarketDisabled, 4, "LXP_PERPS_MARKET_DISABLED"},
		{types.ErrPriceStale, 5, "LXP_PERPS_PRICE_STALE"},
		{types.ErrZeroAmount, 6, "LXP_PERPS_ZERO_AMOUNT"},
		{types.ErrUnsupportedCollateral, 7, "LXP_PERPS_UNSUPPORTED_COLLATERAL"},
		{types.ErrInsufficientCollateral, 8, "LXP_PERPS_INSUFFICIENT_COLLATERAL"},
		{types.ErrInsufficientMargin, 9, "LXP_PERPS_INSUFFICIENT_MARGIN"},
		{types.ErrLeverageTooHigh, 10, "LXP_PERPS_LEVERAGE_TOO_HIGH"},
		{types.ErrTickSize, 11, "LXP_PERPS_TICK_SIZE"},
		{types.ErrLotSize, 12, "LXP_PERPS_LOT_SIZE"},
		{types.ErrMinOrderValue, 13, "LXP_PERPS_MIN_ORDER_VALUE"},
		{types.ErrMaxOrderValue, 14, "LXP_PERPS_MAX_ORDER_VALUE"},
		{types.ErrOpenInterestCap, 15, "LXP_PERPS_OPEN_INTEREST_CAP"},
		{types.ErrInvalidTimeInForce, 16, "LXP_PERPS_INVALID_TIME_IN_FORCE"},
		{types.ErrInvalidOrderType, 17, "LXP_PERPS_INVALID_ORDER_TYPE"},
		{types.ErrPostOnlyWouldCross, 18, "LXP_PERPS_POST_ONLY_WOULD_CROSS"},
		{types.ErrFokNotFilled, 19, "LXP_PERPS_FOK_NOT_FILLED"},
		{types.ErrNoLiquidity, 20, "LXP_PERPS_NO_LIQUIDITY"},
		{types.ErrSlippageExceeded, 21, "LXP_PERPS_SLIPPAGE_EXCEEDED"},
		{types.ErrDeadlineExpired, 22, "LXP_PERPS_DEADLINE_EXPIRED"},
		{types.ErrReduceOnlyViolation, 23, "LXP_PERPS_REDUCE_ONLY_VIOLATION"},
		{types.ErrDuplicateCloid, 24, "LXP_PERPS_DUPLICATE_CLOID"},
		{types.ErrUnknownOrder, 25, "LXP_PERPS_UNKNOWN_ORDER"},
		{types.ErrNotOrderHolder, 26, "LXP_PERPS_NOT_ORDER_HOLDER"},
		{types.ErrUnknownPosition, 27, "LXP_PERPS_UNKNOWN_POSITION"},
		{types.ErrNotPositionHolder, 28, "LXP_PERPS_NOT_POSITION_HOLDER"},
		{types.ErrInvalidTriggerPrice, 29, "LXP_PERPS_INVALID_TRIGGER_PRICE"},
		{types.ErrTriggerNotReached, 30, "LXP_PERPS_TRIGGER_NOT_REACHED"},
		{types.ErrTooManyOpenOrders, 31, "LXP_PERPS_TOO_MANY_OPEN_ORDERS"},
		{types.ErrCrossMarginOnly, 32, "LXP_PERPS_CROSS_MARGIN_ONLY"},
		{types.ErrNotLiquidatable, 33, "LXP_PERPS_NOT_LIQUIDATABLE"},
		{types.ErrLiquidationCooldown, 34, "LXP_PERPS_LIQUIDATION_COOLDOWN"},
		{types.ErrUnauthorized, 35, "LXP_PERPS_UNAUTHORIZED"},
		{types.ErrStaticCall, 36, "LXP_PERPS_STATIC_CALL"},
		{types.ErrDelegatecall, 37, "LXP_PERPS_DELEGATECALL"},
		{types.ErrOverflow, 38, "LXP_PERPS_OVERFLOW"},
		{types.ErrInvalidGenesis, 39, "LXP_PERPS_INVALID_GENESIS"},
		{types.ErrCloseSizeExceedsPosition, 40, "LXP_PERPS_CLOSE_SIZE_EXCEEDS_POSITION"},
		{types.ErrSelfTransfer, 41, "LXP_PERPS_SELF_TRANSFER"},
		{types.ErrZeroAddress, 42, "LXP_PERPS_ZERO_ADDRESS"},
		{types.ErrNoAccount, 43, "LXP_PERPS_NO_ACCOUNT"},
		{types.ErrTokenTransferFailed, 44, "LXP_PERPS_TOKEN_TRANSFER_FAILED"},
		{types.ErrPaused, 45, "LXP_PERPS_PAUSED"},
	}
	require.Len(t, cases, 44)
	seen := make(map[string]uint32, len(cases))
	for i, tc := range cases {
		require.Equal(t, uint32(i+2), tc.err.ABCICode(), "gap or reorder before code %d", tc.code)
		require.Equal(t, tc.code, tc.err.ABCICode())
		require.Equal(t, types.ModuleName, tc.err.Codespace())
		require.Equal(t, tc.reason, tc.err.Error())
		require.True(t, strings.HasPrefix(tc.err.Error(), "LXP_PERPS_"), tc.err.Error())
		previous, duplicate := seen[tc.err.Error()]
		require.False(t, duplicate, "codes %d and %d share a description", previous, tc.code)
		seen[tc.err.Error()] = tc.code
	}
	require.Equal(t, uint32(45), cases[len(cases)-1].err.ABCICode())
	require.Equal(t, "LXP_PERPS_UNKNOWN_MARKET", types.PerpsCode("UNKNOWN_MARKET"))
}
