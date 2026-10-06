package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/ceremony/internal/archive"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/ceremony/internal/attestor"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/ceremony/internal/envelope"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/ceremony/internal/migrate"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/ceremony/internal/rehearsal"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/ceremony/internal/testsupport"
)

func invoke(env map[string]string, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(context.Background(), args, func(k string) string { return env[k] }, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestPlanArchiveDeliverThroughTheCommand(t *testing.T) {
	pg := testsupport.StartPostgres(t)
	testsupport.ApplyGatewayMigrations(t, pg.DB)
	_, masterB64 := testsupport.MasterKey(t)
	vs := testsupport.GatewayVectors(t, masterB64, 5)
	standard := testsupport.InsertWallet(t, pg.DB, vs[0], "standard")
	testsupport.InsertWallet(t, pg.DB, vs[1], "agent")
	funded := testsupport.InsertWallet(t, pg.DB, vs[2], "funded")
	testsupport.InsertFundedAccount(t, pg.DB, funded)
	done := testsupport.InsertWallet(t, pg.DB, vs[3], "standard")
	testsupport.MarkMigrated(t, pg.DB, done, migrate.SecpKeyID(done))

	env := map[string]string{migrate.EnvDatabaseURL: pg.URL}
	code, out, errOut := invoke(env, "plan")
	if code != 0 {
		t.Fatalf("plan exit %d: %s", code, errOut)
	}
	if out != "wallets=4 eligible=2 funded=1 already_migrated=1\n" {
		t.Fatalf("plan printed %q", out)
	}

	nodes := testsupport.StartNodes(t)
	for k, v := range nodes.Env() {
		env[k] = v
	}
	env[envelope.EnvMasterKey] = masterB64
	env[archive.EnvPassphrase] = "operator held archive passphrase"
	env[archive.EnvPath] = filepath.Join(t.TempDir(), "funded.archive")

	code, _, errOut = invoke(env, "deliver")
	if code != 1 || !strings.Contains(errOut, "archive") {
		t.Fatalf("deliver before archive exit %d: %s", code, errOut)
	}

	code, out, errOut = invoke(env, "archive", "--report-only-counts")
	if code != 1 || out != "" || !strings.Contains(errOut, "archive: open") {
		t.Fatalf("archive counts before archive exit %d out %q: %s", code, out, errOut)
	}
	code, out, errOut = invoke(env, "deliver", "--report-only-counts")
	if code != 1 || out != "wallets=4 eligible=3 funded=1 migrated=1 eligible_not_migrated=2\n" || !strings.Contains(errOut, "eligible wallets are not migrated: 2 of 3") {
		t.Fatalf("deliver counts before deliver exit %d out %q: %s", code, out, errOut)
	}
	for _, n := range nodes.IDs {
		if nodes.Verifications[n] != 0 || nodes.Signs[n] != 0 || nodes.Holds(n, migrate.SecpKeyID(standard)) {
			t.Fatalf("deliver --report-only-counts reached node %s", n)
		}
	}
	if _, err := os.Stat(env[archive.EnvPath]); !os.IsNotExist(err) {
		t.Fatalf("counts runs wrote an archive: %v", err)
	}

	code, out, errOut = invoke(env, "archive")
	if code != 0 || out != "funded_archived=1 verified=true\n" {
		t.Fatalf("archive exit %d out %q: %s", code, out, errOut)
	}
	sealed, err := os.ReadFile(env[archive.EnvPath])
	if err != nil {
		t.Fatal(err)
	}
	archiveCounts := fmt.Sprintf("funded_rows=1 archive_rows=1 verified=true archive_sha256=%x\n", sha256.Sum256(sealed))
	code, out, errOut = invoke(env, "archive", "--report-only-counts")
	if code != 0 || out != archiveCounts {
		t.Fatalf("archive counts exit %d out %q: %s", code, out, errOut)
	}

	code, out, errOut = invoke(env, "deliver")
	if code != 0 {
		t.Fatalf("deliver exit %d: %s", code, errOut)
	}
	if out != "eligible=2 funded_archived=1 read=2 verified=2 imported=2 refreshed=2 test_signed=2 matched=2\n" {
		t.Fatalf("deliver printed %q", out)
	}
	if strings.Contains(out+errOut, masterB64) {
		t.Fatal("output carries the master key")
	}
	for _, n := range nodes.IDs[:attestor.SignQuorum] {
		if nodes.Verifications[n] != 4 || nodes.Signs[n] != 0 {
			t.Fatalf("node %s granted %d verifications and %d owner signatures", n, nodes.Verifications[n], nodes.Signs[n])
		}
	}
	if info, err := os.Stat(env[archive.EnvPath]); err != nil || info.Size() == 0 {
		t.Fatalf("funded archive missing after deliver: %v", err)
	}

	code, out, _ = invoke(env, "plan")
	if code != 0 || out != "wallets=4 eligible=0 funded=1 already_migrated=3\n" {
		t.Fatalf("plan after deliver exit %d printed %q", code, out)
	}
	code, out, errOut = invoke(env, "deliver", "--report-only-counts")
	if code != 0 || out != "wallets=4 eligible=3 funded=1 migrated=3 eligible_not_migrated=0\n" {
		t.Fatalf("deliver counts after deliver exit %d out %q: %s", code, out, errOut)
	}
	code, out, errOut = invoke(env, "archive", "--report-only-counts")
	if code != 0 || out != archiveCounts {
		t.Fatalf("archive counts after deliver exit %d out %q: %s", code, out, errOut)
	}

	late := testsupport.InsertWallet(t, pg.DB, vs[4], "funded")
	testsupport.InsertFundedAccount(t, pg.DB, late)
	code, out, errOut = invoke(env, "archive", "--report-only-counts")
	if code != 1 || !strings.HasPrefix(out, "funded_rows=2 archive_rows=1 verified=false archive_sha256=") || !strings.Contains(errOut, archive.ErrVerify.Error()) {
		t.Fatalf("archive counts with a new funded row exit %d out %q: %s", code, out, errOut)
	}
}

func TestDeliverExitsNonZeroOnMismatch(t *testing.T) {
	pg := testsupport.StartPostgres(t)
	testsupport.ApplyGatewayMigrations(t, pg.DB)
	_, masterB64 := testsupport.MasterKey(t)
	vs := testsupport.GatewayVectors(t, masterB64, 1)
	id := testsupport.InsertWallet(t, pg.DB, vs[0], "standard")
	nodes := testsupport.StartNodes(t)
	nodes.CorruptSigning(migrate.SecpKeyID(id))
	env := nodes.Env()
	env[migrate.EnvDatabaseURL] = pg.URL
	env[envelope.EnvMasterKey] = masterB64
	env[archive.EnvPassphrase] = "operator held archive passphrase"
	env[archive.EnvPath] = filepath.Join(t.TempDir(), "funded.archive")
	if code, _, errOut := invoke(env, "archive"); code != 0 {
		t.Fatalf("archive exit %d: %s", code, errOut)
	}
	code, out, errOut := invoke(env, "deliver")
	if code != 1 || !strings.Contains(errOut, migrate.ErrAddressMismatch.Error()+": wallet "+id+" stored "+common.HexToAddress(vs[0].Address).Hex()+" recovered ") {
		t.Fatalf("deliver exit %d: %s", code, errOut)
	}
	if out != "eligible=1 funded_archived=0 read=1 verified=1 imported=1 refreshed=1 test_signed=1 matched=0\n" {
		t.Fatalf("deliver printed %q", out)
	}
	var migrated int
	if err := pg.DB.QueryRow(`select count(*) from wallets where migrated_at is not null`).Scan(&migrated); err != nil {
		t.Fatal(err)
	}
	if migrated != 0 {
		t.Fatalf("%d wallets marked migrated after a refused deliver", migrated)
	}
}

func TestUsageAndMissingEnvironment(t *testing.T) {
	if code, _, _ := invoke(map[string]string{}); code != 2 {
		t.Fatalf("no subcommand exit %d", code)
	}
	if code, _, _ := invoke(map[string]string{}, "export"); code != 2 {
		t.Fatalf("unknown subcommand exit %d", code)
	}
	for _, args := range [][]string{
		{"plan", "--report-only-counts"},
		{"deliver", "--delta"},
		{"archive", "--force"},
		{"rehearse", "--delta"},
		{"move", "--report-only-counts"},
		{"deliver", "--report-only-counts", "--report-only-counts"},
		{"move", "--delta", "extra"},
	} {
		code, out, errOut := invoke(map[string]string{}, args...)
		if code != 2 || out != "" || !strings.Contains(errOut, "usage: ceremony") {
			t.Fatalf("%v exit %d: %s", args, code, errOut)
		}
	}
	for _, args := range [][]string{{"move"}, {"move", "--delta"}} {
		code, _, errOut := invoke(map[string]string{}, args...)
		if code != 1 || !strings.Contains(errOut, rehearsal.EnvSourceURL) {
			t.Fatalf("%v without configuration exit %d: %s", args, code, errOut)
		}
	}
	code, _, errOut := invoke(map[string]string{}, "deliver", "--report-only-counts")
	if code != 1 || !strings.Contains(errOut, migrate.EnvDatabaseURL) {
		t.Fatalf("deliver counts without database exit %d: %s", code, errOut)
	}
	code, _, errOut = invoke(map[string]string{migrate.EnvDatabaseURL: "postgres://postgres@127.0.0.1:1/postgres"}, "archive", "--report-only-counts")
	if code != 1 || !strings.Contains(errOut, archive.EnvPath) {
		t.Fatalf("archive counts without an archive path exit %d: %s", code, errOut)
	}
	code, out, errOut := invoke(map[string]string{}, "rehearse", "--report-only-counts")
	if code != 1 || out != "" || !strings.Contains(errOut, "rehearsal: invalid configuration") {
		t.Fatalf("rehearse counts without configuration exit %d: %s", code, errOut)
	}
	code, _, errOut = invoke(map[string]string{}, "plan")
	if code != 1 || !strings.Contains(errOut, migrate.EnvDatabaseURL) {
		t.Fatalf("plan without database exit %d: %s", code, errOut)
	}
	code, out, errOut = invoke(map[string]string{}, "rehearse")
	if code != 1 || out != "" || !strings.Contains(errOut, "rehearsal: invalid configuration") {
		t.Fatalf("rehearse without configuration exit %d: %s", code, errOut)
	}
}

func TestMoveDeltaAndRehearseThroughTheCommand(t *testing.T) {
	pg := testsupport.StartPostgres(t)
	testsupport.ApplyGatewayLedger(t, pg.DB, "005_agent_actions.sql")
	_, masterB64 := testsupport.MasterKey(t)
	vs := testsupport.GatewayVectors(t, masterB64, 3)
	standard := testsupport.InsertWallet(t, pg.DB, vs[0], "standard")
	testsupport.InsertWallet(t, pg.DB, vs[1], "agent")
	funded := testsupport.InsertWallet(t, pg.DB, vs[2], "funded")
	testsupport.InsertFundedAccount(t, pg.DB, funded)
	testsupport.InsertSignature(t, pg.DB, standard)
	reader := pg.ReadOnlyRole(t, "ceremony_reader")
	target := pg.Database(t, "gateway_platform")
	env := map[string]string{
		rehearsal.EnvSourceURL:            reader,
		migrate.EnvDatabaseURL:            target.URL,
		rehearsal.EnvGatewayMigrationsDir: testsupport.GatewayMigrationsDir(t),
	}

	code, out, errOut := invoke(env, "move")
	if code != 0 {
		t.Fatalf("move exit %d: %s", code, errOut)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 14 || lines[13] != "tables=13 rows=18 ledger_before=5 applied_after=4" {
		t.Fatalf("move printed %q", out)
	}
	for _, l := range lines[:13] {
		f := strings.Fields(l)
		if len(f) != 5 || strings.TrimPrefix(f[1], "source_") != strings.TrimPrefix(f[2], "target_") || strings.TrimPrefix(f[3], "source_") != strings.TrimPrefix(f[4], "target_") {
			t.Fatalf("move line %q", l)
		}
	}
	if !strings.Contains(out, "table=wallets source_rows=3 target_rows=3 source_sha256=") {
		t.Fatalf("move printed %q", out)
	}
	if strings.Contains(out+errOut, vs[0].Address) || strings.Contains(out+errOut, "127.0.0.1") {
		t.Fatal("move output carries an address or a host")
	}
	code, _, errOut = invoke(env, "move")
	if code != 1 || !strings.Contains(errOut, rehearsal.ErrTargetNotEmpty.Error()) {
		t.Fatalf("second move exit %d: %s", code, errOut)
	}
	code, out, errOut = invoke(env, "move", "--delta")
	if code != 0 || !strings.HasSuffix(out, "tables=11 dropped=2 moved=0\n") {
		t.Fatalf("delta exit %d out %q: %s", code, out, errOut)
	}
	code, out, errOut = invoke(env, "plan")
	if code != 0 || out != "wallets=3 eligible=2 funded=1 already_migrated=0\n" {
		t.Fatalf("plan on the moved database exit %d out %q: %s", code, out, errOut)
	}

	env[rehearsal.EnvAdminURL] = pg.URL
	env[rehearsal.EnvAttestorBin] = testsupport.AttestorBinary(t)
	env[envelope.EnvMasterKey] = masterB64
	env[archive.EnvPassphrase] = "operator held archive passphrase"
	summary := "wallets=3 eligible=2 funded_archived=1 already_migrated=0 read=2 verified=2 imported=2 refreshed=2 test_signed=2 matched=2\ntables=13 rows=18 ledger_before=5 applied_after=4\n"
	env[archive.EnvPath] = filepath.Join(t.TempDir(), "full.archive")
	code, out, errOut = invoke(env, "rehearse")
	if code != 0 || !strings.HasPrefix(out, summary) || !strings.Contains(out, "\ntable=wallets source_rows=3 target_rows=3 source_sha256=") {
		t.Fatalf("rehearse exit %d out %q: %s", code, out, errOut)
	}
	env[archive.EnvPath] = filepath.Join(t.TempDir(), "counts.archive")
	code, out, errOut = invoke(env, "rehearse", "--report-only-counts")
	if code != 0 || !strings.HasPrefix(out, summary) || !strings.Contains(out, "\ntable=wallets source_rows=3 target_rows=3\n") || strings.Contains(out, "sha256") {
		t.Fatalf("rehearse counts exit %d out %q: %s", code, out, errOut)
	}
}
