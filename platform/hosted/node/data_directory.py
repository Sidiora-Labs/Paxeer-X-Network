#!/usr/bin/env python3
import os
import ctypes
import hashlib
import json
import re
import shutil
import stat
import sys

GENERATION_MARKER = '.reset-generation.json'
RESET_ID = re.compile(r'[0-9a-f]{32}\Z')
GENESIS_FILES = frozenset((
    'genesis/genesis.manifest', 'genesis/genesis-request.lxgb',
    'genesis/genesis.registration', 'genesis/00000000000000000000.lxs',
    'genesis/paxeer-registration-request.lxrr',
    'genesis/paxeer-deployment-descriptor.lxgd'))


def _identity(reset_id, generation=None):
    if not isinstance(reset_id, str) or not RESET_ID.fullmatch(reset_id):
        raise ValueError('invalid generation identity')
    if generation is not None and (type(generation) is not int
                                   or not 1 <= generation <= 0x7fffffffffffffff):
        raise ValueError('invalid generation number')


def _private_directory(descriptor):
    metadata = os.fstat(descriptor)
    if metadata.st_uid != os.geteuid() or stat.S_IMODE(metadata.st_mode) != 0o700:
        raise ValueError('generation directory must be owned and mode 0700')


def prepare_stage(data_dir, state_dir, reset_id):
    _identity(reset_id)
    data, data_fd = open_directory(data_dir)
    state, state_fd = open_directory(state_dir)
    descriptors = [data_fd, state_fd]
    try:
        _private_directory(state_fd)
        if state == data or state.startswith(data + os.sep) or data.startswith(state + os.sep):
            raise ValueError('generation state overlaps live data')
        if os.fstat(data_fd).st_dev != os.fstat(state_fd).st_dev:
            raise ValueError('generation staging must share the live data device')
        parent = state_fd
        for name in ('generations', reset_id):
            try:
                os.mkdir(name, 0o700, dir_fd=parent)
                os.fsync(parent)
            except FileExistsError:
                pass
            child = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                            dir_fd=parent)
            descriptors.append(child)
            _private_directory(child)
            parent = child
        for name in ('data', 'run'):
            try:
                os.mkdir(name, 0o700, dir_fd=parent)
                os.fsync(parent)
            except FileExistsError:
                pass
            child = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                            dir_fd=parent)
            descriptors.append(child)
            if name == 'run':
                metadata = os.fstat(child)
                if (metadata.st_uid != os.geteuid()
                        or stat.S_IMODE(metadata.st_mode) not in (0o700, 0o750)):
                    raise ValueError('staged run directory has unsafe ownership or mode')
            else:
                _private_directory(child)
        return os.path.join(state, 'generations', reset_id)
    finally:
        for descriptor in reversed(descriptors):
            os.close(descriptor)


def _canonical_genesis(root_fd, genesis):
    if (not isinstance(genesis, dict) or set(genesis) != GENESIS_FILES
            or any(not isinstance(value, str) or not re.fullmatch(r'[0-9a-f]{64}', value)
                   for value in genesis.values())):
        raise ValueError('invalid canonical generation binding')
    directory = os.open('genesis', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                        dir_fd=root_fd)
    try:
        for path, expected in genesis.items():
            descriptor = os.open(path.split('/')[1], os.O_RDONLY | os.O_NOFOLLOW,
                                 dir_fd=directory)
            try:
                metadata = os.fstat(descriptor)
                if (not stat.S_ISREG(metadata.st_mode) or metadata.st_uid != os.geteuid()
                        or metadata.st_nlink != 1):
                    raise ValueError('invalid canonical generation artifact')
                digest = hashlib.sha256()
                while True:
                    block = os.read(descriptor, 1048576)
                    if not block:
                        break
                    digest.update(block)
                if digest.hexdigest() != expected:
                    raise ValueError('canonical generation artifact changed')
            finally:
                os.close(descriptor)
    finally:
        os.close(directory)


def _sync_tree(descriptor, count=None):
    count = [0] if count is None else count
    for name in os.listdir(descriptor):
        count[0] += 1
        if count[0] > 4096:
            raise ValueError('generation output exceeds file bound')
        metadata = os.stat(name, dir_fd=descriptor, follow_symlinks=False)
        if metadata.st_uid != os.geteuid():
            raise ValueError('generation output owner differs')
        if stat.S_ISDIR(metadata.st_mode):
            child = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                            dir_fd=descriptor)
            try:
                _sync_tree(child, count)
            finally:
                os.close(child)
        elif stat.S_ISREG(metadata.st_mode) and metadata.st_nlink == 1:
            child = os.open(name, os.O_RDONLY | os.O_NOFOLLOW, dir_fd=descriptor)
            try:
                os.fsync(child)
            finally:
                os.close(child)
        else:
            raise ValueError('generation output is not an owned ordinary file')
    os.fsync(descriptor)


def _marker(descriptor):
    try:
        marker = os.open(GENERATION_MARKER, os.O_RDONLY | os.O_NOFOLLOW,
                         dir_fd=descriptor)
    except FileNotFoundError:
        return None
    try:
        metadata = os.fstat(marker)
        if (not stat.S_ISREG(metadata.st_mode) or metadata.st_uid != os.geteuid()
                or metadata.st_nlink != 1 or stat.S_IMODE(metadata.st_mode) != 0o600
                or not 1 <= metadata.st_size <= 4096):
            raise ValueError('invalid generation marker')
        value = json.loads(os.read(marker, 4097))
        if (not isinstance(value, dict) or set(value) != {
                'version', 'reset_id', 'generation', 'data_dir', 'genesis'}
                or type(value['version']) is not int or value['version'] != 1
                or not isinstance(value['data_dir'], str)
                or value['data_dir'] != os.path.abspath(value['data_dir'])
                or not isinstance(value['genesis'], dict)
                or set(value['genesis']) != GENESIS_FILES
                or any(not isinstance(digest, str) or not re.fullmatch(r'[0-9a-f]{64}', digest)
                       for digest in value['genesis'].values())):
            raise ValueError('invalid generation marker')
        _identity(value['reset_id'], value['generation'])
        return value
    finally:
        os.close(marker)


def seal_generation(data_dir, staged_data, reset_id, generation, genesis):
    _identity(reset_id, generation)
    data, live_fd = open_directory(data_dir)
    staged, descriptor = open_directory(staged_data)
    try:
        _private_directory(descriptor)
        if (data == staged or os.fstat(live_fd).st_dev != os.fstat(descriptor).st_dev
                or staged.startswith(data + os.sep) or data.startswith(staged + os.sep)):
            raise ValueError('invalid staged generation location')
        _canonical_genesis(descriptor, genesis)
        expected = {'version': 1, 'reset_id': reset_id, 'generation': generation,
                    'data_dir': data, 'genesis': genesis}
        marker = _marker(descriptor)
        if marker is not None:
            if marker != expected:
                raise ValueError('generation marker conflicts')
        else:
            encoded = json.dumps(expected, sort_keys=True, separators=(',', ':')).encode()
            temporary = GENERATION_MARKER + '.prepared'
            marker_fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                                0o600, dir_fd=descriptor)
            with os.fdopen(marker_fd, 'wb') as handle:
                handle.write(encoded)
                handle.flush()
                os.fsync(handle.fileno())
            os.rename(temporary, GENERATION_MARKER, src_dir_fd=descriptor, dst_dir_fd=descriptor)
        _sync_tree(descriptor)
        stage_root, stage_fd = open_directory(os.path.dirname(staged))
        try:
            _sync_tree(stage_fd)
        finally:
            os.close(stage_fd)
        return expected
    finally:
        os.close(descriptor)
        os.close(live_fd)


def activate_generation(data_dir, staged_data, reset_id, generation, genesis):
    _identity(reset_id, generation)
    data, live_fd = open_directory(data_dir)
    stage_fd = None
    parents = []
    try:
        expected = {'version': 1, 'reset_id': reset_id, 'generation': generation,
                    'data_dir': data, 'genesis': genesis}
        live_marker = _marker(live_fd)
        if live_marker == expected:
            _canonical_genesis(live_fd, genesis)
            return 'active'
        if live_marker is not None and live_marker['generation'] >= generation:
            raise ValueError('activation cannot replace a later or conflicting generation')
        staged, stage_fd = open_directory(staged_data)
        _private_directory(stage_fd)
        if (data == staged or staged.startswith(data + os.sep) or data.startswith(staged + os.sep)
                or os.fstat(live_fd).st_dev != os.fstat(stage_fd).st_dev
                or _marker(stage_fd) != expected):
            raise ValueError('staged generation does not match activation authority')
        _canonical_genesis(stage_fd, genesis)
        for path in (data, staged):
            _, parent = open_directory(os.path.dirname(path))
            parents.append(parent)
            metadata = os.stat(os.path.basename(path), dir_fd=parent, follow_symlinks=False)
            source = live_fd if len(parents) == 1 else stage_fd
            held = os.fstat(source)
            if (metadata.st_dev, metadata.st_ino) != (held.st_dev, held.st_ino):
                raise ValueError('generation path changed during activation')
            os.fsync(parent)
        libc = ctypes.CDLL(None, use_errno=True)
        exchange = getattr(libc, 'renameat2', None)
        if exchange is None:
            raise ValueError('atomic generation exchange is unavailable')
        exchange.argtypes = (ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p,
                             ctypes.c_uint)
        exchange.restype = ctypes.c_int
        if exchange(parents[0], os.fsencode(os.path.basename(data)), parents[1],
                    os.fsencode(os.path.basename(staged)), 2) != 0:
            error = ctypes.get_errno()
            raise OSError(error, os.strerror(error))
        return 'activated'
    finally:
        for descriptor in parents:
            os.close(descriptor)
        if stage_fd is not None:
            os.close(stage_fd)
        os.close(live_fd)


def rollback_stage(data_dir, state_dir, reset_id):
    _identity(reset_id)
    data, live_fd = open_directory(data_dir)
    state, state_fd = open_directory(state_dir)
    descriptors = [live_fd, state_fd]
    try:
        _private_directory(state_fd)
        if state == data or state.startswith(data + os.sep) or data.startswith(state + os.sep):
            raise ValueError('generation state overlaps live data')
        if os.fstat(live_fd).st_dev != os.fstat(state_fd).st_dev:
            raise ValueError('generation state device differs')
        live_marker = _marker(live_fd)
        if live_marker is not None and live_marker['reset_id'] == reset_id:
            raise ValueError('an active generation cannot be rolled back')
        try:
            generations = os.open('generations', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                                  dir_fd=state_fd)
        except FileNotFoundError:
            return 'absent'
        descriptors.append(generations)
        _private_directory(generations)
        try:
            stage = os.open(reset_id, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                            dir_fd=generations)
        except FileNotFoundError:
            return 'absent'
        descriptors.append(stage)
        _private_directory(stage)
        staged_data = os.open('data', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                              dir_fd=stage)
        descriptors.append(staged_data)
        _private_directory(staged_data)
        if _marker(staged_data) is not None:
            raise ValueError('a sealed generation requires durable owner reconciliation')
        if not shutil.rmtree.avoids_symlink_attacks:
            raise ValueError('safe generation rollback is unavailable')
        shutil.rmtree(reset_id, dir_fd=generations)
        os.fsync(generations)
        return 'rolled_back'
    finally:
        for descriptor in reversed(descriptors):
            os.close(descriptor)


def open_directory(path, create=False):
    absolute = os.path.abspath(path)
    if absolute == os.path.sep:
        raise ValueError("data directory must not be the filesystem root")
    descriptor = os.open(os.path.sep, os.O_PATH | os.O_DIRECTORY)
    try:
        components = absolute.split(os.path.sep)[1:]
        for index, component in enumerate(components):
            if create:
                try:
                    os.mkdir(component, mode=0o700, dir_fd=descriptor)
                except FileExistsError:
                    pass
            access = os.O_RDONLY if index == len(components) - 1 else os.O_PATH
            child = os.open(component, access | os.O_DIRECTORY | os.O_NOFOLLOW,
                            dir_fd=descriptor)
            os.close(descriptor)
            descriptor = child
        metadata = os.fstat(descriptor)
        if metadata.st_uid != os.geteuid():
            raise ValueError("data directory must be owned by the bootstrap user")
        return absolute, descriptor
    except BaseException:
        os.close(descriptor)
        raise


def main():
    if len(sys.argv) == 5 and sys.argv[1] == 'prepare-stage':
        print(prepare_stage(*sys.argv[2:]))
        return
    if len(sys.argv) == 5 and sys.argv[1] == 'rollback-stage':
        print(rollback_stage(*sys.argv[2:]))
        return
    if len(sys.argv) == 7 and sys.argv[1] in ('seal-generation', 'activate-generation'):
        operation = seal_generation if sys.argv[1] == 'seal-generation' else activate_generation
        result = operation(sys.argv[2], sys.argv[3], sys.argv[4], int(sys.argv[5]),
                           json.loads(sys.argv[6]))
        print(json.dumps(result, sort_keys=True, separators=(',', ':')))
        return
    if len(sys.argv) != 3 or sys.argv[1] not in ("prepare", "clear", "inspect"):
        raise ValueError("usage: data_directory.py prepare|clear|inspect PATH")
    absolute, descriptor = open_directory(sys.argv[2], sys.argv[1] == "prepare")
    try:
        if sys.argv[1] == "prepare":
            os.fchmod(descriptor, 0o700)
            print(absolute)
        elif sys.argv[1] == 'inspect':
            print(absolute)
        else:
            if not shutil.rmtree.avoids_symlink_attacks:
                raise ValueError("safe directory removal is unavailable")
            for entry in os.listdir(descriptor):
                metadata = os.stat(entry, dir_fd=descriptor, follow_symlinks=False)
                if stat.S_ISDIR(metadata.st_mode):
                    shutil.rmtree(entry, dir_fd=descriptor)
                else:
                    os.unlink(entry, dir_fd=descriptor)
            os.fsync(descriptor)
    finally:
        os.close(descriptor)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError) as error:
        print(f"bootstrap data directory refused: {error}", file=sys.stderr)
        sys.exit(1)
