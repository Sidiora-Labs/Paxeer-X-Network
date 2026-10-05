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
manifest = ['--locked', '--manifest-path', 'programs/Cargo.toml', '-p', 'layerx-programs-market']

def execute(command, name, timeout):
    log = evidence / ('104.35.5-' + name + '.log')
    with log.open('w') as output:
        result = subprocess.run(command, cwd=root, stdin=subprocess.DEVNULL, stdout=output,
                                stderr=subprocess.STDOUT, timeout=timeout)
    print('exit=' + str(result.returncode) + ' log=' + str(log), flush=True)
    if result.returncode:
        sys.exit(result.returncode)
    return log.read_text()

execute(cargo + ['build', *manifest, '--target', 'wasm32-unknown-unknown', '--release'],
        'build-market-guest', 1500)
metadata = json.loads(subprocess.run(cargo + ['metadata', '--format-version', '1', '--no-deps',
                                              '--locked', '--manifest-path', 'programs/Cargo.toml'],
                                     cwd=root, stdin=subprocess.DEVNULL, capture_output=True,
                                     text=True, check=True, timeout=300).stdout)
guest = Path(metadata['target_directory']) / 'wasm32-unknown-unknown/release/layerx_programs_market.wasm'
if guest.is_symlink() or not guest.is_file() or guest.read_bytes()[:4] != b'\0asm':
    raise RuntimeError('production Market guest was not produced')
built = execute(cargo + ['test', *manifest, '--lib', '--no-run', '--message-format=json'],
                'build-market-tests', 1500)
executables = []
for line in built.splitlines():
    if line.startswith('{'):
        entry = json.loads(line)
        if (entry.get('reason') == 'compiler-artifact' and entry.get('executable')
                and entry.get('target', {}).get('name') == 'layerx_programs_market'
                and entry.get('profile', {}).get('test')):
            executables.append(entry['executable'])
if len(executables) != 1:
    raise RuntimeError('exact Market unit test executable required')
market = Path(executables[0])
if market.is_symlink() or not market.is_file() or not os.access(market, os.X_OK):
    raise RuntimeError('invalid Market unit test executable')
inventory = execute([str(market), '--list'], 'market-inventory', 240)
declared = set(re.findall(r'^(.+): test$', inventory, re.M))
required = {
    'settle::tests::last_height_challenge_freezes_and_late_challenge_is_refused',
    'settle::tests::finalization_is_after_window_and_conserves_escrow',
    'settle::tests::both_arbiter_outcomes_conserve_escrow_and_challenge_stake',
    'settle::tests::unchallenged_claim_is_final_and_its_settlement_irreversible',
    'settle::tests::dispute_requires_stake_and_contradiction_and_freezes_settlement',
    'settle::tests::settlement_conserves_value_on_honest_challenged_and_expiry_paths',
    'tests::delivered_usage_settlement_conserves_escrow_and_is_readable',
    'tests::absent_provider_expiry_records_exact_refund_once',
    'tests::settlement_decoder_refuses_invalid_totals_status_and_noncanonical_bytes',
    'tests::marketplace_selector_refuses_legacy_unchecked_settlement_and_unknown_operations',
    'tests::provider_absence_refunds_expired_lease',
    'tests::unfunded_and_mid_work_expiry_are_refused',
}
missing = sorted(required - declared)
if missing:
    raise RuntimeError('Market executable omits required settlement cases: ' + ', '.join(missing))
output = execute([str(market), '--nocapture', '--test-threads=1'], 'market-settlement', 600)
counts = re.findall(r'test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;', output)
if len(counts) != 1 or int(counts[0][0]) != len(declared) or counts[0][1:] != ('0', '0'):
    raise RuntimeError('Market corpus was incomplete or ignored')
passed = set(re.findall(r'^test (\S+) \.\.\. ok$', output, re.M))
if not required <= passed:
    raise RuntimeError('required settlement cases did not pass: ' + ', '.join(sorted(required - passed)))
print('PAXEER_X_GATE tests=' + str(len(declared)) + ' skipped=0', flush=True)
GATE
