#!/usr/bin/env python3
import argparse
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]
FRONTEND = ROOT / 'explorer/frontend'
FILTERS = [
    'lib/api', 'ui/pages', 'ui/shared/pagination', 'ui/shared/nft',
    'ui/address/utils', 'ui/address/details/AddressQrCode', 'ui/address/AddressCoinBalance',
]


def refuse(reason, code=78):
    print('scan load gate refused: ' + reason, file=sys.stderr)
    raise SystemExit(code)


def identity():
    revision = subprocess.check_output(['git', '-C', str(ROOT), 'rev-parse', 'HEAD'], text=True).strip()
    tree = subprocess.check_output(['git', '-C', str(ROOT), 'rev-parse', 'HEAD^{tree}'], text=True).strip()
    dirty = subprocess.check_output([
        'git', '-C', str(ROOT), 'status', '--porcelain=v1', '--untracked-files=normal',
    ], text=True)
    if dirty:
        refuse('qualification requires a clean published source candidate')
    return {'revision': revision, 'tree': tree}


def evidence_directory():
    configured = os.environ.get('PAXEER_X_EVIDENCE_DIR')
    if not configured:
        refuse('PAXEER_X_EVIDENCE_DIR must name protected qualification storage')
    directory = Path(configured).resolve()
    info = directory.stat()
    if (directory == ROOT or ROOT in directory.parents or not stat.S_ISDIR(info.st_mode)
            or info.st_uid != os.geteuid() or info.st_mode & 0o077):
        refuse('evidence storage must be private, owner-controlled and outside source')
    return directory


def execute(command, log, deadline):
    remaining = deadline - time.time()
    if remaining <= 0:
        refuse('original task deadline elapsed', 124)
    with log.open('xb') as stream:
        os.chmod(log, 0o600)
        try:
            code = subprocess.run(command, cwd=FRONTEND, stdin=subprocess.DEVNULL,
                                  stdout=stream, stderr=subprocess.STDOUT,
                                  timeout=remaining).returncode
        except subprocess.TimeoutExpired:
            code = 124
    print('command=' + json.dumps(command) + ' exit=' + str(code) + ' log=' + str(log))
    if code:
        raise SystemExit(code if code > 0 else 1)


def write_private(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, sort_keys=True)
        stream.write('\n')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--build', action='store_true')
    arguments = parser.parse_args()
    source = identity()
    directory = evidence_directory()
    configured_deadline = os.environ.get('PAXEER_X_TASK_DEADLINE_EPOCH')
    if configured_deadline is None:
        refuse('PAXEER_X_TASK_DEADLINE_EPOCH must preserve the admitted task cutoff')
    deadline = int(configured_deadline)
    attempt = str(time.time_ns())
    node = str(Path(os.environ.get('PAXEER_X_SCAN_NODE', sys.executable)).resolve())
    if 'PAXEER_X_SCAN_NODE' not in os.environ:
        refuse('PAXEER_X_SCAN_NODE must name the actual admitted Node executable')
    build_record = directory / 'scan-load-build.json'
    if arguments.build:
        command = [node, str(FRONTEND / 'node_modules/next/dist/bin/next'), 'build']
        execute(command, directory / ('scan-load-build-' + attempt + '.log'), deadline)
        if identity() != source:
            refuse('published source changed during compilation')
        write_private(build_record, {'schema': 'layerx.scan-load-build.v1', 'source': source,
                                     'command': command, 'exit_code': 0})
        return

    fd = os.open(build_record, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd) as stream:
        info = os.fstat(stream.fileno())
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid()
                or info.st_mode & 0o077 or info.st_size > 4096):
            refuse('build evidence must be a bounded protected owner file')
        build = json.load(stream)
    if (build.get('schema') != 'layerx.scan-load-build.v1' or build.get('source') != source
            or build.get('exit_code') != 0):
        refuse('focused verification requires this candidate\'s successful declared build')
    report = directory / ('scan-load-vitest-' + attempt + '.json')
    command = [node, str(FRONTEND / 'node_modules/vitest/vitest.mjs'), 'run', *FILTERS,
               '--reporter=json', '--outputFile=' + str(report)]
    execute(command, directory / ('scan-load-verify-' + attempt + '.log'), deadline)
    os.chmod(report, 0o600)
    result = json.loads(report.read_text())
    tests = result.get('numTotalTests')
    skipped = result.get('numPendingTests', 0) + result.get('numTodoTests', 0)
    if (type(tests) is not int or tests < 1 or result.get('success') is not True
            or result.get('numPassedTests') != tests or skipped):
        refuse('the retained focused corpus did not fully execute and pass')
    if identity() != source:
        refuse('published source changed during focused verification')
    print('PAXEER_X_GATE tests=' + str(tests) + ' skipped=0')


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError) as error:
        refuse('required actual compiler, fixture or protected evidence unavailable: ' + type(error).__name__)
