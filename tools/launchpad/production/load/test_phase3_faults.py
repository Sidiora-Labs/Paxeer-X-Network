import asyncio
import dataclasses
import os
import unittest

from phase2_types import Artifacts, Bindings, bound_document, digest, source_digest
from phase3_runner import Histogram, IsolatedFaultControl, aggregate_hosts, pool_for, run, skew_topics
from scheduler import Sample


class SkewArithmeticTests(unittest.TestCase):
    def test_exact_hot_cold_subscription_and_read_schedule(self):
        pools = ["0x" + format(index + 1, "040x") for index in range(6)]
        selected = [pool_for(index, pools, subscriptions=True) for index in range(25000)]
        self.assertEqual(selected.count(pools[0]), 20000)
        self.assertEqual(len(selected) - selected.count(pools[0]), 5000)
        subscriptions = [skew_topics(pools, index) for index in range(25000)]
        self.assertTrue(all(len(topics) == 5 and len(set(topics)) == 5 for topics in subscriptions))
        self.assertEqual(sum(len(topics) for index, topics in enumerate(subscriptions) if selected[index] == pools[0]), 100000)
        reads = [pool_for(index, pools) for index in range(10000)]
        self.assertEqual(reads.count(pools[0]), 6000)
        self.assertEqual(len(reads) - reads.count(pools[0]), 4000)

    def test_timeout_error_and_missing_arrivals_keep_full_denominator(self):
        histogram = Histogram()
        for index, status in enumerate(("received", "timeout", "error", "missed")):
            histogram.add(Sample(index, 1000 + index, attempted_ns=None if status == "missed" else 2000 + index, completed_ns=2000000000 + index, status=status))
        result = histogram.result()
        self.assertEqual(result["scheduled"], 4)
        self.assertEqual(result["latency_denominator"], 4)
        self.assertEqual(result["latency_censored"], 1)
        self.assertEqual(result["timeout"], 1)
        self.assertEqual(result["error"], 1)
        self.assertEqual(result["availability"], .25)
        self.assertIsNone(result["latency_ns_upper_bound"]["0.99"])
        self.assertFalse(result["monthly_availability_qualified"])

    def test_missing_inputs_cannot_claim_fault_or_capacity_evidence(self):
        with self.assertRaises(ValueError):
            aggregate_hosts([], Bindings(*(["0" * 64] * 4)))
        with self.assertRaises(ValueError):
            pool_for(0, [])
        with self.assertRaises(ValueError):
            IsolatedFaultControl({"executable": "/proc/self/exe", "sha256": "0" * 64})
        with self.assertRaises(ValueError):
            Histogram().add(Sample(0, 1, completed_ns=2, status="scheduled"))


class RealSkewAndFaultJourney(unittest.TestCase):
    def test_actual_hot_pool_and_isolated_dependency_loss(self):
        path = os.environ.get("LOAD_PHASE3_CONFIG_FILE")
        sha = os.environ.get("LOAD_PHASE3_CONFIG_SHA256")
        output = os.environ.get("LOAD_PHASE3_OUTPUT_DIR")
        self.assertTrue(path and sha and output, "actual source-bound admitted four-host fault workload required")
        config = bound_document(path, sha)
        self.assertEqual(config["bindings"]["generator"], source_digest())
        artifacts = Artifacts(output, phase=3)
        try:
            result = asyncio.run(asyncio.wait_for(run(config, artifacts), 1800))
            self.assertEqual(result["accepted_connections"], 25000)
            self.assertEqual(result["hot_pool_subscribers"], 20000)
            self.assertEqual(result["hot_subscriptions"], 100000)
            self.assertEqual(result["cold_subscriptions"], 25000)
            self.assertEqual(result["http"]["groups"]["all"]["scheduled"], 1500000)
            self.assertTrue(result["cold_pool_p99_passed"])
            self.assertTrue(result["canonical_reconciliation_passed"])
            self.assertTrue(result["authorization_passed"])
            self.assertTrue(result["resources"]["resource_headroom_passed"])
            self.assertEqual(result["forced_close_count"], result["recovered_receiver_count"])
            self.assertFalse(result["capacity_qualified"])
            self.assertFalse(result["phase3_scenario_qualified"])
            self.assertFalse(result["monthly_availability_qualified"])
            if config["host_index"] == 0:
                self.assertEqual([row["dependency"] for row in result["faults"]], ["postgres", "redis", "nats"])
                self.assertTrue(all(row["paused_duration_ns"] >= 2000000000 for row in result["faults"]))
            else:
                self.assertEqual(result["faults"], [])
            artifacts.json("result.json", result)
        finally:
            artifacts.close()
            artifacts.json("artifact-sha256.json", artifacts.checksums())


if __name__ == "__main__":
    unittest.main()
