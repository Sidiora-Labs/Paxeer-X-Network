package perps_test

import (
	"encoding/json"
	"math"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	perpskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	perpstypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// positionsNowMs is the block time of every positions test; the pricefeed
// aggregate is one second older, so the price is fresh.
const positionsNowMs int64 = 1_800_000_000_000

var (
	positionsTraderA = common.HexToAddress("0x00000000000000000000000000000000000A11CE")
	positionsMakerB  = common.HexToAddress("0x00000000000000000000000000000000000000B2")
	positionsBidderC = common.HexToAddress("0x00000000000000000000000000000000000000C3")
	positionsKeeperK = common.HexToAddress("0x00000000000000000000000000000000000000D4")
)

// positionsKeepers are the application's precompile keepers plus the perps
// keeper.
type positionsKeepers struct {
	utils.Keepers
	perps *perpskeeper.Keeper
}

func (k positionsKeepers) PerpsK() *perpskeeper.Keeper { return k.perps }

// positionsEnv is the perps precompile over the real perps keeper, the real
// pricefeed keeper and a real EVM, with BTC (market 0, tick 0.1, lot 0.00001)
// listed, a fresh price and mark of 60,000 USD and the USDL ERC20 runtime at
// Params.CollateralToken.
type positionsEnv struct {
	t          *testing.T
	k          *perpskeeper.Keeper
	perpsKey   sdk.StoreKey
	stateDB    *state.DBImpl
	evm        *vm.EVM
	precompile *pcommon.Precompile
	perpsABI   abi.ABI
	erc20ABI   abi.ABI
	usdl       common.Address
	askID      uint256.Int
}

func positionsE18(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), big.NewInt(1_000_000_000_000_000_000))
}

func positionsWord(t *testing.T, decimal string) uint256.Int {
	t.Helper()
	value, err := uint256.FromDecimal(decimal)
	require.NoError(t, err)
	return *value
}

func positionsMarket() perpstypes.Market {
	return perpstypes.Market{
		ID: 0, Symbol: "BTC", Name: "BTC", OracleSymbol: "BTC", SzDecimals: 5, PxDecimals: 1, MaxLeverage: 1000,
		TierClass: perpstypes.TierClassA, ImpactNotionalUsd: sdk.NewIntWithDecimal(20_000, 18),
		OiCapUsd: sdk.NewIntWithDecimal(500_000_000, 18), FundingCapPpmPerHour: 40_000, Enabled: true,
	}
}

// newPositionsEnv seeds walletA USDL units and the same allowance to 0x1015
// for trader A, books 10,000 USDL for maker B, bidder C and keeper K in the
// store with the matching USDL held by the perps module, and rests B's ask of
// 1 BTC at 60,000 USD.
func newPositionsEnv(t *testing.T, walletA int64) *positionsEnv {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockHeight(12).WithBlockTime(time.UnixMilli(positionsNowMs).UTC())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	pfKey := sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	pricefeed := pricefeedkeeper.NewKeeper(pfKey, app.StakingKeeper, app.AccountKeeper, &app.EvmKeeper)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, pfKey)
	perpsKey := sdk.NewKVStoreKey(perpstypes.StoreKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, perpsKey)
	k := perpskeeper.NewKeeper(perpsKey, &app.EvmKeeper, pricefeed)
	require.NoError(t, k.SetMarket(ctx, k.Authority(), positionsMarket()))
	aggregate, err := json.Marshal(pricefeedtypes.Aggregate{
		IndexPrice: sdk.NewIntWithDecimal(60_000, 18), MarkPrice: sdk.NewIntWithDecimal(60_000, 18),
		Basis: sdk.ZeroInt(), TimestampMs: uint64(positionsNowMs - 1_000), Height: ctx.BlockHeight(), Sources: 1,
	})
	require.NoError(t, err)
	ctx.KVStore(pfKey).Set(pricefeedtypes.AggregateKey(0), aggregate)

	erc20ABI := positionsReadABI(t, "../../example/contracts/erc20/ERC20.abi")
	perpsABI, err := perpsabi.ABI()
	require.NoError(t, err)
	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	e := &positionsEnv{t: t, k: k, perpsKey: perpsKey, perpsABI: perpsABI, erc20ABI: erc20ABI,
		usdl: params.CollateralToken}

	creation, err := os.ReadFile("../../example/contracts/erc20/ERC20.bin")
	require.NoError(t, err)
	deployer := common.HexToAddress("0x00000000000000000000000000000000000DE910")
	nonce := app.EvmKeeper.GetNonce(ctx, deployer)
	_, err = app.EvmKeeper.CallEVM(ctx, deployer, nil, nil, common.FromHex(strings.TrimSpace(string(creation))))
	require.NoError(t, err)
	runtime := app.EvmKeeper.GetCode(ctx, crypto.CreateAddress(deployer, nonce))
	require.NotEmpty(t, runtime)
	app.EvmKeeper.SetCode(ctx, e.usdl, runtime)

	perpsAddress := common.HexToAddress(perps.PerpsAddress)
	e.seedBalance(ctx, positionsTraderA, walletA)
	e.seedAllowance(ctx, positionsTraderA, perpsAddress, walletA)
	const booked int64 = 10_000_000_000
	e.seedBalance(ctx, k.ModuleEVMAddress(), 3*booked)
	for _, account := range []common.Address{positionsMakerB, positionsBidderC, positionsKeeperK} {
		_, err := k.Deposit(ctx, account, *uint256.NewInt(uint64(booked)))
		require.NoError(t, err)
	}
	marketState, err := k.GetMarketState(ctx, 0)
	require.NoError(t, err)
	marketState.MarkPrice = *uint256.MustFromBig(positionsE18(60_000))
	k.SetMarketState(ctx, marketState)
	e.askID = e.rest(ctx, positionsMakerB, false, 60_000)

	precompile, err := perps.NewPrecompile(positionsKeepers{Keepers: app.GetPrecompileKeepers(), perps: k})
	require.NoError(t, err)
	e.precompile = precompile
	e.stateDB = state.NewDBImpl(ctx, &app.EvmKeeper, false)
	blockCtx, err := app.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
	require.NoError(t, err)
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(app.EvmKeeper.ChainID(ctx))
	e.evm = vm.NewEVM(*blockCtx, e.stateDB, cfg, vm.Config{}, app.EvmKeeper.CustomPrecompiles(ctx))
	return e
}

func positionsReadABI(t *testing.T, path string) abi.ABI {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()
	parsed, err := abi.JSON(file)
	require.NoError(t, err)
	return parsed
}

// seedBalance writes balanceOf[holder] (slot 3 of the ERC20) of the USDL
// runtime.
func (e *positionsEnv) seedBalance(ctx sdk.Context, holder common.Address, amount int64) {
	slot := crypto.Keccak256Hash(common.LeftPadBytes(holder.Bytes(), 32), common.LeftPadBytes([]byte{3}, 32))
	testkeeper.EVMTestApp.EvmKeeper.SetState(ctx, e.usdl, slot, common.BigToHash(big.NewInt(amount)))
}

// seedAllowance writes allowance[owner][spender] (slot 4 of the ERC20) of the
// USDL runtime.
func (e *positionsEnv) seedAllowance(ctx sdk.Context, owner, spender common.Address, amount int64) {
	inner := crypto.Keccak256(common.LeftPadBytes(owner.Bytes(), 32), common.LeftPadBytes([]byte{4}, 32))
	slot := crypto.Keccak256Hash(common.LeftPadBytes(spender.Bytes(), 32), inner)
	testkeeper.EVMTestApp.EvmKeeper.SetState(ctx, e.usdl, slot, common.BigToHash(big.NewInt(amount)))
}

func (e *positionsEnv) ctx() sdk.Context { return e.stateDB.Ctx() }

// rest places a GTC limit order of 1 BTC at price whole USD for account.
func (e *positionsEnv) rest(ctx sdk.Context, account common.Address, isLong bool, price int64) uint256.Int {
	e.t.Helper()
	result, err := e.k.PlaceOrder(ctx, account, perpskeeper.OrderParams{
		MarketID: 0, IsLong: isLong, OrderType: uint8(perpstypes.OrderTypeLimit),
		TimeInForce: uint8(perpstypes.TimeInForceGTC), SizeUsd: *uint256.MustFromBig(positionsE18(price)),
		Leverage: *uint256.MustFromBig(positionsE18(20)), LimitPrice: *uint256.MustFromBig(positionsE18(price)),
	})
	require.NoError(e.t, err)
	return result.OrderID
}

// setMark sets the BTC mark the keeper settles and sizes at.
func (e *positionsEnv) setMark(price *big.Int) {
	e.t.Helper()
	marketState, err := e.k.GetMarketState(e.ctx(), 0)
	require.NoError(e.t, err)
	marketState.MarkPrice = *uint256.MustFromBig(price)
	e.k.SetMarketState(e.ctx(), marketState)
}

func (e *positionsEnv) setPaused(paused bool) {
	e.t.Helper()
	params, err := e.k.GetParams(e.ctx())
	require.NoError(e.t, err)
	params.Paused = paused
	require.NoError(e.t, e.k.UpdateParams(e.ctx(), e.k.Authority(), params))
}

// cancelAsk cancels what remains of maker B's ask.
func (e *positionsEnv) cancelAsk() {
	e.t.Helper()
	_, err := e.k.CancelOrder(e.ctx(), positionsMakerB, e.askID)
	require.NoError(e.t, err)
}

func (e *positionsEnv) call(caller common.Address, method string, args ...interface{}) ([]byte, error) {
	e.t.Helper()
	input, err := e.perpsABI.Pack(method, args...)
	require.NoError(e.t, err)
	var (
		ret     []byte
		callErr error
	)
	snapshot := e.stateDB.Snapshot()
	require.NotPanics(e.t, func() {
		ret, callErr = e.precompile.Run(e.evm, caller, caller, input, big.NewInt(0), false, false, nil)
	})
	if callErr != nil {
		e.stateDB.RevertToSnapshot(snapshot)
	}
	return ret, callErr
}

func (e *positionsEnv) mustCall(caller common.Address, method string, args ...interface{}) []byte {
	e.t.Helper()
	ret, err := e.call(caller, method, args...)
	require.NoError(e.t, err, "%s reverted with %x", method, ret)
	return ret
}

// requireRevert asserts the call reverted with exactly the selector of the
// parameterless custom error name.
func (e *positionsEnv) requireRevert(name string, caller common.Address, method string, args ...interface{}) {
	e.t.Helper()
	ret, err := e.call(caller, method, args...)
	require.ErrorIs(e.t, err, vm.ErrExecutionReverted, method)
	require.Equal(e.t, crypto.Keccak256([]byte(name + "()"))[:4], ret, "%s revert data", method)
}

// open runs openPosition(0, usdl, 100 USDL, 10x, long) for A and returns the
// position id.
func (e *positionsEnv) open() *big.Int {
	e.t.Helper()
	ret := e.mustCall(positionsTraderA, "openPosition", big.NewInt(0), e.usdl, big.NewInt(100_000_000),
		positionsE18(10), true)
	out, err := e.perpsABI.Methods["openPosition"].Outputs.Unpack(ret)
	require.NoError(e.t, err)
	require.Len(e.t, out, 1)
	return out[0].(*big.Int)
}

func (e *positionsEnv) usdlBalance(holder common.Address) *big.Int {
	e.t.Helper()
	input, err := e.erc20ABI.Pack("balanceOf", holder)
	require.NoError(e.t, err)
	ret, _, err := e.evm.StaticCall(holder, e.usdl, input, 100_000)
	require.NoError(e.t, err)
	out, err := e.erc20ABI.Methods["balanceOf"].Outputs.Unpack(ret)
	require.NoError(e.t, err)
	return out[0].(*big.Int)
}

func (e *positionsEnv) collateral(account common.Address) uint256.Int {
	e.t.Helper()
	held, err := e.k.GetAccount(e.ctx(), account)
	require.NoError(e.t, err)
	return held.Collateral
}

// position reads account's BTC position through the AccountMarketPosition
// index; ok is false when the account holds none.
func (e *positionsEnv) position(account common.Address) (perpstypes.Position, bool) {
	e.t.Helper()
	store := e.ctx().KVStore(e.perpsKey)
	idBytes := store.Get(perpstypes.AccountMarketPositionKey(account, 0))
	if idBytes == nil {
		return perpstypes.Position{}, false
	}
	var id uint256.Int
	id.SetBytes(idBytes)
	bz := store.Get(perpstypes.PositionKey(id))
	require.NotNil(e.t, bz)
	var position perpstypes.Position
	require.NoError(e.t, position.Unmarshal(bz))
	return position, true
}

// perpsLogs returns the logs of event name emitted by 0x1015, each with its
// non-indexed fields by name.
func (e *positionsEnv) perpsLogs(name string) ([]*ethtypes.Log, []map[string]interface{}) {
	e.t.Helper()
	event, ok := e.perpsABI.Events[name]
	require.True(e.t, ok, name)
	var (
		logs   []*ethtypes.Log
		fields []map[string]interface{}
	)
	for _, log := range e.stateDB.GetAllLogs() {
		if log.Address != common.HexToAddress(perps.PerpsAddress) || len(log.Topics) == 0 || log.Topics[0] != event.ID {
			continue
		}
		values := map[string]interface{}{}
		require.NoError(e.t, event.Inputs.NonIndexed().UnpackIntoMap(values, log.Data))
		logs = append(logs, log)
		fields = append(fields, values)
	}
	return logs, fields
}

func positionsTopic(value *big.Int) common.Hash { return common.BigToHash(value) }

func positionsAddressTopic(account common.Address) common.Hash {
	return common.BytesToHash(account.Bytes())
}

func TestPositionsOpenPositionCompatMarketOrder(t *testing.T) {
	e := newPositionsEnv(t, 100_000_000)
	module := e.k.ModuleEVMAddress()
	moduleBefore := e.usdlBalance(module)

	id := e.open()

	position, ok := e.position(positionsTraderA)
	require.True(t, ok)
	require.Equal(t, position.ID.ToBig(), id)
	require.True(t, position.IsLong)
	require.Equal(t, positionsWord(t, "16500000000000000"), position.SizeBase)
	require.Equal(t, *uint256.MustFromBig(positionsE18(60_000)), position.EntryPrice)
	require.Equal(t, *uint256.MustFromBig(positionsE18(10)), position.Leverage)
	require.Equal(t, *uint256.NewInt(99_554_500), e.collateral(positionsTraderA))
	require.Zero(t, e.usdlBalance(positionsTraderA).Sign())
	require.Equal(t, new(big.Int).Add(moduleBefore, big.NewInt(100_000_000)), e.usdlBalance(module))

	maker, ok := e.position(positionsMakerB)
	require.True(t, ok)
	logs, fields := e.perpsLogs("PositionOpened")
	require.Len(t, logs, 2)
	require.Equal(t, []common.Hash{e.perpsABI.Events["PositionOpened"].ID, positionsTopic(maker.ID.ToBig()),
		positionsAddressTopic(positionsMakerB), positionsTopic(big.NewInt(0))}, logs[0].Topics)
	require.Equal(t, false, fields[0]["isLong"])
	require.Equal(t, []common.Hash{e.perpsABI.Events["PositionOpened"].ID, positionsTopic(id),
		positionsAddressTopic(positionsTraderA), positionsTopic(big.NewInt(0))}, logs[1].Topics)
	require.Equal(t, true, fields[1]["isLong"])
	require.Equal(t, positionsE18(990), fields[1]["sizeUsd"])
	require.Equal(t, positionsE18(10), fields[1]["leverage"])
	require.Equal(t, positionsE18(60_000), fields[1]["entryPrice"])
	require.Equal(t, e.usdl, fields[1]["collateralToken"])
	require.Equal(t, big.NewInt(100_000_000), fields[1]["collateralAmount"])
	fills, _ := e.perpsLogs("Fill")
	require.Len(t, fills, 1)

	e.requireRevert("PerpsUnsupportedCollateral", positionsTraderA, "openPosition", big.NewInt(0),
		common.Address{}, big.NewInt(1_000_000), positionsE18(10), true)
	e.requireRevert("PerpsInsufficientCollateral", positionsTraderA, "openPosition", big.NewInt(0), e.usdl,
		big.NewInt(1_000_000), positionsE18(10), true)
}

func TestPositionsAddSizeReweightsEntry(t *testing.T) {
	e := newPositionsEnv(t, 150_000_000)
	id := e.open()
	e.cancelAsk()
	e.rest(e.ctx(), positionsMakerB, false, 62_000)
	e.setMark(positionsE18(62_000))

	e.mustCall(positionsTraderA, "addSize", id, big.NewInt(50_000_000), positionsE18(10))

	position, ok := e.position(positionsTraderA)
	require.True(t, ok)
	require.Equal(t, id, position.ID.ToBig())
	require.Equal(t, positionsWord(t, "24480000000000000"), position.SizeBase)
	require.Equal(t, positionsWord(t, "60651960784313725490196"), position.EntryPrice)
	require.Equal(t, *uint256.NewInt(182_331_858), e.collateral(positionsTraderA))
	require.Zero(t, e.usdlBalance(positionsTraderA).Sign())

	maker, ok := e.position(positionsMakerB)
	require.True(t, ok)
	logs, fields := e.perpsLogs("PositionEntryPriceChanged")
	require.Len(t, logs, 2)
	require.Equal(t, positionsTopic(maker.ID.ToBig()), logs[0].Topics[1])
	require.Equal(t, positionsTopic(id), logs[1].Topics[1])
	require.Equal(t, positionsE18(60_000), fields[1]["oldEntryPrice"])
	require.Equal(t, wordBig(positionsWord(t, "60651960784313725490196")), fields[1]["newEntryPrice"])

	e.requireRevert("PerpsNotPositionHolder", positionsBidderC, "addSize", id, big.NewInt(1_000_000), positionsE18(10))
	e.requireRevert("PerpsUnknownPosition", positionsTraderA, "addSize", big.NewInt(999), big.NewInt(1_000_000),
		positionsE18(10))
}

func TestPositionsAddCollateralDepositsCross(t *testing.T) {
	e := newPositionsEnv(t, 120_000_000)
	id := e.open()
	before, ok := e.position(positionsTraderA)
	require.True(t, ok)
	e.setPaused(true)

	e.mustCall(positionsTraderA, "addCollateral", id, big.NewInt(20_000_000))

	require.Equal(t, *uint256.NewInt(119_554_500), e.collateral(positionsTraderA))
	require.Zero(t, e.usdlBalance(positionsTraderA).Sign())
	after, ok := e.position(positionsTraderA)
	require.True(t, ok)
	require.Equal(t, before.SizeBase, after.SizeBase)
	require.Equal(t, before.EntryPrice, after.EntryPrice)
	logs, fields := e.perpsLogs("CollateralDeposited")
	require.NotEmpty(t, logs)
	last := len(logs) - 1
	require.Equal(t, positionsAddressTopic(positionsTraderA), logs[last].Topics[1])
	require.Equal(t, big.NewInt(20_000_000), fields[last]["amount"])
	require.Equal(t, big.NewInt(119_554_500), fields[last]["newBalance"])

	e.requireRevert("PerpsZeroAmount", positionsTraderA, "addCollateral", id, big.NewInt(0))
	e.requireRevert("PerpsNotPositionHolder", positionsBidderC, "addCollateral", id, big.NewInt(1))
}

func TestPositionsRemoveCollateralBoundedByWithdrawable(t *testing.T) {
	e := newPositionsEnv(t, 129_445_500)
	id := e.open()
	e.mustCall(positionsTraderA, "addCollateral", id, big.NewInt(29_445_500))
	withdrawable, err := e.k.Withdrawable(e.ctx(), positionsTraderA)
	require.NoError(t, err)
	require.Equal(t, *uint256.NewInt(30_000_000), withdrawable)

	e.requireRevert("PerpsInsufficientMargin", positionsTraderA, "removeCollateral", id, big.NewInt(30_000_001))
	require.Zero(t, e.usdlBalance(positionsTraderA).Sign())

	e.mustCall(positionsTraderA, "removeCollateral", id, big.NewInt(30_000_000))

	require.Equal(t, big.NewInt(30_000_000), e.usdlBalance(positionsTraderA))
	require.Equal(t, *uint256.NewInt(99_000_000), e.collateral(positionsTraderA))
	logs, fields := e.perpsLogs("CollateralWithdrawn")
	require.Len(t, logs, 1)
	require.Equal(t, positionsAddressTopic(positionsTraderA), logs[0].Topics[1])
	require.Equal(t, big.NewInt(30_000_000), fields[0]["amount"])
	require.Equal(t, big.NewInt(99_000_000), fields[0]["newBalance"])

	e.requireRevert("PerpsZeroAmount", positionsTraderA, "removeCollateral", id, big.NewInt(0))
	e.requireRevert("PerpsNotPositionHolder", positionsBidderC, "removeCollateral", id, big.NewInt(1))
}

func TestPositionsClosePositionRealisesPnl(t *testing.T) {
	e := newPositionsEnv(t, 100_000_000)
	id := e.open()
	e.cancelAsk()
	e.rest(e.ctx(), positionsBidderC, true, 61_000)
	module := e.k.ModuleEVMAddress()
	moduleBefore := e.usdlBalance(module)
	e.setPaused(true)

	e.requireRevert("PerpsNotPositionHolder", positionsBidderC, "closePosition", id)
	e.mustCall(positionsTraderA, "closePosition", id)

	_, held := e.position(positionsTraderA)
	require.False(t, held)
	require.Equal(t, *uint256.NewInt(115_601_575), e.collateral(positionsTraderA))
	require.Equal(t, moduleBefore, e.usdlBalance(module))
	require.Zero(t, e.usdlBalance(positionsTraderA).Sign())
	logs, fields := e.perpsLogs("PositionClosed")
	require.Len(t, logs, 1)
	require.Equal(t, []common.Hash{e.perpsABI.Events["PositionClosed"].ID, positionsTopic(id),
		positionsAddressTopic(positionsTraderA), positionsTopic(big.NewInt(0))}, logs[0].Topics)
	require.Equal(t, new(big.Int).Div(positionsE18(10_065), big.NewInt(10)), fields[0]["closedSizeUsd"])
	require.Equal(t, positionsE18(61_000), fields[0]["exitPrice"])
	require.Equal(t, new(big.Int).Div(positionsE18(165), big.NewInt(10)), fields[0]["realizedPnl"])
	require.Equal(t, true, fields[0]["isFullClose"])
	settled, _ := e.perpsLogs("TradeSettled")
	require.NotEmpty(t, settled)

	e.requireRevert("PerpsUnknownPosition", positionsTraderA, "closePosition", id)
}

func TestPositionsPartialCloseRoundsToLot(t *testing.T) {
	e := newPositionsEnv(t, 100_000_000)
	id := e.open()
	e.cancelAsk()
	e.rest(e.ctx(), positionsBidderC, true, 61_000)
	e.setMark(positionsE18(61_000))

	above := new(big.Int).Add(new(big.Int).Div(positionsE18(10_065), big.NewInt(10)), big.NewInt(1))
	e.requireRevert("PerpsCloseSizeExceedsPosition", positionsTraderA, "partialClose", id, above)
	e.requireRevert("PerpsZeroAmount", positionsTraderA, "partialClose", id, big.NewInt(0))

	e.mustCall(positionsTraderA, "partialClose", id, positionsE18(500))

	position, ok := e.position(positionsTraderA)
	require.True(t, ok)
	require.Equal(t, id, position.ID.ToBig())
	require.Equal(t, positionsWord(t, "8310000000000000"), position.SizeBase)
	require.Equal(t, *uint256.MustFromBig(positionsE18(60_000)), position.EntryPrice)
	logs, fields := e.perpsLogs("PositionClosed")
	require.Len(t, logs, 1)
	require.Equal(t, positionsTopic(id), logs[0].Topics[1])
	require.Equal(t, new(big.Int).Div(positionsE18(49_959), big.NewInt(100)), fields[0]["closedSizeUsd"])
	require.Equal(t, positionsE18(61_000), fields[0]["exitPrice"])
	require.Equal(t, false, fields[0]["isFullClose"])
}

func TestPositionsClosePositionWithParamsBounds(t *testing.T) {
	e := newPositionsEnv(t, 100_000_000)
	id := e.open()
	e.cancelAsk()
	e.rest(e.ctx(), positionsBidderC, true, 61_000)
	now := uint64(positionsNowMs / 1_000)

	e.requireRevert("PerpsSlippageExceeded", positionsTraderA, "closePositionWithParams", id, big.NewInt(0),
		positionsE18(61_500), uint64(0))
	e.requireRevert("PerpsDeadlineExpired", positionsTraderA, "closePositionWithParams", id, big.NewInt(0),
		positionsE18(61_000), now-1)
	position, ok := e.position(positionsTraderA)
	require.True(t, ok)
	require.Equal(t, positionsWord(t, "16500000000000000"), position.SizeBase)

	e.mustCall(positionsTraderA, "closePositionWithParams", id, big.NewInt(0), positionsE18(61_000), now)

	_, held := e.position(positionsTraderA)
	require.False(t, held)
	logs, fields := e.perpsLogs("PositionClosed")
	require.Len(t, logs, 1)
	require.Equal(t, positionsE18(61_000), fields[0]["exitPrice"])
	require.Equal(t, true, fields[0]["isFullClose"])
}

func TestPositionsLiquidatePermissionless(t *testing.T) {
	e := newPositionsEnv(t, 100_000_000)
	id := e.open()
	e.rest(e.ctx(), positionsBidderC, true, 54_000)

	e.requireRevert("PerpsNotLiquidatable", positionsKeeperK, "liquidate", id)

	e.setMark(positionsE18(53_990))
	e.setPaused(true)
	e.requireRevert("PerpsPaused", positionsKeeperK, "liquidate", id)
	e.setPaused(false)
	keeperBefore := e.collateral(positionsKeeperK)

	e.mustCall(positionsKeeperK, "liquidate", id)

	_, held := e.position(positionsTraderA)
	require.False(t, held)
	require.Equal(t, keeperBefore, e.collateral(positionsKeeperK))
	logs, fields := e.perpsLogs("Liquidation")
	require.Len(t, logs, 1)
	require.Equal(t, []common.Hash{e.perpsABI.Events["Liquidation"].ID, positionsTopic(id),
		positionsAddressTopic(positionsTraderA), positionsTopic(big.NewInt(0))}, logs[0].Topics)
	require.Equal(t, positionsE18(53_990), fields[0]["liquidationPrice"])
	require.Zero(t, fields[0]["penalty"].(*big.Int).Sign())
	require.Equal(t, positionsKeeperK, fields[0]["keeper"])
}
