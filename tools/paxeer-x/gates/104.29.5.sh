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
TESTS = ROOT / 'programs/crates/layerx-programs-runtime/tests'
CARGO = os.environ.get('PROGRAMS_CARGO', '/root/.cargo/bin/cargo')
UNIT = {
    'meter::occupancy_class_tests::occupancy_is_its_own_class_outside_one_off_execution_charges': 'occupancy is a resource class distinct from one-off read and write charges',
    'occupancy::tests::evidence_replays_identically_under_its_recorded_schedule': 'occupancy is priced by the recorded schedule, charged to the declared payer and replays identically from canonical evidence',
    'occupancy::tests::property_usage_is_monotone_in_bytes_and_contiguous_batches': 'occupancy is monotone in bytes and in batches held',
    'occupancy::tests::property_drop_charges_through_commit_and_never_after': 'a dropped namespace stops accruing at the batch it was dropped in',
    'occupancy::tests::gaps_refuse_and_committed_drop_stops_future_accrual': 'accounting follows the contiguous batch sequence only',
    'occupancy::tests::multiple_payers_settle_atomically_with_isolated_arrears': 'each namespace is charged to its own responsible account',
    'occupancy::tests::lifetime_ceiling_exhaustion_freezes_only_its_position': 'persistent state keeps paying until its charge ceiling is exhausted',
}
INTEGRATION = {
    'occupancy_movement_is_monotonic_bounded_and_preserves_other_prices': 'occupancy price is a governed fee schedule coefficient',
    'recorded_schedule_prices_real_meter_without_current_head_fallback': 'recorded schedules price without current-head fallback',
    'governed_fee_history_reprices_each_recorded_version_exactly': 'replay reprices each recorded schedule version exactly',
}
SUITES = ['fee_governance', 'replay']
WALL_CLOCK = re.compile(r'\b(SystemTime|Instant|UNIX_EPOCH|chrono)\b|std::time')
RELEASE_SCOPE = 'the remaining integration suites and aggregate make programs-test'


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


def passed_names(output):
    return set(re.findall(r'^test (\S+) \.\.\. ok$', output, re.M))


def production_path():
    meter = (SRC / 'meter.rs').read_text()
    kinds = re.search(r'^pub enum ResourceKind \{\n(.*?)^\}', meter, re.M | re.S)
    require(kinds and re.findall(r'^    (\w+),$', kinds.group(1), re.M).count('StorageOccupancy') == 1,
            'meter has no distinct storage occupancy resource class')
    require(re.search(r'ResourceKind::StorageOccupancy => None,', meter),
            'occupancy is folded into a one-off execution budget dimension')
    usage = re.search(r'^pub struct MeteredUsage \{\n(.*?)^\}', meter, re.M | re.S)
    require(usage and 'pub occupancy_byte_batches: u128,' in usage.group(1) and 'pub occupancy_fee_units: u128,' in usage.group(1),
            'metered usage does not record occupancy separately')
    require('fee_units_per_occupancy_byte_batch: u64,' in meter, 'fee schedule has no occupancy coefficient')
    occupancy = (SRC / 'occupancy.rs').read_text()
    prepare = occupancy[occupancy.index('    fn prepare_positions('):]
    prepare = prepare[:prepare.index('\n    }\n')]
    require('let price = schedule.occupancy_byte_batch_price();' in prepare,
            'occupancy is not priced through the supplied fee schedule')
    require('OccupancyError::MissingResponsibility' in prepare and 'if batch != expected' in prepare,
            'occupancy is not bound to a declared payer and the contiguous batch sequence')
    require(re.search(r'u128::from\(position\.bytes\)\s*\.checked_mul\(u128::from\(intervals\)\)', occupancy),
            'occupancy is not namespace bytes held across batch intervals')
    for name, text in (('meter.rs', meter), ('occupancy.rs', occupancy)):
        require(not WALL_CLOCK.search(text), f'{name} reads a wall clock')
    replay = occupancy[occupancy.index('    pub fn replay_evidence('):]
    replay = replay[:replay.index('\n    }\n')]
    require('recorded.fee_schedule()' in replay and 'prepared.settlement != recorded' in replay,
            'occupancy evidence does not replay under its recorded fee schedule')
    ffi = (SRC / 'ffi_call.rs').read_text()
    require(ffi.count('occupancy_byte_batches: occupancy.byte_batches,') >= 1 and
            ffi.count('occupancy_fee_units: occupancy.fee_units,') >= 1,
            'protocol terminal usage does not carry the settled occupancy')
    commit = (SRC / 'commit.rs').read_text()
    require('usage.occupancy_byte_batches.to_be_bytes()' in commit and 'usage.occupancy_fee_units.to_be_bytes()' in commit,
            'canonical execution evidence does not commit occupancy usage')
    print('production path: distinct occupancy class, schedule-priced per declared payer, batch-sequenced, no wall clock, recorded in usage and evidence')
    return 9


try:
    count = production_path()
    env = dict(os.environ, CARGO_TARGET_DIR=os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/task-104.29.5'))
    base = [CARGO, 'test', '--locked', '--manifest-path', 'programs/Cargo.toml', '-p', 'layerx-programs-runtime']
    output = run(base + ['--lib', '--', 'occupancy::', 'meter::'], 'occupancy and meter unit suites', env)
    missing = sorted(name for name in UNIT if name not in passed_names(output))
    require(not missing, 'unit acceptance tests did not pass: ' + ' '.join(missing))
    passed, skipped = tally(output, 'unit', 1)
    count += passed
    require(set(SUITES) <= {path.stem for path in TESTS.glob('*.rs')}, 'retained occupancy suite inventory changed')
    output = run(base + [arg for suite in SUITES for arg in ('--test', suite)], 'occupancy integration suites', env)
    missing = sorted(name for name in INTEGRATION if name not in passed_names(output))
    require(not missing, 'integration acceptance tests did not pass: ' + ' '.join(missing))
    passed, ignored = tally(output, 'integration', len(SUITES))
    count += passed
    skipped += ignored
    for name, criterion in {**UNIT, **INTEGRATION}.items():
        print(f'criterion: {criterion}: {name} ok')
    print(RELEASE_SCOPE + ' remain release scope; not run or credited here')
    print(f'PAXEER_X_GATE tests={count} skipped={skipped}')
except Exception as error:
    print('storage occupancy: ' + str(error), file=sys.stderr)
    raise SystemExit(1)
PY
