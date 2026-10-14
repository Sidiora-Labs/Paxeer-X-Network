package bindings

import (
	sdkErrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

// Codes for wasm contract errors
var (
	DefaultCodespace = "wasmbinding"

	ErrParsingPaxWasmMsg = sdkErrors.Register(DefaultCodespace, 2, "Error parsing Pax Wasm Message")
)
