#!/usr/bin/env python3
import argparse
import contextlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import socket
import ssl
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

import paxeer_x_runtime_fixture as fixture
from paxeer_x_finality_fixture import FinalityProducer

ROOT = fixture.ROOT
VERIFY = 'timeout 1800s python3 tests/daemon/paxeer_x_kernel_readiness.py'
EXTRA = {'layerx-runtime-clock', 'layerxctl', 'layerx-handover', 'layerx-archive-codec', 'layerx-guarantor'}
SERVICES = ('treasury-signer', 'layerxd', 'layerxd-authority', 'guarantor-1', 'guarantor-2')
REQUIRED = {'matching-interfaces', 'authority-is-not-archive', 'wrong-origin', 'wrong-genesis',
            'wrong-network', 'stale-head', 'archive-unavailable', 'archive-stale',
            'archive-restart-unrecovered', 'archive-restart-recovered', 'replica-unavailable',
            'replica-missing-evidence', 'replica-restart-recovered', 'missing-manifest',
            'inaccessible-lni', 'supervisor-not-ready', 'clock-authority', 'absent-kernel'}


def require(value, message):
    fixture.require(value, message)


def wait(probe, label, timeout=90):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        try:
            value = probe()
            if value:
                return value
        except (OSError, ValueError, RuntimeError, urllib.error.URLError) as error:
            last = type(error).__name__
        time.sleep(.1)
    raise RuntimeError('readiness deadline: ' + label + ' (' + str(last) + ')')


def environment(path):
    return dict(line.split('=', 1) for line in Path(path).read_text().splitlines() if line and not line.startswith('#'))


def supplementary(path):
    require(bool(path), 'PAXEER_X_KERNEL_ARTIFACTS must name a private prebuilt manifest')
    fixture.private(path)
    value = json.loads(Path(path).read_text())
    revision = value.get('source_revision', '')
    require(value.get('version') == 1 and set(value.get('artifacts', {})) == EXTRA, 'kernel artifact schema/set')
    require(re.fullmatch('[a-f0-9]{40}', revision), 'kernel artifact source revision')
    tree = fixture.run(['git', 'rev-parse', revision + '^{tree}'], capture_output=True).stdout.decode().strip()
    require(value.get('source_tree') == tree, 'kernel artifact source tree')
    required_paths = {
        'layerx-runtime-clock': ['platform/hosted/runtime-clock', 'agent/crates/layerx-types'],
        'layerxctl': ['cmd/layerxctl', 'agent/crates'],
        'layerx-handover': ['cmd/layerx-handover', 'src', 'include'],
        'layerx-archive-codec': ['cmd/layerx-archive-codec', 'src', 'include'],
        'layerx-guarantor': ['cmd/layerx-guarantor', 'src', 'include'],
    }
    for name, row in value['artifacts'].items():
        target = Path(row.get('path', ''))
        require(target.is_absolute() and target.is_file() and not target.is_symlink()
                and os.access(target, os.X_OK), 'prebuilt executable missing: ' + name)
        require(row.get('source_revision') == revision and row.get('sha256') == fixture.digest(target),
                'prebuilt executable binding: ' + name)
        paths = row.get('source_paths')
        require(isinstance(paths, list) and all(p in paths for p in required_paths[name])
                and all(isinstance(p, str) and p and not p.startswith('/') and '..' not in Path(p).parts for p in paths),
                'production source binding: ' + name)
        require(not fixture.run(['git', 'diff', '--name-only', revision, 'HEAD', '--', *paths], capture_output=True).stdout,
                'prebuilt production dependency changed: ' + name)
    return value


def free_port():
    with socket.socket() as stream:
        stream.bind(('127.0.0.1', 0))
        return stream.getsockname()[1]


class ClockFinalityProducer(FinalityProducer):
    def output(self, guarantor):
        return (self.runtime.directory / ('guarantor-' + str(guarantor['index']) + '.clock.log')).read_text(errors='replace')


class Kernel(fixture.RuntimeFixture):
    def produce(self, label, argv, env=None, timeout=120):
        if label == 'bootstrap':
            args = list(map(str, argv[2:]))
            self.bootstrap = []
            for option, value in zip(args[::2], args[1::2]):
                if option not in ('--data-dir', '--run-dir', '--layerxd'):
                    self.bootstrap.extend((option, value))
            self.bootstrap_env = dict(env or {})
        return super().produce(label, argv, env, timeout)

    def install_extras(self, manifest):
        self.extra = {}
        for name, row in manifest['artifacts'].items():
            destination = self.directory / 'inputs' / name
            shutil.copyfile(row['path'], destination)
            require(fixture.digest(destination) == row['sha256'], 'staged executable digest: ' + name)
            destination.chmod(0o700)
            os.chown(destination, fixture.UID, fixture.UID)
            self.extra[name] = destination
        self.env['PATH'] = str(self.directory / 'inputs') + ':' + os.environ.get('PATH', '')
        self.init = self.directory / 'init'
        self.init.mkdir(mode=0o700)
        self.clock_root = self.directory / 'clocks'
        self.clock_root.mkdir(mode=0o700)
        os.chown(self.clock_root, fixture.UID, fixture.UID)

    def clock_launch(self, name, argv, env=None, uid=fixture.UID):
        require(name not in self.processes, 'clock role already owned: ' + name)
        clock = self.clock_root / str(SERVICES.index(name))
        clock.mkdir(mode=0o700, exist_ok=True)
        os.chown(clock, uid, fixture.UID)
        command = [str(self.extra['layerx-runtime-clock']), '--runtime-dir', str(clock), '--', *map(str, argv)]
        log = (self.directory / (name + '.clock.log')).open('ab')
        self.logs.append(log)
        process = subprocess.Popen(command, cwd=ROOT, env=self.env | (env or {}), stdin=subprocess.DEVNULL,
                                   stdout=log, stderr=log, user=uid, group=fixture.UID, extra_groups=[], start_new_session=True)
        self.processes[name] = process
        record = self.init / name
        record.write_text(str(uid) + ' running ' + str(process.pid) + '\n')
        record.chmod(0o600)
        wait(lambda: Path('/proc/' + str(process.pid) + '/exe').resolve().name == 'layerx-runtime-clock', 'real clock ' + name)
        return process.pid

    def supervisor(self, role):
        name = 'layerxd' if role == 'sequencer' else 'layerxd-authority'
        command = ['bash', ROOT / 'platform/hosted/node/supervisor.sh', '--role', role,
                   '--data-dir', self.directory / 'node', '--run-dir', self.directory / 'run',
                   '--layerxd', self.binary('layerxd')]
        if role == 'sequencer':
            command += ['--', *self.bootstrap]
        return self.clock_launch(name, command, self.bootstrap_env)

    def replica_without_status_manifest(self):
        script = 'set -ea; . "$1"; unset LAYERX_AUTHORITY_STATUS_GENESIS_MANIFEST; exec "$2" --authority-replica "$3"'
        return self.clock_launch('layerxd-authority', ['bash', '-c', script, 'kernel-readiness',
            self.directory / 'node/replica.env', self.binary('layerxd'), self.directory / 'node/replica.conf'])

    def supervised(self, manifest):
        self.install_extras(manifest)
        for name, selected in (('layerx-handover', self.extra['layerx-handover']),
                               ('layerx-genesis-build', self.binary('layerx-genesis-build'))):
            for destination in (ROOT / 'build/bin' / name, Path('/usr/local/bin') / name):
                if destination.exists():
                    require(destination.is_file() and not destination.is_symlink(), 'unbound supervisor binary path')
                    subprocess.run(['mount', '--bind', str(selected), str(destination)], check=True)
                    subprocess.run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', str(destination)], check=True)
        self.stop_role('sequencer')
        self.stop_role('replica')
        self.clock_launch('treasury-signer', [sys.executable, ROOT / 'platform/hosted/node/signer/signer.py',
            '--socket', self.directory / 'run/treasury-signer.sock', '--provider', 'file',
            '--key-file', self.directory / 'keys/treasury.seed', '--allowed-uid', str(fixture.UID),
            '--socket-group', str(fixture.UID)])
        wait(lambda: (self.directory / 'run/treasury-signer.sock').is_socket(), 'real treasury signer')
        self.supervisor('replica')
        self.supervisor('sequencer')
        wait(lambda: (self.directory / 'run/supervisor.sock').is_socket(), 'real supervisor', 150)
        self.readiness()
        producer = ClockFinalityProducer(self, {'artifacts': {'layerx-guarantor': manifest['artifacts']['layerx-guarantor']}})
        producer.bond()
        producer.tls()
        for position, guarantor in enumerate(producer.guarantors):
            name = 'guarantor-' + str(guarantor['index'])
            self.clock_launch(name, [producer.binaries['layerx-guarantor']], producer.environment(guarantor, position), uid=0)
        self.producer = producer
        producer.registered(1)


class ForeignNetwork(fixture.RuntimeFixture):
    def readiness(self):
        self.wait(lambda: (self.directory / 'run/layerxd.lni.sock').is_socket())


class Qualification:
    def __init__(self, args, evidence):
        self.args, self.evidence = args, evidence
        self.cases, self.processes, self.logs, self.archives, self.fixtures = [], {}, [], {}, []
        self.work = Path(tempfile.mkdtemp(prefix='lxkr-'))
        self.work.chmod(0o755)
        self.bundle = fixture.artifacts(os.environ.get('PAXEER_X_RUNTIME_ARTIFACTS', ''))
        self.client = fixture.client_artifact(os.environ.get('PAXEER_X_RUNTIME_CLIENT_MANIFEST', ''))
        self.extra = supplementary(os.environ.get('PAXEER_X_KERNEL_ARTIFACTS', ''))
        self.revision = fixture.run(['git', 'rev-parse', 'HEAD'], capture_output=True).stdout.decode().strip()
        self.app = args.app

    def case(self, name):
        require(name in REQUIRED and name not in self.cases, 'unknown/duplicate case')
        self.cases.append(name)
        print('KERNEL_CASE ' + name, flush=True)

    def start(self, name, argv, env=None):
        require(name not in self.processes, 'process already owned: ' + name)
        log = (self.evidence / (name + '.log')).open('ab')
        self.logs.append(log)
        process = subprocess.Popen(list(map(str, argv)), cwd=ROOT, env=dict(os.environ, PYTHONDONTWRITEBYTECODE='1') | (env or {}),
                                   stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True)
        self.processes[name] = process
        return process

    def stop(self, name):
        process = self.processes.pop(name)
        require(process.poll() is None, 'owned process exited: ' + name)
        process.send_signal(signal.SIGCONT)
        process.terminate()
        process.wait(timeout=20)
        require(process.returncode in (0, -signal.SIGTERM), 'owned process shutdown: ' + name)

    def json(self, origin, route, token=None):
        request = urllib.request.Request(origin + route)
        if token:
            request.add_header('Authorization', 'Bearer ' + token)
        try:
            with urllib.request.urlopen(request, context=self.tls, timeout=3) as response:
                return response.status, json.load(response)
        except urllib.error.HTTPError as error:
            body = error.read(1048577)
            try:
                return error.code, json.loads(body)
            except ValueError:
                return error.code, None

    def authority(self):
        node = environment(self.kernel.directory / 'node/replica.env')
        return self.json('http://127.0.0.1:' + node['LAYERX_AUTHORITY_PORT'],
                         '/v1/receipt-authority/status', node['LAYERX_AUTHORITY_BEARER_TOKEN'])

    def archive(self, name, runtime, upstream=None):
        port = free_port()
        url = 'https://127.0.0.1:' + str(port)
        node = environment(runtime.directory / 'node/node.env')
        config = dict(network_id=runtime.manifest['network_id'],
                      genesis_sha256=runtime.manifest['genesis_sha256'],
                      sequencer_id=node['LAYERX_NODE_SEQUENCER_ID'],
                      sequencer_public_key=node['LAYERX_NODE_SEQUENCER_PUBLIC_KEY'],
                      sequencer_first_batch=1, allow_loopback_dev=True,
                      poll_interval_seconds=.1, freshness_budget_seconds=2,
                      codec=str(self.kernel.extra['layerx-archive-codec']),
                      ca_file=str(self.work / 'tls.crt'), tls_cert=str(self.work / 'tls.crt'),
                      tls_key=str(self.work / 'tls.key'), data_dir=str(self.work / name),
                      listen='127.0.0.1:' + str(port), public_url=url)
        if upstream:
            config.update(sync_mode='remote', upstreams=[upstream])
        else:
            config.update(sync_mode='local', genesis_manifest=str(runtime.directory / 'node/genesis/genesis.manifest'),
                          genesis_snapshot=str(runtime.directory / 'node/genesis/00000000000000000000.lxs'),
                          source_log=str(runtime.directory / 'node/checkpoints/da-bodies.log'))
        path = self.work / (name + '.json')
        fixture.write_json(path, config)
        self.archives[name] = (url, path)
        self.restart_archive(name)
        wait(lambda: self.json(url, '/v1/sync/readiness')[0] == 200, name + ' actual durable readiness')
        return url

    def restart_archive(self, name):
        _, path = self.archives[name]
        return self.start(name, [self.kernel.binary('layerxd'), '--relay-archive', path],
                          self.kernel.env | {'LAYERX_RELAY_ARCHIVE_RUNTIME': str(ROOT / 'platform/relay_archive/runtime.py')})

    def collect(self, label, origin=None, data=None, run=None, init=None):
        env = dict(os.environ, CHECK_LIVE_KERNEL_LOCAL='1', CHECK_LIVE_KERNEL_APP=self.app,
                   CHECK_LIVE_KERNEL_DATA_DIR=str(data or self.kernel.directory / 'node'),
                   CHECK_LIVE_KERNEL_RUN_DIR=str(run or self.kernel.directory / 'run'),
                   CHECK_LIVE_KERNEL_INIT_DIR=str(init or self.kernel.init),
                   CHECK_LIVE_KERNEL_ARCHIVE_ORIGIN=origin or self.relay,
                   CHECK_LIVE_KERNEL_ARCHIVE_CA=str(self.work / 'tls.crt'),
                   CHECK_LIVE_KERNEL_CTL=str(self.kernel.extra['layerxctl']), PYTHONDONTWRITEBYTECODE='1')
        result = subprocess.run(['bash', str(ROOT / 'tools/bringup/check-live.sh'), 'kernel-node'], cwd=ROOT, env=env,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=60)
        (self.evidence / (label + '.stdout')).write_bytes(result.stdout)
        (self.evidence / (label + '.stderr')).write_bytes(result.stderr)
        return {'exit_code': result.returncode, 'stdout': result.stdout.decode()}

    def positive(self, label):
        def probe():
            require(all(process.poll() is None for process in self.kernel.processes.values()),
                    'owned kernel service exited; inspect private process logs')
            value = self.collect(label)
            return value if value['exit_code'] == 0 else None
        value = wait(probe, label)
        for marker in ('pass genesis ', 'pass sequencer ', 'pass supervisor state=running', 'pass lni socket=present',
                       'pass archive head=', 'pass replica head=', *('pass clock ' + role for role in SERVICES)):
            require(marker in value['stdout'], 'missing positive interface assertion: ' + marker)
        require('fail ' not in value['stdout'], 'positive collector contains failure')
        return value

    def negative(self, label, marker, **kwargs):
        value = self.collect(label, **kwargs)
        require(value['exit_code'] == 1 and marker in value['stdout'], 'negative collector assertion: ' + label)
        return value

    def submit(self, runtime, operation, sequence):
        result = runtime.invoke(operation, sequence, 'kernel-' + operation)
        require(any(line.startswith(b'receipt ') and b' result=0 ' in line for line in result.stdout.splitlines()),
                'signed native operation produced no successful receipt: ' + operation)
        if hasattr(runtime, 'producer'):
            runtime.producer.registered({'register': 1, 'open': 2, 'open-bob': 3}[operation])

    def new_runtime(self, name, network=77, cls=fixture.RuntimeFixture):
        old = fixture.NETWORK
        try:
            fixture.NETWORK = network
            runtime = cls(self.work / name, self.bundle, self.client)
            self.fixtures.append(runtime)
            runtime.generate()
            if network == 77:
                self.submit(runtime, 'register', 0)
            return runtime
        finally:
            fixture.NETWORK = old

    def run(self):
        self.kernel = self.new_runtime('kernel', cls=Kernel)
        self.kernel.supervised(self.extra)
        subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
                        '-subj', '/CN=localhost', '-addext', 'subjectAltName=IP:127.0.0.1,DNS:localhost',
                        '-keyout', str(self.work / 'tls.key'), '-out', str(self.work / 'tls.crt')],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=30)
        self.tls = ssl.create_default_context(cafile=self.work / 'tls.crt')
        self.origin = self.archive('origin', self.kernel)
        self.relay = self.archive('relay', self.kernel, self.origin)
        self.positive('initial')
        authority_origin = 'http://127.0.0.1:' + str(self.kernel.ports[8])
        token = environment(self.kernel.directory / 'node/replica.env')['LAYERX_AUTHORITY_BEARER_TOKEN']
        for route in ('/v1/sync/network', '/v1/sync/head'):
            require(self.json(authority_origin, route, token)[0] == 404, 'authority unexpectedly serves archive route')
        self.positive('authority-archive-404')
        self.case('authority-is-not-archive')
        self.negative('wrong-origin', 'fail genesis ', origin=authority_origin)
        self.case('wrong-origin')
        self.stop('origin')
        self.submit(self.kernel, 'open', 1)
        wait(lambda: self.authority()[1].get('head_batch') == '2', 'second real authority batch')
        require(self.json(self.relay, '/v1/sync/head')[1]['head_batch'] == '1', 'stale archive must retain genuine first batch')
        self.negative('stale-head', 'fail replica ')
        self.case('stale-head')
        wait(lambda: self.json(self.relay, '/readyz')[1]['freshness'] == 'stale', 'archive observation expires', 15)
        require(self.json(self.relay, '/v1/sync/readiness')[0] == 503, 'stale archive readiness must refuse')
        self.negative('archive-stale', 'fail replica ')
        self.case('archive-stale')
        old_pid = self.processes['relay'].pid
        self.stop('relay')
        self.negative('archive-unavailable', 'fail replica ')
        self.case('archive-unavailable')
        restarted = self.restart_archive('relay')
        wait(lambda: self.json(self.relay, '/v1/sync/readiness')[0] == 503, 'archive live but unrecovered')
        require(restarted.poll() is None and old_pid != restarted.pid, 'archive did not restart')
        self.negative('archive-restart-unrecovered', 'fail replica ')
        self.case('archive-restart-unrecovered')
        self.restart_archive('origin')
        self.positive('archive-restart-recovered')
        self.case('archive-restart-recovered')
        self.submit(self.kernel, 'open-bob', 0)
        wait(lambda: self.authority()[1].get('head_batch') == '3', 'third real authority batch')
        passing = self.positive('passing')
        require('pass replica head={"head":3}' in passing['stdout'], 'legacy three genuine batches')
        self.case('matching-interfaces')
        original_replica_pid = self.kernel.processes['layerxd-authority'].pid
        self.kernel.stop_role('layerxd-authority')
        self.negative('replica-unavailable', 'fail replica ')
        self.case('replica-unavailable')
        durable = Path(environment(self.kernel.directory / 'node/replica.env')['LAYERX_AUTHORITY_REPLICA_LOG'])
        retained = durable.with_name(durable.name + '.qualification-retained')
        durable.rename(retained)
        try:
            self.kernel.supervisor('replica')
            wait(lambda: self.authority()[0] == 503, 'real listener missing durable evidence')
            require(self.json(self.relay, '/v1/sync/readiness')[0] == 200, 'archive affected by replica restart')
            self.negative('replica-missing-evidence', 'fail replica ')
            self.case('replica-missing-evidence')
            self.kernel.stop_role('layerxd-authority')
        finally:
            if 'layerxd-authority' in self.kernel.processes:
                self.kernel.stop_role('layerxd-authority')
            if durable.exists():
                durable.unlink()
            retained.rename(durable)
        self.kernel.supervisor('replica')
        require(self.kernel.processes['layerxd-authority'].pid != original_replica_pid, 'replica PID did not change')
        self.positive('replica-restart-recovered')
        self.case('replica-restart-recovered')
        self.kernel.stop_role('layerxd-authority')
        self.kernel.replica_without_status_manifest()
        wait(lambda: self.authority()[0] == 503, 'missing manifest listener status')
        self.negative('missing-manifest', 'fail genesis ')
        self.case('missing-manifest')
        self.kernel.stop_role('layerxd-authority')
        self.kernel.supervisor('replica')
        self.positive('manifest-restored')
        for name, marker, case in (('layerxd.lni.sock', 'fail lni ', 'inaccessible-lni'),
                                   ('supervisor.sock', 'fail supervisor ', 'supervisor-not-ready')):
            target = self.kernel.directory / 'run' / name
            held = target.with_name(name + '.held')
            target.rename(held)
            try:
                self.negative(case, marker)
                self.case(case)
            finally:
                held.rename(target)
        record = self.kernel.init / 'guarantor-1'
        original = record.read_bytes()
        plain = self.start('plain-child', ['/bin/sleep', '300'])
        record.write_text('0 running ' + str(plain.pid) + '\n')
        try:
            one_failing = self.negative('one_failing', 'fail clock guarantor-1 exec=sleep')
            require(sum(line.startswith('fail ') for line in one_failing['stdout'].splitlines()) == 1,
                    'clock fault must be the sole logical failure')
            self.case('clock-authority')
        finally:
            record.write_bytes(original)
            self.stop('plain-child')
        absent = self.work / 'absent-kernel'
        absent.mkdir(mode=0o700)
        no_kernel = self.negative('no_kernel_image', 'fail genesis ', data=absent, run=absent, init=absent,
                                  origin='https://127.0.0.1:' + str(free_port()))
        for marker in ('fail genesis ', 'fail sequencer ', 'fail supervisor ', 'fail lni ', 'fail replica ',
                       *('fail clock ' + service for service in SERVICES)):
            require(marker in no_kernel['stdout'], 'absent kernel assertion missing: ' + marker)
        require(sum(line.startswith('fail ') for line in no_kernel['stdout'].splitlines()) == 10,
                'absent kernel must fail all ten logical checks')
        self.case('absent-kernel')
        for name, network in (('wrong-genesis', 77), ('wrong-network', 78)):
            other = self.new_runtime(name, network=network, cls=ForeignNetwork if network != 77 else fixture.RuntimeFixture)
            other_origin = self.archive(name + '-archive', other)
            require(other.manifest['genesis_sha256'] != self.kernel.manifest['genesis_sha256'], 'foreign signed genesis not distinct')
            actual = self.json(other_origin, '/v1/sync/network')[1]
            require(actual['network_id'] == network, 'foreign producer network')
            self.negative(name, 'fail genesis ', origin=other_origin)
            self.case(name)
        self.positive('final-recovered')
        require(set(self.cases) == REQUIRED, 'incomplete required case inventory')
        report = dict(source_revision=self.revision, genesis_sha256=self.kernel.manifest['genesis_sha256'],
                      sequencer_public_key=self.kernel.manifest['sequencer_public_key'],
                      cases=dict(passing=passing, one_failing=one_failing, no_kernel_image=no_kernel))
        fixture.write_json(self.evidence / 'report.json', report)
        return dict(revision=self.revision, command=VERIFY, exit_code=0, log_path=str(self.evidence),
                    cases=self.cases, runtime_artifact_revision=self.bundle['source_revision'],
                    kernel_artifact_revision=self.extra['source_revision'], deployed_qualification=False)

    def cleanup(self):
        for name in reversed(list(self.processes)):
            with contextlib.suppress(Exception):
                self.stop(name)
        for runtime in reversed(self.fixtures):
            try:
                runtime.cleanup()
            finally:
                for path in runtime.directory.glob('*.log'):
                    if path.is_file() and not path.is_symlink():
                        shutil.copyfile(path, self.evidence / (runtime.directory.name + '-' + path.name))
        for log in self.logs:
            log.close()


def main():
    global ROOT
    parser = argparse.ArgumentParser()
    parser.add_argument('--legacy-check-live', type=Path)
    parser.add_argument('--app', default='kernel-readiness-local')
    parser.add_argument('--isolated-child', action='store_true', help=argparse.SUPPRESS)
    parser.add_argument('--evidence', type=Path, help=argparse.SUPPRESS)
    args = parser.parse_args()
    os.umask(0o077)
    require(os.geteuid() == 0, 'isolated native process qualification requires root')
    if not args.isolated_child:
        evidence = args.legacy_check_live or Path(os.environ.get('PAXEER_X_KERNEL_EVIDENCE_ROOT', '/tmp')) / (
            'kernel-readiness-evidence-' + str(os.getpid()) + '-' + str(time.time_ns()))
        require(evidence.is_absolute() and not evidence.exists(), 'fresh absolute evidence directory required')
        evidence.mkdir(mode=0o700, parents=False)
        command = ['unshare', '--mount', '--net', '--pid', '--fork', '--kill-child=KILL', '--mount-proc', sys.executable,
                   str(Path(__file__).resolve()), '--isolated-child', '--evidence', str(evidence), '--app', args.app]
        env = dict(os.environ, PYTHONDONTWRITEBYTECODE='1')
        for namespace in ('net', 'pid', 'mnt'):
            env['PAXEER_X_KERNEL_PARENT_' + namespace.upper()] = os.readlink('/proc/self/ns/' + namespace)
        revision = fixture.run(['git', 'rev-parse', 'HEAD'], capture_output=True).stdout.decode().strip()
        exit_code = 1
        with (evidence / 'qualification.log').open('wb') as log:
            try:
                for variable in ('PAXEER_X_RUNTIME_ARTIFACTS', 'PAXEER_X_RUNTIME_CLIENT_MANIFEST', 'PAXEER_X_KERNEL_ARTIFACTS'):
                    require(os.environ.get(variable), variable + ' is required; no build or synthetic fallback is permitted')
                for executable in ('unshare', 'mount', 'ip', 'socat', 'setpriv', 'openssl', 'bash'):
                    require(shutil.which(executable), 'required real-process prerequisite: ' + executable)
                result = subprocess.run(command, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=1750)
                exit_code = result.returncode
            except subprocess.TimeoutExpired:
                exit_code = 124
                log.write(b'bounded qualification deadline exceeded\n')
            except (RuntimeError, OSError) as error:
                log.write((type(error).__name__ + ': ' + str(error) + '\n').encode())
        record = dict(revision=revision, command=VERIFY, exit_code=exit_code, log_path=str(evidence / 'qualification.log'))
        if not (evidence / 'result.json').exists():
            fixture.write_json(evidence / 'result.json', record)
        print(json.dumps(record))
        return exit_code
    for namespace in ('net', 'pid', 'mnt'):
        require(os.environ.get('PAXEER_X_KERNEL_PARENT_' + namespace.upper()) != os.readlink('/proc/self/ns/' + namespace)
                and os.environ.get('PAXEER_X_KERNEL_PARENT_' + namespace.upper()), 'isolated ' + namespace + ' namespace required')
    subprocess.run(['mount', '--make-rprivate', '/'], check=True)
    subprocess.run(['ip', 'link', 'set', 'lo', 'up'], check=True)
    source = Path(tempfile.mkdtemp(prefix='lxks-'))
    source.chmod(0o755)
    subprocess.run(['mount', '--bind', str(ROOT), str(source)], check=True)
    subprocess.run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', str(source)], check=True)
    ROOT = source
    fixture.ROOT = source
    python_root = Path(tempfile.mkdtemp(prefix='lxkp-'))
    python_root.chmod(0o755)
    subprocess.run(['mount', '--bind', sys.prefix, str(python_root)], check=True)
    subprocess.run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', str(python_root)], check=True)
    interpreter = Path(sys.executable).relative_to(Path(sys.prefix))
    sys.executable = str(python_root / interpreter)
    os.environ['PATH'] = str(python_root / 'bin') + ':/usr/local/bin:/usr/bin:/bin'
    qualification = None
    result = None
    try:
        qualification = Qualification(args, args.evidence)
        result = qualification.run()
    except Exception as error:
        revision = fixture.run(['git', 'rev-parse', 'HEAD'], capture_output=True).stdout.decode().strip()
        result = dict(revision=revision, command=VERIFY, exit_code=1, log_path=str(args.evidence / 'qualification.log'),
                      error=type(error).__name__ + ': ' + str(error), cases=[] if qualification is None else qualification.cases)
    finally:
        if qualification is not None:
            try:
                qualification.cleanup()
            except Exception as error:
                if result is None:
                    result = dict(revision=qualification.revision, command=VERIFY,
                                  log_path=str(args.evidence / 'qualification.log'), cases=qualification.cases)
                result['exit_code'] = 1
                result['cleanup_error'] = type(error).__name__ + ': ' + str(error)
    fixture.write_json(args.evidence / 'result.json', result)
    print(json.dumps(result), flush=True)
    return result['exit_code']


if __name__ == '__main__':
    sys.exit(main())
