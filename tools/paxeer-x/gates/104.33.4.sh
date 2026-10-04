#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
exec python3 - "$root" "$@" <<'PY'
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import time

ROOT = Path(sys.argv[1]).resolve()
TASK = '104.33.4'
GATE = 'tools/paxeer-x/gates/104.33.4.sh'
INTERPRETER = 'programs/crates/layerx-programs-interpreter/'
RUNTIME = 'programs/crates/layerx-programs-runtime/'
MINIMUM = {
    'unit': {'tests::frozen_success_encodings_are_canonical'},
    'conformance': {'fixed_opcode_oracle_freezes_every_success_effect_and_step',
        'fixed_refusals_cover_structure_arithmetic_amount_and_depth_without_effects',
        'complete_submission_validation_refuses_unbounded_and_noncanonical_data'},
    'runtime': {'built_interpreter_runs_success_vectors_through_the_real_candidate_runtime',
        'built_interpreter_refusals_leave_real_runtime_state_and_effects_empty',
        'built_interpreter_refuses_absent_foreign_and_insufficient_transfer_grants',
        'built_interpreter_storage_reads_and_writes_stay_in_program_principal_namespace',
        'built_interpreter_repeated_wasm_execution_has_identical_records_and_resource_usage'},
}
RELEASE_REQUIRED = ['make programs-test', 'Programs differential, aggregate and strict lint matrices']


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def private(path, directory=False):
    info = path.lstat()
    require((stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode))
        and not path.is_symlink() and info.st_uid == os.geteuid() and not info.st_mode & 0o077,
        'private caller-owned evidence required')


def save(path, value):
    if path.exists() or path.is_symlink():
        private(path)
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')
    path.chmod(0o600)


def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT, text=True).strip()


def snapshot():
    paths = subprocess.check_output(['git', 'ls-files', '-z'], cwd=ROOT).split(b'\0')
    return {os.fsdecode(path): digest(ROOT / os.fsdecode(path)) for path in paths if path
        and (os.fsdecode(path) == GATE or os.fsdecode(path).startswith(('programs/', '.cargo/')))
        and not any(part.startswith('.env') for part in Path(os.fsdecode(path)).parts)
        and Path(os.fsdecode(path)).suffix in {'.rs', '.toml', '.lock', '.c', '.h', '.json', '.kvx', '.hex', '.sh'}
        and (ROOT / os.fsdecode(path)).is_file()}


def remaining(deadline, bound):
    value = min(bound, deadline - time.time())
    require(value > 0, 'original task cutoff reached')
    return value


def command(argv, log, environment, deadline, bound=1150):
    with log.open('w') as output:
        result = subprocess.run([str(value) for value in argv], cwd=ROOT, env=environment,
            stdin=subprocess.DEVNULL, stdout=output, stderr=subprocess.STDOUT,
            timeout=remaining(deadline, bound))
    log.chmod(0o600)
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, argv)
    return log.read_text()


def capture(path, directory, revision, executable):
    source = Path(path).resolve(strict=True)
    require(source.is_file() and (not executable or os.access(source, os.X_OK)), 'actual compiler artifact required')
    expected = digest(source)
    target = directory / source.name
    require(not target.exists() and not target.is_symlink(), 'fresh artifact destination required')
    shutil.copyfile(source, target)
    target.chmod(0o700 if executable else 0o600)
    require(digest(source) == expected == digest(target), 'compiler artifact changed while capturing')
    return {'path': str(target), 'sha256': expected, 'source_revision': revision, 'executable': executable}


def checked(row):
    path = Path(row['path'])
    private(path)
    require(digest(path) == row['sha256'] and (not row['executable'] or os.access(path, os.X_OK)),
        'immutable compiler artifact changed')
    return path


def build(directory):
    require(not git('status', '--porcelain', '--untracked-files=no'), 'clean published source required')
    before = snapshot()
    revision, tree = git('rev-parse', 'HEAD'), git('rev-parse', 'HEAD^{tree}')
    deadline = float(os.environ.get('PAXEER_X_TASK_DEADLINE_EPOCH', str(time.time() + 1800)))
    jobs = os.environ.get('CARGO_BUILD_JOBS', '3')
    require(jobs in {'1', '2', '3', '4'}, 'bounded compiler job count required')
    environment = dict(os.environ, CARGO_BUILD_JOBS=jobs,
        CARGO_TARGET_DIR=os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/programs'),
        PATH='/root/.cargo/bin:' + os.environ.get('PATH', ''))
    cargo = os.environ.get('CARGO', '/root/.cargo/bin/cargo')
    commands = [
        [cargo, 'build', '--locked', '--offline', '--manifest-path', 'programs/Cargo.toml',
         '-p', 'layerx-programs-interpreter', '--release', '--target', 'wasm32-unknown-unknown', '--message-format=json'],
        [cargo, 'test', '--locked', '--offline', '--manifest-path', 'programs/Cargo.toml',
         '-p', 'layerx-programs-interpreter', '--lib', '--test', 'conformance', '--no-run', '--message-format=json'],
        [cargo, 'test', '--locked', '--offline', '--manifest-path', 'programs/Cargo.toml',
         '-p', 'layerx-programs-runtime', '--test', 'interpreter_program', '--no-run', '--message-format=json'],
    ]
    binaries, prefixes, compiler = {}, set(), []
    capture_dir = directory / 'compiled'
    capture_dir.mkdir(mode=0o700)
    lock_path = Path('/root/lx-cargo/interpreter-build.lock'
        if environment['CARGO_TARGET_DIR'] == '/root/lx-target/arbiter-prestate/rust'
        else '/root/lx-cargo/native-build.lock')
    lock_path.parent.mkdir(parents=True, exist_ok=True)
    with lock_path.open('a') as lock:
        while True:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError:
                time.sleep(min(0.2, remaining(deadline, 0.2)))
        for index, argv in enumerate(commands):
            text = command(argv, directory / f'build-{index}.log', environment, deadline)
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
                if index == 0 and row['target']['name'] == 'layerx_programs_interpreter':
                    wasm = [value for value in row['filenames'] if value.endswith('.wasm')]
                    require(len(wasm) == 1, 'actual ordinary interpreter Wasm artifact required')
                    binaries['wasm'] = capture(wasm[0], capture_dir, revision, False)
                elif row.get('executable') and row['profile']['test']:
                    name = row['target']['name']
                    key = {'layerx_programs_interpreter': 'unit', 'conformance': 'conformance',
                           'interpreter_program': 'runtime'}.get(name)
                    if key:
                        require(key not in binaries, 'duplicate declared test artifact')
                        binaries[key] = capture(row['executable'], capture_dir, revision, True)
    require(set(binaries) == {'wasm', 'unit', 'conformance', 'runtime'}, 'complete genuine interpreter artifact set required')
    fixed = {GATE, 'programs/Cargo.toml', 'programs/Cargo.lock', 'programs/.cargo/config.toml'}
    selected = {path: value for path, value in before.items() if path in fixed
        or path.startswith(tuple(prefixes)) or path.startswith((INTERPRETER, RUNTIME, 'programs/sdk/', 'programs/vendor/'))}
    require(all(digest(ROOT / path) == value for path, value in selected.items()), 'actual build source changed')
    save(directory / 'build-manifest.json', {'schema': 'layerx.interpreter-task-build.v1', 'task': TASK,
        'source_revision': revision, 'source_tree': tree, 'source_hashes': selected,
        'artifacts': binaries, 'compiler_artifacts': compiler, 'commands': commands,
        'build_exit': 0, 'deadline_epoch': deadline, 'release_required': RELEASE_REQUIRED,
        'release_qualification': 'UNRUN'})
    print('ARTIFACTS ' + str(directory / 'build-manifest.json'), flush=True)


def qualify(directory):
    record = directory / 'build-manifest.json'
    private(record)
    built = json.loads(record.read_text())
    require(built['schema'] == 'layerx.interpreter-task-build.v1' and built['task'] == TASK
        and built['build_exit'] == 0, 'actual successful interpreter build required')
    require(built['source_revision'] == git('rev-parse', 'HEAD') and built['source_tree'] == git('rev-parse', 'HEAD^{tree}'),
        'compiled revision or tree changed')
    require(all(digest(ROOT / path) == value for path, value in built['source_hashes'].items()), 'compiled source changed')
    binaries = {key: checked(row) for key, row in built['artifacts'].items()}
    require(set(binaries) == {'wasm', 'unit', 'conformance', 'runtime'}, 'complete captured interpreter artifacts required')
    environment = dict(os.environ, LAYERX_INTERPRETER_WASM=str(binaries['wasm']))
    deadline, executed, total = built['deadline_epoch'], {}, 0
    for suite, minimum in MINIMUM.items():
        inventory = subprocess.check_output([str(binaries[suite]), '--list', '--format', 'terse'], cwd=ROOT,
            env=environment, text=True, timeout=remaining(deadline, 30))
        names = re.findall(r'^(.+): test$', inventory, re.M)
        require(names and len(names) == len(set(names)) and minimum <= set(names), 'complete scoped real test inventory required')
        argv = [binaries[suite], '--nocapture', '--test-threads=1']
        text = command(argv, directory / (suite + '.log'), environment, deadline, 1700)
        print(text, end='', flush=True)
        passed = re.findall(r'^test ([^\s]+) \.\.\. ok$', text, re.M)
        summary = re.findall(r'^test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;[^\n]*$', text, re.M)
        require(len(passed) == len(set(passed)) and set(passed) == set(names), 'every scoped interpreter test must pass once')
        require(len(summary) == 1 and tuple(map(int, summary[0])) == (len(names), 0, 0), 'non-skipped complete interpreter result required')
        executed[suite] = names
        total += len(names)
    require(all(digest(ROOT / path) == value for path, value in built['source_hashes'].items()), 'source changed during qualification')
    for row in built['artifacts'].values():
        checked(row)
    save(directory / 'result.json', {'task': TASK, 'revision': built['source_revision'],
        'command': 'timeout 30m tools/paxeer-x/verify-task.sh 104.33.4', 'exit_code': 0,
        'log_path': str(directory), 'executed_tests': executed, 'tests': total, 'skipped': 0,
        'release_required': RELEASE_REQUIRED, 'release_qualification': 'UNRUN'})
    print(f'PAXEER_X_GATE tests={total} skipped=0', flush=True)


try:
    args = sys.argv[2:]
    require(args in ([], ['--build']), 'only --build or the declared verify selector is admitted')
    directory = Path(os.environ.get('PAXEER_X_INTERPRETER_EVIDENCE',
        '/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task104334'))
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    private(directory, True)
    directory = directory.resolve(strict=True)
    require(ROOT != directory and ROOT not in directory.parents, 'private evidence outside checkout required')
    build(directory) if args else qualify(directory)
except subprocess.CalledProcessError as error:
    print('104.33.4 command exit=' + str(error.returncode), file=sys.stderr)
    sys.exit(error.returncode if error.returncode > 0 else 1)
except subprocess.TimeoutExpired:
    print('104.33.4 original cutoff or bounded command reached', file=sys.stderr)
    sys.exit(124)
except (RuntimeError, OSError, KeyError, ValueError, TypeError) as error:
    print('104.33.4 prerequisite or artifact binding refused: ' + str(error), file=sys.stderr)
    sys.exit(78)
PY
