#!/usr/bin/env python3
"""Qualify source ABI bindings through a disposable production registry process.

Before the single candidate build, run --snapshot-build; afterwards run
--record-artifacts with its Cargo JSON log. Neither mode compiles anything.
The default gate consumes that record and provisioned protocol deployments.
PAXEER_X_SOURCE_ABI_INPUTS contains journal/ and abi{1,2,3,4}/, each with
program-id, source.uri, source.archive, source.plan, module.wasm and mismatch/ and
unsupported/ directories containing source.uri, source.archive, source.plan, module.wasm.
Archives and plans use the production SourceArchive and BuildPlan encodings.
All authority material and builder quota mounts must be provisioned separately.
"""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import socket
import ssl
import stat
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[3]
COMMAND = 'timeout 30m python3 tools/qualification/paxeer-x/programs_source_abi.py'
BUILD_COMMAND = ('cargo build --locked --manifest-path platform/Cargo.toml '
                 '-p layerx-platform-registry --bin layerx-program-registry '
                 '--bin layerx-cgroup-exec --message-format=json')
PREFIX = 'PAXEER_X_SOURCE_ABI_'
CONTAINER_STATE = '/run/layerx/source-abi-state'
TARGETS = {'layerx-program-registry', 'layerx-cgroup-exec'}
IMPORTS = {1: ('layerx_v1', 'storage_read'), 2: ('layerx_v2', 'response_write'),
           3: ('layerx_v3', 'oracle_read'), 4: ('layerx_v4', 'web_read')}
PROVENANCE = {'program_id', 'version', 'abi_version', 'code_hash', 'activity_id',
              'receipt_digest', 'batch_header_digest', 'state_root', 'programs_root',
              'observed_sequence', 'observed_at'}


def require(value, message):
    if not value:
        raise ValueError(message)


def setting(name):
    value = os.environ.get(PREFIX + name)
    require(value, PREFIX + name + ' is required')
    return value


def capture(command):
    result = subprocess.run(command, cwd=ROOT, stdin=subprocess.DEVNULL,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            timeout=60, text=True)
    require(result.returncode == 0, 'command failed: ' + command[0])
    return result.stdout.strip()


def identity():
    require(not capture(['git', 'status', '--porcelain=v1', '--untracked-files=normal']),
            'candidate checkout must be clean')
    return {'revision': capture(['git', 'rev-parse', 'HEAD^{commit}']),
            'tree': capture(['git', 'rev-parse', 'HEAD^{tree}'])}


def digest(data):
    return hashlib.sha256(data).hexdigest()


def artifact(path):
    path = Path(path).resolve(strict=True)
    info = path.stat()
    require(stat.S_ISREG(info.st_mode) and info.st_size > 0,
            'missing nonempty artifact: ' + str(path))
    with path.open('rb') as stream:
        hashed = hashlib.file_digest(stream, 'sha256').hexdigest()
    return {'path': str(path), 'sha256': hashed, 'bytes': info.st_size}


def private_directory(path):
    raw = Path(path).absolute()
    require(not raw.is_symlink(), 'private directory cannot be a symlink')
    path = raw.resolve(strict=True)
    info = path.stat()
    require(path != ROOT and ROOT not in path.parents and stat.S_ISDIR(info.st_mode)
            and info.st_uid == os.geteuid() and not info.st_mode & 0o077,
            'caller-owned private directory outside repository required')
    return path


def load_private(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd) as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                and not info.st_mode & 0o077 and info.st_nlink == 1,
                'private singly linked caller-owned record required')
        return json.load(stream)


def write_private(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())


def snapshot(directory):
    write_private(directory / 'build-inputs.json', {'source': identity(),
                  'command': BUILD_COMMAND, 'started_ns': time.time_ns()})


def record(directory, cargo_json):
    before = load_private(directory / 'build-inputs.json')
    require(before['source'] == identity(), 'candidate changed during build')
    require(Path(cargo_json).stat().st_mtime_ns >= before['started_ns'],
            'Cargo output predates candidate input snapshot')
    binaries = {}
    finished = []
    for line in Path(cargo_json).read_text().splitlines():
        event = json.loads(line)
        if event.get('reason') == 'build-finished':
            finished.append(event.get('success'))
        target = event.get('target', {})
        if (event.get('reason') == 'compiler-artifact' and target.get('name') in TARGETS
                and target.get('kind') == ['bin'] and event.get('executable')):
            require(not event['profile']['test'] and event.get('features') == [],
                    'test binary or unexpected features cannot qualify hosted process')
            require(Path(event['manifest_path']).resolve() ==
                    ROOT / 'platform/hosted/registry/Cargo.toml',
                    'Cargo artifact belongs to a different source checkout')
            name = target['name']
            require(name not in binaries, 'duplicate candidate binary')
            binaries[name] = artifact(event['executable'])
    require(finished and all(value is True for value in finished)
            and set(binaries) == TARGETS, 'successful production registry and supervisor build required')
    write_private(directory / 'artifacts.json', dict(before, artifacts=binaries,
                  cargo_json=artifact(cargo_json), completed_ns=time.time_ns()))


def input_files(root):
    require(root.is_dir() and not root.is_symlink(), 'provisioned inputs are absent')
    require(not any(path.is_symlink() for path in root.rglob('*')), 'input symlink refused')
    paths = []
    for abi in range(1, 5):
        base = root / ('abi' + str(abi))
        paths.append(base / 'program-id')
        for directory in (base, base / 'mismatch', base / 'unsupported'):
            for name in ('source.uri', 'source.archive', 'source.plan', 'module.wasm'):
                paths.append(directory / name)
    journal = root / 'journal'
    require(journal.is_dir(), 'genuine deployment journal is required')
    records = sorted(path for path in journal.iterdir() if path.is_file() and
                     (path.name == 'head' or re.fullmatch(
                         '[0-9a-f]{64}[.](envelope|admission|deployment)', path.name)))
    require(any(path.suffix == '.envelope' for path in records)
            or any(path.suffix == '.admission' for path in records),
            'genuine admission proofs are absent')
    paths.extend(records)
    return [artifact(path) for path in paths]


def read_token(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd) as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and not info.st_mode & 0o007
                and info.st_nlink == 1 and info.st_size <= 8192, 'unsafe token file')
        token = stream.read().strip()
    require(token and '\n' not in token and '\r' not in token, 'invalid credential framing')
    return token


def wasm_imports(data):
    require(data[:8] == b'\0asm\x01\0\0\0', 'fixture is not Wasm v1')
    cursor = 8

    def leb():
        nonlocal cursor
        value = 0
        for shift in range(0, 35, 7):
            require(cursor < len(data), 'truncated Wasm fixture')
            byte = data[cursor]
            cursor += 1
            value |= (byte & 127) << shift
            if byte < 128:
                return value
        raise ValueError('oversized Wasm fixture integer')

    def string():
        nonlocal cursor
        size = leb()
        end = cursor + size
        require(end <= len(data), 'truncated Wasm import')
        value = data[cursor:end].decode('utf-8')
        cursor = end
        return value

    while cursor < len(data):
        section = data[cursor]
        cursor += 1
        size = leb()
        end = cursor + size
        require(end <= len(data), 'truncated Wasm section')
        if section == 2:
            imports = []
            for _ in range(leb()):
                module, name = string(), string()
                require(cursor < end and data[cursor] == 0, 'fixture imports must be functions')
                cursor += 1
                leb()
                imports.append((module, name))
            require(cursor == end, 'noncanonical fixture import section')
            return imports
        cursor = end
    return []


def source_input(path):
    uri = (path / 'source.uri').read_text().strip()
    parsed = urllib.parse.urlsplit(uri)
    require(parsed.scheme in ('https', 'ipfs') and parsed.netloc
            and not parsed.username and not any(char.isspace() for char in uri),
            'provisioned canonical source URI required')
    archive = (path / 'source.archive').read_bytes()
    plan = (path / 'source.plan').read_text()
    module = (path / 'module.wasm').read_bytes()
    require(archive.startswith(b'LXSRCv1\0') and len(archive) > 12,
            'canonical production source archive required')
    fields = {}
    for line in plan.splitlines():
        if line.strip() and not line.lstrip().startswith('#'):
            key, value = line.split('=', 1)
            require(key.strip() not in fields, 'duplicate source plan field')
            fields[key.strip()] = value.strip()
    require(fields.get('version') == '1', 'production BuildPlan v1 required')
    for key in ('toolchain_digest', 'dependency_lock_digest', 'builder_image_digest'):
        require(re.fullmatch('[0-9a-f]{64}', fields.get(key, '')), 'missing source plan pin')
    return {'uri': uri, 'archive': archive, 'plan': plan, 'module': module, 'fields': fields}


class Gate:
    def __init__(self, directory, record_value):
        self.root = Path(tempfile.mkdtemp(prefix='source-abi-', dir=directory))
        self.record = record_value
        self.results = []
        self.container = None
        self.launch_number = 0
        self.request_number = 0
        self.deadline = time.monotonic() + 1740
        self.state = self.root / 'state'
        self.state.mkdir(mode=0o700)
        self.inputs = private_directory(setting('INPUTS'))
        self.initial_inputs = input_files(self.inputs)
        require(os.geteuid() == 0 and setting('DISPOSABLE') == '1',
                'provisioned disposable root-operated boundary required')
        self.image = setting('RUNTIME_IMAGE')
        require(re.fullmatch(r'sha256:[0-9a-f]{64}', self.image),
                'runtime image must be a local immutable image ID')
        require(capture(['docker', 'image', 'inspect', '--format', '{{.Id}}', self.image]) == self.image,
                'runtime image is unavailable')
        self.url = setting('URL').rstrip('/')
        parsed = urllib.parse.urlsplit(self.url)
        require(parsed.scheme == 'https' and parsed.hostname in ('127.0.0.1', '::1')
                and parsed.port and not parsed.username and not parsed.path
                and not parsed.query and not parsed.fragment, 'loopback HTTPS listener URL required')
        self.host, self.port = parsed.hostname, parsed.port
        self.material = Path(setting('MATERIAL')).resolve(strict=True)
        self.quota = Path(setting('QUOTA_ROOT')).resolve(strict=True)
        require(self.quota.is_dir() and self.material.is_dir(), 'provisioned mounts missing')
        require(any(path.is_dir() and path.stat().st_dev != self.quota.stat().st_dev
                    for path in self.quota.iterdir()), 'dedicated mounted hard-quota slots required')
        for slot in self.quota.iterdir():
            if slot.is_dir():
                require(all(path.name in ('.layerx-build-lock', 'lost+found')
                            for path in slot.iterdir()), 'qualification quota slot is not empty')
        self.environment = {key: value for key, value in os.environ.items()
                            if key.startswith(('LAYERX_REGISTRY_', 'LAYERX_EVENTS_'))}
        for name in ('BUILDER_ENVIRONMENT_ROOT', 'BUILDER_ENTRYPOINT', 'BUILDER_IMAGE_DIGEST',
                     'BUILDER_ISOLATION_RUNTIME', 'BUILDER_ISOLATION_RUNTIME_DIGEST',
                     'NODE_ENDPOINT', 'NODE_AUTHORIZATION', 'RECEIPT_AUTHORITY_ENDPOINT',
                     'RECEIPT_AUTHORITY_AUTHORIZATION', 'RECEIPT_AUTHORITY_REPLICA_ID',
                     'SEQUENCER_TRUST_HISTORY', 'TLS_CERT_DER', 'TLS_KEY_DER',
                     'CLIENT_CA_DER', 'OUTBOUND_CA_DER', 'CLIENT_IDENTITY_PKCS12',
                     'CLIENT_IDENTITY_PASSWORD_FILE', 'REQUEST_TOKEN_FILE',
                     'PUBLICATION_TOKEN_FILE', 'IDENTITY_URL'):
            require(self.environment.get('LAYERX_REGISTRY_' + name),
                    'missing production configuration: LAYERX_REGISTRY_' + name)
        require('LAYERX_REGISTRY_LNI_SOCKET' not in self.environment,
                'use provisioned HTTPS node evidence boundary for this gate')
        for name in ('PROGRAM', 'WEBHOOKS'):
            require(self.environment.get('LAYERX_EVENTS_' + name + '_UPSTREAM_URL'),
                    'real event upstream configuration required')
        self.builder = Path(self.environment['LAYERX_REGISTRY_BUILDER_ENVIRONMENT_ROOT']).resolve(strict=True)
        for key, value in self.environment.items():
            if key.endswith(('_TOKEN_FILE', '_CA_DER', '_CERT_DER', '_KEY_DER',
                             '_PKCS12', '_PASSWORD_FILE', '_TRUST_HISTORY')):
                path = Path(value).resolve(strict=True)
                require(self.material in path.parents,
                        'authority file must be inside the provisioned material directory: ' + key)
        context = ssl.create_default_context(cafile=setting('CLIENT_CA_PEM'))
        context.load_cert_chain(setting('CLIENT_CERT_PEM'), setting('CLIENT_KEY_PEM'))
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}),
                                                  urllib.request.HTTPSHandler(context=context))
        self.request_token = read_token(self.environment['LAYERX_REGISTRY_REQUEST_TOKEN_FILE'])
        self.publication_token = read_token(self.environment['LAYERX_REGISTRY_PUBLICATION_TOKEN_FILE'])
        self.publication_key = read_token(setting('PUBLICATION_KEY_FILE'))
        (self.state / 'journal').mkdir(mode=0o700)
        for saved in self.initial_inputs:
            path = Path(saved['path'])
            if path.parent == self.inputs / 'journal':
                shutil.copyfile(path, self.state / 'journal' / path.name)
        for name in ('sources', 'verified'):
            (self.state / name).mkdir(mode=0o700)
        for path in [self.state, *self.state.rglob('*')]:
            require(not path.is_symlink(), 'disposable state symlink refused')
            os.chown(path, 4030, 4030)
            os.chmod(path, 0o700 if path.is_dir() else 0o600)
        self.environment.update({
            'LAYERX_REGISTRY_STATE': CONTAINER_STATE,
            'LAYERX_REGISTRY_JOURNAL': CONTAINER_STATE + '/journal',
            'LAYERX_REGISTRY_SOURCE_MIRROR': CONTAINER_STATE + '/sources',
            'LAYERX_REGISTRY_VERIFIED': CONTAINER_STATE + '/verified',
            'LAYERX_REGISTRY_BUILD_ROOT': str(self.quota),
            'LAYERX_REGISTRY_HOST_CGROUP_MOUNT': '/run/layerx/source-abi-cgroup',
            'LAYERX_REGISTRY_BUILDER_JOB_SUPERVISOR': '/run/layerx/source-abi-supervisor',
            'LAYERX_REGISTRY_BUILDER_JOB_SUPERVISOR_DIGEST':
                self.record['artifacts']['layerx-cgroup-exec']['sha256'],
            'LAYERX_REGISTRY_LISTEN': ('[' + self.host + ']' if ':' in self.host else self.host) + ':' + str(self.port),
            'LAYERX_REGISTRY_ATTEMPTS': '2',
        })

    def remaining(self):
        require(time.monotonic() < self.deadline, 'qualification deadline exceeded')
        return max(1, min(180, self.deadline - time.monotonic()))

    def case(self, name, evidence):
        require(name not in {row['name'] for row in self.results}, 'duplicate qualification case')
        self.results.append({'name': name, 'status': 'passed', 'evidence': evidence})
        write_private(self.root / ('case-' + name + '.json'), self.results[-1])

    def state_info(self):
        return json.loads(capture(['docker', 'inspect', '--format', '{{json .State}}', self.container]))

    def start(self, expected_refusal=None):
        self.remaining()
        require(self.container is None, 'a candidate process is already running')
        with socket.socket(socket.AF_INET6 if ':' in self.host else socket.AF_INET) as sock:
            sock.bind((self.host, self.port))
        self.launch_number += 1
        command = ['docker', 'run', '--detach', '--network', 'host', '--cgroupns', 'private',
                   '--read-only', '--user', '0:0', '--cap-drop', 'ALL',
                   '--cap-add', 'CHOWN', '--cap-add', 'SETUID', '--cap-add', 'SETGID',
                   '--security-opt', 'no-new-privileges', '--entrypoint', '/run/layerx/source-abi-registry']
        mounts = [(self.state, CONTAINER_STATE, False), (self.material, str(self.material), True),
                  (self.builder, str(self.builder), True), (self.quota, str(self.quota), False),
                  (Path('/sys/fs/cgroup'), '/run/layerx/source-abi-cgroup', False),
                  (Path(self.record['artifacts']['layerx-program-registry']['path']),
                   '/run/layerx/source-abi-registry', True),
                  (Path(self.record['artifacts']['layerx-cgroup-exec']['path']),
                   '/run/layerx/source-abi-supervisor', True)]
        for source, destination, readonly in mounts:
            require(',' not in str(source), 'unsupported mount path')
            mount = 'type=bind,src=' + str(source) + ',dst=' + destination
            command.extend(['--mount', mount + (',readonly' if readonly else '')])
        for key in sorted(self.environment):
            command.extend(['--env', key])
        command.append(self.image)
        result = subprocess.run(command, env=dict(os.environ, **self.environment),
                                stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, text=True, timeout=60)
        require(result.returncode == 0, 'disposable candidate container launch failed')
        self.container = result.stdout.strip()
        require(re.fullmatch('[0-9a-f]{64}', self.container), 'invalid container identity')
        limit = time.monotonic() + min(90, self.remaining())
        while time.monotonic() < limit:
            state = self.state_info()
            if not state['Running']:
                require(expected_refusal is not None and state['ExitCode'] != 0,
                        'candidate exited before readiness')
                log = self.logs()
                require(expected_refusal in log, 'candidate failed outside the required refusal boundary')
                return
            try:
                status, _ = self.http('/healthz', save=False)
                if status == 200:
                    require(expected_refusal is None, 'corrupt provenance was accepted at startup')
                    executable = Path('/proc') / str(state['Pid']) / 'exe'
                    with executable.open('rb') as stream:
                        running_digest = hashlib.file_digest(stream, 'sha256').hexdigest()
                    expected = self.record['artifacts']['layerx-program-registry']
                    require(running_digest == expected['sha256']
                            and executable.stat().st_size == expected['bytes'],
                            'running executable differs from candidate artifact')
                    write_private(self.root / f'process-{self.launch_number}.json',
                                  {'container': self.container, 'pid': state['Pid'],
                                   'image': self.image, 'executable': expected})
                    return
            except (OSError, urllib.error.URLError):
                pass
            time.sleep(0.1)
        raise ValueError('candidate readiness/refusal deadline exceeded')

    def logs(self):
        result = subprocess.run(['docker', 'logs', self.container], stdout=subprocess.PIPE,
                                stderr=subprocess.STDOUT, text=True, timeout=15)
        require(result.returncode == 0, 'candidate log read failed')
        return result.stdout

    def stop(self):
        if self.container:
            subprocess.run(['docker', 'stop', '--time', '10', self.container],
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=20, check=True)
            log = self.root / f'process-{self.launch_number}.log'
            fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(fd, 'w') as stream:
                stream.write(self.logs())
            subprocess.run(['docker', 'rm', self.container], stdout=subprocess.DEVNULL,
                           stderr=subprocess.DEVNULL, timeout=20, check=True)
            self.container = None

    def http(self, path, body=None, key=None, save=True):
        publication = path == '/__registry/sources'
        headers = {'Authorization': 'Bearer ' + (self.publication_token if publication else self.request_token),
                   'Content-Type': 'application/json'}
        if body is not None:
            headers['Layerx-Key'] = self.publication_key
        if key:
            headers['Idempotency-Key'] = key
        data = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(self.url + path, data=data, headers=headers)
        try:
            response = self.opener.open(request, timeout=self.remaining())
        except urllib.error.HTTPError as error:
            response = error
        with response:
            raw = response.read(16 * 1024 * 1024 + 1)
            require(len(raw) <= 16 * 1024 * 1024, 'oversized hosted response')
            document = json.loads(raw)
            status = response.status
        if save:
            self.request_number += 1
            write_private(self.root / f'http-{self.request_number:04d}.json',
                          {'path': path, 'status': status, 'response': document})
        return status, document

    def publish(self, fixture, suffix):
        uri = fixture['uri']
        status, body = self.http('/__registry/sources', {
            'source_uri': uri, 'plan': fixture['plan'], 'archive_hex': fixture['archive'].hex()})
        require(status == 200 and body.get('mirrored') is True
                and body.get('source_digest') == digest(fixture['archive']), 'real source publication failed')
        return {'source_uri': uri, 'source_digest': body['source_digest']}

    def refuse(self, name, path, request, code, status=422, detail=None, key=None):
        actual, body = self.http(path, request, key or str(uuid.uuid4()))
        require(actual == status and body.get('error', {}).get('code') == code,
                name + ': wrong refusal boundary')
        if detail:
            require(detail in body['error'].get('detail', ''), name + ': wrong refusal reason')
        self.case(name, {'response': f'http-{self.request_number:04d}.json'})

    def check_binding(self, document, abi, program, module, before=None):
        require(document['program_id'] == program and document['abi_version'] == abi
                and document['code_hash'] == digest(module)
                and document['reproduced_artifact_digest'] == digest(module)
                and document['source']['status'] == 'verified', 'source result lost deployment ABI binding')
        provenance = document['deployment_provenance']
        require(set(provenance) == PROVENANCE, 'incomplete verified deployment provenance')
        require(provenance['program_id'] == program and provenance['abi_version'] == abi
                and provenance['version'] == document['version']
                and provenance['code_hash'] == digest(module)
                and provenance['receipt_digest'] == document['deployment_receipt_digest'],
                'source result provenance disagrees with deployment')
        for key in ('activity_id', 'receipt_digest', 'batch_header_digest', 'state_root', 'programs_root'):
            require(re.fullmatch('[0-9a-f]{64}', provenance[key]) and int(provenance[key], 16),
                    'missing genuine deployment evidence identity')
        require(provenance['observed_sequence'] > 0 and provenance['observed_at'] > 0,
                'missing deployment freshness evidence')
        if before is not None:
            require(provenance == before['deployment_provenance'], 'deployment provenance changed on retry')
            for field in ('source_uri', 'source_digest', 'environment_digest'):
                require(document[field] == before[field], 'source build binding changed on retry')
        return provenance

    def request_record(self, program, response):
        records = []
        for path in (self.state / 'journal' / 'verification-requests').glob('*.request'):
            value = json.loads(path.read_text())
            if (value['program'] == program and value['state']['phase'] == 'completed'
                    and json.loads(value['state']['response']['body']) == response):
                records.append((path, value))
        require(len(records) == 1, 'exactly one durable completed request is required')
        path, value = records[0]
        require(value['attempt'] == 1, 'idempotent source retry rebuilt its artifact')
        return artifact(path)

    def projection(self, program, expected):
        status, body = self.http('/v1/programs/registry/' + program)
        require(status == 200 and body['receipt']['verification'] == 'receipt-verified'
                and re.fullmatch('[0-9a-f]{64}', body.get('discovery_public_key', ''))
                and re.fullmatch('[0-9a-f]{128}', body.get('discovery_signature', ''))
                and re.fullmatch('[0-9a-f]{64}', body.get('receipt_digest', '')),
                'current receipt-verified projection is unavailable')
        version = next(row for row in body['versions'] if row['version'] == expected['version'])
        require(version['abi_version'] == expected['abi_version']
                and version['code_hash'] == expected['code_hash']
                and version['deployment_receipt_digest'] == expected['deployment_receipt_digest']
                and version['deployment_provenance'] == expected['deployment_provenance']
                and version['source']['status'] == 'verified', 'persisted registry projection lost binding')

    def exercise(self):
        self.start()
        positives = []
        verified_digests = set()
        for abi in range(1, 5):
            base = self.inputs / ('abi' + str(abi))
            fixture = source_input(base)
            require(IMPORTS[abi] in wasm_imports(fixture['module']), 'ABI-specific fixture import absent')
            program = (base / 'program-id').read_text().strip()
            require(re.fullmatch('[0-9a-f]{64}', program) and int(program, 16), 'invalid provisioned program ID')
            path = '/v1/programs/registry/' + program + '/source'
            require(digest(fixture['archive']) not in verified_digests,
                    'ABI fixtures must have distinct source archives')
            request = self.publish(fixture, 'abi' + str(abi))
            mirror = self.state / 'sources' / (request['source_digest'] + '.archive')
            original = mirror.read_bytes()
            try:
                mirror.write_bytes(original[:-1] + bytes([original[-1] ^ 1]))
                self.refuse(f'abi{abi}-altered-source', path, request, 'source_unverifiable',
                            detail='mirrored archive hashes to')
            finally:
                mirror.write_bytes(original)
            for field, detail in (('toolchain_digest', 'toolchain pin'),
                                  ('dependency_lock_digest', 'dependency lock')):
                mutated = dict(fixture)
                replacement = '01' * 32 if fixture['fields'][field] != '01' * 32 else '02' * 32
                mutated['plan'] = '\n'.join(field + ' = ' + replacement
                    if line.partition('=')[0].strip() == field else line
                    for line in fixture['plan'].splitlines()) + '\n'
                bad = self.publish(mutated, f'abi{abi}-{field}')
                self.refuse(f'abi{abi}-altered-{field}', path, bad, 'source_unverifiable', detail=detail)
            request = self.publish(fixture, 'abi' + str(abi))
            key = str(uuid.uuid4())
            status, positive = self.http(path, request, key)
            require(status == 200, 'ABI source verification did not succeed')
            require(positive['source_uri'] == request['source_uri']
                    and positive['source_digest'] == request['source_digest'],
                    'source result differs from the requested canonical archive')
            self.check_binding(positive, abi, program, fixture['module'])
            verified_digests.add(digest(fixture['archive']))
            self.projection(program, positive)
            self.case(f'abi{abi}-reproducible', {'source_response': positive,
                                              'independent_attempts': 2})
            completed = self.request_record(program, positive)
            status, repeated = self.http(path, request, key)
            require(status == 200 and repeated == positive, 'same-request retry changed result')
            require(self.request_record(program, positive) == completed,
                    'same-request retry changed the completed durable request')
            self.case(f'abi{abi}-retry', {'response': f'http-{self.request_number:04d}.json'})
            for requested in (0, 5, 65535, 1 if abi != 1 else 2):
                self.refuse(f'abi{abi}-caller-abi-{requested}', path,
                            dict(request, abi_version=requested), 'invalid_argument', 400)
            for field, value in (('version', positive['version'] + 1),
                                 ('deployment_receipt_digest', '01' * 32), ('code_hash', digest(fixture['module']))):
                self.refuse(f'abi{abi}-caller-{field}', path, dict(request, **{field: value}),
                            'invalid_argument', 400)
            for variant in ('mismatch', 'unsupported'):
                other = source_input(base / variant)
                require(other['module'] != fixture['module']
                        and digest(other['archive']) not in verified_digests,
                        'negative fixture duplicates positive source or artifact')
                imports = wasm_imports(other['module'])
                if variant == 'unsupported':
                    unsupported_import = next(((module, name) for module, name in imports
                        if module not in {f'layerx_v{n}' for n in range(1, abi + 1)}), None)
                    require(unsupported_import is not None, 'unsupported fixture lacks foreign ABI import')
                bad = self.publish(other, f'abi{abi}-{variant}')
                if variant == 'mismatch':
                    self.refuse(f'abi{abi}-{variant}', path, bad, 'source_mismatch', 409)
                else:
                    self.refuse(f'abi{abi}-{variant}', path, bad, 'source_unverifiable',
                                detail='forbidden import ' + '::'.join(unsupported_import))
            request = self.publish(fixture, 'abi' + str(abi))
            status, final_positive = self.http(path, request, str(uuid.uuid4()))
            require(status == 200, 'positive source did not recover after refusal')
            self.check_binding(final_positive, abi, program, fixture['module'], positive)
            unknown = digest(b'LayerX/source-abi/unverified\0' + bytes.fromhex(program))
            self.refuse(f'abi{abi}-unverified-deployment', '/v1/programs/registry/' + unknown + '/source',
                        request, 'not_found', 404)
            positives.append((abi, program, fixture, request, positive, key, completed))
        self.stop()
        self.start()
        for abi, program, fixture, request, positive, key, completed in positives:
            self.projection(program, positive)
            status, repeated = self.http('/v1/programs/registry/' + program + '/source', request, key)
            require(status == 200 and repeated == positive, 'retry after restart changed the result')
            require(artifact(completed['path']) == completed,
                    'retry after restart changed the original durable request')
            self.check_binding(repeated, abi, program, fixture['module'], positive)
            self.case(f'abi{abi}-restart', {'response': f'http-{self.request_number:04d}.json'})
        self.stop()
        abi, program, fixture, request, positive, key, completed = positives[0]
        record_path = self.state / 'verified' / f'{program}-{positive["version"]}.verified'
        saved = record_path.read_bytes()
        original = json.loads(saved)
        require('deployment_provenance' in original, 'production store omitted deployment provenance')
        changes = [('abi_version', 5), ('abi_version-relabel', 2)]
        changes += [(field, '01' * 32 if original['deployment_provenance'][field] != '01' * 32 else '02' * 32)
                    for field in ('code_hash', 'activity_id', 'receipt_digest', 'batch_header_digest',
                                  'state_root', 'programs_root')]
        changes += [(field, original['deployment_provenance'][field] + 1)
                    for field in ('observed_sequence', 'observed_at')]
        for field, value in changes:
            changed = copy.deepcopy(original)
            changed['deployment_provenance'][field.removesuffix('-relabel')] = value
            try:
                record_path.write_text(json.dumps(changed))
                self.start(expected_refusal=('corrupt' if field == 'abi_version'
                                            else 'mismatched deployment provenance'))
                self.case('persisted-' + field, {'log': f'process-{self.launch_number}.log'})
            finally:
                self.stop()
                record_path.write_bytes(saved)
        for field in ('deployment_provenance',):
            changed = copy.deepcopy(original)
            del changed[field]
            try:
                record_path.write_text(json.dumps(changed))
                self.start(expected_refusal='corrupt')
                self.case('persisted-missing-' + field, {'log': f'process-{self.launch_number}.log'})
            finally:
                self.stop()
                record_path.write_bytes(saved)
        changed = copy.deepcopy(original)
        changed['program'] = digest(b'LayerX/source-abi/absent\0' + bytes.fromhex(program))
        try:
            record_path.write_text(json.dumps(changed))
            self.start(expected_refusal='no verified deployment')
            self.case('matching-hash-without-deployment', {'log': f'process-{self.launch_number}.log'})
        finally:
            self.stop()
            record_path.write_bytes(saved)
        self.start()
        for _, program, _, _, positive, _, _ in positives:
            self.projection(program, positive)
        self.case('provenance-refusal-recovery', {'response': f'http-{self.request_number:04d}.json'})
        self.stop()
        require(input_files(self.inputs) == self.initial_inputs, 'provisioned source/evidence inputs changed')


def required_cases():
    cases = set()
    for abi in range(1, 5):
        names = {'reproducible', 'retry', 'altered-source', 'altered-toolchain_digest',
                 'altered-dependency_lock_digest', 'mismatch', 'unsupported',
                 'unverified-deployment', 'restart', 'caller-version',
                 'caller-deployment_receipt_digest', 'caller-code_hash'}
        names.update('caller-abi-' + str(value) for value in (0, 5, 65535, 1 if abi != 1 else 2))
        cases.update(f'abi{abi}-' + name for name in names)
    cases.update('persisted-' + name for name in (
        'abi_version', 'abi_version-relabel', 'code_hash', 'activity_id', 'receipt_digest',
        'batch_header_digest', 'state_root', 'programs_root', 'observed_sequence', 'observed_at',
        'missing-deployment_provenance'))
    cases.update({'matching-hash-without-deployment', 'provenance-refusal-recovery'})
    return cases


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    group = parser.add_mutually_exclusive_group()
    group.add_argument('--snapshot-build', action='store_true')
    group.add_argument('--record-artifacts', metavar='CARGO_JSON')
    args = parser.parse_args()
    gate = None
    directory = None
    candidate = None
    try:
        candidate = {'revision': capture(['git', 'rev-parse', 'HEAD^{commit}'])}
        if not os.environ.get(PREFIX + 'EVIDENCE'):
            directory = Path(tempfile.mkdtemp(prefix='source-abi-refusal-'))
            raise ValueError(PREFIX + 'EVIDENCE is required')
        directory = private_directory(setting('EVIDENCE'))
        if args.snapshot_build:
            snapshot(directory)
            return 0
        if args.record_artifacts:
            record(directory, args.record_artifacts)
            return 0
        value = load_private(directory / 'artifacts.json')
        require(value['source'] == identity(), 'artifact producer differs from candidate')
        candidate = value['source']
        require(set(value['artifacts']) == TARGETS, 'candidate binary inventory incomplete')
        for saved in [*value['artifacts'].values(), value['cargo_json']]:
            require(artifact(saved['path']) == saved, 'candidate artifact changed')
        gate = Gate(directory, value)
        gate.exercise()
        require({case['name'] for case in gate.results} == required_cases(),
                'required source ABI cases were omitted')
        require(identity() == value['source'], 'candidate changed during gate')
        for saved in value['artifacts'].values():
            require(artifact(saved['path']) == saved, 'candidate artifact changed during gate')
        write_private(gate.root / 'result.json', {'source': value['source'], 'command': COMMAND,
                      'exit_code': 0, 'cases': gate.results, 'skipped': 0,
                      'artifacts': value['artifacts'], 'inputs': gate.initial_inputs,
                      'evidence': str(gate.root)})
        print(f'PAXEER_X_GATE tests={len(gate.results)} skipped=0 evidence={gate.root}')
        return 0
    except Exception as error:
        if directory is not None:
            failure = (gate.root if gate is not None else directory) / ('refused-' + uuid.uuid4().hex + '.json')
            passed = gate.results if gate is not None else []
            unfinished = required_cases() - {case['name'] for case in passed}
            write_private(failure, {'source': candidate, 'command': COMMAND, 'exit_code': 1,
                          'status': 'unqualified', 'error': str(error),
                          'planned_cases': sorted(required_cases()),
                          'cases': passed + [{'name': name, 'status': 'not_completed'}
                                             for name in sorted(unfinished)],
                          'evidence': str(failure.parent), 'skipped': 0})
            print(f'source ABI qualification refused; evidence={failure}', file=sys.stderr)
        else:
            fallback = Path(tempfile.mkdtemp(prefix='source-abi-refusal-')) / 'refused.json'
            write_private(fallback, {'source': candidate, 'command': COMMAND, 'exit_code': 1,
                          'status': 'unqualified', 'error': str(error),
                          'planned_cases': sorted(required_cases()),
                          'cases': [{'name': name, 'status': 'not_completed'}
                                    for name in sorted(required_cases())], 'skipped': 0})
            print(f'source ABI qualification refused; evidence={fallback}', file=sys.stderr)
        return 1
    finally:
        if gate is not None:
            gate.stop()


if __name__ == '__main__':
    sys.exit(main())
