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
UNIT_SCOPES = ['abi::', 'calls::', 'engine::', 'host::', 'validate::']
INVARIANTS = ['validate::linker_invariant_tests::nested_resolution_reuses_the_engine_owned_linker',
              'validate::linker_invariant_tests::nested_calls_reuse_the_engine_linker_without_constructing_one',
              'validate::linker_invariant_tests::deep_composition_measures_the_avoided_per_frame_linker_rebuild']
MEASUREMENT = re.compile(r'^test validate::linker_invariant_tests::deep_composition_measures_the_avoided_per_frame_linker_rebuild \.\.\. linker-hoist deep-composition frames=(\d+) hoisted_ns=(\d+) shared_instantiation_ns=(\d+) '
                         r'rebuilt_instantiation_ns=(\d+) avoided_ns=(\d+) per_call_rebuild_ns=(\d+) speedup_permille=(\d+)\nok$', re.M)


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
    sources = {str(path.relative_to(SRC)): path.read_text() for path in SRC.rglob('*.rs')}
    builders = sorted(name for name, text in sources.items() if 'Linker::new(' in text)
    require(builders == ['host/mod.rs'], f'host linker registration outside host/mod.rs: {builders}')
    host = sources['host/mod.rs']
    require(host.count('Linker::new(') == 1, 'host/mod.rs registers the host surface more than once')
    require(re.search(r'^pub\(crate\) fn linker\(engine: &Engine\) -> Result<HostLinker, ExecutionFault> \{\n'
                      r'    LINKER_CONSTRUCTIONS\.with\(', host, re.M), 'linker construction is not counted')
    callers = sorted(name for name, text in sources.items() if re.search(r'\bhost::linker\(', text))
    require(callers == ['engine.rs', 'qualification.rs', 'validate.rs'], f'unexpected host linker constructors: {callers}')
    require(re.search(r'fn construct_host_linker\(engine: &Engine\)', sources['engine.rs'])
            and re.search(r'let linker = construct_host_linker\(&engine\)\?;', sources['engine.rs']),
            'engine does not build its linker exactly once at construction')
    validate = sources['validate.rs']
    production, _, tests = validate.partition('#[cfg(test)]\nmod linker_invariant_tests')
    require('host::linker(' not in production, 'validation constructs a linker outside the engine')
    require('host::linker(engine.inner())' in tests, 'measurement does not rebuild through the production constructor')
    require('let linker = engine.host_linker();' in production, 'validated modules do not share the engine linker')
    require(production.count('self\n            .linker\n            .instantiate(') + production.count('self.linker.instantiate(') >= 3,
            'instantiation paths do not use the shared linker')
    impl = re.search(r'^impl HostLinker \{\n(.*?)^\}', host, re.M | re.S)
    require(impl and '&mut self' not in impl.group(1), 'host linker exposes mutation after construction')
    require(re.search(r'^pub\(crate\) struct HostLinker \{\n    linker: Linker<RuntimeState>,', host, re.M),
            'per-execution state is not confined to the store')
    calls = sources['calls.rs']
    require('Linker' not in calls and 'instantiate_composed' in calls, 'nested calls do not reuse the validated module linker')
    print('structure: one registration site, engine-owned immutable linker, shared nested instantiation')
    return 9


try:
    count = structure()
    env = dict(os.environ, CARGO_TARGET_DIR=os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/task-104.32.2'))
    base = [CARGO, 'test', '--locked', '--manifest-path', 'programs/Cargo.toml', '-p', 'layerx-programs-runtime']
    output = run(base + ['--lib', '--', 'validate::linker_invariant_tests', '--nocapture', '--test-threads=1'],
                 'linker invariant and deep-composition measurement', env)
    ran = set(re.findall(r'^test (\S+) \.\.\. (?:linker-hoist [^\n]*\n)?ok$', output, re.M))
    require(all(name in ran for name in INVARIANTS), 'linker invariant test missing')
    found = MEASUREMENT.findall(output)
    require(len(found) == 1, 'deep-composition measurement not reported')
    frames, hoisted, shared, rebuilt, avoided, rebuild, permille = (int(value) for value in found[0])
    require(frames == 9 and hoisted > 0 and rebuilt > shared and avoided > 0 and rebuild > hoisted and permille > 1000,
            'deep-composition measurement shows no improvement')
    passed, skipped = tally(output, 'invariant', 1)
    count += passed
    suites = sorted(path.stem for path in TESTS.glob('*.rs'))
    require(EXTERNAL_ARTIFACT_SUITES <= set(suites) and {'abi_linker', 'composition', 'determinism', 'step_conformance'} <= set(suites),
            'retained conformance suite inventory changed')
    selected = [suite for suite in suites if suite not in EXTERNAL_ARTIFACT_SUITES]
    output = run(base + ['--lib', '--'] + UNIT_SCOPES, 'runtime linker unit suites', env)
    ran = re.findall(r'^test (\S+) \.\.\. ok$', output, re.M)
    require(all(any(name.startswith(scope) for name in ran) for scope in UNIT_SCOPES), 'runtime linker unit scope missing')
    passed, ignored = tally(output, 'unit', 1)
    count += passed
    skipped += ignored
    output = run(base + ['--no-fail-fast'] + [arg for suite in selected for arg in ('--test', suite)],
                 'runtime conformance suite', env)
    passed, ignored = tally(output, 'conformance', len(selected))
    count += passed
    skipped += ignored
    print('external-artifact suites ' + ' '.join(sorted(EXTERNAL_ARTIFACT_SUITES)) + ', the guest-artifact market lib suite, remaining lib modules outside the linker scope, programs-interpreter-bench and aggregate make programs-test remain release scope; not run or credited here')
    print(f'PAXEER_X_GATE tests={count} skipped={skipped}')
except Exception as error:
    print('host linker hoist: ' + str(error), file=sys.stderr)
    raise SystemExit(1)
PY
