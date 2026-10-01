#!/usr/bin/env python3
"""Recovered finality is verified against the anchor's authenticated historical
membership record, while fresh admission keeps enforcing current eligibility.

The corpus is a real capture: run with --capture <phase> against the loopback
JSON-RPC of a Paxeer node whose anchor finalized the fixture checkpoint, once at
registration (phase "admission") and once after a later signer rotation, exit or
minimum-bond change (phase "rotated"). Replay never synthesizes a chain answer;
a missing recording, binary or exchange fails the gate.
"""
import copy
import http.server
import json
import os
from pathlib import Path
import shlex
import subprocess
import sys
import threading
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
BINARY = ROOT / 'build/tests/lxp_test_daemon_finality_authority'
ANCHOR = '0x' + '0' * 36 + '1014'
PINS = ('LAYERX_NODE_PAXEER_RPC_URL', 'LAYERX_NODE_PAXEER_RPC_ADDRESS', 'LAYERX_NODE_PAXEER_RPC_PORT',
        'LAYERX_NODE_PAXEER_CHAIN_ID', 'LAYERX_NODE_SETTLEMENT_CONTRACT', 'LAYERX_NODE_CHECKPOINT_REGISTRY')
PHASES = ('admission', 'rotated')


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


def recover(url, recording, phase):
    env = {name: value for name, value in os.environ.items() if name not in PINS}
    env.update(LAYERX_NODE_PAXEER_RPC_URL=url, LAYERX_NODE_PAXEER_CHAIN_ID=str(phase['chain_id']))
    result = subprocess.run([str(BINARY), 'recover', phase['transaction'], str(phase['block']),
                             str(recording / 'attestations')], cwd=ROOT, env=env,
                            capture_output=True, text=True, timeout=120)
    assert result.returncode == 0, ('recover', result.returncode, result.stdout, result.stderr)
    return json.loads(result.stdout)


def capture(recording, name):
    upstream = os.environ.get('LAYERX_NODE_PAXEER_RPC_URL', '')
    if not upstream:
        raise SystemExit('--capture needs LAYERX_NODE_PAXEER_RPC_URL naming the node loopback JSON-RPC')
    phase = {'chain_id': int(os.environ['LAYERX_NODE_PAXEER_CHAIN_ID']),
             'transaction': os.environ['PAXEER_X_REGISTRATION_TRANSACTION'],
             'block': int(os.environ['PAXEER_X_REGISTRATION_BLOCK'])}
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
        outcome = recover(server.url, recording, phase)
    finally:
        server.close()
    phase['exchanges'] = exchanges
    phase['outcome'] = outcome
    (recording / (name + '.json')).write_text(json.dumps(phase, indent=1, sort_keys=True) + '\n')
    print(json.dumps({name: outcome}, sort_keys=True))


def matching(exchanges, method, prefix=None):
    found = [item for item in exchanges if item['method'] == method and
             (prefix is None or item['params'][0]['data'].startswith(prefix))]
    assert found, (method, prefix)
    return found


def flip(text, index):
    digit = text[index]
    return text[:index] + ('1' if digit == '0' else '0') + text[index + 1:]


def tamper_block_hash(exchanges):
    item = matching(exchanges, 'eth_getBlockByNumber')[0]
    item['result']['hash'] = flip(item['result']['hash'], 10)


def tamper_guarantors(exchanges):
    for item in matching(exchanges, 'eth_call'):
        result = item['result']
        if item['params'][0]['data'].startswith(GUARANTORS):
            item['result'] = result[:-1] + ('1' if result[-1] == '0' else '0')
            return
    raise AssertionError('checkpointGuarantors exchange absent')


def tamper_signature(exchanges):
    item = matching(exchanges, 'eth_getTransactionByHash')[0]
    item['result']['input'] = item['result']['input'][:10]


def tamper_receipt_block(exchanges):
    item = matching(exchanges, 'eth_getTransactionReceipt')[0]
    item['result']['blockHash'] = flip(item['result']['blockHash'], 10)


def tamper_event(exchanges):
    item = matching(exchanges, 'eth_getTransactionReceipt')[0]
    for log in item['result']['logs']:
        log['data'] = flip(log['data'], 10)


def tamper_root(exchanges):
    for item in matching(exchanges, 'eth_call'):
        if len(item['result']) == 2 + 18 * 64:
            item['result'] = flip(item['result'], 2 + 64 * 7 + 10)
            return
    raise AssertionError('checkpoint exchange absent')


def tamper_status(exchanges):
    for item in matching(exchanges, 'eth_call'):
        if (len(item['params'][0]['data']) == 74 and not item['params'][0]['data'].startswith(GUARANTORS) and
                len(item['result']) == 66 and int(item['result'], 16) == 2):
            item['result'] = '0x' + '%064x' % 1
            return
    raise AssertionError('statusOf exchange absent')


def tamper_chain(exchanges):
    for item in matching(exchanges, 'eth_chainId'):
        item['result'] = hex(int(item['result'], 16) + 1)


GUARANTORS = '0x8ea69468'
REFUSALS = (('wrong registration block hash', tamper_block_hash),
            ('wrong guarantor membership record', tamper_guarantors),
            ('attestation signatures absent from the registration transaction', tamper_signature),
            ('receipt in a different block', tamper_receipt_block),
            ('wrong settlement event', tamper_event),
            ('wrong checkpoint root', tamper_root),
            ('checkpoint not final', tamper_status),
            ('wrong chain domain', tamper_chain))


def main():
    command = 'timeout 1800s python3 tests/daemon/paxeer_x_historical_finality.py'
    revision = subprocess.run(['git', 'rev-parse', 'HEAD'], cwd=ROOT, capture_output=True, text=True).stdout.strip()
    evidence = os.environ.get('PAXEER_X_EVIDENCE_DIR', '')
    print('revision=%s command=%s evidence=%s' % (revision, shlex.quote(command), evidence or '<unset>'))
    if not BINARY.is_file():
        raise SystemExit('prerequisite missing: prebuilt ' + str(BINARY.relative_to(ROOT)))
    location = os.environ.get('PAXEER_X_HISTORICAL_FINALITY_RECORDING', '')
    if not location:
        raise SystemExit('prerequisite missing: PAXEER_X_HISTORICAL_FINALITY_RECORDING names no captured finalized history')
    recording = Path(location)
    arguments = sys.argv[1:]
    if arguments[:1] == ['--capture']:
        assert len(arguments) == 2 and arguments[1] in PHASES, arguments
        capture(recording, arguments[1])
        return 0
    phases = {}
    for name in PHASES:
        path = recording / (name + '.json')
        if not path.is_file():
            raise SystemExit('prerequisite missing: captured %s recording %s' % (name, path))
        phases[name] = json.loads(path.read_text())
    cases = 0
    admitted, rotated = phases['admission'], phases['rotated']
    assert admitted['transaction'] == rotated['transaction'] and admitted['block'] == rotated['block']
    server = replay(admitted['exchanges'])
    try:
        outcome = recover(server.url, recording, admitted)
    finally:
        server.close()
    assert outcome == {'history': 0, 'admission': 0, 'frontier_unchanged': True}, ('admission', outcome)
    cases += 1
    print('fresh admission at registration membership passed')
    server = replay(rotated['exchanges'])
    try:
        outcome = recover(server.url, recording, rotated)
    finally:
        server.close()
    assert outcome['history'] == 0 and outcome['frontier_unchanged'], ('recovery after rotation', outcome)
    cases += 1
    print('recovery after membership change passed')
    assert outcome['admission'] != 0, ('current eligibility substituted for history', outcome)
    cases += 1
    print('fresh admission after membership change refused')
    for name, change in REFUSALS:
        exchanges = copy.deepcopy(rotated['exchanges'])
        change(exchanges)
        server = replay(exchanges)
        try:
            outcome = recover(server.url, recording, rotated)
        finally:
            server.close()
        assert outcome['history'] != 0 and outcome['frontier_unchanged'], (name, outcome)
        cases += 1
        print(name + ' refused')
    server = replay(rotated['exchanges'])
    server.close()
    outcome = recover(server.url, recording, rotated)
    assert outcome['history'] != 0 and outcome['frontier_unchanged'], ('unavailable history', outcome)
    cases += 1
    print('unavailable historical evidence refused')
    print('cases=%d' % cases)
    return 0


if __name__ == '__main__':
    sys.exit(main())
