from __future__ import annotations
import copy
import hashlib
import json
from pathlib import Path
import sys
import tempfile
import unittest

TOPOLOGY = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(TOPOLOGY))
from phase2 import v1
from phase2.evidence import artifact, failing_report, validate_inventory, validate_metric, validate_adversary, ADVERSARIES


class StorageEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.config = v1.load(TOPOLOGY / 'fixtures/synthetic-config.json')
        self.plan = v1.compile_plan(self.config)
        self.plan['runtimeOwners'] = dict(v1.RUNTIME_ROLES)
        self.inventory = {'schemaVersion': 2,
                          'zones': {zone: 'provider-domain-' + str(i) for i, zone in enumerate(self.config['zones'])},
                          'postgres': [{'id': 'pg-' + str(i), 'zone': zone,
                                        'allocatedBytes': self.config['storage']['postgresBytesPerMember']}
                                       for i, zone in enumerate(self.config['zones'])],
                          'nats': [{'id': 'broker-' + str(i), 'zone': zone,
                                    'allocatedBytes': self.config['storage']['natsBytesPerMember'],
                                    'configuredStoreBytes': self.config['storage']['natsBytesPerMember'] * 9 // 10}
                                   for i, zone in enumerate(self.config['zones'])],
                          'runtimeOwners': dict(v1.RUNTIME_ROLES)}

    def test_normal_and_qualification_ledgers_remain_separate(self):
        ledgers = self.plan['capacity']['broker']
        self.assertEqual(ledgers['normal']['rawLogicalBytes'], 36000000000)
        self.assertEqual(ledgers['normal']['rawR3Bytes'], 108000000000)
        self.assertEqual(ledgers['qualification']['rawLogicalBytes'], 622080000000)
        self.assertEqual(ledgers['qualification']['rawR3Bytes'], 1866240000000)
        self.assertEqual(ledgers['qualification']['rawR3HeadroomBytes'], 3732480000000)
        self.assertGreater(ledgers['qualification']['requiredBytesWithTwofoldHeadroom'], 3732480000000)
        self.assertEqual(self.plan['capacity']['postgres']['thirtyDayRawBytes'], 555360000000)
        self.assertEqual(self.plan['capacity']['postgres']['rowIndexAmplifiedBytesPerMember'], 1666080000000)
        validate_inventory(self.inventory, self.plan, self.config)

    def test_one_undersized_postgres_member_rejects_even_when_total_capacity_fits(self):
        inventory = copy.deepcopy(self.inventory)
        inventory['postgres'][0]['allocatedBytes'] = 1666080000000
        with self.assertRaisesRegex(v1.Invalid, 'PostgreSQL allocation requires expansion'):
            validate_inventory(inventory, self.plan, self.config)

    def test_normal_retention_allocation_cannot_satisfy_qualification(self):
        inventory = copy.deepcopy(self.inventory)
        for member in inventory['nats']:
            member['configuredStoreBytes'] = self.plan['capacity']['broker']['normal']['requiredBytesWithTwofoldHeadroom'] // 3 + 1
        with self.assertRaisesRegex(v1.Invalid, 'qualification broker storage requires expansion'):
            validate_inventory(inventory, self.plan, self.config)

    def test_repeated_provider_failure_domain_rejected(self):
        inventory = copy.deepcopy(self.inventory)
        for zone in inventory['zones']:
            inventory['zones'][zone] = 'same-provider-domain'
        with self.assertRaisesRegex(v1.Invalid, 'not independent'):
            validate_inventory(inventory, self.plan, self.config)

    def test_broker_cannot_charge_unusable_full_volume(self):
        inventory = copy.deepcopy(self.inventory)
        inventory['nats'][0]['configuredStoreBytes'] = inventory['nats'][0]['allocatedBytes']
        with self.assertRaisesRegex(v1.Invalid, 'broker store limit'):
            validate_inventory(inventory, self.plan, self.config)

    def test_runtime_service_owner_drift_rejected(self):
        inventory = copy.deepcopy(self.inventory)
        inventory['runtimeOwners']['api'] = 'pnld'
        with self.assertRaisesRegex(v1.Invalid, 'ownership drift'):
            validate_inventory(inventory, self.plan, self.config)


class ArtifactEvidenceTests(unittest.TestCase):
    def test_byte_identity_does_not_accept_equivalent_json(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'input.json'
            path.write_bytes(b'{"a":1}\n')
            reference = {'path': str(path), 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}
            self.assertEqual(artifact(reference), {'a': 1})
            path.write_bytes(b'{ "a": 1 }\n')
            with self.assertRaisesRegex(v1.Invalid, 'byte identity mismatch'):
                artifact(reference)

    def test_missing_evidence_reports_all_eighteen_failed(self):
        report = failing_report('measured provider evidence missing')
        self.assertFalse(report['passed'])
        self.assertEqual(set(report['requirements']), set(v1.REQS))
        self.assertTrue(all(result['passed'] is False for result in report['requirements'].values()))

    def test_metric_samples_bind_interval_and_bounded_dimensions(self):
        config = v1.load(TOPOLOGY / 'fixtures/synthetic-config.json')
        scenario = {'metric': 'postgres_server_connections', 'sampleCount': 2,
                    'startedAt': '2026-01-01T00:00:00Z', 'finishedAt': '2026-01-01T00:00:01Z'}
        samples = [{'timestamp': stamp, 'value': 400, 'service': 'postgres',
                    'environment': config['environment'], 'zone': config['zones'][0]}
                   for stamp in (scenario['startedAt'], scenario['finishedAt'])]
        metric = {'metric': scenario['metric'], 'samples': samples}
        validate_metric(metric, scenario, config)
        changed = copy.deepcopy(metric)
        changed['samples'][1]['timestamp'] = '2026-01-01T00:01:00Z'
        with self.assertRaisesRegex(v1.Invalid, 'outside scenario'):
            validate_metric(changed, scenario, config)
        changed = copy.deepcopy(metric)
        changed['samples'][0]['zone'] = 'unreviewed-zone'
        with self.assertRaisesRegex(v1.Invalid, 'bounded deployment inventory'):
            validate_metric(changed, scenario, config)

    def test_nonfinite_or_boolean_metric_value_rejected(self):
        config = v1.load(TOPOLOGY / 'fixtures/synthetic-config.json')
        scenario = {'metric': 'postgres_server_connections', 'sampleCount': 2,
                    'startedAt': '2026-01-01T00:00:00Z', 'finishedAt': '2026-01-01T00:00:01Z'}
        for value in (True, float('nan'), float('inf')):
            metric = {'metric': scenario['metric'], 'samples': [
                {'timestamp': scenario['startedAt'], 'value': value, 'service': 'postgres',
                 'environment': config['environment'], 'zone': config['zones'][0]},
                {'timestamp': scenario['finishedAt'], 'value': 400, 'service': 'postgres',
                 'environment': config['environment'], 'zone': config['zones'][0]}]}
            with self.assertRaisesRegex(v1.Invalid, 'finite numeric'):
                validate_metric(metric, scenario, config)


class AdversarialEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.config=v1.load(TOPOLOGY/'fixtures/synthetic-config.json')
        self.plan=v1.compile_plan(self.config)
        self.scenario={'requirement':'D14_001','startedAt':'2026-01-01T00:00:00Z',
                       'finishedAt':'2026-01-01T00:01:00Z'}
        self.inputs={'schemaVersion':3,'requirement':'D14_001','action':'zone_application_loss',
                     'environment':'qualification','context':'isolated-context','namespace':self.config['namespace'],'namespaceUID':'namespace-uid-one',
                     'configurationDigest':self.plan['configurationDigest'],'deploymentDigest':self.plan['deploymentDigest'],
                     'faultStartedAt':'2026-01-01T00:00:10Z','faultFinishedAt':'2026-01-01T00:00:30Z',
                     'parameters':{'removedZone':self.config['zones'][0],'dynamicRps':10000}}
        self.observed={'schemaVersion':3,'requirement':'D14_001','configurationDigest':self.plan['configurationDigest'],
                       'deploymentDigest':self.plan['deploymentDigest'],'startedAt':self.scenario['startedAt'],
                       'finishedAt':self.scenario['finishedAt'],'observations':{'unavailableZone':self.config['zones'][0],
                        'admittedRequests':10000,'failedAdmittedRequests':0,'activePostgresWriters':1,
                        'survivingZones':self.config['zones'][1:]}}
        self.reconciliation={'schemaVersion':3,'requirement':'D14_001','beforeDigest':'a'*64,'afterDigest':'b'*64,
                             'acknowledgedWriteIds':['write-one'],'receiptIds':['write-one'],'effectCounts':{'write-one':1},
                             'lostAcknowledgedWrites':0,'duplicateDatabaseEffects':0,'activePostgresWriters':1,
                             'restored':True,'recoveredAt':'2026-01-01T00:00:50Z','timeline':[
                              {'at':'2026-01-01T00:00:00Z','phase':'before','stateDigest':'a'*64},
                              {'at':'2026-01-01T00:00:10Z','phase':'fault','stateDigest':'c'*64},
                              {'at':'2026-01-01T00:00:30Z','phase':'recovering','stateDigest':'d'*64},
                              {'at':'2026-01-01T00:00:50Z','phase':'recovered','stateDigest':'b'*64}]}

    def check(self):
        return validate_adversary(self.inputs,self.observed,self.reconciliation,self.scenario,self.plan,self.config)

    def test_zone_contract_checks_real_validator_data_path(self):
        self.assertTrue(self.check()['passed'])
        self.observed['observations']['activePostgresWriters']=2
        with self.assertRaisesRegex(v1.Invalid,'activePostgresWriters'):
            self.check()

    def test_all_eighteen_actions_have_explicit_outcome_checks(self):
        self.assertEqual(set(ADVERSARIES),set(v1.REQS))
        for requirement in v1.REQS:
            with self.subTest(requirement=requirement):
                inputs=copy.deepcopy(self.inputs);inputs['requirement']=requirement;inputs['action']=ADVERSARIES[requirement]
                observed=copy.deepcopy(self.observed);observed['requirement']=requirement;observed['observations']={}
                reconciliation=copy.deepcopy(self.reconciliation);reconciliation['requirement']=requirement
                scenario=dict(self.scenario,requirement=requirement)
                with self.assertRaises(v1.Invalid):
                    validate_adversary(inputs,observed,reconciliation,scenario,self.plan,self.config)

    def test_receipt_loss_duplicate_effect_and_failed_restore_reject(self):
        changes=[('receiptIds',[]),('effectCounts',{'write-one':2}),('restored',False),
                 ('activePostgresWriters',2),('lostAcknowledgedWrites',1),('duplicateDatabaseEffects',1)]
        for field,value in changes:
            with self.subTest(field=field):
                original=copy.deepcopy(self.reconciliation)
                self.reconciliation[field]=value
                with self.assertRaises(v1.Invalid):self.check()
                self.reconciliation=original

    def test_fault_and_recovery_timelines_bind_state_and_order(self):
        for change in (lambda r:r['timeline'].pop(2),
                       lambda r:r['timeline'][-1].update(stateDigest='c'*64),
                       lambda r:r.update(recoveredAt='2026-01-01T00:00:20Z'),
                       lambda r:r['timeline'][1].update(at='2026-01-01T00:00:05Z')):
            original=copy.deepcopy(self.reconciliation);change(self.reconciliation)
            with self.assertRaises(v1.Invalid):self.check()
            self.reconciliation=original

    def test_changed_deployment_and_production_fault_inputs_reject(self):
        self.inputs['environment']='production'
        with self.assertRaisesRegex(v1.Invalid,'isolated'):self.check()
        self.inputs['environment']='qualification';self.observed['deploymentDigest']='c'*64
        with self.assertRaisesRegex(v1.Invalid,'deployment identity'):self.check()

    def test_normal_ledger_cannot_replace_replay_and_headroom_failure_rejects(self):
        self.scenario['requirement']='D14_009';self.inputs['requirement']='D14_009';self.inputs['action']='retention_ledgers'
        self.observed['requirement']='D14_009';self.reconciliation['requirement']='D14_009'
        self.inputs['parameters']={'rawEnvelopeBytes':1200,'retentionSeconds':259200,
                                   'normalEventsPerDay':10000000,'qualificationEventsPerSecond':2000}
        self.observed['observations']={'rawEnvelopeBytes':1200,'retentionSeconds':259200,
            'normal':self.plan['capacity']['broker']['normal'],'qualification':self.plan['capacity']['broker']['normal']}
        with self.assertRaisesRegex(v1.Invalid,'qualification'):self.check()
        values=self.observed['observations'];values['qualification']=self.plan['capacity']['broker']['qualification']
        values.update(measuredBrokerOverheadPermille=100,measuredStreams=self.config['streams'],
                      postgresThirtyDayRawBytes=555360000000,postgresAmplifiedBytesPerMember=1666080000000,
                      derivedBytesPerMember=200*1024**3,walBytesPerMember=128*1024**3,vacuumBytesPerMember=128*1024**3,
                      postgresMembers=[{'id':'pg-'+str(i),'allocatedBytes':self.config['storage']['postgresBytesPerMember'],
                                        'freeBytes':self.config['storage']['postgresBytesPerMember']//2} for i in range(3)])
        self.assertTrue(self.check()['passed'])
        values['postgresMembers'][0]['freeBytes']=1
        with self.assertRaisesRegex(v1.Invalid,'free headroom'):self.check()


if __name__ == '__main__':
    unittest.main()
