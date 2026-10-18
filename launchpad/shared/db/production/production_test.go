package production

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func manifest() RoleManifest {
	return RoleManifest{Service: "stats", Schema: "l07_stats", OwnerRole: "l07_stats_owner", RuntimeRole: "l07_stats_runtime", Grants: []Grant{{Schema: "l07_stats", Object: "balances", Privileges: []string{"SELECT", "INSERT", "UPDATE", "DELETE"}}, {Schema: "l07_indexer", Object: "events_v1", Privileges: []string{"SELECT"}, View: true, Columns: []string{"id"}}}}
}
func TestRoleManifestRejectsEscalation(t *testing.T) {
	m := manifest()
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []func(*RoleManifest){func(m *RoleManifest) { m.Grants[1].Privileges = []string{"INSERT"} }, func(m *RoleManifest) { m.Grants[1].View = false }, func(m *RoleManifest) { m.Grants[1].Columns = nil }, func(m *RoleManifest) { m.Grants[1].Object = "events" }, func(m *RoleManifest) { m.Grants[0].Privileges = []string{"CREATE"} }, func(m *RoleManifest) { m.Grants[0].GrantOption = true }, func(m *RoleManifest) { m.RuntimeRole = m.OwnerRole }, func(m *RoleManifest) { m.Superuser = true }, func(m *RoleManifest) { m.BypassRLS = true }, func(m *RoleManifest) { m.Grants[1].Columns = []string{"id); DROP TABLE events;--"} }, func(m *RoleManifest) { m.RuntimeOwnsObjects = true }}
	for i, mutate := range cases {
		m := manifest()
		mutate(&m)
		if _, err := m.GrantSQL(); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
}
func TestFleetAdmission(t *testing.T) {
	if err := ValidateFleet(100, 10, []Allocation{{"interactive", 5, 10}, {"batch", 2, 10}}); err != nil {
		t.Fatal(err)
	}
	for _, a := range [][]Allocation{{{"interactive", 5, 10}, {"batch", 3, 10}}, {{"overflow", math.MaxInt64, math.MaxInt64}}, {{"a", 1, 1}, {"a", 1, 1}}} {
		if ValidateFleet(100, 10, a) == nil {
			t.Fatal("unsafe fleet accepted")
		}
	}
	if ValidateFleet(4, 4, nil) == nil {
		t.Fatal("reserve rounded away")
	}
	if err := ValidateFleet(math.MaxInt64, 0, nil); err != nil {
		t.Fatal(err)
	}
}
func migration(sql []byte) MigrationManifest {
	return MigrationManifest{MigrationID: 70000, Service: "core/stats-workers", Schema: "stats", OwnerLane: "L07", Path: "proposed.sql", SHA256: Checksum(sql), PredecessorIDs: []int64{140000}, InterfaceDigest: Checksum([]byte("interface")), ReaderMinVersion: 1, EstimatedLockMS: 2000, TransactionMode: "transactional", RollbackStrategy: "additive; leave schema", BackfillStrategy: "not required", ApprovalManifestDigest: Checksum([]byte("approval"))}
}
func TestMigrationSafety(t *testing.T) {
	sql := []byte("ALTER TABLE stats.pool_stats ADD COLUMN extra text")
	m := migration(sql)
	p := MigrationManifest{MigrationID: 140000, SHA256: Checksum([]byte("predecessor"))}
	registry := map[int64]MigrationManifest{140000: p, m.MigrationID: m}
	applied := map[int64]string{140000: p.SHA256}
	validate := func(m MigrationManifest) error {
		return m.Validate(sql, registry, applied, Checksum([]byte("interface")), Checksum([]byte("approval")), 1)
	}
	if err := validate(m); err != nil {
		t.Fatal(err)
	} // numerical ID order is not dependency order.
	cases := []func(*MigrationManifest){func(m *MigrationManifest) { m.SHA256 = Checksum([]byte("changed")) }, func(m *MigrationManifest) { m.PredecessorIDs = []int64{1} }, func(m *MigrationManifest) { m.PredecessorIDs = []int64{70000} }, func(m *MigrationManifest) { m.PredecessorIDs = []int64{140000, 140000} }, func(m *MigrationManifest) { m.Destructive = true }, func(m *MigrationManifest) { m.ReaderMinVersion = 2 }, func(m *MigrationManifest) { m.MigrationID = 80000 }, func(m *MigrationManifest) { m.TransactionMode = "auto" }, func(m *MigrationManifest) { m.EstimatedLockMS = 2001 }, func(m *MigrationManifest) { m.ApprovalManifestDigest = Checksum([]byte("other")) }}
	for i, mutate := range cases {
		m := migration(sql)
		mutate(&m)
		if validate(m) == nil {
			t.Errorf("unsafe migration %d accepted", i)
		}
	}
	delete(registry, m.MigrationID)
	if validate(m) == nil {
		t.Fatal("unregistered current migration accepted")
	}
	registry[m.MigrationID] = m
	for name, mutate := range map[string]func(*MigrationManifest){
		"remove predecessor":    func(m *MigrationManifest) { m.PredecessorIDs = nil },
		"destructive downgrade": func(m *MigrationManifest) { m.Destructive = false },
		"rollback strategy":     func(m *MigrationManifest) { m.RollbackStrategy = "unapproved" },
		"lock estimate":         func(m *MigrationManifest) { m.EstimatedLockMS = 100 },
		"transaction mode":      func(m *MigrationManifest) { m.TransactionMode = "nontransactional" },
		"backfill strategy":     func(m *MigrationManifest) { m.BackfillStrategy = "unapproved" },
		"reader version":        func(m *MigrationManifest) { m.ReaderMinVersion = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			approved := migration(sql)
			approved.Destructive = true
			approved.ReaderCompatibilityProof = Checksum([]byte("reader proof"))
			approved.ReaderMinVersion = 2
			candidate := approved
			mutate(&candidate)
			approvedRegistry := map[int64]MigrationManifest{140000: p, approved.MigrationID: approved}
			if candidate.Validate(sql, approvedRegistry, applied, approved.InterfaceDigest, approved.ApprovalManifestDigest, 2) == nil {
				t.Fatal("altered registered migration accepted")
			}
		})
	}
	replacementSQL := []byte("DROP TABLE stats.pool_stats")
	replacement := m
	replacement.SHA256 = Checksum(replacementSQL)
	if replacement.Validate(replacementSQL, registry, applied, m.InterfaceDigest, m.ApprovalManifestDigest, 1) == nil {
		t.Fatal("recomputed SQL checksum with copied approval accepted")
	}
	applied[140000] = Checksum([]byte("drift"))
	if validate(m) == nil {
		t.Fatal("predecessor drift accepted")
	}
	for _, bad := range [][2]string{{"core/indexer", "stats"}, {"core/indexer", "\"indexer\""}, {"unknown", "indexer"}} {
		if _, err := HistoryTable(bad[0], bad[1]); err == nil {
			t.Fatal("unregistered history")
		}
	}
}
func fixture(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("L07_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("real PostgreSQL required: run tools/storage/check.py")
	}
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	if err := p.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	return p
}
func exec(t *testing.T, q interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, sql string, args ...any) {
	t.Helper()
	if _, err := q.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}
func sqlState(t *testing.T, err error, want string) {
	t.Helper()
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != want {
		t.Fatalf("want SQLSTATE %s, got %v", want, err)
	}
}

func TestPostgresRoleAndNumericBoundaries(t *testing.T) {
	p := fixture(t)
	ctx := context.Background()
	exec(t, p, `CREATE ROLE l07_stats_owner NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
 CREATE ROLE l07_stats_runtime NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
 CREATE ROLE l07_indexer_owner NOLOGIN;
 REVOKE CREATE ON SCHEMA public FROM PUBLIC;
 CREATE SCHEMA l07_stats AUTHORIZATION l07_stats_owner;
 CREATE SCHEMA l07_indexer AUTHORIZATION l07_indexer_owner;
 CREATE TABLE l07_stats.balances (amount numeric(78,0) NOT NULL CHECK (amount >= 0 AND amount <= 115792089237316195423570985008687907853269984665640564039457584007913129639935));
 ALTER TABLE l07_stats.balances OWNER TO l07_stats_owner;
 CREATE TABLE l07_indexer.events (id bigint PRIMARY KEY, payload text NOT NULL);
 ALTER TABLE l07_indexer.events OWNER TO l07_indexer_owner;
 CREATE VIEW l07_indexer.events_v1 AS SELECT id FROM l07_indexer.events;
 INSERT INTO l07_indexer.events VALUES (1,'unchanged')`)
	grants, err := manifest().GrantSQL()
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range grants {
		exec(t, p, sql)
	}
	conn, err := p.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	exec(t, conn, "SET ROLE l07_stats_runtime")
	_, err = conn.Exec(ctx, "INSERT INTO l07_indexer.events VALUES (2,'forbidden')")
	sqlState(t, err, "42501")
	var n int
	exec(t, conn, "RESET ROLE")
	if err = conn.QueryRow(ctx, "SELECT count(*) FROM l07_indexer.events").Scan(&n); err != nil || n != 1 {
		t.Fatalf("forbidden write changed rows: %d %v", n, err)
	}
	exec(t, conn, "SET ROLE l07_stats_runtime")
	for _, sql := range []string{"CREATE TABLE l07_stats.evil (id int)", "ALTER TABLE l07_stats.balances ADD COLUMN evil int", "DROP TABLE l07_stats.balances", "CREATE TABLE public.evil(id int)", "SELECT * FROM l07_indexer.events"} {
		_, err = conn.Exec(ctx, sql)
		sqlState(t, err, "42501")
	}
	if err = conn.QueryRow(ctx, "SELECT id FROM l07_indexer.events_v1").Scan(&n); err != nil || n != 1 {
		t.Fatal("versioned view unavailable", err)
	}
	for _, s := range []string{"9", "100", "1000", db.Uint256Max} {
		if err = db.ValidateRawUint256(s); err != nil {
			t.Fatal(err)
		}
		exec(t, conn, "INSERT INTO l07_stats.balances VALUES ($1::numeric)", s)
	}
	rows, err := conn.Query(ctx, "SELECT amount::text FROM l07_stats.balances AS balances ORDER BY balances.amount DESC")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		got = append(got, s)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{db.Uint256Max, "1000", "100", "9"}) {
		t.Fatal(got)
	}
	sorted, err := SortRawBalances(ctx, conn, []string{"9", "100", "1000"})
	if err != nil || !reflect.DeepEqual(sorted, []string{"1000", "100", "9"}) {
		t.Fatalf("numeric helper: %v %v", sorted, err)
	}
	if _, err = SortRawBalances(ctx, conn, []string{"1e3"}); err == nil {
		t.Fatal("exponent accepted")
	}
	exec(t, conn, "RESET ROLE")
}

func TestPostgresCoordinatorAndCancellation(t *testing.T) {
	p := fixture(t)
	ctx := context.Background()
	lock := PostgresCoordinatorLock{Pool: p, Key: 70000, AcquireTimeout: time.Second}
	lease, err := lock.TryLock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lock.TryLock(ctx); !errors.Is(err, ErrMigrationInProgress) {
		t.Fatal("second coordinator", err)
	}
	if err = lease.Release(ctx); err != nil {
		t.Fatal(err)
	}
	lease, err = lock.TryLock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = lease.Release(ctx); err != nil {
		t.Fatal(err)
	}
	dsn := os.Getenv("L07_POSTGRES_DSN")
	opts := db.PoolOptions{MaxConns: 1, QueryClass: db.Interactive, AcquireTimeout: 50 * time.Millisecond, Service: "l07", Instance: "fixture"}
	bounded, err := db.NewPool(ctx, dsn, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer bounded.Close()
	first, err := db.AcquireBounded(ctx, bounded, opts)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = db.AcquireBounded(ctx, bounded, opts)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("acquisition not bounded: %v", err)
	}
	first.Release()
	exec(t, p, "CREATE TABLE l07_rollback (id int PRIMARY KEY)")
	sentinel := errors.New("rollback boundary")
	err = db.WithTransaction(ctx, bounded, opts, pgx.TxOptions{}, func(tx pgx.Tx) error { exec(t, tx, "INSERT INTO l07_rollback VALUES (1)"); return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	var count int
	if err = p.QueryRow(ctx, "SELECT count(*) FROM l07_rollback").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial transaction %d %v", count, err)
	}
	err = db.WithTransaction(ctx, bounded, opts, pgx.TxOptions{}, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, "INSERT INTO l07_rollback VALUES (1)"); return err })
	if err != nil {
		t.Fatal(err)
	}
	if err = p.QueryRow(ctx, "SELECT count(*) FROM l07_rollback").Scan(&count); err != nil || count != 1 {
		t.Fatalf("commit missing: %d %v", count, err)
	}
	exec(t, p, "TRUNCATE l07_rollback")
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("panic was swallowed")
			}
		}()
		_ = db.WithTransaction(ctx, bounded, opts, pgx.TxOptions{}, func(tx pgx.Tx) error { exec(t, tx, "INSERT INTO l07_rollback VALUES (1)"); panic("rollback fixture") })
	}()
	if err = p.QueryRow(ctx, "SELECT count(*) FROM l07_rollback").Scan(&count); err != nil || count != 0 {
		t.Fatalf("panic left partial write: %d %v", count, err)
	}
	cancelCtx, cancel := context.WithTimeout(ctx, 40*time.Millisecond)
	defer cancel()
	start = time.Now()
	err = db.WithTransaction(cancelCtx, bounded, opts, pgx.TxOptions{}, func(tx pgx.Tx) error { _, err := tx.Exec(cancelCtx, "SELECT pg_sleep(5)"); return err })
	if err == nil {
		t.Fatal("uncancelled sleep")
	}
	usable, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	if err = bounded.Ping(usable); err != nil || time.Since(start) > time.Second {
		t.Fatalf("connection unusable: %v", err)
	}
	var statement, lockTimeout string
	if err = bounded.QueryRow(ctx, "SELECT current_setting('statement_timeout'),current_setting('lock_timeout')").Scan(&statement, &lockTimeout); err != nil || statement != "350ms" || lockTimeout != "100ms" {
		t.Fatalf("deadlines %s %s %v", statement, lockTimeout, err)
	}
	_, err = bounded.Exec(ctx, "SELECT pg_sleep(1)")
	sqlState(t, err, "57014")
	blocker, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	exec(t, blocker, "LOCK TABLE l07_rollback IN ACCESS EXCLUSIVE MODE")
	migrationPool, err := db.NewPool(ctx, dsn, db.PoolOptions{MaxConns: 1, QueryClass: db.Migration})
	if err != nil {
		t.Fatal(err)
	}
	defer migrationPool.Close()
	start = time.Now()
	_, err = migrationPool.Exec(ctx, "ALTER TABLE l07_rollback ADD COLUMN value text")
	sqlState(t, err, "55P03")
	elapsed := time.Since(start)
	if elapsed < 1900*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("DDL lock deadline %s", elapsed)
	}
	blockedCtx, stopBlocked := context.WithTimeout(ctx, 40*time.Millisecond)
	start = time.Now()
	err = db.WithTransaction(blockedCtx, bounded, opts, pgx.TxOptions{}, func(tx pgx.Tx) error {
		_, err := tx.Exec(blockedCtx, "INSERT INTO l07_rollback VALUES (2)")
		return err
	})
	stopBlocked()
	if err == nil {
		t.Fatal("blocked insert was not canceled")
	}
	recovery, stopRecovery := context.WithTimeout(ctx, time.Second)
	if err = bounded.Ping(recovery); err != nil || time.Since(start) > time.Second {
		t.Fatalf("blocked query connection recovery: %v", err)
	}
	stopRecovery()
	if err = blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = p.QueryRow(ctx, "SELECT count(*) FROM l07_rollback").Scan(&count); err != nil || count != 0 {
		t.Fatalf("canceled insert changed rows: %d %v", count, err)
	}
	if err = p.Ping(ctx); err != nil {
		t.Fatal("traffic interrupted", err)
	}
	t.Logf("DDL lock wait=%s; cancellation recovery under1s; SQLSTATE 42501/57014/55P03 observed", elapsed)
}

func TestPhase3CancellationAndAuthorization(t *testing.T) {
	pool, dsn := phase2MigrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	exec(t, pool, `CREATE SCHEMA indexer;CREATE TABLE indexer.events(id bigint PRIMARY KEY,payload text NOT NULL);INSERT INTO indexer.events VALUES(1,'original');
 CREATE ROLE l07_phase3_stats_runtime NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
 GRANT USAGE ON SCHEMA indexer,stats TO l07_phase3_stats_runtime;
 CREATE TABLE stats.phase3_blocked(id bigint PRIMARY KEY);GRANT SELECT,INSERT ON stats.phase3_blocked TO l07_phase3_stats_runtime`)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, conn, "SET ROLE l07_phase3_stats_runtime")
	for _, statement := range []string{
		"INSERT INTO indexer.events VALUES(2,'forbidden')", "UPDATE indexer.events SET payload='changed' WHERE id=1", "DELETE FROM indexer.events WHERE id=1",
		"ALTER TABLE indexer.events ADD COLUMN illicit text", "DROP TABLE indexer.events", "CREATE TABLE stats.illicit(id int)",
	} {
		_, err = conn.Exec(ctx, statement)
		sqlState(t, err, "42501")
	}
	exec(t, conn, "RESET ROLE")
	conn.Release()
	var unchanged bool
	if err = pool.QueryRow(ctx, "SELECT count(*)=1 AND min(payload)='original' FROM indexer.events").Scan(&unchanged); err != nil || !unchanged {
		t.Fatal("authorization changed target", err)
	}
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	exec(t, blocker, "LOCK TABLE stats.phase3_blocked IN ACCESS EXCLUSIVE MODE")
	options := db.PoolOptions{MaxConns: 1, QueryClass: db.Interactive, Service: "phase3", Instance: "cancel", AcquireTimeout: 50 * time.Millisecond}
	bounded, err := db.NewPool(ctx, dsn, options)
	if err != nil {
		t.Fatal(err)
	}
	defer db.ClosePool(bounded)
	for attempt := 0; attempt < 8; attempt++ {
		blocked, stop := context.WithTimeout(ctx, 40*time.Millisecond)
		var observation db.QueryObservation
		err = db.WithObservedTransaction(blocked, bounded, options, pgx.TxOptions{}, db.StatementFingerprint("INSERT INTO stats.phase3_blocked VALUES ($1)"), func(value db.QueryObservation) { observation = value }, func(tx pgx.Tx) error {
			_, failure := tx.Exec(blocked, "INSERT INTO stats.phase3_blocked VALUES($1)", attempt)
			return failure
		})
		stop()
		if err == nil || observation.Outcome != "deadline" {
			t.Fatal("blocked SQL did not cancel", err, observation)
		}
		recovery, done := context.WithTimeout(ctx, time.Second)
		recovered := time.Now()
		err = db.WithTransaction(recovery, bounded, options, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
			var one int
			if failure := tx.QueryRow(recovery, "SELECT 1").Scan(&one); failure != nil {
				return failure
			}
			if one != 1 {
				return errors.New("connection protocol differs")
			}
			return nil
		})
		done()
		if err != nil || time.Since(recovered) > time.Second || bounded.Stat().AcquiredConns() != 0 {
			t.Fatal("cancelled SQL connection not reusable or discarded within one second", err)
		}
	}
	if err = blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM stats.phase3_blocked").Scan(&count); err != nil || count != 0 {
		t.Fatal("cancelled writes survived", count, err)
	}
}

func TestPhase3RestoreTimestamp(t *testing.T) {
	if os.Getenv("L07_PHASE3") != "1" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	restored, err := pgxpool.New(ctx, os.Getenv("L07_RESTORE_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var expected RecoverySnapshot
	if err = json.Unmarshal([]byte(os.Getenv("L07_RECOVERY_EXPECTED")), &expected); err != nil {
		t.Fatal(err)
	}
	if expected.CanonicalCount != 2 || expected.TradeCount != 2 || expected.Volume != "109" || expected.Fees != "3" || expected.OutboxCount != 2 || expected.Published != 1 || expected.Pending != 1 || expected.BrokerSequence != 7 {
		t.Fatal("timestamp fixture differs from declared totals", expected)
	}
	var heights []int64
	if err = restored.QueryRow(ctx, "SELECT array_agg(height ORDER BY height,transaction_index,log_index) FROM indexer.indexer_raw_logs").Scan(&heights); err != nil || !reflect.DeepEqual(heights, []int64{1, 2}) {
		t.Fatal("reverse arrival did not preserve canonical order", heights, err)
	}
	actual, err := CaptureRecoverySnapshot(ctx, restored)
	if err != nil {
		t.Fatal(err)
	}
	if err = ReconcileRecoverySnapshot(actual, expected); err != nil {
		t.Fatal(err, actual, expected)
	}
	current, err := CaptureRecoverySnapshot(ctx, fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if ReconcileRecoverySnapshot(current, expected) == nil || current.CanonicalCount <= actual.CanonicalCount || current.OutboxCount <= actual.OutboxCount {
		t.Fatal("restore did not exclude post-target committed transaction")
	}
	var promoted bool
	if err = restored.QueryRow(ctx, "SELECT NOT pg_is_in_recovery()").Scan(&promoted); err != nil || !promoted {
		t.Fatal("timestamp recovery did not promote", err)
	}
	t.Logf("target=%s snapshot=%+v", os.Getenv("L07_RECOVERY_TARGET"), actual)
}

func TestRecoverySnapshotRejectsBoundaryMismatch(t *testing.T) {
	expected := RecoverySnapshot{CanonicalCount: 2, CanonicalSHA256: Checksum([]byte("canonical")), TradeCount: 2, Volume: "109", Fees: "3", OutboxCount: 2, OutboxSHA256: Checksum([]byte("outbox")), Published: 1, Pending: 1, BrokerSequence: 7}
	if err := ReconcileRecoverySnapshot(expected, expected); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*RecoverySnapshot){func(s *RecoverySnapshot) { s.CanonicalSHA256 = Checksum([]byte("different")) }, func(s *RecoverySnapshot) { s.Volume = "110" }, func(s *RecoverySnapshot) { s.Fees = "4" }, func(s *RecoverySnapshot) { s.OutboxSHA256 = Checksum([]byte("different")) }, func(s *RecoverySnapshot) { s.BrokerSequence++ }, func(s *RecoverySnapshot) { s.Pending++ }} {
		actual := expected
		mutate(&actual)
		if ReconcileRecoverySnapshot(actual, expected) == nil {
			t.Fatal("recovery mismatch accepted")
		}
	}
}
