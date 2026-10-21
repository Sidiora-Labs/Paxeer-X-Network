package search

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Store struct{ Pool *pgxpool.Pool }

// Search takes a bounded repeatable-read snapshot and rechecks current moderation
// authority even for retained generations. Redis is never an authority.
func (s Store) Search(ctx context.Context, r Request) (Page, error) {
	q, e := ValidateRequest(r)
	if e != nil {
		return Page{}, e
	}
	if s.Pool == nil {
		return Page{}, fmt.Errorf("%w: missing PostgreSQL pool", ErrUnavailable)
	}
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	tx, e := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return Page{}, fmt.Errorf("%w: begin", ErrUnavailable)
	}
	defer rollbackSearch(tx)
	if _, e = tx.Exec(ctx, "SET LOCAL statement_timeout = '150ms'"); e != nil {
		return Page{}, fmt.Errorf("%w: timeout configuration", ErrUnavailable)
	}
	p := Page{Items: []Hit{}, MatchKind: q.Kind, Status: "ok"}
	if e = tx.QueryRow(ctx, "SELECT generation,watermark FROM stats_search.generation WHERE singleton").Scan(&p.Generation, &p.Watermark); e != nil {
		return Page{}, fmt.Errorf("%w: index watermark", ErrUnavailable)
	}
	after := ""
	if c := r.Cursor; c != nil {
		if c.Generation != p.Generation || c.Snapshot != p.Watermark {
			return Page{}, fmt.Errorf("%w: stale cursor", ErrInvalid)
		}
		after = c.After
	}
	predicate := `(d.name_key LIKE $2 ESCAPE '\' OR d.ticker_key LIKE $2 ESCAPE '\')`
	value := like(q.Key)
	if q.Kind == "exact_address" {
		predicate = `(d.pool_address=$2 OR d.token_address=$2)`
		value = q.Key
	}
	if q.Kind == "partial_address" {
		predicate = `(d.pool_address LIKE $2 ESCAPE '\' OR d.token_address LIKE $2 ESCAPE '\')`
		value = q.Key + "%"
	}
	// Exact pool identity sorts first; token associations are deterministic by pool.
	rows, e := tx.Query(ctx, `SELECT d.pool_address,d.token_address,d.name FROM stats_search.documents d
 JOIN stats_search.visibility v ON v.pool_address=d.pool_address
 WHERE d.generation=$1 AND `+predicate+` AND d.canonical AND NOT d.quarantined
 AND v.public AND NOT v.tombstone AND v.revision>=d.visibility_revision
 AND d.revision<=$3 AND ($4='' OR d.pool_status=$4) AND ($5='' OR (CASE WHEN d.pool_address=$6 THEN 0 ELSE 1 END,d.pool_address) > (CASE WHEN $5=$6 THEN 0 ELSE 1 END,$5))
 ORDER BY (d.pool_address=$6) DESC,d.pool_address ASC LIMIT $7`, p.Generation, value, p.Watermark, r.Filter.PoolStatus, after, q.Key, r.Limit+1)
	if e != nil {
		return Page{}, fmt.Errorf("%w: query", ErrUnavailable)
	}
	defer rows.Close()
	for rows.Next() {
		var pool, token, name string
		if e = rows.Scan(&pool, &token, &name); e != nil {
			return Page{}, fmt.Errorf("%w: decode", ErrUnavailable)
		}
		p.Items = append(p.Items, Hit{Kind: r.Kind, ID: "125:" + pool, Label: name, PoolAddress: &pool, TokenAddress: &token, Score: "1"})
	}
	if e = rows.Err(); e != nil {
		return Page{}, fmt.Errorf("%w: rows", ErrUnavailable)
	}
	if len(p.Items) > r.Limit {
		p.Items = p.Items[:r.Limit]
		p.Next = &Cursor{Version: Version, NormalizationVersion: Version, FilterDigest: Binding(q, r), Generation: p.Generation, Snapshot: p.Watermark, After: *p.Items[len(p.Items)-1].PoolAddress}
	}
	if len(p.Items) == 0 && q.Kind == "exact_address" {
		p.Status = "not-indexed"
	}
	if e = tx.Commit(ctx); e != nil {
		return Page{}, fmt.Errorf("%w: commit", ErrUnavailable)
	}
	return p, nil
}
