from __future__ import annotations

import unittest
from capacity import allocation, histogram_delta, percentile_buckets
from workload import MIX, ledger, validate_mix


class FrozenCacheMeasurementMath(unittest.TestCase):
    def test_frozen_quotes_and_writes_are_subsets(self):
        frozen = ledger()
        self.assertEqual(sum(validate_mix(MIX).values()),10000)
        self.assertEqual(frozen['requests_24h'],864000000)
        self.assertEqual(frozen['writes_24h'],172800000)
        self.assertEqual(sum(frozen['surge_mix'].values()),20000)
        self.assertEqual(frozen['surge_mix']['quotes'],4000)
        self.assertEqual(frozen['surge_mix']['writes'],4000)
        with self.assertRaises(ValueError): validate_mix({**MIX,'quotes':4000})

    def test_missing_authority_and_wrong_duration_rejected(self):
        with self.assertRaises(KeyError): allocation({})
        with self.assertRaises(ValueError): allocation({'profile':'soak','duration_seconds':60})
        with self.assertRaises(ValueError): allocation({'profile':'surge','duration_seconds':1})
        with self.assertRaises(ValueError): allocation({'profile':'example','duration_seconds':86400})

    def test_histogram_reset_and_missing_tail_are_unavailable(self):
        self.assertIsNone(percentile_buckets([0]*32,.99))
        with self.assertRaises(ValueError): histogram_delta([0]*32,[1]*32)
        with self.assertRaises(ValueError): histogram_delta([0]*31,[0]*32)
        self.assertEqual(histogram_delta([0]*32,[0]*32),[0]*32)


if __name__=='__main__': unittest.main()
