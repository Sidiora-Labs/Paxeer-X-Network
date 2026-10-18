from __future__ import annotations
from datetime import datetime, timezone
import hashlib
import math
from pathlib import Path
from . import v1
from .attestations import regular_bytes


def artifact(reference):
    v1.keys(reference, ['path', 'sha256'], 'artifact reference')
    v1.sha(reference['sha256'], 'artifact digest')
    data = regular_bytes(reference['path'])
    v1.need(hashlib.sha256(data).hexdigest() == reference['sha256'], 'artifact byte identity mismatch')
    return v1.decode(data)


def validate_inventory(value, plan, config, phase=2):
    expected=['schemaVersion','zones','postgres','nats','runtimeOwners']
    if phase==3: expected.append('qualificationTarget')
    v1.keys(value,expected,'measured inventory')
    v1.need(type(value['schemaVersion']) is int and value['schemaVersion']==phase,'inventory version')
    if phase==3:
        target=value['qualificationTarget']
        v1.keys(target,['context','namespace','namespaceUID','environment'],'measured qualification target')
        v1.text(target['context'],'measured qualification context',253)
        v1.text(target['namespaceUID'],'measured qualification namespace UID')
        v1.need(target['environment']=='qualification' and target['namespace']==config['namespace'],
                'measured fault target must be isolated qualification infrastructure')
    v1.keys(value['zones'], config['zones'], 'provider zones')
    domains = list(value['zones'].values())
    for domain in domains:
        v1.text(domain, 'provider failure domain')
    v1.need(len(set(domains)) == 3, 'provider failure domains not independent')
    v1.need(value['runtimeOwners'] == plan['runtimeOwners'], 'runtime service ownership drift')
    for key in ('postgres', 'nats'):
        members = value[key]
        v1.need(type(members) is list and len(members) == 3, 'three measured storage members required')
        seen_ids, seen_zones = set(), set()
        for member in members:
            expected = ['id', 'zone', 'allocatedBytes'] + (['configuredStoreBytes'] if key == 'nats' else [])
            v1.keys(member, expected, 'measured ' + key + ' member')
            v1.text(member['id'], 'member id')
            v1.need(member['id'] not in seen_ids and member['zone'] in config['zones']
                    and member['zone'] not in seen_zones, 'storage members require distinct zones and identities')
            seen_ids.add(member['id'])
            seen_zones.add(member['zone'])
            v1.integer(member['allocatedBytes'], 1, 100 * v1.TIB, 'measured allocation')
            if key == 'postgres':
                v1.need(member['allocatedBytes'] >= plan['capacity']['postgres']['requiredBytesPerMember'],
                        'measured PostgreSQL allocation requires expansion')
            else:
                v1.integer(member['configuredStoreBytes'], 1, member['allocatedBytes'] * 9 // 10,
                           'measured broker store limit')
        if key == 'nats':
            usable = sum(member['configuredStoreBytes'] for member in members)
            for mode in ('normal', 'qualification'):
                v1.need(usable >= plan['capacity']['broker'][mode]['requiredBytesWithTwofoldHeadroom'],
                        'measured ' + mode + ' broker storage requires expansion')
    return value


def validate_metric(value, scenario, config):
    v1.keys(value, ['metric', 'samples'], 'metric artifact')
    v1.need(value['metric'] == scenario['metric'], 'metric identity mismatch')
    v1.integer(scenario['sampleCount'], 2, 10000000, 'metric sample count')
    samples = value['samples']
    v1.need(type(samples) is list and len(samples) == scenario['sampleCount'], 'metric sample count mismatch')
    start = v1.timestamp(scenario['startedAt'], 'scenario start')
    finish = v1.timestamp(scenario['finishedAt'], 'scenario finish')
    previous = None
    for sample in samples:
        v1.keys(sample, ['timestamp', 'value', 'service', 'environment', 'zone'], 'metric sample')
        instant = v1.timestamp(sample['timestamp'], 'sample timestamp')
        v1.need(start <= instant <= finish and (previous is None or instant >= previous),
                'metric timestamp outside scenario or out of order')
        previous = instant
        v1.need(type(sample['value']) in (int, float) and math.isfinite(sample['value']),
                'metric value must be finite numeric')
        v1.need(sample['service'] in set(config['images']) | {'platform'} and
                sample['environment'] == config['environment'] and sample['zone'] in config['zones'],
                'metric dimensions outside bounded deployment inventory')


def verify_bundle(bundle, plan, config, verifier, now=None):
    v1.keys(bundle, ['schemaVersion', 'payload', 'signature'], 'signed topology evidence')
    v1.need(type(bundle['schemaVersion']) is int and bundle['schemaVersion'] in (2, 3), 'bundle version')
    payload = bundle['payload']
    v1.keys(payload, ['schemaVersion', 'configurationDigest', 'deploymentDigest', 'sourceDigest',
                     'interfaceDigest', 'chainManifestDigest', 'baseEvidence', 'inventory', 'restore',
                     'canary', 'requirements'], 'attested payload')
    v1.need(type(payload['schemaVersion']) is int and payload['schemaVersion'] == bundle['schemaVersion'], 'payload version')
    for key in ('configurationDigest', 'deploymentDigest', 'sourceDigest', 'interfaceDigest', 'chainManifestDigest'):
        v1.need(payload[key] == plan[key], 'attested ' + key + ' mismatch')
    authenticated = verifier.verify(payload, bundle['signature'])
    measured = artifact(payload['baseEvidence'])
    v1.validate_evidence(measured, v1.compile_plan(config), config, now=now)
    inventory = artifact(payload['inventory'])
    v1.need(payload['inventory']['sha256'] == measured['inventoryDigest'], 'inventory attestation mismatch')
    validate_inventory(inventory, plan, config, phase=bundle['schemaVersion'])
    restored = artifact(payload['restore'])
    v1.keys(restored, ['restored', 'duplicateDatabaseEffects', 'lostAcknowledgedWrites', 'activePostgresWriters'],
            'restore reconciliation')
    v1.need(restored['restored'] is True, 'restore not successful')
    for key, expected in (('duplicateDatabaseEffects', 0), ('lostAcknowledgedWrites', 0), ('activePostgresWriters', 1)):
        v1.need(type(restored[key]) is int and restored[key] == expected, 'restore invariant failed: ' + key)
    canary = artifact(payload['canary'])
    v1.keys(canary, ['configurationDigest', 'deploymentDigest', 'sourceDigest', 'interfaceDigest',
                    'startedAt', 'finishedAt', 'trafficPercent', 'healthy', 'accountingReconciled', 'samples'],
            'canary evidence')
    for key in ('configurationDigest', 'deploymentDigest', 'sourceDigest', 'interfaceDigest'):
        v1.need(canary[key] == plan[key], 'canary identity mismatch')
    start = v1.timestamp(canary['startedAt'], 'canary start')
    end = v1.timestamp(canary['finishedAt'], 'canary finish')
    now = now or datetime.now(timezone.utc)
    v1.need(start <= end <= now and (end-start).total_seconds() >= config['release']['canarySeconds']
            and (now-end).total_seconds() <= config['release']['qualificationMaxAgeSeconds'],
            'canary interval invalid or stale')
    v1.need(type(canary['trafficPercent']) is int and canary['trafficPercent'] == 5 and
            canary['healthy'] is True and canary['accountingReconciled'] is True,
            'healthy five percent canary required')
    v1.need(type(canary['samples']) is list and 2 <= len(canary['samples']) <= 100000, 'canary samples missing')
    previous = None
    for sample in canary['samples']:
        v1.keys(sample, ['timestamp', 'totalRequests', 'candidateRequests', 'sloPassed', 'accountingPassed'],
                'canary sample')
        instant = v1.timestamp(sample['timestamp'], 'canary sample time')
        v1.need(start <= instant <= end and (previous is None or instant >= previous), 'canary sample interval')
        previous = instant
        v1.integer(sample['totalRequests'], 1, 2**63-1, 'canary requests')
        v1.integer(sample['candidateRequests'], 1, sample['totalRequests'], 'canary candidate requests')
        v1.need(sample['candidateRequests'] * 100 == sample['totalRequests'] * 5 and
                sample['sloPassed'] is True and sample['accountingPassed'] is True, 'canary measurement failed')
    v1.need(v1.timestamp(canary['samples'][0]['timestamp'], 'canary first sample') == start and
            v1.timestamp(canary['samples'][-1]['timestamp'], 'canary last sample') == end,
            'canary observations do not cover interval')
    v1.keys(payload['requirements'], v1.REQS, 'requirement artifacts')
    report = {}
    for scenario in measured['scenarios']:
        req = scenario['requirement']
        refs = payload['requirements'][req]
        v1.keys(refs, ['artifact', 'input', 'reconciliation', 'metric'], 'requirement artifact references')
        for kind, key in (('artifact', 'artifactDigest'), ('input', 'inputDigest'), ('reconciliation', 'reconciliationDigest')):
            v1.need(refs[kind]['sha256'] == scenario[key], 'scenario artifact identity mismatch')
            artifact(refs[kind])
        validate_metric(artifact(refs['metric']), scenario, config)
        if bundle['schemaVersion'] == 3:
            validate_adversary(artifact(refs['input']), artifact(refs['artifact']),
                               artifact(refs['reconciliation']), scenario, plan, config,
                               target=inventory['qualificationTarget'])
            metric=artifact(refs['metric'])
            fault_start=v1.timestamp(artifact(refs['input'])['faultStartedAt'],'fault start')
            fault_end=v1.timestamp(artifact(refs['input'])['faultFinishedAt'],'fault end')
            v1.need(any(fault_start<=v1.timestamp(sample['timestamp'],'fault metric time')<=fault_end
                        for sample in metric['samples']),'metric observations omit fault interval')
            v1.need(metric['samples'][0]['timestamp']==scenario['startedAt'] and
                    metric['samples'][-1]['timestamp']==scenario['finishedAt'],
                    'metric observations do not cover adversarial interval')
        report[req] = {'passed': True, 'metric': scenario['metric'], 'sampleCount': scenario['sampleCount'],
                       'artifactDigest': scenario['artifactDigest'], 'startedAt': scenario['startedAt'],
                       'finishedAt': scenario['finishedAt']}
    return {'schemaVersion': bundle['schemaVersion'], 'passed': True, 'evidenceDigest': v1.digest(bundle),
            'deploymentDigest': plan['deploymentDigest'], 'authenticated': authenticated, 'requirements': report}


def failing_report(reason, plan=None):
    return {'schemaVersion': 2, 'passed': False,
            'deploymentDigest': plan.get('deploymentDigest') if plan else None,
            'requirements': {req: {'passed': False, 'reason': reason} for req in v1.REQS}}


ADVERSARIES = dict(zip(v1.REQS, (
    'zone_application_loss', 'fleet_arithmetic', 'api_zone_loss', 'socket_zone_loss',
    'frame_burst', 'postgres_primary_loss', 'maximum_connections', 'broker_loss',
    'retention_ledgers', 'worker_zone_loss', 'cache_loss', 'combined_zone_loss',
    'denied_promotion', 'busy_sigterm', 'postgres_disconnect', 'committed_hash_conflict',
    'media_isolation', 'public_database_drift')))


def validate_adversary(inputs, observed, reconciliation, scenario, plan, config, target=None):
    req = scenario['requirement']
    v1.keys(inputs, ['schemaVersion', 'requirement', 'action', 'environment', 'context', 'namespace', 'namespaceUID',
                     'configurationDigest', 'deploymentDigest', 'faultStartedAt', 'faultFinishedAt',
                     'parameters'], 'adversarial input')
    v1.need(type(inputs['schemaVersion']) is int and inputs['schemaVersion'] == 3 and
            inputs['requirement'] == req and inputs['action'] == ADVERSARIES[req], 'adversarial input identity')
    v1.need(inputs['environment'] == 'qualification' and inputs['namespace'] == config['namespace'],
            'adversaries require isolated qualification infrastructure')
    v1.text(inputs['context'], 'qualification context', 253)
    v1.text(inputs['namespaceUID'],'qualification namespace UID')
    if target is not None:
        v1.need(all(inputs[field]==target[field] for field in ('context','namespace','namespaceUID','environment')),
                'scenario infrastructure differs from measured qualification target')
    v1.need(type(inputs['parameters']) is dict, 'scenario parameters required')
    start = v1.timestamp(scenario['startedAt'], 'scenario start')
    finish = v1.timestamp(scenario['finishedAt'], 'scenario finish')
    fault_start = v1.timestamp(inputs['faultStartedAt'], 'fault start')
    fault_end = v1.timestamp(inputs['faultFinishedAt'], 'fault end')
    v1.need(start <= fault_start < fault_end <= finish, 'fault interval missing or outside scenario')
    v1.keys(observed, ['schemaVersion', 'requirement', 'configurationDigest', 'deploymentDigest',
                       'startedAt', 'finishedAt', 'observations'], 'adversarial observation')
    v1.need(type(observed['schemaVersion']) is int and observed['schemaVersion'] == 3 and
            observed['requirement'] == req and observed['startedAt'] == scenario['startedAt'] and
            observed['finishedAt'] == scenario['finishedAt'], 'adversarial observation identity')
    for document in (inputs, observed):
        for field in ('configurationDigest', 'deploymentDigest'):
            v1.need(document[field] == plan[field], 'adversarial deployment identity changed')
    validate_reconciliation(reconciliation, req, start, finish, fault_start, fault_end)
    if req in ('D14_013','D14_018'):
        v1.need(reconciliation['beforeDigest']==reconciliation['afterDigest'],
                'denied control action changed authority state')
    parameters=inputs['parameters']
    required_inputs={
        'D14_001':{'dynamicRps':10000},
        'D14_002':{'sessionsPerDay':1000000,'requestsPerSession':50,'peakClients':100000},
        'D14_003':{'dynamicRps':10000},
        'D14_004':{'connectedClients':100000,'removedReplicas':10},
        'D14_005':{'framesPerSecond':1000000,'frameBytes':400,'burstSeconds':600},
        'D14_007':{'scaledReplicas':{role:row[1] for role,row in v1.workloads(config).items()}},
        'D14_009':{'rawEnvelopeBytes':1200,'retentionSeconds':259200,'normalEventsPerDay':10000000,'qualificationEventsPerSecond':2000},
        'D14_010':{'eventsPerSecond':2000,'burstEventsPerSecond':10000,'burstSeconds':300},
        'D14_011':{'deletedCacheDataset':'entire','dynamicRps':10000},
        'D14_012':{'dynamicRps':10000},
        'D14_013':{'attempts':['missingEvidence','changedDigest','staleActor','unauthorizedActor']},
        'D14_014':{'signal':'SIGTERM','busyWrites':True,'busySockets':True},
        'D14_015':{'disconnectedDependency':'postgres','redisAvailable':True},
        'D14_017':{'viewers':20000,'meanViewerBitsPerSecond':2000000}}
    for field,expected in required_inputs.get(req,{}).items():
        v1.need(field in parameters and type(parameters[field]) is type(expected) and parameters[field]==expected,
                req+' scenario workload input mismatch: '+field)
    for requirement,field in (('D14_006','primaryId'),('D14_008','brokerId'),('D14_016','endpointId'),
                              ('D14_018','exposedResourceId')):
        if req==requirement: v1.text(parameters.get(field),req+' fault input '+field)
    values = observed['observations']
    v1.need(type(values) is dict, 'scenario observations required')
    def exact(field, expected):
        v1.need(field in values and type(values[field]) is type(expected) and values[field] == expected,
                req + ' outcome failed: ' + field)
    def number(field, low, high=2**63-1):
        v1.integer(values.get(field), low, high, req + ' ' + field)
    def identifiers(field, count=None):
        items = values.get(field)
        v1.need(type(items) is list and 1 <= len(items) <= 100000 and
                (count is None or len(items) == count), req + ' identity inventory missing: ' + field)
        for item in items:
            v1.text(item, field)
        v1.need(len(set(items)) == len(items), req + ' duplicate member identity: ' + field)
        return items
    def replicas(field, count, target, maximum=None):
        rows = values.get(field)
        v1.need(type(rows) is list and len(rows) == count, req + ' survivor measurements missing')
        seen = set()
        for row in rows:
            expected=['id','zone','rate','p99Ms','latencyTargetMs']
            if req=='D14_003': expected += ['readP99Ms','quoteP99Ms']
            if req=='D14_004':
                expected += [field+suffix for field in ('fileDescriptors','memoryBytes','fanoutBytes') for suffix in ('','Limit')]
            v1.keys(row,expected,'survivor measurement')
            v1.text(row['id'], 'survivor id')
            v1.need(row['id'] not in seen and row['zone'] in config['zones'] and
                    row['zone'] != inputs['parameters'].get('removedZone'), 'survivor identity invalid')
            seen.add(row['id'])
            v1.integer(row['rate'], target, maximum or 2**63-1, 'survivor throughput')
            v1.integer(row['latencyTargetMs'], 1, 2000, 'latency target')
            v1.integer(row['p99Ms'], 0, row['latencyTargetMs'], 'measured survivor latency')
            if req=='D14_003':
                v1.integer(row['readP99Ms'],0,400,'surviving read latency')
                v1.integer(row['quoteP99Ms'],0,750,'surviving quote latency')
            if req=='D14_004':
                for field in ('fileDescriptors','memoryBytes','fanoutBytes'):
                    v1.integer(row[field+'Limit'],1,2**63-1,'measured survivor '+field+' limit')
                    v1.integer(row[field],1,row[field+'Limit'],'measured survivor '+field)
    if req in ('D14_001', 'D14_003', 'D14_004', 'D14_010', 'D14_012'):
        v1.need(inputs['parameters'].get('removedZone') in config['zones'], 'removed zone input missing')
        exact('unavailableZone', inputs['parameters']['removedZone'])
    if req == 'D14_001':
        number('admittedRequests', 1); exact('failedAdmittedRequests', 0); exact('activePostgresWriters', 1)
        identifiers('survivingZones', 2)
        v1.need(set(values['survivingZones']) == set(config['zones']) - {values['unavailableZone']},
                'surviving zones mismatch')
    elif req == 'D14_002':
        for field, value in (('dau',500000),('sessionsPerDay',1000000),('requestsPerSession',50),
                             ('dynamicRequestsPerDay',50000000),('peakClients',100000),
                             ('averageConcurrencyNumerator',31250),('averageConcurrencyDenominator',3)):
            exact(field, value)
    elif req == 'D14_003':
        number('dynamicRps',10000); replicas('survivors',16,625)
    elif req == 'D14_004':
        exact('connectedClients',100000); exact('removedReplicas',10); exact('reconnectFailures',0)
        replicas('survivors',20,5000,config['admission']['socketConnectionsPerReplica'])
        for field in ('fileDescriptors', 'memoryBytes', 'fanoutBytes'):
            number(field+'Limit',1); number(field,1,values[field+'Limit'])
    elif req == 'D14_005':
        number('framesPerSecond',1000000); number('burstSeconds',600); exact('frameBytes',400); exact('truncatedFrames',0)
        number('payloadBytesPerSecond',400000000); number('transportOverheadBytesPerSecond',1)
        number('wireBytesPerSecond',values['payloadBytesPerSecond']+values['transportOverheadBytesPerSecond'])
    elif req == 'D14_006':
        exact('activePostgresWriters',1); exact('oldPrimaryFenced',True)
        eligible = identifiers('eligibleReplicaIds')
        v1.need(values.get('promotedId') in eligible, 'promotion not synchronous eligible')
        number('promotions',1,1); number('acknowledgedSynchronousCommits',1)
        exact('recoveredSynchronousCommits',values['acknowledgedSynchronousCommits'])
    elif req == 'D14_007':
        number('applicationConnections',1,539); number('acquireDeadlineMs',1,config['connections']['acquireTimeoutMs'])
        number('excessWorkRejected',1)
        exact('scaledReplicas',{role: row[1] for role,row in v1.workloads(config).items()})
    elif req == 'D14_008':
        exact('availableBrokerMembers',2); exact('currentQuorumMembers',2)
        number('admittedMessages',1); exact('persistedMessages',values['admittedMessages'])
        number('redeliveries',1); exact('duplicateDatabaseEffects',0); exact('consumerRecovered',True)
    elif req == 'D14_009':
        exact('rawEnvelopeBytes',1200); exact('retentionSeconds',259200)
        exact('normal',plan['capacity']['broker']['normal'])
        exact('qualification',plan['capacity']['broker']['qualification'])
        number('measuredBrokerOverheadPermille',1,config['storage']['brokerOverheadPermille'])
        exact('measuredStreams',config['streams'])
        pg = plan['capacity']['postgres']
        exact('postgresThirtyDayRawBytes',555360000000); exact('postgresAmplifiedBytesPerMember',1666080000000)
        for field,key in (('derivedBytesPerMember','derivedPgBytesPerMember'),('walBytesPerMember','walBytesPerMember'),
                          ('vacuumBytesPerMember','vacuumBytesPerMember')):
            number(field,1,config['storage'][key])
        members = values.get('postgresMembers')
        v1.need(type(members) is list and len(members)==3,'measured PostgreSQL member capacities missing')
        seen = set()
        for member in members:
            v1.keys(member,['id','allocatedBytes','freeBytes'],'PostgreSQL capacity observation')
            v1.text(member['id'],'member id'); v1.need(member['id'] not in seen,'duplicate PostgreSQL capacity member')
            seen.add(member['id'])
            v1.integer(member['allocatedBytes'],pg['requiredBytesPerMember'],100*v1.TIB,'measured PostgreSQL allocation')
            v1.integer(member['freeBytes'],1,member['allocatedBytes'],'measured PostgreSQL free bytes')
            used = 1666080000000 + sum(values[f] for f in ('derivedBytesPerMember','walBytesPerMember','vacuumBytesPerMember'))
            v1.need(used + member['allocatedBytes']*config['storage']['freeHeadroomPermille']//1000 <= member['allocatedBytes']
                    and member['freeBytes']*1000 >= member['allocatedBytes']*config['storage']['freeHeadroomPermille'],
                    'PostgreSQL measured free headroom insufficient')
    elif req == 'D14_010':
        number('eventsPerSecond',2000); number('lagSeconds',0,values.get('lagLimitSeconds',-1))
        number('lagLimitSeconds',1,900); number('recoverySeconds',0,900)
        number('burstEventsPerSecond',10000); number('burstSeconds',300)
        exact('staleLeaseAdvances',0); exact('validLeaseWriters',1); number('staleLeaseAttempts',1)
    elif req == 'D14_011':
        exact('cacheEntriesAfterDelete',0); exact('lostBusinessEvents',0); number('rebuildSeconds',1,900)
        number('coldReadLimit',1,config['admission']['coldReadInflightPerReplica'])
        number('coldReadInflight',0,values['coldReadLimit']); number('coldReadsRejected',1)
    elif req == 'D14_012':
        exact('removedRoles',['api','sockets','workers','nats']); number('dynamicRps',10000)
        number('readP99Ms',0,values.get('readLatencyTargetMs',-1)); number('readLatencyTargetMs',1,400)
        number('nonessentialRejected',1); exact('unsafeAcknowledgments',0)
        number('writeRps',2000,values['dynamicRps']); number('quoteRps',2000,values['dynamicRps'])
        v1.need(values['writeRps']+values['quoteRps']<=values['dynamicRps'],'write/quote subsets exceeded total')
    elif req == 'D14_013':
        for field in ('missingEvidenceRejected','changedDigestRejected','staleActorRejected','unauthorizedActorRejected'):
            exact(field,True)
        exact('productionReplicasReplaced',0); exact('authorityChanged',False); exact('credentialBytesExposed',0)
    elif req == 'D14_014':
        exact('signal','SIGTERM'); exact('readinessRemovedBeforeAdmissionStop',True)
        number('propagationSeconds',5); number('drainSeconds',0,30); number('terminationGraceSeconds',45)
        number('completedWrites',1); exact('completedWriteReceipts',values['completedWrites'])
        number('uncompletedRequests',1); exact('retriableErrors',values['uncompletedRequests'])
        number('socketReconnects',1); exact('replayCursorReconnects',values['socketReconnects'])
    elif req == 'D14_015':
        exact('postgresAvailable',False); exact('redisAvailable',True); exact('livenessHealthy',True)
        exact('writeReady',False); exact('dependencyReason','postgres'); exact('staleReadsMarked',True)
        number('snapshotAgeSeconds',1); number('probeTimeoutMs',1,2000); exact('failureThreshold',3)
    elif req == 'D14_016':
        number('committedHeight',1)
        for field in ('acceptedHash','conflictingHash'):
            v1.need(type(values.get(field)) is str and __import__('re').fullmatch(r'0x[0-9a-fA-F]{64}',values[field]),'committed hash missing')
        v1.need(values['acceptedHash'] != values['conflictingHash'],'no hash conflict exercised')
        exact('endpointQuarantined',True); v1.text(values.get('incidentId'),'integrity incident')
        exact('canonicalRollbackCount',0); exact('canonicalHashAfter',values['acceptedHash'])
        number('headIngestP99Ms',0,500)
    elif req == 'D14_017':
        exact('viewers',20000); exact('meanViewerBitsPerSecond',2000000)
        number('cdnPayloadBitsPerSecond',40000000000); exact('apiMediaPayloadBytes',0)
        number('cdnTransportOverheadBitsPerSecond',1)
    elif req == 'D14_018':
        exact('rejected',True); exact('appliedResources',0); exact('authorityChanged',False)
        exact('credentialBytesExposed',0); v1.text(values.get('exposedResourceId'),'exposed resource identifier')
        exact('exposedResourceId',parameters['exposedResourceId'])
    return {'requirement':req,'passed':True,'phase':3}


def validate_reconciliation(value, requirement, start, finish, fault_start, fault_end):
    v1.keys(value,['schemaVersion','requirement','beforeDigest','afterDigest','acknowledgedWriteIds',
                   'receiptIds','effectCounts','lostAcknowledgedWrites','duplicateDatabaseEffects',
                   'activePostgresWriters','restored','recoveredAt','timeline'],'adversarial reconciliation')
    v1.need(type(value['schemaVersion']) is int and value['schemaVersion']==3 and
            value['requirement']==requirement,'reconciliation identity mismatch')
    for field in ('beforeDigest','afterDigest'):
        v1.sha(value[field],field)
    v1.need(value['restored'] is True,'adversarial recovery or restore failed')
    recovered = v1.timestamp(value['recoveredAt'],'recovery time')
    v1.need(fault_end <= recovered <= finish,'recovery outside scenario interval')
    for field,expected in (('lostAcknowledgedWrites',0),('duplicateDatabaseEffects',0),('activePostgresWriters',1)):
        v1.need(type(value[field]) is int and value[field]==expected,'reconciliation invariant failed: '+field)
    for field in ('acknowledgedWriteIds','receiptIds'):
        ids=value[field]
        v1.need(type(ids) is list and len(ids)<=100000,'bounded write reconciliation required')
        for identity in ids:
            v1.text(identity,'durable write identity')
        v1.need(len(set(ids))==len(ids),'duplicate durable write identity')
    v1.need(set(value['acknowledgedWriteIds'])<=set(value['receiptIds']),'acknowledged write missing durable receipt')
    effects=value['effectCounts']
    v1.need(type(effects) is dict and len(effects)<=100000 and set(effects)==set(value['receiptIds']),
            'complete database effect reconciliation required')
    for count in effects.values():
        v1.need(type(count) is int and count==1,'duplicate or missing database effect')
    if requirement in ('D14_006','D14_008','D14_011','D14_012','D14_014'):
        v1.need(len(value['acknowledgedWriteIds'])>0,'fault requires acknowledged writes to reconcile')
    events=value['timeline']
    v1.need(type(events) is list and 4<=len(events)<=100000,'incident timeline missing')
    previous=start
    phases=set()
    previous_phase=-1
    for event in events:
        v1.keys(event,['at','phase','stateDigest'],'incident event')
        instant=v1.timestamp(event['at'],'incident timestamp')
        v1.need(previous<=instant<=finish,'incident timeline order invalid'); previous=instant
        v1.sha(event['stateDigest'],'incident state digest')
        phase=event['phase']
        v1.need(phase in ('before','fault','recovering','recovered'),'unknown incident phase')
        order=('before','fault','recovering','recovered').index(phase)
        v1.need(order>=previous_phase,'incident phase order invalid'); previous_phase=order
        v1.need((phase!='before' or instant<=fault_start) and
                (phase!='fault' or fault_start<=instant<=fault_end) and
                (phase!='recovering' or instant>=fault_end) and
                (phase!='recovered' or instant>=recovered),'incident phase outside fault/recovery interval')
        phases.add(phase)
    v1.need(phases=={'before','fault','recovering','recovered'},'complete recovery timeline required')
    v1.need(events[0]['stateDigest']==value['beforeDigest'] and events[-1]['stateDigest']==value['afterDigest'],
            'incident timeline does not bind reconciled states')
