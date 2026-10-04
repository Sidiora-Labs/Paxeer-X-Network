#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)
exec python3 - "$root" "$@" <<'PYGATE'
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import shutil
import socket as sockets
import stat
import subprocess
import sys
import time

ROOT = Path(sys.argv[1]).resolve()
TASK = '104.38.2'
GATE = 'tools/paxeer-x/gates/104.38.2.sh'
CASES = {
    'canonical-admission-and-receipt-selectors',
    'maximum-canonical-activity-and-bounded-deadline',
    'wrong-network-refusal',
    'wrong-protocol-refusal',
    'excessive-frame-refusal-before-body-allocation',
    'actual-incomplete-frame-closure-at-deadline',
    'signature-authentication-refusal',
    'malformed-submit-refusal',
    'real-response-loss-retains-original-bytes',
    'durable-unknown-receipt-after-native-restart',
    'post-restart-duplicate-preserves-original-receipt',
    'wrong-unix-peer-refusal-before-frame-parse',
}


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def private(path, directory=False):
    path = Path(path)
    info = path.lstat()
    require(not path.is_symlink() and info.st_uid == os.geteuid()
            and not info.st_mode & 0o077
            and (stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode)),
            'private caller-owned fixture or evidence required')
    return path.resolve(strict=True)


def load(path):
    return json.loads(private(path).read_text())


def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT, text=True).strip()


def sources():
    names = subprocess.check_output(['git', 'ls-files', '-z'], cwd=ROOT).split(b'\0')
    selected = {os.fsdecode(name) for name in names if name}
    selected.add(GATE)
    return {name: digest(ROOT / name) for name in sorted(selected)
            if name.startswith(('agent/', 'programs/', 'runtime/', 'src/', 'include/', '.cargo/', 'platform/crates/runtime-clock/', 'platform/crates/layerx-runtime-clock/'))
            and not any(part.startswith('.env') for part in Path(name).parts)
            and Path(name).suffix in {'.rs', '.toml', '.lock', '.c', '.h'}
            and (ROOT / name).is_file()} | {GATE: digest(ROOT / GATE)}


def save(path, value):
    if path.exists():
        private(path)
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'w') as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write('\n')


def checked(row):
    path = Path(row['path'])
    require(path.is_absolute() and not path.is_symlink() and path.is_file()
            and os.access(path, os.X_OK) and digest(path) == row['sha256'],
            'genuine unchanged prebuilt executable required')
    return path.resolve(strict=True)


def run(argv, path, environment, uid=None, gid=None):
    def credentials():
        os.setgroups([])
        os.setgid(gid)
        os.setuid(uid)
    with path.open('w') as stream:
        result = subprocess.run([str(arg) for arg in argv], cwd=ROOT, env=environment,
            stdin=subprocess.DEVNULL, stdout=stream, stderr=subprocess.STDOUT,
            timeout=min(600, max(1, deadline - time.time())),
            preexec_fn=credentials if uid is not None else None)
    path.chmod(0o600)
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, argv)
    return path.read_text()


def build():
    before = sources()
    argv = ['flock', '/root/lx-cargo/agent-build.lock',
            '/root/.cargo/bin/cargo', 'build', '--locked', '--manifest-path',
            'agent/tests/boundary/Cargo.toml', '--message-format=json']
    environment = dict(os.environ, CARGO_BUILD_JOBS='2',
        CARGO_TARGET_DIR=os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/agent-boundary-104382'),
        PATH='/root/.cargo/bin:' + os.environ.get('PATH', ''))
    output = run(argv, directory / 'build.log', environment)
    binaries = []
    for line in output.splitlines():
        if not line.startswith('{'):
            continue
        record = json.loads(line)
        if record.get('reason') == 'compiler-artifact' and record.get('executable'):
            if record['target']['name'] == 'agent-boundary-conformance':
                binaries.append(Path(record['executable']).resolve(strict=True))
    require(len(binaries) == 1 and sources() == before, 'one source-matched actual boundary artifact required')
    binary = binaries[0]
    save(directory / 'build-manifest.json', {'schema': 'layerx.production-lni-build.v1',
        'revision': git('rev-parse', 'HEAD'), 'tree': git('rev-parse', 'HEAD^{tree}'),
        'sources': before, 'build_exit_code': 0, 'command': argv,
        'harness': {'path': str(binary), 'sha256': digest(binary)}})


def inside(path, base):
    path = Path(path).resolve(strict=True)
    require(base in path.parents, 'runtime fixture input must belong to its disposable directory')
    return path


def process_identity(pid, native, uid, configuration):
    process = Path('/proc') / str(pid)
    require(pid > 1 and process.is_dir() and process.stat().st_uid == uid,
            'genuine disposable native owner process required')
    require((process / 'exe').resolve(strict=True) == native,
            'fixture owner is not the proven production native executable')
    argv = (process / 'cmdline').read_bytes().split(b'\0')
    require(argv[1:3] == [b'--serve', os.fsencode(configuration)]
            and len([arg for arg in argv if arg]) == 3,
            'fixture must use the actual production --serve configuration')
    state = (process / 'stat').read_text().rsplit(')', 1)[1].split()
    require(state[0] != 'Z', 'native fixture owner is not running')
    return state[19]


def qualify():
    require(os.geteuid() == 0, 'qualification requires genuine distinct Unix fixture identities')
    built = load(directory / 'build-manifest.json')
    require(built.get('schema') == 'layerx.production-lni-build.v1'
            and built['build_exit_code'] == 0 and built['sources'] == sources()
            and built['revision'] == git('rev-parse', 'HEAD')
            and built['tree'] == git('rev-parse', 'HEAD^{tree}'),
            'successful exact published-source build required')
    harness = checked(built['harness'])
    fixture_path = os.environ.get('PAXEER_X_LNI_PRODUCTION_FIXTURE')
    require(fixture_path, 'PAXEER_X_LNI_PRODUCTION_FIXTURE genuine disposable native fixture required')
    fixture = load(fixture_path)
    require(fixture.get('schema') == 'layerx.production-lni-fixture.v1'
            and fixture.get('disposable') is True, 'explicit disposable native fixture admission required')
    base = Path(fixture['fixture_root']).resolve(strict=True)
    native_manifest = load(fixture['native_artifact_manifest'])
    require(native_manifest.get('version') == 1 and native_manifest['build']['exit_code'] == 0,
            'genuine successful native build provenance required')
    native_sources = native_manifest.get('source_hashes')
    require(isinstance(native_sources, dict) and native_sources
            and any(name.startswith('cmd/layerxd/') for name in native_sources),
            'actual native dependency-source provenance required')
    require(all(not any(part.startswith('.env') for part in Path(name).parts)
                and digest(ROOT / name) == value for name, value in native_sources.items()),
            'native owner dependencies do not match this candidate')
    native = checked(native_manifest['artifacts']['layerxd'])
    configuration = inside(fixture['configuration_path'], base)
    require(not any(part.startswith('.env') for part in configuration.parts),
            'native configuration cannot materialize an environment file')
    owner_uid, client_uid, rejected_uid, client_gid = [int(fixture[key]) for key in
        ('native_uid', 'client_uid', 'rejected_uid', 'client_gid')]
    owner_gid = int(fixture['native_gid'])
    require(all(value > 0 for value in (owner_uid, client_uid, rejected_uid, client_gid))
            and len({owner_uid, client_uid, rejected_uid}) == 3,
            'distinct real daemon, admitted and rejected Unix identities required')
    base_info = base.stat()
    require(base_info.st_uid == os.geteuid() and base_info.st_gid == client_gid
            and stat.S_ISDIR(base_info.st_mode) and base_info.st_mode & 0o777 == 0o750,
            'disposable fixture root must permit the real client group without write access')
    executable_directory = base / 'qualification-bin'
    executable_directory.mkdir(mode=0o750, exist_ok=True)
    executable_info = executable_directory.lstat()
    require(not executable_directory.is_symlink() and stat.S_ISDIR(executable_info.st_mode)
            and executable_info.st_uid == os.geteuid() and not executable_info.st_mode & 0o022,
            'qualification executable directory must be owner-pinned')
    os.chown(executable_directory, os.geteuid(), client_gid)
    os.chmod(executable_directory, 0o750)
    runtime_harness = executable_directory / 'agent-boundary-conformance'
    require(not runtime_harness.is_symlink(), 'qualification artifact symlink refused')
    shutil.copyfile(harness, runtime_harness)
    os.chown(runtime_harness, os.geteuid(), client_gid)
    os.chmod(runtime_harness, 0o750)
    require(digest(runtime_harness) == built['harness']['sha256'], 'copied genuine artifact changed')
    node_environment = fixture['node_environment']
    client_environment = fixture['client_environment']
    require(isinstance(node_environment, dict) and isinstance(client_environment, dict)
            and all(isinstance(key, str) and isinstance(value, str)
                    for key, value in [*node_environment.items(), *client_environment.items()]),
            'actual native and client fixture environments required')
    require(all(key.startswith('LAYERX_NODE_') or key.startswith('LAYERX_RUNTIME_CLOCK_')
                for key in node_environment)
            and all(key.startswith('LAYERX_QUALIFY_') for key in client_environment),
            'fixture configuration contains an undeclared execution override')
    socket = inside(client_environment['LAYERX_QUALIFY_LNI_SOCKET'], base)
    admission = inside(node_environment['LAYERX_NODE_CHECKPOINT_DIRECTORY'], base)
    require(node_environment['LAYERX_NODE_LNI_SOCKET'] == str(socket)
            and int(node_environment['LAYERX_NODE_LNI_ALLOWED_UID']) == client_uid
            and int(node_environment['LAYERX_NODE_LNI_ALLOWED_GID']) == client_gid
            and int(client_environment['LAYERX_QUALIFY_LNI_DAEMON_UID']) == owner_uid
            and int(client_environment['LAYERX_QUALIFY_LNI_CLIENT_GID']) == client_gid
            and int(node_environment['LAYERX_NODE_LNI_FRAME_BYTES']) == 1_146_902
            and int(node_environment['LAYERX_NODE_LNI_DEADLINE_MS'])
                == int(client_environment['LAYERX_QUALIFY_LNI_DEADLINE_MS']),
            'fixture Unix authorization differs from the actual production owner')
    required_inputs = ('LAYERX_QUALIFY_SIGNED_ACTIVITY', 'LAYERX_QUALIFY_MAX_SIGNED_ACTIVITY',
                       'LAYERX_QUALIFY_UNKNOWN_SIGNED_ACTIVITY')
    inputs = {key: inside(client_environment[key], base) for key in required_inputs}
    require(len(set(inputs.values())) == len(required_inputs), 'distinct genuine signed fixture activities required')
    pid = int(fixture['native_node_pid'])
    original_identity = process_identity(pid, native, owner_uid, configuration)
    environment = {'PATH': os.environ.get('PATH', ''), **client_environment}
    reports = []
    reports.append(run([runtime_harness, '--production-peer-refusal'], directory / 'peer.log',
                       environment, rejected_uid, client_gid))
    reports.append(run([runtime_harness, '--production-required'], directory / 'production.log',
                       environment, client_uid, client_gid))
    unknown_bytes = inputs['LAYERX_QUALIFY_UNKNOWN_SIGNED_ACTIVITY'].read_bytes()
    journal = admission / '.layerxd-lni-admission.log'
    until = min(deadline, time.time() + 5)
    durable = False
    while time.time() < until:
        data = journal.read_bytes()
        offset = 32
        while offset + 64 <= len(data):
            length = int.from_bytes(data[offset + 16:offset + 20], 'big')
            require(0 < length <= 1_048_576, 'native admission journal activity bound violated')
            end = offset + 64 + length
            if end > len(data):
                break
            if data[offset + 64:end] == unknown_bytes:
                durable = True
            offset = end
        if durable:
            break
        time.sleep(0.02)
    require(durable, 'actual retained unknown bytes never reached the durable native journal')
    require(process_identity(pid, native, owner_uid, configuration) == original_identity,
            'fixture owner changed before the controlled disposable crash')
    os.kill(pid, signal.SIGKILL)
    until = min(deadline, time.time() + 5)
    while time.time() < until:
        process = Path('/proc') / str(pid)
        if not process.exists() or process.joinpath('stat').read_text().rsplit(')', 1)[1].split()[0] == 'Z':
            break
        time.sleep(0.02)
    else:
        raise RuntimeError('disposable native owner did not stop after its controlled crash')
    def native_credentials():
        os.setgroups([client_gid])
        os.setgid(owner_gid)
        os.setuid(owner_uid)
    native_log = directory / 'native-restart.log'
    with native_log.open('w') as stream:
        restarted = subprocess.Popen([str(native), '--serve', str(configuration)], cwd=base,
            env={'PATH': os.environ.get('PATH', ''), **node_environment}, stdin=subprocess.DEVNULL,
            stdout=stream, stderr=subprocess.STDOUT, preexec_fn=native_credentials)
        try:
            until = min(deadline, time.time() + 20)
            while time.time() < until:
                require(restarted.poll() is None, 'actual native owner failed during durable restart')
                try:
                    probe = sockets.socket(sockets.AF_UNIX)
                    probe.settimeout(0.1)
                    probe.connect(str(socket))
                    probe.close()
                    break
                except OSError:
                    time.sleep(0.05)
            else:
                raise RuntimeError('actual native owner never recovered its private LNI listener')
            recovered_identity = process_identity(restarted.pid, native, owner_uid, configuration)
            require(restarted.pid != pid or recovered_identity != original_identity,
                    'restart reused the original process identity')
            reports.append(run([runtime_harness, '--production-recovery'], directory / 'recovery.log',
                               environment, client_uid, client_gid))
            before_duplicate = digest(journal)
            reports.append(run([runtime_harness, '--production-duplicate'], directory / 'duplicate.log',
                               environment, client_uid, client_gid))
            require(digest(journal) == before_duplicate,
                    'post-restart duplicate appended a second native admission')
        finally:
            if restarted.poll() is None:
                restarted.terminate()
                try:
                    restarted.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    restarted.kill()
                    restarted.wait(timeout=5)
    native_log.chmod(0o600)
    found = re.findall(r'^LNI_CASE ([a-z0-9-]+)$', ''.join(reports), re.M)
    require(len(found) == len(CASES) and set(found) == CASES,
            'every mandatory genuine production case must pass exactly once')
    require(sources() == built['sources'] and digest(harness) == built['harness']['sha256']
            and digest(runtime_harness) == built['harness']['sha256'],
            'compiled source or harness changed during qualification')
    save(directory / 'result.json', {'task': TASK, 'revision': built['revision'],
        'command': 'timeout 30m tools/paxeer-x/verify-task.sh 104.38.2',
        'exit_code': 0, 'executed_cases': found, 'skipped': 0,
        'log_path': str(directory / 'production.log'), 'release_qualification': 'UNRUN'})
    print('PAXEER_X_GATE tests=' + str(len(CASES)) + ' skipped=0', flush=True)


try:
    directory = Path(os.environ.get('PAXEER_X_LNI_EVIDENCE',
        '/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task104382'))
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    directory = private(directory, True)
    require(ROOT not in directory.parents and directory != ROOT, 'private evidence must be outside checkout')
    deadline = float(os.environ.get('PAXEER_X_TASK_DEADLINE_EPOCH', str(time.time() + 1800)))
    args = sys.argv[2:]
    require(args in ([], ['--build']), '104.38.2 accepts only --build or no arguments')
    if args:
        build()
    else:
        qualify()
except subprocess.CalledProcessError as error:
    print('104.38.2 actual command exit=' + str(error.returncode), file=sys.stderr)
    sys.exit(error.returncode if error.returncode > 0 else 1)
except subprocess.TimeoutExpired:
    print('104.38.2 bounded task deadline exhausted', file=sys.stderr)
    sys.exit(124)
except (RuntimeError, OSError, ValueError, KeyError, TypeError) as error:
    print('104.38.2 genuine fixture or provenance refused: ' + str(error), file=sys.stderr)
    sys.exit(78)
PYGATE
