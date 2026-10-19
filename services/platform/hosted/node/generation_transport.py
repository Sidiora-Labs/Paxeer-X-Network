#!/usr/bin/env python3
import argparse
import array
import base64
import contextlib
import fcntl
import hashlib
import hmac
import os
import re
import socket
import stat
import struct
import sys
import threading

from reset_state import (MAX_STATE, Store, StoreError, canonical, decode,
                         open_directory, private_file, read_request)

GENESIS = ('genesis/genesis.manifest', 'genesis/genesis-request.lxgb',
           'genesis/genesis.registration', 'genesis/00000000000000000000.lxs',
           'genesis/paxeer-registration-request.lxrr',
           'genesis/paxeer-deployment-descriptor.lxgd')
IDENTITY_FILES = ('producer.env', 'key.pem', 'genesis.lxs', 'genesis.manifest',
                  'genesis.registration', 'identities.txt', 'node.conf')
PUBLIC_FIELDS = ('LAYERX_NODE_NETWORK_ID', 'LAYERX_NODE_SEQUENCER_ID',
                 'LAYERX_NODE_SEQUENCER_PUBLIC_KEY', 'LAYERX_NODE_REPLICA_ID')
MAX_ARTIFACT = 64 * 1024 * 1024


def environment(raw):
    result = {}
    for line in raw.decode('utf-8').splitlines():
        key, separator, value = line.partition('=')
        if (not separator or not re.fullmatch(r'LAYERX_[A-Z0-9_]+', key)
                or key in result or any(ord(c) < 32 or ord(c) == 127 for c in value)):
            raise StoreError('unsafe_environment')
        result[key] = value
    return result


def artifact(directory, name, maximum=MAX_ARTIFACT, private=False):
    parts = name.split('/')
    if any(part in ('', '.', '..') for part in parts):
        raise StoreError('unsafe_artifact')
    parent = os.dup(directory)
    descriptor = None
    try:
        for part in parts[:-1]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                            dir_fd=parent)
            metadata = os.fstat(child)
            if metadata.st_uid != os.geteuid() or metadata.st_mode & 0o022:
                os.close(child)
                raise StoreError('unsafe_artifact_directory')
            os.close(parent)
            parent = child
        descriptor = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK,
                             dir_fd=parent)
        metadata = os.fstat(descriptor)
        if private:
            private_file(metadata)
        elif (not stat.S_ISREG(metadata.st_mode) or metadata.st_uid != os.geteuid()
              or metadata.st_mode & 0o022 or metadata.st_nlink != 1):
            raise StoreError('unsafe_artifact')
        if metadata.st_size > maximum:
            raise StoreError('artifact_too_large')
        return descriptor
    except BaseException:
        if descriptor is not None:
            os.close(descriptor)
        raise
    finally:
        os.close(parent)


def contents(descriptor, maximum):
    raw = os.pread(descriptor, maximum + 1, 0)
    if len(raw) > maximum:
        raise StoreError('artifact_too_large')
    return raw


class Broker:
    def __init__(self, data_dir, state_dir, run_dir, allowed_uid, allowed_gid):
        self.data_dir = os.path.abspath(data_dir)
        self.state_dir = os.path.abspath(state_dir)
        self.run_dir = os.path.abspath(run_dir)
        self.allowed_uid = allowed_uid
        self.allowed_gid = allowed_gid
        for value in (allowed_uid, allowed_gid):
            if type(value) is not int or not 0 <= value <= 0xffffffff:
                raise StoreError('invalid_peer')

    def request(self, request, peer):
        if peer != (self.allowed_uid, self.allowed_gid):
            raise StoreError('unauthorized_peer')
        common = {'version', 'operation', 'expected_generation'}
        if not isinstance(request, dict) or request.get('operation') not in ('identity', 'core'):
            raise StoreError('invalid_request')
        identity = request['operation'] == 'identity'
        if set(request) != common | ({'slot', 'capability'} if identity else set()):
            raise StoreError('invalid_request')
        if type(request['version']) is not int or request['version'] != 1:
            raise StoreError('unsupported_version')
        expected = request['expected_generation']
        if expected is not None and (type(expected) is not int or not 1 <= expected <= 0x7fffffffffffffff):
            raise StoreError('invalid_generation')
        with contextlib.ExitStack() as stack:
            state_directory = open_directory(self.state_dir)
            stack.callback(os.close, state_directory)
            if identity:
                slot = request['slot']
                if type(slot) is not int or slot not in (1, 2):
                    raise StoreError('unauthorized_slot')
                try:
                    supplied = base64.b64decode(request['capability'], validate=True)
                except (ValueError, TypeError):
                    raise StoreError('unauthorized_slot') from None
                if len(supplied) != 32 or base64.b64encode(supplied).decode() != request['capability']:
                    raise StoreError('unauthorized_slot')
                authorization = os.open('generation-authorizations',
                                        os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                                        dir_fd=state_directory)
                stack.callback(os.close, authorization)
                metadata = os.fstat(authorization)
                if metadata.st_uid != os.geteuid() or stat.S_IMODE(metadata.st_mode) != 0o700:
                    raise StoreError('unsafe_authorization')
                capability = artifact(authorization, 'slot-' + str(slot) + '.cap', 32, private=True)
                stack.callback(os.close, capability)
                stored = contents(capability, 32)
                if len(stored) != 32 or not hmac.compare_digest(supplied, stored):
                    raise StoreError('unauthorized_slot')
            lock = os.open('state.lock', os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK,
                           dir_fd=state_directory)
            stack.callback(os.close, lock)
            private_file(os.fstat(lock))
            fcntl.flock(lock, fcntl.LOCK_SH | fcntl.LOCK_NB)
            state_file = artifact(state_directory, 'state.json', MAX_STATE, private=True)
            stack.callback(os.close, state_file)
            state = decode(contents(state_file, MAX_STATE))
            reader = Store.__new__(Store)
            reader.network = state.get('network') if isinstance(state, dict) else None
            reader._validate(state)
            data = open_directory(self.data_dir)
            stack.callback(os.close, data)
            reset_id = None
            generation = state['generation']
            marker = None
            try:
                marker_file = artifact(data, '.reset-generation.json', 8192)
                stack.callback(os.close, marker_file)
                marker = decode(contents(marker_file, 8192))
            except FileNotFoundError:
                pass
            if marker is None:
                if state['generation'] != 1 or state['baseline_generation'] != 1 or state['active'] is not None:
                    raise StoreError('generation_not_admitted')
                binding = None
            else:
                if not isinstance(marker, dict) or set(marker) != {'version', 'reset_id', 'generation', 'data_dir', 'genesis'}:
                    raise StoreError('invalid_generation_marker')
                reset_id = marker['reset_id']
                record = state['records'].get(reset_id) if isinstance(reset_id, str) else None
                if (record is None or type(marker['version']) is not int or marker['version'] != 1
                        or marker['data_dir'] != self.data_dir or marker['generation'] != record['generation']
                        or record['phase'] not in ('activating', 'completed')
                        or marker['genesis'] != record.get('genesis')
                        or (record['phase'] == 'activating' and state['active'] != reset_id)
                        or (record['phase'] == 'completed' and state['generation'] != record['generation'])
                        or (record['phase'] == 'activating' and record.get('activation_response') != {
                            'state': 'reset', 'reset_id': reset_id, 'generation': record['generation']})):
                    raise StoreError('generation_not_admitted')
                generation = record['generation']
                binding = record['genesis']
                if set(binding) != set(GENESIS):
                    raise StoreError('invalid_genesis')
            if expected is not None and expected != generation:
                raise StoreError('generation_mismatch')
            hashes = {}
            public_descriptors = {}
            proof = os.open('.generation-proof', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                            dir_fd=data)
            stack.callback(os.close, proof)
            metadata = os.fstat(proof)
            if metadata.st_uid != os.geteuid() or stat.S_IMODE(metadata.st_mode) != 0o700:
                raise StoreError('unsafe_generation_proof')
            for name in GENESIS:
                descriptor = artifact(proof, name, private=True)
                stack.callback(os.close, descriptor)
                hashes[name] = hashlib.sha256(contents(descriptor, MAX_ARTIFACT)).hexdigest()
                public_descriptors[name] = descriptor
            if binding is not None and hashes != binding:
                raise StoreError('genesis_mismatch')
            node_environment = artifact(data, 'node.env', 65536)
            stack.callback(os.close, node_environment)
            node = environment(contents(node_environment, 65536))
            public = {key: node[key] for key in PUBLIC_FIELDS}
            if (not public['LAYERX_NODE_NETWORK_ID'].isdecimal()
                    or int(public['LAYERX_NODE_NETWORK_ID']) != state['network']
                    or any(not re.fullmatch(r'[0-9a-f]{64}', public[key]) for key in ('LAYERX_NODE_SEQUENCER_ID', 'LAYERX_NODE_REPLICA_ID'))
                    or not re.fullmatch(r'[0-9a-f]{64}', public['LAYERX_NODE_SEQUENCER_PUBLIC_KEY'])):
                raise StoreError('unsafe_environment')
            files = {}
            if identity:
                root = 'producer-generations/guarantor-' + str(slot) + '/identity/'
                for name in IDENTITY_FILES:
                    descriptor = artifact(data, root + name, 65536 if name in ('producer.env', 'key.pem', 'node.conf', 'identities.txt') else MAX_ARTIFACT)
                    stack.callback(os.close, descriptor)
                    files[name] = descriptor
                for exported, source in (('genesis.lxs', GENESIS[3]), ('genesis.manifest', GENESIS[0]), ('genesis.registration', GENESIS[2])):
                    if hashlib.sha256(contents(files[exported], MAX_ARTIFACT)).hexdigest() != hashes[source]:
                        raise StoreError('identity_genesis_mismatch')
            else:
                descriptor = artifact(data, 'core.env', 65536)
                stack.callback(os.close, descriptor)
                files['core.env'] = descriptor
                files.update(public_descriptors)
            current = open_directory(self.data_dir)
            stack.callback(os.close, current)
            if (os.fstat(current).st_dev, os.fstat(current).st_ino) != (os.fstat(data).st_dev, os.fstat(data).st_ino):
                raise StoreError('generation_changed')
            result = {'version': 1, 'generation': generation, 'reset_id': reset_id,
                      'artifacts': list(files), 'public': public}
            retained = []
            try:
                for descriptor in files.values():
                    retained.append(os.dup(descriptor))
            except BaseException:
                for descriptor in retained:
                    os.close(descriptor)
                raise
            return result, retained

    def handle(self, connection):
        descriptors = []
        try:
            _, uid, gid = struct.unpack('iII', connection.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12))
            result, descriptors = self.request(decode(read_request(connection)), (uid, gid))
            rights = array.array('i', descriptors)
            raw = canonical(result) + b'\n'
            sent = connection.sendmsg([raw], [(socket.SOL_SOCKET, socket.SCM_RIGHTS, rights)])
            if sent < len(raw):
                connection.sendall(raw[sent:])
        except (StoreError, OSError, ValueError, TypeError, KeyError, UnicodeError):
            try:
                connection.sendall(canonical({'error': {'code': 'generation_unavailable'}}) + b'\n')
            except OSError:
                pass
        finally:
            for descriptor in descriptors:
                os.close(descriptor)
            connection.close()

    def serve(self, path):
        if os.path.dirname(os.path.abspath(path)) != self.run_dir:
            raise StoreError('unsafe_socket_path')
        directory = os.open('/', os.O_RDONLY | os.O_DIRECTORY)
        try:
            for part in self.run_dir.split('/')[1:]:
                child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=directory)
                os.close(directory)
                directory = child
            metadata = os.fstat(directory)
            if metadata.st_uid != os.geteuid() or metadata.st_mode & 0o002:
                raise StoreError('unsafe_run_directory')
            with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as listener:
                listener.bind(path)
                os.chown(path, os.geteuid(), self.allowed_gid)
                os.chmod(path, 0o660)
                listener.listen(16)
                slots = threading.BoundedSemaphore(16)
                while True:
                    connection, _ = listener.accept()
                    connection.settimeout(10)
                    if not slots.acquire(blocking=False):
                        connection.close()
                        continue
                    def worker(client):
                        try:
                            self.handle(client)
                        finally:
                            slots.release()
                    threading.Thread(target=worker, args=(connection,), daemon=True).start()
        finally:
            os.close(directory)


def main():
    parser = argparse.ArgumentParser()
    for name in ('data-dir', 'state-dir', 'run-dir', 'socket'):
        parser.add_argument('--' + name, required=True)
    parser.add_argument('--allowed-uid', type=int, required=True)
    parser.add_argument('--allowed-gid', type=int, required=True)
    args = parser.parse_args()
    try:
        Broker(args.data_dir, args.state_dir, args.run_dir, args.allowed_uid, args.allowed_gid).serve(args.socket)
    except (OSError, StoreError):
        print('generation transport unavailable', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
