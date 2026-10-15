package launchpad_test

import (
	"encoding/json"
	"fmt"
	"math/big"
	"testing"
	"time"

	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/launchpad"
	launchpadv610 "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/launchpad/legacy/v610"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

const quote = types.DefaultQuoteDenom

var (
	genesisTime = time.Unix(1_800_000_000, 0).UTC()
	deadline    = big.NewInt(genesisTime.Add(10_000 * time.Hour).Unix())
	precompile  = common.HexToAddress(launchpad.LaunchpadAddress)
)

type keepers struct {
	utils.Keepers
	launchpad *launchpadkeeper.Keeper
}

func (k keepers) LaunchpadK() *launchpadkeeper.Keeper { return k.launchpad }

func (h *harness) fund(amount int64) (sdk.AccAddress, common.Address) {
	h.t.Helper()
	acc, address := testkeeper.MockAddressPair()
	testkeeper.EVMTestApp.EvmKeeper.SetAddressMapping(h.stateDB.Ctx(), acc, address)
	coins := sdk.NewCoins(sdk.NewCoin(quote, sdk.NewInt(amount)))
	require.NoError(h.t, testkeeper.EVMTestApp.BankKeeper.MintCoins(h.stateDB.Ctx(), "evm", coins))
	require.NoError(h.t, testkeeper.EVMTestApp.BankKeeper.SendCoinsFromModuleToAccount(h.stateDB.Ctx(), "evm", acc, coins))
	return acc, address
}

func (h *harness) method(name string) *abi.Method {
	h.t.Helper()
	m, ok := h.precompile.GetABI().Methods[name]
	require.True(h.t, ok, name)
	return &m
}

func (h *harness) input(name string, args ...interface{}) []byte {
	h.t.Helper()
	m := h.method(name)
	packed, err := m.Inputs.Pack(args...)
	require.NoError(h.t, err)
	return append(append([]byte(nil), m.ID...), packed...)
}

func (h *harness) run(from common.Address, name string, value *big.Int, readOnly, delegate bool, args ...interface{}) ([]interface{}, string) {
	h.t.Helper()
	res, err := h.precompile.Run(h.evm, from, from, h.input(name, args...), value, readOnly, delegate, nil)
	if err != nil {
		require.ErrorIs(h.t, err, vm.ErrExecutionReverted)
		reason, unpackErr := abi.UnpackRevert(res)
		require.NoError(h.t, unpackErr)
		require.NotEmpty(h.t, reason)
		return nil, reason
	}
	out, err := h.method(name).Outputs.Unpack(res)
	require.NoError(h.t, err)
	return out, ""
}

func (h *harness) call(from common.Address, name string, args ...interface{}) []interface{} {
	h.t.Helper()
	out, reason := h.run(from, name, nil, false, false, args...)
	require.Empty(h.t, reason, name)
	return out
}

func (h *harness) reverts(from common.Address, name string, args ...interface{}) string {
	h.t.Helper()
	_, reason := h.run(from, name, nil, false, false, args...)
	require.NotEmpty(h.t, reason, name)
	return reason
}

func (h *harness) view(name string, args ...interface{}) []interface{} {
	h.t.Helper()
	out, reason := h.run(h.caller, name, nil, true, false, args...)
	require.Empty(h.t, reason, name)
	return out
}

func (h *harness) balance(acc sdk.AccAddress, denom string) sdk.Int {
	return testkeeper.EVMTestApp.BankKeeper.GetBalance(h.stateDB.Ctx(), acc, denom).Amount
}

func (h *harness) logs(signature string) []*ethtypes.Log {
	var out []*ethtypes.Log
	for _, log := range h.stateDB.GetAllLogs() {
		if log.Topics[0] == crypto.Keccak256Hash([]byte(signature)) {
			require.Equal(h.t, precompile, log.Address)
			out = append(out, log)
		}
	}
	return out
}

func (h *harness) create() (common.Address, string) {
	h.t.Helper()
	out := h.call(h.caller, launchpadv610.CreateMarketMethod, "Kindle Token", "KNDL", uint8(types.FeeStrategyClaim))
	return out[0].(common.Address), out[1].(string)
}

func (h *harness) market(token common.Address) launchpadv610.Market {
	h.t.Helper()
	return *abi.ConvertType(h.view(launchpadv610.GetMarketMethod, token)[0], new(launchpadv610.Market)).(*launchpadv610.Market)
}

func sameJSON(t *testing.T, want, got interface{}) {
	t.Helper()
	wantJSON, err := json.Marshal(want)
	require.NoError(t, err)
	gotJSON, err := json.Marshal(got)
	require.NoError(t, err)
	require.JSONEq(t, string(wantJSON), string(gotJSON))
}

func addressTopic(address common.Address) common.Hash { return common.BytesToHash(address.Bytes()) }

func word(data []byte, index int) *big.Int {
	return new(big.Int).SetBytes(data[32*index : 32*index+32])
}

func TestRequiredGasIsPinned(t *testing.T) {
	h := newHarness(t)
	token := common.HexToAddress("0x00000000000000000000000000000000000f00f0")
	for _, tc := range []struct {
		input []byte
		want  uint64
	}{
		{h.input(launchpadv610.CreateMarketMethod, "Kindle Token", "KNDL", uint8(0)), 3000 + 16*224 + 5000*12 + 9_000_000},
		{h.input(launchpadv610.BuyMethod, token, big.NewInt(1), big.NewInt(0), token, deadline), 3000 + 16*160 + 5000*8},
		{h.input(launchpadv610.SellMethod, token, big.NewInt(1), big.NewInt(0), token, deadline), 3000 + 16*160 + 5000*6},
		{h.input(launchpadv610.SetFeeStrategyMethod, token, uint8(1)), 3000 + 16*64 + 5000},
		{h.input(launchpadv610.ClaimFeesMethod, token, token), 3000 + 16*64 + 5000*3},
		{h.input(launchpadv610.ExecuteBurnMethod, token), 3000 + 16*32 + 5000*3},
		{h.input(launchpadv610.ExecuteAirdropMethod, token), 3000 + 16*32 + 5000*4},
		{h.input(launchpadv610.ClaimAirdropMethod, token), 3000 + 16*32 + 5000*4},
		{h.input(launchpadv610.ExecuteLpRewardsMethod, token), 3000 + 16*32 + 5000},
		{h.input(launchpadv610.PauseMethod, token), 3000 + 16*32 + 5000},
		{h.input(launchpadv610.UnpauseMethod, token), 3000 + 16*32 + 5000},
		{h.input(launchpadv610.QuoteBuyMethod, token, big.NewInt(1)), 3000 + 16*64},
		{h.input(launchpadv610.GetProtocolFeesMethod, token), 3000 + 16*32},
		{h.input(launchpadv610.GetMarketsMethod, big.NewInt(0), big.NewInt(10)), 3000 + 16*64},
		{h.input(launchpadv610.GetMarketCountMethod), 3000},
		{h.input(launchpadv610.GetConfigMethod), 3000},
	} {
		require.Equal(t, tc.want, h.precompile.RequiredGas(tc.input))
	}
	require.Equal(t, uint64(9_066_584), h.precompile.RequiredGas(h.input(launchpadv610.CreateMarketMethod, "Kindle Token", "KNDL", uint8(0))))
	require.Equal(t, uint64(45_560), h.precompile.RequiredGas(h.input(launchpadv610.BuyMethod, token, big.NewInt(1), big.NewInt(0), token, deadline)))
	require.Equal(t, crypto.Keccak256([]byte("getProtocolFees(address)"))[:4], h.method(launchpadv610.GetProtocolFeesMethod).ID)
}

func TestNewPrecompileNeedsTheLaunchpadKeeper(t *testing.T) {
	_, err := launchpad.NewPrecompile(&utils.EmptyKeepers{})
	require.Error(t, err)
	_, err = launchpad.NewPrecompile(keepers{Keepers: testkeeper.EVMTestApp.GetPrecompileKeepers(), launchpad: nil})
	require.Error(t, err)
}

func TestCreateMarketAndTradeThroughThePrecompile(t *testing.T) {
	h := newHarness(t)
	token, denom := h.create()
	require.Equal(t, "factory/"+h.keeper.ModuleAddress().String()+"/lp1", denom)
	pointer, _, exists := testkeeper.EVMTestApp.EvmKeeper.GetERC20NativePointer(h.stateDB.Ctx(), denom)
	require.True(t, exists)
	require.Equal(t, pointer, token)
	require.Equal(t, sdk.NewInt(900_000_000), h.balance(h.callerAcc, quote))
	require.Equal(t, sdk.NewInt(100_000_000), h.balance(h.keeper.TreasuryAddress(), quote))
	created := h.logs("MarketCreated(address,address,string,string,string,uint8)")
	require.Len(t, created, 1)
	require.Equal(t, []common.Hash{created[0].Topics[0], addressTopic(token), addressTopic(h.caller)}, created[0].Topics)

	market := h.market(token)
	sameJSON(t, launchpadv610.Market{Token: token, Denom: denom, Index: 1, Name: "Kindle Token", Symbol: "KNDL",
		Creator: h.caller, Guardian: h.caller, FeeRightsHolder: h.caller, FeeStrategy: 0, Paused: false,
		TotalSupply: big.NewInt(1_000_000_000_000_000), VirtualQuoteReserve: big.NewInt(10_000_000_000),
		RealQuoteBalance: big.NewInt(0), TokenReserve: big.NewInt(1_000_000_000_000_000),
		CreatedAt: big.NewInt(genesisTime.Unix()), CumulativeVolume: big.NewInt(0), AccumulatedQuoteFees: big.NewInt(0),
		AccumulatedTokenFees: big.NewInt(0), AccumulatedFees: big.NewInt(0), AirdropEpoch: big.NewInt(0),
		AirdropBalance: big.NewInt(0), Price: big.NewInt(10_000_000_000_000)}, market)
	require.Equal(t, big.NewInt(1), h.view(launchpadv610.GetMarketCountMethod)[0])
	markets := *abi.ConvertType(h.view(launchpadv610.GetMarketsMethod, big.NewInt(0), big.NewInt(10))[0], new([]launchpadv610.Market)).(*[]launchpadv610.Market)
	sameJSON(t, []launchpadv610.Market{market}, markets)
	require.Len(t, h.view(launchpadv610.GetMarketsMethod, big.NewInt(1), big.NewInt(10))[0], 0)
	byCreator := *abi.ConvertType(h.view(launchpadv610.GetMarketsByCreatorMethod, h.caller)[0], new([]launchpadv610.Market)).(*[]launchpadv610.Market)
	sameJSON(t, []launchpadv610.Market{market}, byCreator)
	require.Len(t, h.view(launchpadv610.GetMarketsByCreatorMethod, token)[0], 0)
	sameJSON(t, []interface{}{big.NewInt(10_000_000_000), big.NewInt(0), big.NewInt(1_000_000_000_000_000)},
		h.view(launchpadv610.GetReservesMethod, token))
	require.Equal(t, big.NewInt(10_000_000_000_000), h.view(launchpadv610.GetPriceMethod, token)[0])
	sameJSON(t, []interface{}{[8]*big.Int{big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(0), big.NewInt(0), big.NewInt(0)}, big.NewInt(0), big.NewInt(0)}, h.view(launchpadv610.GetPriceSnapshotsMethod, token))
	require.Equal(t, 0, h.view(launchpadv610.GetAccumulatedFeesMethod, token)[0].(*big.Int).Sign())
	require.Equal(t, []string{"quoteDenom", "virtualQuoteDefault", "virtualTokenDefault", "minFeeBps", "maxFeeBps",
		"baseFeeBps", "protocolFeeBps", "feeDecayRate", "volatilityWeight", "concentrationWeight", "creationFee"},
		h.method(launchpadv610.GetConfigMethod).Outputs[0].Type.TupleRawNames)
	config := *abi.ConvertType(h.view(launchpadv610.GetConfigMethod)[0], new(launchpadv610.Config)).(*launchpadv610.Config)
	sameJSON(t, launchpadv610.Config{QuoteDenom: quote, VirtualQuoteDefault: big.NewInt(10_000_000_000),
		VirtualTokenDefault: big.NewInt(1_000_000_000_000_000), MinFeeBps: big.NewInt(10), MaxFeeBps: big.NewInt(300),
		BaseFeeBps: big.NewInt(30), ProtocolFeeBps: big.NewInt(1000), FeeDecayRate: big.NewInt(500),
		VolatilityWeight: big.NewInt(100), ConcentrationWeight: big.NewInt(100), CreationFee: big.NewInt(100_000_000)}, config)
	require.Equal(t, big.NewInt(300), h.view(launchpadv610.GetFeeBpsMethod, token)[0])
	require.Equal(t, 0, h.view(launchpadv610.GetProtocolFeesMethod, token)[0].(*big.Int).Sign())

	quoted := h.view(launchpadv610.QuoteBuyMethod, token, big.NewInt(100_000_000))
	require.Equal(t, []interface{}{big.NewInt(9_606_813_905_120), big.NewInt(300), big.NewInt(3_000_000)}, quoted)
	out := h.call(h.caller, launchpadv610.BuyMethod, token, big.NewInt(100_000_000), big.NewInt(9_606_813_905_120), h.caller, deadline)
	require.Equal(t, big.NewInt(9_606_813_905_120), out[0])
	require.Equal(t, sdk.NewInt(9_606_813_905_120), h.balance(h.callerAcc, denom))
	require.Equal(t, sdk.NewInt(800_000_000), h.balance(h.callerAcc, quote))
	require.Equal(t, sdk.NewInt(100_300_000), h.balance(h.keeper.TreasuryAddress(), quote))
	require.Equal(t, big.NewInt(300_000), h.view(launchpadv610.GetProtocolFeesMethod, token)[0])
	swaps := h.logs("Swap(address,address,address,bool,uint256,uint256,uint256,uint256)")
	require.Len(t, swaps, 1)
	require.Equal(t, []common.Hash{swaps[0].Topics[0], addressTopic(token), addressTopic(h.caller), addressTopic(h.caller)}, swaps[0].Topics)
	require.Equal(t, big.NewInt(1), word(swaps[0].Data, 0))
	require.Equal(t, big.NewInt(100_000_000), word(swaps[0].Data, 1))
	require.Equal(t, big.NewInt(9_606_813_905_120), word(swaps[0].Data, 2))
	require.Equal(t, big.NewInt(3_000_000), word(swaps[0].Data, 3))
	require.Equal(t, big.NewInt(10_194_940_899_999), word(swaps[0].Data, 4))
	fees := h.logs("FeeRecorded(address,uint256,uint256,uint256)")
	require.Len(t, fees, 1)
	require.Equal(t, big.NewInt(3_000_000), word(fees[0].Data, 0))
	require.Equal(t, big.NewInt(300_000), word(fees[0].Data, 1))
	require.Equal(t, big.NewInt(2_700_000), word(fees[0].Data, 2))

	require.Equal(t, []interface{}{big.NewInt(10_000_000_000), big.NewInt(97_000_000), big.NewInt(990_393_186_094_880)},
		h.view(launchpadv610.GetReservesMethod, token))
	require.Equal(t, big.NewInt(10_194_940_899_999), h.view(launchpadv610.GetPriceMethod, token)[0])
	snapshots := h.view(launchpadv610.GetPriceSnapshotsMethod, token)
	require.Equal(t, big.NewInt(10_194_940_899_999), snapshots[0].([8]*big.Int)[0])
	require.Equal(t, big.NewInt(1), snapshots[1])
	require.Equal(t, big.NewInt(1), snapshots[2])
	require.Equal(t, big.NewInt(2_700_000), h.view(launchpadv610.GetAccumulatedFeesMethod, token)[0])

	require.Equal(t, "LXP_LAUNCHPAD_SLIPPAGE_EXCEEDED", h.reverts(h.caller, launchpadv610.SellMethod, token,
		big.NewInt(4_803_406_952_560), big.NewInt(47_278_913), h.caller, deadline))
	require.Equal(t, []interface{}{big.NewInt(47_278_912), big.NewInt(300), big.NewInt(144_102_208_576)},
		h.view(launchpadv610.QuoteSellMethod, token, big.NewInt(4_803_406_952_560)))
	out = h.call(h.caller, launchpadv610.SellMethod, token, big.NewInt(4_803_406_952_560), big.NewInt(47_278_912), h.caller, deadline)
	require.Equal(t, big.NewInt(47_278_912), out[0])
	require.Equal(t, sdk.NewInt(847_278_912), h.balance(h.callerAcc, quote))
	market = h.market(token)
	require.Equal(t, big.NewInt(49_721_088), market.RealQuoteBalance)
	require.Equal(t, big.NewInt(144_102_208_576), market.AccumulatedTokenFees)
	afterSell := h.view(launchpadv610.GetPriceMethod, token)[0].(*big.Int)
	require.Equal(t, market.Price, afterSell)
	require.Equal(t, -1, afterSell.Cmp(big.NewInt(10_194_940_899_999)), "a sell lowers the price")
	sameJSON(t, []interface{}{[8]*big.Int{big.NewInt(10_194_940_899_999), afterSell, big.NewInt(0), big.NewInt(0),
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0)}, big.NewInt(2), big.NewInt(2)},
		h.view(launchpadv610.GetPriceSnapshotsMethod, token))
	require.Equal(t, big.NewInt(300_000), h.view(launchpadv610.GetProtocolFeesMethod, token)[0])
	require.Len(t, h.logs("FeeRecorded(address,uint256,uint256,uint256)"), 1, "a sell records no quote fee")

	_, other := h.fund(10)
	require.Contains(t, h.reverts(other, launchpadv610.BuyMethod, token, big.NewInt(1_000_000), big.NewInt(0), other, deadline),
		"insufficient funds")
	require.Equal(t, "LXP_LAUNCHPAD_ZERO_ADDRESS", h.reverts(h.caller, launchpadv610.BuyMethod, token,
		big.NewInt(1_000_000), big.NewInt(0), common.Address{}, deadline))
	require.Equal(t, "LXP_LAUNCHPAD_DEADLINE_EXPIRED", h.reverts(h.caller, launchpadv610.BuyMethod, token,
		big.NewInt(1_000_000), big.NewInt(0), h.caller, big.NewInt(genesisTime.Unix()-1)))
	require.Equal(t, fmt.Sprintf("LXP_LAUNCHPAD_UNKNOWN_MARKET: token %s", h.caller.Hex()),
		h.reverts(h.caller, launchpadv610.BuyMethod, h.caller, big.NewInt(1_000_000), big.NewInt(0), h.caller, deadline))
	require.Equal(t, "LXP_LAUNCHPAD_INVALID_STRATEGY: 4", h.reverts(h.caller, launchpadv610.CreateMarketMethod,
		"Kindle Token", "KNDL", uint8(4)))
	h.call(h.caller, launchpadv610.PauseMethod, token)
	pausedMarket := h.market(token)
	quoteBalance, tokenBalance := h.balance(h.callerAcc, quote), h.balance(h.callerAcc, denom)
	require.Equal(t, "LXP_LAUNCHPAD_PAUSED", h.reverts(h.caller, launchpadv610.SellMethod, token,
		big.NewInt(1), big.NewInt(0), h.caller, deadline))
	sameJSON(t, pausedMarket, h.market(token))
	require.Equal(t, quoteBalance, h.balance(h.callerAcc, quote))
	require.Equal(t, tokenBalance, h.balance(h.callerAcc, denom))
	require.Equal(t, big.NewInt(300_000), h.view(launchpadv610.GetProtocolFeesMethod, token)[0])
	sameJSON(t, []interface{}{[8]*big.Int{big.NewInt(10_194_940_899_999), afterSell, big.NewInt(0), big.NewInt(0),
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0)}, big.NewInt(2), big.NewInt(2)},
		h.view(launchpadv610.GetPriceSnapshotsMethod, token))

	h.call(h.caller, launchpadv610.UnpauseMethod, token)
	h.call(h.caller, launchpadv610.SellMethod, token, big.NewInt(1_000_000_000_000), big.NewInt(0), h.caller, deadline)
	third := h.view(launchpadv610.GetPriceMethod, token)[0].(*big.Int)
	require.Equal(t, -1, third.Cmp(afterSell), "a second sell lowers the price again")
	sameJSON(t, []interface{}{[8]*big.Int{big.NewInt(10_194_940_899_999), afterSell, third, big.NewInt(0),
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0)}, big.NewInt(3), big.NewInt(3)},
		h.view(launchpadv610.GetPriceSnapshotsMethod, token))
	market = h.market(token)
	require.Equal(t, third, market.Price)
	sameJSON(t, []interface{}{market.VirtualQuoteReserve, market.RealQuoteBalance, market.TokenReserve},
		h.view(launchpadv610.GetReservesMethod, token))
	require.Equal(t, market.AccumulatedFees, h.view(launchpadv610.GetAccumulatedFeesMethod, token)[0])
	require.Equal(t, big.NewInt(2_700_000), market.AccumulatedFees)
	stored, found := h.keeper.GetMarketByPointer(h.stateDB.Ctx(), token)
	require.True(t, found)
	feeBps, err := h.keeper.FeeBps(h.stateDB.Ctx(), stored)
	require.NoError(t, err)
	require.Equal(t, feeBps, h.view(launchpadv610.GetFeeBpsMethod, token)[0])
	require.Equal(t, big.NewInt(1), h.view(launchpadv610.GetMarketCountMethod)[0])
	message, broken := launchpadkeeper.SolvencyInvariant(h.keeper)(h.stateDB.Ctx())
	require.False(t, broken, message)
	t.Run("TestGetProtocolFeesPerMarket", TestGetProtocolFeesPerMarket)
}

func TestGetProtocolFeesPerMarket(t *testing.T) {
	h := newHarness(t)
	params := h.keeper.GetParams(h.stateDB.Ctx())
	params.MinFeeBps, params.MaxFeeBps, params.BaseFeeBps = 100, 100, 100
	params.ProtocolFeeBps = 2000
	require.NoError(t, h.keeper.UpdateParams(h.stateDB.Ctx(), h.keeper.Authority(), params))
	first, firstDenom := h.create()
	second, secondDenom := h.create()
	require.NotEqual(t, first, second)
	require.NotEqual(t, firstDenom, secondDenom)
	require.Equal(t, 0, h.view(launchpadv610.GetProtocolFeesMethod, first)[0].(*big.Int).Sign())
	require.Equal(t, 0, h.view(launchpadv610.GetProtocolFeesMethod, second)[0].(*big.Int).Sign())
	h.call(h.caller, launchpadv610.BuyMethod, first, big.NewInt(10_000), big.NewInt(0), h.caller, deadline)
	require.Equal(t, big.NewInt(20), h.view(launchpadv610.GetProtocolFeesMethod, first)[0])
	require.Equal(t, 0, h.view(launchpadv610.GetProtocolFeesMethod, second)[0].(*big.Int).Sign())
	h.call(h.caller, launchpadv610.BuyMethod, second, big.NewInt(10_000), big.NewInt(0), h.caller, deadline)
	require.Equal(t, big.NewInt(20), h.view(launchpadv610.GetProtocolFeesMethod, first)[0])
	require.Equal(t, big.NewInt(20), h.view(launchpadv610.GetProtocolFeesMethod, second)[0])
	for _, denom := range []string{firstDenom, secondDenom} {
		fees, err := h.keeper.GetProtocolFees(h.stateDB.Ctx(), denom)
		require.NoError(t, err)
		require.Equal(t, sdk.NewInt(20), fees)
	}
	require.Equal(t, []string{"quoteDenom", "virtualQuoteDefault", "virtualTokenDefault", "minFeeBps", "maxFeeBps",
		"baseFeeBps", "protocolFeeBps", "feeDecayRate", "volatilityWeight", "concentrationWeight", "creationFee"},
		h.method(launchpadv610.GetConfigMethod).Outputs[0].Type.TupleRawNames)
	config := *abi.ConvertType(h.view(launchpadv610.GetConfigMethod)[0], new(launchpadv610.Config)).(*launchpadv610.Config)
	require.Equal(t, big.NewInt(2000), config.ProtocolFeeBps)
	_, reason := h.run(h.caller, launchpadv610.GetProtocolFeesMethod, nil, true, false, h.caller)
	require.Equal(t, fmt.Sprintf("LXP_LAUNCHPAD_UNKNOWN_MARKET: token %s", h.caller.Hex()), reason)
}

func TestFeeRightsAndGuardianThroughThePrecompile(t *testing.T) {
	h := newHarness(t)
	token, denom := h.create()
	_, trader := h.fund(2_000_000_000)
	strangerAcc, stranger := h.fund(0)
	h.call(trader, launchpadv610.BuyMethod, token, big.NewInt(333_333_334), big.NewInt(0), trader, deadline)
	require.Equal(t, big.NewInt(9_000_000), h.view(launchpadv610.GetAccumulatedFeesMethod, token)[0])

	require.Equal(t, "LXP_LAUNCHPAD_NOT_FEE_RIGHTS_HOLDER", h.reverts(stranger, launchpadv610.ClaimFeesMethod, token, stranger))
	require.Equal(t, "LXP_LAUNCHPAD_WRONG_STRATEGY", h.reverts(h.caller, launchpadv610.ExecuteBurnMethod, token))
	out := h.call(h.caller, launchpadv610.ClaimFeesMethod, token, stranger)
	require.Equal(t, big.NewInt(9_000_000), out[0])
	require.Equal(t, sdk.NewInt(9_000_000), h.balance(strangerAcc, quote))
	claimed := h.logs("FeesClaimed(address,address,uint256)")
	require.Len(t, claimed, 1)
	require.Equal(t, []common.Hash{claimed[0].Topics[0], addressTopic(token), addressTopic(stranger)}, claimed[0].Topics)
	require.Equal(t, "LXP_LAUNCHPAD_NO_FEES_ACCUMULATED", h.reverts(h.caller, launchpadv610.ClaimFeesMethod, token, stranger))

	require.Equal(t, uint8(0), h.call(h.caller, launchpadv610.SetFeeStrategyMethod, token, uint8(types.FeeStrategyAirdrop))[0])
	require.Len(t, h.logs("FeeStrategyChanged(address,uint8,uint8)"), 1)
	require.Equal(t, "LXP_LAUNCHPAD_AIRDROP_NOT_TRIGGERED", h.reverts(stranger, launchpadv610.ClaimAirdropMethod, token))
	h.call(trader, launchpadv610.BuyMethod, token, big.NewInt(333_333_334), big.NewInt(0), trader, deadline)
	require.Equal(t, big.NewInt(9_000_000), h.call(h.caller, launchpadv610.ExecuteAirdropMethod, token)[0])
	airdrops := h.logs("AirdropExecuted(address,uint256,uint256)")
	require.Len(t, airdrops, 1)
	require.Equal(t, big.NewInt(1), word(airdrops[0].Data, 1))
	require.Equal(t, big.NewInt(1), h.market(token).AirdropEpoch)
	traderAcc := testkeeper.EVMTestApp.EvmKeeper.GetPaxAddressOrDefault(h.stateDB.Ctx(), trader)
	holding := h.balance(traderAcc, denom).BigInt()
	want := new(big.Int).Quo(new(big.Int).Mul(big.NewInt(9_000_000), holding), big.NewInt(1_000_000_000_000_000))
	require.Equal(t, want, h.call(trader, launchpadv610.ClaimAirdropMethod, token)[0])
	require.Equal(t, "LXP_LAUNCHPAD_ALREADY_CLAIMED", h.reverts(trader, launchpadv610.ClaimAirdropMethod, token))
	require.Equal(t, "LXP_LAUNCHPAD_ZERO_AMOUNT", h.reverts(stranger, launchpadv610.ClaimAirdropMethod, token))
	require.Len(t, h.logs("AirdropClaimed(address,address,uint256,uint256)"), 1)

	h.call(h.caller, launchpadv610.SetFeeStrategyMethod, token, uint8(types.FeeStrategyLpRewards))
	h.call(trader, launchpadv610.BuyMethod, token, big.NewInt(333_333_334), big.NewInt(0), trader, deadline)
	real := h.market(token).RealQuoteBalance
	require.Equal(t, big.NewInt(9_000_000), h.call(h.caller, launchpadv610.ExecuteLpRewardsMethod, token)[0])
	require.Equal(t, new(big.Int).Add(real, big.NewInt(9_000_000)), h.market(token).RealQuoteBalance)
	require.Len(t, h.logs("LpRewardsExecuted(address,uint256)"), 1)

	h.call(h.caller, launchpadv610.SetFeeStrategyMethod, token, uint8(types.FeeStrategyBurn))
	h.call(trader, launchpadv610.BuyMethod, token, big.NewInt(333_333_334), big.NewInt(0), trader, deadline)
	require.Equal(t, big.NewInt(9_000_000), h.call(h.caller, launchpadv610.ExecuteBurnMethod, token)[0])
	require.Len(t, h.logs("FeesBurned(address,uint256)"), 1)

	require.Equal(t, "LXP_LAUNCHPAD_NOT_GUARDIAN", h.reverts(stranger, launchpadv610.PauseMethod, token))
	require.Equal(t, true, h.call(h.caller, launchpadv610.PauseMethod, token)[0])
	require.True(t, h.market(token).Paused)
	require.Equal(t, "LXP_LAUNCHPAD_PAUSED", h.reverts(trader, launchpadv610.BuyMethod, token, big.NewInt(1_000_000), big.NewInt(0), trader, deadline))
	require.Equal(t, "LXP_LAUNCHPAD_PAUSED", h.reverts(h.caller, launchpadv610.PauseMethod, token))
	h.call(h.caller, launchpadv610.UnpauseMethod, token)
	require.Len(t, h.logs("PauseToggled(address,bool)"), 2)
	h.call(trader, launchpadv610.BuyMethod, token, big.NewInt(1_000_000), big.NewInt(0), trader, deadline)
	message, broken := launchpadkeeper.SolvencyInvariant(h.keeper)(h.stateDB.Ctx())
	require.False(t, broken, message)
}

func TestRejectsDelegatecallStaticcallWritesAndValue(t *testing.T) {
	h := newHarness(t)
	token, _ := h.create()
	_, reason := h.run(h.caller, launchpadv610.BuyMethod, nil, false, true, token, big.NewInt(1_000_000), big.NewInt(0), h.caller, deadline)
	require.Contains(t, reason, "delegatecall")
	_, reason = h.run(h.caller, launchpadv610.GetMarketCountMethod, nil, true, true)
	require.Contains(t, reason, "delegatecall")
	_, reason = h.run(h.caller, launchpadv610.BuyMethod, nil, true, false, token, big.NewInt(1_000_000), big.NewInt(0), h.caller, deadline)
	require.Contains(t, reason, "staticcall")
	_, reason = h.run(h.caller, launchpadv610.CreateMarketMethod, nil, true, false, "Kindle Token", "KNDL", uint8(0))
	require.Contains(t, reason, "staticcall")
	_, reason = h.run(h.caller, launchpadv610.PauseMethod, nil, true, false, token)
	require.Contains(t, reason, "staticcall")
	_, reason = h.run(h.caller, launchpadv610.BuyMethod, big.NewInt(1), false, false, token, big.NewInt(1_000_000), big.NewInt(0), h.caller, deadline)
	require.NotEmpty(t, reason)
	require.Equal(t, big.NewInt(1), h.view(launchpadv610.GetMarketCountMethod)[0])
	require.True(t, h.market(token).RealQuoteBalance.Sign() == 0, "no rejected call moved funds")
}

func TestClaimAirdropForEpochThroughThePrecompile(t *testing.T) {
	h := newHarness(t)
	token, denom := h.create()
	_, trader := h.fund(4_000_000_000)
	strangerAcc, stranger := h.fund(0)
	h.call(h.caller, launchpadv610.SetFeeStrategyMethod, token, uint8(types.FeeStrategyAirdrop))
	require.Equal(t, "LXP_LAUNCHPAD_INVALID_AIRDROP_EPOCH: "+denom+"/1", h.reverts(trader, launchpadv610.ClaimAirdropForEpochMethod, token, uint64(1)))
	h.call(trader, launchpadv610.BuyMethod, token, big.NewInt(333_333_334), big.NewInt(0), trader, deadline)
	require.Equal(t, big.NewInt(9_000_000), h.call(h.caller, launchpadv610.ExecuteAirdropMethod, token)[0])
	traderAcc := testkeeper.EVMTestApp.EvmKeeper.GetPaxAddressOrDefault(h.stateDB.Ctx(), trader)
	holding := h.balance(traderAcc, denom).BigInt()
	supply := testkeeper.EVMTestApp.BankKeeper.GetSupply(h.stateDB.Ctx(), denom).Amount.BigInt()
	want1 := new(big.Int).Quo(new(big.Int).Mul(big.NewInt(9_000_000), holding), supply)

	// After the boundary the trader moves its whole holding to the stranger
	// and a second epoch opens; epoch 1 still pays the trader, never the stranger.
	require.NoError(t, testkeeper.EVMTestApp.BankKeeper.SendCoins(h.stateDB.Ctx(), traderAcc, strangerAcc,
		sdk.NewCoins(sdk.NewCoin(denom, sdk.NewIntFromBigInt(holding)))))
	h.call(trader, launchpadv610.BuyMethod, token, big.NewInt(333_333_334), big.NewInt(0), trader, deadline)
	require.Equal(t, big.NewInt(9_000_000), h.call(h.caller, launchpadv610.ExecuteAirdropMethod, token)[0])
	require.Equal(t, "LXP_LAUNCHPAD_INVALID_AIRDROP_EPOCH: "+denom+"/0", h.reverts(trader, launchpadv610.ClaimAirdropForEpochMethod, token, uint64(0)))
	require.Equal(t, "LXP_LAUNCHPAD_INVALID_AIRDROP_EPOCH: "+denom+"/3", h.reverts(trader, launchpadv610.ClaimAirdropForEpochMethod, token, uint64(3)))
	require.Equal(t, "LXP_LAUNCHPAD_ZERO_AMOUNT", h.reverts(stranger, launchpadv610.ClaimAirdropForEpochMethod, token, uint64(1)))
	require.Equal(t, want1, h.call(trader, launchpadv610.ClaimAirdropForEpochMethod, token, uint64(1))[0])
	require.Equal(t, "LXP_LAUNCHPAD_ALREADY_CLAIMED", h.reverts(trader, launchpadv610.ClaimAirdropForEpochMethod, token, uint64(1)))
	claimed := h.logs("AirdropClaimed(address,address,uint256,uint256)")
	require.Len(t, claimed, 1)
	require.Equal(t, big.NewInt(1), word(claimed[0].Data, 1))
	require.Equal(t, uint64(3000+16*64+5000*4), h.precompile.RequiredGas(h.input(launchpadv610.ClaimAirdropForEpochMethod, token, uint64(1))))
	message, broken := launchpadkeeper.SolvencyInvariant(h.keeper)(h.stateDB.Ctx())
	require.False(t, broken, message)
}
