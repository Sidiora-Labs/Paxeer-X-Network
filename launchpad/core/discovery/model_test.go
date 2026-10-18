package discovery

import (
	"crypto/sha256"
	"errors"
	"reflect"
	"testing"
	"time"
)

const pool = "0x1111111111111111111111111111111111111111"

func TestFeedVisibilityOrderingAndTombstone(t *testing.T) {
	now := time.Now()
	a := SourceRef{ID: "a", EventID: "b", Kind: "pool", SourceRevision: 1, VisibilityRevision: 1, PublishedAt: now, Public: true, Authorized: true, Pools: []string{pool}}
	b := a
	b.ID = "b"
	b.EventID = "a"
	private := a
	private.Public = false
	got, e := Candidates([]SourceRef{a, b, private}, now, now, 50)
	if e != nil || len(got) != 2 || got[0].ID != "b" {
		t.Fatal(got, e)
	}
	dead := a
	dead.Tombstone = true
	dead.VisibilityRevision = 2
	if _, e = ApplySource(dead, a); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	got, e = Candidates([]SourceRef{a}, now.Add(-61*time.Second), now, 1)
	if e != nil || len(got) != 0 {
		t.Fatal(got, e)
	}
	if _, e = Candidates(nil, now, now, 51); e == nil {
		t.Fatal("unbounded page")
	}
}
func TestMembershipInvitationReplayAndConflict(t *testing.T) {
	now := time.Now()
	token := "01234567890123456789012345678901"
	i := Invitation{SquadID: "s", AccountID: "a", ExpiresAt: now.Add(time.Hour), TokenHash: sha256.Sum256([]byte(token))}
	m := Membership{SquadID: "s", AccountID: "a", Status: "invited", Revision: 1}
	i, m, e := Accept(i, m, token, 1, now)
	if e != nil || !m.CanPublish() {
		t.Fatal(m, e)
	}
	_, again, e := Accept(i, m, token, 2, now)
	if e != nil || again.Revision != 2 {
		t.Fatal(again, e)
	}
	if _, _, e = Accept(i, m, token, 1, now); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	m.Status = "removed"
	if m.CanPublish() {
		t.Fatal("removed member authorized")
	}
	s := Squad{OwnerAccountID: "old", Revision: 1}
	if _, e = s.ConfirmOwnerTransfer("old", OwnerTransfer{FromAccountID: "old", ToAccountID: "new", SquadRevision: 1}); e == nil {
		t.Fatal("unconfirmed transfer")
	}
}
func TestCalloutRevisionAndDeletion(t *testing.T) {
	c := Callout{ID: "c", AuthorAccountID: "a", PoolAddress: pool, Body: "thesis", Direction: "neutral", Disclosure: "organic", ModerationStatus: "approved", CreatedAt: time.Now(), Revision: 1}
	if e := c.ValidatePublication("a", pool); e != nil {
		t.Fatal(e)
	}
	original := c.CreatedAt
	c, e := c.Revise("a", "correction", 1)
	if e != nil || c.Revision != 2 || c.CreatedAt != original {
		t.Fatal(c, e)
	}
	if _, e = c.Revise("b", "bad", 2); e == nil {
		t.Fatal("forged author")
	}
	if _, e = c.Revise("a", "<script>", 2); e == nil {
		t.Fatal("markup accepted")
	}
	c, e = c.Remove("a", 2)
	if e != nil || c.Body != "" || !c.Deleted {
		t.Fatal(c, e)
	}
}
func TestExactAlertCrossingReplayStalenessAndRearm(t *testing.T) {
	now := time.Now()
	r := AlertRule{ID: "r", OwnerAccountID: "a", PoolAddress: pool, Metric: "price", Direction: "crosses_above", Threshold: "10", Hysteresis: "1", Cooldown: 300 * time.Second, Channels: []string{"inapp"}, Revision: 1}
	s := AlertCheckpoint{}
	for i, v := range []string{"9", "11", "12"} {
		next, c, _, e := Evaluate(r, s, Metric{Value: v, Revision: uint64(i + 1), ObservedAt: now, Quality: "complete"}, now)
		if e != nil || c != (i == 1) {
			t.Fatal(i, c, e)
		}
		s = next
	}
	if _, c, reason, e := Evaluate(r, s, Metric{Value: "11", Revision: 2, ObservedAt: now, Quality: "complete"}, now); e != nil || c || reason != "replay" {
		t.Fatal(reason, e)
	}
	if next, c, reason, e := Evaluate(r, s, Metric{Value: "11", Revision: 4, ObservedAt: now.Add(-11 * time.Second), Quality: "complete"}, now); e != nil || c || reason != "stale_or_unqualified" || next != s {
		t.Fatal(reason, e)
	}
	r.Revision = 2
	r.Threshold = "10.00000000000000000001"
	s, _, _, _ = Evaluate(r, s, Metric{Value: "10.00000000000000000000", Revision: 4, ObservedAt: now, Quality: "complete"}, now)
	_, c, _, e := Evaluate(r, s, Metric{Value: "10.00000000000000000002", Revision: 5, ObservedAt: now, Quality: "complete"}, now)
	if e != nil || !c {
		t.Fatal("exact precision", e)
	}
	r.Cooldown = 59 * time.Second
	if r.Validate() == nil {
		t.Fatal("cooldown bound")
	}
}

func TestFrozenRationalPricePrecision(t *testing.T) {
	a, e := exact("1/3")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := exact("333333333333333333333333333333/1000000000000000000000000000000")
	if a.Cmp(b) <= 0 {
		t.Fatal("rational rounded")
	}
	for _, bad := range []string{"1/0", "-1/3", "1e20", "NaN"} {
		if _, e = exact(bad); e == nil {
			t.Fatal(bad)
		}
	}
	r := AlertRule{ID: "r", OwnerAccountID: "a", PoolAddress: pool, Threshold: "1/3", Direction: "crosses_above", Revision: 1, Channels: []string{"inapp"}}
	r, e = NewAlertRule(r)
	if e != nil || r.Cooldown != 300*time.Second {
		t.Fatal(r, e)
	}
}

func TestRevisionExhaustionPreservesAllState(t *testing.T) {
	now := time.Now()
	const exhausted = ^uint64(0)
	token := "01234567890123456789012345678901"
	i := Invitation{SquadID: "s", AccountID: "a", ExpiresAt: now.Add(time.Hour), TokenHash: sha256.Sum256([]byte(token))}
	m := Membership{SquadID: "s", AccountID: "a", Status: "invited", Revision: exhausted}
	gotI, gotM, e := Accept(i, m, token, exhausted, now)
	if !errors.Is(e, ErrRevisionExhausted) || gotI != i || gotM != m {
		t.Fatal("accept mutated exhausted state", gotI, gotM, e)
	}
	c := Callout{ID: "c", AuthorAccountID: "a", Body: "original", PositionDisclosure: "user-declared", CreatedAt: now, Revision: exhausted}
	gotC, e := c.Revise("a", "replacement", exhausted)
	if !errors.Is(e, ErrRevisionExhausted) || gotC != c {
		t.Fatal("revise mutated exhausted state", gotC, e)
	}
	gotC, e = c.Remove("a", exhausted)
	if !errors.Is(e, ErrRevisionExhausted) || gotC != c {
		t.Fatal("remove mutated exhausted state", gotC, e)
	}
	s := Squad{ID: "s", OwnerAccountID: "old", Revision: exhausted}
	p, e := s.ProposeOwnerTransfer("old", "new", exhausted)
	if e != nil {
		t.Fatal(e)
	}
	gotS, e := s.ConfirmOwnerTransfer("new", p)
	if !errors.Is(e, ErrRevisionExhausted) || gotS != s {
		t.Fatal("transfer mutated exhausted state", gotS, e)
	}
}
func TestSourceReplayRejectsEquivocationByRevision(t *testing.T) {
	old := SourceRef{ID: "a", EventID: "event", Kind: "callout", CreatorPublicID: "author", Pools: []string{pool}, SourceRevision: 5, VisibilityRevision: 7, PublishedAt: time.Now(), Public: true, Authorized: true}
	for _, mutate := range []func(*SourceRef){func(s *SourceRef) { s.Public = false }, func(s *SourceRef) { s.Authorized = false }, func(s *SourceRef) { s.EventID = "different" }, func(s *SourceRef) { s.CreatorPublicID = "different" }, func(s *SourceRef) { s.Pools = []string{"0x2222222222222222222222222222222222222222"} }, func(s *SourceRef) { s.Corrected = true }, func(s *SourceRef) { s.Tombstone = true }, func(s *SourceRef) { s.Kind = "pool" }, func(s *SourceRef) { s.PublishedAt = s.PublishedAt.Add(time.Second) }} {
		next := old
		mutate(&next)
		got, e := ApplySource(old, next)
		if !errors.Is(e, ErrConflict) || !reflect.DeepEqual(got, old) {
			t.Fatal("equivocation accepted", got, e)
		}
	}
	got, e := ApplySource(old, old)
	if e != nil || !reflect.DeepEqual(got, old) {
		t.Fatal("replay rejected", e)
	}
	// Advancing content alone cannot overwrite current visibility at an equal revision.
	next := old
	next.SourceRevision++
	next.EventID = "correction"
	next.Authorized = false
	if _, e = ApplySource(old, next); !errors.Is(e, ErrConflict) {
		t.Fatal("visibility equivocation accepted", e)
	}
	next.Authorized = true
	got, e = ApplySource(old, next)
	if e != nil || got.SourceRevision != 6 {
		t.Fatal("valid content update rejected", e)
	}
	dead := old
	dead.Tombstone = true
	dead.Public = false
	dead.VisibilityRevision++
	got, e = ApplySource(old, dead)
	if e != nil {
		t.Fatal(e)
	}
	replay, e := ApplySource(got, dead)
	if e != nil || !reflect.DeepEqual(replay, dead) {
		t.Fatal("tombstone replay rejected", e)
	}
	resurrect := old
	resurrect.SourceRevision = 6
	resurrect.VisibilityRevision = dead.VisibilityRevision
	if _, e = ApplySource(dead, resurrect); !errors.Is(e, ErrConflict) {
		t.Fatal("equal visibility resurrection", e)
	}
}
func TestStaleAlertConfigurationCannotRearmAcceptedState(t *testing.T) {
	now := time.Now()
	r := AlertRule{ID: "r", OwnerAccountID: "a", PoolAddress: pool, Metric: "price", Threshold: "10", Direction: "crosses_above", Hysteresis: "0", Cooldown: 300 * time.Second, Channels: []string{"inapp"}, Revision: 5}
	accepted := AlertCheckpoint{RuleRevision: 6, MetricRevision: 20, BaselineSet: true, Armed: false, LastTrigger: now.Add(-time.Minute)}
	for _, value := range []string{"9", "11"} {
		got, c, reason, e := Evaluate(r, accepted, Metric{Value: value, Revision: 21, ObservedAt: now, Quality: "complete"}, now)
		if !errors.Is(e, ErrConflict) || c || reason != "stale_rule_revision" || got != accepted {
			t.Fatal("stale configuration changed checkpoint", got, c, reason, e)
		}
	}
	r.Disabled = true
	got, _, _, e := Evaluate(r, accepted, Metric{Value: "9", Revision: 22, ObservedAt: now, Quality: "complete"}, now)
	if !errors.Is(e, ErrConflict) || got != accepted {
		t.Fatal("stale disabled configuration accepted", e)
	}
}

func TestSquadAggregationExcludesUnknownBasisWithoutInventingProfit(t *testing.T) {
	entries := []PnLInterval{
		{AccountID: "known", KnownBasis: true, Reconciled: true, VerifiedLink: true, AttributionOptIn: true, NetPnL: "0.00000000000000000001"},
		{AccountID: "unknown", KnownBasis: false, Reconciled: true, VerifiedLink: true, AttributionOptIn: true, NetPnL: "99999999999999999999"},
		{AccountID: "unlinked", KnownBasis: true, Reconciled: true, VerifiedLink: false, AttributionOptIn: true, NetPnL: "9000"},
	}
	got, err := AggregateSquad(entries)
	if err != nil || got.NetPnL != "0.00000000000000000001" || got.EligibleMembers != 1 || got.ExcludedUnknownBasis != 1 || got.ExcludedUnqualified != 1 {
		t.Fatal(got, err)
	}
	if _, err = AggregateSquad(append(entries, entries[0])); !errors.Is(err, ErrConflict) {
		t.Fatal("duplicate membership counted twice", err)
	}
}
