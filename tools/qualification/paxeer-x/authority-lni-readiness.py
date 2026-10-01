#!/usr/bin/env python3
import json
import os
from pathlib import Path
import re
import signal
import socket
import subprocess
import sys
import tempfile
import urllib.request
import urllib.error

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'tests/daemon'))
import paxeer_x_runtime_fixture as fixture
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat


def require(condition, reason):
    if not condition:
        raise RuntimeError('authority LNI readiness refused: ' + reason)


def command(args, **kwargs):
    return subprocess.run([str(value) for value in args], check=True, **kwargs)


def inputs():
    require(os.geteuid() == 0 and os.getegid() == 4020, 'real LNI admission requires UID0/GID4020')
    bundle = fixture.artifacts(os.environ['PAXEER_X_RUNTIME_ARTIFACTS'])
    client = fixture.client_artifact(os.environ['PAXEER_X_RUNTIME_CLIENT_MANIFEST'])
    path = os.environ['PAXEER_X_AUTHORITY_BUILD_MANIFEST']
    fixture.private(path)
    built = json.loads(Path(path).read_text())
    revision = command(['git', '-C', ROOT, 'rev-parse', 'HEAD'], capture_output=True).stdout.decode().strip()
    require(built['source_revision'] == revision and built['build_exit'] == 0, 'build source or exit mismatch')
    for name in ('authority', 'authority_tests', 'authority_unit', 'gateway_tests'):
        row = built['artifacts'][name]
        target = Path(row['path'])
        require(target.is_absolute() and target.is_file() and not target.is_symlink()
                and os.access(target, os.X_OK) and fixture.digest(target) == row['sha256'], 'prebuilt artifact ' + name)
    return bundle, client, built


def worker(directory):
    bundle, client, built = inputs()
    for name in ('net', 'pid', 'mnt'):
        require(os.readlink('/proc/self/ns/' + name) != os.environ['PAXEER_X_PARENT_' + name.upper()], 'namespace isolation')
    command(['ip', 'link', 'set', 'lo', 'up'])
    command(['mount', '-t', 'tmpfs', '-o', 'mode=0755,nosuid,nodev', 'lni-readiness', '/tmp'])
    source = Path('/tmp/lni-source')
    source.mkdir(mode=0o755)
    command(['mount', '--bind', ROOT, source])
    command(['mount', '-o', 'remount,bind,ro,nosuid,nodev', source])
    fixture.ROOT = source
    python_root = Path('/tmp/lni-python')
    python_root.mkdir(mode=0o755)
    command(['mount', '--bind', sys.prefix, python_root])
    command(['mount', '-o', 'remount,bind,ro,nosuid,nodev', python_root])
    os.environ['PATH'] = str(python_root / 'bin') + ':/usr/local/bin:/usr/bin:/bin'
    runtime = fixture.RuntimeFixture(directory, bundle, client)
    authority = None
    count = 0
    tests_log = runtime.directory / 'contract-tests.log'
    def run_test(name, test, env):
        nonlocal count
        result = subprocess.run([built['artifacts'][name]['path'], test, '--exact', '--nocapture', '--test-threads=1'],
                                env=dict(os.environ, **env), capture_output=True, text=True, timeout=30)
        with tests_log.open('a') as log:
            log.write(result.stdout + result.stderr)
        require(result.returncode == 0 and '1 passed; 0 failed; 0 ignored' in result.stdout, 'actual Rust contract failed: ' + test)
        matches = re.findall(r'^PAXEER_X_(?:LNI|AUTHORITY)_CASES=(\d+)$', result.stdout, re.M)
        require(len(matches) == 1 and int(matches[0]) > 0, 'actual case accounting')
        count += int(matches[0])
        print('PAXEER_X_PROGRESS cases=' + str(count), flush=True)
    try:
        runtime.generate()
        d = runtime.directory
        node = dict(line.split('=', 1) for line in (d / 'node/node.env').read_text().splitlines())
        genesis = fixture.digest(d / 'node/genesis/genesis.manifest')
        cert, key = d / 'authority.der', d / 'authority-key.der'
        with (d / 'tls-producer.log').open('wb') as log:
            command(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1', '-subj', '/CN=localhost',
                     '-addext', 'subjectAltName=DNS:localhost', '-keyout', d / 'authority-key.pem', '-out', d / 'authority.pem'], stdout=log, stderr=log)
            command(['openssl', 'x509', '-in', d / 'authority.pem', '-outform', 'DER', '-out', cert], stdout=log, stderr=log)
            command(['openssl', 'pkcs8', '-topk8', '-nocrypt', '-in', d / 'authority-key.pem', '-outform', 'DER', '-out', key], stdout=log, stderr=log)
        token = d / 'authority-token'
        token.write_text(os.urandom(32).hex())
        token.chmod(0o600)
        reservation = fixture.reserve_ports(1)[0]
        port = reservation.getsockname()[1]
        reservation.close()
        base = {'LAYERX_AUTHORITY_LISTEN': '127.0.0.1:' + str(port), 'LAYERX_AUTHORITY_TLS_CERT_DER': str(cert),
                'LAYERX_AUTHORITY_TLS_KEY_DER': str(key), 'LAYERX_AUTHORITY_TOKEN_FILES': str(token),
                'LAYERX_AUTHORITY_REPLICA_URL': 'http://127.0.0.1:' + str(runtime.ports[8]),
                'LAYERX_AUTHORITY_REPLICA_BEARER_TOKEN_FILE': str(d / 'node/secrets/replica-token'),
                'LAYERX_AUTHORITY_REPLICA_ID': node['LAYERX_NODE_REPLICA_ID'],
                'LAYERX_AUTHORITY_LNI_SOCKET': runtime.manifest['node_socket'],
                'LAYERX_AUTHORITY_PROTOCOL_NETWORK_ID': str(fixture.NETWORK), 'LAYERX_AUTHORITY_NETWORK_ID': 'paxeer-x-lni',
                'LAYERX_AUTHORITY_WIRE_VERSION': '3', 'LAYERX_AUTHORITY_SEQUENCER_ID': node['LAYERX_NODE_SEQUENCER_ID'],
                'LAYERX_AUTHORITY_SEQUENCER_PUBLIC_KEY': runtime.manifest['sequencer_public_key'],
                'LAYERX_AUTHORITY_FIRST_BATCH': '1', 'LAYERX_AUTHORITY_LAST_BATCH': str(2**64 - 1)}
        def start_authority(changes=None, group=4020):
            nonlocal authority
            old_pid = None
            if authority is not None:
                old_pid = authority.pid
                authority.terminate()
                authority.wait(timeout=15)
            with (d / 'authority.log').open('ab') as log:
                authority = subprocess.Popen([built['artifacts']['authority']['path']], env=base | (changes or {}), stdout=log, stderr=log, group=group, extra_groups=[])
            def listening():
                require(authority.poll() is None, 'authority exited')
                with socket.create_connection(('127.0.0.1', port), timeout=.3):
                    return True
            runtime.wait(listening)
            require(authority.pid != old_pid, 'authority restart reused process')
        capture = d / 'node-info.bin'
        response = d / 'readiness.json'
        def probe(label, ready, lni_ready, lni_reason, replica_ready=True, capture_lni=False, gateway_expected=None):
            case = {'ca_der': str(cert), 'port': port, 'ready': ready, 'replica_ready': replica_ready,
                    'lni_ready': lni_ready, 'lni_reason': lni_reason, 'empty': replica_ready,
                    'response_file': str(response)}
            if capture_lni:
                case.update(node_socket=runtime.manifest['node_socket'], network_id=fixture.NETWORK, capture=str(capture))
            path = d / (label + '.json')
            fixture.write_json(path, case)
            run_test('authority_tests', 'authority_lni_readiness_case', {'PAXEER_X_AUTHORITY_LNI_CASE': str(path)})
            gateway = {'ca_der': str(cert), 'endpoint': 'https://localhost:' + str(port), 'token_file': str(token),
                       'network_id': 'paxeer-x-lni', 'protocol_network_id': fixture.NETWORK, 'wire_version': '3',
                       'expected': gateway_expected or ('ready' if ready else 'unavailable'), 'mutations': False,
                       'response_file': str(response)}
            gateway_path = d / (label + '-gateway.json')
            fixture.write_json(gateway_path, gateway)
            run_test('gateway_tests', 'authority_readiness_contract_tests::authority_readiness_contract',
                     {'PAXEER_X_AUTHORITY_CONTRACT_CASE': str(gateway_path)})
            return gateway_path
        replica_token = (d / 'node/secrets/replica-token').read_text().strip()
        empty_request = urllib.request.Request('http://127.0.0.1:' + str(runtime.ports[8]) + '/v1/batches/' + '0' * 64 + '/receipt-authority?receipt_digest=' + '0' * 64,
            headers={'Authorization': 'Bearer ' + replica_token})
        try:
            urllib.request.urlopen(empty_request, timeout=3)
            raise RuntimeError('empty real replica unexpectedly supplied batch evidence')
        except urllib.error.HTTPError as error:
            require(error.code == 404, 'empty valid replica must answer404')
        count += 1
        start_authority()
        good_case = probe('empty-genesis', True, True, 'ready', capture_lni=True)
        run_test('authority_unit', 'lni_readiness_tests::actual_node_info_refuses_incompatible_receipt_admission',
                 {'PAXEER_X_LNI_CAPTURE': str(capture)})
        run_test('gateway_tests', 'authority_lni_compatibility_tests::authenticated_lni_preserves_the_strict_four_field_contract',
                 {'PAXEER_X_AUTHORITY_CONTRACT_CASE': str(good_case)})
        start_authority(group=0)
        probe('wrong-peer-admission', False, False, 'unavailable')
        start_authority({'LAYERX_AUTHORITY_LNI_SOCKET': str(d / 'absent.sock')})
        probe('missing-lni', False, False, 'unavailable')
        wrong_key = Ed25519PrivateKey.generate().public_key().public_bytes(Encoding.Raw, PublicFormat.Raw).hex()
        start_authority({'LAYERX_AUTHORITY_SEQUENCER_PUBLIC_KEY': wrong_key})
        probe('wrong-key', False, False, 'key_mismatch')
        start_authority({'LAYERX_AUTHORITY_PROTOCOL_NETWORK_ID': str(fixture.NETWORK + 1)})
        probe('wrong-network', False, False, 'identity_mismatch', gateway_expected='identity_mismatch')
        start_authority()
        probe('authority-restart', True, True, 'ready', capture_lni=True)
        sequencer = runtime.processes['sequencer']
        os.kill(sequencer.pid, signal.SIGSTOP)
        try:
            probe('handshake-timeout', False, False, 'timeout')
        finally:
            os.kill(sequencer.pid, signal.SIGCONT)
        runtime.readiness()
        probe('handshake-reconnect', True, True, 'ready', capture_lni=True)
        runtime.stop_role('sequencer', kill=True)
        probe('lni-loss', False, False, 'unavailable')
        runtime.start_role('sequencer')
        runtime.readiness()
        probe('lni-restart', True, True, 'ready', capture_lni=True)
        runtime.stop_role('replica', kill=True)
        probe('replica-loss', False, True, 'ready', replica_ready=False, capture_lni=True)
        runtime.start_role('replica')
        runtime.wait(runtime.replica_ready)
        probe('replica-reconnect', True, True, 'ready', capture_lni=True)
        require(fixture.digest(d / 'node/genesis/genesis.manifest') == genesis, 'readiness changed signed genesis')
        print(f'PAXEER_X_GATE tests={count} skipped=0', flush=True)
    finally:
        if authority is not None and authority.poll() is None:
            authority.terminate()
            authority.wait(timeout=15)
        runtime.cleanup()


def main():
    os.umask(0o077)
    if len(sys.argv) == 3 and sys.argv[1] == '--worker':
        worker(sys.argv[2])
        return 0
    inputs()
    evidence = Path(os.environ['PAXEER_X_AUTHORITY_EVIDENCE'])
    evidence.mkdir(mode=0o700, parents=True, exist_ok=True)
    directory = Path(tempfile.mkdtemp(prefix='px-authority-lni-', dir='/var/tmp'))
    directory.rmdir()
    (evidence / 'runtime-directory').write_text(str(directory) + '\n')
    env = dict(os.environ)
    for name in ('net', 'pid', 'mnt'):
        env['PAXEER_X_PARENT_' + name.upper()] = os.readlink('/proc/self/ns/' + name)
    log_path = evidence / 'worker.log'
    with log_path.open('wb') as log:
        result = subprocess.run(['unshare', '--mount', '--net', '--pid', '--fork', '--kill-child=KILL', '--mount-proc', '--propagation', 'private',
                                 sys.executable, str(Path(__file__).resolve()), '--worker', str(directory)],
                                env=env, stdout=log, stderr=log, timeout=840)
    log = log_path.read_text()
    markers = re.findall(r'^PAXEER_X_GATE tests=(\d+) skipped=0$', log, re.M)
    progress = re.findall(r'^PAXEER_X_PROGRESS cases=(\d+)$', log, re.M)
    count = int(markers[0]) if len(markers) == 1 else int(progress[-1]) if progress else 0
    print(f'PAXEER_X_GATE tests={count} skipped=0', flush=True)
    require(result.returncode == 0 and len(markers) == 1 and count > 0, 'worker failed; inspect private worker.log')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
