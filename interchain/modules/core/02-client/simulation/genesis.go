package simulation

import (
	"math/rand"

	simtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/simulation"

	"github.com/Sidiora-Labs/Paxeer-X-Network/interchain/modules/core/02-client/types"
)

// GenClientGenesis returns the default client genesis state.
func GenClientGenesis(_ *rand.Rand, _ []simtypes.Account) types.GenesisState {
	return types.DefaultGenesisState()
}
