#!/usr/bin/env python3
"""Recovered finality is verified against the anchor's authenticated historical
membership, while fresh admission keeps enforcing current eligibility.

Everything runs live: an owned Paxeer chain and LayerX kernel (RuntimeFixture),
two real layerx-guarantor processes bonded in the anchor precompile, a checkpoint
they drive to FINAL, a real beginUnbond that makes one signer ineligible, and the
kernel restarted over the same disk. Nothing is replayed or recorded; a missing
prerequisite fails the gate.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shlex
import subprocess
import sys
import tempfile
import threading
import time
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent))
import paxeer_x_runtime_fixture as fixture  # noqa: E402
import paxeer_x_finality_fixture as finality  # noqa: E402

TAMPER = ('wrong block', 'earlier block', 'wrong set version', 'older set version', 'wrong membership proof',
          'short signer list', 'wrong chain', 'wrong anchor domain', 'wrong certificate threshold',
          'wrong certificate signature', 'wrong receipt', 'wrong checkpoint root', 'wrong checkpoint id', 'unavailable history')
FINALITY_KIND = 3


def save(directory, result):
    path = Path(directory) / 'result.json'
    temporary = path.with_suffix('.tmp')
    temporary.write_text(json.dumps(result, sort_keys=True) + '\n')
    temporary.chmod(0o600)
    os.replace(temporary, path)


def verifier_lines(result):
    return result.stdout.decode(errors='replace').splitlines()


def prefix_digest(path, length):
    with Path(path).open('rb') as stream:
        return hashlib.sha256(stream.read(length)).hexdigest()


def sequencer_refuses(runtime, seconds=90):
    """Start the sequencer over the current disk and report whether it refuses (exits) instead of serving."""
    runtime.start_role('sequencer')
    process = runtime.processes['sequencer']
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        if process.poll() is not None:
            del runtime.processes['sequencer']
            return True, process.returncode
        if (runtime.directory / 'run/layerxd.lni.sock').is_socket() and runtime.invoke('ready', 0, 'corrupt-ready', check=False).returncode == 0:
            return False, None
        time.sleep(.2)
    return False, None


def worker(directory, bundle, client, manifest):
    fixture.require(os.geteuid() == 0, 'namespace worker requires root')
    for name in ('net', 'pid', 'mnt'):
        fixture.require(os.readlink('/proc/self/ns/' + name) != os.environ['PAXEER_X_PARENT_' + name.upper()], 'namespace not isolated')
    fixture.run(['ip', 'link', 'set', 'lo', 'up'])
    fixture.run(['mount', '-t', 'tmpfs', '-o', 'mode=0755,nosuid,nodev', 'finality-fixture', '/tmp'])
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
    result = {'live_producer': True, 'replayed': False}
    producer = None
    try:
        runtime.generate()
        for operation, sequence in (('register', 0), ('open', 1), ('open-bob', 0)):
            runtime.invoke(operation, sequence, operation)
        producer = finality.FinalityProducer(runtime, manifest)
        producer.bond()
        producer.tls()
        producer.start()
        producer.registered(1)
        provenance = producer.provenance(1)
        fixture.write_json(Path(directory) / 'producer-manifest.json', provenance)
        result['producer'] = provenance
        ident = provenance['checkpoint_id']
        first = producer.guarantors[0]
        admitted = producer.verifier(first, 1, ident, True)
        result['admission_before_exit'] = dict(exit=admitted.returncode, lines=verifier_lines(admitted))
        save(directory, result)

        # ac_4: forced termination of the owned kernel while checkpoint 2 evidence registration is in flight.
        before = producer.records()
        log = runtime.directory / 'node/logs/evidence.log'
        before_digest = prefix_digest(log, before['valid_end'])
        minted = []
        thread = threading.Thread(target=lambda: minted.append(runtime.invoke('mint', 2, 'mint')))
        thread.start()
        producer.wait(lambda: int(producer.call('statusOf(uint64)(uint8)', 2)[0]) != 0, 300)
        killed = runtime.stop_role('sequencer', kill=True)
        thread.join(timeout=60)
        crashed = producer.records()
        runtime.start_role('sequencer')
        runtime.readiness()
        reopened = producer.records()
        producer.registered(2)
        settled = producer.records()
        result['crash'] = dict(killed_pid=killed, before=len(before['records']), at_crash=len(crashed['records']),
                               reopened=len(reopened['records']), settled=len(settled['records']),
                               prefix_preserved=prefix_digest(log, before['valid_end']) == before_digest
                               and crashed['records'][:len(before['records'])] == before['records']
                               and settled['records'][:len(before['records'])] == before['records'],
                               crash_previous_or_complete=crashed['records'] in (before['records'], settled['records'][:len(before['records']) + 1]),
                               finality_records=sum(r['evidence_kind'] == FINALITY_KIND for r in settled['records']),
                               mint_completed=bool(minted) and minted[0].returncode == 0)
        ident2, status2 = producer.checkpoint(2)
        result['crash']['checkpoint_2'] = dict(id=ident2, status=status2)
        save(directory, result)

        # ac_1 setup: stop the producers, then a real beginUnbond makes one finalized signer ineligible.
        members_before = producer.call('checkpointGuarantors(uint64)(bytes32[])', 1)
        producer.stop()
        exited = producer.guarantors[1]
        seen = producer.unbond(exited)
        result['exit'] = dict(guarantor_id=exited['id'], eligible=seen['eligible'], bond=seen['bond'], minimum_bond=producer.min_bond,
                              still_final=int(producer.call('statusOf(uint64)(uint8)', 1)[0]) == finality.STATUS_FINAL,
                              guarantors_unchanged=producer.call('checkpointGuarantors(uint64)(bytes32[])', 1) == members_before)
        save(directory, result)

        # ac_1: the owned kernel restarts over the same disk and replays the FINAL evidence after the exit.
        recorded = producer.records()
        read_before = runtime.invoke('read', 0, 'before-restart').stdout
        old = runtime.restart()
        restarted = producer.records()
        result['restart'] = dict(old_pids=old, records_identical=restarted == recorded,
                                 bytes_identical=prefix_digest(log, recorded['valid_end']) == prefix_digest(log, restarted['valid_end']),
                                 state_identical=runtime.invoke('read', 0, 'after-restart').stdout == read_before)
        save(directory, result)

        # ac_1/ac_2/ac_3: the produced payloads recover historically; fresh admission refuses; tampering refuses.
        cases = {}
        for batch, checkpoint in ((1, ident), (2, ident2)):
            refused = producer.verifier(first, batch, checkpoint, False)
            cases[str(batch)] = dict(exit=refused.returncode, lines=verifier_lines(refused))
        result['after_exit'] = cases
        save(directory, result)

        # ac_4 / truncated or corrupt log: a stopped kernel must refuse a damaged evidence log, never serve a partial record.
        last = restarted['records'][-1]
        middle = restarted['records'][0]
        original = log.read_bytes()
        runtime.stop_role('sequencer')
        damaged = bytearray(original)
        damaged[middle['offset'] + 40] ^= 0x01
        log.write_bytes(bytes(damaged))
        corrupt_refused, corrupt_code = sequencer_refuses(runtime)
        if 'sequencer' in runtime.processes:
            runtime.stop_role('sequencer')
        log.write_bytes(original)
        truncated = bytearray(original)
        cut = last['offset'] + 32 + last['length'] // 2
        truncated[cut:last['offset'] + 32 + last['length']] = bytes(last['offset'] + 32 + last['length'] - cut)
        log.write_bytes(bytes(truncated))
        truncated_refused, truncated_code = sequencer_refuses(runtime)
        truncated_records = None
        if 'sequencer' in runtime.processes:
            truncated_records = producer.records()['records']
            runtime.stop_role('sequencer')
        log.write_bytes(original)
        runtime.start_role('sequencer')
        runtime.readiness()
        result['damaged_log'] = dict(corrupt_refused=corrupt_refused, corrupt_exit=corrupt_code,
                                     truncated_refused=truncated_refused, truncated_exit=truncated_code,
                                     truncated_prior=truncated_records is None or truncated_records == restarted['records'][:-1],
                                     restored=producer.records() == restarted)
        save(directory, result)
        producer.cleanup()
        runtime.cleanup()
        result['owned_cleanup'] = True
        save(directory, result)
    finally:
        if producer is not None:
            producer.cleanup()
        runtime.cleanup()


class Contract(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.evidence = Path(os.environ.get('PAXEER_X_EVIDENCE_DIR', ''))
        fixture.require(cls.evidence.is_absolute() and cls.evidence.is_dir(), 'PAXEER_X_EVIDENCE_DIR required')
        cls.bundle = fixture.artifacts(os.environ.get('PAXEER_X_RUNTIME_ARTIFACTS', ''))
        cls.client = fixture.client_artifact(os.environ.get('PAXEER_X_RUNTIME_CLIENT_MANIFEST', ''))
        cls.manifest_path = os.environ.get('PAXEER_X_FINALITY_SUPPLEMENTAL_MANIFEST', '')
        cls.manifest = finality.supplemental(cls.manifest_path)
        directory = Path(tempfile.mkdtemp(prefix='px-finality-', dir='/var/tmp'))
        directory.rmdir()
        cls.directory = directory
        env = dict(os.environ)
        for name in ('net', 'pid', 'mnt'):
            env['PAXEER_X_PARENT_' + name.upper()] = os.readlink('/proc/self/ns/' + name)
        command = ['unshare', '--mount', '--net', '--pid', '--fork', '--kill-child=KILL', '--mount-proc', '--propagation', 'private',
                   sys.executable, str(Path(__file__).resolve()), '--worker', str(directory)]
        with (cls.evidence / 'finality-worker.log').open('wb') as log:
            cls.worker = subprocess.run(command, env=env, stdout=log, stderr=log, timeout=1500)
        print('worker command: ' + shlex.join(command), flush=True)
        print('worker exit=' + str(cls.worker.returncode) + ' private evidence=' + str(directory), flush=True)
        (cls.evidence / 'finality-directory').write_text(str(directory) + '\n')
        path = directory / 'result.json'
        cls.result = json.loads(path.read_text()) if path.is_file() else {}
        fixture.write_json(cls.evidence / 'finality-result.json', cls.result)
        if (directory / 'producer-manifest.json').is_file():
            fixture.write_json(cls.evidence / 'producer-manifest.json', json.loads((directory / 'producer-manifest.json').read_text()))

    def case(self, name):
        self.assertIn(name, self.result, 'live phase did not complete: ' + name + '; worker exit ' + str(self.worker.returncode))
        return self.result[name]

    def test_01_missing_supplemental_refused(self):
        with self.assertRaisesRegex(RuntimeError, 'supplemental guarantor manifest required'):
            finality.supplemental('')

    def test_02_supplemental_digest_refused(self):
        value = json.loads(json.dumps(self.manifest))
        value['artifacts']['layerx-guarantor']['sha256'] = '0' * 64
        path = self.evidence / 'wrong-supplemental.json'
        path.unlink(missing_ok=True)
        fixture.write_json(path, value)
        with self.assertRaisesRegex(RuntimeError, 'supplemental digest mismatch'):
            finality.supplemental(path)

    def test_03_live_producer_final(self):
        producer = self.case('producer')
        self.assertTrue(self.result['live_producer'])
        self.assertFalse(self.result['replayed'])
        self.assertEqual(producer['anchor_status'], 'FINAL')
        self.assertEqual(len(producer['checkpoint_guarantors']), 2)
        self.assertFalse(producer['credentials_included'])
        for g in producer['guarantors']:
            names = [row['name'] for row in g['files']]
            self.assertTrue(any(n.endswith('.checkpoint') for n in names) and any(n.endswith('.finality') for n in names)
                            and any(n.endswith('.header') for n in names))

    def test_04_fresh_admission_before_exit(self):
        admitted = self.case('admission_before_exit')
        self.assertEqual(admitted['exit'], 0, admitted['lines'])
        self.assertTrue(any(line.startswith('historical recovery passed') for line in admitted['lines']))
        self.assertIn('fresh admission passed status=0', admitted['lines'])

    def test_05_crash_during_registration(self):
        crash = self.case('crash')
        self.assertTrue(crash['prefix_preserved'])
        self.assertTrue(crash['crash_previous_or_complete'])
        self.assertIn(crash['at_crash'], (crash['before'], crash['before'] + 1))
        self.assertGreater(crash['settled'], crash['before'])
        self.assertEqual(crash['checkpoint_2']['status'], finality.STATUS_FINAL)

    def test_06_signer_exit(self):
        exit_ = self.case('exit')
        self.assertFalse(exit_['eligible'])
        self.assertLess(exit_['bond'], exit_['minimum_bond'])
        self.assertTrue(exit_['still_final'])
        self.assertTrue(exit_['guarantors_unchanged'])

    def test_07_restart_historical_recovery(self):
        restart = self.case('restart')
        self.assertTrue(restart['records_identical'])
        self.assertTrue(restart['bytes_identical'])
        self.assertTrue(restart['state_identical'])
        for batch, case in self.case('after_exit').items():
            self.assertTrue(any(line.startswith('historical recovery passed') for line in case['lines']), batch)

    def test_08_fresh_admission_refused_after_exit(self):
        for batch, case in self.case('after_exit').items():
            self.assertTrue(any(line.startswith('fresh admission refused status=') for line in case['lines']), batch)

    def test_09_tamper_refusals(self):
        for batch, case in self.case('after_exit').items():
            self.assertEqual(case['exit'], 0, case['lines'])
            for name in TAMPER:
                self.assertTrue(any(line.startswith(name + ' refused status=') for line in case['lines']), batch + ' ' + name)

    def test_10_damaged_log_refused(self):
        damaged = self.case('damaged_log')
        self.assertTrue(damaged['corrupt_refused'])
        self.assertTrue(damaged['truncated_refused'] or damaged['truncated_prior'])
        self.assertTrue(damaged['restored'])
        self.assertTrue(self.result.get('owned_cleanup'))


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--worker')
    args = parser.parse_args()
    if args.worker:
        worker(args.worker, fixture.artifacts(os.environ['PAXEER_X_RUNTIME_ARTIFACTS']),
               fixture.client_artifact(os.environ['PAXEER_X_RUNTIME_CLIENT_MANIFEST']),
               finality.supplemental(os.environ['PAXEER_X_FINALITY_SUPPLEMENTAL_MANIFEST']))
        return 0
    revision = fixture.run(['git', 'rev-parse', 'HEAD'], capture_output=True).stdout.decode().strip()
    print('revision=' + revision + ' command=' + shlex.join([sys.executable, *sys.argv]), flush=True)
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(Contract))
    ok = result.wasSuccessful() and result.testsRun == 10 and not result.skipped
    print('exit=' + str(0 if ok else 1) + ' evidence=' + os.environ.get('PAXEER_X_EVIDENCE_DIR', ''), flush=True)
    print('PAXEER_X_GATE tests=' + str(result.testsRun) + ' skipped=' + str(len(result.skipped)), flush=True)
    return 0 if ok else 1


if __name__ == '__main__':
    raise SystemExit(main())
