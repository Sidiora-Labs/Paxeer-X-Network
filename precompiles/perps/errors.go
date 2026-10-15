package perps

import (
	"errors"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// ErrKeeperMissing refuses every call of a precompile built on keepers that
// do not implement PerpsKeepers.
var ErrKeeperMissing = errors.New("perps: the perps keeper is not wired")

// customErrors pairs every keeper error of modules/perps/types/errors.go with
// its parameterless custom error of contracts/perps.md section 19.
var customErrors = []struct {
	err  error
	name string
}{
	{types.ErrInvalidParams, "PerpsInvalidParams"},
	{types.ErrUnknownMarket, "PerpsUnknownMarket"},
	{types.ErrMarketDisabled, "PerpsMarketDisabled"},
	{types.ErrPriceStale, "PerpsPriceStale"},
	{types.ErrZeroAmount, "PerpsZeroAmount"},
	{types.ErrUnsupportedCollateral, "PerpsUnsupportedCollateral"},
	{types.ErrInsufficientCollateral, "PerpsInsufficientCollateral"},
	{types.ErrInsufficientMargin, "PerpsInsufficientMargin"},
	{types.ErrLeverageTooHigh, "PerpsLeverageTooHigh"},
	{types.ErrTickSize, "PerpsTickSize"},
	{types.ErrLotSize, "PerpsLotSize"},
	{types.ErrMinOrderValue, "PerpsMinOrderValue"},
	{types.ErrMaxOrderValue, "PerpsMaxOrderValue"},
	{types.ErrOpenInterestCap, "PerpsOpenInterestCap"},
	{types.ErrInvalidTimeInForce, "PerpsInvalidTimeInForce"},
	{types.ErrInvalidOrderType, "PerpsInvalidOrderType"},
	{types.ErrPostOnlyWouldCross, "PerpsPostOnlyWouldCross"},
	{types.ErrFokNotFilled, "PerpsFokNotFilled"},
	{types.ErrNoLiquidity, "PerpsNoLiquidity"},
	{types.ErrSlippageExceeded, "PerpsSlippageExceeded"},
	{types.ErrDeadlineExpired, "PerpsDeadlineExpired"},
	{types.ErrReduceOnlyViolation, "PerpsReduceOnlyViolation"},
	{types.ErrDuplicateCloid, "PerpsDuplicateCloid"},
	{types.ErrUnknownOrder, "PerpsUnknownOrder"},
	{types.ErrNotOrderHolder, "PerpsNotOrderHolder"},
	{types.ErrUnknownPosition, "PerpsUnknownPosition"},
	{types.ErrNotPositionHolder, "PerpsNotPositionHolder"},
	{types.ErrInvalidTriggerPrice, "PerpsInvalidTriggerPrice"},
	{types.ErrTriggerNotReached, "PerpsTriggerNotReached"},
	{types.ErrTooManyOpenOrders, "PerpsTooManyOpenOrders"},
	{types.ErrCrossMarginOnly, "PerpsCrossMarginOnly"},
	{types.ErrNotLiquidatable, "PerpsNotLiquidatable"},
	{types.ErrLiquidationCooldown, "PerpsLiquidationCooldown"},
	{types.ErrUnauthorized, "PerpsUnauthorized"},
	{types.ErrStaticCall, "PerpsStaticCall"},
	{types.ErrDelegatecall, "PerpsDelegatecall"},
	{types.ErrOverflow, "PerpsOverflow"},
	{types.ErrInvalidGenesis, "PerpsInvalidGenesis"},
	{types.ErrCloseSizeExceedsPosition, "PerpsCloseSizeExceedsPosition"},
	{types.ErrSelfTransfer, "PerpsSelfTransfer"},
	{types.ErrZeroAddress, "PerpsZeroAddress"},
	{types.ErrNoAccount, "PerpsNoAccount"},
	{types.ErrTokenTransferFailed, "PerpsTokenTransferFailed"},
	{types.ErrPaused, "PerpsPaused"},
}

// revertOf is the revert data of err: the 4-byte selector of the custom error
// of the keeper error err wraps, or Error(string) carrying the text of an
// error that has no custom error.
func revertOf(err error) []byte {
	for _, custom := range customErrors {
		if errors.Is(err, custom.err) {
			return crypto.Keccak256([]byte(custom.name + "()"))[:4]
		}
	}
	return pcommon.RevertReason(err)
}
