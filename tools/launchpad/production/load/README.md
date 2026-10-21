# L48 workload primitives

Run `python3 -m unittest discover -s tools/launchpad/production/load -p 'test_*.py' -v`.
Only loopback ports 24800/24801 are opened; occupied ports fail rather than reusing peers.
The tests collect actual monotonic sender/receiver timestamps, retain every scheduled
arrival, and exercise a real two-second receiver pause and RFC6455 masked subscription.
The receiver is a calibration echo, not an application gateway or chain fixture.

`workload.ledger()` returns exact fraction/decimal-byte accounting for the shared
500,000 DAU model. Quotes and writes are included in total HTTP RPS. It does not
assert measured capacity. `scheduler.run` schedules independently of completions,
keeps timeout/missed/error arrivals, and bounds memory and concurrent tasks.
Latency starts at scheduled arrival; sender/receiver monotonic clocks must never
be subtracted across different hosts. Missing arrivals remain explicit failures,
not fabricated latency samples.

`calibration.evaluate` checks raw sample digest and counters, resource deltas,
expiry, four distinct host labels and candidate/topology/generator bindings. It
is a metric validator, not trusted host identity/clock attestation. A coordinator
must collect and authenticate external host/clock/NIC telemetry before treating
its generator-calibrated result as evidence. It always returns capacity false.
CPU readings are host aggregate; dedicated generator/cgroup sampling is required
on shared hardware. NIC speed must be measured from the actual hardware rather
than inferred from loopback. This local suite never certifies those prerequisites.

Frozen schema validation requires installed Python jsonschema (current environment
4.25.1); dependencies and the root qualifier/plugin registry remain integrator owned.
Full managed-provider playback, 4-host calibration, 100k/200k sockets, 24h soak,
reconciliation/lag and durability measurements remain unexecuted.
