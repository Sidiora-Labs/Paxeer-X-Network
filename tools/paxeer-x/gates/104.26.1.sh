#!/usr/bin/env bash
set -euo pipefail
if (( $# != 0 )); then
    exit 2
fi
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)
exec python3 - "$root" <<'PY'
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time

sys.dont_write_bytecode = True
ROOT = Path(sys.argv[1])
sys.path.insert(0, str(ROOT / 'tools/paxeer-x'))
from candidate import Invalid, load_private, protected_path, write_private

CASES = {
    'on_ramp_receipt', 'off_ramp_receipt', 'external_custody_labels',
    'principal_isolation', 'operator_authorization', 'payer_grant_required',
    'customer_authority_rejected', 'out_of_order_refused', 'order_idempotency',
    'rebalance_own_account_finality', 'rebalance_idempotency', 'recovery_same_identifiers',
}
TARGETS = {
    ('layerx-ramp-toolkit', 'layerx_ramp_toolkit'),
    ('layerx-ramp-toolkit', 'contracts'),
    ('layerx-ramp-toolkit', 'paxeer_reorg'),
    ('layerx-ramp-toolkit', 'ramp-verify-receipt'),
    ('layerx-reference-ramp', 'layerx-reference-ramp'),
    ('layerx-reference-ramp', 'plain_listener'),
}
SELECTED = ['platform/ramps', 'platform/Cargo.toml', 'platform/Cargo.lock',
            '.github/workflows/ramp-sandbox.yml', 'tools/paxeer-x/gates/104.26.1.sh', 'Makefile']
DEADLINE = time.monotonic() + 1740


def require(condition, message):
    if not condition:
        raise Invalid(message)


def git(*args):
    return subprocess.check_output(['git', '-C', str(ROOT), *args], text=True, timeout=30).strip()


def digest(path):
    h = hashlib.sha256()
    with open(path, 'rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()


def env_path(name):
    value = os.environ.get(name)
    require(value, name + ' is required')
    path = protected_path(protected_path(value).resolve())
    require(ROOT not in path.parents and path != ROOT, 'private material must be outside source')
    return path


def binary(row):
    path = protected_path(protected_path(row['path']).resolve())
    require(path.is_file() and os.access(path, os.X_OK) and digest(path) == row['sha256'],
            'missing or mismatched actual prebuilt executable')
    return path


def execute(command, log, env=None):
    remaining = DEADLINE - time.monotonic()
    require(remaining > 0, 'qualification deadline')
    fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        completed = subprocess.run(command, cwd=ROOT, env=env, stdin=subprocess.DEVNULL,
                                   stdout=stream, stderr=subprocess.STDOUT, timeout=remaining, check=False)
    require(completed.returncode == 0, 'real command failed; exact output retained privately')
    return Path(log).read_text()


def producer(phase):
    destination = Path(os.environ['PAXEER_X_RAMP_BUILD_MANIFEST'])
    require(destination.is_absolute() and ROOT not in destination.parents and not destination.exists(),
            'fresh private build manifest outside source required')
    require(destination.parent.is_dir() and not destination.parent.stat().st_mode & 0o077,
            'private build manifest directory required')
    directory = env_path('PAXEER_X_EVIDENCE_DIR')
    before = directory / 'ramp-build-inputs.json'
    if phase == 'prepare':
        require(not git('status', '--porcelain=v1', '--untracked-files=normal'), 'source must be clean')
        tracked = subprocess.check_output(['git', '-C', str(ROOT), 'ls-files', '-z', '--', *SELECTED]).split(b'\0')
        hashes = {os.fsdecode(path): digest(ROOT / os.fsdecode(path)) for path in tracked if path}
        value = {'schema': 'ramp-prebuilt-v1', 'revision': git('rev-parse', 'HEAD'),
                 'tree': git('rev-parse', 'HEAD^{tree}'), 'sourcehashmap': hashes,
                 'source_digest': hashlib.sha256(json.dumps(hashes, sort_keys=True, separators=(',', ':')).encode()).hexdigest(),
                 'started_ns': time.time_ns()}
        write_private(before, value)
        print(value['revision'] + ' ' + value['source_digest'])
        return
    require(phase == 'record', 'unknown producer phase')
    value = load_private(before)
    require(value['revision'] == git('rev-parse', 'HEAD') and value['tree'] == git('rev-parse', 'HEAD^{tree}')
            and not git('status', '--porcelain=v1', '--untracked-files=normal'), 'build source changed')
    require(all(digest(ROOT / path) == expected for path, expected in value['sourcehashmap'].items()), 'build inputs changed')
    cargo = directory / 'ramp-cargo.jsonl'
    require(cargo.stat().st_mtime_ns >= value['started_ns'], 'build output predates snapshot')
    tests, production, finished = [], {}, []
    for line in cargo.read_text().splitlines():
        event = json.loads(line)
        if event.get('reason') == 'build-finished': finished.append(event.get('success'))
        if event.get('reason') != 'compiler-artifact' or not event.get('executable'): continue
        target = event['target']['name']
        package = 'layerx-ramp-toolkit' if str(event['manifest_path']).endswith('/ramps/toolkit/Cargo.toml') else 'layerx-reference-ramp'
        require(Path(event['manifest_path']).resolve() in (ROOT / 'platform/ramps/toolkit/Cargo.toml',
                ROOT / 'platform/ramps/reference/Cargo.toml'), 'artifact from unexpected package')
        exe = Path(event['executable']).resolve()
        owned = directory / ('test-' + target if event['profile']['test'] else target)
        import shutil
        require(not owned.exists() and not owned.is_symlink(), 'fresh artifact path required')
        shutil.copyfile(exe, owned)
        owned.chmod(0o700)
        row = {'path': str(owned), 'sha256': digest(owned), 'package': package, 'target': target}
        if event['profile']['test']:
            listing = execute([str(owned), '--list', '--format', 'terse'], directory / ('inventory-' + target + '.log'))
            row['expected_test_names'] = re.findall(r'^(.+): test$', listing, re.M)
            tests.append(row)
        else: production[target] = row
    require(finished == [True] and {(row['package'], row['target']) for row in tests} == TARGETS
            and len(tests) == len(TARGETS), 'complete successful build corpus required')
    require(set(production) == {'layerx-reference-ramp', 'ramp-verify-receipt'}, 'both production binaries required')
    value.update(exit_code=0, testbinaries=tests, referencebinary=production['layerx-reference-ramp'],
                 receiptverifierbinary=production['ramp-verify-receipt'],
                 doctests={'count': 0, 'sourcehashmap': {path: sha for path, sha in value['sourcehashmap'].items() if path.endswith('.rs')}})
    write_private(destination, value)


try:
    if os.environ.get('PAXEER_X_RAMP_PRODUCER_PHASE'):
        producer(os.environ['PAXEER_X_RAMP_PRODUCER_PHASE'])
        sys.exit(0)
    require(os.environ.get('LAYERX_DEPLOYMENT_PROFILE') == 'private-network',
            'only the explicitly provisioned private testnet profile is admitted')
    evidence = env_path('PAXEER_X_EVIDENCE_DIR')
    info = evidence.stat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid() and not info.st_mode & 0o077,
            'evidence directory must be owned and private')
    manifest = load_private(env_path('PAXEER_X_RAMP_BUILD_MANIFEST'))
    revision, tree = git('rev-parse', 'HEAD'), git('rev-parse', 'HEAD^{tree}')
    require(not git('status', '--porcelain=v1', '--untracked-files=normal'), 'source must be clean')
    require(manifest['schema'] == 'ramp-prebuilt-v1' and manifest['exit_code'] == 0
            and manifest['revision'] == revision and manifest['tree'] == tree,
            'actual successful build source identity required')
    tracked = subprocess.check_output(['git', '-C', str(ROOT), 'ls-files', '-z', '--', *SELECTED]).split(b'\0')
    hashes = {os.fsdecode(path): digest(ROOT / os.fsdecode(path)) for path in tracked if path}
    require(hashes and hashes == manifest['sourcehashmap'], 'complete source hash inventory mismatch')
    source_digest = hashlib.sha256(json.dumps(hashes, sort_keys=True, separators=(',', ':')).encode()).hexdigest()
    require(source_digest == manifest['source_digest'], 'compile-time source digest mismatch')
    docs = {path: value for path, value in hashes.items() if path.endswith('.rs')}
    require(manifest['doctests'] == {'count': 0, 'sourcehashmap': docs},
            'source-bound zero-doctest inventory required')
    for path in docs:
        text = (ROOT / path).read_text()
        require(not any(marker in text for marker in (chr(96) * 3, '#[doc', '/*!', '/**'))
                and not re.search(r'^\s*//[/!](?: {4}|\t)', text, re.M),
                'new documentation examples require a genuine compiled doc corpus')
    reference = binary(manifest['referencebinary'])
    verifier = binary(manifest['receiptverifierbinary'])
    targets = manifest['testbinaries']
    require(len(targets) == len(TARGETS) and {(row['package'], row['target']) for row in targets} == TARGETS,
            'all retained toolkit and reference test targets required')
    total = 0
    inventories = []
    for index, row in enumerate(targets):
        exe = binary(row)
        names = row['expected_test_names']
        require(isinstance(names, list) and len(names) == len(set(names))
                and all(isinstance(name, str) and name for name in names), 'invalid genuine test inventory')
        listing = execute([str(exe), '--list', '--format', 'terse'], evidence / f'ramp-tests-{index}-list.log')
        actual = re.findall(r'^(.+): test$', listing, re.M)
        require(sorted(actual) == sorted(names), 'prebuilt test inventory changed')
        output = execute([str(exe), '--test-threads=1'], evidence / f'ramp-tests-{index}.log')
        summaries = re.findall(r'test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored; (\d+) measured; (\d+) filtered out;', output)
        require(len(summaries) == 1, 'missing exact Rust test result')
        passed, failed, ignored, measured, filtered = map(int, summaries[0])
        require(passed == len(names) and failed == ignored == measured == filtered == 0,
                'retained test corpus failed or omitted a case')
        total += passed
        inventories.append({'package': row['package'], 'target': row['target'], 'passed': passed})
    require(total > 0, 'empty retained test corpus')
    require(digest(reference) == manifest['referencebinary']['sha256'], 'reference artifact changed')
    env = dict(os.environ, LAYERX_RAMP_EXPECTED_REVISION=revision,
               LAYERX_RAMP_EXPECTED_SOURCE_DIGEST=source_digest,
               LAYERX_RAMP_RECEIPT_VERIFIER_BINARY=str(verifier),
               PAXEER_X_RAMP_FULL_CONTRACT='1')
    require(digest(verifier) == manifest['receiptverifierbinary']['sha256'], 'verifier artifact changed')
    runtime_output = evidence / 'ramp-sandbox-result.json'
    require(not runtime_output.exists(), 'runtime result must be fresh')
    execute(['sh', str(ROOT / 'platform/ramps/sandbox-journey.sh')],
            evidence / 'ramp-sandbox-runtime.log', env)
    report = load_private(runtime_output)
    require(report['schema_version'] == 1 and report['source_revision'] == revision
            and report['deployment_profile'] == 'private-network' and report['qualified'] is True
            and report['runtime_source_bound'] is True, 'genuine candidate-bound testnet journey required')
    require(report['on'].get('maintained_receipt') is True and report['off'].get('maintained_receipt') is True,
            'both real maintained receipt paths must execute')
    require(set(report['cases']) == CASES and all(value is True for value in report['cases'].values()),
            'every real ramp journey/refusal/recovery/rebalance case must pass')
    require(git('rev-parse', 'HEAD') == revision and git('rev-parse', 'HEAD^{tree}') == tree
            and not git('status', '--porcelain=v1', '--untracked-files=normal'), 'source changed during qualification')
    write_private(evidence / 'ramp-qualification-summary.json', {
        'revision': revision, 'tree': tree, 'source_digest': source_digest,
        'retained_test_targets': inventories, 'runtime_cases': sorted(CASES),
        'tests': total + len(CASES), 'skipped': 0,
        'github': {name: os.environ.get(name) for name in
                   ('GITHUB_REPOSITORY', 'GITHUB_SHA', 'GITHUB_RUN_ID', 'GITHUB_RUN_ATTEMPT', 'GITHUB_JOB')},
    })
    print(f'PAXEER_X_GATE tests={total + len(CASES)} skipped=0')
except (Invalid, OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
    print('104.26.1 refused missing, unsafe, failed or incomplete real ramp qualification inputs; private logs retained',
          file=sys.stderr)
    sys.exit(1)
PY
