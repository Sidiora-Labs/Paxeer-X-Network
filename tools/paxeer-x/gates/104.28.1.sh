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
EXTERNAL_ARTIFACT_SUITES = {'interpreter_program', 'program_spend_composition'}
UNITS = {
    'abi/mod.rs': [r'^pub struct Abi \{', r'^pub struct AbiEffects \{', r'^pub struct AbiCommit \{',
                   r'^pub enum AbiError \{', r'^pub use capability::\{Capability, CapabilitySet\};'],
    'abi/capability.rs': [r'^pub enum Capability \{', r'^pub struct CapabilitySet\(',
                          r'^    pub fn narrow\(', r'^    pub\(crate\) fn narrow_for_program_edge\('],
    'abi/storage_ops.rs': [r'^impl Abi \{', r'^    pub fn storage_read\(', r'^    pub fn storage_write\(',
                           r'^    pub fn storage_delete\('],
    'host/mod.rs': [r'^pub\(crate\) struct RuntimeState \{', r'^pub\(crate\) fn linker\(',
                    r'^pub\(crate\) mod memory;', r'^mod storage;', r'^mod events;', r'^mod calls;', r'^mod transfer;'],
    'host/memory.rs': [r'^pub\(crate\) fn read_guest\(', r'^pub\(crate\) fn write_guest\(',
                       r'^pub\(super\) fn validate_output\('],
    'host/storage.rs': [r'^pub\(super\) fn register\(', r'"storage_read"'],
    'host/events.rs': [r'^pub\(super\) fn register\(', r'"event_emit"'],
    'host/calls.rs': [r'^pub\(super\) fn register\(', r'"program_call"'],
    'host/transfer.rs': [r'^pub\(super\) fn register\(', r'"transfer_402"'],
}
SOLE_OWNER = {r'^pub\(crate\) struct RuntimeState\b': 'host/mod.rs', r'^pub struct Abi \{': 'abi/mod.rs',
              r'^pub struct CapabilitySet\(': 'abi/capability.rs'}
MAP = ['# Module map', 'abi/mod.rs', 'abi/capability.rs', 'abi/storage_ops.rs', 'host/mod.rs',
       'host/memory.rs', 'host/{storage,events,calls,transfer}.rs', 'RuntimeState']


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def run(argv, label, env=None, seconds=1500):
    result = subprocess.run(argv, cwd=ROOT, env=env, stdin=subprocess.DEVNULL, capture_output=True,
                            text=True, timeout=seconds)
    sys.stdout.write(result.stdout)
    sys.stdout.write(result.stderr)
    require(result.returncode == 0, f'{label} exit {result.returncode}')
    return result.stdout + result.stderr


def layout():
    require(not (SRC / 'abi.rs').exists() and not (SRC / 'host.rs').exists(), 'legacy monolithic abi.rs or host.rs remains')
    for unit, patterns in UNITS.items():
        text = (SRC / unit).read_text()
        for pattern in patterns:
            require(re.search(pattern, text, re.M), f'{unit} does not own {pattern}')
    for pattern, owner in SOLE_OWNER.items():
        owners = sorted(str(path.relative_to(SRC)) for path in SRC.rglob('*.rs') if re.search(pattern, path.read_text(), re.M))
        require(owners == [owner], f'{pattern} owned by {owners}, expected only {owner}')
    lib = (SRC / 'lib.rs').read_text()
    require(all(entry in lib for entry in MAP), 'runtime crate documentation lacks the module map')
    print(f'layout: {len(UNITS)} units and {len(SOLE_OWNER)} sole owners match the module map')
    return sum(len(patterns) for patterns in UNITS.values()) + len(SOLE_OWNER) + 1


def tally(output, label, expected_binaries):
    results = re.findall(r'^test result: (\w+)\. (\d+) passed; (\d+) failed; (\d+) ignored;', output, re.M)
    require(len(results) == expected_binaries, f'{label}: {len(results)} test binaries reported, expected {expected_binaries}')
    require(all(state == 'ok' and failed == '0' for state, _, failed, _ in results), f'{label}: failing test binary')
    passed = sum(int(item[1]) for item in results)
    ignored = sum(int(item[3]) for item in results)
    require(passed > 0, f'{label}: no test ran')
    return passed, ignored


try:
    count = layout()
    run(['sh', 'tests/programs/runtime-module-boundaries.sh'], 'module-boundary lint')
    count += 1
    run(['tests/programs/check-abi-drift.sh'], 'frozen ABI drift check')
    count += 1
    env = dict(os.environ, CARGO_TARGET_DIR=os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/task-104.28.1'))
    base = [CARGO, 'test', '--locked', '--manifest-path', 'programs/Cargo.toml', '-p', 'layerx-programs-runtime']
    output = run(base + ['--lib', '--', 'abi::', 'host::'], 'abi and host unit suites', env)
    ran = re.findall(r'^test (\S+) \.\.\. ok$', output, re.M)
    require(any(name.startswith('abi::') for name in ran) and any(name.startswith('host::') for name in ran), 'abi or host unit suite missing')
    passed, skipped = tally(output, 'unit', 1)
    count += passed
    suites = sorted(path.stem for path in TESTS.glob('*.rs'))
    require(EXTERNAL_ARTIFACT_SUITES <= set(suites) and {'isolation', 'abi_linker', 'composition', 'monetary_law'} <= set(suites), 'retained suite inventory changed')
    selected = [suite for suite in suites if suite not in EXTERNAL_ARTIFACT_SUITES]
    output = run(base + [arg for suite in selected for arg in ('--test', suite)], 'existing integration suites', env)
    passed, ignored = tally(output, 'integration', len(selected))
    count += passed
    skipped += ignored
    print('external-artifact suites ' + ' '.join(sorted(EXTERNAL_ARTIFACT_SUITES)) + ' and aggregate make programs-test remain release scope; not run or credited here')
    print(f'PAXEER_X_GATE tests={count} skipped={skipped}')
except Exception as error:
    print('runtime module split: ' + str(error), file=sys.stderr)
    raise SystemExit(1)
PY
