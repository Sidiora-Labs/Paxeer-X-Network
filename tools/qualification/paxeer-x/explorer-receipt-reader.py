import hashlib
import json
import os
from pathlib import Path
import socket
import re
import subprocess
import sys
import tempfile
import threading

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'tests/daemon'))
import paxeer_x_runtime_fixture as fixture
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat


def require(value, message):
    if not value:
        raise RuntimeError('receipt reader gate refused: ' + message)


def command(args, **kwargs):
    return subprocess.run([str(arg) for arg in args], check=True, **kwargs)


def revision():
    return command(['git', '-C', ROOT, 'rev-parse', 'HEAD'], capture_output=True).stdout.decode().strip()


def inputs():
    bundle = fixture.artifacts(os.environ['PAXEER_X_RUNTIME_ARTIFACTS'])
    manifest_path = os.environ['PAXEER_X_READER_BUILD_MANIFEST']
    fixture.private(manifest_path)
    built = json.loads(Path(manifest_path).read_text())
    require(built['source_revision'] == revision(), 'reader build source differs')
    for name in ('test', 'authority'):
        row = built[name]
        target = Path(row['path'])
        require(target.is_absolute() and target.is_file() and not target.is_symlink()
                and os.access(target, os.X_OK) and fixture.digest(target) == row['sha256'], 'prebuilt ' + name)
    client_path = os.environ['PAXEER_X_RUNTIME_CLIENT_MANIFEST']
    fixture.private(client_path)
    client = json.loads(Path(client_path).read_text())
    require(fixture.digest(client['path']) == client['sha256'], 'client digest')
    changed = command(['git', '-C', ROOT, 'diff', '--name-only', client['source_revision'], 'HEAD', '--',
        'tests/daemon/lxp_test_runtime_fixture.c', 'include', 'src'], capture_output=True).stdout
    require(not changed, 'prebuilt real client production source differs')
    return bundle, client, built


class ReaderFixture(fixture.RuntimeFixture):
    def produce(self, label, argv, env=None, timeout=120):
        if label == 'bootstrap':
            key = Ed25519PrivateKey.from_private_bytes((self.directory / 'keys/treasury.seed').read_bytes())
            public = key.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw).hex()
            argv = [*argv, '--handover-authority', public]
        return super().produce(label, argv, env, timeout)


def worker(directory):
    bundle, client, built = inputs()
    for name in ('net', 'pid', 'mnt'):
        require(os.readlink('/proc/self/ns/' + name) != os.environ['PAXEER_X_PARENT_' + name.upper()], 'namespace isolation')
    command(['ip', 'link', 'set', 'lo', 'up'])
    command(['mount', '-t', 'tmpfs', '-o', 'mode=0755,nosuid,nodev', 'reader-fixture', '/tmp'])
    source = Path('/tmp/reader-source')
    source.mkdir(mode=0o755)
    command(['mount', '--bind', ROOT, source])
    command(['mount', '-o', 'remount,bind,ro,nosuid,nodev', source])
    fixture.ROOT = source
    python_root = Path('/tmp/reader-python')
    python_root.mkdir(mode=0o755)
    command(['mount', '--bind', sys.prefix, python_root])
    command(['mount', '-o', 'remount,bind,ro,nosuid,nodev', python_root])
    os.environ['PATH'] = str(python_root / 'bin') + ':/usr/local/bin:/usr/bin:/bin'
    os.setegid(fixture.UID)
    runtime = ReaderFixture(directory, bundle, client)
    authority = None
    token_fd = None
    control = None
    try:
        runtime.generate()
        rows = []
        for op, seq in [('register', 0), ('open', 1), ('open-bob', 0), ('mint', 2), ('burn', 3), ('grant-issue', 4), ('grant-revoke', 5), ('sends', 6)]:
            result = runtime.invoke(op, seq, 'reader-' + op)
            produced = [dict(item.split('=', 1) for item in line.split()[1:]) for line in result.stdout.decode().splitlines() if line.startswith('receipt ')]
            require(produced and all(row['result'] == '0' for row in produced), 'signed operation receipt ' + op)
            rows.extend(produced)
        (runtime.directory / 'salt').rename(runtime.directory / 'first-asset-salt')
        (runtime.directory / 'salt').write_bytes(os.urandom(32))
        for op, seq in [('register', 26), ('open', 27), ('mint', 28)]:
            result = runtime.invoke(op, seq, 'reader-second-asset-' + op)
            produced = [dict(item.split('=', 1) for item in line.split()[1:]) for line in result.stdout.decode().splitlines() if line.startswith('receipt ')]
            require(produced and all(row['result'] == '0' for row in produced), 'second asset signed operation receipt ' + op)
            rows.extend(produced)
        evidence = runtime.catch_up(rows)
        require(len(rows) == len(evidence), 'receipt/evidence cardinality')
        for row, document in zip(rows, evidence):
            row['evidence'] = document
        d = runtime.directory
        node = dict(line.split('=', 1) for line in (d / 'node/node.env').read_text().splitlines())
        cert = d / 'authority.der'
        ca = d / 'authority-ca.der'
        key = d / 'authority-key.der'
        extensions = d / 'authority-extensions.cnf'
        extensions.write_text('basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\nsubjectAltName=DNS:localhost,IP:127.0.0.1\n')
        with (d / 'tls-producer.log').open('wb') as log:
            command(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1', '-subj', '/CN=Reader Fixture CA',
                '-addext', 'basicConstraints=critical,CA:TRUE,pathlen:0', '-addext', 'keyUsage=critical,keyCertSign,cRLSign',
                '-keyout', d / 'authority-ca-key.pem', '-out', d / 'authority-ca.pem'], stdout=log, stderr=log)
            command(['openssl', 'req', '-new', '-newkey', 'rsa:2048', '-nodes', '-subj', '/CN=localhost',
                '-keyout', d / 'authority-key.pem', '-out', d / 'authority.csr'], stdout=log, stderr=log)
            command(['openssl', 'x509', '-req', '-in', d / 'authority.csr', '-CA', d / 'authority-ca.pem',
                '-CAkey', d / 'authority-ca-key.pem', '-CAcreateserial', '-days', '1', '-extfile', extensions,
                '-out', d / 'authority.pem'], stdout=log, stderr=log)
            command(['openssl', 'x509', '-in', d / 'authority.pem', '-outform', 'DER', '-out', cert], stdout=log, stderr=log)
            command(['openssl', 'x509', '-in', d / 'authority-ca.pem', '-outform', 'DER', '-out', ca], stdout=log, stderr=log)
            command(['openssl', 'pkcs8', '-topk8', '-nocrypt', '-in', d / 'authority-key.pem', '-outform', 'DER', '-out', key], stdout=log, stderr=log)
        token_fd = os.memfd_create('reader-authority-bearer', 0)
        os.fchmod(token_fd, 0o600)
        os.write(token_fd, os.urandom(32).hex().encode())
        os.lseek(token_fd, 0, os.SEEK_SET)
        token_path = '/proc/self/fd/' + str(token_fd)
        reservation = fixture.reserve_ports(1)[0]
        port = reservation.getsockname()[1]
        reservation.close()
        authority_env = {'LAYERX_AUTHORITY_LISTEN': '127.0.0.1:' + str(port), 'LAYERX_AUTHORITY_TLS_CERT_DER': str(cert),
            'LAYERX_AUTHORITY_TLS_KEY_DER': str(key), 'LAYERX_AUTHORITY_TOKEN_FILES': token_path,
            'LAYERX_AUTHORITY_REPLICA_URL': 'http://127.0.0.1:' + str(runtime.ports[8]),
            'LAYERX_AUTHORITY_REPLICA_BEARER_TOKEN_FILE': str(d / 'node/secrets/replica-token'),
            'LAYERX_AUTHORITY_REPLICA_ID': node['LAYERX_NODE_REPLICA_ID'],
            'LAYERX_AUTHORITY_LNI_SOCKET': str(d / 'run/layerxd.lni.sock'), 'LAYERX_AUTHORITY_PROTOCOL_NETWORK_ID': '77',
            'LAYERX_AUTHORITY_NETWORK_ID': 'paxeer-x-reader', 'LAYERX_AUTHORITY_WIRE_VERSION': '3',
            'LAYERX_AUTHORITY_SEQUENCER_ID': node['LAYERX_NODE_SEQUENCER_ID'],
            'LAYERX_AUTHORITY_SEQUENCER_PUBLIC_KEY': runtime.manifest['sequencer_public_key'],
            'LAYERX_AUTHORITY_FIRST_BATCH': '1', 'LAYERX_AUTHORITY_LAST_BATCH': str(2**64 - 1)}
        with (d / 'authority.log').open('wb') as log:
            authority = subprocess.Popen([built['authority']['path']], env=authority_env, pass_fds=(token_fd,), stdout=log, stderr=log)
        def authority_ready():
            require(authority.poll() is None, 'actual TLS authority exited')
            with socket.create_connection(('127.0.0.1', port), timeout=.3):
                return True
        runtime.wait(authority_ready)
        control_path = str(d / 'reader-control.sock')
        control = socket.socket(socket.AF_UNIX)
        control.bind(control_path)
        control.listen(1)
        control.settimeout(300)
        errors = []
        def stop_replica():
            try:
                conn, _ = control.accept()
                with conn:
                    require(conn.recv(64) == b'stop-replica\n', 'unknown control request')
                    runtime.stop_role('replica', kill=True)
                    conn.sendall(b'stopped\n')
            except Exception as error:
                errors.append(type(error).__name__)
        thread = threading.Thread(target=stop_replica, daemon=True)
        thread.start()
        registration = (d / 'node/genesis/paxeer-registration-request.lxrr').read_bytes()
        require(len(registration) == 73, 'canonical native registration')
        fixture_path = d / 'reader.json'
        fixture.write_json(fixture_path, {'authority_endpoint': 'https://localhost:' + str(port),
            'node_endpoint': 'https://localhost:' + str(runtime.ports[7]), 'ca': str(ca),
            'replica_id': node['LAYERX_NODE_REPLICA_ID'], 'sequencer_key': runtime.manifest['sequencer_public_key'],
            'genesis_root': registration[9:41].hex(), 'genesis_trust': str(d / 'node/genesis/genesis-handover-trust.lxt'),
            'node_socket': str(d / 'run/layerxd.lni.sock'), 'control_socket': control_path, 'receipts': rows})
        env = dict(os.environ, PAXEER_X_READER_FIXTURE=str(fixture_path), PAXEER_X_READER_TOKEN_FD=token_path)
        with (d / 'reader-test.log').open('wb') as log:
            result = subprocess.run([built['test']['path'], '--exact', 'real_receipts_preserve_per_receipt_authority_and_refuse_untrusted_evidence', '--nocapture', '--test-threads=1'], env=env, pass_fds=(token_fd,), stdout=log, stderr=log, timeout=300)
        counts = re.search(r'test result: \w+\. (\d+) passed; (\d+) failed; (\d+) ignored;', (d / 'reader-test.log').read_text())
        if counts:
            passed, failed, skipped = map(int, counts.groups())
            fixture.write_json(d / 'reader-result.json', {'tests': passed + failed + skipped, 'passed': passed, 'failed': failed, 'skipped': skipped, 'exit': result.returncode})
        require(result.returncode == 0, 'prebuilt real reader test exited ' + str(result.returncode))
        thread.join(timeout=2)
        require(not thread.is_alive() and not errors, 'real replica outage control did not complete')
        require('1 passed; 0 failed; 0 ignored' in (d / 'reader-test.log').read_text(), 'test accounting')
    finally:
        if control is not None:
            control.close()
        if authority is not None and authority.poll() is None:
            authority.terminate()
            authority.wait(timeout=20)
        if token_fd is not None:
            os.close(token_fd)
        runtime.cleanup()


def main():
    os.umask(0o077)
    if len(sys.argv) == 3 and sys.argv[1] == '--worker':
        worker(sys.argv[2])
        return 0
    inputs()
    evidence = Path(os.environ['PAXEER_X_READER_EVIDENCE'])
    evidence.mkdir(mode=0o700, parents=True, exist_ok=True)
    directory = Path(tempfile.mkdtemp(prefix='px-reader-', dir='/var/tmp'))
    directory.rmdir()
    (evidence / 'runtime-directory').write_text(str(directory) + '\n')
    env = dict(os.environ)
    for name in ('net', 'pid', 'mnt'):
        env['PAXEER_X_PARENT_' + name.upper()] = os.readlink('/proc/self/ns/' + name)
    with (evidence / 'worker.log').open('wb') as log:
        result = subprocess.run(['unshare', '--mount', '--net', '--pid', '--fork', '--kill-child=KILL', '--mount-proc', '--propagation', 'private',
            sys.executable, str(Path(__file__).resolve()), '--worker', str(directory)], env=env, stdout=log, stderr=log, timeout=1100)
    report_path = directory / 'reader-result.json'
    report = json.loads(report_path.read_text()) if report_path.exists() else {'tests': 0, 'skipped': 0}
    print('PAXEER_X_GATE tests=' + str(report['tests']) + ' skipped=' + str(report['skipped']), flush=True)
    return result.returncode


if __name__ == '__main__':
    raise SystemExit(main())
