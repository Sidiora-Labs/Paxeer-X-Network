#!/usr/bin/env python3
import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import signal
import ssl
import stat
import subprocess
import sys
import time
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[3]
MAX_BYTES = 8 * 1024 * 1024
DEADLINE = time.monotonic() + 1700
COUNT = 0


def require(value, message):
    if not value:
        raise RuntimeError(message)


def check(value, message):
    global COUNT
    require(value, message)
    COUNT += 1


def remaining():
    value = DEADLINE - time.monotonic()
    require(value > 0, 'source boundary gate deadline')
    return value


def private(value, directory=False):
    path = Path(value)
    require(path.is_absolute() and path.resolve() == path
            and not any(part == '.env' or part.startswith('.env.') for part in path.parts),
            'private absolute non-environment path required')
    info = path.stat()
    require(info.st_uid == os.geteuid() and info.st_mode & 0o077 == 0,
            'private material ownership or permissions')
    require(stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode),
            'private material type')
    return path


def load(path):
    path = private(path)
    require(path.stat().st_size <= MAX_BYTES, 'private input bound')
    def unique(items):
        result = {}
        for key, value in items:
            require(key not in result, 'duplicate input key')
            result[key] = value
        return result
    return json.loads(path.read_bytes(), object_pairs_hook=unique)


def digest(path):
    result = hashlib.sha256()
    with Path(path).open('rb') as handle:
        for chunk in iter(lambda: handle.read(1048576), b''):
            result.update(chunk)
    return result.hexdigest()


def git(*args):
    return subprocess.run(['git', '-C', str(ROOT), *args], check=True,
                          capture_output=True, timeout=min(60, remaining())).stdout


def identity():
    revision = git('rev-parse', 'HEAD').decode().strip()
    value = hashlib.sha256()
    names = git('ls-files', '-z').decode().split('\0')
    for name in sorted(name for name in names if name and
                       (name.endswith(('.rs', '.c', '.h', '.cpp', '.proto', '.kvx', '.ts', '.tsx'))
                        or Path(name).name in ('Cargo.toml', 'Cargo.lock', 'build.rs'))):
        path = ROOT / name
        require(path.is_file() and not path.is_symlink(), 'candidate source inventory')
        value.update(name.encode() + b'\0' + digest(path).encode() + b'\n')
    return revision, value.hexdigest()


def executable(row, revision, source):
    path = Path(row['path'])
    require(path.is_absolute() and path.is_file() and not path.is_symlink()
            and os.access(path, os.X_OK), 'prebuilt executable required')
    require(row['source_revision'] == revision and row['source_digest'] == source
            and row['build_exit'] == 0 and digest(path) == row['sha256'],
            'executable source binding')
    return path


def endpoint(value):
    url = urlsplit(value)
    require(url.scheme == 'https' and url.hostname in ('localhost', '127.0.0.1')
            and url.port and not url.username and not url.password
            and url.path in ('', '/') and not url.query and not url.fragment,
            'isolated loopback HTTPS endpoint required')
    return url


def request(url, context, method, path, headers=None, body=None):
    trace = 'trc_' + os.urandom(16).hex()
    supplied = {'X-LayerX-Trace': trace, 'Accept': 'application/json', **(headers or {})}
    if body is not None:
        supplied['Content-Type'] = 'application/json'
        body = json.dumps(body, separators=(',', ':')).encode()
    connection = http.client.HTTPSConnection(url.hostname, url.port, context=context,
                                            timeout=min(20, remaining()))
    try:
        connection.request(method, path, body=body, headers=supplied)
        response = connection.getresponse()
        raw = response.read(MAX_BYTES + 1)
        require(len(raw) <= MAX_BYTES, 'HTTP response bound')
        data = json.loads(raw)
        check(response.getheader('X-LayerX-Trace') == trace and data.get('trace') == trace,
              'end-to-end HTTP trace')
        check(response.getheader('Cache-Control') == 'no-store', 'private response caching')
        if data.get('ok') is False:
            error = data.get('error', {})
            check(isinstance(error.get('code'), str) and isinstance(error.get('copy_key'), str)
                  and error.get('retry') in ('retriable', 'retriable-after', 'structural', 'final'),
                  'typed error shape')
            if error['retry'] == 'retriable-after':
                check(type(error.get('retry_after_ms')) is int and error['retry_after_ms'] > 0,
                      'typed retry timing')
        else:
            check(data.get('ok') is True and 'result' in data, 'success envelope')
        return response.status, data
    finally:
        connection.close()


CLIENT = r'''
import { readFile, writeFile } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';
import { randomBytes } from 'node:crypto';
const [root, inputPath, resultPath] = process.argv.slice(2);
const { HumanApiError, operationNames, schemaVersion } =
  await import(pathToFileURL(root + '/human/apps/web/src/api/generated/index.ts').href);
const { humanApi } = await import(pathToFileURL(root + '/human/apps/web/src/api/index.ts').href);
const { default: webConfiguration } = await import(pathToFileURL(root + '/human/apps/web/next.config.mjs').href);
const { conformance } = await import(pathToFileURL(root + '/human/apps/web/src/api/generated/conformance.ts').href);
const fixture = JSON.parse(await readFile(inputPath, 'utf8'));
let tests = 0;
function check(ok, message) { if (!ok) throw new Error(message); tests += 1; }
const rewrites = await webConfiguration.rewrites();
const browserRewrite = rewrites.find((row) => row.source === '/human/v1/:path*');
check(browserRewrite?.destination === new URL('/v1/:path*', fixture.url).href,
  'browser Human API mount reaches the production service route');
check(rewrites.some((row) => row.source === '/v1/:path*'
  && row.destination === browserRewrite.destination), 'existing direct service route retained');
const outputs = new Map();
const successful = new Set();
let balanceTrace;
const sessions = new Map(Object.entries(fixture.sessions).map(([name, row]) => [name, new Map(Object.entries(row.cookies))]));
function resolve(value) {
  if (Array.isArray(value)) return value.map(resolve);
  if (value && typeof value === 'object') {
    if (Object.keys(value).length === 1 && typeof value.$result === 'string') {
      const [id, ...parts] = value.$result.split('/');
      let found = outputs.get(id);
      for (const part of parts) found = found?.[part];
      if (found === undefined) throw new Error('unresolved genuine operation result');
      return found;
    }
    return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, resolve(item)]));
  }
  return value;
}
function clientFor(principal) {
  const jar = sessions.get(principal);
  if (!jar) throw new Error('unknown fixture principal');
  return humanApi({
    csrfToken: () => jar.get('__Host-layerx_csrf'),
    trace: () => 'trc_' + randomBytes(16).toString('hex'),
    fetch: async (input, init) => {
      check(input.startsWith('/human/v1/'), 'browser wrapper retains the public Human API mount');
      const destination = browserRewrite.destination.replace(':path*', input.slice('/human/v1/'.length));
      const headers = new Headers(init.headers);
      headers.set('Origin', fixture.origin);
      headers.set('Cookie', [...jar].map(([name, value]) => name + '=' + value).join('; '));
      const response = await fetch(destination, { ...init, headers, signal: AbortSignal.timeout(20000), redirect: 'error' });
      const envelope = await response.clone().json();
      if (new URL(destination).pathname === '/v1/account/balance') balanceTrace = envelope.trace;
      check(envelope.trace === headers.get('X-LayerX-Trace')
        && response.headers.get('X-LayerX-Trace') === envelope.trace, 'generated-client trace mismatch');
      for (const cookie of response.headers.getSetCookie()) {
        const [pair, ...attributes] = cookie.split(';').map((part) => part.trim());
        const equals = pair.indexOf('=');
        const name = pair.slice(0, equals), value = pair.slice(equals + 1);
        check(name.startsWith('__Host-layerx_') && attributes.includes('Secure')
          && attributes.includes('Path=/') && attributes.includes('SameSite=Strict')
          && (name === '__Host-layerx_csrf' || attributes.includes('HttpOnly')),
          'browser cookie security');
        jar.set(name, value);
      }
      return response;
    },
  });
}
const clients = new Map([...sessions.keys()].map((name) => [name, clientFor(name)]));
const version = await clients.get(fixture.principal).version();
const startingPosition = await clients.get(fixture.principal).streamOpen();
check(version.schema.major === schemaVersion.major && version.schema.minor === schemaVersion.minor, 'live schema/client version parity');
check(Array.isArray(fixture.cases) && fixture.cases.length <= 1024, 'bounded genuine request inventory');
for (const row of fixture.cases) {
  check(typeof row.id === 'string' && !outputs.has(row.id) && operationNames.includes(row.operation), 'unique known operation case');
  const client = clients.get(row.principal);
  check(client !== undefined, 'case principal');
  const run = { client, params: resolve(row.params ?? {}),
    ...(row.body === undefined ? {} : { body: resolve(row.body) }),
    ...(row.idempotencyKey === undefined ? {} : { idempotencyKey: row.idempotencyKey }) };
  if (row.error !== undefined) {
    let failure;
    try { await conformance[row.operation](run); } catch (error) { failure = error; }
    check(failure instanceof HumanApiError && failure.detail.code === row.error.code
      && failure.detail.retry === row.error.retry && failure.status === row.error.status,
      'real typed operation refusal');
    outputs.set(row.id, { error: failure.detail });
  } else {
    const result = await conformance[row.operation](run);
    outputs.set(row.id, result);
    successful.add(row.operation);
    if (row.operation === 'agent.create') {
      check(result.kind === 'agent-create' && result.stages.length > 0
        && Number.isFinite(Date.parse(result.started_at))
        && Number.isFinite(Date.parse(result.updated_at))
        && Date.parse(result.updated_at) >= Date.parse(result.started_at),
        'canonical genuine creation journey timestamps and stages');
      let recovered = await client.journeyGet(result.journey_id);
      check(recovered.journey_id === result.journey_id && recovered.kind === result.kind,
        'principal-owned creation journey recovery');
      const recoveryDeadline = Date.now() + 60000;
      while (!['done', 'done-finalised', 'refused'].includes(recovered.state)
        && Date.now() < recoveryDeadline) {
        await new Promise((resolve) => setTimeout(resolve, 250));
        recovered = await client.journeyGet(result.journey_id);
      }
      check(['done', 'done-finalised'].includes(recovered.state),
        'background creation owner completes without another economic POST');
      let page = await client.journeyList();
      const listed = [];
      const cursors = new Set();
      for (;;) {
        listed.push(...page.journeys);
        if (page.next_cursor === 'cur_end') break;
        check(!cursors.has(page.next_cursor) && cursors.size < 1024,
          'creation pagination cursor advances within genuine fixture bounds');
        cursors.add(page.next_cursor);
        page = await client.journeyPage(page.next_cursor);
      }
      check(listed.filter((journey) => journey.journey_id === recovered.journey_id).length === 1,
        'creation journey participates exactly once in canonical snapshot pagination');
      const settled = recovered.state === 'done' || recovered.state === 'done-finalised';
      if (settled) {
        check(recovered.stages.every((stage) => ['done', 'done-finalised'].includes(stage.state)
          && stage.evidence.some((reference) => reference.class === 'layerx-receipt'
            && ['receipt-verified', 'checkpoint-finalised', 'settlement-anchored'].includes(reference.verification))),
          'every completed creation stage retains genuine receipt backing');
        const protection = recovered.stages.find((stage) => stage.stage_id === 'stg_agent_create_4');
        check(protection !== undefined && protection.evidence.some((reference) =>
          reference.class === 'local-journey-state' && reference.verification === 'unverified'),
          'capability evidence remains explicitly local in the receipt-backed protection stage');
      }
      for (const reference of recovered.evidence) {
        const material = await client.evidenceGet(reference.evidence_id);
        check(material.evidence_id === reference.evidence_id && material.class === reference.class
          && material.verification === reference.verification
          && Buffer.from(material.bytes_base64, 'base64').length > 0,
          'creation evidence exports its actual producer bytes and verification');
      }
      const foreign = [...clients.entries()].find(([principal]) => principal !== row.principal);
      check(foreign !== undefined,
        'creation isolation requires a distinct authenticated principal');
      let denied;
      try { await foreign[1].journeyGet(result.journey_id); } catch (error) { denied = error; }
      check(denied instanceof HumanApiError && ['not-found', 'forbidden'].includes(denied.detail.code),
        'cross-principal creation journey recovery refused');
    }
    if (row.replay === true) {
      check(typeof row.idempotencyKey === 'string' && typeof result.journey_id === 'string', 'economic replay fixture');
      const replay = await conformance[row.operation](run);
      check(replay.journey_id === result.journey_id, 'idempotency retains original journey');
    }
  }
}
check(operationNames.every((operation) => successful.has(operation)), 'every schema operation exercised through real generated client');
check(fixture.cases.some((row) => row.replay === true), 'economic idempotency acceptance absent');
const client = clients.get(fixture.principal);
const balance = await client.accountBalance();
check(['receipt-verified', 'checkpoint-finalised', 'settlement-anchored'].includes(balance.verification)
  && balance.evidence.length > 0 && balance.freshness.source_head.length > 0
  && Number.isSafeInteger(balance.freshness.age_seconds) && balance.freshness.age_seconds >= 0,
  'receipt-backed balance and freshness');
for (const reference of balance.evidence) {
  const evidence = await client.evidenceGet(reference.evidence_id);
  check(evidence.evidence_id === reference.evidence_id && evidence.verification === reference.verification
    && Buffer.from(evidence.bytes_base64, 'base64').length > 0, 'real evidence readback');
}
const position = startingPosition;
const seen = new Set();
const kinds = new Set();
let cursor = position.cursor;
let caughtUp = false;
for (let page = 0; page < 256; page += 1) {
  const first = await client.streamNext(cursor);
  const reconnect = await client.streamNext(cursor);
  check(first.events.every((event, index) => event.cursor === reconnect.events[index]?.cursor), 'stable stream replay cursor');
  for (const event of first.events) {
    check(!seen.has(event.cursor), 'stream duplicate cursor');
    seen.add(event.cursor);
    kinds.add(event.kind);
  }
  if (first.next_cursor === cursor) {
    check(first.events.length === 0, 'unchanged cursor must be empty');
    caughtUp = true;
    break;
  }
  check(first.events.length > 0 && first.events.at(-1).cursor === first.next_cursor, 'stream continuation position');
  cursor = first.next_cursor;
}
check(caughtUp && kinds.has('journey-progress') && kinds.has('notification')
  && [...kinds].some((kind) => kind.startsWith('approval-')), 'real journey approval notification stream coverage');
const other = clients.get(fixture.other_principal);
check(other !== undefined && fixture.other_principal !== fixture.principal, 'distinct principal fixture');
let foreign;
try { await other.streamNext(position.cursor); } catch (error) { foreign = error; }
check(foreign instanceof HumanApiError && ['forbidden', 'invalid-request'].includes(foreign.detail.code), 'cross-principal stream refusal');
await writeFile(resultPath, JSON.stringify({ tests, balance: { amount: balance.money.amount.toString(),
  currency: balance.money.currency, source_head: balance.freshness.source_head,
  verification: balance.verification }, balance_trace: balanceTrace, cookies: Object.fromEntries(sessions.get(fixture.principal)) }), { mode: 0o600 });
'''


class Processes:
    def __init__(self, directory, artifacts):
        self.directory, self.artifacts, self.children = directory, artifacts, {}

    def start(self, row):
        name = row['name']
        require(re.fullmatch('[a-z][a-z0-9-]{0,63}', name) and name not in self.children,
                'unique production process name')
        path = self.artifacts[row['artifact']]
        log = (self.directory / (name + '.log')).open('xb')
        os.chmod(log.name, 0o600)
        env = dict(os.environ, **row['environment'])
        require(all(isinstance(value, str) for value in row['environment'].values()), 'process environment')
        child = subprocess.Popen([str(path), *row.get('arguments', [])], cwd=ROOT,
                                 env=env, stdin=subprocess.DEVNULL, stdout=log, stderr=log,
                                 start_new_session=True)
        self.children[name] = (child, log, row)
        return child

    def stop(self, name):
        child, log, row = self.children.pop(name)
        if child.poll() is None:
            os.killpg(child.pid, signal.SIGTERM)
            try:
                child.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(child.pid, signal.SIGKILL)
                child.wait(timeout=10)
        log.close()
        return row

    def close(self):
        for child, _, _ in self.children.values():
            if child.poll() is None:
                os.killpg(child.pid, signal.SIGTERM)
        until = time.monotonic() + 10
        for child, log, _ in self.children.values():
            try:
                child.wait(timeout=max(0.01, until - time.monotonic()))
            except subprocess.TimeoutExpired:
                os.killpg(child.pid, signal.SIGKILL)
                child.wait(timeout=2)
            log.close()
        self.children.clear()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--manifest', required=True)
    args = parser.parse_args()
    material = load(args.manifest)
    require(material['schema'] == 'layerx-human-api-boundary.v1', 'manifest version')
    revision, source = identity()
    directory = private(material['evidence_directory'], True)
    require(not any(directory.iterdir()), 'fresh private evidence directory required')
    artifacts = {name: executable(row, revision, source) for name, row in material['artifacts'].items()}
    for required in ('service', 'components', 'agent', 'core', 'paxeer', 'schema_check', 'api_gen'):
        require(required in artifacts, 'production artifact missing: ' + required)
    node = Path(material['node'])
    require(node.is_absolute() and node.is_file() and os.access(node, os.X_OK)
            and digest(node) == material['node_sha256'], 'pinned Node runtime')
    ca = private(material['ca_pem'])
    context = ssl.create_default_context(cafile=str(ca))
    url = endpoint(material['url'])
    require(urlsplit(material['origin']).scheme == 'https', 'browser HTTPS origin')
    for label, argv in (
        ('schema', [artifacts['schema_check'], ROOT / 'human/schema/human-api']),
        ('generated-drift', [artifacts['api_gen'], '--check', ROOT / 'human/schema/human-api',
                             ROOT / 'human/apps/web/src/api/generated']),
    ):
        with (directory / (label + '.log')).open('xb') as log:
            os.chmod(log.name, 0o600)
            result = subprocess.run([str(arg) for arg in argv], cwd=ROOT, stdout=log, stderr=log,
                                    timeout=min(60, remaining()))
            check(result.returncode == 0, label + ' gate failed')
    processes = Processes(directory, artifacts)
    try:
        require(5 <= len(material['processes']) <= 32, 'bounded real process inventory')
        by_role = {row['artifact']: row for row in material['processes']}
        require(all(role in by_role for role in ('service', 'components', 'agent', 'core', 'paxeer')),
                'real production process roles')
        service = by_role['service']['environment']
        require(service['LAYERX_HUMAN_LISTENER'] == 'tls'
                and service['LAYERX_HUMAN_BIND'] == '127.0.0.1:' + str(url.port)
                and service['LAYERX_HUMAN_WEB_ORIGIN'] == material['origin'],
                'production HTTPS service configuration')
        private(service['LAYERX_HUMAN_TLS_CERT_DER'])
        private(service['LAYERX_HUMAN_TLS_KEY_DER'])
        for row in material['processes']:
            processes.start(row)
        expires = time.monotonic() + min(90, remaining())
        while True:
            require(all(child.poll() is None for child, _, _ in processes.children.values()),
                    'production process exited during startup')
            try:
                status, ready = request(url, context, 'GET', '/readyz')
                if status == 200 and ready.get('result', {}).get('ready') is True:
                    break
            except (OSError, http.client.HTTPException):
                pass
            require(time.monotonic() < expires, 'production readiness deadline')
            time.sleep(0.1)
        check(ready['result']['components'] == dict.fromkeys(
            ('human_service', 'custody', 'agent', 'core', 'paxeer'), 'ready'), 'redacted readiness components')
        status, live = request(url, context, 'GET', '/livez')
        check(status == 200 and live['result']['live'] is True, 'liveness')
        status, failure = request(url, context, 'GET', '/v1/account/balance')
        check(status == 401 and failure['error']['code'] == 'unauthenticated', 'unauthenticated account refusal')
        runner = directory / 'generated-client.mts'
        runner.write_text(CLIENT)
        os.chmod(runner, 0o600)
        output = directory / 'client-result.json'
        with (directory / 'generated-client.log').open('xb') as log:
            os.chmod(log.name, 0o600)
            result = subprocess.run([str(node), '--experimental-strip-types', str(runner), str(ROOT),
                                     args.manifest, str(output)], cwd=ROOT,
                                    env=dict(os.environ, NODE_EXTRA_CA_CERTS=str(ca), NODE_TLS_REJECT_UNAUTHORIZED="1", NODE_OPTIONS="", LAYERX_HUMAN_SERVICE_URL=material['url']),
                                    stdout=log, stderr=log, timeout=min(900, remaining()))
        check(result.returncode == 0, 'generated client live operation contract')
        observed = load(output)
        agent_log = directory / (by_role['agent']['name'] + '.log')
        require(agent_log.stat().st_size <= MAX_BYTES, 'bounded agent trace log')
        check(('human_request trace=' + observed['balance_trace'] + ' outcome=0').encode()
              in agent_log.read_bytes(), 'original browser trace reaches authenticated agent operation')
        global COUNT
        COUNT += observed['tests']
        headers = {'Cookie': '; '.join(key + '=' + value for key, value in observed['cookies'].items()),
                   'Origin': material['origin']}
        status, failure = request(url, context, 'POST', '/v1/stream', headers)
        check(status == 403 and failure['error']['code'] == 'forbidden', 'missing CSRF refusal')
        headers['X-LayerX-CSRF'] = observed['cookies']['__Host-layerx_csrf']
        hostile = dict(headers, Origin='https://unlisted.invalid')
        status, failure = request(url, context, 'POST', '/v1/stream', hostile)
        check(status == 403 and failure['error']['code'] == 'forbidden', 'foreign browser origin refusal')
        processes.stop(by_role['agent']['name'])
        status, stale = request(url, context, 'GET', '/v1/account/balance', headers)
        balance = stale.get('result', {})
        check(status == 200 and balance['money']['amount'] == observed['balance']['amount']
              and balance['money']['currency'] == observed['balance']['currency']
              and balance['verification'] == observed['balance']['verification']
              and balance['freshness']['source_head'] == observed['balance']['source_head']
              and balance['freshness']['within_bound'] is False
              and balance['freshness']['age_seconds'] >= 0, 'last verified balance survives agent outage honestly')
        status, degraded = request(url, context, 'GET', '/readyz')
        check(status == 503 and degraded['result']['components']['agent'] != 'ready', 'agent degradation distinguished')
        limit = int(service.get('LAYERX_HUMAN_REQUESTS_PER_MINUTE', '240'))
        require(0 < limit <= 1000, 'bounded principal rate qualification')
        for _ in range(limit + 1):
            status, failure = request(url, context, 'GET', '/v1/account/balance', headers)
            if status == 429:
                break
        check(status == 429 and failure['error']['code'] == 'rate-limited'
              and failure['error']['retry'] == 'retriable-after', 'principal rate refusal')
        processes.stop(by_role['components']['name'])
        status, degraded = request(url, context, 'GET', '/readyz')
        check(status == 503 and degraded['result']['components']['human_service'] == 'degraded'
              and all(degraded['result']['components'][key] == 'unavailable'
                      for key in ('custody', 'agent', 'core', 'paxeer')), 'component outage remains redacted')
        report = {'revision': revision, 'source_digest': source, 'tests': COUNT, 'skipped': 0}
        report_path = directory / 'report.json'
        report_path.write_text(json.dumps(report, sort_keys=True) + '\n')
        os.chmod(report_path, 0o600)
        print('PAXEER_X_GATE tests=' + str(COUNT) + ' skipped=0', flush=True)
    finally:
        processes.close()


if __name__ == '__main__':
    def interrupted(_signal, _frame):
        raise RuntimeError('bounded gate interrupted')
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        main()
    except Exception:
        print('Human API boundary refused; inspect private process and client logs.', file=sys.stderr)
        sys.exit(1)
