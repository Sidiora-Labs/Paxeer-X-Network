package keeper_test

import (
	"math/big"
	"testing"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	keeperv610 "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/legacy/v610/keeper"
	typesv610 "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/legacy/v610/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	tokenfactorykeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/tokenfactory/keeper"
	launchpadprecompile "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/launchpad"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// migrationEVM is the EVM keeper with the retirement runtime of the
// launchpad precompile package.
type migrationEVM struct{ *evmkeeper.Keeper }

func (m migrationEVM) InstallRetired(ctx sdk.Context, retired, replacement common.Address) error {
	return launchpadprecompile.InstallRetired(ctx, m.Keeper, retired, replacement)
}

func TestMigrateV6120Signature(t *testing.T) {
	var migrate func(sdk.Context, keeper.Keeper, keeper.EVMReader) error = keeper.MigrateV6120
	require.NotNil(t, migrate)

	app := testkeeper.EVMTestApp
	key := sdk.NewKVStoreKey(types.StoreKey)
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockTime(genesisTime)
	ctx = launchpadtest.WithStore(ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx)), key)
	k := keeper.NewKeeper(key, app.BankKeeper, tokenfactorykeeper.NewMsgServerImpl(app.TokenFactoryKeeper), &app.EvmKeeper)
	legacy := keeperv610.NewKeeper(key, app.BankKeeper, tokenfactorykeeper.NewMsgServerImpl(app.TokenFactoryKeeper),
		&app.EvmKeeper)
	legacy.InitGenesis(ctx, *typesv610.DefaultGenesis())
	e := &env{t: t, ctx: ctx, k: k}
	creator := e.account(250_000_000)
	_, err := legacy.CreateMarket(ctx, nil, creator, "Kindle Token", "KNDL", typesv610.FeeStrategyClaim)
	require.NoError(t, err)

	before := storeEntries(ctx, key)
	err = migrate(ctx, *k, migrationEVM{&app.EvmKeeper})
	require.ErrorIs(t, err, keeper.ErrNativeMarketsExist)
	require.EqualError(t, upgradeError(err), "upgrade v6.12.0: launchpad: native markets exist")
	require.Equal(t, before, storeEntries(ctx, key))
}

func TestParamsFromProtocolConfig(t *testing.T) {
	e := newEnv(t)
	evm := &testkeeper.EVMTestApp.EvmKeeper
	generation1 := common.HexToAddress("0xEeDF5409cFD30bd14D0399318c7d2150265575e5")
	generation2 := common.HexToAddress("0xF3669eDc475Cb6E1F0d7635240ABD8e8FB7E20C6")
	quote := common.HexToAddress("0x85FcD13735F4309833A503EE804ea32395851479")
	word := func(v int64) common.Hash { return common.BigToHash(big.NewInt(v)) }
	set := func(config common.Address, values map[int64]common.Hash) {
		for slot, value := range values {
			evm.SetState(e.ctx, config, word(slot), value)
		}
	}
	virtualQuote := new(big.Int).Mul(big.NewInt(10_000), big.NewInt(1_000_000))
	values := map[int64]common.Hash{1: common.BytesToHash(quote.Bytes()), 2: common.BigToHash(virtualQuote),
		3: common.BigToHash(new(big.Int).Exp(big.NewInt(10), big.NewInt(27), nil)), 4: word(30), 5: word(300),
		6: word(100), 7: word(2000), 8: word(500), 9: word(1000), 10: word(0), 11: word(1_000_000)}
	set(generation2, values)
	set(generation1, values)
	set(generation1, map[int64]common.Hash{6: word(150), 11: word(2_000_000)})

	params, diffs, err := keeper.ParamsFromProtocolConfig(e.ctx, evm)
	require.NoError(t, err)
	require.Equal(t, types.Params{VirtualQuoteDefault: sdk.NewIntFromBigInt(virtualQuote),
		VirtualTokenDefault: sdk.NewIntFromBigInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(27), nil)),
		MinFeeBps:           30, MaxFeeBps: 300, BaseFeeBps: 100, ProtocolFeeBps: 2000, FeeDecayRate: 500,
		VolatilityWeight: 1000, ConcentrationWeight: 0, CreationFee: sdk.NewInt(1_000_000),
		QuoteToken: quote.Hex()}, params)
	require.Equal(t, []keeper.ProtocolConfigDiff{
		{Field: "baseFeeBps", Generation1: "150", Generation2: "100"},
		{Field: "creationFee", Generation1: "2000000", Generation2: "1000000"},
	}, diffs)

	set(generation2, map[int64]common.Hash{10: word(1)})
	_, _, err = keeper.ParamsFromProtocolConfig(e.ctx, evm)
	require.ErrorIs(t, err, types.ErrInvalidParams)
	require.ErrorContains(t, err, "concentration weight must be 0")
}
