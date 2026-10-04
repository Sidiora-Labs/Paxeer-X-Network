#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import signal
import stat
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[3]
SCHEMA = 'paxeer-x.profile2-accounts-artifacts.v1'
TEST = 'lifecycle::account_upgrades::funded_account_profile2_survives_abi_upgrades_and_winddown'
COMMAND = 'timeout 20m python3 tools/qualification/paxeer-x/programs_accounts_winddown.py'
REQUIRED = {'deploy-abi2', 'owner-opt-in-profile2', 'fund-abi2', 'upgrade-abi3',
            'additional-account-abi3', 'fund-abi3', 'upgrade-abi4',
            'additional-account-abi4', 'fund-abi4', 'missing-transfer-capability',
            'native-restart', 'route', 'deprecate', 'exit',
            'route-abi3', 'route-abi4', 'exit-abi3', 'exit-abi4', 'tombstone',
            'proof-corruption-refused', 'stale-head-refused'}
DECLARED = ('tools/paxeer-x/build/6.6.mk',
            'tools/qualification/paxeer-x/programs_accounts_winddown.py',
            'platform/hosted/agent-boundary/tests/real_node/lifecycle/account_upgrades.rs')


def require(condition, message):
    if not condition:
        raise ValueError(message)


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def private_directory():
    path = Path(os.environ.get('PAXEER_X_PROFILE2_ACCOUNTS_EVIDENCE',
        '/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task6.6')).absolute()
    require(not path.is_symlink(), 'private evidence directory must not be a symlink')
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    path = path.resolve(strict=True)
    info = path.stat()
    require(path != ROOT and ROOT not in path.parents and
            info.st_uid == os.geteuid() and not info.st_mode & 0o077,
            'caller-owned private evidence outside checkout required')
    return path


def sources():
    output = subprocess.check_output(['git', 'ls-files', '-z', '--',
        'Makefile', 'src', 'include', 'cmd/layerxd', 'cmd/layerx-genesis',
        'programs/crates', 'programs/sdk/rust', 'programs/Cargo.toml',
        'programs/Cargo.lock', 'agent/crates', 'agent/Cargo.toml', 'agent/Cargo.lock',
        'platform/hosted/agent-boundary', 'platform/Cargo.toml', 'platform/Cargo.lock',
        'contracts/config', 'rust-toolchain.toml'], cwd=ROOT)
    names = {os.fsdecode(name) for name in output.split(b'\0') if name}
    names.update(DECLARED)
    return {name: digest(ROOT / name) for name in sorted(names)
            if Path(name).suffix in {'.c', '.h', '.rs', '.toml', '.lock', '.json', '.mk', '.py'}
            or name == 'Makefile'}


def artifact(path):
    path = Path(path).resolve(strict=True)
    require(path.is_file() and path.stat().st_size > 0, 'missing artifact: ' + str(path))
    return {'path': str(path), 'sha256': digest(path), 'bytes': path.stat().st_size}


def write_private(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())


def launch(command, log, environment):
    print('COMMAND ' + json.dumps(command), flush=True)
    fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        process = subprocess.Popen(command, cwd=ROOT, env=environment,
            stdin=subprocess.DEVNULL, stdout=stream, stderr=subprocess.STDOUT,
            start_new_session=True)
        try:
            code = process.wait(timeout=1100)
        except subprocess.TimeoutExpired:
            code = 124
        finally:
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=10)
    print(f'EXIT {code} LOG {log}', flush=True)
    if code:
        raise subprocess.CalledProcessError(code, command)
    return log.read_text()


def build(arguments, evidence):
    before = sources()
    directory = Path(arguments.build_dir).resolve()
    native = directory / 'native'
    target = Path(os.environ.get('PAXEER_X_PROFILE2_RUST_TARGET',
                               '/root/lx-target/arbiter-prestate/rust'))
    cargo = shlex.split(arguments.cargo)
    stamp = str(time.time_ns())
    environment = dict(os.environ, CARGO_TARGET_DIR=str(target), CARGO_BUILD_JOBS='6')
    launch([*cargo, 'build', '--locked', '--manifest-path', 'programs/Cargo.toml',
            '-p', 'layerx-programs-sandbox', '--lib', '--features', 'host-ffi'],
           evidence / ('build-runtime-' + stamp + '.log'), environment)
    runtime = target / 'debug/liblayerx_programs_sandbox.a'
    launch(['make', '-j6', 'BUILD_DIR=' + str(native),
            'PAXEER_X_PROFILE2_RUNTIME_LIB=' + str(runtime), 'paxeer-x-profile2-native'],
           evidence / ('build-native-' + stamp + '.log'), environment)
    environment['CARGO_TARGET_DIR'] = os.environ.get('PAXEER_X_PROFILE2_PLATFORM_TARGET',
                                                   '/root/lx-target/platform')
    output = launch([*cargo, 'test', '--locked', '--manifest-path', 'platform/Cargo.toml',
                    '-p', 'layerx-platform-agent-boundary', '--test', 'real_node',
                    '--no-run', '--message-format=json'],
                   evidence / ('build-process-fixture-' + stamp + '.log'), environment)
    binaries = set()
    for line in output.splitlines():
        if line.startswith('{'):
            event = json.loads(line)
            if (event.get('reason') == 'compiler-artifact' and event.get('executable')
                    and event.get('profile', {}).get('test') is True
                    and event.get('target', {}).get('name') == 'real_node'):
                binaries.add(event['executable'])
    require(len(binaries) == 1, 'exactly one successfully compiled real-process fixture required')
    require(sources() == before, 'source changed during qualification build')
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    manifest = evidence / ('artifacts-' + stamp + '.json')
    write_private(manifest, {'schema': SCHEMA, 'task': '6.6', 'revision': revision,
        'sources': before, 'test': TEST, 'artifacts': {
            'layerxd': artifact(native / 'bin/layerxd'),
            'genesis-builder': artifact(native / 'bin/layerx-genesis-build'),
            'native-library': artifact(native / 'liblayerx.a'),
            'runtime-library': artifact(runtime), 'real-node-tests': artifact(binaries.pop())}})
    print('PAXEER_X_PROFILE2_ACCOUNTS_MANIFEST=' + str(manifest), flush=True)


def verify(arguments, evidence):
    raw = arguments.manifest or os.environ.get('PAXEER_X_PROFILE2_ACCOUNTS_MANIFEST')
    require(raw, 'genuine candidate artifact manifest is required')
    path = Path(raw).absolute()
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd) as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                and not info.st_mode & 0o077, 'private caller-owned manifest required')
        manifest = json.load(stream)
    require(manifest['schema'] == SCHEMA and manifest['task'] == '6.6'
            and manifest['test'] == TEST and manifest['sources'] == sources(),
            'candidate artifacts do not bind the complete current source')
    artifacts = manifest['artifacts']
    require(set(artifacts) == {'layerxd', 'genesis-builder', 'native-library',
                             'runtime-library', 'real-node-tests'}, 'incomplete artifacts')
    for saved in artifacts.values():
        require(artifact(saved['path']) == saved, 'candidate artifact changed')
    require(os.geteuid() == 0, 'real process fixture requires root identity provisioning')
    for binary in ('anvil', 'forge', 'openssl', 'setpriv', 'python3'):
        require(shutil.which(binary), 'real fixture prerequisite missing: ' + binary)
    for name in ('sign-credit', 'test-credit'):
        require((ROOT / 'build/tests/bridge' / name).is_file(),
                'genuine custody provisioning executable missing: ' + name)
    guest = ROOT / 'programs/sdk/rust/examples/escrow/target/wasm32-unknown-unknown/release/layerx_reference_escrow.wasm'
    require(guest.is_file() and guest.read_bytes()[:8] == b'\0asm\x01\0\0\0',
            'parent-built genuine escrow guest required')
    run = evidence / ('verify-' + str(time.time_ns()))
    run.mkdir(mode=0o700)
    environment = dict(os.environ, PAXEER_X_PROFILE2_ACCOUNTS_EVIDENCE=str(run),
        LAYERX_TEST_NATIVE_BIN_DIR=str(Path(artifacts['layerxd']['path']).parent))
    result = {'revision': manifest['revision'], 'command': COMMAND,
              'exit_code': None, 'cases': [], 'log': str(run / 'process.log')}
    try:
        output = launch([artifacts['real-node-tests']['path'], '--exact', TEST,
                         '--nocapture', '--test-threads=1'], run / 'process.log', environment)
        cases = re.findall(r'^PROFILE2_ACCOUNT_CASE ([a-z0-9_-]+)$', output, re.M)
        result['cases'] = cases
        require(len(cases) == len(set(cases)) and set(cases) == REQUIRED,
                'missing, changed or duplicated required case evidence')
        summaries = re.findall(r'^test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;', output, re.M)
        require(summaries == [('1', '0', '0')], 'fixture failed, absent or skipped')
        require(list(run.glob('*.receipt')) and list(run.glob('*.state')),
                'genuine receipts and authenticated state evidence required')
        require(sources() == manifest['sources'], 'source changed during verification')
        for saved in artifacts.values():
            require(artifact(saved['path']) == saved, 'process artifact changed during verification')
        result['exit_code'] = 0
    except subprocess.CalledProcessError as error:
        result['exit_code'] = error.returncode
        raise
    except (OSError, ValueError, KeyError, TypeError) as error:
        result['exit_code'] = 1
        raise
    finally:
        write_private(run / 'result.json', result)
        print('EVIDENCE ' + str(run / 'result.json'), flush=True)


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--build', action='store_true')
    parser.add_argument('--build-dir', default='/root/lx-target/profile2-accounts')
    parser.add_argument('--cargo', default='cargo')
    parser.add_argument('--manifest')
    arguments = parser.parse_args()
    try:
        evidence = private_directory()
        if arguments.build:
            build(arguments, evidence)
        else:
            verify(arguments, evidence)
    except subprocess.CalledProcessError as error:
        print('profile2 account qualification refused: ' + str(error), file=sys.stderr)
        return error.returncode
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print('profile2 account qualification refused: ' + str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
