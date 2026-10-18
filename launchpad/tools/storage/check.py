#!/usr/bin/env python3
import os
import pathlib
import signal
import subprocess
import sys
import time
import uuid

ROOT = pathlib.Path(__file__).resolve().parents[2]
NAME = 'kindle-l07-postgres-' + uuid.uuid4().hex
IMAGE = 'postgres:16-alpine'


def interrupted(signum, _frame):
    raise SystemExit(128 + signum)


def docker(*args, **kwargs):
    return subprocess.run(['docker', *args], check=True, timeout=30, **kwargs)


def main():
    for signum in (signal.SIGINT, signal.SIGTERM):
        signal.signal(signum, interrupted)
    created = False
    result = 1
    try:
        docker('run', '--detach', '--pull=never', '--name', NAME,
               '--label', 'kindle.lane=L07', '--tmpfs', '/var/lib/postgresql/data:rw',
               '--publish', '127.0.0.1::5432', '-e', 'POSTGRES_USER=kl',
               '-e', 'POSTGRES_PASSWORD=kl', '-e', 'POSTGRES_DB=kindle_test',
               IMAGE, stdout=sys.stderr)
        created = True
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            ready = subprocess.run(
                ['docker', 'exec', NAME, 'pg_isready', '-U', 'kl', '-d', 'kindle_test'],
                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
            if ready.returncode == 0:
                break
            time.sleep(.1)
        else:
            raise RuntimeError('private PostgreSQL fixture not ready within 30 seconds')
        binding = docker('port', NAME, '5432/tcp', capture_output=True,
                         text=True).stdout.strip()
        host, port = binding.rsplit(':', 1)
        if host != '127.0.0.1' or not port.isdecimal() or not 1 <= int(port) <= 65535:
            raise RuntimeError('private PostgreSQL fixture is not bound to loopback')
        env = dict(os.environ, GOWORK='off', GOMAXPROCS='2', GOFLAGS='-mod=readonly',
                   L07_POSTGRES_DSN=f'postgres://kl:kl@127.0.0.1:{port}/kindle_test?sslmode=disable')
        process = subprocess.Popen(
            ['go', 'test', '-p', '1', '-count=1', '-json', './db', './db/production'],
            cwd=ROOT / 'shared', env=env, start_new_session=True)
        try:
            result = process.wait(timeout=900)
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait(timeout=5)
    except subprocess.TimeoutExpired as exc:
        print(f'L07 fixture command timed out: {exc.cmd}', file=sys.stderr)
        result = 124
    except (subprocess.CalledProcessError, RuntimeError, OSError) as exc:
        print(f'L07 fixture failed: {exc}', file=sys.stderr)
        result = getattr(exc, 'returncode', 1)
    finally:
        if created:
            try:
                docker('rm', '--force', NAME, stdout=subprocess.DEVNULL)
            except (subprocess.SubprocessError, OSError) as exc:
                print(f'L07 fixture cleanup failed: {exc}', file=sys.stderr)
                result = result or 1
    return result


def phase4_main(configuration, expected_sha256):
    import copy
    import hashlib
    import importlib.util
    import json
    import shlex
    import stat
    from datetime import datetime, timezone

    def need(condition, message):
        if not condition:
            raise RuntimeError(message)

    def exact(value, fields):
        need(type(value) is dict and set(value) == set(fields), 'incomplete or unknown measurement inputs')

    def digest(value):
        import re
        need(isinstance(value, str) and re.fullmatch(r'[0-9a-f]{64}', value) and value != '0' * 64,
             'nonzero trusted source digest required')
        return value

    def bound(binding, maximum=2 * 1024 * 1024, private=False):
        exact(binding, ('path', 'sha256'))
        path = pathlib.Path(binding['path'])
        need(path.is_absolute() and path.is_file() and not any(item.is_symlink() for item in (path, *path.parents)),
             'actual absolute regular source required')
        info = path.stat()
        need(info.st_size <= maximum and stat.S_ISREG(info.st_mode), 'source input exceeds bound')
        need(not private or info.st_uid == os.getuid() and info.st_mode & 0o077 == 0, 'private owned provider input required')
        data = path.read_bytes()
        need(hashlib.sha256(data).hexdigest() == digest(binding['sha256']), 'source identity drift')
        return path, data

    def document(binding, private=False):
        _, raw = bound(binding, private=private)
        def pairs(values):
            result = {}
            for key, value in values:
                need(key not in result, 'duplicate input field')
                result[key] = value
            return result
        return json.loads(raw, object_pairs_hook=pairs)

    _, raw = bound({'path': configuration, 'sha256': expected_sha256}, private=True)
    config = json.loads(raw)
    exact(config, ('version', 'realm', 'identity', 'tools', 'topologyConfiguration', 'topologyPlan',
                   'namespaceUID', 'context', 'budget', 'postgres', 'workload', 'partition', 'azFault',
                   'restore', 'outputDirectory', 'durationSeconds', 'sampleSeconds'))
    need(config['version'] == 4 and config['realm'] == 'isolated-storage-measurement', 'isolated storage measurement authority required')
    exact(config['identity'], ('sourceSHA256', 'interfaceSHA256', 'deploymentSHA256'))
    for value in config['identity'].values():
        digest(value)
    need(type(config['durationSeconds']) is int and 60 <= config['durationSeconds'] <= 1800 and
         type(config['sampleSeconds']) is int and 1 <= config['sampleSeconds'] <= 5, 'bounded sustained sampling required')
    output = pathlib.Path(config['outputDirectory'])
    need(output.is_absolute() and output.is_dir() and output.resolve() == output and output.stat().st_mode & 0o077 == 0,
         'private precreated measurement directory required')
    run_id = uuid.uuid4().hex
    evidence = output / ('storage-' + run_id + '.jsonl')
    fd = os.open(evidence, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    stream = os.fdopen(fd, 'w')
    started = datetime.now(timezone.utc)
    monotonic_started = time.monotonic()

    def capture(action, observation):
        row = {'version': 4, 'runID': run_id, 'identity': config['identity'],
               'configurationSHA256': expected_sha256, 'at': datetime.now(timezone.utc).isoformat(),
               'monotonicNanoseconds': time.monotonic_ns(), 'action': action, 'observation': observation}
        stream.write(json.dumps(row, sort_keys=True, separators=(',', ':')) + '\n')
        stream.flush()
        os.fsync(stream.fileno())

    def interrupted(signum, _frame):
        raise SystemExit(128 + signum)
    for signum in (signal.SIGINT, signal.SIGTERM):
        signal.signal(signum, interrupted)

    provider_path = ROOT / 'deploy/production/recovery/phase2/providers.py'
    specification = importlib.util.spec_from_file_location('kindle_storage_recovery_providers', provider_path)
    recovery = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(recovery)
    sys.path.insert(0, str(ROOT / 'deploy/production/topology'))
    from phase2.providers import Kubernetes, validate_connected_plan, v1
    tools = recovery.BoundTools(config['tools'])
    topology = document(config['topologyConfiguration'])
    plan = document(config['topologyPlan'])
    validate_connected_plan(plan, topology)
    need(plan['sourceDigest'] == config['identity']['sourceSHA256'] and
         plan['interfaceDigest'] == config['identity']['interfaceSHA256'] and
         plan['deploymentDigest'] == config['identity']['deploymentSHA256'], 'measured release candidate drift')
    kube_binding = config['tools'].get('kubectl')
    need(kube_binding is not None, 'actual Kubernetes provider required')
    operator = Kubernetes(kube_binding['path'], kube_binding['sha256'], config['context'], topology['namespace'], output)
    operator.qualification_target(config['namespaceUID'])
    need(config['restore']['sourceRegion'] == topology['region'], 'recovery source region differs from observed deployment topology')
    budget = document(config['budget'])
    allocation = json.loads(tools.run('reader', ['--budget-allocation', config['budget']['path'], config['budget']['sha256']]))
    need(set(config['postgres']) == {target['id'] for target in budget['targets']}, 'all PostgreSQL members require monitor identities')
    databases = {name: recovery.PostgreSQL(tools, **binding) for name, binding in config['postgres'].items()}
    workload = config['workload']
    exact(workload, ('tool', 'configuration', 'timeoutSeconds'))
    workload_path, _ = bound(workload['configuration'], private=True)
    need(workload['tool'] in tools.tools and type(workload['timeoutSeconds']) is int and
         config['durationSeconds'] <= workload['timeoutSeconds'] <= 1800, 'actual integrated workload executable required')
    originals = {}
    hpas = {}
    child = None
    recovery_resources = None
    faulted = False
    summary = {'passed': False, 'runID': run_id, 'identity': config['identity']}

    def sessions():
        rows = {}
        for target in budget['targets']:
            database = databases[target['id']]
            observed = database.document("SELECT json_build_object('maxConnections',current_setting('max_connections')::bigint,'sessions',count(*) FILTER(WHERE backend_type='client backend'),'runtimeSessions',count(*) FILTER(WHERE backend_type='client backend' AND application_name LIKE 'kindle_%'),'observedAt',clock_timestamp()) FROM pg_stat_activity")
            approved = allocation['targets'][target['id']]
            need(observed['maxConnections'] == target['maxConnections'], 'actual PostgreSQL maximum differs from approved budget')
            need(observed['sessions'] <= approved['total'] and observed['sessions'] <= approved['ceiling'], 'aggregate sessions exceed approved fleet allocation')
            rows[target['id']] = observed
        capture('database-sessions', rows)
        return rows

    def acknowledged(identities):
        need(isinstance(identities, list) and 1 <= len(identities) <= 10000 and
             len(set(identities)) == len(identities) and all(isinstance(value, str) and 1 <= len(value) <= 512 for value in identities),
             'nonempty unique acknowledged business write identities required')
        primary = databases[config['azFault']['target']]
        values = ','.join(recovery.literal(value) for value in identities)
        observed = primary.document("SELECT coalesce(json_agg(batch_id ORDER BY batch_id),'[]'::json) FROM indexer.indexer_ingest_commits WHERE batch_id IN (" + values + ')')
        need(sorted(observed) == sorted(identities), 'lost acknowledged durable write')
        return hashlib.sha256(json.dumps(observed, separators=(',', ':')).encode()).hexdigest()

    try:
        for deployment, role in v1.RUNTIME_ROLES.items():
            clients = [client for client in budget['clients'] if client['role'] == role]
            maximum = topology['workloads'][deployment]['maxReplicas']
            need(clients and all(client['processes']['maximum'] >= maximum for client in clients), 'maximum replicas differ from approved fleet allocation')
            current = json.loads(operator._invoke(['get', 'deployment', deployment, '-o', 'json']))
            need(current['spec']['template']['metadata']['annotations'].get('kindle.io/source-sha256') == config['identity']['sourceSHA256'], 'deployed source differs from measured candidate')
            originals[deployment] = current['spec']['replicas']
            hpa = json.loads(operator._invoke(['get', 'hpa', deployment, '-o', 'json']))
            hpas[deployment] = hpa['spec']['minReplicas']
            need(hpa['spec']['maxReplicas'] == maximum, 'live maximum differs from approved topology')
            operator._invoke(['patch', 'hpa', deployment, '--type=json', '-p', json.dumps([
                {'op': 'test', 'path': '/metadata/resourceVersion', 'value': hpa['metadata']['resourceVersion']},
                {'op': 'replace', 'path': '/spec/minReplicas', 'value': maximum}])])
            operator._invoke(['scale', 'deployment/' + deployment, '--replicas=' + str(maximum)])
            operator._invoke(['rollout', 'status', 'deployment/' + deployment, '--timeout=45s'])
        capture('maximum-replicas', operator.rehearsal_snapshot())
        binding = tools.tools[workload['tool']]
        recovery.regular(binding['path'])
        need(recovery.file_sha(binding['path']) == binding['sha256'], 'workload executable changed')
        receipt_path = output / ('workload-' + run_id + '.json')
        receipt_fd = os.open(receipt_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        receipt_stream = os.fdopen(receipt_fd, 'wb')
        child = subprocess.Popen([binding['path'], '--configuration', str(workload_path), '--configuration-sha256',
                                  workload['configuration']['sha256'], '--run-id', run_id, '--duration-seconds', str(config['durationSeconds']),
                                  '--source-sha256', config['identity']['sourceSHA256'], '--interface-sha256', config['identity']['interfaceSHA256'],
                                  '--deployment-sha256', config['identity']['deploymentSHA256']], stdout=receipt_stream,
                                 stderr=subprocess.DEVNULL, start_new_session=True)
        deadline = time.monotonic() + config['durationSeconds']
        sample_count = 0
        while time.monotonic() < deadline:
            need(child.poll() is None, 'sustained business workload exited before declared duration')
            observed_pods = operator.rehearsal_snapshot()
            for deployment in v1.RUNTIME_ROLES:
                ready = sum(pod['role'] == deployment and pod['ready'] and not pod['terminating'] for pod in observed_pods['pods'])
                need(ready == topology['workloads'][deployment]['maxReplicas'], 'maximum replica population changed during measurement')
            sessions()
            sample_count += 1
            time.sleep(config['sampleSeconds'])
        exit_code = child.wait(timeout=max(1, workload['timeoutSeconds'] - config['durationSeconds']))
        receipt_stream.close()
        need(exit_code == 0, 'integrated workload failed')
        need(receipt_path.stat().st_size <= 2 * 1024 * 1024, 'raw workload receipt exceeds bound')
        receipt = json.loads(receipt_path.read_bytes())
        need(receipt['runID'] == run_id and receipt['identity'] == config['identity'] and receipt['acknowledgedWriteIDs'],
             'fresh candidate-bound workload acknowledgments required')
        acknowledged_digest = acknowledged(receipt['acknowledgedWriteIDs'])
        capture('workload', {'exitCode': exit_code, 'rawSHA256': recovery.file_sha(receipt_path), 'samples': sample_count,
                             'acknowledgedDigest': acknowledged_digest, 'acknowledgedWrites': len(receipt['acknowledgedWriteIDs'])})
        partition_result = phase4_partition(config['partition'], databases, tools, recovery, capture, need)
        fault = config['azFault']
        exact(fault, ('target', 'request'))
        need(fault['target'] in databases, 'fault primary database allocation required')
        faulted = True
        fault_start = time.monotonic()
        recovered = phase4_az(operator, topology, fault['request'], capture, need)
        faulted = False
        after_digest = acknowledged(receipt['acknowledgedWriteIDs'])
        need(after_digest == acknowledged_digest, 'AZ-loss acknowledged reconciliation changed')
        capture('az-loss', {'request': fault['request'], 'recovered': recovered, 'elapsedSeconds': time.monotonic() - fault_start,
                            'lostAcknowledgedWrites': 0, 'acknowledgedDigest': after_digest})
        restore_result = phase4_restore(config['restore'], tools, recovery, config['identity'], run_id, capture, need)
        summary.update(passed=True, sampleCount=sample_count, partition=partition_result, restore=restore_result,
                       startedAt=started.isoformat(), endedAt=datetime.now(timezone.utc).isoformat(), elapsedSeconds=time.monotonic() - monotonic_started)
    except BaseException as failure:
        capture('failure', {'errorType': type(failure).__name__, 'passed': False})
        raise
    finally:
        if child is not None and child.poll() is None:
            os.killpg(child.pid, signal.SIGTERM)
            try:
                child.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(child.pid, signal.SIGKILL)
                child.wait(timeout=5)
        for deployment, replicas in originals.items():
            if deployment in hpas:
                operator._invoke(['patch', 'hpa', deployment, '--type=merge', '-p', json.dumps({'spec': {'minReplicas': hpas[deployment]}})])
            operator._invoke(['scale', 'deployment/' + deployment, '--replicas=' + str(replicas)])
        if summary['passed']:
            capture('result', summary)
        stream.close()
    print(json.dumps({'evidencePath': str(evidence), 'evidenceSHA256': recovery.file_sha(evidence), 'result': summary}, sort_keys=True))
    return 0


def phase4_partition(config, databases, tools, provider, capture, need):
    import hashlib
    import json
    exact = lambda value, fields: need(type(value) is dict and set(value) == set(fields), 'complete reviewed partition inputs required')
    exact(config, ('target', 'schema', 'parent', 'expired', 'active', 'future', 'archive', 'object', 'archiveSHA256', 'replayWatermarks', 'maximumBytes'))
    need(config['target'] in databases and type(config['maximumBytes']) is int and config['maximumBytes'] > 0, 'partition target and byte allocation required')
    for key in ('schema', 'parent', 'expired', 'active', 'future'):
        provider.identifier(config[key])
    schema, parent = provider.identifier(config['schema']), provider.identifier(config['parent'])
    expired = schema + '.' + provider.identifier(config['expired'])
    active = schema + '.' + provider.identifier(config['active'])
    future = schema + '.' + provider.identifier(config['future'])
    database = databases[config['target']]
    observation = database.document("SELECT json_build_object('rows',(SELECT count(*) FROM " + active + "),'heapBytes',pg_relation_size(" + provider.literal(active) + "::regclass),'indexBytes',pg_indexes_size(" + provider.literal(active) + "::regclass),'totalBytes',pg_total_relation_size(" + provider.literal(active) + "::regclass),'partitions',(SELECT json_agg(relid::text) FROM pg_partition_tree(" + provider.literal(schema + '.' + parent) + '::regclass)))')
    need(observation['rows'] >= 10000000, 'real daily partition fixture contains fewer than ten million events')
    need(observation['totalBytes'] <= config['maximumBytes'], 'measured partition exceeds approved byte budget')
    for relation in (expired, active, future):
        exists = database.query('SELECT EXISTS(SELECT 1 FROM pg_partition_tree(' + provider.literal(schema + '.' + parent) + '::regclass) WHERE relid=' + provider.literal(relation) + '::regclass)')
        need(exists == 't', 'expired/active/future partition not attached to approved parent')
    archive = provider.S3Archive(tools, **config['archive'])
    archive.verify_policy()
    obj = config['object']
    archive.head(obj['key'], obj['version_id'], obj['sha256'], obj['size'])
    need(config['archiveSHA256'] == obj['sha256'], 'partition archive checksum differs from retention approval')
    need(isinstance(config['replayWatermarks'], list) and config['replayWatermarks'], 'all accepted replay source manifests required')
    replay_observations = []
    for item in config['replayWatermarks']:
        exact(item, ('generation', 'query', 'minimumHeight'))
        exact(item['query'], ('path', 'sha256'))
        query = provider.regular(item['query']['path'], 65536)
        need(provider.file_sha(query) == item['query']['sha256'], 'reviewed replay watermark query changed')
        result = database.document(query.read_text())
        need(type(item['minimumHeight']) is int and item['minimumHeight'] > 0 and
             result['generation'] == item['generation'] and result['expiredPartition'] == config['expired'] and
             type(result['minimumRetainedHeight']) is int and result['minimumRetainedHeight'] >= item['minimumHeight'] and
             result['eligible'] is True, 'actual replay/inbox source still references expired partition')
        replay_observations.append(result)
    before = database.query('SELECT count(*)::text FROM ' + active)
    database.query("BEGIN;SET LOCAL lock_timeout='2s';ALTER TABLE " + schema + '.' + parent + ' DETACH PARTITION ' + expired + ';DROP TABLE ' + expired + ';COMMIT;')
    after = database.query('SELECT count(*)::text FROM ' + active)
    need(after == before, 'rotation changed active replay/query rows')
    future_exists = database.query('SELECT EXISTS(SELECT 1 FROM pg_partition_tree(' + provider.literal(schema + '.' + parent) + '::regclass) WHERE relid=' + provider.literal(future) + '::regclass)')
    need(future_exists == 't', 'partition rotation removed future coverage')
    observation.update(expiredPartition=config['expired'], activeRowsAfter=int(after), archiveSHA256=obj['sha256'],
                       replayWatermarks=replay_observations,
                       projectedDailyBytes=((observation['heapBytes'] + observation['indexBytes']) * 10000000 + observation['rows'] - 1) // observation['rows'])
    capture('partition-rotation', observation)
    return observation


def phase4_restore(config, tools, provider, identity, run_id, capture, need):
    import json
    from datetime import datetime, timezone
    need(type(config) is dict and set(config) == {'sourceRegion', 'destinationRegion', 'tool', 'configuration', 'recoveryPoint', 'expectedSnapshot'},
         'complete cross-region restore authority required')
    need(isinstance(config['sourceRegion'], str) and isinstance(config['destinationRegion'], str) and
         config['sourceRegion'] != config['destinationRegion'], 'two independently identified recovery regions required')
    configuration = provider.regular(config['configuration']['path'], 2 * 1024 * 1024, private=True)
    need(provider.file_sha(configuration) == config['configuration']['sha256'], 'restore configuration changed')
    need(config['tool'] in tools.tools, 'actual cross-region restore provider executable unavailable')
    requested_at = datetime.now(timezone.utc)
    monotonic_started = time.monotonic()
    raw = tools.run(config['tool'], ['--native-restore', '--configuration', str(configuration), '--configuration-sha256', config['configuration']['sha256'],
                   '--run-id', run_id, '--source-sha256', identity['sourceSHA256'], '--interface-sha256', identity['interfaceSHA256'],
                   '--deployment-sha256', identity['deploymentSHA256']], timeout=1800)
    restored = json.loads(raw)
    elapsed = time.monotonic() - monotonic_started
    need(restored['runID'] == run_id and restored['identity'] == identity and restored['restored'] is True and
         restored['sourceRegion'] == config['sourceRegion'] and restored['destinationRegion'] == config['destinationRegion'],
         'fresh actual cross-region restored identity required')
    need(restored['observedSnapshot'] == config['expectedSnapshot'] and restored['activePostgresWriters'] == 1,
         'restored canonical identities, totals, outbox or writer authority differ')
    point = datetime.fromisoformat(restored['recoveryPoint'].replace('Z', '+00:00'))
    need(point.tzinfo is not None and restored['recoveryPoint'] == config['recoveryPoint'], 'observed recovery point differs from chosen timestamp')
    age = (requested_at - point).total_seconds()
    need(0 <= age <= 300 and elapsed <= 1800, 'cross-region recovery exceeds RPO300s or RTO1800s')
    result = {'requestedAt': requested_at.isoformat(), 'finishedAt': datetime.now(timezone.utc).isoformat(),
              'elapsedSeconds': elapsed, 'recoveryPointAgeSeconds': age, 'recoveryPoint': restored['recoveryPoint'],
              'sourceRegion': restored['sourceRegion'], 'destinationRegion': restored['destinationRegion'],
              'nativeResultSHA256': __import__('hashlib').sha256(raw).hexdigest()}
    capture('cross-region-restore', result)
    return result


def phase4_az(operator, topology, request, capture, need):
    import copy
    import json
    need(type(request) is dict and set(request) == {'id', 'namespaceUID', 'removedZone', 'clusterUID', 'pods', 'holdSeconds'},
         'exact database AZ fault inventory required')
    operator.qualification_target(request['namespaceUID'])
    need(request['removedZone'] in topology['zones'] and type(request['holdSeconds']) is int and 1 <= request['holdSeconds'] <= 60,
         'approved zone and bounded outage duration required')
    cluster = json.loads(operator._invoke(['get', 'cluster', 'postgres', '-o', 'json']))
    need(cluster['metadata']['uid'] == request['clusterUID'], 'fault database cluster UID changed')
    need(cluster['spec']['postgresql']['synchronous']['dataDurability'] == 'required' and
         cluster['spec']['postgresql']['synchronous']['number'] >= 1, 'required synchronous durability unavailable')
    original = copy.deepcopy(cluster['spec']['affinity'])
    snapshot = operator.rehearsal_snapshot()
    expected = []
    for pod in snapshot['pods']:
        if pod['role'] != 'postgres' or pod['terminating']:
            continue
        node = json.loads(operator._invoke(['get', 'node', pod['node'], '-o', 'json']))
        labels = node['metadata']['labels']
        need(labels.get('topology.kubernetes.io/region') == topology['region'], 'database node region differs from topology')
        if labels.get('topology.kubernetes.io/zone') == request['removedZone']:
            expected.append({'name': pod['id'], 'uid': pod['uid']})
    need(expected and sorted(expected, key=lambda item: item['name']) == sorted(request['pods'], key=lambda item: item['name']),
         'fault must include every database pod in approved lost zone')
    need(any(pod['writer'] and pod['id'] in {item['name'] for item in expected} for pod in snapshot['pods']),
         'database AZ fault must remove acknowledged-write primary authority')
    affinity = copy.deepcopy(original)
    required = affinity.setdefault('nodeAffinity', {}).setdefault('requiredDuringSchedulingIgnoredDuringExecution', {'nodeSelectorTerms': [{}]})
    for term in required['nodeSelectorTerms']:
        term.setdefault('matchExpressions', []).append({'key': 'topology.kubernetes.io/zone', 'operator': 'NotIn', 'values': [request['removedZone']]})
    changed = False
    try:
        operator._invoke(['patch', 'cluster', 'postgres', '--type=json', '-p', json.dumps([
            {'op': 'test', 'path': '/metadata/uid', 'value': request['clusterUID']},
            {'op': 'test', 'path': '/metadata/resourceVersion', 'value': cluster['metadata']['resourceVersion']},
            {'op': 'replace', 'path': '/spec/affinity', 'value': affinity}])])
        changed = True
        capture('az-fault-before', {'clusterUID': request['clusterUID'], 'zone': request['removedZone'], 'pods': expected, 'snapshot': snapshot})
        for pod in expected:
            operator._invoke(['delete', '--raw', '/api/v1/namespaces/' + operator.namespace + '/pods/' + pod['name'], '-f', '-'],
                             json.dumps({'apiVersion': 'v1', 'kind': 'DeleteOptions', 'gracePeriodSeconds': 0,
                                         'preconditions': {'uid': pod['uid']}}).encode())
        time.sleep(request['holdSeconds'])
        deadline = time.monotonic() + 120
        while True:
            recovered = operator.rehearsal_snapshot()
            capture('az-fault-sample', recovered)
            writers = [pod for pod in recovered['pods'] if pod['role'] == 'postgres' and pod['writer'] and pod['ready'] and not pod['terminating']]
            if len(writers) == 1 and writers[0]['uid'] not in {item['uid'] for item in expected}:
                node = json.loads(operator._invoke(['get', 'node', writers[0]['node'], '-o', 'json']))
                need(node['metadata']['labels']['topology.kubernetes.io/zone'] != request['removedZone'], 'writer returned in unavailable AZ')
                return recovered
            need(time.monotonic() < deadline, 'database AZ failover did not reconcile exactly one survivor writer')
            time.sleep(1)
    finally:
        if changed:
            current = json.loads(operator._invoke(['get', 'cluster', 'postgres', '-o', 'json']))
            operator._invoke(['patch', 'cluster', 'postgres', '--type=json', '-p', json.dumps([
                {'op': 'test', 'path': '/metadata/uid', 'value': request['clusterUID']},
                {'op': 'test', 'path': '/metadata/resourceVersion', 'value': current['metadata']['resourceVersion']},
                {'op': 'test', 'path': '/spec/affinity', 'value': affinity},
                {'op': 'replace', 'path': '/spec/affinity', 'value': original}])])


def phase4_native_restore():
    import argparse
    import hashlib
    import importlib.util
    import json
    import tarfile
    import urllib.request
    from datetime import datetime, timezone
    parser = argparse.ArgumentParser()
    parser.add_argument('--native-restore', action='store_true', required=True)
    for name in ('configuration', 'configuration-sha256', 'run-id', 'source-sha256', 'interface-sha256', 'deployment-sha256'):
        parser.add_argument('--' + name, required=True)
    arguments = parser.parse_args()
    def interrupted(signum, _frame):
        raise SystemExit(128 + signum)
    for signum in (signal.SIGINT, signal.SIGTERM):
        signal.signal(signum, interrupted)
    def need(condition, message):
        if not condition: raise RuntimeError(message)
    spec = importlib.util.spec_from_file_location('kindle_native_restore', ROOT / 'deploy/production/recovery/phase2/providers.py')
    provider = importlib.util.module_from_spec(spec); spec.loader.exec_module(provider)
    path = provider.regular(arguments.configuration, 2 * 1024 * 1024, private=True)
    need(provider.file_sha(path) == arguments.configuration_sha256, 'native restore authority changed')
    config = json.loads(path.read_bytes())
    need(set(config) == {'version', 'realm', 'identity', 'tools', 'sourceRegion', 'destinationRegion', 'instanceID', 'archive',
                        'baseObject', 'baseManifestSHA256', 'ownedDirectory', 'restoreDirectory', 'postgres', 'targetTime', 'timeline',
                        'port', 'walCommand', 'systemIdentifier', 'promoteRestored'}, 'complete genuine native restore inputs required')
    identity = {'sourceSHA256': arguments.source_sha256, 'interfaceSHA256': arguments.interface_sha256, 'deploymentSHA256': arguments.deployment_sha256}
    need(config['version'] == 4 and config['realm'] == 'isolated-storage-measurement' and config['identity'] == identity,
         'native restore candidate/realm differs')
    need(os.getuid() != 0, 'PostgreSQL physical restore requires actual non-root recovery identity')
    metadata = 'http://169.254.169.254/latest/'
    token_request = urllib.request.Request(metadata + 'api/token', method='PUT', headers={'X-aws-ec2-metadata-token-ttl-seconds': '60'})
    with urllib.request.urlopen(token_request, timeout=2) as response: token = response.read(4096).decode()
    request = urllib.request.Request(metadata + 'dynamic/instance-identity/document', headers={'X-aws-ec2-metadata-token': token})
    with urllib.request.urlopen(request, timeout=2) as response: actual_host = json.loads(response.read(65536))
    need(actual_host['instanceId'] == config['instanceID'] and actual_host['region'] == config['destinationRegion'] and
         actual_host['region'] != config['sourceRegion'], 'actual restore host is not in approved separate region')
    tools = provider.BoundTools(config['tools'])
    archive = provider.S3Archive(tools, **config['archive']); archive.verify_policy()
    owned = pathlib.Path(config['ownedDirectory'])
    need(owned.is_absolute() and owned.is_dir() and owned.resolve() == owned and owned.stat().st_mode & 0o077 == 0,
         'private owned isolated restore directory required')
    obj = config['baseObject']
    payload = archive.download(obj, owned / ('base-' + arguments.run_id + '.tar'))
    base = owned / ('base-' + arguments.run_id)
    base.mkdir(mode=0o700)
    with tarfile.open(payload) as contents: contents.extractall(base, filter='data')
    postgres = provider.PostgreSQL(tools, **config['postgres'])
    destination = pathlib.Path(config['restoreDirectory'])
    need(destination.parent == owned, 'physical restore output is outside approved owned directory')
    result = None
    try:
        result = postgres.restore_physical(base, destination, target_time=config['targetTime'], timeline=config['timeline'],
                                          port=config['port'], wal_command=config['walCommand'], expected_system_id=config['systemIdentifier'],
                                          expected_manifest_sha256=config['baseManifestSHA256'])
        ready = postgres.document("SELECT json_build_object('paused',pg_is_wal_replay_paused(),'recoveryPoint',pg_last_xact_replay_timestamp(),'systemIdentifier',(SELECT system_identifier::text FROM pg_control_system()))")
        need(ready['paused'] is True and ready['systemIdentifier'] == config['systemIdentifier'], 'physical timestamp recovery not reached under correct authority')
        need(config['promoteRestored'] is True, 'reviewed isolated restore promotion authority required')
        postgres.query('SELECT pg_wal_replay_resume()')
        promotion_deadline = time.monotonic() + 30
        while postgres.query('SELECT NOT pg_is_in_recovery()') != 't':
            need(time.monotonic() < promotion_deadline, 'isolated restored writer did not promote')
            time.sleep(0.1)
        recovery_query = (ROOT / 'shared/db/production/migrations.go').read_text().split('const recoverySnapshotSQL = `', 1)[1].split('`', 1)[0]
        recovery_query = recovery_query.replace("coalesce(jsonb_agg(to_jsonb(e) ORDER BY chain_id,height,transaction_index,log_index),'[]'::jsonb)::text", "encode(sha256(convert_to(coalesce(jsonb_agg(to_jsonb(e) ORDER BY chain_id,height,transaction_index,log_index),'[]'::jsonb)::text,'UTF8')),'hex')")
        recovery_query = recovery_query.replace("coalesce(jsonb_agg(to_jsonb(o) ORDER BY event_id),'[]'::jsonb)::text", "encode(sha256(convert_to(coalesce(jsonb_agg(to_jsonb(o) ORDER BY event_id),'[]'::jsonb)::text,'UTF8')),'hex')")
        import csv, io
        raw = tools.run('psql', ['-X', '--no-password', '--csv', '-t', '-v', 'ON_ERROR_STOP=1'],
                        env=postgres.environment, data=recovery_query.encode(), timeout=30).decode()
        values = next(csv.reader(io.StringIO(raw)))
        need(len(values) == 12 and values[10:] == ['0', '0'], 'restored canonical/outbox boundaries invalid')
        snapshot = dict(canonical_count=int(values[0]), canonical_sha256=values[1],
                        trade_count=int(values[2]), volume=values[3], fees=values[4], outbox_count=int(values[5]),
                        outbox_sha256=values[6], published=int(values[7]),
                        pending=int(values[8]), broker_sequence=int(values[9]))
        print(json.dumps({'runID': arguments.run_id, 'identity': identity, 'restored': True,
                          'sourceRegion': config['sourceRegion'], 'destinationRegion': actual_host['region'], 'observedSnapshot': snapshot,
                          'activePostgresWriters': 1, 'recoveryPoint': ready['recoveryPoint'], 'recoveredAt': datetime.now(timezone.utc).isoformat(),
                          'trafficCutoverAllowed': False}, sort_keys=True))
    finally:
        if result is not None:
            tools.run('pg_ctl', ['--pgdata=' + str(destination), '--wait', '--timeout=60', 'stop'], timeout=90)
    return 0


if __name__ == '__main__':
    if sys.argv[1:2] == ['--native-restore']:
        try:
            sys.exit(phase4_native_restore())
        except (RuntimeError, ValueError, OSError, KeyError, subprocess.SubprocessError):
            print('L07 native restore failed', file=sys.stderr)
            sys.exit(1)
    if sys.argv[1:2] == ['--phase4']:
        if len(sys.argv) != 4:
            raise SystemExit('phase4 requires absolute configuration and trusted SHA256')
        try:
            sys.exit(phase4_main(sys.argv[2], sys.argv[3]))
        except (RuntimeError, ValueError, KeyError, OSError, subprocess.SubprocessError) as failure:
            print('L07 measurement failed: ' + type(failure).__name__, file=sys.stderr)
            sys.exit(1)
    sys.exit(main())
