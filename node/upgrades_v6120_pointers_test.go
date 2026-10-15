package app

import (
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/artifacts/native"
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	layerxbridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	spottypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	upgradetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

// v6120PointerERC reads name, symbol and decimals from a deployed pointer.
func v6120PointerERC(t *testing.T, a *App, ctx sdk.Context, pointer common.Address) utils.ERCMetadata {
	t.Helper()
	caller := a.AccountKeeper.GetModuleAddress(evmtypes.ModuleName)
	read := func(method string) interface{} {
		input, err := native.GetParsedABI().Pack(method)
		require.NoError(t, err)
		output, err := a.EvmKeeper.StaticCallEVM(ctx, caller, &pointer, input)
		require.NoError(t, err, method)
		values, err := native.GetParsedABI().Unpack(method, output)
		require.NoError(t, err, method)
		require.Len(t, values, 1, method)
		return values[0]
	}
	return utils.ERCMetadata{Name: read("name").(string), Symbol: read("symbol").(string), Decimals: read("decimals").(uint8)}
}

// v6120AllMetadata returns every bank denom metadata in store order.
func v6120AllMetadata(a *App, ctx sdk.Context) []banktypes.Metadata {
	var all []banktypes.Metadata
	a.BankKeeper.IterateAllDenomMetaData(ctx, func(metadata banktypes.Metadata) bool {
		all = append(all, metadata)
		return false
	})
	return all
}

func TestV6120Pointers(t *testing.T) {
	sidDenom := layerxbridgetypes.SidioraDenom()
	newApp := func() (*App, sdk.Context) {
		w := NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), false)
		return w.App, w.Ctx
	}
	requireNoPointers := func(a *App, ctx sdk.Context) {
		for _, denom := range []string{evmkeeper.BaseDenom, sidDenom} {
			_, _, exists := a.EvmKeeper.GetERC20NativePointer(ctx, denom)
			require.False(t, exists, denom)
		}
	}

	// Without the SID metadata the hook refuses before it writes anything.
	a, ctx := newApp()
	_, found := a.BankKeeper.GetDenomMetaData(ctx, evmkeeper.BaseDenom)
	require.False(t, found)
	require.EqualError(t, a.createV6120Pointers(ctx), sidDenom+" metadata is absent")
	_, found = a.BankKeeper.GetDenomMetaData(ctx, evmkeeper.BaseDenom)
	require.False(t, found)
	requireNoPointers(a, ctx)

	// SID metadata whose exponent is not 6 is refused.
	registerSidioraRemoteAsset(t, a, ctx, layerxbridgetypes.SidioraHomeChainID)
	sid, found := a.BankKeeper.GetDenomMetaData(ctx, sidDenom)
	require.True(t, found)
	wrongSID := sid
	wrongSID.DenomUnits = []*banktypes.DenomUnit{{Denom: sidDenom, Exponent: 0}, {Denom: layerxbridgetypes.SidioraSymbol, Exponent: 8}}
	cached, _ := ctx.CacheContext()
	a.BankKeeper.SetDenomMetaData(cached, wrongSID)
	require.EqualError(t, a.createV6120Pointers(cached), sidDenom+" exponent 8, want 6")
	_, found = a.BankKeeper.GetDenomMetaData(cached, evmkeeper.BaseDenom)
	require.False(t, found)
	requireNoPointers(a, cached)

	// Existing uhpx metadata with exponent 18 fails the plan through the
	// pointers hook, and no pointer is created.
	cached, _ = ctx.CacheContext()
	wrongPax := v6120PaxMetadata
	wrongPax.DenomUnits = []*banktypes.DenomUnit{{Denom: evmkeeper.BaseDenom, Exponent: 0}, {Denom: "PAX", Exponent: 18}}
	a.BankKeeper.SetDenomMetaData(cached, wrongPax)
	bindNilV6120Hooks(a)
	a.v6120HookTable[0] = v6120Hook{name: "pointers", run: a.createV6120Pointers}
	newVM, err := a.runV6120Upgrade(cached, upgradetypes.Plan{Name: V6120Upgrade}, a.UpgradeKeeper.GetModuleVersionMap(cached))
	require.EqualError(t, err, "upgrade v6.12.0: pointers: uhpx exponent 18, want 6")
	require.Nil(t, newVM)
	paxAfter, found := a.BankKeeper.GetDenomMetaData(cached, evmkeeper.BaseDenom)
	require.True(t, found)
	require.Equal(t, wrongPax, paxAfter)
	requireNoPointers(a, cached)

	// An existing SID pointer is kept; uhpx gets its metadata and a new pointer.
	requireNoPointers(a, ctx)
	sidERC := utils.ERCMetadata{Name: layerxbridgetypes.SidioraSymbol, Symbol: layerxbridgetypes.SidioraSymbol, Decimals: 6}
	var sidPointer common.Address
	require.NoError(t, a.EvmKeeper.RunWithOneOffEVMInstance(ctx, func(evm *vm.EVM) error {
		var upsertErr error
		sidPointer, upsertErr = a.EvmKeeper.UpsertERCNativePointer(ctx, evm, sidDenom, sidERC)
		return upsertErr
	}, func(string, string) {}))
	metadataBefore := v6120AllMetadata(a, ctx)

	require.NoError(t, a.createV6120Pointers(ctx))

	pax, found := a.BankKeeper.GetDenomMetaData(ctx, evmkeeper.BaseDenom)
	require.True(t, found)
	require.Equal(t, v6120PaxMetadata, pax)
	require.NoError(t, pax.Validate())
	require.ElementsMatch(t, append(metadataBefore, pax), v6120AllMetadata(a, ctx))
	paxPointer, _, exists := a.EvmKeeper.GetERC20NativePointer(ctx, evmkeeper.BaseDenom)
	require.True(t, exists)
	require.Equal(t, utils.ERCMetadata{Name: "PAX", Symbol: "PAX", Decimals: 6}, v6120PointerERC(t, a, ctx, paxPointer))
	keptSID, _, exists := a.EvmKeeper.GetERC20NativePointer(ctx, sidDenom)
	require.True(t, exists)
	require.Equal(t, sidPointer, keptSID)
	require.Equal(t, sidERC, v6120PointerERC(t, a, ctx, keptSID))
	require.NotEqual(t, paxPointer, keptSID)
	for denom, pointer := range map[string]common.Address{evmkeeper.BaseDenom: paxPointer, sidDenom: keptSID} {
		pointee, _, exists := a.EvmKeeper.GetNativePointee(ctx, pointer.Hex())
		require.True(t, exists, denom)
		require.Equal(t, denom, pointee)
	}

	// USDL has no pointer and no bank denom.
	usdl := common.HexToAddress(spottypes.USDLAddress)
	_, _, exists = a.EvmKeeper.GetNativePointee(ctx, usdl.Hex())
	require.False(t, exists)
	for _, metadata := range v6120AllMetadata(a, ctx) {
		require.NotEqual(t, "USDL", metadata.Symbol, metadata.Base)
		pointer, _, exists := a.EvmKeeper.GetERC20NativePointer(ctx, metadata.Base)
		if exists {
			require.NotEqual(t, usdl, pointer, metadata.Base)
		}
	}

	// A second run writes nothing and returns nil.
	evmModule := a.EvmKeeper.GetEVMAddressOrDefault(ctx, a.AccountKeeper.GetModuleAddress(evmtypes.ModuleName))
	nonce := a.EvmKeeper.GetNonce(ctx, evmModule)
	metadataAfter := v6120AllMetadata(a, ctx)
	again := ctx.WithEventManager(sdk.NewEventManager())
	require.NoError(t, a.createV6120Pointers(again))
	require.Empty(t, again.EventManager().Events())
	require.Equal(t, nonce, a.EvmKeeper.GetNonce(ctx, evmModule))
	require.Equal(t, metadataAfter, v6120AllMetadata(a, ctx))
	for denom, pointer := range map[string]common.Address{evmkeeper.BaseDenom: paxPointer, sidDenom: keptSID} {
		current, _, exists := a.EvmKeeper.GetERC20NativePointer(ctx, denom)
		require.True(t, exists, denom)
		require.Equal(t, pointer, current, denom)
	}

	// On a fresh chain the hook creates both pointers with decimals 6.
	a, ctx = newApp()
	registerSidioraRemoteAsset(t, a, ctx, layerxbridgetypes.SidioraHomeChainID)
	require.NoError(t, a.createV6120Pointers(ctx))
	for denom, want := range map[string]utils.ERCMetadata{
		evmkeeper.BaseDenom: {Name: "PAX", Symbol: "PAX", Decimals: 6},
		sidDenom:            sidERC,
	} {
		pointer, _, exists := a.EvmKeeper.GetERC20NativePointer(ctx, denom)
		require.True(t, exists, denom)
		require.Equal(t, want, v6120PointerERC(t, a, ctx, pointer), denom)
	}
}
