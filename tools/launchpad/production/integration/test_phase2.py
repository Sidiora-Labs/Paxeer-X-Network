import os
from pathlib import Path
import tempfile
import unittest

from evidence_policy import validate_phase2
from journeys import Journey, JourneyError, load_configuration, persistence


class Phase2EvidenceBoundary(unittest.TestCase):
    def test_absent_actual_results_never_qualify(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError):
                validate_phase2(directory, {}, {}, 0)

    def test_local_arithmetic_cannot_claim_phase2(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError):
                validate_phase2(directory, {'task':'2.49', 'evidence_level':'synthetic_mathematical', 'phase2_qualified':True}, {}, 0)

    def test_unknown_recipient_basis_is_not_persisted_success(self):
        with self.assertRaises(persistence.PersistenceError):
            persistence.assert_persistence({'watchlist':[], 'chat':[]}, {'transactions':[], 'http_request_ids':[]})

    def test_duplicate_durable_change_is_not_ignored(self):
        fields = ('fills', 'stats', 'buckets', 'stats_outbox', 'charts', 'chart_outbox', 'positions', 'stats_inbox', 'candles_inbox', 'pnl_inbox')
        original = {key:[] for key in fields}
        changed = {**original, 'stats':[{'unexpected_durable_effect':1}]}
        with self.assertRaises(persistence.PersistenceError):
            persistence.assert_duplicate_invariant(original, changed)


class ActualPersistedJourneys(unittest.TestCase):
    def test_current_services_browser_receipts_and_faults(self):
        path = os.environ.get('PHASE2_CONFIG_FILE')
        digest = os.environ.get('PHASE2_CONFIG_SHA256')
        source = os.environ.get('QUALIFICATION_CANDIDATE_SHA256')
        output = os.environ.get('PHASE2_OUTPUT_DIR')
        self.assertTrue(path and digest and source and output, 'actual admitted runtime and current source binding required; no skips or provider substitution')
        configuration = load_configuration(Path(path), digest, source)
        result = Journey(configuration, path, Path(output)).run()
        self.assertTrue(result['phase2_qualified'])
        self.assertFalse(result['production_qualified'])
        self.assertFalse(result['capacity_qualified'])
        self.assertFalse(result['disaster_recovery_qualified'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
