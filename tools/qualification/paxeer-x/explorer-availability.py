#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
SOURCE_PATHS = [
    'human/crates/layerx-explorer-index/src/main.rs',
    'human/crates/layerx-explorer-index/src/unified.rs',
    'human/crates/layerx-explorer-index/src/query.rs',
    'human/crates/layerx-explorer-index/tests/unified_availability.rs',
    'human/apps/web/src/explorer/model.ts',
    'human/apps/web/src/explorer/client.ts',
    'human/apps/web/src/app/explorer/accounts/[accountId]/page.tsx',
    'tools/qualification/paxeer-x/explorer-availability.py',
]


def digest(path):
    value = hashlib.sha256()
    with Path(path).open('rb') as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b''):
            value.update(chunk)
    return value.hexdigest()


def source_bound(artifact, manifest):
    if not artifact or not Path(artifact).is_file() or not os.access(artifact, os.X_OK):
        return False
    if not manifest or not Path(manifest).is_file():
        return False
    if Path(manifest).stat().st_mode & 0o077:
        return False
    binding = json.loads(Path(manifest).read_text())
    expected = {path: digest(ROOT / path) for path in SOURCE_PATHS}
    binary = Path(artifact).resolve().parent.parent / 'layerx-explorer-index'
    if not binary.is_file() or not os.access(binary, os.X_OK):
        return False
    return (binding.get('source_sha256') == expected
            and binding.get('artifact') == str(Path(artifact).resolve())
            and binding.get('artifact_sha256') == digest(artifact)
            and binding.get('binary') == str(binary)
            and binding.get('binary_sha256') == digest(binary))


def main():
    evidence = Path(os.environ['PAXEER_X_EVIDENCE_DIR'])
    if not evidence.is_dir() or evidence.stat().st_mode & 0o077:
        raise RuntimeError('availability gate requires owner-only evidence directory')
    outcomes = []
    browser_log = evidence / 'explorer-availability-browser.log'
    with browser_log.open('wb') as output:
        result = subprocess.run(['npm', '--prefix', str(ROOT / 'human/apps/web'), 'run', 'typecheck'],
                                cwd=ROOT, stdout=output, stderr=subprocess.STDOUT, timeout=240)
    browser_log.chmod(0o600)
    outcomes.append({'gate': 'actual_browser_typecheck', 'exit_code': result.returncode, 'log_path': str(browser_log)})
    artifact = os.environ.get('PAXEER_X_UNIFIED_AVAILABILITY_TEST')
    if not source_bound(artifact, os.environ.get('PAXEER_X_UNIFIED_AVAILABILITY_MANIFEST')):
        outcomes.append({'gate': 'real_unified_availability', 'exit_code': 3,
                         'reason': 'missing source-bound prebuilt unified_availability executable'})
    else:
        test_log = evidence / 'explorer-availability-rust.log'
        with test_log.open('wb') as output:
            result = subprocess.run([artifact, '--nocapture'], cwd=ROOT, stdout=output,
                                    stderr=subprocess.STDOUT, timeout=300)
        test_log.chmod(0o600)
        outcomes.append({'gate': 'real_unified_availability', 'exit_code': result.returncode, 'log_path': str(test_log)})
    print(json.dumps({'outcomes': outcomes, 'skipped': 0}, sort_keys=True))
    return 0 if all(outcome['exit_code'] == 0 for outcome in outcomes) else 1


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (KeyError, OSError, RuntimeError, ValueError, subprocess.TimeoutExpired) as error:
        print(str(error), file=sys.stderr)
        sys.exit(3)
