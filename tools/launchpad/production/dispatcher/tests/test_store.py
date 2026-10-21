import json
import hashlib
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch
import socket

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from store import Store, StoreError, CLASSES

CAP = dict(cpu=8, memory=8192, pids=100, disk=10000, executor=30, go_build=2, container_start=1)
BIND = dict.fromkeys(('baseline', 'candidate', 'interface', 'layout'), 'a'*64)
RES = dict(cpu=1, memory=100, pids=1, disk=10, executor=1, go_build=0, container_start=0)


class StoreTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.path = Path(self.tmp.name) / 'leases.sqlite'
        self.store = Store(self.path, CAP, 'boot-test')

    def tearDown(self):
        self.store.close()
        self.tmp.cleanup()

    def claim(self, **kw):
        return self.store.claim('task', 'L01', 'owner', BIND, RES, **kw)

    def test_claim_heartbeat_complete(self):
        token = self.claim()
        updated = self.store.heartbeat(token)
        self.assertGreaterEqual(updated['expires'], token['expires'])
        self.store.complete(updated)
        self.assertEqual(self.store.active(), [])
        for action in (self.store.validate, self.store.heartbeat, self.store.release, self.store.complete):
            with self.assertRaises(StoreError):
                action(token)

    def test_expiry_takeover_monotonic_fence(self):
        token = self.claim(ttl=.05)
        time.sleep(.08)
        replacement = self.claim()
        self.assertEqual(replacement['fence'], token['fence'] + 1)
        self.assertNotEqual(replacement['attempt'], token['attempt'])
        for action in (self.store.validate, self.store.heartbeat, self.store.release, self.store.complete):
            with self.assertRaises(StoreError):
                action(token)

    def test_restart_invalidates_active_and_old_store(self):
        old = self.claim()
        restarted = Store(self.path, CAP, 'boot-new')
        try:
            new = restarted.claim('task', 'L01', 'owner', BIND, RES)
            self.assertGreater(new['fence'], old['fence'])
            with self.assertRaises(StoreError):
                restarted.validate(old)
            with self.assertRaises(StoreError):
                self.store.claim('different', 'L02', 'owner', BIND, RES)
        finally:
            restarted.close()

    def test_all_resource_caps(self):
        for resource in CLASSES:
            with self.subTest(resource=resource):
                request = dict(RES)
                request[resource] = CAP[resource]+1
                with self.assertRaises(StoreError):
                    self.store.claim('task', 'L01', 'owner', BIND, request)
        token = self.store.claim('one', 'L01', 'owner', BIND, dict(RES, go_build=2))
        with self.assertRaises(StoreError):
            self.store.claim('two', 'L02', 'owner', BIND, dict(RES, go_build=1))
        self.store.release(token)
        self.store.claim('two', 'L02', 'owner', BIND, dict(RES, go_build=1))

    def test_binding_and_token_tampering(self):
        for binding in ({}, dict(BIND, baseline='bad'), dict(BIND, extra='b'*64)):
            with self.assertRaises(StoreError):
                self.store.claim('task', 'L01', 'owner', binding, RES)
        token = self.claim()
        for key in ('task', 'lane', 'owner', 'attempt', 'fence', 'boot', 'bindings', 'resources', 'allocation'):
            altered = dict(token)
            altered[key] = 'altered'
            with self.assertRaises(StoreError):
                self.store.validate(altered)

    def test_invalid_lane_rejected(self):
        for lane in ('L00', 'L51', 'lane', 'L1', '../L01'):
            with self.assertRaises(StoreError):
                self.store.claim('task', lane, 'owner', BIND, RES)

    def test_candidate_rebind_invalidates_prior_tokens(self):
        token = self.claim()
        rebound = self.store.bind_candidate(token, 'b'*64)
        self.assertGreater(rebound['fence'], token['fence'])
        self.assertEqual(rebound['attempt'], token['attempt'])
        self.assertEqual(rebound['allocation'], token['allocation'])
        self.assertEqual(rebound['resources'], token['resources'])
        self.assertEqual(rebound['bindings'], dict(BIND, candidate='b'*64))
        self.assertEqual(rebound['expires'], token['expires'])
        for action in (self.store.validate, self.store.heartbeat, self.store.release, self.store.complete):
            with self.assertRaises(StoreError):
                action(token)
        with self.assertRaises(StoreError):
            self.store.bind_candidate(token, 'c'*64)
        self.store.validate(rebound)
        for digest in ('bad', '', 'A'*64, None):
            with self.assertRaises(StoreError):
                self.store.bind_candidate(rebound, digest)
        self.store.validate(rebound)

    def test_boolean_fence_rejected(self):
        token = self.claim()
        with self.assertRaises(StoreError):
            self.store.validate(dict(token, fence=True))

    def test_invalid_ttl_and_resources(self):
        for ttl in (0, -1, float('nan'), float('inf'), 3601, True):
            with self.assertRaises(StoreError):
                self.claim(ttl=ttl)
        for resources in (dict(RES, cpu=float('nan')), dict(RES, pids=.5), dict(RES, executor=0), dict(RES, unknown=1)):
            with self.assertRaises(StoreError):
                self.store.claim('task', 'L01', 'owner', BIND, resources)

    def test_subprocess_claim_race_same_task_and_lane(self):
        script = '''import json,sys,time
from store import Store,StoreError
s=Store(sys.argv[1],json.loads(sys.argv[2]),'boot-test')
while not __import__('os').path.exists(sys.argv[3]): time.sleep(.005)
try:
 t=s.claim('task','L01',sys.argv[4],json.loads(sys.argv[5]),json.loads(sys.argv[6]))
 print('won',flush=True)
except StoreError: print('lost',flush=True)
finally:s.close()
'''
        gate = str(Path(self.tmp.name)/'gate')
        env = dict(os.environ, PYTHONPATH=str(Path(__file__).resolve().parents[1]))
        children = [subprocess.Popen([sys.executable, '-c', script, str(self.path), json.dumps(CAP), gate, str(i), json.dumps(BIND), json.dumps(RES)], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env) for i in range(12)]
        Path(gate).touch()
        outputs = []
        for process in children:
            out, err = process.communicate(timeout=30)
            self.assertEqual(process.returncode, 0, err)
            outputs.append(out.strip())
        self.assertEqual(outputs.count('won'), 1, outputs)
        self.assertEqual(outputs.count('lost'), 11, outputs)
        self.assertEqual(len(self.store.active()), 1)

    def test_ai_capacity_and_explicit_process_harness(self):
        with self.assertRaises(StoreError):
            Store(Path(self.tmp.name)/'ai.db', dict(CAP, executor=31))
        harness = Store(Path(self.tmp.name)/'process.db', dict(CAP, executor=50), mode='process-harness')
        harness.close()

    def test_monotonic_expiry_ignores_wallclock(self):
        token = self.claim(ttl=.04)
        with patch('time.time', return_value=-1000000):
            time.sleep(.06)
            with self.assertRaises(StoreError):
                self.store.heartbeat(token)
            replacement = self.claim()
            self.assertGreater(replacement['fence'], token['fence'])

    def test_ports_unique_and_occupied_port_skipped(self):
        with socket.socket() as listener:
            occupied_port = None
            for candidate_port in range(20100, 20200):
                try:
                    listener.bind(('127.0.0.1', candidate_port))
                except OSError:
                    continue
                occupied_port = candidate_port
                break
            self.assertIsNotNone(occupied_port, 'no free canonical L01 port available for real socket proof')
            first = self.claim()
            second = self.store.claim('two', 'L02', 'owner', BIND, RES)
            self.assertNotIn(occupied_port, first['allocation']['ports'])
            self.assertTrue(all(20100 <= p <= 20199 for p in first['allocation']['ports']))
            self.assertTrue(all(20200 <= p <= 20299 for p in second['allocation']['ports']))
            self.assertEqual(first['allocation']['migration_range'], [10000, 19999])
            self.assertEqual(second['allocation']['migration_range'], [20000, 29999])
            self.assertFalse(set(first['allocation']['ports']) & set(second['allocation']['ports']))
            self.assertNotEqual(first['allocation']['service_namespace'], second['allocation']['service_namespace'])

    def test_os_restart_invalidates_same_coordinator_boot(self):
        token = self.claim()
        restarted = Store(self.path, CAP, 'boot-test', host_boot_id='simulated-new-os-boot')
        try:
            self.assertEqual(restarted.active(), [])
            with self.assertRaises(StoreError):
                restarted.validate(token)
            replacement = restarted.claim('task', 'L01', 'owner', BIND, RES)
            self.assertGreater(replacement['fence'], token['fence'])
            with self.assertRaises(StoreError):
                self.store.validate(token)
            with self.assertRaises(StoreError):
                self.store.claim('other', 'L02', 'owner', BIND, RES)
        finally:
            restarted.close()

    def test_accept_durable_index_and_failure_preserves_lease(self):
        token = self.claim()
        artifact = Path(self.tmp.name)/'accepted.json'
        artifact.write_bytes(b'{"passed":true}')
        digest = hashlib.sha256(artifact.read_bytes()).hexdigest()
        with self.assertRaises(StoreError):
            self.store.accept(token, artifact, 'b'*64)
        self.store.validate(token)
        self.assertIsNone(self.store.accepted(token['attempt']))
        record = self.store.accept(token, artifact, digest)
        self.assertEqual(record['token'], token)
        with self.assertRaises(StoreError):
            self.store.validate(token)
        restarted = Store(self.path, CAP, 'acceptance-restarted')
        try:
            self.assertEqual(restarted.accepted(token['attempt']), record)
        finally:
            restarted.close()

    def test_accept_publisher_failure_and_expiration_fail_closed(self):
        token = self.claim(ttl=.05)
        artifact = Path(self.tmp.name)/'accepted.json'
        content = b'{}'
        digest = hashlib.sha256(content).hexdigest()
        def failed_publisher():
            raise RuntimeError('simulated disk publication failure')
        with self.assertRaises(RuntimeError):
            self.store.accept(token, artifact, digest, failed_publisher)
        self.store.validate(token)
        def slow_publisher():
            artifact.write_bytes(content)
            time.sleep(.08)
        with self.assertRaises(StoreError):
            self.store.accept(token, artifact, digest, slow_publisher)
        self.assertIsNone(self.store.accepted(token['attempt']))
        self.assertTrue(artifact.exists())  # orphan file is not acceptance

    def test_latest_accepted_uses_authoritative_highest_fence(self):
        records = []
        for index in range(2):
            token = self.claim()
            artifact = Path(self.tmp.name)/('accepted-' + str(index) + '.json')
            artifact.write_text(json.dumps({'index': index}))
            records.append(self.store.accept(token, artifact, hashlib.sha256(artifact.read_bytes()).hexdigest()))
        self.assertGreater(records[1]['fence'], records[0]['fence'])
        self.assertEqual(self.store.latest_accepted('task'), records[1])
        self.assertEqual(self.store.accepted(records[0]['attempt']), records[0])
        self.assertIsNone(self.store.latest_accepted('unknown'))
        restarted = Store(self.path, CAP, 'latest-restarted')
        try:
            self.assertEqual(restarted.latest_accepted('task'), records[1])
        finally:
            restarted.close()

    def test_same_boot_capacity_disagreement_rejected(self):
        with self.assertRaises(StoreError):
            Store(self.path, dict(CAP, cpu=9), 'boot-test')


if __name__ == '__main__':
    unittest.main()
