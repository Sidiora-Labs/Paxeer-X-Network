package delivery

import (
	"context"
	"encoding/json"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestArchiveManifestRequiresEveryBlockIncludingEmpty(t *testing.T) {
	m := ArchiveManifest{Version: 1, ChainID: 125, SourceStream: "canonical", Deployment: "deployment", DecoderDigest: strings.Repeat("a", 64), ABIDigest: strings.Repeat("b", 64), CodeDigest: strings.Repeat("c", 64), FromHeight: 10, ToHeight: 11, RawCount: 1, Complete: true, Blocks: []ArchiveBlock{{10, "0x" + strings.Repeat("d", 64), 1, Hash([]byte("raw\n")), 4}, {11, "0x" + strings.Repeat("e", 64), 0, Hash(nil), 0}}}
	if err := ValidateArchiveManifest(m); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ArchiveManifest
	if json.Unmarshal(raw, &decoded) != nil || ValidateArchiveManifest(decoded) != nil {
		t.Fatal("roundtrip changed manifest")
	}
	m.Blocks = m.Blocks[:1]
	if ValidateArchiveManifest(m) == nil {
		t.Fatal("missing empty block accepted")
	}
	decoded.RawCount++
	if ValidateArchiveManifest(decoded) == nil {
		t.Fatal("incorrect source count accepted")
	}
	decoded.RawCount--
	decoded.Complete = false
	if ValidateArchiveManifest(decoded) == nil {
		t.Fatal("incomplete archive accepted")
	}
}

func TestArchiveActualCanonicalPostgresAndObjectStorage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(os.Getenv("DELIVERY_TEST_DSN"))
	if err != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 25432 || cfg.ConnConfig.Database != "kindle_fixture" {
		t.Fatal("explicit private DELIVERY_TEST_DSN required")
	}
	cfg.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	bucket := os.Getenv("DELIVERY_ARCHIVE_BUCKET")
	if !strings.HasPrefix(bucket, "kd-") {
		t.Fatal("explicit private kd- archive bucket required")
	}
	objects, err := storage.New(ctx, storage.Config{Endpoint: os.Getenv("DELIVERY_ARCHIVE_ENDPOINT"), AccessKeyID: os.Getenv("DELIVERY_ARCHIVE_ACCESS_KEY"), SecretAccessKey: os.Getenv("DELIVERY_ARCHIVE_SECRET_KEY"), Bucket: bucket})
	if err != nil {
		t.Fatal("actual object storage configuration required")
	}
	from, err := strconv.ParseInt(os.Getenv("DELIVERY_ARCHIVE_FROM"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	to, err := strconv.ParseInt(os.Getenv("DELIVERY_ARCHIVE_TO"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	schema := "kd_archive_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err = pool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	}()
	ddl, err := ReplayDDL(schema)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(pool, schema, fixturePolicy())
	if err != nil {
		t.Fatal(err)
	}
	archive, err := NewArchive(store, objects, schema, os.Getenv("DELIVERY_ARCHIVE_STREAM"), os.Getenv("DELIVERY_ARCHIVE_DEPLOYMENT"), os.Getenv("DELIVERY_ARCHIVE_DECODER_SHA"), os.Getenv("DELIVERY_ARCHIVE_ABI_SHA"), os.Getenv("DELIVERY_ARCHIVE_CODE_SHA"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, digest, err := archive.Capture(ctx, from, to)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := archive.Load(ctx, digest)
	if err != nil || loaded.RawCount != manifest.RawCount || loaded.ToHeight != to {
		t.Fatal("manifest changed", err)
	}
	var expected int64
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM indexer.indexer_raw_logs_v1 WHERE chain_id=125 AND height BETWEEN $1 AND $2", from, to).Scan(&expected); err != nil || expected != manifest.RawCount {
		t.Fatal("raw authority count differs", expected, manifest.RawCount, err)
	}
	for _, block := range manifest.Blocks {
		_, records, err := archive.ReadBlock(ctx, digest, block.Height)
		if err != nil || int64(len(records)) != block.RawCount {
			t.Fatal("block coverage changed", block, err)
		}
	}
	last := manifest.Blocks[len(manifest.Blocks)-1]
	if err = objects.Delete(ctx, archive.key("segments", last.SegmentHash)); err != nil {
		t.Fatal("private fixture segment removal failed")
	}
	if _, _, err = archive.ReadBlock(ctx, digest, last.Height); err == nil {
		t.Fatal("missing archive segment accepted")
	}
}
