# Paxeer X chain metrics

Helpers that `paxd` uses to emit chain-specific metrics on top of the vendored telemetry package in [`sdk/telemetry`](../../sdk/telemetry/metrics.go).

- [`metrics_util.go`](metrics_util.go) holds the counters, gauges and histograms (transaction processing type, block and DeliverTx latency, oracle vote penalties, epochs, EVM gas and base fee, RPC request counts and latency, websocket connects, association and nonce errors, and others). The `Safe*` wrappers recover from a panic in the telemetry call instead of propagating it.
- `SetupOtelMetricsProvider` installs an OpenTelemetry meter provider backed by a Prometheus exporter with the namespace `pax_chain`.
- [`labels.go`](labels.go) defines the transaction processing type labels (`synchronous`, `synchronous_giga`, `optimistic_concurrency`, `occ_giga`).

When telemetry is enabled, `sdk/telemetry` always attaches an in-memory sink with a 10 second interval and 1 minute retention: metrics are aggregated over 10 seconds and kept for 1 minute.
