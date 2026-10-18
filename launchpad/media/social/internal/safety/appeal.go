package safety

import (
	"context"
	"time"
)

type AppealOutcome struct {
	ID        string     `json:"id"`
	ActionID  string     `json:"actionId"`
	State     string     `json:"state"`
	Revision  int64      `json:"revision"`
	Reason    string     `json:"reason"`
	DecidedAt *time.Time `json:"decidedAt"`
}

func (s *Store) PrivateAppeal(ctx context.Context, id string) (AppealOutcome, error) {
	var result AppealOutcome
	if !s.available() {
		return result, ErrUnavailable
	}
	p, e := Actor(ctx, s.now())
	if e != nil {
		return result, e
	}
	e = s.DB.QueryRow(ctx, `SELECT ap.id,ap.action_id,ap.state,ap.revision,COALESCE((SELECT decision.reason FROM safety_actions decision WHERE decision.case_id=a.case_id AND decision.kind='appeal_'||ap.state AND decision.revision=ap.revision ORDER BY decision.created_at DESC,decision.id DESC LIMIT 1),''),ap.decided_at FROM safety_appeals ap JOIN safety_actions a ON a.id=ap.action_id WHERE ap.id=$1 AND ap.appellant=$2`, id, p.Wallet).Scan(&result.ID, &result.ActionID, &result.State, &result.Revision, &result.Reason, &result.DecidedAt)
	return result, e
}

func (s *Store) ReviewAppeal(ctx context.Context, id string, expected int64, reason string) error {
	if !s.available() {
		return ErrUnavailable
	}
	p, e := Actor(ctx, s.now())
	if e != nil {
		return e
	}
	if expected < 1 || !Bounded(reason, 1000) {
		return ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var issuer, caseID, pool, state, caseState string
	var rev int64
	e = tx.QueryRow(ctx, `SELECT a.actor,c.id,c.pool_id,ap.state,ap.revision,c.state FROM safety_appeals ap JOIN safety_actions a ON a.id=ap.action_id JOIN safety_cases c ON c.id=a.case_id WHERE ap.id=$1 FOR UPDATE OF c,ap`, id).Scan(&issuer, &caseID, &pool, &state, &rev, &caseState)
	if e != nil {
		return e
	}
	if issuer == p.Wallet || !Authorized(p, pool, true, s.now()) {
		tx.Rollback(ctx)
		return s.denial(ctx, p, "appeal_review")
	}
	if rev != expected || state != "submitted" || caseState != "appealed" {
		return ErrConflict
	}
	if _, e = tx.Exec(ctx, `UPDATE safety_appeals SET state='reviewing',revision=revision+1,reviewer=$2 WHERE id=$1`, id, p.Wallet); e != nil {
		return e
	}
	if _, e = audit(ctx, tx, p, &caseID, "appeal_reviewing", reason, "policy-v1", rev+1); e != nil {
		return e
	}
	if e = outbox(ctx, tx, "appeal_changed", id, rev+1); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
