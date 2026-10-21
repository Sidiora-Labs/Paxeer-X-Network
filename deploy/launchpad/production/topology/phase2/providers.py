from __future__ import annotations
import copy
import hashlib
import json
from datetime import datetime, timezone
import uuid
from importlib.util import module_from_spec, spec_from_file_location
import os
from pathlib import Path
import subprocess
import tempfile
from . import v1
from .attestations import regular_bytes


REQUIRED_DOCUMENTS = {'api': {'accounting_read'}, 'indexer': {'receipt', 'delivery'},
                      'charts': {'delivery'}, 'stats': {'delivery'}, 'pnl': {'delivery'},
                      'gateway': {'identity'}}


def connected_plan(config, source_root, adapter_sha256, documents, chain_file, chain_sha256):
    v1.need(config.get('schemaVersion') == 2, 'connected operator requires additive topology v2')
    base = v1.compile_plan(config)
    root = Path(source_root).resolve()
    path = root / 'spec/production-system/integration/runtime/adapters.py'
    v1.need(hashlib.sha256(regular_bytes(path)).hexdigest() == adapter_sha256,
            'integrated runtime adapter digest mismatch')
    spec = spec_from_file_location('kindle_integrated_runtime', path)
    adapters = module_from_spec(spec)
    spec.loader.exec_module(adapters)
    v1.keys(documents, adapters.ROLES, 'runtime document roles')
    v1.need(set(adapters.ROLES) == set(v1.RUNTIME_ROLES), 'integrated runtime role drift')
    bindings = {}
    for role, docs in documents.items():
        required = {'budget', 'readiness'} | REQUIRED_DOCUMENTS.get(role, set())
        v1.need(type(docs) is dict and required <= set(docs) <= set(adapters.DOCUMENT_ENV),
                'runtime documents incomplete for ' + role)
        v1.need('private_manifest' not in docs, 'private fixture cannot authorize cluster deployment')
        bindings[role] = {}
        for kind, document in docs.items():
            v1.keys(document, ['path', 'sha256'], 'document binding')
            v1.sha(document['sha256'], 'document digest')
            _, observed = adapters.document_binding(document['path'])
            v1.need(observed == document['sha256'], 'runtime document changed: ' + role + '/' + kind)
            bindings[role][kind] = observed
        declared = config['runtimeDocuments'][role]
        v1.need(bindings[role]['budget'] == declared['budgetSHA256'] and
                bindings[role]['readiness'] == declared['expectedSchemaSHA256'],
                'rendered runtime document digest drift')
    chain_bytes = regular_bytes(chain_file)
    v1.need(hashlib.sha256(chain_bytes).hexdigest() == chain_sha256, 'chain manifest digest mismatch')
    chain = v1.load(chain_file)
    v1.need(type(chain.get('core')) is dict and chain['core'].get('chainId') == 125,
            'chain125 deployment manifest required')
    addresses = {key: value for key, value in chain['core'].items()
                 if type(value) is str and value.startswith('0x')}
    v1.need(addresses and all(__import__('re').fullmatch(r'0x[0-9a-fA-F]{40}', value)
                             and int(value, 16) != 0 for value in addresses.values()),
            'chain contract address malformed')
    resources = copy.deepcopy(base['resources'])
    for item in resources['items']:
        if item['kind'] != 'Deployment':
            continue
        role = item['metadata']['name']
        v1.need(adapters.ROLES[role][2] == v1.RUNTIME_ROLES[role], 'service owner identity drift')
        pod = item['spec']['template']
        pod['metadata']['annotations'].update({'kindle.io/source-sha256': base['sourceDigest'],
                                               'kindle.io/interface-sha256': base['interfaceDigest'],
                                               'kindle.io/chain-manifest-sha256': chain_sha256,
                                               'kindle.io/runtime-role': adapters.ROLES[role][2]})
        env = pod['spec']['containers'][0]['env']
        existing = {value['name'] for value in env}
        for kind, sha in sorted(bindings[role].items()):
            path_env, sha_env = adapters.DOCUMENT_ENV[kind]
            for name, value in ((path_env, '/run/kindle/' + ('readiness' if kind == 'readiness' else kind) + '.json'),
                                (sha_env, sha)):
                if name not in existing:
                    env.append({'name': name, 'value': value})
                    existing.add(name)
        pod['metadata']['annotations']['kindle.io/runtime-documents-sha256'] = v1.digest(bindings[role])
    result = {'schemaVersion': 2, 'configurationDigest': base['configurationDigest'],
              'deploymentDigest': v1.digest(resources), 'sourceDigest': base['sourceDigest'],
              'interfaceDigest': base['interfaceDigest'], 'baseDeclarationDigest': base['deploymentDigest'],
              'resources': resources, 'capacity': base['capacity'], 'documentBindings': bindings,
              'chainManifestDigest': chain_sha256, 'chainId': 125,
              'chainRecordedStatus': chain['core'].get('recordedStatus'),
              'chainAddresses': addresses, 'runtimeOwners': {r: adapters.ROLES[r][2] for r in adapters.ROLES}}
    validate_connected_plan(result, config)
    return result


DOCUMENT_ENV = {
    'budget': ('DB_BUDGET_FILE','DB_BUDGET_SHA256'),
    'readiness': ('EXPECTED_SCHEMA_FILE','EXPECTED_SCHEMA_SHA256'),
    'receipt': ('INDEXER_RECEIPT_CONFIG_FILE','INDEXER_RECEIPT_CONFIG_SHA256'),
    'delivery': ('DELIVERY_CONFIG_FILE','DELIVERY_CONFIG_SHA256'),
    'identity': ('IDENTITY_CONFIG_FILE','IDENTITY_CONFIG_SHA256'),
    'accounting_read': ('ACCOUNTING_READ_CONFIG_FILE','ACCOUNTING_READ_CONFIG_SHA256')}


def validate_connected_plan(plan, config):
    v1.need(plan['configurationDigest'] == v1.digest(config), 'connected configuration drift')
    v1.need(plan['sourceDigest'] == config['release']['sourceDigest'] and
            plan['interfaceDigest'] == config['release']['interfaceDigest'], 'connected release identity drift')
    checked = dict(plan, schemaVersion=1)
    v1.validate_rendered(checked, config)
    v1.need(plan.get('runtimeOwners') == v1.RUNTIME_ROLES, 'connected runtime authority drift')
    v1.sha(plan['chainManifestDigest'],'connected chain digest')
    base = v1.compile_plan(config)
    v1.need(plan['baseDeclarationDigest']==base['deploymentDigest'] and plan['capacity']==base['capacity'],
            'connected declaration drift')
    v1.keys(plan['documentBindings'],v1.RUNTIME_ROLES,'connected document role inventory')
    expected = copy.deepcopy(base['resources'])
    for item in expected['items']:
        if item['kind'] != 'Deployment':
            continue
        role = item['metadata']['name']
        documents = plan['documentBindings'][role]
        required = {'budget','readiness'} | REQUIRED_DOCUMENTS.get(role,set())
        v1.need(type(documents) is dict and required <= set(documents) <= set(DOCUMENT_ENV),
                'connected document inventory drift')
        for sha in documents.values():
            v1.sha(sha,'connected document digest')
        declared = config['runtimeDocuments'][role]
        v1.need(documents['budget']==declared['budgetSHA256'] and
                documents['readiness']==declared['expectedSchemaSHA256'],'connected document declaration drift')
        pod = item['spec']['template']
        pod['metadata']['annotations'].update({'kindle.io/source-sha256':plan['sourceDigest'],
            'kindle.io/interface-sha256':plan['interfaceDigest'],
            'kindle.io/chain-manifest-sha256':plan['chainManifestDigest'],
            'kindle.io/runtime-role':v1.RUNTIME_ROLES[role],
            'kindle.io/runtime-documents-sha256':v1.digest(documents)})
        env = pod['spec']['containers'][0]['env']
        existing = {value['name'] for value in env}
        for kind,sha in sorted(documents.items()):
            path_env,sha_env = DOCUMENT_ENV[kind]
            for name,value in ((path_env,'/run/kindle/'+('readiness' if kind=='readiness' else kind)+'.json'),
                               (sha_env,sha)):
                if name not in existing:
                    env.append({'name':name,'value':value}); existing.add(name)
    v1.need(plan['resources']==expected,'connected plan differs from reviewed compiler output')
    return True


def safe_diagnostics(plan):
    return {key: plan[key] for key in ('schemaVersion', 'configurationDigest', 'deploymentDigest',
            'sourceDigest', 'interfaceDigest', 'baseDeclarationDigest', 'chainManifestDigest',
            'chainId', 'chainRecordedStatus', 'chainAddresses', 'runtimeOwners', 'documentBindings', 'capacity')}


class Kubernetes:
    def __init__(self, executable, executable_sha256, context, namespace, evidence_directory):
        self.executable = str(Path(executable).absolute())
        v1.need(os.access(self.executable, os.X_OK), 'kubectl executable unavailable')
        v1.need(hashlib.sha256(regular_bytes(self.executable, 128 * 1024 * 1024)).hexdigest() == executable_sha256,
                'kubectl executable digest mismatch')
        v1.text(context, 'explicit approved Kubernetes context', 253)
        v1.need(not context.startswith('-'), 'invalid Kubernetes context')
        v1.name(namespace, 'namespace')
        self.executable_sha256 = executable_sha256
        self.context, self.namespace = context, namespace
        self.evidence_directory = Path(evidence_directory)
        self.evidence_directory.mkdir(parents=True, exist_ok=True)

    def _invoke(self, argv, stdin=None):
        v1.need(hashlib.sha256(regular_bytes(self.executable, 128 * 1024 * 1024)).hexdigest() == self.executable_sha256,
                'kubectl executable changed')
        with tempfile.TemporaryFile() as stdout, tempfile.TemporaryFile() as stderr:
            timed_out=False
            try:
                result = subprocess.run([self.executable, '--context', self.context, '--namespace', self.namespace,
                                         '--request-timeout=20s', *argv], input=stdin,
                                        stdout=stdout, stderr=stderr, timeout=60, check=False)
                exit_code=result.returncode
            except subprocess.TimeoutExpired:
                timed_out=True
                exit_code=None
            stdout.seek(0)
            data = stdout.read(2 * 1024 * 1024 + 1)
            stderr.seek(0)
            error = stderr.read(2 * 1024 * 1024 + 1)
            record = {'schemaVersion': 2, 'at': datetime.now(timezone.utc).isoformat(),
                      'argv': ['kubectl', '--context', self.context, '--namespace', self.namespace, *argv],
                      'executableDigest': self.executable_sha256, 'exitCode': exit_code, 'timedOut':timed_out,
                      'stdoutTruncated':len(data)>2*1024*1024,
                      'inputDigest': hashlib.sha256(stdin).hexdigest() if stdin is not None else None,
                      'stdoutDigest': hashlib.sha256(data).hexdigest(),
                      'stderrPrefixDigest': hashlib.sha256(error).hexdigest(),
                      'stderrTruncated': len(error) > 2 * 1024 * 1024}
            evidence = self.evidence_directory / ('kubernetes-' + uuid.uuid4().hex + '.json')
            with evidence.open('x') as output:
                json.dump(record, output, sort_keys=True)
                output.write('\n')
            v1.need(not timed_out and len(data)<=2*1024*1024 and exit_code==0,
                    'Kubernetes operation failed with exit ' + str(exit_code)
                    + '; receipt=' + str(evidence))
            return data

    def dry_run(self, plan):
        self._invoke(['apply', '--server-side', '--field-manager=kindle-topology-v2',
                      '--dry-run=server', '-f', '-', '-o', 'name'], v1.canonical(plan['resources']))
        return {'deploymentDigest': plan['deploymentDigest'], 'context': self.context,
                'namespace': self.namespace, 'dryRun': 'server', 'accepted': True}

    def activate(self, plan):
        self._invoke(['apply', '--server-side', '--field-manager=kindle-topology-v2',
                      '-f', '-', '-o', 'name'], v1.canonical(plan['resources']))
        for item in plan['resources']['items']:
            if item['kind'] in ('Deployment', 'StatefulSet'):
                self._invoke(['rollout', 'status', item['kind'].lower() + '/' + item['metadata']['name'],
                              '--timeout=45s'])
        return {'deploymentDigest': plan['deploymentDigest'], 'context': self.context,
                'namespace': self.namespace, 'applied': True}

    def qualification_target(self, namespace_uid):
        value = v1.decode(self._invoke(['get','namespace',self.namespace,'-o','json']))
        v1.need(value['metadata']['uid'] == namespace_uid and
                value['metadata'].get('labels',{}).get('kindle.io/environment') == 'qualification',
                'fault target is not the independently marked qualification namespace')

    def rehearsal_snapshot(self):
        value = v1.decode(self._invoke(['get','pods','-o','json']))
        pods=[]
        for item in value['items']:
            meta=item['metadata']; labels=meta.get('labels',{})
            role=labels.get('app.kubernetes.io/name') or ('postgres' if labels.get('cnpg.io/cluster')=='postgres' else None)
            if role is None:
                continue
            pods.append({'id':meta['name'],'uid':meta['uid'],'role':role,
                         'node':item['spec'].get('nodeName'),'phase':item.get('status',{}).get('phase'),
                         'writer':labels.get('cnpg.io/instanceRole')=='primary',
                         'ready':any(c.get('type')=='Ready' and c.get('status')=='True' for c in item.get('status',{}).get('conditions',[])),
                         'terminating':bool(meta.get('deletionTimestamp'))})
        return {'at':datetime.now(timezone.utc).isoformat(),'pods':pods,
                'activePostgresWriters':sum(p['writer'] and not p['terminating'] for p in pods)}

    def prepare_rehearsal(self, plan, request):
        v1.keys(request,['id','action','namespaceUID','removedZone','pods','holdSeconds'],'rehearsal request')
        v1.name(request['id'],'rehearsal id'); v1.text(request['namespaceUID'],'qualification namespace UID')
        v1.integer(request['holdSeconds'],1,60,'bounded fault duration')
        v1.need(request['action'] in ('zone_application_loss','combined_zone_loss','postgres_primary_loss',
                                     'broker_loss','busy_sigterm'),'unsupported native infrastructure adversary')
        self.qualification_target(request['namespaceUID'])
        snapshot=self.rehearsal_snapshot()
        available={p['id']:p for p in snapshot['pods']}
        v1.need(type(request['pods']) is list and 1<=len(request['pods'])<=200,'exact fault pod inventory required')
        seen=set()
        for pod in request['pods']:
            v1.keys(pod,['name','uid','role'],'fault pod')
            v1.name(pod['name'],'fault pod name'); v1.text(pod['uid'],'fault pod UID')
            actual=available.get(pod['name'])
            v1.need(actual is not None and actual['uid']==pod['uid'] and actual['role']==pod['role'] and
                    not actual['terminating'] and pod['name'] not in seen,'fault pod identity changed')
            seen.add(pod['name'])
            if request['action']=='postgres_primary_loss':
                v1.need(len(request['pods'])==1 and actual['writer'],'fault requires exactly the current primary')
            elif request['action']=='broker_loss':
                v1.need(len(request['pods'])==1 and pod['role']=='nats','fault requires exactly one broker')
            elif request['action']=='busy_sigterm':
                v1.need(len(request['pods'])==1 and pod['role'] in v1.RUNTIME_ROLES,'fault requires application replica')
            else:
                node=v1.decode(self._invoke(['get','node',actual['node'],'-o','json']))
                v1.need(request['removedZone'] in [expression['values'][i]
                        for item in plan['resources']['items'] if item['kind']=='Deployment'
                        for term in item['spec']['template']['spec']['affinity']['nodeAffinity']['requiredDuringSchedulingIgnoredDuringExecution']['nodeSelectorTerms']
                        for expression in term['matchExpressions'] if expression['key']=='topology.kubernetes.io/zone'
                        for i in range(len(expression['values']))] and
                        node['metadata']['labels'].get('topology.kubernetes.io/zone')==request['removedZone'],
                        'fault pod is outside approved zone')
                allowed=set(v1.RUNTIME_ROLES)|({'nats'} if request['action']=='combined_zone_loss' else set())
                v1.need(pod['role'] in allowed,'zone fault includes unowned infrastructure')
        if request['action'] in ('zone_application_loss','combined_zone_loss'):
            allowed=set(v1.RUNTIME_ROLES)|({'nats'} if request['action']=='combined_zone_loss' else set())
            expected=set()
            nodes={}
            for pod in snapshot['pods']:
                if pod['role'] not in allowed or pod['terminating']:
                    continue
                v1.need(pod['node'] is not None,'unscheduled pod cannot prove zone loss')
                if pod['node'] not in nodes:
                    node=v1.decode(self._invoke(['get','node',pod['node'],'-o','json']))
                    nodes[pod['node']]=node['metadata']['labels'].get('topology.kubernetes.io/zone')
                if nodes[pod['node']]==request['removedZone']:
                    expected.add(pod['id'])
            v1.need(seen==expected,'zone fault must include every owned replica in the removed zone')
            target_names={p['role'] for p in request['pods']}
            resources=[item for item in plan['resources']['items'] if item['kind'] in ('Deployment','StatefulSet')
                       and item['metadata']['name'] in target_names]
        else:
            resources=[]
        return {'before':snapshot,'restoreResources':{'apiVersion':'v1','kind':'List','items':resources}}

    def inject_rehearsal(self, plan, request, prepared):
        self.qualification_target(request['namespaceUID'])
        for item in prepared['restoreResources']['items']:
            affinity=copy.deepcopy(item['spec']['template']['spec']['affinity'])
            if 'nodeAffinity' not in affinity:
                affinity['nodeAffinity']={'requiredDuringSchedulingIgnoredDuringExecution':{'nodeSelectorTerms':[{'matchExpressions':[]}]}}
            for term in affinity['nodeAffinity']['requiredDuringSchedulingIgnoredDuringExecution']['nodeSelectorTerms']:
                term.setdefault('matchExpressions',[]).append({'key':'topology.kubernetes.io/zone','operator':'NotIn','values':[request['removedZone']]})
            self._invoke(['patch',item['kind'].lower(),item['metadata']['name'],'--type=merge','-p',
                          json.dumps({'spec':{'template':{'spec':{'affinity':affinity}}}})])
        for pod in request['pods']:
            self._invoke(['delete','--raw','/api/v1/namespaces/'+self.namespace+'/pods/'+pod['name'],'-f','-'],
                         v1.canonical({'apiVersion':'v1','kind':'DeleteOptions','gracePeriodSeconds':45 if request['action']=='busy_sigterm' else 0,
                                       'preconditions':{'uid':pod['uid']}}))
        import time
        time.sleep(request['holdSeconds'])
        return self.rehearsal_snapshot()

    def recover_rehearsal(self, request, prepared):
        self.qualification_target(request['namespaceUID'])
        for item in prepared['restoreResources']['items']:
            current=v1.decode(self._invoke(['get',item['kind'].lower(),item['metadata']['name'],'-o','json']))
            expected=item['spec']['template']['metadata'].get('annotations',{}).get('kindle.io/config-sha256')
            actual=current['spec']['template']['metadata'].get('annotations',{}).get('kindle.io/config-sha256')
            v1.need(actual==expected,'recovery configuration changed')
            patch=[{'op':'test','path':'/metadata/resourceVersion','value':current['metadata']['resourceVersion']},
                   {'op':'replace','path':'/spec/template/spec/affinity','value':item['spec']['template']['spec']['affinity']}]
            self._invoke(['patch',item['kind'].lower(),item['metadata']['name'],'--type=json','-p',json.dumps(patch)])
        for item in prepared['restoreResources']['items']:
            self._invoke(['rollout','status',item['kind'].lower()+'/'+item['metadata']['name'],'--timeout=45s'])
        roles={pod['role'] for pod in request['pods']}
        for role in roles:
            label='cnpg.io/cluster=postgres' if role=='postgres' else 'app.kubernetes.io/name='+role
            self._invoke(['wait','--for=condition=Ready','pod','-l',label,'--timeout=45s'])
        recovered=self.rehearsal_snapshot()
        v1.need(recovered['activePostgresWriters']==1,'recovery must reconcile exactly one writer')
        for role in roles:
            before=sum(p['role']==role and not p['terminating'] for p in prepared['before']['pods'])
            after=sum(p['role']==role and p['ready'] and not p['terminating'] for p in recovered['pods'])
            v1.need(after>=before,'recovery replica reconciliation incomplete: '+role)
        return recovered
