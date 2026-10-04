#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
PREFIX = 'LAYERX_HUMAN_MOVEMENT_PROVIDER_'
TEST = 'custody_profile_startup_enforces_owner_binding_and_retained_pin'
CASES = ['valid-profile', 'same-profile-restart', 'missing-path', 'absent-file',
         'wrong-network', 'wrong-policy-hash', 'wrong-module', 'wrong-asset',
         'wrong-reserve', 'wrong-light-authority', 'wrong-protocol', 'short-profile',
         'long-profile', 'group-readable', 'group-readable-directory', 'symlink-profile',
         'non-owner', 'changed-authority-retained-pin', 'changed-checkpoint-authority',
         'changed-custody-reference', 'changed-checkpoint-registry', 'restored-profile-restart']


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def private_directory(path, owner=None):
    path = Path(path)
    info = path.lstat()
    require(path.is_absolute() and path.resolve() == path and stat.S_ISDIR(info.st_mode)
            and info.st_uid == (os.geteuid() if owner is None else owner) and stat.S_IMODE(info.st_mode) == 0o700,
            'owner-only canonical fixture directory required')
    return path


def private_bytes(path, maximum=1048576, owner=None):
    path = Path(path)
    private_directory(path.parent, owner)
    info = path.lstat()
    require(path.is_absolute() and path.resolve() == path and stat.S_ISREG(info.st_mode)
            and info.st_nlink == 1 and info.st_uid == (os.geteuid() if owner is None else owner)
            and stat.S_IMODE(info.st_mode) == 0o600 and 0 < info.st_size <= maximum,
            'owner-only regular bounded fixture input required')
    return path.read_bytes()


def projection_directory(path):
    path = Path(path)
    info = path.lstat()
    require(path.is_absolute() and path.resolve() == path and stat.S_ISDIR(info.st_mode)
            and info.st_uid == 4020 and info.st_gid == 4020
            and stat.S_IMODE(info.st_mode) == 0o500,
            'canonical movement read-only projection directory required')
    return path


def projection_bytes(path, maximum=1048576):
    path = Path(path)
    projection_directory(path.parent)
    info = path.lstat()
    require(path.is_absolute() and path.resolve() == path and stat.S_ISREG(info.st_mode)
            and info.st_nlink == 1 and info.st_uid == 0 and info.st_gid == 4020
            and stat.S_IMODE(info.st_mode) == 0o440 and 0 < info.st_size <= maximum,
            'canonical root-owned bounded read-only movement projection required')
    return path.read_bytes()


def projection_value(directory, suffix):
    value = projection_bytes(directory / (PREFIX + suffix), 65536).decode('utf-8')
    require(value and not any(character in value for character in '\r\n\0'),
            'canonical read-only projected movement value required')
    return value


def unique(items):
    output = {}
    for key, value in items:
        require(key not in output, 'duplicate fixture/manifest field refused')
        output[key] = value
    return output


def document(path):
    return json.loads(private_bytes(path), object_pairs_hook=unique)


def digest(value):
    return hashlib.sha256(value).hexdigest()


def sources():
    paths = set()
    for package in ('layerx-human-movement-provider', 'layerx-paxeer-client'):
        base = ROOT / 'human/crates' / package
        paths.update(path for path in (base / 'src').rglob('*') if path.is_file())
        paths.update(path for path in (base / 'tests').rglob('*') if path.is_file())
        paths.add(base / 'Cargo.toml')
    for relative in ('human/Cargo.toml', 'human/Cargo.lock', 'platform/hosted/human/owner_custody.py',
                     'platform/hosted/human/material.py', 'docker/kernel/init.sh',
                     'docker/human-service/entrypoint.sh', 'tests/bridge/custody_credit.py',
                     'tests/bridge/comet_credit.py', 'tests/fixtures/custody/native-credit-receipt/profile',
                     'tools/qualification/paxeer-x/movement-custody-profile.py'):
        paths.add(ROOT / relative)
    require(all(path.is_file() and not path.is_symlink() for path in paths), 'complete real source inputs required')
    return {str(path.relative_to(ROOT)): digest(path.read_bytes()) for path in sorted(paths)}


def artifact(row):
    require(set(row) == {'path', 'sha256'}, 'exact supplied artifact identity required')
    path = Path(row['path'])
    info = path.lstat()
    require(path.is_absolute() and path.resolve() == path and stat.S_ISREG(info.st_mode)
            and info.st_uid == os.geteuid() and info.st_mode & 0o022 == 0
            and os.access(path, os.X_OK) and path.read_bytes()[:4] == b'\x7fELF'
            and digest(path.read_bytes()) == row['sha256'], 'genuine prebuilt executable identity required')
    return path


def env_value(directory, suffix, owner=None):
    value = private_bytes(directory / (PREFIX + suffix), 65536, owner).decode('utf-8')
    require(value and not any(character in value for character in '\r\n\0'), 'canonical projected movement value required')
    return value


def main():
    require(not sys.argv[1:], 'this gate consumes prebuilt artifacts only')
    fixture_path = os.environ.get('LAYERX_MOVEMENT_CUSTODY_FIXTURE')
    manifest_path = os.environ.get('LAYERX_MOVEMENT_CUSTODY_ARTIFACT_MANIFEST')
    if not fixture_path or not manifest_path:
        print(json.dumps({'exit_code': 78, 'status': 'prerequisite-unavailable',
                          'reason': 'protected genuine owner fixture and source-bound prebuilt artifact manifest required'}))
        return 78
    require(os.geteuid() == 0, 'isolated root fixture required for real non-owner refusal; no skipped cases')
    fixture = document(fixture_path)
    require(set(fixture) == {'version', 'isolated', 'producer_directory', 'material_root',
                            'kernel_projection_directory', 'runtime_directory', 'policy_path', 'evidence_root'}
            and fixture['version'] == 1 and fixture['isolated'] is True,
            'exact genuine producer-to-runtime isolated fixture contract required')
    manifest = document(manifest_path)
    bound = sources()
    require(set(manifest) == {'version', 'sources', 'artifacts'} and manifest['version'] == 1
            and manifest['sources'] == bound and set(manifest['artifacts']) == {'movement', 'startup'},
            'complete current source/prebuilt movement and startup manifest required')
    movement = artifact(manifest['artifacts']['movement'])
    startup = artifact(manifest['artifacts']['startup'])
    producer = private_directory(fixture['producer_directory'])
    material = private_directory(fixture['material_root'])
    projection = projection_directory(fixture['kernel_projection_directory'])
    runtime = private_directory(fixture['runtime_directory'], 4020)
    evidence_parent = private_directory(fixture['evidence_root'])
    policy = document(fixture['policy_path'])
    owner = document(producer / 'owner-custody.json')
    profile = private_bytes(producer / 'custody.profile', 223)
    require(len(profile) == 223 and profile[:5] == b'LXBC3'
            and int.from_bytes(profile[5:13], 'big') == 125
            and profile[13:33].hex() == '0000000000000000000000000000000000001013'
            and profile[33:65] == hashlib.sha256(b'LX:CUSTODY:MODULE:v1' + b'layerxcustody' + profile[13:33]).digest(),
            'actual canonical native custody producer binding required')
    profile_hash = digest(profile)
    require(owner.get('custody_profile') == 'custody.profile'
            and owner.get('custody_profile_sha256') == '0x' + profile_hash
            and owner.get('asset') == profile[97:129].hex()
            and owner.get('runtime_sha256') == profile[33:65].hex()
            and owner.get('vault', '').lower() == '0x' + profile[13:33].hex(),
            'actual owner bootstrap custody output must bind exact profile')
    require(policy['movement']['CUSTODY_PROFILE'] == '/run/human-private/movement/custody.profile'
            and policy['movement']['CUSTODY_PROFILE_SHA256'] == '0x' + profile_hash,
            'actual typed owner policy must bind private path and complete profile digest')
    config = private_directory(material / 'movement-config')
    require(projection_bytes(projection / 'custody.profile', 223) == profile,
            'actual read-only kernel projected profile bytes must match producer')
    for path, owner_uid in ((material / 'movement/custody.profile', 0), (runtime / 'custody.profile', 4020)):
        require(private_bytes(path, 223, owner_uid) == profile, 'producer/material/kernel/runtime profile bytes must match')
    require(env_value(config, 'CUSTODY_PROFILE') == policy['movement']['CUSTODY_PROFILE']
            and env_value(config, 'CUSTODY_PROFILE_SHA256') == '0x' + profile_hash
            and env_value(config, 'NETWORK_ID') == str(int.from_bytes(profile[201:205], 'big'))
            and env_value(config, 'PROTOCOL_VERSION') == '3'
            and profile[205:207] == b'\x00\x03', 'actual material environment network/profile policy mismatch')
    projected_env = projection_directory(projection / 'env')
    for suffix in ('CUSTODY_PROFILE', 'CUSTODY_PROFILE_SHA256', 'NETWORK_ID', 'PROTOCOL_VERSION',
                   'PAXEER_CHECKPOINT_AUTHORITY', 'CUSTODY_REFERENCE', 'PAXEER_CHECKPOINT_REGISTRY', 'PAXEER_RPC_URLS'):
        require(projection_value(projected_env, suffix) == env_value(config, suffix), 'actual kernel projected environment mismatch')
    ca = runtime / 'ca.der'
    private_bytes(ca, 65536, 4020)
    authority = env_value(config, 'PAXEER_CHECKPOINT_AUTHORITY')
    reference = env_value(config, 'CUSTODY_REFERENCE')
    registry = env_value(config, 'PAXEER_CHECKPOINT_REGISTRY')
    for value, width in ((authority, 64), (reference, 64), (registry, 40)):
        require(re.fullmatch('0x[0-9a-fA-F]{' + str(width) + '}', value) and int(value[2:], 16),
                'actual separate checkpoint registration authority required')
    for suffix in ('PAXEER_CHECKPOINT_AUTHORITY', 'CUSTODY_REFERENCE', 'PAXEER_CHECKPOINT_REGISTRY'):
        require(policy['movement'][suffix] == env_value(config, suffix), 'owner checkpoint policy projection mismatch')
    nonce = os.urandom(12).hex()
    evidence = evidence_parent / ('movement-custody-' + nonce)
    evidence.mkdir(mode=0o700)
    environment = {'LAYERX_MOVEMENT_CUSTODY_BINARY': str(movement),
                   'LAYERX_MOVEMENT_CUSTODY_OWNER_PROFILE': str(runtime / 'custody.profile'),
                   'LAYERX_MOVEMENT_CUSTODY_CA_DER': str(ca),
                   'LAYERX_MOVEMENT_CUSTODY_RPC_URLS': env_value(config, 'PAXEER_RPC_URLS'),
                   'LAYERX_MOVEMENT_CUSTODY_CHECKPOINT_AUTHORITY': authority,
                   'LAYERX_MOVEMENT_CUSTODY_REFERENCE': reference,
                   'LAYERX_MOVEMENT_CUSTODY_CHECKPOINT_REGISTRY': registry,
                   'LAYERX_MOVEMENT_CUSTODY_TEST_EVIDENCE': str(evidence)}
    log = evidence / 'startup.log'
    with log.open('xb') as output:
        log.chmod(0o600)
        process = subprocess.Popen([str(startup), '--nocapture', '--test-threads=1'], cwd=ROOT,
                                   env=environment, stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
        try:
            code = process.wait(timeout=600)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait()
            code = 124
    print(json.dumps({'command': [str(startup), '--nocapture', '--test-threads=1'],
                      'exit_code': code, 'log_path': str(log)}), flush=True)
    if code:
        return code
    output = private_bytes(log).decode('utf-8')
    for test in (TEST, 'executable_refuses_missing_configuration_without_exposing_environment',
                 'probe_refuses_incomplete_transport_configuration_and_an_absent_listener'):
        require('test ' + test + ' ... ok' in output, 'every retained and new actual startup case must execute')
    require('test result: ok. 3 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out' in output,
            'complete unskipped actual startup corpus required')
    report = document(evidence / 'startup-cases.json')
    require(report == {'version': 1, 'captured': CASES, 'owner': CASES, 'role_cases': ['movement-uid4020-startup', 'movement-uid4020-restart'], 'owner_profile_sha256': profile_hash},
            'complete real owner/captured refusal and recovery evidence required')
    pinned = b'LXMPA1' + profile + bytes.fromhex(authority[2:] + reference[2:] + registry[2:])
    require(len(pinned) == 313 and private_bytes(evidence / 'owner/state/custody-profile.pin', 313) == pinned,
            'actual immutable owner profile/checkpoint authority pin must survive refusal and restart')
    require(sources() == bound, 'source changed during qualification')
    artifact(manifest['artifacts']['movement'])
    artifact(manifest['artifacts']['startup'])
    require(private_bytes(producer / 'custody.profile', 223) == profile
            and private_bytes(runtime / 'custody.profile', 223, 4020) == profile,
            'actual owner custody inputs changed during qualification')
    print(json.dumps({'status': 'passed', 'exit_code': 0, 'log_path': str(log), 'evidence': str(evidence)}))
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (OSError, ValueError, KeyError, TypeError, RuntimeError) as error:
        print(json.dumps({'status': 'refused', 'exit_code': 2,
                          'reason': str(error) if isinstance(error, RuntimeError) else type(error).__name__}))
        sys.exit(2)
