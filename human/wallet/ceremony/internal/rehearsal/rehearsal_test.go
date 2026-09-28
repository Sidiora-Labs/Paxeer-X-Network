package rehearsal_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/archive"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/envelope"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/migrate"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/rehearsal"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/testsupport"
)

const (
	passphrase   = "rehearsal archive passphrase of adequate length"
	sourceLedger = "005_agent_actions.sql"
	readerRole   = "ceremony_reader"
)

type source struct {
	pg      *testsupport.Postgres
	reader  string
	ledger  []string
	master  []byte
	vectors []testsupport.Vector
}

func newSource(t *testing.T, vectors int) *source {
	t.Helper()
	s := &source{pg: testsupport.StartPostgres(t)}
	s.ledger = testsupport.ApplyGatewayLedger(t, s.pg.DB, sourceLedger)
	var masterB64 string
	s.master, masterB64 = testsupport.MasterKey(t)
	s.vectors = testsupport.GatewayVectors(t, masterB64, vectors)
	return s
}

func (s *source) standardFixture(t *testing.T) string {
	t.Helper()
	standard := testsupport.InsertWallet(t, s.pg.DB, s.vectors[0], "standard")
	agent := testsupport.InsertWallet(t, s.pg.DB, s.vectors[1], "agent")
	funded := testsupport.InsertWallet(t, s.pg.DB, s.vectors[2], "funded")
	testsupport.InsertFundedAccount(t, s.pg.DB, funded)
	testsupport.InsertSignature(t, s.pg.DB, standard)
	testsupport.InsertSignature(t, s.pg.DB, agent)
	for i := 0; i < 3; i++ {
		if _, err := s.pg.DB.Exec(`select nextval(pg_get_serial_sequence('wallet_signatures', 'id'))`); err != nil {
			t.Fatal(err)
		}
	}
	s.reader = s.pg.ReadOnlyRole(t, readerRole)
	return funded
}

func options(t *testing.T, s *source) rehearsal.Options {
	return rehearsal.Options{
		SourceURL:     s.reader,
		MigrationsDir: testsupport.GatewayMigrationsDir(t),
		AdminURL:      s.pg.URL,
		AttestorBin:   testsupport.AttestorBinary(t),
		MasterKey:     s.master,
		ArchivePath:   filepath.Join(t.TempDir(), "funded.archive"),
		Passphrase:    []byte(passphrase),
	}
}

func databases(t *testing.T, pg *testsupport.Postgres) int {
	t.Helper()
	var n int
	if err := pg.DB.QueryRow(`select count(*) from pg_database where datname like 'ceremony_rehearsal_%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func relations(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`select count(*) from pg_class c join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = 'public' and c.relkind in ('r', 'p', 'v', 'm')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func strs(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v sql.NullString
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v.String)
	}
	return out
}

func tableCopy(t *testing.T, r rehearsal.CopyReport, name string) rehearsal.TableCopy {
	t.Helper()
	for _, c := range r.Tables {
		if c.Table == name {
			return c
		}
	}
	t.Fatalf("report has no table %s", name)
	return rehearsal.TableCopy{}
}

func TestCopySourceReadsOneSnapshotIntoTheFullLedger(t *testing.T) {
	s := newSource(t, 4)
	s.standardFixture(t)
	target := s.pg.Database(t, "rehearsal_target")
	all := testsupport.GatewayMigrationFiles(t)
	dir := testsupport.GatewayMigrationsDir(t)

	holder, err := s.pg.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback()
	if _, err := holder.Exec(`lock table wallet_signatures in access exclusive mode`); err != nil {
		t.Fatal(err)
	}

	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cwdBefore := testsupport.DirEntries(t, cwd)

	type result struct {
		r   rehearsal.CopyReport
		err error
	}
	done := make(chan result, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	go func() {
		r, err := rehearsal.CopySource(ctx, s.reader, target.URL, dir)
		done <- result{r, err}
	}()

	deadline := time.Now().Add(time.Minute)
	for {
		var waiting int
		if err := s.pg.DB.QueryRow(`select count(*) from pg_stat_activity where usename = $1 and wait_event_type = 'Lock'`, readerRole).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting == 1 {
			break
		}
		select {
		case res := <-done:
			t.Fatalf("copy finished before the concurrent write: %v", res.err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the copy never waited on the held table")
		}
		time.Sleep(50 * time.Millisecond)
	}
	var late string
	if err := holder.QueryRow(`insert into wallets (user_id, address, encrypted_private_key, key_version, chain_id, kind)
		values (gen_random_uuid(), $1, $2, $3, 125, 'standard') returning id::text`, s.vectors[3].Address, s.vectors[3].Envelope, s.vectors[3].Version).Scan(&late); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(`insert into wallet_signatures (user_id, wallet_id, address, kind, request_hash)
		select user_id, id, address, 'message', 'late' from wallets where id = $1::uuid`, late); err != nil {
		t.Fatal(err)
	}
	if err := holder.Commit(); err != nil {
		t.Fatal(err)
	}

	res := <-done
	if res.err != nil {
		t.Fatalf("copy: %v", res.err)
	}
	r := res.r
	if err := r.Verify(); err != nil {
		t.Fatal(err)
	}
	if len(r.Tables) != 13 {
		t.Fatalf("copied %d tables, want the 13 of the source ledger", len(r.Tables))
	}
	for _, c := range r.Tables {
		if c.SourceRows != c.TargetRows || c.SourceDigest != c.TargetDigest {
			t.Fatalf("table %s differs: %+v", c.Table, c)
		}
	}
	if w := tableCopy(t, r, "wallets"); w.SourceRows != 3 {
		t.Fatalf("wallets copied %d rows, want the snapshot's 3", w.SourceRows)
	}
	if sig := tableCopy(t, r, "wallet_signatures"); sig.SourceRows != 2 {
		t.Fatalf("signatures copied %d rows, want the snapshot's 2", sig.SourceRows)
	}
	if l := tableCopy(t, r, "_migrations"); l.SourceRows != int64(len(s.ledger)) {
		t.Fatalf("ledger copied %d rows", l.SourceRows)
	}
	if r.LedgerBefore != len(s.ledger) || !reflect.DeepEqual(r.AppliedAfter, all[len(s.ledger):]) {
		t.Fatalf("ledger before %d applied after %v", r.LedgerBefore, r.AppliedAfter)
	}
	if n := strs(t, s.pg.DB, `select count(*)::text from wallets`); n[0] != "4" {
		t.Fatalf("source holds %s wallets after the concurrent write", n[0])
	}
	if got := strs(t, target.DB, `select filename from _migrations order by filename`); !reflect.DeepEqual(got, all) {
		t.Fatalf("target ledger %v, want %v", got, all)
	}
	srcApplied := strs(t, s.pg.DB, `select filename || ' ' || applied_at::text from _migrations order by filename`)
	tgtApplied := strs(t, target.DB, `select filename || ' ' || applied_at::text from _migrations where filename <= '`+sourceLedger+`' order by filename`)
	if !reflect.DeepEqual(srcApplied, tgtApplied) {
		t.Fatal("the source ledger rows were not carried over")
	}
	for _, col := range []string{"migrated_at", "attestor_key_id", "archived_at", "did", "binding_state"} {
		if n := strs(t, target.DB, `select count(*)::text from information_schema.columns where table_name = 'wallets' and column_name = '`+col+`'`); n[0] != "1" {
			t.Fatalf("target wallets lack %s", col)
		}
	}
	srcRows := strs(t, s.pg.DB, `select id::text || ' ' || address || ' ' || coalesce(last_used_at::text, '') from wallets where address <> '`+s.vectors[3].Address+`' order by id`)
	tgtRows := strs(t, target.DB, `select id::text || ' ' || address || ' ' || coalesce(last_used_at::text, '') from wallets order by id`)
	if !reflect.DeepEqual(srcRows, tgtRows) {
		t.Fatalf("target wallets differ from the source snapshot: %v vs %v", tgtRows, srcRows)
	}
	if got := strs(t, target.DB, `select tgenabled::text from pg_trigger where tgname = 'wallet_signatures_touch_wallet'`); len(got) != 1 || got[0] != "O" {
		t.Fatalf("target trigger state %v after the load", got)
	}
	if got := strs(t, target.DB, `select nextval(pg_get_serial_sequence('wallet_signatures', 'id'))::text`); got[0] != "6" {
		t.Fatalf("target signature sequence continues at %s, want the source's last value 5 plus one", got[0])
	}
	if got := testsupport.DirEntries(t, tmp); len(got) != 0 {
		t.Fatalf("copy created files %v", got)
	}
	if got := testsupport.DirEntries(t, cwd); !reflect.DeepEqual(got, cwdBefore) {
		t.Fatalf("copy changed the working directory: %v", got)
	}
}

func TestCopySourceRefusesAWritableRole(t *testing.T) {
	s := newSource(t, 3)
	s.standardFixture(t)
	target := s.pg.Database(t, "rehearsal_target")
	dir := testsupport.GatewayMigrationsDir(t)
	ctx := context.Background()

	if _, err := rehearsal.CopySource(ctx, s.pg.URL, target.URL, dir); !errors.Is(err, rehearsal.ErrWritableRole) || !strings.Contains(err.Error(), "superuser") {
		t.Fatalf("superuser source accepted: %v", err)
	}
	writer := s.pg.ReadOnlyRole(t, "ceremony_writer")
	if _, err := s.pg.DB.Exec(`grant insert on wallets to ceremony_writer`); err != nil {
		t.Fatal(err)
	}
	if _, err := rehearsal.CopySource(ctx, writer, target.URL, dir); !errors.Is(err, rehearsal.ErrWritableRole) || !strings.Contains(err.Error(), "wallets") {
		t.Fatalf("writable source accepted: %v", err)
	}
	if n := relations(t, target.DB); n != 0 {
		t.Fatalf("refused copy left %d relations in the target", n)
	}
}

func TestCopySourceRefusesALedgerNamingAnUnknownFile(t *testing.T) {
	s := newSource(t, 3)
	s.standardFixture(t)
	if _, err := s.pg.DB.Exec(`insert into _migrations(filename) values ('008_not_in_directory.sql')`); err != nil {
		t.Fatal(err)
	}
	target := s.pg.Database(t, "rehearsal_target")
	_, err := rehearsal.CopySource(context.Background(), s.reader, target.URL, testsupport.GatewayMigrationsDir(t))
	if !errors.Is(err, rehearsal.ErrUnknownMigration) || !strings.Contains(err.Error(), "008_not_in_directory.sql") {
		t.Fatalf("unknown ledger file accepted: %v", err)
	}
	if n := relations(t, target.DB); n != 0 {
		t.Fatalf("refused copy left %d relations in the target", n)
	}
}

func TestCopyReportVerifyComparesCountsAndDigests(t *testing.T) {
	same := [32]byte{1, 2, 3}
	ok := rehearsal.CopyReport{Tables: []rehearsal.TableCopy{{Table: "wallets", SourceRows: 3, TargetRows: 3, SourceDigest: same, TargetDigest: same}}}
	if err := ok.Verify(); err != nil {
		t.Fatal(err)
	}
	short := rehearsal.CopyReport{Tables: []rehearsal.TableCopy{{Table: "wallets", SourceRows: 3, TargetRows: 2, SourceDigest: same, TargetDigest: same}}}
	if err := short.Verify(); !errors.Is(err, rehearsal.ErrCountMismatch) || !strings.Contains(err.Error(), "wallets source 3 target 2") {
		t.Fatalf("count difference accepted: %v", err)
	}
	altered := rehearsal.CopyReport{Tables: []rehearsal.TableCopy{{Table: "wallets", SourceRows: 3, TargetRows: 3, SourceDigest: same, TargetDigest: [32]byte{9}}}}
	if err := altered.Verify(); !errors.Is(err, rehearsal.ErrDigestMismatch) {
		t.Fatalf("digest difference accepted: %v", err)
	}
}

func TestRehearseMigratesEveryWalletThroughFiveDaemons(t *testing.T) {
	s := newSource(t, 3)
	funded := s.standardFixture(t)
	before := testsupport.Fingerprint(t, s.pg.DB)
	o := options(t, s)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	r, err := rehearsal.Rehearse(ctx, o)
	if err != nil {
		t.Fatalf("rehearse: %v (report %s)", err, r)
	}
	if got := r.String(); got != "wallets=3 eligible=2 funded_archived=1 already_migrated=0 read=2 verified=2 imported=2 refreshed=2 test_signed=2 matched=2" {
		t.Fatalf("report line %q", got)
	}
	if err := r.Copy.Verify(); err != nil {
		t.Fatal(err)
	}
	if w := tableCopy(t, r.Copy, "wallets"); w.SourceRows != 3 || w.TargetRows != 3 {
		t.Fatalf("wallets copy %+v", w)
	}
	counts := r.Counts()
	if !strings.Contains(counts, "\ntable=wallets source_rows=3 target_rows=3\n") || strings.Contains(counts, "sha256") {
		t.Fatalf("counts report %q", counts)
	}
	if full := r.Full(); !strings.Contains(full, "\ntable=wallets source_rows=3 target_rows=3 source_sha256=") {
		t.Fatalf("full report %q", full)
	}
	rows, err := archive.Open(o.ArchivePath, []byte(passphrase))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !strings.Contains(string(rows[0]), funded) || !strings.Contains(string(rows[0]), s.vectors[2].Envelope) {
		t.Fatalf("archive rows %d do not hold the funded wallet", len(rows))
	}
	if _, err := archive.Open(o.ArchivePath, []byte(passphrase+" wrong")); err == nil {
		t.Fatal("archive opened with the wrong passphrase")
	}
	if after := testsupport.Fingerprint(t, s.pg.DB); !reflect.DeepEqual(before, after) {
		t.Fatal("rehearsal wrote to the source database")
	}
	if n := databases(t, s.pg); n != 0 {
		t.Fatalf("%d temporary databases left behind", n)
	}
}

func TestRehearseStopsOnMismatch(t *testing.T) {
	s := newSource(t, 3)
	wrong := s.vectors[0]
	wrong.Address = s.vectors[2].Address
	bad := testsupport.InsertWallet(t, s.pg.DB, wrong, "standard")
	testsupport.InsertWallet(t, s.pg.DB, s.vectors[1], "agent")
	s.reader = s.pg.ReadOnlyRole(t, readerRole)
	o := options(t, s)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	r, err := rehearsal.Rehearse(ctx, o)
	var we *migrate.WalletError
	if !errors.As(err, &we) || we.WalletID != bad || we.Stage != "open" || !errors.Is(err, envelope.ErrAddressMismatch) {
		t.Fatalf("mismatch did not stop the rehearsal: %v", err)
	}
	if r.Wallets != 2 || r.Eligible != 2 || r.Report != (migrate.Report{Read: 1}) {
		t.Fatalf("report %s", r)
	}
	if n := databases(t, s.pg); n != 0 {
		t.Fatalf("%d temporary databases left behind", n)
	}
}

func TestLoadOptionsRequiresEveryInput(t *testing.T) {
	env := map[string]string{
		rehearsal.EnvSourceURL:            "postgres://ceremony_reader@127.0.0.1:1/wallets",
		rehearsal.EnvGatewayMigrationsDir: "/nonexistent/migrations",
		rehearsal.EnvAdminURL:             "postgres://postgres@127.0.0.1:1/postgres",
		rehearsal.EnvAttestorBin:          "/nonexistent/attestor",
		archive.EnvPath:                   "/nonexistent/funded.archive",
		archive.EnvPassphrase:             passphrase,
		envelope.EnvMasterKey:             "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	}
	o, err := rehearsal.LoadOptions(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	o.Wipe()
	for name := range env {
		missing := func(k string) string {
			if k == name {
				return ""
			}
			return env[k]
		}
		if _, err := rehearsal.LoadOptions(missing); err == nil {
			t.Fatalf("options loaded without %s", name)
		}
	}
}
