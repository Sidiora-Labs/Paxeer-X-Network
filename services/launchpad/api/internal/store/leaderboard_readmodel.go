package store

import (
	"context"
	"crypto/sha256"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ChainWatermark is the contract Watermark shape every computed read model
// attaches to its envelope. ObservedAt is pre-formatted to the frozen
// millisecond UTC pattern.
type ChainWatermark struct {
	ChainID            string `json:"chainId"`
	Height             string `json:"height"`
	BlockHash          string `json:"blockHash"`
	ProjectionRevision string `json:"projectionRevision"`
	ObservedAt         string `json:"observedAt"`
	Integrity          string `json:"integrity"`
}

const watermarkTimeLayout = "2006-01-02T15:04:05.000Z"

// LeaderboardMethodologyVersion names the computation behind each category so
// clients can tell apart revisions of the ranking rule.
// Trader ranks realised PnL (contract Leaderboard: "PnL rankings excluded
// where incomplete basis"; requirement D07_025): see RealizedTraderPnL.
const LeaderboardMethodologyVersion = "fills-v2:trader=realized-pnl(sell usdlAmountRaw - proportional_floor_v1 basis of buy paidInputRaw; unknown-basis disposals excluded);creator=buy-side feeRaw on canonical pools;volume=usdlAmountRaw"

var leaderboardWindows = map[string]int64{"24h": 24 * 3600, "7d": 7 * 24 * 3600, "30d": 30 * 24 * 3600}

var leaderboardSnapshotNamespace = uuid.NewSHA1(uuid.NameSpaceURL, []byte("sidiora:v2:leaderboards"))

type LeaderboardEntry struct {
	Wallet       string
	MetricRaw    string
	MetricSigned string
	Quality      string
	signed       *big.Int
}

type LeaderboardSnapshot struct {
	SnapshotID         string
	Category           string
	Window             string
	MethodologyVersion string
	WindowEnd          int64
	Entries            []LeaderboardEntry
	Watermark          ChainWatermark
	Revision           string
	Quality            string
}

// LeaderboardWindowSeconds reports the frozen window lengths.
func LeaderboardWindowSeconds(window string) (int64, bool) {
	seconds, ok := leaderboardWindows[window]
	return seconds, ok
}

// Leaderboard ranks wallets over committed accounting fills. windowEnd of zero
// pins the window to the database transaction clock; a positive value replays
// a previously issued page boundary so cursors stay stable.
func (s *AccountingReadModel) Leaderboard(ctx context.Context, category, window string, windowEnd int64) (*LeaderboardSnapshot, error) {
	seconds, ok := leaderboardWindows[window]
	if s == nil || s.pool == nil || !ok || (category != "trader" && category != "creator" && category != "volume") || windowEnd < 0 {
		return nil, ErrAccountingNotReady
	}
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer rollbackRead(tx)
	if _, err = tx.Exec(ctx, "SET LOCAL statement_timeout = '150ms'"); err != nil {
		return nil, err
	}
	commits := pgx.Identifier{s.config.StatsSchema}.Sanitize() + ".accounting_market_commits_v1"
	if windowEnd == 0 {
		if err = tx.QueryRow(ctx, `SELECT floor(extract(epoch FROM transaction_timestamp()))::bigint`).Scan(&windowEnd); err != nil {
			return nil, err
		}
	}
	var height, blockHash, revision, observed string
	err = tx.QueryRow(ctx, `SELECT w.height, w.block_hash, (SELECT max(sequence) FROM `+commits+` WHERE projection_version=$1)::text, (SELECT max((fill->>'blockTimestampSeconds')::bigint) FROM `+commits+` WHERE projection_version=$1)::text FROM (SELECT fill->'position'->>'height' AS height, fill->'position'->>'blockHash' AS block_hash FROM `+commits+` WHERE projection_version=$1 ORDER BY (fill->'position'->>'height')::numeric DESC, (fill->'position'->>'transactionIndex')::numeric DESC, (fill->'position'->>'logIndex')::numeric DESC LIMIT 1) w`, s.config.ProjectionVersion).Scan(&height, &blockHash, &revision, &observed)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrAccountingNotReady
		}
		return nil, err
	}
	if _, err = ParseUInt256(height); err != nil || !validBlockHash(blockHash) || revision == "0" {
		return nil, ErrDataIntegrity
	}
	if _, err = ParseUInt256(revision); err != nil {
		return nil, ErrDataIntegrity
	}
	observedAt, err := strconv.ParseInt(observed, 10, 64)
	if err != nil || observedAt <= 0 {
		return nil, ErrDataIntegrity
	}
	from := windowEnd - seconds
	var rows pgx.Rows
	entries := []LeaderboardEntry{}
	unknownBasis := 0
	switch category {
	case "trader":
		fills, loadErr := traderRealizedRows(ctx, tx, commits, s.config.ProjectionVersion, from, windowEnd)
		if loadErr != nil {
			return nil, loadErr
		}
		if entries, unknownBasis, err = RealizedTraderPnL(fills, from, windowEnd); err != nil {
			return nil, err
		}
	case "volume":
		rows, err = tx.Query(ctx, `SELECT wallet, sum(usdl)::text, '0', bool_and(complete) FROM (SELECT COALESCE(fill->'attribution'->>'trader', fill->'attribution'->>'transactionSender') AS wallet, (fill->>'usdlAmountRaw')::numeric AS usdl, fill->'attribution'->>'quality' <> 'unavailable' AS complete FROM `+commits+` WHERE projection_version=$1 AND (fill->>'blockTimestampSeconds')::bigint > $2 AND (fill->>'blockTimestampSeconds')::bigint <= $3) f GROUP BY wallet`, s.config.ProjectionVersion, from, windowEnd)
	case "creator":
		rows, err = tx.Query(ctx, `SELECT c.creator, sum(CASE WHEN f.fill->>'side'='buy' THEN (f.fill->>'feeRaw')::numeric ELSE 0 END)::text, '0', bool_and(f.fill->'execution'->>'quality' <> 'unavailable' AND (f.fill->>'side'='buy' OR (f.fill->>'feeRaw')::numeric = 0)) FROM `+commits+` f JOIN indexer.indexer_canonical_creations_v1 c ON c.pool=f.pool WHERE f.projection_version=$1 AND (f.fill->>'blockTimestampSeconds')::bigint > $2 AND (f.fill->>'blockTimestampSeconds')::bigint <= $3 GROUP BY c.creator`, s.config.ProjectionVersion, from, windowEnd)
	}
	if err != nil {
		return nil, err
	}
	if rows != nil {
		defer rows.Close()
	}
	for rows != nil && rows.Next() {
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
		if len(entry.MetricRaw) > 156 {
			return nil, ErrDataIntegrity
		}
		entries = append(entries, entry)
	}
	if rows != nil {
		rows.Close()
		if err = rows.Err(); err != nil {
			return nil, err
		}
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
	digest.Write([]byte(category + "|" + window + "|" + strconv.FormatInt(windowEnd, 10) + "|" + height + "|" + revision + realizedDigestSuffix(unknownBasis)))
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
		MethodologyVersion: LeaderboardMethodologyVersion,
		WindowEnd:          windowEnd,
		Entries:            entries,
		Watermark:          ChainWatermark{ChainID: "125", Height: height, BlockHash: strings.ToLower(blockHash), ProjectionRevision: revision, ObservedAt: time.Unix(observedAt, 0).UTC().Format(watermarkTimeLayout), Integrity: "healthy"},
		Revision:           revision,
		Quality:            quality,
	}, nil
}

// PublicEntry renders one ranked entry in the contract LeaderboardEntry shape.
func (e LeaderboardEntry) PublicEntry(rank int) map[string]any {
	out := map[string]any{"rank": rank, "wallet": e.Wallet, "metricRaw": e.MetricRaw, "quality": e.Quality}
	if e.MetricSigned != "" {
		out["metricSigned"] = e.MetricSigned
	}
	return out
}
