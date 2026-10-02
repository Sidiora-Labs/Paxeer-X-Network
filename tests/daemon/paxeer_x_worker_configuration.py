import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
EXPECTED_TESTS = 9
DEPRECATED = (b'layerxd: config deprecated: version 1 configuration accepted; rewrite as '
              b'config_version=2 (network_workers, projection_workers and checkpoint_workers are removed)\n')


def refused_line(key, value):
    return ('layerxd: config refused: ' + key + '=' + str(value) +
            ' has no execution owner; remove it and use config_version=2\n').encode()


def v2(verify='2', serial='false', extra='', version='2', role='sequencer'):
    return ('config_version=' + version + '\nrole=' + role + '\nnetwork_id=42\nstart_sequence=0\nverify_workers=' +
            verify + '\nserial_execution=' + serial + '\n' + extra)


def v1(verify=2, network=0, projection=0, checkpoint=0, serial='false'):
    return ('role=sequencer\nnetwork_id=42\nstart_sequence=0\nverify_workers=%d\nnetwork_workers=%d\n'
            'projection_workers=%d\ncheckpoint_workers=%d\nserial_execution=%s\n'
            % (verify, network, projection, checkpoint, serial))


def prerequisites():
    missing = []
    values = {}
    for name in ('PAXEER_X_LAYERXD', 'PAXEER_X_TEST_LAYERXD'):
        value = os.environ.get(name, '')
        if not value or not os.path.isfile(value) or not os.access(value, os.X_OK):
            missing.append(name)
        values[name] = value
    evidence = os.environ.get('PAXEER_X_EVIDENCE_DIR', '')
    if not evidence or not os.path.isdir(evidence) or stat.S_IMODE(os.stat(evidence).st_mode) != 0o700:
        missing.append('PAXEER_X_EVIDENCE_DIR')
    values['PAXEER_X_EVIDENCE_DIR'] = evidence
    return missing, values


class Gate:
    revision = ''
    layerxd = ''
    native = ''
    evidence = Path()
    records = []

    @classmethod
    def run(cls, argv, label, timeout=900):
        log = cls.evidence / (label + '.log')
        result = subprocess.run([str(x) for x in argv], cwd=ROOT, capture_output=True, timeout=timeout)
        log.write_bytes(result.stdout + result.stderr)
        os.chmod(log, 0o600)
        record = {'revision': cls.revision, 'command': ' '.join(str(x) for x in argv), 'exit_code': result.returncode,
                  'evidence': str(log)}
        cls.records.append(record)
        print('PAXEER_X_COMMAND revision=%s exit=%d evidence=%s command=%s'
              % (cls.revision, result.returncode, log, record['command']), flush=True)
        return result

    @classmethod
    def check_config(cls, text, label):
        directory = Path(tempfile.mkdtemp(prefix='px-worker-config-', dir=cls.evidence))
        path = directory / (label + '.conf')
        path.write_text(text)
        os.chmod(path, 0o600)
        return cls.run([cls.layerxd, '--check-config', path], 'check-config-' + label, timeout=60)


class Contract(unittest.TestCase):
    def accepted(self, text, label, stderr=b''):
        result = Gate.check_config(text, label)
        self.assertEqual(result.returncode, 0, label + ': ' + result.stderr.decode(errors='replace'))
        self.assertEqual(result.stderr, stderr, label)

    def refused(self, text, label, stderr=None):
        result = Gate.check_config(text, label)
        self.assertEqual(result.returncode, 1, label)
        self.assertIn(b'layerxd: failed with result ', result.stderr, label)
        if stderr is not None:
            self.assertTrue(result.stderr.startswith(stderr), label + ': ' + result.stderr.decode(errors='replace'))
            self.assertEqual(result.stderr.count(b'layerxd: config refused: '), 1, label)

    def test_01_native_dispatch_lifecycle_and_restart(self):
        result = Gate.run([Gate.native], 'test-layerxd', timeout=1200)
        self.assertEqual(result.returncode, 0, result.stderr.decode(errors='replace'))

    def test_02_supported_counts_accepted(self):
        for role in ('sequencer', 'replica', 'guarantor'):
            self.accepted(v2(role=role), 'v2-' + role)
        self.accepted(v2(verify='0'), 'v2-zero')
        self.accepted(v2(verify='16'), 'v2-maximum')
        self.accepted(v2(verify='0', serial='true'), 'v2-serial')

    def test_03_boundary_counts_refused(self):
        self.refused(v2(verify='17'), 'v2-over-limit')
        self.refused(v2(verify='18446744073709551616'), 'v2-overflow')
        self.refused(v2(verify='-1'), 'v2-negative')
        self.refused(v2(verify='1', serial='true'), 'v2-serial-with-workers')

    def test_04_inactive_pool_keys_refused_in_v2(self):
        for key in ('network_workers', 'projection_workers', 'checkpoint_workers'):
            self.refused(v2(extra=key + '=0\n'), 'v2-' + key)
        self.refused(v2(extra='executor_threads=4\n'), 'v2-unknown-key')
        self.refused(v2() + v2(), 'v2-repeated')

    def test_05_unsupported_version_refused(self):
        for version in ('0', '1', '3', ''):
            self.refused(v2(version=version), 'version-' + (version or 'empty'))

    def test_06_legacy_zero_pools_deprecated(self):
        self.accepted(v1(), 'v1-zero-pools', DEPRECATED)
        self.accepted(v1(verify=0, serial='true'), 'v1-serial', DEPRECATED)
        self.refused(v1(verify=17), 'v1-verify-over-limit')
        self.refused(v1(verify=1, serial='true'), 'v1-serial-with-workers')

    def test_07_legacy_active_pools_refused_with_owner_diagnostic(self):
        self.refused(v1(network=2), 'v1-network', refused_line('network_workers', 2))
        self.refused(v1(projection=2), 'v1-projection', refused_line('projection_workers', 2))
        self.refused(v1(checkpoint=1), 'v1-checkpoint', refused_line('checkpoint_workers', 1))
        self.refused(v1(network=0, projection=3, checkpoint=1), 'v1-first-in-file-order',
                     refused_line('projection_workers', 3))
        self.refused(v1(network=17), 'v1-pool-over-old-bound')

    def test_08_bootstrap_writes_supported_configuration(self):
        source = (ROOT / 'platform/hosted/node/bootstrap.sh').read_text()
        match = re.search(r"write_config\(\) \{\n    printf '([^']*)' \"\$1\" \"\$NETWORK_ID\" > \"\$2\"\n\}", source)
        self.assertIsNotNone(match, 'bootstrap write_config not found')
        self.assertNotRegex(match.group(1), 'network_workers|projection_workers|checkpoint_workers')
        for role in ('sequencer', 'replica'):
            rendered = Gate.run(['bash', '-c', 'printf "$0" "$1" "$2"', match.group(1), role, '42'],
                                'bootstrap-render-' + role, timeout=60)
            self.assertEqual(rendered.returncode, 0)
            self.assertTrue(rendered.stdout.startswith(b'config_version=2\n'))
            self.accepted(rendered.stdout.decode(), 'bootstrap-' + role)

    def test_09_missing_prerequisite_refused(self):
        env = {key: value for key, value in os.environ.items() if key != 'PAXEER_X_TEST_LAYERXD'}
        result = subprocess.run([sys.executable, str(Path(__file__).resolve())], cwd=ROOT, env=env,
                                capture_output=True, timeout=60)
        log = Gate.evidence / 'missing-prerequisite.log'
        log.write_bytes(result.stdout + result.stderr)
        os.chmod(log, 0o600)
        self.assertEqual(result.returncode, 2)
        self.assertIn(b'missing prerequisite PAXEER_X_TEST_LAYERXD', result.stderr)


def main():
    os.umask(0o077)
    missing, values = prerequisites()
    if missing:
        for name in missing:
            print('paxeer_x_worker_configuration: missing prerequisite ' + name, file=sys.stderr, flush=True)
        return 2
    Gate.revision = subprocess.run(['git', 'rev-parse', 'HEAD'], cwd=ROOT, check=True,
                                   capture_output=True).stdout.decode().strip()
    Gate.layerxd = values['PAXEER_X_LAYERXD']
    Gate.native = values['PAXEER_X_TEST_LAYERXD']
    Gate.evidence = Path(values['PAXEER_X_EVIDENCE_DIR']) / ('worker-configuration-' + Gate.revision[:12])
    Gate.evidence.mkdir(mode=0o700, parents=True, exist_ok=True)
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(Contract))
    passed = result.wasSuccessful() and result.testsRun == EXPECTED_TESTS and not result.skipped
    summary = Gate.evidence / 'result.json'
    summary.write_text(json.dumps({'revision': Gate.revision, 'command': 'timeout 1800s python3 tests/daemon/paxeer_x_worker_configuration.py',
                                   'exit_code': 0 if passed else 1, 'tests': result.testsRun, 'skipped': len(result.skipped),
                                   'commands': Gate.records}, indent=2, sort_keys=True) + '\n')
    os.chmod(summary, 0o600)
    print('PAXEER_X_EVIDENCE revision=%s path=%s' % (Gate.revision, Gate.evidence), flush=True)
    print('PAXEER_X_GATE tests=' + str(result.testsRun) + ' skipped=' + str(len(result.skipped)), flush=True)
    return 0 if passed else 1


if __name__ == '__main__':
    sys.exit(main())
