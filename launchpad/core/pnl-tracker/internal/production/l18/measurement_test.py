import hashlib
import importlib.util
import json
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest

location = pathlib.Path(__file__).resolve()
spec = importlib.util.spec_from_file_location('l18_measurement', location.with_name('measurement.py'))
measurement = importlib.util.module_from_spec(spec)
spec.loader.exec_module(measurement)


class Measurement(unittest.TestCase):
    def test_exact_monetary_boundary(self):
        self.assertEqual(measurement.amount('1000000'), 1000000)
        self.assertEqual(measurement.amount(str((1 << 256)-1)), (1 << 256)-1)
        for invalid in ('01', '-1', '1.0', '1e6', str(1 << 256), 1000000):
            with self.assertRaises(measurement.InputFailure):
                measurement.amount(invalid)

    def test_actual_source_identity(self):
        expected = hashlib.sha256(location.read_bytes()).hexdigest()
        self.assertEqual(measurement.sha(location), expected)

    def test_actual_process_missing_provider_fails_without_metrics(self):
        with tempfile.TemporaryDirectory(prefix='l18-measurement-input-') as temporary:
            output = pathlib.Path(temporary)/'run'
            result = subprocess.run([sys.executable,str(location.with_name('measurement.py')),'--config',str(pathlib.Path(temporary)/'absent.json'),'--output-dir',str(output)],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=10)
            self.assertEqual(result.returncode, 2)
            report = json.loads((output/'measurement.json').read_text())
            self.assertFalse(report['lane_measurement_passed'])
            self.assertFalse(report['capacity_qualified'])
            self.assertNotIn('reads',report)
            self.assertNotIn('reward_awards_created',report)
            self.assertEqual(report['failure_class'],'InputFailure')

    def test_private_input_enforcement_and_no_historical_output_reuse(self):
        with tempfile.TemporaryDirectory(prefix='l18-measurement-private-') as temporary:
            path = pathlib.Path(temporary)/'input.json'
            path.write_text('{}')
            path.chmod(0o644)
            with self.assertRaises(measurement.InputFailure):
                measurement.private_file(path)
            path.chmod(0o600)
            self.assertEqual(measurement.private_file(path), path)
            output = pathlib.Path(temporary)/'existing'
            output.mkdir()
            marker = output/'measurement.json'
            marker.write_text('previous actual failure\n')
            result = subprocess.run([sys.executable,str(location.with_name('measurement.py')),'--config',str(path),'--output-dir',str(output)],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=10)
            self.assertNotEqual(result.returncode,0)
            self.assertEqual(marker.read_text(),'previous actual failure\n')

    def test_real_provider_measurement(self):
        config_path = os.environ.get('L18_MEASUREMENT_CONFIG')
        self.assertTrue(config_path,'actual private runtime/DB/RPC/auth configuration required')
        config, runtime, digest = measurement.load_config(pathlib.Path(config_path),'observe')
        self.assertEqual(config['release_digest'],os.environ['L18_RELEASE_DIGEST'])
        self.assertEqual(config['interface_digest'],os.environ['L18_INTERFACE_DIGEST'])
        self.assertLessEqual(config['duration_seconds'],120,'focused lane observation bounded to120s; shared24h run belongs serial9.1')
        output = os.environ.get('L18_MEASUREMENT_OUTPUT')
        self.assertTrue(output,'exclusive private actual measurement output required')
        result = subprocess.run([sys.executable,str(location.with_name('measurement.py')),'--config',str(config_path),'--output-dir',output,'--mode','observe'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=180)
        self.assertEqual(result.returncode,0,result.stderr.decode())
        report = json.loads((pathlib.Path(output)/'measurement.json').read_text())
        self.assertTrue(report['lane_measurement_passed'])
        self.assertFalse(report['capacity_qualified'])
        self.assertEqual(report['source_digest'],config['source_digest'])
        self.assertEqual(report['reads']['failures'],0)
        self.assertEqual(report['reads']['completed'],sum(read['rps'] for read in config['reads'])*config['duration_seconds'])
        for name, expected in report['artifact_manifest'].items():
            self.assertEqual(measurement.sha(pathlib.Path(output)/name),expected)


if __name__ == '__main__':
    unittest.main(verbosity=2)
