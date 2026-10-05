#!/usr/bin/env bash
set -euo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
exec python3 - <<'GATE'
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys

root = Path.cwd()
evidence = Path(os.environ['PAXEER_X_EVIDENCE_DIR'])
cargo = shlex.split(os.environ.get('PROGRAMS_CARGO', 'cargo'))
manifest = ['--locked', '--manifest-path', 'programs/Cargo.toml', '-p', 'layerx-programs-arbiter']
prefix = 'bisect::tests::'
required = {prefix + name for name in (
    'honest_defender_wins_against_every_lying_challenger_strategy',
    'honest_challenger_defeats_every_lying_defender_divergence',
    'dispute_over_host_call_isolates_the_storage_write_step',
    'dispute_over_trap_judges_the_terminal_trap_step',
    'absent_party_loses_by_default_at_the_declared_deadline',
)}

def execute(command, name, timeout):
    log = evidence / ('104.35.3-' + name + '.log')
    with log.open('w') as output:
        result = subprocess.run(command, cwd=root, stdin=subprocess.DEVNULL, stdout=output,
                                stderr=subprocess.STDOUT, timeout=timeout)
    print('exit=' + str(result.returncode) + ' log=' + str(log), flush=True)
    if result.returncode:
        sys.exit(result.returncode)
    return log.read_text()

built = execute(cargo + ['test', *manifest, '--lib', '--no-run', '--message-format=json'],
                'build-arbiter-tests', 1500)
executables = []
for line in built.splitlines():
    if line.startswith('{'):
        entry = json.loads(line)
        if (entry.get('reason') == 'compiler-artifact' and entry.get('executable')
                and entry.get('target', {}).get('name') == 'layerx_programs_arbiter'
                and entry.get('profile', {}).get('test')):
            executables.append(entry['executable'])
if len(executables) != 1:
    raise RuntimeError('exact arbiter unit test executable required')
arbiter = Path(executables[0])
if arbiter.is_symlink() or not arbiter.is_file() or not os.access(arbiter, os.X_OK):
    raise RuntimeError('invalid arbiter unit test executable')
inventory = execute([str(arbiter), prefix, '--list'], 'bisection-inventory', 240)
declared = set(re.findall(r'^(.+): test$', inventory, re.M))
if declared != required:
    raise RuntimeError('bisection corpus differs from the declared acceptance cases')
output = execute([str(arbiter), prefix, '--nocapture', '--test-threads=1'], 'bisection', 900)
counts = re.findall(r'test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;', output)
if len(counts) != 1 or int(counts[0][0]) != len(required) or counts[0][1:] != ('0', '0'):
    raise RuntimeError('bisection corpus was incomplete or ignored')
passed = set(re.findall(r'^test (\S+) \.\.\. ok$', output, re.M))
if passed != required:
    raise RuntimeError('required bisection cases did not pass: ' + ', '.join(sorted(required - passed)))
print('PAXEER_X_GATE tests=' + str(len(required)) + ' skipped=0', flush=True)
GATE
