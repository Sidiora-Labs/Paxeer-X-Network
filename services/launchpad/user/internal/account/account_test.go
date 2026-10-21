package account

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func str(s string) *string { return &s }
func fixture(t *testing.T) Store {
	t.Helper()
	dsn := os.Getenv("L35_TEST_POSTGRES")
	if dsn == "" {
		t.Fatal("L35_TEST_POSTGRES required: tests must exercise real PostgreSQL")
	}
	ctx := context.Background()
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	schema := "kd_d14a27bc53524f4b6a51a940_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = admin.Exec(ctx, `CREATE SCHEMA `+schema); e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		pool.Close()
		if _, e := admin.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`); e != nil {
			t.Error(e)
		}
		admin.Close()
	})
	b, e := os.ReadFile(filepath.Join("..", "..", "..", "..", "spec", "production-system", "handoffs", "L35", "schema.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(b)); e != nil {
		t.Fatal(e)
	}
	return Store{pool, func() time.Time { return time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC) }}
}
func owner(t *testing.T, s Store, n int) Principal {
	t.Helper()
	id := uuid.New()
	if e := s.Create(context.Background(), id, fmt.Sprintf("0x%040x", n)); e != nil {
		t.Fatal(e)
	}
	return Principal{id, s.now()}
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func TestValidationAndCrypto(t *testing.T) {
	for _, b := range []string{`{"expectedRevision":"0","unknown":true}`, `{"expectedRevision":"0"} {}`, string([]byte{0xff}), strings.Repeat(" ", 16385)} {
		var p ProfilePatch
		if Decode(strings.NewReader(b), &p) == nil {
			t.Fatalf("accepted %q", b[:min(len(b), 80)])
		}
	}
	var p ProfilePatch
	must(t, Decode(strings.NewReader(`{"expectedRevision":"0","avatarAssetId":null}`), &p))
	if !p.AvatarAssetID.Set || p.AvatarAssetID.Value != nil {
		t.Fatal("null not preserved")
	}
	p.DisplayName = str(" e\u0301 ")
	must(t, p.Validate())
	if *p.DisplayName != "é" {
		t.Fatal(*p.DisplayName)
	}
	for _, link := range []string{"javascript:alert(1)", "http://host", "https://u:p@host", "https://host/a\n"} {
		if SafeLink(link) == nil {
			t.Fatal(link)
		}
	}
	must(t, SafeLink("https://example.org/a"))
	for _, v := range []PreferencesPatch{{ExpectedRevision: "0", Timezone: str("Mars/Olympus")}, {ExpectedRevision: "0", Locale: str("bad_@@")}} {
		if v.Validate() == nil {
			t.Fatal("invalid preferences")
		}
	}
	key := bytes.Repeat([]byte{7}, 32)
	sealed, e := Seal(key, []byte("provider-token"), []byte("accountA"))
	must(t, e)
	plain, e := Open(key, sealed, []byte("accountA"))
	must(t, e)
	if string(plain) != "provider-token" {
		t.Fatal("roundtrip")
	}
	if _, e = Open(key, sealed, []byte("accountB")); e == nil {
		t.Fatal("cross-owner decrypt")
	}
	sealed[len(sealed)-1] ^= 1
	if _, e = Open(key, sealed, []byte("accountA")); e == nil {
		t.Fatal("tamper accepted")
	}
}
func TestPostgresProfilePreferencesWatchConsent(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	p := owner(t, s, 1)
	other := owner(t, s, 2)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- s.PatchProfile(ctx, p, p.AccountID, ProfilePatch{ExpectedRevision: "0", DisplayName: str("<script>alert(1)</script>")})
		}()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, ErrConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
	pub, e := s.Public(ctx, "0x0000000000000000000000000000000000000001")
	must(t, e)
	b, _ := json.Marshal(pub)
	if !bytes.Contains(b, []byte(`\u003cscript\u003e`)) {
		t.Fatal("markup not escaped", string(b))
	}
	for _, field := range []string{"accountId", "preferences", "session", "watchlist", "email", "endpoint"} {
		if bytes.Contains(b, []byte(field)) {
			t.Fatal("public leak", field)
		}
	}
	must(t, s.PatchPreferences(ctx, p, p.AccountID, PreferencesPatch{ExpectedRevision: pub.Revision, Timezone: str("Europe/Berlin")}))
	pub, e = s.Public(ctx, pub.Wallet)
	must(t, e)
	must(t, s.PatchProfile(ctx, p, p.AccountID, ProfilePatch{ExpectedRevision: pub.Revision, Bio: str("updated")}))
	q, e := s.ReadPreferences(ctx, p, p.AccountID)
	must(t, e)
	if q.Currency != "USD" || q.Timezone != "Europe/Berlin" {
		t.Fatal(q)
	}
	if _, e = s.ReadPreferences(ctx, other, p.AccountID); !errors.Is(e, ErrDenied) {
		t.Fatal("private read", e)
	}
	lookup := func(context.Context, int, string) (bool, error) { return true, nil }
	for _, n := range []int{10, 11, 12, 10} {
		must(t, s.AddWatch(ctx, p, p.AccountID, fmt.Sprintf("0x%040x", n), lookup))
	}
	v, e := s.Watches(ctx, p, p.AccountID, nil, 2)
	must(t, e)
	if len(v) != 2 {
		t.Fatal(v)
	}
	c := WatchCursor{v[1].AddedAt, strings.Replace(v[1].Pool, "b", "B", 1)}
	tail, e := s.Watches(ctx, p, p.AccountID, &c, 2)
	must(t, e)
	if len(tail) != 1 || tail[0].Pool == v[1].Pool {
		t.Fatal(tail)
	}
	if _, e = s.Watches(ctx, other, p.AccountID, nil, 10); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if e = s.AddWatch(ctx, p, p.AccountID, fmt.Sprintf("0x%040x", 99), func(context.Context, int, string) (bool, error) { return false, nil }); !errors.Is(e, ErrMissing) {
		t.Fatal(e)
	}
	pub, e = s.Public(ctx, pub.Wallet)
	must(t, e)
	must(t, s.SetConsent(ctx, p, p.AccountID, pub.Revision, Consent{Category: "watchlist", Channel: "push", Enabled: true, Source: "settings", PolicyVersion: "1"}))
	pub, e = s.Public(ctx, pub.Wallet)
	must(t, e)
	var rev int64
	must(t, s.DB.QueryRow(ctx, `SELECT revision FROM account_state WHERE account_id=$1`, p.AccountID).Scan(&rev))
	ok, e := s.Eligible(ctx, p.AccountID, "watchlist", "push", rev)
	must(t, e)
	if !ok {
		t.Fatal("consent suppressed")
	}
	ok, e = s.Eligible(ctx, p.AccountID, "marketing", "email", rev)
	must(t, e)
	if ok {
		t.Fatal("marketing default")
	}
	must(t, s.SetConsent(ctx, p, p.AccountID, pub.Revision, Consent{Category: "watchlist", Channel: "push", Enabled: false, Source: "unsubscribe", PolicyVersion: "1"}))
	ok, e = s.Eligible(ctx, p.AccountID, "watchlist", "push", rev)
	must(t, e)
	if ok {
		t.Fatal("stale reenrollment")
	}
	var count int
	must(t, s.DB.QueryRow(ctx, `SELECT count(*) FROM account_outbox WHERE account_id=$1`, p.AccountID).Scan(&count))
	if count < 8 {
		t.Fatal("missing transactional events", count)
	}
}
func TestPostgresPrivacyExportDeletionBackfill(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	p := owner(t, s, 3)
	other := owner(t, s, 4)
	key := bytes.Repeat([]byte{9}, 32)
	if e := s.BindEndpoint(ctx, p, "secret-token", "web", "proof", key, nil); e == nil {
		t.Fatal("missing ownership verifier")
	}
	verify := func(_ context.Context, id uuid.UUID, token, proof string, now time.Time) error {
		if proof != "fresh-provider-proof" && proof != "fresh-provider-proof-B" {
			return ErrDenied
		}
		return nil
	}
	must(t, s.BindEndpoint(ctx, p, "secret-token", "web", "fresh-provider-proof", key, verify))
	must(t, s.BindEndpoint(ctx, other, "secret-token", "web", "fresh-provider-proof-B", key, verify))
	var actual uuid.UUID
	var sealed []byte
	must(t, s.DB.QueryRow(ctx, `SELECT account_id,ciphertext FROM account_endpoint`).Scan(&actual, &sealed))
	if actual != other.AccountID || bytes.Contains(sealed, []byte("secret-token")) {
		t.Fatal("endpoint ownership or encryption")
	}
	must(t, s.RevokeEndpoint(ctx, other, "secret-token", key))
	var revoked bool
	must(t, s.DB.QueryRow(ctx, `SELECT revoked FROM account_endpoint`).Scan(&revoked))
	if !revoked {
		t.Fatal("revocation")
	}
	current, e := s.Public(ctx, "0x0000000000000000000000000000000000000003")
	must(t, e)
	must(t, s.PatchProfile(ctx, p, p.AccountID, ProfilePatch{ExpectedRevision: current.Revision, DisplayName: str("private-name")}))
	job, e := s.RequestExport(ctx, p)
	must(t, e)
	snapshot, e := s.ExportSnapshot(ctx, p)
	must(t, e)
	if bytes.Contains(snapshot, []byte("secret-token")) || !bytes.Contains(snapshot, []byte("private-name")) {
		t.Fatal(string(snapshot))
	}
	token, e := s.ReadyExport(ctx, job.ID, "encrypted-object-ref")
	must(t, e)
	if _, e = s.ConsumeExport(ctx, other, job.ID, token); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	_, e = s.ConsumeExport(ctx, p, job.ID, token)
	must(t, e)
	if _, e = s.ConsumeExport(ctx, p, job.ID, token); !errors.Is(e, ErrDenied) {
		t.Fatal("replay", e)
	}
	job2, e := s.RequestExport(ctx, p)
	must(t, e)
	token2, e := s.ReadyExport(ctx, job2.ID, "encrypted-object-ref")
	must(t, e)
	late := s
	late.Clock = func() time.Time { return s.now().Add(16 * time.Minute) }
	if _, e = late.ConsumeExport(ctx, p, job2.ID, token2); !errors.Is(e, ErrDenied) {
		t.Fatal("expired authorization", e)
	}
	if _, e = s.RequestDeletion(ctx, p, "", nil); e == nil {
		t.Fatal("missing session integration")
	}
	revokedSessions := false
	del, e := s.RequestDeletion(ctx, p, "", func(_ context.Context, tx pgx.Tx, id uuid.UUID) error { revokedSessions = true; return nil })
	must(t, e)
	if !revokedSessions {
		t.Fatal("revoke not invoked")
	}
	pub, e := s.Public(ctx, "0x0000000000000000000000000000000000000003")
	must(t, e)
	if pub.DisplayName != "" || pub.Bio != "" {
		t.Fatal("personal text survives")
	}
	if e = s.PatchProfile(ctx, p, p.AccountID, ProfilePatch{ExpectedRevision: "0", DisplayName: str("resurrection")}); !errors.Is(e, ErrState) {
		t.Fatal(e)
	}
	must(t, s.AckDeletion(ctx, del.ID, "images"))
	var state string
	must(t, s.DB.QueryRow(ctx, `SELECT state FROM account_job WHERE job_id=$1`, del.ID).Scan(&state))
	if state == "completed" {
		t.Fatal("early completion")
	}
	must(t, s.AckDeletion(ctx, del.ID, "notifications"))
	must(t, s.DB.QueryRow(ctx, `SELECT state FROM account_job WHERE job_id=$1`, del.ID).Scan(&state))
	if state != "completed" {
		t.Fatal(state)
	}
	id := uuid.New()
	digest := sha256.Sum256([]byte("source"))
	must(t, s.Backfill(ctx, "legacy-1", id, "0x00000000000000000000000000000000000000aB", digest[:]))
	must(t, s.Backfill(ctx, "legacy-1", id, "0x00000000000000000000000000000000000000Ab", digest[:]))
	conflictID := uuid.New()
	must(t, s.Backfill(ctx, "legacy-2", conflictID, "0x00000000000000000000000000000000000000ab", digest[:]))
	must(t, s.DB.QueryRow(ctx, `SELECT state FROM account_backfill WHERE source_key='legacy-2'`).Scan(&state))
	if state != "quarantined" {
		t.Fatal(state)
	}
	must(t, s.Backfill(ctx, "legacy-2", conflictID, "0x00000000000000000000000000000000000000cd", digest[:]))
	var quarantinedCount int
	must(t, s.DB.QueryRow(ctx, `SELECT count(*) FROM account_state WHERE account_id=$1`, conflictID).Scan(&quarantinedCount))
	if quarantinedCount != 0 {
		t.Fatal("quarantine replay linked identity")
	}
	var n int
	must(t, s.DB.QueryRow(ctx, `SELECT count(*) FROM account_state WHERE wallet='0x00000000000000000000000000000000000000ab'`).Scan(&n))
	if n != 1 {
		t.Fatal(n)
	}
}
func TestPostgresIdempotencyRollbackAndLimit(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	p := owner(t, s, 5)
	key := uuid.New()
	calls := 0
	op := func(tx pgx.Tx) (json.RawMessage, error) { calls++; return json.RawMessage(`{"ok":true}`), nil }
	_, e := s.Idempotent(ctx, p, "POST", "/v1/me/exports", key, []byte(`{}`), op)
	must(t, e)
	_, e = s.Idempotent(ctx, p, "POST", "/v1/me/exports", key, []byte(`{}`), op)
	must(t, e)
	if calls != 1 {
		t.Fatal(calls)
	}
	if _, e = s.Idempotent(ctx, p, "POST", "/v1/me/exports", key, []byte(`{"changed":true}`), op); !errors.Is(e, ErrIdempotency) {
		t.Fatal(e)
	}
	_, e = s.DB.Exec(ctx, `INSERT INTO account_watch SELECT $1,125,'0x'||lpad(to_hex(i),40,'0'),$2,0,false FROM generate_series(1,500) i`, p.AccountID, s.now())
	must(t, e)
	if e = s.AddWatch(ctx, p, p.AccountID, "0x000000000000000000000000000000000000ffff", func(context.Context, int, string) (bool, error) { return true, nil }); !errors.Is(e, ErrState) {
		t.Fatal(e)
	}
	var rev int64
	must(t, s.DB.QueryRow(ctx, `SELECT revision FROM account_state WHERE account_id=$1`, p.AccountID).Scan(&rev))
	if rev != 0 {
		t.Fatal("failed mutation changed revision", rev)
	}
}

func TestWireAdaptersAndSignedProof(t *testing.T) {
	key, secret, e := ed25519.GenerateKey(rand.Reader)
	must(t, e)
	id := uuid.New()
	now := time.Now().UTC()
	h := sha256.Sum256([]byte("token"))
	challenge := EndpointChallenge{id.String(), hex.EncodeToString(h[:]), "a-unique-challenge-nonce", now.Add(time.Minute)}
	body, _ := json.Marshal(challenge)
	proof := base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(secret, body))
	verify := SignedEndpointVerifier(key)
	must(t, verify(context.Background(), id, "token", proof, now))
	for _, v := range []struct {
		id    uuid.UUID
		token string
		at    time.Time
	}{{uuid.New(), "token", now}, {id, "other-token", now}, {id, "token", now.Add(2 * time.Minute)}} {
		if verify(context.Background(), v.id, v.token, proof, v.at) == nil {
			t.Fatal("unbound or expired proof accepted")
		}
	}
	for _, pair := range []struct {
		err    error
		status int
		code   string
	}{{ErrConflict, 412, "REVISION_CONFLICT"}, {ErrDenied, 403, "SCOPE_DENIED"}, {ErrIdempotency, 409, "IDEMPOTENCY_CONFLICT"}, {ErrUnknown, 422, "FIELD_UNKNOWN"}, {errors.New("provider secret"), 503, "DEPENDENCY_UNAVAILABLE"}} {
		rec := httptest.NewRecorder()
		WriteError(rec, pair.err, id)
		if rec.Code != pair.status {
			t.Fatal(rec.Code)
		}
		var v WireError
		must(t, json.Unmarshal(rec.Body.Bytes(), &v))
		if v.Code != pair.code || strings.Contains(rec.Body.String(), "provider secret") {
			t.Fatal(v)
		}
	}
	var prefs PreferencesPatch
	must(t, Decode(strings.NewReader(`{"expectedRevision":"0","channels":{"inapp":true,"push":false,"email":false},"quietHours":{"start":"22:00","end":"07:00","timezone":"UTC"}}`), &prefs))
	must(t, prefs.Validate())
	if v := prefs.Channels.Enabled(); len(v) != 1 || v[0] != "inapp" {
		t.Fatal(v)
	}
	for _, body := range []string{`{"expectedRevision":"0","bio":null}`, `{"expectedRevision":"0","displayName":null}`} {
		var p ProfilePatch
		if Decode(strings.NewReader(body), &p) == nil {
			t.Fatal("illegal null accepted")
		}
	}
	env, e := Response(id, map[string]PublicProfile{"profile": {Wallet: "0x0000000000000000000000000000000000000001", Revision: "1"}}, "1", now)
	must(t, e)
	b, _ := json.Marshal(env)
	for _, field := range []string{"requestId", "data", "watermark", "revision", "quality", "nextCursor", "generatedAt"} {
		if !bytes.Contains(b, []byte(`"`+field+`"`)) {
			t.Fatal(field)
		}
	}
	max256 := "115792089237316195423570985008687907853269984665640564039457584007913129639935"
	if !validRevision(max256) || validRevision(max256+"0") {
		t.Fatal("UInt256 boundary")
	}
}
func TestPostgresConflictingSourceRaceAndProofReplay(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	digest := sha256.Sum256([]byte("source"))
	var wg sync.WaitGroup
	results := make(chan error, 2)
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results <- s.Backfill(ctx, "same-source", ids[i], fmt.Sprintf("0x%040x", i+300), digest[:])
		}(i)
	}
	wg.Wait()
	close(results)
	for e := range results {
		must(t, e)
	}
	var count int
	must(t, s.DB.QueryRow(ctx, `SELECT count(*) FROM account_state WHERE account_id=ANY($1)`, ids).Scan(&count))
	if count != 1 {
		t.Fatal("same source created multiple accounts", count)
	}
	var st string
	must(t, s.DB.QueryRow(ctx, `SELECT state FROM account_backfill WHERE source_key='same-source'`).Scan(&st))
	if st != "quarantined" {
		t.Fatal(st)
	}
	p := owner(t, s, 400)
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	must(t, e)
	token := "real-crypto-token"
	h := sha256.Sum256([]byte(token))
	body, _ := json.Marshal(EndpointChallenge{p.AccountID.String(), hex.EncodeToString(h[:]), "unique-provider-nonce", s.now().Add(time.Minute)})
	proof := base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, body))
	key := bytes.Repeat([]byte{3}, 32)
	must(t, s.BindEndpoint(ctx, p, token, "web", proof, key, SignedEndpointVerifier(pub)))
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	alias := proof[:len(proof)-1] + string(alphabet[strings.IndexByte(alphabet, proof[len(proof)-1])+1])
	if e = s.BindEndpoint(ctx, p, token, "web", alias, key, SignedEndpointVerifier(pub)); !errors.Is(e, ErrDenied) {
		t.Fatal("noncanonical signature replay accepted", e)
	}

	if e = s.BindEndpoint(ctx, p, token, "web", proof, key, SignedEndpointVerifier(pub)); !errors.Is(e, ErrDenied) {
		t.Fatal("proof replay", e)
	}
}

func TestPostgresCallerOwnedMutationRollback(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	p := owner(t, s, 900)
	change := func(tx pgx.Tx, a *state) error {
		a.Profile.DisplayName = "atomic-profile"
		return nil
	}
	tx, e := s.DB.Begin(ctx)
	must(t, e)
	must(t, s.mutateTx(ctx, tx, p, p.AccountID, "0", "profile.updated", change))
	must(t, tx.Rollback(ctx))
	var revision int64
	var profile []byte
	must(t, s.DB.QueryRow(ctx, `SELECT revision,profile FROM account_state WHERE account_id=$1`, p.AccountID).Scan(&revision, &profile))
	var count int
	must(t, s.DB.QueryRow(ctx, `SELECT count(*) FROM account_outbox WHERE account_id=$1`, p.AccountID).Scan(&count))
	if revision != 0 || count != 0 || bytes.Contains(profile, []byte("atomic-profile")) {
		t.Fatal(revision, count, string(profile))
	}
	tx, e = s.DB.Begin(ctx)
	must(t, e)
	must(t, s.mutateTx(ctx, tx, p, p.AccountID, "0", "profile.updated", change))
	must(t, tx.Commit(ctx))
	must(t, s.DB.QueryRow(ctx, `SELECT revision,profile FROM account_state WHERE account_id=$1`, p.AccountID).Scan(&revision, &profile))
	must(t, s.DB.QueryRow(ctx, `SELECT count(*) FROM account_outbox WHERE account_id=$1`, p.AccountID).Scan(&count))
	if revision != 1 || count != 1 || !bytes.Contains(profile, []byte("atomic-profile")) {
		t.Fatal(revision, count, string(profile))
	}
}

func TestDeletionFrozenProjection(t *testing.T) {
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	j := Job{uuid.New(), uuid.New(), "deletion", "processing", now}
	data := deletionProjection(j)
	encoded, e := json.Marshal(data)
	must(t, e)
	if data.Job.State != "running" || data.Job.JobID != j.ID.String() || data.Job.RequestedAt != "2026-10-12T00:00:00.000Z" || len(data.Job.DomainResults) != 4 || len(data.RetentionSummary) != 2 || data.Job.DomainResults[2].State != "pending" || data.Job.DomainResults[2].CompletedAt != nil || !data.RetentionSummary[0].IrreversibleChainData {
		t.Fatal(string(encoded))
	}
	var value map[string]json.RawMessage
	must(t, json.Unmarshal(encoded, &value))
	var job map[string]json.RawMessage
	must(t, json.Unmarshal(value["job"], &job))
	for _, key := range []string{"jobId", "accountId", "kind", "state", "requestedAt", "expiresAt", "domainResults", "retainedReasons", "downloadUrl"} {
		if _, ok := job[key]; !ok {
			t.Fatal(key, string(encoded))
		}
	}
}

func TestDeletionGuardErrorClassification(t *testing.T) {
	denied := &pgconn.PgError{Code: "28000", Message: "proof denied"}
	if !errors.Is(deletionGuardError(denied), ErrDenied) {
		t.Fatal("guard denial not classified")
	}
	operational := &pgconn.PgError{Code: "08006", Message: "connection failure"}
	if deletionGuardError(operational) != operational || deletionGuardError(nil) != nil {
		t.Fatal("operational failure was masked")
	}
}
