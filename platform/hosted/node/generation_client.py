#!/usr/bin/env python3
import argparse
import array
import base64
import fcntl
import hashlib
import os
import re
import signal
import socket
import stat
import struct
import subprocess
import sys
import tempfile
import time

from generation_transport import GENESIS, IDENTITY_FILES, PUBLIC_FIELDS, environment
from reset_state import StoreError, canonical, decode, open_directory

PRODUCER_FIELDS = {'LAYERX_GUARANTOR_ID', 'LAYERX_NODE_NETWORK_ID',
                   'LAYERX_NODE_ASSET_ID', 'LAYERX_NODE_SEQUENCER_ID',
                   'LAYERX_NODE_SEQUENCER_PUBLIC_KEY', 'LAYERX_NODE_FIRST_BATCH',
                   'LAYERX_NODE_LAST_BATCH'}
CORE_FIELDS = {'LAYERX_CORE_SEQUENCER_ID', 'LAYERX_CORE_TREASURY_ASSET'}
IDENTITY_PATHS = {'producer.env': 'LAYERX_GUARANTOR_PRODUCER_ENV_FILE',
                  'key.pem': 'LAYERX_GUARANTOR_KEY_FILE',
                  'genesis.lxs': 'LAYERX_NODE_SNAPSHOT',
                  'genesis.manifest': 'LAYERX_NODE_GENESIS_MANIFEST',
                  'genesis.registration': 'LAYERX_NODE_GENESIS_REGISTRATION',
                  'identities.txt': 'LAYERX_NODE_IDENTITIES',
                  'node.conf': 'LAYERX_GUARANTOR_NODE_CONFIG'}


def close_descriptors(descriptors):
    for descriptor in descriptors:
        os.close(descriptor)


def receive(socket_path, operation, slot=None, capability=None, expected_generation=None, broker_uid=4020, broker_gid=4020):
    metadata = os.lstat(socket_path)
    if (not stat.S_ISSOCK(metadata.st_mode) or stat.S_IMODE(metadata.st_mode) != 0o660
            or metadata.st_uid != broker_uid or metadata.st_gid != broker_gid
            or os.getegid() != broker_gid):
        raise StoreError('unsafe_generation_socket')
    request = {'version': 1, 'operation': operation, 'expected_generation': expected_generation}
    if operation == 'identity':
        request.update(slot=slot, capability=base64.b64encode(capability).decode())
    descriptors = []
    try:
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
            connection.settimeout(10)
            connection.connect(socket_path)
            _, uid, gid = struct.unpack('iII', connection.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12))
            if uid != broker_uid or gid != broker_gid:
                raise StoreError('unsafe_generation_peer')
            current = os.lstat(socket_path)
            if (current.st_dev, current.st_ino) != (metadata.st_dev, metadata.st_ino):
                raise StoreError('generation_socket_changed')
            connection.sendall(canonical(request) + b'\n')
            raw = bytearray()
            deadline = time.monotonic() + 10
            while b'\n' not in raw:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise StoreError('generation_timeout')
                connection.settimeout(remaining)
                chunk, ancillary, flags, _ = connection.recvmsg(8193 - len(raw), socket.CMSG_SPACE(16 * array.array('i').itemsize), socket.MSG_CMSG_CLOEXEC)
                for level, kind, value in ancillary:
                    if level != socket.SOL_SOCKET or kind != socket.SCM_RIGHTS:
                        raise StoreError('invalid_generation_descriptors')
                    received = array.array('i')
                    received.frombytes(value[:len(value) - len(value) % received.itemsize])
                    descriptors.extend(received)
                if flags & (socket.MSG_CTRUNC | socket.MSG_TRUNC) or not chunk:
                    raise StoreError('incomplete_generation')
                raw.extend(chunk)
                if len(raw) > 8192 or len(descriptors) > 16:
                    raise StoreError('generation_too_large')
            if not raw.endswith(b'\n') or raw.count(b'\n') != 1:
                raise StoreError('invalid_generation')
            result = decode(bytes(raw[:-1]))
            names = IDENTITY_FILES if operation == 'identity' else ('core.env',) + GENESIS
            if (not isinstance(result, dict) or set(result) != {'version', 'generation', 'reset_id', 'artifacts', 'public'}
                    or type(result['version']) is not int or result['version'] != 1
                    or type(result['generation']) is not int or not 1 <= result['generation'] <= 0x7fffffffffffffff
                    or (result['reset_id'] is not None and not isinstance(result['reset_id'], str))
                    or (result['reset_id'] is not None and not re.fullmatch(r'[0-9a-f]{32}', result['reset_id']))
                    or result['artifacts'] != list(names) or len(descriptors) != len(names)
                    or not isinstance(result['public'], dict) or set(result['public']) != set(PUBLIC_FIELDS)):
                raise StoreError('generation_unavailable')
            if expected_generation is not None and result['generation'] != expected_generation:
                raise StoreError('generation_mismatch')
            public = result['public']
            if (any(not isinstance(value, str) for value in public.values())
                    or not public[PUBLIC_FIELDS[0]].isdecimal()
                    or not 0 <= int(public[PUBLIC_FIELDS[0]]) <= 0xffffffff
                    or any(not re.fullmatch(r'[0-9a-f]{64}', public[key]) for key in ('LAYERX_NODE_SEQUENCER_ID', 'LAYERX_NODE_REPLICA_ID'))
                    or not re.fullmatch(r'[0-9a-f]{64}', public['LAYERX_NODE_SEQUENCER_PUBLIC_KEY'])):
                raise StoreError('unsafe_environment')
            for descriptor in descriptors:
                info = os.fstat(descriptor)
                if (not stat.S_ISREG(info.st_mode) or info.st_uid != uid
                        or info.st_mode & 0o022 or info.st_nlink != 1
                        or info.st_size > 64 * 1024 * 1024
                        or fcntl.fcntl(descriptor, fcntl.F_GETFL) & os.O_ACCMODE != os.O_RDONLY):
                    raise StoreError('unsafe_generation_descriptor')
            return result, descriptors
    except BaseException:
        close_descriptors(descriptors)
        raise


def child_environment(result, descriptors, operation):
    files = dict(zip(result['artifacts'], descriptors))
    filename = 'producer.env' if operation == 'identity' else 'core.env'
    raw = os.pread(files[filename], 65537, 0)
    if len(raw) > 65536:
        raise StoreError('unsafe_environment')
    adopted = environment(raw)
    public = result['public']
    if operation == 'identity':
        if set(adopted) != PRODUCER_FIELDS:
            raise StoreError('unsafe_environment')
        for key in ('LAYERX_GUARANTOR_ID', 'LAYERX_NODE_ASSET_ID', 'LAYERX_NODE_SEQUENCER_ID'):
            if not re.fullmatch(r'[0-9a-f]{64}', adopted[key]):
                raise StoreError('unsafe_environment')
        if (not re.fullmatch(r'[0-9a-f]{64}', adopted['LAYERX_NODE_SEQUENCER_PUBLIC_KEY'])
                or adopted['LAYERX_NODE_FIRST_BATCH'] != '1'
                or adopted['LAYERX_NODE_LAST_BATCH'] != '18446744073709551615'
                or any(adopted[key] != public[key] for key in ('LAYERX_NODE_NETWORK_ID', 'LAYERX_NODE_SEQUENCER_ID', 'LAYERX_NODE_SEQUENCER_PUBLIC_KEY'))):
            raise StoreError('unsafe_environment')
        adopted.update({key: '/proc/self/fd/' + str(files[name]) for name, key in IDENTITY_PATHS.items()})
    else:
        if set(adopted) not in (CORE_FIELDS, CORE_FIELDS | {'LAYERX_CORE_TREASURY_SIGNER_SOCKET'}):
            raise StoreError('unsafe_environment')
        if any(not re.fullmatch(r'[0-9a-f]{64}', adopted[key]) for key in CORE_FIELDS):
            raise StoreError('unsafe_environment')
        if adopted['LAYERX_CORE_SEQUENCER_ID'] != public['LAYERX_NODE_SEQUENCER_ID']:
            raise StoreError('unsafe_environment')
        if 'LAYERX_CORE_TREASURY_SIGNER_SOCKET' in adopted and (not adopted['LAYERX_CORE_TREASURY_SIGNER_SOCKET'].startswith('/')
                or any(c.isspace() for c in adopted['LAYERX_CORE_TREASURY_SIGNER_SOCKET'])):
            raise StoreError('unsafe_environment')
        adopted.update(public)
        adopted.update(LAYERX_CORE_NETWORK_ID=public['LAYERX_NODE_NETWORK_ID'],
                       LAYERX_AUTHORITY_PROTOCOL_NETWORK_ID=public['LAYERX_NODE_NETWORK_ID'],
                       LAYERX_AUTHORITY_SEQUENCER_ID=public['LAYERX_NODE_SEQUENCER_ID'],
                       LAYERX_AUTHORITY_SEQUENCER_PUBLIC_KEY=public['LAYERX_NODE_SEQUENCER_PUBLIC_KEY'],
                       LAYERX_AUTHORITY_REPLICA_ID=public['LAYERX_NODE_REPLICA_ID'])
        adopted['LAYERX_CORE_ENV_FILE'] = '/proc/self/fd/' + str(files['core.env'])
        adopted['LAYERX_NODE_GENESIS_MANIFEST'] = '/proc/self/fd/' + str(files[GENESIS[0]])
        adopted['LAYERX_NODE_GENESIS_REGISTRATION'] = '/proc/self/fd/' + str(files[GENESIS[2]])
        adopted['LAYERX_NODE_SNAPSHOT'] = '/proc/self/fd/' + str(files[GENESIS[3]])
    result_env = os.environ.copy()
    result_env.update(adopted)
    result_env['LAYERX_GENERATION_FD_MODE'] = '1'
    result_env['LAYERX_GENERATION_NUMBER'] = str(result['generation'])
    return result_env


def capability_file(path, owner):
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        metadata = os.fstat(descriptor)
        if (not stat.S_ISREG(metadata.st_mode) or stat.S_IMODE(metadata.st_mode) != 0o440
                or metadata.st_uid != owner or metadata.st_gid != os.getegid()
                or metadata.st_nlink != 1 or metadata.st_size != 32):
            raise StoreError('unsafe_capability')
        return os.pread(descriptor, 33, 0)
    finally:
        os.close(descriptor)


def materialize(result, descriptors, operation, adopted):
    parent = None
    if operation == 'identity':
        state = os.environ.get('LAYERX_GUARANTOR_STATE_DIR')
        if not state or not os.path.isabs(state):
            raise StoreError('missing_private_generation_state')
        parent = os.path.join(state, '.generation-views')
        directory = open_directory(parent, create=True)
        os.close(directory)
    view = tempfile.TemporaryDirectory(prefix='generation-' + str(result['generation']) + '-', dir=parent)
    directory = open_directory(view.name)
    paths = {}
    try:
        for name, source in zip(result['artifacts'], descriptors):
            filename = name.rsplit('/', 1)[-1]
            target = os.open(filename, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=directory)
            expected = hashlib.sha256()
            offset = 0
            try:
                while True:
                    raw = os.pread(source, 1024 * 1024, offset)
                    if not raw:
                        break
                    offset += len(raw)
                    if offset > 64 * 1024 * 1024:
                        raise StoreError('generation_too_large')
                    expected.update(raw)
                    remaining = memoryview(raw)
                    while remaining:
                        count = os.write(target, remaining)
                        if count <= 0:
                            raise StoreError('generation_write_unavailable')
                        remaining = remaining[count:]
                os.fsync(target)
            finally:
                os.close(target)
            target = os.open(filename, os.O_RDONLY | os.O_NOFOLLOW, dir_fd=directory)
            try:
                observed = hashlib.sha256()
                source_again = hashlib.sha256()
                offset = 0
                while True:
                    raw = os.pread(target, 1024 * 1024, offset)
                    retained = os.pread(source, 1024 * 1024, offset)
                    if raw != retained:
                        raise StoreError('generation_content_changed')
                    if not raw:
                        break
                    offset += len(raw)
                    observed.update(raw)
                    source_again.update(retained)
                if observed.digest() != expected.digest() or source_again.digest() != expected.digest():
                    raise StoreError('generation_content_changed')
            finally:
                os.close(target)
            paths[name] = os.path.join(view.name, filename)
        os.fsync(directory)
        if operation == 'identity':
            adopted.update({key: paths[name] for name, key in IDENTITY_PATHS.items()})
        else:
            adopted['LAYERX_CORE_ENV_FILE'] = paths['core.env']
            adopted['LAYERX_NODE_GENESIS_MANIFEST'] = paths[GENESIS[0]]
            adopted['LAYERX_NODE_GENESIS_REGISTRATION'] = paths[GENESIS[2]]
            adopted['LAYERX_NODE_SNAPSHOT'] = paths[GENESIS[3]]
        return view, adopted
    except BaseException:
        view.cleanup()
        raise
    finally:
        os.close(directory)


def supervise(args, command):
    stopping = False
    child = None
    held = []
    active_view = None
    capability = None
    def terminate_child():
        nonlocal child
        if child is not None:
            if child.poll() is None:
                os.killpg(child.pid, signal.SIGTERM)
                try:
                    child.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    os.killpg(child.pid, signal.SIGKILL)
                    child.wait()
            child = None
    def stop(_number, _frame):
        nonlocal stopping
        stopping = True
    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    current = None
    try:
        while not stopping:
            received = []
            try:
                if args.operation == 'identity' and capability is None:
                    capability = capability_file(args.capability_file, args.broker_uid)
                result, received = receive(args.socket, args.operation, args.slot, capability,
                                           broker_uid=args.broker_uid)
                token = (result['generation'], result['reset_id'])
                env = child_environment(result, received, args.operation)
                if current is not None and token[0] < current[0]:
                    raise StoreError('generation_regressed')
                if child is None or token != current:
                    next_view, env = materialize(result, received, args.operation, env)
                    terminate_child()
                    if active_view is not None:
                        active_view.cleanup()
                    active_view = next_view
                    close_descriptors(held)
                    held = received
                    received = []
                    child = subprocess.Popen(command, env=env, pass_fds=held, start_new_session=True)
                    current = token
                elif child.poll() is not None:
                    return child.returncode
            except (StoreError, OSError, ValueError, KeyError, UnicodeError):
                terminate_child()
                if active_view is not None:
                    active_view.cleanup()
                    active_view = None
                close_descriptors(held)
                held = []
            finally:
                close_descriptors(received)
            deadline = time.monotonic() + args.watch_seconds
            while not stopping and time.monotonic() < deadline:
                time.sleep(min(0.1, max(0, deadline - time.monotonic())))
        return 0
    finally:
        terminate_child()
        if active_view is not None:
            active_view.cleanup()
        close_descriptors(held)


def main():
    separator = sys.argv.index('--') if '--' in sys.argv else len(sys.argv)
    parser = argparse.ArgumentParser()
    parser.add_argument('operation', choices=('identity', 'core'))
    parser.add_argument('--socket', required=True)
    parser.add_argument('--slot', type=int)
    parser.add_argument('--capability-file')
    parser.add_argument('--watch-seconds', type=float, default=1)
    parser.add_argument('--broker-uid', type=int, default=4020)
    args = parser.parse_args(sys.argv[1:separator])
    command = sys.argv[separator + 1:]
    if (not command or not 0.1 <= args.watch_seconds <= 30
            or not 0 <= args.broker_uid <= 0xffffffff
            or (args.operation == 'identity' and (args.slot not in (1, 2) or not args.capability_file))
            or (args.operation == 'core' and (args.slot is not None or args.capability_file is not None))):
        parser.error('invalid generation consumer')
    return supervise(args, command)


if __name__ == '__main__':
    sys.exit(main())
