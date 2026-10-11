package addr_test

import (
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"testing"

	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/addr"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestAssociatePubKey(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	pre, _ := addr.NewPrecompile(testApp.GetPrecompileKeepers())
	associatePubKey, err := pre.ABI.MethodById(pre.GetExecutor().(*addr.PrecompileExecutor).AssociatePubKeyID)

	// Target refers to the address that the caller is trying to associate.
	targetPrivKey := testkeeper.MockPrivateKey()
	targetPubKey := targetPrivKey.PubKey()
	targetPubKeyHex := hex.EncodeToString(targetPubKey.Bytes())
	targetPaxAddress, targetEvmAddress := testkeeper.PrivateKeyToAddresses(targetPrivKey)

	// Caller refers to the party calling the precompile.
	callerPrivKey := testkeeper.MockPrivateKey()
	callerPaxAddress, callerEvmAddress := testkeeper.PrivateKeyToAddresses(callerPrivKey)
	callerPubKey := callerPrivKey.PubKey()
	callerPubKeyHex := hex.EncodeToString(callerPubKey.Bytes())

	// Associate these addresses, so we can use them to test the case where addresses are already associated association.
	k.SetAddressMapping(ctx, callerPaxAddress, callerEvmAddress)

	require.Nil(t, err)

	happyPathOutput, _ := associatePubKey.Outputs.Pack(targetPaxAddress.String(), targetEvmAddress)

	type args struct {
		evm         *vm.EVM
		caller      common.Address
		pubKey      string
		value       *big.Int
		readOnly    bool
		suppliedGas uint64
	}
	tests := []struct {
		name       string
		args       args
		wantRet    []byte
		wantErr    bool
		wantErrMsg string
		wrongRet   bool
	}{
		{
			name: "fails if payable",
			args: args{
				evm: &vm.EVM{
					StateDB:   state.NewDBImpl(ctx, k, true),
					TxContext: vm.TxContext{Origin: callerEvmAddress},
				},
				caller: callerEvmAddress,
				pubKey: targetPubKeyHex,
				value:  big.NewInt(10),
			},
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
		{
			name: "fails on static call",
			args: args{
				evm: &vm.EVM{
					StateDB:   state.NewDBImpl(ctx, k, true),
					TxContext: vm.TxContext{Origin: callerEvmAddress},
				},
				caller:   callerEvmAddress,
				pubKey:   targetPubKeyHex,
				value:    big.NewInt(10),
				readOnly: true,
			},
			wantErr:    true,
			wantErrMsg: "cannot call associate pub key precompile from staticcall",
		},
		{
			name: "fails if input is appended with 0x",
			args: args{
				evm: &vm.EVM{
					StateDB:   state.NewDBImpl(ctx, k, true),
					TxContext: vm.TxContext{Origin: callerEvmAddress},
				},
				caller: callerEvmAddress,
				pubKey: fmt.Sprintf("0x%v", targetPubKeyHex),
				value:  big.NewInt(0),
			},
			wantErr:    true,
			wantErrMsg: "encoding/hex: invalid byte: U+0078 'x'",
		},
		{
			name: "fails if addresses are already associated",
			args: args{
				evm: &vm.EVM{
					StateDB:   state.NewDBImpl(ctx, k, true),
					TxContext: vm.TxContext{Origin: callerEvmAddress},
				},
				caller: callerEvmAddress,
				pubKey: callerPubKeyHex,
				value:  big.NewInt(0),
			},
			wantErr:    true,
			wantErrMsg: fmt.Sprintf("address %s is already associated with evm address %s", callerPaxAddress, callerEvmAddress),
		},
		{
			name: "fails if insufficient gas provided",
			args: args{
				evm: &vm.EVM{
					StateDB:   state.NewDBImpl(ctx, k, true),
					TxContext: vm.TxContext{Origin: callerEvmAddress},
				},
				caller:      callerEvmAddress,
				pubKey:      targetPubKeyHex,
				value:       big.NewInt(0),
				suppliedGas: 1,
			},
			wantErr:    true,
			wantErrMsg: "execution reverted: {ReadFlat}",
		},
		{
			name: "happy path - associates addresses if signature is correct",
			args: args{
				evm: &vm.EVM{
					StateDB:   state.NewDBImpl(ctx, k, true),
					TxContext: vm.TxContext{Origin: callerEvmAddress},
				},
				caller: callerEvmAddress,
				pubKey: targetPubKeyHex,
				value:  big.NewInt(0),
			},
			wantRet: happyPathOutput,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create the precompile and inputs
			p, _ := addr.NewPrecompile(testApp.GetPrecompileKeepers())
			require.Nil(t, err)
			inputs, err := associatePubKey.Inputs.Pack(tt.args.pubKey)
			require.Nil(t, err)

			// Make the call to associate.
			suppliedGas := uint64(40000)
			if tt.args.suppliedGas != 0 {
				suppliedGas = tt.args.suppliedGas
			}
			ret, _, err := p.RunAndCalculateGas(tt.args.evm, tt.args.caller, tt.args.caller, append(p.GetExecutor().(*addr.PrecompileExecutor).AssociatePubKeyID, inputs...), suppliedGas, tt.args.value, nil, tt.args.readOnly, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v %v", err, tt.wantErr, string(ret))
				return
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Nil(t, ret)
			} else if tt.wrongRet {
				// tt.wrongRet is set if we expect a return value that's different from the happy path. This means that the wrong addresses were associated.
				require.NotEqual(t, tt.wantRet, ret)
			} else {
				require.Equal(t, tt.wantRet, ret)
			}
		})
	}
}

func TestAssociate(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	pre, _ := addr.NewPrecompile(testApp.GetPrecompileKeepers())
	associate, err := pre.ABI.MethodById(pre.GetExecutor().(*addr.PrecompileExecutor).AssociateID)
	require.Nil(t, err)

	// Target refers to the address that the caller is trying to associate.
	targetPrivKey := testkeeper.MockPrivateKey()
	targetPrivHex := hex.EncodeToString(targetPrivKey.Bytes())
	targetPaxAddress, targetEvmAddress := testkeeper.PrivateKeyToAddresses(targetPrivKey)
	targetKey, _ := crypto.HexToECDSA(targetPrivHex)

	// Create the inputs
	emptyData := make([]byte, 32)
	prefixedMessage := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(emptyData)) + string(emptyData)
	hash := crypto.Keccak256Hash([]byte(prefixedMessage))
	sig, err := crypto.Sign(hash.Bytes(), targetKey)
	require.Nil(t, err)

	r := fmt.Sprintf("0x%v", new(big.Int).SetBytes(sig[:32]).Text(16))
	s := fmt.Sprintf("0x%v", new(big.Int).SetBytes(sig[32:64]).Text(16))
	v := fmt.Sprintf("0x%v", new(big.Int).SetBytes([]byte{sig[64]}).Text(16))

	// Caller refers to the party calling the precompile.
	callerPrivKey := testkeeper.MockPrivateKey()
	callerPaxAddress, callerEvmAddress := testkeeper.PrivateKeyToAddresses(callerPrivKey)
	callerPrivHex := hex.EncodeToString(callerPrivKey.Bytes())
	callerKey, _ := crypto.HexToECDSA(callerPrivHex)

	// Associate these addresses, so we can use them to test the case where addresses are already associated association.
	k.SetAddressMapping(ctx, callerPaxAddress, callerEvmAddress)
	callerSig, err := crypto.Sign(hash.Bytes(), callerKey)
	callerR := fmt.Sprintf("0x%v", new(big.Int).SetBytes(callerSig[:32]).Text(16))
	callerS := fmt.Sprintf("0x%v", new(big.Int).SetBytes(callerSig[32:64]).Text(16))
	callerV := fmt.Sprintf("0x%v", new(big.Int).SetBytes([]byte{callerSig[64]}).Text(16))

	happyPathOutput, _ := associate.Outputs.Pack(targetPaxAddress.String(), targetEvmAddress)

	type args struct {
		evm      *vm.EVM
		caller   common.Address
		v        string
		r        string
		s        string
		msg      string
		value    *big.Int
		readOnly bool
	}
	tests := []struct {
		name       string
		args       args
		wantRet    []byte
		wantErr    bool
		wantErrMsg string
		wrongRet   bool
	}{
		{
			name: "fails if payable",
			args: args{
				evm: &vm.EVM{
					StateDB:   state.NewDBImpl(ctx, k, true),
					TxContext: vm.TxContext{Origin: callerEvmAddress},
				},
				caller: callerEvmAddress,
				v:      v,
				r:      r,
				s:      s,
				msg:    prefixedMessage,
				value:  big.NewInt(10),
			},
			wantErr:    true,
			wantErrMsg: "sending funds to a non-payable function",
		},
		{
			name: "fails on static calls",
			args: args{
				evm: &vm.EVM{
					StateDB:   state.NewDBImpl(ctx, k, true),
					TxContext: vm.TxContext{Origin: callerEvmAddress},
				},
				caller:   callerEvmAddress,
				v:        v,
				r:        r,
				s:        s,
				msg:      prefixedMessage,
				value:    big.NewInt(10),
				readOnly: true,
			},
			wantErr:    true,
			wantErrMsg: "cannot call associate precompile from staticcall",
		},
		{
			name: "fails if input is not hex",
			args: args{
				evm: &vm.EVM{
					StateDB:   state.NewDBImpl(ctx, k, true),
					TxContext: vm.TxContext{Origin: callerEvmAddress},
				},
				caller: callerEvmAddress,
				v:      "nothex",
				r:      r,
				s:      s,
				msg:    prefixedMessage,
				value:  big.NewInt(0),
			},
			wantErr:    true,
			wantErrMsg: "encoding/hex: invalid byte: U+006E 'n'",
		},
		{
			name: "fails if addresses are already associated",
			args: args{
				evm: &vm.EVM{
					StateDB:   state.NewDBImpl(ctx, k, true),
					TxContext: vm.TxContext{Origin: callerEvmAddress},
				},
				caller: callerEvmAddress,
				v:      callerV,
				r:      callerR,
				s:      callerS,
				msg:    prefixedMessage,
				value:  big.NewInt(0),
			},
			wantErr:    true,
			wantErrMsg: fmt.Sprintf("address %s is already associated with evm address %s", callerPaxAddress, callerEvmAddress),
		},
		{
			name: "associates wrong address if invalid signature (different message)",
			args: args{
				evm: &vm.EVM{
					StateDB:   state.NewDBImpl(ctx, k, true),
					TxContext: vm.TxContext{Origin: callerEvmAddress},
				},
				caller: callerEvmAddress,
				v:      v,
				r:      r,
				s:      s, // Pass in r instead of s here for invalid value
				msg:    "Not the signed message",
				value:  big.NewInt(0),
			},
			wantRet:  happyPathOutput,
			wrongRet: true,
		},
		{
			name: "happy path - associates addresses if signature is correct",
			args: args{
				evm: &vm.EVM{
					StateDB:   state.NewDBImpl(ctx, k, true),
					TxContext: vm.TxContext{Origin: callerEvmAddress},
				},
				caller: callerEvmAddress,
				v:      v,
				r:      r,
				s:      s,
				msg:    prefixedMessage,
				value:  big.NewInt(0),
			},
			wantRet: happyPathOutput,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create the precompile and inputs
			p, _ := addr.NewPrecompile(testApp.GetPrecompileKeepers())
			require.Nil(t, err)
			inputs, err := associate.Inputs.Pack(tt.args.v, tt.args.r, tt.args.s, tt.args.msg)
			require.Nil(t, err)

			// Make the call to associate.
			ret, _, err := p.RunAndCalculateGas(tt.args.evm, tt.args.caller, tt.args.caller, append(p.GetExecutor().(*addr.PrecompileExecutor).AssociateID, inputs...), 40000, tt.args.value, nil, tt.args.readOnly, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v %v", err, tt.wantErr, string(ret))
				return
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Nil(t, ret)
			} else if tt.wrongRet {
				// tt.wrongRet is set if we expect a return value that's different from the happy path. This means that the wrong addresses were associated.
				require.NotEqual(t, tt.wantRet, ret)
			} else {
				require.Equal(t, tt.wantRet, ret)
			}
		})
	}
}

func TestGetAddr(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	pre, _ := addr.NewPrecompile(testApp.GetPrecompileKeepers())
	getPaxAddr, err := pre.ABI.MethodById(pre.GetExecutor().(*addr.PrecompileExecutor).GetPaxAddressID)
	require.Nil(t, err)
	getEvmAddr, err := pre.ABI.MethodById(pre.GetExecutor().(*addr.PrecompileExecutor).GetEvmAddressID)
	require.Nil(t, err)

	paxAddr, evmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, paxAddr, evmAddr)

	stateDB := &state.DBImpl{}
	stateDB.WithCtx(ctx)

	getPaxAddrBz, err := getPaxAddr.Inputs.Pack(evmAddr)
	require.Nil(t, err)
	res, _, err := pre.RunAndCalculateGas(&vm.EVM{StateDB: stateDB}, evmAddr, evmAddr, append(getPaxAddr.ID, getPaxAddrBz...), 20000, common.Big0, nil, true, false)
	require.Nil(t, err)
	unpacked, err := getPaxAddr.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, 1, len(unpacked))
	require.Equal(t, paxAddr.String(), unpacked[0].(string))

	getEvmAddrBz, err := getEvmAddr.Inputs.Pack(paxAddr.String())
	require.Nil(t, err)
	res, _, err = pre.RunAndCalculateGas(&vm.EVM{StateDB: stateDB}, evmAddr, evmAddr, append(getEvmAddr.ID, getEvmAddrBz...), 20000, common.Big0, nil, true, false)
	require.Nil(t, err)
	unpacked, err = getEvmAddr.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, 1, len(unpacked))
	require.Equal(t, evmAddr, unpacked[0].(common.Address))
}

func TestAddrRevertReasons(t *testing.T) {
	const planHeight = int64(100)
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(planHeight - 1).WithChainID("layerx-binding-test")
	ctx = ctx.WithMultiStore(ctx.MultiStore().CacheMultiStore())
	k := &testApp.EvmKeeper
	p, err := addr.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	chainID := k.ChainID(ctx)
	sign := func(private ed25519.PrivateKey, evmAddress common.Address, nonce uint64) []byte {
		message, err := types.LayerXBindMessage(chainID, evmAddress, nonce)
		require.NoError(t, err)
		return ed25519.Sign(private, message)
	}

	// An associated key, for the already-associated refusal.
	associatedKey := testkeeper.MockPrivateKey()
	associatedPax, associatedEvm := testkeeper.PrivateKeyToAddresses(associatedKey)
	k.SetAddressMapping(ctx, associatedPax, associatedEvm)
	associatedPubKey := hex.EncodeToString(associatedKey.PubKey().Bytes())

	// A bound address, for the one-to-one refusals.
	_, boundEvm := testkeeper.MockAddressPair()
	boundDid, boundPrivate := newDidKey(t)
	var boundSignature [64]byte
	copy(boundSignature[:], sign(boundPrivate, boundEvm, 0))
	nonce, err := k.BindLayerX(ctx, boundEvm, boundDid, boundSignature)
	require.NoError(t, err)
	require.Equal(t, uint64(0), nonce)

	// An address whose bind nonce is used up.
	_, exhaustedEvm := testkeeper.MockAddressPair()
	ctx.KVStore(k.GetStoreKey()).Set(types.LayerXBindNonceKey(exhaustedEvm), binary.BigEndian.AppendUint64(nil, math.MaxUint64))

	unknownPax, unknownEvm := testkeeper.MockAddressPair()
	_, thief := testkeeper.MockAddressPair()
	freshDid, freshPrivate := newDidKey(t)
	nonCanonical := [32]byte{0xed}
	for i := 1; i < 31; i++ {
		nonCanonical[i] = 0xff
	}
	nonCanonical[31] = 0x7f

	k.UpgradeKeeper().SetDone(ctx.WithBlockHeight(planHeight), pcommon.MergeUpgrade)

	tests := []struct {
		name     string
		caller   common.Address
		readOnly bool
		delegate bool
		gas      uint64
		method   string
		args     []interface{}
		today    string
		code     string
		detail   string
		reason   string
		keeper   error
	}{
		{
			name: "panic", caller: unknownEvm, gas: 1, method: addr.AssociatePubKey, args: []interface{}{associatedPubKey},
			today: "execution reverted: {ReadFlat}", code: "LXP_ADDR_PANIC", detail: "{ReadFlat}",
			reason: "LXP_ADDR_PANIC: execution reverted: <recovered value>; {ReadFlat}",
		},
		{
			name: "associate staticcall", caller: unknownEvm, readOnly: true, method: addr.Associate, args: []interface{}{"", "", "", ""},
			today: "cannot call associate precompile from staticcall", code: "LXP_ADDR_STATICCALL", detail: addr.Associate,
			reason: "LXP_ADDR_STATICCALL: cannot call <method> from staticcall; associate",
		},
		{
			name: "associatePubKey staticcall", caller: unknownEvm, readOnly: true, method: addr.AssociatePubKey, args: []interface{}{associatedPubKey},
			today: "cannot call associate pub key precompile from staticcall", code: "LXP_ADDR_STATICCALL", detail: addr.AssociatePubKey,
			reason: "LXP_ADDR_STATICCALL: cannot call <method> from staticcall; associatePubKey",
		},
		{
			name: "bindLayerX staticcall", caller: unknownEvm, readOnly: true, method: addr.BindLayerXMethod, args: []interface{}{freshDid, sign(freshPrivate, unknownEvm, 0)},
			today: "cannot call bindLayerX from staticcall", code: "LXP_ADDR_STATICCALL", detail: addr.BindLayerXMethod,
			reason: "LXP_ADDR_STATICCALL: cannot call <method> from staticcall; bindLayerX",
		},
		{
			name: "unbindLayerX staticcall", caller: boundEvm, readOnly: true, method: addr.UnbindLayerXMethod,
			today: "cannot call unbindLayerX from staticcall", code: "LXP_ADDR_STATICCALL", detail: addr.UnbindLayerXMethod,
			reason: "LXP_ADDR_STATICCALL: cannot call <method> from staticcall; unbindLayerX",
		},
		{
			name: "bindLayerX delegatecall", caller: unknownEvm, delegate: true, method: addr.BindLayerXMethod, args: []interface{}{freshDid, sign(freshPrivate, unknownEvm, 0)},
			today: "cannot delegatecall bindLayerX", code: "LXP_ADDR_DELEGATECALL", detail: addr.BindLayerXMethod,
			reason: "LXP_ADDR_DELEGATECALL: cannot delegatecall <method>; bindLayerX",
		},
		{
			name: "unbindLayerX delegatecall", caller: boundEvm, delegate: true, method: addr.UnbindLayerXMethod,
			today: "cannot delegatecall unbindLayerX", code: "LXP_ADDR_DELEGATECALL", detail: addr.UnbindLayerXMethod,
			reason: "LXP_ADDR_DELEGATECALL: cannot delegatecall <method>; unbindLayerX",
		},
		{
			name: "evm address not associated", caller: unknownEvm, method: addr.GetPaxAddressMethod, args: []interface{}{unknownEvm},
			today: fmt.Sprintf("EVM address %s is not associated", unknownEvm.Hex()), code: "LXP_ADDR_EVM_NOT_ASSOCIATED", detail: unknownEvm.Hex(),
			reason: "LXP_ADDR_EVM_NOT_ASSOCIATED: EVM address is not associated; " + unknownEvm.Hex(),
		},
		{
			name: "pax address not associated", caller: unknownEvm, method: addr.GetEvmAddressMethod, args: []interface{}{unknownPax.String()},
			today: fmt.Sprintf("pax address %s is not associated", unknownPax.String()), code: "LXP_ADDR_PAX_NOT_ASSOCIATED", detail: unknownPax.String(),
			reason: "LXP_ADDR_PAX_NOT_ASSOCIATED: pax address is not associated; " + unknownPax.String(),
		},
		{
			name: "already associated", caller: unknownEvm, method: addr.AssociatePubKey, args: []interface{}{associatedPubKey},
			today: fmt.Sprintf("address %s is already associated with evm address %s", associatedPax, associatedEvm), code: "LXP_ADDR_ALREADY_ASSOCIATED", detail: associatedPax.String(),
			reason: "LXP_ADDR_ALREADY_ASSOCIATED: address is already associated with evm address; " + associatedPax.String(),
		},
		{
			name: "evm address not bound", caller: unknownEvm, method: addr.GetLayerXDidMethod, args: []interface{}{unknownEvm},
			today: fmt.Sprintf("EVM address %s is not bound to a LayerX DID", unknownEvm.Hex()), code: "LXP_ADDR_EVM_NOT_BOUND", detail: unknownEvm.Hex(),
			reason: "LXP_ADDR_EVM_NOT_BOUND: EVM address is not bound to a LayerX DID; " + unknownEvm.Hex(),
		},
		{
			name: "did not bound", caller: unknownEvm, method: addr.GetEvmAddrByLayerXMethod, args: []interface{}{freshDid},
			today: fmt.Sprintf("LayerX DID %s is not bound to an EVM address", types.LayerXDid(freshDid)), code: "LXP_ADDR_DID_NOT_BOUND", detail: types.LayerXDid(freshDid),
			reason: "LXP_ADDR_DID_NOT_BOUND: LayerX DID is not bound to an EVM address; " + types.LayerXDid(freshDid),
		},
		{
			name: "signature length", caller: unknownEvm, method: addr.BindLayerXMethod, args: []interface{}{freshDid, sign(freshPrivate, unknownEvm, 0)[:63]},
			today: types.ErrLayerXSignatureLength.Error(), code: "LXP_ADDR_LAYERX_SIGNATURE_LENGTH",
			reason: "LXP_ADDR_LAYERX_SIGNATURE_LENGTH: layerx binding: signature must be 64 bytes", keeper: types.ErrLayerXSignatureLength,
		},
		{
			name: "signature refused", caller: thief, method: addr.BindLayerXMethod, args: []interface{}{freshDid, sign(freshPrivate, unknownEvm, 0)},
			today: types.ErrLayerXSignature.Error(), code: "LXP_ADDR_LAYERX_SIGNATURE",
			reason: "LXP_ADDR_LAYERX_SIGNATURE: layerx binding: signature refused", keeper: types.ErrLayerXSignature,
		},
		{
			name: "address bound", caller: boundEvm, method: addr.BindLayerXMethod, args: []interface{}{freshDid, sign(freshPrivate, boundEvm, 1)},
			today: types.ErrLayerXAddressBound.Error(), code: "LXP_ADDR_LAYERX_ADDRESS_BOUND",
			reason: "LXP_ADDR_LAYERX_ADDRESS_BOUND: layerx binding: evm address is already bound to a did", keeper: types.ErrLayerXAddressBound,
		},
		{
			name: "did bound", caller: unknownEvm, method: addr.BindLayerXMethod, args: []interface{}{boundDid, sign(boundPrivate, unknownEvm, 0)},
			today: types.ErrLayerXDidBound.Error(), code: "LXP_ADDR_LAYERX_DID_BOUND",
			reason: "LXP_ADDR_LAYERX_DID_BOUND: layerx binding: did is already bound to an evm address", keeper: types.ErrLayerXDidBound,
		},
		{
			name: "not bound", caller: unknownEvm, method: addr.UnbindLayerXMethod,
			today: types.ErrLayerXNotBound.Error(), code: "LXP_ADDR_LAYERX_NOT_BOUND",
			reason: "LXP_ADDR_LAYERX_NOT_BOUND: layerx binding: evm address is not bound to a did", keeper: types.ErrLayerXNotBound,
		},
		{
			name: "nonce exhausted", caller: exhaustedEvm, method: addr.BindLayerXMethod, args: []interface{}{freshDid, sign(freshPrivate, exhaustedEvm, math.MaxUint64)},
			today: types.ErrLayerXNonceExhausted.Error(), code: "LXP_ADDR_LAYERX_NONCE_EXHAUSTED",
			reason: "LXP_ADDR_LAYERX_NONCE_EXHAUSTED: layerx binding: nonce exhausted", keeper: types.ErrLayerXNonceExhausted,
		},
		{
			name: "non canonical key", caller: unknownEvm, method: addr.BindLayerXMethod, args: []interface{}{nonCanonical, sign(freshPrivate, unknownEvm, 0)},
			today: types.ErrLayerXNonCanonicalKey.Error(), code: "LXP_ADDR_LAYERX_NON_CANONICAL_KEY",
			reason: "LXP_ADDR_LAYERX_NON_CANONICAL_KEY: layerx binding: did public key is not canonical", keeper: types.ErrLayerXNonCanonicalKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, err := p.ABI.Pack(tt.method, tt.args...)
			require.NoError(t, err)
			gas := tt.gas
			if gas == 0 {
				gas = 200000
			}
			run := func(height int64) ([]byte, error) {
				stateDB := state.NewDBImpl(ctx.WithBlockHeight(height), k, false)
				ret, _, err := p.RunAndCalculateGas(&vm.EVM{StateDB: stateDB}, tt.caller, tt.caller, input, gas, big.NewInt(0), nil, tt.readOnly, tt.delegate)
				require.Equal(t, vm.ErrExecutionReverted, err)
				return ret, stateDB.GetPrecompileError()
			}

			// Below the plan height the refusal keeps today's bytes and text.
			ret, refusal := run(planHeight - 1)
			require.Nil(t, ret)
			require.EqualError(t, refusal, tt.today)
			if tt.keeper != nil {
				require.Equal(t, tt.keeper, refusal)
			}

			// From the plan height it is Error(string) with its LXP_ADDR_ code.
			ret, refusal = run(planHeight)
			require.Equal(t, pcommon.LXPRevert(tt.code, tt.detail), ret)
			require.Equal(t, []byte{0x08, 0xc3, 0x79, 0xa0}, ret[:4])
			reason, err := abi.UnpackRevert(ret)
			require.NoError(t, err)
			require.Equal(t, tt.reason, reason)
			require.EqualError(t, refusal, tt.reason)
			if tt.keeper != nil {
				require.ErrorIs(t, refusal, tt.keeper)
			}
		})
	}
}
