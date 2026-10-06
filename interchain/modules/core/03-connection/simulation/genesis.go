package simulation

import (
	"math/rand"

	simtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/simulation"

	"github.com/Sidiora-Labs/Paxeer-X-Network/interchain/modules/core/03-connection/types"
)

// GenConnectionGenesis returns the default connection genesis state.
func GenConnectionGenesis(_ *rand.Rand, _ []simtypes.Account) types.GenesisState {
	return types.DefaultGenesisState()
}
