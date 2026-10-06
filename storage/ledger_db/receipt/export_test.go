package receipt

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	types2 "github.com/Sidiora-Labs/Paxeer-X-Network/storage/db_engine/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// RecoverReceiptStore exposes recoverReceiptStore for testing.
func RecoverReceiptStore(changelogPath string, db types2.StateStore) error {
	return recoverReceiptStore(changelogPath, db)
}

// GetLogsForTx exposes getLogsForTx for testing.
func GetLogsForTx(receipt *types.Receipt, logStartIndex uint) []*ethtypes.Log {
	return getLogsForTx(receipt, logStartIndex)
}
