package devsocial

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/migrate"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/migrations"
)

func TestRuntimeDSNPutsChatFirst(t *testing.T) {
	for in, want := range map[string]string{
		"postgres://u:p@h:5432/db?sslmode=disable":                                   "chat,public",
		"postgres://u:p@h:5432/db?search_path=social_goose%2Cpublic&sslmode=require": "chat,social_goose,public",
		"host=h port=5432 user=u password=p dbname=db sslmode=disable":               "chat,public",
		"host=h user=u dbname=db search_path=social_goose":                           "chat,social_goose",
	} {
		out, err := RuntimeDSN(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		cfg, err := pgconn.ParseConfig(out)
		if err != nil {
			t.Fatalf("%s -> %s: %v", in, out, err)
		}
		if got := cfg.RuntimeParams["search_path"]; got != want {
			t.Fatalf("%s: search_path %q, want %q", in, got, want)
		}
		if cfg.Host != "h" || cfg.Database != "db" {
			t.Fatalf("%s: connection target changed: %s", in, out)
		}
	}
}

// TestMigrationIsRerunnable applies the goose migrations to a fresh fixture
// database, then replays the 00002 Up section: every statement must be a
// no-op against existing objects.
func TestMigrationIsRerunnable(t *testing.T) {
	ctx := context.Background()
	adminDSN := os.Getenv("SOCIAL_FIXTURE_POSTGRES_DSN")
	if adminDSN == "" {
		adminDSN = "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"
	}
	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	name := "social_fleet2_mig_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	}()
	cfg, _ := pgx.ParseConfig(adminDSN)
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", cfg.User, cfg.Password, cfg.Host, cfg.Port, name)
	if err = migrate.Up(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	if err = migrate.Up(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	raw, err := migrations.FS.ReadFile("00002_dev_v2_social.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.SplitN(string(raw), "-- +goose Down", 2)[0]
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err = conn.Exec(ctx, up); err != nil {
		t.Fatalf("replaying 00002 Up: %v", err)
	}
}

// TestRailwayStyleSearchPath boots the migrations the way Railway configures
// the service (search_path=social_goose,public on DATABASE_URL) and checks the
// runtime DSN resolves the unqualified durable-chat tables in "chat".
func TestRailwayStyleSearchPath(t *testing.T) {
	ctx := context.Background()
	adminDSN := os.Getenv("SOCIAL_FIXTURE_POSTGRES_DSN")
	if adminDSN == "" {
		adminDSN = "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"
	}
	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	name := "social_fleet2_sp_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	}()
	cfg, _ := pgx.ParseConfig(adminDSN)
	base := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", cfg.User, cfg.Password, cfg.Host, cfg.Port, name)
	setup, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = setup.Exec(ctx, `CREATE SCHEMA social_goose`); err != nil {
		t.Fatal(err)
	}
	_ = setup.Close(ctx)
	dsn := base + "&search_path=social_goose,public"
	if err = migrate.Up(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	runtime, err := RuntimeDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var schema string
	if err = conn.QueryRow(ctx, `SELECT n.nspname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.oid='chat_messages'::regclass`).Scan(&schema); err != nil || schema != "chat" {
		t.Fatalf("chat_messages resolved to %q: %v", schema, err)
	}
	if err = conn.QueryRow(ctx, `SELECT n.nspname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.oid='goose_db_version'::regclass`).Scan(&schema); err != nil || schema != "social_goose" {
		t.Fatalf("goose_db_version resolved to %q: %v", schema, err)
	}
}
