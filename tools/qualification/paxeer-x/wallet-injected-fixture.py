#!/usr/bin/env python3
"""Explicit build and real-extension baseline qualification; no build inside the gate."""
import argparse
import functools
import hashlib
import http.server
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import threading
import time
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[3]
HERE = Path(__file__).resolve().parent


def private_json(path, value):
    fd = os.open(path, os.O_CREAT | os.O_TRUNC | os.O_WRONLY, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, indent=2)
        stream.write('\n')


def rpc(url, method, params):
    body = json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params}).encode()
    request = urllib.request.Request(url, body, {'Content-Type': 'application/json'})
    with urllib.request.urlopen(request, timeout=3) as response:
        result = json.load(response)
    if 'error' in result:
        raise RuntimeError('Isolated RPC refused ' + method)
    return result['result']


def port():
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        return listener.getsockname()[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--provision', action='store_true')
    parser.add_argument('--build-entry', action='store_true')
    parser.add_argument('--verify-baseline', action='store_true')
    args = parser.parse_args()
    if sum((args.provision, args.build_entry, args.verify_baseline)) != 1:
        parser.error('Select exactly one provisioning, explicit build or baseline verification mode')
    evidence = Path(os.environ['WALLET_FIXTURE_EVIDENCE']).resolve()
    if evidence == ROOT or ROOT in evidence.parents:
        raise RuntimeError('Private fixture evidence must be outside the repository')
    evidence.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(evidence, 0o700)
    os.umask(0o077)
    env = dict(os.environ, WALLET_FIXTURE_ROOT=str(ROOT))
    if args.provision:
        dependencies = evidence / 'deps'
        dependencies.mkdir(mode=0o700, exist_ok=True)
        packages = ['esbuild@0.25.12', '@playwright/test@1.62.0', 'react@18.3.1', 'react-dom@18.3.1', 'next@16.2.12', 'ethers@6.17.0', 'framer-motion@11.18.2', 'lucide-react@0.511.0', '@supabase/supabase-js@2.105.4', 'viem@2.56.9', '@noble/curves@2.4.0', '@noble/hashes@2.4.0', '@noble/ciphers@2.4.0', '@scure/bip32@2.4.0', '@scure/bip39@2.4.0']
        subprocess.run(['npm', '--prefix', str(dependencies), 'install', '--ignore-scripts', '--no-audit', '--no-fund', '--save-exact', *packages], check=True, timeout=300)
        archive = evidence / 'metamask-chrome-13.50.0.zip'
        expected = 'b759caca275dec1a10edfebb9d1de1d26589a92104d8b0523ec442930e20e47c'
        if not archive.exists():
            with urllib.request.urlopen('https://github.com/MetaMask/metamask-extension/releases/download/v13.50.0/metamask-chrome-13.50.0.zip', timeout=60) as response:
                archive.write_bytes(response.read(30_000_001))
        if hashlib.sha256(archive.read_bytes()).hexdigest() != expected:
            raise RuntimeError('Official extension archive digest mismatch')
        extension = evidence / 'extension'
        extension.mkdir(mode=0o700, exist_ok=True)
        with zipfile.ZipFile(archive) as zipped:
            for member in zipped.infolist():
                target = (extension / member.filename).resolve()
                if extension not in target.parents:
                    raise RuntimeError('Extension archive contains a non-local path')
            zipped.extractall(extension)
        env['PLAYWRIGHT_BROWSERS_PATH'] = str(evidence / 'browsers')
        subprocess.run(['node', str(dependencies / 'node_modules/playwright/cli.js'), 'install', 'chromium', '--no-shell'], check=True, env=env, timeout=300)
        private_json(evidence / 'dependencies.json', {'packages': packages, 'extension_sha256': expected, 'extension': str(extension), 'archive': str(archive), 'browsers': env['PLAYWRIGHT_BROWSERS_PATH'], 'dependencies': str(dependencies)})
        return 0
    driver = str(HERE / 'wallet-injected-fixture.mjs')
    if args.build_entry:
        return subprocess.call(['node', driver, '--build-entry'], env=env)
    manifest_path = evidence / 'build.json'
    if not manifest_path.is_file():
        raise RuntimeError('Missing explicit prebuilt production-component target')
    manifest = json.loads(manifest_path.read_text())
    for relative, digest in manifest['source_hashes'].items():
        if hashlib.sha256((ROOT / relative).read_bytes()).hexdigest() != digest:
            raise RuntimeError('Prebuilt target does not match source: ' + relative)
    if hashlib.sha256((evidence / 'bundle/app.js').read_bytes()).hexdigest() != manifest['bundle_sha256']:
        raise RuntimeError('Prebuilt bundle digest mismatch')
    anvil = Path(os.environ['WALLET_FIXTURE_ANVIL']).resolve()
    if not anvil.is_file():
        raise RuntimeError('Missing real local EVM executable')
    run = evidence / ('run-' + str(time.time_ns()))
    run.mkdir(mode=0o700)
    processes = []
    logs = []
    server = None
    result = {'status': 'failed', 'baseline_source_sha': manifest['source_sha'], 'builds_in_gate': 0}
    try:
        endpoints = {}
        for chain in (125, 126):
            chain_port = port()
            logfile = open(run / f'chain-{chain}.log', 'wb')
            logs.append(logfile)
            process = subprocess.Popen([str(anvil), '--host', '127.0.0.1', '--port', str(chain_port), '--chain-id', str(chain), '--silent', '--state', str(run / f'chain-{chain}.json')], stdout=logfile, stderr=logfile, start_new_session=True)
            processes.append(process)
            endpoint = f'http://127.0.0.1:{chain_port}'
            for attempt in range(100):
                if process.poll() is not None:
                    raise RuntimeError('Isolated chain process exited')
                try:
                    if int(rpc(endpoint, 'eth_chainId', []), 16) == chain:
                        break
                except (OSError, ValueError):
                    pass
                time.sleep(.1)
            else:
                raise RuntimeError('Isolated chain did not become ready')
            endpoints[str(chain)] = endpoint
        class QuietHandler(http.server.SimpleHTTPRequestHandler):
            def log_message(self, *_args):
                pass
        server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), functools.partial(QuietHandler, directory=str(evidence / 'bundle')))
        threading.Thread(target=server.serve_forever, daemon=True).start()
        env.update(WALLET_FIXTURE_RUN=str(run), WALLET_FIXTURE_APP=f'http://127.0.0.1:{server.server_port}', WALLET_FIXTURE_CHAINS=json.dumps(endpoints))
        with open(run / 'browser.log', 'wb') as browser_log:
            code = subprocess.call(['node', driver, '--verify-baseline'], env=env, stdout=browser_log, stderr=browser_log, timeout=780)
        result.update(exit_code=code, browser_record=str(run / 'baseline.json'), log=str(run / 'browser.log'))
        if code == 0:
            recorded = json.loads((run / 'baseline.json').read_text())
            required = {'discovery', 'connect_rejection', 'connection', 'missing_embedded_configuration', 'network_switch', 'message_signing', 'typed_signing', 'controlled_receipt', 'mid_confirmation_switch', 'lock', 'reconnect', 'reload'}
            if set(recorded['completed_cases']) != required:
                raise RuntimeError('Real browser coverage is incomplete')
            result['status'] = 'passed'
        return code
    finally:
        if server:
            server.shutdown()
        for process in reversed(processes):
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait()
        for logfile in logs:
            logfile.close()
        private_json(run / 'result.json', result)
        private_json(evidence / 'latest.json', dict(result, run=str(run)))


if __name__ == '__main__':
    try:
        sys.exit(main())
    except Exception as error:
        print(type(error).__name__ + ': ' + str(error), file=sys.stderr)
        sys.exit(1)
