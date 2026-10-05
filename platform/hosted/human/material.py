#!/usr/bin/env python3
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import stat
import sys
import tempfile
import unicodedata
from urllib.parse import urlsplit

# LayerX custody on Paxeer is the native layerxcustody module behind this precompile. Deposits,
# withdrawal claims and forced exits are all calls on it, so the four custody bindings are this
# constant address and no longer come from the Solidity deployment record.
CUSTODY_PRECOMPILE = '0x0000000000000000000000000000000000001013'
HUMAN_KMS_ENDPOINT = '127.0.0.1:9450'
HUMAN_KMS_SERVER_NAME = 'layerx-human-kms'
HUMAN_KMS_PROVIDER = 'layerx-human-kms'


def write(directory, name, value):
    path = directory / name
    with path.open('x', encoding='utf-8') as output:
        os.chmod(path, 0o600)
        output.write(str(value))


def protected_json(path):
    return parse(read_bytes(path))


BUNDLE_SCHEMA = 'layerx.human.owner-bundle.v2'
BUNDLE_REFUSED = 'Human owner bundle refused: '
EVIDENCE_INPUTS = {
    'components': 'components.json', 'agent': 'agent.json',
    'purpose_catalog': 'purpose-catalog.json', 'authority': 'authority.json',
    'principal_policy': 'principal-policy.json', 'recovery_policy': 'recovery-policy.json',
    'movement': 'movement-policy.json',
}
JOURNAL_RECORD = re.compile(r'[0-9a-f]{64}\.(admission|deployment)')


def refuse(detail):
    raise ValueError(BUNDLE_REFUSED + detail)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def entry(name, data):
    return {'name': name, 'sha256': digest(data), 'size': len(data)}


def protected_file(path, mode):
    path = Path(path)
    try:
        info = path.lstat()
    except OSError:
        refuse('missing ' + path.name)
    if (not path.is_absolute() or path.resolve() != path or info.st_uid != os.geteuid()
            or stat.S_IMODE(info.st_mode) != mode
            or not (stat.S_ISDIR(info.st_mode) if mode == 0o700 else stat.S_ISREG(info.st_mode) and info.st_nlink == 1)):
        refuse('ownership, type or mode of ' + path.name)


def journal_records(directory):
    from provision import journal_records as producer_journal
    try:
        return producer_journal(directory)
    except (ValueError, OSError):
        refuse('protected paired admission/deployment journal')


def owner_evidence(evidence):
    policy = {}
    for key, filename in EVIDENCE_INPUTS.items():
        try:
            policy[key] = protected_json(evidence / filename)
        except (ValueError, OSError):
            refuse('protected owner evidence ' + filename)
        if type(policy[key]) is not dict or not policy[key]:
            refuse('empty owner evidence ' + filename)
    authority = policy['authority']
    if (set(authority) != {'tenant', 'principal', 'core-clock-horizon'}
            or type(authority['tenant']) is not str
            or not re.fullmatch(r'[A-Za-z0-9_-]{1,128}', authority['tenant'])
            or type(authority['principal']) is not str
            or not re.fullmatch(r'did:[a-z0-9]+:[^;,\s]+', authority['principal'])
            or type(authority['core-clock-horizon']) is not int or authority['core-clock-horizon'] <= 0):
        refuse('owner authority evidence')
    principals = policy['principal_policy'].get('principals')
    if (type(principals) is not list or not principals
            or sum(1 for p in principals if type(p) is dict and p.get('tenant') == authority['tenant']
                   and p.get('principal') == authority['principal']) != 1):
        refuse('owner principal policy evidence')
    return policy


def read_bytes(path, maximum=1048576):
    from provision import protected_bytes
    try:
        return protected_bytes(path, maximum)
    except (ValueError, OSError):
        refuse('protected producer file unavailable: ' + Path(path).name)


def parse(data):
    from provision import strict_pairs
    return json.loads(data, object_pairs_hook=strict_pairs,
                      parse_constant=lambda _: refuse('nonfinite JSON'))


def write_bytes(path, data):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as output:
        output.write(data)
        output.flush()
        os.fsync(output.fileno())


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


PRODUCER_FILES = {'source-binding.json', 'owner-result.json', 'owner-registration.json', 'custody.profile', 'owner-custody.json',
                  'naming-deployment-result.json', 'treasury.json', 'sequencer.json',
                  'native-context.json', *{label + suffix for label in ('credit', 'identity', 'rotation', 'recovery')
                                           for suffix in ('.activity', '.receipt')}}
ONBOARDING_FILES = {'LAYERX_HUMAN_TENANCY_DIGEST', 'LAYERX_HUMAN_AUTH_INDEX_KEY',
                    'LAYERX_HUMAN_STREAM_CURSOR_KEY', 'recovery-policy.json', 'registry.json'}


def policy_from_inputs(inputs, records, network, chain):
    from provision import identity, recovery_policy, peer_binding, validate_native_records
    from owner_native import receipt_fields, digest as native_digest
    deployment = parse(inputs['deployment.json'])
    registry = parse(inputs['module-registry.json'])
    if (type(network) is not int or not 0 < network < 2**32 or type(chain) is not int or not 0 < chain < 2**64
            or deployment.get('network_id') != network or deployment.get('chain_id') != chain
            or registry.get('network_id') != network or registry.get('schema_version') != 2
            or not registry.get('assets') or not registry.get('modules')):
        refuse('deployment network, chain or module registry binding')
    policy = {key: parse(inputs[name]) for key, name in EVIDENCE_INPUTS.items()}
    if any(type(value) is not dict or not value for value in policy.values()):
        refuse('empty owner evidence')
    producer = {name: inputs['producer-records/' + name] for name in PRODUCER_FILES}
    binding = parse(producer['source-binding.json'])
    owner = parse(producer['owner-result.json'])
    registration = parse(producer['owner-registration.json'])
    authority = policy['authority']
    if (set(authority) != {'tenant', 'principal', 'core-clock-horizon'}
            or {key: authority[key] for key in ('tenant', 'principal')} != binding
            or policy['agent'].get('HUMAN_PEERS') != peer_binding(binding, 'owner bundle')
            or type(authority['core-clock-horizon']) is not int or authority['core-clock-horizon'] <= 0
            or registration['identity']['did'] != owner['did']
            or policy['components'].get('AGENT_ACTOR') != owner['did']
            or policy['components'].get('AGENT_AUTHORITY') != registration['authority']
            or policy['components'].get('AGENT_OWNER_ACCOUNT') != 'agent:' + owner['did'] + ':main'):
        refuse('original owner identity, authority or principal binding')
    identity(registration['identity'], 'owner bundle')
    recovery_policy(policy['recovery_policy'], 'owner bundle')
    if policy['recovery_policy'] != {'root': owner['recovery_root'], 'threshold': owner['recovery_threshold'],
                                    'delay_seconds': owner['recovery_delay_seconds']}:
        refuse('original recovery policy binding')
    principals = policy['principal_policy'].get('principals')
    if (type(principals) is not list or len(principals) != 1
            or any(principals[0].get(key) != binding[key] for key in ('tenant', 'principal'))
            or principals[0].get('account_id') != registration['owner_account']
            or principals[0].get('identities') != [registration['identity']]
            or not any(asset.get('asset') == principals[0].get('asset_id') for asset in registry['assets'])):
        refuse('actual owner principal policy binding')
    context = parse(producer['native-context.json'])
    if set(context) != {'network_id', 'sequencer_public_key'} or context['network_id'] != network:
        refuse('native producer network binding')
    key = context['sequencer_public_key']
    if type(key) is not str or not re.fullmatch('[0-9a-f]{64}', key) or int(key, 16) == 0:
        refuse('native sequencer pin')
    validate_native_records(producer, registration, network)
    references = [registration['identity']['evidence'], registration['identity']['rotation']['evidence'],
                  registration['identity']['recovery']['evidence']]
    observed = set()
    for label in ('credit', 'identity', 'rotation', 'recovery'):
        result = receipt_fields(producer[label + '.receipt'], bytes.fromhex(key), 'owner bundle receipt')
        if (result['activity_id'] != native_digest(b'activity-id', producer[label + '.activity']).hex()
                or result['module'] != (8 if label == 'credit' else 7) or result['version'] != 1):
            refuse('native signed activity and receipt binding')
        observed.add((result['activity_id'], result['receipt_digest']))
        if label == 'credit' and (result['target'] != registration['owner_account'] or result['amount'] <= 0):
            refuse('actual owner custody credit')
    if any((ref['activity_id'], ref['receipt_digest']) not in observed for ref in references):
        refuse('owner registration receipt references')
    naming = parse(producer['naming-deployment-result.json'])
    if (naming.get('state') != 'deployed' or type(naming.get('activity_id')) is not str
            or not re.fullmatch('[0-9a-f]{64}', naming['activity_id'])
            or any(naming.get('receipt_digest', '') + suffix not in records for suffix in ('.admission', '.deployment'))):
        refuse('actual naming deployment journal pair')
    modules = []
    seen = set()
    for module in registry['modules']:
        number, ordinals = module['module'], module['ordinals']
        if (type(number) is not int or not 1 <= number <= 9 or number in seen
                or type(ordinals) is not list or not ordinals
                or any(type(value) is not int or not 1 <= value < 65536 for value in ordinals)
                or len(set(ordinals)) != len(ordinals)):
            refuse('module activity registry')
        seen.add(number)
        modules.append({'module_id': number, 'activity_types': [(number << 16) | ordinal for ordinal in ordinals]})
    asset = principals[0]['asset_id']
    counterparties = [parse(producer[name])['account'] for name in ('treasury.json', 'sequencer.json')]
    if (len(set(counterparties)) != 2 or any(type(value) is not str or not re.fullmatch('[0-9a-f]{64}', value)
                                           or int(value, 16) == 0 for value in counterparties)):
        refuse('actual purpose counterparty accounts')
    catalog = policy['purpose_catalog']
    activities = sorted(activity for module in modules for activity in module['activity_types'])
    if (set(catalog) != {'version', 'presets'} or not isinstance(catalog['presets'], list)
            or not catalog['presets'] or policy['agent'].get('HUMAN_LIMIT_SCOPE_ID') != registration['owner_account']
            or policy['agent'].get('HUMAN_LIMIT_CONSUMED') != 0):
        refuse('actual purpose catalogue or initial owner limit')
    for preset in catalog['presets']:
        if (preset.get('activity_types') != activities
                or preset.get('counterparties') != [list(bytes.fromhex(value)) for value in counterparties]
                or preset.get('assets') != [list(bytes.fromhex(asset))]
                or preset.get('budget_asset') != list(bytes.fromhex(asset))):
            refuse('purpose catalogue registry, asset or counterparty binding')
    policy['registry'] = {'network_id': network, 'protocol_version': 3, 'modules': modules}
    checkpoint = deployment['addresses']['checkpoint_registry']
    if type(checkpoint) is not str or not re.fullmatch('0x[0-9a-fA-F]{40}', checkpoint) or int(checkpoint, 16) == 0:
        refuse('deployed checkpoint registry')
    policy['components'].update(PAXEER_EXIT_CONTRACT=CUSTODY_PRECOMPILE,
                                PAXEER_WITHDRAWAL_CLAIMS_CONTRACT=CUSTODY_PRECOMPILE)
    policy['movement'].update(PAXEER_VAULT=CUSTODY_PRECOMPILE, PAXEER_CHECKPOINT_REGISTRY=checkpoint,
                             PAXEER_CLAIMS_CONTRACT=CUSTODY_PRECOMPILE, PAXEER_EXIT_CONTRACT=CUSTODY_PRECOMPILE)
    profile = producer['custody.profile']
    custody = parse(producer['owner-custody.json'])
    if (len(profile) != 223 or policy['movement'].get('CUSTODY_PROFILE') != '/run/human-private/movement/custody.profile'
            or policy['movement'].get('CUSTODY_PROFILE_SHA256') != '0x' + digest(profile)
            or custody.get('custody_profile') != 'custody.profile'
            or custody.get('custody_profile_sha256') != '0x' + digest(profile)
            or custody.get('vault') != CUSTODY_PRECOMPILE):
        refuse('authenticated custody profile binding')
    policy['journal_directory'] = 'journal'
    if 'onboarding-configuration.json' in inputs:
        onboarding = parse(inputs['onboarding-configuration.json'])
        if (set(onboarding) != {'directory', 'sponsor_principal', 'initial_funding'}
                or onboarding['sponsor_principal'] != owner['principal']
                or type(onboarding['initial_funding']) is not int or not 0 < onboarding['initial_funding'] < 2**128):
            refuse('onboarding sponsor binding')
        policy['onboarding_configuration'] = dict(onboarding, directory='onboarding')
    return policy


def assemble_policy(evidence, deployment, registry_path, output, network, chain):
    evidence, output = Path(evidence), Path(output)
    directory = output.parent
    protected_file(directory, 0o700)
    if output.name != 'policy.json':
        refuse('canonical policy filename')
    inputs = {name: read_bytes(evidence / name) for name in EVIDENCE_INPUTS.values()}
    inputs.update({'deployment.json': read_bytes(deployment), 'module-registry.json': read_bytes(registry_path)})
    for name in PRODUCER_FILES:
        inputs['producer-records/' + name] = read_bytes(evidence / 'producer-records' / name)
    onboarding = {}
    source = evidence / 'onboarding-configuration.json'
    if source.exists() or source.is_symlink():
        inputs[source.name] = read_bytes(source)
        declared = parse(inputs[source.name])
        config = Path(declared['directory'])
        protected_file(config, 0o700)
        for name in ONBOARDING_FILES - {'recovery-policy.json', 'registry.json'}:
            onboarding[name] = read_bytes(config / name, 128)
        retained = Path(registry_path).parent / 'human'
        onboarding['recovery-policy.json'] = read_bytes(retained / 'identity/recovery-policy.json')
        onboarding['registry.json'] = read_bytes(retained / 'kms/registry.json')
    records = journal_records(evidence / 'journal')
    policy = policy_from_inputs(inputs, records, network, chain)
    if onboarding and (parse(onboarding['registry.json']) != policy['registry']
                       or parse(onboarding['recovery-policy.json']) != policy['recovery_policy']):
        refuse('retained onboarding registry or recovery changed')
    policy_bytes = json.dumps(policy, sort_keys=True, separators=(',', ':'), allow_nan=False).encode()
    manifest = {'schema': BUNDLE_SCHEMA, 'network_id': network, 'chain_id': chain,
                'authority_sha256': digest(json.dumps(policy['authority'], sort_keys=True, separators=(',', ':')).encode()),
                'policy_sha256': digest(policy_bytes),
                'inputs': [entry(name, data) for name, data in sorted(inputs.items())],
                'onboarding': [entry(name, data) for name, data in sorted(onboarding.items())],
                'journal': [entry(name, data) for name, data in sorted(records.items())]}
    files = {'policy.json': policy_bytes,
             'bundle-manifest.json': json.dumps(manifest, sort_keys=True, separators=(',', ':')).encode()}
    files.update({'inputs/' + name: data for name, data in inputs.items()})
    files.update({'journal/' + name: data for name, data in records.items()})
    files.update({'onboarding/' + name: data for name, data in onboarding.items()})
    lock = directory.parent / ('.' + directory.name + '-publish')
    try:
        lock.mkdir(mode=0o700)
    except FileExistsError:
        refuse('reconciliation required: bundle publication interrupted or active')
    try:
        if any(directory.iterdir()):
            try:
                verify_bundle(directory, network, chain)
            except (ValueError, OSError, KeyError, TypeError):
                refuse('reconciliation required: retained or interrupted bundle invalid')
            actual = {str(path.relative_to(directory)): read_bytes(path) for path in directory.rglob('*') if path.is_file()}
            if actual != files:
                refuse('reconciliation required: retained bundle contents differ')
            return
        for name, data in sorted(files.items(), key=lambda item: item[0] == 'bundle-manifest.json'):
            path = directory / name
            path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            write_bytes(path, data)
        for path in sorted((p for p in directory.rglob('*') if p.is_dir()), key=lambda p: len(p.parts), reverse=True):
            sync_directory(path)
        sync_directory(directory)
        sync_directory(directory.parent)
    finally:
        lock.rmdir()


def verify_bundle(directory, network, chain):
    directory = Path(directory)
    protected_file(directory, 0o700)
    manifest = parse(read_bytes(directory / 'bundle-manifest.json'))
    if (type(manifest) is not dict or set(manifest) != {'schema', 'network_id', 'chain_id', 'authority_sha256',
            'policy_sha256', 'inputs', 'journal', 'onboarding'} or manifest['schema'] != BUNDLE_SCHEMA
            or manifest['network_id'] != network or manifest['chain_id'] != chain):
        refuse('bundle schema, network or chain mismatch')
    allowed = {'policy.json', 'bundle-manifest.json'}
    loaded = {}
    for section in ('inputs', 'journal', 'onboarding'):
        entries = manifest[section]
        if type(entries) is not list or len(entries) > 128:
            refuse('bundle manifest entries')
        names = set()
        data = {}
        for item in entries:
            if (type(item) is not dict or set(item) != {'name', 'sha256', 'size'} or type(item['name']) is not str
                    or not re.fullmatch(r'(?:producer-records/)?[A-Za-z0-9_.-]+', item['name'])
                    or item['name'] in ('.', '..') or item['name'] in names
                    or type(item['size']) is not int or not 0 < item['size'] <= 1048576
                    or type(item['sha256']) is not str or not re.fullmatch('[0-9a-f]{64}', item['sha256'])):
                refuse('bundle manifest entry bounds or path')
            names.add(item['name'])
            name = section + '/' + item['name']
            data[item['name']] = read_bytes(directory / name)
            if entry(item['name'], data[item['name']]) != item:
                refuse('producer output bytes differ: ' + item['name'])
            allowed.add(name)
        loaded[section] = data
    expected_inputs = set(EVIDENCE_INPUTS.values()) | {'deployment.json', 'module-registry.json'} | {'producer-records/' + name for name in PRODUCER_FILES}
    if 'onboarding-configuration.json' in loaded['inputs']:
        expected_inputs.add('onboarding-configuration.json')
        if set(loaded['onboarding']) != ONBOARDING_FILES:
            refuse('retained onboarding outputs missing')
    elif loaded['onboarding']:
        refuse('undeclared onboarding outputs')
    if set(loaded['inputs']) != expected_inputs:
        refuse('producer input inventory differs')
    actual = set()
    expected_dirs = {str(Path(name).parent) for name in allowed} - {'.'}
    expected_dirs.add('inputs')
    for path in directory.rglob('*'):
        name = str(path.relative_to(directory))
        if path.is_dir() and not path.is_symlink():
            protected_file(path, 0o700)
            if name not in expected_dirs:
                refuse('undeclared bundle directory')
        else:
            protected_file(path, 0o600)
            actual.add(name)
    if actual != allowed:
        refuse('bundle file inventory differs')
    records = journal_records(directory / 'journal')
    if records != loaded['journal']:
        refuse('journal inventory differs')
    policy_bytes = read_bytes(directory / 'policy.json')
    policy = parse(policy_bytes)
    expected = policy_from_inputs(loaded['inputs'], records, network, chain)
    if policy != expected or digest(policy_bytes) != manifest['policy_sha256']:
        refuse('policy differs from declared actual producer outputs')
    authority = digest(json.dumps(policy['authority'], sort_keys=True, separators=(',', ':')).encode())
    if authority != manifest['authority_sha256']:
        refuse('authority digest differs')
    if loaded['onboarding']:
        if (parse(loaded['onboarding']['registry.json']) != policy['registry']
                or parse(loaded['onboarding']['recovery-policy.json']) != policy['recovery_policy']):
            refuse('retained onboarding binding differs')
        for name in ONBOARDING_FILES - {'registry.json', 'recovery-policy.json'}:
            value = loaded['onboarding'][name].decode()
            if not re.fullmatch('[A-Za-z0-9_-]{43}', value) or len(base64.urlsafe_b64decode(value + '=')) != 32:
                refuse('retained onboarding key encoding')
    return {'network_id': network, 'chain_id': chain, 'authority_sha256': authority,
            'policy_sha256': manifest['policy_sha256'], 'bundle_sha256': digest(read_bytes(directory / 'bundle-manifest.json')),
            'records': len(records)}


def relocate_bundle(source, destination, network, chain):
    source, destination = Path(source), Path(destination)
    before = verify_bundle(source, network, chain)
    if destination.exists() or destination.is_symlink():
        refuse('reconciliation required: relocation destination already exists')
    protected_file(destination.parent, 0o700)
    pending = Path(tempfile.mkdtemp(prefix='.owner-relocate-', dir=destination.parent))
    try:
        for path in sorted(source.rglob('*')):
            target = pending / path.relative_to(source)
            if path.is_dir() and not path.is_symlink():
                protected_file(path, 0o700)
                target.mkdir(mode=0o700)
            else:
                write_bytes(target, read_bytes(path))
        if verify_bundle(source, network, chain) != before or verify_bundle(pending, network, chain) != before:
            refuse('bundle changed during relocation')
        for path in sorted((p for p in pending.rglob('*') if p.is_dir()), key=lambda p: len(p.parts), reverse=True):
            sync_directory(path)
        sync_directory(pending)
        os.rename(pending, destination)
        pending = None
        sync_directory(destination.parent)
        return before
    finally:
        if pending is not None:
            shutil.rmtree(pending)


def material_inventory(root):
    root = Path(root)
    protected_file(root, 0o700)
    entries = []
    for path in sorted(root.rglob('*')):
        if path == root / 'material-manifest.json':
            continue
        if path.is_dir() and not path.is_symlink():
            protected_file(path, 0o700)
        else:
            name = str(path.relative_to(root))
            entries.append(entry(name, read_bytes(path)))
    if not entries or len(entries) > 512:
        refuse('retained material output count')
    return {'schema': 'layerx.human.retained-material.v1', 'files': entries}


def seal_material(root):
    root = Path(root)
    value = material_inventory(root)
    write_bytes(root / 'material-manifest.json', json.dumps(value, sort_keys=True, separators=(',', ':')).encode())
    for path in sorted((p for p in root.rglob('*') if p.is_dir()), key=lambda p: len(p.parts), reverse=True):
        sync_directory(path)
    sync_directory(root)


def verify_material(root):
    root = Path(root)
    if parse(read_bytes(root / 'material-manifest.json')) != material_inventory(root):
        refuse('reconciliation required: retained material output changed')


def genesis_binding(directory):
    directory = Path(directory)
    protected_file(directory, 0o700)
    result = []
    for name in ('metadata.lxgb', 'asset-id', 'replica-id'):
        path = directory / name
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, 'rb') as source:
            info = os.fstat(source.fileno())
            if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or info.st_nlink != 1
                    or info.st_mode & 0o022 or not 0 < info.st_size <= 1048576):
                refuse('protected genesis binding output')
            data = source.read(1048577)
            after = os.fstat(source.fileno())
            if (len(data) != info.st_size or info.st_mtime_ns != after.st_mtime_ns
                    or info.st_ctime_ns != after.st_ctime_ns):
                refuse('genesis changed during binding')
        result.append(entry(name, data))
    return {'schema': 'layerx.human.genesis-binding.v1', 'files': result}


def secret_arguments(directory, network, chain):
    directory = Path(directory)
    verify_bundle(directory, network, chain)
    names = set()
    total = sum(path.stat().st_size for path in directory.rglob('*') if path.is_file())
    if total > 1048576:
        refuse('bundle exceeds Kubernetes Secret payload bound')
    for path in sorted(directory.rglob('*')):
        if not path.is_file():
            continue
        name = str(path.relative_to(directory)).replace('/', '.')
        if name in names or len(name) > 253 or not re.fullmatch('[A-Za-z0-9_.-]+', name):
            refuse('secret projection key collision')
        names.add(name)
        sys.stdout.buffer.write(('--from-file=' + name + '=' + str(path)).encode() + b'\0')


def projected_files(directory, selected=None):
    directory = Path(directory)
    if not directory.is_absolute() or directory.resolve() != directory:
        refuse('noncanonical Secret projection root')
    info = directory.lstat()
    readonly = bool(os.statvfs(directory).f_flag & os.ST_RDONLY)
    if (not stat.S_ISDIR(info.st_mode) or info.st_uid not in (0, os.geteuid())
            or (info.st_mode & 0o022 and not readonly)):
        refuse('Secret projection root permissions')
    marker = directory / '..data'
    generation = marker.resolve(strict=True) if marker.is_symlink() else directory
    if generation != directory and (generation.parent != directory or generation.resolve() != generation):
        refuse('Secret projection generation outside mount')
    info = generation.lstat()
    if (not stat.S_ISDIR(info.st_mode) or info.st_uid not in (0, os.geteuid())
            or (info.st_mode & 0o022 and not readonly)):
        refuse('Secret projection generation permissions')
    names = {path.name for path in generation.iterdir()}
    if selected is not None:
        if not set(selected) <= names:
            refuse('required consumer projection absent')
        names = set(selected)
    if not names or len(names) > 256 or any(not re.fullmatch('[A-Za-z0-9_.-]+', name) or name.startswith('.') for name in names):
        refuse('Secret projection key inventory')
    result = {}
    total = 0
    for name in sorted(names):
        if generation != directory and (directory / name).resolve(strict=True) != generation / name:
            refuse('Secret projection key outside pinned generation')
        fd = os.open(generation / name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, 'rb') as source:
            info = os.fstat(source.fileno())
            if (not stat.S_ISREG(info.st_mode) or info.st_uid not in (0, os.geteuid()) or info.st_nlink != 1
                    or info.st_mode & 0o022 or not 0 < info.st_size <= 1048576):
                refuse('Secret projection file bounds or permissions')
            data = source.read(1048577)
            after = os.fstat(source.fileno())
            if (len(data) != info.st_size or info.st_mtime_ns != after.st_mtime_ns
                    or info.st_ctime_ns != after.st_ctime_ns):
                refuse('Secret projection changed during copy')
        total += len(data)
        if total > 1048576:
            refuse('Secret projection exceeds payload bound')
        result[name] = data
    if generation != directory and marker.resolve(strict=True) != generation:
        refuse('Secret projection generation changed')
    return result


def import_secret(directory, destination, network, chain):
    files = projected_files(directory)
    if 'bundle-manifest.json' not in files or 'policy.json' not in files:
        refuse('Secret bundle manifest and policy required')
    manifest = parse(files['bundle-manifest.json'])
    if manifest.get('schema') != BUNDLE_SCHEMA:
        refuse('Secret bundle schema')
    paths = {'policy.json': 'policy.json', 'bundle-manifest.json': 'bundle-manifest.json'}
    for section in ('inputs', 'journal', 'onboarding'):
        entries = manifest[section]
        if type(entries) is not list or len(entries) > 128:
            refuse('Secret manifest bounds')
        for item in entries:
            name = item['name']
            if type(name) is not str or not re.fullmatch(r'(?:producer-records/)?[A-Za-z0-9_-][A-Za-z0-9_.-]*', name):
                refuse('Secret relative entry name')
            relative = section + '/' + name
            key = relative.replace('/', '.')
            if key in paths:
                refuse('Secret projection collision')
            paths[key] = relative
    if set(files) != set(paths):
        refuse('Secret projection differs from exact manifest')
    destination = Path(destination)
    protected_file(destination.parent, 0o700)
    pending = Path(tempfile.mkdtemp(prefix='.secret-owner-', dir=destination.parent))
    try:
        for key, relative in paths.items():
            path = pending / relative
            path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            write_bytes(path, files[key])
        binding = verify_bundle(pending, network, chain)
        if projected_files(directory) != files:
            refuse('Secret projection changed before publication')
        if destination.exists() or destination.is_symlink():
            if verify_bundle(destination, network, chain) != binding:
                refuse('reconciliation required: mounted owner bundle changed')
            return binding
        for path in sorted((p for p in pending.rglob('*') if p.is_dir()), key=lambda p: len(p.parts), reverse=True):
            sync_directory(path)
        sync_directory(pending)
        os.rename(pending, destination)
        pending = None
        sync_directory(destination.parent)
        return binding
    finally:
        if pending is not None:
            shutil.rmtree(pending)


def verify_projected_material(bundle, projections, network, chain):
    bundle, projections = Path(bundle), Path(projections)
    verify_bundle(bundle, network, chain)
    policy = parse(read_bytes(bundle / 'policy.json'))
    if parse(projected_files(projections / 'module-registry', {'registry.json'})['registry.json']) != parse(read_bytes(bundle / 'inputs/module-registry.json')):
        refuse('runtime canonical module registry differs from actual producer input')
    json_fields = [('components', 'purpose-catalog.json', 'purpose_catalog'), ('kms', 'registry.json', 'registry'),
                   ('identity', 'recovery-policy.json', 'recovery_policy'), ('authority', 'principal-policy.json', 'principal_policy')]
    for folder, name, key in json_fields:
        if parse(projected_files(projections / folder, {name})[name]) != policy[key]:
            refuse('runtime consumer JSON differs from verified owner bundle')
    value_fields = [('components-config', 'LAYERX_HUMAN_', policy['components']),
                    ('agent-config', 'LAYERX_AGENT_', policy['agent']),
                    ('authority-config', '', policy['authority']),
                    ('movement-config', 'LAYERX_HUMAN_MOVEMENT_PROVIDER_', policy['movement'])]
    for folder, prefix, values in value_fields:
        expected = {prefix + key: str(value).encode() for key, value in values.items()}
        if projected_files(projections / folder, expected) != expected:
            refuse('runtime consumer values differ from verified owner bundle')
    expected = {'LAYERX_HUMAN_NETWORK_ID': str(network).encode(), 'LAYERX_HUMAN_PAXEER_CHAIN_ID': str(chain).encode()}
    if 'onboarding_configuration' in policy:
        for name in ONBOARDING_FILES - {'registry.json', 'recovery-policy.json'}:
            expected[name] = read_bytes(bundle / 'onboarding' / name)
    if projected_files(projections / 'components-config', expected) != expected:
        refuse('runtime consumer network or original onboarding keys differ')
    if projected_files(projections / 'journal') != journal_records(bundle / 'journal'):
        refuse('runtime consumer journal differs from verified owner bundle')


def passkey_relying_party(web_origin):
    if not web_origin:
        return 'paxportwallet.com', 'https://paxportwallet.com'
    parts = urlsplit(web_origin)
    host = parts.hostname or ''
    if (parts.scheme != 'https' or parts.port is not None or parts.path or parts.query
            or parts.fragment or parts.username or parts.password
            or web_origin != 'https://' + host
            or not re.fullmatch(r'[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*', host)
            or re.fullmatch(r'[0-9.]+', host)):
        raise ValueError('Human web origin refused')
    return host, web_origin


def component_defaults(network, chain, web_origin=''):
    rp_id, origin = passkey_relying_party(web_origin)
    return {
        'RP_ID': rp_id, 'RP_NAME': 'LayerX Human',
        'ORIGIN': origin,
        'CEREMONY_TTL_SECONDS': 300, 'ASSERTION_TTL_SECONDS': 60,
        'SESSION_TTL_SECONDS': 3600, 'REFRESH_TTL_SECONDS': 86400,
        'STEP_UP_TTL_SECONDS': 300, 'AUTH_RATE_ATTEMPTS': 5,
        'AUTH_RATE_WINDOW_SECONDS': 60, 'RETENTION_JOURNEYS_SECONDS': 2592000,
        'RETENTION_NOTIFICATIONS_SECONDS': 604800, 'RETENTION_AUDIT_SECONDS': 7776000,
        'RETENTION_TELEMETRY_SECONDS': 604800, 'RETENTION_CACHE_SECONDS': 300,
        'CAPABILITY_TTL_SECONDS': 30, 'AGENT_SOCKET': '/run/layerx/human/owner/agent.sock',
        'KMS_PROVIDER_REFERENCE': HUMAN_KMS_PROVIDER, 'KMS_ENDPOINT': HUMAN_KMS_ENDPOINT,
        'KMS_SERVER_NAME': HUMAN_KMS_SERVER_NAME,
        'KMS_ROOT_CERTIFICATE_DER': '/run/human-private/components/ca.der',
        'KMS_CLIENT_CERTIFICATE_DER': '/run/human-private/components/kms-client.der',
        'KMS_CLIENT_PRIVATE_KEY_DER': '/run/human-private/components/kms-client-key.der',
        'NETWORK_ID': network, 'PROTOCOL_VERSION': 3, 'SIGNING_RATE_MAXIMUM': 60,
        'SIGNING_RATE_WINDOW_SECONDS': 60, 'AGENT_TIMESTAMP_SPAN_SECONDS': 60,
        'AGENT_FEE_LIMIT': 1000, 'BINDING_STATEMENT_TTL_SECONDS': 300,
        'AGENT_PURPOSE_CATALOG': '/run/human-private/components/purpose-catalog.json',
        'PAXEER_RPC_URL': 'https://paxeer-boundary.layerx-testnet.svc.cluster.local:9443',
        'PAXEER_RPC_TIMEOUT_SECONDS': 10,
        'PAXEER_TRUST_ANCHOR_DER': '/run/human-private/components/ca.der',
        'PAXEER_CHAIN_ID': chain, 'EXIT_REQUIRED_CONFIRMATIONS': 12,
        'ACTIVITY_FRESHNESS_SECONDS': 60, 'ACTIVITY_EXPORT_MAXIMUM_BYTES': 1048576,
        'EXIT_POLL_CADENCE_SECONDS': 5, 'EXIT_DELAYED_AFTER_POLLS': 12,
        'CONTINUATION_UNKNOWN_DEADLINE_SECONDS': 300,
    }

def movement_defaults(network, chain):
    return {
        'MODE': 'movement', 'ALLOWED_GID': 4020, 'MAX_FRAME_BYTES': 1048576,
        'DEADLINE_SECONDS': 5,
        'EVIDENCE_ROOT': '/var/lib/layerx/human/evidence',
        'PAXEER_RPC_URLS': json.dumps([
            'https://paxeer-boundary.layerx-testnet.svc.cluster.local:9443',
            'https://paxeer-observer-boundary.layerx-testnet.svc.cluster.local:9443']),
        'PAXEER_CA_DER': '/run/human-private/movement/ca.der',
        'PAXEER_CHAIN_ID': chain, 'PAXEER_MINIMUM_AGREEMENT': 2,
        'NETWORK_ID': network, 'PROTOCOL_VERSION': 3,
        'POLL_SECONDS': 5, 'DELAYED_AFTER_POLLS': 12,
        'KMS_ENDPOINT': HUMAN_KMS_ENDPOINT, 'KMS_SERVER_NAME': HUMAN_KMS_SERVER_NAME,
        'KMS_PROVIDER_REFERENCE': HUMAN_KMS_PROVIDER,
        'KMS_CA_DER': '/run/human-private/movement/ca.der',
        'KMS_CLIENT_CERT_DER': '/run/human-private/movement/kms-executor.der',
        'KMS_CLIENT_KEY_DER': '/run/human-private/movement/kms-executor-key.der',
    }


def main():
    root = Path(sys.argv[1])
    network, chain = int(sys.argv[2]), int(sys.argv[3])
    config = component_defaults(network, chain, sys.argv[5] if len(sys.argv) > 5 else '')
    policy = protected_json(sys.argv[4]) if sys.argv[4] else None
    if policy is not None:
        if Path(policy.get('journal_directory', '')).is_absolute():
            refuse('producer-local journal path')
        verify_bundle(Path(sys.argv[4]).parent, network, chain)
    onboarding = policy.get('onboarding_configuration') if policy else None
    if onboarding is not None:
        if onboarding['directory'] != 'onboarding':
            refuse('producer-local onboarding path')
        directory = Path(sys.argv[4]).parent / 'onboarding'
        for source, destination in [('registry.json', root / 'kms/registry.json'), ('recovery-policy.json', root / 'identity/recovery-policy.json')]:
            data = read_bytes(directory / source)
            if destination.exists() or destination.is_symlink():
                if read_bytes(destination) != data:
                    refuse('reconciliation required: retained onboarding output differs')
            else:
                write_bytes(destination, data)
    for name in ('TENANCY_DIGEST', 'AUTH_INDEX_KEY', 'STREAM_CURSOR_KEY'):
        if onboarding is None:
            config[name] = base64.urlsafe_b64encode(secrets.token_bytes(32)).decode().rstrip('=')
        else:
            from provision import protected_bytes
            source = directory / ('LAYERX_HUMAN_' + name)
            value = protected_bytes(source, 128).decode()
            if not re.fullmatch('[A-Za-z0-9_-]{43}', value) or len(base64.urlsafe_b64decode(value + '=')) != 32:
                raise ValueError('onboarding configuration binding refused')
            config[name] = value
    for prefix in ('AGENT', 'KMS'):
        for key, value in {'MAX_FRAME_BYTES': 1048576, 'MAX_CONNECTIONS': 4,
                           'MAX_STREAMS': 4, 'MAX_QUEUED_BYTES': 4194304,
                           'DEADLINE_SECONDS': 10}.items():
            config[f'{prefix}_{key}'] = value
    for provider, uid in [('IDENTITY', 4020), ('SECURITY', 4020), ('MOVEMENT', 4020)]:
        config[f'{provider}_SOCKET'] = f'/run/layerx/human/{provider.lower()}.sock'
        config[f'{provider}_MAX_FRAME_BYTES'] = 1048576
        config[f'{provider}_DEADLINE_SECONDS'] = 10
        if provider != 'SECURITY':
            config[f'{provider}_PEER_UID'] = uid
            config[f'{provider}_PEER_GID'] = 4020
    agent = {
        'MODE': 'human-owner',
        'HUMAN_AUTHORITY_CA_DER': '/run/human-private/agent/ca.der',
        'AUTHORITY_CA_DER': '/run/human-private/agent/ca.der',
        'HUMAN_NODE_LNI': '/run/layerx/node/layerxd.lni.sock',
        'HUMAN_STORE': '/var/lib/layerx/human/agent/store',
        'HUMAN_SOCKET': '/run/layerx/human/owner/agent.sock',
        'HUMAN_SESSION_KEY_ROOT': '/var/lib/layerx/human/agent/sessions',
        'HUMAN_SESSION_OPERATOR_SECRET_FILE': '/run/human-private/agent/session-operator',
        'HUMAN_SOCKET_UID': 4021, 'HUMAN_SOCKET_GID': 4020, 'HUMAN_SOCKET_MODE': '0660',
        'HUMAN_NETWORK_ID': network, 'HUMAN_PROTOCOL_VERSION': 3,
        'HUMAN_DEADLINE_MS': 10000, 'HUMAN_MAX_FRAME_BYTES': 1048576,
        'HUMAN_MAX_CONNECTIONS': 4, 'HUMAN_MAX_STREAMS': 4,
        'HUMAN_MAX_QUEUED_BYTES': 4194304, 'HUMAN_MAX_PAYLOAD_BYTES': 1048576,
        'HUMAN_TIMESTAMP_SPAN': 60, 'HUMAN_RECONNECT_ATTEMPTS': 5,
        'HUMAN_RECONNECT_BASE_MS': 100, 'HUMAN_RECONNECT_MAX_MS': 2000,
        'HUMAN_RECONNECT_JITTER_PERCENT': 10,
        'HUMAN_AUTHORITY_ENDPOINT': 'https://layerx-receipt-authority.layerx-testnet.svc.cluster.local:9443',
        'HUMAN_AUTHORITY_MAX_BYTES': 1048576,
        'PROGRAM_LISTEN': '127.0.0.1:9453', 'PROGRAM_MAX_STALENESS_MS': 60000,
        'NODE_ENDPOINT': 'http://127.0.0.1:9401',
        'AUTHORITY_ENDPOINT': 'https://layerx-receipt-authority.layerx-testnet.svc.cluster.local:9443',
        'AUTHORITY_REPLICA_ID': (root.parent / 'receipt-authority-replica-id').read_text().strip(),
        'SEQUENCER_TRUST_HISTORY': '/run/human-private/agent/trust-history',
        'DEPLOYMENT_JOURNAL': '/run/human-private/agent/journal',
    }
    journal = root / 'journal'
    journal.mkdir(mode=0o700)
    if sys.argv[4]:
        required = {'components', 'agent', 'purpose_catalog', 'registry', 'journal_directory',
                    'authority', 'principal_policy', 'recovery_policy', 'movement'}
        if onboarding is not None:
            required.add('onboarding_configuration')
        if set(policy) != required:
            raise ValueError('Human policy fields do not match the documented contract')
        component_keys = {'AGENT_ACTOR', 'AGENT_AUTHORITY', 'AGENT_OWNER_ACCOUNT',
                          'AGENT_RECOVERY_ROOT', 'AGENT_RECOVERY_THRESHOLD',
                          'PAXEER_EXIT_CONTRACT', 'PAXEER_WITHDRAWAL_CLAIMS_CONTRACT'}
        if onboarding is not None:
            component_keys.update(('ONBOARDING_SPONSOR_PRINCIPAL', 'ONBOARDING_INITIAL_FUNDING'))
        agent_keys = {'HUMAN_PEERS', 'HUMAN_LIMIT_SCOPE', 'HUMAN_LIMIT_SCOPE_ID',
                      'HUMAN_LIMIT_ID', 'HUMAN_LIMIT_NAME', 'HUMAN_LIMIT_CEILING',
                      'HUMAN_LIMIT_CONSUMED'}
        for values, expected in [(policy['components'], component_keys), (policy['agent'], agent_keys)]:
            if set(values) != expected or any(not str(v) or '\n' in str(v) or '\0' in str(v) for v in values.values()):
                raise ValueError('Human policy binding fields refused')
        for name in ('PAXEER_EXIT_CONTRACT', 'PAXEER_WITHDRAWAL_CLAIMS_CONTRACT'):
            address = policy['components'][name]
            if (not re.fullmatch(r'0x[0-9a-fA-F]{40}', address)
                    or address.lower() != CUSTODY_PRECOMPILE):
                raise ValueError('Human custody contract binding refused')
        if policy['registry']['network_id'] != network or policy['registry']['protocol_version'] != 3:
            raise ValueError('Human registry network or protocol mismatch')
        peers = policy['agent']['HUMAN_PEERS']
        if not isinstance(peers, str):
            raise ValueError('Human peer policy fields refused')
        peer = re.fullmatch(r'uid=(4020);tenant=([A-Za-z0-9_-]{1,128});principal=(did:[a-z0-9]+:[^;,]+)', peers)
        if peer is None:
            raise ValueError('Human peer policy must authorize only component UID 4020')
        tenant, principal = peer.group(2, 3)
        if (len(principal.encode('utf-8')) > 255
                or any(c.isspace() or unicodedata.category(c) == 'Cc' for c in principal)):
            raise ValueError('Human peer principal refused')
        authority = policy['authority']
        if set(authority) != {'tenant', 'principal', 'core-clock-horizon'}:
            raise ValueError('Human authority fields refused')
        if int(authority['core-clock-horizon']) <= 0:
            raise ValueError('Human core clock horizon refused')
        if tenant != authority['tenant'] or principal != authority['principal']:
            raise ValueError('Human authority peer binding differs')
        principals = policy['principal_policy']['principals']
        bound = [p for p in principals if p['tenant'] == authority['tenant']
                 and p['principal'] == authority['principal']]
        if len(bound) != 1:
            raise ValueError('Human authority principal binding missing')
        recovery = policy['recovery_policy']
        if (set(recovery) != {'root', 'threshold', 'delay_seconds'}
                or len(recovery['root']) != 32
                or any(type(b) is not int or not 0 <= b <= 255 for b in recovery['root'])
                or not any(recovery['root'])
                or not 1 <= recovery['threshold'] <= 65535 or recovery['delay_seconds'] <= 0
                or policy['components']['AGENT_RECOVERY_ROOT'] != base64.urlsafe_b64encode(
                    bytes(recovery['root'])).decode().rstrip('=')
                or int(policy['components']['AGENT_RECOVERY_THRESHOLD']) != recovery['threshold']):
            raise ValueError('Human recovery policy binding differs')
        movement = movement_defaults(network, chain)
        movement_keys = {'PAXEER_VAULT', 'PAXEER_CHECKPOINT_REGISTRY',
                         'PAXEER_CLAIMS_CONTRACT', 'PAXEER_EXIT_CONTRACT',
                         'CUSTODY_PROFILE', 'CUSTODY_PROFILE_SHA256',
                         'PAXEER_CHECKPOINT_AUTHORITY', 'CUSTODY_REFERENCE',
                         'PAXEER_CONFIRMATIONS', 'CHECKPOINT_INTERVAL_SECONDS',
                         'PAXEER_BLOCK_SECONDS', 'REMINDER_INTERVAL_SECONDS'}
        if set(policy['movement']) != movement_keys:
            raise ValueError('Human movement policy fields refused')
        for key, value in policy['movement'].items():
            if key == 'CUSTODY_PROFILE':
                if value != '/run/human-private/movement/custody.profile':
                    raise ValueError('Human custody profile path refused')
                continue
            width = 40 if key in {'PAXEER_VAULT', 'PAXEER_CHECKPOINT_REGISTRY',
                                 'PAXEER_CLAIMS_CONTRACT', 'PAXEER_EXIT_CONTRACT'} else 64
            if key in {'PAXEER_CONFIRMATIONS', 'CHECKPOINT_INTERVAL_SECONDS',
                       'PAXEER_BLOCK_SECONDS', 'REMINDER_INTERVAL_SECONDS'}:
                if type(value) is not int or value <= 0:
                    raise ValueError('Human movement timing refused')
            elif not re.fullmatch(r'0x[0-9a-fA-F]{' + str(width) + '}', value) or int(value, 16) == 0:
                raise ValueError('Human movement binding refused')
        for name in ('PAXEER_VAULT', 'PAXEER_CLAIMS_CONTRACT', 'PAXEER_EXIT_CONTRACT'):
            if policy['movement'][name].lower() != CUSTODY_PRECOMPILE:
                raise ValueError('Human movement custody binding must name the custody precompile')
        if (policy['movement']['PAXEER_CLAIMS_CONTRACT'] != policy['components']['PAXEER_WITHDRAWAL_CLAIMS_CONTRACT']
                or policy['movement']['PAXEER_EXIT_CONTRACT'] != policy['components']['PAXEER_EXIT_CONTRACT']):
            raise ValueError('Human movement custody bindings differ')
        movement.update(policy['movement'])
        profile = read_bytes(Path(sys.argv[4]).parent / 'inputs/producer-records/custody.profile')
        if len(profile) != 223 or movement['CUSTODY_PROFILE_SHA256'] != '0x' + digest(profile):
            refuse('retained authenticated custody profile differs')
        (root / 'movement').mkdir(mode=0o700, exist_ok=True)
        write_bytes(root / 'movement/custody.profile', profile)
        for key, value in movement.items():
            write(root / 'movement-config', 'LAYERX_HUMAN_MOVEMENT_PROVIDER_' + key, value)
        for key, value in authority.items():
            if not str(value) or any(c in str(value) for c in '\r\n\0'):
                raise ValueError('Human authority value refused')
            write(root / 'authority-config', key, value)
        write(root / 'authority', 'principal-policy.json', json.dumps(policy['principal_policy']))
        if onboarding is None:
            write(root / 'identity', 'recovery-policy.json', json.dumps(recovery))
        elif protected_json(root / 'identity/recovery-policy.json') != recovery:
            raise ValueError('retained KMS recovery policy changed')
        if onboarding is not None:
            if (set(onboarding) != {'directory', 'sponsor_principal', 'initial_funding'}
                    or policy['components']['ONBOARDING_SPONSOR_PRINCIPAL'] != onboarding['sponsor_principal']
                    or policy['components']['ONBOARDING_INITIAL_FUNDING'] != onboarding['initial_funding']
                    or type(onboarding['initial_funding']) is not int or not 0 < onboarding['initial_funding'] < 2**128):
                raise ValueError('onboarding sponsor configuration refused')
            config.update(IDENTITY_BINDING_SOCKET='/run/layerx/human/identity-binding.sock',
                IDENTITY_BINDING_TENANT=tenant, IDENTITY_BINDING_PEER_UID=4020,
                IDENTITY_BINDING_PEER_GID=4020, IDENTITY_BINDING_DEADLINE_SECONDS=10)
        config['EXIT_REQUIRED_CONFIRMATIONS'] = movement['PAXEER_CONFIRMATIONS']
        config.update(policy['components'])
        agent.update(policy['agent'])
        write(root / 'components', 'purpose-catalog.json', json.dumps(policy['purpose_catalog']))
        if onboarding is None:
            write(root / 'kms', 'registry.json', json.dumps(policy['registry']))
        elif protected_json(root / 'kms/registry.json') != policy['registry']:
            raise ValueError('retained KMS registry changed')
        for name, data in journal_records(Path(sys.argv[4]).parent / policy['journal_directory']).items():
            destination = journal / name
            destination.write_bytes(data)
            destination.chmod(0o600)
    for key, value in config.items():
        write(root / 'config', 'LAYERX_HUMAN_' + key, value)
    history_files = [root / 'agent/genesis-handover-trust.lxt', root / 'agent/handover-finality.conf']
    if any(path.exists() or path.is_symlink() for path in history_files):
        from provision import protected_bytes
        protected_bytes(root / 'agent/genesis-handover-trust.lxt')
        protected_bytes(root / 'agent/handover-finality.conf')
        agent.update(GENESIS_TRUST='/run/human-private/agent/genesis-handover-trust.lxt',
                     HANDOVER_FINALITY='/run/human-private/agent/handover-finality.conf')
    for key, value in agent.items():
        write(root / 'agent-config', 'LAYERX_AGENT_' + key, value)


def kms_prerequisite(registry_path, tls_root, destination, network, asset, state):
    destination, tls_root, state = Path(destination), Path(tls_root), Path(state)
    protected_file(destination.parent, 0o700)
    if type(network) is not int or not 0 < network < 2**32 or not re.fullmatch('[0-9a-f]{64}', asset):
        refuse('KMS native registry scope')
    registry = parse(read_bytes(registry_path))
    if (set(registry) != {'schema_version', 'assets', 'modules'} or registry['schema_version'] != 2
            or not any(record.get('asset') == asset for record in registry['assets'])):
        refuse('KMS canonical native asset registry')
    modules, seen = [], set()
    for module in registry['modules']:
        if (set(module) != {'module', 'ordinals'} or type(module['module']) is not int
                or not 1 <= module['module'] <= 9 or module['module'] in seen
                or type(module['ordinals']) is not list or not module['ordinals']
                or len(set(module['ordinals'])) != len(module['ordinals'])
                or any(type(n) is not int or not 1 <= n < 65536 for n in module['ordinals'])):
            refuse('KMS canonical native module registry')
        seen.add(module['module'])
        modules.append({'module_id': module['module'], 'activity_types': [(module['module'] << 16) | n for n in module['ordinals']]})
    if not modules or len(modules) > 32:
        refuse('KMS module count')
    files = {'registry.json': json.dumps({'network_id': network, 'protocol_version': 3, 'modules': modules}, sort_keys=True).encode()}
    for role, source, name in [('human-kms', 'cert.der', 'kms-server.der'), ('human-kms', 'key.der', 'kms-server-key.der'),
                              ('human-kms', 'ca.der', 'ca.der'), ('human-kms-client', 'cert.der', 'kms-client.der'),
                              ('human-kms-executor', 'cert.der', 'kms-executor.der')]:
        files[name] = read_bytes(tls_root / role / source, 65536)
    if any(read_bytes(tls_root / role / 'ca.der', 65536) != files['ca.der']
           for role in ('human-kms-client', 'human-kms-executor')):
        refuse('KMS client and server trust roots differ')
    if files['kms-client.der'] == files['kms-executor.der']:
        refuse('KMS service and restricted executor identities must differ')
    if destination.exists() or destination.is_symlink():
        protected_file(destination, 0o700)
        verify_material(destination)
        if any(read_bytes(destination / name) != value for name, value in files.items()):
            refuse('retained KMS registry or identity differs; preserving reconciliation required')
        if len(read_bytes(destination / 'kms-seal', 32)) != 32:
            refuse('retained KMS seal bounds')
        return
    if state.is_symlink() or (state.exists() and any(state.iterdir())):
        refuse('existing KMS state requires its original retained seal and identities')
    pending = Path(tempfile.mkdtemp(prefix='.kms-material-', dir=destination.parent))
    try:
        for name, value in files.items():
            write_bytes(pending / name, value)
        write_bytes(pending / 'kms-seal', secrets.token_bytes(32))
        seal_material(pending)
        import ctypes
        libc = ctypes.CDLL(None, use_errno=True)
        if libc.renameat2(-100, os.fsencode(pending), -100, os.fsencode(destination), 1):
            raise OSError(ctypes.get_errno(), 'KMS material immutable publication refused')
        pending = None
        sync_directory(destination.parent)
    finally:
        if pending is not None:
            shutil.rmtree(pending)


REGISTRY_GENERATION_SCHEMA = 'layerx.kernel.registry-generation.v1'
REGISTRY_GENERATION_FIELDS = {'schema', 'generation', 'network_id', 'sequencer_id', 'sequencer_public_key', 'replica_id', 'genesis_metadata_sha256', 'asset_id', 'history_sha256', 'replica_sha256'}
REGISTRY_GENERATION_FILES = {'generation.json', 'replica-id', 'trust-history'}


def registry_read(path, public=False):
    path = Path(path)
    if not path.is_absolute() or path.resolve() != path:
        refuse('registry material canonical path')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as source:
        info = os.fstat(source.fileno())
        modes = (0o400, 0o440, 0o600, 0o644, 0o444) if public else (0o400, 0o440, 0o600)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid not in (0, os.geteuid(), 4020, 4030)
                or info.st_nlink != 1 or stat.S_IMODE(info.st_mode) not in modes
                or not 0 < info.st_size <= 1048576):
            refuse('registry material owner, mode or bounds')
        data = source.read(1048577)
        after = os.fstat(source.fileno())
        if len(data) != info.st_size or (info.st_mtime_ns, info.st_ctime_ns) != (after.st_mtime_ns, after.st_ctime_ns):
            refuse('registry material changed during read')
        return data


def registry_manifest(files):
    import struct
    if set(files) != REGISTRY_GENERATION_FILES:
        refuse('registry generation file inventory')
    manifest = parse(files['generation.json'])
    if type(manifest) is not dict or set(manifest) != REGISTRY_GENERATION_FIELDS or manifest['schema'] != REGISTRY_GENERATION_SCHEMA:
        refuse('registry generation schema')
    if type(manifest['network_id']) is not int or not 0 < manifest['network_id'] < 2**32:
        refuse('registry generation network')
    for name in REGISTRY_GENERATION_FIELDS - {'schema', 'network_id'}:
        if type(manifest[name]) is not str or not re.fullmatch('[0-9a-f]{64}', manifest[name]):
            refuse('registry generation identifier')
    unsigned = {key: value for key, value in manifest.items() if key != 'generation'}
    if manifest['generation'] != digest(json.dumps(unsigned, sort_keys=True, separators=(',', ':')).encode()):
        refuse('registry generation digest')
    if (not re.fullmatch(b'[0-9a-f]{64}\n?', files['replica-id'])
            or files['replica-id'].decode().rstrip('\n') != manifest['replica_id']
            or digest(files['replica-id']) != manifest['replica_sha256']
            or digest(files['trust-history']) != manifest['history_sha256']):
        refuse('registry generation exact producer bytes')
    history = files['trust-history']; magic = b'LayerX/sequencer-trust-history/v1\0'
    if not history.startswith(magic) or len(history) < len(magic) + 4:
        refuse('registry generation trust history framing')
    current, retired = struct.unpack('>HH', history[len(magic):len(magic)+4])
    if current != 1 or retired > 256 or len(history) != len(magic) + 4 + 103 * (current + retired):
        refuse('registry generation trust history bounds')
    for offset in range(current + retired):
        entry = history[len(magic)+4+103*offset:len(magic)+4+103*(offset+1)]
        protocol, network, epoch = struct.unpack('>HIQ', entry[:14])
        first, last, retired_flag, retired_at = struct.unpack('>QQBQ', entry[78:])
        if protocol != 3 or network != manifest['network_id'] or epoch == 0 or not 0 < first <= last or retired_flag not in (0, 1):
            refuse('registry generation trust history entry')
        if offset == 0 and (entry[14:46].hex() != manifest['sequencer_id'] or entry[46:78].hex() != manifest['sequencer_public_key'] or retired_flag != 0 or retired_at != 0):
            refuse('registry generation current sequencer mismatch')
        if offset > 0 and (retired_flag != 1 or retired_at == 0):
            refuse('registry generation retired sequencer mismatch')
    if digest(('layerx-sequencer:' + manifest['sequencer_public_key']).encode()) != manifest['sequencer_id']:
        refuse('registry generation sequencer derivation')
    return manifest


def verify_registry_material(directory):
    directory = Path(directory)
    if (directory / 'current').is_symlink():
        selected = (directory / 'current').resolve(strict=True)
        if selected.parent != directory / 'generations' or not re.fullmatch('[0-9a-f]{64}', selected.name):
            refuse('registry generation selector')
        directory = selected
    info = directory.lstat()
    if directory.resolve() != directory or not stat.S_ISDIR(info.st_mode) or info.st_uid not in (0, os.geteuid(), 4030) or stat.S_IMODE(info.st_mode) not in (0o700, 0o750):
        refuse('registry generation directory owner or mode')
    if {path.name for path in directory.iterdir()} != REGISTRY_GENERATION_FILES:
        refuse('registry generation directory inventory')
    if any((directory / name).lstat().st_uid != info.st_uid for name in REGISTRY_GENERATION_FILES):
        refuse('registry generation mixed-owner tuple')
    files = {name: registry_read(directory / name) for name in REGISTRY_GENERATION_FILES}
    manifest = registry_manifest(files)
    return {'directory': str(directory), 'generation': manifest['generation'], 'manifest': manifest, 'files': files}


def publish_registry_material(destination, files, producer=False):
    import fcntl
    destination = Path(destination)
    if not destination.is_absolute() or destination.resolve() != destination:
        refuse('registry generation destination')
    try:
        destination.mkdir(mode=0o750 if producer else 0o700)
        if producer:
            os.chown(destination, 0, 4020); os.chmod(destination, 0o750)
    except FileExistsError:
        pass
    info = destination.lstat()
    if info.st_uid != os.geteuid() or stat.S_IMODE(info.st_mode) != (0o750 if producer else 0o700):
        refuse('registry generation store protection')
    lock_fd = os.open(destination / '.registry-material.lock', os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    with os.fdopen(lock_fd, 'r+b') as lock:
        info = os.fstat(lock.fileno())
        if info.st_uid != os.geteuid() or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1:
            refuse('registry generation publication lock')
        fcntl.flock(lock, fcntl.LOCK_EX)
        manifest = registry_manifest(files)
        marker = destination / 'current'
        if marker.is_symlink():
            prior = verify_registry_material(destination)
            if prior['generation'] != manifest['generation']:
                refuse('retained registry generation differs; owner reconciliation required')
        elif marker.exists():
            refuse('registry generation selector type')
        generations = destination / 'generations'
        try:
            generations.mkdir(mode=0o750 if producer else 0o700)
            if producer:
                os.chown(generations, 0, 4020); os.chmod(generations, 0o750)
        except FileExistsError:
            pass
        if generations.is_symlink() or generations.stat().st_uid != os.geteuid() or stat.S_IMODE(generations.stat().st_mode) != (0o750 if producer else 0o700):
            refuse('registry generation directory protection')
        selected = generations / manifest['generation']
        if selected.exists():
            retained = verify_registry_material(selected)
            if retained['files'] != files:
                refuse('registry generation replay conflict')
        else:
            pending = Path(tempfile.mkdtemp(prefix='.pending-', dir=destination))
            try:
                for name, data in files.items():
                    write_bytes(pending / name, data)
                    if producer:
                        os.chown(pending / name, 0, 4020); os.chmod(pending / name, 0o440)
                if producer:
                    os.chown(pending, 0, 4020); os.chmod(pending, 0o750)
                verify_registry_material(pending); sync_directory(pending)
                os.rename(pending, selected); sync_directory(generations)
            finally:
                if pending.exists(): shutil.rmtree(pending)
        if producer:
            os.chown(destination, 0, 4020); os.chown(generations, 0, 4020)
        if marker.is_symlink() and os.readlink(marker) == 'generations/' + manifest['generation']:
            return {'directory': str(selected), 'generation': manifest['generation']}
        temporary = destination / ('.current-' + secrets.token_hex(8))
        os.symlink('generations/' + manifest['generation'], temporary)
        os.replace(temporary, marker); sync_directory(destination)
        return {'directory': str(selected), 'generation': manifest['generation']}


def export_registry_material(source):
    material = verify_registry_material(source)
    return {'schema': 'layerx.kernel.registry-export.v1', 'generation': material['generation'],
            'files': {name: base64.b64encode(data).decode() for name, data in material['files'].items()}}


def import_registry_material(source, destination, network, sequencer, public):
    exported = parse(registry_read(Path(source)))
    if type(exported) is not dict or set(exported) != {'schema', 'generation', 'files'} or exported['schema'] != 'layerx.kernel.registry-export.v1' or type(exported['files']) is not dict or set(exported['files']) != REGISTRY_GENERATION_FILES:
        refuse('registry transfer schema')
    files = {name: base64.b64decode(value, validate=True) for name, value in exported['files'].items()}
    manifest = registry_manifest(files)
    if (manifest['generation'] != exported['generation'] or manifest['network_id'] != network
            or manifest['sequencer_id'] != sequencer or manifest['sequencer_public_key'] != public):
        refuse('registry transfer authenticated producer identity')
    return publish_registry_material(destination, files)


def kernel_registry_material_produce(genesis, seed, destination, network, retained_history=None):
    import struct
    import subprocess
    if os.geteuid() != 0 or not 0 < network < 2**32:
        refuse('root kernel registry producer required')
    genesis = Path(genesis)
    for path, uid, gid, mode in ((Path(seed), 4020, 4020, 0o600), (genesis / 'metadata.lxgb', 4020, 4020, 0o600), (genesis / 'asset-id', 0, 0, 0o444), (genesis / 'replica-id', 0, 0, 0o444)):
        info = path.lstat()
        if (info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)) != (uid, gid, mode):
            refuse('kernel protected producer ownership')
    metadata = registry_read(genesis / 'metadata.lxgb', True)
    asset = registry_read(genesis / 'asset-id', True)
    replica = registry_read(genesis / 'replica-id', True)
    key = registry_read(Path(seed))
    if not re.fullmatch(b'[0-9a-f]{64}\n?', key) or not re.fullmatch(b'[0-9a-f]{64}\n?', asset) or not re.fullmatch(b'[0-9a-f]{64}\n?', replica):
        refuse('kernel genesis canonical identity')
    result = subprocess.run(['openssl', 'pkey', '-inform', 'DER', '-pubout', '-outform', 'DER'],
        input=bytes.fromhex('302e020100300506032b657004220420') + bytes.fromhex(key.decode().rstrip('\n')),
        stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=True, timeout=10)
    if len(result.stdout) != 44 or result.stdout[:12] != bytes.fromhex('302a300506032b6570032100'):
        refuse('kernel public identity derivation')
    public = result.stdout[12:].hex(); sequencer = digest(('layerx-sequencer:' + public).encode())
    if replica.decode().rstrip('\n') != digest(('layerx-authority-replica:' + public).encode()):
        refuse('kernel genesis replica identity mismatch')
    history = b'LayerX/sequencer-trust-history/v1\0' + struct.pack('>HH', 1, 0) + struct.pack('>HIQ', 3, network, 1) + bytes.fromhex(sequencer) + bytes.fromhex(public) + struct.pack('>QQBQ', 1, 1 << 40, 0, 0)
    if retained_history is not None and (Path(retained_history).exists() or Path(retained_history).is_symlink()):
        if registry_read(Path(retained_history)) != history:
            refuse('kernel retained history differs from authenticated genesis')
    manifest = {'schema': REGISTRY_GENERATION_SCHEMA, 'network_id': network, 'sequencer_id': sequencer,
        'sequencer_public_key': public, 'replica_id': replica.decode().rstrip('\n'),
        'genesis_metadata_sha256': digest(metadata), 'asset_id': asset.decode().rstrip('\n'),
        'history_sha256': digest(history), 'replica_sha256': digest(replica)}
    manifest['generation'] = digest(json.dumps(manifest, sort_keys=True, separators=(',', ':')).encode())
    files = {'generation.json': json.dumps(manifest, sort_keys=True, separators=(',', ':')).encode() + b'\n', 'replica-id': replica, 'trust-history': history}
    return publish_registry_material(destination, files, True)


def hosted_registry_material_produce(data, destination):
    import struct
    import subprocess
    data = Path(data)
    if os.geteuid() != 4020:
        refuse('hosted kernel producer identity')
    protected_file(data, 0o700)

    def environment(name):
        path = data / name
        protected_file(path, 0o600)
        raw = registry_read(path)
        values = {}
        for line in raw.decode('ascii').splitlines():
            if not line:
                continue
            key, separator, value = line.partition('=')
            if (not separator or not re.fullmatch('LAYERX_[A-Z0-9_]+', key)
                    or key in values or not value or any(ord(c) < 32 or ord(c) == 127 for c in value)):
                refuse('hosted kernel environment framing')
            values[key] = value
        return raw, values

    node_raw, node = environment('node.env')
    replica_raw, replica = environment('replica.env')
    names = ('NETWORK_ID', 'SEQUENCER_ID', 'SEQUENCER_PUBLIC_KEY', 'REPLICA_ID', 'ASSET_ID')
    values = {name: node.get('LAYERX_NODE_' + name) for name in names}
    if not re.fullmatch('[1-9][0-9]*', values['NETWORK_ID'] or ''):
        refuse('hosted kernel network identity')
    network = int(values['NETWORK_ID'])
    if not 0 < network < 2**32 or any(not re.fullmatch('[0-9a-f]{64}', values[name] or '') for name in names[1:]):
        refuse('hosted kernel public identity')
    if any(replica.get('LAYERX_AUTHORITY_' + name) != values[name] for name in ('SEQUENCER_ID', 'SEQUENCER_PUBLIC_KEY', 'REPLICA_ID')):
        refuse('hosted kernel replica generation mismatch')
    first_text = replica.get('LAYERX_AUTHORITY_FIRST_BATCH', '')
    last_text = replica.get('LAYERX_AUTHORITY_LAST_BATCH', '')
    if not re.fullmatch('[1-9][0-9]*', first_text) or not re.fullmatch('[1-9][0-9]*', last_text):
        refuse('hosted kernel batch authorization')
    first, last = int(first_text), int(last_text)
    if not 0 < first <= last < 2**64:
        refuse('hosted kernel batch authorization bounds')
    manifest_path = data / 'genesis/genesis.manifest'
    manifest_bytes = registry_read(manifest_path, True)
    if (len(manifest_bytes) < 114 or manifest_bytes[:6] != bytes.fromhex('000347010003')
            or struct.unpack('>I', manifest_bytes[6:10])[0] != network
            or manifest_bytes[-104:-100] != struct.pack('>I', 32)
            or manifest_bytes[-68:-64] != struct.pack('>I', 64)
            or manifest_bytes[-100:-68].hex() != values['SEQUENCER_PUBLIC_KEY']):
        refuse('hosted signed genesis identity mismatch; finalized history requires owner reconciliation')
    if digest(('layerx-sequencer:' + values['SEQUENCER_PUBLIC_KEY']).encode()) != values['SEQUENCER_ID']:
        refuse('hosted kernel sequencer derivation')
    subprocess.run(['/usr/local/bin/layerx-handover', '--verify-key', str(manifest_path),
                    str(data / 'checkpoints/da-bodies.log'), values['SEQUENCER_PUBLIC_KEY']],
                   stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                   check=True, timeout=30)
    request_path = data / 'genesis/genesis-request.lxgb'
    request = registry_read(request_path)
    if (len(request) < 23 or request[:7] != b'LXGB\x02\x00\x03'
            or struct.unpack('>I', request[7:11])[0] != network):
        refuse('hosted native genesis request network')
    parameters = struct.unpack('>H', request[19:21])[0]
    offset = 21 + 66 * parameters
    if parameters > 64 or offset + 2 > len(request):
        refuse('hosted native genesis request parameter bounds')
    guarantors = struct.unpack('>H', request[offset:offset + 2])[0]
    offset += 2 + 81 * guarantors
    if guarantors > 32 or offset + 32 > len(request) or request[offset:offset + 32].hex() != values['ASSET_ID']:
        refuse('hosted native genesis request asset identity')
    if (registry_read(manifest_path, True) != manifest_bytes or registry_read(data / 'node.env') != node_raw
            or registry_read(data / 'replica.env') != replica_raw):
        refuse('hosted kernel generation changed during verification')
    replica_bytes = (values['REPLICA_ID'] + '\n').encode()
    history = (b'LayerX/sequencer-trust-history/v1\0' + struct.pack('>HH', 1, 0)
               + struct.pack('>HIQ', 3, network, 1) + bytes.fromhex(values['SEQUENCER_ID'])
               + bytes.fromhex(values['SEQUENCER_PUBLIC_KEY']) + struct.pack('>QQBQ', first, last, 0, 0))
    generation = {'schema': REGISTRY_GENERATION_SCHEMA, 'network_id': network,
                  'sequencer_id': values['SEQUENCER_ID'], 'sequencer_public_key': values['SEQUENCER_PUBLIC_KEY'],
                  'replica_id': values['REPLICA_ID'], 'genesis_metadata_sha256': digest(request),
                  'asset_id': values['ASSET_ID'], 'history_sha256': digest(history), 'replica_sha256': digest(replica_bytes)}
    generation['generation'] = digest(json.dumps(generation, sort_keys=True, separators=(',', ':')).encode())
    files = {'generation.json': json.dumps(generation, sort_keys=True, separators=(',', ':')).encode() + b'\n',
             'replica-id': replica_bytes, 'trust-history': history}
    return publish_registry_material(destination, files)


POLICY_GRAPH = {
    'genesis': ('owner-input:signed native genesis request and sequencer seed', ()),
    'sequencer-identity': ('kernel_registry_material_produce', ('genesis',)),
    'registry-material': ('import_registry_material', ('sequencer-identity',)),
    'paxeer-deployment': ('paxeer_contracts_deploy', ()),
    'owner-policy': ('owner-input:beta-owner-policy.json', ()),
    'module-registry': ('registry_deployment_produce', ('registry-material',)),
    'owner-evidence': ('human_owner_provision', ('sequencer-identity', 'owner-policy', 'paxeer-deployment')),
    'native-evidence': ('human_native_provision', ('owner-evidence', 'module-registry')),
    'deployment-journal': ('human_journal_deploy', ('module-registry',)),
    'naming-evidence': ('naming_program_deploy', ('deployment-journal',)),
    'principal-policy': ('principal_policy', ('native-evidence', 'owner-policy')),
    'assembled-policy': ('assemble_policy', ('paxeer-deployment', 'module-registry', 'owner-evidence', 'native-evidence',
                                             'deployment-journal', 'naming-evidence', 'principal-policy')),
    'role-authority': ('publish_authority_material', ('assembled-policy', 'registry-material')),
}
POLICY_GRAPH_FILES = {
    'owner-evidence': tuple(n for n in EVIDENCE_INPUTS.values() if n != 'principal-policy.json') + tuple(
        'producer-records/' + n for n in ('source-binding.json', 'owner-result.json', 'owner-registration.json',
                                          'owner-custody.json', 'custody.profile', 'treasury.json', 'sequencer.json')),
    'native-evidence': ('producer-records/native-context.json',) + tuple(
        'producer-records/' + label + suffix for label in ('credit', 'identity', 'rotation', 'recovery')
        for suffix in ('.activity', '.receipt')),
    'naming-evidence': ('producer-records/naming-deployment-result.json',),
    'principal-policy': ('principal-policy.json',),
}
POLICY_GRAPH_ROLES = {
    'human-security': ('registry-material',),
    'human-components': ('assembled-policy',),
    'human-identity': ('assembled-policy',),
    'human-movement': ('assembled-policy',),
    'human-owner': ('assembled-policy', 'registry-material'),
    'receipt-authority': ('role-authority',),
}
AUTHORITY_GENERATION_SCHEMA = 'layerx.human.authority-generation.v1'
AUTHORITY_FILES = ('principal-policy.json', 'registry.json', 'authority.json')


def policy_graph_order(graph=None):
    graph = POLICY_GRAPH if graph is None else graph
    for name, (producer, requires) in graph.items():
        if type(producer) is not str or not producer or any(r not in graph for r in requires):
            refuse('policy graph node without declared producer or requirement: ' + name)
    order, done = [], set()
    pending = dict(graph)
    while pending:
        ready = sorted(name for name, (_, requires) in pending.items() if set(requires) <= done)
        if not ready:
            refuse('policy graph cycle: ' + ','.join(sorted(pending)))
        for name in ready:
            order.append(name)
            done.add(name)
            del pending[name]
    return order


def policy_graph_status(evidence, journal, deployment, module_registry, bundle, registry, authority, network, chain):
    evidence, journal, bundle, registry, authority = map(Path, (evidence, journal, bundle, registry, authority))
    nodes, facts = {}, {}

    class Waiting(Exception):
        pass

    def present(path):
        if not os.path.lexists(path):
            raise Waiting('missing ' + str(path))
        return read_bytes(path)

    def generation():
        if not os.path.lexists(registry / 'current') and not os.path.lexists(registry / 'generation.json'):
            raise Waiting('kernel registry generation not yet produced')
        selected = verify_registry_material(registry)
        if selected['manifest']['network_id'] != network:
            refuse('registry generation network differs')
        facts['registry'] = selected['manifest']
        return selected['manifest']

    def owner_policy():
        authority = parse(present(evidence / 'authority.json'))
        agent = parse(present(evidence / 'agent.json'))
        if (type(authority) is not dict or type(authority.get('core-clock-horizon')) is not int
                or authority['core-clock-horizon'] <= 0 or type(agent) is not dict
                or type(agent.get('HUMAN_LIMIT_CEILING')) is not int or agent['HUMAN_LIMIT_CEILING'] <= 0
                or type(agent.get('HUMAN_LIMIT_ID')) is not str or not re.fullmatch('[0-9a-f]{64}', agent['HUMAN_LIMIT_ID'])):
            refuse('owner policy horizon or limit binding')

    def module():
        value = parse(present(Path(module_registry)))
        if (type(value) is not dict or value.get('network_id') != network or value.get('schema_version') != 2
                or not value.get('assets') or not value.get('modules')):
            refuse('module registry network or content')
        if not any(asset.get('asset') == facts['registry']['asset_id'] for asset in value['assets']):
            refuse('module registry does not carry the kernel genesis asset')

    def files(node):
        for name in POLICY_GRAPH_FILES[node]:
            present(evidence / name)

    def native():
        files('native-evidence')
        context = parse(read_bytes(evidence / 'producer-records/native-context.json'))
        if (type(context) is not dict or context.get('network_id') != network
                or context.get('sequencer_public_key') != facts['registry']['sequencer_public_key']):
            refuse('native evidence sequencer or network differs from the kernel identity generation')

    def journal_check():
        if not os.path.lexists(journal) or (journal.is_dir() and not journal.is_symlink() and not any(journal.iterdir())):
            raise Waiting('admitted/deployed Programs journal not yet produced')
        records = journal_records(journal)
        if not any(name.endswith('.deployment') for name in records):
            refuse('deployment journal without deployed records')
        facts['journal'] = records

    def naming():
        files('naming-evidence')
        value = parse(read_bytes(evidence / 'producer-records/naming-deployment-result.json'))
        if (type(value) is not dict or value.get('state') != 'deployed' or type(value.get('receipt_digest')) is not str
                or any(value['receipt_digest'] + suffix not in facts['journal'] for suffix in ('.admission', '.deployment'))):
            refuse('naming evidence not bound to an admitted/deployed journal pair')

    def assembled():
        present(bundle / 'policy.json')
        present(bundle / 'bundle-manifest.json')
        facts['bundle'] = verify_bundle(bundle, network, chain)

    def role_authority():
        if not os.path.lexists(authority / 'current'):
            raise Waiting('role authority generation not yet published')
        manifest = verify_authority_material(authority)
        if (manifest['bundle_sha256'] != facts['bundle']['bundle_sha256']
                or manifest['registry_generation'] != facts['registry']['generation']):
            refuse('published role authority differs from the assembled policy or kernel identity generation')

    checks = {'genesis': lambda: generation()['genesis_metadata_sha256'], 'sequencer-identity': generation,
              'registry-material': generation, 'paxeer-deployment': lambda: present(Path(deployment)),
              'owner-policy': owner_policy, 'module-registry': module,
              'owner-evidence': lambda: files('owner-evidence'), 'native-evidence': native,
              'deployment-journal': journal_check, 'naming-evidence': naming,
              'principal-policy': lambda: files('principal-policy'), 'assembled-policy': assembled,
              'role-authority': role_authority}
    order = policy_graph_order()
    for name in order:
        producer, requires = POLICY_GRAPH[name]
        node = {'producer': producer, 'requires': list(requires)}
        refused = [r for r in requires if nodes[r]['state'] == 'refused']
        waiting = [r for r in requires if nodes[r]['state'] == 'waiting']
        if refused:
            node.update(state='refused', reason='requires refused ' + ','.join(refused))
        elif waiting:
            node.update(state='waiting', reason='waiting on ' + ','.join(waiting))
        else:
            try:
                checks[name]()
                node.update(state='ready', reason=None)
            except Waiting as error:
                node.update(state='waiting', reason=str(error))
            except (ValueError, OSError, KeyError, TypeError, AttributeError) as error:
                node.update(state='refused', reason=str(error) or type(error).__name__)
        nodes[name] = node
    roles = {}
    for role, requires in POLICY_GRAPH_ROLES.items():
        states = {nodes[r]['state'] for r in requires}
        roles[role] = 'refused' if 'refused' in states else 'waiting' if 'waiting' in states else 'ready'
    return {'schema': 'layerx.human.policy-graph-status.v1', 'order': order, 'nodes': nodes, 'roles': roles}


def authority_manifest(files):
    if set(files) != set(AUTHORITY_FILES) | {'generation.json'}:
        refuse('authority generation file inventory')
    manifest = parse(files['generation.json'])
    fields = {'schema', 'generation', 'network_id', 'chain_id', 'bundle_sha256', 'policy_sha256', 'authority_sha256',
              'registry_generation', 'files'}
    if type(manifest) is not dict or set(manifest) != fields or manifest['schema'] != AUTHORITY_GENERATION_SCHEMA:
        refuse('authority generation schema')
    unsigned = {key: value for key, value in manifest.items() if key != 'generation'}
    if manifest['generation'] != digest(json.dumps(unsigned, sort_keys=True, separators=(',', ':')).encode()):
        refuse('authority generation digest')
    if manifest['files'] != [entry(name, files[name]) for name in AUTHORITY_FILES]:
        refuse('authority generation bytes differ from manifest')
    return manifest


def authority_directory(directory):
    directory = Path(directory)
    protected_file(directory, 0o700)
    if {path.name for path in directory.iterdir()} != set(AUTHORITY_FILES) | {'generation.json'}:
        refuse('inconsistent partial authority publication: ' + directory.name)
    files = {name: read_bytes(directory / name) for name in (*AUTHORITY_FILES, 'generation.json')}
    manifest = authority_manifest(files)
    if directory.name != manifest['generation']:
        refuse('authority generation directory name')
    return manifest, files


def verify_authority_material(destination):
    destination = Path(destination)
    marker = destination / 'current'
    if not marker.is_symlink():
        refuse('authority generation selector')
    selected = marker.resolve(strict=True)
    if selected.parent != destination / 'generations' or not re.fullmatch('[0-9a-f]{64}', selected.name):
        refuse('authority generation selector target')
    return authority_directory(selected)[0]


def publish_authority_material(bundle, registry, destination, network, chain, placement=None):
    import fcntl
    bundle, destination = Path(bundle), Path(destination)
    binding = verify_bundle(bundle, network, chain)
    selected = verify_registry_material(registry)['manifest']
    if selected['network_id'] != network:
        refuse('kernel identity generation network differs')
    context = parse(read_bytes(bundle / 'inputs/producer-records/native-context.json'))
    if context.get('sequencer_public_key') != selected['sequencer_public_key']:
        refuse('assembled policy sequencer differs from the kernel identity generation')
    module_registry = read_bytes(bundle / 'inputs/module-registry.json')
    if not any(asset.get('asset') == selected['asset_id'] for asset in parse(module_registry)['assets']):
        refuse('assembled module registry does not carry the kernel genesis asset')
    policy = parse(read_bytes(bundle / 'policy.json'))
    files = {'principal-policy.json': json.dumps(policy['principal_policy'], sort_keys=True, separators=(',', ':')).encode(),
             'registry.json': module_registry,
             'authority.json': json.dumps(policy['authority'], sort_keys=True, separators=(',', ':')).encode()}
    manifest = {'schema': AUTHORITY_GENERATION_SCHEMA, 'network_id': network, 'chain_id': chain,
                'bundle_sha256': binding['bundle_sha256'], 'policy_sha256': binding['policy_sha256'],
                'authority_sha256': binding['authority_sha256'], 'registry_generation': selected['generation'],
                'files': [entry(name, files[name]) for name in AUTHORITY_FILES]}
    manifest['generation'] = digest(json.dumps(manifest, sort_keys=True, separators=(',', ':')).encode())
    files['generation.json'] = json.dumps(manifest, sort_keys=True, separators=(',', ':')).encode()
    if not destination.is_absolute() or destination.resolve() != destination:
        refuse('authority generation destination')
    try:
        destination.mkdir(mode=0o700)
    except FileExistsError:
        pass
    protected_file(destination, 0o700)
    lock_fd = os.open(destination / '.authority.lock', os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    with os.fdopen(lock_fd, 'r+b') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        generations = destination / 'generations'
        marker = destination / 'current'
        if marker.is_symlink():
            retained = verify_authority_material(destination)
            if retained['generation'] != manifest['generation']:
                refuse('retained authority generation differs; owner reconciliation required')
        elif marker.exists():
            refuse('authority generation selector type')
        for pending in destination.glob('.pending-*'):
            protected_file(pending, 0o700)
            for path in pending.iterdir():
                if path.name not in files or read_bytes(path) != files[path.name]:
                    refuse('inconsistent partial authority publication: ' + pending.name)
            shutil.rmtree(pending)
        try:
            generations.mkdir(mode=0o700)
        except FileExistsError:
            pass
        protected_file(generations, 0o700)
        target = generations / manifest['generation']
        if os.path.lexists(target):
            if authority_directory(target)[1] != files:
                refuse('authority generation replay conflict')
        else:
            pending = Path(tempfile.mkdtemp(prefix='.pending-', dir=destination))
            for name in (*AUTHORITY_FILES, 'generation.json'):
                write_bytes(pending / name, files[name])
            sync_directory(pending)
            os.rename(pending, target)
            sync_directory(generations)
        if not marker.is_symlink():
            temporary = destination / ('.current-' + secrets.token_hex(8))
            os.symlink('generations/' + manifest['generation'], temporary)
            os.replace(temporary, marker)
            sync_directory(destination)
        if placement is not None:
            placement = Path(placement)
            protected_file(placement, 0o700)
            for name in AUTHORITY_FILES:
                path = placement / name
                if os.path.lexists(path):
                    if read_bytes(path) != files[name]:
                        refuse('placed role authority differs from published generation: ' + name)
                    continue
                temporary = placement / ('.' + name + '.' + secrets.token_hex(8))
                write_bytes(temporary, files[name])
                os.rename(temporary, path)
            sync_directory(placement)
    return {'directory': str(target), 'generation': manifest['generation'],
            'bundle_sha256': manifest['bundle_sha256'], 'registry_generation': manifest['registry_generation']}


if __name__ == '__main__':
    os.umask(0o077)
    try:
        if len(sys.argv) == 3 and sys.argv[1] == '--export-registry-material':
            print(json.dumps(export_registry_material(sys.argv[2]), sort_keys=True))
        elif len(sys.argv) == 7 and sys.argv[1] == '--import-registry-material':
            print(json.dumps(import_registry_material(sys.argv[2], sys.argv[3], int(sys.argv[4]), sys.argv[5], sys.argv[6]), sort_keys=True))
        elif len(sys.argv) == 4 and sys.argv[1] == '--hosted-registry-material-produce':
            print(json.dumps(hosted_registry_material_produce(sys.argv[2], sys.argv[3]), sort_keys=True))
        elif len(sys.argv) == 11 and sys.argv[1] == '--policy-graph-status':
            print(json.dumps(policy_graph_status(*sys.argv[2:9], int(sys.argv[9]), int(sys.argv[10])), sort_keys=True))
        elif len(sys.argv) in (7, 8) and sys.argv[1] == '--publish-authority-material':
            print(json.dumps(publish_authority_material(sys.argv[2], sys.argv[3], sys.argv[4], int(sys.argv[5]), int(sys.argv[6]),
                                                        sys.argv[7] if len(sys.argv) == 8 else None), sort_keys=True))
        elif len(sys.argv) == 3 and sys.argv[1] == '--verify-authority-material':
            print(json.dumps(verify_authority_material(sys.argv[2]), sort_keys=True))
        elif len(sys.argv) == 3 and sys.argv[1] == '--verify-registry-material':
            result = verify_registry_material(sys.argv[2])
            print(json.dumps({key: value for key, value in result.items() if key != 'files'}, sort_keys=True))
        elif len(sys.argv) in (6, 7) and sys.argv[1] == '--kernel-registry-material-produce':
            print(json.dumps(kernel_registry_material_produce(sys.argv[2], sys.argv[3], sys.argv[4], int(sys.argv[5]), sys.argv[6] if len(sys.argv) == 7 else None), sort_keys=True))
        elif len(sys.argv) == 8 and sys.argv[1] == '--kms-prerequisite':
            kms_prerequisite(sys.argv[2], sys.argv[3], sys.argv[4], int(sys.argv[5]), sys.argv[6], sys.argv[7])
        elif len(sys.argv) > 1 and sys.argv[1] == '--assemble':
            assemble_policy(*sys.argv[2:6], int(sys.argv[6]), int(sys.argv[7]))
        elif len(sys.argv) > 1 and sys.argv[1] == '--verify-bundle':
            print(json.dumps(verify_bundle(sys.argv[2], int(sys.argv[3]), int(sys.argv[4])), sort_keys=True))
        elif len(sys.argv) > 1 and sys.argv[1] == '--relocate-bundle':
            print(json.dumps(relocate_bundle(sys.argv[2], sys.argv[3], int(sys.argv[4]), int(sys.argv[5])), sort_keys=True))
        elif len(sys.argv) > 1 and sys.argv[1] == '--seal-material':
            seal_material(sys.argv[2])
        elif len(sys.argv) > 1 and sys.argv[1] == '--verify-material':
            verify_material(sys.argv[2])
        elif len(sys.argv) > 1 and sys.argv[1] == '--genesis-binding':
            print(json.dumps(genesis_binding(sys.argv[2]), sort_keys=True))
        elif len(sys.argv) > 1 and sys.argv[1] == '--import-secret':
            print(json.dumps(import_secret(sys.argv[2], sys.argv[3], int(sys.argv[4]), int(sys.argv[5])), sort_keys=True))
        elif len(sys.argv) > 1 and sys.argv[1] == '--verify-projected-material':
            verify_projected_material(sys.argv[2], sys.argv[3], int(sys.argv[4]), int(sys.argv[5]))
        elif len(sys.argv) > 1 and sys.argv[1] == '--secret-arguments':
            secret_arguments(sys.argv[2], int(sys.argv[3]), int(sys.argv[4]))
        else:
            main()
    except ValueError as error:
        if str(error).startswith(BUNDLE_REFUSED):
            raise SystemExit(str(error))
        raise SystemExit(BUNDLE_REFUSED + 'check policy fields, file ownership and bounds')
    except (OSError, KeyError, TypeError):
        raise SystemExit(BUNDLE_REFUSED + 'check policy fields, file ownership and bounds')
