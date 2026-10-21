package safety

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Decision string

const (
	Allow       Decision = "allow"
	Deny        Decision = "deny"
	Unavailable Decision = "unavailable"
)

type TargetResolver interface {
	Resolve(context.Context, string, string) (poolID, affectedWallet string, err error)
}
type Store struct {
	Targets TargetResolver
	DB      *pgxpool.Pool
	Clock   func() time.Time
}

func (s *Store) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}
func (s *Store) available() bool { return s != nil && s.DB != nil }

// Check is uncached: every surface observes authoritative bans and exact expiry.
func (s *Store) Check(ctx context.Context, wallet, scope, resource string) Decision {
	if !s.available() || !walletRE.MatchString(wallet) || !validScope(scope, resource) {
		return Unavailable
	}
	var banned bool
	e := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM safety_bans WHERE wallet=$1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>$4) AND (scope='global' OR (scope=$2 AND resource=$3)))`, wallet, scope, resource, s.now()).Scan(&banned)
	if e != nil {
		return Unavailable
	}
	if banned {
		return Deny
	}
	return Allow
}
func validScope(scope, resource string) bool {
	return (scope == "global" && resource == "") || ((scope == "pool" || scope == "dm" || scope == "voice") && Bounded(resource, 256))
}

type Report struct{ TargetType, TargetID, PoolID, AffectedWallet, Category, Description string }
type Case struct {
	ID         string
	TargetType string
	TargetID   string
	PoolID     string
	State      string
	Revision   int64
}

func (s *Store) Report(ctx context.Context, r Report) (Case, error) {
	if !s.available() {
		return Case{}, ErrUnavailable
	}
	p, e := Actor(ctx, s.now())
	if e != nil {
		return Case{}, e
	}
	if !contains([]string{"message", "comment", "stream", "profile", "token", "voice_participant"}, r.TargetType) || !Bounded(r.TargetID, 160) || !Bounded(r.Category, 256) || len(r.Description) > 4000 || len(r.PoolID) > 256 || r.AffectedWallet != "" && !walletRE.MatchString(r.AffectedWallet) {
		return Case{}, ErrInvalid
	}
	if s.Targets == nil {
		return Case{}, ErrUnavailable
	}
	pool, affected, err := s.Targets.Resolve(ctx, r.TargetType, r.TargetID)
	if err != nil {
		return Case{}, ErrUnavailable
	}
	if len(pool) > 256 || affected != "" && !walletRE.MatchString(affected) {
		return Case{}, ErrInvalid
	}
	r.PoolID = pool
	r.AffectedWallet = affected
	var c Case
	e = s.DB.QueryRow(ctx, `INSERT INTO safety_cases(id,target_type,target_id,pool_id,affected_wallet,reporter,category,description) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(reporter,target_type,target_id) DO UPDATE SET target_id=EXCLUDED.target_id RETURNING id,target_type,target_id,pool_id,state,revision`, uuid.NewString(), r.TargetType, r.TargetID, r.PoolID, r.AffectedWallet, p.Wallet, r.Category, r.Description).Scan(&c.ID, &c.TargetType, &c.TargetID, &c.PoolID, &c.State, &c.Revision)
	return c, e
}
func audit(ctx context.Context, tx pgx.Tx, p Principal, caseID *string, kind, reason, policy string, rev int64) (string, error) {
	id := uuid.NewString()
	b, _ := json.Marshal([]any{id, caseID, p.Wallet, p.SessionID, p.Role, p.Pools, kind, reason, policy, rev})
	h := sha256.Sum256(b)
	scope, _ := json.Marshal(p.Pools)
	_, e := tx.Exec(ctx, `INSERT INTO safety_actions(id,case_id,actor,session_id,actor_role,actor_scope,kind,reason,policy_version,revision,evidence_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id, caseID, p.Wallet, p.SessionID, p.Role, scope, kind, reason, policy, rev, "0x"+hex.EncodeToString(h[:]))
	return id, e
}
func outbox(ctx context.Context, tx pgx.Tx, kind, id string, rev int64) error {
	b, _ := json.Marshal(map[string]any{"entityId": id, "revision": rev, "kind": kind})
	_, e := tx.Exec(ctx, `INSERT INTO safety_outbox(id,kind,entity_id,revision,payload) VALUES($1,$2,$3,$4,$5)`, uuid.NewString(), kind, id, rev, b)
	return e
}
func (s *Store) denial(ctx context.Context, p Principal, kind string) error {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return ErrUnavailable
	}
	defer tx.Rollback(ctx)
	if _, e = audit(ctx, tx, p, nil, "denied:"+kind, "insufficient authority", "authority-v1", 0); e != nil {
		return ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return ErrUnavailable
	}
	return ErrForbidden
}
func (s *Store) Transition(ctx context.Context, id string, expected int64, next, reason, policy string) (int64, error) {
	if !s.available() {
		return 0, ErrUnavailable
	}
	p, e := Actor(ctx, s.now())
	if e != nil {
		return 0, e
	}
	if _, e = uuid.Parse(id); e != nil || expected < 1 || !Bounded(reason, 1000) || !Bounded(policy, 160) {
		return 0, ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback(ctx)
	var pool, state string
	var rev int64
	e = tx.QueryRow(ctx, `SELECT pool_id,state,revision FROM safety_cases WHERE id=$1 FOR UPDATE`, id).Scan(&pool, &state, &rev)
	if e != nil {
		return 0, e
	}
	if !Authorized(p, pool, true, s.now()) {
		tx.Rollback(ctx)
		return 0, s.denial(ctx, p, "case_transition")
	}
	valid := (state == "open" && (next == "triaged" || next == "actioned" || next == "dismissed")) || (state == "triaged" && (next == "actioned" || next == "dismissed"))
	if rev != expected || !valid {
		return 0, ErrConflict
	}
	rev++
	if _, e = tx.Exec(ctx, `UPDATE safety_cases SET state=$2,revision=$3 WHERE id=$1`, id, next, rev); e != nil {
		return 0, e
	}
	if _, e = audit(ctx, tx, p, &id, next, reason, policy, rev); e != nil {
		return 0, e
	}
	if e = outbox(ctx, tx, "case_changed", id, rev); e != nil {
		return 0, e
	}
	return rev, tx.Commit(ctx)
}

type BanCommand struct {
	Wallet, Scope, Resource, Reason, PolicyVersion string
	ExpiresAt                                      *time.Time
}

func (s *Store) Ban(ctx context.Context, c BanCommand) (string, error) {
	if !s.available() {
		return "", ErrUnavailable
	}
	p, e := Actor(ctx, s.now())
	if e != nil {
		return "", e
	}
	if !walletRE.MatchString(c.Wallet) || !validScope(c.Scope, c.Resource) || !Bounded(c.Reason, 1000) || !Bounded(c.PolicyVersion, 160) || c.ExpiresAt != nil && !c.ExpiresAt.After(s.now()) {
		return "", ErrInvalid
	}
	pool := c.Resource
	if c.Scope != "pool" {
		pool = ""
	}
	if c.Scope == "voice" {
		if e = s.DB.QueryRow(ctx, `SELECT pool_id FROM voice_rooms WHERE id=$1`, c.Resource).Scan(&pool); e != nil {
			return "", ErrUnavailable
		}
	}
	if !Authorized(p, pool, true, s.now()) {
		return "", s.denial(ctx, p, "ban")
	}
	id := uuid.NewString()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	_, e = tx.Exec(ctx, `INSERT INTO safety_bans(id,wallet,scope,resource,actor,expires_at)VALUES($1,$2,$3,$4,$5,$6)`, id, c.Wallet, c.Scope, c.Resource, p.Wallet, c.ExpiresAt)
	if e != nil {
		return "", e
	}
	if _, e = audit(ctx, tx, p, nil, "ban:"+id, c.Reason, c.PolicyVersion, 1); e != nil {
		return "", e
	}
	if e = outbox(ctx, tx, "policy_changed", id, 1); e != nil {
		return "", e
	}
	return id, tx.Commit(ctx)
}
func (s *Store) Revoke(ctx context.Context, id string, expected int64, reason string) error {
	if !s.available() {
		return ErrUnavailable
	}
	p, e := Actor(ctx, s.now())
	if e != nil {
		return e
	}
	if !Bounded(reason, 1000) || expected < 1 {
		return ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var scope, resource string
	var rev int64
	if e = tx.QueryRow(ctx, `SELECT scope,resource,revision FROM safety_bans WHERE id=$1 FOR UPDATE`, id).Scan(&scope, &resource, &rev); e != nil {
		return e
	}
	pool := resource
	if scope != "pool" {
		pool = ""
	}
	if scope == "voice" {
		if e = tx.QueryRow(ctx, `SELECT pool_id FROM voice_rooms WHERE id=$1`, resource).Scan(&pool); e != nil {
			return ErrUnavailable
		}
	}
	if !Authorized(p, pool, true, s.now()) {
		tx.Rollback(ctx)
		return s.denial(ctx, p, "revoke")
	}
	if rev != expected {
		return ErrConflict
	}
	tag, e := tx.Exec(ctx, `UPDATE safety_bans SET revoked_at=$2,revision=revision+1 WHERE id=$1 AND revoked_at IS NULL`, id, s.now())
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	if _, e = audit(ctx, tx, p, nil, "revoke:"+id, reason, "policy-v1", rev+1); e != nil {
		return e
	}
	if e = outbox(ctx, tx, "policy_changed", id, rev+1); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// CaseEvidence is private and never part of a public target response.
func (s *Store) CaseEvidence(ctx context.Context, id string) (string, error) {
	if !s.available() {
		return "", ErrUnavailable
	}
	p, e := Actor(ctx, s.now())
	if e != nil {
		return "", e
	}
	if !contains(p.Cases, id) {
		return "", s.denial(ctx, p, "evidence")
	}
	var description, pool string
	e = s.DB.QueryRow(ctx, `SELECT description,pool_id FROM safety_cases WHERE id=$1`, id).Scan(&description, &pool)
	if e != nil {
		return "", e
	}
	if !Authorized(p, pool, false, s.now()) {
		return "", s.denial(ctx, p, "evidence")
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	if _, e = audit(ctx, tx, p, &id, "evidence_read", "case scoped access", "authority-v1", 0); e != nil {
		return "", e
	}
	if e = tx.Commit(ctx); e != nil {
		return "", e
	}
	return description, nil
}
func (s *Store) Appeal(ctx context.Context, actionID, reason string) (string, error) {
	if !s.available() {
		return "", ErrUnavailable
	}
	p, e := Actor(ctx, s.now())
	if e != nil {
		return "", e
	}
	if _, e = uuid.Parse(actionID); e != nil || !Bounded(reason, 1000) {
		return "", ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	var affected, caseID, state string
	var rev int64
	e = tx.QueryRow(ctx, `SELECT c.affected_wallet,c.id,c.state,c.revision FROM safety_actions a JOIN safety_cases c ON c.id=a.case_id WHERE a.id=$1 AND a.kind='actioned' FOR UPDATE OF c`, actionID).Scan(&affected, &caseID, &state, &rev)
	if e != nil {
		return "", e
	}
	if affected != p.Wallet {
		return "", ErrForbidden
	}
	var id string
	e = tx.QueryRow(ctx, `SELECT id FROM safety_appeals WHERE action_id=$1`, actionID).Scan(&id)
	if e == nil {
		return id, tx.Commit(ctx)
	}
	if e != pgx.ErrNoRows {
		return "", e
	}
	if state != "actioned" {
		return "", ErrConflict
	}
	id = uuid.NewString()
	if _, e = tx.Exec(ctx, `INSERT INTO safety_appeals(id,action_id,appellant,reason)VALUES($1,$2,$3,$4)`, id, actionID, p.Wallet, reason); e != nil {
		return "", e
	}
	if _, e = tx.Exec(ctx, `UPDATE safety_cases SET state='appealed',revision=revision+1 WHERE id=$1`, caseID); e != nil {
		return "", e
	}
	if _, e = audit(ctx, tx, p, &caseID, "appeal_submitted", reason, "policy-v1", rev+1); e != nil {
		return "", e
	}
	if e = outbox(ctx, tx, "case_changed", caseID, rev+1); e != nil {
		return "", e
	}
	if e = outbox(ctx, tx, "appeal_changed", id, 1); e != nil {
		return "", e
	}
	return id, tx.Commit(ctx)
}

// DecideAppeal records a separate reviewer decision. Content restoration and
// policy reversal require explicit commands; an appeal never silently restores.
func (s *Store) DecideAppeal(ctx context.Context, id string, expected int64, outcome, reason string) error {
	if !s.available() {
		return ErrUnavailable
	}
	p, e := Actor(ctx, s.now())
	if e != nil {
		return e
	}
	if expected < 1 || !contains([]string{"upheld", "reversed"}, outcome) || !Bounded(reason, 1000) {
		return ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var issuer, caseID, pool, state, caseState string
	var rev, caseRev int64
	e = tx.QueryRow(ctx, `SELECT a.actor,c.id,c.pool_id,ap.state,ap.revision,c.state,c.revision FROM safety_appeals ap JOIN safety_actions a ON a.id=ap.action_id JOIN safety_cases c ON c.id=a.case_id WHERE ap.id=$1 FOR UPDATE OF c,ap`, id).Scan(&issuer, &caseID, &pool, &state, &rev, &caseState, &caseRev)
	if e != nil {
		return e
	}
	if issuer == p.Wallet || !Authorized(p, pool, true, s.now()) {
		tx.Rollback(ctx)
		return s.denial(ctx, p, "appeal_decision")
	}
	if rev != expected || (state != "submitted" && state != "reviewing") || caseState != "appealed" {
		return ErrConflict
	}
	_, e = tx.Exec(ctx, `UPDATE safety_appeals SET state=$2,revision=revision+1,reviewer=$3,decided_at=$4 WHERE id=$1`, id, outcome, p.Wallet, s.now())
	if e != nil {
		return e
	}
	if _, e = audit(ctx, tx, p, &caseID, "appeal_"+outcome, reason, "policy-v1", rev+1); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE safety_cases SET state='final',revision=revision+1 WHERE id=$1`, caseID); e != nil {
		return e
	}
	if e = outbox(ctx, tx, "case_changed", caseID, caseRev+1); e != nil {
		return e
	}
	if e = outbox(ctx, tx, "appeal_changed", id, rev+1); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// HTTPStatus lets adapters preserve retryable dependency and revision semantics.
func HTTPStatus(e error) int {
	switch {
	case errors.Is(e, ErrForbidden):
		return 403
	case errors.Is(e, ErrConflict):
		return 409
	case errors.Is(e, ErrInvalid):
		return 400
	case errors.Is(e, pgx.ErrNoRows):
		return 404
	default:
		return 503
	}
}
