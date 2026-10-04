#!/usr/bin/env python3
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

ROOT = Path(__file__).resolve().parents[3]


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def run_case(command, log, deadline):
    remaining = deadline - time.monotonic()
    require(remaining > 0, 'shared actual-process gate deadline exhausted')
    with log.open('xb') as output:
        process = subprocess.Popen(command, cwd=ROOT, stdin=subprocess.DEVNULL,
                                   stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
        try:
            code = process.wait(timeout=remaining)
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait(timeout=5)
    require(log.stat().st_size <= 64 * 1024 * 1024, 'actual-process output exceeds gate bound')
    output = log.read_text()
    print(output, end='', flush=True)
    require(code == 0, 'actual-process case exited %d; log=%s' % (code, log))
    summaries = re.findall(r'^PAXEER_X_GATE tests=(\d+) skipped=(\d+)(?: actual_cases=(\d+))?$',
                           output, re.M)
    require(len(summaries) == 1, 'one actual-process count required; log=' + str(log))
    tests, skipped, cases = summaries[0]
    require(int(tests) > 0 and skipped == '0', 'nonzero executed and zero skipped tests required')
    return output, int(tests), int(cases) if cases else None


def main():
    os.umask(0o077)
    tests = 0
    completed = 0
    try:
        require(len(sys.argv) == 1, 'this gate accepts no alternate corpus or test filter')
        require(os.geteuid() == 0, 'actual process and peer identity corpus requires root')
        for variable in ('PAXEER_X_CORE_BUILD_MANIFEST', 'PAXEER_X_FOUNDATION_ARTIFACT_MANIFEST',
                         'LAYERX_CUSTODY_ARTIFACT_MANIFEST', 'LAYERX_RESET_NATIVE_ARTIFACT_MANIFEST'):
            location = os.environ.get(variable)
            require(location, variable + ' is required')
            path = Path(location)
            info = path.lstat()
            require(path.is_absolute() and not any(p.is_symlink() for p in (path, *path.parents))
                    and stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                    and info.st_nlink == 1 and stat.S_IMODE(info.st_mode) == 0o600,
                    variable + ' must name an owned protected artifact manifest')
        evidence = Path(tempfile.mkdtemp(prefix='layerx-reset-activation-', dir='/tmp'))
        evidence.chmod(0o700)
        print('actual reset activation evidence: ' + str(evidence), flush=True)
        deadline = time.monotonic() + 840
        reset, reset_tests, reset_cases = run_case(
            [sys.executable, str(ROOT / 'platform/hosted/node/tests/reset_recovery.py'), '--staged'],
            evidence / 'supervisor.log', deadline)
        require(reset_cases == 18, 'all legacy and staged activation cases must execute')
        documents = [json.loads(line) for line in reset.splitlines()
                     if line.startswith('{') and 'actual_cases_completed' in line]
        require(len(documents) == 1, 'exact reset corpus execution summary required')
        summary = documents[0]
        require(summary['actual_process_tests_planned'] == 2 and summary['durable_store_tests_planned'] > 0
                and summary['tests_run'] == summary['durable_store_tests_planned'] + 2
                and summary['failures'] == summary['errors'] == summary['skipped'] == 0,
                'complete actual native process and durable store coverage required')
        tests += reset_tests
        completed += 1
        _, funded_tests, funded_cases = run_case(
            [sys.executable, str(ROOT / 'platform/hosted/core/tests/receipt_retention.py')],
            evidence / 'funded-receipts.log', deadline)
        require(funded_tests == 2 and funded_cases is None, 'both genuine funded SEND cases must execute')
        tests += funded_tests
        completed += 1
        return 0
    except (AssertionError, ImportError, OSError, ValueError, KeyError, RuntimeError,
            subprocess.SubprocessError) as error:
        print('supervisor-reset-recovery: refusal: ' + str(error), file=sys.stderr, flush=True)
        return 1
    finally:
        print('PAXEER_X_GATE tests=%d skipped=0 corpora=%d' % (tests, completed), flush=True)


if __name__ == '__main__':
    signal.signal(signal.SIGTERM, lambda signum, frame: sys.exit(128 + signum))
    sys.exit(main())
