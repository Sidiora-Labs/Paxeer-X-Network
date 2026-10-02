#!/usr/bin/env python3
import copy
import datetime
import hashlib
import http.server
import importlib.util
import ipaddress
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import threading
import time
import urllib.request

sys.dont_write_bytecode = True
import paxeer_x_runtime_fixture as fixture
from paxeer_x_checkpoint_retry import manifest, require
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.x509.oid import NameOID, ExtendedKeyUsageOID
from eth_account import Account
from eth_keys import keys

ROOT = Path(__file__).resolve().parents[2]


def load_settlement():
    spec = importlib.util.spec_from_file_location('checkpoint_settlement', ROOT / 'cmd/layerx-guarantor/settlement.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def wait(predicate, reason, seconds=180):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        value = predicate()
        if value:
            return value
        time.sleep(.025)
    raise RuntimeError('deadline: ' + reason)


class Transport:
    def __init__(self, upstream):
        self.upstream, self.arm = upstream, None
        self.entered, self.release = threading.Event(), threading.Event()
        self.transactions = set()
        self.drop = 0
        owner = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                body = self.rfile.read(int(self.headers['Content-Length']))
                request = json.loads(body)
                if owner.drop and request['method'] == 'eth_getTransactionReceipt':
                    owner.drop -= 1
                    self.close_connection = True
                    return
                try:
                    if request['method'] == 'eth_sendRawTransaction' and owner.arm == 'before-broadcast':
                        owner.arm = None
                        owner.entered.set()
                        require(owner.release.wait(60), 'pre-broadcast barrier was not released')
                    with urllib.request.urlopen(urllib.request.Request(owner.upstream, body,
                            {'Content-Type': 'application/json'}), timeout=10) as response:
                        answer = response.read()
                    value = json.loads(answer)
                    if request['method'] == 'eth_sendRawTransaction' and 'result' in value:
                        owner.transactions.add(value['result'])
                        if owner.arm == 'broadcast':
                            owner.arm = None
                            owner.entered.set()
                            require(owner.release.wait(60), 'broadcast interruption was not released')
                            self.close_connection = True
                            return
                    self.send_response(200)
                    self.send_header('Content-Type', 'application/json')
                    self.send_header('Content-Length', str(len(answer)))
                    self.end_headers()
                    self.wfile.write(answer)
                except (OSError, BrokenPipeError):
                    self.close_connection = True

            def log_message(self, *_):
                pass

        self.server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.port = self.server.server_address[1]
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()

    def barrier(self, before=False):
        self.entered.clear()
        self.release.clear()
        self.arm = 'before-broadcast' if before else 'broadcast'

    def close(self):
        self.release.set()
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=10)


class Runtime(fixture.RuntimeFixture):
    def __init__(self, directory, bundle, client, window):
        self.window = window
        super().__init__(directory, bundle, client)

    def produce(self, label, argv, env=None, timeout=120):
        if label == 'anchor-genesis':
            argv = [*argv, '--challenge-window-seconds', str(self.window)]
        return super().produce(label, argv, env, timeout)


def write_private(path, data):
    path.write_bytes(data)
    path.chmod(0o600)
    os.chown(path, fixture.UID, fixture.UID)


def tls(directory):
    directory.mkdir(mode=0o700)
    os.chown(directory, fixture.UID, fixture.UID)
    ca_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, 'checkpoint retry fixture')])
    now = datetime.datetime.now(datetime.timezone.utc)
    ca = (x509.CertificateBuilder().subject_name(name).issuer_name(name)
          .public_key(ca_key.public_key()).serial_number(x509.random_serial_number())
          .not_valid_before(now - datetime.timedelta(minutes=1)).not_valid_after(now + datetime.timedelta(days=1))
          .add_extension(x509.BasicConstraints(ca=True, path_length=0), critical=True)
          .sign(ca_key, hashes.SHA256()))
    write_private(directory / 'ca.pem', ca.public_bytes(serialization.Encoding.PEM))
    for index in (1, 2):
        key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
        leaf = (x509.CertificateBuilder().subject_name(x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, 'guarantor-' + str(index))]))
                .issuer_name(name).public_key(key.public_key()).serial_number(x509.random_serial_number())
                .not_valid_before(now - datetime.timedelta(minutes=1)).not_valid_after(now + datetime.timedelta(days=1))
                .add_extension(x509.SubjectAlternativeName([x509.IPAddress(ipaddress.ip_address('127.0.0.1'))]), critical=False)
                .add_extension(x509.ExtendedKeyUsage([ExtendedKeyUsageOID.SERVER_AUTH, ExtendedKeyUsageOID.CLIENT_AUTH]), critical=False)
                .sign(ca_key, hashes.SHA256()))
        write_private(directory / f'{index}.pem', leaf.public_bytes(serialization.Encoding.PEM))
        write_private(directory / f'{index}.key', key.private_bytes(serialization.Encoding.PEM,
                      serialization.PrivateFormat.PKCS8, serialization.NoEncryption()))


def transaction(s, rpc, account, data, value=0, success=True, target=None):
    tx = {'chainId': 125, 'nonce': int(rpc.call('eth_getTransactionCount', [account.address, 'pending']), 16),
          'to': s.to_checksum_address(target or s.ANCHOR), 'data': data, 'value': value,
          'gas': 5_000_000, 'gasPrice': int(rpc.call('eth_gasPrice', []), 16)}
    signed = account.sign_transaction(tx)
    digest = rpc.call('eth_sendRawTransaction', ['0x' + bytes(signed.raw_transaction).hex()])
    require(s.raw(digest, 32) == bytes(signed.hash), 'real transaction hash mismatch')
    receipt = wait(lambda: rpc.call('eth_getTransactionReceipt', [digest]), 'real transaction receipt', 60)
    require(int(receipt['status'], 16) == int(success), 'unexpected genuine receipt status')
    return receipt


def producers(runtime, proxy, s, timeout_ms):
    directory = runtime.directory
    account = Account.from_key((directory / 'keys/deployer.key').read_text().strip())
    rpc = s.RPC(runtime.rpc_url)
    members = []
    for index in (1, 2):
        identity = directory / f'guarantor-{index}/identity'
        private = serialization.load_pem_private_key((identity / 'key.pem').read_bytes(), password=None)
        key = keys.PrivateKey(private.private_numbers().private_value.to_bytes(32, 'big'))
        public = key.public_key.to_compressed_bytes()
        identifier = hashlib.sha256(b'layerx-beta-guarantor:' + public.hex().encode()).digest()
        signer = key.public_key.to_checksum_address()
        transaction(s, rpc, account, s.calldata('registerGuarantor(bytes32,address)', ('bytes32', 'address'),
                    (identifier, signer)), value=1_000_000 * s.UNIT_WEI)
        transaction(s, rpc, account, s.calldata('activateGuarantor(bytes32)', ('bytes32',), (identifier,)))
        members.append({'guarantor_id': '0x' + identifier.hex(), 'signer': signer, 'public_key': '0x' + public.hex()})
    tls(directory / 'tls')
    policy = json.loads((ROOT / 'contracts/config/checkpoint-settlement.json').read_text())
    policy['settlement_domains']['retry'] = {'paxeer_chain_id': 125, 'network_id': fixture.NETWORK,
        'settlement_contract': s.ANCHOR, 'guarantor_bond': s.ANCHOR, 'minimum_bond': 1_000_000,
        'maximum_attestation_delay_ms': 3_600_000, 'guarantor_set': sorted(members, key=lambda m: m['guarantor_id'])}
    write_private(directory / 'settlement.json', json.dumps(policy).encode())
    reservations = fixture.reserve_ports(2)
    ports = [sock.getsockname()[1] for sock in reservations]
    for sock in reservations:
        sock.close()
    environments = []
    for index, member in enumerate(members, 1):
        identity = directory / f'guarantor-{index}/identity'
        state = directory / f'producer-{index}'
        state.mkdir(mode=0o700)
        os.chown(state, fixture.UID, fixture.UID)
        env = {'LAYERX_NODE_PAXEER_CHAIN_ID': '125', 'LAYERX_NODE_PAXEER_RPC_ADDRESS': '127.0.0.1',
            'LAYERX_NODE_PAXEER_RPC_PORT': str(proxy.port), 'LAYERX_NODE_SETTLEMENT_CONTRACT': s.ANCHOR,
            'LAYERX_NODE_CHECKPOINT_REGISTRY': s.ANCHOR, 'LAYERX_NODE_FIRST_BATCH': '1',
            'LAYERX_NODE_LAST_BATCH': str(2**64-1), 'LAYERX_NODE_NETWORK_ID': str(fixture.NETWORK),
            'LAYERX_NODE_ASSET_ID': fixture.ASSET,
            'LAYERX_NODE_SEQUENCER_ID': hashlib.sha256(('layerx-sequencer:' + runtime.manifest['sequencer_public_key']).encode()).hexdigest(),
            'LAYERX_NODE_SEQUENCER_PUBLIC_KEY': runtime.manifest['sequencer_public_key'],
            'LAYERX_NODE_SNAPSHOT': str(identity / 'genesis.lxs'),
            'LAYERX_NODE_GENESIS_MANIFEST': str(identity / 'genesis.manifest'),
            'LAYERX_NODE_GENESIS_REGISTRATION': str(identity / 'genesis.registration'),
            'LAYERX_NODE_IDENTITIES': str(identity / 'identities.txt'),
            'LAYERX_GUARANTOR_NODE_CONFIG': str(identity / 'node.conf'),
            'LAYERX_GUARANTOR_ID': member['guarantor_id'][2:], 'LAYERX_GUARANTOR_KEY_FILE': str(identity / 'key.pem'),
            'LAYERX_GUARANTOR_STATE_DIR': str(state),
            'LAYERX_GUARANTOR_LNI_SOCKET': str(directory / 'run/layerxd.lni.sock'),
            'LAYERX_GUARANTOR_SETTLEMENT_FILE': str(directory / 'settlement.json'),
            'LAYERX_GUARANTOR_SETTLEMENT_DOMAIN': 'retry',
            'LAYERX_GUARANTOR_SUBMITTER_KEY_FILE': str(directory / 'keys/deployer.key'),
            'LAYERX_GUARANTOR_SUBMITTER_LOCK_FILE': str(directory / 'submitter.lock'),
            'LAYERX_GUARANTOR_PYTHON': str(Path('/tmp/retry-python/bin') / Path(sys.executable).name),
            'LAYERX_GUARANTOR_SETTLEMENT_HELPER': str(ROOT / 'cmd/layerx-guarantor/settlement.py'),
            'LAYERX_GUARANTOR_CHECKPOINT_TIMEOUT_MS': str(timeout_ms),
            'LAYERX_GUARANTOR_LISTEN_PORT': str(ports[index-1]),
            'LAYERX_GUARANTOR_PEER_URL': f'https://127.0.0.1:{ports[2-index]}',
            'LAYERX_GUARANTOR_TLS_CA_FILE': str(directory / 'tls/ca.pem'),
            'LAYERX_GUARANTOR_TLS_CERT_FILE': str(directory / f'tls/{index}.pem'),
            'LAYERX_GUARANTOR_TLS_KEY_FILE': str(directory / f'tls/{index}.key')}
        environments.append(env)
    return environments, account


def launch(runtime, environments, pause=None):
    for index, env in enumerate(environments, 1):
        if pause:
            env = env | {'LAYERX_GUARANTOR_CHECKPOINT_PAUSE_AT': pause}
        runtime.launch('producer-' + str(index), [runtime.binary('layerx-guarantor'), '--once'], env)


def kill_producers(runtime):
    for name in ('producer-1', 'producer-2'):
        process = runtime.processes.pop(name)
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait(timeout=10)


def journals(runtime):
    return [json.loads(path.read_text()) for path in runtime.directory.glob('producer-*/checkpoint-progress-*.json')]


def stopped(runtime):
    return all('\nState:\tT' in Path(f'/proc/{runtime.processes[name].pid}/status').read_text()
               for name in ('producer-1', 'producer-2'))


def negative_requests(s, rpc, captured, runtime, account):
    nonce = int(rpc.call('eth_getTransactionCount', [account.address, 'latest']), 16)
    original = copy.deepcopy(captured)
    original['rpc_url'] = runtime.rpc_url
    for field in ('wire_output', 'native_facts'):
        original.pop(field, None)
    h = s.values(s.HEADER_TYPES, original['header'])
    for mode in ('checkpoint-binding', 'unbonded', 'stale-attestation'):
        request = copy.deepcopy(original)
        state = runtime.directory / ('refusal-' + mode)
        state.mkdir(mode=0o700)
        request['state_dir'] = str(state)
        if mode == 'checkpoint-binding':
            request['checkpoint_id'] = '0x' + bytes(32).hex()
            try:
                s.register(rpc, request)
            except ValueError as error:
                require(str(error) == 'checkpoint id mismatch', 'wrong binding refusal')
            else:
                raise AssertionError('mismatched checkpoint accepted')
            continue
        attestations = [list(s.values(s.ATTESTATION_TYPES, a)) for a in request['attestations']]
        a = attestations[0]
        if mode == 'unbonded':
            key = keys.PrivateKey(bytes(Account.create().key))
        else:
            selected = None
            for identity in runtime.directory.glob('guarantor-*/identity/key.pem'):
                secret = serialization.load_pem_private_key(identity.read_bytes(), password=None)
                candidate = keys.PrivateKey(secret.private_numbers().private_value.to_bytes(32, 'big'))
                if candidate.public_key.to_checksum_address().lower() == a[14].lower():
                    selected = candidate
            require(selected is not None, 'real guarantor key missing')
            key = selected
            a[13] = h[13] + 3_600_001
        a[14] = key.public_key.to_checksum_address()
        signature = key.sign_msg_hash(hashlib.sha256(b'LXP/v2/guarantor-attestation\0' +
                    s.encode_packed(s.ATTESTATION_TYPES[:14], a[:14])).digest())
        a[15], a[16], a[17] = signature.r.to_bytes(32, 'big'), signature.s.to_bytes(32, 'big'), signature.v + 27
        request['attestations'] = [['0x' + item.hex() if isinstance(item, bytes) else item for item in row]
                                   for row in attestations]
        request['submit_calldata'] = s.submit_calldata(h, s.raw(request['header_signature'], 64),
                    s.raw(request['validity_proof']), attestations, request['threshold'])
        result = s.register(rpc, request)
        require(result['progress_status'] == 4, 'invalid real certificate accepted: ' + mode)
        expected = 'unbonded guarantor' if mode == 'unbonded' else 'stale attestedAt'
        require(result['progress_error'] == expected, 'wrong certificate refusal: ' + mode)
    members = [{'guarantor_id': row[7], 'signer': row[14]} for row in original['attestations']]
    query = dict(original, epoch=h[2], guarantors=members)
    observed = s.membership(rpc, query)['block_number']
    historical = s.membership(rpc, query, hex(observed))
    require(historical['block_number'] == observed, 'historical membership observation changed')
    for index, block in enumerate((0, -1, True, '1', 1.5, 2 ** 64)):
        source = runtime.directory / ('invalid-membership-' + str(index) + '.json')
        destination = runtime.directory / ('invalid-membership-' + str(index) + '.result.json')
        source.write_text(json.dumps(dict(query, observed_block_number=block)))
        refused = subprocess.run([sys.executable, str(ROOT / 'cmd/layerx-guarantor/settlement.py'),
                    'membership', str(source), str(destination)], capture_output=True, timeout=30)
        require(refused.returncode != 0 and b'membership observation block invalid' in refused.stderr,
                'invalid historical membership block accepted')
    require(int(rpc.call('eth_getTransactionCount', [account.address, 'latest']), 16) == nonce,
            'invalid request broadcast a transaction')


def scenario(directory, bundle, client, window, name):
    s = load_settlement()
    runtime = Runtime(directory, bundle, client, window)
    proxy = None
    try:
        runtime.generate()
        require(json.loads((runtime.directory / 'anchor.json').read_text())['params']['challenge_window_seconds'] == window,
                'selected challenge window differs from actual genesis')
        proxy = Transport(runtime.rpc_url)
        environments, account = producers(runtime, proxy, s, 3000 if name == 'deadline' else 180_000)
        runtime.invoke('register', 0, 'signed-register')
        rpc = s.RPC(runtime.rpc_url)
        competitor = Account.create()
        if name == 'reverted-finalize':
            transaction(s, rpc, account, '0x', value=10 ** 20, target=competitor.address)
        nonce = int(rpc.call('eth_getTransactionCount', [account.address, 'latest']), 16)
        if window:
            proxy.barrier()
        launch(runtime, environments, 'before-feedback' if name == 'retry-boundaries' else None)
        if window:
            wait(proxy.entered.is_set, 'submit broadcast barrier')
            requests = list(runtime.directory.glob('producer-*/settlement-*/request.json'))
            captured = next((json.loads(path.read_text()) for path in requests if 'submit_calldata' in json.loads(path.read_text())), None)
            require(captured is not None, 'real producer registration request missing')
            kill_producers(runtime)
            proxy.release.set()
            record = wait(lambda: s.anchor_checkpoint(rpc, 1)[1], 'submitted keeper record')
            require(record[12] == s.STATUS_SUBMITTED, 'nonzero window did not stay pending')
            checkpoint = record[1]
            initial = journals(runtime)
            require(any('submit' in row for row in initial) and all(row['submit']['transaction_id'] in proxy.transactions for row in initial if 'submit' in row),
                    'submission hash not durable before response')
            shared = list(runtime.directory.glob('submitter.lock.checkpoint-*-submit.json'))
            require(len(shared) == 1, 'one shared signed submission journal required')
            signed = json.loads(shared[0].read_text())
            require(s.keccak(s.raw(signed['raw_transaction'])) == s.raw(signed['transaction_id'], 32),
                    'signed submission journal hash differs')
            require(s.Account.recover_transaction(s.raw(signed['raw_transaction'])).lower() == account.address.lower(),
                    'signed submission journal signer differs')
            require(all(row['submit']['transaction_id'] == signed['transaction_id'] for row in initial if 'submit' in row),
                    'producers used different signed submission transactions')
            require(not list(runtime.directory.glob('producer-*/*.feedback')), 'pending checkpoint sent feedback')
            if name == 'retry-boundaries':
                negative_requests(s, rpc, captured, runtime, account)
                reverted = transaction(s, rpc, account, s.calldata('finalize(uint64)', ('uint64',), (1,)), success=False)
                require(not reverted['logs'], 'reverted finalize leaked logs')
                proxy.drop = 1
                launch(runtime, environments, 'before-feedback')
                wait(lambda: any(row['phase'] == 'submitted' for row in journals(runtime)), 'durable pending state')
                kill_producers(runtime)
                require(s.anchor_checkpoint(rpc, 1)[1][1] == checkpoint, 'restart changed checkpoint')
                proxy.barrier()
                launch(runtime, environments, 'before-feedback')
                wait(proxy.entered.is_set, 'finalize broadcast barrier', window + 120)
                kill_producers(runtime)
                proxy.release.set()
                wait(lambda: s.anchor_checkpoint(rpc, 1)[0] == s.STATUS_FINAL, 'real finalization')
                launch(runtime, environments, 'before-feedback')
                wait(lambda: stopped(runtime), 'before feedback crash boundary')
                feedback = {str(p): p.read_bytes() for p in runtime.directory.glob('producer-*/*.feedback')}
                require(feedback and not list(runtime.directory.glob('producer-*/*.feedback-done')), 'pre-feedback boundary')
                kill_producers(runtime)
                launch(runtime, environments, 'after-feedback-before-done')
                wait(lambda: stopped(runtime), 'confirmed feedback crash boundary')
                kill_producers(runtime)
                launch(runtime, environments)
                require(all(p.read_bytes() == feedback[str(p)] for p in runtime.directory.glob('producer-*/*.feedback')),
                        'feedback bytes changed on restart')
            elif name == 'reverted-finalize':
                proxy.barrier(before=True)
                launch(runtime, environments)
                wait(proxy.entered.is_set, 'signed finalize before broadcast', window + 120)
                transaction(s, rpc, competitor, s.calldata('finalize(uint64)', ('uint64',), (1,)))
                proxy.release.set()
            elif name == 'challenged':
                evidence = hashlib.sha256(b'real checkpoint retry challenge').digest()
                transaction(s, rpc, account, s.calldata('openChallenge(uint64,uint8,bytes32)',
                            ('uint64', 'uint8', 'bytes32'), (1, 0, evidence)), value=1_000_000 * s.UNIT_WEI)
                launch(runtime, environments)
            else:
                time.sleep(3.1)
                launch(runtime, environments)
        results = [runtime.processes['producer-' + str(i)].wait(timeout=220) for i in (1, 2)]
        if name == 'reverted-finalize':
            errors = [row for row in journals(runtime) if row['progress_status'] == 4]
            require(errors and any(code != 0 for code in results), 'reverted producer finalize did not halt')
            for row in errors:
                require('finalize' in row and row['progress_error'] == 'finalization transaction failed', 'wrong finalize refusal')
                receipt = rpc.call('eth_getTransactionReceipt', [row['finalize']['transaction_id']])
                require(receipt is not None and int(receipt['status'], 16) == 0, 'genuine reverted finalize receipt required')
            for path in runtime.directory.glob('producer-*/checkpoint-progress-*.json'):
                row = json.loads(path.read_text())
                if row['progress_status'] == 4:
                    require(not list(path.parent.glob('*.feedback')), 'reverted producer sent final feedback')
        elif name in ('challenged', 'deadline'):
            require(all(code != 0 for code in results), 'terminal checkpoint refusal did not halt both producers')
            wanted = 3 if name == 'challenged' else 4
            require(journals(runtime) and all(row['progress_status'] == wanted for row in journals(runtime)), 'durable terminal status')
            require(not list(runtime.directory.glob('producer-*/*.feedback')), 'refused checkpoint sent feedback')
            require(s.anchor_checkpoint(rpc, 1)[0] == s.STATUS_SUBMITTED, 'refused checkpoint finalized')
        else:
            require(results == [0, 0], 'real producers failed: ' + repr(results))
            require(s.anchor_checkpoint(rpc, 1)[0] == s.STATUS_FINAL, 'producer claimed final before anchor')
            done = list(runtime.directory.glob('producer-*/*.feedback-done'))
            require(len(done) == 2 and done[0].read_bytes() == done[1].read_bytes(), 'confirmed checkpoint differs')
            expected = 3 if window else 1
            require(int(rpc.call('eth_getTransactionCount', [account.address, 'latest']), 16) == nonce + expected,
                    'retry submitted duplicate chain transactions')
            saved = {str(p): p.read_bytes() for p in runtime.directory.glob('producer-*/*.feedback')}
            for key in ('producer-1', 'producer-2'):
                del runtime.processes[key]
            runtime.stop_role('sequencer', kill=True)
            runtime.start_role('sequencer')
            runtime.readiness()
            launch(runtime, environments)
            require([runtime.processes['producer-' + str(i)].wait(timeout=120) for i in (1, 2)] == [0, 0],
                    'same-disk daemon/producer final recovery')
            require(all(Path(path).read_bytes() == raw for path, raw in saved.items()), 'durable final feedback altered')
            require(int(rpc.call('eth_getTransactionCount', [account.address, 'latest']), 16) == nonce + expected,
                    'completed restart submitted another transaction')
        fixture.write_json(runtime.directory / 'checkpoint-result.json', {'scenario': name, 'window_seconds': window,
                           'checkpoint_status': s.anchor_checkpoint(rpc, 1)[0], 'producer_exit_codes': results})
    finally:
        if proxy:
            proxy.close()
        runtime.cleanup()


def main():
    global ROOT
    require(len(sys.argv) == 3 and sys.argv[1] == '--worker', 'invoke through paxeer_x_checkpoint_retry.py')
    for name in ('net', 'pid', 'mnt'):
        require(os.readlink('/proc/self/ns/' + name) != os.environ['PAXEER_X_PARENT_' + name.upper()], 'namespace isolation required')
    value = manifest()
    bundle = fixture.artifacts(os.environ.get('PAXEER_X_RUNTIME_ARTIFACTS', ''))
    client = fixture.client_artifact(os.environ.get('PAXEER_X_RUNTIME_CLIENT_MANIFEST', ''))
    bundle['artifacts']['layerx-guarantor'] = value['artifacts']['guarantor']
    fixture.run(['ip', 'link', 'set', 'lo', 'up'])
    fixture.run(['mount', '-t', 'tmpfs', '-o', 'mode=0755,nosuid,nodev', 'checkpoint-retry', '/tmp'])
    for target, source in (('/tmp/retry-source', ROOT), ('/tmp/retry-python', sys.prefix)):
        Path(target).mkdir(mode=0o755)
        fixture.run(['mount', '--bind', source, target])
        fixture.run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', target])
    ROOT = fixture.ROOT = Path('/tmp/retry-source')
    os.environ['PATH'] = '/tmp/retry-python/bin:/usr/local/bin:/usr/bin:/bin'
    directory = Path(sys.argv[2])
    directory.mkdir(mode=0o700)
    os.chown(directory, fixture.UID, fixture.UID)
    names = []
    for name, window in [('zero-window', 0), ('retry-boundaries', 50), ('challenged', 50), ('deadline', 50), ('reverted-finalize', 50)]:
        scenario(directory / name, bundle, client, window, name)
        names.append(name)
    fixture.write_json(directory / 'result.json', {'scenarios': names})


if __name__ == '__main__':
    os.umask(0o077)
    main()
