package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	seiconfig "github.com/sei-protocol/sei-config"
	"github.com/sei-protocol/seictl/sidecar/engine"
	"github.com/sei-protocol/seictl/sidecar/rpc"
	seis3 "github.com/sei-protocol/seictl/sidecar/s3"
	"github.com/sei-protocol/seilog"
)

var exportLog = seilog.NewLogger("seictl", "task", "result-export")

const (
	exportStateFile = ".sei-sidecar-last-export.json"
	defaultPageSize = 1000
)

var defaultRPCEndpoint = fmt.Sprintf("http://localhost:%d", seiconfig.PortRPC)

// errSinkWrite tags a failure writing to the export sink (the gzip/upload pipe),
// distinguishing a downstream/S3 cause from a genuine producer fault (RPC,
// marshaling, ctx). When the sink fails mid-stream the real cause is the upload
// error, so the write error must not be returned as if the producer failed.
var errSinkWrite = errors.New("writing to export sink")

// ResultExportRequest holds the parameters for the result-export task.
type ResultExportRequest struct {
	Bucket      string `json:"bucket"`
	Prefix      string `json:"prefix"`
	Region      string `json:"region"`
	RPCEndpoint string `json:"rpcEndpoint"`

	// CanonicalRPC enables comparison mode. When set, the exporter compares
	// local block execution against this canonical RPC endpoint and completes
	// when app-hash divergence is detected.
	CanonicalRPC string `json:"canonicalRpc"`

	// MigrationMode tunes comparison for an AppHash-breaking migration shadow
	// (e.g. memiavl->flatkv): AppHash divergence from canonical is expected
	// every block, so it is treated as informational and the verdict keys on
	// execution-results equivalence (LastResultsHash + gas + per-tx receipts).
	MigrationMode bool `json:"migrationMode,omitempty"`

	// ContinueOnDivergence selects survey mode: the comparison records each
	// divergent block to the compare page and keeps going instead of halting on
	// the first divergence. Default false preserves the production tripwire. The
	// comparator's verdict is unchanged — every field is compared authentically;
	// this only decides whether a divergence stops the run. Classifying benign vs
	// real divergences is the downstream `seictl report` step's job. Has no
	// effect outside comparison mode (it requires CanonicalRPC).
	ContinueOnDivergence bool `json:"continueOnDivergence,omitempty"`

	// ShadowEVMRPC and CanonicalEVMRPC are the EVM JSON-RPC endpoints for the
	// shadow and canonical chains. When both are set, Layer 2 (logical state
	// diff) is enabled, comparing storage/code/nonce for the keys each block
	// touched. These are EVM JSON-RPC (eth_*), distinct from the CometBFT RPC
	// used for Layers 0/1.
	ShadowEVMRPC    string `json:"shadowEvmRpc,omitempty"`
	CanonicalEVMRPC string `json:"canonicalEvmRpc,omitempty"`

	// TraceRPC is the EVM JSON-RPC endpoint used for prestate traces
	// (debug_traceBlockByNumber) to derive each block's touched keys. Defaults
	// to CanonicalEVMRPC. Requires the debug_ namespace enabled on that node.
	TraceRPC string `json:"traceRpc,omitempty"`
}

type exportState struct {
	LastExportedHeight int64 `json:"lastExportedHeight"`
}

// ResultExporter queries the local seid RPC for block results and uploads
// them in compressed NDJSON pages to S3.
type ResultExporter struct {
	homeDir           string
	chainID           string
	podName           string
	s3UploaderFactory seis3.UploaderFactory
}

// NewResultExporter creates an exporter targeting the given home directory.
// chainID and podName label shadow comparison metrics; pass empty strings
// if the exporter is only used in non-comparison mode.
func NewResultExporter(homeDir, chainID, podName string, factory seis3.UploaderFactory) *ResultExporter {
	if factory == nil {
		factory = seis3.DefaultUploaderFactory
	}
	return &ResultExporter{homeDir: homeDir, chainID: chainID, podName: podName, s3UploaderFactory: factory}
}

func (e *ResultExporter) Handler() engine.TaskHandler {
	return engine.TypedHandler(func(ctx context.Context, cfg ResultExportRequest) error {
		if cfg.Bucket == "" {
			return fmt.Errorf("result-export: missing required param 'bucket'")
		}
		if cfg.Region == "" {
			return fmt.Errorf("result-export: missing required param 'region'")
		}
		if cfg.RPCEndpoint == "" {
			cfg.RPCEndpoint = defaultRPCEndpoint
		}
		if cfg.CanonicalRPC != "" {
			return e.ExportAndCompare(ctx, cfg)
		}
		return e.Export(ctx, cfg)
	})
}

// Export queries the local node for block results and uploads pages to S3.
// Each invocation exports as many complete pages as are available since the
// last export height. The state file tracks progress across invocations.
func (e *ResultExporter) Export(ctx context.Context, cfg ResultExportRequest) error {
	last := e.readExportState()
	startHeight := last.LastExportedHeight + 1

	latestHeight, err := queryLatestHeight(ctx, cfg.RPCEndpoint)
	if err != nil {
		exportLog.Info("RPC unavailable, will retry", "err", err)
		return nil
	}

	if startHeight > latestHeight {
		exportLog.Debug("no new blocks to export",
			"last-exported", last.LastExportedHeight,
			"latest", latestHeight)
		return nil
	}

	uploader, err := e.s3UploaderFactory(ctx, cfg.Region)
	if err != nil {
		return fmt.Errorf("building S3 uploader: %w", err)
	}

	rpcClient := rpc.NewClient(cfg.RPCEndpoint, nil)
	prefix := normalizePrefix(cfg.Prefix)

	availableBlocks := latestHeight - startHeight + 1
	fullPages := int(availableBlocks) / defaultPageSize

	if fullPages == 0 {
		exportLog.Debug("not enough blocks for a full page yet",
			"available", availableBlocks,
			"page-size", defaultPageSize)
		return nil
	}

	for page := 0; page < fullPages; page++ {
		pageStart := startHeight + int64(page*defaultPageSize)
		pageEnd := pageStart + int64(defaultPageSize) - 1

		exportLog.Info("exporting result page",
			"start", pageStart,
			"end", pageEnd,
			"bucket", cfg.Bucket)

		if err := e.exportPage(ctx, rpcClient, uploader, cfg.Bucket, cfg.Region, prefix, pageStart, pageEnd); err != nil {
			return fmt.Errorf("exporting page %d-%d: %w", pageStart, pageEnd, err)
		}

		if err := e.writeExportState(exportState{LastExportedHeight: pageEnd}); err != nil {
			exportLog.Warn("failed to persist export state, deferring to next scheduled run",
				"last-exported", pageEnd, "err", err)
			return nil
		}
	}

	lastExported := startHeight + int64(fullPages*defaultPageSize) - 1
	exportLog.Info("export complete",
		"pages", fullPages,
		"last-exported", lastExported,
		"latest-available", latestHeight)

	return nil
}

// exportPage collects block results for [start, end] and streams a gzipped
// NDJSON file to S3.
func (e *ResultExporter) exportPage(
	ctx context.Context,
	client *rpc.Client,
	uploader seis3.Uploader,
	bucket, region, prefix string,
	start, end int64,
) error {
	key := fmt.Sprintf("%s%d-%d.ndjson.gz", prefix, start, end)

	var collectErr error
	_, uploadErr := seis3.StreamGzipFunc(ctx, uploader, bucket, key, func(w io.Writer) error {
		collectErr = e.collectResults(ctx, client, w, start, end)
		return collectErr
	})
	switch {
	case collectErr != nil && !errors.Is(collectErr, errSinkWrite):
		// Genuine producer fault (block_results RPC, marshaling, ctx cancel) —
		// not an S3 problem; return it as-is, never mislabeled with S3 hints.
		return collectErr
	case uploadErr != nil:
		// Upload failed, including mid-stream (which also shows up as a tagged
		// errSinkWrite in collectErr). Keep the S3 classification + Retryable hint.
		return seis3.ClassifyS3Error("result-export", bucket, key, region, uploadErr)
	case collectErr != nil:
		// A sink write failed without the upload reporting an error — surface it.
		return collectErr
	}
	return nil
}

// collectResults queries block_results for each height and writes one NDJSON
// line per block to w. Each line is a JSON object with height, time, and the
// raw block_results response. gzip/pipe/checksum are owned by the s3 helper.
func (e *ResultExporter) collectResults(ctx context.Context, client *rpc.Client, w io.Writer, start, end int64) error {
	for h := start; h <= end; h++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		result, err := queryBlockResults(ctx, client, h)
		if err != nil {
			return fmt.Errorf("querying block_results at height %d: %w", h, err)
		}

		record := map[string]any{
			"height":        h,
			"exported_at":   time.Now().UTC().Format(time.RFC3339),
			"block_results": result,
		}

		line, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("marshaling result at height %d: %w", h, err)
		}
		line = append(line, '\n')

		if _, err := w.Write(line); err != nil {
			return fmt.Errorf("%w at height %d: %w", errSinkWrite, h, err)
		}
	}

	return nil
}

func queryLatestHeight(ctx context.Context, rpcEndpoint string) (int64, error) {
	c := rpc.NewStatusClient(rpcEndpoint, nil)
	h, err := c.LatestHeight(ctx)
	if err != nil {
		return 0, err
	}
	if h <= 0 {
		return 0, fmt.Errorf("latest_block_height is %d, node may still be syncing", h)
	}
	return h, nil
}

func queryBlockResults(ctx context.Context, client *rpc.Client, height int64) (json.RawMessage, error) {
	body, err := client.GetRaw(ctx, fmt.Sprintf("/block_results?height=%d", height))
	if err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

func (e *ResultExporter) readExportState() exportState {
	data, err := os.ReadFile(filepath.Join(e.homeDir, exportStateFile))
	if err != nil {
		return e.bootstrapExportState()
	}
	var state exportState
	if err := json.Unmarshal(data, &state); err != nil {
		return e.bootstrapExportState()
	}
	return state
}

// bootstrapExportState reads the snapshot height file written by the restorer
// and uses it as the initial last-exported height, so the exporter begins at
// the first block after the restored snapshot rather than from block 1.
func (e *ResultExporter) bootstrapExportState() exportState {
	data, err := os.ReadFile(filepath.Join(e.homeDir, SnapshotHeightFile))
	if err != nil {
		return exportState{}
	}
	h, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || h <= 0 {
		return exportState{}
	}
	exportLog.Info("bootstrapping export state from snapshot height", "height", h)
	return exportState{LastExportedHeight: h}
}

func (e *ResultExporter) writeExportState(state exportState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshaling export state: %w", err)
	}
	path := filepath.Join(e.homeDir, exportStateFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing export state: %w", err)
	}
	return nil
}
