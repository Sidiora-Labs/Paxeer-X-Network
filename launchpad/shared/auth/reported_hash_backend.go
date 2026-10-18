package auth

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// ReportedBlock is a header together with the block hash the node reports for
// it (eth_getBlockByNumber "hash"). Hash is the only block identity proof
// verification compares or records: on Paxeer (chain 125, CometBFT-backed
// Cosmos-EVM) the reported hash is the consensus hash, and go-ethereum's
// types.Header.Hash() of the same header names no block there, so EIP-1898
// reads by it fail with "block not found by hash".
type ReportedBlock struct {
	Header *types.Header
	Hash   common.Hash
}

// Number returns the block height, or nil for an empty block.
func (b ReportedBlock) Number() *big.Int {
	if b.Header == nil || b.Header.Number == nil {
		return nil
	}
	return new(big.Int).Set(b.Header.Number)
}

func (b ReportedBlock) valid() bool {
	return b.Header != nil && b.Header.Number != nil && b.Header.Number.Sign() >= 0 && b.Header.Number.IsUint64() && b.Hash != (common.Hash{})
}

// ReportedHashBackend is the production ContractBackend: a chain-125 JSON-RPC
// client whose block identity is the node-reported hash. Headers come from
// eth_getBlockByNumber with the reported "hash" kept beside them; state reads
// (eth_getCode, eth_call) are addressed by block number and the verifiers in
// this package bracket them with reported-hash checks of that height before
// and after the read (see readPinned), the same way shared/chain's
// Client.atPinned does.
type ReportedHashBackend struct {
	rpc    *rpc.Client
	client *ethclient.Client
}

// NewReportedHashBackend wraps a JSON-RPC client. Close closes that client;
// callers that own the connection elsewhere (an *ethclient.Client they close
// themselves) need not call it.
func NewReportedHashBackend(client *rpc.Client) *ReportedHashBackend {
	return &ReportedHashBackend{rpc: client, client: ethclient.NewClient(client)}
}

func (b *ReportedHashBackend) ChainID(ctx context.Context) (*big.Int, error) {
	return b.client.ChainID(ctx)
}

// ReportedBlockByNumber returns the header at number (nil means latest) with
// the hash the node reports for it.
func (b *ReportedHashBackend) ReportedBlockByNumber(ctx context.Context, number *big.Int) (ReportedBlock, error) {
	tag := "latest"
	if number != nil {
		tag = hexutil.EncodeBig(number)
	}
	var raw json.RawMessage
	if err := b.rpc.CallContext(ctx, &raw, "eth_getBlockByNumber", tag, false); err != nil {
		return ReportedBlock{}, err
	}
	block, err := decodeReportedBlock(raw)
	if err != nil {
		return ReportedBlock{}, err
	}
	if number != nil && block.Header.Number.Cmp(number) != 0 {
		return ReportedBlock{}, errors.New("header height mismatch")
	}
	return block, nil
}

// CodeAt reads code at height number.
func (b *ReportedHashBackend) CodeAt(ctx context.Context, address common.Address, number *big.Int) ([]byte, error) {
	if number == nil {
		return nil, ErrScopeDenied
	}
	return b.client.CodeAt(ctx, address, number)
}

// CallContract executes call at height number.
func (b *ReportedHashBackend) CallContract(ctx context.Context, call ethereum.CallMsg, number *big.Int) ([]byte, error) {
	if number == nil {
		return nil, ErrScopeDenied
	}
	return b.client.CallContract(ctx, call, number)
}

// Close releases the RPC client.
func (b *ReportedHashBackend) Close() { b.rpc.Close() }

func decodeReportedBlock(raw json.RawMessage) (ReportedBlock, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return ReportedBlock{}, ethereum.NotFound
	}
	var header types.Header
	if err := json.Unmarshal(raw, &header); err != nil {
		return ReportedBlock{}, err
	}
	var reported struct {
		Hash *common.Hash `json:"hash"`
	}
	if err := json.Unmarshal(raw, &reported); err != nil {
		return ReportedBlock{}, err
	}
	if reported.Hash == nil {
		return ReportedBlock{}, errors.New("block without node-reported identity")
	}
	block := ReportedBlock{Header: &header, Hash: *reported.Hash}
	if !block.valid() {
		return ReportedBlock{}, errors.New("block without node-reported identity")
	}
	return block, nil
}
