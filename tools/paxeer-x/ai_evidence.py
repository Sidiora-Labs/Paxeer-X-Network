#!/usr/bin/env python3
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time
import unittest

SELECTORS = ('workspace-admission', 'deployment-admission', 'real-services-admission',
             'storage-profile-admission', 'storage-prerequisite-evidence')
PATHS = ('tools/paxeer-x/ai_evidence.py', 'tools/paxeer-x/tests/ai_evidence_test.py')
MAX_BYTES = 4 * 1024 * 1024
SHA = re.compile(r'[0-9a-f]{64}')
REV = re.compile(r'[0-9a-f]{40}')
IDENTITY = {
    'task_id': 'ai.P-WORKSPACE',
    'decision_id': 'px26-fm1-workspace-2026-10-08-01',
    'approved_by': 'px26-leader-delegated-px26-fm1',
}
LIMITS = [
    'Own sanitized proc metadata is visible.',
    'Synthetic tmpfs sibling writes do not create corresponding host siblings.',
    'Nested userns creation succeeds without new mount/chroot or demonstrated host escape.',
    'Parent native controller remains host capable.',
]
READ_SCOPE = [
    '39 enumerated ai-market contract files',
    'embedded canonical AI task/requirement/acceptance packet',
    'existing tools/paxeer-x approved nonsecret style inputs',
    'programs/Cargo.toml', 'programs/sdk/rust/Cargo.toml',
    'this exact private decision manifest',
]
CONTRACT_PATHS = (
    'acceptance/system-journeys.kvx', 'contracts/core.kvx',
    'contracts/integration-map.kvx', 'contracts/operation-registry.json',
    'contracts/storage-lifecycle.kvx', 'contracts/wire.schema.json',
    *tuple('features/F%02d-%s%s' % (number, name, suffix)
           for number, name in enumerate(('markets', 'workers', 'evaluators',
               'commit-reveal', 'aggregation', 'rewards', 'reputation', 'admission',
               'artifacts', 'views-sdk'), 1)
           for suffix in ('.kvx', '.requirements.json')),
    'index.kvx', 'planning/execution.kvx', 'planning/task-graph.json', 'roles.kvx',
    *tuple('schematics/%02d-%s.mmd' % (number, name)
           for number, name in enumerate(('service-boundaries', 'task-and-epoch-lifecycle',
               'participant-authorization', 'score-commit-reveal-aggregation',
               'conserved-rewards', 'evidence-privacy-finalized-reads',
               'storage-archive-retirement', 'implementation-dependency-waves'), 1)),
    'schematics/index.kvx',
)
CAPS = {'committed_blobs': 512, 'committed_bytes': 67108864,
        'staged_new_blobs': 4, 'blob_bytes': 1048576,
        'module_kv': 512, 'staged_writes': 64}
FUTURE_TASKS = {'deployment-admission': 'ai.P-DEPLOYMENT',
                'real-services-admission': 'ai.P-REAL-SERVICES',
                'storage-profile-admission': 'ai.P-STORAGE-PROFILE',
                'storage-prerequisite-evidence': 'ai.T-C04'}
FUTURE_FIELDS = {
    'deployment-admission': ('domain', 'genesis', 'source', 'artifact', 'abi',
        'runtime', 'fees', 'account_registration', 'immutable_asset', 'capabilities',
        'authenticated_height', 'finality', 'proof', 'balance', 'capacity',
        'native_round_trip'),
    'real-services-admission': ('rights', 'artifacts', 'runner', 'evaluator',
        'durable_storage', 'tls', 'tenant', 'kms'),
    'storage-profile-admission': ('version', 'ordinal', 'activation', 'compatibility',
        'controller', 'archive_generations', 'reference_registry', 'hot_horizon',
        'indefinite_retention', 'certificate', 'work', 'fees', 'reserve'),
    'storage-prerequisite-evidence': ('ai.T-S01', 'ai.T-S02', 'ai.T-S03'),
}


class Invalid(ValueError):
    pass


# Only source-controlled labels may cross the diagnostic boundary. Exception
# text, paths, record values and unittest parameter representations stay private.
REASON_LABELS = {
    'invalid CLI arguments': 'cli-arguments',
    'invalid selector': 'selector',
    'invalid JSON': 'json-encoding',
    'duplicate JSON key': 'json-duplicate-key',
    'JSON constant': 'json-constant',
    'record size': 'record-size',
    'record must be object': 'record-type',
    'invalid SHA256': 'sha256-type',
    'invalid revision': 'revision-type',
    'path: string': 'path-string',
    'path: unresolved': 'path-unresolved',
    'path must be canonical absolute': 'path-canonical',
    'secret path forbidden': 'path-forbidden',
    'unsafe path ancestor': 'path-ancestor',
    'unsafe or unavailable path': 'path-unavailable',
    'evidence directory must be private': 'directory-permissions',
    'file ownership or links': 'file-ownership-links',
    'file permissions': 'file-permissions',
    'file size': 'file-size',
    'file changed while reading': 'file-changed',
    'file replaced while reading': 'file-replaced',
    'file read failed': 'file-read',
    'file SHA256 mismatch': 'file-sha256-pin',
    'evidence path escape': 'path-escape',
    'identity command unavailable': 'identity-command-unavailable',
    'identity command failed': 'identity-command-failed',
    'identity command encoding': 'identity-command-encoding',
    'future decision': 'decision-future',
    'contract pin mismatch': 'contract-pin',
    'overlay pin mismatch': 'overlay-pin',
    'stale Git HEAD': 'git-head',
    'stale Git branch': 'git-branch',
    'actual Python must be 3.12.3': 'python-version',
    'actual Python executable mismatch': 'python-executable',
    'empty runtime evidence': 'runtime-empty',
    'missing runtime control': 'runtime-control-absent',
    'validator must execute materialized owned path': 'validator-location',
    'distinct authority files required': 'authority-distinct',
    'admission object differs from bytes': 'admission-object-binding',
    'selector evidence absent': 'selector-evidence-absent',
    'focused tests unavailable': 'tests-unavailable',
    'focused tests incomplete': 'tests-incomplete',
    'focused tests failed': 'tests-failed',
}
for _label in ('admission', 'approval', 'candidate', 'contract', 'toolchain',
               'python', 'rustc', 'cargo', 'dependencies', 'ownership', 'access',
               'evidence', 'model', 'authorization', 'overlay', 'contract pin',
               'overlay pin', 'test overlay'):
    for _suffix in ('fields', 'count', 'paths', 'coverage'):
        REASON_LABELS[_label + ': ' + _suffix] = _label.replace(' ', '-') + '-' + _suffix
for _label in ('task_id', 'decision_id', 'approved_by', 'admission schema',
               'approval schema', 'selector identity', 'decision issuance',
               'manager', 'writer', 'claim', 'fence', 'exclusive paths',
               'exclusive writer', 'approved base', 'approved branch', 'base pin',
               'branch pin', 'application version', 'contract version pin',
               'python version', 'rustc version', 'cargo version', 'rustc release',
               'cargo release', 'targets', 'validator dependencies',
               'new dependencies', 'lock identity', 'lock evidence', 'network',
               'web', 'mcp', 'apps', 'full_sandbox_pass', 'ignore_user_config',
               'ignore_rules', 'ephemeral', 'instruction discovery', 'runtime scope',
               'infrastructure exit', 'infrastructure qualification',
               'confinement limitations', 'read scope', 'write scope',
               'allowed tools', 'policy_sha256 pin', 'runtime_result_sha256 pin',
               'evidence directory pin', 'runtime log pin', 'gate ownership',
               'requested model', 'model evidence limit', 'runtime exit',
               'metadata authority', 'product authority', 'overlay readiness',
               'approval readiness', 'admission reference', 'approval reference',
               'admission bytes pin', 'producer runtime_scope_result',
               'producer full_sandbox_pass', 'producer infrastructure_probe_exit',
               'producer infrastructure_probe_qualification'):
    REASON_LABELS[_label + ': mismatch'] = _label.replace(' ', '-') + '-mismatch'


def refusal_reason(error):
    if type(error) is Invalid:
        if len(error.args) == 1 and type(error.args[0]) is str:
            return REASON_LABELS.get(error.args[0], 'invalid-evidence')
        return 'invalid-evidence'
    for kind, label in ((OSError, 'filesystem-error'), (ImportError, 'import-error'),
                        (RuntimeError, 'runtime-error'), (ValueError, 'value-error')):
        if isinstance(error, kind):
            return label
    return 'test-exception'


def require(condition, message):
    if not condition:
        raise Invalid(message)


class Parser(argparse.ArgumentParser):
    def error(self, message):
        raise Invalid('invalid CLI arguments')


def keys(value, expected, label):
    require(type(value) is dict and set(value) == set(expected), label + ': fields')


def text(value, label, maximum=4096):
    require(type(value) is str and 0 < len(value) <= maximum and
            not any(ord(c) < 32 for c in value), label + ': string')
    require(value.lower() not in ('unknown', 'placeholder', 'pending', 'todo', 'none'),
            label + ': unresolved')
    return value


def integer(value, label, lower=0, upper=2**63 - 1):
    require(type(value) is int and lower <= value <= upper, label + ': integer')


def digest(value):
    require(type(value) is str and SHA.fullmatch(value) is not None, 'invalid SHA256')
    return value


def revision(value):
    require(type(value) is str and REV.fullmatch(value) is not None, 'invalid revision')


def same(value, expected, label):
    require(type(value) is type(expected) and value == expected, label + ': mismatch')


def duplicate_free(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'duplicate JSON key')
        result[key] = value
    return result


def parse(raw):
    require(type(raw) is bytes and 0 < len(raw) <= MAX_BYTES, 'record size')
    try:
        value = json.loads(raw.decode('utf-8'), object_pairs_hook=duplicate_free,
                           parse_constant=lambda _: (_ for _ in ()).throw(Invalid('JSON constant')))
    except (UnicodeError, json.JSONDecodeError, RecursionError):
        raise Invalid('invalid JSON') from None
    require(type(value) is dict, 'record must be object')
    return value


def safe_path(value):
    text(value, 'path')
    path = Path(value)
    require(path.is_absolute() and str(path) == value and
            not any(part in ('.', '..') for part in value.split('/')), 'path must be canonical absolute')
    require('//' not in value, 'path must be canonical absolute')
    for part in path.parts:
        lowered = part.lower()
        require(not (lowered == '.env' or lowered.startswith('.env.') or
                     lowered in ('.ssh', '.aws', '.gnupg', 'credentials', 'secrets') or
                     lowered.endswith(('.pem', '.key'))), 'secret path forbidden')
    return path


def open_path(value, directory=False):
    path = safe_path(str(value))
    fd = os.open('/', os.O_RDONLY | os.O_DIRECTORY)
    try:
        for index, part in enumerate(path.parts[1:]):
            last = index == len(path.parts) - 2
            flags = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK
            if not last or directory:
                flags |= os.O_DIRECTORY
            new_fd = os.open(part, flags, dir_fd=fd)
            os.close(fd)
            fd = new_fd
            info = os.fstat(fd)
            if not last:
                require(stat.S_ISDIR(info.st_mode) and info.st_uid in (0, os.geteuid())
                        and info.st_mode & 0o022 == 0, 'unsafe path ancestor')
        return fd
    except Invalid:
        os.close(fd)
        raise
    except OSError:
        os.close(fd)
        raise Invalid('unsafe or unavailable path') from None


def private_directory(value):
    fd = open_path(value, directory=True)
    try:
        info = os.fstat(fd)
        require(stat.S_ISDIR(info.st_mode) and info.st_uid in (0, os.geteuid())
                and info.st_mode & 0o077 == 0, 'evidence directory must be private')
    finally:
        os.close(fd)
    return safe_path(value)


def read_bytes(value, private=True):
    fd = open_path(value)
    try:
        before = os.fstat(fd)
        require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and
                before.st_uid in (0, os.geteuid()), 'file ownership or links')
        require(before.st_mode & (0o077 if private else 0o022) == 0,
                'file permissions')
        require(0 < before.st_size <= MAX_BYTES, 'file size')
        chunks = []
        remaining = MAX_BYTES + 1
        while remaining:
            chunk = os.read(fd, min(65536, remaining))
            if not chunk:
                break
            chunks.append(chunk)
            remaining -= len(chunk)
        raw = b''.join(chunks)
        after = os.fstat(fd)
        require((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns,
                 before.st_ctime_ns, before.st_uid, before.st_mode, before.st_nlink) ==
                (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns,
                 after.st_ctime_ns, after.st_uid, after.st_mode, after.st_nlink) and len(raw) == before.st_size,
                'file changed while reading')
        require(os.stat(value, follow_symlinks=False).st_ino == before.st_ino,
                'file replaced while reading')
        return raw
    except OSError:
        raise Invalid('file read failed') from None
    finally:
        os.close(fd)


def pinned_file(path, expected, private=True):
    raw = read_bytes(path, private)
    require(hashlib.sha256(raw).hexdigest() == digest(expected), 'file SHA256 mismatch')
    return raw


def inside(value, directory):
    path = safe_path(value)
    require(path != directory and path.is_relative_to(directory), 'evidence path escape')
    return path


def command(argv, cwd):
    env = {name: value for name, value in os.environ.items()
           if not name.startswith(('GIT_', 'PYTHON')) and name not in ('LD_PRELOAD', 'LD_LIBRARY_PATH')}
    env.update({'GIT_CONFIG_NOSYSTEM': '1', 'GIT_CONFIG_GLOBAL': os.devnull,
                'GIT_TERMINAL_PROMPT': '0', 'LC_ALL': 'C'})
    try:
        result = subprocess.run(argv, cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                timeout=15, check=False)
    except (OSError, subprocess.TimeoutExpired):
        raise Invalid('identity command unavailable') from None
    require(result.returncode == 0 and len(result.stdout) <= 65536, 'identity command failed')
    try:
        return result.stdout.decode('utf-8').strip()
    except UnicodeError:
        raise Invalid('identity command encoding') from None


def git(checkout, *args):
    return command(['git', '--no-optional-locks', '-C', str(checkout), *args], checkout)


def file_list(value, expected_paths, label):
    require(type(value) is list and len(value) == len(expected_paths), label + ': count')
    result = {}
    for row in value:
        keys(row, ('path', 'sha256'), label)
        require(type(row['path']) is str and row['path'] in expected_paths and
                row['path'] not in result, label + ': paths')
        result[row['path']] = digest(row['sha256'])
    require(set(result) == set(expected_paths), label + ': coverage')
    return result


def ownership(value):
    keys(value, ('manager', 'writer', 'codify_attempt', 'fence', 'paths', 'exclusive'), 'ownership')
    same(value['manager'], 'px26-fm1', 'manager')
    same(value['writer'], 'isolated-cli-author-W-CORE-WORKSPACE', 'writer')
    same(value['codify_attempt'], '3b57d71f2384b9f82b2612d78da0c854062402a195b3112775f14b2460f3e4d1', 'claim')
    same(value['fence'], 2, 'fence')
    same(value['paths'], list(PATHS), 'exclusive paths')
    same(value['exclusive'], True, 'exclusive writer')


def validate_structure(admission, approval):
    keys(admission, ('schema', 'selector', 'task_id', 'decision_id', 'approved_by',
        'issued_at_unix', 'candidate', 'contract', 'toolchain', 'dependencies',
        'ownership', 'access', 'evidence', 'model', 'overlay', 'other_selector_records',
        'authorization'), 'admission')
    keys(approval, ('schema', 'selector', 'task_id', 'decision_id', 'approved_by',
        'admission_path', 'admission_sha256', 'base_revision', 'branch',
        'contract_version', 'contract_files', 'ownership', 'policy_sha256',
        'runtime_result_sha256', 'runtime_log_sha256', 'evidence_directory',
        'overlay_files', 'status'), 'approval')
    same(admission['schema'], 'paxai.admission.v1', 'admission schema')
    same(approval['schema'], 'paxai.approval.v1', 'approval schema')
    for record in (admission, approval):
        same(record['selector'], SELECTORS[0], 'selector identity')
        for name, value in IDENTITY.items():
            same(record[name], value, name)
        ownership(record['ownership'])
    integer(admission['issued_at_unix'], 'issued time', 1)
    same(admission['issued_at_unix'], 1791454712, 'decision issuance')
    require(admission['issued_at_unix'] <= int(time.time()) + 300, 'future decision')
    keys(admission['candidate'], ('checkout', 'base_revision', 'branch'), 'candidate')
    candidate = admission['candidate']
    safe_path(candidate['checkout'])
    revision(candidate['base_revision'])
    same(candidate['base_revision'], 'ce269bfe23c1db984a8ae62ce13aa36c238ce2fd', 'approved base')
    same(candidate['branch'], 'px26/fm1/ai-workspace', 'approved branch')
    same(approval['base_revision'], candidate['base_revision'], 'base pin')
    same(approval['branch'], candidate['branch'], 'branch pin')
    keys(admission['contract'], ('version', 'files'), 'contract')
    same(admission['contract']['version'], 1, 'application version')
    same(approval['contract_version'], 1, 'contract version pin')
    expected = tuple('spec/paxeer-x/ai-markets/' + name for name in CONTRACT_PATHS)
    contracts = file_list(admission['contract']['files'], expected, 'contract')
    require(file_list(approval['contract_files'], expected, 'contract pin') == contracts,
            'contract pin mismatch')
    keys(admission['toolchain'], ('python', 'rustc', 'cargo', 'installed_targets'), 'toolchain')
    tools = admission['toolchain']
    keys(tools['python'], ('executable', 'version'), 'python')
    safe_path(tools['python']['executable'])
    same(tools['python']['version'], '3.12.3', 'python version')
    for name, release in (('rustc', 'ed61e7d7e 2025-11-07'), ('cargo', 'ea2d97820 2025-10-10')):
        keys(tools[name], ('executable', 'version', 'release_identity'), name)
        safe_path(tools[name]['executable'])
        same(tools[name]['version'], '1.91.1', name + ' version')
        same(tools[name]['release_identity'], release, name + ' release')
    same(tools['installed_targets'], ['wasm32-unknown-unknown', 'x86_64-unknown-linux-gnu'], 'targets')
    keys(admission['dependencies'], ('validator', 'new_dependencies', 'programs_lock_sha256',
                                    'programs_lock_evidence'), 'dependencies')
    deps = admission['dependencies']
    same(deps['validator'], 'python-standard-library-only', 'validator dependencies')
    same(deps['new_dependencies'], [], 'new dependencies')
    same(deps['programs_lock_sha256'], 'f20c66cb7d488269d52a8d5a14a99f2fc23ec1b57dfbe697497a2d7744d67354', 'lock identity')
    same(deps['programs_lock_evidence'], 'Root verified actual Programs lock SHA256 before dispatch; author may not read unapproved lockfile.', 'lock evidence')
    access = admission['access']
    keys(access, ('policy_path', 'policy_sha256', 'runtime_result_path',
        'runtime_result_sha256', 'runtime_scope_result', 'infrastructure_probe_exit',
        'infrastructure_probe_qualification', 'full_sandbox_pass', 'limits',
        'author_read_scope', 'author_write_scope', 'allowed_tools', 'network', 'web',
        'mcp', 'apps', 'ignore_user_config', 'ignore_rules', 'project_doc_max_bytes',
        'ephemeral'), 'access')
    for name in ('network', 'web', 'mcp', 'apps', 'full_sandbox_pass'):
        same(access[name], False, name)
    for name in ('ignore_user_config', 'ignore_rules', 'ephemeral'):
        same(access[name], True, name)
    same(access['project_doc_max_bytes'], 0, 'instruction discovery')
    same(access['runtime_scope_result'], 'PASS', 'runtime scope')
    same(access['infrastructure_probe_exit'], 1, 'infrastructure exit')
    same(access['infrastructure_probe_qualification'], 'UNRUN', 'infrastructure qualification')
    same(access['limits'], LIMITS, 'confinement limitations')
    same(access['author_read_scope'], READ_SCOPE, 'read scope')
    same(access['author_write_scope'], 'exclusive private workspace-output subtree only; controller materializes exact two product files', 'write scope')
    same(access['allowed_tools'], ['shell for approved reads/private output', 'apply_patch within private workspace-output'], 'allowed tools')
    for name in ('policy_sha256', 'runtime_result_sha256'):
        same(digest(access[name]), digest(approval[name]), name + ' pin')
    safe_path(access['policy_path'])
    safe_path(access['runtime_result_path'])
    evidence = admission['evidence']
    keys(evidence, ('directory', 'approval_path', 'runtime_log_path', 'runtime_log_sha256',
                    'gate_log_path', 'gate_log_status'), 'evidence')
    directory = safe_path(evidence['directory'])
    same(approval['evidence_directory'], str(directory), 'evidence directory pin')
    for name in ('approval_path', 'runtime_log_path', 'gate_log_path'):
        inside(evidence[name], directory)
    inside(access['runtime_result_path'], directory)
    same(digest(evidence['runtime_log_sha256']), digest(approval['runtime_log_sha256']), 'runtime log pin')
    same(evidence['gate_log_status'], 'not-yet-run-root-Codify-owns-gate', 'gate ownership')
    keys(admission['model'], ('requested', 'backend_identity', 'runtime_probe_exit'), 'model')
    same(admission['model']['requested'], 'gpt-6.1-sol', 'requested model')
    same(admission['model']['backend_identity'], 'not-independently-emitted-by-CLI-events', 'model evidence limit')
    same(admission['model']['runtime_probe_exit'], 0, 'runtime exit')
    keys(admission['authorization'], ('metadata_authority', 'product_authority'), 'authorization')
    same(admission['authorization']['metadata_authority'], 'Root explicitly delegated private actual workspace manifests from supplied verified facts; no runtime/deployment success may be invented.', 'metadata authority')
    same(admission['authorization']['product_authority'], 'Only isolated CLI authors the two exact files; root owns canonical qualification/publication.', 'product authority')
    keys(admission['overlay'], ('status', 'files'), 'overlay')
    same(admission['overlay']['status'], 'READY', 'overlay readiness')
    same(approval['status'], 'READY', 'approval readiness')
    overlay = file_list(admission['overlay']['files'], PATHS, 'overlay')
    require(file_list(approval['overlay_files'], PATHS, 'overlay pin') == overlay, 'overlay pin mismatch')
    require(type(admission['other_selector_records']) is dict and
            set(admission['other_selector_records']).issubset(SELECTORS[1:]), 'other selector fields')
    for value in admission['other_selector_records'].values():
        keys(value, ('path', 'sha256'), 'other selector reference')
        inside(value['path'], directory)
        digest(value['sha256'])
    digest(approval['admission_sha256'])
    safe_path(approval['admission_path'])
    return contracts, overlay


def validate_workspace(admission, approval, admission_path, approval_path, admission_raw):
    contracts, overlay = validate_structure(admission, approval)
    directory = private_directory(admission['evidence']['directory'])
    inside(str(admission_path), directory)
    inside(str(approval_path), directory)
    same(approval['admission_path'], str(admission_path), 'admission reference')
    same(admission['evidence']['approval_path'], str(approval_path), 'approval reference')
    same(hashlib.sha256(admission_raw).hexdigest(), approval['admission_sha256'], 'admission bytes pin')
    require(parse(admission_raw) == admission, 'admission object differs from bytes')
    candidate = admission['candidate']
    checkout = safe_path(candidate['checkout'])
    require(git(checkout, 'rev-parse', '--verify', 'HEAD^{commit}') == candidate['base_revision'], 'stale Git HEAD')
    require(git(checkout, 'symbolic-ref', '--short', 'HEAD') == candidate['branch'], 'stale Git branch')
    common = git(checkout, 'rev-parse', '--path-format=absolute', '--git-common-dir')
    canonical = safe_path(common).parent
    for name, expected in contracts.items():
        pinned_file(str(canonical / name), expected, private=False)
    require(sys.version_info[:3] == (3, 12, 3), 'actual Python must be 3.12.3')
    python = admission['toolchain']['python']['executable']
    require(os.path.samefile(sys.executable, python), 'actual Python executable mismatch')
    access = admission['access']
    pinned_file(access['policy_path'], access['policy_sha256'])
    runtime = parse(pinned_file(access['runtime_result_path'], access['runtime_result_sha256']))
    validate_runtime(runtime)
    pinned_file(admission['evidence']['runtime_log_path'], admission['evidence']['runtime_log_sha256'])
    for name, expected in overlay.items():
        pinned_file(str(checkout / name), expected, private=False)
    require(Path(__file__).absolute() == checkout / PATHS[0], 'validator must execute materialized owned path')
    return admission


def validate_runtime(runtime):
    require(type(runtime) is dict and runtime, 'empty runtime evidence')
    # The pinned producer explicitly preserves the earlier unrun qualification.
    # Admission uses UNRUN; the actual producer spelling is UNRUN_UNCHANGED.
    # Neither statement certifies infrastructure or a full sandbox pass.
    for name, expected in (('runtime_scope_result', 'PASS'), ('full_sandbox_pass', False),
                           ('infrastructure_probe_exit', 1),
                           ('infrastructure_probe_qualification', 'UNRUN_UNCHANGED')):
        require(name in runtime, 'missing runtime control')
        same(runtime[name], expected, 'producer ' + name)


def load_authority(admission_path=None, approval_path=None):
    admission_path = safe_path(admission_path if admission_path is not None else
                               os.environ.get('PAXAI_ADMISSION_RECORD', ''))
    approval_path = safe_path(approval_path if approval_path is not None else
                              os.environ.get('PAXAI_APPROVAL_RECORD', ''))
    require(admission_path != approval_path, 'distinct authority files required')
    raw = read_bytes(str(admission_path))
    approval = parse(read_bytes(str(approval_path)))
    admission = parse(raw)
    validate_workspace(admission, approval, admission_path, approval_path, raw)
    return admission, approval


def evidence_item(value, directory, base, selector, name):
    keys(value, ('path', 'sha256', 'identity'), 'selector evidence reference')
    path = inside(value['path'], directory)
    raw = pinned_file(str(path), value['sha256'])
    record = parse(raw)
    keys(record, ('schema', 'selector', 'task_id', 'decision_id', 'approved_by',
                  'kind', 'identity', 'base_revision', 'status', 'observed_at_unix',
                  'facts', 'attachments', 'command', 'exit_code', 'log_path', 'log_sha256'), 'selector evidence')
    same(record['schema'], 'paxai.selector-evidence.v1', 'evidence schema')
    same(record['selector'], selector, 'evidence selector')
    same(record['task_id'], name if selector == 'storage-prerequisite-evidence'
         else FUTURE_TASKS[selector], 'evidence task')
    same(record['decision_id'], base['decision_id'], 'evidence decision')
    same(record['approved_by'], base['approved_by'], 'evidence approver')
    same(record['base_revision'], base['candidate']['base_revision'], 'evidence revision')
    same(record['kind'], name, 'evidence kind')
    same(record['identity'], text(value['identity'], 'evidence identity', 256), 'evidence identity')
    same(record['status'], 'PASS', 'evidence status')
    text(record['command'], 'producer command', 1024)
    same(record['exit_code'], 0, 'producer exit')
    integer(record['observed_at_unix'], 'observation time', base['issued_at_unix'], int(time.time()) + 300)
    pinned_file(str(inside(record['log_path'], directory)), record['log_sha256'])
    require(type(record['facts']) is dict and record['facts'], 'missing structured evidence facts')
    if selector == 'storage-prerequisite-evidence':
        same(record['command'], record['facts'].get('command'), 'producer command binding')
        same(record['exit_code'], record['facts'].get('exit_code'), 'producer exit binding')
    hashes = {field: expected for field, expected in record['facts'].items()
              if field.endswith('_sha256')}
    for field in ('key_digests', 'persistence_receipts'):
        if field in record['facts']:
            values = record['facts'][field]
            require(type(values) is list and len(values) == 2, 'archive attachment count')
            for index, expected in enumerate(values):
                hashes[field + '/' + str(index)] = digest(expected)
    keys(record['attachments'], hashes, 'evidence attachment coverage')
    for field, expected in hashes.items():
        attachment = record['attachments'][field]
        keys(attachment, ('path', 'sha256'), 'evidence attachment')
        same(digest(attachment['sha256']), digest(expected), 'attachment fact binding')
        pinned_file(str(inside(attachment['path'], directory)), expected)
    return record['facts']


FACT_FIELDS = {
    'domain': {'chain_domain': 'sha', 'genesis_sha256': 'sha'},
    'genesis': {'chain_domain': 'sha', 'genesis_sha256': 'sha', 'config_sha256': 'sha'},
    'source': {'revision': 'rev', 'tree_sha256': 'sha'},
    'artifact': {'artifact_sha256': 'sha', 'manifest_sha256': 'sha', 'source_revision': 'rev'},
    'abi': {'host_abi': 'positive', 'artifact_abi': 'positive', 'application_version': 'one'},
    'runtime': {'version': 'positive', 'context_v2': 'true', 'hash': 'true', 'signature': 'true', 'shared_storage': 'true'},
    'fees': {'version': 'positive', 'schedule_sha256': 'sha', 'metering_verified': 'true'},
    'account_registration': {'program_id': 'sha', 'account': 'sha', 'asset': 'sha', 'receipt_sha256': 'sha', 'derived_ownership_verified': 'true'},
    'immutable_asset': {'asset': 'sha', 'immutable': 'true'},
    'capabilities': {'grants_sha256': 'sha', 'shared_read': 'true', 'shared_write': 'true', 'emit_event': 'true', 'program_spend': 'true'},
    'authenticated_height': {'height': 'uint', 'context_selector': 'five', 'authenticated': 'true'},
    'finality': {'chain_domain': 'sha', 'finalized_height': 'uint', 'state_root': 'sha', 'verifier_config_sha256': 'sha', 'trust_roots_sha256': 'sha', 'verification_receipt_sha256': 'sha'},
    'proof': {'state_root': 'sha', 'proof_sha256': 'sha', 'verification_receipt_sha256': 'sha', 'balance_proof_verified': 'true'},
    'balance': {'account': 'sha', 'asset': 'sha', 'amount': 'uint', 'state_root': 'sha', 'proof_sha256': 'sha'},
    'capacity': {'observation_root': 'sha', 'free_blobs': 'uint', 'free_bytes': 'uint', 'reserved_blobs': 'uint', 'reserved_bytes': 'uint', 'serialized_reservations': 'true'},
    'native_round_trip': {'source_revision': 'rev', 'artifact_sha256': 'sha', 'real_host_paths': 'true', 'funding_principal_verified': 'true', 'payout_owner_verified': 'true', 'spend_ceiling_verified': 'true', 'rollback_verified': 'true'},
    'rights': {'authority_sha256': 'sha', 'rights_sha256': 'sha', 'live_revocation_checked': 'true'},
    'artifacts': {'runner_sha256': 'sha', 'evaluator_sha256': 'sha', 'manifest_sha256': 'sha'},
    'runner': {'artifact_sha256': 'sha', 'tenant_identity': 'text', 'real_execution_receipt_sha256': 'sha'},
    'evaluator': {'artifact_sha256': 'sha', 'tenant_identity': 'text', 'real_execution_receipt_sha256': 'sha'},
    'durable_storage': {'provider_identity': 'text', 'tenant_identity': 'text', 'write_receipt_sha256': 'sha', 'read_receipt_sha256': 'sha', 'restart_recovery_verified': 'true'},
    'tls': {'endpoint_identity': 'text', 'certificate_sha256': 'sha', 'verification_receipt_sha256': 'sha', 'expires_at_unix': 'future'},
    'tenant': {'tenant_identity': 'text', 'isolation_policy_sha256': 'sha', 'isolation_receipt_sha256': 'sha'},
    'kms': {'tenant_identity': 'text', 'key_generation': 'positive', 'key_policy_sha256': 'sha', 'signing_receipt_sha256': 'sha', 'revocation_checked': 'true'},
    'version': {'profile_version': 'positive', 'module_version': 'positive', 'profile_sha256': 'sha'},
    'ordinal': {'module': 'nine', 'ordinal': 'positive', 'registry_sha256': 'sha'},
    'activation': {'batch': 'uint', 'chain_domain': 'sha', 'finality_receipt_sha256': 'sha'},
    'compatibility': {'historical_replay_preserved': 'true', 'compatibility_receipt_sha256': 'sha'},
    'controller': {'authority_sha256': 'sha', 'generation': 'positive', 'rotation_policy_sha256': 'sha', 'revocation_policy_sha256': 'sha'},
    'archive_generations': {'operators': 'operators', 'generations': 'generations', 'key_digests': 'digests'},
    'reference_registry': {'registry_sha256': 'sha', 'namespace_heads': 'true', 'replay_pairs': 'true', 'pending_activities': 'true', 'retained_roots': 'true', 'unknown_references_pinned': 'true'},
    'hot_horizon': {'batches': 'positive', 'occupancy_blobs': 'uint', 'occupancy_bytes': 'uint', 'occupancy_kv': 'uint', 'live_obligations_pinned': 'true'},
    'indefinite_retention': {'indefinite': 'true', 'provisioned_growth_sha256': 'sha', 'historical_proof_retrieval_receipt_sha256': 'sha'},
    'certificate': {'threshold': 'two', 'archive_count': 'two', 'manifest_sha256': 'sha', 'profile_sha256': 'sha', 'persistence_receipts': 'digests', 'root_verified': 'true', 'continuity_verified': 'true'},
    'work': {'max_candidates': 'positive', 'max_scan_entries': 'positive', 'max_deletions': 'positive', 'root_transition_verified': 'true', 'paired_replay_retirement': 'true'},
    'reserve': {'blobs': 'positive', 'bytes': 'positive', 'kv': 'positive', 'serialized': 'true', 'pending_obligations_covered': 'true'},
}


def fact_value(value, rule):
    if rule == 'sha':
        digest(value)
    elif rule == 'rev':
        revision(value)
    elif rule == 'text':
        text(value, 'fact identity', 256)
    elif rule in ('positive', 'uint', 'future'):
        integer(value, 'fact integer', 1 if rule == 'positive' else
                int(time.time()) + 1 if rule == 'future' else 0)
    elif rule in ('true', 'one', 'two', 'five', 'nine'):
        same(value, {'true': True, 'one': 1, 'two': 2, 'five': 5, 'nine': 9}[rule], 'fact constraint')
    elif rule in ('operators', 'generations', 'digests'):
        require(type(value) is list and len(value) == 2, 'two archive identities required')
        for item in value:
            fact_value(item, {'operators': 'text', 'generations': 'positive', 'digests': 'sha'}[rule])
        if rule != 'generations':
            require(value[0] != value[1], 'archive independence required')
    else:
        raise Invalid('unrecognised fact rule')


def validate_facts(selector, facts, record):
    for name, value in facts.items():
        rules = FACT_FIELDS[name]
        keys(value, rules, 'structured ' + name + ' facts')
        for field, rule in rules.items():
            fact_value(value[field], rule)
    if selector == 'deployment-admission':
        domain = facts['domain']
        for name in ('genesis', 'finality'):
            same(facts[name]['chain_domain'], domain['chain_domain'], 'chain domain binding')
        same(facts['genesis']['genesis_sha256'], domain['genesis_sha256'], 'genesis binding')
        same(facts['source']['revision'], record['base_revision'], 'source revision binding')
        for name in ('artifact', 'native_round_trip'):
            same(facts[name]['source_revision'], record['base_revision'], 'artifact source binding')
        same(facts['native_round_trip']['artifact_sha256'], facts['artifact']['artifact_sha256'], 'host artifact binding')
        same(facts['abi']['artifact_abi'], facts['abi']['host_abi'], 'admitted ABI binding')
        require(facts['abi']['host_abi'] >= 2, 'v2 host surface required')
        for name in ('account_registration', 'balance'):
            same(facts[name]['asset'], facts['immutable_asset']['asset'], 'immutable asset binding')
        same(facts['account_registration']['account'], facts['balance']['account'], 'derived account binding')
        same(facts['proof']['state_root'], facts['finality']['state_root'], 'finalized proof binding')
        same(facts['balance']['state_root'], facts['finality']['state_root'], 'finalized balance binding')
        same(facts['balance']['proof_sha256'], facts['proof']['proof_sha256'], 'balance proof binding')
        require(facts['authenticated_height']['height'] >= facts['finality']['finalized_height'], 'height/finality contradiction')
        capacity = facts['capacity']
        require(64 <= capacity['free_blobs'] <= CAPS['committed_blobs'] and
                capacity['reserved_blobs'] >= 4 and
                capacity['reserved_blobs'] <= capacity['free_blobs'] and
                0 < capacity['reserved_bytes'] <= capacity['free_bytes'] <= CAPS['committed_bytes'], 'capacity reserve unavailable')
    elif selector == 'real-services-admission':
        for name in ('runner', 'evaluator', 'durable_storage', 'kms'):
            same(facts[name]['tenant_identity'], facts['tenant']['tenant_identity'], 'tenant binding')
        for name in ('runner', 'evaluator'):
            same(facts[name]['artifact_sha256'], facts['artifacts'][name + '_sha256'], 'service artifact binding')
    elif selector == 'storage-profile-admission':
        same(facts['certificate']['profile_sha256'], facts['version']['profile_sha256'], 'certificate profile binding')
        horizon = facts['hot_horizon']
        reserve = facts['reserve']
        require(horizon['occupancy_blobs'] + reserve['blobs'] <= CAPS['committed_blobs'] and
                horizon['occupancy_bytes'] + reserve['bytes'] <= CAPS['committed_bytes'] and
                horizon['occupancy_kv'] + reserve['kv'] <= CAPS['module_kv'], 'profile exceeds native bounds')
        require(facts['work']['max_candidates'] <= CAPS['staged_new_blobs'] and
                facts['work']['max_deletions'] <= 3 and
                facts['work']['max_deletions'] <= facts['work']['max_candidates'] and
                facts['work']['max_scan_entries'] <= CAPS['module_kv'], 'cleanup work exceeds bounds')
        require(reserve['blobs'] >= 4, 'insufficient obligation reserve')


def validate_future(selector, admission):
    require(selector in SELECTORS[1:], 'invalid future selector')
    reference = admission['other_selector_records'].get(selector)
    require(reference is not None, 'selector evidence absent')
    keys(reference, ('path', 'sha256'), 'selector record reference')
    directory = private_directory(admission['evidence']['directory'])
    record = parse(pinned_file(str(inside(reference['path'], directory)), reference['sha256']))
    keys(record, ('schema', 'selector', 'task_id', 'decision_id', 'approved_by',
                  'status', 'base_revision', 'contract_version', 'native_caps',
                  'namespace', 'finality_policy', 'evidence'), 'selector record')
    same(record['schema'], 'paxai.selector-admission.v1', 'selector schema')
    same(record['selector'], selector, 'selector')
    same(record['task_id'], FUTURE_TASKS[selector], 'selector task')
    same(record['decision_id'], admission['decision_id'], 'selector decision')
    same(record['approved_by'], admission['approved_by'], 'selector approver')
    same(record['status'], 'APPROVED', 'selector approval')
    same(record['base_revision'], admission['candidate']['base_revision'], 'selector revision')
    same(record['contract_version'], 1, 'selector contract')
    keys(record['native_caps'], CAPS, 'native caps')
    for name, expected in CAPS.items():
        same(record['native_caps'][name], expected, 'native cap')
    same(record['namespace'], 'one-program-shared-key', 'native namespace')
    same(record['finality_policy'], 'verified-authoritative-finality', 'finality policy')
    keys(record['evidence'], FUTURE_FIELDS[selector], 'evidence coverage')
    facts = {name: evidence_item(value, directory, admission, selector, name)
             for name, value in record['evidence'].items()}
    if selector == 'storage-prerequisite-evidence':
        commands = {'ai.T-S01': 'python3 tests/relay-archive/state_archive_lifecycle.py --native-node build/bin/layerxd',
                    'ai.T-S02': 'make test-paxai-blob-lifecycle',
                    'ai.T-S03': 'make test-paxai-blob-admission'}
        profiles = set()
        for task, value in facts.items():
            keys(value, ('task_id', 'revision', 'command', 'exit_code', 'profile_sha256',
                         'native_caps', 'producer_rerun'), 'prerequisite facts')
            same(value['task_id'], task, 'prerequisite task')
            same(value['revision'], record['base_revision'], 'prerequisite revision')
            same(value['command'], commands[task], 'prerequisite command')
            same(value['exit_code'], 0, 'prerequisite exit')
            same(value['producer_rerun'], False, 'producer rerun')
            keys(value['native_caps'], CAPS, 'prerequisite caps')
            for name, expected in CAPS.items():
                same(value['native_caps'][name], expected, 'prerequisite cap')
            profiles.add(digest(value['profile_sha256']))
        require(len(profiles) == 1, 'prerequisite profiles disagree')
        profile_ref = admission['other_selector_records'].get('storage-profile-admission')
        require(profile_ref is not None and profile_ref['sha256'] in profiles, 'matching profile absent')
        validate_future('storage-profile-admission', admission)
    else:
        validate_facts(selector, facts, record)
    return record


def validate_selector(selector):
    require(type(selector) is str and selector in SELECTORS, 'invalid selector')
    admission, approval = load_authority()
    if selector != SELECTORS[0]:
        validate_future(selector, admission)
    return admission, approval


class SafeTestResult(unittest.TestResult):
    def __init__(self, test_class, methods):
        super().__init__()
        self.test_class = test_class
        self.methods = frozenset(methods)
        self.diagnostics = set()

    def identity(self, test):
        if type(test) is self.test_class:
            name = getattr(test, '_testMethodName', None)
            if type(name) is str and name in self.methods:
                return 'EvidenceTests.' + name
        return 'fixture-or-loader'

    def _exc_info_to_string(self, err, test):
        # unittest otherwise formats exception values and full tracebacks.
        return refusal_reason(err[1])

    def note(self, test, outcome, error=None):
        reason = refusal_reason(error[1]) if error is not None else outcome
        self.diagnostics.add((self.identity(test), outcome, reason))

    def addError(self, test, err):
        self.note(test, 'error', err)
        super().addError(test, err)

    def addFailure(self, test, err):
        self.note(test, 'failure', err)
        super().addFailure(test, err)

    def addSubTest(self, test, subtest, err):
        if err is not None:
            outcome = 'failure' if issubclass(err[0], test.failureException) else 'error'
            self.note(test, 'subtest-' + outcome, err)
        super().addSubTest(test, subtest, err)

    def addSkip(self, test, reason):
        self.note(test, 'skip')
        super().addSkip(test, 'skip')

    def addUnexpectedSuccess(self, test):
        self.note(test, 'unexpected-success')
        super().addUnexpectedSuccess(test)


def focused_tests(admission, approval):
    path = Path(__file__).parent / 'tests' / 'ai_evidence_test.py'
    spec = importlib.util.spec_from_file_location('paxai_evidence_tests', path)
    require(spec is not None and spec.loader is not None, 'focused tests unavailable')
    module = importlib.util.module_from_spec(spec)
    expected = file_list(admission['overlay']['files'], PATHS, 'test overlay')[PATHS[1]]
    raw = pinned_file(str(path), expected, private=False)
    exec(compile(raw, str(path), 'exec'), module.__dict__)
    module.AUTHORITY = (admission, approval)
    suite = unittest.defaultTestLoader.loadTestsFromModule(module)
    require(suite.countTestCases() >= 10, 'focused tests incomplete')
    test_class = module.EvidenceTests
    methods = tuple(name for name in vars(test_class)
                    if re.fullmatch(r'test_[a-z0-9_]{1,100}', name))
    result = SafeTestResult(test_class, methods)
    suite.run(result)
    for identity, outcome, reason in sorted(result.diagnostics):
        print('REFUSE testcase=' + identity + ' outcome=' + outcome +
              ' reason=' + reason, file=sys.stderr)
    require(result.wasSuccessful() and not result.skipped, 'focused tests failed')


def main(argv=None):
    stage = 'cli'
    try:
        parser = Parser(prog='ai_evidence.py', add_help=True, allow_abbrev=False)
        parser.add_argument('--selector', required=True, choices=SELECTORS)
        args = parser.parse_args(argv)
        stage = 'authority'
        admission, approval = validate_selector(args.selector)
        if args.selector == SELECTORS[0]:
            stage = 'focused-tests'
            focused_tests(admission, approval)
        print('PASS ' + args.selector + ' evidence-consumption-only')
        return 0
    except (Invalid, OSError, ValueError, ImportError, RuntimeError) as error:
        print('REFUSE evidence validation failed stage=' + stage +
              ' reason=' + refusal_reason(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.modules.setdefault('ai_evidence', sys.modules[__name__])
    sys.dont_write_bytecode = True
    raise SystemExit(main())
