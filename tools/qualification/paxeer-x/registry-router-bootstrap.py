#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time
import tomllib

ROOT = Path(__file__).resolve().parents[3]
STAGES = ('material', 'registry-bootstrap', 'router-activation', 'routed-proof')
PUBLIC = 'https://api-mainnet-beta.paxeer.network'
REGISTRY_TOML = 'platform/hosted/registry/fly.toml'
ENDPOINT_TOML = 'human/wallet/deploy/endpoint.toml'
SCOPE = (REGISTRY_TOML, 'docker/platform-registry/init.sh', ENDPOINT_TOML, 'tools/bringup/ca.sh',
         'tools/bringup/check-live.sh', 'tools/bringup/check-live.test.sh',
         'tools/qualification/paxeer-x/registry-router-bootstrap.py')
REFUSAL = 'fail router-activation missing=registry-bootstrap producer=stage:registry-bootstrap'
KEY_PATTERNS = (re.compile(rb'-----BEGIN [A-Z ]*PRIVATE KEY-----'),
                re.compile(rb'(?i)(token|password|secret|authorization|key)[A-Z_]*\s*[=:]\s*"?[0-9a-f]{64}\b'))


class Refused(Exception):
    pass


def require(condition, reason):
    if not condition:
        raise Refused(reason)


def sha256(path):
    digest = hashlib.sha256()
    with open(path, 'rb') as handle:
        for block in iter(lambda: handle.read(1 << 20), b''):
            digest.update(block)
    return digest.hexdigest()


def private_dir(path):
    info = os.stat(path)
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o700,
            f'{path} is not a 0700 directory of this user')


def private_file(path):
    info = os.lstat(path)
    require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) & 0o077 == 0,
            f'{path} is not a private regular file of this user')


class Run:
    def __init__(self, state):
        self.state = state
        self.log = (state / 'commands.log').open('a')

    def script(self, label, argv, env, timeout=60):
        environment = {'PATH': os.environ.get('PATH', '/usr/bin:/bin'), 'HOME': str(self.state), 'LC_ALL': 'C'}
        environment.update(env)
        result = subprocess.run(['bash', *argv], cwd=ROOT, env=environment, capture_output=True, text=True,
                                timeout=timeout, stdin=subprocess.DEVNULL)
        self.log.write(f'## {label} argv={argv} exit={result.returncode}\n{result.stdout}{result.stderr}\n')
        self.log.flush()
        return result


def load_toml(relative):
    with open(ROOT / relative, 'rb') as handle:
        return tomllib.load(handle)


def ca_rows(run):
    result = run.script('ca-services', ['tools/bringup/ca.sh', 'services'], {})
    require(result.returncode == 0, f'tools/bringup/ca.sh services exited {result.returncode}')
    rows = {}
    for line in result.stdout.splitlines():
        fields = line.split()
        require(len(fields) == 7, f'ca.sh services row has {len(fields)} fields: {line}')
        rows[fields[0]] = dict(zip(('service', 'toml', 'group', 'custody', 'cn', 'eku', 'sans'), fields))
    return rows


def render_plan(run):
    result = run.script('registry-plan', ['tools/bringup/check-live.sh', 'registry-plan'], {})
    require(result.returncode == 0, f'check-live.sh registry-plan exited {result.returncode}: {result.stdout.strip()}')
    plan = []
    for line in result.stdout.splitlines():
        match = re.fullmatch(r'stage (\d+) (\S+) requires=(\S+) needs=(\S+) producers=(\S+)', line)
        require(match, f'unreadable plan line: {line}')
        producers = dict(item.split(':', 1) for item in match.group(5).split(','))
        plan.append({'index': int(match.group(1)), 'name': match.group(2),
                     'requires': [] if match.group(3) == '-' else match.group(3).split(','),
                     'needs': match.group(4).split(','), 'producers': producers})
    return plan


def case_plan_order(plan, **_):
    require([row['name'] for row in plan] == list(STAGES), f'plan stages {[row["name"] for row in plan]}')
    require([row['index'] for row in plan] == [1, 2, 3, 4], 'plan stage numbering')
    return ' -> '.join(STAGES)


def case_plan_acyclic(plan, **_):
    names = [row['name'] for row in plan]
    edges = {row['name']: set(row['requires']) for row in plan}
    for row in plan:
        for need in row['requires']:
            require(need in names, f'{row["name"]} requires unknown stage {need}')
        for producer in row['producers'].values():
            if producer.startswith('stage:'):
                edges[row['name']].add(producer[6:])
    order, ready = [], [name for name in names if not edges[name]]
    pending = {name: set(deps) for name, deps in edges.items()}
    while ready:
        name = ready.pop(0)
        order.append(name)
        for other, deps in pending.items():
            if name in deps:
                deps.discard(name)
                if not deps and other not in order and other not in ready:
                    ready.append(other)
    require(order == list(STAGES), f'dependency cycle or disorder: topological order {order}')
    for row in plan:
        for need in edges[row['name']]:
            require(names.index(need) < names.index(row['name']), f'{row["name"]} depends on later stage {need}')
    return 'topological order equals stage order'


def case_registry_independent(plan, **_):
    row = next(row for row in plan if row['name'] == 'registry-bootstrap')
    tokens = set(row['requires']) | set(row['needs']) | set(row['producers'].values())
    require(not tokens & {'router-activation', 'routed-proof', 'stage:router-activation', 'stage:routed-proof'},
            f'registry bootstrap depends on the router: {sorted(tokens)}')
    require(row['requires'] == ['material'], f'registry bootstrap requires {row["requires"]}')
    router = next(row for row in plan if row['name'] == 'router-activation')
    require('registry-bootstrap' in router['requires'], 'router activation does not require registry bootstrap')
    return 'registry bootstrap requires material only'


def case_producers(plan, rows, registry, endpoint, **_):
    secrets = {item['secret_name'] for item in endpoint.get('files', [])}
    comment = (ROOT / REGISTRY_TOML).read_text()
    for row in plan:
        for need in row['needs']:
            producer = row['producers'].get(need)
            require(producer, f'{row["name"]}: prerequisite {need} has no producer')
            kind, _, name = producer.partition(':')
            if kind == 'ca.sh':
                require(name in rows, f'{need}: ca.sh has no service {name}')
            elif kind == 'fly-secret':
                require(name in secrets or re.search(r'\b' + re.escape(name) + r'\b', comment),
                        f'{need}: Fly secret {name} is declared by neither toml')
            elif kind == 'stage':
                require(STAGES.index(name) < STAGES.index(row['name']), f'{need}: producer stage {name} is not earlier')
            else:
                require(kind in ('init.sh', 'deploy'), f'{need}: unknown producer kind {kind}')
    return f'{sum(len(row["needs"]) for row in plan)} prerequisites with producers'


def case_shared_material(rows, registry, endpoint, **_):
    env = registry['env']
    for service, keys in (('registry', ('LAYERX_REGISTRY_TLS_CERT_DER', 'LAYERX_REGISTRY_TLS_KEY_DER', 'LAYERX_REGISTRY_CLIENT_CA_DER')),
                          ('registry-event-client', ('LAYERX_REGISTRY_IDENTITY_CLIENT_IDENTITY_PKCS12',
                                                     'LAYERX_REGISTRY_IDENTITY_CLIENT_IDENTITY_PASSWORD_FILE'))):
        row = rows.get(service)
        require(row and row['toml'] == REGISTRY_TOML and row['custody'] == 'volume', f'ca.sh row {service}')
        for key in keys:
            require(Path(env[key]).parent == Path('/data/tls') / service, f'{key} is not under the ca.sh {service} directory')
    gateway = rows.get('gateway-client')
    require(gateway and gateway['toml'] == ENDPOINT_TOML and gateway['eku'] == 'clientAuth', 'ca.sh row gateway-client')
    files = {item['guest_path']: item['secret_name'] for item in endpoint.get('files', [])}
    genv = endpoint['env']
    prefix = gateway['custody']
    require(files.get(genv['LAYERX_GATEWAY_CLIENT_IDENTITY_PKCS12']) == prefix + '_P12', 'gateway client identity is not the ca.sh gateway-client P12')
    require(files.get(genv['LAYERX_GATEWAY_CLIENT_IDENTITY_PASSWORD_FILE']) == prefix + '_PASSWORD', 'gateway client password is not the ca.sh gateway-client password')
    port = env['LAYERX_REGISTRY_LISTEN'].rsplit(':', 1)[1]
    require(genv['LAYERX_GATEWAY_PROGRAM_REGISTRY_URL'] == f'https://{registry["app"]}.internal:{port}',
            'router registry URL is not the registry app ingress')
    require(files.get(genv['LAYERX_GATEWAY_PROGRAM_REGISTRY_TOKEN_FILE']), 'router registry token has no producing secret')
    require('index.paxeer.network' in rows['registry']['sans'], 'registry SAN list')
    return 'registry and router consume the ca.sh identities and one internal CA'


def case_no_key_material(staged, **_):
    paths = [ROOT / relative for relative in SCOPE]
    if staged:
        paths.append(staged['path'])
        paths.extend(staged['records'])
    for path in paths:
        data = Path(path).read_bytes()
        for pattern in KEY_PATTERNS:
            require(not pattern.search(data), f'key material pattern in {path}')
    return f'{len(paths)} files hold no key material'


def case_public_interface(registry, endpoint, staged, **_):
    probe = (ROOT / 'tools/bringup/check-live.sh').read_text()
    require(f'local url={PUBLIC} ' in probe, 'check_router no longer targets the public unified interface')
    for relative in (REGISTRY_TOML, ENDPOINT_TOML):
        require('.fly.dev' not in (ROOT / relative).read_text(), f'{relative} routes through a fly.dev name')
    require(staged, 'staged evidence absent: DNS API1-API16 and the routed URL are runtime facts')
    dns = staged['doc']['dns']
    for n in range(1, 17):
        name = f'API{n}'
        require(name in dns['before'] and dns['before'][name] == dns['after'].get(name), f'{name} DNS changed or unrecorded')
    require(staged['doc']['routed_url'] == PUBLIC, 'routed proof did not use the public unified interface')
    return 'public interface and API1-API16 DNS unchanged'


def case_router_refusal(run, state, hosts_file, **_):
    require(hosts_file, 'operator host map absent: supply --hosts-file or BRINGUP_HOSTS_FILE (check-live.sh router loads it first)')
    private_file(hosts_file)
    hosts = {'BRINGUP_HOSTS_FILE': str(hosts_file)}
    unset = run.script('router-stage-dir-unset', ['tools/bringup/check-live.sh', 'router'], hosts)
    require(unset.returncode == 2 and 'check-live: CHECK_LIVE_STAGE_DIR is unset' in unset.stderr.splitlines(),
            f'router without CHECK_LIVE_STAGE_DIR exited {unset.returncode}')
    stages = state / 'router-refusal'
    stages.mkdir(mode=0o700)
    absent = run.script('router-bootstrap-absent', ['tools/bringup/check-live.sh', 'router'], dict(hosts, CHECK_LIVE_STAGE_DIR=str(stages)))
    require(absent.returncode == 1 and absent.stdout.splitlines() == [REFUSAL], f'absent bootstrap record: exit {absent.returncode}')
    record = stages / 'registry-bootstrap.json'
    failed_record = json.dumps({'stage': 'registry-bootstrap', 'outcome': 'failed'})
    record.write_text(failed_record)
    failed = run.script('router-bootstrap-failed', ['tools/bringup/check-live.sh', 'router'], dict(hosts, CHECK_LIVE_STAGE_DIR=str(stages)))
    require(failed.returncode == 1 and failed.stdout.splitlines() == [REFUSAL], f'failed bootstrap record: exit {failed.returncode}')
    require(record.read_text() == failed_record, 'the probe changed the stage record')
    return 'named refusal for unset, absent and failed registry bootstrap; record retained'


def load_staged(path, plan):
    require(path, 'staged runtime evidence absent: supply --staged-evidence or PAXEER_X_STAGED_EVIDENCE')
    path = Path(path)
    private_file(path)
    doc = json.loads(path.read_text())
    base = path.parent
    records = []
    for attempt in doc['stages']:
        record = (base / attempt['record']).resolve()
        private_file(record)
        require(sha256(record) == attempt['record_sha256'], f'record {attempt["record"]} changed')
        records.append(record)
    return {'path': path, 'doc': doc, 'records': records, 'base': base}


def case_staged_order(staged, plan, **_):
    require(staged, 'staged runtime evidence absent')
    doc = staged['doc']
    head = subprocess.run(['git', '-C', str(ROOT), 'merge-base', '--is-ancestor', doc['revision'], 'HEAD'])
    require(re.fullmatch(r'[0-9a-f]{40}', doc['revision']) and head.returncode == 0, 'evidence revision is not an ancestor of HEAD')
    require(re.fullmatch(r'\S+@sha256:[0-9a-f]{64}', doc['candidate']['image']), 'candidate image is not pinned by digest')
    position, previous = -1, None
    for attempt in doc['stages']:
        index = STAGES.index(attempt['stage'])
        if index != position:
            require(index == position + 1, f'stage {attempt["stage"]} out of order')
            require(previous is None or previous['outcome'] == 'passed', f'{attempt["stage"]} started before {STAGES[position]} passed')
        else:
            require(previous['outcome'] == 'failed', f'{attempt["stage"]} repeated after it passed')
        require(attempt['outcome'] in ('passed', 'failed'), f'{attempt["stage"]} outcome {attempt["outcome"]}')
        require(attempt['started_at'] <= attempt['finished_at'], f'{attempt["stage"]} times')
        position, previous = index, attempt
    require([attempt['stage'] for attempt in doc['stages'] if attempt['outcome'] == 'passed'] == list(STAGES),
            'each stage must pass exactly once, in order')
    return f'candidate {doc["candidate"]["image"]} {len(doc["stages"])} attempts in order'


def case_staged_inventory(staged, plan, **_):
    require(staged, 'staged runtime evidence absent')
    doc = staged['doc']
    inventory = doc['inventory']
    for key in ('services', 'images', 'volumes', 'credentials', 'prerequisites'):
        require(isinstance(inventory.get(key), list), f'inventory lacks {key}')
    require(inventory['recorded_at'] <= doc['stages'][0]['started_at'], 'inventory recorded after the first deployment action')
    rows = {}
    for row in inventory['credentials'] + inventory['prerequisites']:
        require(set(row) == {'name', 'producer', 'reused'} and isinstance(row['reused'], bool),
                f'inventory row {row.get("name")} carries more than name/producer/reused')
        rows.setdefault(row['name'], row)
    for stage in plan:
        for need in stage['needs']:
            producer = stage['producers'][need]
            if producer.startswith('stage:'):
                continue
            require(need in rows and rows[need]['producer'] == producer, f'{need} not inventoried with producer {producer}')
    return f'{len(rows)} inventoried items with producers'


def case_staged_resume(staged, **_):
    require(staged, 'staged runtime evidence absent')
    attempts = staged['doc']['stages']
    for before, after in zip(attempts, attempts[1:]):
        if before['outcome'] == 'failed':
            require(after['stage'] == before['stage'] and after['attempt'] == before['attempt'] + 1,
                    f'{before["stage"]} attempt {before["attempt"]} failed and was not resumed')
            require(set(before['checks']) <= set(after['checks']), f'{before["stage"]} resumed with fewer checks')
            require(after['state'] == before['state'], f'{before["stage"]} resumed on a different retained state')
    for record in staged['records']:
        require(record.is_file(), f'retained record {record} discarded')
    return f'{sum(a["outcome"] == "failed" for a in attempts)} failed attempts resumed with state and checks kept'


def case_routed_receipt(staged, run, **_):
    require(staged, 'staged runtime evidence absent')
    proof = staged['doc']['receipt']
    executable = Path(proof['argv'][0])
    require(executable.is_absolute() and executable.is_file() and os.access(executable, os.X_OK), 'receipt verifier is not a prebuilt executable')
    require(sha256(executable) == proof['executable_sha256'], 'receipt verifier digest')
    receipt = (staged['base'] / proof['receipt']).resolve()
    private_file(receipt)
    require(sha256(receipt) == proof['receipt_sha256'], 'receipt changed')
    result = subprocess.run(proof['argv'], cwd=staged['base'], capture_output=True, timeout=120, stdin=subprocess.DEVNULL)
    run.log.write(f'## receipt-verifier exit={result.returncode}\n')
    require(result.returncode == 0, f'receipt verifier exited {result.returncode}')
    require(hashlib.sha256(result.stdout).hexdigest() == proof['stdout_sha256'], 'receipt verifier output differs from the staged proof')
    require(proof['read_via'] == PUBLIC, 'receipt was not read through the router')
    return 'signed registry receipt read through the router verifies'


CASES = (('plan-order', case_plan_order), ('plan-acyclic', case_plan_acyclic),
         ('registry-no-router-dependency', case_registry_independent), ('producer-for-every-prerequisite', case_producers),
         ('shared-material', case_shared_material), ('no-key-material', case_no_key_material),
         ('public-interface', case_public_interface), ('router-named-missing-prerequisite', case_router_refusal),
         ('staged-evidence-order', case_staged_order), ('staged-inventory', case_staged_inventory),
         ('staged-resume', case_staged_resume), ('staged-routed-receipt', case_routed_receipt))


def main():
    parser = argparse.ArgumentParser(description='Registry identity bootstrap before router activation.')
    parser.add_argument('--evidence-dir', default=os.environ.get('PAXEER_X_EVIDENCE_DIR'))
    parser.add_argument('--staged-evidence', default=os.environ.get('PAXEER_X_STAGED_EVIDENCE'))
    parser.add_argument('--hosts-file', default=os.environ.get('BRINGUP_HOSTS_FILE'))
    args = parser.parse_args()
    if not args.evidence_dir:
        print('registry-router bootstrap refused: PAXEER_X_EVIDENCE_DIR is unset', file=sys.stderr)
        return 2
    private_dir(args.evidence_dir)
    state = Path(args.evidence_dir) / f'registry-router-bootstrap-{time.strftime("%Y%m%dT%H%M%SZ", time.gmtime())}-{os.getpid()}'
    state.mkdir(mode=0o700)
    run = Run(state)
    context = {'run': run, 'state': state, 'staged': None, 'hosts_file': args.hosts_file}
    failures = passed = 0
    try:
        context['plan'] = render_plan(run)
        context['rows'] = ca_rows(run)
        context['registry'] = load_toml(REGISTRY_TOML)
        context['endpoint'] = load_toml(ENDPOINT_TOML)
    except (Refused, OSError, KeyError, ValueError, subprocess.SubprocessError) as error:
        print(f'FAIL inputs {error}', flush=True)
        print('PAXEER_X_GATE tests=0 skipped=0', flush=True)
        return 1
    try:
        context['staged'] = load_staged(args.staged_evidence, context['plan'])
    except (Refused, OSError, KeyError, ValueError, TypeError) as error:
        print(f'FAIL staged-evidence-required {error}', flush=True)
        failures += 1
    for name, case in CASES:
        try:
            detail = case(**context)
            print(f'PASS {name} {detail}', flush=True)
            passed += 1
        except (Refused, OSError, KeyError, ValueError, TypeError, IndexError, StopIteration, subprocess.SubprocessError) as error:
            print(f'FAIL {name} {error}', flush=True)
            failures += 1
    total = passed + failures
    (state / 'result.json').write_text(json.dumps({'tests': total, 'passed': passed, 'failed': failures, 'skipped': 0}))
    print(f'PAXEER_X_GATE tests={total} skipped=0', flush=True)
    return 0 if failures == 0 else 1


if __name__ == '__main__':
    sys.exit(main())
