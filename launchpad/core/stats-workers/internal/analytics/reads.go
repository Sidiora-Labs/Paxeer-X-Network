package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"strconv"
	"time"
)

type SnapshotRead struct {
	Snapshot Snapshot `json:"snapshot"`
	Stale    bool     `json:"stale"`
}

func (a ReactionAuthority) ReadSnapshot(ctx context.Context, pool, model string, now int64) (SnapshotRead, error) {
	if a.Pool == nil {
		return SnapshotRead{}, ErrDependencyUnavailable
	}
	if !address.MatchString(pool) || !noncePattern.MatchString(model) {
		return SnapshotRead{}, ErrInvalidInput
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	table, err := a.table("analytics_snapshots")
	if err != nil {
		return SnapshotRead{}, err
	}
	var raw []byte
	var height, revision string
	if err = a.Pool.QueryRow(ctx, "SELECT height::text,revision::text,payload FROM "+table+" WHERE pool=$1 AND model=$2", pool, model).Scan(&height, &revision, &raw); err != nil {
		return SnapshotRead{}, err
	}
	var s Snapshot
	if json.Unmarshal(raw, &s) != nil || s.Pool != pool || s.Model != model || s.Validate() != nil || s.Watermark.Height != height || s.Watermark.ProjectionRevision != revision {
		return SnapshotRead{}, ErrConflict
	}
	observed, err := time.Parse("2006-01-02T15:04:05.000Z", s.Watermark.ObservedAt)
	if err != nil {
		return SnapshotRead{}, err
	}
	return SnapshotRead{Snapshot: s, Stale: s.Stale(now) || now < observed.Unix() || now-observed.Unix() > 30}, nil
}

type PublicReactions struct {
	Pool     string           `json:"poolAddress"`
	Revision string           `json:"revision"`
	Counts   map[string]int64 `json:"reactions"`
}

func (a ReactionAuthority) ReadReactions(ctx context.Context, pool string) (PublicReactions, error) {
	if a.Pool == nil {
		return PublicReactions{}, ErrDependencyUnavailable
	}
	if !address.MatchString(pool) {
		return PublicReactions{}, ErrInvalidInput
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := a.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return PublicReactions{}, err
	}
	defer tx.Rollback(ctx)
	p, err := a.table("reaction_pools")
	if err != nil {
		return PublicReactions{}, err
	}
	votes, _ := a.table("reaction_votes")
	result := PublicReactions{Pool: pool, Revision: "0", Counts: map[string]int64{}}
	for _, r := range []string{"bullish", "hot", "diamond", "bearish", "trash", "warning"} {
		result.Counts[r] = 0
	}
	err = tx.QueryRow(ctx, "SELECT revision::text FROM "+p+" WHERE pool=$1", pool).Scan(&result.Revision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	rows, err := tx.Query(ctx, "SELECT reaction,count(*) FROM "+votes+" WHERE pool=$1 AND reaction<>'clear' GROUP BY reaction", pool)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var r string
		var count int64
		if err = rows.Scan(&r, &count); err != nil {
			rows.Close()
			return result, err
		}
		result.Counts[r] = count
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

type ReplayItem struct {
	Cursor   string   `json:"cursor"`
	Snapshot Snapshot `json:"snapshot"`
}
type ReplayPage struct {
	Items      []ReplayItem `json:"items"`
	NextCursor string       `json:"nextCursor"`
}

func (a ReactionAuthority) ReplaySnapshots(ctx context.Context, pool, after string, limit int) (ReplayPage, error) {
	if a.Pool == nil {
		return ReplayPage{}, ErrDependencyUnavailable
	}
	if !address.MatchString(pool) || limit < 1 || limit > 100 {
		return ReplayPage{}, ErrInvalidInput
	}
	cursor, err := strconv.ParseInt(after, 10, 64)
	if err != nil || cursor < 0 || strconv.FormatInt(cursor, 10) != after {
		return ReplayPage{}, ErrInvalidInput
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	table, err := a.table("analytics_outbox")
	if err != nil {
		return ReplayPage{}, err
	}
	rows, err := a.Pool.Query(ctx, "SELECT id::text,model,revision::text,payload FROM "+table+" WHERE pool=$1 AND id>$2 ORDER BY id LIMIT $3", pool, cursor, limit)
	if err != nil {
		return ReplayPage{}, err
	}
	defer rows.Close()
	page := ReplayPage{Items: []ReplayItem{}, NextCursor: after}
	for rows.Next() {
		var item ReplayItem
		var raw []byte
		var model, revision string
		if err = rows.Scan(&item.Cursor, &model, &revision, &raw); err != nil {
			return page, err
		}
		if json.Unmarshal(raw, &item.Snapshot) != nil || item.Snapshot.Pool != pool || item.Snapshot.Validate() != nil || item.Snapshot.Model != model || item.Snapshot.Watermark.ProjectionRevision != revision {
			return page, ErrConflict
		}
		page.Items = append(page.Items, item)
		page.NextCursor = item.Cursor
	}
	return page, rows.Err()
}
