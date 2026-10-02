#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[4]
sys.path.insert(0, str(ROOT / 'tests/daemon'))

NATIVE_PATHS = ('Makefile', 'src', 'include', 'cmd', 'programs', 'agent',
                'contracts/config/checkpoint-settlement.json')
CASES = ('funded_admin_send_is_proven_and_replayed_after_restart',
         'funded_receipt_archive_survives_actual_reset_and_refuses_storage_faults')


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def git(*arguments):
    return subprocess.check_output(['git', *arguments], cwd=ROOT, text=True).strip()


def document(variable):
    location = os.environ.get(variable)
    require(location, variable + ' is required')
    path = Path(location)
    info = path.lstat()
    require(path.is_absolute() and stat.S_ISREG(info.st_mode) and
            info.st_uid == os.geteuid() and info.st_nlink == 1 and not info.st_mode & 0o077,
            variable + ' must name an owned private regular file')
    return json.loads(path.read_text())


def executable(record, revision):
    require(record['source_revision'] == revision, 'executable source revision mismatch')
    path = Path(record['path'])
    require(path.is_absolute() and not any(p.is_symlink() for p in (path, *path.parents)),
            'absolute executable path without symlinks required')
    require(path.is_file() and os.access(path, os.X_OK), 'executable missing')
    with path.open('rb') as stream:
        require(stream.read(4) == b'\x7fELF', 'real executable ELF required')
        stream.seek(0)
        require(hashlib.file_digest(stream, 'sha256').hexdigest() == record['sha256'],
                'executable digest mismatch')
    return path


def watch_supervisor(path, output):
    import ctypes
    import select
    import struct
    import time

    library = ctypes.CDLL(None, use_errno=True)
    descriptor = library.inotify_init1(os.O_CLOEXEC | os.O_NONBLOCK)
    require(descriptor >= 0, 'inotify unavailable')
    try:
        watch = library.inotify_add_watch(descriptor, os.fsencode(path), 0x80)
        require(watch >= 0, 'durable supervisor state watch unavailable')
        Path(output).write_bytes(b'')
        Path(output + '.ready').write_bytes(b'ready')
        deadline = time.monotonic() + 900
        while time.monotonic() < deadline:
            if not select.select([descriptor], [], [], 1)[0]:
                continue
            events = os.read(descriptor, 65536)
            offset = 0
            while offset < len(events):
                _, mask, _, length = struct.unpack_from('iIII', events, offset)
                name = events[offset + 16:offset + 16 + length].split(b'\0', 1)[0]
                offset += 16 + length
                require(not mask & (0x4000 | 0x8000), 'supervisor watcher overflow or lost watch')
                if mask & 0x80 and name == b'state.json':
                    with Path(output).open('ab') as stream:
                        stream.write(b'durable supervisor reset admitted\n')
                        stream.flush()
                        os.fsync(stream.fileno())
        raise RuntimeError('supervisor watcher deadline')
    finally:
        os.close(descriptor)


def main():
    count = 0
    try:
        from custody_chain import artifact_manifest
        require(os.geteuid() == 0, 'real process harness requires root')
        require(not git('status', '--porcelain', '--untracked-files=normal'), 'clean source required')
        revision = git('rev-parse', 'HEAD')
        build = document('PAXEER_X_CORE_BUILD_MANIFEST')
        require(build['version'] == 1 and build['build_exit'] == 0 and
                build['source_revision'] == revision and
                build['source_tree'] == git('rev-parse', 'HEAD^{tree}'),
                'successful source-bound core build required')
        boundary = executable(build['artifacts']['boundary_tests'], revision)
        core = executable(build['artifacts']['core'], revision)
        require(build['core_binary_path'] == str(core), 'test compiled core binary path mismatch')
        foundation = document('PAXEER_X_FOUNDATION_ARTIFACT_MANIFEST')
        require(foundation['version'] == 1 and foundation['build']['exit_code'] == 0,
                'successful native build required')
        native_revision = foundation['source_revision']
        require(foundation['source_tree'] == git('rev-parse', native_revision + '^{tree}'),
                'native source tree mismatch')
        require(not git('diff', native_revision, revision, '--', *NATIVE_PATHS),
                'native sources differ from candidate')
        native = {name: executable(foundation['artifacts'][name], native_revision)
                  for name in ('layerxd', 'layerx-genesis-build', 'layerx-handover')}
        directory = native['layerxd'].parent
        require(all(path.parent == directory for path in native.values()),
                'native executables must share one source-bound directory')
        artifact_manifest(os.environ.get('LAYERX_CUSTODY_ARTIFACT_MANIFEST'))
        socat = Path(os.environ['LAYERX_TEST_SOCAT_BIN'])
        require(socat.is_absolute() and socat.is_file() and os.access(socat, os.X_OK),
                'real socat executable required')
        with socat.open('rb') as stream:
            require(stream.read(4) == b'\x7fELF', 'socat must be an ELF executable')
        env = os.environ.copy()
        env.update(LAYERX_TEST_NATIVE_BIN_DIR=str(directory), LAYERX_TEST_RETAIN_STATE='1',
                   LAYERX_TEST_PYTHON=sys.executable)
        result = subprocess.run([str(boundary), '--test-threads=1', '--nocapture', '--exact', *CASES],
                                cwd=ROOT, env=env, stdout=subprocess.PIPE,
                                stderr=subprocess.STDOUT, text=True, timeout=840)
        print(result.stdout, end='')
        summaries = re.findall(r'^test result: (ok|FAILED)\. (\d+) passed; (\d+) failed; '
                               r'(\d+) ignored; (\d+) measured; (\d+) filtered out;', result.stdout, re.M)
        if len(summaries) == 1:
            count = int(summaries[0][1])
        require(result.returncode == 0 and len(summaries) == 1, 'real receipt retention cases failed')
        state, passed, failed, ignored, measured, _ = summaries[0]
        require(state == 'ok' and int(passed) == len(CASES) and failed == ignored == measured == '0',
                'exact executed case count required')
        count = int(passed)
        return 0
    except (ImportError, OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        print('receipt-retention: refusal: ' + str(error), file=sys.stderr)
        return 1
    finally:
        print(f'PAXEER_X_GATE tests={count} skipped=0')


if __name__ == '__main__':
    if len(sys.argv) == 4 and sys.argv[1] == '--watch-supervisor':
        watch_supervisor(sys.argv[2], sys.argv[3])
    else:
        sys.exit(main())
