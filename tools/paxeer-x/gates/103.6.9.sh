#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
exec python3 - "$root" "$@" <<'PY'
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time

ROOT = Path(sys.argv[1]).resolve()
TASK = '103.6.9'
SCHEMA = 'layerx.simulation-task-build.v1'
GATE = 'tools/paxeer-x/gates/103.6.9.sh'
WRAPPER = 'tools/runtime/run-with-clock.sh'
MANDATORY = {
    'real_node': {'real_program_simulation_executes_without_committing'},
    'core_tests': {'core_simulation_uses_lni_without_committing'},
}

RELEASE_REQUIRED = [
    'make agent-test-boundary',
    'cargo test --locked --manifest-path platform/Cargo.toml -p layerx-platform-core --all-targets -- --test-threads=1',
    'cargo clippy --locked --manifest-path platform/Cargo.toml -p layerx-platform-core --all-targets -- -D warnings',
    'make layerxd test-program-artifacts test-batch-wal-recovery',
    'cargo test --locked --manifest-path platform/Cargo.toml -p layerx-platform-agent-boundary --all-targets -- --test-threads=1',
    'cargo clippy --locked --manifest-path platform/Cargo.toml -p layerx-platform-agent-boundary --all-targets -- -D warnings',
]


def require(value, message):
    if not value:
        raise RuntimeError(message)


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def private(path, directory=False):
    info = path.lstat()
    require((stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode))
            and not path.is_symlink() and info.st_uid == os.geteuid()
            and not info.st_mode & 0o077, 'private caller-owned evidence required')


def load(path):
    path = Path(path)
    private(path)
    return json.loads(path.read_text())


def save(path, value):
    if path.exists() or path.is_symlink():
        private(path)
    with path.open('w') as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write('\n')
    path.chmod(0o600)


def git(*arguments):
    return subprocess.check_output(['git', *arguments], cwd=ROOT, text=True).strip()


def evidence():
    path = Path(os.environ.get('PAXEER_X_SIMULATION_EVIDENCE',
        '/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task10369'))
    path.mkdir(parents=True, exist_ok=True, mode=0o700)
    private(path, True)
    path = path.resolve(strict=True)
    require(path != ROOT and ROOT not in path.parents, 'evidence must remain outside checkout')
    return path


def source_snapshot():
    inventory = subprocess.check_output(['git', 'ls-files', '-z'], cwd=ROOT).split(b'\0')
    paths = {os.fsdecode(path) for path in inventory if path} | {GATE, WRAPPER}
    return {path: digest(ROOT / path) for path in sorted(paths)
            if not any(part.startswith('.env') for part in Path(path).parts)
            and Path(path).suffix in {'.rs', '.toml', '.lock', '.c', '.h', '.py', '.sh', '.json'}
            and (ROOT / path).is_file()}


def artifact(path, revision):
    path = Path(path).resolve(strict=True)
    require(path.is_file() and os.access(path, os.X_OK), 'actual executable required')
    return {'path': str(path), 'sha256': digest(path), 'source_revision': revision}


def checked(row):
    path = Path(row['path'])
    require(path.is_absolute() and path.is_file() and not path.is_symlink()
            and os.access(path, os.X_OK) and digest(path) == row['sha256'],
            'prebuilt executable missing or changed')
    return path


def native_inputs():
    supplied = os.environ.get('PAXEER_X_FOUNDATION_ARTIFACT_MANIFEST')
    require(supplied, 'PAXEER_X_FOUNDATION_ARTIFACT_MANIFEST with genuine native build provenance required')
    manifest = Path(supplied).resolve(strict=True)
    foundation = load(manifest)
    require(foundation.get('version') == 1 and foundation['build']['exit_code'] == 0,
            'successful genuine native foundation build required')
    rows = {name: foundation['artifacts'][name] for name in ('layerxd', 'layerx-genesis-build')}
    paths = {name: checked(row) for name, row in rows.items()}
    directory = paths['layerxd'].parent
    require(paths['layerx-genesis-build'].parent == directory, 'native executables must share their genuine bundle')
    if os.environ.get('LAYERX_TEST_NATIVE_BIN_DIR'):
        require(Path(os.environ['LAYERX_TEST_NATIVE_BIN_DIR']).resolve(strict=True) == directory,
                'configured native fixture differs from genuine foundation artifacts')
    return {'path': str(manifest), 'sha256': digest(manifest), 'artifacts': rows}, directory


def remaining(deadline, bound):
    seconds = min(bound, deadline - time.time())
    require(seconds > 0, 'original task deadline exhausted')
    return seconds


def command(argv, log, environment, deadline, bound):
    with log.open('w') as output:
        result = subprocess.run([str(value) for value in argv], cwd=ROOT, env=environment,
            stdin=subprocess.DEVNULL, stdout=output, stderr=subprocess.STDOUT,
            timeout=remaining(deadline, bound))
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, argv)
    return log.read_text()


def build(directory):
    require(os.geteuid() == 0, 'real root-owned native fixture isolation required')
    native, native_directory = native_inputs()
    record = directory / 'build-manifest.json'
    record.unlink(missing_ok=True)
    before = source_snapshot()
    revision, tree = git('rev-parse', 'HEAD'), git('rev-parse', 'HEAD^{tree}')
    deadline = float(os.environ.get('PAXEER_X_TASK_DEADLINE_EPOCH', str(time.time() + 1800)))
    jobs = os.environ.get('CARGO_BUILD_JOBS', '3')
    require(jobs in {'1', '2', '3', '4'}, 'bounded compiler job count required')
    environment = dict(os.environ, CARGO_BUILD_JOBS=jobs,
        CARGO_TARGET_DIR=os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/platform'),
        PATH='/root/.cargo/bin:' + os.environ.get('PATH', ''))
    cargo = os.environ.get('CARGO', '/root/.cargo/bin/cargo')
    commands = [
        [cargo, 'build', '--locked', '--manifest-path', 'platform/Cargo.toml',
         '-p', 'layerx-runtime-clock', '--bin', 'layerx-runtime-clock', '--message-format=json'],
        [cargo, 'test', '--locked', '--manifest-path', 'platform/Cargo.toml',
         '-p', 'layerx-platform-agent-boundary', '--test', 'real_node',
         '--no-run', '--message-format=json'],
        [cargo, 'test', '--locked', '--manifest-path', 'platform/Cargo.toml',
         '-p', 'layerx-platform-core', '--test', 'boundary',
         '--no-run', '--message-format=json'],
    ]
    compiler = []
    prefixes = set()
    for index, argv in enumerate(commands):
        text = command(argv, directory / ('build-' + str(index) + '.log'), environment, deadline, 1150)
        for line in text.splitlines():
            if not line.startswith('{'):
                continue
            row = json.loads(line)
            if row.get('reason') != 'compiler-artifact':
                continue
            compiler.append(row)
            manifest = Path(row['manifest_path'])
            if ROOT in manifest.parents:
                prefixes.add(str(manifest.parent.relative_to(ROOT)) + '/')
    binaries = {}
    for row in compiler:
        if not row.get('executable'):
            continue
        name, is_test = row['target']['name'], row['profile']['test']
        if name == 'layerx-runtime-clock' and not is_test:
            binaries['runtime_clock'] = artifact(row['executable'], revision)
        elif row['manifest_path'] == str(ROOT / 'platform/hosted/agent-boundary/Cargo.toml'):
            if name == 'real_node' and is_test:
                binaries['real_node'] = artifact(row['executable'], revision)
            elif name == 'layerx-agent-boundary' and not is_test:
                binaries['boundary'] = artifact(row['executable'], revision)
        elif row['manifest_path'] == str(ROOT / 'platform/hosted/core/Cargo.toml'):
            if name == 'boundary' and is_test:
                binaries['core_tests'] = artifact(row['executable'], revision)
            elif name == 'layerx-core-boundary' and not is_test:
                binaries['core'] = artifact(row['executable'], revision)
    require(set(binaries) == {'runtime_clock', 'boundary', 'real_node', 'core', 'core_tests'},
            'actual clock, both boundary services and both real-process test compiler artifacts required')
    fixed = {GATE, WRAPPER, 'platform/Cargo.toml', 'platform/Cargo.lock',
             'agent/Cargo.toml', 'agent/Cargo.lock', 'programs/Cargo.toml', 'programs/Cargo.lock',
             'human/Cargo.toml', 'human/Cargo.lock', 'interop/Cargo.toml', 'interop/Cargo.lock'}
    selected = {path: value for path, value in before.items()
        if path in fixed or path.startswith(tuple(prefixes)) or path.startswith(('.cargo/', 'include/', 'src/', 'cmd/layerxd/', 'tests/support/'))}
    require(all(digest(ROOT / path) == value for path, value in selected.items()), 'actual compiler dependency source changed')
    require(digest(native['path']) == native['sha256'], 'foundation manifest changed while compiling')
    for row in native['artifacts'].values():
        checked(row)
    save(record, {'schema': SCHEMA, 'task': TASK, 'source_revision': revision, 'source_tree': tree,
        'source_hashes': selected, 'artifacts': binaries, 'compiler_artifacts': compiler,
        'native': native, 'native_directory': str(native_directory), 'commands': commands,
        'build_exit_code': 0, 'deadline_epoch': deadline,
        'release_required': RELEASE_REQUIRED, 'release_qualification': 'UNRUN'})
    print('ARTIFACTS ' + str(record), flush=True)


def qualify(directory):
    require(os.geteuid() == 0, 'real root-owned native fixture isolation required')
    built = load(directory / 'build-manifest.json')
    require(built.get('schema') == SCHEMA and built.get('task') == TASK
            and built.get('build_exit_code') == 0, 'actual successful task build manifest required')
    require(built['source_revision'] == git('rev-parse', 'HEAD')
            and built['source_tree'] == git('rev-parse', 'HEAD^{tree}'), 'build revision or tree changed')
    require(all(digest(ROOT / path) == value for path, value in built['source_hashes'].items()),
            'actual compiled dependency source changed')
    binaries = {name: checked(row) for name, row in built['artifacts'].items()}
    require(set(binaries) == {'runtime_clock', 'boundary', 'real_node', 'core', 'core_tests'}, 'complete actual compiler artifacts required')
    native, native_directory = native_inputs()
    require(native == built['native'] and str(native_directory) == built['native_directory'],
            'genuine native fixture differs from compiled candidate')
    deadline = built['deadline_epoch']
    environment = {key: value for key, value in os.environ.items()
                   if not key.startswith('LAYERX_RUNTIME_CLOCK_')}
    environment.update(LAYERX_TEST_NATIVE_BIN_DIR=str(native_directory),
        LAYERX_RUNTIME_CLOCK_BIN=str(binaries['runtime_clock']),
        LAYERX_RUNTIME_CLOCK_DIRECTORY='/tmp',
        PATH='/root/.cargo/bin:' + os.environ.get('PATH', ''))
    executed = {}
    for suite, required_cases in MANDATORY.items():
        inventory = subprocess.run([str(binaries[suite]), '--list', '--format', 'terse'], cwd=ROOT,
            env=environment, stdin=subprocess.DEVNULL, capture_output=True, text=True,
            check=True, timeout=remaining(deadline, 30))
        names = re.findall(r'^(.+): test$', inventory.stdout, re.M)
        require(len(names) == len(set(names)) and required_cases <= set(names),
                'mandatory genuine simulation cases must be declared')
        save(directory / (suite + '-inventory.json'), {'task_cases': sorted(required_cases),
            'release_only_additional_cases': sorted(set(names) - required_cases),
            'release_required': RELEASE_REQUIRED, 'release_qualification': 'UNRUN'})
        argv = [ROOT / WRAPPER, binaries[suite], *sorted(required_cases),
                '--exact', '--nocapture', '--test-threads=1']
        log = directory / (suite + '-simulation.log')
        text = command(argv, log, environment, deadline, 1700)
        print(text, end='', flush=True)
        passed = re.findall(r'^test ([^\s]+) \.\.\. ok$', text, re.M)
        require(len(passed) == len(set(passed)) and set(passed) == required_cases,
                'each mandatory genuine simulation case must pass exactly once')
        summaries = re.findall(r'^test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;[^\n]*$', text, re.M)
        require(len(summaries) == 1 and tuple(map(int, summaries[0])) == (len(required_cases), 0, 0),
                'complete focused non-skipped simulation result required')
        executed[suite] = sorted(required_cases)
    require(all(digest(ROOT / path) == value for path, value in built['source_hashes'].items()),
            'source changed during actual qualification')
    for row in [*built['artifacts'].values(), *native['artifacts'].values()]:
        checked(row)
    save(directory / 'result.json', {'task': TASK, 'revision': built['source_revision'],
        'command': 'timeout 30m tools/paxeer-x/verify-task.sh 103.6.9', 'exit_code': 0,
        'log_path': str(directory), 'executed_tests': executed, 'skipped': 0,
        'release_required': RELEASE_REQUIRED, 'release_qualification': 'UNRUN'})
    print(f'PAXEER_X_GATE tests={sum(map(len, MANDATORY.values()))} skipped=0', flush=True)


def main():
    directory = evidence()
    args = sys.argv[2:]
    require(args in ([], ['--build']), '103.6.9 accepts only --build or no selector arguments')
    if args:
        build(directory)
    else:
        qualify(directory)


try:
    main()
except subprocess.CalledProcessError as error:
    print('103.6.9 command exit=' + str(error.returncode), file=sys.stderr)
    sys.exit(error.returncode if error.returncode > 0 else 1)
except subprocess.TimeoutExpired:
    print('103.6.9 original bounded task deadline or command timeout', file=sys.stderr)
    sys.exit(124)
except (RuntimeError, OSError, KeyError, ValueError, TypeError) as error:
    print('103.6.9 prerequisite or artifact binding refused: ' + str(error), file=sys.stderr)
    sys.exit(78)
PY
