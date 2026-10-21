package notifications

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	_ "github.com/jackc/pgx/v5/stdlib"
	"os"
	"sync"
	"testing"
	"time"
)

func notificationDatabase(t *testing.T) (Store, string) {
	t.Helper()
	dsn := os.Getenv("NOTIFICATIONS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("NOTIFICATIONS_TEST_DATABASE_URL must select an isolated PostgreSQL fixture")
	}
	db, e := sql.Open("pgx", dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, e = db.ExecContext(ctx, Schema); e != nil {
		t.Fatal(e)
	}
	var id [16]byte
	if _, e = rand.Read(id[:]); e != nil {
		t.Fatal(e)
	}
	return Store{db}, hex.EncodeToString(id[:])
}
func TestPostgresHundredTriggerReplaysAndFencedRecovery(t *testing.T) {
	s, account := notificationDatabase(t)
	ctx := context.Background()
	now := time.Now().UTC()
	n := InboxItem{Identity: Identity{account, "trigger", "v2"}, Category: "market", Summary: "Threshold crossed", CreatedAt: now}
	ds := []DeliveryAttempt{{Channel: "push", TargetID: "installation"}}
	t.Cleanup(func() { s.DeleteAccount(ctx, account) })
	var id string
	for i := 0; i < 100; i++ {
		got, created, e := s.CommitTrigger(ctx, n, ds)
		if e != nil || created != (i == 0) {
			t.Fatal(i, created, e)
		}
		if id != "" && id != got.ID {
			t.Fatal("identity changed")
		}
		id = got.ID
	}
	rows, e := s.List(ctx, account, 50)
	if e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	claimed, e := s.Claim(ctx, "first", now, 100)
	if e != nil {
		t.Fatal(e)
	}
	var claim OutboxItem
	for _, v := range claimed {
		if v.Notification.ID == id {
			claim = v
		}
	}
	if claim.ID == "" {
		t.Fatal("committed outbox absent")
	}
	if _, e = s.DB.Exec(`UPDATE discovery_notifications.outbox SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, id); e != nil {
		t.Fatal(e)
	}
	recovered, e := s.Claim(ctx, "second", time.Now(), 100)
	if e != nil {
		t.Fatal(e)
	}
	var second OutboxItem
	for _, v := range recovered {
		if v.Notification.ID == id {
			second = v
		}
	}
	if second.ID == "" || second.ID == claim.ID {
		t.Fatal("fence did not advance")
	}
	d := claim.Deliveries[0]
	d.Attempt = 1
	d.State = "unknown"
	d.Reason = "provider_timeout"
	d.NextAt = time.Now().Add(time.Minute)
	if e = s.Complete(ctx, "first", claim.ID, d); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("expired claimant accepted", e)
	}
	if e = s.Complete(ctx, "second", second.ID, d); e != nil {
		t.Fatal(e)
	}
	if e = s.SetRead(ctx, "different-account", id, true); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal(e)
	}
	if e = s.SetRead(ctx, account, id, true); e != nil {
		t.Fatal(e)
	}
	if e = s.SetRead(ctx, account, id, true); e != nil {
		t.Fatal(e)
	}
	rows, e = s.List(ctx, account, 1)
	if e != nil || !rows[0].Read {
		t.Fatal(rows, e)
	}
}
func TestWebPushEndpointAdmission(t *testing.T) {
	for _, endpoint := range []string{"http://fcm.googleapis.com/x", "https://127.0.0.1/x", "https://fcm.googleapis.com.evil.test/x", "https://user@fcm.googleapis.com/x", "https://fcm.googleapis.com:443/x"} {
		if subscription(endpoint) == nil {
			t.Fatal(endpoint)
		}
	}
	if e := subscription("https://fcm.googleapis.com/fcm/send/subscription"); e != nil {
		t.Fatal(e)
	}
}

func TestPostgresSendInterruptionPersistsUnknownAndFencesRestart(t *testing.T) {
	s, account := notificationDatabase(t)
	ctx := context.Background()
	now := time.Now().UTC()
	n := InboxItem{Identity: Identity{account, "interrupted-send", "v3"}, Category: "watchlist", Summary: "Threshold crossed", CreatedAt: now}
	item, created, err := s.CommitTrigger(ctx, n, []DeliveryAttempt{{Channel: "push", TargetID: "device", TargetRevision: 8, ConsentRevision: 3}})
	if err != nil || !created {
		t.Fatal(created, err)
	}
	t.Cleanup(func() { s.DeleteAccount(ctx, account) })
	claims, err := s.Claim(ctx, account, time.Now(), 100)
	if err != nil {
		t.Fatal(err)
	}
	var claim OutboxItem
	for _, c := range claims {
		if c.Notification.ID == item.ID {
			claim = c
		}
	}
	if claim.ID == "" {
		t.Fatal("committed inbox had no resumable outbox")
	}
	d := claim.Deliveries[0]
	d.Attempt = 1
	d.NextAt = now
	if err = s.BeginSend(ctx, account, claim.ID, d); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE discovery_notifications.outbox SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	restarted := Store{s.DB}
	claims, err = restarted.Claim(ctx, account+"restart", time.Now(), 100)
	if err != nil {
		t.Fatal(err)
	}
	var recovered OutboxItem
	for _, c := range claims {
		if c.Notification.ID == item.ID {
			recovered = c
		}
	}
	if recovered.ID == "" || recovered.ID == claim.ID {
		t.Fatal("restart did not acquire new fence")
	}
	got := recovered.Deliveries[0]
	if got.State != "unknown" || !got.AcceptanceUnknown || got.Reason != "send_interrupted" || got.Attempt != 1 {
		t.Fatal("possible acceptance lost on restart", got)
	}
	d.State = "accepted"
	d.ProviderReceipt = "receipt"
	if err = s.Complete(ctx, account, claim.ID, d); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("expired sender wrote acceptance", err)
	}
	got.Attempt++
	got.State = "suppressed"
	got.Reason = "target_revoked"
	got = preserveUncertainty(got, true)
	if err = restarted.Complete(ctx, account+"restart", recovered.ID, got); err != nil {
		t.Fatal(err)
	}
	var terminal bool
	if err = s.DB.QueryRowContext(ctx, `SELECT terminal FROM discovery_notifications.outbox WHERE id=$1`, item.ID).Scan(&terminal); err != nil || !terminal {
		t.Fatal("uncertain stopped send was not terminal", terminal, err)
	}
}

func TestPostgresDeletionPreventsTriggerReplayAndOutstandingSend(t *testing.T) {
	s, account := notificationDatabase(t)
	ctx := context.Background()
	n := InboxItem{Identity: Identity{account, "deletion-replay", "v3"}, Category: "chat", Summary: "Private message", CreatedAt: time.Now().UTC()}
	item, _, err := s.CommitTrigger(ctx, n, []DeliveryAttempt{{Channel: "push", TargetID: "device", TargetRevision: 4, ConsentRevision: 3}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DeleteAccount(ctx, account) })
	claims, err := s.Claim(ctx, account, time.Now(), 100)
	if err != nil {
		t.Fatal(err)
	}
	var claim OutboxItem
	for _, c := range claims {
		if c.Notification.ID == item.ID {
			claim = c
		}
	}
	if claim.ID == "" {
		t.Fatal("outbox missing")
	}
	if err = s.DeleteAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	d := claim.Deliveries[0]
	d.Attempt = 1
	d.NextAt = time.Now()
	if err = s.BeginSend(ctx, account, claim.ID, d); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("send admitted after inbox erasure", err)
	}
	for replay := 0; replay < 100; replay++ {
		if _, created, e := s.CommitTrigger(ctx, n, nil); !errors.Is(e, ErrForbidden) || created {
			t.Fatal("erased content recreated", replay, created, e)
		}
	}
	rows, err := s.List(ctx, account, 50)
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	var count int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM discovery_notifications.outbox WHERE id=$1`, item.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestProviderRejectionsRequireAuthoritativeStatusAndType(t *testing.T) {
	for _, sample := range []struct {
		status  int
		body    string
		disable bool
	}{
		{410, `{"reason":"Unregistered"}`, true},
		{400, `{"reason":"BadDeviceToken"}`, true},
		{503, `{"reason":"Unregistered"}`, false},
		{403, `{"reason":"BadDeviceToken"}`, false},
	} {
		result, err := apnsResult(sample.status, nil, []byte(sample.body))
		if err != nil || result.DisableTarget != sample.disable || ValidateResult(result) != nil {
			t.Fatal(result, err)
		}
	}
	for _, sample := range []struct {
		status  int
		body    string
		disable bool
	}{
		{404, `{"error":{"details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"UNREGISTERED"}]}}`, true},
		{503, `{"error":{"details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"UNREGISTERED"}]}}`, false},
		{404, `{"error":{"details":[{"@type":"unrelated","errorCode":"UNREGISTERED"}]}}`, false},
	} {
		result, err := fcmResult(sample.status, []byte(sample.body))
		if err != nil || result.DisableTarget != sample.disable || ValidateResult(result) != nil {
			t.Fatal(result, err)
		}
	}
	for _, status := range []int{404, 410} {
		result, err := webPushResult(status, nil)
		if err != nil || result.State != "rejected" || !result.DisableTarget {
			t.Fatal(result, err)
		}
	}
	if result, err := webPushResult(202, nil); err != nil || result.State != "unknown" {
		t.Fatal("missing receipt claimed acceptance", result, err)
	}
}

func TestPostgresConcurrentHundredTriggersHaveOneLogicalEffect(t *testing.T) {
	s, account := notificationDatabase(t)
	ctx := context.Background()
	n := InboxItem{Identity: Identity{account, "concurrent-trigger", "v3"}, Category: "watchlist", Summary: "Threshold crossed", CreatedAt: time.Now().UTC()}
	t.Cleanup(func() { s.DeleteAccount(ctx, account) })
	results := make(chan error, 100)
	created := make(chan bool, 100)
	var workers sync.WaitGroup
	for replay := 0; replay < 100; replay++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, newItem, err := s.CommitTrigger(ctx, n, []DeliveryAttempt{{Channel: "push", TargetID: "device"}})
			results <- err
			created <- newItem
		}()
	}
	workers.Wait()
	close(results)
	close(created)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	count := 0
	for inserted := range created {
		if inserted {
			count++
		}
	}
	if count != 1 {
		t.Fatal("concurrent trigger committed more than once", count)
	}
	rows, err := s.List(ctx, account, 50)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	var outboxes int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM discovery_notifications.outbox WHERE id=$1`, rows[0].ID).Scan(&outboxes); err != nil || outboxes != 1 {
		t.Fatal(outboxes, err)
	}
}
