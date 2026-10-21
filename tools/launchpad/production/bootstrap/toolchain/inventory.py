#!/usr/bin/env python3
"""Offline, bounded inventory; declarations and cached licenses are not resolution proof."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess

EXCLUDED = {'.git', 'node_modules', '.next', '.turbo', 'vendor', '__pycache__'}

def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def command(argv, cwd):
    executable = shutil.which(argv[0])
    if not executable:
        return {'argv': argv, 'path': None, 'exit_code': 127, 'stdout': '', 'stderr': 'not installed'}
    try:
        result = subprocess.run(argv, cwd=cwd, env={**os.environ, 'GOTOOLCHAIN': 'local', 'COREPACK_ENABLE_NETWORK': '0'}, capture_output=True, text=True, timeout=15)
        return {'argv': argv, 'path': executable, 'exit_code': result.returncode, 'stdout': result.stdout, 'stderr': result.stderr}
    except subprocess.TimeoutExpired as error:
        return {'argv': argv, 'path': executable, 'exit_code': 124, 'stdout': str(error.stdout or ''), 'stderr': '15 second timeout'}

def license_sources(directory):
    if not directory.is_dir():
        return []
    return [{'path': str(p), 'sha256': digest(p)} for p in sorted(directory.iterdir()) if p.is_file() and p.name.lower().startswith(('license', 'copying', 'notice', 'copyright'))]

def escaped_module(value):
    return ''.join('!' + c.lower() if c.isupper() else c for c in value)

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parents[5])
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    root = args.root.resolve()
    paths = []
    for home in ('launchpad', 'apps/launchpad', 'services/launchpad', 'sdk/launchpad', 'contracts/launchpad', 'deploy/launchpad', 'tools/launchpad'):
        for directory, children, files in os.walk(root / home, followlinks=False):
            children[:] = sorted(d for d in children if d not in EXCLUDED and not Path(directory, d).is_symlink())
            paths.extend(Path(directory, f) for f in sorted(files) if f in {'go.mod', 'package.json'} or f.lower().startswith(('license', 'copying', 'notice', 'copyright')))
    if len(paths) > 10000:
        raise SystemExit('inventory exceeded 10000 source file bound')
    tools = [command(a, root) for a in [['go', 'version'], ['node', '--version'], ['npm', '--version'], ['pnpm', '--version'], ['corepack', '--version'], ['python3', '--version'], ['protoc', '--version'], ['buf', '--version'], ['docker', '--version'], ['solc', '--version'], ['forge', '--version']]]
    cache_result = command(['go', 'env', 'GOMODCACHE'], root)
    cache = Path(cache_result['stdout'].strip()) if cache_result['exit_code'] == 0 else None
    manifests, licenses, errors = [], [], []
    unique_go = {}
    for path in paths:
        relative = str(path.relative_to(root))
        if path.name == 'go.mod':
            parsed = command(['go', 'mod', 'edit', '-json', str(path)], root)
            if parsed['exit_code']:
                errors.append({'path': relative, 'command': parsed})
                continue
            data = json.loads(parsed['stdout'])
            manifests.append({'path': relative, 'sha256': digest(path), 'kind': 'go', 'declarations': data})
            for dependency in data.get('Require') or []:
                key = dependency['Path'] + '@' + dependency['Version']
                unique_go[key] = dependency
        elif path.name == 'package.json':
            data = json.loads(path.read_text())
            manifests.append({'path': relative, 'sha256': digest(path), 'kind': 'npm', 'name': data.get('name'), 'version': data.get('version'), 'license': data.get('license'), 'packageManager': data.get('packageManager'), 'engines': data.get('engines'), 'dependencies': {k: data[k] for k in ['dependencies', 'devDependencies', 'peerDependencies', 'optionalDependencies', 'overrides', 'pnpm'] if k in data}})
        else:
            licenses.append({'path': relative, 'sha256': digest(path)})
    cached = []
    for key, dep in sorted(unique_go.items()):
        directory = cache / escaped_module(key) if cache else None
        sources = license_sources(directory) if directory else []
        cached.append({'module': dep['Path'], 'version': dep['Version'], 'cached': bool(directory and directory.is_dir()), 'license_sources': sources, 'license_status': 'source_present_unclassified' if sources else 'unresolved'})
    npm_dependencies = []
    for manifest in manifests:
        if manifest['kind'] != 'npm':
            continue
        for section in ['dependencies', 'devDependencies', 'peerDependencies', 'optionalDependencies']:
            for name, version in sorted(manifest['dependencies'].get(section, {}).items()):
                npm_dependencies.append({'manifest': manifest['path'], 'section': section, 'name': name, 'declared_version': version, 'license_status': 'unresolved', 'reason': 'Declared version does not prove resolved package or its license'})
    report = {'schema_version': 1, 'generated_at': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'root': str(root), 'scope': 'Source declarations and offline Go cache license sources; no network resolution, transitive npm audit or legal classification', 'tools': tools, 'go_cache_probe': cache_result, 'manifests': manifests, 'repository_license_sources': licenses, 'go_dependency_licenses': cached, 'npm_dependency_licenses': npm_dependencies, 'errors': errors, 'summary': {'go_manifests': sum(m['kind'] == 'go' for m in manifests), 'npm_manifests': sum(m['kind'] == 'npm' for m in manifests), 'unique_go_dependencies': len(cached), 'go_dependencies_with_license_source': sum(bool(d['license_sources']) for d in cached), 'repository_license_sources': len(licenses)}}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({'output': str(args.output), 'summary': report['summary'], 'errors': len(errors)}))
    return bool(errors)

if __name__ == '__main__':
    raise SystemExit(main())
