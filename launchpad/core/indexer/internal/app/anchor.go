package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/receipt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// historicalEndpointID names the optional archive endpoint configured through
// INDEXER_HISTORICAL_RPC_URL in the committed source.
const historicalEndpointID = "paxeer-historical"

// maxRehydratedMarkets bounds boot rehydration: two code epochs per market
// within the qualification's 4096-epoch authority.
const maxRehydratedMarkets = 2040

// bindRuntimeAnchor requires the receipt authority and the committed source to
// pin the same chain identity in the same block-identity mode, and an anchor to
// be the issued initial checkpoint the first live batch must extend.
func bindRuntimeAnchor(lifecycle *lifecycleRuntimeDocument, authority receipt.RPCOptions) error {
	committed := lifecycle.Committed
	if committed.BlockIdentity != authority.BlockIdentity {
		return errors.New("indexer: committed provider block identity disagrees with receipt authority")
	}
	if authority.Anchor == nil {
		if committed.Anchor != nil {
			return errors.New("indexer: committed provider anchor without receipt authority anchor")
		}
		return nil
	}
	if committed.Anchor == nil || committed.Anchor.Height != authority.Anchor.Height || committed.Anchor.Hash != authority.Anchor.Hash || committed.GenesisHash != (common.Hash{}) {
		return errors.New("indexer: committed provider anchor disagrees with receipt authority")
	}
	if lifecycle.InitialCheckpointHeight < 0 || uint64(lifecycle.InitialCheckpointHeight) != authority.Anchor.Height || lifecycle.InitialCheckpointHash != strings.ToLower(authority.Anchor.Hash.Hex()) {
		return errors.New("indexer: chain anchor must be the issued initial checkpoint")
	}
	return nil
}

// attestDurableAnchor proves chain identity from durable state once the anchor
// block has been pruned from every configured source: the stream's first
// committed batch extended exactly the anchor height, and the durable watermark
// is beyond it. Every live batch is required to extend the previous durable
// block hash (LiveQualifiedSource.PriorHash), and boot re-verifies the
// watermark block hash against the live chain, so the persisted chain descends
// from the anchor.
func attestDurableAnchor(ctx context.Context, pool *pgxpool.Pool, stream string, anchor receipt.BlockAnchor) error {
	if pool == nil || stream == "" || anchor.Hash == (common.Hash{}) {
		return errors.New("indexer: durable anchor attestation unavailable")
	}
	var extended bool
	var watermark int64
	err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM indexer.indexer_ingest_commits WHERE chain_id=125 AND stream_id=$1 AND previous_height=$2), COALESCE((SELECT raw_complete_height FROM indexer.indexer_ingest_watermarks WHERE chain_id=125 AND stream_id=$1), -1)`, stream, int64(anchor.Height)).Scan(&extended, &watermark)
	if err != nil {
		return err
	}
	if !extended || watermark <= int64(anchor.Height) {
		return errors.New("indexer: no durable ingestion descends from the pruned chain anchor")
	}
	return nil
}

// durableBlockHash is the hash the next live batch must extend: the issued
// initial checkpoint at its height, otherwise the completed block at height.
func durableBlockHash(ctx context.Context, pool *pgxpool.Pool, stream string, height uint64, lifecycle lifecycleRuntimeDocument) (common.Hash, error) {
	if lifecycle.InitialCheckpointHeight >= 0 && height == uint64(lifecycle.InitialCheckpointHeight) {
		return common.HexToHash(lifecycle.InitialCheckpointHash), nil
	}
	var hash string
	err := pool.QueryRow(ctx, `SELECT block_hash FROM indexer.indexer_block_completeness WHERE chain_id=125 AND stream_id=$1 AND height=$2 AND fetch_complete AND required_decode_complete`, stream, int64(height)).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return common.Hash{}, fmt.Errorf("indexer: durable block %d has no completed source block", height)
	}
	if err != nil {
		return common.Hash{}, err
	}
	if len(hash) != 66 || !strings.HasPrefix(hash, "0x") {
		return common.Hash{}, errors.New("indexer: durable block hash invalid")
	}
	return common.HexToHash(hash), nil
}

// persistedMarkets lists the markets of previously qualified canonical
// creations for boot rehydration of the receipt authority.
func persistedMarkets(ctx context.Context, pool *pgxpool.Pool) ([]receipt.DiscoveredMarket, error) {
	rows, err := pool.Query(ctx, `SELECT token, pool, height, transaction_index FROM indexer.indexer_canonical_creations ORDER BY height, transaction_index, log_index LIMIT $1`, maxRehydratedMarkets+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	markets := []receipt.DiscoveredMarket{}
	for rows.Next() {
		var token, poolAddress string
		var height, index int64
		if err = rows.Scan(&token, &poolAddress, &height, &index); err != nil {
			return nil, err
		}
		if !common.IsHexAddress(token) || !common.IsHexAddress(poolAddress) || height < 0 || index < 0 || index > 1<<32-1 {
			return nil, errors.New("indexer: persisted canonical creation invalid")
		}
		markets = append(markets, receipt.DiscoveredMarket{Token: common.HexToAddress(token), Pool: common.HexToAddress(poolAddress), Height: uint64(height), TransactionIndex: uint(index)})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(markets) > maxRehydratedMarkets {
		return nil, errors.New("indexer: qualified market count exceeds the receipt authority bound")
	}
	return markets, nil
}

// seedCanonicalWatermark starts a fresh production fleet at the issued
// initial checkpoint: when the stream has no watermark yet, it is created at
// InitialCheckpointHeight, so ingestion begins at InitialCheckpointHeight+1
// (the start height). An existing watermark is never moved.
func seedCanonicalWatermark(ctx context.Context, pool *pgxpool.Pool, stream string, lifecycle lifecycleRuntimeDocument) error {
	if pool == nil || stream == "" || lifecycle.InitialCheckpointHeight < 0 || len(lifecycle.InitialCheckpointHash) != 66 {
		return errors.New("indexer: issued initial checkpoint required")
	}
	_, err := pool.Exec(ctx, `INSERT INTO indexer.indexer_ingest_watermarks(chain_id,stream_id,raw_complete_height,typed_complete_height) VALUES(125,$1,$2,$2) ON CONFLICT (chain_id,stream_id) DO NOTHING`, stream, lifecycle.InitialCheckpointHeight)
	return err
}
