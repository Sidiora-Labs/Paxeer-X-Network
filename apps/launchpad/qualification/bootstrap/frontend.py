#!/usr/bin/env python3
"""Read-only frontend prerequisites; nonzero means build is not qualified."""
import argparse
import json
import pathlib
import re
import shutil
import subprocess
import sys

CLIENT = pathlib.Path(__file__).resolve().parents[2]
WEB = CLIENT / 'web'

def inspect():
    root = json.loads((CLIENT / 'package.json').read_text())
    app = json.loads((WEB / 'package.json').read_text())
    manifests = list((CLIENT.parents[1] / 'sdk/launchpad/packages').glob('*/package.json'))
    packages = {json.loads(p.read_text())['name']: p.parent for p in manifests}
    required = sorted(k for k, v in app['dependencies'].items() if v.startswith('workspace:'))
    missing = [k for k in required if k not in packages]
    imports = {}
    unresolved = []
    for path in sorted((WEB / 'src').rglob('*')):
        if path.suffix not in ('.ts', '.tsx', '.js', '.jsx'): continue
        for name in re.findall(r'''(?:\bfrom\s+|\bimport\s*\(|\brequire\s*\()['"]([^'"]+)['"]''', path.read_text()):
            if name.startswith('.') or name.startswith('@/'):
                candidate = path.parent / name if name.startswith('.') else WEB / 'src' / name[2:]
                if not any(p.exists() for p in [candidate, *[pathlib.Path(str(candidate)+s) for s in ('.ts','.tsx','.js','.jsx','.json','.css')], candidate/'index.ts',candidate/'index.tsx']):
                    unresolved.append({'source': str(path.relative_to(CLIENT)), 'import': name})
            else:
                package = '/'.join(name.split('/')[:2]) if name.startswith('@') else name.split('/')[0]
                imports.setdefault(package, set()).add(str(path.relative_to(CLIENT)))
    tools = {}
    for tool in ('node', 'pnpm'):
        if shutil.which(tool):
            p = subprocess.run([tool, '--version'], text=True, capture_output=True)
            tools[tool] = {'version': p.stdout.strip(), 'exit_code': p.returncode}
        else: tools[tool] = {'version': None, 'exit_code': 127}
    lock = (CLIENT / 'pnpm-lock.yaml').read_text()
    lock_links = re.findall(r'version: link:([^\n]+)', lock)
    stale_links = [v for v in lock_links if not (WEB / v).exists()]
    declared = set(app['dependencies']) | set(app['devDependencies']) | set(root.get('dependencies', {})) | set(root.get('devDependencies', {}))
    undeclared = sorted(k for k in imports if k not in declared and k not in ('node:crypto','node:buffer','node:path','path','crypto','fs'))
    blockers = []
    if missing: blockers.append('missing workspace packages')
    if unresolved: blockers.append('unresolved local imports')
    if stale_links: blockers.append('stale lockfile workspace links; root must regenerate after package integration')
    if undeclared: blockers.append('undeclared external imports require review')
    expected = root['packageManager'].split('@')[-1]
    if tools['pnpm']['version'] != expected: blockers.append('pnpm version differs from declared packageManager')
    return {'schema_version': 1, 'qualified': False, 'prerequisites_ready': not blockers,
            'toolchain': tools, 'declared_package_manager': root['packageManager'],
            'declared_versions': {k: app.get('dependencies', {}).get(k,app.get('devDependencies', {}).get(k)) for k in ('next','react','react-dom','typescript','vitest','eslint','eslint-config-next')},
            'workspace_packages': required, 'missing_workspace_packages': missing,
            'unresolved_local_imports': unresolved, 'undeclared_external_imports': undeclared,
            'external_imports': {k:sorted(v) for k,v in sorted(imports.items())},
            'stale_lock_links': stale_links, 'existing_tests': [str(p.relative_to(CLIENT)) for p in sorted((WEB/'src').rglob('*.test.*'))],
            'blockers': blockers,
            'limitations': ['Import scan is lexical, not TypeScript resolution or exported-symbol qualification.', 'No install, build, tests, native runtime or live browser qualification is implied.']}

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--output', type=pathlib.Path)
    args = parser.parse_args()
    result = inspect()
    encoded = json.dumps(result, indent=2)+'\n'
    if args.output: args.output.write_text(encoded)
    print(encoded, end='')
    sys.exit(0 if result['prerequisites_ready'] else 1)
