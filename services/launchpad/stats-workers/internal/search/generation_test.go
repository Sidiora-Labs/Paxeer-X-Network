package search

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestGenerationCountDigestCutoverAndCurrentTombstones(t *testing.T) {
	pool := integratedDatabase(t)
	d := seedIndexed(t, pool, 1)
	s := Store{Pool: pool}
	ctx := context.Background()
	observed := time.Now().UTC()
	if err := s.BeginGeneration(ctx, "g2", 1, observed); err != nil {
		t.Fatal(err)
	}
	seedIndexed(t, pool, 2)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	count, digest, err := GenerationDigest(ctx, tx, "g2")
	rollbackSearch(tx)
	if err != nil || count != 2 {
		t.Fatal(count, err)
	}
	proof := GenerationProof{Generation: "g2", SourceWatermark: 1, SourceObservedAt: observed, Rows: count, Digest: strings.Repeat("0", 64)}
	if err = s.ActivateGeneration(ctx, proof); err == nil {
		t.Fatal("bad digest activated")
	}
	var active string
	if err = pool.QueryRow(ctx, "SELECT generation FROM stats_search.active_v2 WHERE singleton").Scan(&active); err != nil || active != "g1" {
		t.Fatal(active, err)
	}
	proof.Digest = digest
	if err = s.ActivateGeneration(ctx, proof); err != nil {
		t.Fatal(err)
	}
	applyFixture(t, pool, Change{Version: 2, Kind: "visibility", Revision: 5, PoolAddress: d.PoolAddress, Public: false, Tombstone: true})
	p, err := s.SearchIntegrated(ctx, IntegratedRequest{Request: Request{Query: d.PoolAddress, Kind: "pool", Limit: 25}, Sort: "address"})
	if err != nil || len(p.Items) != 0 {
		t.Fatal(p, err)
	}
}

func TestInterruptedGenerationCatchupHasAtomicRecovery(t *testing.T) {
	pool := integratedDatabase(t)
	ctx := context.Background()
	store := Store{Pool: pool}
	d := seedIndexed(t, pool, 1)
	observed := time.Now().UTC()
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.BeginGeneration(canceled, "interrupted", 1, observed); err == nil {
		t.Fatal("interrupted rebuild committed")
	}
	var abandoned int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM stats_search.generations_v2 WHERE generation='interrupted'").Scan(&abandoned); err != nil || abandoned != 0 {
		t.Fatal(abandoned, err)
	}
	if err := store.BeginGeneration(ctx, "g2", 1, observed); err != nil {
		t.Fatal(err)
	}
	seedIndexed(t, pool, 2)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	count, digest, err := GenerationDigest(ctx, tx, "g2")
	rollbackSearch(tx)
	if err != nil || count != 2 {
		t.Fatal(count, digest, err)
	}
	proof := GenerationProof{Generation: "g2", SourceWatermark: 1, SourceObservedAt: observed, Rows: count, Digest: digest}
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackSearch(blocker)
	if _, err = blocker.Exec(ctx, "SELECT generation FROM stats_search.generations_v2 WHERE generation='g2' FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err = store.ActivateGeneration(ctx, proof); err == nil {
		t.Fatal("locked catch-up generation activated")
	}
	if time.Since(started) > time.Second {
		t.Fatal("cutover lock wait exceeded bounded budget")
	}
	var active string
	if err = pool.QueryRow(ctx, "SELECT generation FROM stats_search.active_v2 WHERE singleton").Scan(&active); err != nil || active != "g1" {
		t.Fatal(active, err)
	}
	if err = blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	d.Name, d.NameKey, d.MetadataRevision = "During catchup", "during catchup", 2
	applyFixture(t, pool, Change{Version: 2, Kind: "metadata", Revision: 2, PoolAddress: d.PoolAddress, Document: &d})
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	count, digest, err = GenerationDigest(ctx, tx, "g2")
	rollbackSearch(tx)
	if err != nil {
		t.Fatal(err)
	}
	proof.Rows, proof.Digest = count, digest
	proof.SourceWatermark = 2
	proof.SourceObservedAt = time.Now().UTC()
	if err = store.AdvanceGenerationBoundary(ctx, "g2", proof.SourceWatermark, proof.SourceObservedAt); err != nil {
		t.Fatal(err)
	}
	if err = store.ActivateGeneration(ctx, proof); err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{d.PoolAddress, fixtureAddress(2)} {
		page, err := store.SearchIntegrated(ctx, IntegratedRequest{Request: Request{Query: address, Kind: "pool", Limit: 25}, Sort: "address"})
		if err != nil || page.Generation != "g2" || len(page.Items) != 1 {
			t.Fatal(page, err)
		}
		if address == d.PoolAddress && page.Items[0].Label != "During catchup" {
			t.Fatal("lost catch-up metadata", page)
		}
	}
	applyFixture(t, pool, Change{Version: 2, Kind: "visibility", Revision: 5, PoolAddress: d.PoolAddress, Tombstone: true})
	applyFixture(t, pool, Change{Version: 2, Kind: "profile_deletion", Revision: 5, ProfileWallet: d.Creator})
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	count, digest, err = GenerationDigest(ctx, tx, "g1")
	rollbackSearch(tx)
	if err != nil {
		t.Fatal(err)
	}
	var baselineObserved time.Time
	if err = pool.QueryRow(ctx, "SELECT source_observed_at FROM stats_search.generations_v2 WHERE generation='g1'").Scan(&baselineObserved); err != nil {
		t.Fatal(err)
	}
	if err = store.ActivateGeneration(ctx, GenerationProof{Generation: "g1", SourceWatermark: 1, SourceObservedAt: baselineObserved.UTC(), Rows: count, Digest: digest}); err == nil {
		t.Fatal("unvalidated baseline generation rollback accepted")
	}
	if err = pool.QueryRow(ctx, "SELECT generation FROM stats_search.active_v2 WHERE singleton").Scan(&active); err != nil || active != "g2" {
		t.Fatal(active, err)
	}
}

func TestGenerationLagAndCountFailurePreserveActivePointer(t *testing.T) {
	pool := integratedDatabase(t)
	ctx := context.Background()
	store := Store{Pool: pool}
	seedIndexed(t, pool, 1)
	observed := time.Now().UTC().Add(-6 * time.Second)
	if err := store.BeginGeneration(ctx, "g2", 1, observed); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	count, digest, err := GenerationDigest(ctx, tx, "g2")
	rollbackSearch(tx)
	if err != nil {
		t.Fatal(err)
	}
	proof := GenerationProof{Generation: "g2", SourceWatermark: 1, SourceObservedAt: observed, Rows: count, Digest: digest}
	if err = store.ActivateGeneration(ctx, proof); err == nil {
		t.Fatal("stale generation activated")
	}
	observed = time.Now().UTC()
	if err = store.AdvanceGenerationBoundary(ctx, "g2", 2, observed); err != nil {
		t.Fatal(err)
	}
	proof.SourceWatermark, proof.SourceObservedAt, proof.Rows = 2, observed, count+1
	if err = store.ActivateGeneration(ctx, proof); err == nil {
		t.Fatal("wrong row count activated")
	}
	if err = store.AdvanceGenerationBoundary(ctx, "g2", 1, observed); err == nil {
		t.Fatal("catch-up watermark regressed")
	}
	var active string
	if err = pool.QueryRow(ctx, "SELECT generation FROM stats_search.active_v2 WHERE singleton").Scan(&active); err != nil || active != "g1" {
		t.Fatal(active, err)
	}
	proof.Rows = count
	if err = store.ActivateGeneration(ctx, proof); err != nil {
		t.Fatal(err)
	}
}

func TestValidatedRollbackContinuesCurrentTombstones(t *testing.T) {
	pool := integratedDatabase(t)
	ctx := context.Background()
	store := Store{Pool: pool}
	d := seedIndexed(t, pool, 1)
	proofFor := func(generation string, watermark int64, observed time.Time) GenerationProof {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		count, digest, err := GenerationDigest(ctx, tx, generation)
		rollbackSearch(tx)
		if err != nil {
			t.Fatal(err)
		}
		return GenerationProof{Generation: generation, SourceWatermark: watermark, SourceObservedAt: observed, Rows: count, Digest: digest}
	}
	observed := time.Now().UTC()
	if err := store.BeginGeneration(ctx, "g2", 2, observed); err != nil {
		t.Fatal(err)
	}
	if err := store.ActivateGeneration(ctx, proofFor("g2", 2, observed)); err != nil {
		t.Fatal(err)
	}
	newObserved := time.Now().UTC()
	if err := store.BeginGeneration(ctx, "g3", 3, newObserved); err != nil {
		t.Fatal(err)
	}
	if err := store.ActivateGeneration(ctx, proofFor("g3", 3, newObserved)); err != nil {
		t.Fatal(err)
	}
	applyFixture(t, pool, Change{Version: 2, Kind: "visibility", Revision: 9, PoolAddress: d.PoolAddress, Tombstone: true})
	if err := store.ActivateGeneration(ctx, proofFor("g2", 2, observed)); err != nil {
		t.Fatal(err)
	}
	page, err := store.SearchIntegrated(ctx, IntegratedRequest{Request: Request{Query: d.PoolAddress, Kind: "pool", Limit: 25}, Sort: "address"})
	if err != nil || page.Generation != "g2" || len(page.Items) != 0 || page.Status != "not-indexed" {
		t.Fatal("rollback resurrected current tombstone", page, err)
	}
}
