package perps_test

import (
	"encoding/json"
	"maps"
	"math"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	nodeapp "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	perpsprecompile "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	sdktestutil "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/module"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// serviceRecorder records the services RegisterServices registers.
type serviceRecorder struct{ services []string }

func (r *serviceRecorder) RegisterService(sd *grpc.ServiceDesc, _ interface{}) {
	r.services = append(r.services, sd.ServiceName)
}

// invariantRecorder records the routes RegisterInvariants registers.
type invariantRecorder struct{ routes []string }

func (r *invariantRecorder) RegisterRoute(moduleName, route string, _ sdk.Invariant) {
	r.routes = append(r.routes, moduleName+"/"+route)
}

func TestModuleShape(t *testing.T) {
	key := sdk.NewKVStoreKey(types.StoreKey)
	ctx := sdktestutil.DefaultContext(key, sdk.NewTransientStoreKey("transient_perps")).
		WithBlockTime(time.Unix(1_800_000_000, 0))
	am := perps.NewAppModule(keeper.NewKeeper(key, nil, nil))

	var _ module.AppModule = am
	require.Equal(t, "perps", am.Name())
	require.Equal(t, "perps", am.AppModuleBasic.Name())
	require.Equal(t, uint64(1), am.ConsensusVersion())
	require.Equal(t, sdk.Route{}, am.Route())
	require.Equal(t, types.ModuleName, am.QuerierRoute())
	require.Nil(t, am.LegacyQuerierHandler(nil))
	require.Nil(t, am.GetTxCmd())
	require.Nil(t, am.GetQueryCmd())

	msgServer, queryServer := &serviceRecorder{}, &serviceRecorder{}
	am.RegisterServices(module.NewConfigurator(nil, msgServer, queryServer))
	require.Empty(t, msgServer.services)
	require.Empty(t, queryServer.services)

	invariants := &invariantRecorder{}
	am.RegisterInvariants(invariants)
	require.Equal(t, []string{
		"perps/" + keeper.SolvencyInvariantName, "perps/" + keeper.OpenInterestInvariantName,
		"perps/" + keeper.BookInvariantName, "perps/" + keeper.OrdersInvariantName,
		"perps/" + keeper.PositionsInvariantName, "perps/" + keeper.FundingInvariantName,
		"perps/" + keeper.CountersInvariantName, "perps/" + keeper.DeterminismInvariantName,
		"perps/" + keeper.IndexesInvariantName,
	}, invariants.routes)

	defaultGenesis := am.DefaultGenesis(nil)
	want, err := json.Marshal(types.DefaultGenesis())
	require.NoError(t, err)
	require.JSONEq(t, string(want), string(defaultGenesis))
	require.NoError(t, am.ValidateGenesis(nil, nil, defaultGenesis))

	genesisCh := make(chan json.RawMessage, 2)
	genesisCh <- defaultGenesis
	genesisCh <- json.RawMessage(`{"params":`)
	close(genesisCh)
	require.ErrorIs(t, am.ValidateGenesisStream(nil, nil, genesisCh), types.ErrInvalidGenesis)

	err = am.ValidateGenesis(nil, nil, json.RawMessage(`{"params":`))
	require.ErrorIs(t, err, types.ErrInvalidGenesis)
	require.Contains(t, err.Error(), "LXP_PERPS_INVALID_GENESIS")

	invalid := types.DefaultGenesis()
	invalid.Params.CollateralToken = common.Address{}
	encoded, err := json.Marshal(invalid)
	require.NoError(t, err)
	err = am.ValidateGenesis(nil, nil, encoded)
	require.ErrorIs(t, err, types.ErrInvalidGenesis)
	require.ErrorIs(t, err, types.ErrInvalidParams)

	require.Panics(t, func() { am.InitGenesis(ctx, nil, json.RawMessage(`{"params":`)) })
	require.Panics(t, func() { am.InitGenesis(ctx, nil, encoded) })

	require.Equal(t, []abci.ValidatorUpdate{}, am.InitGenesis(ctx, nil, defaultGenesis))
	exported := am.ExportGenesis(ctx, nil)
	require.JSONEq(t, string(defaultGenesis), string(exported))

	var streamed []json.RawMessage
	for chunk := range am.ExportGenesisStream(ctx, nil) {
		streamed = append(streamed, chunk)
	}
	require.Len(t, streamed, 1)
	require.JSONEq(t, string(exported), string(streamed[0]))
}

// domainOrder is the OrderParams tuple of placeOrder.
type domainOrder struct {
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

// domainEvent is a log of 0x1015 decoded against the perps ABI.
type domainEvent struct {
	name   string
	topics []common.Hash
	fields map[string]interface{}
}

// domainInvariants keeps the invariants RegisterInvariants registers.
type domainInvariants map[string]sdk.Invariant

func (r domainInvariants) RegisterRoute(moduleName, route string, invariant sdk.Invariant) {
	r[moduleName+"/"+route] = invariant
}

// domainChain is the application's perps keeper, the module built over it
// and the precompile built from the application's precompile keepers, served
// at 0x1015 in a real EVM over the application's EVM keeper, with the mainnet
// USDL runtime named the collateral and BTC (0) listed.
type domainChain struct {
	t          *testing.T
	app        *nodeapp.App
	k          *keeper.Keeper
	am         perps.AppModule
	ctx        sdk.Context
	abi        abi.ABI
	precompile vm.PrecompiledContract
	usdl       common.Address
	invariants domainInvariants
}

var (
	domainVenue = common.HexToAddress("0x0000000000000000000000000000000000001015")
	domainA     = common.BytesToAddress(crypto.Keccak256([]byte("perps domain trader A"))[12:])
	domainB     = common.BytesToAddress(crypto.Keccak256([]byte("perps domain maker B"))[12:])
)

const (
	// domainStartSeconds is the block time of the first block, ten seconds
	// into a whole hour.
	domainStartSeconds int64 = 1_800_000_010
	// domainHolding is the USDL each trader holds: 1,000 USDL.
	domainHolding int64  = 1_000_000_000
	domainGas     uint64 = 30_000_000
)

func domainUsd(whole int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(whole), big.NewInt(1_000_000_000_000_000_000))
}

func domainWord(value int64) []byte { return common.LeftPadBytes(big.NewInt(value).Bytes(), 32) }

func newDomainChain(t *testing.T) *domainChain {
	t.Helper()
	application := nodeapp.Setup(t, false, false, false)
	ctx := application.GetContextForDeliverTx([]byte{}).WithBlockHeight(10).
		WithBlockTime(time.Unix(domainStartSeconds, 0).UTC()).WithEventManager(sdk.NewEventManager())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	parsed, err := perpsabi.ABI()
	require.NoError(t, err)
	c := &domainChain{t: t, app: application, k: application.PerpsKeeper, ctx: ctx, abi: parsed,
		invariants: domainInvariants{}}

	// The module is built over the application's keeper and its genesis
	// round-trips through that keeper.
	c.am = perps.NewAppModule(c.k)
	genesis := c.am.DefaultGenesis(nil)
	require.Equal(t, []abci.ValidatorUpdate{}, c.am.InitGenesis(ctx, nil, genesis))
	require.JSONEq(t, string(genesis), string(c.am.ExportGenesis(ctx, nil)))
	c.am.RegisterInvariants(c.invariants)
	require.Len(t, c.invariants, 9)

	// The precompile is built from the application's precompile keepers,
	// which hand it the same perps keeper.
	keepers := application.GetPrecompileKeepers()
	wired, ok := keepers.(perpsprecompile.PerpsKeepers)
	require.True(t, ok, "the application's precompile keepers carry the perps keeper")
	require.Same(t, c.k, wired.PerpsK())
	c.precompile, err = perpsprecompile.NewPrecompile(keepers)
	require.NoError(t, err)

	// The mainnet USDL runtime (_balances at slot 0, _totalSupply at slot 2)
	// holds 1,000 USDL for each trader and is named the collateral token.
	raw, err := os.ReadFile("../../precompiles/perps/testdata/usdl_runtime.hex")
	require.NoError(t, err)
	code := common.FromHex(strings.TrimSpace(string(raw)))
	require.NotEmpty(t, code)
	c.usdl = common.HexToAddress("0x85FcD13735F4309833A503EE804ea32395851479")
	application.EvmKeeper.SetCode(ctx, c.usdl, code)
	for _, holder := range []common.Address{domainA, domainB} {
		slot := crypto.Keccak256Hash(common.LeftPadBytes(holder.Bytes(), 32), domainWord(0))
		application.EvmKeeper.SetState(ctx, c.usdl, slot, common.BigToHash(big.NewInt(domainHolding)))
	}
	application.EvmKeeper.SetState(ctx, c.usdl, common.BigToHash(big.NewInt(2)),
		common.BigToHash(big.NewInt(2*domainHolding)))
	params, err := c.k.GetParams(ctx)
	require.NoError(t, err)
	params.CollateralToken = c.usdl
	require.NoError(t, c.k.UpdateParams(ctx, c.k.Authority(), params))
	require.NoError(t, c.k.SetMarket(ctx, c.k.Authority(), types.Market{
		ID: 0, Symbol: "BTC", Name: "Bitcoin", OracleSymbol: "BTC", SzDecimals: 5, PxDecimals: 1,
		MaxLeverage: types.VenueMaxLeverage, TierClass: types.TierClassA,
		ImpactNotionalUsd:    sdk.NewIntWithDecimal(20_000, 18),
		OiCapUsd:             sdk.NewIntWithDecimal(1_000_000_000, 18),
		FundingCapPpmPerHour: 40_000, Enabled: true,
	}))
	require.Equal(t, domainHolding, c.balanceOf(domainA))
	require.Equal(t, domainHolding, c.balanceOf(domainB))
	return c
}

// setIndex stores the BTC aggregate of the application's pricefeed store at
// 50,000 USD for index and mark, stamped at the block time.
func (c *domainChain) setIndex() {
	c.t.Helper()
	bz, err := json.Marshal(pricefeedtypes.Aggregate{
		IndexPrice:  sdk.NewIntWithDecimal(50_000, 18),
		MarkPrice:   sdk.NewIntWithDecimal(50_000, 18),
		Basis:       sdk.ZeroInt(),
		TimestampMs: uint64(c.ctx.BlockTime().UnixMilli()), //nolint:gosec
		Height:      c.ctx.BlockHeight(),
		Sources:     1,
	})
	require.NoError(c.t, err)
	c.ctx.KVStore(c.app.GetKey(pricefeedtypes.StoreKey)).Set(pricefeedtypes.AggregateKey(0), bz)
}

// begin moves to the next block at seconds with a fresh BTC index.
func (c *domainChain) begin(seconds int64) {
	c.ctx = c.ctx.WithBlockHeight(c.ctx.BlockHeight() + 1).WithBlockTime(time.Unix(seconds, 0).UTC()).
		WithEventManager(sdk.NewEventManager())
	c.app.EvmKeeper.BeginBlock(c.ctx)
	c.setIndex()
}

// endBlock runs the module's EndBlocker and requires no step to have failed
// and every invariant the module registers to hold.
func (c *domainChain) endBlock() {
	c.t.Helper()
	perps.EndBlocker(c.ctx, *c.k)
	for _, event := range c.ctx.EventManager().Events() {
		require.NotEqual(c.t, "perps_endblock_error", event.Type, "EndBlock step failed: %v", event.Attributes)
	}
	for route, invariant := range c.invariants {
		message, broken := invariant(c.ctx)
		require.False(c.t, broken, "%s: %s", route, message)
	}
}

// receipt is the perps synthetic receipt of the block, its logs decoded
// against the perps ABI.
func (c *domainChain) receipt() []domainEvent {
	c.t.Helper()
	kind := evmkeeper.SyntheticReceiptPerps
	receipt, err := c.app.EvmKeeper.GetTransientReceipt(c.ctx, evmkeeper.SyntheticReceiptHash(kind, c.ctx.BlockHeight()),
		uint64(evmkeeper.SyntheticReceiptProvisionalIndex(kind)))
	require.NoError(c.t, err)
	return c.decode(evmkeeper.GetLogsForTx(receipt, 0))
}

// tx calls method of 0x1015 from caller through the EVM, requires success,
// commits the state and returns the decoded perps logs, the USDL Transfer
// logs as [from, to, amount] and the outputs.
func (c *domainChain) tx(from common.Address, method string, args ...interface{}) ([]domainEvent, [][3]*big.Int,
	[]interface{}) {
	c.t.Helper()
	input, err := c.abi.Pack(method, args...)
	require.NoError(c.t, err)
	stateDB := state.NewDBImpl(c.ctx, &c.app.EvmKeeper, false)
	blockCtx, err := c.app.EvmKeeper.GetVMBlockContext(c.ctx, core.GasPool(math.MaxUint64))
	require.NoError(c.t, err)
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(c.app.EvmKeeper.ChainID(c.ctx))
	contracts := map[common.Address]vm.PrecompiledContract{}
	maps.Copy(contracts, c.app.EvmKeeper.CustomPrecompiles(c.ctx))
	contracts[domainVenue] = c.precompile
	evm := vm.NewEVM(*blockCtx, stateDB, cfg, vm.Config{}, contracts)
	ret, _, err := evm.Call(from, domainVenue, input, domainGas, uint256.NewInt(0))
	require.NoError(c.t, err, "%s reverted with 0x%x", method, ret)
	logs := stateDB.GetAllLogs()
	_, err = stateDB.Finalize()
	require.NoError(c.t, err)
	outputs, err := c.abi.Unpack(method, ret)
	require.NoError(c.t, err)
	transferTopic := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	var transfers [][3]*big.Int
	for _, log := range logs {
		if log.Address == c.usdl && log.Topics[0] == transferTopic {
			transfers = append(transfers, [3]*big.Int{log.Topics[1].Big(), log.Topics[2].Big(),
				new(big.Int).SetBytes(log.Data)})
		}
	}
	return c.decode(logs), transfers, outputs
}

// decode decodes every log of 0x1015 against the perps ABI, requiring each
// to be a perps event.
func (c *domainChain) decode(logs []*ethtypes.Log) []domainEvent {
	c.t.Helper()
	var out []domainEvent
	for _, log := range logs {
		if log.Address != domainVenue {
			continue
		}
		event, err := c.abi.EventByID(log.Topics[0])
		require.NoError(c.t, err)
		fields := map[string]interface{}{}
		require.NoError(c.t, event.Inputs.NonIndexed().UnpackIntoMap(fields, log.Data), event.Name)
		out = append(out, domainEvent{name: event.Name, topics: log.Topics, fields: fields})
	}
	return out
}

// requireBig requires a decoded integer field to equal want.
func requireBig(t *testing.T, want *big.Int, got interface{}) {
	t.Helper()
	value, ok := got.(*big.Int)
	require.True(t, ok, "%v is not an integer", got)
	require.Zero(t, want.Cmp(value), "want %s, got %s", want, value)
}

func domainCount(events []domainEvent, name string) int {
	n := 0
	for _, event := range events {
		if event.name == name {
			n++
		}
	}
	return n
}

// only is the single event named name in events.
func (c *domainChain) only(events []domainEvent, name string) domainEvent {
	c.t.Helper()
	require.Equal(c.t, 1, domainCount(events, name), "%s in %v", name, events)
	for _, event := range events {
		if event.name == name {
			return event
		}
	}
	return domainEvent{}
}

// of is the event named name whose user topic, its second indexed argument,
// is holder.
func (c *domainChain) of(events []domainEvent, name string, holder common.Address) domainEvent {
	c.t.Helper()
	topic := common.BytesToHash(holder.Bytes())
	for _, event := range events {
		if event.name == name && event.topics[2] == topic {
			return event
		}
	}
	c.t.Fatalf("no %s of %s in %v", name, holder, events)
	return domainEvent{}
}

func (c *domainChain) usdlCall(caller common.Address, signature string, words ...[]byte) []byte {
	c.t.Helper()
	input := crypto.Keccak256([]byte(signature))[:4]
	for _, word := range words {
		input = append(input, word...)
	}
	ret, err := c.app.EvmKeeper.CallEVM(c.ctx, caller, &c.usdl, nil, input)
	require.NoError(c.t, err)
	return ret
}

func (c *domainChain) approve(owner common.Address, amount int64) {
	c.t.Helper()
	ret := c.usdlCall(owner, "approve(address,uint256)", common.LeftPadBytes(domainVenue.Bytes(), 32),
		domainWord(amount))
	require.Equal(c.t, domainWord(1), ret)
}

func (c *domainChain) balanceOf(holder common.Address) int64 {
	c.t.Helper()
	value := new(big.Int).SetBytes(c.usdlCall(holder, "balanceOf(address)", common.LeftPadBytes(holder.Bytes(), 32)))
	require.True(c.t, value.IsInt64())
	return value.Int64()
}

func (c *domainChain) account(holder common.Address) types.Account {
	c.t.Helper()
	account, err := c.k.GetAccount(c.ctx, holder)
	require.NoError(c.t, err)
	return account
}

func (c *domainChain) collateral(holder common.Address) int64 {
	c.t.Helper()
	collateral := c.account(holder).Collateral
	require.True(c.t, collateral.IsUint64())
	return int64(collateral.Uint64()) //nolint:gosec
}

func domainOrderOf(isLong bool, orderType types.OrderType, tif types.TimeInForce, sizeUsd, leverage,
	price int64) domainOrder {
	return domainOrder{MarketId: new(big.Int), IsLong: isLong, OrderType: uint8(orderType),
		TimeInForce: uint8(tif), SizeUsd: domainUsd(sizeUsd), Leverage: domainUsd(leverage),
		LimitPrice: domainUsd(price), TriggerPrice: new(big.Int), AcceptablePrice: new(big.Int),
		CollateralAmount: new(big.Int), TakeProfitPrice: new(big.Int), StopLossPrice: new(big.Int)}
}

// TestPerpsDomainWiring builds the perps module over the application's perps
// keeper and the precompile from the application's precompile keepers, and
// runs one deposit, order, fill, funding hour and close through them: maker B
// rests a 1,000 USD ask at 50,000 USD, trader A buys it at market, the next
// whole hour's EndBlock charges A's long the 100 ppm per 8 h interest and
// credits B's short, and A closes against B's bid; both accounts end flat and
// withdrawn with exactly the fees and funding of contracts/perps-parameters.md
// gone, and every module invariant holds after each block.
func TestPerpsDomainWiring(t *testing.T) {
	c := newDomainChain(t)
	zero := new(big.Int)
	price := domainUsd(50_000)
	custody := c.k.ModuleEVMAddress()

	// The first block's EndBlock fixes the BTC mark at the index and starts
	// the hourly funding index at the block's hour.
	c.setIndex()
	c.endBlock()
	mark := c.only(c.receipt(), types.EventMarkPriceUpdated)
	requireBig(t, price, mark.fields["markPrice"])
	requireBig(t, price, mark.fields["indexPrice"])
	market, err := c.k.GetMarketState(c.ctx, 0)
	require.NoError(t, err)
	require.Equal(t, uint64(domainStartSeconds/3600), market.LastIndexHour)
	require.True(t, market.FundingIndex.IsZero())

	// Deposit: A books 200 USDL and B 100 USDL by allowance to 0x1015.
	c.begin(domainStartSeconds + 1)
	for _, deposit := range []struct {
		holder common.Address
		amount int64
	}{{domainA, 200_000_000}, {domainB, 100_000_000}} {
		c.approve(deposit.holder, deposit.amount)
		_, _, out := c.tx(deposit.holder, "createVault")
		require.Equal(t, []interface{}{domainVenue}, out)
		events, transfers, _ := c.tx(deposit.holder, "deposit", c.usdl, big.NewInt(deposit.amount))
		require.Equal(t, [][3]*big.Int{{deposit.holder.Big(), custody.Big(), big.NewInt(deposit.amount)}}, transfers)
		deposited := c.only(events, types.EventCollateralDeposited)
		requireBig(t, big.NewInt(deposit.amount), deposited.fields["amount"])
		requireBig(t, big.NewInt(deposit.amount), deposited.fields["newBalance"])
		require.Equal(t, deposit.amount, c.collateral(deposit.holder))
	}
	require.Equal(t, int64(300_000_000), c.balanceOf(custody))

	// Order: B rests a GTC ask of 1,000 USD (0.02 BTC) at 50,000 USD and 20x.
	events, transfers, out := c.tx(domainB, "placeOrder",
		domainOrderOf(false, types.OrderTypeLimit, types.TimeInForceGTC, 1_000, 20, 50_000))
	require.Empty(t, transfers)
	require.Zero(t, domainCount(events, types.EventFill))
	require.Len(t, out, 1)
	askID, ok := out[0].(*big.Int)
	require.True(t, ok)
	ask, err := c.k.GetOrder(c.ctx, *uint256.MustFromBig(askID))
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusResting, ask.Status)

	// Fill: A buys 1,000 USD at market and 10x against B's ask; A pays the
	// tier-0 taker fee of 450 ppm (0.45 USD) and B the maker fee of 150 ppm
	// (0.15 USD).
	events, transfers, _ = c.tx(domainA, "placeOrder",
		domainOrderOf(true, types.OrderTypeMarket, types.TimeInForceIOC, 1_000, 10, 0))
	require.Empty(t, transfers)
	fill := c.only(events, types.EventFill)
	require.Equal(t, domainB, fill.fields["maker"])
	require.Equal(t, domainA, fill.fields["taker"])
	require.Equal(t, true, fill.fields["takerIsLong"])
	requireBig(t, price, fill.fields["price"])
	requireBig(t, domainUsd(1_000), fill.fields["sizeUsd"])
	requireBig(t, big.NewInt(150_000_000_000_000_000), fill.fields["makerFeeUsd"])
	requireBig(t, big.NewInt(450_000_000_000_000_000), fill.fields["takerFeeUsd"])
	opened := c.of(events, types.EventPositionOpened, domainA)
	require.Equal(t, true, opened.fields["isLong"])
	requireBig(t, price, opened.fields["entryPrice"])
	require.Equal(t, false, c.of(events, types.EventPositionOpened, domainB).fields["isLong"])
	positionID := opened.topics[1].Big()
	position, err := c.k.GetPosition(c.ctx, *uint256.MustFromBig(positionID))
	require.NoError(t, err)
	require.Equal(t, *uint256.NewInt(20_000_000_000_000_000), position.SizeBase)
	require.Equal(t, int64(200_000_000-450_000), c.collateral(domainA))
	require.Equal(t, int64(100_000_000-150_000), c.collateral(domainB))
	c.endBlock()
	market, err = c.k.GetMarketState(c.ctx, 0)
	require.NoError(t, err)
	require.True(t, market.FundingIndex.IsZero(), "no funding inside the hour")

	// Funding hour: the next whole hour fixes the hourly rate at the 100 ppm
	// per 8 h interest over 8 (12.5 ppm), the index moves 50,000 x 12.5 ppm =
	// 0.625 USD per BTC and the sweep books 0.02 BTC x 0.625 = 0.0125 USD from
	// A's long to B's short.
	c.begin(domainStartSeconds + 3_600)
	c.endBlock()
	records := c.receipt()
	requireBig(t, big.NewInt(12_500_000_000_000), c.only(records, types.EventFundingSettled).fields["fundingRate"])
	require.Equal(t, 2, domainCount(records, types.EventPositionFundingApplied))
	market, err = c.k.GetMarketState(c.ctx, 0)
	require.NoError(t, err)
	require.Equal(t, uint64(domainStartSeconds/3600+1), market.LastIndexHour)
	require.Equal(t, *uint256.NewInt(625_000_000_000_000_000), market.FundingIndex)
	require.Equal(t, int64(200_000_000-450_000-12_500), c.collateral(domainA))
	require.Equal(t, int64(100_000_000-150_000+12_500), c.collateral(domainB))

	// Close: B rests a reduce-only bid of 1,000 USD at 50,000 USD against its
	// short and A closes its long against it; A pays the 450 ppm taker fee
	// again and B, the venue's only maker, earns the 30 ppm rebate (0.03 USD)
	// of the top rebate tier.
	c.begin(domainStartSeconds + 3_601)
	closeBid := domainOrderOf(true, types.OrderTypeLimit, types.TimeInForceGTC, 1_000, 20, 50_000)
	closeBid.ReduceOnly = true
	events, transfers, _ = c.tx(domainB, "placeOrder", closeBid)
	require.Empty(t, transfers)
	require.Zero(t, domainCount(events, types.EventFill))
	events, transfers, _ = c.tx(domainA, "closePosition", positionID)
	require.Empty(t, transfers)
	fill = c.only(events, types.EventFill)
	require.Equal(t, domainB, fill.fields["maker"])
	require.Equal(t, domainA, fill.fields["taker"])
	require.Equal(t, false, fill.fields["takerIsLong"])
	requireBig(t, price, fill.fields["price"])
	requireBig(t, domainUsd(1_000), fill.fields["sizeUsd"])
	requireBig(t, big.NewInt(-30_000_000_000_000_000), fill.fields["makerFeeUsd"])
	requireBig(t, big.NewInt(450_000_000_000_000_000), fill.fields["takerFeeUsd"])
	closed := c.of(events, types.EventPositionClosed, domainA)
	require.Equal(t, opened.topics[1], closed.topics[1])
	requireBig(t, price, closed.fields["exitPrice"])
	requireBig(t, zero, closed.fields["realizedPnl"])
	require.Equal(t, true, closed.fields["isFullClose"])
	require.Zero(t, c.account(domainA).PositionCount)
	require.Zero(t, c.account(domainB).PositionCount)
	finalA, finalB := int64(200_000_000-450_000-12_500-450_000), int64(100_000_000-150_000+12_500+30_000)
	require.Equal(t, finalA, c.collateral(domainA))
	require.Equal(t, finalB, c.collateral(domainB))
	c.endBlock()

	// Both withdraw everything: the venue keeps exactly the fees net of the
	// rebate, 0.45 + 0.15 + 0.45 - 0.03 = 1.02 USDL.
	c.begin(domainStartSeconds + 3_602)
	for _, withdrawal := range []struct {
		holder common.Address
		amount int64
	}{{domainA, finalA}, {domainB, finalB}} {
		events, transfers, _ := c.tx(withdrawal.holder, "withdraw", c.usdl, big.NewInt(withdrawal.amount))
		require.Equal(t, [][3]*big.Int{{custody.Big(), withdrawal.holder.Big(), big.NewInt(withdrawal.amount)}},
			transfers)
		withdrawn := c.only(events, types.EventCollateralWithdrawn)
		requireBig(t, big.NewInt(withdrawal.amount), withdrawn.fields["amount"])
		requireBig(t, zero, withdrawn.fields["newBalance"])
		require.Zero(t, c.collateral(withdrawal.holder))
	}
	require.Equal(t, domainHolding-450_000-12_500-450_000, c.balanceOf(domainA))
	require.Equal(t, domainHolding-150_000+12_500+30_000, c.balanceOf(domainB))
	require.Equal(t, int64(1_020_000), c.balanceOf(custody))
	c.endBlock()
}
