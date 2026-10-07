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

ROOT = Path(__file__).resolve().parents[3]
STAGES = ('material', 'registry-bootstrap', 'router-activation', 'routed-proof')
PUBLIC = 'https://api-mainnet-beta.paxeer.network'
REGISTRY_ENV = 'docker/platform-registry/registry.env.example'
REGISTRY_UNIT = 'docker/platform-registry/layerx-registry.service'
ENDPOINT_ENV = 'platform/hosted/gateway/railway.env.example'
ENDPOINT_FILES = 'docker/platform-gateway/files.tsv'
REGISTRY_TARGET = 'box:REGISTRY_HOST'
ENDPOINT_TARGET = 'railway:router'
REGISTRY_NAME = 'index.paxeer.network'
SCOPE = (REGISTRY_ENV, REGISTRY_UNIT, 'docker/platform-registry/init.sh', ENDPOINT_ENV, ENDPOINT_FILES, 'tools/bringup/ca.sh',
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
    info = os.lstat(path)
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o700,
            f'{path} is not a 0700 directory of this user')


def private_file(path):
    info = os.lstat(path)
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) & 0o077 == 0,
            f'{path} is not a private regular file of this user')


class Run:
    def __init__(self, state):
        self.state = state
        self.log = (state / 'commands.log').open('a')

    def script(self, label, argv, env, timeout=60):
        environment = {'PATH': os.environ.get('PATH', '/usr/bin:/bin'), 'LC_ALL': 'C'}
        environment.update(env)
        result = subprocess.run(['bash', *argv], cwd=ROOT, env=environment, capture_output=True, text=True,
                                timeout=timeout, stdin=subprocess.DEVNULL)
        self.log.write(f'## {label} argv={argv} exit={result.returncode}\n{result.stdout}{result.stderr}\n')
        self.log.flush()
        return result


def load_env(relative, files=None):
    env = {}
    for line in (ROOT / relative).read_text().splitlines():
        if not line.strip() or line.lstrip().startswith('#'):
            continue
        name, separator, value = line.partition('=')
        require(separator and re.fullmatch(r'[A-Z][A-Z0-9_]*', name), f'{relative}: malformed line {line}')
        require(name not in env, f'{relative}: {name} set twice')
        env[name] = value
    rows = []
    if files:
        for line in (ROOT / files).read_text().splitlines():
            if not line or line.startswith('#'):
                continue
            fields = line.split('\t')
            require(len(fields) == 4, f'{files}: row with {len(fields)} columns: {line}')
            rows.append({'secret_name': fields[0], 'guest_path': fields[1]})
    return {'env': env, 'files': rows}


def validate_stage(directory, stage, revision, image, generation):
    require(stage in STAGES, 'unknown deployment stage')
    require(isinstance(revision, str) and re.fullmatch(r'[0-9a-f]{40}', revision), 'candidate revision pin absent')
    require(isinstance(image, str) and re.fullmatch(r'\S+@sha256:[0-9a-f]{64}', image), 'candidate image pin absent')
    require(isinstance(generation, str) and re.fullmatch(r'[0-9a-f]{64}', generation), 'material generation pin absent')
    directory = Path(directory)
    private_dir(directory)
    previous_digest = None
    previous_finished = None
    for name in STAGES[:STAGES.index(stage) + 1]:
        path = directory / (name + '.json')
        private_file(path)
        row = json.loads(path.read_text())
        require(row.get('schema_version') == 1 and row.get('stage') == name and row.get('outcome') == 'passed',
                name + ': successful versioned stage record absent')
        require((row.get('revision'), row.get('image'), row.get('material_generation')) == (revision, image, generation),
                name + ': candidate/image/material mismatch')
        require(row.get('previous_record_sha256') == previous_digest, name + ': prerequisite record changed')
        require(isinstance(row.get('checks'), list) and row['checks'] and
                all(isinstance(check, str) and check for check in row['checks']) and
                len(row['checks']) == len(set(row['checks'])), name + ': explicit distinct checks absent')
        require(type(row.get('started_at')) is int and type(row.get('finished_at')) is int and
                0 <= row['started_at'] <= row['finished_at'] and
                (previous_finished is None or previous_finished <= row['started_at']), name + ': stage order invalid')
        previous_digest = sha256(path)
        previous_finished = row['finished_at']
    return previous_digest


def case_material_tokens(run, state, **_):
    material = state / 'owned-token-material'
    material.mkdir(mode=0o700)
    env = {'LAYERX_REGISTRY_REQUEST_TOKEN_FILE': str(material / 'tokens/request'),
           'LAYERX_REGISTRY_PUBLICATION_TOKEN_FILE': str(material / 'tokens/publication'),
           'LAYERX_REGISTRY_STATE': str(material / 'state'),
           'LAYERX_REGISTRY_JOURNAL': str(material / 'journal')}
    init = 'docker/platform-registry/init.sh'
    missing = run.script('registry-no-token-generation-during-startup', [init], env)
    require(missing.returncode == 1 and 'producer=registry-env-init--prepare-material' in missing.stderr,
            'startup did not refuse missing material before privileged setup')
    require(not Path(env['LAYERX_REGISTRY_REQUEST_TOKEN_FILE']).exists(), 'startup generated a replacement token')
    created = run.script('registry-material-producer', [init, '--prepare-material'], env)
    require(created.returncode == 0, 'production token material producer failed')
    request = Path(env['LAYERX_REGISTRY_REQUEST_TOKEN_FILE'])
    publication = Path(env['LAYERX_REGISTRY_PUBLICATION_TOKEN_FILE'])
    first, second = request.read_bytes(), publication.read_bytes()
    require(first != second and re.fullmatch(rb'[0-9a-f]{64}\n', first) and
            re.fullmatch(rb'[0-9a-f]{64}\n', second), 'producer token pair contract')
    resumed = run.script('registry-material-resume', [init, '--prepare-material'], env)
    require(resumed.returncode == 0 and (request.read_bytes(), publication.read_bytes()) == (first, second),
            'resume replaced retained credentials')
    held = publication.with_name('publication.retained')
    publication.rename(held)
    absent = run.script('registry-material-missing-retained-token', [init, '--prepare-material'], env)
    require(absent.returncode == 1 and not publication.exists() and request.read_bytes() == first,
            'partial retained material was regenerated')
    held.rename(publication)
    publication.write_bytes(first)
    collision = run.script('registry-material-token-collision', [init, '--prepare-material'], env)
    require(collision.returncode == 1, 'equal credential roles were admitted')
    publication.write_bytes(second)
    publication.rename(held)
    publication.symlink_to(held)
    link = run.script('registry-material-symlink-refusal', [init, '--prepare-material'], env)
    require(link.returncode == 1 and held.read_bytes() == second, 'linked credential material admitted or overwritten')
    publication.unlink(); held.rename(publication)
    alias = request.with_name('request.alias')
    os.link(request, alias)
    linked = run.script('registry-material-hardlink-refusal', [init, '--prepare-material'], env)
    require(linked.returncode == 1, 'multiply linked credential material admitted')
    alias.unlink()
    request.chmod(0o644)
    permissions = run.script('registry-material-token-permissions', [init, '--prepare-material'], env)
    require(permissions.returncode == 1, 'unprotected token material admitted')
    request.chmod(0o600)
    recovered = run.script('registry-material-restored-original-pair', [init, '--prepare-material'], env)
    require(recovered.returncode == 0 and (request.read_bytes(), publication.read_bytes()) == (first, second),
            'restored original material did not resume')
    return 'actual producer created distinct credentials once; startup, restart, loss, collision and permissions refused or resumed correctly'


def case_stage_candidate_binding(staged, state, **_):
    require(staged, 'staged runtime evidence absent')
    doc = staged['doc']
    directory = state / 'observed-passed-stages'
    directory.mkdir(mode=0o700)
    selected = {}
    for attempt, path in zip(doc['stages'], staged['records']):
        if attempt['outcome'] == 'passed':
            selected[attempt['stage']] = path
    require(set(selected) == set(STAGES), 'actual passed stage set incomplete')
    for name, path in selected.items():
        (directory / (name + '.json')).write_bytes(path.read_bytes())
    pins = (doc['revision'], doc['candidate']['image'], doc['material_generation'])
    validate_stage(directory, 'routed-proof', *pins)
    for index, altered in ((0, '0' * 40), (1, 'refused@sha256:' + '0' * 64), (2, '0' * 64)):
        wrong = list(pins); wrong[index] = altered
        require(tuple(wrong) != pins, 'independent stage pins cannot be all zero')
        try:
            validate_stage(directory, 'registry-bootstrap', *wrong)
        except Refused:
            continue
        raise Refused('stale candidate/image/material prerequisite accepted')
    path = directory / 'registry-bootstrap.json'
    original = path.read_bytes()
    row = json.loads(original)
    row['previous_record_sha256'] = '0' * 64
    path.write_text(json.dumps(row))
    try:
        validate_stage(directory, 'registry-bootstrap', *pins)
    except Refused:
        pass
    else:
        raise Refused('replaced material prerequisite accepted')
    path.write_bytes(original)
    require(validate_stage(directory, 'routed-proof', *pins), 'original retained stage chain did not resume')
    return 'real stage chain bound to independently selected candidate/image/material; substitutions refused and original chain retained'


def ca_rows(run):
    result = run.script('ca-services', ['tools/bringup/ca.sh', 'services'], {})
    require(result.returncode == 0, f'tools/bringup/ca.sh services exited {result.returncode}')
    rows = {}
    for line in result.stdout.splitlines():
        fields = line.split()
        require(len(fields) == 6, f'ca.sh services row has {len(fields)} fields: {line}')
        rows[fields[0]] = dict(zip(('service', 'target', 'custody', 'cn', 'eku', 'sans'), fields))
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


def case_spec_stage_graph(**_):
    text = (ROOT / 'spec/paxeer-x/spec.kvx').read_text()
    tasks = {}
    for match in re.finditer(r'(?ms)^\[task\.([0-9.]+)\]\n(.*?)(?=^\[|\Z)', text):
        dependencies = re.search(r'^requires = (\[.*\])$', match.group(2), re.M)
        require(dependencies is not None, 'task dependency declaration absent: ' + match.group(1))
        tasks[match.group(1)] = json.loads(dependencies.group(1))
    order = ('108.2.5', '108.2.6', '108.2.2', '108.2.9')
    for before, after in zip(order, order[1:]):
        require(before in tasks.get(after, []), after + ': preceding deployment stage absent')
    active, complete = set(), set()
    def visit(task):
        require(task in tasks, 'unknown prerequisite task ' + task)
        require(task not in active, 'deployment dependency cycle at ' + task)
        if task in complete:
            return
        active.add(task)
        for dependency in tasks[task]:
            visit(dependency)
        active.remove(task); complete.add(task)
    visit(order[-1])
    return 'actual material/bootstrap/router/routed-proof task graph is acyclic'


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
    comment = (ROOT / REGISTRY_ENV).read_text()
    for row in plan:
        for need in row['needs']:
            producer = row['producers'].get(need)
            require(producer, f'{row["name"]}: prerequisite {need} has no producer')
            kind, _, name = producer.partition(':')
            if kind == 'ca.sh':
                require(name in rows, f'{need}: ca.sh has no service {name}')
            elif kind == 'fly-secret':
                require(name in secrets or re.search(r'\b' + re.escape(name) + r'\b', comment),
                        f'{need}: secret {name} is declared by neither env file')
            elif kind == 'stage':
                require(STAGES.index(name) < STAGES.index(row['name']), f'{need}: producer stage {name} is not earlier')
            else:
                require(kind in ('init.sh', 'deploy'), f'{need}: unknown producer kind {kind}')
                if kind == 'init.sh':
                    require(name == '--prepare-material', f'{need}: token production must precede registry activation')
    return f'{sum(len(row["needs"]) for row in plan)} prerequisites with producers'


def case_shared_material(rows, registry, endpoint, **_):
    env = registry['env']
    for service, keys in (('registry', ('LAYERX_REGISTRY_TLS_CERT_DER', 'LAYERX_REGISTRY_TLS_KEY_DER', 'LAYERX_REGISTRY_CLIENT_CA_DER')),
                          ('registry-event-client', ('LAYERX_REGISTRY_IDENTITY_CLIENT_IDENTITY_PKCS12',
                                                     'LAYERX_REGISTRY_IDENTITY_CLIENT_IDENTITY_PASSWORD_FILE'))):
        row = rows.get(service)
        require(row and row['target'] == REGISTRY_TARGET and row['custody'] == 'volume', f'ca.sh row {service}')
        for key in keys:
            require(Path(env[key]).parent == Path('/data/tls') / service, f'{key} is not under the ca.sh {service} directory')
    gateway = rows.get('gateway-client')
    require(gateway and gateway['target'] == ENDPOINT_TARGET and gateway['eku'] == 'clientAuth', 'ca.sh row gateway-client')
    files = {item['guest_path']: item['secret_name'] for item in endpoint.get('files', [])}
    genv = endpoint['env']
    prefix = gateway['custody']
    require(files.get(genv['LAYERX_GATEWAY_CLIENT_IDENTITY_PKCS12']) == prefix + '_P12', 'gateway client identity is not the ca.sh gateway-client P12')
    require(files.get(genv['LAYERX_GATEWAY_CLIENT_IDENTITY_PASSWORD_FILE']) == prefix + '_PASSWORD', 'gateway client password is not the ca.sh gateway-client password')
    port = env['LAYERX_REGISTRY_LISTEN'].rsplit(':', 1)[1]
    require(genv['LAYERX_GATEWAY_PROGRAM_REGISTRY_URL'] == f'https://{REGISTRY_NAME}:{port}',
            'router registry URL is not the registry box ingress')
    require(files.get(genv['LAYERX_GATEWAY_PROGRAM_REGISTRY_TOKEN_FILE']), 'router registry token has no producing secret')
    require('DNS:' + REGISTRY_NAME in rows['registry']['sans'].split(','), 'registry ingress SAN absent')
    published = re.findall(r'(?:^|\s)-p (\S+)', (ROOT / REGISTRY_UNIT).read_text())
    health = env['LAYERX_REGISTRY_HEALTH_LISTEN'].rsplit(':', 1)[1]
    require(published == ['%s:%s' % (port, port), '127.0.0.1:%s:%s' % (health, health)],
            'registry must expose only its mTLS listener and a loopback health listener')
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
    for relative in (REGISTRY_ENV, ENDPOINT_ENV):
        require('.fly.dev' not in (ROOT / relative).read_text(), f'{relative} routes through a fly.dev name')
    require(staged, 'staged evidence absent: DNS API1-API16 and the routed URL are runtime facts')
    dns = staged['doc']['dns']
    for n in range(1, 17):
        name = f'API{n}'
        require(name in dns['before'] and isinstance(dns['before'][name], (str, list, dict)) and
                bool(dns['before'][name]) and dns['before'][name] == dns['after'].get(name), f'{name} DNS changed or unrecorded')
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
    record.write_text(json.dumps({'stage': 'material', 'outcome': 'passed'}))
    wrong = run.script('router-bootstrap-wrong-stage', ['tools/bringup/check-live.sh', 'router'], dict(hosts, CHECK_LIVE_STAGE_DIR=str(stages)))
    require(wrong.returncode == 1 and wrong.stdout.splitlines() == [REFUSAL], 'router accepted a different passed stage')
    return 'named refusal for unset, absent and failed registry bootstrap; record retained'


def retained_artifact(base, relative):
    require(isinstance(relative, str) and relative and not Path(relative).is_absolute()
            and '..' not in Path(relative).parts, 'retained evidence reference must remain within its private directory')
    target = base / relative
    require(not target.is_symlink() and target.resolve().is_relative_to(base.resolve()),
            'retained evidence reference escapes its private directory')
    private_file(target)
    return target


def load_staged(path, plan):
    require(path, 'staged runtime evidence absent: supply --staged-evidence or PAXEER_X_STAGED_EVIDENCE')
    path = Path(path)
    private_file(path)
    doc = json.loads(path.read_text())
    base = path.parent
    records = []
    for attempt in doc['stages']:
        record = retained_artifact(base, attempt['record'])
        require(sha256(record) == attempt['record_sha256'], f'record {attempt["record"]} changed')
        recorded = json.loads(record.read_text())
        require(recorded.get('stage') == attempt['stage'] and recorded.get('outcome') == attempt['outcome'],
                f'record {attempt["record"]} disagrees with its staged outcome')
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
        require(previous is None or previous['finished_at'] <= attempt['started_at'],
                f'{attempt["stage"]} overlapped its prerequisite or earlier attempt')
        position, previous = index, attempt
    require([attempt['stage'] for attempt in doc['stages'] if attempt['outcome'] == 'passed'] == list(STAGES),
            'each stage must pass exactly once, in order')
    return f'candidate {doc["candidate"]["image"]} {len(doc["stages"])} attempts in order'


def case_staged_inventory(staged, plan, **_):
    require(staged, 'staged runtime evidence absent')
    doc = staged['doc']
    inventory = doc['inventory']
    for key in ('services', 'images', 'volumes', 'credentials', 'prerequisites'):
        require(isinstance(inventory.get(key), list) and inventory[key], f'inventory lacks {key}')
    require(inventory['recorded_at'] <= doc['stages'][0]['started_at'], 'inventory recorded after the first deployment action')
    rows = {}
    for row in inventory['credentials'] + inventory['prerequisites']:
        require(set(row) == {'name', 'producer', 'reused'} and isinstance(row['reused'], bool),
                f'inventory row {row.get("name")} carries more than name/producer/reused')
        require(row['name'] not in rows or rows[row['name']] == row, 'conflicting inventoried prerequisite producer')
        rows[row['name']] = row
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


def build_verifier(evidence_dir, target_dir):
    private_dir(evidence_dir)
    require(Path(target_dir).is_absolute(), 'verifier target directory must be absolute')
    target = Path(target_dir).resolve()
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    tree = subprocess.check_output(['git', 'rev-parse', 'HEAD^{tree}'], cwd=ROOT, text=True).strip()
    require(subprocess.run(['git', 'diff', '--quiet', 'HEAD'], cwd=ROOT).returncode == 0,
            'verifier build requires a committed source candidate')
    require(not subprocess.check_output(['git', 'ls-files', '--others', '--exclude-standard'], cwd=ROOT).strip(),
            'verifier build refuses untracked source inputs')
    manifest = Path(evidence_dir) / 'registry-receipt-verifier.json'
    require(not manifest.exists(), 'verifier build evidence already exists; retain it')
    argv = ['cargo', 'build', '--locked', '--release', '--manifest-path', 'platform/Cargo.toml',
            '-p', 'layerx-platform-cli', '--bin', 'layerx', '--target-dir', str(target)]
    log_path = Path(evidence_dir) / 'registry-receipt-verifier-build.log'
    with log_path.open('x') as log:
        result = subprocess.run(argv, cwd=ROOT, stdout=log, stderr=subprocess.STDOUT,
                                stdin=subprocess.DEVNULL, timeout=900)
    require(result.returncode == 0, f'verifier build exited {result.returncode}; log={log_path}')
    require(subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip() == revision
            and subprocess.run(['git', 'diff', '--quiet', 'HEAD'], cwd=ROOT).returncode == 0,
            'verifier source changed during compilation')
    executable = target / 'release' / 'layerx'
    require(executable.is_file() and os.access(executable, os.X_OK), 'built layerx executable absent')
    record = {'schema_version': 1, 'producer': 'registry-router-bootstrap:build-verifier',
              'source': {'revision': revision, 'tree': tree}, 'command': argv,
              'exit_code': result.returncode, 'log_path': str(log_path),
              'artifacts': {'layerx': {'path': str(executable), 'sha256': sha256(executable)}}}
    with manifest.open('x') as output:
        json.dump(record, output, sort_keys=True)
    print(str(manifest))
    return 0


def bound_verifier(manifest_path):
    require(manifest_path, 'source-bound verifier manifest is required')
    private_file(manifest_path)
    manifest = json.loads(Path(manifest_path).read_text())
    require(manifest['schema_version'] == 1
            and manifest['producer'] == 'registry-router-bootstrap:build-verifier'
            and manifest['exit_code'] == 0, 'receipt verifier producer evidence is invalid')
    revision = manifest['source']['revision']
    require(re.fullmatch(r'[0-9a-f]{40}', revision), 'verifier revision is invalid')
    tree = subprocess.check_output(['git', 'rev-parse', revision + '^{tree}'], cwd=ROOT, text=True).strip()
    current_tree = subprocess.check_output(['git', 'rev-parse', 'HEAD^{tree}'], cwd=ROOT, text=True).strip()
    require(tree == manifest['source']['tree'] == current_tree,
            'receipt verifier does not bind the current source candidate')
    require(subprocess.run(['git', 'diff', '--quiet', 'HEAD'], cwd=ROOT).returncode == 0,
            'receipt verifier source candidate is dirty')
    artifact = manifest['artifacts']['layerx']
    executable = Path(artifact['path'])
    require(executable.is_absolute() and executable.name == 'layerx'
            and executable.is_file() and os.access(executable, os.X_OK), 'source-bound layerx executable absent')
    require(sha256(executable) == artifact['sha256'], 'source-bound layerx executable changed')
    expected = ['cargo', 'build', '--locked', '--release', '--manifest-path', 'platform/Cargo.toml',
                '-p', 'layerx-platform-cli', '--bin', 'layerx', '--target-dir', str(executable.parent.parent)]
    require(manifest['command'] == expected, 'verifier was not produced by the declared CLI build')
    return executable


def fetch_routed(path, config, state, name):
    require(config, 'private routed curl configuration is required')
    private_file(config)
    require(path.startswith('/') and '?' not in path, 'routed proof path is invalid')
    output = subprocess.run(['/usr/bin/curl', '--config', str(config), '--no-insecure', '--proto', '=https',
                             '--request', 'GET', '--no-location', '--max-redirs', '0', '--max-time', '30', '--max-filesize', '2097152',
                             '--fail', '--silent', '--show-error', '--url', PUBLIC + path],
                            stdin=subprocess.DEVNULL, capture_output=True, timeout=35)
    require(output.returncode == 0, f'public routed {name} read failed: curl exit {output.returncode}')
    require(0 < len(output.stdout) <= 2097152, f'public routed {name} response size refused')
    (state / (name + '.json')).write_bytes(output.stdout)
    return json.loads(output.stdout)


def case_routed_receipt(staged, run, state, verifier_manifest, receipt_authority, routed_curl_config, **_):
    require(staged, 'staged runtime evidence absent')
    proof = staged['doc']['receipt']
    require('argv' not in proof, 'caller-selected receipt verifier commands are forbidden')
    executable = bound_verifier(verifier_manifest)
    manifest = json.loads(Path(verifier_manifest).read_text())
    require(staged['doc']['revision'] == manifest['source']['revision'], 'staged proof and verifier source revisions differ')
    require(receipt_authority, 'independently supplied receipt authority facts are required')
    private_file(receipt_authority)
    facts = json.loads(Path(receipt_authority).read_text())
    fields = ('batch_id', 'asset', 'previous_state_root', 'resulting_state_root', 'sequencer_public_key')
    require(set(facts) == set(fields), 'independent receipt authority fact fields differ')
    for key in fields:
        require(isinstance(facts[key], str) and re.fullmatch(r'[0-9a-f]{64}', facts[key])
                and facts[key] != '0' * 64, f'independent authority {key} is invalid')
    for key in ('program_id', 'activity_id', 'receipt_digest'):
        require(re.fullmatch(r'[0-9a-f]{64}', proof[key]) and proof[key] != '0' * 64,
                f'routed proof {key} is invalid')
    receipt = retained_artifact(staged['base'], proof['receipt'])
    require(sha256(receipt) == proof['receipt_sha256'], 'receipt changed')
    require(proof['read_via'] == PUBLIC, 'receipt was not read through the router')
    registry = fetch_routed('/v1/programs/registry/' + proof['program_id'], routed_curl_config, state, 'registry-read')
    require(registry['program_id'] == proof['program_id'], 'routed registry returned a different program')
    require(registry['receipt']['deployment_receipt_digest'] == proof['receipt_digest'],
            'routed registry deployment receipt differs')
    routed = fetch_routed('/v1/receipts/' + proof['activity_id'], routed_curl_config, state, 'receipt-read')
    require(bytes.fromhex(routed['receipt']) == receipt.read_bytes(), 'routed canonical receipt differs from retained receipt')
    argv = [str(executable), '--json', 'receipt', 'verify', '--receipt', str(receipt)]
    for field in fields:
        argv.extend(['--' + field.replace('_', '-'), facts[field]])
    environment = {'PATH': os.environ.get('PATH', '/usr/bin:/bin'), 'LC_ALL': 'C'}
    result = subprocess.run(argv, cwd=staged['base'], env=environment, capture_output=True,
                            timeout=120, stdin=subprocess.DEVNULL)
    run.log.write(f'## source-bound-layerx-receipt-verifier exit={result.returncode}\n')
    require(result.returncode == 0, f'production receipt verifier exited {result.returncode}')
    result_doc = json.loads(result.stdout)
    require(result_doc['ok'] is True and result_doc['kind'] == 'receipt.verified', 'receipt verifier did not report typed success')
    verified = result_doc['data']
    require(verified['verified'] is True and verified['result_code'] == 0
            and verified['receipt_digest'] == proof['receipt_digest']
            and verified['activity_id'] == proof['activity_id']
            and verified['batch_id'] == facts['batch_id']
            and verified['canonical_bytes'] == receipt.stat().st_size,
            'verified receipt is not the exact successful routed registry receipt')
    (state / 'receipt-verification.json').write_bytes(result.stdout)
    corrupted = bytearray(receipt.read_bytes())
    require(corrupted, 'canonical receipt is empty')
    corrupted[-1] ^= 1
    negative = state / 'corrupted-receipt.bin'
    negative.write_bytes(corrupted)
    negative_argv = list(argv)
    negative_argv[negative_argv.index('--receipt') + 1] = str(negative)
    refused = subprocess.run(negative_argv, cwd=staged['base'], env=environment, capture_output=True,
                             timeout=120, stdin=subprocess.DEVNULL)
    require(refused.returncode != 0, 'production receipt verifier accepted corrupted canonical receipt bytes')
    return 'source-bound production verifier accepted the exact routed receipt and refused canonical receipt corruption'


CASES = (('plan-order', case_plan_order), ('plan-acyclic', case_plan_acyclic), ('spec-stage-graph', case_spec_stage_graph),
         ('registry-no-router-dependency', case_registry_independent), ('producer-for-every-prerequisite', case_producers),
         ('shared-material', case_shared_material), ('material-token-producer-and-recovery', case_material_tokens),
         ('candidate-bound-stages', case_stage_candidate_binding), ('no-key-material', case_no_key_material),
         ('public-interface', case_public_interface), ('router-named-missing-prerequisite', case_router_refusal),
         ('staged-evidence-order', case_staged_order), ('staged-inventory', case_staged_inventory),
         ('staged-resume', case_staged_resume), ('staged-routed-receipt', case_routed_receipt))


def main():
    parser = argparse.ArgumentParser(description='Registry identity bootstrap before router activation.')
    parser.add_argument('--evidence-dir', default=os.environ.get('PAXEER_X_EVIDENCE_DIR'))
    parser.add_argument('--staged-evidence', default=os.environ.get('PAXEER_X_STAGED_EVIDENCE'))
    parser.add_argument('--hosts-file', default=os.environ.get('BRINGUP_HOSTS_FILE'))
    parser.add_argument('--build-verifier', action='store_true')
    parser.add_argument('--verifier-target-dir', default=os.environ.get('PAXEER_X_VERIFIER_TARGET_DIR'))
    parser.add_argument('--verifier-manifest', default=os.environ.get('PAXEER_X_VERIFIER_MANIFEST'))
    parser.add_argument('--receipt-authority', default=os.environ.get('PAXEER_X_RECEIPT_AUTHORITY'))
    parser.add_argument('--routed-curl-config', default=os.environ.get('PAXEER_X_ROUTED_CURL_CONFIG'))
    parser.add_argument('--check-stage', choices=STAGES)
    parser.add_argument('--stage-dir', default=os.environ.get('CHECK_LIVE_STAGE_DIR'))
    parser.add_argument('--candidate-revision', default=os.environ.get('CHECK_LIVE_CANDIDATE_REVISION'))
    parser.add_argument('--candidate-image', default=os.environ.get('CHECK_LIVE_CANDIDATE_IMAGE'))
    parser.add_argument('--material-generation', default=os.environ.get('CHECK_LIVE_MATERIAL_GENERATION'))
    args = parser.parse_args()
    if args.check_stage:
        try:
            validate_stage(args.stage_dir, args.check_stage, args.candidate_revision, args.candidate_image, args.material_generation)
            return 0
        except (Refused, OSError, KeyError, ValueError, TypeError):
            return 1
    if not args.evidence_dir:
        print('registry-router bootstrap refused: PAXEER_X_EVIDENCE_DIR is unset', file=sys.stderr)
        return 2
    os.umask(0o077)
    private_dir(args.evidence_dir)
    if args.build_verifier:
        try:
            require(args.verifier_target_dir, 'explicit verifier target directory is required')
            return build_verifier(args.evidence_dir, args.verifier_target_dir)
        except (Refused, OSError, ValueError, subprocess.SubprocessError) as error:
            print(f'verifier build refused: {error}', file=sys.stderr)
            return 1
    state = Path(args.evidence_dir) / f'registry-router-bootstrap-{time.strftime("%Y%m%dT%H%M%SZ", time.gmtime())}-{os.getpid()}'
    state.mkdir(mode=0o700)
    run = Run(state)
    context = {'run': run, 'state': state, 'staged': None, 'hosts_file': args.hosts_file,
               'verifier_manifest': args.verifier_manifest, 'receipt_authority': args.receipt_authority,
               'routed_curl_config': args.routed_curl_config}
    failures = passed = 0
    try:
        context['plan'] = render_plan(run)
        context['rows'] = ca_rows(run)
        context['registry'] = load_env(REGISTRY_ENV)
        context['endpoint'] = load_env(ENDPOINT_ENV, ENDPOINT_FILES)
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
