package launchpad

import (
	"bytes"
	"encoding/json"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

type opticalContractArtifact struct {
	ABI     json.RawMessage `json:"abi"`
	Bin     string          `json:"bin"`
	Runtime string          `json:"bin-runtime"`
}

func opticalContracts(t *testing.T) map[string]opticalContractArtifact {
	t.Helper()
	root := os.Getenv("LAUNCHPAD_CONTRACTS_DIR")
	require.NotEmpty(t, root, "LAUNCHPAD_CONTRACTS_DIR must name the reference contracts directory")
	solc := os.Getenv("SOLC")
	if solc == "" {
		solc = "solc"
	}
	command := exec.Command(solc, "--base-path", root, "--combined-json", "abi,bin,bin-runtime",
		"--optimize", "--via-ir", "--evm-version", "paris",
		filepath.Join(root, "opticals/presets/LaunchpadOptical.sol"),
		filepath.Join(root, "core/SidioraPool.sol"),
		filepath.Join(root, "core/SidioraERC20.sol"),
		filepath.Join(root, "data/PoolRegistry.sol"),
		filepath.Join(root, "test/BitFlagWrapper.sol"))
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	output, err := command.Output()
	require.NoError(t, err, diagnostics.String())
	var compiled struct {
		Contracts map[string]opticalContractArtifact `json:"contracts"`
	}
	require.NoError(t, json.Unmarshal(output, &compiled))
	artifacts := make(map[string]opticalContractArtifact)
	for name, artifact := range compiled.Contracts {
		artifacts[name[strings.LastIndex(name, ":")+1:]] = artifact
	}
	return artifacts
}

func TestOpticalHooksSeeTheContractCallers(t *testing.T) {
	artifacts := opticalContracts(t)
	db, err := state.New(ethtypes.EmptyRootHash, state.NewDatabaseForTesting())
	require.NoError(t, err)
	creator := common.BytesToAddress(crypto.Keccak256([]byte("creator")))
	pool := common.BytesToAddress(crypto.Keccak256([]byte("pool")))
	registry := common.BytesToAddress(crypto.Keccak256([]byte("registry")))
	recipient := common.BytesToAddress(crypto.Keccak256([]byte("recipient")))
	accumulator := launchpadkeeper.Generation2Surfaces[types.RoleFeeAccumulator]
	require.Equal(t, common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66"), accumulator)
	type opticalCall struct {
		from, to common.Address
		input    []byte
	}
	var calls []opticalCall
	cfg := &runtime.Config{State: db, Origin: creator, Time: 100, BlockNumber: big.NewInt(1), GasLimit: 30_000_000,
		EVMConfig: vm.Config{Tracer: &tracing.Hooks{OnEnter: func(_ int, _ byte, from, to common.Address, input []byte, _ uint64, _ *big.Int) {
			calls = append(calls, opticalCall{from, to, common.CopyBytes(input)})
		}}}}
	runtime.SetDefaults(cfg)
	evm := runtime.NewEnv(cfg)
	contractABI := func(name string) abi.ABI {
		artifact, ok := artifacts[name]
		require.True(t, ok, name)
		parsed, err := abi.JSON(bytes.NewReader(artifact.ABI))
		require.NoError(t, err)
		return parsed
	}
	pack := func(name, method string, args ...interface{}) []byte {
		parsed := contractABI(name)
		input, err := parsed.Pack(method, args...)
		require.NoError(t, err)
		return input
	}
	deploy := func(name string, args ...interface{}) common.Address {
		parsed := contractABI(name)
		constructor, err := parsed.Constructor.Inputs.Pack(args...)
		require.NoError(t, err)
		code := common.FromHex(artifacts[name].Bin)
		require.NotEmpty(t, code, name)
		_, address, _, err := evm.Create(creator, append(code, constructor...), cfg.GasLimit, uint256.NewInt(0))
		require.NoError(t, err, name)
		return address
	}
	call := func(from, to common.Address, input []byte) []byte {
		ret, _, err := evm.Call(from, to, input, cfg.GasLimit, uint256.NewInt(0))
		require.NoError(t, err, "return: %x", ret)
		return ret
	}
	callNested := func(ctx sdk.Context, from, to common.Address, input []byte) (sdk.Context, []byte, error) {
		ret, _, err := evm.Call(from, to, input, cfg.GasLimit, uint256.NewInt(0))
		return ctx, ret, err
	}
	for name, address := range map[string]common.Address{"PoolRegistry": registry, "SidioraPool": pool} {
		code := common.FromHex(artifacts[name].Runtime)
		require.NotEmpty(t, code, name)
		db.SetCode(address, code)
	}
	call(creator, registry, pack("PoolRegistry", "initialize", common.Address{}, creator))
	call(creator, registry, pack("PoolRegistry", "grantRole", crypto.Keccak256Hash([]byte("FACTORY_ROLE")), creator))
	token := deploy("SidioraERC20", "Pool Token", "POOL", big.NewInt(1_000_000), creator)
	optical := deploy("LaunchpadOptical", registry, creator, creator, []common.Address{},
		big.NewInt(10), big.NewInt(100), big.NewInt(1000), big.NewInt(1000), creator, accumulator)
	call(creator, registry, pack("PoolRegistry", "register", pool, token, creator, optical, big.NewInt(1)))
	db.SetState(pool, common.BigToHash(big.NewInt(0)), common.BytesToHash(token.Bytes()))
	db.SetState(pool, common.BigToHash(big.NewInt(2)), common.BytesToHash(optical.Bytes()))
	db.SetState(pool, common.BigToHash(big.NewInt(3)), common.BytesToHash(accumulator.Bytes()))
	call(creator, token, pack("SidioraERC20", "bindPool", pool))
	market := types.Market{PoolAddress: pool.Hex(), TokenAddress: token.Hex(), Optical: optical.Hex(), Generation: types.Generation1}
	ctx := sdk.Context{}
	opticalABI := contractABI("LaunchpadOptical")
	assertCall := func(index int, from common.Address, method string, args ...interface{}) {
		t.Helper()
		require.Greater(t, len(calls), index)
		require.Equal(t, opticalCall{from, optical, pack("LaunchpadOptical", method, args...)}, calls[index])
	}
	readAmount := func(method string, args ...interface{}) *big.Int {
		ret := call(creator, optical, pack("LaunchpadOptical", method, args...))
		values, err := opticalABI.Methods[method].Outputs.Unpack(ret)
		require.NoError(t, err)
		return values[0].(*big.Int)
	}
	assertStorage := func(slot int64, value int64) {
		t.Helper()
		key := crypto.Keccak256Hash(common.LeftPadBytes(pool.Bytes(), 32), common.LeftPadBytes(big.NewInt(slot).Bytes(), 32))
		require.Equal(t, common.BigToHash(big.NewInt(value)), db.GetState(optical, key))
	}
	var flags uint8
	t.Run("getFlags uses the pool", func(t *testing.T) {
		calls = nil
		_, ret, err := callOptical(ctx, callNested, market, 0, "getFlags")
		require.NoError(t, err)
		require.Equal(t, common.LeftPadBytes([]byte{23}, 32), ret)
		flags = ret[31]
		require.Len(t, calls, 1)
		assertCall(0, pool, "getFlags")
		assertStorage(1, 0)
	})
	t.Run("BitFlag intersection semantics", func(t *testing.T) {
		wrapper := deploy("BitFlagWrapper")
		parsed := contractABI("BitFlagWrapper")
		for _, mask := range []uint8{0, 1, 2, 4, 8, 16, 3, 9, 24, 31, 128, 255} {
			ret := call(creator, wrapper, pack("BitFlagWrapper", "hasFlag", flags, mask))
			values, err := parsed.Methods["hasFlag"].Outputs.Unpack(ret)
			require.NoError(t, err)
			require.Equal(t, values[0].(bool), hasFlag(flags, mask), "mask %d", mask)
		}
		require.True(t, hasFlag(1, 3))
		require.False(t, hasFlag(flags, 8))
	})
	t.Run("beforeSwap records registry start once", func(t *testing.T) {
		calls = nil
		_, ret, err := callOptical(ctx, callNested, market, flags, "beforeSwap", creator, true, big.NewInt(1000))
		require.NoError(t, err)
		expected, err := opticalABI.Methods["beforeSwap"].Outputs.Pack(true, big.NewInt(0))
		require.NoError(t, err)
		require.Equal(t, expected, ret)
		assertCall(0, pool, "beforeSwap", pool, creator, true, big.NewInt(1000))
		assertStorage(1, 100)
		require.Equal(t, int64(100), readAmount("poolStartTime", pool).Int64())
	})
	t.Run("afterSwap keeps token accounting with the token", func(t *testing.T) {
		calls = nil
		_, ret, err := callOptical(ctx, callNested, market, flags, "afterSwap", creator, false, big.NewInt(1000), big.NewInt(500))
		require.NoError(t, err)
		require.Equal(t, common.RightPadBytes(common.FromHex("f74c1c1a"), 32), ret)
		assertCall(0, pool, "afterSwap", pool, creator, false, big.NewInt(1000), big.NewInt(500))
		require.Equal(t, int64(0), readAmount("tokensSold", pool, creator).Int64())
		assertStorage(1, 100)
	})
	t.Run("fee hooks use the current accumulator for every generation", func(t *testing.T) {
		for _, generation := range []types.Generation{types.Generation1, types.Generation2} {
			m := market
			m.Generation = generation
			calls = nil
			_, ret, err := callFeeOptical(ctx, callNested, m, "beforeFeeDistribution", big.NewInt(10_000))
			require.NoError(t, err)
			require.Equal(t, common.LeftPadBytes(big.NewInt(9000).Bytes(), 32), ret)
			assertCall(0, accumulator, "getFlags")
			assertCall(1, accumulator, "beforeFeeDistribution", pool, big.NewInt(10_000))
		}
		assertStorage(3, 2000)
		assertStorage(4, 2000)
		require.Equal(t, int64(2000), readAmount("accumulatedUsdl", pool).Int64())
		require.Equal(t, int64(2000), readAmount("totalRaised", pool).Int64())
	})
	t.Run("disabled fee after hook is not called", func(t *testing.T) {
		calls = nil
		_, ret, err := callFeeOptical(ctx, callNested, market, "afterFeeDistribution", big.NewInt(9000))
		require.NoError(t, err)
		require.Equal(t, common.RightPadBytes(common.FromHex("ae3bd4bd"), 32), ret)
		require.Len(t, calls, 1)
		assertCall(0, accumulator, "getFlags")
		assertStorage(3, 2000)
	})
	t.Run("enabled fee after hook accepts only the accumulator", func(t *testing.T) {
		calls = nil
		_, ret, err := invokeOptical(ctx, callNested, market, accumulator, 8, 8, "afterFeeDistribution", pool, big.NewInt(9000))
		require.NoError(t, err)
		require.Equal(t, common.RightPadBytes(common.FromHex("ae3bd4bd"), 32), ret)
		assertCall(0, accumulator, "afterFeeDistribution", pool, big.NewInt(9000))
		_, ret, err = invokeOptical(ctx, callNested, market, pool, 8, 8, "afterFeeDistribution", pool, big.NewInt(9000))
		require.ErrorIs(t, err, vm.ErrExecutionReverted)
		require.Equal(t, common.FromHex("6f61f641"), ret)
	})
	t.Run("foreign pool revert is preserved", func(t *testing.T) {
		m := market
		m.PoolAddress = recipient.Hex()
		_, ret, err := callOptical(ctx, callNested, m, flags, "beforeSwap", creator, true, big.NewInt(1000))
		require.ErrorIs(t, err, vm.ErrExecutionReverted)
		require.Equal(t, common.FromHex("6f61f641"), ret)
	})
	t.Run("transfer hook executes only from the real token", func(t *testing.T) {
		require.Equal(t, opticalAddress(market), common.BytesToAddress(call(creator, pool, pack("SidioraPool", "opticalAddress"))))
		calls = nil
		ret, _, err := evm.Call(creator, token, pack("SidioraERC20", "transfer", recipient, big.NewInt(100)), cfg.GasLimit, uint256.NewInt(0))
		require.ErrorIs(t, err, vm.ErrExecutionReverted)
		require.Equal(t, common.FromHex("1392d261"), ret)
		assertCall(2, token, "getFlags")
		assertCall(3, token, "beforeTokenTransfer", pool, creator, recipient, big.NewInt(100))
		evm.Context.Time = 160
		call(creator, token, pack("SidioraERC20", "transfer", recipient, big.NewInt(100)))
		require.Equal(t, int64(100), readAmount("tokensSold", pool, creator).Int64())
		call(creator, token, pack("SidioraERC20", "approve", recipient, big.NewInt(50)))
		call(recipient, token, pack("SidioraERC20", "transferFrom", creator, recipient, big.NewInt(50)))
		require.Equal(t, int64(150), readAmount("tokensSold", pool, creator).Int64())
		_, _, err = callOptical(ctx, callNested, market, flags, "beforeSwap", creator, false, big.NewInt(150))
		require.NoError(t, err)
		require.Equal(t, int64(150), readAmount("tokensSold", pool, creator).Int64())
		ret = call(creator, token, pack("SidioraERC20", "balanceOf", recipient))
		require.Equal(t, common.LeftPadBytes(big.NewInt(150).Bytes(), 32), ret)
	})
	t.Run("zero optical and absent flags make no calls", func(t *testing.T) {
		m := market
		m.Optical = common.Address{}.Hex()
		calls = nil
		_, ret, err := callOptical(ctx, callNested, m, 0, "getFlags")
		require.NoError(t, err)
		require.Equal(t, make([]byte, 32), ret)
		_, ret, err = callFeeOptical(ctx, callNested, m, "beforeFeeDistribution", big.NewInt(77))
		require.NoError(t, err)
		require.Equal(t, common.LeftPadBytes([]byte{77}, 32), ret)
		_, ret, err = callOptical(ctx, callNested, market, 0, "afterSwap", creator, true, big.NewInt(1), big.NewInt(1))
		require.NoError(t, err)
		require.Equal(t, common.RightPadBytes(common.FromHex("f74c1c1a"), 32), ret)
		require.Empty(t, calls)
	})
	t.Run("missing optical code reverts on decoding", func(t *testing.T) {
		m := market
		m.Optical = recipient.Hex()
		_, ret, err := callOptical(ctx, callNested, m, 0, "getFlags")
		require.ErrorIs(t, err, vm.ErrExecutionReverted)
		require.Empty(t, ret)
	})
	t.Run("invalid amounts are refused before the hook", func(t *testing.T) {
		for _, amount := range []*big.Int{nil, big.NewInt(-1), new(big.Int).Lsh(big.NewInt(1), 256)} {
			calls = nil
			_, ret, err := callOptical(ctx, callNested, market, flags, "beforeSwap", creator, true, amount)
			require.ErrorIs(t, err, types.ErrOverflow)
			require.Empty(t, ret)
			require.Empty(t, calls)
		}
	})

	t.Run("precompile never dispatches token transfer hooks", func(t *testing.T) {
		calls = nil
		_, _, err := callOptical(ctx, callNested, market, flags, "beforeTokenTransfer", creator, recipient, big.NewInt(1))
		require.ErrorIs(t, err, types.ErrInvalidHookResponse)
		require.Empty(t, calls)
	})
}
