import hashlib
from pathlib import Path
import sqlite3
import tempfile
import unittest

from measure_phase4 import percentile, pinned_file, read_receipts, sql_file, validate_ledger


class Phase4MeasurementTests(unittest.TestCase):
    def test_empty_negative_and_fractional_histograms_cannot_qualify(self):
        for histogram in ({}, {-1: 1}, {1: 0}, {1.5: 1}, {1: True}):
            with self.assertRaises(ValueError):
                percentile(histogram, 99)

    def test_weighted_tail_preserves_the_exact_frozen_boundary(self):
        self.assertEqual(percentile({500000000: 99, 500000001: 1}, 99), 500000000)
        self.assertEqual(percentile({500000000: 98, 500000001: 2}, 99), 500000001)

    def test_changed_actual_source_file_is_rejected_before_provider_execution(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'source'
            path.write_bytes(b'original source')
            pin = {'path': str(path), 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}
            self.assertEqual(pinned_file(pin), path)
            path.write_bytes(b'changed source')
            with self.assertRaises(ValueError):
                pinned_file(pin)

    def test_missing_authority_and_shortened_burst_are_failed_inputs(self):
        with self.assertRaises((KeyError, ValueError)):
            validate_ledger({})
        ledger = {'version': 1, 'candidate_sha256': 'a' * 64, 'approved_by': 'release-owner', 'approval_reference': 'private-authority', 'sustained': {'rate': 2000, 'seconds': 60}, 'burst': {'rate': 10000, 'seconds': 299}}
        with self.assertRaisesRegex(ValueError, 'five-minute'):
            validate_ledger(ledger)
        ledger['burst']['seconds'] = 300
        ledger['burst']['rate'] = 9999
        with self.assertRaisesRegex(ValueError, '10000'):
            validate_ledger(ledger)

    def test_effect_provider_sql_cannot_escape_the_read_only_transaction(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'effects.sql'
            for sql in ('COMMIT; DELETE FROM delivery_inbox', "SELECT '{{event_ids}}'; COMMIT", "SELECT '{{event_ids}}' -- trailing statement"):
                path.write_text(sql)
                pin = {'path': str(path), 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}
                with self.assertRaises(ValueError):
                    sql_file(pin, ['a' * 64])

    def test_empty_receipt_file_cannot_become_zero_latency_success(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'receipts.jsonl'
            path.write_text('')
            database = sqlite3.connect(':memory:')
            try:
                with self.assertRaisesRegex(ValueError, 'complete timed'):
                    read_receipts(path, database, 'burst', 10000, 300, 500000000)
            finally:
                database.close()


if __name__ == '__main__':
    unittest.main()
