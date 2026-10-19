package keeper_test

import (
	"encoding/binary"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	receiptstore "github.com/Sidiora-Labs/Paxeer-X-Network/storage/ledger_db/receipt"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func restingReceipt(t *testing.T, ctx sdk.Context) *evmtypes.Receipt {
	t.Helper()
	receipt, err := testkeeper.EVMTestApp.EvmKeeper.GetTransientReceipt(ctx,
		evmkeeper.SyntheticReceiptHash(evmkeeper.SyntheticReceiptSpot, ctx.BlockHeight()),
		uint64(evmkeeper.SyntheticReceiptProvisionalIndex(evmkeeper.SyntheticReceiptSpot)))
	require.NoError(t, err)
	return receipt
}

func restingEventLogs(receipt *evmtypes.Receipt, address common.Address, signature string) []*ethtypes.Log {
	var out []*ethtypes.Log
	topic := crypto.Keccak256Hash([]byte(signature))
	for _, log := range evmkeeper.GetLogsForTx(receipt, 0) {
		if log.Address == address && len(log.Topics) > 0 && log.Topics[0] == topic {
			out = append(out, log)
		}
	}
	return out
}

func TestEndBlockFillsLimitOrder(t *testing.T) {
	e := newLimitEnv(t)
	e.price(limitPrice(600))
	id := e.place(types.OrderTypeLimitBuy, 100_000_000, 500, 0)
	before := e.balance(e.token, e.user)
	e.price(limitPrice(500))
	require.NoError(t, spot.EndBlocker(e.ctx, e.k))
	require.Equal(t, types.OrderStatusExecuted, e.order(id).Status)
	require.Equal(t, "199800000", e.balance(e.token, e.user).Sub(before).String())
	logs := restingEventLogs(restingReceipt(t, e.ctx), common.HexToAddress(types.PECOROrdersAddress), "LimitOrderExecuted(uint256,uint256)")
	require.Len(t, logs, 1)
	require.Equal(t, common.BigToHash(big.NewInt(1)), logs[0].Topics[1])
	require.Equal(t, common.LeftPadBytes(limitPrice(500).BigInt().Bytes(), 32), logs[0].Data)
	require.Equal(t, uint64(e.ctx.BlockHeight()), logs[0].BlockNumber)
	require.False(t, e.ctx.KVStore(e.key).Has(limitKey(types.FillBackoffPrefix, limitID(id))))
}

func TestEndBlockOrder(t *testing.T) {
	f := newStopFixture(t)
	f.params.OrderPriceImpactEnabled = false
	f.setParams()
	id, err := f.k.PlaceStopLimit(f.ctx, f.user, f.stable, f.base, sdk.NewInt(1_000_000_000), stopTestPrice(45), stopTestPrice(40), 0, types.OrderTypeStopLimitSell)
	require.NoError(t, err)
	before := f.quoteBalance()
	f.setPrice(45)
	work, err := f.k.ExecuteResting(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), work.Samples)
	require.Equal(t, uint64(1), work.Activated)
	require.Equal(t, uint64(1), work.Filled)
	order, err := f.k.GetStopLimitOrder(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusExecuted, order.Status)
	require.Equal(t, "449550000", f.quoteBalance().Sub(before).String())
	receipt := restingReceipt(t, f.ctx)
	rail := common.HexToAddress(types.PECORStopOrdersAddress)
	activated := restingEventLogs(receipt, rail, "StopLimitActivated(uint256,uint256)")
	executed := restingEventLogs(receipt, rail, "StopLimitExecuted(uint256,uint256)")
	require.Len(t, activated, 1)
	require.Len(t, executed, 1)
	require.Less(t, activated[0].Index, executed[0].Index)
	require.Equal(t, common.LeftPadBytes(stopTestPrice(45).BigInt().Bytes(), 32), executed[0].Data)
	transfers := restingEventLogs(receipt, f.stable, "Transfer(address,address,uint256)")
	require.Len(t, transfers, 1)
	require.Equal(t, common.BytesToHash(f.k.CustodyEVMAddress(f.ctx).Bytes()), transfers[0].Topics[1])
	require.Equal(t, common.BytesToHash(f.user.Bytes()), transfers[0].Topics[2])
	require.Equal(t, common.LeftPadBytes(big.NewInt(449_550_000).Bytes(), 32), transfers[0].Data)
}

func TestEndBlockSkipsFailingFill(t *testing.T) {
	e := newLimitEnv(t)
	params, err := e.k.GetParams(e.ctx)
	require.NoError(t, err)
	params.OrderPriceImpactEnabled = false
	e.put(types.ParamsKey, params)
	first := e.place(types.OrderTypeLimitBuy, 1_000_000_000, 500, 0)
	second := e.place(types.OrderTypeLimitBuy, 100_000_000, 500, 0)
	book, err := e.k.GetBook(e.ctx, e.token)
	require.NoError(t, err)
	book.Reserves = sdk.NewInt(300_000_000)
	e.put(limitKey(types.BookPrefix, e.token.Bytes()), book)
	original := e.order(first)
	work, err := e.k.ExecuteResting(e.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), work.FillAttempts)
	require.Equal(t, uint64(1), work.Filled)
	require.Equal(t, original, e.order(first))
	require.Equal(t, types.OrderStatusExecuted, e.order(second).Status)
	backoff, err := e.k.FillBackoff(e.ctx, first)
	require.NoError(t, err)
	require.Equal(t, types.FillBackoff{OrderID: first, Failures: 1, NextRetryHeight: e.ctx.BlockHeight() + 2}, backoff)
	escrow, err := e.k.GetEscrow(e.ctx, e.stable)
	require.NoError(t, err)
	require.Equal(t, "1000000000", escrow.String())
	logs := restingEventLogs(restingReceipt(t, e.ctx), common.HexToAddress(types.PECOROrdersAddress), "LimitOrderExecuted(uint256,uint256)")
	require.Len(t, logs, 1)
	require.Equal(t, common.BigToHash(new(big.Int).SetUint64(second)), logs[0].Topics[1])
	e.ctx = e.ctx.WithBlockHeight(e.ctx.BlockHeight() + 1).WithBlockTime(e.ctx.BlockTime().Add(time.Second))
	e.price(limitPrice(500))
	work, err = e.k.ExecuteResting(e.ctx)
	require.NoError(t, err)
	require.Zero(t, work.FillAttempts)
	after, err := e.k.FillBackoff(e.ctx, first)
	require.NoError(t, err)
	require.Equal(t, backoff, after)
	e.ctx = e.ctx.WithBlockHeight(e.ctx.BlockHeight() + 1).WithBlockTime(e.ctx.BlockTime().Add(time.Second))
	e.price(limitPrice(500))
	work, err = e.k.ExecuteResting(e.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), work.FillAttempts)
	after, err = e.k.FillBackoff(e.ctx, first)
	require.NoError(t, err)
	require.Equal(t, types.FillBackoff{OrderID: first, Failures: 2, NextRetryHeight: e.ctx.BlockHeight() + 4}, after)
}

func TestEndBlockFillCap(t *testing.T) {
	e := newLimitEnv(t)
	params, err := e.k.GetParams(e.ctx)
	require.NoError(t, err)
	params.MaxFillsPerBlock = 2
	e.put(types.ParamsKey, params)
	for i := 0; i < 3; i++ {
		e.place(types.OrderTypeLimitBuy, 100_000_000, 500, 0)
	}
	work, err := e.k.ExecuteResting(e.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), work.Filled)
	require.Equal(t, types.OrderStatusExecuted, e.order(1).Status)
	require.Equal(t, types.OrderStatusExecuted, e.order(2).Status)
	require.Equal(t, types.OrderStatusPending, e.order(3).Status)
	cursor := e.ctx.KVStore(e.key).Get(limitKey(types.CursorPrefix, []byte{1}))
	require.Equal(t, append([]byte{byte(types.FillIndexLimit)}, limitID(2)...), cursor)
	e.ctx = e.ctx.WithBlockHeight(e.ctx.BlockHeight() + 1).WithBlockTime(e.ctx.BlockTime().Add(time.Second))
	e.price(limitPrice(500))
	work, err = e.k.ExecuteResting(e.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), work.Filled)
	require.Equal(t, types.OrderStatusExecuted, e.order(3).Status)
	t.Run("wrap", func(t *testing.T) {
		e := newLimitEnv(t)
		params, err := e.k.GetParams(e.ctx)
		require.NoError(t, err)
		params.FillScanPerBlock = 1
		e.put(types.ParamsKey, params)
		e.place(types.OrderTypeLimitBuy, 100_000_000, 400, 0)
		e.place(types.OrderTypeLimitBuy, 100_000_000, 400, 0)
		for _, want := range []uint64{1, 2, 1} {
			work, err := e.k.ExecuteResting(e.ctx)
			require.NoError(t, err)
			require.Equal(t, uint64(1), work.FillsExamined)
			require.Zero(t, work.FillAttempts)
			cursor := e.ctx.KVStore(e.key).Get(limitKey(types.CursorPrefix, []byte{1}))
			require.Equal(t, want, binary.BigEndian.Uint64(cursor[1:]))
			e.ctx = e.ctx.WithBlockHeight(e.ctx.BlockHeight() + 1).WithBlockTime(e.ctx.BlockTime().Add(time.Second))
			e.price(limitPrice(500))
		}
	})
}

func TestEndBlockLogsAreQueryable(t *testing.T) {
	e := newLimitEnv(t)
	e.place(types.OrderTypeLimitBuy, 100_000_000, 500, 0)
	require.NoError(t, spot.EndBlocker(e.ctx, e.k))
	evm := &testkeeper.EVMTestApp.EvmKeeper
	evm.SetTxResults([]*abci.ExecTxResult{{Code: 0}, {Code: 0}, {Code: 0}})
	require.NoError(t, evm.FlushTransientReceipts(e.ctx))
	hash := crypto.Keccak256Hash(append([]byte("spot-endblock"), limitID(uint64(e.ctx.BlockHeight()))...))
	var receipt *evmtypes.Receipt
	require.Eventually(t, func() bool {
		var err error
		receipt, err = evm.GetReceipt(e.ctx, hash)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, uint32(4), receipt.TransactionIndex)
	require.Equal(t, hash.Hex(), receipt.TxHashHex)
	rail := common.HexToAddress(types.PECOROrdersAddress)
	logs := restingEventLogs(receipt, rail, "LimitOrderExecuted(uint256,uint256)")
	require.Len(t, logs, 1)
	require.Equal(t, hash, logs[0].TxHash)
	require.Equal(t, uint(4), logs[0].TxIndex)
	require.Equal(t, uint64(e.ctx.BlockHeight()), logs[0].BlockNumber)
	var bloom ethtypes.Bloom
	bloom.SetBytes(receipt.LogsBloom)
	require.True(t, ethtypes.BloomLookup(bloom, rail))
	require.True(t, ethtypes.BloomLookup(bloom, logs[0].Topics[0]))
}

func TestTransactionFillPrecedesEndBlock(t *testing.T) {
	e := newLimitEnv(t)
	id := e.place(types.OrderTypeLimitBuy, 100_000_000, 500, 0)
	events, err := e.k.ExecuteLimit(e.ctx, id)
	require.NoError(t, err)
	require.Equal(t, "LimitOrderExecuted", events[len(events)-1].Name)
	before := e.balance(e.token, e.user)
	work, err := e.k.ExecuteResting(e.ctx)
	require.NoError(t, err)
	require.Zero(t, work.FillAttempts)
	require.Zero(t, work.Filled)
	require.Equal(t, before, e.balance(e.token, e.user))
	require.Equal(t, types.OrderStatusExecuted, e.order(id).Status)
	_, err = testkeeper.EVMTestApp.EvmKeeper.GetTransientReceipt(e.ctx,
		evmkeeper.SyntheticReceiptHash(evmkeeper.SyntheticReceiptSpot, e.ctx.BlockHeight()),
		uint64(evmkeeper.SyntheticReceiptProvisionalIndex(evmkeeper.SyntheticReceiptSpot)))
	require.ErrorIs(t, err, receiptstore.ErrNotFound)
}

func TestFillQualityRule(t *testing.T) {
	e := newLimitEnv(t)
	require.NoError(t, e.k.RecordSample(e.ctx, e.token))
	id := e.place(types.OrderTypeLimitSell, 1_000_000_000, 500, 0)
	params, err := e.k.GetParams(e.ctx)
	require.NoError(t, err)
	params.FillQualityTwapPeriod = 300
	params.MaxTwapDeviationBps = 200
	e.put(types.ParamsKey, params)
	e.ctx = e.ctx.WithBlockTime(e.ctx.BlockTime().Add(300 * time.Second)).WithBlockHeight(e.ctx.BlockHeight() + 1)
	e.price(limitPrice(515))
	before := e.snapshot()
	work, err := e.k.ExecuteResting(e.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), work.FillAttempts)
	require.Zero(t, work.Filled)
	require.Equal(t, types.OrderStatusPending, e.order(id).Status)
	after := e.snapshot()
	for key, value := range before {
		if len(key) > 0 && (key[0] == types.TwapSamplePrefix[0] || key[0] == types.TwapRingPrefix[0] || key[0] == types.CursorPrefix[0] || key[0] == types.FillBackoffPrefix[0]) {
			continue
		}
		require.Equal(t, value, after[key], "state changed at %x", key)
	}
	backoff, err := e.k.FillBackoff(e.ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.FillBackoff{OrderID: id, Failures: 1, NextRetryHeight: e.ctx.BlockHeight() + 2}, backoff)
	twap, err := e.k.TWAP(e.ctx, e.token, 300)
	require.NoError(t, err)
	require.Equal(t, limitPrice(500), twap)
}

func TestEndBlockBoundedWork(t *testing.T) {
	f := newStopFixture(t)
	f.params.OrderPriceImpactEnabled = false
	f.setParams()
	gs := types.DefaultGenesis()
	gs.Params = f.params
	gs.NextOrderID = 10_001
	for _, token := range []common.Address{f.stable, f.base} {
		book, err := f.k.GetBook(f.ctx, token)
		require.NoError(t, err)
		gs.Books = append(gs.Books, book)
	}
	for userID := uint64(1); userID <= 100; userID++ {
		user := common.BigToAddress(new(big.Int).SetUint64(userID))
		gs.ActiveCounts = append(gs.ActiveCounts, types.ActiveOrderCount{User: user, Count: 100})
		for j := uint64(0); j < 100; j++ {
			id := (userID-1)*100 + j + 1
			order := types.StopLimitOrder{ID: id, User: user, Stablecoin: f.stable, Token: f.base, Amount: sdk.NewInt(100_000_000), StopPrice: stopTestPrice(45), LimitPrice: stopTestPrice(40), OrderType: types.OrderTypeStopLimitSell, Status: types.OrderStatusPending, CreatedAt: uint64(f.ctx.BlockTime().Unix())}
			gs.StopLimitOrders = append(gs.StopLimitOrders, order)
			gs.ActiveStopLimitOrders = append(gs.ActiveStopLimitOrders, id)
			gs.UserOrders = append(gs.UserOrders, types.UserOrder{User: user, OrderID: id, OrderType: order.OrderType})
			gs.OrderMinOutputs = append(gs.OrderMinOutputs, types.OrderMinOutput{OrderID: id, MinOutput: sdk.ZeroInt()})
		}
	}
	gs.Escrows = []types.TokenEscrow{{Token: f.base, Amount: sdk.NewInt(1_000_000_000_000)}}
	require.NoError(t, gs.Validate())
	app := testkeeper.EVMTestApp
	require.NoError(t, app.BankKeeper.SendCoins(f.ctx, app.EvmKeeper.GetPaxAddressOrDefault(f.ctx, f.user), keeper.CustodyAddress(), sdk.NewCoins(sdk.NewInt64Coin("uhpx", 1_000_000_000_000))))
	f.k.InitGenesis(f.ctx, *gs)
	f.setPrice(45)
	work, err := f.k.ExecuteResting(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(500), work.ActivationsExamined)
	require.Equal(t, uint64(500), work.Activated)
	require.LessOrEqual(t, work.FillsExamined, uint64(500))
	require.Equal(t, uint64(200), work.Filled)
	require.Equal(t, uint64(200), work.FillAttempts)
	require.Zero(t, work.ExpiriesExamined)
	cursor := f.ctx.KVStore(f.key).Get(limitKey(types.CursorPrefix, []byte{0}))
	require.Equal(t, uint64(500), binary.BigEndian.Uint64(cursor))
	for _, tc := range []struct {
		id     uint64
		status types.OrderStatus
	}{{1, types.OrderStatusExecuted}, {200, types.OrderStatusExecuted}, {201, types.OrderStatusActivated}, {500, types.OrderStatusActivated}, {501, types.OrderStatusPending}, {10_000, types.OrderStatusPending}} {
		order, err := f.k.GetStopLimitOrder(f.ctx, tc.id)
		require.NoError(t, err)
		require.Equal(t, tc.status, order.Status)
	}
	f.ctx = f.ctx.WithBlockHeight(f.ctx.BlockHeight() + 1).WithBlockTime(f.ctx.BlockTime().Add(time.Second))
	f.setPrice(45)
	work, err = f.k.ExecuteResting(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(500), work.ActivationsExamined)
	cursor = f.ctx.KVStore(f.key).Get(limitKey(types.CursorPrefix, []byte{0}))
	require.Equal(t, uint64(1000), binary.BigEndian.Uint64(cursor))
	t.Run("expiry budget", func(t *testing.T) {
		e := newLimitEnv(t)
		params, err := e.k.GetParams(e.ctx)
		require.NoError(t, err)
		params.MaxExpiriesPerBlock = 2
		e.put(types.ParamsKey, params)
		for i := 0; i < 3; i++ {
			e.place(types.OrderTypeLimitBuy, 100_000_000, 500, 1001)
		}
		e.ctx = e.ctx.WithBlockTime(time.Unix(1001, 0).UTC())
		e.price(limitPrice(500))
		work, err := e.k.ExecuteResting(e.ctx)
		require.NoError(t, err)
		require.Equal(t, uint64(2), work.ExpiriesExamined)
		require.Equal(t, uint64(2), work.Expired)
		require.Zero(t, work.Filled)
		require.Equal(t, types.OrderStatusExpired, e.order(1).Status)
		require.Equal(t, types.OrderStatusExpired, e.order(2).Status)
		require.Equal(t, types.OrderStatusPending, e.order(3).Status)
		raw := e.ctx.KVStore(e.key).Get(limitKey(types.TwapRingPrefix, e.token.Bytes()))
		var ring types.TwapRing
		require.NoError(t, json.Unmarshal(raw, &ring))
		require.Equal(t, uint32(1), ring.Count)
	})
}
