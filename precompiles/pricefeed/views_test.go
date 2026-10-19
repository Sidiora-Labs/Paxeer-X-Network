package pricefeed_test

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/pricefeed"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

const (
	unknownMarket = "LXP_PRICEFEED_UNKNOWN_MARKET"
	batchTooLarge = "LXP_PRICEFEED_BATCH_TOO_LARGE"

	aggregateMs     = uint64(1_700_000_000_000)
	aggregateHeight = int64(300)
	readMs          = int64(1_700_000_005_000)
)

var (
	validatorV = common.HexToAddress("0x00000000000000000000000000000000000000a1")
	validatorW = common.HexToAddress("0x00000000000000000000000000000000000000a2")
	unknownAcc = common.HexToAddress("0x0000000000000000000000000000000000000007")
	aboveU64   = new(big.Int).Lsh(big.NewInt(1), 64)
)

// priceView, marketView, observationView and paramsView decode the tuples of
// precompiles/pricefeed/abi/abi.json by field name.
type priceView struct {
	IndexPrice  *big.Int
	MarkPrice   *big.Int
	TimestampMs uint64
	Height      int64
	Sources     uint32
	Fresh       bool
}

type marketView struct {
	MarketId        uint64
	Symbol          string
	ChainflowSymbol string
	MinPrice        *big.Int
	MaxPrice        *big.Int
	Enabled         bool
}

type observationView struct {
	Sequence          uint64
	IndexPrice        *big.Int
	MarkPrice         *big.Int
	SourceTimestampMs uint64
	Height            int64
}

type paramsView struct {
	StalenessWindowMs            uint64
	MaxClockSkewMs               uint64
	MinSources                   uint32
	MaxDivergenceBps             uint32
	MaxDeviationBps              uint32
	MaxObservationsPerSubmission uint32
	MaxSubmissionBytes           uint32
	MarkEmaTauMs                 uint64
	MaxMarkBasisBps              uint32
	MaxPriceAgeMs                uint64
}

// meteredView runs a view through a static call and checks it is
// charged exactly the view base gas plus the per-byte gas of its arguments.
func (h *harness) meteredView(name string, args ...interface{}) []interface{} {
	h.t.Helper()
	input := h.input(name, args...)
	ret, left, err := h.evm.StaticCall(h.caller, pricefeedAddress, input, suppliedGas)
	require.NoError(h.t, err, name)
	require.Equal(h.t, suppliedGas-(3000+16*uint64(len(input)-4)), left, name)
	out, err := h.method(name).Outputs.Unpack(ret)
	require.NoError(h.t, err)
	return out
}

// viewReverts runs a view through a static call and returns its revert reason.
func (h *harness) viewReverts(name string, args ...interface{}) string {
	h.t.Helper()
	ret, _, err := h.evm.StaticCall(h.caller, pricefeedAddress, h.input(name, args...), suppliedGas)
	require.ErrorIs(h.t, err, vm.ErrExecutionReverted)
	reason, err := abi.UnpackRevert(ret)
	require.NoError(h.t, err)
	return reason
}

func (h *harness) requireReverts(code, name string, args ...interface{}) {
	h.t.Helper()
	reason := h.viewReverts(name, args...)
	require.True(h.t, strings.HasPrefix(reason, code), "%s: %q", name, reason)
}

// seedState imports the default params with the given feed keys,
// observations and aggregates through the module's genesis import.
func (h *harness) seedState(feedKeys []types.GenesisFeedKey, observations []types.GenesisObservation,
	aggregates []types.GenesisAggregate) {
	h.t.Helper()
	require.NoError(h.t, h.keeper.InitGenesis(h.ctx(), types.GenesisState{
		Params:       types.DefaultParams(types.DefaultAuthority()),
		FeedKeys:     feedKeys,
		Observations: observations,
		Aggregates:   aggregates,
	}))
}

func (h *harness) atBlockMs(ms int64) {
	h.stateDB.WithCtx(h.ctx().WithBlockTime(time.UnixMilli(ms)))
}

func e18(units int64) sdk.Int { return sdk.NewIntWithDecimal(units, 18) }

func aggregateOf(marketID uint64, index, mark int64, sources uint32) types.GenesisAggregate {
	return types.GenesisAggregate{MarketID: marketID, Aggregate: types.Aggregate{
		IndexPrice: e18(index), MarkPrice: e18(mark), Basis: e18(mark - index),
		TimestampMs: aggregateMs, Height: aggregateHeight, Sources: sources,
	}}
}

// canonical re-sets a decoded integer so that zero carries the same internal
// representation as big.NewInt(0) under deep equality.
func canonical(x *big.Int) *big.Int { return new(big.Int).Set(x) }

func priceOf(out []interface{}) priceView {
	price := *abi.ConvertType(out[0], new(priceView)).(*priceView)
	price.IndexPrice, price.MarkPrice = canonical(price.IndexPrice), canonical(price.MarkPrice)
	return price
}

func TestViewVersionedSetup(t *testing.T) {
	h := newHarness(t)
	versions := pricefeed.GetVersioned("v6.12.0", keepers{
		Keepers: testkeeper.EVMTestApp.GetPrecompileKeepers(), pricefeed: h.keeper,
	})
	require.Len(t, versions, 1)
	contract, ok := versions["v6.12.0"]
	require.True(t, ok)
	p, ok := contract.(*pricefeed.Precompile)
	require.True(t, ok)
	require.Equal(t, pricefeedAddress, p.Address())
	ret, err := p.Run(h.evm, h.caller, h.caller, h.input("getMarketCount"), big.NewInt(0), true, false, nil)
	require.NoError(t, err)
	out, err := h.method("getMarketCount").Outputs.Unpack(ret)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(18), out[0])
}

func TestViewGetPrice(t *testing.T) {
	h := newHarness(t)
	h.seedState(nil, nil, []types.GenesisAggregate{aggregateOf(0, 65000, 65010, 4)})
	h.atBlockMs(readMs)

	want := priceView{IndexPrice: e18(65000).BigInt(), MarkPrice: e18(65010).BigInt(), TimestampMs: aggregateMs,
		Height: aggregateHeight, Sources: 4, Fresh: true}
	require.Equal(t, want, priceOf(h.meteredView("getPrice", big.NewInt(0))))
	require.Equal(t, want, priceOf(h.meteredView("getPriceBySymbol", "BTC")))

	stored, err := h.keeper.GetPrice(h.ctx(), 0)
	require.NoError(t, err)
	require.Equal(t, want, priceView{IndexPrice: stored.Index.BigInt(), MarkPrice: stored.Mark.BigInt(),
		TimestampMs: stored.TimestampMs, Height: stored.Height, Sources: stored.Sources, Fresh: stored.Fresh})

	// A listed market without an aggregate reads zero prices, not fresh.
	none := priceView{IndexPrice: big.NewInt(0), MarkPrice: big.NewInt(0)}
	require.Equal(t, none, priceOf(h.meteredView("getPrice", big.NewInt(6))))
	require.Equal(t, none, priceOf(h.meteredView("getPriceBySymbol", "NVDA")))

	// Past max_price_age_ms the same aggregate reads not fresh.
	h.atBlockMs(int64(aggregateMs + types.DefaultMaxPriceAgeMs + 1))
	stale := want
	stale.Fresh = false
	require.Equal(t, stale, priceOf(h.meteredView("getPrice", big.NewInt(0))))
}

func TestViewGetPrices(t *testing.T) {
	h := newHarness(t)
	h.seedState(nil, nil, []types.GenesisAggregate{aggregateOf(0, 65000, 65010, 4), aggregateOf(1, 3500, 3501, 3)})
	h.atBlockMs(readMs)

	btc := priceView{IndexPrice: e18(65000).BigInt(), MarkPrice: e18(65010).BigInt(), TimestampMs: aggregateMs,
		Height: aggregateHeight, Sources: 4, Fresh: true}
	eth := priceView{IndexPrice: e18(3500).BigInt(), MarkPrice: e18(3501).BigInt(), TimestampMs: aggregateMs,
		Height: aggregateHeight, Sources: 3, Fresh: true}
	decode := func(out []interface{}) []priceView {
		return *abi.ConvertType(out[0], new([]priceView)).(*[]priceView)
	}
	require.Equal(t, []priceView{eth, btc}, decode(h.meteredView("getPrices", []*big.Int{big.NewInt(1), big.NewInt(0)})))
	require.Empty(t, decode(h.meteredView("getPrices", []*big.Int{})))

	full := make([]*big.Int, 64)
	for i := range full {
		full[i] = big.NewInt(int64(i % 2))
	}
	prices := decode(h.meteredView("getPrices", full))
	require.Len(t, prices, 64)
	for i, price := range prices {
		if i%2 == 0 {
			require.Equal(t, btc, price, i)
		} else {
			require.Equal(t, eth, price, i)
		}
	}

	h.requireReverts(batchTooLarge, "getPrices", append(full, big.NewInt(0)))
}

func TestViewUnknownMarket(t *testing.T) {
	h := newHarness(t)
	h.seedState(nil, nil, nil)
	listed := big.NewInt(int64(len(types.Markets)))

	h.requireReverts(unknownMarket, "getPrice", aboveU64)
	h.requireReverts(unknownMarket, "getPrice", listed)
	h.requireReverts(unknownMarket, "getPriceBySymbol", "btc")
	h.requireReverts(unknownMarket, "getPriceBySymbol", "")
	h.requireReverts(unknownMarket, "getPrices", []*big.Int{big.NewInt(0), aboveU64})
	h.requireReverts(unknownMarket, "getPrices", []*big.Int{listed})
	h.requireReverts(unknownMarket, "getMarket", aboveU64)
	h.requireReverts(unknownMarket, "getMarket", listed)
	h.requireReverts(unknownMarket, "getObservation", aboveU64, validatorV)
	h.requireReverts(unknownMarket, "getObservation", listed, validatorV)
	h.requireReverts(unknownMarket, "marketIdOf", "Btc")
}

func TestViewMarketIdOf(t *testing.T) {
	h := newHarness(t)
	h.seedState(nil, nil, nil)

	require.Equal(t, big.NewInt(8), h.meteredView("marketIdOf", "XAU")[0])
	for _, market := range types.Markets {
		require.Equal(t, new(big.Int).SetUint64(market.ID), canonical(h.meteredView("marketIdOf", market.Symbol)[0].(*big.Int)), market.Symbol)
	}
	h.requireReverts(unknownMarket, "marketIdOf", "xau")
	h.requireReverts(unknownMarket, "marketIdOf", "PERP_XAU_USDC")
}

func TestViewGetMarket(t *testing.T) {
	h := newHarness(t)
	h.seedState(nil, nil, nil)

	market := func(id int64) marketView {
		return *abi.ConvertType(h.meteredView("getMarket", big.NewInt(id))[0], new(marketView)).(*marketView)
	}
	require.Equal(t, marketView{MarketId: 13, Symbol: "HYPE", ChainflowSymbol: "HYPE", MinPrice: big.NewInt(1),
		MaxPrice: new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil), Enabled: true}, market(13))
	for _, listed := range types.Markets {
		got := market(int64(listed.ID))
		require.Equal(t, marketView{MarketId: listed.ID, Symbol: listed.Symbol, ChainflowSymbol: listed.ChainflowSymbol,
			MinPrice: listed.MinPrice.BigInt(), MaxPrice: listed.MaxPrice.BigInt(), Enabled: listed.Enabled}, got)
	}
	require.Equal(t, big.NewInt(18), h.meteredView("getMarketCount")[0])
}

func TestViewGetObservation(t *testing.T) {
	h := newHarness(t)
	record := types.ObservationRecord{Sequence: 7, IndexPrice: sdk.NewIntWithDecimal(650005, 17),
		MarkPrice: e18(65010), SourceTimestampMs: 1_699_999_999_500, Height: 100}
	h.seedState(nil, []types.GenesisObservation{{MarketID: 0, Operator: sdk.ValAddress(validatorV.Bytes()),
		Observation: record}}, nil)

	observation := func(id int64, validator common.Address) observationView {
		out := h.meteredView("getObservation", big.NewInt(id), validator)
		got := *abi.ConvertType(out[0], new(observationView)).(*observationView)
		got.IndexPrice, got.MarkPrice = canonical(got.IndexPrice), canonical(got.MarkPrice)
		return got
	}
	require.Equal(t, observationView{Sequence: 7, IndexPrice: record.IndexPrice.BigInt(),
		MarkPrice: record.MarkPrice.BigInt(), SourceTimestampMs: 1_699_999_999_500, Height: 100}, observation(0, validatorV))
	zero := observationView{IndexPrice: big.NewInt(0), MarkPrice: big.NewInt(0)}
	require.Equal(t, zero, observation(0, validatorW))
	require.Equal(t, zero, observation(1, validatorV))
}

func TestViewFeedKeys(t *testing.T) {
	h := newHarness(t)
	pubKey := secp256k1.GenPrivKey().PubKey()
	feedAccount := sdk.AccAddress(pubKey.Address())
	h.seedState([]types.GenesisFeedKey{{Operator: sdk.ValAddress(validatorV.Bytes()), FeedKey: types.FeedKey{
		PubKey: common.Bytes2Hex(pubKey.Bytes()), FeedAccount: feedAccount.String()}}}, nil, nil)

	out := h.meteredView("getFeedKey", validatorV)
	require.Equal(t, pubKey.Bytes(), out[0])
	require.Equal(t, common.BytesToAddress(feedAccount), out[1])
	require.Equal(t, validatorV, h.meteredView("getValidatorByFeedAccount", common.BytesToAddress(feedAccount))[0])

	out = h.meteredView("getFeedKey", validatorW)
	require.Equal(t, []byte{}, out[0])
	require.Equal(t, common.Address{}, out[1])
	require.Equal(t, common.Address{}, h.meteredView("getValidatorByFeedAccount", unknownAcc)[0])
}

func TestViewGetParams(t *testing.T) {
	h := newHarness(t)
	h.seedState(nil, nil, nil)

	got := *abi.ConvertType(h.meteredView("getParams")[0], new(paramsView)).(*paramsView)
	require.Equal(t, paramsView{StalenessWindowMs: 10000, MaxClockSkewMs: 2000, MinSources: 1, MaxDivergenceBps: 50,
		MaxDeviationBps: 1000, MaxObservationsPerSubmission: 32, MaxSubmissionBytes: 8192, MarkEmaTauMs: 150000,
		MaxMarkBasisBps: 500, MaxPriceAgeMs: 15000}, got)
}
