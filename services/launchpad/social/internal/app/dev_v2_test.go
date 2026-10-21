package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// The development app is booted against a fresh database on the fixture
// Postgres and the fixture Redis (the stores the service uses in development),
// with the indexer's real indexer.pools DDL seeded with one pool.

func fixtureEnv(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

type devHarness struct {
	t       *testing.T
	app     *App
	server  *httptest.Server
	admin   *pgx.Conn
	dbConn  *pgx.Conn
	pool    string
	token   string
	creator string
	// client reaches server; nil means http.DefaultClient (the platform
	// harness sets the gateway's mTLS client).
	client *http.Client
}

func newDevHarness(t *testing.T) *devHarness {
	t.Helper()
	ctx := context.Background()
	adminDSN := fixtureEnv("SOCIAL_FIXTURE_POSTGRES_DSN", "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable")
	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatalf("fixture postgres: %v", err)
	}
	name := "social_fleet2_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgx.ParseConfig(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", cfg.User, cfg.Password, cfg.Host, cfg.Port, name)
	db, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	h := &devHarness{t: t, admin: admin, dbConn: db, pool: addr(0x9001), token: addr(0x7001), creator: addr(0xc0ffee)}
	t.Cleanup(func() {
		_ = db.Close(context.Background())
		if _, err := admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
		_ = admin.Close(context.Background())
	})
	// indexer.pools is owned by core/indexer (its migrations/00001_init.sql).
	if _, err = db.Exec(ctx, `CREATE SCHEMA indexer;
CREATE TABLE "indexer"."pools" ("pool_address" varchar(42) PRIMARY KEY NOT NULL,"token_address" varchar(42) NOT NULL,"creator" varchar(42) NOT NULL,"optical" varchar(42) NOT NULL,"pool_id" varchar(66) NOT NULL,"nft_id" bigint,"created_at" bigint NOT NULL,"created_block" bigint NOT NULL,"tx_hash" varchar(66) NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO indexer.pools VALUES($1,$2,$3,$4,$5,1,$6,100,$7)`, h.pool, h.token, h.creator, addr(0x0b7), "0x"+strings.Repeat("ab", 32), time.Now().Unix(), "0x"+strings.Repeat("cd", 32)); err != nil {
		t.Fatal(err)
	}
	app, err := New(ctx, config.Config{
		NodeEnv:            "development",
		DatabaseURL:        dsn,
		RedisURL:           fixtureEnv("SOCIAL_FIXTURE_REDIS_URL", "redis://:kindle_fixture_password@127.0.0.1:26379/9"),
		MaxMessageLength:   500,
		RateWindowSeconds:  10,
		MaxPoolMsgsPerWin:  20,
		MaxDmMsgsPerWindow: 20,
		WSSendBuffer:       64,
		WSWriteTimeoutSecs: 10,
		WSReadLimitBytes:   4096,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("boot development app: %v", err)
	}
	h.app = app
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{}, 2)
	go func() { _ = app.Hub.Run(runCtx); done <- struct{}{} }()
	go func() { _ = app.dev.Run(runCtx); done <- struct{}{} }()
	h.server = httptest.NewServer(app.Router)
	t.Cleanup(func() {
		h.server.Close()
		cancel()
		<-done
		<-done
		app.Close()
	})
	return h
}

func addr(n int) string { return fmt.Sprintf("0x%040x", n) }

type response struct {
	status int
	body   []byte
}

func (h *devHarness) do(method, path, actor string, body any, header map[string]string) response {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			h.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		h.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if actor != "" {
		req.Header.Set("X-Actor-Wallet", actor)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	client := h.client
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return response{res.StatusCode, raw}
}

// expect asserts the status and, for a JSON body, its contract schema.
func (h *devHarness) expect(r response, status int, schema string) map[string]any {
	h.t.Helper()
	if r.status != status {
		h.t.Fatalf("status %d, want %d: %s", r.status, status, r.body)
	}
	if schema == "" {
		return nil
	}
	if err := v2.ValidateJSON(schema, r.body); err != nil {
		h.t.Fatalf("%s: %v: %s", schema, err, r.body)
	}
	var out map[string]any
	if err := json.Unmarshal(r.body, &out); err != nil {
		h.t.Fatal(err)
	}
	return out
}

func data(m map[string]any) map[string]any { return m["data"].(map[string]any) }
func items(m map[string]any) []any         { return data(m)["items"].([]any) }

func TestDevelopmentV2SocialSurface(t *testing.T) {
	h := newDevHarness(t)
	alice, bob := addr(0xa11ce), addr(0xb0b)

	// ── anonymous reads: real empty collections ────────────────────────────
	if got := items(h.expect(h.do("GET", "/v2/pools/"+h.pool+"/callouts", "", nil, nil), 200, "CalloutListResponse")); len(got) != 0 {
		t.Fatalf("callouts = %v", got)
	}
	if got := items(h.expect(h.do("GET", "/v2/bounties", "", nil, nil), 200, "BountyListResponse")); len(got) != 0 {
		t.Fatalf("bounties = %v", got)
	}
	stats := data(h.expect(h.do("GET", "/v2/bounties/stats", "", nil, nil), 200, "BountiesStatsResponse"))
	if stats["openCount"] != float64(0) || stats["unclaimedPoolUsdlRaw"] != "0" || stats["paidOutUsdlRaw"] != "0" || stats["submissionsCount"] != float64(0) {
		t.Fatalf("stats = %v", stats)
	}
	if got := items(h.expect(h.do("GET", "/v2/pools/"+h.pool+"/comments", "", nil, nil), 200, "CommentsListResponse")); len(got) != 0 {
		t.Fatalf("comments = %v", got)
	}
	h.expect(h.do("GET", "/v2/bounties?state=bogus", "", nil, nil), 422, "Error")
	h.expect(h.do("GET", "/v2/bounties?cursor=nope", "", nil, nil), 400, "Error")

	// ── writes without a resolved actor are 401 ────────────────────────────
	callout := map[string]any{"poolAddress": h.pool, "body": "accumulating here", "direction": "bullish", "disclosure": "organic", "clientCalloutId": uuid.NewString()}
	bounty := map[string]any{"title": "Best meme", "description": "Make the best meme for the coin.", "assetAddress": strings.ToLower(deployment.SettlementAddress), "budgetRaw": "5000000", "poolAddress": h.pool}
	comment := map[string]any{"body": "first", "clientCommentId": uuid.NewString()}
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/v2/callouts", callout},
		{"POST", "/v2/bounties", bounty},
		{"POST", "/v2/pools/" + h.pool + "/comments", comment},
		{"POST", "/v2/reports", map[string]any{"targetType": "token", "targetId": h.token, "reason": "spam", "evidence": ""}},
		{"POST", "/v2/chat/read", map[string]any{"conversation_id": "pool:" + h.pool, "through_sequence": "1"}},
	} {
		r := h.expect(h.do(c.method, c.path, "", c.body, nil), 401, "Error")
		if r["code"] != "AUTH_EXPIRED" {
			t.Fatalf("%s: %v", c.path, r)
		}
	}
	if r := h.do("POST", "/v2/social/pool/"+h.pool+"/comments", "", comment, map[string]string{"Idempotency-Key": "k"}); r.status != 401 {
		t.Fatalf("social write without actor: %d %s", r.status, r.body)
	}
	if r := h.do("POST", "/v2/callouts", "not-a-wallet", callout, nil); r.status != 401 {
		t.Fatalf("malformed actor: %d", r.status)
	}
	if r := h.do("GET", "/v2/chat/history?conversation_id=pool:"+h.pool, "", nil, nil); r.status != 401 {
		t.Fatalf("anonymous chat history: %d", r.status)
	}

	// ── callouts ────────────────────────────────────────────────────────────
	created := data(h.expect(h.do("POST", "/v2/callouts", alice, callout, nil), 201, "CalloutCreateResponse"))["callout"].(map[string]any)
	if created["authorWallet"] != alice || created["poolAddress"] != h.pool || created["revision"] != "1" {
		t.Fatalf("callout = %v", created)
	}
	again := data(h.expect(h.do("POST", "/v2/callouts", alice, callout, nil), 201, "CalloutCreateResponse"))["callout"].(map[string]any)
	if again["calloutId"] != created["calloutId"] {
		t.Fatalf("retry created a second callout: %v vs %v", again, created)
	}
	changed := map[string]any{"poolAddress": h.pool, "body": "changed", "direction": "bullish", "disclosure": "organic", "clientCalloutId": callout["clientCalloutId"]}
	h.expect(h.do("POST", "/v2/callouts", alice, changed, nil), 409, "Error")
	creatorOnly := map[string]any{"poolAddress": h.pool, "body": "I made this", "direction": "neutral", "disclosure": "creator", "clientCalloutId": uuid.NewString()}
	h.expect(h.do("POST", "/v2/callouts", alice, creatorOnly, nil), 403, "Error")
	h.expect(h.do("POST", "/v2/callouts", h.creator, creatorOnly, nil), 201, "CalloutCreateResponse")
	unknown := map[string]any{"poolAddress": addr(0xdead), "body": "x", "direction": "bearish", "disclosure": "organic", "clientCalloutId": uuid.NewString()}
	h.expect(h.do("POST", "/v2/callouts", alice, unknown, nil), 404, "Error")
	h.expect(h.do("POST", "/v2/callouts", alice, map[string]any{"poolAddress": h.pool}, nil), 422, "Error")
	list := h.expect(h.do("GET", "/v2/pools/"+h.pool+"/callouts?limit=1", "", nil, nil), 200, "CalloutListResponse")
	if len(items(list)) != 1 || list["nextCursor"] == nil {
		t.Fatalf("page 1 = %s", list)
	}
	list = h.expect(h.do("GET", "/v2/pools/"+h.pool+"/callouts?limit=1&cursor="+list["nextCursor"].(string), "", nil, nil), 200, "CalloutListResponse")
	if len(items(list)) != 1 || list["nextCursor"] != nil || items(list)[0].(map[string]any)["calloutId"] != created["calloutId"] {
		t.Fatalf("page 2 = %v", list)
	}

	// ── bounties ────────────────────────────────────────────────────────────
	key := map[string]string{"Idempotency-Key": uuid.NewString()}
	b := data(h.expect(h.do("POST", "/v2/bounties", alice, bounty, key), 201, "BountyCreateResponse"))
	if b["fundingTransactionRequest"] != nil {
		t.Fatalf("funding request without escrow: %v", b)
	}
	made := b["bounty"].(map[string]any)
	if made["state"] != "draft" || made["creatorWallet"] != alice || made["rewardUsdlRaw"] != "5000000" || made["poolAddress"] != h.pool {
		t.Fatalf("bounty = %v", made)
	}
	if again := data(h.expect(h.do("POST", "/v2/bounties", alice, bounty, key), 201, "BountyCreateResponse"))["bounty"].(map[string]any); again["bountyId"] != made["bountyId"] {
		t.Fatalf("idempotent create returned %v", again)
	}
	other := map[string]any{"title": "Other", "description": "d", "assetAddress": strings.ToLower(deployment.SettlementAddress), "budgetRaw": "1"}
	h.expect(h.do("POST", "/v2/bounties", alice, other, key), 409, "Error")
	badAsset := map[string]any{"title": "t", "description": "d", "assetAddress": addr(0x5), "budgetRaw": "1"}
	h.expect(h.do("POST", "/v2/bounties", alice, badAsset, nil), 422, "Error")
	tokenBounty := map[string]any{"title": "Token reward", "description": "Paid in the coin", "assetAddress": h.token, "budgetRaw": "100", "poolAddress": h.pool}
	tb := data(h.expect(h.do("POST", "/v2/bounties", bob, tokenBounty, nil), 201, "BountyCreateResponse"))["bounty"].(map[string]any)
	if _, ok := tb["rewardUsdlRaw"]; ok {
		t.Fatalf("token bounty claims a USDL value: %v", tb)
	}
	id := made["bountyId"].(string)
	if got := items(h.expect(h.do("GET", "/v2/bounties", "", nil, nil), 200, "BountyListResponse")); len(got) != 0 {
		t.Fatalf("drafts listed publicly: %v", got)
	}
	if got := items(h.expect(h.do("GET", "/v2/bounties", alice, nil, nil), 200, "BountyListResponse")); len(got) != 1 {
		t.Fatalf("creator list = %v", got)
	}
	h.expect(h.do("GET", "/v2/bounties/"+id, "", nil, nil), 404, "Error")
	h.expect(h.do("GET", "/v2/bounties/"+id, alice, nil, nil), 200, "BountyGetResponse")
	submission := map[string]any{"body": "here is mine", "clientSubmissionId": uuid.NewString(), "evidenceUrls": []string{"https://example.org/meme.png"}}
	h.expect(h.do("POST", "/v2/bounties/"+id+"/submissions", bob, submission, nil), 404, "Error")

	// Funding is an on-chain escrow step chain 125 cannot perform yet; the
	// fixture records a funded bounty directly in the store to exercise the
	// open-bounty paths.
	if _, err := h.dbConn.Exec(context.Background(), `UPDATE chat.v2_bounties SET state='open' WHERE bounty_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	h.expect(h.do("POST", "/v2/bounties/"+id+"/submissions", alice, map[string]any{"body": "self", "clientSubmissionId": uuid.NewString(), "evidenceUrls": []string{"https://example.org/a"}}, nil), 403, "Error")
	sub := data(h.expect(h.do("POST", "/v2/bounties/"+id+"/submissions", bob, submission, nil), 201, "BountySubmitResponse"))["submission"].(map[string]any)
	if sub["state"] != "submitted" || sub["awardRaw"] != nil || sub["authorWallet"] != bob {
		t.Fatalf("submission = %v", sub)
	}
	if got := items(h.expect(h.do("GET", "/v2/bounties/"+id+"/submissions", "", nil, nil), 200, "BountySubmissionsResponse")); len(got) != 1 {
		t.Fatalf("submissions = %v", got)
	}
	stats = data(h.expect(h.do("GET", "/v2/bounties/stats", "", nil, nil), 200, "BountiesStatsResponse"))
	if stats["openCount"] != float64(1) || stats["unclaimedPoolUsdlRaw"] != "5000000" || stats["submissionsCount"] != float64(1) {
		t.Fatalf("stats = %v", stats)
	}
	acceptPath := "/v2/bounties/" + id + "/submissions/" + sub["submissionId"].(string) + ":accept"
	h.expect(h.do("POST", acceptPath, bob, map[string]any{"awardRaw": "1", "expectedBountyRevision": "1"}, nil), 403, "Error")
	h.expect(h.do("POST", acceptPath, alice, map[string]any{"awardRaw": "1", "expectedBountyRevision": "7"}, nil), 409, "Error")
	h.expect(h.do("POST", acceptPath, alice, map[string]any{"awardRaw": "5000001", "expectedBountyRevision": "1"}, nil), 422, "Error")
	accepted := data(h.expect(h.do("POST", acceptPath, alice, map[string]any{"awardRaw": "2000000", "expectedBountyRevision": "1"}, map[string]string{"Idempotency-Key": "accept-1"}), 201, "BountyAcceptResponse"))
	if accepted["settlementRequest"] != nil || accepted["submission"].(map[string]any)["state"] != "accepted" || accepted["submission"].(map[string]any)["awardRaw"] != "2000000" {
		t.Fatalf("accept = %v", accepted)
	}
	h.expect(h.do("POST", acceptPath, alice, map[string]any{"awardRaw": "2000000", "expectedBountyRevision": "1"}, map[string]string{"Idempotency-Key": "accept-1"}), 201, "BountyAcceptResponse")
	got := data(h.expect(h.do("GET", "/v2/bounties/"+id, "", nil, nil), 200, "BountyGetResponse"))["bounty"].(map[string]any)
	if got["state"] != "reviewing" || got["winnersCount"] != float64(1) {
		t.Fatalf("after accept = %v", got)
	}
	stats = data(h.expect(h.do("GET", "/v2/bounties/stats", "", nil, nil), 200, "BountiesStatsResponse"))
	if stats["openCount"] != float64(0) || stats["unclaimedPoolUsdlRaw"] != "3000000" {
		t.Fatalf("stats after accept = %v", stats)
	}
	if got := items(h.expect(h.do("GET", "/v2/bounties?state=reviewing&poolAddress="+h.pool, "", nil, nil), 200, "BountyListResponse")); len(got) != 1 {
		t.Fatalf("filtered list = %v", got)
	}

	// ── comments (contract + /v2/social share interaction_comments) ────────
	c := data(h.expect(h.do("POST", "/v2/pools/"+h.pool+"/comments", alice, comment, nil), 201, "CommentCreateResponse"))["comment"].(map[string]any)
	if c["authorWallet"] != alice || c["revision"] != "1" || c["likeCount"] != "0" {
		t.Fatalf("comment = %v", c)
	}
	cid := c["commentId"].(string)
	if again := data(h.expect(h.do("POST", "/v2/pools/"+h.pool+"/comments", alice, comment, nil), 201, "CommentCreateResponse"))["comment"].(map[string]any); again["commentId"] != cid {
		t.Fatalf("comment retry = %v", again)
	}
	h.expect(h.do("POST", "/v2/pools/"+addr(0xdead)+"/comments", alice, map[string]any{"body": "x", "clientCommentId": uuid.NewString()}, nil), 404, "Error")
	reply := data(h.expect(h.do("POST", "/v2/pools/"+h.pool+"/comments", bob, map[string]any{"body": "reply", "clientCommentId": uuid.NewString(), "parentCommentId": cid}, nil), 201, "CommentCreateResponse"))["comment"].(map[string]any)
	if reply["parentCommentId"] != cid {
		t.Fatalf("reply = %v", reply)
	}
	like := data(h.expect(h.do("PUT", "/v2/comments/"+cid+"/likes/me", bob, nil, nil), 200, "CommentLikeResponse"))
	if like["likeCount"] != "1" || like["liked"] != true {
		t.Fatalf("like = %v", like)
	}
	var socialList struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	r := h.do("GET", "/v2/social/pool/"+h.pool+"/comments?limit=50", "", nil, nil)
	if r.status != 200 || json.Unmarshal(r.body, &socialList) != nil || len(socialList.Data.Items) != 2 {
		t.Fatalf("social list %d %s", r.status, r.body)
	}
	edited := data(h.expect(h.do("PATCH", "/v2/comments/"+cid, alice, map[string]any{"body": "first, edited", "expectedRevision": "1"}, nil), 200, "CommentEditResponse"))["comment"].(map[string]any)
	if edited["revision"] != "2" || edited["body"] != "first, edited" {
		t.Fatalf("edit = %v", edited)
	}
	h.expect(h.do("PATCH", "/v2/comments/"+cid, alice, map[string]any{"body": "stale", "expectedRevision": "1"}, nil), 409, "Error")
	h.expect(h.do("PATCH", "/v2/comments/"+cid, bob, map[string]any{"body": "not mine", "expectedRevision": "2"}, nil), 403, "Error")
	h.expect(h.do("DELETE", "/v2/comments/"+cid+"/likes/me", bob, nil, nil), 204, "")
	if social := h.do("POST", "/v2/social/pool/"+h.pool+"/comments", bob, map[string]any{"body": "via social", "clientCommentId": uuid.NewString()}, map[string]string{"Idempotency-Key": uuid.NewString()}); social.status != 200 {
		t.Fatalf("social create %d %s", social.status, social.body)
	}
	report := data(h.expect(h.do("POST", "/v2/reports", bob, map[string]any{"targetType": "comment", "targetId": cid, "reason": "spam", "evidence": "repeated"}, nil), 201, "ReportCreateResponse"))["report"].(map[string]any)
	if report["state"] != "open" || report["reason"] != "spam" || report["reporterWallet"] != bob {
		t.Fatalf("report = %v", report)
	}
	h.expect(h.do("POST", "/v2/reports", bob, map[string]any{"targetType": "comment", "targetId": uuid.NewString(), "reason": "spam", "evidence": ""}, nil), 404, "Error")
	h.expect(h.do("DELETE", "/v2/comments/"+cid, alice, nil, nil), 204, "")
	if got := items(h.expect(h.do("GET", "/v2/pools/"+h.pool+"/comments", "", nil, nil), 200, "CommentsListResponse")); len(got) != 2 {
		t.Fatalf("after delete = %v", got)
	}

	// ── follows through /v2/social and the contract route share one graph ──
	if f := h.do("PUT", "/v2/social/follow/"+alice, bob, nil, nil); f.status != 200 {
		t.Fatalf("social follow %d %s", f.status, f.body)
	}
	fs := data(h.expect(h.do("GET", "/v2/profiles/"+alice+"/follow-stats", bob, nil, nil), 200, "FollowStatsResponse"))
	if fs["followers"] != float64(1) || fs["isFollowing"] != true {
		t.Fatalf("follow stats = %v", fs)
	}

	// ── chat: WS send persists durably, fans out, and /v2/chat/history reads it
	wsURL := strings.Replace(h.server.URL, "http", "ws", 1) + "/ws"
	if _, res, err := websocket.DefaultDialer.Dial(wsURL, nil); err == nil || res == nil || res.StatusCode != 401 {
		t.Fatalf("anonymous upgrade: %v %v", err, res)
	}
	dial := func(actor string) *websocket.Conn {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"X-Actor-Wallet": {actor}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
	ac, bc := dial(alice), dial(bob)
	for _, conn := range []*websocket.Conn{ac, bc} {
		if err := conn.WriteJSON(map[string]any{"type": "join_pool", "pool_address": h.pool}); err != nil {
			t.Fatal(err)
		}
		readFrame(t, conn, "joined_pool")
	}
	if err := ac.WriteJSON(map[string]any{"type": "pool_message", "pool_address": h.pool, "content": "gm <b>chat</b>", "client_message_id": "c-1"}); err != nil {
		t.Fatal(err)
	}
	persisted := readFrame(t, ac, "message_persisted")
	if persisted["sequence"] != "1" {
		t.Fatalf("persisted = %v", persisted)
	}
	delivered := readFrame(t, bc, "pool_message")
	if delivered["content"] != "gm chat" || delivered["sender"] != alice {
		t.Fatalf("fan-out = %v", delivered)
	}
	var history struct {
		Messages []map[string]any `json:"messages"`
	}
	hr := h.do("GET", "/v2/chat/history?conversation_id=pool:"+h.pool, bob, nil, nil)
	if hr.status != 200 || json.Unmarshal(hr.body, &history) != nil || len(history.Messages) != 1 || history.Messages[0]["content"] != "gm chat" {
		t.Fatalf("history %d %s", hr.status, hr.body)
	}
	read := h.do("POST", "/v2/chat/read", bob, map[string]any{"conversation_id": "pool:" + h.pool, "through_sequence": "1"}, nil)
	if read.status != 200 {
		t.Fatalf("read marker %d %s", read.status, read.body)
	}
	revise := h.do("POST", "/v2/chat/revise", alice, map[string]any{"message_id": persisted["id"], "expected_revision": "1", "content": "gm, revised", "remove": false}, nil)
	if revise.status != 200 || !strings.Contains(string(revise.body), "gm, revised") {
		t.Fatalf("revise %d %s", revise.status, revise.body)
	}
	if _, err := h.dbConn.Exec(context.Background(), `INSERT INTO chat.chat_bans(id,wallet,pool_address,reason,banned_by,created_at) VALUES('ban-1',$1,$2,'spam',$3,0)`, bob, h.pool, h.creator); err != nil {
		t.Fatal(err)
	}
	if err := bc.WriteJSON(map[string]any{"type": "pool_message", "pool_address": h.pool, "content": "blocked", "client_message_id": "c-2"}); err != nil {
		t.Fatal(err)
	}
	if e := readFrame(t, bc, "error"); !strings.Contains(fmt.Sprint(e["message"]), "denied") {
		t.Fatalf("banned send = %v", e)
	}
	h.expect(h.do("POST", "/v2/callouts", bob, map[string]any{"poolAddress": h.pool, "body": "banned", "direction": "bearish", "disclosure": "organic", "clientCalloutId": uuid.NewString()}, nil), 403, "Error")
}

func readFrame(t *testing.T, conn *websocket.Conn, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_ = conn.SetReadDeadline(deadline)
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("waiting for %q: %v", want, err)
		}
		var m map[string]any
		if json.Unmarshal(raw, &m) == nil && m["type"] == want {
			return m
		}
	}
}
