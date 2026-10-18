import asyncio
import copy
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import release
from render import RecoveryIdentity
import recovery


def identity():
    return RecoveryIdentity(source_sha256='1'*64, interface_sha256='2'*64, deployment_sha256='3'*64,
                            inventory_sha256='4'*64, environment='qualification', service='recovery',
                            primary_region='region-primary', recovery_region='region-recovery',
                            chain_id=125, addresses={'event-emitter':'0x'+'a'*40}).diagnostic()


def timeline():
    binding = dict(candidate='1'*64, interface='2'*64, topology='5'*64, generator='6'*64)
    values = []
    for instant in (1000,1010):
        queries = {}
        for rid in recovery.METRICS:
            response = dict(status='success', data=dict(resultType='vector', result=[dict(
                metric=dict(service='recovery', environment='qualification', zone='region-recovery'), value=[instant,'1'])]))
            queries[rid] = response
        values.append(dict(version=4,lane='L44',run_id='test-contract',bindings=binding,identity=identity(),kind='metrics',
                           observed_utc_ns=instant*10**9,payload=dict(authority_sha256='7'*64,queries=queries)))
    return values, dict(identity=identity(), run_id='test-contract',bindings=binding,started_ns=1000*10**9,
                        ended_ns=1010*10**9,step_seconds=10,authority_sha256='7'*64)


class ReleaseContracts(unittest.TestCase):
    def test_exact_kubernetes_quantity_math(self):
        self.assertEqual(release.quantity('500m',cpu=True),'0.500')
        self.assertEqual(release.quantity('8Gi'),8589934592)
        self.assertEqual(release.quantity('1.5Gi'),1610612736)
        for value in (None,True,'NaN','-1','1unknown','0','1.1'):
            with self.subTest(value=value),self.assertRaises(ValueError):release.quantity(value)

    def test_exact_live_timeline_shapes_and_labels(self):
        rows, parameters = timeline()
        samples = recovery.release_timeline(rows,**parameters)
        self.assertEqual(set(samples),set(recovery.METRICS))
        self.assertEqual(len(samples['D14_031']),2)
        changed=copy.deepcopy(rows)
        changed[1]['payload']['queries']['D14_031']['data']['result'][0]['metric']['environment']='production'
        with self.assertRaises(ValueError):recovery.release_timeline(changed,**parameters)

    def test_missing_ambiguous_stale_warned_and_nonfinite_series_fail(self):
        rows, parameters = timeline()
        for mode in ('missing','ambiguous','stale','warnings','nan','fractional'):
            changed=copy.deepcopy(rows);response=changed[1]['payload']['queries']['D14_033']
            if mode=='missing':response['data']['result']=[]
            if mode=='ambiguous':response['data']['result']*=2
            if mode=='stale':response['data']['result'][0]['value'][0]=900
            if mode=='warnings':response['warnings']=['partial response']
            if mode=='nan':response['data']['result'][0]['value'][1]='NaN'
            if mode=='fractional':response['data']['result'][0]['value'][1]='1.1'
            with self.subTest(mode=mode),self.assertRaises(ValueError):recovery.release_timeline(changed,**parameters)

    def test_counter_reset_and_absent_faults_cannot_pass_release(self):
        rows, parameters = timeline()
        samples=recovery.release_timeline(rows,**parameters)
        checks=recovery.release_metric_checks(samples,ended_ns=parameters['ended_ns'])
        self.assertFalse(checks['D14_033']['passed'])
        self.assertFalse(checks['D14_036']['passed'])
        self.assertFalse(checks['D14_039']['passed'])
        samples['D14_033'][0]['value']='2';samples['D14_033'][1]['value']='1'
        self.assertFalse(recovery.release_metric_checks(samples,ended_ns=parameters['ended_ns'])['D14_033']['passed'])

    def test_real_filesystem_bound_documents_reject_changes_and_public_permissions(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'admitted.json';raw=b'{"version":4}'
            path.write_bytes(raw);path.chmod(0o600)
            binding=dict(path=str(path),sha256=hashlib.sha256(raw).hexdigest())
            self.assertEqual(release.bound(binding),dict(version=4))
            path.write_bytes(b'{"version":3}')
            with self.assertRaises(ValueError):release.bound(binding)
            path.write_bytes(raw);path.chmod(0o644)
            with self.assertRaises(ValueError):release.bound(binding)

    def test_actual_cli_missing_input_writes_ten_failed_results(self):
        with tempfile.TemporaryDirectory() as directory:
            output=Path(directory)/'result'
            command=[sys.executable,str(Path(release.__file__).resolve()),'--mode','evaluate','--configuration',str(Path(directory)/'missing.json'),
                     '--configuration-sha256','a'*64,'--output-directory',str(output)]
            result=subprocess.run(command,capture_output=True,text=True,timeout=20,check=False)
            self.assertEqual(result.returncode,1,result.stderr)
            report=json.loads((output/'release-result.json').read_bytes())
            self.assertEqual({row['requirement'] for row in report['results']},set(recovery.METRICS))
            self.assertTrue(all(row['outcome']=='fail' for row in report['results']))
            self.assertFalse(report['capacity_qualified'])
            self.assertEqual(report['final_shared_gate'],'9.1')


class ActualReadOnlyProviders(unittest.TestCase):
    def test_actual_allocations_metrics_and_database_snapshot_are_captured(self):
        path=os.environ.get('RECOVERY_PHASE4_COLLECTION_CONFIG')
        digest=os.environ.get('RECOVERY_PHASE4_COLLECTION_CONFIG_SHA256')
        if not path or not digest:raise RuntimeError('admitted isolated read-only recovery measurement providers unavailable')
        config=release.bound(dict(path=path,sha256=digest))
        with tempfile.TemporaryDirectory() as directory:
            journal=release.Journal(Path(directory)/'capture')
            result=asyncio.run(release.Collector(config,journal).collect())
            self.assertEqual(result['outcome'],'pass')
            self.assertEqual(set(result['samples']),set(recovery.METRICS))
            self.assertFalse(result['capacity_qualified'])
            for name,expected in result['artifacts'].items():self.assertEqual(release.file_sha(journal.root/name),expected)


if __name__=='__main__':
    unittest.main(verbosity=2)
