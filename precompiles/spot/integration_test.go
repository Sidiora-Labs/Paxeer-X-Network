package spot_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"testing"
	"time"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	layerxbridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	minttypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/mint/types"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	spotmodule "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot"
	spotkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	spottypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/spot"
	putils "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/utils"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

const (
	hyperpaxGas          = uint64(30_000_000)
	hyperpaxSlippageBps  = int64(50)
	hyperpaxDeadline     = int64(60)
	hyperpaxOrderExpiry  = int64(86_400)
	hyperpaxStartHeight  = int64(10)
	hyperpaxStartSeconds = int64(1_800_000_000)
)

var (
	hyperpaxPECOR    = common.HexToAddress(spottypes.PECORAddress)
	hyperpaxOrders   = common.HexToAddress(spottypes.PECOROrdersAddress)
	hyperpaxStops    = common.HexToAddress(spottypes.PECORStopOrdersAddress)
	hyperpaxQuoter   = common.HexToAddress(spottypes.PECORQuoterAddress)
	hyperpaxVault    = common.HexToAddress(spottypes.PECORVaultAddress)
	hyperpaxRails    = []common.Address{hyperpaxPECOR, hyperpaxOrders, hyperpaxStops, hyperpaxQuoter, hyperpaxVault}
	hyperpaxUnmapped = []common.Address{
		common.HexToAddress("0x0000000000000000000000000000000000001026"),
		common.HexToAddress("0x0000000000000000000000000000000000001027"),
	}
	hyperpaxTransferTopic = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
)

// hyperpaxKeepers are the application keepers with the spot keeper of the
// replay beside them.
type hyperpaxKeepers struct {
	putils.Keepers
	spot *spotkeeper.Keeper
}

func (k hyperpaxKeepers) SpotK() *spotkeeper.Keeper { return k.spot }

type hyperpaxLimitTuple struct {
	Id          *big.Int
	User        common.Address
	Stablecoin  common.Address
	Token       common.Address
	Amount      *big.Int
	TargetPrice *big.Int
	OrderType   uint8
	Status      uint8
	CreatedAt   *big.Int
	ExpiresAt   *big.Int
}

// hyperpaxBalances are the holdings a spot call moves: USDL and PAX of the
// trader and of custody, and the USDL and PAX reserves of the vault.
type hyperpaxBalances struct {
	UserUSDL, CustodyUSDL, UserPAX, CustodyPAX, ReserveUSDL, ReservePAX *big.Int
}

type hyperpaxDelta struct {
	UserUSDL, CustodyUSDL, UserPAX, CustodyPAX, ReserveUSDL, ReservePAX int64
}

type hyperpaxTx struct {
	receipt *evmtypes.Receipt
	logs    []*ethtypes.Log
	ret     []byte
}

// hyperpaxEnv is a test application with a spot keeper over its own store,
// the real price feed, bank and EVM keepers of the application, USDL at its
// fixed address and the five rails of the spot precompile set.
type hyperpaxEnv struct {
	t       *testing.T
	a       *app.App
	ctx     sdk.Context
	k       *spotkeeper.Keeper
	set     map[common.Address]*pcommon.Precompile
	erc20   abi.ABI
	usdl    common.Address
	pax     common.Address
	sid     common.Address
	user    common.Address
	lp      common.Address
	txIndex int
}

func hyperpaxAddress(label string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte(label)))
}

func hyperpaxTopic(address common.Address) common.Hash { return common.BytesToHash(address.Bytes()) }

func hyperpaxID(id uint64) common.Hash { return common.BigToHash(new(big.Int).SetUint64(id)) }

func hyperpaxUSD(dollars int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(dollars), big.NewInt(1e18))
}

func hyperpaxStrings(values ...interface{}) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = fmt.Sprint(value)
	}
	return out
}

func hyperpaxStrip(log *ethtypes.Log) *ethtypes.Log {
	stripped := &ethtypes.Log{Address: log.Address, Topics: append([]common.Hash{}, log.Topics...)}
	if len(log.Data) > 0 {
		stripped.Data = append([]byte{}, log.Data...)
	}
	return stripped
}

func newHyperpaxEnv(t *testing.T) *hyperpaxEnv {
	t.Helper()
	wrapper := app.NewTestWrapper(t, time.Unix(hyperpaxStartSeconds, 0).UTC(), secp256k1.GenPrivKey().PubKey(), true)
	a := wrapper.App
	ctx := wrapper.Ctx.WithBlockHeight(hyperpaxStartHeight).WithBlockTime(time.Unix(hyperpaxStartSeconds, 0).UTC())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	a.EvmKeeper.BeginBlock(ctx)
	key := sdk.NewKVStoreKey(spottypes.StoreKey)
	ctx = launchpadtest.WithStore(ctx, key)
	k := spotkeeper.NewKeeper(key, a.BankKeeper, &a.EvmKeeper, a.PricefeedKeeper)
	raw, err := os.ReadFile("../launchpad/abi/SidioraERC20.json")
	require.NoError(t, err)
	erc20, err := abi.JSON(bytes.NewReader(raw))
	require.NoError(t, err)
	set, err := spot.NewPrecompileSet(hyperpaxKeepers{a.GetPrecompileKeepers(), k})
	require.NoError(t, err)
	e := &hyperpaxEnv{t: t, a: a, ctx: ctx, k: k, set: set, erc20: erc20,
		usdl: common.HexToAddress(spottypes.USDLAddress),
		user: hyperpaxAddress("hyperpax trader"), lp: hyperpaxAddress("hyperpax liquidity provider")}

	e.price(spottypes.PAXMarketID, 5e17)
	e.price(spottypes.SIDMarketID, 2e18)
	e.installUSDL()
	e.setMetadata(evmkeeper.BaseDenom, "PAX")
	e.setMetadata(layerxbridgetypes.SidioraDenom(), layerxbridgetypes.SidioraSymbol)
	e.pax = e.createPointer(evmkeeper.BaseDenom, "PAX")
	e.sid = e.createPointer(layerxbridgetypes.SidioraDenom(), layerxbridgetypes.SidioraSymbol)
	require.NoError(t, spotkeeper.InitV6120(e.ctx, *k))
	params, err := k.GetParams(e.ctx)
	require.NoError(t, err)
	params.SwapFeeBps, params.Tier1FeeBps, params.Tier2FeeBps = 30, 20, 50
	params.OrderFeeBps, params.OrderTier1FeeBps, params.OrderTier2FeeBps = 10, 20, 50
	require.NoError(t, k.UpdateParams(e.ctx, k.Authority(), params))

	e.fund(e.lp, 1_000_000e6)
	e.fund(e.user, 5_000e6)
	e.exec(e.lp, e.usdl, e.erc20Pack("transfer", e.user, big.NewInt(10_000e6)))
	require.Equal(t, "10000000000", e.usdlBalance(e.user).String())

	e.exec(e.lp, e.usdl, e.erc20Pack("approve", hyperpaxVault, big.NewInt(500_000e6)))
	tx := e.exec(e.lp, hyperpaxVault, e.pack(hyperpaxVault, "deposit", e.usdl, big.NewInt(500_000e6)))
	e.requireLogs(tx.logs,
		e.transfer(e.lp, e.custody(), 500_000e6),
		e.event(hyperpaxVault, "ReservesUpdated", []common.Hash{hyperpaxTopic(e.usdl)}, big.NewInt(0), big.NewInt(500_000e6)),
		e.event(hyperpaxVault, "Deposit", []common.Hash{hyperpaxTopic(e.usdl), hyperpaxTopic(e.lp)}, big.NewInt(500_000e6), big.NewInt(500_000e6)))
	tx = e.exec(e.lp, hyperpaxVault, e.pack(hyperpaxVault, "deposit", e.pax, big.NewInt(1_000_000e6)))
	e.requireLogs(tx.logs,
		e.event(hyperpaxVault, "ReservesUpdated", []common.Hash{hyperpaxTopic(e.pax)}, big.NewInt(0), big.NewInt(1_000_000e6)),
		e.event(hyperpaxVault, "Deposit", []common.Hash{hyperpaxTopic(e.pax), hyperpaxTopic(e.lp)}, big.NewInt(1_000_000e6), big.NewInt(1_000_000e6)))
	require.Equal(t, "500000000000", e.reserves(e.usdl).String())
	require.Equal(t, "1000000000000", e.reserves(e.pax).String())
	require.Equal(t, "0", e.bank(e.lp).String())
	return e
}

func (e *hyperpaxEnv) price(market uint64, price int64) {
	e.t.Helper()
	raw, err := json.Marshal(pricefeedtypes.Aggregate{IndexPrice: sdk.NewInt(price), MarkPrice: sdk.NewInt(price), Basis: sdk.ZeroInt(),
		TimestampMs: uint64(e.ctx.BlockTime().UnixMilli()), Height: e.ctx.BlockHeight(), Sources: 1})
	require.NoError(e.t, err)
	e.ctx.KVStore(e.a.GetKey(pricefeedtypes.StoreKey)).Set(pricefeedtypes.AggregateKey(market), raw)
}

// installUSDL deploys the token with the liquidity provider holding the
// supply and moves its code and storage to the fixed USDL address.
func (e *hyperpaxEnv) installUSDL() {
	e.t.Helper()
	args, err := e.erc20.Constructor.Inputs.Pack("USD Ledger", "USDL", big.NewInt(1_000_000e6), e.lp)
	require.NoError(e.t, err)
	deployer := hyperpaxAddress("hyperpax token deployer")
	nonce := e.a.EvmKeeper.GetNonce(e.ctx, deployer)
	_, err = e.a.EvmKeeper.CallEVM(e.ctx, deployer, nil, nil, append(common.FromHex(ordersTokenCreation), args...))
	require.NoError(e.t, err)
	deployed := crypto.CreateAddress(deployer, nonce)
	code := e.a.EvmKeeper.GetCode(e.ctx, deployed)
	require.NotEmpty(e.t, code)
	var slots [][2]common.Hash
	e.a.EvmKeeper.IterateState(e.ctx, func(address common.Address, key, value common.Hash) bool {
		if address == deployed {
			slots = append(slots, [2]common.Hash{key, value})
		}
		return false
	})
	require.NotEmpty(e.t, slots)
	e.a.EvmKeeper.SetCode(e.ctx, e.usdl, code)
	for _, slot := range slots {
		e.a.EvmKeeper.SetState(e.ctx, e.usdl, slot[0], slot[1])
	}
	require.Equal(e.t, "1000000000000", e.usdlBalance(e.lp).String())
}

func (e *hyperpaxEnv) setMetadata(denom, display string) {
	e.a.BankKeeper.SetDenomMetaData(e.ctx, banktypes.Metadata{
		DenomUnits: []*banktypes.DenomUnit{{Denom: denom, Exponent: 0}, {Denom: display, Exponent: spottypes.OpeningTokenDecimals}},
		Base:       denom,
		Display:    display,
		Name:       display,
		Symbol:     display,
	})
}

func (e *hyperpaxEnv) createPointer(denom, symbol string) common.Address {
	e.t.Helper()
	err := e.a.EvmKeeper.RunWithOneOffEVMInstance(e.ctx, func(evm *vm.EVM) error {
		_, upsertErr := e.a.EvmKeeper.UpsertERCNativePointer(e.ctx, evm, denom,
			utils.ERCMetadata{Name: symbol, Symbol: symbol, Decimals: spottypes.OpeningTokenDecimals})
		return upsertErr
	}, func(string, string) {})
	require.NoError(e.t, err)
	pointer, _, exists := e.a.EvmKeeper.GetERC20NativePointer(e.ctx, denom)
	require.True(e.t, exists)
	return pointer
}

func (e *hyperpaxEnv) fund(holder common.Address, amount int64) {
	e.t.Helper()
	coins := sdk.NewCoins(sdk.NewInt64Coin(evmkeeper.BaseDenom, amount))
	require.NoError(e.t, e.a.BankKeeper.MintCoins(e.ctx, minttypes.ModuleName, coins))
	require.NoError(e.t, e.a.BankKeeper.SendCoinsFromModuleToAccount(e.ctx, minttypes.ModuleName, e.a.EvmKeeper.GetPaxAddressOrDefault(e.ctx, holder), coins))
}

func (e *hyperpaxEnv) custody() common.Address { return e.k.CustodyEVMAddress(e.ctx) }

func (e *hyperpaxEnv) precompiles(ctx sdk.Context) map[common.Address]vm.PrecompiledContract {
	precompiles := map[common.Address]vm.PrecompiledContract{}
	for address, precompile := range e.a.EvmKeeper.CustomPrecompiles(ctx) {
		precompiles[address] = precompile
	}
	for address, precompile := range e.set {
		precompiles[address] = precompile
	}
	return precompiles
}

func (e *hyperpaxEnv) evm(ctx sdk.Context, stateDB *state.DBImpl) *vm.EVM {
	e.t.Helper()
	block, err := e.a.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
	require.NoError(e.t, err)
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(e.a.EvmKeeper.ChainID(ctx))
	return vm.NewEVM(*block, stateDB, cfg, vm.Config{NoBaseFee: true}, e.precompiles(ctx))
}

// send runs one EVM transaction as the EVM message server does and returns
// its receipt and the logs read back from the stored receipt.
func (e *hyperpaxEnv) send(from, to common.Address, value *big.Int, data []byte) hyperpaxTx {
	e.t.Helper()
	if value == nil {
		value = new(big.Int)
	}
	ctx := e.ctx.WithTxIndex(e.txIndex).WithIsEVM(true)
	stateDB := state.NewDBImpl(ctx, &e.a.EvmKeeper, false)
	defer stateDB.Cleanup()
	msg := &core.Message{From: from, To: &to, Nonce: stateDB.GetNonce(from), Value: value, GasLimit: hyperpaxGas,
		GasPrice: new(big.Int), GasFeeCap: new(big.Int), GasTipCap: new(big.Int), Data: data}
	evm := e.evm(ctx, stateDB)
	evm.SetTxContext(core.NewEVMTxContext(msg))
	gp := core.GasPool(math.MaxUint64)
	res, err := core.NewStateTransition(evm, msg, &gp, true, true).Execute()
	require.NoError(e.t, err)
	_, err = stateDB.Finalize()
	require.NoError(e.t, err)
	vmError := ""
	if res.Err != nil {
		vmError = res.Err.Error()
	}
	hash := crypto.Keccak256Hash(from.Bytes(), new(big.Int).SetUint64(msg.Nonce).Bytes())
	_, err = e.a.EvmKeeper.WriteReceipt(ctx, stateDB, msg, uint32(ethtypes.DynamicFeeTxType), hash, res.UsedGas, vmError)
	require.NoError(e.t, err)
	receipt, err := e.a.EvmKeeper.GetTransientReceipt(e.ctx, hash, uint64(e.txIndex))
	require.NoError(e.t, err)
	require.Equal(e.t, hash.Hex(), receipt.TxHashHex)
	e.txIndex++
	return hyperpaxTx{receipt: receipt, logs: evmkeeper.GetLogsForTx(receipt, 0), ret: res.ReturnData}
}

func (e *hyperpaxEnv) exec(from, to common.Address, data []byte) hyperpaxTx {
	e.t.Helper()
	tx := e.send(from, to, nil, data)
	require.Equal(e.t, uint32(ethtypes.ReceiptStatusSuccessful), tx.receipt.Status, tx.receipt.VmError)
	return tx
}

func (e *hyperpaxEnv) static(to common.Address, data []byte) ([]byte, error) {
	e.t.Helper()
	stateDB := state.NewDBImpl(e.ctx, &e.a.EvmKeeper, false)
	ret, _, err := e.evm(e.ctx, stateDB).StaticCall(e.user, to, data, hyperpaxGas)
	return ret, err
}

func (e *hyperpaxEnv) pack(rail common.Address, name string, args ...interface{}) []byte {
	e.t.Helper()
	data, err := e.set[rail].GetABI().Pack(name, args...)
	require.NoError(e.t, err, name)
	return data
}

func (e *hyperpaxEnv) unpack(rail common.Address, name string, ret []byte) []interface{} {
	e.t.Helper()
	values, err := e.set[rail].GetABI().Unpack(name, ret)
	require.NoError(e.t, err, name)
	return values
}

func (e *hyperpaxEnv) call(rail common.Address, name string, args ...interface{}) []interface{} {
	e.t.Helper()
	ret, err := e.static(rail, e.pack(rail, name, args...))
	require.NoError(e.t, err, name)
	return e.unpack(rail, name, ret)
}

func (e *hyperpaxEnv) uint(rail common.Address, name string, args ...interface{}) *big.Int {
	e.t.Helper()
	values := e.call(rail, name, args...)
	require.Len(e.t, values, 1)
	return values[0].(*big.Int)
}

func (e *hyperpaxEnv) erc20Pack(name string, args ...interface{}) []byte {
	e.t.Helper()
	data, err := e.erc20.Pack(name, args...)
	require.NoError(e.t, err, name)
	return data
}

func (e *hyperpaxEnv) erc20Uint(name string, args ...interface{}) *big.Int {
	e.t.Helper()
	ret, err := e.static(e.usdl, e.erc20Pack(name, args...))
	require.NoError(e.t, err, name)
	values, err := e.erc20.Unpack(name, ret)
	require.NoError(e.t, err, name)
	require.Len(e.t, values, 1)
	return values[0].(*big.Int)
}

func (e *hyperpaxEnv) usdlBalance(holder common.Address) *big.Int {
	return e.erc20Uint("balanceOf", holder)
}

func (e *hyperpaxEnv) allowance(owner, spender common.Address) *big.Int {
	return e.erc20Uint("allowance", owner, spender)
}

func (e *hyperpaxEnv) bank(holder common.Address) *big.Int {
	return e.a.BankKeeper.GetBalance(e.ctx, e.a.EvmKeeper.GetPaxAddressOrDefault(e.ctx, holder), evmkeeper.BaseDenom).Amount.BigInt()
}

func (e *hyperpaxEnv) reserves(token common.Address) *big.Int {
	return e.uint(hyperpaxVault, "getReserves", token)
}

func (e *hyperpaxEnv) fees(rail, token common.Address) *big.Int {
	return e.uint(rail, "accumulatedFees", token)
}

func (e *hyperpaxEnv) balances() hyperpaxBalances {
	e.t.Helper()
	return hyperpaxBalances{
		UserUSDL:    e.usdlBalance(e.user),
		CustodyUSDL: e.usdlBalance(e.custody()),
		UserPAX:     e.bank(e.user),
		CustodyPAX:  e.a.BankKeeper.GetBalance(e.ctx, spotkeeper.CustodyAddress(), evmkeeper.BaseDenom).Amount.BigInt(),
		ReserveUSDL: e.reserves(e.usdl),
		ReservePAX:  e.reserves(e.pax),
	}
}

func (e *hyperpaxEnv) requireDelta(before hyperpaxBalances, delta hyperpaxDelta) {
	e.t.Helper()
	after := e.balances()
	add := func(base *big.Int, change int64) *big.Int { return new(big.Int).Add(base, big.NewInt(change)) }
	require.Equal(e.t,
		hyperpaxStrings(add(before.UserUSDL, delta.UserUSDL), add(before.CustodyUSDL, delta.CustodyUSDL),
			add(before.UserPAX, delta.UserPAX), add(before.CustodyPAX, delta.CustodyPAX),
			add(before.ReserveUSDL, delta.ReserveUSDL), add(before.ReservePAX, delta.ReservePAX)),
		hyperpaxStrings(after.UserUSDL, after.CustodyUSDL, after.UserPAX, after.CustodyPAX, after.ReserveUSDL, after.ReservePAX))
}

func (e *hyperpaxEnv) event(rail common.Address, name string, indexed []common.Hash, values ...interface{}) *ethtypes.Log {
	e.t.Helper()
	event, ok := e.set[rail].GetABI().Events[name]
	require.True(e.t, ok, name)
	data, err := event.Inputs.NonIndexed().Pack(values...)
	require.NoError(e.t, err, name)
	return hyperpaxStrip(&ethtypes.Log{Address: rail, Topics: append([]common.Hash{event.ID}, indexed...), Data: data})
}

func (e *hyperpaxEnv) transfer(from, to common.Address, amount int64) *ethtypes.Log {
	e.t.Helper()
	event := e.erc20.Events["Transfer"]
	require.Equal(e.t, hyperpaxTransferTopic, event.ID)
	data, err := event.Inputs.NonIndexed().Pack(big.NewInt(amount))
	require.NoError(e.t, err)
	return hyperpaxStrip(&ethtypes.Log{Address: e.usdl, Topics: []common.Hash{event.ID, hyperpaxTopic(from), hyperpaxTopic(to)}, Data: data})
}

// requireLogs compares the logs of the five rails and the USDL transfers
// of a receipt with want, in any order.
func (e *hyperpaxEnv) requireLogs(got []*ethtypes.Log, want ...*ethtypes.Log) {
	e.t.Helper()
	served := map[common.Address]bool{}
	for _, rail := range hyperpaxRails {
		served[rail] = true
	}
	kept := []*ethtypes.Log{}
	for _, log := range got {
		if served[log.Address] || log.Address == e.usdl && len(log.Topics) > 0 && log.Topics[0] == hyperpaxTransferTopic {
			kept = append(kept, hyperpaxStrip(log))
		}
	}
	if want == nil {
		want = []*ethtypes.Log{}
	}
	require.ElementsMatch(e.t, want, kept)
}

func (e *hyperpaxEnv) deadline(seconds int64) *big.Int {
	return big.NewInt(e.ctx.BlockTime().Unix() + seconds)
}

func (e *hyperpaxEnv) nextBlock() {
	e.ctx = e.ctx.WithBlockHeight(e.ctx.BlockHeight() + 1).WithBlockTime(e.ctx.BlockTime().Add(time.Second))
	e.a.EvmKeeper.BeginBlock(e.ctx)
	e.txIndex = 0
}

// endBlock runs the spot end-blocker and returns the logs of the spot
// synthetic receipt of the block.
func (e *hyperpaxEnv) endBlock() []*ethtypes.Log {
	e.t.Helper()
	require.NoError(e.t, spotmodule.EndBlocker(e.ctx, e.k))
	hash := evmkeeper.SyntheticReceiptHash(evmkeeper.SyntheticReceiptSpot, e.ctx.BlockHeight())
	index := evmkeeper.SyntheticReceiptProvisionalIndex(evmkeeper.SyntheticReceiptSpot)
	receipt, err := e.a.EvmKeeper.GetTransientReceipt(e.ctx, hash, uint64(index))
	require.NoError(e.t, err)
	require.Equal(e.t, uint32(ethtypes.ReceiptStatusSuccessful), receipt.Status)
	return evmkeeper.GetLogsForTx(receipt, 0)
}

func (e *hyperpaxEnv) quote(name string, args ...interface{}) spotkeeper.QuoteResult {
	e.t.Helper()
	values := e.call(hyperpaxQuoter, name, args...)
	require.Len(e.t, values, 1)
	return *abi.ConvertType(values[0], new(spotkeeper.QuoteResult)).(*spotkeeper.QuoteResult)
}

func hyperpaxQuoteFields(q spotkeeper.QuoteResult) []string {
	return hyperpaxStrings(q.AmountIn, q.AmountOut, q.GrossAmountOut, q.ExecutionPrice, q.SpotPriceIn, q.SpotPriceOut,
		q.FeeAmount, q.FeeBps, q.SufficientLiquidity, q.AvailableLiquidity, q.PriceTimestampIn, q.PriceTimestampOut,
		q.PriceStaleIn, q.PriceStaleOut, q.UtilizationCapBps, q.TradeUtilizationBps, q.CappedByUtilization, q.TwapOut,
		q.TwapDeviationBps, q.EffectiveFeeBps, q.Tier, q.PriceImpactBps, q.PriceImpactDeduction)
}

func hyperpaxMinOut(amountOut *big.Int) *big.Int {
	out := new(big.Int).Mul(amountOut, big.NewInt(10_000-hyperpaxSlippageBps))
	return out.Quo(out, big.NewInt(10_000))
}

func (e *hyperpaxEnv) limitOrder(id uint64) []string {
	e.t.Helper()
	values := e.call(hyperpaxOrders, "getLimitOrder", new(big.Int).SetUint64(id))
	require.Len(e.t, values, 1)
	o := *abi.ConvertType(values[0], new(hyperpaxLimitTuple)).(*hyperpaxLimitTuple)
	return hyperpaxStrings(o.Id, o.User, o.Stablecoin, o.Token, o.Amount, o.TargetPrice, o.OrderType, o.Status, o.CreatedAt, o.ExpiresAt)
}

func (e *hyperpaxEnv) stopLimitOrder(id uint64) []string {
	e.t.Helper()
	values := e.call(hyperpaxStops, "getStopLimitOrder", new(big.Int).SetUint64(id))
	require.Len(e.t, values, 1)
	o := *abi.ConvertType(values[0], new(stopLimitTuple)).(*stopLimitTuple)
	return hyperpaxStrings(o.Id, o.User, o.Stablecoin, o.Token, o.Amount, o.StopPrice, o.LimitPrice, o.OrderType, o.Status, o.CreatedAt, o.ExpiresAt)
}

func (e *hyperpaxEnv) stopLossOrder(id uint64) []string {
	e.t.Helper()
	values := e.call(hyperpaxStops, "getStopLossOrder", new(big.Int).SetUint64(id))
	require.Len(e.t, values, 1)
	o := *abi.ConvertType(values[0], new(stopLossTuple)).(*stopLossTuple)
	return hyperpaxStrings(o.Id, o.User, o.Stablecoin, o.Token, o.Amount, o.TriggerPrice, o.Status, o.CreatedAt, o.ExpiresAt)
}

func (e *hyperpaxEnv) ids(rail common.Address, name string) []string {
	e.t.Helper()
	values := e.call(rail, name, e.user)
	require.Len(e.t, values, 1)
	ids := values[0].([]*big.Int)
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// hyperpaxMarketPath replays quoteMarketBuy, the exact USDL approval to
// 0x1021 and marketBuy of useSpotOrders.marketBuy for 1,000 USDL of PAX.
func hyperpaxMarketPath(e *hyperpaxEnv) {
	t := e.t
	amount := big.NewInt(1_000e6)
	now := e.ctx.BlockTime().Unix()
	quote := e.quote("quoteMarketBuy", e.usdl, e.pax, amount)
	require.Equal(t, hyperpaxStrings(int64(1_000e6), int64(1_994e6), int64(2_000e6), int64(1_994_000), int64(1e18), int64(5e17),
		int64(6e6), int64(30), true, int64(1_000_000e6), now, now,
		false, false, int64(9000), int64(20), false, int64(0),
		int64(0), int64(30), int64(0), int64(0), int64(0)), hyperpaxQuoteFields(quote))
	minOut := hyperpaxMinOut(quote.AmountOut)
	require.Equal(t, "1984030000", minOut.String())

	allowanceBefore := e.allowance(e.user, hyperpaxPECOR)
	tx := e.exec(e.user, e.usdl, e.erc20Pack("approve", hyperpaxPECOR, amount))
	e.requireLogs(tx.logs)
	require.Equal(t, "1000000000", e.allowance(e.user, hyperpaxPECOR).String())
	require.Equal(t, "0", allowanceBefore.String())

	before := e.balances()
	feesBefore := e.fees(hyperpaxPECOR, e.pax)
	tx = e.exec(e.user, hyperpaxPECOR, e.pack(hyperpaxPECOR, "marketBuy", e.usdl, e.pax, amount, minOut, e.deadline(hyperpaxDeadline)))
	require.Equal(t, "1994000000", e.unpack(hyperpaxPECOR, "marketBuy", tx.ret)[0].(*big.Int).String())
	e.requireLogs(tx.logs,
		e.transfer(e.user, e.custody(), 1_000e6),
		e.event(hyperpaxPECOR, "TieredFeeApplied", []common.Hash{hyperpaxTopic(e.user), hyperpaxTopic(e.pax)}, hyperpaxUSD(1_000), big.NewInt(30), big.NewInt(6e6)),
		e.event(hyperpaxVault, "ReservesUpdated", []common.Hash{hyperpaxTopic(e.usdl)}, before.ReserveUSDL, new(big.Int).Add(before.ReserveUSDL, big.NewInt(1_000e6))),
		e.event(hyperpaxVault, "ReservesUpdated", []common.Hash{hyperpaxTopic(e.pax)}, before.ReservePAX, new(big.Int).Sub(before.ReservePAX, big.NewInt(1_994e6))),
		e.event(hyperpaxPECOR, "MarketOrderExecuted", []common.Hash{hyperpaxTopic(e.user), hyperpaxTopic(e.usdl), hyperpaxTopic(e.pax)}, big.NewInt(1_000e6), big.NewInt(1_994e6), true))
	e.requireDelta(before, hyperpaxDelta{UserUSDL: -1_000e6, CustodyUSDL: 1_000e6, UserPAX: 1_994e6, CustodyPAX: -1_994e6, ReserveUSDL: 1_000e6, ReservePAX: -1_994e6})
	require.Equal(t, "0", e.allowance(e.user, hyperpaxPECOR).String())
	require.Equal(t, new(big.Int).Add(feesBefore, big.NewInt(6e6)).String(), e.fees(hyperpaxPECOR, e.pax).String())
}

// hyperpaxPaxPath replays quoteMarketSell and marketSell of 1,000 PAX from
// the native balance, with no wrap and no approval.
func hyperpaxPaxPath(e *hyperpaxEnv) {
	t := e.t
	amount := big.NewInt(1_000e6)
	quote := e.quote("quoteMarketSell", e.pax, e.usdl, amount)
	require.Equal(t, hyperpaxStrings(int64(1_000e6), int64(498_500_000), int64(500e6), int64(5e17), int64(1e18), int64(1_500_000), int64(30), true, false, false),
		hyperpaxStrings(quote.AmountIn, quote.AmountOut, quote.GrossAmountOut, quote.SpotPriceIn, quote.SpotPriceOut, quote.FeeAmount, quote.FeeBps,
			quote.SufficientLiquidity, quote.PriceStaleIn, quote.PriceStaleOut))
	minOut := hyperpaxMinOut(quote.AmountOut)
	require.Equal(t, "496007500", minOut.String())

	before := e.balances()
	feesBefore := e.fees(hyperpaxPECOR, e.usdl)
	tx := e.exec(e.user, hyperpaxPECOR, e.pack(hyperpaxPECOR, "marketSell", e.pax, e.usdl, amount, minOut, e.deadline(hyperpaxDeadline)))
	require.Equal(t, "498500000", e.unpack(hyperpaxPECOR, "marketSell", tx.ret)[0].(*big.Int).String())
	e.requireLogs(tx.logs,
		e.event(hyperpaxPECOR, "TieredFeeApplied", []common.Hash{hyperpaxTopic(e.user), hyperpaxTopic(e.usdl)}, hyperpaxUSD(500), big.NewInt(30), big.NewInt(1_500_000)),
		e.event(hyperpaxVault, "ReservesUpdated", []common.Hash{hyperpaxTopic(e.pax)}, before.ReservePAX, new(big.Int).Add(before.ReservePAX, big.NewInt(1_000e6))),
		e.event(hyperpaxVault, "ReservesUpdated", []common.Hash{hyperpaxTopic(e.usdl)}, before.ReserveUSDL, new(big.Int).Sub(before.ReserveUSDL, big.NewInt(498_500_000))),
		e.transfer(e.custody(), e.user, 498_500_000),
		e.event(hyperpaxPECOR, "MarketOrderExecuted", []common.Hash{hyperpaxTopic(e.user), hyperpaxTopic(e.usdl), hyperpaxTopic(e.pax)}, big.NewInt(498_500_000), big.NewInt(1_000e6), false))
	e.requireDelta(before, hyperpaxDelta{UserUSDL: 498_500_000, CustodyUSDL: -498_500_000, UserPAX: -1_000e6, CustodyPAX: 1_000e6, ReserveUSDL: -498_500_000, ReservePAX: 1_000e6})
	require.Equal(t, new(big.Int).Add(feesBefore, big.NewInt(1_500_000)).String(), e.fees(hyperpaxPECOR, e.usdl).String())
}

// hyperpaxLimitPath replays useSpotOrders.placeLimitBuy, modifyLimitOrder and
// cancelLimitOrder; the order takes id.
func hyperpaxLimitPath(e *hyperpaxEnv, id uint64) {
	t := e.t
	amount := big.NewInt(100e6)
	target := big.NewInt(5e17)
	expiry := e.deadline(hyperpaxOrderExpiry)
	now := e.ctx.BlockTime().Unix()

	e.requireLogs(e.exec(e.user, e.usdl, e.erc20Pack("approve", hyperpaxOrders, amount)).logs)
	require.Equal(t, "100000000", e.allowance(e.user, hyperpaxOrders).String())
	before := e.balances()
	tx := e.exec(e.user, hyperpaxOrders, e.pack(hyperpaxOrders, "placeLimitBuy", e.usdl, e.pax, amount, target, expiry))
	require.Equal(t, fmt.Sprint(id), e.unpack(hyperpaxOrders, "placeLimitBuy", tx.ret)[0].(*big.Int).String())
	e.requireLogs(tx.logs,
		e.transfer(e.user, e.custody(), 100e6),
		e.event(hyperpaxOrders, "LimitOrderCreated", []common.Hash{hyperpaxID(id), hyperpaxTopic(e.user)}, uint8(spottypes.OrderTypeLimitBuy), amount, target))
	e.requireDelta(before, hyperpaxDelta{UserUSDL: -100e6, CustodyUSDL: 100e6})
	require.Equal(t, "0", e.allowance(e.user, hyperpaxOrders).String())
	require.Equal(t, hyperpaxStrings(id, e.user, e.usdl, e.pax, amount, target, uint8(spottypes.OrderTypeLimitBuy), uint8(spottypes.OrderStatusPending), now, expiry), e.limitOrder(id))
	require.Equal(t, []string{fmt.Sprint(id)}, e.ids(hyperpaxOrders, "getUserLimitOrders"))

	modified := big.NewInt(45e16)
	tx = e.exec(e.user, hyperpaxOrders, e.pack(hyperpaxOrders, "modifyLimitOrder", new(big.Int).SetUint64(id), modified, expiry))
	e.requireLogs(tx.logs, e.event(hyperpaxOrders, "LimitOrderModified", []common.Hash{hyperpaxID(id)}, modified, expiry))
	e.requireDelta(before, hyperpaxDelta{UserUSDL: -100e6, CustodyUSDL: 100e6})
	require.Equal(t, hyperpaxStrings(id, e.user, e.usdl, e.pax, amount, modified, uint8(spottypes.OrderTypeLimitBuy), uint8(spottypes.OrderStatusPending), now, expiry), e.limitOrder(id))

	tx = e.exec(e.user, hyperpaxOrders, e.pack(hyperpaxOrders, "cancelLimitOrder", new(big.Int).SetUint64(id)))
	e.requireLogs(tx.logs,
		e.transfer(e.custody(), e.user, 100e6),
		e.event(hyperpaxOrders, "LimitOrderCancelled", []common.Hash{hyperpaxID(id)}))
	e.requireDelta(before, hyperpaxDelta{})
	require.Equal(t, hyperpaxStrings(id, e.user, e.usdl, e.pax, amount, modified, uint8(spottypes.OrderTypeLimitBuy), uint8(spottypes.OrderStatusCancelled), now, expiry), e.limitOrder(id))
}

// hyperpaxStopPath replays useSpotOrders.placeStopLimitBuy and placeStopLoss,
// moves the PAX index to the trigger, lets EndBlock fill the stop-loss and
// cancels the stop-limit; the orders take first and first+1.
func hyperpaxStopPath(e *hyperpaxEnv, first uint64) {
	t := e.t
	stopLimit, stopLoss := first, first+1
	amount := big.NewInt(100e6)
	tokens := big.NewInt(1_000e6)
	stopPrice, limitPrice, trigger := big.NewInt(6e17), big.NewInt(65e16), big.NewInt(4e17)
	expiry := e.deadline(hyperpaxOrderExpiry)
	now := e.ctx.BlockTime().Unix()

	e.requireLogs(e.exec(e.user, e.usdl, e.erc20Pack("approve", hyperpaxStops, amount)).logs)
	before := e.balances()
	tx := e.exec(e.user, hyperpaxStops, e.pack(hyperpaxStops, "placeStopLimitBuy", e.usdl, e.pax, amount, stopPrice, limitPrice, expiry))
	require.Equal(t, fmt.Sprint(stopLimit), e.unpack(hyperpaxStops, "placeStopLimitBuy", tx.ret)[0].(*big.Int).String())
	e.requireLogs(tx.logs,
		e.transfer(e.user, e.custody(), 100e6),
		e.event(hyperpaxStops, "StopLimitOrderCreated", []common.Hash{hyperpaxID(stopLimit), hyperpaxTopic(e.user)}, uint8(spottypes.OrderTypeStopLimitBuy), amount, stopPrice, limitPrice))
	e.requireDelta(before, hyperpaxDelta{UserUSDL: -100e6, CustodyUSDL: 100e6})
	require.Equal(t, "0", e.allowance(e.user, hyperpaxStops).String())
	stopLimitRecord := func(status spottypes.OrderStatus) []string {
		return hyperpaxStrings(stopLimit, e.user, e.usdl, e.pax, amount, stopPrice, limitPrice, uint8(spottypes.OrderTypeStopLimitBuy), uint8(status), now, expiry)
	}
	require.Equal(t, stopLimitRecord(spottypes.OrderStatusPending), e.stopLimitOrder(stopLimit))

	before = e.balances()
	tx = e.exec(e.user, hyperpaxStops, e.pack(hyperpaxStops, "placeStopLoss", e.pax, e.usdl, tokens, trigger, expiry))
	require.Equal(t, fmt.Sprint(stopLoss), e.unpack(hyperpaxStops, "placeStopLoss", tx.ret)[0].(*big.Int).String())
	e.requireLogs(tx.logs,
		e.event(hyperpaxStops, "StopLossOrderCreated", []common.Hash{hyperpaxID(stopLoss), hyperpaxTopic(e.user)}, tokens, trigger))
	e.requireDelta(before, hyperpaxDelta{UserPAX: -1_000e6, CustodyPAX: 1_000e6})
	stopLossRecord := func(status spottypes.OrderStatus) []string {
		return hyperpaxStrings(stopLoss, e.user, e.usdl, e.pax, tokens, trigger, uint8(status), now, expiry)
	}
	require.Equal(t, stopLossRecord(spottypes.OrderStatusPending), e.stopLossOrder(stopLoss))
	require.Equal(t, []string{fmt.Sprint(stopLimit)}, e.ids(hyperpaxStops, "getUserStopLimitOrders"))
	require.Equal(t, []string{fmt.Sprint(stopLoss)}, e.ids(hyperpaxStops, "getUserStopLossOrders"))
	require.Equal(t, "1", e.uint(hyperpaxStops, "getActiveStopLossOrderCount").String())
	require.Equal(t, "1", e.uint(hyperpaxStops, "getActiveStopLimitOrderCount").String())

	e.nextBlock()
	e.price(spottypes.PAXMarketID, 4e17)
	e.price(spottypes.SIDMarketID, 2e18)
	before = e.balances()
	feesBefore := e.fees(hyperpaxStops, e.usdl)
	e.requireLogs(e.endBlock(),
		e.event(hyperpaxVault, "ReservesUpdated", []common.Hash{hyperpaxTopic(e.pax)}, before.ReservePAX, new(big.Int).Add(before.ReservePAX, big.NewInt(1_000e6))),
		e.event(hyperpaxVault, "ReservesUpdated", []common.Hash{hyperpaxTopic(e.usdl)}, before.ReserveUSDL, new(big.Int).Sub(before.ReserveUSDL, big.NewInt(399_600_000))),
		e.event(hyperpaxStops, "StopLossOrderExecuted", []common.Hash{hyperpaxID(stopLoss)}, trigger),
		e.transfer(e.custody(), e.user, 399_600_000))
	e.requireDelta(before, hyperpaxDelta{UserUSDL: 399_600_000, CustodyUSDL: -399_600_000, ReserveUSDL: -399_600_000, ReservePAX: 1_000e6})
	require.Equal(t, stopLossRecord(spottypes.OrderStatusExecuted), e.stopLossOrder(stopLoss))
	require.Equal(t, stopLimitRecord(spottypes.OrderStatusPending), e.stopLimitOrder(stopLimit))
	require.Equal(t, new(big.Int).Add(feesBefore, big.NewInt(400_000)).String(), e.fees(hyperpaxStops, e.usdl).String())
	require.Equal(t, "0", e.uint(hyperpaxStops, "getActiveStopLossOrderCount").String())

	before = e.balances()
	tx = e.exec(e.user, hyperpaxStops, e.pack(hyperpaxStops, "cancelStopLimitOrder", new(big.Int).SetUint64(stopLimit)))
	e.requireLogs(tx.logs,
		e.transfer(e.custody(), e.user, 100e6),
		e.event(hyperpaxStops, "StopLimitCancelled", []common.Hash{hyperpaxID(stopLimit)}))
	e.requireDelta(before, hyperpaxDelta{UserUSDL: 100e6, CustodyUSDL: -100e6})
	require.Equal(t, stopLimitRecord(spottypes.OrderStatusCancelled), e.stopLimitOrder(stopLimit))
	require.Equal(t, "0", e.uint(hyperpaxStops, "getActiveStopLimitOrderCount").String())
}

// hyperpaxCalls are the rail methods the Hyperpax spot hooks call, with the
// selector and mutability the application encodes them with.
var hyperpaxCalls = []struct {
	rail       common.Address
	name       string
	selector   string
	mutability string
}{
	{hyperpaxPECOR, "marketBuy", "77a61952", "nonpayable"},
	{hyperpaxPECOR, "marketSell", "57f24201", "nonpayable"},
	{hyperpaxPECOR, "vault", "fbfa77cf", "view"},
	{hyperpaxPECOR, "accumulatedFees", "fcf66664", "view"},
	{hyperpaxQuoter, "quoteMarketBuy", "6f6ac6a2", "view"},
	{hyperpaxQuoter, "quoteMarketSell", "5323412e", "view"},
	{hyperpaxQuoter, "vault", "fbfa77cf", "view"},
	{hyperpaxOrders, "placeLimitBuy", "947de5b1", "nonpayable"},
	{hyperpaxOrders, "getUserLimitOrders", "dd5d02e6", "view"},
	{hyperpaxOrders, "getLimitOrder", "53061026", "view"},
	{hyperpaxOrders, "modifyLimitOrder", "3f08abf2", "nonpayable"},
	{hyperpaxOrders, "cancelLimitOrder", "a5cdc8fc", "nonpayable"},
	{hyperpaxOrders, "vault", "fbfa77cf", "view"},
	{hyperpaxOrders, "accumulatedFees", "fcf66664", "view"},
	{hyperpaxStops, "placeStopLimitBuy", "afbf4a9b", "nonpayable"},
	{hyperpaxStops, "placeStopLoss", "695970ef", "nonpayable"},
	{hyperpaxStops, "cancelStopLimitOrder", "1f407810", "nonpayable"},
	{hyperpaxStops, "getStopLimitOrder", "307ff324", "view"},
	{hyperpaxStops, "getStopLossOrder", "4c34cf02", "view"},
	{hyperpaxStops, "getUserStopLimitOrders", "e93c4673", "view"},
	{hyperpaxStops, "getUserStopLossOrders", "21297b41", "view"},
	{hyperpaxStops, "getActiveStopLossOrderCount", "7a5c00ac", "view"},
	{hyperpaxStops, "getActiveStopLimitOrderCount", "61bc56a0", "view"},
	{hyperpaxStops, "vault", "fbfa77cf", "view"},
	{hyperpaxStops, "accumulatedFees", "fcf66664", "view"},
	{hyperpaxVault, "deposit", "47e7ef24", "nonpayable"},
	{hyperpaxVault, "getReserves", "3e99c1e4", "view"},
}

func TestHyperpaxCallSequence(t *testing.T) {
	e := newHyperpaxEnv(t)

	addresses := make([]common.Address, 0, len(e.set))
	for address := range e.set {
		addresses = append(addresses, address)
	}
	require.ElementsMatch(t, hyperpaxRails, addresses)
	served := e.precompiles(e.ctx)
	for _, rail := range hyperpaxRails {
		require.Same(t, e.set[rail], served[rail])
	}
	for _, rail := range []common.Address{hyperpaxPECOR, hyperpaxOrders, hyperpaxStops, hyperpaxQuoter} {
		values := e.call(rail, "vault")
		require.Equal(t, []interface{}{hyperpaxVault}, values)
	}

	for _, entry := range hyperpaxCalls {
		method, ok := e.set[entry.rail].GetABI().Methods[entry.name]
		require.True(t, ok, entry.name)
		require.Equal(t, entry.selector, hex.EncodeToString(method.ID), entry.name)
		require.Equal(t, entry.mutability, method.StateMutability, entry.name)
	}

	buy := e.pack(hyperpaxPECOR, "marketBuy", e.usdl, e.pax, big.NewInt(1_000e6), big.NewInt(1_984_030_000), e.deadline(hyperpaxDeadline))
	_, err := e.static(hyperpaxPECOR, buy)
	require.Error(t, err)

	before := e.balances()
	for _, address := range hyperpaxUnmapped {
		_, ok := e.set[address]
		require.False(t, ok)
		_, ok = served[address]
		require.False(t, ok)
		require.Empty(t, e.a.EvmKeeper.GetCode(e.ctx, address))
		tx := e.exec(e.user, address, buy)
		require.Empty(t, tx.ret)
		require.Empty(t, tx.logs)
	}
	e.requireDelta(before, hyperpaxDelta{})

	refused := e.send(e.user, hyperpaxPECOR, big.NewInt(1), buy)
	require.Equal(t, uint32(ethtypes.ReceiptStatusFailed), refused.receipt.Status)
	require.Empty(t, refused.logs)
	e.requireDelta(before, hyperpaxDelta{})

	hyperpaxMarketPath(e)
	hyperpaxPaxPath(e)
	hyperpaxLimitPath(e, 1)
	hyperpaxStopPath(e, 2)
}

func TestHyperpaxMarketPath(t *testing.T) { hyperpaxMarketPath(newHyperpaxEnv(t)) }

func TestHyperpaxLimitPath(t *testing.T) { hyperpaxLimitPath(newHyperpaxEnv(t), 1) }

func TestHyperpaxStopPath(t *testing.T) { hyperpaxStopPath(newHyperpaxEnv(t), 1) }

func TestHyperpaxPaxPath(t *testing.T) { hyperpaxPaxPath(newHyperpaxEnv(t)) }
