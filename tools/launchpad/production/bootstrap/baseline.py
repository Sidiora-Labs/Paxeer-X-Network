#!/usr/bin/env python3
"""Read-only source/dependency baseline gate. Writes evidence only to its evidence directory."""
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

PINNED = {'go': '1.26.4', 'node': '24.16.0', 'pnpm': '9.15.0', 'typescript': '5.9.3'}
SKIP = {'.git', '.codegraph', 'node_modules', '.next', '.turbo', '__pycache__', 'vendor', 'coverage', 'dist', 'evidence', '.cache', '.pytest_cache', 'test-results', 'playwright-report'}

def sha(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1048576), b''):
            h.update(block)
    return h.hexdigest()

LAUNCHPAD_HOMES = ('launchpad', 'apps/launchpad', 'services/launchpad', 'sdk/launchpad', 'contracts/launchpad', 'deploy/launchpad', 'tools/launchpad')

def source_files(root, evidence):
    for home in LAUNCHPAD_HOMES:
        yield from home_files(root / home, evidence)

def home_files(base, evidence):
    for directory, children, files in os.walk(base, followlinks=False):
        children[:] = sorted(x for x in children if x not in SKIP and not Path(directory, x).is_symlink() and Path(directory, x).resolve() != evidence)
        for name in sorted(files):
            path = Path(directory, name)
            if path.is_symlink() or name.endswith(('.pyc', '.tsbuildinfo')) or name == '.env' or name.startswith('.env.'):
                continue
            yield path

def license_files(directory):
    if not directory.is_dir():
        return []
    return [{'path': str(p), 'sha256': sha(p)} for p in sorted(directory.iterdir()) if p.is_file() and p.name.lower().startswith(('license', 'copying', 'notice', 'copyright'))]

def escaped(value):
    return ''.join('!' + c.lower() if c.isupper() else c for c in value)

def read_pins(path):
    result = {}
    aliases = {'golang': 'go', 'nodejs': 'node'}
    for line in path.read_text().splitlines():
        parts = line.split('#', 1)[0].split()
        if parts:
            if len(parts) != 2:
                raise ValueError('tool version entry must have exactly one version')
            result[aliases.get(parts[0], parts[0])] = parts[1]
    return result

def main():
    if not os.environ.get('QUALIFICATION_EVIDENCE_DIR'):
        print('QUALIFICATION_EVIDENCE_DIR is required', file=sys.stderr)
        return 2
    root = Path(os.environ.get('QUALIFICATION_SOURCE_ROOT', Path(__file__).resolve().parents[4])).resolve()
    contracts = Path(os.environ.get('QUALIFICATION_CONTRACT_ROOT', root / 'contracts/launchpad')).resolve()
    evidence = Path(os.environ['QUALIFICATION_EVIDENCE_DIR']).resolve()
    evidence.mkdir(parents=True, exist_ok=True)
    report = {'schema_version': 1, 'started_at': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'source_root': str(root), 'contract_root': str(contracts), 'required_versions': PINNED, 'commands': [], 'failures': [], 'manifest_files': [], 'go_modules': [], 'npm_packages': [], 'license_boundary': 'Source evidence only; missing or unclassified license sources remain explicit. No legal approval asserted.'}
    def fail(message):
        report['failures'].append(message)
    def run(argv, cwd=root, timeout=180):
        index = len(report['commands'])
        try:
            result = subprocess.run(argv, cwd=cwd, env={**os.environ, 'GOWORK': 'off', 'GOMAXPROCS': '2', 'GOTOOLCHAIN': 'local', 'COREPACK_ENABLE_NETWORK': '0'}, text=True, capture_output=True, timeout=timeout)
            code, stdout, stderr = result.returncode, result.stdout, result.stderr
        except (OSError, subprocess.TimeoutExpired) as exc:
            code = 124 if isinstance(exc, subprocess.TimeoutExpired) else 127
            stdout = getattr(exc, 'stdout', None) or ''
            stderr = getattr(exc, 'stderr', None) or str(exc)
            stdout = stdout.decode(errors='replace') if isinstance(stdout, bytes) else stdout
            stderr = stderr.decode(errors='replace') if isinstance(stderr, bytes) else stderr
        log = evidence / ('baseline-command-%03d.log' % index)
        log.write_text(stdout + '\n--- stderr ---\n' + stderr)
        report['commands'].append({'argv': argv, 'cwd': str(cwd), 'exit_code': code, 'log': str(log)})
        if code:
            fail('Command failed: ' + ' '.join(argv))
        return code, stdout
    actual_pins = {}
    pin_file = root / 'tools/launchpad/production/bootstrap/toolchain/versions.txt'
    try:
        actual_pins = read_pins(pin_file)
        report['declared_versions'] = actual_pins
        for key, expected in PINNED.items():
            if actual_pins.get(key) != expected:
                fail('tools/launchpad/production/bootstrap/toolchain/versions.txt mismatch for ' + key)
    except (OSError, ValueError) as exc:
        fail('Required tools/production/bootstrap/toolchain/versions.txt unavailable/invalid: ' + str(exc))
    checks = {'python': ['python3', '--version'], 'go': ['go', 'version'], 'node': ['node', '--version'], 'pnpm': ['pnpm', '--version']}
    tsc = root / 'apps/launchpad/web/node_modules/typescript/bin/tsc'
    checks['typescript'] = ['node', str(tsc), '--version']
    for key, argv in checks.items():
        code, output = run(argv, timeout=20)
        match = re.search(r'(?<![\d.])(?:go|v|Version )?(\d+\.\d+\.\d+)(?![\d.])', output)
        if code or not match or match.group(1) != (actual_pins.get('python', actual_pins.get('python3')) if key == 'python' else PINNED[key]):
            fail('Installed tool version mismatch: ' + key)
    lock = root / 'apps/launchpad/pnpm-lock.yaml'
    locked_packages = set()
    try:
        text = lock.read_text()
        locked_packages = set(re.findall(r'''^  ['"]?((?:@[^/\s]+/)?[^@\s:'"]+)@([^([\s:'"]+)[^\n]*:\s*$''', text, re.M))
        if not re.search(r"^lockfileVersion:\s*['\"]?9\.0['\"]?\s*$", text, re.M):
            fail('pnpm lockfileVersion is not 9.0')
        if not re.search(r'^  [\'\"]?typescript@5\.9\.3[:\'\"]', text, re.M):
            fail('TypeScript 5.9.3 absent from lock packages')
        package = json.loads((root / 'apps/launchpad/package.json').read_text())
        if package.get('packageManager') != 'pnpm@' + PINNED['pnpm']:
            fail('client packageManager does not match pinned pnpm')
    except (OSError, ValueError) as exc:
        fail('Required client lock/manifest unavailable: ' + str(exc))
    files = list(source_files(root, evidence))
    initial_hashes = {str(p.relative_to(root)): sha(p) for p in files}
    for path in files:
        if path.name in {'go.mod', 'go.sum', 'package.json', 'pnpm-lock.yaml', 'versions.txt'}:
            report['manifest_files'].append({'path': str(path.relative_to(root)), 'sha256': sha(path)})
    modules = sorted(p for p in files if p.name == 'go.mod' and 'spec' not in p.relative_to(root).parts)
    if len(modules) != 13:
        fail('Expected exactly 13 production Go modules; found ' + str(len(modules)))
    cache_code, cache_output = run(['go', 'env', 'GOMODCACHE'], timeout=20)
    cache = Path(cache_output.strip()) if not cache_code else None
    for mod in modules:
        row = {'path': str(mod.relative_to(root)), 'dependencies': [], 'replacements': []}
        report['go_modules'].append(row)
        if not mod.with_name('go.sum').is_file():
            fail('Missing go.sum: ' + row['path'])
        code, output = run(['go', 'mod', 'edit', '-json'], cwd=mod.parent, timeout=20)
        if not code:
            data = json.loads(output)
            for replacement in data.get('Replace') or []:
                new = replacement['New']
                record = dict(replacement)
                if not new.get('Version'):
                    target = (mod.parent / new['Path']).resolve()
                    record['resolved_path'] = str(target)
                    if not (target / 'go.mod').is_file():
                        fail('Unresolved Go replacement: ' + str(target))
                row['replacements'].append(record)
            for dep in data.get('Require') or []:
                directory = cache / escaped(dep['Path'] + '@' + dep['Version']) if cache else Path('/nonexistent')
                sources = license_files(directory)
                row['dependencies'].append({**dep, 'license_sources': sources, 'license_status': 'source_present_unclassified' if sources else 'unresolved'})
        run(['go', 'mod', 'verify'], cwd=mod.parent)
    package_names = {}
    package_manifests = []
    for path in files:
        if path.name == 'package.json' and 'client' in path.relative_to(root).parts:
            data = json.loads(path.read_text())
            package_names[data.get('name')] = str(path)
            package_manifests.append((path, data))
    for path, data in package_manifests:
        for section in ['dependencies', 'devDependencies', 'peerDependencies', 'optionalDependencies']:
            for name, version in data.get(section, {}).items():
                if version.startswith('workspace:') and name not in package_names:
                    fail('Unresolved npm workspace package ' + name + ' in ' + str(path))
                if version.startswith(('file:', 'link:')) and not (path.parent / version.split(':', 1)[1]).exists():
                    fail('Unresolved npm local dependency ' + name + ' in ' + str(path))
    packages = root / 'apps/launchpad/node_modules/.pnpm'
    if not packages.is_dir():
        fail('Resolved pnpm store unavailable')
    else:
        for entry in sorted(packages.iterdir()):
            nested = entry / 'node_modules'
            if not nested.is_dir() or entry.name == 'node_modules':
                continue
            for item in sorted(nested.iterdir()):
                candidates = sorted(item.iterdir()) if item.name.startswith('@') and item.is_dir() else [item]
                for package in candidates:
                    if package.is_symlink() or not (package / 'package.json').is_file():
                        continue
                    try:
                        data = json.loads((package / 'package.json').read_text())
                        sources = license_files(package)
                        if (data.get('name'), data.get('version')) not in locked_packages:
                            fail('Installed npm package absent from lock: ' + str(data.get('name')) + '@' + str(data.get('version')))
                        report['npm_packages'].append({'name': data.get('name'), 'version': data.get('version'), 'manifest': str(package / 'package.json'), 'manifest_sha256': sha(package / 'package.json'), 'declared_license': data.get('license'), 'license_sources': sources, 'license_status': 'source_present_unclassified' if sources else 'unresolved'})
                    except (OSError, ValueError) as exc:
                        fail('Unreadable installed package: ' + str(exc))
        if not report['npm_packages']:
            fail('No real installed npm package manifests found')
    abi = root / 'sdk/launchpad/protocol/bootstrap/abi_baseline.py'
    if not abi.is_file():
        fail('ABI baseline helper missing: ' + str(abi))
    elif not contracts.is_dir():
        fail('Contract source root missing: ' + str(contracts))
    else:
        run([sys.executable, str(abi), '--contracts', str(contracts), '--output', str(evidence / 'abi-baseline.json')])
    abi_report = evidence / 'abi-baseline.json'
    if abi_report.is_file():
        report['abi_evidence'] = {'path': str(abi_report), 'sha256': sha(abi_report)}
    report['candidate_files'] = [{'path': str(p.relative_to(root)), 'sha256': sha(p)} for p in files]
    if initial_hashes != {str(p.relative_to(root)): sha(p) for p in source_files(root, evidence)}:
        fail('Source changed during baseline collection; rerun against final integrated candidate')
    report['candidate_digest'] = hashlib.sha256(json.dumps(report['candidate_files'], sort_keys=True, separators=(',', ':')).encode()).hexdigest()
    report['finished_at'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    report['status'] = 'passed' if not report['failures'] else 'failed'
    output = evidence / 'baseline.json'
    output.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({'status': report['status'], 'failures': report['failures'], 'report': str(output), 'candidate_digest': report['candidate_digest'], 'npm_packages': len(report['npm_packages'])}))
    return bool(report['failures'])

if __name__ == '__main__':
    raise SystemExit(main())
