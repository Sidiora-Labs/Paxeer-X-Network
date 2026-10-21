"""Negative qualification of the real frozen register, without provider calls."""
import copy
import importlib.util
import os
from pathlib import Path
import unittest

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
module_spec = importlib.util.spec_from_file_location("capability_check", HERE / "capability_check.py")
check = importlib.util.module_from_spec(module_spec)
module_spec.loader.exec_module(check)


class CapabilityTests(unittest.TestCase):
    def setUp(self):
        self.document = check.load(ROOT / "spec/production-system/integration/contracts/v1/capabilities.json")
        self.spec = Path(os.environ.get("CAPABILITY_SPEC", str(ROOT / "spec/production-system/spec.kvx")))
        self.authorities = {"workspace": ROOT, "contracts": Path("/root/mk-falcon/contracts"), "evidence": Path("/tmp/kindle-implementation-20261009/results")}

    def errors(self, document=None, materialized=False):
        return check.validate(document or self.document, ROOT, self.spec, self.authorities, materialized)

    def test_real_source_register_consistency(self):
        self.assertEqual(self.errors(), [])

    def test_external_wallet_cannot_be_marked_ready(self):
        document = copy.deepcopy(self.document)
        next(c for c in document["capabilities"] if c["id"] == "external_wallet")["state"] = "ready"
        self.assertTrue(any("ready without" in e for e in self.errors(document)))

    def test_absent_deployment_cannot_be_marked_ready(self):
        document = copy.deepcopy(self.document)
        cap = next(c for c in document["capabilities"] if c["id"] == "conditional_orders")
        cap["state"] = "ready"
        self.assertTrue(any("optional deployment absent" in e for e in self.errors(document)))

    def test_waiving_required_feature_fails(self):
        document = copy.deepcopy(self.document)
        document["capabilities"][0]["requiredForRelease"] = False
        self.assertTrue(self.errors(document))

    def test_stale_source_and_unknown_catalog_fail(self):
        document = copy.deepcopy(self.document)
        document["sources"][0]["sha256"] = "0" * 64
        document["capabilities"][0]["endpointRefs"].append("IF_INVENTED")
        errors = self.errors(document)
        self.assertTrue(any("stale source hash" in e for e in errors))
        self.assertTrue(any("invalid endpoint" in e for e in errors))

    def test_missing_required_artifact_fails(self):
        document = copy.deepcopy(self.document)
        document["requiredArtifacts"].append("spec/production-system/integration/contracts/v1/nonexistent.json")
        self.assertTrue(any("missing required frozen artifact" in e for e in self.errors(document, True)))

    def test_source_traversal_fails(self):
        document = copy.deepcopy(self.document)
        document["sources"][0]["path"] = "../../outside"
        self.assertTrue(any("escapes authority" in e for e in self.errors(document)))

    def test_empty_json_runtime_proof_fails(self):
        self.assertTrue(check.proof_errors({}, "provider", "external_wallet", "digest", 1))

    def test_embedded_provider_cannot_return_as_core_requirement(self):
        document = copy.deepcopy(self.document)
        document["provider"] = {"id": "hosted_signer"}
        self.assertTrue(any("superseded embedded provider" in e for e in self.errors(document)))

    def test_unconfigured_walletconnect_cannot_be_mandatory(self):
        document = copy.deepcopy(self.document)
        document["wallet"]["walletConnect"]["requiredForCore"] = True
        self.assertTrue(any("WalletConnect cannot be" in e for e in self.errors(document)))

    def test_source_only_external_wallet_ready_is_rejected(self):
        document = copy.deepcopy(self.document)
        cap = next(c for c in document["capabilities"] if c["id"] == "external_wallet")
        cap["state"] = "ready"
        for pre in cap["prerequisites"]:
            pre["state"] = "verified"
            pre["sourceRefs"] = ["core-manifest"]
        cap["qualificationEvidence"] = {"outcome": "passed", "statusCodesOnly": False, "sourceRefs": ["core-manifest"]}
        errors = self.errors(document)
        self.assertTrue(any("runtime prerequisite missing fresh proof file" in e for e in errors))
        self.assertTrue(any("source-only qualification evidence forbidden" in e for e in errors))

    def test_misstated_scope_and_501_are_not_proof(self):
        proof = {"kind": "source", "capabilityId": "external_wallet", "outcome": "passed", "exitCode": 0, "checks": {"HTTP501": True}, "statusCodesOnly": True}
        self.assertTrue(check.proof_errors(proof, "provider", "external_wallet", "digest", 1))

    def test_empty_materialized_catalog_rejected(self):
        schemas = {ref for cap in self.document["capabilities"] for ref in cap["schemaRefs"]}
        endpoints = {ref for cap in self.document["capabilities"] for ref in cap["endpointRefs"]}
        self.assertTrue(check.catalog_errors({}, {}, {}, schemas, endpoints))

    def test_actual_materialized_bundle_maps_catalog_not_defs_count(self):
        bundle = Path(os.environ.get("CAPABILITY_MATERIALIZED_ROOT", str(ROOT))) / "spec/production-system/integration/contracts/v1"
        definitions = check.load(bundle / "schema-definitions.json")["$defs"]
        catalog = check.load(bundle / "catalog-map.json")
        api = check.load(bundle / "openapi.json")
        schemas = {ref for cap in self.document["capabilities"] for ref in cap["schemaRefs"]}
        endpoints = {ref for cap in self.document["capabilities"] for ref in cap["endpointRefs"]}
        self.assertEqual(check.catalog_errors(definitions, catalog, api, schemas, endpoints), [])
        corrupt = copy.deepcopy(catalog)
        corrupt["schemas"][next(iter(corrupt["schemas"]))]["wireName"] = "AbsentDefinition"
        self.assertTrue(check.catalog_errors(definitions, corrupt, api, schemas, endpoints))


if __name__ == "__main__":
    unittest.main()
