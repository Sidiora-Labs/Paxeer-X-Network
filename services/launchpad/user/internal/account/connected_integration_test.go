package account

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/internaltest"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
	"github.com/google/uuid"
)

func connectedFixture(t *testing.T) Store {
	t.Helper()
	s := fixture(t)
	b, e := os.ReadFile(filepath.Join("..", "..", "..", "..", "spec", "production-system", "handoffs", "L35", "phase2", "schema.sql"))
	must(t, e)
	_, e = s.DB.Exec(context.Background(), string(b))
	must(t, e)
	identity, e := os.ReadFile(filepath.Join("..", "..", "..", "..", "spec", "production-system", "handoffs", "L33", "schema-proposal.sql"))
	must(t, e)
	_, e = s.DB.Exec(context.Background(), string(identity))
	must(t, e)
	return s
}
func TestConnectedOwnerPreferencesAndConsentExport(t *testing.T) {
	s := connectedFixture(t)
	p := owner(t, s, 5101)
	other := owner(t, s, 5102)
	ctx := context.Background()
	handler := (HTTP{Store: s}).Handler()
	requests := []map[string]any{}
	defer func() {
		b, e := json.Marshal(map[string]any{"requirements": []string{"D11_022", "D11_023", "D11_025"}, "requests": requests, "requestCount": len(requests)})
		must(t, e)
		t.Log("L35_HTTP_EVIDENCE " + string(b))
	}()
	send := func(method, path, body string, p Principal) (int, []byte) {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r = r.WithContext(WithPrincipal(r.Context(), p))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		requests = append(requests, map[string]any{"method": method, "path": path, "status": w.Code})
		return w.Code, w.Body.Bytes()
	}
	status, _ := send(http.MethodPatch, "/v1/me/preferences", `{"expectedRevision":"0","timezone":"Europe/Berlin"}`, p)
	if status != 200 {
		t.Fatal(status)
	}
	status, raw := send(http.MethodPatch, "/v1/me/preferences", `{"expectedRevision":"1","timezone":"Mars/Olympus"}`, p)
	if status != 422 || !bytes.Contains(raw, []byte(`/timezone`)) {
		t.Fatalf("%d %s", status, raw)
	}
	status, _ = send(http.MethodPatch, "/v1/me/preferences", `{"expectedRevision":"1","unregistered":true}`, p)
	if status != 422 {
		t.Fatal(status)
	}
	status, _ = send(http.MethodPatch, "/v1/me/preferences", `{"expectedRevision":"1","locale":"fr"}`, p)
	if status != 200 {
		t.Fatal(status)
	}
	q, e := s.ReadPreferences(ctx, p, p.AccountID)
	must(t, e)
	if q.Timezone != "Europe/Berlin" || q.Locale != "fr" {
		t.Fatal(q)
	}
	must(t, s.PatchExtendedPreferences(ctx, p, ExtendedPreferencesPatch{PreferencesPatch: PreferencesPatch{ExpectedRevision: q.Revision}, Currency: str("EUR"), Consents: []ConsentPatch{{Category: "watchlist", Channel: "push", Enabled: true}, {Category: "marketing", Channel: "email", Enabled: false}}}))
	q, e = s.ReadPreferences(ctx, p, p.AccountID)
	must(t, e)
	if q.Currency != "EUR" || q.Timezone != "Europe/Berlin" {
		t.Fatal(q)
	}
	snapshot, e := s.ExportSnapshot(ctx, p)
	must(t, e)
	var export map[string]json.RawMessage
	must(t, json.Unmarshal(snapshot, &export))
	var consents []struct {
		Category string `json:"category"`
		Channel  string `json:"channel"`
		Enabled  bool   `json:"enabled"`
	}
	must(t, json.Unmarshal(export["consents"], &consents))
	if len(consents) != 2 {
		t.Fatal(string(snapshot))
	}
	for _, c := range consents {
		if c.Category == "watchlist" && !c.Enabled || c.Category == "marketing" && c.Enabled {
			t.Fatal(c)
		}
	}
	if _, e = s.ReadPreferences(ctx, other, p.AccountID); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if bytes.Contains(snapshot, []byte(other.AccountID.String())) || bytes.Contains(snapshot, []byte("sessionId")) || bytes.Contains(snapshot, []byte("ciphertext")) {
		t.Fatal(string(snapshot))
	}
}
func TestConnectedRevisionInboxRejectsReplayAndResurrection(t *testing.T) {
	s := connectedFixture(t)
	p := owner(t, s, 5201)
	ctx := context.Background()
	event := ConsentRevision{EventID: uuid.NewString(), AccountID: p.AccountID, Revision: 3, Category: "watchlist", Channel: "push", Enabled: false, RevokeRevision: 3, At: s.now()}
	must(t, s.ConsumeConsent(ctx, event))
	must(t, s.ConsumeConsent(ctx, event))
	var count int
	must(t, s.DB.QueryRow(ctx, `SELECT count(*) FROM account_revision_inbox`).Scan(&count))
	if count != 1 {
		t.Fatal(count)
	}
	changed := event
	changed.Enabled = true
	if e := s.ConsumeConsent(ctx, changed); !errors.Is(e, delivery.ErrIntegrity) {
		t.Fatal(e)
	}
	stale := event
	stale.EventID = uuid.NewString()
	stale.Revision = 2
	stale.RevokeRevision = 0
	stale.Enabled = true
	must(t, s.ConsumeConsent(ctx, stale))
	ok, e := s.Eligible(ctx, p.AccountID, "watchlist", "push", 3)
	must(t, e)
	if ok {
		t.Fatal("stale import reenrolled owner")
	}
	_, e = s.DB.Exec(ctx, `UPDATE account_state SET deleted=true WHERE account_id=$1`, p.AccountID)
	must(t, e)
	event.EventID = uuid.NewString()
	event.Revision = 4
	event.Enabled = true
	if e = s.ConsumeConsent(ctx, event); !errors.Is(e, ErrState) {
		t.Fatal(e)
	}
}
func TestConnectedInputAndTelemetryBoundaries(t *testing.T) {
	var patch ProfilePatch
	if Decode(strings.NewReader(`{"expectedRevision":"0","bio":"a","bio":"b"}`), &patch) == nil {
		t.Fatal("duplicate field accepted")
	}
	m := &Metrics{}
	m.Observe("profile", "success", time.Millisecond)
	m.Observe("0x-private-wallet", "secret-token", time.Hour)
	s := m.Snapshot()
	if len(s) != 1 || s["profile.success"].Count != 1 {
		t.Fatal(s)
	}
	for _, v := range []DeviceBody{{Platform: "web", Provider: "apns", Token: "token", DeviceProof: "proof"}, {Platform: "android", Provider: "fcm", Token: "token", DeviceProof: strings.Repeat("x", 257)}} {
		if v.Validate() == nil {
			t.Fatal("invalid endpoint accepted")
		}
	}
}

func TestConnectedEncryptedExportWorkerAndSingleUseDownload(t *testing.T) {
	s := connectedFixture(t)
	ctx := context.Background()
	now := s.now()
	s.Clock = func() time.Time { return now }
	p := owner(t, s, 5301)
	other := owner(t, s, 5302)
	required := []string{"L35_TEST_OBJECT_ENDPOINT", "L35_TEST_OBJECT_ACCESS_KEY", "L35_TEST_OBJECT_SECRET_KEY", "L35_TEST_OBJECT_BUCKET"}
	for _, name := range required {
		if os.Getenv(name) == "" {
			t.Fatalf("%s required: encrypted export must use real object storage", name)
		}
	}
	objects, e := storage.New(ctx, storage.Config{Endpoint: os.Getenv(required[0]), AccessKeyID: os.Getenv(required[1]), SecretAccessKey: os.Getenv(required[2]), Bucket: os.Getenv(required[3]), Region: "us-east-1"})
	must(t, e)
	key := make([]byte, 32)
	_, e = rand.Read(key)
	must(t, e)
	runtime := &Runtime{Objects: objects, ExportKey: key, DownloadBaseURL: "https://accounts.test", Metrics: &Metrics{}}
	worker := Worker{Store: s, Runtime: runtime, Owner: "real-export-worker", LeaseDuration: time.Minute}
	job, e := s.RequestExport(ctx, p)
	must(t, e)
	runtime.Objects = nil
	if e = worker.RunOnce(ctx); e == nil {
		t.Fatal("unavailable export dependency acknowledged")
	}
	var failedID uuid.UUID
	var failedState string
	must(t, s.DB.QueryRow(ctx, `SELECT job_id,state FROM account_job WHERE account_id=$1`, p.AccountID).Scan(&failedID, &failedState))
	if failedID != job.ID || failedState != "failed" {
		t.Fatal("failure replaced durable export job", failedID, failedState)
	}
	runtime.Objects = objects
	must(t, worker.RunOnce(ctx))
	var payload []byte
	var state string
	must(t, s.DB.QueryRow(ctx, `SELECT state,payload FROM account_job WHERE job_id=$1`, job.ID).Scan(&state, &payload))
	if state != "ready" {
		t.Fatal(state)
	}
	var metadata exportMetadata
	must(t, json.Unmarshal(payload, &metadata))
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := objects.Delete(cleanup, metadata.EncryptedObject); e != nil {
			t.Error(e)
		}
	})
	sealed, _, e := objects.GetBytes(ctx, metadata.EncryptedObject)
	must(t, e)
	if bytes.Contains(sealed, []byte(`"preferences"`)) {
		t.Fatal("plaintext snapshot stored")
	}
	plain, e := Open(key, sealed, []byte(job.ID.String()))
	must(t, e)
	if !bytes.Contains(plain, []byte(`"preferences"`)) || bytes.Contains(plain, []byte(other.AccountID.String())) {
		t.Fatal(string(plain))
	}
	token, e := Open(key, metadata.Authorization, []byte(job.ID.String()))
	must(t, e)
	if _, e = s.ConsumeExport(ctx, other, job.ID, string(token)); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	_, e = s.ConsumeExport(ctx, p, job.ID, string(token))
	must(t, e)
	if _, e = s.ConsumeExport(ctx, p, job.ID, string(token)); !errors.Is(e, ErrDenied) {
		t.Fatal("download token replay", e)
	}
	now = now.Add(25 * time.Hour)
	must(t, worker.PurgeExpiredExports(ctx, 100))
	if _, _, e = objects.GetBytes(ctx, metadata.EncryptedObject); !errors.Is(e, storage.ErrNotFound) {
		t.Fatal("retained encrypted object", e)
	}
}

func TestConnectedFrozenProfileAndVersionedExtensions(t *testing.T) {
	s := connectedFixture(t)
	p := owner(t, s, 5401)
	var v ExtendedProfilePatch
	must(t, DecodeExtendedProfile(strings.NewReader(`{"expectedRevision":"0","displayName":" é ","website":"https://example.org","socials":{"twitter":"https://example.org/profile"}}`), &v))
	must(t, s.PatchExtendedProfile(context.Background(), p, v))
	profile, e := s.Public(context.Background(), fmt.Sprintf("0x%040x", 5401))
	must(t, e)
	if profile.DisplayName != "é" || profile.Website == nil || *profile.Website != "https://example.org" {
		t.Fatal(profile)
	}
	var frozen ProfilePatch
	if Decode(strings.NewReader(`{"expectedRevision":"1","website":"https://example.org"}`), &frozen) == nil {
		t.Fatal("frozen v1 widened")
	}
	var invalid ExtendedProfilePatch
	for _, body := range []string{`{"expectedRevision":"1","website":"javascript:alert(1)"}`, `{"expectedRevision":"1","socials":{"unknown":"https://example.org"}}`, `{"expectedRevision":"1","bio":null}`} {
		if DecodeExtendedProfile(strings.NewReader(body), &invalid) == nil {
			t.Fatalf("invalid extended profile accepted: %s", body)
		}
	}
	must(t, DecodeExtendedProfile(strings.NewReader(`{"expectedRevision":"1","website":null,"socials":{"twitter":null}}`), &v))
	must(t, s.PatchExtendedProfile(context.Background(), p, v))
	profile, e = s.Public(context.Background(), profile.Wallet)
	must(t, e)
	if profile.Website != nil || profile.DisplayName != "é" {
		t.Fatal(profile)
	}
}
func TestConnectedGatewayRejectsForgedAssertion(t *testing.T) {
	s := connectedFixture(t)
	p := owner(t, s, 5501)
	handler := (HTTP{Store: s}).GatewayHandler(sharedauth.ServiceAssertionVerifier{Transport: sharedauth.AssertionTransport{AssertionHeader: "X-Test-Assertion", SignatureHeader: "X-Test-Signature"}}, sharedauth.PostgresIdentityResolver{Pool: s.DB}, sharedauth.OperationAuthorizer{})
	r := httptest.NewRequest(http.MethodPatch, "/v1/me/preferences", strings.NewReader(`{"expectedRevision":"0","locale":"fr"}`))
	r.Header.Set("X-Test-Assertion", "forged")
	r.Header.Set("X-Test-Signature", "forged")
	r = r.WithContext(WithPrincipal(r.Context(), p))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("forged context/header trusted: %d", w.Code)
	}
	prefs, e := s.ReadPreferences(context.Background(), p, p.AccountID)
	must(t, e)
	if prefs.Locale != "en" || prefs.Revision != "0" {
		t.Fatal(prefs)
	}
}

func TestConnectedDeviceProofReassignmentAndRevocation(t *testing.T) {
	s := connectedFixture(t)
	p := owner(t, s, 5601)
	other := owner(t, s, 5602)
	ctx := context.Background()
	schema := s.DB.Config().ConnConfig.RuntimeParams["search_path"]
	ddl, e := delivery.SchemaDDL(schema)
	must(t, e)
	_, e = s.DB.Exec(ctx, ddl)
	must(t, e)
	publisher, e := delivery.NewStore(s.DB, schema, delivery.Policy{Subjects: map[string]bool{"users.endpoint.bound": true, "users.endpoint.revoked": true}, EventTypes: map[string]bool{"endpoint.bound": true, "endpoint.revoked": true}})
	must(t, e)
	public, private, e := ed25519.GenerateKey(rand.Reader)
	must(t, e)
	key := make([]byte, 32)
	_, e = rand.Read(key)
	must(t, e)
	fingerprintKey := make([]byte, 32)
	_, e = rand.Read(fingerprintKey)
	must(t, e)
	runtime := &Runtime{EndpointKey: key, EndpointFingerprintKey: fingerprintKey, DeviceVerifier: CompactDeviceProofVerifier(public), Delivery: publisher, Subjects: map[string]string{"endpoint.bound": "users.endpoint.bound", "endpoint.revoked": "users.endpoint.revoked"}, Metrics: &Metrics{}}
	handler := (HTTP{Store: s, Runtime: runtime}).Handler()
	token := "real-provider-proof-bound-token"
	sessions := map[uuid.UUID]uuid.UUID{p.AccountID: uuid.New(), other.AccountID: uuid.New()}
	wallets := map[uuid.UUID]string{p.AccountID: fmt.Sprintf("0x%040x", 5601), other.AccountID: fmt.Sprintf("0x%040x", 5602)}
	for _, owner := range []Principal{p, other} {
		seedConnectedIdentity(t, s, owner, sessions[owner.AccountID], wallets[owner.AccountID])
	}
	proof := func(owner Principal, device uuid.UUID) string {
		wire := make([]byte, 107)
		wire[0] = 2
		copy(wire[1:17], owner.AccountID[:])
		session := sessions[owner.AccountID]
		copy(wire[17:33], session[:])
		copy(wire[33:49], device[:])
		digest := sha256.Sum256([]byte(token))
		copy(wire[49:81], digest[:])
		_, e := rand.Read(wire[81:97])
		must(t, e)
		binary.BigEndian.PutUint64(wire[97:105], uint64(s.now().Add(time.Minute).Unix()))
		wire[105], wire[106] = 1, 1
		return base64.RawURLEncoding.EncodeToString(append(wire, ed25519.Sign(private, wire)...))
	}
	send := func(method string, owner Principal, device uuid.UUID, body any) (int, []byte) {
		var raw []byte
		if body != nil {
			raw, e = json.Marshal(body)
			must(t, e)
		}
		r := httptest.NewRequest(method, "/v1/me/devices/"+device.String(), bytes.NewReader(raw))
		identity := sharedauth.IdentityPrincipal{AccountID: owner.AccountID.String(), SessionID: sessions[owner.AccountID].String(), Wallet: wallets[owner.AccountID], ProofAt: owner.ProofAt, AccessExpiresAt: time.Now().Add(time.Hour)}
		r = r.WithContext(context.WithValue(WithPrincipal(r.Context(), owner), identityContextKey{}, identity))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code, w.Body.Bytes()
	}
	device := uuid.New()
	body := DeviceBody{Platform: "web", Provider: "webpush", Token: token, DeviceProof: proof(p, device)}
	status, response := send(http.MethodPut, p, device, body)
	if status != 200 {
		t.Fatalf("%d %s", status, response)
	}
	if bytes.Contains(response, []byte(token)) || bytes.Contains(response, []byte("ciphertext")) {
		t.Fatal(string(response))
	}
	status, _ = send(http.MethodPut, p, device, body)
	if status != 403 {
		t.Fatal("proof replay accepted", status)
	}
	nextDevice := uuid.New()
	status, _ = send(http.MethodPut, other, nextDevice, body)
	if status != 403 {
		t.Fatal("cross-account device proof accepted", status)
	}
	body.DeviceProof = proof(other, nextDevice)
	status, response = send(http.MethodPut, other, nextDevice, body)
	if status != 200 {
		t.Fatalf("%d %s", status, response)
	}
	old, e := s.Devices(ctx, p)
	must(t, e)
	if len(old) != 0 {
		t.Fatal("previous binding retained", old)
	}
	q, e := s.ReadPreferences(ctx, other, other.AccountID)
	must(t, e)
	must(t, s.SetConsent(ctx, other, other.AccountID, q.Revision, Consent{Category: "watchlist", Channel: "push", Enabled: true, Source: "settings", PolicyVersion: "1"}))
	var revision int64
	must(t, s.DB.QueryRow(ctx, `SELECT revision FROM account_state WHERE account_id=$1`, other.AccountID).Scan(&revision))
	eligible, e := s.EndpointEligible(ctx, other.AccountID, nextDevice, "watchlist", "push", revision)
	must(t, e)
	if !eligible {
		t.Fatal("current endpoint ineligible")
	}
	status, response = send(http.MethodDelete, other, nextDevice, nil)
	if status != 204 {
		t.Fatalf("%d %s", status, response)
	}
	eligible, e = s.EndpointEligible(ctx, other.AccountID, nextDevice, "watchlist", "push", revision)
	must(t, e)
	if eligible {
		t.Fatal("revoked endpoint eligible")
	}
}

func seedConnectedIdentity(t *testing.T, s Store, p Principal, session uuid.UUID, wallet string) {
	t.Helper()
	ctx := context.Background()
	_, e := s.DB.Exec(ctx, `INSERT INTO identity_accounts VALUES($1,0,clock_timestamp());`, p.AccountID)
	must(t, e)
	_, e = s.DB.Exec(ctx, `INSERT INTO identity_wallet_links VALUES($1,$2,125,$3,'private','verified',clock_timestamp())`, uuid.New(), p.AccountID, wallet)
	must(t, e)
	access, csrf := make([]byte, 32), make([]byte, 32)
	_, e = rand.Read(access)
	must(t, e)
	_, e = rand.Read(csrf)
	must(t, e)
	_, e = s.DB.Exec(ctx, `INSERT INTO identity_sessions VALUES($1,$2,$3,0,$4,$5,clock_timestamp(),clock_timestamp()+interval '1 hour',clock_timestamp()+interval '1 day',clock_timestamp(),clock_timestamp(),'connected-real-device',NULL)`, session, p.AccountID, wallet, access, csrf)
	must(t, e)
}

func TestConnectedPhase3ConcurrentReplayAndDatabaseInterruption(t *testing.T) {
	s := connectedFixture(t)
	ctx := context.Background()
	p := owner(t, s, 5701)
	key := uuid.New()
	body := []byte(`{"expectedRevision":"0","displayName":"durable"}`)
	mutate := func(tx pgx.Tx) (json.RawMessage, error) {
		c := context.WithValue(ctx, accountTransactionKey{}, tx)
		if e := s.PatchProfile(c, p, p.AccountID, ProfilePatch{ExpectedRevision: "0", DisplayName: str("durable")}); e != nil {
			return nil, e
		}
		return json.RawMessage(`{"revision":"1"}`), nil
	}
	results := make(chan error, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, e := s.Idempotent(ctx, p, "PATCH", "/v1/profiles/owner", key, body, mutate)
			if e == nil && !bytes.Equal(result, []byte(`{"revision":"1"}`)) {
				e = errors.New("replay response changed")
			}
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	for e := range results {
		must(t, e)
	}
	var revision, events, records int64
	must(t, s.DB.QueryRow(ctx, `SELECT revision,(SELECT count(*) FROM account_outbox WHERE account_id=$1),(SELECT count(*) FROM account_idempotency WHERE account_id=$1) FROM account_state WHERE account_id=$1`, p.AccountID).Scan(&revision, &events, &records))
	if revision != 1 || events != 1 || records != 1 {
		t.Fatal(revision, events, records)
	}
	_, e := s.Idempotent(ctx, p, "PATCH", "/v1/profiles/owner", key, []byte(`{"different":true}`), mutate)
	if !errors.Is(e, ErrIdempotency) {
		t.Fatal("changed replay accepted", e)
	}
	conn, e := s.DB.Acquire(ctx)
	must(t, e)
	tx, e := conn.Begin(ctx)
	must(t, e)
	c := context.WithValue(ctx, accountTransactionKey{}, tx)
	must(t, s.PatchProfile(c, p, p.AccountID, ProfilePatch{ExpectedRevision: "1", Bio: str("unacknowledged-secret-text")}))
	var pid int32
	must(t, tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	var killed bool
	must(t, s.DB.QueryRow(ctx, `SELECT pg_terminate_backend($1)`, pid).Scan(&killed))
	if !killed {
		t.Fatal("database interruption did not occur")
	}
	if e = tx.Commit(ctx); e == nil {
		t.Fatal("interrupted commit acknowledged")
	}
	conn.Release()
	profile, e := s.Public(ctx, fmt.Sprintf("0x%040x", 5701))
	must(t, e)
	if profile.Revision != "1" || profile.DisplayName != "durable" || profile.Bio != "" {
		t.Fatal("acknowledged write lost or interrupted mutation escaped", profile)
	}
	must(t, s.PatchProfile(ctx, p, p.AccountID, ProfilePatch{ExpectedRevision: "1", Bio: str("recovered")}))
	must(t, s.DB.QueryRow(ctx, `SELECT count(*) FROM account_outbox WHERE account_id=$1`, p.AccountID).Scan(&events))
	if events != 2 {
		t.Fatal("interrupted outbox persisted", events)
	}
	t.Log(`L35_PHASE3_EVIDENCE {"requirements":["D11_019","D11_030"],"concurrentReplayCount":16,"interruption":"owned_postgres_backend_terminated"}`)
}

func TestConnectedPhase3ConsentConcurrentReplayAndPrivacy(t *testing.T) {
	s := connectedFixture(t)
	ctx := context.Background()
	p := owner(t, s, 5801)
	other := owner(t, s, 5802)
	must(t, s.PatchProfile(ctx, other, other.AccountID, ProfilePatch{ExpectedRevision: "0", Bio: str("foreign-private-canary")}))
	event := ConsentRevision{EventID: uuid.NewString(), AccountID: p.AccountID, Revision: 4, Category: "watchlist", Channel: "push", Enabled: true, At: s.now()}
	results := make(chan error, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.ConsumeConsent(ctx, event) }()
	}
	wg.Wait()
	close(results)
	for e := range results {
		must(t, e)
	}
	marketing := event
	marketing.EventID, marketing.Category, marketing.Channel, marketing.Enabled = uuid.NewString(), "marketing", "email", false
	marketing.Revision, marketing.RevokeRevision = 5, 5
	must(t, s.ConsumeConsent(ctx, marketing))
	eligible, e := s.Eligible(ctx, p.AccountID, "watchlist", "push", 4)
	must(t, e)
	if !eligible {
		t.Fatal("independent marketing opt-out suppressed watchlist")
	}
	eligible, e = s.Eligible(ctx, p.AccountID, "marketing", "email", 5)
	must(t, e)
	if eligible {
		t.Fatal("marketing opt-out lost")
	}
	stale := marketing
	stale.EventID, stale.Revision, stale.RevokeRevision, stale.Enabled = uuid.NewString(), 4, 0, true
	must(t, s.ConsumeConsent(ctx, stale))
	snapshot, e := s.ExportSnapshot(ctx, p)
	must(t, e)
	for _, secret := range []string{"foreign-private-canary", other.AccountID.String(), "access_hash", "csrf_hash", "sessionId", "ciphertext"} {
		if bytes.Contains(snapshot, []byte(secret)) {
			t.Fatal("private export leak", secret)
		}
	}
	metrics := &Metrics{}
	for _, secret := range []string{"foreign-private-canary", "0x-private-wallet", "provider-token", "nonce", "signature"} {
		metrics.Observe(secret, "success", time.Millisecond)
		metrics.Observe("profile", secret, time.Millisecond)
	}
	metrics.ObserveHTTP("/v1/profiles/0x-private-wallet", 412, time.Millisecond)
	raw, e := json.Marshal(metrics.Snapshot())
	must(t, e)
	if string(raw) == "{}" || bytes.Contains(raw, []byte("private")) || bytes.Contains(raw, []byte("provider")) {
		t.Fatal("unbounded metric fields", string(raw))
	}
	status, wire := PublicError(errors.New("database password=private-canary provider-token"), uuid.New())
	if status != 503 || wire.Message != "DEPENDENCY_UNAVAILABLE" || wire.Details != nil {
		t.Fatal("dependency diagnostic exposed", wire)
	}
	t.Log(`L35_PHASE3_EVIDENCE {"requirements":["D11_023","D11_025"],"concurrentReplayCount":16}`)
}

func TestConnectedPhase3DeviceIdentityRevocation(t *testing.T) {
	s := connectedFixture(t)
	ctx := context.Background()
	p := owner(t, s, 5901)
	session, device := uuid.New(), uuid.New()
	seedConnectedIdentity(t, s, p, session, fmt.Sprintf("0x%040x", 5901))
	must(t, s.SetConsent(ctx, p, p.AccountID, "0", Consent{Category: "watchlist", Channel: "push", Enabled: true, Source: "settings", PolicyVersion: "1"}))
	_, e := s.DB.Exec(ctx, `INSERT INTO account_device_installation VALUES($1,$2,$3,'webpush','web',$4,false)`, device, p.AccountID, session, s.now())
	must(t, e)
	sealed, e := Seal(bytes.Repeat([]byte{7}, 32), []byte("private-provider-token"), []byte(p.AccountID.String()))
	must(t, e)
	_, e = s.DB.Exec(ctx, `INSERT INTO account_endpoint(fingerprint,account_id,ciphertext,platform,consent_revision,validated_at,revoked,device_id,provider,session_id) VALUES($1,$2,$3,'web',1,$4,false,$5,'webpush',$6)`, bytes.Repeat([]byte{9}, 32), p.AccountID, sealed, s.now(), device, session)
	must(t, e)
	eligible, e := s.EndpointEligible(ctx, p.AccountID, device, "watchlist", "push", 1)
	must(t, e)
	if !eligible {
		t.Fatal("live session endpoint denied")
	}
	_, e = s.DB.Exec(ctx, `UPDATE identity_sessions SET revoked_at=clock_timestamp() WHERE session_id=$1`, session)
	must(t, e)
	eligible, e = s.EndpointEligible(ctx, p.AccountID, device, "watchlist", "push", 1)
	must(t, e)
	if eligible {
		t.Fatal("revoked identity dispatch eligible")
	}
	resolver := sharedauth.PostgresIdentityResolver{Pool: s.DB}
	_, e = resolver.Resolve(ctx, sharedauth.Principal{Wallet: fmt.Sprintf("0x%040x", 5901), SessionID: session.String(), Roles: []string{"owner"}})
	if e == nil {
		t.Fatal("revoked identity resolved")
	}
	_, e = s.DB.Exec(ctx, `UPDATE identity_sessions SET revoked_at=NULL WHERE session_id=$1;`, session)
	must(t, e)
	_, e = s.DB.Exec(ctx, `UPDATE identity_accounts SET auth_epoch=auth_epoch+1 WHERE account_id=$1`, p.AccountID)
	must(t, e)
	eligible, e = s.EndpointEligible(ctx, p.AccountID, device, "watchlist", "push", 1)
	must(t, e)
	if eligible {
		t.Fatal("stale auth epoch dispatch eligible")
	}
	t.Log(`L35_PHASE3_EVIDENCE {"requirements":["D11_024","D11_026"]}`)
}

func TestConnectedPhase3OwnedHTTPProfileAndWatchlist(t *testing.T) {
	TestPostgresHTTPFrozenProfile(t)
	TestPostgresHTTPFrozenWatchlist(t)
	t.Log(`L35_PHASE3_EVIDENCE {"requirements":["D11_019","D11_020","D11_021"],"limitations":["owner HTTP adapters exercised; external gateway proof is a separate connected gate"]}`)
}

func connectedLegacyFixture(t *testing.T) Store {
	t.Helper()
	pool := internaltest.NewPostgres(t)
	ctx := context.Background()
	for _, relative := range []string{"L35/schema.sql", "L35/phase2/schema.sql", "L33/schema-proposal.sql"} {
		ddl, e := os.ReadFile(filepath.Join("..", "..", "..", "..", "spec", "production-system", "handoffs", filepath.FromSlash(relative)))
		must(t, e)
		_, e = pool.Exec(ctx, string(ddl))
		must(t, e)
	}
	return Store{DB: pool, Clock: func() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }}
}

func TestConnectedPhase3LegacyReplayRollbackAndQuarantine(t *testing.T) {
	s := connectedLegacyFixture(t)
	ctx := context.Background()
	p := owner(t, s, 0xabc)
	session := uuid.New()
	wallet := fmt.Sprintf("0x%040x", 0xabc)
	seedConnectedIdentity(t, s, p, session, wallet)
	_, e := s.DB.Exec(ctx, `INSERT INTO users.user_profiles(wallet_address,display_name,bio,created_at,updated_at) VALUES($1,'Legacy','old',1,1)`, strings.ReplaceAll(wallet, "abc", "ABC"))
	must(t, e)
	_, e = s.DB.Exec(ctx, `INSERT INTO users.watchlists(wallet_address,pool_address,added_at) VALUES($1,$2,1),($1,$3,1)`, strings.ReplaceAll(wallet, "abc", "ABC"), fmt.Sprintf("0x%040x", 0xdef), strings.ReplaceAll(fmt.Sprintf("0x%040x", 0xdef), "def", "DEF"))
	must(t, e)
	r := BackfillRange{ID: uuid.NewString(), Limit: 100}
	first, e := s.BackfillLegacyRange(ctx, r)
	must(t, e)
	second, e := s.BackfillLegacyRange(ctx, r)
	must(t, e)
	if first != second || first.Linked != 1 || first.Quarantined != 0 || first.WatchlistCount != 1 || first.Checksum == "" {
		t.Fatal(first, second)
	}
	var accounts, watches int
	must(t, s.DB.QueryRow(ctx, `SELECT count(*) FROM account_state WHERE wallet=$1`, wallet).Scan(&accounts))
	must(t, s.DB.QueryRow(ctx, `SELECT count(*) FROM account_watch WHERE account_id=$1`, p.AccountID).Scan(&watches))
	if accounts != 1 || watches != 1 {
		t.Fatal(accounts, watches)
	}
	must(t, s.PatchProfile(ctx, p, p.AccountID, ProfilePatch{ExpectedRevision: "0", DisplayName: str("New stable record")}))
	must(t, s.SetStableAccountReads(ctx, false))
	legacy, e := s.PublicFromReadSwitch(ctx, wallet)
	must(t, e)
	if legacy.Source != "legacy" || legacy.Profile.DisplayName != "Legacy" {
		t.Fatal(legacy)
	}
	must(t, s.SetStableAccountReads(ctx, true))
	current, e := s.PublicFromReadSwitch(ctx, wallet)
	must(t, e)
	if current.Profile.DisplayName != "New stable record" || current.Generation <= legacy.Generation {
		t.Fatal("rollback erased stable write", current)
	}
	_, e = s.DB.Exec(ctx, `INSERT INTO users.user_profiles(wallet_address,display_name,bio,created_at,updated_at) VALUES($1,'Conflicting','different',1,1)`, wallet)
	must(t, e)
	third, e := s.BackfillLegacyRange(ctx, BackfillRange{ID: uuid.NewString(), Limit: 100})
	must(t, e)
	if third.Quarantined != 1 || third.Linked != 0 {
		t.Fatal("mixed-case conflicting identities merged", third)
	}
	current, e = s.PublicFromReadSwitch(ctx, wallet)
	must(t, e)
	if current.Profile.DisplayName != "New stable record" {
		t.Fatal("quarantine changed business record", current)
	}
	t.Log(`L35_PHASE3_EVIDENCE {"requirements":["D11_030"],"providers":["real_postgres"],"scenarios":["mixed_case_backfill_replay","rollback_preserves_stable_write","conflict_quarantine"]}`)
}

func TestConnectedPhase3DeletionRetryAndImmutableBoundary(t *testing.T) {
	s := connectedLegacyFixture(t)
	ctx := context.Background()
	p := owner(t, s, 6101)
	session := uuid.New()
	wallet := fmt.Sprintf("0x%040x", 6101)
	seedConnectedIdentity(t, s, p, session, wallet)
	objects := internaltest.NewStore(t, "phase3-account-images")
	exports := internaltest.NewStore(t, "phase3-account-exports")
	imageKey := "user-images/" + p.AccountID.String() + "/avatar.png"
	image, e := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aBf8AAAAASUVORK5CYII=")
	must(t, e)
	must(t, objects.Put(ctx, imageKey, bytes.NewReader(image), int64(len(image)), "image/png"))
	_, e = s.DB.Exec(ctx, `INSERT INTO users.user_profiles(wallet_address,display_name,bio,created_at,updated_at) VALUES($1,'Private legacy name','private legacy bio',1,1)`, wallet)
	must(t, e)
	_, e = s.DB.Exec(ctx, `INSERT INTO users.user_images(id,wallet_address,image_type,storage_key,mime_type,size_bytes,uploaded_at) VALUES($1,$2,'avatar',$3,'image/png',$4,1)`, uuid.NewString(), wallet, imageKey, len(image))
	must(t, e)
	pool := fmt.Sprintf("0x%040x", 6102)
	internaltest.SeedPool(t, s.DB, pool, fmt.Sprintf("0x%040x", 6103), wallet, 1)
	lookup := func(ctx context.Context, chain int, address string) (bool, error) {
		if chain != 125 {
			return false, ErrInvalid
		}
		var present bool
		e := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM indexer.pools WHERE pool_address=$1)`, address).Scan(&present)
		return present, e
	}
	must(t, s.PutWatch(ctx, p, p.AccountID, pool, true, lookup))
	must(t, s.PatchProfile(ctx, p, p.AccountID, ProfilePatch{ExpectedRevision: "1", DisplayName: str("Private stable name"), Bio: str("private stable bio")}))
	must(t, s.SetConsent(ctx, p, p.AccountID, "2", Consent{Category: "watchlist", Channel: "push", Enabled: true, Source: "settings", PolicyVersion: "1"}))
	del, e := s.RequestDeletion(ctx, p, "3", func(c context.Context, tx pgx.Tx, id uuid.UUID) error {
		return sharedauth.RevokeIdentityAccountTx(c, tx, id.String(), "account_deleted")
	})
	must(t, e)
	var revoked bool
	must(t, s.DB.QueryRow(ctx, `SELECT revoked_at IS NOT NULL FROM identity_sessions WHERE session_id=$1`, session).Scan(&revoked))
	if !revoked {
		t.Fatal("sessions not revoked")
	}
	public, e := s.Public(ctx, wallet)
	must(t, e)
	if public.DisplayName != "" || public.Bio != "" || public.AvatarAssetID != nil {
		t.Fatal("profile not purged", public)
	}
	var privateRows int
	must(t, s.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM account_watch WHERE account_id=$1)+(SELECT count(*) FROM account_consent WHERE account_id=$1)+(SELECT count(*) FROM account_endpoint WHERE account_id=$1)`, p.AccountID).Scan(&privateRows))
	if privateRows != 0 {
		t.Fatal("private rows retained", privateRows)
	}
	purger := ImagePurger{Store: s, Objects: objects, Exports: exports}
	worker := Worker{Store: s, Runtime: &Runtime{DeletionOwners: map[string]DeletionOwner{"images": purger}}, Owner: "phase3-deletion-worker", LeaseDuration: time.Minute}
	if e = worker.RunOnce(ctx); e == nil {
		t.Fatal("missing downstream owner acknowledged")
	}
	var state string
	var payload []byte
	must(t, s.DB.QueryRow(ctx, `SELECT state,payload FROM account_job WHERE job_id=$1`, del.ID).Scan(&state, &payload))
	var progress struct {
		Acks []string `json:"acks"`
	}
	must(t, json.Unmarshal(payload, &progress))
	seen := map[string]bool{}
	for _, ack := range progress.Acks {
		seen[ack] = true
	}
	if state != "failed" || !seen["user"] || !seen["sessions"] || !seen["images"] || seen["notifications"] {
		t.Fatal("partial owner acknowledgments were not durable", state)
	}
	if _, _, e = objects.GetBytes(ctx, imageKey); !errors.Is(e, storage.ErrNotFound) {
		t.Fatal("image survives acknowledged image owner", e)
	}
	if e = worker.RunOnce(ctx); e == nil {
		t.Fatal("retry silently acknowledged missing downstream")
	}
	must(t, s.DB.QueryRow(ctx, `SELECT state FROM account_job WHERE job_id=$1`, del.ID).Scan(&state))
	if state == "completed" {
		t.Fatal("partial deletion completed")
	}
	var creator string
	must(t, s.DB.QueryRow(ctx, `SELECT creator FROM indexer.pools WHERE pool_address=$1`, pool).Scan(&creator))
	if creator != wallet {
		t.Fatal("immutable creator fact removed", creator)
	}
	event := ConsentRevision{EventID: uuid.NewString(), AccountID: p.AccountID, Revision: 100, Category: "watchlist", Channel: "push", Enabled: true, At: s.now()}
	if e = s.ConsumeConsent(ctx, event); !errors.Is(e, ErrState) {
		t.Fatal("historical replay resurrected personal state", e)
	}
	must(t, s.SetStableAccountReads(ctx, false))
	projection, e := s.PublicFromReadSwitch(ctx, wallet)
	must(t, e)
	if projection.Source != "stable" || projection.Profile.DisplayName != "" {
		t.Fatal("rollback resurrected tombstone", projection)
	}
	t.Log(`L35_PHASE3_EVIDENCE {"requirements":["D11_026","D11_027"],"providers":["real_postgres","real_object_storage"],"limitations":["notification owner missing: deletion must remain retryable"]}`)
}

func TestConnectedPhase3LiveSocketRevocationAndHTTPWrites(t *testing.T) {
	values := map[string]string{}
	for _, name := range []string{"L35_PHASE3_GATEWAY_URL", "L35_PHASE3_STREAM_URL", "L35_PHASE3_ORIGIN", "L35_PHASE3_ACCESS_TOKEN", "L35_PHASE3_WALLET_PRIVATE_KEY", "L35_PHASE3_OPERATION_ENVIRONMENT"} {
		values[name] = os.Getenv(name)
		if values[name] == "" {
			t.Fatalf("%s required: admitted live gateway, identity and private stream fixture unavailable", name)
		}
	}
	base, e := url.Parse(values["L35_PHASE3_GATEWAY_URL"])
	must(t, e)
	stream, e := url.Parse(values["L35_PHASE3_STREAM_URL"])
	must(t, e)
	if base.User != nil || base.RawQuery != "" || base.Fragment != "" || base.Path != "" || (base.Scheme != "https" && !(base.Scheme == "http" && (base.Hostname() == "127.0.0.1" || base.Hostname() == "localhost"))) {
		t.Fatal("admitted gateway URL must be an HTTPS or loopback origin")
	}
	if stream.User != nil || stream.RawQuery != "" || stream.Fragment != "" || stream.Path != "/v2/stream" || (stream.Scheme != "wss" && !(stream.Scheme == "ws" && (stream.Hostname() == "127.0.0.1" || stream.Hostname() == "localhost"))) {
		t.Fatal("admitted stream URL must target the actual v2 stream boundary")
	}
	private, e := crypto.HexToECDSA(strings.TrimPrefix(values["L35_PHASE3_WALLET_PRIVATE_KEY"], "0x"))
	if e != nil {
		t.Fatal("invalid admitted ephemeral wallet key")
	}
	wallet := strings.ToLower(crypto.PubkeyToAddress(private.PublicKey).Hex())
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	requests := []map[string]any{}
	defer func() {
		raw, e := json.Marshal(map[string]any{"requirements": []string{"D11_019", "D11_020", "D11_026"}, "requests": requests, "requestCount": len(requests), "providers": []string{"live_gateway", "live_identity", "live_private_socket"}})
		must(t, e)
		t.Log("L35_PHASE3_EVIDENCE " + string(raw))
	}()
	send := func(method, path, route string, body []byte, signature bool) (int, []byte) {
		req, e := http.NewRequest(method, base.String()+path, bytes.NewReader(body))
		must(t, e)
		req.Header.Set("Authorization", "Bearer "+values["L35_PHASE3_ACCESS_TOKEN"])
		req.Header.Set("Origin", values["L35_PHASE3_ORIGIN"])
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Request-ID", uuid.NewString())
		if signature {
			digest, e := sharedauth.BodyDigest(body)
			must(t, e)
			nonce := make([]byte, 32)
			_, e = rand.Read(nonce)
			must(t, e)
			now := time.Now().Unix()
			envelope := sharedauth.OperationEnvelope{Version: 1, ChainID: "125", Environment: values["L35_PHASE3_OPERATION_ENVIRONMENT"], Actor: wallet, Action: "profile.update", Method: method, Path: path, BodyHash: digest, Nonce: hex.EncodeToString(nonce), IssuedAt: now, ExpiresAt: now + 120}
			statement, e := envelope.Statement()
			must(t, e)
			sig, e := crypto.Sign(accounts.TextHash([]byte(statement)), private)
			must(t, e)
			raw, e := json.Marshal(envelope)
			must(t, e)
			req.Header.Set(sharedauth.OperationEnvelopeHeader, base64.RawURLEncoding.EncodeToString(raw))
			req.Header.Set(sharedauth.OperationSignatureHeader, hexutil.Encode(sig))
		}
		response, e := client.Do(req)
		if e != nil {
			t.Fatal("admitted gateway request failed")
		}
		defer response.Body.Close()
		raw, e := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		must(t, e)
		requests = append(requests, map[string]any{"method": method, "route": route, "status": response.StatusCode, "requestId": req.Header.Get("X-Request-ID")})
		return response.StatusCode, raw
	}
	path := "/v1/profiles/" + wallet
	status, raw := send("GET", path, "/v1/profiles/{wallet}", nil, false)
	if status != 200 {
		t.Fatal("profile precondition unavailable", status)
	}
	var profile struct {
		Data struct {
			Profile PublicProfile `json:"profile"`
		} `json:"data"`
	}
	must(t, json.Unmarshal(raw, &profile))
	before := []byte(fmt.Sprintf(`{"expectedRevision":%q,"bio":"phase3-live-before-revocation"}`, profile.Data.Profile.Revision))
	status, raw = send("PATCH", path, "/v1/profiles/{wallet}", before, true)
	if status != 200 {
		t.Fatal("live authenticated mutation precondition failed", status)
	}
	must(t, json.Unmarshal(raw, &profile))
	topic := "private.notifications." + wallet
	status, raw = send("GET", "/v2/snapshots?topics="+url.QueryEscape(topic), "/v2/snapshots", nil, false)
	if status != 200 {
		t.Fatal("private snapshot precondition unavailable", status)
	}
	var snapshot struct {
		Data struct {
			Snapshot struct {
				ResumeCursor string `json:"resumeCursor"`
			} `json:"snapshot"`
		} `json:"data"`
	}
	must(t, json.Unmarshal(raw, &snapshot))
	if snapshot.Data.Snapshot.ResumeCursor == "" {
		t.Fatal("private snapshot missing real resume cursor")
	}
	binding := sharedauth.StreamTicketBinding{SchemaMajor: 2, Topics: []string{topic}, FilterDigest: sharedauth.StreamFilterDigest([]string{topic}), Origin: values["L35_PHASE3_ORIGIN"], Endpoint: "/v2/stream"}
	body, e := json.Marshal(binding)
	must(t, e)
	status, raw = send("POST", "/v2/stream/tickets", "/v2/stream/tickets", body, false)
	if status != 200 {
		t.Fatal("private ticket precondition unavailable", status)
	}
	var ticket struct {
		Data struct {
			Ticket string `json:"ticket"`
		} `json:"data"`
	}
	must(t, json.Unmarshal(raw, &ticket))
	if len(ticket.Data.Ticket) != 64 {
		t.Fatal("missing real private ticket")
	}
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second, Subprotocols: []string{"sidiora.v2", "ticket." + ticket.Data.Ticket}}
	headers := http.Header{"Origin": []string{values["L35_PHASE3_ORIGIN"]}}
	conn, _, e := dialer.Dial(stream.String(), headers)
	if e != nil {
		t.Fatal("admitted private socket failed to connect")
	}
	defer conn.Close()
	must(t, conn.WriteJSON(map[string]any{"op": "subscribe", "subscriptionId": uuid.NewString(), "topics": []string{topic}, "afterCursor": snapshot.Data.Snapshot.ResumeCursor}))
	must(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, raw, e = conn.ReadMessage()
	must(t, e)
	var control struct {
		Op   string `json:"op"`
		Code string `json:"code"`
	}
	must(t, json.Unmarshal(raw, &control))
	if control.Op != "subscribed" {
		t.Fatal("private socket was not admitted", control.Op)
	}
	for control.Code != "REPLAY_COMPLETE" {
		_, raw, e = conn.ReadMessage()
		if e != nil {
			t.Fatal("admitted socket never reached the live phase")
		}
		must(t, json.Unmarshal(raw, &control))
		if control.Op == "error" || control.Op == "resnapshot_required" {
			t.Fatal("admitted socket replay failed", control.Code)
		}
	}
	status, _ = send("POST", "/v2/auth/sessions:logout-all", "/v2/auth/sessions:logout-all", []byte(`{}`), false)
	if status != 204 {
		t.Fatal("real identity revocation failed", status)
	}
	must(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	revoked := false
	for !revoked {
		_, raw, e = conn.ReadMessage()
		if e != nil {
			if websocket.IsCloseError(e, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.ClosePolicyViolation) {
				revoked = true
				break
			}
			t.Fatal("private socket did not report revocation within bound")
		}
		must(t, json.Unmarshal(raw, &control))
		if control.Code == "AUTH_EXPIRED" || control.Code == "AUTH_REQUIRED" {
			revoked = true
			break
		}
		var frame map[string]json.RawMessage
		must(t, json.Unmarshal(raw, &frame))
		if _, ok := frame["family"]; ok {
			t.Fatal("private business frame delivered after acknowledged revocation")
		}
	}
	after := []byte(fmt.Sprintf(`{"expectedRevision":%q,"bio":"phase3-live-after-revocation"}`, profile.Data.Profile.Revision))
	status, _ = send("PATCH", path, "/v1/profiles/{wallet}", after, true)
	if status != 401 {
		t.Fatal("revoked identity HTTP write was not denied at authentication", status)
	}
	status, raw = send("GET", path, "/v1/profiles/{wallet}", nil, false)
	if status != 200 || bytes.Contains(raw, []byte("phase3-live-after-revocation")) || !bytes.Contains(raw, []byte("phase3-live-before-revocation")) {
		t.Fatal("revoked write changed acknowledged profile", status)
	}
}

func TestConnectedPhase3IdentityDependencyInterruption(t *testing.T) {
	s := connectedFixture(t)
	ctx := context.Background()
	p := owner(t, s, 6201)
	must(t, s.PatchProfile(ctx, p, p.AccountID, ProfilePatch{ExpectedRevision: "0", Bio: str("acknowledged-before-identity-outage")}))
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	must(t, e)
	endpoint := "http://" + listener.Addr().String()
	must(t, listener.Close())
	serviceToken := make([]byte, 32)
	_, e = rand.Read(serviceToken)
	must(t, e)
	authority := sharedauth.HTTPIdentityAuthority{Endpoint: endpoint, ServiceToken: hex.EncodeToString(serviceToken)}
	handler := (HTTP{Store: s}).ServiceHandler(authority)
	r := httptest.NewRequest("PATCH", "/v1/profiles/"+fmt.Sprintf("0x%040x", 6201), strings.NewReader(`{"expectedRevision":"1","bio":"unacknowledged-during-outage"}`))
	r.Header.Set("Authorization", "Bearer "+hex.EncodeToString(serviceToken))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 503 || !bytes.Contains(w.Body.Bytes(), []byte(`"retryable":true`)) {
		t.Fatal("identity outage misrepresented", w.Code)
	}
	profile, e := s.Public(ctx, fmt.Sprintf("0x%040x", 6201))
	must(t, e)
	if profile.Revision != "1" || profile.Bio != "acknowledged-before-identity-outage" {
		t.Fatal("identity outage changed acknowledged state", profile.Revision)
	}
	var events int
	must(t, s.DB.QueryRow(ctx, `SELECT count(*) FROM account_outbox WHERE account_id=$1`, p.AccountID).Scan(&events))
	if events != 1 {
		t.Fatal("unacknowledged outage event persisted", events)
	}
	if bytes.Contains(w.Body.Bytes(), []byte(endpoint)) || bytes.Contains(w.Body.Bytes(), []byte(hex.EncodeToString(serviceToken))) {
		t.Fatal("dependency diagnostic leaked")
	}
	t.Log(`L35_PHASE3_EVIDENCE {"requirements":["D11_019","D11_030"],"providers":["real_postgres","closed_real_identity_transport"],"limitations":["outage is transport interruption; live identity lifecycle exercised separately"]}`)
}

func TestConnectedPhase3PreferenceFieldsAndOlderClients(t *testing.T) {
	s := connectedFixture(t)
	ctx := context.Background()
	p := owner(t, s, 6301)
	reduced := true
	must(t, s.PatchExtendedPreferences(ctx, p, ExtendedPreferencesPatch{PreferencesPatch: PreferencesPatch{ExpectedRevision: "0"}, Currency: str("EUR"), ChartInterval: str("4h"), ReducedMotion: &reduced}))
	handler := (HTTP{Store: s, Runtime: &Runtime{ExtendedV3: true}}).Handler()
	requests := []map[string]any{}
	defer func() {
		raw, e := json.Marshal(map[string]any{"requirements": []string{"D11_022"}, "requests": requests, "requestCount": len(requests)})
		must(t, e)
		t.Log("L35_PHASE3_EVIDENCE " + string(raw))
	}()
	for _, test := range []struct{ body, pointer, code string }{
		{`{"expectedRevision":"1","timezone":"Mars/Olympus"}`, "/timezone", "FIELD_INVALID"},
		{`{"expectedRevision":"1","timezone":"Unknown"}`, "/timezone", "FIELD_INVALID"},
		{`{"expectedRevision":"1","currency":"XYZ"}`, "/currency", "FIELD_INVALID"},
		{`{"expectedRevision":"1","chartInterval":"100y"}`, "/chartInterval", "FIELD_INVALID"},
		{`{"expectedRevision":"1","unknownPreference":true}`, "/unknownPreference", "FIELD_UNKNOWN"},
	} {
		response := httpRequest(handler, p, "PATCH", "/v3/me/preferences", test.body)
		requests = append(requests, map[string]any{"method": "PATCH", "route": "/v3/me/preferences", "status": response.Code})
		var wire WireError
		must(t, json.Unmarshal(response.Body.Bytes(), &wire))
		if response.Code != 422 || wire.Code != test.code || len(wire.FieldErrors) != 1 || wire.FieldErrors[0].Pointer != test.pointer {
			t.Fatal("field-level validation mismatch", response.Code, wire)
		}
	}
	must(t, s.PatchPreferences(ctx, p, p.AccountID, PreferencesPatch{ExpectedRevision: "1", Locale: str("fr")}))
	prefs, e := s.ReadPreferences(ctx, p, p.AccountID)
	must(t, e)
	if prefs.Currency != "EUR" || prefs.ChartInterval != "4h" || !prefs.ReducedMotion || prefs.Locale != "fr" || prefs.SchemaVersion != 2 || prefs.Revision != "2" {
		t.Fatal("older client erased extended fields", prefs.Revision)
	}
}
