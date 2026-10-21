package consumer_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/holdings"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/consumer"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

const holderBalance = "100000000000000" // 1e14 = 10% of supply

// waitForHolderCount polls pool_stats.holder_count until it equals want or the
// timeout elapses (the debounced refresh runs asynchronously).
func waitForHolderCount(t *testing.T, ctx context.Context, st *store.Store, addr string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if poolHolderCount(t, ctx, st, addr) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("holder_count for %s never reached %d within %s (last=%d)", addr, want, timeout, poolHolderCount(t, ctx, st, addr))
}

func TestHolderTracker(t *testing.T) {
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	rdb := internaltest.NewRedis(t)

	t.Run("refresh is debounced, not synchronous", func(t *testing.T) {
		const addr = "0xholder_defer"
		seedPoolStats(t, ctx, st, addr, nil, "1000000")
		ht := consumer.NewHolderTracker(st, rdb, discardLogger(), 250*time.Millisecond)
		defer ht.Close()

		if err := ht.ProcessSwap(ctx, consumer.HolderSwap{
			PoolAddress: addr, Sender: "0xbuyer", IsBuy: true, AmountIn: "0", AmountOut: holderBalance,
		}); err != nil {
			t.Fatalf("process swap: %v", err)
		}
		// The holder row exists immediately (synchronous balance delta)...
		if bal, ok, _ := st.GetHolderBalance(ctx, addr, "0xbuyer"); !ok || bal != holderBalance {
			t.Fatalf("balance = %s ok=%v, want %s", bal, ok, holderBalance)
		}
		// ...but holder_count has NOT been refreshed yet (debounced).
		if n := poolHolderCount(t, ctx, st, addr); n != 0 {
			t.Fatalf("holder_count = %d immediately after swap, want 0 (refresh deferred)", n)
		}
		// After the debounce window the single refresh runs and writes the count.
		waitForHolderCount(t, ctx, st, addr, 1, 3*time.Second)
	})

	t.Run("a burst of swaps coalesces into one refresh", func(t *testing.T) {
		const addr = "0xholder_burst"
		seedPoolStats(t, ctx, st, addr, nil, "1000000")
		ht := consumer.NewHolderTracker(st, rdb, discardLogger(), 300*time.Millisecond)
		defer ht.Close()

		const n = 5
		for i := 0; i < n; i++ {
			sender := fmt.Sprintf("0xburst%02d", i)
			if err := ht.ProcessSwap(ctx, consumer.HolderSwap{
				PoolAddress: addr, Sender: sender, IsBuy: true, AmountIn: "0", AmountOut: holderBalance,
			}); err != nil {
				t.Fatalf("process swap %d: %v", i, err)
			}
		}
		// The coalesced refresh fires once after the window and sees all n holders.
		waitForHolderCount(t, ctx, st, addr, n, 3*time.Second)
	})

	t.Run("new-holder sell is a no-op and schedules no refresh", func(t *testing.T) {
		const addr = "0xholder_nosell"
		seedPoolStats(t, ctx, st, addr, nil, "1000000")
		// Sentinel holder_count: if a refresh ran it would recompute this to 0.
		if _, err := st.Pool().Exec(ctx, `UPDATE stats.pool_stats SET holder_count=99 WHERE pool_address=$1`, addr); err != nil {
			t.Fatalf("seed sentinel: %v", err)
		}
		ht := consumer.NewHolderTracker(st, rdb, discardLogger(), 100*time.Millisecond)
		defer ht.Close()

		if err := ht.ProcessSwap(ctx, consumer.HolderSwap{
			PoolAddress: addr, Sender: "0xnewseller", IsBuy: false, AmountIn: holderBalance, AmountOut: "0",
		}); err != nil {
			t.Fatalf("process new-holder sell: %v", err)
		}
		// No holder row was created.
		if _, ok, _ := st.GetHolderBalance(ctx, addr, "0xnewseller"); ok {
			t.Fatal("new-holder sell must not create a holder row")
		}
		// Well past the debounce window, the sentinel is untouched -> no refresh ran.
		time.Sleep(400 * time.Millisecond)
		if n := poolHolderCount(t, ctx, st, addr); n != 99 {
			t.Fatalf("holder_count = %d, want 99 (no refresh should have run)", n)
		}
	})

	t.Run("Close cancels a pending refresh", func(t *testing.T) {
		const addr = "0xholder_close"
		seedPoolStats(t, ctx, st, addr, nil, "1000000")
		ht := consumer.NewHolderTracker(st, rdb, discardLogger(), 800*time.Millisecond)

		if err := ht.ProcessSwap(ctx, consumer.HolderSwap{
			PoolAddress: addr, Sender: "0xlate", IsBuy: true, AmountIn: "0", AmountOut: holderBalance,
		}); err != nil {
			t.Fatalf("process swap: %v", err)
		}
		ht.Close() // stop the pending timer before it fires

		time.Sleep(1100 * time.Millisecond)
		if n := poolHolderCount(t, ctx, st, addr); n != 0 {
			t.Fatalf("holder_count = %d after Close, want 0 (timer should have been cancelled)", n)
		}
	})

	t.Run("RefreshNow recomputes synchronously and invalidates the cache", func(t *testing.T) {
		const addr = "0xholder_refreshnow"
		seedPoolStats(t, ctx, st, addr, nil, "1000000")
		if err := rdb.Set(ctx, "stats:"+addr, `{"stale":true}`, time.Minute).Err(); err != nil {
			t.Fatalf("seed cache: %v", err)
		}
		// Two holders inserted directly so RefreshNow has data to aggregate.
		for i, h := range []string{"0xrn1", "0xrn2"} {
			if _, err := st.Pool().Exec(ctx, `
				INSERT INTO stats.pool_holders (pool_address, holder_address, balance, pct_of_supply, last_updated)
				VALUES ($1,$2,$3,'1000',$4)`, addr, h, holderBalance, 100+i); err != nil {
				t.Fatalf("insert holder: %v", err)
			}
		}
		ht := consumer.NewHolderTracker(st, rdb, discardLogger(), time.Hour) // long debounce: prove the sync path
		defer ht.Close()

		if err := ht.RefreshNow(ctx, addr); err != nil {
			t.Fatalf("refresh now: %v", err)
		}
		if n := poolHolderCount(t, ctx, st, addr); n != 2 {
			t.Fatalf("holder_count = %d, want 2", n)
		}
		if exists, _ := rdb.Exists(ctx, "stats:"+addr).Result(); exists != 0 {
			t.Fatal("RefreshNow must delete the stats cache key")
		}
	})
}

func TestTransferHoldingsPublicJourney(t *testing.T) {
	ctx := context.Background()
	pg := internaltest.NewPostgres(t)
	st := store.New(pg)
	ddl, err := holdings.SchemaSQL("holdings_phase2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pg.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	repository, err := holdings.NewRepository(pg, "holdings_phase2")
	if err != nil {
		t.Fatal(err)
	}
	address := func(n int) string { return fmt.Sprintf("0x%040x", n) }
	token, pool, a, b := address(1), address(2), address(3), address(4)
	if _, err = st.InsertInitialPoolStats(ctx, store.InitialPoolStats{PoolAddress: pool, TokenAddress: token, CreatorAddress: &a, Price: "0", MarketCap: "0", High24h: "0", Low24h: "0", CreatedAt: 10, UpdatedAt: 10}); err != nil {
		t.Fatal(err)
	}
	if err = repository.Discover(ctx, token, 6, "10", true); err != nil {
		t.Fatal(err)
	}
	makeEvent := func(from, to, amount string, n int) holdings.Transfer {
		return holdings.Transfer{ChainID: "125", Token: token, From: from, To: to, Amount: amount, Height: "10", TransactionIndex: "0", LogIndex: fmt.Sprint(n), TransactionHash: fmt.Sprintf("0x%064x", n+1), BlockHash: fmt.Sprintf("0x%064x", 100)}
	}
	for _, e := range []holdings.Transfer{makeEvent(holdings.Zero, a, "100", 0), makeEvent(a, b, "25", 1), makeEvent(b, holdings.Zero, "5", 2)} {
		if _, err = repository.Apply(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	router := chi.NewRouter()
	httpapi.RegisterTransferHolders(router, st, repository)
	request := func(query string) (int, map[string]any) {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/v1/pools/"+pool+"/holders"+query, nil))
		if w.Code == 200 && w.Header().Get("Server-Timing") == "" {
			t.Fatal("actual holder origin timing absent")
		}
		var body map[string]any
		if err = json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return w.Code, body
	}
	status, body := request("?limit=1")
	if status != 200 {
		t.Fatal(status, body)
	}
	data := body["data"].(map[string]any)
	items := data["items"].([]any)
	holder := items[0].(map[string]any)
	if holder["wallet"] != a || holder["balanceRaw"] != "75" || holder["share"].(map[string]any)["basisPoints"] != "7894" || data["supplyRaw"] != "95" || data["coverage"] != "complete" || data["tokenAddress"] != token {
		t.Fatal(body)
	}
	cursor, ok := body["nextCursor"].(string)
	if !ok || len(cursor) > 256 {
		t.Fatal(body)
	}
	if _, err = repository.Apply(ctx, makeEvent(a, b, "70", 3)); err != nil {
		t.Fatal(err)
	}
	status, next := request("?limit=1&cursor=" + url.QueryEscape(cursor))
	if status != 200 || next["revision"] != body["revision"] {
		t.Fatal(status, next)
	}
	nextHolder := next["data"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if nextHolder["wallet"] != b || nextHolder["balanceRaw"] != "20" {
		t.Fatal(next)
	}
	if _, err = pg.Exec(ctx, "UPDATE holdings_phase2.snapshots SET expires_at=clock_timestamp()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	status, body = request("?cursor=" + url.QueryEscape(cursor))
	if status != 409 || body["code"] != "RESNAPSHOT_REQUIRED" {
		t.Fatal(status, body)
	}
	status, body = request("?cursor=invalid")
	if status != 400 || body["code"] != "CURSOR_INVALID" {
		t.Fatal(status, body)
	}
	status, body = request("?limit=101")
	if status != 422 {
		t.Fatal(status, body)
	}
}

func TestLegacyHoldingsQuarantine(t *testing.T) {
	ctx := context.Background()
	pg := internaltest.NewPostgres(t)
	ddl, err := holdings.SchemaSQL("holdings_legacy")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pg.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	r, err := holdings.NewRepository(pg, "holdings_legacy")
	if err != nil {
		t.Fatal(err)
	}
	pool := fmt.Sprintf("0x%040x", 1)
	for i, raw := range []string{"100", "-5", "malformed"} {
		if _, err = pg.Exec(ctx, "INSERT INTO stats.pool_holders(pool_address,holder_address,balance,pct_of_supply,last_updated) VALUES($1,$2,$3,'0',1)", pool, fmt.Sprintf("0x%040x", i+2), raw); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := pg.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.ImportLegacyRowsTx(ctx, tx, pool); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var valid, quarantined int
	if err = pg.QueryRow(ctx, "SELECT count(*) FILTER(WHERE numeric_balance IS NOT NULL),count(*) FILTER(WHERE numeric_balance IS NULL) FROM holdings_legacy.legacy_rows").Scan(&valid, &quarantined); err != nil || valid != 1 || quarantined != 2 {
		t.Fatal(valid, quarantined, err)
	}
	var authoritative int
	if err = pg.QueryRow(ctx, "SELECT count(*) FROM holdings_legacy.balances").Scan(&authoritative); err != nil || authoritative != 0 {
		t.Fatal(authoritative, err)
	}
}

func TestHolderWorkerCommitBeforeAcknowledge(t *testing.T) {
	if os.Getenv("L14_BROKER_CRASH_CHILD") == "1" {
		ctx := context.Background()
		pg, err := pgxpool.New(ctx, os.Getenv("L14_BROKER_POSTGRES"))
		if err != nil {
			t.Fatal(err)
		}
		r, err := holdings.NewRepository(pg, "holdings_broker_crash")
		if err != nil {
			t.Fatal(err)
		}
		options, err := goredis.ParseURL(os.Getenv("L14_BROKER_REDIS"))
		if err != nil {
			t.Fatal(err)
		}
		redis := goredis.NewClient(options)
		messages, err := redis.XReadGroup(ctx, &goredis.XReadGroupArgs{Group: "holders", Consumer: "crashing-worker", Streams: []string{os.Getenv("L14_BROKER_STREAM"), ">"}, Count: 1, Block: time.Second}).Result()
		if err != nil || len(messages) != 1 || len(messages[0].Messages) != 1 {
			t.Fatal(messages, err)
		}
		payload, ok := messages[0].Messages[0].Values["transfer"].(string)
		if !ok {
			t.Fatal("Transfer payload absent")
		}
		var e holdings.Transfer
		if err = json.Unmarshal([]byte(payload), &e); err != nil {
			t.Fatal(err)
		}
		if _, err = r.Apply(ctx, e); err != nil {
			t.Fatal(err)
		}
		os.Exit(23)
	}
	// Never start a container here: the broker-crash journey runs only against
	// the operator-provided Postgres and Redis (STATS_TEST_* variables).
	redisURL := os.Getenv("STATS_TEST_REDIS_URL")
	if redisURL == "" || os.Getenv("STATS_TEST_POSTGRES_DSN") == "" {
		t.Skip("STATS_TEST_POSTGRES_DSN and STATS_TEST_REDIS_URL required: this broker-crash test never starts containers")
	}
	ctx := context.Background()
	dsn, pg := internaltest.NewPostgresWithDSN(t)
	ddl, err := holdings.SchemaSQL("holdings_broker_crash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pg.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	r, err := holdings.NewRepository(pg, "holdings_broker_crash")
	if err != nil {
		t.Fatal(err)
	}
	token, wallet := fmt.Sprintf("0x%040x", 301), fmt.Sprintf("0x%040x", 302)
	if err = r.Discover(ctx, token, 6, "10", true); err != nil {
		t.Fatal(err)
	}
	e := holdings.Transfer{ChainID: "125", Token: token, From: holdings.Zero, To: wallet, Amount: "10", Height: "10", TransactionIndex: "0", LogIndex: "0", TransactionHash: fmt.Sprintf("0x%064x", 301), BlockHash: fmt.Sprintf("0x%064x", 300)}
	// The shared Redis database is namespaced by a per-run stream key.
	stream := fmt.Sprintf("l14_transfers_%d", time.Now().UnixNano())
	options, err := goredis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	redis := goredis.NewClient(options)
	t.Cleanup(func() {
		_ = redis.Del(context.Background(), stream).Err()
		_ = redis.Close()
	})
	payload, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if err = redis.XGroupCreateMkStream(ctx, stream, "holders", "0").Err(); err != nil {
		t.Fatal(err)
	}
	id, err := redis.XAdd(ctx, &goredis.XAddArgs{Stream: stream, Values: map[string]any{"transfer": string(payload)}}).Result()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	childCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	child := exec.CommandContext(childCtx, binary, "-test.run=^TestHolderWorkerCommitBeforeAcknowledge$", "-test.count=1")
	child.Env = append(os.Environ(), "L14_BROKER_CRASH_CHILD=1", "L14_BROKER_POSTGRES="+dsn, "L14_BROKER_REDIS="+redisURL, "L14_BROKER_STREAM="+stream)
	output, childErr := child.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(childErr, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("worker crash: %v %s", childErr, output)
	}
	pending, err := redis.XPending(ctx, stream, "holders").Result()
	if err != nil || pending.Count != 1 {
		t.Fatal(pending, err)
	}
	claimed, err := redis.XClaim(ctx, &goredis.XClaimArgs{Stream: stream, Group: "holders", Consumer: "restarted-worker", MinIdle: 0, Messages: []string{id}}).Result()
	if err != nil || len(claimed) != 1 || claimed[0].ID != id {
		t.Fatal(claimed, err)
	}
	var replay holdings.Transfer
	if err = json.Unmarshal([]byte(claimed[0].Values["transfer"].(string)), &replay); err != nil {
		t.Fatal(err)
	}
	result, err := r.Apply(ctx, replay)
	if err != nil || !result.Duplicate || result.Revision != 1 {
		t.Fatal(result, err)
	}
	if n, err := redis.XAck(ctx, stream, "holders", id).Result(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	// The acknowledgement is observed from a new connection. Restarting the
	// broker is not possible against the shared operator Redis, so restart
	// durability of the acknowledgement is outside this test.
	reconnected := goredis.NewClient(options)
	t.Cleanup(func() { _ = reconnected.Close() })
	pending, err = reconnected.XPending(ctx, stream, "holders").Result()
	if err != nil || pending.Count != 0 {
		t.Fatal(pending, err)
	}
	snapshot, err := r.Snapshot(ctx, token, 100)
	if err != nil || snapshot.Supply != "10" || snapshot.Revision != 1 || len(snapshot.Balances) != 1 || snapshot.Balances[0].Raw != "10" {
		t.Fatal(snapshot, err)
	}
}

func TestHoldingsPublicHostileInputsAndDependencyLoss(t *testing.T) {
	ctx := context.Background()
	pg := internaltest.NewPostgres(t)
	st := store.New(pg)
	ddl, err := holdings.SchemaSQL("holdings_hostile")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pg.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	repository, err := holdings.NewRepository(pg, "holdings_hostile")
	if err != nil {
		t.Fatal(err)
	}
	address := func(n int) string { return fmt.Sprintf("0x%040x", n) }
	token, pool, caller, b, c := address(401), address(402), address(403), address(404), address(405)
	if _, err = st.InsertInitialPoolStats(ctx, store.InitialPoolStats{PoolAddress: pool, TokenAddress: token, CreatorAddress: &caller, Price: "0", MarketCap: "0", High24h: "0", Low24h: "0", CreatedAt: 10, UpdatedAt: 10}); err != nil {
		t.Fatal(err)
	}
	if err = repository.Discover(ctx, token, 6, "10", true); err != nil {
		t.Fatal(err)
	}
	makeEvent := func(from, to, amount string, n int) holdings.Transfer {
		return holdings.Transfer{ChainID: "125", Token: token, From: from, To: to, Amount: amount, Height: "10", TransactionIndex: "0", LogIndex: fmt.Sprint(n), TransactionHash: fmt.Sprintf("0x%064x", n+1), BlockHash: fmt.Sprintf("0x%064x", 100)}
	}
	for _, e := range []holdings.Transfer{makeEvent(holdings.Zero, b, "10", 0), makeEvent(b, c, "7", 1)} {
		if _, err = repository.Apply(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	router := chi.NewRouter()
	httpapi.RegisterTransferHolders(router, st, repository)
	request := func(path string) (int, map[string]any, string) {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/v1/pools/"+pool+"/holders"+path, nil))
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return w.Code, body, w.Header().Get("Cache-Control")
	}
	for _, query := range []string{"?limit=1&limit=2", "?cursor=a&cursor=b", "?offset=0", "?filter=unknown", "?limit=0", "?limit=-1", "?limit=1e2", "?limit=101"} {
		code, body, cache := request(query)
		if code != 422 || body["data"] != nil || cache != "private, no-store" {
			t.Fatal(query, code, body, cache)
		}
	}
	code, first, _ := request("?limit=1")
	if code != 200 {
		t.Fatal(code, first)
	}
	data := first["data"].(map[string]any)
	item := data["items"].([]any)[0].(map[string]any)
	provenance := data["lastTransferProvenance"].(map[string]any)
	if item["wallet"] != c || item["balanceRaw"] != "7" || data["supplyRaw"] != "10" || provenance["actor"] != nil || provenance["actorSource"] != "unknown" || provenance["recipient"] != c {
		t.Fatal(first)
	}
	cursor := first["nextCursor"].(string)
	if _, err = repository.Apply(ctx, makeEvent(b, c, "2", 2)); err != nil {
		t.Fatal(err)
	}
	code, stale, cache := request("?limit=1&cursor=" + url.QueryEscape(cursor))
	if code != 200 || stale["quality"] != "stale" || stale["revision"] != first["revision"] || cache != "private, no-store" {
		t.Fatal(code, stale, cache)
	}
	old := stale["data"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if old["wallet"] != b || old["balanceRaw"] != "3" {
		t.Fatal(stale)
	}
	if _, err = pg.Exec(ctx, "UPDATE holdings_hostile.tokens SET coverage='incomplete',revision=revision+1 WHERE token=$1", token); err != nil {
		t.Fatal(err)
	}
	code, incomplete, _ := request("")
	if code != 200 || incomplete["quality"] != "partial" {
		t.Fatal(code, incomplete)
	}
	partial := incomplete["data"].(map[string]any)
	for _, value := range partial["items"].([]any) {
		if value.(map[string]any)["share"] != nil {
			t.Fatal("incomplete share fabricated", partial)
		}
	}
	distribution := partial["distribution"].(map[string]any)
	if distribution["top10"].(map[string]any)["basisPoints"] != nil || distribution["creator"].(map[string]any)["basisPoints"] != nil {
		t.Fatal(distribution)
	}
	pg.Close()
	code, unavailable, cache := request("")
	if code != 503 || unavailable["code"] != "DEPENDENCY_UNAVAILABLE" || unavailable["data"] != nil || cache != "private, no-store" {
		t.Fatal(code, unavailable, cache)
	}
}
