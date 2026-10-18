package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	goredis "github.com/redis/go-redis/v9"
)

// Development market reads. Outside production the services write the legacy
// development schema (indexer.cursors/pools/swaps/pool_state_snapshots, the
// stats.* tables and the ranking:<category> Redis ZSETs) instead of the
// accounting projections. These reads expose that state so the v2 handlers can
// build the same contract documents production serves. None of them is reached
// from a production code path.

const zeroAddress = "0x0000000000000000000000000000000000000000"

// DevelopmentHead is the indexer's committed cursor (indexer.cursors).
type DevelopmentHead struct {
	Height    int64
	UpdatedAt time.Time
}

// DevelopmentIndexerHead reads the development indexer cursor for chain 125.
// A missing row means the indexer has not committed a block yet.
func (s *Store) DevelopmentIndexerHead(ctx context.Context) (DevelopmentHead, error) {
	ctx, cancel, tx, err := s.beginLegacyRead(ctx)
	if err != nil {
		return DevelopmentHead{}, err
	}
	defer cancel()
	defer rollbackRead(tx)
	var head DevelopmentHead
	err = tx.QueryRow(ctx, `SELECT last_processed_block, updated_at FROM indexer.cursors WHERE chain_id=125`).Scan(&head.Height, &head.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return DevelopmentHead{}, ErrAccountingNotReady
	}
	if err != nil {
		return DevelopmentHead{}, err
	}
	if head.Height <= 0 {
		return DevelopmentHead{}, ErrAccountingNotReady
	}
	if err = tx.Commit(ctx); err != nil {
		return DevelopmentHead{}, err
	}
	return head, nil
}

// DevelopmentEventRef locates one indexed log by transaction, height and log
// index. The block hash and transaction index are not stored by the
// development indexer and are resolved from the chain by the caller.
type DevelopmentEventRef struct {
	TransactionHash string
	Height          int64
	LogIndex        int64
}

// DevelopmentPool is one indexer.pools row with its latest indexed state and
// the fee rate observed on its latest qualifying swap.
type DevelopmentPool struct {
	Pool                string
	Token               string
	Creator             string
	Optical             string
	PoolID              string
	CreatedAt           int64
	CreatedHeight       int64
	CreationTransaction string
	Reserves            *PoolReserves
	StateEvent          *DevelopmentEventRef
	FeeBps              *int
}

// DevelopmentPools resolves pool registry rows from the development indexer.
// Pools absent from indexer.pools are omitted from the result.
func (s *Store) DevelopmentPools(ctx context.Context, pools []string) (map[string]DevelopmentPool, error) {
	if len(pools) == 0 || len(pools) > 100 {
		return nil, ErrDataIntegrity
	}
	ctx, cancel, tx, err := s.beginLegacyRead(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer rollbackRead(tx)
	rows, err := tx.Query(ctx, `SELECT lower(p.pool_address), lower(p.token_address), lower(p.creator), lower(p.optical), lower(p.pool_id), p.created_at, p.created_block, lower(p.tx_hash),
	st.virtual_reserve, st.real_reserve, st.token_reserve, st.block_number, lower(st.tx_hash), st.log_index, sw.amount_in, sw.fee
FROM indexer.pools p
LEFT JOIN LATERAL (SELECT virtual_reserve, real_reserve, token_reserve, block_number, tx_hash, log_index FROM indexer.pool_state_snapshots WHERE pool_id=p.pool_id ORDER BY block_number DESC, log_index DESC LIMIT 1) st ON true
LEFT JOIN LATERAL (SELECT amount_in, fee FROM indexer.swaps WHERE pool_id=p.pool_id AND (CASE WHEN amount_in ~ '^[0-9]{1,78}$' THEN amount_in::numeric END) >= 10000 ORDER BY block_number DESC, log_index DESC LIMIT 1) sw ON true
WHERE lower(p.pool_address)=ANY($1)`, pools)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]DevelopmentPool{}
	for rows.Next() {
		var p DevelopmentPool
		var virtual, real, token, stateTx, amountIn, fee *string
		var stateHeight, stateLog *int64
		if err = rows.Scan(&p.Pool, &p.Token, &p.Creator, &p.Optical, &p.PoolID, &p.CreatedAt, &p.CreatedHeight, &p.CreationTransaction, &virtual, &real, &token, &stateHeight, &stateTx, &stateLog, &amountIn, &fee); err != nil {
			return nil, err
		}
		for _, address := range []string{p.Pool, p.Token, p.Creator} {
			if _, addressErr := Address(address); addressErr != nil {
				return nil, ErrDataIntegrity
			}
		}
		if p.Optical != zeroAddress {
			if _, addressErr := Address(p.Optical); addressErr != nil {
				return nil, ErrDataIntegrity
			}
		}
		if !validBlockHash(p.PoolID) || !validBlockHash(p.CreationTransaction) || p.CreatedAt <= 0 || p.CreatedHeight <= 0 {
			return nil, ErrDataIntegrity
		}
		if virtual != nil {
			var state PoolReserves
			if state.VirtualUsdl, err = ParseUInt256(*virtual); err != nil {
				return nil, ErrDataIntegrity
			}
			if state.RealUsdl, err = ParseUInt256(*real); err != nil {
				return nil, ErrDataIntegrity
			}
			if state.Token, err = ParseUInt256(*token); err != nil {
				return nil, ErrDataIntegrity
			}
			if stateTx == nil || !validBlockHash(*stateTx) || *stateHeight <= 0 || *stateLog < 0 {
				return nil, ErrDataIntegrity
			}
			state.Height = *stateHeight
			p.Reserves = &state
			p.StateEvent = &DevelopmentEventRef{TransactionHash: *stateTx, Height: *stateHeight, LogIndex: *stateLog}
		}
		// SidioraPool charges floor(amountIn*feeBps/10000) on the input after any
		// optical beforeSwap reduction, while the Swap event carries the paid
		// input. The rate is therefore recoverable exactly only for pools
		// without an optical.
		if amountIn != nil && fee != nil && p.Optical == zeroAddress {
			if bps, ok := FeeBpsFromSwap(*amountIn, *fee); ok {
				p.FeeBps = &bps
			}
		}
		out[p.Pool] = p
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// FeeBpsFromSwap inverts SidioraPool's fee = floor(amountIn*feeBps/10000).
// With amountIn >= 10000 consecutive rates give distinct fees, so the rate is
// unique; anything else is reported as not derivable.
func FeeBpsFromSwap(amountIn, fee string) (int, bool) {
	in, okIn := new(big.Int).SetString(amountIn, 10)
	paid, okFee := new(big.Int).SetString(fee, 10)
	tenThousand := big.NewInt(10000)
	if !okIn || !okFee || in.Cmp(tenThousand) < 0 || paid.Sign() < 0 || paid.Cmp(in) > 0 {
		return 0, false
	}
	numerator := new(big.Int).Mul(paid, tenThousand)
	bps, remainder := new(big.Int).QuoRem(numerator, in, new(big.Int))
	if remainder.Sign() != 0 {
		bps.Add(bps, big.NewInt(1))
	}
	if bps.Cmp(tenThousand) > 0 {
		return 0, false
	}
	check := new(big.Int).Mul(in, bps)
	check.Quo(check, tenThousand)
	if check.Cmp(paid) != 0 {
		return 0, false
	}
	return int(bps.Int64()), true
}

// RankingMembers reads up to max members of ranking:<category> (highest score
// first) and the set size in one MULTI so the page and total agree.
func (s *Store) RankingMembers(ctx context.Context, category string, max int) ([]RankedItem, int64, error) {
	if s == nil || s.rdb == nil {
		return nil, 0, ErrAccountingNotReady
	}
	if max < 1 || max > 1000 {
		return nil, 0, ErrDataIntegrity
	}
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	key := "ranking:" + category
	var members *goredis.ZSliceCmd
	var card *goredis.IntCmd
	_, err := s.rdb.WithTimeout(75*time.Millisecond).TxPipelined(ctx, func(pipe goredis.Pipeliner) error {
		members = pipe.ZRevRangeWithScores(ctx, key, 0, int64(max-1))
		card = pipe.ZCard(ctx, key)
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	items := make([]RankedItem, 0, len(members.Val()))
	for i, z := range members.Val() {
		member, ok := z.Member.(string)
		if !ok || math.IsNaN(z.Score) || math.IsInf(z.Score, 0) {
			return nil, 0, ErrDataIntegrity
		}
		pool, addressErr := Address(member)
		if addressErr != nil {
			return nil, 0, ErrDataIntegrity
		}
		items = append(items, RankedItem{PoolAddress: pool, Score: z.Score, Rank: i + 1})
	}
	return items, card.Val(), nil
}

// DevelopmentMover is one pool ranked by its 24h price change in basis points
// (stats.pool_stats.price_change_24h, written as delta*10000/old).
type DevelopmentMover struct {
	Pool      string
	ChangeBps string
}

// DevelopmentPriceMovers ranks pools with a positive (gainers) or negative
// (losers) 24h price change, strongest first, and reports the eligible count.
func (s *Store) DevelopmentPriceMovers(ctx context.Context, gainers bool, limit int) ([]DevelopmentMover, int64, error) {
	if limit < 1 || limit > 100 {
		return nil, 0, ErrDataIntegrity
	}
	ctx, cancel, tx, err := s.beginLegacyRead(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer cancel()
	defer rollbackRead(tx)
	order, filter := "change DESC", "change > 0"
	if !gainers {
		order, filter = "change ASC", "change < 0"
	}
	rows, err := tx.Query(ctx, `SELECT pool, change::text, count(*) OVER () FROM (SELECT lower(pool_address) AS pool, CASE WHEN price_change_24h ~ '^-?[0-9]{1,77}$' THEN price_change_24h::numeric END AS change FROM stats.pool_stats) s WHERE `+filter+` ORDER BY `+order+`, pool LIMIT $1`, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []DevelopmentMover{}
	var total int64
	for rows.Next() {
		var m DevelopmentMover
		if err = rows.Scan(&m.Pool, &m.ChangeBps, &total); err != nil {
			return nil, 0, err
		}
		if m.Pool, err = Address(m.Pool); err != nil {
			return nil, 0, ErrDataIntegrity
		}
		out = append(out, m)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// DevelopmentHolder is one positive balance from stats.pool_holders.
type DevelopmentHolder struct {
	Wallet  string
	Balance UInt256
}

// DevelopmentHolders pages a pool's holders by balance descending, wallet
// ascending, strictly after (afterBalance, afterWallet) when afterBalance is
// non-empty. Excluded addresses (protocol-owned balances) are skipped.
func (s *Store) DevelopmentHolders(ctx context.Context, pool string, exclude []string, afterBalance, afterWallet string, limit int) ([]DevelopmentHolder, error) {
	if limit < 1 || limit > 101 {
		return nil, ErrDataIntegrity
	}
	var after any
	if afterBalance != "" {
		if _, err := ParseUInt256(afterBalance); err != nil {
			return nil, ErrCursorInvalid
		}
		after = afterBalance
	}
	ctx, cancel, tx, err := s.beginLegacyRead(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer rollbackRead(tx)
	rows, err := tx.Query(ctx, `SELECT wallet, balance FROM (SELECT lower(holder_address) AS wallet, balance, CASE WHEN balance ~ '^[0-9]{1,78}$' THEN balance::numeric END AS amount FROM stats.pool_holders WHERE lower(pool_address)=$1) h
WHERE amount > 0 AND NOT (wallet = ANY($2)) AND ($3::numeric IS NULL OR amount < $3::numeric OR (amount = $3::numeric AND wallet > $4))
ORDER BY amount DESC, wallet ASC LIMIT $5`, pool, exclude, after, afterWallet, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DevelopmentHolder{}
	for rows.Next() {
		var h DevelopmentHolder
		var balance string
		if err = rows.Scan(&h.Wallet, &balance); err != nil {
			return nil, err
		}
		if h.Wallet, err = Address(h.Wallet); err != nil {
			return nil, ErrDataIntegrity
		}
		if h.Balance, err = ParseUInt256(balance); err != nil {
			return nil, ErrDataIntegrity
		}
		out = append(out, h)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// DevelopmentLeaderboardMethodologyVersion names the development computation
// over the indexer swap table.
const DevelopmentLeaderboardMethodologyVersion = "dev-indexer-swaps-v1:trader=net-usdl-flow(sell amountOut - buy amountIn);creator=buy-side fee on created pools;volume=usdl side of each swap"

// DevelopmentLeaderboard ranks wallets over indexer.swaps inside the window
// (windowEnd-window, windowEnd]. windowEnd of zero pins the window to the
// database transaction clock.
func (s *Store) DevelopmentLeaderboard(ctx context.Context, category, window string, windowEnd int64, watermark ChainWatermark) (*LeaderboardSnapshot, error) {
	seconds, ok := leaderboardWindows[window]
	if !ok || (category != "trader" && category != "creator" && category != "volume") || windowEnd < 0 {
		return nil, ErrAccountingNotReady
	}
	ctx, cancel, tx, err := s.beginLegacyRead(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer rollbackRead(tx)
	if windowEnd == 0 {
		if err = tx.QueryRow(ctx, `SELECT floor(extract(epoch FROM transaction_timestamp()))::bigint`).Scan(&windowEnd); err != nil {
			return nil, err
		}
	}
	const swaps = `SELECT pool_id, sender, is_buy, CASE WHEN amount_in ~ '^[0-9]{1,78}$' THEN amount_in::numeric END AS amt_in, CASE WHEN amount_out ~ '^[0-9]{1,78}$' THEN amount_out::numeric END AS amt_out, CASE WHEN fee ~ '^[0-9]{1,78}$' THEN fee::numeric END AS fee FROM indexer.swaps WHERE block_timestamp > $1 AND block_timestamp <= $2`
	var query string
	switch category {
	case "trader":
		query = `SELECT lower(sender), COALESCE(sum(CASE WHEN NOT is_buy THEN amt_out END), 0)::text, COALESCE(sum(CASE WHEN is_buy THEN amt_in END), 0)::text, bool_and(amt_in IS NOT NULL AND amt_out IS NOT NULL) FROM (` + swaps + `) s GROUP BY lower(sender)`
	case "volume":
		query = `SELECT lower(sender), COALESCE(sum(CASE WHEN is_buy THEN amt_in ELSE amt_out END), 0)::text, '0', bool_and(CASE WHEN is_buy THEN amt_in IS NOT NULL ELSE amt_out IS NOT NULL END) FROM (` + swaps + `) s GROUP BY lower(sender)`
	case "creator":
		query = `SELECT lower(p.creator), COALESCE(sum(CASE WHEN s.is_buy THEN s.fee END), 0)::text, '0', bool_and(NOT s.is_buy OR s.fee IS NOT NULL) FROM (` + swaps + `) s JOIN indexer.pools p ON p.pool_id=s.pool_id GROUP BY lower(p.creator)`
	}
	rows, err := tx.Query(ctx, query, windowEnd-seconds, windowEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []LeaderboardEntry{}
	for rows.Next() {
		var wallet, positive, negative string
		var complete bool
		if err = rows.Scan(&wallet, &positive, &negative, &complete); err != nil {
			return nil, err
		}
		normalized, addressErr := Address(wallet)
		if addressErr != nil {
			return nil, ErrDataIntegrity
		}
		plus, okPlus := new(big.Int).SetString(positive, 10)
		minus, okMinus := new(big.Int).SetString(negative, 10)
		if !okPlus || !okMinus || plus.Sign() < 0 || minus.Sign() < 0 {
			return nil, ErrDataIntegrity
		}
		signed := new(big.Int).Sub(plus, minus)
		entry := LeaderboardEntry{Wallet: normalized, MetricRaw: new(big.Int).Abs(signed).String(), Quality: "partial", signed: signed}
		if complete {
			entry.Quality = "complete"
		}
		if category == "trader" {
			entry.MetricSigned = signed.String()
		}
		if len(entry.MetricRaw) > 156 {
			return nil, ErrDataIntegrity
		}
		entries = append(entries, entry)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if c := entries[i].signed.Cmp(entries[j].signed); c != 0 {
			return c > 0
		}
		return entries[i].Wallet < entries[j].Wallet
	})
	quality := "complete"
	digest := sha256.New()
	digest.Write([]byte("development|" + category + "|" + window + "|" + strconv.FormatInt(windowEnd, 10) + "|" + watermark.Height))
	for _, entry := range entries {
		if entry.Quality != "complete" {
			quality = "partial"
		}
		digest.Write([]byte("|" + entry.Wallet + ":" + entry.signed.String() + ":" + entry.Quality))
	}
	return &LeaderboardSnapshot{
		SnapshotID:         uuid.NewSHA1(leaderboardSnapshotNamespace, digest.Sum(nil)).String(),
		Category:           category,
		Window:             window,
		MethodologyVersion: DevelopmentLeaderboardMethodologyVersion,
		WindowEnd:          windowEnd,
		Entries:            entries,
		Watermark:          watermark,
		Revision:           watermark.ProjectionRevision,
		Quality:            quality,
	}, nil
}

// DevelopmentHolderRewardsStats is HolderRewardsStats under the development
// indexer head instead of the production committed-head table.
func (s *Store) DevelopmentHolderRewardsStats(ctx context.Context, watermark ChainWatermark) (*HolderRewardsStats, error) {
	ctx, cancel, tx, err := s.beginLegacyRead(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer rollbackRead(tx)
	total, wallets, err := holderRewardsTotals(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &HolderRewardsStats{CumulativeDistributedRaw: total, WalletsRewarded: wallets, Watermark: watermark}, nil
}

// DevelopmentHolderRewardPools is HolderRewardPools over the development
// schema: the pool creation time comes from indexer.pools because the
// canonical creation table is production-only.
func (s *Store) DevelopmentHolderRewardPools(ctx context.Context, sortKey string, watermark ChainWatermark) (*HolderRewardPoolsSnapshot, error) {
	if sortKey != "rewards" && sortKey != "mcap" && sortKey != "holders" && sortKey != "age" {
		return nil, ErrDataIntegrity
	}
	ctx, cancel, tx, err := s.beginLegacyRead(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer rollbackRead(tx)
	rows, err := tx.Query(ctx, `WITH dist AS (SELECT pool_id, sum(amount::numeric) AS distributed FROM indexer.fee_distributions WHERE strategy=$1 GROUP BY pool_id),
claims AS (SELECT pool_id, count(DISTINCT lower(holder)) AS holders FROM indexer.airdrop_claims WHERE pool_id IS NOT NULL GROUP BY pool_id),
events AS (SELECT pool_id, block_number, log_index, new_strategy AS strategy FROM indexer.fee_strategy_changes UNION ALL SELECT pool_id, block_number, log_index, strategy FROM indexer.fee_distributions),
latest AS (SELECT DISTINCT ON (pool_id) pool_id, strategy FROM events ORDER BY pool_id, block_number DESC, log_index DESC),
candidates AS (SELECT pool_id FROM dist UNION SELECT pool_id FROM claims UNION SELECT pool_id FROM latest WHERE strategy=$1)
SELECT lower(p.pool_address), lower(p.token_address), COALESCE(d.distributed, 0)::text, COALESCE(c.holders, 0), l.strategy, p.created_at, ps.market_cap
FROM candidates k JOIN indexer.pools p ON p.pool_id=k.pool_id
LEFT JOIN dist d ON d.pool_id=k.pool_id
LEFT JOIN claims c ON c.pool_id=k.pool_id
LEFT JOIN latest l ON l.pool_id=k.pool_id
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

// DevelopmentWatermark renders the development indexer head as the contract
// Watermark once the caller has resolved the head block hash from the chain.
func DevelopmentWatermark(head DevelopmentHead, blockHash string) (ChainWatermark, error) {
	if head.Height <= 0 || !validBlockHash(blockHash) {
		return ChainWatermark{}, ErrDataIntegrity
	}
	height := strconv.FormatInt(head.Height, 10)
	return ChainWatermark{ChainID: "125", Height: height, BlockHash: strings.ToLower(blockHash), ProjectionRevision: height, ObservedAt: head.UpdatedAt.UTC().Format(watermarkTimeLayout), Integrity: "healthy"}, nil
}
