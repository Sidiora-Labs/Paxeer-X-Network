package eip712

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	agentcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	codectypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	upgradekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/keeper"
	upgradetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

const (
	nameCap    = 64
	versionCap = 32
	planName   = "merge-plan"
)

func fixture(t *testing.T) (sdk.Context, *upgradekeeper.Keeper, *agentcommon.Precompile) {
	t.Helper()
	db := dbm.NewMemDB()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	key := sdk.NewKVStoreKey(upgradetypes.StoreKey)
	ms := store.NewCommitMultiStore(db)
	ms.MountStoreWithDB(key, sdk.StoreTypeIAVL, db)
	require.NoError(t, ms.LoadLatestVersion())
	ctx := sdk.NewContext(ms, tmproto.Header{Height: 100}, false)
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	upgrade := upgradekeeper.NewKeeper(nil, key, cdc, t.TempDir(), nil)
	upgrade.SetDone(ctx, planName)
	p, err := NewPrecompile(&upgrade, planName, nameCap, versionCap)
	require.NoError(t, err)
	return ctx, &upgrade, p
}

func evmContext(ctx sdk.Context) *vm.EVM {
	db := new(state.DBImpl)
	db.WithCtx(ctx)
	return &vm.EVM{StateDB: db}
}

func call(t *testing.T, ctx sdk.Context, p *agentcommon.Precompile, method string, args ...interface{}) []byte {
	t.Helper()
	input, err := p.Pack(method, args...)
	require.NoError(t, err)
	before := ctx.EventManager().Events()
	logs := 0
	hooks := &tracing.Hooks{OnLog: func(_ *ethtypes.Log) { logs++ }}
	evm := evmContext(ctx)
	state.GetDBImpl(evm.StateDB).SetLogger(hooks)
	data, err := p.Run(evm, common.Address{}, p.Address(), input, nil, true, false, hooks)
	require.NoError(t, err)
	require.Equal(t, before, ctx.EventManager().Events())
	require.Zero(t, logs)
	return data
}

func revertText(t *testing.T, data []byte) string {
	t.Helper()
	text, err := abi.UnpackRevert(data)
	require.NoError(t, err)
	return text
}

func TestDomainSeparatorGolden(t *testing.T) {
	ctx, _, p := fixture(t)
	maxChain := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	for _, vector := range []struct {
		name     string
		version  string
		chainID  *big.Int
		contract common.Address
	}{
		{"Paxeer", "1", big.NewInt(125), common.HexToAddress(Eip712Address)},
		{"", "", big.NewInt(0), common.Address{}},
		{"\u03c0", "\u00e9", maxChain, common.HexToAddress("0xffffffffffffffffffffffffffffffffffffffff")},
	} {
		t.Run(vector.name, func(t *testing.T) {
			chainID := ethmath.HexOrDecimal256(*vector.chainID)
			typed := apitypes.TypedData{
				Types: apitypes.Types{
					"EIP712Domain": {
						{Name: "name", Type: "string"},
						{Name: "version", Type: "string"},
						{Name: "chainId", Type: "uint256"},
						{Name: "verifyingContract", Type: "address"},
					},
					"Request": {{Name: "amount", Type: "uint256"}},
				},
				PrimaryType: "Request",
				Domain:      apitypes.TypedDataDomain{Name: vector.name, Version: vector.version, ChainId: &chainID, VerifyingContract: vector.contract.Hex()},
				Message:     apitypes.TypedDataMessage{"amount": "42"},
			}
			domainMap := apitypes.TypedDataMessage{"name": vector.name, "version": vector.version, "chainId": &chainID, "verifyingContract": vector.contract.Hex()}
			separator, err := typed.HashStruct("EIP712Domain", domainMap)
			require.NoError(t, err)
			beforeChain := new(big.Int).Set(vector.chainID)
			data := call(t, ctx, p, DomainSeparatorMethod, vector.name, vector.version, vector.chainID, vector.contract)
			require.Equal(t, []byte(separator), data)
			require.Zero(t, vector.chainID.Cmp(beforeChain))
			structHash, err := typed.HashStruct("Request", typed.Message)
			require.NoError(t, err)
			domainWord, structWord := common.BytesToHash(separator), common.BytesToHash(structHash)
			digest := call(t, ctx, p, HashTypedDataMethod, [32]byte(domainWord), [32]byte(structWord))
			preimage := append([]byte{0x19, 0x01}, separator...)
			preimage = append(preimage, structHash...)
			require.Equal(t, crypto.Keccak256(preimage), digest)
			if vector.name == "Paxeer" {
				require.Equal(t, typed.Domain.Map(), domainMap)
				want, _, err := apitypes.TypedDataAndHash(typed)
				require.NoError(t, err)
				require.Equal(t, want, digest)
			}
		})
	}
	zeroDigest := call(t, ctx, p, HashTypedDataMethod, [32]byte{}, [32]byte{})
	require.Equal(t, crypto.Keccak256(append([]byte{0x19, 0x01}, make([]byte, 64)...)), zeroDigest)
}

func TestRecoverLowS(t *testing.T) {
	ctx, _, p := fixture(t)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	wantSigner := crypto.PubkeyToAddress(key.PublicKey)
	domain := [32]byte(crypto.Keccak256Hash([]byte("Paxeer X Network")))
	for _, parity := range []byte{0, 1} {
		t.Run(string(rune('0'+parity)), func(t *testing.T) {
			var signature []byte
			var structHash [32]byte
			for i := 0; i < 256; i++ {
				structHash = [32]byte(crypto.Keccak256Hash([]byte{byte(i)}))
				digest := HashTypedData(domain, structHash)
				signature, err = crypto.Sign(digest[:], key)
				require.NoError(t, err)
				if signature[64] == parity {
					break
				}
			}
			require.Equal(t, parity, signature[64])
			signature[64] += 27
			before := bytes.Clone(signature)
			data := call(t, ctx, p, RecoverTypedSignerMethod, domain, structHash, signature)
			require.Equal(t, common.LeftPadBytes(wantSigner[:], 32), data)
			require.Equal(t, before, signature)
			highS := bytes.Clone(signature)
			s := new(big.Int).SetBytes(highS[32:64])
			new(big.Int).Sub(crypto.S256().Params().N, s).FillBytes(highS[32:64])
			highS[64] = 55 - highS[64]
			require.Equal(t, make([]byte, 32), call(t, ctx, p, RecoverTypedSignerMethod, domain, structHash, highS))
		})
	}
}

func TestRecoverRefusals(t *testing.T) {
	ctx, _, p := fixture(t)
	domain, structHash := [32]byte{1}, [32]byte{2}
	base := make([]byte, crypto.SignatureLength)
	base[31], base[63], base[64] = 1, 1, 27
	vectors := []struct {
		name      string
		signature []byte
	}{
		{"empty", nil},
		{"short", make([]byte, 64)},
		{"long", make([]byte, 66)},
	}
	for _, v := range []byte{0, 1, 26, 29, 35, 255} {
		sig := bytes.Clone(base)
		sig[64] = v
		vectors = append(vectors, struct {
			name      string
			signature []byte
		}{"v-" + hex.EncodeToString([]byte{v}), sig})
	}
	for _, field := range []struct {
		name   string
		offset int
	}{{"r", 0}, {"s", 32}} {
		for _, value := range []struct {
			name  string
			value *big.Int
		}{{"zero", big.NewInt(0)}, {"order", crypto.S256().Params().N}} {
			sig := bytes.Clone(base)
			value.value.FillBytes(sig[field.offset : field.offset+32])
			vectors = append(vectors, struct {
				name      string
				signature []byte
			}{field.name + "-" + value.name, sig})
		}
	}
	for _, vector := range vectors {
		t.Run(vector.name, func(t *testing.T) {
			before := bytes.Clone(vector.signature)
			require.Equal(t, make([]byte, 32), call(t, ctx, p, RecoverTypedSignerMethod, domain, structHash, vector.signature))
			require.Equal(t, before, vector.signature)
		})
	}
	digest := HashTypedData(domain, structHash)
	failedRecovery := false
	for r := int64(1); r <= 32; r++ {
		sig := bytes.Clone(base)
		big.NewInt(r).FillBytes(sig[:32])
		sig[64] = 0
		_, err := crypto.SigToPub(digest[:], sig)
		if err == nil {
			continue
		}
		require.True(t, crypto.ValidateSignatureValues(0, big.NewInt(r), big.NewInt(1), true))
		sig[64] = 27
		require.Equal(t, make([]byte, 32), call(t, ctx, p, RecoverTypedSignerMethod, domain, structHash, sig))
		failedRecovery = true
		break
	}
	require.True(t, failedRecovery)
}

func TestCapsAndGas(t *testing.T) {
	ctx, _, p := fixture(t)
	for _, vector := range []struct {
		name    string
		version string
	}{
		{strings.Repeat("n", nameCap), strings.Repeat("v", versionCap)},
		{strings.Repeat("\u00e9", nameCap/2), strings.Repeat("\u03c0", versionCap/2)},
	} {
		require.Len(t, call(t, ctx, p, DomainSeparatorMethod, vector.name, vector.version, big.NewInt(125), p.Address()), 32)
	}
	for _, vector := range []struct {
		name    string
		version string
	}{
		{strings.Repeat("n", nameCap+1), "1"},
		{"Paxeer", strings.Repeat("v", versionCap+1)},
		{strings.Repeat("\u00e9", nameCap/2) + "n", "1"},
		{"Paxeer", strings.Repeat("\u03c0", versionCap/2) + "v"},
	} {
		input, err := p.Pack(DomainSeparatorMethod, vector.name, vector.version, big.NewInt(125), p.Address())
		require.NoError(t, err)
		evm := evmContext(ctx)
		data, err := p.Run(evm, common.Address{}, p.Address(), input, nil, true, false, nil)
		require.ErrorIs(t, err, vm.ErrExecutionReverted)
		require.Equal(t, "LXP_AGENT_BATCH_TOO_LARGE: domain name or version exceeds its byte cap", revertText(t, data))
		require.ErrorIs(t, state.GetDBImpl(evm.StateDB).GetPrecompileError(), agentcommon.ErrBatchTooLarge)
	}
	for _, vector := range []struct {
		method string
		args   []interface{}
		want   uint64
	}{
		{HashTypedDataMethod, []interface{}{[32]byte{1}, [32]byte{2}}, 1204},
		{DomainSeparatorMethod, []interface{}{"Paxeer", "1", big.NewInt(125), p.Address()}, 1780},
		{RecoverTypedSignerMethod, []interface{}{[32]byte{1}, [32]byte{2}, make([]byte, 65)}, 4684},
		{RecoverTypedSignerMethod, []interface{}{[32]byte{1}, [32]byte{2}, []byte{}}, 4396},
	} {
		input, err := p.Pack(vector.method, vector.args...)
		require.NoError(t, err)
		require.Equal(t, vector.want, p.RequiredGas(input))
		method := p.Methods[vector.method]
		executor := &Eip712Precompile{}
		require.Equal(t, vector.want, executor.RequiredGas(input[4:], &method))
	}
	for _, input := range [][]byte{nil, {1, 2, 3}, {0xde, 0xad, 0xbe, 0xef}} {
		require.Equal(t, pcommon.UnknownMethodCallGas, p.RequiredGas(input))
	}
}

func TestHeightGate(t *testing.T) {
	ctx, _, p := fixture(t)
	for _, vector := range []struct {
		method string
		args   []interface{}
	}{
		{HashTypedDataMethod, []interface{}{[32]byte{1}, [32]byte{2}}},
		{DomainSeparatorMethod, []interface{}{"Paxeer", "1", big.NewInt(125), p.Address()}},
		{RecoverTypedSignerMethod, []interface{}{[32]byte{1}, [32]byte{2}, []byte{}}},
	} {
		t.Run(vector.method, func(t *testing.T) {
			input, err := p.Pack(vector.method, vector.args...)
			require.NoError(t, err)
			meter := sdk.NewGasMeter(1, 1, 1)
			evm := evmContext(ctx.WithBlockHeight(99).WithGasMeter(meter))
			data, err := p.Run(evm, common.Address{}, p.Address(), input, nil, true, false, nil)
			require.ErrorIs(t, err, vm.ErrExecutionReverted)
			require.Equal(t, "LXP_AGENT_NOT_ACTIVE: merge-plan not active at height 99", revertText(t, data))
			require.ErrorIs(t, state.GetDBImpl(evm.StateDB).GetPrecompileError(), agentcommon.ErrNotActive)
			require.Zero(t, meter.GasConsumed())
			for _, height := range []int64{100, 101} {
				require.Len(t, call(t, ctx.WithBlockHeight(height), p, vector.method, vector.args...), 32)
			}
		})
	}
}

func TestABIAndFrameworkRefusals(t *testing.T) {
	ctx, _, p := fixture(t)
	require.Equal(t, common.HexToAddress(Eip712Address), p.Address())
	require.Equal(t, PrecompileName, p.GetName())
	require.Len(t, p.Methods, 3)
	require.Empty(t, p.Events)
	for _, vector := range []struct {
		method     string
		signature  string
		selector   string
		mutability string
	}{
		{HashTypedDataMethod, "hashTypedData(bytes32,bytes32)", "d61c3c34", "pure"},
		{DomainSeparatorMethod, "domainSeparator(string,string,uint256,address)", "e6d341cd", "pure"},
		{RecoverTypedSignerMethod, "recoverTypedSigner(bytes32,bytes32,bytes)", "ac580b5c", "view"},
	} {
		method := p.Methods[vector.method]
		id, err := hex.DecodeString(vector.selector)
		require.NoError(t, err)
		require.Equal(t, vector.signature, method.Sig)
		require.Equal(t, id, method.ID)
		require.Equal(t, id, crypto.Keccak256([]byte(vector.signature))[:4])
		require.Equal(t, vector.mutability, method.StateMutability)
		lookup, err := p.MethodById(id)
		require.NoError(t, err)
		require.Equal(t, method, *lookup)
	}
	validInput, err := p.Pack(HashTypedDataMethod, [32]byte{1}, [32]byte{2})
	require.NoError(t, err)
	for _, vector := range []struct {
		name  string
		input []byte
		value *big.Int
		code  agentcommon.ErrorCode
		text  string
	}{
		{"short", []byte{1, 2, 3}, nil, agentcommon.ErrInputTooShort, "LXP_PRECOMPILE_INPUT_TOO_SHORT: input too short to extract method ID"},
		{"unknown", []byte{0xde, 0xad, 0xbe, 0xef}, nil, agentcommon.ErrUnknownMethod, "LXP_PRECOMPILE_UNKNOWN_METHOD: no method with id: 0xdeadbeef"},
		{"nonpayable", validInput, big.NewInt(1), agentcommon.ErrNonPayable, "LXP_PRECOMPILE_NON_PAYABLE: sending funds to a non-payable function"},
	} {
		t.Run(vector.name, func(t *testing.T) {
			evm := evmContext(ctx)
			data, err := p.Run(evm, common.Address{}, p.Address(), vector.input, vector.value, true, false, nil)
			require.ErrorIs(t, err, vm.ErrExecutionReverted)
			require.Equal(t, vector.text, revertText(t, data))
			require.ErrorIs(t, state.GetDBImpl(evm.StateDB).GetPrecompileError(), vector.code)
		})
	}
	for _, vector := range []struct {
		method string
		args   []interface{}
	}{
		{HashTypedDataMethod, []interface{}{[32]byte{1}, [32]byte{2}}},
		{DomainSeparatorMethod, []interface{}{"Paxeer", "1", big.NewInt(125), p.Address()}},
		{RecoverTypedSignerMethod, []interface{}{[32]byte{1}, [32]byte{2}, []byte{}}},
	} {
		input, err := p.Pack(vector.method, vector.args...)
		require.NoError(t, err)
		for _, truncated := range [][]byte{input[:4], input[:len(input)-32]} {
			evm := evmContext(ctx)
			data, err := p.Run(evm, common.Address{}, p.Address(), truncated, nil, true, false, nil)
			require.ErrorIs(t, err, vm.ErrExecutionReverted)
			method := p.Methods[vector.method]
			_, decodeErr := method.Inputs.Unpack(truncated[4:])
			require.Error(t, decodeErr)
			require.Equal(t, "LXP_AGENT_ABI_DECODE: "+decodeErr.Error(), revertText(t, data))
			require.ErrorIs(t, state.GetDBImpl(evm.StateDB).GetPrecompileError(), agentcommon.ErrABIDecode)
		}
	}
	for _, evm := range []*vm.EVM{nil, {}, evmContext(sdk.Context{})} {
		data, err := p.Run(evm, common.Address{}, p.Address(), validInput, nil, true, false, nil)
		require.ErrorIs(t, err, vm.ErrExecutionReverted)
		require.Equal(t, "LXP_PRECOMPILE_NO_CONTEXT: cannot get context from EVM", revertText(t, data))
	}
	data, err := p.Run(evmContext(ctx), common.Address{}, p.Address(), validInput, nil, true, true, nil)
	require.NoError(t, err)
	require.Equal(t, call(t, ctx, p, HashTypedDataMethod, [32]byte{1}, [32]byte{2}), data)
}

func TestExecutorValidation(t *testing.T) {
	ctx, upgrade, p := fixture(t)
	ctx = agentcommon.WithHeightGate(ctx, upgrade, planName)
	executor := &Eip712Precompile{abi: p.GetABI(), maxNameBytes: nameCap, maxVersionBytes: versionCap}
	for _, methodName := range []string{HashTypedDataMethod, DomainSeparatorMethod, RecoverTypedSignerMethod} {
		method := p.Methods[methodName]
		for _, count := range []int{0, len(method.Inputs) - 1, len(method.Inputs) + 1} {
			_, err := executor.Execute(ctx, &method, common.Address{}, common.Address{}, make([]interface{}, count), nil, true, nil, nil)
			require.ErrorIs(t, err, agentcommon.ErrArgCount)
		}
		for i := range method.Inputs {
			var args []interface{}
			switch methodName {
			case HashTypedDataMethod:
				args = []interface{}{[32]byte{}, [32]byte{}}
			case DomainSeparatorMethod:
				args = []interface{}{"Paxeer", "1", big.NewInt(125), p.Address()}
			case RecoverTypedSignerMethod:
				args = []interface{}{[32]byte{}, [32]byte{}, []byte{}}
			}
			args[i] = uint64(1)
			_, err := executor.Execute(ctx, &method, common.Address{}, common.Address{}, args, nil, true, nil, nil)
			require.ErrorIs(t, err, agentcommon.ErrABIDecode)
		}
	}
	method := p.Methods[DomainSeparatorMethod]
	for _, chainID := range []*big.Int{nil, big.NewInt(-1), new(big.Int).Lsh(big.NewInt(1), 256)} {
		_, err := executor.Execute(ctx, &method, common.Address{}, common.Address{}, []interface{}{"Paxeer", "1", chainID, p.Address()}, nil, true, nil, nil)
		require.ErrorIs(t, err, agentcommon.ErrABIDecode)
		require.EqualError(t, err, "LXP_AGENT_ABI_DECODE: chainId must be uint256")
	}
	_, err := executor.Execute(ctx, nil, common.Address{}, common.Address{}, nil, nil, true, nil, nil)
	require.ErrorIs(t, err, agentcommon.ErrUnknownMethod)
	unknown := abi.Method{ID: []byte{0xde, 0xad, 0xbe, 0xef}}
	_, err = executor.Execute(ctx, &unknown, common.Address{}, common.Address{}, nil, nil, true, nil, nil)
	require.ErrorIs(t, err, agentcommon.ErrUnknownMethod)
	_, err = executor.Execute(ctx.WithBlockHeight(99), &method, common.Address{}, common.Address{}, nil, nil, true, nil, nil)
	require.ErrorIs(t, err, agentcommon.ErrNotActive)
	_, err = executor.Execute(sdk.Context{}, &method, common.Address{}, common.Address{}, nil, nil, true, nil, nil)
	require.ErrorIs(t, err, agentcommon.ErrNoContext)
}

func TestConstructorConfiguration(t *testing.T) {
	_, upgrade, _ := fixture(t)
	for _, vector := range []struct {
		upgrade *upgradekeeper.Keeper
		plan    string
		name    int
		version int
		code    agentcommon.ErrorCode
	}{
		{nil, planName, nameCap, versionCap, agentcommon.ErrNotActive},
		{upgrade, "", nameCap, versionCap, agentcommon.ErrNotActive},
		{upgrade, planName, 0, versionCap, agentcommon.ErrABIDecode},
		{upgrade, planName, nameCap, 0, agentcommon.ErrABIDecode},
		{upgrade, planName, -1, versionCap, agentcommon.ErrABIDecode},
		{upgrade, planName, nameCap, -1, agentcommon.ErrABIDecode},
	} {
		p, err := NewPrecompile(vector.upgrade, vector.plan, vector.name, vector.version)
		require.Nil(t, p)
		require.ErrorIs(t, err, vector.code)
	}
}
