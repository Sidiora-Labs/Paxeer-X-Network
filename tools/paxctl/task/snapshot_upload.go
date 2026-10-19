package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/urfave/cli/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sei-protocol/seictl/internal/cliutil"
	sidecar "github.com/sei-protocol/seictl/sidecar/client"
)

// snapshotUploadResult is the CLI's read-side view of the handler's structured
// result, parsed off TaskResult.Result. Outcome/NoopReason are typed against the
// single wire definition (re-exported by sidecar/client), so a handler-side
// rename is a compile error here rather than a silent misclassification.
type snapshotUploadResult struct {
	Outcome    sidecar.UploadOutcome `json:"outcome"`
	NoopReason sidecar.NoopReason    `json:"noopReason"`
	Height     int64                 `json:"height"`
	Key        string                `json:"key"`
}

func snapshotUploadAction(ctx context.Context, c *cli.Command) error {
	node := c.String("node")
	chain := c.String("chain")
	if node == "" && chain == "" {
		cliutil.EmitStatus(os.Stderr, cliutil.UsageError("one of --node or --chain is required"))
		return cli.Exit("", 1)
	}
	if node != "" && chain != "" {
		cliutil.EmitStatus(os.Stderr, cliutil.UsageError("--node and --chain are mutually exclusive: --node is explicit, --chain discovers"))
		return cli.Exit("", 1)
	}

	interval := c.Duration("poll-interval")
	if interval <= 0 {
		cliutil.EmitStatus(os.Stderr, cliutil.UsageError("--poll-interval must be positive (got %s)", interval))
		return cli.Exit("", 1)
	}
	timeout := c.Duration("timeout")
	if timeout <= 0 {
		cliutil.EmitStatus(os.Stderr, cliutil.UsageError("--timeout must be positive (got %s)", timeout))
		return cli.Exit("", 1)
	}

	cfg, ns, err := resolveKube(c)
	if err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}

	if node == "" {
		node, err = discoverPublishNode(ctx, cfg, ns, chain)
		if err != nil {
			cliutil.EmitStatus(os.Stderr, err)
			return cli.Exit("", 1)
		}
		fmt.Fprintf(os.Stderr, "seictl: discovered snapshot-publish node %s for chain %s\n", node, chain)
	}

	sc, err := newSidecarClient(cfg, ns, node, int32(c.Int("port")))
	if err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}

	id := uuid.New()
	fmt.Fprintf(os.Stderr, "seictl: submitting snapshot-upload-once %s to %s/%s (poll every %s, timeout %s)\n",
		id, ns, node, interval, timeout)

	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res, err := runSnapshotUpload(pollCtx, sc, id, interval)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			cliutil.EmitStatus(os.Stderr, timeoutStatus(id, node, timeout))
			return cli.Exit("", 1)
		}
		if errors.Is(err, errTaskDeleted) {
			cliutil.EmitStatus(os.Stderr, deletedStatus(id, node))
			return cli.Exit("", 1)
		}
		cliutil.EmitStatus(os.Stderr, fmt.Errorf("snapshot-upload-once %s on %s: %w", id, node, err))
		return cli.Exit("", 1)
	}

	msg, cerr := classifyUpload(res)
	// The stdout JSON payload is best-effort: the exit code is driven by
	// classifyUpload, not by whether the render succeeds, so a write error here
	// must not mask the verdict (unlike the sibling verbs, whose payload is the
	// whole point and whose printJSON error is checked).
	_ = printJSON(os.Stdout, res)
	if cerr != nil {
		cliutil.EmitStatus(os.Stderr, cerr)
		return cli.Exit("", 1)
	}
	fmt.Fprintf(os.Stderr, "seictl: %s\n", msg)
	return nil
}

// runSnapshotUpload submits one snapshot-upload-once with a caller-generated,
// fresh task ID and polls it to a terminal state. The fresh ID is load-bearing:
// the engine coalesces a reused ID onto an existing Completed row without
// re-running, so reusing one reads back a stale result and never uploads.
func runSnapshotUpload(ctx context.Context, sc *sidecar.SidecarClient, id uuid.UUID, interval time.Duration) (*sidecar.TaskResult, error) {
	req := sidecar.SnapshotUploadOnceTask{}.ToTaskRequest()
	req.Id = &id
	if _, err := sc.SubmitTask(ctx, req); err != nil {
		return nil, fmt.Errorf("submit: %w", err)
	}
	return pollUntilTerminal(ctx, sc, id, interval)
}

// errTaskDeleted signals a 404 on GET after a successful submit: the task row is
// gone (deleted or cancelled out-of-band), so it will never reach terminal and
// the poll returns at once rather than burning the full --timeout. Distinct from
// a transient GET error and from a task that ran and failed.
var errTaskDeleted = errors.New("task deleted while awaiting completion")

// pollUntilTerminal GETs the task on interval until it reaches completed or
// failed, or ctx expires. A transient GetTask error while ctx is still live
// (e.g. a brief rbac-proxy restart mid-upload) is logged to stderr and the poll
// continues to the next tick: the sidecar keeps running the upload, so aborting
// here would only strand a multi-GB upload and let Job backoff submit a
// redundant concurrent one. A 404 (ErrNotFound) is the exception — the row is
// gone and cannot become terminal, so the poll returns errTaskDeleted at once.
// Only ctx expiry otherwise ends the loop with an error (DeadlineExceeded on
// timeout), which the caller maps to a distinct exit message. A persistent GET
// failure stays visible on stderr across ticks.
func pollUntilTerminal(ctx context.Context, sc *sidecar.SidecarClient, id uuid.UUID, interval time.Duration) (*sidecar.TaskResult, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		res, err := sc.GetTask(ctx, id)
		if err != nil {
			if errors.Is(err, sidecar.ErrNotFound) {
				return nil, errTaskDeleted
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			fmt.Fprintf(os.Stderr, "seictl: polling snapshot-upload-once %s: %v (retrying next tick)\n", id, err)
		} else {
			switch res.Status {
			case sidecar.Completed, sidecar.Failed:
				return res, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// classifyUpload maps a terminal TaskResult to (summary, err): a completed
// upload or noop is healthy (nil err, exit 0); a failed task or a completed
// task with an unrecognized outcome is an error carrying a metav1.Status so the
// stderr `jq -r .reason` discriminator works.
func classifyUpload(res *sidecar.TaskResult) (string, error) {
	switch res.Status {
	case sidecar.Completed:
		r := parseUploadResult(res)
		switch r.Outcome {
		case sidecar.OutcomeUploaded:
			return fmt.Sprintf("uploaded snapshot at height %d (%s)", r.Height, r.Key), nil
		case sidecar.OutcomeNoop:
			return fmt.Sprintf("noop (%s) — healthy: chain has not advanced a snapshot interval since the last upload", r.NoopReason), nil
		default:
			return "", failStatus(metav1.StatusReasonInternalError, http.StatusInternalServerError,
				"snapshot-upload-once completed without a recognized outcome (got %q); expected %q or %q",
				r.Outcome, sidecar.OutcomeUploaded, sidecar.OutcomeNoop)
		}
	case sidecar.Failed:
		detail := "(no error detail)"
		if res.Error != nil && *res.Error != "" {
			detail = *res.Error
		}
		return "", failStatus(metav1.StatusReasonInternalError, http.StatusInternalServerError,
			"snapshot-upload-once failed: %s", detail)
	default:
		return "", failStatus(metav1.StatusReasonInternalError, http.StatusInternalServerError,
			"snapshot-upload-once returned non-terminal status %q", res.Status)
	}
}

func parseUploadResult(res *sidecar.TaskResult) snapshotUploadResult {
	var r snapshotUploadResult
	if res.Result != nil {
		_ = json.Unmarshal(*res.Result, &r)
	}
	return r
}

// timeoutStatus mirrors cliutil.WatchExitError's Timeout shaping so the poll
// bound reads the same as a `workflow state-sync` timeout, and points the
// operator at the cancel path since the task may still run server-side.
func timeoutStatus(id uuid.UUID, node string, timeout time.Duration) error {
	return failStatus(metav1.StatusReasonTimeout, http.StatusGatewayTimeout,
		"snapshot-upload-once %s timed out after %s; the task may still be running server-side — cancel it with `seictl task delete %s --node %s`",
		id, timeout, id, node)
}

// deletedStatus shapes the 404-mid-poll verdict: the task row vanished while
// being awaited, so it never ran to a verdict — distinct from a timeout and from
// a task that ran and failed. Reason NotFound so the `jq -r .reason`
// discriminator tells the three apart.
func deletedStatus(id uuid.UUID, node string) error {
	return failStatus(metav1.StatusReasonNotFound, http.StatusNotFound,
		"snapshot-upload-once %s on %s was deleted while awaiting completion; it will not complete — a delete or cancel raced the poll",
		id, node)
}

func failStatus(reason metav1.StatusReason, code int32, format string, args ...interface{}) error {
	return &apierrors.StatusError{ErrStatus: metav1.Status{
		TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"},
		Status:   metav1.StatusFailure,
		Reason:   reason,
		Message:  fmt.Sprintf(format, args...),
		Code:     code,
	}}
}

var snapshotUploadCmd = cli.Command{
	Name:  "snapshot-upload",
	Usage: "Run one snapshot-upload-once on a node and wait for it to finish",
	Description: "The paved road a per-(network,cluster) CronJob invokes daily. " +
		"Selects one target, submits a snapshot-upload-once with a fresh unique " +
		"task ID, and polls it to a terminal state. " +
		"\n\n" +
		"Target: --node names one explicitly. --chain discovers a " +
		"random pod labelled sei.io/snapshot-publish=true,sei.io/chain=<chain> — " +
		"when no pods carry the labels, discovery reports that and --node " +
		"targets a node directly. " +
		"\n\n" +
		"Exit codes are kubectl-wait-compatible: 0 when the task completes with a " +
		"uploaded or noop outcome (a noop is healthy — the chain has not advanced " +
		"a snapshot interval; the verb prints which); nonzero on a failed task " +
		"(the error prints) or on --timeout (a distinct message; the task may " +
		"still be running server-side, and `task delete` cancels it). " +
		"\n\n" +
		"The terminal TaskResult prints as JSON on stdout; progress and the " +
		"verdict print on stderr. The fresh task ID is load-bearing: the engine " +
		"coalesces a reused ID onto a Completed row and never re-runs.",
	Flags: append([]cli.Flag{
		nodeFlag(false),
		&cli.StringFlag{
			Name:  "chain",
			Usage: "Chain ID to discover a snapshot-publish node for — exact match against the sei.io/chain label (mutually exclusive with --node)",
		},
		&cli.DurationFlag{
			Name:  "timeout",
			Value: 2*time.Hour + 15*time.Minute,
			Usage: "Overall poll bound. Defaults above the sidecar's 2h upload deadline so the CLI bound never fires before the server's does.",
		},
		&cli.DurationFlag{
			Name:  "poll-interval",
			Value: 20 * time.Second,
			Usage: "Interval between task GETs (sidecar-local and cheap)",
		},
	}, commonFlags()...),
	Action: snapshotUploadAction,
}
