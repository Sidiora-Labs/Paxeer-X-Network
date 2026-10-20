#!/usr/bin/env python3
"""Normalize an explicitly reviewed manifest; never query or modify a network.

activationHeight is the v6.12.0 activation height, an explicit input that must
lie after the recorded deployment completion height. The nine generation 2
surfaces carry the surface runtime hash; the launchpad precompile sits at 0x1017.
The external USDL code identity is frozen provenance, not a fresh attestation.
"""
import argparse
import hashlib
import json
from pathlib import Path


# The contracts manifest records "finalized" once PoolBeacon ownership is held by
# the Timelock; the runtime deployment vocabulary calls that epoch "active".
RECORDED_STATUS = {'pending-beacon-acceptance': 'pending-beacon-acceptance', 'finalized': 'active'}

RELEASE = 'v6.12.0'
ACTIVATION_HEIGHT = 37220320
SURFACE_RUNTIME_HASH = '0x6302ff80f4168c2af23afbf3117c613edd66d288d120eee825f9ecadd43a392f'
PRECOMPILE_ROLE = 'LaunchpadPrecompile'
PRECOMPILE_ADDRESS = '0x0000000000000000000000000000000000001017'

# Roles in manifest order; True marks an ERC-1967 proxy, False a generation 2 surface.
PROXY_ROLES = (('EventEmitter', True), ('ProtocolConfig', False), ('Router', False),
               ('Quoter', False), ('SidioraFactory', False), ('PoolRegistry', False),
               ('FeeAccumulator', False), ('SidioraNFT', False), ('FeesRouter', False),
               ('OpticalRegistry', True))


def deployment_status(recorded: str) -> str:
    if recorded not in RECORDED_STATUS:
        raise ValueError('unknown_recorded_status:' + recorded)
    return RECORDED_STATUS[recorded]


def confirmed(manifest: dict, records: dict, name: str) -> dict:
    proxy = records[name + '_proxy']
    implementation = records[name + '_impl']
    if proxy['status'] != 'confirmed' or implementation['status'] != 'confirmed':
        raise ValueError('deployment_record_unconfirmed:' + name)
    if manifest[name + '_proxy'].lower() != proxy['address'].lower():
        raise ValueError('deployment_record_conflict:' + name)
    return dict(proxy=proxy, implementation=implementation)


def surface(manifest: dict, records: dict, name: str, runtime_hash: str) -> dict:
    proxy = confirmed(manifest, records, name)['proxy']
    return dict(address=proxy['address'], kind='surface', runtimeHash=runtime_hash)


def generate(source: bytes, frozen_client: dict, activation_height: int,
             surface_runtime_hash: str = SURFACE_RUNTIME_HASH,
             frozen_attestation: dict = None) -> dict:
    manifest = json.loads(source)
    core = frozen_client['core']
    digest = hashlib.sha256(source).hexdigest()
    if digest != core['manifestSha256'] or manifest['_meta']['network'] != '125':
        raise ValueError('reviewed_manifest_identity_mismatch')
    if core['chainId'] != 125:
        raise ValueError('wrong_chain')
    if (len(surface_runtime_hash) != 66 or not surface_runtime_hash.startswith('0x')
            or surface_runtime_hash != surface_runtime_hash.lower()):
        raise ValueError('surface_runtime_hash_malformed')
    int(surface_runtime_hash[2:], 16)
    records = manifest['_deployment']['records']
    deployed = max(r['blockNumber'] for r in records.values())
    if activation_height <= deployed:
        raise ValueError('activation_height_not_after_deployment:%d' % deployed)
    roles = {}
    for name, proxied in PROXY_ROLES:
        if not proxied:
            roles[name] = surface(manifest, records, name, surface_runtime_hash)
            continue
        r = confirmed(manifest, records, name)
        roles[name] = dict(address=r['proxy']['address'], kind='erc1967',
                           runtimeHash=r['proxy']['runtimeHash'],
                           implementationAddress=r['implementation']['address'],
                           implementationHash=r['implementation']['runtimeHash'])
    beacon = records['PoolBeacon']
    pool = records['SidioraPool_impl']
    if beacon['status'] != 'confirmed' or pool['status'] != 'confirmed':
        raise ValueError('deployment_record_unconfirmed:PoolBeacon')
    roles['PoolBeacon'] = dict(address=beacon['address'], kind='beacon',
                               runtimeHash=beacon['runtimeHash'],
                               implementationAddress=pool['address'],
                               implementationHash=pool['runtimeHash'])
    config = manifest['_deployment']['config']
    usdl_hash = config['usdlCodeHash']
    if (config['usdl'].lower() != core['usdl'].lower()
            or manifest['_meta']['usdl'].lower() != core['usdl'].lower()):
        raise ValueError('settlement_identity_conflict')
    if frozen_attestation is not None:
        if str(frozen_attestation['chainId']) != '125':
            raise ValueError('wrong_chain')
        if frozen_attestation['beacon']['expectedOwner'].lower() != manifest['Timelock'].lower():
            raise ValueError('expected_governance_conflict')
        if (frozen_attestation['expectedUsdl'].lower() != core['usdl'].lower()
                or not frozen_attestation['usdlCodeHashMatchesManifest']
                or frozen_attestation['usdlCodeHash'] != usdl_hash):
            raise ValueError('settlement_identity_conflict')
    roles['USDL'] = dict(address=core['usdl'], kind='direct', runtimeHash=usdl_hash)
    roles['Treasury'] = surface(manifest, records, 'Treasury', surface_runtime_hash)
    roles[PRECOMPILE_ROLE] = dict(address=PRECOMPILE_ADDRESS, kind='precompile')
    return dict(schemaVersion=1, deploymentVersion='paxeer125-' + RELEASE,
                sourceManifestSha256=digest, chainId=125,
                activationHeight=activation_height,
                status=deployment_status(core['recordedStatus']), roles=roles,
                native=dict(symbol='PAX', decimals=18),
                settlement=dict(symbol='USDL', decimals=6),
                creationFeeRaw='1000000',
                governance=dict(mode='timelock-handoff',
                                expectedBeaconOwner=manifest['Timelock'],
                                timelockDelaySeconds=manifest['_meta']['timelockDelay']),
                marketModel='AMM', graduation=False, coreOracleRequired=False)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--source', required=True, type=Path)
    p.add_argument('--frozen-client', required=True, type=Path)
    p.add_argument('--frozen-attestation', type=Path)
    p.add_argument('--activation-height', type=int, default=ACTIVATION_HEIGHT)
    p.add_argument('--surface-runtime-hash', default=SURFACE_RUNTIME_HASH)
    p.add_argument('--output', required=True, type=Path)
    p.add_argument('--check', action='store_true')
    args = p.parse_args()
    # Bound each source read before decoding.
    def read(path):
        with path.open('rb') as f:
            data = f.read(2 * 1024 * 1024 + 1)
        if len(data) > 2 * 1024 * 1024:
            raise ValueError('source_size_limit')
        return data
    attestation = json.loads(read(args.frozen_attestation)) if args.frozen_attestation else None
    result = generate(read(args.source), json.loads(read(args.frozen_client)),
                      args.activation_height, args.surface_runtime_hash, attestation)
    rendered = (json.dumps(result, indent=2) + '\n').encode()
    if args.check:
        if read(args.output) != rendered:
            raise ValueError('generated_manifest_drift')
    else:
        args.output.write_bytes(rendered)
    print(json.dumps(dict(checked=args.check, sourceManifestSha256=result['sourceManifestSha256'],
                         status=result['status'], releaseEligible=False)))


if __name__ == '__main__':
    main()
