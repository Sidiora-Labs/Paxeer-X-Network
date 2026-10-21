package safety

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ReversalCommand struct {
	AppealID            string `json:"appealId"`
	BanID               string `json:"banId"`
	ExpectedRevision    int64  `json:"expectedRevision"`
	ExpectedBanRevision int64  `json:"expectedBanRevision"`
	Reason              string `json:"reason"`
	PolicyVersion       string `json:"policyVersion"`
}

func (s *Store) ReverseBanAppeal(ctx context.Context, c ReversalCommand) error {
	if !s.available() {
		return ErrUnavailable
	}
	p, e := Actor(ctx, s.now())
	if e != nil {
		return e
	}
	if _, e = uuid.Parse(c.AppealID); e != nil {
		return ErrInvalid
	}
	if _, e = uuid.Parse(c.BanID); e != nil || c.ExpectedRevision < 1 || c.ExpectedBanRevision < 1 || !Bounded(c.Reason, 1000) || !Bounded(c.PolicyVersion, 160) {
		return ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var issuer, caseID, pool, state, appellant, scope, resource, banWallet, caseState string
	var revision, caseRev, banRev int64
	e = tx.QueryRow(ctx, `SELECT a.actor,c.id,c.pool_id,ap.state,ap.appellant,ap.revision,c.revision,c.state FROM safety_appeals ap JOIN safety_actions a ON a.id=ap.action_id JOIN safety_cases c ON c.id=a.case_id WHERE ap.id=$1 FOR UPDATE OF c,ap`, c.AppealID).Scan(&issuer, &caseID, &pool, &state, &appellant, &revision, &caseRev, &caseState)
	if e != nil {
		return e
	}
	if issuer == p.Wallet || !Authorized(p, pool, true, s.now()) {
		tx.Rollback(ctx)
		return s.denial(ctx, p, "appeal_reversal")
	}
	if revision != c.ExpectedRevision || (state != "submitted" && state != "reviewing") || caseState != "appealed" {
		return ErrConflict
	}
	e = tx.QueryRow(ctx, `SELECT b.scope,b.resource,b.wallet,b.revision FROM safety_bans b JOIN safety_action_bans link ON link.ban_id=b.id JOIN safety_appeals ap ON ap.action_id=link.action_id WHERE b.id=$1 AND ap.id=$2 AND b.revoked_at IS NULL FOR UPDATE OF b`, c.BanID, c.AppealID).Scan(&scope, &resource, &banWallet, &banRev)
	if e != nil {
		return e
	}
	if banWallet != appellant || banRev != c.ExpectedBanRevision {
		return ErrConflict
	}
	banPool := ""
	if scope == "pool" {
		banPool = resource
	}
	if !Authorized(p, banPool, true, s.now()) {
		tx.Rollback(ctx)
		return s.denial(ctx, p, "appeal_reversal")
	}
	if _, e = tx.Exec(ctx, `UPDATE safety_bans SET revoked_at=$2,revision=revision+1 WHERE id=$1`, c.BanID, s.now()); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE safety_appeals SET state='reversed',revision=revision+1,reviewer=$2,decided_at=$3 WHERE id=$1`, c.AppealID, p.Wallet, s.now()); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE safety_cases SET state='final',revision=revision+1 WHERE id=$1`, caseID); e != nil {
		return e
	}
	if _, e = audit(ctx, tx, p, &caseID, "appeal_reversed", c.Reason, c.PolicyVersion, revision+1); e != nil {
		return e
	}
	if e = outbox(ctx, tx, "policy_changed", c.BanID, banRev+1); e != nil {
		return e
	}
	if e = outbox(ctx, tx, "appeal_changed", c.AppealID, revision+1); e != nil {
		return e
	}
	if e = outbox(ctx, tx, "case_changed", caseID, caseRev+1); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) CheckTx(ctx context.Context, tx pgx.Tx, wallet, scope, resource string) Decision {
	if tx == nil || !walletRE.MatchString(wallet) || !validScope(scope, resource) {
		return Unavailable
	}
	var banned bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM safety_bans WHERE wallet=$1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>$4) AND (scope='global' OR (scope=$2 AND resource=$3)))`, wallet, scope, resource, s.now()).Scan(&banned); e != nil {
		return Unavailable
	}
	if banned {
		return Deny
	}
	return Allow
}

func (s *Store) ActionBan(ctx context.Context, caseID string, expected int64, c BanCommand) (string, error) {
	if !s.available() {
		return "", ErrUnavailable
	}
	p, e := Actor(ctx, s.now())
	if e != nil {
		return "", e
	}
	if !walletRE.MatchString(c.Wallet) || !validScope(c.Scope, c.Resource) || !Bounded(c.Reason, 1000) || !Bounded(c.PolicyVersion, 160) || expected < 1 || c.ExpiresAt != nil && !c.ExpiresAt.After(s.now()) {
		return "", ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	var pool, state, affected string
	var rev int64
	if e = tx.QueryRow(ctx, `SELECT pool_id,state,affected_wallet,revision FROM safety_cases WHERE id=$1 FOR UPDATE`, caseID).Scan(&pool, &state, &affected, &rev); e != nil {
		return "", e
	}
	scopePool := ""
	if c.Scope == "pool" {
		scopePool = c.Resource
	}
	if !Authorized(p, pool, true, s.now()) || !Authorized(p, scopePool, true, s.now()) {
		tx.Rollback(ctx)
		return "", s.denial(ctx, p, "case_ban")
	}
	if rev != expected || (state != "open" && state != "triaged") || affected != c.Wallet || (c.Scope == "pool" && c.Resource != pool) {
		return "", ErrConflict
	}
	id := uuid.NewString()
	if _, e = tx.Exec(ctx, `INSERT INTO safety_bans(id,wallet,scope,resource,actor,expires_at)VALUES($1,$2,$3,$4,$5,$6)`, id, c.Wallet, c.Scope, c.Resource, p.Wallet, c.ExpiresAt); e != nil {
		return "", e
	}
	action, e := audit(ctx, tx, p, &caseID, "actioned", c.Reason, c.PolicyVersion, rev+1)
	if e != nil {
		return "", e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO safety_action_bans(action_id,ban_id)VALUES($1,$2)`, action, id); e != nil {
		return "", e
	}
	if _, e = tx.Exec(ctx, `UPDATE safety_cases SET state='actioned',revision=revision+1 WHERE id=$1`, caseID); e != nil {
		return "", e
	}
	if e = outbox(ctx, tx, "policy_changed", id, 1); e != nil {
		return "", e
	}
	if e = outbox(ctx, tx, "case_changed", caseID, rev+1); e != nil {
		return "", e
	}
	return id, tx.Commit(ctx)
}

type RestoreCommand struct {
	CaseID           string `json:"caseId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Reason           string `json:"reason"`
	PolicyVersion    string `json:"policyVersion"`
}

func (s *Store) RestoreAppealedVisibility(ctx context.Context, c RestoreCommand) (Visibility, error) {
	var v Visibility
	if !s.available() {
		return v, ErrUnavailable
	}
	p, e := Actor(ctx, s.now())
	if e != nil {
		return v, e
	}
	if c.ExpectedRevision < 1 || !Bounded(c.Reason, 1000) || !Bounded(c.PolicyVersion, 160) {
		return v, ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return v, e
	}
	defer tx.Rollback(ctx)
	var pool, affected, state string
	var reversed bool
	e = tx.QueryRow(ctx, `SELECT target_type,target_id,pool_id,affected_wallet,state,EXISTS(SELECT 1 FROM safety_actions a WHERE a.case_id=c.id AND a.kind='appeal_reversed') FROM safety_cases c WHERE id=$1 FOR UPDATE`, c.CaseID).Scan(&v.TargetType, &v.TargetID, &pool, &affected, &state, &reversed)
	if e != nil {
		return v, e
	}
	if !Authorized(p, pool, true, s.now()) {
		tx.Rollback(ctx)
		return v, s.denial(ctx, p, "appeal_restore")
	}
	if state != "final" || !reversed {
		return v, ErrConflict
	}
	if affected != "" {
		decision := s.CheckTx(ctx, tx, affected, "global", "")
		if decision == Unavailable {
			return v, ErrUnavailable
		}
		if decision == Deny {
			return v, ErrForbidden
		}
		if pool != "" {
			decision = s.CheckTx(ctx, tx, affected, "pool", pool)
			if decision == Unavailable {
				return v, ErrUnavailable
			}
			if decision == Deny {
				return v, ErrForbidden
			}
		}
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, v.TargetType+":"+v.TargetID); e != nil {
		return v, e
	}
	var revision int64
	var old string
	e = tx.QueryRow(ctx, `SELECT revision,status FROM safety_visibility WHERE target_type=$1 AND target_id=$2 FOR UPDATE`, v.TargetType, v.TargetID).Scan(&revision, &old)
	if e != nil {
		return v, e
	}
	if revision != c.ExpectedRevision || old != "removed" {
		return v, ErrConflict
	}
	v.Status = "restored"
	v.CaseID = c.CaseID
	v.Revision = revision + 1
	if _, e = tx.Exec(ctx, `UPDATE safety_visibility SET revision=$3,status='restored',case_id=$4 WHERE target_type=$1 AND target_id=$2`, v.TargetType, v.TargetID, v.Revision, c.CaseID); e != nil {
		return v, e
	}
	if _, e = audit(ctx, tx, p, &c.CaseID, "visibility_restored", c.Reason, c.PolicyVersion, v.Revision); e != nil {
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
