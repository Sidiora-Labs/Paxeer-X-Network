import argparse
import hashlib
import json
import os
from pathlib import Path
import runpy
import shutil
import signal
import subprocess
import sys
import tempfile
import time

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, NoEncryption, PrivateFormat, PublicFormat

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'tests/bridge'))
from comet_credit import CUSTODY_ADDRESS, module_identity
from custody_credit import DEPOSIT_TOPIC, unhex, write_new
from custody_chain import artifact_manifest, boundaries, calldata, from_environment, owned_chain, retain_custody_proofs

ASSET = 'b5a32b12029f8ddfb905f90f280f664b46390de0fc62770fc197dd87b18cd898'
NETWORK_ID = 77


def native_network_id():
    selected = os.environ.get('LAYERX_TEST_NATIVE_ARBITER_NETWORK_ID')
    assert selected in (None, '7'), 'invalid native arbiter network profile'
    return 7 if selected == '7' else NETWORK_ID

# Custody is the native layerxcustody module behind the precompile at 0x...1013 and nothing
# custodial is deployed for it: the asset map, the sequencer authorization and the deposit-root
# authority are chain genesis state written by platform/hosted/paxeer/custody-genesis.py. One
# base unit of the chain's own denomination is 1e12 wei, so deposit(bytes32) carries the LayerX
# u128 amount scaled by that factor.
CUSTODY_DENOM = 'uhpx'
WEI_PER_BASE_UNIT = 10 ** 12
# Checkpoint settlement and the guarantor bond are the native layerxanchor module behind the
# precompile at 0x...1014; platform/hosted/node/bootstrap.sh accepts no other settlement contract
# or checkpoint registry, which is the address tests/daemon/program-admission.sh already publishes
# for its own non-custody bring-up.
ANCHOR_ADDRESS = '0x0000000000000000000000000000000000001014'
# tests/daemon/program-admission.sh writes exactly this sequencer seed, so the custody genesis
# authorizes the sequencer the node started by this harness really runs.
SEQUENCER_SEED = bytes([0x22]) * 32


def run(*args, **kwargs):
    subprocess.run([str(arg) for arg in args], cwd=ROOT, check=True, **kwargs)


def retain_public_evidence(work, evidence):
    for source in work.rglob('*'):
        relative = source.relative_to(work)
        if (not source.is_file() or source.is_symlink() or 'secrets' in relative.parts
                or (source.suffix in ('.pem', '.key', '.env') and source.name != 'boundary-ca.pem')
                or source.name in ('actor', 'sequencer', 'treasury', 'client')):
            continue
        target = evidence / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, target)


def register(work, url):
    network_id = native_network_id()
    if os.environ.get('LAYERX_TEST_SETTLEMENT_PUBLICATION') == '1':
        module = runpy.run_path(str(ROOT / 'tests/daemon/guarantor-publication-chain.py'))
        module['setup'](work, url)
        return
    request = (work / 'data/genesis/paxeer-registration-request.lxrr').read_bytes()
    assert len(request) == 73
    assert request[:5] == b'LXRR\x01' and int.from_bytes(request[5:9], 'big') == network_id
    # Settlement is the native layerxanchor module, so no guarantor bond and no checkpoint
    # registry is deployed for it and the genesis registration this harness used to overwrite is
    # already the bytes platform/hosted/node/bootstrap.sh derived from the same request.
    assert (work / 'data/genesis/genesis.registration').read_bytes() == (
        b'LXGR\x01' + network_id.to_bytes(4, 'big') + bytes(8) + request[41:73] * 2 + b'\x01')
    write_settlement(work, from_environment(url), ANCHOR_ADDRESS, ANCHOR_ADDRESS)


def write_settlement(work, chain, bond, registry):
    (work / 'settlement.env').write_text(
        'LAYERX_NODE_PAXEER_CHAIN_ID=125\n'
        f'LAYERX_NODE_SETTLEMENT_CONTRACT={bond}\n'
        f'LAYERX_NODE_CHECKPOINT_REGISTRY={registry}\n'
        'LAYERX_NODE_PAXEER_RPC_ADDRESS=127.0.0.1\n'
        f'LAYERX_NODE_PAXEER_RPC_PORT={chain.port}\n')


def custody_genesis(work, network_id=NETWORK_ID, sequencer_seed=SEQUENCER_SEED):
    """The layerxcustody genesis section the native custody module is initialised from.

    The module maps no asset and admits no deposit without it, and it refuses every deposit-root
    registration while the authority parameter is empty, so the Ed25519 key that would have to
    sign one exists before the chain does and its private half stays in the work directory.
    """
    sequencer = Ed25519PrivateKey.from_private_bytes(sequencer_seed).public_key().public_bytes(
        Encoding.Raw, PublicFormat.Raw)
    identifier = hashlib.sha256(b'layerx-sequencer:' + sequencer.hex().encode()).hexdigest()
    authority = Ed25519PrivateKey.generate()
    write_new(work / 'deposit-root-authority.key',
              authority.private_bytes(Encoding.Raw, PrivateFormat.Raw, NoEncryption()))
    genesis = work / 'custody-genesis.json'
    run(sys.executable, 'platform/hosted/paxeer/custody-genesis.py',
        '--network-id', str(network_id), '--sequencer-id', '0x' + identifier,
        '--sequencer-public-key', '0x' + sequencer.hex(),
        '--deposit-root-authority',
        '0x' + authority.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw).hex(),
        '--asset', '0x' + ASSET + ':' + CUSTODY_DENOM, '--output', genesis)
    return genesis


def deposit(chain, beneficiary, amount):
    assert unhex(chain.view(CUSTODY_ADDRESS, 'nativeAssetId()'), 32) == bytes.fromhex(ASSET), \
        'custody genesis maps another native asset'
    deposited = chain.transaction(calldata('deposit(bytes32)', '0x' + beneficiary),
                                  CUSTODY_ADDRESS, value=amount * WEI_PER_BASE_UNIT)
    logs = [entry for entry in deposited['logs']
            if unhex(entry['address'], 20) == unhex(CUSTODY_ADDRESS, 20)
            and entry['topics'][0].lower() == DEPOSIT_TOPIC]
    assert len(logs) == 1 and len(logs[0]['topics']) == 4, 'exactly one custody deposit'
    assert unhex(logs[0]['topics'][2], 32) == bytes.fromhex(ASSET), 'custody deposit asset'
    data = unhex(logs[0]['data'], 96)
    assert data[:32] == bytes.fromhex(beneficiary), 'custody deposit beneficiary'
    assert int.from_bytes(data[32:64], 'big') == amount, 'custody deposit amount'
    assert int(chain.view(CUSTODY_ADDRESS, 'depositCount()'), 16) == 1
    deadline = time.monotonic() + 30
    while int(chain.rpc('eth_blockNumber', []), 16) < int(deposited['blockNumber'], 16) + 2:
        assert time.monotonic() < deadline, 'custody confirmations deadline'
        time.sleep(.1)
    return {'chain_id': 125, 'vault': CUSTODY_ADDRESS, 'runtime_sha256': '0x' + module_identity().hex(),
            'asset': '0x' + ASSET, 'amount': str(amount), 'beneficiary': '0x' + beneficiary,
            'transaction': deposited['transactionHash'], 'deposit_id': logs[0]['topics'][1],
            'deposit_block': int(deposited['blockNumber'], 16)}


def main():
    if len(sys.argv) == 4 and sys.argv[1] == '--register':
        register(Path(sys.argv[2]), sys.argv[3])
        return
    assert os.geteuid() == 0
    parser = argparse.ArgumentParser()
    parser.add_argument('build_dir', nargs='?', default='build')
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument('--module-maintenance', action='store_true')
    modes.add_argument('--handover', action='store_true')
    modes.add_argument('--metered-allowance', action='store_true')
    modes.add_argument('--native-onboarding', action='store_true')
    modes.add_argument('--owner-rotation', action='store_true')
    modes.add_argument('--paid-withdrawal', action='store_true')
    # Export stops before program-admission.sh: DIRECTORY receives the custody profile the node's
    # genesis is built from and the first credit signed by the beneficiary seed, for a node the
    # caller bootstraps itself with that sequencer seed and network id.
    modes.add_argument('--export', metavar='DIRECTORY')
    parser.add_argument('--native-arbiter', action='store_true')
    parser.add_argument('--network-id', type=int, default=NETWORK_ID)
    parser.add_argument('--sequencer-key')
    parser.add_argument('--beneficiary-key')
    parser.add_argument('--artifact-manifest', default=os.environ.get('LAYERX_CUSTODY_ARTIFACT_MANIFEST'))
    args = parser.parse_args()
    assert (args.export is not None) == (args.sequencer_key is not None) == (args.beneficiary_key is not None), \
        '--export, --sequencer-key and --beneficiary-key go together'
    assert 'LAYERX_TEST_NATIVE_ARBITER_NETWORK_ID' not in os.environ, 'select native profile explicitly'
    if args.native_arbiter:
        assert args.handover and args.network_id == 7, '--native-arbiter requires --handover --network-id 7'
        assert os.environ.get('LAYERX_NATIVE_AUTHORITY_FIXTURE_BIN') and os.environ.get('LAYERX_NATIVE_AUTHORITY_OUTPUT'), \
            'native arbiter requires its real fixture executable and output'
        assert not any(name.startswith('LAYERX_TEST_HANDOVER_') for name in os.environ), \
            'native arbiter has an isolated handover profile'
        assert os.environ.get('LAYERX_TEST_SETTLEMENT_PUBLICATION') != '1', 'native arbiter uses native settlement'
    else:
        assert args.export is not None or args.network_id == NETWORK_ID, '--network-id needs --export'
    build = (ROOT / args.build_dir).resolve()
    artifacts_manifest = artifact_manifest(args.artifact_manifest)
    executables = {name: Path(row['path']) for name, row in artifacts_manifest['executables'].items()}
    os.environ['PAXD'] = str(executables['paxd'])
    os.environ['LAYERX_CUSTODY_PROOF_BIN'] = str(executables['layerx-custody-proof'])
    mode = ('--owner-rotation' if args.owner_rotation else
            '--native-onboarding' if args.native_onboarding else
            '--handover' if args.handover else
            '--module-maintenance' if args.module_maintenance else
            '--metered-allowance' if args.metered_allowance else
            '--paid-withdrawal' if args.paid_withdrawal else
            '--export' if args.export is not None else '--withdraw')
    programs_handover = args.handover and bool(os.environ.get('LAYERX_TEST_HANDOVER_PROGRAM_CONSUMER_BIN'))
    amount = 1000000000 if (args.metered_allowance or args.native_onboarding or
                           args.owner_rotation or programs_handover or args.native_arbiter) else 1000000
    assert os.environ.get('LAYERX_TEST_SETTLEMENT_PUBLICATION') != '1' or mode == '--withdraw'
    if args.export is not None:
        for name in ('custody.profile', 'custody.activity', 'custody.json'):
            if (Path(args.export) / name).exists():
                raise FileExistsError('custody export already exists: ' + name)
    logs = Path(args.export) if args.export is not None else ROOT / 'qual-logs/set1'
    logs.mkdir(parents=True, exist_ok=True)
    evidence = Path(tempfile.mkdtemp(prefix='e-daemon-custody-', dir=logs))
    with tempfile.TemporaryDirectory(prefix='lxp-daemon-custody-', dir='/tmp') as directory:
        work = Path(directory)
        work.chmod(0o755)
        print('withdraw custody evidence:', evidence, flush=True)
        try:
            seed = Path(args.beneficiary_key).read_bytes() if args.export is not None else bytes([0x11]) * 32
            sequencer_seed = Path(args.sequencer_key).read_bytes() if args.export is not None else SEQUENCER_SEED
            assert len(seed) == 32 and len(sequencer_seed) == 32, 'seeds are 32 bytes'
            public = Ed25519PrivateKey.from_private_bytes(seed).public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
            did = 'did:layerx:' + public.hex()
            name = ('agent:' + did + ':main').encode()
            beneficiary = hashlib.sha256(b'LX:ACCOUNT:v1' + len(name).to_bytes(4, 'big') + name).hexdigest()
            (work / 'actor').write_bytes(seed)
            (work / 'actor').chmod(0o600)
            artifacts = Path(artifacts_manifest['contract_directory'])
            boundary_binary = executables['layerx-paxeer-boundary']
            with owned_chain(work, artifacts, custody_genesis(work, args.network_id, sequencer_seed)) as first:
                custody = deposit(first, beneficiary, amount)
                (work / 'custody.json').write_text(json.dumps(custody, sort_keys=True) + '\n')
                with boundaries(work, first, boundary_binary) as (origins, ca, identity):
                    retain_custody_proofs(work, origins, ca, identity, custody['vault'])
                    comet = json.loads(first.identity_path.read_text())['comet_url']
                    pair = ['--rpc', origins[0], '--rpc', origins[1], '--ca-bundle', str(ca), '--disposable-identity', str(identity),
                            '--comet-rpc', comet]
                    run(sys.executable, 'tests/bridge/custody_credit.py', 'profile', *pair, '--chain-id', '125',
                        '--network-id', str(args.network_id), '--vault', custody['vault'], '--runtime-sha256', custody['runtime_sha256'],
                        '--asset', '0x' + ASSET, '--trusted-height', '1', '--trusting-period-seconds', '1209600', '--output', work / 'profile')
                    run(sys.executable, 'tests/bridge/custody_credit.py', 'attest', *pair, '--profile', work / 'profile',
                        '--network-id', str(args.network_id), '--transaction', custody['transaction'], '--beneficiary', '0x' + beneficiary,
                        '--beneficiary-key', '0x' + public.hex(), '--expected-amount', str(amount),
                        '--output', work / 'credit')
                    if os.environ.get('LAYERX_CUSTODY_FIXTURE_DIR'):
                        exported = Path(os.environ['LAYERX_CUSTODY_FIXTURE_DIR'])
                        exported.mkdir(parents=True, exist_ok=True)
                        for source, name in ((work/'profile', 'custody.profile'), (work/'credit', 'custody.credit')):
                            with (exported/name).open('xb') as output:
                                output.write(source.read_bytes())
                    run(executables['sign-credit'], work / 'profile', work / 'credit', did, work / 'actor',
                        '0', str(int(time.time() * 1000)), work / 'activity')
                    (work / 'activity').chmod(0o644)
                    if args.export is not None:
                        for source, name in ((work/'profile', 'custody.profile'), (work/'activity', 'custody.activity'),
                                             (work/'custody.json', 'custody.json')):
                            with (Path(args.export)/name).open('xb') as output:
                                output.write(source.read_bytes())
                            (Path(args.export)/name).chmod(0o644)
                    else:
                        env = os.environ | {'LAYERX_TEST_WITHDRAW_PROFILE': str(work / 'profile'),
                            'LAYERX_TEST_WITHDRAW_CREDIT': str(work / 'activity'), 'LAYERX_TEST_WITHDRAW_RPC': first.url,
                            'LAYERX_TEST_ADMISSION_LOG_DIR': str(work), 'LAYERX_TEST_PYTHON': sys.executable,
                            'LAYERX_TEST_CUSTODY_ARTIFACTS': str(artifacts), 'LAYERX_TEST_CUSTODY_FILE': str(work / 'custody.json'),
                            'LAYERX_TEST_CUSTODY_CHAIN_FILE': str(first.identity_path), 'LAYERX_TEST_CUSTODY_BUILD_DIR': str(build)}
                        if args.native_arbiter:
                            env['LAYERX_TEST_NATIVE_ARBITER_NETWORK_ID'] = '7'
                        if os.environ.get('LAYERX_TEST_SETTLEMENT_PUBLICATION') == '1':
                            module = runpy.run_path(str(ROOT / 'tests/daemon/guarantor-publication-chain.py'))
                            module['drive'](work, env, first.url)
                        else:
                            run('bash', 'tests/daemon/program-admission.sh', build, mode, env=env)
        finally:
            for path in work.rglob('*'):
                if path.is_fifo() or path.is_socket():
                    path.unlink()
            retain_public_evidence(work, evidence)
    if args.export is not None:
        print(f'custody deposit and signed first credit for {did} exported to {args.export}')
    else:
        print(f'custody-funded {mode.removeprefix("--")} execution and crash replay passed')


if __name__ == '__main__':
    signal.signal(signal.SIGTERM, lambda signum, frame: sys.exit(128 + signum))
    main()
