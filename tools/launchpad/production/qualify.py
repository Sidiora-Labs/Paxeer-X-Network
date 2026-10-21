#!/usr/bin/env python3
"""Execute registered qualification checks and retain evidence; never change task status."""
from __future__ import annotations

import argparse
from concurrent.futures import ThreadPoolExecutor, wait, FIRST_COMPLETED
from datetime import datetime, timezone
import hashlib
import importlib
import stat
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
import threading
import tomllib

SCHEMA_VERSION = 1
IMPLEMENTED_TASKS = {'0.1', '0.2', '0.3'}
DISPATCH_REQUIRED_TARGETS = {'dispatcher': {'scheduler-harness'}}
DISPATCH_SCENARIOS = frozenset({'whole-dag', 'fifty-process-leases', 'concurrent-control-benchmark', 'claim-race', 'expiry-takeover', 'stale-proof', 'overlap-and-path-safety', 'timeout-cancel-restart', 'resource-denial', 'false-success-rejection', 'barriers-and-integration'})
BUILD_TARGETS = {
    'core/indexer', 'core/trading-charts', 'core/stats-workers',
    'core/pnl-tracker', 'core/ranking-algo', 'core/api',
    'media/metadata', 'media/user', 'media/social', 'media/livestream',
    'media/gateway', 'shared', 'protocol', 'client/apps/web',
}
REQUIRED_TARGETS = {
    'build': BUILD_TARGETS,
    'test': BUILD_TARGETS - {'media/livestream'},
    'fixture': {'private-fixtures', 'core-contracts'},
    'baseline': {'source'},
}

INTERFACE_REQUIRED_TARGETS = {
    'interface': {'materialized-catalog', 'deterministic-codegen', 'compatibility', 'accounting-math', 'capability-prerequisites', 'endpoint-examples'},
    'build': {'generated-go', 'generated-typescript', 'client/packages', 'client/apps/web'},
    'test': {'generated-go', 'generated-typescript', 'client/apps/web', 'image-dependencies', 'image-standalone', 'client-deployment-guards'},
    'fixture': {'real-contract-fixtures', 'postgres-privileges'},
    'baseline': {'deployed-contract-identity', 'migration-ownership', 'client-deployment-binding'},
}

IGNORED_DIRS = {
    '.git', '.codegraph', 'node_modules', '.next', '.gradle', '.turbo', '.cache', '__pycache__',
    '.pytest_cache', 'coverage', 'evidence', 'playwright-report', 'test-results', 'dist',
}
IGNORED_FILES = {'coverage.out', '.coverage', '.DS_Store', 'tsconfig.tsbuildinfo'}


class QualificationError(ValueError):
    pass


class QualificationCancelled(QualificationError):
    pass


class ExecutionControl:
    def __init__(self):
        self.cancelled = threading.Event()
        self.reason = None
        self.started_epoch = time.time()
        self.started_at = datetime.fromtimestamp(self.started_epoch, timezone.utc).isoformat()

    def cancel(self, reason):
        self.reason = self.reason or reason
        self.cancelled.set()


def terminate_group(process):
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        pass
    try:
        return process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        return process.wait(timeout=2)


def canonical_hash(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def file_hash(path):
    digest = hashlib.sha256()
    with path.open('rb') as source:
        while block := source.read(1024 * 1024):
            digest.update(block)
    return digest.hexdigest()


def ignored_source_directory(relative):
    parts = Path(relative).parts
    if parts[:2] == ('shared', 'secrets') and len(parts) > 2 and (len(parts) != 3 or not parts[-1].endswith('.go')):
        return True
    return any(part in IGNORED_DIRS and not (index == 1 and ((part == "coverage" and parts[0] == "protocol") or (part == "secrets" and parts[0] == "shared"))) for index, part in enumerate(parts))


def snapshot(root):
    files = {}
    for directory, dirs, names in os.walk(root, followlinks=False):
        for name in dirs:
            path = Path(directory) / name
            if path.is_symlink() and not ignored_source_directory(path.relative_to(root)):
                files[path.relative_to(root).as_posix()] = 'symlink:' + os.readlink(path)
        dirs[:] = sorted(name for name in dirs if not ignored_source_directory((Path(directory) / name).relative_to(root)) and not (Path(directory) / name).is_symlink())
        for name in sorted(names):
            if name in IGNORED_FILES or name.endswith(('.pyc', '.prof', '.test', '.tsbuildinfo')) or name == '.env' or name.startswith('.env.'):
                continue
            path = Path(directory) / name
            relative = path.relative_to(root).as_posix()
            if ignored_source_directory(relative):
                continue
            if path.is_symlink():
                files[relative] = 'symlink:' + os.readlink(path)
            elif path.is_file():
                files[relative] = file_hash(path)
    return {'sha256': canonical_hash(files), 'files': files}


def task_at(spec, identifier):
    node = spec.get('task', {})
    for part in identifier.split('.'):
        if not isinstance(node, dict) or part not in node:
            raise QualificationError(f'task {identifier} is absent from specification')
        node = node[part]
    if not isinstance(node, dict) or 'title' not in node:
        raise QualificationError(f'task {identifier} is not a task table')
    return node


def validate_registry(registry, task_id, root):
    if registry.get('schema_version') != SCHEMA_VERSION:
        raise QualificationError('registry schema_version must equal1')
    entry = registry.get('tasks', {}).get(task_id)
    if not isinstance(entry, dict):
        raise QualificationError(f'task {task_id} has no registered checks')
    checks = entry.get('checks')
    if not isinstance(checks, list) or not checks:
        raise QualificationError('registered check list must be nonempty')
    policy = REQUIRED_TARGETS if task_id == '0.1' else INTERFACE_REQUIRED_TARGETS if task_id == '0.2' else DISPATCH_REQUIRED_TARGETS if task_id == '0.3' else None
    if policy is None:
        raise QualificationError(f'task {task_id} qualification is not implemented')
    seen, coverage = set(), {kind: set() for kind in policy}
    normalized = []
    for check in checks:
        if not isinstance(check, dict):
            raise QualificationError('every check must be an object')
        check_id = check.get('id')
        if not isinstance(check_id, str) or not re.fullmatch(r'[A-Za-z0-9_.-]+', check_id) or check_id in seen:
            raise QualificationError('check IDs must be unique safe strings')
        seen.add(check_id)
        if check.get('required') is not True or check.get('skip') or check.get('enabled') is False:
            raise QualificationError(f'{check_id}: skipped or optional checks cannot qualify baseline')
        if check.get('evidence_format') not in {None, 'node-tap', 'dispatcher-json'}:
            raise QualificationError(f'{check_id}: unsupported evidence_format')
        kind, target = check.get('kind'), check.get('target')
        if kind not in policy or not isinstance(target, str):
            raise QualificationError(f'{check_id}: unknown kind or missing target')
        argv = check.get('argv')
        if not isinstance(argv, list) or not argv or not all(isinstance(part, str) and part and '\x00' not in part for part in argv):
            raise QualificationError(f'{check_id}: argv must be a nonempty string array')
        if task_id == '0.3':
            if kind != 'dispatcher' or target != 'scheduler-harness' or check.get('evidence_format') != 'dispatcher-json' or argv != ['python3', 'tools/production/dispatcher/qualification.py', '--spec', 'spec/production-system/spec.kvx', '--plan', 'spec/production-system/execution-plan.json', '--state-dir', '{evidence_dir}/dispatcher-state', '--evidence-dir', '{evidence_dir}/dispatcher', '--case', 'all']:
                raise QualificationError('task0.3 requires the actual registered full dispatcher harness')
            if not (root / argv[1]).is_file():
                raise QualificationError('dispatcher qualification checker is absent')
        elif check.get('evidence_format') == 'dispatcher-json':
            raise QualificationError('dispatcher evidence format is restricted to task0.3')
        cwd = check.get('cwd', '.')
        if not isinstance(cwd, str) or Path(cwd).is_absolute():
            raise QualificationError(f'{check_id}: cwd must be relative to source root')
        resolved_cwd = (root / cwd).resolve()
        if not resolved_cwd.is_relative_to(root) or not resolved_cwd.is_dir():
            raise QualificationError(f'{check_id}: cwd missing or outside source root')
        timeout = check.get('timeout_seconds', 600)
        if not isinstance(timeout, int) or isinstance(timeout, bool) or not 1 <= timeout <= 7200:
            raise QualificationError(f'{check_id}: timeout must be1..7200seconds')
        env = check.get('env', {})
        if not isinstance(env, dict) or not all(isinstance(k, str) and isinstance(v, str) for k, v in env.items()):
            raise QualificationError(f'{check_id}: env must map strings to strings')
        coverage[kind].add(target)
        covers = check.get('covers', {})
        if covers:
            permitted = (task_id == '0.2' and kind == 'fixture' and target == 'postgres-privileges' and argv[:2] == ['python3', 'tools/production/interfaces/persistence_check.py'] and covers == {'baseline': ['migration-ownership']})
            if not permitted:
                raise QualificationError(f'{check_id}: unapproved multi-obligation checker mapping')
            coverage['baseline'].add('migration-ownership')
        normalized.append({**check, 'cwd': cwd, 'timeout_seconds': timeout, 'env': env})
    required_targets = {kind: set(targets) for kind, targets in policy.items()}
    package_root = root / 'client/packages'
    if task_id == '0.1' and package_root.is_dir():
        required_targets['build'].add('client/packages')
        for manifest in package_root.glob('*/package.json'):
            package = json.loads(manifest.read_text())
            if package.get('scripts', {}).get('test'):
                required_targets['test'].add(manifest.parent.relative_to(root).as_posix())
    for kind, targets in required_targets.items():
        missing = targets - coverage[kind]
        if missing:
            raise QualificationError(f'missing required {kind} targets: {sorted(missing)}')
    if task_id == '0.3' and len(normalized) != 1:
        raise QualificationError('task0.3 requires exactly one complete harness execution')
    if task_id in {'0.2', '0.3'}:
        interface_bundle(entry.get('interface_bundle'), root)
    return normalized


def interface_bundle(bundle, root, verify_expected=True):
    if not isinstance(bundle, dict):
        raise QualificationError('task0.2 requires frozen interface_bundle metadata')
    manifests = {}
    for category in ('inputs', 'outputs'):
        patterns = bundle.get(category)
        if not isinstance(patterns, list) or not patterns:
            raise QualificationError(f'interface bundle {category} must be nonempty')
        hashes = {}
        for pattern in patterns:
            if not isinstance(pattern, str) or Path(pattern).is_absolute() or '..' in Path(pattern).parts:
                raise QualificationError('unsafe interface bundle path')
            matches = sorted(root.glob(pattern))
            if not matches:
                raise QualificationError(f'interface bundle {category} path absent: {pattern}')
            files = []
            for match in matches:
                files.extend(sorted(match.rglob('*')) if match.is_dir() else [match])
            selected = [path for path in files if path.is_file()
                        and not any(part in IGNORED_DIRS for part in path.relative_to(root).parts)
                        and path.name not in IGNORED_FILES
                        and not path.name.endswith(('.pyc', '.prof', '.test', '.tsbuildinfo'))]
            if not selected:
                raise QualificationError(f'interface bundle {category} path empty: {pattern}')
            for path in selected:
                relative = path.relative_to(root).as_posix()
                if not path.resolve().is_relative_to(root) or path.is_symlink() or any(part in IGNORED_DIRS or part == '.env' or part.startswith('.env.') for part in path.relative_to(root).parts):
                    raise QualificationError(f'interface bundle path excluded or unsafe: {relative}')
                hashes[relative] = file_hash(path)
        manifests[category] = hashes
    if set(manifests['inputs']) & set(manifests['outputs']):
        raise QualificationError('interface bundle inputs and outputs must be disjoint')
    digest = canonical_hash(manifests)
    expected = bundle.get('expected_sha256')
    if verify_expected and (not isinstance(expected, str) or not re.fullmatch('[0-9a-f]{64}', expected)):
        raise QualificationError('interface bundle requires frozen expected_sha256')
    if verify_expected and digest != expected:
        raise QualificationError(f'interface bundle digest stale; observed {digest}')
    return {**manifests, 'sha256': digest}


def dispatcher_evidence(evidence, control):
    """Validate fresh native harness proof; fixture tests of this parser prove no scenarios."""
    directory = evidence / 'dispatcher'
    path = directory / 'result.json'
    if path.is_symlink() or not path.is_file() or path.stat().st_mtime + 1 < control.started_epoch:
        raise QualificationError('dispatcher evidence absent or stale')
    document = json.loads(path.read_text())
    if not isinstance(document, dict):
        raise QualificationError('dispatcher evidence must be an object')
    if document.get('schema_version') != 1 or document.get('task') != '0.3' or document.get('status') != 'passed':
        raise QualificationError('dispatcher evidence identity/status invalid')
    expected = getattr(control, 'candidate_sha256', None)
    if not expected or document.get('source_sha256') != expected:
        raise QualificationError('dispatcher source identity differs from current candidate')
    observed = datetime.fromisoformat(document['started_at'].replace('Z', '+00:00'))
    # Compare serialized UTC timestamps at the same microsecond precision.
    # started_epoch retains sub-microsecond bits which ISO serialization cannot carry.
    anchor = datetime.fromisoformat(control.started_at)
    if observed.tzinfo is None or not anchor.timestamp() <= observed.timestamp() <= time.time() + 1:
        raise QualificationError('dispatcher start timestamp stale or invalid')
    scenarios = document.get('scenarios')
    if not isinstance(scenarios, list) or len(scenarios) != len(DISPATCH_SCENARIOS):
        raise QualificationError('dispatcher scenario inventory incomplete')
    seen = set()
    for scenario in scenarios:
        if not isinstance(scenario, dict):
            raise QualificationError('dispatcher scenario must be an object')
        identifier = scenario.get('id')
        if identifier not in DISPATCH_SCENARIOS or identifier in seen:
            raise QualificationError('dispatcher scenario unknown or duplicated')
        seen.add(identifier)
        count = scenario.get('checks')
        duration = scenario.get('duration_seconds')
        if scenario.get('status') != 'passed' or type(count) is not int or count <= 0 or type(duration) not in (int, float) or not 0 <= duration < float('inf'):
            raise QualificationError('dispatcher scenario empty, failed or invalid')
        artifacts = scenario.get('artifacts')
        if not isinstance(artifacts, dict) or not artifacts:
            raise QualificationError('dispatcher scenario artifacts absent')
        for relative, digest in artifacts.items():
            if not isinstance(relative, str) or Path(relative).is_absolute() or '..' in Path(relative).parts:
                raise QualificationError('dispatcher artifact path unsafe')
            artifact = directory / relative
            if artifact.is_symlink() or not artifact.resolve().is_relative_to(directory.resolve()) or not artifact.is_file() or not isinstance(digest, str) or not re.fullmatch('[0-9a-f]{64}', digest) or file_hash(artifact) != digest:
                raise QualificationError('dispatcher artifact missing or hash differs')
        if identifier == 'whole-dag' and duration >= 30:
            raise QualificationError('whole graph qualification exceeded 30 seconds')
        if identifier == 'fifty-process-leases':
            processes = scenario.get('processes')
            if not isinstance(processes, list) or len(processes) != 50:
                raise QualificationError('fifty actual process records required')
            if not all(isinstance(record, dict) for record in processes):
                raise QualificationError('dispatcher process record must be an object')
            for field in ('pid', 'lane', 'attempt', 'workspace'):
                values = [record.get(field) for record in processes]
                if any(value is None or value == '' for value in values) or len(set(values)) != 50:
                    raise QualificationError('process identities must be distinct and nonempty')
            if {record['lane'] for record in processes} != {f'L{i:02}' for i in range(1, 51)}:
                raise QualificationError('process records must cover all fifty lanes')
            for record in processes:
                if any(type(record.get(field)) is not int or record[field] <= 0 for field in ('pid', 'fence', 'heartbeat_count')):
                    raise QualificationError('process lease or heartbeat evidence invalid')
                if any(type(record.get(field)) not in (int, float) or not 0 <= record[field] < float('inf') for field in ('started_monotonic', 'ended_monotonic')) or record['ended_monotonic'] <= record['started_monotonic']:
                    raise QualificationError('process lifetime evidence invalid')
            overlap = min(record['ended_monotonic'] for record in processes) - max(record['started_monotonic'] for record in processes)
            if overlap < 1 or type(scenario.get('overlap_seconds')) not in (int, float) or not 1 <= scenario['overlap_seconds'] <= overlap + 0.001:
                raise QualificationError('fifty simultaneous process lease overlap not proven')
    return {'scenario_count': len(seen), 'check_count': sum(s['checks'] for s in scenarios), 'result_sha256': file_hash(path), 'result': str(path)}


def expand_tokens(value, root, evidence, contract_root, started_at=None):
    for token, replacement in {'{source_root}': str(root), '{evidence_dir}': str(evidence), '{contract_root}': str(contract_root)}.items():
        value = value.replace(token, replacement)
    if started_at is not None:
        value = value.replace('{qualification_started_at}', started_at)
    return value


def execute_check(check, root, evidence, contract_root=None, control=None):
    control = control or ExecutionControl()
    contract_root = contract_root or root.parent / 'contracts'
    check = {**check, 'argv': [expand_tokens(value, root, evidence, contract_root, control.started_at) for value in check['argv']],
             'env': {key: expand_tokens(value, root, evidence, contract_root, control.started_at) for key, value in check['env'].items()}}
    log = evidence / f'{check["id"]}.log'
    started = datetime.now(timezone.utc).isoformat()
    before = time.monotonic()
    result = {'id': check['id'], 'kind': check['kind'], 'target': check['target'],
              'argv': check['argv'], 'cwd': check['cwd'], 'started_at': started,
              'timeout_seconds': check['timeout_seconds'], 'log': str(log),
              'timed_out': False, 'passed': False}
    environment = {**os.environ, **check['env'],
                   'QUALIFICATION_EVIDENCE_DIR': str(evidence),
                   'QUALIFICATION_SOURCE_ROOT': str(root),
                   'QUALIFICATION_CONTRACT_ROOT': str(contract_root),
                   'QUALIFICATION_STARTED_AT': control.started_at,
                   'QUALIFICATION_STARTED_EPOCH': str(control.started_epoch),
                   'QUALIFICATION_CANDIDATE_SHA256': getattr(control, 'candidate_sha256', '')}
    # Environment values are never copied into evidence or console output.
    process = None
    with log.open('wb') as output:
        try:
            if control.cancelled.is_set():
                result.update(exit_code=None, cancelled=True, not_started=True)
            else:
                process = subprocess.Popen(check['argv'], cwd=root / check['cwd'],
                                           env=environment, stdout=output, stderr=subprocess.STDOUT,
                                           start_new_session=True)
                deadline = time.monotonic() + check['timeout_seconds']
                while True:
                    if control.cancelled.is_set():
                        result.update(cancelled=True, exit_code=terminate_group(process))
                        break
                    remaining = deadline - time.monotonic()
                    if remaining <= 0:
                        result.update(timed_out=True, exit_code=terminate_group(process))
                        break
                    try:
                        result['exit_code'] = process.wait(timeout=min(remaining, 0.2))
                        break
                    except subprocess.TimeoutExpired:
                        continue
                result['passed'] = result['exit_code'] == 0 and not result['timed_out'] and not result.get('cancelled', False)
        except OSError as exc:
            result['exit_code'] = None
            result['launch_error'] = f'{type(exc).__name__}: {exc}'
        except BaseException:
            if process is not None:
                terminate_group(process)
            raise
    if result['passed'] and check.get('evidence_format') == 'node-tap':
        output = log.read_text(errors='replace')
        totals = {}
        for key in ('tests', 'pass', 'fail', 'skipped', 'todo', 'cancelled'):
            found = re.findall(r'^# ' + key + r' (\d+)\s*$', output, flags=re.MULTILINE)
            totals[key] = int(found[-1]) if found else None
        result['native_test_totals'] = totals
        result['passed'] = (isinstance(totals['tests'], int) and totals['tests'] > 0 and totals['pass'] == totals['tests'] and all(totals[key] == 0 for key in ('fail', 'skipped', 'todo', 'cancelled')))
        if not result['passed']:
            result['evidence_error'] = 'native Node suite empty, skipped, failed, cancelled, todo or missing complete TAP totals'
    if result['passed'] and check.get('evidence_format') == 'dispatcher-json':
        try:
            result['dispatcher_evidence'] = dispatcher_evidence(evidence, control)
        except (QualificationError, OSError, ValueError, KeyError, TypeError, ImportError, AttributeError) as exc:
            result['passed'] = False
            result['evidence_error'] = f'{type(exc).__name__}: {exc}'
    result['duration_seconds'] = round(time.monotonic() - before, 6)
    result['log_sha256'] = file_hash(log)
    return result


def parallel_go_test(check):
    return check['kind'] == 'test' and check['target'] in BUILD_TARGETS - {'client/apps/web'}


def execute_checks(checks, root, evidence, contract_root, on_result, control):
    """Only contiguous Go suites overlap; coordinator publishes results in source order."""
    index = 0
    while index < len(checks):
        if control.cancelled.is_set():
            raise QualificationCancelled(control.reason or 'qualification cancelled')
        if not parallel_go_test(checks[index]):
            observed = execute_check(checks[index], root, evidence, contract_root, control)
            on_result(index, observed)
            index += 1
            continue
        end = index
        while end < len(checks) and parallel_go_test(checks[end]):
            end += 1
        pool = ThreadPoolExecutor(max_workers=2, thread_name_prefix='qualification-go')
        active, next_index = {}, index
        try:
            while active or next_index < end:
                while not control.cancelled.is_set() and len(active) < 2 and next_index < end:
                    future = pool.submit(execute_check, checks[next_index], root, evidence, contract_root, control)
                    active[future] = next_index
                    next_index += 1
                if not active:
                    break
                completed, _ = wait(active, timeout=0.2, return_when=FIRST_COMPLETED)
                for future in sorted(completed, key=lambda f: active[f]):
                    position = active.pop(future)
                    on_result(position, future.result())
        except BaseException:
            control.cancel('qualification coordinator interrupted')
            for future, position in sorted(active.items(), key=lambda pair: pair[1]):
                try:
                    on_result(position, future.result(timeout=9))
                except Exception:
                    pass
            raise
        finally:
            pool.shutdown(wait=True, cancel_futures=True)
        index = end
    if control.cancelled.is_set():
        raise QualificationCancelled(control.reason or 'qualification cancelled')


def qualify_accepted_candidate(entry, task_id, task, spec, root, evidence):
    """Consume coordinator acceptance; never pretend archived lane code is integrated."""
    if task.get('kind') != 'parallel_lane':
        raise QualificationError('only explicitly reviewed parallel lane policies are supported')
    policy = entry.get('accepted_candidate_policy')
    if not isinstance(policy, dict) or entry.get('policy_sha256') != canonical_hash(policy):
        raise QualificationError('reviewed accepted-candidate policy absent or hash invalid')
    expected_scope = {'task': task_id, 'lane': task.get('lane'), 'phase': task.get('phase'),
                      'reqs': task.get('reqs'), 'owned': task.get('touches')}
    if any(policy.get(key) != value for key, value in expected_scope.items()):
        raise QualificationError('accepted-candidate policy scope differs from current task')
    checks = policy.get('required_checks')
    if not isinstance(checks, list) or not checks or not all(isinstance(check, dict) and check.get('proof') for check in checks):
        raise QualificationError('accepted candidate requires reviewed native proof checks')
    ids = [check.get('id') for check in checks]
    if any(not isinstance(identifier, str) or not identifier for identifier in ids) or len(set(ids)) != len(ids):
        raise QualificationError('accepted check IDs empty or duplicate')
    for field in ('baseline_sha256', 'interface_sha256', 'layout_sha256'):
        if not isinstance(policy.get(field), str) or not re.fullmatch('[0-9a-f]{64}', policy[field]):
            raise QualificationError('accepted policy digest invalid: ' + field)
    frozen = interface_bundle(entry.get('interface_bundle'), root)
    if frozen['sha256'] != policy['interface_sha256']:
        raise QualificationError('accepted policy frozen interface differs')
    configured = entry.get('coordinator_state_dir')
    if not isinstance(configured, str) or not Path(configured).is_absolute():
        raise QualificationError('coordinator state path must be pinned absolute path')
    state = Path(configured)
    if state.is_symlink() or not state.is_dir() or state.resolve().is_relative_to(root):
        raise QualificationError('coordinator state must be private and outside source')
    for path, permissions in ((state, 0o700), (state / 'coordinator.key', 0o600)):
        if path.is_symlink() or stat.S_IMODE(path.stat().st_mode) != permissions or path.stat().st_uid != os.getuid():
            raise QualificationError('coordinator state/key ownership or permissions invalid')
    identifier = os.environ.get('QUALIFICATION_ACCEPTANCE_ID', '')
    if not re.fullmatch('[A-Za-z0-9][A-Za-z0-9_.-]*', identifier) or identifier in {'.', '..'}:
        raise QualificationError('trusted acceptance ID absent or unsafe')
    if identifier != entry.get('accepted_artifact_id'):
        raise QualificationError('acceptance ID differs from coordinator selected artifact')
    config = json.loads((state / 'config.json').read_text())
    if config.get('fixture') is True:
        raise QualificationError('fixture dispatcher state cannot qualify production lane tasks')
    if config.get('interface') != policy['interface_sha256'] or config.get('layout') != policy['layout_sha256']:
        raise QualificationError('coordinator frozen configuration differs from reviewed policy')
    registry_file = Path(config['registry'])
    if registry_file.is_symlink() or not registry_file.resolve().is_relative_to(state.resolve()):
        raise QualificationError('coordinator check registry outside pinned private state')
    state_files = [state / 'config.json', state / 'coordinator.key', registry_file]
    state_hashes = {str(path): file_hash(path) for path in state_files}
    check_registry = json.loads(registry_file.read_text())
    if check_registry.get(task_id) != checks:
        raise QualificationError('current coordinator check registry differs from reviewed policy')
    sys.path.insert(0, str(root / 'tools/production'))
    try:
        acceptance = importlib.import_module('dispatcher.acceptance')
        graph_module = importlib.import_module('dispatcher.graph')
        graph = graph_module.Graph(root / 'spec/production-system/spec.kvx', root / 'spec/production-system/execution-plan.json')
        if graph.layout_digest != policy['layout_sha256']:
            raise QualificationError('current task layout differs from reviewed policy')
        store_module = importlib.import_module('dispatcher.store')
        store = store_module.Store(config['db'], config['capacity'], boot_id=config['boot'], mode=config['mode'], total_slots=config['total_slots'])
        try:
            verified = acceptance.verify_accepted(
                baseline=Path(config['baseline']), accepted_path=state / 'accepted' / identifier,
                owned=policy['owned'], checks=checks, secret=(state / 'coordinator.key').read_bytes(),
                output_dir=evidence / 'accepted-candidate-source', store=store)
        finally:
            store.close()
    finally:
        sys.path.pop(0)
    if not isinstance(verified, dict) or verified.get('individually_verified') is not True or verified.get('combined_qualified') is not False or verified.get('task') != task_id or verified.get('attempt') != identifier:
        raise QualificationError('coordinator accepted artifact verification failed')
    if verified.get('lane') != policy['lane']:
        raise QualificationError('accepted artifact belongs to another lane')
    proofs = verified.get('proofs')
    if not isinstance(proofs, dict) or set(proofs) != set(ids) or any(not isinstance(proof, dict) or type(proof.get('positive_count')) is not int or proof['positive_count'] <= 0 for proof in proofs.values()):
        raise QualificationError('accepted native proof inventory empty or invalid')
    if any(file_hash(path) != state_hashes[str(path)] for path in state_files):
        raise QualificationError('private coordinator policy changed during verification')
    expected_bindings = {'baseline': policy['baseline_sha256'], 'interface': policy['interface_sha256'], 'layout': policy['layout_sha256']}
    if any(verified.get('bindings', {}).get(key) != digest for key, digest in expected_bindings.items()) or verified.get('owned') != policy['owned'] or verified.get('checks') != checks:
        raise QualificationError('accepted artifact differs from reviewed scope or frozen bindings')
    (evidence / 'accepted-candidate.json').write_text(json.dumps(verified, indent=2) + '\n')
    return {'qualified': True, 'qualification_scope': 'isolated-candidate-qualified',
            'source_scope': 'isolated_candidate', 'canonical_integrated': False,
            'acceptance_id': identifier, 'policy_sha256': entry['policy_sha256'],
            'checks': [{'id': ident, 'passed': True, 'native_proof': proofs[ident], 'source_scope': 'isolated_candidate'} for ident in sorted(ids)],
            'interface_bundle_sha256': frozen['sha256'], 'accepted_candidate': verified,
            'candidate_sha256': verified['bindings']['candidate'],
            'candidate_hash_algorithm': 'qualification-source-v1', 'require_current_source': True}


def run(args, control=None):
    control = control or ExecutionControl()
    root = Path(__file__).resolve().parents[2]
    spec_path = (root / args.spec).resolve() if not args.spec.is_absolute() else args.spec.resolve()
    registry_path = (root / args.registry).resolve() if not args.registry.is_absolute() else args.registry.resolve()
    evidence_base = (root / args.evidence_dir).resolve() if not args.evidence_dir.is_absolute() else args.evidence_dir.resolve()
    if not evidence_base.is_relative_to(root / 'evidence'):
        raise QualificationError('evidence directory must be inside source-root/evidence')
    if not re.fullmatch(r'\d+(?:\.\d+)+', args.task):
        raise QualificationError('task must be a numeric dotted identifier')
    evidence = evidence_base / args.task / f'{time.time_ns()}'
    evidence.mkdir(parents=True, mode=0o700)
    os.chmod(evidence, 0o700)
    result = {'schema_version': 1, 'task': args.task, 'qualified': False,
              'status_mutated': False, 'checks': [], 'errors': [],
              'started_at': control.started_at,
              'evidence_directory': str(evidence)}
    try:
        if args.task not in IMPLEMENTED_TASKS:
            preview = json.loads(registry_path.read_text()) if registry_path.is_file() else {}
            reviewed = preview.get('tasks', {}).get(args.task)
            if not isinstance(reviewed, dict) or not any(isinstance(reviewed.get(key), dict) for key in ('accepted_candidate_policy', 'serial_gate_policy')):
                raise QualificationError(f'task {args.task} qualification is not implemented')
        spec_bytes = spec_path.read_bytes()
        spec = tomllib.loads(spec_bytes.decode())
        if spec.get('meta', {}).get('feature') != 'production-system':
            raise QualificationError('specification feature must be production-system')
        task = task_at(spec, args.task)
        allowed_statuses = {'in_progress', 'done'} if args.task in IMPLEMENTED_TASKS else {'in_progress', 'implemented', 'done'}
        if task.get('status') not in allowed_statuses:
            raise QualificationError('task must be claimed before qualification')
        for dependency in task.get('requires', []):
            if task_at(spec, dependency).get('status') != 'done':
                raise QualificationError(f'dependency {dependency} is not done')
        registry_bytes = registry_path.read_bytes()
        registry = json.loads(registry_bytes)
        if args.task not in IMPLEMENTED_TASKS:
            trusted_registry = root / 'tools/production/bootstrap/check-registry.json'
            if registry_path != trusted_registry or spec_path != root / 'spec/production-system/spec.kvx':
                raise QualificationError('future task requires canonical reviewed registry and specification')
            entry = registry.get('tasks', {}).get(args.task)
            if registry.get('schema_version') != SCHEMA_VERSION or not isinstance(entry, dict):
                raise QualificationError(f'task {args.task} qualification is not implemented')
            if isinstance(entry.get('serial_gate_policy'), dict):
                frozen_output = interface_bundle(entry.get('interface_bundle'), root)
                if frozen_output['sha256'] != entry['serial_gate_policy'].get('output_interface_sha256'):
                    raise QualificationError('serial output interface differs from actual current bundle')
            elif entry.get('interface_bundle') != registry.get('tasks', {}).get('0.2', {}).get('interface_bundle'):
                raise QualificationError('future lane must preserve current accepted bootstrap interface bundle')
            before = snapshot(root)
            if args.candidate_sha256 and before['sha256'] != args.candidate_sha256:
                raise QualificationError('coordinator source differs from required current source')
            (evidence / 'source-before.json').write_text(json.dumps(before, indent=2) + '\n')
            if isinstance(entry.get('serial_gate_policy'), dict):
                from serial_gate import qualify_serial_gate
                accepted = qualify_serial_gate(entry, args.task, task, spec, root, evidence, control=control)
            else:
                accepted = qualify_accepted_candidate(entry, args.task, task, spec, root, evidence)
            if control.cancelled.is_set():
                raise QualificationCancelled(control.reason or 'qualification cancelled')
            after = snapshot(root)
            (evidence / 'source-after.json').write_text(json.dumps(after, indent=2) + '\n')
            if before != after or spec_path.read_bytes() != spec_bytes or registry_path.read_bytes() != registry_bytes:
                raise QualificationError('current source or reviewed policy changed during accepted qualification')
            result.update(accepted)
            result.update(spec_sha256=hashlib.sha256(spec_bytes).hexdigest(), registry_sha256=hashlib.sha256(registry_bytes).hexdigest(), coordinator_source_sha256=before['sha256'])
            result['finished_at'] = datetime.now(timezone.utc).isoformat()
            output = evidence / 'result.json'
            output.write_text(json.dumps(result, indent=2) + '\n')
            print(json.dumps({'qualified': True, 'task': args.task, 'scope': result['qualification_scope'], 'result': str(output)}))
            return 0
        checks = validate_registry(registry, args.task, root)
        frozen_interface = None
        if args.task in {'0.2', '0.3'}:
            frozen_interface = interface_bundle(registry['tasks'][args.task].get('interface_bundle'), root)
            (evidence / 'interface-bundle.json').write_text(json.dumps(frozen_interface, indent=2) + '\n')
            catalog_path = root / 'spec/production-system/integration/contracts/v1/catalog-map.json'
            catalog_document = json.loads(catalog_path.read_text())
            catalog_inputs = catalog_document.get('inputs', {}) if isinstance(catalog_document, dict) else {}
            if not isinstance(catalog_inputs, dict):
                raise QualificationError('frozen catalog input identity must be an object')
            semantic_digest = catalog_inputs.get('catalogSha256')
            if catalog_inputs.get('semanticDigestAlgorithm') != 'interface-catalog-v1' or not isinstance(semantic_digest, str) or not re.fullmatch('[0-9a-f]{64}', semantic_digest):
                raise QualificationError('frozen catalog missing valid semantic input identity')
            result['interface_semantic_input'] = {
                'algorithm': catalog_inputs['semanticDigestAlgorithm'],
                'sha256': semantic_digest, 'catalog_manifest_sha256': file_hash(catalog_path)}
        before = snapshot(root)
        control.candidate_sha256 = before['sha256']
        result.update({'spec_sha256': hashlib.sha256(spec_bytes).hexdigest(),
                       'registry_sha256': hashlib.sha256(registry_bytes).hexdigest(),
                       'candidate_sha256': before['sha256'], 'source_file_count': len(before['files']),
                       'require_current_source': True, 'current_source_flag_requested': args.require_current_source})
        (evidence / 'source-before.json').write_text(json.dumps(before, indent=2) + '\n')
        if args.candidate_sha256 and before['sha256'] != args.candidate_sha256:
            raise QualificationError('candidate hash differs from required candidate')
        observations = {}
        def publish(position, observed):
            observations[position] = observed
            result['checks'] = [observations[key] for key in sorted(observations)]
            result['errors'] = [f'check {entry["id"]} failed' for entry in result['checks'] if not entry['passed']]
            (evidence / 'progress.json').write_text(json.dumps(result, indent=2) + '\n')
            print(json.dumps({'check': observed['id'], 'passed': observed['passed'],
                              'duration_seconds': observed['duration_seconds']}), flush=True)
        execute_checks(checks, root, evidence,
                       args.contract_root.resolve() if args.contract_root else root.parent / 'contracts',
                       publish, control)
        if frozen_interface is not None:
            current_interface = interface_bundle(registry['tasks'][args.task].get('interface_bundle'), root)
            result['interface_bundle_sha256'] = frozen_interface['sha256']
            if current_interface != frozen_interface:
                result['errors'].append('interface bundle changed during qualification')
        after = snapshot(root)
        result['candidate_after_sha256'] = after['sha256']
        (evidence / 'source-after.json').write_text(json.dumps(after, indent=2) + '\n')
        changed = sorted(path for path in before['files'].keys() | after['files'].keys()
                         if before['files'].get(path) != after['files'].get(path))
        result['changed_source_paths'] = changed
        if changed:
            result['errors'].append('source changed during qualification; evidence is stale')
        if spec_path.read_bytes() != spec_bytes or registry_path.read_bytes() != registry_bytes:
            result['errors'].append('specification or registry changed during qualification')
        result['qualified'] = not result['errors'] and len(result['checks']) == len(checks)
    except KeyboardInterrupt:
        control.cancel('qualification interrupted by keyboard')
        result['errors'].append(control.reason)
    except (QualificationError, OSError, ValueError, KeyError, TypeError, ImportError, AttributeError) as exc:
        result['errors'].append(f'{type(exc).__name__}: {exc}')
    if control.cancelled.is_set():
        result['qualified'] = False
        result['cancelled'] = True
        result['cancellation_reason'] = control.reason
    result['finished_at'] = datetime.now(timezone.utc).isoformat()
    output = evidence / 'result.json'
    output.write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps({'qualified': result['qualified'], 'task': args.task,
                      'result': str(output), 'errors': result['errors']}, indent=2))
    return 0 if result['qualified'] else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--task', required=True)
    parser.add_argument('--spec', type=Path, required=True)
    parser.add_argument('--require-current-source', action='store_true', required=True)
    parser.add_argument('--registry', type=Path, default=Path('tools/production/bootstrap/check-registry.json'))
    parser.add_argument('--evidence-dir', type=Path, default=Path('evidence/production-system/qualification'))
    parser.add_argument('--candidate-sha256')
    parser.add_argument('--contract-root', type=Path)
    args = parser.parse_args()
    control = ExecutionControl()
    previous_handlers = {}
    for signum in (signal.SIGINT, signal.SIGTERM):
        previous_handlers[signum] = signal.signal(signum, lambda number, frame: control.cancel(f'qualification cancelled by signal{number}'))
    try:
        return run(args, control)
    except (QualificationError, OSError) as exc:
        print(json.dumps({'qualified': False, 'errors': [str(exc)]}), file=sys.stderr)
        return 1
    finally:
        for signum, handler in previous_handlers.items():
            signal.signal(signum, handler)


if __name__ == '__main__':
    sys.exit(main())
