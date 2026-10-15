package legacyabci

import (
	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/oracle"
	oraclekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/oracle/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps"
	perpskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot"
	spotkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov"
	govkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking"
	stakingkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/keeper"
	"github.com/paxeer-network/paxlog"
)

var logger = paxlog.NewLogger("app")

type EndBlockKeepers struct {
	GovKeeper     *govkeeper.Keeper
	StakingKeeper *stakingkeeper.Keeper
	OracleKeeper  *oraclekeeper.Keeper
	EvmKeeper     *evmkeeper.Keeper
	PerpsKeeper   *perpskeeper.Keeper
	SpotKeeper    *spotkeeper.Keeper
	V6120Live     func() bool
}

func EndBlock(ctx sdk.Context, height int64, blockGasUsed int64, keepers EndBlockKeepers) []abci.ValidatorUpdate {
	gov.EndBlocker(ctx, *keepers.GovKeeper)
	vals := staking.EndBlocker(ctx, *keepers.StakingKeeper)
	oracle.EndBlocker(ctx, *keepers.OracleKeeper)
	if keepers.V6120Live != nil && keepers.V6120Live() {
		perps.EndBlocker(ctx, *keepers.PerpsKeeper)
		if err := spot.EndBlocker(ctx, keepers.SpotKeeper); err != nil {
			logger.Error("spot end block failed", "height", height, "err", err)
		}
	}
	keepers.EvmKeeper.EndBlock(ctx, height, blockGasUsed)
	return vals
}
