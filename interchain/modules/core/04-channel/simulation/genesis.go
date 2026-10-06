package simulation

import (
	"math/rand"

	simtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/simulation"

	"github.com/Sidiora-Labs/Paxeer-X-Network/interchain/modules/core/04-channel/types"
)

// GenChannelGenesis returns the default channel genesis state.
func GenChannelGenesis(_ *rand.Rand, _ []simtypes.Account) types.GenesisState {
	return types.DefaultGenesisState()
}
