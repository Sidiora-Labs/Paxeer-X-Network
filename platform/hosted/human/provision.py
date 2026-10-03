#!/usr/bin/env python3
import argparse
import base64
import hashlib
import shutil
import tempfile
import subprocess
import json
import os
from pathlib import Path
import re
import stat
import struct
import sys
import unicodedata
import time


class Refused(ValueError):
    pass


def require(condition, path, field):
    if not condition:
        raise Refused(f'{path}: invalid {field}')


def fields(value, names, path, field):
    require(type(value) is dict and set(value) == set(names.split()), path, field)


def uint(value, bits, path, field, minimum=0):
    require(type(value) is int and minimum <= value < 1 << bits, path, field)


def text(value, path, field):
    require(type(value) is str and bool(value) and not any(ord(c) < 32 or ord(c) == 127 for c in value), path, field)


def h32(value, path, field):
    require(type(value) is str and re.fullmatch('[0-9a-f]{64}', value) is not None
            and int(value, 16) != 0, path, field)


def array(value, path, field):
    require(type(value) is list, path, field)


def strict_pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate JSON field')
        result[key] = value
    return result


def protected_json(path):
    path = Path(path)
    fd = None
    try:
        require(path.is_absolute() and path.resolve() == path, path, 'canonical absolute path')
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        info = os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1
                and 0 < info.st_size <= 1048576, path, 'protected regular file (0600, owner, single link, <=1 MiB)')
        with os.fdopen(fd, 'rb') as source:
            fd = None
            data = source.read(1048577)
        require(len(data) <= 1048576, path, 'JSON size')
        return json.loads(data, object_pairs_hook=strict_pairs,
                          parse_constant=lambda _: (_ for _ in ()).throw(ValueError('nonfinite JSON')))
    except Refused:
        raise
    except (OSError, ValueError) as error:
        raise Refused(f'{path}: required protected JSON unavailable or invalid') from error
    finally:
        if fd is not None:
            os.close(fd)


def evidence(value, path, field, activities):
    fields(value, 'activity_id receipt_digest', path, field)
    for key in value:
        h32(value[key], path, f'{field}.{key}')
    if activities is not None:
        require(value['activity_id'] in activities, path, f'{field}.activity_id binding')


def key_policy(value, path, field, activities):
    fields(value, 'policy_revision required_delay_seconds maximum_delay_seconds effective_sequence evidence', path, field)
    for key in ('policy_revision', 'required_delay_seconds', 'effective_sequence'):
        uint(value[key], 64, path, f'{field}.{key}', 1)
    uint(value['maximum_delay_seconds'], 64, path, f'{field}.maximum_delay_seconds', value['required_delay_seconds'])
    evidence(value['evidence'], path, f'{field}.evidence', activities)


def identity(value, path, activities=None):
    fields(value, 'did authorities revocation_sequence frozen evidence capabilities rotation recovery', path, 'identity')
    text(value['did'], path, 'identity.did')
    uint(value['revocation_sequence'], 64, path, 'identity.revocation_sequence', 1)
    require(type(value['frozen']) is bool, path, 'identity.frozen')
    array(value['authorities'], path, 'identity.authorities')
    require(bool(value['authorities']), path, 'identity.authorities')
    for authority in value['authorities']:
        fields(authority, 'kind id', path, 'identity.authorities[]')
        require(authority['kind'] in ('primary_key', 'session_key', 'capability_grant'), path, 'authority.kind')
        h32(authority['id'], path, 'authority.id')
    evidence(value['evidence'], path, 'identity.evidence', activities)
    array(value['capabilities'], path, 'identity.capabilities')
    seen = set()
    for capability in value['capabilities']:
        fields(capability, 'authority action_key capability_id activity_types counterparties assets amount_ceiling expiry_sequence enforceable_dimensions evidence', path, 'identity.capabilities[]')
        for key in ('authority', 'action_key', 'capability_id'):
            h32(capability[key], path, f'capability.{key}')
        binding = tuple(capability[k] for k in ('authority', 'action_key', 'capability_id'))
        require(binding not in seen, path, 'duplicate capability binding')
        seen.add(binding)
        require(any(a['id'] == capability['authority'] for a in value['authorities']), path, 'capability authority binding')
        for key in ('activity_types', 'counterparties', 'assets', 'enforceable_dimensions'):
            array(capability[key], path, f'capability.{key}')
        for activity in capability['activity_types']:
            uint(activity, 16, path, 'capability.activity_types[]')
        for key in ('counterparties', 'assets'):
            for item in capability[key]:
                h32(item, path, f'capability.{key}[]')
        amount = capability['amount_ceiling']
        require(type(amount) is str and re.fullmatch('[0-9]+', amount) is not None
                and len(amount) <= 39 and int(amount) < 1 << 128, path, 'capability.amount_ceiling')
        uint(capability['expiry_sequence'], 64, path, 'capability.expiry_sequence', 1)
        require(all(type(d) is str and d in ('activity_type', 'counterparty', 'asset', 'amount', 'rate', 'purpose', 'expiry') for d in capability['enforceable_dimensions']), path, 'capability.enforceable_dimensions')
        evidence(capability['evidence'], path, 'capability.evidence', activities)
    for key in ('rotation', 'recovery'):
        key_policy(value[key], path, f'identity.{key}', activities)


def owner_registration(work_dir, activities=None, owner_did=None):
    path = Path(work_dir) / 'human-evidence-input/owner-registration.json'
    value = protected_json(path)
    fields(value, 'owner_account authority identity', path, 'owner registration')
    h32(value['owner_account'], path, 'owner_account')
    text(value['authority'], path, 'authority')
    identity(value['identity'], path, activities)
    if owner_did is not None:
        require(value['identity']['did'] == owner_did, path, 'LXIP owner DID binding')
    return value



def purpose_catalog(template_path, registry_path, treasury_path, sequencer_path, asset):
    template_path = Path(template_path)
    template = json.loads(template_path.read_text(), object_pairs_hook=strict_pairs)
    fields(template, 'version presets', template_path, 'catalog template')
    text(template['version'], template_path, 'version')
    array(template['presets'], template_path, 'presets')
    require(bool(template['presets']), template_path, 'presets')
    registry = protected_json(registry_path)
    require(type(registry) is dict and registry.get('schema_version') == 2,
            registry_path, 'version 2 module registry')
    h32(asset, registry_path, 'LAYERX_NODE_ASSET_ID')
    array(registry.get('assets'), registry_path, 'assets')
    require(any(type(a) is dict and a.get('asset') == asset for a in registry['assets']),
            registry_path, 'deployed asset registration')
    array(registry.get('modules'), registry_path, 'modules')
    activities = []
    for module in registry['modules']:
        require(type(module) is dict, registry_path, 'module')
        uint(module.get('module'), 16, registry_path, 'module', 1)
        require(module['module'] <= 9, registry_path, 'closed protocol module')
        array(module.get('ordinals'), registry_path, 'ordinals')
        for ordinal in module['ordinals']:
            uint(ordinal, 16, registry_path, 'ordinal', 1)
            activity = (module['module'] << 16) | ordinal
            require(activity not in activities, registry_path, 'duplicate activity')
            activities.append(activity)
    require(bool(activities), registry_path, 'registered activities')
    counterparties = []
    for path in (treasury_path, sequencer_path):
        account = protected_json(path)
        require(type(account) is dict, path, 'account output')
        h32(account.get('account'), path, 'account')
        require(account['account'] not in counterparties, path, 'distinct protocol account')
        counterparties.append(account['account'])
    result = {'version': template['version'], 'presets': []}
    seen = set()
    for preset in template['presets']:
        fields(preset, 'id amount_ceiling rate_maximum_uses rate_window_sequences purposes expiry_sequence session_scopes session_lifetime_seconds budget_period_seconds budget_expiry_seconds initial_funding', template_path, 'preset template')
        text(preset['id'], template_path, 'preset.id')
        require(preset['id'] not in seen, template_path, 'duplicate preset')
        seen.add(preset['id'])
        for name in ('purposes', 'session_scopes'):
            array(preset[name], template_path, name)
            require(bool(preset[name]), template_path, name)
            for item in preset[name]:
                text(item, template_path, name)
            require(len(set(preset[name])) == len(preset[name]), template_path, name)
        for name in ('amount_ceiling', 'initial_funding'):
            uint(preset[name], 128, template_path, name, 1)
        for name in ('rate_maximum_uses', 'rate_window_sequences', 'expiry_sequence',
                     'session_lifetime_seconds', 'budget_period_seconds', 'budget_expiry_seconds'):
            uint(preset[name], 64, template_path, name, 1)
        result['presets'].append(dict(preset, activity_types=sorted(activities),
            counterparties=[list(bytes.fromhex(a)) for a in counterparties],
            assets=[list(bytes.fromhex(asset))], budget_asset=list(bytes.fromhex(asset))))
    return result


def encode_json(value):
    return (json.dumps(value, separators=(',', ':'), allow_nan=False) + '\n').encode()


def write_json(path, value):
    path = Path(path)
    require(path.is_absolute() and path.resolve() == path, path, 'canonical absolute output')
    encoded = encode_json(value)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as output:
        output.write(encoded)
        output.flush()
        os.fsync(output.fileno())


def preserve_binding(request_path, response_path, output_path):
    request = protected_json(request_path)
    response = protected_json(response_path)
    require(type(request) is dict, request_path, 'principal request')
    require(type(response) is dict, response_path, 'principal response')
    text(request.get('tenant'), request_path, 'tenant absent from principal creation request')
    require(type(request['tenant']) is str and re.fullmatch(r'[a-z0-9_.-]{1,128}', request['tenant']) is not None, request_path, 'tenant')
    require(response.get('tenant') == request['tenant'], response_path, 'returned tenant binding')
    text(response.get('sub'), response_path, 'returned principal sub')
    require(request.get('sub') == response['sub'], response_path, 'requested principal binding')
    write_json(output_path, {'tenant': response['tenant'], 'principal': response['sub']})



def recovery_policy(value, path):
    fields(value, 'root threshold delay_seconds', path, 'recovery policy')
    array(value['root'], path, 'root')
    require(len(value['root']) == 32, path, 'root length')
    for byte in value['root']:
        uint(byte, 8, path, 'root byte')
    require(any(value['root']), path, 'nonzero root')
    uint(value['threshold'], 16, path, 'threshold', 1)
    uint(value['delay_seconds'], 64, path, 'delay_seconds', 1)


def owner_request(work_dir, secrets_dir):
    path = Path(secrets_dir) / 'owner-email'
    raw = protected_bytes(path, 320)
    try:
        email = raw.decode('utf-8').removesuffix('\n')
    except UnicodeDecodeError as error:
        raise Refused(f'{path}: invalid owner email encoding') from error
    require(bool(email) and email == email.strip() and email.count('@') == 1
            and not any(c.isspace() or unicodedata.category(c) == 'Cc' for c in email),
            path, 'owner email')
    output = Path(work_dir) / 'human-evidence-input/owner-request.json'
    try:
        write_json(output, {'email': email, 'display_name': 'Beta owner',
                            'idempotency_key': os.urandom(32).hex(), 'now': int(time.time())})
    except OSError as error:
        raise Refused(f'{output}: owner request publication refused; reconcile existing output') from error


def job_input(work_dir):
    root = Path(work_dir) / 'human-evidence-input'
    path = root / 'owner-request.json'
    request = protected_json(path)
    require(path.stat().st_size <= 16384, path, 'LXIP request size')
    fields(request, 'email display_name idempotency_key now', path, 'owner request')
    for name in ('email', 'display_name', 'idempotency_key'):
        text(request[name], path, name)
    uint(request['now'], 64, path, 'now')
    path = root / 'recovery-policy.json'
    recovery_policy(protected_json(path), path)


def owner_result(work_dir, path):
    value = protected_json(path)
    require(len(Path(path).read_text().splitlines()) == 1, path, 'single-line LXIP result')
    fields(value, 'principal did recovery_root recovery_threshold recovery_delay_seconds', path, 'LXIP result')
    text(value['principal'], path, 'principal')
    text(value['did'], path, 'did')
    recovery = {'root': value['recovery_root'], 'threshold': value['recovery_threshold'],
                'delay_seconds': value['recovery_delay_seconds']}
    recovery_policy(recovery, path)
    policy_path = Path(work_dir) / 'human-evidence-input/recovery-policy.json'
    expected = protected_json(policy_path)
    recovery_policy(expected, policy_path)
    require(recovery == expected, path, 'LXIP recovery policy binding')
    return value


def account_requests(work_dir):
    root = Path(work_dir)
    path = root / 'genesis/node.env'
    info = path.lstat()
    require(path.is_absolute() and path.resolve() == path and stat.S_ISREG(info.st_mode)
            and info.st_uid == os.geteuid() and info.st_nlink == 1 and not info.st_mode & 0o022
            and info.st_size <= 65536, path, 'protected bootstrap output')
    selected = {}
    names = {'LAYERX_NODE_TREASURY_DID', 'LAYERX_NODE_TREASURY_PUBLIC_KEY',
             'LAYERX_NODE_SEQUENCER_PUBLIC_KEY'}
    for line in path.read_text().splitlines():
        key, separator, value = line.partition('=')
        if key in names:
            require(separator and key not in selected, path, 'unique bootstrap binding')
            selected[key] = value
    require(set(selected) == names, path, 'treasury and sequencer exports')
    treasury = selected['LAYERX_NODE_TREASURY_PUBLIC_KEY']
    sequencer = selected['LAYERX_NODE_SEQUENCER_PUBLIC_KEY']
    h32(treasury, path, 'treasury public key')
    h32(sequencer, path, 'sequencer public key')
    require(treasury != sequencer, path, 'distinct counterparty keys')
    require(selected['LAYERX_NODE_TREASURY_DID'] == 'did:layerx:' + treasury, path, 'treasury DID')
    for name, key in [('treasury', treasury), ('sequencer', sequencer)]:
        write_json(root / 'human-evidence-input' / (name + '-request.json'), {'did': 'did:layerx:' + key})


def owner_policy():
    path = Path(__file__).with_name('beta-owner-policy.json')
    value = json.loads(path.read_text(), object_pairs_hook=strict_pairs)
    fields(value, 'core-clock-horizon maximum_age_seconds maximum_age_sequences limit activities_source budgets movement', path, 'owner policy')
    for name in ('core-clock-horizon', 'maximum_age_seconds', 'maximum_age_sequences'):
        uint(value[name], 64, path, name, 1)
    fields(value['limit'], 'scope scope_id_source id name ceiling', path, 'limit')
    require(value['limit']['scope'] == 'agent' and value['limit']['scope_id_source'] == 'owner_account', path, 'owner limit scope')
    h32(value['limit']['id'], path, 'limit.id')
    text(value['limit']['name'], path, 'limit.name')
    uint(value['limit']['ceiling'], 128, path, 'limit.ceiling', 1)
    require(value['activities_source'] == 'owner-registration', path, 'activities source')
    array(value['budgets'], path, 'budgets')
    require(len(set(value['budgets'])) == len(value['budgets']), path, 'duplicate budgets')
    for budget in value['budgets']:
        h32(budget, path, 'budget')
    fields(value['movement'], 'PAXEER_CONFIRMATIONS CHECKPOINT_INTERVAL_SECONDS PAXEER_BLOCK_SECONDS REMINDER_INTERVAL_SECONDS', path, 'movement intervals')
    for key, interval in value['movement'].items():
        uint(interval, 64, path, key, 1)
    return value


def principal_policy(binding, registration, asset, policy, path):
    entry = registration['identity']
    references = [entry['evidence'], entry['rotation']['evidence'], entry['recovery']['evidence']]
    references.extend(c['evidence'] for c in entry['capabilities'])
    activities = sorted({r['activity_id'] for r in references})
    identity(entry, path, activities)
    result = {'principals': [{'tenant': binding['tenant'], 'principal': binding['principal'],
        'account_id': registration['owner_account'], 'asset_id': asset,
        'activities': activities, 'budgets': policy['budgets'],
        'maximum_age_seconds': policy['maximum_age_seconds'],
        'maximum_age_sequences': policy['maximum_age_sequences'], 'identities': [entry]}]}
    encoded = json.dumps(result, separators=(',', ':'), allow_nan=False)
    parsed = json.loads(encoded, object_pairs_hook=strict_pairs)
    fields(parsed, 'principals', path, 'principal policy')
    require(len(parsed['principals']) == 1, path, 'single scoped principal')
    principal = parsed['principals'][0]
    fields(principal, 'tenant principal account_id asset_id activities budgets maximum_age_seconds maximum_age_sequences identities', path, 'principal policy entry')
    h32(principal['account_id'], path, 'account_id')
    h32(principal['asset_id'], path, 'asset_id')
    identity(principal['identities'][0], path, principal['activities'])
    require(parsed == result, path, 'principal policy reparse')
    return parsed


def protected_bytes(path, maximum=1048576):
    path = Path(path)
    fd = None
    try:
        require(path.is_absolute() and path.resolve() == path, path, 'canonical path')
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        info = os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1
                and 0 < info.st_size <= maximum, path, 'protected bounded file')
        with os.fdopen(fd, 'rb') as source:
            fd = None
            result = source.read(maximum + 1)
            after = os.fstat(source.fileno())
        require(len(result) == info.st_size and info.st_mtime_ns == after.st_mtime_ns
                and info.st_ctime_ns == after.st_ctime_ns, path, 'file changed during protected read')
        require(0 < len(result) <= maximum, path, 'file bounds')
        return result
    except OSError as error:
        raise Refused(f'{path}: required protected file unavailable') from error
    finally:
        if fd is not None:
            os.close(fd)


def journal_records(path):
    require(path is not None, 'LAYERX_REGISTRY_JOURNAL', 'registry admission/deployment journal absent')
    path = Path(path)
    try:
        info = path.lstat()
        require(path.is_absolute() and path.resolve() == path and stat.S_ISDIR(info.st_mode)
                and info.st_uid == os.geteuid() and not info.st_mode & 0o022,
                path, 'protected registry journal directory')
        paths = sorted(path.iterdir())
    except OSError as error:
        raise Refused(f'{path}: registry admission/deployment journal unavailable') from error
    require(2 <= len(paths) <= 128, path, 'registry journal pair count')
    names = {p.name for p in paths}
    result = {}
    total = 0
    for record in paths:
        require(re.fullmatch(r'[0-9a-f]{64}\.(admission|deployment)', record.name) is not None,
                record, 'journal filename')
        require({record.stem + '.admission', record.stem + '.deployment'} <= names,
                record, 'registry journal pair missing')
        data = protected_bytes(record, 524288)
        total += len(data)
        require(total <= 524288, path, 'journal total size')
        result[record.name] = data
    return result


def materialize_journal(work_dir, source):
    work_dir = Path(work_dir)
    require(work_dir.is_absolute() and work_dir.resolve() == work_dir,
            work_dir, 'canonical work directory')
    records = journal_records(source)
    destination = work_dir / 'registry-journal'
    lock = work_dir / '.registry-journal-publish'
    try:
        lock.mkdir(mode=0o700)
    except FileExistsError as error:
        raise Refused(f'{lock}: publication already active or interrupted') from error
    pending = None
    try:
        require(not destination.exists() and not destination.is_symlink(),
                destination, 'existing journal requires reconciliation')
        pending = Path(tempfile.mkdtemp(prefix='.registry-journal-', dir=work_dir))
        for name, data in records.items():
            fd = os.open(pending / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            with os.fdopen(fd, 'wb') as output:
                output.write(data)
                output.flush()
                os.fsync(output.fileno())
        require(journal_records(pending) == records, pending, 'copied journal bytes')
        fd = os.open(pending, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
        os.rename(pending, destination)
        pending = None
        fd = os.open(work_dir, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
    finally:
        if pending is not None:
            shutil.rmtree(pending)
        lock.rmdir()


def peer_binding(binding, path):
    fields(binding, 'tenant principal', path, 'tenant/principal binding')
    tenant = binding['tenant']
    principal = binding['principal']
    require(type(tenant) is str and re.fullmatch(r'[a-z0-9_-]{1,128}', tenant) is not None,
            path, 'tenant representable by identity and Human peer consumers')
    require(type(principal) is str and re.fullmatch(r'did:[a-z0-9]+:[^;,]+', principal) is not None
            and len(principal.encode('utf-8')) <= 255
            and not any(c.isspace() or unicodedata.category(c) == 'Cc' for c in principal),
            path, 'principal representable by Human peer consumer')
    return f"uid=4020;tenant={tenant};principal={principal}"


def evidence_inputs(work_dir, registry_path, journal_path):
    root = Path(work_dir)
    owner_registration(root)
    job_input(root)
    binding_path = root / 'identity/source-binding.json'
    peer_binding(protected_json(binding_path), binding_path)
    registry = protected_json(registry_path)
    require(type(registry) is dict and registry.get('schema_version') == 2,
            registry_path, 'version 2 module registry')
    journal_records(journal_path)


def retained_evidence(destination, files, records):
    expected = dict(files)
    expected.update({'journal/' + name: data for name, data in records.items()})
    info = destination.lstat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid()
            and stat.S_IMODE(info.st_mode) == 0o700, destination, 'retained evidence directory; reconciliation required')
    retained = {}
    for path in destination.rglob('*'):
        info = path.lstat()
        require(not stat.S_ISLNK(info.st_mode), path, 'retained evidence symlink refused')
        if stat.S_ISDIR(info.st_mode):
            require(info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o700,
                    path, 'retained evidence directory permissions')
        else:
            retained[str(path.relative_to(destination))] = protected_bytes(path)
    changed = sorted((set(expected) ^ set(retained)) | {n for n in expected.keys() & retained.keys() if expected[n] != retained[n]})
    require(not changed, destination, 'retained evidence differs; reconciliation required, protected bindings not overwritten')


def validate_native_records(records, registration, network):
    from owner_native import Reader, digest, span, receipt_fields
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
    path = 'native owner evidence'
    context = json.loads(records['native-context.json'], object_pairs_hook=strict_pairs)
    fields(context, 'network_id sequencer_public_key', path, 'native context')
    require(context['network_id'] == network, path, 'network binding')
    h32(context['sequencer_public_key'], path, 'sequencer key')
    h32(registration['authority'], path, 'owner authority')
    public = bytes.fromhex(registration['authority'])
    did = registration['identity']['did'].encode()
    require(len(did) <= 255, path, 'owner DID length')
    account = hashlib.sha256(b'LX:ACCOUNT:v1' + span(b'agent:' + did + b':main')).hexdigest()
    require(registration['owner_account'] == account, path, 'original owner account')
    results = {}
    for label, module, ordinal in (('credit', 8, 1), ('identity', 7, 1), ('rotation', 7, 2), ('recovery', 7, 3)):
        activity = records[label + '.activity']
        r = Reader(activity, path)
        require(r.take(5) == b'\0\3\x10\1\14', path, 'canonical signed activity')
        def tag(value):
            require(r.number(1) == value, path, 'canonical activity field')
        tag(1)
        require(r.number(2) == 3, path, 'protocol version')
        tag(2)
        require(r.number(4) == network, path, 'signed activity network')
        tag(3)
        require(r.number(4) == (module << 16) | ordinal, path, 'signed native activity type')
        tag(4)
        require(r.span(255) == did, path, 'signed owner DID')
        tag(5)
        require(r.span(32) == public, path, 'signed owner authority')
        tag(6)
        r.number(8)
        tag(7)
        start, end = r.number(8), r.number(8)
        require(start < end, path, 'activity validity')
        tag(8)
        require(len(r.span(32)) == 32, path, 'idempotency key')
        tag(9)
        r.number(16)
        tag(10)
        payload_hash = r.span(32)
        tag(11)
        payload = r.span(1048576)
        require(payload_hash == digest(b'payload-hash', payload), path, 'payload digest')
        unsigned = b'\0\3\x10\1\13' + activity[5:r.offset]
        tag(12)
        signature = r.span(64)
        r.finish()
        try:
            Ed25519PublicKey.from_public_bytes(public).verify(signature, digest(b'signature-preimage', unsigned))
        except Exception as error:
            raise Refused(path + ': owner activity signature refused') from error
        result = receipt_fields(records[label + '.receipt'], bytes.fromhex(context['sequencer_public_key']), path)
        require(result['activity_id'] == digest(b'activity-id', activity).hex()
                and result['module'] == module and result['version'] == 1, path, 'signed receipt activity binding')
        results[label] = result
    require(results['credit']['target'] == account and results['credit']['amount'] > 0, path, 'committed owner credit')
    did_id = digest(b'did-id', len(did).to_bytes(2, 'big') + did)
    def state(result):
        values = [body for module, event, kind, monetary, body in result['effects']
                  if module == 7 and event == 0x7110 and kind == 3 and not monetary]
        require(len(values) == 1 and len(values[0]) == 223 and values[0][:5] == b'LXGI1'
                and values[0][5:37] == did_id, path, 'native committed owner snapshot')
        return values[0]
    def reference(result):
        return {name: result[name] for name in ('activity_id', 'receipt_digest')}
    def key_policy(result, recovery):
        body = state(result)
        revision, delay, maximum = (175, 183, 191) if recovery else (167, 199, 207)
        read = lambda offset: int.from_bytes(body[offset:offset + 8], 'big')
        minimum, upper = read(delay), read(maximum)
        if not recovery:
            minimum, upper = (minimum + 999) // 1000, upper // 1000
        require(minimum > 0 and upper >= minimum, path, 'native policy delay bounds')
        return dict(policy_revision=read(revision), required_delay_seconds=minimum,
                    maximum_delay_seconds=upper, effective_sequence=result['sequence'], evidence=reference(result))
    recovery = results['recovery']
    expected = dict(did=did.decode(), authorities=[dict(kind='primary_key', id=public.hex())],
                    revocation_sequence=int.from_bytes(state(recovery)[69:77], 'big'), frozen=False,
                    evidence=reference(recovery), capabilities=[], rotation=key_policy(results['rotation'], False),
                    recovery=key_policy(recovery, True))
    require(registration['identity'] == expected, path, 'registration must match actual committed native state')


def native_owner_records(work_dir, registration, registry):
    from owner_native import receipt_fields, digest
    root = Path(work_dir)
    inputs = root / 'human-evidence-input'
    config = protected_json(inputs / 'owner-native.json')
    uint(config.get('network_id'), 32, inputs, 'native network', 1)
    require(config['network_id'] == registry.get('network_id'), inputs, 'native registry network binding')
    h32(config.get('sequencer_public_key'), inputs, 'native sequencer public key')
    records = {'native-context.json': encode_json({
        'network_id': config['network_id'], 'sequencer_public_key': config['sequencer_public_key']})}
    references = {registration['identity'][name]['evidence']['receipt_digest']:
                  registration['identity'][name]['evidence']['activity_id'] for name in ('rotation', 'recovery')}
    references[registration['identity']['evidence']['receipt_digest']] = registration['identity']['evidence']['activity_id']
    verified = set()
    for label in ('credit', 'identity', 'rotation', 'recovery'):
        receipt_path = inputs / 'owner-native-run' / (label + '.receipt')
        receipt = protected_bytes(receipt_path)
        activity = protected_bytes(receipt_path.with_suffix('.activity'))
        result = receipt_fields(receipt, bytes.fromhex(config['sequencer_public_key']), receipt_path)
        require(result['activity_id'] == digest(b'activity-id', activity).hex(), receipt_path, 'actual activity/receipt binding')
        require(result['version'] == 1 and result['module'] == (8 if label == 'credit' else 7), receipt_path, 'native receipt module/version')
        if result['receipt_digest'] in references:
            require(result['activity_id'] == references[result['receipt_digest']], receipt_path, 'registration receipt binding')
            verified.add(result['receipt_digest'])
        if label == 'credit':
            require(result['target'] == registration['owner_account'] and result['amount'] > 0,
                    receipt_path, 'funded actual owner account')
        records[label + '.receipt'] = receipt
        records[label + '.activity'] = activity
    require(verified == set(references), inputs, 'registration references require actual signed receipts')
    validate_native_records(records, registration, registry['network_id'])
    return records


def producer_records(work_dir, registry, journal):
    root = Path(work_dir)
    inputs = root / 'human-evidence-input'
    sources = {'source-binding.json': root / 'identity/source-binding.json',
               'owner-result.json': root / 'human-owner-result.json',
               'owner-registration.json': inputs / 'owner-registration.json',
               'naming-deployment-result.json': root / 'naming-deployment-result.json',
               'treasury.json': inputs / 'treasury.json', 'sequencer.json': inputs / 'sequencer.json'}
    records = {name: protected_bytes(path) for name, path in sources.items()}
    naming = protected_json(sources['naming-deployment-result.json'])
    require(naming.get('state') == 'deployed', sources['naming-deployment-result.json'], 'actual naming deployment')
    h32(naming.get('receipt_digest'), inputs, 'naming receipt')
    h32(naming.get('activity_id'), inputs, 'naming activity')
    require(all(naming['receipt_digest'] + suffix in journal for suffix in ('.admission', '.deployment')),
            inputs, 'naming deployment journal pair')
    records.update(native_owner_records(root, protected_json(sources['owner-registration.json']), registry))
    return records


PRODUCER_MANIFEST_SCHEMA = 'layerx.human.owner-producers.v1'


def producer_manifest(work_dir, registry_path, journal_path, output):
    work_dir = Path(work_dir)
    require(work_dir.is_absolute() and work_dir.resolve() == work_dir, work_dir, 'canonical work directory')
    inputs = work_dir / 'human-evidence-input'
    binding_path = work_dir / 'identity/source-binding.json'
    result_path = work_dir / 'human-owner-result.json'
    registration_path = inputs / 'owner-registration.json'
    owner = {}

    def file_digest(path):
        return hashlib.sha256(protected_bytes(path)).hexdigest()

    def owner_identity():
        peer_binding(protected_json(binding_path), binding_path)
        return file_digest(binding_path)

    def owner_result_input():
        owner.update(owner_result(work_dir, result_path))
        return file_digest(result_path)

    def catalog():
        template_path = Path(__file__).with_name('beta-purpose-catalog.json')
        template = json.loads(template_path.read_text(), object_pairs_hook=strict_pairs)
        fields(template, 'version presets', template_path, 'catalog template')
        accounts = []
        for path in (inputs / 'treasury.json', inputs / 'sequencer.json'):
            account = protected_json(path)
            require(type(account) is dict, path, 'account output')
            h32(account.get('account'), path, 'account')
            require(account['account'] not in accounts, path, 'distinct protocol account')
            accounts.append(account['account'])
        return file_digest(inputs / 'treasury.json')

    def authority():
        binding = protected_json(binding_path)
        peer_binding(binding, binding_path)
        owner_policy()
        return file_digest(binding_path)

    def principal():
        registration = owner_registration(work_dir)
        entry = registration['identity']
        references = [entry['evidence'], entry['rotation']['evidence'], entry['recovery']['evidence']]
        references.extend(c['evidence'] for c in entry['capabilities'])
        identity(entry, registration_path, sorted({r['activity_id'] for r in references}))
        return file_digest(registration_path)

    def recovery():
        path = inputs / 'recovery-policy.json'
        recovery_policy(protected_json(path), path)
        return file_digest(path)

    def movement():
        path = inputs / 'movement-source.json'
        value = protected_json(path)
        fields(value, 'CUSTODY_REFERENCE PAXEER_CHECKPOINT_AUTHORITY', path, 'movement source')
        for key, item in value.items():
            require(type(item) is str and re.fullmatch(r'0x[0-9a-fA-F]{64}', item) is not None
                    and int(item, 16) != 0, path, key)
        return file_digest(path)

    def registry():
        require(registry_path is not None, 'LAYERX_MODULE_REGISTRY', 'module registry absent')
        value = protected_json(registry_path)
        require(type(value) is dict and value.get('schema_version') == 2, registry_path, 'version 2 module registry')
        array(value.get('assets'), registry_path, 'assets')
        array(value.get('modules'), registry_path, 'modules')
        require(bool(value['assets']) and bool(value['modules']), registry_path, 'deployed assets and modules')
        return file_digest(registry_path)

    def journal(suffix):
        records = journal_records(journal_path)
        digest = hashlib.sha256()
        for name in sorted(records):
            if name.endswith(suffix):
                digest.update(name.encode() + b'\n' + hashlib.sha256(records[name]).digest())
        return digest.hexdigest()

    def native_registration():
        native_owner_records(work_dir, owner_registration(work_dir), protected_json(registry_path))
        require(bool(owner), result_path, 'LXIP owner result required before native owner registration')
        owner_registration(work_dir, owner_did=owner['did'])
        return file_digest(registration_path)

    def naming():
        path = work_dir / 'naming-deployment-result.json'
        value = protected_json(path)
        require(type(value) is dict and value.get('state') == 'deployed', path, 'deployed naming program')
        h32(value.get('receipt_digest'), path, 'naming receipt_digest')
        h32(value.get('activity_id'), path, 'naming activity_id')
        records = journal_records(journal_path)
        require(all(value['receipt_digest'] + suffix in records for suffix in ('.admission', '.deployment')), path, 'naming journal binding')
        return file_digest(path)

    def relative(path):
        if path is None:
            return None
        path = Path(path)
        try:
            return str(path.relative_to(work_dir))
        except ValueError:
            return None

    checks = [
        ('owner-identity', 'human_owner_provision', relative(binding_path), owner_identity),
        ('owner-result', 'human_owner_provision', relative(result_path), owner_result_input),
        ('purpose-catalog', 'human_owner_provision', relative(inputs / 'treasury.json'), catalog),
        ('authority', 'human_owner_provision', relative(binding_path), authority),
        ('principal-policy', 'human_native_provision', relative(registration_path), principal),
        ('recovery-policy', 'human_owner_provision', relative(inputs / 'recovery-policy.json'), recovery),
        ('movement-policy', 'human_native_owner_prepare', relative(inputs / 'movement-source.json'), movement),
        ('module-registry', 'registry_deployment_produce', 'secrets:module-registry.json', registry),
        ('admission-journal', 'human_journal_deploy', relative(journal_path), lambda: journal('.admission')),
        ('deployment-journal', 'human_journal_deploy', relative(journal_path), lambda: journal('.deployment')),
        ('native-owner-registration', 'human_native_provision', relative(registration_path), native_registration),
        ('naming-evidence', 'naming_program_deploy', relative(work_dir / 'naming-deployment-result.json'), naming),
    ]
    entries = []
    for name, producer, path, check in checks:
        entry = {'input': name, 'producer': producer, 'path': path, 'status': 'blocked', 'sha256': None, 'reason': None}
        try:
            require(path is not None, work_dir, 'producer output absent or outside the work directory')
            entry.update(status='present', sha256=check())
        except (Refused, OSError, ValueError, KeyError, TypeError) as error:
            entry['reason'] = str(error) or type(error).__name__
        entries.append(entry)
    write_json(output, {'schema': PRODUCER_MANIFEST_SCHEMA, 'inputs': entries})
    blocked = [entry for entry in entries if entry['status'] == 'blocked']
    for entry in blocked:
        print(f"blocked: {entry['input']}: {entry['reason']}", file=sys.stderr)
    return not blocked


def assemble(work_dir, registry_path, asset, journal_path):
    work_dir = Path(work_dir)
    require(work_dir.is_absolute() and work_dir.resolve() == work_dir, work_dir, 'canonical work directory')
    destination = work_dir / 'human-evidence'
    inputs = work_dir / 'human-evidence-input'
    owner = owner_result(work_dir, work_dir / 'human-owner-result.json')
    registration = owner_registration(work_dir, owner_did=owner['did'])
    binding_path = work_dir / 'identity/source-binding.json'
    binding = protected_json(binding_path)
    fields(binding, 'tenant principal', binding_path, 'tenant/principal binding')
    require(type(binding['tenant']) is str and re.fullmatch(r'[a-z0-9_.-]{1,128}', binding['tenant']) is not None,
            binding_path, 'tenant')
    text(binding['principal'], binding_path, 'principal')
    peers = peer_binding(binding, binding_path)
    policy = owner_policy()
    catalog = purpose_catalog(Path(__file__).with_name('beta-purpose-catalog.json'), registry_path,
                              inputs / 'treasury.json', inputs / 'sequencer.json', asset)
    head_path = inputs / 'account-head-result.json'
    head = protected_json(head_path)
    fields(head, 'consumed', head_path, 'verified account head result')
    require(type(head['consumed']) is int and head['consumed'] == 0, head_path,
            'fresh first-batch head required; retained consumed limits unavailable')
    movement_path = inputs / 'movement-source.json'
    movement = protected_json(movement_path)
    fields(movement, 'CUSTODY_REFERENCE PAXEER_CHECKPOINT_AUTHORITY', movement_path, 'movement source')
    for key, value in movement.items():
        require(type(value) is str and re.fullmatch(r'0x[0-9a-fA-F]{64}', value) is not None
                and int(value, 16) != 0, movement_path, key)
    recovery = {'root': owner['recovery_root'], 'threshold': owner['recovery_threshold'],
                'delay_seconds': owner['recovery_delay_seconds']}
    onboarding = None
    if (inputs / 'owner-kms.json').exists():
        onboarding = protected_json(inputs / 'onboarding-configuration.json')
        fields(onboarding, 'directory sponsor_principal initial_funding', inputs, 'onboarding configuration')
        require(onboarding['sponsor_principal'] == owner['principal'], inputs, 'KMS sponsor principal')
        uint(onboarding['initial_funding'], 128, inputs, 'initial_funding', 1)
    files = {
        'components.json': {'AGENT_ACTOR': owner['did'], 'AGENT_AUTHORITY': registration['authority'],
            'AGENT_OWNER_ACCOUNT': 'agent:' + registration['identity']['did'] + ':main',
            'AGENT_RECOVERY_ROOT': base64.urlsafe_b64encode(bytes(recovery['root'])).decode().rstrip('='),
            'AGENT_RECOVERY_THRESHOLD': recovery['threshold']},
        'authority.json': dict(binding, **{'core-clock-horizon': policy['core-clock-horizon']}),
        'agent.json': {'HUMAN_PEERS': peers,
            'HUMAN_LIMIT_SCOPE': policy['limit']['scope'], 'HUMAN_LIMIT_SCOPE_ID': registration['owner_account'],
            'HUMAN_LIMIT_ID': policy['limit']['id'], 'HUMAN_LIMIT_NAME': policy['limit']['name'],
            'HUMAN_LIMIT_CEILING': policy['limit']['ceiling'], 'HUMAN_LIMIT_CONSUMED': head['consumed']},
        'principal-policy.json': principal_policy(binding, registration, asset, policy,
                                                  inputs / 'owner-registration.json'),
        'recovery-policy.json': recovery, 'purpose-catalog.json': catalog,
        'movement-policy.json': dict(movement, **policy['movement'])}
    if onboarding is not None:
        files['components.json'].update(ONBOARDING_SPONSOR_PRINCIPAL=onboarding['sponsor_principal'],
            ONBOARDING_INITIAL_FUNDING=onboarding['initial_funding'])
        files['onboarding-configuration.json'] = onboarding
    records = journal_records(journal_path)
    registry = protected_json(registry_path)
    produced = producer_records(work_dir, registry, records)
    lock = work_dir / '.human-evidence-publish'
    try:
        lock.mkdir(mode=0o700)
    except FileExistsError as error:
        raise Refused(f'{lock}: publication already active or interrupted') from error
    pending = None
    try:
        if destination.exists() or destination.is_symlink():
            expected = {name: encode_json(value) for name, value in files.items()}
            expected.update({'producer-records/' + name: data for name, data in produced.items()})
            retained_evidence(destination, expected, records)
            return
        pending = Path(tempfile.mkdtemp(prefix='.human-evidence-', dir=work_dir))
        for name, value in files.items():
            write_json(pending / name, value)
        (pending / 'producer-records').mkdir(mode=0o700)
        for name, data in produced.items():
            fd = os.open(pending / 'producer-records' / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            with os.fdopen(fd, 'wb') as output:
                output.write(data)
                output.flush()
                os.fsync(output.fileno())
        (pending / 'journal').mkdir(mode=0o700)
        for name, data in records.items():
            fd = os.open(pending / 'journal' / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(fd, 'wb') as output:
                output.write(data)
                output.flush()
                os.fsync(output.fileno())
        for directory in (pending / 'producer-records', pending / 'journal', pending):
            fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(fd)
            finally:
                os.close(fd)
        os.rename(pending, destination)
        pending = None
        fd = os.open(work_dir, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
    finally:
        if pending is not None:
            shutil.rmtree(pending)
        lock.rmdir()


def movement_source(work_dir, secrets_dir):
    inputs = Path(work_dir) / 'human-evidence-input'
    path = Path(secrets_dir) / 'human/movement-config/LAYERX_HUMAN_MOVEMENT_PROVIDER_CUSTODY_REFERENCE'
    if path.exists() or path.is_symlink():
        reference = protected_bytes(path, 66).decode('ascii')
    else:
        path = Path(work_dir) / 'paxeer/deployment.json'
        deployment = protected_json(path)
        require(type(deployment) is dict and 'custody_reference' in deployment, path,
                'custody_reference missing; vault registration requires a produced reference')
        reference = deployment['custody_reference']
    require(type(reference) is str and re.fullmatch(r'0x[0-9a-fA-F]{64}', reference) is not None
            and int(reference, 16) != 0, path, 'custody_reference')
    key_path = inputs / 'checkpoint-public.base64'
    try:
        public = base64.b64decode(protected_bytes(key_path, 128), validate=True).decode('ascii')
    except (ValueError, UnicodeError) as error:
        raise Refused('Secret layerx-guarantor-checkpoint-authority/public.hex: invalid public key') from error
    require(re.fullmatch(r'0x[0-9a-fA-F]{64}', public) is not None and int(public, 16) != 0,
            'Secret layerx-guarantor-checkpoint-authority/public.hex', 'checkpoint public key')
    write_json(inputs / 'movement-source.json', {'CUSTODY_REFERENCE': reference, 'PAXEER_CHECKPOINT_AUTHORITY': public})


def qualify_generated_set(work_dir, registry_path, secrets_dir, network, chain):
    import material
    work_dir = Path(work_dir)
    root = work_dir / 'human-material-check'
    require(not root.exists(), root, 'existing material check output')
    evidence = work_dir / 'human-evidence'
    for name in ('components', 'agent', 'authority', 'principal-policy', 'recovery-policy', 'purpose-catalog', 'movement-policy'):
        protected_json(evidence / (name + '.json'))
    journal_records(evidence / 'journal')
    root.mkdir(mode=0o700)
    private = root / 'human'
    private.mkdir(mode=0o700)
    for name in ('components', 'kms', 'config', 'agent-config', 'movement-config', 'authority-config', 'authority', 'identity'):
        (private / name).mkdir(mode=0o700)
    replica = protected_bytes(Path(secrets_dir) / 'receipt-authority-replica-id', 128).decode('ascii').strip()
    h32(replica, secrets_dir, 'receipt authority replica id')
    fd = os.open(root / 'receipt-authority-replica-id', os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as output:
        output.write(replica)
    bundle = root / 'policy-bundle'
    bundle.mkdir(mode=0o700)
    policy_path = bundle / 'policy.json'
    material.assemble_policy(evidence, work_dir / 'paxeer/deployment.json', registry_path,
                             policy_path, network, chain)
    subprocess.run(['python3', str(Path(__file__).with_name('material.py')), str(private),
                    str(network), str(chain), str(policy_path)], check=True)
    subprocess.run(['python3', str(Path(__file__).with_name('test_material.py'))], check=True)


EXPLORER_READ_FUNDING = 'explorer-read-funding'
EXPLORER_READ_CREDIT_VALIDITY_MS = 300000


def explorer_read_identity(secrets_dir):
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
    seed_path = Path(secrets_dir) / 'explorer-read.seed.hex'
    seed = protected_bytes(seed_path, 65).decode('ascii').strip()
    require(re.fullmatch('[0-9a-f]{64}', seed) is not None, seed_path, 'explorer read principal seed')
    signer = Ed25519PrivateKey.from_private_bytes(bytes.fromhex(seed))
    public = signer.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
    published = Path(secrets_dir) / 'explorer-read.pub.hex'
    with open(published, 'rb') as source:
        require(source.read(130).decode('ascii').strip() == public.hex(), published,
                'explorer read principal public key binding')
    did = b'did:layerx:' + public.hex().encode('ascii')
    name = b'agent:' + did + b':main'
    account = hashlib.sha256(b'LX:ACCOUNT:v1' + struct.pack('>I', len(name)) + name).digest()
    return signer, public, did, account


def native_owner_activity(signer, public, did, network, module, ordinal, sequence, not_before, idempotency, fee_limit, payload):
    from owner_native import digest, span
    body = (b'\1' + struct.pack('>H', 3) + b'\2' + struct.pack('>I', network)
        + b'\3' + struct.pack('>I', (module << 16) | ordinal) + b'\4' + span(did)
        + b'\5' + span(public) + b'\6' + struct.pack('>Q', sequence)
        + b'\7' + struct.pack('>QQ', not_before, not_before + EXPLORER_READ_CREDIT_VALIDITY_MS)
        + b'\10' + span(idempotency) + b'\11' + fee_limit.to_bytes(16, 'big')
        + b'\12' + span(digest(b'payload-hash', payload)) + b'\13' + span(payload))
    unsigned = b'\0\3\x10\1\13' + body
    return b'\0\3\x10\1\14' + body + b'\14' + span(signer.sign(digest(b'signature-preimage', unsigned)))


def _explorer_read_funding_prepare(work_dir, secrets_dir):
    from owner_native import protected_write
    _, public, did, account = explorer_read_identity(secrets_dir)
    source = Path(work_dir) / 'human-evidence-input'
    custody_path = source / 'owner-custody.json'
    custody = protected_json(custody_path)
    fields(custody, 'vault asset runtime_sha256 payer', custody_path, 'native custody binding')
    profile = protected_bytes(source / 'custody.profile', 223)
    require(len(profile) == 223 and profile[:5] == b'LXBC3'
            and profile[97:129].hex() == custody['asset'], source / 'custody.profile', 'custody profile asset binding')
    root = Path(work_dir) / EXPLORER_READ_FUNDING
    root.mkdir(mode=0o700)
    target = root / 'human-evidence-input'
    target.mkdir(mode=0o700)
    write_json(target / 'owner-admission.json',
               dict(did=did.decode('ascii'), public_key=public.hex(), owner_account=account.hex()))
    write_json(target / 'owner-custody.json', custody)
    protected_write(target / 'custody.profile', profile)


def _explorer_read_credit_sign(work_dir, secrets_dir, network, state_path, output):
    signer, public, did, account = explorer_read_identity(secrets_dir)
    uint(network, 32, work_dir, 'network id', 1)
    credit_path = Path(work_dir) / EXPLORER_READ_FUNDING / 'human-evidence-input/custody-credit.bin'
    credit = protected_bytes(credit_path)
    require(len(credit) > 363 and credit[:5] == b'LXDC3' and credit[107:139] == account
            and credit[139:171] == public, credit_path, 'explorer read principal custody credit binding')
    amount = int.from_bytes(credit[191:207], 'big')
    require(amount > 0, credit_path, 'custody credit amount')
    state = protected_json(state_path)
    require(type(state) is dict, state_path, 'node read state')
    uint(state.get('account_sequence'), 64, state_path, 'account_sequence')
    activity = native_owner_activity(signer, public, did, network, 8, 1, state['account_sequence'],
        time.time_ns() // 1000000, hashlib.sha256(b'LX:DEPOSIT:NULLIFIER:v1' + credit[43:75]).digest(), 0, credit)
    write_json(output, dict(did=did.decode('ascii'), public_key=public.hex(), account=account.hex(),
                            amount=amount, activity=activity.hex()))


def _explorer_read_credit_submit(work_dir, request_path, output):
    from owner_native import digest, protected_write, receipt, receipt_fields
    config_path = Path(work_dir) / 'human-evidence-input/owner-native.json'
    config = protected_json(config_path)
    require(type(config) is dict, config_path, 'native producer configuration')
    for name in ('node_socket', 'layerxctl'):
        text(config.get(name), config_path, name)
        require(Path(config[name]).is_absolute(), config_path, name)
    uint(config.get('network_id'), 32, config_path, 'network_id', 1)
    h32(config.get('sequencer_public_key'), config_path, 'sequencer_public_key')
    request = protected_json(request_path)
    fields(request, 'did public_key account amount activity', request_path, 'explorer read principal credit request')
    text(request['did'], request_path, 'did')
    h32(request['public_key'], request_path, 'public_key')
    h32(request['account'], request_path, 'account')
    uint(request['amount'], 128, request_path, 'amount', 1)
    require(type(request['activity']) is str and re.fullmatch('(?:[0-9a-f]{2}){1,4096}', request['activity']) is not None,
            request_path, 'signed credit activity')
    signed = bytes.fromhex(request['activity'])
    activity_id = digest(b'activity-id', signed)
    activity_path = Path(request_path).with_name('explorer-read-credit.activity')
    protected_write(activity_path, signed)
    completed = subprocess.run([config['layerxctl'], 'submit', '--public-key', request['public_key'],
        '--activity', str(activity_path), '--socket', config['node_socket'], '--network-id', str(config['network_id']),
        '--protocol-version', '3', '--actor', request['did']], capture_output=True)
    if completed.returncode != 0:
        protected_write(Path(request_path).with_name('layerxctl-refusal.txt'), completed.stderr)
    require(completed.returncode == 0, activity_path, 'submission; layerxctl refused or the outcome is unknown')
    acknowledgement = json.loads(completed.stdout)
    require(acknowledgement['activity_id'] == activity_id.hex() and acknowledgement['state'] == 'acknowledged',
            activity_path, 'durable acknowledgement')
    raw = receipt(config['node_socket'], activity_id)
    receipt_path = Path(request_path).with_name('explorer-read-credit.receipt')
    protected_write(receipt_path, raw)
    result = receipt_fields(raw, bytes.fromhex(config['sequencer_public_key']), receipt_path)
    require(result['activity_id'] == activity_id.hex() and result['module'] == 8 and result['version'] == 1
            and result['target'] == request['account'] and result['amount'] == request['amount'],
            receipt_path, 'committed explorer read principal credit')
    write_json(output, dict(activity_id=result['activity_id'], receipt_digest=result['receipt_digest'],
                            sequence=result['sequence'], account=result['target'], amount=result['amount']))


def explorer_read_funding(step, work_dir, *arguments):
    try:
        step(work_dir, *arguments)
    except Refused:
        raise
    except (OSError, ValueError, KeyError, TypeError, OverflowError) as error:
        raise Refused(f'{Path(work_dir) / EXPLORER_READ_FUNDING}: explorer read principal funding refused; '
                      'preserve the directory and reconcile the custody deposit before retry') from error


def main():
    parser = argparse.ArgumentParser()
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument('--prepare-owner-request', action='store_true')
    mode.add_argument('--validate-evidence-inputs', action='store_true')
    mode.add_argument('--materialize-journal', action='store_true')
    mode.add_argument('--validate-owner-registration', action='store_true')
    mode.add_argument('--produce-owner-registration', action='store_true')
    mode.add_argument('--prepare-owner-admission', action='store_true')
    mode.add_argument('--catalog', action='store_true')
    mode.add_argument('--assemble', action='store_true')
    mode.add_argument('--producer-manifest', action='store_true')
    mode.add_argument('--movement-source', action='store_true')
    mode.add_argument('--qualify-generated-set', action='store_true')
    mode.add_argument('--account-requests', action='store_true')
    mode.add_argument('--validate-job-input', action='store_true')
    mode.add_argument('--validate-owner-result', action='store_true')
    mode.add_argument('--preserve-binding', action='store_true')
    mode.add_argument('--prepare-explorer-read-funding', action='store_true')
    mode.add_argument('--sign-explorer-read-credit', action='store_true')
    mode.add_argument('--submit-explorer-read-credit', action='store_true')
    parser.add_argument('--registry', type=Path)
    parser.add_argument('--treasury', type=Path)
    parser.add_argument('--sequencer', type=Path)
    parser.add_argument('--asset')
    parser.add_argument('--journal', type=Path)
    parser.add_argument('--secrets-dir', type=Path)
    parser.add_argument('--network', type=int)
    parser.add_argument('--chain', type=int)
    parser.add_argument('--request', type=Path)
    parser.add_argument('--response', type=Path)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--work-dir', type=Path, required=True)
    args = parser.parse_args()
    if args.prepare_owner_request:
        require(args.secrets_dir is not None, args.work_dir, 'secrets directory')
        owner_request(args.work_dir, args.secrets_dir)
    elif args.prepare_owner_admission:
        require(args.secrets_dir is not None, args.work_dir, 'secrets directory')
        from owner_native import prepare_admission
        prepare_admission(args.work_dir, args.secrets_dir)
    elif args.produce_owner_registration:
        from owner_native import produce
        produce(args.work_dir)
    elif args.materialize_journal:
        materialize_journal(args.work_dir, args.journal)
    elif args.validate_evidence_inputs:
        require(args.registry is not None, args.work_dir, 'module registry path')
        evidence_inputs(args.work_dir, args.registry, args.journal)
    elif args.qualify_generated_set:
        require(all((args.registry, args.secrets_dir, args.network, args.chain)), args.work_dir, 'generated set qualification arguments')
        qualify_generated_set(args.work_dir, args.registry, args.secrets_dir, args.network, args.chain)
    elif args.movement_source:
        require(args.secrets_dir is not None, args.work_dir, 'secrets directory')
        movement_source(args.work_dir, args.secrets_dir)
    elif args.assemble:
        require(all((args.registry, args.asset)), args.work_dir, 'registry and asset arguments')
        assemble(args.work_dir, args.registry, args.asset, args.journal)
    elif args.producer_manifest:
        require(args.output is not None, args.work_dir, 'producer manifest output')
        if not producer_manifest(args.work_dir, args.registry, args.journal, args.output):
            raise SystemExit(3)
    elif args.account_requests:
        account_requests(args.work_dir)
    elif args.validate_job_input:
        job_input(args.work_dir)
    elif args.validate_owner_result:
        require(args.request is not None, args.work_dir, 'owner result path')
        owner_result(args.work_dir, args.request)
    elif args.catalog:
        require(all((args.registry, args.treasury, args.sequencer, args.asset, args.output)),
                args.work_dir, 'catalog input arguments')
        value = purpose_catalog(Path(__file__).with_name('beta-purpose-catalog.json'),
                                args.registry, args.treasury, args.sequencer, args.asset)
        write_json(args.output, value)
    elif args.preserve_binding:
        require(all((args.request, args.response, args.output)), args.work_dir, 'binding arguments')
        preserve_binding(args.request, args.response, args.output)
    elif args.prepare_explorer_read_funding:
        require(args.secrets_dir is not None, args.work_dir, 'secrets directory')
        explorer_read_funding(_explorer_read_funding_prepare, args.work_dir, args.secrets_dir)
    elif args.sign_explorer_read_credit:
        require(all((args.secrets_dir, args.network, args.request, args.output)), args.work_dir,
                'explorer read principal credit arguments')
        explorer_read_funding(_explorer_read_credit_sign, args.work_dir, args.secrets_dir, args.network,
                              args.request, args.output)
    elif args.submit_explorer_read_credit:
        require(all((args.request, args.output)), args.work_dir, 'explorer read principal credit arguments')
        explorer_read_funding(_explorer_read_credit_submit, args.work_dir, args.request, args.output)
    else:
        owner_registration(args.work_dir)


if __name__ == '__main__':
    import sys
    sys.modules['provision'] = sys.modules[__name__]
    try:
        main()
    except Refused as error:
        raise SystemExit(str(error))
