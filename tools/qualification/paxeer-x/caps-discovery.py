#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'tools/qualification/paxeer-x/fixtures'))
import caps_fixture

RUST_CASES = {'native_complete_evidence_and_mutation_refusals', 'native_live_paginated_discovery',
              'native_client_selection_and_terminal_refusals'}
RUNTIME_CASES = {'real-empty-module', 'real-empty-prefix', 'real-mixed-owner-budgets',
    'real-mixed-owner-grants', 'mutation-during-traversal', 'fresh-after-mutation',
    'revocation-during-traversal', 'period-rollover-during-traversal', 'foreign-object',
    'wrong-root', 'wrong-selection', 'cursor-gap', 'malformed-cursor', 'wrong-network',
    'zero-page', 'oversize-page', 'wrong-rank', 'malformed-request', 'cursor-replay',
    'disconnect', 'restart', 'fresh-after-restart', 'real-finality-authority', 'expiry',
    'active-object-exhaustion', 'snapshot-memory-exhaustion'}

RUST_MARKERS = frozenset(['account-count-padding', 'account-id-substitution', 'account-omission', 'account-order', 'account-value-substitution', 'client-preflight-expiry-and-terminal', 'composite-root-substitution', 'crosschain-owner-kind', 'custody-source-authority-substitution', 'domain-authority-term-rank-refusals', 'duplicate-account-proof', 'duplicate-prefix-leaf', 'empty-prefix-and-module', 'empty-prefix-or-module-root', 'finality-context-substitution', 'finality-selector-substitution', 'first-middle-last-prefix-omission', 'foreign-malformation-before-filter', 'foreign-owner-account-substitution', 'forged-empty-module', 'forged-empty-prefix', 'fresh-after-mutation', 'fresh-after-restart', 'fresh-after-rollover', 'freshness-selector-and-expected-root-refusals', 'invalid-owner-prefix', 'live-paginated-complete-and-terminal', 'live-paginated-confirmed-empty', 'lower-boundary-substitution', 'missing-custody-v2-source', 'noncanonical-budget-or-grant', 'noncanonical-grant-boolean', 'noncanonical-path-padding', 'odd-node-sibling-substitution', 'owned-omission-among-foreign', 'owner-and-foreign-classification', 'owner-name-alias', 'populated-foreign', 'populated-owner', 'range-order', 'real-finality', 'relay-changed-total', 'relay-empty-cursor', 'relay-no-progress', 'relay-oversize-object', 'relay-oversize-page', 'relay-premature-done', 'relay-repeated-cursor', 'relay-truncated-chunk', 'relay-unexpected-proof', 'relay-wrong-correlation', 'relay-wrong-network', 'relay-wrong-root', 'relay-wrong-snapshot', 'relay-zero-chunk', 'retained-and-fresh-root-separation', 'retained-before-mutation', 'retained-before-revocation', 'retained-before-rollover', 'signed-header-authority-substitution', 'signed-header-signature-substitution', 'signed-header-term-substitution', 'universal-leaf-omission', 'upper-boundary-substitution', 'wrong-leaf-count-padding', 'wrong-leaf-index'])
RUST_ADDITIONAL_MARKERS = frozenset(['lower-boundary-omission', 'upper-boundary-omission'])


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def manifest(output):
    path = output / 'manifest.json'
    caps_fixture.fixture.private(path)
    value = json.loads(path.read_text())
    require(value.get('version') == 1 and value.get('purpose') == 'native-complete-caps-discovery', 'artifact contract')
    require(set(value.get('artifacts', {})) == {'native', 'rust', 'layerxd'}, 'artifact set')
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    tree = subprocess.check_output(['git', 'rev-parse', 'HEAD^{tree}'], cwd=ROOT, text=True).strip()
    require((value['revision'], value['source_tree']) == (revision, tree), 'stale source-bound artifacts')
    require(not subprocess.check_output(['git', 'status', '--porcelain'], cwd=ROOT), 'source changed since build')
    for item in value['artifacts'].values():
        executable = Path(item['path'])
        require(executable.is_absolute() and executable.is_file() and not executable.is_symlink()
                and os.access(executable, os.X_OK), 'missing prebuilt executable')
        with executable.open('rb') as stream:
            require(hashlib.file_digest(stream, 'sha256').hexdigest() == item['sha256'], 'artifact digest mismatch')
    caps_fixture.fixture.artifacts(os.environ['PAXEER_X_RUNTIME_ARTIFACTS'])
    caps_fixture.finality.supplemental(os.environ['PAXEER_X_FINALITY_ARTIFACTS'])
    return value


def worker(output, value, directory):
    require(os.geteuid() == 0 and os.getegid() == 4020, 'owned fixture UID0/GID4020 required')
    for name in ('net', 'pid', 'mnt'):
        require(os.readlink('/proc/self/ns/' + name) != os.environ['CAPS_PARENT_' + name.upper()],
                'isolated namespace missing: ' + name)
    subprocess.run(['ip', 'link', 'set', 'lo', 'up'], check=True)
    subprocess.run(['mount', '-t', 'tmpfs', '-o', 'mode=0755,nosuid,nodev', 'caps-discovery-runtime', '/tmp'], check=True)
    source = Path('/tmp/caps-discovery-source'); source.mkdir(mode=0o755)
    subprocess.run(['mount', '--bind', str(ROOT), str(source)], check=True)
    subprocess.run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', str(source)], check=True)
    caps_fixture.fixture.ROOT = source
    python_root = Path('/tmp/caps-discovery-python'); python_root.mkdir(mode=0o755)
    subprocess.run(['mount', '--bind', sys.prefix, str(python_root)], check=True)
    subprocess.run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', str(python_root)], check=True)
    os.environ['PATH'] = str(python_root / 'bin') + ':/usr/local/bin:/usr/bin:/bin'
    result_path = caps_fixture.run(directory, value, value['artifacts']['native']['path'], value['artifacts']['rust']['path'])
    result = json.loads(result_path.read_text())
    cases = result['runtime_cases']
    require(len(cases) == len(RUNTIME_CASES) and set(cases) == RUNTIME_CASES, 'missing or duplicate real runtime cases')
    log = Path(result['log']).read_text()
    for name in RUST_CASES:
        require(re.search(r'(?m)^test ' + re.escape(name) + r' \.\.\. ', log), 'missing Rust case: ' + name)
    match = re.search(r'test result: ok\. (\d+) passed; 0 failed; 0 ignored; (\d+) measured; (\d+) filtered out;', log)
    require(match is not None and tuple(map(int, match.groups())) == (len(RUST_CASES), 0, 0), 'skipped or extra Rust cases')
    markers = set(re.findall(r'CAPS_CASE ([a-z0-9-]+)', log))
    require(RUST_MARKERS <= markers and markers <= RUST_MARKERS | RUST_ADDITIONAL_MARKERS,
            'missing or unexpected Rust case markers: missing=' + repr(sorted(RUST_MARKERS - markers)) + ' unexpected=' + repr(sorted(markers - RUST_MARKERS - RUST_ADDITIONAL_MARKERS)))
    record = {'revision': value['revision'], 'source_tree': value['source_tree'],
              'command': 'timeout 15m python3 tools/qualification/paxeer-x/caps-discovery.py',
              'exit_code': 0, 'log': str(output / 'runtime-worker.log'), 'evidence': str(result_path),
              'runtime_cases': cases, 'rust_test_groups': sorted(RUST_CASES), 'rust_cases': sorted(markers),
              'case_counts': {'native': len(cases), 'rust': len(markers)}, 'skipped': 0}
    target = output / 'gate-result.json'; target.write_text(json.dumps(record, indent=2) + '\n'); target.chmod(0o600)
    print(json.dumps(record))


def main():
    os.umask(0o077)
    output = Path(os.environ['CAPS_DISCOVERY_BUILD_DIR']).resolve()
    value = manifest(output)
    if len(sys.argv) == 3 and sys.argv[1] == '--worker':
        worker(output, value, Path(sys.argv[2])); return
    require(len(sys.argv) == 1, 'unexpected gate arguments')
    directory = Path(tempfile.mkdtemp(prefix='px-caps-discovery-', dir='/var/tmp')); directory.rmdir()
    (output / 'runtime-directory').write_text(str(directory) + '\n')
    env = dict(os.environ)
    for name in ('net', 'pid', 'mnt'):
        env['CAPS_PARENT_' + name.upper()] = os.readlink('/proc/self/ns/' + name)
    with (output / 'runtime-worker.log').open('wb') as log:
        result = subprocess.run(['unshare', '--mount', '--net', '--pid', '--fork', '--kill-child=KILL',
            '--mount-proc', '--propagation', 'private', sys.executable, str(Path(__file__).resolve()),
            '--worker', str(directory)], env=env, stdout=log, stderr=log, group=4020, extra_groups=[], timeout=840)
    print((output / 'runtime-worker.log').read_text(), end='')
    require(result.returncode == 0, 'isolated real caps gate exit ' + str(result.returncode))
    require((output / 'gate-result.json').is_file(), 'missing complete evidence record')


if __name__ == '__main__':
    try:
        main()
    except (OSError, KeyError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print('caps-discovery: ' + str(error), file=sys.stderr)
        sys.exit(1)
