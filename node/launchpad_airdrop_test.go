package app

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	abci "github.com/sidiora-labs/paxeer-network/consensus/abci/types"
	tmproto "github.com/sidiora-labs/paxeer-network/consensus/proto/tendermint/types"
	launchpadtypes "github.com/sidiora-labs/paxeer-network/modules/launchpad/types"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
	authtypes "github.com/sidiora-labs/paxeer-network/sdk/x/auth/types"
	govtypes "github.com/sidiora-labs/paxeer-network/sdk/x/gov/types"
	"github.com/sidiora-labs/paxeer-network/wasm/x/wasm"
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
	fund := func(a *App, ctx sdk.Context, amount int64) sdk.AccAddress {
		acc := sdk.AccAddress(authtypes.NewModuleAddress("airdrop-holder-" + big.NewInt(amount).String() + "-" + big.NewInt(height).String()))
		if amount > 0 {
			coins := sdk.NewCoins(sdk.NewCoin(launchpadtypes.DefaultQuoteDenom, sdk.NewInt(amount)))
			require.NoError(t, a.BankKeeper.MintCoins(ctx, "evm", coins))
			require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", acc, coins))
		}
		return acc
	}

	a, _ := open()
	genesis, err := json.MarshalIndent(NewDefaultGenesisState(MakeEncodingConfig().Marshaler), "", " ")
	require.NoError(t, err)
	_, err = a.InitChain(context.Background(), &abci.RequestInitChain{Time: start, ConsensusParams: DefaultConsensusParams,
		ChainId: "pax-test", AppStateBytes: genesis})
	require.NoError(t, err)

	var creator, trader, holderA, holderB, holderC sdk.AccAddress
	var denom string
	block(a, start, func(ctx sdk.Context) {
		params := launchpadtypes.DefaultParams()
		params.VirtualQuoteDefault = sdk.NewInt(1_000_000_000)
		params.VirtualTokenDefault = sdk.NewInt(1_000_000_000)
		require.NoError(t, a.LaunchpadKeeper.UpdateParams(ctx, authtypes.NewModuleAddress(govtypes.ModuleName).String(), params))
		creator = fund(a, ctx, 100_000_000)
		trader = fund(a, ctx, 40_000_000_000)
		holderA, holderB, holderC = fund(a, ctx, 1), fund(a, ctx, 2), fund(a, ctx, 3)
		market, err := a.LaunchpadKeeper.CreateMarket(ctx, nil, creator, "Kindle Token", "KNDL", launchpadtypes.FeeStrategyAirdrop)
		require.NoError(t, err)
		denom = market.Denom
	})

	mature := start.Add(1000 * time.Hour)
	var funded, aShare, wantB, wantTrader sdk.Int
	var exported []byte
	block(a, mature, func(ctx sdk.Context) {
		far := uint64(mature.Add(time.Hour).Unix())
		_, err := a.LaunchpadKeeper.Swap(ctx, trader, denom, true, big.NewInt(1_000_000_000), big.NewInt(0), trader, far)
		require.NoError(t, err)
		send := func(from, to sdk.AccAddress, amount int64) {
			require.NoError(t, a.BankKeeper.SendCoins(ctx, from, to, sdk.NewCoins(sdk.NewCoin(denom, sdk.NewInt(amount)))))
		}
		send(trader, holderA, 600_000)
		send(trader, holderB, 400_000)
		funded, err = a.LaunchpadKeeper.ExecuteAirdrop(ctx, creator, denom)
		require.NoError(t, err)
		aShare, err = a.LaunchpadKeeper.ClaimAirdrop(ctx, holderA, denom)
		require.NoError(t, err)
		send(holderA, holderC, 600_000)
		wantB, _, err = a.LaunchpadKeeper.AirdropEntitlement(ctx, holderB, denom, 1)
		require.NoError(t, err)
		wantTrader, _, err = a.LaunchpadKeeper.AirdropEntitlement(ctx, trader, denom, 1)
		require.NoError(t, err)
		exported, err = json.Marshal(a.LaunchpadKeeper.ExportGenesis(ctx))
		require.NoError(t, err)
	})
	// Closing the application closes its database.
	require.NoError(t, a.Close())

	reopened, _ := open()
	defer func() { require.NoError(t, reopened.Close()) }()
	require.Equal(t, height, reopened.LastBlockHeight())
	block(reopened, mature.Add(time.Minute), func(ctx sdk.Context) {
		k := reopened.LaunchpadKeeper
		gs := k.ExportGenesis(ctx)
		require.NoError(t, gs.Validate())
		got, err := json.Marshal(gs)
		require.NoError(t, err)
		require.JSONEq(t, string(exported), string(got))
		basis, found, err := k.GetAirdropBasis(ctx, denom, 1)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, aShare, basis.Paid)
		require.True(t, k.HasClaimedAirdrop(ctx, denom, holderA, 1))

		_, err = k.ClaimAirdrop(ctx, holderC, denom)
		require.ErrorIs(t, err, launchpadtypes.ErrZeroAmount)
		_, err = k.ClaimAirdrop(ctx, holderA, denom)
		require.ErrorIs(t, err, launchpadtypes.ErrAlreadyClaimed)
		before := reopened.BankKeeper.GetBalance(ctx, holderB, launchpadtypes.DefaultQuoteDenom).Amount
		bShare, err := k.ClaimAirdrop(ctx, holderB, denom)
		require.NoError(t, err)
		require.Equal(t, wantB, bShare)
		require.Equal(t, before.Add(bShare), reopened.BankKeeper.GetBalance(ctx, holderB, launchpadtypes.DefaultQuoteDenom).Amount)
		_, err = k.ClaimAirdrop(ctx, holderB, denom)
		require.ErrorIs(t, err, launchpadtypes.ErrAlreadyClaimed)
		traderShare, err := k.ClaimAirdropForEpoch(ctx, trader, denom, 1)
		require.NoError(t, err)
		require.Equal(t, wantTrader, traderShare)
		basis, _, err = k.GetAirdropBasis(ctx, denom, 1)
		require.NoError(t, err)
		require.Equal(t, aShare.Add(bShare).Add(traderShare), basis.Paid)
		require.True(t, basis.Paid.LTE(funded))
		market, found := k.GetMarket(ctx, denom)
		require.True(t, found)
		require.Equal(t, funded.Sub(basis.Paid), market.AirdropBalance)
	})
}
