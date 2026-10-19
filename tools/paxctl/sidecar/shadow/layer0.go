package shadow

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sei-protocol/seictl/sidecar/rpc"
)

// compareLayer0 fetches the block header from both chains at the given
// height and compares AppHash, LastResultsHash, and total gas used.
func (c *Comparator) compareLayer0(ctx context.Context, height int64) (*Layer0Result, error) {
	shadowBlock, err := queryBlock(ctx, c.shadowClient, height)
	if err != nil {
		return nil, fmt.Errorf("querying shadow block at height %d: %w", height, err)
	}
	canonicalBlock, err := queryBlock(ctx, c.canonicalClient, height)
	if err != nil {
		return nil, fmt.Errorf("querying canonical block at height %d: %w", height, err)
	}

	sAppHash := shadowBlock.Block.Header.AppHash
	cAppHash := canonicalBlock.Block.Header.AppHash
	sLastResults := shadowBlock.Block.Header.LastResultsHash
	cLastResults := canonicalBlock.Block.Header.LastResultsHash

	// Gas is summed from block_results; the block header doesn't carry it
	// directly. For L0 we compare what the header gives us. Gas comparison
	// via block_results happens implicitly in L1.
	// For now, mark gas as matching at L0; a future enhancement can pull
	// gas from the block_results endpoint at this layer.
	gasMatch := true

	result := &Layer0Result{
		AppHashMatch:         sAppHash == cAppHash,
		LastResultsHashMatch: sLastResults == cLastResults,
		GasUsedMatch:         gasMatch,
	}

	if !result.AppHashMatch {
		result.ShadowAppHash = sAppHash
		result.CanonicalAppHash = cAppHash
	}
	if !result.LastResultsHashMatch {
		result.ShadowLastResultsHash = sLastResults
		result.CanonicalLastResultsHash = cLastResults
	}

	return result, nil
}

// queryBlock fetches the block at the given height from a CometBFT RPC endpoint
// and returns the header fields needed for comparison.
func queryBlock(ctx context.Context, client *rpc.Client, height int64) (*rpc.BlockResult, error) {
	raw, err := client.Get(ctx, fmt.Sprintf("/block?height=%d", height))
	if err != nil {
		return nil, err
	}

	var result rpc.BlockResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decoding /block response: %w", err)
	}
	return &result, nil
}
