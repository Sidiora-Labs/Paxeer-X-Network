import hashlib
import json
import os
from pathlib import Path
import resource
import socket
import struct
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[4]
sys.path.insert(0, str(ROOT / 'tests/daemon'))
import paxeer_x_runtime_fixture as fixture
import paxeer_x_finality_fixture as finality

MAX_OBJECT = 64 * 1024 * 1024


def require(value, reason):
    fixture.require(value, 'caps: ' + reason)


def number(data, start, width):
    return int.from_bytes(data[start:start + width], 'big')


class Wire:
    def __init__(self, path, public):
        self.socket = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.socket.settimeout(65)
        self.socket.connect(str(path))
        tag, payload = self.exchange(1, b'', 0)
        require(tag == 2 and len(payload) >= 93, 'real NodeInfo')
        require(payload[:10] == struct.pack('>HHHI', 1, 8, 3, 77), 'NodeInfo domain')
        require(payload[59:91] == bytes.fromhex(public), 'genesis sequencer pin')
        self.sequence, self.batch = number(payload, 11, 8), number(payload, 19, 8)
        count, offset, capabilities = number(payload, 91, 2), 93, []
        for _ in range(count):
            length = number(payload, offset, 2); offset += 2
            require(length and offset + length <= len(payload), 'capability bounds')
            capabilities.append(payload[offset:offset + length].decode('ascii')); offset += length
        require(offset == len(payload) and 'caps_discovery' in capabilities, 'caps capability')

    def close(self):
        self.socket.close()

    def read(self, count):
        result = b''
        while len(result) < count:
            chunk = self.socket.recv(count - len(result))
            if not chunk: raise ConnectionResetError('native peer closed')
            result += chunk
        return result

    def exchange(self, tag, payload, correlation=42, minor=8):
        frame = struct.pack('>HHHQI', 1, minor, tag, correlation, len(payload)) + payload + bytes(4)
        self.socket.sendall(struct.pack('>I', len(frame)) + frame)
        length = number(self.read(4), 0, 4)
        require(22 <= length <= 2 * 1024 * 1024, 'bounded native frame')
        frame = self.read(length)
        major, revision, response_tag, ident, size = struct.unpack('>HHHQI', frame[:18])
        require((major, revision, ident) == (1, 8, correlation), 'response identity')
        require(18 + size + 4 == len(frame) and frame[-4:] == bytes(4), 'empty proof frame')
        return response_tag, frame[18:18 + size]


def request(did, page=4096, rank=3, selector=1, selected=bytes(32)):
    return bytearray(struct.pack('>HBI', 1, 0, 77) + bytes.fromhex(did) + bytes([selector]) + selected +
                     bytes([rank]) + struct.pack('>I', page) + bytes(100))


def page(connection, selection):
    tag, payload = connection.exchange(42, selection)
    require(tag == 43, 'native caps refused tag=' + str(tag) + ' code=' + payload.hex())
    require(len(payload) >= 119 and payload[:2] == b'\0\1', 'page version')
    offset, total, next_offset = struct.unpack('>III', payload[70:82])
    length = number(payload, 115, 4)
    require(0 < length <= number(selection, 73, 4) and length == len(payload) - 119,
            'positive bounded page')
    require(0 < total <= MAX_OBJECT and next_offset == offset + length <= total,
            'page range')
    require(payload[82] in (0, 1) and (payload[82] == 1) == (next_offset == total), 'terminal page')
    require((payload[83:115] == bytes(32)) == (payload[82] == 1), 'terminal cursor')
    require(payload[2:34] != bytes(32) and payload[38:70] != bytes(32), 'snapshot root')
    if selection[2]:
        require(payload[2:34] == selection[77:109] and payload[38:70] == selection[109:141]
                and offset == number(selection, 141, 4), 'immutable continuation')
    else:
        require(offset == 0, 'initial offset')
    continuation = bytearray(selection)
    continuation[2] = 1
    continuation[77:109] = payload[2:34]
    continuation[109:141] = payload[38:70]
    continuation[141:145] = payload[78:82]
    continuation[145:177] = payload[83:115]
    return payload, continuation


def finish(connection, selection, first=None):
    payload, selection = first or page(connection, selection)
    identity, root, total = payload[2:34], payload[38:70], number(payload, 74, 4)
    chunks = [payload[119:]]
    while payload[82] == 0:
        payload, selection = page(connection, selection)
        require(payload[2:34] == identity and payload[38:70] == root and number(payload, 74, 4) == total,
                'immutable snapshot chain')
        chunks.append(payload[119:])
    result = b''.join(chunks)
    require(len(result) == total, 'complete object')
    return result, root.hex(), selection


def receipt_rows(output):
    return [dict(field.split('=', 1) for field in line.split()[1:])
            for line in output.decode().splitlines() if line.startswith('receipt ')]


def fund(runtime, native, actor, sequence):
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
    d = runtime.directory
    public = Ed25519PrivateKey.from_private_bytes((d / ('keys/' + actor + '.seed')).read_bytes()).public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
    account = b'agent:did:layerx:' + public.hex().encode() + b':main'
    beneficiary = hashlib.sha256(b'LX:ACCOUNT:v1' + len(account).to_bytes(4, 'big') + account).digest()
    operation = 'credit-bob' if actor == 'bob' else 'credit-owner'
    runtime.produce(operation + '-deposit', [sys.executable, fixture.ROOT / 'platform/hosted/paxeer/evm.py', 'send',
        '--rpc', runtime.rpc_url, '--chain', '125', '--key-file', d / 'keys/deployer.key', '--value', str(1000000 * 10**12),
        '0x0000000000000000000000000000000000001013', 'deposit(bytes32)', '0x' + beneficiary.hex()])
    deposited = json.loads((d / (operation + '-deposit.log')).read_text())
    require(int(deposited['status'], 16) == 1, 'real fee deposit')
    logs = [row for row in deposited['logs'] if row['address'].lower() == '0x0000000000000000000000000000000000001013' and len(row['topics']) == 4]
    require(len(logs) == 1, 'unique custody event')
    runtime.wait(lambda: int(runtime.rpc('eth_blockNumber'), 16) >= int(deposited['blockNumber'], 16) + 2)
    credit = d / (operation + '.credit')
    runtime.produce(operation + '-proof', [runtime.binary('layerx-custody-proof'), 'light-credit', '--rpc',
        'http://127.0.0.1:' + str(runtime.ports[2]), '--profile', d / 'custody.profile', '--deposit-id',
        logs[0]['topics'][1], '--owner-key', '0x' + public.hex(), '--output', credit])
    env = runtime.env | {'PAXEER_X_FIXTURE_KEYS': str(d / 'keys'), 'BUDGET_CREDIT_PROFILE': str(d / 'custody.profile'),
        'BUDGET_CREDIT_FILE': str(credit)}
    result = subprocess.run([native, str(d / 'run/layerxd.lni.sock'), str(d / 'salt'), operation,
        str(sequence), '0', str(d / (operation + '.activity')), '0'], env=env, capture_output=True, timeout=60)
    require(result.returncode == 0 and len(receipt_rows(result.stdout)) == 1, 'native credit receipt')


class CapsRuntime(fixture.RuntimeFixture):
    def produce(self, label, argv, env=None, timeout=120):
        if label == 'bootstrap':
            from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
            from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
            authority = Ed25519PrivateKey.from_private_bytes((self.directory / 'keys/treasury.seed').read_bytes()).public_key().public_bytes(Encoding.Raw, PublicFormat.Raw).hex()
            argv = [*argv, '--handover-authority', authority]
        return super().produce(label, argv, env, timeout)


def run(directory, manifest, native, rust):
    bundle = fixture.artifacts(os.environ['PAXEER_X_RUNTIME_ARTIFACTS'])
    require(fixture.digest(bundle['artifacts']['layerxd']['path']) == manifest['artifacts']['layerxd']['sha256'],
            'runtime must use this task native producer')
    client = {'version': 1, 'source_revision': manifest['revision'], 'path': native,
              'sha256': fixture.digest(native)}
    runtime = CapsRuntime(directory, bundle, client)
    runtime.env['MALLOC_MMAP_THRESHOLD_'] = '16384'
    producer = None
    cases, captures = [], []
    try:
        runtime.generate()
        runtime.stop_role('sequencer')
        env_path = runtime.directory / 'node/sequencer.env'
        lines = env_path.read_text().splitlines()
        require(sum(line.startswith('LAYERX_NODE_LNI_DEADLINE_MS=') for line in lines) == 1, 'declared fixture deadline')
        env_path.write_text('\n'.join('LAYERX_NODE_LNI_DEADLINE_MS=60000' if line.startswith('LAYERX_NODE_LNI_DEADLINE_MS=') else line for line in lines) + '\n')
        runtime.start_role('sequencer'); runtime.readiness()
        d, public = runtime.directory, runtime.manifest['sequencer_public_key']
        socket_path = d / 'run/layerxd.lni.sock'
        from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
        from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
        def did(actor):
            key = Ed25519PrivateKey.from_private_bytes((d / ('keys/' + actor + '.seed')).read_bytes()).public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
            text = b'did:layerx:' + key.hex().encode()
            return hashlib.sha256(b'LXP/v1/did-id\0' + len(text).to_bytes(2, 'big') + text).hexdigest()
        owner, foreign = did('treasury'), did('bob')
        def asset(operation, seq, variant=1):
            old = runtime.env.get('CAPS_GRANT_VARIANT')
            runtime.env['CAPS_GRANT_VARIANT'] = str(variant)
            result = runtime.invoke(operation, seq, 'caps-' + operation + '-' + str(seq))
            if old is None: runtime.env.pop('CAPS_GRANT_VARIANT')
            else: runtime.env['CAPS_GRANT_VARIANT'] = old
            require(len(receipt_rows(result.stdout)) == (20 if operation == 'sends' else 1), 'signed mutation receipts')
        def budget(operation, sequence, ident, actor='treasury', amount=5, source=1):
            env = runtime.env | {'PAXEER_X_FIXTURE_KEYS': str(d / 'keys'), 'BUDGET_SOURCE_SEQUENCE': str(source),
                'CAPS_BUDGET_OWNER': actor, 'CAPS_BUDGET_ID': str(ident)}
            result = subprocess.run([native, str(socket_path), str(d / 'salt'), operation, str(sequence),
                str(amount), str(d / (operation + '-' + actor + '-' + str(sequence) + '.activity')), '0'],
                env=env, capture_output=True, timeout=60)
            (d / (operation + '-' + actor + '-' + str(sequence) + '.log')).write_bytes(result.stdout + result.stderr)
            require(result.returncode == 0 and len(receipt_rows(result.stdout)) == 1, 'real budget mutation')
        def save(label, connection, selection, first=None, budgets=0, grants=0):
            value, root, last = finish(connection, selection, first)
            path = d / (label + '.caps'); path.write_bytes(value)
            captures.append({'name': label, 'path': str(path), 'root': root, 'did': selection[7:39].hex(),
                'head_sequence': connection.sequence, 'head_batch': connection.batch, 'budgets': budgets, 'grants': grants,
                'rank': selection[72], 'selector': selection[39], 'selected': selection[40:72].hex()})
            connection.close()
            return last, root
        for op, seq in [('register', 0), ('open', 1), ('open-bob', 0), ('mint', 2), ('burn', 3)]: asset(op, seq)
        connection = Wire(socket_path, public)
        save('empty-prefix-and-module', connection, request(owner))
        cases += ['real-empty-module', 'real-empty-prefix']
        asset('grant-issue', 4); asset('grant-revoke', 5); asset('sends', 6)
        fund(runtime, native, 'treasury', 26); fund(runtime, native, 'bob', 1)
        budget('create', 27, 1, source=21)
        budget('create', 2, 2, actor='bob')
        budget('create', 28, 3, source=22)
        asset('grant-issue', 29, 2); asset('grant-issue-bob', 3, 3); asset('grant-issue', 30, 4)
        connection = Wire(socket_path, public)
        _, initial_root = save('populated-owner', connection, request(owner), budgets=2, grants=3)
        connection = Wire(socket_path, public)
        save('populated-foreign', connection, request(foreign), budgets=1, grants=1)
        cases += ['real-mixed-owner-budgets', 'real-mixed-owner-grants']
        connection = Wire(socket_path, public); selected = request(owner, page=512)
        first = page(connection, selected); require(first[0][82] == 0, 'multi-page fixture')
        budget('spend', 31, 1, amount=1)
        _, retained_root = save('retained-before-mutation', connection, selected, first, 2, 3)
        require(retained_root == initial_root, 'old immutable root')
        connection = Wire(socket_path, public)
        _, fresh_root = save('fresh-after-mutation', connection, request(owner), budgets=2, grants=3)
        require(fresh_root != retained_root, 'fresh observation')
        cases += ['mutation-during-traversal', 'fresh-after-mutation']
        connection = Wire(socket_path, public); selected = request(owner, page=512); first = page(connection, selected)
        asset('grant-revoke', 32, 2)
        save('retained-before-revocation', connection, selected, first, 2, 3)
        cases.append('revocation-during-traversal')
        connection = Wire(socket_path, public); selected = request(owner, page=512); first = page(connection, selected)
        time.sleep(31)
        budget('spend', 33, 3, amount=1)
        save('retained-before-rollover', connection, selected, first, 2, 3)
        cases.append('period-rollover-during-traversal')
        connection = Wire(socket_path, public)
        save('fresh-after-rollover', connection, request(owner), budgets=2, grants=3)
        for label, at in [('foreign-object', 77), ('wrong-root', 109), ('wrong-selection', 7),
                          ('cursor-gap', 144), ('malformed-cursor', 145), ('wrong-network', 6)]:
            connection = Wire(socket_path, public)
            _, continuation = page(connection, request(owner, page=512))
            continuation[at] ^= 1
            tag, _ = connection.exchange(42, continuation)
            require(tag == 25, label + ' refused'); connection.close(); cases.append(label)
        for label, change in [('zero-page', lambda p: p.__setitem__(slice(73, 77), bytes(4))),
                              ('oversize-page', lambda p: p.__setitem__(slice(73, 77), (1048577).to_bytes(4, 'big'))),
                              ('wrong-rank', lambda p: p.__setitem__(72, 5)),
                              ('malformed-request', lambda p: p.pop())]:
            connection = Wire(socket_path, public); selected = request(owner); change(selected)
            tag, _ = connection.exchange(42, selected); require(tag == 25, label); connection.close(); cases.append(label)
        connection = Wire(socket_path, public)
        _, continuation = page(connection, request(owner, page=512)); duplicate = bytes(continuation)
        page(connection, continuation)
        tag, _ = connection.exchange(42, duplicate); require(tag == 25, 'cursor replay'); connection.close(); cases.append('cursor-replay')
        connection = Wire(socket_path, public)
        _, continuation = page(connection, request(owner, page=512)); connection.close()
        connection = Wire(socket_path, public)
        tag, _ = connection.exchange(42, continuation); require(tag == 25, 'disconnect cursor'); connection.close(); cases.append('disconnect')
        connection = Wire(socket_path, public)
        _, continuation = page(connection, request(owner, page=512)); connection.close()
        runtime.restart(kill=True)
        connection = Wire(socket_path, public)
        tag, _ = connection.exchange(42, continuation); require(tag == 25, 'restart cursor'); connection.close(); cases.append('restart')
        connection = Wire(socket_path, public)
        save('fresh-after-restart', connection, request(owner), budgets=2, grants=3)
        cases.append('fresh-after-restart')
        producer = finality.FinalityProducer(runtime, finality.supplemental(os.environ['PAXEER_X_FINALITY_ARTIFACTS']))
        producer.bond(); producer.tls(); producer.start()
        connection = Wire(socket_path, public); batch = connection.batch; connection.close()
        producer.registered(batch)
        provenance = producer.provenance(batch)
        selected = request(owner, rank=4, selector=3, selected=bytes.fromhex(provenance['checkpoint_id']))
        connection = Wire(socket_path, public)
        save('real-finality', connection, selected, budgets=2, grants=3)
        cases.append('real-finality-authority')
        connection = Wire(socket_path, public)
        _, continuation = page(connection, request(owner, page=512))
        time.sleep(61)
        try:
            tag, reason = connection.exchange(42, continuation)
            require(tag == 25 and reason == bytes([4]) + (-303).to_bytes(4, 'big', signed=True), 'expired object refused')
        except (ConnectionResetError, BrokenPipeError):
            pass
        connection.close(); cases.append('expiry')
        retained = []
        try:
            for _ in range(4):
                owned = Wire(socket_path, public)
                first, _ = page(owned, request(owner, page=1))
                require(first[82] == 0, 'retained active object')
                retained.append(owned)
            excess = Wire(socket_path, public)
            tag, reason = excess.exchange(42, request(owner, page=1))
            require(tag == 25 and reason == bytes([4]) + (-5).to_bytes(4, 'big', signed=True), 'active object cap refused'); excess.close()
        finally:
            for owned in retained: owned.close()
        cases.append('active-object-exhaustion')
        vm = next(line for line in Path('/proc/' + str(runtime.processes['sequencer'].pid) + '/status').read_text().splitlines() if line.startswith('VmSize:'))
        current = int(vm.split()[1]) * 1024
        pid = runtime.processes['sequencer'].pid
        prior = resource.prlimit(pid, resource.RLIMIT_AS)
        connection = Wire(socket_path, public)
        try:
            resource.prlimit(pid, resource.RLIMIT_AS, (current, prior[1]))
            tag, reason = connection.exchange(42, request(owner))
            require(tag == 25 and reason == bytes([4]) + (-905).to_bytes(4, 'big', signed=True), 'snapshot allocation exhaustion must refuse')
        finally:
            resource.prlimit(pid, resource.RLIMIT_AS, prior); connection.close()
        cases.append('snapshot-memory-exhaustion')
        fixture.write_json(d / 'caps-inputs.json', {'revision': manifest['revision'], 'socket': str(socket_path),
            'captures': captures, 'sequencer_public_key': public,
            'sequencer_id': hashlib.sha256(('layerx-sequencer:' + public).encode()).hexdigest(),
            'genesis_trust': str(d / 'node/genesis/genesis-handover-trust.lxt'),
            'genesis_manifest': str(d / 'node/genesis/genesis.manifest'),
            'genesis_descriptor': str(d / 'node/genesis/paxeer-deployment-descriptor.lxgd'),
            'handover_authority_public_key': next(line.split('=', 1)[1] for line in (d / 'node/node.env').read_text().splitlines() if line.startswith('LAYERX_NODE_HANDOVER_AUTHORITY_PUBLIC_KEY=')),
            'runtime_cases': cases,
            'finality_provenance': provenance})
        env = runtime.env | {'LAYERX_CAPS_DISCOVERY_INPUTS': str(d / 'caps-inputs.json')}
        result = subprocess.run([rust, '--test-threads=1', '--nocapture'], cwd=ROOT / 'agent', env=env,
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=300)
        (d / 'caps-rust.log').write_bytes(result.stdout)
        print(result.stdout.decode(), end='')
        require(result.returncode == 0, 'independent Rust acceptance')
        fixture.write_json(d / 'caps-result.json', {'revision': manifest['revision'], 'runtime_cases': cases,
            'captures': len(captures), 'rust_exit': result.returncode, 'skipped': 0, 'log': str(d / 'caps-rust.log')})
        return d / 'caps-result.json'
    finally:
        if producer is not None: producer.cleanup()
        runtime.cleanup()
