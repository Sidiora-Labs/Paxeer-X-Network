package archive_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/ceremony/internal/archive"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/ceremony/internal/testsupport"
)

const passphrase = "correct horse battery staple ceremony"

func TestArchiveRoundTripAndVerify(t *testing.T) {
	pg := testsupport.StartPostgres(t)
	db := pg.DB
	testsupport.ApplyGatewayMigrations(t, db)
	_, masterB64 := testsupport.MasterKey(t)
	vs := testsupport.GatewayVectors(t, masterB64, 3)
	fundedKind := testsupport.InsertWallet(t, db, vs[0], "funded")
	testsupport.InsertFundedAccount(t, db, fundedKind)
	enrolled := testsupport.InsertWallet(t, db, vs[1], "standard")
	testsupport.InsertFundedAccount(t, db, enrolled)
	standard := testsupport.InsertWallet(t, db, vs[2], "standard")

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "funded.archive")
	res, err := archive.Archive(ctx, db, path, []byte(passphrase))
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows != 2 || res.Path != path {
		t.Fatalf("result %+v", res)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("archive mode %v", info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		if bytes.Contains(raw, []byte(v.Envelope)) || bytes.Contains(bytes.ToLower(raw), []byte(strings.ToLower(v.Address[2:]))) {
			t.Fatal("archive file carries a row in the clear")
		}
	}

	rows, err := archive.Open(path, []byte(passphrase))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]any{}
	for _, r := range rows {
		var m map[string]any
		if err := json.Unmarshal(r, &m); err != nil {
			t.Fatal(err)
		}
		got[m["id"].(string)] = m
	}
	for id, v := range map[string]testsupport.Vector{fundedKind: vs[0], enrolled: vs[1]} {
		m, ok := got[id]
		if !ok {
			t.Fatalf("wallet %s missing from the archive", id)
		}
		if m["encrypted_private_key"] != v.Envelope || m["address"] != v.Address {
			t.Fatalf("wallet %s archived with different columns", id)
		}
		for _, col := range []string{"user_id", "key_version", "chain_id", "kind", "created_at", "is_disabled", "migrated_at"} {
			if _, ok := m[col]; !ok {
				t.Fatalf("wallet %s archive lacks column %s", id, col)
			}
		}
	}
	if _, ok := got[standard]; ok {
		t.Fatal("standard wallet archived")
	}

	var count int
	if err := db.QueryRow(`select count(*) from wallets`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("archive changed the wallet count to %d", count)
	}

	if _, err := archive.Verify(ctx, db, path, []byte(passphrase)); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Open(path, []byte(passphrase+"x")); !errors.Is(err, archive.ErrDecrypt) {
		t.Fatalf("wrong passphrase: %v", err)
	}
	if _, err := archive.Archive(ctx, db, path, []byte(passphrase)); err == nil {
		t.Fatal("archive overwrote an existing file")
	}
	if _, err := archive.Archive(ctx, db, filepath.Join(t.TempDir(), "short"), []byte("short")); !errors.Is(err, archive.ErrPassphrase) {
		t.Fatalf("short passphrase: %v", err)
	}

	for _, off := range []int{8, 12, len(raw) - 1} {
		mutated := append([]byte{}, raw...)
		mutated[off] ^= 0x01
		if _, err := archive.Unseal(mutated, []byte(passphrase)); !errors.Is(err, archive.ErrDecrypt) {
			t.Fatalf("tampered byte %d: %v", off, err)
		}
	}
	badMagic := append([]byte{}, raw...)
	badMagic[0] ^= 0x01
	if _, err := archive.Unseal(badMagic, []byte(passphrase)); !errors.Is(err, archive.ErrFormat) {
		t.Fatalf("bad magic: %v", err)
	}

	if _, err := db.Exec(`update wallets set disabled_reason = 'changed' where id = $1::uuid`, enrolled); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Verify(ctx, db, path, []byte(passphrase)); !errors.Is(err, archive.ErrVerify) {
		t.Fatalf("verify after a row changed: %v", err)
	}
}

func TestLoadPassphraseAndPath(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	if _, err := archive.LoadPassphrase(getenv); !errors.Is(err, archive.ErrPassphrase) {
		t.Fatalf("unset passphrase: %v", err)
	}
	if _, err := archive.LoadPath(getenv); !errors.Is(err, archive.ErrPath) {
		t.Fatalf("unset path: %v", err)
	}
	env[archive.EnvPassphrase] = passphrase
	env[archive.EnvPath] = "/archive/out"
	p, err := archive.LoadPassphrase(getenv)
	if err != nil || string(p) != passphrase {
		t.Fatalf("passphrase: %v", err)
	}
	if path, err := archive.LoadPath(getenv); err != nil || path != "/archive/out" {
		t.Fatalf("path: %v", err)
	}
}
