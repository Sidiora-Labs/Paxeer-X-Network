package discovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type CalloutService struct {
	Store     Store
	Authority SourceAuthority
}

func (c CalloutService) Publish(ctx context.Context, actor, squadID string, next Callout) error {
	if c.Authority == nil {
		return ErrUnavailable
	}
	verified, e := c.Authority.VerifiedAccount(ctx, actor)
	if e != nil {
		return e
	}
	if !verified {
		return ErrForbidden
	}
	ok, e := c.Authority.CanonicalPool(ctx, next.PoolAddress)
	if e != nil {
		return e
	}
	if !ok {
		return ErrInvalid
	}
	if next.Revision != 1 || next.AuthorAccountID != actor {
		return ErrForbidden
	}
	policy, ok := c.Authority.(PublicationPolicy)
	if !ok {
		return ErrUnavailable
	}
	decision, e := policy.ReviewCallout(ctx, actor, next.PoolAddress, next.Body)
	if e != nil {
		return e
	}
	if !decision.Visible || decision.Revision == 0 || decision.ObservedAt.After(time.Now()) || time.Since(decision.ObservedAt) > 60*time.Second {
		return ErrForbidden
	}
	if e = next.ValidatePublication(actor, next.PoolAddress); e != nil {
		return e
	}
	return c.Store.transaction(ctx, func(tx *sql.Tx) error {
		if e := lock(ctx, tx, "callout-author:"+actor); e != nil {
			return e
		}
		var count int
		e := tx.QueryRowContext(ctx, `SELECT count(*) FROM narrative_discovery.callouts WHERE document->>'AuthorAccountID'=$1 AND (document->>'CreatedAt')::timestamptz>clock_timestamp()-interval '1 minute'`, actor).Scan(&count)
		if e != nil {
			return e
		}
		if count >= 10 {
			return ErrForbidden
		}
		if squadID != "" {
			if e = lock(ctx, tx, "squad:"+squadID); e != nil {
				return e
			}
			m, e := member(ctx, tx, squadID, actor)
			if e != nil || !m.CanPublish() {
				return ErrForbidden
			}
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO narrative_discovery.callouts(id,document)VALUES($1,$2)`, next.ID, encode(next))
		if e != nil {
			return e
		}
		return persistCallout(ctx, tx, next)
	})
}
func persistCallout(ctx context.Context, tx *sql.Tx, c Callout) error {
	_, e := tx.ExecContext(ctx, `INSERT INTO narrative_discovery.callout_revisions(id,revision,document)VALUES($1,$2,$3)`, c.ID, c.Revision, encode(c))
	if e != nil {
		return e
	}
	if c.Deleted {
		_, e = tx.ExecContext(ctx, `UPDATE narrative_discovery.callout_revisions SET document=jsonb_set(jsonb_set(document,'{Body}','""'::jsonb),'{PositionDisclosure}','""'::jsonb) WHERE id=$1`, c.ID)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE narrative_discovery.outbox SET document=jsonb_set(jsonb_set(document,'{Body}','""'::jsonb),'{PositionDisclosure}','""'::jsonb) WHERE id LIKE $1`, "callout:"+c.ID+":%")
		if e != nil {
			return e
		}
	}
	ref := SourceRef{ID: c.ID, EventID: fmt.Sprintf("callout:%s:%d", c.ID, c.Revision), Kind: "callout", CreatorPublicID: c.AuthorAccountID, Pools: []string{c.PoolAddress}, SourceRevision: c.Revision, VisibilityRevision: c.Revision, PublishedAt: c.CreatedAt, Public: !c.Deleted, Authorized: !c.Deleted, Tombstone: c.Deleted, Corrected: c.Revision > 1 && !c.Deleted}
	if e = lock(ctx, tx, "source:"+c.ID); e != nil {
		return e
	}
	var old SourceRef
	var b []byte
	e = tx.QueryRowContext(ctx, `SELECT document FROM narrative_discovery.sources WHERE id=$1`, c.ID).Scan(&b)
	if e != nil && e != sql.ErrNoRows {
		return e
	}
	if e == nil {
		if e = json.Unmarshal(b, &old); e != nil {
			return e
		}
	}
	if _, e = ApplySource(old, ref); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO narrative_discovery.sources(id,kind,document,published_at,event_id,observed_at)VALUES($1,'callout',$2,$3,$4,clock_timestamp())ON CONFLICT(id)DO UPDATE SET document=EXCLUDED.document,event_id=EXCLUDED.event_id,observed_at=EXCLUDED.observed_at`, c.ID, encode(ref), c.CreatedAt, ref.EventID)
	if e != nil {
		return e
	}
	event := struct {
		TargetKind, TargetID, Actor string
		Revision                    uint64
		Removed                     bool
		CreatedAt                   time.Time
	}{"callout", c.ID, c.AuthorAccountID, c.Revision, c.Deleted, c.CreatedAt}
	_, e = tx.ExecContext(ctx, `INSERT INTO narrative_discovery.outbox(id,kind,document)VALUES($1,'callout-target-ref-v2',$2)`, ref.EventID, encode(event))
	return e
}
func (c CalloutService) Edit(ctx context.Context, actor, id, body string, expected uint64, remove bool) (Callout, error) {
	var next Callout
	e := c.Store.transaction(ctx, func(tx *sql.Tx) error {
		var b []byte
		e := tx.QueryRowContext(ctx, `SELECT document FROM narrative_discovery.callouts WHERE id=$1 FOR UPDATE`, id).Scan(&b)
		if e != nil {
			return e
		}
		if e = json.Unmarshal(b, &next); e != nil {
			return e
		}
		if actor == "" || actor != next.AuthorAccountID {
			return ErrForbidden
		}
		if expected != next.Revision {
			return ErrConflict
		}
		if remove {
			next, e = next.Remove(actor, expected)
		} else {
			policy, ok := c.Authority.(PublicationPolicy)
			if !ok {
				return ErrUnavailable
			}
			decision, err := policy.ReviewCallout(ctx, actor, next.PoolAddress, body)
			if err != nil {
				return err
			}
			if !decision.Visible || decision.Revision == 0 || decision.ObservedAt.After(time.Now()) || time.Since(decision.ObservedAt) > 60*time.Second {
				return ErrForbidden
			}
			next, e = next.Revise(actor, body, expected)
		}
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE narrative_discovery.callouts SET document=$1 WHERE id=$2`, encode(next), id)
		if e != nil {
			return e
		}
		return persistCallout(ctx, tx, next)
	})
	return next, e
}
func (c CalloutService) Read(ctx context.Context, id string) (Callout, error) {
	var result Callout
	var b []byte
	e := c.Store.DB.QueryRowContext(ctx, `SELECT document FROM narrative_discovery.callouts WHERE id=$1`, id).Scan(&b)
	if e == nil {
		e = json.Unmarshal(b, &result)
	}
	return result, e
}
