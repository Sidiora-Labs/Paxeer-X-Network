import os
from pathlib import Path
import shutil
import sys
import tempfile
import unittest
import hashlib
import hmac

PHASE=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(PHASE))
from contracts import canonical,Invalid,load,REQUIREMENTS
from control import read_key
from evidence import Evidence,evaluate,evaluate_phase3
from qualify import qualify


class NativeEvidence(unittest.TestCase):
    def setUp(self):
        self.binding=load(os.environ['OBSERVABILITY_RUNTIME_BINDING'])
        self.config=load(PHASE.parent/'config-v2.json')
        self.key=read_key(os.environ['OBSERVABILITY_ATTESTATION_KEY_FILE'])
        self.manifest=Path(os.environ['OBSERVABILITY_SCENARIO_MANIFEST'])

    def test_real_attested_scenarios_reconcile_all_twelve_requirements(self):
        evidence=Evidence(self.manifest,self.binding,self.config,self.key)
        result=evaluate(evidence)
        self.assertEqual([row['requirement'] for row in result['results']],list(REQUIREMENTS))
        self.assertTrue(result['passed'],result['results'])
        self.assertTrue(all(len(row['criteria'])==5 for row in result['results']))

    def test_missing_evidence_yields_twelve_explicit_failures(self):
        with tempfile.TemporaryDirectory() as private:
            result=qualify(self.binding,self.config,Path(private)/'missing.json',self.key,Path(private)/'output')
            self.assertFalse(result['passed'])
            self.assertEqual(len(result['results']),12)
            self.assertTrue(all(row['passed'] is False for row in result['results']))

    def test_actual_manifest_tampering_cannot_authorize_activation(self):
        document=load(self.manifest)
        with tempfile.TemporaryDirectory() as private:
            destination=Path(private)/self.manifest.name
            document['deployment_digest']='0'*64
            destination.write_bytes(canonical(document))
            with self.assertRaises(Invalid):Evidence(destination,self.binding,self.config,self.key)

    def test_native_artifact_tampering_is_rejected(self):
        document=load(self.manifest)
        with tempfile.TemporaryDirectory() as private:
            root=Path(private)
            for name in document['artifacts']:
                destination=root/name;destination.parent.mkdir(parents=True,exist_ok=True)
                shutil.copyfile(self.manifest.parent/name,destination)
            copied=root/self.manifest.name;shutil.copyfile(self.manifest,copied)
            name=next(iter(document['artifacts']))
            with (root/name).open('ab') as stream:stream.write(b'changed')
            with self.assertRaises(Invalid):Evidence(copied,self.binding,self.config,self.key)


class NativeAdversarialEvidence(NativeEvidence):
    def setUp(self):
        super().setUp()
        self.manifest=Path(os.environ['OBSERVABILITY_PHASE3_SCENARIO_MANIFEST'])

    def copy_native(self, root):
        document=load(self.manifest)
        for name in document['artifacts']:
            destination=root/name;destination.parent.mkdir(parents=True,exist_ok=True)
            shutil.copyfile(self.manifest.parent/name,destination)
        destination=root/self.manifest.name
        self.sign_native(destination,document)
        return destination,document

    def sign_native(self, destination, document):
        unsigned={key:value for key,value in document.items() if key!='signature'}
        document['signature']=hmac.new(self.key,canonical(unsigned),hashlib.sha256).hexdigest()
        destination.write_bytes(canonical(document))

    def replace_native(self, root, document, name, value):
        raw=canonical(value);(root/name).write_bytes(raw)
        document['artifacts'][name]['sha256']=hashlib.sha256(raw).hexdigest()

    def require_native_baseline(self):
        result=evaluate_phase3(Evidence(self.manifest,self.binding,self.config,self.key))
        self.assertTrue(result['passed'],result['results'])
        return result

    def test_native_recovery_all_twelve_requirements_and_control_adversaries(self):
        result=self.require_native_baseline()
        self.assertEqual(result['phase'],3)
        self.assertTrue(all(row['recovery']['state_sha256'] for row in result['results']))
        self.assertEqual(result['adversaries']['denied_actions'],['activate','propose','reject','validate'])
        self.assertEqual(result['adversaries']['activation_records'],1)

    def test_each_native_scenario_missing_timeline_fails_its_requirement(self):
        self.require_native_baseline()
        for requirement in REQUIREMENTS:
            with self.subTest(requirement=requirement),tempfile.TemporaryDirectory() as private:
                root=Path(private);destination,document=self.copy_native(root)
                name=document['scenario_artifacts'][requirement]
                scenario=load(root/name);scenario['metric_timeline']='missing-native-timeline.prom'
                self.replace_native(root,document,name,scenario);self.sign_native(destination,document)
                result=evaluate_phase3(Evidence(destination,self.binding,self.config,self.key))
                row=next(row for row in result['results'] if row['requirement']==requirement)
                self.assertFalse(row['passed']);self.assertFalse(result['passed'])

    def test_failed_restore_and_duplicate_database_effects_cannot_qualify(self):
        self.require_native_baseline()
        for failure in ('failed_restore','duplicate_effect'):
            with self.subTest(failure=failure),tempfile.TemporaryDirectory() as private:
                root=Path(private);destination,document=self.copy_native(root)
                if failure=='failed_restore':document['restore']['completed']=False
                else:document['database_effects'].append(dict(document['database_effects'][0]))
                self.sign_native(destination,document)
                with self.assertRaises(Invalid):Evidence(destination,self.binding,self.config,self.key)

    def test_recovery_snapshot_duplicates_and_divergence_are_detected(self):
        self.require_native_baseline()
        for failure in ('duplicate','divergent'):
            with self.subTest(failure=failure),tempfile.TemporaryDirectory() as private:
                root=Path(private);destination,document=self.copy_native(root)
                recovery=load(root/document['phase3']['recoveries']['D14_020'])
                name=recovery['after'];snapshot=load(root/name)
                if failure=='duplicate':snapshot['rows'].append(dict(snapshot['rows'][0]))
                else:snapshot['rows'][0]['value']={'altered_native_row':snapshot['rows'][0]['value']}
                self.replace_native(root,document,name,snapshot);self.sign_native(destination,document)
                result=evaluate_phase3(Evidence(destination,self.binding,self.config,self.key))
                self.assertFalse(result['passed'])
                self.assertFalse(next(row for row in result['results'] if row['requirement']=='D14_020')['passed'])

    def test_zone_loss_stale_acceptance_and_duplicate_activation_are_detected(self):
        self.require_native_baseline()
        for key,field in (('zone_loss','stale_acceptances'),('stale_actor','accepted_effects'),('interrupted_activation','activation_records')):
            with self.subTest(adversary=key),tempfile.TemporaryDirectory() as private:
                root=Path(private);destination,document=self.copy_native(root)
                name=document['phase3'][key];capture=load(root/name)
                capture[field]=2
                self.replace_native(root,document,name,capture);self.sign_native(destination,document)
                result=evaluate_phase3(Evidence(destination,self.binding,self.config,self.key))
                self.assertFalse(result['passed']);self.assertTrue(all(row['passed'] is False for row in result['results']))

    def test_absent_measured_capacity_is_explicit_failure(self):
        self.require_native_baseline()
        with tempfile.TemporaryDirectory() as private:
            root=Path(private);destination,document=self.copy_native(root)
            scenario=load(root/document['scenario_artifacts']['D14_029'])
            name=scenario['resources'];resource=load(root/name)
            resource['measured']['million_reads']=0
            self.replace_native(root,document,name,resource);self.sign_native(destination,document)
            result=evaluate_phase3(Evidence(destination,self.binding,self.config,self.key))
            self.assertFalse(result['passed']);self.assertFalse(next(row for row in result['results'] if row['requirement']=='D14_029')['passed'])

    def test_phase2_evidence_cannot_claim_phase3_adversarial_success(self):
        evidence=Evidence(Path(os.environ['OBSERVABILITY_SCENARIO_MANIFEST']),self.binding,self.config,self.key)
        self.assertEqual(evidence.document['version'],'2.0')
        result=evaluate_phase3(evidence)
        self.assertFalse(result['passed']);self.assertTrue(all(row['passed'] is False for row in result['results']))


if __name__=='__main__':unittest.main(verbosity=2)
