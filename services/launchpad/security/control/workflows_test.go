package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type phase2Environment struct {
	store       *Store
	identity    *PostgresIdentity
	credentials map[string]string
	actors      map[string]string
	server      *httptest.Server
}

func phase2EnvironmentFor(t *testing.T) *phase2Environment {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, fixtureDSN)
	must(t, err)
	must(t, pool.Ping(ctx))
	schema := "l47_p2_" + strings.ReplaceAll(id(), "-", "")[:12]
	identity, err := NewPostgresIdentity(pool, schema, schema)
	must(t, err)
	store, err := New(pool, schema, identity, nil)
	must(t, err)
	must(t, store.Install(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		pool.Close()
	})
	migration, err := IdentityMigration(schema)
	must(t, err)
	_, err = pool.Exec(ctx, migration)
	must(t, err)
	root := os.Getenv("L47_SOURCE_ROOT")
	if root == "" {
		t.Fatal("L47_SOURCE_ROOT must bind the actual source candidate")
	}
	migrationBytes, err := os.ReadFile(filepath.Join(root, "spec/production-system/integration/migrations/330000.sql"))
	must(t, err)
	tx, err := pool.Begin(ctx)
	must(t, err)
	_, err = tx.Exec(ctx, strings.Replace(string(migrationBytes), `SET LOCAL search_path = "users", pg_catalog;`, `SET LOCAL search_path = `+pgx.Identifier{schema}.Sanitize()+`, pg_catalog;`, 1))
	must(t, err)
	must(t, tx.Commit(ctx))
	env := &phase2Environment{store: store, identity: identity, credentials: map[string]string{}, actors: map[string]string{}}
	for _, role := range []string{"infrastructure", "operator2", "finance", "moderator", "support", "user"} {
		token := strings.ReplaceAll(id()+id(), "-", "")
		actor := id()
		credentialID := id()
		actualRole := role
		if role == "operator2" {
			actualRole = "infrastructure"
		}
		scopes := []string{"platform", "service", actor}
		h := sha256.Sum256([]byte(token))
		var session any
		if role == "user" {
			sessionID := id()
			session = sessionID
			_, err = pool.Exec(ctx, "INSERT INTO "+store.schema+".identity_accounts(account_id,created_at) VALUES($1,clock_timestamp())", actor)
			must(t, err)
			_, err = pool.Exec(ctx, "INSERT INTO "+store.schema+".identity_wallet_links(link_id,account_id,chain_id,wallet,visibility,state,linked_at) VALUES($1,$2,125,$3,'private','verified',clock_timestamp())", id(), actor, beneficiary)
			must(t, err)
			_, err = pool.Exec(ctx, "INSERT INTO "+store.schema+".identity_sessions(session_id,account_id,wallet,auth_epoch,access_hash,csrf_hash,issued_at,access_expires_at,refresh_expires_at,reauthenticated_at,last_seen_at,device_label) VALUES($1,$2,$3,0,$4,$4,clock_timestamp(),clock_timestamp()+interval '15 minutes',clock_timestamp()+interval '1 day',clock_timestamp(),clock_timestamp(),'real SQL identity qualification')", sessionID, actor, beneficiary, h[:])
			must(t, err)
		}
		_, err = pool.Exec(ctx, "INSERT INTO "+store.schema+".authority_credentials(credential_id,actor_id,role,scopes,token_hash,session_id,expires_at) VALUES($1,$2,$3,$4,$5,$6,clock_timestamp()+interval '15 minutes')", credentialID, actor, actualRole, scopes, h[:], session)
		must(t, err)
		env.credentials[role] = token
		env.actors[role] = actor
	}
	service, err := NewHTTPService(store, nil)
	must(t, err)
	env.server = httptest.NewServer(service.Handler())
	t.Cleanup(env.server.Close)
	return env
}
func (e *phase2Environment) post(t *testing.T, role, operation string, body any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	must(t, err)
	req, err := http.NewRequest(http.MethodPost, e.server.URL+"/v2/control/"+operation, bytes.NewReader(raw))
	must(t, err)
	req.Header.Set("Authorization", "Bearer "+e.credentials[role])
	req.Header.Set("Content-Type", "application/json")
	res, err := e.server.Client().Do(req)
	must(t, err)
	defer res.Body.Close()
	response, err := io.ReadAll(res.Body)
	must(t, err)
	return res.StatusCode, response
}
func TestPhase2AuthenticatedReplayTenRetriesOneDurableJob(t *testing.T) {
	e := phase2EnvironmentFor(t)
	ctx := context.Background()
	a, err := e.store.ProposeReplay(ctx, e.credentials["infrastructure"], ReplayProposal{Key: "replay-http", Scope: "service", Reason: "restore projection", Expires: time.Now().Add(time.Hour), Payload: ReplayPayload{ChainID: "125", FromHeight: "1", ToHeight: "2", Projection: "candles", DryRun: true}})
	must(t, err)
	a, err = e.store.Transition(ctx, e.credentials["operator2"], "replay-http", "service", "reviewed replay", "approved", a.Revision)
	must(t, err)
	revision := a.Revision
	var job string
	for n := 0; n < 10; n++ {
		status, raw := e.post(t, "infrastructure", "transition", TransitionRequest{Key: "replay-http", Scope: "service", Reason: "execute approved replay", Next: "executing", Expected: &revision})
		if status != 200 {
			t.Fatalf("status%d %s", status, raw)
		}
		var envelope struct {
			Data Action `json:"data"`
		}
		must(t, json.Unmarshal(raw, &envelope))
		if job != "" && job != envelope.Data.JobID {
			t.Fatal("duplicate job")
		}
		job = envelope.Data.JobID
	}
	var count int
	must(t, e.store.pool.QueryRow(ctx, "SELECT count(*) FROM "+e.store.schema+".objects WHERE kind='job'").Scan(&count))
	if count != 1 || job == "" {
		t.Fatal(count, job)
	}
}
func TestPhase2OwnerRecommendationRealAuthorityConcurrentBudget(t *testing.T) {
	e := phase2EnvironmentFor(t)
	ctx := context.Background()
	owner := e.actors["user"]
	token := e.credentials["user"]
	b := Budget{Owner: owner, Asset: usdl, Requests: 1, Spend: "100000000", Expires: time.Now().Add(time.Hour)}
	must(t, e.store.SetBudget(ctx, token, owner, "explicit owner budget", b, 0))
	p := Policy{ID: id(), Owner: owner, Mode: "disclosed_assistant", Disclosure: "Automated recommendation only", Asset: usdl, Instruments: []string{USDLAddress}, Requests: 100, Spend: "100000000", QuoteAge: time.Minute, Expires: time.Now().Add(time.Hour)}
	must(t, e.store.CreatePolicy(ctx, token, owner, "create disclosed policy", p))
	must(t, e.store.PolicyState(ctx, token, owner, p.ID, "explicit enable", "enabled", 1))
	var count atomic.Int32
	var wg sync.WaitGroup
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r, err := e.store.ReserveRecommendation(ctx, token, owner, p.ID, fmt.Sprintf("request%d", n), USDLAddress, "request suggestion", time.Now().Add(-time.Second))
			if err == nil {
				count.Add(1)
				if r.Amount != "0" || r.State != "suggestion" {
					t.Error(r)
				}
			} else if err != BudgetExceeded {
				t.Error(err)
			}
		}(n)
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatal(count.Load())
	}
	view, err := e.store.Campaign(ctx, token, owner, p.ID, "inspect disclosure")
	must(t, err)
	if view.State != "blocked" || view.RecommendationState != "enabled" || len(view.CapabilityBlockers) != 1 {
		t.Fatal(view)
	}
	raw, err := json.Marshal(view)
	must(t, err)
	if bytes.Contains(raw, []byte("transactionHash")) {
		t.Fatal("fabricated transaction hash")
	}
	must(t, e.store.PolicyState(ctx, token, owner, p.ID, "owner stop", "revoked", 3))
	_, err = e.store.ReserveRecommendation(ctx, token, owner, p.ID, "future", USDLAddress, "future queued work", time.Now())
	is(t, err, Denied)
}
func TestPhase2UnfundedBountyHTTPRetainsFundingRequired(t *testing.T) {
	e := phase2EnvironmentFor(t)
	b := bounty(id(), usdl, "100000000")
	status, raw := e.post(t, "finance", "bounties", BountyRequest{Scope: "service", Reason: "publish funded work draft", Bounty: b})
	if status != 200 {
		t.Fatalf("%d %s", status, raw)
	}
	revision := uint64(1)
	status, raw = e.post(t, "finance", "bounty-state", BountyTransitionRequest{TransitionRequest: TransitionRequest{Key: b.ID, Scope: "service", Reason: "activate unfunded draft", Next: "active", Expected: &revision}})
	if status != 503 || !bytes.Contains(raw, []byte("funding_required")) {
		t.Fatalf("%d %s", status, raw)
	}
}
func TestPhase2ModeratorTreasuryDeniedWithImmutableAudit(t *testing.T) {
	e := phase2EnvironmentFor(t)
	status, raw := e.post(t, "moderator", "inventory", InventoryRequest{Scope: "platform", Reason: "attempt treasury adjustment", Asset: usdl})
	if status != 403 {
		t.Fatalf("%d %s", status, raw)
	}
	var count int
	must(t, e.store.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+e.store.schema+".audit WHERE actor=$1 AND operation='inventory' AND result='scope_denied'", e.actors["moderator"]).Scan(&count))
	if count != 1 {
		t.Fatal(count)
	}
	_, err := e.store.pool.Exec(context.Background(), "UPDATE "+e.store.schema+".audit SET result='ok'")
	if err == nil {
		t.Fatal("audit mutable")
	}
}
func TestPhase2CredentialContainmentRevokesActualDatabaseIdentity(t *testing.T) {
	e := phase2EnvironmentFor(t)
	ctx := context.Background()
	actor := e.actors["operator2"]
	snapshot, err := e.identity.Resolve(ctx, e.credentials["operator2"])
	must(t, err)
	start := time.Now()
	must(t, e.store.DetectIncident(ctx, e.credentials["infrastructure"], "service", "compromise", "detected replay credential compromise", actor, "incident-artifact-digest"))
	worker := IncidentWorker{Store: e.store, Identity: e.identity, Credential: e.credentials["infrastructure"], Observe: func(_ string, err error) {
		if err != nil && err != Unavailable {
			t.Error(err)
		}
	}}
	must(t, worker.Drain(ctx))
	_, err = e.identity.Resolve(ctx, e.credentials["operator2"])
	is(t, err, Denied)
	if e.identity.Active(ctx, snapshot) || time.Since(start) > 60*time.Second {
		t.Fatal("revocation outside bound")
	}
	outbox := load[map[string]string](t, e.store, "incident_outbox", "compromise")
	if outbox["state"] != "credentials_revoked" {
		t.Fatal(outbox)
	}
	is(t, (IncidentRecovery{e.store}).Check(ctx, Incident{ID: "compromise", ServiceIdentity: actor}, "anything", "anything"), Denied)
}
func TestPhase2D11SessionRevocationInvalidatesApprovalIdentity(t *testing.T) {
	e := phase2EnvironmentFor(t)
	ctx := context.Background()
	p, err := e.identity.Resolve(ctx, e.credentials["user"])
	must(t, err)
	_, err = e.store.pool.Exec(ctx, "UPDATE "+e.store.schema+".identity_sessions SET revoked_at=clock_timestamp() WHERE session_id=$1", p.SessionID)
	must(t, err)
	_, err = e.identity.Resolve(ctx, e.credentials["user"])
	is(t, err, Denied)
	if e.identity.Active(ctx, p) {
		t.Fatal("revoked session active")
	}
}
func TestPhase2CapsFreshDisableAndUnsupportedSettlementRemainVisible(t *testing.T) {
	e := phase2EnvironmentFor(t)
	ctx := context.Background()
	c := Cap{Capability: "pool_creation", Version: 1, Limit: 0, Effective: time.Now().Add(-time.Second), Expires: time.Now().Add(time.Hour)}
	must(t, e.store.SetCap(ctx, e.credentials["infrastructure"], "platform", "emergency disable", "", c, 0))
	res, err := e.server.Client().Get(e.server.URL + "/v2/control/caps/pool_creation")
	must(t, err)
	defer res.Body.Close()
	var envelope struct {
		Data CapWarning `json:"data"`
	}
	must(t, json.NewDecoder(res.Body).Decode(&envelope))
	if envelope.Data.Allowed || envelope.Data.Stale || envelope.Data.Capability != "pool_creation" {
		t.Fatal(envelope)
	}
	must(t, e.store.Access(ctx, e.credentials["support"], "platform", "market read case", "support_public", "", false))
	status, raw := e.post(t, "finance", "settlements", SettlementRequest{Scope: "service", Key: "bounty.pending", Reason: "configured provider unavailable"})
	if status != 503 || !bytes.Contains(raw, []byte("authority_unavailable")) {
		t.Fatalf("%d %s", status, raw)
	}
}

func TestPhase3QuarantinedApprovalCannotDispatchReplayBeforeRevocation(t *testing.T) {
	e := phase2EnvironmentFor(t)
	ctx := context.Background()
	a, err := e.store.ProposeReplay(ctx, e.credentials["infrastructure"], ReplayProposal{Key: "quarantined-replay", Scope: "service", Reason: "restore projection", Expires: time.Now().Add(time.Hour), Payload: ReplayPayload{ChainID: "125", FromHeight: "1", ToHeight: "2", Projection: "candles", DryRun: true}})
	must(t, err)
	a, err = e.store.Transition(ctx, e.credentials["operator2"], "quarantined-replay", "service", "review bounds", "approved", a.Revision)
	must(t, err)
	must(t, e.store.DetectIncident(ctx, e.credentials["infrastructure"], "service", "approver-compromise", "credential compromise detected", e.actors["operator2"], "retained-detection-event"))
	if !e.identity.Active(ctx, a.Approver) {
		t.Fatal("the test must exercise quarantine before asynchronous credential revocation")
	}
	revision := a.Revision
	status, raw := e.post(t, "infrastructure", "transition", TransitionRequest{Key: "quarantined-replay", Scope: "service", Reason: "execute approved replay", Next: "executing", Expected: &revision})
	if status != 403 {
		t.Fatalf("%d %s", status, raw)
	}
	var count int
	must(t, e.store.pool.QueryRow(ctx, "SELECT count(*) FROM "+e.store.schema+".objects WHERE kind='job'").Scan(&count))
	if count != 0 || load[Action](t, e.store, "action", "quarantined-replay").Revision != revision {
		t.Fatal("quarantined approval created a durable effect")
	}
}

func TestPhase3QuarantinePreventsCapExpansionAndBulkAccess(t *testing.T) {
	e := phase2EnvironmentFor(t)
	ctx := context.Background()
	cap := Cap{Capability: "quote", Version: 1, Limit: 100, Effective: time.Now().Add(-time.Second), Expires: time.Now().Add(time.Hour)}
	for _, q := range []struct{ key, kind, scope, hash string }{
		{"cap-approval", "cap_change", "platform", digest(cap)},
		{"export-approval", "bulk_export", "platform", digest(map[string]string{"dataset": "finance_liabilities", "scope": "platform"})},
	} {
		a, err := e.store.Propose(ctx, e.credentials["infrastructure"], q.key, q.kind, q.scope, "review exact scope", q.hash, time.Now().Add(time.Hour))
		must(t, err)
		_, err = e.store.Transition(ctx, e.credentials["operator2"], q.key, q.scope, "approve exact payload", "approved", a.Revision)
		must(t, err)
	}
	must(t, e.store.DetectIncident(ctx, e.credentials["infrastructure"], "service", "approver-contained", "suspicious credential use", e.actors["operator2"], "detection-event"))
	is(t, e.store.SetCap(ctx, e.credentials["infrastructure"], "platform", "raise quote exposure", "cap-approval", cap, 0), Denied)
	is(t, e.store.Access(ctx, e.credentials["finance"], "platform", "export liabilities", "finance_liabilities", "export-approval", true), Denied)
	cap.Limit = 0
	must(t, e.store.SetCap(ctx, e.credentials["infrastructure"], "platform", "reduce exposure immediately", "", cap, 0))
	must(t, e.store.Access(ctx, e.credentials["support"], "platform", "market read case", "support_public", "", false))
	if load[Action](t, e.store, "action", "export-approval").State != "approved" {
		t.Fatal("denied export consumed approval")
	}
}

func TestPhase3ConcurrentReplayRetriesShareOneDurableJob(t *testing.T) {
	e := phase2EnvironmentFor(t)
	ctx := context.Background()
	a, err := e.store.ProposeReplay(ctx, e.credentials["infrastructure"], ReplayProposal{Key: "parallel-replay", Scope: "service", Reason: "restore exact interval", Expires: time.Now().Add(time.Hour), Payload: ReplayPayload{ChainID: "125", FromHeight: "1", ToHeight: "2", Projection: "candles", DryRun: true}})
	must(t, err)
	a, err = e.store.Transition(ctx, e.credentials["operator2"], "parallel-replay", "service", "approve exact interval", "approved", a.Revision)
	must(t, err)
	revision := a.Revision
	jobs := make(chan string, 10)
	var wg sync.WaitGroup
	for n := 0; n < 10; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := e.store.TransitionWorkflow(ctx, e.credentials["infrastructure"], TransitionRequest{Key: "parallel-replay", Scope: "service", Reason: "retry approved replay", Next: "executing", Expected: &revision})
			if err != nil {
				t.Error(err)
				return
			}
			jobs <- got.JobID
		}()
	}
	wg.Wait()
	close(jobs)
	var first string
	var responses int
	for job := range jobs {
		if job == "" || (first != "" && first != job) {
			t.Fatal("retry dispatched a different job")
		}
		first = job
		responses++
	}
	var count int
	must(t, e.store.pool.QueryRow(ctx, "SELECT count(*) FROM "+e.store.schema+".objects WHERE kind='job'").Scan(&count))
	if responses != 10 || count != 1 {
		t.Fatal(responses, count)
	}
}

func TestPhase3JobWriteInterruptionRollsBackThenRetriesOnce(t *testing.T) {
	e := phase2EnvironmentFor(t)
	ctx := context.Background()
	a, err := e.store.ProposeReplay(ctx, e.credentials["infrastructure"], ReplayProposal{Key: "interrupted-replay", Scope: "service", Reason: "restore projection", Expires: time.Now().Add(time.Hour), Payload: ReplayPayload{ChainID: "125", FromHeight: "1", ToHeight: "2", Projection: "candles", DryRun: true}})
	must(t, err)
	a, err = e.store.Transition(ctx, e.credentials["operator2"], "interrupted-replay", "service", "approve replay", "approved", a.Revision)
	must(t, err)
	_, err = e.store.pool.Exec(ctx, "CREATE FUNCTION "+e.store.schema+".interrupt_job() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.kind='job' THEN RAISE EXCEPTION 'injected durable job interruption'; END IF; RETURN NEW; END $$; CREATE TRIGGER interrupt_job BEFORE INSERT ON "+e.store.schema+".objects FOR EACH ROW EXECUTE FUNCTION "+e.store.schema+".interrupt_job()")
	must(t, err)
	_, err = e.store.Transition(ctx, e.credentials["infrastructure"], "interrupted-replay", "service", "execute replay", "executing", a.Revision)
	if err == nil {
		t.Fatal("database interruption did not fail execution")
	}
	stored := load[Action](t, e.store, "action", "interrupted-replay")
	if stored.State != "approved" || stored.Revision != a.Revision || stored.JobID != "" {
		t.Fatal(stored)
	}
	_, err = e.store.pool.Exec(ctx, "DROP TRIGGER interrupt_job ON "+e.store.schema+".objects")
	must(t, err)
	job, err := e.store.Transition(ctx, e.credentials["infrastructure"], "interrupted-replay", "service", "recover durable replay", "executing", a.Revision)
	must(t, err)
	again, err := e.store.Transition(ctx, e.credentials["infrastructure"], "interrupted-replay", "service", "repeat recovered replay", "executing", a.Revision)
	must(t, err)
	if job.JobID == "" || job.JobID != again.JobID {
		t.Fatal(job, again)
	}
}

func TestPhase3CrossAgentBudgetsRemainDurableAndHistoryIsDisclosed(t *testing.T) {
	e := phase2EnvironmentFor(t)
	ctx := context.Background()
	owner, token := e.actors["user"], e.credentials["user"]
	must(t, e.store.SetBudget(ctx, token, owner, "explicit owner opt in", Budget{Owner: owner, Asset: usdl, Requests: 1, Spend: "100000000", Expires: time.Now().Add(time.Hour)}, 0))
	policies := []string{id(), id()}
	for _, key := range policies {
		must(t, e.store.CreatePolicy(ctx, token, owner, "create disclosed assistant", Policy{ID: key, Owner: owner, Mode: "disclosed_assistant", Disclosure: "Automated recommendation only", Asset: usdl, Instruments: []string{USDLAddress}, Requests: 100, Spend: "100000000", QuoteAge: time.Minute, Expires: time.Now().Add(time.Hour)}))
		must(t, e.store.PolicyState(ctx, token, owner, key, "explicit opt in", "enabled", 1))
	}
	var successes atomic.Int32
	keys := make(chan string, 12)
	var wg sync.WaitGroup
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("agent-request%d", n)
			r, err := e.store.ReserveRecommendation(ctx, token, owner, policies[n%2], key, USDLAddress, "request disclosed suggestion", time.Now().Add(-time.Second))
			if err == nil {
				successes.Add(1)
				keys <- key
				if r.Amount != "0" || r.State != "suggestion" {
					t.Error(r)
				}
			} else if err != BudgetExceeded {
				t.Error(err)
			}
		}(n)
	}
	wg.Wait()
	close(keys)
	if successes.Load() != 1 {
		t.Fatal(successes.Load())
	}
	key := <-keys
	view, err := e.store.Recommendation(ctx, token, owner, "inspect disclosed history", key)
	must(t, err)
	encoded, err := json.Marshal(view)
	must(t, err)
	if view.OrganicRankingEligible || view.Origin != "automated_recommendation" || bytes.Contains(encoded, []byte("transactionHash")) || bytes.Contains(encoded, []byte("balanceChange")) {
		t.Fatal(string(encoded))
	}
	p := load[Policy](t, e.store, "policy", view.Reservation.PolicyID)
	must(t, e.store.PolicyState(ctx, token, owner, p.ID, "revoke future automation", "revoked", p.Revision))
	_, err = e.store.ReserveRecommendation(ctx, token, owner, p.ID, "queued-after-revoke", USDLAddress, "attempt queued work", time.Now().Add(-time.Second))
	is(t, err, Denied)
	view, err = e.store.Recommendation(ctx, token, owner, "inspect retained history", key)
	must(t, err)
	if view.Campaign.RecommendationState != "revoked" || view.Reservation.Amount != "0" || load[Budget](t, e.store, "budget", owner+".usdl").Used != 1 {
		t.Fatal(view)
	}
}

func TestPhase3CredentialRotationCannotReuseRevokedApproval(t *testing.T) {
	e := phase2EnvironmentFor(t)
	ctx := context.Background()
	a, err := e.store.Propose(ctx, e.credentials["infrastructure"], "rotation-replay", "replay", "service", "restore projection", strings.Repeat("a", 64), time.Now().Add(time.Hour))
	must(t, err)
	a, err = e.store.Transition(ctx, e.credentials["operator2"], "rotation-replay", "service", "approve exact scope", "approved", a.Revision)
	must(t, err)
	newToken := strings.ReplaceAll(id()+id(), "-", "")
	h := sha256.Sum256([]byte(newToken))
	_, err = e.store.pool.Exec(ctx, "INSERT INTO "+e.store.schema+".authority_credentials(credential_id,actor_id,role,scopes,token_hash,expires_at) VALUES($1,$2,'infrastructure',$3,$4,clock_timestamp()+interval '15 minutes')", id(), e.actors["operator2"], []string{"platform", "service", e.actors["operator2"]}, h[:])
	must(t, err)
	_, err = e.store.pool.Exec(ctx, "UPDATE "+e.store.schema+".authority_credentials SET revoked_at=clock_timestamp() WHERE credential_id=$1", a.Approver.CredentialID)
	must(t, err)
	if _, err = e.identity.Resolve(ctx, newToken); err != nil {
		t.Fatal("replacement credential unavailable", err)
	}
	if e.identity.Active(ctx, a.Approver) {
		t.Fatal("old approval inherited the new credential")
	}
	_, err = e.store.Transition(ctx, e.credentials["infrastructure"], "rotation-replay", "service", "execute old approval", "executing", a.Revision)
	is(t, err, Denied)
	status, raw := e.post(t, "operator2", "inventory", InventoryRequest{Scope: "platform", Reason: "use revoked credential", Asset: usdl})
	if status != 401 {
		t.Fatalf("%d %s", status, raw)
	}
}

func TestPhase3RecoveryWithoutProviderProofRetainsContainment(t *testing.T) {
	e := phase2EnvironmentFor(t)
	ctx := context.Background()
	must(t, e.store.DetectIncident(ctx, e.credentials["infrastructure"], "service", "recovery-blocked", "compromised service credential", e.actors["finance"], "preserved-event-identity"))
	payload := map[string]string{"incident": "recovery-blocked", "rotation": "rotation-reference", "integrity": "integrity-reference"}
	a, err := e.store.Propose(ctx, e.credentials["infrastructure"], "recovery-approval", "incident_recovery", "service", "review recovery", digest(payload), time.Now().Add(time.Hour))
	must(t, err)
	_, err = e.store.Transition(ctx, e.credentials["operator2"], "recovery-approval", "service", "approve recovery", "approved", a.Revision)
	must(t, err)
	is(t, e.store.RecoverIncident(ctx, e.credentials["infrastructure"], "service", "recovery-blocked", "recover after checks", "recovery-approval", payload["rotation"], payload["integrity"], 1, IncidentRecovery{e.store}), Unavailable)
	incident := load[Incident](t, e.store, "incident", "recovery-blocked")
	if incident.State != "contained" || incident.Revision != 1 || load[map[string]string](t, e.store, "quarantine", e.actors["finance"])["incident"] != incident.ID {
		t.Fatal(incident)
	}
	var hidden Incident
	is(t, e.store.Read(ctx, e.credentials["finance"], "service", "attempt privileged read", "incident", incident.ID, &hidden), Denied)
	if hidden.ID != "" {
		t.Fatal("denied read disclosed incident")
	}
}

func TestPhase3PayoutConnectionLossCannotConfirmSettlement(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	must(t, err)
	endpoint := "https://" + listener.Addr().String()
	must(t, listener.Close())
	provider, err := NewPayoutProvider(endpoint, strings.ReplaceAll(id()+id(), "-", ""), "isolated payout authority", custody, &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}})
	must(t, err)
	result, err := provider.Lookup(context.Background(), "bounty.dependency-loss")
	is(t, err, Unavailable)
	if result.State == "confirmed" || result.Reference != "" {
		t.Fatal(result)
	}
	e := phase2EnvironmentFor(t)
	_, err = NewSettlementWorker(e.store, provider)
	is(t, err, Unavailable)
}
