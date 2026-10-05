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
RUNTIME = ROOT / 'programs/crates/layerx-programs-runtime/src'
SDK = ROOT / 'programs/sdk/rust/src'
CARGO = os.environ.get('PROGRAMS_CARGO', '/root/.cargo/bin/cargo')
MANIFEST = ['--locked', '--manifest-path', 'programs/Cargo.toml']
REQUIRED = {
    'runtime-lib': ['abi::response::tests::unpublished_region_returns_the_entry_code_with_empty_bytes',
                    'abi::response::tests::exact_capacity_and_exact_maximum_publish_whole',
                    'abi::response::tests::over_capacity_is_a_sticky_typed_refusal_never_a_truncation',
                    'abi::response::transport_tests::sibling_fanout_reads_only_its_own_edge_response_and_meters_each_copy',
                    'abi::response::transport_tests::nested_response_past_the_caller_capacity_fails_typed_without_truncation'],
    'activity_response': ['v1_refuses_candidate_import_and_candidate_returns_binary_response',
                          'empty_exact_maximum_and_capacity_refusal_are_not_truncated',
                          'ignored_duplicate_code_mismatch_and_meter_refusals_stay_sticky',
                          'repeated_same_callee_fanout_keeps_edge_responses_distinct_and_charges_each_boundary',
                          'invalid_nested_destination_is_refused_before_child_start_or_graph_entry',
                          'legacy_candidate_call_cannot_discard_a_child_response_or_adopt_child_storage',
                          'nested_output_exhaustion_does_not_adopt_child_storage_effects_or_graph'],
    'sdk-lib': ['call::response_tests::decoded_response_borrows_exact_initialized_prefix',
                'call::response_tests::over_capacity_decode_refuses_without_touching_sentinel',
                'call::response_tests::response_convenience_uses_the_same_exact_capability_encoding',
                'call::response_tests::response_convenience_refuses_an_undersized_scratch_buffer'],
}


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def constant(text, name):
    found = re.search(r'^pub const ' + name + r': [^=]+= ([^;]+);', text, re.M)
    require(found, f'missing constant {name}')
    return found.group(1).strip()


def ordered(text, markers, reason):
    positions = [text.find(marker) for marker in markers]
    require(all(position >= 0 for position in positions) and positions == sorted(positions), reason)


def contract():
    response = (RUNTIME / 'abi/response.rs').read_text()
    sdk_abi = (SDK / 'abi.rs').read_text()
    require(constant(response, 'MAX_CALL_RESPONSE_BYTES') == constant(sdk_abi, 'MAX_CALL_RESPONSE_BYTES'),
            'SDK and runtime response bounds differ')
    ordered(response, ['pub(crate) fn publish(', 'Err(ResponseRefusal::CapacityExceeded {', 'self.published = Some(response);'],
            'response region does not refuse past capacity before accepting the publication')
    manifest = (RUNTIME / 'abi/manifest.rs').read_text()
    require('\\0response_write(i32,i32,i32)->i32\\0program_call_response(i32,i32,i32,i32,i32,i32,i32,i32)->i64\\0' in manifest,
            'frozen ABI-v2 manifest lacks the response operations')
    calls = (RUNTIME / 'host/calls.rs').read_text()
    for name in ('"response_write"', '"program_call_response"'):
        require(name in calls, f'host call family does not register {name}')
    ordered(calls, ['validate_output(&caller, output_pointer, output_capacity)',
                    'execute_nested_call_response(', 'output.write(&mut caller, &pending.response.bytes)',
                    'runtime_calls::adopt_nested_call(caller.data_mut(), pending)'],
            'nested response is not validated, copied into the caller buffer, then adopted in order')
    host = (RUNTIME / 'host/mod.rs').read_text()
    ordered(host, ['pub(crate) fn publish_response(', 'region.publish(response)?;', 'self.meter.charge_output_bytes(bytes)'],
            'response publication is not metered per byte against output bytes')
    execute = (RUNTIME / 'execute.rs').read_text()
    require('pub const fn response(&self) -> Option<&CallResponse> {' in execute,
            'activity record does not return response bytes')
    call = (SDK / 'call.rs').read_text()
    for marker in ("pub struct CallResponse<'a> {", "pub fn invoke_response<'a>(", 'pub fn publish_response('):
        require(marker in call, f'SDK call surface lacks {marker}')
    print('contract: 11 response transport checks hold')
    return 11


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
    count = contract()
    skipped = 0
    env = dict(os.environ, CARGO_TARGET_DIR=os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/task-104.28.3'))
    runtime = [CARGO, 'test'] + MANIFEST + ['-p', 'layerx-programs-runtime']
    for label, argv in [
        ('runtime-lib', runtime + ['--lib', '--', 'abi::response::']),
        ('activity_response', runtime + ['--test', 'activity_response']),
        ('sdk-lib', [CARGO, 'test'] + MANIFEST + ['-p', 'layerx-program-sdk', '--lib', '--', 'call::response_tests::']),
    ]:
        passed, ignored = tally(run(argv, label, env), label, 1, REQUIRED[label])
        count += passed
        skipped += ignored
    print('aggregate make programs-test (the retained reference command), sanitizer and external-artifact matrices remain release scope; not run or credited here')
    print(f'PAXEER_X_GATE tests={count} skipped={skipped}')
except Exception as error:
    print('call response transport: ' + str(error), file=sys.stderr)
    raise SystemExit(1)
PY
