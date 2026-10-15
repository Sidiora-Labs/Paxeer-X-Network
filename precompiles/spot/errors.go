package spot

import (
	"errors"
	"fmt"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/ethereum/go-ethereum/common"
)

// packError is the revert data of a failed spot call: the selector and packed
// arguments of the AbiError err carries (contracts/spot.md#errors), or the
// Error(string) encoding of RevertReason for an error without an ABI form.
func packError(err error) []byte {
	var abiErr *types.AbiError
	if errors.As(err, &abiErr) {
		return abiErr.Data()
	}
	return pcommon.RevertReason(err)
}

// checkFeeCollector is the collectFees authority check of rail, served under
// the ABI names of contracts/spot.md#errors: OwnableUnauthorizedAccount(caller)
// in place of the FEE_COLLECTOR_ROLE check on 0x1021, and NotFeeCollector() in
// place of the engine NotOrderOwner() on 0x1022 and 0x1023.
func checkFeeCollector(rail, caller, feeCollector common.Address) error {
	switch rail {
	case common.HexToAddress(types.PECORAddress):
		if caller != feeCollector {
			return types.OwnableUnauthorizedAccount(caller)
		}
	case common.HexToAddress(types.PECOROrdersAddress), common.HexToAddress(types.PECORStopOrdersAddress):
		if caller != feeCollector {
			return types.ErrNotFeeCollector
		}
	default:
		return fmt.Errorf("spot: collectFees is not served at %s", rail.Hex())
	}
	return nil
}
