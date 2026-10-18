package holdings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"math/big"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func addr(n int) string { return fmt.Sprintf("0x%040x", n) }
func event(token, from, to, amount string, n int) Transfer {
	return Transfer{ChainID: "125", Token: token, From: from, To: to, Amount: amount, Height: "10", TransactionIndex: "0", LogIndex: fmt.Sprint(n), TransactionHash: fmt.Sprintf("0x%064x", n+1), BlockHash: fmt.Sprintf("0x%064x", 100)}
}
func TestExactArithmetic(t *testing.T) {
	s, a, b, err := Mutation(event(addr(1), addr(2), addr(3), "25", 1), "100", "100", "0")
	if err != nil || s != "100" || a != "75" || b != "25" {
		t.Fatal(s, a, b, err)
	}
	s, b, _, err = Mutation(event(addr(1), addr(3), Zero, "5", 2), s, b, "0")
	if err != nil || s != "95" || b != "20" {
		t.Fatal(s, b, err)
	}
	r := BasisPoints("75", "95")
	if r.BasisPoints == nil || *r.BasisPoints != "7894" {
		t.Fatal(r)
	}
	if BasisPoints("1", "0").Reason != "zero_supply" {
		t.Fatal("zero denominator")
	}
	if _, _, _, err = Mutation(event(addr(1), addr(2), addr(3), "11", 3), "10", "10", "0"); !errors.Is(err, ErrIntegrity) {
		t.Fatal(err)
	}
	for _, s := range []string{"-1", "01", "1.0", "1e2", strings.Repeat("9", 78)} {
		if _, err := UInt(s); err == nil {
			t.Fatal(s)
		}
	}
	rows := []Balance{{Address: addr(3), Raw: "9"}, {Address: addr(2), Raw: "100"}, {Address: addr(1), Raw: "20"}}
	if err = Sort(rows); err != nil || rows[0].Raw != "100" || rows[1].Raw != "20" {
		t.Fatal(rows, err)
	}
}
func TestPostgresTransferLedger(t *testing.T) {
	dsn := os.Getenv("L14_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("L14_POSTGRES_DSN required: real private PostgreSQL fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	schema := os.Getenv("L14_POSTGRES_SCHEMA")
	if schema == "" {
		t.Fatal("L14_POSTGRES_SCHEMA required: exact leased fixture namespace")
	}
	ddl, _ := SchemaSQL(schema)
	if _, err = pool.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	r, _ := NewRepository(pool, schema)
	token, a, b := addr(1), addr(2), addr(3)
	if _, err = r.Apply(ctx, event(token, Zero, a, "1", 0)); !errors.Is(err, ErrUnknownToken) {
		t.Fatal(err)
	}
	if err = r.Discover(ctx, token, 6, "10", true); err != nil {
		t.Fatal(err)
	}
	if err = r.Discover(ctx, token, 18, "10", true); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	mint := event(token, Zero, a, "100", 0)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := r.Apply(ctx, mint); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	for _, e := range []Transfer{event(token, a, b, "25", 1), event(token, b, Zero, "5", 2)} {
		if _, err = r.Apply(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	s, err := r.Snapshot(ctx, token, 100)
	if err != nil || s.Supply != "95" || s.Revision != 3 || len(s.Balances) != 2 || s.Balances[0].Raw != "75" || s.Balances[1].Raw != "20" {
		t.Fatal(s, err)
	}
	payload, _ := json.Marshal(s)
	t.Log("persisted response primitive:", string(payload))
	res, err := r.Apply(ctx, mint)
	if err != nil || !res.Duplicate || res.Revision != 1 {
		t.Fatal(res, err)
	}
	conflict := mint
	conflict.Amount = "99"
	if _, err = r.Apply(ctx, conflict); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err = r.Apply(ctx, event(token, a, a, "75", 3)); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Apply(ctx, event(token, a, b, "76", 4)); !errors.Is(err, ErrIntegrity) {
		t.Fatal(err)
	}
	s, err = r.Snapshot(ctx, token, 100)
	if err != nil || s.Supply != "95" || s.Revision != 4 || s.Coverage != "quarantined" || s.Balances[0].Raw != "75" || s.Balances[1].Raw != "20" {
		t.Fatal(s, err)
	}
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM "+schema+".incidents").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	token2 := addr(4)
	if err = r.Discover(ctx, token2, 18, "10", false); err != nil {
		t.Fatal(err)
	}
	for _, e := range []Transfer{event(token2, Zero, b, "10", 0), event(token2, b, addr(5), "7", 1)} {
		if _, err = r.Apply(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	s, err = r.Snapshot(ctx, token2, 100)
	if err != nil || s.Coverage != "incomplete" || s.Supply != "10" || s.Balances[0].Raw != "7" || s.Balances[1].Raw != "3" {
		t.Fatal(s, err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err = r.Apply(canceled, event(token2, b, a, "1", 2)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestMillionHolderStreamingDistribution(t *testing.T) {
	a, err := NewAccumulator("1000000")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000000; i++ {
		if err = a.Add(Balance{Address: addr(i + 1), Raw: "1", Category: "external_wallet"}, false); err != nil {
			t.Fatal(err)
		}
	}
	r, err := a.Finish()
	if err != nil || r.AllPositiveCount != 1000000 || r.Bands[0].Count != 1000000 || r.Bands[0].Raw != "1000000" || *r.Top10.BasisPoints != "0" || len(a.top) != 10 {
		t.Fatal(r, err)
	}
	a, _ = NewAccumulator("100")
	for _, b := range []Balance{{Address: addr(1), Raw: "90", Category: "pool_reserve"}, {Address: addr(2), Raw: "5", Category: "external_wallet"}, {Address: addr(3), Raw: "5", Category: "external_wallet"}} {
		if err = a.Add(b, false); err != nil {
			t.Fatal(err)
		}
	}
	r, err = a.Finish()
	if err != nil || r.AllPositiveCount != 3 || r.ExternalWalletCount != 2 || r.CirculatingRaw != "10" || *r.Top10.BasisPoints != "10000" {
		t.Fatal(r, err)
	}
}

// This bounded genesis replay uses synthetic authenticated-event inputs against
// real PostgreSQL, not an archive provider or receipt qualification.
func TestCheckpointIndependentReplay(t *testing.T) {
	dsn := os.Getenv("L14_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("real PostgreSQL fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	schema := os.Getenv("L14_POSTGRES_SCHEMA")
	if schema == "" {
		t.Fatal("L14_POSTGRES_SCHEMA required: exact leased fixture namespace")
	}
	ddl, _ := SchemaSQL(schema)
	if _, err = pool.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	r, _ := NewRepository(pool, schema)
	token := addr(1)
	if err = r.Discover(ctx, token, 6, "10", true); err != nil {
		t.Fatal(err)
	}
	// Independent bounded integer reference deliberately avoids Mutation and UInt.
	balances := map[string]int64{}
	var supply int64
	events := []Transfer{event(token, Zero, addr(2), "100", 0), event(token, addr(2), addr(3), "25", 1), event(token, addr(3), Zero, "5", 2), event(token, addr(2), addr(4), "9", 3), event(token, addr(4), addr(4), "9", 4), event(token, addr(4), addr(3), "4", 5)}
	for _, e := range events {
		q, err := strconv.ParseInt(e.Amount, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		if e.From == Zero {
			supply += q
		} else {
			balances[e.From] -= q
		}
		if e.To == Zero {
			supply -= q
		} else {
			balances[e.To] += q
		}
		if _, err = r.Apply(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := r.Snapshot(ctx, token, 100)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Supply != strconv.FormatInt(supply, 10) || snapshot.Revision != int64(len(events)) {
		t.Fatal(snapshot)
	}
	actual := append([]Balance(nil), snapshot.Balances...)
	sort.Slice(actual, func(i, j int) bool { return actual[i].Address < actual[j].Address })
	// Independent digest encoding and hashing validates CheckpointDigest framing.
	expected := "holdings_v1\n125\n" + token + "\n" + strconv.FormatInt(supply, 10) + "\n10\n"
	addresses := make([]string, 0, len(balances))
	for a := range balances {
		addresses = append(addresses, a)
	}
	sort.Strings(addresses)
	for _, a := range addresses {
		if balances[a] > 0 {
			expected += a + ":" + strconv.FormatInt(balances[a], 10) + "\n"
		}
	}
	digest, _ := NewCheckpointDigest(token, snapshot.Supply, snapshot.Position.Height)
	if len(actual) != len(addresses) {
		t.Fatal(actual, balances)
	}
	for _, b := range actual {
		if b.Raw != strconv.FormatInt(balances[b.Address], 10) {
			t.Fatal(b, balances)
		}
		if err = digest.Add(b.Address, b.Raw); err != nil {
			t.Fatal(err)
		}
	}
	sum := sha256.Sum256([]byte(expected))
	if digest.Hex() != hex.EncodeToString(sum[:]) {
		t.Fatal(digest.Hex(), expected)
	}
	if err = digest.Add(actual[len(actual)-1].Address, actual[len(actual)-1].Raw); err == nil {
		t.Fatal("duplicate checkpoint row accepted")
	}
}
func TestSnapshotRevisionPagination(t *testing.T) {
	now := time.Unix(100, 0)
	s := Snapshot{Token: addr(1), Revision: 12, Balances: []Balance{{Address: addr(2), Raw: "100"}, {Address: addr(3), Raw: "20"}, {Address: addr(4), Raw: "9"}}}
	f, err := Freeze(s, now)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.Page("", 1, now)
	if err != nil || p.Rows[0].Raw != "100" {
		t.Fatal(p, err)
	}
	s.Revision = 13
	s.Balances[1].Raw = "200"
	newer, _ := Freeze(s, now)
	p2, err := f.Page(p.Next, 1, now)
	if err != nil || p2.Revision != 12 || p2.Rows[0].Raw != "20" {
		t.Fatal(p2, err)
	}
	if _, err = newer.Page(p.Next, 1, now); !errors.Is(err, ErrRestartRequired) {
		t.Fatal(err)
	}
	if _, err = f.Page("", 1, now.Add(15*time.Minute)); !errors.Is(err, ErrRestartRequired) {
		t.Fatal(err)
	}
}

func TestReviewValidationBounds(t *testing.T) {
	if r, err := NewRepository(nil, "valid_schema"); err == nil || r != nil {
		t.Fatal("nil pool accepted")
	}
	for _, tc := range []struct{ balance, supply, reason string }{{"101", "100", "balance_exceeds_supply"}, {"1", "0", "zero_supply"}, {"-1", "1", "invalid_balance"}, {"1", "bad", "unknown_supply"}, {new(big.Int).Add(maxUint, big.NewInt(1)).String(), maxUint.String(), "invalid_balance"}} {
		r := BasisPoints(tc.balance, tc.supply)
		if r.BasisPoints != nil || r.Reason != tc.reason {
			t.Fatal(tc, r)
		}
	}
	for _, balance := range []string{"0", "1", maxUint.String()} {
		r := BasisPoints(balance, maxUint.String())
		if r.BasisPoints == nil {
			t.Fatal(r)
		}
		n, err := strconv.Atoi(*r.BasisPoints)
		if err != nil || n < 0 || n > 10000 {
			t.Fatal(r)
		}
	}
	r := BasisPoints(maxUint.String(), maxUint.String())
	if *r.BasisPoints != "10000" {
		t.Fatal(r)
	}
	a, _ := NewAccumulator("10")
	if err := a.Add(Balance{Address: addr(2), Raw: "10", Category: "external_wallet"}, true); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(a.result)
	total, creator, last := a.total.String(), a.creator.String(), a.lastAddress
	for _, b := range []Balance{{Address: "", Raw: "1", Category: "unknown"}, {Address: Zero, Raw: "1", Category: "unknown"}, {Address: addr(2), Raw: "1", Category: "unknown"}, {Address: addr(1), Raw: "1", Category: "unknown"}, {Address: addr(3), Raw: "0", Category: "unknown"}, {Address: addr(3), Raw: "1", Category: "bad"}} {
		if err := a.Add(b, true); err == nil {
			t.Fatal("invalid row accepted", b)
		}
		after, _ := json.Marshal(a.result)
		if string(after) != string(before) || a.total.String() != total || a.creator.String() != creator || a.lastAddress != last || len(a.top) != 1 || a.top[0].String() != "10" {
			t.Fatal("rejected row mutated accumulator", b)
		}
	}
	normalized, _ := NewAccumulator("1")
	if err := normalized.Add(Balance{Address: "0x" + strings.ToUpper(addr(171)[2:]), Raw: "1", Category: "unknown"}, false); err != nil {
		t.Fatal(err)
	}
	if normalized.lastAddress != addr(171) {
		t.Fatal(normalized.lastAddress)
	}
}

func TestHostileTransferAndCursorEncoding(t *testing.T) {
	base := event(addr(1), Zero, addr(2), "10", 0)
	for _, value := range []string{"", "0x1234", fmt.Sprintf("0x%064x", 0)} {
		for _, field := range []string{"transaction", "block"} {
			e := base
			if field == "transaction" {
				e.TransactionHash = value
			} else {
				e.BlockHash = value
			}
			if _, err := e.Normalize(); err == nil {
				t.Fatal("absent provenance accepted", field, value)
			}
		}
	}
	normalized := base
	normalized.Token = "0x" + strings.ToUpper(addr(171)[2:])
	normalized, err := normalized.Normalize()
	if err != nil || normalized.Token != addr(171) {
		t.Fatal(normalized, err)
	}
	for _, address := range []string{"", "x", "0x00", "0x" + strings.Repeat("z", 40)} {
		if cursor := encodeSnapshotCursor("11111111-1111-4111-8111-111111111111", "1", address); cursor != "" {
			t.Fatal("invalid cursor address encoded", address)
		}
	}
}
