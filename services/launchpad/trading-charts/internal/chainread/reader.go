// Package chainread reads Paxeer chain state bound to node-reported block
// identities. Paxeer (chain 125) is a CometBFT-backed EVM: the block hash its
// nodes report — and the indexer records in every source position — is the
// consensus hash, not the Keccak of the Ethereum header encoding, so
// go-ethereum's types.Header.Hash() names no block there and EIP-1898 reads
// pinned to it fail. Reads here are pinned by height and bracketed by the
// reported hash instead.
package chainread

import (
	"context"
	"errors"
	"math/big"
	"regexp"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ErrUnavailable means the node does not serve the requested block or state.
var ErrUnavailable = errors.New("chainread: block unavailable")

// ErrConflict means the node's block identity disagrees with the expected one.
var ErrConflict = errors.New("chainread: block identity conflict")

// MaxCalls bounds one pinned read.
const MaxCalls = 8

var hashPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)

// Block is a block's identity as the node reports it; Hash is lower case.
type Block struct {
	Height int64
	Hash   string
	Time   int64
}

// Reader reads through one JSON-RPC endpoint.
type Reader struct{ RPC *ethclient.Client }

// Block returns the node-reported identity of the block at height.
func (r Reader) Block(ctx context.Context, height int64) (Block, error) {
	if r.RPC == nil || height < 0 {
		return Block{}, ErrUnavailable
	}
	var raw *struct {
		Number    string `json:"number"`
		Hash      string `json:"hash"`
		Timestamp string `json:"timestamp"`
	}
	if err := r.RPC.Client().CallContext(ctx, &raw, "eth_getBlockByNumber", hexutil.EncodeBig(big.NewInt(height)), false); err != nil {
		return Block{}, err
	}
	if raw == nil || raw.Hash == "" {
		return Block{}, ErrUnavailable
	}
	number, err := hexutil.DecodeUint64(raw.Number)
	if err != nil || number != uint64(height) || !hashPattern.MatchString(raw.Hash) {
		return Block{}, ErrConflict
	}
	timestamp, err := hexutil.DecodeUint64(raw.Timestamp)
	if err != nil || timestamp > uint64(^uint64(0)>>1) {
		return Block{}, ErrConflict
	}
	return Block{Height: height, Hash: strings.ToLower(raw.Hash), Time: int64(timestamp)}, nil
}

// CallAt executes calls against the state at height after proving the node
// reports blockHash for that height, and proves it again afterwards so every
// result belongs to that one block.
func (r Reader) CallAt(ctx context.Context, height int64, blockHash string, calls []ethereum.CallMsg) ([][]byte, error) {
	if len(calls) == 0 || len(calls) > MaxCalls || !hashPattern.MatchString(blockHash) {
		return nil, errors.New("chainread: invalid pinned read")
	}
	expected := strings.ToLower(blockHash)
	before, err := r.Block(ctx, height)
	if err != nil {
		return nil, err
	}
	if before.Hash != expected {
		return nil, ErrConflict
	}
	out := make([][]byte, len(calls))
	for i, call := range calls {
		if out[i], err = r.RPC.CallContract(ctx, call, big.NewInt(height)); err != nil {
			return nil, err
		}
	}
	after, err := r.Block(ctx, height)
	if err != nil {
		return nil, err
	}
	if after.Hash != expected {
		return nil, ErrConflict
	}
	return out, nil
}
