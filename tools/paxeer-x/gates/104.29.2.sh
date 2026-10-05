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
SUITES = ['shared_storage', 'storage_scan', 'namespace_drop', 'isolation', 'composition']
ACCEPTANCE = ['shared_capabilities_encode_append_only_and_narrow_downward',
              'two_principals_update_one_shared_total_with_equal_metering',
              'candidate_guest_increments_one_shared_total_for_two_principals',
              'candidate_guest_shared_read_only_cannot_mutate_the_total',
              'candidate_guest_shared_read_succeeds_and_delete_is_denied_without_mutation',
              'invalid_guest_selectors_refuse_before_memory_or_storage_access',
              'selector_values_are_frozen_and_invalid_values_are_typed',
              'principal_and_shared_grants_do_not_cross_and_denials_are_unmetered',
              'principal_and_shared_access_charge_identical_bytes',
              'candidate_program_call_narrows_shared_authority_before_child_entry']
ACCESS = {('Principal', 'false'): ('StorageRead', 'principal_namespace'),
          ('Principal', 'true'): ('StorageWrite', 'principal_namespace'),
          ('Shared', 'false'): ('SharedStorageRead', 'shared_namespace'),
          ('Shared', 'true'): ('SharedStorageWrite', 'shared_namespace')}
SCOPED = ['storage_read_scoped', 'storage_write_scoped', 'storage_delete_scoped', 'storage_drop_scoped']
SELECTED = ['storage_read_selected', 'storage_write_selected', 'storage_delete_selected',
            'storage_drop_selected', 'storage_scan_preview', 'charge_storage_scan']


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def run(argv, label, env, seconds=1500):
    result = subprocess.run(argv, cwd=ROOT, env=env, stdin=subprocess.DEVNULL, capture_output=True,
                            text=True, timeout=seconds)
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


def block(text, start):
    found = re.search(start, text, re.M)
    require(found, f'{start} missing')
    depth, index = 0, text.index('{', found.end() - 1)
    for position in range(index, len(text)):
        depth += {'{': 1, '}': -1}.get(text[position], 0)
        if depth == 0:
            return text[index:position + 1]
    raise RuntimeError(f'{start} unterminated')


def structure():
    capability = (SRC / 'abi/capability.rs').read_text()
    variants = re.findall(r'^    (\w+)', block(capability, r'^pub enum Capability \{'), re.M)
    require(variants[:4] == ['StorageRead', 'StorageWrite', 'SharedStorageRead', 'SharedStorageWrite'],
            f'shared grants are not distinct from principal grants: {variants[:4]}')
    keys = block(capability, r'^pub\(super\) enum CapabilityKey \{')
    require('SharedStorageRead,' in keys and 'SharedStorageWrite,' in keys, 'shared grants share an authority key')
    for name, tag in (('StorageRead', 1), ('StorageWrite', 2), ('SharedStorageRead', 7), ('SharedStorageWrite', 8)):
        require(f'Capability::{name} => encoded.push({tag}),' in capability, f'{name} encoding tag is not {tag}')
        require(f'{tag} => Capability::{name},' in capability, f'{name} decoding tag is not {tag}')
    narrowing = block(capability, r'^    fn narrow_with_origin\(')
    require(not re.search(r'Shared|StorageRead|StorageWrite', narrowing),
            'narrowing special-cases storage grants instead of composing them downward uniformly')
    require('self.0.get(key).ok_or(AbiError::CapabilityDenied)' in block(capability, r'^    pub\(super\) fn grant\('),
            'a missing grant does not fail typed')

    ops = (SRC / 'abi/storage_ops.rs').read_text()
    require(re.findall(r'^    (\w+) = (\d+),', block(ops, r'^pub enum StorageSelector \{'), re.M)
            == [('Principal', '1'), ('Shared', '2')], 'selector values are not frozen at 1 and 2')
    require(re.search(r'_ => Err\(AbiError::InvalidEncoding\),', block(ops, r'^impl TryFrom<i32> for StorageSelector \{')),
            'an unselected or out-of-range selector is not refused typed')
    access = block(ops, r'^    fn storage_access\(')
    arms = re.findall(r'\(StorageSelector::(\w+), (true|false)\) => \{\s*\(CapabilityKey::(\w+), self\.(\w+)\(\)\)', access)
    require({(s, w): (k, n) for s, w, k, n in arms} == ACCESS and len(arms) == 4,
            'selector-to-grant mapping lets one scope use the other scope grant or namespace')
    outside = ops.replace(access, '')
    require('StorageSelector::Shared' not in outside, 'shared access is special-cased outside the grant mapping')
    for name in SELECTED:
        body = block(ops, r'^    (pub(\(crate\))? )?fn ' + name + r'\(')
        require(re.search(r'self\.storage_access\(selector, (true|false)\)', body)
                and 'self.authorization.capabilities().grant(&capability)?;' in body,
                f'{name} does not resolve and enforce the selected grant')
        grant = body.index('.grant(&capability)?')
        for effect in ('self.storage.', 'meter.charge_'):
            if effect in body:
                require(body.index(effect) > grant, f'{name} touches storage or meter before the grant check')
        for charge in re.findall(r'meter\.charge_storage_\w+\(([^;]*)\)\?;', body):
            require('selector' not in charge and 'namespace' not in charge,
                    f'{name} meters by namespace instead of on one per-byte basis')

    host = (SRC / 'host/storage.rs').read_text()
    require('StorageSelector::try_from(raw).map_err(|error| error_status(&error))' in host,
            'host selector refusal is not the typed ABI status')
    for name in SCOPED:
        start = host.index(f'"{name}",')
        end = min([host.index(f'"{other}",') for other in SCOPED if host.index(f'"{other}",') > start] + [len(host)])
        body = host[start:end]
        require('selector(raw_selector)' in body, f'{name} takes no namespace selector')
        first = body.index('selector(raw_selector)')
        for access in ('read_guest(', 'write_guest(', 'with_abi('):
            if access in body:
                require(body.index(access) > first, f'{name} accesses memory or storage before refusing the selector')
    print('structure: distinct shared grants, frozen selector refusal before access, uniform downward narrowing and per-byte metering verified')
    return 10


try:
    count = structure()
    env = dict(os.environ, CARGO_TARGET_DIR=os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/task-104.29.2'))
    base = [CARGO, 'test', '--locked', '--manifest-path', 'programs/Cargo.toml', '-p', 'layerx-programs-runtime']
    output = run(base + ['--lib', '--', 'abi::'], 'abi unit suite', env)
    passed, skipped = tally(output, 'unit', 1)
    count += passed
    require(set(SUITES) <= {path.stem for path in TESTS.glob('*.rs')}, 'retained shared-access suite inventory changed')
    output = run(base + [arg for suite in SUITES for arg in ('--test', suite)], 'shared-access integration suites', env)
    ran = set(re.findall(r'^test (\S+) \.\.\. ok$', output, re.M))
    require(set(ACCEPTANCE) <= ran, 'acceptance tests missing: ' + ' '.join(sorted(set(ACCEPTANCE) - ran)))
    passed, ignored = tally(output, 'integration', len(SUITES))
    count += passed
    skipped += ignored
    print('aggregate make programs-test remains release scope; not run or credited here')
    print(f'PAXEER_X_GATE tests={count} skipped={skipped}')
except Exception as error:
    print('shared namespace capabilities: ' + str(error), file=sys.stderr)
    raise SystemExit(1)
PY
