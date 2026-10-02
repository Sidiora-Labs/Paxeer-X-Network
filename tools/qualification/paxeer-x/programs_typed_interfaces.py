#!/usr/bin/env python3
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import socket
import ssl
import stat
import subprocess
import sys
import tempfile
import time
import urllib.request
from urllib.parse import urlsplit

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
COMMAND = 'timeout 30m python3 tools/qualification/paxeer-x/programs_typed_interfaces.py'
OUTPUTS = ('client.rs', 'client.ts', 'guest.rs', 'client.go', 'ProgramBindings.java',
           'client.kt', 'client.py', 'client.swift', 'Client.cs')
ROLES = {'node': 'layerxd', 'registry': 'layerx-program-registry',
         'boundary': 'layerx-agent-boundary', 'gateway': 'layerx-gateway'}


def require(value, message):
    if not value:
        raise ValueError(message)


def protected(path):
    path = Path(path)
    require(path.is_absolute() and path.resolve(strict=True) == path and not path.is_symlink(),
            'canonical absolute configuration path required')
    info = path.stat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid() and info.st_nlink == 1
            and not info.st_mode & 0o077 and 0 < info.st_size <= 1048576,
            'private bounded caller-owned regular configuration required')
    return path


def load(path):
    return json.loads(protected(path).read_text())


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def artifact(path):
    path = Path(path).resolve(strict=True)
    require(path.is_file() and path.stat().st_size > 0, 'nonempty artifact required')
    return {'path': str(path), 'sha256': digest(path), 'bytes': path.stat().st_size}


def save(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write('\n')


def identity():
    def git(*args):
        return subprocess.check_output(['git', *args], cwd=ROOT, text=True, timeout=30).strip()
    require(not git('status', '--porcelain=v1', '--untracked-files=normal'), 'clean candidate required')
    return {'revision': git('rev-parse', 'HEAD^{commit}'), 'tree': git('rev-parse', 'HEAD^{tree}')}


def request(config, path):
    url = config['url'].rstrip('/') + path
    require(url.startswith(('https://localhost:', 'https://127.0.0.1:', 'http://127.0.0.1:')),
            'local owned qualification service required')
    token = protected(config['token_file']).read_text().strip()
    require(token and '\r' not in token and '\n' not in token, 'invalid provisioned bearer')
    context = None
    if url.startswith('https:'):
        context = ssl.create_default_context(cafile=str(protected(config['ca_file'])))
        context.load_cert_chain(str(protected(config['certificate_file'])),
                                str(protected(config['private_key_file'])))
    query = urllib.request.Request(url, headers={'Authorization': 'Bearer ' + token})
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, req, fp, code, msg, headers, newurl):
            raise ValueError('authority redirects are forbidden')
    opener = urllib.request.build_opener(NoRedirect(), urllib.request.HTTPSHandler(context=context))
    with opener.open(query, timeout=30) as response:
        data = response.read(16777217)
        require(len(data) <= 16777216, 'authority response exceeded bound')
        return json.loads(data)


class Processes:
    def __init__(self, rows, artifacts, directory):
        require(set(rows) == set(ROLES), 'actual native/registry/boundary/gateway process inventory required')
        self.rows, self.artifacts, self.directory = rows, artifacts, directory
        self.children, self.streams, self.round = {}, [], 0
        self.environments = {role: load(row['environment_file']) for role, row in rows.items()}
        self.ports = {}
        for role, environment in self.environments.items():
            key = {'node':'LAYERX_NODE_PROGRAM_PORT', 'boundary':'LAYERX_AGENT_BOUNDARY_LISTEN',
                   'registry':'LAYERX_REGISTRY_LISTEN', 'gateway':'LAYERX_GATEWAY_LISTEN'}[role]
            listen = environment[key]
            if role != 'node':
                require(listen.startswith('127.0.0.1:'), 'disposable listener must bind loopback')
                listen = listen.rsplit(':',1)[1]
            self.ports[role] = int(listen)
        require(len(set(self.ports.values())) == 4, 'production listeners must be distinct')

    def start(self):
        self.round += 1
        for role in ('node', 'boundary', 'registry', 'gateway'):
            row = self.rows[role]
            binary = self.artifacts[role]['path']
            require(Path(binary).name == ROLES[role], 'unexpected production process binary')
            environment = self.environments[role]
            require(isinstance(environment, dict) and all(isinstance(k, str) and isinstance(v, str)
                    for k, v in environment.items()), 'invalid process environment')
            state = Path(row['state_directory']).resolve(strict=True)
            require(state != ROOT and ROOT not in state.parents and state.stat().st_uid == row['uid']
                    and not state.stat().st_mode & 0o077, 'private disposable daemon-owned state required')
            require(isinstance(row['uid'], int) and isinstance(row['gid'], int)
                    and row['arguments'] == [],
                    'invalid process identity or argv')
            try:
                occupied = socket.create_connection(('127.0.0.1',self.ports[role]),timeout=0.2)
            except OSError:
                pass
            else:
                occupied.close()
                raise ValueError('qualification refuses a listener already owned by another process')
            stream = (self.directory / f'{role}-{self.round}.log').open('xb')
            self.streams.append(stream)
            self.children[role] = subprocess.Popen([binary, *row['arguments']], cwd=state, env=environment,
                user=row['uid'], group=row['gid'], extra_groups=[], stdin=subprocess.DEVNULL,
                stdout=stream, stderr=subprocess.STDOUT, start_new_session=True)
        return {role: child.pid for role, child in self.children.items()}

    def alive(self):
        require(all(child.poll() is None for child in self.children.values()),
                'required production process exited; inspect process logs')

    def stop(self):
        for child in reversed(list(self.children.values())):
            if child.poll() is None:
                os.killpg(child.pid, signal.SIGTERM)
                try:
                    child.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    os.killpg(child.pid, signal.SIGKILL)
                    child.wait(timeout=5)
        self.children.clear()
        for stream in self.streams:
            if not stream.closed:
                stream.close()


def qualify(manifest_path):
    manifest = load(manifest_path)
    require(set(manifest) == {'schema', 'source', 'artifacts', 'build_logs', 'processes',
        'cli_environment_file', 'native_http', 'authority_http', 'trust_history', 'actor', 'evidence_directory', 'consumers'},
        'unexpected manifest fields')
    require(manifest['schema'] == 'paxeer-x.typed-interface-artifacts.v1', 'wrong manifest schema')
    source = identity()
    require(manifest['source'] == source, 'artifacts do not bind exact clean candidate')
    artifacts = manifest['artifacts']
    require(set(artifacts) == set(ROLES) | {'cli', 'fixture-tests', 'registry-tests', 'sdk-tests'},
            'missing candidate executable')
    for row in list(artifacts.values()) + manifest['build_logs']:
        require(artifact(row['path']) == row, 'candidate artifact or build log changed')
    require(manifest['build_logs'] and all(os.access(row['path'], os.X_OK) for row in artifacts.values()),
            'candidate executables and build provenance required')
    parent = Path(manifest['evidence_directory']).resolve(strict=True)
    require(parent.stat().st_uid == os.geteuid() and not parent.stat().st_mode & 0o077
            and parent != ROOT and ROOT not in parent.parents, 'private evidence directory required')
    directory = Path(tempfile.mkdtemp(prefix='typed-interfaces-', dir=parent))
    record = {'task': '7.4', 'source': source, 'command': COMMAND, 'exit_code': 1,
              'cases': [], 'commands': [], 'skipped': 0, 'qualification_complete': False}
    deadline = time.monotonic() + 1650
    processes = Processes(manifest['processes'], artifacts, directory)
    environment = load(manifest['cli_environment_file'])
    require(environment.get('LAYERX_CREDENTIAL_STORE') == 'file'
            and 'LAYERX_CREDENTIAL_PASSPHRASE' in environment and 'LAYERX_CONFIG' in environment,
            'actual provisioned encrypted CLI credentials required')
    configuration = load(environment['LAYERX_CONFIG'])
    active = configuration['environments'][configuration['current_environment']]
    for role, url in [('node',manifest['native_http']['url']), ('boundary',manifest['authority_http']['url']),
                      ('gateway',active['endpoint'])]:
        parsed = urlsplit(url)
        require(parsed.hostname in ('127.0.0.1','localhost') and parsed.port == processes.ports[role]
                and not parsed.username and not parsed.password and parsed.path in ('','/'),
                'service endpoint does not bind the owned candidate listener')
    gateway = processes.environments['gateway']
    for key, role in [('LAYERX_GATEWAY_COMPONENT_URL','boundary'), ('LAYERX_GATEWAY_AUTHORITY_URL','boundary'),
                      ('LAYERX_GATEWAY_PROGRAM_REGISTRY_URL','registry')]:
        parsed = urlsplit(gateway[key])
        require(parsed.hostname in ('127.0.0.1','localhost') and parsed.port == processes.ports[role],
                'gateway does not use the owned candidate boundary/authority/registry')
    record['process_commands'] = [[artifacts[role]['path']] for role in ('node','boundary','registry','gateway')]
    actor = manifest['actor']
    require(configuration['keys'][actor['key']]['public_key'] == actor['public_key']
            and active['sequencer_trust_anchor'] == actor['sequencer_key'], 'authority configuration mismatch')
    require(re.fullmatch('[0-9a-f]{64}', actor['public_key'])
            and re.fullmatch('[0-9a-f]{64}', actor['sequencer_key']), 'invalid public authority identity')
    consumers = manifest['consumers']
    languages = {'rust':'client.rs','typescript':'client.ts','go':'client.go','java':'ProgramBindings.java',
                 'kotlin':'client.kt','python':'client.py','swift':'client.swift','csharp':'Client.cs'}
    required_consumers = {(abi, phase, language) for abi in range(1,5)
                          for phase in ('initial','upgrade') for language in languages}
    require(len(consumers) == 64 and {(row['abi'],row['phase'],row['language']) for row in consumers}
            == required_consumers, 'all ABI/version/language consumers are required')
    sequence = actor['account_sequence']
    require(isinstance(sequence, int) and sequence >= 0, 'initial account sequence required')
    protected(manifest['trust_history'])
    environment = dict(os.environ) | environment | {'PYTHONDONTWRITEBYTECODE': '1'}

    def run(argv, label, env=environment, refusal=None, cwd=ROOT):
        require(time.monotonic() < deadline, 'task deadline exceeded')
        result = subprocess.run(argv, cwd=cwd, env=env, stdin=subprocess.DEVNULL,
            capture_output=True, timeout=min(90, deadline-time.monotonic()))
        path = directory / f'{label}.json'
        save(path, {'command': argv, 'exit_code': result.returncode,
                   'stdout': result.stdout.decode(), 'stderr': result.stderr.decode()})
        record['commands'].append({'command': argv, 'exit_code': result.returncode, 'log': artifact(path)})
        require((result.returncode == 0 if refusal is None else
                 result.returncode not in (0, 124, 127) and refusal in result.stderr.decode()),
                f'{label}: declared result absent; inspect {path}')
        return result

    def cli(args, label, refusal=None):
        result = run([artifacts['cli']['path'], '--json', '--gateway-credential', actor['gateway_credential'], *args], label, refusal=refusal)
        record['cases'].append(label)
        if refusal is not None:
            return None
        value = json.loads(result.stdout)
        require(value['ok'] is True, 'CLI reported failure')
        return value['data']

    def head():
        processes.alive()
        value = request(manifest['native_http'], '/v1/protocol/account-state/head')
        require(re.fullmatch('[0-9a-f]{64}', value['state_root']), 'native head omitted state root')
        return value['state_root']

    def ready():
        until = min(deadline, time.monotonic()+90)
        while True:
            try:
                for port in processes.ports.values():
                    with socket.create_connection(('127.0.0.1',port), timeout=0.2):
                        pass
                return head()
            except (OSError, ValueError):
                processes.alive()
                require(time.monotonic() < until, 'native readiness deadline exceeded')
                time.sleep(0.2)

    def signing():
        now = int(time.time()*1000)
        return ['--key', actor['key'], '--idempotency-key', os.urandom(32).hex(),
                '--account-sequence', str(sequence), '--not-before-ms', str(now),
                '--expires-at-ms', str(now+300000), '--fee-limit', actor['fee_limit']]

    def generate(deployed, fixture, label):
        proof = request(manifest['authority_http'], '/internal/v1/deployment-proof/' + deployed['activity_id'])
        path = directory / f'{label}.deployment'
        path.write_bytes(bytes.fromhex(proof['proof_hex']))
        output = directory / label
        wasm = fixture.parent / ('upgrade.wasm' if fixture.name == 'upgrade.bin' else 'module.wasm')
        cli(['program', 'bindings', '--historical', '--interface', str(fixture), '--deployment-proof', str(path),
             '--trust-history', manifest['trust_history'], '--digest', digest(fixture),
             '--code-hash', digest(wasm), '--output', str(output)], label)
        return output

    def language_consumers(output, abi, phase):
        expected_cases = {'roundtrip_call','typed_failure','stale_digest','wrong_code_hash','malformed_call'}
        for row in (row for row in consumers if row['abi'] == abi and row['phase'] == phase):
            language = row['language']; prefix = f'abi{abi}-{phase}/{language}'
            expected = consumer_sources / prefix
            require(row['compile_exit_code'] == 0 and row['compile_command'], 'consumer compilation did not succeed')
            for saved in [row['compile_log'],row['compiler'],row['runtime'],*row['sources'].values(),*row['outputs']]:
                require(artifact(saved['path']) == saved, 'consumer source, compiler or artifact changed')
            require(set(row['sources']) == {str(path.relative_to(expected)) for path in expected.rglob('*') if path.is_file()},
                    'consumer source closure is incomplete')
            for relative, saved in row['sources'].items():
                require(digest(expected/relative) == saved['sha256'], 'consumer source differs from production emitter')
            binding = row['binding']
            require(binding in row['sources'] and digest(output/languages[language]) == row['sources'][binding]['sha256'],
                    'compiled consumer does not use the receipt-bound CLI binding')
            command = row['command']
            require(command and command[0] == row['runtime']['path'], 'unbound consumer runtime')
            runtime = Path(command[0]).name
            output_paths = {value['path'] for value in row['outputs']}
            if command[0] not in output_paths:
                require(runtime in {'node','python3','java','dotnet','mono'}, 'compiler execution is forbidden in verify')
                require('-c' not in command and '-m' not in command, 'inline interpreter execution refused')
                if runtime == 'dotnet':
                    require(len(command)>1 and command[1].endswith('.dll'), 'only compiled .NET consumers admitted')
                if runtime == 'java':
                    require(not any(arg.endswith('.java') for arg in command), 'Java source launch would compile')
            cwd = Path(row['cwd']).resolve(strict=True)
            closure = {saved['path'] for saved in row['sources'].values()} | output_paths
            require({str(path.resolve()) for path in cwd.rglob('*') if path.is_file()} == closure,
                    'consumer execution directory contains unbound files')
            result = run(command, f'abi{abi}-{phase}-{language}', cwd=cwd)
            cases = re.findall(r'^BINDING_CASE ([a-z_]+)$', result.stdout.decode(), re.M)
            require(len(cases) == len(expected_cases) and set(cases) == expected_cases,
                    'consumer omitted canonical calldata/result/error/refusal cases')
            record['cases'].extend(f'abi{abi}-{phase}-{language}-{case}' for case in cases)

    def client_at(output, abi):
        name = output.name.replace('-', '_')
        spec = importlib.util.spec_from_file_location(name, output/'client.py')
        client = importlib.util.module_from_spec(spec); sys.modules[name] = client; spec.loader.exec_module(client)
        require(client.INTERFACE_ABI_VERSION == abi, 'generated ABI metadata differs')
        call = client.Client(client.CODE_HASH, client.INTERFACE_DIGEST).entry_call_01020304(
            client.Entry_01020304Input(b'typed-interface'))
        require(call.as_bytes() == bytes.fromhex('0102030401200000000f') + b'typed-interface',
                'noncanonical generated calldata')
        require(call.decode_failure(7, bytes.fromhex('011009')).detail == 9, 'typed failure decoding changed')
        try:
            call.decode_output(bytes.fromhex('ff20'))
        except client.BindingRefusal:
            pass
        else:
            raise ValueError('generated decoder admitted an invalid response schema')
        return client, call

    def invoke(program, abi, call, label):
        value = cli(['program','call',program,'--abi-version',str(abi),'--entrypoint','call',
                     '--calldata',call.as_bytes().hex(),'--fuel','1000000',*signing()], label)
        require(value['result_code'] == 0 and value['outcome']['status'] == 'completed'
                and value['receipt'] and value['call_graph'], 'real call lacks verified execution')
        if abi >= 2:
            require(call.decode_output(bytes.fromhex(value['outcome']['response'])) == b'typed-interface',
                    'typed response differs from real guest echo')

    try:
        inputs = directory/'inputs'; inputs.mkdir(mode=0o700)
        output = run([artifacts['fixture-tests']['path'], '--exact', 'native_interfaces::emit_typed_interface_inputs',
            '--nocapture','--test-threads=1'], 'inputs',
            env=dict(environment, PAXEER_X_TYPED_INTERFACE_INPUTS=str(inputs)))
        require(re.findall(r'test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;', output.stdout.decode())
                == [('1','0','0')], 'input producer absent or skipped')
        require(re.findall(r'^TYPED_INTERFACE_INPUT (abi[1-4])$', output.stdout.decode(), re.M)
                == ['abi1','abi2','abi3','abi4'], 'real input producer omitted ABI cases')
        for name, expected in [('registry-tests','interface::conformance_vectors::typed_interfaces_require_the_exact_module_abi_and_capabilities')]:
            result = run([artifacts[name]['path'],'--exact',expected,'--test-threads=1'], name)
            require(re.findall(r'test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;', result.stdout.decode())
                    == [('1','0','0')], 'required source case absent or skipped')
            record['cases'].append(expected)
        consumer_sources = directory/'consumer-sources'; consumer_sources.mkdir(mode=0o700)
        emitted = run([artifacts['sdk-tests']['path'],'--exact','bindgen::vectors::typed_interfaces_retain_versioned_capabilities_in_every_language','--test-threads=1'],
            'consumer-source-producer', env=dict(environment, PAXEER_X_TYPED_INTERFACE_INPUTS=str(inputs),
                PAXEER_X_TYPED_CONSUMER_OUTPUT=str(consumer_sources)))
        require(re.findall(r'test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;', emitted.stdout.decode())
                == [('1','0','0')], 'consumer producer absent or skipped')
        record['cases'].append('bindgen::vectors::typed_interfaces_retain_versioned_capabilities_in_every_language')
        before = processes.start(); ready()
        stored = []
        for abi in range(1, 5):
            fixture = inputs/f'abi{abi}'; program = os.urandom(32).hex()
            flags = signing() + ['--program-id',program,'--previous-state-root',head()]
            for name, reason in [('wrong-hash','another code hash'),('unknown-abi','canonical'),
                                 ('wrong-schema','canonical'),('wrong-capability','module admission refused')]:
                cli(['program','deploy',str(fixture/'module.wasm'),'--interface',str(fixture/f'{name}.bin'),*flags],
                    f'abi{abi}-{name}', refusal=reason)
            deployed = cli(['program','deploy',str(fixture/'module.wasm'),'--interface',str(fixture/'interface.bin'),
                '--upgrade-authority',actor['public_key'],*flags], f'abi{abi}-deploy')
            require(deployed['result_code'] == 0 and deployed['typed_interface'] is True, 'typed deploy incomplete')
            sequence += 1
            document = cli(['program','interface','get',program],f'abi{abi}-interface-read')
            require(document['interface'] == (fixture/'interface.bin').read_bytes().hex()
                    and document['abi_version'] == abi, 'published interface differs')
            output = generate(deployed, fixture/'interface.bin', f'abi{abi}-bindings')
            language_consumers(output, abi, 'initial')
            client, call = client_at(output, abi)
            for code, code_hash, interface_digest in [('CODE_HASH_MISMATCH',bytes(32),client.INTERFACE_DIGEST),
                                                      ('STALE_INTERFACE',client.CODE_HASH,bytes(32))]:
                try:
                    client.Client(code_hash, interface_digest)
                except client.BindingRefusal as error:
                    require(error.code == code, 'wrong generated target refusal')
                else:
                    raise ValueError('generated client accepted a tampered target')
                record['cases'].append(f'abi{abi}-generated-{code}')
            invoke(program, abi, call, f'abi{abi}-generated-call'); sequence += 1
            flags = signing() + ['--program-id',program,'--previous-state-root',head(),'--old-hash',digest(fixture/'module.wasm')]
            refused = cli(['program','upgrade',str(fixture/'upgrade.wasm'),'--interface',str(fixture/'narrow.bin'),*flags],
                          f'abi{abi}-incompatible-upgrade')
            require(refused['result_code'] == -3 and refused['outcome']['status'] == 'refused', 'incompatible upgrade admitted')
            sequence += 1
            retained = cli(['program','interface','get',program], f'abi{abi}-refusal-preserves-interface')
            require(retained['interface'] == document['interface'], 'refused upgrade changed interface')
            flags = signing() + ['--program-id',program,'--previous-state-root',head(),'--old-hash',digest(fixture/'module.wasm')]
            upgraded = cli(['program','upgrade',str(fixture/'upgrade.wasm'),'--interface',str(fixture/'upgrade.bin'),*flags],
                           f'abi{abi}-compatible-upgrade')
            require(upgraded['result_code'] == 0, 'compatible upgrade refused'); sequence += 1
            output = generate(upgraded, fixture/'upgrade.bin', f'abi{abi}-upgraded-bindings')
            language_consumers(output, abi, 'upgrade')
            stored.append((abi,program,fixture,upgraded,output))
        processes.stop(); after = processes.start(); ready()
        require(all(before[role] != after[role] for role in ROLES), 'real process restart missing')
        record['restart'] = {'old_pids':before,'new_pids':after}
        for abi, program, fixture, deployed, original in stored:
            document = cli(['program','interface','get',program], f'abi{abi}-restart-interface')
            require(document['interface'] == (fixture/'upgrade.bin').read_bytes().hex(), 'restart changed interface')
            output = generate(deployed, fixture/'upgrade.bin', f'abi{abi}-restart-bindings')
            require(all((output/name).read_bytes() == (original/name).read_bytes() for name in OUTPUTS),
                    'language binding bytes changed across regeneration and restart')
            _, call = client_at(output, abi)
            invoke(program, abi, call, f'abi{abi}-restart-generated-call'); sequence += 1
        fixture = inputs/'abi4'; program = os.urandom(32).hex()
        deployed = cli(['program','deploy',str(fixture/'module.wasm'),'--program-id',program,
                       '--previous-state-root',head(),*signing()], 'untyped-deploy')
        require(deployed['result_code'] == 0 and deployed['typed_interface'] is False, 'omission masqueraded as typed')
        cli(['program','interface','get',program], 'untyped-interface-refused', refusal='interface_absent')
        required = {f'abi{abi}-{case}' for abi in range(1,5) for case in (
            'wrong-hash','unknown-abi','wrong-schema','wrong-capability','deploy','interface-read','bindings',
            'generated-CODE_HASH_MISMATCH','generated-STALE_INTERFACE','generated-call','incompatible-upgrade',
            'refusal-preserves-interface','compatible-upgrade','upgraded-bindings','restart-interface',
            'restart-bindings','restart-generated-call')}
        required |= {f'abi{abi}-{phase}-{language}-{case}' for abi,phase,language in required_consumers
                     for case in ('roundtrip_call','typed_failure','stale_digest','wrong_code_hash','malformed_call')}
        required |= {'untyped-deploy','untyped-interface-refused',
            'interface::conformance_vectors::typed_interfaces_require_the_exact_module_abi_and_capabilities',
            'bindgen::vectors::typed_interfaces_retain_versioned_capabilities_in_every_language'}
        require(len(record['cases']) == len(set(record['cases'])) and set(record['cases']) == required,
                'case inventory incomplete, duplicated or unexpected')
        require(identity() == source, 'source changed during qualification')
        for saved in artifacts.values():
            require(artifact(saved['path']) == saved, 'candidate executable changed')
        processes.alive(); record['exit_code'] = 0; record['qualification_complete'] = True
    except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.SubprocessError) as error:
        record['failure'] = str(error)
    finally:
        try:
            processes.stop()
        except (OSError, RuntimeError, subprocess.SubprocessError) as error:
            record['cleanup_failure'] = str(error)
            record['exit_code'] = 1
            record['qualification_complete'] = False
        record['evidence'] = [artifact(path) for path in sorted(directory.rglob('*')) if path.is_file() and path.stat().st_size]
        save(directory/'result.json', record)
        print('EVIDENCE ' + str(directory/'result.json'))
    return record['exit_code']


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--manifest', default=os.environ.get('PAXEER_X_TYPED_INTERFACES_MANIFEST'))
    args = parser.parse_args()
    def interrupted(_signum, _frame):
        raise RuntimeError('qualification interrupted')
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        require(args.manifest, 'strict provisioned PAXEER_X_TYPED_INTERFACES_MANIFEST required')
        return qualify(args.manifest)
    except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.SubprocessError) as error:
        print('typed interface qualification refused: ' + str(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
