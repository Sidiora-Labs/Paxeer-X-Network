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
SUITE = 'storage_scan'
UNIT_TESTS = [
    'storage::ordered_scan::tests::empty_prefix_and_single_entry_return_canonical_entry_without_cursor',
    'storage::ordered_scan::tests::ceiling_paginates_in_key_order_independent_of_insertion_order',
    'storage::ordered_scan::tests::foreign_cursor_and_nonfitting_entry_are_refused',
    'storage::ordered_scan::tests::entry_and_complete_page_byte_ceilings_have_independent_exact_bounds',
    'storage::ordered_scan::tests::scan_requires_matching_read_authority_meters_full_pages_and_resumes_across_activities',
    'storage::ordered_scan::tests::principal_and_shared_scans_require_their_distinct_read_grants',
    'storage::ordered_scan::tests::namespace_ranges_preserve_prefix_pages_and_metering_with_foreign_state',
    'storage::ordered_scan::tests::pages_are_independent_of_insertion_order_and_allocation_history_and_frozen',
    'host::scan::tests::scan_pages_are_identical_across_both_engine_tiers_and_insertion_orders',
]
MANIFEST_ENTRY = 'storage_scan_scoped(i32,i32,i32,i32,i32,i32,i32,i32,i32)->i32'


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


def structure():
    lib = (SRC / 'lib.rs').read_text()
    require('\\0layerx_v2\\0' in lib and MANIFEST_ENTRY in lib, 'storage scan host function absent from the ABI manifest')
    host = (SRC / 'host/mod.rs').read_text()
    require(re.search(r'^mod scan;$', host, re.M) and 'scan::register_v2(&mut linker)?;' in host,
            'storage scan host function is not linked')
    scan_host = (SRC / 'host/scan.rs').read_text()
    require('"storage_scan_scoped"' in scan_host and 'CANDIDATE_ABI_MODULE' in scan_host,
            'storage scan is not registered on the candidate ABI module')
    preview = scan_host.index('abi.storage_scan_preview(')
    capacity = scan_host.index('if encoded.len() > output.capacity()')
    charge = scan_host.index('abi.charge_storage_scan(')
    write = scan_host.index('output.write(&mut caller, &encoded)')
    require(preview < capacity < charge < write, 'scan does not refuse before charging and charge before writing')
    storage = (SRC / 'storage/scan.rs').read_text()
    require(re.search(r'^pub const MAX_STORAGE_SCAN_ENTRIES: u32 = \d+;$', storage, re.M) and
            re.search(r'^pub const MAX_STORAGE_SCAN_BYTES: u32 = [\d_]+;$', storage, re.M),
            'declared per-call scan ceilings missing')
    require('cursor_namespace != namespace.canonical_bytes()' in storage and 'cursor_prefix != prefix' in storage and
            'cursor_limits != limits' in storage, 'cursor is not bound to the scan that issued it')
    require('cells: &BTreeMap<StorageAddress, Vec<u8>>' in storage, 'scan does not iterate the canonical ordered map')
    ops = (SRC / 'abi/storage_ops.rs').read_text()
    body = ops[ops.index('fn storage_scan_preview('):ops.index('fn storage_access(')]
    require(body.count('self.authorization.capabilities().grant(&capability)?;') == 2,
            'scan preview and charge do not both enforce the namespace read grant')
    require('meter.charge_storage_read(page.metered_bytes())?;' in body, 'scan is not metered against the storage read class')
    print('structure: linked candidate scan, grant-bound preview and charge, storage-read metering, issuing-scan cursor binding verified')
    return 6


try:
    count = structure()
    env = dict(os.environ, CARGO_TARGET_DIR=os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/task-104.29.3'))
    base = [CARGO, 'test', '--locked', '--manifest-path', 'programs/Cargo.toml', '-p', 'layerx-programs-runtime']
    output = run(base + ['--lib', '--', 'scan::tests::'], 'scan unit suite', env)
    ran = set(re.findall(r'^test (\S+) \.\.\. ok$', output, re.M))
    require(set(UNIT_TESTS) <= ran, 'scan unit tests missing: ' + ' '.join(sorted(set(UNIT_TESTS) - ran)))
    passed, skipped = tally(output, 'unit', 1)
    count += passed
    suite = TESTS / (SUITE + '.rs')
    require(suite.is_file(), 'retained storage scan suite missing')
    declared = set(re.findall(r'#\[test\]\s*\nfn (\w+)\(', suite.read_text()))
    require(declared, 'retained storage scan suite declares no tests')
    output = run(base + ['--test', SUITE], 'storage scan integration suite', env)
    ran = set(re.findall(r'^test (\S+) \.\.\. ok$', output, re.M))
    require(declared <= ran, 'storage scan integration tests missing: ' + ' '.join(sorted(declared - ran)))
    passed, ignored = tally(output, 'integration', 1)
    count += passed
    skipped += ignored
    print('aggregate make programs-test and the cross-surface determinism differential remain release scope; not run or credited here')
    print(f'PAXEER_X_GATE tests={count} skipped={skipped}')
except Exception as error:
    print('ordered storage iteration: ' + str(error), file=sys.stderr)
    raise SystemExit(1)
PY
