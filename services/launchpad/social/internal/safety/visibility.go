package safety

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Visibility is a public-safe tombstone: private evidence and reporter identity
// never leave the separate restricted case store.
type Visibility struct {
	TargetType string `json:"targetType"`
	TargetID   string `json:"targetId"`
	Status     string `json:"status"`
	CaseID     string `json:"caseId"`
	Revision   int64  `json:"revision"`
}

func (s *Store) SetVisibility(ctx context.Context, caseID string, expected int64, status, reason string) (Visibility, error) {
	if !s.available() {
		return Visibility{}, ErrUnavailable
	}
	p, e := Actor(ctx, s.now())
	if e != nil {
		return Visibility{}, e
	}
	if expected < 0 || !contains([]string{"removed", "restored"}, status) || !Bounded(reason, 1000) {
		return Visibility{}, ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Visibility{}, e
	}
	defer tx.Rollback(ctx)
	var v Visibility
	var pool, affected string
	v.CaseID = caseID
	v.Status = status
	e = tx.QueryRow(ctx, `SELECT target_type,target_id,pool_id,affected_wallet FROM safety_cases WHERE id=$1 FOR UPDATE`, caseID).Scan(&v.TargetType, &v.TargetID, &pool, &affected)
	if e != nil {
		return v, e
	}
	if !Authorized(p, pool, true, s.now()) {
		tx.Rollback(ctx)
		return v, s.denial(ctx, p, "visibility")
	}
	// The target key is locked even when another case references the same target.
	_, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, v.TargetType+":"+v.TargetID)
	if e != nil {
		return v, e
	}
	var revision int64
	e = tx.QueryRow(ctx, `SELECT revision FROM safety_visibility WHERE target_type=$1 AND target_id=$2`, v.TargetType, v.TargetID).Scan(&revision)
	if e != nil && e != pgx.ErrNoRows {
		return v, e
	}
	if revision != expected || (revision == 0 && status == "restored") {
		return v, ErrConflict
	}
	if status == "restored" && affected != "" {
		decision := s.CheckTx(ctx, tx, affected, "global", "")
		if decision == Unavailable {
			return v, ErrUnavailable
		}
		if decision != Allow {
			return v, ErrForbidden
		}
		if pool != "" {
			decision = s.CheckTx(ctx, tx, affected, "pool", pool)
			if decision == Unavailable {
				return v, ErrUnavailable
			}
			if decision != Allow {
				return v, ErrForbidden
			}
		}
	}
	v.Revision = revision + 1
	_, e = tx.Exec(ctx, `INSERT INTO safety_visibility(target_type,target_id,revision,status,case_id)VALUES($1,$2,$3,$4,$5) ON CONFLICT(target_type,target_id) DO UPDATE SET revision=EXCLUDED.revision,status=EXCLUDED.status,case_id=EXCLUDED.case_id`, v.TargetType, v.TargetID, v.Revision, status, caseID)
	if e != nil {
		return v, e
	}
	if _, e = audit(ctx, tx, p, &caseID, "visibility_"+status, reason, "policy-v1", v.Revision); e != nil {
		return v, e
	}
	payload, e := json.Marshal(v)
	if e != nil {
		return v, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO safety_outbox(id,kind,entity_id,revision,payload)VALUES($1,'visibility_changed',$2,$3,$4)`, uuid.NewString(), v.TargetType+":"+v.TargetID, v.Revision, payload); e != nil {
		return v, e
	}
	return v, tx.Commit(ctx)
}
func (s *Store) Visibility(ctx context.Context, targetType, targetID string) (Visibility, error) {
	var v Visibility
	if !s.available() {
		return v, ErrUnavailable
	}
	v.TargetType = targetType
	v.TargetID = targetID
	e := s.DB.QueryRow(ctx, `SELECT revision,status,case_id FROM safety_visibility WHERE target_type=$1 AND target_id=$2`, targetType, targetID).Scan(&v.Revision, &v.Status, &v.CaseID)
	return v, e
}

// ApplyVisibility refuses stale replay. Consumers must seed authoritative
// tombstones before exposing replayed content; this helper is not a cache loader.
func ApplyVisibility(current, incoming Visibility) Visibility {
	if current.TargetType != incoming.TargetType || current.TargetID != incoming.TargetID {
		return current
	}
	if incoming.Revision > current.Revision && incoming.Revision > 0 && contains([]string{"removed", "restored"}, incoming.Status) {
		return incoming
	}
	return current
}

type ReplayContent struct {
	TargetType         string `json:"targetType"`
	TargetID           string `json:"targetId"`
	VisibilityRevision int64  `json:"visibilityRevision"`
	Text               string `json:"text"`
}

func (s *Store) ProjectReplay(ctx context.Context, event ReplayContent) (ReplayContent, error) {
	public := event
	public.Text = ""
	if !Bounded(event.TargetType, 64) || !Bounded(event.TargetID, 160) || event.VisibilityRevision < 0 {
		return public, ErrInvalid
	}
	current, e := s.Visibility(ctx, event.TargetType, event.TargetID)
	if e == pgx.ErrNoRows {
		if event.VisibilityRevision != 0 {
			return public, ErrConflict
		}
		return event, nil
	}
	if e != nil {
		return public, ErrUnavailable
	}
	public.VisibilityRevision = current.Revision
	if current.Status == "removed" {
		return public, nil
	}
	if current.Status != "restored" || event.VisibilityRevision != current.Revision {
		return public, ErrConflict
	}
	return event, nil
}
