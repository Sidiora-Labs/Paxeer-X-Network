package production

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"io"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// HistoryTable returns the frozen qualified history; search_path cannot redirect it.
func HistoryTable(service, schema string) (string, error) {
	histories := map[string][2]string{
		"core/indexer":                {"indexer", "migration_control.indexer_versions"},
		"core/api":                    {"api_stream", "migration_control.api_versions"},
		"core/trading-charts":         {"candles", "migration_control.trading_charts_versions"},
		"core/stats-workers":          {"stats", "migration_control.stats_workers_versions"},
		"core/pnl-tracker":            {"pnl", "migration_control.pnl_tracker_versions"},
		"media/user":                  {"users", "migration_control.user_versions"},
		"media/social":                {"chat", "migration_control.social_versions"},
		"media/metadata":              {"metadata", "migration_control.metadata_versions"},
		"media/livestream":            {"livestream", "migration_control.livestream_versions"},
		"core/ranking-algo":           {"ranking", "migration_control.ranking_versions"},
		"core/stats-workers/search":   {"stats_search", "migration_control.search_versions"},
		"platform/recovery":           {"recovery", "migration_control.recovery_versions"},
		"security/control":            {"pnl", "migration_control.control_versions"},
		"core/stats-workers/holdings": {"stats_transfer_holdings", "migration_control.stats_transfer_holdings_versions"},
		"core/api/cache":              {"api_limits", "migration_control.api_limits_versions"},
		"core/discovery":              {"narrative_discovery", "migration_control.narrative_discovery_versions"},
		"core/notifications":          {"discovery_notifications", "migration_control.discovery_notifications_versions"},
		"media/gateway/audit-source":  {"gateway_audit_source", "migration_control.gateway_audit_source_versions"},
		"media/gateway/audit-sink":    {"gateway_audit_sink", "migration_control.gateway_audit_sink_versions"},
		"media/gateway/storage":       {"gateway", "migration_control.gateway_storage_versions"},
		"security/policy":             {"security", "migration_control.security_policy_versions"},
	}
	h, ok := histories[service]
	if !ok || h[0] != schema {
		return "", fmt.Errorf("unregistered service/schema history")
	}
	return h[1], nil
}

type MigrationManifest struct {
	MigrationID            int64                  `json:"migration_id"`
	Service                string                 `json:"service"`
	Schema                 string                 `json:"schema"`
	OwnerLane              string                 `json:"owner_lane"`
	Path                   string                 `json:"path"`
	SHA256                 string                 `json:"sha256"`
	PredecessorIDs         []int64                `json:"predecessor_ids"`
	LegacyPredecessors     []LegacyMigrationAlias `json:"legacy_predecessors"`
	InterfaceDigest        string                 `json:"interface_digest"`
	ReaderMinVersion       int64                  `json:"reader_min_version"`
	EstimatedLockMS        int64                  `json:"estimated_lock_ms"`
	TransactionMode        string                 `json:"transaction_mode"`
	RollbackStrategy       string                 `json:"rollback_strategy"`
	BackfillStrategy       string                 `json:"backfill_strategy"`
	Destructive            bool                   `json:"destructive"`
	ApprovalManifestDigest string                 `json:"approval_manifest_digest"`
	// All deployed readers must have positive compatibility evidence before contraction.
	ReaderCompatibilityProof string `json:"reader_compatibility_proof,omitempty"`
}

func Checksum(sql []byte) string { s := sha256.Sum256(sql); return hex.EncodeToString(s[:]) }
func validDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && s == strings.ToLower(s)
}

// Validate verifies an exact approved source and predecessor set before DDL.
// Applied maps IDs to their recorded checksums, not max(version_id). The caller
// supplies the full approved release registry, including legacy predecessors.
func (m MigrationManifest) Validate(sql []byte, registry map[int64]MigrationManifest, applied map[int64]string, interfaceDigest, approvalDigest string, deployedReaderMin int64) error {
	approved, registered := registry[m.MigrationID]
	if !registered {
		return fmt.Errorf("migration is not registered")
	}
	// Compare all execution and safety fields, not just copied approval labels.
	// Predecessors are a set; normalize private copies without mutating registry.
	candidate := m
	candidate.PredecessorIDs = append([]int64{}, m.PredecessorIDs...)
	approved.PredecessorIDs = append([]int64{}, approved.PredecessorIDs...)
	slices.Sort(candidate.PredecessorIDs)
	slices.Sort(approved.PredecessorIDs)
	if !reflect.DeepEqual(candidate, approved) {
		return fmt.Errorf("migration differs from approved registry entry")
	}
	if _, err := HistoryTable(m.Service, m.Schema); err != nil {
		return err
	}
	var lane int
	if _, err := fmt.Sscanf(m.OwnerLane, "L%d", &lane); err != nil || lane < 1 || lane > 50 || m.OwnerLane != fmt.Sprintf("L%02d", lane) || m.MigrationID < int64(lane)*10000 || m.MigrationID > int64(lane)*10000+9999 {
		return fmt.Errorf("migration outside owner range")
	}
	if m.Path == "" || strings.HasPrefix(m.Path, "/") || strings.Contains(m.Path, "..") || len(sql) == 0 || !validDigest(m.SHA256) || m.SHA256 != Checksum(sql) {
		return fmt.Errorf("migration checksum/path mismatch")
	}
	if !validDigest(interfaceDigest) || m.InterfaceDigest != interfaceDigest || !validDigest(approvalDigest) || m.ApprovalManifestDigest != approvalDigest {
		return fmt.Errorf("unapproved interface/manifest digest")
	}
	if m.ReaderMinVersion < 1 || deployedReaderMin < m.ReaderMinVersion || m.EstimatedLockMS <= 0 || m.EstimatedLockMS > 2000 || m.RollbackStrategy == "" || m.BackfillStrategy == "" {
		return fmt.Errorf("missing safety metadata or incompatible reader")
	}
	if m.TransactionMode != "transactional" && m.TransactionMode != "nontransactional" {
		return fmt.Errorf("unknown transaction mode")
	}
	if m.Destructive && !validDigest(m.ReaderCompatibilityProof) {
		return fmt.Errorf("destructive migration requires reader compatibility evidence")
	}
	if sum, ok := applied[m.MigrationID]; ok {
		if sum != m.SHA256 {
			return fmt.Errorf("applied checksum mismatch")
		}
		return fmt.Errorf("migration already applied")
	}
	seen := map[int64]bool{}
	for _, id := range m.PredecessorIDs {
		p, known := registry[id]
		if id == m.MigrationID || seen[id] || !known || !validDigest(p.SHA256) || applied[id] != p.SHA256 {
			return fmt.Errorf("unknown, unapplied or changed predecessor %d", id)
		}
		seen[id] = true
	}
	return nil
}

var ErrMigrationInProgress = errors.New("migration_in_progress")

// CoordinatorLock must use a dedicated direct PostgreSQL session, never a
// transaction pooler. Coordinator credentials are provisioned outside this API.
type CoordinatorLock interface {
	TryLock(context.Context) (LockLease, error)
}
type LockLease interface{ Release(context.Context) error }
type PostgresCoordinatorLock struct {
	Pool           *pgxpool.Pool
	Key            int64
	AcquireTimeout time.Duration
}
type postgresLease struct {
	mu   sync.Mutex
	conn *pgxpool.Conn
	key  int64
}

func (l PostgresCoordinatorLock) TryLock(ctx context.Context) (LockLease, error) {
	if l.Pool == nil || l.AcquireTimeout <= 0 || l.AcquireTimeout > time.Second*5 {
		return nil, fmt.Errorf("invalid coordinator lock configuration")
	}
	acquireCtx, cancel := context.WithTimeout(ctx, l.AcquireTimeout)
	defer cancel()
	conn, err := l.Pool.Acquire(acquireCtx)
	if err != nil {
		return nil, err
	}
	var locked bool
	if err = conn.QueryRow(acquireCtx, "SELECT pg_try_advisory_lock($1)", l.Key).Scan(&locked); err != nil {
		raw := conn.Hijack()
		cleanup, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = raw.Close(cleanup)
		return nil, err
	}
	if !locked {
		conn.Release()
		return nil, ErrMigrationInProgress
	}
	return &postgresLease{conn: conn, key: l.Key}, nil
}
func (l *postgresLease) Release(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn == nil {
		return nil
	}
	conn := l.conn
	l.conn = nil
	releaseCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var unlocked bool
	err := conn.QueryRow(releaseCtx, "SELECT pg_advisory_unlock($1)", l.key).Scan(&unlocked)
	if err != nil || !unlocked {
		raw := conn.Hijack()
		cleanup, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = raw.Close(cleanup)
		if err != nil {
			return err
		}
		return fmt.Errorf("migration lock ownership lost")
	}
	conn.Release()
	return nil
}

type LegacyMigrationAlias struct {
	Service               string `json:"service"`
	OriginalVersion       int64  `json:"original_version"`
	SourceSHA256          string `json:"source_sha256"`
	UpSHA256              string `json:"up_sha256"`
	StructuralFingerprint string `json:"structural_fingerprint"`
}

func (a LegacyMigrationAlias) Key() (string, error) {
	if _, ok := map[string]bool{"core/indexer": true, "core/trading-charts": true, "core/stats-workers": true, "core/pnl-tracker": true, "media/user": true, "media/social": true, "media/metadata": true, "media/livestream": true}[a.Service]; !ok || a.OriginalVersion < 1 || !validDigest(a.SourceSHA256) || !validDigest(a.UpSHA256) || !validDigest(a.StructuralFingerprint) {
		return "", fmt.Errorf("legacy baseline requires exact reviewed source and structural fingerprints")
	}
	return fmt.Sprintf("%s:%d", a.Service, a.OriginalVersion), nil
}

type MigrationRegistry struct {
	SchemaVersion    int                 `json:"schema_version"`
	ApplicationOrder []int64             `json:"application_order"`
	Registrations    []MigrationManifest `json:"registrations"`
	ApprovalDigest   string              `json:"approval_digest"`
}

func (r MigrationRegistry) ValidateOrder() (map[int64]MigrationManifest, error) {
	if r.SchemaVersion != 2 || !validDigest(r.ApprovalDigest) || len(r.ApplicationOrder) == 0 || len(r.ApplicationOrder) != len(r.Registrations) {
		return nil, fmt.Errorf("unapproved migration registry")
	}
	catalog := map[int64]MigrationManifest{}
	for _, m := range r.Registrations {
		if _, exists := catalog[m.MigrationID]; exists {
			return nil, fmt.Errorf("duplicate migration")
		}
		catalog[m.MigrationID] = m
	}
	seen := map[int64]bool{}
	for _, id := range r.ApplicationOrder {
		m, ok := catalog[id]
		if !ok || seen[id] {
			return nil, fmt.Errorf("invalid application order")
		}
		for _, p := range m.PredecessorIDs {
			if !seen[p] {
				return nil, fmt.Errorf("unapplied DAG predecessor %d", p)
			}
		}
		seen[id] = true
	}
	return catalog, nil
}

const migrationControlDDL = `CREATE ROLE migration_control_owner NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
CREATE SCHEMA migration_control AUTHORIZATION migration_control_owner;
SET ROLE migration_control_owner;
REVOKE ALL ON SCHEMA migration_control FROM PUBLIC;
CREATE TABLE migration_control.applied_migrations(migration_id bigint PRIMARY KEY,service text NOT NULL,schema_name text NOT NULL,checksum text NOT NULL CHECK(checksum ~ '^[0-9a-f]{64}$'),registry_digest text NOT NULL CHECK(registry_digest ~ '^[0-9a-f]{64}$'),applied_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE TABLE migration_control.legacy_baselines(service text NOT NULL,original_version bigint NOT NULL,source_checksum text NOT NULL,up_checksum text NOT NULL,structural_fingerprint text NOT NULL,PRIMARY KEY(service,original_version));
CREATE TABLE migration_control.runtime_readiness(role_name name NOT NULL,service text NOT NULL,registry_digest text NOT NULL,interface_digest text NOT NULL,reader_min_version bigint NOT NULL CHECK(reader_min_version>0),migrations jsonb NOT NULL,PRIMARY KEY(role_name,service));
CREATE FUNCTION migration_control.readiness(wanted text) RETURNS TABLE(registry_digest text,interface_digest text,reader_min_version bigint,migrations jsonb) LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog AS $$ SELECT r.registry_digest,r.interface_digest,r.reader_min_version,r.migrations FROM migration_control.runtime_readiness r WHERE r.role_name=coalesce(nullif(current_setting('role'),'none'),SESSION_USER::text)::name AND r.service=wanted AND NOT EXISTS (SELECT 1 FROM jsonb_each_text(r.migrations) e LEFT JOIN migration_control.applied_migrations a ON a.migration_id=e.key::bigint WHERE a.checksum IS DISTINCT FROM e.value) $$;
REVOKE ALL ON ALL TABLES IN SCHEMA migration_control FROM PUBLIC;
REVOKE ALL ON FUNCTION migration_control.readiness(text) FROM PUBLIC;
RESET ROLE;`

func MigrationControlDDL() string { return migrationControlDDL }

func ApplyRegisteredMigration(ctx context.Context, conn *pgx.Conn, m MigrationManifest, sql []byte, registry map[int64]MigrationManifest, interfaceDigest, approvalDigest string, deployedReaderMin int64) error {
	if conn == nil {
		return fmt.Errorf("dedicated direct coordinator connection required")
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
	if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout='2s'"); err != nil {
		return err
	}
	var coordinator bool
	if err = tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", CoordinatorKey).Scan(&coordinator); err != nil {
		return err
	}
	if !coordinator {
		return ErrMigrationInProgress
	}
	rows, err := tx.Query(ctx, "SELECT migration_id,checksum FROM migration_control.applied_migrations")
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
	for _, legacy := range m.LegacyPredecessors {
		if _, err = legacy.Key(); err != nil {
			return err
		}
		var sourceSum, upSum, fingerprint string
		if err = tx.QueryRow(ctx, "SELECT source_checksum,up_checksum,structural_fingerprint FROM migration_control.legacy_baselines WHERE service=$1 AND original_version=$2", legacy.Service, legacy.OriginalVersion).Scan(&sourceSum, &upSum, &fingerprint); err != nil {
			return err
		}
		if sourceSum != legacy.SourceSHA256 || upSum != legacy.UpSHA256 || fingerprint != legacy.StructuralFingerprint {
			return fmt.Errorf("legacy predecessor authority differs")
		}
	}
	if m.TransactionMode != "transactional" {
		return fmt.Errorf("nontransactional migration requires independently reviewed runner")
	}
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE "+pgx.Identifier{m.Schema + "_owner"}.Sanitize()); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, string(sql)); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "RESET ROLE"); err != nil {
		return err
	}
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
	return tx.Commit(ctx)
}

func HistoryDDL(service, schema string) (string, error) {
	h, err := HistoryTable(service, schema)
	if err != nil {
		return "", err
	}
	parts := strings.Split(h, ".")
	name := pgx.Identifier{parts[0], parts[1]}.Sanitize()
	return "SET LOCAL ROLE migration_control_owner; CREATE TABLE " + name + "(id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,version_id bigint NOT NULL,is_applied boolean NOT NULL,tstamp timestamptz NOT NULL DEFAULT clock_timestamp()); REVOKE ALL ON " + name + " FROM PUBLIC; RESET ROLE;", nil
}

type ExpectedSchema struct {
	Service         string            `json:"service"`
	RuntimeRole     string            `json:"runtime_role"`
	RegistryDigest  string            `json:"registry_digest"`
	InterfaceDigest string            `json:"interface_digest"`
	ReaderVersion   int64             `json:"reader_version"`
	Migrations      map[int64]string  `json:"migrations"`
	Schemas         map[string]string `json:"schemas"`
	Epoch           int64             `json:"epoch"`
	Region          string            `json:"region"`
	Privileges      []Grant           `json:"privileges"`
}

func CatalogFingerprint(ctx context.Context, tx pgx.Tx, schema string) (string, error) {
	if !identifier.MatchString(schema) {
		return "", fmt.Errorf("invalid catalog schema")
	}
	rows, err := tx.Query(ctx, `SELECT kind,identity,definition FROM (
 SELECT 'relation'::text kind,c.relname identity,concat_ws('|',c.relkind::text,pg_get_userbyid(c.relowner),c.relrowsecurity::text,c.relforcerowsecurity::text) definition FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relkind IN ('r','p','v','S')
 UNION ALL SELECT 'column',c.relname||'.'||a.attname,concat_ws('|',a.attnum::text,format_type(a.atttypid,a.atttypmod),a.attnotnull::text,a.attidentity::text,coalesce(pg_get_expr(d.adbin,d.adrelid),'')) FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_attrdef d ON d.adrelid=c.oid AND d.adnum=a.attnum WHERE n.nspname=$1 AND a.attnum>0 AND NOT a.attisdropped
 UNION ALL SELECT 'constraint',c.relname||'.'||k.conname,pg_get_constraintdef(k.oid,true) FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1
 UNION ALL SELECT 'index',c.relname,pg_get_indexdef(c.oid) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relkind='i'
 UNION ALL SELECT 'trigger',c.relname||'.'||t.tgname,pg_get_triggerdef(t.oid,true) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND NOT t.tgisinternal
 UNION ALL SELECT 'function',p.proname||'('||pg_get_function_identity_arguments(p.oid)||')',pg_get_functiondef(p.oid)||'|'||pg_get_userbyid(p.proowner) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname=$1
 UNION ALL SELECT 'view',c.relname,pg_get_viewdef(c.oid,true) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relkind='v'
 ) catalog ORDER BY kind,identity,definition`, schema)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	records := [][3]string{}
	for rows.Next() {
		var r [3]string
		if err = rows.Scan(&r[0], &r[1], &r[2]); err != nil {
			return "", err
		}
		records = append(records, r)
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	if len(records) == 0 {
		return "", fmt.Errorf("expected schema absent")
	}
	b, err := json.Marshal(records)
	if err != nil {
		return "", err
	}
	return Checksum(b), nil
}

func CheckReadiness(ctx context.Context, pool *pgxpool.Pool, want ExpectedSchema) error {
	if err := validateExpectedSchema(want); err != nil {
		return err
	}
	if pool == nil || want.Service == "" || !identifier.MatchString(want.RuntimeRole) || !validDigest(want.RegistryDigest) || !validDigest(want.InterfaceDigest) || want.ReaderVersion < 1 || len(want.Migrations) == 0 || len(want.Schemas) == 0 || want.Epoch < 1 || want.Region == "" {
		return fmt.Errorf("trusted expected schema configuration required")
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := pool.BeginTx(bounded, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(bounded)
	// Catalog fingerprints render object references relative to the
	// search_path. The coordinator records them with the server default
	// ("$user", public: no schema of an operator role exists); a runtime whose
	// pool puts its own schema first (for unqualified table names) must
	// compare in the same rendering context, so the path is pinned for this
	// transaction only.
	if _, err = tx.Exec(bounded, "SET LOCAL search_path TO public"); err != nil {
		return err
	}
	var role string
	var unsafe bool
	err = tx.QueryRow(bounded, `SELECT current_user,r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolbypassrls OR EXISTS(SELECT 1 FROM pg_class c WHERE c.relowner=r.oid AND c.relnamespace NOT IN (SELECT oid FROM pg_namespace WHERE nspname LIKE 'pg_%')) OR EXISTS(SELECT 1 FROM pg_namespace n WHERE n.nspname NOT LIKE 'pg_%' AND (pg_has_role(current_user,n.nspowner,'MEMBER') OR has_schema_privilege(current_user,n.oid,'CREATE'))) FROM pg_roles r WHERE r.rolname=current_user`).Scan(&role, &unsafe)
	if err != nil {
		return err
	}
	if role != want.RuntimeRole || unsafe {
		return fmt.Errorf("runtime role authority mismatch")
	}
	var registry, inter string
	var reader int64
	var raw []byte
	if err = tx.QueryRow(bounded, "SELECT registry_digest,interface_digest,reader_min_version,migrations FROM migration_control.readiness($1)", want.Service).Scan(&registry, &inter, &reader, &raw); err != nil {
		return err
	}
	if registry != want.RegistryDigest || inter != want.InterfaceDigest || reader < 1 || reader > want.ReaderVersion {
		return fmt.Errorf("schema attestation mismatch")
	}
	var applied map[int64]string
	if err = json.Unmarshal(raw, &applied); err != nil {
		return err
	}
	if !reflect.DeepEqual(applied, want.Migrations) {
		return fmt.Errorf("applied migration checksum mismatch")
	}
	schemas := make([]string, 0, len(want.Schemas))
	for schema := range want.Schemas {
		schemas = append(schemas, schema)
	}
	sort.Strings(schemas)
	for _, schema := range schemas {
		fingerprint := want.Schemas[schema]
		if !validDigest(fingerprint) {
			return fmt.Errorf("expected structural fingerprint absent")
		}
		var forbidden bool
		if e := tx.QueryRow(bounded, "SELECT has_schema_privilege(current_user,$1,'CREATE') OR EXISTS(SELECT 1 FROM pg_namespace n WHERE n.nspname=$1 AND pg_has_role(current_user,n.nspowner,'MEMBER'))", schema).Scan(&forbidden); e != nil {
			return e
		}
		if forbidden {
			return fmt.Errorf("runtime schema ownership forbidden")
		}
		actual, e := CatalogFingerprint(bounded, tx, schema)
		if e != nil {
			return e
		}
		if actual != fingerprint {
			return fmt.Errorf("structural fingerprint mismatch")
		}
	}
	var epoch int64
	var region string
	if err = tx.QueryRow(bounded, "SELECT epoch,region FROM recovery.current_epoch()").Scan(&epoch, &region); err != nil {
		return err
	}
	if epoch != want.Epoch || region != want.Region {
		return fmt.Errorf("restoration authority mismatch")
	}
	for _, g := range want.Privileges {
		if err = checkPrivilege(bounded, tx, want.RuntimeRole, g); err != nil {
			return err
		}
	}
	if err = checkUnexpectedPrivileges(bounded, tx, want.RuntimeRole, want.Privileges, want.Schemas); err != nil {
		return err
	}
	return tx.Commit(bounded)
}
func checkPrivilege(ctx context.Context, tx pgx.Tx, role string, g Grant) error {
	if !identifier.MatchString(g.Schema) || !identifier.MatchString(g.Object) {
		return fmt.Errorf("invalid privilege expectation")
	}
	object := pgx.Identifier{g.Schema, g.Object}.Sanitize()
	for _, p := range g.Privileges {
		var ok bool
		var err error
		switch g.ObjectKind {
		case "function":
			object = pgx.Identifier{g.Schema, g.Object}.Sanitize() + "(" + strings.Join(g.ArgumentTypes, ",") + ")"
			err = tx.QueryRow(ctx, "SELECT has_function_privilege($1,$2,$3)", role, object, p).Scan(&ok)
		case "sequence":
			err = tx.QueryRow(ctx, "SELECT has_sequence_privilege($1,$2,$3)", role, object, p).Scan(&ok)
		default:
			if len(g.Columns) > 0 {
				ok = true
				for _, c := range g.Columns {
					var permitted bool
					if err = tx.QueryRow(ctx, "SELECT has_column_privilege($1,$2,$3,$4)", role, object, c, p).Scan(&permitted); err != nil || !permitted {
						ok = false
						break
					}
				}
			} else {
				err = tx.QueryRow(ctx, "SELECT has_table_privilege($1,$2,$3)", role, object, p).Scan(&ok)
			}
		}
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("required runtime privilege absent")
		}
	}
	return nil
}

func ImportLegacyBaseline(ctx context.Context, conn *pgx.Conn, a LegacyMigrationAlias, schema string, source, up []byte) error {
	if conn == nil {
		return fmt.Errorf("dedicated coordinator connection required")
	}
	if _, err := a.Key(); err != nil {
		return err
	}
	if Checksum(source) != a.SourceSHA256 || Checksum(up) != a.UpSHA256 {
		return fmt.Errorf("legacy exact source checksum mismatch")
	}
	history, err := HistoryTable(a.Service, schema)
	if err != nil {
		return err
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var coordinator bool
	if err = tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", CoordinatorKey).Scan(&coordinator); err != nil {
		return err
	}
	if !coordinator {
		return ErrMigrationInProgress
	}
	actual, err := CatalogFingerprint(ctx, tx, schema)
	if err != nil {
		return err
	}
	if actual != a.StructuralFingerprint {
		return fmt.Errorf("legacy structure differs from approved baseline")
	}
	var recorded bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+history+" WHERE version_id=$1 AND is_applied)", a.OriginalVersion).Scan(&recorded); err != nil {
		return err
	}
	if !recorded {
		return fmt.Errorf("original per-service history proof absent; import cannot infer applied version")
	}
	_, err = tx.Exec(ctx, "INSERT INTO migration_control.legacy_baselines(service,original_version,source_checksum,up_checksum,structural_fingerprint) VALUES($1,$2,$3,$4,$5)", a.Service, a.OriginalVersion, a.SourceSHA256, a.UpSHA256, a.StructuralFingerprint)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func RecordRuntimeReadiness(ctx context.Context, conn *pgx.Conn, want ExpectedSchema) error {
	if err := validateExpectedSchema(want); err != nil {
		return err
	}
	if conn == nil || !validDigest(want.RegistryDigest) || !validDigest(want.InterfaceDigest) || want.Service == "" || !identifier.MatchString(want.RuntimeRole) || want.ReaderVersion < 1 || len(want.Migrations) == 0 || len(want.Schemas) == 0 {
		return fmt.Errorf("complete reviewed readiness expectation required")
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for id, expected := range want.Migrations {
		if !validDigest(expected) {
			return fmt.Errorf("invalid expected checksum")
		}
		var actual string
		if err = tx.QueryRow(ctx, "SELECT checksum FROM migration_control.applied_migrations WHERE migration_id=$1", id).Scan(&actual); err != nil {
			return err
		}
		if actual != expected {
			return fmt.Errorf("actual migration checksum mismatch")
		}
	}
	for schema, expected := range want.Schemas {
		if !validDigest(expected) {
			return fmt.Errorf("actual catalog fingerprint required")
		}
		actual, e := CatalogFingerprint(ctx, tx, schema)
		if e != nil {
			return e
		}
		if actual != expected {
			return fmt.Errorf("actual schema fingerprint mismatch")
		}
	}
	b, err := json.Marshal(want.Migrations)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO migration_control.runtime_readiness(role_name,service,registry_digest,interface_digest,reader_min_version,migrations) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(role_name,service) DO UPDATE SET registry_digest=EXCLUDED.registry_digest,interface_digest=EXCLUDED.interface_digest,reader_min_version=EXCLUDED.reader_min_version,migrations=EXCLUDED.migrations", want.RuntimeRole, want.Service, want.RegistryDigest, want.InterfaceDigest, want.ReaderVersion, b)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func LoadMigrationRegistry(data []byte, expectedDigest string) (MigrationRegistry, error) {
	var registry MigrationRegistry
	if !validDigest(expectedDigest) || Checksum(data) != expectedDigest {
		return registry, fmt.Errorf("trusted registry source binding mismatch")
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		return registry, err
	}
	if _, err := registry.ValidateOrder(); err != nil {
		return registry, err
	}
	return registry, nil
}

func LoadExpectedSchema(data []byte, expectedSHA256 string) (ExpectedSchema, error) {
	var expected ExpectedSchema
	if len(data) == 0 || len(data) > 1048576 || !validDigest(expectedSHA256) || Checksum(data) != expectedSHA256 {
		return expected, fmt.Errorf("trusted expected-schema source binding required")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&expected); err != nil {
		return ExpectedSchema{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return ExpectedSchema{}, fmt.Errorf("expected-schema input must contain one JSON document")
	}
	if err := validateExpectedSchema(expected); err != nil {
		return ExpectedSchema{}, err
	}
	return expected, nil
}

func validateExpectedSchema(want ExpectedSchema) error {
	if want.Service == "" || len(want.Service) > 128 || !identifier.MatchString(want.RuntimeRole) || !validDigest(want.RegistryDigest) || !validDigest(want.InterfaceDigest) || want.ReaderVersion < 1 || len(want.Migrations) == 0 || len(want.Migrations) > 256 || len(want.Schemas) == 0 || len(want.Schemas) > 16 || want.Epoch < 1 || !regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`).MatchString(want.Region) || len(want.Privileges) == 0 || len(want.Privileges) > 1024 {
		return fmt.Errorf("complete trusted expected-schema requirements required")
	}
	for id, sum := range want.Migrations {
		if id < 1 || !validDigest(sum) {
			return fmt.Errorf("invalid expected migration checksum")
		}
	}
	for schema, fingerprint := range want.Schemas {
		if !identifier.MatchString(schema) || schema == "public" || strings.HasPrefix(schema, "pg_") || !validDigest(fingerprint) {
			return fmt.Errorf("invalid expected structural fingerprint")
		}
	}
	seen := map[string]bool{}
	for _, g := range want.Privileges {
		if !identifier.MatchString(g.Schema) || !identifier.MatchString(g.Object) || g.GrantOption || len(g.Privileges) == 0 || len(g.Privileges) > 4 || len(g.Columns) > 128 || len(g.ArgumentTypes) > 16 {
			return fmt.Errorf("invalid expected privilege")
		}
		kind := g.ObjectKind
		if kind == "" {
			kind = "table"
		}
		if kind != "table" && kind != "view" && kind != "function" && kind != "sequence" {
			return fmt.Errorf("invalid expected object kind")
		}
		if g.Schema == "migration_control" && (kind != "function" || g.Object != "readiness" || len(g.ArgumentTypes) != 1 || g.ArgumentTypes[0] != "text") {
			return fmt.Errorf("runtime history access forbidden")
		}
		key := g.Schema + "." + g.Object + ":" + kind + ":" + strings.Join(g.Privileges, ",") + ":" + strings.Join(g.Columns, ",") + ":" + strings.Join(g.ArgumentTypes, ",")
		if seen[key] {
			return fmt.Errorf("duplicate expected privilege")
		}
		seen[key] = true
		for _, p := range g.Privileges {
			switch kind {
			case "function":
				if p != "EXECUTE" {
					return fmt.Errorf("invalid function privilege")
				}
			case "sequence":
				if p != "USAGE" {
					return fmt.Errorf("invalid sequence privilege")
				}
			default:
				if p != "SELECT" && p != "INSERT" && p != "UPDATE" && p != "DELETE" {
					return fmt.Errorf("unsafe expected table privilege")
				}
			}
		}
		for _, column := range g.Columns {
			if !identifier.MatchString(column) {
				return fmt.Errorf("invalid expected column")
			}
		}
		if len(g.Columns) > 0 && (kind == "function" || kind == "sequence" || len(g.Privileges) != 1 || (g.Privileges[0] != "SELECT" && g.Privileges[0] != "UPDATE")) {
			return fmt.Errorf("invalid expected column privilege")
		}
		if kind != "function" && len(g.ArgumentTypes) > 0 {
			return fmt.Errorf("function signature on non-function")
		}
		for _, arg := range g.ArgumentTypes {
			switch arg {
			case "bigint", "text", "uuid", "bytea", "integer", "boolean", "jsonb":
			default:
				return fmt.Errorf("invalid expected function argument")
			}
		}
	}
	return nil
}

func checkUnexpectedPrivileges(ctx context.Context, tx pgx.Tx, role string, grants []Grant, schemas map[string]string) error {
	tableAllowed := map[string]map[string]bool{}
	columnAllowed := map[string]map[string]map[string]bool{}
	functionAllowed := map[string]bool{}
	sequenceAllowed := map[string]bool{}
	scope := map[string]bool{"migration_control": true}
	for schema := range schemas {
		scope[schema] = true
	}
	for _, g := range grants {
		scope[g.Schema] = true
		kind := g.ObjectKind
		if kind == "function" {
			functionAllowed[g.Schema+"."+g.Object+"("+strings.Join(g.ArgumentTypes, ",")+")"] = true
			continue
		}
		if kind == "sequence" {
			sequenceAllowed[g.Schema+"."+g.Object] = true
			continue
		}
		if kind != "" && kind != "table" && kind != "view" {
			continue
		}
		key := g.Schema + "." + g.Object
		for _, priv := range g.Privileges {
			if len(g.Columns) == 0 {
				if tableAllowed[key] == nil {
					tableAllowed[key] = map[string]bool{}
				}
				tableAllowed[key][priv] = true
				continue
			}
			if columnAllowed[key] == nil {
				columnAllowed[key] = map[string]map[string]bool{}
			}
			if columnAllowed[key][priv] == nil {
				columnAllowed[key][priv] = map[string]bool{}
			}
			for _, col := range g.Columns {
				columnAllowed[key][priv][col] = true
			}
		}
	}
	names := make([]string, 0, len(scope))
	for schema := range scope {
		names = append(names, schema)
	}
	sort.Strings(names)
	rows, err := tx.Query(ctx, `SELECT n.nspname,c.relname,p.privilege,has_table_privilege($1,c.oid,p.privilege),has_table_privilege($1,c.oid,p.privilege||' WITH GRANT OPTION') FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace CROSS JOIN (VALUES ('SELECT'),('INSERT'),('UPDATE'),('DELETE'),('TRUNCATE'),('REFERENCES'),('TRIGGER')) p(privilege) WHERE n.nspname=ANY($2) AND c.relkind IN ('r','p','v')`, role, names)
	if err != nil {
		return err
	}
	for rows.Next() {
		var schema, table, priv string
		var effective, grantOption bool
		if err = rows.Scan(&schema, &table, &priv, &effective, &grantOption); err != nil {
			rows.Close()
			return err
		}
		if grantOption || (effective && !tableAllowed[schema+"."+table][priv]) {
			rows.Close()
			return fmt.Errorf("unexpected whole-table runtime privilege")
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `SELECT n.nspname,c.relname,a.attname,p.privilege,has_column_privilege($1,c.oid,a.attnum,p.privilege),has_column_privilege($1,c.oid,a.attnum,p.privilege||' WITH GRANT OPTION') FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace CROSS JOIN (VALUES ('SELECT'),('INSERT'),('UPDATE'),('REFERENCES')) p(privilege) WHERE n.nspname=ANY($2) AND c.relkind IN ('r','p','v') AND a.attnum>0 AND NOT a.attisdropped`, role, names)
	if err != nil {
		return err
	}
	for rows.Next() {
		var schema, table, col, priv string
		var effective, grantOption bool
		if err = rows.Scan(&schema, &table, &col, &priv, &effective, &grantOption); err != nil {
			rows.Close()
			return err
		}
		key := schema + "." + table
		if grantOption || (effective && !tableAllowed[key][priv] && !columnAllowed[key][priv][col]) {
			rows.Close()
			return fmt.Errorf("unexpected runtime column privilege")
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `SELECT n.nspname,p.proname,oidvectortypes(p.proargtypes),has_function_privilege($1,p.oid,'EXECUTE'),has_function_privilege($1,p.oid,'EXECUTE WITH GRANT OPTION') FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname=ANY($2)`, role, names)
	if err != nil {
		return err
	}
	for rows.Next() {
		var schema, name, args string
		var effective, grantOption bool
		if err = rows.Scan(&schema, &name, &args, &effective, &grantOption); err != nil {
			rows.Close()
			return err
		}
		if grantOption || (effective && !functionAllowed[schema+"."+name+"("+strings.ReplaceAll(args, " ", "")+")"]) {
			rows.Close()
			return fmt.Errorf("unexpected runtime function privilege")
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `SELECT n.nspname,c.relname,p.privilege,has_sequence_privilege($1,c.oid,p.privilege),has_sequence_privilege($1,c.oid,p.privilege||' WITH GRANT OPTION') FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace CROSS JOIN (VALUES ('USAGE'),('SELECT'),('UPDATE')) p(privilege) WHERE n.nspname=ANY($2) AND c.relkind='S'`, role, names)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var schema, name, priv string
		var effective, grantOption bool
		if err = rows.Scan(&schema, &name, &priv, &effective, &grantOption); err != nil {
			return err
		}
		if grantOption || (effective && (priv != "USAGE" || !sequenceAllowed[schema+"."+name])) {
			return fmt.Errorf("unexpected runtime sequence privilege")
		}
	}
	return rows.Err()
}

type RecoverySnapshot struct {
	CanonicalCount  int64  `json:"canonical_count"`
	CanonicalSHA256 string `json:"canonical_sha256"`
	TradeCount      int64  `json:"trade_count"`
	Volume          string `json:"volume"`
	Fees            string `json:"fees"`
	OutboxCount     int64  `json:"outbox_count"`
	OutboxSHA256    string `json:"outbox_sha256"`
	Published       int64  `json:"published"`
	Pending         int64  `json:"pending"`
	BrokerSequence  int64  `json:"broker_sequence"`
}

const recoverySnapshotSQL = `SELECT
 (SELECT count(*) FROM indexer.indexer_raw_logs),
 (SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY chain_id,height,transaction_index,log_index),'[]'::jsonb)::text FROM indexer.indexer_raw_logs e),
 (SELECT count(*) FROM stats.trade_facts),
 (SELECT coalesce(sum(volume),0)::text FROM stats.trade_facts),
 (SELECT coalesce(sum(fee),0)::text FROM stats.trade_facts),
 (SELECT count(*) FROM indexer.delivery_outbox),
 (SELECT coalesce(jsonb_agg(to_jsonb(o) ORDER BY event_id),'[]'::jsonb)::text FROM indexer.delivery_outbox o),
 (SELECT count(*) FROM indexer.delivery_outbox WHERE state='published'),
 (SELECT count(*) FROM indexer.delivery_outbox WHERE state='pending'),
 (SELECT coalesce(max(broker_sequence),0) FROM indexer.delivery_outbox),
 (SELECT count(*) FROM indexer.indexer_raw_logs WHERE chain_id<>125),
 (SELECT count(*) FROM indexer.indexer_ingest_outbox o LEFT JOIN indexer.indexer_raw_logs e USING(chain_id,transaction_hash,log_index) WHERE e.chain_id IS NULL)`

func CaptureRecoverySnapshot(ctx context.Context, pool *pgxpool.Pool) (RecoverySnapshot, error) {
	var result RecoverySnapshot
	if ctx == nil || pool == nil {
		return result, fmt.Errorf("real recovery database required")
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := pool.BeginTx(bounded, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = tx.Rollback(cleanup)
	}()
	var canonical, outbox string
	var wrongChain, orphan int64
	if err = tx.QueryRow(bounded, recoverySnapshotSQL).Scan(&result.CanonicalCount, &canonical, &result.TradeCount, &result.Volume, &result.Fees, &result.OutboxCount, &outbox, &result.Published, &result.Pending, &result.BrokerSequence, &wrongChain, &orphan); err != nil {
		return RecoverySnapshot{}, err
	}
	if wrongChain != 0 || orphan != 0 {
		return RecoverySnapshot{}, fmt.Errorf("restored canonical authority or outbox boundary differs")
	}
	result.CanonicalSHA256 = Checksum([]byte(canonical))
	result.OutboxSHA256 = Checksum([]byte(outbox))
	if err = tx.Commit(bounded); err != nil {
		return RecoverySnapshot{}, err
	}
	return result, nil
}

func ReconcileRecoverySnapshot(actual, expected RecoverySnapshot) error {
	if expected.CanonicalCount < 1 || expected.TradeCount < 1 || expected.OutboxCount < 1 || !validDigest(expected.CanonicalSHA256) || !validDigest(expected.OutboxSHA256) || expected.Published < 0 || expected.Pending < 0 || expected.Published+expected.Pending > expected.OutboxCount || expected.BrokerSequence < 0 {
		return fmt.Errorf("complete timestamped recovery expectation required")
	}
	for _, total := range []string{expected.Volume, expected.Fees} {
		if !regexp.MustCompile(`^(0|[1-9][0-9]*)$`).MatchString(total) {
			return fmt.Errorf("integer business totals required")
		}
	}
	if actual != expected {
		return fmt.Errorf("restored canonical identities, business totals or outbox boundaries differ")
	}
	return nil
}
