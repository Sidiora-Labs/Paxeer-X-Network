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
MANIFEST = ['--locked', '--manifest-path', 'programs/Cargo.toml']
REQUIRED = {
    'runtime-lib': ['fault::sdk_parity_tests::vocabulary_and_bound_match_the_sdk',
                    'fault::sdk_parity_tests::runtime_failures_decode_in_the_sdk_and_sdk_encodings_decode_in_the_runtime'],
    'sdk-lib': ['error::candidate_refusal_tests::borrowed_reason_accepts_exact_bound_and_rejects_one_past',
                'error::candidate_refusal_tests::guest_cannot_publish_host_only_classes_and_decode_is_strict',
                'error::candidate_refusal_tests::reason_and_refusal_encodings_round_trip_and_refuse_short_buffers',
                'error::candidate_refusal_tests::program_failure_names_the_refusing_program_and_decodes_strictly'],
    'failure_payload': ['refusal_reason_bounds_and_canonical_roundtrip_are_strict',
                        'program_failure_roundtrip_binds_host_program_identity'],
    'activity_failure': ['root_binary_refusal_is_receipt_carriable_with_usage',
                         'depth_one_refusal_preserves_leaf_and_reason_usage',
                         'declared_maximum_depth_preserves_the_actual_leaf_failure',
                         'one_edge_past_declared_depth_is_a_typed_depth_refusal',
                         'refusal_host_boundary_is_exact_bounded_and_first_publication_wins',
                         'refusal_meter_boundary_and_fee_evidence_are_exact',
                         'failure_evidence_is_deterministic_and_binds_leaf_class_reason_and_fee',
                         'program_failure_discards_storage_and_effects_across_multiple_frames',
                         'nested_published_refusal_requires_sentinel_and_wins_over_trap'],
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
    missing = [name for name in required if name not in passed]
    require(not missing, f'{label}: required tests did not pass: {missing}')
    return sum(int(item[1]) for item in results), sum(int(item[3]) for item in results)


try:
    env = dict(os.environ, CARGO_TARGET_DIR=os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/task-104.28.4'))
    count = skipped = 0
    runtime = [CARGO, 'test'] + MANIFEST + ['-p', 'layerx-programs-runtime']
    for label, argv, binaries, required in [
        ('runtime-lib', runtime + ['--lib', '--', 'fault::', 'terminal::'], 1, REQUIRED['runtime-lib']),
        ('sdk-lib', [CARGO, 'test'] + MANIFEST + ['-p', 'layerx-program-sdk', '--lib', '--', 'error::'], 1, REQUIRED['sdk-lib']),
        ('integration', runtime + ['--test', 'failure_payload', '--test', 'activity_failure'], 2,
         REQUIRED['failure_payload'] + REQUIRED['activity_failure']),
    ]:
        passed, ignored = tally(run(argv, label, env), label, binaries, required)
        count += passed
        skipped += ignored
    print('aggregate make programs-test, native kernel sequence and fee suites and external-artifact suites remain release scope; not run or credited here')
    print(f'PAXEER_X_GATE tests={count} skipped={skipped}')
except Exception as error:
    print('typed failure payloads: ' + str(error), file=sys.stderr)
    raise SystemExit(1)
PY
