package discovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type AlertTrigger struct {
	ID        string
	Rule      AlertRule
	Metric    Metric
	CreatedAt time.Time
}

func (s Store) SaveRule(ctx context.Context, actor string, r AlertRule, expected uint64) error {
	if actor == "" || r.OwnerAccountID != actor {
		return ErrForbidden
	}
	if r.Metric == "" || len(r.Metric) > 80 {
		return ErrInvalid
	}
	var e error
	r, e = NewAlertRule(r)
	if e != nil {
		return e
	}
	if r.Revision != expected+1 || expected == ^uint64(0) {
		return ErrConflict
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if e := lock(ctx, tx, "rules:"+actor); e != nil {
			return e
		}
		var b []byte
		e := tx.QueryRowContext(ctx, `SELECT document FROM narrative_discovery.rules WHERE id=$1 FOR UPDATE`, r.ID).Scan(&b)
		if e == sql.ErrNoRows {
			if expected != 0 {
				return ErrConflict
			}
			var count int
			e = tx.QueryRowContext(ctx, `SELECT count(*) FROM narrative_discovery.rules WHERE account_id=$1 AND document->>'Disabled'='false'`, actor).Scan(&count)
			if e != nil {
				return e
			}
			if !r.Disabled && count >= 100 {
				return ErrInvalid
			}
			_, e = tx.ExecContext(ctx, `INSERT INTO narrative_discovery.rules(id,account_id,document,checkpoint)VALUES($1,$2,$3,$4)`, r.ID, actor, encode(r), encode(AlertCheckpoint{}))
			return e
		}
		if e != nil {
			return e
		}
		var old AlertRule
		if e = json.Unmarshal(b, &old); e != nil {
			return e
		}
		if old.OwnerAccountID != actor {
			return ErrForbidden
		}
		if old.Revision != expected {
			return ErrConflict
		}
		if old.Disabled && !r.Disabled {
			var count int
			if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM narrative_discovery.rules WHERE account_id=$1 AND document->>'Disabled'='false'`, actor).Scan(&count); e != nil {
				return e
			}
			if count >= 100 {
				return ErrInvalid
			}
		}
		_, e = tx.ExecContext(ctx, `UPDATE narrative_discovery.rules SET document=$1 WHERE id=$2`, encode(r), r.ID)
		return e
	})
}
func (s Store) EvaluateRule(ctx context.Context, id string, m Metric, now time.Time) (bool, string, error) {
	crossed := false
	reason := ""
	e := s.transaction(ctx, func(tx *sql.Tx) error {
		var rb, cb []byte
		e := tx.QueryRowContext(ctx, `SELECT document,checkpoint FROM narrative_discovery.rules WHERE id=$1 FOR UPDATE`, id).Scan(&rb, &cb)
		if e != nil {
			return e
		}
		var r AlertRule
		var checkpoint AlertCheckpoint
		if e = json.Unmarshal(rb, &r); e == nil {
			e = json.Unmarshal(cb, &checkpoint)
		}
		if e != nil {
			return e
		}
		next, c, why, e := Evaluate(r, checkpoint, m, now)
		if e != nil {
			return e
		}
		crossed = c
		reason = why
		_, e = tx.ExecContext(ctx, `UPDATE narrative_discovery.rules SET checkpoint=$1,last_reason=$3 WHERE id=$2`, encode(next), id, why)
		if e != nil || !c {
			return e
		}
		trigger := AlertTrigger{ID: fmt.Sprintf("%s:%d:%d", r.ID, r.Revision, m.Revision), Rule: r, Metric: m, CreatedAt: now}
		_, e = tx.ExecContext(ctx, `INSERT INTO narrative_discovery.triggers(id,rule_id,rule_revision,metric_revision,document)VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, trigger.ID, r.ID, r.Revision, m.Revision, encode(trigger))
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO narrative_discovery.outbox(id,kind,document)VALUES($1,'threshold-crossing-v2',$2)ON CONFLICT DO NOTHING`, trigger.ID, encode(trigger))
		return e
	})
	return crossed, reason, e
}
