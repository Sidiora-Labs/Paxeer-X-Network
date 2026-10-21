package discovery

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"time"
)

//go:embed schema.sql
var Schema string

type Store struct{ DB *sql.DB }

var ErrUnavailable = errors.New("discovery authority unavailable")

func encode(v any) []byte { b, _ := json.Marshal(v); return b }
func lock(ctx context.Context, tx *sql.Tx, key string) error {
	_, e := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,210021))`, key)
	return e
}
func (s Store) transaction(ctx context.Context, fn func(*sql.Tx) error) error {
	if s.DB == nil {
		return ErrUnavailable
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit()
}
func (s Store) Apply(ctx context.Context, consumer, event string, next SourceRef, observed time.Time) error {
	if consumer == "" || event == "" || next.Validate() != nil || observed.IsZero() || observed.After(time.Now()) {
		return ErrInvalid
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if e := lock(ctx, tx, "source:"+next.ID); e != nil {
			return e
		}
		var old SourceRef
		var b []byte
		e := tx.QueryRowContext(ctx, `SELECT document FROM narrative_discovery.sources WHERE id=$1`, next.ID).Scan(&b)
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		if e == nil {
			if e = json.Unmarshal(b, &old); e != nil {
				return e
			}
		}
		accepted, e := ApplySource(old, next)
		if e != nil {
			return e
		}
		res, e := tx.ExecContext(ctx, `INSERT INTO narrative_discovery.consumer_events(consumer,event_id,observed_at)VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, consumer, event, observed)
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if e != nil || n == 0 {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO narrative_discovery.sources(id,kind,document,published_at,event_id,observed_at)VALUES($1,$2,$3,$4,$5,$6)ON CONFLICT(id) DO UPDATE SET document=EXCLUDED.document,event_id=EXCLUDED.event_id,observed_at=EXCLUDED.observed_at`, accepted.ID, accepted.Kind, encode(accepted), accepted.PublishedAt, accepted.EventID, observed)
		return e
	})
}
func (s Store) Sources(ctx context.Context, now time.Time) ([]SourceRef, error) {
	rows, e := s.DB.QueryContext(ctx, `SELECT document FROM narrative_discovery.sources WHERE observed_at>=$1 AND observed_at<=$2 ORDER BY published_at DESC,event_id LIMIT 2000`, now.Add(-60*time.Second), now)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []SourceRef{}
	for rows.Next() {
		var b []byte
		var ref SourceRef
		if e = rows.Scan(&b); e == nil {
			e = json.Unmarshal(b, &ref)
		}
		if e != nil {
			return nil, e
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}
func (s Store) Source(ctx context.Context, id string) (SourceRef, error) {
	var ref SourceRef
	var b []byte
	e := s.DB.QueryRowContext(ctx, `SELECT document FROM narrative_discovery.sources WHERE id=$1`, id).Scan(&b)
	if e == nil {
		e = json.Unmarshal(b, &ref)
	}
	return ref, e
}
func (s Store) EraseAttribution(ctx context.Context, account string) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE narrative_discovery.sources SET document=jsonb_set(document,'{CreatorPublicID}','""'::jsonb) WHERE document->>'CreatorPublicID'=$1`, account)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `DELETE FROM narrative_discovery.snapshots WHERE account_id=$1`, account)
		return e
	})
}

func (s Store) DeleteAccount(ctx context.Context, account string) error {
	if account == "" {
		return ErrInvalid
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT document FROM narrative_discovery.callouts WHERE document->>'AuthorAccountID'=$1 FOR UPDATE`, account)
		if e != nil {
			return e
		}
		callouts := []Callout{}
		for rows.Next() {
			var b []byte
			var c Callout
			if e = rows.Scan(&b); e == nil {
				e = json.Unmarshal(b, &c)
			}
			if e != nil {
				rows.Close()
				return e
			}
			callouts = append(callouts, c)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, c := range callouts {
			if !c.Deleted {
				next, e := c.Remove(account, c.Revision)
				if e != nil {
					return e
				}
				if e = persistCallout(ctx, tx, next); e != nil {
					return e
				}
				c = next
			}
			c.AuthorAccountID = ""
			_, e = tx.ExecContext(ctx, `UPDATE narrative_discovery.callouts SET document=$1 WHERE id=$2`, encode(c), c.ID)
			if e != nil {
				return e
			}
			_, e = tx.ExecContext(ctx, `UPDATE narrative_discovery.callout_revisions SET document=jsonb_set(document,'{AuthorAccountID}','""'::jsonb) WHERE id=$1`, c.ID)
			if e != nil {
				return e
			}
		}
		_, e = tx.ExecContext(ctx, `DELETE FROM narrative_discovery.snapshots WHERE account_id=$1`, account)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE narrative_discovery.sources SET document=jsonb_set(document,'{CreatorPublicID}','""'::jsonb) WHERE document->>'CreatorPublicID'=$1`, account)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE narrative_discovery.rules SET document=jsonb_set(document,'{Disabled}','true'::jsonb) WHERE account_id=$1`, account)
		return e
	})
}

func NewStore(pool *pgxpool.Pool) (Store, error) {
	if pool == nil {
		return Store{}, ErrUnavailable
	}
	return Store{DB: stdlib.OpenDBFromPool(pool)}, nil
}
