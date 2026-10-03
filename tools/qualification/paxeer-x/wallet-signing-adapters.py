#!/usr/bin/env python3
import hashlib
import http.server
import json
import os
from pathlib import Path
import pwd
import shutil
import signal
import socket
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[3]
STATE = None
PROCESSES = []


def require(value, message):
    if not value:
        raise RuntimeError(message)


def local_url(value):
    url = urllib.parse.urlparse(value)
    require(url.hostname in ('localhost', '127.0.0.1', '::1'), 'fixture endpoint must be loopback-only')
    return value


def protected(path):
    path = Path(path)
    stat = path.lstat()
    require(path.is_file() and not path.is_symlink() and stat.st_uid == os.getuid()
            and stat.st_nlink == 1 and stat.st_mode & 0o077 == 0, 'protected fixture file required')
    return path


def stop(process):
    if process.poll() is None:
        os.killpg(process.pid, signal.SIGKILL)
        process.wait(timeout=10)


def start(argv, environment, name, identity=None):
    with open(STATE / (name + '.log'), 'ab', buffering=0) as output:
        process = subprocess.Popen(argv, cwd=STATE if identity else ROOT, env=environment,
                                   stdout=output, stderr=output, start_new_session=True, **(identity or {}))
    PROCESSES.append(process)
    return process


def main():
    global STATE
    supplied = os.environ.get('WALLET_SIGNING_ADAPTERS_FIXTURE_BUNDLE')
    require(supplied, 'prerequisite: WALLET_SIGNING_ADAPTERS_FIXTURE_BUNDLE with approved real isolated services')
    bundle = Path(supplied).resolve()
    require(bundle.is_dir() and bundle.stat().st_mode & 0o077 == 0, 'private fixture directory required')
    require(not any(path.is_symlink() for path in bundle.rglob('*')), 'fixture symlinks refused')
    require(not any(path.name == '.env' or path.name.startswith('.env.') for path in bundle.rglob('*')),
            'fixture bundle must not include .env files')
    config = json.loads(protected(bundle / 'wallet-signing-adapters-fixture.json').read_text())
    require(config.get('version') == 1 and config.get('isolated') is True, 'isolated version 1 fixture required')
    manifest_path = (bundle / config['source_artifact_manifest']).resolve()
    require(manifest_path.is_relative_to(bundle), 'source manifest must remain inside protected bundle')
    manifest = json.loads(protected(manifest_path).read_text())
    expected = set()
    for relative in ('human/wallet/gateway/src', 'human/wallet/gateway/dist', 'human/wallet/gateway/migrations',
                     'human/wallet/sdk/src', 'human/wallet/sdk/dist',
                     'agent/sdk/typescript/src', 'agent/sdk/typescript/dist',
                     'interop/crates/layerx-gas-station/src'):
        expected.update(str(path.relative_to(ROOT)) for path in (ROOT / relative).rglob('*') if path.is_file())
    expected.update(str(path.relative_to(ROOT)) for path in (ROOT / 'human/wallet/attestor').rglob('*.go'))
    expected.update(('human/wallet/attestor/go.mod', 'human/wallet/attestor/go.sum',
                     'human/apps/wallet/src/account/DepositFlow.tsx',
                     'human/wallet/gateway/test/e2e/wallet-signing-adapters.production.ts',
                     'human/wallet/gateway/package.json', 'human/wallet/sdk/package.json',
                     'agent/sdk/typescript/package.json', 'interop/crates/layerx-gas-station/Cargo.toml'))
    require(expected and set(manifest) == expected, 'complete current source and prebuilt artifact manifest required')
    for relative, digest in manifest.items():
        require(not any(part == '.env' or part.startswith('.env.') for part in Path(relative).parts), 'credential files are not source artifacts')
        require(hashlib.sha256((ROOT / relative).read_bytes()).hexdigest() == digest,
                'source/prebuilt artifact mismatch: ' + relative)
    for name, artifact in config['artifacts'].items():
        path = Path(artifact['path'])
        require(path.is_file() and (name == 'driver' or os.access(path, os.X_OK)), 'prebuilt artifact missing: ' + name)
        require(hashlib.sha256(path.read_bytes()).hexdigest() == artifact['sha256'], 'executable digest mismatch: ' + name)
        require(isinstance(artifact['source_revision'], str) and len(artifact['source_revision']) == 40,
                'executable source provenance missing: ' + name)
    driver_artifact = Path(config['artifacts']['driver']['path']).resolve()
    require(driver_artifact.is_relative_to(ROOT / 'human/wallet/gateway/dist') and driver_artifact.suffix in ('.js', '.mjs'),
            'source-bound prebuilt JavaScript driver under gateway/dist required; no runtime transpiler/build')
    STATE = Path(tempfile.mkdtemp(prefix='wallet-signing-adapters-', dir=os.environ.get('WALLET_SIGNING_ADAPTERS_EVIDENCE_ROOT')))
    STATE.chmod(0o700)
    copied = STATE / 'fixture'
    shutil.copytree(bundle, copied)
    def expand(value):
        if isinstance(value, str):
            return value.replace('{fixture}', str(copied)).replace('{state}', str(STATE))
        if isinstance(value, list):
            return [expand(item) for item in value]
        if isinstance(value, dict):
            return {key: expand(item) for key, item in value.items()}
        return value
    config = expand(config)
    environment = {key: os.environ[key] for key in ('PATH', 'LANG', 'LC_ALL', 'TZ') if key in os.environ}
    services = config['services']
    for kind, count in (('attestor', 5), ('postgres', 1), ('supabase', 1), ('chain', 1), ('station', 1)):
        require(sum(item['kind'] == kind for item in services) == count, 'required real service cardinality: ' + kind)
    require(all(item['kind'] in ('attestor', 'postgres', 'supabase', 'chain', 'station') for item in services), 'unknown fixture service')
    database = Path(config['postgres_data_dir']).resolve()
    baseline = Path(config['postgres_baseline']).resolve()
    require(database.parent == STATE and baseline.is_relative_to(copied), 'independent retained database state required')
    shutil.copytree(baseline, database)
    database_identity = None
    if os.getuid() == 0:
        owner = pwd.getpwnam('postgres')
        database_identity = {'user': owner.pw_uid, 'group': owner.pw_gid}
        os.chown(STATE, owner.pw_uid, owner.pw_gid)
        for path in [database, *database.rglob('*')]:
            require(not path.is_symlink(), 'database snapshot symlinks refused')
            os.chown(path, owner.pw_uid, owner.pw_gid)
    running = []
    def launch_service(item, index):
        env = dict(environment)
        env.update(item['env'])
        for key, value in item['env'].items():
            if key.endswith('_URL') and urllib.parse.urlparse(value).hostname:
                local_url(value)
        return start([config['artifacts'][item['artifact']]['path'], *item['argv']], env,
                     item['kind'] + '-' + str(index), database_identity if item['kind'] == 'postgres' else None)
    def service_ready(item, process):
        endpoint = urllib.parse.urlparse(local_url(item['ready_tcp']))
        require(endpoint.port is not None, 'explicit actual service readiness port required')
        deadline = time.monotonic() + 25
        while time.monotonic() < deadline:
            require(process.poll() is None, 'fixture service exited before readiness: ' + item['kind'])
            try:
                with socket.create_connection((endpoint.hostname, endpoint.port), timeout=1):
                    return
            except OSError:
                time.sleep(0.1)
        raise RuntimeError('actual fixture readiness deadline: ' + item['kind'])
    for index, item in enumerate(services):
        running.append(launch_service(item, index))
        service_ready(item, running[-1])
    upstream = local_url(config['chain_rpc_url'])
    block_receipts = threading.Event()
    drop_send_reply = threading.Event()
    dropped_reply = threading.Event()
    send_counts = {}
    count_lock = threading.Lock()
    gateway = None
    gateway_env = dict(environment)
    gateway_env.update(config['gateway_env'])
    require(gateway_env.get('ATTESTOR_QUORUM') == '3', 'custody threshold must remain three')
    endpoints = gateway_env['ATTESTOR_ENDPOINTS'].split(',')
    require(len(set(endpoints)) == 5, 'exactly five approved custody endpoints required')
    for endpoint in endpoints:
        local_url(endpoint)
    gateway_url = local_url(config['gateway_url'])
    def launch_gateway(name):
        return start([config['artifacts']['node']['path'], str(ROOT / 'human/wallet/gateway/dist/index.js')], gateway_env, name)
    def ready():
        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            require(gateway.poll() is None, 'gateway exited before readiness; inspect retained log')
            try:
                with urllib.request.urlopen(gateway_url + '/readyz', timeout=2) as response:
                    if response.status == 200:
                        return
            except (OSError, urllib.error.URLError):
                pass
            time.sleep(0.25)
        raise RuntimeError('actual gateway readiness deadline exceeded')
    class Proxy(http.server.BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass
        def do_POST(self):
            nonlocal gateway
            data = self.rfile.read(int(self.headers.get('content-length', '0')))
            if self.path == '/control/arm-custody':
                block_receipts.set(); drop_send_reply.set(); dropped_reply.clear()
                return self.answer({'armed': True})
            if self.path == '/control/receipts':
                block_receipts.clear()
                return self.answer({'available': True})
            if self.path == '/control/restart':
                stop(gateway)
                station_index = next(index for index, item in enumerate(services) if item['kind'] == 'station')
                stop(running[station_index])
                running[station_index] = launch_service(services[station_index], station_index)
                service_ready(services[station_index], running[station_index])
                gateway = launch_gateway('gateway-restarted')
                ready()
                return self.answer({'restarted': True})
            if self.path == '/control/lose-quorum':
                for index in [index for index, item in enumerate(services) if item['kind'] == 'attestor'][:3]:
                    stop(running[index])
                return self.answer({'stopped': 3})
            if self.path == '/control/state':
                with count_lock:
                    counts = list(send_counts.values())
                return self.answer({'dropped_reply': dropped_reply.is_set(), 'send_counts': counts})
            request = json.loads(data)
            if request.get('method') == 'eth_getTransactionReceipt' and block_receipts.is_set():
                self.close_connection = True
                return
            if request.get('method') == 'eth_sendRawTransaction':
                with count_lock:
                    raw = request['params'][0]
                    send_counts[raw] = send_counts.get(raw, 0) + 1
            try:
                forwarded = urllib.request.Request(upstream, data=data, headers={'Content-Type': 'application/json'})
                with urllib.request.urlopen(forwarded, timeout=120) as response:
                    body = response.read()
                if request.get('method') == 'eth_sendRawTransaction' and drop_send_reply.is_set():
                    drop_send_reply.clear(); dropped_reply.set()
                    self.close_connection = True
                    return
                self.send_response(200); self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(body))); self.end_headers(); self.wfile.write(body)
            except (OSError, urllib.error.URLError):
                self.close_connection = True
        def answer(self, body):
            encoded = json.dumps(body).encode()
            self.send_response(200); self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(encoded))); self.end_headers(); self.wfile.write(encoded)
    proxy = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Proxy)
    proxy.daemon_threads = True
    threading.Thread(target=proxy.serve_forever, daemon=True).start()
    proxy_url = 'http://127.0.0.1:' + str(proxy.server_port)
    gateway_env.update(HYPERPAXEER_RPC_URL=proxy_url, RPC_URLS=proxy_url)
    local_url(gateway_env['DATABASE_URL'])
    local_url(gateway_env['SUPABASE_URL'])
    local_url(gateway_env['WALLET_GAS_STATION_URL'])
    gateway = launch_gateway('gateway')
    ready()
    runtime = dict(config['cases'])
    runtime.update(gateway_url=gateway_url, rpc_url=proxy_url, real_rpc_url=upstream,
                   control_url=proxy_url, sdk_entry=str(ROOT / 'human/wallet/sdk/dist/index.js'),
                   evidence_dir=str(STATE), fixture_dir=str(copied), database_url=gateway_env['DATABASE_URL'])
    runtime_path = STATE / 'runtime.json'
    runtime_path.write_text(json.dumps(runtime)); runtime_path.chmod(0o600)
    driver_env = dict(environment)
    driver_env['WALLET_SIGNING_ADAPTERS_RUNTIME'] = str(runtime_path)
    driver = start([config['artifacts']['node']['path'], str(driver_artifact)], driver_env, 'cases')
    require(driver.wait(timeout=780) == 0, 'actual SDK adapter cases failed; inspect retained cases.log')
    report = json.loads((STATE / 'cases-result.json').read_text())
    require(report.get('complete') is True, 'actual SDK driver did not complete')
    result = {'status': 'passed', 'exit_code': 0, 'cases': report['cases']}
    (STATE / 'result.json').write_text(json.dumps(result, indent=2))
    print(json.dumps({'status': 'passed', 'evidence': str(STATE)}))
    proxy.shutdown()


if __name__ == '__main__':
    signal.signal(signal.SIGTERM, lambda *_: (_ for _ in ()).throw(KeyboardInterrupt()))
    try:
        main()
    except (Exception, KeyboardInterrupt) as error:
        reason = str(error) if isinstance(error, RuntimeError) else type(error).__name__
        result = {'status': 'failed', 'exit_code': 2, 'reason': reason, 'evidence': str(STATE) if STATE else None}
        if STATE:
            (STATE / 'result.json').write_text(json.dumps(result, indent=2))
        print(json.dumps(result))
        raise SystemExit(2)
    finally:
        for process in reversed(PROCESSES):
            stop(process)
