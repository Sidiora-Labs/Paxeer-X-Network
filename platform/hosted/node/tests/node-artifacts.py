#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[4]
SOURCE_PATHS = ('Makefile', 'rust-toolchain.toml', 'src', 'include', 'cmd', 'programs',
                'agent', 'platform/Cargo.toml', 'platform/Cargo.lock', 'platform/Makefile.inc',
                'tools/build', 'contracts/config/checkpoint-settlement.json',
                'platform/hosted/core', 'platform/hosted/internal',
                'platform/hosted/node/tests/probe')
IMAGE_PATHS = SOURCE_PATHS + ('platform/hosted/node', 'platform/hosted/human',
                            'docker/platform-node', 'migrations', 'contracts', 'precompiles')
NATIVE = ('layerxd', 'layerx-genesis-build', 'layerx-handover', 'layerxctl')
SCHEMA = 'layerx.node-artifacts.v1'


def command(args, **kwargs):
    try:
        return subprocess.run([str(x) for x in args], cwd=ROOT, check=True, **kwargs)
    except subprocess.CalledProcessError as error:
        if error.stdout:
            print(error.stdout.decode(errors='replace') if isinstance(error.stdout, bytes) else error.stdout,
                  end='', file=sys.stderr, flush=True)
        raise


def output(args):
    return command(args, stdout=subprocess.PIPE, text=True).stdout


def git(*args):
    return output(['git', '--no-optional-locks', *args]).strip()


def binding(revision, paths=SOURCE_PATHS):
    data = command(['git', 'ls-tree', '-r', '-z', '--full-tree', revision, '--', *paths],
                   stdout=subprocess.PIPE).stdout
    return hashlib.sha256(data).hexdigest()


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def clean():
    if git('status', '--porcelain=v1', '--untracked-files=normal'):
        raise ValueError('node artifact source is dirty')
    return git('rev-parse', 'HEAD')


def executable(path):
    path = Path(path)
    if not path.is_absolute() or any(p.is_symlink() for p in (path, *path.parents)):
        raise ValueError('node executable requires an absolute nonsymlink path')
    if not path.is_file() or not os.access(path, os.X_OK):
        raise ValueError('node executable unavailable: ' + str(path))
    with path.open('rb') as stream:
        if stream.read(4) != b'\x7fELF':
            raise ValueError('node executable is not ELF: ' + str(path))
    return {'path': str(path), 'sha256': digest(path)}


def image(reference):
    document = json.loads(output(['docker', 'image', 'inspect', reference]))
    if len(document) != 1:
        raise ValueError('one immutable node image required')
    row = document[0]
    revision = (row['Config'].get('Labels') or {}).get('org.opencontainers.image.revision', '')
    if not re.fullmatch('[0-9a-f]{40}', revision):
        raise ValueError('node image requires its actual source revision label')
    if binding(revision, IMAGE_PATHS) != binding('HEAD', IMAGE_PATHS):
        raise ValueError('node image source differs from current source')
    if row['Config'].get('User') != '4020:4020':
        raise ValueError('node image must run as uid/gid 4020')
    if row['Config'].get('Entrypoint') != ['/opt/layerx/supervisor.sh']:
        raise ValueError('node image supervisor entrypoint mismatch')
    if not re.fullmatch('sha256:[0-9a-f]{64}', row['Id']):
        raise ValueError('node image identifier is not immutable')
    return {'id': row['Id'], 'source_revision': revision}


def image_runtime(record):
    command(['docker', 'run', '--rm', '--pull=never', '--network=none', '--read-only',
             '--cap-drop=ALL', '--security-opt=no-new-privileges', '--pids-limit=32',
             '--memory=128m', '--entrypoint=/bin/sh', record['id'], '-ec',
             '\n'.join((
                 'test "$(id -u):$(id -g)" = 4020:4020',
                 'for binary in layerxd layerx-genesis-build layerx-handover layerxctl layerx-guarantor layerx-module-registry; do test -x /usr/local/bin/$binary; done',
                 'for file in bootstrap.sh supervisor.sh reset_state.py data_directory.py genesis_fees.py genesis-modules.conf genesis-module-fees.json checkpoint-settlement.json signer/client.py migrations/0007_history_index.sql; do test -r /opt/layerx/$file; done',
                 'libraries=$(ldd /usr/local/bin/layerxd)',
                 'printf "%s\\n" "$libraries"',
                 '! printf "%s\\n" "$libraries" | grep -q "not found"',
                 'printf "%s\\n" "$libraries" | grep -q "libcrypto.so.3"',
                 'printf "%s\\n" "$libraries" | grep -q "libsqlite3.so.0"',
                 '/usr/local/bin/layerxctl --help',
             ))], timeout=60)


def private_file(path):
    path = Path(path)
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid != os.geteuid() or info.st_mode & 0o077:
        raise ValueError('node manifest must be an owned private regular file')
    resolved = path.resolve()
    if resolved == ROOT or ROOT in resolved.parents:
        raise ValueError('node manifest must be outside source')
    return path


def build(args):
    revision = clean()
    snapshot = binding(revision)
    image_record = image(args.image)
    destination = Path(args.output)
    if not destination.is_absolute() or destination.exists() or ROOT in destination.resolve().parents:
        raise ValueError('new absolute private output outside source required')
    cargo = os.environ.get('PLATFORM_CARGO', 'cargo')
    os.environ['CARGO_BUILD_JOBS'] = str(args.jobs)
    os.environ.pop('CARGO_TARGET_DIR', None)
    command(['make', '-j' + str(args.jobs), 'LXP_REVISION=' + revision, *NATIVE],
            env=dict(os.environ, CARGO_BUILD_JOBS=str(args.jobs)))
    command([cargo, 'build', '--locked', '--offline', '--release', '--manifest-path',
             'platform/hosted/node/tests/probe/Cargo.toml'])
    built = output([cargo, 'test', '--locked', '--offline', '--release', '--no-run',
                    '--message-format=json', '--manifest-path', 'cmd/layerxctl/Cargo.toml'])
    tests = []
    for line in built.splitlines():
        row = json.loads(line)
        if row.get('reason') == 'compiler-artifact' and row.get('profile', {}).get('test') and row.get('executable'):
            tests.append({'target': row['target']['name'], **executable(row['executable'])})
    if not tests or len({row['path'] for row in tests}) != len(tests):
        raise ValueError('CLI test artifacts missing or duplicated')
    if 'files' not in {row['target'] for row in tests}:
        raise ValueError('CLI file refusal corpus missing')
    binaries = {name: executable(ROOT / 'build/bin' / name) for name in NATIVE}
    probe = executable(ROOT / 'platform/hosted/node/tests/probe/target/release/layerx-node-probe')
    if clean() != revision or binding('HEAD') != snapshot:
        raise ValueError('node source changed during build')
    value = {'schema': SCHEMA, 'source_revision': revision, 'source_binding': snapshot,
             'source_paths': list(SOURCE_PATHS), 'binaries': binaries, 'probe': probe,
             'cli_tests': tests, 'image': image_record}
    fd = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, sort_keys=True)
        stream.write('\n')


def load(path):
    clean()
    value = json.loads(private_file(path).read_text())
    if value.get('schema') != SCHEMA or value.get('source_paths') != list(SOURCE_PATHS):
        raise ValueError('node artifact manifest schema mismatch')
    revision = value.get('source_revision', '')
    if not re.fullmatch('[0-9a-f]{40}', revision):
        raise ValueError('node artifact source revision invalid')
    if value.get('source_binding') != binding(revision) or binding(revision) != binding('HEAD'):
        raise ValueError('node artifact source mismatch')
    if set(value.get('binaries', {})) != set(NATIVE):
        raise ValueError('node native artifact set mismatch')
    tests = value.get('cli_tests', [])
    if not tests or 'files' not in {row['target'] for row in tests}:
        raise ValueError('node CLI test corpus missing')
    if len({row['path'] for row in tests}) != len(tests):
        raise ValueError('duplicate node CLI test executable')
    for row in [*value['binaries'].values(), value['probe'], *tests]:
        actual = executable(row['path'])
        if actual['sha256'] != row['sha256']:
            raise ValueError('node artifact hash mismatch: ' + row['path'])
    if image(value['image']['id']) != value['image']:
        raise ValueError('node image identity mismatch')
    return value


def run_tests(args):
    manifest = args.manifest or os.environ.get('PAXEER_X_NODE_ARTIFACT_MANIFEST')
    if not manifest:
        raise ValueError('PAXEER_X_NODE_ARTIFACT_MANIFEST required')
    value = load(manifest)
    custody = os.environ.get('LAYERX_CUSTODY_ARTIFACT_MANIFEST')
    if not custody:
        raise ValueError('LAYERX_CUSTODY_ARTIFACT_MANIFEST required')
    sys.path.insert(0, str(ROOT / 'tests/daemon'))
    from custody_chain import artifact_manifest
    artifact_manifest(custody)
    directories = {str(Path(row['path']).parent) for row in value['binaries'].values()}
    if len(directories) != 1:
        raise ValueError('node native executables must share one directory')
    environment = dict(os.environ, LAYERX_TEST_NATIVE_BIN_DIR=directories.pop(),
                       LAYERX_NODE_TEST_PROBE_BIN=value['probe']['path'])
    environment['PATH'] = environment['LAYERX_TEST_NATIVE_BIN_DIR'] + os.pathsep + environment.get('PATH', '')
    image_runtime(value['image'])
    count = 1
    cli_count = 0
    for row in value['cli_tests']:
        completed = command([row['path'], '--test-threads=1'], stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, text=True, env=environment)
        print(completed.stdout, end='', flush=True)
        summaries = re.findall(r'test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;', completed.stdout)
        if len(summaries) != 1 or any(int(n) for n in summaries[0][1:]):
            raise ValueError('CLI corpus failed, skipped or uncounted')
        cli_count += int(summaries[0][0])
    if cli_count == 0:
        raise ValueError('empty CLI corpus')
    count += cli_count
    command(['bash', '-n', 'platform/hosted/node/bootstrap.sh',
             'platform/hosted/node/supervisor.sh', 'platform/hosted/node/sequencer-env.sh',
             'platform/hosted/node/tests/node-test.sh'])
    command(['bash', 'platform/hosted/node/tests/node-test.sh'], env=environment)
    count += 1
    for script in ('sequencer-seed-test.py', 'bootstrap-test.py', 'node-topology-test.py'):
        completed = command([sys.executable, 'platform/hosted/node/tests/' + script],
                            env=environment, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        print(completed.stdout, end='', flush=True)
        summaries = re.findall(r'Ran (\d+) tests? in ', completed.stdout)
        if len(summaries) != 1 or int(summaries[0]) == 0 or not re.search(r'^OK$', completed.stdout, re.M):
            raise ValueError('node regression corpus missing, skipped or uncounted')
        count += int(summaries[0])
    load(manifest)
    print('PAXEER_X_GATE tests=%d skipped=0' % count, flush=True)


def main():
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest='mode', required=True)
    producer = sub.add_parser('build')
    producer.add_argument('--output', required=True)
    producer.add_argument('--image', required=True)
    producer.add_argument('--jobs', type=int, default=5, choices=range(1, 6))
    consumer = sub.add_parser('run')
    consumer.add_argument('--manifest')
    args = parser.parse_args()
    if args.mode == 'build':
        build(args)
    else:
        run_tests(args)


if __name__ == '__main__':
    try:
        main()
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError) as error:
        print('node-artifacts: ' + str(error), file=sys.stderr)
        sys.exit(error.returncode if isinstance(error, subprocess.CalledProcessError) else 1)
