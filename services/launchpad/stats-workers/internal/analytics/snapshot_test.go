package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSnapshotPersistence(t *testing.T) {
	dsn := os.Getenv("L15_TEST_DSN")
	if dsn == "" {
		t.Fatal("explicit private PostgreSQL fixture required")
	}
	ctx := context.Background()
	p, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	schema := fmt.Sprintf("kd_ce218285d3bff82119d97ee8_s_%x", time.Now().UnixNano())
	if _, e = p.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if _, e := p.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); e != nil {
			t.Error(e)
		}
	}()
	a := ReactionAuthority{p, schema}
	ddl, e := a.SnapshotSQL()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, ddl); e != nil {
		t.Fatal(e)
	}
	pressure, e := CalculatePressure(3, 1, "3000000", "1000000", false)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(pressure)
	s := Snapshot{"0x1111111111111111111111111111111111111111", "pressure-v1", Watermark{"125", "10", "0x" + strings.Repeat("a", 64), "1", "2026-10-09T00:00:00.000Z", "healthy"}, 100, raw}
	persist := func(s Snapshot) error {
		tx, e := p.Begin(ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback(ctx)
		if e = a.PersistSnapshot(ctx, tx, s); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	if e = persist(s); e != nil {
		t.Fatal(e)
	}
	if e = persist(s); e != nil {
		t.Fatal(e)
	}
	var stored []byte
	if e = p.QueryRow(ctx, "SELECT payload FROM "+schema+".analytics_snapshots").Scan(&stored); e != nil {
		t.Fatal(e)
	}
	var actual Snapshot
	if e = json.Unmarshal(stored, &actual); e != nil {
		t.Fatal(e)
	}
	var value Pressure
	if e = json.Unmarshal(actual.Payload, &value); e != nil || value.BuyCountShare == nil || *value.BuyCountShare != 7500 || actual.Watermark.Height != "10" {
		t.Fatal(value, e)
	}
	s.Payload = json.RawMessage(`{"changed":true}`)
	if e = persist(s); e != ErrConflict {
		t.Fatal(e)
	}
	s.Watermark.ProjectionRevision = "2"
	oldHash := s.Watermark.BlockHash
	s.Watermark.BlockHash = "0x" + strings.Repeat("b", 64)
	if e = persist(s); e != ErrConflict {
		t.Fatal("committed hash disagreement accepted", e)
	}
	s.Watermark.BlockHash = oldHash
	if e = persist(s); e != nil {
		t.Fatal(e)
	}
	s.Watermark.ProjectionRevision = "1"
	if e = persist(s); e != ErrConflict {
		t.Fatal(e)
	}
	if !s.Stale(131) || s.Stale(130) {
		t.Fatal("stale boundary")
	}
	tx, e := p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	s.Watermark.ProjectionRevision = "3"
	if e = a.PersistSnapshot(ctx, tx, s); e != nil {
		t.Fatal(e)
	}
	if e = tx.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	var rev string
	if e = p.QueryRow(ctx, "SELECT revision::text FROM "+schema+".analytics_snapshots").Scan(&rev); e != nil || rev != "2" {
		t.Fatal(rev, e)
	}
}
