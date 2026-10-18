from __future__ import annotations
import base64
import copy
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

TOPOLOGY = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(TOPOLOGY))
from phase2 import v1
from phase2.attestations import SignatureVerifier, regular_bytes
from phase2.operator import Journal
from phase2.providers import safe_diagnostics


class JournalTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.path = Path(self.directory.name) / 'operator.db'
        self.journal = Journal(self.path)
        self.addCleanup(self.journal.close)
        self.config = v1.load(TOPOLOGY / 'fixtures/synthetic-config.json')
        self.plan = v1.compile_plan(self.config)

    def test_proposal_and_rejection_survive_new_connection(self):
        proposed = self.journal.propose(self.config, self.plan, 'operator-one')
        self.assertEqual(proposed['state'], 'proposed')
        self.journal.reject(self.config['revision'], 'operator-two', 'measured capacity missing')
        other = Journal(self.path)
        try:
            self.assertEqual(other.proposal(self.config['revision'])['state'], 'rejected')
            audit = other.connection.execute('SELECT state FROM topology_audit ORDER BY sequence').fetchall()
            self.assertEqual([row[0] for row in audit], ['proposed', 'rejected'])
        finally:
            other.close()

    def test_rejected_revision_cannot_be_reused(self):
        self.journal.propose(self.config, self.plan, 'operator-one')
        self.journal.reject(self.config['revision'], 'operator-two', 'provider unavailable')
        with self.assertRaisesRegex(v1.Invalid, 'monotonically'):
            self.journal.propose(self.config, self.plan, 'operator-one')
        self.assertEqual(self.journal.connection.execute('SELECT COUNT(*) FROM topology_proposals').fetchone()[0], 1)

    def test_proposed_candidate_cannot_begin_activation(self):
        self.journal.propose(self.config, self.plan, 'operator-one')
        with self.assertRaisesRegex(v1.Invalid, 'validated'):
            self.journal.begin_activation(self.config['revision'], 0, self.plan, 'a' * 64, 'b' * 64, 'operator-one')
        self.assertEqual(self.journal.connection.execute('SELECT COUNT(*) FROM topology_intent').fetchone()[0], 0)
        self.assertEqual(self.journal.connection.execute('SELECT revision FROM topology_active').fetchone()[0], 0)

    def test_failed_transaction_rolls_back_audit_and_state(self):
        with self.assertRaisesRegex(RuntimeError, 'interruption'):
            with self.journal.transaction() as db:
                self.journal.audit(db, 1, 'operator-one', None, 'proposed', {'revision': 1})
                raise RuntimeError('interruption')
        self.assertEqual(self.journal.connection.execute('SELECT COUNT(*) FROM topology_audit').fetchone()[0], 0)

    def test_journal_symlink_rejected(self):
        link = Path(self.directory.name) / 'linked.db'
        link.symlink_to(self.path)
        with self.assertRaisesRegex(v1.Invalid, 'symlink'):
            Journal(link)


class ActivationInterruptionTests(unittest.TestCase):
    setUp = JournalTests.setUp
    def validated(self):
        self.journal.propose(self.config,self.plan,'operator-one')
        self.journal.validate(self.config['revision'],self.plan,
                              {'passed':True,'evidenceDigest':'a'*64},
                              {'accepted':True,'deploymentDigest':self.plan['deploymentDigest']},'operator-one')

    def test_interrupted_intent_survives_restart_and_denies_changed_actor(self):
        self.validated()
        revision=self.config['revision']
        self.journal.begin_activation(revision,0,self.plan,'a'*64,'b'*64,'operator-one')
        reopened=Journal(self.path)
        try:
            with self.assertRaisesRegex(v1.Invalid,'unreconciled'):
                reopened.begin_activation(revision,0,self.plan,'a'*64,'b'*64,'operator-two')
            with self.assertRaisesRegex(v1.Invalid,'explicit resume'):
                reopened.begin_activation(revision,0,self.plan,'a'*64,'c'*64,'operator-one')
            self.assertEqual(reopened.connection.execute('SELECT authorization_digest FROM topology_intent').fetchone()[0],'b'*64)
            reopened.begin_activation(revision,0,self.plan,'a'*64,'c'*64,'operator-one',resume=True)
            self.assertEqual(reopened.connection.execute('SELECT authorization_digest FROM topology_intent').fetchone()[0],'c'*64)
            self.assertEqual(reopened.connection.execute('SELECT revision FROM topology_active').fetchone()[0],0)
        finally:
            reopened.close()

    def test_missing_evidence_and_stale_revision_never_create_intent(self):
        self.validated()
        for expected,evidence in ((1,'a'*64),(0,'d'*64)):
            with self.assertRaises(v1.Invalid):
                self.journal.begin_activation(self.config['revision'],expected,self.plan,evidence,'b'*64,'operator-one')
        self.assertEqual(self.journal.connection.execute('SELECT COUNT(*) FROM topology_intent').fetchone()[0],0)
        self.assertEqual(self.journal.connection.execute('SELECT revision FROM topology_active').fetchone()[0],0)

    def test_partial_activation_cannot_be_rejected_or_finalize_twice(self):
        self.validated()
        revision=self.config['revision']
        self.journal.begin_activation(revision,0,self.plan,'a'*64,'b'*64,'operator-one')
        with self.assertRaisesRegex(v1.Invalid,'partially applied'):
            self.journal.reject(revision,'operator-one','cancel')
        receipt={'applied':True,'deploymentDigest':self.plan['deploymentDigest']}
        self.journal.activated(revision,receipt,'operator-one')
        with self.assertRaisesRegex(v1.Invalid,'intent missing'):
            self.journal.activated(revision,receipt,'operator-one')
        self.assertEqual(self.journal.connection.execute('SELECT revision FROM topology_active').fetchone()[0],revision)
        self.assertEqual(self.journal.connection.execute("SELECT COUNT(*) FROM topology_audit WHERE state='activated'").fetchone()[0],1)

    def test_unrecovered_fault_blocks_activation(self):
        self.validated()
        self.journal.connection.execute('INSERT INTO topology_rehearsal VALUES(?,?,?,?,?,?,?)',
            ('zone-loss','in_progress','{}','{}',self.plan['deploymentDigest'],'operator-one',None))
        with self.assertRaisesRegex(v1.Invalid,'interrupted rehearsal'):
            self.journal.begin_activation(self.config['revision'],0,self.plan,'a'*64,'b'*64,'operator-one')
        self.assertEqual(self.journal.connection.execute('SELECT COUNT(*) FROM topology_intent').fetchone()[0],0)


class SignatureTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        executable = shutil.which('openssl')
        self.assertIsNotNone(executable, 'real OpenSSL is required')
        self.executable = str(Path(executable).resolve())
        self.private = self.root / 'signer.pem'
        self.public = self.root / 'signer.pub.pem'
        subprocess.run([self.executable, 'genpkey', '-algorithm', 'ED25519', '-out', str(self.private)], check=True,
                       stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        subprocess.run([self.executable, 'pkey', '-in', str(self.private), '-pubout', '-out', str(self.public)],
                       check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.verifier = SignatureVerifier(self.executable, hashlib.sha256(Path(self.executable).read_bytes()).hexdigest(),
                                         {'operator-one': {'path': str(self.public),
                                          'sha256': hashlib.sha256(self.public.read_bytes()).hexdigest(),
                                          'purposes': ['qualification']}})

    def signed(self, payload):
        message = self.root / 'payload.json'
        signature = self.root / 'signature.bin'
        message.write_bytes(v1.canonical(payload))
        subprocess.run([self.executable, 'pkeyutl', '-sign', '-inkey', str(self.private), '-rawin',
                        '-in', str(message), '-out', str(signature)], check=True,
                       stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        return {'schemaVersion': 2, 'keyId': 'operator-one', 'algorithm': 'Ed25519',
                'signature': base64.b64encode(signature.read_bytes()).decode()}

    def test_actual_signature_binds_complete_payload(self):
        payload = {'action': 'activate', 'revision': 7, 'deploymentDigest': 'a' * 64}
        signature = self.signed(payload)
        receipt = self.verifier.verify(payload, signature)
        self.assertEqual(receipt['payloadDigest'], v1.digest(payload))
        altered = copy.deepcopy(payload)
        altered['revision'] += 1
        with self.assertRaisesRegex(v1.Invalid, 'signature verification failed'):
            self.verifier.verify(altered, signature)

    def test_untrusted_identity_rejected_before_native_verification(self):
        payload = {'revision': 1}
        signature = self.signed(payload)
        signature['keyId'] = 'untrusted'
        with self.assertRaisesRegex(v1.Invalid, 'untrusted'):
            self.verifier.verify(payload, signature)

    def test_qualification_signer_cannot_authorize_activation(self):
        payload = {'action': 'activate', 'revision': 1}
        signature = self.signed(payload)
        with self.assertRaisesRegex(v1.Invalid, 'lacks required authority'):
            self.verifier.verify(payload, signature, purpose='activation')

    def test_changed_trusted_key_bytes_rejected(self):
        payload = {'revision': 1}
        signature = self.signed(payload)
        self.public.write_bytes(self.public.read_bytes() + b'\n')
        with self.assertRaisesRegex(v1.Invalid, 'public key changed'):
            self.verifier.verify(payload, signature)

    def test_artifact_size_and_symlink_limits(self):
        path = self.root / 'large.json'
        path.write_bytes(b'12345')
        with self.assertRaisesRegex(v1.Invalid, 'byte limit'):
            regular_bytes(path, 4)
        link = self.root / 'link.json'
        link.symlink_to(path)
        with self.assertRaisesRegex(v1.Invalid, 'regular artifact'):
            regular_bytes(link)


if __name__ == '__main__':
    unittest.main()
