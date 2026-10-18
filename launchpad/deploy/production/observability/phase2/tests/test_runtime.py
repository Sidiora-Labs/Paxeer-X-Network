import os
from pathlib import Path
import sys
import unittest

PHASE=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(PHASE))
from contracts import Invalid, load, validate_binding
from runtime import prometheus_samples, scrape


class Runtime(unittest.TestCase):
    def setUp(self):
        self.config=load(PHASE.parent/'config-v2.json')

    def test_unsafe_pool_wallet_and_raw_route_labels_rejected(self):
        for label in ('pool="0x1"','wallet="0x1"','route="/pool/0x1"','error_code="private-secret"'):
            with self.subTest(label=label),self.assertRaises(Invalid):
                prometheus_samples('sli_read_good_requests_total{service="apid_rest",'+label+'} 1',self.config)

    def test_duplicate_series_nonfinite_values_and_malformed_labels_rejected(self):
        for raw in ('metric 1\nmetric 2','metric NaN','metric +Inf','metric -1','sli_read_good_requests_total{service="apid_rest",service="apid_rest"} 1','sli_read_good_requests_total{service="apid_rest"garbage} 1'):
            with self.subTest(raw=raw),self.assertRaises(Invalid):
                prometheus_samples(raw,self.config)

    def test_actual_twelve_runtime_exports_have_bound_inventory(self):
        binding=load(os.environ['OBSERVABILITY_RUNTIME_BINDING'])
        validate_binding(binding,self.config)
        captures=scrape(binding,self.config)
        self.assertEqual([row['service'] for row in captures],self.config['services'])
        for capture in captures:
            metrics=prometheus_samples(capture['raw'],self.config)
            names={name for name,_ in metrics}
            self.assertTrue({'sli_read_eligible_requests_total','sli_read_good_requests_total','sli_mutation_good_requests_total','durable_transaction_commits_total'}<=names)

    def test_phase3_native_timeline_requires_bounded_sample_timestamp(self):
        raw='sli_read_good_requests_total{service="apid_rest",environment="qualification",zone="zone-a"} 1'
        for timestamp in ('',' 999',' 2001'):
            with self.subTest(timestamp=timestamp),self.assertRaises(Invalid):
                prometheus_samples(raw+timestamp,self.config,interval=(1,2))
        observed=prometheus_samples(raw+' 1500',self.config,interval=(1,2))
        self.assertEqual(sum(observed.values()),1)

    def test_fractional_counters_and_histogram_labels_on_gauges_are_rejected(self):
        for raw in ('sli_read_good_requests_total 1.5','sli_read_good_requests_total 9007199254740994','durable_storage_free_bytes{le="1"} 1'):
            with self.subTest(raw=raw),self.assertRaises(Invalid):
                prometheus_samples(raw,self.config)


if __name__=='__main__':unittest.main(verbosity=2)
