#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
SCHEMA = 'paxeer-x.program-interface-artifacts.v1'
NATIVE_CASES = {
    'abi1', 'abi2', 'abi2-dynamic', 'abi3', 'abi3-dynamic', 'abi4',
    'abi4-dynamic', 'upgrade-widening', 'upgrade-narrowing',
    'upgrade-breaking', 'upgrade-downgrade',
}
PROTOCOL_CASES = {
    'canonical_abi_domains_and_legacy_vectors',
    'exact_capabilities_exports_and_upgrade_policy',
    'canonical_encoding_refuses_malformed_bounds',
    'native_receipt_authorized_interface_reads',
}
LEGACY_CASES = {
    'interface::conformance_vectors::' + name for name in (
        'dynamic_descriptor_uses_v2_and_refuses_old_shapes',
        'canonical_interface_vector_round_trips_and_binds_real_export',
        'binding_refuses_a_declared_entry_absent_from_the_real_module',
        'binding_refuses_entry_that_cannot_accept_discriminator_calldata',
        'upgrade_widening_and_explicit_breaking_declaration_are_distinct',
        'schema_vector_uses_the_frozen_calldata_conventions_and_type_tags',
    )
}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def capture(command):
    return subprocess.check_output(command, cwd=ROOT, text=True, timeout=60).strip()


def identity():
    require(not capture(['git', 'status', '--porcelain=v1', '--untracked-files=normal']),
            'candidate source is dirty')
    return {'revision': capture(['git', 'rev-parse', 'HEAD^{commit}']),
            'tree': capture(['git', 'rev-parse', 'HEAD^{tree}']), 'dirty': False}


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def artifact(path):
    path = Path(path).resolve(strict=True)
    info = path.stat()
    require(stat.S_ISREG(info.st_mode) and info.st_size > 0,
            'missing or empty artifact: ' + str(path))
    return {'path': str(path), 'sha256': digest(path), 'bytes': info.st_size}


def private_directory(path, create=False):
    path = Path(path).absolute()
    require(not path.is_symlink(), 'private directory must not be a symlink')
    if create:
        path.mkdir(mode=0o700, parents=True, exist_ok=True)
    path = path.resolve(strict=True)
    info = path.stat()
    require(path != ROOT and ROOT not in path.parents,
            'artifact directory must be outside the repository')
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid()
            and not info.st_mode & 0o077, 'private caller-owned directory required')
    return path


def write_private(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())


def load_private(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd) as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                and not info.st_mode & 0o077, 'private caller-owned manifest required')
        return json.load(stream)


def build_step(command, log, environment, working_directory):
    print('BUILD ' + json.dumps(command), flush=True)
    fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(('BUILD_COMMAND ' + json.dumps(command) + '\n'
                      + 'BUILD_CWD ' + str(working_directory) + '\n').encode())
        stream.flush()
        result = subprocess.run(command, cwd=working_directory, env=environment,
                                stdin=subprocess.DEVNULL, stdout=stream,
                                stderr=subprocess.STDOUT, timeout=1800)
        stream.write(f'\nBUILD_EXIT {result.returncode}\n'.encode())
        stream.flush()
    require(result.returncode == 0,
            f'build command exited {result.returncode}; log={log}')
    print('BUILD_LOG ' + str(log), flush=True)


def rust_executables(log):
    found = {}
    finished = False
    for line in log.read_text().splitlines():
        if not line.startswith('{'):
            continue
        event = json.loads(line)
        if event.get('reason') == 'build-finished':
            finished = event.get('success') is True
        if (event.get('reason') == 'compiler-artifact'
                and event.get('profile', {}).get('test') is True
                and event.get('executable')):
            name = event.get('target', {}).get('name')
            if name in ('layerx_programs', 'interface_protocol'):
                require(name not in found, 'duplicate Rust test executable')
                require(not event.get('features'), 'unexpected registry test features')
                found[name] = Path(event['executable'])
    require(finished and set(found) == {'layerx_programs', 'interface_protocol'},
            'missing successfully compiled registry test executables')
    return found


def build(output):
    source = identity()
    directory = private_directory(output, create=True)
    require(not any(directory.iterdir()), 'build output must be a fresh empty directory')
    native_dir = directory / 'native-build'
    cargo_dir = Path(os.environ.get('CARGO_TARGET_DIR', str(directory / 'cargo-target'))).absolute()
    environment = dict(os.environ, CARGO_TARGET_DIR=str(cargo_dir),
                       CARGO_PROFILE_DEV_DEBUG='0', CARGO_PROFILE_TEST_DEBUG='0',
                       CARGO_INCREMENTAL='0', PYTHONDONTWRITEBYTECODE='1')
    jobs = int(os.environ.get('PAXEER_X_INTERFACE_BUILD_JOBS', '4'))
    require(1 <= jobs <= 16, 'build jobs must be between 1 and 16')
    environment['CARGO_BUILD_JOBS'] = str(jobs)
    cargo = ['cargo', '+1.91.1']
    library = native_dir / 'liblayerx.a'
    header = native_dir / 'generated/lxp_checkpoint_settlement.h'
    staticlib = cargo_dir / 'debug/liblayerx_programs_sandbox.a'
    native = directory / 'test_interface_protocol'
    compile_command = [
        'cc', '-Iinclude', '-I' + str(header.parent),
        '-std=c17', '-pedantic', '-Werror', '-Wall', '-Wextra', '-Wconversion',
        '-Wshadow', '-Wvla', '-fno-strict-aliasing', '-ffp-contract=off', '-O2',
        'tests/programs/test_interface_protocol.c', '-Wl,--start-group',
        str(library), str(staticlib), '-Wl,--end-group',
        '-lcrypto', '-lsqlite3', '-pthread', '-ldl', '-lm', '-o', str(native),
    ]
    commands = [
        ['make', '--no-print-directory', '-j' + str(jobs),
         'BUILD_DIR=' + str(native_dir), 'LXP_REVISION=' + source['revision'],
         str(library), str(header)],
        cargo + ['build', '--locked', '--manifest-path', str(ROOT / 'programs/Cargo.toml'),
                 '-p', 'layerx-programs-sandbox', '--features', 'host-ffi'],
        compile_command,
        cargo + ['test', '--locked', '--manifest-path', str(ROOT / 'programs/Cargo.toml'),
                 '-p', 'layerx-programs-registry', '--lib', '--test', 'interface_protocol',
                 '--no-run', '--message-format=json'],
    ]
    working_directories = [ROOT, ROOT / 'programs', ROOT, ROOT / 'programs']
    record = {'schema': SCHEMA, 'source': source, 'producer_root': str(ROOT),
              'commands': commands,
              'working_directories': [str(path) for path in working_directories],
              'profiles': {'dev_debug': 0, 'test_debug': 0, 'incremental': False},
              'features': {'sandbox': ['host-ffi'], 'registry-tests': []},
              'toolchains': {'cargo': capture(cargo + ['--version']),
                             'rustc': capture(['rustc', '+1.91.1', '-vV']),
                             'cc': capture(['cc', '--version'])},
              'required_native_cases': sorted(NATIVE_CASES),
              'required_protocol_cases': sorted(PROTOCOL_CASES),
              'required_legacy_cases': sorted(LEGACY_CASES)}
    write_private(directory / 'build-inputs.json', record)
    logs = []
    for index, command in enumerate(commands):
        log = directory / f'build-{index + 1}.log'
        build_step(command, log, environment, working_directories[index])
        logs.append(log)
    binaries = rust_executables(logs[-1])
    for name, path in binaries.items():
        target = directory / name
        shutil.copyfile(path, target)
        target.chmod(0o700)
    require(identity() == source, 'source changed during build')
    record['artifacts'] = {
        'native': artifact(native), 'native-library': artifact(library),
        'sandbox-staticlib': artifact(staticlib), 'generated-header': artifact(header),
        'protocol-tests': artifact(directory / 'interface_protocol'),
        'legacy-tests': artifact(directory / 'layerx_programs'),
    }
    record['logs'] = [artifact(log) for log in logs]
    write_private(directory / 'manifest.json', record)
    print('INTERFACE_BUILD_MANIFEST ' + str(directory / 'manifest.json'))


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    try:
        build(args.output)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print(f'interface build refused: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
