package policy

import (
	"context"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
)

func TestV2PrivilegedMutationRollsBackOnRealAuditFailure(t *testing.T) {
	pool, schema := realSecurityDatabase(t)
	ctx := context.Background()
	table := pgx.Identifier{schema, "audit_outbox"}
	if _, err := pool.Exec(ctx, "CREATE TABLE "+pgx.Identifier{schema, "cap_changes"}.Sanitize()+"(limit_value integer NOT NULL);INSERT INTO "+pgx.Identifier{schema, "cap_changes"}.Sanitize()+" VALUES(1);ALTER TABLE "+table.Sanitize()+" ADD CONSTRAINT reject_fixture_audit CHECK(false)"); err != nil {
		t.Fatal(err)
	}
	record := Audit{Actor: "operator-a", Action: "configuration-write", Target: "quote-cap", BeforeDigest: strings.Repeat("a", 64), AfterDigest: strings.Repeat("b", 64), PolicyVersion: "v2"}
	err := MutateAuditedV2(ctx, pool, table, record, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE "+pgx.Identifier{schema, "cap_changes"}.Sanitize()+" SET limit_value=2")
		return err
	})
	if err == nil {
		t.Fatal("missing mandatory audit committed")
	}
	var value int
	if err = pool.QueryRow(ctx, "SELECT limit_value FROM "+pgx.Identifier{schema, "cap_changes"}.Sanitize()).Scan(&value); err != nil || value != 1 {
		t.Fatal("cap change survived audit failure", value, err)
	}
}

func TestInterruptedPrivilegedTransactionRollsBack(t *testing.T) {
	pool, schema := realSecurityDatabase(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	table := pgx.Identifier{schema, "interrupted_caps"}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE TABLE "+table+"(limit_value integer NOT NULL);INSERT INTO "+table+" VALUES(1)"); err != nil {
		t.Fatal(err)
	}
	err := MutateAuditedV2(ctx, pool, pgx.Identifier{schema, "audit_outbox"}, Audit{Actor: "operator-a", Action: "configuration-write", Target: "quote-cap", BeforeDigest: strings.Repeat("a", 64), AfterDigest: strings.Repeat("b", 64), PolicyVersion: "v3"}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "UPDATE "+table+" SET limit_value=2"); err != nil {
			return err
		}
		cancel()
		return ctx.Err()
	})
	if err == nil {
		t.Fatal("interrupted mutation committed")
	}
	var value, records int
	if err = pool.QueryRow(context.Background(), "SELECT limit_value FROM "+table).Scan(&value); err != nil || value != 1 {
		t.Fatal("interrupted update survived", value, err)
	}
	if err = pool.QueryRow(context.Background(), "SELECT count(*) FROM "+pgx.Identifier{schema, "audit_outbox"}.Sanitize()).Scan(&records); err != nil || records != 0 {
		t.Fatal("interrupted audit survived", records, err)
	}
}
