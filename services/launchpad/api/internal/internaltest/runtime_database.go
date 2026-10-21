package internaltest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/db/production"
)

// RuntimeDatabase is a private database bootstrapped the way the production
// fixture bootstrap (spec/production-system/integration postgres-contracts.go
// fixtureBoot) prepares one, scoped to the registered migrations of the named
// schemas (and their predecessors): reviewed migration_control ledger, NOLOGIN
// owner roles, legacy goose baselines, the registered migrations applied as
// their owners, the reviewed grants.json privileges of one core/api runtime
// role (restricted to the planned schemas), and a seeded recovery epoch.
type RuntimeDatabase struct {
	// AdminDSN reaches the private database as the cluster administrator.
	AdminDSN string
	// RuntimeDSN logs in as the least-privilege core/api runtime role.
	RuntimeDSN  string
	Host        string
	Port        uint16
	Database    string
	RuntimeRole string
	Epoch       int64
	Region      string
}

// apiService is the grants.json service of the core/api runtime roles.
const apiService = "core/api"

// RuntimeRegion is the recovery region the local fixture bootstrap admits.
const RuntimeRegion = "fixture-loopback"

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

// RepositoryRoot is the monorepo root (the reviewed migration inputs live under
// spec/production-system).
func RepositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller unavailable")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "..")
}

func readBound(t *testing.T, root, relative, want string) []byte {
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

// NewAPIRuntimeDatabase bootstraps a private database on the
// CORE_API_TEST_POSTGRES_URL server holding the registered migrations of
// the planned schemas, with the reviewed core/api runtimeRole (api_rest_runtime,
// api_sockets_runtime) granted its grants.json privileges on those schemas.
// Roles are cluster-global, so every reviewed role name carries a per-test
// suffix; the database and every role it created are dropped when the test
// ends.
func NewAPIRuntimeDatabase(t *testing.T, runtimeRole string, planned ...string) RuntimeDatabase {
	t.Helper()
	admin := os.Getenv(ExternalPostgresEnv)
	if admin == "" {
		t.Fatalf("%s is not set: point it at a running PostgreSQL whose role may CREATE DATABASE and CREATE ROLE; tests never start containers", ExternalPostgresEnv)
	}
	ctx := context.Background()
	root := RepositoryRoot(t)
	const migrations = "launchpad/spec/production-system/integration/migrations"

	registryBytes := readBound(t, root, migrations+"/registry.json", "")
	var registry production.MigrationRegistry
	if err := json.Unmarshal(registryBytes, &registry); err != nil {
		t.Fatal(err)
	}
	registered := map[int64]production.MigrationManifest{}
	for _, m := range registry.Registrations {
		registered[m.MigrationID] = m
	}
	rolesBytes := readBound(t, root, migrations+"/grants.json", "")
	var roleDoc struct {
		Roles []production.RoleManifest `json:"roles"`
	}
	if err := json.Unmarshal(rolesBytes, &roleDoc); err != nil {
		t.Fatal(err)
	}
	var legacy legacyHistories
	if err := json.Unmarshal(readBound(t, root, migrations+"/legacy-history-aliases.json", ""), &legacy); err != nil {
		t.Fatal(err)
	}

	var api production.RoleManifest
	owners := map[string]string{} // schema -> reviewed owner role
	firstManifest := map[string]production.RoleManifest{}
	for _, m := range roleDoc.Roles {
		if m.Service == apiService && m.RuntimeRole == runtimeRole {
			api = m
		}
		if _, ok := owners[m.Schema]; !ok {
			owners[m.Schema], firstManifest[m.Schema] = m.OwnerRole, m
		}
	}
	if api.RuntimeRole == "" {
		t.Fatalf("grants.json has no core/api manifest for %s", runtimeRole)
	}
	// Only the manifests this bootstrap provisions are validated; other
	// services' manifests are outside the plan.
	if err := api.Validate(); err != nil {
		t.Fatalf("reviewed role manifest %s: %v", api.RuntimeRole, err)
	}
	// The requested schemas, then every registered migration of those schemas
	// and (transitively) their predecessors.
	readSchemas := map[string]bool{}
	for _, schema := range planned {
		readSchemas[schema] = true
	}
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
		if readSchemas[m.Schema] {
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

	var suffix [5]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	tag := "_t" + hex.EncodeToString(suffix[:])
	rename := func(role string) string { return role + tag }

	adminConfig, err := pgx.ParseConfig(admin)
	if err != nil {
		t.Fatalf("parse %s: %v", ExternalPostgresEnv, err)
	}
	control, err := pgx.ConnectConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("connect %s: %v", ExternalPostgresEnv, err)
	}
	defer control.Close(ctx)
	name := "core_api_runtime" + tag
	created := []string{}
	ownsControlOwner := false
	// Registered first so it runs last: the database goes first, then every
	// cluster-global role this bootstrap created.
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
		if ownsControlOwner {
			created = append(created, "migration_control_owner")
		}
		for i := len(created) - 1; i >= 0; i-- {
			if _, err = c.Exec(context.Background(), "DROP ROLE IF EXISTS "+pgx.Identifier{created[i]}.Sanitize()); err != nil {
				t.Logf("drop test role %s: %v", created[i], err)
			}
		}
	})
	if _, err = control.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	adminURL, err := url.Parse(admin)
	if err != nil {
		t.Fatal(err)
	}
	adminURL.Path = "/" + name
	db := RuntimeDatabase{AdminDSN: adminURL.String(), Host: adminConfig.Host, Port: adminConfig.Port, Database: name, RuntimeRole: rename(api.RuntimeRole), Epoch: 1, Region: RuntimeRegion}
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

	// Registry extension prerequisites are installed by the administrator.
	for _, ext := range []string{"pg_trgm"} {
		exec("extension", "CREATE EXTENSION IF NOT EXISTS "+pgx.Identifier{ext}.Sanitize()+" SCHEMA public")
	}
	// migration_control: the reviewed ledger DDL. Its owner role is
	// cluster-global and unsuffixed; reuse it when another private database
	// on this cluster already holds it.
	ddl := production.MigrationControlDDL()
	var controlOwner bool
	if err = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='migration_control_owner')").Scan(&controlOwner); err != nil {
		t.Fatal(err)
	}
	if controlOwner {
		first := strings.Index(ddl, ";")
		if !strings.HasPrefix(ddl, "CREATE ROLE migration_control_owner ") || first < 0 {
			t.Fatal("migration_control DDL no longer starts with its owner role")
		}
		ddl = ddl[first+1:]
	} else {
		ownsControlOwner = true
	}
	exec("migration control", ddl)

	// Owner roles of every schema the plan touches (NOLOGIN, no attributes).
	planSchemas := make([]string, 0, len(schemas))
	for schema := range schemas {
		if _, ok := owners[schema]; !ok {
			t.Fatalf("schema %s has no reviewed owner", schema)
		}
		planSchemas = append(planSchemas, schema)
	}
	sort.Strings(planSchemas)
	ownerCreated := map[string]bool{}
	for _, schema := range planSchemas {
		owner := rename(owners[schema])
		if !ownerCreated[owner] {
			exec("owner role", "CREATE ROLE "+pgx.Identifier{owner}.Sanitize()+" NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS")
			created = append(created, owner)
			ownerCreated[owner] = true
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
			for _, sql := range []string{"SET LOCAL lock_timeout='2s'; SET LOCAL ROLE " + pgx.Identifier{owner}.Sanitize(), up, "RESET ROLE"} {
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

	// Runtime roles and schemas: the reviewed ProvisionSQL of one manifest per
	// planned schema (the requested core/api manifest for api_stream). Schema
	// capabilities outside the plan are not provisioned.
	runtimeCreated := map[string]bool{}
	for _, schema := range planSchemas {
		m := firstManifest[schema]
		if schema == api.Schema {
			m = api
		}
		if err := m.Validate(); err != nil {
			t.Fatalf("reviewed role manifest %s: %v", m.RuntimeRole, err)
		}
		scoped := production.RoleManifest{Service: m.Service, Schema: m.Schema, OwnerRole: rename(m.OwnerRole), RuntimeRole: rename(m.RuntimeRole)}
		statements, err := scoped.ProvisionSQL()
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range statements {
			if strings.HasPrefix(statement, "CREATE ROLE ") {
				role := strings.Trim(strings.SplitN(strings.TrimPrefix(statement, "CREATE ROLE "), " ", 2)[0], `"`)
				if ownerCreated[role] || runtimeCreated[role] {
					continue
				}
				runtimeCreated[role] = true
				created = append(created, role)
			}
			exec("provision", statement)
		}
	}

	// Owner interfaces (owner_read_grants) of every planned owner, rendered by
	// the reviewed GrantSQL; each is granted once its source view exists.
	type ownerRead struct {
		manifest production.RoleManifest
		grant    production.Grant
	}
	ownerReads := []ownerRead{}
	for _, m := range roleDoc.Roles {
		if !ownerCreated[rename(m.OwnerRole)] {
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
	// one transaction with its history row and applied_migrations ledger row.
	registryDigest := production.Checksum(registryBytes)
	applied := map[int64]string{}
	for _, m := range plan {
		sql := readBound(t, root, m.Path, m.SHA256)
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
		if _, err = tx.Exec(ctx, "GRANT CREATE ON DATABASE "+database+" TO "+as+";\nSET LOCAL lock_timeout='2s'; SET LOCAL ROLE "+as+";\n"+string(sql)+"\n;\nRESET ROLE;\nREVOKE CREATE ON DATABASE "+database+" FROM "+as); err != nil {
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
		applied[m.MigrationID] = m.SHA256
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
	// The runtime role's reviewed grants on the planned schemas.
	runtimeManifest := api
	runtimeManifest.OwnerRole, runtimeManifest.RuntimeRole = rename(api.OwnerRole), db.RuntimeRole
	runtimeManifest.OwnerReadGrants = nil
	runtimeManifest.SchemaCapabilities = nil // capability schemas are outside the plan
	runtimeManifest.Grants = nil
	for _, g := range api.Grants {
		if readSchemas[g.Schema] {
			runtimeManifest.Grants = append(runtimeManifest.Grants, g)
		}
	}
	grants, err := runtimeManifest.GrantSQL()
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range grants {
		exec("runtime grant", statement)
	}
	exec("recovery seed", "INSERT INTO recovery.recovery_control(singleton,epoch,region,revision,checkpoint_sha256) VALUES(true,$1,$2,1,$3)", db.Epoch, db.Region, registryDigest)

	var password [24]byte
	if _, err = rand.Read(password[:]); err != nil {
		t.Fatal(err)
	}
	secret := hex.EncodeToString(password[:])
	var literal string
	if err = conn.QueryRow(ctx, "SELECT quote_literal($1)", secret).Scan(&literal); err != nil {
		t.Fatal(err)
	}
	exec("runtime login", "ALTER ROLE "+pgx.Identifier{db.RuntimeRole}.Sanitize()+" LOGIN PASSWORD "+literal)
	runtimeURL := *adminURL
	runtimeURL.User = url.UserPassword(db.RuntimeRole, secret)
	db.RuntimeDSN = runtimeURL.String()

	return db
}
