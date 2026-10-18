package l16

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"os"
	"strings"
	"testing"
	"time"
)

func str(s string) *string { return &s }
func TestConservationAndUnknown(t *testing.T) {
	p, e := Acquire(Empty(), "10", str("100"))
	if e != nil {
		t.Fatal(e)
	}
	p, e = Acquire(p, "10", nil)
	if e != nil {
		t.Fatal(e)
	}
	p, d, e := Dispose(p, "10", str("150"))
	if e != nil {
		t.Fatal(e)
	}
	if p.Known != "5" || p.Unknown != "5" || p.Cost != "50" || p.Realized != "25" || d.UnknownProceeds != "75" || p.CompleteRealized() != nil {
		t.Fatalf("%+v %+v", p, d)
	}
	s, dst, leg, e := Transfer(p, Empty(), "10", true, "fresh-proofs+consent")
	if e != nil || s.Cost != "0" || dst.Cost != "50" || dst.Unknown != "5" || s.Realized != "25" || leg.Known != "5" {
		t.Fatalf("%+v %+v %v", s, dst, e)
	}
	p = Inventory{Known: "2", Unknown: "1", Cost: "5", Realized: "0", UnknownProceeds: "0", Version: 2}
	p, d, e = Dispose(p, "1", str("4"))
	if e != nil || d.Known != "0" || d.UnknownProceeds != "4" {
		t.Fatal(p, d, e)
	}
	p, d, e = Dispose(p, "2", str("7"))
	if e != nil || d.Cost != "5" || p.Realized != "2" || p.Cost != "0" || p.CompleteRealized() != nil {
		t.Fatal(p, d, e)
	}
}
func TestNormalizationValidationAndMark(t *testing.T) {
	for _, s := range []string{"-1", "01", "+1", "1e2", "NaN", strings.Repeat("9", 79)} {
		if _, e := Uint256(s); e == nil {
			t.Fatal(s)
		}
	}
	for _, d := range []uint8{6, 18} {
		q := "1" + strings.Repeat("0", int(d))
		p, e := Acquire(Empty(), q, str("2000000"))
		if e != nil {
			t.Fatal(e)
		}
		a, e := Average(p, &d)
		if e != nil || a.RatString() != "2" {
			t.Fatal(a, e)
		}
		if v, _ := Average(p, nil); v != nil {
			t.Fatal(v)
		}
		if v, _ := Unrealized(p, nil, true); v != nil {
			t.Fatal(v)
		}
		v, e := Unrealized(p, str("0"), true)
		if e != nil || *v != "-2000000" {
			t.Fatal(v, e)
		}
	}
}
func testLeg(log uint32) Leg {
	return Leg{Identity: Identity{"125", "0x" + strings.Repeat("1", 40), "0x" + strings.Repeat("a", 64), log}, Height: 100, Wallet: "0x" + strings.Repeat("2", 40), Token: "0x" + strings.Repeat("3", 40), Payer: "0x" + strings.Repeat("4", 40), Recipient: "0x" + strings.Repeat("2", 40), Kind: "buy", Quantity: "10", ActualUSDL: str("100"), SettlementReference: "synthetic-domain-fixture-not-receipt"}
}
func TestAttribution(t *testing.T) {
	l := testLeg(1)
	p, e := Apply(Empty(), l)
	if e != nil || p.Known != "10" {
		t.Fatal(p, e)
	}
	l.Wallet = l.Payer
	if _, e = Apply(Empty(), l); e == nil {
		t.Fatal("credited payer rather than beneficiary")
	}
}
func TestPostgresAtomicInboxOutbox(t *testing.T) {
	dsn := os.Getenv("L16_TEST_DSN")
	if dsn == "" {
		t.Fatal("L16_TEST_DSN required for real PostgreSQL qualification")
	}
	ctx := context.Background()
	c, e := pgx.Connect(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close(ctx)
	schema := fmt.Sprintf("kd_0662f16e2ad90a061315a3ea_%d", time.Now().UnixNano())
	if _, e = c.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer func() {
		_, err := c.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		if err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()
	if _, e = c.Exec(ctx, "SET search_path TO "+schema); e != nil {
		t.Fatal(e)
	}
	if _, e = c.Exec(ctx, DDL); e != nil {
		t.Fatal(e)
	}
	l := testLeg(1)
	for i := 0; i < 100; i++ {
		tx, e := c.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		p, inserted, e := ApplyTx(ctx, tx, l)
		if e != nil {
			tx.Rollback(ctx)
			t.Fatal(e)
		}
		if inserted != (i == 0) || p.Cost != "100" {
			t.Fatal(p, inserted)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
	}
	next := testLeg(2)
	next.Predecessor = "missing"
	tx, _ := c.Begin(ctx)
	if _, _, e = ApplyTx(ctx, tx, next); e == nil {
		t.Fatal("gap accepted")
	}
	tx.Rollback(ctx)
	id, _ := l.Identity.Key()
	next.Predecessor = id
	tx, _ = c.Begin(ctx)
	if _, _, e = ApplyTx(ctx, tx, next); e != nil {
		t.Fatal(e)
	}
	tx.Rollback(ctx)
	next.Kind = "sell"
	next.Payer = next.Wallet
	next.ActualUSDL = str("100")
	tx, _ = c.Begin(ctx)
	if _, _, e = ApplyTx(ctx, tx, next); e != nil {
		tx.Rollback(ctx)
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	rebuy := testLeg(3)
	rebuy.ActualUSDL = str("200")
	rebuy.Predecessor, _ = next.Identity.Key()
	tx, _ = c.Begin(ctx)
	if _, _, e = ApplyTx(ctx, tx, rebuy); e != nil {
		tx.Rollback(ctx)
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	var inbox, outbox int
	if e = c.QueryRow(ctx, "SELECT count(*) FROM inventory_l16_inbox").Scan(&inbox); e != nil {
		t.Fatal(e)
	}
	c.QueryRow(ctx, "SELECT count(*) FROM inventory_l16_outbox").Scan(&outbox)
	if inbox != 3 || outbox != 3 {
		t.Fatal(inbox, outbox)
	}
	var raw []byte
	if e = c.QueryRow(ctx, "SELECT payload FROM inventory_l16_outbox WHERE identity=$1", func() string { k, _ := rebuy.Identity.Key(); return k }()).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	var p Inventory
	if e = json.Unmarshal(raw, &p); e != nil || p.Cost != "200" || p.Realized != "0" || p.Known != "10" {
		t.Fatal(string(raw), e)
	}
	t.Logf("persisted/outbox=%s inbox=%d outbox=%d", raw, inbox, outbox)
	// One external transfer has two legitimate wallet effects with one canonical
	// event identity. Both projections commit in the same database transaction.
	outgoing := testLeg(4)
	outgoing.Kind = "outgoing"
	outgoing.ActualUSDL = nil
	outgoing.Quantity = "5"
	outgoing.Predecessor, _ = rebuy.Identity.Key()
	incoming := outgoing
	incoming.Kind = "incoming"
	incoming.Wallet = "0x" + strings.Repeat("5", 40)
	incoming.Predecessor = ""
	tx, _ = c.Begin(ctx)
	source, _, err := ApplyTx(ctx, tx, outgoing)
	if err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	destination, _, err := ApplyTx(ctx, tx, incoming)
	if err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if source.Known != "5" || source.Cost != "100" || destination.Unknown != "5" || destination.Known != "0" {
		tx.Rollback(ctx)
		t.Fatal(source, destination)
	}
	tx.Rollback(ctx)
	if err = c.QueryRow(ctx, "SELECT count(*) FROM inventory_l16_inbox").Scan(&inbox); err != nil || inbox != 3 {
		t.Fatal(inbox, err)
	}
	tx, _ = c.Begin(ctx)
	if _, _, err = ApplyTx(ctx, tx, outgoing); err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if _, _, err = ApplyTx(ctx, tx, incoming); err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	tx, _ = c.Begin(ctx)
	if _, inserted, err := ApplyTx(ctx, tx, incoming); err != nil || inserted {
		tx.Rollback(ctx)
		t.Fatal(inserted, err)
	}
	tx.Rollback(ctx)
	if err = c.QueryRow(ctx, "SELECT count(*) FROM inventory_l16_inbox").Scan(&inbox); err != nil || inbox != 5 {
		t.Fatal(inbox, err)
	}
	if err = c.QueryRow(ctx, "SELECT count(*) FROM inventory_l16_outbox").Scan(&outbox); err != nil || outbox != 5 {
		t.Fatal(outbox, err)
	}
	t.Logf("same-event two-wallet atomic transfer: source=%+v destination=%+v inbox=%d outbox=%d", source, destination, inbox, outbox)
	conflict := l
	conflict.Quantity = "11"
	tx, _ = c.Begin(ctx)
	if _, _, e = ApplyTx(ctx, tx, conflict); e == nil {
		t.Fatal("conflicting identity accepted")
	}
	tx.Rollback(ctx)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	tx, _ = c.Begin(ctx)
	if _, _, e = ApplyTx(canceled, tx, next); e == nil {
		t.Fatal("cancellation ignored")
	}
	tx.Rollback(ctx)
}
func TestCheckpointCanonicalDayBoundary(t *testing.T) {
	day := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	a, b := testLeg(1), testLeg(2)
	b.Kind = "sell"
	b.Payer = b.Wallet
	b.ActualUSDL = str("100")
	b.Predecessor, _ = a.Identity.Key()
	re := testLeg(3)
	re.Predecessor, _ = b.Identity.Key()
	re.ActualUSDL = str("200")
	legs := []TimedLeg{{re, day.Add(3 * time.Hour)}, {b, day.Add(2 * time.Hour)}, {a, day.Add(time.Hour)}, {testLeg(4), day.Add(24 * time.Hour)}}
	cp, e := RebuildDay(context.Background(), Empty(), legs, day, true)
	if e != nil || cp.State.Cost != "200" || cp.State.Known != "10" || cp.State.Realized != "0" {
		t.Fatal(cp, e)
	}
	cp2, e := RebuildDay(context.Background(), Empty(), []TimedLeg{legs[2], legs[1], legs[0]}, day, true)
	if e != nil || cp2.InputDigest != cp.InputDigest {
		t.Fatal(cp2, e)
	}
	t.Logf("canonical checkpoint=%+v", cp)
}

func TestTransferUnlinkedAndFeeSettlement(t *testing.T) {
	p, _ := Acquire(Empty(), "10", str("100"))
	p, _ = Acquire(p, "10", nil)
	source, dest, d, e := Transfer(p, Empty(), "10", false, "")
	if e != nil || source.Cost != "50" || dest.Unknown != "10" || dest.Known != "0" || d.Cost != "50" || source.Realized != "0" {
		t.Fatal(source, dest, d, e)
	}
	if _, _, _, e = Transfer(p, Empty(), "10", true, ""); e == nil {
		t.Fatal("proofless carry")
	}
	p, _ = Acquire(Empty(), "100", str("80"))
	p, d, e = Dispose(p, "100", str("90"))
	if e != nil || p.Realized != "10" || p.Cost != "0" || d.Cost != "80" {
		t.Fatal(p, d, e)
	}
	if _, _, e = Dispose(p, "1", str("1")); e == nil {
		t.Fatal("oversell")
	}
	p, _ = Acquire(Empty(), "5", nil)
	p, _, e = Dispose(p, "5", str("20"))
	if e != nil || p.Realized != "0" || p.UnknownProceeds != "20" || p.CompleteRealized() != nil {
		t.Fatal(p, e)
	}
}

func TestRejectUnsupportedAndInconsistentState(t *testing.T) {
	for _, version := range []uint64{0, 1, 3} {
		p := Empty()
		p.Version = version
		if _, e := Acquire(p, "1", nil); e == nil {
			t.Fatalf("accepted version %d", version)
		}
		if p.CompleteRealized() != nil {
			t.Fatal("invalid state reported complete")
		}
	}
	p := Empty()
	p.UnknownProceeds = "1"
	if _, _, e := Dispose(p, "1", nil); e == nil {
		t.Fatal("inconsistent unknown proceeds accepted")
	}
	if p.CompleteRealized() != nil {
		t.Fatal("unknown proceeds reported complete")
	}
	p.IncompleteRealized = true
	if _, e := Acquire(p, "1", nil); e != nil {
		t.Fatal(e)
	}
}

func TestCheckpointRejectsMixedAndGapped(t *testing.T) {
	day := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	a, b := testLeg(1), testLeg(2)
	b.Predecessor, _ = a.Identity.Key()
	for _, mutate := range []func(*Leg){func(l *Leg) { l.Wallet = "0x" + strings.Repeat("5", 40); l.Recipient = l.Wallet }, func(l *Leg) { l.Token = "0x" + strings.Repeat("6", 40) }, func(l *Leg) { l.Predecessor = "missing" }, func(l *Leg) { l.Identity.Log = a.Identity.Log; l.Identity.Transaction = "0x" + strings.Repeat("b", 64) }} {
		x := b
		mutate(&x)
		if _, e := RebuildDay(context.Background(), Empty(), []TimedLeg{{a, day}, {x, day}}, day, true); e == nil {
			t.Fatal("invalid checkpoint accepted", x)
		}
	}
}

func TestCheckpointIncrementalContinuation(t *testing.T) {
	day := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	first := testLeg(1)
	prior, err := RebuildDay(context.Background(), Empty(), []TimedLeg{{first, day}}, day, true)
	if err != nil {
		t.Fatal(err)
	}
	sell := testLeg(2)
	sell.Kind, sell.Payer, sell.ActualUSDL = "sell", sell.Wallet, str("150")
	sell.Quantity = "5"
	sell.Predecessor, _ = first.Identity.Key()
	next, err := ContinueDay(context.Background(), prior, []TimedLeg{{sell, day.AddDate(0, 0, 1)}}, day.AddDate(0, 0, 1))
	if err != nil || next.State.Known != "5" || next.State.Cost != "50" || next.State.Realized != "100" || next.ParentDigest != prior.InputDigest || !next.CompleteHistory {
		t.Fatal(next, err)
	}
	empty, err := ContinueDay(context.Background(), next, nil, day.AddDate(0, 0, 2))
	if err != nil || empty.State != next.State || empty.LastIdentity != next.LastIdentity || empty.ParentDigest != next.InputDigest {
		t.Fatal(empty, err)
	}
	incomplete := prior
	incomplete.CompleteHistory = false
	partial, err := ContinueDay(context.Background(), incomplete, []TimedLeg{{sell, day.AddDate(0, 0, 1)}}, day.AddDate(0, 0, 1))
	if err != nil || partial.CompleteHistory || partial.InputDigest == next.InputDigest {
		t.Fatal(partial, err)
	}
	for _, mutate := range []func(*Checkpoint){func(cp *Checkpoint) { cp.InputDigest = "bad" }, func(cp *Checkpoint) { cp.Algorithm = "other" }, func(cp *Checkpoint) { cp.Day = "2026-10-08" }, func(cp *Checkpoint) { cp.Partition = "125:bad:bad" }, func(cp *Checkpoint) { cp.LastLogIndex = 2 }} {
		invalid := prior
		mutate(&invalid)
		if _, err := ContinueDay(context.Background(), invalid, []TimedLeg{{sell, day.AddDate(0, 0, 1)}}, day.AddDate(0, 0, 1)); err == nil {
			t.Fatal("invalid continuation accepted", invalid)
		}
	}
}

func TestPostgresLinkedTransferAtomicCarry(t *testing.T) {
	dsn := os.Getenv("L16_TEST_DSN")
	if dsn == "" {
		t.Fatal("L16_TEST_DSN required for real PostgreSQL qualification")
	}
	ctx := context.Background()
	c, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)
	schema := fmt.Sprintf("l16_linked_%d", time.Now().UnixNano())
	if _, err = c.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := c.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	if _, err = c.Exec(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Exec(ctx, DDL); err != nil {
		t.Fatal(err)
	}
	buy := testLeg(1)
	unknown := testLeg(2)
	unknown.Kind, unknown.ActualUSDL = "incoming", nil
	unknown.Predecessor, _ = buy.Identity.Key()
	tx, err := c.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, leg := range []Leg{buy, unknown} {
		if _, _, err = ApplyTx(ctx, tx, leg); err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	outgoing := testLeg(3)
	outgoing.Kind, outgoing.ActualUSDL = "outgoing", nil
	outgoing.Predecessor, _ = unknown.Identity.Key()
	destination := "0x" + strings.Repeat("5", 40)
	outgoing.Recipient = destination
	request := TransferRequest{Source: outgoing, Destination: destination, Carry: true, ProofReference: "fixture-fresh-proofs-and-consent"}
	tx, err = c.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	source, target, inserted, err := ApplyTransferTx(ctx, tx, request)
	if err != nil || !inserted || source.Known != "5" || source.Unknown != "5" || source.Cost != "50" || target.Known != "5" || target.Unknown != "5" || target.Cost != "50" || source.Realized != "0" || target.Realized != "0" {
		tx.Rollback(ctx)
		t.Fatal(source, target, inserted, err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = c.QueryRow(ctx, "SELECT count(*) FROM inventory_l16_inbox").Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	for i := 0; i < 100; i++ {
		tx, err = c.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		source, target, inserted, err = ApplyTransferTx(ctx, tx, request)
		if err != nil || inserted != (i == 0) || source.Cost != "50" || target.Cost != "50" {
			tx.Rollback(ctx)
			t.Fatal(source, target, inserted, err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for table, want := range map[string]int{"inventory_l16_inbox": 4, "inventory_l16_outbox": 4, "inventory_l16_transfers": 1} {
		if err = c.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != want {
			t.Fatal(table, count, err)
		}
	}
	conflict := request
	conflict.ProofReference = "changed-proof"
	tx, err = c.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = ApplyTransferTx(ctx, tx, conflict); err == nil {
		tx.Rollback(ctx)
		t.Fatal("conflicting proof accepted")
	}
	tx.Rollback(ctx)
	conflict = request
	conflict.Source.Identity.Log = 4
	conflict.Source.Predecessor, _ = outgoing.Identity.Key()
	conflict.DestinationPredecessor = "missing"
	tx, err = c.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = ApplyTransferTx(ctx, tx, conflict); err == nil {
		tx.Rollback(ctx)
		t.Fatal("destination gap accepted")
	}
	tx.Rollback(ctx)
	var raw []byte
	partition := "125:" + outgoing.Wallet + ":" + outgoing.Token
	if err = c.QueryRow(ctx, "SELECT state FROM inventory_l16 WHERE partition=$1", partition).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var persisted Inventory
	if err = json.Unmarshal(raw, &persisted); err != nil || persisted.Cost != "50" {
		t.Fatal(persisted, err)
	}
	t.Logf("linked carry persisted source=%+v destination=%+v inbox=4 outbox=4 transfers=1", source, target)
}

func TestTransferFailurePreservesBothInventories(t *testing.T) {
	source, err := Acquire(Empty(), "10", str("100"))
	if err != nil {
		t.Fatal(err)
	}
	invalid := Empty()
	invalid.Version = 1
	for _, carry := range []bool{false, true} {
		a, b, _, err := Transfer(source, invalid, "5", carry, "verified-proofs-and-consent")
		if err == nil || a != source || b != invalid {
			t.Fatal(a, b, err)
		}
	}
	maximum := "115792089237316195423570985008687907853269984665640564039457584007913129639935"
	target, err := Acquire(Empty(), maximum, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, carry := range []bool{false, true} {
		a, b, _, err := Transfer(source, target, "5", carry, "verified-proofs-and-consent")
		if err == nil || a != source || b != target {
			t.Fatal(a, b, err)
		}
	}
}

func TestEconomicLegRejectsMalformedBoundaryBeforePersistence(t *testing.T) {
	for _, amount := range []string{"", "0", "-1", "01", "1e2", "NaN", strings.Repeat("9", 79)} {
		leg := testLeg(1)
		leg.Quantity = amount
		if err := leg.Validate(); err == nil {
			t.Fatal("invalid quantity accepted", amount)
		}
	}
	for _, amount := range []string{"", "-1", "01", "1e2", strings.Repeat("9", 79)} {
		leg := testLeg(1)
		leg.ActualUSDL = &amount
		if err := leg.Validate(); err == nil {
			t.Fatal("invalid consideration accepted", amount)
		}
	}
	for _, height := range []uint64{0, 1 << 63} {
		leg := testLeg(1)
		leg.Height = height
		if err := leg.Validate(); err == nil {
			t.Fatal("invalid height accepted", height)
		}
	}
}

func TestPostgresRestartReversedDeliveryAndPendingPublication(t *testing.T) {
	dsn := os.Getenv("L16_TEST_DSN")
	if dsn == "" {
		t.Fatal("L16_TEST_DSN required for real PostgreSQL qualification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("l16_restart_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = conn.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_, dropErr := conn.Exec(cleanup, "DROP SCHEMA "+quoted+" CASCADE")
		if dropErr != nil {
			t.Error(dropErr)
		}
		conn.Close(cleanup)
	}()
	if _, err = conn.Exec(ctx, "SET search_path TO "+quoted); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, DDL); err != nil {
		t.Fatal(err)
	}
	buy := testLeg(1)
	sell := testLeg(2)
	sell.Kind, sell.Payer, sell.ActualUSDL = "sell", sell.Wallet, str("150")
	sell.Predecessor, _ = buy.Identity.Key()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = ApplyTx(ctx, tx, sell); err == nil {
		tx.Rollback(ctx)
		t.Fatal("reversed successor was applied")
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = conn.QueryRow(ctx, "SELECT count(*) FROM inventory_l16_inbox").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	tx, err = conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, inserted, err := ApplyTx(ctx, tx, buy); err != nil || !inserted {
		tx.Rollback(ctx)
		t.Fatal(inserted, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = conn.Close(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err = pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, "SET search_path TO "+quoted); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		tx, err = conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		state, inserted, e := ApplyTx(ctx, tx, buy)
		if e != nil || inserted || state.Cost != "100" {
			tx.Rollback(ctx)
			t.Fatal(state, inserted, e)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	tx, err = conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	state, inserted, err := ApplyTx(ctx, tx, sell)
	if err != nil || !inserted || state.Known != "0" || state.Realized != "50" {
		tx.Rollback(ctx)
		t.Fatal(state, inserted, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"inventory_l16_inbox", "inventory_l16_outbox"} {
		if err = conn.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 2 {
			t.Fatal(table, count, err)
		}
	}
	var payload []byte
	identity, _ := sell.Identity.Key()
	if err = conn.QueryRow(ctx, "SELECT payload FROM inventory_l16_outbox WHERE identity=$1", identity).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var persisted Inventory
	if err = json.Unmarshal(payload, &persisted); err != nil || persisted != state {
		t.Fatal(persisted, err)
	}
	t.Logf("restart-before-publication persisted=%s inbox=2 outbox=2", payload)
}
