package tasks

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/sei-protocol/seictl/sidecar/engine"
	seis3 "github.com/sei-protocol/seictl/sidecar/s3"
	"github.com/sei-protocol/seilog"
)

var evmDigestLog = seilog.NewLogger("seictl", "task", "evm-logical-digest")

const defaultSeidbPath = "seidb"

// defaultNormalizations are the memiavl normalization modes proved per run.
// "semantic" decodes raw memiavl EVM leaves independently; "translator" runs
// them through flatkv.ImportTranslator. Proving both guards against a bug in
// either decoder masking a real divergence.
var defaultNormalizations = []string{"semantic", "translator"}

// EvmLogicalDigestRequest parameters the evm-logical-digest task. It shells out
// to seidb's evm-logical-digest for a flatkv clone and a memiavl snapshot at the
// same height, compares the backend-independent logical digests, and publishes
// the verdict to S3.
type EvmLogicalDigestRequest struct {
	FlatKVDir  string `json:"flatkvDir"`
	MemIAVLDir string `json:"memiavlDir"`
	Height     int64  `json:"height"`
	Bucket     string `json:"bucket"`
	Prefix     string `json:"prefix"`
	Region     string `json:"region"`

	// Normalizations is the set of memiavl normalization modes to prove.
	// Defaults to ["semantic","translator"] when empty.
	Normalizations []string `json:"normalizations"`

	// SeidbPath is the seidb binary to exec. Defaults to "seidb" (PATH lookup).
	SeidbPath string `json:"seidbPath"`
}

// bucketDigests holds the per-bucket and final logical digests parsed from one
// seidb run. All values are lowercase hex as printed by seidb.
type bucketDigests struct {
	Account string `json:"account"`
	Code    string `json:"code"`
	Storage string `json:"storage"`
	Legacy  string `json:"legacy"`
	Final   string `json:"final"`
	Version int64  `json:"version"`
}

// EndpointDigestRecord is the published verdict for one (height, normalization)
// comparison — the durable artifact a reader trusts. Its own SHA-256 seal lives
// out-of-band (the s3 helper's EmitResult, logged + in the TaskResult): a record
// cannot carry the hash of its own published bytes.
type EndpointDigestRecord struct {
	Height        int64             `json:"height"`
	Normalization string            `json:"normalization"`
	FlatKVDigest  string            `json:"flatkv_digest"`
	MemIAVLDigest string            `json:"memiavl_digest"`
	PerBucket     map[string]bucket `json:"per_bucket"`
	Match         bool              `json:"match"`
	AxesProved    []string          `json:"axes_proved"`
	GeneratedAt   string            `json:"generated_at"`
}

// bucket pairs the flatkv and memiavl digest for one canonical bucket.
type bucket struct {
	FlatKV  string `json:"flatkv"`
	MemIAVL string `json:"memiavl"`
	Match   bool   `json:"match"`
}

// EvmLogicalDigester runs the comparison. It holds no per-request state.
type EvmLogicalDigester struct {
	s3UploaderFactory seis3.UploaderFactory
}

// NewEvmLogicalDigester builds the task handler dependency. A nil factory uses
// the default real-S3 uploader.
func NewEvmLogicalDigester(factory seis3.UploaderFactory) *EvmLogicalDigester {
	if factory == nil {
		factory = seis3.DefaultUploaderFactory
	}
	return &EvmLogicalDigester{s3UploaderFactory: factory}
}

func (d *EvmLogicalDigester) Handler() engine.TaskHandler {
	return engine.TypedHandler(func(ctx context.Context, req EvmLogicalDigestRequest) error {
		return d.run(ctx, req)
	})
}

func (d *EvmLogicalDigester) run(ctx context.Context, req EvmLogicalDigestRequest) error {
	if err := validateDigestRequest(&req); err != nil {
		return err
	}

	uploader, err := d.s3UploaderFactory(ctx, req.Region)
	if err != nil {
		return fmt.Errorf("evm-logical-digest: building S3 uploader: %w", err)
	}
	prefix := normalizePrefix(req.Prefix)

	// FlatKV is normalization-independent (native physical keyspace), so it is
	// digested once and compared against each memiavl normalization.
	flatkv, err := d.runSeidb(ctx, req.SeidbPath, "flatkv", req.FlatKVDir, req.Height, "")
	if err != nil {
		return fmt.Errorf("evm-logical-digest: flatkv digest: %w", err)
	}
	// Trust the run, not the request: a digest taken at the wrong height is a
	// silent false match. seidb WAL-replays flatkv to --height, so the opened
	// version MUST equal the requested height.
	if flatkv.Version != req.Height {
		return fmt.Errorf("evm-logical-digest: flatkv opened version %d != requested height %d", flatkv.Version, req.Height)
	}

	for _, norm := range req.Normalizations {
		memiavl, err := d.runSeidb(ctx, req.SeidbPath, "memiavl", req.MemIAVLDir, req.Height, norm)
		if err != nil {
			return fmt.Errorf("evm-logical-digest: memiavl digest (%s): %w", norm, err)
		}
		// Symmetric with the flatkv check: if seidb clamps to the nearest
		// available snapshot instead of erroring, the comparison would run at
		// the wrong height — a silent false match in the degenerate case.
		if memiavl.Version != req.Height {
			return fmt.Errorf("evm-logical-digest: memiavl opened version %d != requested height %d (%s)", memiavl.Version, req.Height, norm)
		}

		record := buildEndpointDigest(req.Height, norm, flatkv, memiavl)
		key := fmt.Sprintf("%sendpoint-digest-%d-%s.json.gz", prefix, req.Height, norm)

		emit, err := seis3.StreamGzipJSON(ctx, uploader, req.Bucket, key, record)
		if err != nil {
			return seis3.ClassifyS3Error("evm-logical-digest", req.Bucket, key, req.Region, err)
		}

		evmDigestLog.Info("published endpoint digest",
			"height", req.Height,
			"normalization", norm,
			"match", record.Match,
			"flatkv-digest", record.FlatKVDigest,
			"memiavl-digest", record.MemIAVLDigest,
			"key", key,
			"sha256", emit.UncompressedSHA256)
	}

	return nil
}

func validateDigestRequest(req *EvmLogicalDigestRequest) error {
	if req.Bucket == "" {
		return fmt.Errorf("evm-logical-digest: missing required param 'bucket'")
	}
	if req.Region == "" {
		return fmt.Errorf("evm-logical-digest: missing required param 'region'")
	}
	if req.Height <= 0 {
		return fmt.Errorf("evm-logical-digest: 'height' must be a positive block height, got %d", req.Height)
	}
	if req.FlatKVDir == "" {
		return fmt.Errorf("evm-logical-digest: missing required param 'flatkvDir'")
	}
	if req.MemIAVLDir == "" {
		return fmt.Errorf("evm-logical-digest: missing required param 'memiavlDir'")
	}
	if req.SeidbPath == "" {
		req.SeidbPath = defaultSeidbPath
	}
	if len(req.Normalizations) == 0 {
		req.Normalizations = defaultNormalizations
	}
	for _, n := range req.Normalizations {
		if n != "semantic" && n != "translator" {
			return fmt.Errorf("evm-logical-digest: unknown normalization %q (want semantic|translator)", n)
		}
	}
	return nil
}

// runSeidb execs `seidb evm-logical-digest` for one backend and parses the
// per-bucket + FINAL_DIGEST lines and the opened version from its stdout.
func (d *EvmLogicalDigester) runSeidb(ctx context.Context, seidbPath, backend, dir string, height int64, normalization string) (bucketDigests, error) {
	args := []string{
		"evm-logical-digest",
		"--backend", backend,
		"-d", dir,
		"--height", strconv.FormatInt(height, 10),
	}
	if backend == "memiavl" && normalization != "" {
		args = append(args, "--memiavl-normalization", normalization)
	}

	cmd := exec.CommandContext(ctx, seidbPath, args...)
	// Stderr is left nil so Output() captures it into ExitError.Stderr, which
	// stderrTail surfaces on failure.
	out, err := cmd.Output()
	if err != nil {
		return bucketDigests{}, fmt.Errorf("running %s %s: %w%s", seidbPath, strings.Join(args, " "), err, stderrTail(err))
	}
	return parseDigestOutput(string(out))
}

// stderrTail surfaces the captured stderr from an *exec.ExitError so a seidb
// failure is actionable instead of a bare "exit status 1".
func stderrTail(err error) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return fmt.Sprintf(" (stderr: %s)", strings.TrimSpace(string(exitErr.Stderr)))
	}
	return ""
}

// parseDigestOutput extracts the per-bucket digests, the final digest, and the
// opened version from one seidb run's stdout. It is fail-closed: a missing
// FINAL_DIGEST, a missing bucket, or a missing version is an error, never a
// zero-valued "match".
func parseDigestOutput(out string) (bucketDigests, error) {
	var bd bucketDigests
	var (
		haveVersion bool
		haveAccount bool
		haveCode    bool
		haveStorage bool
		haveLegacy  bool
		haveFinal   bool
	)

	scanner := bufio.NewScanner(strings.NewReader(out))
	// seidb prints raw bytecode/values nowhere on stdout, but bump the buffer
	// so a long context/progress line never trips the default 64 KiB cap.
	scanner.Buffer(make([]byte, 0, 1024*1024), 4*1024*1024)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "version:":
			if len(fields) < 2 {
				return bucketDigests{}, fmt.Errorf("seidb output: malformed 'version:' line %q", scanner.Text())
			}
			v, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return bucketDigests{}, fmt.Errorf("parsing version %q: %w", fields[1], err)
			}
			bd.Version = v
			haveVersion = true
		case "account":
			if v, ok := digestField(fields, "bucket_digest="); ok {
				bd.Account = v
				haveAccount = true
			}
		case "code":
			if v, ok := digestField(fields, "bucket_digest="); ok {
				bd.Code = v
				haveCode = true
			}
		case "storage":
			if v, ok := digestField(fields, "bucket_digest="); ok {
				bd.Storage = v
				haveStorage = true
			}
		case "legacy":
			if v, ok := digestField(fields, "bucket_digest="); ok {
				bd.Legacy = v
				haveLegacy = true
			}
		case "FINAL_DIGEST":
			if v, ok := digestField(fields, "digest="); ok {
				bd.Final = v
				haveFinal = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return bucketDigests{}, fmt.Errorf("scanning seidb output: %w", err)
	}

	switch {
	case !haveVersion:
		return bucketDigests{}, fmt.Errorf("seidb output missing 'version:' line")
	case !haveFinal:
		return bucketDigests{}, fmt.Errorf("seidb output missing FINAL_DIGEST line")
	case !haveAccount || !haveCode || !haveStorage || !haveLegacy:
		return bucketDigests{}, fmt.Errorf("seidb output missing a per-bucket digest (account=%t code=%t storage=%t legacy=%t)",
			haveAccount, haveCode, haveStorage, haveLegacy)
	}
	return bd, nil
}

// digestField returns the hex value of the field with the given prefix
// (e.g. "bucket_digest=<hex>"), and false if no such field is present.
func digestField(fields []string, prefix string) (string, bool) {
	for _, f := range fields {
		if rest, ok := strings.CutPrefix(f, prefix); ok {
			return rest, true
		}
	}
	return "", false
}

func buildEndpointDigest(height int64, normalization string, flatkv, memiavl bucketDigests) EndpointDigestRecord {
	perBucket := map[string]bucket{
		"account": {FlatKV: flatkv.Account, MemIAVL: memiavl.Account, Match: flatkv.Account == memiavl.Account},
		"code":    {FlatKV: flatkv.Code, MemIAVL: memiavl.Code, Match: flatkv.Code == memiavl.Code},
		"storage": {FlatKV: flatkv.Storage, MemIAVL: memiavl.Storage, Match: flatkv.Storage == memiavl.Storage},
		"legacy":  {FlatKV: flatkv.Legacy, MemIAVL: memiavl.Legacy, Match: flatkv.Legacy == memiavl.Legacy},
	}
	return EndpointDigestRecord{
		Height:        height,
		Normalization: normalization,
		FlatKVDigest:  flatkv.Final,
		MemIAVLDigest: memiavl.Final,
		PerBucket:     perBucket,
		Match:         flatkv.Final == memiavl.Final,
		AxesProved:    axesProved(normalization),
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
	}
}

// axesProved records which logical axes this digest actually proves. The
// memiavl EVM keyspace carries no balance, and the semantic account decoder
// zeroes the balance field of the account payload, so neither normalization's
// account digest attests balance equivalence — that is the per-block
// comparator's job. We never claim "balance" here; a reader must not infer it.
func axesProved(_ string) []string {
	return []string{"nonce", "code", "code_hash", "storage", "legacy"}
}
