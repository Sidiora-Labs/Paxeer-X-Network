#!/usr/bin/env python3
"""Event delivery contract cases of the Paxeer X Network.

usage: event-delivery-contract.py --case <case> --candidate-manifest <path>

Every case runs the real served binaries with material issued through the real
tools/bringup/ca.sh paths, prints one PASS or FAIL line per assertion and one
RESULT line, and exits nonzero when a required input, execution or assertion
is absent or fails. No private material is printed.

webhook-ingress-roles
    layerx-webhooks as the public process and as the private ingress: the
    public route set refuses every /internal route before any producer or
    operator credential; the ingress admits only internal-CA client leaves
    carrying clientAuth and exactly one webhook role URI SAN together with
    that role's bearer; foreign, expired, roleless and malformed leaves are
    refused; restart and coordinated CA/token rotation keep both contracts;
    the Fly and Kubernetes wiring mount role credentials per role; and
    ca.sh issue-local webhook-operator-client issues and refuses as declared.
"""

import argparse
import hashlib
import http.client
import json
import os
import shutil
import socket
import ssl
import subprocess
import sys
import tempfile
import time
import tomllib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
CA_SH = ROOT / 'tools' / 'bringup' / 'ca.sh'
SCHEMA = 'paxeer-x.candidate.v1'
PRODUCER = 'urn:layerx:webhooks:role:producer'
OPERATOR = 'urn:layerx:webhooks:role:operator'
PRODUCER_ROWS = ('human-event-client', 'gateway-client', 'registry-event-client')
INGRESS_SECRETS = (
    'WEBHOOKS_INGRESS_TLS_CERT', 'WEBHOOKS_INGRESS_TLS_CERT_DER', 'WEBHOOKS_INGRESS_TLS_KEY_DER',
    'WEBHOOKS_COMPONENT_TOKEN', 'WEBHOOKS_AUTHORITY_TOKEN', 'WEBHOOKS_JOURNEY_SOURCE_TOKEN',
    'WEBHOOKS_PAYMENT_SOURCE_TOKEN', 'WEBHOOKS_APPROVAL_SOURCE_TOKEN', 'WEBHOOKS_PROGRAM_SOURCE_TOKEN',
    'WEBHOOKS_SOURCE_TRIGGER_TOKEN', 'WEBHOOKS_OPERATOR_TOKEN', 'WEBHOOKS_SEQUENCER_PUBLIC_KEY',
    'WEBHOOKS_SEQUENCER_ID', 'WEBHOOKS_SEQUENCER_FIRST_BATCH', 'WEBHOOKS_SEQUENCER_LAST_BATCH',
)
PUBLIC_SECRETS = ('WEBHOOKS_IDENTITY_TOKEN',)
SOURCE_EVENT = '7' * 64


class Missing(Exception):
    """A required input or execution is absent."""


class Results:
    def __init__(self):
        self.passed = 0
        self.failed = 0

    def check(self, name, condition, observed=''):
        if condition:
            self.passed += 1
            print(f'PASS {name}')
        else:
            self.failed += 1
            print(f'FAIL {name}: {observed}')


def run(arguments, environment=None, check=True, timeout=60):
    completed = subprocess.run(arguments, env=environment, capture_output=True, text=True,
                               timeout=timeout, check=False)
    if check and completed.returncode != 0:
        raise Missing(f'{Path(arguments[0]).name} {arguments[1] if len(arguments) > 1 else ""} '
                      f'exited {completed.returncode}: {completed.stderr.strip()[-400:]}')
    return completed


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
    results.check('fly http_service serves only the public group',
                  fly.get('http_service', {}).get('processes') == ['public'], fly.get('http_service'))
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
    binary = os.environ.get('LAYERX_WEBHOOKS_BIN') or str(
        Path(os.environ.get('CARGO_TARGET_DIR', ROOT / 'platform' / 'target')) / 'debug' / 'layerx-webhooks')
    if not os.access(binary, os.X_OK):
        raise Missing(f'layerx-webhooks binary absent at {binary}; build layerx-platform-webhooks first')
    for tool in ('openssl', 'bash'):
        if shutil.which(tool) is None:
            raise Missing(f'{tool} is required')
    print(f'candidate schema={manifest["schema"]} services={len(manifest["services"])}')
    wiring(results)
    with tempfile.TemporaryDirectory(prefix='webhook-ingress-roles-') as temporary:
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


def principal_delivery_fairness(manifest, results):
    target = Path(os.environ.get('CARGO_TARGET_DIR', ROOT / 'platform' / 'target')) / 'debug'
    human_target = Path(os.environ.get('CARGO_TARGET_DIR', ROOT / 'human' / 'target')) / 'debug'
    required = {
        'LAYERX_EVENT_SOURCE_BIN': os.environ.get('LAYERX_EVENT_SOURCE_BIN') or str(target / 'layerx-event-source'),
        'LAYERX_HUMAN_SERVICE_BIN': os.environ.get('LAYERX_HUMAN_SERVICE_BIN') or str(human_target / 'layerx-human-service'),
        'LAYERX_WEBHOOKS_BIN': os.environ.get('LAYERX_WEBHOOKS_BIN') or str(target / 'layerx-webhooks'),
    }
    absent = [f'{name}={path}' for name, path in required.items() if not os.access(path, os.X_OK)]
    for name in ('PAXEER_X_EVIDENCE_DIR', 'PAXEER_X_EVENT_FIXTURE_DIR'):
        if not os.environ.get(name):
            absent.append(name)
    fixtures = Path(os.environ.get('PAXEER_X_EVENT_FIXTURE_DIR', '/nonexistent'))
    for name in ('receiver', 'source', 'hosted-delivery', 'identity', 'kms', 'receipt-authority'):
        if not (fixtures / name).is_dir():
            absent.append(f'fixture {fixtures / name}')
    services = {entry.get('name') for entry in manifest['services'] if isinstance(entry, dict)}
    for name in ('layerx-event-source', 'layerx-human-service'):
        if name not in services:
            absent.append(f'candidate service {name}')
    if absent:
        raise Missing('principal-delivery-fairness prerequisites absent: ' + ', '.join(absent))
    raise Missing('principal-delivery-fairness execution body not present: two-principal outbox, '
                  'scheduler bound, restart and credential recovery interfaces are undeclared')


CASES = {'webhook-ingress-roles': webhook_ingress_roles,
         'principal-delivery-fairness': principal_delivery_fairness}


def load_manifest(path):
    if not path:
        raise Missing('--candidate-manifest is required')
    try:
        data = Path(path).read_bytes()
        manifest = json.loads(data)
    except (OSError, ValueError) as error:
        raise Missing(f'candidate manifest unreadable: {type(error).__name__}') from None
    if manifest.get('schema') != SCHEMA or not isinstance(manifest.get('services'), list) \
            or not manifest['services']:
        raise Missing(f'candidate manifest is not a {SCHEMA} document with services')
    print(f'candidate sha256={hashlib.sha256(data).hexdigest()}')
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--case', required=True, choices=sorted(CASES))
    parser.add_argument('--candidate-manifest', required=True)
    arguments = parser.parse_args()
    results = Results()
    try:
        manifest = load_manifest(arguments.candidate_manifest)
        CASES[arguments.case](manifest, results)
    except (Missing, subprocess.TimeoutExpired) as error:
        print(f'MISSING {arguments.case}: {error}')
        results.failed += 1
    print(f'RESULT case={arguments.case} assertions={results.passed + results.failed} failures={results.failed}')
    if arguments.case == 'principal-delivery-fairness':
        revision = subprocess.run(['git', '-C', str(ROOT), 'rev-parse', 'HEAD'], capture_output=True,
                                  text=True).stdout.strip() or 'unknown'
        code = 0 if results.failed == 0 and results.passed > 0 else 1
        print(f'PAXEER_X_GATE tests={results.passed + results.failed} skipped=0')
        print(f'result revision={revision} exit={code} evidence={os.environ.get("PAXEER_X_EVIDENCE_DIR", "absent")}')
    return 0 if results.failed == 0 and results.passed > 0 else 1


if __name__ == '__main__':
    sys.exit(main())
