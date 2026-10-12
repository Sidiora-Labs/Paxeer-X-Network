package keeper_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/keeper"
	xwebtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/xweb"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/prefix"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/address"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// programAsset is a kernel asset id and the bank denom it is held in.
type programAsset struct {
	id    types.Hash32
	denom string
}

var (
	// paxAsset carries the asset id of the kernel program path test.
	paxAsset  = programAsset{types.Hash32{9}, types.ProgramFeeDenomPAX}
	sidAsset  = programAsset{types.Hash32{10}, types.ProgramFeeDenomSID}
	usdcAsset = programAsset{types.Hash32{11}, types.ProgramFeeDenomUSDC}
	usdlAsset = programAsset{types.Hash32{12}, types.ProgramFeeDenomUSDL}

	programP         = types.Hash32(bytes.Repeat([]byte{0x11}, 32))
	programPayload   = []byte("paxeer x network")
	programPrincipal = sdk.AccAddress(bytes.Repeat([]byte{0xa1}, 20))
	programFunding   = sdk.AccAddress(bytes.Repeat([]byte{0xa2}, 20))
	programSequence  = uint64(0x0102030405060708)
	programBalance   = sdk.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 70))
)

// programSuite is the activated suite with the xweb store key in hand, its
// principal and funding accounts holding every program fee asset.
type programSuite struct {
	*suite
	key *sdk.KVStoreKey
}

func newProgramSuite(t *testing.T, denoms ...string) *programSuite {
	t.Helper()
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(startHeight).WithBlockTime(time.Unix(1_800_000_000, 0))
	key := sdk.NewKVStoreKey(types.StoreKey)
	k := keeper.NewKeeper(key, testApp.BankKeeper, &testApp.EvmKeeper)
	ctx = xwebtestutil.WithXWebStore(ctx, key)
	k.InitGenesis(ctx, *types.DefaultGenesis())
	s := &programSuite{suite: &suite{t: t, app: testApp, ctx: ctx, k: k, attestors: xwebtestutil.Attestors(3)}, key: key}
	for _, attestor := range s.attestors {
		require.NoError(t, k.RegisterAttestor(ctx, types.MsgRegisterAttestor{Authority: authority,
			Attestor: attestor.Registration()}))
	}
	if len(denoms) == 0 {
		denoms = programFeeDenoms
	}
	require.NoError(t, k.UpdateParams(ctx, types.MsgSetParams{Authority: authority, Fee: sdk.NewInt(fee),
		MaxPayloadBytes: payloadCap, MaxCallbackGas: callbackCap, TimeoutBlocks: timeout, KernelNetworkId: kernelNetworkID,
		ProgramFeeDenoms: denoms}))
	require.NoError(t, k.Unpause(ctx, types.MsgUnpause{Authority: authority}))
	for _, holder := range []sdk.AccAddress{programPrincipal, programFunding} {
		for _, asset := range []programAsset{paxAsset, sidAsset, usdcAsset, usdlAsset} {
			coins := sdk.NewCoins(sdk.NewCoin(asset.denom, programBalance))
			require.NoError(t, testApp.BankKeeper.MintCoins(ctx, "evm", coins))
			require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", holder, coins))
		}
	}
	s.fund(requester, requesterBal)
	return s
}

// requestData is the request event data: u64 request id, u8 kind, u32
// payload length, payload.
func requestData(id uint64, kind uint8, body []byte) []byte {
	out := binary.BigEndian.AppendUint64(nil, id)
	out = append(out, kind)
	out = binary.BigEndian.AppendUint32(out, uint32(len(body)))
	return append(out, body...)
}

type programEvent struct {
	topic []byte
	data  []byte
}

func webRequest(id uint64) programEvent {
	return programEvent{[]byte(keeper.ProgramRequestTopic), requestData(id, types.KindSearch, programPayload)}
}

func feeLeg(from sdk.AccAddress, asset programAsset, amount sdk.Int) keeper.ProgramLeg {
	return keeper.ProgramLeg{From: from, To: keeper.ProgramFeeAccount(asset.id), Asset: asset.id, Denom: asset.denom,
		Amount: amount}
}

func newCall(legs ...keeper.ProgramLeg) *keeper.ProgramCall {
	return &keeper.ProgramCall{ProgramID: programP, Principal: programPrincipal, Sequence: programSequence, Legs: legs}
}

// run executes one program call as the hosted programs module does: it
// applies the call's legs through the bank, hands every emitted guest event
// to RequestFromProgram, and keeps the legs and the record only when every
// step succeeds.
func (s *programSuite) run(call *keeper.ProgramCall, events ...programEvent) error {
	cached, write := s.ctx.CacheContext()
	for _, leg := range call.Legs {
		if err := s.app.BankKeeper.SendCoins(cached, leg.From, leg.To,
			sdk.NewCoins(sdk.NewCoin(leg.Denom, leg.Amount))); err != nil {
			return err
		}
	}
	for _, event := range events {
		if err := s.k.RequestFromProgram(cached, call, event.topic, event.data); err != nil {
			return err
		}
	}
	write()
	return nil
}

func (s *programSuite) balanceOf(of sdk.AccAddress, asset programAsset) sdk.Int {
	return s.app.BankKeeper.GetBalance(s.ctx, of, asset.denom).Amount
}

func (s *programSuite) raw(programID types.Hash32, id uint64) []byte {
	return s.ctx.KVStore(s.key).Get(keeper.ProgramPendingKey(programID, id))
}

func (s *programSuite) pendingKeys(ctx sdk.Context) int {
	iterator := prefix.NewStore(ctx.KVStore(s.key), []byte("web/pending")).Iterator(nil, nil)
	defer func() { _ = iterator.Close() }()
	count := 0
	for ; iterator.Valid(); iterator.Next() {
		count++
	}
	return count
}

func (s *programSuite) requireNoRecord(id uint64) {
	s.t.Helper()
	require.Nil(s.t, s.raw(programP, id))
	_, found, err := s.k.GetProgramPendingRecord(s.ctx, programP, id)
	require.NoError(s.t, err)
	require.False(s.t, found)
}

func u128(amount sdk.Int) []byte {
	out := make([]byte, 16)
	amount.BigInt().FillBytes(out)
	return out
}

func TestProgramOriginRecordsThePendingRecord(t *testing.T) {
	s := newProgramSuite(t)
	paid := sdk.NewInt(75)
	require.NoError(t, s.run(newCall(feeLeg(programPrincipal, paxAsset, paid)), webRequest(3)))

	key := keeper.ProgramPendingKey(programP, 3)
	require.Equal(t, append(append([]byte("web/pending"), programP[:]...), 0, 0, 0, 0, 0, 0, 0, 3), key)
	raw := s.raw(programP, 3)
	require.Len(t, raw, 123)
	hash := types.Keccak(programPayload)
	feeAccount := keeper.ProgramFeeAccountID(paxAsset.id)
	require.Equal(t, byte(1), raw[0])
	require.Equal(t, byte(2), raw[1])
	require.Equal(t, hash[:], raw[2:34])
	require.Equal(t, paxAsset.id[:], raw[34:66])
	require.Equal(t, feeAccount[:], raw[66:98])
	require.Equal(t, append(make([]byte, 15), 75), raw[98:114])
	require.Equal(t, []byte{1, 2, 3, 4, 5, 6, 7, 8}, raw[114:122])
	require.Equal(t, byte(0), raw[122])

	record, found, err := s.k.GetProgramPendingRecord(s.ctx, programP, 3)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, keeper.ProgramPendingRecord{Kind: types.KindSearch, PayloadHash: hash, FeeAsset: paxAsset.id,
		FeeAccount: feeAccount, Amount: paid, Sequence: programSequence}, record)
	encoded, err := record.Bytes()
	require.NoError(t, err)
	require.Equal(t, raw, encoded)

	require.Equal(t, uint64(1), s.k.ProgramPendingCount(s.ctx))
	require.Equal(t, programBalance.Sub(paid), s.balanceOf(programPrincipal, paxAsset))
	require.Equal(t, paid, s.balanceOf(keeper.ProgramFeeAccount(paxAsset.id), paxAsset))
	require.Equal(t, uint64(0), s.k.Nonce(s.ctx), "origin 2 takes no origin-1 nonce")
	require.Empty(t, s.k.GetRequests(s.ctx))
}

func TestProgramOriginRefusesReuseAndDuplicates(t *testing.T) {
	s := newProgramSuite(t)
	paid := sdk.NewInt(75)
	require.NoError(t, s.run(newCall(feeLeg(programPrincipal, paxAsset, paid)), webRequest(3)))
	stored := s.raw(programP, 3)

	t.Run("sequence_reused", func(t *testing.T) {
		err := s.run(newCall(feeLeg(programPrincipal, paxAsset, paid)), webRequest(3))
		require.EqualError(t, err, "LXP_SEQUENCE_REUSED")
		require.Equal(t, stored, s.raw(programP, 3))
		require.Equal(t, paid, s.balanceOf(keeper.ProgramFeeAccount(paxAsset.id), paxAsset))
	})
	t.Run("another_program_may_use_the_id", func(t *testing.T) {
		other := newCall(feeLeg(programPrincipal, paxAsset, paid))
		other.ProgramID = types.Hash32{0x22}
		require.NoError(t, s.run(other, webRequest(3)))
		require.Len(t, s.raw(other.ProgramID, 3), 123)
	})
	t.Run("duplicate_entry", func(t *testing.T) {
		err := s.run(newCall(feeLeg(programPrincipal, paxAsset, paid)), webRequest(4), webRequest(5))
		require.EqualError(t, err, "LXP_DUPLICATE_ENTRY")
		s.requireNoRecord(4)
		s.requireNoRecord(5)
	})
	for name, data := range map[string][]byte{
		"zero_payload_length": requestData(6, types.KindFetch, nil),
		"kind_zero":           requestData(6, 0, programPayload),
		"kind_three":          requestData(6, types.KindApi, programPayload),
		"empty":               nil,
		"short_header":        requestData(6, types.KindFetch, programPayload)[:12],
		"length_over":         requestData(6, types.KindFetch, programPayload)[:12+len(programPayload)],
		"length_under":        append(requestData(6, types.KindFetch, programPayload), 0),
	} {
		t.Run("non_canonical_"+name, func(t *testing.T) {
			err := s.run(newCall(feeLeg(programPrincipal, paxAsset, paid)),
				programEvent{[]byte(keeper.ProgramRequestTopic), data})
			require.EqualError(t, err, "LXP_NON_CANONICAL")
			s.requireNoRecord(6)
		})
	}
	require.Equal(t, uint64(2), s.k.ProgramPendingCount(s.ctx))
	require.Equal(t, sdk.NewInt(150), s.balanceOf(keeper.ProgramFeeAccount(paxAsset.id), paxAsset))
}

func TestProgramOriginFeeRefusals(t *testing.T) {
	s := newProgramSuite(t)
	stranger := sdk.AccAddress(bytes.Repeat([]byte{0xa3}, 20))
	for name, tc := range map[string]struct {
		legs []keeper.ProgramLeg
		code string
	}{
		"no_leg":   {nil, "LXP_FEE_UNPAYABLE"},
		"zero_leg": {[]keeper.ProgramLeg{feeLeg(programPrincipal, paxAsset, sdk.ZeroInt())}, "LXP_FEE_UNPAYABLE"},
		"leg_elsewhere": {[]keeper.ProgramLeg{{From: programPrincipal, To: stranger, Asset: paxAsset.id,
			Denom: paxAsset.denom, Amount: sdk.NewInt(75)}}, "LXP_FEE_UNPAYABLE"},
		"pax_and_sid": {[]keeper.ProgramLeg{feeLeg(programPrincipal, paxAsset, sdk.NewInt(75)),
			feeLeg(programPrincipal, sidAsset, sdk.NewInt(75))}, "LXP_ASSET_MISMATCH"},
	} {
		t.Run(name, func(t *testing.T) {
			require.EqualError(t, s.run(newCall(tc.legs...), webRequest(3)), tc.code)
			s.requireNoRecord(3)
			require.Equal(t, programBalance, s.balanceOf(programPrincipal, paxAsset))
			require.Equal(t, programBalance, s.balanceOf(programPrincipal, sidAsset))
			require.True(t, s.balanceOf(keeper.ProgramFeeAccount(paxAsset.id), paxAsset).IsZero())
			require.True(t, s.balanceOf(keeper.ProgramFeeAccount(sidAsset.id), sidAsset).IsZero())
			require.Zero(t, s.k.ProgramPendingCount(s.ctx))
		})
	}
	t.Run("legs_of_one_asset_sum", func(t *testing.T) {
		require.NoError(t, s.run(newCall(feeLeg(programPrincipal, paxAsset, sdk.NewInt(30)),
			keeper.ProgramLeg{From: programPrincipal, To: stranger, Asset: sidAsset.id, Denom: sidAsset.denom,
				Amount: sdk.NewInt(5)},
			feeLeg(programPrincipal, paxAsset, sdk.NewInt(45))), webRequest(3)))
		require.Equal(t, u128(sdk.NewInt(75)), s.raw(programP, 3)[98:114])
		require.Equal(t, sdk.NewInt(75), s.balanceOf(keeper.ProgramFeeAccount(paxAsset.id), paxAsset))
	})
}

func TestProgramOriginOnlyFromTheEmittingCall(t *testing.T) {
	s := newProgramSuite(t)
	t.Run("other_topics", func(t *testing.T) {
		for _, topic := range [][]byte{nil, []byte("PAXEERX_WEB_REQUEST_V"), []byte("PAXEERX_WEB_REQUEST_V2"),
			[]byte("paxeerx_web_request_v1"), []byte("PAXEERX_WEB_V1")} {
			require.NoError(t, s.run(newCall(feeLeg(programPrincipal, paxAsset, sdk.NewInt(75))),
				programEvent{topic, requestData(3, types.KindSearch, programPayload)}), string(topic))
		}
		require.Zero(t, s.pendingKeys(s.ctx))
		require.Zero(t, s.k.ProgramPendingCount(s.ctx))
	})
	t.Run("eoa_through_the_precompile", func(t *testing.T) {
		ctx := s.ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(s.ctx))
		precompile := xweb.NewPrecompileWithKeeper(&s.k, s.app.BankKeeper, &s.app.EvmKeeper)
		stateDB := state.NewDBImpl(ctx, &s.app.EvmKeeper, false)
		blockCtx, err := s.app.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
		require.NoError(t, err)
		cfg := evmtypes.DefaultChainConfig().EthereumConfig(s.app.EvmKeeper.ChainID(ctx))
		target := common.HexToAddress(xweb.XWebAddress)
		evm := vm.NewEVM(*blockCtx, stateDB, cfg, vm.Config{}, map[common.Address]vm.PrecompiledContract{
			target: precompile})
		eoa := requester
		selectors := [][]byte{{0xde, 0xad, 0xbe, 0xef}, {0x2d, 0x4b, 0xf7, 0xad}}
		for _, method := range precompile.GetABI().Methods {
			selectors = append(selectors, method.ID)
		}
		for _, selector := range selectors {
			for _, body := range [][]byte{nil, make([]byte, 1024), requestData(3, types.KindSearch, programPayload)} {
				input := append(append([]byte(nil), selector...), body...)
				_, _, _ = evm.Call(eoa, target, input, 5_000_000, uint256.NewInt(0))
			}
		}
		require.Zero(t, s.pendingKeys(stateDB.Ctx()))
		require.Zero(t, s.k.ProgramPendingCount(stateDB.Ctx()))
	})
}

func TestProgramFeeAccountDerivation(t *testing.T) {
	want, err := hex.DecodeString("346304114a1f8532c20ab94295f70a5adf1c802764cbb98d8731f6020c86d041")
	require.NoError(t, err)
	id := keeper.ProgramFeeAccountID(paxAsset.id)
	require.Equal(t, want, id[:])
	preimage := append([]byte("PAXEERX_WEB_FEES_V1"), paxAsset.id[:]...)
	require.Equal(t, sha256.Sum256(preimage), [32]byte(id))
	require.Equal(t, sdk.AccAddress(address.Module("layerx", want)), keeper.ProgramFeeAccount(paxAsset.id))
	require.NotEqual(t, keeper.ProgramFeeAccount(paxAsset.id), keeper.ProgramFeeAccount(sidAsset.id))
}

func TestProgramOriginAnyPositiveAmount(t *testing.T) {
	s := newProgramSuite(t)
	require.NoError(t, s.run(newCall(feeLeg(programPrincipal, usdcAsset, sdk.NewInt(1))), webRequest(1)))
	billion := sdk.NewInt(1_000_000_000)
	require.NoError(t, s.run(newCall(feeLeg(programPrincipal, sidAsset, billion)), webRequest(2)))
	require.Equal(t, u128(sdk.NewInt(1)), s.raw(programP, 1)[98:114])
	require.Equal(t, u128(billion), s.raw(programP, 2)[98:114])

	for _, height := range []int64{startHeight + int64(timeout), startHeight + 3600, startHeight + 1_000_000} {
		later := s.ctx.WithBlockHeight(height)
		for _, id := range []uint64{1, 2} {
			_, err := s.k.Refund(later, id)
			require.ErrorIs(t, err, types.ErrUnknownRequest)
		}
	}
	require.Equal(t, sdk.NewInt(1), s.balanceOf(keeper.ProgramFeeAccount(usdcAsset.id), usdcAsset))
	require.Equal(t, billion, s.balanceOf(keeper.ProgramFeeAccount(sidAsset.id), sidAsset))
	require.Equal(t, programBalance.Sub(sdk.NewInt(1)), s.balanceOf(programPrincipal, usdcAsset))
	require.Equal(t, programBalance.Sub(billion), s.balanceOf(programPrincipal, sidAsset))

	over := sdk.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 128))
	call := &keeper.ProgramCall{ProgramID: programP, Principal: programPrincipal,
		Legs: []keeper.ProgramLeg{feeLeg(programPrincipal, usdlAsset, over)}}
	require.EqualError(t, s.k.RequestFromProgram(s.ctx, call, []byte(keeper.ProgramRequestTopic),
		requestData(3, types.KindFetch, programPayload)), "LXP_OVERFLOW")
	s.requireNoRecord(3)
}

func TestProgramOriginRefusesADenomOutsideTheAllowlist(t *testing.T) {
	s := newProgramSuite(t, types.ProgramFeeDenomPAX)
	err := s.run(newCall(feeLeg(programPrincipal, usdcAsset, sdk.NewInt(75))), webRequest(3))
	require.EqualError(t, err, "LXP_ASSET_MISMATCH")
	s.requireNoRecord(3)
	require.Equal(t, programBalance, s.balanceOf(programPrincipal, usdcAsset))
	require.True(t, s.balanceOf(keeper.ProgramFeeAccount(usdcAsset.id), usdcAsset).IsZero())
	require.Zero(t, s.k.ProgramPendingCount(s.ctx))

	require.NoError(t, s.run(newCall(feeLeg(programPrincipal, paxAsset, sdk.NewInt(75))), webRequest(3)))
	require.Len(t, s.raw(programP, 3), 123)
}

func TestProgramOriginPayerIsThePrincipal(t *testing.T) {
	s := newProgramSuite(t)
	err := s.run(newCall(feeLeg(programFunding, paxAsset, sdk.NewInt(75))), webRequest(3))
	require.EqualError(t, err, "LXP_AUTH_SCOPE")
	s.requireNoRecord(3)
	require.Equal(t, programBalance, s.balanceOf(programFunding, paxAsset))
	require.True(t, s.balanceOf(keeper.ProgramFeeAccount(paxAsset.id), paxAsset).IsZero())

	funded := newCall(feeLeg(programFunding, paxAsset, sdk.NewInt(75)))
	funded.Principal = programFunding
	require.NoError(t, s.run(funded, webRequest(3)), "a funding account equal to the principal pays")
	require.Equal(t, programBalance.Sub(sdk.NewInt(75)), s.balanceOf(programFunding, paxAsset))
}

func TestProgramOriginRefundRefused(t *testing.T) {
	s := newProgramSuite(t)
	paid := sdk.NewInt(75)
	require.NoError(t, s.run(newCall(feeLeg(programPrincipal, paxAsset, paid)), webRequest(3)))
	feeAccount := keeper.ProgramFeeAccount(paxAsset.id)
	for _, height := range []int64{startHeight, startHeight + int64(timeout), startHeight + 3600} {
		_, err := s.k.Refund(s.ctx.WithBlockHeight(height), 3)
		require.ErrorIs(t, err, types.ErrUnknownRequest)
		name, ok := types.LXPName(err)
		require.True(t, ok)
		require.Equal(t, "LXP_XWEB_UNKNOWN_REQUEST", name)
		require.Equal(t, paid, s.balanceOf(feeAccount, paxAsset))
	}
	record, found, err := s.k.GetProgramPendingRecord(s.ctx.WithBlockHeight(startHeight+3600), programP, 3)
	require.NoError(t, err)
	require.True(t, found)
	require.False(t, record.Fulfilled)
	require.Equal(t, paid, record.Amount)

	id := s.request()
	before := s.balance(s.account(requester))
	_, err = s.k.Refund(s.ctx.WithBlockHeight(startHeight+int64(timeout)), id)
	require.NoError(t, err, "origin 1 keeps its timeout refund")
	require.Equal(t, before.Add(sdk.NewInt(fee)), s.balance(s.account(requester)))
	require.Equal(t, paid, s.balanceOf(feeAccount, paxAsset))
}

func TestProgramOriginPaused(t *testing.T) {
	s := newProgramSuite(t)
	id := s.request()
	require.NoError(t, s.k.Pause(s.ctx, types.MsgPause{Authority: authority}))
	requesterBefore := s.balance(s.account(requester))
	moduleBefore := s.balance(s.k.ModuleAddress())

	err := s.run(newCall(feeLeg(programPrincipal, paxAsset, sdk.NewInt(75))), webRequest(3))
	require.ErrorIs(t, err, types.ErrPaused)
	name, ok := types.LXPName(err)
	require.True(t, ok)
	require.Equal(t, "LXP_XWEB_PAUSED", name)
	s.requireNoRecord(3)
	require.Equal(t, programBalance, s.balanceOf(programPrincipal, paxAsset))
	require.True(t, s.balanceOf(keeper.ProgramFeeAccount(paxAsset.id), paxAsset).IsZero())

	_, err = s.k.Request(s.ctx, requester, types.KindFetch, payload, callbackGas, sdk.NewInt(fee))
	require.ErrorIs(t, err, types.ErrPaused)
	length := uint32(len(response))
	_, _, err = s.k.Fulfil(s.ctx, id, response, digest, length,
		s.signed(id, response, digest, length, s.attestors[0], s.attestors[1]))
	require.ErrorIs(t, err, types.ErrPaused)
	require.Equal(t, requesterBefore, s.balance(s.account(requester)))
	require.Equal(t, moduleBefore, s.balance(s.k.ModuleAddress()))

	require.NoError(t, s.k.Unpause(s.ctx, types.MsgUnpause{Authority: authority}))
	require.NoError(t, s.run(newCall(feeLeg(programPrincipal, paxAsset, sdk.NewInt(75))), webRequest(3)))
}

func TestProgramOriginPendingCapacity(t *testing.T) {
	s := newProgramSuite(t)
	for id := uint64(1); id <= keeper.ProgramPendingCapacity; id++ {
		require.NoError(t, s.run(newCall(feeLeg(programPrincipal, paxAsset, sdk.NewInt(1))), webRequest(id)))
	}
	require.Equal(t, uint64(256), s.k.ProgramPendingCount(s.ctx))
	err := s.run(newCall(feeLeg(programPrincipal, paxAsset, sdk.NewInt(1))), webRequest(257))
	require.EqualError(t, err, "LXP_ARENA_EXHAUSTED")
	s.requireNoRecord(257)
	require.Equal(t, uint64(256), s.k.ProgramPendingCount(s.ctx))
	require.Equal(t, sdk.NewInt(256), s.balanceOf(keeper.ProgramFeeAccount(paxAsset.id), paxAsset))
	require.Equal(t, 256, s.pendingKeys(s.ctx))
}
