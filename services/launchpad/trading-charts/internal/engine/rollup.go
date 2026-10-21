package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/jackc/pgx/v5"
	"math/big"
	"sort"
	"strconv"
)

func boundedSum(a, b string) (string, error) {
	parse := func(s string) (*big.Int, error) {
		if len(s) == 0 || len(s) > 1024 || (len(s) > 1 && s[0] == '0') {
			return nil, ErrInvalidFill
		}
		for _, c := range s {
			if c < '0' || c > '9' {
				return nil, ErrInvalidFill
			}
		}
		n, ok := new(big.Int).SetString(s, 10)
		if !ok {
			return nil, ErrInvalidFill
		}
		return n, nil
	}
	x, err := parse(a)
	if err != nil {
		return "", err
	}
	y, err := parse(b)
	if err != nil {
		return "", err
	}
	sum := x.Add(x, y).String()
	if len(sum) > 1024 {
		return "", ErrInvalidFill
	}
	return sum, nil
}
func RollupQualified(children []QualifiedCandle, actors []string, interval string, start int64) (QualifiedCandle, string, error) {
	duration, err := IntervalSeconds(interval)
	if err != nil || interval == "1m" || len(children) == 0 || len(children) > 10080 {
		return QualifiedCandle{}, "", ErrInvalidFill
	}
	bucket, err := BucketStart(start, interval)
	if err != nil || bucket != start {
		return QualifiedCandle{}, "", ErrInvalidFill
	}
	rows := append([]QualifiedCandle(nil), children...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Start < rows[j].Start })
	out := rows[0]
	out.Interval = interval
	out.Start = start
	out.VolumeUsdlRaw = "0"
	out.VolumeTokenRaw = "0"
	out.BuyVolumeUsdlRaw = "0"
	out.SellVolumeUsdlRaw = "0"
	out.Trades = 0
	out.UnknownActorTradeCount = 0
	out.Revision = 0
	lineage := []string{}
	seen := map[int64]bool{}
	for _, child := range rows {
		if child.Interval != "1m" || child.Trades == 0 || child.Start < start || child.Start >= start+duration || child.Start%60 != 0 || seen[child.Start] || child.PoolAddress != out.PoolAddress || child.Token != out.Token || child.Settlement != out.Settlement || child.ActorSemantics != "canonical_swap_actor_v1" {
			return out, "", ErrInvalidFill
		}
		seen[child.Start] = true
		for _, source := range []QualifiedPriceSource{child.OpenSource, child.HighSource, child.LowSource, child.CloseSource} {
			if accounting.ValidateSourcePosition(source.Position) != nil || source.SourceFillID != accounting.SourceFillIdentity(source.Position, source.Provenance.DeploymentID) {
				return out, "", ErrInvalidFill
			}
		}
		if child.FirstPosition.Compare(out.FirstPosition) < 0 {
			out.FirstPosition = child.FirstPosition
			out.Open = child.Open
			out.OpenSource = child.OpenSource
		}
		if child.LastPosition.Compare(out.LastPosition) > 0 {
			out.LastPosition = child.LastPosition
			out.Close = child.Close
			out.CloseSource = child.CloseSource
		}
		cmp, e := child.High.Compare(out.High)
		if e != nil {
			return out, "", e
		}
		if cmp > 0 || (cmp == 0 && ChainOrderFromSource(child.HighSource.Position).Compare(ChainOrderFromSource(out.HighSource.Position)) < 0) {
			out.High = child.High
			out.HighSource = child.HighSource
		}
		cmp, e = child.Low.Compare(out.Low)
		if e != nil {
			return out, "", e
		}
		if cmp < 0 || (cmp == 0 && ChainOrderFromSource(child.LowSource.Position).Compare(ChainOrderFromSource(out.LowSource.Position)) < 0) {
			out.Low = child.Low
			out.LowSource = child.LowSource
		}
		for _, pair := range []struct {
			dst   *string
			value string
		}{{&out.VolumeUsdlRaw, child.VolumeUsdlRaw}, {&out.VolumeTokenRaw, child.VolumeTokenRaw}, {&out.BuyVolumeUsdlRaw, child.BuyVolumeUsdlRaw}, {&out.SellVolumeUsdlRaw, child.SellVolumeUsdlRaw}} {
			sum, e := boundedSum(*pair.dst, pair.value)
			if e != nil {
				return out, "", e
			}
			*pair.dst = sum
		}
		if ^uint64(0)-out.Trades < child.Trades || ^uint64(0)-out.UnknownActorTradeCount < child.UnknownActorTradeCount {
			return out, "", ErrInvalidFill
		}
		out.Trades += child.Trades
		out.UnknownActorTradeCount += child.UnknownActorTradeCount
		lineage = append(lineage, strconv.FormatInt(child.Start, 10)+":"+strconv.FormatUint(child.Revision, 10))
	}
	union := map[string]bool{}
	for _, actor := range actors {
		if !regexpActor(actor) {
			return out, "", ErrInvalidFill
		}
		union[actor] = true
	}
	out.UniqueTraders = uint64(len(union))
	if out.UnknownActorTradeCount > out.Trades || out.UniqueTraders > out.Trades-out.UnknownActorTradeCount {
		return out, "", ErrInvalidFill
	}
	out.Revision = out.Trades
	raw, _ := json.Marshal(lineage)
	digest := sha256.Sum256(raw)
	return out, hex.EncodeToString(digest[:]), nil
}
func EconomicDigest(c QualifiedCandle) (string, error) {
	value := struct {
		Pool, Interval          string
		Start                   int64
		Open, High, Low, Close  Rational
		First, Last             ChainOrder
		USDL, Token, Buy, Sell  string
		Trades, Unique, Unknown uint64
	}{c.PoolAddress, c.Interval, c.Start, c.Open, c.High, c.Low, c.Close, c.FirstPosition, c.LastPosition, c.VolumeUsdlRaw, c.VolumeTokenRaw, c.BuyVolumeUsdlRaw, c.SellVolumeUsdlRaw, c.Trades, c.UniqueTraders, c.UnknownActorTradeCount}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}
func RecomputeRollups(ctx context.Context, st *store.ExactStore, limit int) (int, error) {
	if st == nil || limit < 1 || limit > 64 {
		return 0, ErrInvalidFill
	}
	completed := 0
	for completed < limit {
		tx, err := st.Pool.Begin(ctx)
		if err != nil {
			return completed, err
		}
		err = func() error {
			var pool, interval string
			var start, childRevision int64
			err := tx.QueryRow(ctx, `SELECT pool,interval,start,child_revision FROM `+st.Schema+`.chart_rollup_pending ORDER BY pool,interval,start LIMIT 1`).Scan(&pool, &interval, &start, &childRevision)
			if err != nil {
				return err
			}
			var epoch int64
			if err = tx.QueryRow(ctx, `SELECT epoch FROM `+st.Schema+`.chart_pool_fences WHERE chain_id='125' AND pool=$1 AND NOT frozen FOR UPDATE`, pool).Scan(&epoch); err != nil {
				return err
			}
			err = tx.QueryRow(ctx, `SELECT child_revision FROM `+st.Schema+`.chart_rollup_pending WHERE chain_id='125' AND pool=$1 AND interval=$2 AND start=$3 FOR UPDATE`, pool, interval, start).Scan(&childRevision)
			if err != nil {
				return err
			}
			duration, err := IntervalSeconds(interval)
			if err != nil {
				return err
			}
			rows, err := tx.Query(ctx, `SELECT snapshot FROM `+st.Schema+`.chart_exact WHERE chain_id='125' AND pool=$1 AND interval='1m' AND start>=$2 AND start<$3 ORDER BY start`, pool, start, start+duration)
			if err != nil {
				return err
			}
			children := []QualifiedCandle{}
			for rows.Next() {
				var raw []byte
				var child QualifiedCandle
				if err = rows.Scan(&raw); err != nil {
					rows.Close()
					return err
				}
				if err = json.Unmarshal(raw, &child); err != nil {
					rows.Close()
					return err
				}
				children = append(children, child)
			}
			rows.Close()
			if err = rows.Err(); err != nil {
				return err
			}
			rows, err = tx.Query(ctx, `SELECT DISTINCT actor FROM `+st.Schema+`.chart_traders WHERE chain_id='125' AND pool=$1 AND interval='1m' AND start>=$2 AND start<$3 ORDER BY actor`, pool, start, start+duration)
			if err != nil {
				return err
			}
			actors := []string{}
			for rows.Next() {
				var actor string
				if err = rows.Scan(&actor); err != nil {
					rows.Close()
					return err
				}
				actors = append(actors, actor)
			}
			rows.Close()
			if err = rows.Err(); err != nil {
				return err
			}
			rolled, digest, err := RollupQualified(children, actors, interval, start)
			if err != nil {
				return err
			}
			var priorRaw []byte
			if err = tx.QueryRow(ctx, `SELECT snapshot FROM `+st.Schema+`.chart_exact WHERE chain_id='125' AND pool=$1 AND interval=$2 AND start=$3`, pool, interval, start).Scan(&priorRaw); err != nil {
				return err
			}
			var prior QualifiedCandle
			if err = json.Unmarshal(priorRaw, &prior); err != nil {
				return err
			}
			directDigest, err := EconomicDigest(prior)
			if err != nil {
				return err
			}
			rolledDigest, err := EconomicDigest(rolled)
			if err != nil {
				return err
			}
			if directDigest != rolledDigest {
				return store.ErrIntegrity
			}
			var checkpoint SeriesCheckpoint
			var revision uint64
			if err = tx.QueryRow(ctx, `SELECT watermark_height,watermark_timestamp,has_gaps,revision FROM `+st.Schema+`.chart_series_clock WHERE chain_id='125' AND pool=$1`, pool).Scan(&checkpoint.Height, &checkpoint.Timestamp, &checkpoint.HasGaps, &revision); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO `+st.Schema+`.chart_rollup_lineage VALUES('125',$1,$2,$3,$4,$5,$6) ON CONFLICT(chain_id,pool,interval,start) DO UPDATE SET child_digest=EXCLUDED.child_digest,source_watermark=EXCLUDED.source_watermark,algorithm=EXCLUDED.algorithm`, pool, interval, start, digest, checkpoint.Height, SeriesAlgorithm); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `DELETE FROM `+st.Schema+`.chart_rollup_pending WHERE chain_id='125' AND pool=$1 AND interval=$2 AND start=$3 AND child_revision=$4`, pool, interval, start, childRevision)
			if err != nil {
				return err
			}
			return tx.Commit(ctx)
		}()
		tx.Rollback(ctx)
		if err == pgx.ErrNoRows {
			return completed, nil
		}
		if err != nil {
			return completed, err
		}
		completed++
	}
	return completed, nil
}

func regexpActor(actor string) bool {
	if len(actor) != 42 || actor[:2] != "0x" {
		return false
	}
	for _, c := range actor[2:] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
