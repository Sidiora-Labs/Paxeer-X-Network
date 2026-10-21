import asyncio
import json
import os
from pathlib import Path
import unittest

from capture import CAPTURE_VERSION, final_acceptance, read_manifest, validate_record
from phase2_types import Artifacts, Bindings, bound_document, canonical, digest, source_digest
from phase4_runner import readiness, run_isolated, topics_for
from workload import MIX, final_scenarios, ledger, provisioned_budget


class CapacityArithmetic(unittest.TestCase):
    def test_root_full_day_and_retention_arithmetic(self):
        measured = ledger()
        self.assertEqual(measured["events_24h"], 2000 * 24 * 3600)
        self.assertEqual(measured["requests_24h"], 10000 * 24 * 3600)
        self.assertEqual(measured["writes_24h"], 2000 * 24 * 3600)
        self.assertEqual(measured["frames_24h"], 250000 * 24 * 3600)
        self.assertEqual(measured["nats_r3_headroom_bytes"], 2000 * 1200 * 72 * 3600 * 3 * 2)
        self.assertEqual(sum(MIX.values()), 10000)
        self.assertEqual(MIX["quotes"] + MIX["writes"], 4000)
        self.assertFalse(measured["capacity_qualified"])

    def test_final_scenarios_have_exact_wallclock_and_boundary(self):
        scenarios = final_scenarios()
        self.assertEqual(scenarios["peak-soak"]["duration_seconds"], 86400)
        self.assertEqual(scenarios["fanout-burst"]["duration_seconds"], 600)
        self.assertEqual(scenarios["event-surge"]["expected_unique_identities"], 10000 * 300)
        self.assertEqual(scenarios["connection-surge"]["hold_seconds"], 600)
        self.assertTrue(all(scenario["task"] == "9.1" for scenario in scenarios.values()))
        source = json.loads(Path(__file__).with_name("phase-4-scenario-manifest.json").read_text())
        self.assertEqual(source["final_shared_scenarios"], scenarios)
        self.assertEqual(source["isolated_component_proof"]["duration_seconds"], 60)
        self.assertFalse(source["capacity_qualified"])

    def test_actual_budget_shortfalls_fail_and_planning_bytes_are_not_wire_samples(self):
        with self.assertRaises(ValueError):
            provisioned_budget({"limits": {}})
        readiness_source = json.loads(Path(__file__).with_name("final-integrated-gate-readiness.json").read_text())
        self.assertFalse(readiness_source["harness_ready"])
        self.assertFalse(readiness_source["final_gate"]["qualified"])
        self.assertEqual(readiness_source["final_gate"]["task"], "9.1")
        self.assertFalse(readiness_source["capacity_qualified"])
        self.assertFalse(readiness_source["release_qualified"])

    def test_normal_root_skew_is_distinct_from_eighty_percent_stress(self):
        pools = ["0x" + format(index + 1, "040x") for index in range(6)]
        topics = [topics_for(pools, index) for index in range(1000)]
        self.assertTrue(all(len(set(row)) == 5 for row in topics))
        self.assertEqual(sum(all(pools[0] in topic for topic in row) for row in topics), 600)
        with self.assertRaises(ValueError):
            topics_for([], 0)

    def test_absent_capture_and_authority_fail_closed(self):
        with self.assertRaises(Exception):
            validate_record("GatewayInterval", {})
        with self.assertRaises(Exception):
            validate_record("CaptureManifest", {})
        with self.assertRaises(Exception):
            final_acceptance({})
        with self.assertRaises(ValueError):
            readiness({"bindings": dict.fromkeys(("candidate", "interface", "topology", "generator"), "0" * 64), "native_runtime_authority_path": "/not-an-actual-authority", "native_runtime_authority_sha256": "0" * 64}, [])


class ActualCapacityAdapterProof(unittest.TestCase):
    def test_actual_calibrated_isolated_socket_fanout_event_http_host(self):
        path = os.environ.get("LOAD_PHASE4_CONFIG_FILE")
        sha = os.environ.get("LOAD_PHASE4_CONFIG_SHA256")
        output = os.environ.get("LOAD_PHASE4_OUTPUT_DIR")
        self.assertTrue(path and sha and output, "actual admitted host config, native driver and owned capture directory required")
        config = bound_document(path, sha)
        self.assertEqual(config["bindings"]["generator"], source_digest())
        artifacts = Artifacts(output, phase=4)
        try:
            result = asyncio.run(asyncio.wait_for(run_isolated(config, artifacts), 1800))
            self.assertEqual(result["http"]["groups"]["all"]["scheduled"], 150000)
            self.assertEqual(result["http"]["groups"]["all"]["received"], 150000)
            self.assertEqual(result["events"]["scheduled"], 30000)
            self.assertEqual(result["events"]["received"], 30000)
            self.assertEqual(result["accepted_connections"], config["socket_count"])
            self.assertGreater(result["socket_frames"], 0)
            self.assertGreater(result["socket_checkpoints"], 0)
            self.assertLessEqual(result["generator_resources"]["cpu_fraction"], .7)
            self.assertLessEqual(result["generator_resources"]["nic_fraction"], .7)
            self.assertLessEqual(result["generator_resources"]["scheduler_p99_ns"], 10000000)
            self.assertFalse(result["capacity_qualified"])
            self.assertFalse(result["events"]["new_raw_ingestion_qualified"])
            self.assertFalse(result["monthly_availability_qualified"])
            artifacts.json("result.json", result)
        finally:
            artifacts.close()
            artifacts.json("artifact-sha256.json", artifacts.checksums())


if __name__ == "__main__":
    unittest.main()
