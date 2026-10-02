#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
SCHEMA = 'paxeer-x.storage-recovery-artifacts.v1'
TESTS = ('lxp_test_recovery', 'lxp_test_log_durability', 'lxp_test_batch_wal_recovery')
ORDER_CHECK = 'tests/storage/check_daemon_recovery_order.sh'
SOURCES = tuple('tests/storage/' + name + '.c' for name in TESTS) + (
    ORDER_CHECK, 'cmd/layerxd/lxp_daemon_process.c',
    'cmd/layerxd/lxp_daemon_batch_wal.c',
    'cmd/layerxd/lxp_daemon_batch_wal.h',
    'cmd/layerxd/lxp_daemon_handover_history.c',
    'cmd/layerxd/lxp_daemon_receipt_authority.c',
    'cmd/layerxd/lxp_daemon_evidence.c',
    'src/modules/programs/feed_store.c',
    'tools/paxeer-x/gates/104.38.1.sh',
    'tools/qualification/paxeer-x/storage-recovery-producer.py',
    'tools/qualification/paxeer-x/storage-recovery.py')
TOOLS = ('make', 'cc', 'cargo')
LOG = 'build.log'


def require(condition, message):
    if not condition:
        raise ValueError(message)


def capture(command):
    return subprocess.check_output(command, cwd=ROOT, stdin=subprocess.DEVNULL,
                                   text=True, timeout=60).strip()


def identity():
    require(not capture(['git', 'status', '--porcelain=v1', '--untracked-files=normal']),
            'candidate source is dirty')
    return {'revision': capture(['git', 'rev-parse', '--verify', 'HEAD^{commit}']),
            'tree': capture(['git', 'rev-parse', '--verify', 'HEAD^{tree}']), 'dirty': False}


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def sources():
    found = {}
    for name in SOURCES:
        path = ROOT / name
        require(path.is_file() and not path.is_symlink(), 'missing scoped source: ' + name)
        found[name] = digest(path)
    return found


def artifact(directory, relative):
    path = directory / relative
    require(not path.is_symlink(), 'artifact must not be a symlink: ' + relative)
    info = path.stat()
    require(stat.S_ISREG(info.st_mode) and info.st_size > 0,
            'missing or empty artifact: ' + relative)
    return {'path': relative, 'sha256': digest(path)}


def private_directory(path, create=False):
    path = Path(path).absolute()
    require(not path.is_symlink(), 'private directory must not be a symlink')
    if create:
        path.mkdir(mode=0o700, parents=True, exist_ok=True)
    path = path.resolve(strict=True)
    info = path.stat()
    require(path != ROOT and ROOT not in path.parents,
            'artifact directory must be outside the repository')
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid()
            and not info.st_mode & 0o077, 'private caller-owned directory required')
    return path


def write_private(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())


def load_private(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd) as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                and not info.st_mode & 0o077, 'private caller-owned manifest required')
        return json.load(stream)


def toolchain():
    missing = [tool for tool in TOOLS if shutil.which(tool) is None]
    require(not missing, 'missing toolchain: ' + ', '.join(missing))
    settings = {}
    for name in ('PROGRAMS_TARGET_DIR', 'PROGRAMS_RUNTIME_LIB'):
        value = os.environ.get(name)
        require(value, name + ' is not set')
        settings[name] = value
    return settings


def build(output):
    source = identity()
    settings = toolchain()
    directory = Path(output).absolute()
    require(not directory.exists() or (directory.is_dir() and not any(directory.iterdir())),
            'build output must be absent or a fresh empty directory')
    directory = private_directory(directory, create=True)
    scoped = sources()
    command = ['make', '-j3', 'PROGRAMS_TARGET_DIR=' + settings['PROGRAMS_TARGET_DIR'],
               'PROGRAMS_RUNTIME_LIB=' + settings['PROGRAMS_RUNTIME_LIB']]
    command += ['layerxd'] + ['build/tests/' + name for name in TESTS]
    record = {'schema': SCHEMA, **source, 'command': command, 'cwd': str(ROOT),
              'log': LOG, 'sources': scoped}
    print('BUILD ' + json.dumps(command), flush=True)
    fd = os.open(directory / LOG, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(('BUILD_COMMAND ' + json.dumps(command) + '\n'
                      + 'BUILD_CWD ' + str(ROOT) + '\n').encode())
        stream.flush()
        try:
            exit_code = subprocess.run(command, cwd=ROOT, stdin=subprocess.DEVNULL,
                                       stdout=stream, stderr=subprocess.STDOUT,
                                       timeout=1140).returncode
        except subprocess.TimeoutExpired:
            exit_code = 124
        stream.write(f'\nBUILD_EXIT {exit_code}\n'.encode())
        stream.flush()
        os.fsync(stream.fileno())
    record['exit'] = exit_code
    print('BUILD_LOG ' + str(directory / LOG), flush=True)
    if exit_code != 0:
        write_private(directory / 'manifest.json', record)
        raise ValueError(f'build command exited {exit_code}; log={directory / LOG}')
    require(identity() == source and sources() == scoped, 'source changed during build')
    binaries = directory / 'bin'
    binaries.mkdir(mode=0o700)
    artifacts = {}
    for name in TESTS:
        built = ROOT / 'build/tests' / name
        require(built.is_file() and os.access(built, os.X_OK),
                'make did not produce executable ' + name)
        shutil.copyfile(built, binaries / name)
        (binaries / name).chmod(0o700)
        artifacts[name] = artifact(directory, 'bin/' + name)
    record['artifacts'] = artifacts
    record['log_sha256'] = digest(directory / LOG)
    write_private(directory / 'manifest.json', record)
    print('STORAGE_RECOVERY_BUILD_MANIFEST ' + str(directory / 'manifest.json'))


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    try:
        build(args.output)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print(f'storage recovery build refused: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
