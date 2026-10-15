package perps

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// EndBlocker runs the perps EndBlock pass of contracts/perps.md section 23
// once per block. A failed step is emitted as an error event by the pass and
// never halts the chain.
func EndBlocker(ctx sdk.Context, k keeper.Keeper) {
	k.EndBlock(ctx)
}
