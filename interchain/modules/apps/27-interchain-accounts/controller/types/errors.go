package types

import (
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

// ICA Controller sentinel errors
var (
	ErrControllerSubModuleDisabled = sdkerrors.Register(SubModuleName, 2, "controller submodule is disabled")
)
