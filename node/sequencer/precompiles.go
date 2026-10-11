package sequencer

import (
	"errors"
	"fmt"
	"math"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// ErrTxPosition refuses a position the sealed block cannot give a sequenced
// transaction.
var ErrTxPosition = errors.New("sequencer: transaction position outside the sealed block")

// PrecompileHost reselects the node-local precompile set from the state of a
// context; *node.App satisfies it.
type PrecompileHost interface {
	RefreshPrecompiles(ctx sdk.Context)
}

// SetTxPosition places ctx at txIndex, the index the transaction holds in the
// sealed block, the way ProcessBlock places every block transaction. Index 0
// holds MsgSequenceBatch and the receipt carries the index as a u32, so a
// sequenced position is 1..MaxUint32. The CosmWasm transaction counter is left
// as the block path leaves it: no ante decorator of the block path sets it, so
// a contract sees the same env.transaction at the sequencer and at every
// validator.
func SetTxPosition(ctx sdk.Context, txIndex int) (sdk.Context, error) {
	if txIndex < 1 || uint64(txIndex) > math.MaxUint32 {
		return ctx, fmt.Errorf("%w: index %d, want 1..%d", ErrTxPosition, txIndex, uint64(math.MaxUint32))
	}
	return ctx.WithTxIndex(txIndex), nil
}

// RefreshPrecompiles reselects host's precompile set against the open layer
// of exec, as it stands after BeginBlock and the transactions accepted so far,
// so the block serves the precompiles its own state makes live. It is called
// once after each Open.
func RefreshPrecompiles(host PrecompileHost, exec Executor) error {
	ctx, err := exec.Snapshot()
	if err != nil {
		return err
	}
	host.RefreshPrecompiles(ctx)
	return nil
}
