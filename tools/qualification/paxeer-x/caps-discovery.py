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

NATIVE_CASES = (
    'native_complete_traversal', 'native_cursor_replay_refused', 'native_oversize_page_refused',
    'native_wrong_selection_refused', 'native_cursor_gap_refused', 'native_cross_root_cursor_refused',
    'native_no_progress_refused', 'native_malformed_cursor_refused', 'native_foreign_connection_cursor_refused',
    'native_active_snapshot_limit_refused', 'native_released_snapshot_refused',
    'native_mutation_during_traversal_kept_snapshot_root', 'native_fresh_snapshot_after_mutation')

RUST_CASES = (
    'populated_complete', 'mixed_owner_classification', 'prefix_empty_confirmed', 'empty_module_confirmed',
    'retained_snapshot_after_mutation', 'unsupported_peer_unavailable',
    'omitted_first_record', 'omitted_middle_record', 'omitted_last_record', 'duplicate_record', 'reordered_leaves',
    'omitted_owned_among_foreign', 'forged_lower_boundary', 'forged_upper_boundary', 'empty_vector_not_absence',
    'empty_module_root', 'wrong_root', 'count_manipulation', 'odd_padding_duplicate',
    'wrong_network', 'wrong_authority', 'wrong_term', 'wrong_rank',
    'malformed_budget_encoding', 'malformed_grant_encoding',
    'foreign_account_evidence', 'invalid_account_evidence', 'misleading_account_name_prefix',
    'cursor_expired', 'cursor_after_disconnect', 'cursor_after_restart', 'snapshot_memory_exhaustion',
    'revocation_during_traversal', 'period_rollover_during_traversal')


class MissingPrerequisite(RuntimeError):
    pass


def digest(path):
    with open(path, 'rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def manifest_path():
    explicit = os.environ.get('CAPS_DISCOVERY_MANIFEST')
    if explicit:
        return Path(explicit)
    target = os.environ.get('CARGO_TARGET_DIR')
    if not target:
        raise MissingPrerequisite('CARGO_TARGET_DIR or CAPS_DISCOVERY_MANIFEST required to locate prebuilt artifacts')
    return Path(target).resolve().parent / 'caps-discovery' / 'manifest.json'


def load_manifest():
    path = manifest_path()
    if not path.is_file():
        raise MissingPrerequisite('prebuilt caps-discovery manifest missing: run the build recipe first')
    if path.stat().st_mode & 0o077:
        raise RuntimeError('caps-discovery manifest is group/other accessible')
    manifest = json.loads(path.read_text())
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    if manifest.get('version') != 1 or manifest.get('revision') != revision:
        raise RuntimeError('prebuilt artifact revision mismatch')
    for tree, expected in manifest['source_trees'].items():
        actual = subprocess.check_output(['git', 'rev-parse', 'HEAD:' + tree], cwd=ROOT, text=True).strip()
        if actual != expected:
            raise RuntimeError('prebuilt source tree mismatch: ' + tree)
        subprocess.run(['git', 'diff', '--exit-code', '--quiet', 'HEAD', '--', tree], cwd=ROOT, check=True)
    if set(manifest['artifacts']) != {'layerxd', 'fixture', 'client'}:
        raise RuntimeError('prebuilt artifact set mismatch')
    for name, artifact in manifest['artifacts'].items():
        if not os.access(artifact['path'], os.X_OK) or digest(artifact['path']) != artifact['sha256']:
            raise RuntimeError('prebuilt executable digest mismatch: ' + name)
    return path.parent, manifest, revision


def discovery_gate(runtime, output, manifest, revision):
    sys.path.insert(0, str(ROOT / 'tools/qualification/paxeer-x/fixtures'))
    import caps_fixture
    fixture_dir = Path(tempfile.mkdtemp(prefix='caps-fixture-', dir=output)) / 'scenarios'
    try:
        scenarios = caps_fixture.run_scenarios(manifest['artifacts']['fixture']['path'], runtime, fixture_dir)
    except caps_fixture.MissingPrerequisite as error:
        raise MissingPrerequisite(str(error)) from error
    native = [case for record in scenarios.values() for case in record['cases']]
    missing = [case for case in NATIVE_CASES if case not in native]
    if missing:
        raise MissingPrerequisite('native producer cases did not execute: ' + ','.join(missing))
    if len(native) != len(set(native)):
        raise RuntimeError('native case reported twice')
    runtime.restart()
    env = dict(runtime.env, PAXEER_X_CAPS_FIXTURE_DIR=str(fixture_dir),
               PAXEER_X_FIXTURE_KEYS=str(runtime.directory / 'keys'),
               LAYERX_CAPS_SOCKET=str(runtime.directory / 'run/layerxd.lni.sock'),
               LAYERX_CAPS_RESTARTED='1',
               LAYERX_TEST_PAXEER_CHAIN_ID=str(runtime.manifest['chain_id']),
               LAYERX_CAPS_SEQUENCER_PUBLIC_KEY=runtime.manifest['sequencer_public_key'])
    result = subprocess.run([manifest['artifacts']['client']['path'], '--test-threads=1'], cwd=ROOT / 'agent', env=env,
                            text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=600)
    (output / 'client-gate.log').write_text(result.stdout)
    print(result.stdout, end='')
    if result.returncode:
        raise RuntimeError('caps_discovery client test exit ' + str(result.returncode))
    summary = re.search(r'^test result: ok\. (\d+) passed; 0 failed; 0 ignored;', result.stdout, re.M)
    if not summary:
        raise RuntimeError('caps_discovery client tests skipped, ignored or failed')
    missing = [case for case in RUST_CASES
               if not re.search(r'^test (?:\S+::)?' + case + r' \.\.\. ok$', result.stdout, re.M)]
    if missing:
        raise MissingPrerequisite('required client cases did not execute: ' + ','.join(missing))
    rust = int(summary[1])
    record = {'revision': revision, 'exit_code': 0, 'tests': rust + len(set(native)),
              'test_groups': {'client': rust, 'native': len(set(native))}, 'skipped': 0,
              'scenarios': {name: {'state_root': value['state_root'], 'items': value['items']} for name, value in scenarios.items()},
              'artifacts': {name: value['sha256'] for name, value in manifest['artifacts'].items()},
              'fixture_evidence': str(fixture_dir),
              'claim': 'native complete caps discovery and client verification only; no wallet principal authentication'}
    path = output / 'gate-result.json'
    path.write_text(json.dumps(record, indent=2) + '\n')
    path.chmod(0o600)
    print(json.dumps(record))
    print('PAXEER_X_GATE tests=' + str(record['tests']) + ' skipped=0')


def main():
    os.umask(0o077)
    output, manifest, revision = load_manifest()
    sys.path.insert(0, str(ROOT / 'tests/daemon'))
    import paxeer_x_runtime_fixture as fixture
    bundle = fixture.artifacts(os.environ.get('PAXEER_X_RUNTIME_ARTIFACTS', ''))
    client = fixture.client_artifact(os.environ.get('PAXEER_X_RUNTIME_CLIENT_MANIFEST', ''))
    if bundle['artifacts']['layerxd']['sha256'] != manifest['artifacts']['layerxd']['sha256']:
        raise MissingPrerequisite('runtime bundle layerxd is not the prebuilt caps-discovery daemon')
    if len(sys.argv) == 3 and sys.argv[1] == '--worker':
        if os.geteuid() != 0 or os.getegid() != 4020:
            raise RuntimeError('isolated producer requires UID0/GID4020')
        for name in ('net', 'pid', 'mnt'):
            if os.readlink('/proc/self/ns/' + name) == os.environ['CAPS_PARENT_' + name.upper()]:
                raise RuntimeError('missing isolated namespace: ' + name)
        subprocess.run(['ip', 'link', 'set', 'lo', 'up'], check=True)
        subprocess.run(['mount', '-t', 'tmpfs', '-o', 'mode=0755,nosuid,nodev', 'caps-runtime', '/tmp'], check=True)
        source = Path('/tmp/caps-source')
        source.mkdir(mode=0o755)
        subprocess.run(['mount', '--bind', str(ROOT), str(source)], check=True)
        subprocess.run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', str(source)], check=True)
        fixture.ROOT = source
        python_root = Path('/tmp/caps-python')
        python_root.mkdir(mode=0o755)
        subprocess.run(['mount', '--bind', sys.prefix, str(python_root)], check=True)
        subprocess.run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', str(python_root)], check=True)
        os.environ['PATH'] = str(python_root / 'bin') + ':/usr/local/bin:/usr/bin:/bin'
        runtime = fixture.RuntimeFixture(sys.argv[2], bundle, client)
        try:
            runtime.generate()
            if runtime.rpc('eth_chainId') != hex(runtime.manifest['chain_id']):
                raise RuntimeError('actual isolated chain id mismatch')
            discovery_gate(runtime, output, manifest, revision)
        finally:
            runtime.cleanup()
        return
    directory = Path(tempfile.mkdtemp(prefix='px-caps-discovery-', dir='/var/tmp'))
    directory.rmdir()
    env = dict(os.environ)
    for name in ('net', 'pid', 'mnt'):
        env['CAPS_PARENT_' + name.upper()] = os.readlink('/proc/self/ns/' + name)
    log_path = output / 'runtime-worker.log'
    with log_path.open('wb') as log:
        result = subprocess.run(['unshare', '--mount', '--net', '--pid', '--fork', '--kill-child=KILL',
                                 '--mount-proc', '--propagation', 'private', sys.executable,
                                 str(Path(__file__).resolve()), '--worker', str(directory)],
                                env=env, stdout=log, stderr=log, group=4020, extra_groups=[], timeout=840)
    print(log_path.read_text(), end='')
    if result.returncode:
        raise RuntimeError('isolated caps discovery gate exit ' + str(result.returncode))


if __name__ == '__main__':
    try:
        main()
    except MissingPrerequisite as error:
        print('caps-discovery: missing prerequisite: ' + str(error), file=sys.stderr)
        sys.exit(3)
    except (OSError, KeyError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print('caps-discovery: ' + str(error), file=sys.stderr)
        sys.exit(1)
