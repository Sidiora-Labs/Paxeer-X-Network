package cliutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	toolswatch "k8s.io/client-go/tools/watch"
)

// MatchPhase decides whether a single CR event satisfies the --until
// condition. Returns (true, nil) on match, (false, error) on terminal
// Failed phase, (false, nil) otherwise so the watch keeps streaming. The
// CR-agnostic mechanism each tree shares; the legal --until set differs
// per tree and is validated at parse time (see ValidatePhase).
func MatchPhase(obj *unstructured.Unstructured, until string) (bool, error) {
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	if phase == until {
		return true, nil
	}
	if phase == "Failed" {
		msg, _, _ := unstructured.NestedString(obj.Object, "status", "plan", "failedTaskDetail", "error")
		if msg == "" {
			msg = "(no failedTaskDetail.error on status.plan)"
		}
		return false, fmt.Errorf("terminal Failed phase: %s", msg)
	}
	return false, nil
}

// MatchPhaseAtLeastGeneration is MatchPhase gated on the controller having
// observed at least minGeneration. A terminal phase (Complete or Failed) is
// honored only once status.observedGeneration catches up to the generation the
// caller applied, so an apply-then-watch cannot false-green on — or replay the
// stale failure of — a prior run of a re-applied CR. minGeneration == 0 is the
// ungated case (observedGeneration defaults to 0), so MatchPhase's existing
// callers are unaffected.
func MatchPhaseAtLeastGeneration(obj *unstructured.Unstructured, until string, minGeneration int64) (bool, error) {
	observed, _, _ := unstructured.NestedInt64(obj.Object, "status", "observedGeneration")
	if observed < minGeneration {
		return false, nil
	}
	return MatchPhase(obj, until)
}

// RunWatch streams every event for the named resource as one NDJSON line
// on out, returning nil when MatchPhase(obj, until) is satisfied and a
// metav1.Status-shaped error on timeout / terminal Failed / API error.
func RunWatch(ctx context.Context, cfg *rest.Config, gvr schema.GroupVersionResource, ns, name, until string, timeout time.Duration, out io.Writer) error {
	return RunWatchGen(ctx, cfg, gvr, ns, name, until, 0, timeout, out)
}

// RunWatchGen is RunWatch with a generation gate: a terminal phase is honored
// only once status.observedGeneration >= minGeneration (see
// MatchPhaseAtLeastGeneration). Pass the generation returned by the apply so
// the watch cannot match a stale terminal result the controller has not
// re-confirmed for this apply. minGeneration == 0 is the ungated RunWatch.
func RunWatchGen(ctx context.Context, cfg *rest.Config, gvr schema.GroupVersionResource, ns, name, until string, minGeneration int64, timeout time.Duration, out io.Writer) error {
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("build dynamic client: %w", err)
	}
	resource := dyn.Resource(gvr).Namespace(ns)
	fieldSelector := "metadata.name=" + name
	lw := &cache.ListWatch{
		ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
			opts.FieldSelector = fieldSelector
			return resource.List(ctx, opts)
		},
		WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
			opts.FieldSelector = fieldSelector
			return resource.Watch(ctx, opts)
		},
	}

	enc := json.NewEncoder(out)
	condition := func(event watch.Event) (bool, error) {
		if event.Type == watch.Error {
			return false, apierrors.FromObject(event.Object)
		}
		obj, ok := event.Object.(*unstructured.Unstructured)
		if !ok {
			return false, fmt.Errorf("unexpected object type %T", event.Object)
		}
		if err := enc.Encode(obj.Object); err != nil {
			return false, fmt.Errorf("encode NDJSON: %w", err)
		}
		return MatchPhaseAtLeastGeneration(obj, until, minGeneration)
	}

	watchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	_, err = toolswatch.UntilWithSync(watchCtx, lw, &unstructured.Unstructured{}, nil, condition)
	if err != nil {
		// UntilWithSync returns wait.ErrWaitTimeout for both deadline and
		// cancellation; the watchCtx error preserves which one.
		if ctxErr := watchCtx.Err(); ctxErr != nil {
			err = ctxErr
		}
		return WatchExitError(err, name, ns, until, timeout)
	}
	return nil
}

// WatchExitError shapes the err that came out of UntilWithSync into a
// metav1.Status so stderr discrimination (`jq -r .reason`) covers
// timeout / NotFound / terminal-Failed-phase / transient API failure
// uniformly.
func WatchExitError(err error, name, ns, until string, timeout time.Duration) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &apierrors.StatusError{ErrStatus: metav1.Status{
			TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"},
			Status:   metav1.StatusFailure,
			Reason:   metav1.StatusReasonTimeout,
			Message:  fmt.Sprintf("watch %s/%s timed out after %s waiting for phase=%s", ns, name, timeout, until),
			Code:     http.StatusGatewayTimeout,
		}}
	}
	return err
}

// ValidatePhase rejects an --until value not in the resource's phase enum
// at parse time, so an illegal phase is a crisp Invalid usage error rather
// than a full-timeout wait. legal lists the allowed phases for the message.
func ValidatePhase(until string, legal []string) error {
	for _, p := range legal {
		if until == p {
			return nil
		}
	}
	set := ""
	for i, p := range legal {
		if i > 0 {
			set += ", "
		}
		set += p
	}
	return UsageError("invalid --until=%q; legal phases: %s", until, set)
}
