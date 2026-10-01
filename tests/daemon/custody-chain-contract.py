#!/usr/bin/env python3
import argparse
import copy
import importlib.util
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import unittest

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
from eth_account import Account

from custody_chain import (Chain, ROOT, USDL, artifact, artifact_manifest, calldata,
                           owned_chain, record_artifacts)

spec = importlib.util.spec_from_file_location('custody_exporter', ROOT / 'tests/daemon/withdraw-custody.py')
exporter = importlib.util.module_from_spec(spec)
spec.loader.exec_module(exporter)
ARGS = None


def run_export(arguments, log, timeout):
    process = subprocess.Popen(arguments, cwd=ROOT, stdout=log, stderr=log)
    try:
        return process.wait(timeout=timeout)
    finally:
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=20)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()


class Contract(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix='custody-contract-')
        self.addCleanup(self.directory.cleanup)
        self.work = Path(self.directory.name)
        self.bundle = artifact_manifest(ARGS.artifact_manifest)
        os.environ['PAXD'] = self.bundle['executables']['paxd']['path']
        self.contracts = Path(self.bundle['contract_directory'])

    def test_01_missing_and_incompatible_artifacts_refused(self):
        with self.assertRaises(ValueError):
            artifact_manifest(None)
        for label, update in (
                ('source', lambda d: d.update(source_binding='0' * 64)),
                ('missing', lambda d: d['executables']['paxd'].update(path=str(self.work / 'absent'))),
                ('digest', lambda d: d['contracts']['BetaUsdl'].update(sha256='0' * 64))):
            with self.subTest(label=label):
                value = copy.deepcopy(self.bundle)
                update(value)
                path = self.work / (label + '.json')
                path.write_text(json.dumps(value))
                path.chmod(0o600)
                with self.assertRaises(ValueError):
                    artifact_manifest(path)

    def test_02_real_chain_adapter_and_cleanup(self):
        genesis = exporter.custody_genesis(self.work, 2416, os.urandom(32))
        identity = None
        with owned_chain(self.work, self.contracts, genesis) as chain:
            identity = json.loads(chain.identity_path.read_text())
            self.assertEqual(chain.rpc('eth_chainId', []), '0x7d')
            self.assertEqual(int(chain.view(USDL, 'decimals()'), 16), 6)
            self.assertEqual(chain.view(USDL, 'owner()')[-40:].lower(), chain.account.address[2:].lower())
            self.assertEqual(chain.rpc('eth_getCode', [USDL, 'latest']).lower(),
                             artifact(self.contracts, 'BetaUsdl')['deployedBytecode']['object'].lower())
            native = json.loads((self.work / 'paxeer-genesis.json').read_text())['app_state']
            self.assertEqual(native['layerxcustody']['params']['network_id'], 2416)
            self.assertEqual(native['layerxanchor']['params']['network_id'], 2416)
            self.assertEqual(native['layerxanchor']['params']['paxeer_chain_id'], 125)
            self.assertEqual(native['layerxanchor']['params']['threshold'],
                             json.loads((ROOT / 'contracts/config/checkpoint-settlement.json').read_text())['finality_policy']['certificate_threshold'])
            other = Account.create()
            funded = chain.transaction('0x', other.address, value=10 ** 18)
            self.assertEqual(int(funded['status'], 16), 1)
            self.assertEqual(int(chain.rpc('eth_getBalance', [other.address, 'latest']), 16), 10 ** 18)
            chain.send(USDL, 'mint(address,uint256)', other.address, 20)
            self.assertEqual(int(chain.view(USDL, 'balanceOf(address)', other.address), 16), 20)
            chain.send(USDL, 'transfer(address,uint256)', chain.account.address, 7, signer=other)
            self.assertEqual(int(chain.view(USDL, 'balanceOf(address)', other.address), 16), 13)
            refused = chain.send(USDL, 'mint(address,uint256)', other.address, 1, signer=other, success=False)
            self.assertEqual(int(refused['status'], 16), 0)
            self.assertEqual(int(chain.view(USDL, 'balanceOf(address)', other.address), 16), 13)
            deployed = chain.deploy(artifact(self.contracts, 'WETH'), 'constructor()', [])
            chain.send(deployed, 'deposit()', value=10 ** 15)
            self.assertEqual(int(chain.view(deployed, 'balanceOf(address)', chain.account.address), 16), 10 ** 15)
            for label, update in (
                    ('foreign-process', lambda d: d.update(pid=os.getpid())),
                    ('wrong-anchor', lambda d: d.update(anchor_hash='0x' + '00' * 32)),
                    ('wrong-signer', lambda d: d.update(deployer=other.address)),
                    ('wrong-chain', lambda d: d.update(chain_id=126)),
                    ('forbidden-port', lambda d: d.update(rpc='http://127.0.0.1:18545')),
                    ('remote', lambda d: d.update(rpc='http://192.0.2.1:12345'))):
                with self.subTest(label=label):
                    bad = dict(identity)
                    update(bad)
                    path = self.work / (label + '.json')
                    path.write_text(json.dumps(bad))
                    with self.assertRaises((AssertionError, ValueError)):
                        Chain(path)
        self.assertIsNotNone(identity)
        self.assertFalse(Path('/proc/' + str(identity['pid'])).exists())
        self.assertFalse(Path(identity['home']).exists())

    def test_03_actual_export_and_duplicate_refusal(self):
        keys = {}
        for name in ('sequencer', 'beneficiary'):
            path = self.work / (name + '.key')
            key = os.urandom(32)
            path.write_bytes(key)
            path.chmod(0o600)
            keys[name] = (path, key)
        destination = self.work / 'export'
        arguments = [sys.executable, str(ROOT / 'tests/daemon/withdraw-custody.py'), str(ARGS.build_dir),
                     '--artifact-manifest', str(ARGS.artifact_manifest), '--export', str(destination),
                     '--network-id', '2416', '--sequencer-key', str(keys['sequencer'][0]),
                     '--beneficiary-key', str(keys['beneficiary'][0])]
        evidence = Path(os.environ['PAXEER_X_CUSTODY_EVIDENCE'])
        with (evidence / 'export.log').open('w') as log:
            completed = run_export(arguments, log, 300)
        self.assertEqual(completed, 0, 'actual exporter failed; see export.log')
        profile = (destination / 'custody.profile').read_bytes()
        activity = (destination / 'custody.activity').read_bytes()
        custody = json.loads((destination / 'custody.json').read_text())
        self.assertEqual(len(profile), 223)
        self.assertEqual(profile[:5], b'LXBC3')
        self.assertEqual(int.from_bytes(profile[201:205], 'big'), 2416)
        self.assertEqual(custody['chain_id'], 125)
        self.assertEqual(custody['asset'], '0x' + exporter.ASSET)
        self.assertEqual(int(custody['amount']), 1000000)
        self.assertEqual(activity.count(b'LXDC3'), 1)
        credit = activity[activity.index(b'LXDC3'):]
        public = Ed25519PrivateKey.from_private_bytes(keys['beneficiary'][1]).public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
        self.assertEqual(credit[37:41], (2416).to_bytes(4, 'big'))
        self.assertEqual(credit[75:107], bytes.fromhex(exporter.ASSET))
        self.assertEqual(credit[107:139], bytes.fromhex(custody['beneficiary'][2:]))
        self.assertEqual(credit[139:171], public)
        self.assertEqual(int.from_bytes(credit[191:207], 'big'), 1000000)
        retained = {name: (destination / name).read_bytes() for name in ('custody.profile', 'custody.activity', 'custody.json')}
        with (evidence / 'duplicate.log').open('w') as log:
            duplicate = run_export(arguments, log, 30)
        self.assertNotEqual(duplicate, 0)
        self.assertEqual(retained, {name: (destination / name).read_bytes() for name in retained})


def main():
    global ARGS
    parser = argparse.ArgumentParser()
    parser.add_argument('--build-dir', type=Path, required=True)
    parser.add_argument('--artifact-manifest', type=Path, default=os.environ.get('LAYERX_CUSTODY_ARTIFACT_MANIFEST'))
    parser.add_argument('--record-build', action='store_true')
    ARGS = parser.parse_args()
    ARGS.build_dir = ARGS.build_dir.resolve()
    if ARGS.record_build:
        if ARGS.artifact_manifest is None:
            raise ValueError('build manifest output required')
        record_artifacts(ARGS.build_dir, ARGS.artifact_manifest)
        return
    if not os.environ.get('PAXEER_X_CUSTODY_EVIDENCE'):
        raise ValueError('private evidence directory required')
    evidence = Path(os.environ['PAXEER_X_CUSTODY_EVIDENCE'])
    evidence.mkdir(mode=0o700, parents=True, exist_ok=True)
    if evidence.stat().st_mode & 0o077:
        raise ValueError('evidence directory must be private')
    os.umask(0o077)
    result = unittest.TextTestRunner(verbosity=2, failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Contract))
    print('PAXEER_X_GATE tests=%d skipped=%d' % (result.testsRun, len(result.skipped)))
    raise SystemExit(0 if result.wasSuccessful() else 1)


if __name__ == '__main__':
    signal.signal(signal.SIGTERM, lambda signum, frame: sys.exit(128 + signum))
    main()
