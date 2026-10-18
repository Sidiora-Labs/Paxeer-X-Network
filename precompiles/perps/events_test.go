package perps_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"math/big"
	"slices"
	"testing"
	"time"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	perpskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	perpstypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const (
	// eventsGenesisSeconds is the first block time, a whole hour.
	eventsGenesisSeconds int64  = 1_800_000_000
	eventsGas            uint64 = 30_000_000
	// eventsTreasuryUnits is the USDL supply minted to the treasury.
	eventsTreasuryUnits int64 = 100_000_000_000_000
)

var (
	eventsVenue         = common.HexToAddress("0x0000000000000000000000000000000000001015")
	eventsTransferTopic = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	eventsCloid         = [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	eventsZero          = new(big.Int)
)

// eventsKeepers carries the application keepers and the perps keeper of the
// test store into NewPrecompile.
type eventsKeepers struct {
	utils.Keepers
	perps *perpskeeper.Keeper
}

func (k eventsKeepers) PerpsK() *perpskeeper.Keeper { return k.perps }

// eventsOrder is the OrderParams tuple of placeOrder.
type eventsOrder struct {
	MarketId         *big.Int
	IsLong           bool
	OrderType        uint8
	TimeInForce      uint8
	SizeUsd          *big.Int
	Leverage         *big.Int
	LimitPrice       *big.Int
	TriggerPrice     *big.Int
	AcceptablePrice  *big.Int
	CollateralAmount *big.Int
	ReduceOnly       bool
	TakeProfitPrice  *big.Int
	StopLossPrice    *big.Int
	Deadline         uint64
	Cloid            [16]byte
}

func newOrder(isLong bool, orderType perpstypes.OrderType, tif perpstypes.TimeInForce, sizeUsd, leverage *big.Int) eventsOrder {
	return eventsOrder{MarketId: new(big.Int), IsLong: isLong, OrderType: uint8(orderType), TimeInForce: uint8(tif),
		SizeUsd: sizeUsd, Leverage: leverage, LimitPrice: new(big.Int), TriggerPrice: new(big.Int),
		AcceptablePrice: new(big.Int), CollateralAmount: new(big.Int), TakeProfitPrice: new(big.Int),
		StopLossPrice: new(big.Int)}
}

// eventsEnv is the perps keeper over the real pricefeed keeper, each on its
// own store, with the USDL ERC20 deployed in the real EVM, BTC (0) listed as
// class A and the precompile served at 0x1015.
type eventsEnv struct {
	t          *testing.T
	app        *app.App
	k          *perpskeeper.Keeper
	ctx        sdk.Context
	key        sdk.StoreKey
	pfKey      sdk.StoreKey
	abi        abi.ABI
	precompile vm.PrecompiledContract
	usdl       common.Address
	treasury   common.Address
	price      int64
}

func eventsAddress(name string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte(name))[12:])
}

func newEventsEnv(t *testing.T) *eventsEnv {
	t.Helper()
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(10).
		WithBlockTime(time.Unix(eventsGenesisSeconds, 0).UTC())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	pfKey := sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	pricefeed := pricefeedkeeper.NewKeeper(pfKey, testApp.StakingKeeper, testApp.AccountKeeper, &testApp.EvmKeeper)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, pfKey)
	key := sdk.NewKVStoreKey(perpstypes.StoreKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, key)
	k := perpskeeper.NewKeeper(key, &testApp.EvmKeeper, pricefeed)
	parsed, err := perpsabi.ABI()
	require.NoError(t, err)
	e := &eventsEnv{t: t, app: testApp, k: k, ctx: ctx, key: key, pfKey: pfKey, abi: parsed,
		treasury: eventsAddress("usdl treasury")}
	e.deployUsdl()

	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	params.CollateralToken = e.usdl
	require.NoError(t, k.UpdateParams(ctx, k.Authority(), params))
	require.NoError(t, k.SetMarket(ctx, k.Authority(), perpstypes.Market{
		ID: 0, Symbol: "BTC", Name: "Bitcoin", OracleSymbol: "BTC", SzDecimals: 5, PxDecimals: 1,
		MaxLeverage: perpstypes.VenueMaxLeverage, TierClass: perpstypes.TierClassA,
		ImpactNotionalUsd:    sdk.NewIntWithDecimal(20_000, 18),
		OiCapUsd:             sdk.NewIntWithDecimal(1_000_000_000, 18),
		FundingCapPpmPerHour: 40_000, Enabled: true,
	}))
	precompile, err := perps.NewPrecompile(eventsKeepers{Keepers: testApp.GetPrecompileKeepers(), perps: k})
	require.NoError(t, err)
	e.precompile = precompile
	e.setPrice(60_000)
	return e
}

func (e *eventsEnv) deployUsdl() {
	e.usdl = installAcctUsdl(e.t, e.ctx, e.app, e.treasury, big.NewInt(eventsTreasuryUnits))
	require.NotEmpty(e.t, e.app.EvmKeeper.GetCode(e.ctx, e.usdl))
}

func word(value *big.Int) []byte { return common.LeftPadBytes(value.Bytes(), 32) }

func units(n int64) *big.Int { return big.NewInt(n) }

// erc20 runs a USDL method from caller and requires it to return true.
func (e *eventsEnv) erc20(caller common.Address, signature string, words ...[]byte) {
	e.t.Helper()
	input := crypto.Keccak256([]byte(signature))[:4]
	for _, w := range words {
		input = append(input, w...)
	}
	ret, err := e.app.EvmKeeper.CallEVM(e.ctx, caller, &e.usdl, nil, input)
	require.NoError(e.t, err)
	require.Equal(e.t, word(big.NewInt(1)), ret)
}

func (e *eventsEnv) fund(holder common.Address, amount int64) {
	e.erc20(e.treasury, "transfer(address,uint256)", common.LeftPadBytes(holder.Bytes(), 32), word(big.NewInt(amount)))
}

func (e *eventsEnv) approve(holder common.Address, amount int64) {
	e.erc20(holder, "approve(address,uint256)", common.LeftPadBytes(eventsVenue.Bytes(), 32), word(big.NewInt(amount)))
}

// holder funds a wallet with USDL and approves the venue for all of it.
func (e *eventsEnv) holder(name string, amount int64) common.Address {
	address := eventsAddress(name)
	e.fund(address, amount)
	e.approve(address, amount)
	return address
}

// trader funds a wallet and deposits all of it into its perps account.
func (e *eventsEnv) trader(name string, amount int64) common.Address {
	address := e.holder(name, amount)
	e.tx(address, "deposit", e.usdl, big.NewInt(amount))
	return address
}

// tx calls the precompile from caller through the EVM, requires success,
// commits the state and returns the logs of the call and its outputs.
func (e *eventsEnv) tx(from common.Address, method string, args ...interface{}) ([]*ethtypes.Log, []interface{}) {
	e.t.Helper()
	input, err := e.abi.Pack(method, args...)
	require.NoError(e.t, err)
	stateDB := state.NewDBImpl(e.ctx, &e.app.EvmKeeper, false)
	blockCtx, err := e.app.EvmKeeper.GetVMBlockContext(e.ctx, core.GasPool(math.MaxUint64))
	require.NoError(e.t, err)
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(e.app.EvmKeeper.ChainID(e.ctx))
	contracts := map[common.Address]vm.PrecompiledContract{}
	maps.Copy(contracts, e.app.EvmKeeper.CustomPrecompiles(e.ctx))
	contracts[eventsVenue] = e.precompile
	evm := vm.NewEVM(*blockCtx, stateDB, cfg, vm.Config{}, contracts)
	ret, _, err := evm.Call(from, eventsVenue, input, eventsGas, uint256.NewInt(0))
	require.NoError(e.t, err, "%s reverted with 0x%x", method, ret)
	logs := stateDB.GetAllLogs()
	_, err = stateDB.Finalize()
	require.NoError(e.t, err)
	outputs, err := e.abi.Unpack(method, ret)
	require.NoError(e.t, err)
	return logs, outputs
}

// place submits placeOrder and returns its logs and the order id.
func (e *eventsEnv) place(from common.Address, order eventsOrder) ([]*ethtypes.Log, *big.Int) {
	e.t.Helper()
	logs, out := e.tx(from, "placeOrder", order)
	require.Len(e.t, out, 1)
	return logs, out[0].(*big.Int)
}

// endBlock runs the EndBlock pass and returns the logs of the block's perps
// synthetic receipt, every one from 0x1015.
func (e *eventsEnv) endBlock() []*ethtypes.Log {
	e.t.Helper()
	require.NotEmpty(e.t, e.k.EndBlock(e.ctx))
	kind := evmkeeper.SyntheticReceiptPerps
	receipt, err := e.app.EvmKeeper.GetTransientReceipt(e.ctx, evmkeeper.SyntheticReceiptHash(kind, e.ctx.BlockHeight()),
		uint64(evmkeeper.SyntheticReceiptProvisionalIndex(kind)))
	require.NoError(e.t, err)
	logs := evmkeeper.GetLogsForTx(receipt, 0)
	for _, l := range logs {
		require.Equal(e.t, eventsVenue, l.Address)
	}
	return logs
}

// setAggregate stores the pricefeed aggregate of BTC at the block time.
func (e *eventsEnv) setAggregate(index, mark int64) {
	bz, err := json.Marshal(pricefeedtypes.Aggregate{
		IndexPrice:  sdk.NewIntWithDecimal(index, 18),
		MarkPrice:   sdk.NewIntWithDecimal(mark, 18),
		Basis:       sdk.ZeroInt(),
		TimestampMs: uint64(e.ctx.BlockTime().UnixMilli()),
		Height:      e.ctx.BlockHeight(),
		Sources:     1,
	})
	require.NoError(e.t, err)
	e.ctx.KVStore(e.pfKey).Set(pricefeedtypes.AggregateKey(0), bz)
}

func (e *eventsEnv) marketState() perpstypes.MarketState {
	state, err := e.k.GetMarketState(e.ctx, 0)
	require.NoError(e.t, err)
	return state
}

// setPrice sets the pricefeed index and mark of BTC and the stored index,
// pricefeed mark and mark to whole USD, fresh at the block time; open
// interest and the funding index are kept, and the hourly index starts at the
// hour of the first price.
func (e *eventsEnv) setPrice(whole int64) {
	e.price = whole
	e.setAggregate(whole, whole)
	state := e.marketState()
	price := *uint256.MustFromBig(usd(whole))
	state.IndexPrice, state.PricefeedMark, state.MarkPrice = price, price, price
	state.Fresh = true
	state.PriceTimestampMs = uint64(e.ctx.BlockTime().UnixMilli())
	if state.LastIndexHour == 0 {
		state.LastIndexHour = uint64(e.ctx.BlockTime().Unix()) / 3600
	}
	e.k.SetMarketState(e.ctx, state)
}

// advance moves to the next block, seconds later, at the same price.
func (e *eventsEnv) advance(seconds int64) {
	e.ctx = e.ctx.WithBlockHeight(e.ctx.BlockHeight() + 1).
		WithBlockTime(e.ctx.BlockTime().Add(time.Duration(seconds) * time.Second))
	e.setPrice(e.price)
}

func (e *eventsEnv) setFundingIndex(index *big.Int) {
	state := e.marketState()
	state.FundingIndex = *uint256.MustFromBig(index)
	e.k.SetMarketState(e.ctx, state)
}

func (e *eventsEnv) addOpenInterest(isLong bool, size *big.Int) {
	state := e.marketState()
	delta := uint256.MustFromBig(size)
	if isLong {
		state.LongOpenInterest.Add(&state.LongOpenInterest, delta)
	} else {
		state.ShortOpenInterest.Add(&state.ShortOpenInterest, delta)
	}
	e.k.SetMarketState(e.ctx, state)
}

func (e *eventsEnv) collateral(address common.Address) uint64 {
	account, err := e.k.GetAccount(e.ctx, address)
	require.NoError(e.t, err)
	return account.Collateral.Uint64()
}

// positionOf reads the account's BTC position id from its market index.
func (e *eventsEnv) positionOf(address common.Address) *big.Int {
	bz := e.ctx.KVStore(e.key).Get(perpstypes.AccountMarketPositionKey(address, 0))
	require.Len(e.t, bz, 32)
	return new(big.Int).SetBytes(bz)
}

// seedMakerShare books 20,000 USD of maker volume for the account and
// 1,000,000 USD of venue volume on the block's day: a 2% maker share, rebate
// tier 2 (-20 ppm), with taker tier 0.
func (e *eventsEnv) seedMakerShare(address common.Address) {
	day := uint64(e.ctx.BlockTime().Unix() / 86400)
	taker, maker, venue := uint256.Int{}, uint256.MustFromBig(usd(20_000)), uint256.MustFromBig(usd(1_000_000))
	takerWord, makerWord, venueWord := taker.Bytes32(), maker.Bytes32(), venue.Bytes32()
	store := e.ctx.KVStore(e.key)
	store.Set(perpstypes.VolumeKey(address, day), append(takerWord[:], makerWord[:]...))
	store.Set(perpstypes.VenueVolumeKey(day), venueWord[:])
}

// startPositionIDs moves the position id sequence past the ids the tests
// store directly.
func (e *eventsEnv) startPositionIDs() {
	next := uint256.NewInt(100).Bytes32()
	e.ctx.KVStore(e.key).Set(perpstypes.SequenceKey(perpstypes.SequencePositionID), next[:])
}

// storeAccount stores an account holding collateral USDL units.
func (e *eventsEnv) storeAccount(address common.Address, collateral uint64) {
	store := e.ctx.KVStore(e.key)
	account := perpstypes.Account{Address: address, Created: true}
	if bz := store.Get(perpstypes.AccountKey(address)); bz != nil {
		require.NoError(e.t, account.Unmarshal(bz))
	}
	account.Collateral = *uint256.NewInt(collateral)
	store.Set(perpstypes.AccountKey(address), account.Marshal())
}

// storePosition stores a BTC position of size at reference and entry price
// with its account index, its ADL rank entry and its open interest.
func (e *eventsEnv) storePosition(id uint64, address common.Address, isLong bool, size *big.Int, price int64, rank *big.Int) {
	positionID := *uint256.NewInt(id)
	sizeBase, entry := uint256.MustFromBig(size), uint256.MustFromBig(usd(price))
	var cost uint256.Int
	cost.Mul(sizeBase, entry)
	cost.Div(&cost, uint256.NewInt(1_000_000_000_000_000_000))
	position := perpstypes.Position{ID: positionID, Account: address, MarketID: 0, IsLong: isLong, SizeBase: *sizeBase,
		ReferencePrice: *entry, CostBasisUsd: cost, EntryPrice: *entry, Leverage: *uint256.MustFromBig(usd(100)),
		RankScore: *uint256.MustFromBig(rank), OpenedAt: uint64(e.ctx.BlockTime().Unix())}
	store := e.ctx.KVStore(e.key)
	store.Set(perpstypes.PositionKey(positionID), position.Marshal())
	store.Set(perpstypes.AdlRankIndexKey(0, perpstypes.SideOf(isLong), position.RankScore, positionID), []byte{})
	idWord := positionID.Bytes32()
	store.Set(perpstypes.AccountMarketPositionKey(address, 0), idWord[:])
	account, err := e.k.GetAccount(e.ctx, address)
	require.NoError(e.t, err)
	account.PositionCount++
	store.Set(perpstypes.AccountKey(address), account.Marshal())
	e.addOpenInterest(isLong, size)
}

func (e *eventsEnv) refresh(addresses ...common.Address) {
	for _, address := range addresses {
		_, err := e.k.RefreshMarginRatio(e.ctx, address)
		require.NoError(e.t, err)
	}
}

// expected packs the topics and data of the named event of ABI().
func (e *eventsEnv) expected(name string, args ...interface{}) ([]common.Hash, []byte) {
	e.t.Helper()
	event, ok := e.abi.Events[name]
	require.True(e.t, ok, "event %s", name)
	require.Len(e.t, args, len(event.Inputs), "arguments of %s", name)
	topics := []common.Hash{event.ID}
	var data []interface{}
	for i, input := range event.Inputs {
		if !input.Indexed {
			data = append(data, args[i])
			continue
		}
		rows, err := abi.MakeTopics([]interface{}{args[i]})
		require.NoError(e.t, err)
		topics = append(topics, rows[0][0])
	}
	packed, err := event.Inputs.NonIndexed().Pack(data...)
	require.NoError(e.t, err)
	return topics, packed
}

// requireLog requires a log of 0x1015 with exactly the topics and data of
// the event and returns the index of the first one.
func (e *eventsEnv) requireLog(logs []*ethtypes.Log, name string, args ...interface{}) int {
	e.t.Helper()
	topics, data := e.expected(name, args...)
	var logged []string
	for i, l := range logs {
		if len(l.Topics) == 0 || l.Topics[0] != topics[0] {
			continue
		}
		require.Equal(e.t, eventsVenue, l.Address, "%s is logged from 0x1015", name)
		if slices.Equal(l.Topics, topics) && bytes.Equal(l.Data, data) {
			return i
		}
		logged = append(logged, fmt.Sprintf("topics %v data 0x%x", l.Topics, l.Data))
	}
	require.Failf(e.t, "log not found", "%s topics %v data 0x%x; logged %v", name, topics, data, logged)
	return -1
}

// indices returns the indices of the logs of the named event.
func (e *eventsEnv) indices(logs []*ethtypes.Log, name string) []int {
	id := e.abi.Events[name].ID
	var found []int
	for i, l := range logs {
		if len(l.Topics) > 0 && l.Topics[0] == id {
			found = append(found, i)
		}
	}
	return found
}

// values unpacks the non-indexed arguments of an event log.
func (e *eventsEnv) values(name string, l *ethtypes.Log) []interface{} {
	e.t.Helper()
	values, err := e.abi.Events[name].Inputs.NonIndexed().Unpack(l.Data)
	require.NoError(e.t, err)
	return values
}

// requireTransfer requires the USDL Transfer(from, to, amount) log and
// returns its index.
func (e *eventsEnv) requireTransfer(logs []*ethtypes.Log, from, to common.Address, amount int64) int {
	e.t.Helper()
	want := []common.Hash{eventsTransferTopic, common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes())}
	for i, l := range logs {
		if l.Address == e.usdl && slices.Equal(l.Topics, want) && bytes.Equal(l.Data, word(big.NewInt(amount))) {
			return i
		}
	}
	require.Failf(e.t, "transfer not found", "Transfer(%s, %s, %d)", from, to, amount)
	return -1
}

// fillScenario rests A's 0.01 BTC ask at 60,000 and fills it with B's
// opening IOC buy: a 600 USD fill at tier-0 rates, or with A at maker
// rebate tier 2 when rebate is set.
func fillScenario(e *eventsEnv, rebate bool) (a, b common.Address, makerID, takerID *big.Int, logs []*ethtypes.Log) {
	a = e.trader("account A", 10_000_000_000)
	if rebate {
		e.seedMakerShare(a)
		e.storeAccount(e.k.FeesAccountAddress(), 1_000_000)
	}
	_, makerID = e.place(a, eventsLimitOrder(false, perpstypes.TimeInForceGTC, usd(60_000), usd(600), usd(10)))
	b = e.trader("account B", 10_000_000_000)
	logs, takerID = e.place(b, eventsLimitOrder(true, perpstypes.TimeInForceIOC, usd(60_000), usd(600), usd(10)))
	return a, b, makerID, takerID, logs
}

// openScenario rests C's ask of askUsd at 60,000 and opens A's long through
// openPosition(0, usdl, 100000000, 10e18, true): 0.01650 BTC at 60,000.
func openScenario(e *eventsEnv, askUsd int64) (a common.Address, position *big.Int, logs []*ethtypes.Log) {
	c := e.trader("account C", 100_000_000_000)
	e.place(c, eventsLimitOrder(false, perpstypes.TimeInForceGTC, usd(60_000), usd(askUsd), usd(10)))
	a = e.holder("account A", 100_000_000)
	logs, out := e.tx(a, "openPosition", new(big.Int), e.usdl, units(100_000_000), usd(10), true)
	require.Len(e.t, out, 1)
	position = out[0].(*big.Int)
	require.Equal(e.t, e.positionOf(a), position)
	require.Equal(e.t, uint64(99_554_500), e.collateral(a))
	return a, position, logs
}

// addSizeScenario grows A's 0.01650 BTC long by 0.00798 BTC at 62,000
// through addSize(position, 50000000, 10e18).
func addSizeScenario(e *eventsEnv) (common.Address, *big.Int, []*ethtypes.Log) {
	a, position, _ := openScenario(e, 990)
	e.setPrice(62_000)
	c := eventsAddress("account C")
	e.place(c, eventsLimitOrder(false, perpstypes.TimeInForceGTC, usd(62_000), usd(6_200), usd(10)))
	e.fund(a, 50_000_000)
	e.approve(a, 50_000_000)
	logs, _ := e.tx(a, "addSize", position, units(50_000_000), usd(10))
	return a, position, logs
}

// closeScenario closes A's 0.01650 BTC long into D's bid at 61,000 after the
// funding index advanced so that the long pays 0.05 USD.
func closeScenario(e *eventsEnv) (common.Address, *big.Int, []*ethtypes.Log) {
	a, position, _ := openScenario(e, 990)
	e.setFundingIndex(fixed("3.030303030303030303"))
	e.setPrice(61_000)
	d := e.trader("account D", 100_000_000_000)
	e.place(d, eventsLimitOrder(true, perpstypes.TimeInForceGTC, usd(61_000), fixed("1006.5"), usd(10)))
	logs, _ := e.tx(a, "closePosition", position)
	return a, position, logs
}

// restingLimit places A's GTC buy of 6,000 USD at 59,000 at 20x with cloid
// 0x0102...10 and the given deadline from 1,000 USDL of collateral.
func restingLimit(e *eventsEnv, deadline uint64) (common.Address, *big.Int, []*ethtypes.Log) {
	a := e.trader("account A", 1_000_000_000)
	order := eventsLimitOrder(true, perpstypes.TimeInForceGTC, usd(59_000), usd(6_000), usd(20))
	order.Deadline = deadline
	order.Cloid = eventsCloid
	logs, id := e.place(a, order)
	return a, id, logs
}

// backstopScenario stores A's 1 BTC long entered at entry with collateral
// USDL units, its counterparty short, and the insurance fund's collateral,
// and runs EndBlock at mark 60,000.
func backstopScenario(e *eventsEnv, collateral uint64, size string, entry int64, fund uint64) (common.Address, []*ethtypes.Log) {
	e.startPositionIDs()
	if fund > 0 {
		e.storeAccount(e.k.InsuranceAccountAddress(), fund)
	}
	a := eventsAddress("account A")
	e.storeAccount(a, collateral)
	e.storePosition(7, a, true, fixed(size), entry, eventsZero)
	d := eventsAddress("account D")
	e.storeAccount(d, 1_000_000_000_000)
	e.storePosition(8, d, false, fixed(size), entry, eventsZero)
	e.refresh(a, d)
	e.advance(1)
	return a, e.endBlock()
}

// deficitScenario backstops A's 0.1 BTC long entered at 120,000 with no
// collateral, 6,000 USD below zero at 60,000, against a 5,000 USD fund;
// counterparty short 9 of C ranks first with score 5.5e18.
func deficitScenario(e *eventsEnv) (common.Address, []*ethtypes.Log) {
	e.startPositionIDs()
	e.storeAccount(e.k.InsuranceAccountAddress(), 5_000_000_000)
	a := eventsAddress("account A")
	e.storeAccount(a, 0)
	e.storePosition(7, a, true, fixed("0.1"), 120_000, eventsZero)
	c := eventsAddress("account C")
	e.storeAccount(c, 1_000_000_000_000)
	e.storePosition(9, c, false, fixed("0.1"), 120_000, fixed("5.5"))
	e.refresh(a, c)
	e.advance(1)
	return c, e.endBlock()
}

func TestEventVaultCreated(t *testing.T) {
	e := newEventsEnv(t)
	a := eventsAddress("account A")
	logs, out := e.tx(a, "createVault")
	require.Equal(t, []interface{}{eventsVenue}, out)
	require.Len(t, logs, 1)
	require.Equal(t, 0, e.requireLog(logs, "VaultCreated", a, eventsVenue))

	logs, out = e.tx(a, "createVault")
	require.Equal(t, []interface{}{eventsVenue}, out)
	require.Empty(t, logs)
}

func TestEventCollateralDeposited(t *testing.T) {
	e := newEventsEnv(t)
	a := e.holder("account A", 250_000_000)
	logs, _ := e.tx(a, "deposit", e.usdl, units(250_000_000))
	deposited := e.requireLog(logs, "CollateralDeposited", a, units(250_000_000), units(250_000_000))
	transfer := e.requireTransfer(logs, a, e.k.ModuleEVMAddress(), 250_000_000)
	require.Less(t, transfer, deposited)
	require.Len(t, e.indices(logs, "CollateralDeposited"), 1)
}

func TestEventCollateralWithdrawn(t *testing.T) {
	e := newEventsEnv(t)
	a := e.trader("account A", 250_000_000)
	logs, _ := e.tx(a, "withdraw", e.usdl, units(150_000_000))
	e.requireLog(logs, "CollateralWithdrawn", a, units(150_000_000), units(100_000_000))
	e.requireTransfer(logs, e.k.ModuleEVMAddress(), a, 150_000_000)
	require.Len(t, e.indices(logs, "CollateralWithdrawn"), 1)
}

func TestEventCollateralTransferred(t *testing.T) {
	e := newEventsEnv(t)
	a := e.trader("account A", 250_000_000)
	b := e.trader("account B", 1_000_000)
	logs, _ := e.tx(a, "transferCollateral", b, units(60_000_000))
	e.requireLog(logs, "CollateralTransferred", a, b, units(60_000_000))
	for _, l := range logs {
		require.NotEqual(t, eventsTransferTopic, l.Topics[0], "no USDL Transfer moves for an internal transfer")
	}
}

func TestEventOrderPlaced(t *testing.T) {
	e := newEventsEnv(t)
	a, id, logs := restingLimit(e, 0)
	require.Equal(t, big.NewInt(1), id)
	e.requireLog(logs, "OrderPlaced", id, a, eventsZero, uint8(perpstypes.OrderTypeLimit), true, usd(59_000),
		fixed("5999.71"))
}

func TestEventOrderOptions(t *testing.T) {
	e := newEventsEnv(t)
	_, id, logs := restingLimit(e, 0)
	placed := e.indices(logs, "OrderPlaced")
	require.Len(t, placed, 1)
	options := e.requireLog(logs, "OrderOptions", id, uint8(perpstypes.TimeInForceGTC), false, usd(59_000),
		eventsZero, uint64(0), eventsCloid)
	require.Equal(t, placed[0]+1, options, "OrderOptions directly follows OrderPlaced")
}

func TestEventOrderCancelled(t *testing.T) {
	e := newEventsEnv(t)
	a, id, _ := restingLimit(e, 0)
	logs, _ := e.tx(a, "cancelOrder", id)
	cancelled := e.requireLog(logs, "OrderCancelled", id, a)
	require.Empty(t, logs[cancelled].Data)
}

func TestEventOrderExpired(t *testing.T) {
	e := newEventsEnv(t)
	a, id, _ := restingLimit(e, 1_800_000_100)
	e.advance(101)
	require.Equal(t, int64(1_800_000_101), e.ctx.BlockTime().Unix())
	logs := e.endBlock()
	e.requireLog(logs, "OrderExpired", id, a, eventsZero)
	order, err := e.k.GetOrder(e.ctx, *uint256.MustFromBig(id))
	require.NoError(t, err)
	require.Equal(t, perpstypes.OrderStatusExpired, order.Status)
	require.Nil(t, e.ctx.KVStore(e.key).Get(perpstypes.ExpiryKey(order.Deadline, order.ID)),
		"the expired order leaves the expiry index")
}

func TestEventOrderTriggered(t *testing.T) {
	e := newEventsEnv(t)
	c := e.trader("account C", 100_000_000_000)
	e.place(c, eventsLimitOrder(false, perpstypes.TimeInForceGTC, usd(61_050), usd(1_221), usd(10)))
	a := e.trader("account A", 1_000_000_000)
	stop := newOrder(true, perpstypes.OrderTypeStopMarket, perpstypes.TimeInForceIOC, usd(600), usd(10))
	stop.TriggerPrice = usd(61_000)
	_, id := e.place(a, stop)

	e.advance(1)
	e.setPrice(61_050)
	logs := e.endBlock()
	triggered := e.requireLog(logs, "OrderTriggered", id, eventsZero, usd(61_050))
	takerTopic := common.BigToHash(id)
	var fills int
	for _, i := range e.indices(logs, "Fill") {
		if logs[i].Topics[3] == takerTopic {
			fills++
			require.Greater(t, i, triggered, "OrderTriggered precedes the fills of its order")
		}
	}
	require.Positive(t, fills)
}

func TestEventOrderExecuted(t *testing.T) {
	e := newEventsEnv(t)
	c := e.trader("account C", 100_000_000_000)
	e.place(c, eventsLimitOrder(false, perpstypes.TimeInForceGTC, usd(60_000), usd(600), usd(10)))
	e.place(c, eventsLimitOrder(false, perpstypes.TimeInForceGTC, usd(60_100), usd(601), usd(10)))
	b := e.trader("account B", 10_000_000_000)
	logs, takerID := e.place(b, eventsLimitOrder(true, perpstypes.TimeInForceIOC, usd(60_100), usd(1_202), usd(10)))
	require.Len(t, e.indices(logs, "Fill"), 2)
	e.requireLog(logs, "OrderExecuted", takerID, e.positionOf(b), usd(60_050))
	perOrder := map[common.Hash]int{}
	for _, i := range e.indices(logs, "OrderExecuted") {
		perOrder[logs[i].Topics[1]]++
	}
	require.Equal(t, 1, perOrder[common.BigToHash(takerID)], "the taker order logs OrderExecuted once")
	for _, i := range e.indices(logs, "Fill") {
		require.Equal(t, 1, perOrder[logs[i].Topics[2]], "each maker order logs OrderExecuted once")
	}
	require.Len(t, perOrder, 3)
}

func TestEventFill(t *testing.T) {
	e := newEventsEnv(t)
	a, b, makerID, takerID, logs := fillScenario(e, false)
	e.requireLog(logs, "Fill", eventsZero, makerID, takerID, a, b, true, usd(60_000), usd(600), fixed("0.09"),
		fixed("0.27"))
	require.Len(t, e.indices(logs, "Fill"), 1)
}

func TestEventPositionOpened(t *testing.T) {
	e := newEventsEnv(t)
	a, position, logs := openScenario(e, 60_000)
	e.requireLog(logs, "PositionOpened", position, a, eventsZero, true, usd(990), usd(10), usd(60_000), e.usdl,
		units(100_000_000))
}

func TestEventPositionModified(t *testing.T) {
	e := newEventsEnv(t)
	a, position, logs := addSizeScenario(e)
	// 99,554,500 after the open, +33 USD realised at 62,000, +50 USDL, -0.222642 fee.
	require.Equal(t, uint64(182_331_858), e.collateral(a))
	equity, err := e.k.Equity(e.ctx, a)
	require.NoError(t, err)
	collateralUsd := new(big.Int).Mul(big.NewInt(182_331_858), big.NewInt(1_000_000_000_000))
	require.Equal(t, collateralUsd, equity.BigInt(), "the position is A's only one and sits at mark")
	e.requireLog(logs, "PositionModified", position, fixed("1517.76"), collateralUsd, units(182_331_858))
}

func TestEventPositionEntryPriceChanged(t *testing.T) {
	e := newEventsEnv(t)
	_, position, logs := addSizeScenario(e)
	e.requireLog(logs, "PositionEntryPriceChanged", position, usd(60_000), fixed("60651.960784313725490196"))
}

func TestEventPositionClosed(t *testing.T) {
	e := newEventsEnv(t)
	a, position, logs := closeScenario(e)
	e.requireLog(logs, "PositionClosed", position, a, eventsZero, fixed("1006.5"), usd(61_000), fixed("16.5"), true)
	require.Nil(t, e.ctx.KVStore(e.key).Get(perpstypes.AccountMarketPositionKey(a, 0)))
}

func TestEventPositionTpSlSet(t *testing.T) {
	e := newEventsEnv(t)
	c := e.trader("account C", 100_000_000_000)
	e.place(c, eventsLimitOrder(false, perpstypes.TimeInForceGTC, usd(60_000), usd(990), usd(10)))
	a := e.trader("account A", 100_000_000)
	order := newOrder(true, perpstypes.OrderTypeMarket, perpstypes.TimeInForceIOC, usd(990), usd(10))
	order.TakeProfitPrice, order.StopLossPrice = usd(66_000), usd(57_000)
	logs, _ := e.place(a, order)
	position := e.positionOf(a)
	opened := -1
	for _, i := range e.indices(logs, "PositionOpened") {
		if logs[i].Topics[1] == common.BigToHash(position) {
			opened = i
		}
	}
	require.NotEqual(t, -1, opened)
	set := e.requireLog(logs, "PositionTpSlSet", position, usd(66_000), usd(57_000))
	require.Greater(t, set, opened, "PositionTpSlSet follows PositionOpened")
}

func TestEventTradeSettled(t *testing.T) {
	e := newEventsEnv(t)
	a, position, logs := closeScenario(e)
	e.requireLog(logs, "TradeSettled", position, a, eventsZero, perpskeeper.TradeTypeClose, fixed("1006.5"),
		usd(61_000), fixed("16.5"), fixed("0.452925"), fixed("-0.05"), units(15_997_075))
}

func TestEventFeeCollected(t *testing.T) {
	e := newEventsEnv(t)
	a, b, _, _, logs := fillScenario(e, false)
	e.requireLog(logs, "FeeCollected", e.positionOf(b), b, eventsZero, perpskeeper.FeeTypeTrading, fixed("0.27"),
		units(270_000), e.usdl)
	e.requireLog(logs, "FeeCollected", e.positionOf(a), a, eventsZero, perpskeeper.FeeTypeTrading, fixed("0.09"),
		units(90_000), e.usdl)

	r := newEventsEnv(t)
	a, b, makerID, takerID, logs := fillScenario(r, true)
	r.requireLog(logs, "FeeCollected", r.positionOf(a), a, eventsZero, perpskeeper.FeeTypeMakerRebate, fixed("0.012"),
		units(12_000), r.usdl)
	r.requireLog(logs, "Fill", eventsZero, makerID, takerID, a, b, true, usd(60_000), usd(600), fixed("-0.012"),
		fixed("0.27"))
}

func TestEventMarkPriceUpdated(t *testing.T) {
	e := newEventsEnv(t)
	e.advance(1)
	e.setAggregate(60_100, 60_100)
	logs := e.endBlock()
	e.requireLog(logs, "MarkPriceUpdated", eventsZero, usd(60_100), usd(60_100), usd(60_100))
	require.Len(t, e.indices(logs, "MarkPriceUpdated"), 1)
}

// fundingScenario holds 2 BTC of open interest on each side at 60,000 and
// runs EndBlock at the next whole hour with no premium: the hourly rate is
// the 0.01% interest over 8, 0.00125%.
func fundingScenario(e *eventsEnv) []*ethtypes.Log {
	e.addOpenInterest(true, fixed("2"))
	e.addOpenInterest(false, fixed("2"))
	e.advance(3_600)
	require.Zero(e.t, e.ctx.BlockTime().Unix()%3_600)
	return e.endBlock()
}

func TestEventFundingRateUpdated(t *testing.T) {
	e := newEventsEnv(t)
	logs := fundingScenario(e)
	e.requireLog(logs, "FundingRateUpdated", eventsZero, big.NewInt(3_472_222_222), fixed("0.0003"))
}

func TestEventFundingSettled(t *testing.T) {
	e := newEventsEnv(t)
	logs := fundingScenario(e)
	e.requireLog(logs, "FundingSettled", eventsZero, big.NewInt(12_500_000_000_000), fixed("-1.5"), fixed("1.5"))
	require.Empty(t, e.indices(logs, "PositionFundingApplied"), "no position log is written at the hour")
	require.Equal(t, *uint256.MustFromBig(fixed("0.75")), e.marketState().FundingIndex)
}

func TestEventPositionFundingApplied(t *testing.T) {
	e := newEventsEnv(t)
	c := e.trader("account C", 100_000_000_000)
	e.place(c, eventsLimitOrder(false, perpstypes.TimeInForceGTC, usd(60_000), usd(60_000), usd(10)))
	a := e.trader("account A", 10_000_000_000)
	e.place(a, eventsLimitOrder(true, perpstypes.TimeInForceIOC, usd(60_000), usd(60_000), usd(10)))
	position := e.positionOf(a)
	require.Equal(t, uint64(9_973_000_000), e.collateral(a))

	e.setFundingIndex(fixed("0.75"))
	e.advance(1)
	logs := e.endBlock()
	e.requireLog(logs, "PositionFundingApplied", position, a, eventsZero, fixed("-0.75"), fixed("9972.25"),
		units(9_972_250_000))
	require.Equal(t, uint64(9_972_250_000), e.collateral(a))
}

func TestEventOpenInterestChanged(t *testing.T) {
	e := newEventsEnv(t)
	_, _, _, _, logs := fillScenario(e, false)
	e.requireLog(logs, "OpenInterestChanged", eventsZero, usd(600), usd(600), usd(600), true)
}

func TestEventLiquidation(t *testing.T) {
	e := newEventsEnv(t)
	e.setPrice(59_000)
	b := e.trader("account B", 100_000_000_000)
	e.place(b, eventsLimitOrder(true, perpstypes.TimeInForceGTC, usd(59_000), usd(118_000), usd(10)))
	// A's 10 BTC long entered at 60,000: equity 10,250 - 10,000 = 250 USD,
	// below the 295 USD maintenance and above two thirds of it.
	e.startPositionIDs()
	a := eventsAddress("account A")
	e.storeAccount(a, 10_250_000_000)
	e.storePosition(7, a, true, fixed("10"), 60_000, eventsZero)
	d := eventsAddress("account D")
	e.storeAccount(d, 1_000_000_000_000)
	e.storePosition(8, d, false, fixed("10"), 60_000, eventsZero)
	e.refresh(a, d)
	e.advance(1)
	logs := e.endBlock()
	e.requireLog(logs, "Liquidation", big.NewInt(7), a, eventsZero, usd(59_000), eventsZero, common.Address{})
}

func TestEventBackstopLiquidation(t *testing.T) {
	e := newEventsEnv(t)
	a, logs := backstopScenario(e, 18_000_000, "1", 60_000, 0)
	e.requireLog(logs, "BackstopLiquidation", a, eventsZero, usd(60_000), usd(60_000), usd(18))
	require.NotNil(t, e.ctx.KVStore(e.key).Get(perpstypes.AccountMarketPositionKey(e.k.InsuranceAccountAddress(), 0)),
		"the insurance fund holds the position")
	require.Nil(t, e.ctx.KVStore(e.key).Get(perpstypes.AccountMarketPositionKey(a, 0)))
}

func TestEventInsuranceContribution(t *testing.T) {
	e := newEventsEnv(t)
	_, logs := backstopScenario(e, 18_000_000, "1", 60_000, 0)
	e.requireLog(logs, "InsuranceContribution", e.usdl, units(18_000_000), perpskeeper.InsuranceSourceBackstopMargin)
}

func TestEventInsuranceDraw(t *testing.T) {
	e := newEventsEnv(t)
	_, logs := backstopScenario(e, 0, "1", 60_400, 5_000_000_000)
	e.requireLog(logs, "InsuranceDraw", eventsZero, units(400_000_000), units(4_600_000_000))
	require.Equal(t, uint64(4_600_000_000), e.collateral(e.k.InsuranceAccountAddress()))
}

func TestEventVaultDeficit(t *testing.T) {
	e := newEventsEnv(t)
	_, logs := deficitScenario(e)
	draw := e.requireLog(logs, "InsuranceDraw", eventsZero, units(5_000_000_000), eventsZero)
	deficit := e.requireLog(logs, "VaultDeficit", e.usdl, units(1_000_000_000))
	require.Greater(t, deficit, draw)
	adl := e.indices(logs, "ADLExecuted")
	require.NotEmpty(t, adl, "ADL runs in the same pass")
	require.Greater(t, adl[0], deficit)
}

func TestEventADLExecuted(t *testing.T) {
	e := newEventsEnv(t)
	_, logs := deficitScenario(e)
	// The fund takes position 7 over under its id.
	e.requireLog(logs, "ADLExecuted", big.NewInt(7), usd(6_000))
}

func TestEventADLMatched(t *testing.T) {
	e := newEventsEnv(t)
	c, logs := deficitScenario(e)
	e.requireLog(logs, "ADLMatched", eventsZero, big.NewInt(9), c, usd(6_000), usd(60_000), fixed("5.5"))
}

func TestEventFeeAndInsuranceEnums(t *testing.T) {
	require.Equal(t, []uint8{0, 1, 2, 3, 4, 5}, []uint8{uint8(perpstypes.OrderTypeLimit),
		uint8(perpstypes.OrderTypeStopLimit), uint8(perpstypes.OrderTypeTakeProfit), uint8(perpstypes.OrderTypeStopLoss),
		uint8(perpstypes.OrderTypeMarket), uint8(perpstypes.OrderTypeStopMarket)})
	require.Equal(t, []uint8{0, 1, 2, 3, 4, 5, 6}, []uint8{perpskeeper.TradeTypeOpen, perpskeeper.TradeTypeClose,
		perpskeeper.TradeTypeLiquidation, perpskeeper.TradeTypeReduce, perpskeeper.TradeTypeIncrease,
		perpskeeper.TradeTypeADL, perpskeeper.TradeTypeBackstop})
	require.Equal(t, []uint8{0, 4}, []uint8{perpskeeper.FeeTypeTrading, perpskeeper.FeeTypeMakerRebate})
	require.Equal(t, []uint8{0, 1, 2}, []uint8{perpskeeper.InsuranceSourceTradingFee,
		perpskeeper.InsuranceSourceBackstopMargin, perpskeeper.InsuranceSourceBackstopProfit})

	// One block: taker fills with a maker rebate, then X backstopped at
	// 60,000 and the fund's long closed into Z's bid at 60,100, in profit.
	e := newEventsEnv(t)
	_, _, _, _, block := fillScenario(e, true)
	z := e.trader("account Z", 100_000_000_000)
	placed, _ := e.place(z, eventsLimitOrder(true, perpstypes.TimeInForceGTC, usd(60_100), usd(60_100), usd(10)))
	block = append(block, placed...)
	e.startPositionIDs()
	x := eventsAddress("account X")
	e.storeAccount(x, 1_000_000)
	e.storePosition(7, x, true, fixed("1"), 60_000, eventsZero)
	y := eventsAddress("account Y")
	e.storeAccount(y, 1_000_000_000_000)
	e.storePosition(8, y, false, fixed("1"), 60_000, eventsZero)
	e.refresh(x, y)
	block = append(block, e.endBlock()...)

	require.Len(t, e.abi.Events, 30)
	known := make(map[common.Hash]string, len(e.abi.Events))
	for name, event := range e.abi.Events {
		known[event.ID] = name
	}
	feeTypes, sources := map[uint8]bool{}, map[uint8]bool{}
	for _, l := range block {
		require.Equal(t, eventsVenue, l.Address)
		name, ok := known[l.Topics[0]]
		require.True(t, ok, "topic0 %s is one of the abi.json events", l.Topics[0])
		switch name {
		case "FeeCollected":
			feeType := e.values(name, l)[0].(uint8)
			require.Contains(t, []uint8{perpskeeper.FeeTypeTrading, perpskeeper.FeeTypeMakerRebate}, feeType)
			feeTypes[feeType] = true
		case "InsuranceContribution":
			source := e.values(name, l)[1].(uint8)
			require.LessOrEqual(t, source, perpskeeper.InsuranceSourceBackstopProfit)
			sources[source] = true
		case "TradeSettled":
			require.LessOrEqual(t, e.values(name, l)[0].(uint8), perpskeeper.TradeTypeBackstop)
		case "OrderPlaced":
			require.LessOrEqual(t, e.values(name, l)[0].(uint8), uint8(perpstypes.OrderTypeStopMarket))
		}
	}
	require.True(t, feeTypes[perpskeeper.FeeTypeTrading], "a taker fee is charged")
	require.True(t, feeTypes[perpskeeper.FeeTypeMakerRebate], "a maker rebate is paid")
	require.True(t, sources[perpskeeper.InsuranceSourceBackstopMargin], "the backstop takes margin")
	require.True(t, sources[perpskeeper.InsuranceSourceBackstopProfit], "the backstop close books its profit")
}
