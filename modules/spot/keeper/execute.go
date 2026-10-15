package keeper

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	receipt "github.com/Sidiora-Labs/Paxeer-X-Network/storage/ledger_db/receipt"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type RestingExecution struct {
	Samples, ExpiriesExamined, Expired, ActivationsExamined, Activated, FillsExamined, FillAttempts, Filled uint64
}

type restingReceipts interface {
	GetTransientReceipt(sdk.Context, common.Hash, uint64) (*evmtypes.Receipt, error)
	SetTransientReceipt(sdk.Context, common.Hash, *evmtypes.Receipt) error
}

func (k *Keeper) FillBackoff(ctx sdk.Context, id uint64) (types.FillBackoff, error) {
	entry := types.FillBackoff{OrderID: id}
	raw := k.store(ctx).Get(recordKey(types.FillBackoffPrefix, orderIDBytes(id)))
	if raw == nil {
		return entry, nil
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return entry, err
	}
	if entry.OrderID != id || entry.Failures == 0 || entry.NextRetryHeight <= 0 {
		return entry, fmt.Errorf("spot: invalid fill backoff")
	}
	return entry, nil
}

func (k *Keeper) restingFailure(ctx sdk.Context, id, cap uint64) error {
	entry, err := k.FillBackoff(ctx, id)
	if err != nil {
		return err
	}
	if entry.Failures < math.MaxUint32 {
		entry.Failures++
	}
	delay := cap
	if entry.Failures < 64 {
		delay = min(delay, uint64(1)<<entry.Failures)
	}
	if cap == 0 || ctx.BlockHeight() < 0 || delay > uint64(math.MaxInt64-ctx.BlockHeight()) {
		return fmt.Errorf("spot: retry height out of range")
	}
	entry.NextRetryHeight = ctx.BlockHeight() + int64(delay)
	return putOrderRecord(k.store(ctx), recordKey(types.FillBackoffPrefix, orderIDBytes(id)), entry)
}

func restingPrefix(index types.FillIndex) []byte {
	switch index {
	case types.FillIndexStopLimit:
		return types.ActiveStopLimitPrefix
	case types.FillIndexLimit:
		return types.ActiveLimitPrefix
	default:
		return types.ActiveStopLossPrefix
	}
}

func restingNext(store sdk.KVStore, prefix []byte, after uint64) (id uint64, member, end bool, err error) {
	if after == math.MaxUint64 {
		return 0, false, true, nil
	}
	it := store.Iterator(recordKey(prefix, orderIDBytes(after+1)), sdk.PrefixEndBytes(prefix))
	defer it.Close()
	if !it.Valid() {
		return 0, false, true, nil
	}
	key := it.Key()
	if len(key) < len(prefix)+8 {
		return 0, false, false, fmt.Errorf("spot: malformed active index")
	}
	id = binary.BigEndian.Uint64(key[len(prefix) : len(prefix)+8])
	return id, len(key) == len(prefix)+8, false, nil
}

func (k *Keeper) restingCursors(ctx sdk.Context) (types.EndBlockCursors, error) {
	var cursors types.EndBlockCursors
	var err error
	cursors.Activation, err = orderUint(k.store(ctx), recordKey(types.CursorPrefix, []byte{activationCursor}))
	if err != nil {
		return cursors, err
	}
	if raw := k.store(ctx).Get(recordKey(types.CursorPrefix, []byte{fillCursor})); raw != nil {
		if len(raw) != 9 || raw[0] > byte(types.FillIndexStopLoss) {
			return cursors, fmt.Errorf("spot: invalid fill cursor")
		}
		cursors.FillIndex, cursors.Fill = types.FillIndex(raw[0]), binary.BigEndian.Uint64(raw[1:])
	}
	return cursors, nil
}

func (k *Keeper) ExecuteResting(ctx sdk.Context) (RestingExecution, error) {
	var work RestingExecution
	if ctx.BlockHeight() < 0 || ctx.BlockTime().Unix() < 0 {
		return work, fmt.Errorf("spot: invalid block context")
	}
	if !k.store(ctx).Has(types.ParamsKey) {
		return work, nil
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return work, err
	}
	if params.MaxExpiriesPerBlock == 0 || params.ActivationScanPerBlock == 0 || params.FillScanPerBlock == 0 || params.MaxFillsPerBlock == 0 || params.MaxFillBackoffBlocks == 0 {
		return work, fmt.Errorf("spot: invalid execution budgets")
	}
	if _, ok := k.evmKeeper.(restingReceipts); !ok {
		return work, fmt.Errorf("spot: EVM keeper lacks synthetic receipt storage")
	}
	cursors, err := k.restingCursors(ctx)
	if err != nil {
		return work, err
	}
	ctx = evmkeeper.SyntheticReceiptContext(ctx, evmkeeper.SyntheticReceiptSpot)
	branch, write := ctx.CacheContext()
	for _, token := range params.Tokens {
		if token.IsStablecoin {
			continue
		}
		if err := k.RecordSample(branch, token.Address); err != nil {
			if errors.Is(err, types.ErrStalePrice) || errors.Is(err, types.ErrTokenPaused) {
				continue
			}
			return work, err
		}
		work.Samples++
	}
	if err := k.restingExpiries(branch, params, &work); err != nil {
		return work, err
	}
	if !params.OrdersPaused {
		if err := k.restingActivations(branch, params, &cursors, &work); err != nil {
			return work, err
		}
		if err := k.restingFills(branch, params, &cursors, &work); err != nil {
			return work, err
		}
	}
	k.store(branch).Set(recordKey(types.CursorPrefix, []byte{activationCursor}), orderIDBytes(cursors.Activation))
	k.store(branch).Set(recordKey(types.CursorPrefix, []byte{fillCursor}), append([]byte{byte(cursors.FillIndex)}, orderIDBytes(cursors.Fill)...))
	write()
	ctx.EventManager().EmitEvents(branch.EventManager().Events())
	return work, nil
}

func (k *Keeper) restingExpiries(ctx sdk.Context, params types.Params, work *RestingExecution) error {
	var entries []types.OrderExpiry
	it := sdk.KVStorePrefixIterator(k.store(ctx), types.ExpiryIndexPrefix)
	for ; it.Valid() && uint64(len(entries)) < params.MaxExpiriesPerBlock; it.Next() {
		key := it.Key()
		if len(key) != 17 {
			it.Close()
			return fmt.Errorf("spot: invalid expiry index")
		}
		expiry := types.OrderExpiry{ExpiresAt: binary.BigEndian.Uint64(key[1:9]), OrderID: binary.BigEndian.Uint64(key[9:])}
		if expiry.ExpiresAt > uint64(ctx.BlockTime().Unix()) {
			break
		}
		entries = append(entries, expiry)
	}
	it.Close()
	for _, entry := range entries {
		work.ExpiriesExamined++
		backoff, err := k.FillBackoff(ctx, entry.OrderID)
		if err != nil {
			return err
		}
		if backoff.NextRetryHeight > ctx.BlockHeight() {
			continue
		}
		branch, write := ctx.CacheContext()
		var events []LimitOrderEvent
		switch {
		case k.store(branch).Has(recordKey(types.LimitOrderPrefix, orderIDBytes(entry.OrderID))):
			events, err = k.ExpireLimit(branch, entry.OrderID)
		case k.store(branch).Has(recordKey(types.StopLimitOrderPrefix, orderIDBytes(entry.OrderID))):
			err = k.ExpireStopLimit(branch, entry.OrderID)
			events = []LimitOrderEvent{restingStopEvent("StopLimitCancelled", entry.OrderID)}
		case k.store(branch).Has(recordKey(types.StopLossOrderPrefix, orderIDBytes(entry.OrderID))):
			err = k.ExpireStopLoss(branch, entry.OrderID)
			events = []LimitOrderEvent{restingStopEvent("StopLossOrderCancelled", entry.OrderID)}
		default:
			return fmt.Errorf("spot: expiry order missing")
		}
		if err == nil {
			err = k.restingLogs(branch, events)
		}
		if err != nil {
			if err := k.restingFailure(ctx, entry.OrderID, params.MaxFillBackoffBlocks); err != nil {
				return err
			}
			continue
		}
		k.store(branch).Delete(recordKey(types.FillBackoffPrefix, orderIDBytes(entry.OrderID)))
		write()
		ctx.EventManager().EmitEvents(branch.EventManager().Events())
		work.Expired++
	}
	return nil
}

func (k *Keeper) restingActivations(ctx sdk.Context, params types.Params, cursors *types.EndBlockCursors, work *RestingExecution) error {
	start, wrapped := cursors.Activation, false
	for work.ActivationsExamined < params.ActivationScanPerBlock {
		id, member, end, err := restingNext(k.store(ctx), types.ActiveStopLimitPrefix, cursors.Activation)
		if err != nil {
			return err
		}
		if end {
			if wrapped || start == 0 {
				cursors.Activation = 0
				break
			}
			cursors.Activation = 0
			wrapped = true
			continue
		}
		if wrapped && id > start {
			break
		}
		cursors.Activation = id
		work.ActivationsExamined++
		if !member {
			continue
		}
		backoff, err := k.FillBackoff(ctx, id)
		if err != nil {
			return err
		}
		if backoff.NextRetryHeight > ctx.BlockHeight() {
			continue
		}
		order, err := k.GetStopLimitOrder(ctx, id)
		if err != nil {
			return err
		}
		if order.Status != types.OrderStatusPending || stopExpired(ctx, order.ExpiresAt) {
			continue
		}
		branch, write := ctx.CacheContext()
		activated, err := k.ActivateStopLimit(branch, id)
		if err != nil {
			if err := k.restingFailure(ctx, id, params.MaxFillBackoffBlocks); err != nil {
				return err
			}
			continue
		}
		if !activated {
			continue
		}
		price, err := k.IndexPrice(branch, order.Token)
		if err != nil {
			return err
		}
		if err := k.restingLogs(branch, []LimitOrderEvent{restingStopEvent("StopLimitActivated", id, price.BigInt())}); err != nil {
			return err
		}
		write()
		ctx.EventManager().EmitEvents(branch.EventManager().Events())
		work.Activated++
	}
	return nil
}

func (k *Keeper) restingFills(ctx sdk.Context, params types.Params, cursors *types.EndBlockCursors, work *RestingExecution) error {
	startIndex, start, wrapped := cursors.FillIndex, cursors.Fill, false
	for work.FillsExamined < params.FillScanPerBlock && work.Filled < params.MaxFillsPerBlock {
		id, member, end, err := restingNext(k.store(ctx), restingPrefix(cursors.FillIndex), cursors.Fill)
		if err != nil {
			return err
		}
		if end {
			if wrapped && cursors.FillIndex == startIndex {
				break
			}
			cursors.Fill = 0
			cursors.FillIndex = (cursors.FillIndex + 1) % 3
			if cursors.FillIndex == startIndex {
				if wrapped || start == 0 {
					break
				}
				wrapped = true
			}
			continue
		}
		if wrapped && cursors.FillIndex == startIndex && id > start {
			break
		}
		cursors.Fill = id
		work.FillsExamined++
		if !member {
			continue
		}
		backoff, err := k.FillBackoff(ctx, id)
		if err != nil {
			return err
		}
		if backoff.NextRetryHeight > ctx.BlockHeight() {
			continue
		}
		branch, write := ctx.CacheContext()
		filled, err := k.restingFill(branch, cursors.FillIndex, id)
		if err != nil {
			work.FillAttempts++
			if err := k.restingFailure(ctx, id, params.MaxFillBackoffBlocks); err != nil {
				return err
			}
			continue
		}
		if !filled {
			continue
		}
		k.store(branch).Delete(recordKey(types.FillBackoffPrefix, orderIDBytes(id)))
		write()
		ctx.EventManager().EmitEvents(branch.EventManager().Events())
		work.FillAttempts++
		work.Filled++
	}
	return nil
}

func (k *Keeper) restingFill(ctx sdk.Context, index types.FillIndex, id uint64) (bool, error) {
	var token, stable common.Address
	var target sdk.Int
	var buy bool
	var expiry uint64
	switch index {
	case types.FillIndexStopLimit:
		order, err := k.GetStopLimitOrder(ctx, id)
		if err != nil {
			return false, err
		}
		if order.ID == 0 || order.Status != types.OrderStatusActivated {
			return false, nil
		}
		token, stable, target, buy, expiry = order.Token, order.Stablecoin, order.LimitPrice, order.OrderType.IsBuy(), order.ExpiresAt
	case types.FillIndexLimit:
		order, err := k.GetLimitOrder(ctx, id)
		if err != nil {
			return false, err
		}
		if order.ID == 0 || !order.IsActive() {
			return false, nil
		}
		token, stable, target, buy, expiry = order.Token, order.Stablecoin, order.TargetPrice, order.OrderType.IsBuy(), order.ExpiresAt
	case types.FillIndexStopLoss:
		order, err := k.GetStopLossOrder(ctx, id)
		if err != nil {
			return false, err
		}
		if order.ID == 0 || !order.IsActive() {
			return false, nil
		}
		token, stable, target, expiry = order.Token, order.Stablecoin, order.TriggerPrice, order.ExpiresAt
	default:
		return false, fmt.Errorf("spot: invalid fill index")
	}
	if stopExpired(ctx, expiry) {
		return false, nil
	}
	price, err := k.IndexPrice(ctx, token)
	if err != nil {
		return false, err
	}
	below := buy || index == types.FillIndexStopLoss
	if below && price.GT(target) || !below && price.LT(target) {
		return false, nil
	}
	quality, err := k.FillQualityOK(ctx, token, price)
	if err != nil {
		return false, err
	}
	if !quality {
		return false, types.ErrStalePrice
	}
	var events []LimitOrderEvent
	if index == types.FillIndexLimit {
		events, err = k.ExecuteLimit(ctx, id)
	} else {
		input, output := token, stable
		if buy {
			input, output = stable, token
		}
		beforeIn, readErr := k.GetBook(ctx, input)
		if readErr != nil {
			return false, readErr
		}
		beforeOut, readErr := k.GetBook(ctx, output)
		if readErr != nil {
			return false, readErr
		}
		name := "StopLimitExecuted"
		if index == types.FillIndexStopLimit {
			err = k.ExecuteStopLimit(ctx, id)
		} else {
			err = k.ExecuteStopLoss(ctx, id)
			name = "StopLossOrderExecuted"
		}
		if err != nil {
			return false, err
		}
		afterIn, readErr := k.GetBook(ctx, input)
		if readErr != nil {
			return false, readErr
		}
		afterOut, readErr := k.GetBook(ctx, output)
		if readErr != nil {
			return false, readErr
		}
		events = []LimitOrderEvent{
			{Address: common.HexToAddress(types.PECORVaultAddress), Name: "ReservesUpdated", Args: []interface{}{input, beforeIn.Reserves.BigInt(), afterIn.Reserves.BigInt()}},
			{Address: common.HexToAddress(types.PECORVaultAddress), Name: "ReservesUpdated", Args: []interface{}{output, beforeOut.Reserves.BigInt(), afterOut.Reserves.BigInt()}},
			restingStopEvent(name, id, price.BigInt()),
		}
	}
	if err != nil {
		return false, err
	}
	if err := k.restingLogs(ctx, events); err != nil {
		return false, err
	}
	return true, nil
}

func restingStopEvent(name string, id uint64, words ...*big.Int) LimitOrderEvent {
	event := LimitOrderEvent{Address: common.HexToAddress(types.PECORStopOrdersAddress), Name: name, Args: []interface{}{new(big.Int).SetUint64(id)}}
	for _, word := range words {
		event.Args = append(event.Args, word)
	}
	return event
}

func restingLog(event LimitOrderEvent) (*ethtypes.Log, error) {
	var signature string
	var indexed common.Hash
	var words []interface{}
	switch event.Name {
	case "ReservesUpdated":
		if event.Address != common.HexToAddress(types.PECORVaultAddress) || len(event.Args) != 3 {
			return nil, fmt.Errorf("spot: invalid reserves event")
		}
		token, ok := event.Args[0].(common.Address)
		if !ok {
			return nil, fmt.Errorf("spot: invalid reserves token")
		}
		indexed = common.BytesToHash(token.Bytes())
		signature = "ReservesUpdated(address,uint256,uint256)"
		words = event.Args[1:]
	case "LimitOrderExecuted", "StopLimitActivated", "StopLimitExecuted", "StopLossOrderExecuted", "LimitOrderExpired", "StopLimitCancelled", "StopLossOrderCancelled":
		rail := common.HexToAddress(types.PECORStopOrdersAddress)
		if event.Name == "LimitOrderExecuted" || event.Name == "LimitOrderExpired" {
			rail = common.HexToAddress(types.PECOROrdersAddress)
		}
		count := 2
		if event.Name == "LimitOrderExpired" || event.Name == "StopLimitCancelled" || event.Name == "StopLossOrderCancelled" {
			count = 1
		}
		if event.Address != rail || len(event.Args) != count {
			return nil, fmt.Errorf("spot: invalid order event")
		}
		id, ok := event.Args[0].(*big.Int)
		if !ok || id == nil || id.Sign() <= 0 || id.BitLen() > 256 {
			return nil, fmt.Errorf("spot: invalid event id")
		}
		indexed = common.BigToHash(id)
		signature = event.Name + "(uint256)"
		if count == 2 {
			signature = event.Name + "(uint256,uint256)"
		}
		words = event.Args[1:]
	default:
		return nil, fmt.Errorf("spot: unsupported resting event %s", event.Name)
	}
	log := &ethtypes.Log{Address: event.Address, Topics: []common.Hash{crypto.Keccak256Hash([]byte(signature)), indexed}}
	for _, arg := range words {
		word, ok := arg.(*big.Int)
		if !ok || word == nil || word.Sign() < 0 || word.BitLen() > 256 {
			return nil, fmt.Errorf("spot: invalid event word")
		}
		log.Data = append(log.Data, common.LeftPadBytes(word.Bytes(), 32)...)
	}
	return log, nil
}

func (k *Keeper) restingLogs(ctx sdk.Context, events []LimitOrderEvent) error {
	if len(events) == 0 {
		return nil
	}
	receipts, ok := k.evmKeeper.(restingReceipts)
	if !ok {
		return fmt.Errorf("spot: EVM keeper lacks synthetic receipt storage")
	}
	hash := evmkeeper.SyntheticReceiptHash(evmkeeper.SyntheticReceiptSpot, ctx.BlockHeight())
	index := evmkeeper.SyntheticReceiptProvisionalIndex(evmkeeper.SyntheticReceiptSpot)
	rcpt, err := receipts.GetTransientReceipt(ctx, hash, uint64(index))
	if err != nil {
		if !errors.Is(err, receipt.ErrNotFound) {
			return err
		}
		rcpt = &evmtypes.Receipt{TxHashHex: hash.Hex(), BlockNumber: uint64(ctx.BlockHeight()), TransactionIndex: index, Status: uint32(ethtypes.ReceiptStatusSuccessful)}
	}
	for _, event := range events {
		log, err := restingLog(event)
		if err != nil {
			return err
		}
		rcpt.Logs = append(rcpt.Logs, evmkeeper.ConvertSyntheticEthLog(log))
	}
	if uint64(len(rcpt.Logs)) > math.MaxUint32 {
		return fmt.Errorf("spot: receipt log count exceeds uint32")
	}
	for i, log := range rcpt.Logs {
		log.Index = uint32(i)
	}
	bloom := ethtypes.CreateBloom(&ethtypes.Receipt{Logs: evmkeeper.GetLogsForTx(rcpt, 0)})
	rcpt.LogsBloom = bloom[:]
	return receipts.SetTransientReceipt(ctx, hash, rcpt)
}
