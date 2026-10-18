from __future__ import annotations

from scheduler import percentile


def latency_measurements(ingest_rows, browser_rows, *, clock_offset_ns, clock_uncertainty_ns):
    if type(clock_offset_ns) is not int or type(clock_uncertainty_ns) is not int or not 0 <= clock_uncertainty_ns <= 5000000:
        raise ValueError("independently calibrated UTC uncertainty<=5ms required")
    committed = {}
    for row in ingest_rows:
        identity = row["event_id"]
        stamp = int(row["durable_commit_utc_ns"])
        if stamp <= 0 or row["timestamp_authority"] != "postgres_transaction_commit":
            raise ValueError("actual transaction commit timestamp required")
        if identity in committed and committed[identity] != stamp:
            raise ValueError("immutable commit timestamp changed")
        committed[identity] = stamp
    samples, identities = [], set()
    for row in browser_rows:
        identity = row["event_id"]
        if identity not in committed:
            raise ValueError("browser event has no durable ingestion evidence")
        key = (row["receiver_id"], identity)
        if key in identities:
            raise ValueError("duplicate browser latency sample")
        identities.add(key)
        received = int(row["received_utc_ns"]) - clock_offset_ns
        lag = received - committed[identity]
        if lag < -clock_uncertainty_ns:
            raise ValueError("browser timestamp precedes commit beyond clock bounds")
        samples.append(lag + clock_uncertainty_ns)
    if not samples:
        raise ValueError("actual browser delivery samples required")
    measured = {str(p): percentile(samples, p) for p in (.5, .95, .99, .999, 1)}
    return {"sample_count": len(samples), "latency_ns_upper_bound": measured, "clock_offset_ns": clock_offset_ns, "clock_uncertainty_ns": clock_uncertainty_ns, "p99_target_ns": 1000000000, "target_passed": measured["0.99"] <= 1000000000, "capacity_qualified": False}
