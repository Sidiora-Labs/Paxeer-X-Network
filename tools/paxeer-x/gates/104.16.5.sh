#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
mode=verify
if [[ ${1:-} == --build-artifacts && $# == 1 ]]; then
  mode=build
elif (($#)); then
  printf 'usage: %s [--build-artifacts]\n' "$0" >&2
  exit 2
fi
exec python3 - "$mode" <<'PY'
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import sys
import tempfile
import time

ROOT = Path.cwd().resolve()
MODE = sys.argv[1]
COUNT = 0
CHILDREN = []
SAMPLES = {
    'seller': 'platform/docs/samples/paid-endpoint-express',
    'buyer': 'platform/docs/samples/first-payment-typescript',
}
SOURCES = (
    'platform/Cargo.toml', 'platform/Cargo.lock',
    'platform/tools/benchmark/Cargo.toml', 'platform/tools/benchmark/src/main.rs',
    'tools/paxeer-x/gates/104.16.5.sh',
    'platform/Makefile.inc', '.github/workflows/platform.yml',
    *(directory + '/' + name for directory in SAMPLES.values()
      for name in ('index.mjs', 'package.json')),
)

def require(value, reason):
    if not value:
        raise RuntimeError(reason)

def admitted(path):
    require(not any(part == '.env' or part.startswith('.env.') for part in path.parts),
            'credential path refused')
    return path

def read(path, private=False):
    path = admitted(Path(path))
    require(path.is_absolute() and not path.is_symlink(), 'absolute regular input required')
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(descriptor, 'rb') as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1
                and info.st_size <= 16 * 1024 * 1024, 'bounded regular input required')
        if private:
            require(info.st_uid == os.geteuid() and info.st_mode & 0o077 == 0,
                    'protected owner input required')
        return stream.read(16 * 1024 * 1024 + 1)

def digest(path):
    path = admitted(Path(path))
    require(path.is_absolute() and not path.is_symlink(), 'absolute regular digest input required')
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(descriptor, 'rb') as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1
                and info.st_size <= 1024 * 1024 * 1024, 'bounded regular digest input required')
        result = hashlib.sha256()
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            result.update(chunk)
        return result.hexdigest()

def private_json(path):
    return json.loads(read(Path(path), True))

def write(path, value):
    path = admitted(Path(path))
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'wb') as stream:
        stream.write(value)
        stream.flush()
        os.fsync(stream.fileno())

def json_write(path, value):
    write(path, (json.dumps(value, indent=2) + '\n').encode())

def private_directory(path):
    path = admitted(Path(path))
    info = path.stat()
    require(path.is_absolute() and path.resolve() == path and not path.is_symlink()
            and stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid()
            and info.st_mode & 0o077 == 0 and not path.is_relative_to(ROOT),
            'private owner directory outside checkout required')
    return path

def run(argv, output, expected, timeout):
    descriptor = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'wb') as stream:
        child = subprocess.Popen(argv, cwd=ROOT, stdin=subprocess.DEVNULL,
                                 stdout=stream, stderr=subprocess.STDOUT, start_new_session=True)
        CHILDREN.append(child)
        try:
            code = child.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            os.killpg(child.pid, signal.SIGKILL)
            child.wait()
            raise RuntimeError('bounded benchmark process timeout') from None
    require(code == expected, 'actual benchmark returned unexpected exit: ' + str(code))
    return read(output).decode('utf-8')

def source_manifest():
    tracked = subprocess.check_output(['git', 'ls-files', '-z', '--', *SOURCES]).split(b'\0')
    require({entry.decode() for entry in tracked if entry} == set(SOURCES),
            'published benchmark source closure required')
    require(subprocess.run(['git', 'diff', '--quiet', 'HEAD', '--', *SOURCES]).returncode == 0,
            'benchmark source differs from published revision')
    return {relative: digest(ROOT / relative) for relative in SOURCES}

def build(manifest_path):
    private_directory(manifest_path.parent)
    require(not manifest_path.exists(), 'fresh artifact manifest required')
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
    sources = source_manifest()
    target = Path(os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/platform'))
    require(target.is_absolute() and not target.is_relative_to(ROOT), 'external absolute build target required')
    environment = os.environ.copy()
    environment['CARGO_TARGET_DIR'] = str(target)
    environment['CARGO_BUILD_JOBS'] = '2'
    command = ['cargo', 'build', '--locked', '--manifest-path', 'platform/Cargo.toml',
               '-p', 'layerx-platform-benchmark']
    child = subprocess.Popen(command, cwd=ROOT, env=environment,
                             stdin=subprocess.DEVNULL, start_new_session=True)
    CHILDREN.append(child)
    try:
        code = child.wait(timeout=20 * 60)
    except subprocess.TimeoutExpired:
        os.killpg(child.pid, signal.SIGKILL)
        child.wait()
        raise RuntimeError('bounded benchmark build timeout') from None
    require(code == 0, 'declared benchmark build failed exit: ' + str(code))
    require(source_manifest() == sources, 'benchmark source changed during build')
    executable = target / 'debug/layerx-platform-benchmark'
    require(executable.is_file() and not executable.is_symlink() and os.access(executable, os.X_OK),
            'actual benchmark executable required')
    frozen_executable = manifest_path.parent / 'layerx-platform-benchmark'
    source_descriptor = os.open(executable, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(source_descriptor, 'rb') as source:
        original = os.fstat(source.fileno())
        require(stat.S_ISREG(original.st_mode) and original.st_size <= 1024 * 1024 * 1024,
                'bounded compiled executable required')
        destination = os.open(frozen_executable,
            os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o700)
        source_hash = hashlib.sha256()
        with os.fdopen(destination, 'wb') as output:
            for chunk in iter(lambda: source.read(1024 * 1024), b''):
                source_hash.update(chunk)
                output.write(chunk)
            output.flush()
            os.fsync(output.fileno())
        final = os.fstat(source.fileno())
        require((original.st_dev, original.st_ino, original.st_size, original.st_mtime_ns, original.st_ctime_ns)
                == (final.st_dev, final.st_ino, final.st_size, final.st_mtime_ns, final.st_ctime_ns),
                'compiled executable changed during freezing')
    require(digest(frozen_executable) == source_hash.hexdigest(), 'compiled artifact copy digest mismatch')
    directory_descriptor = os.open(manifest_path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(directory_descriptor)
    finally:
        os.close(directory_descriptor)
    json_write(manifest_path, {'version': 'layerx-ten-line-artifacts-v1',
               'source_revision': revision, 'source_files': sources,
               'benchmark': {'path': str(frozen_executable), 'sha256': digest(frozen_executable)}})

def sample_report(path, snapshot, passed):
    report = private_json(path)
    require(set(report) == {'version', 'registry_url', 'snapshot_id', 'snapshot_sha256',
                           'counting_rule', 'limit', 'passed', 'samples'}, 'closed measurement required')
    require(report['version'] == 'layerx-ten-line-measurement-v1'
            and report['snapshot_id'] == snapshot['snapshot_id']
            and report['registry_url'] == snapshot['registry_url'].rstrip('/')
            and report['limit'] == 10 and type(report['limit']) is int
            and report['passed'] is passed and isinstance(report['counting_rule'], str)
            and report['counting_rule'], 'actual measurement identity mismatch')
    require(isinstance(report['samples'], list) and len(report['samples']) == 2,
            'both actual sample measurements required')
    measured = {}
    for sample in report['samples']:
        require(set(sample) == {'name', 'source', 'source_sha256', 'first_integration_line',
                'last_integration_line', 'integration_lines', 'within_limit', 'lock_sha256',
                'package_versions', 'registry_packages', 'published_imports_checked'},
                'closed sample measurement required')
        name = sample['name']
        require(name in SAMPLES and name not in measured, 'exact seller and buyer measurements required')
        entry = snapshot['samples'][name]
        require(sample['source'] == SAMPLES[name] + '/index.mjs'
                and sample['source_sha256'] == entry['quickstart_sha256']
                and sample['lock_sha256'] == entry['lock_sha256']
                and type(sample['integration_lines']) is int and sample['integration_lines'] > 0
                and sample['within_limit'] is (sample['integration_lines'] <= 10)
                and sample['published_imports_checked'] is True
                and type(sample['registry_packages']) is int and sample['registry_packages'] > 0
                and isinstance(sample['package_versions'], dict) and sample['package_versions'],
                'genuine published import and line-count evidence required')
        measured[name] = sample
    require(set(measured) == set(SAMPLES), 'both actual measured samples required')
    return report, measured

def variant(state, label, snapshot, change_source=None, change_lock=None):
    repository = state / (label + '-source')
    repository.mkdir(mode=0o700)
    mutated = copy.deepcopy(snapshot)
    for name, relative in SAMPLES.items():
        directory = repository / relative
        directory.mkdir(mode=0o700, parents=True)
        source = read(ROOT / relative / 'index.mjs')
        if name == 'seller' and change_source:
            source = change_source(source)
        write(directory / 'index.mjs', source)
        write(directory / 'package.json', read(ROOT / relative / 'package.json'))
        mutated['samples'][name]['quickstart_sha256'] = hashlib.sha256(source).hexdigest()
    if change_lock:
        entry = mutated['samples']['seller']
        lock = json.loads(read(Path(entry['lock_file']), True))
        change_lock(lock)
        lock_path = state / (label + '-lock.json')
        json_write(lock_path, lock)
        entry['lock_file'] = str(lock_path)
        entry['lock_sha256'] = digest(lock_path)
    snapshot_path = state / (label + '-snapshot.json')
    json_write(snapshot_path, mutated)
    return repository, snapshot_path, mutated

def verify(manifest_path):
    global COUNT
    raw_snapshot = os.environ.get('LAYERX_TEN_LINE_REGISTRY_SNAPSHOT')
    registry_url = os.environ.get('LAYERX_TEN_LINE_REGISTRY_URL')
    require(raw_snapshot or registry_url,
            'prerequisite: protected snapshot or explicit actual registry URL required')
    manifest = private_json(manifest_path)
    require(set(manifest) == {'version', 'source_revision', 'source_files', 'benchmark'}
            and manifest['version'] == 'layerx-ten-line-artifacts-v1', 'closed benchmark artifacts required')
    require(manifest['source_revision'] == subprocess.check_output(
        ['git', 'rev-parse', 'HEAD'], text=True).strip()
        and manifest['source_files'] == source_manifest(), 'actual executable source provenance mismatch')
    require(set(manifest['benchmark']) == {'path', 'sha256'}, 'closed executable descriptor required')
    executable = Path(manifest['benchmark']['path'])
    require(executable.is_absolute() and executable.is_file() and not executable.is_symlink()
            and os.access(executable, os.X_OK) and digest(executable) == manifest['benchmark']['sha256'],
            'genuine source-bound benchmark executable required')
    evidence = private_directory(Path(os.environ['PAXEER_X_EVIDENCE_DIR']))
    state = Path(tempfile.mkdtemp(prefix='104.16.5-', dir=evidence))
    state.chmod(0o700)
    deadline = time.monotonic() + 20 * 60
    if raw_snapshot:
        snapshot_path = Path(raw_snapshot)
    else:
        capture = state / 'registry-capture'
        run([str(executable), '--repository', str(ROOT), '--capture-snapshot',
             '--registry-url', registry_url, '--output', str(capture)],
            state / 'registry-capture.log', 0, max(1, deadline - time.monotonic()))
        snapshot_path = capture / 'registry-snapshot.json'
    snapshot = private_json(snapshot_path)
    def invoke(label, repository, supplied_snapshot, expected):
        output = state / (label + '-measurement')
        run([str(executable), '--repository', str(repository), '--snapshot', str(supplied_snapshot),
             '--output', str(output)], state / (label + '.log'), expected,
            max(1, deadline - time.monotonic()))
        return output
    output = invoke('published', ROOT, snapshot_path, 0)
    report, measured = sample_report(output / 'ten-line-counts.json', snapshot, True)
    require(report['snapshot_sha256'] == digest(snapshot_path)
            and all(sample['integration_lines'] <= 10 for sample in measured.values()),
            'retained published quickstarts exceed ten integration lines')
    COUNT += 2
    repository, supplied, changed = variant(state, 'oversized', snapshot, lambda value: value.replace(
        b'// layerx:end integration', b'void 0;\n' * 11 + b'// layerx:end integration', 1))
    oversized = invoke('oversized', repository, supplied, 1)
    report, measured = sample_report(oversized / 'ten-line-counts.json', changed, False)
    require(report['snapshot_sha256'] == digest(supplied)
            and measured['seller']['integration_lines'] > 10, 'actual oversized quickstart must fail')
    COUNT += 1
    repository, supplied, _ = variant(state, 'markers', snapshot, lambda value: value.replace(
        b'// layerx:end integration', b'// removed integration marker', 1))
    invalid = invoke('markers', repository, supplied, 78)
    require(not (invalid / 'ten-line-counts.json').exists(), 'invalid markers cannot issue measurement')
    require('ten-line-benchmark: complete-integration-block-required' in read(state / 'markers.log').decode(),
            'genuine marker refusal required')
    COUNT += 1
    def shortcut(lock):
        packages = lock.get('packages')
        require(isinstance(packages, dict), 'genuine published lock packages required')
        candidate = next((entry for name, entry in packages.items() if name), None)
        require(isinstance(candidate, dict), 'genuine published transitive package required')
        candidate['resolved'] = 'file:./local-shortcut'
    repository, supplied, _ = variant(state, 'local-shortcut', snapshot, change_lock=shortcut)
    invalid = invoke('local-shortcut', repository, supplied, 78)
    require(not (invalid / 'ten-line-counts.json').exists(), 'local shortcuts cannot issue measurement')
    require('ten-line-benchmark: snapshot-registry-resolution-required' in read(
            state / 'local-shortcut.log').decode(), 'genuine local shortcut refusal required')
    COUNT += 1
    print(json.dumps({'status': 'passed', 'artifact': str(output / 'ten-line-counts.json'),
                      'snapshot_id': snapshot['snapshot_id'], 'cases': COUNT}))

try:
    raw = os.environ.get('PAXEER_X_TEN_LINE_ARTIFACTS')
    require(raw, 'protected PAXEER_X_TEN_LINE_ARTIFACTS path required')
    artifacts = Path(raw)
    require(artifacts.is_absolute(), 'absolute artifact manifest path required')
    if MODE == 'build':
        build(artifacts)
    else:
        verify(artifacts)
except (Exception, KeyboardInterrupt) as error:
    print(json.dumps({'status': 'refused', 'reason': str(error) if isinstance(error, RuntimeError)
                      else type(error).__name__}), file=sys.stderr)
    raise SystemExit(78)
finally:
    for child in CHILDREN:
        if child.poll() is None:
            os.killpg(child.pid, signal.SIGKILL)
            child.wait()
    if MODE == 'verify':
        print('PAXEER_X_GATE tests=' + str(COUNT) + ' skipped=0')
PY
