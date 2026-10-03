#!/usr/bin/env bash
set -euo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
exec python3 - <<'PYGATE'
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys
import time


def require(condition, message):
    if not condition:
        raise ValueError(message)


def read_json(path):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, 'duplicate result field')
            result[key] = value
        return result
    return json.loads(path.read_text(), object_pairs_hook=unique)


try:
    os.umask(0o077)
    root = Path.cwd().resolve()
    frontend = root / 'explorer/frontend'
    raw = os.environ.get('PAXEER_X_EVIDENCE_DIR')
    require(bool(raw), 'PAXEER_X_EVIDENCE_DIR is required')
    evidence = Path(raw)
    require(evidence.is_absolute() and evidence.resolve() == evidence
            and not evidence.is_relative_to(root), 'evidence must be outside the repository without symlinks')
    info = evidence.stat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid() and info.st_mode & 0o077 == 0,
            'evidence must be a private directory owned by the caller')
    integrity = frontend / 'node_modules/.yarn-integrity'
    require(integrity.is_file() and not integrity.is_symlink(), 'complete frozen-lock frontend install required; this gate never installs')
    require(isinstance(read_json(integrity), dict), 'frontend install integrity is malformed')
    require(all((frontend / 'node_modules/.bin' / name).is_file() for name in ('tsc', 'vitest')),
            'installed typecheck and test executables required')
    yarn = shutil.which('yarn')
    require(yarn is not None, 'installed Yarn required')
    report = evidence / 'vitest.json'
    require(not report.exists() and not report.is_symlink(), 'fresh Vitest result path required')
    logs = [evidence / name for name in ('typecheck.log', 'vitest.log')]
    require(all(not path.exists() and not path.is_symlink() for path in logs), 'fresh gate log paths required')
    env = dict(os.environ, CI='true', COREPACK_ENABLE_NETWORK='0', YARN_ENABLE_NETWORK='0')
    deadline = time.monotonic() + 1780
    commands = [
        [yarn, 'lint:tsc'],
        [yarn, 'test:vitest', 'run', 'ui/shared/scan', 'ui/shared/statusTag', 'toolkit/theme',
         '--reporter=json', '--outputFile=' + str(report)],
    ]
    for command, log in zip(commands, logs):
        with log.open('x') as output:
            completed = subprocess.run(command, cwd=frontend, env=env, stdin=subprocess.DEVNULL,
                                       stdout=output, stderr=subprocess.STDOUT,
                                       timeout=max(1, deadline - time.monotonic()))
        require(completed.returncode == 0, log.name + ' command failed: exit ' + str(completed.returncode))
    require(report.is_file() and not report.is_symlink(), 'Vitest did not emit its actual report')
    data = read_json(report)
    names = ('numTotalTests', 'numPassedTests', 'numFailedTests', 'numPendingTests', 'numTodoTests')
    require(all(type(data.get(name)) is int and data[name] >= 0 for name in names), 'Vitest counters missing or malformed')
    total = data['numTotalTests']
    require(data.get('success') is True and total > 0 and data['numPassedTests'] == total
            and data['numFailedTests'] == data['numPendingTests'] == data['numTodoTests'] == 0,
            'Vitest failed, skipped or did not execute tests')
    suites = data.get('testResults')
    require(isinstance(suites, list) and suites, 'Vitest suite results absent')
    required = {
        'toolkit/theme/recipes/recipes.spec.ts',
        'ui/shared/scan/ScanDirectionBadge.spec.tsx',
        'ui/shared/scan/ScanSectionTabs.spec.tsx',
        'ui/shared/scan/ScanMethodChip.spec.tsx',
        'ui/shared/statusTag/StatusTag.spec.tsx',
    }
    seen = set()
    executed = 0
    for suite in suites:
        require(suite.get('status') == 'passed', 'Vitest suite did not pass')
        name = Path(suite['name']).resolve()
        require(name.is_relative_to(frontend), 'foreign Vitest suite')
        relative = str(name.relative_to(frontend))
        require(relative not in seen, 'duplicate Vitest suite')
        seen.add(relative)
        assertions = suite.get('assertionResults')
        require(isinstance(assertions, list) and assertions, 'empty Vitest suite')
        require(all(row.get('status') == 'passed' for row in assertions), 'nonpassing Vitest assertion')
        executed += len(assertions)
        if relative in required and relative.endswith('.tsx'):
            for appearance in ('light', 'dark'):
                require(any(appearance + ' appearance' in ' '.join(row.get('ancestorTitles', []))
                            for row in assertions), 'required appearance did not execute')
    require(required <= seen and executed == total, 'required task corpus missing or counter mismatch')
    print('PAXEER_X_GATE tests=' + str(total) + ' skipped=0')
except (ValueError, OSError, KeyError, TypeError, subprocess.SubprocessError) as error:
    print('110.8.2 refused: ' + str(error), file=sys.stderr)
    raise SystemExit(1)
PYGATE
