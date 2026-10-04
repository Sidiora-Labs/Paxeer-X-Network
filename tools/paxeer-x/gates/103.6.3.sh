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
import stat
import shutil
import subprocess
import sys

ROOT = Path(sys.argv[1])
TASK = '103.6.3'
SCHEMA = 'layerx.authority-task-build.v1'
GATE = 'tools/paxeer-x/gates/103.6.3.sh'
DRIVERS = ('tools/qualification/paxeer-x/authority-lni-readiness.py',
           'tools/qualification/paxeer-x/router-authority-schema.py',
           'tests/daemon/paxeer_x_runtime_fixture.py', 'tools/runtime/run-with-clock.sh')
FIXTURE_TESTS = {
    ('real_node', 'router_authority_readiness_schema_restart_contract'): 'serializer',
    ('real_node', 'authority_lni_readiness_case'): 'readiness',
    ('layerx-receipt-authority', 'lni_readiness_tests::actual_node_info_refuses_incompatible_receipt_admission'): 'readiness',
}
MANDATORY = {
    ('real_node', 'real_node_authority_serves_verified_facts_and_reflects_replica_loss'),
    ('real_node', 'real_replica_readiness_relay_and_refusals_without_sequencer'),
    ('real_node', 'evidence_read_token_reads_only_receipt_authority'),
    ('budget_proof_exports', 'real_budget_exports_bind_each_selector_finality_and_tamper_refusals'),
} | set(FIXTURE_TESTS)


def require(condition, reason):
    if not condition:
        raise RuntimeError(reason)


def digest(path):
    with Path(path).open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def git(*arguments):
    return subprocess.check_output(['git', *arguments], cwd=ROOT, text=True).strip()


def private(path, directory=False):
    info = path.lstat()
    require((stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode))
            and info.st_uid == os.geteuid() and not info.st_mode & 0o077,
            'private caller-owned regular evidence required: ' + str(path))


def evidence_directory():
    path = Path(os.environ.get('PAXEER_X_AUTHORITY_EVIDENCE',
        '/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task10363'))
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    private(path, True)
    path = path.resolve(strict=True)
    require(ROOT != path and ROOT not in path.parents, 'evidence must be outside checkout')
    return path


def save(path, value):
    if path.exists() or path.is_symlink():
        private(path)
    with path.open('w') as output:
        json.dump(value, output, indent=2, sort_keys=True)
        output.write('\n')
    path.chmod(0o600)


def artifact(path, revision):
    path = Path(path).resolve(strict=True)
    require(path.is_file() and os.access(path, os.X_OK), 'actual executable required')
    return {'path': str(path), 'sha256': digest(path), 'source_revision': revision}


def checked(row):
    path = Path(row['path'])
    require(path.is_absolute() and path.is_file() and not path.is_symlink()
            and os.access(path, os.X_OK) and digest(path) == row['sha256'],
            'prebuilt executable changed or missing')
    return path


def source_snapshot():
    inventory = subprocess.check_output(['git', 'ls-files', '-z'], cwd=ROOT).split(b'\0')
    paths = {os.fsdecode(path) for path in inventory if path} | {GATE} | set(DRIVERS)
    return {path: digest(ROOT / path) for path in sorted(paths)
            if Path(path).suffix in {'.rs', '.toml', '.lock', '.py', '.sh', '.json'}
            and (ROOT / path).is_file()}


def build(directory):
    (directory / 'build-manifest.json').unlink(missing_ok=True)
    initial = source_snapshot()
    revision, tree = git('rev-parse', 'HEAD'), git('rev-parse', 'HEAD^{tree}')
    cargo = os.environ.get('CARGO', '/root/.cargo/bin/cargo')
    environment = dict(os.environ, CARGO_BUILD_JOBS='4',
        CARGO_TARGET_DIR=os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/authority-contract'),
        PATH='/root/.cargo/bin:' + os.environ.get('PATH', ''))
    commands = [
        [cargo, 'build', '--locked', '--manifest-path', 'platform/Cargo.toml',
         '-p', 'layerx-runtime-clock', '--bin', 'layerx-runtime-clock', '--message-format=json'],
        [cargo, 'test', '--locked', '--manifest-path', 'platform/Cargo.toml',
         '-p', 'layerx-platform-authority', '-p', 'layerx-platform-gateway',
         '--all-targets', '--no-run', '--message-format=json'],
    ]
    records = []
    lock_path = Path('/root/lx-cargo/authority-contract.lock')
    lock_path.parent.mkdir(parents=True, exist_ok=True)
    with lock_path.open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        for index, command in enumerate(commands):
            log = directory / ('build-' + str(index) + '.log')
            with log.open('w') as output:
                result = subprocess.run(command, cwd=ROOT, env=environment,
                    stdin=subprocess.DEVNULL, stdout=output, stderr=subprocess.STDOUT, timeout=1100)
            if result.returncode:
                raise subprocess.CalledProcessError(result.returncode, command)
            for line in log.read_text().splitlines():
                if line.startswith('{'):
                    value = json.loads(line)
                    if value.get('reason') == 'compiler-artifact':
                        records.append(value)
    prefixes = set()
    artifacts, tests = {}, {}
    staged = directory / 'compiled-artifacts'
    staged.mkdir(mode=0o700, exist_ok=True)
    private(staged, True)

    def captured(executable):
        original = Path(executable).resolve(strict=True)
        target = staged / original.name
        original_digest = digest(original)
        shutil.copyfile(original, target)
        target.chmod(0o700)
        require(digest(target) == original_digest, 'compiled executable copy changed')
        return artifact(target, revision)

    for row in records:
        manifest = Path(row['manifest_path'])
        if ROOT in manifest.parents:
            prefixes.add(str(manifest.parent.relative_to(ROOT)) + '/')
        executable = row.get('executable')
        if not executable:
            continue
        target = row['target']['name']
        is_test = row['profile']['test']
        crate = manifest.parent
        if crate == ROOT / 'platform/hosted/authority':
            if is_test:
                require(target not in tests, 'duplicate declared authority test artifact')
                tests[target] = captured(executable)
            elif target == 'layerx-receipt-authority':
                artifacts['authority'] = captured(executable)
        elif crate == ROOT / 'platform/hosted/gateway' and is_test and target == 'layerx-gateway':
            artifacts['gateway_tests'] = captured(executable)
        elif target == 'layerx-runtime-clock' and not is_test:
            artifacts['runtime_clock'] = captured(executable)
    require({'authority', 'gateway_tests', 'runtime_clock'} <= set(artifacts),
            'actual service, gateway contract and runtime clock executables required')
    require({'real_node', 'layerx-receipt-authority', 'budget_proof_exports',
             'human_tls', 'maintenance_identity'} <= set(tests), 'complete authority test targets required')
    artifacts.update(authority_tests=tests['real_node'], authority_unit=tests['layerx-receipt-authority'])
    fixed = {GATE, *DRIVERS, 'platform/Cargo.toml', 'platform/Cargo.lock',
             'agent/Cargo.toml', 'agent/Cargo.lock', 'programs/Cargo.toml', 'programs/Cargo.lock'}
    selected = {path: value for path, value in initial.items()
                if path in fixed or path.startswith(tuple(prefixes)) or path.startswith('.cargo/')}
    require(all(digest(ROOT / path) == value for path, value in selected.items()),
            'actual build dependency source changed')
    save(directory / 'build-manifest.json', {
        'schema': SCHEMA, 'task': TASK, 'source_revision': revision, 'source_tree': tree,
        'build_exit': 0, 'commands': commands, 'artifacts': artifacts,
        'authority_test_artifacts': tests, 'source_hashes': selected,
        'release_required': ['native aggregate coverage', 'strict all-target Clippy', 'release matrices'],
        'release_qualification': 'UNRUN',
    })
    print('ARTIFACTS ' + str(directory / 'build-manifest.json'), flush=True)


def logged(command, path, environment, group=None):
    with path.open('w') as output:
        options = {} if group is None else {'group': group, 'extra_groups': []}
        result = subprocess.run([str(part) for part in command], cwd=ROOT, env=environment,
            stdin=subprocess.DEVNULL, stdout=output, stderr=subprocess.STDOUT, timeout=840, **options)
    text = path.read_text()
    print(text, end='', flush=True)
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, command)
    return text


def qualify(directory):
    require(os.geteuid() == 0, 'actual isolated native qualification requires root')
    manifest_path = directory / 'build-manifest.json'
    private(manifest_path)
    built = json.loads(manifest_path.read_text())
    require(built.get('schema') == SCHEMA and built.get('task') == TASK and built.get('build_exit') == 0,
            'successful actual scoped build manifest required')
    require(built['source_revision'] == git('rev-parse', 'HEAD')
            and built['source_tree'] == git('rev-parse', 'HEAD^{tree}'), 'build revision changed')
    require(all(digest(ROOT / path) == value for path, value in built['source_hashes'].items()),
            'actual build dependency source changed')
    for row in [*built['artifacts'].values(), *built['authority_test_artifacts'].values()]:
        checked(row)
    for name in ('PAXEER_X_FOUNDATION_ARTIFACT_MANIFEST', 'PAXEER_X_RUNTIME_CLIENT_MANIFEST',
                 'PAXEER_X_BUDGET_PROOF_CORPUS'):
        require(os.environ.get(name), 'genuine required input missing: ' + name)
        private(Path(os.environ[name]))
    foundation = Path(os.environ['PAXEER_X_FOUNDATION_ARTIFACT_MANIFEST'])
    source_bound_native = json.loads(foundation.read_text())
    require(source_bound_native.get('version') == 1 and source_bound_native['build']['exit_code'] == 0,
            'successful genuine native artifact manifest required')
    native = Path(source_bound_native['artifacts']['layerxd']['path']).parent
    require(Path(source_bound_native['artifacts']['layerx-genesis-build']['path']).parent == native,
            'real native executables must share their bundle')
    environment = dict(os.environ, PAXEER_X_AUTHORITY_BUILD_MANIFEST=str(manifest_path),
        PAXEER_X_AUTHORITY_EVIDENCE=str(directory),
        PAXEER_X_RUNTIME_ARTIFACTS=str(foundation), LAYERX_TEST_NATIVE_BIN_DIR=str(native),
        PAXEER_X_AUTHORITY_BIN=built['artifacts']['authority']['path'],
        PAXEER_X_GATEWAY_TEST_BIN=built['artifacts']['gateway_tests']['path'],
        LAYERX_RUNTIME_CLOCK_BIN=built['artifacts']['runtime_clock']['path'],
        PATH='/root/.cargo/bin:' + os.environ.get('PATH', ''))
    clock = checked(built['artifacts']['runtime_clock'])
    inventory = {}
    for target, row in sorted(built['authority_test_artifacts'].items()):
        binary = checked(row)
        result = subprocess.run([str(binary), '--list', '--format', 'terse'], cwd=ROOT,
            env=environment, stdin=subprocess.DEVNULL, capture_output=True, text=True,
            check=True, timeout=30)
        names = re.findall(r'^(.+): test$', result.stdout, re.M)
        require(len(names) == len(set(names)), 'duplicate test names')
        inventory[target] = names
    declared = {(target, name) for target, names in inventory.items() for name in names}
    require(MANDATORY <= declared, 'mandatory non-skipped authority case missing')
    save(directory / 'test-inventory.json', {'tests': inventory, 'fixture_owned': sorted(FIXTURE_TESTS)})
    executed = set()
    for target, name in sorted(declared - set(FIXTURE_TESTS)):
        binary = checked(built['authority_test_artifacts'][target])
        label = hashlib.sha256((target + ':' + name).encode()).hexdigest()[:16]
        text = logged([clock, '--runtime-dir', '/tmp', '--', binary, name, '--exact',
                       '--nocapture', '--test-threads=1'], directory / ('test-' + label + '.log'),
                      environment)
        require('test result: ok. 1 passed; 0 failed; 0 ignored; 0 measured;' in text,
                'required test did not execute exactly once: ' + target + ':' + name)
        executed.add((target, name))
    fixture_cases = 0
    for mode, script in (('readiness', DRIVERS[0]), ('serializer', DRIVERS[1])):
        fixture_directory = directory / mode
        fixture_directory.mkdir(mode=0o700, exist_ok=True)
        private(fixture_directory, True)
        fixture_environment = dict(environment, PAXEER_X_AUTHORITY_EVIDENCE=str(fixture_directory))
        text = logged([clock, '--runtime-dir', '/tmp', '--', sys.executable, ROOT / script],
                      directory / (mode + '.log'), fixture_environment, group=4020)
        counts = re.findall(r'^PAXEER_X_GATE tests=(\d+) skipped=0$', text, re.M)
        require(len(counts) == 1 and int(counts[0]) > 0, 'complete genuine fixture case accounting required')
        fixture_cases += int(counts[0])
        executed.update(test for test, owner in FIXTURE_TESTS.items() if owner == mode)
    require(executed == declared, 'declared authority test partition incomplete')
    require(all(digest(ROOT / path) == value for path, value in built['source_hashes'].items()),
            'source changed during qualification')
    for row in [*built['artifacts'].values(), *built['authority_test_artifacts'].values()]:
        checked(row)
    save(directory / 'result.json', {'task': TASK, 'revision': built['source_revision'],
        'command': 'timeout 30m tools/paxeer-x/verify-task.sh 103.6.3', 'exit': 0,
        'declared_tests': sorted(declared), 'executed_tests': sorted(executed),
        'fixture_cases': fixture_cases, 'release_qualification': 'UNRUN',
        'release_required': built['release_required']})
    print(f'PAXEER_X_GATE tests={len(declared - set(FIXTURE_TESTS)) + fixture_cases} skipped=0', flush=True)


def main():
    os.umask(0o077)
    require(sys.argv[2:] in ([], ['--build']), '103.6.3 accepts only --build or no arguments')
    directory = evidence_directory()
    if sys.argv[2:]:
        build(directory)
    else:
        qualify(directory)


try:
    main()
except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
    print('103.6.3: refused: ' + str(error), file=sys.stderr)
    sys.exit(error.returncode if isinstance(error, subprocess.CalledProcessError) else 1)
PY
