#!/usr/bin/env bash
set -euo pipefail

usage() {
    cat <<'EOF'
usage: tools/ci/beta-ledger-check.sh [--ledger PATH] [--spec PATH] [--revisions]

Validates every record of the LayerX beta executed-evidence ledger
(spec/layerx-beta/qualification.kvx by default) and prints the set of distinct
gate revisions. Every violation is listed on stderr; the exit status is 1 when
at least one violation exists, 2 on usage or environment errors, 0 otherwise.

  --ledger PATH   ledger to check (default spec/layerx-beta/qualification.kvx)
  --spec PATH     feature spec used to resolve tasks and acceptance criteria
                  (default spec/layerx-beta/spec.kvx)
  --evidence-root PATH  private evidence directory outside source (required)
  --candidate REV candidate revision (default current HEAD)
  --revisions     print only the distinct gate revisions, one per line

Parsing rules (mirroring spec/specgen/kvx.go):
  * a line whose trimmed text is [name] opens a section; every later key = value
    line belongs to it; a section name or a key repeated inside one section is
    a violation
  * comments start at a # outside double quotes and run to end of line
  * a value is a double-quoted string ("..." with \" and \\ escapes), a list
    ([...] split on commas outside quotes, each item a quoted string) or a bare
    scalar; a value containing ${...} is a violation because ledger values must
    be literal
  * a section is either gate.<task>.<n> or observation.<task>.<n>; any other
    section name is a violation

Gate records ([gate.<task>.<n>]) must satisfy:
  * keys task, reqs, revision, command, environment, started_at, outcome,
    evidence, source_evidence and note are all present and no other key exists
  * task equals the <task> part of the section name and [task.<task>] exists
    in the feature spec
  * reqs is a non-empty list of <req>.<ac> pairs, each resolving to key ac_<ac>
    under [req.<req>] in the feature spec
  * revision is a 40-hex commit that exists in this repository
    (git cat-file -e <revision>^{commit})
  * command, after any leading VAR=value words, is either
      - make <targets...>: every word that is not an option or VAR=value must
        be a target defined as "<target>:" (multi-target lines are split on
        whitespace) in Makefile or in any file it includes through a literal
        include/-include/sinclude line, transitively; -C, -f, -I, -o and -W are
        not supported
      - sh|bash|python3|python|node <script> ...: the first non-option word
        after the interpreter must be an existing file in the tree
      - <path> ...: a relative path to an executable regular file in the tree
  * environment is a non-empty string
  * started_at matches YYYY-MM-DDTHH:MM:SSZ
  * outcome is pass, fail or blocked; a blocked gate carries a non-empty note
  * evidence and source_evidence resolve inside the explicit private evidence root;
    typed clean start/end source, complete mutation observation, exact command binding
    and immutable evidence digests are required for every format, including plain logs
  * every file named status.json or report.json that is the evidence path or
    lies below an evidence directory is a qualification runner document: a
    JSON object whose schema is layerx-qualification-status-v1 or
    layerx-qualification-report-v1 respectively, whose source_revision equals
    the record revision and whose source_identity equals the clean-tree
    identity sha256(<revision> || 0x00) that
    tools/qualification/release_runner.py computes for a tree with no tracked
    changes and no untracked files

Observation records ([observation.<task>.<n>]) must satisfy:
  * keys task, file, symbol, observed, assumption and severity are present;
    the <task> part of the section name is the task that recorded the
    observation and task (<n> or <n>.<m>) names the task it concerns;
    severity is blocker, suspect, assumption or note
  * no gate key (reqs, revision, command, environment, started_at, outcome,
    evidence, note) and no other unknown key is present
EOF
}

KVX_PARSER='
function trim(s) { sub(/^[ \t]+/, "", s); sub(/[ \t]+$/, "", s); return s }
function strip_comment(s,   i, n, c, inq, out) {
    n = length(s); inq = 0; out = ""
    for (i = 1; i <= n; i++) {
        c = substr(s, i, 1)
        if (inq && c == "\\") { out = out c substr(s, i + 1, 1); i++; continue }
        if (c == "\"") inq = !inq
        else if (c == "#" && !inq) break
        out = out c
    }
    if (inq) return "\001"
    return out
}
function unquote(s,   i, n, c, out) {
    n = length(s); out = ""
    for (i = 2; i < n; i++) {
        c = substr(s, i, 1)
        if (c == "\\") { i++; out = out substr(s, i, 1); continue }
        out = out c
    }
    return out
}
function emit(kind, key, vtype, val) {
    printf "%s\036%s\036%s\036%s\036%s\036%d\n", kind, section, key, vtype, val, NR
}
function split_list(s,   i, n, c, inq, item, out, count) {
    n = length(s); inq = 0; item = ""; out = ""; count = 0
    for (i = 2; i < n; i++) {
        c = substr(s, i, 1)
        if (inq && c == "\\") { item = item c substr(s, i + 1, 1); i++; continue }
        if (c == "\"") { inq = !inq; item = item c; continue }
        if (c == "," && !inq) {
            item = trim(item)
            if (item !~ /^".*"$/) return "\001"
            out = out (count ? "\037" : "") unquote(item); count++; item = ""
            continue
        }
        item = item c
    }
    item = trim(item)
    if (item != "") {
        if (item !~ /^".*"$/) return "\001"
        out = out (count ? "\037" : "") unquote(item); count++
    }
    return "\002" out
}
BEGIN { section = "" }
{
    line = $0
    sub(/\r$/, "", line)
    stripped = strip_comment(line)
    if (stripped == "\001") { emit("error", "", "", "unterminated double-quoted string"); next }
    stripped = trim(stripped)
    if (stripped == "") next
    if (stripped ~ /^\[.*\]$/) {
        section = substr(stripped, 2, length(stripped) - 2)
        emit("section", "", "", "")
        next
    }
    eq = index(stripped, "=")
    if (eq == 0) { emit("error", "", "", "line is neither a section header nor key = value"); next }
    key = trim(substr(stripped, 1, eq - 1))
    val = trim(substr(stripped, eq + 1))
    if (key !~ /^[A-Za-z_][A-Za-z0-9_.-]*$/) { emit("error", key, "", "invalid key"); next }
    if (index(val, "${") > 0) { emit("error", key, "", "value contains ${...} interpolation; ledger values must be literal"); next }
    if (val ~ /^".*"$/) { emit("pair", key, "string", unquote(val)); next }
    if (val ~ /^\[.*\]$/) {
        items = split_list(val)
        if (items == "\001") { emit("error", key, "", "list items must be double-quoted strings"); next }
        emit("pair", key, "list", substr(items, 2))
        next
    }
    emit("pair", key, "scalar", val)
}
'

source_evidence() {
    python3 - "$@" <<'PY_SOURCE'
import argparse
import ctypes
import datetime
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import stat
import struct
import subprocess
import sys
import uuid

SCHEMA = 'layerx-focused-source-evidence-v1'
LIMIT = 16 * 1024 * 1024


class EvidenceError(ValueError):
    pass


def need(value, message):
    if not value:
        raise EvidenceError(message)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':')).encode()


def sha(value):
    return hashlib.sha256(value).hexdigest()


def pairs(items):
    result = {}
    for key, value in items:
        need(key not in result, 'duplicate JSON key')
        result[key] = value
    return result


def secret_path(path):
    name = Path(path).name.lower()
    return (name in ('.env', '.env.local', '.env.production', '.env.development',
                     'credentials', 'credentials.json', 'id_rsa', 'id_ed25519')
            or name.endswith(('.key', '.pem')))


def private_root(path, root):
    path = Path(path).absolute()
    need(path == path.resolve() and root != path and root not in path.parents,
         'evidence root must be canonical and outside source')
    info = path.stat()
    need(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid() and
         info.st_mode & 0o077 == 0, 'evidence root must be owned and private')
    return path


def protected(path, base, directory=False):
    path = Path(path)
    path = path if path.is_absolute() else base / path
    need(path == path.resolve() and base in path.parents, 'evidence escapes private root')
    need(not secret_path(path), 'credential paths forbidden')
    info = path.stat()
    need(info.st_uid == os.geteuid() and info.st_mode & 0o077 == 0,
         'evidence permissions invalid')
    if directory:
        need(stat.S_ISDIR(info.st_mode), 'evidence directory required')
    else:
        need(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_size <= LIMIT,
             'bounded regular evidence file required')
    return path


def load(path, base):
    return json.loads(protected(path, base).read_text(), object_pairs_hook=pairs)


def atomic(path, value, replace=False):
    temporary = path.with_name('.source-' + uuid.uuid4().hex + '.tmp')
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(canonical(value) + b'\n')
        stream.flush()
        os.fsync(stream.fileno())
    if replace:
        os.replace(temporary, path)
    else:
        os.link(temporary, path)
        temporary.unlink()


def git(root, *args):
    result = subprocess.run(['git', '--no-optional-locks', '-C', str(root), *args],
                            stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                            stderr=subprocess.DEVNULL, timeout=20)
    need(result.returncode == 0, 'source inspection failed')
    return result.stdout


def source_paths(root, depth=0):
    need(depth <= 8, 'submodule nesting exceeds inspection bound')
    paths = set(p for p in git(root, 'ls-files', '-z', '--cached', '--others',
                              '--exclude-standard').split(b'\0') if p)
    for row in git(root, 'ls-files', '--stage', '-z').split(b'\0'):
        if row.startswith(b'160000 ') and b'\t' in row:
            name = row.split(b'\t', 1)[1]
            directory = root / os.fsdecode(name)
            if directory.is_dir() and not directory.is_symlink() and (directory / '.git').exists():
                paths.update(name + b'/' + child for child in source_paths(directory, depth + 1))
    return sorted(paths)


def snapshot(root):
    revision = git(root, 'rev-parse', 'HEAD').decode().strip()
    tree = git(root, 'rev-parse', 'HEAD^{tree}').decode().strip()
    paths = source_paths(root)
    sensitive = [os.fsdecode(path) for path in paths if secret_path(os.fsdecode(path))]
    args = ['status', '--porcelain=v2', '-z', '--untracked-files=all', '--ignore-submodules=none', '--', '.']
    args.extend(':(exclude,literal)' + path for path in sensitive)
    status_bytes = git(root, *args)
    metadata = []
    for name in paths:
        path = root / os.fsdecode(name)
        try:
            info = path.lstat()
            fields = [info.st_mode, info.st_size, info.st_mtime_ns, info.st_ctime_ns]
        except FileNotFoundError:
            fields = None
        metadata.append([name.hex(), fields])
    inspection = not sensitive
    clean = not status_bytes and inspection
    state_digest = sha(status_bytes)
    metadata_digest = sha(canonical(metadata))
    return {'revision': revision, 'tree': tree, 'clean': clean,
            'source_identity': sha(revision.encode() + b'\0') if clean else
                               sha(canonical([revision, state_digest, metadata_digest])),
            'identity_algorithm': 'clean:git-commit-sha256-v1' if clean else
                                  'dirty:git-status-metadata-sha256-v1',
            'status_sha256': state_digest, 'metadata_sha256': metadata_digest,
            'index_tree': 'status-sha256:' + state_digest, 'inspection_ok': inspection}


class Monitor:
    def __init__(self, root):
        self.root = root
        self.complete = True
        self.changed = False
        self.watches = {}
        self.fd = -1
        try:
            libc = ctypes.CDLL(None, use_errno=True)
            self.fd = libc.inotify_init1(os.O_NONBLOCK | os.O_CLOEXEC)
            need(self.fd >= 0, 'mutation monitor unavailable')
            directories = {root}
            inspection_roots = {root}
            for raw in source_paths(root):
                path = root / os.fsdecode(raw)
                if path.is_dir() and (path / '.git').exists():
                    inspection_roots.add(path)
                    directories.add(path)
                parent = path.parent
                while parent != root and root in parent.parents:
                    directories.add(parent)
                    parent = parent.parent
            gitdirs = set()
            for inspection_root in inspection_roots:
                gitdir = Path(git(inspection_root, 'rev-parse', '--absolute-git-dir').decode().strip())
                common = Path(git(inspection_root, 'rev-parse', '--path-format=absolute', '--git-common-dir').decode().strip())
                gitdirs.update((gitdir, common))
                for refs in (gitdir / 'refs', common / 'refs'):
                    if refs.is_dir():
                        gitdirs.add(refs)
                        gitdirs.update(path for path in refs.rglob('*') if path.is_dir())
            for directory in directories | gitdirs:
                if directory.is_symlink():
                    self.complete = False
                    continue
                watch = libc.inotify_add_watch(self.fd, os.fsencode(directory), 0x00000FCE)
                need(watch >= 0, 'mutation watch unavailable')
                self.watches[watch] = (directory, directory in gitdirs)
        except (OSError, ValueError):
            self.complete = False

    def finish(self):
        if self.fd >= 0:
            try:
                while True:
                    data = os.read(self.fd, 65536)
                    offset = 0
                    while offset < len(data):
                        watch, mask, cookie, size = struct.unpack_from('iIII', data, offset)
                        name = os.fsdecode(data[offset + 16:offset + 16 + size].split(b'\0')[0])
                        offset += 16 + size
                        if mask & (0x4000 | 0x8000):
                            self.complete = False
                        item = self.watches.get(watch)
                        if item is None:
                            self.complete = False
                            continue
                        directory, internal = item
                        if internal and directory.name != 'refs' and 'refs' not in directory.parts:
                            if name not in ('HEAD', 'index', 'packed-refs', 'config'):
                                continue
                        if not internal:
                            relative = str((directory / name).relative_to(self.root))
                            ignored = subprocess.run(['git', '--no-optional-locks', '-C', str(self.root),
                                                      'check-ignore', '-q', '--', relative],
                                                     stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                                     timeout=10).returncode
                            if ignored == 0:
                                continue
                            if ignored not in (0, 1):
                                self.complete = False
                        self.changed = True
            except BlockingIOError:
                pass
            except (OSError, subprocess.TimeoutExpired):
                self.complete = False
            finally:
                os.close(self.fd)
        return {'method': 'linux-inotify-source-and-git-v1',
                'complete': self.complete, 'changed': self.changed}


def evidence_digest(reference, base, sidecar):
    path = Path(reference)
    path = path if path.is_absolute() else base / path
    if path.is_dir():
        protected(path, base, True)
        files = sorted(path.rglob('*'))
        need(len(files) <= 10000, 'evidence directory exceeds bound')
        rows = []
        for file in files:
            if file == sidecar:
                continue
            need(not file.is_symlink(), 'evidence symlink forbidden')
            if file.is_dir():
                protected(file, base, True)
                continue
            rows.append([str(file.relative_to(path)), sha(protected(file, base).read_bytes())])
        digest = sha(canonical(rows))
        kind = 'directory'
    else:
        files = [protected(path, base)]
        digest = sha(files[0].read_bytes())
        kind = 'file'
    return {'reference': reference, 'kind': kind, 'sha256': digest}, files


def reasons(record, candidate):
    result = set()
    start, end = record['source_start'], record['source_end']
    if record['state'] != 'complete': result.add('incomplete')
    if record['development_outcome'] != 'pass' or record['command_exit_status'] != 0:
        result.add('blocked' if not record['executed'] else 'command_failed')
    if not record['executed']: result.add('blocked')
    if not start['inspection_ok'] or not end['inspection_ok']: result.add('source_inspection_failed')
    if not start['clean']: result.add('dirty_start')
    if not end['clean']: result.add('dirty_end')
    if start['revision'] != end['revision']: result.add('revision_changed')
    if start['revision'] != candidate or end['revision'] != candidate: result.add('candidate_mismatch')
    if start != end or record['mutation_observation']['changed']: result.add('source_changed')
    if not record['mutation_observation']['complete']: result.add('mutation_observation_incomplete')
    return sorted(result)


def validate(record, base, candidate, root):
    need(re.fullmatch('[0-9a-f]{40}', candidate or ''), 'candidate revision required')
    expected_tree = git(root, 'rev-parse', '--verify', candidate + '^{tree}').decode().strip()
    sidecar = protected(record['source_evidence'], base)
    value = load(sidecar, base)
    fields = {'schema', 'run_id', 'state', 'command', 'argv', 'environment', 'started_at',
              'finished_at', 'source_start', 'source_end', 'development_outcome',
              'command_exit_status', 'executed', 'release_eligible', 'ineligibility_reasons',
              'evidence', 'mutation_observation'}
    need(isinstance(value, dict) and set(value) == fields and value['schema'] == SCHEMA, 'invalid source schema')
    need(re.fullmatch('[0-9a-f]{32}', value['run_id']), 'invalid run identity')
    need(value['state'] == 'complete', 'source evidence incomplete')
    need(type(value['executed']) is bool and type(value['release_eligible']) is bool,
         'invalid eligibility type')
    need(type(value['command_exit_status']) is int and 0 <= value['command_exit_status'] <= 255,
         'invalid command exit status')
    need(value['development_outcome'] in ('pass', 'fail', 'blocked'), 'invalid development outcome')
    need((value['development_outcome'] == 'pass') == (value['executed'] and value['command_exit_status'] == 0),
         'command outcome mismatch')
    for key in ('command', 'environment', 'started_at'):
        need(isinstance(value[key], str) and value[key] and value[key] == record[key], 'source binding mismatch')
    need(value['development_outcome'] == record['outcome'] and record['revision'] == candidate,
         'candidate or outcome mismatch')
    need(isinstance(value['argv'], list) and value['argv'] and
         all(isinstance(word, str) for word in value['argv']) and
         shlex.join(value['argv']) == value['command'], 'command argv mismatch')
    for key in ('started_at', 'finished_at'):
        need(isinstance(value[key], str) and re.fullmatch(r'\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z', value[key]),
             'invalid timestamp')
        datetime.datetime.strptime(value[key], '%Y-%m-%dT%H:%M:%SZ')
    need(value['finished_at'] >= value['started_at'], 'reversed timestamps')
    for key in ('source_start', 'source_end'):
        source = value[key]
        need(isinstance(source, dict) and set(source) == {'revision', 'tree', 'clean', 'source_identity',
             'identity_algorithm', 'status_sha256', 'metadata_sha256', 'index_tree', 'inspection_ok'},
             'invalid source snapshot')
        need(type(source['clean']) is bool and type(source['inspection_ok']) is bool, 'invalid source flags')
        for field in ('revision', 'tree'):
            need(isinstance(source[field], str) and re.fullmatch('[0-9a-f]{40}', source[field]), 'invalid source object')
        for field in ('source_identity', 'status_sha256', 'metadata_sha256'):
            need(isinstance(source[field], str) and re.fullmatch('[0-9a-f]{64}', source[field]), 'invalid source digest')
        need(source['index_tree'] == 'status-sha256:' + source['status_sha256'], 'invalid staged identity')
        expected = 'clean:git-commit-sha256-v1' if source['clean'] else 'dirty:git-status-metadata-sha256-v1'
        need(source['identity_algorithm'] == expected, 'invalid identity algorithm')
        if source['clean']:
            need(source['tree'] == expected_tree, 'candidate tree mismatch')
            need(source['source_identity'] == sha(source['revision'].encode() + b'\0') and
                 source['status_sha256'] == sha(b'') and source['inspection_ok'], 'invalid clean identity')
    monitor = value['mutation_observation']
    need(isinstance(monitor, dict) and set(monitor) == {'method', 'complete', 'changed'} and
         monitor['method'] == 'linux-inotify-source-and-git-v1' and
         type(monitor['complete']) is bool and type(monitor['changed']) is bool, 'invalid mutation observation')
    actual, files = evidence_digest(record['evidence'], base, sidecar)
    need(actual == value['evidence'], 'evidence digest or reference changed')
    for path in files:
        if path.name in ('status.json', 'report.json'):
            nested = load(path, base)
            expected = 'layerx-qualification-' + path.stem + '-v1'
            need(nested.get('schema') == expected and nested.get('source_revision') == candidate and
                 nested.get('source_identity') == sha(candidate.encode() + b'\0'), 'nested runner identity mismatch')
    why = reasons(value, candidate)
    need(value['ineligibility_reasons'] == why and value['release_eligible'] == (not why),
         'eligibility declaration contradicts evidence')
    return {'eligible': not why, 'reasons': why, 'sidecar_sha256': sha(sidecar.read_bytes()),
            'evidence_sha256': actual['sha256']}


def recipe(path, task):
    sections = {}
    section = None
    for line in Path(path).read_text().splitlines():
        match = re.fullmatch(r'\[([^]]+)\]\s*', line)
        if match:
            section = match[1]
            need(section not in sections, 'duplicate recipe section')
            sections[section] = {}
        elif section and re.match(r'^[a-z_][a-z0-9_]*\s*=', line):
            key, raw = line.split('=', 1)
            sections[section][key.strip()] = raw.strip()
    need('task.' + task in sections, 'task absent from selected spec')
    block = sections['task.' + task]
    commands = [json.loads(block['verify_cmd'])]
    reqs = []
    for req in json.loads(block['reqs']):
        reqs.extend(req + '.' + key[3:] for key in sections['req.' + req] if re.fullmatch('ac_[0-9]+', key))
    need(reqs, 'task has no acceptance criteria')
    return commands, reqs


def run(args, root, base):
    expected, reqs = recipe(args.spec, args.task)
    commands = args.commands
    need(commands == expected, 'focused commands differ from selected task spec')
    ledger = Path(args.ledger).absolute()
    need(base in ledger.parents and ledger == ledger.resolve(), 'ledger must be inside private evidence root')
    environment = 'python=' + sys.version.split()[0] + '; platform=' + sys.platform + '; machine=' + os.uname().machine
    failure = False
    for command in commands:
        need(re.fullmatch(r'make(?: [A-Za-z0-9_-]+)+', command), 'expected plain make targets')
        argv = shlex.split(command)
        identifier = uuid.uuid4().hex
        directory = base / identifier
        directory.mkdir(mode=0o700)
        log = directory / 'command.log' if args.evidence_kind == 'directory' else base / (identifier + '.log')
        sidecar = directory / 'source-evidence.json' if args.evidence_kind == 'directory' else base / (identifier + '.log.source.json')
        evidence = str(directory if args.evidence_kind == 'directory' else log)
        monitor = Monitor(root)
        start = snapshot(root)
        now = lambda: datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')
        value = {'schema': SCHEMA, 'run_id': identifier, 'state': 'running', 'command': command,
                 'argv': argv, 'environment': environment, 'started_at': now(), 'finished_at': None,
                 'source_start': start, 'source_end': None, 'development_outcome': 'blocked',
                 'command_exit_status': 125, 'executed': False, 'release_eligible': False,
                 'ineligibility_reasons': ['incomplete'], 'evidence': None,
                 'mutation_observation': {'method': 'linux-inotify-source-and-git-v1',
                                          'complete': False, 'changed': False}}
        atomic(sidecar, value)
        blocked = any(word in ('platform-beta-cluster-up', 'platform-hosted-smoke',
                               'platform-beta-cluster-down') for word in argv)
        with log.open('xb') as output:
            if blocked:
                output.write(b'Cluster operation requires separate owner authorization.\n')
                code = 125
            else:
                os.environ['LAYERX_QUALIFICATION_ARTIFACT_DIR'] = str(directory)
                try:
                    process = subprocess.run(argv, cwd=root, stdout=output, stderr=subprocess.STDOUT,
                                             stdin=subprocess.DEVNULL, timeout=args.timeout)
                    code = process.returncode if process.returncode >= 0 else 128 - process.returncode
                except subprocess.TimeoutExpired:
                    code = 124
        value.update(state='complete', finished_at=now(), source_end=snapshot(root),
                     development_outcome='blocked' if blocked else 'pass' if code == 0 else 'fail',
                     command_exit_status=code, executed=not blocked,
                     mutation_observation=monitor.finish())
        value['evidence'], unused = evidence_digest(evidence, base, sidecar)
        value['ineligibility_reasons'] = reasons(value, start['revision'])
        value['release_eligible'] = not value['ineligibility_reasons']
        atomic(sidecar, value, True)
        record = {'task': args.task, 'reqs': reqs, 'revision': start['revision'], 'command': command,
                  'environment': environment, 'started_at': value['started_at'],
                  'outcome': value['development_outcome'], 'evidence': evidence,
                  'source_evidence': str(sidecar),
                  'note': 'Owner authorization required' if blocked else
                          'development outcome retained; release eligibility requires source evidence'}
        with ledger.open('a+') as stream:
            os.fchmod(stream.fileno(), 0o600)
            fcntl.flock(stream, fcntl.LOCK_EX)
            stream.seek(0)
            ordinals = re.findall(r'^\[gate\.' + re.escape(args.task) + r'\.(\d+)\]$', stream.read(), re.M)
            ordinal = max(map(int, ordinals), default=0) + 1
            stream.write('\n[gate.' + args.task + '.' + str(ordinal) + ']\n')
            for key, item in record.items():
                stream.write(key + ' = ' + json.dumps(item) + '\n')
            stream.flush()
            os.fsync(stream.fileno())
        print(json.dumps({'outcome': value['development_outcome'], 'command_exit_status': code,
                          'release_eligible': value['release_eligible'], 'source_evidence': str(sidecar)}))
        failure |= code != 0
    return int(failure)


parser = argparse.ArgumentParser(description='Typed focused source evidence; private inputs only, no credential values.')
parser.add_argument('operation', choices=['run', 'validate', 'capture'])
parser.add_argument('--root', required=True)
parser.add_argument('--evidence-root', required=True)
parser.add_argument('--candidate')
parser.add_argument('--record')
parser.add_argument('--ledger')
parser.add_argument('--spec')
parser.add_argument('--task')
parser.add_argument('--timeout', type=int, default=600)
parser.add_argument('--evidence-kind', choices=['file', 'directory'], default='file')
parser.add_argument('commands', nargs='*')
arguments = sys.argv[1:]
trailing = []
if '--' in arguments:
    separator = arguments.index('--')
    trailing = arguments[separator + 1:]
    arguments = arguments[:separator]
args = parser.parse_args(arguments)
if trailing:
    args.commands = trailing
try:
    root = Path(args.root).resolve()
    base = private_root(args.evidence_root, root)
    if args.operation == 'run':
        need(args.ledger and args.spec and args.task and 1 <= args.timeout <= 1800, 'missing run inputs')
        sys.exit(run(args, root, base))
    elif args.operation == 'capture':
        print(json.dumps(snapshot(root)))
    else:
        record = json.loads(args.record, object_pairs_hook=pairs)
        result = validate(record, base, args.candidate, root)
        print(json.dumps(result, sort_keys=True))
        sys.exit(0 if result['eligible'] else 1)
except EvidenceError as error:
    print(json.dumps({'eligible': False, 'reasons': [str(error)]}))
    sys.exit(1)
except (OSError, ValueError, KeyError, TypeError, AttributeError, subprocess.TimeoutExpired):
    print(json.dumps({'eligible': False, 'reasons': ['invalid_or_unavailable_source_evidence']}))
    sys.exit(1)
PY_SOURCE
}

beta_ledger_check() {
    local root ledger="" spec="" evidence_root="${PAXEER_X_EVIDENCE_DIR:-}" candidate="" revisions_only=0
    root=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
    while [ "$#" -gt 0 ]; do
        case $1 in
        --ledger)
            [ "$#" -ge 2 ] || { usage >&2; return 2; }
            ledger=$2
            shift 2
            ;;
        --spec)
            [ "$#" -ge 2 ] || { usage >&2; return 2; }
            spec=$2
            shift 2
            ;;
        --evidence-root|--candidate)
            [ "$#" -ge 2 ] || return 2
            if [ "$1" = --candidate ]; then candidate=$2; else evidence_root=$2; fi
            shift 2
            ;;
        --revisions)
            revisions_only=1
            shift
            ;;
        -h | --help)
            usage
            return 0
            ;;
        *)
            usage >&2
            return 2
            ;;
        esac
    done
    ledger=${ledger:-$root/spec/layerx-beta/qualification.kvx}
    spec=${spec:-$root/spec/paxeer-x/spec.kvx}
    candidate=${candidate:-$(git -C "$root" rev-parse HEAD)}
    [ -n "$evidence_root" ] || { echo "beta-ledger-check: private --evidence-root required" >&2; return 2; }
    [ -f "$ledger" ] || { echo "beta-ledger-check: ledger not found: $ledger" >&2; return 2; }
    [ -f "$spec" ] || { echo "beta-ledger-check: feature spec not found: $spec" >&2; return 2; }
    command -v python3 >/dev/null 2>&1 || { echo "beta-ledger-check: python3 is required" >&2; return 2; }
    git -C "$root" rev-parse --git-dir >/dev/null 2>&1 || { echo "beta-ledger-check: $root is not a git repository" >&2; return 2; }

    local -a violations=() order=()
    local -A value=() vtype=() lineno=() section_line=() section_keys=()
    local -A spec_sections=() spec_keys=()
    local -A make_targets=() makefiles_seen=()
    local -A revisions=()
    local kind section key type val ln
    local gates=0 observations=0

    while IFS=$'\036' read -r kind section key type val ln; do
        case $kind in
        error)
            violations+=("$ledger:$ln: $val")
            ;;
        section)
            if [ -n "${section_line[$section]+x}" ]; then
                violations+=("$ledger:$ln: duplicate section [$section] (first at line ${section_line[$section]})")
            else
                section_line[$section]=$ln
                section_keys[$section]=""
                order+=("$section")
            fi
            ;;
        pair)
            if [ -z "$section" ]; then
                violations+=("$ledger:$ln: key '$key' appears before any section")
                continue
            fi
            if [ -n "${value[$section$'\037'$key]+x}" ]; then
                violations+=("$ledger:$ln: duplicate key '$key' in [$section]")
                continue
            fi
            value[$section$'\037'$key]=$val
            vtype[$section$'\037'$key]=$type
            lineno[$section$'\037'$key]=$ln
            section_keys[$section]="${section_keys[$section]} $key"
            ;;
        esac
    done < <(awk "$KVX_PARSER" "$ledger")

    while IFS=$'\036' read -r kind section key type val ln; do
        case $kind in
        section) spec_sections[$section]=1 ;;
        pair) spec_keys[$section$'\037'$key]=1 ;;
        esac
    done < <(awk "$KVX_PARSER" "$spec")

    collect_makefile() {
        local file=$1 include
        [ -n "${makefiles_seen[$file]+x}" ] && return 0
        makefiles_seen[$file]=1
        [ -f "$root/$file" ] || return 0
        local target line
        while IFS= read -r line; do
            for target in ${line%%:*}; do
                make_targets[$target]=1
            done
        done < <(grep -E '^[^[:space:]#=:$][^=:#]*:([^=]|$)' "$root/$file" || true)
        while IFS= read -r include; do
            collect_makefile "$include"
        done < <(sed -n -E 's/^-?s?include[[:space:]]+([^[:space:]$]+)[[:space:]]*$/\1/p' "$root/$file")
    }
    collect_makefile Makefile

    in_tree_path() {
        case $1 in
        /* | "" | ../* | */../* | */.. | ..) return 1 ;;
        esac
        return 0
    }

    command_violation() {
        local command=$1 word w script=""
        local -a words=()
        read -r -a words <<<"$command"
        while [ "${#words[@]}" -gt 0 ] && [[ ${words[0]} =~ ^[A-Za-z_][A-Za-z0-9_]*= ]]; do
            words=("${words[@]:1}")
        done
        [ "${#words[@]}" -gt 0 ] || { echo "command names no executable"; return 0; }
        word=${words[0]}
        case $word in
        make)
            local found=0
            for w in "${words[@]:1}"; do
                case $w in
                -C | -f | -I | -o | -W) echo "make option $w is not supported; only the top-level Makefile and its includes are recognised"; return 0 ;;
                -*) continue ;;
                *=*) continue ;;
                esac
                found=1
                [ -n "${make_targets[$w]+x}" ] || echo "make target '$w' is not defined in Makefile or an included file"
            done
            [ "$found" -eq 1 ] || echo "make command names no target"
            ;;
        sh | bash | python3 | python | node)
            for w in "${words[@]:1}"; do
                case $w in -*) continue ;; esac
                script=$w
                break
            done
            [ -n "$script" ] || { echo "interpreter command names no script"; return 0; }
            in_tree_path "$script" && [ -f "$root/$script" ] || echo "script '$script' is not a file in the tree"
            ;;
        *)
            in_tree_path "$word" && [ -f "$root/$word" ] && [ -x "$root/$word" ] || echo "'$word' is not an executable file in the tree"
            ;;
        esac
    }

    runner_document_violations() {
        local file=$1 revision=$2
        python3 - "$file" "$revision" "$root" <<'PY'
import hashlib
import json
import os
import sys

path, revision, root = sys.argv[1:4]
shown = os.path.relpath(path, root)
expected = {
    "status.json": "layerx-qualification-status-v1",
    "report.json": "layerx-qualification-report-v1",
}[os.path.basename(path)]
try:
    with open(path, "rb") as handle:
        document = json.load(handle)
except (OSError, ValueError) as error:
    print(f"{shown}: not a JSON document ({error})")
    sys.exit(0)
if not isinstance(document, dict):
    print(f"{shown}: not a JSON object")
    sys.exit(0)
schema = document.get("schema")
if schema != expected:
    print(f"{shown}: schema {schema!r} is not {expected!r}")
source_revision = document.get("source_revision")
if source_revision != revision:
    print(f"{shown}: source_revision {source_revision!r} differs from the record revision {revision}")
identity = hashlib.sha256(revision.encode("ascii") + b"\0").hexdigest()
source_identity = document.get("source_identity")
if source_identity != identity:
    print(f"{shown}: source_identity {source_identity!r} is not the clean-tree identity {identity} for revision {revision}")
PY
    }

    local gate_keys="task reqs revision command environment started_at outcome evidence source_evidence note"
    local observation_keys="task file symbol observed assumption severity"
    local record_task task_part req present line_ref
    for section in "${order[@]}"; do
        line_ref="$ledger:${section_line[$section]}"
        if [[ $section =~ ^gate\.([0-9]+(\.[0-9]+)?)\.([0-9]+)$ ]]; then
            gates=$((gates + 1))
            task_part=${BASH_REMATCH[1]}
            for key in $gate_keys; do
                [ -n "${value[$section$'\037'$key]+x}" ] || violations+=("$line_ref: [$section] lacks key '$key'")
            done
            for key in ${section_keys[$section]}; do
                case " $gate_keys " in
                *" $key "*) ;;
                *) violations+=("$line_ref: [$section] carries unknown key '$key'") ;;
                esac
            done
            record_task=${value[$section$'\037'task]-}
            if [ -n "${value[$section$'\037'task]+x}" ]; then
                [ "$record_task" = "$task_part" ] || violations+=("$line_ref: [$section] task '$record_task' differs from the section task '$task_part'")
                [ -n "${spec_sections[task.$record_task]+x}" ] || violations+=("$line_ref: [$section] task '$record_task' is not a [task.*] section of $spec")
            fi
            if [ -n "${value[$section$'\037'reqs]+x}" ]; then
                if [ "${vtype[$section$'\037'reqs]}" != list ] || [ -z "${value[$section$'\037'reqs]}" ]; then
                    violations+=("$line_ref: [$section] reqs must be a non-empty list")
                else
                    while IFS= read -r -d $'\037' req || [ -n "$req" ]; do
                        if [[ $req =~ ^([0-9]+)\.([0-9]+)$ ]]; then
                            [ -n "${spec_keys[req.${BASH_REMATCH[1]}$'\037'ac_${BASH_REMATCH[2]}]+x}" ] || violations+=("$line_ref: [$section] req '$req' does not resolve to ac_${BASH_REMATCH[2]} under [req.${BASH_REMATCH[1]}] in $spec")
                        else
                            violations+=("$line_ref: [$section] req '$req' is not of the form <req>.<ac>")
                        fi
                    done < <(printf '%s\037' "${value[$section$'\037'reqs]}")
                fi
            fi
            if [ -n "${value[$section$'\037'revision]+x}" ]; then
                val=${value[$section$'\037'revision]}
                if [[ $val =~ ^[0-9a-f]{40}$ ]] && git -C "$root" cat-file -e "$val^{commit}" 2>/dev/null; then
                    revisions[$val]=1
                else
                    violations+=("$line_ref: [$section] revision '$val' is not a commit in this repository")
                fi
            fi
            if [ -n "${value[$section$'\037'command]+x}" ]; then
                while IFS= read -r val; do
                    [ -z "$val" ] || violations+=("$line_ref: [$section] command '${value[$section$'\037'command]}': $val")
                done < <(command_violation "${value[$section$'\037'command]}")
            fi
            if [ -n "${value[$section$'\037'environment]+x}" ] && [ -z "${value[$section$'\037'environment]}" ]; then
                violations+=("$line_ref: [$section] environment is empty")
            fi
            if [ -n "${value[$section$'\037'started_at]+x}" ] && ! [[ ${value[$section$'\037'started_at]} =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]]; then
                violations+=("$line_ref: [$section] started_at '${value[$section$'\037'started_at]}' is not YYYY-MM-DDTHH:MM:SSZ")
            fi
            if [ -n "${value[$section$'\037'outcome]+x}" ]; then
                case ${value[$section$'\037'outcome]} in
                pass | fail) ;;
                blocked)
                    [ -n "${value[$section$'\037'note]-}" ] || violations+=("$line_ref: [$section] is blocked without a note naming the owner action")
                    ;;
                *) violations+=("$line_ref: [$section] outcome '${value[$section$'\037'outcome]}' is not pass, fail or blocked") ;;
                esac
            fi
            local record_json source_result
            record_json=$(python3 - "${value[$section$'\037'revision]-}" \
                "${value[$section$'\037'command]-}" "${value[$section$'\037'environment]-}" \
                "${value[$section$'\037'started_at]-}" "${value[$section$'\037'outcome]-}" \
                "${value[$section$'\037'evidence]-}" "${value[$section$'\037'source_evidence]-}" <<'PY'
import json, sys
print(json.dumps(dict(zip(('revision', 'command', 'environment', 'started_at', 'outcome',
                          'evidence', 'source_evidence'), sys.argv[1:]))))
PY
            )
            if ! source_result=$(source_evidence validate --root "$root" --evidence-root "$evidence_root" \
                                 --candidate "$candidate" --record "$record_json"); then
                violations+=("$line_ref: [$section] source evidence is not release eligible: $source_result")
            fi
        elif [[ $section =~ ^observation\.([0-9]+(\.[0-9]+)?)\.([0-9]+)$ ]]; then
            observations=$((observations + 1))
            task_part=${BASH_REMATCH[1]}
            for key in $observation_keys; do
                [ -n "${value[$section$'\037'$key]+x}" ] || violations+=("$line_ref: [$section] lacks key '$key'")
            done
            for key in ${section_keys[$section]}; do
                case " $observation_keys " in
                *" $key "*) continue ;;
                esac
                case " reqs revision command environment started_at outcome evidence note " in
                *" $key "*) violations+=("$line_ref: [$section] carries gate key '$key'") ;;
                *) violations+=("$line_ref: [$section] carries unknown key '$key'") ;;
                esac
            done
            if [ -n "${value[$section$'\037'task]+x}" ] && ! [[ ${value[$section$'\037'task]} =~ ^[0-9]+(\.[0-9]+)?$ ]]; then
                violations+=("$line_ref: [$section] task '${value[$section$'\037'task]}' is not a task identifier")
            fi
            if [ -n "${value[$section$'\037'severity]+x}" ]; then
                case ${value[$section$'\037'severity]} in
                blocker | suspect | assumption | note) ;;
                *) violations+=("$line_ref: [$section] severity '${value[$section$'\037'severity]}' is not blocker, suspect, assumption or note") ;;
                esac
            fi
        else
            violations+=("$line_ref: [$section] is neither gate.<task>.<n> nor observation.<task>.<n>")
        fi
    done

    local -a distinct=()
    if [ "${#revisions[@]}" -gt 0 ]; then
        while IFS= read -r val; do distinct+=("$val"); done < <(printf '%s\n' "${!revisions[@]}" | sort)
    fi

    if [ "${#violations[@]}" -gt 0 ]; then
        printf 'beta-ledger-check: %d violation(s)\n' "${#violations[@]}" >&2
        printf '  %s\n' "${violations[@]}" >&2
        return 1
    fi
    if [ "$revisions_only" -eq 1 ]; then
        [ "${#distinct[@]}" -eq 0 ] || printf '%s\n' "${distinct[@]}"
        return 0
    fi
    printf 'beta-ledger-check: %d gate record(s), %d observation record(s) in %s\n' "$gates" "$observations" "${ledger#"$root"/}"
    printf 'beta-ledger-check: distinct gate revisions (%d):\n' "${#distinct[@]}"
    [ "${#distinct[@]}" -eq 0 ] || printf '  %s\n' "${distinct[@]}"
    return 0
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
    if [[ ${1:-} == --source-* ]]; then
        operation=${1#--source-}
        shift
        source_evidence "$operation" "$@"
    else
        beta_ledger_check "$@"
    fi
fi
