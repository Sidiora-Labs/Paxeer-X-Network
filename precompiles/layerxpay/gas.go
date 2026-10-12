package layerxpay

import (
	"bytes"
	_ "embed"
	"math"

	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	storetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
)

//go:embed abi.json
var gasABIJSON []byte

var gasABI, gasABIError = abi.JSON(bytes.NewReader(gasABIJSON))

var methodGas = pcommon.GasTable{
	"escrowOpen":              writeGas(60000),
	"escrowCapture":           writeGas(60000),
	"escrowPartialCapture":    writeGas(60000),
	"escrowRelease":           writeGas(60000),
	"escrowTimeout":           writeGas(60000),
	"escrowDisputeOpen":       writeGas(30000),
	"escrowDisputeResolve":    writeGas(80000),
	"budgetCreate":            writeGas(50000),
	"budgetCreateNative":      writeGas(50000),
	"budgetFund":              writeGas(40000),
	"budgetFundNative":        writeGas(40000),
	"budgetAmend":             writeGas(30000),
	"budgetDelegateAdd":       writeGas(30000),
	"budgetDelegateRemove":    writeGas(30000),
	"budgetSpend":             writeGas(50000),
	"budgetClose":             writeGas(50000),
	"budgetDefund":            writeGas(50000),
	"budgetRevoke":            writeGas(50000),
	"budgetPull":              writeGas(58000),
	"budgetDelegateSpend":     writeGas(60000),
	"streamOpen":              writeGas(50000),
	"streamTopUp":             writeGas(50000),
	"streamMeter":             writeGas(38000),
	"streamSettle":            writeGas(60000),
	"streamPause":             writeGas(30000),
	"streamResume":            writeGas(30000),
	"streamClose":             writeGas(80000),
	"serviceOfferPublish":     writeGas(30000),
	"serviceOfferWithdraw":    writeGas(30000),
	"serviceAgreementPropose": writeGas(30000),
	"serviceAgreementAccept":  writeGas(40000),
	"serviceCommitTask":       writeGas(40000),
	"serviceCommitAbandon":    writeGas(40000),
	"serviceToolExecAttest":   writeGas(38000),
	"serviceProgressReport":   writeGas(30000),
	"serviceDeliver":          writeGas(50000),
	"serviceAccept":           writeGas(30000),
	"serviceReject":           writeGas(30000),
	"serviceDisputeOpen":      writeGas(50000),
	"serviceDisputeResolve":   writeGas(100000),
	"getEscrow":               {},
	"escrowRemaining":         {},
	"getEscrowResult":         {},
	"getBudget":               {},
	"budgetRemaining":         {},
	"getStream":               {},
	"streamAccrued":           {},
	"getStreamResult":         {},
	"getOffer":                {},
	"getAgreement":            {},
	"getCommitment":           {},
	"getExecution":            {},
	"getDelivery":             {},
	"latestDelivery":          {},
	"getProgress":             {},
	"progressHighWater":       {},
	"getDispute":              {},
}

func writeGas(flat uint64) pcommon.MethodGas {
	return pcommon.MethodGas{Write: true, Surcharge: flat - storetypes.KVGasConfig().WriteCostFlat}
}

func RequiredGas(input []byte) uint64 {
	if gasABIError != nil {
		return math.MaxUint64
	}
	if len(input) >= 4 {
		if method, err := gasABI.MethodById(input[:4]); err == nil {
			if row, ok := methodGas[method.Name]; ok {
				return gasForLength(row, uint64(len(input)))
			}
		}
	}
	var largest pcommon.MethodGas
	for _, row := range methodGas {
		if row.Write && (!largest.Write || row.Surcharge > largest.Surcharge) {
			largest = row
		}
	}
	return gasForLength(largest, uint64(len(input)))
}

func gasForLength(row pcommon.MethodGas, length uint64) uint64 {
	config := storetypes.KVGasConfig()
	flat, perByte := config.ReadCostFlat, config.ReadCostPerByte
	if row.Write {
		flat, perByte = config.WriteCostFlat, config.WriteCostPerByte
	}
	if row.Surcharge > math.MaxUint64-flat {
		return math.MaxUint64
	}
	flat += row.Surcharge
	if length > (math.MaxUint64-flat)/perByte {
		return math.MaxUint64
	}
	return flat + perByte*length
}
