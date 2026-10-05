#!/usr/bin/env bash
set -euo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
export PATH="$HOME/.cargo/bin:/usr/local/go/bin:$PATH"
exec python3 - <<'PY'
import os
import re
import shutil
import subprocess
import sys

TESTS = 0


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def run(label, argv, cwd='.', env=None, timeout=1500):
    require(shutil.which(argv[0]), label + ' toolchain ' + argv[0] + ' is not installed')
    print('== ' + label + ': ' + ' '.join(argv), flush=True)
    result = subprocess.run(argv, cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                            capture_output=True, text=True, timeout=timeout)
    sys.stdout.write(result.stdout)
    sys.stderr.write(result.stderr)
    require(result.returncode == 0, label + ' failed with exit ' + str(result.returncode))
    return result.stdout + result.stderr


def credit(label, count):
    global TESTS
    require(count > 0, label + ' ran no tests')
    TESTS += count


try:
    run('Programs refusal fixture derivation',
        ['python3', 'platform/sdk/conformance/fixtures/generate_program_receipt_refusals.py', '--check'])
    credit('fixture derivation', 1)

    run('seven-language receipt contract drift',
        ['cargo', 'run', '--offline', '--locked', '--manifest-path', 'platform/Cargo.toml',
         '-p', 'layerx-platform-sdkgen', '--', '--check-program-contracts', os.getcwd()])
    credit('contract drift', 1)
    out = run('receipt generator model',
              ['cargo', 'test', '--offline', '--locked', '--manifest-path', 'platform/Cargo.toml',
               '-p', 'layerx-platform-sdkgen', '--test', 'drift'])
    summary = re.findall(r'test result: ok\. (\d+) passed; 0 failed; 0 ignored', out)
    require(summary, 'generator drift summary missing')
    credit('generator drift', sum(map(int, summary)))

    out = run('Rust SDK receipts',
              ['cargo', 'test', '--offline', '--locked', '--manifest-path', 'agent/Cargo.toml',
               '-p', 'layerx-sdk', '--test', 'receipt_fixture'])
    summary = re.findall(r'test result: ok\. (\d+) passed; 0 failed; 0 ignored', out)
    require(len(summary) == 1, 'Rust receipt suite summary missing')
    credit('Rust', int(summary[0]))

    env = dict(os.environ, GOPROXY='off')
    out = run('Go SDK receipts',
              ['go', 'test', '-count=1', '-v', '-run', 'Receipt|ExplicitProtocolThree', '.'],
              cwd='platform/sdk/go', env=env)
    require('--- SKIP' not in out, 'Go receipt test skipped')
    credit('Go', len(re.findall(r'^--- PASS: \S+', out, re.M)))

    tsc = os.path.join('node_modules', '.bin', 'tsc')
    require(os.access(tsc, os.X_OK), 'TypeScript toolchain is not installed (run make platform-js-install)')
    run('TypeScript SDK build', [tsc, '-p', 'agent/sdk/typescript/tsconfig.json'])
    run('TypeScript SDK receipts', ['node', 'agent/sdk/typescript/dist/test/receipt-fixture.test.js'])
    credit('TypeScript', 1)

    env = dict(os.environ, PYTHONPATH='agent/sdk/python', PYTHONDONTWRITEBYTECODE='1')
    out = run('Python SDK receipts',
              ['python3', '-m', 'unittest', '-v', 'tests/agent/sdk/python/test_receipt_fixture.py'], env=env)
    ran = re.findall(r'^Ran (\d+) tests? in', out, re.M)
    require(len(ran) == 1 and re.search(r'^OK$', out, re.M) and 'skipped' not in out,
            'Python receipt suite incomplete')
    credit('Python', int(ran[0]))

    out = run('JVM SDK receipts',
              ['mvn', '-o', '-B', '-f', 'platform/sdk/jvm/pom.xml', '-Dtest=ReceiptFixtureTest',
               '-Dsurefire.failIfNoSpecifiedTests=true', 'test'])
    summary = re.findall(r'Tests run: (\d+), Failures: 0, Errors: 0, Skipped: 0$', out, re.M)
    require(summary, 'JVM receipt suite summary missing')
    credit('JVM', int(summary[-1]))

    out = run('Swift SDK receipts',
              ['swift', 'test', '--disable-automatic-resolution', '--package-path', 'platform/sdk/swift',
               '--filter', 'ReceiptFixtureTests'])
    passed = re.findall(r"Test Case '.*ReceiptFixtureTests.*' passed", out)
    require('skipped' not in out.lower(), 'Swift receipt test skipped')
    credit('Swift', len(passed))

    out = run('.NET SDK receipts',
              ['dotnet', 'test', 'platform/sdk/dotnet/tests/LayerX.Sdk.Tests/LayerX.Sdk.Tests.csproj',
               '--no-restore', '--nologo', '--filter', 'FullyQualifiedName~ReceiptFixtureTests'])
    summary = re.findall(r'Passed!\s+-\s+Failed:\s+0, Passed:\s+(\d+), Skipped:\s+0', out)
    require(len(summary) == 1, '.NET receipt suite summary missing')
    credit('.NET', int(summary[0]))

    print('PAXEER_X_GATE tests=' + str(TESTS) + ' skipped=0')
except (OSError, RuntimeError, subprocess.SubprocessError) as error:
    print('SDK receipt parity qualification refused: ' + str(error), file=sys.stderr)
    sys.exit(78)
PY
