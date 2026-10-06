package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	launchpadtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	bridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	layerxgovtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxgov/types"
	appparams "github.com/Sidiora-Labs/Paxeer-X-Network/node/params"
	cdctypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/wasm/x/wasm"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

const (
	launchpadGovDirEnv    = "PAXEER_X_LAUNCHPAD_GOV_DIR"
	launchpadGovHomeEnv   = "PAXEER_X_LAUNCHPAD_GOV_HOME"
	launchpadGovParamsEnv = "PAXEER_X_LAUNCHPAD_GOV_PARAMS"
	launchpadGovMarketEnv = "PAXEER_X_LAUNCHPAD_GOV_MARKET"
)

func openLaunchpadGovApp(t *testing.T, home, dir string) *App {
	database, err := dbm.NewGoLevelDB("application", dir)
	require.NoError(t, err)
	return New(database, nil, true, map[int64]bool{}, home, 1, true, nil, MakeEncodingConfig(),
		wasm.EnableAllProposals, TestAppOpts{}, EmptyWasmOpts, nil)
}

func launchpadGovBlock(t *testing.T, a *App, at time.Time, body func(sdk.Context)) {
	height := a.LastBlockHeight() + 1
	_, err := a.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{ChainID: "pax-test", Height: height}})
	require.NoError(t, err)
	ctx := a.GetContextForDeliverTx([]byte{}).WithBlockTime(at).WithBlockHeight(height)
	body(ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx)).WithEventManager(sdk.NewEventManager()))
	a.SetDeliverStateToCommit()
	_, err = a.Commit(context.Background())
	require.NoError(t, err)
}

func launchpadGovFund(t *testing.T, a *App, ctx sdk.Context, name string, amount int64) sdk.AccAddress {
	acc := sdk.AccAddress(authtypes.NewModuleAddress(name))
	coins := sdk.NewCoins(sdk.NewCoin(launchpadtypes.DefaultQuoteDenom, sdk.NewInt(amount)))
	require.NoError(t, a.BankKeeper.MintCoins(ctx, "evm", coins))
	require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", acc, coins))
	return acc
}

func rawLayerXProposal(t *testing.T, msgs ...sdk.Msg) *layerxgovtypes.LayerXProposal {
	p := &layerxgovtypes.LayerXProposal{Title: "Launchpad parameters", Description: "Raw proposal content"}
	for _, msg := range msgs {
		packed, err := cdctypes.NewAnyWithValue(msg)
		require.NoError(t, err)
		p.Messages = append(p.Messages, packed)
	}
	return p
}

// TestLaunchpadGovernanceDispatch drives launchpad parameter changes through
// the registered x/gov LayerX proposal route of a durable application, checks
// every refusal leaves the committed parameters intact, and then reads the
// accepted parameters from an independent application process opened on the
// committed database.
func TestLaunchpadGovernanceDispatch(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	start := time.Unix(1_800_000_000, 0).UTC()
	a := openLaunchpadGovApp(t, home, dir)
	genesis, err := json.MarshalIndent(NewDefaultGenesisState(MakeEncodingConfig().Marshaler), "", " ")
	require.NoError(t, err)
	_, err = a.InitChain(context.Background(), &abci.RequestInitChain{Time: start, ConsensusParams: DefaultConsensusParams,
		ChainId: "pax-test", AppStateBytes: genesis})
	require.NoError(t, err)

	authority := layerxgovtypes.GovernanceAuthority()
	require.Equal(t, authority, a.LaunchpadKeeper.Authority())
	var market launchpadtypes.Market
	launchpadGovBlock(t, a, start, func(ctx sdk.Context) {
		creator := launchpadGovFund(t, a, ctx, "launchpad-gov-creator", 1_000_000_000)
		market, err = a.LaunchpadKeeper.CreateMarket(ctx, nil, creator, "Governed Token", "GOVT", launchpadtypes.FeeStrategyBurn)
		require.NoError(t, err)
	})

	updated := launchpadtypes.DefaultParams()
	updated.CreationFee = sdk.NewInt(250_000_000)
	updated.VirtualQuoteDefault = sdk.NewInt(20_000_000_000)
	updated.ProtocolFeeBps = 2_000
	updated.BaseFeeBps = 40
	require.NoError(t, updated.Validate())

	launchpadGovBlock(t, a, start.Add(time.Minute), func(ctx sdk.Context) {
		before := a.LaunchpadKeeper.GetParams(ctx)
		require.Equal(t, launchpadtypes.DefaultParams(), before)
		router := a.GovKeeper.Router()
		require.True(t, router.HasRoute(layerxgovtypes.RouterKey))
		handler := router.GetRoute(layerxgovtypes.RouterKey)
		msgRouter := a.MsgServiceRouter()
		require.NotNil(t, msgRouter.Handler(&launchpadtypes.MsgUpdateParams{}))
		unchanged := func() {
			require.Equal(t, before, a.LaunchpadKeeper.GetParams(ctx))
			got, found := a.LaunchpadKeeper.GetMarket(ctx, market.Denom)
			require.True(t, found)
			require.Equal(t, market, got)
			for _, event := range ctx.EventManager().Events() {
				require.NotEqual(t, launchpadtypes.EventTypeParamsUpdated, event.Type)
			}
		}

		// Ordinary account and forged authorities: refused by proposal
		// validation, by the registered message service and by the proposal
		// handler; no unsigned path reaches the keeper.
		ordinary := sdk.AccAddress(authtypes.NewModuleAddress("launchpad-gov-ordinary")).String()
		forged := []string{ordinary, authtypes.NewModuleAddress(launchpadtypes.ModuleName).String(), a.LaunchpadKeeper.ModuleAddress().String(), ""}
		for _, caller := range forged {
			msg := &launchpadtypes.MsgUpdateParams{Authority: caller, Params: updated}
			forgedProposal, err := layerxgovtypes.NewLayerXProposal("Forged", "Not the governance account", msg)
			require.NoError(t, err)
			require.Error(t, forgedProposal.ValidateBasic())
			_, err = msgRouter.Handler(msg)(ctx, msg)
			require.Error(t, err)
			require.Error(t, handler(ctx, rawLayerXProposal(t, msg)))
			unchanged()
		}
		_, err := msgRouter.Handler(&launchpadtypes.MsgUpdateParams{})(ctx, &launchpadtypes.MsgUpdateParams{Authority: ordinary, Params: updated})
		require.ErrorIs(t, err, launchpadtypes.ErrUnauthorized)

		// Invalid fee, cap and denom combinations from the governance authority.
		invalid := []func(*launchpadtypes.Params){
			func(p *launchpadtypes.Params) { p.MinFeeBps, p.MaxFeeBps = 301, 300 },
			func(p *launchpadtypes.Params) { p.BaseFeeBps = p.MaxFeeBps + 1 },
			func(p *launchpadtypes.Params) { p.MaxFeeBps = launchpadtypes.BpsDenominator },
			func(p *launchpadtypes.Params) { p.ProtocolFeeBps = launchpadtypes.MaxProtocolFeeBps + 1 },
			func(p *launchpadtypes.Params) { p.CreationFee = sdk.NewInt(-1) },
			func(p *launchpadtypes.Params) { p.VirtualQuoteDefault = sdk.ZeroInt() },
			func(p *launchpadtypes.Params) { p.QuoteDenom = "1!bad" },
		}
		for _, mutate := range invalid {
			params := updated
			mutate(&params)
			require.ErrorIs(t, params.Validate(), launchpadtypes.ErrInvalidParams)
			msg := &launchpadtypes.MsgUpdateParams{Authority: authority, Params: params}
			_, err := msgRouter.Handler(msg)(ctx, msg)
			require.Error(t, err)
			require.Error(t, handler(ctx, rawLayerXProposal(t, msg)))
			require.ErrorIs(t, a.LaunchpadKeeper.UpdateParams(ctx, authority, params), launchpadtypes.ErrInvalidParams)
			unchanged()
		}

		// Unregistered message: not a governance message, refused before dispatch.
		send := &banktypes.MsgSend{FromAddress: authority, ToAddress: ordinary, Amount: sdk.NewCoins(sdk.NewInt64Coin(launchpadtypes.DefaultQuoteDenom, 1))}
		_, err = layerxgovtypes.NewLayerXProposal("Unregistered", "Bank send", send)
		require.Error(t, err)
		require.Error(t, handler(ctx, rawLayerXProposal(t, &launchpadtypes.MsgUpdateParams{Authority: authority, Params: updated}, send)))
		unchanged()

		// Execution failure: the launchpad update precedes a failing message and
		// the whole proposal rolls back.
		failing := &bridgetypes.MsgSetCap{Authority: authority, ChainID: 999999, MaxInFlight: sdk.NewInt(20), MaxPerTx: sdk.NewInt(10)}
		p, err := layerxgovtypes.NewLayerXProposal("Atomic", "Launchpad then failing bridge cap",
			&launchpadtypes.MsgUpdateParams{Authority: authority, Params: updated}, failing)
		require.NoError(t, err)
		require.ErrorIs(t, handler(ctx, p), bridgetypes.ErrUnknownChain)
		unchanged()

		// A submitted proposal that never passes changes nothing.
		p, err = layerxgovtypes.NewLayerXProposal("Launchpad parameters", "Raise creation fee and protocol share",
			&launchpadtypes.MsgUpdateParams{Authority: authority, Params: updated})
		require.NoError(t, err)
		stored, err := a.GovKeeper.SubmitProposal(ctx, p)
		require.NoError(t, err)
		proposal, found := a.GovKeeper.GetProposal(ctx, stored.ProposalId)
		require.True(t, found)
		unchanged()

		// The accepted proposal's content executes through the gov router.
		require.NoError(t, handler(ctx, proposal.GetContent()))
		require.Equal(t, updated, a.LaunchpadKeeper.GetParams(ctx))
		emitted := 0
		for _, event := range ctx.EventManager().Events() {
			if event.Type == launchpadtypes.EventTypeParamsUpdated {
				emitted++
			}
		}
		require.Equal(t, 1, emitted)
		got, found := a.LaunchpadKeeper.GetMarket(ctx, market.Denom)
		require.True(t, found)
		require.Equal(t, market, got)
		require.Equal(t, uint64(1), a.LaunchpadKeeper.GetMarketCount(ctx))
	})

	launchpadGovBlock(t, a, start.Add(2*time.Minute), func(ctx sdk.Context) {
		require.Equal(t, updated, a.LaunchpadKeeper.GetParams(ctx))
		exported := a.LaunchpadKeeper.ExportGenesis(ctx)
		require.Equal(t, updated, exported.Params)
		// The v6.10 upgrade handler sets QuoteDenom through UpdateParams with
		// Keeper.Authority(); that call stays valid on governed parameters.
		upgrade, _ := ctx.CacheContext()
		v610 := a.LaunchpadKeeper.GetParams(upgrade)
		v610.QuoteDenom = appparams.BaseCoinUnit
		require.NoError(t, a.LaunchpadKeeper.UpdateParams(upgrade, a.LaunchpadKeeper.Authority(), v610))
		require.Equal(t, updated, a.LaunchpadKeeper.GetParams(ctx))
	})
	height := a.LastBlockHeight()
	require.NoError(t, a.Close())

	encodedParams, err := json.Marshal(updated)
	require.NoError(t, err)
	encodedMarket, err := json.Marshal(market)
	require.NoError(t, err)
	child := exec.Command(os.Args[0], "-test.run", "^TestLaunchpadGovernanceReopenedProcess$", "-test.count=1", "-test.v")
	child.Env = append(os.Environ(), launchpadGovDirEnv+"="+dir, launchpadGovHomeEnv+"="+home,
		launchpadGovParamsEnv+"="+string(encodedParams), launchpadGovMarketEnv+"="+string(encodedMarket))
	output, err := child.CombinedOutput()
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), "--- PASS: TestLaunchpadGovernanceReopenedProcess")

	reopened := openLaunchpadGovApp(t, home, dir)
	defer func() { require.NoError(t, reopened.Close()) }()
	require.Equal(t, height+1, reopened.LastBlockHeight(), "child process commits one block on the shared database")
	launchpadGovBlock(t, reopened, start.Add(time.Hour), func(ctx sdk.Context) {
		require.Equal(t, updated, reopened.LaunchpadKeeper.GetParams(ctx))
		require.Equal(t, uint64(2), reopened.LaunchpadKeeper.GetMarketCount(ctx))
	})
}

// TestLaunchpadGovernanceReopenedProcess is the independent application
// process TestLaunchpadGovernanceDispatch starts on its committed database.
func TestLaunchpadGovernanceReopenedProcess(t *testing.T) {
	dir := os.Getenv(launchpadGovDirEnv)
	if dir == "" {
		t.Skip("entered only as the child process of TestLaunchpadGovernanceDispatch")
	}
	var want launchpadtypes.Params
	require.NoError(t, json.Unmarshal([]byte(os.Getenv(launchpadGovParamsEnv)), &want))
	var market launchpadtypes.Market
	require.NoError(t, json.Unmarshal([]byte(os.Getenv(launchpadGovMarketEnv)), &market))
	a := openLaunchpadGovApp(t, os.Getenv(launchpadGovHomeEnv), dir)
	defer func() { require.NoError(t, a.Close()) }()
	require.Positive(t, a.LastBlockHeight(), strconv.FormatInt(a.LastBlockHeight(), 10))
	launchpadGovBlock(t, a, time.Unix(1_800_000_600, 0).UTC(), func(ctx sdk.Context) {
		require.Equal(t, want, a.LaunchpadKeeper.GetParams(ctx))
		got, found := a.LaunchpadKeeper.GetMarket(ctx, market.Denom)
		require.True(t, found)
		require.Equal(t, market, got)
		creator := launchpadGovFund(t, a, ctx, "launchpad-gov-reopened-creator", 1_000_000_000)
		balance := a.BankKeeper.GetBalance(ctx, creator, want.QuoteDenom).Amount
		created, err := a.LaunchpadKeeper.CreateMarket(ctx, nil, creator, "Reopened Token", "REOP", launchpadtypes.FeeStrategyBurn)
		require.NoError(t, err)
		require.Equal(t, want.CreationFee, balance.Sub(a.BankKeeper.GetBalance(ctx, creator, want.QuoteDenom).Amount))
		require.Equal(t, want.VirtualQuoteDefault, created.VirtualQuoteReserve)
		require.Equal(t, uint64(2), a.LaunchpadKeeper.GetMarketCount(ctx))
	})
}
