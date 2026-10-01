#!/usr/bin/env python3
"""Bounded qualification of wallet transfer truth (req 150).

Drives the production send screen (human/apps/wallet) in Chromium against an
isolated anvil chain that produces real pending, successful, reverted,
replaced and dropped transactions, then requires sealed/final evidence from a
bound explorer status service. Exits nonzero on any unmet acceptance or
missing prerequisite; never installs or rebuilds anything.
"""

import json
import os
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
APP = ROOT / 'human/apps/wallet'
SDK_DIST = ROOT / 'human/wallet/sdk/dist/index.js'
CHAIN_ID = 125
SENDER = '0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266'
TESTS = 6


def refuse(message):
    print(f'wallet-transfer-truth: REFUSED: {message}', file=sys.stderr)
    print(f'PAXEER_X_GATE tests={TESTS} skipped=0')
    sys.exit(1)


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


def rpc(url, method, params=()):
    body = json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': list(params)}).encode()
    request = urllib.request.Request(url, body, {'content-type': 'application/json'})
    with urllib.request.urlopen(request, timeout=5) as response:
        reply = json.load(response)
    if 'error' in reply:
        raise RuntimeError(f'{method}: {reply["error"]}')
    return reply['result']


def prerequisites():
    anvil = shutil.which('anvil')
    if anvil is None:
        refuse('anvil is not on PATH (source the task environment)')
    if not (APP / 'node_modules/.bin/playwright').exists():
        refuse(f'{APP}/node_modules is not installed; supply the prebuilt wallet app dependencies')
    if not SDK_DIST.exists():
        refuse(f'{SDK_DIST} is missing; supply the prebuilt @paxeer/wallet SDK')
    browsers = Path(os.environ.get('PLAYWRIGHT_BROWSERS_PATH', Path.home() / '.cache/ms-playwright'))
    if not any(browsers.glob('chromium*')):
        refuse(f'no Chromium build under {browsers}; supply the prebuilt Playwright browser')
    return anvil


def explorer_binding():
    url = os.environ.get('PAXEER_X_EXPLORER_STATUS_URL', '')
    manifest_path = os.environ.get('PAXEER_X_CANDIDATE_MANIFEST', '')
    if not manifest_path or not Path(manifest_path).is_file():
        return None, 'PAXEER_X_CANDIDATE_MANIFEST does not name a candidate manifest'
    manifest = json.loads(Path(manifest_path).read_text())
    explorer = next((s for s in manifest.get('services', []) if s.get('id') == 'explorer'), None)
    if explorer is None or explorer['bindings'].get('source_revision', 'unknown') == 'unknown':
        return None, 'the candidate manifest binds no explorer status service with a known source revision'
    if not url:
        return None, 'PAXEER_X_EXPLORER_STATUS_URL is not set to the bound explorer status service'
    return url, None


def main():
    anvil = prerequisites()
    state = Path(tempfile.mkdtemp(prefix='wallet-transfer-truth-'))
    port = free_port()
    url = f'http://127.0.0.1:{port}'
    log = (state / 'anvil.log').open('w')
    chain = subprocess.Popen([anvil, '--port', str(port), '--chain-id', str(CHAIN_ID), '--silent'],
                             stdout=log, stderr=subprocess.STDOUT)
    try:
        deadline = time.monotonic() + 30
        while True:
            try:
                if int(rpc(url, 'eth_chainId'), 16) != CHAIN_ID:
                    refuse('anvil answered the wrong chain id')
                break
            except OSError:
                if time.monotonic() > deadline or chain.poll() is not None:
                    refuse(f'anvil did not start; see {state}/anvil.log')
                time.sleep(0.2)
        env = dict(os.environ,
                   CI='1',
                   WALLET_TRANSFER_TRUTH_ANVIL_URL=url,
                   WALLET_TRANSFER_TRUTH_SENDER=SENDER,
                   NEXT_TELEMETRY_DISABLED='1')
        result = subprocess.run(
            [str(APP / 'node_modules/.bin/playwright'), 'test', '--config', 'playwright.wallet-app.config.ts',
             'e2e/send.spec.ts', '--reporter=line', f'--output={state}/results'],
            cwd=APP, env=env, timeout=600)
        if result.returncode != 0:
            print(f'wallet-transfer-truth: browser flow failed (exit {result.returncode}); state in {state}', file=sys.stderr)
            print(f'PAXEER_X_GATE tests={TESTS} skipped=0')
            return 1
        explorer, why = explorer_binding()
        if explorer is None:
            refuse(f'sealed/final rung evidence (req 150 ac_2) needs a real explorer status binding: {why}')
        final_tx = os.environ.get('PAXEER_X_EXPLORER_FINAL_TX', '')
        if len(final_tx) != 66:
            refuse('PAXEER_X_EXPLORER_FINAL_TX must name a finalized transaction on the bound explorer')
        request = urllib.request.Request(f"{explorer.rstrip('/')}/api/v2/transactions/{final_tx}/status",
                                         headers={'accept': 'application/json'})
        with urllib.request.urlopen(request, timeout=10) as response:
            status = json.load(response)
        if status.get('rung') != 'final' or status.get('finalized_batch_number') is None:
            print(f'wallet-transfer-truth: explorer did not report final evidence: {status}', file=sys.stderr)
            print(f'PAXEER_X_GATE tests={TESTS} skipped=0')
            return 1
        print(f'PAXEER_X_GATE tests={TESTS} skipped=0')
        return 0
    finally:
        chain.terminate()
        chain.wait(timeout=10)
        log.close()


if __name__ == '__main__':
    sys.exit(main())
