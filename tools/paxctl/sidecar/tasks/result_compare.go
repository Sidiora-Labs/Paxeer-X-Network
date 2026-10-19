package tasks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"

	seis3 "github.com/sei-protocol/seictl/sidecar/s3"
	"github.com/sei-protocol/seictl/sidecar/shadow"
)

const (
	comparePollInterval = 5 * time.Second
	comparePageSize     = 100

	// finalFlushTimeout bounds the best-effort flush of the trailing compare
	// page when a survey is stopped. The loop's context is already cancelled at
	// that point, so the flush runs on a fresh deadline; the pod's termination
	// grace period must accommodate it.
	finalFlushTimeout = 10 * time.Second
)

// Guard the external contract Comparator.Close relies on: *ethclient.Client must
// expose a no-return Close(). If a go-ethereum upgrade changed it to Close()
// error, the no-return assertion in Comparator.Close would silently skip it
// (the leak we already fixed once) — this fails the build instead.
var _ interface{ Close() } = (*ethclient.Client)(nil)

// comparisonLoop holds the state for a running block comparison session.
type comparisonLoop struct {
	exporter     *ResultExporter
	comparator   *shadow.Comparator
	uploader     seis3.Uploader
	cfg          ResultExportRequest
	prefix       string
	height       int64
	pageBuf      []shadow.CompareResult
	pollInterval time.Duration
}

// ExportAndCompare runs a continuous comparison between the local shadow node
// and a canonical chain.
//
// By default it completes successfully on the first divergence, uploading a
// DivergenceReport alongside the comparison pages. In survey mode
// (cfg.ContinueOnDivergence) a divergence never halts the run: the comparison
// tails the chain until the context is cancelled, and a clean cancellation
// completes the task — being stopped is the survey's natural end, not a failure.
func (e *ResultExporter) ExportAndCompare(ctx context.Context, cfg ResultExportRequest) error {
	loop, err := e.newComparisonLoop(ctx, cfg)
	if err != nil {
		return err
	}
	defer loop.comparator.Close()

	exportLog.Info("starting block comparison",
		"start-height", loop.height,
		"canonical-rpc", cfg.CanonicalRPC,
		"bucket", cfg.Bucket)

	return loop.run(ctx)
}

func (e *ResultExporter) newComparisonLoop(ctx context.Context, cfg ResultExportRequest) (*comparisonLoop, error) {
	uploader, err := e.s3UploaderFactory(ctx, cfg.Region)
	if err != nil {
		return nil, fmt.Errorf("building S3 uploader: %w", err)
	}

	var compOpts []shadow.Option
	if cfg.MigrationMode {
		compOpts = append(compOpts, shadow.WithMigrationMode())
	}

	// Layer 2 (logical state diff) is enabled when both EVM JSON-RPC endpoints
	// are configured. Touched keys come from a prestate trace on TraceRPC
	// (defaults to the canonical endpoint).
	if cfg.ShadowEVMRPC != "" && cfg.CanonicalEVMRPC != "" {
		shadowState, err := ethclient.Dial(cfg.ShadowEVMRPC)
		if err != nil {
			return nil, fmt.Errorf("dialing shadow EVM RPC: %w", err)
		}
		canonicalState, err := ethclient.Dial(cfg.CanonicalEVMRPC)
		if err != nil {
			shadowState.Close()
			return nil, fmt.Errorf("dialing canonical EVM RPC: %w", err)
		}
		traceRPC := cfg.TraceRPC
		if traceRPC == "" {
			traceRPC = cfg.CanonicalEVMRPC
		}
		keySource, err := shadow.NewTraceKeySource(traceRPC)
		if err != nil {
			shadowState.Close()
			canonicalState.Close()
			return nil, fmt.Errorf("building trace key source: %w", err)
		}
		compOpts = append(compOpts, shadow.WithLayer2(shadowState, canonicalState, keySource))
	}

	last := e.readExportState()
	return &comparisonLoop{
		exporter:     e,
		comparator:   shadow.NewComparator(cfg.RPCEndpoint, cfg.CanonicalRPC, compOpts...),
		uploader:     uploader,
		cfg:          cfg,
		prefix:       normalizePrefix(cfg.Prefix),
		height:       last.LastExportedHeight + 1,
		pollInterval: comparePollInterval,
	}, nil
}

func (l *comparisonLoop) run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return l.finalize(err)
		}

		latestHeight, err := l.waitForBlocks(ctx)
		if err != nil {
			return l.finalize(err)
		}

		diverged, err := l.compareBlocksUpTo(ctx, latestHeight)
		if err != nil {
			return l.finalize(err)
		}
		if diverged {
			return nil
		}
	}
}

// finalize maps the loop's exit error to a task verdict. Survey mode has no
// divergence-halt, so being stopped is its natural end: a clean context
// cancellation (e.g. sidecar shutdown) completes the task rather than failing
// it. Any other error — and any error in the default halt-on-divergence mode —
// propagates and fails the run.
//
// Before completing, the trailing partial page (blocks compared since the last
// full-page boundary) is flushed — otherwise those blocks would be silently
// absent from S3 while the task reports success, and a completed task is not
// re-run. If that final flush fails the run fails instead, so a restart
// re-surveys the trailing blocks from the last persisted height.
func (l *comparisonLoop) finalize(err error) error {
	if l.cfg.ContinueOnDivergence && errors.Is(err, context.Canceled) {
		flushCtx, cancel := context.WithTimeout(context.Background(), finalFlushTimeout)
		defer cancel()
		if ferr := l.flushFinalPage(flushCtx); ferr != nil {
			return fmt.Errorf("flushing final survey page on shutdown: %w", ferr)
		}
		exportLog.Info("survey stopped; completing", "last-height", l.height)
		return nil
	}
	return err
}

// flushFinalPage uploads whatever remains in the page buffer (a partial page
// below comparePageSize) and persists the height of its last block, so a
// stopped survey loses no compared blocks. It keys the persisted height off the
// buffer's last entry rather than l.height, which has already advanced past it.
func (l *comparisonLoop) flushFinalPage(ctx context.Context) error {
	if len(l.pageBuf) == 0 {
		return nil
	}
	lastHeight := l.pageBuf[len(l.pageBuf)-1].Height
	if err := flushComparePage(ctx, l.uploader, l.cfg.Bucket, l.prefix, l.pageBuf); err != nil {
		return err
	}
	l.exporter.persistHeight(lastHeight)
	l.pageBuf = l.pageBuf[:0]
	return nil
}

func (l *comparisonLoop) waitForBlocks(ctx context.Context) (int64, error) {
	for {
		latestHeight, err := queryLatestHeight(ctx, l.cfg.RPCEndpoint)
		if err != nil {
			exportLog.Debug("shadow RPC unavailable, will retry", "err", err)
			if err := sleep(ctx, l.pollInterval); err != nil {
				return 0, err
			}
			continue
		}

		if l.height <= latestHeight {
			return latestHeight, nil
		}

		if err := sleep(ctx, l.pollInterval); err != nil {
			return 0, err
		}
	}
}

func (l *comparisonLoop) compareBlocksUpTo(ctx context.Context, latestHeight int64) (diverged bool, _ error) {
	for l.height <= latestHeight {
		if err := ctx.Err(); err != nil {
			return false, err
		}

		result, err := l.comparator.CompareBlock(ctx, l.height)
		if err != nil {
			exportLog.Warn("comparison failed, will retry", "height", l.height, "err", err)
			return false, sleep(ctx, l.pollInterval)
		}

		shadow.BlocksCompared.WithLabelValues(l.exporter.chainID, l.exporter.podName).Inc()
		l.pageBuf = append(l.pageBuf, *result)

		if result.Diverged() {
			if !l.cfg.ContinueOnDivergence {
				return true, l.handleDivergence(ctx, *result)
			}
			// Survey mode: the divergent block is already appended to the compare
			// page (its authentic per-block verdict); record it and fall through to
			// the normal page-flush + height++ discipline. Unlike handleDivergence we
			// upload NO per-block DivergenceReport — that re-fetches block +
			// block_results from both chains and writes an S3 object per block, which
			// would overload the sidecar over a multi-million-block sweep. The page
			// (flushed and truncated at comparePageSize, so memory stays bounded) is
			// the survey record; seictl report classifies benign vs real downstream.
			l.recordDivergence(*result)
		}

		if err := l.flushPageIfFull(ctx); err != nil {
			return false, err
		}

		l.height++
	}
	return false, nil
}

func (l *comparisonLoop) handleDivergence(ctx context.Context, result shadow.CompareResult) error {
	layer := l.incDivergenceMetric(result)

	exportLog.Info("app-hash divergence detected",
		"height", l.height,
		"divergence-layer", layer,
		"shadow-app-hash", result.Layer0.ShadowAppHash,
		"canonical-app-hash", result.Layer0.CanonicalAppHash)

	if err := l.uploadDivergenceReport(ctx, result); err != nil {
		exportLog.Warn("failed to upload divergence report, continuing", "err", err)
	}

	if err := flushComparePage(ctx, l.uploader, l.cfg.Bucket, l.prefix, l.pageBuf); err != nil {
		return fmt.Errorf("flushing final comparison page: %w", err)
	}

	l.exporter.persistHeight(l.height)
	return nil
}

// recordDivergence notes a divergent block under ContinueOnDivergence (survey
// mode): it increments the divergence metric and logs, but — unlike
// handleDivergence — uploads no per-block DivergenceReport and forces no early
// page flush. The block is already in the page; it rides the normal
// flushPageIfFull boundary, so the in-memory buffer stays bounded over a long
// sweep and the run continues instead of halting.
func (l *comparisonLoop) recordDivergence(result shadow.CompareResult) {
	layer := l.incDivergenceMetric(result)
	exportLog.Info("divergence recorded; continuing (survey mode)",
		"height", l.height, "divergence-layer", layer)
}

// incDivergenceMetric increments the divergence counter under the result's
// layer label and returns that label. The label encoding (nil DivergenceLayer
// → "0", else the layer number) is one contract shared by both the
// halt-on-divergence and survey-mode paths, so it lives in a single place.
func (l *comparisonLoop) incDivergenceMetric(result shadow.CompareResult) string {
	layer := "0"
	if result.DivergenceLayer != nil {
		layer = fmt.Sprintf("%d", *result.DivergenceLayer)
	}
	shadow.Divergences.WithLabelValues(l.exporter.chainID, l.exporter.podName, layer).Inc()
	return layer
}

func (l *comparisonLoop) uploadDivergenceReport(ctx context.Context, result shadow.CompareResult) error {
	report, err := l.comparator.BuildDivergenceReport(ctx, l.height, result)
	if err != nil {
		return fmt.Errorf("building divergence report: %w", err)
	}

	key := fmt.Sprintf("%sdivergence-%d.report.json.gz", l.prefix, l.height)
	_, err = seis3.StreamGzipJSON(ctx, l.uploader, l.cfg.Bucket, key, report)
	return err
}

func (l *comparisonLoop) flushPageIfFull(ctx context.Context) error {
	if len(l.pageBuf) < comparePageSize {
		return nil
	}

	// A flush failure ends the run rather than continuing: the uploader (AWS SDK)
	// already retries transient faults, so an error here is a real failure, and
	// swallowing it would leave pageBuf un-truncated and growing every iteration
	// — unbounded under a persistent S3 fault on a never-halting survey. Failing
	// lets the task restart and resume from the last persisted (flushed) height.
	if err := flushComparePage(ctx, l.uploader, l.cfg.Bucket, l.prefix, l.pageBuf); err != nil {
		return fmt.Errorf("flushing comparison page: %w", err)
	}

	l.exporter.persistHeight(l.height)
	l.pageBuf = l.pageBuf[:0]
	return nil
}

// persistHeight saves the last exported height to disk, logging on failure.
func (e *ResultExporter) persistHeight(height int64) {
	if err := e.writeExportState(exportState{LastExportedHeight: height}); err != nil {
		exportLog.Warn("failed to persist export state", "err", err)
	}
}

// --- S3 upload helpers ---

func flushComparePage(ctx context.Context, uploader seis3.Uploader, bucket, prefix string, results []shadow.CompareResult) error {
	if len(results) == 0 {
		return nil
	}

	start := results[0].Height
	end := results[len(results)-1].Height
	key := fmt.Sprintf("%s%d-%d.compare.ndjson.gz", prefix, start, end)

	exportLog.Info("flushing comparison page", "key", key, "blocks", len(results))
	_, err := seis3.StreamGzipNDJSON(ctx, uploader, bucket, key, results)
	return err
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
