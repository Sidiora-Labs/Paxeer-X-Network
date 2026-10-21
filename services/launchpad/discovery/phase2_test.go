package discovery

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	_ "github.com/jackc/pgx/v5/stdlib"
	"os"
	"testing"
	"time"
)

func realStore(t *testing.T) (Store, string) {
	t.Helper()
	dsn := os.Getenv("DISCOVERY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("DISCOVERY_TEST_DATABASE_URL must select an isolated PostgreSQL fixture")
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
func TestPostgresDurableAlertCrossingAndRestart(t *testing.T) {
	s, id := realStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	r := AlertRule{ID: id, OwnerAccountID: id, PoolAddress: pool, Metric: "price", Threshold: "10", Direction: "crosses_above", Channels: []string{"inapp"}, Revision: 1}
	if e := s.SaveRule(ctx, id, r, 0); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		s.DB.Exec(`DELETE FROM narrative_discovery.outbox WHERE id LIKE $1`, id+":%")
		s.DB.Exec(`DELETE FROM narrative_discovery.triggers WHERE rule_id=$1`, id)
		s.DB.Exec(`DELETE FROM narrative_discovery.rules WHERE id=$1`, id)
	})
	for i, value := range []string{"9", "11", "12"} {
		cross, _, e := s.EvaluateRule(ctx, id, Metric{Value: value, Revision: uint64(i + 1), ObservedAt: now, Quality: "complete"}, now)
		if e != nil || cross != (i == 1) {
			t.Fatal(i, cross, e)
		}
	}
	restarted := Store{s.DB}
	cross, reason, e := restarted.EvaluateRule(ctx, id, Metric{Value: "11", Revision: 2, ObservedAt: now, Quality: "complete"}, now)
	if e != nil || cross || reason != "replay" {
		t.Fatal(cross, reason, e)
	}
	var count int
	if e = s.DB.QueryRow(`SELECT count(*) FROM narrative_discovery.triggers WHERE rule_id=$1`, id).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
}
func TestPostgresInvitationReplayAndRemovedAuthorization(t *testing.T) {
	s, id := realStore(t)
	ctx := context.Background()
	owner, recipient := id+"o", id+"r"
	q := Squad{ID: id, OwnerAccountID: owner, Name: "Research", Policy: "invite", Revision: 1}
	if e := s.CreateSquad(ctx, owner, q); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		for _, table := range []string{"transfers", "invitations", "members"} {
			s.DB.Exec(`DELETE FROM narrative_discovery.`+table+` WHERE squad_id=$1`, id)
		}
		s.DB.Exec(`DELETE FROM narrative_discovery.squads WHERE id=$1`, id)
	})
	token := id + id
	if e := s.Invite(ctx, owner, id, recipient, token, time.Now().Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	m, e := s.AcceptInvitation(ctx, recipient, id, token, 1, time.Now())
	if e != nil || !m.CanPublish() {
		t.Fatal(m, e)
	}
	again, e := s.AcceptInvitation(ctx, recipient, id, token, 2, time.Now())
	if e != nil || again.Revision != m.Revision {
		t.Fatal(again, e)
	}
	if e = s.SetMemberStatus(ctx, owner, id, recipient, "removed", 2); e != nil {
		t.Fatal(e)
	}
	q.Revision = 2
	if e = s.UpdateSquad(ctx, recipient, q, 1); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
}
func TestSignedCursorAccountRevisionAndTampering(t *testing.T) {
	now := time.Now().UTC()
	s := CursorSigner{Key: []byte("01234567890123456789012345678901")}
	c := Cursor{Snapshot: "snapshot", AccountID: "alice", Algorithm: "v1", PreferenceRevision: 3, GraphRevision: 4, ExpiresAt: now.Add(time.Minute)}
	token, e := s.Sign(c)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Verify(token, "alice", 3, 4, now); e != nil {
		t.Fatal(e)
	}
	for _, account := range []string{"bob", ""} {
		if _, e = s.Verify(token, account, 3, 4, now); e == nil {
			t.Fatal("foreign cursor accepted")
		}
	}
	if _, e = s.Verify(token, "alice", 4, 4, now); e == nil {
		t.Fatal("stale preferences accepted")
	}
	if _, e = s.Verify("x"+token, "alice", 3, 4, now); e == nil {
		t.Fatal("tampering accepted")
	}
}

func TestPostgresTombstoneReplayAndDeletionEraseHistoricalText(t *testing.T) {
	s, id := realStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	c := Callout{ID: id, AuthorAccountID: id + "author", PoolAddress: pool, Body: "original thesis", PositionDisclosure: "declared position", Direction: "neutral", Disclosure: "organic", ModerationStatus: "approved", CreatedAt: now, Revision: 1}
	if err := s.transaction(ctx, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, `INSERT INTO narrative_discovery.callouts(id,document) VALUES($1,$2)`, id, encode(c)); e != nil {
			return e
		}
		return persistCallout(ctx, tx, c)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.DB.Exec(`DELETE FROM narrative_discovery.outbox WHERE id LIKE $1`, "callout:"+id+":%")
		s.DB.Exec(`DELETE FROM narrative_discovery.callout_revisions WHERE id=$1`, id)
		s.DB.Exec(`DELETE FROM narrative_discovery.callouts WHERE id=$1`, id)
		s.DB.Exec(`DELETE FROM narrative_discovery.sources WHERE id=$1`, id)
		s.DB.Exec(`DELETE FROM narrative_discovery.consumer_events WHERE consumer=$1`, id)
	})
	original, err := s.Source(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	service := CalloutService{Store: s}
	if _, err = service.Edit(ctx, "different-author", id, "", 1, true); !errors.Is(err, ErrForbidden) {
		t.Fatal("foreign deletion admitted", err)
	}
	if _, err = service.Edit(ctx, c.AuthorAccountID, id, "", 0, true); !errors.Is(err, ErrConflict) {
		t.Fatal("stale delete admitted", err)
	}
	deleted, err := service.Edit(ctx, c.AuthorAccountID, id, "", 1, true)
	if err != nil || !deleted.Deleted || deleted.Body != "" || deleted.PositionDisclosure != "" || !deleted.CreatedAt.Equal(now) {
		t.Fatal(deleted, err)
	}
	for replay := 0; replay < 100; replay++ {
		if err = s.Apply(ctx, id, id+"stale", original, time.Now().UTC()); !errors.Is(err, ErrConflict) {
			t.Fatal("creation resurrected deleted callout", replay, err)
		}
	}
	current, err := s.Source(ctx, id)
	if err != nil || !current.Tombstone || current.SourceRevision != 2 {
		t.Fatal(current, err)
	}
	feed := Feed{Store: s}
	card, err := feed.Detail(ctx, c.AuthorAccountID, id, time.Now())
	if err != nil || card.State != "removed" || card.Body != "" {
		t.Fatal(card, err)
	}
	var retained int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM narrative_discovery.callout_revisions WHERE id=$1 AND (document->>'Body'<>'' OR document->>'PositionDisclosure'<>'')`, id).Scan(&retained); err != nil || retained != 0 {
		t.Fatal("historical text retained", retained, err)
	}
}

func TestPostgresSourceAuthorityRejectsForgedCreatorAndResourceIdentity(t *testing.T) {
	s, id := realStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	c := Callout{ID: id, AuthorAccountID: id + "author", PoolAddress: pool, Body: "thesis", Direction: "neutral", Disclosure: "organic", ModerationStatus: "approved", CreatedAt: now, Revision: 1}
	if err := s.transaction(ctx, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, `INSERT INTO narrative_discovery.callouts(id,document) VALUES($1,$2)`, id, encode(c)); e != nil {
			return e
		}
		return persistCallout(ctx, tx, c)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.DB.Exec(`DELETE FROM narrative_discovery.outbox WHERE id LIKE $1`, "callout:"+id+":%")
		s.DB.Exec(`DELETE FROM narrative_discovery.callout_revisions WHERE id=$1`, id)
		s.DB.Exec(`DELETE FROM narrative_discovery.callouts WHERE id=$1`, id)
		s.DB.Exec(`DELETE FROM narrative_discovery.sources WHERE id=$1`, id)
	})
	ref, err := s.Source(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	owned := OwnedSources{Callouts: CalloutService{Store: s}}
	for _, mutate := range []func(*SourceRef){
		func(v *SourceRef) { v.CreatorPublicID = "forged" },
		func(v *SourceRef) { v.EventID = "forged" },
		func(v *SourceRef) { v.Pools = []string{"0x2222222222222222222222222222222222222222"} },
		func(v *SourceRef) { v.PublishedAt = v.PublishedAt.Add(time.Second) },
		func(v *SourceRef) { v.Corrected = true },
	} {
		forged := ref
		mutate(&forged)
		if err = s.Consume(ctx, owned, PublicEvent{Consumer: id, EventID: id, Source: forged, ObservedAt: time.Now()}); !errors.Is(err, ErrForbidden) {
			t.Fatal("forged authority fields admitted", forged, err)
		}
	}
}

func TestPostgresSquadRecipientConfirmationAndStaleRoleEdits(t *testing.T) {
	s, id := realStore(t)
	ctx := context.Background()
	owner, recipient := id+"owner", id+"recipient"
	q := Squad{ID: id, OwnerAccountID: owner, Name: "Research", Policy: "invite", Revision: 1}
	if err := s.CreateSquad(ctx, owner, q); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, table := range []string{"transfers", "invitations", "members"} {
			s.DB.Exec(`DELETE FROM narrative_discovery.`+table+` WHERE squad_id=$1`, id)
		}
		s.DB.Exec(`DELETE FROM narrative_discovery.squads WHERE id=$1`, id)
	})
	token := id + id
	if err := s.Invite(ctx, owner, id, recipient, token, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptInvitation(ctx, recipient, id, token, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.ProposeTransfer(ctx, recipient, id, owner, 1); !errors.Is(err, ErrForbidden) {
		t.Fatal("ordinary member proposed transfer", err)
	}
	if err := s.ProposeTransfer(ctx, owner, id, recipient, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmTransfer(ctx, owner, id); !errors.Is(err, ErrForbidden) {
		t.Fatal("owner bypassed recipient confirmation", err)
	}
	if err := s.SetMemberStatus(ctx, owner, id, recipient, "removed", 1); !errors.Is(err, ErrConflict) {
		t.Fatal("stale role mutation admitted", err)
	}
	if err := s.SetMemberStatus(ctx, owner, id, recipient, "banned", 2); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmTransfer(ctx, recipient, id); !errors.Is(err, ErrForbidden) {
		t.Fatal("banned recipient gained ownership", err)
	}
	if _, err := s.AcceptInvitation(ctx, recipient, id, token, 3, time.Now()); !errors.Is(err, ErrForbidden) {
		t.Fatal("consumed invitation restored banned member", err)
	}
}
