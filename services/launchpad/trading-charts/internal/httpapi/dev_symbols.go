package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// TokenMetadataResolver resolves the verified Token document of a pool.
// ErrUnknownPool reports a pool the authority has no creation record for.
type TokenMetadataResolver interface {
	Resolve(ctx context.Context, pool string) (interfaces.Token, error)
}

// developmentMarketCreatedTopic is EventEmitter.MarketCreated; topic1 is the
// poolId. The development indexer turns this event into indexer.pools rows.
var developmentMarketCreatedTopic = crypto.Keccak256Hash([]byte("MarketCreated(bytes32,address,address,address,address,uint256,uint256)"))

// DevelopmentPoolMetadataSource is the non-production TokenMetadataResolver.
// Production resolves a pool through indexer.indexer_canonical_creations_v1 and
// a verified (anchor-pinned) RPC client; neither exists in development. Here
// the pool's creation record comes from the development indexer
// (indexer.pools), its creation position from the MarketCreated log in the
// creation transaction's receipt, and name/symbol/decimals/totalSupply from
// eth_call at the chain head. Block hashes are always the ones the node
// reports, never recomputed locally (Cosmos-EVM header hashes differ).
//
// Creation positions are immutable and cached for the process lifetime, so a
// pool resolved once keeps resolving after the node prunes its creation block.
type DevelopmentPoolMetadataSource struct {
	Pool  *pgxpool.Pool
	Chain *rpc.Client

	mu        sync.Mutex
	positions map[string]interfaces.ChainPosition
}

const developmentPositionCacheSize = 4096

func (s *DevelopmentPoolMetadataSource) cachedPosition(pool string) (interfaces.ChainPosition, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.positions[pool]
	return p, ok
}

func (s *DevelopmentPoolMetadataSource) rememberPosition(pool string, p interfaces.ChainPosition) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.positions == nil || len(s.positions) >= developmentPositionCacheSize {
		s.positions = map[string]interfaces.ChainPosition{}
	}
	s.positions[pool] = p
}

type developmentReceipt struct {
	BlockHash        string `json:"blockHash"`
	BlockNumber      string `json:"blockNumber"`
	TransactionHash  string `json:"transactionHash"`
	TransactionIndex string `json:"transactionIndex"`
	Status           string `json:"status"`
	Logs             []struct {
		Topics   []string `json:"topics"`
		LogIndex string   `json:"logIndex"`
		Removed  bool     `json:"removed"`
	} `json:"logs"`
}

func validHash(s string) bool {
	if len(s) != 66 || !strings.HasPrefix(s, "0x") {
		return false
	}
	_, err := hexutil.Decode(s)
	return err == nil
}

// creationPosition resolves the MarketCreated log (topic1 == poolID) inside the
// recorded creation transaction at the recorded creation height.
func (s *DevelopmentPoolMetadataSource) creationPosition(ctx context.Context, pool, poolID, txHash string, height int64) (interfaces.ChainPosition, error) {
	if p, ok := s.cachedPosition(pool); ok {
		return p, nil
	}
	var receipt *developmentReceipt
	if err := s.Chain.CallContext(ctx, &receipt, "eth_getTransactionReceipt", txHash); err != nil || receipt == nil {
		return interfaces.ChainPosition{}, errors.New("creation receipt unavailable")
	}
	number, errNumber := hexutil.DecodeUint64(receipt.BlockNumber)
	txIndex, errIndex := hexutil.DecodeUint64(receipt.TransactionIndex)
	if errNumber != nil || errIndex != nil || height < 0 || number != uint64(height) || !validHash(receipt.BlockHash) || !strings.EqualFold(receipt.TransactionHash, txHash) || receipt.Status != "0x1" {
		return interfaces.ChainPosition{}, errors.New("creation receipt conflicts with indexer record")
	}
	var found *interfaces.ChainPosition
	for _, l := range receipt.Logs {
		if l.Removed || len(l.Topics) < 2 || !strings.EqualFold(l.Topics[0], developmentMarketCreatedTopic.Hex()) || !strings.EqualFold(l.Topics[1], poolID) {
			continue
		}
		logIndex, err := hexutil.DecodeUint64(l.LogIndex)
		if err != nil || found != nil {
			return interfaces.ChainPosition{}, errors.New("ambiguous creation log")
		}
		found = &interfaces.ChainPosition{ChainId: "125", Height: interfaces.UInt256(strconv.FormatUint(number, 10)), TransactionIndex: interfaces.UInt256(strconv.FormatUint(txIndex, 10)), LogIndex: interfaces.UInt256(strconv.FormatUint(logIndex, 10)), TransactionHash: strings.ToLower(txHash), BlockHash: strings.ToLower(receipt.BlockHash)}
	}
	if found == nil {
		return interfaces.ChainPosition{}, errors.New("creation log not found")
	}
	s.rememberPosition(pool, *found)
	return *found, nil
}

// Resolve implements TokenMetadataResolver over the development stores.
func (s *DevelopmentPoolMetadataSource) Resolve(ctx context.Context, pool string) (interfaces.Token, error) {
	var token interfaces.Token
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if s == nil || s.Pool == nil || s.Chain == nil || !poolAddressPattern.MatchString(pool) || pool != strings.ToLower(pool) {
		return token, errors.New("development metadata authority unavailable")
	}
	var tokenAddress, poolID, txHash string
	var createdBlock int64
	err := s.Pool.QueryRow(ctx, `SELECT lower(token_address), lower(pool_id), lower(tx_hash), created_block FROM indexer.pools WHERE lower(pool_address)=$1`, pool).Scan(&tokenAddress, &poolID, &txHash, &createdBlock)
	if errors.Is(err, pgx.ErrNoRows) {
		return token, ErrUnknownPool
	}
	if err != nil {
		return token, err
	}
	if !poolAddressPattern.MatchString(tokenAddress) || !validHash(poolID) || !validHash(txHash) {
		return token, errors.New("invalid development creation record")
	}
	position, err := s.creationPosition(ctx, pool, poolID, txHash, createdBlock)
	if err != nil {
		return token, err
	}
	var head *struct {
		Number string `json:"number"`
		Hash   string `json:"hash"`
	}
	if err = s.Chain.CallContext(ctx, &head, "eth_getBlockByNumber", "latest", false); err != nil || head == nil || !validHash(head.Hash) {
		return token, errors.New("chain head unavailable")
	}
	if _, err = hexutil.DecodeUint64(head.Number); err != nil {
		return token, errors.New("chain head unavailable")
	}
	selectors := []string{"0x06fdde03", "0x95d89b41", "0x313ce567", "0x18160ddd"}
	results := make([]hexutil.Bytes, len(selectors))
	batch := make([]rpc.BatchElem, len(selectors))
	for i, selector := range selectors {
		batch[i] = rpc.BatchElem{Method: "eth_call", Args: []any{map[string]any{"to": common.HexToAddress(tokenAddress), "data": selector}, head.Number}, Result: &results[i]}
	}
	if err = s.Chain.BatchCallContext(ctx, batch); err != nil {
		return token, errors.New("token metadata unavailable")
	}
	for _, b := range batch {
		if b.Error != nil {
			return token, errors.New("token metadata unavailable")
		}
	}
	if len(results[2]) != 32 || len(results[3]) != 32 {
		return token, errors.New("token metadata unavailable")
	}
	name, err := decodeMetadataString(results[0], 160)
	if err != nil {
		return token, err
	}
	symbol, err := decodeMetadataString(results[1], 32)
	if err != nil {
		return token, err
	}
	decimalValue := new(big.Int).SetBytes(results[2])
	if !decimalValue.IsInt64() || decimalValue.Int64() > 255 {
		return token, errors.New("invalid token decimals")
	}
	decimals := interfaces.SafeInteger(decimalValue.Int64())
	total := interfaces.UInt256(new(big.Int).SetBytes(results[3]).String())
	// The revision binds the pool, token, creation position and the metadata
	// values, not the head the values were read at, so it is stable while the
	// token's metadata is unchanged.
	revisionInput, _ := json.Marshal([]any{"charts-dev-token-metadata-v1", pool, tokenAddress, position, []byte(results[0]), []byte(results[1]), []byte(results[2]), []byte(results[3])})
	digest := sha256.Sum256(revisionInput)
	revision := interfaces.UInt256(new(big.Int).SetBytes(digest[:]).String())
	token = interfaces.Token{Address: tokenAddress, Name: name, Symbol: symbol, Decimals: &decimals, DecimalsQuality: "verified", MetadataRevision: revision, SourcePosition: &position, SupplyQuality: "verified", TotalSupplyRaw: &total}
	raw, err := json.Marshal(token)
	if err != nil || interfaces.ValidateJSON("Token", raw) != nil {
		return interfaces.Token{}, errors.New("development token metadata violates the Token contract")
	}
	return token, nil
}
