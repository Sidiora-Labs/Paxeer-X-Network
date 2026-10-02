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


CASES = {'principal-credential-lifecycle': principal_credential_lifecycle,
         'principal-delivery-fairness': principal_delivery_fairness}
MANIFEST_DIGEST = None


def main():
    global MANIFEST_DIGEST
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--case', choices=tuple(CASES), required=True)
    parser.add_argument('--candidate-manifest', required=True)
    arguments = parser.parse_args()
    results = Results()
    try:
        manifest, MANIFEST_DIGEST = load_manifest(arguments.candidate_manifest)
        CASES[arguments.case](manifest, results)
    except Missing as error:
        results.failures += 1
        print('MISSING ' + str(error), flush=True)
    except (OSError, ValueError, KeyError, TypeError, subprocess.TimeoutExpired) as error:
        results.failures += 1
        print('FAIL execution refused: ' + type(error).__name__, flush=True)
    print(f'RESULT case={arguments.case} assertions={results.passes + results.failures} '
          f'failures={results.failures}', flush=True)
    return 0 if results.passes > 0 and results.failures == 0 else 1


if __name__ == '__main__':
    sys.exit(main())
