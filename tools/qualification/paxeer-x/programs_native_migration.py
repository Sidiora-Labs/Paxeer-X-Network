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
    'include/layerx/lxp_kernel.h', 'include/layerx/programs.h',
    'src/protocol/lxp_kernel.c', 'src/modules/programs/fee.c',
    'src/modules/programs/fees.c',
    'include/layerx/lxp_governance.h', 'src/modules/governance/lxp_governance.c',
    'src/modules/programs/registration.c', 'cmd/layerxd/lxp_daemon_process.c',
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
    daemon_process = native / 'obj/cmd/layerxd/lxp_daemon_process.o'
    binary = native / 'tests/programs_lifecycle'
    binary.parent.mkdir(mode=0o700)
    jobs = int(os.environ.get(PREFIX + 'BUILD_JOBS', '4'))
    require(1 <= jobs <= 16, 'build jobs must be 1 through 16')
    compiler = shutil.which('cc')
    require(compiler, 'native compiler missing')
    commands = [
        ['make', '--no-print-directory', '-j' + str(jobs),
         'BUILD_DIR=' + str(native), 'LXP_REVISION=' + source['revision'],
         str(library), str(header), str(daemon_process)],
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
            'checkpoint_header': artifact(header),
            'daemon_process_object': artifact(daemon_process)}
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



ACCOUNTING_RESULTS = {'combined_fee_once': 0, 'trap_zero_fee': -3,
    'exhaustion_zero_fee': -601, 'signed_limit': -602, 'available_funds': -602,
    'frozen_payer': -602, 'wrong_asset': -402, 'checked_overflow': -500,
    'missing_profile': -101, 'recorded_profile_replay': 0,
    'restart_record_and_ledger': 0, 'duplicate_no_second_fee': -302,
    'downstream_rollback': -1002, 'historical_record_not_repriced': 0}
ACCOUNTING_STATUSES = {name: 0 for name in ACCOUNTING_RESULTS}
ACCOUNTING_STATUSES.update(available_funds=-602, duplicate_no_second_fee=-302, downstream_rollback=-1002)
ACCOUNTING_REQUIRED = {(abi, name) for abi in range(1, 5) for name in ACCOUNTING_RESULTS}
ACCOUNTING_FIELDS = {'name', 'abi', 'status', 'result', 'expected', 'fee_hi',
    'fee_lo', 'ledger_unchanged', 'program_unchanged', 'accounting_present',
    'root_before', 'root_after'}
LEGACY_SCHEMA = 'paxeer-x.native-migration-legacy-provenance.v1'
LEGACY_REVISION = '9e0e098bc429e6209a4e8a153001217fbcc84d6b'
LEGACY_SOURCE_HASHES = {
    'programs/crates/layerx-programs-runtime/src/execute.rs': '4b8d5eb31f162be09acabc5c3a429e1a3205a5dcb28d466a66281a0d3500ad19',
    'programs/crates/layerx-programs-runtime/src/ffi.rs': 'edd8741d230d673e10160754cfc9b434089056a9e89e3907f143fa7cf882b8c3',
    'programs/crates/layerx-programs-runtime/src/meter.rs': '2544a39745d0c04ff1875ec0e0d203775701526672cb181f1e357567109e2060'}


def validate_accounting(rows, evidence):
    require(len(rows) == len(ACCOUNTING_REQUIRED), 'missing or extra full-kernel accounting cases')
    indexed = {}
    for row in rows:
        require(isinstance(row, dict) and set(row) == ACCOUNTING_FIELDS,
                'invalid full-kernel accounting schema')
        key = (row['abi'], row['name'])
        require(type(row['abi']) is int and key in ACCOUNTING_REQUIRED and key not in indexed,
                'unexpected or duplicate full-kernel accounting case')
        indexed[key] = row
        require(all(type(row[field]) is int for field in ('status', 'result', 'expected'))
                and row['result'] == row['expected'] == ACCOUNTING_RESULTS[row['name']],
                'typed accounting outcome mismatch')
        require(row['status'] == ACCOUNTING_STATUSES[row['name']],
                'accounting refusal crossed the declared native owner boundary')
        require(all(type(row[field]) is int and 0 <= row[field] < 2**64
                    for field in ('fee_hi', 'fee_lo')), 'invalid checked integer accounting fee')
        require(all(type(row[field]) is bool for field in
                    ('ledger_unchanged', 'program_unchanged', 'accounting_present')),
                'missing accounting preservation predicates')
        for field in ('root_before', 'root_after'):
            require(isinstance(row[field], str) and re.fullmatch('[0-9a-f]{64}', row[field])
                    and int(row[field], 16), 'missing authenticated accounting root')
        if row['name'] in ('combined_fee_once', 'recorded_profile_replay'):
            require(row['status'] == 0 and (row['fee_hi'] or row['fee_lo'] > 1)
                    and not row['ledger_unchanged'] and not row['program_unchanged']
                    and row['accounting_present'] and row['root_before'] != row['root_after'],
                    'migration did not commit its exact combined fee and accounting')
        else:
            require(row['fee_hi'] == row['fee_lo'] == 0
                    and row['ledger_unchanged'] and row['program_unchanged'],
                    'failed, repeated or reopened migration charged or changed program state')
            expected_presence = row['name'] in ('restart_record_and_ledger', 'duplicate_no_second_fee',
                                                       'historical_record_not_repriced')
            require(row['accounting_present'] is expected_presence,
                    'unexpected migration accounting persistence')
        if row['name'] in ('restart_record_and_ledger', 'duplicate_no_second_fee',
                           'downstream_rollback', 'historical_record_not_repriced'):
            require(row['root_before'] == row['root_after'], 'reopen, duplicate or rollback changed root')
        if row['name'] in ('duplicate_no_second_fee', 'downstream_rollback'):
            require(row['status'] == row['result'], 'wrong duplicate or rollback kernel refusal')
    require(set(indexed) == ACCOUNTING_REQUIRED, 'incomplete full-kernel accounting inventory')
    for abi in range(1, 5):
        committed = indexed[(abi, 'combined_fee_once')]
        replay = indexed[(abi, 'recorded_profile_replay')]
        require({key: value for key, value in committed.items() if key != 'name'} ==
                {key: value for key, value in replay.items() if key != 'name'},
                'recorded accounting replay changed fee, roots or outcome')
        for name in ('restart_record_and_ledger', 'duplicate_no_second_fee'):
            require(indexed[(abi, name)]['root_after'] == committed['root_after'],
                    'reopened accounting record changed committed root')
        require(indexed[(abi, 'downstream_rollback')]['root_before'] == committed['root_before'],
                'downstream rollback failed to restore genuine prestate')
        fd = private_fd(evidence / f'accounting.abi{abi}.record.bin')
        with os.fdopen(fd, 'rb') as stream:
            record = stream.read(614)
        require(len(record) == 613 and record[:5] == b'LXMA1', 'missing canonical accounting record')
        fee_hi = int.from_bytes(record[597:605], 'big')
        fee_lo = int.from_bytes(record[605:613], 'big')
        require((fee_hi, fee_lo) == (committed['fee_hi'], committed['fee_lo']),
                'accounting record does not bind actual charged fee')
        runtime_fee = int.from_bytes(record[581:597], 'big')
        require(runtime_fee > 0 and (fee_hi << 64 | fee_lo) == runtime_fee + 1,
                'runtime and validation fees were not charged exactly once')


def legacy_provenance(package_path, provenance_path, pinned_revision, current_source):
    require(pinned_revision == LEGACY_REVISION and
            pinned_revision != current_source['revision'], 'trusted original source revision required')
    try:
        old_revision = subprocess.check_output(['git', 'rev-parse', pinned_revision + '^{commit}'],
            cwd=ROOT, stderr=subprocess.DEVNULL, text=True).strip()
    except subprocess.CalledProcessError as error:
        raise PrerequisiteMissing('trusted original source commit is unavailable') from error
    require(old_revision == pinned_revision, 'historical source pin does not identify exact commit')
    ancestry = subprocess.run(['git', 'merge-base', '--is-ancestor', pinned_revision,
                              current_source['revision']], cwd=ROOT,
        stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=60)
    require(ancestry.returncode == 0, 'legacy engine source is not an original ancestor')
    private_directory(Path(package_path).absolute().parent)
    private_directory(Path(provenance_path).absolute().parent)
    provenance = load_private(provenance_path)
    require(set(provenance) == {'schema', 'source', 'producer_root', 'commands',
        'working_directories', 'steps', 'artifacts', 'started_ns', 'completed_ns', 'exit_code'},
        'invalid legacy producer provenance fields')
    require(provenance['schema'] == LEGACY_SCHEMA and provenance['exit_code'] == 0
        and type(provenance['started_ns']) is int and type(provenance['completed_ns']) is int
        and provenance['completed_ns'] >= provenance['started_ns'],
        'legacy producer did not finish successfully')
    source = provenance['source']
    require(set(source) == {'revision', 'tree', 'task_source_sha256'} and
        source['revision'] == pinned_revision and
        source['tree'] == capture(['git', 'rev-parse', pinned_revision + '^{tree}']) and
        source['task_source_sha256'] == LEGACY_SOURCE_HASHES,
        'legacy producer is not bound to the original source')
    for path in LEGACY_SOURCE_HASHES:
        original = subprocess.check_output(['git', 'show', pinned_revision + ':' + path], cwd=ROOT)
        require(hashlib.sha256(original).hexdigest() == source['task_source_sha256'][path],
                'legacy producer source hash differs from original commit')
    producer_root = private_directory(provenance['producer_root'])
    require(producer_root != ROOT and subprocess.check_output(
        ['git', 'rev-parse', 'HEAD^{commit}'], cwd=producer_root, text=True).strip() == pinned_revision
        and not subprocess.check_output(['git', 'status', '--porcelain=v1',
                                         '--untracked-files=normal'], cwd=producer_root),
        'genuine clean original producer checkout required')
    artifacts = provenance['artifacts']
    require(set(artifacts) == {'legacy_fixture', 'producer', 'native_library', 'sandbox_library'},
            'legacy producer artifacts incomplete')
    for name, saved in artifacts.items():
        require(set(saved) == {'path', 'sha256', 'bytes'} and
                artifact(saved['path'], executable=name == 'producer') == saved,
                'legacy producer artifact changed')
    require(artifacts['legacy_fixture'] == artifact(package_path),
            'legacy package differs from genuine producer output')
    commands, steps, directories = (provenance[name] for name in
                                    ('commands', 'steps', 'working_directories'))
    require(isinstance(commands, list) and commands and len(commands) == len(steps) == len(directories),
            'legacy real producer steps absent')
    invoked = False
    for command, step, directory in zip(commands, steps, directories):
        require(isinstance(command, list) and command and all(isinstance(arg, str) for arg in command)
            and directory == str(producer_root) and set(step) == {'command', 'exit_code', 'log'}
            and step['command'] == command and type(step['exit_code']) is int and step['exit_code'] == 0
            and artifact(step['log']['path']) == step['log'], 'unverifiable original producer execution')
        invoked |= command[0] == artifacts['producer']['path']
    require(invoked, 'bound original engine producer was never executed')
    fd = private_fd(package_path)
    try:
        length = os.fstat(fd).st_size
        require(237 < length <= 16777216, 'bounded genuine legacy package required')
        header = os.pread(fd, 237, 0)
        require(len(header) == 237 and header[:5] == b'LXLF1'
                and header[5:45] == pinned_revision.encode('ascii'),
                'legacy binary package source pin differs')
        for name, offset in (('producer', 45), ('native_library', 77), ('sandbox_library', 109)):
            require(header[offset:offset + 32].hex() == artifacts[name]['sha256'],
                    'legacy package does not bind original engine artifact')
        sizes = [int.from_bytes(header[offset:offset + 4], 'big') for offset in (189, 193, 197, 201)]
        require(all(sizes) and sum(sizes) + 237 == length, 'legacy package component lengths invalid')
        return fd, provenance
    except BaseException:
        os.close(fd)
        raise


def verify(directory, manifest_path, authority_path, governor_path, legacy_path, legacy_manifest_path, legacy_revision):
    source = identity()
    private_directory(Path(manifest_path).absolute().parent)
    private_directory(Path(authority_path).absolute().parent)
    private_directory(Path(governor_path).absolute().parent)
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
                               'checkpoint_header', 'daemon_process_object'}, 'candidate artifacts missing')
    object_path = Path(artifacts['native_library']['path']).parent / 'obj/cmd/layerxd/lxp_daemon_process.o'
    require(artifacts['daemon_process_object']['path'] == str(object_path)
            and str(object_path) in manifest['commands'][0],
            'actual generic-owner daemon object compile is absent')
    with object_path.open('rb') as stream:
        require(stream.read(4) == b'\x7fELF', 'genuine compiled native process object required')
    for name, saved in artifacts.items():
        require(artifact(saved['path'], executable=name == 'programs_lifecycle') == saved,
                'candidate artifact changed')
    authority = private_fd(authority_path)
    governor = private_fd(governor_path)
    legacy = None
    try:
        legacy, provenance = legacy_provenance(legacy_path, legacy_manifest_path,
                                               legacy_revision, source)
        require(os.fstat(authority).st_size == 32, '32-byte provisioned authority seed required')
        require(os.fstat(governor).st_size == 32, '32-byte provisioned governance seed required')
        require(os.pread(authority, 32, 0) != os.pread(governor, 32, 0),
                'genesis signer and governance authority must be distinct')
        unshare = shutil.which('unshare')
        require(unshare, 'network isolation utility missing')
        isolation = artifact(Path(unshare).resolve(), executable=True)
        run = Path(tempfile.mkdtemp(prefix='native-migration-', dir=directory))
        evidence = run / 'native'
        evidence.mkdir(mode=0o700)
        command = [unshare, '--net', '--', artifacts['programs_lifecycle']['path'], '--native-migration']
        environment = {'PATH': os.defpath, 'LANG': 'C', 'LC_ALL': 'C',
            PREFIX + 'RUN': str(evidence), PREFIX + 'AUTHORITY_FD': str(authority),
            PREFIX + 'GOVERNOR_FD': str(governor),
            PREFIX + 'LEGACY_FIXTURE_FD': str(legacy),
            PREFIX + 'LEGACY_SOURCE_REVISION': LEGACY_REVISION}
        log = run / 'native.log'
        record = {'schema': 'paxeer-x.native-migration-result.v1', 'source': source,
            'command': COMMAND, 'process_command': command, 'manifest': artifact(manifest_path),
            'isolation_utility': isolation, 'exit_code': None, 'cases': [],
            'accounting_cases': [], 'legacy_cases': [],
            'legacy_provenance': artifact(legacy_manifest_path),
            'legacy_fixture': artifact(legacy_path), 'legacy_source': provenance['source'],
            'skipped': None,
            'evidence': [], 'log': None, 'status': 'unqualified', 'error': None}
        try:
            code = launch(command, environment, run, log, 1500, pass_fds=(authority, governor, legacy))
            record['exit_code'] = code
            require(log.stat().st_size <= 1048576, 'oversized native result')
            output = log.read_text(encoding='utf-8', errors='strict')
            rows = [json.loads(line[len('NATIVE_MIGRATION_CASE '):], object_pairs_hook=closed_object)
                    for line in output.splitlines() if line.startswith('NATIVE_MIGRATION_CASE ')]
            record['cases'] = rows
            require(code == 0, 'native migration process failed; log=' + str(log))
            validate_cases(rows)
            accounting_rows = [json.loads(line[len('NATIVE_MIGRATION_ACCOUNTING_CASE '):], object_pairs_hook=closed_object)
                for line in output.splitlines() if line.startswith('NATIVE_MIGRATION_ACCOUNTING_CASE ')]
            record['accounting_cases'] = accounting_rows
            validate_accounting(accounting_rows, evidence)
            accounting_summary = [json.loads(line[len('NATIVE_MIGRATION_ACCOUNTING_SUMMARY '):], object_pairs_hook=closed_object)
                for line in output.splitlines() if line.startswith('NATIVE_MIGRATION_ACCOUNTING_SUMMARY ')]
            require(accounting_summary == [{'cases': 56, 'skipped': 0}] and
                all(type(value) is int for value in accounting_summary[0].values()),
                'full-kernel accounting corpus skipped or incomplete')
            legacy_rows = [json.loads(line[len('NATIVE_MIGRATION_LEGACY_CASE '):], object_pairs_hook=closed_object)
                for line in output.splitlines() if line.startswith('NATIVE_MIGRATION_LEGACY_CASE ')]
            record['legacy_cases'] = legacy_rows
            require(len(legacy_rows) == 1 and set(legacy_rows[0]) == ACCOUNTING_FIELDS,
                'genuine original-source legacy replay evidence absent')
            legacy_row = legacy_rows[0]
            require(legacy_row['name'] == 'authenticated_legacy_replay' and
                type(legacy_row['abi']) is int and legacy_row['abi'] == 1 and
                all(type(legacy_row[field]) is int and legacy_row[field] == 0
                    for field in ('status', 'result', 'expected', 'fee_hi')) and
                type(legacy_row['fee_lo']) is int and legacy_row['fee_lo'] == 1 and
                all(legacy_row[field] is False for field in
                    ('ledger_unchanged', 'program_unchanged', 'accounting_present')) and
                all(isinstance(legacy_row[field], str) and re.fullmatch('[0-9a-f]{64}', legacy_row[field])
                    and int(legacy_row[field], 16) for field in ('root_before', 'root_after')) and
                legacy_row['root_before'] != legacy_row['root_after'],
                'legacy execution changed original ABI, fee or accounting semantics')
            legacy_summary = [json.loads(line[len('NATIVE_MIGRATION_LEGACY_SUMMARY '):], object_pairs_hook=closed_object)
                for line in output.splitlines() if line.startswith('NATIVE_MIGRATION_LEGACY_SUMMARY ')]
            require(legacy_summary == [{'cases': 1, 'skipped': 0}] and
                all(type(value) is int for value in legacy_summary[0].values()),
                'legacy producer was skipped or incomplete')
            for name, expected_rows in (('accounting.jsonl', accounting_rows), ('legacy.jsonl', legacy_rows)):
                fd = private_fd(evidence / name)
                with os.fdopen(fd) as stream:
                    saved = [json.loads(line, object_pairs_hook=closed_object) for line in stream]
                require(saved == expected_rows, 'persisted accounting or legacy results differ')
            require(artifact(evidence / 'legacy.verified-input.bin')['sha256'] ==
                provenance['artifacts']['legacy_fixture']['sha256'], 'legacy input substituted during replay')
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
            required_files |= {'accounting.jsonl', 'legacy.jsonl', 'legacy.verified-input.bin'}
            required_files |= {f'accounting.abi{abi}.{suffix}' for abi in range(1, 5)
                for suffix in ('prestate.snapshot.bin', 'receipt.bin', 'record.bin',
                               'committed.snapshot.bin', 'occupancy.bin')}
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
            legacy_check, checked_provenance = legacy_provenance(legacy_path, legacy_manifest_path,
                                                                 legacy_revision, source)
            os.close(legacy_check)
            require(checked_provenance == provenance, 'original producer provenance changed during replay')
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
        if legacy is not None:
            os.close(legacy)
        os.close(governor)
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
            legacy_path = setting('LEGACY_FIXTURE_FILE')
            legacy_manifest_path = setting('LEGACY_PROVENANCE')
            legacy_revision = os.environ.get(PREFIX + 'LEGACY_SOURCE_REVISION', LEGACY_REVISION)
            keys = []
            for role in ('AUTHORITY', 'GOVERNOR'):
                key_path = os.environ.get(PREFIX + role + '_FILE')
                if not key_path:
                    key_directory = Path(tempfile.mkdtemp(prefix=role.lower() + '-', dir=directory))
                    key_path = key_directory / 'seed'
                    fd = os.open(key_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
                    with os.fdopen(fd, 'wb') as stream:
                        remaining = 32
                        while remaining:
                            chunk = os.getrandom(remaining)
                            require(chunk, 'secure authority entropy unavailable')
                            stream.write(chunk)
                            remaining -= len(chunk)
                        stream.flush()
                        os.fsync(stream.fileno())
                keys.append(key_path)
            verify(directory, manifest_path, *keys, legacy_path, legacy_manifest_path, legacy_revision)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        if directory is None:
            directory = Path(tempfile.mkdtemp(prefix='native-migration-refusal-'))
        refusal = directory / ('refused-' + str(time.time_ns()) + '.json')
        code = 78 if isinstance(error, (PrerequisiteMissing, FileNotFoundError)) else 1
        write_private(refusal, {'schema': 'paxeer-x.native-migration-refusal.v1',
            'command': COMMAND + (' --build-artifacts' if args.build_artifacts else ''),
            'revision': capture(['git', 'rev-parse', 'HEAD^{commit}']),
            'exit_code': code, 'status': 'unqualified', 'error': str(error),
            'required_cases': [list(key) for key in sorted(REQUIRED)],
            'required_accounting_cases': [list(key) for key in sorted(ACCOUNTING_REQUIRED)],
            'legacy_source_revision': LEGACY_REVISION})
        print('native migration qualification refused; evidence=' + str(refusal), file=sys.stderr)
        return code
    return 0


if __name__ == '__main__':
    sys.exit(main())
