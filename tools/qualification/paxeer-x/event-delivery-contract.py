#!/usr/bin/env python3
import argparse
import hashlib
import hmac
import http.client
import json
import os
from pathlib import Path
import re
import shutil
import socket
import ssl
import subprocess
import sys
import tempfile
import time
import tomllib
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[3]
sys.dont_write_bytecode = True


class Missing(Exception):
    pass


class Results:
    def __init__(self):
        self.passes = 0
        self.failures = 0

    def check(self, label, condition, detail=None):
        if condition:
            self.passes += 1
        else:
            self.failures += 1
        print(('PASS ' if condition else 'FAIL ') + label, flush=True)


def private_directory(path):
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    if path.is_symlink() or path.stat().st_uid != os.geteuid() or path.stat().st_mode & 0o077:
        raise Missing('evidence directory must be owner-only')
    return path


def run(arguments, environment=None, check=True, timeout=30):
    result = subprocess.run([str(value) for value in arguments], env=environment,
                            stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=timeout)
    if check and result.returncode:
        raise Missing(f'process refused: {Path(arguments[0]).name} exit={result.returncode}')
    return result


def secret(directory, name, value):
    path = directory / name
    with path.open('x') as stream:
        stream.write(value + '\n')
    path.chmod(0o600)
    return str(path)


def free_port():
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        return listener.getsockname()[1]


def ca_init(directory):
    directory = private_directory(directory)
    environment = dict(os.environ, LAYERX_CA_DIR=str(directory))
    run(['bash', ROOT / 'tools/bringup/ca.sh', 'init'], environment)
    run(['openssl', 'x509', '-in', directory / 'ca.pem', '-outform', 'DER', '-out', directory / 'ca.der'])
    return directory


def leaf(directory, ca, common_name, usage, names):
    directory = private_directory(directory)
    config = directory / 'leaf.cnf'
    config.write_text('basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\n'
                      + f'extendedKeyUsage={usage}\n' + (f'subjectAltName={names}\n' if names else ''))
    run(['openssl', 'req', '-new', '-newkey', 'rsa:2048', '-nodes', '-sha256',
         '-subj', '/CN=' + common_name, '-keyout', directory / 'key.pem', '-out', directory / 'request.pem'])
    run(['openssl', 'x509', '-req', '-in', directory / 'request.pem', '-CA', ca / 'ca.pem',
         '-CAkey', ca / 'ca.key', '-set_serial', str(int.from_bytes(os.urandom(16), 'big')),
         '-days', '1', '-sha256', '-extfile', config, '-out', directory / 'cert.pem'])
    return directory


def der(directory):
    run(['openssl', 'x509', '-in', directory / 'cert.pem', '-outform', 'DER', '-out', directory / 'cert.der'])
    run(['openssl', 'pkcs8', '-topk8', '-nocrypt', '-in', directory / 'key.pem', '-outform', 'DER',
         '-out', directory / 'key.der'])


def load_manifest(path):
    path = protected_input(path, 'candidate manifest')
    result = run([sys.executable, ROOT / 'tools/paxeer-x/candidate.py', 'validate', path,
                  '--repo', ROOT, '--spec', ROOT / 'spec/paxeer-x/spec.kvx'], check=False)
    if result.returncode:
        raise Missing('candidate manifest validation refused')
    document = json.loads(path.read_bytes())
    if document.get('schema') != 'paxeer-x.candidate.v1' or not document.get('services'):
        raise Missing('candidate manifest lacks services')
    return document, hashlib.sha256(path.read_bytes()).hexdigest()


def bind_binary(manifest, manifest_digest, binary):
    location = os.environ.get('PAXEER_X_EVENT_BINARY_ATTESTATION')
    if not location:
        raise Missing('PAXEER_X_EVENT_BINARY_ATTESTATION is required')
    attestation = json.loads(protected_input(location, 'binary attestation').read_bytes())
    expected = {'schema', 'candidate_manifest_sha256', 'source_revision', 'binary_sha256'}
    if not isinstance(attestation, dict) or set(attestation) != expected \
            or attestation['schema'] != 'paxeer-x.event-source-binary.v1' \
            or attestation['candidate_manifest_sha256'] != manifest_digest \
            or attestation['source_revision'] != manifest['source']['revision']:
        raise Missing('event source binary attestation does not bind the candidate manifest')
    with Path(binary).open('rb') as stream:
        digest = hashlib.file_digest(stream, 'sha256').hexdigest()
    if attestation['binary_sha256'] != digest:
        raise Missing('event source binary digest differs from attested artifact')
    print('candidate binary sha256=' + digest)


KINDS = ('journeys', 'approvals', 'payments', 'programs')
SINGULAR = {'journeys': 'journey', 'approvals': 'approval', 'payments': 'payment', 'programs': 'program'}
UPSTREAM_VARIABLES = ('LAYERX_EVENTS_UPSTREAM_TOKEN_FILE', 'LAYERX_EVENTS_UPSTREAM_CLIENT_IDENTITY_PKCS12',
                      'LAYERX_EVENTS_UPSTREAM_CLIENT_IDENTITY_PASSWORD_FILE', 'LAYERX_EVENTS_UPSTREAM_COOKIE_FILE')
ENROLLMENT_BOUND = 1_048_576
REFUSED_LINE = re.compile(r'^layerx-event-source: enrollment generation (\d+) kept, snapshot refused: (\S+)$',
                          re.MULTILINE)
ADOPTED_LINE = re.compile(r'^layerx-event-source: enrollment generation (\d+) adopted with (\d+) principals$',
                          re.MULTILINE)


def event_id(kind, resource, sequence):
    data = b''
    for part in (kind.encode(), resource.encode(), sequence.to_bytes(8, 'big')):
        data += len(part).to_bytes(8, 'big') + part
    return hashlib.sha256(data).hexdigest()


def protected_input(path, label):
    path = Path(path)
    if not path.is_absolute() or path.is_symlink() or not path.is_file():
        raise Missing(f'{label} is not an absolute regular file')
    status = path.stat()
    if status.st_uid != os.geteuid() or status.st_mode & 0o077:
        raise Missing(f'{label} is not owned by the harness user with mode 0600')
    return path


def upstream_inputs():
    """The real upstream of every source role and principal credentials it
    issued, named by PAXEER_X_EVENT_UPSTREAMS. Nothing is synthesized: a
    missing role, principal, credential or rotated credential is refused."""
    location = os.environ.get('PAXEER_X_EVENT_UPSTREAMS')
    if not location:
        raise Missing('PAXEER_X_EVENT_UPSTREAMS is required: the real upstream of every source role')
    try:
        document = json.loads(protected_input(location, 'PAXEER_X_EVENT_UPSTREAMS').read_bytes())
    except ValueError:
        raise Missing('PAXEER_X_EVENT_UPSTREAMS is not JSON') from None
    if not isinstance(document, dict) or sorted(document) != sorted(KINDS):
        raise Missing(f'PAXEER_X_EVENT_UPSTREAMS must name exactly {", ".join(KINDS)}')
    roles = {}
    for kind in KINDS:
        entry = document[kind]
        if not isinstance(entry, dict) or not str(entry.get('url', '')).startswith('https://'):
            raise Missing(f'{kind}: upstream url is required')
        origin = urlsplit(entry['url'])
        if origin.scheme != 'https' or not origin.hostname or origin.username or origin.password \
                or origin.query or origin.fragment or origin.path not in ('', '/'):
            raise Missing(f'{kind}: upstream must be an HTTPS origin')
        environment = entry.get('environment', {})
        if not isinstance(environment, dict) or any(name not in UPSTREAM_VARIABLES for name in environment):
            raise Missing(f'{kind}: upstream environment names an undeclared variable')
        for name, value in environment.items():
            protected_input(value, f'{kind} {name}')
        principals = entry.get('principals')
        if not isinstance(principals, list) or len(principals) != 3:
            raise Missing(f'{kind}: exactly three upstream-issued principals are required')
        credentials = []
        for index, principal in enumerate(principals):
            names = ['credential_file'] + (['rotated_credential_file'] if index == 0 else [])
            if not isinstance(principal, dict) or not isinstance(principal.get('principal'), str) \
                    or any(not isinstance(principal.get(name), str) for name in names):
                raise Missing(f'{kind}: principal {index} lacks {" or ".join(names)}')
            for name in names:
                value = protected_input(principal[name], f'{kind} principal {index} {name}').read_bytes()
                stripped = value.rstrip(b'\r\n')
                if not stripped or stripped != stripped.strip() or len(stripped) > 4096:
                    raise Missing(f'{kind}: principal {index} {name} is not a bounded credential')
                credentials.append(stripped)
        if len(set(credentials)) != len(credentials) or len({p['principal'] for p in principals}) != 3:
            raise Missing(f'{kind}: principals and credentials must be distinct')
        roles[kind] = {'url': entry['url'], 'ca_der': str(protected_input(entry.get('ca_der', ''), f'{kind} ca_der')),
                       'environment': environment, 'principals': principals}
    return roles


class Enrollment:
    """Writes signed v1 enrollment snapshots the way an enrollment authority
    does: credential files and the snapshot are owned by the service user,
    mode 0600, and replaced by rename."""

    def __init__(self, work, kind, key):
        self.directory = private_directory(work / 'enrollment')
        self.kind = kind
        self.key = key
        self.path = self.directory / 'credentials.json'
        self.serial = 0

    def credential(self, value, mode=0o600):
        self.serial += 1
        path = self.directory / f'credential-{self.serial}'
        path.write_bytes(value + b'\n')
        path.chmod(mode)
        return path

    def document(self, generation, entries, key=None):
        message = f'layerx-enrollment-v1\n{self.kind}\n{generation}\n'.encode()
        for principal, path in entries:
            content = Path(path).read_bytes().rstrip(b'\r\n') if Path(path).exists() else b''
            message += principal.encode() + b'\n' + hashlib.sha256(content).hexdigest().encode() + b'\n'
        mac = hmac.new(key or self.key, message, hashlib.sha256).hexdigest()
        return {'version': 1, 'generation': generation,
                'principals': [{'principal': principal, 'credential_file': str(path)} for principal, path in entries],
                'mac': mac}

    def write(self, data):
        staged = self.directory / 'credentials.json.new'
        staged.write_bytes(data if isinstance(data, bytes) else json.dumps(data).encode())
        staged.chmod(0o600)
        staged.replace(self.path)


class EventSource:
    def __init__(self, binary, environment, log):
        self.port = int(environment['LAYERX_EVENTS_LISTEN'].rsplit(':', 1)[1])
        self.logfile = log
        self.log = log.open('ab')
        self.child = subprocess.Popen([binary], env=environment, stdin=subprocess.DEVNULL,
                                      stdout=self.log, stderr=self.log)
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            if self.child.poll() is not None:
                self.log.close()
                raise Missing(f'layerx-event-source exited {self.child.returncode} before listening')
            try:
                with socket.create_connection(('127.0.0.1', self.port), timeout=1):
                    return
            except OSError:
                time.sleep(0.1)
        self.stop()
        raise Missing('layerx-event-source never listened')

    def refusals(self):
        return REFUSED_LINE.findall(self.logfile.read_text(errors='replace'))

    def adoptions(self):
        return ADOPTED_LINE.findall(self.logfile.read_text(errors='replace'))

    def stop(self):
        if self.child.poll() is None:
            self.child.terminate()
            try:
                self.child.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.child.kill()
                self.child.wait()
        if not self.log.closed:
            self.log.close()


class Source:
    """One running source of one role with the TLS client identity, source
    token and producer token it is configured with."""

    def __init__(self, server_ca, client, token, producer, responses):
        self.server_ca, self.client, self.token, self.producer = server_ca, client, token, producer
        self.responses = responses
        self.process = None

    def call(self, method, path, bearer=None, body=None):
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
        context.load_verify_locations(str(self.server_ca))
        context.load_cert_chain(str(self.client / 'cert.pem'), str(self.client / 'key.pem'))
        connection = http.client.HTTPSConnection('localhost', self.process.port, context=context, timeout=30)
        headers = {'Content-Type': 'application/json'} if body is not None else {}
        if bearer is not None:
            headers['Authorization'] = f'Bearer {bearer}'
        try:
            connection.request(method, path, body=body, headers=headers)
            response = connection.getresponse()
            raw = response.read()
        except (ssl.SSLError, ConnectionError, socket.timeout) as error:
            return ('refused', type(error).__name__, {})
        finally:
            connection.close()
        self.responses.append(raw)
        try:
            document = json.loads(raw)
        except ValueError:
            document = {}
        if not isinstance(document, dict):
            document = {}
        code = document['error'].get('code') if isinstance(document.get('error'), dict) else None
        return (response.status, code, document)

    def ready(self):
        status, _, document = self.call('GET', '/readyz')
        return status, document

    def await_ready(self, predicate, seconds=30):
        """Polls /readyz until the observed state satisfies predicate; the
        returned state, not the wait, is what the caller asserts on."""
        deadline = time.monotonic() + seconds
        observed = self.ready()
        while not predicate(*observed) and time.monotonic() < deadline:
            time.sleep(0.25)
            observed = self.ready()
        return observed

    def await_log(self, read, wanted, seconds=30):
        deadline = time.monotonic() + seconds
        lines = read()
        while wanted not in lines and time.monotonic() < deadline:
            time.sleep(0.25)
            lines = read()
        return wanted in lines

    def produce(self, body):
        return self.call('POST', '/internal/v1/observe', self.producer, body)

    def event(self, identifier):
        return self.call('GET', f'/internal/v1/events/{identifier}', self.token)


def observation(kind, principal, resource, sequence):
    singular = SINGULAR[kind]
    document = {'kind': singular, 'id': event_id(singular, resource, sequence), 'principal': principal,
                'resource': resource, 'sequence': sequence, 'source_sequence': sequence,
                'occurred_at': 1_790_000_000 + sequence, 'facts': [{'name': 'state', 'value': f'step-{sequence}'}]}
    if kind == 'payments':
        document.update({'activity_id': resource, 'amount': str(1000 + sequence), 'asset': '0' * 63 + '1'})
    return json.dumps(document, separators=(',', ':')).encode()


def resource_name(kind, label):
    return hashlib.sha256(f'{kind}:{label}'.encode()).hexdigest() if kind == 'payments' else f'{label}-resource'


def source_environment(kind, role, work, server, ca, listen, token_file, producers_file, key_file, enrollment):
    environment = {
        'PATH': os.environ.get('PATH', '/usr/bin:/bin'),
        'LAYERX_EVENTS_LISTEN': f'127.0.0.1:{listen}',
        'LAYERX_EVENTS_KIND': kind,
        'LAYERX_EVENTS_STATE_DIR': str(work / 'state'),
        'LAYERX_EVENTS_TOKEN_FILE': token_file,
        'LAYERX_EVENTS_TLS_CERT_DER': str(server / 'cert.der'),
        'LAYERX_EVENTS_TLS_KEY_DER': str(server / 'key.der'),
        'LAYERX_EVENTS_CLIENT_CA_DER': str(ca / 'ca.der'),
        'LAYERX_EVENTS_UPSTREAM_URL': role['url'],
        'LAYERX_EVENTS_UPSTREAM_CA_DER': role['ca_der'],
        'LAYERX_EVENTS_PRODUCERS_FILE': producers_file,
        'LAYERX_EVENTS_CREDENTIALS_FILE': str(enrollment.path),
        'LAYERX_EVENTS_ENROLLMENT_KEY_FILE': key_file,
    }
    environment.update(role['environment'])
    return environment


def refused_update(results, source, label, data, code, generation, served, refusals):
    """Writes one refused update and asserts the refusal code /readyz names,
    the unchanged adopted generation and that the last valid snapshot is
    still served. Consecutive updates carry different codes, so a changed
    last_refusal identifies this update's refusal."""
    previous = source.ready()[1].get('last_refusal')
    if previous == code:
        raise Missing(f'{label}: consecutive refusals share {code}')
    source.enrollment.write(data)
    status, state = source.await_ready(lambda s, d: d.get('last_refusal') != previous)
    results.check(f'{label}: refused with {code}', state.get('last_refusal') == code, state.get('last_refusal'))
    results.check(f'{label}: /readyz keeps generation {generation} ready',
                  status == 200 and state.get('ready') is True and state.get('generation') == generation,
                  (status, state))
    observed = source.event(served['id'])
    results.check(f'{label}: the last valid snapshot still serves its events',
                  observed[0] == 200 and observed[2] == served, observed[:2])
    refusals.append((label, code, status))
    print(f'REFUSAL {label} code={state.get("last_refusal")} readyz={status}')


def credential_lifecycle(results, binary, kind, role, work, ca, server, client, secrets, responses):
    label = kind
    p0, p1, p2 = (entry['principal'] for entry in role['principals'])
    key = os.urandom(32).hex().encode()
    secrets.append(key)
    key_file = secret(work, 'enrollment-key', key.decode())
    token, producer = os.urandom(24).hex(), os.urandom(24).hex()
    secrets.extend([token.encode(), producer.encode()])
    token_file = secret(work, 'source-token', token)
    producer_file = secret(work, 'producer-token', producer)
    producers_file = secret(work, 'producers.json', json.dumps(
        [{'token_file': producer_file, 'allow_principal_digest': kind in ('payments', 'programs')}]))
    private_directory(work / 'state')
    logs = private_directory(work / 'logs')
    enrollment = Enrollment(work, kind, key)
    original = {}
    for index, entry in enumerate(role['principals']):
        for name in ('credential_file', 'rotated_credential_file'):
            if name in entry:
                value = Path(entry[name]).read_bytes().rstrip(b'\r\n')
                secrets.append(value)
                original[(index, name)] = enrollment.credential(value)
    c0, c1, c2 = (original[(index, 'credential_file')] for index in range(3))
    c0_rotated = original[(0, 'rotated_credential_file')]
    environment = source_environment(kind, role, work, server, ca, free_port(), token_file, producers_file,
                                     key_file, enrollment)
    signed = run([binary, '--empty-enrollment-mac'], environment)
    expected_mac = enrollment.document(0, [])['mac']
    results.check(f'{label}: initializer signs the canonical empty enrollment without secret arguments',
                  signed.stdout.strip() == expected_mac)
    responses.extend([signed.stdout.encode(), signed.stderr.encode()])
    source = Source(server / 'ca.pem', client, token, producer, responses)
    source.enrollment = enrollment

    unkeyed = run([binary], {name: value for name, value in environment.items()
                             if name != 'LAYERX_EVENTS_ENROLLMENT_KEY_FILE'}, check=False, timeout=30)
    results.check(f'{label}: a source without the enrollment key refuses to start',
                  unkeyed.returncode != 0 and 'LAYERX_EVENTS_ENROLLMENT_KEY_FILE' in unkeyed.stderr,
                  f'exit={unkeyed.returncode}')

    source.process = EventSource(binary, environment, logs / 'source-1.log')
    pid = source.process.child.pid
    try:
        status, state = source.ready()
        results.check(f'{label}: with no snapshot the source is not ready and waits for principals',
                      status == 503 and state.get('ready') is False and state.get('state') == 'waiting-principals',
                      (status, state))
        observed = source.call('GET', '/livez')
        results.check(f'{label}: with no snapshot the source is live', observed[0] == 200, observed[:2])

        enrollment.write(enrollment.document(1, []))
        results.check(f'{label}: the empty generation 1 is adopted',
                      source.await_log(source.process.adoptions, ('1', '0')), source.process.adoptions())
        enrollment.write(enrollment.document(1, [(p0, c0)]))
        results.check(f'{label}: a different generation 1 conflicts with the adopted empty one',
                      source.await_log(source.process.refusals, ('1', 'enrollment_generation_conflict')),
                      source.process.refusals())
        status, state = source.ready()
        results.check(f'{label}: empty valid enrollment is live but not ready',
                      status == 503 and state.get('ready') is False and state.get('state') == 'waiting-principals',
                      (status, state))
        observed = source.call('GET', '/livez')
        results.check(f'{label}: empty valid enrollment stays live', observed[0] == 200, observed[:2])
        r0, r1 = resource_name(kind, 'first'), resource_name(kind, 'second')
        first = observation(kind, p0, r0, 1)
        observed = source.produce(first)
        results.check(f'{label}: empty enrollment refuses first-event acceptance',
                      observed[0] == 503, observed[:2])

        enrollment.write(enrollment.document(2, [(p0, c0), (p1, c1)]))
        status, state = source.await_ready(lambda s, d: d.get('generation') == 2)
        results.check(f'{label}: generation 2 enrolls two upstream-bound principals without a restart',
                      status == 200 and state.get('ready') is True and state.get('principals') == 2
                      and state.get('last_refusal') is None and source.process.child.poll() is None
                      and source.process.child.pid == pid, (status, state))
        observed = source.produce(first)
        results.check(f'{label}: an enrolled principal publishes its first event',
                      observed[0] == 200 and observed[2].get('principal') == p0
                      and observed[2].get('id') == event_id(SINGULAR[kind], r0, 1), observed[:2])
        event0 = observed[2]
        second = observation(kind, p1, r1, 1)
        observed = source.produce(second)
        results.check(f'{label}: the second principal publishes its own event',
                      observed[0] == 200 and observed[2].get('principal') == p1, observed[:2])
        event1 = observed[2]
        observed = source.event(event0.get('id', ''))
        results.check(f'{label}: the stored event is served with its canonical identity',
                      observed[0] == 200 and observed[2] == event0, observed[:2])

        refusals = []
        refused_update(results, source, f'{label} malformed JSON', b'{"version":1,', 'enrollment_malformed', 2,
                       event0, refusals)
        oversized = json.dumps(enrollment.document(3, [(p0, c0), (p1, c1)])).encode()
        oversized = oversized[:-1] + b',"padding":"' + b'0' * ENROLLMENT_BOUND + b'"}'
        refused_update(results, source, f'{label} oversized', oversized, 'enrollment_oversized', 2, event0,
                       refusals)
        refused_update(results, source, f'{label} legacy flat map', {p0: str(c0)}, 'enrollment_malformed', 2,
                       event0, refusals)
        refused_update(results, source, f'{label} duplicate principal',
                       enrollment.document(3, [(p0, c0), (p0, c0), (p1, c1)]), 'enrollment_duplicate', 2,
                       event0, refusals)
        unknown = enrollment.document(3, [(p0, c0), (p1, c1)])
        unknown['extra'] = True
        refused_update(results, source, f'{label} unknown field', unknown, 'enrollment_malformed', 2, event0,
                       refusals)
        refused_update(results, source, f'{label} foreign key',
                       enrollment.document(3, [(p0, c0), (p1, c1)], key=os.urandom(32).hex().encode()),
                       'enrollment_unauthenticated', 2, event0, refusals)
        refused_update(results, source, f'{label} mismatched principal',
                       enrollment.document(3, [(p0, c2), (p1, c1)]), 'enrollment_principal_mismatch', 2,
                       event0, refusals)
        tampered = enrollment.document(3, [(p0, c0), (p1, c1)])
        tampered['principals'][1]['credential_file'] = str(c2)
        refused_update(results, source, f'{label} entry changed after signing', tampered,
                       'enrollment_unauthenticated', 2, event0, refusals)
        open_credential = enrollment.credential(c0_rotated.read_bytes().rstrip(b'\r\n'), mode=0o644)
        refused_update(results, source, f'{label} group-readable credential',
                       enrollment.document(3, [(p0, open_credential), (p1, c1)]), 'enrollment_unprotected', 2,
                       event0, refusals)
        open_credential.unlink()
        refused_update(results, source, f'{label} absent credential',
                       enrollment.document(3, [(p0, enrollment.directory / 'absent'), (p1, c1)]),
                       'enrollment_credential_unreadable', 2, event0, refusals)
        refused_update(results, source, f'{label} stale generation', enrollment.document(1, [(p1, c1)]),
                       'enrollment_stale_generation', 2, event0, refusals)
        refused_update(results, source, f'{label} conflicting generation', enrollment.document(2, [(p1, c1)]),
                       'enrollment_generation_conflict', 2, event0, refusals)
        results.check(f'{label}: every refused update was recorded with a nonzero code',
                      len(refusals) == 12 and all(code for _, code, _ in refusals), len(refusals))

        linked = enrollment.directory / 'credential-link'
        linked.symlink_to(c0_rotated)
        refused_update(results, source, f'{label} symlinked credential',
                       enrollment.document(3, [(p0, linked), (p1, c1)]),
                       'enrollment_unprotected', 2, event0, refusals)
        linked.unlink()
        too_many = {'version': 1, 'generation': 3, 'mac': '0' * 64,
                    'principals': [{'principal': 'p', 'credential_file': '/c'}] * 10_001}
        refused_update(results, source, f'{label} excessive principal count', too_many,
                       'enrollment_oversized', 2, event0, refusals)
        os.link(c0_rotated, linked)
        refused_update(results, source, f'{label} hard-linked credential',
                       enrollment.document(3, [(p0, linked), (p1, c1)]),
                       'enrollment_unprotected', 2, event0, refusals)
        linked.unlink()
        oversized_credential = enrollment.credential(b'x' * 4097)
        refused_update(results, source, f'{label} oversized credential',
                       enrollment.document(3, [(p0, oversized_credential), (p1, c1)]),
                       'enrollment_credential_unreadable', 2, event0, refusals)
        oversized_credential.unlink()

        enrollment.write(enrollment.document(3, [(p0, c0_rotated), (p1, c1)]))
        status, state = source.await_ready(lambda s, d: d.get('generation') == 3)
        results.check(f'{label}: generation 3 rotates one principal while the other is unchanged, no restart',
                      status == 200 and state.get('principals') == 2 and source.process.child.pid == pid
                      and source.process.child.poll() is None, (status, state))
        observed = source.event(event0['id'])
        results.check(f'{label}: the rotated principal still reads its own stored event',
                      observed[0] == 200 and observed[2] == event0, observed[:2])
        observed = source.event(event1['id'])
        results.check(f'{label}: the unchanged principal keeps its stored event',
                      observed[0] == 200 and observed[2] == event1, observed[:2])
        after_rotation = observation(kind, p0, r0, 2)
        observed = source.produce(after_rotation)
        results.check(f'{label}: the rotated credential binds the next event of its own principal',
                      observed[0] == 200 and observed[2].get('principal') == p0, observed[:2])
        event0b = observed[2]
        refused_update(results, source, f'{label} retired credential reassigned to another principal',
                       enrollment.document(4, [(p0, c0_rotated), (p1, c1), (p2, c0)]),
                       'enrollment_credential_reused', 3, event0, refusals)
        refused_update(results, source, f'{label} conflicting generation 3', enrollment.document(3, [(p1, c1)]),
                       'enrollment_generation_conflict', 3, event0, refusals)
        refused_update(results, source, f'{label} live credential shared by two principals',
                       enrollment.document(4, [(p0, c0_rotated), (p1, c0_rotated)]),
                       'enrollment_credential_reused', 3, event0, refusals)

        enrollment.write(enrollment.document(5, [(p1, c1)]))
        status, state = source.await_ready(lambda s, d: d.get('generation') == 5)
        results.check(f'{label}: generation 5 removes a principal without a restart',
                      status == 200 and state.get('principals') == 1 and source.process.child.pid == pid,
                      (status, state))
        for record in (event0, event0b):
            observed = source.event(record['id'])
            results.check(f'{label}: a removed principal\'s stored event is not disclosed',
                          observed[:2] == (404, 'event_not_found'), observed[:2])
        observed = source.produce(observation(kind, p0, r0, 3))
        results.check(f'{label}: a removed principal cannot publish at the effective generation',
                      observed[0] in (403, 503), observed[:2])
        observed = source.call('POST', '/internal/v1/observe', token,
                               json.dumps({'principal': p0, 'resource': r0}).encode())
        results.check(f'{label}: a removed principal cannot be observed with the source token',
                      observed[0] in (403, 503), observed[:2])
        observed = source.produce(observation(kind, p1, r0, 1))
        results.check(f'{label}: a remaining principal cannot publish another principal\'s event identity',
                      observed[0] in (403, 409), observed[:2])
        observed = source.event(event1['id'])
        results.check(f'{label}: the remaining principal keeps its event across removal',
                      observed[0] == 200 and observed[2] == event1, observed[:2])
        kept = set(source.process.refusals())
        results.check(f'{label}: every distinct refusal code is logged with the kept generation',
                      {code for _, code, _ in refusals} <= {code for _, code in kept}
                      and all(generation in ('1', '2', '3') for generation, _ in kept), sorted(kept))
        results.check(f'{label}: adoptions of generations 2, 3 and 5 are logged with their principal counts',
                      {('2', '2'), ('3', '2'), ('5', '1')} <= set(source.process.adoptions()),
                      source.process.adoptions())
        source.process.stop()

        persisted = work / 'state' / 'enrollment.json'
        results.check(f'{label}: the adopted generation is persisted owner-only',
                      persisted.is_file() and not persisted.is_symlink()
                      and persisted.stat().st_mode & 0o077 == 0, persisted.exists())
        source.process = EventSource(binary, environment, logs / 'source-2.log')
        status, state = source.await_ready(lambda s, d: s == 200 and d.get('generation') == 5)
        results.check(f'{label}: restart replays generation 5 and is ready',
                      status == 200 and state.get('principals') == 1, (status, state))
        observed = source.event(event1['id'])
        results.check(f'{label}: restart keeps the canonical identity of a pending event',
                      observed[0] == 200 and observed[2] == event1, observed[:2])
        observed = source.produce(second)
        results.check(f'{label}: an authorized credential rebinds the identical observation to the same record',
                      observed[0] == 200 and observed[2] == event1, observed[:2])
        observed = source.event(event0['id'])
        results.check(f'{label}: restart does not disclose the removed principal\'s event',
                      observed[:2] == (404, 'event_not_found'), observed[:2])
        observed = source.produce(first)
        results.check(f'{label}: restart does not let the removed principal rebind its event',
                      observed[0] in (403, 503), observed[:2])
        enrollment.write(enrollment.document(6, [(p0, c0_rotated), (p1, c1)]))
        status, state = source.await_ready(lambda s, d: d.get('generation') == 6)
        results.check(f'{label}: re-enrollment of the rotated credential is adopted',
                      status == 200 and state.get('principals') == 2, (status, state))
        for record in (event0, event0b):
            observed = source.event(record['id'])
            results.check(f'{label}: the re-enrolled principal reads its event with its canonical identity',
                          observed[0] == 200 and observed[2] == record, observed[:2])
        source.process.stop()

        enrollment.write(enrollment.document(4, [(p0, c0_rotated), (p1, c1)]))
        source.process = EventSource(binary, environment, logs / 'source-3.log')
        status, state = source.await_ready(lambda s, d: d.get('last_refusal') == 'enrollment_stale_generation')
        results.check(f'{label}: a restart with a snapshot below the persisted generation stays not ready',
                      status == 503 and state.get('ready') is False
                      and state.get('last_refusal') == 'enrollment_stale_generation', (status, state))
        restarted = source.process.child.pid
        enrollment.write(enrollment.document(7, [(p0, c0_rotated), (p1, c1)]))
        status, state = source.await_ready(lambda s, d: d.get('generation') == 7)
        results.check(f'{label}: a newer valid snapshot makes the restarted source ready without a restart',
                      status == 200 and state.get('ready') is True and source.process.child.pid == restarted,
                      (status, state))
        source.process.stop()

        enrollment.write(enrollment.document(7, [(p1, c1)]))
        source.process = EventSource(binary, environment, logs / 'source-4.log')
        status, state = source.await_ready(
            lambda s, d: d.get('last_refusal') == 'enrollment_generation_conflict')
        results.check(f'{label}: a restart with the persisted generation but other fingerprints is refused',
                      status == 503 and state.get('ready') is False, (status, state))
        enrollment.write(enrollment.document(8, [(p0, c0_rotated), (p1, c1)]))
        status, state = source.await_ready(lambda s, d: d.get('generation') == 8)
        results.check(f'{label}: generation 8 restores readiness', status == 200, (status, state))
        observed = source.event(event0['id'])
        results.check(f'{label}: replayed ownership serves the first event to its principal',
                      observed[0] == 200 and observed[2] == event0, observed[:2])
        saved_state = persisted.with_suffix('.saved')
        persisted.rename(saved_state)
        persisted.mkdir(mode=0o700)
        try:
            refused_update(results, source, f'{label} persistence failure',
                           enrollment.document(9, [(p0, c0_rotated), (p1, c1)]),
                           'enrollment_unavailable', 8, event0, refusals)
        finally:
            source.process.stop()
            persisted.rmdir()
            saved_state.rename(persisted)
        enrollment.write(enrollment.document(8, [(p0, c0_rotated), (p1, c1)]))
        unreachable = dict(environment, LAYERX_EVENTS_UPSTREAM_URL=f'https://localhost:{free_port()}')
        source.process = EventSource(binary, unreachable, logs / 'source-unavailable.log')
        status, state = source.await_ready(
            lambda s, d: d.get('last_refusal') == 'enrollment_upstream_unavailable')
        results.check(f'{label}: restart requires upstream readmission of unchanged persisted credentials',
                      status == 503 and state.get('principals') == 0
                      and state.get('last_refusal') == 'enrollment_upstream_unavailable')
        observed = source.event(event0['id'])
        results.check(f'{label}: unavailable restart cannot disclose persisted events', observed[0] == 503)
        observed = source.produce(first)
        results.check(f'{label}: unavailable restart cannot rebind pending events', observed[0] == 503)
        source.process.stop()
        source.process = EventSource(binary, environment, logs / 'source-recovered.log')
        status, state = source.await_ready(lambda s, d: s == 200 and d.get('generation') == 8)
        results.check(f'{label}: upstream recovery readmits the persisted generation', status == 200)
        observed = source.event(event0['id'])
        results.check(f'{label}: upstream recovery preserves canonical event ownership',
                      observed[0] == 200 and observed[2] == event0)
    finally:
        source.process.stop()
    log_text = ''.join(path.read_text(errors='replace') for path in sorted(logs.iterdir()))
    results.check(f'{label}: refusal lines carry only a generation and a code',
                  all(code.startswith('enrollment_') and code.replace('_', '').isalpha()
                      for _, code in REFUSED_LINE.findall(log_text)), 'log')
    return enrollment.directory


def secret_scan(results, work, secrets, responses, exclude):
    """Scans every log, response body and generated artifact for the raw and
    unkeyed-digest form of every generated or issued secret."""
    needles = set()
    for value in secrets:
        needles.add(value)
        needles.add(hashlib.sha256(value).hexdigest().encode())
    leaked = []
    scanned = 0
    for path in sorted(work.rglob('*')):
        if not path.is_file() or path.is_symlink() or any(parent in exclude for parent in path.parents):
            continue
        if path.name in ('enrollment-key', 'source-token', 'producer-token', 'producers.json') \
                or path.suffix in ('.pem', '.der', '.srl', '.cnf', '.p12') or path.name in ('password',):
            continue
        data = path.read_bytes()
        scanned += 1
        leaked.extend(path.relative_to(work).as_posix() for needle in needles if needle in data)
    for index, body in enumerate(responses):
        scanned += 1
        leaked.extend(f'response {index}' for needle in needles if needle in body)
    results.check('no protected credential bytes in logs, responses or generated artifacts',
                  scanned > 0 and not leaked, sorted(set(leaked)))
    print(f'scanned artifacts={scanned} secrets={len(secrets)}')


def fly_init_enrollment(results):
    init = (ROOT / 'platform/hosted/internal/fly-init.sh').read_text()
    results.check('fly init exports the enrollment key of every source group',
                  'LAYERX_EVENTS_ENROLLMENT_KEY_FILE=' in init, 'fly-init.sh')
    results.check('fly init no longer writes a legacy flat credential map',
                  "printf '{}\\n' >\"$run_dir/credentials.json\"" not in init, 'fly-init.sh')


def principal_credential_lifecycle(manifest, results):
    directory = os.environ.get('PAXEER_X_HOSTED_BIN_DIR')
    if not directory:
        raise Missing('PAXEER_X_HOSTED_BIN_DIR is required')
    binary = str(Path(directory) / 'layerx-event-source')
    if not os.access(binary, os.X_OK):
        raise Missing(f'layerx-event-source binary absent at {binary}')
    for tool in ('openssl', 'bash'):
        if shutil.which(tool) is None:
            raise Missing(f'{tool} is required')
    bind_binary(manifest, MANIFEST_DIGEST, binary)
    roles = upstream_inputs()
    print(f'candidate schema={manifest["schema"]} services={len(manifest["services"])}')
    fly_init_enrollment(results)
    evidence = os.environ.get('PAXEER_X_EVIDENCE_DIR')
    if not evidence:
        raise Missing('PAXEER_X_EVIDENCE_DIR is required')
    work = Path(tempfile.mkdtemp(prefix='principal-credential-lifecycle-',
                                dir=private_directory(Path(evidence))))
    work.chmod(0o700)
    print('evidence=' + str(work))
    ca = ca_init(work / 'ca')
    server = leaf(work / 'server', ca, 'localhost', 'serverAuth', 'DNS:localhost')
    der(server)
    shutil.copy(ca / 'ca.pem', server / 'ca.pem')
    client = leaf(work / 'client', ca, 'layerx-event-client', 'clientAuth', '')
    secrets, responses, completed = [], [], []
    for kind in KINDS:
        credential_lifecycle(results, binary, kind, roles[kind], private_directory(work / kind), ca, server,
                             client, secrets, responses)
        completed.append(kind)
    results.check('all four source roles completed the lifecycle', completed == list(KINDS), completed)
    secret_scan(results, work, secrets, responses,
                {work / 'ca', work / 'server', work / 'client'} | {work / kind / 'enrollment' for kind in KINDS})


class FairWorker:
    def __init__(self, binary, environment, log):
        self.log = log.open('ab')
        self.child = subprocess.Popen([binary], env=environment, stdin=subprocess.DEVNULL,
                                      stdout=self.log, stderr=self.log)

    def stop(self):
        if self.child.poll() is None:
            self.child.terminate()
            try:
                self.child.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.child.kill()
                self.child.wait(timeout=10)
        self.log.close()


def fair_status(path, principal, operation='status'):
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
        connection.settimeout(5)
        connection.connect(str(path))
        connection.sendall(json.dumps({'operation': operation, 'principal': principal}).encode())
        connection.shutdown(socket.SHUT_WR)
        chunks = bytearray()
        while len(chunks) <= 131072:
            block = connection.recv(16384)
            if not block:
                break
            chunks.extend(block)
        if len(chunks) > 131072:
            raise Missing('Human event status exceeds its bound')
        value = json.loads(chunks)
    if not isinstance(value, dict) or value.get('principal') != principal or 'delivery' not in value:
        raise Missing('Human event status refused')
    return value


def fair_wait(predicate, seconds, worker):
    deadline = time.monotonic() + seconds
    while True:
        if worker.child.poll() is not None:
            raise Missing('Human worker exited during fairness case')
        try:
            result = predicate()
            if result:
                return result
        except (OSError, ValueError):
            pass
        if time.monotonic() >= deadline:
            raise Missing('Human fairness progress exceeded its declared bound')
        time.sleep(0.2)


def fair_http(endpoint, request):
    origin = urlsplit(endpoint['url'])
    if origin.scheme != 'https' or not origin.hostname or origin.username or origin.password:
        raise Missing('fairness endpoint must be an authenticated HTTPS origin')
    context = ssl.create_default_context(cafile=str(protected_input(endpoint['ca_pem'], 'endpoint CA')))
    if 'client_cert_pem' in endpoint or 'client_key_pem' in endpoint:
        context.load_cert_chain(str(protected_input(endpoint['client_cert_pem'], 'client certificate')),
                                str(protected_input(endpoint['client_key_pem'], 'client key')))
    headers = {}
    for name, path in endpoint.get('header_files', {}).items():
        value = protected_input(path, 'request credential').read_bytes().rstrip(b'\r\n').decode()
        if any(ord(char) < 32 or ord(char) == 127 for char in value):
            raise Missing('request credential contains control characters')
        headers[name] = value
    headers.update(request.get('headers', {}))
    body = protected_input(request['body_file'], 'mutation body').read_bytes() if 'body_file' in request else None
    if body is not None:
        headers['Content-Type'] = 'application/json'
    path = request['path']
    if not path.startswith('/') or path.startswith('//') or any(char in path for char in '\r\n'):
        raise Missing('invalid fairness request path')
    connection = http.client.HTTPSConnection(origin.hostname, origin.port or 443, context=context, timeout=30)
    try:
        connection.request(request['method'], path, body=body, headers=headers)
        response = connection.getresponse()
        raw = response.read(2_097_153)
        if len(raw) > 2_097_152:
            raise Missing('fairness response exceeds bound')
        return response.status, json.loads(raw)
    finally:
        connection.close()


def fair_binary(fixture, name, manifest):
    entry = fixture['binaries'][name]
    binary = Path(entry['path'])
    if not binary.is_absolute() or binary.name != name or not binary.is_file() or not os.access(binary, os.X_OK):
        raise Missing('fairness binary is absent: ' + name)
    with binary.open('rb') as stream:
        digest = hashlib.file_digest(stream, 'sha256').hexdigest()
    if entry['sha256'] != digest or entry['source_revision'] != manifest['source']['revision']:
        raise Missing('fairness binary is not bound to the candidate: ' + name)
    return str(binary)


def fair_snapshot(enrollment, generation, entries):
    enrollment.write(enrollment.document(generation, entries))


def principal_delivery_fairness(manifest, results):
    location = os.environ.get('PAXEER_X_HUMAN_FAIRNESS')
    if not location:
        raise Missing('PAXEER_X_HUMAN_FAIRNESS is required')
    fixture = json.loads(protected_input(location, 'Human fairness attachment').read_bytes())
    if fixture.get('schema') != 'paxeer-x.human-event-fairness.v1' \
            or fixture.get('scope') != 'isolated-real-process' \
            or fixture.get('candidate_manifest_sha256') != MANIFEST_DIGEST:
        raise Missing('Human fairness attachment is not bound to this isolated candidate')
    failed, healthy = fixture['failed_principal'], fixture['healthy_principal']
    if not re.fullmatch(r'[a-z0-9_-]{1,128}', failed) or not re.fullmatch(r'[a-z0-9_-]{1,128}', healthy) \
            or failed >= healthy:
        raise Missing('the repeatedly failing principal must precede the healthy principal')
    for principal in (failed, healthy):
        requests = fixture['enqueue'][principal]
        outage_requests = fixture['destination_outage_enqueue'][principal]
        if len(outage_requests) != 2 or any(request['method'] != 'POST' for request in outage_requests):
            raise Missing('two further real Human mutations per principal are required for destination outage')
        if len(requests) != 2 or any(request['method'] != 'POST' for request in requests):
            raise Missing('exactly two real Human mutations per principal are required')
    binaries = {name: fair_binary(fixture, name, manifest)
                for name in ('layerx-event-source', 'layerx-human-components', 'layerx-webhooks')}
    evidence = os.environ.get('PAXEER_X_EVIDENCE_DIR')
    if not evidence:
        raise Missing('PAXEER_X_EVIDENCE_DIR is required')
    root = Path(fixture['isolation_root']).resolve(strict=True)
    if root == Path('/') or root.stat().st_uid != os.geteuid() or root.stat().st_mode & 0o077:
        raise Missing('fairness isolation root must be owner-only')
    human_env = dict(fixture['human_environment'])
    for name in ('LAYERX_HUMAN_STORE_ROOT', 'LAYERX_HUMAN_CUSTODY_ROOT', 'LAYERX_HUMAN_AUTH_INDEX_ROOT',
                 'LAYERX_HUMAN_COMPONENT_SOCKET', 'LAYERX_HUMAN_RECIPIENT_SOCKET'):
        path = Path(human_env[name]).resolve()
        if root not in path.parents:
            raise Missing('Human runtime state is outside the isolated fixture')
    work = Path(tempfile.mkdtemp(prefix='principal-delivery-fairness-',
                                dir=private_directory(Path(evidence))))
    work.chmod(0o700)
    print('evidence=' + str(work))
    status_directory = Path(tempfile.mkdtemp(prefix='lx-events-'))
    status_directory.chmod(0o700)
    status_path = status_directory / 'status.sock'
    print('status_directory=' + str(status_directory))
    human_env['LAYERX_HUMAN_EVENT_STATUS_SOCKET'] = str(status_path)
    source_processes = []
    source_environments = {}
    source_by_kind = {}
    worker = None
    webhook = None
    enrollments = {}
    try:
        for kind in ('journeys', 'approvals'):
            role = fixture['sources'][kind]
            env = dict(role['environment'])
            if env['LAYERX_EVENTS_KIND'] != kind or not env['LAYERX_EVENTS_LISTEN'].startswith('127.0.0.1:'):
                raise Missing('fairness source kind or loopback listener mismatch')
            directory = private_directory(work / kind)
            key = protected_input(env['LAYERX_EVENTS_ENROLLMENT_KEY_FILE'], 'source enrollment key').read_bytes().rstrip(b'\r\n')
            enrollment = Enrollment(directory, kind, key)
            env['LAYERX_EVENTS_CREDENTIALS_FILE'] = str(enrollment.path)
            env['LAYERX_EVENTS_STATE_DIR'] = str(private_directory(directory / 'state'))
            entries = [(principal, protected_input(role['credentials'][principal], 'principal credential'))
                       for principal in (failed, healthy)]
            fair_snapshot(enrollment, 1, [])
            enrollments[kind] = (enrollment, entries)
            source = EventSource(binaries['layerx-event-source'], env, directory / 'source.log')
            source_processes.append(source)
            source_environments[kind] = env
            source_by_kind[kind] = source
        webhook = FairWorker(binaries['layerx-webhooks'], dict(fixture['webhook_environment']), work / 'webhook.log')
        worker = FairWorker(binaries['layerx-human-components'], human_env, work / 'human-1.log')
        fair_wait(lambda: status_path.exists(), 60, worker)
        for principal in (failed, healthy):
            before = fair_status(status_path, principal)
            results.check('isolated Human outbox starts empty for ' + principal, before['pending_count'] == 0)
            if before['pending_count'] != 0:
                raise Missing('fairness outbox is not initially empty')
            for request in fixture['enqueue'][principal]:
                status, _ = fair_http(fixture['human_endpoints'][principal], request)
                results.check('real Human mutation enqueues a durable event', status in (200, 201, 202))
                if status not in (200, 201, 202):
                    raise Missing('real Human event mutation refused')
        initial = {principal: fair_status(status_path, principal) for principal in (failed, healthy)}
        for principal, state in initial.items():
            results.check('both subject transitions are durably pending for ' + principal,
                          state['pending_count'] == 2 and state['pending'] is not None)
            if state['pending_count'] != 2 or state['pending'] is None:
                raise Missing('required Human outbox events are absent')
        if {state['pending']['observation']['kind'] for state in initial.values()} != {'journey', 'approval'}:
            raise Missing('the two principals must exercise separate journey and approval destinations')
        refused = fair_wait(lambda: (state if (state := fair_status(status_path, failed))['delivery']['attempts'] > 0
                                    else None), 60, worker)
        first = initial[failed]['pending']
        results.check('the first enumerated principal has a real delivery refusal',
                      refused['delivery']['last_refusal'] in ('observation_refused', 'observation_unavailable'))
        for enrollment, entries in enrollments.values():
            fair_snapshot(enrollment, 2, [entries[1]])
        started = time.monotonic()
        delivered = fair_wait(lambda: (state if (state := fair_status(status_path, healthy))['pending_count'] == 0
                                      else None), 120, worker)
        failed_state = fair_status(status_path, failed)
        results.check('healthy principal delivers both events within the bounded scheduling window',
                      time.monotonic() - started <= 120 and delivered['delivery']['last_progress_at'] is not None)
        results.check('healthy progress preserves the failing principal head bytes and refusal evidence',
                      failed_state['pending'] == first and failed_state['pending_count'] == 2
                      and failed_state['delivery']['first_refused_at'] == refused['delivery']['first_refused_at'])
        terminal = fair_wait(lambda: (state if (state := fair_status(status_path, failed))['delivery']['redelivery_required']
                                     else None), 180, worker)
        results.check('retry exhaustion retains explicit redelivery state and canonical bytes',
                      terminal['delivery']['attempts'] == 5 and terminal['pending'] == first
                      and terminal['pending_count'] == 2 and terminal['delivery']['last_refusal'] is not None)
        worker.stop()
        worker = FairWorker(binaries['layerx-human-components'], human_env, work / 'human-2.log')
        replay = fair_wait(lambda: fair_status(status_path, failed), 60, worker)
        results.check('restart replays the selection cursor, backoff, terminal state and refusal evidence',
                      replay['delivery']['selected_turn'] >= terminal['delivery']['selected_turn']
                      and replay['delivery']['attempts'] == 5 and replay['delivery']['redelivery_required']
                      and replay['delivery']['first_refused_at'] == terminal['delivery']['first_refused_at']
                      and replay['pending'] == first)
        results.check('restart does not resurrect healthy deliveries', fair_status(status_path, healthy)['pending_count'] == 0)
        for enrollment, entries in enrollments.values():
            fair_snapshot(enrollment, 3, entries)
        recovered = fair_wait(lambda: (state if (state := fair_status(status_path, failed))['pending_count'] == 0
                                      else None), 120, worker)
        results.check('authenticated credential generation recovery resumes the failed principal',
                      recovered['delivery']['last_recovery_generation'] == 3
                      and not recovered['delivery']['redelivery_required']
                      and recovered['delivery']['last_progress_at'] is not None)
        for principal in (failed, healthy):
            endpoint = fixture['webhook_readers'][principal]
            status, events = fair_http(endpoint, {'method': 'GET', 'path': '/v1/webhooks/events'})
            if not isinstance(events, list):
                raise Missing('real webhook event ledger is unavailable')
            head = initial[principal]['pending']['observation']
            expected = [event_id(head['kind'], head['resource'], sequence) for sequence in (1, 2)]
            matching = [event for event in events if event.get('id') in expected]
            results.check('canonical webhook ledger has each subject event exactly once for ' + principal,
                          status == 200 and len(matching) == 2 and {event['id'] for event in matching} == set(expected)
                          and [event['subject_sequence'] for event in sorted(matching, key=lambda event: event['subject_sequence'])] == [1, 2])
            status2, replayed = fair_http(endpoint, {'method': 'GET', 'path': '/v1/webhooks/events'})
            results.check('replay has no duplicate event effect for ' + principal, status2 == 200 and replayed == events)
        failed_kind = initial[failed]['pending']['observation']['kind'] + 's'
        source_by_kind[failed_kind].stop()
        for principal in (failed, healthy):
            for request in fixture['destination_outage_enqueue'][principal]:
                status, _ = fair_http(fixture['human_endpoints'][principal], request)
                results.check('real Human transition remains durable during destination outage', status in (200, 201, 202))
                if status not in (200, 201, 202):
                    raise Missing('destination outage mutation refused')
        unavailable = fair_wait(lambda: (state if (state := fair_status(status_path, failed))['delivery']['attempts'] > 0
                                        and state['pending_count'] == 2 else None), 60, worker)
        held = unavailable['pending']
        healthy_head = initial[healthy]['pending']['observation']
        healthy_last = event_id(healthy_head['kind'], healthy_head['resource'], 4)
        progressed = fair_wait(lambda: (state if (state := fair_status(status_path, healthy))['pending_count'] == 0
                                       and state['delivery']['last_delivered_id'] == healthy_last else None), 120, worker)
        results.check('an unavailable destination cannot block the other principal destination',
                      progressed['delivery']['last_delivered_id'] == healthy_last
                      and fair_status(status_path, failed)['pending'] == held)
        replacement = EventSource(binaries['layerx-event-source'], source_environments[failed_kind],
                                  work / failed_kind / 'source-restarted.log')
        source_processes.append(replacement)
        failed_head = initial[failed]['pending']['observation']
        failed_last = event_id(failed_head['kind'], failed_head['resource'], 4)
        resumed = fair_wait(lambda: (state if (state := fair_status(status_path, failed))['pending_count'] == 0
                                    and state['delivery']['last_delivered_id'] == failed_last else None), 120, worker)
        results.check('the recovered destination preserves the exact retained event stream',
                      resumed['delivery']['last_delivered_id'] == failed_last)
        ledger = {}
        for principal in (failed, healthy):
            status, events = fair_http(fixture['webhook_readers'][principal], {'method': 'GET', 'path': '/v1/webhooks/events'})
            head = initial[principal]['pending']['observation']
            expected = [event_id(head['kind'], head['resource'], sequence) for sequence in (1, 2, 3, 4)]
            if not isinstance(events, list):
                raise Missing('durable webhook event ledger is unavailable after recovery')
            matching = [event for event in events if event.get('id') in expected]
            results.check('destination restart produces exactly one canonical effect per event for ' + principal,
                          status == 200 and len(matching) == 4 and {event['id'] for event in matching} == set(expected))
            ledger[principal] = events
        worker.stop()
        worker = FairWorker(binaries['layerx-human-components'], human_env, work / 'human-3.log')
        for principal in (failed, healthy):
            final_state = fair_wait(lambda: fair_status(status_path, principal), 60, worker)
            status, events = fair_http(fixture['webhook_readers'][principal], {'method': 'GET', 'path': '/v1/webhooks/events'})
            results.check('final worker restart does not recreate acknowledged effects for ' + principal,
                          final_state['pending_count'] == 0 and status == 200 and events == ledger[principal])
        results.check('the real webhook ingress remained running', webhook.child.poll() is None)
    finally:
        if worker is not None:
            worker.stop()
        if webhook is not None:
            webhook.stop()
        for process in source_processes:
            process.stop()


def roles_webhook_ingress_roles(manifest, results):
    CA_SH = ROOT / 'tools' / 'bringup' / 'ca.sh'
    SCHEMA = 'paxeer-x.candidate.v1'
    PRODUCER = 'urn:layerx:webhooks:role:producer'
    OPERATOR = 'urn:layerx:webhooks:role:operator'
    PRODUCER_ROWS = ('human-event-client', 'gateway-client', 'registry-event-client')
    INGRESS_SECRETS = (
        'WEBHOOKS_COMPONENT_TOKEN', 'WEBHOOKS_AUTHORITY_TOKEN', 'WEBHOOKS_JOURNEY_SOURCE_TOKEN',
        'WEBHOOKS_PAYMENT_SOURCE_TOKEN', 'WEBHOOKS_APPROVAL_SOURCE_TOKEN', 'WEBHOOKS_PROGRAM_SOURCE_TOKEN',
        'WEBHOOKS_SOURCE_TRIGGER_TOKEN', 'WEBHOOKS_OPERATOR_TOKEN', 'WEBHOOKS_SEQUENCER_PUBLIC_KEY',
        'WEBHOOKS_SEQUENCER_ID', 'WEBHOOKS_SEQUENCER_FIRST_BATCH', 'WEBHOOKS_SEQUENCER_LAST_BATCH',
    )
    PUBLIC_SECRETS = ('WEBHOOKS_IDENTITY_TOKEN',)
    SOURCE_EVENT = '7' * 64
    
    
    def ca_sh(arguments, ca_dir, check=True):
        environment = {'PATH': os.environ.get('PATH', '/usr/bin:/bin'), 'LAYERX_CA_DIR': str(ca_dir),
                       'HOME': str(ca_dir.parent)}
        return run(['bash', str(CA_SH), *arguments], environment, check=check)
    
    
    def private_directory(path):
        path.mkdir(mode=0o700)
        return path
    
    
    def ca_init(directory):
        output = ca_sh(['init'], directory).stdout.strip()
        if len(output.split(':')) != 32:
            raise Missing(f'ca.sh init printed no fingerprint: {output!r}')
        return directory
    
    
    def row(table, service):
        for line in table.splitlines():
            fields = line.split()
            if fields and fields[0] == service:
                return fields
        raise Missing(f'ca.sh has no row {service}')
    
    
    def leaf(directory, ca_dir, cn, eku, sans, expired=False):
        """Issues a leaf under ca_dir with openssl, the signing step of ca.sh."""
        directory = private_directory(directory)
        key, csr, cert, ext = (directory / name for name in ('key.pem', 'csr.pem', 'cert.pem', 'ext.cnf'))
        run(['openssl', 'genpkey', '-algorithm', 'EC', '-pkeyopt', 'ec_paramgen_curve:P-256', '-out', str(key)])
        run(['openssl', 'req', '-new', '-key', str(key), '-subj', f'/O=Paxeer X Network/CN={cn}', '-out', str(csr)])
        lines = ['basicConstraints=CA:FALSE', 'keyUsage=digitalSignature,keyEncipherment']
        if eku:
            lines.append(f'extendedKeyUsage={eku}')
        if sans:
            lines.append(f'subjectAltName={sans}')
        ext.write_text('\n'.join(lines) + '\n')
        if expired:
            (directory / 'index.txt').write_text('')
            (directory / 'serial').write_text('01\n')
            config = directory / 'ca.cnf'
            config.write_text(
                '[ca]\ndefault_ca = d\n[d]\n'
                f'database = {directory}/index.txt\nnew_certs_dir = {directory}\nserial = {directory}/serial\n'
                'default_md = sha256\npolicy = p\nunique_subject = no\n[p]\ncommonName = supplied\n'
                'organizationName = optional\n')
            run(['openssl', 'ca', '-batch', '-notext', '-config', str(config), '-cert', str(ca_dir / 'ca.pem'),
                 '-keyfile', str(ca_dir / 'ca.key'), '-in', str(csr), '-out', str(cert),
                 '-startdate', '20200101000000Z', '-enddate', '20200102000000Z', '-extfile', str(ext)])
        else:
            run(['openssl', 'x509', '-req', '-in', str(csr), '-CA', str(ca_dir / 'ca.pem'), '-CAkey',
                 str(ca_dir / 'ca.key'), '-CAserial', str(directory / 'ca.srl'), '-CAcreateserial',
                 '-days', '30', '-sha256', '-extfile', str(ext), '-out', str(cert)])
        return directory
    
    
    def der(directory):
        run(['openssl', 'x509', '-in', str(directory / 'cert.pem'), '-outform', 'DER', '-out',
             str(directory / 'cert.der')])
        run(['openssl', 'pkcs8', '-topk8', '-nocrypt', '-in', str(directory / 'key.pem'), '-outform', 'DER',
             '-out', str(directory / 'key.der')])
    
    
    def secret(directory, name, value):
        path = directory / name
        path.write_text(value)
        path.chmod(0o600)
        return str(path)
    
    
    def free_port():
        with socket.socket() as probe:
            probe.bind(('127.0.0.1', 0))
            return probe.getsockname()[1]
    
    
    def base_environment(work, client, internal_ca, listen):
        """The shared configuration of both roles: upstreams are bound to a closed
        port, so canonical source retrieval, Redis and KMS stay real and refused."""
        closed = free_port()
        shared = work / 'shared'
        if not shared.exists():
            private_directory(shared)
        environment = {
            'PATH': os.environ.get('PATH', '/usr/bin:/bin'),
            'LAYERX_WEBHOOKS_LISTEN': f'127.0.0.1:{listen}',
            'LAYERX_WEBHOOKS_INTERNAL_CA_DER': str(internal_ca),
            'LAYERX_WEBHOOKS_PUBLIC_CA_DER': str(internal_ca),
            'LAYERX_WEBHOOKS_CLIENT_IDENTITY_PKCS12': str(client / 'identity.p12'),
            'LAYERX_WEBHOOKS_CLIENT_IDENTITY_PASSWORD_FILE': str(client / 'password'),
            'LAYERX_WEBHOOKS_REDIS_URL': f'rediss://localhost:{closed}',
            'LAYERX_WEBHOOKS_REDIS_USERNAME_FILE': secret(shared, 'redis-username', 'webhooks'),
            'LAYERX_WEBHOOKS_REDIS_PASSWORD_FILE': secret(shared, 'redis-password', os.urandom(16).hex()),
            'LAYERX_WEBHOOKS_CURSOR_KEY_FILE': secret(shared, 'cursor-key', os.urandom(32).hex()),
            'LAYERX_WEBHOOKS_KMS_URL': f'https://localhost:{closed}',
            'LAYERX_WEBHOOKS_KMS_TOKEN_FILE': secret(shared, 'kms-token', os.urandom(16).hex()),
            'LAYERX_WEBHOOKS_INSTANCE_ID': 'webhook-ingress-roles',
            'LAYERX_WEBHOOKS_LXP_WIRE_VERSION': '3',
            'LAYERX_WEBHOOKS_NETWORK_ID': 'paxeer-webhook-ingress-roles',
            'LAYERX_WEBHOOKS_IDENTITY_URL': f'https://localhost:{closed}',
            'LAYERX_WEBHOOKS_COMPONENT_URL': f'https://localhost:{closed}',
            'LAYERX_WEBHOOKS_AUTHORITY_URL': f'https://localhost:{closed}',
        }
        for stem in ('JOURNEY', 'PAYMENT', 'APPROVAL', 'PROGRAM'):
            environment[f'LAYERX_WEBHOOKS_{stem}_SOURCE_URL'] = f'https://localhost:{closed}'
        return environment
    
    
    def public_environment(work, client, internal_ca, listen):
        environment = base_environment(work, client, internal_ca, listen)
        environment.update({
            'LAYERX_WEBHOOKS_ROLE': 'public',
            'LAYERX_WEBHOOKS_LISTENER': 'plain',
            'LAYERX_WEBHOOKS_IDENTITY_TOKEN_FILE': secret(work / 'shared', 'identity-token', os.urandom(16).hex()),
        })
        return environment
    
    
    def ingress_environment(work, client, internal_ca, server, client_ca, listen, tokens):
        environment = base_environment(work, client, internal_ca, listen)
        shared = work / 'shared'
        environment.update({
            'LAYERX_WEBHOOKS_ROLE': 'ingress',
            'LAYERX_WEBHOOKS_LISTENER': 'tls',
            'LAYERX_WEBHOOKS_TLS_CERT_DER': str(server / 'cert.der'),
            'LAYERX_WEBHOOKS_TLS_KEY_DER': str(server / 'key.der'),
            'LAYERX_WEBHOOKS_INGRESS_CLIENT_CA_DER': str(client_ca),
            'LAYERX_WEBHOOKS_SOURCE_TRIGGER_TOKEN_FILE': tokens['source'],
            'LAYERX_WEBHOOKS_OPERATOR_TOKEN_FILE': tokens['operator'],
            'LAYERX_WEBHOOKS_COMPONENT_TOKEN_FILE': secret(shared, 'component-token', os.urandom(16).hex()),
            'LAYERX_WEBHOOKS_AUTHORITY_TOKEN_FILE': secret(shared, 'authority-token', os.urandom(16).hex()),
            'LAYERX_WEBHOOKS_SEQUENCER_PUBLIC_KEY_FILE': secret(shared, 'sequencer-public-key', '58' + '66' * 31),
            'LAYERX_WEBHOOKS_SEQUENCER_ID_FILE': secret(shared, 'sequencer-id', '22' * 32),
            'LAYERX_WEBHOOKS_SEQUENCER_FIRST_BATCH_FILE': secret(shared, 'sequencer-first-batch', '1'),
            'LAYERX_WEBHOOKS_SEQUENCER_LAST_BATCH_FILE': secret(shared, 'sequencer-last-batch', str(2 ** 64 - 1)),
        })
        for stem in ('JOURNEY', 'PAYMENT', 'APPROVAL', 'PROGRAM'):
            environment[f'LAYERX_WEBHOOKS_{stem}_SOURCE_TOKEN_FILE'] = secret(
                shared, f'{stem.lower()}-source-token', os.urandom(16).hex())
        return environment
    
    
    class Process:
        def __init__(self, binary, environment, log):
            self.port = int(environment['LAYERX_WEBHOOKS_LISTEN'].rsplit(':', 1)[1])
            self.log = log.open('ab')
            self.child = subprocess.Popen([binary], env=environment, stdin=subprocess.DEVNULL,
                                          stdout=self.log, stderr=self.log)
            deadline = time.monotonic() + 20
            while time.monotonic() < deadline:
                if self.child.poll() is not None:
                    raise Missing(f'layerx-webhooks exited {self.child.returncode} before listening; see {log.name}')
                try:
                    with socket.create_connection(('127.0.0.1', self.port), timeout=1):
                        return
                except OSError:
                    time.sleep(0.1)
            self.stop()
            raise Missing('layerx-webhooks never listened')
    
        def stop(self):
            if self.child.poll() is None:
                self.child.terminate()
                try:
                    self.child.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    self.child.kill()
                    self.child.wait()
            self.log.close()
    
    
    def answer(connection, method, path, bearer=None, headers=None):
        request_headers = dict(headers or {})
        if bearer is not None:
            request_headers['Authorization'] = f'Bearer {bearer}'
        try:
            connection.request(method, path, body=b'{}' if method == 'POST' else None, headers=request_headers)
            response = connection.getresponse()
            body = response.read()
        except (ssl.SSLError, ConnectionError, socket.timeout) as error:
            return ('refused', type(error).__name__)
        finally:
            connection.close()
        try:
            document = json.loads(body)
        except ValueError:
            document = {}
        code = document.get('error', {}).get('code') if isinstance(document.get('error'), dict) else None
        return (response.status, code if code is not None else document)
    
    
    def plain(port, method, path, bearer=None, headers=None):
        return answer(http.client.HTTPConnection('127.0.0.1', port, timeout=10), method, path, bearer, headers)
    
    
    def tls(port, server_ca, identity, method, path, bearer=None, headers=None):
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
        context.load_verify_locations(str(server_ca))
        if identity is not None:
            context.load_cert_chain(str(identity / 'cert.pem'), str(identity / 'key.pem'))
        connection = http.client.HTTPSConnection('localhost', port, context=context, timeout=10)
        return answer(connection, method, path, bearer, headers)
    
    
    def startup_refusal(binary, environment, expected):
        completed = run([binary], environment, check=False, timeout=30)
        return completed.returncode == 2 and completed.stderr.strip() == f'layerx-webhooks: {expected}', \
            f'exit={completed.returncode} stderr={completed.stderr.strip()[-200:]}'
    
    
    def wiring(results):
        fly = tomllib.loads((ROOT / 'platform/hosted/webhooks/fly.toml').read_text())
        files = {entry['secret_name']: entry.get('processes') for entry in fly.get('files', [])}
        for name in INGRESS_SECRETS:
            results.check(f'fly mounts {name} only into ingress', files.get(name) == ['ingress'], files.get(name))
        for name in PUBLIC_SECRETS:
            results.check(f'fly mounts {name} only into public', files.get(name) == ['public'], files.get(name))
        results.check('fly public API stays on private TLS behind the unified endpoint',
                      not fly.get('http_service') and fly.get('processes', {}).get('public') == 'public', fly.get('http_service'))
        for name in ('WEBHOOKS_INGRESS_TLS_CERT', 'WEBHOOKS_INGRESS_TLS_CERT_DER', 'WEBHOOKS_INGRESS_TLS_KEY_DER'):
            results.check('both TLS roles mount their server identity', files.get(name) == ['public', 'ingress'])
        results.check('fly exposes no service for the ingress group', not fly.get('services'), fly.get('services'))
        role_variables = [name for name in fly.get('env', {}) if name.endswith(
            ('SOURCE_TRIGGER_TOKEN_FILE', 'OPERATOR_TOKEN_FILE', 'IDENTITY_TOKEN_FILE', 'INGRESS_CLIENT_CA_DER'))]
        results.check('fly shared env carries no role credential path', not role_variables, role_variables)
        init = (ROOT / 'platform/hosted/webhooks/fly-init.sh').read_text()
        public, _, ingress = init.partition('\ningress)\n')
        results.check('fly init public role is public and reads only the identity token',
                      'LAYERX_WEBHOOKS_ROLE=public' in public and 'IDENTITY_TOKEN_FILE' in public
                      and 'SOURCE_TRIGGER' not in public and 'OPERATOR_TOKEN' not in public, 'public branch')
        results.check('fly init ingress role is TLS with the internal client CA',
                      'LAYERX_WEBHOOKS_ROLE=ingress' in ingress and 'LAYERX_WEBHOOKS_LISTENER=tls' in ingress
                      and 'LAYERX_WEBHOOKS_INGRESS_CLIENT_CA_DER=/run/layerx/ca/internal.der' in ingress
                      and 'IDENTITY_TOKEN_FILE' not in ingress, 'ingress branch')
        documents = (ROOT / 'platform/hosted/webhooks/deployment.yaml').read_text().split('\n---\n')
        deployments = {}
        for document in documents:
            if 'kind: Deployment' in document and 'role: public' in document:
                deployments['public'] = document
            elif 'kind: Deployment' in document and 'role: ingress' in document:
                deployments['ingress'] = document
        public_doc, ingress_doc = deployments.get('public', ''), deployments.get('ingress', '')
        results.check('kubernetes public deployment is role public without trigger credentials',
                      'LAYERX_WEBHOOKS_ROLE, value: public' in public_doc and 'source-trigger' not in public_doc
                      and 'operator' not in public_doc, 'public deployment')
        results.check('kubernetes ingress deployment requires the client CA and lacks the identity token',
                      'LAYERX_WEBHOOKS_ROLE, value: ingress' in ingress_doc
                      and 'LAYERX_WEBHOOKS_INGRESS_CLIENT_CA_DER' in ingress_doc
                      and 'tokens/identity' not in ingress_doc, 'ingress deployment')
        routed = [document for document in documents if 'kind: Ingress' in document and '/v1/webhooks' in document]
        results.check('kubernetes edge routes /v1/webhooks only to the public service',
                      len(routed) == 1 and 'name: layerx-webhooks-public,' in routed[0]
                      and '/internal' not in routed[0], routed)
    
    
    def issue_local_cases(results, work, ca_dir):
        table = ca_sh(['services'], ca_dir).stdout
        local = ca_sh(['local-services'], ca_dir).stdout
        for service in PRODUCER_ROWS:
            fields = row(table, service)
            results.check(f'ca.sh {service} carries only the producer role SAN with clientAuth',
                          fields[5] == 'clientAuth' and fields[6] == f'URI:{PRODUCER}', fields[5:])
        results.check('ca.sh services carry no operator role', OPERATOR not in table, 'services table')
        results.check('ca.sh services keep the local operator identity out of Fly',
                      all(not line.startswith('webhook-operator-client ') for line in table.splitlines()), 'services')
        fields = row(local, 'webhook-operator-client')
        results.check('ca.sh local-services declares the fixed operator identity',
                      fields[1:] == ['-', '-', 'local', 'layerx-webhooks-operator', 'clientAuth', f'URI:{OPERATOR}'],
                      fields)
        holder = private_directory(work / 'operator-holder')
        destination = holder / 'operator'
        issued = ca_sh(['issue-local', 'webhook-operator-client', '--output-dir', str(destination)], ca_dir, check=False)
        printed = issued.stdout + issued.stderr
        results.check('ca.sh issue-local issues the operator identity', issued.returncode == 0
                      and issued.stdout.startswith('issued webhook-operator-client custody=local fingerprint='),
                      f'exit={issued.returncode} {issued.stderr.strip()[-200:]}')
        results.check('ca.sh issue-local prints no private material',
                      'PRIVATE KEY' not in printed and 'BEGIN' not in printed, 'output')
        if issued.returncode != 0:
            raise Missing('the operator identity was not issued')
        names = sorted(path.name for path in destination.iterdir())
        results.check('issued bundle is complete', names == sorted(
            ['ca.der', 'ca.pem', 'cert.der', 'cert.pem', 'identity.p12', 'key.der', 'key.pem', 'password']), names)
        results.check('issued bundle directory is mode 0700', destination.stat().st_mode & 0o777 == 0o700,
                      oct(destination.stat().st_mode))
        results.check('issued bundle files are mode 0600',
                      all((destination / name).stat().st_mode & 0o777 == 0o600 for name in names), 'modes')
        results.check('no staging directory remains', [path.name for path in holder.iterdir()] == ['operator'],
                      list(holder.iterdir()))
        text = run(['openssl', 'x509', '-in', str(destination / 'cert.pem'), '-noout', '-ext',
                    'subjectAltName,extendedKeyUsage']).stdout
        results.check('issued leaf carries exactly the operator role and clientAuth',
                      f'URI:{OPERATOR}' in text and 'TLS Web Client Authentication' in text
                      and 'Server Authentication' not in text and PRODUCER not in text, text)
        before = sorted((path.name, path.stat().st_mtime_ns) for path in destination.iterdir())
        again = ca_sh(['issue-local', 'webhook-operator-client', '--output-dir', str(destination)], ca_dir, check=False)
        results.check('issue-local refuses an existing destination and leaves it intact',
                      again.returncode == 1 and 'already exists' in again.stderr
                      and sorted((path.name, path.stat().st_mtime_ns) for path in destination.iterdir()) == before,
                      f'exit={again.returncode}')
        link = holder / 'link'
        link.symlink_to(holder)
        linked = ca_sh(['issue-local', 'webhook-operator-client', '--output-dir', str(link / 'operator-2')],
                       ca_dir, check=False)
        results.check('issue-local refuses a symbolic link component',
                      linked.returncode == 1 and 'symbolic link' in linked.stderr, f'exit={linked.returncode}')
        open_parent = work / 'open-parent'
        open_parent.mkdir()
        open_parent.chmod(0o777)
        insecure = ca_sh(['issue-local', 'webhook-operator-client', '--output-dir', str(open_parent / 'operator')],
                         ca_dir, check=False)
        results.check('issue-local refuses a group or world writable parent',
                      insecure.returncode == 1 and 'writable by group or others' in insecure.stderr
                      and not (open_parent / 'operator').exists(), f'exit={insecure.returncode}')
        overlap = ca_sh(['issue-local', 'webhook-operator-client', '--output-dir', str(ca_dir / 'operator')],
                        ca_dir, check=False)
        results.check('issue-local refuses a destination in the CA directory',
                      overlap.returncode == 1 and 'CA directory' in overlap.stderr
                      and not (ca_dir / 'operator').exists(), f'exit={overlap.returncode}')
        relative = ca_sh(['issue-local', 'webhook-operator-client', '--output-dir', 'operator'], ca_dir, check=False)
        results.check('issue-local refuses a relative destination', relative.returncode == 1, relative.returncode)
        for arguments in (['issue-local', 'webhook-operator-client', '--output-dir', str(holder / 'x'), '--role',
                           'producer'],
                          ['issue-local', 'webhook-operator-client', '--san', f'URI:{PRODUCER}'],
                          ['issue-local', 'webhook-operator-client']):
            selected = ca_sh(arguments, ca_dir, check=False)
            results.check(f'issue-local refuses caller-selected arguments {arguments[2:]}',
                          selected.returncode == 2 and not (holder / 'x').exists(), selected.returncode)
        foreign = ca_sh(['issue-local', 'human-event-client', '--output-dir', str(holder / 'y')], ca_dir, check=False)
        results.check('issue-local refuses a Fly service identity', foreign.returncode == 2
                      and not (holder / 'y').exists(), foreign.returncode)
        return destination
    
    
    def leaves(directory, ca, human):
        """The producer leaf of the human-event-client row and every refused
        variant of it, issued under one CA."""
        directory = private_directory(directory)
        return {
            'producer': leaf(directory / 'producer', ca, human[4], human[5], human[6]),
            'roleless': leaf(directory / 'roleless', ca, human[4], 'clientAuth', ''),
            'duplicate': leaf(directory / 'duplicate', ca, human[4], 'clientAuth', f'URI:{PRODUCER},URI:{PRODUCER}'),
            'contradictory': leaf(directory / 'contradictory', ca, human[4], 'clientAuth',
                                  f'URI:{PRODUCER},URI:{OPERATOR}'),
            'unknown': leaf(directory / 'unknown', ca, human[4], 'clientAuth', 'URI:urn:layerx:webhooks:role:admin'),
            'uppercase': leaf(directory / 'uppercase', ca, human[4], 'clientAuth', f'URI:{PRODUCER.upper()}'),
            'no-eku': leaf(directory / 'no-eku', ca, human[4], '', f'URI:{PRODUCER}'),
            'cn-only': leaf(directory / 'cn-only', ca, PRODUCER, 'clientAuth', ''),
            'server-only': leaf(directory / 'server-only', ca, human[4], 'serverAuth', f'URI:{PRODUCER}'),
            'expired': leaf(directory / 'expired', ca, human[4], human[5], human[6], expired=True),
        }
    
    
    def ingress_contract(results, port, server_ca, identities, tokens, label):
        """The ingress identity and bearer contract for one generation."""
        event = f'/internal/v1/events/payment/{SOURCE_EVENT}'
        producer, operator = identities['producer'], identities['operator']
        observed = tls(port, server_ca, producer, 'POST', event, tokens['source'])
        results.check(f'{label}: producer leaf and source bearer reach canonical source retrieval',
                      observed == (503, 'dependency_unavailable'), observed)
        observed = tls(port, server_ca, producer, 'POST', event)
        results.check(f'{label}: producer leaf without bearer is refused',
                      observed == (401, 'source_authentication_required'), observed)
        observed = tls(port, server_ca, producer, 'POST', event, tokens['operator'])
        results.check(f'{label}: producer leaf with the operator bearer is refused',
                      observed == (401, 'source_authentication_required'), observed)
        observed = tls(port, server_ca, producer, 'POST', '/internal/v1/dispatch', tokens['operator'])
        results.check(f'{label}: producer leaf cannot dispatch', observed == (403, 'operator_role_required'), observed)
        observed = tls(port, server_ca, operator, 'POST', '/internal/v1/dispatch', tokens['operator'])
        results.check(f'{label}: operator leaf and operator bearer reach the delivery state',
                      observed == (503, 'dependency_unavailable'), observed)
        observed = tls(port, server_ca, operator, 'POST', '/internal/v1/dispatch', tokens['source'])
        results.check(f'{label}: operator leaf with the source bearer is refused',
                      observed == (401, 'operator_authentication_required'), observed)
        observed = tls(port, server_ca, operator, 'POST', '/internal/v1/dispatch')
        results.check(f'{label}: operator leaf without bearer is refused',
                      observed == (401, 'operator_authentication_required'), observed)
        observed = tls(port, server_ca, operator, 'POST', event, tokens['source'])
        results.check(f'{label}: operator leaf cannot publish', observed == (403, 'producer_role_required'), observed)
        for name in ('roleless', 'duplicate', 'contradictory', 'unknown', 'uppercase', 'no-eku', 'cn-only'):
            observed = tls(port, server_ca, identities[name], 'POST', event, tokens['source'],
                           {'X-LayerX-Role': 'producer'})
            results.check(f'{label}: {name} leaf is refused before the bearer',
                          observed == (403, 'peer_role_refused'), observed)
        for name in ('foreign', 'expired', 'server-only'):
            observed = tls(port, server_ca, identities[name], 'POST', event, tokens['source'])
            results.check(f'{label}: {name} leaf is refused in the handshake', observed[0] == 'refused', observed)
        observed = tls(port, server_ca, None, 'POST', event, tokens['source'])
        results.check(f'{label}: absent client certificate is refused in the handshake',
                      observed[0] == 'refused', observed)
        observed = tls(port, server_ca, producer, 'GET', '/v1/webhooks/scheme')
        results.check(f'{label}: ingress serves no developer route', observed == (404, 'not_found'), observed)
        observed = tls(port, server_ca, producer, 'GET', '/healthz')
        results.check(f'{label}: ingress health names the ingress role',
                      observed[0] == 503 and isinstance(observed[1], dict) and observed[1].get('role') == 'ingress',
                      observed)
    
    
    def public_contract(results, port, tokens, label):
        for method, path, bearer in (('POST', f'/internal/v1/events/payment/{SOURCE_EVENT}', tokens['source']),
                                     ('POST', '/internal/v1/dispatch', tokens['operator']),
                                     ('GET', '/internal', None), ('POST', '/INTERNAL/v1/dispatch', tokens['operator']),
                                     ('POST', '//internal/v1/dispatch', tokens['operator'])):
            observed = plain(port, method, path, bearer)
            results.check(f'{label}: public refuses {method} {path}', observed == (404, 'not_found'), observed)
        observed = plain(port, 'GET', '/v1/webhooks/scheme')
        results.check(f'{label}: public serves the scheme document', observed[0] == 200, observed)
        observed = plain(port, 'GET', '/v1/webhooks/endpoints', tokens['source'])
        results.check(f'{label}: public developer routes require a developer session',
                      observed == (401, 'session_required'), observed)
        observed = plain(port, 'GET', '/healthz')
        results.check(f'{label}: public health names the public role',
                      isinstance(observed[1], dict) and observed[1].get('role') == 'public', observed)
    
    
    def webhook_ingress_roles(manifest, results):
        binary = str(Path(os.environ['PAXEER_X_HOSTED_BIN_DIR']) / 'layerx-webhooks')
        if not os.access(binary, os.X_OK):
            raise Missing(f'layerx-webhooks binary absent at {binary}; build layerx-platform-webhooks first')
        for tool in ('openssl', 'bash'):
            if shutil.which(tool) is None:
                raise Missing(f'{tool} is required')
        print(f'candidate schema={manifest["schema"]} services={len(manifest["services"])}')
        wiring(results)
        evidence = os.environ.get('PAXEER_X_EVIDENCE_DIR')
        if not evidence:
            raise Missing('PAXEER_X_EVIDENCE_DIR is required')
        temporary = tempfile.mkdtemp(prefix='webhook-ingress-roles-', dir=private_directory(Path(evidence)))
        print('evidence=' + temporary)
        if True:
            work = Path(temporary)
            work.chmod(0o700)
            logs = private_directory(work / 'logs')
            ca = ca_init(work / 'ca')
            foreign_ca = ca_init(work / 'foreign-ca')
            table = ca_sh(['services'], ca).stdout
            developer = row(table, 'developer')
            app = tomllib.loads((ROOT / developer[1]).read_text())['app']
            server = leaf(work / 'server', ca, developer[4], developer[5], developer[6].replace('<app>', app))
            der(server)
            client = leaf(work / 'client', ca, row(table, 'developer-client')[4], 'clientAuth', '')
            run(['bash', '-c', 'cd "$0" && cp "$1" ca.pem && openssl rand -hex 32 >password && '
                 'openssl pkcs12 -export -inkey key.pem -in cert.pem -certfile ca.pem -passout file:password '
                 '-out identity.p12', str(client), str(ca / 'ca.pem')])
            human = row(table, 'human-event-client')
            identities = leaves(work / 'generation-1', ca, human)
            identities['foreign'] = leaf(work / 'foreign', foreign_ca, human[4], human[5], human[6])
            identities['operator'] = issue_local_cases(results, work, ca)
            tokens_dir = private_directory(work / 'tokens-1')
            values = {'source': os.urandom(24).hex(), 'operator': os.urandom(24).hex()}
            tokens = {name: secret(tokens_dir, name, value) for name, value in values.items()}
            server_ca = server / 'ca.pem'
            shutil.copy(ca / 'ca.pem', server_ca)
    
            public_port = free_port()
            public_env = public_environment(work, client, ca / 'ca.der', public_port)
            for variable, value in (('LAYERX_WEBHOOKS_SOURCE_TRIGGER_TOKEN_FILE', tokens['source']),
                                    ('LAYERX_WEBHOOKS_OPERATOR_TOKEN_FILE', tokens['operator']),
                                    ('LAYERX_WEBHOOKS_INGRESS_CLIENT_CA_DER', str(ca / 'ca.der'))):
                passed, observed = startup_refusal(binary, {**public_env, variable: value},
                                                   f'{variable} is set with LAYERX_WEBHOOKS_ROLE public')
                results.check(f'public refuses to load {variable}', passed, observed)
            passed, observed = startup_refusal(
                binary, {name: value for name, value in public_env.items() if name != 'LAYERX_WEBHOOKS_ROLE'},
                'LAYERX_WEBHOOKS_ROLE must be public or ingress')
            results.check('a process without an explicit role refuses to start', passed, observed)
            ingress_port = free_port()
            ingress_env = ingress_environment(work, client, ca / 'ca.der', server, ca / 'ca.der', ingress_port, tokens)
            passed, observed = startup_refusal(binary, {**ingress_env, 'LAYERX_WEBHOOKS_LISTENER': 'plain'},
                                               'LAYERX_WEBHOOKS_ROLE ingress requires LAYERX_WEBHOOKS_LISTENER tls')
            results.check('ingress refuses a plain listener', passed, observed)
            passed, observed = startup_refusal(
                binary, {name: value for name, value in ingress_env.items()
                         if name != 'LAYERX_WEBHOOKS_INGRESS_CLIENT_CA_DER'},
                'LAYERX_WEBHOOKS_INGRESS_CLIENT_CA_DER is required')
            results.check('ingress refuses to start without the client CA', passed, observed)
            passed, observed = startup_refusal(
                binary, {**ingress_env, 'LAYERX_WEBHOOKS_IDENTITY_TOKEN_FILE': public_env[
                    'LAYERX_WEBHOOKS_IDENTITY_TOKEN_FILE']},
                'LAYERX_WEBHOOKS_IDENTITY_TOKEN_FILE is set with LAYERX_WEBHOOKS_ROLE ingress')
            results.check('ingress refuses to load the developer identity token', passed, observed)
    
            passed, observed = startup_refusal(binary, {**ingress_env, 'LAYERX_WEBHOOKS_OPERATOR_TOKEN_FILE': tokens['source']},
                                               'source and operator trigger credentials must be distinct')
            results.check('ingress refuses a shared source/operator bearer', passed, observed)
            public = Process(binary, public_env, logs / 'public.log')
            ingress = Process(binary, ingress_env, logs / 'ingress-1.log')
            try:
                public_contract(results, public_port, values, 'generation 1')
                ingress_contract(results, ingress_port, server_ca, identities, values, 'generation 1')
                ingress.stop()
                public_contract(results, public_port, values, 'ingress down')
                ingress = Process(binary, ingress_env, logs / 'ingress-restart.log')
                ingress_contract(results, ingress_port, server_ca, identities, values, 'restart')
    
                rotated_ca = ca_init(work / 'ca-2')
                rotated = leaves(work / 'generation-2', rotated_ca, human)
                rotated['operator'] = private_directory(work / 'operator-holder-2')
                issued = ca_sh(['issue-local', 'webhook-operator-client', '--output-dir',
                                str(rotated['operator'] / 'operator')], rotated_ca, check=False)
                results.check('rotation issues a new operator identity', issued.returncode == 0, issued.returncode)
                rotated['operator'] = rotated['operator'] / 'operator'
                rotated['foreign'] = identities['producer']
                tokens_dir = private_directory(work / 'tokens-2')
                new_values = {'source': os.urandom(24).hex(), 'operator': os.urandom(24).hex()}
                new_tokens = {name: secret(tokens_dir, name, value) for name, value in new_values.items()}
                ingress.stop()
                ingress = Process(binary, {**ingress_env,
                                           'LAYERX_WEBHOOKS_INGRESS_CLIENT_CA_DER': str(rotated_ca / 'ca.der'),
                                           'LAYERX_WEBHOOKS_SOURCE_TRIGGER_TOKEN_FILE': new_tokens['source'],
                                           'LAYERX_WEBHOOKS_OPERATOR_TOKEN_FILE': new_tokens['operator']},
                                  logs / 'ingress-rotated.log')
                ingress_contract(results, ingress_port, server_ca, rotated, new_values, 'rotated')
                event = f'/internal/v1/events/payment/{SOURCE_EVENT}'
                observed = tls(ingress_port, server_ca, rotated['producer'], 'POST', event, values['source'])
                results.check('rotated: the previous source bearer is refused',
                              observed == (401, 'source_authentication_required'), observed)
                observed = tls(ingress_port, server_ca, rotated['operator'], 'POST', '/internal/v1/dispatch',
                               values['operator'])
                results.check('rotated: the previous operator bearer is refused',
                              observed == (401, 'operator_authentication_required'), observed)
                observed = tls(ingress_port, server_ca, identities['operator'], 'POST', '/internal/v1/dispatch',
                               new_values['operator'])
                results.check('rotated: the previous operator leaf is refused in the handshake',
                              observed[0] == 'refused', observed)
                public.stop()
                public = Process(binary, public_env, logs / 'public-restart.log')
                public_contract(results, public_port, new_values, 'public restart')
            finally:
                ingress.stop()
                public.stop()
    
    
    webhook_ingress_roles(manifest, results)


class roles_Process:
    def __init__(self, binary, environment, log):
        self.port = int(environment['LAYERX_WEBHOOKS_LISTEN'].rsplit(':', 1)[1])
        self.log = log.open('ab')
        self.child = subprocess.Popen([binary], env=environment, stdin=subprocess.DEVNULL,
                                      stdout=self.log, stderr=self.log)
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            if self.child.poll() is not None:
                raise Missing(f'layerx-webhooks exited {self.child.returncode} before listening; see {log.name}')
            try:
                with socket.create_connection(('127.0.0.1', self.port), timeout=1):
                    return
            except OSError:
                time.sleep(0.1)
        self.stop()
        raise Missing('layerx-webhooks never listened')

    def stop(self):
        if self.child.poll() is None:
            self.child.terminate()
            try:
                self.child.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.child.kill()
                self.child.wait()
        self.log.close()


def roles_ca_sh(arguments, ca_dir, check=True):
    return run(['bash', ROOT / 'tools/bringup/ca.sh', *arguments],
               dict(os.environ, LAYERX_CA_DIR=str(ca_dir)), check=check, timeout=60)

PRODUCER = 'urn:layerx:webhooks:role:producer'


def roles_artifacts(manifest):
    location = os.environ.get('PAXEER_X_WEBHOOK_ARTIFACTS')
    if not location:
        raise Missing('PAXEER_X_WEBHOOK_ARTIFACTS is required')
    value = json.loads(protected_input(location, 'webhook process artifacts').read_bytes())
    if not isinstance(value, dict) or value.get('schema') != 'paxeer-x.webhook-process-artifacts.v1' \
            or value.get('candidate_manifest_sha256') != MANIFEST_DIGEST \
            or value.get('source_revision') != manifest['source']['revision']:
        raise Missing('webhook process artifacts do not bind this candidate')
    required = {'layerx-webhooks', 'layerx-identity', 'layerx-kms', 'layerx-gateway',
                'layerx-event-source', 'layerx-agent-boundary', 'layerx-receipt-authority', 'layerx-program-registry',
                'builder-isolation', 'builder-supervisor'}
    if set(value.get('artifacts', {})) != required:
        raise Missing('webhook runtime artifact set is incomplete')
    directory = Path(os.environ['PAXEER_X_HOSTED_BIN_DIR']).resolve(strict=True)
    for name, artifact in value['artifacts'].items():
        path = Path(artifact['path']) if name.startswith('builder-') else directory / name
        if not path.is_absolute() or str(path) != artifact['path'] or path.is_symlink() or not os.access(path, os.X_OK):
            raise Missing('webhook runtime artifact layout mismatch')
        with path.open('rb') as stream:
            digest = hashlib.file_digest(stream, 'sha256').hexdigest()
        if digest != artifact['sha256'] or artifact['source_revision'] != value['source_revision']:
            raise Missing('webhook runtime artifact changed')
    configuration = json.loads(protected_input(os.environ['PAXEER_X_REGISTRY_CONFIGURATION'], 'registry configuration').read_bytes())
    for name, key in (('builder-isolation', 'ISOLATION_RUNTIME'), ('builder-supervisor', 'JOB_SUPERVISOR')):
        row = value['artifacts'][name]
        if configuration['LAYERX_REGISTRY_BUILDER_' + key] != row['path'] or configuration['LAYERX_REGISTRY_BUILDER_' + key + '_DIGEST'] != row['sha256']:
            raise Missing('registry builder configuration does not bind the candidate artifacts')
    head = run(['git', '-C', ROOT, 'rev-parse', 'HEAD']).stdout.strip()
    if head != manifest['source']['revision'] or run(['git', '-C', ROOT, 'status', '--porcelain']).stdout:
        raise Missing('webhook gate requires the clean candidate source')
    return value


def webhook_ingress_roles(manifest, results):
    roles_artifacts(manifest)
    sys.path.insert(0, str(ROOT / 'tools/qualification/paxeer-x/fixtures'))
    if os.environ.get('PAXEER_X_WEBHOOK_HEALTHY_WORKER'):
        roles_healthy_worker(manifest, results, Path(os.environ['PAXEER_X_WEBHOOK_HEALTHY_WORKER']))
        return
    roles_webhook_ingress_roles(manifest, results)
    from hosted_delivery_fixture import CaMaterial
    from event_receiver_fixture import EventReceiverFixture
    evidence = private_directory(Path(os.environ['PAXEER_X_EVIDENCE_DIR']))
    work = Path(tempfile.mkdtemp(prefix='webhook-delivery-', dir=evidence))
    work.chmod(0o700)
    ca = CaMaterial(work)
    receiver = EventReceiverFixture(work, ca)
    try:
        receiver._materials()
        receiver._start_namespace()
    except BaseException:
        receiver._terminate(receiver.holder)
        raise
    descriptor = {'parent_pid': os.getpid(), 'candidate_manifest_sha256': MANIFEST_DIGEST,
                  'network_namespace': os.stat(f'/proc/{receiver.holder.pid}/ns/net').st_ino,
                  'mount_namespace': os.stat(f'/proc/{receiver.holder.pid}/ns/mnt').st_ino,
                  'work': str(work), 'ca': str(ca.directory), 'receiver_root': str(receiver.root),
                  'receiver_endpoint': receiver.endpoint, 'receiver_address': receiver.address,
                  'receiver_port': receiver.port,
                  'receiver_cert': str(receiver.materials['server_cert']),
                  'receiver_key': str(receiver.materials['server_key'])}
    attachment = work / 'worker.json'
    attachment.write_text(json.dumps(descriptor))
    attachment.chmod(0o600)
    command = receiver.ns_command([sys.executable, str(Path(__file__).resolve()), '--case', 'webhook-ingress-roles',
                                  '--candidate-manifest', str(MANIFEST_PATH)])
    try:
        child_environment = {key: value for key, value in os.environ.items() if key.lower() not in ('http_proxy', 'https_proxy', 'all_proxy', 'no_proxy')}
        child_environment.update(PAXEER_X_WEBHOOK_HEALTHY_WORKER=str(attachment), NO_PROXY='*')
        child = subprocess.Popen(command, env=child_environment,
            stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, start_new_session=True)
        try:
            output, errors = child.communicate(timeout=900)
        except subprocess.TimeoutExpired:
            child.terminate()
            try:
                output, errors = child.communicate(timeout=45)
            except subprocess.TimeoutExpired:
                child.kill()
                output, errors = child.communicate()
        (work / 'worker.log').write_text(output + errors)
        for line in output.splitlines():
            if line.startswith(('PASS ', 'FAIL ', 'MISSING ')):
                print(line, flush=True)
                if line.startswith('PASS '):
                    results.passes += 1
                else:
                    results.failures += 1
        results.check('all real delivery fixture executions completed', child.returncode == 0)
    finally:
        receiver.stop()
        receiver._terminate(receiver.holder)


def roles_healthy_worker(manifest, results, attachment):
    import signal
    def stopped(signum, frame):
        raise SystemExit(128 + signum)
    signal.signal(signal.SIGTERM, stopped)
    import base64
    import importlib.util
    from hosted_delivery_fixture import CaMaterial, TlsRedis, binary, wait_port, private_file
    from identity_fixture import IdentityFixture
    from kms_fixture import KmsFixture
    from event_source_fixture import EventSourceFixture, PRINCIPAL
    from receipt_authority_fixture import der_pair, https, wait_until, node_environment, NETWORK_NAME
    descriptor = json.loads(protected_input(attachment, 'healthy worker attachment').read_bytes())
    if descriptor['parent_pid'] != os.getppid() or descriptor['candidate_manifest_sha256'] != MANIFEST_DIGEST \
            or descriptor['network_namespace'] != os.stat('/proc/self/ns/net').st_ino \
            or descriptor['mount_namespace'] != os.stat('/proc/self/ns/mnt').st_ino:
        raise Missing('healthy worker must be launched by this gate in its owned isolated namespace')
    work = Path(descriptor['work'])
    ca = CaMaterial.__new__(CaMaterial)
    ca.directory = Path(descriptor['ca'])
    ca.ca_pem, ca.ca_der, ca._key = ca.directory / 'ca.pem', ca.directory / 'ca.der', ca.directory / 'ca.key'
    ca._serial = 100
    specification = importlib.util.spec_from_file_location('webhook_runtime', ROOT / 'tests/daemon/paxeer_x_runtime_fixture.py')
    runtime_module = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(runtime_module)
    bundle = runtime_module.artifacts(os.environ.get('PAXEER_X_RUNTIME_ARTIFACTS'))
    client = runtime_module.client_artifact(os.environ.get('PAXEER_X_RUNTIME_CLIENT'))
    runtime_path = Path(tempfile.mkdtemp(prefix='webhook-runtime-'))
    runtime_path.rmdir()
    runtime = runtime_module.RuntimeFixture(runtime_path, bundle, client)
    redis = TlsRedis(work, ca)
    identity = IdentityFixture(work, ca)
    kms = KmsFixture(work, ca)
    ingress = public = source = receiver = None
    logs = []
    receiver_root = Path(descriptor['receiver_root'])

    def start_receiver():
        marker = receiver_root / 'receiver.ready'
        marker.unlink(missing_ok=True)
        log = (work / 'receiver-worker.log').open('ab')
        logs.append(log)
        child = subprocess.Popen([sys.executable, str(ROOT / 'tools/qualification/paxeer-x/fixtures/event_receiver_fixture.py'),
            'serve', str(receiver_root), descriptor['receiver_address'], str(descriptor['receiver_port']),
            descriptor['receiver_cert'], descriptor['receiver_key']], stdout=log, stderr=log,
            stdin=subprocess.DEVNULL, start_new_session=True)
        wait_until(lambda: marker.exists(), child, 'verified webhook receiver', 30)
        return child

    def stop_receiver(child):
        if child is not None and child.poll() is None:
            child.terminate()
            child.wait(timeout=15)

    def request(port, method, path, bearer=None, body=None, peer=None, extra=None):
        headers = dict(extra or {})
        if bearer:
            headers['Authorization'] = 'Bearer ' + bearer
        encoded = None if body is None else json.dumps(body).encode()
        if encoded is not None:
            headers['Content-Type'] = 'application/json'
        status, kind, data = https(method, f'https://localhost:{port}{path}', ca, encoded, headers, client=peer)
        return status, json.loads(data) if kind.startswith('application/json') and data else {}

    def require(label, condition):
        results.check(label, condition)
        if not condition:
            raise Missing(label)

    try:
        runtime.generate()
        results.check('real signed runtime and independent authority replica are ready', bool(runtime.readiness()))
        redis.start()
        identity.start()
        kms.start()
        tokens = private_directory(work / 'role-tokens')
        source_token = private_file(tokens / 'source', os.urandom(32).hex())
        operator_token = private_file(tokens / 'operator', os.urandom(32).hex())
        public_port, ingress_port = free_port(), free_port()
        source = EventSourceFixture(work, ca, runtime,
            {'url': f'https://localhost:{ingress_port}', 'token_file': source_token}, identity)
        source.start()
        wait_until(source.registry.ready, source.registry.process, 'genuine registry builder and event readiness', 60)
        wait_until(lambda: source._ready(source.gateway_endpoint), source.gateway.process, 'gateway and its genuine registry dependency', 60)
        require('gateway retains its real registry readiness dependency', source._ready(source.gateway_endpoint))
        session = (source.dirs['tokens'] / 'developer-session').read_text().strip()
        server = der_pair(ca, work, 'webhook-server')
        outbound = der_pair(ca, work, 'webhook-client', (), True, ())
        password = private_file(work / 'webhook-client.password', os.urandom(24).hex())
        p12 = work / 'webhook-client.p12'
        run(['openssl', 'pkcs12', '-export', '-in', outbound['cert_pem'], '-inkey', outbound['key_pem'],
             '-certfile', ca.ca_pem, '-passout', 'file:' + str(password), '-out', p12])
        p12.chmod(0o600)
        node = node_environment(runtime)
        shared = {
            'PATH': '/usr/bin:/bin', 'LAYERX_WEBHOOKS_REDIS_URL': redis.endpoint,
            'LAYERX_WEBHOOKS_REDIS_USERNAME_FILE': str(redis.materials['username']),
            'LAYERX_WEBHOOKS_REDIS_PASSWORD_FILE': str(redis.materials['password']),
            'LAYERX_WEBHOOKS_INTERNAL_CA_DER': str(ca.ca_der), 'LAYERX_WEBHOOKS_PUBLIC_CA_DER': str(ca.ca_der),
            'LAYERX_WEBHOOKS_CLIENT_IDENTITY_PKCS12': str(p12),
            'LAYERX_WEBHOOKS_CLIENT_IDENTITY_PASSWORD_FILE': str(password),
            'LAYERX_WEBHOOKS_KMS_URL': kms.endpoint, 'LAYERX_WEBHOOKS_KMS_TOKEN_FILE': str(kms.token_file),
            'LAYERX_WEBHOOKS_CURSOR_KEY_FILE': str(private_file(work / 'cursor.key', os.urandom(32).hex())),
            'LAYERX_WEBHOOKS_TLS_CERT_DER': str(server['cert_der']),
            'LAYERX_WEBHOOKS_TLS_KEY_DER': str(server['key_der']), 'LAYERX_WEBHOOKS_LISTENER': 'tls',
            'LAYERX_WEBHOOKS_INSTANCE_ID': 'ingress-real-delivery',
            'LAYERX_WEBHOOKS_NETWORK_ID': NETWORK_NAME, 'LAYERX_WEBHOOKS_LXP_WIRE_VERSION': '3',
        }
        public_env = dict(shared, LAYERX_WEBHOOKS_ROLE='public', LAYERX_WEBHOOKS_LISTEN=f'127.0.0.1:{public_port}',
            LAYERX_WEBHOOKS_IDENTITY_URL=identity.endpoint,
            LAYERX_WEBHOOKS_IDENTITY_TOKEN_FILE=str(identity.service_tokens['layerx-webhooks']))
        ingress_env = dict(shared, LAYERX_WEBHOOKS_ROLE='ingress', LAYERX_WEBHOOKS_LISTEN=f'127.0.0.1:{ingress_port}',
            LAYERX_WEBHOOKS_INGRESS_CLIENT_CA_DER=str(ca.ca_der),
            LAYERX_WEBHOOKS_SOURCE_TRIGGER_TOKEN_FILE=str(source_token),
            LAYERX_WEBHOOKS_OPERATOR_TOKEN_FILE=str(operator_token),
            LAYERX_WEBHOOKS_COMPONENT_URL=source.boundary_endpoint,
            LAYERX_WEBHOOKS_COMPONENT_TOKEN_FILE=str(source.boundary_tokens['webhook']),
            LAYERX_WEBHOOKS_AUTHORITY_URL=source.authority.endpoint,
            LAYERX_WEBHOOKS_AUTHORITY_TOKEN_FILE=str(source.authority.token_files['webhooks']))
        public_key, sequencer = source._sequencer()
        for key, value in {'SEQUENCER_PUBLIC_KEY': public_key, 'SEQUENCER_ID': sequencer,
                           'SEQUENCER_FIRST_BATCH': node['LAYERX_NODE_FIRST_BATCH'],
                           'SEQUENCER_LAST_BATCH': node['LAYERX_NODE_LAST_BATCH']}.items():
            ingress_env['LAYERX_WEBHOOKS_' + key + '_FILE'] = str(private_file(work / key.lower(), value))
        for kind in ('JOURNEY', 'PAYMENT', 'APPROVAL', 'PROGRAM'):
            ingress_env[f'LAYERX_WEBHOOKS_{kind}_SOURCE_URL'] = source.endpoint
            ingress_env[f'LAYERX_WEBHOOKS_{kind}_SOURCE_TOKEN_FILE'] = str(source.source_token_file)
        public = roles_Process(str(binary('layerx-webhooks')), public_env, work / 'public.log')
        ingress = roles_Process(str(binary('layerx-webhooks')), ingress_env, work / 'ingress.log')
        operator_dir = work / 'local-operator'
        issued = roles_ca_sh(['issue-local', 'webhook-operator-client', '--output-dir', str(operator_dir)], ca.directory, check=False)
        require('healthy dispatch identity comes from fixed local operator issuance', issued.returncode == 0)
        operator = {'cert_pem': operator_dir / 'cert.pem', 'key_pem': operator_dir / 'key.pem'}
        producer = source.producer_client
        wait_until(lambda: request(ingress_port, 'GET', '/healthz', peer=producer)[0] == 200,
                   ingress.child, 'all real ingress dependencies', 60)
        require('real public delivery state and KMS are healthy', request(public_port, 'GET', '/healthz')[0] == 200)
        status, registration = request(public_port, 'POST', '/v1/webhooks/endpoints', session,
            {'url': descriptor['receiver_endpoint'], 'kinds': ['payment'], 'minimum_verification': 'receipt-verified'},
            extra={'Idempotency-Key': 'real-ingress-registration'})
        require('real developer registration binds a KMS key and durable endpoint', status == 201 and bool(registration.get('endpoint')))
        private_file(receiver_root / 'public-keys.json', registration['public_keys_json'])
        receiver = start_receiver()
        status, _ = request(public_port, 'POST', '/v1/webhooks/endpoints', session,
            {'url': 'https://127.0.0.1:8443/events', 'kinds': ['payment']}, extra={'Idempotency-Key': 'private-destination-refused'})
        require('destination guard still rejects a loopback endpoint', status not in (200, 201, 202))
        first = source.produce_event('payment')
        def effect(identifier):
            return (receiver_root / 'effects' / identifier).exists()
        wait_until(lambda: effect(first), ingress.child, 'verified KMS-signed first delivery', 90)
        body = (receiver_root / 'effects' / first).read_bytes()
        require('first effect came from the real canonical source', source.read_event(first)[1].get('id') == first)
        status, duplicate = request(ingress_port, 'POST', '/internal/v1/events/payment/' + first,
            source_token.read_text().strip(), {}, producer)
        require('canonical duplicate publication preserves its durable position', status == 202 and duplicate.get('duplicate') is True)
        status, ledger = request(public_port, 'GET', '/v1/webhooks/events', session)
        require('only one canonical first event is stored', status == 200 and sum(item['id'] == first for item in ledger) == 1)
        stop_receiver(receiver)
        receiver = None
        second = source.produce_event('payment')
        wait_until(lambda: request(public_port, 'GET', '/v1/webhooks/events', session)[0] == 200 and
                   any(item['id'] == second for item in request(public_port, 'GET', '/v1/webhooks/events', session)[1]),
                   ingress.child, 'durable delivery during destination outage', 60)
        ingress.stop(); public.stop()
        redis.restart(); kms.restart(); identity.restart()
        public = roles_Process(str(binary('layerx-webhooks')), public_env, work / 'public-restarted.log')
        ingress = roles_Process(str(binary('layerx-webhooks')), ingress_env, work / 'ingress-restarted.log')
        receiver = start_receiver()
        wait_until(lambda: effect(second), ingress.child, 'retained delivery after real store and process restart', 180)
        require('restart preserves the acknowledged first effect bytes', (receiver_root / 'effects' / first).read_bytes() == body)
        for path in ('/internal/v1/dispatch', '/internal/v1/events/payment/' + second):
            require('public TLS refuses private routes after durable restart', request(public_port, 'POST', path,
                source_token.read_text().strip(), {})[0] == 404)
        rotated_ca = CaMaterial(private_directory(work / 'rotation'))
        rotated_producer = der_pair(rotated_ca, work, 'rotated-producer', [PRODUCER], True, ())
        rotated_operator_dir = work / 'rotated-local-operator'
        issued = roles_ca_sh(['issue-local', 'webhook-operator-client', '--output-dir', str(rotated_operator_dir)],
                             rotated_ca.directory, check=False)
        require('rotation issues the operator through its fixed policy', issued.returncode == 0)
        rotated_operator = {'cert_pem': rotated_operator_dir / 'cert.pem', 'key_pem': rotated_operator_dir / 'key.pem'}
        new_source = private_file(tokens / 'source-rotated', os.urandom(32).hex())
        new_operator = private_file(tokens / 'operator-rotated', os.urandom(32).hex())
        ingress.stop()
        ingress_env.update(LAYERX_WEBHOOKS_INGRESS_CLIENT_CA_DER=str(rotated_ca.ca_der),
            LAYERX_WEBHOOKS_SOURCE_TRIGGER_TOKEN_FILE=str(new_source), LAYERX_WEBHOOKS_OPERATOR_TOKEN_FILE=str(new_operator))
        ingress = roles_Process(str(binary('layerx-webhooks')), ingress_env, work / 'ingress-rotated.log')
        try:
            request(ingress_port, 'POST', '/internal/v1/events/payment/' + first, new_source.read_text().strip(), {}, producer)
            old_refused = False
        except (OSError, ssl.SSLError):
            old_refused = True
        require('rotation refuses the previous producer certificate', old_refused)
        require('rotation refuses the previous source token', request(ingress_port, 'POST',
            '/internal/v1/events/payment/' + first, source_token.read_text().strip(), {}, rotated_producer)[0] == 401)
        require('rotation refuses the previous operator token', request(ingress_port, 'POST',
            '/internal/v1/dispatch', operator_token.read_text().strip(), {}, rotated_operator)[0] == 401)
        require('producer role cannot dispatch with the rotated operator bearer', request(ingress_port, 'POST',
            '/internal/v1/dispatch', new_operator.read_text().strip(), {}, rotated_producer)[0] == 403)
        rotated_password = private_file(tokens / 'client-rotated.password', os.urandom(24).hex())
        rotated_p12 = tokens / 'client-rotated.p12'
        run(['openssl', 'pkcs12', '-export', '-in', rotated_producer['cert_pem'], '-inkey', rotated_producer['key_pem'],
             '-passout', 'file:' + str(rotated_password), '-out', rotated_p12])
        source.ingress.update(token_file=new_source, client_identity=rotated_p12, client_password=rotated_password)
        source.gateway.stop()
        source.gateway.start()
        third = source.produce_event('payment')
        wait_until(lambda: effect(third), ingress.child, 'coordinated credential rotation with real producer', 90)
        status, duplicate = request(ingress_port, 'POST', '/internal/v1/events/payment/' + third,
            new_source.read_text().strip(), {}, rotated_producer)
        require('rotated exact retry reuses its canonical event', status == 202 and duplicate.get('duplicate') is True)
        status, dispatch = request(ingress_port, 'POST', '/internal/v1/dispatch', new_operator.read_text().strip(), {}, rotated_operator)
        require('rotated operator dispatch uses the real durable delivery service', status == 200 and isinstance(dispatch, dict))
        status, final = request(public_port, 'GET', '/v1/webhooks/events', session)
        require('restart and rotation retain exactly the three real payment events', status == 200 and
                sorted(item['id'] for item in final) == sorted((first, second, third)))
        effects = sorted(path.name for path in (receiver_root / 'effects').iterdir())
        require('verified receiver applies each canonical event effect once', effects == sorted((first, second, third)))
        records = [json.loads(path.read_text()) for path in sorted((receiver_root / 'received').glob('*.json'))]
        require('every delivered body has a verified production KMS signature', bool(records) and all(item['verified'] for item in records))
        stop_receiver(receiver)
        receiver = start_receiver()
        replayed = records[-1]
        replay_body = base64.b64decode(replayed['body'], validate=True)
        replay_headers = {key: value for key, value in replayed['headers'].items() if key.startswith('layerx-webhook-')}
        replay_headers['Content-Type'] = 'application/json'
        status, _, _ = https('POST', descriptor['receiver_endpoint'], ca, replay_body, replay_headers)
        require('shipped receiver verifies and deduplicates an exact signed redelivery after restart', status == 200 and
                sorted(path.name for path in (receiver_root / 'effects').iterdir()) == effects)
        status, _, _ = https('POST', descriptor['receiver_endpoint'], ca, replay_body + b' ', replay_headers)
        require('shipped receiver refuses a changed body under the original signature', status == 401)
        stale_headers = dict(replay_headers)
        stale_headers['layerx-webhook-timestamp'] = str(int(stale_headers['layerx-webhook-timestamp']) - 3600)
        status, _, _ = https('POST', descriptor['receiver_endpoint'], ca, replay_body, stale_headers)
        require('shipped receiver refuses a stale signed delivery', status == 401)

        for kind, stem in ((source.authority, 'receipt authority'), (kms, 'KMS')):
            kind.stop()
            if kind is source.authority:
                require('canonical receipt retrieval fails closed when authority is unavailable', request(ingress_port, 'POST',
                    '/internal/v1/events/payment/' + first, new_source.read_text().strip(), {}, rotated_producer)[0] == 503)
            require('unavailable real ' + stem + ' cannot report healthy ingress',
                    request(ingress_port, 'GET', '/healthz', peer=rotated_producer)[0] == 503)
            kind.start()
        wait_until(lambda: request(ingress_port, 'GET', '/healthz', peer=rotated_producer)[0] == 200,
                   ingress.child, 'restored real ingress dependencies', 60)
    finally:
        stop_receiver(receiver)
        if ingress is not None: ingress.stop()
        if public is not None: public.stop()
        if source is not None: source.stop()
        kms.stop(); identity.stop(); redis.stop(); runtime.cleanup()
        for log in logs: log.close()




def provisioning_check(results, label, condition):
    results.check('principal provisioning: ' + label, condition)
    if not condition:
        raise Missing('principal provisioning: ' + label)


def provisioning_inputs(manifest):
    location = os.environ.get('PAXEER_X_EVENT_PROVISIONING')
    if not location:
        raise Missing('PAXEER_X_EVENT_PROVISIONING is required: genuine Human onboarding dependencies and candidate-bound prebuilt services')
    document = json.loads(protected_input(location, 'principal provisioning fixture').read_bytes())
    if document.get('schema') != 'paxeer-x.event-principal-provisioning.v1' \
            or document.get('candidate_manifest_sha256') != MANIFEST_DIGEST \
            or document.get('source_revision') != manifest['source']['revision'] \
            or document.get('scope') != 'isolated-real-process':
        raise Missing('principal provisioning fixture must bind this isolated candidate')
    root = private_directory(Path(document['isolation_root']).resolve(strict=True))
    if root == Path('/'):
        raise Missing('isolated writable root required')
    required = {'kms', 'journeys', 'approvals', 'payments', 'programs', 'identity',
                'human-components', 'human-service', 'webhook-public', 'webhook-ingress'}
    services = document['services']
    if not required <= set(services) or len(services) > 40:
        raise Missing('all five internal groups, real Human issuer, identity and webhook roles are required')
    allowed = {'layerx-kms', 'layerx-event-source', 'layerx-identity', 'layerx-human-components',
               'layerx-human-service', 'layerx-webhooks', 'layerx-gateway', 'layerx-program-registry',
               'layerx-agent-boundary', 'layerx-receipt-authority', 'layerxd', 'paxd', 'redis-server',
               'layerx-guarantor', 'layerx-human-kms', 'layerx-human-security-provider',
               'layerx-human-movement-provider', 'layerx-human-identity-provider', 'layerx-human-onboarding',
               'layerx-agentd', 'layerx-identity-binding', 'layerx-remote-signer'}
    artifacts = document['artifacts']
    for name, artifact in artifacts.items():
        if name not in allowed | {'layerx-runtime-clock', 'node'}:
            raise Missing('undeclared executable in principal provisioning fixture')
        path = Path(artifact['path'])
        if not path.is_absolute() or not path.is_file() or path.is_symlink() or not os.access(path, os.X_OK):
            raise Missing('missing prebuilt provisioning executable: ' + name)
        with path.open('rb') as handle:
            digest = hashlib.file_digest(handle, 'sha256').hexdigest()
        if digest != artifact['sha256'] or artifact['source_revision'] != manifest['source']['revision']:
            raise Missing('prebuilt provisioning executable is not candidate-bound: ' + name)
    if not {'layerx-runtime-clock', 'node'} <= set(artifacts):
        raise Missing('prebuilt runtime clock and Node with native TypeScript support are required')
    states = {}
    expected_phases = {'kms': 'internal', 'journeys': 'internal', 'approvals': 'internal',
                       'payments': 'internal', 'programs': 'internal', 'identity': 'dependency',
                       'human-components': 'human', 'human-service': 'human',
                       'webhook-public': 'webhook', 'webhook-ingress': 'webhook'}
    for name, service in services.items():
        if service['binary'] not in allowed or service['binary'] not in artifacts:
            raise Missing('service is not a bound production executable: ' + name)
        if service['phase'] not in ('dependency', 'internal', 'human', 'webhook'):
            raise Missing('explicit dependency phase required: ' + name)
        if name in expected_phases and service['phase'] != expected_phases[name]:
            raise Missing('production bootstrap dependency ordering mismatch: ' + name)
        env = json.loads(protected_input(service['environment_file'], name + ' protected configuration').read_bytes())
        if not isinstance(env, dict) or not all(isinstance(k, str) and isinstance(v, str) for k, v in env.items()):
            raise Missing('bounded environment object required')
        for key, value in env.items():
            if any(ord(char) < 32 for char in key + value):
                raise Missing('configuration contains control characters')
            if re.search(r'(TOKEN|PASSWORD|SECRET|PRIVATE_KEY)$', key):
                raise Missing('credentials must use protected references: ' + key)
            if key.endswith(('_FILE', '_DER', '_PKCS12')):
                if name in ('journeys', 'approvals') and key == 'LAYERX_EVENTS_CREDENTIALS_FILE':
                    if root not in Path(value).resolve().parents:
                        raise Missing('generated source enrollment is outside the owned root')
                else:
                    protected_input(value, name + ' protected reference')
        for path in service['state_directories']:
            selected = Path(path).resolve()
            if root not in selected.parents or selected.is_symlink():
                raise Missing('production persistence is outside the owned isolation root')
        service['environment'] = env
        service['arguments'] = service.get('arguments', [])
        if not isinstance(service['arguments'], list) or any(not isinstance(v, str) or '\x00' in v for v in service['arguments']):
            raise Missing('production arguments must be a bounded argument vector')
        if len(service['arguments']) > 128:
            raise Missing('production argument bound')
        if name in ('kms', *KINDS):
            variable = 'LAYERX_KMS_STATE_DIR' if name == 'kms' else 'LAYERX_EVENTS_STATE_DIR'
            state = Path(env[variable]).resolve()
            if str(state) not in service['state_directories'] or root not in state.parents:
                raise Missing('internal group persistence binding missing')
            states[name] = state
            if state.exists() and any(state.iterdir()):
                raise Missing('fresh-volume case requires empty internal persistent directories')
            if name != 'kms':
                if env.get('LAYERX_EVENTS_KIND') != name or not env.get('LAYERX_EVENTS_LISTEN', '').startswith('127.0.0.1:'):
                    raise Missing('internal source kind or private listener mismatch')
                for key in ('LAYERX_EVENTS_TLS_CERT_DER', 'LAYERX_EVENTS_TLS_KEY_DER',
                            'LAYERX_EVENTS_CLIENT_CA_DER', 'LAYERX_EVENTS_UPSTREAM_CA_DER',
                            'LAYERX_EVENTS_ENROLLMENT_KEY_FILE', 'LAYERX_EVENTS_PRODUCERS_FILE'):
                    protected_input(env[key], name + ' mandatory mTLS/enrollment material')
            else:
                if not env.get('LAYERX_KMS_LISTEN', '').startswith('127.0.0.1:'):
                    raise Missing('KMS must retain its private listener')
                for key in ('TLS_CERT_DER', 'TLS_KEY_DER', 'CLIENT_CA_DER', 'TOKEN_FILE', 'SEAL_SECRET_FILE'):
                    protected_input(env['LAYERX_KMS_' + key], 'KMS mandatory protected material')
    if len(set(states.values())) != 5 or any(a in b.parents or b in a.parents for a in states.values() for b in states.values() if a != b):
        raise Missing('five internal groups require independent persistent volumes')
    human = services['human-components']['environment']
    for key in ('LAYERX_HUMAN_STORE_ROOT', 'LAYERX_HUMAN_CUSTODY_ROOT', 'LAYERX_HUMAN_AUTH_INDEX_ROOT'):
        path = Path(human[key]).resolve()
        if root not in path.parents:
            raise Missing('Human issuer must use owned production bootstrap volumes')
    if services['human-service']['environment'].get('LAYERX_HUMAN_LISTENER') != 'tls':
        raise Missing('real Human issuer must serve TLS')
    for name in ('webhook-ingress', 'webhook-public'):
        env = services[name]['environment']
        if env.get('LAYERX_WEBHOOKS_ROLE') != name.removeprefix('webhook-') \
                or env.get('LAYERX_WEBHOOKS_LISTENER') != 'tls' \
                or not env.get('LAYERX_WEBHOOKS_LISTEN', '').startswith('127.0.0.1:'):
            raise Missing('webhook roles must remain separate private TLS processes')
        for key in ('REDIS_USERNAME_FILE', 'REDIS_PASSWORD_FILE', 'KMS_TOKEN_FILE',
                    'INTERNAL_CA_DER', 'PUBLIC_CA_DER', 'CLIENT_IDENTITY_PKCS12',
                    'CLIENT_IDENTITY_PASSWORD_FILE', 'CURSOR_KEY_FILE'):
            protected_input(env['LAYERX_WEBHOOKS_' + key], 'webhook mandatory dependency')
    ingress = services['webhook-ingress']['environment']
    for key in ('INGRESS_CLIENT_CA_DER', 'COMPONENT_TOKEN_FILE', 'AUTHORITY_TOKEN_FILE',
                'SOURCE_TRIGGER_TOKEN_FILE', 'OPERATOR_TOKEN_FILE', 'SEQUENCER_PUBLIC_KEY_FILE',
                'SEQUENCER_ID_FILE', 'SEQUENCER_FIRST_BATCH_FILE', 'SEQUENCER_LAST_BATCH_FILE',
                'JOURNEY_SOURCE_TOKEN_FILE', 'APPROVAL_SOURCE_TOKEN_FILE',
                'PAYMENT_SOURCE_TOKEN_FILE', 'PROGRAM_SOURCE_TOKEN_FILE'):
        protected_input(ingress['LAYERX_WEBHOOKS_' + key], 'webhook protected trust material')
    if not ingress.get('LAYERX_WEBHOOKS_NETWORK_ID') or ingress.get('LAYERX_WEBHOOKS_LXP_WIRE_VERSION') != '3':
        raise Missing('webhook exact network/protocol pins required')
    protected_input(services['webhook-public']['environment']['LAYERX_WEBHOOKS_IDENTITY_TOKEN_FILE'],
                    'webhook authenticated identity reference')
    if document['human']['url'] != 'https://localhost':
        raise Missing('owned Human HTTPS origin must be https://localhost on isolated port 443')
    for kind in ('journeys', 'approvals'):
        if Path(services[kind]['environment']['LAYERX_EVENTS_CREDENTIALS_FILE']).exists():
            raise Missing('fresh source credentials must be installed by the real Human session issuer')
    for phase in ('fresh', 'restored', 'rotated'):
        if set(document['mutations'][phase]) != {'journey', 'approval'}:
            raise Missing('every phase requires both genuine journey and approval mutations')
        for requests in document['mutations'][phase].values():
            if not isinstance(requests, list) or len(requests) != 2:
                raise Missing('two actual transitions per subject are required in every phase')
            for request in requests:
                if request['method'] != 'POST' or not request['path'].startswith('/v1/'):
                    raise Missing('events must originate in genuine public Human mutation routes')
                protected_input(request['body_file'], 'real Human mutation body')
    if run(['git', '-C', ROOT, 'rev-parse', 'HEAD']).stdout.strip() != document['source_revision'] \
            or run(['git', '-C', ROOT, 'status', '--porcelain']).stdout:
        raise Missing('provisioning requires the clean final candidate source')
    return document


class ProvisioningHttp:
    def __init__(self, endpoint):
        self.endpoint = endpoint
        self.cookies = {}
        self.last_cookie_attributes = {}

    def request(self, method, path, body=None, headers=None, client_identity=True):
        from http.cookies import SimpleCookie
        endpoint = urlsplit(self.endpoint['url'])
        if endpoint.scheme != 'https' or endpoint.hostname not in ('localhost', '127.0.0.1') \
                or endpoint.username or endpoint.password or endpoint.path not in ('', '/'):
            raise Missing('provisioning APIs must be real TLS listeners inside the owned namespace')
        if not path.startswith('/') or path.startswith('//') or any(c in path for c in '\r\n'):
            raise Missing('invalid production request path')
        context = ssl.create_default_context(cafile=str(protected_input(self.endpoint['ca_pem'], 'TLS root')))
        if client_identity and 'client_cert_pem' in self.endpoint:
            context.load_cert_chain(str(protected_input(self.endpoint['client_cert_pem'], 'mTLS certificate')),
                                    str(protected_input(self.endpoint['client_key_pem'], 'mTLS private key')))
        request_headers = {'Origin': self.endpoint['url']}
        for name, location in self.endpoint.get('header_files', {}).items():
            value = protected_input(location, 'authenticated API credential').read_text().strip()
            if any(ord(c) < 32 or ord(c) == 127 for c in value):
                raise Missing('invalid protected header')
            request_headers[name] = value
        if self.cookies:
            request_headers['Cookie'] = '; '.join(k + '=' + v for k, v in self.cookies.items())
        if '__Host-layerx_csrf' in self.cookies:
            request_headers['X-LayerX-CSRF'] = self.cookies['__Host-layerx_csrf']
        request_headers.update(headers or {})
        encoded = None if body is None else json.dumps(body, separators=(',', ':')).encode()
        if encoded is not None:
            request_headers['Content-Type'] = 'application/json'
        connection = http.client.HTTPSConnection(endpoint.hostname, endpoint.port or 443, context=context, timeout=25)
        try:
            connection.request(method, path, encoded, request_headers)
            response = connection.getresponse()
            raw = response.read(1_048_577)
            if len(raw) > 1_048_576:
                raise Missing('production response bound exceeded')
            for name, value in response.getheaders():
                if name.lower() != 'set-cookie':
                    continue
                parsed = SimpleCookie()
                parsed.load(value)
                for cookie_name, morsel in parsed.items():
                    if cookie_name not in ('__Host-layerx_access', '__Host-layerx_refresh', '__Host-layerx_csrf'):
                        continue
                    if not morsel['secure'] or morsel['path'] != '/' or morsel['domain'] \
                            or morsel['samesite'].lower() != 'strict' \
                            or (cookie_name != '__Host-layerx_csrf' and not morsel['httponly']):
                        raise Missing('issued Human cookie security attributes refused')
                    self.cookies[cookie_name] = morsel.value
                    self.last_cookie_attributes[cookie_name] = dict(morsel)
            return response.status, json.loads(raw) if raw else {}
        finally:
            connection.close()


class ProvisioningProcesses:
    def __init__(self, fixture, work):
        self.fixture, self.work = fixture, work
        self.children, self.logs = {}, []
        self.clock_root = Path(tempfile.mkdtemp(prefix='lxpc-'))
        self.clock_root.chmod(0o700)

    def start(self, name):
        if name in self.children:
            raise Missing('process already owned: ' + name)
        service = self.fixture['services'][name]
        env = dict(service['environment'])
        env.setdefault('PATH', '/usr/local/bin:/usr/bin:/bin')
        clock = self.clock_root / str(list(self.fixture['services']).index(name))
        clock.mkdir(mode=0o700, exist_ok=True)
        binary = self.fixture['artifacts'][service['binary']]['path']
        command = [self.fixture['artifacts']['layerx-runtime-clock']['path'], '--runtime-dir', str(clock), '--',
                   binary, *service['arguments']]
        log = (self.work / (name + '.log')).open('ab')
        self.logs.append(log)
        self.children[name] = subprocess.Popen(command, env=env, stdin=subprocess.DEVNULL,
                                              stdout=log, stderr=log, start_new_session=True)

    def stop(self, name):
        child = self.children.pop(name)
        if child.poll() is not None:
            raise Missing('production process exited unexpectedly: ' + name)
        child.terminate()
        child.wait(timeout=15)
        if child.returncode not in (0, 1, 143):
            raise Missing('production process refused orderly shutdown: ' + name)

    def alive(self):
        if not self.children or any(child.poll() is not None for child in self.children.values()):
            raise Missing('owned production dependency exited')

    def wait(self, predicate, label, seconds=90):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            self.alive()
            try:
                value = predicate()
                if value:
                    return value
            except (OSError, ValueError):
                pass
            time.sleep(.2)
        raise Missing('real-process deadline: ' + label)

    def close(self):
        import signal
        for child in reversed(list(self.children.values())):
            if child.poll() is None:
                child.terminate()
                try:
                    child.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    os.killpg(child.pid, signal.SIGKILL)
                    child.wait(timeout=10)
        for log in self.logs:
            log.close()


def provisioning_worker(manifest, results, attachment, readiness=False):
    import base64
    import select
    import signal
    import uuid
    descriptor = json.loads(protected_input(attachment, 'provisioning namespace attachment').read_bytes())
    if descriptor['parent_pid'] != os.getppid() or descriptor['candidate_manifest_sha256'] != MANIFEST_DIGEST \
            or descriptor['network_namespace'] != os.stat('/proc/self/ns/net').st_ino \
            or descriptor['mount_namespace'] != os.stat('/proc/self/ns/mnt').st_ino:
        raise Missing('principal provisioning worker must run inside this gate-owned namespace')
    fixture = provisioning_inputs(manifest)
    work = Path(descriptor['work'])
    processes = ProvisioningProcesses(fixture, work)
    human = ProvisioningHttp(fixture['human'])
    source_clients = {kind: ProvisioningHttp(fixture['sources'][kind]) for kind in ('journeys', 'approvals')}
    public = ProvisioningHttp(fixture['webhook_public'])
    identity = ProvisioningHttp(fixture['identity_provisioning'])
    receiver = authenticator = None
    receiver_log = auth_log = None
    cases, bindings, observed_events = [], {}, []

    def check(label, value):
        provisioning_check(results, label, value)
        cases.append(label)

    def human_ok(method, path, body=None, headers=None):
        status, value = human.request(method, path, body, headers)
        check('genuine Human ' + method + ' ' + path + ' accepted', status in (200, 201, 202)
              and isinstance(value.get('result'), dict))
        return value['result']

    def binding(kind):
        status, value = source_clients[kind].request('GET', '/internal/v1/principals/' + principal + '/issued-enrollment')
        return value if status == 200 and value.get('bound') is True else None

    def identity_bound(value):
        return value and value.get('principal') == principal and value.get('tenant') == tenant \
            and value.get('session_id') == session['session_id'] \
            and isinstance(value.get('revision'), int) and value['revision'] > 0 \
            and isinstance(value.get('generation'), int) and value['generation'] > 0

    def assertion(operation, ceremony):
        authenticator.stdin.write(json.dumps({'operation': operation, 'ceremony': ceremony}) + '\n')
        authenticator.stdin.flush()
        if not select.select([authenticator.stdout], [], [], 20)[0]:
            raise Missing('real WebAuthn authenticator deadline')
        raw = authenticator.stdout.readline(131073)
        if len(raw) > 131072 or not raw.endswith('\n'):
            raise Missing('bounded real WebAuthn response required')
        value = json.loads(raw)
        if set(value) != {'credential'} or not isinstance(value['credential'], str):
            raise Missing('real authenticator did not return its signed credential')
        return value['credential']

    def records():
        output = []
        for path in sorted((Path(descriptor['receiver_root']) / 'received').glob('*.json')):
            record = json.loads(path.read_bytes())
            if record.get('verified') is not True:
                raise Missing('receiver did not verify the production KMS signature')
            body = json.loads(base64.b64decode(record['body'], validate=True))
            output.append((record, body['event']))
        return output

    def replace(value, values):
        if isinstance(value, str):
            for key, replacement in values.items():
                value = value.replace('${' + key + '}', replacement)
            if '${' in value:
                raise Missing('mutation references no genuine response binding')
            return value
        if isinstance(value, dict):
            return {key: replace(item, values) for key, item in value.items()}
        if isinstance(value, list):
            return [replace(item, values) for item in value]
        return value

    def pointer(value, path):
        if not path.startswith('/'):
            raise Missing('actual response resource JSON pointer required')
        for field in path[1:].split('/'):
            value = value[field.replace('~1', '/').replace('~0', '~')]
        if not isinstance(value, str) or not value:
            raise Missing('real mutation returned no resource identity')
        return value

    def delivery_phase(phase):
        for kind in ('journey', 'approval'):
            resource = None
            for index, request in enumerate(fixture['mutations'][phase][kind]):
                values = {'principal': principal, 'tenant': tenant, 'account': account['account_id']}
                if resource is not None:
                    values['resource'] = resource
                body = replace(json.loads(protected_input(request['body_file'], 'Human mutation').read_bytes()), values)
                path = replace(request['path'], values)
                result = human_ok('POST', path, body, {'Idempotency-Key': str(uuid.uuid4())})
                returned = pointer(result, request['resource_pointer'])
                if resource is None:
                    resource = returned
                check(phase + ' ' + kind + ' retains the real subject', returned == resource)
                def delivered():
                    matching = [(record, event) for record, event in records()
                                if event.get('kind') == kind and event.get('subject') == resource
                                and not record.get('duplicate')]
                    return matching if len(matching) == index + 1 else None
                current = processes.wait(delivered, phase + ' ordered signed ' + kind)
                event = current[-1][1]
                source_token = protected_input(fixture['services'][kind + 's']['environment']['LAYERX_EVENTS_TOKEN_FILE'],
                                               'canonical source reader').read_text().strip()
                status, canonical = source_clients[kind + 's'].request('GET', '/internal/v1/events/' + event['id'],
                                               headers={'Authorization': 'Bearer ' + source_token})
                check(phase + ' ' + kind + ' delivery equals canonical authenticated source', status == 200
                      and canonical['id'] == event['id'] and canonical['principal'] == principal
                      and canonical['subject'] == resource and canonical['subject_sequence'] == event['subject_sequence'])
                check(phase + ' ' + kind + ' canonical event has expected derived identity',
                      canonical['id'] == event_id(kind, resource, canonical['subject_sequence']))
                observed_events.append(event['id'])
            check(phase + ' ' + kind + ' strict delivery order', len(current) == 2
                  and current[0][1]['subject_sequence'] + 1 == current[1][1]['subject_sequence']
                  and current[0][0]['sequence'] < current[1][0]['sequence'])

    try:
        for kind in ('journeys', 'approvals'):
            env = fixture['services'][kind]['environment']
            source_binary = fixture['artifacts']['layerx-event-source']['path']
            signed = run([source_binary, '--empty-enrollment-mac'], env)
            mac = signed.stdout.strip()
            if not re.fullmatch('[0-9a-f]{64}', mac):
                raise Missing('production source initializer refused empty bootstrap enrollment')
            snapshot = Path(env['LAYERX_EVENTS_CREDENTIALS_FILE'])
            if snapshot.exists():
                raise Missing('fresh bootstrap snapshot unexpectedly exists')
            private_directory(snapshot.parent)
            with snapshot.open('x') as handle:
                handle.write(json.dumps({'version': 1, 'generation': 0, 'principals': [], 'mac': mac}))
                handle.flush()
                os.fsync(handle.fileno())
            snapshot.chmod(0o600)
        for phase in ('dependency', 'internal'):
            for name, service in fixture['services'].items():
                if service['phase'] == phase:
                    processes.start(name)
        for kind, client in source_clients.items():
            state = processes.wait(lambda: (v if (v := client.request('GET', '/readyz'))[0] == 503 else None),
                                   kind + ' waiting before Human')
            check(kind + ' empty fresh volume is explicitly waiting for principals', state[1].get('ready') is False
                  and state[1].get('state') == 'waiting-principals')
            check(kind + ' source is independently live before Human', client.request('GET', '/livez')[0] == 200)
            status, refusal = client.request('POST', '/internal/v1/observe', {})
            check(kind + ' cannot accept first events before principal issuance', status == 503
                  and refusal.get('error', {}).get('code') == 'waiting_principals')
        for name, service in fixture['services'].items():
            if service['phase'] in ('human', 'webhook'):
                processes.start(name)
        processes.wait(lambda: human.request('GET', '/livez')[0] == 200, 'genuine Human issuer listener')
        if readiness:
            producer_readiness_empty(fixture, processes, human, source_clients, check)
        auth_log = (work / 'authenticator.log').open('ab')
        authenticator = subprocess.Popen([fixture['artifacts']['node']['path'], '--experimental-strip-types',
            str(ROOT / 'human/apps/web/e2e/software-authenticator.ts'), 'https://localhost'],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=auth_log, text=True, bufsize=1)
        email = 'event-' + uuid.uuid4().hex + '@paxeer.network'
        account = human_ok('POST', '/v1/accounts', {'email': email, 'display_name': 'Event qualification'},
                           {'Idempotency-Key': str(uuid.uuid4())})
        registration = human_ok('POST', '/v1/passkeys/registrations', {'account_id': account['account_id']})
        passkey = human_ok('POST', '/v1/passkeys/registrations/' + registration['registration_id'],
                          {'credential': assertion('register', registration['ceremony'])})
        challenge = human_ok('POST', '/v1/passkeys/assertions', {'email': email})
        proof = human_ok('POST', '/v1/passkeys/assertions/' + challenge['assertion_id'],
                        {'credential': assertion('assert', challenge['ceremony'])})
        check('session authority came from a verified registered passkey', proof['passkey_id'] == passkey['passkey_id']
              and proof['assertion_id'] == challenge['assertion_id'])
        opening = {'assertion_id': proof['assertion_id'], 'device': {'label': 'Event qualification', 'platform': 'WebAuthn'}}
        opening_key = str(uuid.uuid4())
        session = human_ok('POST', '/v1/sessions', opening, {'Idempotency-Key': opening_key})
        check('Human issuer installed its three protected session cookies',
              set(human.cookies) == {'__Host-layerx_access', '__Host-layerx_refresh', '__Host-layerx_csrf'}
              and session.get('current') is True)
        who = human_ok('GET', '/internal/v1/principal')
        principal, tenant = who['sub'], who['tenant_id']
        check('authenticated Human principal includes exact tenant and session binding', who.get('active') is True
              and who.get('session_id') == session['session_id'] and bool(principal) and bool(tenant))
        if readiness:
            producer_readiness_checks(manifest, fixture, processes, work, principal, human, check)
            return
        for kind in source_clients:
            bindings[kind] = processes.wait(lambda kind=kind: (v if identity_bound(v := binding(kind)) else None),
                                            kind + ' automatic authorized enrollment')
            check(kind + ' ready only after authenticated upstream admission',
                  source_clients[kind].request('GET', '/readyz')[0] == 200)
        for kind in ('payments', 'programs'):
            client = ProvisioningHttp(fixture['sources'][kind])
            owner = fixture['source_owners'][kind]
            response = processes.wait(lambda: (v if (v := client.request('GET',
                '/internal/v1/principals/' + owner['principal'] + '/enrollment'))[0] == 200
                and v[1].get('bound') is True else None), kind + ' actual upstream ownership')
            check(kind + ' retains separately declared authenticated upstream ownership',
                  response[1].get('principal') == owner['principal'] and owner['tenant']
                  and client.request('GET', '/readyz')[0] == 200)
        same = human_ok('POST', '/v1/sessions', opening, {'Idempotency-Key': opening_key})
        check('exact session-open retry is idempotent', same['session_id'] == session['session_id'])
        for kind in source_clients:
            check(kind + ' idempotent session issuance retains enrollment generation', binding(kind) == bindings[kind])
        status, admitted = identity.request('POST', '/v1/principals',
            {'tenant': tenant, 'sub': principal, 'allowed_signer_public_keys': []})
        check('webhook ownership is provisioned through authenticated identity', status == 200
              and admitted.get('tenant') == tenant and admitted.get('sub') == principal)
        status, developer = identity.request('POST', '/v1/sessions', {'tenant': tenant, 'sub': principal})
        check('webhook developer session comes from the actual identity issuer', status == 200
              and developer.get('sub') == principal and str(developer.get('token', '')).startswith('ses_'))
        public.endpoint = dict(public.endpoint, header_files={})
        bearer = {'Authorization': 'Bearer ' + developer['token']}
        registration_status, endpoint = public.request('POST', '/v1/webhooks/endpoints',
            {'url': descriptor['receiver_endpoint'], 'kinds': ['journey', 'approval']},
            bearer | {'Idempotency-Key': str(uuid.uuid4())})
        check('real webhook endpoint binds identity and KMS signing keys', registration_status == 201
              and endpoint.get('endpoint') and isinstance(endpoint.get('public_keys_json'), str))
        receiver_root = Path(descriptor['receiver_root'])
        keys = receiver_root / 'public-keys.json'
        keys.write_text(endpoint['public_keys_json'])
        keys.chmod(0o600)
        receiver_log = (work / 'receiver-worker.log').open('ab')
        receiver = subprocess.Popen([sys.executable, str(ROOT / 'tools/qualification/paxeer-x/fixtures/event_receiver_fixture.py'),
            'serve', str(receiver_root), descriptor['receiver_address'], str(descriptor['receiver_port']),
            descriptor['receiver_cert'], descriptor['receiver_key']], stdin=subprocess.DEVNULL,
            stdout=receiver_log, stderr=receiver_log)
        processes.wait(lambda: (receiver_root / 'receiver.ready').exists(), 'real signed webhook receiver')
        delivery_phase('fresh')
        for kind, client in source_clients.items():
            row = {'principal': principal, 'tenant': tenant, 'revision': bindings[kind]['revision'] + 1,
                   'session_id': session['session_id'], 'credential': human.cookies['__Host-layerx_access'],
                   'expires_at': int(time.time()) + int(human.last_cookie_attributes['__Host-layerx_access']['max-age'])}
            negatives = [('wrong-principal', dict(row, principal=principal + '-other'), 403, 'enrollment_principal_mismatch'),
                         ('wrong-tenant', dict(row, tenant=tenant + '-other'), 403, 'enrollment_principal_mismatch'),
                         ('invalid-session', dict(row, session_id=session['session_id'] + '-invalid'), 403, 'enrollment_principal_mismatch'),
                         ('invalid-credential', dict(row, credential='not-an-issued-session'), 503, 'enrollment_upstream_unavailable'),
                         ('malformed-credential', dict(row, credential='invalid;cookie'), 400, 'enrollment_malformed'),
                         ('expired-credential', dict(row, expires_at=1), 400, 'enrollment_malformed')]
            for label, request, expected_status, expected_code in negatives:
                status, refused = client.request('POST', '/internal/v1/enrollments', request)
                check(kind + ' rejects ' + label, status == expected_status
                      and isinstance(refused.get('error'), dict) and refused['error'].get('code') == expected_code)
                check(kind + ' ' + label + ' cannot replace valid binding', binding(kind) == bindings[kind])
            status, refused = client.request('POST', '/internal/v1/enrollments', row,
                                       {'Authorization': 'Bearer unauthorized-writer'})
            check(kind + ' unauthorized writer fails closed', status == 401
                  and refused.get('error', {}).get('code') == 'unauthorized')
            try:
                status, _ = client.request('POST', '/internal/v1/enrollments', row, client_identity=False)
                rejected = status in (401, 403)
            except (OSError, ssl.SSLError):
                rejected = True
            check(kind + ' enrollment requires mTLS', rejected)
        original_pids = {name: child.pid for name, child in processes.children.items()}
        restart_names = ['human-service', 'human-components', 'webhook-ingress', 'webhook-public',
                         'journeys', 'approvals', 'payments', 'programs', 'kms', 'identity']
        for name in restart_names:
            processes.stop(name)
        for name in reversed(restart_names):
            processes.start(name)
        for kind in source_clients:
            processes.wait(lambda kind=kind: binding(kind) == bindings[kind], kind + ' restored durable binding')
            check(kind + ' restored volume re-admits same principal and tenant', identity_bound(binding(kind)))
        check('restored volumes run new real service processes', all(processes.children[name].pid != original_pids[name]
              for name in restart_names))
        delivery_phase('restored')
        old_cookie = human.cookies['__Host-layerx_access']
        session = human_ok('POST', '/v1/sessions/refresh', {})
        check('rotation is issued by the authenticated Human refresh route', human.cookies['__Host-layerx_access'] != old_cookie
              and session.get('current') is True)
        for kind in source_clients:
            old = bindings[kind]
            current = processes.wait(lambda kind=kind, old=old: (value if identity_bound(value := binding(kind))
                                    and value['revision'] > old['revision'] and value['generation'] > old['generation'] else None),
                                    kind + ' authorized session rotation')
            bindings[kind] = current
            check(kind + ' rotation keeps the exact principal and tenant', identity_bound(current))
            stale = {'principal': principal, 'tenant': tenant, 'revision': old['revision'],
                     'session_id': session['session_id'], 'credential': human.cookies['__Host-layerx_access'],
                     'expires_at': int(time.time()) + int(human.last_cookie_attributes['__Host-layerx_access']['max-age'])}
            status, refused = source_clients[kind].request('POST', '/internal/v1/enrollments', stale)
            check(kind + ' stale issuer revision cannot undo authenticated rotation', status == 409
                  and refused.get('error', {}).get('code') == 'enrollment_issuer_conflict'
                  and binding(kind) == current)
        delivery_phase('rotated')
        status, ledger = public.request('GET', '/v1/webhooks/events', headers=bearer)
        check('all twelve canonical journey and approval transitions persist exactly once', status == 200
              and len(observed_events) == 12 and len(set(observed_events)) == 12
              and all(sum(event['id'] == identifier for event in ledger) == 1 for identifier in observed_events))
        replay_record = next(record for record, event in records() if event['id'] == observed_events[-1])
        replay_body = base64.b64decode(replay_record['body'], validate=True)
        from receipt_authority_fixture import https
        from hosted_delivery_fixture import CaMaterial
        ca = CaMaterial.__new__(CaMaterial)
        ca.ca_pem = Path(fixture['ca']['certificate'])
        ca.ca_der = Path(fixture['ca']['der'])
        headers = {key: value for key, value in replay_record['headers'].items() if key.startswith('layerx-webhook-')}
        headers['Content-Type'] = 'application/json'
        before = sorted(path.name for path in (receiver_root / 'effects').iterdir())
        status, _, _ = https('POST', descriptor['receiver_endpoint'], ca, replay_body, headers)
        check('exact signed webhook retry has no duplicate economic effect', status == 200
              and before == sorted(path.name for path in (receiver_root / 'effects').iterdir()))
        status, _, _ = https('POST', descriptor['receiver_endpoint'], ca, replay_body + b' ', headers)
        check('changed delivery bytes fail the real signature verifier', status == 401)
        record = {'candidate_manifest_sha256': MANIFEST_DIGEST, 'source_revision': manifest['source']['revision'],
                  'principal': principal, 'tenant': tenant, 'bindings': bindings, 'canonical_events': observed_events,
                  'assertions': cases, 'deployed_qualification': False}
        path = work / 'principal-provisioning.json'
        path.write_text(json.dumps(record, sort_keys=True))
        path.chmod(0o600)
        encoded = path.read_bytes()
        check('qualification record excludes issued authentication material',
              all(value.encode() not in encoded for value in (*human.cookies.values(), developer['token'])))
    finally:
        for child in (receiver, authenticator):
            if child is not None and child.poll() is None:
                child.terminate()
                try:
                    child.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    child.kill()
                    child.wait(timeout=5)
        for log in (receiver_log, auth_log):
            if log is not None:
                log.close()
        processes.close()


def event_principal_provisioning(manifest, results):
    fixture = provisioning_inputs(manifest)
    sys.path.insert(0, str(ROOT / 'tools/qualification/paxeer-x/fixtures'))
    worker = os.environ.get('PAXEER_X_EVENT_PROVISIONING_WORKER')
    if worker:
        provisioning_worker(manifest, results, worker)
        return
    from hosted_delivery_fixture import CaMaterial
    from event_receiver_fixture import EventReceiverFixture
    evidence = os.environ.get('PAXEER_X_EVIDENCE_DIR')
    if not evidence:
        raise Missing('PAXEER_X_EVIDENCE_DIR is required')
    work = Path(tempfile.mkdtemp(prefix='event-principal-provisioning-', dir=private_directory(Path(evidence))))
    work.chmod(0o700)
    print('evidence=' + str(work))
    fly = tomllib.loads((ROOT / 'platform/hosted/internal/fly.toml').read_text())
    expected = {'kms', *KINDS}
    provisioning_check(results, 'all five internal groups remain private-only', set(fly['processes']) == expected
                       and not fly.get('services') and not fly.get('http_service'))
    mounts = fly.get('mounts', [])
    provisioning_check(results, 'all five deployment groups retain independent persistent volumes', len(mounts) == 5
                       and {tuple(row['processes']) for row in mounts} == {(name,) for name in expected}
                       and len({row['source'] for row in mounts}) == 5)
    ca = CaMaterial.__new__(CaMaterial)
    ca.directory = Path(fixture['ca']['directory'])
    ca.ca_pem = protected_input(fixture['ca']['certificate'], 'genuine fixture CA certificate')
    ca.ca_der = protected_input(fixture['ca']['der'], 'genuine fixture CA DER')
    ca._key = protected_input(fixture['ca']['key_file'], 'protected fixture CA signing authority')
    ca._serial = int.from_bytes(os.urandom(12), 'big')
    receiver = EventReceiverFixture(work, ca, address=fixture['receiver_address'])
    try:
        receiver._materials()
        receiver._start_namespace()
        descriptor = {'parent_pid': os.getpid(), 'candidate_manifest_sha256': MANIFEST_DIGEST,
                      'network_namespace': os.stat(f'/proc/{receiver.holder.pid}/ns/net').st_ino,
                      'mount_namespace': os.stat(f'/proc/{receiver.holder.pid}/ns/mnt').st_ino,
                      'work': str(work), 'receiver_root': str(receiver.root), 'receiver_endpoint': receiver.endpoint,
                      'receiver_address': receiver.address, 'receiver_port': receiver.port,
                      'receiver_cert': str(receiver.materials['server_cert']), 'receiver_key': str(receiver.materials['server_key'])}
        attachment = work / 'worker.json'
        attachment.write_text(json.dumps(descriptor))
        attachment.chmod(0o600)
        command = receiver.ns_command([sys.executable, str(Path(__file__).resolve()), '--case', 'event-principal-provisioning',
                                       '--candidate-manifest', str(MANIFEST_PATH)])
        environment = {key: value for key, value in os.environ.items()
                       if key.lower() not in ('http_proxy', 'https_proxy', 'all_proxy', 'no_proxy')}
        environment.update(PAXEER_X_EVENT_PROVISIONING_WORKER=str(attachment), NO_PROXY='*')
        child = subprocess.Popen(command, env=environment, stdin=subprocess.DEVNULL,
                                 stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        try:
            output, errors = child.communicate(timeout=1100)
        except subprocess.TimeoutExpired:
            child.terminate()
            output, errors = child.communicate(timeout=30)
        (work / 'worker.log').write_text(output + errors)
        for line in output.splitlines():
            if line.startswith(('PASS ', 'FAIL ', 'MISSING ')):
                print(line, flush=True)
                if line.startswith('PASS '):
                    results.passes += 1
                else:
                    results.failures += 1
        result = {'revision': manifest['source']['revision'], 'command':
                  'timeout 20m python3 tools/qualification/paxeer-x/event-delivery-contract.py --case event-principal-provisioning --candidate-manifest "$PAXEER_X_CANDIDATE_MANIFEST"',
                  'exit_code': child.returncode, 'log_path': str(work / 'worker.log')}
        (work / 'result.json').write_text(json.dumps(result, sort_keys=True))
        provisioning_check(results, 'complete real provisioning worker executed', child.returncode == 0
                           and (work / 'principal-provisioning.json').is_file())
    finally:
        receiver.stop()
        receiver._terminate(receiver.holder)


def producer_readiness_inputs(fixture):
    configuration = fixture.get('producer_readiness')
    if not isinstance(configuration, dict) or set(configuration) != {'clients', 'producer_ingress', 'operator_ingress', 'dependencies'}:
        raise Missing('producer readiness needs real client health endpoints and distinct producer/operator mTLS ingress identities')
    clients = configuration['clients']
    if set(clients) != {'human', 'registry', 'gateway'}:
        raise Missing('all three real producer clients are required')
    expected = {'human': ('layerx-human-components', '/readyz'),
                'registry': ('layerx-program-registry', '/healthz'),
                'gateway': ('layerx-gateway', '/readyz')}
    for name, (binary, path) in expected.items():
        item = clients[name]
        if item['service'] not in fixture['services'] \
                or fixture['services'][item['service']]['binary'] != binary or item['path'] != path:
            raise Missing('readiness must observe the actual production client: ' + name)
        ProvisioningHttp(item['endpoint'])
    if clients['human']['endpoint'] != fixture['human']:
        raise Missing('Human readiness must use the genuine Human service listener')
    dependencies = configuration['dependencies']
    binaries = {'redis': {'redis-server'}, 'kms': {'layerx-kms'},
                'component': {'layerx-agent-boundary'},
                'authority': {'layerx-receipt-authority'},
                'source-principal': {'layerx-human-service', 'layerx-human-components'}}
    if not isinstance(dependencies, dict) or set(dependencies) != set(binaries):
        raise Missing('readiness requires every owned real dependency for deadline refusal and restoration')
    ingress_env = fixture['services']['webhook-ingress']['environment']
    origins = {'redis': ingress_env['LAYERX_WEBHOOKS_REDIS_URL'],
               'kms': ingress_env['LAYERX_WEBHOOKS_KMS_URL'],
               'component': ingress_env['LAYERX_WEBHOOKS_COMPONENT_URL'],
               'authority': ingress_env['LAYERX_WEBHOOKS_AUTHORITY_URL'],
               'source-principal': fixture['services']['journeys']['environment']['LAYERX_EVENTS_UPSTREAM_URL']}
    for name, service_name in dependencies.items():
        service = fixture['services'].get(service_name)
        if not service or service['binary'] not in binaries[name]:
            raise Missing('deadline dependency must be an owned production process: ' + name)
        parsed = urlsplit(origins[name])
        if parsed.hostname not in ('localhost', '127.0.0.1'):
            raise Missing('deadline dependency must remain inside this isolated namespace: ' + name)
        port = parsed.port or (6379 if name == 'redis' else 443)
        if name == 'redis':
            if '--tls-port' not in service['arguments'] or service['arguments'][service['arguments'].index('--tls-port') + 1] != str(port):
                raise Missing('Redis deadline process must own the configured real TLS port')
        elif not any(value in (f'127.0.0.1:{port}', f'localhost:{port}', f'0.0.0.0:{port}')
                     for key, value in service['environment'].items() if key.endswith(('_LISTEN', '_BIND'))):
            raise Missing('deadline process must own the configured dependency listener: ' + name)
    ingress = fixture['services']['webhook-ingress']['environment']['LAYERX_WEBHOOKS_LISTEN']
    for role in ('producer_ingress', 'operator_ingress'):
        endpoint = configuration[role]
        parsed = urlsplit(endpoint['url'])
        if parsed.hostname not in ('localhost', '127.0.0.1') or parsed.port != int(ingress.rsplit(':', 1)[1]):
            raise Missing('trigger readiness must reach the owned real ingress process')
        for key in ('client_cert_pem', 'client_key_pem', 'ca_pem'):
            protected_input(endpoint[key], 'actual ' + role + ' TLS material')
        if set(endpoint.get('header_files', {})) != {'Authorization'}:
            raise Missing('trigger client requires its existing scoped bearer reference')
        protected_input(endpoint['header_files']['Authorization'], 'trigger scoped bearer')
    return configuration


def producer_readiness_empty(fixture, processes, human, sources, check):
    producer_readiness_inputs(fixture)
    for kind, client in sources.items():
        status, value = client.request('GET', '/internal/v1/producer-readiness')
        check(kind + ' authenticated empty source refuses admission while live', status == 503
              and value.get('role') == 'source-producer' and value.get('principals') == 0
              and value.get('ready') is False and client.request('GET', '/livez')[0] == 200)
    processes.wait(lambda: human.request('GET', '/readyz')[0] == 503, 'empty principal producer refusal', 15)
    check('Human idle producer is live but unready before first principal', human.request('GET', '/livez')[0] == 200)


def producer_readiness_checks(manifest, fixture, processes, work, principal, human, check):
    import signal
    configuration = producer_readiness_inputs(fixture)
    clients = {name: ProvisioningHttp(item['endpoint']) for name, item in configuration['clients'].items()}
    sources = {kind: ProvisioningHttp(fixture['sources'][kind]) for kind in KINDS}
    ingress = ProvisioningHttp(configuration['producer_ingress'])
    operator = ProvisioningHttp(configuration['operator_ingress'])
    singular = {'journeys': 'journey', 'approvals': 'approval', 'payments': 'payment', 'programs': 'program'}
    affected = {'journeys': 'human', 'approvals': 'human', 'payments': 'gateway', 'programs': 'registry'}

    def ready(name, expected=200):
        return clients[name].request('GET', configuration['clients'][name]['path'])[0] == expected

    def admission(kind):
        status, value = sources[kind].request('GET', '/internal/v1/producer-readiness')
        return status == 200 and value.get('schema') == 'layerx.event-admission.v1' \
            and value.get('role') == 'source-producer' and value.get('kind') == singular[kind] \
            and value.get('ready') is True and value.get('principals', 0) > 0 \
            and value.get('generation', 0) > 0 and value.get('fresh_for_ms') == 10000

    def journals_empty():
        return all((Path(fixture['services'][kind]['environment']['LAYERX_EVENTS_STATE_DIR']) / 'journal.log')
                   .read_bytes() == b'' for kind in KINDS)

    def restored(label):
        for kind in KINDS:
            processes.wait(lambda kind=kind: admission(kind), label + ' source admission ' + kind, 40)
        for name in clients:
            processes.wait(lambda name=name: ready(name), label + ' producer ready ' + name, 40)
        check(label + ' readiness does not synthesize or require a canonical event', journals_empty())

    restored('first-principal bootstrap')
    check('readiness bootstrap uses a genuinely issued Human principal', bool(principal)
          and human.request('GET', '/internal/v1/principal')[1].get('result', {}).get('sub') == principal)
    for kind, client in sources.items():
        status, _ = client.request('GET', '/internal/v1/producer-readiness',
                                   headers={'Authorization': 'Bearer invalid-readiness-credential'})
        check(kind + ' liveness cannot authenticate invalid producer credentials', status == 401
              and client.request('GET', '/livez')[0] == 200)
        reader = protected_input(fixture['services'][kind]['environment']['LAYERX_EVENTS_TOKEN_FILE'],
                                 'existing source reader').read_text().strip()
        check(kind + ' reader authority cannot admit a producer',
              client.request('GET', '/internal/v1/producer-readiness',
                             headers={'Authorization': 'Bearer ' + reader})[0] == 401)
        try:
            status, _ = client.request('GET', '/internal/v1/producer-readiness', client_identity=False)
            refused = status in (401, 403)
        except (OSError, ssl.SSLError):
            refused = True
        check(kind + ' producer admission requires a verified client certificate', refused)
        path = '/internal/v1/readiness/' + singular[kind]
        processes.wait(lambda: ingress.request('GET', path)[0] == 200, kind + ' real trigger admission')
        status, value = ingress.request('GET', path)
        check(kind + ' trigger admission is a separate authenticated contract', status == 200
              and value.get('role') == 'webhook-trigger' and value.get('kind') == singular[kind]
              and value.get('schema') == 'layerx.event-admission.v1' and value.get('ready') is True
              and value.get('generation', 0) > 0 and value.get('principals', 0) > 0
              and value.get('fresh_for_ms') == 10000)
        check(kind + ' operator cannot use producer readiness', operator.request('GET', path)[0] == 403)
        check(kind + ' trigger refuses invalid scoped bearer', ingress.request('GET', path,
              headers={'Authorization': 'Bearer invalid-readiness-credential'})[0] == 401)
        try:
            status, _ = ingress.request('GET', path, client_identity=False)
            refused = status in (401, 403)
        except (OSError, ssl.SSLError):
            refused = True
        check(kind + ' trigger readiness retains mTLS producer authority', refused)
        for deadline in ('invalid', str(int(time.time() * 1000) - 1000)):
            started = time.monotonic()
            status, _ = ingress.request('GET', path, headers={'X-LayerX-Admission-Deadline-Ms': deadline})
            check(kind + ' trigger refuses malformed or expired absolute deadline without probing',
                  status in (400, 503) and time.monotonic() - started < 2)
            started = time.monotonic()
            status, _ = client.request('GET', '/internal/v1/producer-readiness',
                                      headers={'X-LayerX-Admission-Deadline-Ms': deadline})
            check(kind + ' source refuses malformed or expired absolute deadline without probing',
                  status == 503 and time.monotonic() - started < 2)
    for dependency, service_name in configuration['dependencies'].items():
        child = processes.children[service_name]
        child.send_signal(signal.SIGSTOP)
        try:
            if dependency == 'source-principal':
                started = time.monotonic()
                status, value = sources['journeys'].request('GET', '/internal/v1/producer-readiness')
                check('principal identity transport cannot outlive the five-second admission budget',
                      status == 503 and value.get('ready') is False and time.monotonic() - started < 6)
                check('principal dependency stall leaves source independently live',
                      sources['journeys'].request('GET', '/livez')[0] == 200)
                started = time.monotonic()
                status, _ = sources['journeys'].request('GET', '/internal/v1/producer-readiness',
                    headers={'X-LayerX-Admission-Deadline-Ms': str(int(time.time() * 1000) + 250)})
                check('source principal checks preserve the shorter caller deadline',
                      status == 503 and time.monotonic() - started < 1)
            else:
                started = time.monotonic()
                status, _ = ingress.request('GET', '/internal/v1/readiness/journey')
                check(dependency + ' transport cannot outlive the shared five-second trigger budget',
                      status == 503 and time.monotonic() - started < 6)
                started = time.monotonic()
                status, _ = ingress.request('GET', '/internal/v1/readiness/journey',
                    headers={'X-LayerX-Admission-Deadline-Ms': str(int(time.time() * 1000) + 250)})
                check(dependency + ' trigger checks preserve the shorter caller deadline',
                      status == 503 and time.monotonic() - started < 1)
                for name in clients:
                    processes.wait(lambda name=name: ready(name, 503), dependency + ' producer refusal ' + name, 15)
                check(dependency + ' refused readiness leaves canonical state intact', journals_empty())
        finally:
            child.send_signal(signal.SIGCONT)
        restored(dependency + ' dependency resumed')
    for kind in KINDS:
        name = affected[kind]
        child = processes.children[kind]
        child.send_signal(signal.SIGSTOP)
        started = time.monotonic()
        try:
            processes.wait(lambda: ready(name, 503), kind + ' hung source refusal', 15)
            check(kind + ' stopped real source expires producer readiness within freshness bound',
                  time.monotonic() - started < 15)
            time.sleep(6)
            check(kind + ' stalled operation cannot restore readiness from cached success', ready(name, 503))
        finally:
            child.send_signal(signal.SIGCONT)
        restored(kind + ' resumed dependency')
        old_pid = processes.children[kind].pid
        processes.stop(kind)
        processes.wait(lambda: ready(name, 503), kind + ' unavailable refusal', 15)
        processes.start(kind)
        restored(kind + ' source restart')
        check(kind + ' source recovery uses a new real process', processes.children[kind].pid != old_pid)
    processes.stop('webhook-ingress')
    for name in clients:
        processes.wait(lambda name=name: ready(name, 503), name + ' trigger unavailable', 15)
    check('all live idle producers refuse unavailable ingress', human.request('GET', '/livez')[0] == 200
          and clients['gateway'].request('GET', '/livez')[0] == 200)
    processes.start('webhook-ingress')
    restored('trigger restored')
    invalid = secret(work, 'invalid-producer-readiness-token', 'invalid-readiness-credential')
    suffixes = ('URL', 'CA_DER', 'TOKEN_FILE', 'CLIENT_IDENTITY_PKCS12', 'CLIENT_IDENTITY_PASSWORD_FILE', 'COOKIE_FILE')
    for name, item in configuration['clients'].items():
        service = fixture['services'][item['service']]
        original = dict(service['environment'])
        source_kind = {'human': 'JOURNEY', 'registry': 'PROGRAM', 'gateway': 'PAYMENT'}[name]
        for prefix in ('LAYERX_EVENTS_' + source_kind, 'LAYERX_EVENTS_WEBHOOKS'):
            processes.stop(item['service'])
            service['environment'] = dict(original, **{prefix + '_UPSTREAM_TOKEN_FILE': invalid})
            processes.start(item['service'])
            processes.wait(lambda: ready(name, 503), name + ' invalid ' + prefix, 15)
            check(name + ' rejects loaded but unauthenticated ' + prefix + ' credentials', ready(name, 503))
            processes.stop(item['service'])
            service['environment'] = dict(original)
            processes.start(item['service'])
            restored(name + ' valid ' + prefix + ' credentials restored')
        processes.stop(item['service'])
        missing = dict(original)
        missing.pop('LAYERX_EVENTS_' + source_kind + '_UPSTREAM_TOKEN_FILE')
        service['environment'] = missing
        processes.start(item['service'])
        child = processes.children[item['service']]
        child.wait(timeout=15)
        check(name + ' partial producer configuration refuses startup', child.returncode != 0)
        processes.children.pop(item['service'])
        if name in ('human', 'registry'):
            groups = ('LAYERX_EVENTS_JOURNEY', 'LAYERX_EVENTS_APPROVAL', 'LAYERX_EVENTS_WEBHOOKS') if name == 'human' \
                else ('LAYERX_EVENTS_PROGRAM', 'LAYERX_EVENTS_WEBHOOKS')
            service['environment'] = {key: value for key, value in original.items()
                if not any(key == prefix + '_UPSTREAM_' + suffix for prefix in groups for suffix in suffixes)}
            processes.start(item['service'])
            child = processes.children[item['service']]
            child.wait(timeout=15)
            check(name + ' mandatory producer group cannot be absent', child.returncode != 0)
            processes.children.pop(item['service'])
        if name == 'gateway':
            absent = {key: value for key, value in original.items()
                      if not any(key == prefix + '_UPSTREAM_' + suffix
                                 for prefix in ('LAYERX_EVENTS_PAYMENT', 'LAYERX_EVENTS_WEBHOOKS') for suffix in suffixes)}
            service['environment'] = absent
            processes.start(item['service'])
            processes.wait(lambda: ready(name), 'gateway whole producer group absent', 40)
            check('gateway producer is optional only with the complete group absent', ready(name))
            processes.stop(item['service'])
            absent['LAYERX_EVENTS_WEBHOOKS_UPSTREAM_COOKIE_FILE'] = invalid
            service['environment'] = absent
            processes.start(item['service'])
            child = processes.children[item['service']]
            child.wait(timeout=15)
            check('gateway credential-only partial group refuses startup', child.returncode != 0)
            processes.children.pop(item['service'])
        service['environment'] = original
        processes.start(item['service'])
        restored(name + ' producer restart')
    check('all readiness failures and recoveries leave canonical journals empty', journals_empty())
    (work / 'producer-readiness.json').write_text(json.dumps({
        'source_revision': manifest['source']['revision'], 'candidate_manifest_sha256': MANIFEST_DIGEST,
        'real_processes': sorted(processes.children), 'canonical_events_created': 0,
        'deployed_qualification': False}, sort_keys=True))


def producer_readiness(manifest, results):
    fixture = provisioning_inputs(manifest)
    producer_readiness_inputs(fixture)
    sys.path.insert(0, str(ROOT / 'tools/qualification/paxeer-x/fixtures'))
    worker = os.environ.get('PAXEER_X_EVENT_PROVISIONING_WORKER')
    if worker:
        provisioning_worker(manifest, results, worker, readiness=True)
        return
    from hosted_delivery_fixture import CaMaterial
    from event_receiver_fixture import EventReceiverFixture
    evidence = os.environ.get('PAXEER_X_EVIDENCE_DIR')
    if not evidence:
        raise Missing('PAXEER_X_EVIDENCE_DIR is required')
    work = Path(tempfile.mkdtemp(prefix='producer-readiness-', dir=private_directory(Path(evidence))))
    work.chmod(0o700)
    print('evidence=' + str(work))
    fly = tomllib.loads((ROOT / 'platform/hosted/internal/fly.toml').read_text())
    expected = {'kms', *KINDS}
    provisioning_check(results, 'all five internal groups remain private-only', set(fly['processes']) == expected
                       and not fly.get('services') and not fly.get('http_service'))
    mounts = fly.get('mounts', [])
    provisioning_check(results, 'all five deployment groups retain independent persistent volumes', len(mounts) == 5
                       and {tuple(row['processes']) for row in mounts} == {(name,) for name in expected}
                       and len({row['source'] for row in mounts}) == 5)
    ca = CaMaterial.__new__(CaMaterial)
    ca.directory = Path(fixture['ca']['directory'])
    ca.ca_pem = protected_input(fixture['ca']['certificate'], 'genuine fixture CA certificate')
    ca.ca_der = protected_input(fixture['ca']['der'], 'genuine fixture CA DER')
    ca._key = protected_input(fixture['ca']['key_file'], 'protected fixture CA signing authority')
    ca._serial = int.from_bytes(os.urandom(12), 'big')
    receiver = EventReceiverFixture(work, ca, address=fixture['receiver_address'])
    try:
        receiver._materials()
        receiver._start_namespace()
        descriptor = {'parent_pid': os.getpid(), 'candidate_manifest_sha256': MANIFEST_DIGEST,
                      'network_namespace': os.stat(f'/proc/{receiver.holder.pid}/ns/net').st_ino,
                      'mount_namespace': os.stat(f'/proc/{receiver.holder.pid}/ns/mnt').st_ino,
                      'work': str(work), 'receiver_root': str(receiver.root), 'receiver_endpoint': receiver.endpoint,
                      'receiver_address': receiver.address, 'receiver_port': receiver.port,
                      'receiver_cert': str(receiver.materials['server_cert']), 'receiver_key': str(receiver.materials['server_key'])}
        attachment = work / 'worker.json'
        attachment.write_text(json.dumps(descriptor))
        attachment.chmod(0o600)
        command = receiver.ns_command([sys.executable, str(Path(__file__).resolve()), '--case', 'producer-readiness',
                                       '--candidate-manifest', str(MANIFEST_PATH)])
        environment = {key: value for key, value in os.environ.items()
                       if key.lower() not in ('http_proxy', 'https_proxy', 'all_proxy', 'no_proxy')}
        environment.update(PAXEER_X_EVENT_PROVISIONING_WORKER=str(attachment), NO_PROXY='*')
        child = subprocess.Popen(command, env=environment, stdin=subprocess.DEVNULL,
                                 stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        try:
            output, errors = child.communicate(timeout=1100)
        except subprocess.TimeoutExpired:
            child.terminate()
            output, errors = child.communicate(timeout=30)
        (work / 'worker.log').write_text(output + errors)
        for line in output.splitlines():
            if line.startswith(('PASS ', 'FAIL ', 'MISSING ')):
                print(line, flush=True)
                if line.startswith('PASS '):
                    results.passes += 1
                else:
                    results.failures += 1
        result = {'revision': manifest['source']['revision'], 'command':
                  'timeout 20m python3 tools/qualification/paxeer-x/event-delivery-contract.py --case producer-readiness --candidate-manifest "$PAXEER_X_CANDIDATE_MANIFEST"',
                  'exit_code': child.returncode, 'log_path': str(work / 'worker.log')}
        (work / 'result.json').write_text(json.dumps(result, sort_keys=True))
        provisioning_check(results, 'complete real provisioning worker executed', child.returncode == 0
                           and (work / 'producer-readiness.json').is_file())
    finally:
        receiver.stop()
        receiver._terminate(receiver.holder)


CASES = {'producer-readiness': producer_readiness,
         'principal-credential-lifecycle': principal_credential_lifecycle,
         'principal-delivery-fairness': principal_delivery_fairness,
         'webhook-ingress-roles': webhook_ingress_roles,
         'event-principal-provisioning': event_principal_provisioning}
MANIFEST_DIGEST = None
MANIFEST_PATH = None


def main():
    global MANIFEST_DIGEST, MANIFEST_PATH
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--case', choices=tuple(CASES), required=True)
    parser.add_argument('--candidate-manifest', required=True)
    arguments = parser.parse_args()
    results = Results()
    try:
        MANIFEST_PATH = Path(arguments.candidate_manifest).resolve()
        manifest, MANIFEST_DIGEST = load_manifest(arguments.candidate_manifest)
        CASES[arguments.case](manifest, results)
    except Missing as error:
        results.failures += 1
        print('MISSING ' + str(error), flush=True)
    except (OSError, ValueError, KeyError, TypeError, RuntimeError, ImportError, subprocess.TimeoutExpired) as error:
        results.failures += 1
        print('FAIL execution refused: ' + type(error).__name__, flush=True)
    print(f'RESULT case={arguments.case} assertions={results.passes + results.failures} '
          f'failures={results.failures}', flush=True)
    return 0 if results.passes > 0 and results.failures == 0 else 1


if __name__ == '__main__':
    sys.exit(main())
