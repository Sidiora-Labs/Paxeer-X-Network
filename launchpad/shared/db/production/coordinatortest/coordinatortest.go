// Package coordinatortest bootstraps a private PostgreSQL database the way the
// production migration coordinator does (spec/production-system/integration
// postgres-contracts.go -action bootstrap, fixtureBoot, as run by
// kindle-migrate), scoped to the registered migrations a set of runtime roles
// reads, so a service's tests run its real production code paths as its real
// least-privilege runtime role against the coordinator-owned schema.
//
// It generalises core/ranking-algo/internal/internaltest/runtime_database.go
// to any runtime role of the reviewed role manifest: the reviewed
// migration_control ledger, NOLOGIN owner roles, the legacy goose baselines of
// the planned services, the registered migrations applied as their owners in
// application order (with history and applied_migrations rows), the owner
// interfaces (owner_read_grants), the function EXECUTE revocation, the
// reviewed grants.json runtime privileges, a seeded recovery epoch, LOGIN
// runtime roles, and an ExpectedSchema document per runtime role recorded with
// RecordRuntimeReadiness from the actual catalog fingerprints.
//
// Roles are cluster-global, so every reviewed role name carries a per-call
// suffix; registered SQL that names a reviewed role (for example
// "GRANT ... TO chat_runtime") is bound to the suffixed role after its
// reviewed sha256 is verified. The database and every role it created are
// dropped when the test ends. It never starts containers: the caller passes
// the DSN of an already running server whose role may CREATE DATABASE and
// CREATE ROLE. It is imported only by tests.
package coordinatortest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/db/production"
)

// Region is the recovery region the private bootstrap seeds.
const Region = "fixture-loopback"

// Epoch is the recovery epoch the private bootstrap seeds.
const Epoch = 1

const migrationsDir = "spec/production-system/integration/migrations"

// Runtime is one bootstrapped least-privilege runtime role.
type Runtime struct {
	// Role is the per-test role name (the reviewed name plus a suffix).
	Role string
	// Service is the reviewed manifest service (ExpectedSchema.service).
	Service string
	// DSN logs in as Role.
	DSN string
	// ExpectedSchema is the canonical ExpectedSchema JSON the runtime loads
	// through EXPECTED_SCHEMA_FILE / EXPECTED_SCHEMA_SHA256, and
	// ExpectedSchemaSHA256 its digest.
	ExpectedSchema       []byte
	ExpectedSchemaSHA256 string
}

// Database is a private coordinator-bootstrapped database.
type Database struct {
	// AdminDSN reaches the database as the cluster administrator (test
	// seeding stands in for the producers that own the other schemas).
	AdminDSN string
	Name     string
	// Applied maps every applied registered migration to its checksum.
	Applied map[int64]string
	// Runtimes is keyed by the reviewed runtime role name.
	Runtimes map[string]Runtime
	// Rename maps a reviewed role name to its per-test name.
	Rename func(string) string
}

// ExpectedSchemaFile writes the runtime role's ExpectedSchema document to a
// test-owned file and returns its path and sha256.
func (d Database) ExpectedSchemaFile(t testing.TB, role string) (string, string) {
	t.Helper()
	rt, ok := d.Runtimes[role]
	if !ok {
		t.Fatalf("runtime role %s was not bootstrapped", role)
	}
	path := filepath.Join(t.TempDir(), "expected-schema.json")
	if err := os.WriteFile(path, rt.ExpectedSchema, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, rt.ExpectedSchemaSHA256
}

type legacyHistories struct {
	Histories []struct {
		Service string `json:"service"`
		Schema  string `json:"schema"`
		History string `json:"history"`
		Sources []struct {
			OriginalVersion int64  `json:"original_version"`
			Path            string `json:"path"`
			SourceSHA256    string `json:"source_sha256"`
			UpSHA256        string `json:"up_sha256"`
		} `json:"sources"`
	} `json:"histories"`
}

type registryDocument struct {
	production.MigrationRegistry
	Extensions []struct {
		Extension string `json:"extension"`
		Schema    string `json:"schema"`
	} `json:"extension_prerequisites"`
}

// RepositoryRoot is the monorepo root (the reviewed migration inputs live
// under spec/production-system).
func RepositoryRoot(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller unavailable")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
}

func readBound(t testing.TB, root, relative, want string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, relative))
	if err != nil {
		t.Fatal(err)
	}
	if want != "" && production.Checksum(raw) != want {
		t.Fatalf("%s differs from its reviewed sha256", relative)
	}
	return raw
}

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// Bootstrap creates a private database on the server adminDSN names and
// bootstraps it for the given reviewed runtime roles (grants.json
// runtime_role names).
func Bootstrap(t testing.TB, adminDSN string, runtimeRoles ...string) Database {
	t.Helper()
	if adminDSN == "" {
		t.Fatal("admin DSN is empty: point it at a running PostgreSQL whose role may CREATE DATABASE and CREATE ROLE; tests never start containers")
	}
	if len(runtimeRoles) == 0 {
		t.Fatal("no runtime role requested")
	}
	ctx := context.Background()
	root := RepositoryRoot(t)

	registryBytes := readBound(t, root, migrationsDir+"/registry.json", "")
	var registry registryDocument
	if err := json.Unmarshal(registryBytes, &registry); err != nil {
		t.Fatal(err)
	}
	registered := map[int64]production.MigrationManifest{}
	for _, m := range registry.Registrations {
		if _, dup := registered[m.MigrationID]; dup {
			t.Fatalf("migration %d registered twice", m.MigrationID)
		}
		registered[m.MigrationID] = m
	}
	// The registry's application order must be a topological order of the
	// predecessor DAG covering every registration exactly once.
	ordered := map[int64]bool{}
	for _, id := range registry.ApplicationOrder {
		m, ok := registered[id]
		if !ok || ordered[id] {
			t.Fatalf("application order names migration %d that is unregistered or repeated", id)
		}
		for _, p := range m.PredecessorIDs {
			if !ordered[p] {
				t.Fatalf("migration %d is ordered before its predecessor %d", id, p)
			}
		}
		ordered[id] = true
	}
	if len(ordered) != len(registered) {
		t.Fatal("application order does not cover every registration")
	}
	rolesBytes := readBound(t, root, migrationsDir+"/grants.json", "")
	var roleDoc struct {
		Roles []production.RoleManifest `json:"roles"`
	}
	if err := json.Unmarshal(rolesBytes, &roleDoc); err != nil {
		t.Fatal(err)
	}
	var legacy legacyHistories
	if err := json.Unmarshal(readBound(t, root, migrationsDir+"/legacy-history-aliases.json", ""), &legacy); err != nil {
		t.Fatal(err)
	}

	owners := map[string]string{} // schema -> reviewed owner role
	reviewedRoles := map[string]bool{}
	byRuntime := map[string]production.RoleManifest{}
	for _, m := range roleDoc.Roles {
		if err := m.Validate(); err != nil {
			t.Fatalf("reviewed role manifest %s: %v", m.RuntimeRole, err)
		}
		if _, ok := owners[m.Schema]; !ok {
			owners[m.Schema] = m.OwnerRole
		}
		for _, c := range m.SchemaCapabilities {
			if _, ok := owners[c.Schema]; !ok {
				owners[c.Schema] = c.OwnerRole
			}
			reviewedRoles[c.OwnerRole] = true
		}
		reviewedRoles[m.OwnerRole], reviewedRoles[m.RuntimeRole] = true, true
		if _, ok := byRuntime[m.RuntimeRole]; !ok {
			byRuntime[m.RuntimeRole] = m
		}
	}
	requested := []production.RoleManifest{}
	readSchemas := map[string]map[string]bool{} // runtime role -> schemas its readiness fingerprints
	allRead := map[string]bool{}
	for _, role := range runtimeRoles {
		m, ok := byRuntime[role]
		if !ok {
			t.Fatalf("grants.json has no manifest for runtime role %s", role)
		}
		requested = append(requested, m)
		schemas := map[string]bool{m.Schema: true}
		for _, g := range m.Grants {
			if g.Schema != "migration_control" {
				schemas[g.Schema] = true
			}
		}
		readSchemas[role] = schemas
		for s := range schemas {
			allRead[s] = true
		}
	}

	// Every registered migration of the read schemas and, transitively, its
	// predecessors, in the reviewed application order.
	needed := map[int64]bool{}
	var include func(int64)
	include = func(id int64) {
		if needed[id] {
			return
		}
		m, ok := registered[id]
		if !ok {
			t.Fatalf("migration %d is not registered", id)
		}
		needed[id] = true
		for _, p := range m.PredecessorIDs {
			include(p)
		}
	}
	for _, m := range registry.Registrations {
		if allRead[m.Schema] {
			include(m.MigrationID)
		}
	}
	plan := []production.MigrationManifest{}
	schemas := map[string]bool{}
	services := map[string]bool{}
	for _, id := range registry.ApplicationOrder {
		if needed[id] {
			plan = append(plan, registered[id])
			schemas[registered[id].Schema] = true
			services[registered[id].Service] = true
		}
	}
	for s := range allRead {
		if !schemas[s] {
			t.Fatalf("schema %s is read by a requested runtime role but no registered migration creates it", s)
		}
	}

	var suffix [5]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	tag := "_t" + hex.EncodeToString(suffix[:])
	rename := func(role string) string { return role + tag }
	// Registered SQL may name reviewed roles; bind those names to the
	// per-test roles (longest names first so no name rewrites a prefix).
	names := make([]string, 0, len(reviewedRoles))
	for name := range reviewedRoles {
		if !identifier.MatchString(name) {
			t.Fatalf("reviewed role %q is not an identifier", name)
		}
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return len(names[i]) > len(names[j]) || (len(names[i]) == len(names[j]) && names[i] < names[j])
	})
	roleReference := regexp.MustCompile(`\b(` + strings.Join(names, "|") + `)\b`)
	bind := func(sql []byte) string {
		return roleReference.ReplaceAllStringFunc(string(sql), rename)
	}

	adminConfig, err := pgx.ParseConfig(adminDSN)
	if err != nil {
		t.Fatalf("parse admin DSN: %v", err)
	}
	control, err := pgx.ConnectConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("connect admin DSN: %v", err)
	}
	defer control.Close(ctx)
	name := "coordinator" + tag
	created := []string{}
	// Registered first so it runs last: the database goes first, then every
	// cluster-global role this bootstrap created, then the shared ledger
	// owner if no other private database still uses it.
	t.Cleanup(func() {
		c, err := pgx.ConnectConfig(context.Background(), adminConfig)
		if err != nil {
			t.Logf("cleanup: %v", err)
			return
		}
		defer c.Close(context.Background())
		if _, err = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Logf("drop test database: %v", err)
		}
		for i := len(created) - 1; i >= 0; i-- {
			if _, err = c.Exec(context.Background(), "DROP ROLE IF EXISTS "+pgx.Identifier{created[i]}.Sanitize()); err != nil {
				t.Logf("drop test role %s: %v", created[i], err)
			}
		}
		var used bool
		if err = c.QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM pg_shdepend d JOIN pg_roles r ON r.oid=d.refobjid WHERE r.rolname='migration_control_owner')").Scan(&used); err == nil && !used {
			_, _ = c.Exec(context.Background(), "DROP ROLE IF EXISTS migration_control_owner")
		}
	})
	if _, err = control.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	adminURL, err := url.Parse(adminDSN)
	if err != nil || (adminURL.Scheme != "postgres" && adminURL.Scheme != "postgresql") {
		t.Fatal("admin DSN must be a postgres:// URL")
	}
	adminURL.Path = "/" + name
	db := Database{AdminDSN: adminURL.String(), Name: name, Runtimes: map[string]Runtime{}, Rename: rename}
	conn, err := pgx.Connect(ctx, db.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	exec := func(step, sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", step, err)
		}
	}

	// kindle-migrate: no CREATE on public for PUBLIC (CheckReadiness rejects
	// any CREATE) and the registry's extension prerequisites installed by the
	// administrator, never at runtime.
	exec("public create", "REVOKE CREATE ON SCHEMA public FROM PUBLIC")
	for _, x := range registry.Extensions {
		if !identifier.MatchString(x.Extension) || !identifier.MatchString(x.Schema) {
			t.Fatal("invalid extension prerequisite")
		}
		exec("extension", "CREATE EXTENSION IF NOT EXISTS "+pgx.Identifier{x.Extension}.Sanitize()+" WITH SCHEMA "+pgx.Identifier{x.Schema}.Sanitize())
	}
	// migration_control: the reviewed ledger DDL. Its owner role is
	// cluster-global and unsuffixed; it is created once per cluster.
	ddl := production.MigrationControlDDL()
	first := strings.Index(ddl, ";")
	if !strings.HasPrefix(ddl, "CREATE ROLE migration_control_owner ") || first < 0 {
		t.Fatal("migration_control DDL no longer starts with its owner role")
	}
	exec("migration control owner", "DO $$ BEGIN "+ddl[:first+1]+" EXCEPTION WHEN duplicate_object THEN NULL; END $$")
	exec("migration control", ddl[first+1:])

	// Owner roles of every planned schema (NOLOGIN, no attributes).
	planSchemas := make([]string, 0, len(schemas))
	for schema := range schemas {
		if _, ok := owners[schema]; !ok {
			t.Fatalf("schema %s has no reviewed owner", schema)
		}
		planSchemas = append(planSchemas, schema)
	}
	sort.Strings(planSchemas)
	roleCreated := map[string]bool{}
	for _, schema := range planSchemas {
		owner := rename(owners[schema])
		if !roleCreated[owner] {
			exec("owner role", "CREATE ROLE "+pgx.Identifier{owner}.Sanitize()+" NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS")
			created = append(created, owner)
			roleCreated[owner] = true
		}
	}
	database := pgx.Identifier{name}.Sanitize()

	// Legacy goose baselines of the planned services, as their owners.
	histories := map[string]bool{}
	for _, h := range legacy.Histories {
		if !services[h.Service] {
			continue
		}
		owner := rename(owners[h.Schema])
		history, err := production.HistoryTable(h.Service, h.Schema)
		if err != nil || history != h.History {
			t.Fatalf("legacy history %s: %v", h.Service, err)
		}
		historyDDL, err := production.HistoryDDL(h.Service, h.Schema)
		if err != nil {
			t.Fatal(err)
		}
		exec("legacy history", historyDDL)
		histories[h.Service] = true
		exec("legacy create", "GRANT CREATE ON DATABASE "+database+" TO "+pgx.Identifier{owner}.Sanitize())
		for _, source := range h.Sources {
			full := readBound(t, root, source.Path, source.SourceSHA256)
			up := strings.SplitN(string(full), "-- +goose Down", 2)[0]
			if production.Checksum([]byte(up)) != source.UpSHA256 {
				t.Fatalf("legacy %s up SQL differs from its reviewed hash", source.Path)
			}
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, sql := range []string{"SET LOCAL lock_timeout='2s'; SET LOCAL ROLE " + pgx.Identifier{owner}.Sanitize(), bind([]byte(up)), "RESET ROLE"} {
				if _, err = tx.Exec(ctx, sql); err != nil {
					t.Fatalf("legacy %s: %v", source.Path, err)
				}
			}
			fingerprint, err := production.CatalogFingerprint(ctx, tx, h.Schema)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(ctx, "INSERT INTO "+history+"(version_id,is_applied) VALUES($1,true)", source.OriginalVersion); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(ctx, "INSERT INTO migration_control.legacy_baselines(service,original_version,source_checksum,up_checksum,structural_fingerprint) VALUES($1,$2,$3,$4,$5)", h.Service, source.OriginalVersion, source.SourceSHA256, source.UpSHA256, fingerprint); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}
		exec("legacy revoke", "REVOKE CREATE ON DATABASE "+database+" FROM "+pgx.Identifier{owner}.Sanitize())
	}

	// Runtime roles and schemas: the reviewed ProvisionSQL of every manifest
	// of a planned schema (registered SQL may grant to any of their runtime
	// roles). Schema capabilities outside the plan are not provisioned.
	for _, m := range roleDoc.Roles {
		if !schemas[m.Schema] {
			continue
		}
		scoped := production.RoleManifest{Service: m.Service, Schema: m.Schema, OwnerRole: rename(m.OwnerRole), RuntimeRole: rename(m.RuntimeRole)}
		statements, err := scoped.ProvisionSQL()
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range statements {
			if strings.HasPrefix(statement, "CREATE ROLE ") {
				role := strings.Trim(strings.SplitN(strings.TrimPrefix(statement, "CREATE ROLE "), " ", 2)[0], `"`)
				if roleCreated[role] {
					continue
				}
				roleCreated[role] = true
				created = append(created, role)
			}
			exec("provision", statement)
		}
	}

	// Owner interfaces (owner_read_grants) of every planned owner, rendered
	// by the reviewed GrantSQL; each is granted once its source exists.
	type ownerRead struct {
		manifest production.RoleManifest
		grant    production.Grant
	}
	ownerReads := []ownerRead{}
	for _, m := range roleDoc.Roles {
		if !roleCreated[rename(m.OwnerRole)] {
			continue
		}
		for _, g := range m.OwnerReadGrants {
			if schemas[g.Schema] {
				scoped := production.RoleManifest{Service: m.Service, Schema: m.Schema, OwnerRole: rename(m.OwnerRole), RuntimeRole: rename(m.RuntimeRole), OwnerReadGrants: []production.Grant{g}}
				ownerReads = append(ownerReads, ownerRead{scoped, g})
			}
		}
	}
	grantOwnerReads := func(tx pgx.Tx, owner string) {
		t.Helper()
		for _, read := range ownerReads {
			if owner != "" && read.manifest.OwnerRole != rename(owner) {
				continue
			}
			var present bool
			if err := tx.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", pgx.Identifier{read.grant.Schema, read.grant.Object}.Sanitize()).Scan(&present); err != nil {
				t.Fatal(err)
			}
			if !present {
				continue
			}
			statements, err := read.manifest.GrantSQL()
			if err != nil {
				t.Fatalf("owner interface %s.%s: %v", read.grant.Schema, read.grant.Object, err)
			}
			for _, statement := range statements {
				if _, err = tx.Exec(ctx, statement); err != nil {
					t.Fatalf("owner interface %s.%s: %v", read.grant.Schema, read.grant.Object, err)
				}
			}
		}
	}

	// The registered migrations, in application order, each as its owner in
	// one transaction with its history row and applied_migrations row.
	registryDigest := production.Checksum(registryBytes)
	db.Applied = map[int64]string{}
	for _, m := range plan {
		raw := readBound(t, root, m.Path, m.SHA256)
		owner := owners[m.Schema]
		history, err := production.HistoryTable(m.Service, m.Schema)
		if err != nil {
			t.Fatal(err)
		}
		if !histories[m.Service] {
			historyDDL, err := production.HistoryDDL(m.Service, m.Schema)
			if err != nil {
				t.Fatal(err)
			}
			exec("history", historyDDL)
			histories[m.Service] = true
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		grantOwnerReads(tx, owner)
		as := pgx.Identifier{rename(owner)}.Sanitize()
		if _, err = tx.Exec(ctx, "GRANT CREATE ON DATABASE "+database+" TO "+as+";\nSET LOCAL lock_timeout='2s'; SELECT pg_advisory_xact_lock(718623014); SET LOCAL ROLE "+as+";\n"+bind(raw)+"\n;\nRESET ROLE;\nREVOKE CREATE ON DATABASE "+database+" FROM "+as); err != nil {
			t.Fatalf("migration %d: %v", m.MigrationID, err)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO "+history+"(version_id,is_applied) VALUES($1,true)", m.MigrationID); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO migration_control.applied_migrations(migration_id,service,schema_name,checksum,registry_digest) VALUES($1,$2,$3,$4,$5)", m.MigrationID, m.Service, m.Schema, m.SHA256, registryDigest); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatalf("migration %d: %v", m.MigrationID, err)
		}
		db.Applied[m.MigrationID] = m.SHA256
	}
	// Functions are created EXECUTE-able by PUBLIC; close them (the reviewed
	// grants below re-add exactly the reviewed EXECUTEs).
	for _, schema := range planSchemas {
		owner := pgx.Identifier{rename(owners[schema])}.Sanitize()
		exec("revoke execute", "REVOKE EXECUTE ON ALL FUNCTIONS IN SCHEMA "+pgx.Identifier{schema}.Sanitize()+" FROM PUBLIC")
		exec("default execute", "ALTER DEFAULT PRIVILEGES FOR ROLE "+owner+" REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC")
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	grantOwnerReads(tx, "")
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	// The reviewed runtime privileges of every requested role.
	for _, m := range requested {
		scoped := m
		scoped.OwnerRole, scoped.RuntimeRole, scoped.OwnerReadGrants, scoped.SchemaCapabilities = rename(m.OwnerRole), rename(m.RuntimeRole), nil, nil
		grants, err := scoped.GrantSQL()
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range grants {
			exec("runtime grant "+m.RuntimeRole, statement)
		}
	}
	if schemas["recovery"] {
		exec("recovery seed", "INSERT INTO recovery.recovery_control(singleton,epoch,region,revision,checkpoint_sha256) VALUES(true,$1,$2,1,$3)", Epoch, Region, registryDigest)
	}

	fingerprints := map[string]string{}
	read, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	for schema := range allRead {
		if fingerprints[schema], err = production.CatalogFingerprint(ctx, read, schema); err != nil {
			t.Fatalf("fingerprint %s: %v", schema, err)
		}
	}
	if err = read.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for _, m := range requested {
		role := rename(m.RuntimeRole)
		var password [24]byte
		if _, err = rand.Read(password[:]); err != nil {
			t.Fatal(err)
		}
		secret := hex.EncodeToString(password[:])
		var literal string
		if err = conn.QueryRow(ctx, "SELECT quote_literal($1)", secret).Scan(&literal); err != nil {
			t.Fatal(err)
		}
		exec("runtime login", "ALTER ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN PASSWORD "+literal)
		runtimeURL := *adminURL
		runtimeURL.User = url.UserPassword(role, secret)
		expected := production.ExpectedSchema{Service: m.Service, RuntimeRole: role, RegistryDigest: registryDigest, InterfaceDigest: production.Checksum(rolesBytes), ReaderVersion: 1, Migrations: db.Applied, Schemas: map[string]string{}, Epoch: Epoch, Region: Region, Privileges: m.Grants}
		for schema := range readSchemas[m.RuntimeRole] {
			expected.Schemas[schema] = fingerprints[schema]
		}
		if err = production.RecordRuntimeReadiness(ctx, conn, expected); err != nil {
			t.Fatalf("record readiness %s: %v", m.RuntimeRole, err)
		}
		document, err := json.Marshal(expected)
		if err != nil {
			t.Fatal(err)
		}
		digest := production.Checksum(document)
		if _, err = production.LoadExpectedSchema(document, digest); err != nil {
			t.Fatalf("expected schema document %s: %v", m.RuntimeRole, err)
		}
		db.Runtimes[m.RuntimeRole] = Runtime{Role: role, Service: m.Service, DSN: runtimeURL.String(), ExpectedSchema: document, ExpectedSchemaSHA256: digest}
	}
	return db
}
