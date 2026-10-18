from __future__ import annotations

import hashlib
import argparse
import json
import os
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent))
from providers import regular
from render import RecoveryIdentity, load_operator
import recovery

ObservationArchive = load_operator().ObservationArchive


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--phase', type=int, choices=(2, 3), default=2)
    args = parser.parse_args()
    import test_operator
    if args.phase == 2:
        suite = unittest.TestSuite([unittest.defaultTestLoader.loadTestsFromTestCase(test_operator.OperatorContracts),
                                   unittest.defaultTestLoader.loadTestsFromTestCase(test_operator.ActualPostgreSQLOperator)])
    else:
        import test_recovery
        import test_postgres
        suite = unittest.TestSuite([
            unittest.defaultTestLoader.loadTestsFromTestCase(test_operator.AdversarialContracts),
            unittest.defaultTestLoader.loadTestsFromTestCase(test_operator.ActualBackupAuthorization),
            unittest.defaultTestLoader.loadTestsFromTestCase(test_recovery.AdversarialResultContracts),
            unittest.TestSuite(test_postgres.PostgreSQLRecovery(name) for name in
                               unittest.defaultTestLoader.getTestCaseNames(test_postgres.PostgreSQLRecovery)
                               if name.startswith('test_v3_'))])
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    expected_tests = 10 if args.phase == 2 else 13
    success = result.wasSuccessful() and result.testsRun == expected_tests and not result.skipped
    if not success:
        return 1
    filename = os.environ.get('RECOVERY_OPERATOR_QUALIFICATION_CONFIG')
    expected_sha = os.environ.get('RECOVERY_OPERATOR_QUALIFICATION_CONFIG_SHA256')
    observation = os.environ.get('RECOVERY_OPERATOR_SCENARIO_OBSERVATIONS')
    if not filename or not expected_sha or not observation:
        raise ValueError('actual measured recovery scenario inputs unavailable')
    raw = regular(filename, recovery.MAX_JSON, private=True).read_bytes()
    if hashlib.sha256(raw).hexdigest() != expected_sha:
        raise ValueError('qualification input identity changed')
    config = recovery.load(raw)
    if config['realm'] != 'isolated-recovery-qualification':
        raise ValueError('isolated rehearsal authority required')
    identity = RecoveryIdentity(**config['identity']).validate()
    archive_type = ObservationArchive if args.phase == 2 else load_operator().AdversarialArchive
    archive_options = {'native_tools': config['tools']} if args.phase == 3 else {}
    archive = archive_type(config['observation_directory'], config['observation_signing_key'], identity, **archive_options)
    payload = archive.read(observation)
    if args.phase == 3:
        scenarios_file = os.environ.get('RECOVERY_OPERATOR_SCENARIO_REHEARSALS')
        if not scenarios_file:
            raise ValueError('native adversarial scenario receipts unavailable')
        archive.read_scenarios(scenarios_file, payload)
    print(json.dumps({'version': args.phase, 'tests': result.testsRun, 'requirement_count': len(payload['evidence']),
                      'source_sha256': identity.source_sha256, 'interface_sha256': identity.interface_sha256,
                      'observation_sha256': recovery.sha(payload), 'outcome': 'pass'}, sort_keys=True))
    return 0


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (ValueError, OSError) as exc:
        print(json.dumps({'outcome': 'fail', 'error_type': type(exc).__name__}), file=sys.stderr)
        raise SystemExit(1)
