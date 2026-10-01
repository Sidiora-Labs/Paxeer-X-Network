#!/usr/bin/env python3
"""Focused gate for metered stream accrual bounded by the stream lifetime.

Runs the prebuilt test_stream_meter target, which drives real METER, SETTLE,
PAUSE and CLOSE activities through the kernel on a primary, a replica and a
node restored from a durable snapshot store. Every prerequisite must already
exist; nothing is built here. The run log and the snapshot store go to a
private directory under PAXEER_X_EVIDENCE_DIR, outside the repository.
"""
import os
import re
import shlex
import stat
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
BINARY = ROOT / os.environ.get('BUILD_DIR', 'build') / 'tests' / 'test_stream_meter'
SOURCES = [
    'src/modules/stream/lx_stream_execution.h',
    'src/modules/stream/lx_stream_open.c',
    'src/modules/stream/lx_stream_meter.c',
    'src/modules/stream/lx_stream_accrue.c',
    'src/modules/stream/lx_stream_record.c',
    'tests/modules/test_stream_meter.c',
]
CASES = [
    'attested_meter',
    'metered_boundaries',
    'attestation_preimage',
    'meter_payload',
    'dispatch_fixture',
    'dispatched_window',
    'dispatched_refusals',
    'restart_and_replica',
]
CASE = re.compile(r'^case (\S+) ok$', re.MULTILINE)


def fail(message):
    print('metered-end-boundary: ' + message, file=sys.stderr)
    sys.exit(1)


def revision():
    result = subprocess.run(['git', '-C', str(ROOT), 'rev-parse', 'HEAD'],
                            capture_output=True, text=True)
    if result.returncode != 0:
        fail('cannot resolve the checkout revision')
    return result.stdout.strip()


def evidence_root():
    raw = os.environ.get('PAXEER_X_EVIDENCE_DIR')
    if not raw:
        fail('PAXEER_X_EVIDENCE_DIR is not set')
    path = Path(raw).resolve()
    if path == ROOT or ROOT in path.parents:
        fail('evidence directory is inside the repository')
    try:
        info = path.stat()
    except OSError:
        fail('evidence directory is unavailable')
    if not stat.S_ISDIR(info.st_mode) or info.st_mode & 0o077:
        fail('evidence directory must be a private directory')
    return path


def prerequisites():
    if not BINARY.is_file() or not os.access(BINARY, os.X_OK):
        fail('missing prebuilt target ' + str(BINARY.relative_to(ROOT)))
    built = BINARY.stat().st_mtime
    for source in SOURCES:
        path = ROOT / source
        if not path.is_file():
            fail('missing source ' + source)
        if path.stat().st_mtime > built:
            fail('prebuilt target is older than ' + source)


def main():
    head = revision()
    prerequisites()
    run = evidence_root() / 'metered-end-boundary' / time.strftime('%Y%m%dT%H%M%SZ', time.gmtime())
    store = run / 'snapshot-store'
    os.makedirs(store, mode=0o700)
    command = [str(BINARY), str(store)]
    result = subprocess.run(command, cwd=ROOT, capture_output=True, text=True, timeout=1500)
    output = result.stdout + result.stderr
    log = run / 'test_stream_meter.log'
    fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as stream:
        stream.write(output)
    sys.stdout.write(output)
    passed = CASE.findall(result.stdout)
    print('revision=' + head)
    print('command=' + shlex.join(command))
    print('exit_code=' + str(result.returncode))
    print('evidence=' + str(log))
    print('cases=' + str(len(passed)))
    if result.returncode != 0:
        fail('test_stream_meter exited with ' + str(result.returncode))
    if passed != CASES:
        fail('executed cases ' + ','.join(passed) + ' do not match ' + ','.join(CASES))
    return 0


if __name__ == '__main__':
    sys.exit(main())
