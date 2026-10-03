#!/usr/bin/env python3
"""Custody governance artifacts: produce the five test executables with their
manifest, or run them from a validated manifest without compiling anything."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

SCHEMA = 'paxeer-x.custody-governance-artifacts.v1'
ROOT = Path(__file__).resolve().parents[3]
BINARIES = {
    'custody.test': './modules/layerxcustody',
    'custody-keeper.test': './modules/layerxcustody/keeper',
    'custody-types.test': './modules/layerxcustody/types',
    'custody-cli.test': './modules/layerxcustody/client/cli',
    'node-custody.test': './node',
}
NODE_CASES = (
    'TestCustodyGovernanceRouterDispatchesAllMessages',
    'TestCustodyGovernanceRouterRefusalsAreAtomic',
    'TestCustodyGovernanceSubmissionDoesNotExecute',
    'TestCustodyGovernanceMsgServiceAuthority',
    'TestCustodyGovernanceV611ActivationBoundaries',
    'TestCustodyGovernanceHistoricalContextRefusal',
)
INPUTS = ('go.mod', 'go.sum', 'vendor/modules.txt')
RESULT = re.compile(r'^--- (PASS|FAIL|SKIP): (\S+)', re.M)
TIMEOUT = '20m'


class Refused(Exception):
    pass


def run(argv, **kw):
    return subprocess.run(argv, cwd=kw.pop('cwd', ROOT), text=True, capture_output=True, check=False, **kw)


def out(argv):
    proc = run(argv)
    if proc.returncode:
        raise Refused(f'{" ".join(argv)} exited {proc.returncode}: {proc.stderr.strip()}')
    return proc.stdout.strip()


def sha256(path):
    h = hashlib.sha256()
    with open(path, 'rb') as f:
        for chunk in iter(lambda: f.read(1 << 20), b''):
            h.update(chunk)
    return h.hexdigest()


def source_digests():
    digests = {}
    for pkg in BINARIES.values():
        for path in sorted((ROOT / pkg).glob('*.go')):
            digests[str(path.relative_to(ROOT))] = sha256(path)
    for name in INPUTS:
        digests[name] = sha256(ROOT / name)
    return digests


def identity():
    return {'revision': out(['git', 'rev-parse', 'HEAD']), 'tree': out(['git', 'rev-parse', 'HEAD^{tree}'])}


def produce(artifacts):
    artifacts.mkdir(parents=True, exist_ok=True)
    packages = list(BINARIES.values())
    listed = out(['go', 'list', '-f', '{{.ImportPath}} {{.Dir}}', *packages]).splitlines()
    dirs = {str(Path(line.split(' ', 1)[1]).relative_to(ROOT)) for line in listed}
    missing = [p for p in packages if os.path.normpath(p) not in dirs]
    if missing:
        raise Refused(f'go list omitted custody packages: {missing}')
    closure = out(['go', 'list', '-deps', '-test', *packages]).splitlines()
    keys = ('GOOS', 'GOARCH', 'CGO_ENABLED', 'GOFLAGS')
    env = dict(zip(keys, out(['go', 'env', *keys]).split('\n')))
    producers, outputs = [], {}
    for name, pkg in BINARIES.items():
        target = artifacts / name
        argv = ['go', 'test', '-c', '-o', str(target), pkg]
        proc = run(argv)
        sys.stdout.write(proc.stdout)
        sys.stderr.write(proc.stderr)
        producers.append({'argv': argv, 'exit': proc.returncode})
        if proc.returncode:
            raise Refused(f'producer {" ".join(argv)} exited {proc.returncode}')
        ldd = run(['ldd', str(target)])
        outputs[name] = {'package': pkg, 'sha256': sha256(target), 'ldd': ldd.stdout.strip() or ldd.stderr.strip()}
    manifest = {
        'schema': SCHEMA, **identity(), 'sources': source_digests(), 'go_version': out(['go', 'version']),
        'go_env': env, 'tags': env.get('GOFLAGS', ''), 'closure_packages': len(closure),
        'producers': producers, 'outputs': outputs,
    }
    (artifacts / 'manifest.json').write_text(json.dumps(manifest, indent=2, sort_keys=True) + '\n')
    print(f'custody governance artifacts written: {len(outputs)} executables')


def load(manifest_path):
    manifest = json.loads(manifest_path.read_text())
    if manifest.get('schema') != SCHEMA:
        raise Refused(f'manifest schema {manifest.get("schema")!r} is not {SCHEMA}')
    current = identity()
    for field in ('revision', 'tree'):
        if manifest.get(field) != current[field]:
            raise Refused(f'stale manifest: {field} {manifest.get(field)} != {current[field]}')
    if manifest.get('sources') != source_digests():
        raise Refused('stale manifest: scoped source digests differ from the checkout')
    if any(p.get('exit') != 0 for p in manifest.get('producers', [])) or len(manifest.get('producers', [])) != len(BINARIES):
        raise Refused('manifest producers are incomplete or failed')
    outputs = manifest.get('outputs', {})
    for name, pkg in BINARIES.items():
        entry = outputs.get(name)
        path = manifest_path.parent / name
        if not entry or entry.get('package') != pkg:
            raise Refused(f'manifest lacks {name} for {pkg}')
        if not path.is_file() or not os.access(path, os.X_OK):
            raise Refused(f'missing executable {path}')
        if sha256(path) != entry.get('sha256'):
            raise Refused(f'{name} does not match its recorded sha256')
    return manifest


def execute(binary, pkg, extra):
    argv = [str(binary), '-test.v', '-test.count=1', f'-test.timeout={TIMEOUT}', *extra]
    proc = subprocess.run(argv, cwd=ROOT / pkg, text=True, capture_output=True, check=False)
    sys.stdout.write(proc.stdout)
    sys.stderr.write(proc.stderr)
    results = RESULT.findall(proc.stdout)
    print(f'custody governance: {binary.name} exit={proc.returncode} results={len(results)}')
    return proc.returncode, results


def gate(manifest_path):
    load(manifest_path)
    tests = skipped = 0
    failures = []
    for name, pkg in BINARIES.items():
        extra = ['-test.run', '^(' + '|'.join(NODE_CASES) + ')$'] if name == 'node-custody.test' else []
        code, results = execute(manifest_path.parent / name, pkg, extra)
        if code:
            failures.append(f'{name} exited {code}')
        if not results:
            failures.append(f'{name} ran no tests')
        status = {}
        for verdict, case in results:
            status[case] = verdict
            tests += 1
            skipped += verdict == 'SKIP'
            if verdict == 'FAIL':
                failures.append(f'{name} {case} failed')
        if name == 'node-custody.test':
            for case in NODE_CASES:
                if status.get(case) != 'PASS':
                    failures.append(f'declared case {case} is {status.get(case, "missing")}')
            if set(status) != set(NODE_CASES):
                failures.append(f'node cases differ from the declared set: {sorted(status)}')
    print(f'PAXEER_X_GATE tests={tests} skipped={skipped}')
    if failures:
        raise Refused('; '.join(failures))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--produce', type=Path, help='artifact directory to compile into')
    parser.add_argument('--manifest', type=Path, help='manifest of prebuilt artifacts to run')
    args = parser.parse_args()
    if bool(args.produce) == bool(args.manifest):
        parser.error('pass exactly one of --produce or --manifest')
    try:
        produce(args.produce.resolve()) if args.produce else gate(args.manifest.resolve())
    except (Refused, OSError, ValueError) as err:
        print(f'custody governance refused: {err}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
