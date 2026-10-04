package migrate_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/attestor"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/envelope"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/migrate"
	"github.com/sidiora-labs/paxeer-network/human/wallet/ceremony/internal/testsupport"
)

func setup(t *testing.T) *sql.DB {
	t.Helper()
	pg := testsupport.StartPostgres(t)
	testsupport.ApplyGatewayMigrations(t, pg.DB)
	return pg.DB
}

func migratedIDs(t *testing.T, db *sql.DB) map[string]bool {
	t.Helper()
	rows, err := db.Query(`select id::text from wallets where migrated_at is not null`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out[id] = true
	}
	return out
}

func TestPlanMigrationExcludesFundedAndMigrated(t *testing.T) {
	db := setup(t)
	_, masterB64 := testsupport.MasterKey(t)
	vs := testsupport.GatewayVectors(t, masterB64, 5)
	standard := testsupport.InsertWallet(t, db, vs[0], "standard")
	agent := testsupport.InsertWallet(t, db, vs[1], "agent")
	funded := testsupport.InsertWallet(t, db, vs[2], "funded")
	testsupport.InsertFundedAccount(t, db, funded)
	fundedKindOnly := testsupport.InsertWallet(t, db, vs[3], "funded")
	done := testsupport.InsertWallet(t, db, vs[4], "standard")
	testsupport.MarkMigrated(t, db, done, migrate.SecpKeyID(done))

	ctx := context.Background()
	wallets, err := migrate.ReadWallets(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(wallets) != 5 {
		t.Fatalf("read %d wallets", len(wallets))
	}
	for _, w := range wallets {
		if w.EncryptedPrivateKey == "" || !common.IsHexAddress(w.Address) || w.ChainID != 125 || w.KeyVersion != 1 {
			t.Fatalf("wallet %s read incompletely", w.ID)
		}
	}
	plan, err := migrate.PlanMigration(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Total != 5 || plan.AlreadyMigrated != 1 || len(plan.Funded) != 2 || len(plan.Eligible) != 2 {
		t.Fatalf("plan total=%d migrated=%d funded=%d eligible=%d", plan.Total, plan.AlreadyMigrated, len(plan.Funded), len(plan.Eligible))
	}
	eligible := map[string]bool{plan.Eligible[0].ID: true, plan.Eligible[1].ID: true}
	if !eligible[standard] || !eligible[agent] {
		t.Fatal("standard and agent wallets are not the eligible set")
	}
	fundedSet := map[string]bool{plan.Funded[0].ID: true, plan.Funded[1].ID: true}
	if !fundedSet[funded] || !fundedSet[fundedKindOnly] {
		t.Fatal("funded wallets are not excluded")
	}
}

func TestPlanMigrationRequiresMigratedColumn(t *testing.T) {
	db := setup(t)
	if _, err := db.Exec(`alter table wallets drop column migrated_at`); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.PlanMigration(context.Background(), db); !errors.Is(err, migrate.ErrSchema) {
		t.Fatalf("missing column: %v", err)
	}
}

func TestDeliverMigratesEveryEligibleWallet(t *testing.T) {
	db := setup(t)
	master, masterB64 := testsupport.MasterKey(t)
	vs := testsupport.GatewayVectors(t, masterB64, 4)
	var ids []string
	for i := 0; i < 3; i++ {
		ids = append(ids, testsupport.InsertWallet(t, db, vs[i], []string{"standard", "agent", "standard"}[i]))
	}
	funded := testsupport.InsertWallet(t, db, vs[3], "funded")
	testsupport.InsertFundedAccount(t, db, funded)

	nodes := testsupport.StartNodes(t)
	client, err := attestor.New(nodes.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	plan, err := migrate.PlanMigration(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	report, err := migrate.Deliver(ctx, db, client, master, plan)
	if err != nil {
		t.Fatal(err)
	}
	want := migrate.Report{Read: 3, Verified: 3, Imported: 3, Refreshed: 3, TestSigned: 3, Matched: 3}
	if report != want {
		t.Fatalf("report %+v want %+v", report, want)
	}
	migrated := migratedIDs(t, db)
	for _, id := range ids {
		if !migrated[id] {
			t.Fatalf("wallet %s not marked migrated", id)
		}
		for _, n := range nodes.IDs {
			if !nodes.Holds(n, migrate.SecpKeyID(id)) || !nodes.Holds(n, migrate.IdentityKeyID(id)) {
				t.Fatalf("node %s lacks a share of wallet %s", n, id)
			}
			if nodes.Epoch(n, migrate.SecpKeyID(id)) != 1 || nodes.Epoch(n, migrate.IdentityKeyID(id)) != 1 {
				t.Fatalf("node %s did not refresh wallet %s", n, id)
			}
		}
	}
	for _, n := range nodes.IDs[:attestor.SignQuorum] {
		if nodes.Verifications[n] != 6 || nodes.Signs[n] != 0 {
			t.Fatalf("node %s granted %d verifications and %d owner signatures", n, nodes.Verifications[n], nodes.Signs[n])
		}
	}
	if migrated[funded] || nodes.Holds("n1", migrate.SecpKeyID(funded)) {
		t.Fatal("funded wallet was delivered")
	}
	again, err := migrate.PlanMigration(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Eligible) != 0 || again.AlreadyMigrated != 3 {
		t.Fatalf("second plan eligible=%d migrated=%d", len(again.Eligible), again.AlreadyMigrated)
	}
}

func TestDeliverStopsTheRunOnSignatureMismatch(t *testing.T) {
	db := setup(t)
	master, masterB64 := testsupport.MasterKey(t)
	vs := testsupport.GatewayVectors(t, masterB64, 3)
	for _, v := range vs {
		testsupport.InsertWallet(t, db, v, "standard")
	}
	nodes := testsupport.StartNodes(t)
	client, err := attestor.New(nodes.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	plan, err := migrate.PlanMigration(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	first, second, third := plan.Eligible[0], plan.Eligible[1], plan.Eligible[2]
	nodes.CorruptSigning(migrate.SecpKeyID(second.ID))

	report, err := migrate.Deliver(ctx, db, client, master, plan)
	var mm *migrate.MismatchError
	if !errors.As(err, &mm) || !errors.Is(err, migrate.ErrAddressMismatch) {
		t.Fatalf("mismatch: %v", err)
	}
	if mm.WalletID != second.ID || mm.Stored != common.HexToAddress(second.Address) || mm.Recovered == mm.Stored {
		t.Fatalf("mismatch error names %s %s %s", mm.WalletID, mm.Stored.Hex(), mm.Recovered.Hex())
	}
	if report.Read != 2 || report.TestSigned != 2 || report.Matched != 1 {
		t.Fatalf("report %+v", report)
	}
	migrated := migratedIDs(t, db)
	if !migrated[first.ID] || migrated[second.ID] || migrated[third.ID] || len(migrated) != 1 {
		t.Fatalf("migrated set %v", migrated)
	}
	if nodes.Holds("n1", migrate.SecpKeyID(third.ID)) {
		t.Fatal("run continued past the mismatch")
	}
}

func TestDeliverRefusesEnvelopeAddressMismatch(t *testing.T) {
	db := setup(t)
	master, masterB64 := testsupport.MasterKey(t)
	vs := testsupport.GatewayVectors(t, masterB64, 2)
	wrong := vs[0]
	wrong.Address = vs[1].Address
	id := testsupport.InsertWallet(t, db, wrong, "standard")
	nodes := testsupport.StartNodes(t)
	client, err := attestor.New(nodes.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	plan, err := migrate.PlanMigration(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	report, err := migrate.Deliver(ctx, db, client, master, plan)
	var we *migrate.WalletError
	if !errors.As(err, &we) || we.WalletID != id || we.Stage != "open" || !errors.Is(err, envelope.ErrAddressMismatch) {
		t.Fatalf("envelope mismatch: %v", err)
	}
	if report.Verified != 0 || report.Imported != 0 {
		t.Fatalf("report %+v", report)
	}
	for _, n := range nodes.IDs {
		if nodes.Holds(n, migrate.SecpKeyID(id)) {
			t.Fatal("key delivered after an envelope mismatch")
		}
	}
	if len(migratedIDs(t, db)) != 0 {
		t.Fatal("wallet marked migrated after an envelope mismatch")
	}
}

func TestLoadOptionsRequiresProtectedSealingKey(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(root, "journal-key")
	key := bytes.Repeat([]byte{0x36}, 32)
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"CEREMONY_ID": "retained-original-identity", "CEREMONY_JOURNAL_DIR": root, "CEREMONY_JOURNAL_KEY_FILE": keyPath, "CEREMONY_REHEARSAL_RECEIPT": filepath.Join(root, "counts.sealed"), "CEREMONY_RPC_URL": "http://127.0.0.1:8545"}
	get := func(k string) string { return values[k] }
	options, err := migrate.LoadOptions(get)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(options.JournalKey, key) || options.CeremonyID != values["CEREMONY_ID"] || options.Rehearsal {
		t.Fatal("loaded ceremony authority differs")
	}
	for i := range options.JournalKey {
		options.JournalKey[i] = 0
	}
	if err = os.Chmod(keyPath, 0640); err != nil {
		t.Fatal(err)
	}
	if _, err = migrate.LoadOptions(get); !errors.Is(err, migrate.ErrJournal) {
		t.Fatalf("readable key accepted: %v", err)
	}
	if err = os.Chmod(keyPath, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "key-link")
	if err = os.Symlink(keyPath, link); err != nil {
		t.Fatal(err)
	}
	values["CEREMONY_JOURNAL_KEY_FILE"] = link
	if _, err = migrate.LoadOptions(get); !errors.Is(err, migrate.ErrJournal) {
		t.Fatalf("symlink key accepted: %v", err)
	}
	values["CEREMONY_JOURNAL_KEY_FILE"] = keyPath
	values["CEREMONY_ID"] = "../other"
	if _, err = migrate.LoadOptions(get); !errors.Is(err, migrate.ErrJournal) {
		t.Fatalf("path-changing ceremony accepted: %v", err)
	}
	values["CEREMONY_ID"] = "retained-original-identity"
	values["CEREMONY_REHEARSAL_RECEIPT"] = "relative"
	if _, err = migrate.LoadOptions(get); !errors.Is(err, migrate.ErrJournal) {
		t.Fatalf("relative receipt accepted: %v", err)
	}
}

func TestLoadOptionsRejectsMissingOrTruncatedJournalKey(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(root, "key")
	values := map[string]string{"CEREMONY_ID": "retained", "CEREMONY_JOURNAL_DIR": root, "CEREMONY_JOURNAL_KEY_FILE": keyPath, "CEREMONY_REHEARSAL_RECEIPT": filepath.Join(root, "receipt")}
	get := func(k string) string { return values[k] }
	if _, err := migrate.LoadOptions(get); !errors.Is(err, migrate.ErrJournal) {
		t.Fatalf("missing key accepted: %v", err)
	}
	if err := os.WriteFile(keyPath, []byte{1, 2, 3}, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.LoadOptions(get); !errors.Is(err, migrate.ErrJournal) {
		t.Fatalf("truncated key accepted: %v", err)
	}
}
