#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
exec python3 - "$@" <<'PY'
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import time

ROOT = Path.cwd()
OUT = Path(os.environ.get('PAXEER_X_MONETARY_ARTIFACTS', '/root/lx-ops/paxeer-x-integration-2026-10-03/task-104.21.1/artifacts')).absolute()
RUST = Path(os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/programs')).absolute()
NATIVE = Path(os.environ.get('PAXEER_X_MONETARY_BUILD_DIR', '/root/lx-target/task-104.21.1/native')).absolute()
CARGO = os.environ.get('PROGRAMS_CARGO', '/root/.cargo/bin/cargo')
ISOLATION = ['unknown_and_ambient_kernel_imports_are_rejected', 'denied_event_and_transfer_guests_have_no_effects', 'guest_memory_bounds_refusal_cannot_write_or_emit_effects', 'capability_narrowing_rejects_missing_grants_and_limit_widening_without_effects', 'nested_frames_use_their_own_program_memory_and_storage_namespace', 'protocol_private_namespace_is_unreachable_from_guest_selectors_and_keys']
COMPOSITION = ['authority_denial_does_not_create_a_phantom_edge_or_start_the_child', 'production_depth_boundary_and_one_past_are_atomic', 'delegated_capability_escalation_matrix_never_enters_the_child', 'production_fanout_boundary_and_one_past_are_atomic', 'production_visit_boundary_and_one_past_are_atomic', 'direct_and_indirect_reentrancy_are_typed_and_atomic', 'nested_guest_event_aggregate_accepts_sixty_four_and_rolls_back_sixty_five']
BOUNDARY = ['transfer::tests::transfer_set_is_bound_to_invocation_authority_and_exact_order', 'transfer::tests::explicit_v2_principal_authority_preserves_kernel_legs_and_legacy_decoding', 'transfer::tests::account_bound_commitment_preserves_signer_and_refuses_forged_names', 'transfer::tests::applied_kernel_evidence_authenticates_each_field_order_and_multiplicity', 'transfer::tests::empty_invalid_and_overflowing_sets_are_refused_before_core', 'transfer::tests::forged_program_or_principal_is_an_invariant_one_violation', 'transfer::tests::child_transfer_requires_a_reachable_call_graph_edge', 'transfer::tests::child_call_cannot_change_the_invoking_principal', 'transfer::tests::child_transfer_must_fit_its_narrowed_asset_recipient_and_amount', 'transfer::tests::malformed_zero_and_oversized_transfer_sets_never_reach_the_kernel_boundary', 'transfer::tests::disconnected_and_forged_nested_call_staging_is_rejected_as_invariant_one', 'transfer::tests::canonical_decoder_rejects_inner_and_trailing_event_malleability', 'transfer::tests::real_guest_mixed_effects_seal_exact_dual_authority_kernel_set']
NATIVE_CASES = ['program_leg_insufficient_balance_refusal_receipt_fees_sequence', 'mixed_source_kernel_law', 'program_leg_atomic_rollback_after_first_leg', 'program_leg_cumulative_bound_refusal', 'kernel_primitive_sole_balance_mutation', 'real_mixed_program_authority_kernel_case']


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def git(*args):
    return subprocess.check_output(['git', '--no-optional-locks', *args], cwd=ROOT, text=True).strip()


def sources():
    names = git('ls-files', 'src', 'include', 'programs', 'platform', 'agent', 'crates', 'tools/protocol', 'contracts/config', 'Makefile', 'tools/paxeer-x/gates/104.21.1.sh', 'tests/programs/test_monetary_law.c', 'tests/programs/test_call_activity.c').splitlines()
    selected = [name for name in names if not Path(name).name.startswith('.env') and (Path(name).suffix in ('.rs', '.c', '.h', '.inc', '.toml', '.lock', '.json') or name in ('Makefile', 'tools/paxeer-x/gates/104.21.1.sh'))]
    require(selected and all((ROOT / name).is_file() for name in selected), 'complete canonical monetary-law source closure required')
    return {name: sha(ROOT / name) for name in selected}


def private_json(path, data):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as output:
        json.dump(data, output, indent=2)
        output.write('\n')


def run(argv, label, env=None, seconds=600):
    log = OUT / (label + '.log')
    with log.open('wb') as stream:
        result = subprocess.run(argv, cwd=ROOT, env=env, stdout=stream, stderr=subprocess.STDOUT, timeout=seconds)
    require(result.returncode == 0, f'{label} exit {result.returncode}; log {log}')
    return log.read_text(errors='replace')


def build():
    require(not git('status', '--porcelain=v1'), 'build requires an immutable published clean candidate')
    before = sources()
    lockpath = Path('/root/lx-cargo/native-build.lock')
    lockpath.parent.mkdir(parents=True, exist_ok=True)
    with lockpath.open('a') as lock:
        started = time.monotonic()
        while True:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError:
                require(time.monotonic() - started < 90, 'native build admission timed out after90seconds')
                time.sleep(0.2)
        waited = time.monotonic() - started
        env = dict(os.environ, CARGO_TARGET_DIR=str(RUST), CARGO_BUILD_JOBS='4')
        run([CARGO, 'build', '--locked', '--manifest-path', 'programs/Cargo.toml', '-p', 'layerx-programs-sandbox', '--features', 'host-ffi'], 'sandbox-build', env)
        runtime = RUST / 'debug/liblayerx_programs_sandbox.a'
        require(runtime.is_file(), 'actual host-ffi sandbox staticlib missing')
        native_binary = NATIVE / 'tests/programs_monetary_law'
        run(['make', '-j4', '-o', 'programs-build', 'BUILD_DIR=' + str(NATIVE), 'PROGRAMS_TARGET_DIR=' + str(RUST), 'PROGRAMS_RUNTIME_LIB=' + str(runtime), 'PROGRAMS_CARGO=' + CARGO, str(native_binary)], 'native-build', env)
        raw = run([CARGO, 'test', '--locked', '--manifest-path', 'programs/Cargo.toml', '-p', 'layerx-programs-runtime', '--lib', '--test', 'monetary_law', '--test', 'isolation', '--test', 'composition', '--no-run', '--message-format=json'], 'rust-build', env)
        found = {}
        for line in raw.splitlines():
            try:
                item = json.loads(line)
            except ValueError:
                continue
            if item.get('reason') == 'compiler-artifact' and item.get('profile', {}).get('test') and item.get('executable'):
                name = item['target']['name']
                if name in ('layerx_programs_runtime', 'monetary_law', 'isolation', 'composition'):
                    require(name not in found, 'duplicate compiler test artifact')
                    found[name] = item['executable']
        require(set(found) == {'layerx_programs_runtime', 'monetary_law', 'isolation', 'composition'}, 'missing actual compiler-produced test artifacts')
        found['native'] = str(native_binary)
        require(before == sources() and not git('status', '--porcelain=v1'), 'source changed during whole build')
        binaries = {}
        target = OUT / 'bin'
        target.mkdir(mode=0o700, exist_ok=True)
        for name, path in found.items():
            require(Path(path).is_file() and not Path(path).is_symlink(), 'ordinary genuine compiler binary required')
            copy = target / name
            shutil.copyfile(path, copy)
            copy.chmod(0o700)
            binaries[name] = {'path': str(copy), 'sha256': sha(copy), 'compiler_artifact': str(path)}
        private_json(OUT / 'manifest.json', {'schema': 'paxeer-x.program-monetary-law-build.v1', 'revision': git('rev-parse', 'HEAD'), 'tree': git('rev-parse', 'HEAD^{tree}'), 'sources': before, 'binaries': binaries, 'native_lock_wait_seconds': waited, 'build_commands': ['sandbox-build.log', 'native-build.log', 'rust-build.log'], 'runtime_sha256': sha(runtime), 'native_testing_library_sha256': sha(NATIVE / 'liblayerx-testing.a'), 'aggregate_release_gate': 'make programs-test; retained and UNRUN at task scope'})


def verify():
    path = OUT / 'manifest.json'
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and not path.is_symlink() and info.st_uid == os.getuid() and info.st_nlink == 1 and not info.st_mode & 0o077, 'private ordinary build manifest required')
    record = json.loads(path.read_text())
    require(record['schema'] == 'paxeer-x.program-monetary-law-build.v1' and record['revision'] == git('rev-parse', 'HEAD') and record['tree'] == git('rev-parse', 'HEAD^{tree}') and record['sources'] == sources() and not git('status', '--porcelain=v1'), 'build source/candidate identity mismatch')
    for item in record['binaries'].values():
        require(sha(item['path']) == item['sha256'], 'prebuilt compiler artifact changed')
    count = 0
    monetary = re.findall(r'#\[test\]\s*fn\s+(\w+)\(', (ROOT / 'programs/crates/layerx-programs-runtime/tests/monetary_law.rs').read_text())
    require(len(monetary) >= 9 and len(set(monetary)) == len(monetary), 'complete monetary-law case inventory missing')
    selected = {'monetary_law': monetary, 'isolation': ISOLATION, 'composition': COMPOSITION, 'layerx_programs_runtime': BOUNDARY}
    outcomes = []
    for name, cases in selected.items():
        binary = record['binaries'][name]['path']
        listing = run([binary, '--list'], name + '-inventory', seconds=60)
        available = re.findall(r'^(.+): test$', listing, re.M)
        require(set(cases) <= set(available), name + ': required genuine test missing')
        if name == 'monetary_law':
            require(set(available) == set(cases), 'monetary-law binary/source case inventory differs')
        for index, case in enumerate(cases):
            output = run([binary, '--exact', case, '--nocapture', '--test-threads=1'], name + '-' + str(index), seconds=120)
            require(re.search(r'^test result: ok\. 1 passed; 0 failed; 0 ignored;', output, re.M), 'required test did not run exactly once without skip: ' + case)
            outcomes.append({'suite': name, 'case': case, 'exit_code': 0})
            count += 1
    output = run([record['binaries']['native']['path']], 'native-verify', seconds=180)
    native_cases = re.findall(r'^CASE ([a-z_]+) ok$', output, re.M)
    require(native_cases == NATIVE_CASES and re.search(r'^PASSED 6$', output, re.M), 'complete genuine native monetary-law corpus missing')
    count += len(native_cases)
    require(record['sources'] == sources(), 'source changed during qualification')
    private_json(OUT / 'result.json', {'revision': record['revision'], 'tree': record['tree'], 'exit_code': 0, 'tests': count, 'skipped': 0, 'outcomes': outcomes, 'native_cases': native_cases, 'aggregate_release_gate': 'make programs-test remains required at release scope; not run or credited by this task'})
    print(f'PAXEER_X_GATE tests={count} skipped=0')


os.umask(0o077)
try:
    require(len(sys.argv) == 1 or sys.argv[1:] == ['--build'], 'usage:104.21.1.sh [--build]')
    OUT.mkdir(mode=0o700, parents=True, exist_ok=True)
    require(not OUT.is_symlink() and OUT.stat().st_uid == os.getuid() and not OUT.stat().st_mode & 0o077 and ROOT != OUT and ROOT not in OUT.parents, 'protected external artifact directory required')
    if sys.argv[1:] == ['--build']:
        build()
    else:
        verify()
except Exception as error:
    print('program monetary law: ' + str(error), file=sys.stderr)
    raise SystemExit(1)
PY
