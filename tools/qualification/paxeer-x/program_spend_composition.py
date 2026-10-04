#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import signal
import stat
import subprocess
import sys
import time

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
TASK = '104.30.4'
SCHEMA = 'paxeer-x.program-spend-composition-build.v1'
MAKEFILE = 'tools/qualification/paxeer-x/program_spend_composition.mk'
NATIVE_CASES = ['owner_transfer_and_narrowed_edge', 'amount_escalation_atomic',
    'asset_escalation_atomic', 'destination_escalation_atomic', 'repeated_visits_owner_fee_once',
    'depth_escalation_atomic', 'fanout_escalation_atomic', 'repeated_late_escalation_atomic']
RUST_CASES = {
    'layerx_programs_runtime': [
        'abi::capability::tests::frozen_v1_encoding_remains_exact_and_refuses_the_v2_tag',
        'abi::capability::tests::v2_decoder_rejects_unknown_noncanonical_and_unbound_grants',
        'abi::capability::tests::program_spend_narrows_exact_identity_and_amount_across_every_edge',
        'abi::capability::tests::program_spend_handoff_requires_the_exact_owner_frame_and_aggregate_limit',
        'abi::capability::tests::inherited_escalation_never_stages_a_descendant_edge_or_transfer',
        'abi::capability::tests::owner_origin_cannot_replace_an_inherited_program_spend_limit',
        'abi::capability::tests::owner_escalation_never_stages_an_edge_or_transfer'],
    'program_spend_composition': [
        'canonical_program_grants_narrow_at_every_depth_and_repeated_visit',
        'fanout_does_not_merge_distinct_principal_or_program_authority',
        'encoded_program_grants_never_change_legacy_or_accept_unknown_tags',
        'actual_guest_narrowing_preserves_owner_leg_and_repeated_visits',
        'actual_guest_escalation_rolls_back_the_preceding_owner_leg',
        'actual_native_failure_terminal_retains_class_reason_and_rejecting_frame'],
    'isolation': ['capability_narrowing_rejects_missing_grants_and_limit_widening_without_effects',
        'program_spend_capability_binds_owner_seed_account_asset_and_destination'],
    'monetary_law': ['candidate_program_transfer_host_issues_exact_owner_frame_authority',
        'real_wasm_program_leg_from_underivable_account_is_refused',
        'real_wasm_program_leg_staged_by_callee_frame_is_refused',
        'real_wasm_cumulative_program_legs_refuse_one_past_grant_atomically'],
}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def capture(argv):
    result = subprocess.run(argv, cwd=ROOT, check=True, stdin=subprocess.DEVNULL,
        capture_output=True, text=True, timeout=60)
    return result.stdout.strip()


def source():
    require(not capture(['git', 'status', '--porcelain=v1', '--untracked-files=normal']),
        'source candidate must be clean')
    paths = capture(['git', 'ls-files', '--', 'src', 'include', 'programs/crates',
        'programs/sdk/rust', 'programs/.cargo', 'programs/vendor', 'agent/crates',
        'agent/Cargo.lock', 'agent/Cargo.toml', 'programs/Cargo.lock', 'programs/Cargo.toml',
        'Makefile', 'platform/Makefile.inc', 'tools/build/sanitizers.mk',
        'rust-toolchain.toml', 'contracts/config/checkpoint-settlement.json',
        'tests/programs/test_call_activity.c', 'tests/programs/test_spend_capability_composition.c',
        MAKEFILE, 'tools/paxeer-x/gates/104.30.4.sh',
        'tools/qualification/paxeer-x/program_spend_composition.py']).splitlines()
    paths = [p for p in paths if '.env' not in Path(p).name]
    return {'revision': capture(['git', 'rev-parse', 'HEAD^{commit}']),
        'tree': capture(['git', 'rev-parse', 'HEAD^{tree}']),
        'inputs': {p: digest(ROOT / p) for p in sorted(paths)}}


def private_directory():
    raw = os.environ.get('PAXEER_X_EVIDENCE_DIR')
    require(raw, 'PAXEER_X_EVIDENCE_DIR is required')
    path = Path(raw).absolute()
    info = path.stat()
    require(path.resolve(strict=True) == path and ROOT != path and ROOT not in path.parents
        and stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid()
        and stat.S_IMODE(info.st_mode) == 0o700, 'private caller-owned 0700 evidence directory required')
    return path


def write_private(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write('\n'); stream.flush(); os.fsync(stream.fileno())


def artifact(path):
    path = Path(path).absolute()
    require(path.resolve(strict=True) == path and path.is_file() and path.stat().st_size > 0,
        'nonempty source-bound regular artifact required')
    return {'path': str(path), 'sha256': digest(path), 'bytes': path.stat().st_size}


def checked(saved, elf=False):
    require(artifact(saved['path']) == saved, 'prebuilt artifact changed')
    path = Path(saved['path'])
    if elf:
        with path.open('rb') as stream:
            require(stream.read(4) == b'\x7fELF' and os.access(path, os.X_OK), 'genuine ELF required')
    return path


class GateProcessError(ValueError):
    def __init__(self, step):
        self.step = step
        super().__init__('process failed: exit=' + str(step['exit_code']) + ' log=' + step['log']['path'])


def launch(argv, directory, environment, deadline, name):
    remaining = deadline - time.monotonic()
    require(remaining > 0, '30-minute bound exhausted')
    log = directory / (name + '.log')
    fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(('COMMAND ' + json.dumps(argv) + '\nCWD ' + str(ROOT) + '\n').encode())
        stream.flush()
        try:
            process = subprocess.Popen(argv, cwd=ROOT, env=environment, start_new_session=True,
                stdin=subprocess.DEVNULL, stdout=stream, stderr=subprocess.STDOUT)
            code = process.wait(timeout=remaining)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait()
            code = 124
        stream.write(f'\nEXIT {code}\n'.encode()); stream.flush(); os.fsync(stream.fileno())
    print(f'COMMAND_EXIT {code} LOG {log}', flush=True)
    step = {'argv': argv, 'exit_code': code, 'log': artifact(log)}
    if code != 0: raise GateProcessError(step)
    return step


def build(args, evidence):
    candidate = source()
    directory = evidence / ('spend-build-' + str(time.time_ns()))
    directory.mkdir(mode=0o700)
    build_dir = directory / 'native'
    target = Path(os.environ.get('CARGO_TARGET_DIR', str(directory / 'cargo'))).absolute()
    environment = dict(os.environ, CARGO_TARGET_DIR=str(target), CARGO_INCREMENTAL='0',
        CARGO_PROFILE_DEV_DEBUG='0', CARGO_PROFILE_TEST_DEBUG='0', CARGO_BUILD_JOBS='4',
        PYTHONDONTWRITEBYTECODE='1')
    deadline = time.monotonic() + 1800
    record = {'schema': SCHEMA, 'source': candidate, 'features': {'sandbox': ['host-ffi'], 'runtime': []},
        'native_cases': NATIVE_CASES, 'rust_cases': RUST_CASES,
        'toolchain': capture(['rustc', '+1.91.1', '-vV']),
        'compiler': capture([*shlex.split(args.cc), '--version']), 'compiler_command': shlex.split(args.cc), 'compiler_shell': args.cc,
        'native_build_directory': str(build_dir), 'cargo_target_directory': str(target),
        'steps': [], 'artifacts': {}, 'completed': False}
    try:
        record['steps'].append(launch(['cargo', '+1.91.1', 'build', '--locked',
            '--manifest-path', 'programs/Cargo.toml', '-p', 'layerx-programs-sandbox',
            '--features', 'host-ffi'], directory, environment, deadline, 'sandbox'))
        sandbox = target / 'debug/liblayerx_programs_sandbox.a'
        record['steps'].append(launch(['make', '-j4', '-f', 'Makefile', '-f', MAKEFILE,
            'BUILD_DIR=' + str(build_dir), 'CC=' + args.cc, 'LXP_REVISION=' + candidate['revision'],
            'PAXEER_SPEND_RUNTIME_LIB=' + str(sandbox), 'paxeer-x-native-104.30.4'],
            directory, environment, deadline, 'native'))
        command = ['cargo', '+1.91.1', 'test', '--locked', '--manifest-path', 'programs/Cargo.toml',
            '-p', 'layerx-programs-runtime', '--lib']
        for name in ('program_spend_composition', 'isolation', 'monetary_law'):
            command.extend(['--test', name])
        command.extend(['--no-run', '--message-format=json'])
        step = launch(command, directory, environment, deadline, 'runtime')
        record['steps'].append(step)
        found = {}
        finished = False
        for line in Path(step['log']['path']).read_text().splitlines():
            if not line.startswith('{'):
                continue
            event = json.loads(line)
            if event.get('reason') == 'build-finished': finished = event.get('success') is True
            name = event.get('target', {}).get('name')
            if (event.get('reason') == 'compiler-artifact' and event.get('profile', {}).get('test')
                    and event.get('executable') and name in RUST_CASES):
                require(name not in found, 'duplicate test binary')
                found[name] = artifact(event['executable'])
        require(finished and set(found) == set(RUST_CASES), 'declared runtime binaries missing')
        native = build_dir / 'tests/programs_spend_capability_composition'
        guests = directory / 'guests'; guests.mkdir(mode=0o700)
        record['steps'].append(launch([str(native), '--emit-wasm', str(guests)],
            directory, environment, deadline, 'guests'))
        required = {f'case{i}.{kind}.wasm' for i in range(8) for kind in ('owner', 'child', 'descendant')} | {'payee.bin'}
        require({p.name for p in guests.iterdir()} == required, 'incomplete actual guest inventory')
        record['artifacts'] = {'native': artifact(native), 'sandbox': artifact(sandbox),
            'library': artifact(build_dir / 'liblayerx.a'), 'rust': found,
            'headers': [artifact(p) for p in sorted((build_dir / 'generated').glob('*.h'))],
            'guests': [artifact(p) for p in sorted(guests.iterdir())]}
        require(record['artifacts']['headers'], 'generated native headers missing')
        require(source() == candidate, 'candidate changed during build')
        record['completed'] = True
    finally:
        error = sys.exc_info()[1]
        if isinstance(error, GateProcessError): record['steps'].append(error.step)
        write_private(evidence / ('task-' + TASK + '-build.json'), record)


def qualify(evidence):
    manifest_path = evidence / ('task-' + TASK + '-build.json')
    fd = os.open(manifest_path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd) as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
            and stat.S_IMODE(info.st_mode) == 0o600, 'protected producer manifest required')
        manifest = json.load(stream)
    candidate = source()
    require(manifest['schema'] == SCHEMA and manifest['source'] == candidate
        and manifest['completed'] is True and manifest['native_cases'] == NATIVE_CASES
        and manifest['rust_cases'] == RUST_CASES
        and manifest['features'] == {'sandbox': ['host-ffi'], 'runtime': []},
        'source or required producer inventory mismatch')
    require(manifest['toolchain'] == capture(['rustc', '+1.91.1', '-vV']), 'toolchain changed')
    require(manifest['compiler'] == capture([*manifest['compiler_command'], '--version']), 'native compiler changed')
    expected_runtime = ['cargo', '+1.91.1', 'test', '--locked', '--manifest-path', 'programs/Cargo.toml',
        '-p', 'layerx-programs-runtime', '--lib', '--test', 'program_spend_composition',
        '--test', 'isolation', '--test', 'monetary_law', '--no-run', '--message-format=json']
    expected_sandbox = ['cargo', '+1.91.1', 'build', '--locked', '--manifest-path',
        'programs/Cargo.toml', '-p', 'layerx-programs-sandbox', '--features', 'host-ffi']
    require(len(manifest['steps']) == 4 and manifest['steps'][0]['argv'] == expected_sandbox
        and manifest['steps'][2]['argv'] == expected_runtime, 'declared real compiler invocation missing')
    for step in manifest['steps']:
        require(step['exit_code'] == 0, 'producer step failed'); checked(step['log'])
    require(len(manifest['steps']) == 4, 'complete producer steps required')
    artifacts = manifest['artifacts']
    native = checked(artifacts['native'], elf=True)
    build_dir = Path(manifest['native_build_directory'])
    target = Path(manifest['cargo_target_directory'])
    require(native == build_dir / 'tests/programs_spend_capability_composition'
        and Path(artifacts['sandbox']['path']) == target / 'debug/liblayerx_programs_sandbox.a'
        and Path(artifacts['library']['path']) == build_dir / 'liblayerx.a', 'native build output identity mismatch')
    require(manifest['steps'][1]['argv'] == ['make', '-j4', '-f', 'Makefile', '-f', MAKEFILE,
        'BUILD_DIR=' + str(build_dir), 'CC=' + manifest['compiler_shell'],
        'LXP_REVISION=' + candidate['revision'], 'PAXEER_SPEND_RUNTIME_LIB=' + artifacts['sandbox']['path'],
        'paxeer-x-native-104.30.4'], 'actual native compiler recipe missing')
    checked(artifacts['sandbox']); checked(artifacts['library'])
    require(artifacts['headers'], 'native generated headers absent')
    for header in artifacts['headers']: checked(header)
    guests = [checked(saved) for saved in artifacts['guests']]
    required = {f'case{i}.{kind}.wasm' for i in range(8) for kind in ('owner', 'child', 'descendant')} | {'payee.bin'}
    require({p.name for p in guests} == required and len({p.parent for p in guests}) == 1,
        'native guest artifact inventory changed')
    for path in guests:
        if path.suffix == '.wasm': require(path.read_bytes()[:8] == b'\0asm\1\0\0\0', 'real Wasm required')
    require(manifest['steps'][3]['argv'] == [str(native), '--emit-wasm', str(guests[0].parent)],
        'genuine native guest producer invocation missing')
    directory = evidence / ('spend-verify-' + str(time.time_ns())); directory.mkdir(mode=0o700)
    outputs = directory / 'native-results'; outputs.mkdir(mode=0o700)
    environment = dict(os.environ, PAXEER_X_SPEND_GUESTS=str(guests[0].parent),
        PAXEER_X_SPEND_RESULTS=str(outputs), RUST_BACKTRACE='1')
    deadline = time.monotonic() + 1800
    record = {'schema': 'paxeer-x.program-spend-composition-result.v1', 'source': candidate,
        'manifest': artifact(manifest_path), 'steps': [], 'cases': [], 'completed': False}
    try:
        step = launch([str(native)], directory, environment, deadline, 'native')
        record['steps'].append(step)
        output = Path(step['log']['path']).read_text()
        require(re.findall(r'^CASE (\S+) ok$', output, re.M) == NATIVE_CASES
            and re.findall(r'^PASSED (\d+)$', output, re.M) == ['8'], 'native cases missing')
        require({p.name for p in outputs.iterdir()} == {f'case{i}.terminal.bin' for i in range(8)},
            'actual native receipt terminal inventory incomplete')
        record['native_terminals'] = [artifact(p) for p in sorted(outputs.iterdir())]
        record['cases'].extend('native:' + name for name in NATIVE_CASES)
        require(set(artifacts['rust']) == set(RUST_CASES), 'runtime test binaries missing')
        for binary_name, names in RUST_CASES.items():
            binary = checked(artifacts['rust'][binary_name], elf=True)
            for index, name in enumerate(names):
                step = launch([str(binary), '--exact', name, '--test-threads=1'],
                    directory, environment, deadline, binary_name + '-' + str(index))
                record['steps'].append(step)
                output = Path(step['log']['path']).read_text()
                require(re.findall(r'^test (\S+) \.\.\. (\S+)$', output, re.M) == [(name, 'ok')]
                    and re.search(r'^test result: ok\. 1 passed; 0 failed; 0 ignored;', output, re.M),
                    'required real runtime case missing, failed or skipped')
                record['cases'].append(binary_name + ':' + name)
        require(source() == candidate, 'source changed during qualification')
        for saved in artifacts['rust'].values(): checked(saved, elf=True)
        checked(artifacts['native'], elf=True)
        record['completed'] = True
        print('PAXEER_X_GATE tests=' + str(len(record['cases'])) + ' skipped=0')
    finally:
        error = sys.exc_info()[1]
        if isinstance(error, GateProcessError): record['steps'].append(error.step)
        write_private(directory / 'result.json', record)
        print('EVIDENCE ' + str(directory / 'result.json'))


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--build', action='store_true')
    parser.add_argument('--build-dir', default='build')
    parser.add_argument('--cc', default='cc')
    args = parser.parse_args()
    try:
        evidence = private_directory()
        if args.build: build(args, evidence)
        else: qualify(evidence)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print('program spend qualification refused: ' + str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
