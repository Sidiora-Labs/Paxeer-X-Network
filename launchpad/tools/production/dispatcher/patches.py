"""Content addressed, exclusively owned patches and isolated verified staging."""
import fnmatch
import hashlib
import json
import os
import re
from pathlib import Path, PurePosixPath
import shutil
import stat
import subprocess
from .source_policy import ignored

class PatchError(ValueError):
    pass

def _hash(data):
    return hashlib.sha256(data).hexdigest()

def _json(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':')).encode()

def _path(value):
    if not isinstance(value, str) or not value or '\\' in value or '\x00' in value or ':' in value:
        raise PatchError('invalid relative path')
    parts = value.split('/')
    if value.startswith('/') or any(p in ('', '.', '..') for p in parts):
        raise PatchError('unsafe relative path')
    return value

def _ancestors(path):
    for ancestor in [Path(path), *Path(path).parents]:
        if ancestor.is_symlink():
            raise PatchError('symlink ancestor forbidden')

def _reserved(path):
    name = PurePosixPath(path).name
    # Reviewed lane-owned leaf artifacts; these never bypass ownership checks.
    owned_manifest = path in {'client/packages/sdk/package.json', 'client/apps/mobile/package.json'}
    owned_generated = path == 'client/packages/sdk/src/types.gen.ts'
    lane_handoff = re.fullmatch(r'spec/production-system/handoffs/L(?:0[1-9]|[1-4][0-9]|50)/.+', path) is not None
    return (path == 'spec' or (path.startswith('spec/') and not lane_handoff) or
            path.startswith('shared/interfaces/') or path.startswith('client/packages/production-contracts/') or
            (name == 'package.json' and not owned_manifest) or
            name in { 'pnpm-lock.yaml', 'package-lock.json', 'yarn.lock', 'go.mod', 'go.sum', 'Cargo.lock', 'Cargo.toml', 'requirements.txt', 'uv.lock', 'pyproject.toml', 'go.work', 'go.work.sum', 'pnpm-workspace.yaml', 'sqlc.yaml', 'AGENTS.md', 'GOTCHA.kvx'} or
            name.endswith('.gen.go') or (name.endswith('.gen.ts') and not owned_generated) or
            '/migrations/' in '/' + path + '/' or '/internal/db/sqlcdb/' in '/' + path + '/' or
            path.endswith('/internal/app/app.go') or (name == 'main.go' and '/cmd/' in '/' + path))

def _owned(path, patterns):
    return any(fnmatch.fnmatchcase(path, pattern) and (pattern.endswith('/**') or len(path.split('/')) == len(pattern.split('/'))) for pattern in patterns)

def _tree(root):
    root = Path(root)
    _ancestors(root)
    if root.is_symlink() or not root.is_dir():
        raise PatchError('root must be a real directory')
    result = {}
    for current, dirs, files in os.walk(root, followlinks=False):
        dirs[:] = [name for name in dirs if not ignored((Path(current) / name).relative_to(root).as_posix())]
        files = [name for name in files if not ignored((Path(current) / name).relative_to(root).as_posix())]
        for name in dirs + files:
            p = Path(current) / name
            info = p.lstat()
            if stat.S_ISLNK(info.st_mode) or not (stat.S_ISDIR(info.st_mode) or stat.S_ISREG(info.st_mode)):
                raise PatchError('links and special files forbidden')
            if stat.S_ISREG(info.st_mode):
                if info.st_nlink != 1:
                    raise PatchError('hardlinks forbidden')
                rel = _path(p.relative_to(root).as_posix())
                result[rel] = {'sha256': _hash(p.read_bytes()), 'mode': stat.S_IMODE(info.st_mode)}
    return result

def _digest(tree):
    return _hash(_json(tree))

def build_patch(baseline, candidate, owned, output):
    """Return a manifest; all changed paths must belong to explicit ownership."""
    if not isinstance(owned, list) or not owned:
        raise PatchError('ownership required')
    for pattern in owned:
        _path(pattern)
        if pattern in ('*', '**') or _reserved(pattern):
            raise PatchError('broad or reserved ownership')
    before, after = _tree(baseline), _tree(candidate)
    entries = []
    for path in sorted(set(before) | set(after)):
        if before.get(path) == after.get(path):
            continue
        if _reserved(path) or not _owned(path, owned):
            raise PatchError('changed path outside ownership: ' + path)
        entries.append({'path': path, 'before': before.get(path), 'after': after.get(path)})
    if not entries:
        raise PatchError('empty patch')
    output = Path(output)
    _ancestors(output)
    if output.exists() or output.is_symlink():
        raise PatchError('output already exists')
    for root in (Path(baseline).resolve(), Path(candidate).resolve()):
        if output.resolve().is_relative_to(root) or root.is_relative_to(output.resolve()):
            raise PatchError('patch output overlaps source')
    output.mkdir(parents=True)
    try:
        (output / 'blobs').mkdir()
        for entry in entries:
            if entry['after']:
                data = (Path(candidate) / entry['path']).read_bytes()
                if _hash(data) != entry['after']['sha256']:
                    raise PatchError('candidate changed during collection')
                (output / 'blobs' / _hash(data)).write_bytes(data)
        if _tree(candidate) != after or _tree(baseline) != before:
            raise PatchError('source changed during collection')
        manifest = {'version': 1, 'baseline': _digest(before), 'owned': sorted(owned), 'entries': entries}
        manifest['patch_digest'] = _hash(_json(manifest))
        (output / 'manifest.json').write_bytes(_json(manifest))
        return {**manifest, 'manifest_path': str(output / 'manifest.json')}
    except Exception:
        shutil.rmtree(output)
        raise

def _load(patch):
    if isinstance(patch, dict):
        patch = patch.get('manifest_path')
    path = Path(patch)
    _ancestors(path)
    if path.is_dir():
        path /= 'manifest.json'
    if path.is_symlink() or not path.is_file() or path.stat().st_nlink != 1:
        raise PatchError('unsafe manifest')
    _tree(path.parent)
    data = json.loads(path.read_bytes())
    expected = data.pop('patch_digest', None)
    if expected != _hash(_json(data)) or data.get('version') != 1:
        raise PatchError('manifest digest mismatch')
    data['patch_digest'] = expected
    return path.parent, data

def stage_patches(baseline, patches, stage, verify=None, ownership=None):
    """Build deterministic merged tree, verify it, and return qualified evidence."""
    if not patches or verify is None or not isinstance(ownership, dict):
        raise PatchError('patches and verifier required')
    before = _tree(baseline)
    stage = Path(stage)
    _ancestors(stage)
    if stage.exists() or stage.is_symlink():
        raise PatchError('stage already exists')
    baseline = Path(baseline).resolve()
    if stage.resolve().is_relative_to(baseline) or baseline.is_relative_to(stage.resolve()):
        raise PatchError('stage and baseline overlap')
    changes, identities, claimed_patterns = {}, [], []
    for patch in patches:
        directory, manifest = _load(patch)
        if manifest['baseline'] != _digest(before):
            raise PatchError('baseline mismatch')
        identities.append(manifest['patch_digest'])
        patterns = ownership.get(manifest['patch_digest'])
        if not isinstance(patterns, list) or not patterns or sorted(patterns) != manifest.get('owned'):
            raise PatchError('missing or mismatched trusted ownership')
        lane_prefixes = []
        for pattern in patterns:
            _path(pattern)
            if pattern in ('*', '**') or _reserved(pattern):
                raise PatchError('invalid trusted ownership')
            prefix = pattern.split('*', 1)[0].split('?', 1)[0].split('[', 1)[0]
            for previous in claimed_patterns:
                if prefix.startswith(previous) or previous.startswith(prefix):
                    raise PatchError('overlapping ownership patterns')
            lane_prefixes.append(prefix)
        claimed_patterns.extend(lane_prefixes)
        for entry in manifest.get('entries', []):
            path = _path(entry['path'])
            if path in changes or ignored(path) or _reserved(path) or not _owned(path, patterns):
                raise PatchError('overlap or forbidden path: ' + path)
            if entry['before'] != before.get(path):
                raise PatchError('base file mismatch')
            after = entry['after']
            if after is not None:
                digest = after.get('sha256', '')
                if len(digest) != 64 or any(c not in '0123456789abcdef' for c in digest):
                    raise PatchError('invalid content digest')
                blob = directory / 'blobs' / digest
                if not blob.is_file() or blob.is_symlink() or blob.stat().st_nlink != 1 or _hash(blob.read_bytes()) != digest:
                    raise PatchError('content hash mismatch')
                mode = after.get('mode')
                if not isinstance(mode, int) or mode < 0 or mode > 0o777:
                    raise PatchError('unsafe mode')
            changes[path] = (directory, entry)
    if not changes:
        raise PatchError('empty stage')
    try:
        stage.mkdir(parents=True)
        for path, identity in sorted(before.items()):
            source = baseline / path
            destination = stage / path
            destination.parent.mkdir(parents=True, exist_ok=True)
            content = source.read_bytes()
            if _hash(content) != identity['sha256']:
                raise PatchError('baseline changed during copy')
            destination.write_bytes(content)
            destination.chmod(identity['mode'])
        for path, (directory, entry) in sorted(changes.items()):
            destination = stage / path
            if entry['after'] is None:
                destination.unlink()
            else:
                destination.parent.mkdir(parents=True, exist_ok=True)
                destination.write_bytes((directory / 'blobs' / entry['after']['sha256']).read_bytes())
                destination.chmod(entry['after']['mode'])
        staged = _tree(stage)
        if callable(verify):
            result = verify(stage)
            success = result is True or (isinstance(result, dict) and result.get('exit_code') == 0 and result.get('qualified') is True)
        elif isinstance(verify, list) and verify and all(isinstance(x, str) for x in verify):
            result = subprocess.run(verify, cwd=stage, capture_output=True, timeout=300)
            success = result.returncode == 0
        else:
            raise PatchError('invalid verifier')
        if not success:
            raise PatchError('stage verification failed')
        if _tree(baseline) != before or _tree(stage) != staged:
            raise PatchError('verification changed source')
        return {'qualified': True, 'baseline_digest': _digest(before), 'result_digest': _digest(staged), 'patches': sorted(identities), 'stage': str(stage)}
    except Exception:
        if stage.exists():
            shutil.rmtree(stage)
        raise
