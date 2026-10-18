#!/usr/bin/env python3
"""Runner control-flow tests; these do not qualify any application service."""
import copy
import importlib.util
import json
import os
import hashlib
import tomllib
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import time
import unittest

RUNNER_PATH = Path(__file__).resolve().parents[1] / 'qualify.py'
SPEC = importlib.util.spec_from_file_location('production_qualifier', RUNNER_PATH)
runner = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(runner)


class RunnerTests(unittest.TestCase):
    def test_protocol_coverage_is_source_and_nested_outputs_stay_ignored(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / 'protocol' / 'coverage'
            source.mkdir(parents=True)
            (source / 'checkpoint.go').write_text('package coverage\n')
            (source / 'fixture.json').write_text('{"height":"125"}')
            (source / 'dist').mkdir()
            (source / 'dist' / 'generated.go').write_text('ignored')
            (source / 'coverage').mkdir()
            (source / 'coverage' / 'index.html').write_text('ignored')
            (source / 'node_modules').symlink_to(root / 'unread-dependencies', target_is_directory=True)
            (source / '.env').symlink_to(root / 'unread-secret')
            (root / 'coverage').mkdir()
            (root / 'coverage' / 'index.html').write_text('ignored')
            first = runner.snapshot(root)
            self.assertEqual(set(first['files']), {'protocol/coverage/checkpoint.go', 'protocol/coverage/fixture.json'})
            (source / 'checkpoint.go').write_text('package coverage\n// actual source edit\n')
            self.assertNotEqual(first['sha256'], runner.snapshot(root)['sha256'])
            second = runner.snapshot(root)
            (source / 'dist' / 'generated.go').write_text('changed ignored output')
            self.assertEqual(second, runner.snapshot(root))

    def test_shared_secrets_hashes_only_direct_go_source(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / 'shared' / 'secrets'
            source.mkdir(parents=True)
            (source / 'manager.go').write_text('package secrets\n')
            (source / 'manager_test.go').write_text('package secrets\n')
            for name in ('credentials.json', 'private.pem', '.env', 'key.txt'):
                (source / name).symlink_to(root / 'unread-secret')
            (source / 'generated').mkdir()
            (source / 'generated' / 'secret.go').symlink_to(root / 'unread-secret')
            first = runner.snapshot(root)
            self.assertEqual(set(first['files']), {'shared/secrets/manager.go', 'shared/secrets/manager_test.go'})
            (source / 'manager.go').write_text('package secrets\n// actual source edit\n')
            self.assertNotEqual(first['sha256'], runner.snapshot(root)['sha256'])

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.evidence = self.root / 'evidence'
        self.evidence.mkdir()

    def check(self, source, timeout=5):
        program = self.root / 'probe.py'
        program.write_text(source)
        return {'id': 'probe', 'kind': 'test', 'target': 'runner-control-flow',
                'argv': [sys.executable, str(program)], 'cwd': '.',
                'timeout_seconds': timeout, 'env': {}, 'required': True}

    def test_real_process_success_records_exit_and_exact_output(self):
        result = runner.execute_check(self.check("print('observed output')\n"), self.root, self.evidence)
        self.assertTrue(result['passed'])
        self.assertEqual(result['exit_code'], 0)
        self.assertEqual(Path(result['log']).read_text(), 'observed output\n')
        self.assertEqual(result['log_sha256'], runner.file_hash(Path(result['log'])))

    def test_real_failure_is_preserved(self):
        result = runner.execute_check(self.check("import sys\nprint('failure detail',file=sys.stderr)\nsys.exit(7)\n"), self.root, self.evidence)
        self.assertFalse(result['passed'])
        self.assertEqual(result['exit_code'], 7)
        self.assertIn('failure detail', Path(result['log']).read_text())

    def test_timeout_terminates_actual_process(self):
        result = runner.execute_check(self.check('import time\ntime.sleep(30)\n', timeout=1), self.root, self.evidence)
        self.assertFalse(result['passed'])
        self.assertTrue(result['timed_out'])
        self.assertLess(result['duration_seconds'], 10)
        self.assertNotEqual(result['exit_code'], 0)

    def test_missing_executable_cannot_pass(self):
        check = self.check('')
        check['argv'] = [str(self.root / 'does-not-exist')]
        result = runner.execute_check(check, self.root, self.evidence)
        self.assertFalse(result['passed'])
        self.assertIsNone(result['exit_code'])
        self.assertIn('launch_error', result)

    def test_source_digest_changes_on_added_modified_deleted_files(self):
        source = self.root / 'source.go'
        empty = runner.snapshot(self.root)
        source.write_text('package source\n')
        added = runner.snapshot(self.root)
        self.assertNotEqual(empty['sha256'], added['sha256'])
        source.write_text('package updated\n')
        modified = runner.snapshot(self.root)
        self.assertNotEqual(added['sha256'], modified['sha256'])
        source.unlink()
        self.assertEqual(empty['sha256'], runner.snapshot(self.root)['sha256'])

    def test_evidence_and_secrets_are_not_read_or_hashed(self):
        before = runner.snapshot(self.root)
        (self.root / '.env').write_text('NOT_A_REAL_CREDENTIAL=example\n')
        (self.evidence / 'log').write_text('generated evidence\n')
        self.assertEqual(before['sha256'], runner.snapshot(self.root)['sha256'])

    def complete_registry(self):
        checks = []
        for kind, targets in runner.REQUIRED_TARGETS.items():
            for index, target in enumerate(sorted(targets)):
                checks.append({'id': f'{kind}-{index}', 'kind': kind, 'target': target,
                               'argv': [sys.executable, '--version'], 'cwd': '.',
                               'timeout_seconds': 5, 'required': True})
        return {'schema_version': 1, 'tasks': {'0.1': {'checks': checks}}}

    def test_registry_requires_every_registered_target(self):
        registry = self.complete_registry()
        self.assertEqual(len(runner.validate_registry(registry, '0.1', self.root)), len(registry['tasks']['0.1']['checks']))
        registry['tasks']['0.1']['checks'].pop()
        with self.assertRaises(runner.QualificationError):
            runner.validate_registry(registry, '0.1', self.root)

    def test_registry_rejects_skip_optional_duplicate_and_escape(self):
        for change in ({'skip': True}, {'required': False}, {'cwd': '..'}, {'argv': []}, {'timeout_seconds': 0}):
            registry = self.complete_registry()
            registry['tasks']['0.1']['checks'][0].update(change)
            with self.subTest(change=change), self.assertRaises(runner.QualificationError):
                runner.validate_registry(registry, '0.1', self.root)
        registry = self.complete_registry()
        registry['tasks']['0.1']['checks'].append(copy.deepcopy(registry['tasks']['0.1']['checks'][0]))
        with self.assertRaises(runner.QualificationError):
            runner.validate_registry(registry, '0.1', self.root)

    def test_unknown_task_rejects_before_execution(self):
        process = subprocess.run([sys.executable, str(RUNNER_PATH), '--task', '999.999',
                                  '--spec', 'spec/production-system/spec.kvx',
                                  '--require-current-source'], capture_output=True, text=True)
        self.assertEqual(process.returncode, 1)
        observed = json.loads(process.stdout)
        self.assertFalse(observed['qualified'])
        self.assertIn('not implemented', observed['errors'][0])
        evidence = json.loads(Path(observed['result']).read_text())
        self.assertEqual(evidence['checks'], [])

    def test_evidence_tokens_expand_without_shell_interpretation(self):
        check = self.check("import os,sys\nprint(sys.argv[1])\nprint(os.environ['QUALIFICATION_EVIDENCE_DIR'])\n")
        check['argv'].append('{evidence_dir}/literal $HOME $(whoami)')
        result = runner.execute_check(check, self.root, self.evidence)
        self.assertTrue(result['passed'])
        content = Path(result['log']).read_text()
        self.assertIn(str(self.evidence) + '/literal $HOME $(whoami)', content)
        self.assertEqual(content.splitlines()[1], str(self.evidence))

    def test_dist_build_outputs_do_not_change_source_digest(self):
        before = runner.snapshot(self.root)
        output = self.root / 'client/packages/example/dist'
        output.mkdir(parents=True)
        (output / 'index.js').write_text('export const generated = true;\n')
        self.assertEqual(before['sha256'], runner.snapshot(self.root)['sha256'])

    def test_sdk_declared_test_requires_registered_target(self):
        package = self.root / 'client/packages/example'
        package.mkdir(parents=True)
        (package / 'package.json').write_text(json.dumps({'scripts': {'test': 'node --test'}}))
        with self.assertRaises(runner.QualificationError):
            runner.validate_registry(self.complete_registry(), '0.1', self.root)

    def test_real_private_anvil_start_identity_and_cleanup(self):
        wallet_path = RUNNER_PATH.parent / 'bootstrap/wallet_fixture.py'
        module_spec = importlib.util.spec_from_file_location('wallet_fixture', wallet_path)
        wallet = importlib.util.module_from_spec(module_spec)
        module_spec.loader.exec_module(wallet)
        with (self.evidence / 'anvil.log').open('wb') as log:
            process, url = wallet.start_node(log)
            try:
                self.assertTrue(url.startswith('http://127.0.0.1:'))
                self.assertEqual(wallet.rpc(url, 'eth_chainId'), '0x7d')
                self.assertIn('anvil', wallet.rpc(url, 'web3_clientVersion').lower())
            finally:
                wallet.stop(process)
        self.assertIsNotNone(process.poll())

    def test_actual_node_tap_pass_and_skip_are_distinguished(self):
        program = self.root / 'native.test.mjs'
        check = self.check('')
        check.update(argv=['node', '--test', '--test-reporter=tap', str(program)], evidence_format='node-tap')
        program.write_text("import test from 'node:test';import assert from 'node:assert/strict';test('actual assertion',()=>assert.equal(2+2,4));\n")
        passed = runner.execute_check(check, self.root, self.evidence)
        self.assertTrue(passed['passed'])
        self.assertEqual(passed['native_test_totals']['tests'], 1)
        program.write_text("import test from 'node:test';test.skip('unexecuted assertion',()=>{});\n")
        skipped = runner.execute_check(check, self.root, self.evidence)
        self.assertEqual(skipped['exit_code'], 0)
        self.assertFalse(skipped['passed'])
        self.assertEqual(skipped['native_test_totals']['skipped'], 1)

    def concurrent_probe(self, identifier, target, kind='test', code=0, delay=0.12):
        script = self.root / 'concurrent_probe.py'
        script.write_text("""import fcntl,json,os,sys,time
path,identifier,delay,code=sys.argv[1:]
def record(event):
 with open(path,'a') as out:
  fcntl.flock(out,fcntl.LOCK_EX)
  out.write(json.dumps({'id':identifier,'event':event,'time':time.monotonic()})+'\\n')
  out.flush()
  fcntl.flock(out,fcntl.LOCK_UN)
record('begin')
time.sleep(float(delay))
record('end')
sys.exit(int(code))
""")
        return {'id': identifier, 'kind': kind, 'target': target,
                'argv': [sys.executable, str(script), str(self.evidence / 'timeline.jsonl'), identifier, str(delay), str(code)],
                'cwd': '.', 'timeout_seconds': 20, 'required': True, 'env': {}}

    def test_two_go_suites_overlap_but_never_three_and_failure_is_preserved(self):
        checks = [self.concurrent_probe('go-'+str(index), target, code=7 if index==1 else 0)
                  for index,target in enumerate(['core/indexer','core/trading-charts','core/stats-workers','core/pnl-tracker'])]
        checks.append(self.concurrent_probe('sdk', 'client/packages/example'))
        checks.append(self.concurrent_probe('build', 'core/api', kind='build'))
        observed, publisher_threads = {}, []
        def publish(position,result):
            observed[position] = result
            publisher_threads.append(threading.get_ident())
        runner.execute_checks(checks,self.root,self.evidence,self.root/'contracts',publish,runner.ExecutionControl())
        events = [json.loads(line) for line in (self.evidence/'timeline.jsonl').read_text().splitlines()]
        active, maximum = set(), 0
        for event in events:
            if event['event']=='begin':
                active.add(event['id'])
                maximum=max(maximum,len(active))
                if event['id'] in {'sdk','build'}:
                    self.assertEqual(active,{event['id']})
            else:
                active.remove(event['id'])
        self.assertEqual(maximum,2)
        self.assertEqual(active,set())
        self.assertEqual(len(observed),len(checks))
        self.assertEqual(observed[1]['exit_code'],7)
        self.assertFalse(observed[1]['passed'])
        self.assertTrue(all(value['passed'] for key,value in observed.items() if key!=1))
        self.assertTrue(all(value==threading.get_ident() for value in publisher_threads))
        self.assertEqual([observed[key]['id'] for key in sorted(observed)],[check['id'] for check in checks])

    def test_cancellation_stops_new_jobs_and_preserves_actual_child_exits(self):
        checks=[self.concurrent_probe('cancel-'+str(index),target,delay=30)
                for index,target in enumerate(['core/indexer','core/trading-charts','core/stats-workers'])]
        control=runner.ExecutionControl()
        observed={}
        timer=threading.Timer(0.3,lambda:control.cancel('test cancellation'))
        started=time.monotonic()
        timer.start()
        try:
            with self.assertRaises(runner.QualificationCancelled):
                runner.execute_checks(checks,self.root,self.evidence,self.root/'contracts',lambda position,result:observed.__setitem__(position,result),control)
        finally:
            timer.cancel()
        self.assertLess(time.monotonic()-started,10)
        self.assertEqual(set(observed),{0,1})
        self.assertTrue(all(result.get('cancelled') and not result['passed'] and result['exit_code'] is not None for result in observed.values()))
        events=[json.loads(line) for line in (self.evidence/'timeline.jsonl').read_text().splitlines()]
        self.assertNotIn('cancel-2',{event['id'] for event in events})

    def interface_registry(self):
        (self.root/'schema.json').write_text('{"type":"integer"}\n')
        (self.root/'types.gen.go').write_text('package generated\n')
        bundle={'inputs':['schema.json'],'outputs':['types.gen.go']}
        bundle['expected_sha256']=runner.canonical_hash({'inputs':{'schema.json':runner.file_hash(self.root/'schema.json')},'outputs':{'types.gen.go':runner.file_hash(self.root/'types.gen.go')}})
        checks=[]
        for kind,targets in runner.INTERFACE_REQUIRED_TARGETS.items():
            for index,target in enumerate(sorted(targets)):
                checks.append({'id':kind+'-'+str(index),'kind':kind,'target':target,'argv':[sys.executable,'--version'],'cwd':'.','timeout_seconds':5,'required':True})
        return {'schema_version':1,'tasks':{'0.2':{'interface_bundle':bundle,'checks':checks}}}

    def test_interface_policy_does_not_demand_bootstrap_retests(self):
        registry=self.interface_registry()
        normalized=runner.validate_registry(registry,'0.2',self.root)
        self.assertEqual(len(normalized),21)
        self.assertNotIn('core/indexer',{check['target'] for check in normalized})
        registry['tasks']['0.2']['checks'].pop()
        with self.assertRaises(runner.QualificationError):
            runner.validate_registry(registry,'0.2',self.root)

    def test_frozen_interface_bundle_rejects_missing_stale_input_and_output(self):
        registry=self.interface_registry()
        bundle=registry['tasks']['0.2']['interface_bundle']
        before=runner.interface_bundle(bundle,self.root)
        self.assertEqual(before['sha256'],bundle['expected_sha256'])
        for relative in ('schema.json','types.gen.go'):
            path=self.root/relative
            original=path.read_text()
            path.write_text(original+'changed\n')
            with self.subTest(relative=relative),self.assertRaises(runner.QualificationError):
                runner.interface_bundle(bundle,self.root)
            path.write_text(original)
        (self.root/'types.gen.go').unlink()
        with self.assertRaises(runner.QualificationError):
            runner.interface_bundle(bundle,self.root)

    def test_frozen_interface_bundle_rejects_absent_hash_or_escaping_path(self):
        bundle=self.interface_registry()['tasks']['0.2']['interface_bundle']
        for update in ({'expected_sha256':'0'*64},{'inputs':['../schema.json']},{'expected_sha256':None}):
            with self.subTest(update=update),self.assertRaises(runner.QualificationError):
                runner.interface_bundle({**bundle,**update},self.root)

    def test_absent_python_checker_is_actual_nonzero_failure(self):
        check=self.check('')
        check['argv']=[sys.executable,str(self.root/'absent_checker.py')]
        observed=runner.execute_check(check,self.root,self.evidence)
        self.assertEqual(observed['exit_code'],2)
        self.assertFalse(observed['passed'])
        self.assertIn('absent_checker.py',Path(observed['log']).read_text())

    def test_single_real_persistence_checker_must_explicitly_cover_migration_validation(self):
        registry=self.interface_registry()
        checks=registry['tasks']['0.2']['checks']
        checks[:]=[check for check in checks if check['target']!='migration-ownership']
        postgres=next(check for check in checks if check['target']=='postgres-privileges')
        postgres['argv']=['python3','tools/production/interfaces/persistence_check.py','--compose','deploy/production/bootstrap/compose.yml']
        with self.assertRaises(runner.QualificationError):
            runner.validate_registry(registry,'0.2',self.root)
        postgres['covers']={'baseline':['migration-ownership']}
        self.assertEqual(len(runner.validate_registry(registry,'0.2',self.root)),20)
        postgres['argv']=[sys.executable,'--version']
        with self.assertRaises(runner.QualificationError):
            runner.validate_registry(registry,'0.2',self.root)

    def test_fresh_run_anchor_is_exact_literal_argument_and_environment(self):
        control=runner.ExecutionControl()
        check=self.check("import os,sys\nprint(sys.argv[1])\nprint(os.environ['QUALIFICATION_STARTED_AT'])\n")
        check['argv'].append('{qualification_started_at}')
        observed=runner.execute_check(check,self.root,self.evidence,control=control)
        self.assertTrue(observed['passed'])
        self.assertEqual(Path(observed['log']).read_text().splitlines(),[control.started_at,control.started_at])

    def test_endpoint_examples_checker_is_mandatory(self):
        registry=self.interface_registry()
        checks=registry['tasks']['0.2']['checks']
        checks[:]=[check for check in checks if check['target']!='endpoint-examples']
        with self.assertRaises(runner.QualificationError):
            runner.validate_registry(registry,'0.2',self.root)

    def test_recursive_interface_bundle_ignores_normal_import_and_build_cache(self):
        inputs=self.root/'inputs'
        outputs=self.root/'outputs'
        inputs.mkdir()
        outputs.mkdir()
        (inputs/'schema.json').write_text('{}')
        (outputs/'generated.json').write_text('{}')
        bundle={'inputs':['inputs'],'outputs':['outputs']}
        initial=runner.interface_bundle(bundle,self.root,verify_expected=False)
        bundle['expected_sha256']=initial['sha256']
        cache=inputs/'__pycache__'
        cache.mkdir()
        (cache/'module.cpython-311.pyc').write_bytes(b'normal generated bytecode')
        dist=outputs/'dist'
        dist.mkdir()
        (dist/'generated.js').write_text('generated build output')
        self.assertEqual(runner.interface_bundle(bundle,self.root),initial)

    def dispatcher_fixture(self):
        control=runner.ExecutionControl()
        control.candidate_sha256='a'*64
        directory=self.evidence/'dispatcher'
        directory.mkdir(exist_ok=True)
        artifact=directory/'probe.log'
        artifact.write_text('parser fixture only; not dispatcher qualification')
        scenarios=[{'id':identifier,'status':'passed','checks':1,'duration_seconds':0.1,'artifacts':{'probe.log':runner.file_hash(artifact)}} for identifier in sorted(runner.DISPATCH_SCENARIOS)]
        fifty=next(s for s in scenarios if s['id']=='fifty-process-leases')
        fifty['processes']=[{'pid':i+1,'lane':f'L{i+1:02}','attempt':f'a{i}','fence':1,'workspace':f'/fixture/{i}','heartbeat_count':1,'started_monotonic':1,'ended_monotonic':3} for i in range(50)]
        fifty['overlap_seconds']=2
        doc={'schema_version':1,'task':'0.3','status':'passed','source_sha256':control.candidate_sha256,'started_at':control.started_at,'scenarios':scenarios}
        path=directory/'result.json'
        path.write_text(json.dumps(doc))
        return control,doc,path

    def test_dispatcher_parser_complete_fixture_is_not_runtime_qualification(self):
        control,doc,path=self.dispatcher_fixture()
        observed=runner.dispatcher_evidence(self.evidence,control)
        self.assertEqual(observed['scenario_count'],11)
        self.assertEqual(observed['check_count'],11)

    def test_dispatcher_timestamp_serialization_precision_keeps_strict_anchor(self):
        control,doc,path=self.dispatcher_fixture()
        serialized=runner.datetime.fromisoformat(control.started_at).timestamp()
        control.started_epoch=serialized+0.0000002
        self.assertEqual(runner.dispatcher_evidence(self.evidence,control)['scenario_count'],11)
        observed=runner.datetime.fromisoformat(control.started_at)
        from datetime import timedelta
        doc['started_at']=(observed-timedelta(microseconds=1)).isoformat()
        path.write_text(json.dumps(doc))
        with self.assertRaises(runner.QualificationError):runner.dispatcher_evidence(self.evidence,control)

    def test_dispatcher_missing_stale_empty_duplicate_and_failed_proofs_reject(self):
        control,doc,path=self.dispatcher_fixture()
        mutations=[lambda d:d.update(source_sha256='b'*64),lambda d:d.update(started_at='2000-01-01T00:00:00+00:00'),lambda d:d['scenarios'].pop(),lambda d:d['scenarios'][0].update(checks=0),lambda d:d['scenarios'][0].update(checks=True),lambda d:d['scenarios'][0].update(status='skipped'),lambda d:d['scenarios'][0].update(artifacts={}),lambda d:d['scenarios'][0].update(id=d['scenarios'][1]['id'])]
        for mutation in mutations:
            changed=copy.deepcopy(doc);mutation(changed);path.write_text(json.dumps(changed))
            with self.subTest(changed=changed),self.assertRaises(runner.QualificationError):
                runner.dispatcher_evidence(self.evidence,control)
        path.unlink()
        with self.assertRaises(runner.QualificationError):runner.dispatcher_evidence(self.evidence,control)

    def test_dispatcher_fifty_identity_overlap_and_artifact_hash_reject(self):
        control,doc,path=self.dispatcher_fixture()
        for field,value in [('pid',1),('lane','L01'),('heartbeat_count',0),('fence',0),('ended_monotonic',1.1)]:
            changed=copy.deepcopy(doc)
            fifty=next(s for s in changed['scenarios'] if s['id']=='fifty-process-leases')
            fifty['processes'][1][field]=value
            path.write_text(json.dumps(changed))
            with self.subTest(field=field),self.assertRaises(runner.QualificationError):runner.dispatcher_evidence(self.evidence,control)
        path.write_text(json.dumps(doc))
        (self.evidence/'dispatcher/probe.log').write_text('tampered')
        with self.assertRaises(runner.QualificationError):runner.dispatcher_evidence(self.evidence,control)

    def test_dispatcher_actual_zero_exit_without_native_evidence_fails(self):
        check=self.check("print('not proof')")
        check['evidence_format']='dispatcher-json'
        observed=runner.execute_check(check,self.root,self.evidence)
        self.assertEqual(observed['exit_code'],0)
        self.assertFalse(observed['passed'])

    def test_dispatcher_registry_missing_checker_and_wrong_command_reject(self):
        bundle=self.interface_registry()['tasks']['0.2']['interface_bundle']
        argv=['python3','tools/production/dispatcher/qualification.py','--spec','spec/production-system/spec.kvx','--plan','spec/production-system/execution-plan.json','--state-dir','{evidence_dir}/dispatcher-state','--evidence-dir','{evidence_dir}/dispatcher','--case','all']
        entry={'interface_bundle':bundle,'checks':[{'id':'dispatcher','kind':'dispatcher','target':'scheduler-harness','required':True,'evidence_format':'dispatcher-json','argv':argv,'cwd':'.','timeout_seconds':600}]}
        registry={'schema_version':1,'tasks':{'0.3':entry}}
        with self.assertRaises(runner.QualificationError):runner.validate_registry(registry,'0.3',self.root)
        checker=self.root/argv[1];checker.parent.mkdir(parents=True);checker.write_text('# parser registry fixture')
        self.assertEqual(len(runner.validate_registry(registry,'0.3',self.root)),1)
        entry['checks'][0]['argv']=[sys.executable,'--version']
        with self.assertRaises(runner.QualificationError):runner.validate_registry(registry,'0.3',self.root)

    def test_actual_cli_accepted_candidate_positive_and_failclosed_negatives(self):
        # This tests the generic acceptance protocol using an isolated real unittest.
        # It does not register or qualify any production feature task.
        dispatcher=Path(os.environ.get('PRODUCTION_DISPATCHER_SOURCE',str(RUNNER_PATH.parent/'dispatcher')))
        canonical=Path(os.environ.get('PRODUCTION_DISPATCHER_SPEC_ROOT',str(RUNNER_PATH.parents[2])))
        self.assertTrue((dispatcher/'cli.py').is_file(),'actual dispatcher CLI required')
        target=self.root/'coordinator-source';target.mkdir()
        specdir=target/'spec/production-system';specdir.mkdir(parents=True)
        text=(canonical/'spec/production-system/spec.kvx').read_text()
        import importlib.util, re
        fixture_spec=importlib.util.spec_from_file_location('qualification_fixture',dispatcher/'qualification_fixture.py')
        fixture_module=importlib.util.module_from_spec(fixture_spec);fixture_spec.loader.exec_module(fixture_module)
        plan=json.loads((canonical/'spec/production-system/execution-plan.json').read_text())
        text,plan=fixture_module.normalize_fixture(text,plan)
        specpath=specdir/'spec.kvx';specpath.write_text(text)
        planpath=specdir/'execution-plan.json';planpath.write_text(json.dumps(plan))
        task=runner.task_at(tomllib.loads(text),'1.01')
        source=self.root/'baseline-source';owned=source/'shared/chain/readpolicy';owned.mkdir(parents=True)
        (owned/'value.txt').write_text('baseline')
        (owned/'test_value.py').write_text("import unittest\nfrom pathlib import Path\nclass ActualTest(unittest.TestCase):\n def test_real_value(self): self.assertEqual(Path('shared/chain/readpolicy/value.txt').read_text(),'implemented')\n")
        (target/'schema.json').write_text('{"type":"integer"}')
        (target/'generated.json').write_text('{"generated":true}')
        bundle={'inputs':['schema.json'],'outputs':['generated.json']}
        bundle['expected_sha256']=runner.interface_bundle(bundle,target,verify_expected=False)['sha256']
        state=self.root/'private-state';capacity=self.root/'capacity.json';resources=self.root/'resources.json'
        capacity.write_text(json.dumps(dict(cpu=1,memory=64*1024**2,pids=20,disk=64*1024**2,executor=1,go_build=1,container_start=1)))
        resources.write_text(json.dumps(dict(cpu=.5,memory=1024**2,pids=2,disk=1024**2,executor=1,go_build=0,container_start=0)))
        commands=[]
        def cli(*args,fail=False):
            command=[sys.executable,str(dispatcher/'cli.py'),'--state-dir',str(state),*map(str,args)]
            result=subprocess.run(command,capture_output=True,text=True,timeout=30,env={**os.environ,'PYTHONDONTWRITEBYTECODE':'1'})
            commands.append({'argv':command,'exit_code':result.returncode,'stdout':result.stdout,'stderr':result.stderr})
            if fail:
                self.assertNotEqual(result.returncode,0,'forbidden actual CLI operation succeeded')
                return None
            self.assertEqual(result.returncode,0,result.stderr)
            return json.loads(result.stdout)
        cli('init','--spec',specpath,'--plan',planpath,'--source',source,'--interface-digest',bundle['expected_sha256'],'--capacity',capacity,'--available-ai-slots','1')
        claimed=cli('claim','--task','1.01','--owner','actual-protocol-test','--resources',resources)
        token=Path(claimed['token']);workspace=Path(claimed['lease']['workspace'])
        (workspace/'shared/chain/readpolicy/value.txt').write_text('implemented')
        cli('candidate','--token',token);cli('patch','--token',token)
        checks=[{'id':'actual-native-unittest','command':[sys.executable,'-m','unittest','discover','-s','shared/chain/readpolicy','-p','test_*.py','-v'],'timeout':10,'proof':'unittest','inputs':['shared/chain/readpolicy/value.txt','shared/chain/readpolicy/test_value.py']}]
        checksfile=self.root/'checks.json'
        for bad_command in ([sys.executable,'-m','unittest','discover','-s','shared/chain/readpolicy','-p','missing.py','-v'],[sys.executable,'-c',"import unittest; T=type('Skipped',(unittest.TestCase,),{'test_skip':unittest.skip('deliberate')(lambda self:None)}); unittest.main()"]):
            rejected=[{**checks[0],'command':bad_command}]
            checksfile.write_text(json.dumps(rejected));cli('register-checks','--task','1.01','--checks',checksfile)
            cli('qualify','--token',token,fail=True)
        checksfile.write_text(json.dumps(checks));cli('register-checks','--task','1.01','--checks',checksfile)
        cli('qualify','--token',token);accepted=cli('accept','--token',token)
        acceptedpath=Path(accepted['accepted_path']);body=json.loads((acceptedpath/'accepted.json').read_text());config=json.loads((state/'config.json').read_text())
        policy={'task':'1.01','lane':task['lane'],'phase':task['phase'],'reqs':task['reqs'],'owned':task['touches'],'baseline_sha256':body['bindings']['baseline'],'interface_sha256':body['bindings']['interface'],'layout_sha256':body['bindings']['layout'],'required_checks':checks}
        entry={'accepted_candidate_policy':policy,'policy_sha256':runner.canonical_hash(policy),'coordinator_state_dir':str(state),'accepted_artifact_id':accepted['id'],'interface_bundle':bundle}
        prior=os.environ.get('QUALIFICATION_ACCEPTANCE_ID');os.environ['QUALIFICATION_ACCEPTANCE_ID']=accepted['id']
        self.addCleanup(lambda:os.environ.pop('QUALIFICATION_ACCEPTANCE_ID',None) if prior is None else os.environ.__setitem__('QUALIFICATION_ACCEPTANCE_ID',prior))
        sys.path.insert(0,str(dispatcher.parent));self.addCleanup(lambda:sys.path.remove(str(dispatcher.parent)))
        def verify(candidate_entry=entry):
            output=self.evidence/str(time.time_ns());output.mkdir()
            return runner.qualify_accepted_candidate(candidate_entry,'1.01',task,tomllib.loads(text),target,output)
        observed=verify();self.assertTrue(observed['qualified']);self.assertEqual(len(observed['checks']),1);self.assertGreater(observed['checks'][0]['native_proof']['positive_count'],0);self.assertFalse(observed['canonical_integrated']);self.assertEqual(observed['source_scope'],'isolated_candidate')
        self.assertEqual((source/'shared/chain/readpolicy/value.txt').read_text(),'baseline')
        # Run the actual qualifier CLI on an isolated claimed fixture ledger.
        taskstart=text.index('[task.1.01]');taskend=text.index('\n[task.',taskstart+1)
        claimedchunk=re.sub(r'^status = ".*"$', 'status = "implemented"',text[taskstart:taskend],flags=re.M)
        claimedtext=text[:taskstart]+claimedchunk+text[taskend:];specpath.write_text(claimedtext)
        plan['spec_sha256']=hashlib.sha256(specpath.read_bytes()).hexdigest();plan['tasks']['1.01']['status']='implemented'
        for projected_lane in plan['lanes']:
            for projected_task in projected_lane['tasks']:
                if projected_task['id']=='1.01':projected_task['status']='implemented'
        planpath.write_text(json.dumps(plan))
        tools=target/'tools/production';(tools/'bootstrap').mkdir(parents=True)
        (tools/'qualify.py').write_bytes(RUNNER_PATH.read_bytes())
        (tools/'dispatcher').symlink_to(dispatcher,target_is_directory=True)
        (tools/'bootstrap/check-registry.json').write_text(json.dumps({'schema_version':1,'tasks':{'0.2':{'interface_bundle':bundle},'1.01':entry}}))
        process=subprocess.run([sys.executable,str(tools/'qualify.py'),'--task','1.01','--spec','spec/production-system/spec.kvx','--require-current-source'],capture_output=True,text=True,timeout=30,env={**os.environ,'PYTHONDONTWRITEBYTECODE':'1'})
        self.assertEqual(process.returncode,0,process.stdout+process.stderr)
        qualification=json.loads(Path(json.loads(process.stdout)['result']).read_text())
        self.assertTrue(qualification['qualified']);self.assertFalse(qualification['canonical_integrated'])
        self.assertEqual(qualification['qualification_scope'],'isolated-candidate-qualified')
        self.assertEqual(len(qualification['checks']),1)
        for change in ({'accepted_artifact_id':'another'},{'policy_sha256':'0'*64}):
            with self.subTest(change=change),self.assertRaises(runner.QualificationError):verify({**entry,**change})
        for key,value in [('lane','L02'),('reqs',[]),('required_checks',[]),('baseline_sha256','0'*64),('interface_sha256','0'*64),('layout_sha256','0'*64)]:
            changed=copy.deepcopy(entry);changed['accepted_candidate_policy'][key]=value;changed['policy_sha256']=runner.canonical_hash(changed['accepted_candidate_policy'])
            with self.subTest(key=key),self.assertRaises(ValueError):verify(changed)
        manifest=acceptedpath/'accepted.json';original=manifest.read_bytes();manifest.write_text('{}')
        with self.assertRaises(ValueError):verify()
        manifest.write_bytes(original)
        original_checks=(state/'checks.json').read_bytes();(state/'checks.json').write_text('{}')
        with self.assertRaises(runner.QualificationError):verify()
        (state/'checks.json').write_bytes(original_checks)
        config['fixture']=True;(state/'config.json').write_text(json.dumps(config))
        with self.assertRaises(runner.QualificationError):verify()
        print(json.dumps({'actual_acceptance_protocol_commands':len(commands),'actual_nonzero_rejections':sum(c['exit_code']!=0 for c in commands),'isolated_candidate_only':True}),flush=True)
        (self.evidence/'actual-cli-acceptance-commands.json').write_text(json.dumps(commands,indent=2))

    def test_task_resolution_respects_numeric_toml_nesting(self):
        spec = {'task': {'0': {'1': {'title': 'bootstrap'}}}}
        self.assertEqual(runner.task_at(spec, '0.1')['title'], 'bootstrap')
        with self.assertRaises(runner.QualificationError):
            runner.task_at(spec, '0.2')


if __name__ == '__main__':
    unittest.main(verbosity=2)
