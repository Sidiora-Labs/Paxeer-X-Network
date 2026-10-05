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
CARGO = os.environ.get('PROGRAMS_CARGO', '/root/.cargo/bin/cargo')
INJECT = 'meter::inject::golden_vectors::'
REQUIRED_UNIT = {
    INJECT + 'loop_charges_at_function_entry_and_every_back_edge_are_frozen',
    INJECT + 'instrumentation_is_a_pure_function_of_module_and_schedule',
    INJECT + 'loop_golden_charge_matches_the_reference_engine_fuel_tier',
    INJECT + 'empty_function_has_frozen_executable_bytes',
    INJECT + 'protocol_schedule_has_frozen_big_endian_record',
    INJECT + 'version_one_refuses_a_nonzero_coefficient_change',
    INJECT + 'br_table_selector_is_not_counted_as_a_dropped_kept_value',
    'cache::tests::only_exact_hash_and_versions_construct_an_artifact',
    'cache::tests::disabled_miss_hit_and_eviction_have_identical_execution_observations',
    'cache::tests::large_activity_mix_is_identical_with_cache_disabled_and_enabled',
    'qualification::tests::dispatcher_refuses_unknown_runtime_and_unsupported_abi',
}
REQUIRED_INTEGRATION = {
    'differential_gate_agrees_on_every_committed_vector',
    'differential_gate_evidence_is_reproducible_per_vector',
    'independent_engines_produce_identical_evidence',
    'recorded_v1_replays_identically_after_a_simulated_upgrade',
    'mixed_v1_v2_history_selects_each_recorded_abi_and_fee_schedule',
    'mixed_history_preserves_recorded_abi_and_refuses_version_substitution',
    'authorized_storage_write_rolls_back_after_real_fuel_exhaustion',
}


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


def tally(output, label, expected_binaries, required):
    results = re.findall(r'^test result: (\w+)\. (\d+) passed; (\d+) failed; (\d+) ignored;', output, re.M)
    require(len(results) == expected_binaries, f'{label}: {len(results)} test binaries reported, expected {expected_binaries}')
    require(all(state == 'ok' and failed == '0' for state, _, failed, _ in results), f'{label}: failing test binary')
    passed = set(re.findall(r'^test (\S+) \.\.\. ok$', output, re.M))
    missing = sorted(required - passed)
    require(not missing, f'{label}: required tests did not pass: {missing}')
    return sum(int(item[1]) for item in results), sum(int(item[3]) for item in results)


try:
    env = {key: value for key, value in os.environ.items() if key != 'PAXEER_X_DETERMINISM_VECTOR_DIR'}
    base = [CARGO, 'test', '--locked', '--manifest-path', 'programs/Cargo.toml', '-p', 'layerx-programs-runtime']
    output = run(base + ['--lib', '--', INJECT, 'cache::tests::', 'qualification::tests::'],
                 'charge-point golden, artifact binding and dispatcher unit suites', env)
    count, skipped = tally(output, 'unit', 1, REQUIRED_UNIT)
    output = run(base + ['--test', 'replay', '--test', 'determinism'],
                 'reference-tier differential corpus and recorded-schedule replay suites', env)
    passed, ignored = tally(output, 'integration', 2, REQUIRED_INTEGRATION)
    count += passed
    skipped += ignored
    print('native metering-schedule protocol-state vector, parallel differential binary and aggregate '
          'make programs-test remain release scope; not run or credited here')
    print(f'PAXEER_X_GATE tests={count} skipped={skipped}')
except Exception as error:
    print('engine-independent metering: ' + str(error), file=sys.stderr)
    raise SystemExit(1)
PY
