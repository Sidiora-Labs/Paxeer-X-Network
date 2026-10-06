package solomachine

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/interchain/modules/light-clients/06-solomachine/types"
)

// Name returns the solo machine client name.
func Name() string {
	return types.SubModuleName
}
