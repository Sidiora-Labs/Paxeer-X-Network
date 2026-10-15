package keeper

import (
	"errors"
	"fmt"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	receiptstore "github.com/Sidiora-Labs/Paxeer-X-Network/storage/ledger_db/receipt"
	"github.com/Sidiora-Labs/Paxeer-X-Network/utils"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// venueLogAddress is 0x1015, the address every perps log is emitted from.
var venueLogAddress = common.HexToAddress("0x0000000000000000000000000000000000001015")

// WriteEndBlockReceipt packs the records of one EndBlock pass as logs of
// 0x1015 and writes them as the block's perps synthetic receipt, hash
// SyntheticReceiptHash(SyntheticReceiptPerps, height) at the provisional
// index math.MaxUint32 - 2, which the flush rewrites to the block's
// transaction count. Logs already written under that receipt in the block are
// kept ahead of the new ones. A pass without records writes nothing, and a
// record that does not pack writes nothing.
func (k *Keeper) WriteEndBlockReceipt(ctx sdk.Context, records []types.EventRecord) error {
	if len(records) == 0 {
		return nil
	}
	logs := make([]*ethtypes.Log, len(records))
	for i, record := range records {
		topics, data, err := perpsabi.PackEvent(record.Name, record.Args...)
		if err != nil {
			return fmt.Errorf("pack endblock log %d: %w", i, err)
		}
		logs[i] = &ethtypes.Log{Address: venueLogAddress, Topics: topics, Data: data}
	}
	receiptCtx := evmkeeper.SyntheticReceiptContext(ctx, evmkeeper.SyntheticReceiptPerps)
	txHash := common.Hash(receiptCtx.TxSum())
	txIndex := uint32(receiptCtx.TxIndex()) //nolint:gosec

	receipt, err := k.evm.GetTransientReceipt(receiptCtx, txHash, uint64(txIndex))
	switch {
	case err == nil:
		receipt.Logs = append(receipt.Logs, utils.Map(logs, evmkeeper.ConvertSyntheticEthLog)...)
	case errors.Is(err, receiptstore.ErrNotFound):
		receipt = &evmtypes.Receipt{
			TxType:           ethtypes.LegacyTxType,
			TxHashHex:        txHash.Hex(),
			BlockNumber:      uint64(ctx.BlockHeight()), //nolint:gosec
			TransactionIndex: txIndex,
			From:             venueLogAddress.Hex(),
			Logs:             utils.Map(logs, evmkeeper.ConvertSyntheticEthLog),
			Status:           uint32(ethtypes.ReceiptStatusSuccessful),
		}
	default:
		return fmt.Errorf("load perps endblock receipt: %w", err)
	}
	for i, l := range receipt.Logs {
		l.Index = uint32(i) //nolint:gosec
	}
	bloom := ethtypes.CreateBloom(&ethtypes.Receipt{Logs: evmkeeper.GetLogsForTx(receipt, 0)})
	receipt.LogsBloom = bloom[:]
	if err := k.evm.SetTransientReceipt(receiptCtx, txHash, receipt); err != nil {
		return fmt.Errorf("persist perps endblock receipt: %w", err)
	}
	return nil
}
