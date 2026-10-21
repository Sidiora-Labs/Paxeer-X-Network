package safety

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"sync"
	"testing"
	"time"
)

const walletA = "0x1111111111111111111111111111111111111111"
const walletB = "0x2222222222222222222222222222222222222222"

type fixtureTargets struct{}

func (fixtureTargets) Resolve(context.Context, string, string) (string, string, error) {
	return "pool-a", walletB, nil
}
func fixture(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	dsn := os.Getenv("L38_POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"
	}
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	schema := "kd_9677e1202ca2546e7bac624f_" + uuid.New().String()[:8]
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+identifier); e != nil {
		admin.Close()
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		db.Close()
		if _, e := admin.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE"); e != nil {
			t.Error(e)
		}
		admin.Close()
	})
	ddl, e := os.ReadFile("../../../../../launchpad/spec/production-system/handoffs/L38/schema-proposal.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, string(ddl)); e != nil {
		t.Fatal(e)
	}
	return &Store{DB: db, Clock: func() time.Time { return time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC) }, Targets: fixtureTargets{}}
}
func principal(s *Store, wallet, role string) context.Context {
	return WithPrincipal(context.Background(), Principal{Wallet: wallet, SessionID: "private-fixture", Role: role, Pools: []string{"pool-a"}, Expires: s.now().Add(time.Hour), StepUp: s.now()})
}
func TestPostgresCasesRaceAndAppendOnly(t *testing.T) {
	s := fixture(t)
	ctx := principal(s, walletA, "room_moderator")
	r := Report{TargetType: "message", TargetID: "private-message", Category: "abuse", Description: "private DM evidence"}
	c, e := s.Report(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	dup, e := s.Report(ctx, r)
	if e != nil || dup.ID != c.ID || dup.Revision != 1 {
		t.Fatalf("duplicate %+v %v", dup, e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, state := range []string{"actioned", "dismissed"} {
		wg.Add(1)
		go func(next string) {
			defer wg.Done()
			_, e := s.Transition(ctx, c.ID, 1, next, "reviewed", "policy-1")
			errs <- e
		}(state)
	}
	wg.Wait()
	close(errs)
	ok, conflicts := 0, 0
	for e := range errs {
		if e == nil {
			ok++
		} else if errors.Is(e, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatalf("race ok=%d conflict=%d", ok, conflicts)
	}
	var actions, events int
	s.DB.QueryRow(ctx, `SELECT count(*) FROM safety_actions WHERE case_id=$1`, c.ID).Scan(&actions)
	s.DB.QueryRow(ctx, `SELECT count(*) FROM safety_outbox WHERE entity_id=$1`, c.ID).Scan(&events)
	if actions != 1 || events != 1 {
		t.Fatalf("actions=%d outbox=%d", actions, events)
	}
	if _, e = s.DB.Exec(ctx, `UPDATE safety_actions SET reason='rewrite'`); e == nil {
		t.Fatal("audit update allowed")
	}
	if _, e = s.DB.Exec(ctx, `DELETE FROM safety_actions`); e == nil {
		t.Fatal("audit deletion allowed")
	}
	if _, e = s.CaseEvidence(ctx, c.ID); !errors.Is(e, ErrForbidden) {
		t.Fatal("unscoped evidence exposed", e)
	}
	p, _ := Actor(ctx, s.now())
	p.Cases = []string{c.ID}
	evidence, e := s.CaseEvidence(WithPrincipal(ctx, p), c.ID)
	if e != nil || evidence != r.Description {
		t.Fatal(e)
	}
}
func TestPostgresBanAuthorityExpiryAndUnavailable(t *testing.T) {
	s := fixture(t)
	reviewer := principal(s, walletA, "reviewer")
	cmd := BanCommand{Wallet: walletB, Scope: "global", Reason: "abuse", PolicyVersion: "v1"}
	if _, e := s.Ban(reviewer, cmd); !errors.Is(e, ErrForbidden) {
		t.Fatal("reviewer global ban", e)
	}
	var denials int
	if e := s.DB.QueryRow(reviewer, `SELECT count(*) FROM safety_actions WHERE actor=$1 AND kind='denied:ban'`, walletA).Scan(&denials); e != nil || denials != 1 {
		t.Fatal("denial audit", e, denials)
	}
	room := principal(s, walletA, "room_moderator")
	cmd.Scope = "pool"
	cmd.Resource = "foreign"
	if _, e := s.Ban(room, cmd); !errors.Is(e, ErrForbidden) {
		t.Fatal("cross-pool ban", e)
	}
	cmd.Resource = "pool-a"
	expiry := s.now().Add(time.Second)
	cmd.ExpiresAt = &expiry
	id, e := s.Ban(room, cmd)
	if e != nil {
		t.Fatal(e)
	}
	if s.Check(room, walletB, "pool", "pool-a") != Deny {
		t.Fatal("ban not active")
	}
	s.Clock = func() time.Time { return expiry }
	if s.Check(room, walletB, "pool", "pool-a") != Allow {
		t.Fatal("expiry boundary denied")
	}
	cmd.ExpiresAt = nil
	id, e = s.Ban(room, cmd)
	if e != nil {
		t.Fatal(e)
	}
	admin := principal(s, walletA, "global_administrator")
	global := cmd
	global.Scope = "global"
	global.Resource = ""
	if _, e = s.Ban(admin, global); e != nil {
		t.Fatal(e)
	}
	if e = s.Revoke(room, id, 1, "appeal reversal"); e != nil {
		t.Fatal(e)
	}
	if s.Check(room, walletB, "pool", "pool-a") != Deny {
		t.Fatal("global ban bypassed by room reversal")
	}
	canceled, cancel := context.WithCancel(room)
	cancel()
	for _, scope := range []string{"pool", "dm", "voice"} {
		if s.Check(canceled, walletB, scope, "pool-a") != Unavailable {
			t.Fatal("query failure allowed", scope)
		}
	}
	var writes int
	s.DB.QueryRow(room, `SELECT count(*) FROM safety_outbox`).Scan(&writes)
	if writes != 4 {
		t.Fatal("unexpected outbox writes", writes)
	}
}
func TestPostgresAppealIdentityAndReviewerConflict(t *testing.T) {
	s := fixture(t)
	moderator := principal(s, walletA, "room_moderator")
	c, e := s.Report(moderator, Report{TargetType: "message", TargetID: "appeal-message", Category: "abuse"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Transition(moderator, c.ID, 1, "actioned", "policy decision", "v1"); e != nil {
		t.Fatal(e)
	}
	var action string
	s.DB.QueryRow(moderator, `SELECT id FROM safety_actions WHERE case_id=$1`, c.ID).Scan(&action)
	if _, e = s.Appeal(moderator, action, "wrong account"); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	affected := principal(s, walletB, "reviewer")
	id, e := s.Appeal(affected, action, "please review")
	if e != nil {
		t.Fatal(e)
	}
	dup, e := s.Appeal(affected, action, "duplicate")
	if e != nil || dup != id {
		t.Fatal(e)
	}
	if e = s.DecideAppeal(moderator, id, 1, "reversed", "conflict"); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	independent := principal(s, "0x3333333333333333333333333333333333333333", "room_moderator")
	if e = s.DecideAppeal(independent, id, 1, "reversed", "independent review"); e != nil {
		t.Fatal(e)
	}
	if e = s.DecideAppeal(independent, id, 1, "upheld", "stale"); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
}
func TestRealEd25519SessionSignature(t *testing.T) {
	now := time.Now().UTC()
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	p := Principal{Wallet: walletA, SessionID: "signed", Role: "reviewer", Expires: now.Add(time.Minute)}
	raw, _ := json.Marshal(p)
	payload := base64.RawURLEncoding.EncodeToString(raw)
	sig := base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, raw))
	if _, e = VerifySession(payload, sig, pub, now); e != nil {
		t.Fatal(e)
	}
	if _, e = VerifySession(payload+"A", sig, pub, now); e == nil {
		t.Fatal("tampered session accepted")
	}
	if _, e = VerifySession(payload, sig, pub, p.Expires); e == nil {
		t.Fatal("expired session accepted")
	}
}

func TestPostgresVisibilityRejectsReplayAndRequiresRevisionedRestore(t *testing.T) {
	s := fixture(t)
	ctx := principal(s, walletA, "room_moderator")
	c, e := s.Report(ctx, Report{TargetType: "message", TargetID: "tombstone-message", Category: "abuse", Description: "never public"})
	if e != nil {
		t.Fatal(e)
	}
	removal, e := s.SetVisibility(ctx, c.ID, 0, "removed", "policy removal")
	if e != nil {
		t.Fatal(e)
	}
	persisted, e := s.Visibility(ctx, "message", "tombstone-message")
	if e != nil || persisted.Status != "removed" {
		t.Fatal(persisted, e)
	}
	stale := removal
	stale.Revision = 0
	stale.Status = "restored"
	if ApplyVisibility(persisted, stale).Status != "removed" {
		t.Fatal("stale replay resurrected")
	}
	if _, e = s.SetVisibility(ctx, c.ID, 0, "restored", "stale restore"); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	restored, e := s.SetVisibility(ctx, c.ID, 1, "restored", "explicit restoration")
	if e != nil || restored.Revision != 2 {
		t.Fatal(restored, e)
	}
	if ApplyVisibility(restored, removal).Status != "restored" {
		t.Fatal("stale remove overwrote restore")
	}
}

func TestPostgresOutboxFailureRollsBackCaseAndAction(t *testing.T) {
	s := fixture(t)
	ctx := principal(s, walletA, "room_moderator")
	c, e := s.Report(ctx, Report{TargetType: "message", TargetID: "rollback-message", Category: "spam"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec(ctx, `ALTER TABLE safety_outbox ADD CONSTRAINT fixture_reject_outbox CHECK(false) NOT VALID`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Transition(ctx, c.ID, 1, "actioned", "reviewed", "v1"); e == nil {
		t.Fatal("failed outbox acknowledged")
	}
	var state string
	var rev int64
	if e = s.DB.QueryRow(ctx, `SELECT state,revision FROM safety_cases WHERE id=$1`, c.ID).Scan(&state, &rev); e != nil || state != "open" || rev != 1 {
		t.Fatal(state, rev, e)
	}
	var count int
	if e = s.DB.QueryRow(ctx, `SELECT count(*) FROM safety_actions WHERE case_id=$1`, c.ID).Scan(&count); e != nil || count != 0 {
		t.Fatal("partial audit commit", count, e)
	}
}

func TestPostgresAppealLifecyclePrivateOutcomeAndRollback(t *testing.T) {
	s := fixture(t)
	issuer := principal(s, walletA, "room_moderator")
	affected := principal(s, walletB, "reviewer")
	independent := principal(s, "0x3333333333333333333333333333333333333333", "room_moderator")
	c, e := s.Report(issuer, Report{TargetType: "message", TargetID: "lifecycle-message", Category: "abuse"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Transition(issuer, c.ID, 1, "actioned", "decision", "v1"); e != nil {
		t.Fatal(e)
	}
	var action string
	if e = s.DB.QueryRow(issuer, `SELECT id FROM safety_actions WHERE case_id=$1 AND kind='actioned'`, c.ID).Scan(&action); e != nil {
		t.Fatal(e)
	}
	id, e := s.Appeal(affected, action, "review request")
	if e != nil {
		t.Fatal(e)
	}
	var state string
	var rev int64
	if e = s.DB.QueryRow(issuer, `SELECT state,revision FROM safety_cases WHERE id=$1`, c.ID).Scan(&state, &rev); e != nil || state != "appealed" || rev != 3 {
		t.Fatal(state, rev, e)
	}
	if _, e = s.PrivateAppeal(issuer, id); !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal("private appeal leaked", e)
	}
	if e = s.ReviewAppeal(issuer, id, 1, "self review"); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	if e = s.ReviewAppeal(independent, id, 1, "assigned independent review"); e != nil {
		t.Fatal(e)
	}
	if e = s.DecideAppeal(independent, id, 1, "upheld", "stale revision"); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec(issuer, `ALTER TABLE safety_outbox ADD CONSTRAINT reject_appeal_outbox CHECK(false) NOT VALID`); e != nil {
		t.Fatal(e)
	}
	if e = s.DecideAppeal(independent, id, 2, "upheld", "independent outcome"); e == nil {
		t.Fatal("failed outbox acknowledged")
	}
	outcome, e := s.PrivateAppeal(affected, id)
	if e != nil || outcome.State != "reviewing" || outcome.Revision != 2 || outcome.DecidedAt != nil {
		t.Fatal(outcome, e)
	}
	if e = s.DB.QueryRow(issuer, `SELECT state,revision FROM safety_cases WHERE id=$1`, c.ID).Scan(&state, &rev); e != nil || state != "appealed" || rev != 3 {
		t.Fatal(state, rev, e)
	}
	if _, e = s.DB.Exec(issuer, `ALTER TABLE safety_outbox DROP CONSTRAINT reject_appeal_outbox`); e != nil {
		t.Fatal(e)
	}
	if e = s.DecideAppeal(independent, id, 2, "upheld", "independent outcome"); e != nil {
		t.Fatal(e)
	}
	outcome, e = s.PrivateAppeal(affected, id)
	if e != nil || outcome.State != "upheld" || outcome.Revision != 3 || outcome.Reason != "independent outcome" || outcome.DecidedAt == nil || !outcome.DecidedAt.Equal(s.now()) {
		t.Fatal(outcome, e)
	}
	if e = s.DB.QueryRow(issuer, `SELECT state,revision FROM safety_cases WHERE id=$1`, c.ID).Scan(&state, &rev); e != nil || state != "final" || rev != 4 {
		t.Fatal(state, rev, e)
	}
}
