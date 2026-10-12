// Package layerxprograms is the precompile at 0x101D over the LayerX kernel
// programs module.
package layerxprograms

import (
	"embed"

	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/addr"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
)

//go:embed abi.json
var abiFS embed.FS

var programsABI = pcommon.MustGetABI(abiFS, "abi.json")

const (
	lifecycleSurcharge uint64 = 100000
	executionSurcharge uint64 = 50000
	// retireSurcharge covers the two archive-key ed25519 signatures of a
	// retirement certificate plus the proof and manifest work.
	retireSurcharge uint64 = 2*addr.LayerXBindSignatureGas + 20000
)

var (
	readGas  = pcommon.MethodGas{}
	writeGas = pcommon.MethodGas{Write: true}
)

// gasTable is the fixed per-method gas of every 0x101D selector: the KV read
// or write cost of the input from KVGasConfig plus the method's surcharge.
var gasTable = pcommon.GasTable{
	"deploy":           {Write: true, Surcharge: lifecycleSurcharge},
	"upgrade":          {Write: true, Surcharge: lifecycleSurcharge},
	"call":             {Write: true, Surcharge: executionSurcharge},
	"sandboxExecute":   {Write: true, Surcharge: executionSurcharge},
	"simulateCall":     {Surcharge: executionSurcharge},
	"retireStorage":    {Write: true, Surcharge: retireSurcharge},
	"registryRead":     writeGas,
	"transfer":         writeGas,
	"registerAccount":  writeGas,
	"setExitRoute":     writeGas,
	"deprecate":        writeGas,
	"tombstone":        writeGas,
	"exitValue":        writeGas,
	"exitValueBounded": writeGas,
	"sandboxFund":      writeGas,
	"sandboxActivate":  writeGas,

	"getProgram":                 readGas,
	"getInterface":               readGas,
	"getProgramOwner":            readGas,
	"getProgramAccount":          readGas,
	"getProgramAccountById":      readGas,
	"getAccountProfile":          readGas,
	"getValueAccount":            readGas,
	"getStateHead":               readGas,
	"getWindDown":                readGas,
	"getExitRoute":               readGas,
	"getSandbox":                 readGas,
	"getFeeSchedule":             readGas,
	"getCurrentFeeSchedule":      readGas,
	"getPendingFeeSchedule":      readGas,
	"getMeteringSchedule":        readGas,
	"getCurrentMeteringSchedule": readGas,
	"getWebRequest":              readGas,
	"getRetirementControl":       readGas,
	"quoteCallFee":               readGas,
}

// RequiredGas is the fixed charge of a call with calldata input: the selector
// is resolved through the ABI method lookup and charged its table row over
// the whole input. Input without a known selector is charged the read cost of
// the input and the call reverts as an unknown method.
func RequiredGas(input []byte) uint64 {
	if len(input) >= 4 {
		if method, err := programsABI.MethodById(input[:4]); err == nil {
			if row, ok := gasTable[method.Name]; ok {
				return row.Gas(input)
			}
		}
	}
	return readGas.Gas(input)
}
