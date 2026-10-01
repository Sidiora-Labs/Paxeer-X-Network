#!/usr/bin/env python3
import argparse
import ast
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import ssl
import stat
import subprocess
import sys
import tempfile
import time
import unittest
import urllib.request

ROOT = Path(__file__).resolve().parents[3]
BINARIES = ('paxd', 'attestor', 'layerxd', 'layerx-genesis-build', 'layerx-module-registry', 'layerx-custody-proof')
NETWORK = 2411
ASSETS = {name: hashlib.sha256(('layerx-asset:125:' + name).encode()).hexdigest() for name in ('PAX', 'USDL')}
USDL = '0x85FcD13735F4309833A503EE804ea32395851479'
CUSTODY = '0x0000000000000000000000000000000000001013'
ANCHOR = '0x0000000000000000000000000000000000001014'
ENV = {'PATH': '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin', 'LC_ALL': 'C', 'PYTHONDONTWRITEBYTECODE': '1'}


def require(value, reason):
    if not value:
        raise RuntimeError('foundation refused: ' + reason)


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def run(argv, *, env=None, timeout=90, input=None, check=True):
    result = subprocess.run([str(x) for x in argv], input=input, capture_output=True,
                            env={**ENV, **(env or {})}, timeout=timeout)
    if check:
        require(result.returncode == 0, Path(str(argv[0])).name + ' producer exit=' + str(result.returncode))
    return result


def private(path, directory=False):
    path = Path(path)
    info = path.lstat()
    require(not path.is_symlink() and info.st_uid == os.geteuid(), 'unexpected owner or symlink')
    require(stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode), 'unexpected file type')
    require(stat.S_IMODE(info.st_mode) == (0o700 if directory else 0o600), 'unexpected mode')
    if not directory:
        require(info.st_nlink == 1 and info.st_size <= 8 * 1024 * 1024, 'file links or size')


def write_json(path, value):
    descriptor = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'w') as stream:
        json.dump(value, stream, sort_keys=True)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())


def artifact_contract(path):
    require(bool(path), 'PAXEER_X_FOUNDATION_ARTIFACTS is required; missing source-bound prebuilt ' + ', '.join(BINARIES))
    private(path)
    value = json.loads(Path(path).read_text())
    require(value.get('version') == 1, 'artifact manifest version')
    revision = value.get('source_revision', '')
    require(re.fullmatch('[0-9a-f]{40}', revision), 'artifact source revision')
    tree = run(['git', '-C', ROOT, 'rev-parse', revision + '^{tree}']).stdout.decode().strip()
    require(tree == value.get('source_tree'), 'artifact source tree mismatch')
    changed = run(['git', '-C', ROOT, 'diff', '--name-only', revision, 'HEAD']).stdout.decode().splitlines()
    require(all(name.startswith(('spec/', 'tools/paxeer-x/gates/', 'tools/bringup/tests/')) or name in ('AGENTS.md', 'GOTCHA.kvx', 'tools/bringup/build-foundation-artifacts.sh') for name in changed), 'selected dependency source differs from artifact source')
    require(not run(['git', '-C', ROOT, 'status', '--porcelain']).stdout, 'selected checkout is dirty')
    rows = value.get('artifacts', {})
    require(set(rows) == set(BINARIES), 'artifact set differs from declared six executables')
    for name in BINARIES:
        row = rows.get(name, {})
        target = Path(row.get('path', ''))
        require(target.is_absolute() and target.is_file() and not target.is_symlink() and os.access(target, os.X_OK), 'missing executable ' + name)
        require(row.get('source_revision') == revision, 'wrong source for ' + name)
        require(re.fullmatch('[0-9a-f]{64}', row.get('sha256', '')), 'missing digest for ' + name)
        require(digest(target) == row['sha256'], 'digest mismatch for ' + name)
    for row in value.get('runtime_libraries', []):
        target = Path(row.get('path', ''))
        require(target.is_absolute() and target.is_file() and not target.is_symlink(), 'runtime library missing')
        require(row.get('source_revision') == revision and digest(target) == row.get('sha256'), 'runtime library source or digest mismatch')
    return value


def shell_function(source, name):
    single = re.search(r'^' + re.escape(name) + r'\(\) \{[^\n]*\} *$', source, re.M)
    if single:
        return single.group(0)
    match = re.search(r'^' + re.escape(name) + r'\(\) \{\n.*?^\}', source, re.M | re.S)
    require(match is not None, 'production function absent: ' + name)
    return match.group(0)


def rpc(method, params=None, comet=False):
    request = urllib.request.Request('http://127.0.0.1:' + ('26657' if comet else '8545'),
                                    json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params or ([] if not comet else {})}).encode(),
                                    {'Content-Type': 'application/json'})
    with urllib.request.urlopen(request, timeout=3) as response:
        value = json.load(response)
    require('error' not in value and 'result' in value, 'actual chain RPC rejected request')
    return value['result']


def wait_for(probe, processes):
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        require(all(process.poll() is None for process in processes), 'dependency process exited before protocol readiness')
        try:
            value = probe()
            if value:
                return value
        except (OSError, ValueError, RuntimeError):
            pass
        time.sleep(0.5)
    raise RuntimeError('foundation refused: bounded protocol readiness expired')


class Foundation:
    def __init__(self, directory, artifacts):
        self.directory = Path(directory)
        self.artifacts = artifacts
        self.processes = []
        self.producers = []
        self.logs = []
        self.runtime_env = {'LD_LIBRARY_PATH': str(self.directory / 'inputs/lib')}

    def binary(self, name):
        return self.artifacts['artifacts'][name]['path']

    def produce(self, argv, **kwargs):
        kwargs['env'] = {**self.runtime_env, **kwargs.get('env', {})}
        result = run(argv, check=False, **kwargs)
        record = {'command': [str(x) for x in argv], 'exit_code': result.returncode}
        self.producers.append(record)
        with (self.directory / 'producers.jsonl').open('a') as stream:
            stream.write(json.dumps(record, sort_keys=True) + '\n')
            stream.flush()
            os.fsync(stream.fileno())
        require(result.returncode == 0, Path(str(argv[0])).name + ' producer exit=' + str(result.returncode))
        return result.stdout

    def start(self, name, argv, env):
        log = (self.directory / (name + '.log')).open('ab')
        self.logs.append(log)
        process = subprocess.Popen([str(x) for x in argv], stdin=subprocess.DEVNULL, stdout=log, stderr=log,
                                   env={**ENV, **self.runtime_env, **env}, start_new_session=True)
        self.processes.append(process)
        return process

    def stop(self):
        for process in reversed(self.processes):
            if process.poll() is None:
                process.terminate()
        for process in reversed(self.processes):
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
        self.processes.clear()
        for log in self.logs:
            log.close()
        self.logs.clear()

    def key(self, name):
        pem = self.directory / 'keys' / (name + '.pem')
        self.produce(['openssl', 'genpkey', '-algorithm', 'ED25519', '-out', pem])
        public = self.produce(['openssl', 'pkey', '-in', pem, '-pubout', '-outform', 'DER'])[-32:].hex()
        seed = self.produce(['openssl', 'pkey', '-in', pem, '-outform', 'DER'])[-32:].hex()
        (pem.parent / (name + '.seed')).write_text(seed)
        return public

    def tls(self):
        source = (ROOT / 'platform/hosted/tests/beta-cluster.sh').read_text()
        functions = '\n'.join(shell_function(source, name) for name in ('random_hex', 'write_token', 'issue_cert', 'issue_client_identity', 'issue_server_identity'))
        ca = self.directory / 'ca'
        script = 'set -euo pipefail\numask 077\n' + functions + '\n'
        for authority in ('peer', 'operator'):
            root = ca / authority
            script += 'CA_DIR=' + shlex.quote(str(root)) + '\nmkdir -m0700 -p "$CA_DIR"\n'
            script += 'openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$CA_DIR/ca.key"\n'
            script += 'openssl req -x509 -new -key "$CA_DIR/ca.key" -days 2 -sha256 -subj ' + shlex.quote('/CN=Disposable foundation ' + authority) + ' -addext basicConstraints=critical,CA:TRUE,pathlen:0 -addext keyUsage=critical,keyCertSign,cRLSign -out "$CA_DIR/ca.crt"\n'
            script += 'openssl x509 -in "$CA_DIR/ca.crt" -outform DER -out "$CA_DIR/ca.der"\n'
            if authority == 'peer':
                for index in range(1, 6):
                    script += 'issue_cert attestor-' + str(index) + ' paxeer-attestor-' + str(index) + ' serverAuth,clientAuth ' + shlex.quote('DNS:paxeer-attestor-' + str(index) + '.internal,IP:fd24:11::' + str(index)) + '\n'
                for name, cn, usage, san in (
                    ('human-attestor-client', 'layerx-human-components', 'clientAuth', ''),
                    ('human-kms', 'layerx-human-kms', 'serverAuth', 'DNS:layerx-human-kms,IP:127.0.0.1'),
                    ('human-kms-client', 'layerx-human-components', 'clientAuth', ''),
                    ('human-kms-executor', 'layerx-human-movement', 'clientAuth', ''),
                    ('identity', 'layerx-identity', 'serverAuth', 'DNS:layerx-identity,IP:127.0.0.1'),
                    ('registry', 'layerx-program-registry', 'serverAuth', 'DNS:layerx-program-registry,IP:127.0.0.1')):
                    script += 'issue_cert ' + ' '.join(map(shlex.quote, (name, cn, usage, san))) + '\n'
                for name in ('human-event-client', 'registry-event-client'):
                    script += 'issue_client_identity ' + name + ' layerx-' + name + '\n'
                script += "issue_server_identity human layerx-human 'DNS:layerx-human,IP:127.0.0.1'\n"
            else:
                script += 'issue_client_identity operator foundation-operator\n'
        self.produce(['bash', '-se'], input=script.encode())
        from cryptography import x509
        from cryptography.hazmat.primitives import serialization
        from cryptography.hazmat.primitives.serialization import pkcs12
        for root in (ca / 'peer', ca / 'operator'):
            for leaf in root.iterdir():
                if not leaf.is_dir():
                    continue
                cert = x509.load_der_x509_certificate((leaf / 'cert.der').read_bytes())
                key = serialization.load_der_private_key((leaf / 'key.der').read_bytes(), None)
                require(cert.public_key().public_bytes(serialization.Encoding.DER, serialization.PublicFormat.SubjectPublicKeyInfo) == key.public_key().public_bytes(serialization.Encoding.DER, serialization.PublicFormat.SubjectPublicKeyInfo), 'TLS key/certificate mismatch')
                for bundle in leaf.glob('*.p12'):
                    private_key, certificate, authorities = pkcs12.load_key_and_certificates(bundle.read_bytes(), (leaf / 'password').read_bytes())
                    require(private_key is not None and certificate == cert and len(authorities) == 1, 'PKCS12 consumer contract')
        require(digest(ca / 'peer/ca.der') != digest(ca / 'operator/ca.der'), 'attestor authority separation')

    def generate(self):
        d = self.directory
        for name in ('keys', 'governance', 'ca', 'attestors', 'downstream', 'genesis'):
            (d / name).mkdir(mode=0o700)
        sequencer = self.key('sequencer')
        treasury = self.key('treasury')
        authority = self.produce(['python3', ROOT / 'platform/hosted/node/checkpoint-authority.py', d / 'keys/deposit.pem']).decode().strip().removeprefix('0x')
        (d / 'keys/deployer.key').write_text('0x' + os.urandom(32).hex())
        deployer = self.produce(['python3', ROOT / 'platform/hosted/paxeer/evm.py', 'address', d / 'keys/deployer.key']).decode().strip()
        (d / 'keys/recipient.key').write_text('0x' + os.urandom(32).hex())
        recipient = self.produce(['python3', ROOT / 'platform/hosted/paxeer/evm.py', 'address', d / 'keys/recipient.key']).decode().strip()
        sequencer_id = hashlib.sha256(('layerx-sequencer:' + sequencer).encode()).hexdigest()
        common = ['--network-id', NETWORK, '--sequencer-id', sequencer_id, '--sequencer-public-key', sequencer]
        self.produce(['python3', ROOT / 'platform/hosted/paxeer/custody-genesis.py', *common, '--deposit-root-authority', authority,
                      '--asset', ASSETS['PAX'] + ':uhpx', '--asset', ASSETS['USDL'] + ':uusdl:' + USDL, '--output', d / 'governance/custody.json'])
        self.produce(['python3', ROOT / 'platform/hosted/paxeer/anchor-genesis.py', *common, '--authority-evm', deployer,
                      '--paxeer-chain-id', '125', '--threshold', '1', '--output', d / 'governance/anchor.json'])
        self.chain_env = {'PAXD': self.binary('paxd'), 'LAYERX_PAXEER_HOME': str(d / 'chain'), 'LAYERX_PAXEER_CHAIN_ID': '125',
                          'LAYERX_PAXEER_DEPLOYER_ADDRESS': deployer, 'LAYERX_PAXEER_CUSTODY_GENESIS_FILE': str(d / 'governance/custody.json'),
                          'LAYERX_PAXEER_ANCHOR_GENESIS_FILE': str(d / 'governance/anchor.json'), 'LAYERX_PAXEER_DEPOSIT_ROOT_AUTHORITY': authority}
        self.produce(['bash', ROOT / 'platform/hosted/paxeer/init-chain.sh'], env=self.chain_env, timeout=180)
        write_json(d / 'chain-environment.json', self.chain_env)
        self.start_chain()
        require(rpc('eth_chainId') == '0x7d', 'wrong actual EVM chain')
        require(rpc('eth_call', [{'to': CUSTODY, 'data': '0xb4768600'}, 'latest']) == '0x' + authority, 'actual deposit authority mismatch')
        require(rpc('eth_call', [{'to': CUSTODY, 'data': '0xaafcde84'}, 'latest']) == '0x' + ASSETS['PAX'], 'actual native asset mismatch')
        require(rpc('eth_call', [{'to': CUSTODY, 'data': '0xca65021c' + USDL[2:].lower().zfill(64)}, 'latest']) == '0x' + ASSETS['USDL'], 'actual USDL asset mismatch')
        require(int(rpc('eth_call', [{'to': USDL, 'data': '0x313ce567'}, 'latest']), 16) == 18, 'actual governed USDL decimals')
        height = int(wait_for(lambda: (value if int((value := rpc('status', comet=True))['sync_info']['latest_block_height']) >= 3 else None), self.processes)['sync_info']['latest_block_height'])
        self.produce([self.binary('layerx-custody-proof'), 'light-profile', '--rpc', 'http://127.0.0.1:26657', '--asset', '0x' + ASSETS['PAX'], '--network-id', NETWORK,
                      '--trusted-height', height - 1, '--trusting-period-seconds', '1209600', '--output', d / 'genesis/custody.profile'])
        require((d / 'genesis/custody.profile').stat().st_size == 223, 'canonical custody profile length')
        producer = (ROOT / 'tools/bringup/kernel-genesis.sh').read_text().split("\tpython3 - \"$genesis/metadata.lxgb\" \"$treasury_public\" \"${records[@]}\" <<'PY'\n", 1)
        require(len(producer) == 2, 'canonical metadata producer missing')
        metadata = producer[1].split('\nPY\n', 1)[0]
        self.produce(['python3', '-', d / 'genesis/metadata.lxgb', treasury, ASSETS['PAX'] + ':PAX:18', ASSETS['USDL'] + ':USDL:18'], input=metadata.encode())
        self.produce(['bash', ROOT / 'platform/hosted/node/bootstrap.sh', '--data-dir', d / 'node', '--run-dir', d / 'node-run', '--network-id', NETWORK,
                      '--sequencer-key', d / 'keys/sequencer.seed', '--treasury-key', d / 'keys/treasury.seed', '--asset', ASSETS['PAX'],
                      '--genesis-metadata', d / 'genesis/metadata.lxgb', '--custody-profile', d / 'genesis/custody.profile',
                      '--withdrawal-fee', '0', '--module-fees', ROOT / 'platform/hosted/node/genesis-module-fees.json',
                      '--migrations', ROOT / 'migrations/0007_history_index.sql', '--layerxd', self.binary('layerxd'), '--genesis-build', self.binary('layerx-genesis-build'), '--lni-uid', '4021', '--lni-gid', '4020'], timeout=180)
        for command in (
            ['treasury', d / 'downstream/binding-policy.json', NETWORK, ASSETS['PAX'], recipient[2:]],
            ['authorization', d / 'downstream/authorization.json', NETWORK, 125, ANCHOR, ANCHOR, CUSTODY, treasury, ASSETS['PAX'], recipient[2:], '--deposit-authority-key-file', d / 'keys/deposit.pem']):
            self.produce(['python3', ROOT / 'platform/hosted/tests/publication-policy.py', *command])
        registry_args = [self.binary('layerx-module-registry'), 'generate', '--network-id', NETWORK, '--protocol-version', '3', '--asset', ASSETS['PAX'], '--symbol', 'PAX', '--currency', 'PAX', '--decimals', '18', '--custody-profile', d / 'genesis/custody.profile']
        for module in (ROOT / 'platform/hosted/node/genesis-modules.conf').read_text().splitlines():
            if module:
                registry_args += ['--enable-module', module]
        registry = self.produce(registry_args)
        rendered = json.loads(registry)
        require(rendered.get('schema_version') == 2 and rendered.get('assets') == [{'asset': ASSETS['PAX'], 'symbol': 'PAX', 'currency': 'PAX', 'decimals': 18}] and bool(rendered.get('modules')), 'canonical registry output')
        (d / 'downstream/module-registry.json').write_bytes(registry)
        self.tls()
        self.stage_downstream()
        self.configure_attestors()
        self.start_attestors()
        self.check_attestors()
        write_json(d / 'downstream/environment.json', {'LAYERX_NODE_NETWORK_ID': str(NETWORK), 'LAYERX_HUMAN_MOVEMENT_PROVIDER_CUSTODY_PROFILE': str(d / 'genesis/custody.profile'),
                   'LAYERX_HUMAN_ATTESTOR_NODES': ','.join(str(i) + '=[fd24:11::' + str(i) + ']:8443' for i in range(1, 6))})
        write_json(d / 'downstream/endpoint-policy.json', {'network_namespace': os.readlink('/proc/self/ns/net'), 'external_egress': False,
                   'chain_id': 125, 'network_id': NETWORK, 'evm': 'http://127.0.0.1:8545', 'comet': 'http://127.0.0.1:26657',
                   'attestors': [{'node_id': str(i), 'ipv6': 'fd24:11::' + str(i), 'api_port': 8443, 'peer_port': 9443} for i in range(1, 6)]})
        self.manifest = {'version': 1, 'stage': 'dependency-foundation', 'purpose': 'disposable-test-only', 'network_id': NETWORK, 'chain_id': 125,
                         'source_revision': self.artifacts['source_revision'], 'source_tree': self.artifacts['source_tree'], 'artifacts': self.artifacts['artifacts'],
                         'assets': ASSETS, 'deposit_authority': authority, 'sequencer_public_key': sequencer, 'producers': self.producers,
                         'references': {'governance': 'governance', 'genesis': 'node/genesis', 'custody_profile': 'genesis/custody.profile', 'registry_input': 'downstream/module-registry.json',
                                        'tls': 'ca', 'environment': 'downstream/environment.json', 'endpoint_policy': 'downstream/endpoint-policy.json'},
                         'input_artifact_identities': self.artifacts.get('original_artifacts', self.artifacts['artifacts']),
                         'downstream_ready': False, 'pending_producers': {'kms_launch': '24.12', 'registry_authenticated_identity': '24.8', 'registry_owner_native_naming_policy_journal': '24.2', 'full_role_startup': '24.1'}}
        self.publish()

    def stage_downstream(self):
        d = self.directory
        root = d / 'downstream/human'
        for role in ('components', 'kms', 'movement', 'agent', 'identity', 'events'):
            (root / role).mkdir(mode=0o700, parents=True, exist_ok=True)
        rows = {
            'components/ca.der': 'ca.der', 'components/kms-client.der': 'human-kms-client/cert.der',
            'components/kms-client-key.der': 'human-kms-client/key.der',
            'components/attestor-ca.der': 'ca.der', 'components/attestor-client.der': 'human-attestor-client/cert.der',
            'components/attestor-client-key.der': 'human-attestor-client/key.der',
            'kms/ca.der': 'ca.der', 'kms/kms-client.der': 'human-kms-client/cert.der',
            'kms/kms-executor.der': 'human-kms-executor/cert.der', 'kms/kms-server.der': 'human-kms/cert.der',
            'kms/kms-server-key.der': 'human-kms/key.der', 'movement/ca.der': 'ca.der',
            'movement/kms-executor.der': 'human-kms-executor/cert.der', 'movement/kms-executor-key.der': 'human-kms-executor/key.der',
            'agent/ca.der': 'ca.der', 'identity/ca.der': 'ca.der', 'identity/server.der': 'identity/cert.der',
            'identity/server-key.der': 'identity/key.der', 'events/client.p12': 'human-event-client/client.p12',
            'events/password': 'human-event-client/password'}
        for target, source in rows.items():
            shutil.copyfile(d / 'ca/peer' / source, root / target)
        material_source = ROOT / 'platform/hosted/human/material.py'
        tree = ast.parse(material_source.read_text())
        function = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'assemble_policy')
        statements = [node for node in function.body if isinstance(node, ast.Assign) and
                      any(isinstance(target, ast.Subscript) and isinstance(target.value, ast.Name) and target.value.id == 'policy' and
                          isinstance(target.slice, ast.Constant) and target.slice.value == 'registry' for target in node.targets)]
        require(len(statements) == 1, 'canonical KMS registry projection missing')
        context = {'policy': {}, 'network': NETWORK, 'registry': json.loads((d / 'downstream/module-registry.json').read_text())}
        exec(compile(ast.Module(body=statements, type_ignores=[]), str(material_source), 'exec'), context)
        write_json(root / 'kms/registry.json', context['policy']['registry'])
        self.producers.append({'command': ['platform/hosted/human/material.py', 'assemble_policy:registry projection'], 'exit_code': 0})
        shutil.copyfile(d / 'genesis/custody.profile', root / 'movement/custody.profile')
        self.produce(['openssl', 'rand', '-out', root / 'kms/kms-seal', '32'])
        require((root / 'kms/kms-seal').stat().st_size == 32, 'KMS seal producer length')
        write_json(root / 'required-owner-inputs.json', {
            'trust-history': {'producer': '24.8', 'consumers': ['agent', 'security']},
            'principal-policy-and-authority': {'producer': '24.2', 'consumers': ['identity', 'security', 'movement', 'agent']},
            'owner-native-naming-journal': {'producer': '24.2', 'consumers': ['agent']}})

    def start_chain(self):
        self.start('chain', [self.binary('paxd'), 'start', '--home', self.directory / 'chain'], {'HOME': str(self.directory / 'chain')})
        wait_for(lambda: rpc('eth_chainId') == '0x7d', self.processes)

    def configure_attestors(self):
        from cryptography import x509
        from cryptography.hazmat.primitives import serialization
        ca = self.directory / 'ca/peer'
        pins = {}
        for i in range(1, 6):
            certificate = x509.load_pem_x509_certificate((ca / ('attestor-' + str(i)) / 'cert.pem').read_bytes())
            pins[i] = hashlib.sha256(certificate.public_key().public_bytes(serialization.Encoding.DER, serialization.PublicFormat.SubjectPublicKeyInfo)).hexdigest()
        for i in range(1, 6):
            root = self.directory / 'attestors' / str(i)
            root.mkdir(mode=0o700)
            (root / 'node.key').write_bytes(os.urandom(32))
            (root / 'backup.key').write_bytes(os.urandom(32))
            leaf = ca / ('attestor-' + str(i))
            env = {'ATTESTOR_NODE_ID': str(i), 'ATTESTOR_REGION': 'disposable', 'ATTESTOR_CHAIN_ID': '125', 'ATTESTOR_CEREMONY': 'false',
                   'ATTESTOR_LISTEN_ADDR': '[fd24:11::' + str(i) + ']:8443', 'ATTESTOR_PEER_LISTEN_ADDR': '[fd24:11::' + str(i) + ']:9443',
                   'ATTESTOR_PEERS': ','.join(str(j) + '=[fd24:11::' + str(j) + ']:9443' for j in range(1, 6) if j != i),
                   'ATTESTOR_PEER_PINS': ','.join(str(j) + '=' + pins[j] for j in range(1, 6) if j != i),
                   'ATTESTOR_NODE_KEY_FILE': str(root / 'node.key'), 'ATTESTOR_BACKUP_KEY_FILE': str(root / 'backup.key'), 'ATTESTOR_DATA_DIR': str(root / 'data'),
                   'ATTESTOR_TLS_CERT_FILE': str(leaf / 'cert.pem'), 'ATTESTOR_TLS_KEY_FILE': str(leaf / 'key.pem'), 'ATTESTOR_TLS_CA_FILE': str(ca / 'ca.crt'),
                   'ATTESTOR_OPERATOR_CA_FILE': str(self.directory / 'ca/operator/ca.crt'), 'ATTESTOR_RPC_URL': 'http://127.0.0.1:8545',
                   'ATTESTOR_POLICY_FILE': str(ROOT / 'human/wallet/deploy/attestor-policy.json'), 'ATTESTOR_KERNEL_POLICY_FILE': str(ROOT / 'human/wallet/deploy/attestor-kernel-policy.json'),
                   'ATTESTOR_ACTIVITY_TYPES': '0x10005,0x10007,0x30002,0x90005'}
            write_json(root / 'environment.json', env)

    def start_attestors(self):
        for i in range(1, 6):
            env = json.loads((self.directory / 'attestors' / str(i) / 'environment.json').read_text())
            self.start('attestor-' + str(i), [self.binary('attestor')], env)

    def check_attestors(self):
        ca = self.directory / 'ca/peer'
        context = ssl.create_default_context(cafile=str(ca / 'ca.crt'))
        context.load_cert_chain(ca / 'human-attestor-client/cert.pem', ca / 'human-attestor-client/key.pem')
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPSHandler(context=context))
        reports = {}
        for i in range(1, 6):
            def probe():
                with opener.open('https://[fd24:11::' + str(i) + ']:8443/health', timeout=3) as response:
                    health = json.load(response)
                require(health.get('node_id') == str(i), 'actual attestor identity mismatch')
                return health if health.get('ready') is True else None
            reports[str(i)] = wait_for(probe, self.processes)
        return reports

    def publish(self):
        for path in [self.directory, *self.directory.rglob('*')]:
            info = path.lstat()
            require(not path.is_symlink() and (path.is_dir() or path.is_file()) and info.st_uid == os.geteuid(), 'generated material ownership/type')
            path.chmod(0o700 if path.is_dir() or path.parent == self.directory / 'inputs' else 0o600)
        write_json(self.directory / 'foundation.json.next', self.manifest)
        os.replace(self.directory / 'foundation.json.next', self.directory / 'foundation.json')
        descriptor = os.open(self.directory, os.O_RDONLY | os.O_DIRECTORY)
        os.fsync(descriptor)
        os.close(descriptor)

    def restart(self):
        health_before = self.check_attestors()
        height_before = int(rpc('eth_blockNumber'), 16)
        first_block = rpc('eth_getBlockByNumber', ['0x1', False])['hash']
        self.stop()
        frozen = [*sorted((self.directory / 'keys').rglob('*')), *sorted((self.directory / 'ca').rglob('*')),
                  self.directory / 'chain/config/genesis.json', self.directory / 'node/genesis/genesis.manifest', self.directory / 'genesis/custody.profile']
        before = {str(path): digest(path) for path in frozen if path.is_file()}
        for path in frozen:
            private(path, path.is_dir())
        self.produce(['bash', ROOT / 'platform/hosted/paxeer/init-chain.sh'], env=self.chain_env)
        self.start_chain()
        self.start_attestors()
        health_after = self.check_attestors()
        for node in health_before:
            for field in ('node_id', 'share_count', 'refresh_epoch', 'audit_sequence', 'audit_head'):
                require(health_before[node][field] == health_after[node][field], 'retained attestor state changed')
        require(int(rpc('eth_blockNumber'), 16) >= height_before and rpc('eth_getBlockByNumber', ['0x1', False])['hash'] == first_block, 'retained chain history changed')
        require(before == {path: digest(path) for path in before}, 'retained foundation changed immutable material')
        require(rpc('eth_chainId') == '0x7d', 'retained network identity')


def namespace_worker(directory, artifact_file):
    require(os.geteuid() == 0, 'isolated namespace setup requires root')
    for kind, variable in (('net', 'PAXEER_X_PARENT_NETNS'), ('mnt', 'PAXEER_X_PARENT_MNTNS'), ('pid', 'PAXEER_X_PARENT_PIDNS')):
        parent = os.environ.get(variable, '')
        require(re.fullmatch(kind + r':\[[0-9]+\]', parent) and os.readlink('/proc/self/ns/' + kind) != parent, 'namespace isolation was not established: ' + kind)
    run(['mount', '-t', 'tmpfs', '-o', 'mode=0755,nosuid,nodev', 'foundation-run', '/run'])
    run(['ip', 'link', 'set', 'lo', 'up'])
    for i in range(1, 6):
        run(['ip', '-6', 'addr', 'add', 'fd24:11::' + str(i) + '/128', 'dev', 'lo', 'nodad'])
    links = json.loads(run(['ip', '-j', 'link']).stdout)
    require([link['ifname'] for link in links] == ['lo'], 'unexpected external network interface')
    artifacts = artifact_contract(artifact_file)
    global ROOT
    original_source = ROOT
    run(['mount', '-t', 'tmpfs', '-o', 'mode=0755,nosuid,nodev', 'foundation-tmp', '/tmp'])
    namespace_root = Path('/tmp/paxeer-foundation')
    namespace_root.mkdir(mode=0o755)
    source = namespace_root / 'source'
    source.mkdir(mode=0o755)
    run(['mount', '--bind', original_source, source])
    run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', source])
    data = namespace_root / 'data'
    data.mkdir(mode=0o700)
    run(['mount', '--bind', directory, data])
    hosts = namespace_root / 'hosts'
    hosts.write_text('127.0.0.1 localhost\n::1 localhost\n' + ''.join('fd24:11::' + str(i) + ' paxeer-attestor-' + str(i) + '.internal\n' for i in range(1, 6)))
    hosts.chmod(0o644)
    run(['mount', '--bind', hosts, '/etc/hosts'])
    run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', '/etc/hosts'])
    ROOT = source
    artifacts['original_artifacts'] = json.loads(json.dumps(artifacts['artifacts']))
    inputs = data / 'inputs'
    inputs.mkdir(mode=0o700)
    (inputs / 'lib').mkdir(mode=0o700)
    for name, row in artifacts['artifacts'].items():
        destination = inputs / name
        shutil.copyfile(row['path'], destination)
        destination.chmod(0o700)
        require(digest(destination) == row['sha256'], 'staged executable digest mismatch')
        row['path'] = str(destination)
    for row in artifacts.get('runtime_libraries', []):
        destination = inputs / 'lib' / Path(row['path']).name
        require(not destination.exists(), 'duplicate library name')
        shutil.copyfile(row['path'], destination)
        destination.chmod(0o600)
        require(digest(destination) == row['sha256'], 'staged library digest mismatch')
    for path in [data, inputs, *inputs.rglob('*')]:
        os.chown(path, 4020, 4020)
    directory = data
    os.chdir(ROOT)
    os.setgroups([])
    os.setgid(4020)
    os.setuid(4020)
    private(directory, True)
    foundation = Foundation(directory, artifacts)
    try:
        foundation.generate()
        foundation.restart()
        write_json(Path(directory) / 'result.json', {'real_chain': True, 'real_attestors': 5, 'retained_restart': True, 'downstream_ready': False})
    finally:
        foundation.stop()


class FixtureFoundation(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.artifact_file = os.environ.get('PAXEER_X_FOUNDATION_ARTIFACTS', '')
        cls.artifacts = artifact_contract(cls.artifact_file)
        cls.directory = Path(tempfile.mkdtemp(prefix='paxeer-foundation-', dir='/var/tmp'))
        cls.directory.chmod(0o700)
        require(os.geteuid() == 0, 'foundation gate requires disposable namespace authority')

    def test_01_missing_artifact_is_refused(self):
        with self.assertRaisesRegex(RuntimeError, 'required'):
            artifact_contract('')

    def test_02_wrong_source_is_refused(self):
        value = dict(self.artifacts, source_tree='0' * 40)
        path = self.directory / 'wrong-source.json'
        write_json(path, value)
        with self.assertRaisesRegex(RuntimeError, 'source tree mismatch'):
            artifact_contract(path)
        path.unlink()

    def test_03_failed_real_producer_publishes_nothing(self):
        output = self.directory / 'invalid-publication.json'
        result = run(['python3', ROOT / 'platform/hosted/tests/publication-policy.py', 'treasury', output, '0', ASSETS['PAX'], '1' * 40], check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(output.exists())

    def test_04_wrong_network_is_refused_by_real_producer(self):
        result = run(['bash', ROOT / 'platform/hosted/paxeer/init-chain.sh'], env={'LAYERX_PAXEER_CHAIN_ID': '124'}, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(b'not mapped', result.stderr)

    def test_05_private_material_refuses_symlink_owner_and_mode(self):
        target = self.directory / 'protected'
        target.write_bytes(b'private fixture boundary')
        target.chmod(0o600)
        link = self.directory / 'link'
        link.symlink_to(target)
        with self.assertRaisesRegex(RuntimeError, 'symlink'):
            private(link)
        link.unlink()
        target.chmod(0o644)
        with self.assertRaisesRegex(RuntimeError, 'mode'):
            private(target)
        target.chmod(0o600)
        os.chown(target, 4021, 4020)
        try:
            with self.assertRaisesRegex(RuntimeError, 'owner'):
                private(target)
        finally:
            os.chown(target, os.geteuid(), os.getegid())
            target.unlink()

    def test_06_actual_foundation_and_retained_restart(self):
        directory = self.directory / 'generation'
        directory.mkdir(mode=0o700)
        try:
            result = run(['unshare', '--mount', '--pid', '--fork', '--kill-child=KILL', '--net', '--ipc', '--uts', '--propagation', 'private', '--mount-proc',
                          'python3', Path(__file__).resolve(), '--worker', directory, '--artifacts', self.artifact_file],
                         env={'PAXEER_X_PARENT_NETNS': os.readlink('/proc/self/ns/net'), 'PAXEER_X_PARENT_MNTNS': os.readlink('/proc/self/ns/mnt'), 'PAXEER_X_PARENT_PIDNS': os.readlink('/proc/self/ns/pid')}, timeout=1200, check=False)
            (self.directory / 'worker.log').write_bytes(result.stdout + result.stderr)
            self.assertEqual(result.returncode, 0, 'isolated worker failed; private evidence: ' + str(self.directory / 'worker.log'))
            value = json.loads((directory / 'result.json').read_text())
            self.assertEqual(value, {'real_chain': True, 'real_attestors': 5, 'retained_restart': True, 'downstream_ready': False})
        finally:
            self.assertEqual(stat.S_IMODE(self.directory.stat().st_mode), 0o700)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--worker')
    parser.add_argument('--artifacts')
    args = parser.parse_args()
    os.umask(0o077)
    if args.worker:
        namespace_worker(args.worker, args.artifacts)
        return 0
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(FixtureFoundation)
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    print('PAXEER_X_GATE tests=' + str(result.testsRun) + ' skipped=' + str(len(result.skipped)), flush=True)
    return 0 if result.wasSuccessful() and result.testsRun and not result.skipped else 1


if __name__ == '__main__':
    raise SystemExit(main())
