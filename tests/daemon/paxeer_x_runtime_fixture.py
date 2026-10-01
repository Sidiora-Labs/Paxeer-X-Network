import hashlib
import json
import os
from pathlib import Path
import re
import socket
import shutil
import copy
import urllib.error
import stat
import subprocess
import time
import urllib.request

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat

ROOT = Path(__file__).resolve().parents[2]
NETWORK = 77
ASSET = hashlib.sha256(b'layerx-asset:125:PAX').hexdigest()
BINARIES = {'paxd', 'attestor', 'layerxd', 'layerx-genesis-build', 'layerx-module-registry', 'layerx-custody-proof'}
UID = 4020


def require(condition, reason):
    if not condition:
        raise RuntimeError('runtime fixture refused: ' + reason)


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def run(argv, **kwargs):
    return subprocess.run([str(x) for x in argv], cwd=ROOT, check=True, **kwargs)


def private(path):
    p = Path(path)
    info = p.lstat()
    require(stat.S_ISREG(info.st_mode) and not p.is_symlink() and info.st_nlink == 1,
            'manifest is not a regular unlinked file')
    require(stat.S_IMODE(info.st_mode) == 0o600 and info.st_uid == os.geteuid(), 'manifest owner or mode')


def artifacts(path):
    require(bool(path), 'artifact manifest required')
    private(path)
    value = json.loads(Path(path).read_text())
    require(value.get('version') == 1 and set(value.get('artifacts', {})) == BINARIES, 'artifact schema/set')
    revision = value.get('source_revision', '')
    require(re.fullmatch('[a-f0-9]{40}', revision), 'source revision')
    tree = run(['git', 'rev-parse', revision + '^{tree}'], capture_output=True).stdout.decode().strip()
    require(tree == value.get('source_tree'), 'source tree mismatch')
    require(not run(['git', 'status', '--porcelain'], capture_output=True).stdout, 'dirty selected source')
    for name, row in value['artifacts'].items():
        target = Path(row.get('path', ''))
        require(target.is_absolute() and target.is_file() and not target.is_symlink() and os.access(target, os.X_OK), 'missing executable ' + name)
        require(row.get('source_revision') == revision and row.get('sha256') == digest(target), 'executable source/digest mismatch ' + name)
        paths = row.get('source_paths')
        require(isinstance(paths, list) and paths and all(isinstance(p, str) and p and not p.startswith('/') and '..' not in Path(p).parts for p in paths), 'missing/invalid source binding ' + name)
        changed = run(['git', 'diff', '--name-only', revision, 'HEAD', '--', *paths], capture_output=True).stdout
        require(not changed, 'selected production dependency differs: ' + name)
    for row in value.get('runtime_libraries', []):
        target = Path(row['path'])
        require(target.is_absolute() and target.is_file() and not target.is_symlink(), 'runtime library missing')
        require(row['source_revision'] == revision and digest(target) == row['sha256'], 'runtime library binding')
    return value


def client_artifact(path):
    require(bool(path), 'client manifest required; build explicitly before verification')
    private(path)
    value = json.loads(Path(path).read_text())
    head = run(['git', 'rev-parse', 'HEAD'], capture_output=True).stdout.decode().strip()
    require(value.get('version') == 1 and re.fullmatch('[0-9a-f]{40}', value.get('source_revision', '')), 'client source mismatch')
    changed = run(['git', 'diff', '--name-only', value['source_revision'], head, '--',
        'tests/daemon/lxp_test_runtime_fixture.c', 'tests/daemon/lxp_test_program_admission.c',
        'src', 'include', 'programs'], capture_output=True).stdout
    require(not changed, 'client dependency source mismatch')
    target = Path(value.get('path', ''))
    require(target.is_absolute() and target.is_file() and not target.is_symlink() and os.access(target, os.X_OK), 'client executable missing')
    require(digest(target) == value.get('sha256'), 'client executable digest mismatch')
    return value


def write_json(path, value):
    with Path(path).open('x') as stream:
        json.dump(value, stream, sort_keys=True)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())
    Path(path).chmod(0o600)


def reserve_ports(count):
    sockets = []
    for _ in range(count):
        sock = socket.socket()
        sock.bind(('127.0.0.1', 0))
        sockets.append(sock)
    return sockets


class RuntimeFixture:
    def __init__(self, directory, bundle, client):
        require(os.geteuid() == 0, 'root required for distinct real LNI peer credentials')
        self.directory = Path(directory).resolve()
        require(not self.directory.exists(), 'fixture directory already exists')
        self.directory.mkdir(mode=0o700)
        os.chown(self.directory, UID, UID)
        self.bundle, self.client = copy.deepcopy(bundle), client
        inputs = self.directory / 'inputs'
        inputs.mkdir(mode=0o700)
        (inputs / 'lib').mkdir(mode=0o700)
        for name, row in self.bundle['artifacts'].items():
            dest = inputs / name
            shutil.copyfile(row['path'], dest)
            require(digest(dest) == row['sha256'], 'staged executable mismatch')
            dest.chmod(0o700)
            row['path'] = str(dest)
        for row in self.bundle.get('runtime_libraries', []):
            dest = inputs / 'lib' / Path(row['path']).name
            shutil.copyfile(row['path'], dest)
            require(digest(dest) == row['sha256'], 'staged library mismatch')
            dest.chmod(0o600)
            row['path'] = str(dest)
        for path in [inputs, *inputs.rglob('*')]:
            os.chown(path, UID, UID)
        self.processes, self.logs, self.commands = {}, [], []
        self.reservations = reserve_ports(9)
        self.ports = [s.getsockname()[1] for s in self.reservations]
        self.rpc_url = 'http://127.0.0.1:' + str(self.ports[0])
        self.env = {key: val for key, val in os.environ.items() if not key.startswith(('LAYERX_', 'PAXEER_X_'))}
        self.env.update(HOME=str(self.directory), GOMAXPROCS='4', PYTHONDONTWRITEBYTECODE='1')
        self.env['LD_LIBRARY_PATH'] = ':'.join(sorted({str(Path(r['path']).parent) for r in self.bundle.get('runtime_libraries', [])}))

    def binary(self, name):
        return self.bundle['artifacts'][name]['path']

    def produce(self, label, argv, env=None, timeout=120):
        with (self.directory / (label + '.log')).open('ab') as log:
            run(argv, env=self.env | (env or {}), stdout=log, stderr=log, timeout=timeout,
                user=UID, group=UID, extra_groups=[])
        self.commands.append(label)

    def launch(self, name, argv, env=None):
        require(name not in self.processes, 'process already owned: ' + name)
        log = (self.directory / (name + '.log')).open('ab')
        self.logs.append(log)
        process = subprocess.Popen([str(x) for x in argv], cwd=ROOT, env=self.env | (env or {}),
                                   stdin=subprocess.DEVNULL, stdout=log, stderr=log,
                                   user=UID, group=UID, extra_groups=[], start_new_session=True)
        self.processes[name] = process
        return process.pid

    def wait(self, probe, seconds=90):
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            require(all(p.poll() is None for p in self.processes.values()), 'owned daemon exited; inspect private logs')
            try:
                result = probe()
                if result:
                    return result
            except (OSError, ValueError, AssertionError):
                pass
            time.sleep(.15)
        raise RuntimeError('runtime fixture refused: readiness deadline')

    def rpc(self, method, params=None, comet=False):
        port = self.ports[2] if comet else self.ports[0]
        request = urllib.request.Request('http://127.0.0.1:' + str(port),
            json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params if params is not None else ({} if comet else [])}).encode(),
            {'Content-Type': 'application/json'})
        with urllib.request.urlopen(request, timeout=3) as response:
            value = json.load(response)
        require('error' not in value and 'result' in value, 'owned chain RPC refused')
        return value['result']

    def generate(self):
        d = self.directory
        (d / 'keys').mkdir(mode=0o700)
        public = {}
        for name in ('sequencer', 'treasury', 'bob'):
            seed = os.urandom(32)
            (d / 'keys' / (name + '.seed')).write_bytes(seed)
            public[name] = Ed25519PrivateKey.from_private_bytes(seed).public_key().public_bytes(Encoding.Raw, PublicFormat.Raw).hex()
        (d / 'salt').write_bytes(os.urandom(32))
        (d / 'keys/deployer.key').write_text('0x' + os.urandom(32).hex())
        for p in [d / 'keys', *d.rglob('*')]:
            os.chown(p, UID, UID)
            p.chmod(0o700 if p.is_dir() or p.parent == d / 'inputs' else 0o600)
        address = run(['python3', ROOT / 'platform/hosted/paxeer/evm.py', 'address', d / 'keys/deployer.key'], capture_output=True).stdout.decode().strip()
        authority = run(['python3', ROOT / 'platform/hosted/node/checkpoint-authority.py', d / 'keys/deposit.pem'], capture_output=True).stdout.decode().strip().removeprefix('0x')
        common = ['--network-id', NETWORK, '--sequencer-id', hashlib.sha256(('layerx-sequencer:' + public['sequencer']).encode()).hexdigest(), '--sequencer-public-key', public['sequencer']]
        self.produce('custody-genesis', ['python3', ROOT / 'platform/hosted/paxeer/custody-genesis.py', *common, '--deposit-root-authority', authority, '--asset', ASSET + ':uhpx', '--output', d / 'custody.json'])
        policy = json.loads((ROOT / 'contracts/config/checkpoint-settlement.json').read_text())
        threshold = policy['finality_policy']['certificate_threshold']
        self.produce('anchor-genesis', ['python3', ROOT / 'platform/hosted/paxeer/anchor-genesis.py', *common, '--authority-evm', address, '--paxeer-chain-id', '125', '--threshold', threshold, '--output', d / 'anchor.json'])
        self.chain_env = dict(PAXD=self.binary('paxd'), LAYERX_PAXEER_HOME=str(d / 'chain'), LAYERX_PAXEER_CHAIN_ID='125',
            LAYERX_PAXEER_DEPLOYER_ADDRESS=address, LAYERX_PAXEER_CUSTODY_GENESIS_FILE=str(d / 'custody.json'),
            LAYERX_PAXEER_ANCHOR_GENESIS_FILE=str(d / 'anchor.json'), LAYERX_PAXEER_DEPOSIT_ROOT_AUTHORITY=authority,
            LAYERX_PAXEER_COMMIT_TIMEOUT_NANOSECONDS='1000000000')
        for name, port in zip(('EVM', 'EVM_WS', 'RPC', 'P2P', 'GRPC', 'GRPC_WEB', 'API'), self.ports):
            self.chain_env['LAYERX_PAXEER_' + name + '_PORT'] = str(port)
        self.produce('chain-init', ['bash', ROOT / 'platform/hosted/paxeer/init-chain.sh'], self.chain_env, 180)
        for s in self.reservations:
            s.close()
        self.reservations.clear()
        self.start_chain()
        self.wait(lambda: int(self.rpc('eth_blockNumber'), 16) >= 3)
        height = int(self.rpc('status', comet=True)['sync_info']['latest_block_height'])
        self.produce('custody-profile', [self.binary('layerx-custody-proof'), 'light-profile', '--rpc', 'http://127.0.0.1:' + str(self.ports[2]), '--asset', '0x' + ASSET, '--network-id', NETWORK, '--trusted-height', height - 1, '--trusting-period-seconds', '1209600', '--output', d / 'custody.profile'])
        self.produce('bootstrap', ['bash', ROOT / 'platform/hosted/node/bootstrap.sh', '--data-dir', d / 'node', '--run-dir', d / 'run', '--network-id', NETWORK,
            '--sequencer-key', d / 'keys/sequencer.seed', '--treasury-key', d / 'keys/treasury.seed', '--asset', ASSET,
            '--genesis-metadata', d / 'metadata.lxgb', '--custody-profile', d / 'custody.profile', '--withdrawal-fee', '0',
            '--module-fees', ROOT / 'platform/hosted/node/genesis-module-fees.json', '--program-port', self.ports[7], '--replica-port', self.ports[8],
            '--migrations', ROOT / 'migrations/0007_history_index.sql', '--layerxd', self.binary('layerxd'), '--genesis-build', self.binary('layerx-genesis-build'), '--lni-uid', '0', '--lni-gid', UID],
            {'LAYERX_NODE_PAXEER_CHAIN_ID': '125', 'LAYERX_NODE_PAXEER_RPC_URL': self.rpc_url}, 180)
        bob = 'did:layerx:' + public['bob']
        with (d / 'node/identities.txt').open('a') as stream:
            stream.write(bob.encode().hex() + ':' + public['bob'] + ':0\n')
        self.manifest = {'version': 1, 'purpose': 'disposable-real-daemon-fixture', 'network_id': NETWORK, 'chain_id': 125,
            'source_revision': run(['git', 'rev-parse', 'HEAD'], capture_output=True).stdout.decode().strip(),
            'artifacts_source_revision': self.bundle['source_revision'], 'directory': str(d), 'chain_rpc': self.rpc_url,
            'node_socket': str(d / 'run/layerxd.lni.sock'), 'replica_port': self.ports[8], 'keys': str(d / 'keys'),
            'sequencer_public_key': public['sequencer'], 'genesis_sha256': digest(d / 'node/genesis/genesis.manifest'),
            'anchor_genesis_sha256': digest(d / 'anchor.json'), 'custody_genesis_sha256': digest(d / 'custody.json'),
            'client': self.client, 'downstream_module_qualification': False}
        write_json(d / 'fixture.json', self.manifest)
        self.start()

    def start_chain(self):
        self.launch('chain', [self.binary('paxd'), 'start', '--home', self.directory / 'chain', '--consensus.create-empty-blocks-interval=1s'], self.chain_env)
        self.wait(lambda: self.rpc('eth_chainId') == '0x7d')

    def start_role(self, role):
        require(role in ('sequencer', 'replica'), 'unknown kernel role')
        d = self.directory
        if role == 'sequencer':
            script = 'set -e; source "$1"; layerx_sequencer_environment "$2"; exec "$3" --serve "$4"'
            argv = ['bash', '-c', script, 'runtime-fixture', ROOT / 'platform/hosted/node/sequencer-env.sh', d / 'node/sequencer.env', self.binary('layerxd'), d / 'node/sequencer.conf']
        else:
            script = 'set -ea; source "$1"; exec "$2" --authority-replica "$3"'
            argv = ['bash', '-c', script, 'runtime-fixture', d / 'node/replica.env', self.binary('layerxd'), d / 'node/replica.conf']
        return self.launch(role, argv)

    def start(self):
        if 'chain' not in self.processes:
            self.start_chain()
        self.start_role('replica')
        self.wait(self.replica_ready)
        self.start_role('sequencer')
        self.readiness()

    def replica_ready(self):
        with socket.create_connection(('127.0.0.1', self.ports[8]), timeout=.3):
            return True

    def readiness(self):
        return self.wait(lambda: (self.directory / 'run/layerxd.lni.sock').is_socket() and self.invoke('ready', 0, 'readiness', check=False).returncode == 0)

    def invoke(self, operation, sequence, label, check=True):
        env = self.env | {'PAXEER_X_FIXTURE_KEYS': str(self.directory / 'keys')}
        result = subprocess.run([self.client['path'], str(self.directory / 'run/layerxd.lni.sock'), str(self.directory / 'salt'), operation, str(sequence), 'poll'], cwd=ROOT, env=env, capture_output=True, timeout=45)
        (self.directory / (label + '.client.log')).write_bytes(result.stdout + result.stderr)
        if check:
            require(result.returncode == 0, 'real client ' + label + ' exited ' + str(result.returncode))
        return result

    def stop_role(self, name, kill=False):
        require(name in self.processes, 'refuse non-owned process')
        process = self.processes[name]
        require(process.poll() is None, 'owned process exited unexpectedly')
        if kill:
            process.kill()
        else:
            process.terminate()
        process.wait(timeout=20)
        require(process.returncode == (-9 if kill else 0), 'unexpected owned process exit')
        del self.processes[name]
        return process.pid

    def stop(self, kill=False):
        return {name: self.stop_role(name, kill) for name in ('sequencer', 'replica', 'chain') if name in self.processes}

    def restart(self, kill=False):
        old = self.stop(kill)
        self.start()
        require(all(self.processes[name].pid != pid for name, pid in old.items()), 'restart did not create new process')
        require(digest(self.directory / 'node/genesis/genesis.manifest') == self.manifest['genesis_sha256'], 'restart altered signed genesis')
        return old

    def catch_up(self, receipts):
        token = (self.directory / 'node/secrets/replica-token').read_text().strip()
        node_env = dict(line.split('=', 1) for line in (self.directory / 'node/node.env').read_text().splitlines())
        reports = []
        for row in receipts:
            url = 'http://127.0.0.1:' + str(self.ports[8]) + '/v1/batches/' + row['batch'] + '/receipt-authority?receipt_digest=' + row['digest']
            def probe():
                try:
                    request = urllib.request.Request(url, headers={'Authorization': 'Bearer ' + token})
                    with urllib.request.urlopen(request, timeout=3) as response:
                        return json.load(response)
                except urllib.error.HTTPError as error:
                    if error.code == 404:
                        return None
                    raise
            report = self.wait(probe)
            require(report['authority_replica_id'] == node_env['LAYERX_NODE_REPLICA_ID'], 'replica identity changed')
            require(report['sequencer_public_key'] == self.manifest['sequencer_public_key'], 'replica sequencer identity changed')
            proof_dir = self.directory / ('proof-' + row['id'])
            proof_dir.mkdir(mode=0o700, exist_ok=True)
            for name, field in [('header', 'header_hex'), ('signature', 'header_signature'), ('proof', 'receipt_proof_hex')]:
                (proof_dir / name).write_bytes(bytes.fromhex(report['batch_evidence'][field]))
            (proof_dir / 'receipt').write_bytes(bytes.fromhex(row['raw']))
            self.invoke('proof', proof_dir, 'proof-' + row['id'])
            reports.append(report)
        return reports

    def cleanup(self):
        owned = list(self.processes.values())
        for name in list(reversed(self.processes)):
            process = self.processes[name]
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
            del self.processes[name]
        for sock in self.reservations:
            sock.close()
        for log in self.logs:
            log.close()
        require(all(p.poll() is not None for p in owned), 'owned process cleanup incomplete')
