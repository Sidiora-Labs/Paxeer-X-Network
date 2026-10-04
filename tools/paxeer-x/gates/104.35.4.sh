#!/usr/bin/env bash
set -euo pipefail
exec python3 - <<'GATE'
import os
from pathlib import Path
import re
import subprocess
import sys

root = Path.cwd()
evidence = Path(os.environ['PAXEER_X_EVIDENCE_DIR'])
tests = 0

def execute(binary, arguments, name):
    log = evidence / ('104.35.4-' + name + '.log')
    with log.open('w') as output:
        result = subprocess.run([str(binary), *arguments], cwd=root, stdout=output,
                                stderr=subprocess.STDOUT, timeout=240)
    print('exit=' + str(result.returncode) + ' log=' + str(log), flush=True)
    if result.returncode:
        sys.exit(result.returncode)
    return log.read_text()

def binary(variable):
    raw = os.environ.get(variable)
    if not raw:
        print('missing prerequisite: genuine prebuilt ' + variable, file=sys.stderr)
        sys.exit(78)
    path = Path(raw)
    if not path.is_absolute() or path.is_symlink() or not path.is_file() or not os.access(path, os.X_OK):
        raise RuntimeError('invalid actual test executable: ' + variable)
    return path

market = binary('LAYERX_MARKET_TEST_BINARY')
inventory = execute(market, ['--list'], 'market-inventory')
declared = set(re.findall(r'^(.+): test$', inventory, re.M))
required = {
    'tests::canonical_market_state_is_public_and_strictly_decoded',
    'tests::delivered_usage_settlement_conserves_escrow_and_is_readable',
    'tests::absent_provider_expiry_records_exact_refund_once',
    'tests::renter_funding_and_provider_capacity_are_exact_obligations',
    'tests::settlement_decoder_refuses_invalid_totals_status_and_noncanonical_bytes',
    'tests::marketplace_selector_refuses_legacy_unchecked_settlement_and_unknown_operations',
    'tests::funded_lease_holds_capacity_until_expiry_without_direct_settlement',
    'tests::provider_absence_refunds_expired_lease',
    'tests::unfunded_and_mid_work_expiry_are_refused',
}
if not required <= declared:
    raise RuntimeError('market executable omits required actual lifecycle cases')
output = execute(market, ['--nocapture', '--test-threads=1'], 'market-state')
counts = re.findall(r'test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;', output)
if len(counts) != 1 or int(counts[0][0]) != len(declared) or counts[0][1:] != ('0', '0'):
    raise RuntimeError('market corpus was incomplete or ignored')
tests += len(declared)
runtime = binary('LAYERX_MARKET_RUNTIME_TEST_BINARY')
inventory = execute(runtime, ['--list'], 'runtime-inventory')
declared = set(re.findall(r'^(.+): test$', inventory, re.M))
for case in [
    'execute::market_tests::compiled_market_funding_expiry_public_settlement_and_close',
    'execute::market_tests::compiled_market_refuses_unfunded_and_unauthorized_payments_atomically',
    'transfer::tests::owner_frame_and_cumulative_program_spend_boundaries_are_closed',
    'transfer::tests::program_authority_refuses_wrong_seed_program_and_source_typed',
]:
    if case not in declared:
        raise RuntimeError('runtime executable omits required actual case: ' + case)
    output = execute(runtime, [case, '--exact', '--nocapture', '--test-threads=1'], case.replace('::', '-'))
    if len(re.findall(r'test result: ok\. 1 passed; 0 failed; 0 ignored;', output)) != 1:
        raise RuntimeError('runtime case did not execute exactly once: ' + case)
    tests += 1
print('PAXEER_X_GATE tests=' + str(tests) + ' skipped=0', flush=True)
GATE
