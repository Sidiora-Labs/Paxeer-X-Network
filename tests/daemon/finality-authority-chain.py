#!/usr/bin/env python3
import copy
import http.server
import hashlib
import json
import os
from pathlib import Path
import signal
import stat
import subprocess
import sys
import threading
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
FIXTURE = ROOT / 'tests/daemon/fixtures/finality-authority/rpc.json'
ANCHOR = '0x' + '0' * 36 + '1014'
THRESHOLD = '0x42cde4e8'
LATEST_FINALIZED = '0x6cdd45ae'
CHECKPOINT_GUARANTORS = '0x8ea69468'
GUARANTOR = '0xb3fc9298'
REFUSED = 3
PINS = ('LAYERX_NODE_PAXEER_RPC_URL', 'LAYERX_NODE_PAXEER_RPC_ADDRESS', 'LAYERX_NODE_PAXEER_RPC_PORT',
        'LAYERX_NODE_PAXEER_CHAIN_ID', 'LAYERX_NODE_SETTLEMENT_CONTRACT', 'LAYERX_NODE_CHECKPOINT_REGISTRY')


def key(method, params):
    return json.dumps([method, params], sort_keys=True)


class Server:
    def __init__(self, answer):
        outer = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                request = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
                reply = {'jsonrpc': '2.0', 'id': request['id']}
                reply.update(answer(request['method'], request['params']))
                body = json.dumps(reply).encode()
                self.send_response(200)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                self.wfile.write(body)
                outer.requests.append(key(request['method'], request['params']))

            def log_message(self, *_args):
                pass

        self.requests = []
        self.httpd = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.url = 'http://127.0.0.1:%d' % self.httpd.server_address[1]
        threading.Thread(target=self.httpd.serve_forever, daemon=True).start()

    def close(self):
        self.httpd.shutdown()
        self.httpd.server_close()


def replay(exchanges):
    table = {key(item['method'], item['params']): item['result'] for item in exchanges}
    return Server(lambda method, params: {'result': table[key(method, params)]}
                  if key(method, params) in table else
                  {'error': {'code': -32000, 'message': 'not recorded'}})


def bind(binary, url, chain_id, **extra):
    env = {name: value for name, value in os.environ.items() if name not in PINS}
    env.update(LAYERX_NODE_PAXEER_RPC_URL=url, LAYERX_NODE_PAXEER_CHAIN_ID=str(chain_id), **extra)
    return subprocess.run([str(binary), 'bind'], cwd=ROOT, env=env, capture_output=True, text=True, timeout=120)


def refused(binary, url, chain_id, name, **extra):
    result = bind(binary, url, chain_id, **extra)
    assert result.returncode == REFUSED, (name, result.returncode, result.stdout, result.stderr)


def call(item, selector):
    return (item['method'] == 'eth_call' and item['params'][0]['to'].lower() == ANCHOR and
            item['params'][0]['data'].startswith(selector))


def words(result):
    body = result[2:]
    assert len(body) % 64 == 0 and body, result
    return [int(body[i:i + 64], 16) for i in range(0, len(body), 64)]


def expected(fixture):
    exchanges = fixture['exchanges']
    chain = [item for item in exchanges if item['method'] == 'eth_chainId']
    threshold = [item for item in exchanges if call(item, THRESHOLD)]
    latest = [item for item in exchanges if call(item, LATEST_FINALIZED)]
    assert len(chain) == 1 and int(chain[0]['result'], 16) == fixture['chain_id']
    assert len(threshold) == 1 and len(latest) == 1
    batch, exists = words(latest[0]['result'])
    guarantors = 0
    if exists:
        lists = [item for item in exchanges if call(item, CHECKPOINT_GUARANTORS)]
        assert len(lists) == 1 and int(lists[0]['params'][0]['data'][10:], 16) == batch
        guarantors = words(lists[0]['result'])[1]
    return {'chain_id': fixture['chain_id'], 'threshold': words(threshold[0]['result'])[0],
            'finalized_exists': bool(exists), 'finalized_batch': batch, 'finalized_guarantors': guarantors}


def tampered(fixture, selector, change):
    exchanges = copy.deepcopy(fixture['exchanges'])
    matched = [item for item in exchanges if call(item, selector)]
    assert matched, selector
    change(matched[0])
    return exchanges



RECORDING_CASES = (
    'finalized-history', 'short-signer-list', 'unknown-signer', 'altered-checkpoint',
    'altered-receipt', 'wrong-domain', 'truncated-proof', 'corrupted-proof',
    'truncated-checkpoint', 'corrupted-checkpoint', 'wrong-network',
)
REQUIRED_RECORDING_CASES = RECORDING_CASES + ('membership-change-recovery', 'daemon-restart')


def exchange_digest(exchanges):
    return hashlib.sha256(json.dumps(exchanges, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


class Recorder:
    def __init__(self, upstream):
        self.exchanges = []
        self.lock = threading.Lock()

        def forward(method, params):
            request = json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params}).encode()
            with urllib.request.urlopen(urllib.request.Request(
                    upstream, request, {'Content-Type': 'application/json'}), timeout=30) as response:
                raw = response.read(16 * 1024 * 1024 + 1)
            if len(raw) > 16 * 1024 * 1024:
                raise RuntimeError('authentic RPC response exceeds recording bound')
            answer = json.loads(raw)
            if answer.get('jsonrpc') != '2.0' or answer.get('id') != 1:
                raise RuntimeError('authentic RPC envelope mismatch')
            if ('result' in answer) == ('error' in answer):
                raise RuntimeError('authentic RPC result/error envelope mismatch')
            row = {'method': method, 'params': copy.deepcopy(params)}
            if 'result' in answer:
                row['result'] = copy.deepcopy(answer['result'])
                forwarded = {'result': answer['result']}
            else:
                row['error'] = copy.deepcopy(answer['error'])
                forwarded = {'error': answer['error']}
            with self.lock:
                self.exchanges.append(row)
            return forwarded

        self.forward = forward
        self.server = Server(forward)
        self.url = self.server.url

    def rpc(self, method, params):
        response = self.forward(method, params)
        if 'error' in response:
            raise RuntimeError('authentic RPC query refused: ' + method)
        return response['result']

    def close(self):
        self.server.close()

    def snapshot(self):
        with self.lock:
            return copy.deepcopy(self.exchanges)


def _hex(value, length):
    if not isinstance(value, str) or len(value) != length or any(character not in '0123456789abcdef' for character in value):
        raise RuntimeError('recording digest/identity is not canonical hexadecimal')
    return value


def _block(value):
    if not isinstance(value, dict) or type(value.get('number')) is not int or not 0 <= value['number'] < 2 ** 64:
        raise RuntimeError('authentic capture block number required')
    digest = value.get('hash', '')
    if not digest.startswith('0x') or _hex(digest[2:], 64) == '0' * 64:
        raise RuntimeError('authentic nonzero capture block hash required')


def validate_recordings(path):
    path = Path(path).resolve(strict=True)
    info = path.stat()
    if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077 or info.st_uid != os.geteuid():
        raise RuntimeError('private authentic recording manifest required')
    value = json.loads(path.read_text())
    if value.get('version') != 1 or value.get('purpose') != 'finality-authority-recordings' \
            or value.get('chain_id') != 125 or value.get('anchor') != ANCHOR:
        raise RuntimeError('authentic finality recording domain mismatch')
    _hex(value.get('source_revision'), 40)
    _block(value.get('capture_block'))
    if set(value.get('required_cases', [])) != set(REQUIRED_RECORDING_CASES):
        raise RuntimeError('mandatory finality recording cases missing')
    producer = value.get('producer', {})
    if producer.get('version') != 1 or producer.get('purpose') != 'historical-finality-producer' \
            or producer.get('chain_id') != value['chain_id'] or producer.get('anchor') != ANCHOR \
            or producer.get('anchor_status') != 'FINAL' or producer.get('credentials_included') is not False:
        raise RuntimeError('genuine finalized producer provenance required')
    _hex(producer.get('source_revision'), 40)
    members = producer.get('checkpoint_guarantors', [])
    guarantors = producer.get('guarantors', [])
    if len(members) < 2 or members != sorted(set(members)) \
            or members != [row.get('guarantor_id') for row in guarantors]:
        raise RuntimeError('genuine finalized threshold membership missing')
    for member in members:
        _hex(member, 64)
    phases = value.get('phases', {})
    if set(phases) != {'before', 'after'}:
        raise RuntimeError('both authenticated membership phases required')
    captured_block = False
    for phase in phases.values():
        state = Path(phase.get('state_dir', ''))
        if not state.is_absolute() or not state.is_dir() or state.is_symlink():
            raise RuntimeError('authentic producer state directory missing')
        batch = phase.get('batch')
        checkpoint = _hex(phase.get('checkpoint_id'), 64)
        if type(batch) is not int or batch <= 0 or batch != producer.get('batch') \
                or checkpoint != producer.get('checkpoint_id'):
            raise RuntimeError('recorded checkpoint identity differs from producer provenance')
        exchanges = phase.get('exchanges')
        if not isinstance(exchanges, list) or not exchanges or exchange_digest(exchanges) != phase.get('exchange_sha256'):
            raise RuntimeError('authentic RPC exchange digest mismatch')
        table = {}
        authentic_chain = False
        authentic_receipt = False
        for row in exchanges:
            if not isinstance(row.get('method'), str) or not isinstance(row.get('params'), list) \
                    or ('result' in row) == ('error' in row):
                raise RuntimeError('authentic RPC recording shape invalid')
            name = key(row['method'], row['params'])
            response = {'result': row['result']} if 'result' in row else {'error': row['error']}
            if name in table and table[name] != response:
                raise RuntimeError('RPC facts changed within one authenticated capture phase')
            table[name] = response
            result = row.get('result')
            if row['method'] == 'eth_chainId' and result == hex(value['chain_id']):
                authentic_chain = True
            if row['method'] == 'eth_getBlockByNumber' and isinstance(result, dict):
                if result.get('number') == hex(value['capture_block']['number']) \
                        and result.get('hash') == value['capture_block']['hash']:
                    captured_block = True
            if row['method'] == 'eth_getTransactionReceipt' and isinstance(result, dict) \
                    and result.get('status') == '0x1' and len(row['params']) == 1 \
                    and result.get('transactionHash') == row['params'][0]:
                logs = result.get('logs', [])
                authentic_receipt = authentic_receipt or any(
                    isinstance(log, dict) and log.get('address', '').lower() == ANCHOR
                    and isinstance(log.get('topics'), list) and log['topics'] for log in logs)
        if not authentic_chain or not authentic_receipt:
            raise RuntimeError('actual chain domain and finalized submission receipt/event required in each phase')
        matching = [row for row in guarantors if Path(row.get('state', '')).resolve() == state.resolve()]
        if len(matching) != 1:
            raise RuntimeError('recorded state directory lacks authentic producer provenance')
        names = set()
        for row in matching[0].get('files', []):
            name = row.get('name', '')
            if Path(name).name != name or not name:
                raise RuntimeError('producer filename escaped private state')
            target = state / name
            if not target.is_file() or target.is_symlink() or target.stat().st_size != row.get('bytes') \
                    or hashlib.sha256(target.read_bytes()).hexdigest() != row.get('sha256'):
                raise RuntimeError('authentic producer file digest mismatch')
            names.add(name)
        required = {'%020d.checkpoint' % batch, '%020d.finality' % batch, checkpoint + '.header'}
        if not required <= names:
            raise RuntimeError('genuine checkpoint, finality and signed header files required')
    if not captured_block:
        raise RuntimeError('capture block/hash lacks authentic recorded RPC header')
    transition = value.get('membership_transition', {})
    if transition.get('guarantor_id') not in members or transition.get('eligible_after') is not False \
            or transition.get('still_final') is not True or transition.get('guarantors_unchanged') is not True:
        raise RuntimeError('actual membership-change transition provenance required')
    restart = value.get('restart', {})
    if any(restart.get(name) is not True for name in ('records_identical', 'bytes_identical', 'state_identical')):
        raise RuntimeError('actual daemon restart evidence required')
    return value


def replay_recordings(exchanges):
    table = {}
    for row in exchanges:
        table[key(row['method'], row['params'])] = {'result': row['result']} if 'result' in row else {'error': row['error']}
    return Server(lambda method, params: table[key(method, params)] if key(method, params) in table else
                  {'error': {'code': -32000, 'message': 'authentic call was not recorded'}})


def verify_recordings(binary, manifest):
    value = validate_recordings(manifest)
    observed = {}
    for name, expected_admission in (('before', 'admit'), ('after', 'refuse')):
        phase = value['phases'][name]
        server = replay_recordings(phase['exchanges'])
        try:
            environment = {key: entry for key, entry in os.environ.items() if key not in PINS}
            environment.update(LAYERX_NODE_PAXEER_RPC_URL=server.url, LAYERX_NODE_PAXEER_CHAIN_ID=str(value['chain_id']))
            command = [str(binary), 'recorded', phase['state_dir'], str(phase['batch']), phase['checkpoint_id'], expected_admission]
            result = subprocess.run(command, cwd=ROOT, env=environment, capture_output=True, text=True, timeout=300)
            if result.returncode != 0:
                raise RuntimeError('actual recorded finality callback refused phase ' + name + ': exit=' + str(result.returncode))
            markers = [line[len('FINALITY_RECORDINGS_CASES '):] for line in result.stdout.splitlines()
                       if line.startswith('FINALITY_RECORDINGS_CASES ')]
            if len(markers) != 1:
                raise RuntimeError('actual callback case accounting missing in ' + name)
            record = json.loads(markers[0])
            cases = record.get('cases', {})
            if set(cases) != set(RECORDING_CASES) or any(cases[case] is not True for case in RECORDING_CASES) \
                    or record.get('version') != 1 or record.get('batch') != phase['batch'] \
                    or record.get('network_id') != value['producer'].get('network_id') \
                    or record.get('checkpoint_id') != '0x' + phase['checkpoint_id'] \
                    or record.get('historical_status') != 0 or record.get('frontier_unchanged') is not True \
                    or record.get('settlement_contract') != ANCHOR or record.get('paxeer_chain_id') != 125:
                raise RuntimeError('mandatory actual callback identity/case failed in ' + name)
            fresh = record.get('fresh_status')
            if type(fresh) is not int or (name == 'before' and fresh != 0) or (name == 'after' and fresh == 0):
                raise RuntimeError('actual membership fresh-admission status mismatch in ' + name)
            _block({'number': record.get('observed_block'), 'hash': record.get('observed_block_hash')})
            for field in ('payload_sha256', 'proof_sha256', 'header_sha256', 'settlement_anchor'):
                digest = record.get(field, '')
                if not digest.startswith('0x') or _hex(digest[2:], 64) == '0' * 64:
                    raise RuntimeError('actual callback content digest missing in ' + name)
            expected = 'fresh admission passed status=0' if name == 'before' else 'fresh admission refused status='
            if not any(line.startswith('historical recovery passed') for line in result.stdout.splitlines()) \
                    or not any(line.startswith(expected) for line in result.stdout.splitlines()):
                raise RuntimeError('actual membership recovery/admission result missing in ' + name)
            observed[name] = record
        finally:
            server.close()
    return {'cases': list(REQUIRED_RECORDING_CASES), 'phases': observed, 'source_revision': value['source_revision']}


def capture(binary, upstream, chain_id):
    exchanges = []

    def forward(method, params):
        request = json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params}).encode()
        with urllib.request.urlopen(urllib.request.Request(
                upstream, request, {'Content-Type': 'application/json'}), timeout=10) as response:
            answer = json.loads(response.read())
        if 'result' in answer:
            exchanges.append({'method': method, 'params': params, 'result': answer['result']})
            return {'result': answer['result']}
        return {'error': answer.get('error', {'code': -32000, 'message': 'no result'})}

    server = Server(forward)
    try:
        result = bind(binary, server.url, chain_id)
    finally:
        server.close()
    assert result.returncode == 0, (result.returncode, result.stderr)
    FIXTURE.parent.mkdir(parents=True, exist_ok=True)
    FIXTURE.write_text(json.dumps({'chain_id': chain_id, 'anchor': ANCHOR, 'exchanges': exchanges},
                                  indent=1, sort_keys=True) + '\n')
    print(result.stdout.strip())


def main():
    arguments = sys.argv[1:]
    if arguments[:1] == ['--recordings']:
        if len(arguments) != 3:
            raise RuntimeError('--recordings requires the genuine private manifest and built native binary')
        binary = Path(arguments[2]).resolve(strict=True)
        if not binary.is_file():
            raise RuntimeError('actual finality callback binary missing')
        print(json.dumps(verify_recordings(binary, arguments[1]), sort_keys=True))
        return
    capturing = arguments[:1] == ['--capture']
    if capturing:
        arguments = arguments[1:]
    binary = Path(arguments[0] if arguments else ROOT / 'build/tests/lxp_test_daemon_finality_authority').resolve()
    if not binary.is_file():
        raise RuntimeError('finality-authority C fixture is not built: ' + str(binary))
    live = os.environ.get('LAYERX_NODE_PAXEER_RPC_URL', '')
    live_chain = int(os.environ.get('LAYERX_NODE_PAXEER_CHAIN_ID', '125'))
    if capturing:
        if not live:
            raise RuntimeError('--capture needs LAYERX_NODE_PAXEER_RPC_URL naming the node loopback JSON-RPC')
        capture(binary, live, live_chain)
        return
    if not FIXTURE.is_file():
        raise RuntimeError('recorded chain fixture is absent: ' + str(FIXTURE.relative_to(ROOT)) +
                           '; record it on a node with LAYERX_NODE_PAXEER_RPC_URL set by running this script with --capture')
    fixture = json.loads(FIXTURE.read_text())
    assert fixture['anchor'] == ANCHOR
    want = expected(fixture)
    server = replay(fixture['exchanges'])
    try:
        result = bind(binary, server.url, fixture['chain_id'])
        assert result.returncode == 0, (result.returncode, result.stderr)
        assert json.loads(result.stdout) == want, (result.stdout, want)
        assert sorted(set(server.requests)) == sorted(key(item['method'], item['params'])
                                                      for item in fixture['exchanges']), 'replay coverage'
        refused(binary, server.url, fixture['chain_id'] + 1, 'wrong chain')
        refused(binary, server.url.replace('127.0.0.1', '10.0.0.1'), fixture['chain_id'], 'remote rpc')
        refused(binary, server.url, fixture['chain_id'], 'solidity settlement',
                LAYERX_NODE_SETTLEMENT_CONTRACT='0x' + '11' * 20)
        refused(binary, server.url, fixture['chain_id'], 'port pin disagrees',
                LAYERX_NODE_PAXEER_RPC_PORT=str(server.httpd.server_address[1] + 1))
    finally:
        server.close()
    refused(binary, server.url, fixture['chain_id'], 'unreachable chain')
    cases = [('zero threshold', THRESHOLD, lambda item: item.update(result='0x' + '0' * 64))]
    if want['finalized_exists']:
        cases.append(('short guarantor list', CHECKPOINT_GUARANTORS,
                      lambda item: item.update(result='0x' + '%064x' % 32 + '0' * 64)))
        cases.append(('unknown guarantor', GUARANTOR,
                      lambda item: item.update(result='0x' + '0' * 64 + item['result'][66:])))
    for name, selector, change in cases:
        server = replay(tampered(fixture, selector, change))
        try:
            refused(binary, server.url, fixture['chain_id'], name)
        finally:
            server.close()
    print(json.dumps({'fixture': want}, sort_keys=True))
    if live:
        result = bind(binary, live, live_chain)
        assert result.returncode == 0, ('live loopback rpc', result.returncode, result.stderr)
        print(json.dumps({'live': json.loads(result.stdout)}, sort_keys=True))


def terminate(signum, _frame):
    raise SystemExit(128 + signum)


if __name__ == '__main__':
    signal.signal(signal.SIGTERM, terminate)
    signal.signal(signal.SIGINT, terminate)
    main()
