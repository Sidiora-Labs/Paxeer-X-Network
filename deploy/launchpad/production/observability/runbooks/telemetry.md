# Telemetry incident

Owner: platform-oncall. Incident commander: incident-commander. Acknowledge <=5 minutes; customer updates <=15 minutes.

Reject high-cardinality labels before export: only finite service/environment/zone/route-template/status-class/error/stage/dependency vocabularies. Per-instance budget20000; do not allow wallet, token, transaction, incidentID or rawURL dimensions. Drop telemetry rather than blocking business commits. Use 512MiB collector memory limit plus runtime/cgroup ceiling; trace failures may be dropped under pressure. Never export arbitrary bodies, JWTs, signatures, stream keys or private messages. Collector positive allowlists are defense in depth; validate scope/span-link/exemplar content at source before enabling pipeline. Inspect redaction canaries in isolated qualification and restrict exporter mTLS and network access.

Evidence: UTC timestamps, release digest, configuration revision, eligible counts, affected route templates, resource inventory, observed failure/recovery and rehearsal result. Keep sensitive forensic identifiers in access-controlled incident artifacts, never public metrics.

Release prerequisite: named human responder mapping and a recorded rehearsal; role names here do not establish staffed coverage.
