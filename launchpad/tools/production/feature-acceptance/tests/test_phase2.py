import importlib.util
import json
import pathlib
import tempfile
import unittest

DIRECTORY = pathlib.Path(__file__).resolve().parents[1]
ROOT = DIRECTORY.parents[2]
spec = importlib.util.spec_from_file_location('journey_collection', DIRECTORY/'collect_journeys.py')
collector = importlib.util.module_from_spec(spec)
spec.loader.exec_module(collector)
journey = collector.journey
policy = journey.policy


class Phase2Tests(unittest.TestCase):
    def setUp(self):
        self.inventory = json.loads((DIRECTORY/'inventory.json').read_text())
        self.context = {key: policy.digest({'binding': key, 'scope': 'policy-negative-cases-only'}) for key in policy.BINDINGS}
        self.context.update(inventory=policy.digest(self.inventory), environment='policy-negative-cases', implementation_principals=['implementation-owner'], run_id='negative-policy-invocation', harness='policy-rejection-tests')
        self.authority = {'providers': {}, 'settlements': {}, 'native_builds': {}}
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)

    def collect(self, bundles, dependencies):
        return collector.collect(bundles, inventory=self.inventory, context=self.context, trust={}, artifact_root=self.temp.name, dependencies=dependencies, authority=self.authority, now=2000000000)

    def test_actual_inventory_all_55_owners_and_missing_proofs_remain_blocked(self):
        result = self.collect([], {})
        self.assertEqual(len(result['features']), 55)
        self.assertEqual({r['feature'] for r in result['features']}, policy.FEATURES)
        self.assertTrue(all(r['owner_lanes'] and r['requirement_owner_lanes'] and r['state'] == 'blocked' for r in result['features']))
        self.assertTrue(all(b['owner'] and b['dependency'] and b['remediation'] for b in result['blockers']))
        self.assertFalse(result['complete_feature_policy_coverage'])
        self.assertFalse(result['release_qualified'])

    def test_dependency_declaration_does_not_create_owner_execution(self):
        dependencies = {d['id']: True for f in self.inventory['features'] for d in f['prerequisites']}
        result = self.collect([], dependencies)
        self.assertTrue(all(r['state'] == 'pending' for r in result['features']))
        self.assertEqual(len(result['blockers']), 55)
        self.assertFalse(result['complete_feature_policy_coverage'])

    def test_caller_claimed_pass_not_an_owner_result(self):
        with self.assertRaises(policy.Rejected):
            self.collect([{'feature': 'PF21', 'passed': True}], {})

    def test_duplicate_owner_result_rejected(self):
        bad = {'feature': 'PF21', 'evidence': {}, 'review': {}, 'journey': {}}
        with self.assertRaises(policy.Rejected):
            self.collect([bad, bad], {})

    def test_truthy_dependency_is_not_admitted_boolean(self):
        dependencies = {d['id']: 1 for f in self.inventory['features'] for d in f['prerequisites']}
        result = self.collect([], dependencies)
        self.assertTrue(all(r['state'] == 'blocked' for r in result['features']))

    def test_actual_v2_catalog_funding_voice_bounty_automation_stay_unavailable(self):
        catalog = json.loads((ROOT/'spec/production-system/integration/contracts/v2/capabilities.json').read_text())
        selected = [c for c in catalog['capabilities'] if c['id'] in {'funding', 'voice', 'bounty', 'automation'}]
        self.assertGreaterEqual(len(selected), 3)
        for capability in selected:
            with self.subTest(capability=capability['id']):
                self.assertEqual(capability['state'], 'blocked')
                self.assertTrue(capability['releaseBlockers'])
                self.assertIsNone(capability['qualificationEvidence'])

    def test_native_responsive_web_substitution_rejected(self):
        with self.assertRaises(policy.Rejected):
            journey.native_device({'type': 'responsive_web'}, self.context, self.authority, {})

    def test_economic_unsupported_authority_cannot_pass(self):
        record = {'type': 'settlement', 'feature': 'PF28', 'registry': 'registry', 'contract_address': '0x'+'1'*40, 'code_sha256': '1'*64, 'action': 'settle', 'transaction': {}, 'receipt': {}, 'transfers': []}
        with self.assertRaisesRegex(policy.Rejected, 'unsupported settlement authority'):
            journey.settlement(record, 'PF28', {'registry': 'registry'}, self.authority)

    def test_funding_vendor_status_without_actual_provider_not_accepted(self):
        record = {'type': 'provider_chain125', 'registry': 'registry', 'provider_id': 'not-admitted', 'support_sha256': '1'*64, 'license_sha256': '2'*64, 'direction': 'deposit', 'rail': {}, 'intent': {}, 'transaction': {}, 'receipt': {}, 'transfer': {}}
        with self.assertRaisesRegex(policy.Rejected, 'provider chain125 support not admitted'):
            journey.funding(record, {'registry': 'registry'}, self.authority)

    def test_voice_blocker_preserves_disabled_capability(self):
        record = {'type': 'voice_blocker', 'capabilities': {key: False for key in journey.VOICE_FIELDS}, 'http_status': 503, 'reason': 'Provider unqualified', 'owner': 'L38', 'dependency': 'voice_provider', 'remediation': 'Admit actual provider evidence', 'review_date': '2033-05-18', 'room_state': 'scheduled', 'provider_session': None}
        self.assertIs(journey.voice(record, self.authority), False)
        record['capabilities']['speak'] = True
        with self.assertRaisesRegex(policy.Rejected, 'grants capability'):
            journey.voice(record, self.authority)

    def test_voice_enabled_without_real_provider_rejected(self):
        record = {'type': 'voice_provider', 'capabilities': {key: True for key in journey.VOICE_FIELDS}, 'provider_id': 'not-admitted', 'session_id': 'session', 'join_receipt': 'join', 'revocation_receipt': 'revoked', 'end_receipt': 'end'}
        with self.assertRaisesRegex(policy.Rejected, 'voice provider not admitted'):
            journey.voice(record, self.authority)

    def test_boolean_and_noncanonical_chain_quantity_rejected(self):
        for value in [True, 125, '125', '0x00', '0x', None]:
            with self.subTest(value=value), self.assertRaises(policy.Rejected):
                journey.quantity(value)

    def test_zero_and_noncanonical_economic_amount_rejected(self):
        for value in [0, '0', '01', '-1', '1.0', str(2**256)]:
            with self.subTest(value=value), self.assertRaises(policy.Rejected):
                journey.amount(value)

    def test_collection_schema_requires_distinct_native_and_web_types(self):
        schema = json.loads((DIRECTORY/'journey-evidence.schema.json').read_text())
        variants = schema['$defs']['artifact']['oneOf']
        kinds = {v['properties']['type']['const'] for v in variants}
        self.assertTrue({'provider_chain125', 'native_device', 'responsive_web', 'voice_provider', 'voice_blocker', 'settlement'}.issubset(kinds))


class Phase3CollectionTests(unittest.TestCase):
    setUp = Phase2Tests.setUp
    def test_phase3_exact_audit_authority_coverage_required(self):
        with self.assertRaisesRegex(policy.Rejected, 'audit authority exact feature coverage'):
            collector.collect([], inventory=self.inventory, context=self.context, trust={}, artifact_root=self.temp.name, dependencies={}, authority=self.authority, now=2000000000, audit_authority={})

    def test_phase3_absent_real_owner_evidence_stays_blocked(self):
        result = collector.collect([], inventory=self.inventory, context=self.context, trust={}, artifact_root=self.temp.name, dependencies={}, authority=self.authority, now=2000000000, audit_authority={feature: {} for feature in policy.FEATURES})
        self.assertEqual(result['version'], 3)
        self.assertEqual(result['task'], '3.50')
        self.assertTrue(all(feature['state'] == 'blocked' for feature in result['features']))
        self.assertFalse(result['complete_feature_policy_coverage'])
        self.assertFalse(result['release_qualified'])

    def test_phase3_bundle_cannot_omit_independent_audit(self):
        with self.assertRaisesRegex(policy.Rejected, 'owner result bundle fields'):
            collector.collect([dict(feature='PF21', evidence={}, review={}, journey={})], inventory=self.inventory, context=self.context, trust={}, artifact_root=self.temp.name, dependencies={}, authority=self.authority, now=2000000000, audit_authority={feature: {} for feature in policy.FEATURES})

    def test_phase3_audit_schema_and_manifest_match_entrypoints(self):
        schema = json.loads((DIRECTORY/'evidence.schema.json').read_text())
        fields = schema['$defs']['audit_envelope']['properties']['payload']['required']
        self.assertTrue({'evidence_sha256', 'review_sha256', 'bindings', 'run_id', 'required_traces_sha256', 'findings_sha256', 'blockers_sha256', 'redactions'}.issubset(fields))
        manifest = json.loads((DIRECTORY/'phase-3-scenario-manifest.json').read_text())
        evidence_policy = json.loads((DIRECTORY/'phase-3-evidence-policy.json').read_text())
        self.assertEqual(manifest['qualification_entrypoint'], 'journey_policy.qualify_audited_feature')
        self.assertEqual(evidence_policy['collector_cli_phase'], 3)
        self.assertFalse(manifest['release_qualified'])


if __name__ == '__main__':
    unittest.main()
