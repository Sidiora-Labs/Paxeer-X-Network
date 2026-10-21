package l18

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// syntheticAuthority exercises accounting/storage invariants only. These
// attestations are not receipts, deployed payout support, or funding evidence.
type syntheticAuthority struct{}

func (syntheticAuthority) VerifyFunding(context.Context, Funding) error { return nil }
func (syntheticAuthority) VerifyAward(context.Context, Award) error     { return nil }

const actorA = "0x1111111111111111111111111111111111111111"
const actorB = "0x2222222222222222222222222222222222222222"
const actorC = "0x3333333333333333333333333333333333333333"

func storage(t *testing.T) *Ledger {
	t.Helper()
	dsn := os.Getenv("L18_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("L18_POSTGRES_DSN required: real owned PostgreSQL fixture")
	}
	ctx := context.Background()
	p, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	suffix := make([]byte, 4)
	if _, e = rand.Read(suffix); e != nil {
		t.Fatal(e)
	}
	schema := "kd_5baa0fca6c0fcb137ad0885e_" + hex.EncodeToString(suffix)
	if _, e = p.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		_, e := p.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		if e != nil {
			t.Error(e)
		}
		p.Close()
	})
	l, e := NewLedger(p, schema, "l18-fixture", syntheticAuthority{})
	if e != nil {
		t.Fatal(e)
	}
	tx, e := l.begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	proposal, e := os.ReadFile(filepath.Join("../../../../../../launchpad/spec/production-system/handoffs/L18/phase2/migration-proposal.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, SchemaDDL+"\n"+string(proposal)); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	return l
}
func funding() Funding {
	return Funding{"campaign", "v1", "2026-10-10", USDL, actorA, "synthetic-fixture-authority", "0x" + strings.Repeat("a", 64), "1000000", "1000000", "1000000"}
}
func award(id, beneficiary string) Award {
	return Award{id, "campaign", "v1", "2026-10-10", USDL, beneficiary, "cashback", "1000000", "125:0x" + strings.Repeat("b", 64) + ":0", ""}
}
func query(t *testing.T, l *Ledger, sql string, args ...any) string {
	t.Helper()
	ctx := context.Background()
	tx, e := l.begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	var s string
	if e = tx.QueryRow(ctx, sql, args...).Scan(&s); e != nil {
		t.Fatal(e)
	}
	return s
}
func TestPostgresReservationReplayIsolationAndHold(t *testing.T) {
	l := storage(t)
	ctx := context.Background()
	f := funding()
	if e := l.Fund(ctx, f); e != nil {
		t.Fatal(e)
	}
	if e := l.Fund(ctx, f); e != nil {
		t.Fatal(e)
	}
	changed := f
	changed.FundedRaw = "2"
	if e := l.Fund(ctx, changed); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	a := award("award-1", actorB)
	out, e := l.Accrue(ctx, a)
	if e != nil || out.PayableRaw != "1000000" || out.SettlementAvailable {
		t.Fatalf("%+v %v", out, e)
	}
	if _, e = l.Accrue(ctx, a); e != nil {
		t.Fatal(e)
	}
	a.EligibleRaw = "1"
	if _, e = l.Accrue(ctx, a); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	other := award("award-2", actorC)
	out, e = l.Accrue(ctx, other)
	if e != nil || out.PayableRaw != "0" || out.Status != "unfunded" {
		t.Fatalf("%+v %v", out, e)
	}
	other = award("award-3", actorC)
	other.Asset = actorC
	other.Provenance = "125:0x" + strings.Repeat("c", 64) + ":0"
	out, e = l.Accrue(ctx, other)
	if e != nil || out.PayableRaw != "0" {
		t.Fatalf("asset contamination: %+v %v", out, e)
	}
	if got := query(t, l, "SELECT count(*)::text FROM l18_entries"); got != "1" {
		t.Fatal(got)
	}
	if got := query(t, l, "SELECT reserved::text FROM l18_funding"); got != "1000000" {
		t.Fatal(got)
	}
	if e = l.Hold(ctx, "review-1", "award-1", "canonical fill under fraud review"); e != nil {
		t.Fatal(e)
	}
	out, e = l.Liability(ctx, "award-1")
	if e != nil || out.Status != "pending_review" || out.PayableRaw != "1000000" || out.AppealStatus != "open" {
		t.Fatalf("hold %+v %v", out, e)
	}
	if e = l.AttemptRewardPayout(ctx, "award-1"); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	// Payload evidence comes from the actual transactionally persisted outbox.
	var payload string
	payload = query(t, l, "SELECT payload::text FROM l18_outbox WHERE identity='award:award-1'")
	var v Liability
	if json.Unmarshal([]byte(payload), &v) != nil || v.PayableRaw != "1000000" {
		t.Fatal(payload)
	}
	t.Logf("actual PostgreSQL award/outbox payload: %s", payload)
}
func TestPostgresConcurrentBudgetAndRollback(t *testing.T) {
	l := storage(t)
	ctx := context.Background()
	if e := l.Fund(ctx, funding()); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a := award(fmt.Sprintf("award-%d", i), fmt.Sprintf("0x%040x", i+100))
			a.EligibleRaw = "100000"
			a.Provenance = fmt.Sprintf("125:0x%064x:0", i+1)
			_, e := l.Accrue(ctx, a)
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if got := query(t, l, "SELECT SUM(payable)::text FROM l18_awards"); got != "1000000" {
		t.Fatal(got)
	}
	// A forced real DB outbox uniqueness failure must roll back inbox and award.
	tx, e := l.begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, e = tx.Exec(ctx, `INSERT INTO l18_outbox(identity,topic,payload) VALUES('award:rollback','fixture','{}')`)
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = l.Accrue(ctx, award("rollback", actorB)); e == nil {
		t.Fatal("missing outbox failure")
	}
	if got := query(t, l, "SELECT count(*)::text FROM l18_awards WHERE identity='rollback'"); got != "0" {
		t.Fatal(got)
	}
	if got := query(t, l, "SELECT count(*)::text FROM l18_inbox WHERE identity='award:rollback'"); got != "0" {
		t.Fatal(got)
	}
}
func TestPostgresConsentFirstBindingAndExpiry(t *testing.T) {
	l := storage(t)
	ctx := context.Background()
	clock := time.Unix(1000000, 0)
	a, e := NewAttribution(l, []byte(strings.Repeat("x", 32)), func() time.Time { return clock }, "campaign", "policy1")
	if e != nil {
		t.Fatal(e)
	}
	first, e := a.IssueClick(actorA, "nonce-a")
	if e != nil {
		t.Fatal(e)
	}
	clock = clock.Add(time.Hour)
	second, _ := a.IssueClick(actorC, "nonce-b")
	if _, e = a.Bind(ctx, "no-consent", actorB, false, []string{first}); !errors.Is(e, ErrUnauthorized) {
		t.Fatal(e)
	}
	if _, e = a.Bind(ctx, "self", actorA, true, []string{first}); !errors.Is(e, ErrUnauthorized) {
		t.Fatal(e)
	}
	if _, e = a.Bind(ctx, "forged", actorB, true, []string{first + "x"}); e == nil {
		t.Fatal("forged accepted")
	}
	out, e := a.Bind(ctx, "first", actorB, true, []string{second, first})
	if e != nil || out.Referrer != actorA {
		t.Fatalf("%+v %v", out, e)
	}
	for i := 0; i < 10; i++ {
		out, e = a.Bind(ctx, fmt.Sprintf("repeat-%d", i), actorB, true, []string{second})
		if e != nil || out.Referrer != actorA {
			t.Fatalf("%+v %v", out, e)
		}
	}
	clock = clock.Add(8 * 24 * time.Hour)
	out, e = a.Bind(ctx, "expired", actorC, true, []string{first})
	if e != nil || out.Attribution != "organic" {
		t.Fatalf("%+v %v", out, e)
	}
	if got := query(t, l, "SELECT count(*)::text FROM l18_entries"); got != "0" {
		t.Fatal("binding minted liability", got)
	}
	id, _ := ReferralAwardIdentity("campaign", "policy1", actorB)
	same, _ := ReferralAwardIdentity("campaign", "policy1", strings.ToUpper(actorB))
	if id != same {
		t.Fatal("noncanonical conversion")
	}
}
func TestPostgresMissingAuthorityNeverPromisesReward(t *testing.T) {
	l := storage(t)
	l.authority = nil
	if e := l.Fund(context.Background(), funding()); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	if _, e := l.Accrue(context.Background(), award("a", actorA)); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
}

func TestPostgresReferralConversionHasCanonicalIdentity(t *testing.T) {
	l := storage(t)
	ctx := context.Background()
	a := award("unused", actorA)
	a.Kind = "referral"
	a.Referred = actorB
	a.Identity, _ = ReferralAwardIdentity(a.Program, a.Version, a.Referred)
	out, e := l.Accrue(ctx, a)
	if e != nil || out.PayableRaw != "0" || out.Status != "unfunded" {
		t.Fatalf("unfunded conversion %+v %v", out, e)
	}
	for i := 0; i < 10; i++ {
		if _, e = l.Accrue(ctx, a); e != nil {
			t.Fatal(e)
		}
	}
	secondFill := a
	secondFill.Provenance = "125:0x" + strings.Repeat("c", 64) + ":1"
	if _, e = l.Accrue(ctx, secondFill); !errors.Is(e, ErrConflict) {
		t.Fatal("second fill created another referral conversion", e)
	}
	self := a
	self.Referred = actorA
	self.Identity, _ = ReferralAwardIdentity(self.Program, self.Version, self.Referred)
	if _, e = l.Accrue(ctx, self); !errors.Is(e, ErrUnauthorized) {
		t.Fatal(e)
	}
	forged := a
	forged.Identity = "client-picked"
	if _, e = l.Accrue(ctx, forged); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	if got := query(t, l, "SELECT count(*)::text FROM l18_awards"); got != "1" {
		t.Fatal(got)
	}
	if got := query(t, l, "SELECT count(*)::text FROM l18_entries"); got != "0" {
		t.Fatal(got)
	}
}
