from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import tempfile
import unittest
import uuid

from providers import BoundTools, PostgreSQL, Unavailable, file_sha, identifier, literal, regular, safe_key
from render import RecoveryIdentity, load_operator, render
import recovery

ObservationArchive = load_operator().ObservationArchive
RecoveryOperator = load_operator().RecoveryOperator
RecoveryProposal = load_operator().RecoveryProposal
reconcile_v2 = load_operator().reconcile_v2


ROOT = Path(__file__).resolve().parents[1]


def identity():
    return RecoveryIdentity(source_sha256='1' * 64, interface_sha256='2' * 64,
                            deployment_sha256='3' * 64, inventory_sha256='4' * 64,
                            environment='qualification', service='recovery',
                            primary_region='region-primary', recovery_region='region-recovery',
                            chain_id=125, addresses={'event-emitter': '0x' + 'a' * 40})


class OperatorContracts(unittest.TestCase):
    def test_render_preserves_disabled_resources_and_safe_deployment_diagnostics(self):
        config = json.loads((ROOT / 'config.example.json').read_text())
        resources = json.loads((ROOT / 'resources.json').read_text())
        refs = {name: 'secret://qualification/' + name for name in
                ('backup', 'restore', 'retention', 'application', 'coordinator', 'evidence-signing')}
        result = render(config, resources, identity(), refs)
        self.assertEqual(result['identity']['chain_id'], 125)
        self.assertEqual(result['identity']['addresses'], identity().addresses)
        self.assertEqual(result['identity']['service'], 'recovery')
        self.assertEqual(result['state'], 'proposed')
        self.assertIs(result['traffic_cutover_allowed'], False)
        self.assertIs(result['resources']['resources']['drills']['capacity_qualified'], False)
        self.assertEqual(result['secret_references'], refs)

    def test_wrong_chain_zero_deployment_and_duplicate_credentials_rejected(self):
        original = identity()
        for change in ({'chain_id': 1}, {'deployment_sha256': '0' * 64},
                       {'recovery_region': original.primary_region}, {'addresses': {'event-emitter': '0x' + '0' * 40}}):
            with self.subTest(change=change), self.assertRaises(ValueError):
                RecoveryIdentity(**{**original.__dict__, **change}).validate()
        config = json.loads((ROOT / 'config.example.json').read_text())
        resources = json.loads((ROOT / 'resources.json').read_text())
        refs = {name: 'secret://qualification/shared' for name in
                ('backup', 'restore', 'retention', 'application', 'coordinator', 'evidence-signing')}
        with self.assertRaises(ValueError):
            render(config, resources, original, refs)

    def test_sql_values_do_not_become_sql_identifiers(self):
        self.assertEqual(literal("actor's-principal"), "'actor''s-principal'")
        for value in ('recovery; DROP SCHEMA recovery', 'recovery.other', 'pg_catalog--'):
            with self.assertRaises(ValueError):
                identifier(value)
        with self.assertRaises(ValueError):
            literal('actor\x00admin')

    def test_real_filesystem_credentials_require_private_owned_regular_file(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'credentials'
            path.write_bytes(b'actual-file-bytes')
            path.chmod(0o600)
            self.assertEqual(regular(path, 1024, private=True), path)
            self.assertEqual(file_sha(path), hashlib.sha256(b'actual-file-bytes').hexdigest())
            path.chmod(0o644)
            with self.assertRaises(ValueError):
                regular(path, 1024, private=True)
            link = Path(directory) / 'link'
            link.symlink_to(path)
            with self.assertRaises(ValueError):
                regular(link)

    def test_missing_provider_and_unsafe_archive_keys_fail(self):
        with self.assertRaises(Unavailable):
            BoundTools({})
        for key in ('../other', '/absolute', 'a//b', 'a/./b', 'a/../b', 'a\x00b'):
            with self.assertRaises(ValueError):
                safe_key(key)

    def test_unsigned_observation_cannot_validate_or_claim_capacity(self):
        with tempfile.TemporaryDirectory() as directory:
            key = Path(directory) / 'runner-key'
            key.write_bytes(os.urandom(32))
            key.chmod(0o600)
            observations = ObservationArchive(directory, key, identity())
            path = Path(directory) / 'observation.json'
            path.write_text(json.dumps({'payload': {'version': 2}, 'hmac_sha256': '0' * 64}))
            with self.assertRaisesRegex(ValueError, 'untrusted runner'):
                observations.read(path)

    def test_version2_reconciliation_preserves_exact_signed_realized_amount(self):
        checkpoint = dict(chain_id=125, height=100, block_hash='a' * 64, epoch=3,
                          source_sha256=identity().source_sha256, deployment_sha256=identity().deployment_sha256,
                          interface_sha256=identity().interface_sha256)
        value = dict(version=2, checkpoint=checkpoint, snapshot_id='snapshot-one', counts={'inventories': 1},
                     amounts_raw={'realized-usdl': '-123456789012345678901234567890'},
                     event_identities_sha256='5' * 64)
        self.assertTrue(reconcile_v2(value, json.loads(json.dumps(value)))['reconciled'])
        for amount in ('-0', '+1', '01', '1.0', 1.0, '0'):
            changed = json.loads(json.dumps(value))
            changed['amounts_raw']['realized-usdl'] = amount
            with self.subTest(amount=amount), self.assertRaises(ValueError):
                reconcile_v2(value, changed)


class ActualPostgreSQLOperator(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        filename = os.environ.get('RECOVERY_OPERATOR_QUALIFICATION_CONFIG')
        digest = os.environ.get('RECOVERY_OPERATOR_QUALIFICATION_CONFIG_SHA256')
        if not filename or not digest:
            raise RuntimeError('admitted isolated PostgreSQL recovery configuration unavailable')
        raw = regular(filename, recovery.MAX_JSON, private=True).read_bytes()
        if hashlib.sha256(raw).hexdigest() != digest:
            raise RuntimeError('isolated recovery configuration identity mismatch')
        config = recovery.load(raw)
        if config.get('realm') != 'isolated-recovery-qualification':
            raise RuntimeError('production resources forbidden for native recovery qualification')
        cls.identity = RecoveryIdentity(**config['identity']).validate()
        cls.database = PostgreSQL(BoundTools(config['tools']), config['postgres_service_file'],
                                  config['postgres_service'], config['postgres_role'])
        cls.operator = RecoveryOperator(cls.database, cls.identity, None)

    def setUp(self):
        current = self.database.document("SELECT json_build_object('epoch',epoch,'region',region,'revision',revision,'checkpoint',checkpoint_sha256) FROM recovery.recovery_control WHERE singleton;")
        self.assertEqual(current['region'], self.identity.primary_region)
        self.proposal = RecoveryProposal('drill-' + uuid.uuid4().hex, self.database.role, current['epoch'],
                                         current['revision'] + 1, current['checkpoint'])

    def tearDown(self):
        pid = literal(self.proposal.proposal_id)
        actor = literal(self.database.role)
        self.database.query('BEGIN; DELETE FROM recovery.recovery_audit WHERE proposal_id=' + pid +
                            ' AND actor=' + actor + '; DELETE FROM recovery.recovery_proposals WHERE proposal_id=' +
                            pid + ' AND actor=' + actor + " AND state IN ('proposed','rejected'); COMMIT;")

    def test_actual_proposal_rejection_and_audit_are_durable(self):
        self.operator.propose(self.proposal)
        self.operator.reject(self.proposal)
        row = self.database.document("SELECT json_build_object('state',p.state,'actor',p.actor,'audit_count',"
                                     '(SELECT count(*) FROM recovery.recovery_audit a WHERE a.proposal_id=p.proposal_id)) '
                                     'FROM recovery.recovery_proposals p WHERE p.proposal_id=' + literal(self.proposal.proposal_id))
        self.assertEqual(row, {'state': 'rejected', 'actor': self.database.role, 'audit_count': 2})

    def test_actual_stale_epoch_cannot_create_proposal(self):
        stale = RecoveryProposal(self.proposal.proposal_id, self.database.role, self.proposal.expected_epoch + 1,
                                 self.proposal.revision, self.proposal.checkpoint_sha256)
        from providers import ProviderFailure
        with self.assertRaises(ProviderFailure):
            self.operator.propose(stale)
        self.assertEqual(self.database.query('SELECT count(*) FROM recovery.recovery_proposals WHERE proposal_id=' +
                                            literal(stale.proposal_id)), '0')

    def test_actual_database_rejects_unfenced_activation(self):
        self.operator.propose(self.proposal)
        from providers import ProviderFailure
        with self.assertRaises(ProviderFailure):
            self.database.query('SELECT recovery.activate_fenced_v2(' + literal(self.proposal.proposal_id) + ',' +
                                literal(self.database.role) + ');')
        current = self.database.query('SELECT epoch FROM recovery.current_epoch();')
        self.assertEqual(current, str(self.proposal.expected_epoch))


class AdversarialContracts(unittest.TestCase):
    def test_real_rehearsal_configuration_rejects_production_realm(self):
        from providers import NativeRehearsalRunner
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'configuration.json'
            value = dict(version=3, realm='production', identity=identity().diagnostic(), tools={}, plans={}, output_directory=directory)
            raw = recovery.canonical(value)
            path.write_bytes(raw); path.chmod(0o600)
            with self.assertRaisesRegex(ValueError, 'isolated rehearsal identity'):
                NativeRehearsalRunner(path, hashlib.sha256(raw).hexdigest(), identity())

    def test_real_rehearsal_configuration_cannot_change_after_admission(self):
        from providers import NativeRehearsalRunner
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'configuration.json'
            path.write_bytes(b'{}'); path.chmod(0o600)
            with self.assertRaisesRegex(ValueError, 'authority changed'):
                NativeRehearsalRunner(path, 'a'*64, identity())

    def test_unsigned_native_scenarios_cannot_change_authority(self):
        with tempfile.TemporaryDirectory() as directory:
            key = Path(directory) / 'independent-key'
            key.write_bytes(os.urandom(32)); key.chmod(0o600)
            import sys
            executable = Path(sys.executable).resolve()
            archive = load_operator().AdversarialArchive(directory, key, identity(),
                                                         native_tools={'python': {'path': str(executable), 'sha256': file_sha(executable)}})
            path = Path(directory) / 'native.json'
            path.write_bytes(recovery.canonical({'payload': {'version': 3}, 'hmac_sha256': '0'*64}))
            path.chmod(0o600)
            with self.assertRaisesRegex(ValueError, 'untrusted adversarial'):
                archive.read_scenarios(path, {})


class ActualBackupAuthorization(unittest.TestCase):
    def test_real_application_backup_delete_and_other_environment_decrypt_denied(self):
        import hmac
        import time
        from providers import BackupAccessRehearsal, S3Archive
        filename = os.environ.get('RECOVERY_ADVERSARIAL_PROVIDER_CONFIG')
        digest = os.environ.get('RECOVERY_ADVERSARIAL_PROVIDER_CONFIG_SHA256')
        if not filename or not digest:
            raise RuntimeError('admitted isolated backup IAM/KMS rehearsal authority unavailable')
        raw = regular(filename, recovery.MAX_JSON, private=True).read_bytes()
        if hashlib.sha256(raw).hexdigest() != digest:
            raise RuntimeError('admitted backup IAM/KMS configuration changed')
        config = recovery.load(raw)
        recovery.exact(config, {'version', 'realm', 'identity', 'tools', 'application_archive', 'authoritative_archive',
                                'principal_arn', 'object', 'ciphertext', 'other_key_arn', 'audit_file', 'audit_key'})
        self.assertEqual(config['version'], 3)
        self.assertEqual(config['realm'], 'isolated-recovery-qualification')
        admitted_identity = RecoveryIdentity(**config['identity']).validate()
        tools = BoundTools(config['tools'])
        runner = BackupAccessRehearsal(S3Archive(tools, **config['application_archive']),
                                     S3Archive(tools, **config['authoritative_archive']), principal_arn=config['principal_arn'])
        started = int(time.time())
        observed = runner.attempt(config['object'], config['ciphertext'], config['other_key_arn'])
        ended = int(time.time())
        self.assertEqual(observed['delete_error'], 'AccessDenied')
        self.assertEqual(observed['decrypt_error'], 'AccessDeniedException')
        self.assertEqual(observed['plaintext_bytes'], 0)
        signed = recovery.load(regular(config['audit_file'], recovery.MAX_JSON, private=True).read_bytes())
        recovery.exact(signed, {'payload', 'hmac_sha256'})
        key = regular(config['audit_key'], 4096, private=True).read_bytes()
        self.assertGreaterEqual(len(key), 32)
        signature = hmac.new(key, recovery.canonical(signed['payload']), hashlib.sha256).hexdigest()
        self.assertTrue(hmac.compare_digest(signature, signed['hmac_sha256']))
        audit = signed['payload']
        recovery.exact(audit, {'version', 'realm', 'identity', 'events'})
        self.assertEqual(audit['version'], 3)
        self.assertEqual(audit['realm'], config['realm'])
        self.assertEqual(audit['identity'], admitted_identity.diagnostic())
        self.assertEqual(len(audit['events']), 2)
        by_action = {}
        for event in audit['events']:
            recovery.exact(event, {'operation', 'principal_arn', 'error', 'request_id', 'unix', 'resource'})
            self.assertNotIn(event['operation'], by_action)
            self.assertEqual(event['principal_arn'], observed['principal_arn'])
            recovery.integer(event['unix'], started, ended)
            self.assertRegex(event['request_id'], r'^[A-Za-z0-9-]{8,128}$')
            by_action[event['operation']] = event
        self.assertEqual(set(by_action), {'DeleteObject', 'Decrypt'})
        self.assertEqual(by_action['DeleteObject']['error'], 'AccessDenied')
        self.assertEqual(by_action['DeleteObject']['resource'], {'bucket': runner.authority.bucket,
                                                               'key': config['object']['key'], 'version_id': config['object']['version_id']})
        self.assertEqual(by_action['Decrypt']['error'], 'AccessDeniedException')
        self.assertEqual(by_action['Decrypt']['resource'], {'kms_key_arn': config['other_key_arn']})


if __name__ == '__main__':
    unittest.main(verbosity=2)
