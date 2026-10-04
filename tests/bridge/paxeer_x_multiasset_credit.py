import argparse
import hashlib
import importlib.util
import json
import os
import re
import shutil
import stat
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
SYMBOLS = ('PAX', 'SID', 'USDC', 'USDL')
NATIVE_PATHS = ('Makefile', 'src', 'include', 'cmd', 'programs', 'agent',
                'platform', 'contracts/config/checkpoint-settlement.json')
NATIVE_ARTIFACTS = {
    'credit': 'tests/bridge/test-credit',
    'admission': 'tests/bridge/test-credit-admission',
    'sign_credit': 'tests/bridge/sign-credit',
    'publication': 'tests/lxp_test_maintenance_publication',
    'genesis': 'bin/layerx-genesis-build',
}
EXECUTED = 0


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


def git(*arguments):
    return subprocess.check_output(['git', *arguments], cwd=ROOT).decode().strip()


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def private_document(path):
    path = Path(path)
    info = path.lstat()
    require(path.is_absolute() and not any(p.is_symlink() for p in (path, *path.parents))
            and ROOT not in path.parents and stat.S_ISREG(info.st_mode)
            and info.st_uid == os.geteuid() and info.st_nlink == 1
            and not info.st_mode & 0o077 and info.st_size <= 131072,
            'owned private bounded manifest outside source required')
    return json.loads(path.read_text(), object_pairs_hook=no_duplicates)


def artifact(row):
    require(set(row) == {'path', 'sha256'}, 'closed executable artifact required')
    path = Path(row['path'])
    require(path.is_absolute() and not any(p.is_symlink() for p in (path, *path.parents))
            and path.is_file() and os.access(path, os.X_OK)
            and path.stat().st_uid == os.geteuid() and not path.stat().st_mode & 0o022,
            'real protected executable required')
    with path.open('rb') as stream:
        require(stream.read(4) == b'\x7fELF', 'actual executable ELF required')
    require(digest(path) == row['sha256'], 'executable digest mismatch')
    return path


def connected_inputs(fixture, build):
    require(os.geteuid() == 0, 'actual namespace corpus requires root')
    require(not git('status', '--porcelain', '--untracked-files=all'), 'complete clean candidate required')
    manifest_name = os.environ.get('LAYERX_MULTI_ASSET_BUILD_MANIFEST')
    runtime_name = os.environ.get('LAYERX_MULTI_ASSET_RUNTIME_FIXTURE')
    native_name = os.environ.get('LAYERX_MULTI_ASSET_NATIVE_MANIFEST')
    missing = [name for name, value in (
        ('LAYERX_MULTI_ASSET_BUILD_MANIFEST', manifest_name),
        ('LAYERX_MULTI_ASSET_RUNTIME_FIXTURE', runtime_name),
        ('LAYERX_MULTI_ASSET_NATIVE_MANIFEST', native_name)) if not value or not Path(value).is_file()]
    if missing:
        print('Missing genuine connected multi-asset inputs: ' + ', '.join(missing), file=sys.stderr)
        raise SystemExit(78)
    candidate = private_document(manifest_name)
    require(set(candidate) == {'version', 'source_revision', 'source_tree', 'build_exit', 'artifacts'}
            and candidate['version'] == 1 and candidate['build_exit'] == 0
            and candidate['source_revision'] == git('rev-parse', 'HEAD')
            and candidate['source_tree'] == git('rev-parse', 'HEAD^{tree}')
            and set(candidate['artifacts']) == {'custody_asset_send', 'probe'},
            'actual candidate Rust build required')
    binaries = {name: artifact(row) for name, row in candidate['artifacts'].items()}
    native = private_document(native_name)
    require(set(native) == {'version', 'source_revision', 'source_binding', 'source_paths', 'artifacts'}
            and native['version'] == 1 and native['source_paths'] == list(NATIVE_PATHS)
            and re.fullmatch('[0-9a-f]{40}', native['source_revision']), 'closed genuine native manifest required')
    for revision in (native['source_revision'], 'HEAD'):
        binding = hashlib.sha256(subprocess.check_output(
            ['git', 'ls-tree', '-r', '-z', '--full-tree', revision, '--', *NATIVE_PATHS], cwd=ROOT)).hexdigest()
        require(binding == native['source_binding'], 'native source compatibility mismatch')
    require(set(native['artifacts']) == {*NATIVE_ARTIFACTS, 'layerxd', 'layerxctl',
                                         'receipt_authority', 'relay', 'custody_proof'},
            'complete native and actual runtime artifacts required')
    natives = {name: artifact(row) for name, row in native['artifacts'].items()}
    for name, relative in NATIVE_ARTIFACTS.items():
        require(natives[name] == build / relative, 'prebuilt native corpus path mismatch: ' + name)
    runtime = private_document(runtime_name)
    require(set(runtime) == {'version', 'namespace_pid', 'runtime_root', 'run_root', 'tls_root',
                             'network_id', 'processes'} and runtime['version'] == 1
            and runtime['network_id'] == 125 and type(runtime['namespace_pid']) is int
            and runtime['namespace_pid'] > 1,
            'closed genuine owner runtime fixture required')
    pid = runtime['namespace_pid']
    net = os.stat(f'/proc/{pid}/ns/net')
    own = os.stat('/proc/self/ns/net')
    require((net.st_dev, net.st_ino) != (own.st_dev, own.st_ino), 'live network namespace forbidden')
    require(os.stat(f'/proc/{pid}/ns/mnt').st_ino == os.stat('/proc/self/ns/mnt').st_ino,
            'fixture filesystem must be visible without entering production mounts')
    require(set(runtime['processes']) == {'sequencer', 'replica', 'receipt_authority', 'relay'},
            'actual node, receipt and relay processes required')
    for role, row in runtime['processes'].items():
        require(set(row) == {'pid', 'start_time_ticks'} and type(row['pid']) is int
                and row['pid'] > 1 and type(row['start_time_ticks']) is int,
                'closed actual process identity required')
        process = Path('/proc') / str(row['pid'])
        require(int((process / 'stat').read_text().rsplit(')', 1)[1].split()[19]) == row['start_time_ticks'],
                'runtime process was replaced')
        actual_net = (process / 'ns/net').stat()
        require((actual_net.st_dev, actual_net.st_ino) == (net.st_dev, net.st_ino),
                'runtime process escaped isolated network namespace')
        name = 'layerxd' if role in ('sequencer', 'replica') else role
        require((process / 'exe').resolve() == natives[name], 'actual runtime executable mismatch: ' + role)
    for key in ('runtime_root', 'run_root', 'tls_root'):
        path = Path(runtime[key])
        require(path.is_absolute() and path.is_dir() and path != ROOT and ROOT not in path.parents
                and not any(p.is_symlink() for p in (path, *path.parents)), 'isolated real runtime directories required')
    require((Path(runtime['runtime_root']) / 'genesis/custody.registry').read_bytes()
            == (fixture / 'custody.registry').read_bytes(), 'runtime registry differs from approved fixture')
    for role in ('sender', 'recipient'):
        key = Path(runtime['runtime_root']) / 'keys/value-loop' / (role + '.key')
        info = key.lstat()
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_size == 32
                and info.st_uid == 4021 and not info.st_mode & 0o077,
                'existing genuine isolated signer required')
    require(all((Path(runtime['runtime_root']) / 'value-loop/assets' / symbol).is_dir()
                for symbol in SYMBOLS), 'owner-provisioned funded asset state required')
    return runtime, binaries, natives


def connected_corpus(runtime, binaries, natives):
    global EXECUTED
    namespace = ['nsenter', '--target', str(runtime['namespace_pid']), '--net', '--']
    env = os.environ.copy()
    env.update(LAYERX_KERNEL_DATA=runtime['runtime_root'], LAYERX_KERNEL_RUN=runtime['run_root'],
               LAYERX_KERNEL_TLS=runtime['tls_root'], LAYERX_NODE_NETWORK_ID='125')
    with tempfile.TemporaryDirectory(prefix='multiasset-tools-') as directory:
        tools = Path(directory)
        tools.chmod(0o755)
        for name, path in (('layerx-node-probe', binaries['probe']), ('layerxctl', natives['layerxctl']),
                           ('sign-credit', natives['sign_credit']), ('layerx-custody-proof', natives['custody_proof'])):
            target = tools / name
            shutil.copyfile(path, target)
            target.chmod(0o755)
            require(digest(target) == digest(path), 'ephemeral executable copy differs from actual artifact')
        env['PATH'] = str(tools) + os.pathsep + os.environ.get('PATH', '')
        for symbol in SYMBOLS:
            outputs = []
            for _ in range(2):
                result = subprocess.run([*namespace, 'bash', str(ROOT / 'tools/bringup/value-loop.sh'),
                                         'ASSET=' + symbol, 'CHECKPOINT_SECONDS=30'], cwd=ROOT, env=env,
                                        stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=180)
                print(result.stdout, end='', flush=True)
                require(result.returncode == 0, 'actual asset value loop failed: ' + symbol)
                asset_id = hashlib.sha256(('layerx-asset:125:' + symbol).encode()).hexdigest()
                require('asset ' + symbol + ' id=' + asset_id in result.stdout.splitlines(),
                        'value-loop selected wrong approved asset')
                accounts = re.findall(r'^account (sender|recipient) did=(did:layerx:[0-9a-f]{64}) '
                                      r'main=[0-9a-f]{64} asset_account=(\S+) account_id=([0-9a-f]{64})$',
                                      result.stdout, re.M)
                require(len(accounts) == 2 and {row[0] for row in accounts} == {'sender', 'recipient'},
                        'both selected authenticated asset accounts required')
                for _, did, account, account_id in accounts:
                    expected = 'agent:' + did + ':asset:' + asset_id
                    encoded = expected.encode()
                    require(account == expected and account_id == hashlib.sha256(
                        b'LX:ACCOUNT:v1' + len(encoded).to_bytes(4, 'big') + encoded).hexdigest(),
                        'value-loop used a main account or another asset beneficiary')
                activities = re.findall(r'^activity id=([0-9a-f]{64})$', result.stdout, re.M)
                require(len(activities) == 1 and re.search(r'^checkpoint .*status=(submitted|final)$', result.stdout, re.M),
                        'actual stable Send and checkpoint evidence required')
                balances = re.findall(r'^balance (sender|recipient) (.+)$', result.stdout, re.M)
                require(len(balances) == 2 and {role for role, _ in balances} == {'sender', 'recipient'},
                        'both actual asset account balances required')
                require(all(json.loads(value).get('balance') is not None
                            and json.loads(value).get('refused') is None for _, value in balances),
                        'actual balance read was refused')
                outputs.append((activities[0], balances))
            require(outputs[0] == outputs[1], 'asset replay changed original Send or balances')
            EXECUTED += 1
        result = subprocess.run([*namespace, str(binaries['custody_asset_send']), '--test-threads=1', '--nocapture'],
                                cwd=ROOT, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                text=True, timeout=180)
        print(result.stdout, end='', flush=True)
        summaries = re.findall(r'^test result: (ok|FAILED)\. (\d+) passed; (\d+) failed; (\d+) ignored; '
                               r'(\d+) measured; (\d+) filtered out;', result.stdout, re.M)
        require(result.returncode == 0 and len(summaries) == 1 and summaries[0][0] == 'ok'
                and int(summaries[0][1]) > 0 and all(value == '0' for value in summaries[0][2:]),
                'genuine focused core cases failed, skipped or empty')
        EXECUTED += int(summaries[0][1])


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--build', action='store_true')
    parser.add_argument('--connected', action='store_true')
    parser.add_argument('--fixture', type=Path,
                        default=Path(os.environ.get('LAYERX_MULTI_ASSET_CUSTODY_FIXTURE',
                                                    ROOT / 'tests/fixtures/custody/paxeer-multiasset-v1')))
    parser.add_argument('--build-dir', type=Path, default=Path(os.environ.get('BUILD_DIR', ROOT / 'build')))
    args = parser.parse_args()
    build = args.build_dir.resolve()
    require(not (args.build and args.connected), 'connected qualification consumes prebuilt native artifacts only')
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
    runtime = binaries = natives = None
    if args.connected:
        runtime, binaries, natives = connected_inputs(fixture, build)
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
    global EXECUTED
    EXECUTED += 25
    if args.connected:
        connected_corpus(runtime, binaries, natives)


if __name__ == '__main__':
    status = 1
    revision = git('rev-parse', 'HEAD')
    evidence = os.environ.get('LAYERX_MULTI_ASSET_GATE_EVIDENCE')
    evidence_validated = False
    try:
        if '--connected' in sys.argv:
            require(evidence, 'explicit private gate evidence path required')
            destination = Path(evidence)
            require(destination.is_absolute() and ROOT not in destination.parents and not destination.exists()
                    and not any(p.is_symlink() for p in (destination, *destination.parents))
                    and destination.parent.stat().st_uid == os.geteuid()
                    and not destination.parent.stat().st_mode & 0o077, 'new private evidence path required')
            evidence_validated = True
        main()
        status = 0
    except SystemExit as error:
        status = error.code if type(error.code) is int else 1
    except (ValueError, OSError, KeyError, AssertionError, subprocess.SubprocessError) as error:
        print('multiasset-credit: refusal: ' + str(error), file=sys.stderr)
    finally:
        if '--connected' in sys.argv and evidence_validated:
            value = {'revision': revision, 'command': ['timeout', '15m', 'bash', 'tools/paxeer-x/gates/5.1.sh', 'verify'],
                     'exit_code': status, 'cases': EXECUTED, 'skipped': 0}
            try:
                fd = os.open(evidence, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
                with os.fdopen(fd, 'w') as stream:
                    json.dump(value, stream, sort_keys=True); stream.write('\n'); stream.flush(); os.fsync(stream.fileno())
                fd = os.open(destination.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
                try:
                    os.fsync(fd)
                finally:
                    os.close(fd)
                print('PAXEER_X_MULTI_ASSET_GATE revision=' + revision + ' exit=' + str(status)
                      + ' cases=' + str(EXECUTED) + ' skipped=0 evidence=' + evidence, flush=True)
            except OSError as error:
                print('multiasset-credit: evidence refusal: ' + str(error), file=sys.stderr)
                status = 1
    sys.exit(status)
