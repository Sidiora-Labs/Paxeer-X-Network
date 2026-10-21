import os
from pathlib import Path
import sys
import tempfile
import unittest
import json
import sqlite3
import subprocess
from concurrent.futures import ThreadPoolExecutor

PHASE=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(PHASE))
from contracts import Invalid,load,canonical,sha
from control import Control
from render import render,immutable_bundle,verify_bundle


class OperatorControl(unittest.TestCase):
    def setUp(self):
        self.binding=load(os.environ['OBSERVABILITY_RUNTIME_BINDING'])
        self.config=load(PHASE.parent/'config-v2.json')
        self.actor=next(iter(self.binding['responders'].values()))

    def test_real_sqlite_compare_and_swap_rejects_stale_and_unqualified_activation(self):
        with tempfile.TemporaryDirectory() as private:
            control=Control(Path(private)/'control.sqlite')
            proposed=control.change('propose',self.actor,self.binding,self.config,0)
            self.assertEqual(proposed['state'],'proposed')
            with self.assertRaises(Invalid):control.change('reject',self.actor,self.binding,self.config,0)
            with self.assertRaises(Invalid):control.change('activate',self.actor,self.binding,self.config,1)
            self.assertEqual(control.current()['state'],'proposed')
            rejected=control.change('reject',self.actor,self.binding,self.config,1)
            self.assertEqual(rejected['state'],'rejected')
            self.assertEqual(rejected['previous_sha256'],proposed['record_sha256'])
            with self.assertRaises(Invalid):control.change('propose',self.actor,self.binding,self.config,2)

    def test_render_exports_identity_and_secret_references_only(self):
        with tempfile.TemporaryDirectory() as private:
            manifest=render(self.binding,self.config,private)
            diagnostics=load(Path(private)/'diagnostics.json')
            self.assertEqual(diagnostics['chain_id'],125)
            self.assertEqual(diagnostics['contract_addresses'],self.binding['contract_addresses'])
            self.assertEqual(len(diagnostics['services']),12)
            self.assertEqual(manifest['secret_references'],self.config['exporter'])
            self.assertFalse(manifest['activation_allowed'])
            self.assertNotIn('responders',diagnostics)
            self.assertNotIn('probes',diagnostics)

    def test_missing_human_owner_and_environment_mismatch_rejected(self):
        from copy import deepcopy
        for change in ('owner','environment'):
            binding=deepcopy(self.binding)
            if change=='owner':binding['responders']={}
            else:binding['environment']='other'
            with tempfile.TemporaryDirectory() as private,self.assertRaises(Invalid):
                Control(Path(private)/'control.sqlite').change('propose',self.actor,binding,self.config,0)

    def authority(self, directory, actions):
        import time
        path=Path(directory)/'authority.json'
        path.write_bytes(canonical({'version':'3.0','binding_sha256':sha(canonical(self.binding)),'expires_at':time.time()+300,'principals':[{'uid':os.geteuid(),'actor':self.actor,'actions':actions}]}))
        path.chmod(0o600)
        return path

    def test_real_os_principal_denial_preserves_durable_journal(self):
        with tempfile.TemporaryDirectory() as private:
            policy=self.authority(private,['propose'])
            control=Control(Path(private)/'control.sqlite',policy,phase=3)
            proposed=control.change('propose',self.actor,self.binding,self.config,0)
            for action in ('validate','activate','reject'):
                with self.subTest(action=action),self.assertRaises(Invalid):
                    control.change(action,self.actor,self.binding,self.config,1)
                self.assertEqual(control.current(),proposed)
            with self.assertRaises(Invalid):Control(control.path,phase=2)
            document=load(policy);document['principals'][0]['uid']=os.geteuid()+1
            policy.write_bytes(canonical(document))
            with self.assertRaises(Invalid):control.change('reject',self.actor,self.binding,self.config,1)
            self.assertEqual(control.current(),proposed)

    def test_public_expired_and_foreign_binding_authority_are_denied(self):
        with tempfile.TemporaryDirectory() as private:
            policy=self.authority(private,['propose'])
            control=Control(Path(private)/'control.sqlite',policy,phase=3)
            original=load(policy)
            for field,value in (('expires_at',1),('binding_sha256','0'*64)):
                document=dict(original);document[field]=value;policy.write_bytes(canonical(document))
                with self.assertRaises(Invalid):control.change('propose',self.actor,self.binding,self.config,0)
                self.assertIsNone(control.current())
            policy.write_bytes(canonical(original));policy.chmod(0o644)
            with self.assertRaises(Invalid):control.change('propose',self.actor,self.binding,self.config,0)
            self.assertIsNone(control.current())

    def test_competing_stale_actors_cannot_duplicate_transition(self):
        with tempfile.TemporaryDirectory() as private:
            control=Control(Path(private)/'control.sqlite')
            control.change('propose',self.actor,self.binding,self.config,0)
            def reject():
                try:
                    return control.change('reject',self.actor,self.binding,self.config,1)['sequence']
                except Invalid:
                    return 'stale'
            with ThreadPoolExecutor(max_workers=2) as pool:
                results=list(pool.map(lambda _:reject(),range(2)))
            self.assertCountEqual(results,[2,'stale'])
            self.assertEqual(control.current()['sequence'],2)
            with sqlite3.connect(control.path) as db:
                self.assertEqual(db.execute('SELECT COUNT(*) FROM transitions').fetchone()[0],2)

    def test_journal_column_corruption_cannot_accept_new_authority(self):
        with tempfile.TemporaryDirectory() as private:
            control=Control(Path(private)/'control.sqlite')
            control.change('propose',self.actor,self.binding,self.config,0)
            with sqlite3.connect(control.path) as db:
                db.execute('UPDATE transitions SET actor=? WHERE sequence=1',('unassigned-actor',))
            with self.assertRaises(Invalid):control.change('reject',self.actor,self.binding,self.config,1)
            with self.assertRaises(Invalid):control.current()
            with sqlite3.connect(control.path) as db:
                self.assertEqual(db.execute('SELECT COUNT(*) FROM transitions').fetchone()[0],1)

    def test_process_exit_during_sqlite_transaction_preserves_prior_authority(self):
        with tempfile.TemporaryDirectory() as private:
            control=Control(Path(private)/'control.sqlite')
            proposed=control.change('propose',self.actor,self.binding,self.config,0)
            script='import os,sqlite3,sys;db=sqlite3.connect(sys.argv[1]);db.execute("BEGIN IMMEDIATE");db.execute("UPDATE transitions SET state=\'activated\' WHERE sequence=1");os._exit(73)'
            result=subprocess.run([sys.executable,'-c',script,str(control.path)],capture_output=True,timeout=5)
            self.assertEqual(result.returncode,73)
            self.assertEqual(control.current(),proposed)

    def test_orphan_bundle_recovery_is_idempotent_and_tampering_is_denied(self):
        with tempfile.TemporaryDirectory() as private:
            control=Control(Path(private)/'control.sqlite')
            proposed=control.change('propose',self.actor,self.binding,self.config,0)
            first,manifest=immutable_bundle(self.binding,self.config,Path(private)/'bundles')
            second,recovered=immutable_bundle(self.binding,self.config,Path(private)/'bundles')
            self.assertEqual((first,manifest),(second,recovered))
            self.assertEqual(control.current(),proposed)
            (first/'otel-collector.yaml').write_bytes(b'altered')
            with self.assertRaises(Invalid):verify_bundle(self.binding,self.config,first)
            with self.assertRaises(Invalid):immutable_bundle(self.binding,self.config,Path(private)/'bundles')
            self.assertEqual(control.current(),proposed)

    def test_symlinked_authority_and_output_cannot_write_outside_private_target(self):
        with tempfile.TemporaryDirectory() as private:
            policy=self.authority(private,['propose'])
            link=Path(private)/'authority-link.json';link.symlink_to(policy)
            control=Control(Path(private)/'control.sqlite',link,phase=3)
            with self.assertRaises(OSError):control.change('propose',self.actor,self.binding,self.config,0)
            actual=Path(private)/'actual';actual.mkdir()
            output=Path(private)/'output';output.symlink_to(actual,target_is_directory=True)
            with self.assertRaises(Invalid):immutable_bundle(self.binding,self.config,output)
            self.assertEqual(list(actual.iterdir()),[])


if __name__=='__main__':unittest.main(verbosity=2)
