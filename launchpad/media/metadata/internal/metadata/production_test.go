package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"sync"
	"testing"
	"time"
)

type testVerifier struct{}
type denyVerifier struct{}

func (denyVerifier) AuthorizeInTx(context.Context, pgx.Tx, VerifiedAction) error { return ErrForbidden }

func (testVerifier) AuthorizeInTx(ctx context.Context, tx pgx.Tx, a VerifiedAction) error { return nil } // Explicit verifier seam; signature crypto belongs to L34.
func TestProductionPatchBounds(t *testing.T) {
	for _, raw := range []string{`{"expectedRevision":"0","unknown":1}`, `{"expectedRevision":"0","ExpectedRevision":"1"}`, `{"expectedRevision":"0","expectedRevision":"1"}`, `{"expectedRevision":"00"}`, `{"expectedRevision":"0","description":null}`, `{"expectedRevision":"0","tags":null}`, `{"expectedRevision":"0","description":"\u0000"}`, `{"expectedRevision":"0","tags":["x","x"]}`} {
		if _, e := ParsePatch([]byte(raw)); e == nil {
			t.Fatal(raw)
		}
	}
	b, _ := json.Marshal(map[string]any{"expectedRevision": "0", "description": strings.Repeat("x", 4097)})
	if _, e := ParsePatch(b); e == nil {
		t.Fatal("oversize accepted")
	}
	p, e := ParsePatch([]byte(`{"expectedRevision":"0","description":"e\u0301<script>"}`))
	if e != nil || *p.Description != "é<script>" {
		t.Fatal(p, e)
	}
}
func TestProductionPersistentRevision(t *testing.T) {
	ctx, c := context.WithTimeout(context.Background(), 30*time.Second)
	defer c()
	pool, e := pgxpool.New(ctx, "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable")
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	schema := fmt.Sprintf("kd_acf8ff69ca3a26ba76d2244d_%d", time.Now().UnixNano())
	_, e = pool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize())
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, e := pool.Exec(c, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); e != nil {
			t.Errorf("cleanup: %v", e)
		}
	}()
	s, _ := NewProductionStore(pool, schema, testVerifier{})
	if _, e = pool.Exec(ctx, s.SchemaDDL()); e != nil {
		t.Fatal(e)
	}
	addr := "0x" + strings.Repeat("1", 40)
	wallet := "0x" + strings.Repeat("2", 40)
	_, e = pool.Exec(ctx, "INSERT INTO "+s.table("pools")+" VALUES($1,$2,$1)", addr, wallet)
	if e != nil {
		t.Fatal(e)
	}
	raw := []byte(`{"expectedRevision":"0","description":"hello"}`)
	h := sha256.Sum256(raw)
	a := VerifiedAction{Wallet: wallet, OperationID: "11111111-1111-1111-1111-111111111111", Method: "PATCH", Path: "/v1/tokens/" + addr + "/metadata", BodyHash: hex.EncodeToString(h[:])}
	r, e := s.Apply(ctx, addr, raw, a)
	if e != nil || r != "1" {
		t.Fatal(r, e)
	}
	r, e = s.Apply(ctx, addr, raw, a)
	if e != nil || r != "1" {
		t.Fatal(r, e)
	}
	var n int
	pool.QueryRow(ctx, "SELECT count(*) FROM "+s.table("outbox")).Scan(&n)
	if n != 1 {
		t.Fatal(n)
	}
	s.Verifier = denyVerifier{}
	if _, e = s.Apply(ctx, addr, raw, a); e != ErrForbidden {
		t.Fatal("unauthenticated replay", e)
	}
	s.Verifier = testVerifier{}
	var audit map[string]any
	var payload []byte
	if e = pool.QueryRow(ctx, "SELECT payload FROM "+s.table("outbox")+" WHERE pool=$1 AND revision=1", addr).Scan(&payload); e != nil {
		t.Fatal(e)
	}
	json.Unmarshal(payload, &audit)
	if audit["actor"] != wallet || audit["previousRevision"] != "0" || audit["document"].(map[string]any)["description"] != "hello" {
		t.Fatal(audit)
	}
	a.OperationID = "22222222-2222-2222-2222-222222222222"
	if _, e = s.Apply(ctx, addr, raw, a); e != ErrConflict {
		t.Fatal(e)
	}
	a.Path = "/v1/tokens/0x" + strings.Repeat("3", 40)
	if _, e = s.Apply(ctx, addr, raw, a); e != ErrForbidden {
		t.Fatal(e)
	}
	a.Path = "/v1/tokens/" + addr + "/metadata"
	a.Wallet = "0x" + strings.Repeat("4", 40)
	if _, e = s.Apply(ctx, addr, raw, a); e != ErrForbidden {
		t.Fatal(e)
	}
	a.Wallet = wallet
	raw = []byte(`{"expectedRevision":"1","description":"next"}`)
	h = sha256.Sum256(raw)
	a.BodyHash = hex.EncodeToString(h[:])
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a := a
			a.OperationID = fmt.Sprintf("33333333-3333-3333-3333-%012d", i)
			_, e := s.Apply(ctx, addr, raw, a)
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	ok, conflict := 0, 0
	for e := range errs {
		if e == nil {
			ok++
		} else if e == ErrConflict {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatal(ok, conflict)
	}
	// Real DB outbox failure must roll back document, operation and nonce seam.
	if _, e = pool.Exec(ctx, "ALTER TABLE "+s.table("outbox")+" RENAME TO outbox_hidden"); e != nil {
		t.Fatal(e)
	}
	rollbackRaw := []byte(`{"expectedRevision":"2","description":"must rollback"}`)
	rh := sha256.Sum256(rollbackRaw)
	ra := a
	ra.OperationID = "66666666-6666-6666-6666-666666666666"
	ra.BodyHash = hex.EncodeToString(rh[:])
	if _, e = s.Apply(ctx, addr, rollbackRaw, ra); e == nil {
		t.Fatal("missing outbox accepted")
	}
	var persisted string
	pool.QueryRow(ctx, "SELECT revision::text FROM "+s.table("documents")+" WHERE pool=$1", addr).Scan(&persisted)
	if persisted != "2" {
		t.Fatal(persisted)
	}
	pool.Exec(ctx, "ALTER TABLE "+s.table("outbox_hidden")+" RENAME TO outbox")
	other := "0x" + strings.Repeat("5", 40)
	pool.Exec(ctx, "INSERT INTO "+s.table("pools")+" VALUES($1,$2,$1)", other, wallet)
	if e = s.Restrict(ctx, other, "7", true); e != nil {
		t.Fatal(e)
	}
	var preBlocked bool
	pool.QueryRow(ctx, "SELECT blocked FROM "+s.table("documents")+" WHERE pool=$1", other).Scan(&preBlocked)
	if !preBlocked {
		t.Fatal("pre-document restriction lost")
	}
	if e = s.Restrict(ctx, addr, "5", true); e != nil {
		t.Fatal(e)
	}
	s.Restrict(ctx, addr, "4", false)
	var blocked bool
	pool.QueryRow(ctx, "SELECT blocked FROM "+s.table("documents")+" WHERE pool=$1", addr).Scan(&blocked)
	if !blocked {
		t.Fatal("old allow replaced block")
	}
}
