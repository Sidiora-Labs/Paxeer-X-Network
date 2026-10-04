#!/usr/bin/env python3
import argparse
import base64
import http.client
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import selectors
import signal
import socket
import stat
import subprocess
import sys
import time
from urllib.parse import urlsplit, quote

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
PREFIX = 'PAXEER_X_MCP_WEB_'
COMMAND = 'timeout 30m python3 tools/qualification/paxeer-x/mcp_web_payer.py'
CASES = ('web.search', 'web.fetch', 'web.content', 'wrong_tenant', 'wrong_session',
         'missing_payer', 'approval_rejected', 'approval_expired', 'disclosure_changed',
         'budget_insufficient', 'payment_mismatch', 'content_unverifiable',
         'unknown_submission_restart', 'idempotent_charge_restart', 'wrong_resource',
         'search_quote_mismatch', 'missing_pax_metadata', 'paused_pax_metadata', 'wrong_pax_metadata')
SOURCE_PATHS = ('agent/Cargo.toml', 'agent/Cargo.lock', 'agent/crates',
    'platform/Cargo.toml', 'platform/Cargo.lock', 'platform/cli',
    'human/crates/layerx-human-kms', 'human/wallet/attestor',
    'interop/Cargo.toml', 'interop/Cargo.lock', 'interop/crates/x-websearch',
    'interop/crates/layerx-x402', 'programs/crates', 'programs/sdk/rust',
    'tools/qualification/paxeer-x/mcp_web_payer.py',
    'tools/qualification/paxeer-x/mcp_daemon_revocation.py',
    'tools/qualification/paxeer-x/agent_http_bounds.py',
    'tools/qualification/paxeer-x/agent_tenant_readiness.py',
    'tools/qualification/paxeer-x/fixtures/agentd_fixture.py')


class Missing(ValueError):
    pass


def require(value, reason):
    if not value:
        raise ValueError(reason)


def needed(value, reason):
    if not value:
        raise Missing(reason)
    return value


def digest(path):
    require(not any(part.startswith('.env') for part in Path(path).parts), 'credential environment hashing refused')
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def closed(pairs):
    value = {}
    for key, item in pairs:
        require(key not in value, 'duplicate JSON field')
        value[key] = item
    return value


def private(path, directory=False):
    path = Path(path).absolute()
    require(not any(part.startswith('.env') for part in path.parts), 'credential environment owner input refused')
    needed(path.exists(), 'required protected owner input missing')
    require(path.resolve() == path, 'protected path symlink refused')
    info = path.stat()
    require(info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == (0o700 if directory else 0o600)
        and (stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode) and info.st_nlink == 1),
        'protected owner path permissions or type invalid')
    return path


def load(path):
    path = private(path)
    require(path.stat().st_size <= 4_194_304, 'protected input exceeds bound')
    return json.loads(path.read_text(), object_pairs_hook=closed)


def save(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())


def artifact(path):
    path = Path(path).absolute()
    needed(path.is_file(), 'genuine owner executable is missing')
    require(path.resolve() == path and os.access(path, os.X_OK), 'canonical executable required')
    with path.open('rb') as stream:
        require(stream.read(4) == b'\x7fELF', 'genuine production ELF required')
    return {'path': str(path), 'sha256': digest(path), 'bytes': path.stat().st_size}


def source():
    require(not subprocess.check_output(['git', 'status', '--porcelain', '--untracked-files=no'], cwd=ROOT),
            'clean immutable candidate required')
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    paths = subprocess.check_output(['git', 'ls-files', '-z', '--', *SOURCE_PATHS], cwd=ROOT).split(b'\0')
    selected = [os.fsdecode(path) for path in paths if path and
        not any(part.startswith('.env') for part in Path(os.fsdecode(path)).parts)]
    files = [name for name in selected if Path(name).suffix in
             ('.rs', '.py', '.toml', '.lock', '.go', '.mod', '.sum', '.kvx')]
    needed('agent/crates/layerx-agentd-host/src/payer.rs' in files,
           'actual retained daemon ProductionWebPayer producer source missing')
    needed('tools/qualification/paxeer-x/mcp_web_payer.py' in files, 'published harness source missing')
    for name in files:
        committed = subprocess.check_output(['git', 'show', revision + ':' + name], cwd=ROOT)
        require(hashlib.sha256(committed).hexdigest() == digest(ROOT / name), 'candidate source differs from committed blob')
    return {'revision': revision, 'files': {name: digest(ROOT / name) for name in sorted(files)}}


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


REVOCATION = module('paid_web_revocation', Path(__file__).with_name('mcp_daemon_revocation.py'))
HTTP = REVOCATION.HTTP


def build_steps(rows, owner=None):
    needed(isinstance(rows, list) and rows, 'actual successful build commands and logs required')
    for row in rows:
        require(set(row) == {'command', 'exit_code', 'log', 'sha256'} and row['exit_code'] == 0 and
            isinstance(row['command'], list) and row['command'] and
            all(isinstance(item, str) for item in row['command']), 'closed successful producer build record required')
        log = private(row['log'])
        require(digest(log) == row['sha256'] and log.stat().st_size > 0, 'actual build log provenance changed')
    if owner == 'x-websearch':
        require(any('x-websearch' in row['command'] and 'build' in row['command'] and
            '--locked' in row['command'] for row in rows), 'actual locked x-websearch build missing')
    elif owner == 'attestor':
        require(any(Path(row['command'][0]).name == 'go' and 'build' in row['command'] and
            any('cmd/attestor' in arg for arg in row['command']) for row in rows), 'actual attestor build missing')


def build(directory):
    before = source()
    target = Path(needed(os.environ.get(PREFIX + 'AGENT_TARGET'), 'private Agent target required')).absolute()
    platform = Path(needed(os.environ.get(PREFIX + 'PLATFORM_TARGET'), 'private CLI target required')).absolute()
    require(target != platform and ROOT not in target.parents and ROOT not in platform.parents,
            'distinct external compilation targets required')
    cargo = os.environ.get(PREFIX + 'CARGO', '/root/.cargo/bin/cargo')
    commands = [(['flock', '/root/lx-cargo/agent-build.lock', cargo, 'build', '--locked', '--manifest-path', 'agent/Cargo.toml',
        '-p', 'layerx-agentd-host', '--bin', 'layerx-agentd', '-p', 'layerx-mcp',
        '--bin', 'layerx-mcp', '--message-format=json'], target),
        (['flock', '/root/lx-cargo/platform-build.lock', cargo, 'build', '--locked', '--manifest-path', 'platform/Cargo.toml',
         '-p', 'layerx-platform-cli', '--bin', 'layerx', '--message-format=json'], platform)]
    rows, binaries = [], {}
    deadline = time.monotonic() + 1200
    for index, (command, cache) in enumerate(commands):
        log = directory / f'build-{index}.log'
        environment = dict(os.environ, CARGO_TARGET_DIR=str(cache), CARGO_BUILD_JOBS='4')
        with log.open('xb') as stream:
            process = subprocess.Popen(command, cwd=ROOT, env=environment,
                stdin=subprocess.DEVNULL, stdout=stream, stderr=subprocess.STDOUT, start_new_session=True)
            try:
                code = process.wait(timeout=max(1, deadline - time.monotonic()))
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                code = process.wait()
        rows.append({'command': command, 'exit_code': code, 'log': str(log), 'sha256': digest(log)})
        require(code == 0, 'focused production build failed; private log=' + str(log))
        for line in log.read_text().splitlines():
            if line.startswith('{'):
                event = json.loads(line)
                if event.get('reason') == 'compiler-artifact' and event.get('executable'):
                    name = event.get('target', {}).get('name')
                    if name in ('layerx-agentd', 'layerx-mcp', 'layerx'):
                        binaries[name] = artifact(event['executable'])
    require(set(binaries) == {'layerx-agentd', 'layerx-mcp', 'layerx'} and source() == before,
            'actual entrypoint build or immutable source binding absent')
    path = directory / 'artifacts.json'
    save(path, {'schema': 'paxeer-x.mcp-web-artifacts.v1', 'source': before,
                'steps': rows, 'artifacts': binaries})
    print(PREFIX + 'ARTIFACTS=' + str(path), flush=True)


class Stdio(REVOCATION.McpProcess):
    def start(self):
        self.starts += 1
        log = self.directory / (self.label + '-' + str(self.starts) + '.log')
        with log.open('xb') as output:
            self.process = subprocess.Popen([str(self.binary), 'mcp', 'serve',
                '--daemon-binding', str(self.path)], cwd=self.fixture.directory,
                stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=output)
        self.message('initialize', {})

    def message(self, method, params):
        self.sequence += 1
        request = {'jsonrpc': '2.0', 'id': self.sequence, 'method': method, 'params': params}
        self.process.stdin.write(json.dumps(request, separators=(',', ':')).encode() + b'\n')
        self.process.stdin.flush()
        with selectors.DefaultSelector() as selector:
            selector.register(self.process.stdout, selectors.EVENT_READ)
            require(selector.select(30), 'real stdio MCP response timeout')
            raw = self.process.stdout.readline(1_048_577)
        require(raw.endswith(b'\n') and len(raw) <= 1_048_576, 'stdio MCP framing invalid')
        response = json.loads(raw, object_pairs_hook=closed)
        require(response.get('id') == self.sequence and response.get('jsonrpc') == '2.0'
                and 'error' not in response, 'actual CLI MCP protocol response refused')
        save(self.directory / f'{self.label}-response-{self.starts}-{self.sequence}.json', response)
        return response['result']

    def stop(self):
        super().stop()
        if self.process is not None:
            for stream in (self.process.stdin, self.process.stdout):
                if stream is not None:
                    stream.close()


class Corpus:
    def __init__(self, directory, manifest, profile):
        self.directory, self.manifest, self.profile = directory, manifest, profile
        self.clients, self.owners = {}, []
        self.daemon = None
        self.fixture = HTTP.AgentdFixture(directory / 'fixture')
        require(profile['schema'] == 'paxeer-x.mcp-web-owner-fixture.v1' and set(profile) ==
            {'schema', 'owners', 'participants', 'cases', 'payer_seed', 'pricing', 'producer_provenance'}, 'closed paid owner fixture required')
        require(set(profile['participants']) == {'socket', 'stdio'} and set(profile['cases']) == set(CASES) and
            all(set(row) == {'socket', 'stdio'} for row in profile['cases'].values()),
                'both entrypoints and all required paid cases must be provisioned')
        self.fixture_provenance()
        self.fixture.artifacts.update({name: dict(row, source_revision=manifest['source']['revision'])
            for name, row in manifest['artifacts'].items() if name != 'layerx'})
        config = HTTP.daemon_config(self.fixture)
        self.start_owners()
        for mode, participants in profile['participants'].items():
            require(set(participants) == set(CASES), 'each case requires a genuine independently bound session')
            for name, row in participants.items():
                require(set(row) == {'binding_seed', 'credential_request'}, 'closed participant identity required')
                credential = self.fixture.envelope(row['credential_request'])['credential']
                cls = REVOCATION.McpProcess if mode == 'socket' else Stdio
                binary = manifest['artifacts']['layerx-mcp' if mode == 'socket' else 'layerx']['path']
                label = mode + '-' + name.replace('.', '-')
                client = cls(self.fixture, Path(binary), row['binding_seed'], credential, directory, label, True)
                self.clients[(mode, name)] = client
        require(len({str(client.path) for client in self.clients.values()}) == 2 * len(CASES),
                'MCP process binding paths overlap')
        config['LAYERX_AGENTD_MCP_BINDINGS'] = ','.join(str(client.path) for client in self.clients.values())
        needed(profile['payer_seed'] in self.fixture.values, 'genuine daemon payer seed missing')
        config['LAYERX_AGENTD_MCP_PAYERS'] = self.fixture.values[profile['payer_seed']]
        payers = load(config['LAYERX_AGENTD_MCP_PAYERS'])
        require(isinstance(payers, list) and payers, 'actual retained daemon payer configuration required')
        for (mode, name), client in self.clients.items():
            bound = [row for row in payers if row.get('tenant') == client.credential['tenant'] and
                row.get('session_id') == client.credential['session_id']]
            require(len(bound) == (0 if name == 'missing_payer' else 1),
                    'actual daemon payer session attachment does not match case authority')
        self.daemon = HTTP.Daemon(Path(manifest['artifacts']['layerx-agentd']['path']), config,
                                 directory, int(self.fixture.values['agent_program_port']), self.fixture)
        self.daemon.start()
        self.serial = 0

    def fixture_provenance(self):
        document = load(self.profile['producer_provenance'])
        require(set(document) == {'schema', 'source', 'steps', 'seed_sha256', 'upstream_sha256'} and
            document['schema'] == 'paxeer-x.mcp-web-produced-fixture.v1' and
            document['source'] == self.manifest['source'], 'actual owner-produced fixture source provenance missing')
        build_steps(document['steps'])
        seeds = self.fixture.document.get('seeds', {})
        require(set(document['seed_sha256']) == set(seeds), 'genuine fixture seed inventory incomplete')
        for name, raw in seeds.items():
            root = Path(raw)
            entries = [root] if root.is_file() else [root] + sorted(root.rglob('*'))
            require(not any(any(part.startswith('.env') for part in path.parts) for path in entries),
                    'credential environment seed traversal refused before reading or copying')
            paths = [path for path in entries if path.is_file()]
            require(not any(part.startswith('.env') for part in root.parts) and
                not any(any(part.startswith('.env') for part in path.parts) for path in paths),
                'credential environment paths cannot be hashed or copied')
            require(paths and all(not path.is_symlink() for path in paths), 'genuine fixture seed unavailable')
            actual = {str(path.relative_to(root)) if root.is_dir() else '.': digest(path) for path in paths}
            require(actual == document['seed_sha256'][name], 'owner-produced fixture state changed')
        require(document['upstream_sha256'] == {name: row['sha256'] for name, row in self.fixture.artifacts.items()},
                'fixture does not bind actual native producer artifacts')

    def start_owners(self):
        require(set(self.profile['owners']) == {'attestor', 'x-websearch'}, 'real KMS and paid-web owners required')
        for role, row in self.profile['owners'].items():
            require(set(row) == {'artifact', 'source_revision', 'source_paths', 'source_sha256',
                'arguments', 'environment', 'ready_socket', 'build_provenance'}, 'closed genuine owner artifact required')
            require(row['source_revision'] == self.manifest['source']['revision'] and
                row['source_paths'] and set(row['source_paths']) == set(row['source_sha256']),
                'owner source provenance unavailable')
            for name in row['source_paths']:
                require(name in self.manifest['source']['files'] and row['source_sha256'][name] ==
                    self.manifest['source']['files'][name], 'owner artifact does not bind current production source')
            provenance = load(row['build_provenance'])
            require(set(provenance) == {'schema', 'source_revision', 'source_sha256', 'artifact', 'steps'} and
                provenance['schema'] == 'paxeer-x.mcp-web-owner-build.v1' and
                provenance['source_revision'] == row['source_revision'] and
                provenance['source_sha256'] == row['source_sha256'] and
                provenance['artifact'] == row['artifact'], 'genuine owner build provenance does not bind executable')
            build_steps(provenance['steps'], role)
            require(artifact(row['artifact']['path']) == row['artifact'], 'real owner executable changed')
            expected = 'interop/crates/x-websearch/src/main.rs' if role == 'x-websearch' else 'human/wallet/attestor/cmd/attestor/main.go'
            needed(expected in row['source_paths'], 'actual production owner entrypoint binding missing')
            environment = {'PATH': os.environ.get('PATH', os.defpath)}
            environment.update(self.fixture.substitute(row['environment']))
            command = [row['artifact']['path']] + [item.format_map(self.fixture.values) for item in row['arguments']]
            log = (self.directory / (role + '.log')).open('xb')
            process = subprocess.Popen(command, cwd=self.fixture.directory, env=environment,
                stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT)
            self.owners.append((process, log, row))
            endpoint = row['ready_socket'].format_map(self.fixture.values)
            deadline = time.monotonic() + 60
            while True:
                require(process.poll() is None and time.monotonic() < deadline, 'real paid owner did not start')
                try:
                    if endpoint.startswith('/'):
                        with socket.socket(socket.AF_UNIX) as connection:
                            connection.connect(endpoint)
                    else:
                        host, port = endpoint.rsplit(':', 1)
                        require(host in ('127.0.0.1', 'localhost'), 'disposable owner endpoint must be loopback')
                        socket.create_connection((host, int(port)), timeout=1).close()
                    break
                except OSError:
                    time.sleep(0.1)

    def challenge(self, client, call, expected, wrong_resource=False):
        require(set(expected) == {'asset', 'payTo', 'payer', 'amount', 'currency', 'scheme', 'network'},
                'closed exact offer identity required')
        arguments = call['arguments']
        if call['tool'] == 'web.search':
            target = '/search?q=' + quote(arguments['query'], safe='')
        elif call['tool'] == 'web.fetch':
            target = '/fetch?url=' + quote(arguments['url'], safe='')
        else:
            require(call['tool'] != 'web.content', 'stored content must retain its unpaid policy')
            raise ValueError('actual paid operation missing')
        endpoint = urlsplit(client.binding['web']['endpoint'])
        require(endpoint.scheme == 'http' and endpoint.hostname in ('127.0.0.1', 'localhost', '::1')
            and endpoint.username is None and endpoint.password is None, 'actual paid owner must be disposable loopback')
        connection = http.client.HTTPConnection(endpoint.hostname, endpoint.port, timeout=20)
        try:
            connection.request('GET', target)
            response = connection.getresponse()
            raw = response.read(1_048_577)
            require(response.status == 402 and len(raw) <= 1_048_576, 'real unpaid resource did not return bounded 402')
            header = response.getheader('PAYMENT-REQUIRED')
            needed(header, 'actual 402 challenge header missing')
            challenge = json.loads(base64.b64decode(header, validate=True), object_pairs_hook=closed)
        finally:
            connection.close()
        require(challenge.get('x402Version') == 2 and isinstance(challenge.get('resource'), dict) and
            isinstance(challenge['resource'].get('url'), str), 'actual challenge resource identity missing')
        resource = urlsplit(challenge['resource']['url'])
        matches = challenge['resource']['url'] == 'http://' + endpoint.netloc + target
        require(matches != wrong_resource, 'real challenge does not exhibit required resource binding or mismatch')
        offers = [item for item in challenge.get('accepts', []) if item.get('scheme') == expected['scheme'] and
            item.get('network') == expected['network'] and
            item.get('extra', {}).get('layerx', {}).get('currency') == expected['currency']]
        require(len(offers) == 1 and all(offers[0].get(key) == expected[key] for key in
            ('scheme', 'network', 'amount', 'asset', 'payTo')), 'real 402 offer differs from the exact declared payment')
        path = self.directory / (client.label + '-challenge.json')
        save(path, {'target': target, 'challenge': challenge, 'body_sha256': hashlib.sha256(raw).hexdigest()})
        return str(path)

    def rpc(self, key, verified=False):
        self.serial += 1
        request = self.fixture.envelope(key)
        request['request_id'] = str(900000 + self.serial)
        status, response = HTTP.rpc_request(self.daemon, request)
        save(self.directory / f'rpc-{self.serial}.json', {'status': status, 'response': response})
        require(status == 200 and response.get('request_id') == request['request_id'], 'actual owner RPC refused')
        if verified:
            require(response.get('verification_status', {}).get('state') == 'achieved' and
                response['verification_status'].get('level') in ('StateProven', 'CheckpointFinalised', 'SettlementAnchored'),
                'payment state lacks authenticated protocol authority')
        return response['value']

    def invocation(self, client, call, label, absence=False):
        command = [self.manifest['artifacts']['layerx-agentd']['path'], '--mcp-evidence',
            self.daemon.config['LAYERX_AGENT_HUMAN_STORE'], client.credential['tenant'],
            call['idempotency_key'], call['tool']]
        result = subprocess.run(command, cwd=self.fixture.directory, stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
        require(result.returncode == 0, 'real daemon invocation serializer failed')
        record = json.loads(result.stdout, object_pairs_hook=closed)
        if record is None and absence:
            save(self.directory / (label + '-invocation.json'), None)
            return None
        needed(record is not None, 'genuine durable invocation record absent')
        REVOCATION.invocation_evidence(record, client.credential, call)
        save(self.directory / (label + '-invocation.json'), record)
        return record

    def balance(self, request):
        value = self.rpc(request, True)
        needed(isinstance(value, dict) and isinstance(value.get('amount'), str) and
            re.fullmatch('[0-9]+', value['amount']), 'actual STATE_PROVEN balance request required')
        return int(value['amount'])

    def case(self, mode, name):
        row = self.profile['cases'][name][mode]
        require(set(row) == {'call', 'before_requests', 'track_request', 'balance_request',
            'expected_refusal', 'expected_charge', 'offer'}, 'closed real paid case declaration required')
        client = self.clients[(mode, name)]
        client.start()
        require(type(row['expected_charge']) is bool and
            (name != 'web.search' or row['call']['tool'] == 'web.search' and row['call']['arguments'].get('currency') == 'PAX') and
            (name not in ('web.fetch', 'web.content') or row['call']['tool'] == name) and
            (name not in ('web.search', 'web.fetch', 'web.content') or row['expected_refusal'] is None) and
            (name != 'web.content' or not row['expected_charge']) and
            (name in ('web.search', 'web.fetch', 'web.content', 'unknown_submission_restart', 'idempotent_charge_restart')
                or row['expected_refusal'] is not None) and
            (name not in ('wrong_tenant', 'wrong_session', 'missing_payer', 'approval_rejected',
                'approval_expired', 'disclosure_changed', 'budget_insufficient', 'wrong_resource',
                'search_quote_mismatch', 'missing_pax_metadata', 'paused_pax_metadata', 'wrong_pax_metadata')
                or not row['expected_charge']) and
            (name not in ('web.search', 'web.fetch', 'unknown_submission_restart', 'idempotent_charge_restart') or row['expected_charge']),
            'required success, refusal or paid-state case contract changed')
        for request in row['before_requests']:
            self.rpc(request)
        before = self.balance(row['balance_request'])
        challenge = self.challenge(client, row['call'], row['offer'], name == 'wrong_resource') if row['offer'] is not None else None
        if name == 'unknown_submission_restart':
            durable = self.invocation(client, row['call'], client.label + '-unknown')
            require(durable['native_preparation'] is not None and durable['response'] is None and
                durable['phase'] == 'effect_started' and
                any(event['kind'] == 'native_transmission_started' for event in durable['events']) and
                any(event['kind'] == 'effect_unknown' for event in durable['events']),
                'genuine owner-produced unknown submission record is missing')
            unknown = self.rpc(row['track_request'])
            require(unknown.get('submission', {}).get('state') == 'Unknown' and
                unknown.get('activity_id'), 'actual native owner does not retain the unknown activity')
            self.daemon.stop()
            client.stop()
            self.daemon.start()
            client.start()
            refused, value = client.call(row['call'])
        else:
            refused, value = client.call(row['call'])
        if row['expected_refusal'] is not None:
            require(isinstance(row['expected_refusal'], dict) and
                row['expected_refusal'].get('state') in ('refused', 'unknown') and
                isinstance(row['expected_refusal'].get('class'), str) and
                isinstance(row['expected_refusal'].get('reason'), str) and row['expected_refusal']['reason'],
                'closed typed actual daemon refusal required')
            require(refused and 'result' not in value and value.get('refusal') == row['expected_refusal'],
                    'missing exact refusal or unauthenticated fallback content returned')
        else:
            require(not refused and value.get('tool') == row['call']['tool'] and
                value.get('result', {}).get('untrusted') is True, 'verified paid content was not returned')
            content = value['result']
            if row['call']['tool'] != 'web.content':
                offer = row['offer']
                settlement = content.get('settlement', {})
                require(set(offer) == {'asset', 'payTo', 'payer', 'amount', 'currency', 'scheme', 'network'} and
                    all(settlement.get(key) == item for key, item in offer.items()) and
                    settlement.get('verificationLevel') == 'sequencer-signed', 'returned payment does not bind exact offer')
                if row['call']['tool'] == 'web.search' and row['offer']['currency'] == 'PAX':
                    pricing = self.profile['pricing']
                    require(pricing == {'usd_numerator': 1, 'usd_denominator': 1000,
                        'pax_usd_numerator': 336, 'pax_usd_denominator': 25,
                        'asset_decimals': pricing['asset_decimals'], 'rounding': 'ceiling'} and
                        type(pricing['asset_decimals']) is int and 0 <= pricing['asset_decimals'] <= 38,
                        'exact existing search quote or rounding policy missing')
                    units = (10**pricing['asset_decimals'] + 13439) // 13440
                    require(offer['currency'] == 'PAX' and offer['amount'] == str(units),
                            'search price differs from exact 1/13440 PAX rounded units')
        durable = self.invocation(client, row['call'], client.label, name in ('wrong_tenant', 'wrong_session'))
        tracked = None
        if row['expected_charge']:
            needed(row['track_request'], 'actual payment tracking request missing')
            deadline = time.monotonic() + 90
            while True:
                tracked = self.rpc(row['track_request'])
                if tracked.get('submission', {}).get('state') == 'Executed':
                    break
                require(time.monotonic() < deadline, 'unknown payment never reconciled to verified execution')
                time.sleep(0.2)
            tracked = self.rpc(row['track_request'], True)
            require(tracked.get('submission', {}).get('state') == 'Executed', 'executed payment lost verified authority')
            facts = REVOCATION.READINESS.receipt_facts(tracked['receipt']['canonical_bytes'], tracked['activity_id'])
            if row['offer'] is not None and not refused:
                require(value['result']['settlement']['receiptDigest'] == facts['receipt_sha256'],
                        'delivered content and actual daemon receipt disagree')
            first = tracked
            paid_balance = self.balance(row['balance_request'])
            require(paid_balance < before, 'actual verified execution did not debit the payer')
            client.stop()
            self.daemon.stop()
            self.daemon.start()
            client.start()
            replay_refused, replay_value = client.call(row['call'])
            require((replay_refused, replay_value) == (refused, value), 'restart changed durable paid delivery')
            require(self.rpc(row['track_request'], True) == first, 'replay replaced its actual native payment')
        after = self.balance(row['balance_request'])
        if not row['expected_charge']:
            require(before == after, 'refused or unpaid content call charged the payer')
        else:
            require(after == paid_balance, 'restart replay charged the actual payer a second time')
        client.stop()
        return {'mode': mode, 'case': name, 'durable_phase': durable['phase'] if durable else None, 'challenge': challenge,
            'balance_before': str(before), 'balance_after': str(after),
            'activity_id': tracked['activity_id'] if tracked else None}

    def close(self):
        for client in self.clients.values():
            client.stop()
        if self.daemon is not None:
            self.daemon.stop()
        if hasattr(self, 'fixture'):
            self.fixture.cleanup()
        for process, log, _ in reversed(self.owners):
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
            log.close()


def verify(directory):
    before = source()
    manifest = load(needed(os.environ.get(PREFIX + 'ARTIFACTS'), 'candidate artifact manifest required'))
    require(set(manifest) == {'schema', 'source', 'steps', 'artifacts'} and
        manifest['schema'] == 'paxeer-x.mcp-web-artifacts.v1' and manifest['source'] == before and
        set(manifest['artifacts']) == {'layerx-agentd', 'layerx-mcp', 'layerx'}, 'complete immutable artifact binding required')
    build_steps(manifest['steps'])
    require(len(manifest['steps']) == 2 and
        'layerx-agentd-host' in manifest['steps'][0]['command'] and
        'layerx-mcp' in manifest['steps'][0]['command'] and
        'layerx-platform-cli' in manifest['steps'][1]['command'], 'actual focused entrypoint build commands missing')
    for row in manifest['artifacts'].values():
        require(artifact(row['path']) == row, 'candidate executable changed')
    needed(os.environ.get('PAXEER_X_AGENTD_UPSTREAM_ARTIFACTS'), 'real native upstream owner fixture required')
    profile = load(needed(os.environ.get(PREFIX + 'FIXTURE'), 'protected real paid owner fixture required'))
    for namespace in ('net', 'mnt', 'pid'):
        parent = os.environ.get('PAXEER_X_HTTP_PARENT_' + namespace.upper())
        needed(parent and os.readlink('/proc/self/ns/' + namespace) != parent,
               'actual disposable isolated namespace required')
    corpus = None
    results = []
    try:
        corpus = Corpus.__new__(Corpus)
        corpus.__init__(directory, manifest, profile)
        for name in CASES:
            for mode in ('socket', 'stdio'):
                results.append(corpus.case(mode, name))
                save(directory / ('case-' + mode + '-' + name.replace('.', '-') + '.json'), results[-1])
        require(len(results) == 2 * len(CASES) and source() == before, 'paid process case inventory incomplete')
        for row in manifest['artifacts'].values():
            require(artifact(row['path']) == row, 'candidate artifact changed during process run')
        for _, _, row in corpus.owners:
            require(artifact(row['artifact']['path']) == row['artifact'], 'genuine paid owner artifact changed')
        for path in directory.rglob('*.log'):
            raw = path.read_bytes()
            require(not any(secret.encode() in raw for secret in corpus.fixture.secret_values() if secret),
                    'actual process logs disclosed protected authority')
        return results
    except (ValueError, KeyError, TypeError, OSError, subprocess.SubprocessError, HTTP.Failure, HTTP.FixtureRefused) as failure:
        if corpus is not None and hasattr(corpus, 'fixture'):
            if isinstance(failure, (Missing, FileNotFoundError)):
                raise Missing(corpus.fixture.redact(str(failure))) from None
            raise ValueError(corpus.fixture.redact(str(failure))) from None
        raise
    finally:
        if corpus is not None:
            corpus.close()


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--build-artifacts', action='store_true')
    parser.add_argument('--worker', action='store_true', help=argparse.SUPPRESS)
    args = parser.parse_args()
    directory, results, code, error = None, [], 1, None
    try:
        base = private(needed(os.environ.get(PREFIX + 'EVIDENCE'), 'private evidence directory required'), True)
        require(base != ROOT and ROOT not in base.parents, 'evidence must remain outside checkout')
        directory = base / ('build-' if args.build_artifacts else 'verify-')
        directory = directory.with_name(directory.name + str(time.time_ns()))
        directory.mkdir(mode=0o700)
        if args.build_artifacts:
            build(directory)
        elif args.worker:
            subprocess.run(['ip', 'link', 'set', 'lo', 'up'], check=True, timeout=10)
            results = verify(directory)
        else:
            environment = dict(os.environ)
            for namespace in ('net', 'pid', 'mnt'):
                environment['PAXEER_X_HTTP_PARENT_' + namespace.upper()] = os.readlink('/proc/self/ns/' + namespace)
            log = directory / 'namespace.log'
            with log.open('xb') as output:
                process = subprocess.Popen(['unshare', '--mount', '--net', '--pid', '--fork', '--mount-proc',
                    sys.executable, str(Path(__file__).resolve()), '--worker'], env=environment,
                    stdin=subprocess.DEVNULL, stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
                try:
                    code = process.wait(timeout=1500)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGTERM)
                    try:
                        process.wait(timeout=10)
                    except subprocess.TimeoutExpired:
                        os.killpg(process.pid, signal.SIGKILL)
                        process.wait()
                    raise ValueError('bounded actual process qualifier timed out')
            needed(code in (0, 1, 78), 'actual disposable namespace authority unavailable; private log=' + str(log))
            if code != 0:
                error = 'actual isolated qualifier refused; private log=' + str(log)
                return code
            evidence = [line[len('EVIDENCE '):] for line in log.read_text().splitlines() if line.startswith('EVIDENCE ')]
            require(len(evidence) == 1, 'actual isolated producer result missing')
            child = private(evidence[0])
            require(base in child.parents, 'actual isolated evidence escaped protected directory')
            record = load(child)
            require(record['exit_code'] == 0 and record['qualified'] is True and
                len(record['cases']) == 2 * len(CASES), 'actual isolated producer case inventory incomplete')
            results = record['cases']
        code = 0
    except (Missing, FileNotFoundError) as failure:
        code, error = 78, str(failure)
    except (ValueError, KeyError, TypeError, IndexError, OSError, subprocess.SubprocessError,
            HTTP.Failure, HTTP.FixtureRefused) as failure:
        code, error = 1, str(failure)
    finally:
        if directory is not None:
            path = directory / 'result.json'
            save(path, {'command': COMMAND + (' --build-artifacts' if args.build_artifacts else ''),
                'revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(),
                'required_cases': 2 * len(CASES), 'exit_code': code, 'cases': results, 'skipped': 0,
                'qualified': code == 0 and not args.build_artifacts and len(results) == 2 * len(CASES), 'error': error})
            print('EVIDENCE ' + str(path), flush=True)
    return code


if __name__ == '__main__':
    sys.exit(main())
