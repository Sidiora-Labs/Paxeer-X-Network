#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[4]
PACKAGE = ROOT / 'platform/hosted/core/Cargo.toml'
NATIVE_NAMES = ('layerxd', 'layerx-genesis-build', 'layerx-handover')
NATIVE_PATHS = ('Makefile', 'src', 'include', 'cmd', 'programs', 'agent',
                'contracts/config/checkpoint-settlement.json')
INTEGRATION_TARGETS = {'boundary', 'send', 'signer', 'custody_asset_send'}
HARNESS_KEYS = {'lib:layerx_platform_core', 'bin:layerx-core-boundary'} | {
    'test:' + name for name in INTEGRATION_TARGETS
}
RELEASE_REQUIRED = [
    'cargo test --locked --manifest-path platform/Cargo.toml -p layerx-platform-core --all-targets -- --test-threads=1',
    'cargo clippy --locked --manifest-path platform/Cargo.toml -p layerx-platform-core --all-targets -- -D warnings',
]
sys.dont_write_bytecode = True
sys.path.insert(0, str(ROOT / 'tests/daemon'))


def require(value, message):
    if not value:
        raise RuntimeError(message)


def git(*arguments):
    return subprocess.check_output(['git', *arguments], cwd=ROOT, text=True).strip()


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def private(path, directory=False):
    info = path.lstat()
    require(path.is_absolute() and not any(p.is_symlink() for p in (path, *path.parents))
            and (stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode))
            and info.st_uid == os.geteuid() and not info.st_mode & 0o077,
            'owned private evidence required')
    if not directory:
        require(info.st_nlink == 1, 'evidence must have one link')


def load(path):
    private(path)
    return json.loads(path.read_text())


def save(path, value):
    if path.exists() or path.is_symlink():
        private(path)
    with path.open('w') as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write('\n')
    path.chmod(0o600)


def manifest_path():
    supplied = os.environ.get('PAXEER_X_CORE_BUILD_MANIFEST')
    require(supplied, 'PAXEER_X_CORE_BUILD_MANIFEST is required')
    path = Path(supplied)
    require(path.is_absolute() and ROOT not in path.parents,
            'core build evidence must be absolute and outside the checkout')
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    private(path.parent, True)
    return path


def artifact(path, revision):
    path = Path(path)
    require(path.is_absolute() and not any(p.is_symlink() for p in (path, *path.parents))
            and path.is_file() and os.access(path, os.X_OK), 'real executable required')
    with path.open('rb') as stream:
        require(stream.read(4) == b'\x7fELF', 'real executable ELF required')
    return {'path': str(path), 'sha256': digest(path), 'source_revision': revision}


def checked(row):
    current = artifact(row['path'], row['source_revision'])
    require(all(current[key] == row[key] for key in current), 'source-bound executable changed')
    return Path(row['path'])


def native_inputs():
    supplied = os.environ.get('PAXEER_X_FOUNDATION_ARTIFACT_MANIFEST')
    require(supplied, 'genuine native foundation artifact manifest required')
    path = Path(supplied)
    foundation = load(path)
    require(foundation.get('version') == 1 and foundation['build']['exit_code'] == 0,
            'successful native foundation build required')
    rows = {name: foundation['artifacts'][name] for name in NATIVE_NAMES}
    for row in rows.values():
        revision = row['source_revision']
        require(re.fullmatch(r'[a-f0-9]{40}', revision) is not None, 'native build revision required')
        git('rev-parse', revision + '^{tree}')
        require(not git('diff', revision, 'HEAD', '--', *NATIVE_PATHS),
                'native artifact sources differ from candidate')
        checked(row)
    directory = Path(rows['layerxd']['path']).parent
    require(all(Path(row['path']).parent == directory for row in rows.values()),
            'native artifacts must share their genuine source-bound directory')
    return {'path': str(path), 'sha256': digest(path), 'artifacts': rows}, directory


def source_snapshot(prefixes):
    inventory = subprocess.check_output(['git', 'ls-files', '-z'], cwd=ROOT).split(b'\0')
    result = {}
    for raw in inventory:
        if not raw:
            continue
        name = os.fsdecode(raw)
        path = Path(name)
        if any(part.startswith('.env') for part in path.parts):
            continue
        if name.startswith(tuple(prefixes)) and path.suffix in {'.rs', '.toml', '.lock', '.py', '.sh'}:
            result[name] = digest(ROOT / path)
    for name in ('platform/Cargo.toml', 'platform/Cargo.lock', 'rust-toolchain.toml',
                 'tools/paxeer-x/gates/103.6.2.sh', 'tools/runtime/run-with-clock.sh'):
        result[name] = digest(ROOT / name)
    return result


def command(argv, log, environment, bound=1800):
    deadline = float(os.environ.get('PAXEER_X_TASK_DEADLINE_EPOCH', str(time.time() + bound)))
    remaining = min(bound, deadline - time.time())
    require(remaining > 0, 'original task deadline exhausted')
    with log.open('w') as stream:
        result = subprocess.Popen([str(value) for value in argv], cwd=ROOT, env=environment,
                                  stdin=subprocess.DEVNULL, stdout=stream, stderr=subprocess.STDOUT,
                                  start_new_session=True)
        try:
            result.wait(timeout=remaining)
        except subprocess.TimeoutExpired:
            os.killpg(result.pid, signal.SIGTERM)
            try:
                result.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(result.pid, signal.SIGKILL)
                result.wait()
            raise
    log.chmod(0o600)
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, argv)
    return log.read_text()


def build():
    require(os.geteuid() == 0, 'real native UID separation requires root')
    require(not git('status', '--porcelain', '--untracked-files=normal'), 'clean source required')
    destination = manifest_path()
    require(not destination.exists(), 'build manifest already exists; use fresh task evidence')
    revision = git('rev-parse', 'HEAD')
    tree = git('rev-parse', 'HEAD^{tree}')
    environment = dict(os.environ, CARGO_BUILD_JOBS='4', PYTHONDONTWRITEBYTECODE='1',
                       CARGO_TARGET_DIR=os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/platform'),
                       PATH='/root/.cargo/bin:' + os.environ.get('PATH', ''))
    cargo = os.environ.get('CARGO', '/root/.cargo/bin/cargo')
    binary_command = [cargo, 'build', '--locked', '--manifest-path', 'platform/Cargo.toml',
                      '-p', 'layerx-platform-core', '--bin', 'layerx-core-boundary',
                      '-p', 'layerx-runtime-clock', '--bin', 'layerx-runtime-clock', '--message-format=json']
    test_command = [cargo, 'test', '--locked', '--manifest-path', 'platform/Cargo.toml',
                    '-p', 'layerx-platform-core', '--no-run', '--message-format=json']
    compiler = []
    for index, args in enumerate((binary_command, test_command)):
        output = command(args, destination.parent / f'core-build-{index}.log', environment)
        for line in output.splitlines():
            try:
                row = json.loads(line)
            except json.JSONDecodeError:
                continue
            if row.get('reason') == 'compiler-artifact':
                compiler.append(row)
    require(not git('status', '--porcelain', '--untracked-files=normal')
            and git('rev-parse', 'HEAD') == revision, 'source changed while compiling')
    prefixes = {'platform/hosted/core/'}
    binaries = {}
    harnesses = {}
    for row in compiler:
        package = Path(row['manifest_path'])
        if ROOT in package.parents:
            prefixes.add(str(package.parent.relative_to(ROOT)) + '/')
        executable = row.get('executable')
        if not executable:
            continue
        if row['target']['name'] == 'layerx-runtime-clock' and not row['profile']['test']:
            binaries['runtime_clock'] = artifact(executable, revision)
        if package != PACKAGE:
            continue
        if row['profile']['test']:
            key = '/'.join(row['target']['kind']) + ':' + row['target']['name']
            harnesses[key] = {'target': row['target'], **artifact(executable, revision)}
        elif row['target']['name'] == 'layerx-core-boundary':
            binaries['layerx-core-boundary'] = artifact(executable, revision)
    require({'runtime_clock', 'layerx-core-boundary'} <= binaries.keys(),
            'actual core and clock compiler artifacts missing')
    require({row['target']['name'] for row in harnesses.values()
             if row['target']['kind'] == ['test']} == INTEGRATION_TARGETS,
            'full core integration compiler artifact set required')
    require(any(row['target']['kind'] == ['lib'] for row in harnesses.values())
            and any(row['target']['kind'] == ['bin'] for row in harnesses.values()),
            'core library and binary unit harnesses required')
    require(set(harnesses) == HARNESS_KEYS, 'every declared core harness required')
    binaries['core'] = binaries['layerx-core-boundary']
    boundary = next(row for row in harnesses.values() if row['target']['name'] == 'boundary')
    binaries['boundary_tests'] = {key: boundary[key] for key in ('path', 'sha256', 'source_revision')}
    save(destination, {'version': 1, 'source_revision': revision, 'source_tree': tree,
         'build_exit': 0, 'core_binary_path': binaries['core']['path'], 'artifacts': binaries,
         'harnesses': harnesses, 'source_hashes': source_snapshot(prefixes),
         'commands': [binary_command, test_command], 'release_required': RELEASE_REQUIRED,
         'release_qualification': 'UNRUN'})
    return 0


def verified_build():
    destination = manifest_path()
    bound = load(destination)
    require(bound['version'] == 1 and bound['build_exit'] == 0
            and bound['source_revision'] == git('rev-parse', 'HEAD')
            and bound['source_tree'] == git('rev-parse', 'HEAD^{tree}')
            and not git('status', '--porcelain', '--untracked-files=normal'),
            'successful exact-source core build required')
    require(all(digest(ROOT / path) == value for path, value in bound['source_hashes'].items()),
            'compiled source changed')
    require(set(bound['harnesses']) == HARNESS_KEYS, 'every declared core harness required')
    for row in bound['artifacts'].values():
        require(row['source_revision'] == bound['source_revision'], 'core artifact source mismatch')
        checked(row)
    for row in bound['harnesses'].values():
        require(row['source_revision'] == bound['source_revision'], 'core harness source mismatch')
        checked(row)
    return destination, bound


def run_harnesses():
    destination, bound = verified_build()
    require(os.environ.get('PAXEER_X_CORE_ISOLATED_VERIFY') == bound['source_revision'],
            'isolated core verification required')
    total = 0
    records = []
    for index, (name, row) in enumerate(sorted(bound['harnesses'].items())):
        path = Path(row['path'])
        listing = command([path, '--list', '--format', 'terse'],
                          destination.parent / f'core-case-list-{index}.log', os.environ)
        cases = re.findall(r'^(.+): test$', listing, re.M)
        output = command([path, '--test-threads=1', '--nocapture'],
                         destination.parent / f'core-test-{index}.log', os.environ)
        print(output, end='', flush=True)
        summaries = re.findall(r'^test result: (ok|FAILED)\. (\d+) passed; (\d+) failed; '
                               r'(\d+) ignored; (\d+) measured; (\d+) filtered out;', output, re.M)
        require(len(summaries) == 1, 'one complete core harness summary required')
        state, passed, failed, ignored, measured, filtered = summaries[0]
        require(state == 'ok' and int(passed) == len(cases)
                and failed == ignored == measured == filtered == '0',
                'all core cases must execute without ignored or filtered tests')
        total += int(passed)
        records.append({'harness': name, 'cases': cases, 'exit_code': 0,
                        'log': str(destination.parent / f'core-test-{index}.log')})
    require(total > 0, 'empty core acceptance')
    verified_build()
    save(destination.parent / 'core-verify-result.json', {'source_revision': bound['source_revision'],
         'command': 'timeout 30m tools/paxeer-x/verify-task.sh 103.6.2', 'exit_code': 0,
         'tests': total, 'skipped': 0, 'harnesses': records,
         'release_required': RELEASE_REQUIRED, 'release_qualification': 'UNRUN'})
    print(f'PAXEER_X_GATE tests={total} skipped=0')
    return 0


def verify():
    from custody_chain import artifact_manifest
    require(os.geteuid() == 0, 'real native UID separation requires root')
    destination, bound = verified_build()
    native, directory = native_inputs()
    artifact_manifest(os.environ.get('LAYERX_CUSTODY_ARTIFACT_MANIFEST'))
    require(os.environ.get('LAYERX_MULTI_ASSET_RUNTIME_FIXTURE'),
            'genuine four-asset runtime fixture required for full core acceptance')
    socat = os.environ.get('LAYERX_TEST_SOCAT_BIN')
    require(socat, 'real socat executable required for supervisor reset')
    artifact(socat, bound['source_revision'])
    environment = dict(os.environ, LAYERX_TEST_NATIVE_BIN_DIR=str(directory),
                       LAYERX_TEST_PYTHON=sys.executable, PYTHONDONTWRITEBYTECODE='1',
                       LAYERX_RUNTIME_CLOCK_BIN=bound['artifacts']['runtime_clock']['path'],
                       PAXEER_X_CORE_ISOLATED_VERIFY=bound['source_revision'])
    output = command(['unshare', '--net', '--mount', '--pid', '--fork', '--mount-proc', 'bash', '-c',
        'set -e; mount --make-rprivate /; ip link set lo up; exec "$@"', 'core-real-suite',
        ROOT / 'tools/runtime/run-with-clock.sh', sys.executable, Path(__file__), '--verify-isolated'],
        destination.parent / 'core-isolated-verify.log', environment)
    require(digest(native['path']) == native['sha256'], 'native foundation changed while verifying')
    print(output, end='', flush=True)
    return 0


def main():
    args = sys.argv[1:]
    require(args in ([], ['--build'], ['--verify-isolated']), 'unknown core qualification selector')
    if args == ['--build']:
        return build()
    if args == ['--verify-isolated']:
        return run_harnesses()
    return verify()


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (KeyError, OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print('core-qualification: refusal: ' + str(error), file=sys.stderr)
        sys.exit(error.returncode if isinstance(error, subprocess.CalledProcessError) else 1)
