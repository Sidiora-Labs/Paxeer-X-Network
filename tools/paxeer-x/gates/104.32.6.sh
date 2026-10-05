#!/usr/bin/env bash
set -euo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
exec python3 - <<'PY'
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path.cwd()
SRC = ROOT / 'programs/crates/layerx-programs-runtime/src'
CARGO = os.environ.get('PROGRAMS_CARGO', '/root/.cargo/bin/cargo')
UNIT = {
    'schedule::tests::partition_is_a_pure_canonical_function_of_batch_contents': 'partition is a pure canonical function of the batch contents',
    'schedule::tests::disjoint_batches_share_one_level_and_all_conflicting_batches_serialise': 'disjoint activities share a level and conflicting activities serialise',
    'schedule::tests::unresolved_absent_declaration_is_a_barrier': 'an unresolved absent declaration is a scheduling barrier',
    'schedule::tests::actor_sequence_conflicts_across_distinct_principals': 'a shared actor sequence is a protocol conflict',
    'schedule::tests::parallel_and_refused_parallelism_commit_the_canonical_serial_result': 'parallel and refused parallelism commit the canonical serial result',
    'schedule::tests::failed_activity_commits_nothing_under_every_strategy': 'a failed activity commits nothing under every strategy',
}
HARNESS = {
    'low-conflict': 'low-conflict production batch is one level and identical to serial',
    'all-conflicting': 'all-conflicting production batch is one level per activity and identical to serial',
}
MEASURE = re.compile(r'^program batch differential workload=(\S+) activities=(\d+) levels=(\d+) workers=(\d+) '
                     r'serial_ns=(\d+) parallel_ns=(\d+)$', re.M)
HARNESS_QUALIFICATIONS = 6
RELEASE_SCOPE = 'aggregate make programs-test, sanitizers and the remaining Programs suites'


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def run(argv, label, env):
    result = subprocess.run(argv, cwd=ROOT, env=env, stdin=subprocess.DEVNULL, capture_output=True,
                            text=True, timeout=1500)
    sys.stdout.write(result.stdout)
    sys.stdout.write(result.stderr)
    require(result.returncode == 0, f'{label} exit {result.returncode}')
    return result.stdout + result.stderr


def tally(output, label, expected_binaries):
    results = re.findall(r'^test result: (\w+)\. (\d+) passed; (\d+) failed; (\d+) ignored;', output, re.M)
    require(len(results) == expected_binaries, f'{label}: {len(results)} test binaries reported, expected {expected_binaries}')
    require(all(state == 'ok' and failed == '0' for state, _, failed, _ in results), f'{label}: failing test binary')
    passed = sum(int(item[1]) for item in results)
    require(passed > 0, f'{label}: no test ran')
    return passed, sum(int(item[3]) for item in results)


def production_path():
    kernel = (ROOT / 'src/protocol/lxp_kernel.c').read_text()
    require('status = layerx_programs_schedule_plan(items, count, levels,' in kernel,
            'kernel batch preparation does not plan from declared access sets')
    require('levels[index] > (uint16_t)(levels[index - 1U] + 1U)' in kernel,
            'kernel does not require nondecreasing contiguous canonical levels')
    require(re.search(r'maximum_workers > 1U &&\s*pthread_create\(', kernel) and
            '(void)kernel_prepare_worker_run(&workers[worker_index]);' in kernel,
            'level workers lack the identical inline refusal path')
    require(re.search(r'if \(levels\[index\] == level\) \{\s*if \(index != settled_count\)', kernel),
            'prepared level results are not applied in canonical activity order')
    daemon = (ROOT / 'cmd/layerxd/lxp_daemon_process.c').read_text()
    require(re.search(r'lxp_kernel_prepare_activity_batch\(\s*&process->kernel, activities, executions, count,\s*maximum_workers',
                      daemon), 'production daemon does not prepare batches through the scheduler')
    ffi = (SRC / 'ffi_call.rs').read_text()
    require('if enrichment_complete && has_storage_writes {' in ffi,
            'treasury occupancy dependency is not derived from effective storage writes')
    require('let graph = crate::ConflictGraph::from_accesses(&accesses);' in ffi,
            'protocol plan does not use the deterministic conflict graph')
    print('production path: kernel plans levels from declared access sets, runs each level on bounded workers with '
          'an identical inline refusal path and applies results in canonical order')
    return 8


try:
    count = production_path()
    target = os.environ.get('CARGO_TARGET_DIR', str(ROOT / 'programs/target'))
    env = dict(os.environ, CARGO_TARGET_DIR=target,
               PATH=str(Path(CARGO).parent) + os.pathsep + os.environ.get('PATH', ''))
    output = run([CARGO, 'test', '--locked', '--manifest-path', 'programs/Cargo.toml', '-p', 'layerx-programs-runtime',
                  '--features', 'host-ffi', '--lib', '--', 'schedule::'], 'scheduler unit suite', env)
    passed_names = set(re.findall(r'^test (\S+) \.\.\. ok$', output, re.M))
    missing = sorted(name for name in UNIT if name not in passed_names)
    require(not missing, 'scheduler acceptance tests did not pass: ' + ' '.join(missing))
    passed, skipped = tally(output, 'scheduler unit', 1)
    count += passed
    output = run(['make', 'programs-differential', 'PROGRAMS_CARGO=' + CARGO], 'programs-differential', env)
    passed, ignored = tally(output, 'replay and determinism', 2)
    count += passed
    skipped += ignored
    rows = {row[0]: row for row in MEASURE.findall(output)}
    require(sorted(rows) == sorted(HARNESS), 'differential harness did not report both workloads')
    for workload, criterion in HARNESS.items():
        _, activities, levels, workers, serial_ns, parallel_ns = rows[workload]
        activities, levels = int(activities), int(levels)
        require(activities >= 32, f'{workload}: batch of {activities} activities is not a large batch')
        require(levels == (1 if workload == 'low-conflict' else activities), f'{workload}: {levels} levels')
        serial_ns, parallel_ns = int(serial_ns), int(parallel_ns)
        require(serial_ns > 0 and parallel_ns > 0, f'{workload}: missing timing')
        print(f'speedup workload={workload} activities={activities} levels={levels} workers={workers} '
              f'serial_ns={serial_ns} parallel_ns={parallel_ns} speedup={serial_ns / parallel_ns:.3f}')
        print(f'criterion: {criterion}: ok')
    count += HARNESS_QUALIFICATIONS
    for name, criterion in UNIT.items():
        print(f'criterion: {criterion}: {name} ok')
    print('criterion: identical state root, canonical receipts, events and per-activity fuel serial versus parallel: '
          'programs_parallel_differential ok')
    print(RELEASE_SCOPE + ' remain release scope; not run or credited here')
    print(f'PAXEER_X_GATE tests={count} skipped={skipped}')
except Exception as error:
    print('parallel program scheduling: ' + str(error), file=sys.stderr)
    raise SystemExit(1)
PY
