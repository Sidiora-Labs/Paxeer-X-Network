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
SUITES = ['isolation', 'namespace_drop', 'shared_storage', 'storage_scan']
UNIT_TESTS = ['storage::namespace::tests::canonical_bytes_are_frozen_for_both_program_scopes',
              'storage::namespace::tests::ordering_matches_canonical_bytes_across_every_variant',
              'storage::namespace::tests::every_namespace_names_exactly_its_owning_program_and_scope']
FIXED_BEFORE_ENTRY = ['let principal_namespace = StorageNamespace::principal(program, authorization.principal());',
                      'let shared_namespace = StorageNamespace::shared(program);']


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


def enum_body(text, name):
    found = re.search(r'^pub enum ' + name + r' \{\n(.*?)^\}', text, re.M | re.S)
    require(found, f'{name} enum missing')
    return found.group(1)


def structure():
    namespace = (SRC / 'storage/namespace.rs').read_text()
    variants = re.findall(r'^    (\w+) \{(.*?)\},$', enum_body(namespace, 'StorageNamespace'), re.M | re.S)
    names = [name for name, _ in variants]
    require(names[:2] == ['PrincipalScoped', 'ProgramShared'], f'StorageNamespace variants {names}')
    require(all(re.search(r'\bprogram: ProgramId,', fields) for _, fields in variants),
            'a StorageNamespace variant does not carry its owning program')
    require(re.search(r'principal: PrincipalId,', variants[0][1]) and 'principal' not in variants[1][1],
            'principal scope is not carried in the type')
    require(not re.search(r'#\[derive\([^)]*\b(PartialOrd|Ord|Default)\b', namespace),
            'StorageNamespace ordering is derived instead of frozen')
    storage = (SRC / 'storage/mod.rs').read_text()
    require(re.search(r'^pub use namespace::StorageNamespace;$', storage, re.M), 'storage plane does not export the enum')
    require(re.search(r'^pub\(crate\) struct StorageAddress \{\n    namespace: StorageNamespace,\n    key: Vec<u8>,\n\}', storage, re.M),
            'storage addresses are not keyed by the closed namespace')
    selector = enum_body((SRC / 'abi/storage_ops.rs').read_text(), 'StorageSelector')
    require(re.findall(r'^    (\w+)', selector, re.M) == ['Principal', 'Shared'] and 'ProgramId' not in selector,
            'guest-visible selector can name more than the two host-fixed scopes')
    abi = (SRC / 'abi/mod.rs').read_text()
    require(all(abi.count(line) == 2 for line in FIXED_BEFORE_ENTRY), 'ABI frames do not fix both namespaces from the executing program')
    require(not re.search(r'\.(principal|shared)_namespace\s*=[^=]', '\n'.join(path.read_text() for path in SRC.rglob('*.rs'))),
            'a fixed frame namespace is reassigned after construction')
    print('structure: closed two-scope namespace enum, host-fixed frame namespaces and closed guest selector verified')
    return 8


try:
    count = structure()
    env = dict(os.environ, CARGO_TARGET_DIR=os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/task-104.29.1'))
    base = [CARGO, 'test', '--locked', '--manifest-path', 'programs/Cargo.toml', '-p', 'layerx-programs-runtime']
    output = run(base + ['--lib', '--', 'storage::'], 'storage unit suite', env)
    ran = set(re.findall(r'^test (\S+) \.\.\. ok$', output, re.M))
    require(set(UNIT_TESTS) <= ran, 'namespace unit tests missing: ' + ' '.join(sorted(set(UNIT_TESTS) - ran)))
    passed, skipped = tally(output, 'unit', 1)
    count += passed
    require(set(SUITES) <= {path.stem for path in TESTS.glob('*.rs')}, 'retained namespace suite inventory changed')
    output = run(base + [arg for suite in SUITES for arg in ('--test', suite)], 'namespace integration suites', env)
    passed, ignored = tally(output, 'integration', len(SUITES))
    count += passed
    skipped += ignored
    print('aggregate make programs-test remains release scope; not run or credited here')
    print(f'PAXEER_X_GATE tests={count} skipped={skipped}')
except Exception as error:
    print('program state namespaces: ' + str(error), file=sys.stderr)
    raise SystemExit(1)
PY
