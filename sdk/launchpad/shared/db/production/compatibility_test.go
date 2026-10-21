package production

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v1"
	"github.com/jackc/pgx/v5"
)

func TestPhase2ReaderCompatibility(t *testing.T) {
	pool, readerDSN := phase2MigrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	password := os.Getenv("L07_READER_PASSWORD")
	if password == "" {
		t.Fatal("dedicated real reader credentials required")
	}

	for _, role := range []string{"l07_reader_old", "l07_reader_new"} {
		exec(t, pool, "CREATE ROLE "+role+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS")
		var alterRole string
		if err := pool.QueryRow(ctx, "SELECT format('ALTER ROLE %I PASSWORD %L',$1::text,$2::text)", role, password).Scan(&alterRole); err != nil {
			t.Fatal(err)
		}
		exec(t, pool, alterRole)
		exec(t, pool, "GRANT USAGE ON SCHEMA stats TO "+role)
	}
	conn, err := pgx.Connect(ctx, readerDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	baselineSQL := `CREATE TABLE stats.phase2_reader_pools(pool_address text PRIMARY KEY,payload jsonb NOT NULL);CREATE VIEW stats.phase2_reader_pools_v1 AS SELECT pool_address,payload FROM stats.phase2_reader_pools;GRANT SELECT(pool_address,payload) ON stats.phase2_reader_pools_v1 TO l07_reader_old;`
	baseline := phase2RegisteredMigration(70010, baselineSQL, nil)
	registry := map[int64]MigrationManifest{baseline.MigrationID: baseline}
	if err = ApplyCoordinatedMigration(ctx, conn, baseline, []byte(baselineSQL), registry, baseline.InterfaceDigest, baseline.ApprovalManifestDigest, 1); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "spec", "production-system", "integration", "contracts", "v1", "examples.json"))
	if err != nil {
		t.Fatal(err)
	}
	var examples any
	if err = json.Unmarshal(raw, &examples); err != nil {
		t.Fatal(err)
	}
	var frozenPool []byte
	var find func(any)
	find = func(value any) {
		if frozenPool != nil {
			return
		}
		switch v := value.(type) {
		case map[string]any:
			if _, ok := v["virtualUsdlReserveRaw"]; ok {
				candidate, _ := json.Marshal(v)
				if interfaces.ValidateJSON("Pool", candidate) == nil {
					frozenPool = candidate
					return
				}
			}
			for _, item := range v {
				find(item)
			}
		case []any:
			for _, item := range v {
				find(item)
			}
		}
	}
	find(examples)
	if frozenPool == nil {
		t.Fatal("frozen genuine Pool contract example missing")
	}
	var row interfaces.Pool
	if err = json.Unmarshal(frozenPool, &row); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO stats.phase2_reader_pools VALUES($1,$2::jsonb)", row.PoolAddress, frozenPool); err != nil {
		t.Fatal(err)
	}
	reader := func(binaryEnv, role string) []byte {
		t.Helper()
		binary := os.Getenv(binaryEnv)
		if binary == "" || !filepath.IsAbs(binary) {
			t.Fatal("parent-built independent reader binary required", binaryEnv)
		}
		parsed, err := url.Parse(readerDSN)
		if err != nil {
			t.Fatal(err)
		}
		parsed.User = url.UserPassword(role, password)
		command := osexec.CommandContext(ctx, binary)
		command.Env = append(os.Environ(), "L07_READER_DSN="+parsed.String())
		var stderr bytes.Buffer
		command.Stderr = &stderr
		output, err := command.Output()
		if err != nil {
			t.Fatalf("independent %s reader exited: %v", role, err)
		}
		return output
	}
	oldBefore := reader("L07_OLD_READER_BIN", "l07_reader_old")
	expansion := `ALTER TABLE stats.phase2_reader_pools ADD COLUMN extra_note text,ADD COLUMN reader_version bigint NOT NULL DEFAULT 1;CREATE VIEW stats.phase2_reader_pools_v2 AS SELECT pool_address,payload,extra_note,reader_version FROM stats.phase2_reader_pools;GRANT SELECT(pool_address,payload,extra_note,reader_version) ON stats.phase2_reader_pools_v2 TO l07_reader_new`
	expanded := phase2RegisteredMigration(70011, expansion, []int64{70010})
	registry[expanded.MigrationID] = expanded
	if err = ApplyCoordinatedMigration(ctx, conn, expanded, []byte(expansion), registry, expanded.InterfaceDigest, expanded.ApprovalManifestDigest, 1); err != nil {
		t.Fatal(err)
	}
	oldAfter := reader("L07_OLD_READER_BIN", "l07_reader_old")
	newAfter := reader("L07_NEW_READER_BIN", "l07_reader_new")
	if !bytes.Equal(oldBefore, oldAfter) || !bytes.Equal(oldBefore, newAfter) {
		t.Fatal("expanded schema changed frozen Pool representation")
	}
	exec(t, pool, "UPDATE stats.phase2_reader_pools SET extra_note='backfilled',reader_version=2")
	if !bytes.Equal(oldBefore, reader("L07_NEW_READER_BIN", "l07_reader_new")) {
		t.Fatal("backfill changed preexisting representation")
	}
	if !strings.Contains(string(newAfter), `"virtualUsdlReserveRaw":"`) {
		t.Fatal("raw amounts lost decimal-string serialization")
	}
	oldBytes, err := os.ReadFile(os.Getenv("L07_OLD_READER_BIN"))
	if err != nil {
		t.Fatal(err)
	}
	newBytes, err := os.ReadFile(os.Getenv("L07_NEW_READER_BIN"))
	if err != nil {
		t.Fatal(err)
	}
	if Checksum(oldBytes) == Checksum(newBytes) {
		t.Fatal("independent old/new binaries required")
	}
	t.Logf("old_binary_sha256=%s new_binary_sha256=%s expansion_sha256=%s representation_sha256=%s old_exit=0 new_exit=0", Checksum(oldBytes), Checksum(newBytes), Checksum([]byte(expansion)), Checksum(oldBefore))
}
