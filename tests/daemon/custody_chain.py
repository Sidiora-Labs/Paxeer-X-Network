import contextlib
import hashlib
import http.client
import json
import os
from pathlib import Path
import stat
import sys
import socket
import ssl
import subprocess
import tempfile
import time
from urllib.parse import urlsplit

from eth_account import Account
from eth_utils import to_checksum_address

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'tests/bridge'))
from custody_credit import Rpc, eth_hash
from deploy_local_custody import command as encoded_command

USDL = '0x85FcD13735F4309833A503EE804ea32395851479'
FORBIDDEN_PORTS = {18545, 19443, 6379}


CONTRACTS = ('GuarantorBond', 'CheckpointRegistry', 'BetaUsdl',
             'CheckpointChallengeManager', 'LayerXBetaTimelock', 'AssetRegistry', 'LayerXVault', 'WETH')
SOURCE_PATHS = ('Makefile', 'chain.mk', 'go.mod', 'go.sum', 'rust-toolchain.toml',
                'src', 'include', 'cmd', 'programs', 'agent', 'platform', 'contracts',
                'admin', 'consensus', 'custodyproof', 'daemon', 'engine', 'interchain',
                'layerxproof', 'modules', 'node', 'precompiles', 'ratelimiter', 'rpc',
                'sdk', 'storage', 'store', 'sync', 'types', 'utils', 'wasm', 'wasm-runtime',
                'wasmbinding', 'tools/chain', 'tests/bridge/sign_credit.c', 'tests/bridge/files.h',
                'loadtest/contracts/evm/lib', 'foundry.toml', 'remappings.txt')
EXECUTABLES = ('paxd', 'layerx-custody-proof', 'layerx-paxeer-boundary', 'sign-credit')


def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT)


def source_binding(revision):
    return hashlib.sha256(git('ls-tree', '-r', '-z', '--full-tree', revision, '--', *SOURCE_PATHS)).hexdigest()


def file_digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def artifact_manifest(path):
    if not path:
        raise ValueError('explicit prebuilt custody artifact manifest required')
    path = Path(path)
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_mode & 0o077 or info.st_uid != os.geteuid():
        raise ValueError('custody artifact manifest must be an owned private regular file')
    value = json.loads(path.read_text())
    if value.get('version') != 1 or tuple(value.get('source_paths', [])) != SOURCE_PATHS:
        raise ValueError('custody artifact manifest schema')
    revision = value.get('source_revision', '')
    if len(revision) != 40 or any(c not in '0123456789abcdef' for c in revision):
        raise ValueError('custody artifact revision')
    if git('status', '--porcelain', '--untracked-files=normal'):
        raise ValueError('dirty custody artifact consumer')
    if value.get('source_binding') != source_binding(revision) or value['source_binding'] != source_binding('HEAD'):
        raise ValueError('custody artifact source mismatch')
    if set(value.get('executables', {})) != set(EXECUTABLES) or set(value.get('contracts', {})) != set(CONTRACTS):
        raise ValueError('custody artifact set mismatch')
    for executable, entries in ((True, value['executables']), (False, value['contracts'])):
        for name, row in entries.items():
            target = Path(row['path'])
            if not target.is_absolute() or any(p.is_symlink() for p in (target, *target.parents)):
                raise ValueError('artifact path must be absolute without symlinks')
            if not target.is_file() or file_digest(target) != row['sha256']:
                raise ValueError('artifact missing or digest mismatch: ' + name)
            if executable and (not os.access(target, os.X_OK) or target.open('rb').read(4) != b'\x7fELF'):
                raise ValueError('artifact is not an executable ELF: ' + name)
    directory = Path(value['contract_directory'])
    for name, row in value['contracts'].items():
        if Path(row['path']) != directory / (name + '.sol') / (name + '.json'):
            raise ValueError('contract artifact directory mismatch')
    return value


def record_artifacts(build, output):
    if git('status', '--porcelain', '--untracked-files=normal'):
        raise ValueError('dirty custody artifact producer')
    revision = git('rev-parse', 'HEAD').decode().strip()
    contracts = build / 'withdraw-contracts/artifacts'
    binaries = {'paxd': build / 'paxd', 'layerx-custody-proof': build / 'bin/layerx-custody-proof',
                'layerx-paxeer-boundary': ROOT / 'platform/target/debug/layerx-paxeer-boundary',
                'sign-credit': build / 'tests/bridge/sign-credit'}
    def row(path):
        return {'path': str(path.resolve()), 'sha256': file_digest(path)}
    value = dict(version=1, source_revision=revision, source_binding=source_binding(revision),
                 source_paths=list(SOURCE_PATHS), contract_directory=str(contracts.resolve()),
                 executables={name: row(path) for name, path in binaries.items()},
                 contracts={name: row(contracts / (name + '.sol') / (name + '.json')) for name in CONTRACTS})
    descriptor = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'w') as stream:
        json.dump(value, stream, sort_keys=True)
        stream.write('\n')
    artifact_manifest(output)


def command(*args, **kwargs):
    return subprocess.run([str(arg) for arg in args], cwd=ROOT, check=True, **kwargs)


def artifact(directory, name):
    return json.loads((directory / f'{name}.sol/{name}.json').read_text())


def calldata(signature, *args):
    return encoded_command('cast', 'calldata', signature, *map(str, args))


def reserve_port():
    reservation = socket.socket()
    reservation.bind(('127.0.0.1', 0))
    if reservation.getsockname()[1] in FORBIDDEN_PORTS:
        reservation.close()
        raise ValueError('reserved port is forbidden')
    return reservation


class Chain:
    def __init__(self, identity_path):
        self.identity_path = Path(identity_path)
        identity = json.loads(self.identity_path.read_text())
        parsed = urlsplit(identity['rpc'])
        assert parsed.scheme == 'http' and parsed.hostname == '127.0.0.1'
        assert identity['chain_id'] == 125
        assert parsed.port is not None and parsed.port not in FORBIDDEN_PORTS and parsed.path == ''
        assert not parsed.username and not parsed.password and not parsed.query and not parsed.fragment
        os.kill(identity['pid'], 0)
        process_args = Path(f"/proc/{identity['pid']}/cmdline").read_bytes().split(b'\0')
        assert os.fsencode(identity['home']) in process_args
        assert b'start' in process_args and b'--home' in process_args
        self.account = Account.from_key(Path(identity['deployer_key']).read_bytes())
        self.url = identity['rpc']
        self.directory = Path(identity['evidence_dir'])
        self.port = parsed.port
        self.transport = Rpc(self.url)
        assert Path(f"/proc/{identity['pid']}/exe").resolve() == Path(identity['executable']).resolve()
        assert self.rpc('eth_chainId', []) == '0x7d'
        anchor = self.rpc('eth_getBlockByNumber', [hex(identity['anchor_number']), False])
        assert anchor['hash'] == identity['anchor_hash']
        assert self.account.address == identity['deployer']

    def rpc(self, method, params):
        return self.transport.call(method, params, allow_missing=method in (
            'eth_getTransactionReceipt', 'eth_getTransactionByHash'))

    def transaction(self, data, to=None, success=True, value=0, signer=None):
        assert self.rpc('eth_chainId', []) == '0x7d'
        account = self.account if signer is None else signer
        transaction = {
            'chainId': 125,
            'nonce': int(self.rpc('eth_getTransactionCount', [account.address, 'pending']), 16),
            'data': data,
            'gas': 15_000_000,
            'gasPrice': int(self.rpc('eth_gasPrice', []), 16),
            'value': value,
        }
        if to is not None:
            transaction['to'] = to_checksum_address(to)
        signed = account.sign_transaction(transaction)
        digest = self.rpc('eth_sendRawTransaction', ['0x' + bytes(signed.raw_transaction).hex()])
        assert bytes.fromhex(digest.removeprefix('0x')) == eth_hash(bytes(signed.raw_transaction))
        deadline = time.monotonic() + 60
        while time.monotonic() < deadline:
            receipt = self.rpc('eth_getTransactionReceipt', [digest])
            if receipt is not None:
                assert receipt['transactionHash'].lower() == digest.lower(), 'receipt identity'
                assert int(receipt['status'], 16) == int(success), receipt
                with (self.directory / 'transactions.jsonl').open('a') as evidence:
                    evidence.write(json.dumps(receipt, sort_keys=True) + '\n')
                return receipt
            time.sleep(.1)
        diagnostic = {'transaction_hash': digest, 'submitted_nonce': transaction['nonce'],
                      'gas_price': transaction['gasPrice'], 'gas_limit': transaction['gas']}
        for name, method, parameters in (
            ('latest_nonce', 'eth_getTransactionCount', [account.address, 'latest']),
            ('pending_nonce', 'eth_getTransactionCount', [account.address, 'pending']),
            ('block_number', 'eth_blockNumber', []),
            ('transaction', 'eth_getTransactionByHash', [digest]),
            ('pool_status', 'txpool_status', []),
        ):
            try:
                diagnostic[name] = self.rpc(method, parameters)
            except (OSError, ValueError, AssertionError, http.client.HTTPException) as error:
                diagnostic[name] = {'error': str(error)}
        (self.directory / ('transaction-timeout-' + digest.removeprefix('0x') + '.json')).write_text(
            json.dumps(diagnostic, sort_keys=True) + '\n')
        raise AssertionError('transaction receipt deadline: ' + digest)

    def view(self, address, signature, *args):
        encoded = calldata(signature, *args)
        return self.rpc('eth_call', [{'to': to_checksum_address(address), 'data': encoded}, 'latest'])

    def send(self, address, signature, *args, success=True, value=0, signer=None):
        return self.transaction(calldata(signature, *args), address, success, value, signer)

    def deploy(self, contract, signature, args):
        encoded = encoded_command('cast', 'abi-encode', signature, *map(str, args))
        bytecode = contract['bytecode']['object'].removeprefix('0x')
        assert bytecode and bytes.fromhex(bytecode), 'contract bytecode'
        receipt = self.transaction('0x' + bytecode + encoded.removeprefix('0x'))
        address = to_checksum_address(receipt['contractAddress'])
        assert self.rpc('eth_getCode', [address, 'latest']) not in ('0x', '0x0'), 'deployed code missing'
        return address


def from_environment(url):
    chain = Chain(os.environ['LAYERX_TEST_CUSTODY_CHAIN_FILE'])
    assert chain.url == url
    return chain


def govern(chain, timelock, target, signature, *args):
    data = calldata(signature, *args)
    assert int(chain.view(timelock, 'minDelay()'), 16) == 0

    def execute(destination, encoded):
        nonce = int(chain.view(timelock, 'operationNonce()'), 16)
        salt = '0x' + hashlib.sha256((destination + encoded + str(nonce)).encode()).hexdigest()
        chain.send(timelock, 'schedule(address,uint256,bytes,bytes32,uint64)',
                   destination, '0', encoded, salt, '0')
        chain.send(timelock, 'execute(address,uint256,bytes,bytes32,uint256)',
                   destination, '0', encoded, salt, str(nonce))

    execute(timelock, calldata('setCallPermission(address,bytes4,bool)',
                                  target, data[:10], 'true'))
    execute(target, data)


def retain_custody_proofs(work, origins, ca, identity, vault):
    from deploy_local_custody import disposable_rpc

    observations = []
    for origin in origins:
        rpc = disposable_rpc(origin, ca, identity)
        header = rpc.call('eth_getBlockByNumber', ['finalized', False])
        proof = {'method': 'eth_getProof', 'params': [vault, ['0x0'], header['number']]}
        try:
            proof['result'] = rpc.call(proof['method'], proof['params'])
        except ValueError as error:
            proof['error'] = str(error)
        observations.append({'origin': origin, 'finalized_header': header, 'storage_proof': proof,
                             'runtime_code': rpc.call('eth_getCode', [vault, header['number']])})
    (work / 'custody-proof-responses.json').write_text(json.dumps(observations, sort_keys=True) + '\n')


@contextlib.contextmanager
def owned_chain(work, artifacts, custody_genesis=None):
    with tempfile.TemporaryDirectory(prefix='lxp-custody-paxd-') as temporary, contextlib.ExitStack() as resources:
        private = Path(temporary)
        ports, reservations = [], []
        for _ in range(7):
            reservation = reserve_port()
            assert reservation.getsockname()[1] not in FORBIDDEN_PORTS
            ports.append(reservation.getsockname()[1])
            reservations.append(reservation)
        account = Account.create()
        key_file = private / 'deployer.key'
        descriptor = os.open(key_file, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, 'wb') as output:
            output.write(bytes(account.key))
        token = artifact(artifacts, 'BetaUsdl')
        runtime = token['deployedBytecode']['object']
        runtime_file = private / 'BetaUsdl.runtime.hex'
        runtime_file.write_text(runtime + '\n')
        chain_home = private / 'chain'
        env = {key: value for key, value in os.environ.items() if not key.startswith('LAYERX_PAXEER_')}
        env.update(LAYERX_PAXEER_HOME=str(chain_home), LAYERX_PAXEER_CHAIN_ID='125',
                   LAYERX_PAXEER_COMMIT_TIMEOUT_NANOSECONDS='1000000000',
                   LAYERX_PAXEER_DEPLOYER_ADDRESS=account.address,
                   LAYERX_PAXEER_USDL_RUNTIME=str(runtime_file))
        if custody_genesis is not None:
            # LayerX custody is the native layerxcustody module behind the precompile at 0x…1013:
            # init-chain.sh merges this section into app_state.layerxcustody before validate-genesis,
            # and without it the module maps no asset and admits no deposit.
            env['LAYERX_PAXEER_CUSTODY_GENESIS_FILE'] = str(custody_genesis)
            custody = json.loads(Path(custody_genesis).read_text())
            selected = custody['params']['sequencer_authorizations']
            assert len(selected) == 1, 'one selected sequencer required'
            policy = json.loads((ROOT / 'contracts/config/checkpoint-settlement.json').read_text())
            anchor_file = private / 'anchor-genesis.json'
            command(sys.executable, 'platform/hosted/paxeer/anchor-genesis.py',
                    '--network-id', str(custody['params']['network_id']),
                    '--sequencer-id', selected[0]['sequencer_id'],
                    '--sequencer-public-key', selected[0]['public_key'],
                    '--first-batch', selected[0]['first_batch_number'],
                    '--last-batch', selected[0]['last_batch_number'],
                    '--authority-evm', account.address, '--paxeer-chain-id', '125',
                    '--threshold', str(policy['finality_policy']['certificate_threshold']),
                    '--output', anchor_file)
            env['LAYERX_PAXEER_ANCHOR_GENESIS_FILE'] = str(anchor_file)
            env['LAYERX_PAXEER_DEPOSIT_ROOT_AUTHORITY'] = custody['params']['deposit_root_authority']
        env['GOMAXPROCS'] = str(min(4, int(env.get('GOMAXPROCS', '4'))))
        assert int(env['GOMAXPROCS']) > 0
        for name, port in zip(('EVM', 'EVM_WS', 'RPC', 'P2P', 'GRPC', 'GRPC_WEB', 'API'), ports):
            env[f'LAYERX_PAXEER_{name}_PORT'] = str(port)
        with (work / 'paxd-init.log').open('w') as log:
            command('bash', 'platform/hosted/paxeer/init-chain.sh', env=env, stdout=log, stderr=log)
        genesis_bytes = (chain_home / 'config/genesis.json').read_bytes()
        document = json.loads(genesis_bytes)
        consensus_timeout = document['consensus_params']['timeout']
        if custody_genesis is not None:
            assert document['app_state']['layerxcustody'] == custody
            assert document['app_state']['layerxanchor'] == json.loads(anchor_file.read_text())
        assert consensus_timeout['commit'] == '1000000000'
        assert consensus_timeout['bypass_commit_timeout'] is False
        with (work / 'paxd-reinit.log').open('w') as log:
            command('bash', 'platform/hosted/paxeer/init-chain.sh', env=env, stdout=log, stderr=log)
        assert (chain_home / 'config/genesis.json').read_bytes() == genesis_bytes
        with (work / 'paxd-reinit-mismatch.log').open('w') as log:
            refused = subprocess.run(['bash', 'platform/hosted/paxeer/init-chain.sh'], cwd=ROOT,
                env=env | {'LAYERX_PAXEER_COMMIT_TIMEOUT_NANOSECONDS': '500000000'},
                stdout=log, stderr=log, check=False)
        assert refused.returncode != 0
        assert 'requested commit timeout differs from initialised genesis' in (work / 'paxd-reinit-mismatch.log').read_text()
        assert (chain_home / 'config/genesis.json').read_bytes() == genesis_bytes
        (work / 'paxeer-genesis.json').write_bytes(genesis_bytes)
        for reservation in reservations:
            reservation.close()
        process = None
        try:
            with (work / 'paxd.log').open('w') as log:
                process = subprocess.Popen([env.get('PAXD', 'paxd'), 'start', '--home', str(chain_home),
                                            '--consensus.create-empty-blocks-interval=1s'],
                                           cwd=ROOT, env=env, stdout=log, stderr=log)
            transport = Rpc(f'http://127.0.0.1:{ports[0]}')
            deadline = time.monotonic() + 60
            while time.monotonic() < deadline:
                assert process.poll() is None, 'owned Paxeer process exited'
                try:
                    assert transport.call('eth_chainId', []) == '0x7d'
                    if int(transport.call('eth_blockNumber', []), 16) > 0:
                        break
                except (OSError, http.client.HTTPException):
                    pass
                time.sleep(.1)
            else:
                raise AssertionError('owned Paxeer readiness deadline')
            assert transport.call('eth_getCode', [USDL, 'latest']).lower() == runtime.lower()
            identity = {
                'chain_id': 125, 'rpc': f'http://127.0.0.1:{ports[0]}', 'pid': process.pid,
                'executable': str(Path(f'/proc/{process.pid}/exe').resolve()),
                'home': str(chain_home), 'deployer_key': str(key_file), 'deployer': account.address,
                'evidence_dir': str(work),
                'comet_url': f'http://127.0.0.1:{ports[2]}',
                'anchor_number': int(transport.call('eth_blockNumber', []), 16),
            }
            identity['anchor_hash'] = transport.call('eth_getBlockByNumber', [hex(identity['anchor_number']), False])['hash']
            identity_path = private / 'owned-chain.json'
            identity_path.write_text(json.dumps(identity))
            chain = Chain(identity_path)
            chain.directory = work
            assert int(chain.view(USDL, 'owner()'), 16) == int(account.address, 16)
            assert int(chain.view(USDL, 'decimals()'), 16) == 6
            provenance = {
                'chain_id': 125, 'deployer': account.address, 'usdl': USDL,
                'create_empty_blocks_interval': '1s',
                'commit_timeout_nanoseconds': 1000000000,
                'genesis_sha256': hashlib.sha256(genesis_bytes).hexdigest(),
                'anchor_number': identity['anchor_number'], 'anchor_hash': identity['anchor_hash'],
                'token_runtime_sha256': hashlib.sha256(bytes.fromhex(runtime.removeprefix('0x'))).hexdigest(),
                'token_source_sha256': hashlib.sha256((ROOT / 'platform/hosted/paxeer/contracts/BetaUsdl.sol').read_bytes()).hexdigest(),
                'initializer_sha256': hashlib.sha256((ROOT / 'platform/hosted/paxeer/init-chain.sh').read_bytes()).hexdigest(),
            }
            (work / 'chain-provenance.json').write_text(json.dumps(provenance, sort_keys=True) + '\n')
            yield chain
        finally:
            for reservation in reservations:
                reservation.close()
            if process is not None and process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()


@contextlib.contextmanager
def boundaries(work, source, binary):
    identity = json.loads(source.identity_path.read_text())
    private = source.identity_path.parent
    processes = []
    reservations = []
    try:
        with (work / 'boundary-certificates.log').open('w') as log:
            command('openssl', 'req', '-x509', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256',
                    '-nodes', '-keyout', private / 'ca.key', '-out', private / 'ca.pem', '-days', '1',
                    '-subj', '/CN=LayerX custody qualification CA', stdout=log, stderr=log)
            command('openssl', 'req', '-new', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256',
                    '-nodes', '-keyout', private / 'tls.key', '-out', private / 'server.csr',
                    '-subj', '/CN=localhost', stdout=log, stderr=log)
            (private / 'extensions').write_text(
                'subjectAltName=DNS:localhost,IP:127.0.0.1\nbasicConstraints=CA:FALSE\nextendedKeyUsage=serverAuth\n')
            command('openssl', 'x509', '-req', '-in', private / 'server.csr', '-CA', private / 'ca.pem',
                    '-CAkey', private / 'ca.key', '-CAcreateserial', '-days', '1', '-extfile', private / 'extensions',
                    '-out', private / 'cert.pem', stdout=log, stderr=log)
            command('openssl', 'x509', '-in', private / 'cert.pem', '-outform', 'DER', '-out', private / 'cert.der',
                    stdout=log, stderr=log)
            command('openssl', 'pkcs8', '-topk8', '-nocrypt', '-in', private / 'tls.key', '-outform', 'DER',
                    '-out', private / 'tls.der', stdout=log, stderr=log)
        ca = work / 'boundary-ca.pem'
        ca.write_bytes((private / 'ca.pem').read_bytes())
        context = ssl.create_default_context(cafile=ca)
        origins = []
        genesis = None
        for index in range(2):
            reservation = reserve_port()
            reservations.append(reservation)
            port = reservation.getsockname()[1]
            env = os.environ | {
                'LAYERX_PAXEER_CHAIN_ID': '125', 'LAYERX_PAXEER_BOUNDARY_LISTEN': f'127.0.0.1:{port}',
                'LAYERX_PAXEER_BOUNDARY_TLS_CERT_DER': str(private / 'cert.der'),
                'LAYERX_PAXEER_BOUNDARY_TLS_KEY_DER': str(private / 'tls.der'),
                'LAYERX_PAXEER_NODE_URL': source.url, 'LAYERX_PAXEER_COMET_URL': identity['comet_url'],
            }
            with (work / f'boundary-{index}.log').open('w') as log:
                reservation.close()
                process = subprocess.Popen([str(binary)], cwd=ROOT, env=env, stdout=log, stderr=log)
            processes.append(process)
            deadline = time.monotonic() + 30
            while time.monotonic() < deadline:
                assert process.poll() is None, 'owned boundary exited'
                connection = http.client.HTTPSConnection('localhost', port, context=context, timeout=5)
                try:
                    connection.request('GET', '/readyz')
                    response = connection.getresponse()
                    response.read()
                    if response.status == 200:
                        break
                except (OSError, http.client.HTTPException):
                    pass
                finally:
                    connection.close()
                time.sleep(.1)
            else:
                raise AssertionError('owned boundary readiness deadline')
            connection = http.client.HTTPSConnection('localhost', port, context=context, timeout=5)
            try:
                connection.request('GET', '/genesis')
                response = connection.getresponse()
                document = response.read(64 * 1024 * 1024 + 1)
                assert response.status == 200 and len(document) <= 64 * 1024 * 1024
                assert response.headers.get_all('X-LayerX-Genesis-SHA256') == [hashlib.sha256(document).hexdigest()]
            finally:
                connection.close()
            if genesis is None:
                expected = json.loads((work / 'paxeer-genesis.json').read_bytes())
                abci = expected['consensus_params']['abci']
                assert type(abci['vote_extensions_enable_height']) is int
                abci['vote_extensions_enable_height'] = str(abci['vote_extensions_enable_height'])
                assert json.loads(document) == expected
                genesis = document
            else:
                assert document == genesis
            origins.append(f'https://localhost:{port}')
        (work / 'boundary-genesis.json').write_bytes(genesis)
        disposable = {
            'rpc_origins': origins, 'chain_id': 125, 'comet_chain_id': json.loads(genesis)['chain_id'],
            'genesis_sha256': '0x' + hashlib.sha256(genesis).hexdigest(),
            'ca_sha256': '0x' + hashlib.sha256(ca.read_bytes()).hexdigest(), 'genesis_source': 'boundary',
        }
        path = work / 'disposable-identity.json'
        path.write_text(json.dumps(disposable, sort_keys=True) + '\n')
        yield origins, ca, path
    finally:
        for reservation in reservations:
            reservation.close()
        for process in processes:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
