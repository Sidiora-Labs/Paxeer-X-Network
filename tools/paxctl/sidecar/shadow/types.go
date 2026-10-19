// Package shadow provides block-by-block comparison between a shadow
// chain node and a canonical chain node. The comparison is layered:
//
//   - Layer 0: Block header hashes (AppHash, LastResultsHash, gas).
//     If these match, the block is identical and deeper layers are skipped.
//   - Layer 1: Transaction receipt comparison (status, gas, logs, etc.).
//     Run only when Layer 0 fails, to isolate which transactions diverged.
//   - Layer 2: State diff comparison — logical EVM state (storage/code/nonce)
//     for the keys a block touched, read via EVM RPC on both sides. The
//     load-bearing check for an AppHash-breaking migration shadow, where the
//     committed root diverges by design and only logical state can be compared.
//   - Layer 3: Execution trace comparison (future).
package shadow

import "encoding/json"

// CompareResult holds the comparison output for a single block.
type CompareResult struct {
	// Height is the block height that was compared.
	Height int64 `json:"height"`

	// Timestamp is the UTC time the comparison was performed.
	Timestamp string `json:"timestamp"`

	// Match is true when all checked layers agree between shadow and canonical.
	// In migration mode an expected AppHash divergence does not clear Match.
	Match bool `json:"match"`

	// MigrationMode records that this comparison treated AppHash divergence as
	// expected (an AppHash-breaking migration shadow), keying the verdict on
	// execution-results equivalence instead.
	MigrationMode bool `json:"migrationMode,omitempty"`

	// DivergenceLayer is the first layer that detected a mismatch.
	// Nil when Match is true.
	DivergenceLayer *int `json:"divergenceLayer,omitempty"`

	// Layer0 holds the block-level hash comparison. Always populated.
	Layer0 Layer0Result `json:"layer0"`

	// Layer1 holds the transaction receipt comparison.
	// Only populated when Layer 0 detected a divergence.
	Layer1 *Layer1Result `json:"layer1,omitempty"`

	// Layer2 holds the logical state-diff comparison. Populated only when a
	// state reader and key source are configured (see WithLayer2).
	Layer2 *Layer2Result `json:"layer2,omitempty"`
}

// Diverged returns true when the comparison detected a mismatch at any layer.
func (r *CompareResult) Diverged() bool {
	return !r.Match
}

// Layer0Result compares block-level hashes. This is the cheapest check;
// if all fields match, the block is identical and no further comparison
// is needed.
type Layer0Result struct {
	AppHashMatch         bool `json:"appHashMatch"`
	LastResultsHashMatch bool `json:"lastResultsHashMatch"`
	GasUsedMatch         bool `json:"gasUsedMatch"`

	// Raw values are included when there is a mismatch, for diagnostics.
	ShadowAppHash    string `json:"shadowAppHash,omitempty"`
	CanonicalAppHash string `json:"canonicalAppHash,omitempty"`

	ShadowLastResultsHash    string `json:"shadowLastResultsHash,omitempty"`
	CanonicalLastResultsHash string `json:"canonicalLastResultsHash,omitempty"`

	ShadowGasUsed    int64 `json:"shadowGasUsed,omitempty"`
	CanonicalGasUsed int64 `json:"canonicalGasUsed,omitempty"`
}

// Match returns true when all Layer 0 fields agree.
func (r Layer0Result) Match() bool {
	return r.AppHashMatch && r.LastResultsHashMatch && r.GasUsedMatch
}

// Layer1Result compares individual transaction receipts within a block.
// Only populated when Layer 0 fails.
type Layer1Result struct {
	// TotalTxs is the number of transactions in the block.
	TotalTxs int `json:"totalTxs"`

	// TxCountMatch is true when both chains have the same number of txs.
	TxCountMatch bool `json:"txCountMatch"`

	// Divergences lists the per-transaction differences found.
	Divergences []TxDivergence `json:"divergences,omitempty"`

	// Indeterminate is set when the receipt comparison could not run (RPC error).
	// In migration mode Layer 1 is a load-bearing check, so an indeterminate
	// Layer 1 forces the block to fail closed rather than pass silently.
	Indeterminate bool   `json:"indeterminate,omitempty"`
	Error         string `json:"error,omitempty"`
}

// TxDivergence records a mismatch for a single transaction within a block.
type TxDivergence struct {
	// TxIndex is the position of the transaction within the block.
	TxIndex int `json:"txIndex"`

	// Fields lists which receipt fields diverged.
	Fields []FieldDivergence `json:"fields"`
}

// FieldDivergence records a single field-level mismatch in a tx receipt.
type FieldDivergence struct {
	Field     string `json:"field"`
	Shadow    any    `json:"shadow"`
	Canonical any    `json:"canonical"`
}

// Layer2Result compares logical EVM state (storage slots, code, nonce) for the
// keys a block touched, read via EVM RPC on both chains. This is the logical-
// content truth check; it never compares the committed root, which is
// schedule-dependent for a migration shadow and so not a correctness oracle.
type Layer2Result struct {
	// AccountsChecked is the number of touched accounts compared.
	AccountsChecked int `json:"accountsChecked"`

	// KeysChecked is the number of individual state reads compared (storage
	// slots plus per-account balance/code/nonce checks).
	KeysChecked int `json:"keysChecked"`

	// Divergences lists the logical-state mismatches found.
	Divergences []StateDivergence `json:"divergences,omitempty"`

	// Indeterminate is set when the layer could not be evaluated (a key source
	// or state read failed). A migration shadow's load-bearing check could not
	// run, so the block must NOT be counted clean — Error carries the cause.
	Indeterminate bool   `json:"indeterminate,omitempty"`
	Error         string `json:"error,omitempty"`
}

// StateDivergence records a single logical-state mismatch between the shadow
// and canonical chains. Values are hex for legibility in reports.
type StateDivergence struct {
	Kind      string `json:"kind"` // storage | code | nonce
	Addr      string `json:"addr"`
	Slot      string `json:"slot,omitempty"` // set only for storage
	Shadow    string `json:"shadow"`
	Canonical string `json:"canonical"`
}

// DivergenceReport is a self-contained investigation artifact for a single
// app-hash divergence event. It includes the layered comparison result plus
// the full block and block_results from both chains, giving engineers all
// the context needed to diagnose why the shadow node diverged without
// querying external systems.
type DivergenceReport struct {
	Height     int64         `json:"height"`
	Timestamp  string        `json:"timestamp"`
	Comparison CompareResult `json:"comparison"`
	Shadow     ChainSnapshot `json:"shadow"`
	Canonical  ChainSnapshot `json:"canonical"`
}

// ChainSnapshot captures the raw RPC responses from one chain at a
// specific height. The JSON is preserved verbatim for offline analysis.
type ChainSnapshot struct {
	Block        json.RawMessage `json:"block"`
	BlockResults json.RawMessage `json:"blockResults"`
}
