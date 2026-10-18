package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const fixtureDSN = "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"
const fixtureSchema = "kd_0a19ad96c21f31f2e55be197"
const fixtureAddress = "0x1111111111111111111111111111111111111111"

func deterministicFixture(t *testing.T) (*DeterministicReader, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	p, e := pgxpool.New(ctx, fixtureDSN)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Close)
	if _, e = p.Exec(ctx, `CREATE SCHEMA `+fixtureSchema); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := p.Exec(context.Background(), `DROP SCHEMA `+fixtureSchema+` CASCADE`); e != nil {
			t.Error(e)
		}
	})
	ddl := `CREATE TABLE ` + fixtureSchema + `.market_snapshots(chain_id text,pool_address text,revision numeric(78,0),applied_event numeric(78,0),chain_height numeric(78,0),block_hash text,observed_at timestamptz,base jsonb,PRIMARY KEY(chain_id,pool_address,revision));CREATE TABLE ` + fixtureSchema + `.market_trades(chain_id text,pool_address text,block_height numeric(78,0),transaction_index numeric(78,0),log_index numeric(78,0),event_id text,applied_event numeric(78,0),payload jsonb,PRIMARY KEY(chain_id,pool_address,event_id));CREATE INDEX market_trades_order ON ` + fixtureSchema + `.market_trades(chain_id,pool_address,block_height DESC,transaction_index DESC,log_index DESC,event_id DESC) INCLUDE(applied_event);`
	if _, e = p.Exec(ctx, ddl); e != nil {
		t.Fatal(e)
	}
	keys, e := NewCursorKeys("test", map[string][]byte{"test": []byte(strings.Repeat("a", 32))})
	if e != nil {
		t.Fatal(e)
	}
	r, e := NewDeterministicReader(p, keys, fixtureSchema)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `INSERT INTO `+fixtureSchema+`.market_snapshots VALUES('125',$1,1,150,3,$2,now(),'{"reserveRaw":"150"}')`, fixtureAddress, "0x"+strings.Repeat("a", 64)); e != nil {
		t.Fatal(e)
	}
	return r, p
}
func TestL22Precision(t *testing.T) {
	max := "115792089237316195423570985008687907853269984665640564039457584007913129639935"
	for _, s := range []string{max, "1000000", "0"} {
		v, e := ParseUInt256(s)
		if e != nil {
			t.Fatal(e)
		}
		b, e := json.Marshal(v)
		if e != nil || string(b) != `"`+s+`"` {
			t.Fatalf("round trip %s %v", b, e)
		}
	}
	for _, s := range []string{"-1", "+1", "1e6", "01", strings.Repeat("9", 78), ""} {
		if _, e := ParseUInt256(s); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	if _, e := Address("0x" + strings.Repeat("g", 40)); e == nil {
		t.Fatal("hex accepted")
	}
	fee, e := PoolCreationFee(fixtureAddress)
	if e != nil || fee.Raw != "1000000" || fee.Decimals != 6 || fee.ChainID != "125" {
		t.Fatal(fee, e)
	}
}
func TestL22RealPostgresPaginationSnapshotDeadline(t *testing.T) {
	r, p := deterministicFixture(t)
	ctx := context.Background()
	for i := 1; i <= 150; i++ {
		_, e := p.Exec(ctx, `INSERT INTO `+fixtureSchema+`.market_trades VALUES('125',$1,$2,$3,0,$4,$5,$6::jsonb)`, fixtureAddress, (i-1)/50+1, (i-1)%50, fmt.Sprintf("event-%03d", i), i, fmt.Sprintf(`{"timestamp":123,"raw":"%d"}`, i))
		if e != nil {
			t.Fatal(e)
		}
	}
	seen := map[string]bool{}
	token := ""
	last := 151
	for page := 0; page < 3; page++ {
		got, e := r.Trades(ctx, fixtureAddress, "", token, 50)
		if e != nil {
			t.Fatal(e)
		}
		if len(got.Trades) != 50 {
			t.Fatal(len(got.Trades))
		}
		for _, trade := range got.Trades {
			var n int
			fmt.Sscanf(trade.Order.EventID, "event-%d", &n)
			if n >= last || seen[trade.Order.EventID] {
				t.Fatal("order/duplicate", "page", page, "n", n, "last", last, "order", trade.Order)
			}
			seen[trade.Order.EventID] = true
			last = n
		}
		if page == 0 {
			token = *got.NextCursor
			_, e = p.Exec(ctx, `INSERT INTO `+fixtureSchema+`.market_trades VALUES('125',$1,3,49,1,'new-in-flight',151,'{}')`, fixtureAddress)
			if e == nil {
				_, e = p.Exec(ctx, `INSERT INTO `+fixtureSchema+`.market_snapshots SELECT chain_id,pool_address,2,151,chain_height,block_hash,now(),'{"reserveRaw":"151"}' FROM `+fixtureSchema+`.market_snapshots WHERE revision=1`)
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e = r.Trades(ctx, "0x2222222222222222222222222222222222222222", "", token, 50); !errors.Is(e, ErrCursorScope) {
				t.Fatal(e)
			}
			if _, e = r.Trades(ctx, fixtureAddress, "", token+"x", 50); !errors.Is(e, ErrCursorInvalid) {
				t.Fatal(e)
			}
		} else if got.NextCursor != nil {
			token = *got.NextCursor
		}
		if got.Watermark.AppliedEvent != "150" || string(got.Base) != `{"reserveRaw": "150"}` {
			t.Fatalf("watermark/base mismatch %+v %s", got.Watermark, got.Base)
		}
		if page == 2 && got.HasMore {
			t.Fatal("extra page")
		}
	}
	if len(seen) != 150 {
		t.Fatal(len(seen))
	}
	// Pause after the snapshot SELECT has established repeatable-read visibility.
	e := r.read(ctx, fixtureAddress, "0", func(ctx context.Context, tx pgx.Tx, s SnapshotRead) error {
		if _, e := p.Exec(ctx, `INSERT INTO `+fixtureSchema+`.market_snapshots SELECT chain_id,pool_address,3,152,chain_height,block_hash,now(),'{"reserveRaw":"152"}' FROM `+fixtureSchema+`.market_snapshots WHERE revision=1`); e != nil {
			return e
		}
		var revision string
		if e := tx.QueryRow(ctx, `SELECT max(revision)::text FROM `+fixtureSchema+`.market_snapshots`).Scan(&revision); e != nil {
			return e
		}
		if revision != string(s.Watermark.ProjectionRevision) {
			t.Fatalf("nonrepeatable snapshot %s %+v", revision, s)
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	start := time.Now()
	e = r.read(ctx, fixtureAddress, "0", func(ctx context.Context, tx pgx.Tx, _ SnapshotRead) error {
		_, e := tx.Exec(ctx, "SELECT pg_sleep(0.5)")
		return e
	})
	if e == nil || time.Since(start) > 450*time.Millisecond {
		t.Fatal("deadline not enforced", e, time.Since(start))
	}
	t.Log("150 unique same-second canonical rows; concurrent insert excluded; repeatable-read watermark coherent; statement timeout enforced")
	// This proves index eligibility on a small fixture, not the 10M-row SLO.
	tx, e := p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	_, e = tx.Exec(ctx, "SET LOCAL enable_seqscan=off")
	if e != nil {
		t.Fatal(e)
	}
	rows, e := tx.Query(ctx, `EXPLAIN (ANALYZE,BUFFERS) SELECT event_id FROM `+fixtureSchema+`.market_trades WHERE chain_id='125' AND pool_address=$1 ORDER BY block_height DESC,transaction_index DESC,log_index DESC,event_id DESC LIMIT 51`, fixtureAddress)
	if e != nil {
		t.Fatal(e)
	}
	plan := ""
	for rows.Next() {
		var line string
		rows.Scan(&line)
		plan += line + "\n"
	}
	rows.Close()
	if !strings.Contains(plan, "market_trades_order") {
		t.Fatal(plan)
	}
	t.Log(plan)
}
func TestL22CursorRotationExpiry(t *testing.T) {
	old, _ := NewCursorKeys("old", map[string][]byte{"old": []byte(strings.Repeat("a", 32))})
	now := time.Now()
	p := cursorPayload{Version: 1, Chain: "125", Pool: fixtureAddress, FilterDigest: FilterDigest(""), Size: 50, Issued: now.Unix(), Expiry: now.Unix() + 900, Order: ChainOrder{Height: "1", TransactionIndex: "0", LogIndex: "0", EventID: "event"}, Watermark: ReadWatermark{ChainID: "125", Height: "1", ProjectionRevision: "1", AppliedEvent: "1"}}
	token, e := old.seal(p)
	if e != nil {
		t.Fatal(e)
	}
	rotated, _ := NewCursorKeys("new", map[string][]byte{"old": []byte(strings.Repeat("a", 32)), "new": []byte(strings.Repeat("b", 32))})
	if _, e = rotated.open(token, fixtureAddress, p.FilterDigest, 50, now); e != nil {
		t.Fatal(e)
	}
	if _, e = rotated.open(token, fixtureAddress, p.FilterDigest, 50, now.Add(15*time.Minute)); !errors.Is(e, ErrCursorExpired) {
		t.Fatal(e)
	}
	if _, e = rotated.open(token, fixtureAddress, p.FilterDigest, 100, now); !errors.Is(e, ErrCursorScope) {
		t.Fatal(e)
	}
}

func TestL22RealPostgresRejectsCorruptWatermark(t *testing.T) {
	r, p := deterministicFixture(t)
	ctx := context.Background()
	for _, hash := range []string{"", "0x" + strings.Repeat("g", 64), "0x" + strings.Repeat("a", 63)} {
		if _, e := p.Exec(ctx, `UPDATE `+fixtureSchema+`.market_snapshots SET block_hash=$1`, hash); e != nil {
			t.Fatal(e)
		}
		if _, e := r.Snapshot(ctx, fixtureAddress); !errors.Is(e, ErrDataIntegrity) {
			t.Fatalf("hash=%q err=%v", hash, e)
		}
	}
	for _, timestamp := range []any{nil, time.Time{}} {
		if _, e := p.Exec(ctx, `UPDATE `+fixtureSchema+`.market_snapshots SET block_hash=$1,observed_at=$2`, "0x"+strings.Repeat("a", 64), timestamp); e != nil {
			t.Fatal(e)
		}
		if _, e := r.Snapshot(ctx, fixtureAddress); !errors.Is(e, ErrDataIntegrity) {
			t.Fatalf("timestamp=%v err=%v", timestamp, e)
		}
	}
	if _, e := p.Exec(ctx, `UPDATE `+fixtureSchema+`.market_snapshots SET block_hash=NULL,observed_at=now()`); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Snapshot(ctx, fixtureAddress); !errors.Is(e, ErrDataIntegrity) {
		t.Fatal(e)
	}
}

func TestProductionContinuationWithoutRetainedSnapshotRequiresRestart(t *testing.T) {
	r, p := deterministicFixture(t)
	ctx := context.Background()
	for i := 1; i <= 2; i++ {
		if _, err := p.Exec(ctx, `INSERT INTO `+fixtureSchema+`.market_trades VALUES('125',$1,1,$2,0,$3,$2,'{}')`, fixtureAddress, i, fmt.Sprintf("retained-event-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	page, err := r.Trades(ctx, fixtureAddress, "", "", 1)
	if err != nil || page.NextCursor == nil {
		t.Fatal(page, err)
	}
	if _, err = p.Exec(ctx, `DELETE FROM `+fixtureSchema+`.market_snapshots`); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Trades(ctx, fixtureAddress, "", *page.NextCursor, 1); !errors.Is(err, ErrCursorExpired) {
		t.Fatal("missing retained snapshot reported as pool absence", err)
	}
}
