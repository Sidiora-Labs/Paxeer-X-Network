#!/usr/bin/env python3
"""Real local custody chain for the local gateway qualification.

Nothing custodial is simulated or deployed here. The chain is a real ``paxd``
brought up by ``tests/daemon/custody_chain.py``, the same path
``tests/daemon/withdraw-custody.py`` and
``human/crates/layerx-paxeer-client/tests/disposable_custody.py`` drive, from
genesis whose ``layerxcustody`` section is written by
``platform/hosted/paxeer/custody-genesis.py``. Custody is therefore the native
module behind the precompile at ``0x0000000000000000000000000000000000001013``
and every deposit is a real transaction to it. The two independent TLS
boundaries over that chain give the verified disposable identity and the pair of
RPC origins ``tests/bridge/custody_credit.py`` requires, and the chain's own
Comet RPC origin is the light-client endpoint its profile and credit are proved
against.

One JSON object per line on stdin, one answer per line on stdout:

``start``    bring the chain and its two boundaries up; answer the EVM origin,
             the Comet RPC origin, the two boundary origins, the CA bundle, the
             disposable identity, the custody precompile and the module identity
``deposit``  deposit base units for one beneficiary account through
             ``deposit(bytes32)`` on the precompile; answer the transaction
``stop``     tear the boundaries and the chain down
"""
import contextlib
import importlib.util
import json
import os
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[5]
sys.path.insert(0, str(ROOT / 'tests/bridge'))
from comet_credit import CUSTODY_ADDRESS, module_identity
from custody_credit import DEPOSIT_TOPIC, require, unhex, write_new
from deploy_local_custody import calldata, command, disposable_rpc, send, signer
from cryptography.hazmat.primitives.asymmetric import ed25519
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat

WEI_PER_BASE_UNIT = 10 ** 12
CUSTODY_DENOM = 'uhpx'
CUSTODY_CHAIN_ID = 125
BETA_USDL_SOURCE = 'platform/hosted/paxeer/contracts/BetaUsdl.sol'
FORGE_ARTIFACTS = 'build/forge-artifacts'


def daemon_custody_chain():
    """``tests/daemon/custody_chain.py`` loaded from its own path.

    This file carries the same basename, so the module is loaded explicitly
    rather than through the import path.
    """
    path = ROOT / 'tests/daemon/custody_chain.py'
    specification = importlib.util.spec_from_file_location('layerx_daemon_custody_chain', path)
    require(specification is not None and specification.loader is not None,
            'daemon custody chain module required')
    module = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(module)
    return module


def hex32(value, name):
    require(isinstance(value, str), name + ' must be a 32-byte hex string')
    return '0x' + unhex(value, 32).hex()


def executable(variable, default):
    path = Path(os.environ.get(variable, default)).resolve()
    require(path.is_file() and os.access(path, os.X_OK),
            'real custody prerequisite is missing: ' + variable + ' ' + str(path))
    return path


class CustodyChain:
    def __init__(self):
        self.stack = contextlib.ExitStack()
        self.chain = None
        self.rpc = None
        self.account = None
        self.asset = None

    def start(self, request):
        require(self.chain is None, 'custody chain already started')
        work = Path(request['work']).resolve()
        require(work.is_dir(), 'existing work directory required')
        asset = hex32(request['asset'], 'asset')
        network_id = int(request['network_id'])
        require(0 < network_id < 2 ** 32, 'network id')
        boundary = executable('LAYERX_PAXEER_BOUNDARY_BIN',
                              Path(os.environ.get('CARGO_TARGET_DIR', ROOT / '.lane-target'))
                              / 'debug/layerx-paxeer-boundary')
        proof = executable('LAYERX_CUSTODY_PROOF_BIN', ROOT / 'build/bin/layerx-custody-proof')
        os.environ['LAYERX_CUSTODY_PROOF_BIN'] = str(proof)
        node = executable('PAXD', ROOT / 'build/paxd')
        os.environ['PAXD'] = str(node)
        module = daemon_custody_chain()
        # Custody is the native layerxcustody module, so nothing custodial is built here: the
        # only contract the chain still needs is the BetaUsdl runtime init-chain.sh writes into
        # genesis.
        command('forge', 'build', BETA_USDL_SOURCE)
        authority = ed25519.Ed25519PrivateKey.generate().public_key().public_bytes(
            Encoding.Raw, PublicFormat.Raw)
        custody_genesis = work / 'custody-genesis.json'
        command('python3', str(ROOT / 'platform/hosted/paxeer/custody-genesis.py'),
                '--network-id', str(network_id),
                '--sequencer-id', hex32(request['sequencer_id'], 'sequencer id'),
                '--sequencer-public-key', hex32(request['sequencer_public_key'], 'sequencer key'),
                '--deposit-root-authority', '0x' + authority.hex(),
                '--asset', asset + ':' + CUSTODY_DENOM,
                '--output', str(custody_genesis))
        chain = self.stack.enter_context(
            module.owned_chain(work, ROOT / FORGE_ARTIFACTS, custody_genesis))
        origins, ca, identity = self.stack.enter_context(
            module.boundaries(work, chain, boundary))
        disposable = json.loads(Path(identity).read_text())
        require(disposable['chain_id'] == CUSTODY_CHAIN_ID, 'native custody chain identity')
        owned = json.loads(chain.identity_path.read_text())
        key_file = work / 'custody-deployer.key'
        write_new(key_file, ('0x' + Path(owned['deployer_key']).read_bytes().hex()).encode())
        rpc = disposable_rpc(origins[0], str(ca), str(identity))
        account = signer(rpc, key_file)
        require(unhex(account, 20) == unhex(owned['deployer'], 20), 'chain deployer account')
        native = rpc.call('eth_call', [{'to': CUSTODY_ADDRESS,
                                       'data': calldata('nativeAssetId()')}, 'latest'])
        require(unhex(native, 32) == unhex(asset, 32),
                'custody genesis maps another native asset')
        self.chain, self.rpc, self.account, self.asset = chain, rpc, account, asset
        return {'evm_rpc': chain.url, 'comet_rpc': owned['comet_url'], 'origins': list(origins),
                'ca_bundle': str(ca), 'disposable_identity': str(identity),
                'chain_id': CUSTODY_CHAIN_ID, 'comet_chain_id': disposable['comet_chain_id'],
                'vault': CUSTODY_ADDRESS, 'runtime_sha256': '0x' + module_identity().hex(),
                'asset': asset}

    def deposit(self, request):
        require(self.rpc is not None, 'custody chain not started')
        beneficiary = hex32(request['beneficiary'], 'beneficiary')
        amount = int(request['amount'])
        require(0 < amount < 2 ** 128, 'deposit base units')
        receipt = send(self.rpc, self.account, CUSTODY_ADDRESS,
                       calldata('deposit(bytes32)', beneficiary), amount * WEI_PER_BASE_UNIT)
        logs = [entry for entry in receipt['logs']
                if unhex(entry['address'], 20) == unhex(CUSTODY_ADDRESS, 20)
                and entry['topics'][0].lower() == DEPOSIT_TOPIC]
        require(len(logs) == 1 and len(logs[0]['topics']) == 4, 'exactly one custody deposit')
        require(unhex(logs[0]['topics'][2], 32) == unhex(self.asset, 32), 'custody deposit asset')
        data = unhex(logs[0]['data'], 96)
        require(data[:32] == unhex(beneficiary, 32), 'custody deposit beneficiary')
        require(int.from_bytes(data[32:64], 'big') == amount, 'custody deposit amount')
        return {'transaction': receipt['transactionHash'], 'beneficiary': beneficiary,
                'amount': str(amount)}

    def stop(self):
        self.stack.close()
        self.chain, self.rpc, self.account = None, None, None
        return {'stopped': True}


def main():
    custody = CustodyChain()
    try:
        for line in sys.stdin:
            line = line.strip()
            if not line:
                continue
            request = json.loads(line)
            name = request['command']
            if name == 'start':
                answer = custody.start(request)
            elif name == 'deposit':
                answer = custody.deposit(request)
            elif name == 'stop':
                answer = custody.stop()
            else:
                raise ValueError('unknown custody chain command: ' + str(name))
            sys.stdout.write(json.dumps(answer, sort_keys=True) + '\n')
            sys.stdout.flush()
            if name == 'stop':
                break
    finally:
        custody.stack.close()


if __name__ == '__main__':
    main()
