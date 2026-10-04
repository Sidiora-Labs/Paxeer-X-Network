#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import stat
import subprocess
import sys
import tempfile
import time

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
TEST = 'real_native_send_profile_owner_hold_reopen_decisions_and_exact_resume'
SOURCES = (
    'agent/crates/layerx-agent-api/src/identity.rs',
    'agent/crates/layerx-agentd/src/agent_rpc_wire.rs',
    'agent/crates/layerx-agentd/src/agent_rpc.rs',
    'agent/crates/layerx-sdk/src/native_effect.rs',
    'agent/crates/layerx-sdk/src/agent_envelope.rs',
    'agent/crates/layerx-agentd/src/agent_rpc_dispatch.rs',
    'agent/crates/layerx-agentd/src/agent_rpc_adapters.rs',
    'agent/crates/layerx-agentd/src/human.rs',
    'agent/crates/layerx-agentd/src/human_runtime.rs',
    'agent/crates/layerx-agentd/src/capability/binding.rs',
    'agent/crates/layerx-agentd/src/capability/effects.rs',
    'agent/crates/layerx-agentd/src/capability/timed.rs',
    'agent/crates/layerx-agentd/src/session.rs',
    'agent/crates/layerx-agentd/src/approval/native_effect.rs',
    'agent/crates/layerx-agentd/tests/native_send_profile.rs',
    'tools/qualification/paxeer-x/native_send_profile.py',
)
REFUSALS = {'cross-profile', 'replay', 'expiry', 'principal', 'session', 'canonical-digest'}
MARKERS = {'native-send: exact-original-resume', 'native-send: grant', 'native-send: reject',
           'native-send: distinct-domain-held-reopen'} | {'native-send: refusal-' + case for case in REFUSALS}


def require(value, message):
    if not value:
        raise RuntimeError(message)


def protected(path, maximum, uid):
    path = Path(path)
    require('.env' not in path.parts and not path.name.startswith('.env'),
            'environment credential files are outside this gate')
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= maximum
            and info.st_mode & 0o077 == 0 and info.st_uid == uid,
            'genuine input must be regular, protected, bounded and owned by the admitted peer')
    return path


def digest(path):
    with Path(path).open('rb') as stream:
        result = hashlib.sha256()
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            result.update(block)
        return result.hexdigest()


def inside(path, directory):
    return Path(path).resolve().is_relative_to(directory)


def main():
    require(not sys.argv[1:], 'unsupported native Send gate arguments')
    fixture_path = os.environ.get('PAXEER_X_NATIVE_SEND_PROFILE_FIXTURE')
    require(fixture_path,
            'missing genuine protected PAXEER_X_NATIVE_SEND_PROFILE_FIXTURE: issuer-signed Send purpose, '
            'mTLS owner and kernel peer, isolated daemon PID/argv/artifact/state, original signed resume '
            'and independent grant/reject/refusal inputs are required')
    fixture_path = Path(fixture_path)
    info = fixture_path.lstat()
    require(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= 131072
            and info.st_mode & 0o077 == 0, 'native Send fixture must be protected and bounded')
    fixture = json.loads(fixture_path.read_text())
    require(fixture.get('schema') == 'paxeer-x.native-send-profile.v1'
            and fixture.get('isolated_real_owner') is True, 'genuine disposable owner fixture required')
    uid, gid = fixture.get('uid'), fixture.get('gid')
    require(type(uid) is int and uid > 0 and type(gid) is int and gid > 0
            and info.st_uid == uid and os.getuid() in (0, uid), 'genuine non-root kernel peer required')
    hashes = fixture.get('source_hashes', {})
    require(all(hashes.get(source) == digest(ROOT / source) for source in SOURCES),
            'genuine fixture must bind all final native Send implementation sources')
    target = Path(os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/agent')).resolve()
    candidates = [path for path in (target / 'debug/deps').glob('native_send_profile-*')
                  if path.is_file() and os.access(path, os.X_OK) and not path.name.endswith('.d')]
    require(len(candidates) == 1, 'exact current prebuilt native Send corpus required; gate never builds')
    binary = candidates[0]
    require(digest(binary) == fixture.get('test_binary_sha256'), 'genuine fixture must bind exact corpus artifact')
    require(binary.stat().st_mtime_ns >= max((ROOT / source).stat().st_mtime_ns for source in SOURCES),
            'prebuilt native Send corpus predates final source')
    daemon = fixture.get('disposable_daemon', {})
    require(daemon.get('allow_restart') is True, 'explicitly disposable daemon restart authority required')
    pid = daemon.get('pid')
    require(type(pid) is int and pid > 1 and pid != os.getpid(), 'actual disposable daemon PID required')
    state = Path(daemon['state_directory']).resolve()
    require(state.is_dir() and state != Path('/') and state != ROOT and state.is_relative_to(Path('/tmp')),
            'daemon qualification state must be an existing isolated directory under /tmp')
    state_info = state.stat()
    require(state_info.st_uid == uid and state_info.st_mode & 0o077 == 0, 'private owner-owned disposable state required')
    daemon_binary = Path(daemon['binary']).resolve()
    require(daemon_binary == target / 'debug/layerx-agentd'
            and digest(daemon_binary) == daemon.get('binary_sha256'), 'exact final Agent daemon artifact required')
    require(daemon_binary.stat().st_mtime_ns >= max((ROOT / source).stat().st_mtime_ns
            for source in SOURCES if source.endswith('.rs')), 'daemon artifact predates final native Send source')
    argv = daemon.get('argv')
    require(type(argv) is list and 2 <= len(argv) <= 128 and argv[0] == str(daemon_binary)
            and all(type(arg) is str and 0 < len(arg) <= 4096 and '\x00' not in arg for arg in argv),
            'exact bounded disposable daemon launch argv required')
    require(any(inside(arg, state) for arg in argv[1:] if arg.startswith('/'))
            and all(inside(arg, state) and not any(part.startswith('.env') for part in Path(arg).parts)
                    for arg in argv[1:] if arg.startswith('/')),
            'daemon launch paths must refer only to isolated qualification state')
    require(Path(f'/proc/{pid}/exe').resolve() == daemon_binary
            and Path(f'/proc/{pid}').stat().st_uid == uid,
            'restart PID must be the genuine isolated owner daemon')
    require(Path(f'/proc/{pid}/cmdline').read_bytes() == b''.join(arg.encode() + b'\x00' for arg in argv),
            'running disposable daemon argv does not match owner admission')
    for key in ('retained_store', 'human_socket', 'lni_socket'):
        require(inside(fixture[key], state), 'daemon socket and state paths must remain isolated')
    restart_log = Path(daemon['restart_log'])
    require(inside(restart_log, state) and not restart_log.is_symlink(), 'private disposable restart log required')
    if restart_log.exists():
        require(restart_log.stat().st_uid == uid and restart_log.stat().st_mode & 0o077 == 0,
                'existing restart log must be private and owner-owned')
    else:
        fd = os.open(restart_log, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        os.close(fd)
        if os.getuid() == 0:
            os.chown(restart_log, uid, gid)
    for key in ('rpc_client_cert_pem', 'rpc_client_key_pem', 'ca_der_file'):
        protected(fixture[key], 65536, uid)
    protected(fixture['generic_prepare_file'], 4194304, uid)
    cases = fixture.get('cases')
    require(type(cases) is list and len(cases) == 2 and [case.get('decision') for case in cases] == ['grant', 'reject'],
            'independent genuine grant and reject cases required')
    for case in cases:
        for key in ('prepare_file', 'held_submit_file', 'original_resume_file'):
            protected(case[key], 4194304, uid)
        protected(case['purpose_canonical_file'], 4096, uid)
    negatives = fixture.get('refusal_cases')
    require(type(negatives) is list and len(negatives) == len(REFUSALS)
            and {case.get('case') for case in negatives} == REFUSALS, 'complete genuine refusal matrix required')
    for case in negatives:
        protected(case['request_file'], 4194304, uid)
        protected(case['original_prepare_file'], 4194304, uid)
        if case['case'] == 'expiry':
            protected(case['purpose_canonical_file'], 4096, uid)
        if case['case'] in ('principal', 'session'):
            protected(case['foreign_credential_file'], 16384, uid)
        require(type(case.get('expected_status')) is int and case['expected_status'] in (400, 403, 409)
                and type(case.get('expected_reason')) is str and case['expected_reason'],
                'exact genuine typed refusal outcome required')
    def peer_identity():
        os.setgroups([])
        os.setgid(gid)
        os.setuid(uid)
        os.umask(0o077)
    deadline = min(time.time() + 480, float(os.environ.get('PAXEER_X_TASK_DEADLINE_UNIX', time.time() + 480)))
    timeout = int(deadline - time.time())
    require(timeout > 0, 'original task deadline has expired')
    with tempfile.TemporaryDirectory(prefix='layerx-native-send-', dir='/tmp') as directory:
        directory = Path(directory)
        copied = directory / 'native_send_profile'
        shutil.copyfile(binary, copied)
        require(digest(copied) == fixture['test_binary_sha256'], 'copied corpus artifact differs')
        if os.getuid() == 0:
            os.chown(directory, uid, gid)
            os.chown(copied, uid, gid)
        os.chmod(directory, 0o700)
        os.chmod(copied, 0o500)
        environment = {'PAXEER_X_NATIVE_SEND_PROFILE_FIXTURE': str(fixture_path), 'PATH': '/usr/bin:/bin'}
        process = subprocess.Popen([str(copied), '--exact', TEST, '--nocapture'], cwd=directory,
                                   env=environment, preexec_fn=peer_identity if os.getuid() == 0 else None,
                                   start_new_session=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            stdout, stderr = process.communicate(timeout=timeout)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.communicate()
            raise RuntimeError('native Send corpus exceeded original bounded deadline')
        finally:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
    sys.stdout.buffer.write(stdout)
    sys.stderr.buffer.write(stderr)
    require(process.returncode == 0 and b'running 1 test' in stdout and b'1 passed; 0 failed' in stdout,
            'real native Send corpus failed or did not execute')
    require(MARKERS <= set(stdout.decode('utf-8', errors='strict').splitlines()),
            'real native Send corpus omitted a mandatory positive or refusal case')
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except RuntimeError as error:
        print('native Send profile refused: ' + str(error), file=sys.stderr)
        sys.exit(1)
    except (OSError, ValueError, KeyError, TypeError, subprocess.TimeoutExpired):
        print('native Send profile refused: genuine protected input or process unavailable', file=sys.stderr)
        sys.exit(1)
