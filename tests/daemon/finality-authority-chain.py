#!/usr/bin/env python3
import copy
import http.server
import json
import os
from pathlib import Path
import signal
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
