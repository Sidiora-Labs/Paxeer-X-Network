import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import sys
import unittest
import concurrent.futures

import paxeer_x_runtime_fixture as fixture
from pay1 import validate_reads
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat


def receipts(raw):
    rows = [dict(item.split('=', 1) for item in line.split()[1:]) for line in raw.decode().splitlines() if line.startswith('receipt ')]
    fixture.require(rows and all(row['result'] == '0' and len(row['id']) == 64 for row in rows), 'no successful signed receipts')
    return rows


def worker(directory, bundle, client):
    fixture.require(os.geteuid() == 0, 'namespace worker requires root')
    for name in ('net', 'pid', 'mnt'):
        fixture.require(os.readlink('/proc/self/ns/' + name) != os.environ['PAXEER_X_PARENT_' + name.upper()], 'namespace not isolated')
    fixture.run(['ip', 'link', 'set', 'lo', 'up'])
    fixture.run(['mount', '-t', 'tmpfs', '-o', 'mode=0755,nosuid,nodev', 'runtime-fixture', '/tmp'])
    source = Path('/tmp/runtime-source')
    source.mkdir(mode=0o755)
    fixture.run(['mount', '--bind', fixture.ROOT, source])
    fixture.run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', source])
    fixture.ROOT = source
    python_root = Path('/tmp/runtime-python')
    python_root.mkdir(mode=0o755)
    fixture.run(['mount', '--bind', sys.prefix, python_root])
    fixture.run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', python_root])
    os.environ['PATH'] = str(python_root / 'bin') + ':/usr/local/bin:/usr/bin:/bin'
    runtime = fixture.RuntimeFixture(directory, bundle, client)
    sentinel = subprocess.Popen(['sleep', '1200'])
    try:
        runtime.generate()
        chain_before = int(runtime.rpc('eth_blockNumber'), 16)
        genesis_block = runtime.rpc('eth_getBlockByNumber', ['0x1', False])['hash']
        public = Ed25519PrivateKey.from_private_bytes((runtime.directory / 'keys/treasury.seed').read_bytes()).public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
        salt = (runtime.directory / 'salt').read_bytes()
        retained = []
        for stage, operation, sequence in [(0, 'register', 0), (1, 'open', 1), (2, 'open-bob', 0), (3, 'mint', 2), (4, 'burn', 3), (5, 'grant-issue', 4), (6, 'grant-revoke', 5)]:
            retained.extend(receipts(runtime.invoke(operation, sequence, operation).stdout))
            validate_reads(runtime.invoke('read', 0, operation + '-state').stdout, salt, public, stage)
        retained.extend(receipts(runtime.invoke('sends', 6, 'canonical-sends').stdout))
        before = runtime.invoke('read', 0, 'before').stdout
        validate_reads(before, salt, public, 7)
        first_evidence = runtime.catch_up(retained)
        for forced in (False, True):
            runtime.restart(kill=forced)
            fixture.require(runtime.invoke('read', 0, 'after-' + str(forced)).stdout == before, 'durable state differs after restart')
            for row in retained:
                raw = runtime.invoke('receipt', row['id'], 'retained-' + row['id']).stdout.decode().strip()
                fixture.require(raw == row['raw'], 'retained signed receipt differs')
            fixture.require(runtime.catch_up(retained) == first_evidence, 'replica evidence differs after same-disk restart')
        fixture.require(int(runtime.rpc('eth_blockNumber'), 16) >= chain_before, 'chain height regressed')
        fixture.require(runtime.rpc('eth_getBlockByNumber', ['0x1', False])['hash'] == genesis_block, 'chain history changed')
        old_replica = runtime.stop_role('replica', kill=True)
        with concurrent.futures.ThreadPoolExecutor(max_workers=1) as executor:
            pending = executor.submit(runtime.invoke, 'send-one', 26, 'during-replica-outage')
            time.sleep(.2)
            runtime.start_role('replica')
            fixture.require(runtime.processes['replica'].pid != old_replica, 'replica was not replaced')
            runtime.wait(runtime.replica_ready)
            retained.extend(receipts(pending.result().stdout))
        runtime.catch_up(retained)
        fixture.require(runtime.invoke('read', 0, 'after-catchup').stdout != before, 'new signed send produced no state change')
        runtime.cleanup()
        fixture.require(sentinel.poll() is None, 'cleanup killed non-owned process')
        fixture.write_json(Path(directory) / 'result.json', {'real_chain': True, 'kernel_admission': True, 'canonical_signed_sends': 21,
            'signed_receipts': len(retained), 'graceful_restart': True, 'forced_restart': True, 'same_disk_state_and_receipts': True,
            'authenticated_replica_catchup': True, 'owned_cleanup': True, 'downstream_module_credit': False})
    finally:
        runtime.cleanup()
        sentinel.terminate()
        sentinel.wait(timeout=5)


class Contract(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.bundle_path = os.environ.get('PAXEER_X_RUNTIME_ARTIFACTS', '')
        cls.client_path = os.environ.get('PAXEER_X_RUNTIME_CLIENT_MANIFEST', '')
        cls.bundle = fixture.artifacts(cls.bundle_path)
        cls.client = fixture.client_artifact(cls.client_path)
        cls.evidence = Path(os.environ['PAXEER_X_RUNTIME_EVIDENCE'])
        cls.evidence.mkdir(mode=0o700, parents=True, exist_ok=True)

    def test_01_missing_artifact_refused(self):
        with self.assertRaisesRegex(RuntimeError, 'required'):
            fixture.artifacts('')

    def test_02_mismatched_source_refused(self):
        path = self.evidence / 'wrong-source.json'
        fixture.write_json(path, dict(self.bundle, source_tree='0' * 40))
        with self.assertRaisesRegex(RuntimeError, 'source tree mismatch'):
            fixture.artifacts(path)

    def test_03_missing_executable_refused(self):
        value = json.loads(json.dumps(self.bundle))
        value['artifacts']['paxd']['path'] = str(self.evidence / 'absent-paxd')
        path = self.evidence / 'missing-executable.json'
        fixture.write_json(path, value)
        with self.assertRaisesRegex(RuntimeError, 'missing executable'):
            fixture.artifacts(path)

    def test_04_wrong_network_refused(self):
        result = subprocess.run(['bash', str(fixture.ROOT / 'platform/hosted/paxeer/init-chain.sh')],
            env=dict(os.environ, LAYERX_PAXEER_CHAIN_ID='124'), capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(b'not mapped', result.stderr)
        (self.evidence / 'wrong-network.log').write_bytes(result.stdout + result.stderr)

    def test_05_actual_lifecycle(self):
        directory = Path(tempfile.mkdtemp(prefix='px-runtime-', dir='/var/tmp'))
        directory.rmdir()
        env = dict(os.environ)
        for name in ('net', 'pid', 'mnt'):
            env['PAXEER_X_PARENT_' + name.upper()] = os.readlink('/proc/self/ns/' + name)
        with (self.evidence / 'worker.log').open('wb') as log:
            result = subprocess.run(['unshare', '--mount', '--net', '--pid', '--fork', '--kill-child=KILL', '--mount-proc', '--propagation', 'private',
                'python3', str(Path(__file__).resolve()), '--worker', str(directory)], env=env, stdout=log, stderr=log, timeout=1000)
        (self.evidence / 'runtime-directory').write_text(str(directory) + '\n')
        self.assertEqual(result.returncode, 0, 'real lifecycle failed; evidence ' + str(directory))
        result = json.loads((directory / 'result.json').read_text())
        self.assertEqual(result['canonical_signed_sends'], 21)
        self.assertTrue(result['authenticated_replica_catchup'])
        fixture.write_json(self.evidence / 'result.json', result)


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--worker')
    args = parser.parse_args()
    if args.worker:
        worker(args.worker, fixture.artifacts(os.environ['PAXEER_X_RUNTIME_ARTIFACTS']), fixture.client_artifact(os.environ['PAXEER_X_RUNTIME_CLIENT_MANIFEST']))
        return 0
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(Contract))
    print('PAXEER_X_GATE tests=' + str(result.testsRun) + ' skipped=' + str(len(result.skipped)), flush=True)
    return 0 if result.wasSuccessful() and result.testsRun == 5 and not result.skipped else 1


if __name__ == '__main__':
    raise SystemExit(main())
