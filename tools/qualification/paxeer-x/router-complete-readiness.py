#!/usr/bin/env python3
import hashlib
import http.client
import ipaddress
import json
import os
from pathlib import Path
import re
import signal
import shutil
import socket
import ssl
import stat
import subprocess
import sys
import tempfile
import time
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[3]
ROSTER = {'durable_store', 'event_producer', 'paxeer_chain', 'core_agent_boundary',
          'public_core', 'independent_receipt_authority', 'identity', 'program_registry'}
KERNEL = {'core_agent_boundary', 'public_core', 'independent_receipt_authority', 'identity', 'program_registry'}
EXECUTABLES = {'gateway': 'layerx-gateway', 'public_core': 'layerx-core-boundary', 'identity': 'layerx-identity'}
KERNEL_ENV = ('LAYERX_GATEWAY_COMPONENT_URL', 'LAYERX_GATEWAY_COMPONENT_TOKEN_FILE',
              'LAYERX_GATEWAY_PUBLIC_CORE_URL', 'LAYERX_GATEWAY_AUTHORITY_URL',
              'LAYERX_GATEWAY_AUTHORITY_TOKEN_FILE', 'LAYERX_GATEWAY_IDENTITY_URL',
              'LAYERX_GATEWAY_IDENTITY_TOKEN_FILE', 'LAYERX_GATEWAY_PROGRAM_REGISTRY_URL',
              'LAYERX_GATEWAY_PROGRAM_REGISTRY_TOKEN_FILE', 'LAYERX_GATEWAY_CLIENT_IDENTITY_PKCS12',
              'LAYERX_GATEWAY_CLIENT_IDENTITY_PASSWORD_FILE', 'LAYERX_GATEWAY_SEQUENCER_PUBLIC_KEY_FILE',
              'LAYERX_GATEWAY_SEQUENCER_ID_FILE', 'LAYERX_GATEWAY_SEQUENCER_FIRST_BATCH_FILE',
              'LAYERX_GATEWAY_SEQUENCER_LAST_BATCH_FILE', 'LAYERX_GATEWAY_KEY_PROVISIONING_KEY_FILE',
              'LAYERX_GATEWAY_MODULE_REGISTRY_FILE')


def require(condition, reason):
    if not condition:
        raise RuntimeError(reason)


def private(path, directory=False):
    path = Path(path)
    info = path.lstat()
    require(path.is_absolute() and path.resolve() == path and info.st_uid == os.geteuid()
            and not info.st_mode & 0o077
            and (stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode) and info.st_nlink == 1),
            'protected input ownership, path or permissions refused')
    return path


def document(variable):
    require(os.environ.get(variable), variable + ' is required')
    path = private(os.environ[variable])
    require(path.stat().st_size <= 1048576, 'input document exceeds bound')
    return json.loads(path.read_text())


def git(*arguments):
    return subprocess.check_output(['git', *arguments], cwd=ROOT, text=True).strip()


def artifact(row, revision, name=None):
    path = Path(row['path'])
    require(path.is_absolute() and path.resolve() == path and path.is_file() and os.access(path, os.X_OK),
            'prebuilt executable path refused')
    require(not path.stat().st_mode & 0o022 and path.stat().st_uid == os.geteuid(), 'prebuilt executable ownership refused')
    if revision is not None:
        require(row['source_revision'] == revision, 'prebuilt source revision differs')
    else:
        require(row.get('kind') == 'system-package' and row.get('package_version'), 'system package provenance is required')
    if name is not None:
        require(path.name == name, 'unexpected production executable')
    with path.open('rb') as stream:
        require(stream.read(4) == b'\x7fELF', 'prebuilt native executable required')
        stream.seek(0)
        require(hashlib.file_digest(stream, 'sha256').hexdigest() == row['sha256'], 'prebuilt executable changed')
    return path


def start_ticks(pid):
    return Path(f'/proc/{pid}/stat').read_text().rsplit(')', 1)[1].split()[19]


def namespace(pid):
    return os.readlink(f'/proc/{pid}/ns/net')


def endpoint(value):
    parsed = urlsplit(value)
    require(parsed.scheme in ('https', 'rediss') and parsed.hostname and parsed.port
            and parsed.username is None and parsed.password is None
            and parsed.path in ('', '/') and not parsed.query and not parsed.fragment,
            'explicit canonical TLS endpoint required')
    addresses = socket.getaddrinfo(parsed.hostname, parsed.port, type=socket.SOCK_STREAM)
    require(addresses and all(ipaddress.ip_address(row[4][0]).is_loopback for row in addresses),
            'qualification endpoints must resolve only within isolated loopback')
    return parsed


def owns_listener(pid, value):
    parsed = endpoint(value)
    inodes = set()
    for descriptor in Path(f'/proc/{pid}/fd').iterdir():
        try:
            target = os.readlink(descriptor)
        except FileNotFoundError:
            continue
        if target.startswith('socket:['):
            inodes.add(target[8:-1])
    for table in ('tcp', 'tcp6'):
        for line in Path('/proc/self/net/' + table).read_text().splitlines()[1:]:
            fields = line.split()
            if fields[3] == '0A' and int(fields[1].split(':')[1], 16) == parsed.port and fields[9] in inodes:
                return
    raise RuntimeError('real service process does not own its configured listener')


def inputs():
    require(not git('status', '--porcelain', '--untracked-files=no'), 'committed source required')
    revision = git('rev-parse', 'HEAD')
    built = document('PAXEER_X_ROUTER_READINESS_BUILD_MANIFEST')
    fixture = document('PAXEER_X_ROUTER_READINESS_FIXTURE')
    require(built['source_revision'] == fixture['source_revision'] == revision
            and built['source_tree'] == git('rev-parse', 'HEAD^{tree}')
            and built['build_exit'] == 0, 'source-bound successful scoped build required')
    binaries = {name: artifact(built['artifacts'][name], revision, expected)
                for name, expected in EXECUTABLES.items()}
    binaries['gateway_tests'] = artifact(built['artifacts']['gateway_tests'], revision)
    require(fixture['version'] == 1, 'fixture version refused')
    anchor = fixture['namespace_anchor_pid']
    require(type(anchor) is int and anchor > 1 and start_ticks(anchor) == fixture['namespace_anchor_start_ticks'],
            'retained namespace owner changed')
    return revision, binaries, fixture


def readiness_document(status, body, unavailable=frozenset(), chain_only=False):
    require(status == (503 if unavailable and not chain_only else 200), 'gateway readiness status mismatch')
    backends = body['backends']
    require(set(backends) == ROSTER, 'configured dependency roster omitted or duplicated a backend')
    for name in ROSTER:
        expected = {'state': 'unavailable', 'reason': 'not_configured'} if chain_only and name in KERNEL else (
            {'state': 'unavailable', 'reason': 'unreachable'} if name in unavailable else {'state': 'ready', 'reason': 'ready'})
        require(backends[name] == expected, 'backend state mismatch: ' + name)
    require(body['status'] == ('degraded' if unavailable or chain_only else 'ready'), 'complete readiness status mismatch')
    require(body['valid_until_ms'] > body['observed_at_ms'], 'readiness freshness invalid')
    return body


class Run:
    def __init__(self, revision, binaries, fixture, directory):
        self.revision, self.binaries, self.fixture, self.directory = revision, binaries, fixture, directory
        self.children, self.environments, self.logs = {}, {}, []
        self.cases = []
        self.context = ssl.create_default_context(cafile=str(private(fixture['ca_pem'])))
        self.context.minimum_version = ssl.TLSVersion.TLSv1_2
        self.context.load_cert_chain(str(private(fixture['client_certificate_pem'])), str(private(fixture['client_key_pem'])))
        state = private(fixture['state_root'], directory=True)
        for name in EXECUTABLES:
            path = private(fixture[name]['environment_file'])
            values = json.loads(path.read_text())
            require(isinstance(values, dict) and all(isinstance(k, str) and isinstance(v, str) and k.startswith('LAYERX_')
                    for k, v in values.items()), 'explicit production environment required')
            require('LAYERX_GATEWAY_ROUTE_BINDINGS_FILE' not in values,
                    'focused kernel fixture must not launch unrelated product routes')
            for key, value in values.items():
                if key.endswith('_URL'):
                    if key in ('LAYERX_CORE_NODE_URL', 'LAYERX_CORE_REPLICA_URL'):
                        parsed_node = urlsplit(value)
                        require(parsed_node.scheme == 'http' and parsed_node.hostname == '127.0.0.1'
                                and parsed_node.port and parsed_node.path in ('', '/')
                                and not parsed_node.username and not parsed_node.password
                                and not parsed_node.query and not parsed_node.fragment,
                                'native fixture HTTP transport must remain on isolated loopback')
                    else:
                        endpoint(value)
                if key.endswith('_SOCKET'):
                    socket_path = Path(value)
                    require(socket_path.is_absolute() and socket_path.resolve() == socket_path
                            and socket_path.is_relative_to(state), 'fixture socket escaped retained state')
            self.environments[name] = values
            parsed = endpoint(fixture[name]['endpoint'])
            listen_key = {'gateway': 'LAYERX_GATEWAY_LISTEN', 'public_core': 'LAYERX_CORE_LISTEN', 'identity': 'LAYERX_IDENTITY_LISTEN'}[name]
            require(values[listen_key] == '127.0.0.1:' + str(parsed.port), 'explicit isolated listener binding required')
            require(type(fixture[name]['gid']) is int and 0 <= fixture[name]['gid'] < 2**31, 'explicit service peer group required')
        for name, variable in (('public_core', 'LAYERX_CORE_STATE_DIR'), ('identity', 'LAYERX_IDENTITY_STATE_DIR')):
            retained = private(self.environments[name][variable], directory=True)
            require(retained.is_relative_to(state), 'service state must remain in isolated retained state')
        gateway = self.environments['gateway']
        require(gateway['LAYERX_GATEWAY_LISTENER'] == 'tls', 'real gateway TLS listener required')
        require(gateway['LAYERX_GATEWAY_PUBLIC_CORE_URL'] == fixture['public_core']['endpoint']
                and gateway['LAYERX_GATEWAY_IDENTITY_URL'] == fixture['identity']['endpoint'], 'configured service binding mismatch')
        require(gateway['LAYERX_GATEWAY_NETWORK_ID'] != gateway['LAYERX_GATEWAY_PROTOCOL_NETWORK_ID'],
                'network label must exercise numeric public-core semantics')
        core_token = private(self.environments['public_core']['LAYERX_CORE_RECEIPT_EVENTS_TOKEN_FILE']).read_bytes()
        require(core_token == private(gateway['LAYERX_GATEWAY_COMPONENT_TOKEN_FILE']).read_bytes(),
                'gateway and core must consume the existing scoped credential')
        self.dependencies()

    def dependencies(self):
        observed = set()
        services = {'component': 'layerx-agent-boundary', 'authority': 'layerx-receipt-authority',
                    'registry': 'layerx-program-registry', 'redis': 'redis-server', 'paxeer': 'layerx-paxeer-boundary',
                    'event-source': 'layerx-event-source', 'webhooks': 'layerx-webhooks'}
        for row in self.fixture['dependencies']:
            pid = row['pid']
            require(type(pid) is int and pid > 1 and start_ticks(pid) == row['start_ticks']
                    and namespace(pid) == namespace('self'), 'real dependency process changed or escaped isolation')
            require(row['service'] in services, 'unrecognized production dependency')
            executable = artifact(row['artifact'], None if row['service'] == 'redis' else self.revision, services[row['service']])
            require(Path(f'/proc/{pid}/exe').resolve() == executable, 'dependency executable does not match its source-bound artifact')
            for value in row['endpoints']:
                owns_listener(pid, value)
                require(value not in observed, 'duplicate dependency endpoint')
                observed.add(value)
        gateway = self.environments['gateway']
        expected = {gateway[name] for name in ('LAYERX_GATEWAY_COMPONENT_URL', 'LAYERX_GATEWAY_AUTHORITY_URL',
                                              'LAYERX_GATEWAY_PROGRAM_REGISTRY_URL', 'LAYERX_GATEWAY_REDIS_URL')}
        expected.update(json.loads(gateway['LAYERX_GATEWAY_PAXEER_RPC_URLS']))
        expected.update(value for key, value in gateway.items() if key.startswith('LAYERX_EVENTS_') and key.endswith('_URL'))
        require(expected <= observed, 'configured dependency missing from genuine process roster')

    def stop(self, name):
        process = self.children.pop(name, None)
        if process is not None:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)

    def start(self, name, changes=None, remove=()):
        previous = self.children.get(name)
        old_pid = previous.pid if previous is not None else None
        self.stop(name)
        environment = {'PATH': os.environ['PATH'], 'LANG': 'C', **self.environments[name], **(changes or {})}
        for key in remove:
            environment.pop(key, None)
        log = (self.directory / (name + '.log')).open('ab')
        self.logs.append(log)
        process = subprocess.Popen([str(self.binaries[name])], cwd=ROOT, env=environment,
                                   stdin=subprocess.DEVNULL, stdout=log, stderr=log,
                                   group=self.fixture[name]['gid'], extra_groups=[])
        self.children[name] = process
        require(process.pid != old_pid, 'restart must create a new process')
        self.wait(lambda: self.listening(name))

    def listening(self, name):
        require(self.children[name].poll() is None, 'production service exited before serving')
        owns_listener(self.children[name].pid, self.fixture[name]['endpoint'])
        return True

    def wait(self, predicate):
        deadline = time.monotonic() + 45
        while time.monotonic() < deadline:
            try:
                if predicate():
                    return
            except (OSError, RuntimeError, http.client.HTTPException):
                pass
            time.sleep(.2)
        raise RuntimeError('bounded real service transition timed out')

    def request(self, name, path, method='GET', payload=None, token_file=None):
        parsed = endpoint(self.fixture[name]['endpoint'])
        require(parsed.scheme == 'https', 'HTTPS application request required')
        headers = {'Accept': 'application/json', 'Content-Type': 'application/json', 'Connection': 'close'}
        if token_file:
            token = private(token_file).read_text().strip()
            require(0 < len(token) <= 4096 and token.isascii() and all(33 <= ord(c) <= 126 for c in token), 'bounded service credential required')
            headers['Authorization'] = 'Bearer ' + token
        body = None if payload is None else json.dumps(payload).encode()
        connection = http.client.HTTPSConnection(parsed.hostname, parsed.port, timeout=20, context=self.context)
        try:
            connection.request(method, path, body=body, headers=headers)
            response = connection.getresponse()
            require(response.getheader('Content-Type') == 'application/json', 'exact response media type required')
            raw = response.read(1048577)
            require(len(raw) <= 1048576 and not 300 <= response.status < 400, 'bounded nonredirected response required')
            return response.status, json.loads(raw)
        finally:
            connection.close()

    def readiness(self, unavailable=frozenset(), chain_only=False):
        status, body = self.request('gateway', '/readyz/core' if not chain_only else '/readyz')
        result = readiness_document(status, body, unavailable, chain_only)
        if not chain_only:
            product_status, product = self.request('gateway', '/readyz')
            require(product_status == status and product['backends'] == body['backends'],
                    'public readiness disagrees with the configured dependency vector')
        return result

    def usable(self):
        account = self.fixture['account_id']
        require(re.fullmatch('[0-9a-f]{64}', account), 'actual account selector required')
        path = '/v1/accounts/' + account + '/balance'
        direct_status, direct = self.request('public_core', path)
        status, routed = self.request('gateway', path)
        require(direct_status == status == 200 and direct == routed and routed['ok'] is True,
                'healthy public read is not the actual core result')
        require(routed['result']['account_id'] == account and routed['result']['verification'] == 'state_proven',
                'public read lacks state-proven canonical ownership')
        status, keys = self.request('gateway', '/v1/keys', token_file=self.fixture['session_token_file'])
        require(status == 200 and keys['ok'] is True and isinstance(keys['keys'], list),
                'healthy gateway session path is not usable')

    def record(self, name):
        self.cases.append(name)
        (self.directory / 'cases.json').write_text(json.dumps(self.cases))
        print('PASS ' + name, flush=True)

    def close(self):
        for name in tuple(self.children):
            self.stop(name)
        for log in self.logs:
            log.close()


def worker(directory):
    def terminate(_signal, _frame):
        raise RuntimeError('qualification interrupted')
    signal.signal(signal.SIGTERM, terminate)
    revision, binaries, fixture = inputs()
    require(namespace('self') == namespace(fixture['namespace_anchor_pid'])
            and namespace('self') != os.environ['PAXEER_X_ROUTER_PARENT_NETNS'], 'isolated retained network namespace required')
    run = Run(revision, binaries, fixture, private(directory, directory=True))
    try:
        for name in ('public_core', 'identity', 'gateway'):
            run.start(name)
        run.wait(lambda: bool(run.readiness()))
        baseline = run.readiness()
        run.usable()
        run.record('healthy-configured-roster-and-real-read-session-paths')
        gateway = run.environments['gateway']
        case = {'ca_der': gateway['LAYERX_GATEWAY_OUTBOUND_CA_DER'],
                'client_pkcs12': gateway['LAYERX_GATEWAY_CLIENT_IDENTITY_PKCS12'],
                'client_password_file': gateway['LAYERX_GATEWAY_CLIENT_IDENTITY_PASSWORD_FILE'],
                'core_endpoint': fixture['public_core']['endpoint'],
                'identity_endpoint': fixture['identity']['endpoint'],
                'core_token_file': gateway['LAYERX_GATEWAY_COMPONENT_TOKEN_FILE'],
                'identity_token_file': gateway['LAYERX_GATEWAY_IDENTITY_TOKEN_FILE'],
                'identity_wrong_role_token_file': gateway['LAYERX_GATEWAY_IDENTITY_PROVISIONING_TOKEN_FILE'],
                'protocol_network_id': int(gateway['LAYERX_GATEWAY_PROTOCOL_NETWORK_ID']),
                'network_label': gateway['LAYERX_GATEWAY_NETWORK_ID'],
                'wire_version': gateway['LAYERX_GATEWAY_LXP_WIRE_VERSION']}
        case_path = run.directory / 'contract-case.json'
        case_path.write_text(json.dumps(case))
        with (run.directory / 'contract.log').open('w') as log:
            result = subprocess.run([str(binaries['gateway_tests']),
                'complete_readiness_contract_tests::real_core_and_identity_readiness_contract',
                '--exact', '--nocapture', '--test-threads=1'], cwd=ROOT,
                env={'PATH': os.environ['PATH'], 'PAXEER_X_ROUTER_READINESS_CASE': str(case_path)},
                stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT, timeout=90)
        output = (run.directory / 'contract.log').read_text()
        require(result.returncode == 0 and '1 passed; 0 failed; 0 ignored' in output
                and len(re.findall(r'^PAXEER_X_ROUTER_CONTRACT_CASES=[1-9][0-9]*$', output, re.M)) == 1,
                'real response contract/authentication/negative-shape cases did not execute')
        run.record('real-authenticated-contracts-and-network-shape-refusals')
        for name in ('public_core', 'identity'):
            run.stop(name)
            run.wait(lambda: bool(run.readiness({name})))
            run.dependencies()
            run.record('only-' + name + '-unavailable')
            run.start(name)
            run.wait(lambda: bool(run.readiness()))
            run.usable()
            run.record(name + '-restart-retained-state-recovery')
        run.start('public_core', remove=('LAYERX_CORE_RECEIPT_EVENTS_TOKEN_FILE',))
        run.wait(lambda: bool(run.readiness({'public_core'})))
        status, missing = run.request('public_core', '/internal/readyz',
                                      token_file=gateway['LAYERX_GATEWAY_COMPONENT_TOKEN_FILE'])
        require(status == 503 and missing['error']['code'] == 'gateway_readiness_not_configured',
                'missing existing gateway credential lacks a named refusal')
        run.record('missing-core-gateway-credential-refused')
        run.start('public_core')
        run.wait(lambda: bool(run.readiness()))
        run.usable()
        run.record('core-credential-restored-with-same-retained-state')
        wrong = int(gateway['LAYERX_GATEWAY_PROTOCOL_NETWORK_ID']) % (2**32 - 1) + 1
        run.start('gateway', {'LAYERX_GATEWAY_PROTOCOL_NETWORK_ID': str(wrong)})
        run.wait(lambda: bool(run.readiness({'public_core', 'independent_receipt_authority'})))
        run.record('wrong-network-real-router-refuses-core-and-authority')
        run.start('gateway')
        run.wait(lambda: bool(run.readiness()))
        require(run.readiness()['backends'] == baseline['backends'], 'restart changed the configured roster')
        run.usable()
        run.record('router-restart-same-roster-and-path-recovery')
        for omitted in ('public_core', 'identity'):
            observed = dict(baseline, backends=dict(baseline['backends']))
            del observed['backends'][omitted]
            try:
                readiness_document(200, observed)
            except RuntimeError:
                pass
            else:
                raise RuntimeError('omitted configured backend escaped gate roster requirement')
        run.start('gateway', remove=('LAYERX_GATEWAY_PUBLIC_CORE_URL',))
        status, omitted = run.request('gateway', '/readyz/core')
        require(omitted['backends']['public_core'] == {'state': 'unavailable', 'reason': 'not_configured'},
                'real configuration omission is not identified')
        try:
            readiness_document(status, omitted)
        except RuntimeError:
            pass
        else:
            raise RuntimeError('real configured-roster omission escaped the focused gate')
        run.record('configured-roster-omission-refused')
        run.start('gateway')
        run.wait(lambda: bool(run.readiness()))
        run.usable()
        run.record('configured-roster-restored')
        run.start('gateway', changes={
            'LAYERX_GATEWAY_SERVICE_CLIENT_IDENTITY_PKCS12': gateway['LAYERX_GATEWAY_CLIENT_IDENTITY_PKCS12'],
            'LAYERX_GATEWAY_SERVICE_CLIENT_IDENTITY_PASSWORD_FILE': gateway['LAYERX_GATEWAY_CLIENT_IDENTITY_PASSWORD_FILE'],
        }, remove=KERNEL_ENV)
        run.wait(lambda: bool(run.readiness(chain_only=True)))
        status, chain = run.request('gateway', '/rpc', 'POST', {'jsonrpc': '2.0', 'id': 1, 'method': 'eth_chainId', 'params': []})
        require(status == 200 and chain['result'] == '0x7d' and chain['id'] == 1, 'chain-only Paxeer method failed')
        status, refusal = run.request('gateway', '/rpc', 'POST', {'jsonrpc': '2.0', 'id': 2,
                                        'method': 'lx_getAccount', 'params': [fixture['account_id']]})
        require(status == 200 and refusal['id'] == 2 and refusal['error']['code'] == -32010
                and refusal['error']['data'] == {'code': 'kernel_unavailable', 'backend': 'public_core', 'reason': 'not_configured'}
                and 'result' not in refusal, 'chain-only typed kernel refusal changed')
        status, session_refusal = run.request('gateway', '/v1/keys', token_file=fixture['session_token_file'])
        require(status == 503 and session_refusal['error'] == {
            'code': 'kernel_unavailable', 'backend': 'identity', 'reason': 'not_configured'},
            'chain-only identity refusal changed')
        run.record('chain-only-startup-chain-method-and-typed-kernel-refusal')
        run.dependencies()
        require(git('rev-parse', 'HEAD') == revision and not git('status', '--porcelain', '--untracked-files=no'), 'source changed during qualification')
        print('PAXEER_X_GATE tests=' + str(len(run.cases)) + ' skipped=0')
        return 0
    finally:
        run.close()


def main():
    os.umask(0o077)
    if len(sys.argv) == 3 and sys.argv[1] == '--worker':
        return worker(sys.argv[2])
    require(len(sys.argv) == 1, 'gate accepts no arguments')
    revision, binaries, fixture = inputs()
    require(os.geteuid() == 0, 'explicit isolated network namespace entry requires root')
    require(shutil.which('nsenter') is not None, 'nsenter is a required qualification prerequisite')
    require(namespace('self') != namespace(fixture['namespace_anchor_pid']), 'running host namespace is not an isolated fixture')
    parent = private(os.environ['PAXEER_X_EVIDENCE_DIR'], directory=True)
    directory = Path(tempfile.mkdtemp(prefix='router-complete-readiness-', dir=parent))
    environment = dict(os.environ, PAXEER_X_ROUTER_PARENT_NETNS=namespace('self'))
    process = subprocess.Popen(['nsenter', '--net=/proc/' + str(fixture['namespace_anchor_pid']) + '/ns/net',
                                sys.executable, str(Path(__file__).resolve()), '--worker', str(directory)],
                               cwd=ROOT, env=environment, start_new_session=True)
    try:
        code = process.wait(timeout=840)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGTERM)
        try:
            process.wait(timeout=30)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait(timeout=5)
        code = 124
    (directory / 'result.json').write_text(json.dumps({'revision': revision, 'exit_code': code,
        'command': 'timeout 15m python3 tools/qualification/paxeer-x/router-complete-readiness.py'}))
    return code


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.SubprocessError, http.client.HTTPException) as error:
        print('router-complete-readiness: refused: ' + str(error), file=sys.stderr)
        sys.exit(1)
