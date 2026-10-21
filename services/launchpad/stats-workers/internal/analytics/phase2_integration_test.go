package analytics_test

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/analytics"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/risk"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	redis "github.com/redis/go-redis/v9"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func phase2Authority(t *testing.T) analytics.ReactionAuthority {
	t.Helper()
	dsn := os.Getenv("L15_TEST_DSN")
	if dsn == "" {
		t.Fatal("L15_TEST_DSN required for real PostgreSQL qualification")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("l15_phase2_%d", time.Now().UnixNano())
	if _, err = pool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanup, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
		pool.Close()
	})
	a := analytics.ReactionAuthority{Pool: pool, Schema: schema}
	for _, schemaFn := range []func() (string, error){a.SchemaSQL, a.SnapshotSQL, a.ProjectionSQL} {
		ddl, err := schemaFn()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	return a
}
func TestPhase2RiskRowToHTTPExactBoundaries(t *testing.T) {
	a := phase2Authority(t)
	ctx := context.Background()
	pool := "0x" + strings.Repeat("1", 40)
	router := chi.NewRouter()
	httpapi.RegisterDurableAnalytics(router, a)
	model := risk.ProjectionModelV1{}
	if err := model.Validate(); err != nil {
		t.Fatal(err)
	}
	for n, c := range []struct {
		top      string
		expected int
		factor   string
	}{{"3000", 0, ""}, {"3001", 5, ""}, {"5000", 5, ""}, {"5001", 15, "high_concentration"}, {"8000", 15, "high_concentration"}, {"8001", 25, "extreme_concentration"}} {
		now := time.Now().Unix()
		wm := analytics.Watermark{ChainID: "125", Height: "1", BlockHash: "0x" + strings.Repeat("a", 64), ProjectionRevision: fmt.Sprint(n + 1), ObservedAt: time.Unix(now, 0).UTC().Format("2006-01-02T15:04:05.000Z"), Integrity: "healthy"}
		payload, err := model.Calculate(analytics.RiskInputs{CreatedAt: now - 604800, Top10Concentration: c.top, CreatorHoldingsPct: "0", SupplyRaw: "100", UniqueTraders24h: 3, HolderCount: 5}, now, wm)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := a.Pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		snapshot := analytics.Snapshot{Pool: pool, Model: model.Version(), Watermark: wm, CalculatedAt: now, Payload: payload}
		if err = a.PersistSnapshot(ctx, tx, snapshot); err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", "/v2/stats/"+pool+"/risk", nil))
		if recorder.Code != 200 {
			t.Fatal(recorder.Code, recorder.Body.String())
		}
		var response struct {
			Snapshot analytics.Snapshot `json:"snapshot"`
			Stale    bool               `json:"stale"`
		}
		if json.Unmarshal(recorder.Body.Bytes(), &response) != nil {
			t.Fatal(recorder.Body.String())
		}
		var result risk.V1Result
		if err = json.Unmarshal(response.Snapshot.Payload, &result); err != nil {
			t.Fatal(err)
		}
		if result.Score == nil || *result.Score != c.expected || strings.Join(result.Factors, ",") != c.factor || response.Stale || response.Snapshot.Watermark != wm || response.Snapshot.CalculatedAt != now {
			t.Fatal(response, result)
		}
	}
}
func TestPhase2PressureNullAndReplaySQLToHTTP(t *testing.T) {
	a := phase2Authority(t)
	ctx := context.Background()
	pool := "0x" + strings.Repeat("2", 40)
	p, err := analytics.CalculatePressure(3, 1, "3000000", "1000000", false)
	if err != nil {
		t.Fatal(err)
	}
	if p.BuyCountShare == nil || *p.BuyCountShare != 7500 {
		t.Fatal(p)
	}
	empty, err := analytics.CalculatePressure(0, 0, "0", "0", false)
	if err != nil || empty.BuyCountShare != nil || empty.BuyNotionalShare != nil {
		t.Fatal(empty, err)
	}
	now := time.Now().Unix()
	wm := analytics.Watermark{ChainID: "125", Height: "2", BlockHash: "0x" + strings.Repeat("b", 64), ProjectionRevision: "1", ObservedAt: time.Unix(now, 0).UTC().Format("2006-01-02T15:04:05.000Z"), Integrity: "healthy"}
	payload, err := json.Marshal(analytics.PressureWindows{OneHour: p, TwentyFourHours: empty})
	if err != nil {
		t.Fatal(err)
	}
	s := analytics.Snapshot{Pool: pool, Model: "pressure-v1", Watermark: wm, CalculatedAt: now, Payload: payload}
	tx, err := a.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = a.PersistSnapshot(ctx, tx, s); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "INSERT INTO "+a.Schema+".analytics_outbox(pool,model,revision,payload) VALUES($1,$2,1,$3::jsonb)", pool, s.Model, string(raw)); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := a.ReplaySnapshots(ctx, pool, "0", 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].Snapshot.Watermark != wm {
		t.Fatal(page, err)
	}
	next, err := a.ReplaySnapshots(ctx, pool, page.NextCursor, 1)
	if err != nil || len(next.Items) != 0 {
		t.Fatal(next, err)
	}
	r := chi.NewRouter()
	httpapi.RegisterDurableAnalytics(r, a)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/v2/stats/"+pool+"/pressure", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "7500") || !strings.Contains(w.Body.String(), "no_activity") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestPhase2AgeAndUnknownInputsRowToHTTP(t *testing.T) {
	a := phase2Authority(t)
	ctx := context.Background()
	r := chi.NewRouter()
	httpapi.RegisterDurableAnalytics(r, a)
	for n, c := range []struct {
		age         int64
		top, supply string
		score       int
		factor      string
		unknown     bool
	}{{3599, "0", "100", 25, "very_new", false}, {3600, "0", "100", 15, "new", false}, {86399, "0", "100", 15, "new", false}, {86400, "0", "100", 5, "", false}, {604799, "0", "100", 5, "", false}, {604800, "0", "100", 0, "", false}, {604800, "abc", "100", 0, "", true}, {604800, "9000", "", 0, "", true}, {-1, "0", "100", 0, "", true}} {
		pool := fmt.Sprintf("0x%040x", n+1)
		now := time.Now().Unix()
		wm := analytics.Watermark{ChainID: "125", Height: "1", BlockHash: "0x" + strings.Repeat("c", 64), ProjectionRevision: "1", ObservedAt: time.Unix(now, 0).UTC().Format("2006-01-02T15:04:05.000Z"), Integrity: "healthy"}
		payload, err := (risk.ProjectionModelV1{}).Calculate(analytics.RiskInputs{CreatedAt: now - c.age, Top10Concentration: c.top, CreatorHoldingsPct: "0", SupplyRaw: c.supply, UniqueTraders24h: 3, HolderCount: 5}, now, wm)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := a.Pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = a.PersistSnapshot(ctx, tx, analytics.Snapshot{Pool: pool, Model: risk.ModelVersion, Watermark: wm, CalculatedAt: now, Payload: payload}); err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		r.ServeHTTP(recorder, httptest.NewRequest("GET", "/v2/stats/"+pool+"/risk", nil))
		if recorder.Code != 200 {
			t.Fatal(recorder.Code)
		}
		var response struct {
			Snapshot analytics.Snapshot `json:"snapshot"`
		}
		if json.Unmarshal(recorder.Body.Bytes(), &response) != nil {
			t.Fatal(recorder.Body.String())
		}
		var result risk.V1Result
		if err = json.Unmarshal(response.Snapshot.Payload, &result); err != nil {
			t.Fatal(err)
		}
		if c.unknown {
			if result.Score != nil || result.Coverage != "incomplete" || len(result.Unavailable) == 0 {
				t.Fatal(c, result)
			}
		} else if result.Score == nil || *result.Score != c.score || strings.Join(result.Factors, ",") != c.factor {
			t.Fatal(c, result)
		}
	}
}

func TestPhase2RealSignedReactionTxRollbackAndReplay(t *testing.T) {
	a := phase2Authority(t)
	ctx := context.Background()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	wallet := strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
	i := analytics.Intent{ChainID: "125", Pool: "0x" + strings.Repeat("5", 40), Wallet: wallet, Reaction: "hot", Nonce: "signed_nonce", Expiry: time.Now().Unix() + 120}
	canonical, err := i.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	signedBytes := append([]byte("KindleLaunch reaction v1\n"), canonical...)
	signature, err := crypto.Sign(accounts.TextHash(signedBytes), key)
	if err != nil {
		t.Fatal(err)
	}
	encoded := "0x" + hex.EncodeToString(signature)
	verify := func(_ context.Context, wallet string, message []byte) error {
		if !auth.VerifyWalletSignature(wallet, string(message), encoded) {
			return auth.ErrScopeDenied
		}
		return nil
	}
	tx, err := a.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.ApplyInTx(ctx, tx, i, verify)
	if err != nil || result.Counts["hot"] != 1 {
		tx.Rollback(ctx)
		t.Fatal(result, err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	public, err := a.ReadReactions(ctx, i.Pool)
	if err != nil || public.Counts["hot"] != 0 || public.Revision != "0" {
		t.Fatal(public, err)
	}
	result, err = a.Apply(ctx, i, verify)
	if err != nil {
		t.Fatal(err)
	}
	again, err := a.Apply(ctx, i, verify)
	if err != nil || again.Revision != result.Revision {
		t.Fatal(again, err)
	}
	changed := i
	changed.Pool = "0x" + strings.Repeat("6", 40)
	if _, err = a.Apply(ctx, changed, verify); !errors.Is(err, auth.ErrScopeDenied) {
		t.Fatal("changed pool accepted by old signature", err)
	}
	var leaked int
	if err = a.Pool.QueryRow(ctx, "SELECT count(*) FROM "+a.Schema+".reaction_outbox WHERE payload ? 'userVote' OR payload ? 'wallet'").Scan(&leaked); err != nil || leaked != 0 {
		t.Fatal(leaked, err)
	}
}

func TestPhase2AcknowledgedReactionsSurviveRealRedisRestart(t *testing.T) {
	a := phase2Authority(t)
	ctx := context.Background()
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Fatal("real redis-server fixture required", err)
	}
	dir := t.TempDir()
	socket := filepath.Join(dir, "redis.sock")
	var process *exec.Cmd
	start := func() {
		process = exec.Command(binary, "--port", "0", "--unixsocket", socket, "--unixsocketperm", "700", "--dir", dir, "--save", "", "--appendonly", "no")
		if err := process.Start(); err != nil {
			t.Fatal(err)
		}
	}
	stop := func() {
		if process != nil {
			if err := process.Process.Signal(syscall.SIGTERM); err != nil {
				t.Error(err)
			}
			if err := process.Wait(); err != nil {
				t.Error(err)
			}
			process = nil
		}
	}
	start()
	defer stop()
	cache := redis.NewClient(&redis.Options{Network: "unix", Addr: socket})
	defer cache.Close()
	waitReady := func() {
		deadline := time.Now().Add(3 * time.Second)
		for {
			bounded, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
			err := cache.Ping(bounded).Err()
			cancel()
			if err == nil {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("real Redis unavailable", err)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitReady()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	wallet := strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
	pool := "0x" + strings.Repeat("7", 40)
	apply := func(reaction, nonce string, revision int64) {
		i := analytics.Intent{ChainID: "125", Pool: pool, Wallet: wallet, Reaction: reaction, Nonce: nonce, Expiry: time.Now().Unix() + 120, ExpectedRevision: revision}
		canonical, err := i.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		message := append([]byte("KindleLaunch reaction v1\n"), canonical...)
		signature, err := crypto.Sign(accounts.TextHash(message), key)
		if err != nil {
			t.Fatal(err)
		}
		encoded := "0x" + hex.EncodeToString(signature)
		_, err = a.Apply(ctx, i, func(_ context.Context, wallet string, message []byte) error {
			if !auth.VerifyWalletSignature(wallet, string(message), encoded) {
				return auth.ErrScopeDenied
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	apply("hot", "redis_hot", 0)
	time.Sleep(5100 * time.Millisecond)
	apply("diamond", "redis_diamond", 1)
	if err = cache.Set(ctx, "l15:cache:"+pool, "stale-cache", 0).Err(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	wm := analytics.Watermark{ChainID: "125", Height: "1", BlockHash: "0x" + strings.Repeat("e", 64), ProjectionRevision: "1", ObservedAt: time.Unix(now-31, 0).UTC().Format("2006-01-02T15:04:05.000Z"), Integrity: "healthy"}
	pressure, err := analytics.CalculatePressure(3, 1, "3000000", "1000000", false)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(analytics.PressureWindows{OneHour: pressure, TwentyFourHours: pressure})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := analytics.Snapshot{Pool: pool, Model: "pressure-v1", Watermark: wm, CalculatedAt: now - 31, Payload: payload}
	tx, err := a.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.PersistSnapshot(ctx, tx, snapshot); err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	cacheKey, err := snapshot.CacheKey()
	if err != nil {
		t.Fatal(err)
	}
	snapshotRaw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err = cache.Set(ctx, cacheKey, snapshotRaw, 0).Err(); err != nil {
		t.Fatal(err)
	}
	stop()
	start()
	waitReady()
	if _, err = cache.Get(ctx, cacheKey).Result(); err != redis.Nil {
		t.Fatal("canonical cache survived volatile Redis restart", err)
	}
	if _, err = cache.Get(ctx, "l15:cache:"+pool).Result(); err != redis.Nil {
		t.Fatal("Redis restart unexpectedly retained volatile cache", err)
	}
	router := chi.NewRouter()
	httpapi.RegisterDurableAnalytics(router, a)
	staleResponse := httptest.NewRecorder()
	router.ServeHTTP(staleResponse, httptest.NewRequest("GET", "/v2/stats/"+pool+"/pressure", nil))
	var stale analytics.SnapshotRead
	if staleResponse.Code != 200 || json.Unmarshal(staleResponse.Body.Bytes(), &stale) != nil || !stale.Stale || stale.Snapshot.CalculatedAt != snapshot.CalculatedAt || stale.Snapshot.Watermark != wm {
		t.Fatal("Redis restart changed durable freshness", staleResponse.Code, staleResponse.Body.String())
	}
	err = httpapi.RegisterDurableReactions(router, httpapi.DurableReactionDeps{Authority: a, Policies: auth.PolicyRegistry{Version: "phase2-fixture", Actions: map[string]auth.Policy{"reaction.replace": {Scope: "wallet", OwnerRequired: true, RouteTemplate: "/v2/stats/{poolAddress}/reactions"}}}})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/v2/stats/"+pool+"/reactions", nil))
	if recorder.Code != 200 {
		t.Fatal(recorder.Code, recorder.Body.String())
	}
	var result analytics.PublicReactions
	if err = json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || result.Counts["hot"] != 0 || result.Counts["diamond"] != 1 || result.Revision != "2" {
		t.Fatal(result, err)
	}
}

func phase3ReactionSignature(t *testing.T, key *ecdsa.PrivateKey, intent analytics.Intent) string {
	t.Helper()
	canonical, err := intent.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	signature, err := crypto.Sign(accounts.TextHash(append([]byte("KindleLaunch reaction v1\n"), canonical...)), key)
	if err != nil {
		t.Fatal(err)
	}
	return "0x" + hex.EncodeToString(signature)
}
func phase3ReactionVerifier(signature string) func(context.Context, string, []byte) error {
	return func(_ context.Context, wallet string, message []byte) error {
		if !auth.VerifyWalletSignature(wallet, string(message), signature) {
			return auth.ErrScopeDenied
		}
		return nil
	}
}
func TestPhase3SignedNonceConflictConcurrencyAndCorruptReplay(t *testing.T) {
	a := phase2Authority(t)
	ctx := context.Background()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	wallet := strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
	intent := analytics.Intent{ChainID: "125", Pool: "0x" + strings.Repeat("a", 40), Wallet: wallet, Reaction: "hot", Nonce: "phase3-nonce", Expiry: time.Now().Unix() + 120}
	signature := phase3ReactionSignature(t, key, intent)
	accepted, err := a.Apply(ctx, intent, phase3ReactionVerifier(signature))
	if err != nil || accepted.Revision != 1 {
		t.Fatal(accepted, err)
	}
	for _, change := range []func(*analytics.Intent){func(i *analytics.Intent) { i.Pool = "0x" + strings.Repeat("b", 40) }, func(i *analytics.Intent) { i.Reaction = "diamond" }, func(i *analytics.Intent) { i.ExpectedRevision = 1 }} {
		changed := intent
		change(&changed)
		signed := phase3ReactionSignature(t, key, changed)
		if _, err = a.Apply(ctx, changed, phase3ReactionVerifier(signed)); !errors.Is(err, analytics.ErrConflict) {
			t.Fatal("same nonce conflicting signed intent accepted", err)
		}
	}
	if _, err = a.Pool.Exec(ctx, "UPDATE "+a.Schema+".reaction_wallets SET last_change=clock_timestamp()-interval '6 seconds'"); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		result analytics.ReactionResult
		err    error
	}
	done := make(chan outcome, 2)
	for _, reaction := range []string{"diamond", "warning"} {
		changed := intent
		changed.Reaction = reaction
		changed.Nonce = "parallel-" + reaction
		changed.ExpectedRevision = 1
		signed := phase3ReactionSignature(t, key, changed)
		go func(i analytics.Intent, s string) {
			result, err := a.Apply(ctx, i, phase3ReactionVerifier(s))
			done <- outcome{result, err}
		}(changed, signed)
	}
	success, limited := 0, 0
	for n := 0; n < 2; n++ {
		out := <-done
		if out.err == nil {
			success++
		} else if errors.Is(out.err, analytics.ErrCooldown) {
			limited++
		} else {
			t.Fatal(out.err)
		}
	}
	if success != 1 || limited != 1 {
		t.Fatal(success, limited)
	}
	public, err := a.ReadReactions(ctx, intent.Pool)
	if err != nil || public.Revision != "2" || public.Counts["hot"] != 0 || public.Counts["diamond"]+public.Counts["warning"] != 1 {
		t.Fatal(public, err)
	}
	var rows, nonces, outbox int
	if err = a.Pool.QueryRow(ctx, "SELECT count(*) FROM "+a.Schema+".reaction_votes").Scan(&rows); err != nil || rows != 1 {
		t.Fatal(rows, err)
	}
	if err = a.Pool.QueryRow(ctx, "SELECT count(*) FROM "+a.Schema+".reaction_nonces").Scan(&nonces); err != nil || nonces != 2 {
		t.Fatal(nonces, err)
	}
	if err = a.Pool.QueryRow(ctx, "SELECT count(*) FROM "+a.Schema+".reaction_outbox").Scan(&outbox); err != nil || outbox != 2 {
		t.Fatal(outbox, err)
	}
	if _, err = a.Pool.Exec(ctx, "UPDATE "+a.Schema+".reaction_nonces SET result=jsonb_set(result,'{revision}','999') WHERE nonce=$1", intent.Nonce); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Apply(ctx, intent, phase3ReactionVerifier(signature)); !errors.Is(err, analytics.ErrConflict) {
		t.Fatal("corrupt original result served", err)
	}
}

type phase3ReactionCrashRecord struct {
	Intent    analytics.Intent
	Signature string
}

func TestPhase3ReactionCrashHelper(t *testing.T) {
	if os.Getenv("L15_REACTION_CRASH_HELPER") != "1" {
		return
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("L15_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	a := analytics.ReactionAuthority{Pool: pool, Schema: os.Getenv("L15_CRASH_SCHEMA")}
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	i := analytics.Intent{ChainID: "125", Pool: "0x" + strings.Repeat("c", 40), Wallet: strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex()), Reaction: "hot", Nonce: "crash-reaction", Expiry: time.Now().Unix() + 120}
	record := phase3ReactionCrashRecord{Intent: i, Signature: phase3ReactionSignature(t, key, i)}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = a.ApplyInTx(ctx, tx, i, phase3ReactionVerifier(record.Signature)); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("L15_CRASH_STAGE") == "after_commit" {
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(os.Getenv("L15_CRASH_MARKER")+".tmp", raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(os.Getenv("L15_CRASH_MARKER")+".tmp", os.Getenv("L15_CRASH_MARKER")); err != nil {
		t.Fatal(err)
	}
	select {}
}
func TestPhase3SignedReactionWorkerCrashConsistency(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"before_commit", "after_commit"} {
		t.Run(stage, func(t *testing.T) {
			a := phase2Authority(t)
			ctx := context.Background()
			dir := t.TempDir()
			marker := filepath.Join(dir, "intent.json")
			logPath := filepath.Join(dir, "worker.log")
			log, err := os.Create(logPath)
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			child := exec.Command(executable, "-test.run=^TestPhase3ReactionCrashHelper$", "-test.timeout=20s")
			child.Env = append(os.Environ(), "L15_REACTION_CRASH_HELPER=1", "L15_CRASH_SCHEMA="+a.Schema, "L15_CRASH_STAGE="+stage, "L15_CRASH_MARKER="+marker)
			child.Stdout, child.Stderr = log, log
			if err = child.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if child.ProcessState == nil {
					child.Process.Kill()
					child.Wait()
				}
			}()
			deadline := time.Now().Add(10 * time.Second)
			var raw []byte
			for {
				raw, err = os.ReadFile(marker)
				if err == nil {
					break
				}
				if time.Now().After(deadline) {
					data, _ := os.ReadFile(logPath)
					t.Fatal("reaction worker did not reach crash boundary", string(data))
				}
				time.Sleep(10 * time.Millisecond)
			}
			var record phase3ReactionCrashRecord
			if json.Unmarshal(raw, &record) != nil {
				t.Fatal("invalid real signed intent")
			}
			if err = child.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			err = child.Wait()
			var exited *exec.ExitError
			if !errors.As(err, &exited) || !exited.ProcessState.Sys().(syscall.WaitStatus).Signaled() || exited.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
				t.Fatal("real reaction worker SIGKILL required", err)
			}
			before, err := a.ReadReactions(ctx, record.Intent.Pool)
			if err != nil {
				t.Fatal(err)
			}
			want := int64(0)
			if stage == "after_commit" {
				want = 1
			}
			if before.Counts["hot"] != want {
				t.Fatal(before, stage)
			}
			result, err := a.Apply(ctx, record.Intent, phase3ReactionVerifier(record.Signature))
			if err != nil || result.Revision != 1 || result.PoolRevision != 1 || result.Counts["hot"] != 1 {
				t.Fatal(result, err)
			}
			duplicate, err := a.Apply(ctx, record.Intent, phase3ReactionVerifier(record.Signature))
			if err != nil || !reflect.DeepEqual(result, duplicate) {
				t.Fatal(result, duplicate, err)
			}
			for _, table := range []string{"reaction_votes", "reaction_nonces", "reaction_outbox"} {
				var count int
				if err = a.Pool.QueryRow(ctx, "SELECT count(*) FROM "+a.Schema+"."+table).Scan(&count); err != nil || count != 1 {
					t.Fatal(table, count, err)
				}
			}
		})
	}
}
