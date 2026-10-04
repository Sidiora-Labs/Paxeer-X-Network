#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import stat
import subprocess
import sys
import tempfile
import time

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
COMMAND = 'timeout 30m python3 tools/qualification/paxeer-x/programs_native_migration.py'
PREFIX = 'PAXEER_X_NATIVE_MIGRATION_'
SCHEMA = 'paxeer-x.native-migration-artifacts.v1'
SOURCE_PATHS = (
    'programs/crates/layerx-programs-runtime/src/ffi.rs',
    'programs/crates/layerx-programs-runtime/src/execute.rs',
    'programs/crates/layerx-programs-runtime/src/lifecycle.rs',
    'src/modules/programs/deploy.c', 'tests/programs/test_lifecycle.c',
    'tools/qualification/paxeer-x/programs_native_migration.py',
)
OPERATIONS = ('valid_import', 'unknown_abi', 'downgrade', 'wrong_code_hash',
              'wrong_prior_hash', 'unauthorized_upgrader', 'invalid_export',
              'trap', 'resource_exhaustion', 'incompatible_schedule', 'unknown_schedule')
REQUIRED = {(abi, phase, operation) for abi in range(1, 5)
            for phase in ('initial', 'snapshot_replay') for operation in OPERATIONS}
REQUIRED |= {(abi, 'restart', 'replay_upgrade') for abi in range(1, 5)}
COEFFICIENTS = [1, 1, 1, 1, 1, 8, 8, 64, 8]
RESULTS = {'valid_import': 0, 'unknown_abi': -101, 'downgrade': -101,
           'wrong_code_hash': -104, 'wrong_prior_hash': -213,
           'replay_upgrade': -213, 'unauthorized_upgrader': -204,
           'invalid_export': -106, 'trap': -3, 'resource_exhaustion': -601,
           'incompatible_schedule': -3, 'unknown_schedule': -3}
CASE_FIELDS = {'name', 'phase', 'abi', 'result', 'expected', 'dispatch_status',
               'recorded_abi', 'version', 'effects', 'staged', 'state_unchanged',
               'artifact_count', 'root_before', 'root_after', 'code_hash',
               'schedule_version', 'schedule_coefficients'}


class PrerequisiteMissing(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise ValueError(message)


def setting(name):
    value = os.environ.get(PREFIX + name)
    if not value:
        raise PrerequisiteMissing(PREFIX + name + ' is required')
    return value


def capture(command):
    result = subprocess.run(command, cwd=ROOT, stdin=subprocess.DEVNULL,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            text=True, timeout=60)
    require(result.returncode == 0, 'candidate identity command failed')
    return result.stdout.strip()


def checksum(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def artifact(path, executable=False):
    path = Path(path).absolute()
    require(path.resolve(strict=True) == path, 'artifact symlink refused')
    info = path.stat()
    require(stat.S_ISREG(info.st_mode) and info.st_size > 0,
            'nonempty regular artifact required')
    if executable:
        require(os.access(path, os.X_OK), 'candidate binary is not executable')
        with path.open('rb') as stream:
            require(stream.read(4) == b'\x7fELF', 'genuine native ELF required')
    return {'path': str(path), 'sha256': checksum(path), 'bytes': info.st_size}


def identity():
    require(not capture(['git', 'status', '--porcelain=v1', '--untracked-files=normal']),
            'qualification candidate must be clean')
    return {'revision': capture(['git', 'rev-parse', 'HEAD^{commit}']),
            'tree': capture(['git', 'rev-parse', 'HEAD^{tree}']),
            'task_source_sha256': {path: checksum(ROOT / path) for path in SOURCE_PATHS}}


def private_directory(path):
    path = Path(path).absolute()
    require(path.resolve(strict=True) == path, 'private directory symlink refused')
    info = path.stat()
    require(path != ROOT and ROOT not in path.parents and stat.S_ISDIR(info.st_mode)
            and info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o700,
            'caller-owned 0700 directory outside repository required')
    return path


def private_fd(path):
    path = Path(path).absolute()
    require(path.resolve(strict=True) == path, 'private file symlink refused')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1,
                'caller-owned singly linked 0600 file required')
        return fd
    except BaseException:
        os.close(fd)
        raise


def closed_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'duplicate JSON field')
        result[key] = value
    return result


def load_private(path):
    fd = private_fd(path)
    with os.fdopen(fd) as stream:
        require(os.fstat(stream.fileno()).st_size <= 1048576, 'oversized manifest')
        return json.load(stream, object_pairs_hook=closed_object)


def write_private(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())


def launch(command, environment, cwd, log, timeout, pass_fds=()):
    fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(('PROCESS_COMMAND ' + json.dumps(command) + '\n'
                      + 'PROCESS_CWD ' + str(cwd) + '\n').encode())
        stream.flush()
        process = subprocess.Popen(command, cwd=cwd, env=environment,
            stdin=subprocess.DEVNULL, stdout=stream, stderr=subprocess.STDOUT,
            pass_fds=pass_fds, start_new_session=True)
        try:
            code = process.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            code = 124
        finally:
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=5)
        stream.write(f'\nPROCESS_EXIT {code}\n'.encode())
        stream.flush()
        os.fsync(stream.fileno())
        return code


def build(directory):
    source = identity()
    require(not any(directory.iterdir()), 'build output directory must be empty')
    native = directory / 'native-build'
    cargo = Path(os.environ.get(PREFIX + 'TARGET_DIR', str(directory / 'programs-target'))).absolute()
    native.mkdir(mode=0o700)
    cargo.mkdir(mode=0o700, exist_ok=True)
    cargo_info = cargo.stat()
    require(cargo.resolve(strict=True) == cargo and cargo != ROOT and ROOT not in cargo.parents
            and stat.S_ISDIR(cargo_info.st_mode) and cargo_info.st_uid == os.geteuid()
            and not cargo_info.st_mode & 0o022, 'protected caller-owned build target required')
    library = native / 'liblayerx.a'
    header = native / 'generated/lxp_checkpoint_settlement.h'
    sandbox = cargo / 'debug/liblayerx_programs_sandbox.a'
    binary = native / 'tests/programs_lifecycle'
    binary.parent.mkdir(mode=0o700)
    jobs = int(os.environ.get(PREFIX + 'BUILD_JOBS', '4'))
    require(1 <= jobs <= 16, 'build jobs must be 1 through 16')
    compiler = shutil.which('cc')
    require(compiler, 'native compiler missing')
    commands = [
        ['make', '--no-print-directory', '-j' + str(jobs),
         'BUILD_DIR=' + str(native), 'LXP_REVISION=' + source['revision'],
         str(library), str(header)],
        ['cargo', 'build', '--locked', '--manifest-path', str(ROOT / 'programs/Cargo.toml'),
         '-p', 'layerx-programs-sandbox', '--features', 'host-ffi'],
        [compiler, '-Iinclude', '-I' + str(header.parent), '-std=c17', '-pedantic',
         '-Werror', '-Wall', '-Wextra', '-Wconversion', '-Wshadow', '-Wvla',
         '-fno-strict-aliasing', '-ffp-contract=off', '-O2',
         'tests/programs/test_lifecycle.c', '-Wl,--start-group', str(library),
         str(sandbox), '-Wl,--end-group', '-lssl', '-lcrypto', '-lsqlite3',
         '-pthread', '-ldl', '-lm', '-o', str(binary)],
    ]
    environment = dict(os.environ, CARGO_TARGET_DIR=str(cargo),
        CARGO_BUILD_JOBS=str(jobs), CARGO_INCREMENTAL='0',
        CARGO_PROFILE_DEV_DEBUG='0', PYTHONDONTWRITEBYTECODE='1')
    record = {'schema': SCHEMA, 'source': source, 'producer_root': str(ROOT),
              'commands': commands, 'working_directories': [str(ROOT), str(ROOT / 'programs'), str(ROOT)],
              'steps': [], 'artifacts': {}, 'started_ns': time.time_ns(),
              'completed_ns': None, 'exit_code': None}
    deadline = time.monotonic() + 1200
    try:
        for index, command in enumerate(commands):
            remaining = deadline - time.monotonic()
            require(remaining > 0, 'bounded build deadline exceeded')
            log = directory / f'build-{index + 1}.log'
            code = launch(command, environment, Path(record['working_directories'][index]),
                          log, remaining)
            record['steps'].append({'command': command, 'exit_code': code, 'log': artifact(log)})
            record['exit_code'] = code
            require(code == 0, 'candidate build failed; log=' + str(log))
        require(identity() == source, 'source changed during build')
        record['artifacts'] = {'programs_lifecycle': artifact(binary, executable=True),
            'native_library': artifact(library), 'sandbox_library': artifact(sandbox),
            'checkpoint_header': artifact(header)}
        record['completed_ns'] = time.time_ns()
    finally:
        write_private(directory / 'artifacts.json', record)
        print('EVIDENCE ' + str(directory / 'artifacts.json'), flush=True)


def validate_cases(rows):
    require(len(rows) == len(REQUIRED), 'missing or extra native migration cases')
    seen = set()
    for row in rows:
        require(isinstance(row, dict) and set(row) == CASE_FIELDS, 'invalid case schema')
        key = (row['abi'], row['phase'], row['name'])
        require(key in REQUIRED and key not in seen, 'unexpected or duplicate native case')
        seen.add(key)
        recorded = 2 if row['abi'] == 1 and row['name'] == 'downgrade' else row['abi']
        require(type(row['abi']) is int and type(row['recorded_abi']) is int
                and row['recorded_abi'] == recorded, 'recorded ABI mismatch')
        require(type(row['schedule_version']) is int and row['schedule_version'] == 1
                and row['schedule_coefficients'] == COEFFICIENTS
                and all(type(value) is int for value in row['schedule_coefficients']),
                'migration used incompatible or default metering')
        require(type(row['result']) is int and type(row['expected']) is int
                and row['result'] == row['expected'] == RESULTS[row['name']]
                and type(row['dispatch_status']) is int and row['dispatch_status'] == 0,
                'typed migration result/dispatch mismatch')
        for field in ('root_before', 'root_after', 'code_hash'):
            require(isinstance(row[field], str) and re.fullmatch('[0-9a-f]{64}', row[field])
                    and int(row[field], 16) != 0, 'missing native state/code evidence')
        require(type(row['state_unchanged']) is bool, 'state preservation evidence absent')
        require(all(type(row[field]) is int and row[field] >= 0
                    for field in ('version', 'effects', 'staged', 'artifact_count')),
                'invalid native accounting evidence')
        if row['name'] == 'valid_import':
            require(row['result'] == 0 and row['version'] == 2 and row['effects'] == 1
                    and row['staged'] >= 1 and row['artifact_count'] == 2
                    and not row['state_unchanged'] and row['root_before'] != row['root_after'],
                    'successful migration failed to commit exactly one version/effect')
        else:
            require(row['result'] != 0 and row['effects'] == 0 and row['state_unchanged']
                    and row['root_before'] == row['root_after']
                    and row['staged'] == 0 and row['artifact_count'] ==
                        (2 if row['phase'] == 'restart' else 1)
                    and row['version'] == (2 if row['phase'] == 'restart' else 1),
                    'refused migration altered prior state/version/effects')
    require(seen == REQUIRED, 'native migration inventory incomplete')
    indexed = {(row['abi'], row['phase'], row['name']): row for row in rows}
    for abi in range(1, 5):
        for operation in OPERATIONS:
            initial = {key: value for key, value in indexed[(abi, 'initial', operation)].items()
                       if key != 'phase'}
            replay = {key: value for key, value in indexed[(abi, 'snapshot_replay', operation)].items()
                      if key != 'phase'}
            require(initial == replay, 'snapshot replay changed recorded ABI or deterministic outcome')


def verify(directory, manifest_path, authority_path):
    source = identity()
    private_directory(Path(manifest_path).absolute().parent)
    private_directory(Path(authority_path).absolute().parent)
    manifest = load_private(manifest_path)
    require(set(manifest) == {'schema', 'source', 'producer_root', 'commands',
        'working_directories', 'steps', 'artifacts', 'started_ns', 'completed_ns', 'exit_code'},
        'invalid build manifest fields')
    require(manifest['schema'] == SCHEMA and manifest['source'] == source
            and manifest['producer_root'] == str(ROOT) and manifest['exit_code'] == 0
            and type(manifest['completed_ns']) is int
            and manifest['completed_ns'] >= manifest['started_ns'],
            'candidate build source/provenance missing')
    require(len(manifest['commands']) == 3 and len(manifest['steps']) == 3
            and manifest['working_directories'] == [str(ROOT), str(ROOT / 'programs'), str(ROOT)],
            'native build steps missing')
    for command, step in zip(manifest['commands'], manifest['steps']):
        require(set(step) == {'command', 'exit_code', 'log'}
                and step['command'] == command and step['exit_code'] == 0
                and artifact(step['log']['path']) == step['log'], 'unverifiable build step')
    artifacts = manifest['artifacts']
    require(set(artifacts) == {'programs_lifecycle', 'native_library', 'sandbox_library',
                               'checkpoint_header'}, 'candidate artifacts missing')
    for name, saved in artifacts.items():
        require(artifact(saved['path'], executable=name == 'programs_lifecycle') == saved,
                'candidate artifact changed')
    authority = private_fd(authority_path)
    try:
        require(os.fstat(authority).st_size == 32, '32-byte provisioned authority seed required')
        unshare = shutil.which('unshare')
        require(unshare, 'network isolation utility missing')
        isolation = artifact(Path(unshare).resolve(), executable=True)
        run = Path(tempfile.mkdtemp(prefix='native-migration-', dir=directory))
        evidence = run / 'native'
        evidence.mkdir(mode=0o700)
        command = [unshare, '--net', '--', artifacts['programs_lifecycle']['path'], '--native-migration']
        environment = {'PATH': os.defpath, 'LANG': 'C', 'LC_ALL': 'C',
            PREFIX + 'RUN': str(evidence), PREFIX + 'AUTHORITY_FD': str(authority)}
        log = run / 'native.log'
        record = {'schema': 'paxeer-x.native-migration-result.v1', 'source': source,
            'command': COMMAND, 'process_command': command, 'manifest': artifact(manifest_path),
            'isolation_utility': isolation, 'exit_code': None, 'cases': [], 'skipped': None,
            'evidence': [], 'log': None, 'status': 'unqualified', 'error': None}
        try:
            code = launch(command, environment, run, log, 1500, pass_fds=(authority,))
            record['exit_code'] = code
            require(log.stat().st_size <= 1048576, 'oversized native result')
            output = log.read_text(encoding='utf-8', errors='strict')
            rows = [json.loads(line[len('NATIVE_MIGRATION_CASE '):], object_pairs_hook=closed_object)
                    for line in output.splitlines() if line.startswith('NATIVE_MIGRATION_CASE ')]
            record['cases'] = rows
            require(code == 0, 'native migration process failed; log=' + str(log))
            validate_cases(rows)
            summaries = [json.loads(line[len('NATIVE_MIGRATION_SUMMARY '):], object_pairs_hook=closed_object)
                         for line in output.splitlines() if line.startswith('NATIVE_MIGRATION_SUMMARY ')]
            require(summaries == [{'cases': 92, 'skipped': 0, 'authority_provisioned': True,
                'incompatible_schedule_refused': True, 'snapshot_verified': True}],
                'native corpus skipped or lacks authority')
            require(type(summaries[0]['cases']) is int and type(summaries[0]['skipped']) is int
                    and all(summaries[0][field] is True for field in
                            ('authority_provisioned', 'incompatible_schedule_refused', 'snapshot_verified')),
                    'invalid native summary types')
            result_fd = private_fd(evidence / 'results.jsonl')
            with os.fdopen(result_fd) as stream:
                saved_rows = [json.loads(line, object_pairs_hook=closed_object) for line in stream]
            require(saved_rows == rows, 'persisted native results differ from process results')
            required_files = {'results.jsonl', 'signed-genesis.bin'}
            required_files |= {f'baseline.abi{abi}.{case}.snapshot.bin' for abi in range(1, 5) for case in OPERATIONS}
            required_files |= {f'committed.abi{abi}.snapshot.bin' for abi in range(1, 5)}
            required_files |= {f'receipt.abi{abi}.{phase}.{operation}.bin'
                               for abi, phase, operation in REQUIRED}
            require({path.name for path in evidence.iterdir()} == required_files,
                    'native genesis, baseline or committed snapshots absent')
            for path in sorted(evidence.iterdir()):
                fd = private_fd(path)
                os.close(fd)
                record['evidence'].append(artifact(path))
            require(identity() == source, 'candidate source changed during qualification')
            for name, saved in artifacts.items():
                require(artifact(saved['path'], executable=name == 'programs_lifecycle') == saved,
                        'candidate artifact changed during qualification')
            record['skipped'] = 0
            record['status'] = 'passed'
        except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
            record['error'] = str(error)
            raise
        finally:
            if log.exists():
                record['log'] = artifact(log)
            write_private(run / 'result.json', record)
            print('EVIDENCE ' + str(run / 'result.json'), flush=True)
    finally:
        os.close(authority)


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--build-artifacts', action='store_true')
    args = parser.parse_args()
    directory = None
    try:
        directory = private_directory(setting('EVIDENCE'))
        if args.build_artifacts:
            build(directory)
        else:
            manifest_path = setting('ARTIFACTS')
            authority_path = os.environ.get(PREFIX + 'AUTHORITY_FILE')
            if not authority_path:
                authority_directory = Path(tempfile.mkdtemp(prefix='authority-', dir=directory))
                authority_path = authority_directory / 'seed'
                fd = os.open(authority_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
                with os.fdopen(fd, 'wb') as stream:
                    remaining = 32
                    while remaining:
                        chunk = os.getrandom(remaining)
                        require(chunk, 'secure authority entropy unavailable')
                        stream.write(chunk)
                        remaining -= len(chunk)
                    stream.flush()
                    os.fsync(stream.fileno())
            verify(directory, manifest_path, authority_path)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        if directory is None:
            directory = Path(tempfile.mkdtemp(prefix='native-migration-refusal-'))
        refusal = directory / ('refused-' + str(time.time_ns()) + '.json')
        code = 78 if isinstance(error, (PrerequisiteMissing, FileNotFoundError)) else 1
        write_private(refusal, {'schema': 'paxeer-x.native-migration-refusal.v1',
            'command': COMMAND + (' --build-artifacts' if args.build_artifacts else ''),
            'revision': capture(['git', 'rev-parse', 'HEAD^{commit}']),
            'exit_code': code, 'status': 'unqualified', 'error': str(error),
            'required_cases': [list(key) for key in sorted(REQUIRED)]})
        print('native migration qualification refused; evidence=' + str(refusal), file=sys.stderr)
        return code
    return 0


if __name__ == '__main__':
    sys.exit(main())
