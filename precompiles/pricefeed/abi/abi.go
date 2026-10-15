// Package abi exports the ABI of the price feed precompile at 0x1020 with no
// other import, so indexers decode its logs without importing the precompile.
package abi

import _ "embed"

// JSON is the content of precompiles/pricefeed/abi/abi.json.
//
//go:embed abi.json
var JSON string
