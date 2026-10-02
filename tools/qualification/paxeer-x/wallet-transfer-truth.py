#!/usr/bin/env python3
import json
import os
from pathlib import Path
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[3]
APP = ROOT / 'human/apps/wallet'
SDK_DIST = ROOT / 'human/wallet/sdk/dist/index.js'
CHAIN_ID = 125
TESTS = 6


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def required(name):
    value = os.environ.get(name, '')
    require(bool(value), f'missing prerequisite: {name}')
    return value


def endpoint(name, loopback=False):
    value = required(name)
    parsed = urllib.parse.urlsplit(value)
    require(parsed.scheme in ('http', 'https') and parsed.hostname and not parsed.username and not parsed.password,
            f'{name} must identify an HTTP service without embedded credentials')
    if loopback:
        require(parsed.hostname in ('localhost', '127.0.0.1', '::1'), f'{name} must identify the isolated prebuilt wallet')
    return value.rstrip('/')


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


def rpc(url, method, params=()):
    body = json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': list(params)}).encode()
    request = urllib.request.Request(url, body, {'content-type': 'application/json'})
    with urllib.request.urlopen(request, timeout=10) as response:
        reply = json.load(response)
    require(reply.get('jsonrpc') == '2.0' and reply.get('id') == 1 and 'result' in reply and 'error' not in reply,
            f'{method} returned no valid JSON-RPC result')
    return reply['result']


def prerequisites():
    anvil = shutil.which('anvil')
    require(anvil is not None, 'anvil must be preinstalled')
    node = shutil.which('node')
    require(node is not None, 'node must be preinstalled')
    playwright = APP / 'node_modules/@playwright/test/cli.js'
    require(playwright.is_file(), 'supply preinstalled Playwright dependencies')
    require(SDK_DIST.is_file(), 'supply the prebuilt @paxeer/wallet SDK')
    base_url = endpoint('WALLET_TRANSFER_TRUTH_BASE_URL', loopback=True)
    endpoint('WALLET_TRANSFER_TRUTH_BROWSER_RPC_URL')
    endpoint('PAXEER_X_TRANSFER_FINAL_RPC_URL')
    endpoint('PAXEER_X_EXPLORER_STATUS_URL')
    state = Path(required('WALLET_TRANSFER_TRUTH_STORAGE_STATE')).resolve()
    require(state.is_file(), 'supply retained real authenticated wallet storage state')
    require(state.stat().st_mode & 0o077 == 0, 'authenticated storage state must be private')
    manifest = json.loads(Path(required('PAXEER_X_CANDIDATE_MANIFEST')).read_text())
    explorer = next((service for service in manifest.get('services', []) if service.get('id') == 'explorer'), None)
    require(explorer is not None and re.fullmatch(r'[0-9a-f]{40}', explorer.get('bindings', {}).get('source_revision', '')),
            'candidate must bind an explorer service to a known source revision')
    for name in ('PAXEER_X_EXPLORER_SEALED_TX', 'PAXEER_X_EXPLORER_FINAL_TX'):
        require(re.fullmatch(r'0x[0-9a-fA-F]{64}', required(name)), f'{name} must name a real transaction')
    sender = required('WALLET_TRANSFER_TRUTH_SENDER')
    require(re.fullmatch(r'0x[0-9a-fA-F]{40}', sender), 'controlled chain sender must be an address')
    return anvil, node, playwright, base_url, state, sender


def check_bound_evidence():
    rpc_url = endpoint('PAXEER_X_TRANSFER_FINAL_RPC_URL')
    explorer = endpoint('PAXEER_X_EXPLORER_STATUS_URL')
    require(int(rpc(rpc_url, 'eth_chainId'), 16) == CHAIN_ID, 'bound evidence RPC is not Paxeer chain 125')
    for rung, name in (('sealed', 'PAXEER_X_EXPLORER_SEALED_TX'), ('final', 'PAXEER_X_EXPLORER_FINAL_TX')):
        transaction = required(name)
        submitted = rpc(rpc_url, 'eth_getTransactionByHash', [transaction])
        require(isinstance(submitted, dict) and submitted.get('from', '').lower() == required('WALLET_TRANSFER_TRUTH_SENDER').lower() and
                isinstance(submitted.get('to'), str) and submitted.get('input') == '0x' and int(submitted.get('value', '0x0'), 16) > 0,
                f'{rung} evidence must be a native transfer by the authenticated controlled wallet')
        receipt = rpc(rpc_url, 'eth_getTransactionReceipt', [transaction])
        require(isinstance(receipt, dict) and receipt.get('transactionHash', '').lower() == transaction.lower() and
                receipt.get('status') == '0x1', f'{rung} evidence has no matching successful receipt')
        block = rpc(rpc_url, 'eth_getBlockByNumber', [receipt['blockNumber'], False])
        require(isinstance(block, dict) and block.get('hash') == receipt.get('blockHash') and
                transaction.lower() in [item.lower() for item in block.get('transactions', []) if isinstance(item, str)],
                f'{rung} receipt is not canonical')
        request = urllib.request.Request(f'{explorer}/api/v2/transactions/{transaction}/status', headers={'accept': 'application/json'})
        with urllib.request.urlopen(request, timeout=10) as response:
            status = json.load(response)
        require(status.get('rung') == rung and status.get('block_number') == int(receipt['blockNumber'], 16) and
                status.get('sealed_batch_number') is not None and re.fullmatch(r'0x[0-9a-f]{64}', status.get('checkpoint_id') or ''),
                f'{rung} explorer evidence does not bind the canonical receipt')
        if rung == 'final':
            require(status.get('finalized_batch_number') is not None, 'finalized checkpoint evidence is absent')


def main():
    completed = 0
    skipped = 0
    state = Path(tempfile.mkdtemp(prefix='wallet-transfer-truth-'))
    processes, logs = [], []
    try:
        anvil, node, playwright, base_url, storage_state, sender = prerequisites()
        check_bound_evidence()
        urls = []
        for chain_id in (CHAIN_ID, 126):
            port = free_port()
            url = f'http://127.0.0.1:{port}'
            urls.append(url)
            log = (state / f'anvil-{chain_id}.log').open('w')
            logs.append(log)
            chain = subprocess.Popen([anvil, '--host', '127.0.0.1', '--port', str(port), '--chain-id', str(chain_id), '--silent'],
                                     stdout=log, stderr=subprocess.STDOUT)
            processes.append(chain)
            deadline = time.monotonic() + 30
            while True:
                try:
                    require(int(rpc(url, 'eth_chainId'), 16) == chain_id, 'controlled chain identity differs')
                    break
                except OSError:
                    require(time.monotonic() < deadline and chain.poll() is None, f'controlled chain failed; see {state}')
                    time.sleep(0.2)
        require(sender.lower() in [account.lower() for account in rpc(urls[0], 'eth_accounts')],
                'authenticated wallet sender must be an unlocked controlled-chain account')
        env = dict(os.environ, CI='1', WALLET_TRANSFER_TRUTH_ANVIL_URL=urls[0],
                   WALLET_TRANSFER_TRUTH_WRONG_RPC_URL=urls[1], NEXT_TELEMETRY_DISABLED='1')
        config = state / 'playwright.config.cjs'
        report = state / 'report.json'
        config.write_text('module.exports = ' + json.dumps({
            'testDir': str(APP / 'e2e'), 'testMatch': 'send.spec.ts', 'workers': 1,
            'fullyParallel': False, 'timeout': 120000, 'retries': 0,
            'reporter': [['json', {'outputFile': str(report)}]],
            'outputDir': str(state / 'results'),
            'use': {'baseURL': base_url, 'browserName': 'chromium', 'headless': True, 'storageState': str(storage_state)},
        }) + ';\n')
        with (state / 'browser.log').open('w') as log:
            result = subprocess.run([node, str(playwright), 'test', '--config', str(config), '--grep', 'send transfer truth'],
                                    cwd=APP, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=720)
        if report.is_file():
            stats = json.loads(report.read_text()).get('stats', {})
            completed = stats.get('expected', 0)
            skipped = stats.get('skipped', 0)
            require(stats.get('skipped') == 0 and stats.get('unexpected') == 0 and stats.get('flaky') == 0,
                    f'focused browser outcomes failed; see {state}/browser.log')
        require(result.returncode == 0 and completed == TESTS, f'focused browser gate incomplete; see {state}/browser.log')
        print(f'PAXEER_X_GATE tests={completed} skipped={skipped}')
        print(f'wallet-transfer-truth evidence={state}')
        return 0
    except (AssertionError, RuntimeError, OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        print(f'wallet-transfer-truth: REFUSED: {error}; evidence={state}', file=sys.stderr)
        print(f'PAXEER_X_GATE tests={completed} skipped={skipped}')
        return 1
    finally:
        for chain in processes:
            chain.terminate()
            try:
                chain.wait(timeout=10)
            except subprocess.TimeoutExpired:
                chain.kill()
                chain.wait(timeout=5)
        for log in logs:
            log.close()


if __name__ == '__main__':
    sys.exit(main())
