package interactions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/fanout"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"
)

type connectedFixture struct {
	db      *pgxpool.Pool
	store   Store
	policy  *PGPolicy
	http    *HTTP
	durable *delivery.Store
	ctx     map[string]context.Context
	wallets []string
	pool    string
	schema  string
	redis   *redis.Client
}

func connected(t *testing.T) *connectedFixture {
	t.Helper()
	ctx := context.Background()
	dsn := os.Getenv("L37_PHASE2_DSN")
	schema := os.Getenv("L37_PHASE2_SCHEMA")
	canonical := os.Getenv("L37_PHASE2_CANONICAL_SCHEMA")
	if dsn == "" || !regexp.MustCompile(`^kd_[0-9a-f]{24}$`).MatchString(schema) || canonical == "" {
		t.Fatal("real leased L37_PHASE2_DSN, L37_PHASE2_SCHEMA and admitted L37_PHASE2_CANONICAL_SCHEMA required")
	}
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(admin.Close)
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
		var remains bool
		if err := admin.QueryRow(cleanup, `SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)`, schema).Scan(&remains); err != nil || remains {
			t.Errorf("owned fixture namespace remains: %v", err)
		}
	})
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = pgx.Identifier{schema}.Sanitize() + ",pg_catalog"
	cfg.MaxConns = 12
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(db.Close)
	for _, path := range []string{"spec/production-system/integration/migrations/330000.sql", "spec/production-system/integration/migrations/350000.sql", "spec/production-system/integration/migrations/360000.sql", "spec/production-system/integration/migrations/370000.sql", "spec/production-system/integration/migrations/380000.sql", "spec/production-system/handoffs/L37/phase2/schema-proposal.sql"} {
		raw, err := os.ReadFile(filepath.Join("../../../..", path))
		if err != nil {
			t.Fatal(err)
		}
		ddl := regexp.MustCompile(`(?m)^SET LOCAL search_path[^\n]*\n`).ReplaceAllString(string(raw), "")
		if _, err = db.Exec(ctx, ddl); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	if _, e = db.Exec(ctx, `CREATE TABLE pools(pool_address text PRIMARY KEY,creator text NOT NULL)`); e != nil {
		t.Fatal(e)
	}
	deliveryDDL, e := delivery.SchemaDDL(schema)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, deliveryDDL); e != nil {
		t.Fatal(e)
	}
	policy := delivery.Policy{Subjects: map[string]bool{"lane37.public": true, "lane37.private": true, "lane37.erasure": true}, EventTypes: map[string]bool{"social.edge.changed": true, "social.comment.changed": true, "social.callout.changed": true, "social.mention.intent.v1": true, "account.social.erasure.v1": true}}
	durable, e := delivery.NewStore(db, schema, policy)
	if e != nil {
		t.Fatal(e)
	}
	position := CanonicalSourceOrder{Positions: pgx.Identifier{canonical, "canonical_positions"}}
	tx, e := db.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = position.Read(ctx, tx); e != nil {
		rollback(tx)
		t.Fatal("admitted real mined canonical source position required: ", e)
	}
	rollback(tx)
	publisher := &Publisher{Delivery: durable, Subjects: map[string]string{"social.edge.changed": "lane37.public", "social.comment.changed": "lane37.public", "social.callout.changed": "lane37.public"}, NotificationSubject: "lane37.private", Watermark: position.Read}
	pgpolicy := &PGPolicy{DB: db, Accounts: pgx.Identifier{schema, "account_state"}, Pools: pgx.Identifier{schema, "pools"}, Sessions: pgx.Identifier{schema, "identity_sessions"}, IdentityAccounts: pgx.Identifier{schema, "identity_accounts"}, WalletLinks: pgx.Identifier{schema, "identity_wallet_links"}, Publisher: publisher, SessionGuard: pgx.Identifier{schema, "social_session_live"}, AccountGuard: pgx.Identifier{schema, "social_account_active"}, PoolGuard: pgx.Identifier{schema, "social_pool_creator"}}
	guards, e := pgpolicy.GuardDDL()
	if e != nil {
		t.Fatal(e)
	}
	for _, ddl := range guards {
		if _, e = db.Exec(ctx, ddl); e != nil {
			t.Fatal(e)
		}
	}
	f := &connectedFixture{db: db, store: Store{db, pgpolicy}, policy: pgpolicy, durable: durable, ctx: map[string]context.Context{}, schema: schema}
	for i := 0; i < 3; i++ {
		w := "0x" + strings.ReplaceAll(uuid.NewString(), "-", "") + fmt.Sprintf("%08x", i)
		account, session := uuid.NewString(), uuid.NewString()
		hash := sha256.Sum256([]byte(session))
		now := time.Now().UTC()
		if _, e = db.Exec(ctx, `INSERT INTO identity_accounts VALUES($1,0,$2)`, account, now); e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(ctx, `INSERT INTO identity_wallet_links VALUES($1,$2,125,$3,'private','verified',$4)`, uuid.NewString(), account, w, now); e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(ctx, `INSERT INTO identity_sessions(session_id,account_id,wallet,auth_epoch,access_hash,csrf_hash,issued_at,access_expires_at,refresh_expires_at,reauthenticated_at,last_seen_at,device_label) VALUES($1,$2,$3,0,$4,$4,$5,$6,$6,$5,$5,'native')`, session, account, w, hash[:], now, now.Add(time.Hour)); e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(ctx, `INSERT INTO account_state(account_id,wallet,profile,preferences) VALUES($1,$2,'{}','{}')`, account, w); e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(ctx, `INSERT INTO interaction_preferences(wallet,inbox) VALUES($1,'everyone')`, w); e != nil {
			t.Fatal(e)
		}
		principal := fanout.Principal{Wallet: w, SessionID: session, ExpiresAt: now.Add(time.Hour)}
		principal.Validate = func(ctx context.Context) error { return pgpolicy.ValidateSession(ctx, w, session) }
		f.ctx[w] = WithIdentity(ctx, principal)
		f.wallets = append(f.wallets, w)
	}
	f.pool = "0x" + strings.Repeat("f", 40)
	if _, e = db.Exec(ctx, `INSERT INTO pools VALUES($1,$2)`, f.pool, f.wallets[1]); e != nil {
		t.Fatal(e)
	}
	redisURL := os.Getenv("L37_PHASE2_REDIS_URL")
	if redisURL == "" {
		t.Fatal("real L37_PHASE2_REDIS_URL required")
	}
	options, e := redis.ParseURL(redisURL)
	if e != nil {
		t.Fatal(e)
	}
	f.redis = redis.NewClient(options)
	t.Cleanup(func() { _ = f.redis.Close() })
	if e = f.redis.Ping(ctx).Err(); e != nil {
		t.Fatal(e)
	}
	f.http, e = NewHTTP(f.store, pgpolicy, NewViewerCache(f.redis), CommittedReceiptVerifier{DB: db, Transactions: pgx.Identifier{canonical, "canonical_transactions"}, Blocks: pgx.Identifier{canonical, "canonical_blocks"}})
	if e != nil {
		t.Fatal(e)
	}
	return f
}
func mustConnected(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func TestConnectedInteractionsPolicyErasureAndCache(t *testing.T) {
	f := connected(t)
	a, b, c := f.wallets[0], f.wallets[1], f.wallets[2]
	ctx := f.ctx[a]
	mustConnected(t, f.store.Follow(ctx, Request{Actor: a, ID: b, Key: "follow"}))
	for _, actor := range []string{a, c, a, c} {
		v, _, e := f.store.FollowStats(f.ctx[actor], b, actor, f.http.Cache)
		mustConnected(t, e)
		if v.IsFollowing != (actor == a) {
			t.Fatal("warm cache leaked viewer following state")
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/users/"+b+"/follow-stats", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	f.http.Routes().ServeHTTP(response, request)
	if response.Code != 200 || !strings.Contains(response.Header().Get("Cache-Control"), "private") {
		t.Fatalf("private real handler: %d", response.Code)
	}
	chat := fanout.DurableStore{Pool: f.db, Policy: f.policy.SendPolicy, CursorKey: []byte(strings.Repeat("k", 32))}
	_, e := chat.Send(ctx, fanout.SendInput{Actor: a, Kind: "dm", Target: b, ClientID: "before-block", Content: "hello"})
	mustConnected(t, e)
	mustConnected(t, f.policy.SetBlock(f.ctx[b], a, false))
	_, e = chat.Send(ctx, fanout.SendInput{Actor: a, Kind: "dm", Target: b, ClientID: "after-block", Content: "blocked"})
	if e != ErrDenied {
		t.Fatalf("active principal bypassed block: %v", e)
	}
	var messages, outbox int
	mustConnected(t, f.db.QueryRow(ctx, `SELECT count(*) FROM chat_messages`).Scan(&messages))
	mustConnected(t, f.db.QueryRow(ctx, `SELECT count(*) FROM chat_outbox`).Scan(&outbox))
	if messages != 1 || outbox != 1 {
		t.Fatal("denied DM persisted or emitted")
	}
	mustConnected(t, f.db.QueryRow(ctx, `SELECT count(*) FROM chat_members WHERE read_sequence<>0`).Scan(&messages))
	if messages != 0 {
		t.Fatal("denied send changed unread cursor")
	}
	root := Request{Actor: a, Pool: f.pool, ID: uuid.NewString(), Body: "safe 界", Key: "root"}
	mustConnected(t, f.store.CreateComment(ctx, root))
	wrong := root
	wrong.ID = uuid.NewString()
	wrong.Parent = root.ID
	wrong.Pool = "0x" + strings.Repeat("e", 40)
	wrong.Key = "cross"
	if e = f.store.CreateComment(ctx, wrong); e == nil {
		t.Fatal("cross-pool parent accepted")
	}
	parent := root.ID
	for depth := 2; depth <= 5; depth++ {
		r := root
		r.ID = uuid.NewString()
		r.Parent = parent
		r.Key = fmt.Sprint("depth", depth)
		e = f.store.CreateComment(ctx, r)
		if depth == 5 {
			if e != ErrDepth {
				t.Fatalf("depth5: %v", e)
			}
		} else {
			mustConnected(t, e)
		}
		parent = r.ID
	}
	mustConnected(t, f.db.QueryRow(ctx, `SELECT count(*) FROM interaction_comments WHERE id=$1`, wrong.ID).Scan(&messages))
	if messages != 0 {
		t.Fatal("invalid parent persisted")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- f.store.React(ctx, Request{Actor: a, ID: root.ID, Kind: "like", Key: fmt.Sprint("like", i)})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		mustConnected(t, err)
	}
	count, e := f.store.Count(ctx, root.ID, "like")
	mustConnected(t, e)
	if count != 1 {
		t.Fatal("like inflation")
	}
	errs = make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- f.store.React(ctx, Request{Actor: a, ID: root.ID, Kind: "like", Remove: true, Key: fmt.Sprint("unlike", i)})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		mustConnected(t, err)
	}
	count, e = f.store.Count(ctx, root.ID, "like")
	mustConnected(t, e)
	if count != 0 {
		t.Fatal("unlike counter drift")
	}
	mustConnected(t, f.store.HideComment(f.ctx[b], Request{Actor: b, Pool: f.pool, ID: root.ID, Revision: 1, Body: "pool policy", Key: "creator-hide"}))
	if _, e = f.db.Exec(ctx, `UPDATE pools SET creator=$2 WHERE pool_address=$1`, f.pool, c); e != nil {
		t.Fatal(e)
	}
	r := root
	r.ID = uuid.NewString()
	r.Key = "new-root"
	mustConnected(t, f.store.CreateComment(ctx, r))
	e = f.store.HideComment(f.ctx[b], Request{Actor: b, Pool: f.pool, ID: r.ID, Revision: 1, Body: "old creator", Key: "stale-creator"})
	if e != ErrDenied {
		t.Fatalf("transferred creator retained authority: %v", e)
	}
	edit := r
	edit.Body = "author edit"
	edit.Revision = 1
	edit.Key = "author-edit"
	mustConnected(t, f.store.EditComment(ctx, edit))
	reply := r
	reply.ID = uuid.NewString()
	reply.Parent = root.ID
	reply.Key = "deleted-parent"
	if e = f.store.CreateComment(ctx, reply); e != ErrInvalid {
		t.Fatalf("deleted parent accepted: %v", e)
	}
	if _, e = f.db.Exec(ctx, `INSERT INTO interaction_callout_authority VALUES($1,1,$2,false)`, "source-callout", a); e != nil {
		t.Fatal(e)
	}
	callout := Request{Actor: a, ID: "source-callout", Revision: 1, Key: "mention"}
	for i := 0; i < 2; i++ {
		mustConnected(t, f.store.ReferenceCallout(ctx, callout, []string{b, c}, "", f.http.Verifier))
	}
	mustConnected(t, f.db.QueryRow(ctx, `SELECT count(*) FROM interaction_notification_intents WHERE target='source-callout'`).Scan(&messages))
	if messages != 1 {
		t.Fatal("blocked/duplicate mention intent")
	}
	var recipient string
	mustConnected(t, f.db.QueryRow(ctx, `SELECT recipient FROM interaction_notification_intents WHERE target='source-callout'`).Scan(&recipient))
	if recipient != c {
		t.Fatal("wrong mention recipient")
	}
	mustConnected(t, f.db.QueryRow(ctx, `SELECT count(*) FROM `+pgx.Identifier{f.schema, "delivery_outbox"}.Sanitize()+` WHERE subject='lane37.private'`).Scan(&messages))
	if messages != 1 {
		t.Fatal("mention intent not atomically enqueued")
	}
	tx, e := f.db.Begin(ctx)
	mustConnected(t, e)
	defer rollback(tx)
	raw, e := f.db.Query(ctx, `SELECT payload FROM interaction_public_events ORDER BY revision DESC,event_id`)
	mustConnected(t, e)
	events := [][]byte{}
	for raw.Next() {
		var payload []byte
		mustConnected(t, raw.Scan(&payload))
		events = append(events, payload)
	}
	mustConnected(t, raw.Err())
	raw.Close()
	for i := 0; i < 2; i++ {
		for _, payload := range events {
			v, e := DecodePublicEvent(payload)
			mustConnected(t, e)
			typ, _ := eventType(v.Action)
			mustConnected(t, ProjectPublicEvent(ctx, tx, "replay", delivery.Envelope{EventType: typ, Payload: payload}))
		}
	}
	mustConnected(t, tx.Commit(ctx))
	mustConnected(t, f.db.QueryRow(ctx, `SELECT count(*) FROM interaction_feed_projection WHERE generation='replay' AND NOT removed AND convert_from(payload,'UTF8')::jsonb->>'action'='comment'`).Scan(&messages))
	if messages != 4 {
		t.Fatalf("projection count mismatch: %d", messages)
	}
	caseID := uuid.NewString()
	tx, e = f.db.Begin(ctx)
	mustConnected(t, e)
	defer rollback(tx)
	mustConnected(t, f.store.BeginErasureTx(ctx, tx, ErasureCommand{CaseID: caseID, Wallet: a, PolicyVersion: "social-retention-v1"}))
	mustConnected(t, tx.Commit(ctx))
	for i := 0; i < 30; i++ {
		receipt, e := f.store.EraseBatch(ctx, caseID)
		mustConnected(t, e)
		if receipt.State == "completed" {
			break
		}
		if i == 29 {
			t.Fatal("erasure did not resume to completion")
		}
	}
	tx, e = f.db.Begin(ctx)
	mustConnected(t, e)
	defer rollback(tx)
	for _, payload := range events {
		v, e := DecodePublicEvent(payload)
		mustConnected(t, e)
		typ, _ := eventType(v.Action)
		mustConnected(t, ProjectPublicEvent(ctx, tx, "restored", delivery.Envelope{EventType: typ, Payload: payload}))
	}
	mustConnected(t, tx.Commit(ctx))
	mustConnected(t, f.db.QueryRow(ctx, `SELECT count(*) FROM interaction_feed_projection WHERE generation='restored' AND actor=$1 AND NOT removed`, a).Scan(&messages))
	if messages != 0 {
		t.Fatal("archive replay resurrected erased content")
	}
	visible, _, e := f.http.comments(ctx, f.pool, 100, "")
	mustConnected(t, e)
	if len(visible) != 0 {
		t.Fatal("public API resurrected erased comments")
	}
	if e = f.store.Follow(ctx, Request{Actor: a, ID: c, Key: "after-erasure"}); e != ErrDenied {
		t.Fatalf("deleted wallet mutated: %v", e)
	}
	t.Log("connected actor cache, real transaction policy, blocks, comments, creator transfer, concurrency, mentions, erasure and replay reconciled")
}
func TestConnectedBoundedEqualTimestampTraversal(t *testing.T) {
	f := connected(t)
	ctx := f.ctx[f.wallets[0]]
	owner := f.wallets[0]
	_, e := f.db.Exec(ctx, `INSERT INTO interaction_follows SELECT '0x'||lpad(to_hex(i),40,'0'),$1,'2026-01-01'::timestamptz FROM generate_series(3,100002)i`, owner)
	mustConnected(t, e)
	seen := map[string]bool{}
	cursor := ""
	for {
		page, e := f.http.visibleFollows(ctx, owner, false, 100, cursor)
		mustConnected(t, e)
		if len(page.Edges) > 100 {
			t.Fatal("unbounded page")
		}
		for _, edge := range page.Edges {
			if seen[edge.Wallet] {
				t.Fatal("duplicate equal-time edge")
			}
			seen[edge.Wallet] = true
		}
		cursor = page.Next
		if cursor == "" {
			break
		}
	}
	if len(seen) != 100000 {
		t.Fatalf("missing edges: %d", len(seen))
	}
}
func TestConnectedDurableBrokerInbox(t *testing.T) {
	f := connected(t)
	url := os.Getenv("L37_PHASE2_NATS_URL")
	if url == "" {
		t.Fatal("real file-backed L37_PHASE2_NATS_URL required")
	}
	nc, e := nats.Connect(url, nats.NoReconnect(), nats.Timeout(time.Second))
	mustConnected(t, e)
	t.Cleanup(nc.Close)
	js, e := jetstream.New(nc)
	mustConnected(t, e)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stream := f.schema + "_feed"
	_, e = js.CreateStream(ctx, jetstream.StreamConfig{Name: stream, Subjects: []string{"lane37.public"}, Storage: jetstream.FileStorage, Replicas: 1, MaxAge: 72 * time.Hour, Duplicates: time.Minute})
	mustConnected(t, e)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := js.DeleteStream(cleanup, stream); e != nil {
			t.Error(e)
		}
	})
	cfg := natsdriver.ConsumerConfig{Stream: stream, Durable: "l37_projection", Subject: "lane37.public", Batch: 16, MaxAckPending: 64, MaxWaiting: 1, MaxDeliver: 10, FetchWait: 250 * time.Millisecond, AckWait: time.Second}
	brokerCfg, e := cfg.BrokerConsumerConfig()
	mustConnected(t, e)
	_, e = js.CreateConsumer(ctx, stream, brokerCfg)
	mustConnected(t, e)
	consumer, e := natsdriver.NewConsumer(ctx, nc, cfg)
	mustConnected(t, e)
	t.Cleanup(consumer.Close)
	publisher, e := natsdriver.NewPublisher(nc, stream)
	mustConnected(t, e)
	a, b := f.wallets[0], f.wallets[1]
	mustConnected(t, f.store.Follow(f.ctx[a], Request{Actor: a, ID: b, Key: "broker-follow"}))
	mustConnected(t, f.store.Follow(f.ctx[a], Request{Actor: a, ID: b, Key: "broker-unfollow", Remove: true}))
	leases, e := f.durable.Claim(ctx, "l37_native", 10, time.Minute)
	mustConnected(t, e)
	if len(leases) != 2 {
		t.Fatalf("expected two durable changes, got%d", len(leases))
	}
	for i := len(leases) - 1; i >= 0; i-- {
		lease := leases[i]
		ack, e := publisher.Publish(ctx, lease.Subject, lease.EventID, lease.Wire)
		mustConnected(t, e)
		mustConnected(t, f.durable.MarkPublished(ctx, lease, ack))
		_, e = js.Publish(ctx, lease.Subject, lease.Wire, jetstream.WithMsgID("redelivery_"+hex.EncodeToString([]byte(fmt.Sprint(i)))))
		mustConnected(t, e)
	}
	processed := 0
	for processed < 4 {
		messages, e := consumer.Fetch(ctx)
		mustConnected(t, e)
		for _, message := range messages {
			mustConnected(t, ConsumePublic(ctx, f.durable, "broker_replay", message.Subject, message.Wire, message))
			processed++
		}
	}
	var active, inbox int
	mustConnected(t, f.db.QueryRow(ctx, `SELECT count(*) FROM interaction_feed_projection WHERE generation='broker_replay' AND NOT removed`).Scan(&active))
	if active != 0 {
		t.Fatal("out-of-order replay restored removed follow")
	}
	mustConnected(t, f.db.QueryRow(ctx, `SELECT count(*) FROM `+pgx.Identifier{f.schema, "delivery_inbox"}.Sanitize()+` WHERE consumer_id='social_feed'`).Scan(&inbox))
	if inbox != 2 {
		t.Fatalf("inbox deduplication: %d", inbox)
	}
}

func TestConnectedAdversarialRemovalReplayAndContent(t *testing.T) {
	f := connected(t)
	a, b := f.wallets[0], f.wallets[1]
	ctx := f.ctx[a]
	for i := 0; i < 12; i++ {
		id := uuid.NewString()
		mustConnected(t, f.store.CreateComment(ctx, Request{Actor: a, Pool: f.pool, ID: id, Body: "original", Key: "create-" + id}))
		start := make(chan struct{})
		results := make(chan struct {
			kind string
			err  error
		}, 2)
		go func() {
			<-start
			results <- struct {
				kind string
				err  error
			}{"edit", f.store.EditComment(ctx, Request{Actor: a, ID: id, Body: "racing edit", Revision: 1, Key: "edit-" + id})}
		}()
		go func() {
			<-start
			results <- struct {
				kind string
				err  error
			}{"hide", f.store.HideComment(f.ctx[b], Request{Actor: b, Pool: f.pool, ID: id, Body: "community policy", Revision: 1, Key: "hide-" + id})}
		}()
		close(start)
		won := ""
		for n := 0; n < 2; n++ {
			result := <-results
			if result.err == nil {
				if won != "" {
					t.Fatal("both expected-revision mutations committed")
				}
				won = result.kind
			} else if !errors.Is(result.err, ErrRevision) {
				t.Fatal(result.err)
			}
		}
		if won == "" {
			t.Fatal("neither racing mutation committed")
		}
		if won == "edit" {
			mustConnected(t, f.store.HideComment(f.ctx[b], Request{Actor: b, Pool: f.pool, ID: id, Body: "community policy", Revision: 2, Key: "hide-latest-" + id}))
		}
		if err := f.store.EditComment(ctx, Request{Actor: a, ID: id, Body: "resurrection", Revision: 1, Key: "stale-" + id}); !errors.Is(err, ErrRevision) {
			t.Fatalf("stale edit: %v", err)
		}
		var removed bool
		var body string
		mustConnected(t, f.db.QueryRow(ctx, `SELECT removed,body FROM interaction_comments WHERE id=$1`, id).Scan(&removed, &body))
		if !removed || body != "" {
			t.Fatal("race resurrected removed text")
		}
	}
	for i, body := range []string{"<script>alert(1)</script>", "javascript:alert(1)", strings.Repeat("界", 2000), strings.Repeat("😀", 2000), "a\n\tb"} {
		mustConnected(t, f.store.CreateComment(ctx, Request{Actor: a, Pool: f.pool, ID: uuid.NewString(), Body: body, Key: fmt.Sprint("safe-corpus-", i)}))
	}
	for i, body := range []string{"", " \n\t", "x\x00", "x\u202e", "x\u2066", "x\u200b", string([]byte{255}), strings.Repeat("界", 2001), strings.Repeat("😀", 2001)} {
		id := uuid.NewString()
		if err := f.store.CreateComment(ctx, Request{Actor: a, Pool: f.pool, ID: id, Body: body, Key: fmt.Sprint("unsafe-corpus-", i)}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("corpus %d: %v", i, err)
		}
		var rows int
		mustConnected(t, f.db.QueryRow(ctx, `SELECT count(*) FROM interaction_comments WHERE id=$1`, id).Scan(&rows))
		if rows != 0 {
			t.Fatal("rejected corpus persisted")
		}
	}
	mustConnected(t, f.store.Follow(ctx, Request{Actor: a, ID: b, Key: "replay-follow"}))
	stats, tag, err := f.store.FollowStats(ctx, b, a, f.http.Cache)
	mustConnected(t, err)
	mustConnected(t, f.store.Follow(ctx, Request{Actor: a, ID: b, Key: "replay-follow-noop"}))
	again, nextTag, err := f.store.FollowStats(ctx, b, a, f.http.Cache)
	mustConnected(t, err)
	if stats != again || tag != nextTag {
		t.Fatal("duplicate mutation changed snapshot or ETag")
	}
	request := httptest.NewRequest(http.MethodGet, "/users/"+b+"/follow-stats", nil).WithContext(ctx)
	request.Header.Set("If-None-Match", tag)
	response := httptest.NewRecorder()
	f.http.Routes().ServeHTTP(response, request)
	if response.Code != http.StatusNotModified || response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("conditional privacy: %d %v", response.Code, response.Header())
	}
	rows, err := f.db.Query(ctx, `SELECT payload FROM interaction_public_events ORDER BY revision DESC,event_id DESC`)
	mustConnected(t, err)
	payloads := [][]byte{}
	for rows.Next() {
		var payload []byte
		mustConnected(t, rows.Scan(&payload))
		payloads = append(payloads, payload)
	}
	mustConnected(t, rows.Err())
	rows.Close()
	tx, err := f.db.Begin(ctx)
	mustConnected(t, err)
	defer rollback(tx)
	for repeat := 0; repeat < 3; repeat++ {
		for _, payload := range payloads {
			v, err := DecodePublicEvent(payload)
			mustConnected(t, err)
			typ, _ := eventType(v.Action)
			mustConnected(t, ProjectPublicEvent(ctx, tx, "adversarial", delivery.Envelope{EventType: typ, Payload: payload}))
		}
	}
	mustConnected(t, tx.Commit(ctx))
	var authoritative, projected int64
	mustConnected(t, f.db.QueryRow(ctx, `SELECT count(*) FROM interaction_comments WHERE NOT removed`).Scan(&authoritative))
	mustConnected(t, f.db.QueryRow(ctx, `SELECT count(*) FROM interaction_feed_projection WHERE generation='adversarial' AND NOT removed AND convert_from(payload,'UTF8')::jsonb->>'action'='comment'`).Scan(&projected))
	if authoritative != projected {
		t.Fatalf("comment replay drift: %d %d", authoritative, projected)
	}
	mustConnected(t, f.db.QueryRow(ctx, `SELECT count(*) FROM interaction_follows`).Scan(&authoritative))
	mustConnected(t, f.db.QueryRow(ctx, `SELECT count(*) FROM interaction_feed_projection WHERE generation='adversarial' AND NOT removed AND convert_from(payload,'UTF8')::jsonb->>'action'='follow'`).Scan(&projected))
	if authoritative != projected {
		t.Fatalf("edge replay drift: %d %d", authoritative, projected)
	}
	visible, err := f.store.VisibleProjection(ctx, "adversarial", "", 100)
	mustConnected(t, err)
	for _, v := range visible {
		if v.Action == "comment_hide" || strings.Contains(v.Body, "community policy") {
			t.Fatal("private moderation reason reached public feed")
		}
	}
	for _, payload := range payloads {
		v, err := DecodePublicEvent(payload)
		mustConnected(t, err)
		if v.Action != "comment" || v.Removed || v.Revision != 1 {
			continue
		}
		v.Body = "same revision conflicting body"
		wire, err := json.Marshal(v)
		mustConnected(t, err)
		tx, err := f.db.Begin(ctx)
		mustConnected(t, err)
		err = ProjectPublicEvent(ctx, tx, "adversarial", delivery.Envelope{EventType: "social.comment.changed", Payload: wire})
		rollback(tx)
		if !errors.Is(err, delivery.ErrIntegrity) {
			t.Fatalf("conflicting replay identity: %v", err)
		}
		break
	}
	_, err = f.db.Exec(ctx, `INSERT INTO interaction_callout_authority VALUES('private-topic-check',1,$1,false)`, a)
	mustConnected(t, err)
	originalSubject := f.policy.Publisher.NotificationSubject
	f.policy.Publisher.NotificationSubject = "lane37.public"
	err = f.store.ReferenceCallout(ctx, Request{Actor: a, ID: "private-topic-check", Revision: 1, Key: "private-topic-check"}, []string{b}, "", f.http.Verifier)
	f.policy.Publisher.NotificationSubject = originalSubject
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("private/public topic mixing: %v", err)
	}
	var intents int
	mustConnected(t, f.db.QueryRow(ctx, `SELECT count(*) FROM interaction_notification_intents WHERE target='private-topic-check'`).Scan(&intents))
	if intents != 0 {
		t.Fatal("private topic rejection did not roll back intent")
	}
	malicious := PublicEvent{AggregateID: "follow:" + a + ":" + b, Revision: 1, Actor: a, Action: "follow", Target: b, Body: "private DM body"}
	raw, err := json.Marshal(malicious)
	mustConnected(t, err)
	if _, err = DecodePublicEvent(raw); !errors.Is(err, ErrInvalid) {
		t.Fatalf("private edge payload accepted: %v", err)
	}
}

func TestConnectedErasureBoundedRestoreAndHold(t *testing.T) {
	f := connected(t)
	a, b := f.wallets[0], f.wallets[1]
	ctx := f.ctx[a]
	comment := uuid.NewString()
	mustConnected(t, f.store.CreateComment(ctx, Request{Actor: a, Pool: f.pool, ID: comment, Body: "backup body", Key: "backup-comment"}))
	_, err := f.db.Exec(ctx, `INSERT INTO interaction_follows SELECT $1,'0x'||lpad(to_hex(i),40,'0'),'2026-01-01'::timestamptz FROM generate_series(3,1007)i`, a)
	mustConnected(t, err)
	until := time.Now().UTC().Add(time.Hour)
	command := ErasureCommand{CaseID: uuid.NewString(), Wallet: a, PolicyVersion: "social-retention-v1", HoldUntil: &until}
	for retry := 0; retry < 2; retry++ {
		tx, err := f.db.Begin(ctx)
		mustConnected(t, err)
		mustConnected(t, f.store.BeginErasureTx(ctx, tx, command))
		mustConnected(t, tx.Commit(ctx))
	}
	if _, err = f.store.Follows(ctx, a, true, 100, ""); !errors.Is(err, ErrDenied) {
		t.Fatalf("erased direct graph read: %v", err)
	}
	first, err := f.store.EraseBatch(ctx, command.CaseID)
	mustConnected(t, err)
	if first.RowsProcessed != 1000 {
		t.Fatalf("unbounded erasure batch: %d", first.RowsProcessed)
	}
	var remaining int
	mustConnected(t, f.db.QueryRow(ctx, `SELECT count(*) FROM interaction_follows WHERE follower=$1`, a).Scan(&remaining))
	if remaining != 5 {
		t.Fatalf("batch resume remaining: %d", remaining)
	}
	retained := false
	for i := 0; i < 30; i++ {
		receipt, err := f.store.EraseBatch(ctx, command.CaseID)
		mustConnected(t, err)
		if receipt.State == "completed" {
			t.Fatal("safety hold bypassed")
		}
		if receipt.State == "retained" {
			retained = true
			break
		}
	}
	if !retained {
		t.Fatal("erasure did not reach safety hold")
	}
	_, err = f.db.Exec(ctx, `UPDATE interaction_erasure_jobs SET hold_until=clock_timestamp()-interval '1 second' WHERE deletion_case_id=$1`, command.CaseID)
	mustConnected(t, err)
	completed := false
	for i := 0; i < 15; i++ {
		receipt, err := f.store.EraseBatch(ctx, command.CaseID)
		mustConnected(t, err)
		if receipt.State == "completed" {
			completed = true
			break
		}
	}
	if !completed {
		t.Fatal("erasure did not resume after hold")
	}
	_, err = f.db.Exec(ctx, `UPDATE interaction_comments SET body='restored backup',removed=false,revision=1 WHERE id=$1`, comment)
	mustConnected(t, err)
	_, err = f.db.Exec(ctx, `INSERT INTO interaction_follows(follower,followee) VALUES($1,$2)`, a, b)
	mustConnected(t, err)
	visible, _, err := f.http.comments(ctx, f.pool, 100, "")
	mustConnected(t, err)
	if len(visible) != 0 {
		t.Fatal("restore resurrected erased comment")
	}
	page, err := f.store.Follows(f.ctx[b], b, false, 100, "")
	mustConnected(t, err)
	for _, edge := range page.Edges {
		if edge.Wallet == a {
			t.Fatal("restore resurrected erased edge")
		}
	}
	if _, err = f.http.comment(ctx, comment); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("restored direct comment visibility: %v", err)
	}
	if err = f.store.CreateComment(ctx, Request{Actor: a, Pool: f.pool, ID: uuid.NewString(), Body: "after erasure", Key: "erased-create"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("erased mutation: %v", err)
	}
}

type phase4Allocation struct {
	ID           string `json:"allocation_id"`
	Mode         string `json:"mode"`
	ReadRPS      int    `json:"read_rps"`
	WriteRPS     int    `json:"write_rps"`
	Seconds      int    `json:"duration_seconds"`
	Concurrency  int    `json:"concurrency"`
	OriginRegion string `json:"origin_region"`
	SourceSHA256 string `json:"source_sha256"`
}

type phase4Sample struct {
	ID        int       `json:"id"`
	Operation string    `json:"operation"`
	Scheduled time.Time `json:"scheduled_at"`
	Started   time.Time `json:"started_at"`
	Finished  time.Time `json:"finished_at"`
	QueueNS   int64     `json:"queue_ns"`
	LatencyNS int64     `json:"latency_ns"`
	ServiceNS int64     `json:"service_ns"`
	Status    int       `json:"http_status,omitempty"`
	Bytes     int       `json:"response_bytes,omitempty"`
	Admitted  bool      `json:"admitted"`
	Error     string    `json:"error_class,omitempty"`
}

type phase4Capture struct {
	mu           sync.Mutex
	file         *os.File
	latencies    map[string][]int64
	samples      int
	failures     int
	readSuccess  int
	writeSuccess int
}

func (c *phase4Capture) record(v phase4Sample) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := json.NewEncoder(c.file).Encode(v); err != nil {
		return err
	}
	c.samples++
	if !v.Admitted {
		c.failures++
	} else if strings.HasPrefix(v.Operation, "read_") {
		c.readSuccess++
	} else {
		c.writeSuccess++
	}
	if strings.HasPrefix(v.Operation, "read_") {
		c.latencies[v.Operation] = append(c.latencies[v.Operation], v.LatencyNS)
	}
	return nil
}

func phase4WriteJSON(t *testing.T, dir, name string, v any) {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	mustConnected(t, err)
	file, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	mustConnected(t, err)
	_, err = file.Write(append(raw, '\n'))
	if err != nil {
		file.Close()
		t.Fatal(err)
	}
	mustConnected(t, file.Sync())
	mustConnected(t, file.Close())
}

func phase4SourceBinding(t *testing.T) (string, map[string]string) {
	t.Helper()
	paths := []string{
		"media/social/internal/interactions/cache.go", "media/social/internal/interactions/erasure.go", "media/social/internal/interactions/http.go", "media/social/internal/interactions/integration_test.go", "media/social/internal/interactions/outbox.go", "media/social/internal/interactions/policy.go", "media/social/internal/interactions/projection.go", "media/social/internal/interactions/store.go", "media/social/internal/interactions/store_test.go", "media/social/internal/social/comments.go", "media/social/internal/social/follows.go",
		"client/apps/web/src/widgets/community-interactions/interaction-client.ts", "client/apps/web/src/widgets/community-interactions/interaction-controls.test.tsx", "client/apps/web/src/widgets/community-interactions/interaction-controls.tsx", "client/apps/web/src/widgets/community-interactions/plain-content.tsx", "client/apps/web/src/widgets/community-interactions/privacy-settings.tsx", "client/apps/web/src/widgets/community-interactions/tsconfig.json", "client/apps/web/src/widgets/community-interactions/vitest.config.mts",
	}
	sort.Strings(paths)
	hashes := map[string]string{}
	binding := sha256.New()
	for _, path := range paths {
		raw, err := os.ReadFile(filepath.Join("../../../..", path))
		mustConnected(t, err)
		hash := sha256.Sum256(raw)
		value := hex.EncodeToString(hash[:])
		hashes[path] = value
		fmt.Fprintf(binding, "%s\x00%s\n", path, value)
	}
	return hex.EncodeToString(binding.Sum(nil)), hashes
}

func phase4Quantile(values []int64, percent int) int64 {
	copyValues := append([]int64(nil), values...)
	sort.Slice(copyValues, func(i, j int) bool { return copyValues[i] < copyValues[j] })
	return copyValues[(len(copyValues)*percent+99)/100-1]
}

func phase4IndexPlan(v any) bool {
	object, ok := v.(map[string]any)
	if !ok {
		return false
	}
	if table, _ := object["Relation Name"].(string); table == "interaction_follows" {
		kind, _ := object["Node Type"].(string)
		if kind == "Index Scan" || kind == "Index Only Scan" {
			return true
		}
	}
	if children, ok := object["Plans"].([]any); ok {
		for _, child := range children {
			if phase4IndexPlan(child) {
				return true
			}
		}
	}
	return false
}

func TestPhase4InteractionsMeasurement(t *testing.T) {
	allocationPath := os.Getenv("L37_PHASE4_ALLOCATION_PATH")
	allocationHash := os.Getenv("L37_PHASE4_ALLOCATION_SHA256")
	dir := os.Getenv("L37_PHASE4_ARTIFACT_DIR")
	if !filepath.IsAbs(allocationPath) || !filepath.IsAbs(dir) || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(allocationHash) {
		t.Fatal("pinned allocation path/hash and absolute existing artifact directory required")
	}
	resolved, err := filepath.EvalSymlinks(dir)
	mustConnected(t, err)
	if resolved != filepath.Clean(dir) {
		t.Fatal("artifact directory must not traverse a symlink")
	}
	raw, err := os.ReadFile(allocationPath)
	mustConnected(t, err)
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != allocationHash {
		t.Fatal("allocation digest mismatch")
	}
	var allocation phase4Allocation
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	mustConnected(t, decoder.Decode(&allocation))
	if decoder.Decode(&struct{}{}) != io.EOF {
		t.Fatal("trailing allocation bytes")
	}
	if allocation.ID == "" || allocation.Mode != "isolated_origin_harness" || allocation.OriginRegion == "" || allocation.ReadRPS < 1 || allocation.WriteRPS < 1 || allocation.WriteRPS > 2000 || allocation.ReadRPS+allocation.WriteRPS > 10000 || allocation.Seconds < 1 || allocation.Seconds > 600 || allocation.Concurrency < 2 || allocation.Concurrency > 128 || allocation.ReadRPS*allocation.Seconds < 300 || (allocation.ReadRPS+allocation.WriteRPS)*allocation.Seconds > 2000000 {
		t.Fatal("invalid or missing allocation; steady HTTP/write budgets are 10000/2000 per second")
	}
	sourceDigest, sourceHashes := phase4SourceBinding(t)
	if allocation.SourceSHA256 != sourceDigest {
		t.Fatal("allocation targets another source candidate")
	}
	executable, err := os.Executable()
	mustConnected(t, err)
	binary, err := os.ReadFile(executable)
	mustConnected(t, err)
	buildHash := sha256.Sum256(binary)
	phase4WriteJSON(t, dir, "binding.json", map[string]any{"source_sha256": sourceDigest, "source_files": sourceHashes, "executable_sha256": hex.EncodeToString(buildHash[:]), "allocation_sha256": allocationHash, "allocation": allocation, "measurement_boundary": "isolated real origin HTTP and transactional Store mutation paths; shared release acceptance requires serial9.1"})
	f := connected(t)
	ctx, cancelMeasurement := context.WithTimeout(f.ctx[f.wallets[0]], 25*time.Minute)
	defer cancelMeasurement()
	a, b, c := f.wallets[0], f.wallets[1], f.wallets[2]
	mustConnected(t, f.store.Follow(ctx, Request{Actor: a, ID: b, Key: "phase4-viewer-follow"}))
	_, err = f.db.Exec(ctx, `INSERT INTO interaction_follows SELECT '0x'||lpad(to_hex(i),40,'0'),$1,'2026-01-01'::timestamptz FROM generate_series(3,100002)i`, b)
	mustConnected(t, err)
	_, err = f.db.Exec(ctx, `ANALYZE interaction_follows`)
	mustConnected(t, err)
	var planRaw []byte
	mustConnected(t, f.db.QueryRow(ctx, `EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT f.follower,f.created_at FROM interaction_follows f WHERE f.followee=$1 AND (f.created_at,f.follower)<($2,$3) AND NOT EXISTS(SELECT 1 FROM interaction_erased_wallets e WHERE e.wallet=f.follower) ORDER BY f.created_at DESC,f.follower DESC LIMIT 101`, b, time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), "zz").Scan(&planRaw))
	var plan []map[string]any
	mustConnected(t, json.Unmarshal(planRaw, &plan))
	phase4WriteJSON(t, dir, "follower-index-plan.json", plan)
	if len(plan) != 1 || !phase4IndexPlan(plan[0]["Plan"]) {
		t.Fatal("hot follower bounded query lacks an actual graph index scan")
	}
	rootPlan, ok := plan[0]["Plan"].(map[string]any)
	if !ok {
		t.Fatal("missing actual plan")
	}
	actualRows, ok := rootPlan["Actual Rows"].(float64)
	if !ok || rootPlan["Node Type"] != "Limit" || actualRows > 101 {
		t.Fatal("graph plan violates bounded LIMIT")
	}
	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		page, err := f.store.Follows(ctx, b, false, 100, cursor)
		mustConnected(t, err)
		if len(page.Edges) > 100 {
			t.Fatal("unbounded graph page")
		}
		pages++
		for _, edge := range page.Edges {
			if seen[edge.Wallet] {
				t.Fatal("duplicate equal-time cursor edge")
			}
			seen[edge.Wallet] = true
		}
		cursor = page.Next
		if cursor == "" {
			break
		}
	}
	if len(seen) != 100001 {
		t.Fatalf("missing hot graph edges: %d", len(seen))
	}
	phase4WriteJSON(t, dir, "traversal.json", map[string]any{"edges": len(seen), "pages": pages, "page_cap": 100, "duplicate_edges": 0, "missing_edges": 100001 - len(seen)})
	root := uuid.NewString()
	mustConnected(t, f.store.CreateComment(ctx, Request{Actor: a, Pool: f.pool, ID: root, Body: "popular pool measurement", Key: "phase4-root"}))
	server := httptest.NewServer(f.http.Routes())
	defer server.Close()
	client := server.Client()
	client.Timeout = 2 * time.Second
	defer client.CloseIdleConnections()
	file, err := os.OpenFile(filepath.Join(dir, "raw-samples.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	mustConnected(t, err)
	defer file.Close()
	capture := &phase4Capture{file: file, latencies: map[string][]int64{}}
	type job struct {
		id        int
		scheduled time.Time
		operation string
	}
	jobs := make(chan job, allocation.Concurrency)
	var workers sync.WaitGroup
	totalRPS := allocation.ReadRPS + allocation.WriteRPS
	total := totalRPS * allocation.Seconds
	start := time.Now()
	var beforeMemory runtime.MemStats
	runtime.ReadMemStats(&beforeMemory)
	beforeDB := f.db.Stat()
	for worker := 0; worker < allocation.Concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for work := range jobs {
				began := time.Now()
				sample := phase4Sample{ID: work.id, Operation: work.operation, Scheduled: work.scheduled.UTC(), Started: began.UTC(), QueueNS: began.Sub(work.scheduled).Nanoseconds()}
				var operationErr error
				operationCtx, cancelOperation := context.WithTimeout(ctx, 2*time.Second)
				switch work.operation {
				case "write_comment":
					operationErr = f.store.CreateComment(operationCtx, Request{Actor: a, Pool: f.pool, ID: uuid.NewString(), Body: "allocated comment traffic", Key: fmt.Sprint("phase4-comment-", work.id)})
				case "write_reaction":
					operationErr = f.store.React(operationCtx, Request{Actor: a, ID: root, Kind: "like", Remove: work.id%2 == 0, Key: fmt.Sprint("phase4-reaction-", work.id)})
				default:
					path := "/users/" + b + "/followers?limit=100"
					if work.operation == "read_comments" {
						path = "/pool/" + f.pool + "/comments?limit=100"
					}
					if work.operation == "read_stats" {
						path = "/users/" + b + "/follow-stats"
					}
					response, e := client.Get(server.URL + path)
					operationErr = e
					if e == nil {
						sample.Status = response.StatusCode
						payload, e := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
						closeErr := response.Body.Close()
						sample.Bytes = len(payload)
						if e != nil {
							operationErr = e
						} else if closeErr != nil {
							operationErr = closeErr
						} else if response.StatusCode != 200 {
							operationErr = fmt.Errorf("http_%d", response.StatusCode)
						} else if len(payload) > 1<<20 {
							operationErr = ErrInvalid
						} else if response.Header.Get("Cache-Control") != "private, no-store" {
							operationErr = ErrDenied
						} else {
							var envelope struct {
								Data json.RawMessage `json:"data"`
							}
							if json.Unmarshal(payload, &envelope) != nil {
								operationErr = ErrInvalid
							} else if work.operation != "read_stats" {
								var page struct {
									Items []json.RawMessage `json:"items"`
								}
								if json.Unmarshal(envelope.Data, &page) != nil || len(page.Items) > 100 {
									operationErr = ErrInvalid
								}
							}
						}
					}
				}
				cancelOperation()
				finished := time.Now()
				sample.Finished = finished.UTC()
				sample.LatencyNS = finished.Sub(work.scheduled).Nanoseconds()
				sample.ServiceNS = finished.Sub(began).Nanoseconds()
				sample.Admitted = operationErr == nil
				if operationErr != nil {
					sample.Error = operationErr.Error()
				}
				if e := capture.record(sample); e != nil {
					t.Error(e)
				}
			}
		}()
	}
	readIndex, writeIndex := 0, 0
	for i := 0; i < total; i++ {
		scheduled := start.Add(time.Duration(i) * time.Second / time.Duration(totalRPS))
		if delay := time.Until(scheduled); delay > 0 {
			timer := time.NewTimer(delay)
			<-timer.C
		}
		operation := ""
		if (i+1)*allocation.WriteRPS/totalRPS > i*allocation.WriteRPS/totalRPS {
			operation = "write_reaction"
			if writeIndex%2 == 0 {
				operation = "write_comment"
			}
			writeIndex++
		} else {
			operation = []string{"read_followers", "read_comments", "read_stats"}[readIndex%3]
			readIndex++
		}
		select {
		case jobs <- job{i, scheduled, operation}:
		default:
			now := time.Now()
			mustConnected(t, capture.record(phase4Sample{ID: i, Operation: operation, Scheduled: scheduled.UTC(), Started: now.UTC(), Finished: now.UTC(), QueueNS: now.Sub(scheduled).Nanoseconds(), LatencyNS: now.Sub(scheduled).Nanoseconds(), Error: "generator_queue_full"}))
		}
	}
	close(jobs)
	workers.Wait()
	mustConnected(t, file.Sync())
	mustConnected(t, file.Close())
	elapsed := time.Since(start)
	var afterMemory runtime.MemStats
	runtime.ReadMemStats(&afterMemory)
	latencies := map[string]any{}
	for _, operation := range []string{"read_followers", "read_comments", "read_stats"} {
		values := capture.latencies[operation]
		if len(values) < 100 {
			t.Errorf("insufficient percentile population for %s: %d", operation, len(values))
			continue
		}
		p95, p99 := phase4Quantile(values, 95), phase4Quantile(values, 99)
		latencies[operation] = map[string]any{"samples": len(values), "p95_ns": p95, "p99_ns": p99}
		if p95 > int64(150*time.Millisecond) || p99 > int64(400*time.Millisecond) {
			t.Errorf("%s origin SLO: p95=%dns p99=%dns", operation, p95, p99)
		}
	}
	afterDB := f.db.Stat()
	summary := map[string]any{"elapsed_ns": elapsed.Nanoseconds(), "offered_read_rps": allocation.ReadRPS, "offered_write_rps": allocation.WriteRPS, "captured_samples": capture.samples, "failed_samples": capture.failures, "completion_rps": float64(capture.samples-capture.failures) / elapsed.Seconds(), "admitted_read_rps": float64(capture.readSuccess) / elapsed.Seconds(), "admitted_write_rps": float64(capture.writeSuccess) / elapsed.Seconds(), "latencies": latencies, "db_connection_cap": afterDB.MaxConns(), "db_acquire_delta": afterDB.AcquireCount() - beforeDB.AcquireCount(), "db_acquire_wait_ns": (afterDB.AcquireDuration() - beforeDB.AcquireDuration()).Nanoseconds(), "heap_alloc_bytes_before": beforeMemory.HeapAlloc, "heap_alloc_bytes_after": afterMemory.HeapAlloc, "allocation_bytes": afterMemory.TotalAlloc - beforeMemory.TotalAlloc}
	phase4WriteJSON(t, dir, "load-summary.json", summary)
	if capture.samples != total || capture.failures != 0 {
		t.Errorf("incomplete admitted workload: samples=%d expected=%d failures=%d", capture.samples, total, capture.failures)
	}
	for _, viewer := range []string{a, c, a, c} {
		stats, _, err := f.store.FollowStats(f.ctx[viewer], b, viewer, f.http.Cache)
		mustConnected(t, err)
		if stats.IsFollowing != (viewer == a) {
			t.Fatal("warm cache viewer privacy drift")
		}
	}
	phase4WriteJSON(t, dir, "privacy.json", map[string]any{"viewer_checks": 4, "privacy_drift": 0})
	var revision, overlay int64
	mustConnected(t, f.db.QueryRow(ctx, `SELECT COALESCE((SELECT revision FROM interaction_visibility_revision WHERE scope=$1),0),COALESCE((SELECT revision FROM interaction_visibility_revision WHERE scope='erasure_overlay'),0)`, "wallet:"+b).Scan(&revision, &overlay))
	cacheKey := CacheKey(b, "", "public_profile:"+strconv.FormatInt(overlay, 10), revision)
	mustConnected(t, f.redis.Del(ctx, cacheKey).Err())
	beforeCache := f.http.Cache.Metrics()
	barrier := make(chan struct{})
	var burst sync.WaitGroup
	for i := 0; i < allocation.Concurrency; i++ {
		burst.Add(1)
		go func() {
			defer burst.Done()
			<-barrier
			_, _, err := f.store.FollowStats(ctx, b, "", f.http.Cache)
			if err != nil {
				t.Error(err)
			}
		}()
	}
	close(barrier)
	burst.Wait()
	afterCache := f.http.Cache.Metrics()
	phase4WriteJSON(t, dir, "cache-stampede.json", map[string]any{"requests": allocation.Concurrency, "before": beforeCache, "after": afterCache, "origin_fills": afterCache.OriginFills - beforeCache.OriginFills, "coalesced_waiters": afterCache.CoalescedWaiters - beforeCache.CoalescedWaiters})
	if afterCache.ActiveFills != 0 || afterCache.PeakFills > 32 || afterCache.OriginFills-beforeCache.OriginFills >= int64(allocation.Concurrency) || afterCache.CoalescedWaiters == beforeCache.CoalescedWaiters {
		t.Error("stampede did not coalesce or exceeded fill admission")
	}
	var replayed int64
	eventCursor := ""
	replayStarted := time.Now()
	for {
		rows, err := f.db.Query(ctx, `SELECT event_id,payload FROM interaction_public_events WHERE event_id>$1 ORDER BY event_id LIMIT 100`, eventCursor)
		mustConnected(t, err)
		type event struct {
			id      string
			payload []byte
		}
		events := []event{}
		for rows.Next() {
			var v event
			mustConnected(t, rows.Scan(&v.id, &v.payload))
			events = append(events, v)
		}
		mustConnected(t, rows.Err())
		rows.Close()
		if len(events) == 0 {
			break
		}
		for i := len(events) - 1; i >= 0; i-- {
			v := events[i]
			decoded, err := DecodePublicEvent(v.payload)
			mustConnected(t, err)
			typ, _ := eventType(decoded.Action)
			for repeat := 0; repeat < 2; repeat++ {
				tx, err := f.db.Begin(ctx)
				mustConnected(t, err)
				err = ProjectPublicEvent(ctx, tx, "phase4-replay", delivery.Envelope{EventType: typ, Payload: v.payload})
				if err != nil {
					rollback(tx)
					t.Fatal(err)
				}
				mustConnected(t, tx.Commit(ctx))
				replayed++
			}
		}
		eventCursor = events[len(events)-1].id
	}
	var drift int64
	mustConnected(t, f.db.QueryRow(ctx, `WITH expected AS (
	SELECT 'follow:'||follower||':'||followee AS aggregate_id FROM interaction_follows WHERE follower=$1
	UNION ALL SELECT 'comment:'||id FROM interaction_comments WHERE NOT removed
	UNION ALL SELECT 'reaction:'||actor||':'||target||':'||kind FROM interaction_reactions), actual AS (
	SELECT aggregate_id FROM interaction_feed_projection WHERE generation='phase4-replay' AND NOT removed)
	SELECT count(*) FROM ((SELECT aggregate_id FROM expected EXCEPT SELECT aggregate_id FROM actual) UNION ALL (SELECT aggregate_id FROM actual EXCEPT SELECT aggregate_id FROM expected)) differences`, a).Scan(&drift))
	replayElapsed := time.Since(replayStarted)
	apiCount, err := f.store.Count(ctx, root, "like")
	mustConnected(t, err)
	var edgeCount, projectionCount int64
	mustConnected(t, f.db.QueryRow(ctx, `SELECT count(*) FROM interaction_reactions WHERE target=$1 AND kind='like'`, root).Scan(&edgeCount))
	mustConnected(t, f.db.QueryRow(ctx, `SELECT count(*) FROM interaction_feed_projection WHERE generation='phase4-replay' AND NOT removed AND convert_from(payload,'UTF8')::jsonb->>'action'='reaction' AND convert_from(payload,'UTF8')::jsonb->>'target'=$1`, root).Scan(&projectionCount))
	if apiCount != edgeCount || projectionCount != edgeCount {
		t.Errorf("reaction count drift: api=%d authoritative=%d replay=%d", apiCount, edgeCount, projectionCount)
	}
	phase4WriteJSON(t, dir, "replay-reconciliation.json", map[string]any{"projected_deliveries": replayed, "elapsed_ns": replayElapsed.Nanoseconds(), "deliveries_per_second": float64(replayed) / replayElapsed.Seconds(), "aggregate_drift": drift, "api_reaction_count": apiCount, "authoritative_reaction_count": edgeCount, "replay_reaction_count": projectionCount, "seeded_graph_edges_excluded_from_event_comparison": 100000})
	if drift != 0 {
		t.Errorf("authoritative/replay drift=%d", drift)
	}
	_, err = f.db.Exec(ctx, `INSERT INTO interaction_follows SELECT $1,'0x'||lpad(to_hex(i),40,'0'),'2026-01-01'::timestamptz FROM generate_series(200003,201007)i`, c)
	mustConnected(t, err)
	caseID := uuid.NewString()
	tx, err := f.db.Begin(ctx)
	mustConnected(t, err)
	mustConnected(t, f.store.BeginErasureTx(ctx, tx, ErasureCommand{CaseID: caseID, Wallet: c, PolicyVersion: "social-retention-v1"}))
	mustConnected(t, tx.Commit(ctx))
	batches := []map[string]any{}
	previous := int64(0)
	completed := false
	for i := 0; i < 30; i++ {
		began := time.Now()
		receipt, err := f.store.EraseBatch(ctx, caseID)
		mustConnected(t, err)
		rows := receipt.RowsProcessed - previous
		previous = receipt.RowsProcessed
		batches = append(batches, map[string]any{"ordinal": i, "rows": rows, "elapsed_ns": time.Since(began).Nanoseconds(), "state": receipt.State})
		if rows < 0 || rows > 1000 {
			t.Error("deletion batch exceeded bounded 1000 rows")
		}
		if receipt.State == "completed" {
			completed = true
			break
		}
	}
	phase4WriteJSON(t, dir, "erasure-batches.json", batches)
	if !completed {
		t.Error("bounded erasure did not complete")
	}
	var erasedEdges int64
	mustConnected(t, f.db.QueryRow(ctx, `SELECT count(*) FROM interaction_follows WHERE follower=$1 OR followee=$1`, c).Scan(&erasedEdges))
	if erasedEdges != 0 {
		t.Error("bounded erasure left authoritative edges")
	}
	_, err = f.db.Exec(ctx, `INSERT INTO interaction_follows(follower,followee) VALUES($1,$2)`, c, b)
	mustConnected(t, err)
	page, err := f.store.Follows(ctx, b, false, 100, "")
	mustConnected(t, err)
	restoredVisible := false
	for _, edge := range page.Edges {
		if edge.Wallet == c {
			restoredVisible = true
			t.Error("restore overlay exposed erased edge")
		}
	}
	_, _, profileErr := f.store.FollowStats(ctx, c, a, f.http.Cache)
	profileDenied := errors.Is(profileErr, ErrDenied)
	if !profileDenied {
		t.Errorf("restored erased profile: %v", profileErr)
	}
	phase4WriteJSON(t, dir, "restore-overlay.json", map[string]any{"erased_authoritative_edges": erasedEdges, "restored_edge_denied": !restoredVisible, "restored_profile_denied": profileDenied, "public_page_limit": 100})
	manifest := map[string]string{}
	for _, name := range []string{"binding.json", "follower-index-plan.json", "traversal.json", "raw-samples.jsonl", "load-summary.json", "privacy.json", "cache-stampede.json", "replay-reconciliation.json", "erasure-batches.json", "restore-overlay.json"} {
		bytes, err := os.ReadFile(filepath.Join(dir, name))
		mustConnected(t, err)
		digest := sha256.Sum256(bytes)
		manifest[name] = hex.EncodeToString(digest[:])
	}
	phase4WriteJSON(t, dir, "artifact-manifest.json", map[string]any{"sha256": manifest, "source_sha256": sourceDigest, "executable_sha256": hex.EncodeToString(buildHash[:]), "allocation_sha256": allocationHash, "passed": !t.Failed(), "global_release_acceptance": "deferred_to_serial9.1"})
}
