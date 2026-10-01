#!/usr/bin/env python3
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
module = importlib.util.spec_from_file_location('paxeer_x_release', ROOT / 'tools/qualification/paxeer_x_release.py')
release = importlib.util.module_from_spec(module)
module.loader.exec_module(release)


class DurableReleaseEvidence(unittest.TestCase):
    def test_imports_candidate_contract(self):
        self.assertEqual(release.SCHEMA, 'paxeer-x.candidate.v1')
        self.assertEqual(release.DEADLINE_SECONDS, 1500)
        self.assertIn('image_digest', release.BINDING_FIELDS)
        self.assertIn('policy_ref', release.BINDING_FIELDS)

    def test_logical_identity_is_canonical_and_changes_with_candidate(self):
        a = {'candidate_sha256': 'a' * 64, 'cells': ['a', 'b']}
        b = {'cells': ['a', 'b'], 'candidate_sha256': 'a' * 64}
        self.assertEqual(release.request_id(a), release.request_id(b))
        b['candidate_sha256'] = 'b' * 64
        self.assertNotEqual(release.request_id(a), release.request_id(b))

    def test_evidence_deadline_refuses_before_work(self):
        with self.assertRaises(release.Invalid):
            release.remaining(0)

    def test_private_material_and_credential_paths(self):
        with tempfile.TemporaryDirectory() as name:
            path = Path(name)
            path.chmod(0o700)
            self.assertEqual(release.private_dir(path), path)
            path.chmod(0o755)
            with self.assertRaises(release.Invalid):
                release.private_dir(path)
            with self.assertRaises(release.Invalid):
                release.protected_path(path / '.env')

    def test_workflow_requires_actual_accepted_authority(self):
        saved = dict(os.environ)
        try:
            for key in ['PAXEER_X_REQUEST', 'PAXEER_X_CANDIDATE_JSON', 'PAXEER_X_CONTRACT_JSON']:
                os.environ.pop(key, None)
            with self.assertRaises((release.Invalid, ValueError)):
                release.workflow_request()
        finally:
            os.environ.clear()
            os.environ.update(saved)


def main():
    proof_path = os.environ.get('PAXEER_X_QUALIFICATION_BUILD_RECORD')
    if not proof_path:
        print('durable qualification: missing private prebuilt Go bundle identity', file=sys.stderr)
        return 2
    try:
        proof = release.load_private(proof_path)
        binary = Path(proof['binary'])
        head = subprocess.check_output(['git', '-C', str(ROOT), 'rev-parse', 'HEAD'], text=True).strip()
        if proof['revision'] != head or proof['sha256'] != hashlib.sha256(binary.read_bytes()).hexdigest():
            raise ValueError('stale or mutated test bundle')
        if not binary.is_file() or binary.is_symlink() or not os.access(binary, os.X_OK):
            raise ValueError('missing test executable')
    except (OSError, KeyError, ValueError):
        print('durable qualification: refused prebuilt bundle identity', file=sys.stderr)
        return 2
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(DurableReleaseEvidence))
    if not result.wasSuccessful():
        print('PAXEER_X_GATE tests=%d skipped=%d' % (result.testsRun, len(result.skipped)))
        return 1
    run = subprocess.run([str(binary), '-test.run=^TestDurableQualification', '-test.v', '-test.timeout=8m'],
                         cwd=ROOT, capture_output=True, text=True, timeout=510, check=False)
    sys.stdout.write(run.stdout)
    sys.stderr.write(run.stderr)
    count = len(re.findall(r'^--- PASS: TestDurableQualification', run.stdout, re.M))
    skipped = len(re.findall(r'--- SKIP:', run.stdout))
    print('PAXEER_X_GATE tests=%d skipped=%d' % (result.testsRun + count, len(result.skipped) + skipped))
    if run.returncode or count < 9 or skipped:
        return 1
    print('Local registry, production types and cryptography qualified; no CI deployment or release certification claimed.')
    return 0


if __name__ == '__main__':
    sys.exit(main())
