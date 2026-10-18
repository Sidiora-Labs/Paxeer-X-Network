#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time

root = Path(__file__).resolve().parents[2]
owned = root / 'security/control'
parser = argparse.ArgumentParser()
parser.add_argument('--phase', choices=('2', '3'), default='2')
phase = parser.parse_args().phase
output = root / ('evidence/production-system/L47/phase' + phase)
output.mkdir(parents=True, exist_ok=True)
expected = {
    'TestPhase2AuthenticatedReplayTenRetriesOneDurableJob',
    'TestPhase2OwnerRecommendationRealAuthorityConcurrentBudget',
    'TestPhase2UnfundedBountyHTTPRetainsFundingRequired',
    'TestPhase2ModeratorTreasuryDeniedWithImmutableAudit',
    'TestPhase2CredentialContainmentRevokesActualDatabaseIdentity',
    'TestPhase2D11SessionRevocationInvalidatesApprovalIdentity',
    'TestPhase2CapsFreshDisableAndUnsupportedSettlementRemainVisible',
}
if phase == '3':
    expected |= {
        'TestPhase3QuarantinedApprovalCannotDispatchReplayBeforeRevocation',
        'TestPhase3QuarantinePreventsCapExpansionAndBulkAccess',
        'TestPhase3ConcurrentReplayRetriesShareOneDurableJob',
        'TestPhase3JobWriteInterruptionRollsBackThenRetriesOnce',
        'TestPhase3CrossAgentBudgetsRemainDurableAndHistoryIsDisclosed',
        'TestPhase3CredentialRotationCannotReuseRevokedApproval',
        'TestPhase3RecoveryWithoutProviderProofRetainsContainment',
        'TestPhase3PayoutConnectionLossCannotConfirmSettlement',
    }
with tempfile.TemporaryDirectory(prefix='kindle-l47-phase' + phase + '-') as directory:
    work = Path(directory)
    for file in owned.glob('*.go'):
        shutil.copy2(file, work / file.name)
    module = (root / 'shared/go.mod').read_text().replace('=> ../protocol', '=> ' + str(root / 'protocol'))
    (work / 'go.mod').write_text(module)
    shutil.copy2(root / 'shared/go.sum', work / 'go.sum')
    environment = dict(os.environ, GOWORK='off', GOMAXPROCS='2', GOFLAGS='-mod=readonly', L47_SOURCE_ROOT=str(root))
    command = ['go', 'test', '-race', '-p', '1', '-count=1', '-json', '-run', '^TestPhase2' if phase == '2' else '^TestPhase[23]', '.']
    started = time.time()
    result = subprocess.run(command, cwd=work, env=environment, text=True, capture_output=True, timeout=1200)
    (output / 'native.stdout').write_text(result.stdout)
    (output / 'native.stderr').write_text(result.stderr)
    rows = [json.loads(line) for line in result.stdout.splitlines()]
    ran = {row.get('Test') for row in rows if row.get('Action') == 'run'}
    passed = {row.get('Test') for row in rows if row.get('Action') == 'pass'}
    valid = result.returncode == 0 and ran == expected and passed == expected and all(row.get('Action') not in ('fail', 'skip') for row in rows)
    report = {
        'task': phase + '.47', 'native_command': command, 'native_exit_code': result.returncode,
        'started_at_unix': started, 'finished_at_unix': time.time(),
        'passed_cases': sorted(passed), 'required_cases': sorted(expected), 'native_cases_passed': valid,
        'source_sha256': {str(file.relative_to(root)): hashlib.sha256(file.read_bytes()).hexdigest() for file in sorted(owned.glob('*.go'))},
        'release_blockers': ['real payout provider timeout/reconciliation qualification', 'actual frontend pre-wallet cap registration and browser journey', 'real secret/cache revocation provider drill', 'prior serial integration task1.99 acceptance'],
    }
    if phase == '3':
        report['scenario_coverage'] = {
            '1': {'status': 'passed' if 'TestPhase3ConcurrentReplayRetriesShareOneDurableJob' in passed else 'failed', 'test': 'TestPhase3ConcurrentReplayRetriesShareOneDurableJob'},
            '2': {'status': 'blocked', 'reason': 'frontend pre-wallet registration and real browser journey are outside the owned source and require serial integration'},
            '3': {'status': 'passed' if 'TestPhase3CrossAgentBudgetsRemainDurableAndHistoryIsDisclosed' in passed else 'failed', 'test': 'TestPhase3CrossAgentBudgetsRemainDurableAndHistoryIsDisclosed'},
            '4': {'status': 'passed' if 'TestPhase3CrossAgentBudgetsRemainDurableAndHistoryIsDisclosed' in passed else 'failed', 'test': 'TestPhase3CrossAgentBudgetsRemainDurableAndHistoryIsDisclosed'},
            '5': {'status': 'passed' if 'TestPhase2UnfundedBountyHTTPRetainsFundingRequired' in passed else 'failed', 'test': 'TestPhase2UnfundedBountyHTTPRetainsFundingRequired'},
            '6': {'status': 'blocked', 'reason': 'accepted manifest and payout authority are absent; historical synthetic authority fixtures cannot supply this proof'},
            '7': {'status': 'blocked', 'reason': 'genuine payout provider acceptance followed by timeout and confirmed chain reconciliation is unavailable'},
            '8': {'status': 'passed' if 'TestPhase2CredentialContainmentRevokesActualDatabaseIdentity' in passed else 'failed', 'test': 'TestPhase2CredentialContainmentRevokesActualDatabaseIdentity', 'remaining': 'real external secret revocation, cache invalidation and approved recovery'},
            '9': {'status': 'passed' if 'TestPhase2ModeratorTreasuryDeniedWithImmutableAudit' in passed else 'failed', 'test': 'TestPhase2ModeratorTreasuryDeniedWithImmutableAudit'},
        }
        report['task_qualified'] = False
        report['production_qualified'] = False
        report['release_blockers'] = [
            'prior serial integration task2.99 acceptance',
            'actual pre-wallet cap registration and browser journey',
            'accepted manifest authority and real funded reserve competition',
            'real payout timeout and confirmed receipt reconciliation',
            'real secret/cache revocation and approved integrity recovery',
        ]
        report['source_sha256'] = {str(file.relative_to(root)): hashlib.sha256(file.read_bytes()).hexdigest() for file in sorted(owned.iterdir()) if file.suffix in ('.go', '.ts', '.py')}
    (output / 'native-result.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report))
    raise SystemExit(0 if valid and phase == '2' else result.returncode or 1)
