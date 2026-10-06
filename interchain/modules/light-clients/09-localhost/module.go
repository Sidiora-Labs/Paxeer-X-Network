package localhost

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/interchain/modules/light-clients/09-localhost/types"
)

// Name returns the IBC client name
func Name() string {
	return types.SubModuleName
}
