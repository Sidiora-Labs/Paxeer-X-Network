package perps

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
)

// logFromEvent packs one event the keeper collected during a call and emits
// it as a log of the precompile at address.
func logFromEvent(evm *vm.EVM, address common.Address, record types.EventRecord) error {
	topics, data, err := perpsabi.PackEvent(record.Name, record.Args...)
	if err != nil {
		return err
	}
	return pcommon.EmitEVMLog(evm, address, topics, data)
}
