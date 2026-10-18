"""Immutable source snapshots and bounded, owned process execution."""
from __future__ import annotations
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import signal
import stat
import subprocess
import tarfile
import tempfile
import threading
import time
import shutil
from .source_policy import ignored

class WorkspaceError(ValueError):
    pass

_LOG_LIMIT = 1024 * 1024

def _safe(name):
    p = PurePosixPath(name)
    if not name or '\\' in name or name.startswith('/') or any(x in ('', '.', '..') for x in name.split('/')):
        raise WorkspaceError(f'Unsafe archive path: {name!r}')
    return p

def _excluded(parts, extra):
    relative = '/'.join(parts)
    return ignored(relative) or any(relative == x or relative.startswith(x + '/') for x in extra)

def _root(path):
    p = Path(path).absolute()
    for item in (p, *p.parents):
        if item.is_symlink():
            raise WorkspaceError('Symlink path component rejected')
    return p

def create_archive(source, destination, exclude=()):
    source, destination = _root(source), _root(destination)
    if not source.is_dir() or destination.exists():
        raise WorkspaceError('Source must exist and destination must be new')
    if destination == source or source in destination.parents:
        raise WorkspaceError('Archive must be outside source')
    extra = tuple(str(_safe(str(x))) for x in exclude)
    entries = []
    for base, dirs, files in os.walk(source, followlinks=False):
        for name in sorted(dirs + files):
            path = Path(base) / name
            rel = path.relative_to(source).as_posix()
            # Ignore generated state and secrets before touching their metadata.
            if _excluded(PurePosixPath(rel).parts, extra):
                continue
            st = path.lstat()
            # All included source entries must be regular files or directories.
            if stat.S_ISLNK(st.st_mode) or (stat.S_ISREG(st.st_mode) and st.st_nlink != 1) or not (stat.S_ISDIR(st.st_mode) or stat.S_ISREG(st.st_mode)):
                raise WorkspaceError(f'Unsafe source entry: {rel}')
            entries.append((rel, path, st))
        dirs[:] = sorted(d for d in dirs if not _excluded((Path(base) / d).relative_to(source).parts, extra))
    destination.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary = tempfile.mkstemp(dir=destination.parent, prefix='.archive-')
    try:
        with os.fdopen(fd, 'wb') as output, tarfile.open(fileobj=output, mode='w', format=tarfile.USTAR_FORMAT) as archive:
            for rel, path, original in sorted(entries):
                info = tarfile.TarInfo(rel)
                info.uid = info.gid = info.mtime = 0
                info.uname = info.gname = ''
                info.mode = 0o755 if stat.S_ISDIR(original.st_mode) or original.st_mode & 0o111 else 0o644
                if stat.S_ISDIR(original.st_mode):
                    info.type = tarfile.DIRTYPE
                    archive.addfile(info)
                else:
                    filefd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
                    with os.fdopen(filefd, 'rb') as stream:
                        current = os.fstat(stream.fileno())
                        if current.st_nlink != 1 or (current.st_dev, current.st_ino, current.st_size, current.st_mtime_ns) != (original.st_dev, original.st_ino, original.st_size, original.st_mtime_ns):
                            raise WorkspaceError('Source changed while archiving')
                        info.size = current.st_size
                        archive.addfile(info, stream)
                        after = os.fstat(stream.fileno())
                        if (after.st_size, after.st_mtime_ns) != (current.st_size, current.st_mtime_ns):
                            raise WorkspaceError('Source changed while archiving')
        digest = hashlib.sha256(Path(temporary).read_bytes()).hexdigest()
        os.chmod(temporary, 0o444)
        # link creates the final name exclusively; no overwrite race.
        os.link(temporary, destination)
        return digest
    except (OSError, tarfile.TarError) as exc:
        raise WorkspaceError(str(exc)) from exc
    finally:
        Path(temporary).unlink(missing_ok=True)

def create_workspace(archive, destination, *, max_archive_bytes=2*1024**3, max_expanded_bytes=8*1024**3, max_members=100000, max_member_bytes=512*1024**2):
    archive, destination = _root(archive), _root(destination)
    if destination.exists() or not archive.is_file():
        raise WorkspaceError('Archive required; workspace destination must be new')
    if archive.stat().st_nlink != 1:
        raise WorkspaceError('Hardlinked archive rejected')
    limits = (max_archive_bytes, max_expanded_bytes, max_members, max_member_bytes)
    if any(not isinstance(x, int) or isinstance(x, bool) or x <= 0 for x in limits):
        raise WorkspaceError('Positive integer extraction limits required')
    if archive.stat().st_size > max_archive_bytes:
        raise WorkspaceError('Archive exceeds byte limit')
    hasher = hashlib.sha256()
    with archive.open('rb') as stream:
        while chunk := stream.read(1024*1024):
            hasher.update(chunk)
    digest = hasher.hexdigest()
    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = Path(tempfile.mkdtemp(dir=destination.parent, prefix='.workspace-'))
    try:
        with tarfile.open(archive, mode='r:') as tf:
            members = []
            expanded = 0
            seen = set()
            for member in tf:
                members.append(member)
                expanded += member.size
                if len(members) > max_members or member.size > max_member_bytes or expanded > max_expanded_bytes:
                    raise WorkspaceError('Archive extraction limits exceeded')
                _safe(member.name)
                if member.name in seen or not (member.isdir() or member.isfile()):
                    raise WorkspaceError('Duplicate, linked, or special archive member')
                seen.add(member.name)
                if member.size < 0:
                    raise WorkspaceError('Invalid member size')
            # Validate every member before any extraction; all paths remain owned.
            for member in members:
                target = temporary / member.name
                if member.isdir():
                    target.mkdir(parents=True, exist_ok=True)
                else:
                    target.parent.mkdir(parents=True, exist_ok=True)
                    with tf.extractfile(member) as stream, target.open('xb') as out:
                        shutil.copyfileobj(stream, out)
                    target.chmod(0o755 if member.mode & 0o111 else 0o644)
        if destination.exists():
            raise WorkspaceError('Workspace destination appeared during creation')
        os.rename(temporary, destination)
        return digest
    except (OSError, tarfile.TarError) as exc:
        raise WorkspaceError(str(exc)) from exc
    finally:
        if temporary.exists():
            shutil.rmtree(temporary)

def namespace(binding, task, lane, attempt):
    required = ('baseline', 'candidate', 'interface', 'layout')
    if not isinstance(binding, dict) or any(not isinstance(binding.get(k), str) or len(binding[k]) != 64 or any(c not in '0123456789abcdef' for c in binding[k]) for k in required):
        raise WorkspaceError('Four SHA256 bindings required')
    if any(not isinstance(v, str) or not v or any(c not in 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-' for c in v) or v in ('.', '..') for v in (task, lane, attempt)):
        raise WorkspaceError('Invalid namespace identity')
    payload = {k: binding[k] for k in required}
    payload.update(task=task, lane=lane, attempt=attempt)
    return hashlib.sha256(json.dumps(payload, sort_keys=True, separators=(',', ':')).encode()).hexdigest()

def run_process(command, cwd, timeout, env=None):
    if not isinstance(command, (list, tuple)) or not command or any(not isinstance(x, str) or '\x00' in x for x in command) or not isinstance(timeout, (int, float)) or isinstance(timeout, bool) or not 0 < timeout < float('inf'):
        raise WorkspaceError('Command array and finite positive timeout required')
    cwd = _root(cwd)
    if not cwd.is_dir():
        raise WorkspaceError('Working directory required')
    started = time.monotonic()
    buffers = [bytearray(), bytearray()]
    threads = []
    proc = None
    previous = {}
    timed_out = False
    def interrupted(signum, frame):
        raise SystemExit(128 + signum)
    def drain(stream, buf):
        try:
            while chunk := stream.read(65536):
                remaining = _LOG_LIMIT - len(buf)
                if remaining > 0:
                    buf.extend(chunk[:remaining])
        finally:
            stream.close()
    try:
        # Install before spawn so termination cannot bypass owned-group cleanup.
        if threading.current_thread() is threading.main_thread():
            for signum in (signal.SIGTERM, signal.SIGINT):
                previous[signum] = signal.signal(signum, interrupted)
        try:
            proc = subprocess.Popen(command, cwd=cwd, env=env, start_new_session=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        except OSError as exc:
            raise WorkspaceError(str(exc)) from exc
        threads = [threading.Thread(target=drain, args=(stream, buf), daemon=True) for stream, buf in zip((proc.stdout, proc.stderr), buffers)]
        for thread in threads:
            thread.start()
        try:
            proc.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            timed_out = True
    finally:
        # Repeated termination signals must not interrupt the bounded cleanup.
        for signum in previous:
            signal.signal(signum, signal.SIG_IGN)
        try:
            if proc is not None:
                try:
                    os.killpg(proc.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                proc.wait(timeout=5)
                for thread in threads:
                    if thread.ident is not None:
                        thread.join(timeout=2)
        finally:
            for signum, handler in previous.items():
                signal.signal(signum, handler)
    cleanup = not any(t.is_alive() for t in threads)
    return dict(exit_code=proc.returncode, timed_out=timed_out, stdout=buffers[0].decode('utf-8', 'replace'), stderr=buffers[1].decode('utf-8', 'replace'), duration=time.monotonic()-started, pid=proc.pid, cleanup=cleanup)
