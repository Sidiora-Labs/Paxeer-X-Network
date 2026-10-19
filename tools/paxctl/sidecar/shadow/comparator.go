package shadow

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/seictl/sidecar/rpc"
	"github.com/sei-protocol/seilog"
)

var log = seilog.NewLogger("seictl", "shadow")

// layer2Timeout bounds the per-block Layer 2 RPC fan-out (touched-key trace plus
// state reads on both chains) so one slow endpoint cannot stall the compare loop.
const layer2Timeout = 30 * time.Second

// Comparator performs block-by-block comparison between a shadow node and
// a canonical chain node via their RPC endpoints.
type Comparator struct {
	shadowClient    *rpc.Client
	canonicalClient *rpc.Client

	// migrationMode tunes the verdict for an AppHash-breaking migration shadow
	// (e.g. memiavl->flatkv): the shadow's AppHash diverges from canonical by
	// design every block, so AppHash mismatch is expected and informational.
	// The correctness signals become LastResultsHash + gas + per-tx receipts
	// (execution equivalence), and Layer 1 always runs.
	migrationMode bool

	// Layer 2 (logical state diff) runs only when all three are configured.
	shadowState    StateReader
	canonicalState StateReader
	keySource      KeySource
}

// Option configures a Comparator.
type Option func(*Comparator)

// WithMigrationMode treats AppHash divergence as expected (not a mismatch) and
// keys the verdict on execution-results equivalence. Use for a shadow running
// an AppHash-breaking state migration against an un-migrated canonical chain.
func WithMigrationMode() Option {
	return func(c *Comparator) { c.migrationMode = true }
}

// WithLayer2 enables logical state-diff comparison: the keySource yields the
// accounts/slots each block touched, and the two StateReaders (EVM RPC on the
// shadow and canonical chains) supply their logical values to compare.
func WithLayer2(shadowState, canonicalState StateReader, keySource KeySource) Option {
	return func(c *Comparator) {
		c.shadowState = shadowState
		c.canonicalState = canonicalState
		c.keySource = keySource
	}
}

// NewComparator creates a Comparator that queries shadowRPC for the local
// shadow node and canonicalRPC for the reference chain.
func NewComparator(shadowRPC, canonicalRPC string, opts ...Option) *Comparator {
	c := &Comparator{
		shadowClient:    rpc.NewClient(shadowRPC, nil),
		canonicalClient: rpc.NewClient(canonicalRPC, nil),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// CompareBlock performs a layered comparison for the given block height.
// Layer 0 (block headers) always runs. Layer 1 (transaction receipts) runs when
// a real divergence is detected, and always in migration mode — where AppHash,
// the cheap Layer 0 signal, is expected to differ, so the receipt check is the
// real correctness signal.
func (c *Comparator) CompareBlock(ctx context.Context, height int64) (*CompareResult, error) {
	result := &CompareResult{
		Height:        height,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Match:         true,
		MigrationMode: c.migrationMode,
	}

	// --- Layer 0: block header comparison ---
	l0, err := c.compareLayer0(ctx, height)
	if err != nil {
		return nil, err
	}
	result.Layer0 = *l0

	// In migration mode AppHash mismatch is expected; a real Layer 0 divergence
	// is a LastResultsHash mismatch (execution results differ). Per-tx gas is
	// compared in Layer 1; Layer 0 GasUsedMatch is not yet wired (stubbed true),
	// so it is deliberately not part of this verdict. Otherwise any Layer 0 field
	// mismatch (including AppHash) counts.
	// Note: LastResultsHash at height N reflects N-1 execution; the per-tx Layer 1
	// signal lands on the correct height, so attribution stays accurate.
	realL0Divergence := !l0.Match()
	if c.migrationMode {
		realL0Divergence = !l0.LastResultsHashMatch
	}

	// --- Layer 1: transaction receipt comparison ---
	if realL0Divergence || c.migrationMode {
		l1, err := c.compareLayer1(ctx, height)
		if err != nil {
			// In migration mode Layer 1 is load-bearing (AppHash is expected to
			// differ), so an error must fail closed, not silently pass. Outside
			// migration mode Layer 1 only runs after a confirmed Layer 0
			// divergence, so the block is already not clean and the error is
			// merely missing detail.
			log.Warn("layer 1 comparison failed", "height", height, "err", err)
			if c.migrationMode {
				result.Layer1 = &Layer1Result{Indeterminate: true, Error: err.Error()}
			}
		} else {
			result.Layer1 = l1
		}
	}
	l1Diverged := result.Layer1 != nil && len(result.Layer1.Divergences) > 0
	l1Indeterminate := result.Layer1 != nil && result.Layer1.Indeterminate

	// --- Layer 2: logical state diff (when configured) ---
	if c.layer2Enabled() && (realL0Divergence || c.migrationMode) {
		l2, err := c.compareLayer2(ctx, height)
		if err != nil {
			// Fail closed: the load-bearing check could not run, so this block is
			// NOT clean. Record it as indeterminate (forces Match=false below)
			// rather than silently passing on the layer that actually validates
			// the migration.
			log.Warn("layer 2 state comparison could not run; marking indeterminate",
				"height", height, "err", err)
			result.Layer2 = &Layer2Result{Indeterminate: true, Error: err.Error()}
		} else {
			result.Layer2 = l2
		}
	}
	l2Diverged := result.Layer2 != nil && len(result.Layer2.Divergences) > 0
	l2Indeterminate := result.Layer2 != nil && result.Layer2.Indeterminate

	// Attribute to the deepest (most specific) layer that fired: a Layer 1/2
	// divergence or indeterminate is more actionable than the Layer 0 header
	// mismatch that triggered the descent.
	switch {
	case l2Diverged || l2Indeterminate:
		result.Match = false
		layer := 2
		result.DivergenceLayer = &layer
	case l1Diverged || l1Indeterminate:
		result.Match = false
		layer := 1
		result.DivergenceLayer = &layer
	case realL0Divergence:
		result.Match = false
		layer := 0
		result.DivergenceLayer = &layer
	}

	return result, nil
}

func (c *Comparator) layer2Enabled() bool {
	return c.keySource != nil && c.shadowState != nil && c.canonicalState != nil
}

// compareLayer2 fetches the accounts a block touched and compares their logical
// state (balance/code/nonce/storage) between the shadow and canonical chains. It
// bounds the per-block RPC fan-out with a timeout so one slow endpoint cannot
// stall the compare loop.
//
// The CometBFT height is used directly as the EVM block number for the trace and
// the state reads. This holds because sei-chain maps an explicit EVM block number
// straight to the tendermint height (evmrpc getBlockNumber: identity, no offset).
// If a future sei-chain reintroduces an offset, that identity breaks and this
// must translate height -> EVM block number before the EVM calls.
func (c *Comparator) compareLayer2(ctx context.Context, height int64) (*Layer2Result, error) {
	ctx, cancel := context.WithTimeout(ctx, layer2Timeout)
	defer cancel()
	touched, err := c.keySource.TouchedAccounts(ctx, height)
	if err != nil {
		return nil, fmt.Errorf("resolving touched accounts at height %d: %w", height, err)
	}
	return compareState(ctx, height, touched, c.shadowState, c.canonicalState)
}

// Close releases resources held by configured Layer 2 readers / key source.
// go-ethereum's *ethclient.Client and *rpc.Client expose Close() with NO return,
// so they do not satisfy io.Closer — assert the no-return shape instead, or the
// connections leak silently.
func (c *Comparator) Close() {
	for _, r := range []any{c.shadowState, c.canonicalState, c.keySource} {
		if cl, ok := r.(interface{ Close() }); ok {
			cl.Close()
		}
	}
}
