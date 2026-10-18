package processor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMeasurementRejectsAbsentActualProviders(t *testing.T) {
	_, ownFile, _, _ := runtime.Caller(0)
	body, err := os.ReadFile(filepath.Join(filepath.Dir(ownFile), "measurement.go"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "candidate.go")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	fileHash := sha256.Sum256(body)
	source := hex.EncodeToString(fileHash[:])
	candidate := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s\n", len(path), path, source)))

	spec := IngestMeasurementSpec{CaptureMaxBytes: 16 << 20, RunID: "missing-real-providers", CandidateSHA256: hex.EncodeToString(candidate[:]), Profile: "steady", Region: "local-fixture", SourceFiles: map[string]string{path: source}, Duration: 5 * time.Minute, BatchBlocks: 1, MaxMemoryBytes: 1 << 30, MaxDatabaseConnections: 6, DeliverySchema: "indexer", HotPool: "0x" + strings.Repeat("2", 40), OutputPath: filepath.Join(t.TempDir(), "capture.ndjson")}
	if err := validateCandidateFiles(spec.SourceFiles, spec.CandidateSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := (&IngestMeasurementHarness{}).Run(context.Background(), spec); err == nil {
		t.Fatal("missing guarded providers generated measurement success")
	}
	if _, err := os.Stat(spec.OutputPath); !os.IsNotExist(err) {
		t.Fatal("absent provider generated evidence")
	}
	if err := os.WriteFile(path, append(body, []byte("\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateCandidateFiles(spec.SourceFiles, spec.CandidateSHA256); err == nil {
		t.Fatal("changed candidate retained measurement admission")
	}
}
func TestMeasurementStorageAndTraceUseActualPostgres(t *testing.T) {
	ctx, dsn, pool := replayPostgres(t)
	batch := prepare(t, pool)
	ddl, err := delivery.SchemaDDL("indexer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	trace := new(IngestSQLMeasurementTracer)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Tracer = trace
	observed, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer observed.Close()
	before, err := store.New(observed).MeasureIngestStorage(ctx, "indexer")
	if err != nil {
		t.Fatal(err)
	}
	if err = process(ctx, observed, batch); err != nil {
		t.Fatal(err)
	}
	after, err := store.New(observed).MeasureIngestStorage(ctx, "indexer")
	if err != nil {
		t.Fatal(err)
	}
	wal, err := store.WALBytesBetween(before.WALInsertLSN, after.WALInsertLSN)
	if err != nil || wal == 0 {
		t.Fatalf("actual PostgreSQL WAL not measured: %d %v", wal, err)
	}
	counters := trace.Snapshot()
	if counters.QueryCalls == 0 || counters.RawScalarInsertQueries != 4 || counters.CopyCalls != 0 {
		t.Fatalf("actual scalar ingest trace: %+v", counters)
	}
	digest, err := store.New(observed).MeasureCanonicalRange(ctx, "live", 101, 101)
	if err != nil || digest.RawCount != 4 || digest.TypedCount != 3 || digest.ProjectionCount != 3 || len(digest.ProjectionSHA256) != 64 || digest.BlockCount != 1 || len(digest.RawSHA256) != 64 || len(digest.TypedSHA256) != 64 {
		t.Fatalf("actual raw/typed reconciliation: %+v %v", digest, err)
	}
	fresh, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	restarted, err := store.New(fresh).MeasureCanonicalRange(ctx, "live", 101, 101)
	if err != nil || restarted != digest {
		t.Fatalf("fresh database connection digest changed: %+v %v", restarted, err)
	}
	plans, err := store.New(fresh).MeasureIngestPlans(ctx, "live", batch.Records[0], "0x"+strings.Repeat("2", 40))
	if err != nil || len(plans) != 5 {
		t.Fatalf("actual query plans missing: %d %v", len(plans), err)
	}
	for _, plan := range plans {
		if len(plan.Plan) == 0 {
			t.Fatal("empty PostgreSQL EXPLAIN plan")
		}
	}
	if after.RawPartitioned {
		t.Fatal("unpartitioned actual fixture falsely reported partition support")
	}
}
func TestMeasurementHistogramUsesEventWeightsAndTail(t *testing.T) {
	var h measurementHistogram
	h.add(400*time.Millisecond, 99)
	h.add(1200*time.Millisecond, 2)
	if h.p99() != 1200 {
		t.Fatalf("weighted tail hidden: %d", h.p99())
	}
	h.add(time.Hour, 1000)
	if h.p99() != 60001 {
		t.Fatal("overflow tail lost")
	}
	if _, err := store.WALBytesBetween("1/100", "0/FFFF"); err == nil {
		t.Fatal("restored WAL timeline regression counted as throughput")
	}
}
func TestRestoreCheckpointRejectsUnboundOrMalformedBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	if err := os.WriteFile(path, []byte(`{"CandidateSHA256":"bad"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRestoreCheckpoint(path, strings.Repeat("0", 64)); err == nil {
		t.Fatal("wrong checkpoint digest accepted")
	}
	if _, err := (&IngestMeasurementHarness{}).Reconstruct(context.Background(), IngestReconstructionSpec{}); err == nil {
		t.Fatal("absent archive/restore provider accepted")
	}
}
