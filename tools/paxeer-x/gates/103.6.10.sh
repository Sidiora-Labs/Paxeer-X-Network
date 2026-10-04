#!/usr/bin/env bash
# paxeer-x-services: kernel paxeer-boundaries
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)
exec python3 - "$ROOT" "${1:---verify}" <<'PY'
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys

root = Path(sys.argv[1])
mode = sys.argv[2]
if mode not in ('--build', '--verify'):
    raise SystemExit('103.6.10: expected --build or --verify')
directory_name = os.environ.get('LAYERX_FINALITY_QUALIFICATION_DIR', '')
if not directory_name:
    raise SystemExit('103.6.10: private LAYERX_FINALITY_QUALIFICATION_DIR required')
directory = Path(directory_name)
if not directory.is_absolute() or directory.is_symlink() or directory.resolve().is_relative_to(root):
    raise SystemExit('103.6.10: qualification directory must be private and outside checkout')
directory.mkdir(mode=0o700, parents=True, exist_ok=True)
metadata = directory.stat()
if not stat.S_ISDIR(metadata.st_mode) or metadata.st_mode & 0o077 or metadata.st_uid != os.geteuid():
    raise SystemExit('103.6.10: qualification directory permissions must be private')
manifest = directory / 'build.json'

def git(*arguments):
    return subprocess.check_output(['git', '-C', str(root), *arguments], text=True).strip()

def digest(path):
    value = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            value.update(block)
    return value.hexdigest()

def source():
    if git('status', '--porcelain', '--untracked-files=normal'):
        raise SystemExit('103.6.10: frozen clean published checkout required')
    names = git('ls-files', 'Makefile', 'include', 'src', 'cmd', 'programs', 'agent',
                'platform/Cargo.toml', 'platform/Cargo.lock', 'platform/hosted/authority',
                'contracts/config/checkpoint-settlement.json', 'tests/daemon',
                'tools/paxeer-x/gates/103.6.10.sh').splitlines()
    hashes = {name: digest(root / name) for name in names
              if not any(part.startswith('.env') for part in Path(name).parts)
              and (root / name).is_file()}
    return {'revision': git('rev-parse', 'HEAD'), 'tree': git('rev-parse', 'HEAD^{tree}'),
            'sources': hashes}

def run(command, log, environment=None):
    with log.open('w') as stream:
        completed = subprocess.run(command, cwd=root, env=environment,
                                   stdout=stream, stderr=subprocess.STDOUT)
    if completed.returncode:
        raise SystemExit('103.6.10: exit=%d log=%s' % (completed.returncode, log))

if mode == '--build':
    if manifest.exists():
        raise SystemExit('103.6.10: refusing to relabel an existing build manifest')
    binding = source()
    native = directory / 'native'
    cargo = os.environ.get('PLATFORM_CARGO', '/root/.cargo/bin/cargo')
    programs_target = Path(os.environ.get('LAYERX_FINALITY_PROGRAMS_TARGET', '/root/lx-target/programs'))
    platform_target = Path(os.environ.get('LAYERX_FINALITY_PLATFORM_TARGET', '/root/lx-target/platform'))
    if not programs_target.is_absolute() or not platform_target.is_absolute():
        raise SystemExit('103.6.10: absolute Cargo target paths required')
    environment = dict(os.environ, CARGO_BUILD_JOBS='3', CARGO_TARGET_DIR=str(programs_target))
    lock_path = Path(os.environ.get('LAYERX_NATIVE_BUILD_LOCK', '/root/lx-cargo/native-build.lock'))
    lock_path.parent.mkdir(parents=True, exist_ok=True)
    with lock_path.open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        run([cargo, 'build', '--locked', '--manifest-path', 'programs/Cargo.toml',
             '-p', 'layerx-programs-sandbox', '--features', 'host-ffi'],
            directory / 'programs-build.log', environment)
        library = programs_target / 'debug/liblayerx_programs_sandbox.a'
        if not library.is_file():
            raise SystemExit('103.6.10: genuine compiled Programs host-ffi archive missing')
        run(['make', '-j3', '-o', 'programs-build', 'BUILD_DIR=' + str(native),
             'LXP_REVISION=' + binding['revision'], 'PROGRAMS_RUNTIME_LIB=' + str(library),
             str(native / 'tests/lxp_test_daemon_finality_authority'),
             str(native / 'tests/lxp_test_finality_json'), 'layerxd', 'layerx-genesis-build'],
            directory / 'native-build.log', environment)
        environment['CARGO_TARGET_DIR'] = str(platform_target)
        run([cargo, 'test', '--locked', '--manifest-path', 'platform/Cargo.toml',
             '-p', 'layerx-platform-authority', '--test', 'real_node', '--no-run',
             '--message-format=json'], directory / 'platform-build.log', environment)
    artifacts = []
    for line in (directory / 'platform-build.log').read_text().splitlines():
        try:
            row = json.loads(line)
        except ValueError:
            continue
        if row.get('reason') == 'compiler-artifact' and row.get('target', {}).get('name') == 'real_node' \
                and row.get('manifest_path') == str(root / 'platform/hosted/authority/Cargo.toml') \
                and row.get('profile', {}).get('test') is True and row.get('executable'):
            artifacts.append(Path(row['executable']).resolve(strict=True))
    if len(set(artifacts)) != 1:
        raise SystemExit('103.6.10: exact real_node compiler artifact missing or ambiguous')
    if source() != binding:
        raise SystemExit('103.6.10: source changed during build')
    files = [native / 'bin/layerxd', native / 'bin/layerx-genesis-build',
             native / 'tests/lxp_test_daemon_finality_authority', native / 'tests/lxp_test_finality_json',
             native / 'liblayerx.a', library, artifacts[0]]
    value = dict(binding, version=1, purpose='103.6.10-source-bound-build',
                 native_bin=str(native / 'bin'), real_node=str(artifacts[0]),
                 artifacts={str(path.resolve(strict=True)): digest(path) for path in files})
    with manifest.open('x') as stream:
        os.chmod(manifest, 0o600)
        json.dump(value, stream, sort_keys=True)
        stream.write('\n')
    print('103.6.10 build manifest=' + str(manifest))
else:
    metadata = manifest.stat()
    if not stat.S_ISREG(metadata.st_mode) or metadata.st_mode & 0o077 or metadata.st_uid != os.geteuid():
        raise SystemExit('103.6.10: private build manifest required')
    value = json.loads(manifest.read_text())
    binding = source()
    if value.get('version') != 1 or value.get('purpose') != '103.6.10-source-bound-build' \
            or any(value.get(name) != item for name, item in binding.items()):
        raise SystemExit('103.6.10: build manifest does not bind this exact source')
    native = directory / 'native'
    required = {str(path.resolve()) for path in (
        native / 'bin/layerxd', native / 'bin/layerx-genesis-build',
        native / 'tests/lxp_test_daemon_finality_authority', native / 'tests/lxp_test_finality_json',
        native / 'liblayerx.a', Path(value['real_node']))}
    if not required <= set(value.get('artifacts', {})) or len(value['artifacts']) != 7:
        raise SystemExit('103.6.10: required compiled artifacts missing')
    for name, expected in value['artifacts'].items():
        if digest(name) != expected:
            raise SystemExit('103.6.10: compiled artifact digest changed: ' + name)
    environment = dict(os.environ, LAYERX_TEST_NATIVE_BIN_DIR=str(native / 'bin'),
                       LAYERX_TEST_AUTHORITY_REAL_NODE_EXECUTABLE=value['real_node'])
    for pin in ('LAYERX_NODE_PAXEER_RPC_URL', 'LAYERX_NODE_PAXEER_RPC_ADDRESS',
                'LAYERX_NODE_PAXEER_RPC_PORT', 'LAYERX_NODE_PAXEER_CHAIN_ID',
                'LAYERX_NODE_SETTLEMENT_CONTRACT', 'LAYERX_NODE_CHECKPOINT_REGISTRY'):
        environment.pop(pin, None)
    run([str(native / 'tests/lxp_test_finality_json')], directory / 'finality-json.log', environment)
    run(['bash', 'tests/daemon/finality-authority-chain.sh',
         str(native / 'tests/lxp_test_daemon_finality_authority')], directory / 'finality-bind.log', environment)
    run(['bash', 'tests/daemon/bootstrap-send.sh'], directory / 'bootstrap-send.log', environment)
    bootstrap = (directory / 'bootstrap-send.log').read_text()
    summaries = re.findall(r'^test result: ok\. 1 passed; 0 failed; 0 ignored(?:;[^\n]*)?$',
                           bootstrap, re.MULTILINE)
    banners = ['bootstrap-send: genesis, replica, sequencer, signed SEND and receipt proofs; ' + mode + ' logs'
               for mode in ('absent', 'empty')]
    if len(summaries) != 2 or any(bootstrap.splitlines().count(banner) != 1 for banner in banners):
        raise SystemExit('103.6.10: both actual one-case bootstrap executions must pass without ignored cases')
    recordings = os.environ.get('PAXEER_X_FINALITY_AUTHORITY_RECORDINGS_MANIFEST', '')
    if not recordings:
        raise SystemExit('103.6.10: genuine PAXEER_X_FINALITY_AUTHORITY_RECORDINGS_MANIFEST required; historical finality is unqualified')
    run(['bash', 'tests/daemon/finality-authority-chain.sh', '--recordings', recordings,
         str(native / 'tests/lxp_test_daemon_finality_authority')], directory / 'finality-history.log', environment)
    history = json.loads((directory / 'finality-history.log').read_text())
    phases = history.get('phases', {})
    cases = history.get('cases', [])
    if set(phases) != {'before', 'after'} or not isinstance(cases, list) or len(set(cases)) != len(cases):
        raise SystemExit('103.6.10: actual historical case ledger missing')
    callback_cases = set(cases) - {'membership-change-recovery', 'daemon-restart'}
    if not callback_cases or not {'membership-change-recovery', 'daemon-restart'} <= set(cases):
        raise SystemExit('103.6.10: actual historical transition/restart ledger missing')
    historical_count = len(set(cases) - callback_cases)
    for phase in phases.values():
        observed = phase.get('cases', {})
        if set(observed) != callback_cases or any(passed is not True for passed in observed.values()):
            raise SystemExit('103.6.10: actual native historical case did not pass')
        historical_count += len(observed)
    print('103.6.10 verified actual finality callbacks and both fresh-log SEND/replica cases')
    print('PAXEER_X_GATE tests=%d skipped=0' % (2 + len(summaries) + historical_count))
PY
