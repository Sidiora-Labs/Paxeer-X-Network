package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func readySecret(t *testing.T) string {
	t.Helper()
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func readyPool(t *testing.T, ctx context.Context, user, password string) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig(fixtureDSN)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.User = user
	config.ConnConfig.Password = password
	config.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = pool.Ping(ctx); err != nil {
		t.Fatalf("login as %s failed: %v", user, err)
	}
	return pool
}

// TestReadyAcceptsColumnNarrowedViewGrants runs Runtime.Ready against real
// PostgreSQL roles shaped like the production role manifest: identity_runtime
// holds column-level SELECT on the two identity views and nothing on the
// authority_credentials base table.
func TestReadyAcceptsColumnNarrowedViewGrants(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, fixtureDSN)
	if err != nil {
		t.Fatal(err)
	}
	if err = admin.Ping(ctx); err != nil {
		admin.Close()
		t.Fatal("real PostgreSQL required:", err)
	}
	suffix := readySecret(t)[:12]
	schema := "kd_ready_" + suffix
	owner := "kd_ready_owner_" + suffix
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	quotedOwner := pgx.Identifier{owner}.Sanitize()

	var existing []string
	rows, err := admin.Query(ctx, `SELECT rolname FROM pg_roles WHERE rolname IN ('control_runtime','identity_runtime') ORDER BY rolname`)
	if err != nil {
		admin.Close()
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			admin.Close()
			t.Fatal(err)
		}
		existing = append(existing, name)
	}
	rows.Close()
	if len(existing) > 0 {
		admin.Close()
		t.Fatalf("roles %v already exist on the fixture database; refusing to alter shared roles", existing)
	}

	var created []string
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(c, "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		for i := len(created) - 1; i >= 0; i-- {
			if _, err := admin.Exec(c, "DROP ROLE IF EXISTS "+pgx.Identifier{created[i]}.Sanitize()); err != nil {
				t.Errorf("drop role %s: %v", created[i], err)
			}
		}
		admin.Close()
	})
	exec := func(sql string) {
		t.Helper()
		if _, err := admin.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	exec("CREATE ROLE " + quotedOwner + " NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS")
	created = append(created, owner)
	passwords := map[string]string{}
	for _, role := range []string{"control_runtime", "identity_runtime"} {
		passwords[role] = readySecret(t)
		// Password is a random hex string, so literal quoting is safe.
		exec("CREATE ROLE " + role + " LOGIN PASSWORD '" + passwords[role] + "' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS")
		created = append(created, role)
	}
	// The admin must be able to act as the owner to hand it the schema objects.
	exec("GRANT " + quotedOwner + " TO current_user")

	exec("CREATE SCHEMA " + quotedSchema + " AUTHORIZATION " + quotedOwner)
	conn, err := admin.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ddl := []string{
		"SET ROLE " + quotedOwner,
		"CREATE TABLE " + quotedSchema + ".authority_credentials(credential_id uuid PRIMARY KEY,actor_id uuid NOT NULL,role text NOT NULL,scopes text[] NOT NULL,token_hash bytea NOT NULL,session_id uuid,expires_at timestamptz NOT NULL,revoked_at timestamptz)",
		"CREATE VIEW " + quotedSchema + ".authority_credentials_v2 AS SELECT credential_id,actor_id,role,scopes,token_hash,session_id,expires_at,revoked_at FROM " + quotedSchema + ".authority_credentials",
		"CREATE VIEW " + quotedSchema + ".identity_control_wallets_v2 AS SELECT NULL::uuid AS actor_id,NULL::uuid AS account_id,NULL::text AS wallet,NULL::bigint AS chain_id,NULL::text AS state WHERE false",
		"RESET ROLE",
	}
	for _, sql := range ddl {
		if _, err = conn.Exec(ctx, sql); err != nil {
			conn.Release()
			t.Fatalf("%s: %v", sql, err)
		}
	}
	conn.Release()

	exec("GRANT USAGE ON SCHEMA " + quotedSchema + " TO identity_runtime, control_runtime")
	credentialColumns := "credential_id,actor_id,role,scopes,token_hash,session_id,expires_at,revoked_at"
	walletColumns := "actor_id,account_id,wallet,chain_id,state"
	exec("GRANT SELECT (" + credentialColumns + ") ON TABLE " + quotedSchema + ".authority_credentials_v2 TO identity_runtime")
	exec("GRANT SELECT (" + walletColumns + ") ON TABLE " + quotedSchema + ".identity_control_wallets_v2 TO identity_runtime")

	controlPool := readyPool(t, ctx, "control_runtime", passwords["control_runtime"])
	identityPool := readyPool(t, ctx, "identity_runtime", passwords["identity_runtime"])
	runtime, err := NewRuntime(RuntimeConfig{ControlPool: controlPool, IdentityPool: identityPool, ControlSchema: schema, AuthoritySchema: schema, Observe: func(string, error) {}})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("column-narrowed view grants pass", func(t *testing.T) {
		// Guard the shape under test: a whole-table probe would reject it.
		var tableLevel, columnLevel bool
		if err := identityPool.QueryRow(ctx, `SELECT has_table_privilege(current_user,$1,'SELECT'), has_any_column_privilege(current_user,$1,'SELECT')`, quotedSchema+".authority_credentials_v2").Scan(&tableLevel, &columnLevel); err != nil {
			t.Fatal(err)
		}
		if tableLevel || !columnLevel {
			t.Fatalf("grant shape: has_table_privilege=%v has_any_column_privilege=%v, want false/true", tableLevel, columnLevel)
		}
		if err := runtime.Ready(ctx); err != nil {
			t.Fatalf("Ready with column-level view grants: %v", err)
		}
	})
	t.Run("column grant on base table is denied", func(t *testing.T) {
		exec("GRANT SELECT (credential_id) ON TABLE " + quotedSchema + ".authority_credentials TO identity_runtime")
		err := runtime.Ready(ctx)
		exec("REVOKE SELECT (credential_id) ON TABLE " + quotedSchema + ".authority_credentials FROM identity_runtime")
		if !errors.Is(err, Denied) {
			t.Fatalf("Ready with column-level base table grant = %v, want Denied", err)
		}
	})
	t.Run("whole-table view grants pass", func(t *testing.T) {
		exec("REVOKE SELECT (" + credentialColumns + ") ON TABLE " + quotedSchema + ".authority_credentials_v2 FROM identity_runtime")
		exec("REVOKE SELECT (" + walletColumns + ") ON TABLE " + quotedSchema + ".identity_control_wallets_v2 FROM identity_runtime")
		exec("GRANT SELECT ON TABLE " + quotedSchema + ".authority_credentials_v2, " + quotedSchema + ".identity_control_wallets_v2 TO identity_runtime")
		if err := runtime.Ready(ctx); err != nil {
			t.Fatalf("Ready with whole-table view grants: %v", err)
		}
	})
}
