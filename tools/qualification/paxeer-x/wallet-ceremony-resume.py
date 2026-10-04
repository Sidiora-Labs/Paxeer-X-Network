#!/usr/bin/env python3
import concurrent.futures
import copy
import hashlib
import http.client
import http.server
import json
import os
import pwd
from pathlib import Path
import re
import select
import shutil
import signal
import socket
import socketserver
import ssl
import subprocess
import tempfile
import threading
import time
import urllib.parse
import uuid

ROOT = Path(__file__).resolve().parents[3]
PROCESSES = []
STATE = None


def require(value, message):
    if not value:
        raise RuntimeError(message)


def private_file(path):
    path = Path(path)
    stat = path.lstat()
    require(path.is_file() and not path.is_symlink() and stat.st_uid == os.getuid()
            and stat.st_nlink == 1 and stat.st_mode & 0o077 == 0, 'protected regular fixture file required')
    return path


def local_url(value):
    parsed = urllib.parse.urlparse(value)
    require(parsed.hostname in ('localhost', '127.0.0.1', '::1'), 'isolated loopback endpoint required')
    return parsed


def wait_for(predicate, message, seconds=60):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(0.1)
    raise RuntimeError(message)


def stop(process):
    if process.poll() is None:
        os.killpg(process.pid, signal.SIGKILL)
        process.wait(timeout=10)


def start(argv, env, name, identity=None):
    with open(STATE / (name + '.log'), 'ab', buffering=0) as log:
        process = subprocess.Popen(argv, cwd=STATE if identity else ROOT, env=env, stdout=log, stderr=log,
                                   start_new_session=True, **(identity or {}))
    PROCESSES.append(process)
    return process


def tls_context(config):
    context = ssl.create_default_context(cafile=config['ca'])
    context.minimum_version = ssl.TLSVersion.TLSv1_3
    context.load_cert_chain(private_file(config['cert']), private_file(config['key']))
    return context


def request(endpoint, context, path, body):
    parsed = local_url(endpoint)
    require(parsed.scheme == 'https', 'operator request must use mTLS')
    connection = http.client.HTTPSConnection(parsed.hostname, parsed.port, context=context, timeout=120)
    data = json.dumps(body, separators=(',', ':')).encode()
    try:
        connection.request('POST', path, data, {'Content-Type': 'application/json'})
        response = connection.getresponse()
        return response.status, json.loads(response.read())
    finally:
        connection.close()


class PeerRelay(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True

    def __init__(self, listen, target):
        self.target = target
        self.enabled = True
        self.connections = set()
        self.lock = threading.Lock()
        super().__init__(tuple(listen), PeerHandler)

    def isolate(self):
        with self.lock:
            self.enabled = False
            for connection in tuple(self.connections):
                try:
                    connection.shutdown(socket.SHUT_RDWR)
                except OSError:
                    pass


class PeerHandler(socketserver.BaseRequestHandler):
    def handle(self):
        server = self.server
        with server.lock:
            if not server.enabled:
                return
        upstream = socket.create_connection(tuple(server.target), timeout=5)
        sockets = (self.request, upstream)
        with server.lock:
            server.connections.update(sockets)
        try:
            while server.enabled:
                ready, _, _ = select.select(sockets, [], [], 1)
                for source in ready:
                    data = source.recv(65536)
                    if not data:
                        return
                    (upstream if source is self.request else self.request).sendall(data)
        except OSError:
            pass
        finally:
            with server.lock:
                server.connections.difference_update(sockets)
            upstream.close()


class Faults:
    def __init__(self, config, peers):
        self.mode = 'normal'
        self.peers = peers
        self.config = config
        self.lock = threading.Lock()
        self.imports = {}
        self.replayed = 0
        self.lost_reply = False
        self.failed_verification = False
        self.verifications = {}
        self.refreshes = {}
        self.failure = None

    def before(self, node, path, raw):
        body = json.loads(raw)
        with self.lock:
            if path == '/v1/keys/import':
                key = (node['id'], body['key_id'])
                previous = self.imports.get(key)
                if previous is not None:
                    if previous != raw:
                        self.failure = 'retry changed original import bytes'
                        return False
                    self.replayed += 1
                else:
                    self.imports[key] = raw
                if self.mode == 'partial' and node['id'] not in self.config['member_ids'][:2]:
                    return False
            if path == '/v1/sign' and body.get('kind') == 'operator_verification':
                self.verifications[node['id']] = body
                if self.mode == 'failed_sign' and not self.failed_verification:
                    self.failed_verification = True
                    for peer in self.peers:
                        peer.isolate()
        return True

    def after(self, node, path, raw, status, response):
        body = json.loads(raw)
        with self.lock:
            if path == '/v1/keys/refresh' and status == 200:
                result = json.loads(response)
                marker = (body['key_id'], result['epoch'])
                seen = self.refreshes.setdefault(marker, set())
                seen.add(node['id'])
                if len(seen) == 5:
                    transitions = [value for value in self.config['inventory_transitions']
                                   if value['key_id'] == marker[0] and value['epoch'] == marker[1]]
                    require(len(transitions) == 1, 'approved next-epoch inventory is missing or ambiguous')
                    approved = private_file(transitions[0]['file']).read_bytes()
                    for filename in self.config['inventory_destinations']:
                        destination = Path(filename)
                        temporary = destination.with_name(destination.name + '.next')
                        with open(temporary, 'xb') as output:
                            os.chmod(temporary, 0o600)
                            output.write(approved)
                            output.flush()
                            os.fsync(output.fileno())
                        os.replace(temporary, destination)
                        directory = os.open(destination.parent, os.O_RDONLY)
                        try:
                            os.fsync(directory)
                        finally:
                            os.close(directory)
            if path == '/v1/keys/import' and status == 200 and self.mode == 'lost_reply' and not self.lost_reply:
                self.lost_reply = True
                return False
        return True


class OperatorProxy(http.server.ThreadingHTTPServer):
    daemon_threads = True
    allow_reuse_address = True

    def __init__(self, config, faults):
        self.config = config
        self.faults = faults
        self.upstream_contexts = {}
        for role in ('operator', 'gateway'):
            credential = config[role]
            pem = Path(credential['cert']).read_text()
            leaf = pem[:pem.index('-----END CERTIFICATE-----') + len('-----END CERTIFICATE-----')]
            certificate = ssl.PEM_cert_to_DER_cert(leaf)
            self.upstream_contexts[certificate] = tls_context(credential)
        super().__init__(tuple(config['listen']), OperatorHandler)
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.minimum_version = ssl.TLSVersion.TLSv1_3
        context.load_cert_chain(private_file(config['server_cert']), private_file(config['server_key']))
        context.load_verify_locations(config['operator']['ca'])
        context.load_verify_locations(config['gateway']['ca'])
        context.verify_mode = ssl.CERT_REQUIRED
        self.socket = context.wrap_socket(self.socket, server_side=True)


class OperatorHandler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_POST(self):
        raw = self.rfile.read(int(self.headers.get('content-length', '0')))
        require(len(raw) <= 2 * 1024 * 1024, 'request exceeds focused harness bound')
        proxy = self.server
        if not proxy.faults.before(proxy.config, self.path, raw):
            self.send_error(503)
            return
        credential = proxy.upstream_contexts.get(self.connection.getpeercert(binary_form=True))
        if credential is None:
            self.send_error(403)
            return
        endpoint = local_url(proxy.config['upstream'])
        upstream = http.client.HTTPSConnection(endpoint.hostname, endpoint.port,
                                               context=credential, timeout=120)
        try:
            headers = {'Content-Type': 'application/json'}
            for name in ('Authorization', 'X-Agent-Key', 'X-Agent-Nonce', 'X-Agent-Expiry', 'X-Agent-Signature'):
                if name in self.headers:
                    headers[name] = self.headers[name]
            upstream.request('POST', self.path, raw, headers)
            response = upstream.getresponse()
            result = response.read()
            if not proxy.faults.after(proxy.config, self.path, raw, response.status, result):
                self.close_connection = True
                return
            self.send_response(response.status)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(result)))
            self.end_headers()
            self.wfile.write(result)
        except (OSError, http.client.HTTPException):
            self.close_connection = True
        except Exception as error:
            proxy.faults.failure = type(error).__name__
            self.close_connection = True
        finally:
            upstream.close()


def main():
    global STATE
    supplied = os.environ.get('WALLET_CEREMONY_FIXTURE_BUNDLE')
    require(supplied, 'prerequisite: protected approved WALLET_CEREMONY_FIXTURE_BUNDLE')
    bundle = Path(supplied).resolve()
    require(bundle.is_dir() and not bundle.is_symlink() and bundle.stat().st_mode & 0o077 == 0,
            'fixture directory must be private')
    require(not any(path.name == '.env' or path.name.startswith('.env.') for path in bundle.rglob('*')),
            'fixture bundle must not contain .env files')
    config = json.loads(private_file(bundle / 'wallet-ceremony-fixture.json').read_text())
    require(config.get('version') == 1 and config.get('isolated') is True, 'approved isolated fixture version 1 required')
    source_manifest = json.loads((bundle / config['source_manifest']).read_text())
    expected = set()
    for module in ('attestor', 'ceremony'):
        directory = ROOT / 'human/wallet' / module
        expected.update(str(path.relative_to(ROOT)) for path in directory.rglob('*.go'))
        expected.update('human/wallet/' + module + '/' + name for name in ('go.mod', 'go.sum'))
    require(set(source_manifest) == expected, 'complete attestor and ceremony source manifest required')
    for relative, digest in source_manifest.items():
        require(hashlib.sha256((ROOT / relative).read_bytes()).hexdigest() == digest,
                'source and supplied prebuilt artifact revision differ')
    for name, artifact in config['artifacts'].items():
        binary = Path(artifact['path'])
        require(binary.is_file() and os.access(binary, os.X_OK), 'prebuilt executable missing: ' + name)
        require(hashlib.sha256(binary.read_bytes()).hexdigest() == artifact['sha256'], 'artifact digest mismatch: ' + name)
        require(re.fullmatch('[0-9a-f]{40}', artifact['source_revision']), 'artifact source provenance required')
    STATE = Path(tempfile.mkdtemp(prefix='wallet-ceremony-resume-', dir=os.environ.get('WALLET_CEREMONY_EVIDENCE_ROOT')))
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
    base_env = {name: value for name, value in os.environ.items()
                if not name.startswith(('CEREMONY_', 'ATTESTOR_', 'WALLET_', 'SUPABASE_', 'DATABASE_', 'PG'))}
    require(len(config['member_ids']) == len(set(config['member_ids'])) == 5, 'exactly five approved members required')
    peers = []
    for item in config['peer_relays']:
        require(item['listen'][0] == item['target'][0] == '127.0.0.1', 'peer relay must be loopback-only')
        peer = PeerRelay(item['listen'], item['target'])
        peers.append(peer)
        threading.Thread(target=peer.serve_forever, daemon=True).start()
    require(len(peers) == 5, 'five real peer relays required')
    faults = Faults(config, peers)
    proxies = []
    require({item['id'] for item in config['operator_proxies']} == set(config['member_ids']), 'proxy roster differs')
    for item in config['operator_proxies']:
        require(item['listen'][0] == '127.0.0.1', 'operator relay must be loopback-only')
        local_url(item['upstream'])
        proxy = OperatorProxy(item, faults)
        proxies.append(proxy)
        threading.Thread(target=proxy.serve_forever, daemon=True).start()
    services = config['services']
    require(sum(item['kind'] == 'attestor' for item in services) == 5, 'five actual daemon processes required')
    for kind in ('postgres', 'supabase', 'chain'):
        require(sum(item['kind'] == kind for item in services) == 1, 'one actual ' + kind + ' process required')
    require(all(item['kind'] in ('attestor', 'postgres', 'supabase', 'chain') for item in services), 'unknown service type')
    daemon_processes = []
    def restore_nodes(plane):
        for item in plane['retained_stores']:
            active = Path(item['active']).resolve()
            baseline = Path(item['baseline']).resolve()
            require(active.is_relative_to(STATE) and baseline.is_relative_to(STATE)
                    and active != baseline and active != STATE and not baseline.is_relative_to(active),
                    'store reset must be confined to independent copied fixture state')
            if active.exists():
                shutil.rmtree(active)
            shutil.copytree(baseline, active)
        approved = private_file(plane['initial_inventory']).read_bytes()
        for filename in plane['inventory_destinations']:
            path = Path(filename).resolve()
            require(path.is_relative_to(STATE), 'inventory must be isolated retained state')
            path.write_bytes(approved)
            path.chmod(0o600)
    require(len(config['retained_stores']) == 5, 'five original sealed store snapshots required')
    restore_nodes(config)
    postgres_identity = None
    database_state = Path(config['postgres_data_dir']).resolve()
    database_baseline = Path(config['postgres_baseline']).resolve()
    require(database_state.parent == STATE and database_baseline.is_relative_to(copied),
            'database state must be a separate retained child directory')
    shutil.copytree(database_baseline, database_state)
    for path in [database_state, *database_state.rglob('*')]:
        require(not path.is_symlink(), 'database snapshot symlinks refused')
    if os.getuid() == 0:
        owner = pwd.getpwnam('postgres')
        postgres_identity = {'user': owner.pw_uid, 'group': owner.pw_gid}
        os.chown(STATE, owner.pw_uid, owner.pw_gid)
        for path in [database_state, *database_state.rglob('*')]:
            os.chown(path, owner.pw_uid, owner.pw_gid)
    def launch_service(index, item, plane=config):
        environment = dict(base_env)
        environment.update(item['env'])
        if item['kind'] == 'attestor':
            require(not item['argv'], 'actual attestor daemon must start without alternate operation arguments')
            active = Path(environment['ATTESTOR_DATA_DIR']).resolve()
            inventory = Path(environment['ATTESTOR_INVENTORY_FILE']).resolve()
            require(active.is_relative_to(STATE) and str(active) in
                    {str(Path(store['active']).resolve()) for store in plane['retained_stores']},
                    'attestor must use its declared isolated retained store')
            require(inventory.is_relative_to(STATE) and str(inventory) in
                    {str(Path(path).resolve()) for path in plane['inventory_destinations']},
                    'attestor must use its declared isolated approved inventory')
            for key, value in item['env'].items():
                if value and key.endswith('_DIR'):
                    require(Path(value).resolve().is_relative_to(STATE), 'attestor output directory must be isolated')
        for key, value in item['env'].items():
            if key.endswith('_URL') and urllib.parse.urlparse(value).hostname:
                local_url(value)
        return start([config['artifacts'][item['artifact']]['path'], *item['argv']], environment, f'{item["kind"]}-{index}',
                     postgres_identity if item['kind'] == 'postgres' else None)
    for index, item in enumerate(services):
        process = launch_service(index, item)
        if item['kind'] == 'attestor':
            daemon_processes.append((index, item, process))
    rehearsal = config.get('rehearsal')
    require(isinstance(rehearsal, dict), 'separate isolated original-key rehearsal plane required')
    require(rehearsal['member_ids'] == config['member_ids'], 'rehearsal membership must match the approved original roster')
    require(len(rehearsal['retained_stores']) == 5, 'five isolated original sealed rehearsal snapshots required')
    active_paths = {str(Path(item['active']).resolve()) for item in config['retained_stores']}
    rehearsal_paths = {Path(item['active']).resolve() for item in rehearsal['retained_stores']}
    require(all(not Path(live).is_relative_to(isolated) and not isolated.is_relative_to(Path(live))
                for live in active_paths for isolated in rehearsal_paths),
            'rehearsal and live custody stores must be separate')
    restore_nodes(rehearsal)
    rehearsal_peers = []
    for item in rehearsal['peer_relays']:
        require(item['listen'][0] == item['target'][0] == '127.0.0.1', 'rehearsal peer relay must be loopback-only')
        peer = PeerRelay(item['listen'], item['target'])
        rehearsal_peers.append(peer)
        threading.Thread(target=peer.serve_forever, daemon=True).start()
    require(len(rehearsal_peers) == 5, 'five isolated genuine rehearsal peer relays required')
    rehearsal_faults = Faults(rehearsal, rehearsal_peers)
    rehearsal_proxies = []
    require({item['id'] for item in rehearsal['operator_proxies']} == set(config['member_ids']), 'rehearsal proxy roster differs')
    live_endpoints = {local_url(item['upstream']).netloc for item in config['operator_proxies']}
    for item in rehearsal['operator_proxies']:
        require(item['listen'][0] == '127.0.0.1', 'rehearsal operator relay must be loopback-only')
        require(local_url(item['upstream']).netloc not in live_endpoints, 'rehearsal uses a live attestor endpoint')
        proxy = OperatorProxy(item, rehearsal_faults)
        rehearsal_proxies.append(proxy)
        threading.Thread(target=proxy.serve_forever, daemon=True).start()
    require(len(rehearsal['services']) == 5 and all(item['kind'] == 'attestor' for item in rehearsal['services']),
            'five actual isolated rehearsal daemons required')
    rehearsal_processes = [launch_service(index + len(services), item, rehearsal)
                           for index, item in enumerate(rehearsal['services'])]
    def ready_plane(operator_proxies, processes):
        for process in processes:
            require(process.poll() is None, 'actual attestor exited; inspect retained private log')
        for item in operator_proxies:
            endpoint = local_url(item['upstream'])
            connection = http.client.HTTPSConnection(endpoint.hostname, endpoint.port,
                context=tls_context(item['operator']), timeout=2)
            try:
                connection.request('GET', '/health')
                response = connection.getresponse()
                body = json.loads(response.read())
                if response.status != 200 or body.get('node_id') != item['id']:
                    return False
            except (OSError, http.client.HTTPException):
                return False
            finally:
                connection.close()
        return True
    def ready_nodes():
        return ready_plane(config['operator_proxies'], [process for _, _, process in daemon_processes])
    def restart_nodes():
        for _, _, process in daemon_processes:
            stop(process)
        for position, (index, item, _) in enumerate(daemon_processes):
            daemon_processes[position] = (index, item, launch_service(index, item))
        wait_for(ready_nodes, 'retained attestors did not become ready')
    ceremony_env = dict(base_env)
    ceremony_env.update(config['ceremony_env'])
    for key in ('CEREMONY_DATABASE_URL', 'CEREMONY_RPC_URL', 'CEREMONY_SOURCE_DATABASE_URL', 'CEREMONY_REHEARSAL_ADMIN_URL'):
        local_url(ceremony_env[key])
    local_url(ceremony_env['CEREMONY_REHEARSAL_RPC_URL'])
    ceremony = config['artifacts']['ceremony']['path']
    durability = config['artifacts']['attestor_durability_tests']['path']
    focused = start([durability, '-test.v', '-test.count=1', '-test.timeout=90s',
                     '-test.run=^Test(DurableImportExactReplayAndConflictsAfterRestart|CeremonyStageRecoveryRequiresBoundIdentityAndDurableDecision|VerificationEvidenceSurvivesAuditFailureAndRestart|VerificationRecoveryEvidenceRejectsDifferentIdentity)$'],
                    base_env, 'durable-store-audit')
    require(focused.wait(timeout=100) == 0, 'real durable store/audit fault cases failed')
    focused_log = (STATE / 'durable-store-audit.log').read_text()
    for name in ('TestDurableImportExactReplayAndConflictsAfterRestart',
                 'TestCeremonyStageRecoveryRequiresBoundIdentityAndDurableDecision',
                 'TestVerificationEvidenceSurvivesAuditFailureAndRestart',
                 'TestVerificationRecoveryEvidenceRejectsDifferentIdentity'):
        require('--- PASS: ' + name in focused_log and '--- SKIP: ' + name not in focused_log,
                'prebuilt focused binary did not execute required case ' + name)
    def invoke(name, args, expected_success):
        process = start([ceremony, *args], ceremony_env, name)
        code = process.wait(timeout=180)
        require((code == 0) == expected_success, name + ' produced unexpected exit status')
        require(faults.failure is None, faults.failure or 'fault proxy integrity failure')
        return process
    def query(sql):
        output = subprocess.run([config['artifacts']['psql']['path'], '-X', '-A', '-t', '-v', 'ON_ERROR_STOP=1',
                                 ceremony_env['CEREMONY_DATABASE_URL'], '-c', sql], env=base_env,
                                check=True, capture_output=True, text=True, timeout=20)
        return output.stdout.strip()
    wallet_id = str(uuid.UUID(config['wallet_id']))
    def unmigrated():
        require(query("select count(*) from wallets where id='" + wallet_id + "'::uuid and migrated_at is null") == '1',
                'partial ceremony marked the wallet migrated')
    wait_for(ready_nodes, 'real attestors did not become ready')
    def database_ready():
        try:
            return query('select 1') == '1'
        except (subprocess.SubprocessError, OSError):
            return False
    wait_for(database_ready, 'real isolated database did not become ready')
    wait_for(lambda: ready_plane(rehearsal['operator_proxies'], rehearsal_processes),
             'real isolated rehearsal attestors did not become ready')
    require(query("select count(*) from wallets w where w.migrated_at is null and not (w.kind='funded' or exists(select 1 from funded_accounts f where f.wallet_id=w.id))") == '1',
            'focused positive fixture must have exactly one actually eligible standard wallet')
    live_journal = ceremony_env['CEREMONY_JOURNAL_DIR']
    ceremony_env['CEREMONY_REHEARSAL_JOURNAL_DIR'] = config['rehearsal_journal_dir']
    require(ceremony_env['CEREMONY_REHEARSAL_JOURNAL_DIR'] != live_journal, 'separate rehearsal journal required')
    original_rehearsal_nodes = ceremony_env['CEREMONY_REHEARSAL_NODES']
    ceremony_env.pop('CEREMONY_REHEARSAL_NODES')
    invoke('missing-rehearsal-config-refusal', ['rehearse', '--report-only-counts'], False)
    ceremony_env['CEREMONY_REHEARSAL_NODES'] = ceremony_env['CEREMONY_NODES']
    invoke('shared-live-custody-refusal', ['rehearse', '--report-only-counts'], False)
    require(not faults.imports and not rehearsal_faults.imports, 'shared custody refusal reached import')
    ceremony_env['CEREMONY_REHEARSAL_NODES'] = original_rehearsal_nodes
    ceremony_env['CEREMONY_REHEARSAL_JOURNAL_DIR'] = live_journal
    invoke('shared-live-journal-refusal', ['rehearse', '--report-only-counts'], False)
    ceremony_env['CEREMONY_REHEARSAL_JOURNAL_DIR'] = config['rehearsal_journal_dir']
    original_pins = ceremony_env['CEREMONY_REHEARSAL_NODE_PINS']
    pins = original_pins.split(',')
    member, _, pin = pins[0].partition('=')
    pins[0] = member + '=' + ('00' * 32 if pin.strip().removeprefix('0x') != '00' * 32 else '11' * 32)
    ceremony_env['CEREMONY_REHEARSAL_NODE_PINS'] = ','.join(pins)
    invoke('rehearsal-pin-substitution-refusal', ['rehearse', '--report-only-counts'], False)
    ceremony_env['CEREMONY_REHEARSAL_NODE_PINS'] = original_pins
    require(not faults.imports and not rehearsal_faults.imports, 'rehearsal scope refusal reached custody import')
    invoke('rehearsal', ['rehearse', '--report-only-counts'], True)
    require(rehearsal_faults.failure is None, rehearsal_faults.failure or 'rehearsal relay integrity failure')
    require(len(rehearsal_faults.imports) == 5 and not faults.imports,
            'rehearsal must import only into its isolated five-member custody plane')
    rehearsal_output = (STATE / 'rehearsal.log').read_text()
    require(rehearsal_output.strip() and all(re.fullmatch(r'(?:(?:[a-z_]+=[0-9]+|table=[a-z_][a-z0-9_]*)(?: |$))+', line)
                                           for line in rehearsal_output.splitlines()), 'rehearsal must emit only stage counts')
    require(private_file(ceremony_env['CEREMONY_REHEARSAL_RECEIPT']).is_file(), 'durable rehearsal receipt missing')
    for process in rehearsal_processes:
        stop(process)
    for proxy in rehearsal_proxies:
        proxy.shutdown()
    for peer in rehearsal_peers:
        peer.shutdown()
    require(ceremony_env['CEREMONY_JOURNAL_DIR'] == live_journal, 'rehearsal changed live journal configuration')
    wait_for(ready_nodes, 'live attestors changed during isolated rehearsal')
    unmigrated()
    original_did = query("select did from wallets where id='" + wallet_id + "'::uuid")
    require(original_did.startswith('did:') and len(original_did) <= 256, 'fixture original DID missing')
    query("update wallets set did='did:layerx:" + '00' * 32 + "' where id='" + wallet_id + "'::uuid")
    invoke('changed-original-identity', ['deliver'], False)
    unmigrated()
    query("update wallets set did='" + original_did.replace("'", "''") + "' where id='" + wallet_id + "'::uuid")
    require(not faults.imports, 'changed original identity reached import before receipt refusal')
    faults.mode = 'partial'
    invoke('partial-import', ['deliver'], False)
    unmigrated()
    require(len(faults.imports) >= 2, 'partial import did not reach real nodes')
    first = config['operator_proxies'][0]
    original = next(json.loads(raw) for (node, _), raw in faults.imports.items() if node == first['id'])
    for field, replacement, expected in (
            ('session_id', 'gate-conflict-session', 'key_exists'),
            ('ceremony_id', 'gate-conflict-ceremony', 'key_exists'),
            ('epoch', 1, 'key_invalid_share'),
            ('threshold', 2, 'key_invalid_share'),
            ('participants', original['participants'][:-1], 'key_invalid_share'),
            ('public_key', '00' * 65, 'key_invalid_share')):
        conflict = dict(original)
        conflict[field] = replacement
        status, refused = request(first['upstream'], tls_context(first['operator']), '/v1/keys/import', conflict)
        require(status == 409 and refused.get('error', {}).get('code') == expected,
                'conflicting ' + field + ' did not produce its production refusal')
    restart_nodes()
    faults.mode = 'lost_reply'
    invoke('lost-reply', ['deliver'], False)
    require(faults.lost_reply, 'real committed import reply was not interrupted')
    unmigrated()
    restart_nodes()
    faults.mode = 'failed_sign'
    invoke('failed-verification', ['deliver'], False)
    require(faults.failed_verification, 'failure did not reach an actual verification attempt')
    recoverable = 0
    for item in config['operator_proxies']:
        body = faults.verifications.get(item['id'])
        if body is None:
            continue
        status, result = request(item['upstream'], tls_context(item['operator']), '/v1/keys/describe',
                                 {'session_id': 'gate-failed-' + uuid.uuid4().hex, 'key_id': body['key_id']})
        if status == 200 and result.get('verification_state') in ('failed', 'signing'):
            recoverable += 1
    require(recoverable >= 3, 'failed real quorum did not retain recoverable verification state')
    unmigrated()
    for peer in peers:
        peer.enabled = True
    restart_nodes()
    faults.mode = 'normal'
    blocker = start([config['artifacts']['psql']['path'], '-X', '-A', '-t', '-v', 'ON_ERROR_STOP=1',
                     ceremony_env['CEREMONY_DATABASE_URL'], '-c',
                     "begin; lock table wallets in share mode; select pg_sleep(170);"],
                    dict(base_env, PGAPPNAME='wallet-ceremony-gate-blocker'), 'commit-blocker')
    wait_for(lambda: query("select count(*) from pg_locks l join pg_stat_activity a on a.pid=l.pid where a.application_name='wallet-ceremony-gate-blocker' and l.relation='wallets'::regclass and l.mode='ShareLock' and l.granted") == '1', 'database commit barrier missing')
    pending = start([ceremony, 'deliver'], ceremony_env, 'precommit')
    def verified():
        if pending.poll() is not None:
            raise RuntimeError('ceremony exited before retained precommit boundary')
        with faults.lock:
            requests = copy.deepcopy(faults.verifications)
        if len(requests) < 3:
            return False
        complete = 0
        for item in config['operator_proxies']:
            if item['id'] not in requests:
                continue
            body = requests[item['id']]
            status, response = request(item['upstream'], tls_context(item['operator']), '/v1/keys/describe',
                                       {'session_id': 'gate-describe-' + uuid.uuid4().hex, 'key_id': body['key_id']})
            if status == 200 and response.get('verification_state') == 'complete':
                complete += 1
        return complete >= 3
    wait_for(verified, 'quorum verification did not become durable before database mark', 150)
    stop(pending)
    unmigrated()
    stop(blocker)
    restart_nodes()
    invoke('resume-precommit', ['deliver'], True)
    require(query("select count(*) from wallets where id='" + wallet_id + "'::uuid and migrated_at is not null") == '1',
            'verified wallet was not atomically marked')
    require(faults.replayed >= 2, 'exact import bytes were not retried')
    with faults.lock:
        captured = copy.deepcopy(faults.verifications)
    for item in config['operator_proxies']:
        if item['id'] not in captured:
            continue
        body = captured[item['id']]
        status, cached = request(item['upstream'], tls_context(item['operator']), '/v1/sign', body)
        require(status == 200 and cached.get('audit_sequence', 0) > 0 and cached.get('signature'),
                'completed verification did not replay durable evidence')
        changed = dict(body, session_id='gate-second-' + uuid.uuid4().hex)
        status, refused = request(item['upstream'], tls_context(item['operator']), '/v1/sign', changed)
        require(status == 409 and refused.get('error', {}).get('code') == 'verification_used',
                'second successful verification grant was not refused')
    status, recovered_import = request(first['upstream'], tls_context(first['operator']), '/v1/keys/import', original)
    require(status == 200 and recovered_import.get('epoch') == 1,
            'exact original import replay failed at the current approved epoch')
    changed_epoch = dict(original, epoch=1)
    status, refused = request(first['upstream'], tls_context(first['operator']), '/v1/keys/import', changed_epoch)
    require(status == 409 and refused.get('error', {}).get('code') == 'key_invalid_share',
            'advanced-epoch replay admitted a new import mutation')
    status, refused = request(first['upstream'], tls_context(first['gateway']), '/v1/keys/import', original)
    require(status == 403 and refused.get('error', {}).get('code') == 'operator_required',
            'gateway certificate acquired operator import authority')
    generated_probe = dict(next(iter(captured.values())), key_id=config['original_identity_key_id'],
                           session_id='gate-generated-' + uuid.uuid4().hex)
    status, refused = request(first['upstream'], tls_context(first['operator']), '/v1/sign', generated_probe)
    require(status == 409 and refused.get('error', {}).get('code') == 'verification_not_imported',
            'original generated identity acquired an import-only verification grant')
    invoke('final-counts', ['deliver', '--report-only-counts'], True)
    for proxy in proxies:
        proxy.shutdown()
    for peer in peers:
        peer.shutdown()
    result = {'status': 'passed', 'cases': ['missing-rehearsal-config-refusal', 'shared-live-custody-refusal', 'shared-live-journal-refusal',
              'rehearsal-pin-substitution-refusal', 'isolated-rehearsal-custody', 'counts-only-rehearsal', 'changed-original-identity-refusal', 'partial-import', 'exact-import-retry',
              'import-conflict-refusals', 'lost-reply', 'node-restart', 'failed-verification-recovery', 'precommit-process-death',
              'atomic-migration', 'cached-evidence-replay', 'one-shot-refusal',
              'advanced-epoch-import-replay', 'gateway-operator-refusal', 'generated-key-grant-isolation',
              'durable-stage-decision-recovery', 'audit-failure-evidence-recovery'], 'exit_code': 0}
    (STATE / 'result.json').write_text(json.dumps(result, indent=2))
    print(json.dumps({'status': 'passed', 'evidence': str(STATE)}))


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
