# Integrity incident

Owner: platform-oncall. Incident commander: incident-commander. Acknowledge <=5 minutes; customer updates <=15 minutes.

Paxeer chain125 has committed finality: divergent hashes at the same height are integrity incidents, not normal reorgs. Isolate disagreeing RPC nodes, preserve signed deployment pins and height/hash/response evidence in restricted incident storage. Disable quotes and unsafe market submissions through the shared capability control. Escalate to chain operators; never majority-vote an untrusted history or invent confirmation delay. Serve historical reads only with last verified committed height and age. Resume only after operator reconciliation and pinned node integrity checks.

Evidence: UTC timestamps, release digest, configuration revision, eligible counts, affected route templates, resource inventory, observed failure/recovery and rehearsal result. Keep sensitive forensic identifiers in access-controlled incident artifacts, never public metrics.

Release prerequisite: named human responder mapping and a recorded rehearsal; role names here do not establish staffed coverage.
