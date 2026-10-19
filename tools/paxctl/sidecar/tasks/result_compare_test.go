package tasks

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	seis3 "github.com/sei-protocol/seictl/sidecar/s3"
	"github.com/sei-protocol/seictl/sidecar/shadow"
)

// newTestComparisonLoop wires a comparison loop against two RPC servers whose
// blocks diverge at Layer 0 (different app_hash, matching results, no per-tx
// detail) so every compared block is a divergence, with a recording uploader.
// continueOnDivergence selects survey mode vs the default halt-on-first; latest
// is the height the servers report via /status (drives waitForBlocks in run()).
func newTestComparisonLoop(t *testing.T, continueOnDivergence bool, latest int64) (*comparisonLoop, *recordingUploader) {
	t.Helper()
	shadowSrv := fakeRPCAndBlockServer(latest, "SHADOW", "RES", nil)
	canonicalSrv := fakeRPCAndBlockServer(latest, "CANONICAL", "RES", nil)
	t.Cleanup(shadowSrv.Close)
	t.Cleanup(canonicalSrv.Close)

	rec := &recordingUploader{}
	exporter := NewResultExporter(t.TempDir(), "test-chain", "pod-0", func(_ context.Context, _ string) (seis3.Uploader, error) {
		return rec, nil
	})
	return &comparisonLoop{
		exporter:   exporter,
		comparator: shadow.NewComparator(shadowSrv.URL, canonicalSrv.URL),
		uploader:   rec,
		cfg: ResultExportRequest{
			Bucket: "bkt", Region: "us-east-1", Prefix: "p/",
			RPCEndpoint:          shadowSrv.URL,
			CanonicalRPC:         canonicalSrv.URL,
			ContinueOnDivergence: continueOnDivergence,
		},
		prefix:       "p/",
		height:       1,
		pollInterval: time.Millisecond,
	}, rec
}

func countKeys(rec *recordingUploader, substr string) int {
	n := 0
	for _, k := range rec.keys {
		if strings.Contains(k, substr) {
			n++
		}
	}
	return n
}

// TestCompareDefaultMode_HaltsOnDivergence: with ContinueOnDivergence unset the
// first divergent block trips the loop — it reports diverged, uploads a
// per-block divergence report, and does not advance past the divergent height.
func TestCompareDefaultMode_HaltsOnDivergence(t *testing.T) {
	loop, rec := newTestComparisonLoop(t, false, 5)

	diverged, err := loop.compareBlocksUpTo(context.Background(), 5)
	if err != nil {
		t.Fatalf("compareBlocksUpTo: %v", err)
	}
	if !diverged {
		t.Fatal("default mode must halt on the first divergence")
	}
	if loop.height != 1 {
		t.Errorf("height advanced to %d; default-mode halt must not step past the divergent block", loop.height)
	}
	if n := countKeys(rec, "divergence-1.report"); n != 1 {
		t.Errorf("expected exactly one per-block divergence report, got %d", n)
	}
}

// TestCompareSurveyMode_ContinuesPastDivergence: with ContinueOnDivergence set
// the loop surveys every divergent block to the end of the range, advances the
// height, and uploads NO per-block divergence report (the page is the record).
func TestCompareSurveyMode_ContinuesPastDivergence(t *testing.T) {
	loop, rec := newTestComparisonLoop(t, true, 5)

	diverged, err := loop.compareBlocksUpTo(context.Background(), 5)
	if err != nil {
		t.Fatalf("compareBlocksUpTo: %v", err)
	}
	if diverged {
		t.Fatal("survey mode must not halt on divergence")
	}
	if loop.height != 6 {
		t.Errorf("height = %d, want 6 (surveyed every block past the divergences)", loop.height)
	}
	if n := countKeys(rec, ".report"); n != 0 {
		t.Errorf("survey mode must upload no per-block divergence report, got %d", n)
	}
}

// TestCompareSurveyMode_BoundsMemory: surveying past comparePageSize divergent
// blocks must flush-and-truncate the in-memory page rather than accumulate
// every result — the bound that keeps a multi-million-block sweep from
// exhausting the sidecar. Two full pages flush; the buffer holds only the
// trailing remainder.
func TestCompareSurveyMode_BoundsMemory(t *testing.T) {
	loop, rec := newTestComparisonLoop(t, true, 2*comparePageSize+50)

	const blocks = 2*comparePageSize + 50
	diverged, err := loop.compareBlocksUpTo(context.Background(), blocks)
	if err != nil {
		t.Fatalf("compareBlocksUpTo: %v", err)
	}
	if diverged {
		t.Fatal("survey mode must not halt on divergence")
	}
	if len(loop.pageBuf) >= comparePageSize {
		t.Errorf("pageBuf holds %d results; survey mode must flush+truncate at comparePageSize (%d) to bound memory", len(loop.pageBuf), comparePageSize)
	}
	if len(loop.pageBuf) != 50 {
		t.Errorf("pageBuf = %d, want 50 (the remainder after two full-page flushes)", len(loop.pageBuf))
	}
	if n := countKeys(rec, ".compare.ndjson.gz"); n != 2 {
		t.Errorf("expected 2 flushed compare pages, got %d", n)
	}
	if n := countKeys(rec, ".report"); n != 0 {
		t.Errorf("survey mode must upload no per-block divergence report, got %d", n)
	}
}

// TestCompareSurveyMode_RunCompletesOnCancel: survey mode never returns
// diverged=true, so run() only exits when stopped. A clean context cancellation
// is the survey's natural end and must complete the task (return nil), not fail it.
func TestCompareSurveyMode_RunCompletesOnCancel(t *testing.T) {
	loop, _ := newTestComparisonLoop(t, true, 3)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- loop.run(ctx) }()

	// Give the survey a moment to process the few available blocks and begin
	// tailing, then stop it the way a sidecar shutdown would. (Reading loop
	// state here would race run()'s goroutine; a cancel completes cleanly
	// whether the loop is tailing or mid-survey, so a brief wait is enough —
	// the assertion is on the exit verdict, not on catch-up.)
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("survey run on clean cancel = %v, want nil (a stop completes, not fails)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after context cancellation")
	}
}

// TestCompareSurveyMode_FlushesTrailingPageOnStop: when a survey is stopped
// with a partial page buffered (fewer than comparePageSize blocks), that page
// must be flushed to S3 before the task completes — otherwise up to
// comparePageSize-1 compared blocks are silently dropped while the task reports
// success. latest < comparePageSize, so the only page is the trailing partial.
func TestCompareSurveyMode_FlushesTrailingPageOnStop(t *testing.T) {
	loop, rec := newTestComparisonLoop(t, true, 50)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- loop.run(ctx) }()

	// Survey the available blocks and begin tailing, then stop the survey.
	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("survey run on clean stop = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after context cancellation")
	}

	// Safe to read rec.keys: the run goroutine's writes happen-before the done
	// receive, and it has returned.
	if n := countKeys(rec, ".compare.ndjson.gz"); n != 1 {
		t.Errorf("trailing compare page not flushed on stop: got %d compare pages, want 1", n)
	}
	if n := countKeys(rec, ".report"); n != 0 {
		t.Errorf("survey mode must upload no per-block divergence report, got %d", n)
	}
}

// TestCompareSurveyMode_FlushFailureFailsBounded: a persistent flush failure on
// a never-halting survey must fail the run (so the task restarts and resumes)
// rather than silently swallow the error and let pageBuf grow unbounded.
func TestCompareSurveyMode_FlushFailureFailsBounded(t *testing.T) {
	loop, _ := newTestComparisonLoop(t, true, 5*comparePageSize)
	loop.uploader = drainingFailingUploader{err: errors.New("s3 unavailable")}

	diverged, err := loop.compareBlocksUpTo(context.Background(), 5*comparePageSize)
	if err == nil {
		t.Fatal("a persistent flush failure must fail the run, not be swallowed")
	}
	if diverged {
		t.Fatal("a flush failure is an error, not a divergence-halt")
	}
	if len(loop.pageBuf) > comparePageSize {
		t.Errorf("pageBuf grew to %d past comparePageSize (%d) on flush failure — the buffer must stay bounded", len(loop.pageBuf), comparePageSize)
	}
}
