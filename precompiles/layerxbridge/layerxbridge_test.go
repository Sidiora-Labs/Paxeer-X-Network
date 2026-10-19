package layerxbridge_test

import (
	"errors"
	"math/big"
	"testing"
	"time"

	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	bridgekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/keeper"
	bridgetestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxbridge"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

const chainID = uint64(1)

var (
	vault     = common.HexToAddress("0x1111111111111111111111111111111111111111")
	asset     = common.HexToAddress("0x4444444444444444444444444444444444444444")
	remote    = common.HexToAddress("0x3333333333333333333333333333333333333333")
	bridge    = common.HexToAddress(layerxbridge.BridgeAddress)
	authority = types.DefaultAuthority()
)

// keepers are the application's precompile keepers plus the bridge keeper,
// as the application provides them once the bridge is wired.
type keepers struct {
	utils.Keepers
	bridge *bridgekeeper.Keeper
}

func (k keepers) LayerXBridgeK() *bridgekeeper.Keeper { return k.bridge }

// harness drives the precompile through Run, the EVM entry point, against the
// real application keepers on a branch of the test app's state.
type harness struct {
	t          *testing.T
	ctx        sdk.Context
	stateDB    *state.DBImpl
	evm        *vm.EVM
	precompile *pcommon.Precompile
	keeper     bridgekeeper.Keeper
	attestors  []bridgetestutil.Attestor
	caller     common.Address
	callerAcc  sdk.AccAddress
	denom      string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx, _ := app.NewContext(false, tmtypes.Header{}).WithBlockHeight(11).WithBlockTime(time.Unix(1_800_000_000, 0)).CacheContext()
	k, ctx := bridgetestutil.NewKeeper(app, ctx)
	h := &harness{t: t, keeper: k, attestors: bridgetestutil.Attestors(3), denom: types.Denom(chainID, types.Address20(asset))}
	h.callerAcc, h.caller = testkeeper.MockAddressPair()
	app.EvmKeeper.SetAddressMapping(ctx, h.callerAcc, h.caller)
	k.InitGenesis(ctx, *types.DefaultGenesis())
	var err error
	h.precompile, err = layerxbridge.NewPrecompile(keepers{Keepers: app.GetPrecompileKeepers(), bridge: &h.keeper})
	require.NoError(t, err)
	h.at(ctx)
	return h
}

// activate registers chain 1, three attestors with threshold two and caps of
// 1000 per transaction and 1500 in flight.
func (h *harness) activate() {
	h.t.Helper()
	require.NoError(h.t, h.keeper.RegisterChain(h.ctx, types.MsgRegisterChain{Authority: authority,
		Chain: types.Chain{ChainID: chainID, Vault: types.Address20(vault), FinalityDepth: 64, Enabled: true}}))
	require.NoError(h.t, h.keeper.SetAttestors(h.ctx, types.MsgSetAttestors{Authority: authority,
		Set: bridgetestutil.Set(h.attestors, 1_000_000, 2)}))
	require.NoError(h.t, h.keeper.SetCap(h.ctx, types.MsgSetCap{Authority: authority, ChainID: chainID,
		Asset: types.Address20(asset), MaxInFlight: sdk.NewInt(1500), MaxPerTx: sdk.NewInt(1000)}))
}

// at rebuilds the EVM state on ctx, as a new transaction would.
func (h *harness) at(ctx sdk.Context) {
	h.stateDB = state.NewDBImpl(ctx.WithEventManager(sdk.NewEventManager()), &testkeeper.EVMTestApp.EvmKeeper, true)
	h.evm = &vm.EVM{StateDB: h.stateDB, TxContext: vm.TxContext{Origin: h.caller}}
	h.ctx = h.stateDB.Ctx()
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

func (h *harness) run(name string, value *big.Int, readOnly, delegate bool, args ...interface{}) ([]interface{}, error) {
	h.t.Helper()
	res, err := h.precompile.Run(h.evm, h.caller, h.caller, h.input(name, args...), value, readOnly, delegate, nil)
	if err != nil {
		require.ErrorIs(h.t, err, vm.ErrExecutionReverted)
		reason, unpackErr := abi.UnpackRevert(res)
		require.NoError(h.t, unpackErr)
		require.NotEmpty(h.t, reason)
		return nil, err
	}
	out, err := h.method(name).Outputs.Unpack(res)
	require.NoError(h.t, err)
	return out, nil
}

func (h *harness) call(name string, args ...interface{}) ([]interface{}, error) {
	return h.run(name, nil, false, false, args...)
}

func (h *harness) view(name string, args ...interface{}) []interface{} {
	h.t.Helper()
	out, err := h.run(name, nil, true, false, args...)
	require.NoError(h.t, err)
	return out
}

func (h *harness) balance() sdk.Int {
	return testkeeper.EVMTestApp.BankKeeper.GetBalance(h.ctx, h.callerAcc, h.denom).Amount
}

func (h *harness) logs(signature string) []*ethtypes.Log {
	var out []*ethtypes.Log
	for _, log := range h.stateDB.GetAllLogs() {
		if log.Topics[0] == crypto.Keccak256Hash([]byte(signature)) {
			require.Equal(h.t, bridge, log.Address)
			out = append(out, log)
		}
	}
	return out
}

func (h *harness) events(kind string) int {
	count := 0
	for _, event := range h.stateDB.Ctx().EventManager().Events() {
		if event.Type == kind {
			count++
		}
	}
	return count
}

// bridgeInArgs are the bridgeIn arguments for a deposit to the caller, signed
// by signers.
func (h *harness) bridgeInArgs(logIndex uint64, amount int64, signers ...bridgetestutil.Attestor) []interface{} {
	recipient := common.BytesToHash(h.caller.Bytes())
	txHash := common.Hash{0xde, 0xad, byte(logIndex)}
	in := types.BridgeIn{ChainID: chainID, Vault: types.Address20(vault), TxHash: types.Hash32(txHash),
		LogIndex: logIndex, Recipient: types.Hash32(recipient), Asset: types.Address20(asset), Amount: big.NewInt(amount)}
	return []interface{}{chainID, vault, [32]byte(txHash), logIndex, [32]byte(recipient), asset, big.NewInt(amount),
		bridgetestutil.Sign(types.InboundDigest(in), signers...)}
}

func TestBridgeInMintsAtThresholdThroughThePrecompile(t *testing.T) {
	h := newHarness(t)
	require.Equal(t, false, h.view(layerxbridge.GetChainMethod, chainID)[0], "dormant: nothing registered")
	require.Equal(t, false, h.view(layerxbridge.IsPausedMethod)[0])
	_, err := h.call(layerxbridge.BridgeInMethod, h.bridgeInArgs(7, 600, h.attestors[0], h.attestors[1])...)
	require.Error(t, err, "dormant bridge refuses")

	h.activate()
	out, err := h.call(layerxbridge.BridgeInMethod, h.bridgeInArgs(7, 600, h.attestors[0], h.attestors[1])...)
	require.NoError(t, err)
	require.Equal(t, h.denom, out[0])
	require.Equal(t, sdk.NewInt(600), h.balance())

	logs := h.logs("BridgeIn(uint64,bytes32,address,uint64,address,uint256,string)")
	require.Len(t, logs, 1)
	require.Equal(t, common.BigToHash(big.NewInt(1)), logs[0].Topics[1])
	require.Equal(t, common.Hash{0xde, 0xad, 7}, logs[0].Topics[2])
	require.Equal(t, common.BytesToHash(h.caller.Bytes()), logs[0].Topics[3])
	data, err := h.precompile.GetABI().Events[layerxbridge.BridgeInEvent].Inputs.NonIndexed().Unpack(logs[0].Data)
	require.NoError(t, err)
	require.Equal(t, []interface{}{uint64(7), asset, big.NewInt(600), h.denom}, data)
	require.Equal(t, 1, h.events(types.EventBridgeIn))

	require.Equal(t, true, h.view(layerxbridge.IsNullifiedMethod, chainID, [32]byte{0xde, 0xad, 7}, uint64(7))[0])
	require.Equal(t, []interface{}{h.denom, big.NewInt(1500), big.NewInt(1000), big.NewInt(600)},
		h.view(layerxbridge.GetCapMethod, chainID, asset))
	require.Equal(t, []interface{}{true, vault, uint64(64), true}, h.view(layerxbridge.GetChainMethod, chainID))
	attestors := h.view(layerxbridge.GetAttestorsMethod)
	require.Equal(t, []common.Address{common.Address(h.attestors[0].Signer), common.Address(h.attestors[1].Signer),
		common.Address(h.attestors[2].Signer)}, attestors[0])
	require.Equal(t, []*big.Int{big.NewInt(1_000_000), big.NewInt(1_000_000), big.NewInt(1_000_000)}, attestors[1])
	require.Equal(t, uint32(2), attestors[2])
}

func TestBridgeInRefusalsThroughThePrecompile(t *testing.T) {
	h := newHarness(t)
	h.activate()
	testkeeper.EVMTestApp.EvmKeeper.UpgradeKeeper().SetDone(h.ctx, pcommon.MergeUpgrade)
	require.NoError(t, h.keeper.RegisterChain(h.ctx, types.MsgRegisterChain{Authority: authority,
		Chain: types.Chain{ChainID: 2, Vault: types.Address20(vault), FinalityDepth: 64, Enabled: false}}))

	unregistered := h.bridgeInArgs(1, 100, h.attestors[0], h.attestors[1])
	unregistered[0] = uint64(9)
	disabled := h.bridgeInArgs(1, 100, h.attestors[0], h.attestors[1])
	disabled[0] = uint64(2)
	wrongVault := h.bridgeInArgs(1, 100, h.attestors[0], h.attestors[1])
	wrongVault[1] = remote
	unknownAsset := h.bridgeInArgs(1, 100, h.attestors[0], h.attestors[1])
	unknownAsset[5] = remote
	zeroRecipient := h.bridgeInArgs(1, 100, h.attestors[0], h.attestors[1])
	zeroRecipient[4] = [32]byte{}
	unpaddedRecipient := h.bridgeInArgs(1, 100, h.attestors[0], h.attestors[1])
	unpaddedRecipient[4] = [32]byte{1}
	oversizedAmount := h.bridgeInArgs(1, 100, h.attestors[0], h.attestors[1])
	oversizedAmount[6] = new(big.Int).Lsh(big.NewInt(1), 255)
	shortSignature := h.bridgeInArgs(1, 100, h.attestors[0], h.attestors[1])
	shortSignature[7].([][]byte)[0] = shortSignature[7].([][]byte)[0][:types.SignatureLength-1]
	wrongV := h.bridgeInArgs(1, 100, h.attestors[0], h.attestors[1])
	wrongV[7].([][]byte)[0][64] = 0
	highS := h.bridgeInArgs(1, 100, h.attestors[0], h.attestors[1])
	signature := highS[7].([][]byte)[0]
	s := new(big.Int).SetBytes(signature[32:64])
	s.Sub(crypto.S256().Params().N, s).FillBytes(signature[32:64])
	wrongDigest := h.bridgeInArgs(1, 101, h.attestors[0], h.attestors[1])
	wrongDigest[6] = big.NewInt(100)
	tooManySignatures := h.bridgeInArgs(1, 100)
	tooManySignatures[7] = make([][]byte, types.MaxAttestors+1)
	var outsider bridgetestutil.Attestor
	for _, candidate := range bridgetestutil.Attestors(4) {
		if !bridgetestutil.Set(h.attestors, 1, 2).Has(candidate.Signer) {
			outsider = candidate
		}
	}
	require.NotNil(t, outsider.Key)
	cases := []struct {
		name   string
		reason string
		args   []interface{}
	}{
		{"below-threshold", "LXP_BRIDGE_BELOW_THRESHOLD: attestor signatures below threshold",
			h.bridgeInArgs(1, 100, h.attestors[2])},
		{"no-signatures", "LXP_BRIDGE_BELOW_THRESHOLD: attestor signatures below threshold", h.bridgeInArgs(1, 100)},
		{"per-transaction-cap", "LXP_BRIDGE_CAP_EXCEEDED: amount exceeds the asset cap",
			h.bridgeInArgs(1, 1001, h.attestors[0], h.attestors[1])},
		{"unknown-chain", "LXP_BRIDGE_UNKNOWN_CHAIN: chain not registered", unregistered},
		{"disabled-chain", "LXP_BRIDGE_CHAIN_DISABLED: chain disabled", disabled},
		{"wrong-vault", "LXP_BRIDGE_VAULT_MISMATCH: vault is not the registered vault", wrongVault},
		{"unknown-asset", "LXP_BRIDGE_UNKNOWN_ASSET: asset not registered", unknownAsset},
		{"zero-amount", "LXP_BRIDGE_INVALID_REQUEST: invalid bridge request", h.bridgeInArgs(1, 0)},
		{"oversized-amount", "LXP_BRIDGE_INVALID_REQUEST: invalid bridge request", oversizedAmount},
		{"zero-recipient", "LXP_BRIDGE_INVALID_REQUEST: invalid bridge request", zeroRecipient},
		{"unpadded-recipient", "LXP_BRIDGE_INVALID_REQUEST: invalid bridge request", unpaddedRecipient},
		{"descending-signers", "LXP_BRIDGE_BAD_SIGNATURE: invalid attestor signature",
			h.bridgeInArgs(1, 100, h.attestors[1], h.attestors[0])},
		{"duplicate-signer", "LXP_BRIDGE_BAD_SIGNATURE: invalid attestor signature",
			h.bridgeInArgs(1, 100, h.attestors[0], h.attestors[0])},
		{"non-member-signer", "LXP_BRIDGE_BAD_SIGNATURE: invalid attestor signature", h.bridgeInArgs(1, 100, outsider)},
		{"short-signature", "LXP_BRIDGE_BAD_SIGNATURE: invalid attestor signature", shortSignature},
		{"wrong-v", "LXP_BRIDGE_BAD_SIGNATURE: invalid attestor signature", wrongV},
		{"high-s", "LXP_BRIDGE_BAD_SIGNATURE: invalid attestor signature", highS},
		{"wrong-digest", "LXP_BRIDGE_BAD_SIGNATURE: invalid attestor signature", wrongDigest},
		{"too-many-signatures", "LXP_BRIDGE_BAD_SIGNATURE: invalid attestor signature", tooManySignatures},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sub := *h
			sub.t = t
			sub.refuse(layerxbridge.BridgeInMethod, c.reason, c.args...)
		})
	}

	require.NoError(t, h.keeper.Pause(h.ctx, types.MsgPause{Authority: authority}))
	require.Equal(t, true, h.view(layerxbridge.IsPausedMethod)[0])
	h.refuse(layerxbridge.BridgeInMethod, "LXP_BRIDGE_PAUSED: bridge paused",
		h.bridgeInArgs(1, 100, h.attestors[0], h.attestors[1])...)
	require.NoError(t, h.keeper.Unpause(h.ctx, types.MsgUnpause{Authority: authority}))
	require.True(t, h.balance().IsZero())

	_, err := h.call(layerxbridge.BridgeInMethod, h.bridgeInArgs(1, 100, h.attestors[0], h.attestors[1])...)
	require.NoError(t, err)
	h.refuse(layerxbridge.BridgeInMethod, "LXP_BRIDGE_NULLIFIED: remote event already bridged",
		h.bridgeInArgs(1, 100, h.attestors[1], h.attestors[2])...)
	require.Equal(t, sdk.NewInt(100), h.balance())

	_, err = h.run(layerxbridge.BridgeInMethod, nil, true, false, h.bridgeInArgs(2, 100, h.attestors[0], h.attestors[1])...)
	require.Error(t, err, "staticcall cannot write")
	_, err = h.run(layerxbridge.BridgeInMethod, nil, false, true, h.bridgeInArgs(2, 100, h.attestors[0], h.attestors[1])...)
	require.Error(t, err, "delegatecall")
	_, err = h.run(layerxbridge.BridgeInMethod, big.NewInt(1), false, false, h.bridgeInArgs(2, 100, h.attestors[0], h.attestors[1])...)
	require.Error(t, err, "not payable")
	_, err = h.run(layerxbridge.IsPausedMethod, nil, true, true)
	require.Error(t, err, "delegatecall of a view")
	require.Equal(t, sdk.NewInt(100), h.balance())
}

func (h *harness) refuse(name, reason string, args ...interface{}) {
	h.t.Helper()
	genesis := h.keeper.ExportGenesis(h.ctx)
	balance := h.balance()
	moduleBalance := testkeeper.EVMTestApp.BankKeeper.GetBalance(h.ctx, h.keeper.ModuleAddress(), h.denom)
	supply := testkeeper.EVMTestApp.BankKeeper.GetSupply(h.ctx, h.denom)
	logs := h.stateDB.GetAllLogs()
	events := append(sdk.Events{}, h.stateDB.Ctx().EventManager().Events()...)
	res := h.revert(name, false, args...)
	require.Equal(h.t, pcommon.RevertReason(errors.New(reason)), res)
	require.Equal(h.t, reason, revertText(h.t, res))
	require.Equal(h.t, genesis, h.keeper.ExportGenesis(h.ctx))
	require.Equal(h.t, balance, h.balance())
	require.Equal(h.t, moduleBalance, testkeeper.EVMTestApp.BankKeeper.GetBalance(h.ctx, h.keeper.ModuleAddress(), h.denom))
	require.Equal(h.t, supply, testkeeper.EVMTestApp.BankKeeper.GetSupply(h.ctx, h.denom))
	require.Equal(h.t, logs, h.stateDB.GetAllLogs())
	require.Equal(h.t, events, h.stateDB.Ctx().EventManager().Events())
}

// revert runs a call that must fail and returns its revert data.
func (h *harness) revert(name string, delegate bool, args ...interface{}) []byte {
	h.t.Helper()
	res, err := h.precompile.Run(h.evm, h.caller, h.caller, h.input(name, args...), nil, false, delegate, nil)
	require.ErrorIs(h.t, err, vm.ErrExecutionReverted)
	return res
}

func revertText(t *testing.T, data []byte) string {
	t.Helper()
	require.GreaterOrEqual(t, len(data), 4)
	require.Equal(t, []byte{0x08, 0xc3, 0x79, 0xa0}, data[:4])
	text, err := abi.UnpackRevert(data)
	require.NoError(t, err)
	return text
}

func TestBridgeRevertCodes(t *testing.T) {
	h := newHarness(t)
	h.activate()
	require.NoError(t, h.keeper.RegisterChain(h.ctx, types.MsgRegisterChain{Authority: authority,
		Chain: types.Chain{ChainID: 2, Vault: types.Address20(vault), FinalityDepth: 64, Enabled: false}}))
	_, err := h.call(layerxbridge.BridgeInMethod, h.bridgeInArgs(1, 100, h.attestors[0], h.attestors[1])...)
	require.NoError(t, err)

	planHeight := h.ctx.BlockHeight() + 1
	testkeeper.EVMTestApp.EvmKeeper.UpgradeKeeper().SetDone(h.ctx.WithBlockHeight(planHeight), pcommon.MergeUpgrade)

	// Below the plan height a refusal keeps today's revert bytes.
	belowThreshold := h.bridgeInArgs(2, 100, h.attestors[2])
	res := h.revert(layerxbridge.BridgeInMethod, false, belowThreshold...)
	require.Equal(t, pcommon.RevertReason(errors.New("1 of 2: attestor signatures below threshold")), res)
	require.Equal(t, "1 of 2: attestor signatures below threshold", revertText(t, res))

	h.at(h.ctx.WithBlockHeight(planHeight))

	unknownChain := h.bridgeInArgs(2, 100, h.attestors[0], h.attestors[1])
	unknownChain[0] = uint64(9)
	vaultMismatch := h.bridgeInArgs(2, 100, h.attestors[0], h.attestors[1])
	vaultMismatch[1] = remote
	unknownAsset := h.bridgeInArgs(2, 100, h.attestors[0], h.attestors[1])
	unknownAsset[5] = remote
	cases := []struct {
		code    string
		message string
		method  string
		args    []interface{}
	}{
		{"LXP_BRIDGE_UNKNOWN_CHAIN", "chain not registered", layerxbridge.BridgeInMethod, unknownChain},
		{"LXP_BRIDGE_CHAIN_DISABLED", "chain disabled", layerxbridge.BridgeOutMethod,
			[]interface{}{uint64(2), asset, big.NewInt(1), remote}},
		{"LXP_BRIDGE_VAULT_MISMATCH", "vault is not the registered vault", layerxbridge.BridgeInMethod, vaultMismatch},
		{"LXP_BRIDGE_INVALID_REQUEST", "invalid bridge request", layerxbridge.BridgeInMethod,
			h.bridgeInArgs(2, 0, h.attestors[0], h.attestors[1])},
		{"LXP_BRIDGE_INVALID_REQUEST", "invalid bridge request", layerxbridge.BridgeOutMethod,
			[]interface{}{chainID, asset, big.NewInt(101), remote}},
		{"LXP_BRIDGE_UNKNOWN_ASSET", "asset not registered", layerxbridge.BridgeInMethod, unknownAsset},
		{"LXP_BRIDGE_UNKNOWN_ASSET", "asset not registered", layerxbridge.BridgeOutMethod,
			[]interface{}{chainID, remote, big.NewInt(1), remote}},
		{"LXP_BRIDGE_NULLIFIED", "remote event already bridged", layerxbridge.BridgeInMethod,
			h.bridgeInArgs(1, 100, h.attestors[1], h.attestors[2])},
		{"LXP_BRIDGE_CAP_EXCEEDED", "amount exceeds the asset cap", layerxbridge.BridgeInMethod,
			h.bridgeInArgs(2, 1001, h.attestors[0], h.attestors[1])},
		{"LXP_BRIDGE_BAD_SIGNATURE", "invalid attestor signature", layerxbridge.BridgeInMethod,
			h.bridgeInArgs(2, 100, h.attestors[1], h.attestors[0])},
		{"LXP_BRIDGE_BELOW_THRESHOLD", "attestor signatures below threshold", layerxbridge.BridgeInMethod,
			belowThreshold},
	}
	for _, c := range cases {
		res := h.revert(c.method, false, c.args...)
		require.Equal(t, pcommon.LXPRevert(c.code, ""), res, c.code)
		require.Equal(t, c.code+": "+c.message, revertText(t, res), c.code)
	}

	require.NoError(t, h.keeper.Pause(h.ctx, types.MsgPause{Authority: authority}))
	res = h.revert(layerxbridge.BridgeOutMethod, false, chainID, asset, big.NewInt(1), remote)
	require.Equal(t, pcommon.LXPRevert("LXP_BRIDGE_PAUSED", ""), res)
	require.Equal(t, "LXP_BRIDGE_PAUSED: bridge paused", revertText(t, res))
	res = h.revert(layerxbridge.BridgeInMethod, false, h.bridgeInArgs(2, 100, h.attestors[0], h.attestors[1])...)
	require.Equal(t, "LXP_BRIDGE_PAUSED: bridge paused", revertText(t, res))
	require.NoError(t, h.keeper.Unpause(h.ctx, types.MsgUnpause{Authority: authority}))

	// A refusal that is not a bridge module error keeps its text at the plan height.
	res = h.revert(layerxbridge.IsPausedMethod, true)
	require.Equal(t, "cannot delegatecall layerxBridge", revertText(t, res))

	// The methods stay live and unchanged at the plan height.
	out, err := h.call(layerxbridge.BridgeInMethod, h.bridgeInArgs(2, 100, h.attestors[0], h.attestors[1])...)
	require.NoError(t, err)
	require.Equal(t, h.denom, out[0])
	out, err = h.call(layerxbridge.BridgeOutMethod, chainID, asset, big.NewInt(50), remote)
	require.NoError(t, err)
	require.Equal(t, uint64(1), out[0])
	require.Equal(t, sdk.NewInt(150), h.balance())
	require.Equal(t, []interface{}{true, vault, uint64(64), true}, h.view(layerxbridge.GetChainMethod, chainID))
	require.Equal(t, []interface{}{h.denom, big.NewInt(1500), big.NewInt(1000), big.NewInt(150)},
		h.view(layerxbridge.GetCapMethod, chainID, asset))
	require.Equal(t, false, h.view(layerxbridge.IsPausedMethod)[0])
	require.Equal(t, true, h.view(layerxbridge.IsNullifiedMethod, chainID, [32]byte{0xde, 0xad, 2}, uint64(2))[0])
	h.at(h.ctx.WithBlockHeight(planHeight + 1))
	h.refuse(layerxbridge.BridgeInMethod, "LXP_BRIDGE_NULLIFIED: remote event already bridged",
		h.bridgeInArgs(2, 100, h.attestors[0], h.attestors[1])...)

	// Every registered bridge refusal maps to its code by registered name.
	for err, code := range map[error]string{
		types.ErrUnauthorized:   "LXP_BRIDGE_UNAUTHORIZED",
		types.ErrUnknownChain:   "LXP_BRIDGE_UNKNOWN_CHAIN",
		types.ErrChainDisabled:  "LXP_BRIDGE_CHAIN_DISABLED",
		types.ErrUnknownAsset:   "LXP_BRIDGE_UNKNOWN_ASSET",
		types.ErrPaused:         "LXP_BRIDGE_PAUSED",
		types.ErrNullified:      "LXP_BRIDGE_NULLIFIED",
		types.ErrCapExceeded:    "LXP_BRIDGE_CAP_EXCEEDED",
		types.ErrBadSignature:   "LXP_BRIDGE_BAD_SIGNATURE",
		types.ErrBelowThreshold: "LXP_BRIDGE_BELOW_THRESHOLD",
		types.ErrVaultMismatch:  "LXP_BRIDGE_VAULT_MISMATCH",
		types.ErrInvalidRequest: "LXP_BRIDGE_INVALID_REQUEST",
	} {
		require.Equal(t, code, layerxbridge.RevertCode(err), code)
		require.Equal(t, pcommon.RevertReason(errors.New(code+": "+err.Error())), pcommon.LXPRevert(code, ""), code)
	}
	require.Equal(t, "LXP_BRIDGE_UNAUTHORIZED", layerxbridge.RevertCode(types.ErrUnauthorized.Wrap("not the bridge authority")))
	require.Equal(t, "", layerxbridge.RevertCode(types.ErrInvalidParams))
	require.Equal(t, "", layerxbridge.RevertCode(errors.New("cannot delegatecall layerxBridge")))
}

func TestBridgeOutBurnsAndEmitsThroughThePrecompile(t *testing.T) {
	h := newHarness(t)
	h.activate()
	_, err := h.call(layerxbridge.BridgeInMethod, h.bridgeInArgs(1, 900, h.attestors[0], h.attestors[1])...)
	require.NoError(t, err)

	out, err := h.call(layerxbridge.BridgeOutMethod, chainID, asset, big.NewInt(400), remote)
	require.NoError(t, err)
	require.Equal(t, uint64(1), out[0])
	require.Equal(t, sdk.NewInt(500), h.balance())
	require.Equal(t, sdk.NewInt(500), testkeeper.EVMTestApp.BankKeeper.GetSupply(h.ctx, h.denom).Amount)
	require.Equal(t, big.NewInt(500), h.view(layerxbridge.GetCapMethod, chainID, asset)[3])

	logs := h.logs("BridgeOut(uint64,address,uint256,address,uint64)")
	require.Len(t, logs, 1)
	require.Equal(t, []common.Hash{logs[0].Topics[0], common.BigToHash(big.NewInt(1)), common.BytesToHash(asset.Bytes()),
		common.BigToHash(big.NewInt(1))}, logs[0].Topics)
	data, err := h.precompile.GetABI().Events[layerxbridge.BridgeOutEvent].Inputs.NonIndexed().Unpack(logs[0].Data)
	require.NoError(t, err)
	require.Equal(t, []interface{}{big.NewInt(400), remote}, data)
	require.Equal(t, 1, h.events(types.EventBridgeOut))

	out, err = h.call(layerxbridge.BridgeOutMethod, chainID, asset, big.NewInt(500), remote)
	require.NoError(t, err)
	require.Equal(t, uint64(2), out[0])
	require.True(t, h.balance().IsZero())
	_, err = h.call(layerxbridge.BridgeOutMethod, chainID, asset, big.NewInt(1), remote)
	require.Error(t, err, "nothing left to burn")
	_, err = h.run(layerxbridge.BridgeOutMethod, nil, true, false, chainID, asset, big.NewInt(1), remote)
	require.Error(t, err, "staticcall cannot write")
}

func TestBridgeCapBoundariesThroughThePrecompile(t *testing.T) {
	h := newHarness(t)
	h.activate()
	testkeeper.EVMTestApp.EvmKeeper.UpgradeKeeper().SetDone(h.ctx, pcommon.MergeUpgrade)
	for _, index := range []uint64{1, 2} {
		out, err := h.call(layerxbridge.BridgeInMethod, h.bridgeInArgs(index, 700, h.attestors[0], h.attestors[1])...)
		require.NoError(t, err)
		require.Equal(t, []interface{}{h.denom}, out)
	}
	h.refuse(layerxbridge.BridgeInMethod, "LXP_BRIDGE_CAP_EXCEEDED: amount exceeds the asset cap",
		h.bridgeInArgs(3, 101, h.attestors[0], h.attestors[1])...)
	require.Equal(t, false, h.view(layerxbridge.IsNullifiedMethod, chainID, [32]byte{0xde, 0xad, 3}, uint64(3))[0])
	out, err := h.call(layerxbridge.BridgeInMethod, h.bridgeInArgs(3, 100, h.attestors[0], h.attestors[1])...)
	require.NoError(t, err)
	require.Equal(t, []interface{}{h.denom}, out)
	require.Equal(t, sdk.NewInt(1500), h.balance())
	require.Equal(t, []interface{}{h.denom, big.NewInt(1500), big.NewInt(1000), big.NewInt(1500)},
		h.view(layerxbridge.GetCapMethod, chainID, asset))
	h.refuse(layerxbridge.BridgeOutMethod, "LXP_BRIDGE_CAP_EXCEEDED: amount exceeds the asset cap",
		chainID, asset, big.NewInt(1001), remote)
	out, err = h.call(layerxbridge.BridgeOutMethod, chainID, asset, big.NewInt(1000), remote)
	require.NoError(t, err)
	require.Equal(t, []interface{}{uint64(1)}, out)
	require.Equal(t, sdk.NewInt(500), h.balance())
	require.Equal(t, sdk.NewInt(500), testkeeper.EVMTestApp.BankKeeper.GetSupply(h.ctx, h.denom).Amount)
	require.Equal(t, sdk.NewInt(500), h.keeper.InFlight(h.ctx, h.denom))
	require.Equal(t, uint64(1), h.keeper.OutboundNonce(h.ctx, chainID))
	require.NoError(t, h.keeper.SetCap(h.ctx, types.MsgSetCap{Authority: authority, ChainID: chainID,
		Asset: types.Address20(asset), MaxInFlight: sdk.NewInt(1500), MaxPerTx: sdk.NewInt(100)}))
	h.refuse(layerxbridge.BridgeOutMethod, "LXP_BRIDGE_CAP_EXCEEDED: amount exceeds the asset cap",
		chainID, asset, big.NewInt(101), remote)
	out, err = h.call(layerxbridge.BridgeOutMethod, chainID, asset, big.NewInt(100), remote)
	require.NoError(t, err)
	require.Equal(t, []interface{}{uint64(2)}, out)
	require.Equal(t, sdk.NewInt(400), h.balance())
	require.Equal(t, sdk.NewInt(400), testkeeper.EVMTestApp.BankKeeper.GetSupply(h.ctx, h.denom).Amount)
	require.Equal(t, sdk.NewInt(400), h.keeper.InFlight(h.ctx, h.denom))
	require.Equal(t, uint64(2), h.keeper.OutboundNonce(h.ctx, chainID))
	require.Len(t, h.logs("BridgeOut(uint64,address,uint256,address,uint64)"), 2)
	require.Equal(t, 2, h.events(types.EventBridgeOut))
}

func TestBridgeOutboundNonceThroughThePrecompile(t *testing.T) {
	h := newHarness(t)
	h.activate()
	testkeeper.EVMTestApp.EvmKeeper.UpgradeKeeper().SetDone(h.ctx, pcommon.MergeUpgrade)
	_, err := h.call(layerxbridge.BridgeInMethod, h.bridgeInArgs(1, 41, h.attestors[0], h.attestors[1])...)
	require.NoError(t, err)
	for nonce := uint64(1); nonce <= 41; nonce++ {
		out, err := h.call(layerxbridge.BridgeOutMethod, chainID, asset, big.NewInt(1), remote)
		require.NoError(t, err)
		require.Equal(t, []interface{}{nonce}, out)
	}
	genesis := h.keeper.ExportGenesis(h.ctx)
	require.Equal(t, uint64(41), h.keeper.OutboundNonce(h.ctx, chainID))
	cap := h.view(layerxbridge.GetCapMethod, chainID, asset)
	require.Len(t, cap, 4)
	require.Equal(t, []interface{}{h.denom, big.NewInt(1500), big.NewInt(1000)}, cap[:3])
	require.Equal(t, 0, cap[3].(*big.Int).Cmp(big.NewInt(0)))
	require.Equal(t, []interface{}{false, common.Address{}, uint64(0), false},
		h.view(layerxbridge.GetChainMethod, uint64(9)))
	unknownCap := h.view(layerxbridge.GetCapMethod, chainID, remote)
	require.Len(t, unknownCap, 4)
	require.Equal(t, "", unknownCap[0])
	for _, value := range unknownCap[1:] {
		require.Equal(t, 0, value.(*big.Int).Cmp(big.NewInt(0)))
	}
	require.Equal(t, true, h.view(layerxbridge.IsNullifiedMethod, chainID, [32]byte{0xde, 0xad, 1}, uint64(1))[0])
	require.Equal(t, false, h.view(layerxbridge.IsNullifiedMethod, chainID, [32]byte{0xde, 0xad, 1}, uint64(2))[0])
	require.Equal(t, genesis, h.keeper.ExportGenesis(h.ctx))
	require.True(t, h.balance().Equal(sdk.ZeroInt()))
	require.True(t, testkeeper.EVMTestApp.BankKeeper.GetSupply(h.ctx, h.denom).Amount.Equal(sdk.ZeroInt()))
	require.Len(t, h.logs("BridgeOut(uint64,address,uint256,address,uint64)"), 41)
	require.Equal(t, 41, h.events(types.EventBridgeOut))
}

func TestGasIsTheDocumentedFormula(t *testing.T) {
	h := newHarness(t)
	input := h.input(layerxbridge.BridgeInMethod, h.bridgeInArgs(1, 100, h.attestors[0], h.attestors[1])...)
	require.Equal(t, layerxbridge.Gas(uint64(len(input)-4), 2, 8), h.precompile.RequiredGas(input))
	require.Equal(t, 3000+16*uint64(len(input)-4)+3000*2+5000*8, h.precompile.RequiredGas(input))
	input = h.input(layerxbridge.BridgeOutMethod, chainID, asset, big.NewInt(1), remote)
	require.Equal(t, uint64(3000+16*128+5000*6), h.precompile.RequiredGas(input))
	input = h.input(layerxbridge.IsNullifiedMethod, chainID, [32]byte{}, uint64(0))
	require.Equal(t, uint64(3000+16*96), h.precompile.RequiredGas(input))
	input = h.input(layerxbridge.IsPausedMethod)
	require.Equal(t, uint64(3000), h.precompile.RequiredGas(input))
}

func TestNewPrecompileNeedsTheBridgeKeeper(t *testing.T) {
	_, err := layerxbridge.NewPrecompile(keepers{Keepers: testkeeper.EVMTestApp.GetPrecompileKeepers(), bridge: nil})
	require.Error(t, err)
	p := layerxbridge.NewPrecompileWithKeeper(bridgekeeper.Keeper{})
	require.Equal(t, bridge, p.Address())
	require.Equal(t, layerxbridge.PrecompileName, p.GetName())
}
