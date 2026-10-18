import copy
import hashlib
import importlib.util
import json
import pathlib
import tempfile
import unittest

DIRECTORY = pathlib.Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('feature_phase4_policy', DIRECTORY/'policy.py')
p = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p)
fixture_spec = importlib.util.spec_from_file_location('feature_policy_negative_cases', pathlib.Path(__file__).with_name('test_policy.py'))
fixture = importlib.util.module_from_spec(fixture_spec)
fixture_spec.loader.exec_module(fixture)


class Phase4Tests(unittest.TestCase):
    setUpClass = classmethod(fixture.PolicyTests.setUpClass.__func__)
    tearDownClass = classmethod(fixture.PolicyTests.tearDownClass.__func__)
    setUp = fixture.PolicyTests.setUp
    sign = fixture.PolicyTests.sign
    artifact = fixture.PolicyTests.artifact
    envelopes = fixture.PolicyTests.envelopes
    audit_authority = fixture.PolicyTests.audit_authority

    def assignment(self, feature):
        schemas = p.feature_schemas()
        payload = dict(version=4, feature=feature, bindings={k:self.context[k] for k in p.BINDINGS}, environment=self.context['environment'], run_id=self.context['run_id'], harness=self.context['harness'], tool_sha256=p.feature_tool_digest(), schemas_sha256=p.digest(schemas), producer='producer', reviewer='reviewer', implementation_principals_sha256=p.digest(self.context['implementation_principals']), issued_at=self.now-10, expires_at=self.now+600, disposition='assigned')
        return self.sign(payload, 'reviewer')

    def test_all55_actual_feature_ids_map_to_independently_signed_assignment_schema(self):
        for identity in self.trust.values():
            identity['features'] = sorted(p.FEATURES)
        schemas = p.feature_schemas()
        for feature in sorted(p.FEATURES):
            result = p.review_assignment(feature, self.assignment(feature), context=self.context, trust=self.trust, schemas=schemas, tool_sha256=p.feature_tool_digest(), now=self.now)
            self.assertEqual(result['feature'], feature)
            self.assertNotEqual(result['producer'], result['reviewer'])

    def test_self_review_alias_and_implementation_owner_cannot_be_ready(self):
        schemas = p.feature_schemas()
        for principal in ['producer', 'implementer']:
            self.trust['reviewer']['principal'] = principal
            with self.subTest(principal=principal), self.assertRaisesRegex(p.Rejected, 'self review conflict'):
                p.review_assignment(self.feature, self.assignment(self.feature), context=self.context, trust=self.trust, schemas=schemas, tool_sha256=p.feature_tool_digest(), now=self.now)

    def test_assignment_tool_candidate_schema_and_expiry_mismatch_reject(self):
        schemas = p.feature_schemas()
        envelope = self.assignment(self.feature)
        for field, value in [('tool_sha256','0'*64), ('schemas_sha256','0'*64), ('expires_at',self.now), ('environment','another-environment')]:
            payload = copy.deepcopy(envelope['payload'])
            payload[field] = value
            with self.subTest(field=field), self.assertRaises(p.Rejected):
                p.review_assignment(self.feature, self.sign(payload,'reviewer'), context=self.context, trust=self.trust, schemas=schemas, tool_sha256=p.feature_tool_digest(), now=self.now)

    def test_actual_signed_policy_objects_calibrate_without_changing_original_artifacts(self):
        authority = self.audit_authority()
        evidence, review = self.envelopes()
        before = {a['path']:(self.root/a['path']).read_bytes() for a in self.payload['artifacts']}
        result = p.calibrate_feature_evidence(dict(feature=self.feature,evidence=evidence,review=review), qualification_args=dict(inventory=self.inventory,context=self.context,trust=self.trust,artifact_root=self.root,dependencies=self.dependencies,now=self.now), required_traces=authority['required_traces'])
        self.assertEqual(len(result['checks']),8)
        self.assertTrue(all(check['rejected'] for check in result['checks']))
        self.assertEqual(result['tool_sha256'],p.feature_tool_digest())
        self.assertEqual(before,{a['path']:(self.root/a['path']).read_bytes() for a in self.payload['artifacts']})
        self.assertFalse(result['capacity_qualified'])
        self.assertFalse(result['release_qualified'])

    def test_missing_task9_final_evidence_always_rejects(self):
        authority = dict(capture=None,gate_authority={},gate_artifact_root=self.root,capture_acceptance=None,capture_acceptance_config=None,capture_acceptance_tool_sha256='0'*64)
        with self.assertRaisesRegex(p.Rejected,'missing task9.1 final evidence'):
            p.final_release_closure([], [], closure_authority=authority, readiness_authority={}, inventory=self.inventory, context=self.context, trust=self.trust, artifact_root=self.root, dependencies=self.dependencies, authority={}, audit_authority={}, now=self.now)
        with self.assertRaisesRegex(p.Rejected,'missing task9.1 final capture'):
            p.final_release_closure([], [dict(gate='9.1')], closure_authority=authority, readiness_authority={}, inventory=self.inventory, context=self.context, trust=self.trust, artifact_root=self.root, dependencies=self.dependencies, authority={}, audit_authority={}, now=self.now)

    def test_closure_manifest_uses_all200_lane_tasks_and_real_serial_gate_commands(self):
        gates = p.required_release_gates(DIRECTORY.parents[2]/'spec/production-system/spec.kvx')
        self.assertEqual(sum(g['kind']=='parallel_lane' for g in gates.values()),200)
        self.assertIn('9.1',gates)
        self.assertIn('4.99',gates)
        self.assertTrue(all('--require-current-source' in gate['command'] for gate in gates.values()))

    def test_pinned_actual_object_bytes_reject_tamper(self):
        path = self.root/'object.json'
        raw = p.canonical({'boundary':'policy-negative-case-only'})
        path.write_bytes(raw)
        reference = dict(path=str(path),sha256=hashlib.sha256(raw).hexdigest())
        self.assertEqual(p.pinned_document(reference),p.strict_json(raw))
        path.write_bytes(bytes([raw[0]^1])+raw[1:])
        with self.assertRaisesRegex(p.Rejected,'pinned document mismatch'):
            p.pinned_document(reference)

    def test_phase4_manifests_never_declare_global_capacity(self):
        for name in ['phase-4-scenario-manifest.json','phase-4-evidence-policy.json','final-integrated-gate-readiness.json']:
            manifest = json.loads((DIRECTORY/name).read_text())
            self.assertFalse(manifest['release_qualified'])
            self.assertEqual(manifest['final_gate'],'9.1')
        manifest = json.loads((DIRECTORY/'final-integrated-gate-readiness.json').read_text())
        self.assertEqual(set(manifest['feature_ids']),p.FEATURES)
        self.assertEqual(manifest['final_integrated_evidence_state'],'pending')


if __name__ == '__main__':
    unittest.main()
