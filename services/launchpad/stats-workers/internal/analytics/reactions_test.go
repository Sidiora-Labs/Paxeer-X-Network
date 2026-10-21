package analytics

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"sync"
	"testing"
	"time"
)

func TestReactionPostgresAuthority(t *testing.T) {
	dsn := os.Getenv("L15_TEST_DSN")
	if dsn == "" {
		t.Fatal("explicit private PostgreSQL fixture required")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	schema := fmt.Sprintf("kd_ce218285d3bff82119d97ee8_r_%x", time.Now().UnixNano())
	a := ReactionAuthority{pool, schema}
	if _, e = pool.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if _, e := pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); e != nil {
			t.Error(e)
		}
	}()
	ddl, e := a.SchemaSQL()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, ddl); e != nil {
		t.Fatal(e)
	}
	i := Intent{"125", "0x1111111111111111111111111111111111111111", "0x2222222222222222222222222222222222222222", "hot", "first", time.Now().Unix() + 120, 0}
	verify := func(context.Context, string, []byte) error { return nil } // authorized arithmetic/storage fixture, not signature proof
	r, e := a.Apply(ctx, i, verify)
	if e != nil || r.Counts["hot"] != 1 {
		t.Fatal(r, e)
	}
	r, e = a.Apply(ctx, i, verify)
	if e != nil || r.Revision != 1 {
		t.Fatal(r, e)
	}
	other := i
	other.Pool = "0x3333333333333333333333333333333333333333"
	if _, e = a.Apply(ctx, other, verify); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	i.Nonce = "second"
	i.Reaction = "diamond"
	i.ExpectedRevision = 1
	if _, e = a.Apply(ctx, i, verify); !errors.Is(e, ErrCooldown) {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "UPDATE "+schema+".reaction_wallets SET last_change=clock_timestamp()-interval '6 seconds'"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, nonce := range []string{"second", "third"} {
		wg.Add(1)
		go func(n string) { defer wg.Done(); j := i; j.Nonce = n; _, e := a.Apply(ctx, j, verify); results <- e }(nonce)
	}
	wg.Wait()
	close(results)
	ok, limited := 0, 0
	for e := range results {
		if e == nil {
			ok++
		} else if errors.Is(e, ErrCooldown) {
			limited++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 1 || limited != 1 {
		t.Fatal(ok, limited)
	}
	var hot, diamond, outbox int
	if e = pool.QueryRow(ctx, "SELECT count(*) FILTER(WHERE reaction='hot'),count(*) FILTER(WHERE reaction='diamond') FROM "+schema+".reaction_votes").Scan(&hot, &diamond); e != nil || hot != 0 || diamond != 1 {
		t.Fatal(hot, diamond, e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM "+schema+".reaction_outbox WHERE payload ? 'userVote'").Scan(&outbox); e != nil || outbox != 0 {
		t.Fatal(outbox, e)
	}
	// New authority process connection reads PostgreSQL without any Redis state.
	second, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer second.Close()
	if e = second.QueryRow(ctx, "SELECT count(*) FROM "+schema+".reaction_votes WHERE reaction='diamond'").Scan(&diamond); e != nil || diamond != 1 {
		t.Fatal(diamond, e)
	}
	if _, e = pool.Exec(ctx, "UPDATE "+schema+".reaction_wallets SET last_change=clock_timestamp()-interval '6 seconds'"); e != nil {
		t.Fatal(e)
	}
	i.Nonce = "clear"
	i.Reaction = "clear"
	i.ExpectedRevision = 2
	cleared, e := a.Apply(ctx, i, verify)
	if e != nil || cleared.Vote != "clear" || cleared.Counts["diamond"] != 0 || cleared.PoolRevision != 3 {
		t.Fatal(cleared, e)
	}
	if _, e = a.Apply(ctx, i, nil); e == nil {
		t.Fatal("missing verifier accepted")
	}
	// Force the authority to wait behind a real PostgreSQL pool lock. The
	// acceptance clock must be read after that wait, not before it.
	config, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	config.ConnConfig.RuntimeParams["application_name"] = schema
	waiting, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	defer waiting.Close()
	blocked := func(j Intent, holdSeconds float64) (ReactionResult, error) {
		blocker, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer blocker.Rollback(ctx)
		if _, e = blocker.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", schema+":"+j.Pool); e != nil {
			t.Fatal(e)
		}
		type outcome struct {
			r ReactionResult
			e error
		}
		done := make(chan outcome, 1)
		go func() { r, e := (ReactionAuthority{waiting, schema}).Apply(ctx, j, verify); done <- outcome{r, e} }()
		deadline := time.Now().Add(3 * time.Second)
		for {
			var n int
			if e = pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE application_name=$1 AND wait_event='advisory'", schema).Scan(&n); e != nil {
				t.Fatal(e)
			}
			if n == 1 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("authority did not block on owned lock")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if _, e = blocker.Exec(ctx, "SELECT pg_sleep($1)", holdSeconds); e != nil {
			t.Fatal(e)
		}
		if e = blocker.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		select {
		case out := <-done:
			return out.r, out.e
		case <-time.After(3 * time.Second):
			t.Fatal("authority did not resume")
			return ReactionResult{}, nil
		}
	}
	j := i
	j.Wallet = "0x4444444444444444444444444444444444444444"
	j.Nonce = "blocked_expiry"
	j.Reaction = "hot"
	j.ExpectedRevision = 0
	j.Expiry = time.Now().Unix() + 1
	if _, e = blocked(j, 1.5); e == nil {
		t.Fatal("expired blocked intent accepted")
	}
	j.Nonce = "blocked_accept"
	j.Expiry = time.Now().Unix() + 120
	if _, e = blocked(j, 5.2); e != nil {
		t.Fatal(e)
	}
	j.Nonce = "blocked_cooldown"
	j.ExpectedRevision = 1
	if _, e = (ReactionAuthority{waiting, schema}).Apply(ctx, j, verify); !errors.Is(e, ErrCooldown) {
		t.Fatal("cooldown started before lock wait completed", e)
	}
	cancel, c := context.WithCancel(ctx)
	c()
	i.Nonce = "cancel"
	if _, e = a.Apply(cancel, i, verify); e == nil {
		t.Fatal("cancelled mutation succeeded")
	}
}
func TestReactionIntentBounds(t *testing.T) {
	i := Intent{"125", "0x1111111111111111111111111111111111111111", "0x2222222222222222222222222222222222222222", "hot", "one", 123, 0}
	if _, e := i.Canonical(); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*Intent){func(j *Intent) { j.ChainID = "1" }, func(j *Intent) { j.Pool = "bad" }, func(j *Intent) { j.Nonce = "" }, func(j *Intent) { j.Reaction = "other" }, func(j *Intent) { j.ExpectedRevision = -1 }} {
		j := i
		mutate(&j)
		if _, e := j.Canonical(); e == nil {
			t.Fatal(j)
		}
	}
	if _, e := (ReactionAuthority{}).Apply(context.Background(), i, nil); e == nil {
		t.Fatal("unavailable DB accepted")
	}
}
