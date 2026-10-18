package fanout_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/fanout"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func durableFixture(t *testing.T) *fanout.DurableStore {
	t.Helper()
	ctx := context.Background()
	dsn := "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "kd_fc141e636041e07255cca9ba_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	name := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+name); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 2
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+name+" CASCADE"); err != nil {
			t.Error(err)
		}
		admin.Close(context.Background())
	})
	ddl, err := os.ReadFile("../../../../spec/production-system/handoffs/L36/phase1/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(ddl)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `CREATE TABLE fixture_policy(wallet text PRIMARY KEY,allowed boolean NOT NULL); CREATE TABLE fixture_pools(id text PRIMARY KEY);`); err != nil {
		t.Fatal(err)
	}
	for _, wallet := range []string{addr(0xaa), addr(0xbb), addr(0xcc), addr(0xa11ce), addr(0xb0b), addr(0xe7e), addr(0xbad), addr(0xfa57), addr(0x510)} {
		if _, err = pool.Exec(ctx, `INSERT INTO fixture_policy VALUES($1,true)`, wallet); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 4; i++ {
		if _, err = pool.Exec(ctx, `INSERT INTO fixture_pools VALUES($1)`, addr(i)); err != nil {
			t.Fatal(err)
		}
	}
	policy := func(ctx context.Context, tx pgx.Tx, in fanout.SendInput) error {
		wallets := []string{in.Actor}
		if in.Kind == "dm" {
			wallets = append(wallets, in.Target)
		}
		for _, wallet := range wallets {
			var allowed bool
			if err := tx.QueryRow(ctx, `SELECT allowed FROM fixture_policy WHERE wallet=$1 FOR SHARE`, wallet).Scan(&allowed); err != nil {
				return err
			}
			if !allowed {
				return errors.New("banned")
			}
		}
		if in.Kind == "pool" {
			var id string
			if err := tx.QueryRow(ctx, `SELECT id FROM fixture_pools WHERE id=$1 FOR SHARE`, in.Target).Scan(&id); err != nil {
				return err
			}
		}
		return nil
	}
	return &fanout.DurableStore{Pool: pool, Policy: policy, CursorKey: []byte("private-fixture-key-32-bytes-long-L36"), ChannelNamespace: schema + ":"}
}
func sendDM(t *testing.T, s *fanout.DurableStore, id string) fanout.DurableMessage {
	t.Helper()
	m, err := s.Send(context.Background(), fanout.SendInput{Actor: addr(0xaa), Target: addr(0xbb), Kind: "dm", ClientID: id, Content: "private text"})
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func TestDurableAtomicRollbackIdempotencyAndOutboxRecovery(t *testing.T) {
	s := durableFixture(t)
	ctx := context.Background()
	_, err := s.Pool.Exec(ctx, `CREATE FUNCTION fixture_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture insert fault'; END $$; CREATE TRIGGER fixture_fault BEFORE INSERT ON chat_messages FOR EACH ROW EXECUTE FUNCTION fixture_fail();`)
	if err != nil {
		t.Fatal(err)
	}
	in := fanout.SendInput{Actor: addr(0xaa), Target: addr(0xbb), Kind: "dm", ClientID: "retry", Content: "private text"}
	if _, err = s.Send(ctx, in); err == nil {
		t.Fatal("expected insert fault")
	}
	for _, table := range []string{"chat_conversations", "chat_members", "chat_messages", "chat_outbox"} {
		var n int
		if err = s.Pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s rollback rows=%d err=%v", table, n, err)
		}
	}
	if _, err = s.Pool.Exec(ctx, `DROP TRIGGER fixture_fault ON chat_messages`); err != nil {
		t.Fatal(err)
	}
	first := sendDM(t, s, "retry")
	second := sendDM(t, s, "retry")
	if first.ID != second.ID || second.Sequence != 1 {
		t.Fatal("duplicate committed")
	}
	in.Content = "changed"
	if _, err = s.Send(ctx, in); !errors.Is(err, fanout.ErrConflict) {
		t.Fatal(err)
	}
	_, err = s.DrainOutbox(ctx, 1, func(context.Context, string, []string, []byte) error { return errors.New("publisher stopped") })
	if err == nil {
		t.Fatal("expected publisher failure")
	}
	recovered := *s
	delivered := map[string]bool{}
	n, err := recovered.DrainOutbox(ctx, 100, func(_ context.Context, id string, ch []string, p []byte) error {
		if len(ch) != 2 {
			t.Fatal(ch)
		}
		var m fanout.DurableMessage
		if err := json.Unmarshal(p, &m); err != nil {
			t.Fatal(err)
		}
		if m.ID != first.ID {
			t.Fatal(m)
		}
		delivered[id] = true
		return nil
	})
	if err != nil || n != 1 || len(delivered) != 1 {
		t.Fatal(n, err)
	}
	n, err = recovered.DrainOutbox(ctx, 1, func(context.Context, string, []string, []byte) error { t.Fatal("redelivery"); return nil })
	if err != nil || n != 0 {
		t.Fatal(n, err)
	}
}
func TestDurableConcurrentCanonicalConversation(t *testing.T) {
	s := durableFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			actor, target := addr(0xaa), addr(0xbb)
			if i%2 == 1 {
				actor, target = target, actor
			}
			_, err := s.Send(ctx, fanout.SendInput{Actor: actor, Target: target, Kind: "dm", ClientID: fmt.Sprintf("c%d", i), Content: "hello"})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var conversations, members, messages, seq int
	if err := s.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM chat_conversations),(SELECT count(*) FROM chat_members),(SELECT count(*) FROM chat_messages),(SELECT max(sequence) FROM chat_messages)`).Scan(&conversations, &members, &messages, &seq); err != nil {
		t.Fatal(err)
	}
	if conversations != 1 || members != 2 || messages != 12 || seq != 12 {
		t.Fatal(conversations, members, messages, seq)
	}
}
func TestDurablePrivacyCursorAndThousandMessageReplay(t *testing.T) {
	s := durableFixture(t)
	ctx := context.Background()
	m := sendDM(t, s, "first")
	if ids, err := s.List(ctx, addr(0xcc), 100); err != nil || len(ids) != 0 {
		t.Fatal(ids, err)
	}
	if _, err := s.History(ctx, addr(0xcc), m.ConversationID, "", 50); !errors.Is(err, fanout.ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.Authorized(ctx, addr(0xcc), m.ConversationID); !errors.Is(err, fanout.ErrNotFound) {
		t.Fatal(err)
	}
	page, err := s.History(ctx, addr(0xaa), m.ConversationID, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.History(ctx, addr(0xbb), m.ConversationID, page.Next, 50); !errors.Is(err, fanout.ErrCursor) {
		t.Fatal(err)
	}
	if _, err = s.History(ctx, addr(0xaa), m.ConversationID, page.Next+"x", 50); !errors.Is(err, fanout.ErrCursor) {
		t.Fatal(err)
	}
	if _, err = s.History(ctx, addr(0xaa), m.ConversationID, "", 101); !errors.Is(err, fanout.ErrInvalid) {
		t.Fatal(err)
	}
	for i := 2; i <= 1000; i++ {
		sendDM(t, s, fmt.Sprintf("replay-%d", i))
	}
	token := ""
	last := int64(0)
	seen := map[string]bool{}
	for last < 1000 {
		p, err := s.History(ctx, addr(0xbb), m.ConversationID, token, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Messages) == 0 {
			t.Fatal("gap")
		}
		for _, row := range p.Messages {
			if row.Sequence != last+1 || seen[row.ID] {
				t.Fatal("unordered/duplicate", row)
			}
			seen[row.ID] = true
			last = row.Sequence
		}
		token = p.Next
	}
	if len(seen) != 1000 {
		t.Fatal(len(seen))
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE chat_conversations SET retained_from=250 WHERE id=$1`, m.ConversationID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.History(ctx, addr(0xbb), m.ConversationID, "", 100); !errors.Is(err, fanout.ErrReplayExpired) {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, `DELETE FROM chat_members WHERE conversation_id=$1 AND wallet=$2`, m.ConversationID, addr(0xbb)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.History(ctx, addr(0xbb), m.ConversationID, token, 100); !errors.Is(err, fanout.ErrNotFound) {
		t.Fatal(err)
	}
}
func TestDurableRevisionAndReadMonotonicity(t *testing.T) {
	s := durableFixture(t)
	ctx := context.Background()
	m := sendDM(t, s, "edit")
	edited, err := s.Revise(ctx, addr(0xaa), m.ID, 1, "updated", false)
	if err != nil || edited.Revision != 2 {
		t.Fatal(edited, err)
	}
	if _, err = s.Revise(ctx, addr(0xaa), m.ID, 1, "stale", false); !errors.Is(err, fanout.ErrConflict) {
		t.Fatal(err)
	}
	if _, err = s.Revise(ctx, addr(0xcc), m.ID, 2, "foreign", false); !errors.Is(err, fanout.ErrNotFound) {
		t.Fatal(err)
	}
	removed, err := s.Revise(ctx, addr(0xaa), m.ID, 2, "", true)
	if err != nil || removed.Revision != 3 || removed.Content != "" {
		t.Fatal(removed, err)
	}
	if _, err = s.Revise(ctx, addr(0xaa), m.ID, 3, "resurrect", false); !errors.Is(err, fanout.ErrDeleted) {
		t.Fatal(err)
	}
	page, err := s.History(ctx, addr(0xbb), m.ConversationID, "", 50)
	if err != nil || !page.Messages[0].Deleted || page.Messages[0].Content != "" {
		t.Fatal(page, err)
	}
	for _, seq := range []int64{100, 0} {
		if err = s.MarkRead(ctx, addr(0xbb), m.ConversationID, seq); err != nil {
			t.Fatal(err)
		}
	}
	var marker int64
	if err = s.Pool.QueryRow(ctx, `SELECT read_sequence FROM chat_members WHERE conversation_id=$1 AND wallet=$2`, m.ConversationID, addr(0xbb)).Scan(&marker); err != nil || marker != 1 {
		t.Fatal(marker, err)
	}
	if err = s.MarkRead(ctx, addr(0xcc), m.ConversationID, 1); !errors.Is(err, fanout.ErrNotFound) {
		t.Fatal(err)
	}
	s.Clock = func() time.Time { return time.Now().Add(16 * time.Minute) }
	n := sendDM(t, s, "time-window")
	s.Clock = func() time.Time { return time.Now().Add(32 * time.Minute) }
	if _, err = s.Revise(ctx, addr(0xaa), n.ID, 1, "late", false); !errors.Is(err, fanout.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestDurablePolicyDenialAndInputBoundaries(t *testing.T) {
	s := durableFixture(t)
	ctx := context.Background()
	if _, err := s.Pool.Exec(ctx, `UPDATE fixture_policy SET allowed=false WHERE wallet=$1`, addr(0xbb)); err != nil {
		t.Fatal(err)
	}
	in := fanout.SendInput{Actor: addr(0xaa), Target: addr(0xbb), Kind: "dm", ClientID: "blocked", Content: "hello"}
	if _, err := s.Send(ctx, in); err == nil {
		t.Fatal("blocked recipient accepted")
	}
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM chat_messages`).Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	in.ClientID = ""
	if _, err := s.Send(ctx, in); !errors.Is(err, fanout.ErrInvalid) {
		t.Fatal(err)
	}
	s.Policy = nil
	in.ClientID = "missing-policy"
	if _, err := s.Send(ctx, in); !errors.Is(err, fanout.ErrUnavailable) {
		t.Fatal(err)
	}
}

func TestDurableRevokedMemberCannotRestoreMembershipOrRetryRead(t *testing.T) {
	s := durableFixture(t)
	ctx := context.Background()
	m := sendDM(t, s, "before-revoke")
	if _, err := s.Pool.Exec(ctx, `DELETE FROM chat_members WHERE conversation_id=$1 AND wallet=$2`, m.ConversationID, addr(0xaa)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"before-revoke", "after-revoke"} {
		_, err := s.Send(ctx, fanout.SendInput{Actor: addr(0xaa), Target: addr(0xbb), Kind: "dm", ClientID: id, Content: "private text"})
		if !errors.Is(err, fanout.ErrNotFound) {
			t.Fatal(id, err)
		}
	}
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM chat_members WHERE conversation_id=$1 AND wallet=$2`, m.ConversationID, addr(0xaa)).Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}

func TestDurableOfflineRevisionRequiresCoherentSnapshotRestart(t *testing.T) {
	s := durableFixture(t)
	ctx := context.Background()
	m := sendDM(t, s, "offline-revision")
	initial, err := s.History(ctx, addr(0xbb), m.ConversationID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Revise(ctx, addr(0xaa), m.ID, 1, "offline edit", false); err != nil {
		t.Fatal(err)
	}
	reset, err := s.History(ctx, addr(0xbb), m.ConversationID, initial.Next, 50)
	if !errors.Is(err, fanout.ErrSnapshotRequired) || !reset.Reset || len(reset.Messages) != 0 || reset.Next == "" {
		t.Fatal(reset, err)
	}
	refreshed, err := s.History(ctx, addr(0xbb), m.ConversationID, reset.Next, 50)
	if err != nil || len(refreshed.Messages) != 1 || refreshed.Messages[0].Content != "offline edit" || refreshed.Messages[0].Revision != 2 {
		t.Fatal(refreshed, err)
	}
	if _, err = s.Revise(ctx, addr(0xaa), m.ID, 2, "", true); err != nil {
		t.Fatal(err)
	}
	reset, err = s.History(ctx, addr(0xbb), m.ConversationID, refreshed.Next, 50)
	if !errors.Is(err, fanout.ErrSnapshotRequired) || !reset.Reset {
		t.Fatal(reset, err)
	}
	refreshed, err = s.History(ctx, addr(0xbb), m.ConversationID, reset.Next, 50)
	if err != nil || len(refreshed.Messages) != 1 || !refreshed.Messages[0].Deleted || refreshed.Messages[0].Content != "" || refreshed.Messages[0].Revision != 3 {
		t.Fatal(refreshed, err)
	}
	if _, err = s.History(ctx, addr(0xcc), m.ConversationID, reset.Next, 50); err == nil {
		t.Fatal("third wallet got reset snapshot")
	}
}
func TestDurableCancelledTypedWritesRollbackAndPoolRecovers(t *testing.T) {
	s := durableFixture(t)
	ctx := context.Background()
	_, err := s.Pool.Exec(ctx, `CREATE FUNCTION fixture_hold() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(0.4); RETURN NEW; END $$; CREATE TRIGGER fixture_hold_write BEFORE INSERT ON chat_messages FOR EACH ROW EXECUTE FUNCTION fixture_hold();`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		cancelled, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		started := time.Now()
		_, err = s.Send(cancelled, fanout.SendInput{Actor: addr(0xaa), Target: addr(0xbb), Kind: "dm", ClientID: fmt.Sprintf("cancel-%d", i), Content: "cancelled write"})
		cancel()
		if err == nil || time.Since(started) > 3*time.Second {
			t.Fatal("unbounded or successful cancellation", err)
		}
		recovered, done := context.WithTimeout(ctx, 2*time.Second)
		if err = s.Pool.Ping(recovered); err != nil {
			done()
			t.Fatal("pool did not recover", err)
		}
		done()
		for _, table := range []string{"chat_conversations", "chat_members", "chat_messages", "chat_outbox"} {
			var n int
			if err = s.Pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 0 {
				t.Fatal(table, n, err)
			}
		}
	}
	if _, err = s.Pool.Exec(ctx, `DROP TRIGGER fixture_hold_write ON chat_messages`); err != nil {
		t.Fatal(err)
	}
	m := sendDM(t, s, "recovered")
	cancelled, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	started := time.Now()
	_, err = s.DrainOutbox(cancelled, 1, func(pctx context.Context, _ string, _ []string, _ []byte) error { <-pctx.Done(); return pctx.Err() })
	cancel()
	if err == nil || time.Since(started) > 3*time.Second {
		t.Fatal("unbounded or successful publish cancellation", err)
	}
	var pending int
	if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM chat_outbox WHERE delivered_at IS NULL`).Scan(&pending); err != nil || pending != 1 {
		t.Fatal(pending, err)
	}
	recovered, done := context.WithTimeout(ctx, 2*time.Second)
	defer done()
	if err = s.Pool.Ping(recovered); err != nil {
		t.Fatal(err)
	}
	retry := sendDM(t, s, "recovered")
	if retry.ID != m.ID {
		t.Fatal("retry duplicated after cancelled publisher")
	}
}
