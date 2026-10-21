package holdings

import (
	"context"
	"errors"
	"fmt"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"log/slog"
)

type Observer struct {
	counter metric.Int64Counter
	logger  *slog.Logger
}

func NewObserver(logger *slog.Logger) (*Observer, error) {
	counter, err := otel.Meter("stats.holdings.v1").Int64Counter("holdings.transfer.outcomes")
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Observer{counter, logger}, nil
}
func (o *Observer) Committed(ctx context.Context, e Transfer, result ApplyResult) {
	e, _ = e.Normalize()
	outcome := "accepted"
	if result.Duplicate {
		outcome = "duplicate"
	}
	if result.Incident {
		outcome = "rejected"
	}
	o.record(ctx, outcome)
	o.logger.LogAttrs(ctx, slog.LevelInfo, "holdings transfer committed", slog.String("identity", e.Identity()), slog.Int64("revision", result.Revision), slog.String("coverage", result.Coverage), slog.String("outcome", outcome))
}
func (o *Observer) Failed(ctx context.Context) {
	o.record(ctx, "failed")
}

func (o *Observer) record(ctx context.Context, outcome string) {
	for requirement := 13; requirement <= 22; requirement++ {
		o.counter.Add(ctx, 1, metric.WithAttributes(attribute.String("domain", "D05"), attribute.String("requirement", fmt.Sprintf("D05_%03d", requirement)), attribute.String("outcome", outcome)))
	}
}
func (o *Observer) Rejected(ctx context.Context) { o.record(ctx, "rejected") }
func (r *Repository) observeFailure(ctx context.Context, e Transfer, err error) {
	if _, invalid := e.Normalize(); invalid != nil || errors.Is(err, ErrIntegrity) || errors.Is(err, ErrConflict) || errors.Is(err, ErrOrder) || errors.Is(err, ErrUnknownToken) {
		r.observer.Rejected(ctx)
		return
	}
	r.observer.Failed(ctx)
}
