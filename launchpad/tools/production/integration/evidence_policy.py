"""Fail-closed phase1 manifest validation; does not promote runtime evidence."""
import json
from pathlib import Path

def validate_phase1(directory,expected_binding):
    root=Path(directory)
    policy=json.loads((root/"phase-1-evidence-policy.json").read_text())
    manifest=json.loads((root/"phase-1-scenario-manifest.json").read_text())
    required={"baseline","candidate","interface","layout"}
    if set(expected_binding)!=required or any(not isinstance(v,str) or len(v)!=64 or any(c not in "0123456789abcdef" for c in v) for v in expected_binding.values()):
        raise ValueError("invalid coordinator bindings")
    for doc in (policy,manifest):
        if doc.get("binding")!=expected_binding or doc.get("chain_id")!=125:
            raise ValueError("binding mismatch")
    if policy.get("production_qualified") is not False or manifest.get("release_qualified") is not False:
        raise ValueError("phase1 cannot certify production")
    if policy.get("phase1_maximum")!=["synthetic_mathematical","actual_local_adapter"]:
        raise ValueError("phase1 evidence escalation")
    if manifest.get("evidence_policy")!="phase-1-evidence-policy.json": raise ValueError("unbound policy")
    expected={"D16_%03d" % i for i in list(range(11,23))+[29]}
    scenarios=manifest.get("requirements",[])
    if len(scenarios)!=13 or {s.get("id") for s in scenarios}!=expected:
        raise ValueError("scenario omission or duplication")
    if any(s.get("status")!="blocked_pending_integrated_candidate_and_topology" or len(s.get("acceptance",[]))!=5 for s in scenarios):
        raise ValueError("unqualified scenario escalation")
    return {"valid":True,"production_qualified":False,"scenarios":len(scenarios)}

PHASE2_SCENARIOS = frozenset({'deployment-registry', 'persisted-browser-journeys', 'ten-duplicate-redelivery', 'receipt-withheld-reverted', 'concurrent-candle-precommit-crash'})

PHASE3_SCENARIOS = frozenset({'scanner-webhook-auth', 'all-consumer-commit-ack', 'az-loss-ledger', 'redis-outage', 'nats-quorum-loss', 'object-storage-failure', 'application-restart', 'database-pool-exhaustion', 'rpc-integrity-quarantine', 'incompatible-rollback-denial'})


def validate_phase2(directory, result, expected_binding, started_epoch):
    import hashlib
    import math
    import time
    root = Path(directory).resolve()
    fields = {'baseline', 'candidate', 'interface', 'layout', 'topology'}
    if set(expected_binding) != fields or any(not isinstance(v, str) or len(v) != 64 or any(c not in '0123456789abcdef' for c in v) for v in expected_binding.values()):
        raise ValueError('invalid phase2 candidate/interface/topology bindings')
    if result.get('schema_version') != 2 or result.get('task') != '2.49' or result.get('binding') != expected_binding:
        raise ValueError('phase2 identity or bindings differ')
    if result.get('phase2_qualified') is not True or any(result.get(key) is not False for key in ('production_qualified', 'capacity_qualified', 'disaster_recovery_qualified')):
        raise ValueError('phase2 cannot certify capacity or production')
    before, after = result.get('started_epoch'), result.get('finished_epoch')
    if type(before) not in (int, float) or type(after) not in (int, float) or not math.isfinite(before) or not math.isfinite(after) or not started_epoch <= before <= after <= time.time() + 1:
        raise ValueError('phase2 evidence is stale or time is invalid')
    scenarios = result.get('scenarios', [])
    if len(scenarios) != len(PHASE2_SCENARIOS) or {s.get('id') for s in scenarios} != PHASE2_SCENARIOS or any(s.get('passed') is not True or type(s.get('checks')) is not int or s['checks'] <= 0 for s in scenarios):
        raise ValueError('phase2 scenario omitted, failed, duplicated or empty')
    executions = result.get('executions', [])
    names = {r.get('name') for r in executions}
    required = {'registry', 'journeys', 'commit_before_ack', 'duplicate_delivery', 'receipt-withheld-reverted', 'arm_candle', 'concurrent-candle-fills', 'crash_candle', 'recover_candle'}
    if len(executions) != len(required) or names != required:
        raise ValueError('actual phase2 native execution inventory incomplete')
    for entry in executions:
        if entry.get('exit_code') != 0 or not before <= entry.get('started_epoch', 0) <= entry.get('finished_epoch', 0) <= after or not isinstance(entry.get('command'), list) or not entry['command']:
            raise ValueError('phase2 native execution failed or is stale')
    artifacts = result.get('artifacts')
    if not isinstance(artifacts, dict) or not artifacts:
        raise ValueError('phase2 raw artifacts missing')
    required_artifacts = {'registry/result.json', 'journeys/result.json', 'journeys/browser-trace.zip', 'journeys-persisted.json', 'correlation.json', 'duplicate-delivery.json', 'after-ten-deliveries.json', 'receipt-withheld-reverted/result.json', 'receipt-withheld-reverted/browser-trace.zip', 'concurrent-candle-fills/result.json', 'candle-before.json', 'candle-precommit.json', 'candle-recovered.json'}
    if not required_artifacts <= artifacts.keys():
        raise ValueError('phase2 actual browser/receipt/DB artifacts incomplete')
    for relative, digest in artifacts.items():
        path = Path(relative)
        if path.is_absolute() or any(p in ('', '.', '..') for p in relative.split('/')):
            raise ValueError('unsafe artifact path')
        artifact = root / path
        if artifact.is_symlink() or any(p.is_symlink() for p in artifact.parents if p.is_relative_to(root)) or not artifact.resolve().is_relative_to(root) or not artifact.is_file() or artifact.stat().st_mtime < started_epoch or hashlib.sha256(artifact.read_bytes()).hexdigest() != digest:
            raise ValueError('phase2 artifact absent, stale, escaped or changed')
    return {'phase2_qualified': True, 'production_qualified': False, 'scenarios': len(scenarios)}


def validate_phase3(directory, result, expected_binding, started_epoch):
    import hashlib
    import math
    import re
    import time
    root=Path(directory).resolve()
    if set(expected_binding)!={'baseline','candidate','interface','layout','topology'} or any(not isinstance(v,str) or not re.fullmatch('[0-9a-f]{64}',v) for v in expected_binding.values()):
        raise ValueError('complete phase3 candidate/topology/interface binding required')
    if result.get('schema_version')!=3 or result.get('task')!='3.49' or result.get('binding')!=expected_binding or result.get('phase3_qualified') is not True:
        raise ValueError('phase3 identity or binding differs')
    if any(result.get(k) is not False for k in ('production_qualified','capacity_qualified','disaster_recovery_qualified')):
        raise ValueError('bounded phase3 faults cannot certify final capacity or disaster recovery')
    if not re.fullmatch('kd-[0-9a-f]{24}',result.get('namespace','')) or not re.fullmatch('[0-9a-f-]{36}',result.get('attempt','')):
        raise ValueError('phase3 current lease absent')
    before,after=result.get('started_epoch'),result.get('finished_epoch')
    if type(before) not in (int,float) or type(after) not in (int,float) or not math.isfinite(before) or not math.isfinite(after) or not started_epoch<=before<=after<=time.time()+1:
        raise ValueError('phase3 raw evidence is stale')
    scenarios=result.get('scenarios',[])
    if len(scenarios)!=len(PHASE3_SCENARIOS) or {s.get('id') for s in scenarios}!=PHASE3_SCENARIOS or any(s.get('passed') is not True or type(s.get('checks')) is not int or s['checks']<=0 for s in scenarios):
        raise ValueError('phase3 fault scenario omitted, duplicated or failed')
    consumers=result.get('durable_consumers',[])
    if not consumers or len({v['id'] for v in consumers})!=len(consumers) or {v['schema'] for v in consumers}!={'indexer','stats','candles','pnl','users','chat','metadata','livestream','ranking'}:
        raise ValueError('phase3 durable consumer inventory incomplete')
    browser={'registry','journeys','scanner-baseline','scanner-outage','media-auth-adversarial','webhook-valid','webhook-replay','az-surviving','redis-outage','nats-quorum-loss','object-storage-failure','database-pool-exhaustion','rpc-quarantine','application-recovered'}
    required=browser|{'observe_topology-before-az','observe_topology-during-az','exhaust_database_pool','release_database_pool','introduce_rpc_disagreement','restore_rpc','rollback_rehearsal'}
    for index in range(len(consumers)):
        required|={name+'-'+str(index) for name in ('arm_consumer_commit_ack','redeliver_consumer','observe_consumer','crash_consumer','recover_consumer')}
        required.add('redeliver_consumer-recovered-'+str(index))
    executions=result.get('executions',[])
    if len(executions)!=len(required) or {e.get('name') for e in executions}!=required:
        raise ValueError('actual phase3 fault execution inventory incomplete')
    artifacts=result.get('artifacts',{})
    if not isinstance(artifacts,dict) or not artifacts: raise ValueError('actual raw phase3 artifacts absent')
    for relative,digest in artifacts.items():
        path=Path(relative); artifact=root/path
        if path.is_absolute() or '.private' in path.parts or any(p in ('','.','..') for p in relative.split('/')) or not isinstance(digest,str) or not re.fullmatch('[0-9a-f]{64}',digest):
            raise ValueError('unsafe phase3 artifact path or digest')
        if artifact.is_symlink() or any(p.is_symlink() for p in artifact.parents if p.is_relative_to(root)) or not artifact.resolve().is_relative_to(root) or not artifact.is_file() or artifact.stat().st_mtime<started_epoch or hashlib.sha256(artifact.read_bytes()).hexdigest()!=digest:
            raise ValueError('phase3 raw artifact stale, escaped or changed')
    expected_artifacts={'acknowledged-baseline.json','media-before.json','scanner-after.json','webhook-after.json','webhook-accepted.json','webhook-replayed.json','az-before.json','az-surviving.json','az-recovered.json','rpc-before.json','rpc-blocked.json','rollback_rehearsal.json'}
    for index in range(len(consumers)):
        expected_artifacts|={'consumer-effects-before-'+str(index)+'.json','consumer-effects-after-'+str(index)+'.json'}
    for name in browser:
        expected_artifacts.add(name+'/result.json')
        if name!='registry':expected_artifacts|={name+'/browser-trace.zip',name+'/browser.png'}
    for execution in executions:
        if type(execution.get('exit_code')) is not int or execution['exit_code']!=0 or not before<=execution.get('started_epoch',0)<=execution.get('finished_epoch',0)<=after or not isinstance(execution.get('command'),list) or not execution['command']:
            raise ValueError('phase3 native execution failed or is stale')
        log=execution.get('log')
        if log not in artifacts or artifacts[log]!=execution.get('log_sha256'):raise ValueError('phase3 native log binding missing')
        if execution['name'] not in browser:expected_artifacts.add(execution['name']+'.json')
    if not expected_artifacts<=artifacts.keys():raise ValueError('actual phase3 browser, database or fault artifacts incomplete')
    actions=result.get('resource_actions',[])
    if not actions or any(not re.fullmatch('[0-9a-f]{64}',a.get('container_id','')) or a.get('action') not in ('pause','unpause','stop','start') or not before<=a.get('observed_epoch',0)<=after for a in actions):
        raise ValueError('actual exact-resource fault observations absent')
    outstanding={}
    for index,action in enumerate(actions,1):
        raw='resource-action-'+str(index)+'.json'
        if raw not in artifacts or json.loads((root/raw).read_text())!=action:raise ValueError('actual resource action artifact differs')
        cid=action['container_id']
        if action['action'] in ('pause','stop'):
            if cid in outstanding:raise ValueError('resource was already faulted')
            outstanding[cid]=action['action']
        else:
            if outstanding.pop(cid,None)!={'unpause':'pause','start':'stop'}[action['action']]:raise ValueError('resource recovery has no matching fault')
    if outstanding:raise ValueError('phase3 left isolated resources disrupted')
    def read(relative):return json.loads((root/relative).read_text())
    for execution in executions:
        name=execution['name']
        if name in browser:continue
        report=read(name+'.json')
        if report.get('attempt')!=result['attempt'] or report.get('namespace')!=result['namespace'] or report.get('source_sha256')!=expected_binding['candidate']:
            raise ValueError('native control report belongs to a different candidate/lease')
        native=report.get('raw_artifacts')
        if not isinstance(native,dict) or not native or any(artifacts.get(p)!=d for p,d in native.items()):
            raise ValueError('native control report lacks raw provider observations')
    rollback=read('rollback_rehearsal.json')
    if rollback.get('rollback_denied') is not True or rollback.get('schema_compatible') is not False or rollback.get('registry_compatible') is not False or rollback.get('production_changed') is not False or rollback.get('validator_changed') is not False:
        raise ValueError('incompatible candidate rollback denial missing')
    return {'phase3_qualified':True,'production_qualified':False,'scenarios':len(scenarios)}


def validate_phase4(directory,result,expected_binding,started_utc_ns):
    import hashlib
    import re
    import time
    root=Path(directory).resolve()
    if set(expected_binding)!={'baseline','candidate','interface','layout','topology'} or any(not isinstance(v,str) or not re.fullmatch('[0-9a-f]{64}',v) for v in expected_binding.values()):
        raise ValueError('complete current phase4 bindings required')
    if result.get('schema_version')!=4 or result.get('task')!='4.49' or result.get('binding')!=expected_binding or result.get('scope')!='isolated_component' or result.get('final_gate')!='9.1' or result.get('phase4_isolated_proof') is not True:
        raise ValueError('phase4 isolated capture identity differs')
    if any(result.get(key) is not False for key in ('final_integrated_gate_passed','production_qualified','capacity_qualified','disaster_recovery_qualified','actual_elapsed72h_proven')):
        raise ValueError('bounded isolated source harness cannot certify full72hours or final integrated gate')
    for field in ('started_utc_ns','ended_utc_ns'):
        if not isinstance(result.get(field),str) or not re.fullmatch('[1-9][0-9]{0,29}',result[field]):raise ValueError('authoritative UTC capture timestamps required')
    before,after=int(result['started_utc_ns']),int(result['ended_utc_ns'])
    if not started_utc_ns<=before<after<=time.time_ns():raise ValueError('phase4 actual capture is stale or future')
    expected={'isolated-regressions','durable-replay-calibration','physical-pitr-checkpoint','recovery-journey-timestamps'}
    scenarios=result.get('scenarios',[])
    if len(scenarios)!=len(expected) or {r.get('id') for r in scenarios}!=expected or any(r.get('passed') is not True or type(r.get('checks')) is not int or r['checks']<=0 for r in scenarios):
        raise ValueError('phase4 isolated scenario omitted, failed or duplicated')
    artifacts=result.get('artifacts',{})
    if not isinstance(artifacts,dict) or not artifacts:raise ValueError('phase4 real raw artifacts required')
    for relative,sha in artifacts.items():
        path=root/relative
        if Path(relative).is_absolute() or any(part in ('','.','..','.private') for part in relative.split('/')) or not isinstance(sha,str) or not re.fullmatch('[0-9a-f]{64}',sha):raise ValueError('invalid raw artifact identity')
        if path.is_symlink() or any(parent.is_symlink() for parent in path.parents if parent.is_relative_to(root)) or not path.resolve().is_relative_to(root) or not path.is_file() or path.stat().st_mtime_ns<started_utc_ns or hashlib.sha256(path.read_bytes()).hexdigest()!=sha:
            raise ValueError('phase4 raw artifact absent, stale, changed or escaped')
    required={'generator-calibration.json','workload-ledger.json','root-mix.json','provisioned-budget.json','isolated-regressions/result.json','isolated-regressions/correlation.json','isolated-regressions/journeys/browser-trace.zip','isolated-regressions/duplicate-delivery.json','isolated-regressions/receipt-withheld-reverted/browser-trace.zip','isolated-regressions/candle-before.json','isolated-regressions/candle-precommit.json','isolated-regressions/candle-recovered.json','replay-browser/concurrent-candle-fills/result.json','replay-browser/concurrent-candle-fills/browser-trace.zip','replay-before.json','replay-after.json','replay-calibration.json','archive_rebuild.json','checkpoint-expected.json','capture_checkpoint.json','checkpoint-restored.json','checkpoint-reconciliation.json','physical-base-backup.json','postgres-backup-manifest.json','physical-restoration.json','pitr-target.json','recovery_fence.json','recovery_login_read_write.json','recovery-timestamps.json'}
    if not required<=artifacts.keys():raise ValueError('phase4 actual regression, replay or PITR artifact gap')
    def read(name):return json.loads((root/name).read_text())
    regressions=read('isolated-regressions/result.json')
    if regressions.get('phase2_qualified') is not True or regressions.get('production_qualified') is not False:raise ValueError('actual isolated regression run failed')
    phase2_started=regressions.get('started_epoch')
    validate_phase2(root/'isolated-regressions',regressions,regressions.get('binding',{}),phase2_started)
    invocations=result.get('provider_invocations',[])
    if not invocations or not {'psql','pg_basebackup','pg_verifybackup','pg_controldata','pg_ctl'}<={v.get('tool') for v in invocations}:raise ValueError('actual physical PostgreSQL provider invocations incomplete')
    for invocation in invocations:
        if type(invocation.get('exit_code')) is not int or invocation['exit_code']!=0 or not before<=int(invocation['started_utc_ns'])<=int(invocation['ended_utc_ns'])<=after or not re.fullmatch('[0-9a-f]{64}',invocation.get('tool_sha256','')) or artifacts.get(invocation.get('raw_output_path'))!=invocation.get('raw_output_sha256'):
            raise ValueError('actual provider failed, changed or lost raw output')
    for control in ('capture_checkpoint','recovery_fence','recovery_login_read_write','archive_rebuild'):
        report=read(control+'.json')
        if report.get('candidate')!=expected_binding['candidate'] or report.get('attempt')!=result.get('attempt') or report.get('namespace')!=result.get('namespace') or not report.get('raw_artifacts') or any(artifacts.get(path)!=sha for path,sha in report['raw_artifacts'].items()):raise ValueError('current bound native recovery control raw inputs absent')
    timestamps=read('recovery-timestamps.json')
    if timestamps.get('scope')!='isolated_component' or timestamps.get('final_gate')!='9.1' or timestamps.get('final_integrated_recovery_qualified') is not False or timestamps.get('acknowledged_rows_lost')!=0 or int(timestamps['rpo_ns'])>300*10**9 or int(timestamps['rto_ns'])>1800*10**9:
        raise ValueError('isolated RPO/RTO exceeded bounds or promoted final acceptance')
    reconciliation=read('checkpoint-reconciliation.json')
    if reconciliation.get('acknowledged_rows_lost')!=0 or not reconciliation.get('checksums'):raise ValueError('authoritative restored checkpoint mismatch')
    return {'phase4_isolated_proof':True,'final_integrated_gate_passed':False,'capacity_qualified':False}
