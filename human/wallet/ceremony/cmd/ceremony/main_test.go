package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/archive"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/envelope"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/migrate"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/testsupport"
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
	vs := testsupport.GatewayVectors(t, masterB64, 4)
	testsupport.InsertWallet(t, pg.DB, vs[0], "standard")
	testsupport.InsertWallet(t, pg.DB, vs[1], "agent")
	funded := testsupport.InsertWallet(t, pg.DB, vs[2], "funded")
	testsupport.InsertFundedAccount(t, pg.DB, funded)
	done := testsupport.InsertWallet(t, pg.DB, vs[3], "standard")
	if _, err := pg.DB.Exec(`update wallets set migrated_at = now() where id = $1::uuid`, done); err != nil {
		t.Fatal(err)
	}

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

	code, out, errOut = invoke(env, "archive")
	if code != 0 || out != "funded_archived=1 verified=true\n" {
		t.Fatalf("archive exit %d out %q: %s", code, out, errOut)
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

	code, out, _ = invoke(env, "plan")
	if code != 0 || out != "wallets=4 eligible=0 funded=1 already_migrated=3\n" {
		t.Fatalf("plan after deliver exit %d printed %q", code, out)
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
	if code != 1 || !strings.Contains(errOut, "does not recover the stored address") {
		t.Fatalf("deliver exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "matched=0") {
		t.Fatalf("deliver printed %q", out)
	}
}

func TestUsageAndMissingEnvironment(t *testing.T) {
	if code, _, _ := invoke(map[string]string{}); code != 2 {
		t.Fatalf("no subcommand exit %d", code)
	}
	if code, _, _ := invoke(map[string]string{}, "export"); code != 2 {
		t.Fatalf("unknown subcommand exit %d", code)
	}
	code, _, errOut := invoke(map[string]string{}, "plan")
	if code != 1 || !strings.Contains(errOut, migrate.EnvDatabaseURL) {
		t.Fatalf("plan without database exit %d: %s", code, errOut)
	}
}
