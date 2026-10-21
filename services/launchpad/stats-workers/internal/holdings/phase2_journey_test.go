package holdings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func phase2Repository(t *testing.T) (context.Context, *Repository) {
	t.Helper()
	dsn, schema := os.Getenv("L14_POSTGRES_DSN"), os.Getenv("L14_POSTGRES_SCHEMA")
	if dsn == "" || schema == "" {
		t.Fatal("real leased L14_POSTGRES_DSN and L14_POSTGRES_SCHEMA required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	ddl, err := SchemaSQL(schema)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	r, err := NewRepository(pool, schema)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, r
}
func applyFixture(t *testing.T, ctx context.Context, r *Repository, e Transfer) {
	t.Helper()
	if _, err := r.Apply(ctx, e); err != nil {
		t.Fatal(err)
	}
}
func TestDurableTransferJourney(t *testing.T) {
	ctx, r := phase2Repository(t)
	token, a, b, c, pool := addr(1), addr(2), addr(3), addr(4), addr(5)
	if err := r.Discover(ctx, token, 6, "10", true); err != nil {
		t.Fatal(err)
	}
	mint := event(token, Zero, a, "100", 0)
	applyFixture(t, ctx, r, mint)
	applyFixture(t, ctx, r, event(token, a, b, "25", 1))
	applyFixture(t, ctx, r, event(token, b, Zero, "5", 2))
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{a, b} {
		if err = r.ClassifyTx(ctx, tx, token, address, "external_wallet", strings.Repeat("a", 64), 1); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	cursor, err := r.Materialize(ctx, token, pool, a)
	if err != nil {
		t.Fatal(err)
	}
	if len(cursor) > 256 {
		t.Fatal("frozen cursor length exceeded")
	}
	first, err := r.ReadPage(ctx, token, pool, cursor, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Snapshot.Supply != "95" || first.Snapshot.Balances[0].Raw != "75" || first.Next == nil || first.Distribution.Creator.BasisPoints == nil || *first.Distribution.Creator.BasisPoints != "7894" || first.Distribution.Top10.BasisPoints == nil || *first.Distribution.Top10.BasisPoints != "10000" {
		t.Fatal(first)
	}
	applyFixture(t, ctx, r, event(token, a, c, "70", 3))
	second, err := r.ReadPage(ctx, token, pool, *first.Next, 1)
	if err != nil || second.Snapshot.Revision != first.Snapshot.Revision || len(second.Snapshot.Balances) != 1 || second.Snapshot.Balances[0].Address != b || second.Snapshot.Balances[0].Raw != "20" {
		t.Fatal(second, err)
	}
	if _, err = r.ReadPage(ctx, token, addr(99), cursor, 1); !errors.Is(err, ErrRestartRequired) {
		t.Fatal(err)
	}
	if _, err = r.ReadPage(ctx, token, pool, "bad", 1); !errors.Is(err, ErrCursor) {
		t.Fatal(err)
	}
	if _, err = r.pool.Exec(ctx, "UPDATE "+r.schema+".snapshots SET expires_at=clock_timestamp()-interval '1 second' WHERE snapshot_id=$1", first.SnapshotID); err != nil {
		t.Fatal(err)
	}
	if _, err = r.ReadPage(ctx, token, pool, cursor, 1); !errors.Is(err, ErrRestartRequired) {
		t.Fatal(err)
	}
	tx, err = r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.ApplyTx(ctx, tx, event(token, b, c, "21", 4))
	if err != nil || !result.Incident {
		t.Fatal(result, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	s, err := r.Snapshot(ctx, token, 100)
	if err != nil || s.Supply != "95" || s.Coverage != "quarantined" {
		t.Fatal(s, err)
	}
	var n int
	if err = r.pool.QueryRow(ctx, "SELECT count(*) FROM "+r.schema+".incidents").Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var provenance Provenance
	var raw []byte
	if err = r.pool.QueryRow(ctx, "SELECT payload FROM "+r.schema+".provenance WHERE identity=$1", mint.Identity()).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &provenance); err != nil || provenance.Recipient != a || provenance.Actor != nil || provenance.RecipientSource != RecipientTransfer {
		t.Fatal(provenance, err)
	}
}
func TestTransferTransactionRollbackAndCategories(t *testing.T) {
	ctx, r := phase2Repository(t)
	token, reserve, a, b := addr(10), addr(11), addr(12), addr(13)
	if err := r.Discover(ctx, token, 18, "10", true); err != nil {
		t.Fatal(err)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.ApplyTx(ctx, tx, event(token, Zero, reserve, "90", 0)); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	s, err := r.Snapshot(ctx, token, 100)
	if err != nil || s.Revision != 0 || s.Supply != "0" {
		t.Fatal(s, err)
	}
	for _, e := range []Transfer{event(token, Zero, reserve, "90", 0), event(token, Zero, a, "5", 1), event(token, Zero, b, "5", 2)} {
		applyFixture(t, ctx, r, e)
	}
	tx, err = r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for address, category := range map[string]string{reserve: "pool_reserve", a: "external_wallet", b: "external_wallet"} {
		if err = r.ClassifyTx(ctx, tx, token, address, category, strings.Repeat("b", 64), 1); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	cursor, err := r.Materialize(ctx, token, addr(14), "")
	if err != nil {
		t.Fatal(err)
	}
	page, err := r.ReadPage(ctx, token, addr(14), cursor, 100)
	if err != nil {
		t.Fatal(err)
	}
	if page.Distribution.AllPositiveCount != 3 || page.Distribution.ExternalWalletCount != 2 || page.Distribution.CirculatingRaw != "10" || page.Distribution.Supply != "100" {
		t.Fatal(page.Distribution)
	}
}

func TestTransferCrashRecovery(t *testing.T) {
	if stage := os.Getenv("L14_CRASH_STAGE"); stage != "" {
		ctx := context.Background()
		pool, err := pgxpool.New(ctx, os.Getenv("L14_POSTGRES_DSN"))
		if err != nil {
			t.Fatal(err)
		}
		r, err := NewRepository(pool, os.Getenv("L14_POSTGRES_SCHEMA"))
		if err != nil {
			t.Fatal(err)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.ApplyTx(ctx, tx, event(os.Getenv("L14_CRASH_TOKEN"), Zero, addr(201), "10", 0)); err != nil {
			t.Fatal(err)
		}
		if stage == "after_commit" {
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		} else if stage != "before_commit" {
			t.Fatal("unsupported crash stage")
		}
		os.Exit(23)
	}
	ctx, r := phase2Repository(t)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for i, stage := range []string{"before_commit", "after_commit"} {
		token := addr(200 + i)
		if err = r.Discover(ctx, token, 6, "10", true); err != nil {
			t.Fatal(err)
		}
		child := exec.CommandContext(ctx, binary, "-test.run=^TestTransferCrashRecovery$", "-test.count=1")
		child.Env = append(os.Environ(), "L14_CRASH_STAGE="+stage, "L14_CRASH_TOKEN="+token)
		output, childErr := child.CombinedOutput()
		var exited *exec.ExitError
		if !errors.As(childErr, &exited) || exited.ExitCode() != 23 {
			t.Fatalf("crash %s: %v %s", stage, childErr, output)
		}
		s, err := r.Snapshot(ctx, token, 100)
		if err != nil {
			t.Fatal(err)
		}
		wantSupply, wantRevision := "0", int64(0)
		if stage == "after_commit" {
			wantSupply, wantRevision = "10", 1
		}
		if s.Supply != wantSupply || s.Revision != wantRevision {
			t.Fatal(stage, s)
		}
		mint := event(token, Zero, addr(201), "10", 0)
		result, err := r.Apply(ctx, mint)
		if err != nil || result.Duplicate != (stage == "after_commit") {
			t.Fatal(stage, result, err)
		}
		s, err = r.Snapshot(ctx, token, 100)
		if err != nil || s.Supply != "10" || s.Revision != 1 || len(s.Balances) != 1 || s.Balances[0].Raw != "10" {
			t.Fatal(s, err)
		}
		var transfers, provenance int
		if err = r.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM "+r.schema+".transfers WHERE token=$1),(SELECT count(*) FROM "+r.schema+".provenance p JOIN "+r.schema+".transfers t USING(identity) WHERE t.token=$1)", token).Scan(&transfers, &provenance); err != nil || transfers != 1 || provenance != 1 {
			t.Fatal(transfers, provenance, err)
		}
	}
}

func TestHostileTransferLeavesNoPartialEffect(t *testing.T) {
	ctx, r := phase2Repository(t)
	token, owner, recipient := addr(210), addr(211), addr(212)
	if err := r.Discover(ctx, token, 6, "10", true); err != nil {
		t.Fatal(err)
	}
	mint := event(token, Zero, owner, "10", 0)
	applyFixture(t, ctx, r, mint)
	base := event(token, owner, recipient, "1", 1)
	inputs := []Transfer{}
	for _, amount := range []string{"", "-1", "+1", "01", "1.0", "1e1", " 1", strings.Repeat("9", 78)} {
		e := base
		e.Amount = amount
		inputs = append(inputs, e)
	}
	for _, field := range []string{"chain", "token", "from", "to", "transaction", "block", "height", "index"} {
		e := base
		switch field {
		case "chain":
			e.ChainID = ""
		case "token":
			e.Token = Zero
		case "from":
			e.From = "0x1234"
		case "to":
			e.To = ""
		case "transaction":
			e.TransactionHash = fmt.Sprintf("0x%064x", 0)
		case "block":
			e.BlockHash = fmt.Sprintf("0x%064x", 0)
		case "height":
			e.Height = "-1"
		case "index":
			e.LogIndex = "1.5"
		}
		inputs = append(inputs, e)
	}
	conflict := mint
	conflict.Amount = "9"
	inputs = append(inputs, conflict)
	conflict = mint
	conflict.BlockHash = fmt.Sprintf("0x%064x", 200)
	inputs = append(inputs, conflict)
	for i, e := range inputs {
		tx, err := r.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.ApplyTx(ctx, tx, e); err == nil {
			t.Fatal("accepted hostile input", i, e)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal("rejected mutation poisoned caller transaction", i, err)
		}
		s, err := r.Snapshot(ctx, token, 100)
		if err != nil || s.Supply != "10" || s.Revision != 1 || s.Coverage != "complete" || len(s.Balances) != 1 || s.Balances[0].Address != owner || s.Balances[0].Raw != "10" {
			t.Fatal(i, s, err)
		}
		var effects int
		if err = r.pool.QueryRow(ctx, "SELECT count(*) FROM "+r.schema+".transfers WHERE token=$1", token).Scan(&effects); err != nil || effects != 1 {
			t.Fatal(i, effects, err)
		}
	}
	if _, err := r.ApplyCanonicalTransfer(ctx, base, "missing_provenance"); err == nil {
		t.Fatal("unproven Transfer accepted")
	}
}

func TestAccountingIntegrityErrorRollsBackSavepoint(t *testing.T) {
	ctx, r := phase2Repository(t)
	token, owner, recipient := addr(220), addr(221), addr(222)
	if err := r.Discover(ctx, token, 6, "10", true); err != nil {
		t.Fatal(err)
	}
	applyFixture(t, ctx, r, event(token, Zero, owner, "10", 0))
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE "+r.schema+".aggregates SET raw=raw-1 WHERE token=$1 AND raw>0", token); err != nil {
		t.Fatal(err)
	}
	if _, err = r.ApplyTx(ctx, tx, event(token, owner, recipient, "1", 1)); !errors.Is(err, ErrIntegrity) {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	s, err := r.Snapshot(ctx, token, 100)
	if err != nil || s.Revision != 1 || len(s.Balances) != 1 || s.Balances[0].Raw != "10" {
		t.Fatal(s, err)
	}
	if _, err = r.Materialize(ctx, token, addr(223), owner); !errors.Is(err, ErrIntegrity) {
		t.Fatal("inconsistent aggregate advertised", err)
	}
}

func TestNormalizedConcurrentDuplicateTransitions(t *testing.T) {
	ctx, r := phase2Repository(t)
	token, a, b := addr(250), addr(251), addr(252)
	if err := r.Discover(ctx, token, 6, "10", true); err != nil {
		t.Fatal(err)
	}
	for _, e := range []Transfer{event(token, Zero, a, "100", 0), event(token, a, b, "25", 1), event(token, b, Zero, "5", 2)} {
		var work sync.WaitGroup
		errorsFound := make(chan error, 16)
		for i := 0; i < 16; i++ {
			input := e
			if i%2 == 0 {
				input.Token = "0x" + strings.ToUpper(input.Token[2:])
				input.From = "0x" + strings.ToUpper(input.From[2:])
				input.To = "0x" + strings.ToUpper(input.To[2:])
			}
			work.Add(1)
			go func(input Transfer) { defer work.Done(); _, err := r.Apply(ctx, input); errorsFound <- err }(input)
		}
		work.Wait()
		close(errorsFound)
		for err := range errorsFound {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	s, err := r.Snapshot(ctx, token, 100)
	if err != nil || s.Revision != 3 || s.Supply != "95" || len(s.Balances) != 2 || s.Balances[0].Address != a || s.Balances[0].Raw != "75" || s.Balances[1].Address != b || s.Balances[1].Raw != "20" {
		t.Fatal(s, err)
	}
	var identities, provenance int64
	var total string
	if err = r.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM "+r.schema+".transfers WHERE token=$1),(SELECT count(*) FROM "+r.schema+".provenance p JOIN "+r.schema+".transfers t USING(identity) WHERE t.token=$1),(SELECT sum(raw)::text FROM "+r.schema+".aggregates WHERE token=$1)", token).Scan(&identities, &provenance, &total); err != nil || identities != 3 || provenance != 3 || total != "95" {
		t.Fatal(identities, provenance, total, err)
	}
}
