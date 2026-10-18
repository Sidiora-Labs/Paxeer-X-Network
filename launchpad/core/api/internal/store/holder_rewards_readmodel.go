package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrWatermarkQuarantined reports that the committed indexer head is
// quarantined; computed read models refuse to serve under it.
var ErrWatermarkQuarantined = errors.New("INTEGRITY_QUARANTINED")

// feeStrategyAirdrop is the on-chain fee strategy code for holder airdrops
// (IFeesRouter: 0=CLAIM,1=BURN,2=AIRDROP,3=LP_REWARDS).
const feeStrategyAirdrop = 2

var holderRewardsSnapshotNamespace = uuid.NewSHA1(uuid.NameSpaceURL, []byte("sidiora:v2:holder-rewards"))

type HolderRewardsStats struct {
	CumulativeDistributedRaw UInt256
	WalletsRewarded          int64
	Watermark                ChainWatermark
}

type HolderRewardPoolRow struct {
	Pool           string
	Token          string
	DistributedRaw UInt256
	HoldersPaid    int64
	StrategyActive bool
	CreatedAt      int64
	MarketCap      *big.Rat
}

type HolderRewardPoolsSnapshot struct {
	SnapshotID string
	Sort       string
	Items      []HolderRewardPoolRow
	Watermark  ChainWatermark
	Quality    string
}

func committedWatermark(ctx context.Context, tx pgx.Tx) (ChainWatermark, error) {
	var height, blockHash, integrity string
	var observed time.Time
	err := tx.QueryRow(ctx, `SELECT height::text, block_hash, integrity, transaction_timestamp() FROM indexer.indexer_committed_heads_v2 WHERE chain_id=125 ORDER BY height DESC LIMIT 1`).Scan(&height, &blockHash, &integrity, &observed)
	if err != nil {
		if err == pgx.ErrNoRows {
			return ChainWatermark{}, ErrAccountingNotReady
		}
		return ChainWatermark{}, err
	}
	if _, err = ParseUInt256(height); err != nil || height == "0" || !validBlockHash(blockHash) {
		return ChainWatermark{}, ErrDataIntegrity
	}
	switch integrity {
	case "healthy":
	case "quarantined":
		return ChainWatermark{}, ErrWatermarkQuarantined
	default:
		return ChainWatermark{}, ErrDataIntegrity
	}
	return ChainWatermark{ChainID: "125", Height: height, BlockHash: strings.ToLower(blockHash), ProjectionRevision: height, ObservedAt: observed.UTC().Format(watermarkTimeLayout), Integrity: integrity}, nil
}

// HolderRewardsStats aggregates settled airdrop distributions and distinct
// paid holders over every pool, under the latest healthy committed head.
func (s *Store) HolderRewardsStats(ctx context.Context) (*HolderRewardsStats, error) {
	ctx, cancel, tx, err := s.beginLegacyRead(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer rollbackRead(tx)
	watermark, err := committedWatermark(ctx, tx)
	if err != nil {
		return nil, err
	}
	total, wallets, err := holderRewardsTotals(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &HolderRewardsStats{CumulativeDistributedRaw: total, WalletsRewarded: wallets, Watermark: watermark}, nil
}

// holderRewardsTotals sums settled airdrop distributions and counts distinct
// claiming holders inside the caller's read transaction.
func holderRewardsTotals(ctx context.Context, tx pgx.Tx) (UInt256, int64, error) {
	var distributed string
	var wallets int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT sum(amount::numeric) FROM indexer.fee_distributions WHERE strategy=$1), 0)::text, (SELECT count(DISTINCT lower(holder)) FROM indexer.airdrop_claims)`, feeStrategyAirdrop).Scan(&distributed, &wallets); err != nil {
		return "", 0, err
	}
	total, err := ParseUInt256(distributed)
	if err != nil || wallets < 0 {
		return "", 0, ErrDataIntegrity
	}
	return total, wallets, nil
}

// HolderRewardPools lists every pool with airdrop evidence (a settled
// strategy-2 distribution, a holder claim, or an active airdrop strategy),
// ordered by the requested sort with the pool address as the final tiebreak.
func (s *Store) HolderRewardPools(ctx context.Context, sortKey string) (*HolderRewardPoolsSnapshot, error) {
	if sortKey != "rewards" && sortKey != "mcap" && sortKey != "holders" && sortKey != "age" {
		return nil, ErrDataIntegrity
	}
	ctx, cancel, tx, err := s.beginLegacyRead(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer rollbackRead(tx)
	watermark, err := committedWatermark(ctx, tx)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `WITH dist AS (SELECT pool_id, sum(amount::numeric) AS distributed FROM indexer.fee_distributions WHERE strategy=$1 GROUP BY pool_id),
claims AS (SELECT pool_id, count(DISTINCT lower(holder)) AS holders FROM indexer.airdrop_claims WHERE pool_id IS NOT NULL GROUP BY pool_id),
events AS (SELECT pool_id, block_number, log_index, new_strategy AS strategy FROM indexer.fee_strategy_changes UNION ALL SELECT pool_id, block_number, log_index, strategy FROM indexer.fee_distributions),
latest AS (SELECT DISTINCT ON (pool_id) pool_id, strategy FROM events ORDER BY pool_id, block_number DESC, log_index DESC),
candidates AS (SELECT pool_id FROM dist UNION SELECT pool_id FROM claims UNION SELECT pool_id FROM latest WHERE strategy=$1)
SELECT lower(p.pool_address), lower(p.token_address), COALESCE(d.distributed, 0)::text, COALESCE(c.holders, 0), l.strategy, COALESCE(cc.block_timestamp, p.created_at), ps.market_cap
FROM candidates k JOIN indexer.pools p ON p.pool_id=k.pool_id
LEFT JOIN dist d ON d.pool_id=k.pool_id
LEFT JOIN claims c ON c.pool_id=k.pool_id
LEFT JOIN latest l ON l.pool_id=k.pool_id
LEFT JOIN indexer.indexer_canonical_creations_v1 cc ON cc.pool=lower(p.pool_address)
LEFT JOIN stats.pool_stats ps ON lower(ps.pool_address)=lower(p.pool_address)
ORDER BY lower(p.pool_address)`, feeStrategyAirdrop)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, quality, err := scanHolderRewardPools(rows)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return orderHolderRewardPools(sortKey, items, watermark, quality), nil
}

// scanHolderRewardPools reads the candidate rows shared by the production and
// development holder-reward reads. A pool without any strategy evidence makes
// the snapshot partial and is left out.
func scanHolderRewardPools(rows pgx.Rows) ([]HolderRewardPoolRow, string, error) {
	items := []HolderRewardPoolRow{}
	quality := "complete"
	for rows.Next() {
		var row HolderRewardPoolRow
		var distributed string
		var strategy *int32
		var marketCap *string
		if err := rows.Scan(&row.Pool, &row.Token, &distributed, &row.HoldersPaid, &strategy, &row.CreatedAt, &marketCap); err != nil {
			return nil, "", err
		}
		var err error
		if row.Pool, err = Address(row.Pool); err != nil {
			return nil, "", ErrDataIntegrity
		}
		if row.Token, err = Address(row.Token); err != nil {
			return nil, "", ErrDataIntegrity
		}
		if row.DistributedRaw, err = ParseUInt256(distributed); err != nil || row.HoldersPaid < 0 || row.CreatedAt <= 0 {
			return nil, "", ErrDataIntegrity
		}
		if strategy == nil {
			quality = "partial"
			continue
		}
		row.StrategyActive = *strategy == feeStrategyAirdrop
		if marketCap != nil {
			cap, ok := new(big.Rat).SetString(strings.TrimSpace(*marketCap))
			if !ok || cap.Sign() < 0 {
				return nil, "", ErrDataIntegrity
			}
			row.MarketCap = cap
		}
		items = append(items, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	return items, quality, nil
}

// orderHolderRewardPools sorts the rows by the requested key (pool address as
// the final tiebreak) and derives the snapshot identity from the ordered set.
func orderHolderRewardPools(sortKey string, items []HolderRewardPoolRow, watermark ChainWatermark, quality string) *HolderRewardPoolsSnapshot {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		switch sortKey {
		case "rewards":
			if c := uint256Compare(a.DistributedRaw, b.DistributedRaw); c != 0 {
				return c > 0
			}
		case "holders":
			if a.HoldersPaid != b.HoldersPaid {
				return a.HoldersPaid > b.HoldersPaid
			}
		case "age":
			if a.CreatedAt != b.CreatedAt {
				return a.CreatedAt > b.CreatedAt
			}
		case "mcap":
			switch {
			case a.MarketCap != nil && b.MarketCap != nil:
				if c := a.MarketCap.Cmp(b.MarketCap); c != 0 {
					return c > 0
				}
			case a.MarketCap != nil:
				return true
			case b.MarketCap != nil:
				return false
			}
		}
		return a.Pool < b.Pool
	})
	digest := sha256.New()
	digest.Write([]byte(sortKey + "|" + watermark.Height))
	for _, item := range items {
		digest.Write([]byte("|" + item.Pool + ":" + string(item.DistributedRaw) + ":" + strconv.FormatInt(item.HoldersPaid, 10) + ":" + strconv.FormatBool(item.StrategyActive)))
	}
	return &HolderRewardPoolsSnapshot{SnapshotID: uuid.NewSHA1(holderRewardsSnapshotNamespace, digest.Sum(nil)).String(), Sort: sortKey, Items: items, Watermark: watermark, Quality: quality}
}

func uint256Compare(a, b UInt256) int {
	if len(a) != len(b) {
		if len(a) > len(b) {
			return 1
		}
		return -1
	}
	return strings.Compare(string(a), string(b))
}

// Public renders the row in the contract HolderRewardPool shape.
func (r HolderRewardPoolRow) Public() map[string]any {
	return map[string]any{"poolAddress": r.Pool, "tokenAddress": r.Token, "distributedRaw": string(r.DistributedRaw), "holdersPaid": r.HoldersPaid, "strategyActive": r.StrategyActive, "createdAt": time.Unix(r.CreatedAt, 0).UTC().Format(watermarkTimeLayout)}
}
