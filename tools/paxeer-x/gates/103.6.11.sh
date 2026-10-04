#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)
exec python3 - "$root" "$@" <<'PY'
import contextlib
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

os.umask(0o077)
ROOT = Path(sys.argv[1])
TASK = '103.6.11'
GATE = 'tools/paxeer-x/gates/103.6.11.sh'
SCHEMA = 'layerx.composite-state-transition-build.v1'
DEADLINE = float(os.environ.get('PAXEER_X_TASK_DEADLINE_EPOCH', str(time.time() + 1800)))
NATIVE = ['test_state_commitment_transition', 'test_genesis_manifest',
          'test_genesis_module_table', 'test_genesis_builder_cli', 'test_daemon_bootstrap_artifact']
CONTRACTS = ['CheckpointRegistry', 'PaxeerBetaDeploymentValidator', 'ContractManager']
RELEASE = ['make -s check']
DEPENDENT = {'task': '6.10', 'qualification': 'UNRUN',
             'required': 'genuine fresh-genesis SEND and independent replica/account evidence'}
LAST = {'command': [], 'log_path': None}


def require(value, message):
    if not value:
        raise RuntimeError(message)


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def private(path, directory=False):
    info = path.lstat()
    require(not path.is_symlink() and info.st_uid == os.geteuid()
            and not info.st_mode & 0o077
            and (stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode)),
            'caller-owned private evidence required')


def save(path, value):
    if path.exists() or path.is_symlink():
        private(path)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write('\n')


def load(path):
    private(path)
    require(path.stat().st_size <= 64 * 1024 * 1024, 'bounded build manifest required')
    return json.loads(path.read_text())


def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT, text=True).strip()


def source_identity():
    names = subprocess.check_output(['git', 'ls-files', '-z'], cwd=ROOT).split(b'\0')
    suffixes = {'.c', '.h', '.rs', '.toml', '.lock', '.sol', '.json', '.py', '.sh',
                '.kvx', '.lxgb', '.lxgd', '.lxrr', '.lxs', '.manifest', '.bin', '.public'}
    paths = {os.fsdecode(name) for name in names if name} | {GATE, 'Makefile'}
    selected = sorted(path for path in paths
        if not any(part.startswith('.env') for part in Path(path).parts)
        and (Path(path).suffix in suffixes or Path(path).name == 'Makefile')
        and (ROOT / path).is_file())
    return {'revision': git('rev-parse', 'HEAD'), 'tree': git('rev-parse', 'HEAD^{tree}'),
            'hashes': {path: digest(ROOT / path) for path in selected}}


def remaining(bound):
    seconds = min(bound, DEADLINE - time.time())
    require(seconds > 0, 'original task deadline exhausted')
    return seconds


@contextlib.contextmanager
def locked(name):
    path = Path('/root/lx-cargo') / (name + '-build.lock')
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open('a') as stream:
        while True:
            try:
                fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError:
                remaining(1)
                time.sleep(min(1, remaining(1)))
        yield


def command(argv, log, environment=None, bound=1200):
    LAST.update(command=[str(value) for value in argv], log_path=str(log))
    with log.open('w') as stream:
        log.chmod(0o600)
        result = subprocess.run(LAST['command'], cwd=ROOT, env=environment,
            stdin=subprocess.DEVNULL, stdout=stream, stderr=subprocess.STDOUT,
            timeout=remaining(bound))
    print('command=' + json.dumps(LAST['command']) + ' exit=' + str(result.returncode)
          + ' log=' + str(log), flush=True)
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, argv)
    return log.read_text()


def artifact(path, executable=True):
    path = Path(path).resolve(strict=True)
    require(path.is_file() and (not executable or os.access(path, os.X_OK)),
            'genuine compiler artifact required')
    return {'path': str(path), 'sha256': digest(path), 'executable': executable}


def checked(row):
    path = Path(row['path'])
    require(path.is_absolute() and path.is_file() and not path.is_symlink()
            and digest(path) == row['sha256']
            and (not row['executable'] or os.access(path, os.X_OK)),
            'compiled artifact missing or changed')
    return path


def tool(name, override=None):
    value = os.environ.get(override) if override else None
    value = value or shutil.which(name)
    require(value, 'actual ' + name + ' tool required before any task build')
    path = Path(value).absolute()
    require(path.is_file() and os.access(path, os.X_OK), 'executable tool required: ' + name)
    return str(path)


def build(directory):
    require(not (directory / 'build-manifest.json').exists(), 'task build already recorded')
    cargo = tool('cargo', 'CARGO')
    forge = tool('forge', 'PAXEER_X_FORGE_BIN')
    make = tool('make')
    tool('cc')
    before = source_identity()
    environment = dict(os.environ, CARGO_BUILD_JOBS='2', RUSTUP_TOOLCHAIN='1.91.1',
        PATH='/root/.cargo/bin:' + os.environ.get('PATH', ''))
    native_directory = directory / 'native'
    runtime_target = Path(os.environ.get('PAXEER_X_PROGRAMS_TARGET_DIR', '/root/lx-target/programs'))
    runtime_library = runtime_target / 'debug/liblayerx_programs_sandbox.a'
    with locked('native'):
        runtime_env = dict(environment, CARGO_TARGET_DIR=str(runtime_target))
        command([cargo, 'build', '--locked', '--manifest-path', 'programs/Cargo.toml',
                 '-p', 'layerx-programs-sandbox', '--features', 'host-ffi'],
                directory / 'runtime-build.log', runtime_env)
        require(runtime_library.is_file(), 'actual native runtime static library required')
        command([make, '-j2', 'BUILD_DIR=' + str(native_directory),
                 'PROGRAMS_RUNTIME_LIB=' + str(runtime_library),
                 'PROGRAMS_TARGET_DIR=' + str(runtime_target), '-o', 'programs-build',
                 '-o', str(runtime_library), str(native_directory / 'bin/layerx-genesis-build'),
                 *[str(native_directory / 'tests' / name) for name in NATIVE]],
                directory / 'native-build.log', environment)
    native = {name: artifact(native_directory / 'tests' / name) for name in NATIVE}
    native['layerx-genesis-build'] = artifact(native_directory / 'bin/layerx-genesis-build')
    native['runtime_library'] = artifact(runtime_library, False)
    suites = {}
    compiler_rows = []
    agent_specs = [('layerx-wire', []), ('layerx-crypto', ['--test', 'verify']),
                   ('layerx-proof', [])]
    commands = [('agent', package, options) for package, options in agent_specs]
    commands.append(('human', 'layerx-intents', ['--test', 'compile', '--test', 'differential']))
    for index, (workspace, package, options) in enumerate(commands):
        environment['CARGO_TARGET_DIR'] = '/root/lx-target/' + workspace
        with locked(workspace):
            text = command([cargo, 'test', '--locked', '--manifest-path', workspace + '/Cargo.toml',
                    '-p', package, *options, '--no-run', '--message-format=json'],
                    directory / ('rust-build-' + str(index) + '.log'), environment)
        selected = []
        for line in text.splitlines():
            if not line.startswith('{'):
                continue
            row = json.loads(line)
            if row.get('reason') != 'compiler-artifact':
                continue
            compiler_rows.append(row)
            if row['profile']['test'] and row.get('executable'):
                manifest = Path(row['manifest_path'])
                if manifest.parent.name == package:
                    name = package + ':' + row['target']['name']
                    require(name not in suites, 'duplicate test compiler artifact')
                    suites[name] = artifact(row['executable'])
                    selected.append(name)
        require(selected, 'all declared Rust package test binaries required: ' + package)
        if package == 'layerx-crypto':
            require(selected == ['layerx-crypto:verify'], 'exact crypto verify target required')
        if package == 'layerx-intents':
            require(set(selected) == {'layerx-intents:compile', 'layerx-intents:differential'},
                    'both declared intent targets required')
    forge_out = directory / 'forge-artifacts'
    forge_cache = directory / 'forge-cache'
    command([forge, 'build', *['tests/solidity/' + name + '.t.sol' for name in CONTRACTS],
             '--out', str(forge_out), '--cache-path', str(forge_cache)],
            directory / 'contracts-build.log', environment)
    contract_outputs = {str(path.relative_to(forge_out)): artifact(path, False)
                        for path in sorted(forge_out.rglob('*.json'))}
    require(contract_outputs, 'actual Solidity compiler outputs required')
    require(source_identity() == before, 'source changed during task build')
    for row in [*native.values(), *suites.values(), *contract_outputs.values()]:
        checked(row)
    save(directory / 'build-manifest.json', {'schema': SCHEMA, 'task': TASK,
         'identity': before, 'build_exit_code': 0, 'deadline_epoch': DEADLINE,
         'native': native, 'rust': suites, 'compiler_artifacts': compiler_rows,
         'forge': artifact(forge), 'forge_out': str(forge_out), 'forge_cache': str(forge_cache),
         'contracts': contract_outputs, 'release_required': RELEASE,
         'release_qualification': 'UNRUN', 'dependent': DEPENDENT})
    print('ARTIFACTS ' + str(directory / 'build-manifest.json'), flush=True)


def rust_cases(binary, log):
    inventory = command([binary, '--list', '--format', 'terse'], log, bound=30)
    cases = re.findall(r'^(.+): test$', inventory, re.M)
    require(cases and len(cases) == len(set(cases)), 'nonempty exact Rust test inventory required')
    return set(cases)


def qualify(directory):
    built = load(directory / 'build-manifest.json')
    require(built['schema'] == SCHEMA and built['task'] == TASK and built['build_exit_code'] == 0,
            'successful whole task build manifest required')
    require(built['identity'] == source_identity(), 'compiled source revision/tree/bytes changed')
    require(built['deadline_epoch'] == DEADLINE, 'original task deadline changed')
    require(set(built['native']) == set(NATIVE) | {'layerx-genesis-build', 'runtime_library'},
            'all declared native artifacts required')
    native = {name: checked(row) for name, row in built['native'].items()}
    suites = {name: checked(row) for name, row in built['rust'].items()}
    executed = {}
    for name in NATIVE:
        command([native[name]], directory / (name + '.log'))
        executed[name] = {'exit_code': 0, 'cases': 'internal real-kernel assertions'}
    count = len(NATIVE)
    required = {'layerx-wire:hashing': 'protocol_three_account_ids_match_native_ledger_vectors',
                'layerx-crypto:verify': 'state_commitment_signatures_bind_exact_canonical_bytes',
                'layerx-intents:compile': 'protocol_three_send_compilation_and_disclosure_use_native_account_ids',
                'layerx-intents:differential': 'v2_golden_vectors_are_byte_locked_to_the_intent_version'}
    inventories = {}
    for index, (name, binary) in enumerate(sorted(suites.items())):
        cases = rust_cases(binary, directory / ('rust-inventory-' + str(index) + '.log'))
        inventories[name] = sorted(cases)
        if name in required:
            require(required[name] in cases, 'mandatory transition case absent: ' + name)
        text = command([binary, '--test-threads=1', '--nocapture'],
                       directory / ('rust-verify-' + str(index) + '.log'))
        passed = re.findall(r'^test ([^\s]+) \.\.\. ok$', text, re.M)
        summaries = re.findall(r'^test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;[^\n]*$', text, re.M)
        require(len(passed) == len(set(passed)) and set(passed) == cases
                and len(summaries) == 1 and tuple(map(int, summaries[0])) == (len(cases), 0, 0),
                'every existing scoped Rust case must pass exactly once without skips')
        executed[name] = sorted(cases)
        count += len(cases)
    require(set(required) <= set(suites), 'mandatory wire/crypto/intent suites absent')
    save(directory / 'rust-inventories.json', inventories)
    forge = checked(built['forge'])
    for row in built['contracts'].values():
        checked(row)
    report_path = directory / 'contracts-result.json'
    require(not report_path.exists() and not report_path.is_symlink(), 'fresh contract report required')
    command([forge, 'test', '--match-path',
                    'tests/solidity/{' + ','.join(CONTRACTS) + '}.t.sol',
                    '--out', built['forge_out'], '--cache-path', built['forge_cache'],
                    '--json', '--json-file', str(report_path)],
                   directory / 'contracts-verify.log')
    report = load(report_path)
    found = set()
    for suite, value in report.items():
        path = suite.split(':', 1)[0]
        if not value.get('test_results'):
            continue
        require(path in {'tests/solidity/' + name + '.t.sol' for name in CONTRACTS},
                'only declared settlement contract suites may execute')
        results = value['test_results']
        require(all(row['status'] == 'Success' for row in results.values()),
                'every scoped settlement case must pass without skips')
        found.add(Path(path).name.removesuffix('.t.sol'))
        executed[suite] = sorted(results)
        count += len(results)
    require(found == set(CONTRACTS), 'all three declared settlement contract files required')
    require(built['identity'] == source_identity(), 'source changed during task verification')
    for row in [*built['native'].values(), *built['rust'].values(), *built['contracts'].values()]:
        checked(row)
    save(directory / 'result.json', {'task': TASK, 'revision': built['identity']['revision'],
         'command': 'timeout 30m tools/paxeer-x/verify-task.sh 103.6.11', 'exit_code': 0,
         'log_path': str(directory), 'executed': executed, 'rust_cases_and_native_suites': count,
         'skipped': 0, 'release_required': RELEASE, 'release_qualification': 'UNRUN',
         'dependent': DEPENDENT})
    print('PAXEER_X_GATE tests=' + str(count) + ' skipped=0', flush=True)


DIRECTORY = None
try:
    args = sys.argv[2:]
    require(args in ([], ['--build']), '103.6.11 accepts only --build or no selector arguments')
    DIRECTORY = Path(os.environ.get('PAXEER_X_STATE_COMMITMENT_EVIDENCE',
        '/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task103611'))
    DIRECTORY.mkdir(parents=True, exist_ok=True, mode=0o700)
    private(DIRECTORY, True)
    DIRECTORY = DIRECTORY.resolve(strict=True)
    require(DIRECTORY != ROOT and ROOT not in DIRECTORY.parents, 'evidence must be outside checkout')
    if args:
        build(DIRECTORY)
    else:
        qualify(DIRECTORY)
except (subprocess.CalledProcessError, subprocess.TimeoutExpired, RuntimeError, OSError,
        KeyError, ValueError, TypeError) as error:
    code = error.returncode if isinstance(error, subprocess.CalledProcessError) else 124 if isinstance(error, subprocess.TimeoutExpired) else 78
    code = code if code > 0 else 1
    if DIRECTORY is not None:
        save(DIRECTORY / ('build-failure.json' if sys.argv[2:] == ['--build'] else 'failure.json'),
             {'task': TASK, 'revision': git('rev-parse', 'HEAD'), 'exit_code': code,
              **LAST, 'observed': str(error), 'qualification': 'FAILED',
              'release_qualification': 'UNRUN', 'dependent': DEPENDENT})
    print('103.6.11 refused: ' + str(error), file=sys.stderr)
    sys.exit(code)
PY
