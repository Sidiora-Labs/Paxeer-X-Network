package production

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func phase2MigrationFixture(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	admin := fixture(t)
	ctx := context.Background()
	database := fmt.Sprintf("l07_phase2_%d", time.Now().UnixNano())
	exec(t, admin, "CREATE DATABASE "+pgx.Identifier{database}.Sanitize())
	parsed, err := url.Parse(os.Getenv("L07_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + database
	dsn := parsed.String()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, "DROP DATABASE "+pgx.Identifier{database}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error("private migration database cleanup failed", err)
		}
	})
	for _, role := range []string{"migration_control_owner", "stats_owner"} {
		var exists bool
		if err = admin.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)", role).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			exec(t, admin, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS")
		}
	}
	ddl := MigrationControlDDL()
	ddl = ddl[strings.Index(ddl, "\n")+1:]
	exec(t, pool, ddl)
	exec(t, pool, OnlineIndexControlDDL())
	exec(t, pool, "CREATE SCHEMA stats AUTHORIZATION stats_owner")
	history, err := HistoryDDL("core/stats-workers", "stats")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, pool, history)
	return pool, dsn
}
func phase2RegisteredMigration(id int64, sql string, previous []int64) MigrationManifest {
	m := migration([]byte(sql))
	m.MigrationID = id
	m.PredecessorIDs = previous
	m.Path = fmt.Sprintf("phase2/%d.sql", id)
	return m
}
func TestPhase2ConcurrentIndexesAndRecovery(t *testing.T) {
	pool, dsn := phase2MigrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	baselineSQL := `CREATE TABLE stats.phase2_indexed(id bigint NOT NULL,value text NOT NULL);INSERT INTO stats.phase2_indexed VALUES(1,'one'),(1,'duplicate'),(2,'two');`
	baseline := phase2RegisteredMigration(70001, baselineSQL, nil)
	plan := OnlineIndexPlan{Schema: "stats", Table: "phase2_indexed", Name: "phase2_id_unique", Columns: []string{"id"}, Unique: true}
	indexSQL, err := plan.CreateSQL()
	if err != nil {
		t.Fatal(err)
	}
	index := phase2RegisteredMigration(70002, indexSQL, []int64{70001})
	index.TransactionMode = "nontransactional"
	registry := map[int64]MigrationManifest{baseline.MigrationID: baseline, index.MigrationID: index}
	if err = ApplyRegisteredMigration(ctx, conn, baseline, []byte(baselineSQL), registry, baseline.InterfaceDigest, baseline.ApprovalManifestDigest, 1); err != nil {
		t.Fatal(err)
	}
	lock := PostgresCoordinatorLock{Pool: pool, Key: CoordinatorKey, AcquireTimeout: time.Second}
	lease, err := lock.TryLock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = ApplyOnlineIndexMigration(ctx, conn, index, plan, registry, index.InterfaceDigest, index.ApprovalManifestDigest, 1, nil); !errors.Is(err, ErrMigrationInProgress) {
		t.Fatal("second coordinator was admitted", err)
	}
	var lockCount int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND granted AND objid=$1", CoordinatorKey).Scan(&lockCount); err != nil || lockCount != 1 {
		t.Fatal("actual coordinator lock catalog differs", lockCount, err)
	}
	if err = lease.Release(ctx); err != nil {
		t.Fatal(err)
	}
	err = ApplyOnlineIndexMigration(ctx, conn, index, plan, registry, index.InterfaceDigest, index.ApprovalManifestDigest, 1, nil)
	sqlState(t, err, "23505")
	state, err := InspectOnlineIndex(ctx, conn, plan)
	if err != nil || !state.Exists || state.Valid {
		t.Fatal("failed unique concurrent index was not observed invalid", state, err)
	}
	var status, failure string
	if err = pool.QueryRow(ctx, "SELECT state,failure_class FROM migration_control.migration_attempts WHERE migration_id=$1 ORDER BY attempt DESC LIMIT 1", index.MigrationID).Scan(&status, &failure); err != nil || status != "failed" || failure != "23505" {
		t.Fatal(status, failure, err)
	}
	if err = ApplyOnlineIndexMigration(ctx, conn, index, plan, registry, index.InterfaceDigest, index.ApprovalManifestDigest, 1, nil); !errors.Is(err, ErrInvalidOnlineIndex) {
		t.Fatal("invalid index repaired without explicit authority", err)
	}
	exec(t, pool, "DELETE FROM stats.phase2_indexed WHERE value='duplicate'")
	authority, err := json.Marshal(IndexRepairRegistry{Version: 1, Approvals: []IndexRepairApproval{{MigrationID: index.MigrationID, Schema: plan.Schema, Index: plan.Name, Operation: "drop_recreate", FailedChecksum: index.SHA256, FailureClass: "23505"}}})
	if err != nil {
		t.Fatal(err)
	}
	repairs, err := LoadIndexRepairRegistry(authority, Checksum(authority))
	if err != nil {
		t.Fatal(err)
	}
	if err = ApplyOnlineIndexMigration(ctx, conn, index, plan, registry, index.InterfaceDigest, index.ApprovalManifestDigest, 1, &repairs); err != nil {
		t.Fatal(err)
	}
	state, err = InspectOnlineIndex(ctx, conn, plan)
	if err != nil || !state.Valid || !state.Ready {
		t.Fatal(state, err)
	}
	if err = pool.QueryRow(ctx, "SELECT state FROM migration_control.migration_attempts WHERE migration_id=$1 ORDER BY attempt DESC LIMIT 1", index.MigrationID).Scan(&status); err != nil || status != "reconciled" {
		t.Fatal(status, err)
	}
	blockedPlan := OnlineIndexPlan{Schema: "stats", Table: "phase2_indexed", Name: "phase2_value_index", Columns: []string{"value"}}
	blockedSQL, err := blockedPlan.CreateSQL()
	if err != nil {
		t.Fatal(err)
	}
	blocked := phase2RegisteredMigration(70003, blockedSQL, []int64{70002})
	blocked.TransactionMode = "nontransactional"
	registry[blocked.MigrationID] = blocked
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	exec(t, blocker, "LOCK TABLE stats.phase2_indexed IN ACCESS EXCLUSIVE MODE")
	start := time.Now()
	err = ApplyOnlineIndexMigration(ctx, conn, blocked, blockedPlan, registry, blocked.InterfaceDigest, blocked.ApprovalManifestDigest, 1, nil)
	elapsed := time.Since(start)
	sqlState(t, err, "55P03")
	if elapsed < 1900*time.Millisecond || elapsed > 3*time.Second {
		t.Fatal("two-second DDL lock deadline differs", elapsed)
	}
	var live int
	if err = pool.QueryRow(ctx, "SELECT 1").Scan(&live); err != nil || live != 1 {
		t.Fatal("independent interactive query interrupted", err)
	}
	t.Logf("coordinator_second=migration_in_progress invalid_index=true repaired_state=reconciled lock_sqlstate=55P03 lock_wait=%s", elapsed)
}
func TestPhase2MigrationRejectsUnapprovedBeforeDDL(t *testing.T) {
	pool, dsn := phase2MigrationFixture(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	exec(t, pool, "SET ROLE stats_owner;CREATE TABLE stats.phase2_unapproved(id bigint);RESET ROLE")
	plan := OnlineIndexPlan{Schema: "stats", Table: "phase2_unapproved", Name: "phase2_unapproved_idx", Columns: []string{"id"}}
	sql, err := plan.CreateSQL()
	if err != nil {
		t.Fatal(err)
	}
	m := phase2RegisteredMigration(70005, sql, []int64{70004})
	m.TransactionMode = "nontransactional"
	registry := map[int64]MigrationManifest{m.MigrationID: m}
	if err = ApplyOnlineIndexMigration(ctx, conn, m, plan, registry, m.InterfaceDigest, m.ApprovalManifestDigest, 1, nil); err == nil {
		t.Fatal("unknown predecessor reached DDL")
	}
	state, err := InspectOnlineIndex(ctx, conn, plan)
	if err != nil || state.Exists {
		t.Fatal("unapproved predecessor created index", state, err)
	}
	m.PredecessorIDs = nil
	registry[m.MigrationID] = m
	changed := m
	changed.SHA256 = Checksum([]byte("changed"))
	if err = ApplyOnlineIndexMigration(ctx, conn, changed, plan, registry, m.InterfaceDigest, m.ApprovalManifestDigest, 1, nil); err == nil {
		t.Fatal("checksum drift reached DDL")
	}
	state, err = InspectOnlineIndex(ctx, conn, plan)
	if err != nil || state.Exists {
		t.Fatal("checksum drift created index", state, err)
	}
}

func TestPhase3DDLContentionPreservesServingQueries(t *testing.T) {
	pool, dsn := phase2MigrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	exec(t, pool, "SET ROLE stats_owner;CREATE TABLE stats.phase3_serving(id bigint PRIMARY KEY);INSERT INTO stats.phase3_serving VALUES(1);RESET ROLE")
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	exec(t, blocker, "LOCK TABLE stats.phase3_serving IN ACCESS SHARE MODE")
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	sql := "ALTER TABLE stats.phase3_serving ADD COLUMN expanded text"
	m := phase2RegisteredMigration(70006, sql, nil)
	registry := map[int64]MigrationManifest{m.MigrationID: m}
	migrationDone := make(chan error, 1)
	started := time.Now()
	go func() {
		migrationDone <- ApplyCoordinatedMigration(ctx, conn, m, []byte(sql), registry, m.InterfaceDigest, m.ApprovalManifestDigest, 1)
	}()
	var waiting bool
	deadline := time.Now().Add(time.Second)
	for !waiting {
		if err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted AND mode='AccessExclusiveLock')", conn.PgConn().PID()).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if !waiting && time.Now().After(deadline) {
			t.Fatal("DDL did not encounter the actual serving lock")
		}
		if !waiting {
			time.Sleep(5 * time.Millisecond)
		}
	}
	served := 0
	for {
		select {
		case err = <-migrationDone:
			sqlState(t, err, "55P03")
			elapsed := time.Since(started)
			if elapsed < 1900*time.Millisecond || elapsed > 3*time.Second || served < 10 {
				t.Fatal("DDL lock bound or live query continuity differs", elapsed, served)
			}
			var count int
			if err = blocker.QueryRow(ctx, "SELECT count(*) FROM stats.phase3_serving").Scan(&count); err != nil || count != 1 {
				t.Fatal("serving transaction terminated", err)
			}
			if err = blocker.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if err = ApplyCoordinatedMigration(ctx, conn, m, []byte(sql), registry, m.InterfaceDigest, m.ApprovalManifestDigest, 1); err != nil {
				t.Fatal("retry outside user transaction failed", err)
			}
			if err = ApplyCoordinatedMigration(ctx, conn, m, []byte(sql), registry, m.InterfaceDigest, m.ApprovalManifestDigest, 1); err == nil {
				t.Fatal("duplicate migration admitted")
			}
			var applied, failed int
			if err = pool.QueryRow(ctx, "SELECT count(*) FILTER(WHERE state='applied'),count(*) FILTER(WHERE state='failed' AND failure_class='55P03') FROM migration_control.migration_attempts WHERE migration_id=$1", m.MigrationID).Scan(&applied, &failed); err != nil || applied != 1 || failed != 1 {
				t.Fatal("migration attempt outcomes differ", applied, failed, err)
			}
			t.Logf("lock_wait=%s serving_queries=%d applied=%d failed=%d", elapsed, served, applied, failed)
			return
		default:
			var now time.Time
			if err = pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
				t.Fatal("interactive traffic terminated", err)
			}
			served++
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestPhase3InterruptedMigrationRestartsWithoutPartialDDL(t *testing.T) {
	pool, dsn := phase2MigrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	sql := "CREATE TABLE stats.phase3_interrupted(id bigint);SELECT pg_sleep(5)"
	m := phase2RegisteredMigration(70007, sql, nil)
	registry := map[int64]MigrationManifest{m.MigrationID: m}
	finished := make(chan error, 1)
	pid := conn.PgConn().PID()
	go func() {
		finished <- ApplyCoordinatedMigration(ctx, conn, m, []byte(sql), registry, m.InterfaceDigest, m.ApprovalManifestDigest, 1)
	}()
	var sleeping bool
	deadline := time.Now().Add(time.Second)
	for !sleeping {
		if err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event='PgSleep')", pid).Scan(&sleeping); err != nil {
			t.Fatal(err)
		}
		if !sleeping && time.Now().After(deadline) {
			t.Fatal("migration did not enter real transaction")
		}
		if !sleeping {
			time.Sleep(5 * time.Millisecond)
		}
	}
	var killed bool
	if err = pool.QueryRow(ctx, "SELECT pg_terminate_backend($1)", pid).Scan(&killed); err != nil || !killed {
		t.Fatal("coordinator interruption failed", err)
	}
	if err = <-finished; err == nil {
		t.Fatal("interrupted migration returned success")
	}
	var table *string
	if err = pool.QueryRow(ctx, "SELECT to_regclass('stats.phase3_interrupted')::text").Scan(&table); err != nil || table != nil {
		t.Fatal("interrupted migration left DDL", table, err)
	}
	fresh, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close(context.Background())
	if err = ApplyCoordinatedMigration(ctx, fresh, m, []byte(sql), registry, m.InterfaceDigest, m.ApprovalManifestDigest, 1); err != nil {
		t.Fatal("restart failed", err)
	}
	var applied int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM migration_control.applied_migrations WHERE migration_id=$1", m.MigrationID).Scan(&applied); err != nil || applied != 1 {
		t.Fatal("restart applied duplicate business DDL", applied, err)
	}
}
