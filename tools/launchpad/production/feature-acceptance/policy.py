"""Fail-closed feature evidence plugin. Trust/context are coordinator inputs, never evidence inputs.

A signature establishes accountability, not that an arbitrary producer told the truth.
Only independently admitted harness identities may be trusted by the serial runner.
"""
from __future__ import annotations
import base64
import hashlib
import json
import pathlib
import re
import subprocess
import tempfile

FEATURES = {f'PF{i:02}' for i in range(1, 31)} | {f'OC{i:02}' for i in range(1, 26)}
BINDINGS = {'source', 'image', 'migration', 'api_schema', 'event_schema', 'registry', 'topology', 'policy', 'capability', 'inventory'}
SCENARIOS = {'success', 'authorization_denial', 'dependency_outage', 'reconnect_retry'}
HEX = re.compile(r'[0-9a-f]{64}\Z')
MAX_JSON = 8 * 1024 * 1024
MAX_ARTIFACT = 64 * 1024 * 1024

class Rejected(ValueError):
    pass

def require(condition, reason):
    if not condition:
        raise Rejected(reason)

def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False, allow_nan=False).encode()

def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()

def strict_json(raw):
    require(isinstance(raw, bytes) and len(raw) <= MAX_JSON, 'JSON size/type')
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, 'duplicate JSON key: ' + key)
            result[key] = value
        return result
    try:
        return json.loads(raw, object_pairs_hook=pairs, parse_constant=lambda _: (_ for _ in ()).throw(Rejected('nonfinite JSON')))
    except (UnicodeError, json.JSONDecodeError) as exc:
        raise Rejected('invalid JSON') from exc

def keys(value, expected, label):
    require(type(value) is dict and set(value) == set(expected), label + ' fields')

def integer(value):
    return type(value) is int and 0 <= value <= 2**53-1

def text(value, limit=256):
    return type(value) is str and 0 < len(value) <= limit and not any(ord(c) < 32 for c in value)

def verify_signature(envelope, trust, role, feature):
    keys(envelope, {'payload', 'signer', 'signature'}, role + ' envelope')
    signer = envelope['signer']
    require(text(signer) and signer in trust, 'untrusted signer')
    identity = trust[signer]
    keys(identity, {'public_key', 'roles', 'features', 'principal', 'enabled'}, 'trust identity')
    require(identity['enabled'] is True and role in identity['roles'] and feature in identity['features'], 'signer scope/role')
    require(text(identity['principal']), 'trust principal')
    try:
        sig = base64.b64decode(envelope['signature'], validate=True)
    except (ValueError, TypeError) as exc:
        raise Rejected('invalid signature encoding') from exc
    require(len(sig) == 64 and base64.b64encode(sig).decode() == envelope['signature'], 'noncanonical signature')
    require(type(identity['public_key']) is str and len(identity['public_key']) <= 4096, 'public key bounds')
    with tempfile.TemporaryDirectory(prefix='feature-signature-') as temp:
        root = pathlib.Path(temp)
        (root/'key.pem').write_text(identity['public_key'])
        (root/'message').write_bytes(canonical(envelope['payload']))
        (root/'signature').write_bytes(sig)
        try:
            key_der = subprocess.run(['openssl', 'pkey', '-pubin', '-in', str(root/'key.pem'), '-outform', 'DER'], capture_output=True, timeout=5)
            require(key_der.returncode == 0 and len(key_der.stdout) == 44 and key_der.stdout[:12] == bytes.fromhex('302a300506032b6570032100'), 'public key must be Ed25519')
            result = subprocess.run(['openssl', 'pkeyutl', '-verify', '-pubin', '-inkey', str(root/'key.pem'), '-rawin', '-in', str(root/'message'), '-sigfile', str(root/'signature')], capture_output=True, timeout=5)
        except (OSError, subprocess.TimeoutExpired) as exc:
            raise Rejected('signature verifier unavailable') from exc
        require(result.returncode == 0, 'signature mismatch')
    return signer, identity['principal']

def validate_inventory(inventory):
    keys(inventory, {'version', 'features'}, 'inventory')
    require(inventory['version'] == 1 and type(inventory['features']) is list and len(inventory['features']) == 55, 'inventory count/version')
    seen = set()
    for item in inventory['features']:
        keys(item, {'id', 'title', 'requirements', 'owner_lanes', 'prerequisites', 'scenarios', 'state', 'inherited_dependency_ids', 'requirement_owner_lanes'}, 'feature')
        require(item['id'] in FEATURES and item['id'] not in seen, 'inventory feature identity')
        seen.add(item['id'])
        require(type(item['inherited_dependency_ids']) is list and len(set(item['inherited_dependency_ids'])) == len(item['inherited_dependency_ids']), 'inherited dependencies')
        require(type(item['requirement_owner_lanes']) is list and len(item['requirement_owner_lanes']) > 0, 'requirement ownership')
        require(text(item['title']) and item['state'] == 'unqualified', 'inventory declaration')
        require(type(item['requirements']) is list and len(item['requirements']) > 0 and len(set(item['requirements'])) == len(item['requirements']), 'requirements coverage')
        require(all(re.fullmatch(r'D\d{2}_\d{3}', r) for r in item['requirements']), 'requirement identity')
        require(type(item['owner_lanes']) is list and len(item['owner_lanes']) > 0 and all(re.fullmatch(r'L(?:0[1-9]|[1-4][0-9]|50)', lane) for lane in item['owner_lanes']), 'feature owner')
        require(len(set(item['owner_lanes'])) == len(item['owner_lanes']), 'duplicate owners')
        require(type(item['scenarios']) is dict and set(item['scenarios']) == SCENARIOS and all(text(v, 2048) for v in item['scenarios'].values()), 'scenario coverage')
        require(type(item['prerequisites']) is list and len(item['prerequisites']) > 0, 'prerequisites missing')
        require(len({d['id'] for d in item['prerequisites']}) == len(item['prerequisites']), 'duplicate dependencies')
        for dep in item['prerequisites']:
            keys(dep, {'id', 'kind', 'owner', 'remediation'}, 'prerequisite')
            require(all(text(dep[k], 2048) for k in dep) and dep['kind'] in {'contract', 'vendor', 'device', 'service'}, 'prerequisite invalid')
    require(seen == FEATURES, 'inventory exact coverage')
    return {item['id']: item for item in inventory['features']}

def verify_artifacts(entries, root, issued, now):
    require(type(entries) is list and 1 <= len(entries) <= 128, 'artifacts count')
    root = pathlib.Path(root).resolve(strict=True)
    found = {}
    for entry in entries:
        keys(entry, {'path', 'sha256', 'size', 'kind', 'object_version', 'retain_until'}, 'artifact')
        path = entry['path']
        require(text(path, 1024) and '\\' not in path and pathlib.PurePosixPath(path).as_posix() == path and not pathlib.PurePosixPath(path).is_absolute() and '..' not in pathlib.PurePosixPath(path).parts, 'unsafe artifact path')
        require(path not in found and HEX.fullmatch(entry['sha256'] or '') is not None, 'duplicate/artifact hash')
        require(integer(entry['size']) and 0 < entry['size'] <= MAX_ARTIFACT, 'artifact size')
        require(text(entry['object_version']) and integer(entry['retain_until']) and entry['retain_until'] >= max(issued, now) + 180*86400, 'immutable version/retention')
        require(entry['kind'] in {'state', 'receipt', 'provider', 'trace', 'device', 'native_result'}, 'artifact kind')
        target = root
        for part in pathlib.PurePosixPath(path).parts:
            target = target / part
            require(not target.is_symlink(), 'artifact symlink')
        require(target.is_file() and target.stat().st_nlink == 1 and target.resolve().is_relative_to(root), 'artifact missing/outside root')
        with target.open('rb') as handle:
            raw = handle.read(MAX_ARTIFACT+1)
        require(len(raw) == entry['size'] and hashlib.sha256(raw).hexdigest() == entry['sha256'], 'artifact mismatch: '+path)
        found[path] = (entry['kind'], raw)
    return found

def qualify(feature, gate, evidence, review, *, inventory, context, trust, artifact_root, dependencies, now):
    """Returns only policy qualification. Does not execute harnesses or promote releases."""
    features = validate_inventory(inventory)
    require(feature in features, 'unsupported feature')
    require(context.get('inventory') == digest(inventory), 'inventory digest mismatch')
    require(gate in {'feature-user-path-v1', 'final-smoke-v1'}, 'missing/unsupported gate')
    require(integer(now), 'clock invalid')
    keys(context, BINDINGS | {'environment', 'implementation_principals', 'run_id', 'harness'}, 'context')
    require(all(type(context[k]) is str and HEX.fullmatch(context[k]) for k in BINDINGS), 'context hashes')
    require(text(context['run_id']) and text(context['harness']), 'trusted run identity')
    require(text(context['environment']) and type(context['implementation_principals']) is list and all(text(p) for p in context['implementation_principals']), 'context identities')
    for dep in features[feature]['prerequisites']:
        require(dependencies.get(dep['id']) is True, 'blocked dependency: '+dep['id']+' owner='+dep['owner'])
    producer, producer_principal = verify_signature(evidence, trust, 'producer', feature)
    payload = evidence['payload']
    keys(payload, {'version', 'feature', 'gate', 'bindings', 'environment', 'issued_at', 'expires_at', 'status', 'scenarios', 'artifacts', 'harness', 'run_id'}, 'evidence')
    require(payload['version'] == 1 and payload['feature'] == feature and payload['gate'] == gate, 'evidence scope')
    require(payload['bindings'] == {k: context[k] for k in BINDINGS} and payload['environment'] == context['environment'], 'candidate/environment mismatch')
    require(integer(payload['issued_at']) and integer(payload['expires_at']), 'timestamps')
    max_age = 86400 if gate == 'final-smoke-v1' else 7*86400
    require(payload['issued_at'] <= now < payload['expires_at'] <= payload['issued_at'] + max_age and now-payload['issued_at'] <= max_age, 'expired/future evidence')
    require(payload['status'] == 'passed' and payload['harness'] == context['harness'] and payload['run_id'] == context['run_id'], 'nonpassing/wrong run or harness')
    artifacts = verify_artifacts(payload['artifacts'], artifact_root, payload['issued_at'], now)
    kinds = {kind for kind, _ in artifacts.values()}
    require('state' in kinds, 'persisted state evidence missing')
    for dependency_kind, evidence_kind in [('contract', 'receipt'), ('vendor', 'provider'), ('device', 'device')]:
        if any(d['kind'] == dependency_kind for d in features[feature]['prerequisites']):
            require(evidence_kind in kinds, evidence_kind + ' evidence missing')
    require(type(payload['scenarios']) is dict and set(payload['scenarios']) == SCENARIOS, 'missing scenario')
    for name, result in payload['scenarios'].items():
        keys(result, {'status', 'expected_sha256', 'result_artifact', 'authority_artifact', 'trace_artifact'}, 'scenario')
        require(result['status'] == 'passed' and result['expected_sha256'] == digest(features[feature]['scenarios'][name]), 'scenario assertion mismatch')
        require(result['result_artifact'] in artifacts and artifacts[result['result_artifact']][0] == 'native_result', 'native result missing')
        require(result['authority_artifact'] in artifacts and artifacts[result['authority_artifact']][0] in {'state', 'receipt', 'provider'}, 'authoritative state missing')
        require(result['trace_artifact'] in artifacts and artifacts[result['trace_artifact']][0] == 'trace', 'user-path trace missing')
        native = strict_json(artifacts[result['result_artifact']][1])
        keys(native, {'scenario', 'run_id', 'candidate', 'tests', 'failed', 'skipped', 'exit_code', 'assertions_sha256'}, 'native result')
        require(native['scenario'] == name and native['run_id'] == payload['run_id'] and native['candidate'] == context['source'] and native['assertions_sha256'] == result['expected_sha256'], 'native scope mismatch')
        require(integer(native['tests']) and native['tests'] > 0 and all(type(native[k]) is int and native[k] == 0 for k in ['failed','skipped','exit_code']), 'noop/nonpassing native result')
    reviewer, reviewer_principal = verify_signature(review, trust, 'reviewer', feature)
    require(reviewer != producer and reviewer_principal != producer_principal and reviewer_principal not in context['implementation_principals'], 'self review conflict')
    r = review['payload']
    keys(r, {'version', 'feature', 'gate', 'evidence_sha256', 'issued_at', 'expires_at', 'disposition', 'findings', 'artifact_hashes'}, 'review')
    require(r['version'] == 1 and r['feature'] == feature and r['gate'] == gate and r['evidence_sha256'] == digest(evidence), 'review scope/hash')
    require(integer(r['issued_at']) and integer(r['expires_at']) and payload['issued_at'] <= r['issued_at'] <= now < r['expires_at'] <= payload['expires_at'], 'review expiry')
    require(r['disposition'] == 'accepted' and type(r['findings']) is list and all(type(f) is dict and set(f) == {'id','disposition'} and text(f['id']) and f['disposition'] == 'resolved' for f in r['findings']), 'unresolved findings')
    require(r['artifact_hashes'] == {a['path']: a['sha256'] for a in payload['artifacts']}, 'review artifact mismatch')
    return {'qualified': True, 'feature': feature, 'gate': gate, 'evidence_sha256': digest(evidence), 'release_qualified': False, 'assurance': 'trusted-producer-and-independent-review-attestation'}

def release_coverage(bundles, **qualification_args):
    """Reverify all signed feature evidence; never accept caller-claimed result dictionaries."""
    require(type(bundles) is list and len(bundles) == 55, 'incomplete feature coverage')
    require({r.get('feature') for r in bundles if type(r) is dict} == FEATURES, 'feature coverage identity')
    for bundle in bundles:
        keys(bundle, {'feature', 'evidence', 'review'}, 'coverage bundle')
        qualify(bundle['feature'], 'feature-user-path-v1', bundle['evidence'], bundle['review'], **qualification_args)
    return {'feature_policy_coverage': True, 'release_qualified': False}

_qualify = qualify
def qualify(*args, **kwargs):
    try:
        return _qualify(*args, **kwargs)
    except Rejected:
        raise
    except (TypeError, KeyError, AttributeError, ValueError, OSError) as exc:
        raise Rejected('malformed evidence or inaccessible artifact') from exc

REDACTED = '[REDACTED]'
CREDENTIAL_FIELDS = {'authorization', 'proxyauthorization', 'cookie', 'setcookie', 'password', 'passwd', 'secret', 'clientsecret', 'apikey', 'accesstoken', 'refreshtoken', 'idtoken', 'privatekey', 'streamkey', 'token', 'signaturekey'}
IDENTITY_FIELDS = {'transactionHash', 'blockHash', 'hash', 'chainId', 'blockNumber', 'logIndex', 'from', 'to', 'address', 'run_id', 'candidate', 'source_sha256', 'registry', 'amount_raw', 'value', 'trace_id', 'span_id', 'transaction_hash', 'block_hash', 'request_id', 'session_id'}
SECRET_PATTERN = re.compile(r'-----BEGIN (?:[A-Z ]+)?PRIVATE KEY-----|\bBearer\s+(?!\[REDACTED\])[^\s"<>]+|\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}|(?:password|api[_-]?key|access[_-]?token|private[_-]?key|stream[_-]?key)\s*[=:]\s*["\']?(?!\[REDACTED\])[^\s"\'&,;}]+', re.IGNORECASE)


def credential_field(name):
    return type(name) is str and re.sub(r'[-_]', '', name).lower() in CREDENTIAL_FIELDS


def redact_value(value):
    from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit
    if type(value) is list:
        return [redact_value(item) for item in value]
    if type(value) is not dict:
        return value
    output = {}
    for key, item in value.items():
        if credential_field(key):
            output[key] = REDACTED if item not in (None, '') else item
        elif key == 'cookies' and type(item) is list:
            require(all(type(cookie) is dict and 'value' in cookie for cookie in item), 'HAR cookies invalid')
            output[key] = [redact_value({**cookie, 'value': REDACTED if cookie['value'] not in (None, '') else cookie['value']}) for cookie in item]
        elif key == 'value' and credential_field(value.get('name')):
            output[key] = REDACTED if item not in (None, '') else item
        elif key == 'url' and type(item) is str:
            url = urlsplit(item)
            require(url.username is None and url.password is None, 'URL userinfo cannot be retained')
            query = parse_qsl(url.query, keep_blank_values=True)
            output[key] = urlunsplit((url.scheme, url.netloc, url.path, urlencode([(name, REDACTED if credential_field(name) and val else val) for name, val in query]), url.fragment)) if any(credential_field(name) for name, _ in query) else item
        elif key == 'text' and type(item) is str and value.get('mimeType', '').split(';')[0].strip() == 'application/json':
            parsed = strict_json(item.encode())
            redacted = redact_value(parsed)
            output[key] = canonical(redacted).decode() if parsed != redacted else item
        else:
            output[key] = redact_value(item)
    return output


def evidence_identities(value, path=''):
    identities = {}
    if type(value) is dict:
        for key, item in value.items():
            pointer = path+'/'+key.replace('~', '~0').replace('/', '~1')
            if key in IDENTITY_FIELDS and not (key == 'value' and (credential_field(value.get('name')) or '/cookies/' in path)):
                identities[pointer] = item
            if key == 'text' and type(item) is str and value.get('mimeType', '').split(';')[0].strip() == 'application/json':
                identities.update(evidence_identities(strict_json(item.encode()), pointer))
            elif key == 'url' and type(item) is str:
                from urllib.parse import parse_qsl, urlsplit
                for name, val in parse_qsl(urlsplit(item).query, keep_blank_values=True):
                    if name in IDENTITY_FIELDS:
                        identities.setdefault(pointer+'/query/'+name, []).append(val)
            identities.update(evidence_identities(item, pointer))
    elif type(value) is list:
        for index, item in enumerate(value):
            identities.update(evidence_identities(item, path+'/'+str(index)))
    return identities


def assert_secret_free(raw, path):
    require(type(raw) is bytes and not SECRET_PATTERN.search(raw.decode('utf-8', errors='replace')), 'credential in artifact: '+path)
    try:
        value = strict_json(raw)
    except Rejected:
        require(not path.endswith(('.json', '.har')), 'invalid structured artifact: '+path)
        return
    require(redact_value(value) == value, 'unredacted credential field: '+path)


def redact_json_artifact(raw):
    original = strict_json(raw)
    redacted = redact_value(original)
    identities = evidence_identities(original)
    require(identities and identities == evidence_identities(redacted), 'redaction removed authoritative identity')
    output = canonical(redacted)
    assert_secret_free(output, 'redacted JSON')
    return output, digest(identities)


def verify_audit(feature, evidence, review, audit, *, context, trust, artifacts, audit_authority, now):
    keys(audit_authority, {'required_traces', 'findings', 'blockers', 'redactions', 'private_artifact_root'}, 'trusted audit authority')
    signer, _ = verify_signature(audit, trust, 'reviewer', feature)
    require(signer == review['signer'], 'audit reviewer mismatch')
    _, producer_principal = verify_signature(evidence, trust, 'producer', feature)
    _, reviewer_principal = verify_signature(review, trust, 'reviewer', feature)
    require(signer != evidence['signer'] and reviewer_principal != producer_principal and reviewer_principal not in context['implementation_principals'], 'self review conflict')
    payload = audit['payload']
    keys(payload, {'version', 'feature', 'evidence_sha256', 'review_sha256', 'bindings', 'environment', 'run_id', 'harness', 'issued_at', 'expires_at', 'required_traces_sha256', 'findings_sha256', 'blockers_sha256', 'redactions'}, 'audit')
    require(payload['version'] == 3 and payload['feature'] == feature and payload['evidence_sha256'] == digest(evidence) and payload['review_sha256'] == digest(review), 'audit scope/hash')
    require(payload['bindings'] == {k: context[k] for k in BINDINGS} and all(payload[k] == context[k] for k in ['environment', 'run_id', 'harness']), 'audit candidate/run mismatch')
    require(integer(payload['issued_at']) and integer(payload['expires_at']) and review['payload']['issued_at'] <= payload['issued_at'] <= now < payload['expires_at'] <= review['payload']['expires_at'], 'audit expiry')
    traces = audit_authority['required_traces']
    require(type(traces) is list and traces and all(text(path, 1024) for path in traces) and len(set(traces)) == len(traces), 'required service traces invalid')
    require(payload['required_traces_sha256'] == digest(traces), 'required trace manifest mismatch')
    require(any(path.endswith('.har') for path in traces), 'expected HAR trace missing')
    for path in traces:
        require(path in artifacts and artifacts[path][0] == 'trace', 'expected service trace missing: '+path)
    findings = audit_authority['findings']
    require(type(findings) is list and len(findings) <= 4096, 'audit findings invalid')
    seen = set()
    for finding in findings:
        keys(finding, {'id', 'feature', 'severity', 'disposition', 'resolution_artifact'}, 'audit finding')
        require(text(finding['id']) and finding['id'] not in seen and finding['feature'] in {feature, '*'}, 'audit finding identity/scope')
        seen.add(finding['id'])
        require(finding['severity'] in {'critical', 'high', 'medium', 'low', 'info'} and finding['disposition'] in {'open', 'resolved'}, 'audit finding disposition')
        require(finding['severity'] not in {'critical', 'high'} or finding['disposition'] == 'resolved', 'unresolved critical/high finding: '+finding['id'])
        if finding['disposition'] == 'resolved':
            require(finding['resolution_artifact'] in artifacts and artifacts[finding['resolution_artifact']][0] in {'state', 'receipt', 'provider', 'native_result'}, 'finding resolution proof missing: '+finding['id'])
        else:
            require(finding['resolution_artifact'] is None, 'open finding cannot claim resolution')
    require(payload['findings_sha256'] == digest(findings), 'audit finding inventory mismatch')
    blockers = audit_authority['blockers']
    require(type(blockers) is list and len(blockers) <= 4096, 'mandatory blockers invalid')
    for blocker in blockers:
        keys(blocker, {'feature', 'owner', 'dependency', 'evidence', 'remediation', 'review_date'}, 'mandatory blocker')
        require(blocker['feature'] in FEATURES and all(text(blocker[k], 2048) for k in ['owner', 'dependency', 'remediation']) and integer(blocker['review_date']) and blocker['review_date'] <= now, 'mandatory blocker detail invalid')
        require(blocker['evidence'] is None or type(blocker['evidence']) is str and HEX.fullmatch(blocker['evidence']), 'mandatory blocker evidence invalid')
    require(payload['blockers_sha256'] == digest(blockers), 'mandatory blocker inventory mismatch')
    require(not blockers, 'mandatory feature blocker: '+blockers[0]['feature'] if blockers else 'mandatory feature blocker')
    require(type(payload['redactions']) is list and len(payload['redactions']) <= 128 and payload['redactions'] == audit_authority['redactions'], 'redaction authority mismatch')
    seen = set()
    for redaction in payload['redactions']:
        keys(redaction, {'artifact_path', 'original', 'identity_sha256'}, 'redaction binding')
        path = redaction['artifact_path']
        require(type(path) is str and path in artifacts and path not in seen, 'redacted artifact missing/duplicate')
        seen.add(path)
        require(type(redaction['identity_sha256']) is str and HEX.fullmatch(redaction['identity_sha256']), 'redaction identity digest invalid')
        original = redaction['original']
        verified = verify_artifacts([original], audit_authority['private_artifact_root'], evidence['payload']['issued_at'], now)
        require(original['kind'] == artifacts[path][0], 'redaction artifact kind mismatch')
        output, identity_hash = redact_json_artifact(verified[original['path']][1])
        require(output == artifacts[path][1] and identity_hash == redaction['identity_sha256'], 'redaction changed authoritative evidence: '+path)
    for path, (_, raw) in artifacts.items():
        assert_secret_free(raw, path)
    return {'audit_policy_version': 3, 'audit_sha256': digest(audit), 'release_qualified': False}


def audit_adversarial(feature, evidence, review, *, required_traces, **qualification_args):
    import copy
    import shutil
    qualify(feature, 'feature-user-path-v1', evidence, review, **qualification_args)
    require(type(required_traces) is list and required_traces and all(path in {a['path'] for a in evidence['payload']['artifacts'] if a['kind'] == 'trace'} for path in required_traces), 'expected service trace missing')
    har_paths = [path for path in required_traces if path.endswith('.har')]
    require(har_paths, 'expected HAR trace missing')
    results = []
    with tempfile.TemporaryDirectory(prefix='evidence-adversarial-') as temp:
        root = pathlib.Path(temp)
        original_root = pathlib.Path(qualification_args['artifact_root'])
        for entry in evidence['payload']['artifacts']:
            target = root/entry['path']
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(original_root/entry['path'], target)
        path = root/har_paths[0]
        original = path.read_bytes()
        cases = [('one_byte_tamper', 'artifact mismatch:'), ('missing_service_trace', 'artifact missing/outside root'), ('reviewer_conflict', 'self review conflict'), ('stale_thirty_day_evidence', 'expired/future evidence')]
        for name, reason in cases:
            arguments = dict(qualification_args, artifact_root=root)
            if name == 'one_byte_tamper':
                path.write_bytes(bytes([original[0] ^ 1])+original[1:])
            elif name == 'missing_service_trace':
                path.unlink()
            elif name == 'reviewer_conflict':
                arguments['trust'] = copy.deepcopy(qualification_args['trust'])
                arguments['trust'][review['signer']]['principal'] = arguments['trust'][evidence['signer']]['principal']
            else:
                arguments['now'] += 30*86400
            try:
                qualify(feature, 'feature-user-path-v1', evidence, review, **arguments)
            except Rejected as exc:
                require(reason in str(exc), 'adversarial case rejected for unrelated reason: '+name)
                results.append({'scenario': name, 'rejected': True, 'reason': str(exc)})
            else:
                raise Rejected('adversarial case unexpectedly accepted: '+name)
            if name in {'one_byte_tamper', 'missing_service_trace'}:
                path.write_bytes(original)
    return {'checks': results, 'release_qualified': False}


def feature_tool_digest():
    directory = pathlib.Path(__file__).parent
    files = sorted(path for path in directory.iterdir() if path.is_file() and (path.suffix == '.py' or path.name.endswith('.schema.json') or path.name.startswith('phase-') or path.name == 'final-integrated-gate-readiness.json'))
    owned = {path.name: hashlib.sha256(path.read_bytes()).hexdigest() for path in files}
    shared = directory.parent/'load'
    for name in ['capture.py', 'phase2_types.py', 'workload.py', 'final-shared-capture.schema.json']:
        owned['load/'+name] = hashlib.sha256((shared/name).read_bytes()).hexdigest()
    return digest(owned)


def feature_schemas():
    from jsonschema import Draft202012Validator
    directory = pathlib.Path(__file__).parent
    evidence = strict_json((directory/'evidence.schema.json').read_bytes())
    journey = strict_json((directory/'journey-evidence.schema.json').read_bytes())
    definitions = {'$schema': evidence['$schema'], '$defs': evidence['$defs']}
    schemas = {'evidence': evidence, 'review': {**definitions, '$ref': '#/$defs/review_envelope'}, 'audit': {**definitions, '$ref': '#/$defs/audit_envelope'}, 'assignment': {**definitions, '$ref': '#/$defs/review_assignment_envelope'}, 'journey': journey}
    for schema in schemas.values():
        Draft202012Validator.check_schema(schema)
    return schemas


def validate_feature_bundle_schema(bundle, schemas):
    from jsonschema import Draft202012Validator
    keys(bundle, {'feature', 'evidence', 'review', 'journey', 'audit'}, 'phase4 feature bundle')
    require(bundle['feature'] in FEATURES, 'feature schema identity')
    for name in ['evidence', 'review', 'journey', 'audit']:
        Draft202012Validator(schemas[name]).validate(bundle[name])
        require(bundle[name]['payload']['feature'] == bundle['feature'], 'cross-feature schema bundle')


def load_capture_contracts():
    import importlib.util
    import sys
    directory = pathlib.Path(__file__).parents[1]/'load'
    for name in ['phase2_types', 'phase2_http', 'workload']:
        if name in sys.modules:
            require(pathlib.Path(sys.modules[name].__file__).resolve() == (directory/(name+'.py')).resolve(), 'foreign shared capture module')
    sys.path.insert(0, str(directory))
    try:
        spec = importlib.util.spec_from_file_location('feature_shared_capture', directory/'capture.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        import phase2_types
        import workload
        return module, phase2_types, workload
    finally:
        sys.path.pop(0)


def pinned_document(reference):
    keys(reference, {'path', 'sha256'}, 'pinned document')
    require(type(reference['sha256']) is str and HEX.fullmatch(reference['sha256']), 'pinned document hash')
    path = pathlib.Path(reference['path'])
    require(path.is_absolute() and not path.is_symlink() and path.is_file() and path.stat().st_nlink == 1 and path.stat().st_size <= MAX_JSON, 'pinned ordinary document required')
    for parent in path.parents:
        require(not parent.is_symlink(), 'pinned document parent symlink')
    raw = path.read_bytes()
    require(hashlib.sha256(raw).hexdigest() == reference['sha256'], 'pinned document mismatch')
    return strict_json(raw)


def root_capacity_references(authority, context):
    keys(authority, {'tool_sha256', 'schema_sha256', 'shared_bindings', 'root_capacity_ledger', 'root_mix', 'spec'}, 'readiness authority')
    require(authority['tool_sha256'] == feature_tool_digest(), 'validator tool version mismatch')
    schemas = feature_schemas()
    require(authority['schema_sha256'] == digest(schemas), 'validator schema version mismatch')
    capture, types, workload = load_capture_contracts()
    bindings = types.Bindings(**authority['shared_bindings'])
    require(bindings.candidate == context['source'] and bindings.topology == context['topology'], 'shared candidate/topology mismatch')
    require(bindings.generator == types.source_digest(), 'shared capture generator version mismatch')
    require(pinned_document(authority['root_capacity_ledger']) == workload.ledger(), 'root capacity ledger mismatch')
    require(pinned_document(authority['root_mix']) == workload.MIX, 'root workload mix mismatch')
    spec_ref = authority['spec']
    keys(spec_ref, {'path', 'sha256'}, 'spec authority')
    source = pathlib.Path(spec_ref['path'])
    require(source.resolve() == pathlib.Path(__file__).resolve().parents[3]/'spec/production-system/spec.kvx' and not source.is_symlink(), 'current root specification required')
    require(hashlib.sha256(source.read_bytes()).hexdigest() == spec_ref['sha256'], 'root spec digest mismatch')
    return schemas, capture, types, workload, bindings


def review_assignment(feature, envelope, *, context, trust, schemas, tool_sha256, now):
    from jsonschema import Draft202012Validator
    Draft202012Validator(schemas['assignment']).validate(envelope)
    reviewer, principal = verify_signature(envelope, trust, 'reviewer', feature)
    payload = envelope['payload']
    require(payload['feature'] == feature and payload['reviewer'] == reviewer and payload['bindings'] == {k: context[k] for k in BINDINGS}, 'assignment scope/candidate mismatch')
    require(all(payload[k] == context[k] for k in ['environment', 'run_id', 'harness']), 'assignment environment/run mismatch')
    require(payload['tool_sha256'] == tool_sha256 and payload['schemas_sha256'] == digest(schemas) and payload['implementation_principals_sha256'] == digest(context['implementation_principals']), 'assignment tool/schema/owner mismatch')
    require(integer(now) and payload['issued_at'] <= now < payload['expires_at'] <= payload['issued_at']+7*86400, 'assignment expired/future')
    producer = payload['producer']
    require(producer in trust, 'assignment producer missing')
    identity = trust[producer]
    keys(identity, {'public_key', 'roles', 'features', 'principal', 'enabled'}, 'assignment producer identity')
    require(identity['enabled'] is True and 'producer' in identity['roles'] and feature in identity['features'] and text(identity['principal']), 'assignment producer not admitted')
    require(reviewer != producer and principal != identity['principal'] and principal not in context['implementation_principals'], 'self review conflict')
    with tempfile.TemporaryDirectory(prefix='assignment-producer-key-') as temp:
        key = pathlib.Path(temp)/'public.pem'
        require(type(identity['public_key']) is str and len(identity['public_key']) <= 4096, 'producer public key bounds')
        key.write_text(identity['public_key'])
        result = subprocess.run(['openssl', 'pkey', '-pubin', '-in', str(key), '-outform', 'DER'], capture_output=True, timeout=5)
        require(result.returncode == 0 and len(result.stdout) == 44 and result.stdout[:12] == bytes.fromhex('302a300506032b6570032100'), 'producer public key must be Ed25519')
    return {'feature': feature, 'producer': producer, 'reviewer': reviewer, 'assignment_sha256': digest(envelope)}


def calibrate_feature_evidence(bundle, *, qualification_args, required_traces):
    import copy
    arguments = {k: qualification_args[k] for k in ['inventory', 'context', 'trust', 'artifact_root', 'dependencies', 'now']}
    results = audit_adversarial(bundle['feature'], bundle['evidence'], bundle['review'], required_traces=required_traces, **arguments)['checks']
    for field in ['source', 'image', 'registry', 'environment']:
        changed = copy.deepcopy(arguments)
        changed['context'][field] = 'unapproved-environment' if field == 'environment' else hashlib.sha256(('different-'+field).encode()).hexdigest()
        try:
            qualify(bundle['feature'], 'feature-user-path-v1', bundle['evidence'], bundle['review'], **changed)
        except Rejected as exc:
            require('candidate/environment mismatch' in str(exc), 'mismatch probe rejected for unrelated reason')
            results.append({'scenario': field+'_mismatch', 'rejected': True, 'reason': str(exc)})
        else:
            raise Rejected('candidate mismatch unexpectedly accepted: '+field)
    return {'feature': bundle['feature'], 'candidate': arguments['context']['source'], 'tool_sha256': feature_tool_digest(), 'run_id': arguments['context']['run_id'], 'checks': results, 'capacity_qualified': False, 'release_qualified': False}


def validator_readiness(bundles, assignments, *, readiness_authority, inventory, context, trust, artifact_root, dependencies, authority, audit_authority, now):
    import importlib.util
    from jsonschema import Draft202012Validator
    features = validate_inventory(inventory)
    keys(context, BINDINGS | {'environment', 'implementation_principals', 'run_id', 'harness'}, 'readiness context')
    require(context['inventory'] == digest(inventory) and all(type(context[k]) is str and HEX.fullmatch(context[k]) for k in BINDINGS), 'readiness candidate/inventory mismatch')
    schemas, capture, types, workload, _ = root_capacity_references(readiness_authority, context)
    shared_schema = strict_json(capture.SCHEMA_PATH.read_bytes())
    Draft202012Validator.check_schema(shared_schema)
    require(type(assignments) is dict and set(assignments) == FEATURES, 'all55 independent review assignments required')
    mapped = []
    feature_schema = schemas['evidence']['properties']['payload']['properties']['feature']
    for feature in sorted(features):
        Draft202012Validator(feature_schema).validate(feature)
        mapped.append(review_assignment(feature, assignments[feature], context=context, trust=trust, schemas=schemas, tool_sha256=readiness_authority['tool_sha256'], now=now))
    require(type(bundles) is list and 1 <= len(bundles) <= 55 and len({b.get('feature') for b in bundles if type(b) is dict}) == len(bundles), 'actual isolated feature evidence required')
    require(type(audit_authority) is dict and set(audit_authority) == FEATURES, 'audit authority exact coverage required')
    spec = importlib.util.spec_from_file_location('feature_readiness_journey', pathlib.Path(__file__).with_name('journey_policy.py'))
    journey = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(journey)
    qualified, calibration = [], []
    arguments = dict(inventory=inventory, context=context, trust=trust, artifact_root=artifact_root, dependencies=dependencies, authority=authority, now=now)
    for bundle in bundles:
        validate_feature_bundle_schema(bundle, schemas)
        feature = bundle['feature']
        assignment = assignments[feature]['payload']
        require(bundle['evidence']['signer'] == assignment['producer'] and bundle['review']['signer'] == assignment['reviewer'], 'execution differs from independent assignment')
        qualified.append(journey.qualify_audited_feature(feature, bundle['evidence'], bundle['review'], bundle['journey'], bundle['audit'], audit_authority=audit_authority[feature], **arguments))
        calibration.append(calibrate_feature_evidence(bundle, qualification_args=arguments, required_traces=audit_authority[feature]['required_traces']))
    return {'version': 4, 'task': '4.50', 'scope': 'isolated_component', 'candidate': context['source'], 'bindings': {k: context[k] for k in sorted(BINDINGS)}, 'tool_sha256': readiness_authority['tool_sha256'], 'schemas_sha256': digest(schemas), 'run_id': context['run_id'], 'generated_at': now, 'feature_schema_review_mapping': mapped, 'isolated_feature_results': qualified, 'calibration': calibration, 'root_capacity_ledger_sha256': types.digest(types.canonical(workload.ledger())), 'root_mix_sha256': types.digest(types.canonical(workload.MIX)), 'validator_ready': True, 'final_integrated_evidence_state': 'pending', 'final_gate': '9.1', 'capacity_qualified': False, 'release_qualified': False}


def required_release_gates(spec_path):
    import tomllib
    specification = tomllib.loads(pathlib.Path(spec_path).read_text())
    gates = {}
    def visit(value, prefix=''):
        for name, task in value.items():
            task_id = prefix+'.'+name if prefix else name
            if type(task) is dict and 'verify_cmd' in task:
                gates[task_id] = {'command': task['verify_cmd'], 'requires': task.get('requires', []), 'kind': task.get('kind')}
            elif type(task) is dict:
                visit(task, task_id)
    visit(specification['task'])
    require('9.1' in gates and sum(g['kind'] == 'parallel_lane' for g in gates.values()) == 200, 'final mandatory gate inventory incomplete')
    return gates


def verify_integrated_gate(bundle, expected, admitted, *, context, trust, artifact_root, now, capture_sha256):
    keys(bundle, {'gate', 'evidence', 'review'}, 'integrated gate bundle')
    gate = bundle['gate']
    evidence, review = bundle['evidence'], bundle['review']
    keys(admitted, {'producer', 'reviewer', 'tool_sha256'}, 'admitted gate authority')
    producer, producer_principal = verify_signature(evidence, trust, 'gate_producer', 'OC25')
    reviewer, reviewer_principal = verify_signature(review, trust, 'gate_reviewer', 'OC25')
    require(producer == admitted['producer'] and reviewer == admitted['reviewer'], 'gate owner assignment mismatch')
    require(producer != reviewer and producer_principal != reviewer_principal and reviewer_principal not in context['implementation_principals'], 'self review conflict')
    payload = evidence['payload']
    keys(payload, {'version', 'gate', 'command', 'bindings', 'environment', 'run_id', 'tool_sha256', 'issued_at', 'expires_at', 'status', 'artifacts', 'result_artifact', 'capture_sha256'}, 'integrated gate evidence')
    require(payload['version'] == 4 and payload['gate'] == gate and payload['command'] == expected['command'], 'gate command/scope mismatch')
    require(payload['bindings'] == {k: context[k] for k in BINDINGS} and payload['environment'] == context['environment'], 'gate candidate/environment mismatch')
    require(text(payload['run_id']) and payload['tool_sha256'] == admitted['tool_sha256'] and type(admitted['tool_sha256']) is str and HEX.fullmatch(admitted['tool_sha256']), 'gate run/toolversion mismatch')
    require(payload['status'] == 'passed' and integer(payload['issued_at']) and integer(payload['expires_at']), 'nonpassing gate result')
    maximum = 86400 if gate == '9.1' else 7*86400
    require(payload['issued_at'] <= now < payload['expires_at'] <= payload['issued_at']+maximum, 'gate expired/future')
    require(payload['capture_sha256'] == (capture_sha256 if gate == '9.1' else None), 'gate capture binding mismatch')
    if gate == '9.1':
        require(payload['run_id'] == context['run_id'], 'final gate shared run mismatch')
    artifacts = verify_artifacts(payload['artifacts'], artifact_root, payload['issued_at'], now)
    path = payload['result_artifact']
    require(path in artifacts and artifacts[path][0] == 'native_result', 'actual integrated gate result missing')
    native = strict_json(artifacts[path][1])
    keys(native, {'gate', 'command', 'candidate', 'run_id', 'tool_sha256', 'tests', 'failed', 'skipped', 'exit_code', 'checks', 'log_artifact'}, 'native integrated gate result')
    require(all(native[k] == payload[k] for k in ['gate', 'command', 'run_id', 'tool_sha256']) and native['candidate'] == context['source'], 'native gate identity mismatch')
    require(integer(native['tests']) and native['tests'] > 0 and all(type(native[k]) is int and native[k] == 0 for k in ['failed', 'skipped', 'exit_code']), 'nonpassing/noop integrated gate')
    require(type(native['checks']) is list and len(native['checks']) > 0 and all(text(check) for check in native['checks']) and len(set(native['checks'])) == len(native['checks']), 'integrated gate checks absent')
    if gate == '9.1':
        require({'task-graph', 'integrated-mandatory-gates', 'final-shared-24h-soak', 'final-shared-surge', 'recovery-restore', 'complete-feature-evidence', 'operating-budget'}.issubset(native['checks']), 'final integrated mandatory checks absent')
    require(native['log_artifact'] in artifacts and artifacts[native['log_artifact']][0] == 'trace', 'actual gate execution log missing')
    r = review['payload']
    keys(r, {'version', 'gate', 'evidence_sha256', 'bindings', 'issued_at', 'expires_at', 'disposition', 'findings', 'artifact_hashes'}, 'integrated gate review')
    require(r['version'] == 4 and r['gate'] == gate and r['evidence_sha256'] == digest(evidence) and r['bindings'] == payload['bindings'], 'gate review scope/hash mismatch')
    require(integer(r['issued_at']) and integer(r['expires_at']) and payload['issued_at'] <= r['issued_at'] <= now < r['expires_at'] <= payload['expires_at'], 'gate review expiry')
    require(r['disposition'] == 'accepted' and type(r['findings']) is list and all(type(f) is dict and set(f) == {'id', 'disposition'} and text(f['id']) and f['disposition'] == 'resolved' for f in r['findings']), 'unresolved gate review findings')
    require(r['artifact_hashes'] == {a['path']: a['sha256'] for a in payload['artifacts']}, 'gate review artifact mismatch')
    for path, (_, raw) in artifacts.items():
        assert_secret_free(raw, path)
    return {'gate': gate, 'evidence_sha256': digest(evidence), 'review_sha256': digest(review)}


def final_release_closure(bundles, gate_bundles, *, closure_authority, readiness_authority, inventory, context, trust, artifact_root, dependencies, authority, audit_authority, now):
    import importlib.util
    keys(closure_authority, {'capture', 'gate_authority', 'gate_artifact_root', 'capture_acceptance', 'capture_acceptance_config', 'capture_acceptance_tool_sha256'}, 'final closure authority')
    require(type(gate_bundles) is list and any(type(bundle) is dict and bundle.get('gate') == '9.1' for bundle in gate_bundles), 'missing task9.1 final evidence')
    require(closure_authority['capture'] is not None, 'missing task9.1 final capture')
    _, capture, types, workload, bindings = root_capacity_references(readiness_authority, context)
    reference = closure_authority['capture']
    keys(reference, {'path', 'sha256'}, 'shared final capture reference')
    manifest = capture.read_manifest(reference['path'], reference['sha256'], bindings=bindings)
    require(manifest['scope'] == 'final_shared' and manifest['final_gate'] == '9.1' and manifest['run_id'] == context['run_id'], 'isolated/wrong final capture cannot close release')
    require(int(manifest['ended_utc_ns'])-int(manifest['started_utc_ns']) >= 86400*1000000000 and int(manifest['ended_utc_ns']) <= now*1000000000 and now*1000000000-int(manifest['ended_utc_ns']) <= 86400*1000000000, 'missing/stale final integrated24h interval')
    require(set(manifest['required_lane_ids']) == {f'L{i:02}' for i in range(1, 51)} - {'L48'}, 'final shared lane coverage incomplete')
    config = pinned_document(closure_authority['capture_acceptance_config'])
    keys(config, {'bindings', 'capture_manifest_path', 'capture_manifest_sha256', 'runtime_authority_path', 'budget_path', 'budget_sha256', 'scenario_proofs'}, 'shared acceptance invocation')
    require(config['bindings'] == authority_binding(bindings) and config['capture_manifest_path'] == reference['path'] and config['capture_manifest_sha256'] == reference['sha256'], 'shared acceptance invocation mismatch')
    acceptance = pinned_document(closure_authority['capture_acceptance'])
    require(canonical(capture.final_acceptance(config)) == canonical(acceptance), 'shared raw acceptance readout mismatch')
    keys(acceptance, {'schema_version', 'task', 'capture_manifest_sha256', 'bindings', 'run_id', 'scope', 'tool_sha256', 'workload_version', 'root_capacity_ledger_sha256', 'root_mix_sha256', 'scenarios', 'status', 'capacity_qualified', 'release_qualified'}, 'shared final acceptance')
    require(acceptance['schema_version'] == 1 and acceptance['task'] == '9.1' and acceptance['capture_manifest_sha256'] == reference['sha256'] and acceptance['bindings'] == authority_binding(bindings) and acceptance['run_id'] == context['run_id'] and acceptance['scope'] == 'final_shared', 'shared final acceptance binding mismatch')
    require(acceptance['tool_sha256'] == types.source_digest() == closure_authority['capture_acceptance_tool_sha256'] and type(acceptance['tool_sha256']) is str and HEX.fullmatch(acceptance['tool_sha256']), 'shared acceptance toolversion mismatch')
    require(acceptance['status'] == 'passed' and acceptance['capacity_qualified'] is False and acceptance['release_qualified'] is False and acceptance['workload_version'] == workload.VERSION and acceptance['root_capacity_ledger_sha256'] == types.digest(types.canonical(workload.ledger())) and acceptance['root_mix_sha256'] == types.digest(types.canonical(workload.MIX)), 'shared acceptance status/workload mismatch')
    require(type(acceptance['scenarios']) is dict and set(acceptance['scenarios']) == set(workload.final_scenarios()) and all(type(result) is dict and result.get('status') == 'passed' and type(result.get('raw_artifact_sha256')) is str and HEX.fullmatch(result['raw_artifact_sha256']) for result in acceptance['scenarios'].values()), 'shared mandatory scenario proof missing')
    captured_hashes = {artifact['sha256'] for artifact in manifest['artifacts']}
    require(all(result['raw_artifact_sha256'] in captured_hashes for result in acceptance['scenarios'].values()), 'shared scenario raw artifact absent')
    gates = required_release_gates(readiness_authority['spec']['path'])
    require(type(closure_authority['gate_authority']) is dict and set(closure_authority['gate_authority']) == set(gates), 'mandatory gate authority coverage incomplete')
    require(len(gate_bundles) == len(gates) and {b.get('gate') for b in gate_bundles if type(b) is dict} == set(gates), 'mandatory integrated gate evidence missing/duplicate')
    gate_results = [verify_integrated_gate(bundle, gates[bundle['gate']], closure_authority['gate_authority'][bundle['gate']], context=context, trust=trust, artifact_root=closure_authority['gate_artifact_root'], now=now, capture_sha256=reference['sha256']) for bundle in gate_bundles]
    final_evidence = next(bundle['evidence']['payload'] for bundle in gate_bundles if bundle['gate'] == '9.1')
    require(final_evidence['issued_at']*1000000000 >= int(manifest['ended_utc_ns']), 'final gate attestation predates actual soak')
    require({closure_authority['capture_acceptance']['sha256'], closure_authority['capture_acceptance_config']['sha256']}.issubset({a['sha256'] for a in final_evidence['artifacts']}), 'final9.1 signature does not bind actual shared acceptance/config')
    require(type(bundles) is list and len(bundles) == 55 and {b.get('feature') for b in bundles if type(b) is dict} == FEATURES, 'complete55 feature evidence required')
    spec = importlib.util.spec_from_file_location('feature_final_collector', pathlib.Path(__file__).with_name('collect_journeys.py'))
    collector = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(collector)
    features = collector.collect(bundles, inventory=inventory, context=context, trust=trust, artifact_root=artifact_root, dependencies=dependencies, authority=authority, audit_authority=audit_authority, now=now)
    require(features['complete_feature_policy_coverage'] is True and not features['blockers'], 'mandatory feature evidence blocked')
    return {'version': 4, 'final_gate': '9.1', 'candidate': context['source'], 'run_id': context['run_id'], 'capture_manifest_sha256': reference['sha256'], 'capture_acceptance_sha256': closure_authority['capture_acceptance']['sha256'], 'tool_sha256': feature_tool_digest(), 'gate_results': gate_results, 'features': features['features'], 'final_closure_validated': True, 'capacity_qualified': False, 'release_qualified': False}


def authority_binding(bindings):
    import dataclasses
    return dataclasses.asdict(bindings)
