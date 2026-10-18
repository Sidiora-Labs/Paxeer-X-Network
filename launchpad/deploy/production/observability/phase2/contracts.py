from __future__ import annotations

import hashlib
import ipaddress
import json
import math
from pathlib import Path
import re
import os
import stat
import sys
from urllib.parse import urlsplit

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import contracts_v2 as inventory

Invalid = inventory.Invalid
REQUIREMENTS = tuple('D14_%03d' % n for n in range(19, 31))
DIGEST = re.compile(r'^[a-f0-9]{64}$')
ADDRESS = re.compile(r'^0x[a-fA-F0-9]{40}$')


def require(condition, reason):
    if not condition:
        raise Invalid(reason)


def number(value, minimum=0, maximum=2**53):
    require(type(value) in (int, float) and math.isfinite(value) and minimum <= value <= maximum, 'invalid finite number')
    return value


def digest(value):
    require(isinstance(value, str) and DIGEST.fullmatch(value), 'invalid digest')
    return value


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), allow_nan=False).encode()


def sha(value):
    return hashlib.sha256(value).hexdigest()


def private_document(path, limit=65536):
    path = Path(path)
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        metadata = os.fstat(descriptor)
        require(stat.S_ISREG(metadata.st_mode) and metadata.st_uid == os.geteuid() and metadata.st_mode & 0o077 == 0 and metadata.st_size <= limit, 'private authority mount required')
        with os.fdopen(descriptor, 'rb', closefd=False) as stream:
            raw = stream.read(limit + 1)
        require(len(raw) <= limit, 'private authority mount oversized')
        def unique(pairs):
            result = {}
            for key, value in pairs:
                require(key not in result, 'duplicate authority field')
                result[key] = value
            return result
        return json.loads(raw, object_pairs_hook=unique)
    finally:
        os.close(descriptor)


def load(path, limit=4*1024*1024):
    path = Path(path)
    require(path.is_file() and not path.is_symlink() and path.stat().st_size <= limit, 'document unavailable or oversized')
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, 'duplicate JSON key')
            result[key] = value
        return result
    return json.loads(path.read_bytes(), object_pairs_hook=unique, parse_constant=lambda _: (_ for _ in ()).throw(Invalid('nonfinite JSON')))


def endpoint(value, environment, websocket=False):
    require(isinstance(value, str) and len(value) <= 2048, 'invalid endpoint')
    u = urlsplit(value)
    require(u.hostname and not u.username and not u.password and not u.fragment, 'endpoint authority')
    secure = 'wss' if websocket else 'https'
    plain = 'ws' if websocket else 'http'
    if u.scheme != secure:
        try:
            local = ipaddress.ip_address(u.hostname).is_loopback
        except ValueError:
            local = u.hostname == 'localhost'
        require(environment == 'qualification' and u.scheme == plain and local, 'TLS required outside isolated loopback')
    require(not u.query, 'endpoint query must be structured separately')
    return value


def validate_binding(binding, config):
    inventory.validate(config)
    inventory.exact(binding, ('version', 'candidate_sha256', 'input_interface_sha256', 'deployment_digest', 'configuration_revision', 'environment', 'chain_id', 'contract_addresses', 'services', 'prometheus_url', 'responders', 'probes'))
    require(binding['version'] == '2.0', 'runtime binding version')
    for key in ('candidate_sha256', 'input_interface_sha256', 'deployment_digest'):
        digest(binding[key])
    require(binding['configuration_revision'] == config['revision'] and type(binding['configuration_revision']) is int, 'configuration revision mismatch')
    require(binding['environment'] == config['environment'], 'environment mismatch')
    require(type(binding['chain_id']) is int and binding['chain_id'] == 125, 'wrong chain')
    addresses = binding['contract_addresses']
    require(isinstance(addresses, dict) and addresses, 'missing contract address pins')
    for name, address in addresses.items():
        require(isinstance(name, str) and inventory.SAFE.fullmatch(name), 'contract name')
        require(isinstance(address, str) and ADDRESS.fullmatch(address) and int(address, 16) != 0, 'contract address')
    require(isinstance(binding['services'], list) and len(binding['services']) == len(inventory.SERVICES), 'runtime service inventory')
    require([s.get('service') for s in binding['services'] if isinstance(s, dict)] == list(inventory.SERVICES), 'runtime roles differ')
    for service in binding['services']:
        inventory.exact(service, ('service', 'zone', 'base_url', 'owner'))
        require(service['zone'] in config['zones'], 'unconfigured runtime zone')
        require(isinstance(service['owner'], str) and inventory.SAFE.fullmatch(service['owner']), 'missing service owner')
        endpoint(service['base_url'], binding['environment'])
        require(urlsplit(service['base_url']).path in ('','/'),'service endpoint must name origin root')
    endpoint(binding['prometheus_url'], binding['environment'])
    responders = binding['responders']
    require(isinstance(responders, dict) and responders, 'missing responders')
    for role, responder in responders.items():
        require(isinstance(role, str) and inventory.SAFE.fullmatch(role), 'responder role')
        require(isinstance(responder, str) and inventory.SAFE.fullmatch(responder), 'responder identity')
    for alert in config['alerts']:
        require(alert['owner'] in responders and alert['commander'] in responders, 'unowned alert')
    require(isinstance(binding['probes'], list) and [p.get('journey') for p in binding['probes'] if isinstance(p, dict)] == list(inventory.ROUTES[:5]), 'probe inventory')
    for probe in binding['probes']:
        inventory.exact(probe, ('journey', 'url', 'method', 'body', 'headers', 'schema_path', 'schema_sha256', 'frame', 'token_file'))
        ws = probe['journey'] == 'websocket_replay'
        endpoint(probe['url'], binding['environment'], ws)
        require(probe['method'] == ('POST' if probe['journey'] == 'wallet_challenge' else 'GET'), 'synthetic economic method denied')
        require(isinstance(probe['headers'], dict) and set(probe['headers']) <= {'Origin', 'Idempotency-Key'}, 'synthetic headers denied')
        require(all(isinstance(v, str) and '\r' not in v and '\n' not in v for v in probe['headers'].values()), 'invalid header')
        require(isinstance(probe['schema_path'], str) and not Path(probe['schema_path']).is_absolute() and '..' not in Path(probe['schema_path']).parts, 'schema path')
        digest(probe['schema_sha256'])
        if probe['journey'] == 'wallet_challenge':
            require(urlsplit(probe['url']).path == '/v2/auth/challenges', 'challenge endpoint differs from actual provider')
            inventory.exact(probe['body'], ('wallet', 'origin'))
            require(ADDRESS.fullmatch(probe['body']['wallet']) and probe['body']['origin'] == probe['headers'].get('Origin'), 'challenge scope')
        else:
            require(probe['body'] is None, 'unexpected synthetic body')
        if ws:
            require('Origin' in probe['headers'],'socket synthetic origin missing')
            require(isinstance(probe['frame'], dict) and probe['frame'].get('op') == 'subscribe' and probe['frame'].get('afterCursor'), 'replay must use admitted cursor')
            require(isinstance(probe['token_file'], str) and Path(probe['token_file']).is_absolute(), 'nonprivileged session mount missing')
        else:
            require(probe['frame'] is None and probe['token_file'] is None, 'unexpected session or socket data')
    return binding


def safe_diagnostics(binding):
    return {key: binding[key] for key in ('version', 'candidate_sha256', 'input_interface_sha256', 'deployment_digest', 'configuration_revision', 'environment', 'chain_id', 'contract_addresses')} | {'services': [{k: s[k] for k in ('service', 'zone', 'owner')} for s in binding['services']]}
