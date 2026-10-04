#!/usr/bin/env python3
import argparse
import functools
import hashlib
import http.server
import json
import os
from pathlib import Path
import secrets
import signal
import socket
import stat
import subprocess
import sys
import tarfile
import tempfile
import threading
import time
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[3]
OWNED = [f'tools/qualification/paxeer-x/wallet-injected-{suffix}' for suffix in ('fixture.py', 'fixture.mjs', 'entry.tsx')] + ['tools/paxeer-x/gates/14.8.sh']
EXTENSION_VERSION = '13.50.0'
EXTENSION_ASSET = 'metamask-chrome-13.50.0.zip'
EXTENSION_SHA256 = 'b759caca275dec1a10edfebb9d1de1d26589a92104d8b0523ec442930e20e47c'
FOUNDRY_VERSION = '1.8.4'
FOUNDRY_ASSET = 'foundry_v1.8.4_linux_amd64.tar.gz'
FOUNDRY_SHA256 = '699e2207a6a9b27ca17c48c81e56f1677ed9c58b623b59128b4e15ec9da0625e'
PLAYWRIGHT_VERSION = '1.62.0'
EVIDENCE = Path(os.environ.get('WALLET_INJECTED_FIXTURE_ROOT', '/root/lx-ops/paxeer-x-integration-2026-10-03/task-14.8')).absolute()


def require(value, message):
    if not value:
        raise RuntimeError(message)


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def private_json(path, value):
    path = Path(path)
    with path.open('w', opener=lambda name, flags: os.open(name, flags, 0o600)) as stream:
        json.dump(value, stream, indent=2)
        stream.write('\n')


def protected(path):
    path = Path(path)
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and not path.is_symlink() and info.st_uid == os.getuid()
            and info.st_nlink == 1 and info.st_mode & 0o077 == 0, 'private ordinary manifest required')
    return json.loads(path.read_text())


def source_hashes():
    files = [ROOT / name for name in OWNED]
    for folder in ('human/apps/wallet/src', 'human/wallet/sdk/src', 'agent/sdk/typescript/src'):
        files.extend(p for p in (ROOT / folder).rglob('*') if p.is_file() and p.suffix in ('.ts', '.tsx', '.css', '.json'))
    files.extend(ROOT / name for name in ('human/apps/wallet/package.json', 'human/apps/wallet/pnpm-lock.yaml', 'human/wallet/sdk/package.json', 'agent/sdk/typescript/package.json'))
    require(all(path.is_file() for path in files), 'complete actual wallet sources required')
    return {str(path.relative_to(ROOT)): digest(path) for path in sorted(set(files))}


def download(repo, tag, asset, expected):
    url = f'https://github.com/{repo}/releases/download/v{tag}/{asset}'
    metadata_url = f'https://api.github.com/repos/{repo}/releases/tags/v{tag}'
    request = urllib.request.Request(metadata_url, headers={'User-Agent': 'LayerX-disposable-wallet-fixture'})
    with urllib.request.urlopen(request, timeout=30) as response:
        metadata = json.load(response)
    published = next((row for row in metadata['assets'] if row['name'] == asset), None)
    require(published and published['browser_download_url'] == url and published.get('digest') == 'sha256:' + expected,
            'official release asset provenance differs from pinned source')
    target = EVIDENCE / asset
    if not target.is_file():
        with urllib.request.urlopen(urllib.request.Request(url, headers={'User-Agent': 'LayerX-disposable-wallet-fixture'}), timeout=60) as response, target.open('wb') as output:
            while chunk := response.read(1024 * 1024):
                output.write(chunk)
    require(not target.is_symlink() and digest(target) == expected, 'official dependency asset digest differs')
    private_json(EVIDENCE / (asset + '.provenance.json'), {'url': url, 'release': metadata['html_url'], 'tag': metadata['tag_name'], 'sha256': expected})
    return target


def provision():
    EVIDENCE.mkdir(mode=0o700, parents=True, exist_ok=True)
    require(not EVIDENCE.is_symlink() and EVIDENCE.stat().st_mode & 0o077 == 0, 'private evidence directory required')
    archive = download('MetaMask/metamask-extension', EXTENSION_VERSION, EXTENSION_ASSET, EXTENSION_SHA256)
    extension = EVIDENCE / 'metamask'
    extension.mkdir(mode=0o700, exist_ok=True)
    with zipfile.ZipFile(archive) as package:
        for row in package.infolist():
            path = Path(row.filename)
            require(not path.is_absolute() and '..' not in path.parts and not stat.S_ISLNK(row.external_attr >> 16), 'unsafe official extension archive entry')
        if not (extension / 'manifest.json').is_file():
            package.extractall(extension)
        for row in package.infolist():
            if not row.is_dir():
                require((extension / row.filename).is_file() and digest(extension / row.filename) == hashlib.sha256(package.read(row)).hexdigest(), 'unpacked official extension differs')
    manifest = json.loads((extension / 'manifest.json').read_text())
    require(manifest['version'] == EXTENSION_VERSION + '.0' and manifest['manifest_version'] == 3, 'official extension version differs')
    archive = download('foundry-rs/foundry', FOUNDRY_VERSION, FOUNDRY_ASSET, FOUNDRY_SHA256)
    anvil = EVIDENCE / 'foundry' / 'anvil'
    anvil.parent.mkdir(mode=0o700, exist_ok=True)
    with tarfile.open(archive) as package:
        member = package.getmember('anvil')
        require(member.isfile(), 'ordinary official Anvil executable required')
        binary = package.extractfile(member).read()
        if not anvil.is_file():
            anvil.write_bytes(binary)
            anvil.chmod(0o700)
        require(not anvil.is_symlink() and digest(anvil) == hashlib.sha256(binary).hexdigest(), 'Anvil differs from official pinned archive')
    return extension, anvil


def command(argv, name, timeout, environment=None):
    log = EVIDENCE / name
    with log.open('wb') as output:
        result = subprocess.run(argv, cwd=ROOT, env=environment, stdout=output, stderr=subprocess.STDOUT, timeout=timeout)
    require(result.returncode == 0, f'command exit {result.returncode}; log {log}')
    return log


def build():
    extension, anvil = provision()
    dependency_root = Path(os.environ.get('WALLET_INJECTED_NODE_MODULES', '/root/Layerx-protocol/human/apps/wallet/node_modules')).resolve()
    node = Path(os.environ.get('WALLET_INJECTED_NODE', '/root/lx-toolchains/node24/bin/node')).resolve()
    require(node.is_file() and os.access(node, os.X_OK), 'actual Node executable required')
    playwright = dependency_root / '@playwright/test'
    package = json.loads((playwright / 'package.json').read_text())
    require(package['version'] == PLAYWRIGHT_VERSION and package['dependencies']['playwright'] == PLAYWRIGHT_VERSION, 'repository-pinned Playwright required')
    expected = json.loads((ROOT / 'human/apps/wallet/package.json').read_text())['devDependencies']['@playwright/test']
    require(expected == PLAYWRIGHT_VERSION, 'repository browser automation pin changed')
    vite_package = json.loads((dependency_root / 'vite/package.json').read_text())
    require('vite@' + vite_package['version'] in (ROOT / 'human/apps/wallet/pnpm-lock.yaml').read_text(), 'repository-pinned Vite installation required')
    inputs = source_hashes()
    work = EVIDENCE / 'build-input'
    work.mkdir(mode=0o700, exist_ok=True)
    html = work / 'index.html'
    html.write_text('<!doctype html><html><head><meta charset="utf-8"><title>Actual injected wallet fixture</title></head><body><div id="root"></div><script>window.__PAXEER_INJECTED_FIXTURE__=__FIXTURE_BOOTSTRAP__;</script><script type="module" src="' + '/entry.tsx' + '"></script></body></html>')
    (work / 'entry.tsx').write_text('import ' + json.dumps(str(ROOT / OWNED[2])) + ';\n')
    script = work / 'build.mjs'
    config = {'root': str(ROOT), 'dependency_root': str(dependency_root), 'input': str(html), 'output': str(EVIDENCE / 'app')}
    script.write_text('''import fs from 'node:fs';import path from 'node:path';import {createRequire} from 'node:module';import {pathToFileURL} from 'node:url';
const c=JSON.parse(process.argv[2]);const req=createRequire(path.join(c.dependency_root,'../package.json'));
const vite=await import(pathToFileURL(req.resolve('vite')).href);
const aliases=[{find:'@paxeer/wallet',replacement:path.join(c.root,'human/wallet/sdk/src/index.ts')},{find:'@sidiora/layerx-sdk',replacement:path.join(c.root,'agent/sdk/typescript/src/index.ts')},{find:/^@\\//,replacement:path.join(c.root,'human/apps/wallet/src')+'/'}];
const modules=new Set();await vite.build({configFile:false,root:path.dirname(c.input),resolve:{alias:aliases,dedupe:['react','react-dom']},define:{'process.env':'{}','process.env.NODE_ENV':JSON.stringify('production')},plugins:[{name:'actual-pinned-dependencies',resolveId(id){if(!id.startsWith('.')&&!id.startsWith('/')&&!id.startsWith('\\0')){try{return req.resolve(id)}catch{return null}}},moduleParsed(info){modules.add(info.id)},closeBundle(){fs.writeFileSync(path.join(c.output,'source-modules.json'),JSON.stringify([...modules].sort()))}}],build:{outDir:c.output,emptyOutDir:true,rollupOptions:{input:c.input},minify:false}});
const cli=req.resolve('playwright/cli');fs.writeFileSync(path.join(c.output,'playwright-cli.txt'),cli);const mod=req.resolve('@playwright/test');fs.writeFileSync(path.join(c.output,'playwright-module.txt'),mod);
''')
    command([str(node), str(script), json.dumps(config)], 'build.log', 300)
    require(source_hashes() == inputs, 'actual source changed during bundle build')
    cli = (EVIDENCE / 'app/playwright-cli.txt').read_text()
    browser_env = dict(os.environ, PLAYWRIGHT_BROWSERS_PATH=str(EVIDENCE / 'browsers'))
    command([str(node), cli, 'install', 'chromium'], 'browser-install.log', 300, browser_env)
    artifacts = {str(path.relative_to(EVIDENCE / 'app')): digest(path) for path in (EVIDENCE / 'app').rglob('*') if path.is_file()}
    module_files = [Path(name.split('?')[0]) for name in json.loads((EVIDENCE / 'app/source-modules.json').read_text()) if not name.startswith('\0')]
    module_hashes = {str(path): digest(path) for path in module_files if path.is_file() and not path.name.startswith('.env')}
    extension_files = {str(path.relative_to(extension)): digest(path) for path in extension.rglob('*') if path.is_file()}
    private_json(EVIDENCE / 'build.json', {'schema': 'paxeer-x.wallet-injected-build.v1', 'revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(), 'sources': inputs, 'artifacts': artifacts, 'module_hashes': module_hashes, 'extension_files': extension_files, 'vite_version': vite_package['version'], 'node': str(node), 'node_sha256': digest(node), 'anvil': str(anvil), 'anvil_sha256': digest(anvil), 'extension': str(extension), 'extension_archive_sha256': EXTENSION_SHA256, 'playwright_module': (EVIDENCE / 'app/playwright-module.txt').read_text(), 'playwright_version': PLAYWRIGHT_VERSION, 'browsers': str(EVIDENCE / 'browsers')})


def free_port():
    with socket.socket() as handle:
        handle.bind(('127.0.0.1', 0))
        return handle.getsockname()[1]


def rpc(url, method, params=()):
    data = json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': list(params)}).encode()
    with urllib.request.urlopen(urllib.request.Request(url, data, {'content-type': 'application/json'}), timeout=5) as response:
        result = json.load(response)
    require(result.get('jsonrpc') == '2.0' and result.get('id') == 1 and 'result' in result and 'error' not in result, 'actual isolated RPC refused ' + method)
    return result['result']


class AppHandler(http.server.SimpleHTTPRequestHandler):
    def log_message(self, format, *args):
        pass

    def do_GET(self):
        if self.path.split('?')[0] == '/':
            body = (Path(self.directory) / 'index.html').read_text().replace('__FIXTURE_BOOTSTRAP__', json.dumps({'recipient': self.server.recipient, 'amount': '0.0001'})).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'text/html; charset=utf-8')
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        super().do_GET()


def verify():
    build_record = protected(EVIDENCE / 'build.json')
    require(build_record['schema'] == 'paxeer-x.wallet-injected-build.v1' and build_record['sources'] == source_hashes(), 'fresh source-bound actual bundle required; verification never builds')
    for name, value in build_record['artifacts'].items():
        require(digest(EVIDENCE / 'app' / name) == value, 'wallet bundle artifact changed')
    for name in ('node', 'anvil'):
        require(digest(build_record[name]) == build_record[name + '_sha256'], 'bound executable changed')
    require(digest(EVIDENCE / EXTENSION_ASSET) == EXTENSION_SHA256, 'official extension archive changed')
    for path, expected in build_record['module_hashes'].items():
        require(digest(Path(path)) == expected, 'actual compiled dependency module changed')
    for path, expected in build_record['extension_files'].items():
        require(digest(Path(build_record['extension']) / path) == expected, 'actual official extension changed')
    run = Path(tempfile.mkdtemp(prefix='baseline-', dir=EVIDENCE))
    os.chmod(run, 0o700)
    profile = run / 'browser-profile'
    profile.mkdir(mode=0o700)
    password = run / 'wallet-password'
    password.write_text(secrets.token_urlsafe(32))
    password.chmod(0o600)
    processes, streams, server = [], [], None
    started = time.monotonic()
    try:
        chains = {}
        for chain in (125, 126):
            port = free_port()
            log = (run / f'chain-{chain}.log').open('wb')
            streams.append(log)
            process = subprocess.Popen([build_record['anvil'], '--host', '127.0.0.1', '--port', str(port), '--chain-id', str(chain), '--accounts', '3', '--mnemonic-random', '--silent'], env={'PATH': os.defpath, 'HOME': str(run), 'LANG': 'C.UTF-8'}, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
            processes.append(process)
            url = f'http://127.0.0.1:{port}'
            until = time.monotonic() + 30
            while True:
                require(process.poll() is None and time.monotonic() < until, 'owned isolated EVM chain did not start')
                try:
                    require(int(rpc(url, 'eth_chainId'), 16) == chain, 'isolated chain identifier differs')
                    accounts = rpc(url, 'eth_accounts')
                    require(len(accounts) == 3, 'real disposable chain accounts required')
                    break
                except (OSError, TimeoutError):
                    time.sleep(0.1)
            chains[chain] = {'rpc': url, 'genesis': rpc(url, 'eth_getBlockByNumber', ['0x0', False])['hash'], 'pid': process.pid}
        require(chains[125]['rpc'] != chains[126]['rpc'] and chains[125]['genesis'] != chains[126]['genesis'], 'two genuinely isolated chains required')
        server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), functools.partial(AppHandler, directory=str(EVIDENCE / 'app')))
        server.recipient = rpc(chains[125]['rpc'], 'eth_accounts')[2]
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        manifest = run / 'runtime.json'
        private_json(manifest, {'schema': 'paxeer-x.wallet-injected-runtime.v1', 'extension_path': build_record['extension'], 'extension_version': EXTENSION_VERSION, 'playwright_module': build_record['playwright_module'], 'profile_dir': str(profile), 'rpc125': chains[125]['rpc'], 'rpc126': chains[126]['rpc'], 'app_url': f'http://127.0.0.1:{server.server_port}', 'password_file': str(password), 'output_dir': str(run), 'recipient': server.recipient, 'value_wei': '100000000000000', 'source_hashes': build_record['sources']})
        env = {'PATH': os.defpath, 'HOME': str(run), 'LANG': 'C.UTF-8'}
        env['PLAYWRIGHT_BROWSERS_PATH'] = build_record['browsers']
        command([build_record['node'], str(ROOT / OWNED[1]), '--manifest', str(manifest)], 'baseline-browser.log', 600, env)
        result = protected(run / 'baseline.json')
        require(result['schema'] == 'paxeer-x.wallet-injected-baseline.v1' and result.get('fixture_ready') is True and result.get('completed_cases', 0) >= 8 and result.get('skipped_cases') == 0, 'genuine complete baseline observations required')
        require(result.get('source_hashes') == build_record['sources'], 'baseline source identity differs')
        private_json(EVIDENCE / 'result.json', {'schema': 'paxeer-x.wallet-injected-readiness.v1', 'revision': build_record['revision'], 'source_hashes': build_record['sources'], 'chains': chains, 'baseline_record': str(run / 'baseline.json'), 'fixture_ready': True, 'baseline_wrong_chain_observed': result['baseline_wrong_chain_observed'], 'exit_code': 0, 'elapsed_seconds': time.monotonic() - started})
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
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument('--build', action='store_true')
    group.add_argument('--provision', action='store_true')
    group.add_argument('--verify-baseline', action='store_true')
    options = parser.parse_args()
    os.umask(0o077)
    try:
        if options.build:
            build()
        elif options.provision:
            provision()
        else:
            verify()
        print(json.dumps({'exit_code': 0, 'evidence_root': str(EVIDENCE)}))
        return 0
    except Exception as error:
        EVIDENCE.mkdir(mode=0o700, parents=True, exist_ok=True)
        private_json(EVIDENCE / 'failure.json', {'exit_code': 1, 'observed': str(error), 'command': sys.argv, 'log_path': str(EVIDENCE / ('build.log' if options.build else 'baseline-browser.log')), 'fixture_ready': False})
        print(json.dumps({'exit_code': 1, 'observed': str(error), 'log_path': str(EVIDENCE / 'failure.json')}))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
