package app

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	launchpadtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/wasm/x/wasm"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

// TestLaunchpadAirdropEntitlementsSurviveCommitAndReopen runs an airdrop epoch
// on the full application over a durable database, closes it, reopens the
// committed state and settles the remaining entitlements there.
func TestLaunchpadAirdropEntitlementsSurviveCommitAndReopen(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	start := time.Unix(1_800_000_000, 0).UTC()
	open := func() (*App, dbm.DB) {
		database, err := dbm.NewGoLevelDB("application", dir)
		require.NoError(t, err)
		return New(database, nil, true, map[int64]bool{}, home, 1, true, nil, MakeEncodingConfig(),
			wasm.EnableAllProposals, TestAppOpts{}, EmptyWasmOpts, nil), database
	}
	height := int64(0)
	block := func(a *App, at time.Time, body func(sdk.Context)) {
		height++
		_, err := a.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
			Header: &tmproto.Header{ChainID: "pax-test", Height: height}})
		require.NoError(t, err)
		ctx := a.GetContextForDeliverTx([]byte{}).WithBlockTime(at).WithBlockHeight(height)
		body(ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx)))
		a.SetDeliverStateToCommit()
		_, err = a.Commit(context.Background())
		require.NoError(t, err)
	}
	holder := func(name string) common.Address {
		return common.BytesToAddress(authtypes.NewModuleAddress("airdrop-holder-" + name))
	}
	tokenSend := func(a *App, ctx sdk.Context, token, from, to common.Address, amount int64) {
		launchpadERC20Send(t, a, ctx, token, from, "transfer(address,uint256)", common.LeftPadBytes(to.Bytes(), 32),
			launchpadWord(big.NewInt(amount)))
	}

	a, _ := open()
	genesis, err := json.MarshalIndent(NewDefaultGenesisState(MakeEncodingConfig().Marshaler), "", " ")
	require.NoError(t, err)
	_, err = a.InitChain(context.Background(), &abci.RequestInitChain{Time: start, ConsensusParams: DefaultConsensusParams,
		ChainId: "pax-test", AppStateBytes: genesis})
	require.NoError(t, err)

	creator, trader := holder("creator"), holder("trader")
	holderA, holderB, holderC := holder("a"), holder("b"), holder("c")
	roles := a.LaunchpadKeeper.Generation2()
	var market launchpadtypes.Market
	var pool, token common.Address
	block(a, start, func(ctx sdk.Context) {
		recordV6120Done(a, ctx)
		launchpadDeployUSDL(t, a, ctx)
		params := a.LaunchpadKeeper.GetParams(ctx)
		params.VirtualQuoteDefault = sdk.NewInt(1_000_000_000)
		params.VirtualTokenDefault = sdk.NewInt(1_000_000_000)
		require.NoError(t, a.LaunchpadKeeper.UpdateParams(ctx, authtypes.NewModuleAddress(govtypes.ModuleName).String(), params))
		granted, err := a.LaunchpadKeeper.GovernanceGrant(ctx, a.LaunchpadKeeper.Authority(), roles.FeeAccumulator,
			launchpadkeeper.FeesRouterRole, roles.FeesRouter)
		require.NoError(t, err)
		require.True(t, granted)
		launchpadUSDLFund(t, a, ctx, creator, 100_000_000)
		launchpadUSDLFund(t, a, ctx, trader, 40_000_000_000)
		market = launchpadCreateMarket(t, a, ctx, creator, "Kindle Token", "KNDL", launchpadtypes.FeeStrategyAirdrop)
		pool, token = common.HexToAddress(market.PoolAddress), common.HexToAddress(market.TokenAddress)
		require.Equal(t, sdk.NewInt(1_000_000_000), launchpadERC20Balance(t, a, ctx, token, pool))
	})

	// A mature pool charges the 30 bps base fee: 3000000 of the 1000000000
	// USDL buy, of which the 1000 bps protocol cut 300000 goes to the
	// Treasury and the pool cut 2700000 accumulates for the airdrop.
	mature := start.Add(1000 * time.Hour)
	var bought sdk.Int
	block(a, mature, func(ctx sdk.Context) {
		far := uint64(mature.Add(time.Hour).Unix())
		launchpadERC20Send(t, a, ctx, common.HexToAddress(a.LaunchpadKeeper.GetParams(ctx).QuoteToken), trader,
			"transfer(address,uint256)", common.LeftPadBytes(pool.Bytes(), 32), launchpadWord(big.NewInt(1_000_000_000)))
		_, result, err := a.LaunchpadKeeper.Swap(ctx, a.LaunchpadKeeper.ModuleCaller(), state.NewDBImpl(ctx, &a.EvmKeeper, false),
			market.Denom, true, big.NewInt(1_000_000_000), big.NewInt(0), trader, trader, far)
		require.NoError(t, err)
		require.Equal(t, "3000000", result.FeeAmount.String())
		require.Equal(t, "300000", result.ProtocolCut.String())
		require.Equal(t, "2700000", result.PoolCut.String())
		bought = sdk.NewIntFromBigInt(result.AmountOut)
		require.Equal(t, bought, launchpadERC20Balance(t, a, ctx, token, trader))
		tokenSend(a, ctx, token, trader, holderA, 600_000)
		tokenSend(a, ctx, token, trader, holderB, 400_000)
	})

	// The fee-right holder's executeAirdrop reaches the FeeAccumulator through
	// the FeesRouter, the one account holding FEES_ROUTER_ROLE; the epoch
	// snapshot is the block of the buy and the transfers.
	var funded, aShare, wantB sdk.Int
	var exported []byte
	block(a, mature.Add(time.Minute), func(ctx sdk.Context) {
		k := a.LaunchpadKeeper
		got, found := k.GetMarket(ctx, market.Denom)
		require.True(t, found)
		require.Equal(t, a.EvmKeeper.GetPaxAddressOrDefault(ctx, creator).String(), got.FeeRightsHolder)
		_, err := k.TriggerAirdrop(ctx, k.ModuleCaller(), creator, pool)
		require.ErrorIs(t, err, launchpadtypes.ErrMissingRole)
		funded, err = k.TriggerAirdrop(ctx, k.ModuleCaller(), roles.FeesRouter, pool)
		require.NoError(t, err)
		require.Equal(t, sdk.NewInt(2_700_000), funded)
		record, found, err := k.GetPoolAirdropEpoch(ctx, pool, 1)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, uint64(height-1), record.SnapshotBlock)
		require.Equal(t, bought, record.EligibleSupply)
		got, found = k.GetMarket(ctx, market.Denom)
		require.True(t, found)
		require.Equal(t, uint64(1), got.AirdropEpoch)
		require.Equal(t, funded, got.AirdropBalance)

		_, aShare, err = k.ClaimAirdropEpoch(ctx, k.ModuleCaller(), pool, holderA, got.AirdropEpoch)
		require.NoError(t, err)
		require.Equal(t, funded.Mul(sdk.NewInt(600_000)).Quo(bought), aShare)
		require.Equal(t, aShare, launchpadUSDLBalance(t, a, ctx, holderA))
		tokenSend(a, ctx, token, holderA, holderC, 600_000)
		wantB = funded.Mul(sdk.NewInt(400_000)).Quo(bought)
		exported, err = json.Marshal(k.ExportGenesis(ctx))
		require.NoError(t, err)
	})
	// Closing the application closes its database.
	require.NoError(t, a.Close())

	reopened, _ := open()
	defer func() { require.NoError(t, reopened.Close()) }()
	require.Equal(t, height, reopened.LastBlockHeight())
	block(reopened, mature.Add(2*time.Minute), func(ctx sdk.Context) {
		k := reopened.LaunchpadKeeper
		gs := k.ExportGenesis(ctx)
		require.NoError(t, gs.Validate())
		got, err := json.Marshal(gs)
		require.NoError(t, err)
		require.JSONEq(t, string(exported), string(got))
		record, found, err := k.GetPoolAirdropEpoch(ctx, pool, 1)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, funded.Sub(aShare), record.Remaining)
		require.Equal(t, sdk.NewInt(600_000), record.ClaimedSupply)
		require.True(t, k.HasClaimedPoolAirdrop(ctx, pool, holderA, 1))

		// holder C received its tokens after the snapshot.
		_, _, err = k.ClaimAirdropEpoch(ctx, k.ModuleCaller(), pool, holderC, 1)
		require.ErrorIs(t, err, launchpadtypes.ErrZeroAmount)
		_, _, err = k.ClaimAirdropEpoch(ctx, k.ModuleCaller(), pool, holderA, 1)
		require.ErrorIs(t, err, launchpadtypes.ErrAlreadyClaimed)
		before := launchpadUSDLBalance(t, reopened, ctx, holderB)
		_, bShare, err := k.ClaimAirdropEpoch(ctx, k.ModuleCaller(), pool, holderB, 1)
		require.NoError(t, err)
		require.Equal(t, wantB, bShare)
		require.Equal(t, before.Add(bShare), launchpadUSDLBalance(t, reopened, ctx, holderB))
		_, _, err = k.ClaimAirdropEpoch(ctx, k.ModuleCaller(), pool, holderB, 1)
		require.ErrorIs(t, err, launchpadtypes.ErrAlreadyClaimed)

		// The trader is the final eligible claimant and takes the rounding
		// residue, so the epoch pays out exactly what it was funded with.
		traderBefore := launchpadUSDLBalance(t, reopened, ctx, trader)
		_, traderShare, err := k.ClaimAirdropEpoch(ctx, k.ModuleCaller(), pool, trader, 1)
		require.NoError(t, err)
		require.Equal(t, funded.Sub(aShare).Sub(bShare), traderShare)
		require.True(t, traderShare.GTE(funded.Mul(bought.Sub(sdk.NewInt(1_000_000))).Quo(bought)))
		require.Equal(t, traderBefore.Add(traderShare), launchpadUSDLBalance(t, reopened, ctx, trader))
		record, _, err = k.GetPoolAirdropEpoch(ctx, pool, 1)
		require.NoError(t, err)
		require.True(t, record.Remaining.IsZero())
		require.Equal(t, record.EligibleSupply, record.ClaimedSupply)
		require.Equal(t, funded, aShare.Add(bShare).Add(traderShare))
		settled, found := k.GetMarket(ctx, market.Denom)
		require.True(t, found)
		require.True(t, settled.AirdropBalance.IsZero())
		held, err := k.GetQuoteLedger(ctx, launchpadkeeper.FeeHeldLedger(roles.FeeAccumulator))
		require.NoError(t, err)
		require.True(t, held.IsZero())
	})
}
