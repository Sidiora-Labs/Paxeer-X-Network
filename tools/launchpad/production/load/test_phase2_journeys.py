import asyncio
import dataclasses
import json
import os
from pathlib import Path
import time
import unittest

from phase2_runner import aggregate_hosts, resolve_routes, route_category, run, subscription_topics
from phase2_types import Artifacts, Bindings, ROOT, bound_document, canonical, digest, source_digest, validate_v2
from workload import MIX, ledger


class MixedWorkloadTests(unittest.TestCase):
    def test_open_schedule_exact_shared_category_counts(self):
        observed = {category: 0 for category in MIX}
        for index in range(10000):
            observed[route_category(index)] += 1
        self.assertEqual(observed, MIX)
        self.assertEqual(sum(observed.values()), 10000)
        self.assertEqual(observed["quotes"], 2000)
        self.assertEqual(observed["writes"], 2000)

    def test_four_host_shards_preserve_peak_mix(self):
        actual = {category: 0 for category in MIX}
        for host in range(4):
            for index in range(2500):
                actual[route_category(index * 4 + host)] += 1
        self.assertEqual(actual, MIX)

    def test_arithmetic_preserves_final_qualification_boundary(self):
        values = ledger()
        self.assertEqual(values["events_24h"], 2000 * 86400)
        self.assertEqual(values["payload_bytes_24h"], 250000 * 400 * 86400)
        self.assertEqual(values["nats_r3_headroom_bytes"], 2000 * 1200 * 72 * 3600 * 3 * 2)
        self.assertFalse(values["capacity_qualified"])
        self.assertEqual(values["final_task"], "9.1")

    def test_absent_current_candidate_inputs_fail_closed(self):
        with self.assertRaises(ValueError):
            Bindings("missing", "missing", "missing", "missing")
        with self.assertRaises(ValueError):
            resolve_routes({})
        with self.assertRaises(ValueError):
            subscription_topics([], 0)
        with self.assertRaises(ValueError):
            aggregate_hosts([], Bindings(*(["0" * 64] * 4)))


class RealMixedJourneys(unittest.TestCase):
    def test_actual_connected_ten_minute_host(self):
        config_file = os.environ.get("LOAD_PHASE2_CONFIG_FILE")
        config_sha = os.environ.get("LOAD_PHASE2_CONFIG_SHA256")
        output = os.environ.get("LOAD_PHASE2_OUTPUT_DIR")
        self.assertTrue(config_file and config_sha and output, "real admitted source-bound config and output directory required; no implicit local simulation")
        config = bound_document(config_file, config_sha)
        self.assertEqual(config["bindings"]["generator"], source_digest())
        artifacts = Artifacts(output)
        try:
            result = asyncio.run(asyncio.wait_for(run(config, artifacts), 1800))
            self.assertEqual(result["accepted_connections"], 25000)
            self.assertEqual(result["authenticated_connections"], 12500)
            self.assertEqual(result["public_connections"], 12500)
            self.assertEqual(result["accepted_subscriptions"], 125000)
            self.assertLessEqual(result["max_subscriptions"], 20)
            self.assertEqual(result["failure_count"], 0)
            self.assertEqual(result["http"]["scheduled"], 1500000)
            self.assertEqual(result["http"]["received"], 1500000)
            self.assertTrue(result["replay_qualified"])
            self.assertTrue(result["latency"]["target_passed"])
            self.assertFalse(result["capacity_qualified"])
            self.assertFalse(result["phase2_scenario_qualified"])
            artifacts.json("result.json", result)
        finally:
            artifacts.close()
            artifacts.json("artifact-sha256.json", artifacts.checksums())


if __name__ == "__main__":
    unittest.main()
