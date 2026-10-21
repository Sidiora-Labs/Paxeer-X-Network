package metadata

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProductionV2RoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable")
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	schema := fmt.Sprintf("kd_acf8ff69ca3a26ba76d2244d_v2_%d", time.Now().UnixNano())
	if _, e = pool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); e != nil {
		t.Fatal(e)
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, e := pool.Exec(c, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); e != nil {
			t.Error(e)
		}
	}()
	s, e := NewProductionStore(pool, schema, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, s.SchemaDDL()+s.V2SchemaDDL()); e != nil {
		t.Fatal(e)
	}
	key, e := crypto.GenerateKey()
	if e != nil {
		t.Fatal(e)
	}
	wallet := strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
	token := "0x" + strings.Repeat("a", 40)
	addr := "0x" + strings.Repeat("b", 40)
	if _, e = pool.Exec(ctx, "INSERT INTO "+s.table("pools")+" VALUES($1,$2,$3)", addr, wallet, token); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	audience := "https://metadata.example"
	s.Verifier = V2SignedVerifier{Store: s, Audience: audience, Clock: func() time.Time { return now }}
	router := chi.NewRouter()
	V2Handlers{Store: s, Clock: func() time.Time { return now }}.RegisterRoutes(router)
	server := httptest.NewServer(router)
	defer server.Close()
	send := func(key *ecdsa.PrivateKey, target string, raw []byte, op, nonce string) (int, []byte) {
		t.Helper()
		h := sha256.Sum256(raw)
		a := VerifiedAction{Wallet: strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex()), OperationID: op, Method: "PATCH", Path: "/v2/tokens/" + target + "/metadata", BodyHash: hex.EncodeToString(h[:]), Nonce: nonce, ExpiresAt: fmt.Sprint(now.Add(time.Minute).Unix()), Audience: audience, ChainID: 125}
		signature, e := crypto.Sign(accounts.TextHash([]byte(V2SigningMessage(a))), key)
		if e != nil {
			t.Fatal(e)
		}
		req, e := http.NewRequestWithContext(ctx, http.MethodPatch, server.URL+a.Path, bytes.NewReader(raw))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", op)
		req.Header.Set("X-Action-Wallet", a.Wallet)
		req.Header.Set("X-Action-Signature", "0x"+hex.EncodeToString(signature))
		req.Header.Set("X-Action-Nonce", nonce)
		req.Header.Set("X-Action-Expires", a.ExpiresAt)
		req.Header.Set("X-Action-Audience", audience)
		resp, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		b, e := io.ReadAll(resp.Body)
		if e != nil {
			t.Fatal(e)
		}
		return resp.StatusCode, b
	}
	tags := make([]string, 20)
	for i := range tags {
		tags[i] = fmt.Sprint("tag", i)
	}
	socials := []V2Social{}
	for i := 0; i < 5; i++ {
		socials = append(socials, V2Social{fmt.Sprint("site", i), fmt.Sprintf("https://example.com/%d", i)})
	}
	doc := map[string]any{"expectedRevision": "0", "name": "<script>alert(1)</script>", "symbol": "TEST", "description": strings.Repeat("界", 4096), "tags": tags, "socials": socials}
	raw, _ := json.Marshal(doc)
	op := uuid.NewString()
	nonce := strings.Repeat("1", 64)
	t.Log("V2_SCHEMA patch " + string(raw))
	status, body := send(key, token, raw, op, nonce)
	if status != 200 {
		t.Fatalf("PATCH %d %s", status, body)
	}
	t.Log("V2_SCHEMA response " + string(body))
	var patched WireResponse[V2Data]
	if e = json.Unmarshal(body, &patched); e != nil {
		t.Fatal(e)
	}
	resp, e := server.Client().Get(server.URL + "/v2/tokens/" + token + "/metadata")
	if e != nil {
		t.Fatal(e)
	}
	var got WireResponse[V2Data]
	e = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if e != nil {
		t.Fatal(e)
	}
	if resp.StatusCode != 200 || got.Data.Revision != "1" || got.Data.Display.Description != doc["description"] || len(got.Data.Display.Tags) != 20 || len(got.Data.Display.Socials) != 5 || got.Data.Display.Name == nil || *got.Data.Display.Name != doc["name"] || got.Data.PoolAddress != addr || got.Data.TokenAddress != token || got.Data.ChainID != 125 {
		t.Fatal(got)
	}
	if bytes.Contains(body, []byte("<script>")) {
		t.Fatal("script not JSON escaped")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/v2/tokens/"+token+"/metadata", nil)
	req.Header.Set("If-None-Match", resp.Header.Get("ETag"))
	cached, e := server.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	cached.Body.Close()
	if cached.StatusCode != 304 {
		t.Fatal(cached.StatusCode)
	}
	status, body = send(key, token, raw, op, nonce)
	if status != 200 {
		t.Fatalf("replay %d %s", status, body)
	}
	var count int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM "+s.table("v2_outbox")).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
	foreignToken := "0x" + strings.Repeat("d", 40)
	foreignPool := "0x" + strings.Repeat("e", 40)
	if _, e = pool.Exec(ctx, "INSERT INTO "+s.table("pools")+" VALUES($1,$2,$3)", foreignPool, wallet, foreignToken); e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256(raw)
	originalAction := VerifiedAction{Wallet: wallet, OperationID: op, Method: "PATCH", Path: "/v2/tokens/" + token + "/metadata", BodyHash: hex.EncodeToString(hash[:]), Nonce: nonce, ExpiresAt: fmt.Sprint(now.Add(time.Minute).Unix()), Audience: audience, ChainID: 125}
	signature, e := crypto.Sign(accounts.TextHash([]byte(V2SigningMessage(originalAction))), key)
	if e != nil {
		t.Fatal(e)
	}
	crossPool, e := http.NewRequestWithContext(ctx, "PATCH", server.URL+"/v2/tokens/"+foreignToken+"/metadata", bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	crossPool.Header.Set("Content-Type", "application/json")
	crossPool.Header.Set("Idempotency-Key", op)
	crossPool.Header.Set("X-Action-Wallet", wallet)
	crossPool.Header.Set("X-Action-Signature", "0x"+hex.EncodeToString(signature))
	crossPool.Header.Set("X-Action-Nonce", nonce)
	crossPool.Header.Set("X-Action-Expires", originalAction.ExpiresAt)
	crossPool.Header.Set("X-Action-Audience", audience)
	crossResponse, e := server.Client().Do(crossPool)
	if e != nil {
		t.Fatal(e)
	}
	crossResponse.Body.Close()
	if crossResponse.StatusCode != 403 {
		t.Fatal("cross-pool signed replay", crossResponse.StatusCode)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM "+s.table("v2_documents")+" WHERE pool=$1", foreignPool).Scan(&count); e != nil || count != 0 {
		t.Fatal("cross-pool replay changed row", count, e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM "+s.table("v2_outbox")).Scan(&count); e != nil || count != 1 {
		t.Fatal("cross-pool replay emitted event", count, e)
	}
	otherKey, e := crypto.GenerateKey()
	if e != nil {
		t.Fatal(e)
	}
	status, _ = send(otherKey, token, raw, uuid.NewString(), strings.Repeat("2", 64))
	if status != 403 {
		t.Fatal(status)
	}
	status, _ = send(key, "0x"+strings.Repeat("c", 40), raw, uuid.NewString(), strings.Repeat("3", 64))
	if status != 404 {
		t.Fatal(status)
	}
	doc["expectedRevision"] = "1"
	doc["description"] = ""
	raw, _ = json.Marshal(doc)
	status, body = send(key, token, raw, uuid.NewString(), strings.Repeat("4", 64))
	if status != 200 {
		t.Fatalf("empty %d %s", status, body)
	}
	t.Log("V2_SCHEMA patch " + string(raw))
	t.Log("V2_SCHEMA response " + string(body))
	if e = json.Unmarshal(body, &got); e != nil || got.Data.Display.Description != "" || got.Data.Revision != "2" {
		t.Fatal(got, e)
	}
	oversized, _ := json.Marshal(map[string]any{"expectedRevision": "2", "description": strings.Repeat("x", V2BodyLimit)})
	status, _ = send(key, token, oversized, uuid.NewString(), strings.Repeat("9", 64))
	if status != 422 {
		t.Fatal("body limit", status)
	}
	status, _ = send(key, token, raw, uuid.NewString(), nonce)
	if status != 403 {
		t.Fatal("nonce replay", status)
	}
	forged, _ := http.NewRequestWithContext(ctx, "PATCH", server.URL+"/v2/tokens/"+token+"/metadata", bytes.NewReader(raw))
	forged.Header.Set("Content-Type", "application/json")
	forged.Header.Set("Idempotency-Key", uuid.NewString())
	forged.Header.Set("X-Action-Wallet", wallet)
	forged.Header.Set("X-Action-Signature", "0x"+strings.Repeat("0", 130))
	forged.Header.Set("X-Action-Nonce", strings.Repeat("a", 64))
	forged.Header.Set("X-Action-Expires", fmt.Sprint(now.Add(time.Minute).Unix()))
	forged.Header.Set("X-Action-Audience", audience)
	denied, e := server.Client().Do(forged)
	if e != nil {
		t.Fatal(e)
	}
	denied.Body.Close()
	if denied.StatusCode != 403 {
		t.Fatal("forged signature", denied.StatusCode)
	}
	doc["expectedRevision"] = "2"
	doc["description"] = strings.Repeat("x", 4097)
	raw, _ = json.Marshal(doc)
	status, _ = send(key, token, raw, uuid.NewString(), strings.Repeat("5", 64))
	if status != 422 {
		t.Fatal(status)
	}
	doc["description"] = "concurrent"
	raw, _ = json.Marshal(doc)
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			status, _ := send(key, token, raw, uuid.NewString(), strings.Repeat(fmt.Sprint(i+6), 64))
			statuses <- status
		}(i)
	}
	wg.Wait()
	close(statuses)
	ok, conflict := 0, 0
	for v := range statuses {
		if v == 200 {
			ok++
		} else if v == 409 {
			conflict++
		} else {
			t.Fatal(v)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatal(ok, conflict)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM "+s.table("v2_nonces")).Scan(&count); e != nil || count != 3 {
		t.Fatal("nonce rollback", count, e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM "+s.table("v2_outbox")).Scan(&count); e != nil || count != 3 {
		t.Fatal(count, e)
	}
	doc["expectedRevision"] = "3"
	doc["description"] = "dependency recovery"
	recoveryRaw, e := json.Marshal(doc)
	if e != nil {
		t.Fatal(e)
	}
	recoveryID := uuid.NewString()
	recoveryNonce := strings.Repeat("b", 64)
	if _, e = pool.Exec(ctx, "ALTER TABLE "+s.table("v2_outbox")+" RENAME TO unavailable_outbox"); e != nil {
		t.Fatal(e)
	}
	status, _ = send(key, token, recoveryRaw, recoveryID, recoveryNonce)
	if status != 503 {
		t.Fatal("database failure must remain retryable", status)
	}
	var persistedRevision string
	if e = pool.QueryRow(ctx, "SELECT revision::text FROM "+s.table("v2_documents")+" WHERE pool=$1", addr).Scan(&persistedRevision); e != nil || persistedRevision != "3" {
		t.Fatal("failed dependency changed revision", persistedRevision, e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM "+s.table("v2_nonces")).Scan(&count); e != nil || count != 3 {
		t.Fatal("failed dependency consumed nonce", count, e)
	}
	if _, e = pool.Exec(ctx, "ALTER TABLE "+s.table("unavailable_outbox")+" RENAME TO v2_outbox"); e != nil {
		t.Fatal(e)
	}
	status, body = send(key, token, recoveryRaw, recoveryID, recoveryNonce)
	if status != 200 {
		t.Fatalf("recovered signed operation %d %s", status, body)
	}
	status, body = send(key, token, recoveryRaw, recoveryID, recoveryNonce)
	if status != 200 {
		t.Fatalf("duplicate recovered signed operation %d %s", status, body)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM "+s.table("v2_outbox")).Scan(&count); e != nil || count != 4 {
		t.Fatal("recovery duplicated revision", count, e)
	}
	if e = s.Restrict(ctx, addr, "1", true); e != nil {
		t.Fatal(e)
	}
	doc["expectedRevision"] = "3"
	raw, _ = json.Marshal(doc)
	status, _ = send(key, token, raw, uuid.NewString(), strings.Repeat("8", 64))
	if status != 403 {
		t.Fatal(status)
	}
}

func TestProductionV2Bounds(t *testing.T) {
	for _, raw := range []string{`{"expectedRevision":"0","name":""}`, `{"expectedRevision":"0","symbol":""}`, `{"expectedRevision":"0","description":null}`, `{"expectedRevision":"0","description":"x","description":"y"}`, `{"expectedRevision":"0","socials":[{"label":"site","url":"javascript:alert(1)"}]}`, `{"expectedRevision":"0","unknown":true}`, `{"expectedRevision":"0","socials":[{"label":"a","label":"b","url":"https://example.com"}]}`} {
		if _, e := ParseV2Patch([]byte(raw)); e == nil {
			t.Fatal(raw)
		}
	}
	for _, tc := range []struct {
		field string
		value any
	}{{"name", strings.Repeat("界", 81)}, {"symbol", strings.Repeat("x", 17)}, {"description", strings.Repeat("x", 4097)}, {"tags", make([]string, 21)}, {"socials", make([]V2Social, 6)}} {
		b, _ := json.Marshal(map[string]any{"expectedRevision": "0", tc.field: tc.value})
		if _, e := ParseV2Patch(b); e == nil {
			t.Fatal(tc.field)
		}
	}
	p, e := ParseV2Patch([]byte(`{"expectedRevision":"0","name":"e\u0301","description":""}`))
	if e != nil || *p.Name != "é" || *p.Description != "" {
		t.Fatal(p, e)
	}
	if _, e := ParseV2Patch(append([]byte(`{"expectedRevision":"0","name":"`), append([]byte{0xff}, []byte(`"}`)...)...)); e == nil {
		t.Fatal("invalid UTF8")
	}
}

func TestProductionV2SelectedContractPatch(t *testing.T) {
	p, e := ParseV2ContractPatch([]byte(`{"expectedRevision":"0","displayName":"e\u0301","displaySymbol":"SYM","description":"","tags":[],"socialLinks":[{"label":"site","url":"https://example.com"}]}`))
	if e != nil || p.Name == nil || *p.Name != "é" || p.Symbol == nil || *p.Symbol != "SYM" || p.Socials == nil || len(*p.Socials) != 1 {
		t.Fatal(p, e)
	}
	for _, raw := range []string{`{"expectedRevision":"0","displayName":null}`, `{"expectedRevision":"0","name":"legacy"}`, `{"expectedRevision":"0","displayName":"a","displayName":"b"}`, `{"expectedRevision":"0","socialLinks":[{"label":"x","url":"https://user:pass@example.com"}]}`} {
		if _, e := ParseV2ContractPatch([]byte(raw)); e == nil {
			t.Fatal("invalid selected contract accepted", raw)
		}
	}
}
func TestProductionV2MissingCanonicalSourceUnavailable(t *testing.T) {
	router := chi.NewRouter()
	V2ContractHandlers{}.RegisterRoutes(router)
	for _, method := range []string{"GET", "PATCH"} {
		req := httptest.NewRequest(method, "/v2/tokens/0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/metadata", strings.NewReader(`{"expectedRevision":"0"}`))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatal(method, response.Code, response.Body.String())
		}
	}
}

func TestProductionV2RevisionSevenRace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, e := pgxpool.New(ctx, "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	schema := fmt.Sprintf("metadata_revision_race_%d", time.Now().UnixNano())
	if _, e = db.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); e != nil {
		t.Fatal(e)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, e = db.Exec(cleanup, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); e != nil {
			t.Error(e)
		}
	}()
	store, e := NewProductionStore(db, schema, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, store.SchemaDDL()+store.V2SchemaDDL()); e != nil {
		t.Fatal(e)
	}
	key, e := crypto.GenerateKey()
	if e != nil {
		t.Fatal(e)
	}
	wallet := strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
	token := "0x" + strings.Repeat("1", 40)
	if _, e = db.Exec(ctx, "INSERT INTO "+store.table("pools")+" VALUES($1,$2,$1)", token, wallet); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, "INSERT INTO "+store.table("v2_documents")+"(pool,revision) VALUES($1,7)", token); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	audience := "https://metadata.example"
	store.Verifier = V2SignedVerifier{Store: store, Audience: audience, Clock: func() time.Time { return now }}
	errors := make(chan error, 2)
	var group sync.WaitGroup
	for i, id := range []string{"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"} {
		raw, e := json.Marshal(map[string]any{"expectedRevision": "7", "description": id})
		if e != nil {
			t.Fatal(e)
		}
		hash := sha256.Sum256(raw)
		action := VerifiedAction{Wallet: wallet, OperationID: uuid.NewString(), Method: "PATCH", Path: "/v2/tokens/" + token + "/metadata", BodyHash: hex.EncodeToString(hash[:]), Nonce: strings.Repeat(fmt.Sprint(i+1), 64), ExpiresAt: fmt.Sprint(now.Add(time.Minute).Unix()), Audience: audience, ChainID: 125}
		signature, e := crypto.Sign(accounts.TextHash([]byte(V2SigningMessage(action))), key)
		if e != nil {
			t.Fatal(e)
		}
		action.Signature = "0x" + hex.EncodeToString(signature)
		group.Add(1)
		go func(raw []byte, action VerifiedAction) {
			defer group.Done()
			_, e := store.ApplyV2(ctx, token, raw, action, now)
			errors <- e
		}(raw, action)
	}
	group.Wait()
	close(errors)
	passed, conflict := 0, 0
	for e := range errors {
		if e == nil {
			passed++
		} else if e == ErrConflict {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if passed != 1 || conflict != 1 {
		t.Fatal("expected one approved revision and one conflict", passed, conflict)
	}
	var revision string
	var events, nonces int
	if e = db.QueryRow(ctx, "SELECT revision::text FROM "+store.table("v2_documents")+" WHERE pool=$1", token).Scan(&revision); e != nil || revision != "8" {
		t.Fatal(revision, e)
	}
	if e = db.QueryRow(ctx, "SELECT count(*) FROM "+store.table("v2_outbox")).Scan(&events); e != nil || events != 1 {
		t.Fatal(events, e)
	}
	if e = db.QueryRow(ctx, "SELECT count(*) FROM "+store.table("v2_nonces")).Scan(&nonces); e != nil || nonces != 1 {
		t.Fatal(nonces, e)
	}
}
