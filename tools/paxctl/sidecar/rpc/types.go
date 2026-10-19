package rpc

import "encoding/json"

// StatusResult is the inner "result" of the CometBFT /status response,
// after the JSON-RPC envelope has been stripped by Client.Get.
type StatusResult struct {
	NodeInfo NodeInfo `json:"node_info"`
	SyncInfo SyncInfo `json:"sync_info"`
}

// NodeInfo identifies a CometBFT node.
type NodeInfo struct {
	ID string `json:"id"`
	// Network is the chain ID the node is configured for; CometBFT's
	// protocol term. Compared against task params.chainId for the
	// chain-confusion guard in sidecar/tasks/sign_and_broadcast.go.
	Network string `json:"network"`
}

// SyncInfo reports chain sync state.
type SyncInfo struct {
	LatestBlockHeight string `json:"latest_block_height"`
	CatchingUp        bool   `json:"catching_up"`
}

// BlockResult is the inner "result" of the CometBFT /block response.
type BlockResult struct {
	BlockID BlockID `json:"block_id"`
	Block   Block   `json:"block"`
}

// BlockID identifies a block by hash.
type BlockID struct {
	Hash string `json:"hash"`
}

// Block holds the subset of block fields we need.
type Block struct {
	Header BlockHeader `json:"header"`
}

// BlockHeader holds consensus-critical header fields for comparison.
type BlockHeader struct {
	AppHash         string `json:"app_hash"`
	LastResultsHash string `json:"last_results_hash"`
}

// BlockResultsResult is the inner "result" of the CometBFT /block_results response.
type BlockResultsResult struct {
	TxsResults []TxResult `json:"txs_results"`
}

// TxResult holds a single transaction execution result.
type TxResult struct {
	Code      int             `json:"code"`
	Log       string          `json:"log"`
	GasUsed   string          `json:"gas_used"`
	GasWanted string          `json:"gas_wanted"`
	Events    json.RawMessage `json:"events"`
}
