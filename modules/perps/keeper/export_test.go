package keeper

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

const (
	SecondsPerHour         = secondsPerHour
	EventTypeEndBlockError = eventTypeEndBlockError
	AttributeStep          = attributeStep
	AttributeMarketID      = attributeMarketID
	AttributeError         = attributeError
	StepPrices             = stepPrices
	StepPremiumSamples     = stepPremiumSamples
	StepFundingIndex       = stepFundingIndex
	StepTriggers           = stepTriggers
	StepExpiries           = stepExpiries
	StepSweep              = stepSweep
	StepLiquidation        = stepLiquidation
	StepDeficits           = stepDeficits
	StepBackstopCloses     = stepBackstopCloses
)

var (
	USDUnit        = usdUnit
	OrderTriggered = orderTriggered
)

func EndBlockStepNames() []string {
	names := make([]string, 0, len(endBlockSteps))
	for _, step := range endBlockSteps {
		names = append(names, step.name)
	}
	return names
}

func (k *Keeper) LiquidationAccount(ctx sdk.Context, address common.Address) (types.Account, error) {
	return k.liquidationAccount(ctx, address)
}

func (k *Keeper) LiquidationPosition(ctx sdk.Context, positionID uint256.Int) (types.Position, error) {
	return k.liquidationPosition(ctx, positionID)
}

func (k *Keeper) LiquidationRebucket(ctx sdk.Context, address common.Address) (bool, error) {
	return k.liquidationRebucket(ctx, address)
}

func (k *Keeper) InsuranceVenueBooks(ctx sdk.Context) (types.VenueBooks, error) {
	return k.insuranceVenueBooks(ctx)
}
