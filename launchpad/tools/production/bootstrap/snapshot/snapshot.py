#!/usr/bin/env python3
"""Content-addressed source inventory and fail-closed owned change validation."""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import stat
import sys

EXCLUDED_DIRS = frozenset({'.git', 'node_modules', '__pycache__', '.next', '.turbo', '.cache', 'dist', 'build', 'coverage', '.venv', 'venv', 'runtime', 'logs'})
SECRET_NAMES = frozenset({'id_rsa', 'id_ed25519', 'id_ecdsa', '.npmrc', '.pypirc', '.netrc', 'credentials.json', 'service-account.json'})
SECRET_SUFFIXES = ('.pem', '.key', '.p12', '.pfx', '.keystore')


def excluded(parts):
    name = parts[-1]
    return (name in SECRET_NAMES or any(p in EXCLUDED_DIRS for p in parts)
            or any(p in {'secrets', '.secrets', 'credentials'} for p in parts)
            or name == '.env' or name.startswith('.env.')
            or name.endswith(SECRET_SUFFIXES + ('.pyc', '.log', '.tsbuildinfo')))


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True).encode()


def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def inventory(root):
    root = Path(root).resolve(strict=True)
    if not root.is_dir():
        raise ValueError('source root must be a directory')
    files = {}
    for directory, dirs, names in os.walk(root, followlinks=False):
        relative = Path(directory).relative_to(root)
        dirs[:] = sorted(d for d in dirs if not excluded((relative / d).parts))
        for name in sorted(names + [d for d in dirs if (Path(directory) / d).is_symlink()]):
            path = Path(directory) / name
            rel = path.relative_to(root).as_posix()
            if excluded(PurePosixPath(rel).parts):
                continue
            before = path.lstat()
            if stat.S_ISLNK(before.st_mode):
                # Never follow links: an apparent source file must not expose an external secret.
                raise ValueError('source symlink rejected: ' + rel)
            if not stat.S_ISREG(before.st_mode):
                raise ValueError('non-regular source rejected: ' + rel)
            flags = os.O_RDONLY | getattr(os, 'O_NOFOLLOW', 0)
            with os.fdopen(os.open(path, flags), 'rb') as stream:
                opened = os.fstat(stream.fileno())
                h = hashlib.sha256()
                for chunk in iter(lambda: stream.read(1024 * 1024), b''):
                    h.update(chunk)
                after = os.fstat(stream.fileno())
            end = path.lstat()
            identity = lambda s: (s.st_dev, s.st_ino, s.st_size, s.st_mtime_ns, s.st_ctime_ns, s.st_mode)
            if not (identity(before) == identity(opened) == identity(after) == identity(end)):
                raise ValueError('source changed while hashing: ' + rel)
            files[rel] = {'sha256': h.hexdigest(), 'size': after.st_size,
                          'executable': bool(after.st_mode & 0o111)}
    body = {'version': 1, 'policy': 'source-v1', 'files': files}
    return dict(body, sha256=digest(body))


def validate_manifest(manifest):
    if set(manifest) != {'version', 'policy', 'files', 'sha256'}:
        raise ValueError('invalid manifest fields')
    if manifest['version'] != 1 or manifest['policy'] != 'source-v1':
        raise ValueError('unsupported manifest policy')
    if manifest['sha256'] != digest({k: v for k, v in manifest.items() if k != 'sha256'}):
        raise ValueError('manifest digest mismatch')
    for name, entry in manifest['files'].items():
        validate_path(name)
        if excluded(PurePosixPath(name).parts):
            raise ValueError('excluded path in manifest: ' + name)
        if (set(entry) != {'sha256', 'size', 'executable'}
                or not isinstance(entry['sha256'], str) or len(entry['sha256']) != 64
                or any(c not in '0123456789abcdef' for c in entry['sha256'])
                or type(entry['size']) is not int or entry['size'] < 0
                or type(entry['executable']) is not bool):
            raise ValueError('invalid file entry: ' + name)
    return manifest


def validate_path(path):
    if not isinstance(path, str) or not path or '\\' in path:
        raise ValueError('invalid relative path')
    p = PurePosixPath(path)
    if p.is_absolute() or '..' in p.parts or '.' in path.split('/') or p.as_posix() != path:
        raise ValueError('invalid relative path: ' + path)


def owned_diff(baseline, candidate, owned):
    validate_manifest(baseline)
    validate_manifest(candidate)
    if not owned:
        raise ValueError('at least one owned path is required')
    prefixes = []
    for item in owned:
        tree = item.endswith('/**')
        prefix = item[:-3] if tree else item
        validate_path(prefix)
        if any(c in prefix for c in '*?['):
            raise ValueError('only literal paths and directory/** ownership are supported')
        prefixes.append((prefix, tree))
    changes = []
    for path in sorted(baseline['files'].keys() | candidate['files'].keys()):
        old, new = baseline['files'].get(path), candidate['files'].get(path)
        if old == new:
            continue
        if not any(path == prefix or (tree and path.startswith(prefix + '/')) for prefix, tree in prefixes):
            raise ValueError('change outside owned paths: ' + path)
        changes.append({'path': path, 'before': old, 'after': new})
    body = {'version': 1, 'baseline_sha256': baseline['sha256'],
            'candidate_sha256': candidate['sha256'], 'changes': changes}
    return dict(body, sha256=digest(body))


def write_new(path, value):
    # Exclusive creation preserves previous baselines, even when the caller repeats a command.
    with open(path, 'x', encoding='utf-8') as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write('\n')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='action', required=True)
    create = sub.add_parser('create')
    create.add_argument('--root', required=True)
    create.add_argument('--output', required=True)
    for action in ('verify', 'diff'):
        check = sub.add_parser(action)
        check.add_argument('--root', required=True)
        check.add_argument('--baseline', required=True)
        if action == 'diff':
            check.add_argument('--owned', action='append', required=True)
            check.add_argument('--output', required=True)
    args = parser.parse_args()
    try:
        if args.action == 'create':
            if Path(args.output).resolve().is_relative_to(Path(args.root).resolve()):
                raise ValueError('manifest output must be outside source root')
            value = inventory(args.root)
            write_new(args.output, value)
        else:
            with open(args.baseline, encoding='utf-8') as stream:
                baseline = validate_manifest(json.load(stream))
            candidate = inventory(args.root)
            if args.action == 'verify':
                if baseline != candidate:
                    raise ValueError('source drift detected')
                value = {'sha256': candidate['sha256'], 'verified': True}
            else:
                if Path(args.output).resolve().is_relative_to(Path(args.root).resolve()):
                    raise ValueError('diff output must be outside source root')
                value = owned_diff(baseline, candidate, args.owned)
                write_new(args.output, value)
        print(json.dumps({'sha256': value['sha256'], 'action': args.action}))
        return 0
    except (OSError, ValueError, TypeError, KeyError) as error:
        print(str(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
