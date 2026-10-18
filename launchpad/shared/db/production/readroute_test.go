package production

import (
	"context"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWALPositionBoundaries(t *testing.T) {
	for _, value := range []string{"0/0", "1/FFFFFFFF", "FFFFFFFF/FFFFFFFF"} {
		position, err := ParseWALPosition(value)
		if err != nil || position.String() != value {
			t.Fatal(value, position, err)
		}
	}
	for _, value := range []string{"-1/0", "100000000/0", "1/100000000", "1/2/3", "1e3", " /0", "1/+1"} {
		if _, err := ParseWALPosition(value); err == nil {
			t.Fatal("invalid WAL position admitted", value)
		}
	}
}
func phase2AdmittedPool(t *testing.T, dsn, role, password, poolID string, max int32) AdmittedReadPool {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.User = url.UserPassword(role, password)
	config, err := pgxpool.ParseConfig(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	options := db.PoolOptions{Transport: db.TransportDirect, PoolID: poolID, BudgetIdentity: "phase2_fixture", DatabaseAlias: config.ConnConfig.Database, DatabaseUser: config.ConnConfig.User, DatabaseHost: config.ConnConfig.Host, DatabasePort: config.ConnConfig.Port, MaxConns: max, AcquireTimeout: 50 * time.Millisecond, QueryClass: db.Interactive, Service: "route", Instance: "candidate"}
	pool, err := db.NewPool(context.Background(), parsed.String(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.ClosePool(pool) })
	return AdmittedReadPool{pool, options}
}
func TestPhase2PausedReplicaOwnershipAndAdmission(t *testing.T) {
	primary := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	replicaDSN := os.Getenv("L07_REPLICA_DSN")
	password := os.Getenv("L07_READER_PASSWORD")
	if replicaDSN == "" || password == "" {
		t.Fatal("real physical standby and distinct runtime password required")
	}
	replica, err := pgxpool.New(ctx, replicaDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer replica.Close()
	exec(t, primary, `CREATE SCHEMA l07_route;CREATE TABLE l07_route.ownership(asset text PRIMARY KEY,owner text NOT NULL);INSERT INTO l07_route.ownership VALUES('asset','old_owner');CREATE ROLE l07_route_reader LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;GRANT USAGE ON SCHEMA l07_route TO l07_route_reader;GRANT SELECT(asset,owner) ON l07_route.ownership TO l07_route_reader`)
	var alterRole string
	if err = primary.QueryRow(ctx, "SELECT format('ALTER ROLE %I PASSWORD %L',$1::text,$2::text)", "l07_route_reader", password).Scan(&alterRole); err != nil {
		t.Fatal(err)
	}
	exec(t, primary, alterRole)
	var baseline string
	if err = primary.QueryRow(ctx, "SELECT pg_current_wal_lsn()::text").Scan(&baseline); err != nil {
		t.Fatal(err)
	}
	waitReplay := func(position string) {
		t.Helper()
		until := time.Now().Add(5 * time.Second)
		for {
			var ready bool
			if err = replica.QueryRow(ctx, "SELECT pg_is_in_recovery() AND pg_last_wal_replay_lsn()>=$1::pg_lsn", position).Scan(&ready); err != nil {
				t.Fatal(err)
			}
			if ready {
				return
			}
			if time.Now().After(until) {
				t.Fatal("physical standby did not replay required commit")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitReplay(baseline)
	primaryRead := phase2AdmittedPool(t, os.Getenv("L07_POSTGRES_DSN"), "l07_route_reader", password, "primary", 1)
	replicaRead := phase2AdmittedPool(t, replicaDSN, "l07_route_reader", password, "replica", 1)
	router, err := NewReadRouter(primaryRead, replicaRead, 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, replica, "SELECT pg_wal_replay_pause()")
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		replica.Exec(cleanup, "SELECT pg_wal_replay_resume()")
	}()
	var paused bool
	pauseDeadline := time.Now().Add(2 * time.Second)
	for !paused {
		if err = replica.QueryRow(ctx, "SELECT pg_get_wal_replay_pause_state()='paused'").Scan(&paused); err != nil {
			t.Fatal(err)
		}
		if !paused {
			if time.Now().After(pauseDeadline) {
				t.Fatal("standby replay was not actually paused")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	exec(t, primary, "UPDATE l07_route.ownership SET owner='current_owner' WHERE asset='asset'")
	var written string
	if err = primary.QueryRow(ctx, "SELECT pg_current_wal_lsn()::text").Scan(&written); err != nil {
		t.Fatal(err)
	}
	required, err := ParseWALPosition(written)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	ownerRead := func(want, target string, stale bool) func(pgx.Tx, ReadObservation) error {
		return func(tx pgx.Tx, observation ReadObservation) error {
			var owner string
			if err := tx.QueryRow(ctx, "SELECT owner FROM l07_route.ownership WHERE asset=$1", "asset").Scan(&owner); err != nil {
				return err
			}
			if owner != want || observation.Target != target || observation.Stale != stale {
				t.Fatalf("owner=%s target=%s stale=%v", owner, observation.Target, observation.Stale)
			}
			t.Logf("class=%s target=%s observed_lsn=%s stale=%v fallback=%v replay_delay=%s", observation.Class, observation.Target, observation.Watermark, observation.Stale, observation.Fallback, observation.ReplayDelay)
			return nil
		}
	}
	if err = router.WithOwnershipRead(ctx, ownerRead("current_owner", "primary", false)); err != nil {
		t.Fatal(err)
	}
	if err = router.WithRead(ctx, ReadRequest{Class: BoundedStaleRead}, ownerRead("old_owner", "replica", true)); err != nil {
		t.Fatal(err)
	}
	if err = router.WithRead(ctx, ReadRequest{Class: ReadYourWrite, RequiredPosition: required}, ownerRead("current_owner", "primary", false)); err != nil {
		t.Fatal(err)
	}
	hold, err := db.AcquireBounded(ctx, primaryRead.Pool, primaryRead.Options)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	called := false
	err = router.WithRead(ctx, ReadRequest{Class: ReadYourWrite, RequiredPosition: required}, func(pgx.Tx, ReadObservation) error { called = true; return nil })
	hold.Release()
	if !errors.Is(err, ErrReadUnavailable) || called || time.Since(start) > time.Second {
		t.Fatal("replica fallback exceeded bounded primary admission", err, time.Since(start))
	}
	metrics := router.Metrics()
	if metrics[ReadYourWrite].Fallbacks != 2 || metrics[ReadYourWrite].Unavailable != 1 || metrics[BoundedStaleRead].ReplayDelay <= 20*time.Millisecond {
		t.Fatal(metrics)
	}
	if err = router.WithPrimaryMutation(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE l07_route.ownership SET owner='forbidden'")
		return err
	}); err == nil {
		t.Fatal("read-only runtime role acquired write privilege")
	} else {
		sqlState(t, err, "42501")
	}
	exec(t, replica, "SELECT pg_wal_replay_resume()")
	waitReplay(written)
	if err = router.WithRead(ctx, ReadRequest{Class: ReadYourWrite, RequiredPosition: required}, func(tx pgx.Tx, observation ReadObservation) error {
		var owner string
		if err := tx.QueryRow(ctx, "SELECT owner FROM l07_route.ownership WHERE asset=$1", "asset").Scan(&owner); err != nil {
			return err
		}
		if owner != "current_owner" || observation.Target != "replica" || observation.Fallback {
			t.Fatal("caught-up read-your-write snapshot differs", owner, observation)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Logf("paused=true primary_write_lsn=%s replay_lsn_before=%s bounded_fallbacks=%d", written, baseline, metrics[ReadYourWrite].Fallbacks)
}
