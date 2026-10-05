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
FIXTURE = ROOT / 'programs/sdk/rust/response-fixture'
CARGO = os.environ.get('PROGRAMS_CARGO', '/root/.cargo/bin/cargo')
SUITES = ('activity_calldata', 'composition')
CALLDATA_TESTS = ('activity_entry_accepts_empty_and_exact_maximum_calldata',
                  'empty_skips_trapping_allocator_and_copy_meter_is_exact',
                  'root_and_nested_boundaries_deliver_identical_bytes_without_double_charge',
                  'one_past_maximum_refuses_before_a_trapping_start_runs')
ACTIVITY_PATHS = {'pub fn execute_authorized(': 'invoke_authorized_entry(',
                  'pub(crate) fn execute_authorized_budgeted(': 'entrypoint::invoke(',
                  'fn execute_authorized_v2_with_budget(': 'entrypoint::invoke('}
SDK_MACROS = ('entrypoint', 'response_entrypoint', 'failure_entrypoint')


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def run(argv, label, env=None, cwd=ROOT, seconds=1500):
    result = subprocess.run(argv, cwd=cwd, env=env, stdin=subprocess.DEVNULL, capture_output=True,
                            text=True, timeout=seconds)
    sys.stdout.write(result.stdout)
    sys.stdout.write(result.stderr)
    require(result.returncode == 0, f'{label} exit {result.returncode}')
    return result.stdout + result.stderr


def body(text, signature):
    start = text.find(signature)
    require(start >= 0, f'missing {signature}')
    following = re.search(r'^    (?:pub(?:\([a-z]+\))? )?fn ', text[start + len(signature):], re.M)
    end = start + len(signature) + following.start() if following else len(text)
    return text[start:end]


def block(text, signature):
    start = text.find(signature)
    require(start >= 0, f'missing {signature}')
    end = text.find('\n}', start)
    require(end > start, f'unterminated {signature}')
    return text[start:end]


def constant(text, name):
    found = re.search(r'^pub const ' + name + r': [^=]+= ([^;]+);', text, re.M)
    require(found, f'missing constant {name}')
    return found.group(1).strip()


def contract():
    checks = 0
    execute = (RUNTIME / 'execute.rs').read_text()
    request = block(execute, "pub struct AuthorizedExecutionRequest<'a> {")
    for field in ('pub program: ProgramId,', "pub entrypoint: &'a str,", "pub calldata: &'a [u8],"):
        require(field in request, f'AuthorizedExecutionRequest lacks {field}')
        checks += 1
    for signature, entry in ACTIVITY_PATHS.items():
        text = body(execute, signature)
        preflight = text.find('entrypoint::preflight(request.calldata)')
        instance = text.find('.instantiate_composed')
        invoke = text.find(entry)
        require(0 <= preflight < instance < invoke, f'{signature} does not refuse calldata before instantiation and enter through the shared protocol')
        checks += 1
    helper = body(execute, 'fn invoke_authorized_entry(')
    require('entrypoint::invoke(instance, entrypoint, calldata)' in helper, 'root activity entry bypasses the shared protocol')
    checks += 1
    calls = (RUNTIME / 'calls.rs').read_text()
    require('entrypoint::invoke(&mut instance, CALL_ENTRY_EXPORT, input)' in calls, 'program-to-program edge bypasses the shared protocol')
    checks += 1
    entry = (RUNTIME / 'entrypoint.rs').read_text()
    invoke = body(entry, 'pub(crate) fn invoke(')
    order = [invoke.find(marker) for marker in ('preflight(calldata)?', 'CALL_INPUT_FUEL_PER_BYTE',
                                                'consume_copy_fuel(fuel)', 'CALL_RESERVE_EXPORT',
                                                'write_linear_memory', '.call(\n            entrypoint')]
    require(all(index >= 0 for index in order) and order == sorted(order), 'entry protocol order is not preflight, per-byte charge, reserve, write, enter')
    checks += 1
    runtime_abi = (RUNTIME / 'abi/mod.rs').read_text()
    sdk_abi = (SDK / 'abi.rs').read_text()
    require(constant(runtime_abi, 'MAX_CALL_INPUT_BYTES') == constant(sdk_abi, 'MAX_CALL_INPUT_BYTES'), 'SDK and runtime calldata bounds differ')
    checks += 1
    runtime_calls = (RUNTIME / 'calls.rs').read_text()
    for name in ('CALL_ENTRY_EXPORT', 'CALL_RESERVE_EXPORT'):
        require(constant(runtime_calls, name) == constant(sdk_abi, name), f'SDK and runtime {name} differ')
        checks += 1
    sdk_entry = (SDK / 'entry.rs').read_text()
    require(re.search(r'^pub const CALL_INPUT_CAPACITY: usize = MAX_CALL_INPUT_BYTES;', sdk_entry, re.M), 'SDK reservation capacity is not the ABI bound')
    checks += 1
    macros = (SDK / 'macros.rs').read_text()
    for name in SDK_MACROS:
        text = block(macros, 'macro_rules! ' + name + ' {')
        require('fn layerx_reserve(length: i32) -> i32' in text and '$crate::entry::reserve_call_input(length)' in text, f'{name}! does not reserve through SDK entry plumbing')
        require('fn layerx_call(input_pointer: i32, input_length: i32) -> i32' in text and '$crate::entry::with_call_input(' in text, f'{name}! does not receive calldata through SDK entry plumbing')
        checks += 1
    print(f'contract: {checks} entry protocol checks hold')
    return checks


def tally(output, label, expected_binaries):
    results = re.findall(r'^test result: (\w+)\. (\d+) passed; (\d+) failed; (\d+) ignored;', output, re.M)
    require(len(results) == expected_binaries, f'{label}: {len(results)} test binaries reported, expected {expected_binaries}')
    require(all(state == 'ok' and failed == '0' for state, _, failed, _ in results), f'{label}: failing test binary')
    passed = sum(int(item[1]) for item in results)
    ignored = sum(int(item[3]) for item in results)
    require(passed > 0, f'{label}: no test ran')
    return passed, ignored


try:
    count = contract()
    env = dict(os.environ, CARGO_TARGET_DIR=os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/task-104.28.2'))
    argv = [CARGO, 'test', '--locked', '--manifest-path', 'programs/Cargo.toml', '-p', 'layerx-programs-runtime']
    output = run(argv + [arg for suite in SUITES for arg in ('--test', suite)], 'activity calldata and composition suites', env)
    ran = set(re.findall(r'^test (\S+) \.\.\. ok$', output, re.M))
    require(set(CALLDATA_TESTS) <= ran, 'empty, maximum, one-past-maximum or boundary identity case did not pass')
    passed, skipped = tally(output, 'runtime', len(SUITES))
    count += passed
    fixture_env = {key: value for key, value in os.environ.items() if key != 'CARGO_TARGET_DIR'}
    fixture_env['CARGO'] = CARGO
    output = run(['sh', str(FIXTURE / 'build.sh')], 'SDK entry fixture at activity and program-call boundaries', fixture_env)
    require(re.search(r'^/\S+/layerx_response_fixture\.wasm$', output, re.M), 'SDK entry fixture produced no artifact')
    count += 1
    print('aggregate make programs-test, programs-core-test and programs-sdk-rust matrices remain release scope; not run or credited here')
    print(f'PAXEER_X_GATE tests={count} skipped={skipped}')
except Exception as error:
    print('activity calldata entry: ' + str(error), file=sys.stderr)
    raise SystemExit(1)
PY
