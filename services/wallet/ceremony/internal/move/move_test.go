package move_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/ceremony/internal/migrate"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/ceremony/internal/move"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/ceremony/internal/rehearsal"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/ceremony/internal/testsupport"
)

const sourceLedger = "005_agent_actions.sql"

type fixture struct {
	pg      *testsupport.Postgres
	target  *testsupport.Postgres
	opts    move.Options
	vectors []testsupport.Vector
	wallets []string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{pg: testsupport.StartPostgres(t)}
	testsupport.ApplyGatewayLedger(t, f.pg.DB, sourceLedger)
	_, masterB64 := testsupport.MasterKey(t)
	f.vectors = testsupport.GatewayVectors(t, masterB64, 5)
	standard := testsupport.InsertWallet(t, f.pg.DB, f.vectors[0], "standard")
	agent := testsupport.InsertWallet(t, f.pg.DB, f.vectors[1], "agent")
	funded := testsupport.InsertWallet(t, f.pg.DB, f.vectors[2], "funded")
	testsupport.InsertFundedAccount(t, f.pg.DB, funded)
	testsupport.InsertSignature(t, f.pg.DB, standard)
	testsupport.InsertSignature(t, f.pg.DB, agent)
	for i := 0; i < 3; i++ {
		if _, err := f.pg.DB.Exec(`select nextval(pg_get_serial_sequence('wallet_signatures', 'id'))`); err != nil {
			t.Fatal(err)
		}
	}
	f.wallets = []string{standard, agent, funded}
	f.target = f.pg.Database(t, "gateway_platform")
	f.opts = move.Options{
		SourceURL:     f.pg.ReadOnlyRole(t, "ceremony_reader"),
		TargetURL:     f.target.URL,
		MigrationsDir: testsupport.GatewayMigrationsDir(t),
	}
	return f
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	return c
}

func one(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()
	var v string
	if err := db.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func delta(t *testing.T, r move.DeltaReport, name string) move.DeltaTable {
	t.Helper()
	for _, d := range r.Tables {
		if d.Table == name {
			return d
		}
	}
	t.Fatalf("delta report has no table %s", name)
	return move.DeltaTable{}
}

func TestMoveCopiesEveryTableWithMatchingCountsAndDigests(t *testing.T) {
	f := newFixture(t)
	before := testsupport.Fingerprint(t, f.pg.DB)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cwdBefore := testsupport.DirEntries(t, cwd)

	r, err := move.Move(ctx(t), f.opts)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if len(r.Tables) != 13 {
		t.Fatalf("moved %d tables", len(r.Tables))
	}
	var zero [32]byte
	for _, c := range r.Tables {
		if c.SourceRows != c.TargetRows || c.SourceDigest != c.TargetDigest || c.SourceDigest == zero {
			t.Fatalf("table %s: %+v", c.Table, c)
		}
	}

	conn, err := rehearsal.Connect(context.Background(), f.pg.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	tables, err := rehearsal.ReadTables(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	for _, tbl := range tables {
		sum, n, err := rehearsal.Digest(context.Background(), conn.PgConn(), tbl.CopyOut(tbl.Ident(), "x"))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, c := range r.Tables {
			if c.Table == tbl.Name {
				found = true
				if c.SourceDigest != sum || c.SourceRows != n {
					t.Fatalf("table %s: reported digest is not the primary-key-ordered stream", tbl.Name)
				}
			}
		}
		if !found {
			t.Fatalf("table %s was not moved", tbl.Name)
		}
	}

	if after := testsupport.Fingerprint(t, f.pg.DB); !reflect.DeepEqual(before, after) {
		t.Fatal("the move changed the source database")
	}
	if got := testsupport.DirEntries(t, tmp); len(got) != 0 {
		t.Fatalf("the move created files %v", got)
	}
	if got := testsupport.DirEntries(t, cwd); !reflect.DeepEqual(got, cwdBefore) {
		t.Fatalf("the move changed the working directory: %v", got)
	}
	files := testsupport.GatewayMigrationFiles(t)
	if n := one(t, f.target.DB, `select count(*)::text from _migrations`); n != "9" || len(files) != 9 {
		t.Fatalf("target ledger holds %s of %d files", n, len(files))
	}
	plan, err := migrate.PlanMigration(context.Background(), f.target.DB)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Total != 3 || len(plan.Eligible) != 2 || len(plan.Funded) != 1 || plan.AlreadyMigrated != 0 {
		t.Fatalf("plan on the moved rows: total %d eligible %d funded %d", plan.Total, len(plan.Eligible), len(plan.Funded))
	}
	if got := one(t, f.target.DB, `select nextval(pg_get_serial_sequence('wallet_signatures', 'id'))::text`); got != "6" {
		t.Fatalf("target signature sequence continues at %s, want 6", got)
	}
	if got := one(t, f.target.DB, `select count(*)::text from wallets where archived_at is not null`); got != "1" {
		t.Fatalf("%s wallets archived by the remaining migrations", got)
	}

	moved := testsupport.Fingerprint(t, f.target.DB)
	if _, err := move.Move(ctx(t), f.opts); !errors.Is(err, rehearsal.ErrTargetNotEmpty) {
		t.Fatalf("second move into a populated target: %v", err)
	}
	if again := testsupport.Fingerprint(t, f.target.DB); !reflect.DeepEqual(moved, again) {
		t.Fatal("a refused move changed the target")
	}
}

func TestMoveDeltaCopiesOnlyAbsentRows(t *testing.T) {
	f := newFixture(t)
	if _, err := move.Move(ctx(t), f.opts); err != nil {
		t.Fatalf("move: %v", err)
	}
	late := testsupport.InsertWallet(t, f.pg.DB, f.vectors[3], "standard")
	testsupport.InsertSignature(t, f.pg.DB, late)
	testsupport.InsertWallet(t, f.target.DB, f.vectors[4], "standard")
	before := testsupport.Fingerprint(t, f.pg.DB)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	r, err := move.MoveDelta(ctx(t), f.opts)
	if err != nil {
		t.Fatalf("delta: %v", err)
	}
	if r.Moved() != 2 {
		t.Fatalf("delta moved %d rows, want the late wallet and its signature", r.Moved())
	}
	for _, name := range []string{"wallets", "wallet_signatures"} {
		d := delta(t, r, name)
		if d.Moved != 1 || d.SourceDigest != d.TargetDigest || d.Conflicts != 0 || d.Collisions != 0 {
			t.Fatalf("delta %s: %+v", name, d)
		}
	}
	if w := delta(t, r, "wallets"); w.SourceRows != 4 || w.TargetBefore != 4 {
		t.Fatalf("delta wallets counted source %d target %d", w.SourceRows, w.TargetBefore)
	}
	if len(r.Dropped) != 2 || r.Dropped[0].Table != "funded_tiers" || r.Dropped[1].Table != "whitelist_entries" {
		t.Fatalf("dropped tables %+v", r.Dropped)
	}
	if got := one(t, f.target.DB, `select count(*)::text from wallets`); got != "5" {
		t.Fatalf("target holds %s wallets after the delta", got)
	}
	if got := one(t, f.target.DB, `select count(*)::text from wallets where id = $1::uuid and address = $2`, late, f.vectors[3].Address); got != "1" {
		t.Fatal("the late wallet did not arrive intact")
	}
	if got := one(t, f.target.DB, `select nextval(pg_get_serial_sequence('wallet_signatures', 'id'))::text`); got != "7" {
		t.Fatalf("target signature sequence continues at %s, want 7", got)
	}
	if after := testsupport.Fingerprint(t, f.pg.DB); !reflect.DeepEqual(before, after) {
		t.Fatal("the delta changed the source database")
	}
	if got := testsupport.DirEntries(t, tmp); len(got) != 0 {
		t.Fatalf("the delta created files %v", got)
	}

	again, err := move.MoveDelta(ctx(t), f.opts)
	if err != nil || again.Moved() != 0 {
		t.Fatalf("second delta moved %d: %v", again.Moved(), err)
	}
}

func TestMoveDeltaRefusesCollidingRows(t *testing.T) {
	f := newFixture(t)
	if _, err := move.Move(ctx(t), f.opts); err != nil {
		t.Fatalf("move: %v", err)
	}
	moved := testsupport.Fingerprint(t, f.target.DB)

	if _, err := f.pg.DB.Exec(`update wallets set disabled_reason = 'changed on the old service' where id = $1::uuid`, f.wallets[0]); err != nil {
		t.Fatal(err)
	}
	r, err := move.MoveDelta(ctx(t), f.opts)
	if !errors.Is(err, move.ErrDeltaClash) {
		t.Fatalf("changed row accepted: %v", err)
	}
	if w := delta(t, r, "wallets"); w.Conflicts != 1 || w.Moved != 0 {
		t.Fatalf("delta wallets %+v", w)
	}
	if after := testsupport.Fingerprint(t, f.target.DB); !reflect.DeepEqual(moved, after) {
		t.Fatal("a refused delta changed the target")
	}
	if _, err := f.pg.DB.Exec(`update wallets set disabled_reason = null where id = $1::uuid`, f.wallets[0]); err != nil {
		t.Fatal(err)
	}

	testsupport.InsertWallet(t, f.target.DB, f.vectors[4], "standard")
	created := testsupport.Fingerprint(t, f.target.DB)
	testsupport.InsertWallet(t, f.pg.DB, f.vectors[4], "standard")
	r, err = move.MoveDelta(ctx(t), f.opts)
	if !errors.Is(err, move.ErrDeltaClash) {
		t.Fatalf("colliding address accepted: %v", err)
	}
	if w := delta(t, r, "wallets"); w.Collisions != 1 || w.Conflicts != 0 || w.Moved != 0 {
		t.Fatalf("delta wallets %+v", w)
	}
	if after := testsupport.Fingerprint(t, f.target.DB); !reflect.DeepEqual(created, after) {
		t.Fatal("a refused delta changed the target")
	}
}

func TestMoveRefusesAWritableSourceAndMissingConfiguration(t *testing.T) {
	f := newFixture(t)
	writable := f.opts
	writable.SourceURL = f.pg.URL
	if _, err := move.Move(ctx(t), writable); !errors.Is(err, rehearsal.ErrWritableRole) {
		t.Fatalf("superuser source accepted: %v", err)
	}
	if got := one(t, f.target.DB, `select count(*)::text from pg_tables where schemaname = 'public'`); got != "0" {
		t.Fatalf("refused move left %s tables", got)
	}
	if _, err := move.MoveDelta(ctx(t), f.opts); !errors.Is(err, move.ErrNotMoved) {
		t.Fatalf("delta before a move: %v", err)
	}
	env := map[string]string{
		rehearsal.EnvSourceURL:            f.opts.SourceURL,
		migrate.EnvDatabaseURL:            f.opts.TargetURL,
		rehearsal.EnvGatewayMigrationsDir: f.opts.MigrationsDir,
	}
	if o, err := move.LoadOptions(func(k string) string { return env[k] }); err != nil || o != f.opts {
		t.Fatalf("options %+v: %v", o, err)
	}
	for name := range env {
		missing := func(k string) string {
			if k == name {
				return ""
			}
			return env[k]
		}
		if _, err := move.LoadOptions(missing); !errors.Is(err, move.ErrConfig) {
			t.Fatalf("options loaded without %s", name)
		}
	}
}
