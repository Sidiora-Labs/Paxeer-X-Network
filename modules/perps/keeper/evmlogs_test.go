package keeper_test

import (
	"math"
	"math/big"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	receiptstore "github.com/Sidiora-Labs/Paxeer-X-Network/storage/ledger_db/receipt"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

var evmlogsVenueAddress = common.HexToAddress("0x0000000000000000000000000000000000001015")

func newEvmlogsEnv(t *testing.T, height int64, txCount int) (*keeper.Keeper, *evmkeeper.Keeper, sdk.Context) {
	t.Helper()
	testApp := app.Setup(t, false, false, false)
	evm := &testApp.EvmKeeper
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(height)
	evm.BeginBlock(ctx)
	results := make([]*abci.ExecTxResult, txCount)
	for i := range results {
		results[i] = &abci.ExecTxResult{Code: 0}
	}
	evm.SetTxResults(results)
	return keeper.NewKeeper(sdk.NewKVStoreKey(types.StoreKey), evm, nil), evm, ctx
}

func evmlogsRecords() []types.EventRecord {
	return []types.EventRecord{
		{Name: types.EventMarkPriceUpdated, Args: []any{big.NewInt(0), big.NewInt(50_000), big.NewInt(49_990), big.NewInt(50_010)}},
		{Name: types.EventOrderExpired, Args: []any{big.NewInt(4), common.HexToAddress("0x00000000000000000000000000000000000000a1"), big.NewInt(0)}},
	}
}

func evmlogsProvisional(t *testing.T, evm *evmkeeper.Keeper, ctx sdk.Context) (*evmtypes.Receipt, error) {
	t.Helper()
	return evm.GetTransientReceipt(ctx, evmkeeper.SyntheticReceiptHash(evmkeeper.SyntheticReceiptPerps, ctx.BlockHeight()), math.MaxUint32-2)
}

func evmlogsFlushed(t *testing.T, evm *evmkeeper.Keeper, ctx sdk.Context, hash common.Hash) *evmtypes.Receipt {
	t.Helper()
	require.NoError(t, evm.FlushTransientReceipts(ctx))
	var flushed *evmtypes.Receipt
	require.Eventually(t, func() bool {
		r, err := evm.GetReceipt(ctx, hash)
		if err != nil {
			return false
		}
		flushed = r
		return true
	}, 5*time.Second, 10*time.Millisecond)
	return flushed
}

func TestEvmlogsSyntheticReceipt(t *testing.T) {
	for _, txCount := range []int{5, 0} {
		height := int64(500)
		k, evm, ctx := newEvmlogsEnv(t, height, txCount)
		hash := evmkeeper.SyntheticReceiptHash(evmkeeper.SyntheticReceiptPerps, height)

		bad := append(evmlogsRecords(), types.EventRecord{Name: "NotAnEvent"})
		require.ErrorContains(t, k.WriteEndBlockReceipt(ctx, bad), "pack endblock log 2: perps: unknown event NotAnEvent")
		_, err := evmlogsProvisional(t, evm, ctx)
		require.ErrorIs(t, err, receiptstore.ErrNotFound)

		records := evmlogsRecords()
		records = append(records, records[0])
		require.NoError(t, k.WriteEndBlockReceipt(ctx, records[:1]))
		require.NoError(t, k.WriteEndBlockReceipt(ctx, records[1:]))

		r, err := evmlogsProvisional(t, evm, ctx)
		require.NoError(t, err)
		require.Equal(t, hash.Hex(), r.TxHashHex)
		require.Equal(t, uint32(math.MaxUint32-2), r.TransactionIndex)
		require.Equal(t, uint64(height), r.BlockNumber)
		require.Equal(t, uint32(evmtypes.ShellEVMTxType), r.TxType)
		require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), r.Status)
		require.Equal(t, evmlogsVenueAddress.Hex(), r.From)
		require.Equal(t, uint64(0), r.GasUsed)
		require.Len(t, r.Logs, 3)
		for i, record := range records {
			topics, data, err := perpsabi.PackEvent(record.Name, record.Args...)
			require.NoError(t, err)
			l := r.Logs[i]
			require.Equal(t, evmlogsVenueAddress.Hex(), l.Address)
			require.Equal(t, uint32(i), l.Index) //nolint:gosec
			require.True(t, l.Synthetic)
			require.Equal(t, data, l.Data)
			require.Len(t, l.Topics, len(topics))
			for j, topic := range topics {
				require.Equal(t, topic.Hex(), l.Topics[j])
			}
		}
		abiDef, err := perpsabi.ABI()
		require.NoError(t, err)
		require.Equal(t, abiDef.Events[types.EventMarkPriceUpdated].ID.Hex(), r.Logs[0].Topics[0])
		require.Equal(t, abiDef.Events[types.EventOrderExpired].ID.Hex(), r.Logs[1].Topics[0])
		require.Equal(t, common.BytesToHash(common.HexToAddress("0x00000000000000000000000000000000000000a1").Bytes()).Hex(), r.Logs[1].Topics[2])
		bloom := ethtypes.CreateBloom(&ethtypes.Receipt{Logs: evmkeeper.GetLogsForTx(r, 0)})
		require.Equal(t, bloom[:], r.LogsBloom)
		require.True(t, ethtypes.BytesToBloom(r.LogsBloom).Test(evmlogsVenueAddress.Bytes()))

		flushed := evmlogsFlushed(t, evm, ctx, hash)
		require.Equal(t, uint32(txCount), flushed.TransactionIndex) //nolint:gosec
		logs := evmkeeper.GetLogsForTx(flushed, 0)
		require.Len(t, logs, 3)
		for i, l := range logs {
			require.Equal(t, uint(txCount), l.TxIndex)
			require.Equal(t, uint(i), l.Index)
			require.Equal(t, hash, l.TxHash)
			require.Equal(t, evmlogsVenueAddress, l.Address)
		}
	}
}

func TestEvmlogsDeterministicHash(t *testing.T) {
	height := int64(0x0102030405)
	preimage := append([]byte("perps-endblock"), 0x00, 0x00, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05)
	want := crypto.Keccak256Hash(preimage)
	require.Equal(t, want, evmkeeper.SyntheticReceiptHash(evmkeeper.SyntheticReceiptPerps, height))

	write := func() []byte {
		k, evm, ctx := newEvmlogsEnv(t, height, 1)
		require.NoError(t, k.WriteEndBlockReceipt(ctx, evmlogsRecords()))
		r, err := evm.GetTransientReceipt(ctx, want, math.MaxUint32-2)
		require.NoError(t, err)
		require.Equal(t, want.Hex(), r.TxHashHex)
		bz, err := r.Marshal()
		require.NoError(t, err)
		return bz
	}
	require.Equal(t, write(), write())

	k, evm, ctx := newEvmlogsEnv(t, height+1, 1)
	require.NoError(t, k.WriteEndBlockReceipt(ctx, evmlogsRecords()))
	_, err := evm.GetTransientReceipt(ctx, want, math.MaxUint32-2)
	require.ErrorIs(t, err, receiptstore.ErrNotFound)
	next, err := evmlogsProvisional(t, evm, ctx)
	require.NoError(t, err)
	require.NotEqual(t, want.Hex(), next.TxHashHex)
	require.Equal(t, evmkeeper.SyntheticReceiptHash(evmkeeper.SyntheticReceiptPerps, height+1).Hex(), next.TxHashHex)
}

func TestEvmlogsEmptyBlockNoReceipt(t *testing.T) {
	height := int64(21)
	k, evm, ctx := newEvmlogsEnv(t, height, 0)
	require.NoError(t, k.WriteEndBlockReceipt(ctx, nil))
	require.NoError(t, k.WriteEndBlockReceipt(ctx, []types.EventRecord{}))
	_, err := evmlogsProvisional(t, evm, ctx)
	require.ErrorIs(t, err, receiptstore.ErrNotFound)

	require.NoError(t, evm.FlushTransientReceipts(ctx))
	_, err = evm.GetReceipt(ctx, evmkeeper.SyntheticReceiptHash(evmkeeper.SyntheticReceiptPerps, height))
	require.Error(t, err)
}
