package teeattestor_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	xwebkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/keeper"
	xwebtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/testutil"
	xwebtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	agentcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/teeattestor"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

const (
	blockOpen        = uint64(10000)
	activationHeight = int64(100)
	suppliedGas      = uint64(2000000)
	upgradePlan      = "merge-plan"
)

var (
	caller     = ethcommon.HexToAddress("0x000000000000000000000000000000000000a111")
	mrtd       = [32]byte{0x31, 0x52, 0x73}
	reportData = [32]byte{0x02, 0x46, 0x68}
)

type harness struct {
	t          *testing.T
	keeper     *xwebkeeper.Keeper
	keys       []xwebtestutil.Attestor
	stateDB    *state.DBImpl
	evm        *vm.EVM
	precompile *teeattestor.TeeAttestorPrecompile
}

func newHarness(t *testing.T, registered int) *harness {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx, _ := app.GetContextForDeliverTx([]byte{}).CacheContext()
	ctx = ctx.WithBlockHeight(activationHeight).WithBlockTime(time.Unix(int64(blockOpen), 0))
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	k, ctx := xwebtestutil.NewKeeper(app, ctx)
	k.InitGenesis(ctx, *xwebtypes.DefaultGenesis())
	keys := xwebtestutil.Attestors(5)
	if registered > len(keys) {
		keys = xwebtestutil.Attestors(registered)
	}
	for _, key := range keys[:registered] {
		require.NoError(t, k.RegisterAttestor(ctx, xwebtypes.MsgRegisterAttestor{
			Authority: xwebtypes.DefaultAuthority(), Attestor: key.Registration(),
		}))
	}
	app.UpgradeKeeper.SetDone(ctx, upgradePlan)
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	p, err := teeattestor.NewPrecompile(&k, &app.EvmKeeper, &app.UpgradeKeeper, upgradePlan)
	require.NoError(t, err)
	require.NotNil(t, p)
	h := &harness{t: t, keeper: &k, keys: keys, precompile: p}
	h.resetEVM(ctx)
	return h
}

func (h *harness) resetEVM(ctx sdk.Context) {
	h.t.Helper()
	app := testkeeper.EVMTestApp
	h.stateDB = state.NewDBImpl(ctx, &app.EvmKeeper, false)
	block, err := app.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
	require.NoError(h.t, err)
	chain := evmtypes.DefaultChainConfig().EthereumConfig(app.EvmKeeper.ChainID(ctx))
	contracts := map[ethcommon.Address]vm.PrecompiledContract{h.precompile.Address(): h.precompile}
	h.evm = vm.NewEVM(*block, h.stateDB, chain, vm.Config{}, contracts)
}

func (h *harness) quote(family uint8, timestamp uint64, debug bool, nonce uint64, keys ...xwebtestutil.Attestor) []byte {
	h.t.Helper()
	data := make([]byte, 96)
	copy(data[:4], []byte("PXVE"))
	data[4], data[5] = 1, family
	if debug {
		data[6] = 1
	}
	copy(data[7:39], mrtd[:])
	copy(data[39:71], reportData[:])
	binary.BigEndian.PutUint64(data[71:79], timestamp)
	chain := testkeeper.EVMTestApp.EvmKeeper.ChainID(h.stateDB.Ctx())
	require.True(h.t, chain.IsUint64())
	binary.BigEndian.PutUint64(data[79:87], chain.Uint64())
	binary.BigEndian.PutUint64(data[87:95], nonce)
	require.LessOrEqual(h.t, len(keys), math.MaxUint8)
	data[95] = byte(len(keys))
	digest := xwebtypes.Hash32(crypto.Keccak256Hash(data[:95]))
	for _, signature := range xwebtestutil.Sign(digest, keys...) {
		data = append(data, signature...)
	}
	require.Len(h.t, data, 96+65*len(keys))
	return data
}

func (h *harness) input(name string, args ...interface{}) []byte {
	h.t.Helper()
	a := h.precompile.GetABI()
	input, err := a.Pack(name, args...)
	require.NoError(h.t, err)
	return input
}

func (h *harness) view(name string, args ...interface{}) []interface{} {
	h.t.Helper()
	before := h.keeper.ExportGenesis(h.stateDB.Ctx())
	events := h.stateDB.Ctx().EventManager().Events()
	input := h.input(name, args...)
	base := uint64(1000)
	if name == "verify" || name == "verifyAndExpect" {
		base = 46000
	}
	cost := base + 3*uint64(len(input))
	require.Equal(h.t, cost, h.precompile.RequiredGas(input))
	ret, left, err := h.evm.StaticCall(caller, h.precompile.Address(), input, suppliedGas)
	require.NoError(h.t, err)
	require.Equal(h.t, suppliedGas-cost, left)
	require.Empty(h.t, h.stateDB.GetAllLogs())
	require.Equal(h.t, events, h.stateDB.Ctx().EventManager().Events())
	require.Equal(h.t, before, h.keeper.ExportGenesis(h.stateDB.Ctx()))
	a := h.precompile.GetABI()
	output, err := a.Methods[name].Outputs.Unpack(ret)
	require.NoError(h.t, err)
	require.Len(h.t, output, 1)
	return output
}

func (h *harness) refuses(name, message string, code agentcommon.ErrorCode, args ...interface{}) {
	h.t.Helper()
	before := h.keeper.ExportGenesis(h.stateDB.Ctx())
	events := h.stateDB.Ctx().EventManager().Events()
	ret, _, err := h.evm.StaticCall(caller, h.precompile.Address(), h.input(name, args...), suppliedGas)
	require.ErrorIs(h.t, err, vm.ErrExecutionReverted)
	reason, err := abi.UnpackRevert(ret)
	require.NoError(h.t, err)
	require.Equal(h.t, message, reason)
	require.ErrorIs(h.t, h.stateDB.GetPrecompileError(), code)
	require.Empty(h.t, h.stateDB.GetAllLogs())
	require.Equal(h.t, events, h.stateDB.Ctx().EventManager().Events())
	require.Equal(h.t, before, h.keeper.ExportGenesis(h.stateDB.Ctx()))
}

func attestation(t *testing.T, output []interface{}) teeattestor.Attestation {
	t.Helper()
	converted := abi.ConvertType(output[0], new(teeattestor.Attestation))
	value, ok := converted.(*teeattestor.Attestation)
	require.True(t, ok)
	return *value
}

func TestVerifyDevEnvelope(t *testing.T) {
	h := newHarness(t, 5)
	for family := uint8(0); family < 4; family++ {
		t.Run(string(rune('0'+family)), func(t *testing.T) {
			previous := h.t
			h.t = t
			defer func() { h.t = previous }()
			quote := h.quote(family, blockOpen-60, false, 7, h.keys[:3]...)
			want := teeattestor.Attestation{Family: family, Mrtd: mrtd, ReportData: reportData, Timestamp: blockOpen - 60, Debug: false}
			require.Equal(t, want, attestation(t, h.view("verify", family, quote)))
			require.Equal(t, want, attestation(t, h.view("verifyAndExpect", family, quote, reportData)))
		})
	}
	h.refuses("verify", "LXP_AGENT_TEE_UNKNOWN_FAMILY: unknown family", agentcommon.ErrTEEUnknownFamily,
		uint8(4), h.quote(0, blockOpen-60, false, 7, h.keys[:3]...))
}

func TestThresholdAgainstXwebSet(t *testing.T) {
	h := newHarness(t, 5)
	require.Equal(t, uint32(3), h.keeper.GetAttestorSet(h.stateDB.Ctx()).Threshold)
	for count := 0; count < 3; count++ {
		h.refuses("verify", "LXP_AGENT_TEE_UNTRUSTED_CHAIN: untrusted chain", agentcommon.ErrTEEUntrustedChain,
			uint8(0), h.quote(0, blockOpen-60, false, 7, h.keys[:count]...))
	}
	for count := 3; count <= 5; count++ {
		require.Equal(t, reportData, attestation(t, h.view("verify", uint8(0),
			h.quote(0, blockOpen-60, false, 7, h.keys[:count]...))).ReportData)
	}
	for family := uint8(0); family < 4; family++ {
		require.Equal(t, big.NewInt(5), h.view("rootCount", family)[0])
		for index, key := range h.keys {
			require.Equal(t, key.Signer[:], h.view("rootOf", family, big.NewInt(int64(index)))[0])
		}
		for _, index := range []*big.Int{big.NewInt(5), big.NewInt(6), new(big.Int).Lsh(big.NewInt(1), 64),
			new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1)),
			new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))} {
			require.Empty(t, h.view("rootOf", family, index)[0])
		}
	}
	for _, family := range []uint8{4, 255} {
		require.Zero(t, h.view("rootCount", family)[0].(*big.Int).Sign())
		require.Empty(t, h.view("rootOf", family, big.NewInt(0))[0])
	}
	empty := newHarness(t, 0)
	empty.refuses("verify", "LXP_AGENT_TEE_NO_ROOTS: no roots loaded", agentcommon.ErrTEENoRoots,
		uint8(0), empty.quote(0, blockOpen-60, false, 7, empty.keys[:3]...))
	require.Zero(t, empty.view("rootCount", uint8(0))[0].(*big.Int).Sign())
	require.Empty(t, empty.view("rootOf", uint8(0), big.NewInt(0))[0])
	for _, registered := range []int{3, 4, 6} {
		incompatible := newHarness(t, registered)
		incompatible.refuses("verify", "LXP_AGENT_TEE_UNTRUSTED_CHAIN: untrusted chain", agentcommon.ErrTEEUntrustedChain,
			uint8(0), incompatible.quote(0, blockOpen-60, false, 7, incompatible.keys[:3]...))
	}
	for _, threshold := range []uint32{4, 5} {
		strict := newHarness(t, 5)
		governanceCtx, commitThreshold := strict.stateDB.Ctx().CacheContext()
		require.NoError(t, strict.keeper.SetThreshold(governanceCtx, xwebtypes.MsgSetThreshold{
			Authority: xwebtypes.DefaultAuthority(), Threshold: threshold,
		}))
		commitThreshold()
		strict.refuses("verify", "LXP_AGENT_TEE_UNTRUSTED_CHAIN: untrusted chain", agentcommon.ErrTEEUntrustedChain,
			uint8(0), strict.quote(0, blockOpen-60, false, 7, strict.keys[:threshold-1]...))
		require.Equal(t, reportData, attestation(t, strict.view("verify", uint8(0),
			strict.quote(0, blockOpen-60, false, 7, strict.keys[:threshold]...))).ReportData)
	}
}

func TestStaleDebugAndMismatch(t *testing.T) {
	h := newHarness(t, 5)
	for _, timestamp := range []uint64{0, 9300, blockOpen - 601, blockOpen + 1, math.MaxUint64} {
		h.refuses("verify", "LXP_AGENT_TEE_STALE: attestation stale", agentcommon.ErrTEEStale,
			uint8(0), h.quote(0, timestamp, false, 7, h.keys[:3]...))
	}
	for _, timestamp := range []uint64{blockOpen - 600, blockOpen - 1, blockOpen} {
		require.Equal(t, timestamp, attestation(t, h.view("verify", uint8(0),
			h.quote(0, timestamp, false, 7, h.keys[:3]...))).Timestamp)
	}
	h.refuses("verify", "LXP_AGENT_TEE_DEBUG_REFUSED: debug enclave refused", agentcommon.ErrTEEDebugRefused,
		uint8(0), h.quote(0, blockOpen-60, true, 7, h.keys[:3]...))
	h.refuses("verifyAndExpect", "LXP_AGENT_TEE_REPORT_DATA_MISMATCH: report data mismatch", agentcommon.ErrTEEReportDataMismatch,
		uint8(0), h.quote(0, blockOpen-60, false, 7, h.keys[:3]...), [32]byte{0x01})
	h.resetEVM(h.stateDB.Ctx().WithBlockTime(time.Unix(-1, 0)))
	h.refuses("verify", "LXP_AGENT_TEE_STALE: attestation stale", agentcommon.ErrTEEStale,
		uint8(0), h.quote(0, 0, false, 7, h.keys[:3]...))
}

func TestEnvelopeAndSignatureRefusals(t *testing.T) {
	h := newHarness(t, 5)
	valid := h.quote(0, blockOpen-60, false, 7, h.keys[:3]...)
	tests := []struct {
		name string
		edit func([]byte) []byte
	}{
		{"magic", func(q []byte) []byte { q[0] = 'Q'; return q }},
		{"version zero", func(q []byte) []byte { q[4] = 0; return q }},
		{"version unknown", func(q []byte) []byte { q[4] = 2; return q }},
		{"debug noncanonical", func(q []byte) []byte { q[6] = 2; return q }},
		{"family mismatch", func(q []byte) []byte { q[5] = 1; return q }},
		{"chain mismatch", func(q []byte) []byte { q[86] ^= 1; return q }},
		{"count small", func(q []byte) []byte { q[95] = 2; return q }},
		{"count large", func(q []byte) []byte { q[95] = 255; return q }},
		{"count zero with signatures", func(q []byte) []byte { q[95] = 0; return q }},
		{"trailing", func(q []byte) []byte { return append(q, 0) }},
		{"signature truncated", func(q []byte) []byte { return q[:len(q)-1] }},
		{"v zero", func(q []byte) []byte { q[160] = 0; return q }},
		{"v one", func(q []byte) []byte { q[160] = 1; return q }},
		{"v above bound", func(q []byte) []byte { q[160] = 29; return q }},
		{"zero r", func(q []byte) []byte { clear(q[96:128]); return q }},
		{"zero s", func(q []byte) []byte { clear(q[128:160]); return q }},
		{"r at curve order", func(q []byte) []byte { crypto.S256().Params().N.FillBytes(q[96:128]); return q }},
		{"high s", func(q []byte) []byte {
			s := new(big.Int).SetBytes(q[128:160])
			s.Sub(crypto.S256().Params().N, s).FillBytes(q[128:160])
			if q[160] == 27 {
				q[160] = 28
			} else {
				q[160] = 27
			}
			return q
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			previous := h.t
			h.t = t
			defer func() { h.t = previous }()
			quote := test.edit(bytes.Clone(valid))
			h.refuses("verify", "LXP_AGENT_TEE_INVALID_QUOTE: invalid quote", agentcommon.ErrTEEInvalidQuote, uint8(0), quote)
		})
	}
	for _, length := range []int{0, 1, 4, 5, 6, 7, 39, 71, 79, 87, 94, 95} {
		h.refuses("verify", "LXP_AGENT_TEE_INVALID_QUOTE: invalid quote", agentcommon.ErrTEEInvalidQuote, uint8(0), valid[:length])
	}
	h.refuses("verify", "LXP_AGENT_TEE_UNKNOWN_FAMILY: unknown family", agentcommon.ErrTEEUnknownFamily,
		uint8(0), h.quote(4, blockOpen-60, false, 7, h.keys[:3]...))
	h.refuses("verify", "LXP_AGENT_TEE_UNKNOWN_FAMILY: unknown family", agentcommon.ErrTEEUnknownFamily,
		uint8(255), valid)
	for _, keys := range [][]xwebtestutil.Attestor{
		{h.keys[0], h.keys[0], h.keys[1]},
		{h.keys[2], h.keys[1], h.keys[0]},
		{h.keys[0], h.keys[2], h.keys[1]},
	} {
		h.refuses("verify", "LXP_AGENT_TEE_UNTRUSTED_CHAIN: untrusted chain", agentcommon.ErrTEEUntrustedChain,
			uint8(0), h.quote(0, blockOpen-60, false, 7, keys...))
	}
	outsiderKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	outsider := xwebtestutil.Attestor{Key: outsiderKey, Signer: xwebtypes.Address20(crypto.PubkeyToAddress(outsiderKey.PublicKey))}
	require.False(t, h.keeper.GetAttestorSet(h.stateDB.Ctx()).Has(outsider.Signer))
	h.refuses("verify", "LXP_AGENT_TEE_UNTRUSTED_CHAIN: untrusted chain", agentcommon.ErrTEEUntrustedChain,
		uint8(0), h.quote(0, blockOpen-60, false, 7, h.keys[0], h.keys[1], outsider))
}

func TestABISelectorsGasAndEvent(t *testing.T) {
	h := newHarness(t, 5)
	require.Equal(t, ethcommon.HexToAddress("0x0000000000000000000000000000000000000907"), h.precompile.Address())
	a := h.precompile.GetABI()
	require.Len(t, a.Methods, 4)
	for _, row := range []struct {
		name, signature, selector string
		base                      uint64
	}{
		{"verify", "verify(uint8,bytes)", "5fa55ca7", 46000},
		{"verifyAndExpect", "verifyAndExpect(uint8,bytes,bytes32)", "71ded4b3", 46000},
		{"rootOf", "rootOf(uint8,uint256)", "01ec24b8", 1000},
		{"rootCount", "rootCount(uint8)", "26f02502", 1000},
	} {
		method := a.Methods[row.name]
		require.Equal(t, row.signature, method.Sig)
		require.Equal(t, row.selector, hex.EncodeToString(method.ID))
		require.Equal(t, crypto.Keccak256([]byte(row.signature))[:4], method.ID)
		require.Equal(t, "view", method.StateMutability)
		require.Len(t, method.Outputs, 1)
		for _, size := range []int{4, 36, 260, 1024} {
			input := make([]byte, size)
			copy(input, method.ID)
			require.Equal(t, row.base+3*uint64(size), h.precompile.RequiredGas(input))
			require.Equal(t, row.base+3*uint64(size), h.precompile.GetExecutor().RequiredGas(input[4:], &method))
		}
		resolved, err := h.precompile.MethodById(method.ID)
		require.NoError(t, err)
		require.Equal(t, row.signature, resolved.Sig)
	}
	for _, name := range []string{"verify", "verifyAndExpect"} {
		output := a.Methods[name].Outputs[0]
		require.Equal(t, "att", output.Name)
		require.Equal(t, uint8(abi.TupleTy), output.Type.T)
		require.Equal(t, []string{"family", "mrtd", "reportData", "timestamp", "debug"}, output.Type.TupleRawNames)
		for i, kind := range []string{"uint8", "bytes32", "bytes32", "uint64", "bool"} {
			require.Equal(t, kind, output.Type.TupleElems[i].String())
		}
	}
	require.Equal(t, "bytes", a.Methods["rootOf"].Outputs[0].Type.String())
	require.Equal(t, "uint256", a.Methods["rootCount"].Outputs[0].Type.String())
	require.Len(t, a.Events, 1)
	event := a.Events["AttestationVerified"]
	signature := "AttestationVerified(uint8,bytes32,bytes32,uint64,bool)"
	require.Equal(t, signature, event.Sig)
	require.Equal(t, crypto.Keccak256Hash([]byte(signature)), event.ID)
	require.Equal(t, "0x936ac9b7", event.ID.Hex()[:10])
	require.False(t, event.Anonymous)
	require.Len(t, event.Inputs, 5)
	for i, field := range []struct {
		name, kind string
		indexed    bool
	}{
		{"family", "uint8", true}, {"mrtd", "bytes32", true}, {"reportData", "bytes32", false},
		{"timestamp", "uint64", false}, {"debug", "bool", false},
	} {
		require.Equal(t, field.name, event.Inputs[i].Name)
		require.Equal(t, field.kind, event.Inputs[i].Type.String())
		require.Equal(t, field.indexed, event.Inputs[i].Indexed)
	}
	for _, input := range [][]byte{nil, {1}, {1, 2, 3}, {0xde, 0xad, 0xbe, 0xef}} {
		require.Equal(t, pcommon.UnknownMethodCallGas, h.precompile.RequiredGas(input))
	}
	quote := h.quote(0, blockOpen-60, false, 7, h.keys[:3]...)
	input := h.input("verify", uint8(0), quote)
	cost := uint64(46000) + 3*uint64(len(input))
	ret, left, err := h.evm.StaticCall(caller, h.precompile.Address(), input, cost)
	require.NoError(t, err)
	require.Zero(t, left)
	require.NotEmpty(t, ret)
	ret, left, err = h.evm.StaticCall(caller, h.precompile.Address(), input, cost-1)
	require.ErrorIs(t, err, vm.ErrOutOfGas)
	require.Zero(t, left)
	require.Empty(t, ret)
	require.Empty(t, h.stateDB.GetAllLogs())
}

func TestActivationAndReplayDoesNotWrite(t *testing.T) {
	h := newHarness(t, 5)
	quote := h.quote(0, blockOpen-60, false, 0, h.keys[:3]...)
	for _, height := range []int64{activationHeight - 1, activationHeight, activationHeight + 1} {
		h.resetEVM(h.stateDB.Ctx().WithBlockHeight(height))
		if height < activationHeight {
			data, err := h.precompile.Run(h.evm, caller, h.precompile.Address(), h.input("verify", uint8(0), quote), nil, true, false, nil)
			require.ErrorIs(t, err, vm.ErrExecutionReverted)
			reason, err := abi.UnpackRevert(data)
			require.NoError(t, err)
			require.Equal(t, "LXP_AGENT_NOT_ACTIVE: merge-plan not active at height 99", reason)
			require.ErrorIs(t, h.stateDB.GetPrecompileError(), agentcommon.ErrNotActive)
		} else {
			want := teeattestor.Attestation{Family: 0, Mrtd: mrtd, ReportData: reportData, Timestamp: blockOpen - 60, Debug: false}
			require.Equal(t, want, attestation(t, h.view("verify", uint8(0), quote)))
			require.Equal(t, want, attestation(t, h.view("verify", uint8(0), quote)))
			require.Equal(t, want, attestation(t, h.view("verify", uint8(0),
				h.quote(0, blockOpen-60, false, math.MaxUint64, h.keys[:3]...))))
		}
	}
}

func TestFrameworkAndDirectArgumentRefusals(t *testing.T) {
	h := newHarness(t, 5)
	a := h.precompile.GetABI()
	valid := h.input("verify", uint8(0), h.quote(0, blockOpen-60, false, 7, h.keys[:3]...))
	for _, test := range []struct {
		name    string
		evm     *vm.EVM
		input   []byte
		value   *big.Int
		want    agentcommon.ErrorCode
		message string
	}{
		{"no EVM", nil, valid, nil, agentcommon.ErrNoContext, "LXP_PRECOMPILE_NO_CONTEXT: cannot get context from EVM"},
		{"no state", &vm.EVM{}, valid, nil, agentcommon.ErrNoContext, "LXP_PRECOMPILE_NO_CONTEXT: cannot get context from EVM"},
		{"short selector", h.evm, []byte{1, 2, 3}, nil, agentcommon.ErrInputTooShort, "LXP_PRECOMPILE_INPUT_TOO_SHORT: input too short to extract method ID"},
		{"unknown selector", h.evm, []byte{0xde, 0xad, 0xbe, 0xef}, nil, agentcommon.ErrUnknownMethod, "LXP_PRECOMPILE_UNKNOWN_METHOD: no method with id: 0xdeadbeef"},
		{"missing encoded arguments", h.evm, a.Methods["verify"].ID, nil, agentcommon.ErrABIDecode, "LXP_AGENT_ABI_DECODE: abi: attempting to unmarshal an empty string while arguments are expected"},
		{"nonpayable", h.evm, valid, big.NewInt(1), agentcommon.ErrNonPayable, "LXP_PRECOMPILE_NON_PAYABLE: sending funds to a non-payable function"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, err := h.precompile.Run(test.evm, caller, h.precompile.Address(), test.input, test.value, true, false, nil)
			require.ErrorIs(t, err, vm.ErrExecutionReverted)
			reason, err := abi.UnpackRevert(data)
			require.NoError(t, err)
			require.Equal(t, test.message, reason)
			if test.evm != nil && state.GetDBImpl(test.evm.StateDB) != nil {
				require.ErrorIs(t, state.GetDBImpl(test.evm.StateDB).GetPrecompileError(), test.want)
			}
		})
	}
	executor := h.precompile.GetExecutor()
	for _, test := range []struct {
		name string
		args []interface{}
		want agentcommon.ErrorCode
	}{
		{"verify", nil, agentcommon.ErrArgCount},
		{"verify", []interface{}{uint8(0), []byte{}, reportData}, agentcommon.ErrArgCount},
		{"verify", []interface{}{uint64(0), []byte{}}, agentcommon.ErrABIDecode},
		{"verify", []interface{}{uint8(0), "quote"}, agentcommon.ErrABIDecode},
		{"verifyAndExpect", []interface{}{uint8(0), []byte{}}, agentcommon.ErrArgCount},
		{"verifyAndExpect", []interface{}{uint8(0), []byte{}, []byte{}}, agentcommon.ErrABIDecode},
		{"rootCount", nil, agentcommon.ErrArgCount},
		{"rootCount", []interface{}{big.NewInt(0)}, agentcommon.ErrABIDecode},
		{"rootOf", []interface{}{uint8(0)}, agentcommon.ErrArgCount},
		{"rootOf", []interface{}{uint8(0), uint64(0)}, agentcommon.ErrABIDecode},
		{"rootOf", []interface{}{uint8(0), (*big.Int)(nil)}, agentcommon.ErrABIDecode},
		{"rootOf", []interface{}{uint8(0), big.NewInt(-1)}, agentcommon.ErrABIDecode},
		{"rootOf", []interface{}{uint8(0), new(big.Int).Lsh(big.NewInt(1), 256)}, agentcommon.ErrABIDecode},
	} {
		method := a.Methods[test.name]
		_, err := executor.Execute(h.stateDB.Ctx(), &method, caller, h.precompile.Address(), test.args, nil, true, h.evm, nil)
		require.ErrorIs(t, err, test.want)
	}
	_, err := executor.Execute(h.stateDB.Ctx(), nil, caller, h.precompile.Address(), nil, nil, true, h.evm, nil)
	require.ErrorIs(t, err, agentcommon.ErrUnknownMethod)
	require.Empty(t, h.stateDB.GetAllLogs())
}

func TestConstructorRequiresRealKeepersAndActivationReader(t *testing.T) {
	h := newHarness(t, 5)
	app := testkeeper.EVMTestApp
	p, err := teeattestor.NewPrecompile(nil, &app.EvmKeeper, &app.UpgradeKeeper, upgradePlan)
	require.Nil(t, p)
	require.ErrorIs(t, err, agentcommon.ErrNoContext)
	require.EqualError(t, err, "LXP_PRECOMPILE_NO_CONTEXT: tee attestor keepers are unavailable")
	p, err = teeattestor.NewPrecompile(h.keeper, nil, &app.UpgradeKeeper, upgradePlan)
	require.Nil(t, p)
	require.ErrorIs(t, err, agentcommon.ErrNoContext)
	require.EqualError(t, err, "LXP_PRECOMPILE_NO_CONTEXT: tee attestor keepers are unavailable")
	p, err = teeattestor.NewPrecompile(h.keeper, &app.EvmKeeper, nil, upgradePlan)
	require.Nil(t, p)
	require.ErrorIs(t, err, agentcommon.ErrNotActive)
	require.EqualError(t, err, "LXP_AGENT_NOT_ACTIVE: no merge upgrade activation reader bound")
	p, err = teeattestor.NewPrecompile(h.keeper, &app.EvmKeeper, &app.UpgradeKeeper, "")
	require.Nil(t, p)
	require.ErrorIs(t, err, agentcommon.ErrNotActive)
	require.EqualError(t, err, "LXP_AGENT_NOT_ACTIVE: no merge upgrade activation reader bound")
}
