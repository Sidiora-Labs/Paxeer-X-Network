package app

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	layerxbridgetypes "github.com/sidiora-labs/paxeer-network/modules/layerxbridge/types"
	layerxcustodytypes "github.com/sidiora-labs/paxeer-network/modules/layerxcustody/types"
	tokenfactorykeeper "github.com/sidiora-labs/paxeer-network/modules/tokenfactory/keeper"
	tokenfactorytypes "github.com/sidiora-labs/paxeer-network/modules/tokenfactory/types"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
	upgradetypes "github.com/sidiora-labs/paxeer-network/sdk/x/upgrade/types"
	"github.com/stretchr/testify/require"
)

func emergencyProposal(t *testing.T) *layerxcustodytypes.CustodyProposal {
	t.Helper()
	proposal, err := layerxcustodytypes.NewCustodyProposal("Emergency", "Switch custody to emergency",
		&layerxcustodytypes.MsgSetEmergency{Authority: layerxcustodytypes.GovernanceAuthority(), Enabled: true})
	require.NoError(t, err)
	return proposal
}

func TestCustodyGovernanceV611ActivationBoundaries(t *testing.T) {
	require.Equal(t, layerxcustodytypes.GovernanceActivationUpgrade, V611Upgrade)
	g := newCustodyGovernance(t)
	h := g.ctx.BlockHeight()

	before, _ := g.ctx.CacheContext()
	require.ErrorIs(t, g.route()(before, emergencyProposal(t)), layerxcustodytypes.ErrGovernanceNotActive)
	require.False(t, g.k.GetEmergency(before))

	g.activate(t)
	require.Equal(t, h, g.app.UpgradeKeeper.GetDoneHeight(g.ctx, V611Upgrade))

	below, _ := g.ctx.WithBlockHeight(h - 1).CacheContext()
	require.ErrorIs(t, g.route()(below, emergencyProposal(t)), layerxcustodytypes.ErrGovernanceNotActive)
	require.False(t, g.k.GetEmergency(below))

	for _, height := range []int64{h, h + 1} {
		at, _ := g.ctx.WithBlockHeight(height).CacheContext()
		require.NoError(t, g.k.GovernanceExecutionActive(at))
		require.NoError(t, g.route()(at, emergencyProposal(t)), "height %d", height)
		require.True(t, g.k.GetEmergency(at), "height %d", height)
	}
	require.False(t, g.k.GetEmergency(g.ctx), "each height ran on its own branch")
}

func TestCustodyGovernanceHistoricalContextRefusal(t *testing.T) {
	g := newCustodyGovernance(t)
	g.activate(t)
	h := g.ctx.BlockHeight()
	state := g.k.ExportGenesis(g.ctx)

	historical := g.ctx.WithBlockHeight(h - 5)
	events := len(historical.EventManager().Events())
	require.ErrorIs(t, g.k.GovernanceExecutionActive(historical), layerxcustodytypes.ErrGovernanceNotActive)
	require.ErrorIs(t, g.route()(historical, g.allMessages(t)), layerxcustodytypes.ErrGovernanceNotActive)
	g.requireUnchanged(t, historical, state, events)
	require.Equal(t, state, g.k.ExportGenesis(g.ctx))
}

type v611Chain struct {
	app     *App
	ctx     sdk.Context
	holders []common.Address
	listed  []sdk.Int
}

func newV611Chain(t *testing.T) *v611Chain {
	t.Helper()
	a := Setup(t, false, false, false)
	ctx := a.GetContextForDeliverTx([]byte{}).WithBlockHeight(1_000).WithBlockTime(time.Unix(1_800_000_000, 0).UTC())
	holders, listed, err := parseSIDHolders(sidHoldersJSON)
	require.NoError(t, err)
	require.Len(t, holders, 3)
	return &v611Chain{app: a, ctx: ctx, holders: holders, listed: listed}
}

func (c *v611Chain) seed(slot common.Hash, amount int64) {
	c.app.EvmKeeper.SetState(c.ctx, SidioraProxyAddress, slot, common.BigToHash(big.NewInt(amount)))
}

func (c *v611Chain) slot(slot common.Hash) *big.Int {
	return c.app.EvmKeeper.GetState(c.ctx, SidioraProxyAddress, slot).Big()
}

func (c *v611Chain) usid(holder common.Address) sdk.Int {
	return c.app.BankKeeper.GetBalance(c.ctx, c.app.EvmKeeper.GetPaxAddressOrDefault(c.ctx, holder), layerxbridgetypes.SidioraDenom()).Amount
}

func (c *v611Chain) apply(t *testing.T) {
	t.Helper()
	plan := upgradetypes.Plan{Name: V611Upgrade, Height: c.ctx.BlockHeight()}
	require.NoError(t, c.app.UpgradeKeeper.ScheduleUpgrade(c.ctx, plan))
	c.app.UpgradeKeeper.ApplyUpgrade(c.ctx, plan)
	require.Equal(t, c.ctx.BlockHeight(), c.app.UpgradeKeeper.GetDoneHeight(c.ctx, V611Upgrade))
}

func TestV611IsAKnownPlanWithAHandler(t *testing.T) {
	require.Contains(t, knownUpgradePlans(), V611Upgrade)
	c := newV611Chain(t)
	require.True(t, c.app.UpgradeKeeper.HasHandler(V611Upgrade))
	_, ok := layerxStoreUpgrades(V611Upgrade)
	require.False(t, ok, "the plan adds no store")
}

func TestV611MintsTheSlotBalancesZeroesThemAndMovesTheMintAuthority(t *testing.T) {
	c := newV611Chain(t)
	denom := layerxbridgetypes.SidioraDenom()
	bridge := layerxbridgetypes.ModuleAddress().String()
	other := sdk.AccAddress(common.HexToAddress("0x1255000000000000000000000000000000001255").Bytes()).String()

	created, err := c.app.TokenFactoryKeeper.CreateDenom(c.ctx, bridge, layerxbridgetypes.SidioraSubdenom)
	require.NoError(t, err)
	require.Equal(t, denom, created)
	_, err = tokenfactorykeeper.NewMsgServerImpl(c.app.TokenFactoryKeeper).ChangeAdmin(sdk.WrapSDKContext(c.ctx),
		&tokenfactorytypes.MsgChangeAdmin{Sender: bridge, Denom: denom, NewAdmin: other})
	require.NoError(t, err)

	// The second holder's slot differs from its listed balance: the slot wins.
	onchain := []int64{2_000_000, 1_500_000, 500_000}
	require.NotEqual(t, c.listed[1].Int64(), onchain[1])
	for i, holder := range c.holders {
		c.seed(sidLegacyBalanceSlot(holder), onchain[i])
	}
	c.seed(sidLegacyTotalSupplySlot(), 4_000_000)

	c.apply(t)

	for i, holder := range c.holders {
		require.Equal(t, sdk.NewInt(onchain[i]), c.usid(holder), holder.Hex())
		require.Zero(t, c.slot(sidLegacyBalanceSlot(holder)).Sign(), "legacy slot of %s", holder.Hex())
	}
	require.Equal(t, sdk.NewInt(4_000_000), c.app.BankKeeper.GetSupply(c.ctx, denom).Amount)
	require.Zero(t, c.slot(sidShortfallSlot()).Sign())
	require.Equal(t, big.NewInt(c.ctx.BlockHeight()), c.slot(sidMigratedHeightSlot()))

	admin, err := c.app.TokenFactoryKeeper.GetAuthorityMetadata(c.ctx, denom)
	require.NoError(t, err)
	require.Equal(t, bridge, admin.Admin)
	require.NotEqual(t, other, admin.Admin)

	// A second run is a no-op even with a legacy slot holding a value again.
	c.seed(sidLegacyBalanceSlot(c.holders[0]), 7)
	_, err = c.app.runV611Upgrade(c.ctx, upgradetypes.Plan{Name: V611Upgrade}, c.app.UpgradeKeeper.GetModuleVersionMap(c.ctx))
	require.NoError(t, err)
	for i, holder := range c.holders {
		require.Equal(t, sdk.NewInt(onchain[i]), c.usid(holder), holder.Hex())
	}
	require.Equal(t, big.NewInt(7), c.slot(sidLegacyBalanceSlot(c.holders[0])))
	require.Equal(t, sdk.NewInt(4_000_000), c.app.BankKeeper.GetSupply(c.ctx, denom).Amount)
	admin, err = c.app.TokenFactoryKeeper.GetAuthorityMetadata(c.ctx, denom)
	require.NoError(t, err)
	require.Equal(t, bridge, admin.Admin)
}

func TestV611RecordsTheUncoveredSupplyAsAClaimableShortfall(t *testing.T) {
	c := newV611Chain(t)
	denom := layerxbridgetypes.SidioraDenom()
	sum := int64(0)
	for i, holder := range c.holders {
		c.seed(sidLegacyBalanceSlot(holder), c.listed[i].Int64())
		sum += c.listed[i].Int64()
	}
	c.seed(sidLegacyTotalSupplySlot(), sum+123_456)
	missed := common.HexToAddress("0x5a1D0000000000000000000000000000000000d4")
	c.seed(sidLegacyBalanceSlot(missed), 123_456)

	c.apply(t)

	require.Equal(t, sdk.NewInt(sum), c.app.BankKeeper.GetSupply(c.ctx, denom).Amount, "the shortfall is never minted")
	require.Equal(t, big.NewInt(123_456), c.slot(sidShortfallSlot()))
	require.True(t, c.usid(missed).IsZero())
	require.Equal(t, big.NewInt(123_456), c.slot(sidLegacyBalanceSlot(missed)), "an unlisted holder's slot stays for migrateLegacy")
	for _, holder := range c.holders {
		require.Zero(t, c.slot(sidLegacyBalanceSlot(holder)).Sign())
	}

	// The chain had no usid denom: the plan created it under the bridge module.
	admin, err := c.app.TokenFactoryKeeper.GetAuthorityMetadata(c.ctx, denom)
	require.NoError(t, err)
	require.Equal(t, layerxbridgetypes.ModuleAddress().String(), admin.Admin)
}

func TestV611RefusesAMintAboveTheLegacySupply(t *testing.T) {
	c := newV611Chain(t)
	c.seed(sidLegacyBalanceSlot(c.holders[0]), 10)
	c.seed(sidLegacyTotalSupplySlot(), 9)
	err := c.app.migrateSIDHolders(c.ctx, sidHoldersJSON)
	require.ErrorContains(t, err, "over the legacy SID total supply")
}
