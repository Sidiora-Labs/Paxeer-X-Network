"""Coordinator-authenticated executed-check evidence. No canonical state writes."""
import errno
import hashlib
import hmac
import json
import os
import re
from pathlib import Path
import shutil
import signal
import stat
import subprocess
import time
import tempfile
try:
    from .workspace import run_process
    from .source_policy import ignored, SOURCE_POLICY_VERSION
except ImportError:
    # Direct-spec loading used by independent review still needs package-relative
    # dependencies, whose source policy is shared with patch/archive operations.
    import importlib
    import sys
    _parent = str(Path(__file__).resolve().parent.parent)
    sys.path.insert(0, _parent)
    try:
        _ws = importlib.import_module('dispatcher.workspace')
        _policy = importlib.import_module('dispatcher.source_policy')
    finally:
        sys.path.remove(_parent)
    run_process = _ws.run_process
    ignored, SOURCE_POLICY_VERSION = _policy.ignored, _policy.SOURCE_POLICY_VERSION

class EvidenceError(ValueError):
    pass

def _json(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), allow_nan=False).encode()

def _file(path):
    path = Path(path)
    if path.is_symlink() or not path.is_file():
        raise EvidenceError(f'missing or unsafe file: {path}')
    if path.stat().st_nlink != 1:
        raise EvidenceError(f'hardlinked file: {path}')
    return hashlib.sha256(path.read_bytes()).hexdigest()

def source_digest(root):
    raw_root = Path(root)
    if any(p.is_symlink() for p in [raw_root, *raw_root.parents]):
        raise EvidenceError('source root symlink')
    root = raw_root.resolve()
    if not root.is_dir():
        raise EvidenceError('source root missing')
    entries = []
    for current, dirs, files in os.walk(root, followlinks=False):
        current = Path(current)
        dirs[:] = sorted(d for d in dirs if not ignored((current/d).relative_to(root)))
        for name in dirs + sorted(files):
            path = current/name
            relative = path.relative_to(root)
            if ignored(relative):
                continue
            if path.is_symlink():
                raise EvidenceError('source symlink')
            if path.is_file():
                mode = stat.S_IMODE(path.stat().st_mode)
                if mode & 0o7000:
                    raise EvidenceError('special source permission bits')
                entries.append([relative.as_posix(), _file(path), mode])
            elif not path.is_dir():
                raise EvidenceError('special source file')
    entries.sort()
    if not entries:
        raise EvidenceError('empty source root')
    return hashlib.sha256(_json(dict(policy=SOURCE_POLICY_VERSION, files=entries))).hexdigest()

def _paths(paths, root, outside=False):
    if not isinstance(paths, list):
        raise EvidenceError('paths must be a list')
    result = {}
    for value in paths:
        if not isinstance(value, str) or not value:
            raise EvidenceError('invalid declared path')
        path = Path(value)
        if not path.is_absolute():
            path = root / path
        # Check unresolved components before resolve, which otherwise erases links.
        for component in [path, *path.parents]:
            if component.is_symlink():
                raise EvidenceError('symlink declared path')
        path = path.resolve()
        inside = path == root or root in path.parents
        if inside and ignored(path.relative_to(root)):
            raise EvidenceError('declared input outside qualified source policy')
        if inside == outside:
            raise EvidenceError('declared path has wrong source/output boundary')
        if str(path) in result:
            raise EvidenceError('duplicate declared path')
        result[str(path)] = _file(path)
    return result

def _checker(command, root):
    executable = shutil.which(command[0]) if not os.path.isabs(command[0]) else command[0]
    if not executable:
        candidate = root / command[0]
        executable = str(candidate) if candidate.is_file() else None
    if not executable:
        raise EvidenceError('checker executable missing')
    # Executable installations commonly use symlinks; bind their resolved bytes.
    paths = {str(Path(executable).resolve()): _file(Path(executable).resolve())}
    for arg in command[1:]:
        candidate = Path(arg)
        if not candidate.is_absolute():
            candidate = root / candidate
        try:
            info = candidate.lstat()
        except OSError as error:
            # Arbitrary signed argv (notably python -c source) need not be paths.
            # Preserve access, I/O and other filesystem failures as fail-closed.
            if error.errno in (errno.ENOENT, errno.ENOTDIR, errno.ENAMETOOLONG):
                continue
            raise
        if stat.S_ISLNK(info.st_mode):
            raise EvidenceError('checker script symlink')
        if stat.S_ISREG(info.st_mode):
            if any(parent.is_symlink() for parent in candidate.parents):
                raise EvidenceError('checker script symlink ancestor')
            paths[str(candidate.resolve())] = _file(candidate)
    return paths

def _secret(secret):
    if not isinstance(secret, bytes) or len(secret) < 32:
        raise EvidenceError('coordinator secret must contain at least 32 bytes')

def _proof(receipt, policy):
    if policy is None:
        return None
    if isinstance(policy, dict):
        kind = policy.get('type')
    else:
        kind = policy
    text = receipt['stdout'] + '\n' + receipt['stderr']
    if receipt['exit_code'] != 0 or receipt['timed_out']:
        raise EvidenceError('proof process failed')
    if kind == 'unittest':
        counts = re.findall(r'^Ran ([0-9]+) tests? in ', text, re.M)
        if len(counts) != 1 or int(counts[0]) <= 0 or not re.search(r'^OK$', text, re.M):
            raise EvidenceError('empty or incomplete unittest proof')
        if re.search(r'^.*\.\.\. (?:skipped\b|expected failure\b|unexpected success\b)|^FAILED\b|^OK \(', text, re.M | re.I):
            raise EvidenceError('unittest skipped or failed')
        count = int(counts[0])
    elif kind == 'go-test-json':
        events = []
        for line in receipt['stdout'].splitlines():
            try:
                event = json.loads(line)
            except (ValueError, TypeError) as exc:
                raise EvidenceError('invalid Go JSON proof') from exc
            if not isinstance(event, dict):
                raise EvidenceError('invalid Go JSON event')
            events.append(event)
        if not events or any(x.get('Action') in ('skip', 'fail') for x in events):
            raise EvidenceError('Go skipped or failed')
        passed = {(x.get('Package'), x.get('Test')) for x in events if x.get('Action') == 'pass' and isinstance(x.get('Test'), str) and x['Test']}
        if not passed or not any(x.get('Action') == 'pass' and not x.get('Test') for x in events):
            raise EvidenceError('empty or incomplete Go proof')
        count = len(passed)
    elif kind == 'node-tap':
        counts = re.findall(r'^# pass ([0-9]+)$', text, re.M)
        failures = re.findall(r'^# fail ([0-9]+)$', text, re.M)
        if len(counts) != 1 or int(counts[0]) <= 0 or failures != ['0']:
            raise EvidenceError('empty or failed TAP proof')
        if re.search(r'^\s*not ok\b|^\s*(?:not )?ok\b[^\r\n]*?(?<!\\)#\s*(?:SKIP|TODO)\b', text, re.M | re.I):
            raise EvidenceError('TAP skipped or todo')
        for metric in ('skipped', 'todo', 'cancelled'):
            if any(int(n) for n in re.findall(r'^# ' + metric + r' ([0-9]+)$', text, re.M)):
                raise EvidenceError('TAP skipped or incomplete')
        named = re.findall(r'^\s*ok [0-9]+ - .+$', text, re.M)
        if not named:
            raise EvidenceError('no named TAP passes')
        metrics = {name: re.findall(r'^# ' + name + r' ([0-9]+)$', text, re.M)
                   for name in ('tests', 'suites', 'pass', 'fail', 'cancelled', 'skipped', 'todo')}
        plans = re.findall(r'^1\.\.([0-9]+)$', text, re.M)
        top_level = [int(value) for value in re.findall(r'^ok ([0-9]+) - .+$', text, re.M)]
        if (len(re.findall(r'^TAP version 13$', text, re.M)) != 1 or len(plans) != 1 or
                any(len(values) != 1 for values in metrics.values())):
            raise EvidenceError('incomplete or duplicate TAP completion')
        total = int(metrics['tests'][0])
        suites = int(metrics['suites'][0])
        if (int(plans[0]) <= 0 or top_level != list(range(1, int(plans[0]) + 1)) or
                total != int(counts[0]) or len(named) != total + suites):
            raise EvidenceError('TAP plan and completed test totals differ')
        count = int(counts[0])
    else:
        raise EvidenceError('unsupported proof policy')
    return dict(type=kind, positive_count=count)

def _same_directory(path, descriptor):
    try:
        current = Path(path).lstat()
        pinned = os.fstat(descriptor)
        if not stat.S_ISDIR(current.st_mode) or (current.st_dev, current.st_ino) != (pinned.st_dev, pinned.st_ino):
            raise EvidenceError('evidence directory replaced during check')
    except OSError as exc:
        raise EvidenceError('evidence directory disappeared during check') from exc

def run_checks(checks, cwd, output_dir, binding, token, secret):
    _secret(secret)
    root = Path(cwd).resolve()
    raw_output = Path(output_dir).absolute()
    if any(p.is_symlink() for p in [raw_output, *raw_output.parents]):
        raise EvidenceError('diagnostic directory symlink')
    output = raw_output.resolve()
    if output == root or root in output.parents:
        raise EvidenceError('evidence directory must be outside source root')
    if not isinstance(checks, list) or not checks:
        raise EvidenceError('empty checks')
    if not isinstance(binding, dict) or token.get('bindings') != binding:
        raise EvidenceError('token binding mismatch')
    for key in ('baseline', 'candidate', 'interface', 'layout'):
        value = binding.get(key)
        if not isinstance(value, str) or len(value) != 64 or any(c not in '0123456789abcdef' for c in value):
            raise EvidenceError('invalid binding digest')
    source_before = source_digest(root)
    if binding['candidate'] != source_before:
        raise EvidenceError('candidate binding differs from actual source')
    prepared = []
    ids = set()
    for check in checks:
        ident = check.get('id')
        command = check.get('command')
        timeout = check.get('timeout')
        if not isinstance(ident, str) or not ident or ident in ids:
            raise EvidenceError('missing or duplicate check id')
        if not isinstance(command, list) or not command or any(not isinstance(x, str) or not x for x in command):
            raise EvidenceError('invalid command')
        if isinstance(timeout, bool) or not isinstance(timeout, (int, float)) or not 0 < timeout <= 3600:
            raise EvidenceError('invalid timeout')
        if check.get('skip') or check.get('skipped'):
            raise EvidenceError('skipped check')
        ids.add(ident)
        prepared.append((check, _paths(check.get('inputs', []), root), _checker(command, root)))
    output.mkdir(parents=True, exist_ok=True)
    output_fd = os.open(output, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    invocation_fd = None
    try:
        # Fresh names never derive from caller-controlled check IDs.
        # Unsigned diagnostics are never acceptance evidence, even on success.
        invocation = Path(tempfile.mkdtemp(prefix='unqualified-', dir=output))
        os.chmod(invocation, 0o700)
        invocation_fd = os.open(invocation.name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=output_fd)
        receipts = []
        for ordinal, (check, inputs, checker) in enumerate(prepared):
            execution = run_process(check['command'], root, check['timeout'])
            receipt = dict(id=check['id'], command=check['command'], timeout=check['timeout'],
                           input_paths=check.get('inputs', []), output_paths=check.get('outputs', []),
                           inputs=inputs, checker=checker,
                           executed=True, proof_policy=check.get('proof'), **execution)
            diagnostic = dict(kind='unqualified-command-diagnostic', acceptance_eligible=False,
                              root=str(root), bindings=binding, source_before=source_before,
                              token_digest=hashlib.sha256(_json(token)).hexdigest(), receipt=receipt)
            fd = os.open(f'{ordinal:04d}.json', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=invocation_fd)
            with os.fdopen(fd, 'wb') as stream:
                stream.write(_json(diagnostic) + b'\n')
                stream.flush()
                os.fsync(stream.fileno())
            _same_directory(output, output_fd)
            _same_directory(invocation, invocation_fd)
            receipt['outputs'] = _paths(check.get('outputs', []), root, outside=True)
            receipt['proof'] = _proof(receipt, check.get('proof'))
            receipts.append(receipt)
        result = dict(version=1, root=str(root), bindings=binding, token=token,
                      source_digest=source_digest(root), source_before=source_before, receipts=receipts)
        if result['source_digest'] != source_before:
            raise EvidenceError('checks mutated immutable source')
        result['signature'] = hmac.new(secret, _json(result), hashlib.sha256).hexdigest()
        _same_directory(output, output_fd)
        try:
            prior = os.stat('evidence.json', dir_fd=output_fd, follow_symlinks=False)
            if not stat.S_ISREG(prior.st_mode) or prior.st_nlink != 1:
                raise EvidenceError('unsafe existing signed evidence destination')
        except FileNotFoundError:
            pass
        # Descriptor-relative atomic publication cannot follow a replaced pathname.
        name = invocation.name + '-signed.tmp'
        fd = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=output_fd)
        try:
            with os.fdopen(fd, 'wb') as stream:
                stream.write(_json(result) + b'\n')
                stream.flush()
                os.fsync(stream.fileno())
            os.replace(name, 'evidence.json', src_dir_fd=output_fd, dst_dir_fd=output_fd)
            os.fsync(output_fd)
        finally:
            try:
                os.unlink(name, dir_fd=output_fd)
            except FileNotFoundError:
                pass
        return result
    finally:
        if invocation_fd is not None:
            os.close(invocation_fd)
        os.close(output_fd)

def publish(result, secret, store, source_root, required):
    _secret(secret)
    if not isinstance(result, dict):
        raise EvidenceError('invalid evidence')
    body = dict(result)
    signature = body.pop('signature', None)
    expected = hmac.new(secret, _json(body), hashlib.sha256).hexdigest()
    if not isinstance(signature, str) or not hmac.compare_digest(signature, expected):
        raise EvidenceError('unauthenticated evidence')
    if not isinstance(required, list) or not required or any(not isinstance(x, dict) for x in required):
        raise EvidenceError('trusted required check definitions must be dictionaries')
    registry = {x.get('id'): x for x in required}
    if None in registry or len(registry) != len(required):
        raise EvidenceError('duplicate or missing trusted check id')
    root = Path(source_root).resolve()
    if body.get('version') != 1 or body.get('root') != str(root) or body.get('source_digest') != source_digest(root) or body.get('source_before') != body.get('source_digest') or body.get('bindings', {}).get('candidate') != body.get('source_digest'):
        raise EvidenceError('stale or wrong source')
    try:
        current = store.validate(body['token'])
    except Exception as exc:
        raise EvidenceError('invalid live lease') from exc
    if current.get('bindings') != body.get('bindings') or body['token'].get('bindings') != body.get('bindings'):
        raise EvidenceError('stale binding')
    receipts = body.get('receipts')
    if not isinstance(receipts, list) or not receipts:
        raise EvidenceError('empty receipts')
    found = set()
    for receipt in receipts:
        ident = receipt.get('id')
        if not isinstance(ident, str) or ident in found:
            raise EvidenceError('duplicate receipt')
        found.add(ident)
        trusted = registry.get(ident)
        if trusted is None:
            raise EvidenceError('unregistered check')
        for field, receipt_field, default in [('command','command',None), ('timeout','timeout',None), ('inputs','input_paths',[]), ('outputs','output_paths',[]), ('proof','proof_policy',None)]:
            if trusted.get(field, default) != receipt.get(receipt_field):
                raise EvidenceError('check definition differs from trusted registry')
        if receipt.get('executed') is not True or receipt.get('exit_code') != 0 or receipt.get('timed_out') is not False or receipt.get('skipped'):
            raise EvidenceError('check failed, skipped, or unexecuted')
        if receipt.get('proof') != _proof(receipt, trusted.get('proof')):
            raise EvidenceError('invalid proof summary')
        if not receipt.get('checker') or receipt['checker'] != _checker(receipt['command'], root):
            raise EvidenceError('stale checker')
        for kind in ('inputs', 'outputs'):
            recorded = receipt.get(kind)
            if not isinstance(recorded, dict) or recorded != _paths(list(recorded), root, outside=(kind == 'outputs')):
                raise EvidenceError(f'stale {kind}')
    if set(registry) != found:
        raise EvidenceError('missing required check')
    return dict(verified=True, task=current['task'], attempt=current['attempt'], fence=current['fence'],
                bindings=body['bindings'], check_ids=sorted(found),
                proofs={r['id']: r.get('proof') for r in receipts},
                source_digest=body['source_digest'],
                evidence_digest=hashlib.sha256(_json(result)).hexdigest())
