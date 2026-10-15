package app

import (
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	layerxbridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/version"
	upgradetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/types"
	"github.com/ethereum/go-ethereum/common"
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
