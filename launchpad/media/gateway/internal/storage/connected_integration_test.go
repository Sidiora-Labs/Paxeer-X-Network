package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func connectedAuthority(t *testing.T) (*Authority, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	dsn := os.Getenv("L41_POSTGRES_URL")
	if dsn == "" {
		dsn = "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"
	}
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	base := fmt.Sprintf("l41_%d", time.Now().UnixNano())
	metadata := base + "_metadata"
	safety := base + "_safety"
	for _, name := range []string{base, metadata, safety} {
		if _, e = pool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{name}.Sanitize()); e != nil {
			t.Fatal(e)
		}
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		for _, name := range []string{base, metadata, safety} {
			if _, e := pool.Exec(c, "DROP SCHEMA "+pgx.Identifier{name}.Sanitize()+" CASCADE"); e != nil {
				t.Error(e)
			}
		}
	})
	a, e := NewAuthority(pool, base, metadata, safety)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, a.SchemaDDL()); e != nil {
		t.Fatal(e)
	}
	for _, migration := range []struct{ path, from, to string }{
		{"../../../../spec/production-system/integration/migrations/390000.sql", `"metadata"`, pgx.Identifier{metadata}.Sanitize()},
		{"../../../../spec/production-system/integration/migrations/380000.sql", `"chat"`, pgx.Identifier{safety}.Sanitize()},
	} {
		raw, e := os.ReadFile(migration.path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, strings.ReplaceAll(string(raw), migration.from, migration.to)); e != nil {
			t.Fatal(e)
		}
	}
	return a, ctx
}
func seedConnectedAsset(t *testing.T, a *Authority, ctx context.Context) (string, string) {
	t.Helper()
	hash := strings.Repeat("a", 64)
	key := "approved/" + hash + "/256/" + strings.Repeat("b", 64) + ".png"
	token := "0x" + strings.Repeat("c", 40)
	pool := "0x" + strings.Repeat("d", 40)
	if _, e := a.Pool.Exec(ctx, "INSERT INTO "+a.metadata("pools")+"(pool,token,creator) VALUES($1,$2,$3)", pool, token, "0x"+strings.Repeat("e", 40)); e != nil {
		t.Fatal(e)
	}
	if _, e := a.Pool.Exec(ctx, "INSERT INTO "+a.metadata("assets")+"(id,pool,hash,state,object_key,scanner_engine,scanner_revision,size) VALUES('asset',$1,$2,'APPROVED',$3,'clamd','revision',128)", pool, hash, "approved/"+hash); e != nil {
		t.Fatal(e)
	}
	if _, e := a.Pool.Exec(ctx, "INSERT INTO "+a.metadata("documents")+"(pool,moderation_revision) VALUES($1,1)", pool); e != nil {
		t.Fatal(e)
	}
	if _, e := a.Pool.Exec(ctx, "INSERT INTO "+a.safety("safety_cases")+"(id,target_type,target_id,reporter,category,description) VALUES('8a5f44d2-8ee0-42c5-b365-cffdd2e91723','token',$1,$2,'spam','owned qualification fixture')", pool, "0x"+strings.Repeat("e", 40)); e != nil {
		t.Fatal(e)
	}
	if _, e := a.Pool.Exec(ctx, "INSERT INTO "+a.safety("safety_visibility")+"(target_type,target_id,status,revision,case_id) VALUES('token',$1,'restored',1,'8a5f44d2-8ee0-42c5-b365-cffdd2e91723')", pool); e != nil {
		t.Fatal(e)
	}
	if e := a.RegisterLineage(ctx, LineageObject{Hash: hash, Key: key, Kind: "variant", RetainUntil: time.Now().Add(365 * 24 * time.Hour)}); e != nil {
		t.Fatal(e)
	}
	return hash, key
}
func TestConnectedPostgresModerationAndRestore(t *testing.T) {
	a, ctx := connectedAuthority(t)
	hash, key := seedConnectedAsset(t, a, ctx)
	if e := a.Allowed(ctx, key); e != nil {
		t.Fatal(e)
	}
	if _, e := a.Pool.Exec(ctx, "UPDATE "+a.safety("safety_visibility")+" SET status='removed',revision=2"); e != nil {
		t.Fatal(e)
	}
	if e := a.Allowed(ctx, key); !errors.Is(e, ErrRestricted) {
		t.Fatal("moderation not applied", e)
	}
	if _, e := a.Pool.Exec(ctx, "UPDATE "+a.safety("safety_visibility")+" SET status='restored',revision=2"); e != nil {
		t.Fatal(e)
	}
	id, e := a.Restrict(ctx, hash, 2, "deleted")
	if e != nil || id == "" {
		t.Fatal(id, e)
	}
	op, e := a.Operation(ctx, id)
	if e != nil || op.State != "requested" || op.Kind != "delete" {
		t.Fatal(op, e)
	}
	if _, e = a.Pool.Exec(ctx, "UPDATE "+a.table("restrictions")+" SET state='approved',revision=1"); e != nil {
		t.Fatal(e)
	}
	if e = a.Allowed(ctx, key); !errors.Is(e, ErrRestricted) {
		t.Fatal("restored backup resurrected asset", e)
	}
	if e = a.ReplayTombstones(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = a.Restrict(ctx, hash, 3, "approved"); !errors.Is(e, ErrConflict) {
		t.Fatal("tombstone reversed", e)
	}
	if e = a.AuthorizeDownload(ctx, key, 16*time.Minute); !errors.Is(e, ErrRestricted) {
		t.Fatal("long signed link", e)
	}
}
func TestConnectedPurgeStateSurvivesRestart(t *testing.T) {
	a, ctx := connectedAuthority(t)
	hash, key := seedConnectedAsset(t, a, ctx)
	id, e := a.Restrict(ctx, hash, 2, "blocked")
	if e != nil {
		t.Fatal(e)
	}
	fresh, e := NewAuthority(a.Pool, a.Schema, a.MetadataSchema, a.SafetySchema)
	if e != nil {
		t.Fatal(e)
	}
	again, e := fresh.Restrict(ctx, hash, 2, "blocked")
	if e != nil || again != id {
		t.Fatal("operation identity lost", again, e)
	}
	if e = fresh.Allowed(ctx, key); !errors.Is(e, ErrRestricted) {
		t.Fatal("blocked origin allowed", e)
	}
	op, e := fresh.Operation(ctx, id)
	if e != nil || op.State != "requested" || op.Proof != "" {
		t.Fatal("submission manufactured completion", op, e)
	}
}

func TestConnectedAssetSpecificRemovalDeniesBeforeIntake(t *testing.T) {
	a, ctx := connectedAuthority(t)
	_, key := seedConnectedAsset(t, a, ctx)
	if _, e := a.Pool.Exec(ctx, "INSERT INTO "+a.safety("safety_visibility")+"(target_type,target_id,status,revision,case_id) VALUES('asset','asset','removed',2,'8a5f44d2-8ee0-42c5-b365-cffdd2e91723')"); e != nil {
		t.Fatal(e)
	}
	if e := a.Allowed(ctx, key); !errors.Is(e, ErrRestricted) {
		t.Fatal("asset decision bypassed before intake", e)
	}
}

func TestConnectedReplayRebuildsLostWorkWithoutDuplicatingIdentity(t *testing.T) {
	a, ctx := connectedAuthority(t)
	hash, key := seedConnectedAsset(t, a, ctx)
	id, e := a.Restrict(ctx, hash, 2, "deleted")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.Pool.Exec(ctx, "DELETE FROM "+a.table("operations")+" WHERE hash=$1 AND kind='purge'", hash); e != nil {
		t.Fatal(e)
	}
	if _, e = a.Pool.Exec(ctx, "UPDATE "+a.table("restrictions")+" SET state='approved',revision=1 WHERE hash=$1", hash); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if e = a.ReplayTombstones(ctx); e != nil {
			t.Fatal(e)
		}
	}
	var count int
	if e = a.Pool.QueryRow(ctx, "SELECT count(*) FROM "+a.table("operations")+" WHERE hash=$1 AND revision=2", hash).Scan(&count); e != nil || count != 2 {
		t.Fatal(count, e)
	}
	op, e := a.Operation(ctx, id)
	if e != nil || op.State != "requested" || op.ID != id {
		t.Fatal(op, e)
	}
	if e = a.Allowed(ctx, key); !errors.Is(e, ErrRestricted) {
		t.Fatal("restore resurrected removed bytes", e)
	}
}

func TestConnectedRestrictionReplayAndDatabaseInterruption(t *testing.T) {
	a, ctx := connectedAuthority(t)
	hash, key := seedConnectedAsset(t, a, ctx)
	id, e := a.Restrict(ctx, hash, 3, "blocked")
	if e != nil {
		t.Fatal(e)
	}
	for _, state := range []string{"approved", "deleted"} {
		if _, e = a.Restrict(ctx, hash, 2, state); !errors.Is(e, ErrConflict) {
			t.Fatal("out of order decision accepted", state, e)
		}
	}
	if again, e := a.Restrict(ctx, hash, 3, "blocked"); e != nil || again != id {
		t.Fatal("duplicate allocated another operation", again, e)
	}
	interrupted, e := pgxpool.NewWithConfig(ctx, a.Pool.Config())
	if e != nil {
		t.Fatal(e)
	}
	interrupted.Close()
	fresh, e := NewAuthority(interrupted, a.Schema, a.MetadataSchema, a.SafetySchema)
	if e != nil {
		t.Fatal(e)
	}
	if e = fresh.Allowed(ctx, key); !errors.Is(e, ErrUnavailable) {
		t.Fatal("database outage allowed serving", e)
	}
	if _, e = fresh.Restrict(ctx, hash, 4, "deleted"); !errors.Is(e, ErrUnavailable) {
		t.Fatal("database outage claimed mutation", e)
	}
	op, e := a.Operation(ctx, id)
	if e != nil || op.State != "requested" || op.Proof != "" {
		t.Fatal("interrupted mutation altered durable work", op, e)
	}
}
