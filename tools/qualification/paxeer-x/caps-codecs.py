#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import shutil

sys.dont_write_bytecode = True

ROOT = Path(__file__).resolve().parents[3]


def codec_gate(runtime):
    output = Path(os.environ['CAPS_BUILD_DIR']).resolve()
    manifest = json.loads((output / 'manifest.json').read_text())
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    if manifest['revision'] != revision:
        raise RuntimeError('prebuilt artifact revision mismatch')
    for tree, expected in manifest['source_trees'].items():
        actual = subprocess.check_output(['git', 'rev-parse', 'HEAD:' + tree], cwd=ROOT, text=True).strip()
        if actual != expected:
            raise RuntimeError('prebuilt source tree mismatch: ' + tree)
        subprocess.run(['git', 'diff', '--exit-code', 'HEAD', '--', tree], cwd=ROOT, check=True)
    for artifact in [*manifest['artifacts'].values(), *manifest['authority_binaries'].values()]:
        with open(artifact['path'], 'rb') as file:
            actual = hashlib.file_digest(file, 'sha256').hexdigest()
        if actual != artifact['sha256']:
            raise RuntimeError('prebuilt executable digest mismatch')
        if not os.access(artifact['path'], os.X_OK):
            raise RuntimeError('prebuilt input is not executable')
    vectors = output / 'native-vectors.json'
    with vectors.open('w') as target:
        subprocess.run([manifest['artifacts']['native']['path']], cwd=ROOT, stdout=target, check=True, timeout=60)
    native = json.loads(vectors.read_text())
    if native.get('producer') != 'native-budget-codec-and-grant-save':
        raise RuntimeError('unexpected native producer contract')
    native_inputs = Path('/tmp/caps-native')
    native_inputs.mkdir(mode=0o755)
    for name, artifact in manifest['authority_binaries'].items():
        target = native_inputs / name
        shutil.copyfile(artifact['path'], target)
        target.chmod(0o755)
        with target.open('rb') as stream:
            if hashlib.file_digest(stream, 'sha256').hexdigest() != artifact['sha256']:
                raise RuntimeError('isolated native executable copy mismatch')
    env = dict(os.environ, LAYERX_CAPS_VECTORS=str(vectors), LAYERX_TEST_NATIVE_BIN_DIR=str(native_inputs))
    env.update(LAYERX_TEST_PAXEER_CHAIN_ID=str(runtime.manifest['chain_id']),
               LAYERX_TEST_PAXEER_RPC_PORT=str(runtime.ports[0]),
               LAYERX_TEST_SETTLEMENT_CONTRACT='0x0000000000000000000000000000000000001014',
               LAYERX_TEST_CHECKPOINT_REGISTRY='0x0000000000000000000000000000000000001014')
    if runtime.rpc('eth_chainId') != hex(runtime.manifest['chain_id']):
        raise RuntimeError('actual isolated chain id mismatch')
    counts = {}
    for name in ['client', 'agentd']:
        credentials = {'group': 0, 'extra_groups': []} if name == 'agentd' else {}
        result = subprocess.run([manifest['artifacts'][name]['path'], '--test-threads=1'], cwd=ROOT / 'agent', env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=600, **credentials)
        (output / (name + '-gate.log')).write_text(result.stdout)
        print(result.stdout, end='')
        if result.returncode:
            raise RuntimeError(name + ' test exit ' + str(result.returncode))
        match = re.search(r'test result: ok\. (\d+) passed; 0 failed; 0 ignored;', result.stdout)
        if not match or int(match[1]) != {'client': 4, 'agentd': 5}[name]:
            raise RuntimeError('missing cases or skipped tests: ' + name)
        counts[name] = int(match[1])
    record = {'revision': revision, 'exit_code': 0, 'tests': sum(counts.values()), 'test_groups': counts, 'native_vectors': 6, 'skipped': 0, 'claim': 'canonical codecs and existing reconciliation compatibility only'}
    path = output / 'gate-result.json'
    path.write_text(json.dumps(record, indent=2) + '\n')
    path.chmod(0o600)
    print(json.dumps(record))


def main():
    os.umask(0o077)
    sys.path.insert(0, str(ROOT / 'tests/daemon'))
    import paxeer_x_runtime_fixture as fixture
    bundle = fixture.artifacts(os.environ['PAXEER_X_RUNTIME_ARTIFACTS'])
    client = fixture.client_artifact(os.environ['PAXEER_X_RUNTIME_CLIENT_MANIFEST'])
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
            codec_gate(runtime)
        finally:
            runtime.cleanup()
        return
    output = Path(os.environ['CAPS_BUILD_DIR']).resolve()
    directory = Path(tempfile.mkdtemp(prefix='px-caps-', dir='/var/tmp'))
    directory.rmdir()
    (output / 'runtime-directory').write_text(str(directory) + '\n')
    env = dict(os.environ)
    for name in ('net', 'pid', 'mnt'):
        env['CAPS_PARENT_' + name.upper()] = os.readlink('/proc/self/ns/' + name)
    with (output / 'runtime-worker.log').open('wb') as log:
        result = subprocess.run(['unshare', '--mount', '--net', '--pid', '--fork', '--kill-child=KILL',
                                 '--mount-proc', '--propagation', 'private', sys.executable,
                                 str(Path(__file__).resolve()), '--worker', str(directory)],
                                env=env, stdout=log, stderr=log, group=4020, extra_groups=[], timeout=840)
    print((output / 'runtime-worker.log').read_text(), end='')
    if result.returncode:
        raise RuntimeError('isolated complete caps gate exit ' + str(result.returncode))


if __name__ == '__main__':
    try:
        main()
    except (OSError, KeyError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print('caps-codecs: ' + str(error), file=sys.stderr)
        sys.exit(1)
