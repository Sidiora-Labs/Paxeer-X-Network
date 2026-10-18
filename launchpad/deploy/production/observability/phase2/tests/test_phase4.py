import copy
import os
from pathlib import Path
import shutil
import sys
import tempfile
import unittest

PHASE=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(PHASE))
from contracts import canonical, Invalid, load, REQUIREMENTS, sha
from control import read_key
from phase4 import release, validate_plan, load_measurement, counter_deltas
from runtime import prometheus_samples


class NativePhase4Measurement(unittest.TestCase):
    def setUp(self):
        self.binding=load(os.environ['OBSERVABILITY_RUNTIME_BINDING'])
        self.config=load(PHASE.parent/'config-v2.json')
        self.plan=load(os.environ['OBSERVABILITY_PHASE4_PLAN'])
        self.measurement=Path(os.environ['OBSERVABILITY_PHASE4_MEASUREMENT'])
        self.measurement_hash=os.environ['OBSERVABILITY_PHASE4_MEASUREMENT_SHA256']
        self.shared=Path(os.environ['OBSERVABILITY_PHASE4_SHARED_CAPTURE'])
        self.shared_hash=os.environ['OBSERVABILITY_PHASE4_SHARED_CAPTURE_SHA256']
        self.scenario=Path(os.environ['OBSERVABILITY_PHASE3_SCENARIO_MANIFEST'])
        self.scenario_hash=os.environ['OBSERVABILITY_PHASE3_SCENARIO_MANIFEST_SHA256']
        self.key=read_key(os.environ['OBSERVABILITY_ATTESTATION_KEY_FILE'])
        self.assertLessEqual(self.plan['duration_seconds'],300,'task measurement uses bounded isolated component input; final shared soak belongs to serial9.1')

    def evaluate(self,output,*,plan=None,measurement=None,measurement_hash=None,shared=None,shared_hash=None,key=None):
        return release(self.plan if plan is None else plan,self.binding,self.config,self.measurement if measurement is None else measurement,self.measurement_hash if measurement_hash is None else measurement_hash,self.shared if shared is None else shared,self.shared_hash if shared_hash is None else shared_hash,self.scenario,self.scenario_hash,self.key if key is None else key,output)

    def copied_measurement(self,root):
        manifest=load(self.measurement)
        for name in manifest['artifacts']:shutil.copyfile(self.measurement.parent/name,root/name)
        destination=root/'measurement.json';destination.write_bytes(canonical(manifest))
        return destination,manifest

    def test_actual_native_component_capture_emits_twelve_release_measurements(self):
        with tempfile.TemporaryDirectory() as private:
            result=self.evaluate(Path(private)/'result')
            self.assertTrue(result['passed'],result['results'])
            self.assertEqual(result['scope'],'isolated_component')
            self.assertEqual([row['requirement'] for row in result['results']],list(REQUIREMENTS))
            self.assertEqual(len(result['actual_allocations']),len(result['resource_peaks']))
            self.assertFalse(result['capacity_qualified']);self.assertFalse(result['monthly_availability_qualified']);self.assertFalse(result['production_qualified'])
            self.assertEqual(result['final_shared_acceptance'],'deferred_to_9.1')
            self.assertTrue(all((Path(private)/'result'/(requirement+'.json')).is_file() for requirement in REQUIREMENTS))

    def test_missing_shared_capture_yields_twelve_explicit_failures(self):
        with tempfile.TemporaryDirectory() as private:
            result=self.evaluate(Path(private)/'result',shared=Path(private)/'missing.json')
            self.assertFalse(result['passed']);self.assertEqual(len(result['results']),12)
            self.assertTrue(all(row['passed'] is False for row in result['results']))

    def test_foreign_candidate_interface_generator_or_source_cannot_qualify(self):
        for key in ('candidate','interface','generator','source'):
            with self.subTest(key=key),tempfile.TemporaryDirectory() as private:
                plan=copy.deepcopy(self.plan)
                if key=='source':plan['source_sha256']='0'*64
                else:plan['d16_bindings'][key]='0'*64
                result=self.evaluate(Path(private)/'result',plan=plan)
                self.assertFalse(result['passed']);self.assertTrue(all(row['passed'] is False for row in result['results']))

    def test_actual_raw_artifact_tampering_is_rejected(self):
        with tempfile.TemporaryDirectory() as private:
            root=Path(private);destination,manifest=self.copied_measurement(root)
            name=manifest['cycles'][0]['metrics'][0]
            with (root/name).open('ab') as output:output.write(b'\n')
            with self.assertRaises(Invalid):load_measurement(destination,sha(destination.read_bytes()),self.plan,self.binding,self.config)

    def test_missing_cycle_traffic_probe_or_insufficient_budget_cannot_qualify(self):
        for failure in ('cycle','probe','budget'):
            with self.subTest(failure=failure),tempfile.TemporaryDirectory() as private:
                root=Path(private);destination,manifest=self.copied_measurement(root)
                if failure=='cycle':manifest['cycles'].pop()
                elif failure=='probe':manifest['cycles'][0]['probes']=None
                else:manifest['failures'].append({'cycle':0,'reason':'approved_capture_budget_exhausted'})
                destination.write_bytes(canonical(manifest))
                result=self.evaluate(root/'result',measurement=destination,measurement_hash=sha(destination.read_bytes()))
                self.assertFalse(result['passed']);self.assertTrue(all(row['passed'] is False for row in result['results']))

    def test_native_counter_reset_is_detected_per_series(self):
        manifest=load(self.measurement)
        before={};after={}
        for target,cycle in ((before,manifest['cycles'][0]),(after,manifest['cycles'][-1])):
            for name in cycle['metrics']:target.update(prometheus_samples((self.measurement.parent/name).read_bytes(),self.config))
        self.assertTrue(any(value>before[key] for key,value in after.items() if key in before and key[0]=='sli_read_eligible_requests_total'),'actual origin read traffic absent')
        with self.assertRaises(Invalid):counter_deltas((after,before))

    def test_allocation_or_operating_budget_failure_is_not_repaired_with_defaults(self):
        for failure in ('allocation','price'):
            with self.subTest(failure=failure),tempfile.TemporaryDirectory() as private:
                root=Path(private);plan=copy.deepcopy(self.plan);budget=load(plan['budget_path'])
                if failure=='allocation':budget['allocations']=[]
                else:budget['cost']['estimated_daily']=budget['cost']['approved_daily']+1
                path=root/'budget.json';path.write_bytes(canonical(budget));plan['budget_path']=str(path);plan['budget_sha256']=sha(path.read_bytes())
                with self.assertRaises(Invalid):validate_plan(plan,self.binding,self.config)
                result=self.evaluate(root/'result',plan=plan)
                self.assertFalse(result['passed']);self.assertTrue(all(row['passed'] is False for row in result['results']))


if __name__=='__main__':unittest.main(verbosity=2)
