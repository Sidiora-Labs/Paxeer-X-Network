import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import time

import paxeer_x_runtime_fixture as fixture

ANCHOR = '0x0000000000000000000000000000000000001014'
REGISTRY = '0x0000000000000000000000000000000000001004'
GUARANTOR_VIEW = 'guarantor(bytes32)(bytes32,address,address,uint256,uint256,uint8,bool)'
UNIT_WEI = 10 ** 12
STATUS_FINAL = 2
SUPPLEMENTAL = {'layerx-guarantor', 'lxp_test_daemon_finality_authority'}
SUPPLEMENTAL_SOURCES = ['cmd/layerx-guarantor', 'cmd/layerxd', 'src', 'include', 'tests/daemon/lxp_test_finality_authority.c']


def supplemental(path):
    fixture.require(bool(path), 'supplemental guarantor manifest required')
    fixture.private(path)
    value = json.loads(Path(path).read_text())
    fixture.require(value.get('version') == 1 and set(value.get('artifacts', {})) == SUPPLEMENTAL, 'supplemental schema/set')
    revision = value.get('source_revision', '')
    fixture.require(re.fullmatch('[0-9a-f]{40}', revision), 'supplemental source revision')
    changed = fixture.run(['git', 'diff', '--name-only', revision, 'HEAD', '--', *SUPPLEMENTAL_SOURCES], capture_output=True).stdout
    fixture.require(not changed, 'supplemental source differs from HEAD')
    for name, row in value['artifacts'].items():
        target = Path(row.get('path', ''))
        fixture.require(target.is_absolute() and target.is_file() and not target.is_symlink() and os.access(target, os.X_OK), 'missing executable ' + name)
        fixture.require(row.get('sha256') == fixture.digest(target), 'supplemental digest mismatch ' + name)
    return value


def env_file(path):
    return dict(line.split('=', 1) for line in Path(path).read_text().splitlines() if '=' in line and not line.startswith('#'))


class FinalityProducer:
    """Two real layerx-guarantor processes bonded in the live anchor of a RuntimeFixture."""

    def __init__(self, runtime, manifest):
        self.runtime, self.d = runtime, runtime.directory
        self.work = self.d / 'finality'
        self.work.mkdir(mode=0o700)
        self.binaries = {}
        for name, row in manifest['artifacts'].items():
            dest = self.work / name
            shutil.copyfile(row['path'], dest)
            fixture.require(fixture.digest(dest) == row['sha256'], 'staged supplemental mismatch')
            dest.chmod(0o700)
            self.binaries[name] = dest
        self.settlement = {k: v for k, v in env_file(self.d / 'node/sequencer.env').items() if k.startswith('LAYERX_NODE_')}
        fixture.require(self.settlement.get('LAYERX_NODE_PAXEER_RPC_URL') == runtime.rpc_url, 'settlement RPC is not the owned chain')
        self.anchor = json.loads((self.d / 'anchor.json').read_text())
        params = self.anchor.get('params', self.anchor)
        self.min_bond, self.max_delay = int(params['min_bond']), int(params['max_attestation_delay_ms'])
        self.key = self.work / 'deployer.key'
        shutil.copyfile(self.d / 'keys/deployer.key', self.key)
        self.key.chmod(0o600)
        self.deployer = self.evm('address', self.key)
        self.guarantors = []
        for index in (1, 2):
            identity = self.d / ('guarantor-' + str(index)) / 'identity'
            gid = env_file(identity / 'producer.env')['LAYERX_GUARANTOR_ID']
            der = subprocess.run(['openssl', 'ec', '-in', str(identity / 'key.pem'), '-pubout', '-conv_form', 'compressed', '-outform', 'DER'],
                                 capture_output=True, check=True).stdout
            public = der[-33:].hex()
            fixture.require(hashlib.sha256(('layerx-beta-guarantor:' + public).encode()).hexdigest() == gid, 'guarantor identity/key mismatch')
            signer = self.helper('platform/hosted/paxeer/settlement-domain.py', 'signer', public)
            state = self.work / ('producer-' + str(index))
            state.mkdir(mode=0o700)
            inputs = self.work / ('inputs-' + str(index))
            inputs.mkdir(mode=0o700)
            self.guarantors.append(dict(index=index, id=gid, public_key=public, signer=signer, identity=identity, state=state, inputs=inputs))
        self.guarantors.sort(key=lambda g: g['id'])
        self.processes, self.logs = {}, {}
        self.commands = []

    # -- real chain helpers ------------------------------------------------
    def helper(self, script, *argv):
        result = subprocess.run([sys.executable, str(fixture.ROOT / script), *[str(a) for a in argv]], cwd=fixture.ROOT,
                                env=self.runtime.env, capture_output=True, timeout=180)
        self.commands.append([script, *[str(a) for a in argv[:1]], 'exit=' + str(result.returncode)])
        fixture.require(result.returncode == 0, script + ' ' + str(argv[0]) + ' refused: ' + result.stderr.decode()[-400:])
        return result.stdout.decode().strip()

    def evm(self, mode, *argv):
        return self.helper('platform/hosted/paxeer/evm.py', mode, *argv)

    def call(self, signature, *argv):
        return json.loads(self.evm('call', '--rpc', self.runtime.rpc_url, ANCHOR, signature, *argv))

    def send(self, *argv, value=0):
        receipt = json.loads(self.evm('send', '--rpc', self.runtime.rpc_url, '--chain', '125', '--timeout', '120',
                                      '--key-file', self.key, '--value', value, *argv))
        fixture.require(receipt.get('status') == '0x1', 'transaction failed')
        return receipt

    def record(self, gid):
        row = self.call(GUARANTOR_VIEW, '0x' + gid)
        return dict(signer=row[1].lower(), operator=row[2].lower(), bond=int(row[3]), status=int(row[5]), eligible=row[6] in (True, 'true'))

    def bond(self):
        fixture.require(int(self.call('threshold()(uint32)')[0]) == 2, 'anchor threshold is not the two-guarantor policy')
        try:
            self.evm('call', '--rpc', self.runtime.rpc_url, REGISTRY, 'getPaxAddr(address)(string)', self.deployer)
        except RuntimeError:
            self.send(self.deployer)
            self.evm('call', '--rpc', self.runtime.rpc_url, REGISTRY, 'getPaxAddr(address)(string)', self.deployer)
        for g in self.guarantors:
            self.send(ANCHOR, 'registerGuarantor(bytes32,address)', '0x' + g['id'], g['signer'], value=self.min_bond * UNIT_WEI)
            self.send(ANCHOR, 'activateGuarantor(bytes32)', '0x' + g['id'])
            seen = self.record(g['id'])
            fixture.require(seen['signer'] == g['signer'].lower() and seen['operator'] == self.deployer.lower()
                            and seen['status'] == 2 and seen['eligible'] and seen['bond'] >= self.min_bond, 'guarantor not active and eligible')
        self.settlement_file = self.work / 'checkpoint-settlement.json'
        shutil.copyfile(fixture.ROOT / 'contracts/config/checkpoint-settlement.json', self.settlement_file)
        self.settlement_file.chmod(0o600)
        domain = dict(protocol_version=3, paxeer_chain_id=125, network_id=fixture.NETWORK, settlement_contract=ANCHOR,
                      guarantor_bond=ANCHOR, minimum_bond=self.min_bond, maximum_attestation_delay_ms=self.max_delay,
                      guarantor_set=[dict(guarantor_id=g['id'], signer=g['signer'], public_key=g['public_key']) for g in self.guarantors])
        result = subprocess.run([sys.executable, str(fixture.ROOT / 'platform/hosted/paxeer/settlement-domain.py'), 'write', str(self.settlement_file), 'beta'],
                                input=json.dumps(domain).encode(), capture_output=True, timeout=60)
        fixture.require(result.returncode == 0, 'settlement domain refused: ' + result.stderr.decode()[-300:])

    def unbond(self, g, amount=1):
        self.send(ANCHOR, 'beginUnbond(bytes32,uint256)', '0x' + g['id'], amount)
        seen = self.record(g['id'])
        fixture.require(not seen['eligible'] and seen['bond'] < self.min_bond, 'unbond did not make the signer ineligible')
        return seen

    # -- real guarantor processes -----------------------------------------
    def tls(self):
        script = 'set -euo pipefail; . "$1/platform/hosted/tests/beta-cluster.sh"; WORK_DIR=$2; CA_DIR="$2/ca"; SECRETS_DIR="$2/tls-secrets"; ca_generate'
        with (self.work / 'tls.log').open('ab') as log:
            subprocess.run(['bash', '-c', script, 'finality-tls', str(fixture.ROOT), str(self.work)], check=True, stdout=log, stderr=log, timeout=300)
        self.ports = [s.getsockname()[1] for s in fixture.reserve_ports(2)]
        (self.work / 'submitter-lock').mkdir(mode=0o700, exist_ok=True)

    def environment(self, g, position):
        identity, node = g['identity'], env_file(g['identity'] / 'producer.env')
        return self.runtime.env | self.settlement | node | {
            'LAYERX_NODE_SNAPSHOT': str(identity / 'genesis.lxs'), 'LAYERX_NODE_GENESIS_MANIFEST': str(identity / 'genesis.manifest'),
            'LAYERX_NODE_GENESIS_REGISTRATION': str(identity / 'genesis.registration'), 'LAYERX_NODE_IDENTITIES': str(identity / 'identities.txt'),
            'LAYERX_GUARANTOR_NODE_CONFIG': str(identity / 'node.conf'), 'LAYERX_GUARANTOR_KEY_FILE': str(identity / 'key.pem'),
            'LAYERX_GUARANTOR_STATE_DIR': str(g['state']), 'LAYERX_GUARANTOR_LNI_SOCKET': str(self.d / 'run/layerxd.lni.sock'),
            'LAYERX_GUARANTOR_SETTLEMENT_FILE': str(self.settlement_file), 'LAYERX_GUARANTOR_SETTLEMENT_DOMAIN': 'beta',
            'LAYERX_GUARANTOR_SUBMITTER_KEY_FILE': str(self.key), 'LAYERX_GUARANTOR_SUBMITTER_LOCK_FILE': str(self.work / 'submitter-lock/submitter.lock'),
            'LAYERX_GUARANTOR_PUBLICATION_INPUTS_DIR': str(g['inputs']), 'LAYERX_GUARANTOR_PYTHON': sys.executable,
            'LAYERX_GUARANTOR_SETTLEMENT_HELPER': str(fixture.ROOT / 'cmd/layerx-guarantor/settlement.py'),
            'LAYERX_GUARANTOR_LISTEN_PORT': str(self.ports[position]), 'LAYERX_GUARANTOR_PEER_URL': 'https://127.0.0.1:' + str(self.ports[1 - position]),
            'LAYERX_GUARANTOR_TLS_CA_FILE': str(self.work / 'ca/ca.crt'),
            'LAYERX_GUARANTOR_TLS_CERT_FILE': str(self.work / ('ca/guarantor-' + str(g['index']) + '/cert.pem')),
            'LAYERX_GUARANTOR_TLS_KEY_FILE': str(self.work / ('ca/guarantor-' + str(g['index']) + '/key.pem'))}

    def start(self):
        for position, g in enumerate(self.guarantors):
            name = 'guarantor-' + str(g['index'])
            fixture.require(name not in self.processes, 'guarantor already owned')
            log = (self.work / (name + '.log')).open('ab')
            self.logs[name] = log
            self.processes[name] = subprocess.Popen([str(self.binaries['layerx-guarantor'])], cwd=fixture.ROOT, env=self.environment(g, position),
                                                    stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True)
            self.runtime.processes[name] = self.processes[name]

    def stop(self):
        for name, process in list(self.processes.items()):
            fixture.require(process.poll() is None, 'owned guarantor exited unexpectedly: ' + name)
            process.terminate()
            process.wait(timeout=30)
            del self.processes[name]
            self.runtime.processes.pop(name, None)
            self.logs.pop(name).close()

    def output(self, g):
        return (self.work / ('guarantor-' + str(g['index']) + '.log')).read_text(errors='replace')

    # -- publication authorizations (owner and checkpoint-authority signatures) --
    def authorize(self):
        for g in self.guarantors:
            for request in sorted(g['state'].rglob('*.publication-request.json')):
                digest = request.name.split('.')[0]
                if all((h['inputs'] / (digest + '.json')).is_file() for h in self.guarantors):
                    continue
                signed = self.sign(request)
                for h in self.guarantors:
                    target = h['inputs'] / (digest + '.json')
                    if not target.exists():
                        shutil.copyfile(signed, target)
                        target.chmod(0o600)

    def sign(self, request):
        out = self.work / 'publication-signed'
        out.mkdir(mode=0o700, exist_ok=True)
        keys = {}
        for name in ('treasury', 'bob'):
            path = self.work / (name + '.owner')
            if not path.exists():
                path.write_text((self.d / 'keys' / (name + '.seed')).read_bytes().hex())
                path.chmod(0o600)
            keys[name] = path
        authority = self.work / 'deposit.pem'
        if not authority.exists():
            shutil.copyfile(self.d / 'keys/deposit.pem', authority)
            authority.chmod(0o600)
        errors = []
        for owners in (('treasury', 'bob'), ('treasury',), ('bob',)):
            for with_authority in (False, True):
                argv = [sys.executable, str(fixture.ROOT / 'cmd/layerx-guarantor/publication-sign.py'), str(request), str(out)]
                for owner in owners:
                    argv += ['--owner', str(keys[owner]) + '=' + self.deployer]
                if with_authority:
                    argv += ['--checkpoint-authority-key', str(authority)]
                result = subprocess.run(argv, cwd=fixture.ROOT, env=self.runtime.env, capture_output=True, timeout=60)
                if result.returncode == 0:
                    target = out / (request.name.split('.')[0] + '.json')
                    fixture.require(target.is_file(), 'publication signing wrote no authorization')
                    return target
                errors.append(result.stderr.decode().strip()[-200:])
        raise RuntimeError('runtime fixture refused: publication authorization: ' + ' | '.join(errors))

    def wait(self, probe, seconds=240):
        return self.runtime.wait(lambda: (self.authorize(), probe())[1], seconds)

    def registered(self, batch):
        marks = ('registered batch=' + str(batch) + '\n', 'observed registration batch=' + str(batch) + '\n')
        return self.wait(lambda: all(any(m in self.output(g) for m in marks) for g in self.guarantors))

    def attested(self, batch):
        return self.wait(lambda: all('attested batch=' + str(batch) + '\n' in self.output(g) for g in self.guarantors))

    # -- produced evidence --------------------------------------------------
    def checkpoint(self, batch):
        """The checkpoint id of the batch, read from the live anchor for each header the producer kept."""
        g = self.guarantors[0]
        for header in sorted(g['state'].glob('*.header')):
            ident = header.name.split('.')[0]
            if not re.fullmatch('[0-9a-f]{64}', ident):
                continue
            number, status = self.call('checkpointBatch(bytes32)(uint64,uint8)', '0x' + ident)
            if int(number) == batch:
                return ident, int(status)
        raise RuntimeError('runtime fixture refused: no kept header for batch ' + str(batch))

    def files(self, g, batch, ident):
        names = ['%020d.checkpoint' % batch, '%020d.finality' % batch, ident + '.header']
        names += sorted(p.name for p in g['state'].iterdir() if p.name.startswith(('%020d-' % batch, ident + '.')) and p.name not in names)
        rows = []
        for name in names:
            path = g['state'] / name
            fixture.require(path.is_file() and path.stat().st_size > 0, 'producer evidence missing: ' + name)
            rows.append(dict(name=name, bytes=path.stat().st_size, sha256=fixture.digest(path)))
        return rows

    def provenance(self, batch):
        ident, status = self.checkpoint(batch)
        fixture.require(status == STATUS_FINAL and int(self.call('statusOf(uint64)(uint8)', batch)[0]) == STATUS_FINAL, 'checkpoint is not FINAL')
        members = [m.lower().removeprefix('0x') for m in self.call('checkpointGuarantors(uint64)(bytes32[])', batch)[0]]
        fixture.require(members == [g['id'] for g in self.guarantors], 'checkpoint guarantors differ from the bonded producers')
        return dict(version=1, purpose='historical-finality-producer', chain_id=125, network_id=fixture.NETWORK, anchor=ANCHOR,
                    batch=batch, checkpoint_id=ident, anchor_status='FINAL', checkpoint_guarantors=members,
                    guarantors=[dict(guarantor_id=g['id'], signer=g['signer'], public_key=g['public_key'], state=str(g['state']),
                                     files=self.files(g, batch, ident)) for g in self.guarantors],
                    source_revision=self.runtime.manifest['source_revision'], deployer=self.deployer,
                    credentials_included=False)

    def verifier(self, g, batch, ident, admit):
        result = subprocess.run([str(self.binaries['lxp_test_daemon_finality_authority']), 'actual', str(g['state']), str(batch), ident,
                                 'admit' if admit else 'refuse'], cwd=fixture.ROOT, env=self.runtime.env | self.settlement,
                                capture_output=True, timeout=600)
        label = 'verifier-%d-%d-%s.log' % (g['index'], batch, 'admit' if admit else 'refuse')
        (self.work / label).write_bytes(result.stdout + result.stderr)
        return result

    def records(self):
        result = subprocess.run([str(self.binaries['lxp_test_daemon_finality_authority']), 'records', str(self.d / 'node/logs/evidence.log')],
                                cwd=fixture.ROOT, env=self.runtime.env, capture_output=True, timeout=60)
        fixture.require(result.returncode == 0, 'evidence log refused by the reader: ' + result.stderr.decode()[-200:])
        return json.loads(result.stdout)

    def record_bytes(self, records):
        path = self.d / 'node/logs/evidence.log'
        with path.open('rb') as stream:
            data = stream.read(records['valid_end'])
        return hashlib.sha256(data).hexdigest()

    def cleanup(self):
        for name, process in list(self.processes.items()):
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
            self.runtime.processes.pop(name, None)
            del self.processes[name]
        for log in self.logs.values():
            log.close()
        self.logs.clear()
