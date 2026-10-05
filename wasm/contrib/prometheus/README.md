# Setup
Enable Prometheus metrics in `paxd` (node home `~/.paxeer` by default):

* Edit `<node home>/config/app.toml`
```toml
[telemetry]

# Enabled enables the application telemetry functionality. When enabled,
# an in-memory sink is also enabled by default. Operators may also enabled
# other sinks such as Prometheus.
enabled = true
# ...

# PrometheusRetentionTime, when positive, enables a Prometheus metrics sink.
prometheus-retention-time = 15
```

`prometheus-retention-time` must be > 0 (see the Prometheus scrape config)


* Edit `<node home>/config/config.toml`
```toml
[instrumentation]

# When true, Prometheus metrics are served under /metrics on
# PrometheusListenAddr.
# Check out the documentation for the list of available metrics.
prometheus = true
```

Application telemetry is served by the REST API at `http://localhost:1317/metrics?format=prometheus`
(note the `format` parameter). Consensus metrics are served on the instrumentation listen address,
`:26660` by default, which is what [`prometheus.yaml`](prometheus.yaml) scrapes.


# Local testing
## Run Prometheus
From the `wasm/` directory:
```sh
# port 9090 is used by the node's gRPC server already
docker run -it -v $(pwd)/contrib/prometheus:/prometheus  -p9091:9090  prom/prometheus --config.file=/prometheus/prometheus.yaml
```
* Open the console at `http://localhost:9091` and look for `wasm_` metrics

## Run Grafana

```shell
docker run -it -p 3000:3000 grafana/grafana
```
* Add Prometheus data source
`http://host.docker.internal:9091`
### Metrics
The wasm keeper records timings (telemetry `MeasureSince`) under the keys `wasm_contract_instantiate`,
`wasm_contract_execute`, `wasm_contract_migrate`, and `wasm_contract_ibc-*` for the contract IBC callbacks.
