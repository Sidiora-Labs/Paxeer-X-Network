# Availability incident

Owner: platform-oncall. Incident commander: incident-commander. Acknowledge <=5 minutes; customer updates <=15 minutes.

Inspect eligible denominator, explicit exclusions, route template, deployment digest and both burn windows. Reads target 99.95% monthly; mutations 99.9%. Below 100 requests/window report insufficient traffic, never infer healthy. Read origin p95<=150ms p99<=400ms; quote p95<=250ms p99<=750ms with healthy dependencies. Inspect queue and RPC durations separately. Reduce admission before saturation, retain durable outbox and verify committed mutation acknowledgments. Roll back only to a reviewed compatible release. Validate recovery against fresh denominators and durable state reconciliation.

Evidence: UTC timestamps, release digest, configuration revision, eligible counts, affected route templates, resource inventory, observed failure/recovery and rehearsal result. Keep sensitive forensic identifiers in access-controlled incident artifacts, never public metrics.

Release prerequisite: named human responder mapping and a recorded rehearsal; role names here do not establish staffed coverage.
