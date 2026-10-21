#!/usr/bin/env python3
"""Inventory source tests and validate complete native runner output; never run suites implicitly."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

MODULES = ('shared', 'protocol', 'core/api', 'core/indexer', 'core/pnl-tracker',
           'core/ranking-algo', 'core/stats-workers', 'core/trading-charts',
           'media/gateway', 'media/livestream', 'media/metadata', 'media/social', 'media/user')
ROOT = Path(__file__).resolve().parents[4]
GO_TEST = re.compile(r'^func\s+(Test\w+|Example\w*|Fuzz\w+)\s*\(', re.M)
WEB_TEST = re.compile(r'\b(?:test|it)\s*(?:\.(?:skip|todo|only))?\s*\(\s*([\'"`])(.+?)\1', re.S)
SKIP = re.compile(r'\.(?:Skip(?:f|Now)?|skip|todo|only)\b')


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def inventory(root):
    result = {'schema_version': 1, 'evidence_level': 'source_inventory_only',
              'go_modules': [], 'web_files': [], 'issues': [], 'testing_gaps': []}
    for name in MODULES:
        directory = root / name
        manifest = directory / 'go.mod'
        item = {'path': name, 'manifest_exists': manifest.is_file(), 'files': [],
                'command': ['go', 'test', '-p', '1', '-count=1', '-json', './...'],
                'environment': {'GOWORK': 'off', 'GOMAXPROCS': '2'}}
        if not manifest.is_file():
            result['issues'].append('missing module: ' + name)
        else:
            item['manifest_sha256'] = sha(manifest)
            match = re.search(r'^module\s+(\S+)', manifest.read_text(), re.M)
            if not match:
                result['issues'].append('invalid module declaration: ' + name)
            else:
                item['module_import'] = match.group(1)
        for path in sorted(directory.rglob('*_test.go')):
            source = path.read_text()
            declarations = [{'name': m.group(1), 'line': source.count('\n', 0, m.start()) + 1}
                            for m in GO_TEST.finditer(source)]
            item['files'].append({'path': path.relative_to(root).as_posix(), 'sha256': sha(path),
                                  'declarations': declarations,
                                  'package': item.get('module_import', '') +
                                  (('/' + path.parent.relative_to(directory).as_posix())
                                   if path.parent != directory else ''),
                                  'build_constraints': [line for line in source.splitlines()
                                                        if line.startswith('//go:build')],
                                  'skip_or_focus_markers': [i for i, line in enumerate(source.splitlines(), 1)
                                                            if SKIP.search(line)]})
        item['declared_test_count'] = sum(len(f['declarations']) for f in item['files'])
        if not item['declared_test_count']:
            result['testing_gaps'].append('build-only module with no declared Go tests: ' + name)
        result['go_modules'].append(item)
    web = root / 'client/apps/web'
    for path in sorted(web.rglob('*')):
        if not path.is_file() or any(x in path.parts for x in ('node_modules', '.next')):
            continue
        if not re.search(r'\.(?:test|spec)\.[cm]?[jt]sx?$', path.name):
            continue
        source = path.read_text()
        result['web_files'].append({'path': path.relative_to(root).as_posix(), 'sha256': sha(path),
                                   'literal_declarations': [m.group(2) for m in WEB_TEST.finditer(source)],
                                   'skip_or_focus_markers': [i for i, line in enumerate(source.splitlines(), 1)
                                                             if SKIP.search(line)]})
    result['web_discovery_note'] = 'Literal names are source inventory, not runtime test enumeration; native Vitest assertionResults are authoritative.'
    result['web_command'] = ['pnpm', 'exec', 'vitest', 'run', '--reporter=json']
    if not result['web_files']:
        result['issues'].append('no frontend test files')
    return result


def validate_go(events, expected):
    if not expected:
        raise ValueError('no expected named tests; empty suite cannot qualify')
    wanted = {(f['package'], d['name']) for f in expected for d in f['declarations']
              if d['name'].startswith('Test')}
    if not wanted:
        raise ValueError('no expected Test functions')
    # Native go test emits a package-level skip for packages containing no tests.
    # That is distinct from a skipped test, and must be corroborated by its output.
    events = list(events)
    package_output = {}
    for event in events:
        if event.get('Action') == 'output' and not event.get('Test'):
            package_output.setdefault(event.get('Package'), []).append(event.get('Output', ''))
    wanted_packages = {package for package, _ in wanted}
    ran, passed, package_passed, no_test_packages = set(), set(), set(), set()
    for event in events:
        action, package, test = event.get('Action'), event.get('Package'), event.get('Test')
        if action == 'fail':
            raise ValueError('runner reported fail: ' + str(package) + ' ' + str(test))
        if action == 'skip':
            output = ''.join(package_output.get(package, []))
            native_no_tests = any(
                re.fullmatch(r'\?\s+' + re.escape(package) + r'\s+\[no test files\]\s*', line) is not None
                for line in output.splitlines()
            ) if isinstance(package, str) and package else False
            if test or package in wanted_packages or not native_no_tests:
                raise ValueError('runner reported skip: ' + str(package) + ' ' + str(test))
            no_test_packages.add(package)
        if action == 'run' and test:
            ran.add((package, test))
        if action == 'pass':
            if test:
                passed.add((package, test))
            else:
                package_passed.add(package)
    missing = wanted - (ran & passed)
    if missing:
        raise ValueError('missing executed passing tests: ' + repr(sorted(missing)))
    if any(package in no_test_packages for package, _ in ran):
        raise ValueError('package reports no test files despite named test execution')
    unclosed = ran - passed
    if unclosed:
        raise ValueError('test run without final pass: ' + repr(sorted(unclosed)))
    if any(package not in package_passed for package, _ in wanted):
        raise ValueError('missing final package pass')
    return {'executed_named_tests': len(ran), 'expected_top_level_tests': len(wanted),
            'passing_packages': sorted(package_passed), 'no_test_packages': sorted(no_test_packages)}


def validate_vitest(report, root, expected_files):
    if report.get('success') is not True or report.get('numTotalTests', 0) <= 0:
        raise ValueError('Vitest reports failure or no tests')
    for count in ('numFailedTests', 'numPendingTests', 'numTodoTests', 'numFailedTestSuites', 'numPendingTestSuites'):
        if report.get(count, 0) != 0:
            raise ValueError('Vitest nonzero ' + count)
    seen, assertions = set(), 0
    for suite in report.get('testResults', []):
        if suite.get('status') != 'passed':
            raise ValueError('suite did not pass: ' + str(suite.get('name')))
        path = Path(suite['name'])
        if not path.is_absolute():
            path = root / 'client/apps/web' / path
        seen.add(path.resolve())
        entries = suite.get('assertionResults', [])
        if not entries:
            raise ValueError('empty Vitest suite')
        for assertion in entries:
            if assertion.get('status') != 'passed':
                raise ValueError('Vitest assertion failed/skipped/pending')
            assertions += 1
    wanted = {(root / f['path']).resolve() for f in expected_files}
    if not wanted or wanted - seen:
        raise ValueError('missing expected Vitest source files: ' + repr(sorted(str(x) for x in wanted - seen)))
    if assertions != report['numTotalTests'] or report.get('numPassedTests') != assertions:
        raise ValueError('inconsistent Vitest test totals')
    return {'executed_assertions': assertions, 'executed_files': len(seen)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('inventory', 'discover', 'verify-go-json', 'verify-vitest-json', 'run-go', 'run-vitest'))
    parser.add_argument('--root', type=Path, default=ROOT)
    parser.add_argument('--module', choices=MODULES)
    parser.add_argument('--input', type=Path)
    parser.add_argument('--timeout', type=int, default=120)
    parser.add_argument('--log', type=Path, help='Required native stdout log for explicit run modes')
    args = parser.parse_args()
    root = args.root.resolve()
    result = {'schema_version': 1, 'status': 'failed', 'mode': args.mode}
    try:
        catalog = inventory(root)
        if args.mode == 'inventory':
            # Inventory is observation only: issues intentionally make the gate fail.
            result.update(catalog)
            if catalog['issues']:
                raise ValueError('; '.join(catalog['issues']))
        elif args.mode == 'discover':
            if not args.module:
                raise ValueError('--module required')
            manifest = root / args.module / 'go.mod'
            if not manifest.is_file():
                raise ValueError('missing module manifest')
            env = dict(os.environ, GOWORK='off', GOMAXPROCS='2')
            command = ['go', 'list', '-json', './...']
            process = subprocess.run(command, cwd=manifest.parent, env=env, capture_output=True,
                                     text=True, timeout=args.timeout, check=False)
            result.update(command=command, cwd=str(manifest.parent), exit_code=process.returncode,
                          stdout=process.stdout, stderr=process.stderr)
            if process.returncode or not process.stdout.strip():
                raise ValueError('go list failed or discovered no packages')
            decoder, remaining, packages = json.JSONDecoder(), process.stdout, []
            while remaining.strip():
                item, end = decoder.raw_decode(remaining.lstrip())
                remaining = remaining.lstrip()[end:]
                if item.get('Error') or item.get('DepsErrors'):
                    raise ValueError('go list package/dependency errors')
                packages.append(item['ImportPath'])
            result['packages'] = packages
        elif args.mode in ('run-go', 'run-vitest'):
            if not args.log:
                raise ValueError('--log required to preserve native runner output')
            if args.mode == 'run-go':
                if not args.module:
                    raise ValueError('--module required')
                module = next(x for x in catalog['go_modules'] if x['path'] == args.module)
                if not module['manifest_exists'] or not module['declared_test_count']:
                    raise ValueError('missing module or empty Go suite')
                command = ['go', 'test', '-p', '1', '-count=1', '-json', './...']
                cwd = root / args.module
                env = dict(os.environ, GOWORK='off', GOMAXPROCS='2')
            else:
                command = ['pnpm', 'exec', 'vitest', 'run', '--reporter=json']
                cwd = root / 'client/apps/web'
                env = dict(os.environ)
            args.log.parent.mkdir(parents=True, exist_ok=True)
            error_log = args.log.with_name(args.log.name + '.stderr')
            result.update(command=command, cwd=str(cwd), stdout_log=str(args.log), stderr_log=str(error_log))
            with args.log.open('w') as stdout, error_log.open('w') as stderr:
                process = subprocess.run(command, cwd=cwd, env=env, stdout=stdout, stderr=stderr,
                                         timeout=args.timeout, check=False)
            result['exit_code'] = process.returncode
            result['stdout_sha256'] = sha(args.log)
            result['stderr_sha256'] = sha(error_log)
            if process.returncode:
                raise ValueError('native runner exited nonzero: ' + str(process.returncode))
            if args.mode == 'run-go':
                events = [json.loads(line) for line in args.log.read_text().splitlines() if line.strip()]
                result.update(validate_go(events, module['files']))
            else:
                result.update(validate_vitest(json.loads(args.log.read_text()), root, catalog['web_files']))
        elif args.mode == 'verify-go-json':
            if not args.module or not args.input:
                raise ValueError('--module and --input required')
            module = next(x for x in catalog['go_modules'] if x['path'] == args.module)
            if not module['manifest_exists']:
                raise ValueError('missing module manifest')
            events = [json.loads(line) for line in args.input.read_text().splitlines() if line.strip()]
            result.update(validate_go(events, module['files']))
        else:
            if not args.input:
                raise ValueError('--input required')
            result.update(validate_vitest(json.loads(args.input.read_text()), root, catalog['web_files']))
        result['status'] = 'passed' if args.mode != 'inventory' else 'inventoried'
        print(json.dumps(result, indent=2))
        return 0
    except (ValueError, OSError, KeyError, subprocess.TimeoutExpired) as exc:
        result['error'] = str(exc)
        print(json.dumps(result, indent=2))
        return 2


if __name__ == '__main__':
    sys.exit(main())
