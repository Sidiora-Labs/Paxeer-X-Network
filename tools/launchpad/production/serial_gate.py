"""Reviewed serial integration qualification. Never publishes code or task status."""
import hashlib
import importlib.util
import hmac
import json
import os
from pathlib import Path
import re
import shutil
import sqlite3
import stat
import sys
import tempfile

_execution_spec=importlib.util.spec_from_file_location('serial_execution_local',Path(__file__).with_name('serial_execution.py'))
_execution=importlib.util.module_from_spec(_execution_spec)
_execution_spec.loader.exec_module(_execution)

CONTROL_PATHS=frozenset({'spec/production-system/spec.kvx','spec/production-system/execution-plan.json','tools/production/bootstrap/check-registry.json'})

class SerialGateError(ValueError):
    pass

def digest(value):
    return hashlib.sha256(json.dumps(value,sort_keys=True,separators=(',',':'),allow_nan=False).encode()).hexdigest()

def _safe_file(path, digest_expected=None):
    path=Path(path)
    if any(p.is_symlink() for p in [path,*path.parents]) or not path.is_file() or path.stat().st_nlink!=1:
        raise SerialGateError('unsafe or missing artifact')
    data=path.read_bytes()
    if digest_expected and hashlib.sha256(data).hexdigest()!=digest_expected:
        raise SerialGateError('artifact differs from reviewed hash')
    return data

class ReadOnlyIndex:
    """Historical acceptance verification needs no lease or database mutation."""
    def __init__(self,path):
        _safe_file(path)
        self.db=sqlite3.connect(Path(path).resolve().as_uri()+'?mode=ro',uri=True)
    def accepted(self,attempt):
        row=self.db.execute('SELECT record FROM accepted WHERE attempt=?',(attempt,)).fetchone()
        return json.loads(row[0]) if row else None
    def latest_accepted(self,task):
        records=[json.loads(row[0]) for row in self.db.execute('SELECT record FROM accepted')]
        return max((r for r in records if r['task']==task),key=lambda r:r['fence'],default=None)
    def pin_selected(self,selected):
        self.db.execute('BEGIN')
        try:
            rows=[json.loads(r[0]) for r in self.db.execute('SELECT record FROM accepted')]
            result={}
            for task,attempt in selected.items():
                records=[r for r in rows if r['task']==task]
                latest=max(records,key=lambda r:r['fence'],default=None)
                if latest is None or latest.get('attempt')!=attempt:
                    raise SerialGateError('selected acceptance was superseded')
                result[task]=latest
            return digest(result)
        finally:self.db.rollback()
    def close(self): self.db.close()


def reconcile_serial_patch(stage, artifact, reviewed, patches):
    """Reserved integration files may change only as exact root-reviewed entries."""
    if isinstance(reviewed.get('source_policy'),dict) and reviewed['source_policy'].get('version')==4:
        return reconcile_preluded_serial_patch(stage,artifact,reviewed,patches)
    if isinstance(reviewed.get('source_policy'),dict) and reviewed['source_policy'].get('version')==3:
        return reconcile_extended_serial_patch(stage,artifact,reviewed,patches)
    if 'source_policy' in reviewed:
        return reconcile_exact_serial_patch(stage, artifact, reviewed, patches)
    manifest=json.loads(_safe_file(Path(artifact)/'manifest.json',reviewed['manifest_sha256']))
    if set(manifest)!={'version','baseline_digest','entries'} or manifest['version']!=1:
        raise SerialGateError('invalid serial integration manifest')
    before=patches._tree(stage)
    if manifest['baseline_digest']!=digest(before):
        raise SerialGateError('serial patch baseline mismatch')
    entries=manifest['entries']; allowed=reviewed.get('allowed_paths')
    if not isinstance(entries,list) or not entries or not isinstance(allowed,list) or not allowed or len(set(allowed))!=len(allowed):
        raise SerialGateError('empty or duplicate serial integration review')
    seen=set()
    for entry in entries:
        if set(entry)!={'path','before','after'}:
            raise SerialGateError('invalid serial entry')
        path=patches._path(entry['path'])
        if patches.ignored(path) or path in CONTROL_PATHS or path in seen or path not in allowed or not (patches._reserved(path) or path.startswith('tools/production/') or path.startswith('spec/production-system/integration/')):
            raise SerialGateError('unreviewed serial path')
        seen.add(path)
        if entry['before']!=before.get(path) or entry['before']==entry['after']:
            raise SerialGateError('serial change stale or noop')
        target=Path(stage)/path
        after=entry['after']
        if after is None:
            if not target.is_file(): raise SerialGateError('invalid serial deletion')
            target.unlink()
        else:
            if set(after)!={'sha256','mode'} or not re.fullmatch('[a-f0-9]{64}',after['sha256']) or type(after['mode']) is not int or not 0<=after['mode']<=0o777:
                raise SerialGateError('invalid serial content identity')
            data=_safe_file(Path(artifact)/'blobs'/after['sha256'],after['sha256'])
            target.parent.mkdir(parents=True,exist_ok=True)
            target.write_bytes(data);target.chmod(after['mode'])
    if seen!=set(allowed): raise SerialGateError('unused serial review path')
    return manifest



EXACT_SERIAL_OWNERSHIP_SHA256='3d4f3ccd3ba8c1c4cc34d52ea1be1155d30a89003b9c442ce26ec36b04caf01e'
EXACT_SERIAL_POLICY_KIND='reviewed-exact-serial-source'


def exact_serial_paths(reviewed, patches):
    policy=reviewed.get('source_policy')
    if not isinstance(policy,dict) or set(policy)!={'version','kind','task','ownership_artifact','ownership_sha256'}:
        raise SerialGateError('invalid exact serial source policy')
    if type(policy['version']) is not int or policy['version']!=2 or policy['kind']!=EXACT_SERIAL_POLICY_KIND or policy['task']!='1.99':
        raise SerialGateError('unsupported exact serial source policy')
    if policy['ownership_sha256']!=EXACT_SERIAL_OWNERSHIP_SHA256:
        raise SerialGateError('unreviewed exact serial ownership identity')
    ownership=json.loads(_safe_file(policy['ownership_artifact'],EXACT_SERIAL_OWNERSHIP_SHA256))
    if ownership.get('schema_version')!=1 or ownership.get('kind')!='immutable_exact_serial_path_ownership' or ownership.get('task')!='1.99' or ownership.get('path_count')!=336:
        raise SerialGateError('wrong exact serial ownership artifact')
    entries=ownership.get('paths')
    if not isinstance(entries,list) or len(entries)!=336:
        raise SerialGateError('incomplete exact serial ownership')
    paths=set()
    controls=set(ownership.get('root_exclusive_control_paths',[]))|CONTROL_PATHS
    for entry in entries:
        if not isinstance(entry,dict) or set(entry)!={'path','owner'} or not isinstance(entry['owner'],str) or not entry['owner']:
            raise SerialGateError('invalid exact serial ownership entry')
        path=patches._path(entry['path'])
        if any(c in path for c in '*?[') or path in paths or path in controls or patches.ignored(path):
            raise SerialGateError('unsafe exact serial ownership path')
        paths.add(path)
    return frozenset(paths)


EXACT_SERIAL_CONTROL_PATHS=frozenset({'tools/production/serial_gate.py','tools/production/tests/test_serial_gate.py'})


def exact_serial_controls(policy,stage,root,patches):
    if policy.get('task')!='1.99':raise SerialGateError('exact source policy restricted to 1.99')
    reviewed=policy['control_plane'].get('serial_source_controls')
    if not isinstance(reviewed,dict) or set(reviewed)!=EXACT_SERIAL_CONTROL_PATHS:
        raise SerialGateError('exact runner control identities required')
    staged=patches._tree(stage);canonical=patches._tree(root)
    for path in sorted(EXACT_SERIAL_CONTROL_PATHS):
        identity=reviewed[path]
        if not isinstance(identity,dict) or set(identity)!={'before','after'}:
            raise SerialGateError('invalid runner control identity')
        for name in ('before','after'):
            value=identity[name]
            if value is None and name=='before':continue
            if not isinstance(value,dict) or set(value)!={'sha256','mode'} or not isinstance(value['sha256'],str) or not re.fullmatch('[a-f0-9]{64}',value['sha256']) or type(value['mode']) is not int or not 0<=value['mode']<=0o777:
                raise SerialGateError('invalid runner control content identity')
        if identity['before']!=staged.get(path) or identity['after']!=canonical.get(path) or identity['before']==identity['after']:
            raise SerialGateError('runner control source differs from exact review')
    return CONTROL_PATHS|EXACT_SERIAL_CONTROL_PATHS


def reconcile_exact_serial_patch(stage, artifact, reviewed, patches):
    paths=exact_serial_paths(reviewed,patches)
    policy=reviewed['source_policy']
    manifest=json.loads(_safe_file(Path(artifact)/'manifest.json',reviewed['manifest_sha256']))
    if set(manifest)!={'version','baseline_digest','source_policy_sha256','entries'} or type(manifest['version']) is not int or manifest['version']!=2 or manifest['source_policy_sha256']!=digest(policy):
        raise SerialGateError('invalid exact serial integration manifest')
    before=patches._tree(stage)
    if manifest['baseline_digest']!=digest(before):
        raise SerialGateError('serial patch baseline mismatch')
    entries=manifest['entries'];allowed=reviewed.get('allowed_paths')
    if not isinstance(entries,list) or not entries or not isinstance(allowed,list) or not allowed or any(not isinstance(path,str) for path in allowed) or len(set(allowed))!=len(allowed):
        raise SerialGateError('empty or duplicate serial integration review')
    if any(patches._path(path) not in paths for path in allowed):
        raise SerialGateError('unreviewed exact serial path')
    seen=set();changes=[]
    for entry in entries:
        if not isinstance(entry,dict) or set(entry)!={'path','before','after'}:
            raise SerialGateError('invalid serial entry')
        path=patches._path(entry['path'])
        if patches.ignored(path) or path in CONTROL_PATHS or path in seen or path not in allowed or path not in paths:
            raise SerialGateError('unreviewed serial path')
        seen.add(path)
        if entry['before']!=before.get(path) or entry['before']==entry['after']:
            raise SerialGateError('serial change stale or noop')
        target=Path(stage)/path
        patches._ancestors(target)
        after=entry['after']
        if after is None:
            if not target.is_file():raise SerialGateError('invalid serial deletion')
            changes.append((target,None,None))
        else:
            if not isinstance(after,dict) or set(after)!={'sha256','mode'} or not isinstance(after['sha256'],str) or not re.fullmatch('[a-f0-9]{64}',after['sha256']) or type(after['mode']) is not int or not 0<=after['mode']<=0o777:
                raise SerialGateError('invalid serial content identity')
            data=_safe_file(Path(artifact)/'blobs'/after['sha256'],after['sha256'])
            changes.append((target,data,after['mode']))
    if seen!=set(allowed):raise SerialGateError('unused serial review path')
    for target,data,mode in changes:
        if data is None:target.unlink()
        else:
            target.parent.mkdir(parents=True,exist_ok=True)
            target.write_bytes(data);target.chmod(mode)
    return manifest



EXACT_SERIAL_EXTENSION_SHA256='5c6da6f3872b7b129f35a11e6ac80c1ce4da158ed834365fab5c8218802f8cb3'
EXACT_SERIAL_EXTENSION_KIND='reviewed-exact-serial-source-extension'
EXACT_SERIAL_EXTENSION_PATHS=frozenset({'media/user/internal/user/handlers_test.go','spec/production-system/integration/migrations/330001.sql','client/packages/sdk/package.json','client/apps/web/package.json','client/apps/mobile/package.json'})


def exact_serial_extension_paths(reviewed,patches):
    policy=reviewed.get('source_policy')
    keys={'version','kind','task','ownership_artifact','ownership_sha256','extension_artifact','extension_sha256'}
    if not isinstance(policy,dict) or set(policy)!=keys or type(policy['version']) is not int or policy['version']!=3 or policy['kind']!=EXACT_SERIAL_EXTENSION_KIND or policy['task']!='1.99':
        raise SerialGateError('unsupported exact serial extension policy')
    base={'version':2,'kind':EXACT_SERIAL_POLICY_KIND,'task':'1.99','ownership_artifact':policy['ownership_artifact'],'ownership_sha256':policy['ownership_sha256']}
    paths=exact_serial_paths({'source_policy':base},patches)
    if policy['extension_sha256']!=EXACT_SERIAL_EXTENSION_SHA256:
        raise SerialGateError('unreviewed exact serial extension identity')
    extension=json.loads(_safe_file(policy['extension_artifact'],EXACT_SERIAL_EXTENSION_SHA256))
    if set(extension)!={'schema_version','kind','task','base_ownership_sha256','path_count','entries'} or extension['schema_version']!=1 or extension['kind']!='immutable_exact_serial_source_extensions' or extension['task']!='1.99' or extension['base_ownership_sha256']!=EXACT_SERIAL_OWNERSHIP_SHA256 or extension['path_count']!=5 or not isinstance(extension['entries'],list) or len(extension['entries'])!=5:
        raise SerialGateError('invalid exact serial extension artifact')
    entries={}
    for entry in extension['entries']:
        if not isinstance(entry,dict) or set(entry)!={'path','owner','before','after'} or not isinstance(entry['owner'],str) or not entry['owner']:
            raise SerialGateError('invalid exact serial extension entry')
        path=patches._path(entry['path'])
        if path not in EXACT_SERIAL_EXTENSION_PATHS or path in entries or path in paths or path in CONTROL_PATHS or path in EXACT_SERIAL_CONTROL_PATHS or patches.ignored(path):
            raise SerialGateError('unreviewed exact serial extension path')
        for name in ('before','after'):
            identity=entry[name]
            if identity is None and name=='before':continue
            if not isinstance(identity,dict) or set(identity)!={'sha256','mode'} or not isinstance(identity['sha256'],str) or not re.fullmatch('[a-f0-9]{64}',identity['sha256']) or type(identity['mode']) is not int or not 0<=identity['mode']<=0o777:
                raise SerialGateError('invalid exact serial extension content identity')
        if entry['before']==entry['after']:raise SerialGateError('noop exact serial extension')
        entries[path]=entry
    if set(entries)!=EXACT_SERIAL_EXTENSION_PATHS:raise SerialGateError('incomplete exact serial extension')
    return paths|EXACT_SERIAL_EXTENSION_PATHS,entries


def reconcile_extended_serial_patch(stage, artifact, reviewed, patches):
    paths,extensions=exact_serial_extension_paths(reviewed,patches)
    policy=reviewed['source_policy']
    manifest=json.loads(_safe_file(Path(artifact)/'manifest.json',reviewed['manifest_sha256']))
    if set(manifest)!={'version','baseline_digest','source_policy_sha256','entries'} or type(manifest['version']) is not int or manifest['version']!=3 or manifest['source_policy_sha256']!=digest(policy):
        raise SerialGateError('invalid exact serial integration manifest')
    before=patches._tree(stage)
    if manifest['baseline_digest']!=digest(before):
        raise SerialGateError('serial patch baseline mismatch')
    entries=manifest['entries'];allowed=reviewed.get('allowed_paths')
    if not isinstance(entries,list) or not entries or not isinstance(allowed,list) or not allowed or any(not isinstance(path,str) for path in allowed) or len(set(allowed))!=len(allowed):
        raise SerialGateError('empty or duplicate serial integration review')
    if any(patches._path(path) not in paths for path in allowed):
        raise SerialGateError('unreviewed exact serial path')
    seen=set();changes=[]
    for entry in entries:
        if not isinstance(entry,dict) or set(entry)!={'path','before','after'}:
            raise SerialGateError('invalid serial entry')
        path=patches._path(entry['path'])
        if patches.ignored(path) or path in CONTROL_PATHS or path in seen or path not in allowed or path not in paths:
            raise SerialGateError('unreviewed serial path')
        seen.add(path)
        if path in extensions and any(entry[name]!=extensions[path][name] for name in ('before','after')):
            raise SerialGateError('extension source identity differs from exact review')
        if entry['before']!=before.get(path) or entry['before']==entry['after']:
            raise SerialGateError('serial change stale or noop')
        target=Path(stage)/path
        patches._ancestors(target)
        after=entry['after']
        if after is None:
            if not target.is_file():raise SerialGateError('invalid serial deletion')
            changes.append((target,None,None))
        else:
            if not isinstance(after,dict) or set(after)!={'sha256','mode'} or not isinstance(after['sha256'],str) or not re.fullmatch('[a-f0-9]{64}',after['sha256']) or type(after['mode']) is not int or not 0<=after['mode']<=0o777:
                raise SerialGateError('invalid serial content identity')
            data=_safe_file(Path(artifact)/'blobs'/after['sha256'],after['sha256'])
            changes.append((target,data,after['mode']))
    if seen!=set(allowed):raise SerialGateError('unused serial review path')
    if not set(extensions).issubset(seen):raise SerialGateError('required extension source change missing')
    for target,data,mode in changes:
        if data is None:target.unlink()
        else:
            target.parent.mkdir(parents=True,exist_ok=True)
            target.write_bytes(data);target.chmod(mode)
    return manifest



PRELUDED_SERIAL_OWNERSHIP_SHA256='9f4fc356c8d3dd5ecf24d2024daee7280679c49e8fc4a50e118b1e6db9a2d4ad'
PRELUDED_SERIAL_POLICY_KIND='reviewed-exact-serial-source-prelude'
MANDATORY_PRELUDED_STAGE_CHECKS_SHA256='a0deb169d8a0e333eecef56a7a5ff62fa6ac576c6264e9bc63ec81afde40d8a6'
APPROVED_STAGE_PRELUDE={'path':'media/user/internal/user/handlers_test.go','before':{'sha256':'baf05d3a0149c0b0aebf2bf87d96ef3937c473fd074f1878c94b54c4a098b133','mode':0o644},'after':{'sha256':'7c8f6789c043693bccf91d8d9a8bddc065546c27bc3fc1a42a7b98f3b5ffd4e4','mode':0o644},'blob_sha256':'7c8f6789c043693bccf91d8d9a8bddc065546c27bc3fc1a42a7b98f3b5ffd4e4'}


def exact_serial_prelude_paths(reviewed,patches):
    policy=reviewed.get('source_policy')
    if not isinstance(policy,dict) or set(policy)!={'version','kind','task','ownership_artifact','ownership_sha256'} or type(policy['version']) is not int or policy['version']!=4 or policy['kind']!=PRELUDED_SERIAL_POLICY_KIND or policy['task']!='1.99':
        raise SerialGateError('unsupported exact serial prelude policy')
    if policy['ownership_sha256']!=PRELUDED_SERIAL_OWNERSHIP_SHA256:
        raise SerialGateError('unreviewed exact serial prelude ownership')
    ownership=json.loads(_safe_file(policy['ownership_artifact'],PRELUDED_SERIAL_OWNERSHIP_SHA256))
    if set(ownership)!={'schema_version','kind','task','path_count','paths','root_exclusive_control_paths','base_ownership_sha256','stage_prelude'} or ownership['schema_version']!=1 or ownership['kind']!='immutable_exact_serial_path_ownership' or ownership['task']!='1.99' or ownership['path_count']!=388 or ownership['base_ownership_sha256']!=EXACT_SERIAL_OWNERSHIP_SHA256 or ownership['stage_prelude']!=APPROVED_STAGE_PRELUDE or set(ownership['root_exclusive_control_paths'])!=EXACT_SERIAL_CONTROL_PATHS:
        raise SerialGateError('invalid exact serial prelude ownership artifact')
    entries=ownership['paths']
    if not isinstance(entries,list) or len(entries)!=388:raise SerialGateError('incomplete exact serial prelude ownership')
    paths=set()
    for entry in entries:
        if not isinstance(entry,dict) or set(entry)!={'path','owner'} or not isinstance(entry['owner'],str) or not entry['owner']:
            raise SerialGateError('invalid exact serial prelude ownership entry')
        path=patches._path(entry['path'])
        if any(c in path for c in '*?') or ('[' in path and path!='client/apps/web/src/app/tokens/[tokenAddress]/metadata/page.tsx') or path in paths or path in CONTROL_PATHS or path in EXACT_SERIAL_CONTROL_PATHS or patches.ignored(path):
            raise SerialGateError('unsafe exact serial prelude ownership path')
        paths.add(path)
    if APPROVED_STAGE_PRELUDE['path'] not in paths:raise SerialGateError('approved prelude ownership missing')
    return frozenset(paths),ownership['stage_prelude']


def apply_approved_stage_prelude(stage,reviewed,patches):
    _,prelude=exact_serial_prelude_paths(reviewed,patches)
    before=patches._tree(stage)
    if before.get(prelude['path'])!=prelude['before']:raise SerialGateError('approved stage prelude source differs from exact review')
    data=_safe_file(Path(reviewed['artifact_directory'])/'blobs'/prelude['blob_sha256'],prelude['blob_sha256'])
    target=Path(stage)/prelude['path'];patches._ancestors(target)
    target.write_bytes(data);target.chmod(prelude['after']['mode'])
    expected=dict(before);expected[prelude['path']]=prelude['after']
    if patches._tree(stage)!=expected:raise SerialGateError('approved stage prelude changed unexplained source')
    return prelude


def mandatory_preluded_stage_checks(policy,registry,selected):
    if policy.get('staged_checks_sha256')!=MANDATORY_PRELUDED_STAGE_CHECKS_SHA256:
        raise SerialGateError('unreviewed mandatory stage checks')
    checks=json.loads(_safe_file(policy['staged_checks_artifact'],MANDATORY_PRELUDED_STAGE_CHECKS_SHA256))
    if checks!=policy.get('staged_checks') or not isinstance(checks,list) or len(checks)!=70:
        raise SerialGateError('mandatory complete-wave stage checks differ')
    indexed={}
    for check in checks:
        if not isinstance(check,dict) or not isinstance(check.get('id'),str) or check['id'] in indexed or check.get('skip') or check.get('skipped'):
            raise SerialGateError('invalid or omitted mandatory stage check')
        indexed[check['id']]=check
    registered=[]
    for task in sorted(selected):
        lane=registry.get(task)
        if not isinstance(lane,list) or not lane:raise SerialGateError('registered native lane checks missing')
        registered.extend(lane)
    if len(registered)!=68 or len({check.get('id') for check in registered})!=68 or any(indexed.get(check.get('id'))!=check for check in registered):
        raise SerialGateError('mandatory stage check differs from authentic lane registry')
    return checks


def reconcile_preluded_serial_patch(stage, artifact, reviewed, patches):
    paths,prelude=exact_serial_prelude_paths(reviewed,patches)
    policy=reviewed['source_policy']
    manifest=json.loads(_safe_file(Path(artifact)/'manifest.json',reviewed['manifest_sha256']))
    if set(manifest)!={'version','baseline_digest','source_policy_sha256','entries'} or type(manifest['version']) is not int or manifest['version']!=4 or manifest['source_policy_sha256']!=digest(policy):
        raise SerialGateError('invalid exact serial integration manifest')
    before=patches._tree(stage)
    if manifest['baseline_digest']!=digest(before):
        raise SerialGateError('serial patch baseline mismatch')
    if before.get(prelude['path'])!=prelude['after']:raise SerialGateError('approved prelude not present in serial baseline')
    entries=manifest['entries'];allowed=reviewed.get('allowed_paths')
    if not isinstance(entries,list) or not entries or not isinstance(allowed,list) or not allowed or any(not isinstance(path,str) for path in allowed) or len(set(allowed))!=len(allowed):
        raise SerialGateError('empty or duplicate serial integration review')
    if any(patches._path(path) not in paths for path in allowed):
        raise SerialGateError('unreviewed exact serial path')
    seen=set();changes=[]
    for entry in entries:
        if not isinstance(entry,dict) or set(entry)!={'path','before','after'}:
            raise SerialGateError('invalid serial entry')
        path=patches._path(entry['path'])
        if patches.ignored(path) or path in CONTROL_PATHS or path in seen or path not in allowed or path not in paths:
            raise SerialGateError('unreviewed serial path')
        seen.add(path)
        if path==prelude['path']:raise SerialGateError('approved prelude cannot change again in serial patch')
        if entry['before']!=before.get(path) or entry['before']==entry['after']:
            raise SerialGateError('serial change stale or noop')
        target=Path(stage)/path
        patches._ancestors(target)
        after=entry['after']
        if after is None:
            if not target.is_file():raise SerialGateError('invalid serial deletion')
            changes.append((target,None,None))
        else:
            if not isinstance(after,dict) or set(after)!={'sha256','mode'} or not isinstance(after['sha256'],str) or not re.fullmatch('[a-f0-9]{64}',after['sha256']) or type(after['mode']) is not int or not 0<=after['mode']<=0o777:
                raise SerialGateError('invalid serial content identity')
            data=_safe_file(Path(artifact)/'blobs'/after['sha256'],after['sha256'])
            changes.append((target,data,after['mode']))
    if seen!=set(allowed):raise SerialGateError('unused serial review path')
    for target,data,mode in changes:
        if data is None:target.unlink()
        else:
            target.parent.mkdir(parents=True,exist_ok=True)
            target.write_bytes(data);target.chmod(mode)
    return manifest



def qualify_serial_gate(entry, task_id, task, spec, root, output, control=None):
    candidate_policy=entry.get('serial_gate_policy')
    if isinstance(candidate_policy,dict):
        serial=candidate_policy.get('serial_integration')
        if isinstance(serial,dict) and isinstance(serial.get('source_policy'),dict) and serial['source_policy'].get('version')==4:
            return qualify_serial_gate_v4(entry,task_id,task,spec,root,output,control)
    root=Path(root).resolve();output=Path(output).resolve()
    if output==root or root in output.parents:
        # Canonical evidence is normally beneath ignored evidence; source policy
        # permits that boundary, but this module requires externally owned output.
        if not output.is_relative_to(root/'evidence'):
            raise SerialGateError('qualification output must be external or evidence-only')
    policy=entry.get('serial_gate_policy')
    if not isinstance(policy,dict) or entry.get('policy_sha256')!=digest(policy):
        raise SerialGateError('missing reviewed serial gate policy')
    if not re.fullmatch('[1-4]\\.99',task_id) or task.get('kind')!='serial_gate' or policy.get('task')!=task_id:
        raise SerialGateError('unsupported serial gate')
    for name in ('input_interface_sha256','output_interface_sha256'):
        if not re.fullmatch('[a-f0-9]{64}',str(policy.get(name,''))):raise SerialGateError('explicit input and output interface digests required')
    if control is not None and control.cancelled.is_set():raise SerialGateError('serial qualification cancelled before reconstruction')
    phase=int(task_id[0]);wave=2*phase-1
    if task.get('wave')!=wave+1 or policy.get('wave')!=wave:
        raise SerialGateError('wrong serial wave')
    sys.path.insert(0,str(root/'tools/production'))
    try:
        from dispatcher import acceptance,evidence,patches
        from dispatcher.graph import Graph
    finally: sys.path.pop(0)
    graph=Graph(root/'spec/production-system/spec.kvx',root/'spec/production-system/execution-plan.json')
    if graph.tasks.get(task_id)!=task:
        raise SerialGateError('caller task differs from canonical graph')
    controls=policy.get('control_plane')
    if not isinstance(controls,dict): raise SerialGateError('exact control plane review required')
    _safe_file(root/'spec/production-system/spec.kvx',controls.get('spec_sha256'))
    _safe_file(root/'spec/production-system/execution-plan.json',controls.get('plan_sha256'))
    registry_control=json.loads(_safe_file(root/'tools/production/bootstrap/check-registry.json'))
    registry_control.get('tasks',{}).pop(task_id,None)
    if not re.fullmatch('[a-f0-9]{64}',str(controls.get('spec_sha256',''))) or not re.fullmatch('[a-f0-9]{64}',str(controls.get('plan_sha256',''))) or digest(registry_control)!=controls.get('registry_without_self_sha256'):
        raise SerialGateError('control plane differs from exact reviewed source')
    expected={k:v for k,v in graph.tasks.items() if v.get('kind')=='parallel_lane' and v.get('wave')==wave}
    if len(expected)!=50 or set(task.get('requires',[]))!=set(expected) or any(v['status']!='done' for v in expected.values()):
        raise SerialGateError('requires exact fifty done canonical lane tasks')
    selected=policy.get('accepted_ids')
    if not isinstance(selected,dict) or set(selected)!=set(expected) or any(not isinstance(v,str) or not re.fullmatch('[A-Za-z0-9][A-Za-z0-9_.-]*',v) for v in selected.values()) or len(set(selected.values()))!=50:
        raise SerialGateError('requires exact fifty distinct selected acceptances')
    state=Path(policy['state_directory'])
    if not state.is_absolute() or state.resolve().is_relative_to(root): raise SerialGateError('state must be external')
    patches._ancestors(state)
    if stat.S_IMODE(state.stat().st_mode)!=0o700 or state.stat().st_uid!=os.getuid():
        raise SerialGateError('private coordinator state required')
    key_path=state/'coordinator.key'
    if stat.S_IMODE(key_path.stat().st_mode)!=0o600 or key_path.stat().st_uid!=os.getuid(): raise SerialGateError('private signing key required')
    secret=_safe_file(key_path)
    config=json.loads(_safe_file(state/'config.json',policy['config_sha256']))
    if config.get('fixture') or config.get('layout')!=graph.layout_digest or config.get('interface')!=policy['input_interface_sha256']:
        raise SerialGateError('wrong coordinator interface/layout or fixture')
    registry_path=Path(config['registry'])
    if not registry_path.resolve().is_relative_to(state.resolve()): raise SerialGateError('untrusted lane registry')
    registry=json.loads(_safe_file(registry_path,policy['lane_registry_sha256']))
    baseline=Path(config['baseline'])
    if evidence.source_digest(baseline)!=policy['baseline_sha256']: raise SerialGateError('baseline changed')
    for name in ('input_interface_sha256','output_interface_sha256'):
        if not re.fullmatch('[a-f0-9]{64}',str(policy.get(name,''))):raise SerialGateError('explicit input and output interface digests required')
    if entry.get('interface_bundle',{}).get('expected_sha256')!=policy['output_interface_sha256']:
        raise SerialGateError('output interface differs from current reviewed bundle')
    checks=policy.get('combined_checks')
    staged_checks=policy.get('staged_checks',checks)
    if not isinstance(checks,list) or not checks or any(not c.get('proof') for c in checks): raise SerialGateError('native combined checks required')
    output.mkdir(parents=True,exist_ok=True)
    stage=output/'reconstructed-wave'
    if stage.exists(): raise SerialGateError('fresh qualification output required')
    before=evidence.source_digest(root)
    index=ReadOnlyIndex(config['db'])
    try:
        index_pin=index.pin_selected(selected)
        for identifier,attempt in selected.items():
            body=acceptance._signed_load(state/'accepted'/attempt/'accepted.json',secret)
            if body.get('task')!=identifier or body.get('token',{}).get('attempt')!=attempt or body.get('token',{}).get('lane')!=expected[identifier]['lane'] or any(body.get('bindings',{}).get(k)!=v for k,v in {'baseline':policy['baseline_sha256'],'interface':policy['input_interface_sha256'],'layout':graph.layout_digest}.items()):
                raise SerialGateError('accepted lane identity or frozen binding mismatch')
        stage_result=acceptance.stage_accepted(baseline,[state/'accepted'/selected[k] for k in sorted(expected)],
            {k:v['touches'] for k,v in expected.items()},registry,secret,stage,verify=lambda _:True,store=index)
        # Signature authenticates actual previous complete-wave native execution;
        # reconstruction and exact selected accepted IDs establish completeness.
        staged_digest=evidence.source_digest(stage)
        prior=json.loads(_safe_file(policy['stage_evidence_path'],policy['stage_evidence_sha256']))
        raw=dict(prior);signature=raw.pop('signature',None)
        if not isinstance(signature,str) or not hmac.compare_digest(signature,acceptance._sign(raw,secret)):
            raise SerialGateError('invalid staged proof signature')
        binding={'baseline':policy['baseline_sha256'],'candidate':staged_digest,'interface':policy['input_interface_sha256'],'layout':graph.layout_digest}
        if prior.get('token',{}).get('coordinator_integration') is not True or prior.get('bindings')!=binding or prior.get('source_digest')!=staged_digest or prior.get('source_before')!=staged_digest:
            raise SerialGateError('staged proof not bound to complete selected wave')
        # Reuse the same historical proof validation as durable lane acceptance.
        trusted_staged_checks=_execution.historical_checks(prior,staged_checks)
        historical={'evidence':prior,'bindings':binding,'token':prior['token'],'checks':trusted_staged_checks}
        acceptance._historical(historical,secret,stage,Path(policy['stage_outputs_directory']),trusted_staged_checks)
        serial=policy.get('serial_integration')
        if not isinstance(serial,dict): raise SerialGateError('reviewed serial integration patch required')
        control_paths=CONTROL_PATHS
        if 'source_policy' in serial:
            control_paths=exact_serial_controls(policy,stage,root,patches)
        reconcile_serial_patch(stage,serial['artifact_directory'],serial,patches)
        reconciled={k:v for k,v in patches._tree(stage).items() if k not in control_paths}
        canonical={k:v for k,v in patches._tree(root).items() if k not in control_paths}
        if reconciled!=canonical:
            raise SerialGateError('canonical source contains missing or unexplained changes')
        canonical_binding={**binding,'candidate':before,'interface':policy['output_interface_sha256']}
        # Fresh native execution uses current canonical code, never an old receipt.
        with tempfile.TemporaryDirectory(prefix='serial-native-checks-') as native_output:
            execution=_execution.run_checks(checks,root,Path(native_output)/'checks',canonical_binding,
                                 {'serial_gate':task_id,'bindings':canonical_binding},secret,evidence,control)
            shutil.copytree(Path(native_output)/'checks',output/'canonical-checks')
        if evidence.source_digest(root)!=before or {k:v for k,v in patches._tree(stage).items() if k not in control_paths}!=canonical:
            raise SerialGateError('source changed during serial qualification')
        _safe_file(state/'config.json',policy['config_sha256'])
        _safe_file(registry_path,policy['lane_registry_sha256'])
        if _safe_file(key_path)!=secret or evidence.source_digest(baseline)!=policy['baseline_sha256']:
            raise SerialGateError('private coordinator bindings changed during qualification')
        if index.pin_selected(selected)!=index_pin:
            raise SerialGateError('authoritative acceptance index changed during serial checks')
        if control is not None and control.cancelled.is_set():raise SerialGateError('serial qualification cancelled')
        return {'qualified':True,'qualification_scope':'integrated-current-source','canonical_integrated':True,
                'input_interface_sha256':policy['input_interface_sha256'],'output_interface_sha256':policy['output_interface_sha256'],'accepted_index_sha256':index_pin,
                'task':task_id,'accepted_tasks':sorted(expected),'accepted_ids':selected,
                'canonical_sha256':before,'reconciled_product_sha256':digest(canonical),'control_paths':sorted(control_paths),'staged_sha256':staged_digest,'policy_sha256':entry['policy_sha256'],
                'checks':[{'id':r['id'],'passed':True,'native_proof':r['proof']} for r in execution['receipts']],
                'serial_manifest_sha256':serial['manifest_sha256'],'stage_evidence_sha256':policy['stage_evidence_sha256']}
    finally:
        index.close()
        if stage.exists(): shutil.rmtree(stage)


class RetainedPreludeEvidence:
    def __init__(self,module,directory):
        self.module=module;self.directory=Path(directory);self.ordinal=0
    def __getattr__(self,name):return getattr(self.module,name)
    def _proof(self,receipt,policy):
        self.directory.mkdir(parents=True,exist_ok=True)
        fields=('id','command','exit_code','timed_out','stdout','stderr','duration','pid','cleanup')
        observed={name:receipt[name] for name in fields if name in receipt}
        target=self.directory/(str(self.ordinal).zfill(4)+'.json');self.ordinal+=1
        if target.exists() or target.is_symlink():raise SerialGateError('native diagnostic namespace reused')
        target.write_bytes(self.module._json(observed)+b'\n')
        return self.module._proof(receipt,policy)


def qualify_serial_gate_v4(entry, task_id, task, spec, root, output, control=None):
    root=Path(root).resolve();output=Path(output).resolve()
    if output==root or root in output.parents:
        # Canonical evidence is normally beneath ignored evidence; source policy
        # permits that boundary, but this module requires externally owned output.
        if not output.is_relative_to(root/'evidence'):
            raise SerialGateError('qualification output must be external or evidence-only')
    policy=entry.get('serial_gate_policy')
    if not isinstance(policy,dict) or entry.get('policy_sha256')!=digest(policy):
        raise SerialGateError('missing reviewed serial gate policy')
    if not re.fullmatch(r'[1-4]\.99',task_id) or task.get('kind')!='serial_gate' or policy.get('task')!=task_id:
        raise SerialGateError('unsupported serial gate')
    for name in ('input_interface_sha256','output_interface_sha256'):
        if not re.fullmatch('[a-f0-9]{64}',str(policy.get(name,''))):raise SerialGateError('explicit input and output interface digests required')
    if control is not None and control.cancelled.is_set():raise SerialGateError('serial qualification cancelled before reconstruction')
    phase=int(task_id[0]);wave=2*phase-1
    if task.get('wave')!=wave+1 or policy.get('wave')!=wave:
        raise SerialGateError('wrong serial wave')
    sys.path.insert(0,str(root/'tools/production'))
    try:
        from dispatcher import acceptance,evidence,patches
        from dispatcher.graph import Graph
    finally: sys.path.pop(0)
    graph=Graph(root/'spec/production-system/spec.kvx',root/'spec/production-system/execution-plan.json')
    if graph.tasks.get(task_id)!=task:
        raise SerialGateError('caller task differs from canonical graph')
    controls=policy.get('control_plane')
    if not isinstance(controls,dict): raise SerialGateError('exact control plane review required')
    _safe_file(root/'spec/production-system/spec.kvx',controls.get('spec_sha256'))
    _safe_file(root/'spec/production-system/execution-plan.json',controls.get('plan_sha256'))
    registry_control=json.loads(_safe_file(root/'tools/production/bootstrap/check-registry.json'))
    registry_control.get('tasks',{}).pop(task_id,None)
    if not re.fullmatch('[a-f0-9]{64}',str(controls.get('spec_sha256',''))) or not re.fullmatch('[a-f0-9]{64}',str(controls.get('plan_sha256',''))) or digest(registry_control)!=controls.get('registry_without_self_sha256'):
        raise SerialGateError('control plane differs from exact reviewed source')
    expected={k:v for k,v in graph.tasks.items() if v.get('kind')=='parallel_lane' and v.get('wave')==wave}
    if len(expected)!=50 or set(task.get('requires',[]))!=set(expected) or any(v['status']!='done' for v in expected.values()):
        raise SerialGateError('requires exact fifty done canonical lane tasks')
    selected=policy.get('accepted_ids')
    if not isinstance(selected,dict) or set(selected)!=set(expected) or any(not isinstance(v,str) or not re.fullmatch('[A-Za-z0-9][A-Za-z0-9_.-]*',v) for v in selected.values()) or len(set(selected.values()))!=50:
        raise SerialGateError('requires exact fifty distinct selected acceptances')
    state=Path(policy['state_directory'])
    if not state.is_absolute() or state.resolve().is_relative_to(root): raise SerialGateError('state must be external')
    patches._ancestors(state)
    if stat.S_IMODE(state.stat().st_mode)!=0o700 or state.stat().st_uid!=os.getuid():
        raise SerialGateError('private coordinator state required')
    key_path=state/'coordinator.key'
    if stat.S_IMODE(key_path.stat().st_mode)!=0o600 or key_path.stat().st_uid!=os.getuid(): raise SerialGateError('private signing key required')
    secret=_safe_file(key_path)
    config=json.loads(_safe_file(state/'config.json',policy['config_sha256']))
    if config.get('fixture') or config.get('layout')!=graph.layout_digest or config.get('interface')!=policy['input_interface_sha256']:
        raise SerialGateError('wrong coordinator interface/layout or fixture')
    registry_path=Path(config['registry'])
    if not registry_path.resolve().is_relative_to(state.resolve()): raise SerialGateError('untrusted lane registry')
    registry=json.loads(_safe_file(registry_path,policy['lane_registry_sha256']))
    baseline=Path(config['baseline'])
    if evidence.source_digest(baseline)!=policy['baseline_sha256']: raise SerialGateError('baseline changed')
    for name in ('input_interface_sha256','output_interface_sha256'):
        if not re.fullmatch('[a-f0-9]{64}',str(policy.get(name,''))):raise SerialGateError('explicit input and output interface digests required')
    if entry.get('interface_bundle',{}).get('expected_sha256')!=policy['output_interface_sha256']:
        raise SerialGateError('output interface differs from current reviewed bundle')
    checks=policy.get('combined_checks')
    staged_checks=mandatory_preluded_stage_checks(policy,registry,selected)
    if not isinstance(checks,list) or not checks or any(not c.get('proof') for c in checks): raise SerialGateError('native combined checks required')
    output.mkdir(parents=True,exist_ok=True)
    stage=output/'reconstructed-wave'
    if stage.exists(): raise SerialGateError('fresh qualification output required')
    before=evidence.source_digest(root)
    index=ReadOnlyIndex(config['db'])
    try:
        index_pin=index.pin_selected(selected)
        for identifier,attempt in selected.items():
            body=acceptance._signed_load(state/'accepted'/attempt/'accepted.json',secret)
            if body.get('task')!=identifier or body.get('token',{}).get('attempt')!=attempt or body.get('token',{}).get('lane')!=expected[identifier]['lane'] or any(body.get('bindings',{}).get(k)!=v for k,v in {'baseline':policy['baseline_sha256'],'interface':policy['input_interface_sha256'],'layout':graph.layout_digest}.items()):
                raise SerialGateError('accepted lane identity or frozen binding mismatch')
        stage_result=acceptance.stage_accepted(baseline,[state/'accepted'/selected[k] for k in sorted(expected)],
            {k:v['touches'] for k,v in expected.items()},registry,secret,stage,verify=lambda _:True,store=index)
        serial=policy.get('serial_integration')
        if not isinstance(serial,dict):raise SerialGateError('reviewed serial integration patch required')
        prelude=apply_approved_stage_prelude(stage,serial,patches)
        staged_digest=evidence.source_digest(stage)
        binding={'baseline':policy['baseline_sha256'],'candidate':staged_digest,'interface':policy['input_interface_sha256'],'layout':graph.layout_digest}
        stage_output=output/'staged-native-checks'
        prior=_execution.run_checks(staged_checks,stage,stage_output,binding,
              {'coordinator_integration':True,'bindings':binding,'stage_prelude_sha256':digest(prelude)},secret,RetainedPreludeEvidence(evidence,output/'staged-native-executions'),control)
        raw=dict(prior);signature=raw.pop('signature',None)
        if not isinstance(signature,str) or not hmac.compare_digest(signature,acceptance._sign(raw,secret)):
            raise SerialGateError('invalid staged proof signature')
        if prior.get('token',{}).get('coordinator_integration') is not True or prior.get('bindings')!=binding or prior.get('source_digest')!=staged_digest or prior.get('source_before')!=staged_digest:
            raise SerialGateError('staged proof not bound to complete preluded wave')
        trusted_staged_checks=_execution.historical_checks(prior,staged_checks)
        historical={'evidence':prior,'bindings':binding,'token':prior['token'],'checks':trusted_staged_checks}
        acceptance._historical(historical,secret,stage,stage_output,trusted_staged_checks)
        control_paths=CONTROL_PATHS
        if 'source_policy' in serial:
            control_paths=exact_serial_controls(policy,stage,root,patches)
        reconcile_serial_patch(stage,serial['artifact_directory'],serial,patches)
        reconciled={k:v for k,v in patches._tree(stage).items() if k not in control_paths}
        canonical={k:v for k,v in patches._tree(root).items() if k not in control_paths}
        if reconciled!=canonical:
            raise SerialGateError('canonical source contains missing or unexplained changes')
        canonical_binding={**binding,'candidate':before,'interface':policy['output_interface_sha256']}
        # Fresh native execution uses current canonical code, never an old receipt.
        with tempfile.TemporaryDirectory(prefix='serial-native-checks-') as native_output:
            execution=_execution.run_checks(checks,root,Path(native_output)/'checks',canonical_binding,
                                 {'serial_gate':task_id,'bindings':canonical_binding},secret,RetainedPreludeEvidence(evidence,output/'canonical-native-executions'),control)
            shutil.copytree(Path(native_output)/'checks',output/'canonical-checks')
        if evidence.source_digest(root)!=before or {k:v for k,v in patches._tree(stage).items() if k not in control_paths}!=canonical:
            raise SerialGateError('source changed during serial qualification')
        _safe_file(state/'config.json',policy['config_sha256'])
        _safe_file(registry_path,policy['lane_registry_sha256'])
        if _safe_file(key_path)!=secret or evidence.source_digest(baseline)!=policy['baseline_sha256']:
            raise SerialGateError('private coordinator bindings changed during qualification')
        if index.pin_selected(selected)!=index_pin:
            raise SerialGateError('authoritative acceptance index changed during serial checks')
        if control is not None and control.cancelled.is_set():raise SerialGateError('serial qualification cancelled')
        return {'qualified':True,'qualification_scope':'integrated-current-source','canonical_integrated':True,
                'input_interface_sha256':policy['input_interface_sha256'],'output_interface_sha256':policy['output_interface_sha256'],'accepted_index_sha256':index_pin,
                'task':task_id,'accepted_tasks':sorted(expected),'accepted_ids':selected,
                'canonical_sha256':before,'reconciled_product_sha256':digest(canonical),'control_paths':sorted(control_paths),'staged_sha256':staged_digest,'policy_sha256':entry['policy_sha256'],
                'checks':[{'id':r['id'],'passed':True,'native_proof':r['proof']} for r in execution['receipts']],
                'serial_manifest_sha256':serial['manifest_sha256'],'stage_evidence_sha256':hashlib.sha256(_safe_file(stage_output/'evidence.json')).hexdigest(),
                'stage_evidence_path':str(stage_output/'evidence.json'),'stage_prelude':prelude,'staged_check_ids':[r['id'] for r in prior['receipts']]}
    finally:
        index.close()
        if stage.exists(): shutil.rmtree(stage)
