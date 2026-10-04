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
COMMAND = 'timeout 20m python3 tools/qualification/paxeer-x/programs_accounts_winddown.py --task 6.6'
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


def launch(command, log, environment, cwd=ROOT):
    print('COMMAND ' + json.dumps(command), flush=True)
    fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        process = subprocess.Popen(command, cwd=cwd, env=environment,
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


ACCOUNTS_SCHEMA = 'paxeer-x.accounts-winddown-artifacts.v1'
ACCOUNTS_TEST = 'lifecycle::account_upgrades::account_capable_abi_upgrades_preserve_proofs_and_authorized_winddown'
ACCOUNTS_REQUIRED = {'retained-abi2-3-4-proof-transfer-winddown',
    'unsupported-abi-refused', 'unauthorized-route-refused', 'wrong-asset-refused',
    'wrong-principal-refused', 'invalid-proof-refused', 'restart-exit-eligibility',
    'conservation'}
ACCOUNTS_COMMAND = 'timeout 30m python3 tools/qualification/paxeer-x/programs_accounts_winddown.py'
ACCOUNTS_GUEST = 'programs/sdk/rust/examples/escrow/target/wasm32-unknown-unknown/release/layerx_reference_escrow.wasm'
ACCOUNTS_SOURCE_PATHS = ('Makefile', 'src', 'include', 'cmd/layerxd',
    'cmd/layerx-genesis', 'programs/crates', 'programs/sdk/rust',
    'programs/Cargo.toml', 'programs/Cargo.lock', 'programs/.cargo',
    'agent/crates', 'agent/Cargo.toml', 'agent/Cargo.lock',
    'platform/hosted/agent-boundary', 'platform/Cargo.toml', 'platform/Cargo.lock',
    'contracts', 'foundry.toml', 'tests/bridge', 'platform/hosted/paxeer/evm.py',
    'rust-toolchain.toml',
    'tools/paxeer-x/build', 'tools/build', 'platform/Makefile.inc',
    'tools/qualification/paxeer-x/programs_accounts_winddown.py')


class MissingPrerequisite(ValueError):
    pass


def prerequisite(condition, message):
    if not condition:
        raise MissingPrerequisite(message)


def accounts_sources():
    prerequisite(not subprocess.check_output(
        ['git', 'status', '--porcelain', '--untracked-files=no'], cwd=ROOT),
        'clean immutable published candidate required')
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'],
                                       cwd=ROOT, text=True).strip()
    output = subprocess.check_output(['git', 'ls-files', '-z', '--',
                                     *ACCOUNTS_SOURCE_PATHS], cwd=ROOT)
    names = sorted(os.fsdecode(name) for name in output.split(b'\0') if name)
    selected = [name for name in names
        if not any(part.startswith('.env') for part in Path(name).parts)
        and (Path(name).suffix in {'.c', '.h', '.rs', '.toml', '.lock', '.json',
                                  '.mk', '.inc', '.py', '.sh', '.sol'} or name == 'Makefile')]
    prerequisite(all(name in selected for name in (
        'src/modules/programs/accounts.c', 'src/modules/programs/winddown.c',
        'src/modules/programs/deploy.c', 'src/modules/programs/call.c',
        'include/layerx/programs.h',
        'platform/hosted/agent-boundary/tests/real_node/lifecycle/account_upgrades.rs',
        'tests/bridge/deploy_local_custody.py', 'tests/bridge/custody_credit.py',
        'tests/bridge/sign_credit.c', 'tests/bridge/test_credit.c',
        'platform/hosted/paxeer/evm.py',
        'programs/sdk/rust/examples/escrow/src/lib.rs')),
        'complete native and real process producer sources required')
    native_inputs = {str(path.relative_to(ROOT))
        for directory in ('src', 'include', 'cmd/layerxd', 'cmd/layerx-genesis')
        for path in (ROOT / directory).rglob('*')
        if path.suffix in {'.c', '.h'} and path.is_file()}
    require(native_inputs <= set(selected), 'unbound native source input present')
    index = subprocess.check_output(['git', 'ls-files', '--stage', '-z', '--',
                                     *selected], cwd=ROOT)
    indexed = {}
    for entry in index.split(b'\0'):
        if entry:
            metadata, name = entry.split(b'\t', 1)
            mode, blob, stage = metadata.split()
            require(stage == b'0' and mode in (b'100644', b'100755'),
                    'regular resolved source required')
            indexed[os.fsdecode(name)] = blob.decode()
    hashed = subprocess.check_output(['git', 'hash-object', '--stdin-paths'],
        input=''.join(name + '\n' for name in selected), cwd=ROOT, text=True).splitlines()
    require(len(hashed) == len(selected) and
            dict(zip(selected, hashed)) == indexed, 'source differs from immutable candidate')
    bound = {}
    for name in selected:
        path = ROOT / name
        prerequisite(path.is_file() and not path.is_symlink(),
                     'candidate source unavailable: ' + name)
        bound[name] = digest(path)
    return revision, bound


def accounts_directory():
    path = Path(os.environ.get('PAXEER_X_ACCOUNTS_WINDDOWN_EVIDENCE',
        '/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task6.4')).absolute()
    prerequisite(path != ROOT and ROOT not in path.parents and not path.is_symlink(),
                 'private evidence outside candidate required')
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    path = path.resolve(strict=True)
    info = path.stat()
    prerequisite(path != ROOT and ROOT not in path.parents and
        info.st_uid == os.geteuid() and stat.S_ISDIR(info.st_mode)
        and not info.st_mode & 0o077, 'private caller-owned evidence required')
    return path


def accounts_artifact(path, magic=None, executable=False):
    path = Path(path).absolute()
    prerequisite(path.is_file() and not path.is_symlink(),
                 'genuine artifact missing: ' + str(path))
    if magic is not None:
        with path.open('rb') as stream:
            require(stream.read(len(magic)) == magic, 'wrong artifact type: ' + str(path))
    if executable:
        prerequisite(os.access(path, os.X_OK), 'artifact is not executable: ' + str(path))
    return artifact(path)


def accounts_tools():
    result = {}
    for name in ('anvil', 'forge', 'openssl', 'setpriv', 'python3'):
        path = shutil.which(name)
        prerequisite(path, 'real process prerequisite missing: ' + name)
        result[name] = artifact(path)
    for name, path in (('isolated-setpriv', '/usr/bin/setpriv'),
                       ('isolated-python3', '/usr/bin/python3')):
        result[name] = accounts_artifact(path, b'\x7fELF', True)
    for module in ('cryptography',):
        import importlib.util
        prerequisite(importlib.util.find_spec(module), 'custody producer module missing: ' + module)
    return result


def accounts_build(arguments, evidence):
    revision, before = accounts_sources()
    tools = accounts_tools()
    target = Path(os.environ.get('PAXEER_X_ACCOUNTS_WINDDOWN_RUNTIME_TARGET',
                                '/root/lx-target/task-6.4/runtime')).resolve()
    platform = Path(os.environ.get('PAXEER_X_ACCOUNTS_WINDDOWN_PLATFORM_TARGET',
                                  '/root/lx-target/task-6.4/platform')).resolve()
    prerequisite(target != platform and ROOT not in target.parents and ROOT not in platform.parents,
                 'distinct private runtime and platform target directories required')
    cargo = shlex.split(arguments.cargo)
    prerequisite(cargo, 'actual cargo command required')
    environment = dict(os.environ, CARGO_TARGET_DIR=str(target), CARGO_BUILD_JOBS='6')
    guest_environment = dict(environment, CARGO_TARGET_DIR=str(
        ROOT / 'programs/sdk/rust/examples/escrow/target'))
    launch([*cargo, 'build', '--locked', '--release',
            '--target', 'wasm32-unknown-unknown', '--manifest-path',
            str(ROOT / 'programs/sdk/rust/examples/escrow/Cargo.toml')],
           evidence / 'escrow-guest.log', guest_environment,
           ROOT / 'programs/sdk/rust/examples/escrow')
    guest = accounts_artifact(ROOT / ACCOUNTS_GUEST, b'\0asm\x01\0\0\0')
    launch([*cargo, 'build', '--locked', '--manifest-path',
            str(ROOT / 'programs/Cargo.toml'),
            '-p', 'layerx-programs-sandbox', '--lib', '--features', 'host-ffi'],
           evidence / 'runtime.log', environment, ROOT / 'programs')
    runtime = target / 'debug/liblayerx_programs_sandbox.a'
    accounts_artifact(runtime, b'!<arch>\n')
    native = ROOT / 'build'
    linker_flags = shlex.split(os.environ.get('EXTRA_LDFLAGS', ''))
    if '-lssl' not in linker_flags:
        linker_flags.append('-lssl')
    launch(['make', '-j6', '-o', 'programs-build', 'BUILD_DIR=build',
            'LXP_REVISION=' + revision,
            'EXTRA_LDFLAGS=' + shlex.join(linker_flags),
            'PAXEER_X_PROFILE2_RUNTIME_LIB=' + str(runtime),
            'PROGRAMS_RUNTIME_LIB=' + str(runtime), 'paxeer-x-profile2-native',
            'build/tests/bridge/sign-credit', 'build/tests/bridge/test-credit'],
           evidence / 'native.log', environment)
    environment['CARGO_TARGET_DIR'] = str(platform)
    output = launch([*cargo, 'test', '--locked', '--manifest-path', 'platform/Cargo.toml',
                    '-p', 'layerx-platform-agent-boundary', '--test', 'real_node',
                    '--no-run', '--message-format=json'],
                   evidence / 'process-fixture.log', environment)
    binaries = set()
    boundaries = set()
    for line in output.splitlines():
        if line.startswith('{'):
            event = json.loads(line)
            if (event.get('reason') == 'compiler-artifact' and event.get('executable')
                    and event.get('profile', {}).get('test') is True
                    and event.get('target', {}).get('name') == 'real_node'):
                binaries.add(event['executable'])
            if (event.get('reason') == 'compiler-artifact' and event.get('executable')
                    and event.get('target', {}).get('name') == 'layerx-agent-boundary'
                    and 'bin' in event.get('target', {}).get('kind', [])):
                boundaries.add(event['executable'])
    require(len(binaries) == 1 and len(boundaries) == 1,
            'exactly one real process fixture and boundary executable required')
    boundary = Path(boundaries.pop()).absolute()
    accounts_artifact(boundary, b'\x7fELF', True)
    os.chmod(boundary, 0o755)
    require(accounts_sources() == (revision, before), 'source changed during candidate build')
    require(accounts_artifact(ROOT / ACCOUNTS_GUEST, b'\0asm\x01\0\0\0') == guest,
            'provisioned guest changed during build')
    require(accounts_tools() == tools, 'real process tools changed during build')
    artifacts = {
        'layerxd': accounts_artifact(native / 'bin/layerxd', b'\x7fELF', True),
        'genesis-builder': accounts_artifact(native / 'bin/layerx-genesis-build', b'\x7fELF', True),
        'native-library': accounts_artifact(native / 'liblayerx.a', b'!<arch>\n'),
        'native-testing-library': accounts_artifact(native / 'liblayerx-testing.a', b'!<arch>\n'),
        'runtime-library': accounts_artifact(runtime, b'!<arch>\n'),
        'real-node-tests': accounts_artifact(binaries.pop(), b'\x7fELF', True),
        'agent-boundary': accounts_artifact(boundary, b'\x7fELF', True),
        'sign-credit': accounts_artifact(native / 'tests/bridge/sign-credit', b'\x7fELF', True),
        'test-credit': accounts_artifact(native / 'tests/bridge/test-credit', b'\x7fELF', True),
        'escrow-guest': guest}
    path = evidence / 'artifacts.json'
    write_private(path, {'schema': ACCOUNTS_SCHEMA, 'task': '6.4',
        'revision': revision, 'candidate_root': str(ROOT), 'sources': before,
        'test': ACCOUNTS_TEST, 'required_cases': sorted(ACCOUNTS_REQUIRED),
        'tools': tools, 'artifacts': artifacts})
    print('PAXEER_X_ACCOUNTS_WINDDOWN_MANIFEST=' + str(path), flush=True)


def accounts_json_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'duplicate candidate manifest field')
        result[key] = value
    return result


def accounts_manifest(arguments):
    raw = arguments.manifest or os.environ.get('PAXEER_X_ACCOUNTS_WINDDOWN_MANIFEST')
    prerequisite(raw, 'genuine task 6.4 candidate artifact manifest required')
    path = Path(raw).absolute()
    prerequisite(path.is_file() and not path.is_symlink(), 'private candidate manifest missing')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd) as stream:
        info = os.fstat(stream.fileno())
        prerequisite(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
            and not info.st_mode & 0o077 and info.st_nlink == 1
            and info.st_size <= 4_194_304,
            'bounded private caller-owned candidate manifest required')
        manifest = json.load(stream, object_pairs_hook=accounts_json_object)
    revision, bound = accounts_sources()
    require(set(manifest) == {'schema', 'task', 'revision', 'candidate_root', 'sources',
        'test', 'required_cases', 'tools', 'artifacts'} and
        manifest['schema'] == ACCOUNTS_SCHEMA and manifest['task'] == '6.4' and
        manifest['revision'] == revision and manifest['candidate_root'] == str(ROOT) and
        manifest['sources'] == bound and manifest['test'] == ACCOUNTS_TEST and
        manifest['required_cases'] == sorted(ACCOUNTS_REQUIRED),
        'manifest does not bind exact immutable task 6.4 source')
    require(manifest['tools'] == accounts_tools(), 'provisioned real tools changed')
    expected = {'layerxd': ROOT / 'build/bin/layerxd',
        'genesis-builder': ROOT / 'build/bin/layerx-genesis-build',
        'native-library': ROOT / 'build/liblayerx.a',
        'native-testing-library': ROOT / 'build/liblayerx-testing.a',
        'sign-credit': ROOT / 'build/tests/bridge/sign-credit',
        'test-credit': ROOT / 'build/tests/bridge/test-credit',
        'escrow-guest': ROOT / ACCOUNTS_GUEST}
    artifacts = manifest['artifacts']
    require(set(artifacts) == set(expected) | {'runtime-library', 'real-node-tests', 'agent-boundary'},
            'missing task 6.4 process artifact bindings')
    for name, saved in artifacts.items():
        require(set(saved) == {'path', 'sha256', 'bytes'}, 'invalid artifact binding')
        if name in expected:
            require(Path(saved['path']) == expected[name], 'wrong production consumer artifact path')
        if name == 'agent-boundary':
            require(Path(saved['path']).name == 'layerx-agent-boundary' and
                    Path(saved['path']).stat().st_mode & 0o777 == 0o755,
                    'genuine boundary executable must support its isolated UID')
        magic = (b'\0asm\x01\0\0\0' if name == 'escrow-guest' else
                 b'!<arch>\n' if name.endswith('library') else b'\x7fELF')
        require(accounts_artifact(saved['path'], magic,
                name not in {'escrow-guest', 'runtime-library', 'native-library',
                             'native-testing-library'}) == saved, 'candidate artifact changed')
    return manifest


def accounts_verify(arguments, evidence):
    prerequisite(os.geteuid() == 0, 'real process identity provisioning requires root')
    manifest = accounts_manifest(arguments)
    artifacts = manifest['artifacts']
    environment = dict(os.environ, PAXEER_X_PROFILE2_ACCOUNTS_EVIDENCE=str(evidence),
        PAXEER_X_ACCOUNTS_WINDDOWN_EVIDENCE=str(evidence),
        LAYERX_TEST_NATIVE_BIN_DIR=str(ROOT / 'build/bin'))
    output = launch([artifacts['real-node-tests']['path'], '--exact', ACCOUNTS_TEST,
                     '--nocapture', '--test-threads=1'], evidence / 'process.log', environment)
    retained = re.findall(r'^PROFILE2_ACCOUNT_CASE ([a-z0-9_-]+)$', output, re.M)
    require(len(retained) == len(set(retained)) and set(retained) == REQUIRED,
            'retained profile2 case evidence absent, duplicated or changed')
    cases = re.findall(r'^ACCOUNT_ABI_CASE ([a-z0-9_-]+)$', output, re.M)
    require(len(cases) == len(set(cases)) and set(cases) == ACCOUNTS_REQUIRED,
            'required account ABI cases absent, duplicated or changed')
    summaries = re.findall(r'^test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;', output, re.M)
    require(summaries == [('1', '0', '0')], 'real fixture failed, absent or skipped')
    prerequisite(list(evidence.glob('*.receipt')) and list(evidence.glob('*.state')),
                 'real signed receipts and authenticated state evidence missing')
    required_evidence = [
        'account-abi-unauthorized-route.admission.json',
        'account-abi-wrong-principal.admission.json',
        'account-abi-unsupported.admission.json',
        'account-abi-wrong-asset.admission.json',
        'account-abi-invalid-proof.admission.json',
        'account-abi-invalid-proof.LXPS2', 'account-abi-restart.state',
        'account-abi-final.state', 'account-abi-conservation.json']
    required_evidence.extend(f'account-abi-{label}.{suffix}'
        for label in ('initial', 'funded', 'final')
        for suffix in ('main-account', 'main-proof'))
    for name in required_evidence:
        accounts_artifact(evidence / name)
    require(accounts_manifest(arguments) == manifest, 'candidate changed during verification')
    return cases


def accounts_main(arguments):
    evidence = None
    result = {'task': '6.4', 'revision': None,
              'command': ('timeout 20m python3 ' + shlex.join(sys.argv)
                          if arguments.build else ACCOUNTS_COMMAND),
              'stage': 'build' if arguments.build else 'verify',
              'exit_code': None, 'cases': [], 'logs': [], 'error': None}
    code = 1
    try:
        base = accounts_directory()
        evidence = base / (result['stage'] + '-' + str(time.time_ns()))
        evidence.mkdir(mode=0o700)
        result['revision'] = subprocess.check_output(['git', 'rev-parse', 'HEAD'],
                                                    cwd=ROOT, text=True).strip()
        if arguments.build:
            accounts_build(arguments, evidence)
        else:
            result['cases'] = accounts_verify(arguments, evidence)
        code = 0
    except MissingPrerequisite as error:
        code = 78
        result['error'] = str(error)
    except subprocess.CalledProcessError as error:
        code = error.returncode
        result['error'] = str(error)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        result['error'] = str(error)
    finally:
        result['exit_code'] = code
        if evidence is not None:
            result['logs'] = [str(path) for path in sorted(evidence.glob('*.log'))]
            process_log = evidence / 'process.log'
            if process_log.is_file():
                output = process_log.read_text()
                result['cases'] = re.findall(r'^ACCOUNT_ABI_CASE ([a-z0-9_-]+)$', output, re.M)
                result['retained_cases'] = re.findall(r'^PROFILE2_ACCOUNT_CASE ([a-z0-9_-]+)$', output, re.M)
            write_private(evidence / 'result.json', result)
            print('EVIDENCE ' + str(evidence / 'result.json'), flush=True)
        if code:
            print('account ABI qualification refused: ' + str(result['error']), file=sys.stderr)
    return code


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--task', choices=('6.4', '6.6'), default='6.4')
    parser.add_argument('--build', action='store_true')
    parser.add_argument('--build-dir', default='/root/lx-target/profile2-accounts')
    parser.add_argument('--cargo', default='cargo')
    parser.add_argument('--manifest')
    arguments = parser.parse_args()
    try:
        if arguments.task == '6.4':
            return accounts_main(arguments)
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
