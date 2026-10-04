import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
SYMBOLS = ('PAX', 'SID', 'USDC', 'USDL')


def require(value, message):
    if not value:
        raise ValueError(message)


def run(*args):
    subprocess.run([str(arg) for arg in args], cwd=ROOT, check=True)


def no_duplicates(items):
    result = {}
    for key, value in items:
        require(key not in result, 'duplicate approved metadata field')
        result[key] = value
    return result


def approved_metadata(path):
    metadata = json.loads(path.read_text(), object_pairs_hook=no_duplicates)
    require(isinstance(metadata, dict) and set(metadata) == {'assets'}
            and isinstance(metadata['assets'], list) and len(metadata['assets']) == 4,
            'exact approved four-asset metadata required')
    pointers = set()
    for symbol, asset in zip(SYMBOLS, metadata['assets']):
        require(isinstance(asset, dict)
                and set(asset) == {'symbol', 'asset_id', 'token_pointer', 'decimals'},
                'exact approved asset fields required')
        expected = hashlib.sha256(('layerx-asset:125:' + symbol).encode()).hexdigest()
        require(asset['symbol'] == symbol and asset['asset_id'] == expected,
                'approved asset identity or order mismatch')
        pointer = asset['token_pointer']
        require(isinstance(pointer, str) and len(pointer) == 42 and pointer.startswith('0x')
                and pointer == pointer.lower() and len(bytes.fromhex(pointer[2:])) == 20,
                'approved token pointer encoding')
        require(type(asset['decimals']) is int and 0 <= asset['decimals'] <= 38,
                'approved token decimals')
        if symbol == 'PAX':
            require(pointer == '0x' + '00' * 20 and asset['decimals'] == 6,
                    'approved native asset metadata')
        else:
            require(pointer != '0x' + '00' * 20 and pointer not in pointers,
                    'approved token pointer must be nonzero and distinct')
            pointers.add(pointer)
    return metadata


def validate_registry(registry):
    require(len(registry) == 901 and registry[:5] == b'LXBR1', 'closed custody registry framing')
    profiles = []
    for index, symbol in enumerate(SYMBOLS):
        at = 5 + index * 224
        profile = registry[at + 1:at + 224]
        asset = hashlib.sha256(('layerx-asset:125:' + symbol).encode()).digest()
        name = ('system:paxeer-reserve:' + symbol.lower()).encode()
        reserve = hashlib.sha256(b'LX:ACCOUNT:v1' + len(name).to_bytes(4, 'big') + name).digest()
        require(registry[at] == index + 1 and profile[:5] == b'LXBC4'
                and profile[97:129] == asset and profile[129:161] == reserve
                and int.from_bytes(profile[5:13], 'big') == 125
                and profile[205:207] == b'\0\3', 'asset-specific custody profile identity')
        if profiles:
            require(profile[5:97] == profiles[0][5:97]
                    and profile[161:223] == profiles[0][161:223], 'registry trust/domain mismatch')
        profiles.append(profile)
    return profiles


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--build', action='store_true')
    parser.add_argument('--fixture', type=Path,
                        default=Path(os.environ.get('LAYERX_MULTI_ASSET_CUSTODY_FIXTURE',
                                                    ROOT / 'tests/fixtures/custody/paxeer-multiasset-v1')))
    parser.add_argument('--build-dir', type=Path, default=Path(os.environ.get('BUILD_DIR', ROOT / 'build')))
    args = parser.parse_args()
    build = args.build_dir.resolve()
    if args.build:
        run('flock', '/root/lx-cargo/native-build.lock', 'make', '-j6',
            'BUILD_DIR=' + str(build), str(build / 'tests/bridge/test-credit'),
            str(build / 'tests/bridge/test-credit-admission'),
            str(build / 'tests/bridge/sign-credit'),
            str(build / 'tests/lxp_test_maintenance_publication'),
            str(build / 'bin/layerx-genesis-build'),
            str(build / 'bin/layerx-guarantor'), 'custody-proof-build')
        return
    fixture = args.fixture.resolve()
    required = ['custody-assets.json', 'custody.registry', 'request.lxgb', 'genesis.key']
    required += [symbol + suffix for symbol in SYMBOLS for suffix in ('.credit', '.actor.key')]
    missing = [name for name in required if not (fixture / name).is_file()]
    if missing:
        print('Missing genuine approved multi-asset fixture inputs: ' + ', '.join(missing), file=sys.stderr)
        raise SystemExit(78)
    approved_metadata(fixture / 'custody-assets.json')
    profiles = validate_registry((fixture / 'custody.registry').read_bytes())
    require(len((fixture / 'genesis.key').read_bytes()) == 32, 'genuine genesis signing key length')
    spec = importlib.util.spec_from_file_location('multiasset_publication',
                                                ROOT / 'cmd/layerx-guarantor/publication.py')
    publication = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(publication)
    with tempfile.TemporaryDirectory(prefix='multiasset-credit-') as directory:
        work = Path(directory)
        run(build / 'bin/layerx-genesis-build', fixture / 'request.lxgb', fixture / 'genesis.key',
            work / 'genesis', '--custody-registry', fixture / 'custody.registry')
        manifest = work / 'genesis/genesis.manifest'
        credit_args = []
        for index, symbol in enumerate(SYMBOLS):
            profile = profiles[index]
            (work / (symbol + '.profile')).write_bytes(profile)
            credit = (fixture / (symbol + '.credit')).read_bytes()
            require(len(credit) >= 368 and credit[:5] == b'LXDC3'
                    and credit[5:37] == hashlib.sha256(profile).digest()
                    and credit[75:107] == profile[97:129]
                    and credit[327:359] == hashlib.sha256(credit[363:]).digest(),
                    'genuine asset-bound finalized credit input')
            owner = credit[139:171]
            did = 'did:layerx:' + owner.hex()
            timestamp = int.from_bytes(credit[392:400], 'big') * 1000
            activity = work / (symbol + '.activity')
            run(build / 'tests/bridge/sign-credit', '--asset-profile', work / (symbol + '.profile'),
                fixture / (symbol + '.credit'), did, fixture / (symbol + '.actor.key'),
                '0', timestamp, activity)
            credit_args.extend([activity, fixture / (symbol + '.actor.key')])
            run(build / 'tests/bridge/test-credit-admission', '--multiasset', manifest, activity)
            evidence = work / (symbol + '-publication')
            run(build / 'tests/lxp_test_maintenance_publication', '--multiasset', manifest, activity, evidence)
            facts = json.loads((evidence / 'native-facts.json').read_text())
            root = (evidence / 'native-state-root.bin').read_bytes()
            require(len(root) == 32, 'actual native publication root required')
            network = int.from_bytes(profile[201:205], 'big')
            header = [3, network, 0, 0, 0, 0, 0, root]
            _, _, deposits, selected = publication.native_request(
                None, {'native_facts': facts}, header, bytes(32))
            require(len(selected) == 4 and selected[profile[97:129]] == profile
                    and len(deposits) == 1, 'actual per-asset publication selection')
            require(publication.custody_vault_for_deposits(selected, deposits) == profile[13:33],
                    'actual custody authorization vault')
            for malformed in ('missing', 'duplicate', 'swap', 'marker'):
                changed = json.loads(json.dumps(facts))
                if malformed == 'missing':
                    changed['profiles'].pop()
                elif malformed == 'duplicate':
                    changed['profiles'][1] = changed['profiles'][0]
                elif malformed == 'swap':
                    changed['profiles'][0]['profile'] = changed['profiles'][1]['profile']
                else:
                    changed['registry'] = changed['profiles'][0]['profile']
                try:
                    publication.native_request(None, {'native_facts': changed}, header, bytes(32))
                except (ValueError, KeyError, TypeError):
                    pass
                else:
                    raise AssertionError('malformed native registry accepted: ' + malformed)
        run(build / 'tests/bridge/test-credit', '--multiasset', manifest, *credit_args)
        print('Real four-asset custody, conservation, snapshot and publication corpus passed')


if __name__ == '__main__':
    main()
