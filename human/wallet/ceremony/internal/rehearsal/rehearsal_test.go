package rehearsal_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/archive"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/envelope"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/migrate"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/rehearsal"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/testsupport"
)

const passphrase = "rehearsal archive passphrase of adequate length"

var (
	buildOnce sync.Once
	buildBin  string
	buildErr  error
)

func attestorBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "ceremony-attestor-bin-")
		if err != nil {
			buildErr = err
			return
		}
		buildBin = filepath.Join(dir, "attestor")
		cmd := exec.Command("go", "build", "-o", buildBin, "./cmd/attestor")
		cmd.Dir = filepath.Join("..", "..", "..", "attestor")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = errors.New(string(out) + err.Error())
		}
	})
	if buildErr != nil {
		t.Fatalf("build attestor daemon: %v", buildErr)
	}
	return buildBin
}

func pgBinDir() string {
	if d := os.Getenv("PG_BIN_DIR"); d != "" {
		return d
	}
	return testsupport.DefaultPGBinDir
}

func dump(t *testing.T, pg *testsupport.Postgres) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wallets.dump")
	out, err := exec.Command(filepath.Join(pgBinDir(), "pg_dump"), "--format=custom", "--file", path, pg.URL).CombinedOutput()
	if err != nil {
		t.Fatalf("pg_dump: %v\n%s", err, out)
	}
	return path
}

func options(t *testing.T, pg *testsupport.Postgres, master []byte) rehearsal.Options {
	return rehearsal.Options{
		DumpPath:    dump(t, pg),
		AdminURL:    pg.URL,
		AttestorBin: attestorBinary(t),
		PGBinDir:    pgBinDir(),
		MasterKey:   master,
		ArchivePath: filepath.Join(t.TempDir(), "funded.archive"),
		Passphrase:  []byte(passphrase),
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

func TestRehearseMigratesEveryWalletThroughFiveDaemons(t *testing.T) {
	pg := testsupport.StartPostgres(t)
	testsupport.ApplyGatewayMigrations(t, pg.DB)
	master, masterB64 := testsupport.MasterKey(t)
	vs := testsupport.GatewayVectors(t, masterB64, 3)
	testsupport.InsertWallet(t, pg.DB, vs[0], "standard")
	testsupport.InsertWallet(t, pg.DB, vs[1], "agent")
	funded := testsupport.InsertWallet(t, pg.DB, vs[2], "funded")
	testsupport.InsertFundedAccount(t, pg.DB, funded)
	o := options(t, pg, master)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	r, err := rehearsal.Rehearse(ctx, o)
	if err != nil {
		t.Fatalf("rehearse: %v (report %s)", err, r)
	}
	want := rehearsal.Report{Wallets: 3, Eligible: 2, FundedArchived: 1, AlreadyMigrated: 0,
		Report: migrate.Report{Read: 2, Verified: 2, Imported: 2, Refreshed: 2, TestSigned: 2, Matched: 2}}
	if r != want {
		t.Fatalf("report %s, want %s", r, want)
	}
	if got := r.String(); got != "wallets=3 eligible=2 funded_archived=1 already_migrated=0 read=2 verified=2 imported=2 refreshed=2 test_signed=2 matched=2" {
		t.Fatalf("report line %q", got)
	}
	rows, err := archive.Open(o.ArchivePath, []byte(passphrase))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !strings.Contains(string(rows[0]), funded) || !strings.Contains(string(rows[0]), vs[2].Envelope) {
		t.Fatalf("archive rows %d do not hold the funded wallet", len(rows))
	}
	if _, err := archive.Open(o.ArchivePath, []byte(passphrase+" wrong")); err == nil {
		t.Fatal("archive opened with the wrong passphrase")
	}
	var migrated int
	if err := pg.DB.QueryRow(`select count(*) from wallets where migrated_at is not null`).Scan(&migrated); err != nil {
		t.Fatal(err)
	}
	if migrated != 0 {
		t.Fatal("rehearsal wrote to the source database")
	}
	if n := databases(t, pg); n != 0 {
		t.Fatalf("%d temporary databases left behind", n)
	}
}

func TestRehearseStopsOnMismatch(t *testing.T) {
	pg := testsupport.StartPostgres(t)
	testsupport.ApplyGatewayMigrations(t, pg.DB)
	master, masterB64 := testsupport.MasterKey(t)
	vs := testsupport.GatewayVectors(t, masterB64, 3)
	wrong := vs[0]
	wrong.Address = vs[2].Address
	bad := testsupport.InsertWallet(t, pg.DB, wrong, "standard")
	testsupport.InsertWallet(t, pg.DB, vs[1], "agent")
	o := options(t, pg, master)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	r, err := rehearsal.Rehearse(ctx, o)
	var we *migrate.WalletError
	if !errors.As(err, &we) || we.WalletID != bad || we.Stage != "open" || !errors.Is(err, envelope.ErrAddressMismatch) {
		t.Fatalf("mismatch did not stop the rehearsal: %v", err)
	}
	want := rehearsal.Report{Wallets: 2, Eligible: 2, Report: migrate.Report{Read: 1}}
	if r != want {
		t.Fatalf("report %s, want %s", r, want)
	}
	if n := databases(t, pg); n != 0 {
		t.Fatalf("%d temporary databases left behind", n)
	}
}

func TestLoadOptionsRequiresEveryInput(t *testing.T) {
	env := map[string]string{
		rehearsal.EnvDump:        "/nonexistent/wallets.dump",
		rehearsal.EnvAdminURL:    "postgres://postgres@127.0.0.1:1/postgres",
		rehearsal.EnvAttestorBin: "/nonexistent/attestor",
		archive.EnvPath:          "/nonexistent/funded.archive",
		archive.EnvPassphrase:    passphrase,
		envelope.EnvMasterKey:    "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
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
