#!/usr/bin/env python3
"""Inventory or execute explicit standalone Go builds; never skip failed enumeration."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

BASE = Path(__file__).resolve().parent
DEFAULT_ROOT = BASE.parents[3]
PRUNE = {'.git', 'node_modules', 'vendor', '.next', 'dist', '.cache'}


def load_manifest(root, manifest_path):
    manifest = json.loads(manifest_path.read_text())
    if manifest.get('schemaVersion') != 1 or not manifest.get('modules'):
        raise ValueError('unsupported or empty build manifest')
    seen = set()
    for row in manifest['modules']:
        relative = Path(row['path'])
        if relative.is_absolute() or '..' in relative.parts or row['path'] in seen:
            raise ValueError('unsafe or duplicate module path: ' + row['path'])
        seen.add(row['path'])
        directory = (root / relative).resolve()
        if not directory.is_relative_to(root):
            raise ValueError('module escapes root: ' + row['path'])
        gomod = directory / 'go.mod'
        if not gomod.is_file():
            raise ValueError('missing required go.mod: ' + row['path'])
        source = gomod.read_text()
        module = re.search(r'^module\s+(\S+)\s*$', source, re.M)
        version = re.search(r'^go\s+(\S+)\s*$', source, re.M)
        if not module or module.group(1) != row['module'] or not version or version.group(1) != row['goVersion']:
            raise ValueError('module identity/toolchain changed: ' + row['path'])
        if row.get('listArgv') != ['go', 'list', './...'] or row.get('buildArgv') != ['go', 'build', '-p', '1', './...']:
            raise ValueError('unexpected build command: ' + row['path'])
        for entrypoint in row.get('entrypoints', []):
            entry = (directory / entrypoint).resolve()
            if not entry.is_relative_to(directory) or not (entry / 'main.go').is_file():
                raise ValueError('missing/unsafe service entrypoint: ' + row['path'])
    actual = set()
    for current, dirs, files in os.walk(root):
        dirs[:] = [name for name in dirs if name not in PRUNE and not name.startswith('.env')]
        if 'go.mod' in files:
            actual.add(Path(current).relative_to(root).as_posix())
    if actual != seen:
        raise ValueError('module coverage mismatch: missing=' + repr(sorted(seen - actual)) + '; unlisted=' + repr(sorted(actual - seen)))
    return manifest


def execute(root, manifest, selected, stage, log_dir, timeout):
    env = dict(os.environ, GOWORK='off', GOMAXPROCS='2')
    # Do not let ambient GOFLAGS change package selection/build behavior.
    env['GOFLAGS'] = '-mod=readonly'
    log_dir.mkdir(parents=True, exist_ok=True)
    records = []
    failed = False
    for row in manifest['modules']:
        if selected and row['path'] not in selected:
            continue
        record = {'module': row['path'], 'status': 'not_run', 'commands': []}
        records.append(record)
        if failed:
            continue
        for name in ['list'] + (['build'] if stage == 'build' else []):
            argv = row[name + 'Argv']
            prefix = log_dir / (row['path'].replace('/', '_') + '-' + name)
            stdout_path = str(prefix) + '.stdout.log'
            stderr_path = str(prefix) + '.stderr.log'
            try:
                with open(stdout_path, 'wb') as stdout, open(stderr_path, 'wb') as stderr:
                    proc = subprocess.run(argv, cwd=root / row['path'], env=env, stdout=stdout, stderr=stderr, timeout=timeout, check=False)
                code = proc.returncode
            except subprocess.TimeoutExpired:
                code = 124
            except OSError as error:
                Path(stderr_path).write_text(str(error) + '\n')
                code = 127
            command = {'argv': argv, 'cwd': str(root / row['path']), 'exitCode': code,
                       'stdoutLog': stdout_path, 'stderrLog': stderr_path}
            record['commands'].append(command)
            if code != 0:
                record.update(status='failed', reason=name + ' failed; not an empty-module skip')
                failed = True
                break
            if name == 'list' and not Path(stdout_path).read_bytes().strip():
                record.update(status='failed', reason='required module has no enumerated packages')
                failed = True
                break
        if not failed:
            record['status'] = 'passed'
    return records, 1 if failed else 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument('--check', action='store_true', help='source manifest consistency only; not a build')
    mode.add_argument('--plan', action='store_true', help='print exact command inventory; do not execute')
    mode.add_argument('--run', action='store_true', help='execute real commands sequentially')
    parser.add_argument('--root', type=Path, default=DEFAULT_ROOT)
    parser.add_argument('--manifest', type=Path, default=BASE / 'manifest.json')
    parser.add_argument('--module', action='append', default=[])
    parser.add_argument('--stage', choices=['list', 'build'], default='build')
    parser.add_argument('--log-dir', type=Path)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--timeout', type=int, default=300)
    args = parser.parse_args()
    code = 0
    report = {'schemaVersion': 1, 'scope': 'standalone Go build baseline only; no tests, client build, fixture or production qualification',
              'environment': {'GOWORK': 'off', 'GOMAXPROCS': '2', 'GOFLAGS': '-mod=readonly'},
              'mode': 'run' if args.run else 'plan' if args.plan else 'check', 'stage': args.stage}
    try:
        if args.timeout <= 0:
            raise ValueError('timeout must be positive')
        root = args.root.resolve()
        manifest = load_manifest(root, args.manifest)
        unknown = set(args.module) - {m['path'] for m in manifest['modules']}
        if unknown:
            raise ValueError('unknown selected module: ' + repr(sorted(unknown)))
        report['manifestSha256'] = hashlib.sha256(args.manifest.read_bytes()).hexdigest()
        report['root'] = str(root)
        report['modules'] = [r for r in manifest['modules'] if not args.module or r['path'] in args.module]
        report['partialSelection'] = bool(args.module)
        if args.run:
            if not args.log_dir:
                raise ValueError('--run requires --log-dir')
            report['results'], code = execute(root, manifest, set(args.module), args.stage, args.log_dir.resolve(), args.timeout)
            report['status'] = 'failed' if code else 'passed'
        else:
            report['status'] = 'inventory_only'
    except (ValueError, KeyError, TypeError, OSError) as error:
        report.update(status='failed', error=str(error))
        code = 1
    rendered = json.dumps(report, indent=2) + '\n'
    if args.output:
        args.output.write_text(rendered)
    else:
        sys.stdout.write(rendered)
    return code


if __name__ == '__main__':
    sys.exit(main())
