package policy

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
)

func TestRealStatsRoleDeniedIdentityInsert(t *testing.T) {
	pool, schema := realSecurityDatabase(t)
	ctx := context.Background()
	role := schema + "_stats"
	roleID := pgx.Identifier{role}.Sanitize()
	schemaID := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE ROLE "+roleID+" NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;GRANT USAGE ON SCHEMA "+schemaID+" TO "+roleID+";CREATE TABLE "+schemaID+".identity_records(id integer PRIMARY KEY);GRANT SELECT ON "+schemaID+".identity_records TO "+roleID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanup, "DROP OWNED BY "+roleID+";DROP ROLE "+roleID); err != nil {
			t.Error(err)
		}
	})
	config := pool.Config().Copy()
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE "+roleID)
		return err
	}
	limited, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer limited.Close()
	if err = ValidateDatabaseRole(ctx, limited, schema); err != nil {
		t.Fatal("nonowner rejected", err)
	}
	if _, err = limited.Exec(ctx, "INSERT INTO "+schemaID+".identity_records VALUES(1)"); err == nil {
		t.Fatal("stats role crossed identity boundary")
	}
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM "+schemaID+".identity_records").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if ValidateDatabaseRole(ctx, pool, schema) == nil {
		t.Fatal("schema owner accepted as application role")
	}
}

func TestCredentialInventoryRejectsUnboundedAuthority(t *testing.T) {
	now := time.Now()
	base := Credential{ID: "stats-runtime", Owner: "stats", Audience: "stats", Kind: "database", Expires: now.Add(time.Minute), Schemas: []string{"stats"}, Subjects: []string{"stats.outbox"}, ObjectPrefixes: []string{"stats/reports/"}}
	if ValidateCredentials([]Credential{base}, now) != nil {
		t.Fatal("bounded credential rejected")
	}
	for _, which := range []string{"wildcard-subject", "traversal-prefix", "upload-delete", "expired", "migration"} {
		value := base
		switch which {
		case "wildcard-subject":
			value.Subjects = []string{">"}
		case "traversal-prefix":
			value.ObjectPrefixes = []string{"stats/../identity/"}
		case "upload-delete":
			value.Kind = "upload"
			value.DeleteObjects = true
		case "expired":
			value.Expires = now
		case "migration":
			value.Migration = true
			value.Expires = now.Add(16 * time.Minute)
		}
		if ValidateCredentials([]Credential{value}, now) == nil {
			t.Fatal("unbounded credential accepted", which)
		}
	}
}
