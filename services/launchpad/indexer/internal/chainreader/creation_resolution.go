package chainreader

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
)

type CreationResolution struct {
	ChainID          uint64  `json:"chainId"`
	TransactionHash  string  `json:"transactionHash"`
	Status           string  `json:"status"`
	Pool             *string `json:"pool"`
	Token            *string `json:"token"`
	Creator          *string `json:"creator"`
	NFTID            *string `json:"nftId"`
	BlockHash        *string `json:"blockHash"`
	EvidenceLogIndex *uint   `json:"evidenceLogIndex"`
	Failure          string  `json:"failure,omitempty"`
}

type CreationResolver struct {
	rpc      *ethclient.Client
	registry *decode.Registry
}

func NewCreationResolver(ctx context.Context, rpc *ethclient.Client, registry *decode.Registry) (*CreationResolver, error) {
	if rpc == nil || registry == nil {
		return nil, fmt.Errorf("real chain125 RPC and attested registry required")
	}
	call, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	id, err := rpc.ChainID(call)
	if err != nil {
		return nil, fmt.Errorf("creation chain identity: %w", err)
	}
	if id == nil || !id.IsUint64() || id.Uint64() != 125 {
		return nil, fmt.Errorf("creation resolver requires chain125")
	}
	return &CreationResolver{rpc: rpc, registry: registry}, nil
}

func creationHash(s string) (common.Hash, error) {
	if len(s) != 66 || !strings.HasPrefix(s, "0x") {
		return common.Hash{}, fmt.Errorf("exact 32-byte transaction hash required")
	}
	b, err := hex.DecodeString(s[2:])
	if err != nil || len(b) != 32 {
		return common.Hash{}, fmt.Errorf("exact 32-byte transaction hash required")
	}
	h := common.BytesToHash(b)
	if h == (common.Hash{}) {
		return common.Hash{}, fmt.Errorf("zero transaction hash")
	}
	return h, nil
}

func (r *CreationResolver) Resolve(ctx context.Context, chainID uint64, txHash string, expectedCreator *common.Address) (*CreationResolution, error) {
	hash, err := creationHash(txHash)
	if err != nil || chainID != 125 {
		return nil, fmt.Errorf("invalid chain125 creation query: %v", err)
	}
	out := &CreationResolution{ChainID: 125, TransactionHash: strings.ToLower(hash.Hex()), Status: "unavailable"}
	if r == nil || r.rpc == nil || r.registry == nil {
		return out, fmt.Errorf("creation resolver unavailable")
	}
	call, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	receipt, err := r.rpc.TransactionReceipt(call, hash)
	if errors.Is(err, ethereum.NotFound) {
		out.Status = "pending"
		return out, nil
	}
	if err != nil {
		out.Failure = "receipt_rpc"
		return out, fmt.Errorf("exact receipt unavailable: %w", err)
	}
	if receipt == nil || receipt.TxHash != hash || receipt.BlockHash == (common.Hash{}) || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() {
		out.Status = "integrity_error"
		return out, fmt.Errorf("receipt query identity mismatch")
	}
	// The receipt's block hash is the node-reported identity (on chain 125 the
	// CometBFT hash, never go-ethereum's recomputed Header.Hash()); the node
	// must report exactly that hash for the receipt's height.
	var committed *struct {
		Number *hexutil.Big `json:"number"`
		Hash   *common.Hash `json:"hash"`
	}
	if err = r.rpc.Client().CallContext(call, &committed, "eth_getBlockByNumber", hexutil.EncodeBig(receipt.BlockNumber), false); err != nil {
		out.Failure = "historical_header_rpc"
		return out, fmt.Errorf("receipt header unavailable: %w", err)
	}
	if committed == nil || committed.Number == nil || committed.Hash == nil || *committed.Hash != receipt.BlockHash || committed.Number.ToInt().Cmp(receipt.BlockNumber) != 0 {
		out.Status = "integrity_error"
		return out, fmt.Errorf("receipt header mismatch")
	}
	blockHash := strings.ToLower(receipt.BlockHash.Hex())
	out.BlockHash = &blockHash
	if receipt.Status == 0 {
		out.Status = "failed"
		return out, nil
	}
	identity, err := CreationFromReceipt(r.registry, receipt, hash)
	if err != nil {
		out.Status = "integrity_error"
		out.Failure = "native_creation_evidence"
		return out, err
	}
	if expectedCreator != nil && (*expectedCreator == (common.Address{}) || identity.Creator != *expectedCreator) {
		out.Status = "integrity_error"
		return out, fmt.Errorf("creation evidence creator mismatch")
	}
	pool, token, creator, nft := strings.ToLower(identity.Pool.Hex()), strings.ToLower(identity.Token.Hex()), strings.ToLower(identity.Creator.Hex()), identity.NFTID.String()
	index := identity.Raw.LogIndex
	out.Pool, out.Token, out.Creator, out.NFTID, out.EvidenceLogIndex = &pool, &token, &creator, &nft, &index
	out.Status = "ready"
	return out, nil
}
