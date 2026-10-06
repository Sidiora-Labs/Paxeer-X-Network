package legacyabci

import (
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/telemetry"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/capability"
	capabilitykeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/capability/keeper"

	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/distribution"
	distrkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/distribution/keeper"

	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/evidence"
	evidencekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/evidence/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/slashing"
	slashingkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/slashing/keeper"

	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking"
	stakingkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade"
	upgradekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/keeper"

	ibcclient "github.com/Sidiora-Labs/Paxeer-X-Network/interchain/modules/core/02-client"
	ibckeeper "github.com/Sidiora-Labs/Paxeer-X-Network/interchain/modules/core/keeper"
	epochmodulekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/epoch/keeper"
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
)

type BeginBlockKeepers struct {
	EpochKeeper      *epochmodulekeeper.Keeper
	UpgradeKeeper    *upgradekeeper.Keeper
	CapabilityKeeper *capabilitykeeper.Keeper
	DistrKeeper      *distrkeeper.Keeper
	SlashingKeeper   *slashingkeeper.Keeper
	EvidenceKeeper   *evidencekeeper.Keeper
	StakingKeeper    *stakingkeeper.Keeper
	IBCKeeper        *ibckeeper.Keeper
	EvmKeeper        *evmkeeper.Keeper
}

func BeginBlock(
	ctx sdk.Context,
	height int64,
	votes []abci.VoteInfo,
	byzantineValidators []abci.Misbehavior,
	keepers BeginBlockKeepers,
) {
	start := time.Now()
	defer func() {
		legacyAbciMetrics.totalBeginBlockDuration.Record(ctx.Context(), time.Since(start).Seconds())
		// TODO(PLT-343): remove once begin_blocker_duration verified
		telemetry.MeasureSince(start, "module", "total_begin_block")
	}()

	keepers.EpochKeeper.BeginBlock(ctx)
	upgrade.BeginBlocker(*keepers.UpgradeKeeper, ctx)
	capability.BeginBlocker(ctx, *keepers.CapabilityKeeper)
	distribution.BeginBlocker(ctx, votes, *keepers.DistrKeeper)
	slashing.BeginBlocker(ctx, votes, *keepers.SlashingKeeper)
	evidence.BeginBlocker(ctx, byzantineValidators, *keepers.EvidenceKeeper)
	staking.BeginBlocker(ctx, *keepers.StakingKeeper)
	func() {
		ibcStart := time.Now()
		defer func() {
			legacyAbciMetrics.ibcBeginBlockerDuration.Record(ctx.Context(), time.Since(ibcStart).Seconds())
			// TODO(PLT-343): remove once ibc_begin_blocker_duration verified
			telemetry.ModuleMeasureSince("ibc", ibcStart, telemetry.MetricKeyBeginBlocker)
		}()
		ibcclient.BeginBlocker(ctx, keepers.IBCKeeper.ClientKeeper)
	}()
	keepers.EvmKeeper.BeginBlock(ctx)
}
