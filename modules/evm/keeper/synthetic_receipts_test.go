package keeper_test

import (
	"math"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

var syntheticReceiptCaller = common.HexToAddress("0x00000000000000000000000000000000000ca11e")

// deploySyntheticLogEmitter installs runtime code that emits LOG1(topic) with empty data and stops.
func deploySyntheticLogEmitter(k *keeper.Keeper, ctx sdk.Context, addr common.Address, topic common.Hash) {
	code := append([]byte{0x7f}, topic.Bytes()...)
	code = append(code, 0x60, 0x00, 0x60, 0x00, 0xa1, 0x00)
	k.SetCode(ctx, addr, code)
}

func syntheticReceiptApp(t *testing.T, height int64) (*keeper.Keeper, sdk.Context) {
	a := app.Setup(t, false, false, false)
	k := &a.EvmKeeper
	ctx := a.GetContextForDeliverTx([]byte{}).WithBlockHeight(height)
	k.BeginBlock(ctx)
	return k, ctx
}

func TestVenueSyntheticReceiptHash(t *testing.T) {
	height := int64(41)
	heightBytes := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x29}
	want := map[keeper.SyntheticReceiptKind]string{
		keeper.SyntheticReceiptPerps:      "perps-endblock",
		keeper.SyntheticReceiptSpot:       "spot-endblock",
		keeper.SyntheticReceiptActivation: "v6120-activation",
	}
	require.Equal(t, keeper.SyntheticReceiptKind(1), keeper.SyntheticReceiptPerps)
	require.Equal(t, keeper.SyntheticReceiptKind(2), keeper.SyntheticReceiptSpot)
	require.Equal(t, keeper.SyntheticReceiptKind(3), keeper.SyntheticReceiptActivation)
	seen := map[common.Hash]keeper.SyntheticReceiptKind{}
	for kind, tag := range want {
		expected := crypto.Keccak256Hash(append([]byte(tag), heightBytes...))
		got := keeper.SyntheticReceiptHash(kind, height)
		require.Equal(t, expected, got, "kind %d", kind)
		require.NotEqual(t, got, keeper.SyntheticReceiptHash(kind, height+1), "kind %d", kind)
		_, dup := seen[got]
		require.False(t, dup)
		seen[got] = kind
		found, ok := keeper.SyntheticReceiptKindOf(got, height)
		require.True(t, ok)
		require.Equal(t, kind, found)
		_, ok = keeper.SyntheticReceiptKindOf(got, height+1)
		require.False(t, ok)
	}
	require.Equal(t, common.Hash{}, keeper.SyntheticReceiptHash(0, height))
	require.Equal(t, common.Hash{}, keeper.SyntheticReceiptHash(4, height))
	_, ok := keeper.SyntheticReceiptKindOf(common.Hash{}, height)
	require.False(t, ok)

	require.Equal(t, uint32(math.MaxUint32-2), keeper.SyntheticReceiptProvisionalIndex(keeper.SyntheticReceiptPerps))
	require.Equal(t, uint32(math.MaxUint32-1), keeper.SyntheticReceiptProvisionalIndex(keeper.SyntheticReceiptSpot))
	require.Equal(t, uint32(math.MaxUint32), keeper.SyntheticReceiptProvisionalIndex(keeper.SyntheticReceiptActivation))

	ctx := sdk.Context{}.WithBlockHeight(height).WithTxIndex(0)
	for kind := range want {
		sctx := keeper.SyntheticReceiptContext(ctx, kind)
		require.Equal(t, [32]byte(keeper.SyntheticReceiptHash(kind, height)), sctx.TxSum())
		require.Equal(t, int(math.MaxUint32-3+uint32(kind)), sctx.TxIndex())
		require.Equal(t, 0, ctx.TxIndex())
	}
}

func TestVenueSyntheticReceiptMerge(t *testing.T) {
	k, ctx := syntheticReceiptApp(t, 8)
	emitterA := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	emitterB := common.HexToAddress("0x00000000000000000000000000000000000000b2")
	topicA := common.HexToHash("0xaa")
	topicB := common.HexToHash("0xbb")
	deploySyntheticLogEmitter(k, ctx, emitterA, topicA)
	deploySyntheticLogEmitter(k, ctx, emitterB, topicB)

	msg := mockEVMTransactionMessage(t)
	etx, _ := msg.AsTransaction()
	k.SetMsgs([]*types.MsgEVMTransaction{msg})
	k.SetTxResults([]*abci.ExecTxResult{{Code: 0}})
	realBloom := ethtypes.Bloom{0x01}
	k.AppendToEvmTxDeferredInfo(ctx.WithTxIndex(0), realBloom, etx.Hash(), sdk.NewInt(7))

	sctx := keeper.SyntheticReceiptContext(ctx, keeper.SyntheticReceiptPerps)
	_, err := k.CallEVM(sctx, syntheticReceiptCaller, &emitterA, nil, nil)
	require.NoError(t, err)
	_, err = k.CallEVM(sctx, syntheticReceiptCaller, &emitterB, nil, nil)
	require.NoError(t, err)

	hash := keeper.SyntheticReceiptHash(keeper.SyntheticReceiptPerps, 8)
	provisional := uint64(keeper.SyntheticReceiptProvisionalIndex(keeper.SyntheticReceiptPerps))
	r, err := k.GetTransientReceipt(ctx, hash, provisional)
	require.NoError(t, err)
	require.Equal(t, hash.Hex(), r.TxHashHex)
	require.Equal(t, uint32(provisional), r.TransactionIndex)
	require.Equal(t, uint64(8), r.BlockNumber)
	require.Equal(t, uint64(0), r.EffectiveGasPrice)
	require.Len(t, r.Logs, 2)
	merged := map[common.Address][]string{}
	for _, l := range r.Logs {
		merged[common.HexToAddress(l.Address)] = l.Topics
	}
	require.Equal(t, map[common.Address][]string{emitterA: {topicA.Hex()}, emitterB: {topicB.Hex()}}, merged)
	_, err = k.GetTransientReceipt(ctx, hash, 0)
	require.Error(t, err)

	realInfo, found := k.GetEVMTxDeferredInfo(ctx.WithTxIndex(0))
	require.True(t, found)
	require.Equal(t, uint32(0), realInfo.TxIndex)
	require.Equal(t, etx.Hash().Bytes(), realInfo.TxHash)
	require.Equal(t, realBloom[:], realInfo.TxBloom)
	require.Equal(t, sdk.NewInt(7), realInfo.Surplus)

	synthetic, found := k.GetEVMTxDeferredInfo(sctx)
	require.True(t, found)
	require.Equal(t, hash.Bytes(), synthetic.TxHash)
	require.True(t, synthetic.Surplus.IsZero())

	all := k.GetAllEVMTxDeferredInfo(ctx)
	require.Len(t, all, 1)
	require.Equal(t, uint32(0), all[0].TxIndex)
	require.Equal(t, etx.Hash().Bytes(), all[0].TxHash)
}

func TestVenueSyntheticReceiptIndex(t *testing.T) {
	height := int64(9)
	k, ctx := syntheticReceiptApp(t, height)
	k.SetTxResults([]*abci.ExecTxResult{{Code: 0}, {Code: 0}, {Code: 0}})
	realHashes := []common.Hash{common.HexToHash("0x01"), common.HexToHash("0x02"), common.HexToHash("0x03")}
	for i, h := range realHashes {
		require.NoError(t, k.SetTransientReceipt(ctx, h, &types.Receipt{
			TxHashHex:        h.Hex(),
			TransactionIndex: uint32(i), //nolint:gosec
			BlockNumber:      uint64(height),
			GasUsed:          21000,
			Status:           1,
		}))
	}
	for i, kind := range keeper.SyntheticReceiptKinds {
		emitter := common.BytesToAddress([]byte{0xe0 + byte(i)})
		deploySyntheticLogEmitter(k, ctx, emitter, common.HexToHash("0xc0"))
		_, err := k.CallEVM(keeper.SyntheticReceiptContext(ctx, kind), syntheticReceiptCaller, &emitter, nil, nil)
		require.NoError(t, err)
	}
	require.NoError(t, k.FlushTransientReceipts(ctx))

	waitReceipt := func(h common.Hash) *types.Receipt {
		var r *types.Receipt
		require.Eventually(t, func() bool {
			got, err := k.GetReceipt(ctx, h)
			if err != nil {
				return false
			}
			r = got
			return true
		}, 5*time.Second, 10*time.Millisecond)
		return r
	}
	for i, h := range realHashes {
		require.Equal(t, uint32(i), waitReceipt(h).TransactionIndex) //nolint:gosec
	}
	wantIndex := map[keeper.SyntheticReceiptKind]uint32{
		keeper.SyntheticReceiptPerps:      3,
		keeper.SyntheticReceiptSpot:       4,
		keeper.SyntheticReceiptActivation: 5,
	}
	for _, kind := range keeper.SyntheticReceiptKinds {
		hash := keeper.SyntheticReceiptHash(kind, height)
		r := waitReceipt(hash)
		require.Equal(t, wantIndex[kind], r.TransactionIndex, "kind %d", kind)
		logs := keeper.GetLogsForTx(r, 0)
		require.Len(t, logs, 1)
		for _, l := range logs {
			require.Equal(t, uint(wantIndex[kind]), l.TxIndex)
			require.Equal(t, hash, l.TxHash)
		}
	}
}

func TestVenueSyntheticReceiptBloom(t *testing.T) {
	k, ctx := syntheticReceiptApp(t, 8)
	emitter := common.HexToAddress("0x00000000000000000000000000000000000000d4")
	topic := common.HexToHash("0xd00d")
	deploySyntheticLogEmitter(k, ctx, emitter, topic)
	_, err := k.CallEVM(keeper.SyntheticReceiptContext(ctx, keeper.SyntheticReceiptPerps), syntheticReceiptCaller, &emitter, nil, nil)
	require.NoError(t, err)

	k.EndBlock(ctx, 0, 0)

	for name, bloom := range map[string]ethtypes.Bloom{"block": k.GetBlockBloom(ctx), "evm-only": k.GetEvmOnlyBlockBloom(ctx)} {
		require.True(t, ethtypes.BloomLookup(bloom, emitter), name)
		require.True(t, ethtypes.BloomLookup(bloom, topic), name)
		require.False(t, ethtypes.BloomLookup(bloom, common.HexToAddress("0x00000000000000000000000000000000000000d5")), name)
	}
}
