#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import socket
import stat
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[3]
CASES = (
    'producer_schema', 'protected_modes', 'refuse_missing_listener',
    'refuse_unsafe_socket_path', 'refuse_bad_mode', 'refuse_document_wrong_owner_or_mode',
    'refuse_unadmitted_peer', 'admitted_initialize', 'admitted_tools_list',
    'admitted_verified_read', 'restart_reload_no_duplicate_session',
    'web_reference_emitted', 'paid_tool_requires_payer',
    'restart_refuses_changed_authority', 'refuse_listener_owner', 'secret_errors_redacted',
)
TOP_KEYS = {'mode', 'tenant', 'store', 'audit_root', 'session_id', 'session_token_file',
            'session_generation', 'capability_id', 'core_sequence', 'deadline_ms', 'agent',
            'limit', 'listener'}
AGENT_KEYS = {'endpoint', 'bearer_file', 'probe_program'}
LIMIT_KEYS = {'id', 'name', 'scope', 'scope_id', 'ceiling', 'consumed'}
LISTENER_KEYS = {'socket', 'owner_uid', 'owner_gid', 'mode', 'admitted_uids'}
WEB_KEYS = {'endpoint', 'network', 'sequencer_public_key', 'timeout_ms', 'pending_attempts',
            'approval_threshold'}
LISTENER_ENV = ('LAYERX_AGENT_MCP_LISTENER_SOCKET', 'LAYERX_AGENT_MCP_LISTENER_OWNER_UID',
                'LAYERX_AGENT_MCP_LISTENER_OWNER_GID', 'LAYERX_AGENT_MCP_LISTENER_MODE',
                'LAYERX_AGENT_MCP_LISTENER_ADMITTED_UIDS')
WEB_ENV = ('LAYERX_AGENT_MCP_WEB_ENDPOINT', 'LAYERX_AGENT_MCP_WEB_NETWORK',
           'LAYERX_AGENT_MCP_WEB_SEQUENCER_PUBLIC_KEY', 'LAYERX_AGENT_MCP_WEB_TIMEOUT_MS',
           'LAYERX_AGENT_MCP_WEB_PENDING_ATTEMPTS', 'LAYERX_AGENT_MCP_WEB_APPROVAL_THRESHOLD')
SECRET_ENV = ('LAYERX_AGENT_PROGRAM_BEARER_TOKEN', 'LAYERX_AGENT_NODE_BEARER_TOKEN',
              'LAYERX_AGENT_AUTHORITY_BEARER_TOKEN', 'LAYERX_AGENT_HUMAN_AUTHORITY_BEARER')
SOCKET_MODE = '0660'
CLIENT = r'''
import json, socket, sys
path, requests = sys.argv[1], json.loads(sys.argv[2])
connection = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
connection.settimeout(float(sys.argv[3]))
connection.connect(path)
for request in requests:
    connection.sendall((json.dumps(request) + "\n").encode())
connection.shutdown(socket.SHUT_WR)
buffer = b""
try:
    while True:
        chunk = connection.recv(65536)
        if not chunk:
            break
        buffer += chunk
except (ConnectionResetError, socket.timeout) as error:
    print(json.dumps({"transport": type(error).__name__, "responses": []}))
    sys.exit(0)
responses = [json.loads(line) for line in buffer.decode().splitlines() if line.strip()]
print(json.dumps({"transport": "eof", "responses": responses}))
'''


class GateFailure(Exception):
    pass


def require(condition, message):
    if not condition:
        raise GateFailure(message)


def env(name):
    value = os.environ.get(name, '')
    require(value, name + ' is required')
    return value


def capture(command):
    return subprocess.check_output(command, cwd=ROOT, text=True, timeout=60).strip()


def identity():
    require(not capture(['git', 'status', '--porcelain=v1', '--untracked-files=normal']),
            'candidate source is dirty')
    return {'revision': capture(['git', 'rev-parse', 'HEAD^{commit}']),
            'tree': capture(['git', 'rev-parse', 'HEAD^{tree}']), 'dirty': False}


def digest(path):
    with open(path, 'rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def artifact(name):
    path = Path(env(name)).resolve(strict=True)
    info = path.stat()
    require(stat.S_ISREG(info.st_mode) and info.st_size > 0 and os.access(path, os.X_OK),
            'missing or non-executable candidate artifact: ' + name)
    return {'path': str(path), 'sha256': digest(path), 'bytes': info.st_size}


def private_file(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                and not info.st_mode & 0o077, 'private caller-owned file required: ' + str(path))
        return stream.read()


def daemon_environment(path):
    values = {}
    for number, line in enumerate(private_file(path).decode().splitlines(), 1):
        line = line.strip()
        if not line or line.startswith('#'):
            continue
        key, separator, value = line.partition('=')
        require(separator and re.fullmatch(r'[A-Z][A-Z0-9_]*', key),
                f'daemon environment line {number} is not KEY=VALUE')
        require(key not in values, f'daemon environment repeats {key}')
        values[key] = value
    for key in ('LAYERX_AGENT_HUMAN_STORE', 'LAYERX_AGENT_HUMAN_SOCKET', 'LAYERX_AGENT_MCP_MODE',
                'LAYERX_AGENT_MCP_SESSION_ID', 'LAYERX_AGENT_MCP_CAPABILITY_ID',
                'LAYERX_AGENT_MCP_SCOPES', 'LAYERX_AGENT_PROGRAM_LISTEN') + WEB_ENV:
        require(values.get(key), f'provisioned daemon environment lacks {key}')
    require(values['LAYERX_AGENT_MCP_MODE'] == 'full', 'the qualification boots full mode')
    for key in LISTENER_ENV + ('LAYERX_AGENT_MCP_BINDING_ROOT', 'LAYERX_AGENT_MCP_AUDIT_ROOT'):
        require(key not in values, f'{key} is owned by the harness, remove it from the file')
    return values


class Gate:
    def __init__(self):
        self.started = time.time()
        self.source = identity()
        self.agentd = artifact('PAXEER_X_AGENTD_BIN')
        self.mcp = artifact('PAXEER_X_MCP_BIN')
        manifest = json.loads(private_file(Path(env('PAXEER_X_MCP_ARTIFACT_MANIFEST'))))
        require(manifest.get('revision') == self.source['revision']
                and manifest.get('tree') == self.source['tree'],
                'artifact manifest does not name the candidate source')
        for name, binary in (('layerx-agentd', self.agentd), ('layerx-mcp', self.mcp)):
            require(manifest.get('artifacts', {}).get(name) == binary,
                    'candidate artifact differs from its build manifest: ' + name)
            with open(binary['path'], 'rb') as stream:
                require(stream.read(4) == b'\x7fELF', 'candidate artifact is not a native executable')
        self.environment_file = Path(env('PAXEER_X_MCP_DAEMON_ENV')).resolve(strict=True)
        self.daemon = daemon_environment(self.environment_file)
        self.web_scopes = env('PAXEER_X_MCP_WEB_SCOPES')
        self.activity = env('PAXEER_X_MCP_READ_ACTIVITY_ID')
        self.admitted = int(env('PAXEER_X_MCP_ADMITTED_UID'))
        self.refused_uid = int(env('PAXEER_X_MCP_REFUSED_UID'))
        self.group = int(env('PAXEER_X_MCP_SOCKET_GID'))
        require(self.daemon.get('LAYERX_AGENT_MCP_PEER_UID') == str(self.admitted),
                'LAYERX_AGENT_MCP_PEER_UID must name PAXEER_X_MCP_ADMITTED_UID; boot admits the '
                'enrolled peer only')
        require(os.geteuid() == 0, 'peer admission cases run peers under distinct uids; run as root')
        require(len({self.admitted, self.refused_uid, os.geteuid()}) == 3,
                'admitted, refused and daemon uids must be distinct')
        self.boot_seconds = int(os.environ.get('PAXEER_X_MCP_BOOT_SECONDS', '180'))
        require(10 <= self.boot_seconds <= 900, 'PAXEER_X_MCP_BOOT_SECONDS must be 10..900')
        evidence = Path(env('PAXEER_X_EVIDENCE_DIR')).absolute()
        evidence.mkdir(mode=0o700, parents=True, exist_ok=True)
        info = evidence.stat()
        require(info.st_uid == os.geteuid() and not info.st_mode & 0o077,
                'PAXEER_X_EVIDENCE_DIR must be a private caller-owned directory')
        require(evidence != ROOT and ROOT not in evidence.resolve().parents,
                'evidence must stay outside the repository')
        self.run_dir = Path(tempfile.mkdtemp(prefix='mcp-binding-listener-', dir=evidence))
        self.socket_root = Path(tempfile.mkdtemp(prefix='paxeer-x-mcp-socket-')).resolve()
        os.chmod(self.socket_root, 0o711)
        self.logs = []
        self.results = {}
        self.secrets = [self.daemon[key].encode() for key in SECRET_ENV if self.daemon.get(key)]
        self.processes = []
        self.sequence = 0

    def path(self, name):
        return self.run_dir / name

    def log(self, label):
        self.sequence += 1
        path = self.path(f'{self.sequence:02d}-{label}.log')
        self.logs.append(str(path))
        return path

    def state(self, label):
        state = self.path('state-' + label)
        state.mkdir(mode=0o700)
        store = Path(self.daemon['LAYERX_AGENT_HUMAN_STORE'])
        require(store.is_absolute() and store.exists(), 'the provisioned agent store is missing')
        target = state / 'store'
        if store.is_dir():
            shutil.copytree(store, target, symlinks=True)
        else:
            shutil.copy2(store, target)
        socket_dir = self.socket_root / label
        socket_dir.mkdir()
        os.chown(socket_dir, os.geteuid(), self.group)
        os.chmod(socket_dir, 0o750)
        return {'root': state / 'binding', 'audit': state / 'audit', 'store': target,
                'human_socket': state / 'human.sock', 'socket_dir': socket_dir,
                'socket': socket_dir / 'mcp.sock'}

    def daemon_env(self, state, listener=True, web=False, overrides=None):
        values = {key: value for key, value in self.daemon.items() if key not in WEB_ENV}
        values.update({'LAYERX_AGENT_HUMAN_STORE': str(state['store']),
                       'LAYERX_AGENT_HUMAN_SOCKET': str(state['human_socket']),
                       'LAYERX_AGENT_MCP_BINDING_ROOT': str(state['root']),
                       'LAYERX_AGENT_MCP_AUDIT_ROOT': str(state['audit'])})
        state['audit'].mkdir(mode=0o700, exist_ok=True)
        if listener:
            values.update({'LAYERX_AGENT_MCP_LISTENER_SOCKET': str(state['socket']),
                           'LAYERX_AGENT_MCP_LISTENER_OWNER_UID': str(os.geteuid()),
                           'LAYERX_AGENT_MCP_LISTENER_OWNER_GID': str(self.group),
                           'LAYERX_AGENT_MCP_LISTENER_MODE': SOCKET_MODE,
                           'LAYERX_AGENT_MCP_LISTENER_ADMITTED_UIDS': str(self.admitted)})
        if web:
            values.update({key: self.daemon[key] for key in WEB_ENV})
            values['LAYERX_AGENT_MCP_SCOPES'] = self.web_scopes
        values.update(overrides or {})
        base = {key: os.environ[key] for key in ('PATH', 'LANG', 'TZ') if key in os.environ}
        return dict(base, **{key: value for key, value in values.items() if value is not None})

    def spawn(self, command, label, environment):
        log = self.log(label)
        fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        stream = os.fdopen(fd, 'wb')
        stream.write(('COMMAND ' + json.dumps(command) + '\n').encode())
        stream.flush()
        process = subprocess.Popen(command, cwd=self.run_dir, env=environment,
                                   stdin=subprocess.DEVNULL, stdout=stream,
                                   stderr=subprocess.STDOUT, start_new_session=True)
        self.processes.append((process, stream, log))
        return process, log

    def stop(self, process):
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=30)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=30)
        for entry in self.processes:
            if entry[0] is process:
                entry[1].close()

    def wait_for(self, process, path, log):
        deadline = time.monotonic() + self.boot_seconds
        while time.monotonic() < deadline:
            if path.exists() and process.poll() is None:
                return
            require(process.poll() is None,
                    f'process exited {process.returncode} before {path.name} appeared; log={log}')
            time.sleep(0.2)
        raise GateFailure(f'{path.name} did not appear within {self.boot_seconds}s; log={log}')

    def refused(self, process, log, needle):
        try:
            code = process.wait(timeout=self.boot_seconds)
        except subprocess.TimeoutExpired:
            self.stop(process)
            raise GateFailure(f'process kept running instead of refusing; log={log}')
        self.stop(process)
        text = log.read_text(errors='replace')
        require(code != 0, f'process exited 0 instead of refusing; log={log}')
        require(re.search(needle, text, re.I), f'refusal does not name {needle}; log={log}')
        return {'exit': code, 'log': str(log)}

    def boot(self, state, label, **arguments):
        command = [self.agentd['path']]
        process, log = self.spawn(command, label, self.daemon_env(state, **arguments))
        return process, log

    def serve(self, binding, label):
        return self.spawn([self.mcp['path'], str(binding)], label,
                          {key: os.environ[key] for key in ('PATH',) if key in os.environ})

    def peer(self, uid, socket_path, requests):
        command = [sys.executable, '-I', '-c', CLIENT, str(socket_path), json.dumps(requests),
                   '30']
        log = self.log(f'peer-{uid}')
        result = subprocess.run(command, user=uid, group=self.group, extra_groups=[],
                                cwd='/', env={'PATH': os.environ.get('PATH', '/usr/bin')},
                                stdin=subprocess.DEVNULL, capture_output=True, text=True,
                                timeout=90)
        fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'w') as stream:
            stream.write(f'COMMAND {json.dumps(command[:3] + ["<client>"] + command[4:])}\n'
                         f'UID {uid}\nEXIT {result.returncode}\n{result.stdout}\n{result.stderr}\n')
        require(result.returncode == 0, f'peer client failed; log={log}')
        return json.loads(result.stdout), log

    def case(self, name, function):
        require(name not in self.results, 'duplicate case ' + name)
        print('CASE ' + name, flush=True)
        self.results[name] = {'status': 'running'}
        try:
            evidence = function()
        except Exception:
            self.results[name] = {'status': 'failed'}
            raise
        self.results[name] = {'status': 'passed', 'evidence': evidence}

    def scan_secrets(self, binding_root):
        for name in ('session-token', 'daemon-bearer'):
            value = private_file(binding_root / name).strip()
            require(value, name + ' is empty')
            self.secrets.append(value)
        for log in self.logs:
            data = Path(log).read_bytes()
            for secret in self.secrets:
                require(secret not in data, f'a secret value was serialized into {log}')

    def refusal_boot(self, label, **arguments):
        state = self.state(label)
        process, log = self.boot(state, 'agentd-' + label, **arguments)
        outcome = self.refused(process, log, 'listener')
        require(not (state['root'] / 'binding.json').exists(),
                f'a refused boot still published a binding; log={log}')
        require(not state['socket'].exists(), 'a refused boot left a socket')
        return outcome

    def run(self):
        target = self.socket_root / 'link-target'
        target.mkdir()
        os.chown(target, os.geteuid(), self.group)
        os.chmod(target, 0o750)
        link = self.socket_root / 'linked'
        os.symlink(target, link)
        boots = {
            'missing': self.refusal_boot('missing-listener', listener=False),
            'owner': self.refusal_boot('wrong-listener-owner',
                overrides={'LAYERX_AGENT_MCP_LISTENER_OWNER_UID': str(self.refused_uid)}),
            'relative': self.refusal_boot(
                'relative-socket',
                overrides={'LAYERX_AGENT_MCP_LISTENER_SOCKET': 'relative/mcp.sock'}),
            'non_canonical': self.refusal_boot(
                'linked-socket',
                overrides={'LAYERX_AGENT_MCP_LISTENER_SOCKET': str(link / 'mcp.sock')}),
            'missing_parent': self.refusal_boot(
                'missing-parent',
                overrides={'LAYERX_AGENT_MCP_LISTENER_SOCKET':
                           str(self.socket_root / 'absent' / 'mcp.sock')}),
            'modes': {mode: self.refusal_boot(
                'mode-' + mode, overrides={'LAYERX_AGENT_MCP_LISTENER_MODE': mode})
                for mode in ('0666', '0060', '0770')},
        }
        main = self.state('main')
        daemon, daemon_log = self.boot(main, 'agentd-main')
        binding = main['root'] / 'binding.json'
        self.wait_for(daemon, binding, daemon_log)
        document = json.loads(private_file(binding))
        self.scan_secrets(main['root'])
        published = {name: (os.stat(main['root'] / name).st_ino, digest(main['root'] / name))
                     for name in ('binding.json', 'session-token', 'daemon-bearer')}

        def producer_schema():
            require(set(document) == TOP_KEYS, 'binding top-level keys differ: '
                    + json.dumps(sorted(set(document) ^ TOP_KEYS)))
            require(set(document['agent']) == AGENT_KEYS, 'binding agent keys differ')
            require(set(document['limit']) == LIMIT_KEYS, 'binding limit keys differ')
            listener = document['listener']
            require(set(listener) == LISTENER_KEYS, 'binding listener keys differ')
            require(listener == {'socket': str(main['socket']), 'owner_uid': os.geteuid(),
                                 'owner_gid': self.group, 'mode': SOCKET_MODE,
                                 'admitted_uids': [self.admitted]},
                    'listener section does not carry the configured listener')
            require(document['mode'] == 'full', 'binding mode is not full')
            require(document['store'] == str(main['store'])
                    and document['audit_root'] == str(main['audit']), 'store/audit paths differ')
            require(document['session_id'] == self.daemon['LAYERX_AGENT_MCP_SESSION_ID'].lower(),
                    'binding session differs from the enrolled session')
            require(document['capability_id']
                    == self.daemon['LAYERX_AGENT_MCP_CAPABILITY_ID'].lower(),
                    'binding capability differs from the enrolled capability')
            require(document['session_token_file'] == str(main['root'] / 'session-token')
                    and document['agent']['bearer_file'] == str(main['root'] / 'daemon-bearer'),
                    'secret file references differ')
            require(document['agent']['endpoint'] == self.daemon['LAYERX_AGENT_PROGRAM_LISTEN'],
                    'agent endpoint differs from the daemon listener')
            require(isinstance(document['tenant'], str) and document['tenant'],
                    'tenant reference missing')
            for key in ('session_generation', 'core_sequence', 'deadline_ms'):
                require(isinstance(document[key], int) and document[key] >= 0, key + ' invalid')
            return {'binding': str(binding), 'sha256': digest(binding), 'log': str(daemon_log)}

        def protected_modes():
            info = os.lstat(main['root'])
            require(stat.S_ISDIR(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o700
                    and info.st_uid == os.geteuid(), 'binding directory is not owner-only')
            modes = {}
            for name in ('binding.json', 'session-token', 'daemon-bearer'):
                info = os.lstat(main['root'] / name)
                require(stat.S_ISREG(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o600
                        and info.st_uid == os.geteuid(), name + ' is not an owner-only file')
                modes[name] = oct(stat.S_IMODE(info.st_mode))
            token = private_file(main['root'] / 'session-token')
            require(re.fullmatch(rb'[0-9a-f]{64}\n', token), 'session token file shape differs')
            return modes

        self.case('refuse_listener_owner', lambda: boots['owner'])
        self.case('producer_schema', producer_schema)
        self.case('protected_modes', protected_modes)

        def refuse_missing_listener():
            boot = boots['missing']
            stripped = {key: value for key, value in document.items() if key != 'listener'}
            copy = self.path('binding-without-listener.json')
            fd = os.open(copy, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            with os.fdopen(fd, 'w') as stream:
                json.dump(stripped, stream)
            process, log = self.serve(copy, 'mcp-missing-listener')
            return {'daemon': boot, 'socket_binary': self.refused(process, log, 'socket')}

        def refuse_unsafe_socket_path():
            outcomes = {key: boots[key]
                        for key in ('relative', 'non_canonical', 'missing_parent')}
            os.chmod(main['socket_dir'], 0o757)
            try:
                process, log = self.serve(binding, 'mcp-open-socket-dir')
                outcomes['world_reachable_parent'] = self.refused(process, log, 'not owned')
            finally:
                os.chmod(main['socket_dir'], 0o750)
            os.chmod(main['socket_dir'], 0o770)
            try:
                process, log = self.serve(binding, 'mcp-group-writable-socket-dir')
                outcomes['group_writable_parent'] = self.refused(process, log, 'not owned')
            finally:
                os.chmod(main['socket_dir'], 0o750)
            main['socket'].touch(mode=0o600)
            try:
                process, log = self.serve(binding, 'mcp-socket-present')
                outcomes['present_path'] = self.refused(process, log, 'already present')
            finally:
                main['socket'].unlink()
            return outcomes

        def refuse_bad_mode():
            return boots['modes']

        def refuse_document_wrong_owner_or_mode():
            before = digest(binding)
            outcomes = {}
            os.chmod(binding, 0o640)
            try:
                process, log = self.serve(binding, 'mcp-document-mode')
                outcomes['mode_0640'] = self.refused(process, log, 'unreadable')
            finally:
                os.chmod(binding, 0o600)
            os.chown(binding, self.refused_uid, -1)
            try:
                process, log = self.serve(binding, 'mcp-document-owner')
                outcomes['foreign_owner'] = self.refused(process, log, 'unreadable')
            finally:
                os.chown(binding, os.geteuid(), -1)
            for name, field in (('session-token', 'session_token_file'),
                                ('daemon-bearer', 'agent.bearer_file')):
                secret = main['root'] / name
                os.chmod(secret, 0o644)
                try:
                    process, log = self.serve(binding, 'mcp-' + name + '-mode')
                    outcomes[name + '_mode_0644'] = self.refused(process, log, re.escape(field))
                finally:
                    os.chmod(secret, 0o600)
                os.chown(secret, self.refused_uid, -1)
                try:
                    process, log = self.serve(binding, 'mcp-' + name + '-owner')
                    outcomes[name + '_foreign_owner'] = self.refused(process, log, re.escape(field))
                finally:
                    os.chown(secret, os.geteuid(), -1)
            require(digest(binding) == before, 'the published binding changed during refusals')
            return outcomes

        self.case('refuse_missing_listener', refuse_missing_listener)
        self.case('refuse_unsafe_socket_path', refuse_unsafe_socket_path)
        self.case('refuse_bad_mode', refuse_bad_mode)
        self.case('refuse_document_wrong_owner_or_mode', refuse_document_wrong_owner_or_mode)

        def served_session(label, stale=None):
            process, log = self.serve(binding, label)
            self.wait_for(process, main['socket'], log)
            deadline = time.monotonic() + self.boot_seconds
            while True:
                try:
                    info = os.lstat(main['socket'])
                except FileNotFoundError:
                    info = None
                if (info is not None and info.st_ino != stale
                        and stat.S_ISSOCK(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o660
                        and info.st_uid == os.geteuid() and info.st_gid == self.group):
                    return process, log
                require(process.poll() is None and time.monotonic() < deadline,
                        f'the bound socket does not carry the declared owner and mode; log={log}')
                time.sleep(0.1)

        requests = [
            {'jsonrpc': '2.0', 'id': 1, 'method': 'initialize',
             'params': {'protocolVersion': '2025-06-18', 'capabilities': {},
                        'clientInfo': {'name': 'paxeer-x-qualification', 'version': '1'}}},
            {'jsonrpc': '2.0', 'method': 'notifications/initialized'},
            {'jsonrpc': '2.0', 'id': 2, 'method': 'tools/list'},
            {'jsonrpc': '2.0', 'id': 3, 'method': 'tools/call',
             'params': {'name': 'receipt.get', 'arguments': {'activity_id': self.activity}}},
        ]

        def exchange():
            reply, log = self.peer(self.admitted, main['socket'], requests)
            responses = {entry.get('id'): entry for entry in reply['responses']}
            require(reply['transport'] == 'eof' and set(responses) == {1, 2, 3},
                    f'admitted peer did not receive three responses; log={log}')
            return responses, log

        mcp, mcp_log = served_session('mcp-main')
        responses, peer_log = exchange()

        def refuse_unadmitted_peer():
            reply, log = self.peer(self.refused_uid, main['socket'], requests)
            require(reply['responses'] == [] and reply['transport'] in ('eof', 'ConnectionResetError'),
                    f'an unadmitted peer was not closed promptly; log={log}')
            require(mcp.poll() is None, 'the listener stopped after refusing a peer')
            after, again = self.peer(self.admitted, main['socket'], requests[:1])
            require(len(after['responses']) == 1 and 'result' in after['responses'][0],
                    f'the listener stopped serving admitted peers after a refusal; log={again}')
            return {'refused_uid': self.refused_uid, 'log': str(log), 'admitted_after': str(again)}

        def admitted_initialize():
            result = responses[1].get('result')
            require(isinstance(result, dict) and result.get('_meta', {}).get(
                'layerx/binding') == 'agent-daemon' and result['_meta'].get(
                'layerx/deployment_mode') == 'full', f'initialize differs; log={peer_log}')
            return {'protocolVersion': result.get('protocolVersion'), 'log': str(peer_log)}

        def admitted_tools_list():
            tools = responses[2].get('result', {}).get('tools')
            require(isinstance(tools, list) and tools, f'tools/list is empty; log={peer_log}')
            names = sorted(tool.get('name') for tool in tools)
            require('receipt.get' in names, 'the verified read tool is not served')
            return {'tools': names, 'log': str(peer_log)}

        def admitted_verified_read():
            result = responses[3].get('result')
            require(isinstance(result, dict) and result.get('isError') is False,
                    f'the verified read was refused or unverifiable; log={peer_log}')
            content = result.get('structuredContent')
            require(isinstance(content, dict) and content.get('kind') == 'receipt'
                    and content.get('complete') is True and content.get('verification_level') == 2
                    and content.get('activity_id') == self.activity.lower(),
                    f'the read returned no verified receipt for the requested activity; log={peer_log}')
            for field, length in (('canonical_hex', None), ('header_hex', None),
                                  ('header_signature', 128), ('sequencer_public_key', 64)):
                value = content.get(field)
                require(isinstance(value, str) and re.fullmatch(r'(?:[0-9a-f]{2})+', value)
                        and (length is None or len(value) == length),
                        f'receipt evidence field {field} is invalid; log={peer_log}')
            proof = content.get('proof', {})
            require(isinstance(proof.get('leaf_index'), int)
                    and isinstance(proof.get('leaf_count'), int)
                    and 0 <= proof['leaf_index'] < proof['leaf_count']
                    and isinstance(proof.get('siblings'), list)
                    and all(isinstance(value, str) and re.fullmatch(r'[0-9a-f]{64}', value)
                            for value in proof['siblings']),
                    f'receipt inclusion proof is absent or malformed; log={peer_log}')
            return {'tool': 'receipt.get', 'fields': sorted(content), 'log': str(peer_log)}

        self.case('refuse_unadmitted_peer', refuse_unadmitted_peer)
        self.case('admitted_initialize', admitted_initialize)
        self.case('admitted_tools_list', admitted_tools_list)
        self.case('admitted_verified_read', admitted_verified_read)

        def restart_reload_no_duplicate_session():
            self.stop(mcp)
            self.stop(daemon)
            stale = None
            if main['socket'].exists():
                info = os.lstat(main['socket'])
                require(stat.S_ISSOCK(info.st_mode),
                        'the stopped listener left a non-socket at its path')
                stale = info.st_ino
            stale_fd = os.open(main['socket'], os.O_PATH) if stale is not None else None
            restarted, restart_log = self.boot(main, 'agentd-restart')
            deadline = time.monotonic() + self.boot_seconds
            listening = self.daemon['LAYERX_AGENT_PROGRAM_LISTEN'].rsplit(':', 1)
            while True:
                require(restarted.poll() is None,
                        f'the daemon refused to restart on its own binding; log={restart_log}')
                try:
                    socket.create_connection((listening[0], int(listening[1])), 1).close()
                    break
                except OSError:
                    require(time.monotonic() < deadline, f'restart timed out; log={restart_log}')
                    time.sleep(0.2)
            current = {name: (os.stat(main['root'] / name).st_ino, digest(main['root'] / name))
                       for name in published}
            require(current == published, 'restart rewrote the binding or minted a second session')
            try:
                served, served_log = served_session('mcp-restart', stale)
            finally:
                if stale_fd is not None:
                    os.close(stale_fd)
            again, again_log = exchange()
            require(again[3].get('result', {}).get('isError') is False,
                    f'the reloaded binding lost daemon authority; log={again_log}')
            self.stop(served)
            self.stop(restarted)
            return {'daemon_log': str(restart_log), 'mcp_log': str(served_log),
                    'peer_log': str(again_log), 'unchanged': sorted(published)}

        self.case('restart_reload_no_duplicate_session', restart_reload_no_duplicate_session)
        self.scan_secrets(main['root'])

        def restart_refuses_changed_authority():
            outcomes = {}
            process, log = self.boot(main, 'agentd-changed-listener', overrides={
                'LAYERX_AGENT_MCP_LISTENER_ADMITTED_UIDS': f'{self.admitted},{self.refused_uid}'})
            outcomes['changed_admission'] = self.refused(process, log, 'binding')
            changed_bearer = os.urandom(32).hex()
            self.secrets.append(changed_bearer.encode())
            process, log = self.boot(main, 'agentd-changed-bearer', overrides={
                'LAYERX_AGENT_PROGRAM_BEARER_TOKEN': changed_bearer})
            outcomes['changed_bearer'] = self.refused(process, log, 'secret')
            secret = main['root'] / 'daemon-bearer'
            os.chmod(secret, 0o640)
            try:
                process, log = self.boot(main, 'agentd-unprotected-secret')
                outcomes['unprotected_secret'] = self.refused(process, log, 'secret')
            finally:
                os.chmod(secret, 0o600)
            require({name: (os.stat(main['root'] / name).st_ino, digest(main['root'] / name))
                     for name in published} == published,
                    'refused restart modified authority')
            return outcomes

        def secret_errors_redacted():
            secret = private_file(main['root'] / 'session-token').decode().strip()
            altered = dict(document)
            altered[secret] = True
            copy = self.path('binding-unknown-field.json')
            fd = os.open(copy, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            with os.fdopen(fd, 'w') as stream:
                json.dump(altered, stream)
            process, log = self.serve(copy, 'mcp-unknown-field')
            outcome = self.refused(process, log, 'unaccepted field')
            self.scan_secrets(main['root'])
            return outcome

        self.case('restart_refuses_changed_authority', restart_refuses_changed_authority)
        self.case('secret_errors_redacted', secret_errors_redacted)
        self.scan_secrets(main['root'])
        web = self.state('web')
        web_daemon, web_log = self.boot(web, 'agentd-web', web=True)
        web_binding = web['root'] / 'binding.json'
        self.wait_for(web_daemon, web_binding, web_log)
        web_document = json.loads(private_file(web_binding))
        self.scan_secrets(web['root'])

        def web_reference_emitted():
            section = web_document.get('web')
            require(isinstance(section, dict) and set(section) == WEB_KEYS,
                    'the web section is absent or not closed')
            expected = {
                'endpoint': self.daemon['LAYERX_AGENT_MCP_WEB_ENDPOINT'],
                'network': self.daemon['LAYERX_AGENT_MCP_WEB_NETWORK'],
                'sequencer_public_key':
                    self.daemon['LAYERX_AGENT_MCP_WEB_SEQUENCER_PUBLIC_KEY'].lower(),
                'timeout_ms': int(self.daemon['LAYERX_AGENT_MCP_WEB_TIMEOUT_MS']),
                'pending_attempts': int(self.daemon['LAYERX_AGENT_MCP_WEB_PENDING_ATTEMPTS']),
                'approval_threshold': self.daemon['LAYERX_AGENT_MCP_WEB_APPROVAL_THRESHOLD'],
            }
            require(section == expected, 'the web section differs from the configured references')
            require(set(web_document) == TOP_KEYS | {'web'}, 'web binding top-level keys differ')
            return {'binding': str(web_binding), 'sha256': digest(web_binding)}

        def paid_tool_requires_payer():
            process, log = self.serve(web_binding, 'mcp-web-without-payer')
            outcome = self.refused(process, log, 'payer')
            require(not web['socket'].exists(), 'a refused web binding left a socket')
            return outcome

        self.case('web_reference_emitted', web_reference_emitted)
        self.case('paid_tool_requires_payer', paid_tool_requires_payer)
        self.stop(web_daemon)
        self.scan_secrets(web['root'])

    def close(self, error):
        for process, stream, _ in self.processes:
            if process.poll() is None:
                self.stop(process)
            if not stream.closed:
                stream.close()
        shutil.rmtree(self.socket_root, ignore_errors=True)
        skipped = [name for name in CASES if name not in self.results]
        report = {'revision': self.source, 'command': 'timeout 30m python3 '
                  'tools/qualification/paxeer-x/mcp_binding_listener.py',
                  'artifacts': {'layerx-agentd': self.agentd, 'layerx-mcp': self.mcp},
                  'daemon_environment_file': str(self.environment_file),
                  'cases': self.results, 'skipped': skipped, 'error': error,
                  'evidence': str(self.run_dir), 'logs': self.logs,
                  'seconds': round(time.time() - self.started, 3)}
        path = self.run_dir / 'result.json'
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'w') as stream:
            json.dump(report, stream, indent=2, sort_keys=True)
            stream.write('\n')
        print('EVIDENCE ' + str(path), flush=True)
        return skipped


def main():
    def interrupted(signum, _frame):
        raise GateFailure('qualification interrupted by signal ' + str(signum))
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        gate = Gate()
    except (GateFailure, OSError, ValueError, subprocess.SubprocessError) as error:
        print('FAIL ' + str(error), file=sys.stderr, flush=True)
        return 1
    error = None
    try:
        gate.run()
    except (GateFailure, OSError, ValueError, KeyError, subprocess.SubprocessError) as failure:
        error = f'{type(failure).__name__}: {failure}'
    skipped = gate.close(error)
    passed = sum(1 for result in gate.results.values() if result['status'] == 'passed')
    if error or skipped:
        print('FAIL ' + (error or 'cases did not run: ' + ','.join(skipped)),
              file=sys.stderr, flush=True)
        return 1
    print(f'PAXEER_X_GATE tests={passed} skipped={len(skipped)}', flush=True)
    return 0


if __name__ == '__main__':
    sys.exit(main())
