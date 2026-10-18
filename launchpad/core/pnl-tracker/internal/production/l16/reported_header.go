package l16

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Paxeer (chain 125) is a Cosmos-EVM chain: the block hash its nodes report and
// accept in eth_getBlockByHash / EIP-1898 {blockHash} reads is CometBFT-derived,
// not the Keccak of the Ethereum header encoding, so go-ethereum's
// header.Hash() of a fetched header names no block there. The indexer records
// the node-reported hash, and every comparison against it must use the
// node-reported hash too. On chains whose reported hash is the header Keccak
// the two are equal and these helpers change nothing.

// reportedHeaderByNumber returns the header at number together with the hash
// the node reports for that block (lowercase 0x-hex).
func reportedHeaderByNumber(ctx context.Context, client *ethclient.Client, number *big.Int) (*types.Header, string, error) {
	if client == nil || number == nil || number.Sign() < 0 {
		return nil, "", ErrBaselineUnavailable
	}
	var raw json.RawMessage
	if err := client.Client().CallContext(ctx, &raw, "eth_getBlockByNumber", hexutil.EncodeBig(number), false); err != nil {
		return nil, "", err
	}
	return decodeReportedHeader(raw, number)
}

func decodeReportedHeader(raw json.RawMessage, number *big.Int) (*types.Header, string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, "", ethereum.NotFound
	}
	var header types.Header
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, "", err
	}
	var reported struct {
		Hash *common.Hash `json:"hash"`
	}
	if err := json.Unmarshal(raw, &reported); err != nil || reported.Hash == nil || *reported.Hash == (common.Hash{}) {
		return nil, "", delivery.ErrIntegrity
	}
	if header.Number == nil || header.Number.Cmp(number) != 0 {
		return nil, "", delivery.ErrIntegrity
	}
	return &header, strings.ToLower(reported.Hash.Hex()), nil
}

// reportedBlock is the node's view of one block: its reported hash and its
// transaction hashes in block order.
type reportedBlock struct {
	Number       *big.Int
	Hash         common.Hash
	Transactions []common.Hash
}

// reportedBlockByNumber returns the reported hash and ordered transaction
// hashes of the block at number.
func reportedBlockByNumber(ctx context.Context, client *ethclient.Client, number *big.Int) (reportedBlock, error) {
	var block reportedBlock
	if client == nil || number == nil || number.Sign() < 0 {
		return block, ErrBaselineUnavailable
	}
	var raw json.RawMessage
	if err := client.Client().CallContext(ctx, &raw, "eth_getBlockByNumber", hexutil.EncodeBig(number), false); err != nil {
		return block, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return block, ethereum.NotFound
	}
	var body struct {
		Number       *hexutil.Big  `json:"number"`
		Hash         *common.Hash  `json:"hash"`
		Transactions []common.Hash `json:"transactions"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return block, err
	}
	if body.Number == nil || body.Hash == nil || *body.Hash == (common.Hash{}) || body.Number.ToInt().Cmp(number) != 0 {
		return block, delivery.ErrIntegrity
	}
	return reportedBlock{Number: body.Number.ToInt(), Hash: *body.Hash, Transactions: body.Transactions}, nil
}
