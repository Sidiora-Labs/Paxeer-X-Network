#!/usr/bin/env python3
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile


def main():
    web = Path(__file__).resolve().parents[4]
    parser = argparse.ArgumentParser()
    parser.add_argument('--output-dir', type=Path, required=True)
    args = parser.parse_args()
    args.output_dir.mkdir(parents=True, exist_ok=True)
    output = Path(tempfile.mkdtemp(prefix='l25-phase2-', dir=args.output_dir))
    report = {'task': '2.25', 'build_exit_code': None, 'verify_exit_code': None, 'qualified': False, 'started_at': datetime.now(timezone.utc).isoformat()}
    code = 1
    try:
        supplied = os.environ.get('L25_QUALIFICATION_CONFIG_FILE')
        expected = os.environ.get('L25_QUALIFICATION_CONFIG_SHA256')
        if not supplied or not expected:
            raise RuntimeError('Actual admitted network activation, committed head and recipient-specific deposit evidence are required')
        if hashlib.sha256(Path(supplied).read_bytes()).hexdigest() != expected:
            raise RuntimeError('Qualification authority bytes changed')
        authority = json.loads(Path(supplied).read_bytes())
        source = os.environ.get('L25_ADMITTED_SOURCE_SHA256')
        if not source or authority.get('sourceSha256') != source:
            raise RuntimeError('Qualification evidence must bind the actual admitted current source')
        command = ['node', 'node_modules/vitest/vitest.mjs', 'run', '--config', 'src/core/wallet-sdk/vitest.l25.config.ts', 'src/core/wallet-sdk/runtime.integration.test.tsx', '--reporter=default', '--reporter=json', '--outputFile=' + str(output / 'native-tests.json')]
        env = dict(os.environ)
        env['L25_VITEST_CACHE_DIR'] = str(output / 'vite-cache')
        log = output / 'native-tests.log'
        with log.open('wb') as stream:
            result = subprocess.run(command, cwd=web, env=env, stdout=stream, stderr=subprocess.STDOUT, timeout=180, check=False)
        report.update(command=command, native_test_exit_code=result.returncode, log=str(log), authority_sha256=expected)
        if result.returncode:
            raise RuntimeError('Native runtime qualification failed')
        native = json.loads((output / 'native-tests.json').read_bytes())
        if not native.get('success') or native.get('numTotalTests') != 8 or native.get('numPassedTests') != 8 or native.get('numFailedTests') or native.get('numPendingTests'):
            raise RuntimeError('The complete native runtime suite must pass without skips')
        report.update(native_tests_passed=8, native_runtime_checks_passed=True, proof_scope='native_runtime_only',
                      qualified=False, remaining_release_evidence=['real supported-device injected connector signing/reconnection', 'configured WalletConnect proof when enabled', 'fiat vendor chain125 settlement'])
        code = 0
    except Exception as error:
        report['error'] = str(error)
    report['exit_code'] = code
    report['finished_at'] = datetime.now(timezone.utc).isoformat()
    (output / 'result.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({'exit_code': code, 'report': str(output / 'result.json')}))
    return code


if __name__ == '__main__':
    sys.exit(main())
