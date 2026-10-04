import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import socket
import stat
import struct
import subprocess
import sys
import tempfile
import time
from urllib.parse import urlparse

ROOT = Path(__file__).resolve().parents[2]
EVIDENCE = Path(os.environ.get('LAYERX_ADVANCED_TRUST_EVIDENCE',
    '/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task52'))
NATIVE = Path('/root/lx-target/arbiter-prestate/native')
RUST = Path('/root/lx-target/arbiter-prestate/rust')
SOURCE_PATHS = ('Makefile', 'go.mod', 'go.sum', 'custodyproof', 'layerxproof',
    'daemon/layerx-custody-proof', 'src', 'include', 'cmd/layerxd', 'programs',
    'contracts/config/checkpoint-settlement.json', 'tools/bringup/value-loop.sh',
    'tests/bridge/paxeer_x_advanced_trust_credit.py')
TESTS = ('TestLightCurrentTrustRefusesUnauthenticated',
         'TestLightCurrentTrustCommittedFixture',
         'TestLightVectorSkipAcrossValidatorChange')
DEADLINE = 0
COMMANDS = []


def require(condition, message):
    if not condition:
        raise ValueError(message)


def remaining():
    seconds = DEADLINE - time.time()
    require(seconds > 0, 'original task deadline elapsed')
    return seconds


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def pairs(items):
    result = {}
    for key, value in items:
        require(key not in result, 'duplicate manifest field')
        result[key] = value
    return result


def protected(path, limit=2200000):
    path = Path(path)
    require(path.is_absolute() and ROOT not in path.parents
            and not any(item.is_symlink() for item in (path, *path.parents)),
            'absolute private input outside source required')
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
            and info.st_nlink == 1 and not info.st_mode & 0o077
            and 0 < info.st_size <= limit, 'owned protected bounded input required')
    return path


def document(path):
    return json.loads(protected(path).read_text(), object_pairs_hook=pairs)


def write_document(path, value):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'w') as stream:
        json.dump(value, stream, indent=2)
        stream.write('\n')


def git(*arguments):
    return subprocess.check_output(['git', *arguments], cwd=ROOT).decode().strip()


def source():
    require(not git('status', '--porcelain', '--untracked-files=all'),
            'complete frozen clean candidate required')
    tree = subprocess.check_output(['git', 'ls-tree', '-r', '-z', 'HEAD', '--',
                                    *SOURCE_PATHS], cwd=ROOT)
    return {'revision': git('rev-parse', 'HEAD'), 'tree': git('rev-parse', 'HEAD^{tree}'),
            'source_binding': hashlib.sha256(tree).hexdigest()}


def run(command, label, env=None):
    command = [str(item) for item in command]
    log = EVIDENCE / (label + '.log')
    start = time.monotonic()
    with log.open('wb') as output:
        process = subprocess.Popen(command, cwd=ROOT, env=env, stdout=output,
                                   stderr=subprocess.STDOUT, start_new_session=True)
        try:
            code = process.wait(timeout=remaining())
        except BaseException:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
            raise
    COMMANDS.append({'command': command, 'exit_code': code, 'log': str(log),
                     'elapsed_seconds': time.monotonic() - start})
    print(json.dumps(COMMANDS[-1]), flush=True)
    if code:
        raise subprocess.CalledProcessError(code, command)
    return log.read_text(errors='replace')


def artifact(path):
    path = Path(path)
    info = path.lstat()
    require(path.is_absolute() and not path.is_symlink() and stat.S_ISREG(info.st_mode)
            and info.st_uid == os.geteuid() and not info.st_mode & 0o022
            and os.access(path, os.X_OK), 'protected actual executable required')
    with path.open('rb') as stream:
        require(stream.read(4) == b'\x7fELF', 'real ELF artifact required')
    return {'path': str(path), 'sha256': digest(path)}


def build():
    before = source()
    env = os.environ.copy()
    env.update(PATH='/usr/local/go/bin:/root/.cargo/bin:' + env.get('PATH', ''),
               CARGO_BUILD_JOBS='4', CARGO_NET_OFFLINE='true', GOMAXPROCS='4')
    cli, tests = EVIDENCE / 'layerx-custody-proof', EVIDENCE / 'custodyproof.test'
    go = '/usr/local/go/bin/go'
    compilers = {name: artifact(Path(path).resolve()) for name, path in (
        ('go', go), ('cargo', '/root/.cargo/bin/cargo'),
        ('cc', shutil.which('cc', path=env['PATH'])))}
    run([go, 'build', '-mod=readonly', '-buildvcs=true', '-p', '4', '-o', cli,
         './daemon/layerx-custody-proof'], 'build-cli', env)
    run([go, 'test', '-mod=readonly', '-buildvcs=true', '-p', '4', '-c', '-o', tests,
         './custodyproof'], 'build-custodyproof', env)
    run(['flock', '/root/lx-cargo/native-build.lock', 'make', '-j4',
         'BUILD_DIR=' + str(NATIVE), 'LXP_REVISION=' + before['revision'],
         'PROGRAMS_TARGET_DIR=' + str(RUST),
         'PROGRAMS_RUNTIME_LIB=' + str(RUST / 'debug/liblayerx_programs_sandbox.a'),
         'PROGRAMS_CARGO=/root/.cargo/bin/cargo', 'layerxd'], 'build-native', env)
    require(source() == before, 'source changed during task build')
    write_document(EVIDENCE / 'build.json', {'version': 1, 'source': before,
        'exit_code': 0, 'commands': COMMANDS, 'compilers': compilers,
        'artifacts': {'cli': artifact(cli), 'tests': artifact(tests),
                      'layerxd': artifact(NATIVE / 'bin/layerxd')}})


def fixture(path):
    value = document(path)
    require(set(value) == {'profile', 'snapshot', 'credit', 'current_header_hash',
            'sequencer_id', 'sequencer_key', 'first_batch', 'last_batch'},
            'closed genuine advanced trust fixture required')
    for name in ('profile', 'snapshot', 'credit'):
        protected(value[name])
    for name in ('current_header_hash', 'sequencer_id', 'sequencer_key'):
        require(isinstance(value[name], str) and re.fullmatch('[0-9a-f]{64}', value[name])
                and bytes.fromhex(value[name]) != bytes(32), 'nonzero genuine authority pin required')
    require(all(type(value[name]) is int and 0 <= value[name] < 2**64
                for name in ('first_batch', 'last_batch'))
            and value['first_batch'] <= value['last_batch'], 'authorized batch bounds required')
    return value


def go_cases(binary, path, label, all_cases=True):
    env = os.environ.copy()
    env['LAYERX_ADVANCED_TRUST_FIXTURE'] = str(path)
    names = TESTS if all_cases else ('TestLightCurrentTrustCommittedFixture',)
    output = run([binary, '-test.v', '-test.count=1', '-test.run=^(' + '|'.join(names) + ')$'], label, env)
    passed = re.findall(r'^--- PASS: (\S+) ', output, re.M)
    require(sorted(passed) == sorted(names) and '\nPASS\n' in '\n' + output
            and '--- SKIP:' not in output and '--- FAIL:' not in output,
            'mandatory genuine trust cases failed, skipped or absent')


def envelope(tag, correlation, payload=b''):
    return struct.pack('>HHHQI', 1, 5, tag, correlation, len(payload)) + payload + bytes(4)


def receive(connection):
    def exact(length):
        result = b''
        while len(result) < length:
            data = connection.recv(length - len(result))
            require(data, 'truncated actual LNI frame')
            result += data
        return result
    length = int.from_bytes(exact(4), 'big')
    require(22 <= length <= 2200000, 'bounded actual LNI frame required')
    body = exact(length)
    major, minor, tag, correlation, count = struct.unpack('>HHHQI', body[:18])
    require(major == 1 and minor >= 5 and count <= len(body) - 22,
            'actual LNI envelope framing')
    payload = body[18:18 + count]
    proof_count = int.from_bytes(body[18 + count:22 + count], 'big')
    require(proof_count == len(body) - 22 - count, 'actual LNI proof framing')
    return tag, correlation, payload, body[22 + count:]


def exchange(runtime, body, expected, correlation):
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
        connection.settimeout(min(30, remaining()))
        original_uid, original_gid = os.geteuid(), os.getegid()
        try:
            os.setegid(4020)
            os.seteuid(4021)
            connection.connect(runtime['socket'])
        finally:
            os.seteuid(original_uid)
            os.setegid(original_gid)
        uid = struct.unpack('3i', connection.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12))[1]
        require(uid == runtime['daemon_uid'], 'actual daemon peer identity mismatch')
        hello = envelope(1, 0)
        connection.sendall(len(hello).to_bytes(4, 'big') + hello)
        require(receive(connection)[:2] == (2, 0), 'actual native handshake refused')
        connection.sendall(len(body).to_bytes(4, 'big') + body)
        response = receive(connection)
        require(response[:2] == (expected, correlation), 'actual native request refused')
        return response


def native_cases(build_record, before, fixture_path):
    name = os.environ.get('LAYERX_ADVANCED_TRUST_RUNTIME_FIXTURE')
    if not name:
        print('Missing genuine isolated native runtime: LAYERX_ADVANCED_TRUST_RUNTIME_FIXTURE', file=sys.stderr)
        raise SystemExit(78)
    runtime = document(name)
    require(set(runtime) == {'version', 'namespace_pid', 'daemon_uid', 'daemon_args',
            'durable_root', 'socket', 'rpc', 'deposit_id', 'owner_key', 'height',
            'signed_submit', 'before_read_request', 'after_read_request', 'after_fixture'},
            'closed genuine isolated durable native fixture required')
    require(runtime['version'] == 1 and os.geteuid() == 0
            and type(runtime['namespace_pid']) is int and runtime['namespace_pid'] > 1
            and type(runtime['daemon_uid']) is int and runtime['daemon_uid'] == 0,
            'genuine isolated namespace and daemon identity required')
    namespace = Path('/proc') / str(runtime['namespace_pid']) / 'ns/net'
    require(namespace.stat().st_ino != Path('/proc/self/ns/net').stat().st_ino,
            'production network namespace forbidden')
    durable = Path(runtime['durable_root'])
    require(durable.is_absolute() and durable.is_dir() and ROOT not in durable.parents
            and not any(path.is_symlink() for path in (durable, *durable.parents))
            and str(durable) in runtime['daemon_args'], 'explicit actual durable store required')
    require(isinstance(runtime['daemon_args'], list)
            and all(isinstance(arg, str) and arg and '\x00' not in arg for arg in runtime['daemon_args'])
            and Path(runtime['socket']).is_absolute(), 'actual daemon arguments and socket required')
    require(not Path(runtime['socket']).exists(), 'fixture must have no existing daemon socket')
    rpc = urlparse(runtime['rpc'])
    require(rpc.scheme in ('http', 'https') and rpc.hostname in ('127.0.0.1', '::1', 'localhost'),
            'owner provisioned isolated loopback Comet RPC required')
    after = fixture(runtime['after_fixture'])
    require(Path(after['profile']).read_bytes() == Path(before['profile']).read_bytes()
            and after['current_header_hash'] != before['current_header_hash'],
            'immutable profile and genuinely advanced committed head required')
    namespace_args = ['nsenter', '--target', str(runtime['namespace_pid']), '--net', '--']
    command = [*namespace_args, build_record['artifacts']['layerxd']['path'], *runtime['daemon_args']]
    node = None
    output = None
    def start():
        nonlocal node, output
        output = (EVIDENCE / ('native-restart.log' if node is not None else 'native-start.log')).open('ab')
        node = subprocess.Popen(command, cwd=ROOT, stdout=output, stderr=subprocess.STDOUT,
                                start_new_session=True)
        until = time.time() + min(30, remaining())
        while time.time() < until:
            require(node.poll() is None, 'actual native daemon startup failed')
            if Path(runtime['socket']).exists():
                return
            time.sleep(0.1)
        raise TimeoutError('actual native socket startup deadline')
    def stop():
        if node is not None and node.poll() is None:
            os.killpg(node.pid, signal.SIGTERM)
            try:
                node.wait(timeout=min(15, remaining()))
            except BaseException:
                os.killpg(node.pid, signal.SIGKILL)
                node.wait()
                raise
        if output:
            output.close()
    def committed(request_name, selected):
        payload = protected(runtime[request_name]).read_bytes()
        require(len(payload) == 107 and payload[:3] == b'\x00\x01\x06',
                'genuine committed-state request required')
        response = exchange(runtime, envelope(7, 1, payload), 8, 1)
        require(response[2] == b'LXTS1' and response[3] == Path(selected['snapshot']).read_bytes(),
                'production committed snapshot differs from authenticated fixture')
    try:
        start()
        committed('before_read_request', before)
        with tempfile.TemporaryDirectory(prefix='task5.2-', dir='/tmp') as directory:
            work = Path(directory)
            os.chown(work, 4021, 4020)
            work.chmod(0o700)
            cli = work / 'layerx-custody-proof'
            profile = work / 'custody.profile'
            cli_row = build_record['artifacts']['cli']
            shutil.copyfile(cli_row['path'], cli)
            require(digest(cli) == cli_row['sha256'], 'copied actual CLI artifact differs')
            profile_bytes = protected(before['profile']).read_bytes()
            profile.write_bytes(profile_bytes)
            require(profile.read_bytes() == profile_bytes, 'copied approved profile differs')
            for path, mode in ((cli, 0o500), (profile, 0o400)):
                os.chown(path, 4021, 4020)
                path.chmod(mode)
            credit = work / 'generated.credit'
            args = [*namespace_args, 'setpriv', '--reuid=4021', '--regid=4020', '--clear-groups',
                cli, 'light-credit', '--rpc', runtime['rpc'], '--profile', profile,
                '--kernel-socket', runtime['socket'], '--sequencer-id', before['sequencer_id'],
                '--sequencer-key', before['sequencer_key'],
                '--first-authorized-batch', str(before['first_batch']),
                '--last-authorized-batch', str(before['last_batch']),
                '--deposit-id', runtime['deposit_id'], '--owner-key', runtime['owner_key'],
                '--height', str(runtime['height']), '--output', credit]
            run(args, 'actual-cli-current-trust')
            credit_bytes = credit.read_bytes()
            require(credit_bytes == Path(before['credit']).read_bytes(),
                    'actual CLI credit differs from approved genuine deposit')
            require(digest(cli) == cli_row['sha256'] and profile.read_bytes() == profile_bytes,
                    'actual CLI inputs changed during generation')
        submit = protected(runtime['signed_submit']).read_bytes()
        require(len(submit) >= 22 and struct.unpack('>HHH', submit[:6]) == (1, 5, 3)
                and submit.count(credit_bytes) == 1, 'actual preauthorized signed credit submission required')
        correlation = int.from_bytes(submit[6:14], 'big')
        require(correlation != 0, 'signed submission correlation required')
        response = exchange(runtime, submit, 4, correlation)
        require(len(response[3]) == 32, 'real production acknowledged credit identity required')
        committed('after_read_request', after)
        stop()
        start()
        committed('after_read_request', after)
        go_cases(build_record['artifacts']['tests']['path'], runtime['after_fixture'], 'recovered-committed-trust', False)
    finally:
        stop()


def verify():
    record = document(EVIDENCE / 'build.json')
    require(set(record) == {'version', 'source', 'exit_code', 'commands', 'compilers', 'artifacts'}
            and record['version'] == 1 and record['exit_code'] == 0
            and record['source'] == source() and set(record['artifacts']) == {'cli', 'tests', 'layerxd'},
            'successful current-source prebuilt task artifacts required')
    require(set(record['compilers']) == {'go', 'cargo', 'cc'}, 'actual compiler provenance required')
    for row in record['compilers'].values():
        require(set(row) == {'path', 'sha256'} and artifact(row['path']) == row,
                'compiler identity changed since task build')
    for row in record['artifacts'].values():
        require(set(row) == {'path', 'sha256'} and artifact(row['path']) == row,
                'task executable changed since build')
    name = os.environ.get('LAYERX_ADVANCED_TRUST_FIXTURE')
    if not name:
        print('Missing genuine committed current-trust fixture: LAYERX_ADVANCED_TRUST_FIXTURE', file=sys.stderr)
        raise SystemExit(78)
    before = fixture(name)
    go_cases(record['artifacts']['tests']['path'], name, 'current-trust-cases')
    native_cases(record, before, name)
    require(source() == record['source'], 'candidate changed during task qualification')


def main():
    global DEADLINE
    parser = argparse.ArgumentParser()
    parser.add_argument('--build', action='store_true')
    parser.add_argument('--deadline-epoch', type=int, default=int(os.environ.get(
        'PAXEER_X_TASK_DEADLINE_EPOCH', str(int(time.time()) + 1800))))
    args = parser.parse_args()
    DEADLINE = args.deadline_epoch
    require(EVIDENCE.is_absolute() and ROOT not in EVIDENCE.parents
            and not any(path.is_symlink() for path in (EVIDENCE, *EVIDENCE.parents)),
            'private evidence directory outside checkout required')
    EVIDENCE.mkdir(parents=True, exist_ok=True, mode=0o700)
    require(EVIDENCE.stat().st_uid == os.geteuid() and not EVIDENCE.stat().st_mode & 0o077,
            'owned private evidence directory required')
    code = 1
    try:
        build() if args.build else verify()
        code = 0
    except SystemExit as error:
        code = int(error.code)
    except subprocess.CalledProcessError as error:
        code = error.returncode
    except Exception as error:
        print(str(error), file=sys.stderr)
    finally:
        row = {'revision': git('rev-parse', 'HEAD'), 'command': [sys.executable, *sys.argv],
               'exit_code': code, 'log_path': str(EVIDENCE), 'commands': COMMANDS}
        write_document(EVIDENCE / ('build-outcome.json' if args.build else 'verify-outcome.json'), row)
        print(json.dumps(row), flush=True)
    return code


if __name__ == '__main__':
    sys.exit(main())
