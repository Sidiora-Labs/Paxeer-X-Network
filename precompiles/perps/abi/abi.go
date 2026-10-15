package abi

import (
	"bytes"
	_ "embed"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

//go:embed abi.json
var abiJSON []byte

// ABI returns the parsed ABI of the perpetuals precompile.
func ABI() (abi.ABI, error) {
	return abi.JSON(bytes.NewReader(abiJSON))
}
