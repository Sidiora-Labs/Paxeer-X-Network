#!/usr/bin/env python3
import argparse
import functools
import http.server
import importlib.util
import json
import os
from pathlib import Path
import secrets
import signal
import subprocess
import sys
import tempfile
import threading
import time

ROOT = Path(__file__).resolve().parents[3]
MODULE = importlib.util.spec_from_file_location('wallet_injected_fixture', ROOT / 'tools/qualification/paxeer-x/wallet-injected-fixture.py')
fixture = importlib.util.module_from_spec(MODULE)
MODULE.loader.exec_module(fixture)
BASELINE_FILES = tuple(fixture.OWNED)
fixture.OWNED += ['human/apps/wallet/e2e/send.spec.ts', 'tools/qualification/paxeer-x/wallet-injected-chain.py', 'tools/paxeer-x/gates/14.2.sh']
fixture.EVIDENCE = Path(os.environ.get('WALLET_INJECTED_CHAIN_ROOT', '/root/lx-ops/paxeer-x-integration-2026-10-03/task-14.2')).absolute()
EVIDENCE = fixture.EVIDENCE


def build():
    extension, anvil = fixture.provision()
    dependency = Path(os.environ.get('WALLET_INJECTED_NODE_MODULES', '/root/Layerx-protocol/human/apps/wallet/node_modules')).resolve()
    node = Path(os.environ.get('WALLET_INJECTED_NODE', '/root/lx-toolchains/node24/bin/node')).resolve()
    fixture.require(node.is_file() and os.access(node, os.X_OK), 'actual Node executable required')
    package = json.loads((dependency / '@playwright/test/package.json').read_text())
    fixture.require(package['version'] == fixture.PLAYWRIGHT_VERSION and package['dependencies']['playwright'] == fixture.PLAYWRIGHT_VERSION, 'pinned actual Playwright required')
    fixture.require(json.loads((ROOT / 'human/apps/wallet/package.json').read_text())['devDependencies']['@playwright/test'] == fixture.PLAYWRIGHT_VERSION, 'repository Playwright pin differs')
    vite = json.loads((dependency / 'vite/package.json').read_text())
    fixture.require('vite@' + vite['version'] in (ROOT / 'human/apps/wallet/pnpm-lock.yaml').read_text(), 'repository Vite pin differs')
    sources = fixture.source_hashes()
    work = EVIDENCE / 'build-input'
    work.mkdir(mode=0o700, exist_ok=True)
    html = work / 'index.html'
    html.write_text('<!doctype html><html><head><meta charset="utf-8"></head><body><div id="root"></div><script>window.__PAXEER_INJECTED_FIXTURE__=__FIXTURE_BOOTSTRAP__;</script><script type="module" src="/entry.tsx"></script></body></html>')
    (work / 'entry.tsx').write_text('import ' + json.dumps(str(ROOT / BASELINE_FILES[2])) + ';\n')
    sdk = work / 'sdk.ts'
    sdk.write_text('export { WalletInterface } from ' + json.dumps(str(ROOT / 'human/wallet/sdk/src/wallet.ts')) + ';\n')
    producer = work / 'build.mjs'
    producer.write_text("""import fs from 'node:fs';import path from 'node:path';import {createRequire} from 'node:module';import {pathToFileURL} from 'node:url';
const c=JSON.parse(process.argv[2]);const req=createRequire(path.join(c.dependency,'../package.json'));const vite=await import(pathToFileURL(req.resolve('vite')).href);const modules=new Set();
await vite.build({configFile:false,root:path.dirname(c.html),resolve:{alias:[{find:'@paxeer/wallet',replacement:path.join(c.root,'human/wallet/sdk/src/index.ts')},{find:'@sidiora/layerx-sdk/browser',replacement:path.join(c.root,'agent/sdk/typescript/src/browser.ts')},{find:'@sidiora/layerx-sdk',replacement:path.join(c.root,'agent/sdk/typescript/src/index.ts')},{find:/^@\\//,replacement:path.join(c.root,'human/apps/wallet/src')+'/'}],dedupe:['react','react-dom']},define:{'process.env':'{}','process.env.NODE_ENV':JSON.stringify('production')},plugins:[{name:'actual-pinned-dependencies',resolveId(id){if(!id.startsWith('.')&&!id.startsWith('/')&&!id.startsWith('\\0')){try{return req.resolve(id)}catch{return null}}},moduleParsed(info){modules.add(info.id)},closeBundle(){fs.writeFileSync(path.join(c.output,'source-modules.json'),JSON.stringify([...modules].sort()))}}],build:{outDir:c.output,emptyOutDir:true,minify:false,rollupOptions:{input:{app:c.html,sdk:c.sdk},preserveEntrySignatures:'strict',output:{entryFileNames:'[name].js'}}}});
const testReq=createRequire(req.resolve('@playwright/test/package.json'));const pwPackage=testReq.resolve('playwright/package.json');const metadata=JSON.parse(fs.readFileSync(pwPackage,'utf8'));fs.writeFileSync(path.join(c.output,'playwright-cli.txt'),path.join(path.dirname(pwPackage),metadata.bin.playwright));fs.writeFileSync(path.join(c.output,'playwright-module.txt'),req.resolve('@playwright/test'));
""")
    fixture.command([str(node), str(producer), json.dumps({'root': str(ROOT), 'dependency': str(dependency), 'html': str(html), 'sdk': str(sdk), 'output': str(EVIDENCE / 'app')})], 'build.log', 300)
    fixture.require(fixture.source_hashes() == sources, 'source changed during build')
    cli = (EVIDENCE / 'app/playwright-cli.txt').read_text()
    fixture.command([str(node), cli, 'install', 'chromium'], 'browser-install.log', 300, dict(os.environ, PLAYWRIGHT_BROWSERS_PATH=str(EVIDENCE / 'browsers')))
    files = [Path(name.split('?')[0]) for name in json.loads((EVIDENCE / 'app/source-modules.json').read_text()) if not name.startswith('\0')]
    fixture.private_json(EVIDENCE / 'build.json', {'schema': 'paxeer-x.wallet-injected-build.v1',
        'revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(), 'sources': sources,
        'artifacts': {str(path.relative_to(EVIDENCE / 'app')): fixture.digest(path) for path in (EVIDENCE / 'app').rglob('*') if path.is_file()},
        'module_hashes': {str(path): fixture.digest(path) for path in files if path.is_file() and not path.name.startswith('.env')},
        'extension_files': {str(path.relative_to(extension)): fixture.digest(path) for path in extension.rglob('*') if path.is_file()},
        'node': str(node), 'node_sha256': fixture.digest(node), 'anvil': str(anvil), 'anvil_sha256': fixture.digest(anvil),
        'extension': str(extension), 'browsers': str(EVIDENCE / 'browsers'),
        'playwright_module': (EVIDENCE / 'app/playwright-module.txt').read_text(), 'dependency_root': str(dependency), 'vite_version': vite['version']})


def baseline():
    record = os.environ.get('WALLET_INJECTED_BASELINE_RECORD')
    fixture.require(record is not None, '14.8 genuine baseline prerequisite is absent; no wrong-chain reproduction has been observed')
    result = fixture.protected(record)
    fixture.require(result.get('schema') == 'paxeer-x.wallet-injected-baseline.v1'
                    and result.get('fixture_ready') is True and result.get('completed_cases', 0) >= 8
                    and result.get('skipped_cases') == 0, '14.8 complete genuine baseline is required')
    fixture.require(result.get('genuineExtension', {}).get('version') == fixture.EXTENSION_VERSION
                    and result.get('playwrightVersion') == fixture.PLAYWRIGHT_VERSION
                    and result.get('chains') == [125, 126], 'baseline actual provider identity differs')
    sources = result.get('source_hashes', {})
    for name in BASELINE_FILES:
        fixture.require(sources.get(name) == fixture.digest(ROOT / name), 'the original baseline fixture identity changed')
    fixture.require(isinstance(result.get('baseline_wrong_chain_observed'), bool), 'baseline observed/static distinction is absent')
    return {'record': str(Path(record).absolute()), 'sha256': fixture.digest(record),
            'wrong_chain_observed': result['baseline_wrong_chain_observed'], 'fixture_sources': {name: sources[name] for name in BASELINE_FILES}}


def verify():
    prior = baseline()
    build = fixture.protected(EVIDENCE / 'build.json')
    fixture.require(build.get('schema') == 'paxeer-x.wallet-injected-build.v1'
                    and build.get('sources') == fixture.source_hashes(), 'source-bound prebuilt production wallet is required; verification never builds')
    fixture.require(build.get('revision') == subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(), 'published candidate revision differs')
    for name, value in build['artifacts'].items():
        fixture.require(fixture.digest(EVIDENCE / 'app' / name) == value, 'prebuilt wallet artifact changed')
    for name in ('node', 'anvil'):
        fixture.require(fixture.digest(build[name]) == build[name + '_sha256'], 'bound executable changed')
    fixture.require(fixture.digest(EVIDENCE / fixture.EXTENSION_ASSET) == fixture.EXTENSION_SHA256, 'official extension archive changed')
    for name, value in build['module_hashes'].items():
        fixture.require(fixture.digest(name) == value, 'actual dependency module changed')
    for name, value in build['extension_files'].items():
        fixture.require(fixture.digest(Path(build['extension']) / name) == value, 'official extension changed')
    run = Path(tempfile.mkdtemp(prefix='chain-', dir=EVIDENCE))
    run.chmod(0o700)
    profile = run / 'browser-profile'
    profile.mkdir(mode=0o700)
    password = run / 'wallet-password'
    password.write_text(secrets.token_urlsafe(32))
    password.chmod(0o600)
    processes, streams, server = [], [], None
    started = time.monotonic()
    try:
        chains = {}
        genesis_time = int(time.time())
        for chain in (125, 126):
            port = fixture.free_port()
            stream = (run / f'chain-{chain}.log').open('wb')
            streams.append(stream)
            process = subprocess.Popen([build['anvil'], '--host', '127.0.0.1', '--port', str(port),
                '--chain-id', str(chain), '--timestamp', str(genesis_time + chain - 125), '--accounts', '3',
                '--mnemonic-random', '--silent'], env={'PATH': os.defpath, 'HOME': str(run), 'LANG': 'C.UTF-8'},
                stdout=stream, stderr=subprocess.STDOUT, start_new_session=True)
            processes.append(process)
            url = f'http://127.0.0.1:{port}'
            deadline = time.monotonic() + 30
            while True:
                fixture.require(process.poll() is None and time.monotonic() < deadline, 'actual isolated chain startup failed')
                try:
                    fixture.require(int(fixture.rpc(url, 'eth_chainId'), 16) == chain, 'actual chain differs')
                    fixture.require(len(fixture.rpc(url, 'eth_accounts')) == 3, 'actual isolated funding accounts are absent')
                    break
                except (OSError, TimeoutError):
                    time.sleep(0.1)
            chains[chain] = {'rpc': url, 'genesis': fixture.rpc(url, 'eth_getBlockByNumber', ['0x0', False])['hash']}
        fixture.require(chains[125]['rpc'] != chains[126]['rpc'] and chains[125]['genesis'] != chains[126]['genesis'], 'two distinct actual chains are required')
        server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), functools.partial(fixture.AppHandler, directory=str(EVIDENCE / 'app')))
        server.recipient = fixture.rpc(chains[125]['rpc'], 'eth_accounts')[2]
        threading.Thread(target=server.serve_forever, daemon=True).start()
        manifest = run / 'runtime.json'
        fixture.private_json(manifest, {'schema': 'paxeer-x.wallet-injected-chain-runtime.v1',
            'extension_path': build['extension'], 'profile_dir': str(profile), 'password_file': str(password),
            'rpc125': chains[125]['rpc'], 'rpc126': chains[126]['rpc'],
            'app_url': f'http://127.0.0.1:{server.server_port}', 'recipient': server.recipient,
            'output_dir': str(run), 'source_hashes': build['sources']})
        config = run / 'playwright.config.cjs'
        config.write_text('module.exports=' + json.dumps({'testDir': str(ROOT / 'human/apps/wallet/e2e'),
            'testMatch': 'send.spec.ts', 'grep': 'injected chain pinning', 'workers': 1, 'timeout': 600000,
            'fullyParallel': False, 'outputDir': str(run / 'browser-artifacts'), 'reporter': 'line'}) + ';\n')
        env = {'PATH': os.defpath, 'HOME': str(run), 'LANG': 'C.UTF-8',
            'PLAYWRIGHT_BROWSERS_PATH': build['browsers'], 'WALLET_INJECTED_CHAIN_RUNTIME': str(manifest),
            'NODE_PATH': build['dependency_root']}
        cli = (EVIDENCE / 'app/playwright-cli.txt').read_text()
        fixture.command([build['node'], cli, 'test', '--config', str(config)], 'chain-browser.log', 660, env)
        result = fixture.protected(run / 'chain-result.json')
        expected = {'rejected_connection', 'missing_embedded_config', 'expected_chain_transaction',
            'mid_confirmation_switch', 'wrong_chain_reload', 'reconnect_reload', 'accounts_changed_lock', 'sign_out', 'revoked_connection', 'sdk_wrong_chain_signatures'}
        fixture.require(result.get('schema') == 'paxeer-x.wallet-injected-chain-result.v1'
                        and result.get('exit_code') == 0 and result.get('source_hashes') == build['sources']
                        and set(result.get('cases', {})) == expected and result.get('completed_cases') == len(expected)
                        and result.get('skipped_cases') == 0, 'complete source-bound real provider cases are required')
        fixture.require(fixture.source_hashes() == build['sources'], 'source changed during qualification')
        fixture.private_json(EVIDENCE / 'result.json', {'revision': build['revision'],
            'command': 'timeout 15m python3 tools/qualification/paxeer-x/wallet-injected-chain.py',
            'exit_code': 0, 'log_path': str(EVIDENCE / 'chain-browser.log'), 'source_hashes': build['sources'],
            'baseline': prior, 'result_record': str(run / 'chain-result.json'),
            'elapsed_seconds': time.monotonic() - started})
    finally:
        if server:
            server.shutdown()
            server.server_close()
        for process in reversed(processes):
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait(timeout=5)
        for stream in streams:
            stream.close()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--build', action='store_true')
    args = parser.parse_args()
    os.umask(0o077)
    EVIDENCE.mkdir(mode=0o700, parents=True, exist_ok=True)
    try:
        fixture.require(not EVIDENCE.is_symlink() and EVIDENCE.stat().st_mode & 0o077 == 0, 'private evidence directory required')
        if args.build:
            build()
        else:
            verify()
        print(json.dumps({'exit_code': 0, 'log_path': str(EVIDENCE / ('build.log' if args.build else 'chain-browser.log'))}))
        return 0
    except Exception as error:
        fixture.private_json(EVIDENCE / 'failure.json', {'exit_code': 1, 'observed': str(error),
            'command': sys.argv, 'log_path': str(EVIDENCE / ('build.log' if args.build else 'chain-browser.log'))})
        print(json.dumps({'exit_code': 1, 'log_path': str(EVIDENCE / 'failure.json'), 'observed': str(error)}))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
