package stats

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("paxload/stats")

	metrics = struct {
		gasUsed     metric.Int64Histogram
		blockNumber metric.Int64Gauge
		blockTime   metric.Float64Histogram
	}{
		gasUsed: must(meter.Int64Histogram(
			"gas_used",
			metric.WithDescription("Gas used in transactions"),
			metric.WithUnit("{gas}"),
			metric.WithExplicitBucketBoundaries(1, 1000, 10_000, 50_000, 100_000, 200_000, 300_000, 400_000, 500_000, 600_000, 700_000, 800_000, 1_000_000))),
		blockNumber: must(meter.Int64Gauge(
			"block_number",
			metric.WithDescription("Block number in the chain"),
			metric.WithUnit("{height}"))),
		blockTime: must(meter.Float64Histogram(
			"block_time",
			metric.WithDescription("Time taken to produce a block"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0, 2.0, 5.0, 10.0, 20.0))),
	}
)

// must panics if err is non-nil, otherwise returns v.
func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
