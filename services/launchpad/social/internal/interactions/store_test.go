package interactions

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type permit struct{}

func (permit) Check(_ context.Context, _ pgx.Tx, r Request) error {
	if r.Action == "mention" && r.Parent == "0x0000000000000000000000000000000000000003" {
		return ErrDenied
	}
	return nil
} // Private fixture only; never a production authority.
func TestText(t *testing.T) {
	for _, s := range []string{"", strings.Repeat("界", 2001), "x\x00", "x\u202e", string([]byte{255})} {
		if Text(s) == nil {
			t.Fatalf("accepted invalid text")
		}
	}
	if Text(strings.Repeat("界", 2000)) != nil {
		t.Fatal("valid unicode rejected")
	}
	if _, e := Mentions(make([]string, 11)); e == nil {
		t.Fatal("mention bound")
	}
}
func TestPostgres(t *testing.T) {
	dsn := os.Getenv("L37_TEST_DSN")
	if dsn == "" {
		t.Fatal("L37_TEST_DSN required: real PostgreSQL fixture")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	schema := "kd_bd7751bfe593ed7a973f74b3"
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("exact schema cleanup failed: %v", err)
			return
		}
		var remains bool
		if err := admin.QueryRow(cleanupCtx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)", schema).Scan(&remains); err != nil {
			t.Errorf("schema absence verification failed: %v", err)
		} else if remains {
			t.Errorf("owned schema remains after cleanup")
		}
	}()
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.MaxConns = 8
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	proposal, e := os.ReadFile(filepath.Join("../../../../..", "launchpad/spec/production-system/handoffs/L37/phase1/schema-proposal.sql"))
	if e != nil {
		t.Fatal(e)
	}
	ddl := string(proposal)
	if _, e = db.Exec(ctx, ddl); e != nil {
		t.Fatal(e)
	}
	phase2, e := os.ReadFile(filepath.Join("../../../../..", "launchpad/spec/production-system/handoffs/L37/phase2/schema-proposal.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, string(phase2)); e != nil {
		t.Fatal(e)
	}
	s := Store{db, permit{}}
	actor := "0x0000000000000000000000000000000000000001"
	pool := "0x0000000000000000000000000000000000000002"
	follow := Request{Actor: actor, ID: pool, Key: "follow"}
	for i := 0; i < 2; i++ {
		if e = s.Follow(ctx, follow); e != nil {
			t.Fatal(e)
		}
	}
	var edges int
	if e = db.QueryRow(ctx, "SELECT count(*) FROM interaction_follows WHERE follower=$1 AND followee=$2", actor, pool).Scan(&edges); e != nil || edges != 1 {
		t.Fatalf("follow uniqueness %d %v", edges, e)
	}
	changed := follow
	changed.Remove = true
	if e = s.Follow(ctx, changed); e != ErrRevision {
		t.Fatal("changed idempotency payload", e)
	}
	changed.Key = "unfollow"
	for i := 0; i < 2; i++ {
		if e = s.Follow(ctx, changed); e != nil {
			t.Fatal(e)
		}
	}
	if e = db.QueryRow(ctx, "SELECT count(*) FROM interaction_follows WHERE follower=$1 AND followee=$2", actor, pool).Scan(&edges); e != nil || edges != 0 {
		t.Fatalf("unfollow uniqueness %d %v", edges, e)
	}

	if _, e = db.Exec(ctx, `INSERT INTO interaction_follows SELECT '0x'||lpad(to_hex(i),40,'0'),$1,'2026-01-01'::timestamptz FROM generate_series(3,100002)i`, actor); e != nil {
		t.Fatal(e)
	}
	cursor := ""
	seen := map[string]bool{}
	for {
		p, e := s.Follows(ctx, actor, false, 100, cursor)
		if e != nil {
			t.Fatal(e)
		}
		if len(p.Edges) > 100 {
			t.Fatal("unbounded")
		}
		for _, x := range p.Edges {
			if seen[x.Wallet] {
				t.Fatal("duplicate")
			}
			seen[x.Wallet] = true
		}
		cursor = p.Next
		if cursor == "" {
			break
		}
	}
	if len(seen) != 100000 {
		t.Fatalf("missing edges %d", len(seen))
	}
	r := Request{Actor: actor, Pool: pool, ID: "root", Body: "界", Key: "create"}
	if e = s.CreateComment(ctx, r); e != nil {
		t.Fatal(e)
	}
	if e = s.CreateComment(ctx, r); e != nil {
		t.Fatal("retry", e)
	}
	edit := r
	edit.Key = "edit-wrong"
	edit.Actor = pool
	edit.Revision = 1
	edit.Body = "changed"
	if e = s.EditComment(ctx, edit); e != ErrDenied {
		t.Fatal("wrong author", e)
	}
	edit.Actor = actor
	edit.Key = "edit-stale"
	edit.Revision = 2
	if e = s.EditComment(ctx, edit); e != ErrRevision {
		t.Fatal("stale edit", e)
	}
	if _, e = db.Exec(ctx, "UPDATE interaction_comments SET created_at=now()-interval '16 minutes' WHERE id='root'"); e != nil {
		t.Fatal(e)
	}
	edit.Key = "edit-expired"
	edit.Revision = 1
	if e = s.EditComment(ctx, edit); e != ErrDenied {
		t.Fatal("expired edit", e)
	}
	var body string
	var revision int64
	if e = db.QueryRow(ctx, "SELECT body,revision FROM interaction_comments WHERE id='root'").Scan(&body, &revision); e != nil || body != "界" || revision != 1 {
		t.Fatalf("edit rollback %q %d %v", body, revision, e)
	}
	if _, e = db.Exec(ctx, "UPDATE interaction_comments SET created_at=now() WHERE id='root'"); e != nil {
		t.Fatal(e)
	}

	bad := r
	bad.ID = "cross"
	bad.Key = "cross"
	bad.Parent = "root"
	bad.Pool = actor
	if e = s.CreateComment(ctx, bad); e == nil {
		t.Fatal("cross pool")
	}
	parent := "root"
	for i := 2; i <= 5; i++ {
		x := r
		x.ID = fmt.Sprint("depth", i)
		x.Key = x.ID
		x.Parent = parent
		e = s.CreateComment(ctx, x)
		if i == 5 {
			if e != ErrDepth {
				t.Fatalf("depth: %v", e)
			}
		} else if e != nil {
			t.Fatal(e)
		}
		parent = x.ID
	}
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- s.React(ctx, Request{Actor: actor, ID: "root", Kind: "like", Key: fmt.Sprint("like", i)})
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if n, e := s.Count(ctx, "root", "like"); e != nil || n != 1 {
		t.Fatalf("likes %d %v", n, e)
	}
	errs = make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- s.React(ctx, Request{Actor: actor, ID: "root", Kind: "like", Key: fmt.Sprint("unlike", i), Remove: true})
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if n, e := s.Count(ctx, "root", "like"); e != nil || n != 0 {
		t.Fatalf("unlikes %d %v", n, e)
	}
	r.Revision = 1
	r.Remove = true
	r.Key = "delete"
	if e = s.EditComment(ctx, r); e != nil {
		t.Fatal(e)
	}
	if e = s.React(ctx, Request{Actor: actor, ID: "root", Kind: "like", Key: "removed-like"}); e != ErrDenied {
		t.Fatal("deleted target reaction", e)
	}

	bad = r
	bad.ID = "deleted-reply"
	bad.Key = bad.ID
	bad.Parent = "root"
	if e = s.CreateComment(ctx, bad); e == nil {
		t.Fatal("deleted parent")
	}
	callout := Request{Actor: actor, ID: "callout-source-id", Revision: 1, Key: "callout"}
	recipients := []string{"0x0000000000000000000000000000000000000003", pool}
	for i := 0; i < 2; i++ {
		if e = s.ReferenceCallout(ctx, callout, recipients, "unverified", nil); e != nil {
			t.Fatal(e)
		}
	}
	conflict := callout
	conflict.Key = "callout-conflict"
	if e = s.ReferenceCallout(ctx, conflict, []string{actor}, "different", nil); e != ErrRevision {
		t.Fatal("conflicting callout metadata", e)
	}
	var rolledBack int
	if e = db.QueryRow(ctx, "SELECT count(*) FROM interaction_mutations WHERE key IN ('edit-wrong','edit-stale','edit-expired','callout-conflict')").Scan(&rolledBack); e != nil || rolledBack != 0 {
		t.Fatalf("mutation rollback %d %v", rolledBack, e)
	}

	callout.Key = "callout-new-key"
	if err := s.ReferenceCallout(ctx, callout, recipients, "unverified", nil); err != nil {
		t.Fatal(err)
	}
	var events int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM interaction_outbox WHERE action='follow'").Scan(&events); err != nil || events != 2 {
		t.Fatalf("follow event no-op suppression %d %v", events, err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM interaction_outbox WHERE action='reaction'").Scan(&events); err != nil || events != 2 {
		t.Fatalf("reaction event no-op suppression %d %v", events, err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM interaction_outbox WHERE action='callout_reference'").Scan(&events); err != nil || events != 1 {
		t.Fatalf("callout event no-op suppression %d %v", events, err)
	}
	if err := db.QueryRow(ctx, "SELECT revision FROM interaction_aggregate_revisions WHERE aggregate_id=$1", "follow:"+actor+":"+pool).Scan(&revision); err != nil || revision != 2 {
		t.Fatalf("durable edge revision %d %v", revision, err)
	}
	if err := db.QueryRow(ctx, "SELECT revision FROM interaction_outbox WHERE action='comment' AND target='root' ORDER BY revision DESC LIMIT 1").Scan(&revision); err != nil || revision != 2 {
		t.Fatalf("comment tombstone revision %d %v", revision, err)
	}

	var intents int
	if e = db.QueryRow(ctx, "SELECT count(*) FROM interaction_notification_intents").Scan(&intents); e != nil || intents != 1 {
		t.Fatalf("mention intents %d %v", intents, e)
	}

	var count int
	if e = db.QueryRow(ctx, "SELECT count(*) FROM interaction_comments WHERE id IN ('cross','depth5','deleted-reply')").Scan(&count); e != nil || count != 0 {
		t.Fatalf("invalid rows %d %v", count, e)
	}
	if e = s.HideComment(ctx, Request{Actor: pool, Pool: pool, ID: "depth2", Key: "creator-hide-denied", Revision: 1, Body: "policy reason"}); e != ErrDenied {
		t.Fatal("generic authority cannot grant creator privileges", e)
	}
	if e = (Store{DB: db}).Follow(ctx, Request{Actor: actor, ID: pool, Key: "denied"}); e != ErrDenied {
		t.Fatal("missing authority must deny", e)
	}
}

// syntheticVerifier tests the trust boundary only; it is not live receipt evidence.
type syntheticVerifier struct {
	err    error
	called bool
	chain  int
}

func (v *syntheticVerifier) Verify(_ context.Context, _ string, chain int) error {
	v.called = true
	v.chain = chain
	return v.err
}
func TestProvenanceAndDependencies(t *testing.T) {
	ctx := context.Background()
	hash := "0x" + strings.Repeat("a", 64)
	if Provenance(ctx, hash, nil).Provenance != "user_content" {
		t.Fatal("nil verification")
	}
	fail := &syntheticVerifier{err: errors.New("outage")}
	if Provenance(ctx, hash, fail).Provenance != "user_content" {
		t.Fatal("failed verification")
	}
	ok := &syntheticVerifier{}
	if Provenance(ctx, "bad", ok).Provenance != "user_content" || ok.called {
		t.Fatal("invalid hash")
	}
	if Provenance(ctx, hash, ok).Provenance != "verified_transaction" || !ok.called || ok.chain != 125 {
		t.Fatal("canonical verification boundary")
	}
	if _, e := (Store{}).Follows(ctx, "", false, 0, ""); e != ErrDependency {
		t.Fatal("nil follows", e)
	}
	if _, e := (Store{}).Count(ctx, "", ""); e != ErrDependency {
		t.Fatal("nil count", e)
	}
	if err := (Store{}).HideComment(ctx, Request{Actor: "0x0000000000000000000000000000000000000001", Pool: "0x0000000000000000000000000000000000000002", ID: "comment", Revision: 1, Body: "reason", Key: "hide"}); err != ErrDenied {
		t.Fatal("missing creator authority", err)
	}
	if err := (Store{}).React(ctx, Request{Kind: "unknown"}); err != ErrReactionKind {
		t.Fatal("reaction enum", err)
	}
}
