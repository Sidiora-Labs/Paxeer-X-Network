package db

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPoolTag(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"postgres://u:p@host:5432/mydb?sslmode=disable": "mydb",
		"postgres://u:p@host:5432/":                     "default",
		"postgres://u:p@host:5432":                      "default",
		"://bad":                                        "default",
	}
	for dsn, want := range cases {
		if got := poolTag(dsn); got != want {
			t.Errorf("poolTag(%q) = %q, want %q", dsn, got, want)
		}
	}
}

func TestNewPoolBadDSN(t *testing.T) {
	t.Parallel()
	if _, err := NewPool(context.Background(), "not-a-valid-dsn://", PoolOptions{}); err == nil {
		t.Fatal("want error for bad dsn")
	}
}

func TestNewPoolIntegration(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("L07_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("L07_POSTGRES_DSN is not set: point it at a running PostgreSQL database named kindle_test; tests never start containers")
	}

	pool, err := NewPool(ctx, dsn, PoolOptions{MaxConns: 5, MinConns: 1, StatementTimeout: 3 * time.Second})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()
	defer unregister(poolTag(dsn))

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, "select 1").Scan(&n); err != nil || n != 1 {
		t.Fatalf("select 1 = %d, err %v", n, err)
	}

	m := PoolMetrics()
	if _, ok := m["kindle_test"]; !ok {
		t.Errorf("PoolMetrics missing tag kindle_test, got %v", m)
	}
}

func TestPoolOptionsSafety(t *testing.T) {
	for _, o := range []PoolOptions{{MaxConns: -1}, {MaxConns: 1, MinConns: 2}, {QueryClass: "unbounded"}, {QueryClass: Interactive, StatementTimeout: time.Second}, {QueryClass: Migration, LockTimeout: 3 * time.Second}, {StatementTimeout: time.Nanosecond}, {Service: "dsn:password", Instance: "instance"}, {Service: "service"}} {
		if err := o.Validate(); err == nil {
			t.Errorf("unsafe options accepted: %+v", o)
		}
	}
	for _, o := range []PoolOptions{{}, {QueryClass: Interactive}, {QueryClass: Batch}, {QueryClass: Migration}} {
		if err := o.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	statement, lock := (PoolOptions{QueryClass: Interactive}).Deadlines()
	if statement != 350*time.Millisecond || lock != 100*time.Millisecond {
		t.Fatal(statement, lock)
	}
	statement, lock = (PoolOptions{QueryClass: Migration}).Deadlines()
	if statement != 5*time.Minute || lock != 2*time.Second {
		t.Fatal(statement, lock)
	}
}

func TestPhase2QueryObservationOmitsParameters(t *testing.T) {
	dsn := os.Getenv("L07_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("real phase2 PostgreSQL required")
	}
	ctx := context.Background()
	options := PoolOptions{MaxConns: 1, QueryClass: Interactive, StatementTimeout: 20 * time.Millisecond, Service: "phase2_query", Instance: "fixture"}
	pool, err := NewPool(ctx, dsn, options)
	if err != nil {
		t.Fatal(err)
	}
	defer ClosePool(pool)
	fingerprint := StatementFingerprint("SELECT pg_sleep($1)")
	var observed QueryObservation
	err = WithObservedTransaction(ctx, pool, options, pgx.TxOptions{}, fingerprint, func(value QueryObservation) { observed = value }, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, "SELECT pg_sleep($1)", 1); return err })
	var postgres *pgconn.PgError
	if !errors.As(err, &postgres) || postgres.Code != "57014" || observed.Outcome != "statement_timeout" || observed.Fingerprint != fingerprint || observed.Service != "phase2_query" || observed.Elapsed > time.Second {
		t.Fatal("actual timeout observation differs", observed, err)
	}
	if strings.Contains(observed.Fingerprint, "pg_sleep") || len(observed.Fingerprint) != 64 {
		t.Fatal("raw statement escaped observation")
	}
	recovery, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err = pool.Ping(recovery); err != nil {
		t.Fatal("canceled transaction connection did not recover", err)
	}
}
