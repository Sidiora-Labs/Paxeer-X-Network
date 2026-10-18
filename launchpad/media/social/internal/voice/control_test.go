package voice

import (
	"context"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/safety"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

const host = "0x1111111111111111111111111111111111111111"

type knownPools struct{}

func (knownPools) Exists(_ context.Context, p string) (bool, error) { return p == "known-pool", nil }

type allowPolicy struct{}

func (allowPolicy) Check(context.Context, string, string, string) safety.Decision {
	return safety.Allow
}

// failureProvider only exercises unavailable handling. It produces no sessions,
// tokens or transport-success evidence.
type failureProvider struct{ calls int }

func (p *failureProvider) Start(context.Context, string) (string, error) {
	p.calls++
	return "", ErrUnavailable
}
func (p *failureProvider) End(context.Context, string) error { p.calls++; return ErrUnavailable }
func (p *failureProvider) Revoke(context.Context, string, string) error {
	p.calls++
	return ErrUnavailable
}
func (p *failureProvider) Ticket(context.Context, string, string, bool, time.Duration) (string, error) {
	p.calls++
	return "", ErrUnavailable
}
func fixture(t *testing.T) (*Control, context.Context) {
	t.Helper()
	ctx := context.Background()
	dsn := os.Getenv("L38_POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"
	}
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	schema := "kd_9677e1202ca2546e7bac624f_" + uuid.NewString()[:8]
	ident := pgx.Identifier{schema}.Sanitize()
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+ident); e != nil {
		admin.Close()
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		db.Close()
		if _, e := admin.Exec(context.Background(), "DROP SCHEMA "+ident+" CASCADE"); e != nil {
			t.Error(e)
		}
		admin.Close()
	})
	ddl, e := os.ReadFile("../../../../spec/production-system/handoffs/L38/schema-proposal.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, string(ddl)); e != nil {
		t.Fatal(e)
	}
	c := &Control{DB: db, Pools: knownPools{}, Policy: allowPolicy{}}
	ctx = safety.WithPrincipal(ctx, safety.Principal{Wallet: host, SessionID: "voice-fixture", Expires: time.Now().Add(time.Hour)})
	return c, ctx
}
func TestPostgresUnavailableStartStaysDraft(t *testing.T) {
	c, ctx := fixture(t)
	if _, e := c.Create(ctx, "unknown-pool"); !errors.Is(e, safety.ErrInvalid) {
		t.Fatal("unknown pool accepted", e)
	}
	r, e := c.Create(ctx, "known-pool")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Start(ctx, r.ID, 1); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	persisted, e := c.Read(ctx, r.ID)
	if e != nil || persisted.State != "draft" || persisted.ProviderSession != "" || persisted.Revision != 1 {
		t.Fatalf("%+v %v", persisted, e)
	}
	provider := &failureProvider{}
	c.Provider = provider
	c.Gates = Gates{true, true, true, true}
	if _, e = c.Start(ctx, r.ID, 1); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	persisted, e = c.Read(ctx, r.ID)
	if e != nil || persisted.State != "draft" || persisted.Revision != 1 || provider.calls != 1 {
		t.Fatalf("%+v %v", persisted, e)
	}
	restarted := &Control{DB: c.DB}
	restored, e := restarted.Read(ctx, r.ID)
	if e != nil || restored.StartKey != r.StartKey || restored.APIState() != "scheduled" {
		t.Fatal(restored, e)
	}
	if e = c.StartRecording(ctx, r.ID); !errors.Is(e, ErrUnavailable) {
		t.Fatal("recording enabled", e)
	}
}
func TestPostgresCleanupFailureStaysEndingAndDisablesTokens(t *testing.T) {
	c, ctx := fixture(t)
	r, e := c.Create(ctx, "known-pool")
	if e != nil {
		t.Fatal(e)
	}
	// Persisted live-row fixture tests cleanup failure only. This SQL seed is not
	// evidence that any provider session or audio transport actually existed.
	_, e = c.DB.Exec(ctx, `UPDATE voice_rooms SET state='live',provider_session='failure-only-session-fixture' WHERE id=$1`, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	c.Provider = &failureProvider{}
	c.Gates = Gates{true, true, true, true}
	ending, e := c.End(ctx, r.ID, 1)
	if !errors.Is(e, ErrUnavailable) || ending.State != "ending" {
		t.Fatalf("%+v %v", ending, e)
	}
	restored, e := c.Read(ctx, r.ID)
	if e != nil || restored.State != "ending" || restored.Revision != 2 {
		t.Fatalf("%+v %v", restored, e)
	}
	if _, e = c.Ticket(ctx, r.ID); !errors.Is(e, ErrUnavailable) {
		t.Fatal("ending room grants token", e)
	}
	if _, e = c.End(ctx, r.ID, 2); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	restored, _ = c.Read(ctx, r.ID)
	if restored.State != "ending" || restored.Revision != 2 {
		t.Fatal(restored)
	}
	other := safety.WithPrincipal(ctx, safety.Principal{Wallet: "0x2222222222222222222222222222222222222222", SessionID: "other", Expires: time.Now().Add(time.Hour)})
	if _, e = c.End(other, r.ID, 2); !errors.Is(e, safety.ErrForbidden) {
		t.Fatal("non-host ended room", e)
	}
}

func TestPostgresPendingRevocationBlocksPromotionAndToken(t *testing.T) {
	c, ctx := fixture(t)
	r, e := c.Create(ctx, "known-pool")
	if e != nil {
		t.Fatal(e)
	}
	// Failure-only persisted-room fixture; no real voice transport is claimed.
	if _, e = c.DB.Exec(ctx, `UPDATE voice_rooms SET state='live',provider_session='failure-only-session-fixture' WHERE id=$1`, r.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = c.DB.Exec(ctx, `INSERT INTO voice_participants(room_id,wallet)VALUES($1,$2)`, r.ID, host); e != nil {
		t.Fatal(e)
	}
	p := &failureProvider{}
	c.Provider = p
	c.Gates = Gates{true, true, true, true}
	if e = c.Mute(ctx, r.ID, host, 1, false); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	if e = c.ApproveSpeaker(ctx, r.ID, host, 2); !errors.Is(e, safety.ErrConflict) {
		t.Fatal("pending revocation promoted", e)
	}
	if _, e = c.Ticket(ctx, r.ID); !errors.Is(e, ErrUnavailable) {
		t.Fatal("pending revocation minted token", e)
	}
	var role, grant string
	var muted bool
	var rev int64
	if e = c.DB.QueryRow(ctx, `SELECT role,muted,grant_state,revision FROM voice_participants WHERE room_id=$1 AND wallet=$2`, r.ID, host).Scan(&role, &muted, &grant, &rev); e != nil || role != "listener" || !muted || grant != "revoking" || rev != 2 || p.calls != 1 {
		t.Fatal(role, muted, grant, rev, p.calls, e)
	}
}

func TestPostgresVoicePolicyPreservesUnavailable(t *testing.T) {
	c, ctx := fixture(t)
	policy := &safety.Store{DB: c.DB}
	c.Policy = policy
	if e := c.permission(ctx, host, "room-a", "known-pool"); e != nil {
		t.Fatal(e)
	}
	if _, e := c.DB.Exec(ctx, `INSERT INTO safety_bans(id,wallet,scope,resource,actor)VALUES($1,$2,'pool','known-pool',$2)`, uuid.NewString(), host); e != nil {
		t.Fatal(e)
	}
	if e := c.permission(ctx, host, "room-a", "known-pool"); !errors.Is(e, safety.ErrForbidden) {
		t.Fatal("ban allowed", e)
	}
	if _, e := c.DB.Exec(ctx, `DROP TABLE safety_bans`); e != nil {
		t.Fatal(e)
	}
	if e := c.permission(ctx, host, "room-a", "known-pool"); !errors.Is(e, ErrUnavailable) {
		t.Fatal("authority failure was not retryable", e)
	}
}
