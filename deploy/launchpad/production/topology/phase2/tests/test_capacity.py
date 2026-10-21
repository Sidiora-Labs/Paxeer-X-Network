from __future__ import annotations
import hashlib
import json
from pathlib import Path
import sys
import tempfile
import unittest

TOPOLOGY=Path(__file__).resolve().parents[2]
sys.path.insert(0,str(TOPOLOGY))
from phase2 import v1
from phase2.capacity import bytes_quantity, bound_ndjson, counter_rate, exact_series, failure_report, main, quantity, write_result


class NativeCapacityTests(unittest.TestCase):
    def test_exact_kubernetes_quantities_preserve_large_storage_bytes(self):
        self.assertEqual(bytes_quantity('2Ti'),2199023255552)
        self.assertEqual(bytes_quantity('1.5Ti'),1649267441664)
        self.assertEqual(bytes_quantity('4000G'),4000000000000)
        self.assertEqual(quantity('625m')*1000,625)
        self.assertEqual(quantity('0.125'),quantity('125m'))
        for malformed in ('-1Gi','Infinity','NaN','1GiB','0','1;cat','1e999999'):
            with self.subTest(value=malformed):
                with self.assertRaises(v1.Invalid):quantity(malformed)

    def test_raw_capture_hash_covers_complete_stream(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'native.ndjson';path.write_bytes(b'{"observed":1}\n{"observed":2}\n')
            reference={'path':str(path),'sha256':hashlib.sha256(path.read_bytes()).hexdigest()}
            self.assertEqual(list(bound_ndjson(reference)),[{'observed':1},{'observed':2}])
            path.write_bytes(path.read_bytes()+b'{"observed":3}\n')
            with self.assertRaisesRegex(v1.Invalid,'byte identity'):list(bound_ndjson(reference))

    def test_partial_rows_duplicate_keys_and_symlinks_fail(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'native.ndjson'
            for data in (b'{"a":1}',b'{"a":1,"a":2}\n',b'{"a":NaN}\n'):
                path.write_bytes(data)
                with self.assertRaises(v1.Invalid):
                    list(bound_ndjson({'path':str(path),'sha256':hashlib.sha256(data).hexdigest()}))
            link=Path(directory)/'linked.ndjson';link.symlink_to(path)
            with self.assertRaisesRegex(v1.Invalid,'ordinary'):
                list(bound_ndjson({'path':str(link),'sha256':'a'*64}))

    def test_real_counter_math_rejects_reset_or_identity_change(self):
        left={'metric':'completed_total','kind':'counter','series':[
              {'labels':{'replica_id':'api-one'},'timestampNs':'1000000000','value':'625'}]}
        right={'metric':'completed_total','kind':'counter','series':[
               {'labels':{'replica_id':'api-one'},'timestampNs':'2000000000','value':'1250'}]}
        rate=counter_rate(left,right,1000000000)
        self.assertEqual(list(rate.values()),[625])
        right['series'][0]['value']='624'
        with self.assertRaisesRegex(v1.Invalid,'counter reset'):counter_rate(left,right,1000000000)
        right['series'][0]['value']='1250';right['series'][0]['labels']['replica_id']='api-two'
        with self.assertRaisesRegex(v1.Invalid,'identity changed'):counter_rate(left,right,1000000000)

    def test_absent_or_nonfinite_native_series_cannot_mean_zero(self):
        for value in ('NaN','Infinity','-1'):
            with self.assertRaises(v1.Invalid):
                exact_series({'metric':'native','kind':'gauge','series':[
                             {'labels':{'zone':'zone-a'},'timestampNs':'1','value':value}]})
        with self.assertRaises(v1.Invalid):exact_series({'metric':'native','kind':'gauge','series':[]})

    def test_missing_real_inputs_emit_all_eighteen_failures(self):
        with tempfile.TemporaryDirectory() as directory:
            output=Path(directory)/'result.json'
            exit_code=main(['check','--inputs',str(Path(directory)/'absent.json'),
                            '--source-root',str(TOPOLOGY.parents[2]),'--output',str(output)])
            self.assertEqual(exit_code,1)
            value=json.loads(output.read_text())
            self.assertFalse(value['passed']);self.assertFalse(value['capacity_qualified'])
            self.assertEqual(set(value['requirements']),set(v1.REQS))
            self.assertTrue(all(result['passed'] is False for result in value['requirements'].values()))
            self.assertEqual(value['final_gate'],'9.1')

    def test_machine_results_are_durable_exclusive_and_never_promote(self):
        with tempfile.TemporaryDirectory() as directory:
            output=Path(directory)/'result.json';value=failure_report('missing actual restore')
            write_result(output,value)
            self.assertEqual(json.loads(output.read_text()),value)
            self.assertEqual(output.stat().st_mode&0o777,0o600)
            with self.assertRaises(FileExistsError):write_result(output,value)
            self.assertFalse(value['capacity_qualified'])

    def test_storage_ledgers_keep_peak_replay_distinct_from_normal(self):
        config=v1.load(TOPOLOGY/'fixtures/synthetic-config.json');ledgers=v1.capacity(config)
        self.assertEqual(ledgers['broker']['normal']['rawLogicalBytes'],36000000000)
        self.assertEqual(ledgers['broker']['normal']['rawR3Bytes'],108000000000)
        self.assertEqual(ledgers['broker']['qualification']['rawLogicalBytes'],622080000000)
        self.assertEqual(ledgers['broker']['qualification']['rawR3Bytes'],1866240000000)
        self.assertEqual(ledgers['broker']['qualification']['rawR3HeadroomBytes'],3732480000000)
        self.assertEqual(ledgers['postgres']['thirtyDayRawBytes'],555360000000)
        self.assertEqual(ledgers['postgres']['rowIndexAmplifiedBytesPerMember'],1666080000000)


if __name__=='__main__':
    unittest.main()
