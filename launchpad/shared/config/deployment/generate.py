#!/usr/bin/env python3
"""Normalize an explicitly reviewed manifest; never query or modify a network.

activationHeight is the recorded candidate deployment completion height. It
cannot establish an active address epoch while status remains pending governance.
The external USDL code identity is frozen provenance, not a fresh attestation.
"""
import argparse
import hashlib
import json
from pathlib import Path


# The contracts manifest records "finalized" once PoolBeacon ownership is held by
# the Timelock; the runtime deployment vocabulary calls that epoch "active".
RECORDED_STATUS = {'pending-beacon-acceptance': 'pending-beacon-acceptance', 'finalized': 'active'}


def deployment_status(recorded: str) -> str:
    if recorded not in RECORDED_STATUS:
        raise ValueError('unknown_recorded_status:' + recorded)
    return RECORDED_STATUS[recorded]


def generate(source: bytes, frozen_client: dict, frozen_attestation: dict) -> dict:
    manifest = json.loads(source)
    core = frozen_client['core']
    digest = hashlib.sha256(source).hexdigest()
    if digest != core['manifestSha256'] or manifest['_meta']['network'] != '125':
        raise ValueError('reviewed_manifest_identity_mismatch')
    if core['chainId'] != 125 or str(frozen_attestation['chainId']) != '125':
        raise ValueError('wrong_chain')
    records = manifest['_deployment']['records']
    roles = {}
    names = ('EventEmitter', 'ProtocolConfig', 'Router', 'Quoter',
             'SidioraFactory', 'PoolRegistry', 'FeeAccumulator', 'SidioraNFT',
             'FeesRouter', 'OpticalRegistry')
    for name in names:
        proxy = records[name + '_proxy']
        implementation = records[name + '_impl']
        if proxy['status'] != 'confirmed' or implementation['status'] != 'confirmed':
            raise ValueError('deployment_record_unconfirmed:' + name)
        if manifest[name + '_proxy'].lower() != proxy['address'].lower():
            raise ValueError('deployment_record_conflict:' + name)
        roles[name] = dict(address=proxy['address'], kind='erc1967',
                           runtimeHash=proxy['runtimeHash'],
                           implementationAddress=implementation['address'],
                           implementationHash=implementation['runtimeHash'])
    beacon = records['PoolBeacon']
    pool = records['SidioraPool_impl']
    if frozen_attestation['beacon']['expectedOwner'].lower() != manifest['Timelock'].lower():
        raise ValueError('expected_governance_conflict')
    roles['PoolBeacon'] = dict(address=beacon['address'], kind='beacon',
                               runtimeHash=beacon['runtimeHash'],
                               implementationAddress=pool['address'],
                               implementationHash=pool['runtimeHash'])
    if (frozen_attestation['expectedUsdl'].lower() != core['usdl'].lower()
            or not frozen_attestation['usdlCodeHashMatchesManifest']):
        raise ValueError('settlement_identity_conflict')
    roles['USDL'] = dict(address=core['usdl'], kind='direct',
                         runtimeHash=frozen_attestation['usdlCodeHash'])
    return dict(schemaVersion=1, deploymentVersion='paxeer125-' + digest[:16],
                sourceManifestSha256=digest, chainId=125,
                activationHeight=max(r['blockNumber'] for r in records.values()),
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
    p.add_argument('--frozen-attestation', required=True, type=Path)
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
    result = generate(read(args.source), json.loads(read(args.frozen_client)),
                      json.loads(read(args.frozen_attestation)))
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
