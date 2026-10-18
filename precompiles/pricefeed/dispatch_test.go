package pricefeed_test

import (
	"math"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/pricefeed"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/ed25519"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	stakingtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

type dispatchKeepers struct {
	utils.Keepers
	pricefeed *pricefeedkeeper.Keeper
}

func (k dispatchKeepers) PriceFeedK() *pricefeedkeeper.Keeper { return k.pricefeed }

type dispatchHarness struct {
	pc       *pricefeed.Precompile
	keeper   *pricefeedkeeper.Keeper
	db       *state.DBImpl
	evm      *vm.EVM
	caller   common.Address
	operator sdk.ValAddress
	pubKey   []byte
	feed     common.Address
}

func newDispatchHarness(t *testing.T) *dispatchHarness {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx(nil).WithBlockHeight(300).WithBlockTime(time.UnixMilli(100000)).WithEventManager(sdk.NewEventManager())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	k, ctx := pricefeedtestutil.NewKeeper(app, ctx)
	key := secp256k1.GenPrivKeyFromSecret([]byte("pricefeed dispatch operator"))
	operator := sdk.ValAddress(key.PubKey().Address())
	public, err := crypto.DecompressPubkey(key.PubKey().Bytes())
	require.NoError(t, err)
	caller := crypto.PubkeyToAddress(*public)
	app.EvmKeeper.SetAddressMapping(ctx, sdk.AccAddress(operator), caller)
	val, err := stakingtypes.NewValidator(operator, ed25519.GenPrivKeyFromSecret([]byte("pricefeed dispatch consensus")).PubKey(), stakingtypes.Description{})
	require.NoError(t, err)
	app.StakingKeeper.SetValidator(ctx, val.UpdateStatus(stakingtypes.Bonded))
	genesis := types.DefaultGenesis()
	genesis.Aggregates = []types.GenesisAggregate{
		{MarketID: 0, Aggregate: types.Aggregate{IndexPrice: sdk.NewIntWithDecimal(65000, 18), MarkPrice: sdk.NewIntWithDecimal(65010, 18), Basis: sdk.NewIntWithDecimal(10, 18), TimestampMs: 100000, Height: 300, Sources: 4}},
		{MarketID: 1, Aggregate: types.Aggregate{IndexPrice: sdk.NewIntWithDecimal(2000, 18), MarkPrice: sdk.NewIntWithDecimal(2001, 18), Basis: sdk.NewIntWithDecimal(1, 18), TimestampMs: 99999, Height: 299, Sources: 3}},
	}
	genesis.Observations = []types.GenesisObservation{{MarketID: 0, Operator: operator, Observation: types.ObservationRecord{
		Sequence: 7, IndexPrice: sdk.NewIntWithDecimal(65000, 18), MarkPrice: sdk.NewIntWithDecimal(65010, 18), SourceTimestampMs: 100000, Height: 300,
	}}}
	require.NoError(t, k.InitGenesis(ctx, *genesis))
	pc, err := pricefeed.NewPrecompile(dispatchKeepers{Keepers: app.GetPrecompileKeepers(), pricefeed: &k})
	require.NoError(t, err)
	db := state.NewDBImpl(ctx, &app.EvmKeeper, false)
	block, err := app.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
	require.NoError(t, err)
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(app.EvmKeeper.ChainID(ctx))
	contracts := map[common.Address]vm.PrecompiledContract{pc.Address(): pc}
	evm := vm.NewEVM(*block, db, cfg, vm.Config{}, contracts)
	pubKey := secp256k1.GenPrivKeyFromSecret([]byte("pricefeed dispatch feed")).PubKey().Bytes()
	feed := common.BytesToAddress((&secp256k1.PubKey{Key: pubKey}).Address())
	return &dispatchHarness{pc: pc, keeper: &k, db: db, evm: evm, caller: caller, operator: operator, pubKey: pubKey, feed: feed}
}

func (h *dispatchHarness) input(t *testing.T, name string, args ...interface{}) []byte {
	t.Helper()
	input, err := h.pc.GetABI().Pack(name, args...)
	require.NoError(t, err)
	return input
}

func (h *dispatchHarness) call(t *testing.T, name string, args ...interface{}) []interface{} {
	t.Helper()
	input := h.input(t, name, args...)
	const supplied uint64 = 1000000
	method := h.pc.GetABI().Methods[name]
	base := pricefeed.ViewBaseGas
	var ret []byte
	var left uint64
	var err error
	if method.StateMutability == "view" {
		ret, left, err = h.evm.StaticCall(h.caller, h.pc.Address(), input, supplied)
	} else {
		base = pricefeed.WriteBaseGas
		ret, left, err = h.evm.Call(h.caller, h.pc.Address(), input, supplied, uint256.NewInt(0))
	}
	require.NoError(t, err, "%s: %x", name, ret)
	require.Equal(t, supplied-base-pricefeed.GasPerByte*uint64(len(input)-4), left)
	out, err := method.Outputs.Unpack(ret)
	require.NoError(t, err)
	return out
}

func dispatchPrice(out interface{}) pricefeed.PriceView {
	return *abi.ConvertType(out, new(pricefeed.PriceView)).(*pricefeed.PriceView)
}

func TestDispatch(t *testing.T) {
	h := newDispatchHarness(t)
	methods := []struct {
		name  string
		args  []interface{}
		check func(*testing.T, []interface{})
	}{
		{"registerFeedKey", []interface{}{h.pubKey}, func(t *testing.T, out []interface{}) {
			require.Equal(t, []interface{}{h.feed}, out)
			stored, found := h.keeper.GetValidatorByFeedAccount(h.db.Ctx(), sdk.AccAddress(h.feed.Bytes()))
			require.True(t, found)
			require.Equal(t, h.operator, stored)
			logs := h.db.GetAllLogs()
			require.Len(t, logs, 1)
			require.Equal(t, h.pc.Address(), logs[0].Address)
			event := h.pc.GetABI().Events["FeedKeyRegistered"]
			require.Equal(t, []common.Hash{event.ID, common.BytesToHash(h.operator), common.BytesToHash(h.feed.Bytes())}, logs[0].Topics)
			data, err := event.Inputs.NonIndexed().Unpack(logs[0].Data)
			require.NoError(t, err)
			require.Equal(t, []interface{}{h.pubKey, common.Address{}}, data)
		}},
		{"getPrice", []interface{}{big.NewInt(0)}, func(t *testing.T, out []interface{}) {
			got := dispatchPrice(out[0])
			require.Equal(t, "65000000000000000000000", got.IndexPrice.String())
			require.Equal(t, "65010000000000000000000", got.MarkPrice.String())
			require.Equal(t, uint64(100000), got.TimestampMs)
			require.Equal(t, int64(300), got.Height)
			require.Equal(t, uint32(4), got.Sources)
			require.True(t, got.Fresh)
		}},
		{"getPriceBySymbol", []interface{}{"ETH"}, func(t *testing.T, out []interface{}) {
			got := dispatchPrice(out[0])
			require.Equal(t, "2000000000000000000000", got.IndexPrice.String())
			require.Equal(t, "2001000000000000000000", got.MarkPrice.String())
			require.Equal(t, uint64(99999), got.TimestampMs)
			require.Equal(t, int64(299), got.Height)
			require.Equal(t, uint32(3), got.Sources)
			require.True(t, got.Fresh)
		}},
		{"getPrices", []interface{}{[]*big.Int{big.NewInt(1), big.NewInt(0)}}, func(t *testing.T, out []interface{}) {
			got := *abi.ConvertType(out[0], new([]pricefeed.PriceView)).(*[]pricefeed.PriceView)
			require.Len(t, got, 2)
			require.Equal(t, "2000000000000000000000", got[0].IndexPrice.String())
			require.Equal(t, "65000000000000000000000", got[1].IndexPrice.String())
		}},
		{"getMarket", []interface{}{big.NewInt(1)}, func(t *testing.T, out []interface{}) {
			got := *abi.ConvertType(out[0], new(pricefeed.MarketView)).(*pricefeed.MarketView)
			require.Equal(t, uint64(1), got.MarketId)
			require.Equal(t, "ETH", got.Symbol)
			require.Equal(t, "ETH", got.ChainflowSymbol)
			require.Equal(t, "1", got.MinPrice.String())
			require.Equal(t, "1000000000000000000000000000000", got.MaxPrice.String())
			require.True(t, got.Enabled)
		}},
		{"getMarketCount", nil, func(t *testing.T, out []interface{}) { require.Equal(t, "18", out[0].(*big.Int).String()) }},
		{"marketIdOf", []interface{}{"ETH"}, func(t *testing.T, out []interface{}) { require.Equal(t, "1", out[0].(*big.Int).String()) }},
		{"getObservation", []interface{}{big.NewInt(0), common.BytesToAddress(h.operator)}, func(t *testing.T, out []interface{}) {
			got := *abi.ConvertType(out[0], new(pricefeed.ObservationView)).(*pricefeed.ObservationView)
			require.Equal(t, uint64(7), got.Sequence)
			require.Equal(t, "65000000000000000000000", got.IndexPrice.String())
			require.Equal(t, "65010000000000000000000", got.MarkPrice.String())
			require.Equal(t, uint64(100000), got.SourceTimestampMs)
			require.Equal(t, int64(300), got.Height)
		}},
		{"getFeedKey", []interface{}{common.BytesToAddress(h.operator)}, func(t *testing.T, out []interface{}) { require.Equal(t, []interface{}{h.pubKey, h.feed}, out) }},
		{"getValidatorByFeedAccount", []interface{}{h.feed}, func(t *testing.T, out []interface{}) {
			require.Equal(t, []interface{}{common.BytesToAddress(h.operator)}, out)
		}},
		{"getParams", nil, func(t *testing.T, out []interface{}) {
			got := *abi.ConvertType(out[0], new(pricefeed.ParamsView)).(*pricefeed.ParamsView)
			require.Equal(t, pricefeed.ParamsView{StalenessWindowMs: 10000, MaxClockSkewMs: 2000, MinSources: 1,
				MaxDivergenceBps: 50, MaxDeviationBps: 1000, MaxObservationsPerSubmission: 32, MaxSubmissionBytes: 8192,
				MarkEmaTauMs: 150000, MaxMarkBasisBps: 500, MaxPriceAgeMs: 15000}, got)
		}},
		{"revokeFeedKey", nil, func(t *testing.T, out []interface{}) {
			require.Empty(t, out)
			_, found := h.keeper.GetFeedKey(h.db.Ctx(), h.operator)
			require.False(t, found)
			logs := h.db.GetAllLogs()
			require.Len(t, logs, 2)
			require.Equal(t, h.pc.Address(), logs[1].Address)
			require.Equal(t, []common.Hash{h.pc.GetABI().Events["FeedKeyRevoked"].ID, common.BytesToHash(h.operator), common.BytesToHash(h.feed.Bytes())}, logs[1].Topics)
			require.Empty(t, logs[1].Data)
		}},
	}
	require.Len(t, h.pc.GetABI().Methods, len(methods))
	seen := make(map[string]bool)
	for _, tc := range methods {
		t.Run(tc.name, func(t *testing.T) {
			require.False(t, seen[tc.name])
			seen[tc.name] = true
			tc.check(t, h.call(t, tc.name, tc.args...))
		})
	}
	for name := range h.pc.GetABI().Methods {
		require.True(t, seen[name], name)
	}

	t.Run("missing_keeper", func(t *testing.T) {
		bare := struct{ utils.Keepers }{testkeeper.EVMTestApp.GetPrecompileKeepers()}
		missing, err := pricefeed.NewPrecompile(bare)
		require.NoError(t, err)
		for _, tc := range methods {
			ret, err := missing.Run(h.evm, h.caller, h.caller, h.input(t, tc.name, tc.args...), nil, false, false, nil)
			require.ErrorIs(t, err, vm.ErrExecutionReverted, tc.name)
			reason, decodeErr := abi.UnpackRevert(ret)
			require.NoError(t, decodeErr)
			require.Equal(t, "LXP_PRICEFEED_KEEPER_MISSING: pricefeed: the pricefeed keeper is not wired into this application", reason)
		}
	})
	t.Run("unknown_selector", func(t *testing.T) {
		ret, _, err := h.evm.StaticCall(h.caller, h.pc.Address(), []byte{0xff, 0xff, 0xff, 0xff}, 1000000)
		require.ErrorIs(t, err, vm.ErrExecutionReverted)
		reason, err := abi.UnpackRevert(ret)
		require.NoError(t, err)
		require.Equal(t, "no method with id: 0xffffffff", reason)
	})
}

func TestDispatchRefusals(t *testing.T) {
	h := newDispatchHarness(t)
	oversized := make([]*big.Int, 65)
	for i := range oversized {
		oversized[i] = big.NewInt(0)
	}
	cases := []struct {
		name string
		args []interface{}
		code string
	}{
		{"getPrices", []interface{}{oversized}, "LXP_PRICEFEED_BATCH_TOO_LARGE"},
		{"getPrice", []interface{}{new(big.Int).Lsh(big.NewInt(1), 64)}, "LXP_PRICEFEED_UNKNOWN_MARKET"},
		{"getMarket", []interface{}{big.NewInt(18)}, "LXP_PRICEFEED_UNKNOWN_MARKET"},
		{"getPrices", []interface{}{[]*big.Int{big.NewInt(0), big.NewInt(18)}}, "LXP_PRICEFEED_UNKNOWN_MARKET"},
		{"getObservation", []interface{}{big.NewInt(18), common.BytesToAddress(h.operator)}, "LXP_PRICEFEED_UNKNOWN_MARKET"},
		{"getPriceBySymbol", []interface{}{"btc"}, "LXP_PRICEFEED_UNKNOWN_MARKET"},
		{"marketIdOf", []interface{}{"btc"}, "LXP_PRICEFEED_UNKNOWN_MARKET"},
		{"registerFeedKey", []interface{}{[]byte{1}}, "LXP_PRICEFEED_INVALID_KEY"},
		{"revokeFeedKey", nil, "LXP_PRICEFEED_NO_KEY"},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/"+tc.code, func(t *testing.T) {
			ret, _, err := h.evm.Call(h.caller, h.pc.Address(), h.input(t, tc.name, tc.args...), 1000000, uint256.NewInt(0))
			require.ErrorIs(t, err, vm.ErrExecutionReverted)
			require.Equal(t, []byte{0x08, 0xc3, 0x79, 0xa0}, ret[:4])
			reason, err := abi.UnpackRevert(ret)
			require.NoError(t, err)
			require.Equal(t, tc.code, strings.SplitN(reason, ":", 2)[0])
		})
	}
	for _, tc := range []struct {
		name             string
		value            *big.Int
		static, delegate bool
		code             string
		args             []interface{}
	}{
		{name: "getMarketCount", value: big.NewInt(1), code: "LXP_PRECOMPILE_NON_PAYABLE"},
		{name: "getMarketCount", delegate: true, code: "LXP_PRICEFEED_DELEGATECALL"},
		{name: "registerFeedKey", static: true, code: "LXP_PRICEFEED_STATICCALL", args: []interface{}{h.pubKey}},
		{name: "revokeFeedKey", static: true, code: "LXP_PRICEFEED_STATICCALL"},
	} {
		t.Run(tc.code+"/"+tc.name, func(t *testing.T) {
			ret, err := h.pc.Run(h.evm, h.caller, h.caller, h.input(t, tc.name, tc.args...), tc.value, tc.static, tc.delegate, nil)
			require.ErrorIs(t, err, vm.ErrExecutionReverted)
			reason, err := abi.UnpackRevert(ret)
			require.NoError(t, err)
			require.Equal(t, tc.code, strings.SplitN(reason, ":", 2)[0])
		})
	}
	t.Run("missing_argument", func(t *testing.T) {
		method := h.pc.GetABI().Methods["getPrice"]
		ret, err := h.pc.Run(h.evm, h.caller, h.caller, method.ID, nil, true, false, nil)
		require.ErrorIs(t, err, vm.ErrExecutionReverted)
		reason, err := abi.UnpackRevert(ret)
		require.NoError(t, err)
		require.Equal(t, "LXP_PRECOMPILE_ARG_COUNT", strings.SplitN(reason, ":", 2)[0])
	})
}

func TestDispatchEmptyViews(t *testing.T) {
	h := newDispatchHarness(t)
	price := dispatchPrice(h.call(t, "getPrice", big.NewInt(2))[0])
	require.Equal(t, "0", price.IndexPrice.String())
	require.Equal(t, "0", price.MarkPrice.String())
	require.Equal(t, uint64(0), price.TimestampMs)
	require.Equal(t, int64(0), price.Height)
	require.Equal(t, uint32(0), price.Sources)
	require.False(t, price.Fresh)
	observation := *abi.ConvertType(h.call(t, "getObservation", big.NewInt(0), common.Address{})[0], new(pricefeed.ObservationView)).(*pricefeed.ObservationView)
	require.Equal(t, "0", observation.IndexPrice.String())
	require.Equal(t, "0", observation.MarkPrice.String())
	require.Equal(t, uint64(0), observation.Sequence)
	require.Equal(t, uint64(0), observation.SourceTimestampMs)
	require.Equal(t, int64(0), observation.Height)
	require.Equal(t, []interface{}{[]byte{}, common.Address{}}, h.call(t, "getFeedKey", common.Address{}))
	require.Equal(t, []interface{}{common.Address{}}, h.call(t, "getValidatorByFeedAccount", common.Address{}))
	prices := *abi.ConvertType(h.call(t, "getPrices", []*big.Int{})[0], new([]pricefeed.PriceView)).(*[]pricefeed.PriceView)
	require.Empty(t, prices)
	ids := make([]*big.Int, 64)
	for i := range ids {
		ids[i] = big.NewInt(0)
	}
	prices = *abi.ConvertType(h.call(t, "getPrices", ids)[0], new([]pricefeed.PriceView)).(*[]pricefeed.PriceView)
	require.Len(t, prices, 64)
	for _, price := range prices {
		require.Equal(t, "65000000000000000000000", price.IndexPrice.String())
	}
}
