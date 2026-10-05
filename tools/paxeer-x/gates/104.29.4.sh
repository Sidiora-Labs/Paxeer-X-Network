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
SUITE = 'namespace_drop'
MANIFEST_ENTRY = '\\0storage_drop_scoped(i32)->i32\\0'
UNIT = {
    'storage::reclaim::tests::drop_removes_every_cell_of_one_namespace_and_no_byte_adjacent_namespace': 'no cell survives a drop and no adjacent namespace is touched, on full plane contents',
    'storage::reclaim::tests::drop_of_an_absent_namespace_is_exactly_zero_and_leaves_the_plane_unchanged': 'an empty namespace drop reclaims and meters exactly zero',
    'storage::reclaim::tests::replay_fields_must_carry_the_exact_metered_work_of_the_reclamation': 'replayed drop facts carry the exact cell-plus-byte work',
    'occupancy::tests::property_drop_charges_through_commit_and_never_after': 'released occupancy is credited against the rent class from the drop batch on',
    'occupancy::tests::gaps_refuse_and_committed_drop_stops_future_accrual': 'a committed drop stops future occupancy accrual',
}
INTEGRATION = {
    'candidate_drop_of_empty_namespace_records_zero_provisional_reclamation_fact': 'drop of an empty namespace',
    'candidate_drop_reclaims_every_cell_in_the_sixty_four_cell_boundary_fixture': 'drop of a namespace at the cell boundary fixture',
    'candidate_drop_then_write_and_write_then_drop_have_deterministic_ordering': 'drop followed by a write in the same activity',
    'candidate_later_fault_discards_namespace_drop_and_provisional_reclamation_fact': 'a later fault discards the drop exactly as it discards a write',
    'candidate_later_typed_host_refusal_discards_namespace_drop_and_effects': 'a later typed refusal discards the drop and its effects',
    'candidate_denied_and_invalid_selectors_do_not_reclaim_or_meter': 'a namespace the program does not hold is refused before reclamation or metering',
    'candidate_drop_meter_is_exact_and_one_past_refuses_before_mutation': 'drop cost is metered against the declared budget, one past refuses before mutation',
    'candidate_drop_meter_distinguishes_cells_and_key_value_bytes': 'drop cost tracks cells and bytes reclaimed, not a constant',
    'candidate_drop_preserves_every_adjacent_program_principal_and_scope': 'adjacent programs, principals and scopes survive a drop',
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


def tally(output, label, expected_binaries):
    results = re.findall(r'^test result: (\w+)\. (\d+) passed; (\d+) failed; (\d+) ignored;', output, re.M)
    require(len(results) == expected_binaries, f'{label}: {len(results)} test binaries reported, expected {expected_binaries}')
    require(all(state == 'ok' and failed == '0' for state, _, failed, _ in results), f'{label}: failing test binary')
    passed = sum(int(item[1]) for item in results)
    require(passed > 0, f'{label}: no test ran')
    return passed, sum(int(item[3]) for item in results)


def passed_names(output):
    return set(re.findall(r'^test (\S+) \.\.\. ok$', output, re.M))


def ordered(text, needles, reason):
    positions = [text.find(needle) for needle in needles]
    require(all(position >= 0 for position in positions) and positions == sorted(positions), reason)


def production_path():
    lib = (SRC / 'lib.rs').read_text()
    require(MANIFEST_ENTRY in lib, 'namespace drop host function absent from the ABI manifest')
    host = (SRC / 'host/storage.rs').read_text()
    require('"storage_drop_scoped"' in host and 'abi.storage_drop_selected(meter, selected)' in host,
            'namespace drop host function is not linked to the ABI operation')
    ops = (SRC / 'abi/storage_ops.rs').read_text()
    start = ops.index('    pub fn storage_drop_selected(')
    body = ops[start:ops.index('\n    }\n', start)]
    ordered(body, ['self.storage_access(selector, true);',
                   'self.authorization.capabilities().grant(&capability)?;',
                   'enforce_storage_prefix(namespace, AccessMode::Write, &[])',
                   'self.storage.namespace_drop_preview(namespace)?;',
                   'meter.charge_storage_write(drop.metered_work())?;',
                   'self.storage.reclaim_namespace(drop);',
                   'self.effects.namespace_drops.push(drop);'],
            'drop does not refuse ungranted namespaces, then preview, charge, reclaim and record in that order')
    reclaim = (SRC / 'storage/reclaim.rs').read_text()
    require(re.search(r'let metered_work = reclaimed_cells\s*\.checked_add\(reclaimed_key_value_bytes\)', reclaim),
            'drop is not metered by the cells and bytes reclaimed')
    require(re.search(r'namespace_cells\(cells, drop\.namespace\)', reclaim) and 'cells.remove(&address);' in reclaim,
            'reclamation is not bounded to exactly the dropped namespace')
    host_state = (SRC / 'abi/host_state.rs').read_text()
    require('for drop in &abi.effects.namespace_drops {' in host_state and
            'write(&drop.metered_work().to_be_bytes())?;' in host_state,
            'drop facts are not committed with the activity host state')
    print('production path: linked candidate drop, grant-bound refusal before preview, cell-plus-byte metering before reclamation, exact namespace removal, committed drop facts')
    return 6


try:
    count = production_path()
    env = dict(os.environ, CARGO_TARGET_DIR=os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/task-104.29.4'))
    base = [CARGO, 'test', '--locked', '--manifest-path', 'programs/Cargo.toml', '-p', 'layerx-programs-runtime']
    output = run(base + ['--lib', '--', 'storage::reclaim::', 'occupancy::tests::property_drop_charges_through_commit_and_never_after',
                         'occupancy::tests::gaps_refuse_and_committed_drop_stops_future_accrual'], 'drop unit suites', env)
    missing = sorted(name for name in UNIT if name not in passed_names(output))
    require(not missing, 'unit acceptance tests did not pass: ' + ' '.join(missing))
    passed, skipped = tally(output, 'unit', 1)
    count += passed
    suite = TESTS / (SUITE + '.rs')
    require(suite.is_file(), 'retained namespace drop suite missing')
    declared = set(re.findall(r'#\[test\]\s*\nfn (\w+)\(', suite.read_text()))
    require(set(INTEGRATION) <= declared, 'retained namespace drop suite inventory changed')
    output = run(base + ['--test', SUITE], 'namespace drop integration suite', env)
    missing = sorted(declared - passed_names(output))
    require(not missing, 'namespace drop integration tests did not pass: ' + ' '.join(missing))
    passed, ignored = tally(output, 'integration', 1)
    count += passed
    skipped += ignored
    for name, criterion in {**UNIT, **INTEGRATION}.items():
        print(f'criterion: {criterion}: {name} ok')
    print('aggregate make programs-test remains release scope; not run or credited here')
    print(f'PAXEER_X_GATE tests={count} skipped={skipped}')
except Exception as error:
    print('namespace drop and reclamation: ' + str(error), file=sys.stderr)
    raise SystemExit(1)
PY
