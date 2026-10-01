#!/usr/bin/env bash
# Usage:
#   verify-task.sh <task-id> [--manifest PATH]             run the task's registered gate
#   verify-task.sh --identity <task-id> [--manifest PATH]  print the gate identity, run nothing
#   verify-task.sh --check <evidence.json> [--manifest PATH] accept evidence only for this checkout
# A task is registered only by an executable tools/paxeer-x/gates/<task-id>.sh and a [task.<task-id>]
# section in spec/paxeer-x/spec.kvx. A gate must print "PAXEER_X_GATE tests=<n> skipped=<m>".
# Evidence goes to $PAXEER_X_EVIDENCE_DIR (private, outside the repository); the candidate
# manifest comes from --manifest or $PAXEER_X_CANDIDATE_MANIFEST.
# Exit codes: 2 unknown selector/usage, 3 unregistered, 4 not executable, 5 unsafe evidence
# directory, 6 manifest invalid or mismatched, 7 dirty source, 8 gate failed, 9 empty corpus,
# 10 skipped requirements, 11 fabricated or malformed evidence, 12 stale evidence.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
exec python3 - "$here" "$@" <<'PY'
import datetime
import hashlib
import hmac
import json
import os
from pathlib import Path
import re
import secrets
import stat
import subprocess
import sys

sys.dont_write_bytecode = True
HERE = Path(sys.argv[1])
sys.path.insert(0, str(HERE))
from candidate import SCHEMA, UNKNOWN, BINDING_FIELDS, Invalid, load_private, validate, catalogue

EXIT = {'usage': 2, 'unregistered': 3, 'not_executable': 4, 'evidence_dir': 5, 'mismatch': 6,
        'dirty': 7, 'failed': 8, 'empty': 9, 'skipped': 10, 'fabricated': 11, 'stale': 12}
RECORD = 'paxeer-x.gate-evidence.v1'
COUNT = re.compile(r'^PAXEER_X_GATE tests=(\d+) skipped=(\d+)$', re.M)
SELECTOR = re.compile(r'[0-9]+(?:\.[0-9]+)+')
MAINLINE = os.environ.get('PAXEER_X_MAINLINE', 'refs/heads/main')


class Refuse(Exception):
    def __init__(self, kind, reason):
        super().__init__(reason)
        self.code = EXIT[kind]


def git(*args):
    result = subprocess.run(['git', '--no-optional-locks', '-C', str(HERE), *args],
                            stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=60)
    if result.returncode:
        raise Refuse('usage', 'git ' + args[0] + ' failed')
    return result.stdout.strip()


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':')).encode()


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def source():
    return {'revision': git('rev-parse', '--verify', 'HEAD^{commit}'),
            'tree': git('rev-parse', '--verify', 'HEAD^{tree}'),
            'dirty': bool(git('status', '--porcelain=v1', '--untracked-files=normal'))}


def gate(selector):
    if not isinstance(selector, str) or not SELECTOR.fullmatch(selector):
        raise Refuse('usage', 'malformed task selector')
    if not re.search(r'^\[task\.' + re.escape(selector) + r'\]\s*$', SPEC.read_text(encoding='utf-8'), re.M):
        raise Refuse('usage', 'unknown task selector ' + selector)
    path = HERE / 'gates' / (selector + '.sh')
    if path.is_symlink() or not path.is_file():
        raise Refuse('unregistered', 'task ' + selector + ' has no registered gate')
    if not os.access(path, os.X_OK):
        raise Refuse('not_executable', 'gate for task ' + selector + ' is not executable')
    return path


def evidence_dir():
    raw = os.environ.get('PAXEER_X_EVIDENCE_DIR')
    if not raw:
        raise Refuse('evidence_dir', 'PAXEER_X_EVIDENCE_DIR is not set')
    path = Path(raw).resolve()
    if path == ROOT or ROOT in path.parents:
        raise Refuse('evidence_dir', 'evidence directory is inside the repository')
    try:
        info = path.stat()
    except OSError:
        raise Refuse('evidence_dir', 'evidence directory is unavailable') from None
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.geteuid() or info.st_mode & 0o077:
        raise Refuse('evidence_dir', 'evidence directory must be private and owned by the caller')
    return path


def key(directory):
    path = directory / '.dispatch-key'
    if not path.exists():
        staging = directory / ('.dispatch-key.' + secrets.token_hex(8))
        fd = os.open(staging, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'wb') as stream:
            stream.write(secrets.token_bytes(32))
        try:
            os.link(staging, path)
        except FileExistsError:
            pass
        os.unlink(staging)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or
                info.st_mode & 0o077 or info.st_size != 32):
            raise Refuse('evidence_dir', 'evidence signing key is not private')
        return stream.read()


def sign(secret, record):
    return hmac.new(secret, canonical(record), hashlib.sha256).hexdigest()


def manifest(argument):
    raw = argument or os.environ.get('PAXEER_X_CANDIDATE_MANIFEST')
    if not raw:
        raise Refuse('mismatch', 'no candidate manifest supplied')
    try:
        data = load_private(raw)
        validate(data, catalogue(SPEC), ROOT, False, MAINLINE)
    except (Invalid, OSError, ValueError, KeyError, TypeError, RecursionError):
        raise Refuse('mismatch', 'candidate manifest is invalid or does not identify this checkout') from None
    return data, sha256(canonical(data))


def bind(data, path):
    found = re.search(r'^# paxeer-x-services:(.*)$', path.read_text(encoding='utf-8', errors='replace'), re.M)
    services = {service['id']: service for service in data['services']}
    bound, unknown = {}, []
    for name in (found.group(1).split() if found else []):
        if name not in services:
            raise Refuse('mismatch', 'gate declares service ' + name + ' absent from the candidate')
        bindings = services[name]['bindings']
        bound[name] = {field: bindings[field] for field in BINDING_FIELDS}
        unknown += [name + '.' + field for field in BINDING_FIELDS if bindings[field] == UNKNOWN]
    return bound, unknown


def identity(gate_sha, src, candidate_sha):
    return sha256(canonical({'gate_sha256': gate_sha, 'gate_args': [], 'revision': src['revision'],
                             'tree': src['tree'], 'candidate_sha256': candidate_sha}))


def prepare(selector, argument):
    path = gate(selector)
    directory = evidence_dir()
    data, candidate_sha = manifest(argument)
    src = source()
    gate_sha = sha256(path.read_bytes())
    return path, directory, data, candidate_sha, src, gate_sha


def write_record(directory, stem, secret, record):
    record['hmac'] = sign(secret, record)
    target = directory / (stem + '.json')
    fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w', encoding='utf-8') as stream:
        json.dump(record, stream, indent=2, sort_keys=True)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())
    print('evidence: ' + str(target))


def run(selector, argument, dispatcher_argv):
    path, directory, data, candidate_sha, src, gate_sha = prepare(selector, argument)
    bound, unknown = bind(data, path)
    secret = key(directory)
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
    stem = selector + '-' + src['revision'][:12] + '-' + stamp + '-' + secrets.token_hex(4)
    argv = [str(path)]
    record = {'schema': RECORD, 'selector': selector, 'source': src,
              'candidate': {'schema': SCHEMA, 'sha256': candidate_sha,
                            'source_revision': data['source']['revision'],
                            'tree': data['source']['tree'], 'services': bound,
                            'uncredited_fields': unknown},
              'gate': {'path': str(path.relative_to(ROOT)), 'sha256': gate_sha,
                       'identity': identity(gate_sha, src, candidate_sha)},
              'command': {'argv': argv, 'cwd': str(ROOT), 'executed': False,
                          'dispatcher_argv': dispatcher_argv},
              'exit_code': None, 'tests': 0, 'skipped': 0, 'count_reported': False,
              'started_at': now(), 'finished_at': None, 'log': None}
    if src['dirty']:
        record.update(result='dirty', credited=False, finished_at=now())
        write_record(directory, stem, secret, record)
        raise Refuse('dirty', 'source tree is dirty; no gate credit')
    log = directory / (stem + '.log')
    fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    environment = dict(os.environ, PYTHONDONTWRITEBYTECODE='1')
    try:
        with os.fdopen(fd, 'wb') as stream:
            code = subprocess.run(argv, cwd=ROOT, stdin=subprocess.DEVNULL, stdout=stream,
                                  stderr=subprocess.STDOUT, env=environment).returncode
    except OSError:
        raise Refuse('not_executable', 'gate for task ' + selector + ' could not be executed') from None
    output = log.read_bytes()
    counts = COUNT.findall(output.decode('utf-8', errors='replace'))
    tests, skipped = map(int, counts[-1]) if counts else (0, 0)
    after = source()
    if after != src:
        result = 'dirty'
    elif code == 77:
        result = 'skipped'
    elif code:
        result = 'failed'
    elif not counts or tests == 0:
        result = 'empty'
    elif skipped:
        result = 'skipped'
    else:
        result = 'pass'
    record['command']['executed'] = True
    record.update(result=result, credited=result == 'pass', exit_code=code, tests=tests,
                  skipped=skipped, count_reported=bool(counts), finished_at=now(),
                  log={'path': str(log), 'sha256': sha256(output)})
    write_record(directory, stem, secret, record)
    if result != 'pass':
        raise Refuse(result, 'gate for task ' + selector + ' result ' + result)


def check(evidence, argument):
    directory = evidence_dir()
    secret = key(directory)
    try:
        record = load_private(evidence)
        mac = record.pop('hmac')
        if not isinstance(mac, str) or not hmac.compare_digest(mac, sign(secret, record)):
            raise Refuse('fabricated', 'evidence signature does not match')
        if record['schema'] != RECORD:
            raise Refuse('fabricated', 'unsupported evidence schema')
        if record['result'] != 'pass' or record['credited'] is not True:
            kind = record['result'] if record['result'] in EXIT else 'fabricated'
            raise Refuse(kind, 'evidence records no credited pass')
        log = Path(record['log']['path'])
        if log.parent != directory:
            raise Refuse('fabricated', 'evidence log is outside the evidence directory')
        output = log.read_bytes()
        counts = COUNT.findall(output.decode('utf-8', errors='replace'))
        if (sha256(output) != record['log']['sha256'] or not counts or
                [record['tests'], record['skipped']] != list(map(int, counts[-1])) or
                record['tests'] < 1 or record['skipped'] or record['exit_code'] != 0 or
                record['source']['dirty'] is not False):
            raise Refuse('fabricated', 'evidence does not match its log')
        selector = record['selector']
        recorded = (record['source']['revision'], record['source']['tree'])
        gate_sha, candidate_sha = record['gate']['sha256'], record['candidate']['sha256']
    except Refuse:
        raise
    except (Invalid, OSError, ValueError, KeyError, TypeError, AttributeError):
        raise Refuse('fabricated', 'evidence is malformed') from None
    src = source()
    if recorded != (src['revision'], src['tree']):
        raise Refuse('stale', 'evidence revision is not the checkout HEAD')
    if src['dirty']:
        raise Refuse('dirty', 'source tree is dirty')
    path = gate(selector)
    if sha256(path.read_bytes()) != gate_sha:
        raise Refuse('mismatch', 'gate changed since the evidence was recorded')
    if manifest(argument)[1] != candidate_sha:
        raise Refuse('mismatch', 'evidence was recorded against a different candidate')
    print(json.dumps({'selector': selector, 'identity': identity(gate_sha, src, candidate_sha),
                      'evidence': str(Path(evidence).resolve()),
                      'sha256': sha256(Path(evidence).read_bytes())}, sort_keys=True))


def main(args):
    global ROOT, SPEC
    try:
        ROOT = Path(git('rev-parse', '--show-toplevel')).resolve()
        SPEC = ROOT / 'spec/paxeer-x/spec.kvx'
        mode = args[0] if args and args[0] in ('--identity', '--check') else 'run'
        rest = args[1:] if mode != 'run' else list(args)
        argument = None
        if len(rest) == 3 and rest[1] == '--manifest':
            argument = rest[2]
        elif len(rest) != 1:
            raise Refuse('usage', 'usage: verify-task.sh [--identity|--check] <task-id|evidence> [--manifest PATH]')
        if mode == 'run':
            run(rest[0], argument, list(args))
        elif mode == '--check':
            check(rest[0], argument)
        else:
            path, directory, data, candidate_sha, src, gate_sha = prepare(rest[0], argument)
            bind(data, path)
            if src['dirty']:
                raise Refuse('dirty', 'source tree is dirty; no gate credit')
            print(json.dumps({'selector': rest[0], 'identity': identity(gate_sha, src, candidate_sha),
                              'gate_sha256': gate_sha, 'revision': src['revision'],
                              'tree': src['tree'], 'candidate_sha256': candidate_sha}, sort_keys=True))
        return 0
    except Refuse as refusal:
        print('verify-task: refused: ' + str(refusal), file=sys.stderr)
        return refusal.code


sys.exit(main(sys.argv[2:]))
PY
