# Synthetic incident

Owner: platform-oncall. Incident commander: incident-commander. Acknowledge <=5 minutes; customer updates <=15 minutes.

Probe browse, quote, wallet challenge, metadata and WebSocket replay every30s with <=5s deadline and a designated nonprivileged synthetic identity. No signing key, economic write or mainnet transaction is permitted. Check actual route result and response schema, not green process health. A failed route or absent telemetry pages within two minutes once collectors are wired. Publish affected journey, verified impact and next update time; avoid wallet/identity disclosure. Recover route and replay cursor before closing incident.

Evidence: UTC timestamps, release digest, configuration revision, eligible counts, affected route templates, resource inventory, observed failure/recovery and rehearsal result. Keep sensitive forensic identifiers in access-controlled incident artifacts, never public metrics.

Release prerequisite: named human responder mapping and a recorded rehearsal; role names here do not establish staffed coverage.
