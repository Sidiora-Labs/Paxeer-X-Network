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
    'access::tests::canonical_encoding_is_independent_of_builder_order_and_strictly_decodes': 'declared access set has one canonical encoding',
    'access::tests::frozen_empty_and_absent_encodings_match_protocol_bytes': 'empty and absent declarations have frozen protocol bytes',
    'access::tests::declaration_encoding_distinguishes_absence_from_an_explicit_empty_set': 'absence and an explicit empty set are distinct commitments',
    'access::tests::call_activity_field_covers_presence_and_exact_declaration_bytes': 'call activity field covers presence and exact declaration bytes',
    'access::tests::explicit_declaration_refuses_every_access_outside_its_exact_commitment': 'access outside the declaration is a typed refusal',
    'access::tests::absent_means_whole_reachable_set_for_charge_and_resolved_conflicts': 'absent declaration is the whole reachable set',
    'access::tests::absent_charge_resolves_every_capability_reachable_callee_namespace': 'absent charge covers every reachable callee namespace',
    'access::tests::broad_and_extra_declarations_are_deterministically_charged': 'over-declaration is safe and charged',
    'access::tests::overlapping_same_mode_scopes_remain_distinct_and_charged': 'overlapping declared scopes are each charged',
    'access::tests::calldata_derived_declaration_is_exact_and_prior_state_cannot_widen_it': 'calldata derivation is exact and state cannot widen it',
    'access::tests::sdk_calldata_derivation_is_byte_identical_to_the_runtime_commitment': 'SDK-derived declaration is the runtime commitment byte for byte',
    'access::tests::sdk_absent_and_whole_namespace_declarations_match_runtime_bytes': 'SDK absent and explicit whole-namespace declarations match runtime bytes',
    'access::tests::actual_abi_storage_refuses_a_calldata_selected_undeclared_key': 'calldata-dependent access enforced at the ABI',
    'access::tests::actual_abi_prior_state_cannot_select_an_undeclared_followup_key': 'prior-state-dependent access enforced at the ABI',
    'access::tests::actual_nested_abi_refuses_an_undeclared_callee_write': 'callee-dependent access enforced in the nested ABI',
    'access::tests::callee_behaviour_is_checked_against_the_root_activity_declaration': 'callee behaviour checked against the root declaration',
}
GUEST = {
    'declared_access_executes_calldata_selected_guest_keys_and_charges_excess': 'real guest calldata-selected keys execute and over-declaration is charged',
    'declared_access_rolls_back_prior_state_selected_guest_write': 'real guest prior-state-selected undeclared write refuses and rolls back',
    'declared_access_is_enforced_inside_real_nested_guest_frame': 'real nested guest callee write outside the declaration refuses',
}
SUITES = ['shared_storage']
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


def ordered(text, markers, reason):
    positions = [text.find(marker) for marker in markers]
    require(all(position >= 0 for position in positions) and positions == sorted(positions), reason)


def production_path():
    payload = (ROOT / 'agent/crates/layerx-types/src/program_call.rs').read_text()
    require(re.search(r'for body in \[\s*self\.entrypoint,\s*self\.calldata,\s*self\.capabilities,\s*self\.access_declaration,\s*\]', payload),
            'native call activity does not carry the declaration as a length-delimited body')
    ffi = (SRC / 'ffi_call.rs').read_text()
    require('layerx_programs_call_activity_byte(token, ACCESS_DECLARATION, offset)' in ffi and
            'crate::AccessDeclaration::canonical_decode(&encoded_access_declaration)' in ffi,
            'protocol call activity does not strictly decode its declared access set')
    require('AbiError::AccessDeclaration => vec![15]' in ffi, 'declaration refusal is not a typed activity result')
    execute = (SRC / 'execute.rs').read_text()
    sites = [match.start() for match in re.finditer(r'let declaration_charge = access_declaration', execute)]
    require(len(sites) == 2, f'{len(sites)} executor declaration admissions, expected 2')
    for site in sites:
        for installed in ('abi.set_access_declaration(access_declaration)', '.charge_cpu(declaration_charge.total_units())'):
            ordered(execute[site:site + 2000], ['.charge(&reachable)', installed],
                    'executor does not price, charge and install the declaration before guest execution')
    storage = (SRC / 'abi/storage_ops.rs').read_text()
    require(storage.count('.map_err(|_| AbiError::AccessDeclaration)?') >= 5,
            'storage operations do not refuse undeclared access with a typed result')
    calls = (SRC / 'calls.rs').read_text()
    require('child_abi.set_access_declaration(access_declaration);' in calls,
            'nested frames do not inherit the root declaration')
    print('production path: declaration carried in the activity, decoded, charged, installed and enforced in every frame')
    return 6


try:
    count = production_path()
    env = dict(os.environ, CARGO_TARGET_DIR=os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/task-104.32.5'))
    base = [CARGO, 'test', '--locked', '--manifest-path', 'programs/Cargo.toml', '-p', 'layerx-programs-runtime']
    output = run(base + ['--lib', '--', 'access::'], 'access unit suite', env)
    missing = sorted(name for name in UNIT if name not in passed_names(output))
    require(not missing, 'unit acceptance tests did not pass: ' + ' '.join(missing))
    passed, skipped = tally(output, 'unit', 1)
    count += passed
    output = run(base + [arg for suite in SUITES for arg in ('--test', suite)], 'declared access guest suite', env)
    missing = sorted(name for name in GUEST if name not in passed_names(output))
    require(not missing, 'guest acceptance tests did not pass: ' + ' '.join(missing))
    passed, ignored = tally(output, 'integration', len(SUITES))
    count += passed
    skipped += ignored
    for name, criterion in {**UNIT, **GUEST}.items():
        print(f'criterion: {criterion}: {name} ok')
    print(RELEASE_SCOPE + ' remain release scope; not run or credited here')
    print(f'PAXEER_X_GATE tests={count} skipped={skipped}')
except Exception as error:
    print('declared access sets: ' + str(error), file=sys.stderr)
    raise SystemExit(1)
PY
