package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/config"
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types/ethtx"
	launchpadtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	layerxbridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	layerxexchangetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxexchange/types"
	oracletypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/oracle/types"
	perpskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	perpstypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	spotkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	spottypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/legacyabci"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/prefix"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/version"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	upgradetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// v6120HookNames are the hooks of the v6.12.0 plan in the order it runs them.
var v6120HookNames = []string{"pointers", "launchpad", "pricefeed", "perps", "spot"}

// bindNilV6120Hooks binds every hook of the v6.12.0 plan to one that returns
// nil, so the handler runs on a test application without the venue state.
func bindNilV6120Hooks(a *App) {
	a.v6120HookTable = make([]v6120Hook, 0, len(v6120HookNames))
	for _, name := range v6120HookNames {
		a.v6120HookTable = append(a.v6120HookTable, v6120Hook{name: name, run: func(sdk.Context) error { return nil }})
	}
}

// writeV6120UpgradeInfo writes the upgrade info file an operator leaves for the
// v6.12.0 plan, through the keeper that writes it and into the home the
// application reads, so the begin blocker reads exactly what a node reads.
func writeV6120UpgradeInfo(t *testing.T, a *App, height int64) {
	t.Helper()
	require.NoError(t, a.UpgradeKeeper.DumpUpgradeInfoToDisk(height, V6120Upgrade))
	info, err := a.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	require.NoError(t, err)
	require.Equal(t, V6120Upgrade, info.Name)
	require.Equal(t, height, info.Height)
}

func TestV6120IsAKnownPlanWithAHandler(t *testing.T) {
	require.Equal(t, "v6.12.0", V6120Upgrade)
	require.Equal(t, []string{ActivationUpgrade, V610Upgrade, V6120Upgrade, MergeUpgrade}, knownUpgradePlans())
	a := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), false).App
	require.True(t, a.UpgradeKeeper.HasHandler("v6.12.0"))
	require.False(t, a.UpgradeKeeper.HasHandler("v6.11"))
}

func TestV6120RefusesAnUnboundHook(t *testing.T) {
	w := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), false)
	a, ctx := w.App, w.Ctx
	a.v6120HookTable = nil
	names := make([]string, 0, 5)
	for _, hook := range a.v6120Hooks() {
		require.Nil(t, hook.run, hook.name)
		names = append(names, hook.name)
	}
	require.Equal(t, []string{"pointers", "launchpad", "pricefeed", "perps", "spot"}, names)

	versions := a.UpgradeKeeper.GetModuleVersionMap(ctx)
	adminBefore, err := a.TokenFactoryKeeper.GetAuthorityMetadata(ctx, layerxbridgetypes.SidioraDenom())
	require.NoError(t, err)
	migratedBefore := a.EvmKeeper.GetState(ctx, SidioraProxyAddress, sidMigratedHeightSlot())
	require.Equal(t, common.Hash{}, migratedBefore)
	newVM, err := a.runV6120Upgrade(ctx, upgradetypes.Plan{Name: V6120Upgrade}, versions)
	require.EqualError(t, err, "upgrade v6.12.0: hook pointers is not bound")
	require.Nil(t, newVM)

	// With every hook but perps bound, the handler names perps and runs none.
	ran := make([]string, 0, 5)
	a.v6120HookTable = make([]v6120Hook, 0, len(names))
	for _, name := range names {
		hook := v6120Hook{name: name}
		if name != "perps" {
			hook.run = func(sdk.Context) error {
				ran = append(ran, name)
				return nil
			}
		}
		a.v6120HookTable = append(a.v6120HookTable, hook)
	}
	newVM, err = a.runV6120Upgrade(ctx, upgradetypes.Plan{Name: V6120Upgrade}, versions)
	require.EqualError(t, err, "upgrade v6.12.0: hook perps is not bound")
	require.Nil(t, newVM)
	require.Empty(t, ran)

	// No state was written: the version map, the usid authority and the SID
	// migration record are as the handler found them.
	require.Equal(t, versions, a.UpgradeKeeper.GetModuleVersionMap(ctx))
	admin, err := a.TokenFactoryKeeper.GetAuthorityMetadata(ctx, layerxbridgetypes.SidioraDenom())
	require.NoError(t, err)
	require.Equal(t, adminBefore, admin)
	require.Equal(t, migratedBefore, a.EvmKeeper.GetState(ctx, SidioraProxyAddress, sidMigratedHeightSlot()))
}

func TestV6120RunsTheHooksInOrder(t *testing.T) {
	w := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), false)
	a, ctx := w.App, w.Ctx
	ran := make([]string, 0, len(v6120HookNames))
	a.v6120HookTable = make([]v6120Hook, 0, len(v6120HookNames))
	for _, name := range v6120HookNames {
		a.v6120HookTable = append(a.v6120HookTable, v6120Hook{name: name, run: func(hookCtx sdk.Context) error {
			// Every hook runs after the folded SID work.
			require.Equal(t, common.BigToHash(big.NewInt(hookCtx.BlockHeight())),
				a.EvmKeeper.GetState(hookCtx, SidioraProxyAddress, sidMigratedHeightSlot()), name)
			ran = append(ran, name)
			return nil
		}})
	}
	versions := a.UpgradeKeeper.GetModuleVersionMap(ctx)

	newVM, err := a.runV6120Upgrade(ctx, upgradetypes.Plan{Name: V6120Upgrade}, versions)
	require.NoError(t, err)
	require.Equal(t, versions, newVM)
	require.Equal(t, []string{"pointers", "launchpad", "pricefeed", "perps", "spot"}, ran)
	admin, err := a.TokenFactoryKeeper.GetAuthorityMetadata(ctx, layerxbridgetypes.SidioraDenom())
	require.NoError(t, err)
	require.Equal(t, layerxbridgetypes.ModuleAddress().String(), admin.Admin)
}

func TestV6120StopsAtTheFirstHookError(t *testing.T) {
	w := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), false)
	a, ctx := w.App, w.Ctx
	ran := make([]string, 0, len(v6120HookNames))
	a.v6120HookTable = make([]v6120Hook, 0, len(v6120HookNames))
	for _, name := range v6120HookNames {
		a.v6120HookTable = append(a.v6120HookTable, v6120Hook{name: name, run: func(sdk.Context) error {
			ran = append(ran, name)
			if name == "perps" {
				return errors.New("market table empty")
			}
			return nil
		}})
	}

	cached, _ := ctx.CacheContext()
	newVM, err := a.runV6120Upgrade(cached, upgradetypes.Plan{Name: V6120Upgrade}, a.UpgradeKeeper.GetModuleVersionMap(cached))
	require.EqualError(t, err, "upgrade v6.12.0: perps: market table empty")
	require.Nil(t, newVM)
	require.Equal(t, []string{"pointers", "launchpad", "pricefeed", "perps"}, ran)

	// Through the upgrade keeper the error aborts the block and no done height
	// is recorded.
	ran = ran[:0]
	plan := upgradetypes.Plan{Name: V6120Upgrade, Height: ctx.BlockHeight()}
	applied, _ := ctx.CacheContext()
	require.PanicsWithError(t, "upgrade v6.12.0: perps: market table empty", func() {
		a.UpgradeKeeper.ApplyUpgrade(applied, plan)
	})
	require.Equal(t, []string{"pointers", "launchpad", "pricefeed", "perps"}, ran)
	require.Zero(t, a.UpgradeKeeper.GetDoneHeight(applied, V6120Upgrade))
	require.Zero(t, a.UpgradeKeeper.GetDoneHeight(ctx, V6120Upgrade))
}

func TestV6120SkipsAMigratedChainAndZeroSlots(t *testing.T) {
	holders, _, err := parseSIDHolders(sidHoldersJSON)
	require.NoError(t, err)
	require.Len(t, holders, 3)
	denom := layerxbridgetypes.SidioraDenom()
	usid := func(a *App, ctx sdk.Context, holder common.Address) sdk.Int {
		return a.BankKeeper.GetBalance(ctx, a.EvmKeeper.GetPaxAddressOrDefault(ctx, holder), denom).Amount
	}
	seed := func(a *App, ctx sdk.Context, slot common.Hash, amount int64) {
		a.EvmKeeper.SetState(ctx, SidioraProxyAddress, slot, common.BigToHash(big.NewInt(amount)))
	}

	// A listed holder whose slot reads zero is minted nothing; the others are
	// minted their slot balances.
	w := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), false)
	a, ctx := w.App, w.Ctx
	bindNilV6120Hooks(a)
	seed(a, ctx, sidLegacyBalanceSlot(holders[1]), 1_500_000)
	seed(a, ctx, sidLegacyBalanceSlot(holders[2]), 500_000)
	seed(a, ctx, sidLegacyTotalSupplySlot(), 2_000_000)
	_, err = a.runV6120Upgrade(ctx, upgradetypes.Plan{Name: V6120Upgrade}, a.UpgradeKeeper.GetModuleVersionMap(ctx))
	require.NoError(t, err)
	require.True(t, usid(a, ctx, holders[0]).IsZero())
	require.Equal(t, sdk.NewInt(1_500_000), usid(a, ctx, holders[1]))
	require.Equal(t, sdk.NewInt(500_000), usid(a, ctx, holders[2]))
	require.Equal(t, sdk.NewInt(2_000_000), a.BankKeeper.GetSupply(ctx, denom).Amount)
	require.Equal(t, common.Hash{}, a.EvmKeeper.GetState(ctx, SidioraProxyAddress, sidShortfallSlot()))
	require.Equal(t, common.BigToHash(big.NewInt(ctx.BlockHeight())),
		a.EvmKeeper.GetState(ctx, SidioraProxyAddress, sidMigratedHeightSlot()))

	// A chain whose migration marker is already set mints nothing and leaves
	// every legacy slot as it found it.
	w = NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), false)
	a, ctx = w.App, w.Ctx
	bindNilV6120Hooks(a)
	seed(a, ctx, sidMigratedHeightSlot(), 7)
	seed(a, ctx, sidLegacyBalanceSlot(holders[0]), 2_000_000)
	seed(a, ctx, sidLegacyTotalSupplySlot(), 2_000_000)
	_, err = a.runV6120Upgrade(ctx, upgradetypes.Plan{Name: V6120Upgrade}, a.UpgradeKeeper.GetModuleVersionMap(ctx))
	require.NoError(t, err)
	for _, holder := range holders {
		require.True(t, usid(a, ctx, holder).IsZero(), holder.Hex())
	}
	require.True(t, a.BankKeeper.GetSupply(ctx, denom).Amount.IsZero())
	require.Equal(t, common.BigToHash(big.NewInt(2_000_000)),
		a.EvmKeeper.GetState(ctx, SidioraProxyAddress, sidLegacyBalanceSlot(holders[0])))
	require.Equal(t, common.BigToHash(big.NewInt(7)), a.EvmKeeper.GetState(ctx, SidioraProxyAddress, sidMigratedHeightSlot()))
	require.Equal(t, common.Hash{}, a.EvmKeeper.GetState(ctx, SidioraProxyAddress, sidShortfallSlot()))
}

func TestV6120BinaryVersion(t *testing.T) {
	built := version.Version
	version.Version = "v6.12.0"
	defer func() { version.Version = built }()

	a := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), false).App
	require.Equal(t, "v6.12.0", a.Version())
	require.Equal(t, version.Version, a.Version())
}

func TestV6120UpgradeInfoShape(t *testing.T) {
	a := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), false).App

	writeV6120UpgradeInfo(t, a, 41)

	const halt = int64(30_500_000)
	writeV6120UpgradeInfo(t, a, halt+1)
	path, err := a.UpgradeKeeper.GetUpgradeInfoPath()
	require.NoError(t, err)
	require.Equal(t, "upgrade-info.json", filepath.Base(path))
	stat, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), stat.Mode().Perm())

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, `{"name":"v6.12.0","height":30500001}`, string(raw))
	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.Equal(t, map[string]any{"name": "v6.12.0", "height": float64(halt + 1)}, doc)
}

func TestV6120IsAppliedOnlyInTheNamedBlock(t *testing.T) {
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := NewTestWrapper(t, time.Now().UTC(), valPub, true)
	a, ctx := testWrapper.App, testWrapper.Ctx
	bindNilV6120Hooks(a)

	const height = int64(42)
	writeV6120UpgradeInfo(t, a, height+1)

	at := ctx.WithBlockHeight(height)
	recordPreviousProposer(a, at, valPub)
	a.BeginBlock(at, height, nil, nil, false)
	require.Zero(t, a.UpgradeKeeper.GetDoneHeight(at, V6120Upgrade))

	next := ctx.WithBlockHeight(height + 1)
	recordPreviousProposer(a, next, valPub)
	a.BeginBlock(next, height+1, nil, nil, false)
	require.Equal(t, height+1, a.UpgradeKeeper.GetDoneHeight(next, V6120Upgrade))
	_, found := a.UpgradeKeeper.GetUpgradePlan(next)
	require.False(t, found)

	// With no document on disk the plan is not applied in the same block.
	valPub = secp256k1.GenPrivKey().PubKey()
	bare := NewTestWrapper(t, time.Now().UTC(), valPub, true)
	bindNilV6120Hooks(bare.App)
	info, err := bare.App.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	require.NoError(t, err)
	require.Empty(t, info.Name)
	unnamed := bare.Ctx.WithBlockHeight(height + 1)
	recordPreviousProposer(bare.App, unnamed, valPub)
	bare.App.BeginBlock(unnamed, height+1, nil, nil, false)
	require.Zero(t, bare.App.UpgradeKeeper.GetDoneHeight(unnamed, V6120Upgrade))
}

func TestV6120ModulesAreWired(t *testing.T) {
	a := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), true).App
	added := []string{perpstypes.ModuleName, spottypes.ModuleName, pricefeedtypes.ModuleName}
	for _, name := range added {
		require.Contains(t, ModuleBasics, name)
		require.Contains(t, kvStoreKeyNames, name)
		require.NotNil(t, a.GetKey(name), name)
		require.Contains(t, a.mm.Modules, name)
		require.Contains(t, a.mm.OrderInitGenesis, name)
	}
	launchpadAt := -1
	for i, name := range a.mm.OrderInitGenesis {
		if name == launchpadtypes.ModuleName {
			launchpadAt = i
		}
	}
	require.Equal(t, added, a.mm.OrderInitGenesis[launchpadAt+1:launchpadAt+4])
	require.Equal(t, []string{oracletypes.ModuleName, pricefeedtypes.ModuleName}, a.mm.OrderMidBlockers)

	require.NotNil(t, a.PerpsKeeper)
	require.NotNil(t, a.SpotKeeper)
	require.Same(t, a.PerpsKeeper, a.EndBlockKeepers.PerpsKeeper)
	require.Same(t, a.SpotKeeper, a.EndBlockKeepers.SpotKeeper)
	require.NotNil(t, a.EndBlockKeepers.V6120Live)
	require.False(t, a.EndBlockKeepers.V6120Live())
	a.v6120Live = true
	require.True(t, a.EndBlockKeepers.V6120Live())
	a.v6120Live = false
	require.Equal(t, a.PricefeedKeeper, a.CheckTxKeepers.PricefeedKeeper)
	require.Equal(t, a.PricefeedKeeper, a.DeliverTxKeepers.PricefeedKeeper)

	upgrades, ok := layerxStoreUpgrades(V6120Upgrade)
	require.True(t, ok)
	require.Equal(t, v6120StoreUpgrades(), upgrades)

	names := make([]string, 0, len(v6120HookNames))
	for _, hook := range a.v6120Hooks() {
		require.NotNil(t, hook.run, hook.name)
		names = append(names, hook.name)
	}
	require.Equal(t, v6120HookNames, names)
}

func TestV6120ServesTheNewSetFromTheHandler(t *testing.T) {
	w := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), true)
	a, ctx := w.App, w.Ctx
	require.False(t, a.v6120Live)
	require.Len(t, a.v6120Next, 2+len(v6120TestGated))
	before := a.EvmKeeper.CustomPrecompiles(ctx)
	for _, addr := range v6120TestGated {
		require.NotContains(t, before, addr)
	}
	require.Same(t, a.v6120Next[v6120TestExchange][1], before[v6120TestExchange])
	require.Same(t, a.v6120Next[v6120TestLaunchpad][1], before[v6120TestLaunchpad])

	// The handler serves the set before the upgrade keeper records the plan.
	bindNilV6120Hooks(a)
	_, err := a.runV6120Upgrade(ctx, upgradetypes.Plan{Name: V6120Upgrade}, a.UpgradeKeeper.GetModuleVersionMap(ctx))
	require.NoError(t, err)
	require.Zero(t, a.UpgradeKeeper.GetDoneHeight(ctx, V6120Upgrade))
	require.True(t, a.v6120Live)
	served := a.EvmKeeper.CustomPrecompiles(ctx)
	for _, addr := range v6120TestGated {
		require.Same(t, a.v6120Next[addr][0], served[addr], addr.Hex())
	}
	require.Same(t, a.v6120Next[v6120TestExchange][0], served[v6120TestExchange])
	require.Same(t, a.v6120Next[v6120TestLaunchpad][0], served[v6120TestLaunchpad])
	for _, addr := range v6120TestCodeless {
		require.NotContains(t, served, addr)
	}

	// An application loaded over the state with the plan done serves an equal set.
	a.UpgradeKeeper.SetDone(ctx, V6120Upgrade)
	versions := a.EvmKeeper.GetCustomPrecompilesVersions(ctx)
	loaded := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), true)
	b, bctx := loaded.App, loaded.Ctx.WithBlockHeight(ctx.BlockHeight())
	b.UpgradeKeeper.SetDone(bctx, V6120Upgrade)
	require.False(t, b.v6120Live)
	b.RefreshPrecompiles(bctx)
	require.True(t, b.v6120Live)
	require.Equal(t, versions, b.EvmKeeper.GetCustomPrecompilesVersions(bctx))
	reloaded := b.EvmKeeper.CustomPrecompiles(bctx)
	require.Len(t, reloaded, len(a.EvmKeeper.CustomPrecompiles(ctx)))
	for addr := range a.EvmKeeper.CustomPrecompiles(ctx) {
		require.Contains(t, reloaded, addr)
	}
	require.Same(t, b.v6120Next[v6120TestExchange][0], reloaded[v6120TestExchange])
}

func TestV6120ModuleAccounts(t *testing.T) {
	w := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), true)
	a, ctx := w.App, w.Ctx
	require.Equal(t, []string{authtypes.Minter, authtypes.Burner}, maccPerms[spottypes.ModuleName])
	for _, name := range []string{perpstypes.ModuleName, launchpadtypes.ModuleName, pricefeedtypes.ModuleName} {
		require.NotContains(t, maccPerms, name)
	}
	spotAddr := authtypes.NewModuleAddress(spottypes.ModuleName)
	require.True(t, allowedReceivingModAcc[spottypes.ModuleName])
	require.False(t, a.BlacklistedAccAddrs()[spotAddr.String()])

	// A user sends uhpx to the spot module address.
	user := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	coins := sdk.NewCoins(sdk.NewCoin(evmkeeper.BaseDenom, sdk.NewInt(1_000_000)))
	require.NoError(t, a.BankKeeper.MintCoins(ctx, "mint", coins))
	require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", user, coins))
	require.NoError(t, a.BankKeeper.SendCoins(ctx, user, spotAddr, coins))
	require.Equal(t, coins, a.BankKeeper.GetAllBalances(ctx, spotAddr))

	// The spot keeper mints a share denom through the spot module account and
	// refuses a uhpx mint.
	token := common.HexToAddress("0x00000000000000000000000000000000000a11ce")
	holder := common.HexToAddress("0x00000000000000000000000000000000000b0b00")
	denom := a.SpotKeeper.ShareDenom(token)
	require.Equal(t, spotkeeper.ShareDenomPrefix+"0x00000000000000000000000000000000000a11ce", denom)
	err := a.EvmKeeper.RunWithOneOffEVMInstance(ctx, func(evm *vm.EVM) error {
		if _, err := a.SpotKeeper.CreateShareToken(ctx, evm, token, "TKN", 6); err != nil {
			return err
		}
		if err := a.SpotKeeper.MintShares(ctx, evm, denom, holder, sdk.NewInt(5)); err != nil {
			return err
		}
		require.ErrorIs(t, a.SpotKeeper.MintShares(ctx, evm, evmkeeper.BaseDenom, holder, sdk.NewInt(5)),
			spottypes.ErrUnsupportedToken)
		return nil
	}, func(string, string) {})
	require.NoError(t, err)
	require.Equal(t, sdk.NewInt(5), a.BankKeeper.GetSupply(ctx, denom).Amount)
	require.Equal(t, sdk.NewInt(5),
		a.BankKeeper.GetBalance(ctx, a.EvmKeeper.GetPaxAddressOrDefault(ctx, holder), denom).Amount)
	require.Equal(t, coins, a.BankKeeper.GetAllBalances(ctx, spotAddr))
}

// perpsEndBlockErrors counts the perps end block error events of ctx.
func perpsEndBlockErrors(ctx sdk.Context) int {
	n := 0
	for _, event := range ctx.EventManager().Events() {
		if event.Type == "perps_endblock_error" {
			n++
		}
	}
	return n
}

func TestV6120EndBlockOrder(t *testing.T) {
	w := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), true)
	a := w.App
	height := w.Ctx.BlockHeight()

	// Without perps params a perps end block emits a setup error event, which
	// shows whether the venue was called.
	ctx, _ := w.Ctx.CacheContext()
	ctx.KVStore(a.GetKey(perpstypes.StoreKey)).Delete(perpstypes.ParamsKey)

	// Before the plan neither venue runs and nothing panics.
	require.False(t, a.v6120Live)
	dormant := ctx.WithEventManager(sdk.NewEventManager())
	require.NotPanics(t, func() { legacyabci.EndBlock(dormant, height, 0, a.EndBlockKeepers) })
	require.Zero(t, perpsEndBlockErrors(dormant))

	// Once live the perps pass runs before the spot pass and the EVM end block.
	a.v6120Live = true
	live := ctx.WithEventManager(sdk.NewEventManager())
	require.NotPanics(t, func() { legacyabci.EndBlock(live, height, 0, a.EndBlockKeepers) })
	require.Equal(t, 1, perpsEndBlockErrors(live))
}

// seedGigaV6120FromExport is seedV6120FromExport over an application that
// runs the giga executor.
func seedGigaV6120FromExport(t *testing.T, path string) *TestWrapper {
	t.Helper()
	doc := readV6120HaltExport(t, path)
	halt := v6120HaltHeight(t, path)
	heights := readV6120DoneHeights(t, path)

	w := NewGigaTestWrapper(t, doc.GenesisTime, secp256k1.GenPrivKey().PubKey(), true, false)
	a := w.App
	params := doc.ConsensusParams.ToProto()
	_, err := a.InitChain(context.Background(), &abci.RequestInitChain{
		Time:            doc.GenesisTime,
		ChainId:         doc.ChainID,
		ConsensusParams: &params,
		AppStateBytes:   doc.AppState,
		InitialHeight:   doc.InitialHeight,
	})
	require.NoError(t, err)
	w.Ctx = a.NewContext(false, tmproto.Header{Height: halt, ChainID: doc.ChainID, Time: doc.GenesisTime})
	for _, name := range []string{ActivationUpgrade, V610Upgrade} {
		require.LessOrEqual(t, heights[name], halt, name)
		a.UpgradeKeeper.SetDone(w.Ctx.WithBlockHeight(heights[name]), name)
	}
	return w
}

func TestV6120AppliesThePlanOnTheHaltExport(t *testing.T) {
	w := seedGigaV6120FromExport(t, v6120HaltExportPath)
	a, ctx := w.App, w.Ctx
	halt := v6120HaltHeight(t, v6120HaltExportPath)
	require.Equal(t, halt, ctx.BlockHeight())
	require.False(t, a.v6120Live)

	valPub := secp256k1.GenPrivKey().PubKey()
	writeV6120UpgradeInfo(t, a, halt+1)
	next := ctx.WithBlockHeight(halt + 1)
	recordPreviousProposer(a, next, valPub)
	a.BeginBlock(next, halt+1, nil, nil, false)
	require.Equal(t, halt+1, a.UpgradeKeeper.GetDoneHeight(next, V6120Upgrade))
	require.True(t, a.v6120Live)

	// Hook effects: native pointers, price feed params, perps markets and spot tokens.
	for _, denom := range []string{evmkeeper.BaseDenom, layerxbridgetypes.SidioraDenom()} {
		_, _, exists := a.EvmKeeper.GetERC20NativePointer(next, denom)
		require.True(t, exists, denom)
	}
	require.Equal(t, pricefeedtypes.DefaultParams(pricefeedtypes.DefaultAuthority()), a.PricefeedKeeper.GetParams(next))
	markets := 0
	require.NoError(t, a.PerpsKeeper.IterateMarkets(next, func(perpstypes.Market) bool {
		markets++
		return false
	}))
	require.Equal(t, 18, markets)
	spotParams, err := a.SpotKeeper.GetParams(next)
	require.NoError(t, err)
	require.Len(t, spotParams.Tokens, 3)

	// The usid mint authority is the bridge module account.
	admin, err := a.TokenFactoryKeeper.GetAuthorityMetadata(next, layerxbridgetypes.SidioraDenom())
	require.NoError(t, err)
	require.Equal(t, layerxbridgetypes.ModuleAddress().String(), admin.Admin)

	// 0x1015 serves perps and 0x1017 the second-generation launchpad from the
	// plan height; 0x1020-0x1025 are served and 0x1026, 0x1027 carry no code.
	versions := a.EvmKeeper.GetCustomPrecompilesVersions(next)
	require.Equal(t, V6120Upgrade, versions[v6120TestExchange])
	require.Equal(t, V6120Upgrade, versions[v6120TestLaunchpad])
	served := a.EvmKeeper.CustomPrecompiles(next)
	require.Same(t, a.v6120Next[v6120TestExchange][0], served[v6120TestExchange])
	require.Same(t, a.v6120Next[v6120TestLaunchpad][0], served[v6120TestLaunchpad])
	for _, addr := range v6120TestGated {
		require.Same(t, a.v6120Next[addr][0], served[addr], addr.Hex())
	}
	for _, addr := range v6120TestCodeless {
		require.NotContains(t, served, addr)
		require.Empty(t, a.EvmKeeper.GetCode(next, addr), addr.Hex())
	}

	// A call to 0x1015 at H+2 through the giga block path returns the perps result.
	after := ctx.WithBlockHeight(halt + 2).WithBlockTime(ctx.BlockTime().Add(time.Second))
	privKey := secp256k1.GenPrivKey()
	key, err := crypto.HexToECDSA(hex.EncodeToString(privKey.Bytes()))
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	senderPax := sdk.AccAddress(privKey.PubKey().Address())
	a.EvmKeeper.SetAddressMapping(after, senderPax, sender)
	funds := sdk.NewCoins(sdk.NewCoin(evmkeeper.BaseDenom, sdk.NewInt(1_000_000_000_000)))
	require.NoError(t, a.BankKeeper.MintCoins(after, "mint", funds))
	require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(after, "mint", senderPax, funds))

	input := crypto.Keccak256([]byte("getActiveMarketIds()"))[:4]
	exchange := common.HexToAddress(layerxexchangetypes.ExchangeAddress)
	want, err := a.EvmKeeper.StaticCallEVM(after, sender, &exchange, input)
	require.NoError(t, err)
	require.NotEmpty(t, want)

	signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    a.EvmKeeper.GetNonce(after, sender),
		GasPrice: big.NewInt(500_000_000_000),
		Gas:      1_000_000,
		To:       &exchange,
		Value:    big.NewInt(0),
		Data:     input,
	}), ethtypes.LatestSignerForChainID(big.NewInt(config.DefaultChainID)), key)
	require.NoError(t, err)
	txData, err := ethtx.NewTxDataFromTx(signed)
	require.NoError(t, err)
	msg, err := evmtypes.NewMsgEVMTransaction(txData)
	require.NoError(t, err)
	builder := a.GetTxConfig().NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	builder.SetGasLimit(1_000_000)
	bz, err := a.GetTxConfig().TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	_, results, _, err := a.ProcessBlock(after, [][]byte{bz}, &BlockProcessRequest{
		Height: halt + 2,
		Time:   after.BlockTime(),
	}, abci.CommitInfo{}, false, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, uint32(0), results[0].Code, results[0].Log)
	var msgData sdk.TxMsgData
	require.NoError(t, msgData.Unmarshal(results[0].Data))
	require.Len(t, msgData.Data, 1)
	var resp evmtypes.MsgEVMTransactionResponse
	require.NoError(t, resp.Unmarshal(msgData.Data[0].Data))
	require.Equal(t, want, resp.ReturnData)
}

func TestV6120InitialisesTheNewModulesOnAPreUpgradeChain(t *testing.T) {
	w := seedV6120FromExport(t, v6120HaltExportPath)
	a, ctx := w.App, w.Ctx
	halt := v6120HaltHeight(t, v6120HaltExportPath)

	// A chain built before v6.12.0 records no version for the modules the plan
	// adds, and their stores are empty.
	versionStore := prefix.NewStore(ctx.KVStore(a.GetKey(upgradetypes.StoreKey)), []byte{upgradetypes.VersionMapByte})
	stores := map[string][]byte{
		pricefeedtypes.ModuleName: pricefeedtypes.ParamsKey,
		perpstypes.ModuleName:     perpstypes.ParamsKey,
		spottypes.ModuleName:      spottypes.ParamsKey,
	}
	for _, name := range v6120NewModules {
		versionStore.Delete([]byte(name))
		require.False(t, ctx.KVStore(a.GetKey(name)).Has(stores[name]), name)
	}
	before := a.UpgradeKeeper.GetModuleVersionMap(ctx)
	for _, name := range v6120NewModules {
		require.NotContains(t, before, name)
	}

	valPub := secp256k1.GenPrivKey().PubKey()
	writeV6120UpgradeInfo(t, a, halt+1)
	next := ctx.WithBlockHeight(halt + 1)
	recordPreviousProposer(a, next, valPub)
	a.BeginBlock(next, halt+1, nil, nil, false)
	require.Equal(t, halt+1, a.UpgradeKeeper.GetDoneHeight(next, V6120Upgrade))
	require.True(t, a.v6120Live)

	after := a.UpgradeKeeper.GetModuleVersionMap(next)
	for _, name := range v6120NewModules {
		require.Equal(t, uint64(1), after[name], name)
	}
	for name, version := range before {
		require.Equal(t, version, after[name], name)
	}

	// The hooks wrote the v6.12.0 initial state and InitGenesis did not run:
	// the counters InitGenesis writes are absent and read their first values.
	require.Equal(t, pricefeedtypes.DefaultParams(pricefeedtypes.DefaultAuthority()), a.PricefeedKeeper.GetParams(next))
	markets := 0
	require.NoError(t, a.PerpsKeeper.IterateMarkets(next, func(perpstypes.Market) bool {
		markets++
		return false
	}))
	require.Equal(t, 18, markets)
	perpsParams, err := a.PerpsKeeper.GetParams(next)
	require.NoError(t, err)
	require.Equal(t, perpstypes.DefaultParams(), perpsParams)
	require.False(t, next.KVStore(a.GetKey(perpstypes.StoreKey)).Has(perpstypes.SequenceKey(perpstypes.SequencePlacement)))
	spotParams, err := a.SpotKeeper.GetParams(next)
	require.NoError(t, err)
	require.Len(t, spotParams.Tokens, 3)
	require.False(t, next.KVStore(a.GetKey(spottypes.StoreKey)).Has(spottypes.NextOrderIDKey))
	nextOrder, err := a.SpotKeeper.NextOrderID(next)
	require.NoError(t, err)
	require.Equal(t, uint64(1), nextOrder)

	// The state was written once: a second spot run refuses and a second
	// perps seed adds no market.
	require.ErrorIs(t, spotkeeper.InitV6120(next, *a.SpotKeeper), spotkeeper.ErrAlreadyInitialised)
	require.NoError(t, perpskeeper.SeedV6120(next, *a.PerpsKeeper))
	markets = 0
	require.NoError(t, a.PerpsKeeper.IterateMarkets(next, func(perpstypes.Market) bool {
		markets++
		return false
	}))
	require.Equal(t, 18, markets)
}
