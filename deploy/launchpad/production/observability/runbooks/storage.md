# Storage incident

Owner: platform-oncall. Incident commander: incident-commander. Acknowledge <=5 minutes; customer updates <=15 minutes.

At 80% occupied or oldest consumer >60s inspect byte growth horizon and retained stream/WAL inventory. At 90% reduce unsafe admission through the owning control plane before disk-full; an alert itself does not enforce admission. Never delete unacknowledged durable messages, WAL needed by replicas, or reset consumer cursors. Preserve offsets and source hashes. Replay throttle strictly below 25% of measured spare capacity while customer peak traffic is active; absent measurement means replay disabled. Restore disk headroom then verify canonical high-watermark/outbox reconciliation before resuming.

Evidence: UTC timestamps, release digest, configuration revision, eligible counts, affected route templates, resource inventory, observed failure/recovery and rehearsal result. Keep sensitive forensic identifiers in access-controlled incident artifacts, never public metrics.

Release prerequisite: named human responder mapping and a recorded rehearsal; role names here do not establish staffed coverage.
