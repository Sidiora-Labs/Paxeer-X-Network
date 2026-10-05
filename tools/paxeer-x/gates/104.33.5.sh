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
TASK = '104.33.5'
GATE = 'tools/paxeer-x/gates/104.33.5.sh'
BENCH = 'programs/benches/interpreter.rs'
GUIDE = 'platform/docs/content/guide/programs.md'
WORKLOADS = ('arithmetic-store', 'integer-suite', 'bounded-control', 'storage-control-transfer')
WASM = {'interpreter': ('layerx-programs-interpreter', 'layerx_programs_interpreter'),
        'compiled': ('layerx-interpreter-compiled-equivalent', 'layerx_interpreter_compiled_equivalent')}
USAGE = re.compile(r'^route=(interpreted|compiled) workload=(\S+) cpu_fuel=(\d+) memory_bytes=(\d+) '
    r'storage_read_bytes=(\d+) storage_write_bytes=(\d+) output_values=(\d+) output_bytes=(\d+) '
    r'occupancy_byte_batches=(\d+) occupancy_fee_units=(\d+) fee_units=(\d+)$', re.M)
TIMING = re.compile(r'^timing workload=(\S+) interpreted_median_ns=(\d+) compiled_median_ns=(\d+)$', re.M)
TIME = re.compile(r'^interpreter_time_overhead observed_bps=(\d+) published_bps=(\d+) tolerance_bps=(\d+) '
    r'gate_bps=(\d+) samples=(\d+)$', re.M)
FEE = re.compile(r'^interpreter_fee_overhead observed_bps=(\d+) published_bps=(\d+) tolerance_bps=(\d+) gate_bps=(\d+)$', re.M)
RELEASE_REQUIRED = ['make programs-bench on fixed release hardware with recorded conditions',
    'make programs-test', 'Programs differential, aggregate and strict lint matrices']


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
        and (os.fsdecode(path) in {GATE, GUIDE, 'Makefile'} or os.fsdecode(path).startswith(('programs/', '.cargo/')))
        and not any(part.startswith('.env') for part in Path(os.fsdecode(path)).parts)
        and (os.fsdecode(path) in {GUIDE, 'Makefile'}
             or Path(os.fsdecode(path)).suffix in {'.rs', '.toml', '.lock', '.c', '.h', '.json', '.kvx', '.hex', '.sh'})
        and (ROOT / os.fsdecode(path)).is_file()}


def remaining(deadline, bound):
    value = min(bound, deadline - time.time())
    require(value > 0, 'original task cutoff reached')
    return value


def command(argv, log, environment, deadline, bound=1150, cwd=None):
    with log.open('w') as output:
        result = subprocess.run([str(value) for value in argv], cwd=cwd or ROOT, env=environment,
            stdin=subprocess.DEVNULL, stdout=output, stderr=subprocess.STDOUT,
            timeout=remaining(deadline, bound))
    log.chmod(0o600)
    if result.returncode:
        print(log.read_text(), end='', file=sys.stderr, flush=True)
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
    commands = [[cargo, 'build', '--locked', '--offline', '--manifest-path', 'programs/Cargo.toml', '-p', package,
                 '--release', '--target', 'wasm32-unknown-unknown', '--message-format=json']
                for package, _ in WASM.values()]
    commands.append([cargo, 'bench', '--locked', '--offline', '--manifest-path', 'programs/Cargo.toml',
                     '-p', 'layerx-programs-runtime', '--bench', 'interpreter', '--no-run', '--message-format=json'])
    binaries, compiler = {}, []
    capture_dir = directory / ('compiled-' + revision[:12] + '-' + str(time.time_ns()))
    capture_dir.mkdir(mode=0o700)
    lock_path = Path('/root/lx-cargo/native-build.lock')
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
                name = row['target']['name']
                for key, (_, crate) in WASM.items():
                    if index < len(WASM) and name == crate:
                        wasm = [value for value in row['filenames'] if value.endswith('.wasm')]
                        require(len(wasm) == 1 and key not in binaries, 'one ordinary ABI-v2 Wasm artifact per route required')
                        binaries[key] = capture(wasm[0], capture_dir, revision, False)
                if index == len(WASM) and name == 'interpreter' and row['target']['kind'] == ['bench'] and row.get('executable'):
                    require('bench' not in binaries, 'duplicate interpreter bench artifact')
                    binaries['bench'] = capture(row['executable'], capture_dir, revision, True)
    require(set(binaries) == {'interpreter', 'compiled', 'bench'}, 'complete genuine interpreter pricing artifact set required')
    require(snapshot() == before, 'actual build source changed')
    save(directory / 'build-manifest.json', {'schema': 'layerx.interpreter-pricing-build.v1', 'task': TASK,
        'source_revision': revision, 'source_tree': tree, 'source_hashes': before,
        'artifacts': binaries, 'compiler_artifacts': compiler, 'commands': commands,
        'build_exit': 0, 'deadline_epoch': deadline, 'release_required': RELEASE_REQUIRED,
        'release_qualification': 'UNRUN'})
    print('ARTIFACTS ' + str(directory / 'build-manifest.json'), flush=True)


def constant(source, name):
    found = re.findall(r'^const ' + name + r': u128 = ([0-9_]+);$', source, re.M)
    require(len(found) == 1, 'declared bench constant ' + name + ' required')
    return int(found[0].replace('_', ''))


def published():
    source = (ROOT / BENCH).read_text()
    declared = {name: constant(source, name) for name in
                ('PUBLISHED_TIME_MULTIPLIER_BPS', 'PUBLISHED_FEE_MULTIPLIER_BPS', 'TOLERANCE_BPS', 'BPS')}
    require(declared['BPS'] == 10_000 and declared['TOLERANCE_BPS'] > 0
        and declared['PUBLISHED_FEE_MULTIPLIER_BPS'] > declared['BPS']
        and declared['PUBLISHED_TIME_MULTIPLIER_BPS'] > declared['BPS'], 'published multiplier must not imply parity')
    guide = (ROOT / GUIDE).read_text()
    ratio = lambda bps: f"{bps // 10_000}.{bps % 10_000 // 100:02d}x"
    gate = lambda bps: bps * (10_000 + declared['TOLERANCE_BPS']) // 10_000
    stated = [ratio(declared['PUBLISHED_FEE_MULTIPLIER_BPS']) + ' compiled protocol fee',
              ratio(declared['PUBLISHED_TIME_MULTIPLIER_BPS']) + ' compiled execution time',
              f"{declared['TOLERANCE_BPS'] // 100}% regression tolerance"]
    if declared['PUBLISHED_FEE_MULTIPLIER_BPS'] == declared['PUBLISHED_TIME_MULTIPLIER_BPS']:
        stated.append('hard gates at ' + ratio(gate(declared['PUBLISHED_FEE_MULTIPLIER_BPS'])))
    flat = ' '.join(guide.replace('*', '').split())
    missing = [phrase for phrase in stated if phrase not in flat]
    require(not missing, 'programs guide does not state the gated multiplier: ' + '; '.join(missing))
    require('make programs-bench' in flat, 'programs guide must name the release measurement entry point')
    return declared, gate


def retained_entry(environment, deadline):
    plan = subprocess.run(['make', '-n', '--no-print-directory', 'programs-bench'], cwd=ROOT, env=environment,
        stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=remaining(deadline, 60))
    require(plan.returncode == 0, 'make programs-bench must resolve')
    text = plan.stdout
    require(all('-p ' + package in text for package, _ in WASM.values())
        and 'bench --locked -p layerx-programs-runtime --bench interpreter' in text
        and 'LAYERX_INTERPRETER_WASM=' in text and 'LAYERX_COMPILED_EQUIVALENT_WASM=' in text,
        'make programs-bench must run the interpreter measurements over both real Wasm routes')


def qualify(directory):
    record = directory / 'build-manifest.json'
    private(record)
    built = json.loads(record.read_text())
    require(built['schema'] == 'layerx.interpreter-pricing-build.v1' and built['task'] == TASK
        and built['build_exit'] == 0, 'actual successful interpreter pricing build required')
    require(built['source_revision'] == git('rev-parse', 'HEAD') and built['source_tree'] == git('rev-parse', 'HEAD^{tree}'),
        'compiled revision or tree changed')
    require(all(digest(ROOT / path) == value for path, value in built['source_hashes'].items()), 'compiled source changed')
    binaries = {key: checked(row) for key, row in built['artifacts'].items()}
    require(set(binaries) == {'interpreter', 'compiled', 'bench'}, 'complete captured pricing artifacts required')
    deadline = float(os.environ.get('PAXEER_X_TASK_DEADLINE_EPOCH', str(time.time() + 1700)))
    declared, gate = published()
    passed = ['guide-states-gated-multiplier']
    retained_entry(dict(os.environ), deadline)
    passed.append('programs-bench-runs-interpreter-measurements')
    environment = dict(os.environ, LAYERX_INTERPRETER_WASM=str(binaries['interpreter']),
        LAYERX_COMPILED_EQUIVALENT_WASM=str(binaries['compiled']))
    environment.pop('LAYERX_INTERPRETER_BENCH_SAMPLES', None)
    text = command([binaries['bench']], directory / 'bench.log', environment, deadline, 1500)
    print(text, end='', flush=True)
    usage = {}
    for row in USAGE.findall(text):
        require((row[0], row[1]) not in usage, 'duplicate metered usage row')
        usage[(row[0], row[1])] = tuple(map(int, row[2:]))
    require(set(usage) == {(route, name) for route in ('interpreted', 'compiled') for name in WORKLOADS},
        'every representative workload must report metered usage on both routes')
    for name in WORKLOADS:
        interpreted, compiled = usage[('interpreted', name)], usage[('compiled', name)]
        require(compiled[0] > 0 and compiled[-1] > 0, name + ': compiled route must consume metered fuel and fee')
        require(interpreted[0] > compiled[0], name + ': interpreted route must meter the interpreter work it performs')
        require(interpreted[-1] >= compiled[-1], name + ': interpreted route priced below its compiled equivalent')
        passed.append('equivalent-modulo-cost:' + name)
    timings = {row[0]: (int(row[1]), int(row[2])) for row in TIMING.findall(text)}
    require(set(timings) == set(WORKLOADS) and all(value[0] > 0 and value[1] > 0 for value in timings.values()),
        'every representative workload must report positive median timings')
    fee, time_row = FEE.findall(text), TIME.findall(text)
    require(len(fee) == 1 and len(time_row) == 1, 'one fee and one time overhead verdict required')
    fee, time_row = tuple(map(int, fee[0])), tuple(map(int, time_row[0]))
    interpreted_fee = sum(usage[('interpreted', name)][-1] for name in WORKLOADS)
    compiled_fee = sum(usage[('compiled', name)][-1] for name in WORKLOADS)
    require(fee[0] == interpreted_fee * 10_000 // compiled_fee, 'reported fee multiplier does not match metered fee units')
    require(fee[1:] == (declared['PUBLISHED_FEE_MULTIPLIER_BPS'], declared['TOLERANCE_BPS'],
        gate(declared['PUBLISHED_FEE_MULTIPLIER_BPS'])), 'fee verdict does not use the published multiplier')
    require(fee[0] > 10_000 and fee[0] <= fee[3], 'interpreted protocol fee overhead outside its published tolerance')
    passed.append('fee-multiplier-within-tolerance')
    interpreted_ns = sum(value[0] for value in timings.values())
    compiled_ns = sum(value[1] for value in timings.values())
    require(time_row[0] == interpreted_ns * 10_000 // compiled_ns, 'reported time multiplier does not match medians')
    require(time_row[1:4] == (declared['PUBLISHED_TIME_MULTIPLIER_BPS'], declared['TOLERANCE_BPS'],
        gate(declared['PUBLISHED_TIME_MULTIPLIER_BPS'])) and time_row[4] >= 3 and time_row[4] % 2 == 1,
        'time verdict does not use the published multiplier and declared samples')
    require(time_row[0] <= time_row[3], 'interpreted execution time overhead outside its published tolerance')
    passed.append('time-multiplier-within-tolerance')
    require(all(digest(ROOT / path) == value for path, value in built['source_hashes'].items()), 'source changed during qualification')
    for row in built['artifacts'].values():
        checked(row)
    save(directory / 'result.json', {'task': TASK, 'revision': built['source_revision'],
        'command': 'timeout 30m tools/paxeer-x/verify-task.sh 104.33.5', 'exit_code': 0,
        'log_path': str(directory / 'bench.log'), 'checks': passed, 'tests': len(passed), 'skipped': 0,
        'observed_fee_bps': fee[0], 'observed_time_bps': time_row[0], 'samples': time_row[4],
        'release_required': RELEASE_REQUIRED, 'release_qualification': 'UNRUN'})
    print(f'PAXEER_X_GATE tests={len(passed)} skipped=0', flush=True)


try:
    args = sys.argv[2:]
    require(args in ([], ['--build']), 'only --build or the declared verify selector is admitted')
    base = os.environ.get('PAXEER_X_EVIDENCE_DIR')
    require(base, 'PAXEER_X_EVIDENCE_DIR is required')
    base = Path(base).resolve(strict=True)
    private(base, True)
    directory = base / 'task104335'
    directory.mkdir(exist_ok=True, mode=0o700)
    private(directory, True)
    require(ROOT != directory and ROOT not in directory.parents, 'private evidence outside checkout required')
    build(directory) if args else qualify(directory)
except subprocess.CalledProcessError as error:
    print('104.33.5 command exit=' + str(error.returncode), file=sys.stderr)
    sys.exit(error.returncode if error.returncode > 0 else 1)
except subprocess.TimeoutExpired:
    print('104.33.5 original cutoff or bounded command reached', file=sys.stderr)
    sys.exit(124)
except (RuntimeError, OSError, KeyError, ValueError, TypeError) as error:
    print('104.33.5 prerequisite or artifact binding refused: ' + str(error), file=sys.stderr)
    sys.exit(78)
PY
