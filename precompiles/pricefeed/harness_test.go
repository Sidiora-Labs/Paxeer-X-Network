package pricefeed_test

import (
	"fmt"
	"maps"
	"math"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
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
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const suppliedGas = uint64(2_000_000)

var pricefeedAddress = common.HexToAddress("0x0000000000000000000000000000000000001020")

type keepers struct {
	utils.Keepers
	pricefeed *pricefeedkeeper.Keeper
}

func (k keepers) PriceFeedK() *pricefeedkeeper.Keeper { return k.pricefeed }

type harness struct {
	t          *testing.T
	stateDB    *state.DBImpl
	evm        *vm.EVM
	precompile *pricefeed.Precompile
	keeper     *pricefeedkeeper.Keeper
	caller     common.Address
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx(nil).WithBlockHeight(8).WithBlockTime(time.Unix(100, 0)).WithEventManager(sdk.NewEventManager())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	k, ctx := pricefeedtestutil.NewKeeper(app, ctx)
	require.NoError(t, k.InitGenesis(ctx, *pricefeedtypes.DefaultGenesis()))
	p, err := pricefeed.NewPrecompile(keepers{Keepers: app.GetPrecompileKeepers(), pricefeed: &k})
	require.NoError(t, err)
	h := &harness{t: t, keeper: &k, precompile: p, caller: common.HexToAddress("0x9001")}
	h.stateDB = state.NewDBImpl(ctx, &app.EvmKeeper, false)
	block, err := app.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
	require.NoError(t, err)
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(app.EvmKeeper.ChainID(ctx))
	contracts := maps.Clone(app.EvmKeeper.CustomPrecompiles(ctx))
	if contracts == nil {
		contracts = make(map[common.Address]vm.PrecompiledContract)
	}
	contracts[pricefeedAddress] = p
	h.evm = vm.NewEVM(*block, h.stateDB, cfg, vm.Config{}, contracts)
	coins := sdk.NewCoins(sdk.NewCoin(sdk.MustGetBaseDenom(), sdk.NewInt(10)))
	require.NoError(t, app.BankKeeper.MintCoins(h.ctx(), "evm", coins))
	require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(h.ctx(), "evm", app.EvmKeeper.GetPaxAddressOrDefault(h.ctx(), h.caller), coins))
	return h
}

func (h *harness) ctx() sdk.Context { return h.stateDB.Ctx() }

func (h *harness) method(name string) abi.Method {
	h.t.Helper()
	method, ok := h.precompile.GetABI().Methods[name]
	require.True(h.t, ok, name)
	return method
}

func (h *harness) input(name string, args ...interface{}) []byte {
	h.t.Helper()
	method := h.method(name)
	data, err := method.Inputs.Pack(args...)
	require.NoError(h.t, err)
	return append(append([]byte(nil), method.ID...), data...)
}

func (h *harness) call(from common.Address, name string, args ...interface{}) ([]interface{}, uint64, string) {
	h.t.Helper()
	ret, left, err := h.evm.Call(from, pricefeedAddress, h.input(name, args...), suppliedGas, uint256.NewInt(0))
	if err != nil {
		require.ErrorIs(h.t, err, vm.ErrExecutionReverted)
		require.GreaterOrEqual(h.t, len(ret), 4)
		require.Equal(h.t, []byte{0x08, 0xc3, 0x79, 0xa0}, ret[:4])
		reason, unpackErr := abi.UnpackRevert(ret)
		require.NoError(h.t, unpackErr)
		require.NotEmpty(h.t, reason)
		return nil, left, reason
	}
	out, err := h.method(name).Outputs.Unpack(ret)
	require.NoError(h.t, err)
	return out, left, ""
}

func (h *harness) view(name string, args ...interface{}) []interface{} {
	h.t.Helper()
	ret, _, err := h.evm.StaticCall(h.caller, pricefeedAddress, h.input(name, args...), suppliedGas)
	require.NoError(h.t, err)
	out, err := h.method(name).Outputs.Unpack(ret)
	require.NoError(h.t, err)
	return out
}

func requireCode(t *testing.T, reason, code string) {
	t.Helper()
	got, _, found := strings.Cut(reason, ":")
	require.True(t, found, reason)
	require.Equal(t, code, got)
}

func requireRevert(t *testing.T, ret []byte, err error, code string) {
	t.Helper()
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.GreaterOrEqual(t, len(ret), 4)
	require.Equal(t, []byte{0x08, 0xc3, 0x79, 0xa0}, ret[:4])
	reason, unpackErr := abi.UnpackRevert(ret)
	require.NoError(t, unpackErr)
	requireCode(t, reason, code)
}

func (h *harness) validator(seed string, status stakingtypes.BondStatus, associated bool) (sdk.ValAddress, common.Address) {
	h.t.Helper()
	key := secp256k1.GenPrivKeyFromSecret([]byte("operator " + seed))
	account, evmAddress := testkeeper.PrivateKeyToAddresses(key)
	operator := sdk.ValAddress(account)
	val, err := stakingtypes.NewValidator(operator, ed25519.GenPrivKeyFromSecret([]byte("consensus "+seed)).PubKey(), stakingtypes.Description{})
	require.NoError(h.t, err)
	testkeeper.EVMTestApp.StakingKeeper.SetValidator(h.ctx(), val.UpdateStatus(status))
	require.NotEqual(h.t, common.BytesToAddress(operator), evmAddress)
	if associated {
		testkeeper.EVMTestApp.EvmKeeper.SetAddressMapping(h.ctx(), account, evmAddress)
	}
	return operator, evmAddress
}

func feedPubKey(seed string) []byte {
	return secp256k1.GenPrivKeyFromSecret([]byte("feed " + seed)).PubKey().Bytes()
}

func feedAccount(key []byte) sdk.AccAddress {
	return sdk.AccAddress((&secp256k1.PubKey{Key: key}).Address())
}

func forwardingCode(delegate bool) []byte {
	code := common.FromHex("36600060003760006000366000")
	if !delegate {
		code = append(code, 0x60, 0x00)
	}
	code = append(code, 0x73)
	code = append(code, pricefeedAddress.Bytes()...)
	opcode := byte(0xf1)
	if delegate {
		opcode = 0xf4
	}
	code = append(code, 0x5a, opcode, 0x3d, 0x60, 0x00, 0x60, 0x00, 0x3e, 0x60)
	destination := len(code)
	code = append(code, 0x00, 0x57, 0x3d, 0x60, 0x00, 0xfd)
	code[destination] = byte(len(code))
	return append(code, 0x5b, 0x3d, 0x60, 0x00, 0xf3)
}

func TestConventionRequiredGas(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		name   string
		args   []interface{}
		length int
		gas    uint64
	}{
		{"getPrice", []interface{}{big.NewInt(0)}, 36, 3512},
		{"registerFeedKey", []interface{}{make([]byte, 32)}, 100, 31536},
		{"registerFeedKey", []interface{}{feedPubKey("gas")}, 132, 32048},
		{"revokeFeedKey", nil, 4, 30000},
		{"getMarketCount", nil, 4, 3000},
	} {
		t.Run(fmt.Sprintf("%s_%d", tc.name, tc.length), func(t *testing.T) {
			input := h.input(tc.name, tc.args...)
			require.Len(t, input, tc.length)
			require.Equal(t, tc.gas, h.precompile.RequiredGas(input))
		})
	}
	t.Run("metered_view", func(t *testing.T) {
		out, left, reason := h.call(h.caller, "getMarketCount")
		require.Empty(t, reason)
		require.Len(t, out, 1)
		require.Equal(t, "18", out[0].(*big.Int).String())
		require.Equal(t, suppliedGas-3000, left)
	})
	t.Run("insufficient_write_gas", func(t *testing.T) {
		operator, caller := h.validator("gas", stakingtypes.Bonded, true)
		key := feedPubKey("gas")
		_, left, err := h.evm.Call(caller, pricefeedAddress, h.input("registerFeedKey", key), 32047, uint256.NewInt(0))
		require.ErrorIs(t, err, vm.ErrOutOfGas)
		require.Zero(t, left)
		_, found := h.keeper.GetFeedKey(h.ctx(), operator)
		require.False(t, found)
		require.Empty(t, h.stateDB.GetAllLogs())
	})
}

func TestConventionGuards(t *testing.T) {
	t.Run("delegatecall", func(t *testing.T) {
		h := newHarness(t)
		forwarder := common.HexToAddress("0x9002")
		h.stateDB.SetCode(forwarder, forwardingCode(true))
		ret, _, err := h.evm.Call(h.caller, forwarder, h.input("getMarketCount"), suppliedGas, uint256.NewInt(0))
		requireRevert(t, ret, err, "LXP_PRICEFEED_DELEGATECALL")
		require.Empty(t, h.stateDB.GetAllLogs())
	})
	for _, name := range []string{"registerFeedKey", "revokeFeedKey"} {
		t.Run("delegatecall_"+name, func(t *testing.T) {
			h := newHarness(t)
			operator, caller := h.validator("delegate", stakingtypes.Bonded, true)
			key := feedPubKey("delegate")
			_, _, err := h.keeper.RegisterFeedKey(h.ctx(), operator, key)
			require.NoError(t, err)
			forwarder := common.HexToAddress("0x9002")
			h.stateDB.SetCode(forwarder, forwardingCode(true))
			var args []interface{}
			if name == "registerFeedKey" {
				args = []interface{}{feedPubKey("delegate_replacement")}
			}
			ret, _, err := h.evm.Call(caller, forwarder, h.input(name, args...), suppliedGas, uint256.NewInt(0))
			requireRevert(t, ret, err, "LXP_PRICEFEED_DELEGATECALL")
			h.requireFeedKey(operator, key)
			h.requireNoHolder(feedAccount(feedPubKey("delegate_replacement")))
			require.Empty(t, h.stateDB.GetAllLogs())
		})
	}
	for _, name := range []string{"registerFeedKey", "revokeFeedKey"} {
		t.Run("staticcall_"+name, func(t *testing.T) {
			h := newHarness(t)
			operator, caller := h.validator("static", stakingtypes.Bonded, true)
			key := feedPubKey("static")
			var args []interface{}
			if name == "registerFeedKey" {
				args = []interface{}{key}
			} else {
				_, _, err := h.keeper.RegisterFeedKey(h.ctx(), operator, key)
				require.NoError(t, err)
			}
			before, foundBefore := h.keeper.GetFeedKey(h.ctx(), operator)
			ret, _, err := h.evm.StaticCall(caller, pricefeedAddress, h.input(name, args...), suppliedGas)
			requireRevert(t, ret, err, "LXP_PRICEFEED_STATICCALL")
			after, foundAfter := h.keeper.GetFeedKey(h.ctx(), operator)
			require.Equal(t, foundBefore, foundAfter)
			require.Equal(t, before, after)
			require.Empty(t, h.stateDB.GetAllLogs())
		})
	}
	t.Run("keeper_missing", func(t *testing.T) {
		h := newHarness(t)
		unwired, err := pricefeed.NewPrecompile(keepers{Keepers: testkeeper.EVMTestApp.GetPrecompileKeepers()})
		require.NoError(t, err)
		for _, tc := range []struct {
			name string
			args []interface{}
		}{
			{"getMarketCount", nil},
			{"registerFeedKey", []interface{}{feedPubKey("missing")}},
			{"revokeFeedKey", nil},
		} {
			ret, err := unwired.Run(h.evm, h.caller, h.caller, h.input(tc.name, tc.args...), big.NewInt(0), false, false, nil)
			requireRevert(t, ret, err, "LXP_PRICEFEED_KEEPER_MISSING")
		}
		require.Empty(t, h.stateDB.GetAllLogs())
	})
}

func TestConventionNonPayableAndArgCount(t *testing.T) {
	for _, name := range []string{"registerFeedKey", "revokeFeedKey"} {
		t.Run("value_"+name, func(t *testing.T) {
			h := newHarness(t)
			operator, caller := h.validator("value", stakingtypes.Bonded, true)
			key := feedPubKey("value")
			_, _, err := h.keeper.RegisterFeedKey(h.ctx(), operator, key)
			require.NoError(t, err)
			coins := sdk.NewCoins(sdk.NewCoin(sdk.MustGetBaseDenom(), sdk.NewInt(1)))
			require.NoError(t, testkeeper.EVMTestApp.BankKeeper.MintCoins(h.ctx(), "evm", coins))
			require.NoError(t, testkeeper.EVMTestApp.BankKeeper.SendCoinsFromModuleToAccount(h.ctx(), "evm", sdk.AccAddress(operator), coins))
			before := new(uint256.Int).Set(h.stateDB.GetBalance(caller))
			var args []interface{}
			if name == "registerFeedKey" {
				args = []interface{}{feedPubKey("value_replacement")}
			}
			ret, _, err := h.evm.Call(caller, pricefeedAddress, h.input(name, args...), suppliedGas, uint256.NewInt(1))
			requireRevert(t, ret, err, "LXP_PRECOMPILE_NON_PAYABLE")
			require.Equal(t, before, h.stateDB.GetBalance(caller))
			require.True(t, h.stateDB.GetBalance(pricefeedAddress).IsZero())
			h.requireFeedKey(operator, key)
			h.requireNoHolder(feedAccount(feedPubKey("value_replacement")))
			require.Empty(t, h.stateDB.GetAllLogs())
		})
	}
	t.Run("value", func(t *testing.T) {
		h := newHarness(t)
		before := new(uint256.Int).Set(h.stateDB.GetBalance(h.caller))
		ret, _, err := h.evm.Call(h.caller, pricefeedAddress, h.input("getMarketCount"), suppliedGas, uint256.NewInt(1))
		requireRevert(t, ret, err, "LXP_PRECOMPILE_NON_PAYABLE")
		require.Equal(t, before, h.stateDB.GetBalance(h.caller))
		require.True(t, h.stateDB.GetBalance(pricefeedAddress).IsZero())
		require.Empty(t, h.stateDB.GetAllLogs())
	})
	t.Run("argument_counts", func(t *testing.T) {
		h := newHarness(t)
		for name, method := range h.precompile.GetABI().Methods {
			t.Run(name, func(t *testing.T) {
				counts := []int{len(method.Inputs) + 1}
				if len(method.Inputs) > 0 {
					counts = append(counts, len(method.Inputs)-1)
				}
				for _, count := range counts {
					ret, err := h.precompile.GetExecutor().Execute(h.ctx(), &method, h.caller, h.caller, make([]interface{}, count), big.NewInt(0), false, h.evm, nil)
					require.Nil(t, ret)
					require.EqualError(t, err, fmt.Sprintf("LXP_PRECOMPILE_ARG_COUNT: expected %d arguments but got %d", len(method.Inputs), count))
				}
			})
		}
	})
	t.Run("get_price_without_argument", func(t *testing.T) {
		h := newHarness(t)
		ret, _, err := h.evm.Call(h.caller, pricefeedAddress, h.method("getPrice").ID, suppliedGas, uint256.NewInt(0))
		requireRevert(t, ret, err, "LXP_PRECOMPILE_ARG_COUNT")
	})
}

func TestConventionRevertData(t *testing.T) {
	h := newHarness(t)
	operator, caller := h.validator("revert", stakingtypes.Bonded, true)
	ret, _, err := h.evm.Call(caller, pricefeedAddress, h.input("registerFeedKey", []byte{}), suppliedGas, uint256.NewInt(0))
	requireRevert(t, ret, err, "LXP_PRICEFEED_INVALID_KEY")
	_, found := h.keeper.GetFeedKey(h.ctx(), operator)
	require.False(t, found)
	require.Empty(t, h.stateDB.GetAllLogs())
}
