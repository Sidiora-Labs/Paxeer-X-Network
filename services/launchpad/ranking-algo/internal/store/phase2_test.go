package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/ranker"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/queue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func TestCompletedRunActualPostgresAndRedis(t *testing.T) {
	dsn, artifact, artifactSHA := os.Getenv("L19_POSTGRES_DSN"), os.Getenv("L19_FROZEN_INPUT_FILE"), os.Getenv("L19_FROZEN_INPUT_SHA256")
	if dsn == "" || artifact == "" || artifactSHA == "" || os.Getenv("L19_REDIS_URL") == "" {
		t.Fatal("actual PostgreSQL, Redis and checksum-bound owner frozen input required")
	}
	raw, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != artifactSHA {
		t.Fatal("frozen input source differs")
	}
	var input ranker.InputSnapshot
	if err = json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	if input.Provenance == nil || input.Provenance.Validate(input) != nil {
		t.Fatal("owner source authority missing")
	}
	snapshots, err := ranker.ComputeRun(input, false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	schema, recovery := "ranking_l19_"+suffix, "recovery_l19_"+suffix
	legacy, err := os.ReadFile("../../../../../launchpad/spec/production-system/handoffs/L19/phase1/190000_ranking_snapshots.sql")
	if err != nil {
		t.Fatal(err)
	}
	ddl := strings.ReplaceAll(string(legacy), "ranking.", pgx.Identifier{schema}.Sanitize()+".")
	ddl = strings.Replace(ddl, "CREATE SCHEMA ranking;", "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()+";", 1)
	if _, err = pool.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	if _, err = pool.Exec(ctx, strings.ReplaceAll(Phase2DDL, "ranking.", pgx.Identifier{schema}.Sanitize()+".")); err != nil {
		t.Fatal(err)
	}
	template, err := os.ReadFile("../../../../../deploy/launchpad/production/recovery/epoch.sql")
	if err != nil {
		t.Fatal(err)
	}
	epochDDL, err := queue.RenderEpochSQL(recovery, string(template))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{recovery}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{recovery}.Sanitize()+" CASCADE")
	if _, err = pool.Exec(ctx, epochDDL); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO "+pgx.Identifier{recovery, "recovery_control"}.Sanitize()+"(epoch,region,revision,checkpoint_sha256)VALUES(1,'fixture-loopback',1,$1)", artifactSHA); err != nil {
		t.Fatal(err)
	}
	guard, err := queue.NewEpochGuard(recovery, 1, "fixture-loopback")
	if err != nil {
		t.Fatal(err)
	}
	publication, err := NewPublicationStore(pool, schema, guard)
	if err != nil {
		t.Fatal(err)
	}
	if err = publication.Complete(ctx, snapshots); err != nil {
		t.Fatal(err)
	}
	if err = publication.Complete(ctx, snapshots); err != nil {
		t.Fatal("idempotent retry", err)
	}
	for _, table := range []string{"category_heads", "snapshots", "publication_outbox"} {
		var count int
		if err = pool.QueryRow(ctx, "SELECT count(*) FROM "+publication.table(table)).Scan(&count); err != nil || count != 5 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	header, err := publication.Current(ctx, "trending")
	if err != nil {
		t.Fatal(err)
	}
	page, err := publication.Page(ctx, header, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if header.InputDigest != snapshots[0].InputDigest() || int64(len(page.Items)) > header.Total {
		t.Fatal("snapshot fields mixed")
	}
	options, err := redis.ParseURL(os.Getenv("L19_REDIS_URL"))
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	defer client.Close()
	cache, err := NewSnapshotCache(client, "l19_"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	if err = cache.Rebuild(ctx, publication.SnapshotStore, "trending"); err != nil {
		t.Fatal(err)
	}
	cached, err := cache.Get(ctx, header, 0, 100)
	if err != nil || len(cached.Items) != len(page.Items) {
		t.Fatal("durable cache rebuild", err)
	}
	if err = client.Del(ctx, cache.key(header, 0, 100)).Err(); err != nil {
		t.Fatal(err)
	}
	if err = cache.Rebuild(ctx, publication.SnapshotStore, "trending"); err != nil {
		t.Fatal(err)
	}
	if _, err = cache.Get(ctx, header, 0, 100); err != nil {
		t.Fatal("namespace cache loss recovery", err)
	}
	if _, err = pool.Exec(ctx, "UPDATE "+pgx.Identifier{recovery, "recovery_control"}.Sanitize()+" SET epoch=2"); err != nil {
		t.Fatal(err)
	}
	if err = publication.Complete(ctx, snapshots); err == nil {
		t.Fatal("stale recovery epoch wrote ranking effects")
	}
}

type phase3Fixture struct {
	ctx                   context.Context
	publication           *PublicationStore
	input                 ranker.InputSnapshot
	snapshots             []ranker.Snapshot
	dsn, schema, recovery string
}

func newPhase3Fixture(t *testing.T) phase3Fixture {
	t.Helper()
	dsn, artifact, artifactSHA := os.Getenv("L19_POSTGRES_DSN"), os.Getenv("L19_FROZEN_INPUT_FILE"), os.Getenv("L19_FROZEN_INPUT_SHA256")
	if dsn == "" || artifact == "" || artifactSHA == "" || os.Getenv("L19_REDIS_URL") == "" {
		t.Fatal("actual PostgreSQL, Redis and checksum-bound owner frozen input required")
	}
	raw, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != artifactSHA {
		t.Fatal("frozen input source differs")
	}
	var input ranker.InputSnapshot
	if err = json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	if input.Provenance == nil || input.Provenance.Validate(input) != nil {
		t.Fatal("owner source authority missing")
	}
	snapshots, err := ranker.ComputeRun(input, false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	schema, recovery := "ranking_l19_"+suffix, "recovery_l19_"+suffix
	legacy, err := os.ReadFile("../../../../../launchpad/spec/production-system/handoffs/L19/phase1/190000_ranking_snapshots.sql")
	if err != nil {
		t.Fatal(err)
	}
	ddl := strings.ReplaceAll(string(legacy), "ranking.", pgx.Identifier{schema}.Sanitize()+".")
	ddl = strings.Replace(ddl, "CREATE SCHEMA ranking;", "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()+";", 1)
	if _, err = pool.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	})
	if _, err = pool.Exec(ctx, strings.ReplaceAll(Phase2DDL, "ranking.", pgx.Identifier{schema}.Sanitize()+".")); err != nil {
		t.Fatal(err)
	}
	template, err := os.ReadFile("../../../../../deploy/launchpad/production/recovery/epoch.sql")
	if err != nil {
		t.Fatal(err)
	}
	epochDDL, err := queue.RenderEpochSQL(recovery, string(template))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{recovery}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{recovery}.Sanitize()+" CASCADE")
	})
	if _, err = pool.Exec(ctx, epochDDL); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO "+pgx.Identifier{recovery, "recovery_control"}.Sanitize()+"(epoch,region,revision,checkpoint_sha256)VALUES(1,'fixture-loopback',1,$1)", artifactSHA); err != nil {
		t.Fatal(err)
	}
	guard, err := queue.NewEpochGuard(recovery, 1, "fixture-loopback")
	if err != nil {
		t.Fatal(err)
	}
	publication, err := NewPublicationStore(pool, schema, guard)
	if err != nil {
		t.Fatal(err)
	}
	return phase3Fixture{ctx, publication, input, snapshots, dsn, schema, recovery}
}

func TestPhase3CommitWorker(t *testing.T) {
	marker := os.Getenv("L19_PHASE3_COMMIT_MARKER")
	if marker == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("L19_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	guard, err := queue.NewEpochGuard(os.Getenv("L19_PHASE3_RECOVERY_SCHEMA"), 1, "fixture-loopback")
	if err != nil {
		t.Fatal(err)
	}
	publication, err := NewPublicationStore(pool, os.Getenv("L19_PHASE3_RANKING_SCHEMA"), guard)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(os.Getenv("L19_FROZEN_INPUT_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	var input ranker.InputSnapshot
	if err = json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	snapshots, err := ranker.ComputeRun(input, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = publication.Complete(ctx, snapshots); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(marker, []byte("database-commit-complete"), 0600); err != nil {
		t.Fatal(err)
	}
	<-ctx.Done()
	t.Fatal("worker was not killed inside the commit/publication window")
}

func TestPhase3KilledWorkerRecoversOutboxAndDuplicatePublication(t *testing.T) {
	f := newPhase3Fixture(t)
	brokerURL, streamName := os.Getenv("L19_NATS_URL"), os.Getenv("L19_NATS_STREAM")
	u, err := url.Parse(brokerURL)
	if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") || streamName == "" {
		t.Fatal("isolated loopback real NATS stream required")
	}
	nc, err := nats.Connect(brokerURL, nats.NoReconnect(), nats.Timeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := js.Stream(f.ctx, streamName)
	if err != nil {
		t.Fatal(err)
	}
	info, err := stream.Info(f.ctx)
	if err != nil || info.State.Msgs != 0 || info.Config.Storage != jetstream.FileStorage || info.Config.Duplicates < time.Minute {
		t.Fatal("fresh file-backed stream with duplicate window required", err)
	}
	marker := filepath.Join(t.TempDir(), "committed")
	logPath := filepath.Join(t.TempDir(), "killed-worker.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	child := exec.CommandContext(f.ctx, os.Args[0], "-test.run=^TestPhase3CommitWorker$", "-test.timeout=25s")
	child.Env = append(os.Environ(), "L19_PHASE3_COMMIT_MARKER="+marker, "L19_PHASE3_RANKING_SCHEMA="+f.schema, "L19_PHASE3_RECOVERY_SCHEMA="+f.recovery)
	child.Stdout, child.Stderr = log, log
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill() }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err = os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("commit marker unavailable; worker log %s", logPath)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err = child.Wait(); err == nil {
		t.Fatal("worker exited without the requested kill")
	}
	var pending int
	if err = f.publication.pool.QueryRow(f.ctx, "SELECT count(*) FROM "+f.publication.table("publication_outbox")+" WHERE published_at IS NULL").Scan(&pending); err != nil || pending != 5 {
		t.Fatalf("committed intents=%d err=%v", pending, err)
	}
	freshPool, err := pgxpool.New(f.ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer freshPool.Close()
	guard, err := queue.NewEpochGuard(f.recovery, 1, "fixture-loopback")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := NewPublicationStore(freshPool, f.schema, guard)
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := natsdriver.NewPublisher(nc, streamName)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		worked, err := fresh.DispatchOnce(f.ctx, publisher, streamName, 3)
		if err != nil || !worked {
			t.Fatalf("restart dispatch %d: %v", i, err)
		}
	}
	if worked, err := fresh.DispatchOnce(f.ctx, publisher, streamName, 3); err != nil || worked {
		t.Fatal("outbox failed to drain", err)
	}
	info, err = stream.Info(f.ctx)
	if err != nil || info.State.Msgs != 5 {
		t.Fatalf("persisted events=%v err=%v", info, err)
	}
	for sequence := uint64(1); sequence <= 5; sequence++ {
		message, err := stream.GetMsg(f.ctx, sequence)
		if err != nil {
			t.Fatal(err)
		}
		var event struct {
			EventID string `json:"eventId"`
		}
		if err = json.Unmarshal(message.Data, &event); err != nil || event.EventID == "" {
			t.Fatal("ranking event identity missing", err)
		}
		for replay := 0; replay < 2; replay++ {
			ack, err := publisher.Publish(f.ctx, message.Subject, event.EventID, message.Data)
			if err != nil || ack.Sequence != sequence {
				t.Fatal("duplicate ranking changed broker identity", err)
			}
		}
	}
	info, err = stream.Info(f.ctx)
	if err != nil || info.State.Msgs != 5 {
		t.Fatal("duplicate ranking events changed persisted stream", err)
	}
	t.Logf("actual killed worker exit=%v log=%s recovered_categories=5 duplicate_replays=10", child.ProcessState.ExitCode(), logPath)
}

func TestPhase3RollbackEmptyAndCompetingCompletions(t *testing.T) {
	f := newPhase3Fixture(t)
	if err := f.publication.Complete(f.ctx, f.snapshots); err != nil {
		t.Fatal(err)
	}
	in := f.input
	in.RunID = uuid.NewString()
	in.CutoffTimeUTC++
	provenance := *in.Provenance
	provenance.WindowEnd++
	in.Provenance = &provenance
	run, err := ranker.ComputeRun(in, false)
	if err != nil {
		t.Fatal(err)
	}
	constraint := "reject_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = f.publication.pool.Exec(f.ctx, fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK(run_id <> '%s')", f.publication.table("publication_outbox"), pgx.Identifier{constraint}.Sanitize(), in.RunID)); err != nil {
		t.Fatal(err)
	}
	if err = f.publication.Complete(f.ctx, run); err == nil {
		t.Fatal("outbox failure committed a partial heavy run")
	}
	var count int
	if err = f.publication.pool.QueryRow(f.ctx, "SELECT count(*) FROM "+f.publication.table("snapshots")+" WHERE run_id=$1", in.RunID).Scan(&count); err != nil || count != 0 {
		t.Fatal("snapshot survived rollback", err)
	}
	for _, category := range ranker.HeavyCategoryNames() {
		h, err := f.publication.Current(f.ctx, category)
		if err != nil || h.RunID != f.input.RunID {
			t.Fatal("rollback changed category head", err)
		}
	}
	if _, err = f.publication.pool.Exec(f.ctx, "ALTER TABLE "+f.publication.table("publication_outbox")+" DROP CONSTRAINT "+pgx.Identifier{constraint}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	in.RunID = uuid.NewString()
	in.Candidates = []ranker.Candidate{}
	in.Provenance.UniverseDigest = ranker.CanonicalUniverse(in.Candidates)
	empty, err := ranker.ComputeRun(in, false)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { results <- f.publication.Complete(f.ctx, empty) }()
	}
	for i := 0; i < 2; i++ {
		if err = <-results; err != nil {
			t.Fatal("exact competing completion was not idempotent", err)
		}
	}
	for _, category := range ranker.HeavyCategoryNames() {
		h, err := f.publication.Current(f.ctx, category)
		if err != nil || h.Total != 0 || h.Revision != 2 {
			t.Fatal("completed empty category absent or duplicate revision", err)
		}
		page, err := f.publication.Page(f.ctx, h, 0, 100)
		if err != nil || page.Items == nil || len(page.Items) != 0 {
			t.Fatal("empty category confused with cold start", err)
		}
	}
	if err = f.publication.pool.QueryRow(f.ctx, "SELECT count(*) FROM "+f.publication.table("publication_outbox")+" WHERE run_id=$1", in.RunID).Scan(&count); err != nil || count != 5 {
		t.Fatal("duplicate completed empty intents", err)
	}
}
