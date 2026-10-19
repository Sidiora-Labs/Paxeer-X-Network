package launchpad_test

import (
	"bytes"
	"encoding/json"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/launchpad"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
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
	usdlSupply int64  = 30_000_000_000_000
	callGas    uint64 = 30_000_000
)

var (
	genesisTime = time.Unix(1_800_000_000, 0).UTC()
	deadline    = big.NewInt(genesisTime.Add(10_000 * time.Hour).Unix())
	precompile  = common.HexToAddress(types.LaunchpadAddress)

	factory        = launchpadkeeper.Generation2Surfaces[types.RoleFactory]
	router         = launchpadkeeper.Generation2Surfaces[types.RoleRouter]
	quoter         = launchpadkeeper.Generation2Surfaces[types.RoleQuoter]
	feesRouter     = launchpadkeeper.Generation2Surfaces[types.RoleFeesRouter]
	feeAccumulator = launchpadkeeper.Generation2Surfaces[types.RoleFeeAccumulator]
	poolRegistry   = launchpadkeeper.Generation2Surfaces[types.RolePoolRegistry]
	protocolConfig = launchpadkeeper.Generation2Surfaces[types.RoleProtocolConfig]
	feeRights      = launchpadkeeper.Generation2Surfaces[types.RoleFeeRights]
	treasury       = launchpadkeeper.Generation2Surfaces[types.RoleTreasury]

	poolRole = crypto.Keccak256Hash([]byte("POOL_ROLE"))
)

const (
	roleERC20    = "SidioraERC20"
	roleTreasury = "Treasury"
	directRole   = "Launchpad"
)

var (
	abiOnce sync.Once
	abis    map[string]abi.ABI
	abiErr  error
)

// roleABI is the ABI of role read from abi/<role>.json, or abi.json for the
// direct surface of 0x1017.
func roleABI(t *testing.T, role string) abi.ABI {
	t.Helper()
	abiOnce.Do(func() {
		abis = map[string]abi.ABI{}
		files, err := filepath.Glob(filepath.Join("abi", "*.json"))
		if err != nil {
			abiErr = err
			return
		}
		for _, file := range append(files, "abi.json") {
			raw, err := os.ReadFile(file)
			if err != nil {
				abiErr = err
				return
			}
			parsed, err := abi.JSON(bytes.NewReader(raw))
			if err != nil {
				abiErr = err
				return
			}
			name := strings.TrimSuffix(filepath.Base(file), ".json")
			if file == "abi.json" {
				name = directRole
			}
			abis[name] = parsed
		}
	})
	require.NoError(t, abiErr)
	parsed, ok := abis[role]
	require.True(t, ok, role)
	return parsed
}

func method(t *testing.T, role, name string) abi.Method {
	t.Helper()
	m, ok := roleABI(t, role).Methods[name]
	require.True(t, ok, "%s.%s", role, name)
	return m
}

func pack(t *testing.T, role, name string, args ...interface{}) []byte {
	t.Helper()
	m := method(t, role, name)
	packed, err := m.Inputs.Pack(args...)
	require.NoError(t, err, "%s.%s", role, name)
	return append(append([]byte(nil), m.ID...), packed...)
}

// errorData is the revert data of the custom error signature followed by the
// 32-byte words given.
func errorData(signature string, words ...[]byte) []byte {
	data := append([]byte(nil), crypto.Keccak256([]byte(signature))[:4]...)
	for _, w := range words {
		data = append(data, w...)
	}
	return data
}

func addressTopic(a common.Address) common.Hash { return common.BytesToHash(a.Bytes()) }

// same compares by JSON encoding, so zero big.Int values compare equal
// however they were built.
func same(t *testing.T, want, got interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	wantJSON, err := json.Marshal(want)
	require.NoError(t, err)
	gotJSON, err := json.Marshal(got)
	require.NoError(t, err)
	require.JSONEq(t, string(wantJSON), string(gotJSON), msgAndArgs...)
}

type keepers struct {
	utils.Keepers
	launchpad *launchpadkeeper.Keeper
}

func (k keepers) LaunchpadK() *launchpadkeeper.Keeper { return k.launchpad }

// harness is the launchpad keeper over the test application with an EVM
// whose 0x1017 is the precompile under test.
type harness struct {
	t          *testing.T
	stateDB    *state.DBImpl
	evm        *vm.EVM
	precompile vm.PrecompiledContract
	keeper     *launchpadkeeper.Keeper
	caller     common.Address
	usdl       common.Address
}

// newEnv builds the harness at blockTime and, when height is positive, at
// that height, with the default launchpad genesis and no surfaces.
func newEnv(t *testing.T, blockTime time.Time, height int64) *harness {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockTime(blockTime)
	if height > 0 {
		ctx = ctx.WithBlockHeight(height)
	}
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	k, ctx := launchpadtest.NewKeeper(app, ctx)
	k.InitGenesis(ctx, *types.DefaultGenesis())
	h := &harness{t: t, keeper: k}
	p, err := launchpad.NewPrecompile(keepers{Keepers: app.GetPrecompileKeepers(), launchpad: k})
	require.NoError(t, err)
	h.precompile = p
	h.stateDB = state.NewDBImpl(ctx, &app.EvmKeeper, false)
	blockCtx, err := app.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
	require.NoError(t, err)
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(app.EvmKeeper.ChainID(ctx))
	precompiles := map[common.Address]vm.PrecompiledContract{}
	for address, contract := range app.EvmKeeper.CustomPrecompiles(ctx) {
		precompiles[address] = contract
	}
	precompiles[precompile] = h.precompile
	h.evm = vm.NewEVM(*blockCtx, h.stateDB, cfg, vm.Config{}, precompiles)
	h.caller = h.account()
	return h
}

// newHarness is newEnv at genesisTime with USDL deployed in the test EVM,
// its supply held by h.caller and named by Params.QuoteToken, and the
// generation 2 role surfaces installed.
func newHarness(t *testing.T) *harness {
	t.Helper()
	h := newEnv(t, genesisTime, 0)
	h.usdl = h.deployERC20("USD Ledger", "USDL", big.NewInt(usdlSupply), h.caller)
	params := h.keeper.GetParams(h.ctx())
	params.QuoteToken = h.usdl.Hex()
	require.NoError(t, h.keeper.UpdateParams(h.ctx(), h.keeper.Authority(), params))
	require.NoError(t, launchpad.InstallRoleSurfaces(h.ctx(), &testkeeper.EVMTestApp.EvmKeeper, h.keeper))
	require.Equal(t, usdlSupply, h.balanceOf(h.usdl, h.caller).Int64())
	return h
}

func (h *harness) ctx() sdk.Context { return h.stateDB.Ctx() }

// account is a fresh EVM address associated with its Pax account.
func (h *harness) account() common.Address {
	acc, address := testkeeper.MockAddressPair()
	testkeeper.EVMTestApp.EvmKeeper.SetAddressMapping(h.ctx(), acc, address)
	return address
}

// deployERC20 deploys SidioraERC20(name, symbol, supply, holder) from a
// fresh deployer.
func (h *harness) deployERC20(name, symbol string, supply *big.Int, holder common.Address) common.Address {
	h.t.Helper()
	ret, address, _, err := h.evm.Create(h.account(), erc20InitCode(h.t, name, symbol, supply, holder), callGas,
		uint256.NewInt(0))
	require.NoError(h.t, err, "%x", ret)
	require.NotEmpty(h.t, h.stateDB.GetCode(address))
	return address
}

func erc20InitCode(t *testing.T, name, symbol string, supply *big.Int, holder common.Address) []byte {
	t.Helper()
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(t, err)
	args, err := abi.Arguments{{Type: stringType}, {Type: stringType}, {Type: uintType}, {Type: addressType}}.
		Pack(name, symbol, supply, holder)
	require.NoError(t, err)
	return append(common.FromHex(sidioraERC20Creation), args...)
}

// send is an EVM call of input from from to to.
func (h *harness) send(from, to common.Address, input []byte) ([]byte, error) {
	ret, _, err := h.evm.Call(from, to, input, callGas, uint256.NewInt(0))
	return ret, err
}

// do calls role method name at to from from, requires success and returns
// the unpacked outputs.
func (h *harness) do(from, to common.Address, role, name string, args ...interface{}) []interface{} {
	h.t.Helper()
	ret, err := h.send(from, to, pack(h.t, role, name, args...))
	require.NoError(h.t, err, "%s.%s at %s: %x", role, name, to.Hex(), ret)
	out, err := method(h.t, role, name).Outputs.Unpack(ret)
	require.NoError(h.t, err, "%s.%s", role, name)
	return out
}

// view is do as a static call from h.caller.
func (h *harness) view(to common.Address, role, name string, args ...interface{}) []interface{} {
	h.t.Helper()
	ret, _, err := h.evm.StaticCall(h.caller, to, pack(h.t, role, name, args...), callGas)
	require.NoError(h.t, err, "%s.%s at %s: %x", role, name, to.Hex(), ret)
	out, err := method(h.t, role, name).Outputs.Unpack(ret)
	require.NoError(h.t, err, "%s.%s", role, name)
	return out
}

// fails calls role method name at to from from, requires a revert and
// returns its revert data.
func (h *harness) fails(from, to common.Address, role, name string, args ...interface{}) []byte {
	h.t.Helper()
	ret, err := h.send(from, to, pack(h.t, role, name, args...))
	require.ErrorIs(h.t, err, vm.ErrExecutionReverted, "%s.%s at %s", role, name, to.Hex())
	return ret
}

func (h *harness) balanceOf(token, holder common.Address) *big.Int {
	h.t.Helper()
	return h.view(token, roleERC20, "balanceOf", holder)[0].(*big.Int)
}

// approve grants amount of token to 0x1017.
func (h *harness) approve(token, owner common.Address, amount *big.Int) {
	h.t.Helper()
	require.Equal(h.t, true, h.do(owner, token, roleERC20, "approve", precompile, amount)[0])
}

func (h *harness) transfer(token, from, to common.Address, amount *big.Int) {
	h.t.Helper()
	require.Equal(h.t, true, h.do(from, token, roleERC20, "transfer", to, amount)[0])
}

func (h *harness) ledger(ledger launchpadkeeper.QuoteLedger) *big.Int {
	h.t.Helper()
	amount, err := h.keeper.GetQuoteLedger(h.ctx(), ledger)
	require.NoError(h.t, err)
	return amount.BigInt()
}

func (h *harness) custody() common.Address { return h.keeper.ModuleEVMAddress(h.ctx()) }

func (h *harness) creationFee() *big.Int { return h.keeper.GetParams(h.ctx()).CreationFee.BigInt() }

// createMarket approves the creation fee to 0x1017 and creates a market
// from creator through the Router surface.
func (h *harness) createMarket(creator common.Address, name, symbol string, strategy types.FeeStrategy,
	optical common.Address) (token, pool common.Address, nftID *big.Int) {
	h.t.Helper()
	h.approve(h.usdl, creator, h.creationFee())
	out := h.do(creator, router, types.RoleRouter, "createMarket", name, symbol, uint8(strategy), optical)
	return out[0].(common.Address), out[1].(common.Address), out[2].(*big.Int)
}

// market is the keeper record of the market of pool.
func (h *harness) market(pool common.Address) types.Market {
	h.t.Helper()
	m, found := h.keeper.GetMarket(h.ctx(), types.ModuleName+"/"+pool.Hex())
	require.True(h.t, found, pool.Hex())
	return m
}

// logs are the logs emitted at address with the topic of signature after the
// first from logs of the run.
func (h *harness) logs(from int, address common.Address, signature string) []*ethtypes.Log {
	var out []*ethtypes.Log
	topic := crypto.Keccak256Hash([]byte(signature))
	for _, log := range h.stateDB.GetAllLogs()[from:] {
		if log.Address == address && len(log.Topics) > 0 && log.Topics[0] == topic {
			out = append(out, log)
		}
	}
	return out
}

func (h *harness) logCount() int { return len(h.stateDB.GetAllLogs()) }

// eventData unpacks the non-indexed fields of event name of role.
func eventData(t *testing.T, role, name string, log *ethtypes.Log) []interface{} {
	t.Helper()
	event, ok := roleABI(t, role).Events[name]
	require.True(t, ok, "%s event %s", role, name)
	require.Equal(t, event.ID, log.Topics[0], "%s event %s", role, name)
	out, err := event.Inputs.NonIndexed().Unpack(log.Data)
	require.NoError(t, err, "%s event %s", role, name)
	return out
}

// zeroArgs is the zero value of every input of m.
func zeroArgs(m abi.Method) []interface{} {
	args := make([]interface{}, len(m.Inputs))
	for i, input := range m.Inputs {
		args[i] = zeroValue(input.Type.GetType()).Interface()
	}
	return args
}

// zeroValue is the packable zero of an ABI Go type: a *big.Int is a zero
// integer rather than nil, inside tuples and arrays as well.
func zeroValue(typ reflect.Type) reflect.Value {
	if typ == reflect.TypeOf((*big.Int)(nil)) {
		return reflect.ValueOf(new(big.Int))
	}
	value := reflect.New(typ).Elem()
	switch typ.Kind() {
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			value.Field(i).Set(zeroValue(typ.Field(i).Type))
		}
	case reflect.Array:
		for i := 0; i < typ.Len(); i++ {
			value.Index(i).Set(zeroValue(typ.Elem()))
		}
	}
	return value
}

// direct runs input on the precompile as a direct call of from with the call
// kind given.
func (h *harness) direct(from common.Address, input []byte, value *big.Int, readOnly, delegate bool) ([]byte, error) {
	return h.precompile.Run(h.evm, from, from, input, value, readOnly, delegate, nil)
}

func revertReason(t *testing.T, data []byte) string {
	t.Helper()
	reason, err := abi.UnpackRevert(data)
	require.NoError(t, err, "%x", data)
	return reason
}

func quoteResult(t *testing.T, quoted interface{}) (amountOut, fee *big.Int) {
	t.Helper()
	v := reflect.ValueOf(quoted)
	return v.FieldByName("AmountOut").Interface().(*big.Int), v.FieldByName("FeeAmount").Interface().(*big.Int)
}

func TestNewPrecompileNeedsTheLaunchpadKeeper(t *testing.T) {
	_, err := launchpad.NewPrecompile(&utils.EmptyKeepers{})
	require.Error(t, err)
	_, err = launchpad.NewPrecompile(keepers{Keepers: testkeeper.EVMTestApp.GetPrecompileKeepers(), launchpad: nil})
	require.Error(t, err)
}

func TestCreateMarketAndTradeThroughThePrecompile(t *testing.T) {
	h := newHarness(t)
	fee := h.creationFee()
	require.Equal(t, big.NewInt(1_000_000), fee)
	start := h.logCount()
	token, pool, nftID := h.createMarket(h.caller, "Kindle Token", "KNDL", types.FeeStrategyClaim, common.Address{})

	require.Equal(t, new(big.Int).Sub(big.NewInt(usdlSupply), fee), h.balanceOf(h.usdl, h.caller))
	require.Equal(t, fee, h.balanceOf(h.usdl, h.custody()))
	require.Equal(t, fee, h.ledger(launchpadkeeper.TreasuryHeldLedger()))
	created := h.logs(start, precompile, "MarketCreated(bytes32,address,address,address,address,uint256,uint256)")
	require.Len(t, created, 1)
	require.Equal(t, []common.Hash{created[0].Topics[0], addressTopic(pool), addressTopic(token), addressTopic(h.caller)},
		created[0].Topics)
	same(t, []interface{}{pool, common.Address{}, genesisTime.Unix(), h.ctx().BlockHeight()},
		eventData(t, directRole, "MarketCreated", created[0]))

	market := h.market(pool)
	require.Equal(t, token.Hex(), market.TokenAddress)
	require.Equal(t, "Kindle Token", market.Name)
	require.Equal(t, "KNDL", market.Symbol)
	require.Equal(t, types.Generation2, market.Generation)
	require.Equal(t, nftID.Uint64(), market.FeeRightID)

	virtual, supply := big.NewInt(10_000_000_000), big.NewInt(1_000_000_000_000_000)
	same(t, []interface{}{virtual, 0, supply}, h.view(pool, types.RolePool, "getReserves"))
	require.Equal(t, token, h.view(pool, types.RolePool, "tokenAddress")[0])
	require.Equal(t, h.usdl, h.view(pool, types.RolePool, "usdlAddress")[0])
	require.Equal(t, factory, h.view(pool, types.RolePool, "factory")[0])
	require.Equal(t, h.caller, h.view(pool, types.RolePool, "guardian")[0])
	require.Equal(t, big.NewInt(10_000_000_000_000), h.view(pool, types.RolePool, "getPrice")[0])
	require.Equal(t, big.NewInt(300), h.view(pool, types.RolePool, "currentFeeBps")[0])
	require.Equal(t, supply, h.balanceOf(token, pool))
	require.Equal(t, big.NewInt(1), h.view(poolRegistry, types.RolePoolRegistry, "getPoolCount")[0])
	require.Equal(t, []common.Address{pool},
		h.view(quoter, types.RoleQuoter, "getAllPools", big.NewInt(0), big.NewInt(10))[0])

	in := big.NewInt(100_000_000)
	bought, buyFee := big.NewInt(9_606_813_905_120), big.NewInt(3_000_000)
	out, quotedFee := quoteResult(t, h.view(quoter, types.RoleQuoter, "quoteExactInput", pool, in, true)[0])
	require.Equal(t, bought, out)
	require.Equal(t, buyFee, quotedFee)

	h.approve(h.usdl, h.caller, in)
	start = h.logCount()
	require.Equal(t, bought, h.do(h.caller, router, types.RoleRouter, "buy", pool, in, bought, deadline)[0])
	require.Equal(t, bought, h.balanceOf(token, h.caller))
	require.Equal(t, new(big.Int).Sub(supply, bought), h.balanceOf(token, pool))
	require.Equal(t, new(big.Int).Add(fee, in), h.balanceOf(h.usdl, h.custody()))
	require.Equal(t, big.NewInt(97_000_000), h.ledger(launchpadkeeper.HeldQuoteLedger(pool)))
	protocolCut := big.NewInt(300_000)
	poolCut := new(big.Int).Sub(buyFee, protocolCut)
	require.Equal(t, poolCut, h.ledger(launchpadkeeper.FeeHeldLedger(feeAccumulator)))
	require.Equal(t, new(big.Int).Add(fee, protocolCut), h.ledger(launchpadkeeper.TreasuryHeldLedger()))
	price := big.NewInt(10_194_940_899_999)
	same(t, []interface{}{virtual, 97_000_000, new(big.Int).Sub(supply, bought)},
		h.view(pool, types.RolePool, "getReserves"))
	require.Equal(t, price, h.view(pool, types.RolePool, "getPrice")[0])
	require.Equal(t, in, h.view(pool, types.RolePool, "cumulativeVolume")[0])
	require.Equal(t, in, h.view(pool, types.RolePool, "cumulativeBuyUsdlVolume")[0])
	require.Equal(t, buyFee, h.view(pool, types.RolePool, "accumulatedUsdlFees")[0])
	swaps := h.logs(start, precompile, "Swap(bytes32,address,bool,uint256,uint256,uint256,uint256,uint256,uint256)")
	require.Len(t, swaps, 1)
	require.Equal(t, []common.Hash{swaps[0].Topics[0], addressTopic(pool), addressTopic(h.caller)}, swaps[0].Topics)
	same(t, []interface{}{true, in, bought, buyFee, price, genesisTime.Unix(), h.ctx().BlockHeight()},
		eventData(t, directRole, "Swap", swaps[0]))

	sellIn := big.NewInt(4_803_406_952_560)
	sold, sellFee := big.NewInt(47_278_912), big.NewInt(144_102_208_576)
	h.approve(token, h.caller, sellIn)
	require.Equal(t, errorData("SlippageExceeded()"), h.fails(h.caller, router, types.RoleRouter, "sell", pool, sellIn,
		new(big.Int).Add(sold, big.NewInt(1)), deadline))
	out, quotedFee = quoteResult(t, h.view(quoter, types.RoleQuoter, "quoteExactInput", pool, sellIn, false)[0])
	require.Equal(t, sold, out)
	require.Equal(t, sellFee, quotedFee)
	callerUsdl := h.balanceOf(h.usdl, h.caller)
	require.Equal(t, sold, h.do(h.caller, router, types.RoleRouter, "sell", pool, sellIn, sold, deadline)[0])
	require.Equal(t, new(big.Int).Add(callerUsdl, sold), h.balanceOf(h.usdl, h.caller))
	require.Equal(t, new(big.Int).Sub(bought, sellIn), h.balanceOf(token, h.caller))
	require.Equal(t, new(big.Int).Add(new(big.Int).Sub(supply, bought), sellIn), h.balanceOf(token, pool))
	require.Equal(t, big.NewInt(49_721_088), h.view(pool, types.RolePool, "realUsdlBalance")[0])
	require.Equal(t, big.NewInt(49_721_088), h.ledger(launchpadkeeper.HeldQuoteLedger(pool)))
	require.Equal(t, sellFee, h.view(pool, types.RolePool, "accumulatedTokenFees")[0])
	require.Equal(t, sold, h.view(pool, types.RolePool, "cumulativeSellUsdlVolume")[0])
	afterSell := h.view(pool, types.RolePool, "getPrice")[0].(*big.Int)
	require.Equal(t, -1, afterSell.Cmp(price), "a sell lowers the price")
	require.Equal(t, poolCut, h.ledger(launchpadkeeper.FeeHeldLedger(feeAccumulator)), "a sell moves no USDL fee")

	stranger := h.account()
	h.transfer(h.usdl, h.caller, stranger, big.NewInt(10))
	h.approve(h.usdl, stranger, big.NewInt(1_000_000))
	require.Equal(t, errorData("TransferFromFailed()"), h.fails(stranger, router, types.RoleRouter, "buy", pool,
		big.NewInt(1_000_000), big.NewInt(0), deadline))
	require.Equal(t, errorData("DeadlineExpired()"), h.fails(h.caller, router, types.RoleRouter, "buy", pool,
		big.NewInt(1_000_000), big.NewInt(0), big.NewInt(genesisTime.Unix()-1)))
	require.Equal(t, errorData("ZeroAmount()"), h.fails(h.caller, router, types.RoleRouter, "buy", pool,
		big.NewInt(0), big.NewInt(0), deadline))
	require.Equal(t, errorData("PoolNotFound()"), h.fails(h.caller, router, types.RoleRouter, "buy", h.caller,
		big.NewInt(1_000_000), big.NewInt(0), deadline))
	h.approve(h.usdl, h.caller, fee)
	require.Equal(t, errorData("ZeroAddress()"), h.fails(h.caller, router, types.RoleRouter, "createMarket",
		"Kindle Token", "KNDL", uint8(4), common.Address{}))

	require.Equal(t, errorData("Unauthorized()"), h.fails(stranger, pool, types.RolePool, "pause"))
	h.do(h.caller, pool, types.RolePool, "pause")
	require.Equal(t, true, h.view(pool, types.RolePool, "paused")[0])
	held, tokens := h.ledger(launchpadkeeper.HeldQuoteLedger(pool)), h.balanceOf(token, h.caller)
	h.approve(token, h.caller, big.NewInt(1))
	require.Equal(t, errorData("Paused()"), h.fails(h.caller, router, types.RoleRouter, "sell", pool, big.NewInt(1),
		big.NewInt(0), deadline))
	require.Equal(t, held, h.ledger(launchpadkeeper.HeldQuoteLedger(pool)))
	require.Equal(t, tokens, h.balanceOf(token, h.caller))
	h.do(h.caller, pool, types.RolePool, "unpause")

	second := big.NewInt(1_000_000_000_000)
	h.approve(token, h.caller, second)
	h.do(h.caller, router, types.RoleRouter, "sell", pool, second, big.NewInt(0), deadline)
	third := h.view(pool, types.RolePool, "getPrice")[0].(*big.Int)
	require.Equal(t, -1, third.Cmp(afterSell), "a second sell lowers the price again")
	market = h.market(pool)
	same(t, []interface{}{market.VirtualQuoteReserve.BigInt(), market.RealQuoteBalance.BigInt(),
		market.TokenReserve.BigInt()}, h.view(pool, types.RolePool, "getReserves"))
	feeBps, err := h.keeper.FeeBps(h.ctx(), market)
	require.NoError(t, err)
	require.Equal(t, feeBps, h.view(pool, types.RolePool, "currentFeeBps")[0])
	message, broken := launchpadkeeper.SolvencyInvariant(h.keeper)(h.ctx())
	require.False(t, broken, message)
}

func TestRejectsDelegatecallStaticcallWritesAndValue(t *testing.T) {
	h := newHarness(t)
	_, pool, _ := h.createMarket(h.caller, "Kindle Token", "KNDL", types.FeeStrategyClaim, common.Address{})
	h.approve(h.usdl, h.caller, big.NewInt(1_000_000))
	buy := pack(t, directRole, "buy", pool, big.NewInt(1_000_000), big.NewInt(0), deadline)

	ret, err := h.direct(h.caller, buy, nil, false, true)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Contains(t, revertReason(t, ret), "delegatecall")
	ret, err = h.direct(h.caller, pack(t, directRole, "getPoolCount"), nil, true, true)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Contains(t, revertReason(t, ret), "delegatecall")

	for _, c := range []struct {
		to    common.Address
		input []byte
	}{
		{router, pack(t, types.RoleRouter, "buy", pool, big.NewInt(1_000_000), big.NewInt(0), deadline)},
		{router, pack(t, types.RoleRouter, "createMarket", "Kindle Token", "KNDL", uint8(0), common.Address{})},
		{pool, pack(t, types.RolePool, "pause")},
		{precompile, buy},
	} {
		ret, _, err := h.evm.StaticCall(h.caller, c.to, c.input, callGas)
		require.ErrorIs(t, err, vm.ErrExecutionReverted, c.to.Hex())
		require.Contains(t, revertReason(t, ret), "staticcall", c.to.Hex())
	}

	ret, err = h.direct(h.caller, buy, big.NewInt(1), false, false)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.NotEmpty(t, ret)

	require.Equal(t, big.NewInt(1), h.view(poolRegistry, types.RolePoolRegistry, "getPoolCount")[0])
	require.Equal(t, 0, h.ledger(launchpadkeeper.HeldQuoteLedger(pool)).Sign(), "no rejected call moved funds")
	require.Equal(t, h.creationFee(), h.balanceOf(h.usdl, h.custody()))
	require.Equal(t, false, h.view(pool, types.RolePool, "paused")[0])
}
