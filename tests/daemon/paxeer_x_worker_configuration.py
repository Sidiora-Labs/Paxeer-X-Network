import json
import os
from pathlib import Path
import re
import stat
import shlex
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True

ROOT = Path(__file__).resolve().parents[2]
EXPECTED_TESTS = 10
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
    runtime = None

    @classmethod
    def run(cls, argv, label, timeout=900, env=None):
        log = cls.evidence / (label + '.log')
        try:
            result = subprocess.run([str(x) for x in argv], cwd=ROOT, capture_output=True, timeout=timeout, env=env)
        except subprocess.TimeoutExpired as error:
            log.write_bytes((error.stdout or b'') + (error.stderr or b''))
            log.chmod(0o600)
            cls.records.append({'revision': cls.revision, 'command': shlex.join(str(x) for x in argv),
                                'exit_code': 124, 'evidence': str(log)})
            raise
        log.write_bytes(result.stdout + result.stderr)
        os.chmod(log, 0o600)
        record = {'revision': cls.revision, 'command': shlex.join(str(x) for x in argv), 'exit_code': result.returncode,
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


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def bound_artifacts(values):
    import paxeer_x_runtime_fixture as fixture
    manifest_path = os.environ.get('PAXEER_X_WORKER_ARTIFACT_MANIFEST', '')
    require(manifest_path, 'missing prerequisite PAXEER_X_WORKER_ARTIFACT_MANIFEST')
    fixture.private(manifest_path)
    manifest = json.loads(Path(manifest_path).read_text())
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    require(manifest.get('version') == 1 and manifest.get('source_revision') == revision,
            'worker artifact manifest revision mismatch')
    require(not subprocess.check_output(['git', 'status', '--porcelain'], cwd=ROOT), 'dirty source tree')
    require(set(manifest.get('artifacts', {})) == {'layerxd', 'test_layerxd'}, 'worker artifact set')
    for name, variable in [('layerxd', 'PAXEER_X_LAYERXD'), ('test_layerxd', 'PAXEER_X_TEST_LAYERXD')]:
        row = manifest['artifacts'][name]
        path = Path(values[variable])
        require(path.is_absolute() and not any(p.is_symlink() for p in (path, *path.parents)),
                'artifact path must be absolute and have no symlink: ' + name)
        require(str(path) == row.get('path') and fixture.digest(path) == row.get('sha256'),
                'worker artifact path/digest mismatch: ' + name)
        with path.open('rb') as stream:
            require(stream.read(4) == b'\x7fELF', 'native executable required: ' + name)
    bundle = fixture.artifacts(os.environ.get('PAXEER_X_RUNTIME_ARTIFACTS', ''))
    fixture.client_artifact(os.environ.get('PAXEER_X_RUNTIME_CLIENT_MANIFEST', ''))
    require(fixture.digest(values['PAXEER_X_LAYERXD']) == bundle['artifacts']['layerxd']['sha256'],
            'runtime daemon differs from selected task artifact')
    for executable in ('unshare', 'ip', 'mount', 'openssl', 'jq', 'bash'):
        require(shutil.which(executable), 'missing prerequisite ' + executable)
    require(os.geteuid() == 0, 'root required for isolated production runtime')
    return manifest


def projection_rows(directory):
    path = directory / 'node/history.sqlite'
    require(path.is_file(), 'production history projection missing')
    with sqlite3.connect(path.as_uri() + '?mode=ro', uri=True) as db:
        require(db.execute('PRAGMA integrity_check').fetchall() == [('ok',)], 'history integrity failure')
        rows = db.execute('SELECT * FROM history_records ORDER BY record_offset').fetchall()
        meta = db.execute('SELECT * FROM history_index_meta ORDER BY singleton').fetchall()
    require(rows and meta and meta[0][2] == len(rows), 'history projection accounting')
    return rows, meta


def daemon_worker(directory):
    import paxeer_x_runtime_fixture as fixture
    from paxeer_x_runtime_fixture_test import receipts
    for name in ('net', 'mnt', 'pid'):
        require(os.readlink('/proc/self/ns/' + name) != os.environ['PAXEER_X_PARENT_' + name.upper()],
                'namespace not isolated')
    bundle = fixture.artifacts(os.environ['PAXEER_X_RUNTIME_ARTIFACTS'])
    client = fixture.client_artifact(os.environ['PAXEER_X_RUNTIME_CLIENT_MANIFEST'])
    fixture.run(['ip', 'link', 'set', 'lo', 'up'])
    fixture.run(['mount', '-t', 'tmpfs', '-o', 'mode=0755,nosuid,nodev', 'worker-config', '/tmp'])
    source = Path('/tmp/worker-config-source')
    source.mkdir(mode=0o755)
    fixture.run(['mount', '--bind', ROOT, source])
    fixture.run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', source])
    python_root = Path('/tmp/worker-config-python')
    python_root.mkdir(mode=0o755)
    fixture.run(['mount', '--bind', sys.prefix, python_root])
    fixture.run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', python_root])
    os.environ['PATH'] = str(python_root / 'bin') + ':/usr/local/bin:/usr/bin:/bin'
    fixture.ROOT = source
    runtime = fixture.RuntimeFixture(directory, bundle, client)
    completed = []
    try:
        runtime.generate()
        retained = []
        for operation, sequence in [('register', 0), ('open', 1), ('open-bob', 0),
                                    ('mint', 2), ('burn', 3), ('grant-issue', 4), ('grant-revoke', 5)]:
            retained.extend(receipts(runtime.invoke(operation, sequence, operation).stdout))
        retained.extend(receipts(runtime.invoke('sends', 6, 'initial-sends').stdout))
        sequence = 26
        for label, workers, serial, forced, rebuild in [
                ('zero', 0, False, False, False), ('serial', 0, True, True, False),
                ('maximum', 16, False, False, True), ('bounded', 3, False, True, False)]:
            before = runtime.invoke('read', 0, label + '-before').stdout
            authority = runtime.catch_up(retained)
            projection = projection_rows(runtime.directory)
            old = {role: runtime.stop_role(role, kill=forced) for role in ('sequencer', 'replica')}
            snapshots = sorted((runtime.directory / 'node/checkpoints').glob('[0-9]' * 20 + '.lxs'))
            require(snapshots, 'no real state checkpoint produced')
            latest = snapshots[-1]
            snapshot_digest = fixture.digest(latest)
            if rebuild:
                for suffix in ('', '-wal', '-shm'):
                    path = runtime.directory / ('node/history.sqlite' + suffix)
                    if path.exists():
                        path.unlink()
            for role in ('sequencer', 'replica'):
                path = runtime.directory / ('node/' + role + '.conf')
                text = path.read_text()
                require(text.startswith('config_version=2\n') and
                        not any(key in text for key in ('network_workers=', 'projection_workers=', 'checkpoint_workers=')),
                        'bootstrap emitted inactive role pools')
                text = re.sub(r'^verify_workers=.*$', 'verify_workers=' + str(workers), text, flags=re.M)
                text = re.sub(r'^serial_execution=.*$', 'serial_execution=' + str(serial).lower(), text, flags=re.M)
                with path.open('w') as output:
                    output.write(text)
                    output.flush()
                    os.fsync(output.fileno())
            runtime.start()
            require(all(runtime.processes[role].pid != pid for role, pid in old.items()), 'process not replaced')
            require(fixture.digest(latest) == snapshot_digest, 'restart changed canonical checkpoint')
            require(runtime.invoke('read', 0, label + '-after').stdout == before, 'economic state changed on restart')
            require(projection_rows(runtime.directory) == projection, 'projection changed or duplicate work was appended')
            for row in retained:
                recovered = runtime.invoke('receipt', row['id'], label + '-receipt-' + row['id']).stdout.decode().strip()
                require(recovered == row['raw'], 'first canonical signed receipt changed')
            require(runtime.catch_up(retained) == authority, 'replica witness changed')
            effective = max(1, workers)
            report = ('concurrency config_version=2 verify_workers=' + str(workers) +
                      ' effective_program_workers=' + str(effective) + ' serial_execution=' + str(serial).lower() +
                      ' owner=programs-kernel-prepare daemon_threads=executor')
            require(report in (runtime.directory / 'sequencer.log').read_text(), 'effective concurrency diagnostic absent')
            new_rows = receipts(runtime.invoke('send-one', sequence, label + '-new-work').stdout)
            require(len(new_rows) == 1 and new_rows[0]['id'] not in {row['id'] for row in retained}, 'new work identity reused')
            require(int(new_rows[0]['sequence']) > max(int(row['sequence']) for row in retained), 'execution sequence regressed')
            require(runtime.invoke('read', 0, label + '-new-state').stdout != before, 'new admitted work produced no state effect')
            retained.extend(new_rows)
            sequence += 1
            completed.append(label)
        before = runtime.invoke('read', 0, 'partial-before').stdout
        authority = runtime.catch_up(retained)
        runtime.stop_role('sequencer')
        projection = projection_rows(runtime.directory)
        socket = runtime.directory / 'run/layerxd.lni.sock'
        require(not socket.exists(), 'orderly shutdown left listener')
        socket.mkdir(mode=0o700)
        log_path = runtime.directory / 'sequencer.log'
        offset = log_path.stat().st_size
        failed = runtime.start_role('sequencer')
        require(failed.wait(timeout=45) != 0, 'occupied listener did not refuse startup')
        del runtime.processes['sequencer']
        output = log_path.read_bytes()[offset:]
        require(b'startup failed, rolling back daemon_started=1 lni_started=0' in output,
                'failure did not reach partial daemon startup')
        require(b'shutdown joined_threads=1' in output, 'partial startup did not join executor')
        socket.rmdir()
        runtime.start_role('sequencer')
        runtime.readiness()
        require(runtime.invoke('read', 0, 'partial-recovered').stdout == before, 'partial startup changed state')
        require(projection_rows(runtime.directory) == projection, 'partial startup changed projection')
        require(runtime.catch_up(retained) == authority, 'partial startup changed replica witness')
        completed.append('partial-startup-rollback')
        runtime.stop_role('sequencer')
        require(b'shutdown joined_threads=1' in log_path.read_bytes(), 'orderly shutdown missing executor join')
        fixture.write_json(runtime.directory / 'worker-configuration.json', {
            'settings': completed, 'canonical_receipts': len(retained), 'new_signed_sends': 24,
            'projection_rebuild': True, 'checkpoint_recovery': True, 'duplicate_execution': False})
    finally:
        runtime.cleanup()
    return 0


def qualify_daemon():
    directory = Path(tempfile.mkdtemp(prefix='px-worker-config-', dir='/var/tmp'))
    directory.rmdir()
    environment = os.environ.copy()
    for name in ('net', 'mnt', 'pid'):
        environment['PAXEER_X_PARENT_' + name.upper()] = os.readlink('/proc/self/ns/' + name)
    command = ['unshare', '--mount', '--net', '--pid', '--fork', '--kill-child=KILL',
               '--mount-proc', '--propagation', 'private', sys.executable, str(Path(__file__).resolve()),
               '--daemon-worker', str(directory)]
    result = Gate.run(command, 'real-daemon', timeout=950, env=environment)
    require(result.returncode == 0, 'real daemon lifecycle failed; evidence ' + str(directory))
    summary = json.loads((directory / 'worker-configuration.json').read_text())
    require(summary == {'settings': ['zero', 'serial', 'maximum', 'bounded', 'partial-startup-rollback'],
                        'canonical_receipts': 31, 'new_signed_sends': 24,
                        'projection_rebuild': True, 'checkpoint_recovery': True, 'duplicate_execution': False},
            'incomplete real daemon cases')
    Gate.runtime = {'directory': str(directory), 'result': summary}


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
        result = Gate.run([Gate.native], 'test-layerxd', timeout=300)
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
        result = Gate.run([sys.executable, str(Path(__file__).resolve())],
                          'missing-prerequisite', timeout=60, env=env)
        self.assertEqual(result.returncode, 2)
        self.assertIn(b'missing prerequisite PAXEER_X_TEST_LAYERXD', result.stderr)

    def test_10_real_daemon_checkpoint_projection_restart(self):
        qualify_daemon()


def main():
    os.umask(0o077)
    command = 'timeout 1800s python3 tests/daemon/paxeer_x_worker_configuration.py'
    status = 2
    summary = None
    artifact_manifest = None
    tests = skipped = 0
    failure = None
    try:
        Gate.revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
        missing, values = prerequisites()
        parent = Path(values['PAXEER_X_EVIDENCE_DIR'])
        if 'PAXEER_X_EVIDENCE_DIR' not in missing:
            require(not any(path.is_symlink() for path in (parent, *parent.parents)), 'evidence symlink refused')
            parent = parent.resolve()
            require(parent != ROOT and ROOT not in parent.parents, 'evidence must be outside source tree')
            Gate.evidence = Path(tempfile.mkdtemp(prefix='worker-configuration-' + Gate.revision[:12] + '-', dir=parent))
            summary = Gate.evidence / 'result.json'
        require(not missing, '; '.join('missing prerequisite ' + name for name in missing))
        artifact_manifest = bound_artifacts(values)
        Gate.layerxd = values['PAXEER_X_LAYERXD']
        Gate.native = values['PAXEER_X_TEST_LAYERXD']
        result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(Contract))
        tests, skipped = result.testsRun, len(result.skipped)
        status = 0 if result.wasSuccessful() and tests == EXPECTED_TESTS and not skipped else 1
        require(not subprocess.check_output(['git', 'status', '--porcelain'], cwd=ROOT), 'gate changed source tree')
    except (ImportError, OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        failure = str(error)
        print(failure, file=sys.stderr)
        if tests:
            status = 1
    if summary is not None:
        summary.write_text(json.dumps({'revision': Gate.revision, 'command': command,
            'exit_code': status, 'tests': tests, 'skipped': skipped, 'failure': failure,
            'commands': Gate.records, 'artifacts': artifact_manifest, 'runtime': Gate.runtime}, indent=2, sort_keys=True) + '\n')
        summary.chmod(0o600)
    print(f'{Gate.revision or "unknown"} | {command} | {status} | {summary}')
    print('PAXEER_X_GATE tests=' + str(tests) + ' skipped=' + str(skipped), flush=True)
    return status


if __name__ == '__main__':
    if len(sys.argv) == 3 and sys.argv[1] == '--daemon-worker':
        sys.exit(daemon_worker(sys.argv[2]))
    sys.exit(main())
