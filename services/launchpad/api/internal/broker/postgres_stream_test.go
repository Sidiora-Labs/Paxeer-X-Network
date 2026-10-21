package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func postgresStreamStorage(t *testing.T) (context.Context, *PostgresStream) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	dsn := os.Getenv("L11_POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	schema := fmt.Sprintf("stream_%d", time.Now().UnixNano())
	migration, err := os.ReadFile("../../../../../launchpad/spec/production-system/integration/migrations/150002.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, strings.ReplaceAll(string(migration), "api_stream", schema)); err != nil {
		t.Fatalf("real PostgreSQL migration required: %v", err)
	}
	t.Cleanup(func() {
		c, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if _, err := pool.Exec(c, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	return ctx, &PostgresStream{pool: pool, schema: pgx.Identifier{schema}.Sanitize(), key: bytes.Repeat([]byte{7}, 32)}
}
func notificationStreamFrame(t *testing.T, sequence string) Frame {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	at := "2026-10-12T00:00:00.000Z"
	wallet := "0x" + strings.Repeat("1", 40)
	payload, err := json.Marshal(map[string]any{"notificationId": id.String(), "wallet": wallet, "kind": "trade_fill", "entityId": "source-fill", "createdAt": at, "readAt": nil, "deliveryState": "pending"})
	if err != nil {
		t.Fatal(err)
	}
	return Frame{EventID: id.String(), Topic: "private.notifications." + wallet, Sequence: sequence, Revision: sequence, OccurredAt: at, Watermark: json.RawMessage("null"), Payload: payload}
}
func commitStreamFrame(t *testing.T, ctx context.Context, s *PostgresStream, f Frame) {
	t.Helper()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer streamRollback(tx)
	if err = s.AppendProjectionTx(ctx, tx, f); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestPostgresStreamImmutableRollbackAndBarrier(t *testing.T) {
	ctx, s := postgresStreamStorage(t)
	first := notificationStreamFrame(t, "1")
	commitStreamFrame(t, ctx, s, first)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer streamRollback(tx)
	initial, err := s.projections(ctx, tx, []string{first.Topic})
	if err != nil || len(initial) != 1 || initial[0].sequence != "1" {
		t.Fatal(initial, err)
	}
	second := notificationStreamFrame(t, "2")
	commitStreamFrame(t, ctx, s, second)
	captured, err := s.projections(ctx, tx, []string{first.Topic})
	if err != nil || captured[0].sequence != "1" {
		t.Fatal("snapshot barrier moved", captured, err)
	}
	var count int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM "+s.table("stream_delivery_outbox")+" WHERE partition_id=$1 AND sequence<=1", initial[0].partition).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, "UPDATE "+s.table("stream_delivery_outbox")+" SET frame=frame WHERE sequence=1"); err == nil {
		t.Fatal("immutable envelope update accepted")
	}
	rollback, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	third := notificationStreamFrame(t, "3")
	if err = s.AppendProjectionTx(ctx, rollback, third); err != nil {
		t.Fatal(err)
	}
	if err = rollback.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM "+s.table("stream_delivery_outbox")).Scan(&count); err != nil || count != 2 {
		t.Fatal("uncommitted envelope escaped", count, err)
	}
	duplicate, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer streamRollback(duplicate)
	if err = s.AppendProjectionTx(ctx, duplicate, second); err != nil {
		t.Fatal("exact redelivery rejected", err)
	}
	conflict := second
	conflict.Payload = first.Payload
	if err = s.AppendProjectionTx(ctx, duplicate, conflict); !errors.Is(err, ErrFrame) {
		t.Fatal("identity conflict accepted", err)
	}
}
func TestStreamV2PayloadOwnership(t *testing.T) {
	f := notificationStreamFrame(t, "9007199254740993")
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseStreamV2(raw)
	if err != nil || parsed.Sequence != f.Sequence {
		t.Fatal(parsed, err)
	}
	f.Topic = "private.notifications.0x" + strings.Repeat("2", 40)
	raw, _ = json.Marshal(f)
	if _, err = parseStreamV2(raw); err == nil {
		t.Fatal("foreign private payload accepted")
	}
	f.Topic = "private.notifications.0x" + strings.Repeat("1", 40)
	f.Sequence = "01"
	raw, _ = json.Marshal(f)
	if _, err = parseStreamV2(raw); err == nil {
		t.Fatal("noncanonical sequence accepted")
	}
}

func snapshotCursor(t *testing.T, ctx context.Context, s *PostgresStream, topics []string) string {
	t.Helper()
	raw, err := s.PublicSnapshot(ctx, topics)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		ResumeCursor string `json:"resumeCursor"`
	}
	if err = json.Unmarshal(raw, &snapshot); err != nil || snapshot.ResumeCursor == "" {
		t.Fatal(err)
	}
	return snapshot.ResumeCursor
}

func TestPostgresRetainedIdentitiesSurviveInterruptedReplay(t *testing.T) {
	_, s := postgresStreamStorage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	first := publicPriceFrame(t, "1")
	commitStreamFrame(t, ctx, s, first)
	token := snapshotCursor(t, ctx, s, []string{first.Topic})
	initialToken := token
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer streamRollback(tx)
	expected := make(map[string]bool, 10000)
	for sequence := 2; sequence <= 10001; sequence++ {
		f := publicPriceFrame(t, fmt.Sprint(sequence))
		if err = s.AppendProjectionTx(ctx, tx, f); err != nil {
			t.Fatal(err)
		}
		expected[f.EventID] = true
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	replay, err := s.PublicResume(ctx, []string{first.Topic}, token)
	if err != nil {
		t.Fatal(err)
	}
	written := replay.WrittenCheckpoints()
	seen := make(map[string]bool, 10000)
	for len(seen) < len(expected) {
		batch, e := replay.Batch(ctx)
		if e != nil || len(batch) == 0 {
			t.Fatal("retained replay lost continuity", e, len(batch))
		}
		for _, raw := range batch {
			f, e := parseStreamV2(raw)
			if e != nil || !expected[f.EventID] || seen[f.EventID] {
				t.Fatal("retained identity missing or repeated", f.EventID, e)
			}
			seen[f.EventID] = true
			token, e = written.Advance(raw)
			if e != nil {
				t.Fatal(e)
			}
		}
		if len(seen) == 128 {
			replay.Close()
			replay, e = s.PublicResume(ctx, []string{first.Topic}, token)
			if e != nil {
				t.Fatal(e)
			}
			written = replay.WrittenCheckpoints()
		}
	}
	defer replay.Close()
	if replay.Machine.State != Live || len(seen) != 10000 {
		t.Fatal(replay.Machine.State, len(seen))
	}
	var committed int
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM "+s.table("stream_delivery_outbox")+" WHERE sequence>1").Scan(&committed); err != nil || committed != len(seen) {
		t.Fatal(committed, err)
	}
	commitStreamFrame(t, ctx, s, publicPriceFrame(t, "10002"))
	if _, err = s.PublicResume(ctx, []string{first.Topic}, initialToken); !errors.Is(err, ErrReplayLimit) {
		t.Fatal("oversized replay admitted before delivery", err)
	}
}

func TestPostgresResumeRejectsInternalRetentionHoleBeforeDelivery(t *testing.T) {
	ctx, s := postgresStreamStorage(t)
	first := publicPriceFrame(t, "1")
	commitStreamFrame(t, ctx, s, first)
	token := snapshotCursor(t, ctx, s, []string{first.Topic})
	for sequence := 2; sequence <= 4; sequence++ {
		commitStreamFrame(t, ctx, s, publicPriceFrame(t, fmt.Sprint(sequence)))
	}
	if _, err := s.pool.Exec(ctx, "DELETE FROM "+s.table("stream_delivery_outbox")+" WHERE sequence=3"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublicResume(ctx, []string{first.Topic}, token); !errors.Is(err, ErrSequence) {
		t.Fatal("retention hole admitted a partial replay", err)
	}
}

func TestPostgresResumePreflightsByteLimitBeforeDelivery(t *testing.T) {
	_, s := postgresStreamStorage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	first := publicPriceFrame(t, "1")
	commitStreamFrame(t, ctx, s, first)
	token := snapshotCursor(t, ctx, s, []string{first.Topic})
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer streamRollback(tx)
	size, frames := 0, 0
	for sequence := 2; size <= 10*1024*1024; sequence++ {
		f := publicPriceFrame(t, fmt.Sprint(sequence))
		var payload map[string]any
		if err = json.Unmarshal(f.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		source := payload["source"].(map[string]any)
		source["sourceEventId"] = "price-observation_" + strings.Repeat("1", 230)
		source["sourceReference"] = "https://price.example/observations/" + strings.Repeat("2", 220)
		f.Payload, err = json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		raw, e := json.Marshal(f)
		if e != nil {
			t.Fatal(e)
		}
		if err = s.AppendProjectionTx(ctx, tx, f); err != nil {
			t.Fatal(err)
		}
		size += len(raw)
		frames++
		if frames > 10000 {
			t.Fatal("byte-boundary workload exceeded frame boundary")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublicResume(ctx, []string{first.Topic}, token); !errors.Is(err, ErrReplayLimit) {
		t.Fatal("byte bound admitted partial replay", err)
	}
}
