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
    'budget::tests::checked_fee_ceiling_rejects_product_and_sum_overflow': 'admission fee ceiling is checked',
    'meter::declared_budget_tests::declared_ceiling_never_changes_consumed_usage_or_billed_fee': 'usage and fee are independent of the declared ceiling',
    'meter::declared_budget_tests::one_short_declared_ceiling_is_a_typed_refusal_at_the_exact_dimension': 'exceeding the declared ceiling is a typed resource refusal',
}
ACTIVITY = {
    'declared_budget_enforces_exact_v1_bounds_in_stable_dimension_order': 'declaration bounded by protocol maximum and minimum',
    'protocol_minimum_executes_the_smallest_valid_empty_call': 'declared minimum is a viable execution',
    'declared_budget_codec_is_fixed_width_strict_and_revalidates': 'activity declaration codec revalidates bounds',
    'activity_budget_admission_binds_exact_coverage_schedule_and_custom_maximum': 'payer coverage of the ceiling is admitted exactly',
    'invalid_over_and_unfunded_admission_precede_any_poison_guest_work': 'over-maximum and unfunded declarations refuse before guest code',
    'budget_token_mismatch_refuses_before_a_poison_start': 'admitted token mismatch refuses before guest code',
    'protocol_budget_law_makes_public_ceiling_fee_overflow_unreachable': 'protocol maximum ceiling fee is representable',
    'budgeted_storage_read_is_exact_unbilled_on_rejection_and_fee_priced_on_success': 'only consumed usage is billed',
    'sufficient_declared_headroom_never_changes_candidate_usage_or_evidence': 'equal executions under different ceilings have identical usage and evidence',
    'sufficient_seven_dimension_headroom_preserves_nested_success_refusal_and_fault': 'headroom independence across nested outcomes',
    'one_declared_cpu_ceiling_is_shared_by_the_real_v1_call_graph': 'one declared ceiling covers the whole call graph',
    'sibling_calls_share_storage_and_output_ceilings_and_rollback_atomically': 'siblings share one ceiling with atomic rollback',
    'nested_memory_and_table_limits_are_graph_wide_for_v1_and_candidate': 'memory and table ceilings are graph wide',
    'budgeted_v1_cpu_exhaustion_retains_actual_usage_and_failed_graph': 'exhaustion is a typed resource result with actual usage',
    'retained_start_faults_keep_usage_leaf_identity_and_atomic_rollback': 'failed execution rolls back atomically',
}
SUITES = ['activity_budget', 'composition', 'shared_storage']
RELEASE_SCOPE = 'monetary_law, the remaining integration suites and aggregate make programs-test'


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
    budget = (SRC / 'budget.rs').read_text()
    require(re.search(r'match validate_bounds\(resources, ResourceBudget::declared\(\)\)', budget),
            'DeclaredBudget::new is not bounded by the protocol maximum')
    require('MIN_ACTIVITY_CPU_FUEL' in budget and 'InsufficientCoverage' in budget, 'declared minimum or coverage refusal missing')
    execute = (SRC / 'execute.rs').read_text()
    admit = execute[execute.index('pub(crate) fn admit_activity_budget('):]
    admit = admit[:admit.index('\n    }\n')]
    require('validate_bounds(resources, self.effective_activity_maximum())?' in admit and
            'available_fee_units < maximum_fee_units' in admit, 'executor admission does not refuse over-maximum or unfunded declarations')
    ffi = (SRC / 'ffi_call.rs').read_text()
    begin = ffi[ffi.index('pub extern "C" fn layerx_programs_call_begin('):]
    order = [begin.find(marker) for marker in ('DeclaredBudget::new(', '.admit_activity_budget(', '.execute_authorized_v2_budgeted(')]
    require(all(position >= 0 for position in order) and order == sorted(order),
            'protocol call activity does not decode and admit its declared budget before guest execution')
    print('production path: activity declaration decoded, bounded and admitted before guest execution')
    return 3


try:
    count = production_path()
    env = dict(os.environ, CARGO_TARGET_DIR=os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/task-104.28.5'))
    base = [CARGO, 'test', '--locked', '--manifest-path', 'programs/Cargo.toml', '-p', 'layerx-programs-runtime']
    output = run(base + ['--lib', '--', 'budget::', 'meter::'], 'budget and meter unit suites', env)
    missing = sorted(name for name in UNIT if name not in passed_names(output))
    require(not missing, 'unit acceptance tests did not pass: ' + ' '.join(missing))
    passed, skipped = tally(output, 'unit', 1)
    count += passed
    output = run(base + [arg for suite in SUITES for arg in ('--test', suite)], 'declared budget integration suites', env)
    missing = sorted(name for name in ACTIVITY if name not in passed_names(output))
    require(not missing, 'activity budget acceptance tests did not pass: ' + ' '.join(missing))
    passed, ignored = tally(output, 'integration', len(SUITES))
    count += passed
    skipped += ignored
    for name, criterion in {**UNIT, **ACTIVITY}.items():
        print(f'criterion: {criterion}: {name} ok')
    print(RELEASE_SCOPE + ' remain release scope; not run or credited here')
    print(f'PAXEER_X_GATE tests={count} skipped={skipped}')
except Exception as error:
    print('declared execution budget: ' + str(error), file=sys.stderr)
    raise SystemExit(1)
PY
