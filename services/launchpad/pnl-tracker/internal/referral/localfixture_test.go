package referral_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/migrate"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Use an exact owned database in the authorized shared local fixture when set;
// otherwise fall back to the PNL_TEST_POSTGRES_DSN throwaway-database helper.
func referralFixture(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("L18_POSTGRES_DSN")
	if dsn == "" {
		return internaltest.NewPostgres(t)
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	suffix := make([]byte, 4)
	if _, e = rand.Read(suffix); e != nil {
		t.Fatal(e)
	}
	name := "kd_5baa0fca6c0fcb137ad0885e_ref_" + hex.EncodeToString(suffix)
	if _, e = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); e != nil {
		admin.Close()
		t.Fatal(e)
	}
	var pool *pgxpool.Pool
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		if _, e := admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()); e != nil {
			t.Error(e)
		}
		admin.Close()
	})
	u, e := url.Parse(dsn)
	if e != nil {
		t.Fatal(e)
	}
	u.Path = "/" + name
	if e = migrate.Up(ctx, u.String()); e != nil {
		t.Fatal(e)
	}
	pool, e = pgxpool.New(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	return pool
}
