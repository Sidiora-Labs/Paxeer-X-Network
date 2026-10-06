package legacyabci

import (
	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/oracle"
	oraclekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/oracle/keeper"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov"
	govkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking"
	stakingkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/keeper"
)

type EndBlockKeepers struct {
	GovKeeper     *govkeeper.Keeper
	StakingKeeper *stakingkeeper.Keeper
	OracleKeeper  *oraclekeeper.Keeper
	EvmKeeper     *evmkeeper.Keeper
}

func EndBlock(ctx sdk.Context, height int64, blockGasUsed int64, keepers EndBlockKeepers) []abci.ValidatorUpdate {
	gov.EndBlocker(ctx, *keepers.GovKeeper)
	vals := staking.EndBlocker(ctx, *keepers.StakingKeeper)
	oracle.EndBlocker(ctx, *keepers.OracleKeeper)
	keepers.EvmKeeper.EndBlock(ctx, height, blockGasUsed)
	return vals
}
