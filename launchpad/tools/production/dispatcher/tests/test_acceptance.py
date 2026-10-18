import json
from pathlib import Path
import shutil
import sys
import tempfile
import time
import unittest
sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from dispatcher import acceptance, evidence, patches
from dispatcher.store import Store, StoreError

CAP = dict(cpu=8, memory=8192, pids=100, disk=10000, executor=30, go_build=2, container_start=1)
RES = dict(cpu=1, memory=100, pids=1, disk=10, executor=1, go_build=0, container_start=0)

class AcceptanceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(); self.root = Path(self.temp.name)
        self.base = self.root/'base'; self.base.mkdir()
        (self.base/'service').mkdir()
        (self.base/'service/value.txt').write_text('old')
        (self.base/'check.py').write_text("import unittest\nfrom pathlib import Path\nclass Real(unittest.TestCase):\n def test_value(self): self.assertEqual(Path('service/value.txt').read_text(), 'new')\nunittest.main()\n")
        self.candidate = self.root/'candidate'; shutil.copytree(self.base, self.candidate)
        (self.candidate/'service/value.txt').write_text('new')
        self.owned = ['service/**']
        self.patch = patches.build_patch(self.base, self.candidate, self.owned, self.root/'patch')
        self.secret = b'coordinator-only-acceptance-key-00001'
        self.binding = dict(baseline=evidence.source_digest(self.base), candidate=evidence.source_digest(self.candidate), interface='a'*64, layout='b'*64)
        self.store = Store(self.root/'leases.sqlite', CAP, 'boot-one')
        self.token = self.store.claim('1.1', 'L01', 'worker', self.binding, RES)
        self.token['attempt_dir'] = str(self.root/'attempt')
        self.checks = [dict(id='actual', command=[sys.executable, 'check.py'], timeout=5, inputs=['service/value.txt'], proof='unittest')]
        self.result = evidence.run_checks(self.checks, self.candidate, Path(self.token['attempt_dir'])/'checks', self.binding, self.token, self.secret)
    def tearDown(self): self.store.close(); self.temp.cleanup()
    def accept(self):
        return acceptance.accept_artifact(self.store, self.token, self.candidate, self.base, self.patch, self.owned, self.checks, self.secret, self.root/'accepted')
    def stage(self, artifact):
        return acceptance.stage_accepted(self.base, [artifact['path']], {'1.1':self.owned}, {'1.1':self.checks}, self.secret, self.root/'stage', verify=[sys.executable, 'check.py'], store=self.store)
    def test_actual_accept_retires_and_stages_after_restart(self):
        artifact = self.accept()
        with self.assertRaises(StoreError): self.store.validate(self.token)
        with self.assertRaises(evidence.EvidenceError): evidence.publish(self.result, self.secret, self.store, self.candidate, self.checks)
        self.store.close(); self.store = Store(self.root/'leases.sqlite', CAP, 'boot-two')
        shutil.rmtree(self.candidate)
        result = self.stage(artifact)
        self.assertTrue(result['qualified']); self.assertEqual(result['accepted_tasks'], ['1.1'])
        self.assertEqual((self.root/'stage/service/value.txt').read_text(), 'new')
    def test_expired_accept_rejected(self):
        self.store.release(self.token)
        with self.assertRaises(StoreError): self.accept()
    def test_changed_patch_blob_rejected(self):
        digest = self.patch['entries'][0]['after']['sha256']
        (self.root/'patch/blobs'/digest).write_text('changed')
        with self.assertRaises(ValueError): self.accept()
        self.assertTrue(self.store.validate(self.token))
    def test_changed_candidate_after_qualification_rejected(self):
        (self.candidate/'service/value.txt').write_text('changed')
        with self.assertRaises(ValueError): self.accept()
    def test_forged_accepted_record_rejected(self):
        artifact = self.accept(); path = Path(artifact['path'])/'accepted.json'
        data = json.loads(path.read_bytes()); data['task'] = '1.2'; path.write_text(json.dumps(data))
        with self.assertRaises(ValueError): self.stage(artifact)
    def test_post_accept_patch_tamper_rejected(self):
        artifact = self.accept(); directory = Path(artifact['path'])
        digest = self.patch['entries'][0]['after']['sha256']
        (directory/'patch/blobs'/digest).write_text('altered')
        with self.assertRaises(ValueError): self.stage(artifact)
    def test_changed_trusted_registry_rejected(self):
        artifact = self.accept()
        with self.assertRaises(ValueError):
            acceptance.stage_accepted(self.base, [artifact['path']], {'1.1':self.owned}, {'1.1':[dict(self.checks[0], timeout=7)]}, self.secret, self.root/'stage', verify=[sys.executable,'check.py'], store=self.store)
    def test_duplicate_artifact_rejected(self):
        artifact = self.accept()
        with self.assertRaises(ValueError):
            acceptance.stage_accepted(self.base, [artifact['path']]*2, {'1.1':self.owned}, {'1.1':self.checks}, self.secret, self.root/'stage', verify=[sys.executable,'check.py'], store=self.store)
    def test_real_stage_verifier_failure_rejected(self):
        artifact = self.accept()
        with self.assertRaises(ValueError):
            acceptance.stage_accepted(self.base, [artifact['path']], {'1.1':self.owned}, {'1.1':self.checks}, self.secret, self.root/'stage', verify=[sys.executable,'-c','raise SystemExit(3)'], store=self.store)
        self.assertFalse((self.root/'stage').exists())

    def test_public_verify_reconstructs_without_merge_claim(self):
        artifact = self.accept()
        verified = acceptance.verify_accepted(self.base, artifact['path'], self.owned, self.checks, self.secret, self.root/'reviewed-candidate', store=self.store)
        self.assertTrue(verified['individually_verified'])
        self.assertFalse(verified['combined_qualified'])
        self.assertEqual(evidence.source_digest(verified['candidate_path']), self.binding['candidate'])
    def test_trusted_index_required(self):
        artifact = self.accept()
        with self.assertRaises(ValueError):
            acceptance.verify_accepted(self.base, artifact['path'], self.owned, self.checks, self.secret, self.root/'untrusted-candidate')
    def test_signed_but_unindexed_artifact_rejected(self):
        artifact = self.accept()
        other = Store(self.root/'other.sqlite', CAP, 'other-boot')
        try:
            with self.assertRaises(ValueError):
                acceptance.verify_accepted(self.base, artifact['path'], self.owned, self.checks, self.secret, self.root/'orphan-candidate', store=other)
        finally:
            other.close()

if __name__ == '__main__': unittest.main()
