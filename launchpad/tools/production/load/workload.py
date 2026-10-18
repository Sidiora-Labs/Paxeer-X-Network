"""Exact, versioned shared workload; quotes/writes are subsets of HTTP total."""
from fractions import Fraction
MIX = dict(chart=800, portfolio=800, search=600, quotes=2000, writes=2000,
           market=1000, rankings=600, identity_profile=500, metadata_media=500,
           notifications_social=800, capabilities=400)
VERSION = "kindle-500k-dau-v1"
def validate_mix(mix, total=10000):
    if set(mix) != set(MIX) or any(type(v) is not int or v < 0 for v in mix.values()):
        raise ValueError("exact bounded workload categories required")
    if sum(mix.values()) != total:
        raise ValueError("quotes and writes are included in total, not additional")
    return dict(mix)
def ledger():
    validate_mix(MIX)
    return dict(version=VERSION, dau=500000, sessions_per_user=2, seconds_per_session=900,
        requests_per_session=50, requests_per_day=50000000,
        average_concurrency=str(Fraction(500000*2*900,86400)),
        average_rps=str(Fraction(50000000,86400)), peak_http_rps=10000,
        surge_http_rps=20000, peak_mix=dict(MIX), surge_mix={k:v*2 for k,v in MIX.items()},
        sockets_peak=100000, sockets_surge=200000, authenticated_fraction="1/2",
        subscriptions_mean=5, subscriptions_max=20, normal_hot_fraction="3/5",
        stress_hot_fraction="4/5", stress_hot_reads_fraction="3/5",
        events_24h=172800000, raw_event_bytes_24h=207360000000,
        requests_24h=864000000, writes_24h=172800000,
        frames_24h=21600000000, payload_bytes_24h=8640000000000,
        wire_bytes_24h=12960000000000, nats_logical_bytes_72h=622080000000,
        nats_r3_bytes_72h=1866240000000, nats_r3_headroom_bytes=3732480000000,
        broker_metadata_overhead_included=False, video_viewers=20000,
        video_bits_per_second=40000000000, final_task="9.1",
        capacity_qualified=False)

def final_scenarios():
    return {
        "peak-soak": dict(task="9.1", duration_seconds=86400, sockets=100000,
            http_rps=10000, writes_rps=2000, quotes_rps=2000, events_rps=2000,
            frames_rps=250000, payload_bytes_mean=400, wire_bytes_budget_mean=600,
            post_gc_heap_compare_seconds=[14400, 86400], retained_heap_growth_max=.05),
        "connection-surge": dict(task="9.1", baseline_sockets=100000,
            additional_sockets=100000, ramp_seconds=60, sockets=200000,
            hold_seconds=600, recovery_seconds_max=120, authenticated_fraction="1/2",
            subscriptions_mean=5, subscriptions_max=20),
        "fanout-burst": dict(task="9.1", frames_rps=1000000,
            duration_seconds=600, payload_bytes_mean=400,
            wire_bytes_measured=True, slow_readers_fraction="1/100",
            slow_reader_queue_bytes_max=262144, price_updates_per_topic_second_max=2),
        "event-surge": dict(task="9.1", events_rps=10000,
            duration_seconds=300, expected_unique_identities=3000000,
            baseline_events_rps=2000, drain_seconds_max=600,
            durable_ingest_p99_ns_max=500000000, current_chain_throughput_claim=False),
        "managed-video": dict(task="9.1", viewers=20000,
            mean_bits_per_second=2000000, application_video_proxy_allowed=False,
            actual_provider_playback_required=True),
        "hot-pool-skew": dict(task="9.1", baseline_hot_subscribers=80000,
            surge_hot_subscribers=160000, subscriptions_hot_fraction="4/5",
            subscriptions_cold_fraction="1/5", reads_hot_fraction="3/5",
            cold_read_p99_ns_max=400000000,
            total_read_pool_scope_authority_required=True),
    }


def provisioned_budget(document):
    values = ledger()
    required = {
        "canonical_archive_bytes": values["raw_event_bytes_24h"],
        "nats_r3_bytes_with_headroom": values["nats_r3_headroom_bytes"],
        "egress_bytes_per_day": values["wire_bytes_24h"],
        "steady_network_bits_per_second": 250000 * 600 * 8,
        "burst_network_bits_per_second": 1000000 * 600 * 8,
    }
    if set(document["limits"]) != set(required):
        raise ValueError("actual storage and network budget authority required")
    failures = []
    for name, minimum in required.items():
        actual = document["limits"][name]
        if type(actual) is not int or actual < minimum:
            failures.append(name)
    if document.get("retention_seconds", 0) < 259200:
        failures.append("72hour broker retention")
    cost = document.get("cost")
    import math
    if not isinstance(cost, dict) or set(cost) != {"currency", "estimated_daily", "approved_daily", "authority_revision"} or not isinstance(cost["currency"], str) or len(cost["currency"]) != 3:
        raise ValueError("actual measured provision/cost revision required")
    if any(type(cost[key]) not in (int, float) or not math.isfinite(cost[key]) or cost[key] < 0 for key in ("estimated_daily", "approved_daily")):
        raise ValueError("finite operating budget required")
    if cost["estimated_daily"] > cost["approved_daily"]:
        failures.append("operating budget")
    if not isinstance(cost["authority_revision"], str) or not cost["authority_revision"]:
        raise ValueError("operating budget authority missing")
    return dict(required=required, provisioned=document["limits"], failures=failures,
        final_run_permitted=not failures, measured_capacity_qualified=False)
