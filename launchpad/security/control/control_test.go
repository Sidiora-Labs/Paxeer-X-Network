package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const fixtureDSN = "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"
const custody = "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266"
const beneficiary = "0x70997970c51812dc3a010c7d01b50e0d17dc79c8"
const fixtureManifest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

var pax = Asset{Chain: 125, Address: NativePAX, Decimals: 18}
var usdl = Asset{Chain: 125, Address: USDLAddress, Decimals: 6}

type identities struct {
	sync.Mutex
	users map[string]Principal
	calls chan struct{}
}

func (i *identities) Resolve(ctx context.Context, c string) (Principal, error) {
	i.Lock()
	defer i.Unlock()
	if i.calls != nil {
		select {
		case i.calls <- struct{}{}:
		default:
		}
	}
	p, ok := i.users[c]
	if !ok {
		return Principal{}, Denied
	}
	p.Scopes = append([]string(nil), p.Scopes...)
	return p, nil
}
func (i *identities) Active(ctx context.Context, p Principal) bool {
	i.Lock()
	defer i.Unlock()
	for _, v := range i.users {
		if reflect.DeepEqual(v, p) {
			return true
		}
	}
	return false
}
func (i *identities) remove(c string) { i.Lock(); defer i.Unlock(); delete(i.users, c) }
func fixture(t *testing.T, verifier EvidenceVerifier) (*Store, *identities) {
	t.Helper()
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, fixtureDSN)
	if e != nil {
		t.Fatal(e)
	}
	if e = pool.Ping(ctx); e != nil {
		t.Fatal("real PostgreSQL required:", e)
	}
	schema := "kd_e405fbb78b0d908a80e810aa_" + strings.ReplaceAll(id(), "-", "")[:12]
	i := &identities{users: map[string]Principal{}}
	for _, role := range []string{"infrastructure", "finance", "moderator", "support", "user"} {
		i.users[role] = Principal{ID: role, Role: role, Scopes: []string{"platform", "service", "owner", "other"}}
	}
	i.users["operator2"] = Principal{ID: "operator2", Role: "infrastructure", Scopes: []string{"platform", "service", "owner"}}
	i.users["owner"] = Principal{ID: "owner", Role: "user", Scopes: []string{"owner"}}
	s, e := New(pool, schema, i, verifier)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Install(ctx); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = pool.Exec(c, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		pool.Close()
	})
	return s, i
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func is(t *testing.T, e, want error) {
	t.Helper()
	if !errors.Is(e, want) {
		t.Fatalf("got %v want %v", e, want)
	}
}
func load[T any](t *testing.T, s *Store, kind, k string) T {
	t.Helper()
	var v T
	var b []byte
	must(t, s.pool.QueryRow(context.Background(), "SELECT body FROM "+s.schema+".objects WHERE kind=$1 AND id=$2", kind, k).Scan(&b))
	must(t, json.Unmarshal(b, &v))
	return v
}
func proposed(t *testing.T, s *Store, k, kind, scope, hash string) Action {
	t.Helper()
	a, e := s.Propose(context.Background(), "infrastructure", k, kind, scope, "reason", hash, time.Now().Add(time.Hour))
	must(t, e)
	a, e = s.Transition(context.Background(), "operator2", k, scope, "approve", "approved", a.Revision)
	must(t, e)
	return a
}
func TestOperatorDualApprovalTenRetriesOneJob(t *testing.T) {
	s, _ := fixture(t, nil)
	ctx := context.Background()
	a, e := s.Propose(ctx, "infrastructure", "replay1", "replay", "service", "repair projection", strings.Repeat("a", 64), time.Now().Add(time.Hour))
	must(t, e)
	_, e = s.Transition(ctx, "infrastructure", "replay1", "service", "self approve", "approved", 1)
	is(t, e, Denied)
	_, e = s.Transition(ctx, "operator2", "replay1", "service", "stale", "approved", 2)
	is(t, e, Conflict)
	a, e = s.Transition(ctx, "operator2", "replay1", "service", "approve", "approved", a.Revision)
	must(t, e)
	var job string
	for n := 0; n < 10; n++ {
		r, e := s.Transition(ctx, "infrastructure", "replay1", "service", "retry", "executing", a.Revision)
		must(t, e)
		if job != "" && job != r.JobID {
			t.Fatal("duplicate job")
		}
		job = r.JobID
	}
	var count int
	must(t, s.pool.QueryRow(ctx, "SELECT count(*) FROM "+s.schema+".objects WHERE kind='job'").Scan(&count))
	if count != 1 {
		t.Fatal(count)
	}
}
func TestApprovalRevocationAndResourceScope(t *testing.T) {
	s, i := fixture(t, nil)
	a := proposed(t, s, "job", "replay", "service", strings.Repeat("a", 64))
	i.remove("operator2")
	_, e := s.Transition(context.Background(), "infrastructure", "job", "service", "execute", "executing", a.Revision)
	is(t, e, Denied)
	var got Action
	is(t, s.Read(context.Background(), "infrastructure", "other", "wrong scope", "action", "job", &got), Denied)
	if got.ID != "" {
		t.Fatal("denied read leaked data")
	}
}
func TestActorRevokedWhileWaitingForDatabaseLock(t *testing.T) {
	s, i := fixture(t, nil)
	ctx := context.Background()
	tx, e := s.pool.Begin(ctx)
	must(t, e)
	_, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", s.schema)
	must(t, e)
	i.calls = make(chan struct{}, 2)
	done := make(chan error, 1)
	go func() {
		_, e := s.Propose(ctx, "infrastructure", "revoke", "replay", "service", "reason", strings.Repeat("a", 64), time.Now().Add(time.Hour))
		done <- e
	}()
	<-i.calls
	i.remove("infrastructure")
	must(t, tx.Commit(ctx))
	is(t, <-done, Denied)
	var n int
	must(t, s.pool.QueryRow(ctx, "SELECT count(*) FROM "+s.schema+".objects").Scan(&n))
	if n != 0 {
		t.Fatal("revoked actor wrote state")
	}
}
func TestCapsApprovalDecreaseUnknownAndExpiry(t *testing.T) {
	s, _ := fixture(t, nil)
	ctx := context.Background()
	c := Cap{Capability: "pool_creation", Version: 1, Limit: 100, Effective: time.Now().Add(-time.Second), Expires: time.Now().Add(time.Hour)}
	is(t, s.SetCap(ctx, "infrastructure", "platform", "increase", "", c, 0), Denied)
	proposed(t, s, "cap", "cap_change", "platform", digest(c))
	must(t, s.SetCap(ctx, "infrastructure", "platform", "increase", "cap", c, 0))
	d, e := s.Admission(ctx, "pool_creation", 1)
	must(t, e)
	if !d.Allowed || d.Stale {
		t.Fatal(d)
	}
	c.Limit = 0
	must(t, s.SetCap(ctx, "infrastructure", "platform", "emergency stop", "", c, 1))
	d, e = s.Admission(ctx, "pool_creation", 1)
	must(t, e)
	if d.Allowed || d.Stale {
		t.Fatal(d)
	}
	d, e = s.Admission(ctx, "pool_creation", 2)
	must(t, e)
	if d.Allowed || !d.Stale {
		t.Fatal(d)
	}
	// Market read access is independent of write caps.
	must(t, s.Access(ctx, "support", "platform", "support case", "support_public", "", false))
}
func TestUnfunded100USDLAndExpiredDraft(t *testing.T) {
	s, _ := fixture(t, nil)
	ctx := context.Background()
	b := bounty("bounty", usdl, "100000000")
	must(t, s.CreateBounty(ctx, "finance", "service", "create", b))
	is(t, s.BountyState(ctx, "finance", "service", b.ID, "fund", "funded", "", "", 1), FundingRequired)
	is(t, s.BountyState(ctx, "finance", "service", b.ID, "activate", "active", "", "", 1), FundingRequired)
}
func bounty(k string, a Asset, n string) Bounty {
	return Bounty{ID: k, Title: "funded work", Rules: "submit artifact digest for reviewer approval", FundingSource: custody, RefundDisposition: "cancel releases reserved liability to original inventory; no payout implied", Amount: n, Asset: a, Deadline: time.Now().Add(time.Hour)}
}
func policy(k string) Policy {
	return Policy{ID: k, Owner: "owner", Mode: "disclosed_assistant", Disclosure: "Automated recommendation; no transaction execution", Asset: usdl, Instruments: []string{"pool1"}, Requests: 100, Spend: "100000000", QuoteAge: time.Minute, Expires: time.Now().Add(time.Hour)}
}
func setupAutomation(t *testing.T, s *Store, requests uint64) {
	t.Helper()
	ctx := context.Background()
	must(t, s.SetBudget(ctx, "owner", "owner", "opt in", Budget{Owner: "owner", Asset: usdl, Requests: requests, Spend: "100000000", Expires: time.Now().Add(time.Hour)}, 0))
	for _, k := range []string{"bot1", "bot2"} {
		must(t, s.CreatePolicy(ctx, "owner", "owner", "create", policy(k)))
		must(t, s.PolicyState(ctx, "owner", "owner", k, "opt in", "enabled", 1))
	}
}
func TestAutomationOwnerAggregateConcurrentOneReservation(t *testing.T) {
	s, _ := fixture(t, nil)
	setupAutomation(t, s, 1)
	var passed atomic.Int32
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, e := s.ReserveRecommendation(context.Background(), "owner", "owner", []string{"bot1", "bot2"}[n%2], fmt.Sprintf("request%d", n), "pool1", "suggest", time.Now().Add(-time.Second))
			if e == nil {
				passed.Add(1)
			} else if e != BudgetExceeded {
				t.Errorf("unexpected %v", e)
			}
		}(n)
	}
	wg.Wait()
	if passed.Load() != 1 {
		t.Fatal(passed.Load())
	}
	b := load[Budget](t, s, "budget", "owner.usdl")
	if b.Used != 1 || b.Reserved != "0" {
		t.Fatal(b)
	}
}
func TestRecommendationIdempotencyRevocationNoFakeTransaction(t *testing.T) {
	s, _ := fixture(t, nil)
	setupAutomation(t, s, 10)
	ctx := context.Background()
	r, e := s.ReserveRecommendation(ctx, "owner", "owner", "bot1", "suggest1", "pool1", "suggest", time.Now().Add(-time.Second))
	must(t, e)
	again, e := s.ReserveRecommendation(ctx, "owner", "owner", "bot1", "suggest1", "pool1", "retry", time.Now().Add(-time.Second))
	must(t, e)
	if r.ID != again.ID || r.Amount != "0" || r.State != "suggestion" || r.Disclosure == "" {
		t.Fatal(r, again)
	}
	encoded, _ := json.Marshal(r)
	if strings.Contains(strings.ToLower(string(encoded)), "transactionhash") {
		t.Fatal("fabricated tx field")
	}
	p := load[Policy](t, s, "policy", "bot1")
	must(t, s.PolicyState(ctx, "owner", "owner", "bot1", "stop", "revoked", p.Revision))
	_, e = s.ReserveRecommendation(ctx, "owner", "owner", "bot1", "next", "pool1", "suggest", time.Now().Add(-time.Second))
	is(t, e, Denied)
	is(t, s.AuthorizeDelegatedExecution(ctx, "owner"), Unavailable)
}
func TestAutomationDefaultDisabledExpiryInstrumentAndAge(t *testing.T) {
	s, _ := fixture(t, nil)
	ctx := context.Background()
	must(t, s.CreatePolicy(ctx, "owner", "owner", "create", policy("bot")))
	_, e := s.ReserveRecommendation(ctx, "owner", "owner", "bot", "key", "pool1", "suggest", time.Now().Add(-time.Second))
	is(t, e, Denied)
	must(t, s.PolicyState(ctx, "owner", "owner", "bot", "enable", "enabled", 1))
	_, e = s.ReserveRecommendation(ctx, "owner", "owner", "bot", "key", "pool1", "suggest", time.Now().Add(-time.Hour))
	is(t, e, Denied)
	_, e = s.ReserveRecommendation(ctx, "owner", "owner", "bot", "key", "notallowed", "suggest", time.Now().Add(-time.Second))
	is(t, e, Denied)
}
func TestAppendOnlyAuditAndRoleMinimization(t *testing.T) {
	s, _ := fixture(t, nil)
	ctx := context.Background()
	is(t, s.Reconcile(ctx, "moderator", "platform", "adjust treasury", pax), Denied)
	for _, v := range []struct{ credential, dataset string }{{"moderator", "finance_liabilities"}, {"support", "private_messages"}, {"finance", "infra_health"}, {"infrastructure", "finance_liabilities"}, {"support", "payment_secrets"}} {
		is(t, s.Access(ctx, v.credential, "platform", "case reason", v.dataset, "", false), Denied)
	}
	var n int
	must(t, s.pool.QueryRow(ctx, "SELECT count(*) FROM "+s.schema+".audit WHERE result='scope_denied'").Scan(&n))
	if n != 6 {
		t.Fatal(n)
	}
	if _, e := s.pool.Exec(ctx, "UPDATE "+s.schema+".audit SET reason='erase'"); e == nil {
		t.Fatal("audit update succeeded")
	}
	if _, e := s.pool.Exec(ctx, "TRUNCATE "+s.schema+".audit"); e == nil {
		t.Fatal("audit truncate succeeded")
	}
}
func TestIncidentImmediateContainmentAndNoCrossScopeRead(t *testing.T) {
	s, _ := fixture(t, nil)
	ctx := context.Background()
	must(t, s.DetectIncident(ctx, "operator2", "service", "incident1", "credential suspected", "infrastructure", "event-identity-123"))
	_, e := s.Propose(ctx, "infrastructure", "blocked", "replay", "service", "reason", strings.Repeat("a", 64), time.Now().Add(time.Hour))
	is(t, e, Denied)
	i := load[Incident](t, s, "incident", "incident1")
	if i.Contained.Sub(i.Detected) > 60*time.Second || i.State != "contained" {
		t.Fatal(i)
	}
	var hidden Incident
	is(t, s.Read(ctx, "operator2", "other", "read", "incident", "incident1", &hidden), Denied)
	is(t, s.RecoverIncident(ctx, "operator2", "service", "incident1", "restore", "missing", "rotation", "integrity", 1, nil), Unavailable)
}
func TestAuditFailureRollsBackAllDomainEffects(t *testing.T) {
	s, _ := fixture(t, nil)
	ctx := context.Background()
	_, e := s.pool.Exec(ctx, "ALTER TABLE "+s.schema+".audit ADD CONSTRAINT deny_test CHECK(result!='ok')")
	must(t, e)
	_, e = s.Propose(ctx, "infrastructure", "rollback", "replay", "service", "reason", strings.Repeat("a", 64), time.Now().Add(time.Hour))
	if e == nil {
		t.Fatal("audit failure ignored")
	}
	var n int
	must(t, s.pool.QueryRow(ctx, "SELECT count(*) FROM "+s.schema+".objects").Scan(&n))
	if n != 0 {
		t.Fatal("state committed without audit")
	}
}
func TestAmountsBoundsAndCanonicalText(t *testing.T) {
	for _, v := range []string{"", "01", "-1", "1.0", strings.Repeat("9", 79), new(big.Int).Lsh(big.NewInt(1), 256).String()} {
		if _, e := amount(v); e == nil {
			t.Fatal(v)
		}
	}
	if text(string([]byte{255}), 100) {
		t.Fatal("invalid UTF8 accepted")
	}
	if !usdl.Valid() || !pax.Valid() || Asset(Asset{Chain: 125, Address: USDLAddress, Decimals: 18}).Valid() {
		t.Fatal("asset unit mismatch")
	}
	n, e := amount("1000000")
	must(t, e)
	if n.Cmp(big.NewInt(1000000)) != 0 {
		t.Fatal("fee units")
	}
}

// fixtureAuthority is explicitly an isolated test manifest/custody approval,
// never an attestation of the current mainnet deployment or production release.
type fixtureAuthority struct{ deny bool }

func (f fixtureAuthority) Current(ctx context.Context, hash string, a common.Address) error {
	if f.deny || hash != fixtureManifest || a != common.HexToAddress(custody) {
		return Denied
	}
	return nil
}

// Award approval is a synthetic reviewer-authority fixture, not funding proof.
type fixtureAward struct{}

func (fixtureAward) VerifyAward(ctx context.Context, b Bounty, evidence, dest string) error {
	if b.Rules != "submit artifact digest for reviewer approval" || evidence != "reviewed-artifact-sha256" || dest != beneficiary {
		return Denied
	}
	return nil
}
func chain(t *testing.T, awards AwardAuthority) *ChainVerifier {
	t.Helper()
	url := os.Getenv("L47_ANVIL_URL")
	if url == "" {
		t.Fatal("owned Anvil required; use check.py")
	}
	c, e := ethclient.Dial(url)
	must(t, e)
	t.Cleanup(c.Close)
	v, e := NewChainVerifier(c, custody, fixtureManifest, fixtureAuthority{}, awards)
	must(t, e)
	return v
}
func setBalance(t *testing.T, v *ChainVerifier, n string) {
	t.Helper()
	value, ok := new(big.Int).SetString(n, 10)
	if !ok {
		t.Fatal(n)
	}
	var result any
	must(t, v.client.Client().CallContext(context.Background(), &result, "anvil_setBalance", custody, "0x"+value.Text(16)))
}
func readyAward(t *testing.T, s *Store, k, n string) {
	t.Helper()
	ctx := context.Background()
	must(t, s.CreateBounty(ctx, "finance", "service", "create", bounty(k, pax, n)))
	for n, next := range []string{"funded", "active", "submitted", "adjudicating", "awarded"} {
		must(t, s.BountyState(ctx, "finance", "service", k, "transition", next, "reviewed-artifact-sha256", beneficiary, uint64(n+1)))
	}
}
func TestRealChainInventoryConcurrent80Against100(t *testing.T) {
	v := chain(t, fixtureAward{})
	setBalance(t, v, "100000000000000000000")
	s, _ := fixture(t, v)
	must(t, s.Reconcile(context.Background(), "finance", "platform", "initial inventory", pax))
	ctx := context.Background()
	for _, k := range []string{"a", "b"} {
		must(t, s.CreateBounty(ctx, "finance", "service", "create", bounty(k, pax, "80000000000000000000")))
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for _, k := range []string{"a", "b"} {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			e := s.BountyState(ctx, "finance", "service", k, "reserve", "funded", "", "", 1)
			if e == nil {
				successes.Add(1)
			} else if e != FundingRequired {
				t.Error(e)
			}
		}(k)
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal(successes.Load())
	}
	i := load[Inventory](t, s, "inventory", "pax")
	if i.Reserved != "80000000000000000000" || i.Pending != i.Reserved {
		t.Fatal(i)
	}
}
func TestRealChainBountyBeneficiarySourceScopeAndMissingAwardAuthority(t *testing.T) {
	v := chain(t, nil)
	setBalance(t, v, "100000000000000000000")
	s, _ := fixture(t, v)
	ctx := context.Background()
	must(t, s.Reconcile(ctx, "finance", "platform", "inventory", pax))
	b := bounty("b", pax, "80")
	must(t, s.CreateBounty(ctx, "finance", "service", "create", b))
	is(t, s.BountyState(ctx, "finance", "other", "b", "wrong scope", "funded", "", "", 1), Denied)
	var hidden Bounty
	is(t, s.Read(ctx, "finance", "other", "read", "bounty", "b", &hidden), Denied)
	for n, next := range []string{"funded", "active", "submitted", "adjudicating"} {
		must(t, s.BountyState(ctx, "finance", "service", "b", "transition", next, "reviewed-artifact-sha256", beneficiary, uint64(n+1)))
	}
	is(t, s.BountyState(ctx, "finance", "service", "b", "award", "awarded", "reviewed-artifact-sha256", beneficiary, 5), Denied)
	b = bounty("wrong-source", pax, "10")
	b.FundingSource = beneficiary
	must(t, s.CreateBounty(ctx, "finance", "service", "create", b))
	is(t, s.BountyState(ctx, "finance", "service", b.ID, "reserve", "funded", "", "", 1), FundingRequired)
}
func TestRealChainUnknownThenConfirmedSettlementOnePayout(t *testing.T) {
	v := chain(t, fixtureAward{})
	setBalance(t, v, "100000000000000000000")
	s, _ := fixture(t, v)
	ctx := context.Background()
	must(t, s.Reconcile(ctx, "finance", "platform", "inventory", pax))
	readyAward(t, s, "b", "1000000")
	key := "bounty.b"
	unknown := "0x" + strings.Repeat("1", 64)
	must(t, s.ObserveSettlement(ctx, "finance", "service", key, "pending provider", unknown))
	o := load[Obligation](t, s, "settlement", key)
	if o.State != "unknown" {
		t.Fatal(o)
	}
	var hash common.Hash
	must(t, v.client.Client().CallContext(ctx, &hash, "eth_sendTransaction", map[string]string{"from": custody, "to": beneficiary, "value": "0xf4240"}))
	for n := 0; n < 100; n++ {
		if _, e := v.client.TransactionReceipt(ctx, hash); e == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	must(t, s.ObserveSettlement(ctx, "finance", "service", key, "reconcile accepted payout", hash.Hex()))
	for n := 0; n < 10; n++ {
		must(t, s.ObserveSettlement(ctx, "finance", "service", key, "retry", hash.Hex()))
	}
	o = load[Obligation](t, s, "settlement", key)
	if o.State != "settled" || o.SettlementReference != hash.Hex() {
		t.Fatal(o)
	}
	i := load[Inventory](t, s, "inventory", "pax")
	if i.Reserved != "0" || i.Pending != "0" || i.Settled != "1000000" {
		t.Fatal(i)
	}
	var count int
	must(t, s.pool.QueryRow(ctx, "SELECT count(*) FROM "+s.schema+".objects WHERE kind='receipt'").Scan(&count))
	if count != 1 {
		t.Fatal(count)
	}
	// Native gas is separately paid: unexplained inventory drift freezes new awards.
	must(t, s.Reconcile(ctx, "finance", "platform", "reconcile gas drift", pax))
	i = load[Inventory](t, s, "inventory", "pax")
	if !i.Frozen {
		t.Fatal("drift did not freeze")
	}
}
func TestRealReceiptWrongBeneficiaryAndManifestRejected(t *testing.T) {
	v := chain(t, fixtureAward{})
	setBalance(t, v, "100000000000000000000")
	ctx := context.Background()
	var hash common.Hash
	must(t, v.client.Client().CallContext(ctx, &hash, "eth_sendTransaction", map[string]string{"from": custody, "to": beneficiary, "value": "0x50"}))
	for n := 0; n < 100; n++ {
		if _, e := v.client.TransactionReceipt(ctx, hash); e == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	o := Obligation{Amount: "80", Asset: pax, Destination: custody}
	_, e := v.Settlement(ctx, o, hash.Hex())
	is(t, e, Denied)
	o.Destination = beneficiary
	o.Amount = "81"
	_, e = v.Settlement(ctx, o, hash.Hex())
	is(t, e, Denied)
	v.authority = fixtureAuthority{deny: true}
	_, e = v.Inventory(ctx, pax)
	is(t, e, Denied)
}
func TestBountyCancellationReleasesReservePreservesAudit(t *testing.T) {
	v := chain(t, fixtureAward{})
	setBalance(t, v, "100000000000000000000")
	s, _ := fixture(t, v)
	ctx := context.Background()
	must(t, s.Reconcile(ctx, "finance", "platform", "inventory", pax))
	must(t, s.CreateBounty(ctx, "finance", "service", "create", bounty("cancel", pax, "100")))
	must(t, s.BountyState(ctx, "finance", "service", "cancel", "fund", "funded", "", "", 1))
	must(t, s.BountyState(ctx, "finance", "service", "cancel", "published refund", "cancelled", "", "", 2))
	i := load[Inventory](t, s, "inventory", "pax")
	if i.Reserved != "0" || i.Pending != "0" || i.Reversed != "100" {
		t.Fatal(i)
	}
	is(t, s.BountyState(ctx, "finance", "service", "cancel", "award cancelled", "awarded", "reviewed-artifact-sha256", beneficiary, 3), Denied)
}

type blockingAward struct {
	entered chan struct{}
	resume  chan struct{}
}

func (b blockingAward) VerifyAward(ctx context.Context, bounty Bounty, e, d string) error {
	close(b.entered)
	select {
	case <-b.resume:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestActorRevokedDuringReadOnlyAwardVerificationRollsBack(t *testing.T) {
	gate := blockingAward{entered: make(chan struct{}), resume: make(chan struct{})}
	v := chain(t, gate)
	setBalance(t, v, "100000000000000000000")
	s, i := fixture(t, v)
	ctx := context.Background()
	must(t, s.Reconcile(ctx, "finance", "platform", "inventory", pax))
	must(t, s.CreateBounty(ctx, "finance", "service", "create", bounty("b", pax, "80")))
	for n, next := range []string{"funded", "active", "submitted", "adjudicating"} {
		must(t, s.BountyState(ctx, "finance", "service", "b", "transition", next, "reviewed-artifact-sha256", beneficiary, uint64(n+1)))
	}
	done := make(chan error, 1)
	go func() {
		done <- s.BountyState(ctx, "finance", "service", "b", "award", "awarded", "reviewed-artifact-sha256", beneficiary, 5)
	}()
	<-gate.entered
	i.remove("finance")
	close(gate.resume)
	is(t, <-done, Denied)
	b := load[Bounty](t, s, "bounty", "b")
	if b.State != "adjudicating" || b.Revision != 5 {
		t.Fatal(b)
	}
	var count int
	must(t, s.pool.QueryRow(ctx, "SELECT count(*) FROM "+s.schema+".objects WHERE kind='settlement'").Scan(&count))
	if count != 0 {
		t.Fatal("revoked verifier callback committed obligation")
	}
}
func TestExpiredBountyAndInventoryUnavailableRemainUnfunded(t *testing.T) {
	v := chain(t, fixtureAward{})
	setBalance(t, v, "100000000000000000000")
	s, _ := fixture(t, v)
	ctx := context.Background()
	must(t, s.Reconcile(ctx, "finance", "platform", "inventory", pax))
	b := bounty("expired", pax, "10")
	must(t, s.CreateBounty(ctx, "finance", "service", "create", b))
	b = load[Bounty](t, s, "bounty", "expired")
	b.Deadline = time.Now().Add(-time.Second)
	raw, _ := json.Marshal(b)
	_, e := s.pool.Exec(ctx, "UPDATE "+s.schema+".objects SET body=$1 WHERE kind='bounty' AND id='expired'", raw)
	must(t, e)
	is(t, s.BountyState(ctx, "finance", "service", "expired", "reserve", "funded", "", "", 1), Denied)
}

type durableJobVerifier struct{ s *Store }

func (v durableJobVerifier) Verify(ctx context.Context, a Action, state, reference string) error {
	var stored string
	e := v.s.pool.QueryRow(ctx, "SELECT body->>'reference' FROM "+v.s.schema+".objects WHERE kind='executor_result' AND id=$1", a.JobID).Scan(&stored)
	if e != nil || stored != reference {
		return Denied
	}
	return nil
}
func TestActionCompletionRequiresDurableExecutorEvidence(t *testing.T) {
	s, _ := fixture(t, nil)
	a := proposed(t, s, "complete", "replay", "service", strings.Repeat("a", 64))
	ctx := context.Background()
	a, e := s.Transition(ctx, "infrastructure", "complete", "service", "execute", "executing", a.Revision)
	must(t, e)
	is(t, s.CompleteAction(ctx, "infrastructure", "complete", "service", "complete", "succeeded", "result1", a.Revision, durableJobVerifier{s}), Unavailable)
	// This is an actual persisted private executor-result fixture, not proof that
	// an indexer replay worker is integrated or has replayed chain state.
	_, e = s.pool.Exec(ctx, "INSERT INTO "+s.schema+".objects(kind,id,body) VALUES('executor_result',$1,'{\"reference\":\"result1\"}')", a.JobID)
	must(t, e)
	must(t, s.CompleteAction(ctx, "infrastructure", "complete", "service", "complete", "succeeded", "result1", a.Revision, durableJobVerifier{s}))
	final := load[Action](t, s, "action", "complete")
	if final.State != "succeeded" {
		t.Fatal(final)
	}
	again, e := s.Transition(ctx, "infrastructure", "complete", "service", "retry", "executing", a.Revision)
	must(t, e)
	if again.JobID != a.JobID {
		t.Fatal("terminal retry re-executed")
	}
}
func TestCapsExpiryAndOwnerBudgetCannotResetUsage(t *testing.T) {
	s, _ := fixture(t, nil)
	ctx := context.Background()
	c := Cap{Capability: "quote", Version: 1, Limit: 1, Effective: time.Now().Add(-time.Second), Expires: time.Now().Add(time.Hour)}
	proposed(t, s, "caps", "cap_change", "platform", digest(c))
	must(t, s.SetCap(ctx, "infrastructure", "platform", "enable", "caps", c, 0))
	stored := load[Cap](t, s, "caps", "quote")
	stored.Expires = time.Now().Add(-time.Second)
	raw, _ := json.Marshal(stored)
	_, e := s.pool.Exec(ctx, "UPDATE "+s.schema+".objects SET body=$1 WHERE kind='caps' AND id='quote'", raw)
	must(t, e)
	d, e := s.Admission(ctx, "quote", 1)
	must(t, e)
	if d.Allowed || !d.Stale {
		t.Fatal(d)
	}
	setupAutomation(t, s, 1)
	_, e = s.ReserveRecommendation(ctx, "owner", "owner", "bot1", "first", "pool1", "suggest", time.Now().Add(-time.Second))
	must(t, e)
	b := load[Budget](t, s, "budget", "owner.usdl")
	must(t, s.SetBudget(ctx, "owner", "owner", "same limit", Budget{Owner: "owner", Asset: usdl, Requests: 1, Spend: "100000000", Expires: time.Now().Add(time.Hour)}, b.Revision))
	_, e = s.ReserveRecommendation(ctx, "owner", "owner", "bot2", "second", "pool1", "suggest", time.Now().Add(-time.Second))
	is(t, e, BudgetExceeded)
}
func TestStaleInventoryAndExpiredAutomationRejectNewWork(t *testing.T) {
	v := chain(t, fixtureAward{})
	setBalance(t, v, "100000000000000000000")
	s, _ := fixture(t, v)
	ctx := context.Background()
	must(t, s.Reconcile(ctx, "finance", "platform", "inventory", pax))
	i := load[Inventory](t, s, "inventory", "pax")
	i.CheckedAt = time.Now().Add(-61 * time.Second)
	raw, _ := json.Marshal(i)
	_, e := s.pool.Exec(ctx, "UPDATE "+s.schema+".objects SET body=$1 WHERE kind='inventory' AND id='pax'", raw)
	must(t, e)
	must(t, s.CreateBounty(ctx, "finance", "service", "create", bounty("stale", pax, "1")))
	is(t, s.BountyState(ctx, "finance", "service", "stale", "reserve", "funded", "", "", 1), FundingRequired)
	setupAutomation(t, s, 10)
	p := load[Policy](t, s, "policy", "bot1")
	p.Expires = time.Now().Add(-time.Second)
	raw, _ = json.Marshal(p)
	_, e = s.pool.Exec(ctx, "UPDATE "+s.schema+".objects SET body=$1 WHERE kind='policy' AND id='bot1'", raw)
	must(t, e)
	_, e = s.ReserveRecommendation(ctx, "owner", "owner", "bot1", "expired", "pool1", "suggest", time.Now().Add(-time.Second))
	is(t, e, Denied)
}
func TestBulkExportSingleApprovedUseAndNoForbiddenDataset(t *testing.T) {
	s, _ := fixture(t, nil)
	ctx := context.Background()
	is(t, s.Access(ctx, "finance", "platform", "export", "finance_liabilities", "", true), Denied)
	proposed(t, s, "export", "bulk_export", "platform", digest(map[string]string{"dataset": "finance_liabilities", "scope": "platform"}))
	must(t, s.Access(ctx, "finance", "platform", "approved export", "finance_liabilities", "export", true))
	is(t, s.Access(ctx, "finance", "platform", "reuse", "finance_liabilities", "export", true), Denied)
	is(t, s.Access(ctx, "support", "platform", "export messages", "private_messages", "export", true), Denied)
}

func TestPublicMappingsDoNotInventDelegationOrPayment(t *testing.T) {
	p := policy("00000000-0000-4000-8000-000000000001")
	p.Owner = "00000000-0000-4000-8000-000000000002"
	p.State = "enabled"
	p.Instruments = []string{beneficiary}
	v, e := PublicCampaign(p)
	must(t, e)
	if v.State != "blocked" || v.RecommendationState != "enabled" || len(v.CapabilityBlockers) != 1 {
		t.Fatal(v)
	}
	b := bounty("00000000-0000-4000-8000-000000000003", usdl, "1000000")
	b.Scope = "service"
	b.State = "awarded"
	o := &Obligation{BountyID: b.ID, Scope: b.Scope, Asset: usdl, Amount: b.Amount, State: "unknown"}
	view, e := PublicBounty(b, beneficiary, o)
	must(t, e)
	if view.State == "paid" {
		t.Fatal("unknown payout shown as paid")
	}
	o.State = "settled"
	o.SettlementReference = "0x" + strings.Repeat("1", 64)
	view, e = PublicBounty(b, beneficiary, o)
	must(t, e)
	if view.State != "paid" {
		t.Fatal(view)
	}
	b.Asset = pax
	_, e = PublicBounty(b, beneficiary, nil)
	is(t, e, Unavailable)
	if status, code := ErrorResponse(Denied); status != 403 || code != "SCOPE_DENIED" {
		t.Fatal(status, code)
	}
}

func TestCapTemporalAuthorityExpansionRequiresDualApproval(t *testing.T) {
	s, _ := fixture(t, nil)
	ctx := context.Background()
	c := Cap{Capability: "quote", Version: 1, Limit: 100, Effective: time.Now().Add(-time.Second), Expires: time.Now().Add(time.Hour)}
	proposed(t, s, "initial", "cap_change", "platform", digest(c))
	must(t, s.SetCap(ctx, "infrastructure", "platform", "initial", "initial", c, 0))
	extension := c
	extension.Expires = extension.Expires.Add(time.Hour)
	is(t, s.SetCap(ctx, "infrastructure", "platform", "equal limit expiry extension", "", extension, 1), Denied)
	extension.Limit = 50
	is(t, s.SetCap(ctx, "infrastructure", "platform", "lower quota broader interval", "", extension, 1), Denied)
	earlier := c
	earlier.Effective = earlier.Effective.Add(-time.Minute)
	is(t, s.SetCap(ctx, "infrastructure", "platform", "earlier activation", "", earlier, 1), Denied)
	expired := c
	expired.Expires = time.Now().Add(-time.Second)
	expired.Effective = expired.Expires.Add(-time.Hour)
	expired.Revision = 1
	raw, _ := json.Marshal(expired)
	_, e := s.pool.Exec(ctx, "UPDATE "+s.schema+".objects SET body=$1 WHERE kind='caps' AND id='quote'", raw)
	must(t, e)
	is(t, s.SetCap(ctx, "infrastructure", "platform", "reactivate expired equal limit", "", c, 1), Denied)
	zero := extension
	zero.Limit = 0
	must(t, s.SetCap(ctx, "infrastructure", "platform", "emergency disable", "", zero, 1))
}
