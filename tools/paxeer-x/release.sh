#!/usr/bin/env bash
# Usage: release.sh [--manifest PATH] <scope-file|task-id|req.N>...
# Resolves the scope to registered task gates (req.N expands to every task listing N in its reqs),
# refuses an empty scope, unknown or unregistered selectors and requirements without a gate,
# runs each distinct gate identity once through verify-task.sh and writes one release record to
# $PAXEER_X_EVIDENCE_DIR referencing every task record. Refusal codes are those of verify-task.sh.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
exec python3 - "$here" "$@" <<'PY'
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

HERE = Path(sys.argv[1])
VERIFY = HERE / 'verify-task.sh'


def refuse(code, reason):
    print('release: refused: ' + reason, file=sys.stderr)
    sys.exit(code)


def verify(*args):
    return subprocess.run([str(VERIFY), *args, *manifest], stdin=subprocess.DEVNULL,
                          capture_output=True, text=True)


args = sys.argv[2:]
manifest = []
if len(args) >= 2 and args[0] == '--manifest':
    manifest, args = args[:2], args[2:]
tokens = []
for argument in args:
    path = Path(argument)
    if path.is_file():
        tokens += re.sub(r'#.*', '', path.read_text(encoding='utf-8')).split()
    else:
        tokens.append(argument)
if not tokens:
    refuse(2, 'empty release scope')

root = Path(subprocess.run(['git', '-C', str(HERE), 'rev-parse', '--show-toplevel'],
                           capture_output=True, text=True, check=True).stdout.strip())
reqs = {}
for block in re.split(r'(?m)^(?=\[)', (root / 'spec/paxeer-x/spec.kvx').read_text(encoding='utf-8')):
    header = re.match(r'\[task\.([0-9.]+)\]\s*\n', block)
    if header:
        found = re.search(r'^reqs\s*=\s*(\[.*\])\s*$', block, re.M)
        reqs[header.group(1)] = json.loads(found.group(1)) if found else []

selectors = []
for token in tokens:
    if token.startswith('req.'):
        mapped = [task for task, listed in reqs.items() if token[4:] in listed]
        if not mapped:
            refuse(3, 'requirement ' + token + ' has no task gate')
        selectors += mapped
    else:
        selectors.append(token)
selectors = list(dict.fromkeys(selectors))

identities = {}
for selector in selectors:
    result = verify('--identity', selector)
    if result.returncode:
        sys.stderr.write(result.stderr)
        refuse(result.returncode, 'task ' + selector + ' cannot be dispatched')
    identities[selector] = json.loads(result.stdout)

groups = {}
for selector in selectors:
    groups.setdefault(identities[selector]['identity'], []).append(selector)

gates, failure = [], 0
for ident, members in groups.items():
    result = verify(members[0])
    sys.stdout.write(result.stdout)
    sys.stderr.write(result.stderr)
    found = re.findall(r'^evidence: (.+)$', result.stdout, re.M)
    entry = {'identity': ident, 'selectors': members, 'exit_code': result.returncode,
             'evidence': found[-1] if found else None, 'evidence_sha256': None}
    if result.returncode == 0:
        checked = verify('--check', entry['evidence'])
        if checked.returncode:
            sys.stderr.write(checked.stderr)
            entry['exit_code'] = checked.returncode
        else:
            accepted = json.loads(checked.stdout)
            if accepted['identity'] != ident:
                entry['exit_code'] = 6
            entry['evidence_sha256'] = accepted['sha256']
    failure = failure or entry['exit_code']
    gates.append(entry)

first = identities[selectors[0]]
record = {'schema': 'paxeer-x.release-evidence.v1', 'scope': tokens,
          'selectors': {selector: identities[selector]['identity'] for selector in selectors},
          'source': {'revision': first['revision'], 'tree': first['tree']},
          'candidate_sha256': first['candidate_sha256'], 'gates': gates,
          'result': 'refused' if failure else 'pass',
          'created_at': datetime.datetime.now(datetime.timezone.utc).isoformat()}
stamp = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
target = Path(os.environ['PAXEER_X_EVIDENCE_DIR']).resolve() / (
    'release-' + first['revision'][:12] + '-' + stamp + '-' + os.urandom(4).hex() + '.json')
fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
with os.fdopen(fd, 'w', encoding='utf-8') as stream:
    json.dump(record, stream, indent=2, sort_keys=True)
    stream.write('\n')
print('release-evidence: ' + str(target) + ' sha256:' + hashlib.sha256(target.read_bytes()).hexdigest())
sys.exit(failure)
PY
