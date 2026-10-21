package production

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const CoordinatorKey int64 = 718623014
const onlineIndexControlDDL = `SET LOCAL ROLE migration_control_owner;CREATE TABLE migration_control.migration_attempts (
 migration_id bigint NOT NULL,attempt bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 checksum text NOT NULL CHECK(checksum ~ '^[0-9a-f]{64}$'),registry_digest text NOT NULL CHECK(registry_digest ~ '^[0-9a-f]{64}$'),
 state text NOT NULL CHECK(state IN ('started','applied','failed','reconciled')),
 started_at timestamptz NOT NULL DEFAULT clock_timestamp(),finished_at timestamptz,
 lock_wait_ms bigint NOT NULL CHECK(lock_wait_ms>=0),execution_ms bigint NOT NULL DEFAULT 0 CHECK(execution_ms>=0),
 failure_class text,repair_digest text);
REVOKE ALL ON migration_control.migration_attempts FROM PUBLIC;RESET ROLE;`

func OnlineIndexControlDDL() string { return onlineIndexControlDDL }

type OnlineIndexPlan struct {
	Schema, Table, Name string
	Columns             []string
	Unique              bool
}

func (p OnlineIndexPlan) CreateSQL() (string, error) {
	if !identifier.MatchString(p.Schema) || !identifier.MatchString(p.Table) || !identifier.MatchString(p.Name) || len(p.Columns) < 1 || len(p.Columns) > 8 {
		return "", fmt.Errorf("invalid concurrent index identity")
	}
	columns := make([]string, len(p.Columns))
	seen := map[string]bool{}
	for i, c := range p.Columns {
		if !identifier.MatchString(c) || seen[c] {
			return "", fmt.Errorf("invalid index column")
		}
		seen[c] = true
		columns[i] = pgx.Identifier{c}.Sanitize()
	}
	unique := ""
	if p.Unique {
		unique = "UNIQUE "
	}
	return "CREATE " + unique + "INDEX CONCURRENTLY " + pgx.Identifier{p.Name}.Sanitize() + " ON " + pgx.Identifier{p.Schema, p.Table}.Sanitize() + " (" + strings.Join(columns, ", ") + ");", nil
}

type OnlineIndexState struct {
	Exists, Valid, Ready, Unique, DefinitionMatches bool
	Table, Schema, Method                           string
	Columns                                         []string
	Predicate, Expressions                          *string
}

func InspectOnlineIndex(ctx context.Context, conn *pgx.Conn, p OnlineIndexPlan) (OnlineIndexState, error) {
	var state OnlineIndexState
	if _, err := p.CreateSQL(); err != nil || conn == nil {
		return state, fmt.Errorf("dedicated session and exact index required")
	}
	err := conn.QueryRow(ctx, `SELECT i.indisvalid,i.indisready,i.indisunique,t.relname,tn.nspname,am.amname,ARRAY(SELECT a.attname::text FROM unnest(i.indkey) WITH ORDINALITY k(attnum,ord) JOIN pg_attribute a ON a.attrelid=t.oid AND a.attnum=k.attnum ORDER BY k.ord),pg_get_expr(i.indpred,i.indrelid),pg_get_expr(i.indexprs,i.indrelid),pg_get_indexdef(i.indexrelid)=format('CREATE %sINDEX %I ON %I.%I USING btree (%s)',$4::text,$2::text,$1::text,$3::text,(SELECT string_agg(quote_ident(v),', ' ORDER BY ord) FROM unnest($5::text[]) WITH ORDINALITY col(v,ord))) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_index i ON i.indexrelid=c.oid JOIN pg_class t ON t.oid=i.indrelid JOIN pg_namespace tn ON tn.oid=t.relnamespace JOIN pg_am am ON am.oid=c.relam WHERE n.nspname=$1 AND c.relname=$2`, p.Schema, p.Name, p.Table, indexUniquePrefix(p.Unique), p.Columns).Scan(&state.Valid, &state.Ready, &state.Unique, &state.Table, &state.Schema, &state.Method, &state.Columns, &state.Predicate, &state.Expressions, &state.DefinitionMatches)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	state.Exists = true
	return state, nil
}
func indexUniquePrefix(unique bool) string {
	if unique {
		return "UNIQUE "
	}
	return ""
}
func (s OnlineIndexState) matches(p OnlineIndexPlan) bool {
	return s.Exists && s.DefinitionMatches && s.Schema == p.Schema && s.Table == p.Table && s.Unique == p.Unique && s.Method == "btree" && s.Predicate == nil && s.Expressions == nil && reflect.DeepEqual(s.Columns, p.Columns)
}

type IndexRepairApproval struct {
	MigrationID    int64  `json:"migrationId"`
	Schema         string `json:"schema"`
	Index          string `json:"index"`
	Operation      string `json:"operation"`
	FailedChecksum string `json:"failedChecksum"`
	FailureClass   string `json:"failureClass"`
}
type IndexRepairRegistry struct {
	Version   int                   `json:"version"`
	Approvals []IndexRepairApproval `json:"approvals"`
	digest    string
	sealed    []IndexRepairApproval
}

func LoadIndexRepairRegistry(data []byte, expectedSHA256 string) (IndexRepairRegistry, error) {
	var registry IndexRepairRegistry
	if !validDigest(expectedSHA256) || Checksum(data) != expectedSHA256 || len(data) > 1048576 {
		return registry, fmt.Errorf("approved index repair source binding required")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&registry); err != nil {
		return registry, err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return registry, fmt.Errorf("one repair registry document required")
	}
	if registry.Version != 1 || len(registry.Approvals) == 0 || len(registry.Approvals) > 128 {
		return registry, fmt.Errorf("invalid repair registry")
	}
	seen := map[int64]bool{}
	for _, a := range registry.Approvals {
		if a.MigrationID < 1 || seen[a.MigrationID] || !identifier.MatchString(a.Schema) || !identifier.MatchString(a.Index) || a.Operation != "drop_recreate" || !validDigest(a.FailedChecksum) || a.FailureClass == "" || len(a.FailureClass) > 64 {
			return IndexRepairRegistry{}, fmt.Errorf("invalid repair authority")
		}
		seen[a.MigrationID] = true
	}
	registry.digest = expectedSHA256
	registry.sealed = append([]IndexRepairApproval(nil), registry.Approvals...)
	return registry, nil
}

var ErrInvalidOnlineIndex = errors.New("invalid_online_index_requires_approved_repair")

func ApplyOnlineIndexMigration(ctx context.Context, conn *pgx.Conn, m MigrationManifest, plan OnlineIndexPlan, registry map[int64]MigrationManifest, interfaceDigest, approvalDigest string, deployedReaderMin int64, repairs *IndexRepairRegistry) (resultErr error) {
	sql, err := plan.CreateSQL()
	if err != nil {
		return err
	}
	if conn == nil || m.Schema != plan.Schema || m.TransactionMode != "nontransactional" {
		return fmt.Errorf("dedicated direct nontransactional index migration required")
	}
	lockStart := time.Now()
	var locked bool
	if err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", CoordinatorKey).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return ErrMigrationInProgress
	}
	lockWait := time.Since(lockStart)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, resetErr := conn.Exec(cleanup, "RESET ROLE; RESET lock_timeout; RESET statement_timeout")
		var released bool
		releaseErr := conn.QueryRow(cleanup, "SELECT pg_advisory_unlock($1)", CoordinatorKey).Scan(&released)
		if resetErr != nil || releaseErr != nil || !released {
			conn.Close(cleanup)
			if resultErr == nil {
				resultErr = fmt.Errorf("migration coordinator cleanup failed")
			}
		}
	}()
	if _, err = conn.Exec(ctx, "SET lock_timeout='2s'"); err != nil {
		return err
	}
	if _, err = conn.Exec(ctx, "SET statement_timeout='5min'"); err != nil {
		return err
	}
	rows, err := conn.Query(ctx, "SELECT migration_id,checksum FROM migration_control.applied_migrations")
	if err != nil {
		return err
	}
	applied := map[int64]string{}
	for rows.Next() {
		var id int64
		var checksum string
		if err = rows.Scan(&id, &checksum); err != nil {
			rows.Close()
			return err
		}
		applied[id] = checksum
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	if err = m.Validate([]byte(sql), registry, applied, interfaceDigest, approvalDigest, deployedReaderMin); err != nil {
		return err
	}
	for _, a := range m.LegacyPredecessors {
		if _, err = a.Key(); err != nil {
			return err
		}
		var source, up, fingerprint string
		if err = conn.QueryRow(ctx, "SELECT source_checksum,up_checksum,structural_fingerprint FROM migration_control.legacy_baselines WHERE service=$1 AND original_version=$2", a.Service, a.OriginalVersion).Scan(&source, &up, &fingerprint); err != nil {
			return err
		}
		if source != a.SourceSHA256 || up != a.UpSHA256 || fingerprint != a.StructuralFingerprint {
			return fmt.Errorf("legacy predecessor authority differs")
		}
	}
	var previousState, previousChecksum, previousFailure string
	err = conn.QueryRow(ctx, "SELECT state,checksum,COALESCE(failure_class,'') FROM migration_control.migration_attempts WHERE migration_id=$1 ORDER BY attempt DESC LIMIT 1", m.MigrationID).Scan(&previousState, &previousChecksum, &previousFailure)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if previousState == "started" {
		if previousChecksum != m.SHA256 {
			return fmt.Errorf("interrupted migration checksum differs")
		}
		if _, err = conn.Exec(ctx, "UPDATE migration_control.migration_attempts SET state='failed',failure_class='interrupted',finished_at=clock_timestamp() WHERE migration_id=$1 AND state='started'", m.MigrationID); err != nil {
			return err
		}
		previousState = "failed"
		previousFailure = "interrupted"
	}
	state, err := InspectOnlineIndex(ctx, conn, plan)
	if err != nil {
		return err
	}
	if state.Exists && !state.matches(plan) {
		return fmt.Errorf("existing index differs from approved definition")
	}
	repairDigest := ""
	if state.Exists || previousState != "" {
		approved := false
		if repairs != nil && validDigest(repairs.digest) {
			for _, a := range repairs.sealed {
				if a.MigrationID == m.MigrationID && a.Schema == plan.Schema && a.Index == plan.Name && a.FailedChecksum == m.SHA256 && previousChecksum == m.SHA256 && a.FailureClass == previousFailure && (previousState == "failed" || previousState == "started") {
					approved = true
					repairDigest = repairs.digest
				}
			}
		}
		if !approved {
			return ErrInvalidOnlineIndex
		}
	}
	var attempt int64
	if err = conn.QueryRow(ctx, "INSERT INTO migration_control.migration_attempts(migration_id,checksum,registry_digest,state,lock_wait_ms,repair_digest) VALUES($1,$2,$3,'started',$4,NULLIF($5,'')) RETURNING attempt", m.MigrationID, m.SHA256, approvalDigest, lockWait.Milliseconds(), repairDigest).Scan(&attempt); err != nil {
		return err
	}
	started := time.Now()
	finish := func(state, failure string) error {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanup, "RESET ROLE"); err != nil {
			return err
		}
		_, err := conn.Exec(cleanup, "UPDATE migration_control.migration_attempts SET state=$2,finished_at=clock_timestamp(),execution_ms=$3,failure_class=NULLIF($4,'') WHERE attempt=$1 AND state='started'", attempt, state, time.Since(started).Milliseconds(), failure)
		return err
	}
	defer func() {
		if resultErr != nil {
			failure := "execution_error"
			var postgres *pgconn.PgError
			if errors.As(resultErr, &postgres) {
				failure = postgres.Code
			} else if errors.Is(resultErr, ErrInvalidOnlineIndex) {
				failure = "invalid_index"
			}
			if err := finish("failed", failure); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("migration failure audit unavailable: %w", err))
			}
		}
	}()
	if _, err = conn.Exec(ctx, "SET ROLE "+pgx.Identifier{m.Schema + "_owner"}.Sanitize()); err != nil {
		return err
	}
	if state.Exists {
		if _, err = conn.Exec(ctx, "DROP INDEX CONCURRENTLY "+pgx.Identifier{plan.Schema, plan.Name}.Sanitize()); err != nil {
			return err
		}
	}
	if _, err = conn.Exec(ctx, sql); err != nil {
		return err
	}
	state, err = InspectOnlineIndex(ctx, conn, plan)
	if err != nil {
		return err
	}
	if !state.matches(plan) || !state.Valid || !state.Ready {
		return ErrInvalidOnlineIndex
	}
	if _, err = conn.Exec(ctx, "RESET ROLE"); err != nil {
		return err
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		tx.Rollback(cleanup)
	}()
	history, err := HistoryTable(m.Service, m.Schema)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO "+history+"(version_id,is_applied,tstamp) VALUES($1,true,clock_timestamp())", m.MigrationID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO migration_control.applied_migrations(migration_id,service,schema_name,checksum,registry_digest) VALUES($1,$2,$3,$4,$5)", m.MigrationID, m.Service, m.Schema, m.SHA256, approvalDigest); err != nil {
		return err
	}
	outcome := "applied"
	if repairDigest != "" {
		outcome = "reconciled"
	}
	if _, err = tx.Exec(ctx, "UPDATE migration_control.migration_attempts SET state=$2,finished_at=clock_timestamp(),execution_ms=$3 WHERE attempt=$1 AND state='started'", attempt, outcome, time.Since(started).Milliseconds()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func ApplyCoordinatedMigration(ctx context.Context, conn *pgx.Conn, m MigrationManifest, sql []byte, registry map[int64]MigrationManifest, interfaceDigest, approvalDigest string, deployedReaderMin int64) (resultErr error) {
	if conn == nil || m.TransactionMode != "transactional" {
		return fmt.Errorf("dedicated transactional coordinator required")
	}
	started := time.Now()
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", CoordinatorKey).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return ErrMigrationInProgress
	}
	lockWait := time.Since(started)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var released bool
		if err := conn.QueryRow(cleanup, "SELECT pg_advisory_unlock($1)", CoordinatorKey).Scan(&released); err != nil || !released {
			conn.Close(cleanup)
			if resultErr == nil {
				resultErr = fmt.Errorf("migration lock release unavailable")
			}
		}
	}()
	rows, err := conn.Query(ctx, "SELECT migration_id,checksum FROM migration_control.applied_migrations")
	if err != nil {
		return err
	}
	applied := map[int64]string{}
	for rows.Next() {
		var id int64
		var sum string
		if err = rows.Scan(&id, &sum); err != nil {
			rows.Close()
			return err
		}
		applied[id] = sum
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	if err = m.Validate(sql, registry, applied, interfaceDigest, approvalDigest, deployedReaderMin); err != nil {
		return err
	}
	var attempt int64
	if err = conn.QueryRow(ctx, "INSERT INTO migration_control.migration_attempts(migration_id,checksum,registry_digest,state,lock_wait_ms) VALUES($1,$2,$3,'started',$4) RETURNING attempt", m.MigrationID, m.SHA256, approvalDigest, lockWait.Milliseconds()).Scan(&attempt); err != nil {
		return err
	}
	execution := time.Now()
	resultErr = ApplyRegisteredMigration(ctx, conn, m, sql, registry, interfaceDigest, approvalDigest, deployedReaderMin)
	outcome, failure := "applied", ""
	if resultErr != nil {
		outcome = "failed"
		failure = "execution_error"
		var postgres *pgconn.PgError
		if errors.As(resultErr, &postgres) {
			failure = postgres.Code
		}
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if resultErr != nil {
		var committed bool
		if err = conn.QueryRow(cleanup, "SELECT EXISTS(SELECT 1 FROM migration_control.applied_migrations WHERE migration_id=$1 AND checksum=$2 AND registry_digest=$3)", m.MigrationID, m.SHA256, approvalDigest).Scan(&committed); err != nil {
			return errors.Join(resultErr, fmt.Errorf("migration outcome reconciliation unavailable: %w", err))
		}
		if committed {
			outcome = "applied"
			failure = "commit_response_uncertain"
		}
	}
	if _, err = conn.Exec(cleanup, "UPDATE migration_control.migration_attempts SET state=$2,finished_at=clock_timestamp(),execution_ms=$3,failure_class=NULLIF($4,'') WHERE attempt=$1 AND state='started'", attempt, outcome, time.Since(execution).Milliseconds(), failure); err != nil {
		return errors.Join(resultErr, err)
	}
	return resultErr
}
