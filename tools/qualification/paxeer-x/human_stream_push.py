#!/usr/bin/env python3
import importlib.util
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location("human_boundary", ROOT / "tools/qualification/paxeer-x/human-api-boundary.py")
B = importlib.util.module_from_spec(spec)
spec.loader.exec_module(B)

CLIENT = r"""
import { readFile, writeFile } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';
import { randomBytes } from 'node:crypto';
const [root, manifestPath, resultPath, phase, previousPath] = process.argv.slice(2);
const fixture = JSON.parse(await readFile(manifestPath, 'utf8'));
const { createHumanApiClient, HumanApiError, HumanApiDecodeError } = await import(pathToFileURL(root + '/human/apps/web/src/api/generated/index.ts').href);
const { conformance } = await import(pathToFileURL(root + '/human/apps/web/src/api/generated/conformance.ts').href);
let tests = 0;
function check(ok, message) { if (!ok) throw new Error(message); tests += 1; }
const jars = new Map(Object.entries(fixture.sessions).map(([name, row]) => [name, new Map(Object.entries(row.cookies))]));
const streamSizes = [];
function client(name) {
  const jar = jars.get(name);
  check(jar !== undefined, 'genuine principal session required');
  return createHumanApiClient({ baseUrl: fixture.url, csrfToken: () => jar.get('__Host-layerx_csrf'),
    trace: () => 'trc_' + randomBytes(16).toString('hex'),
    fetch: async (url, init) => {
      const headers = new Headers(init.headers);
      headers.set('Origin', fixture.origin);
      headers.set('Cookie', [...jar].map(([name, value]) => name + '=' + value).join('; '));
      const response = await fetch(url, { ...init, headers, redirect: 'error' });
      for (const cookie of response.headers.getSetCookie()) {
        const pair = cookie.split(';')[0], at = pair.indexOf('=');
        jar.set(pair.slice(0, at), pair.slice(at + 1));
      }
      if (response.headers.get('Content-Type')?.startsWith('text/event-stream')) {
        const measured = { bytes: 0 };
        streamSizes.push(measured);
        check(response.body !== null, 'real streaming response body');
        const body = response.body.pipeThrough(new TransformStream({ transform(chunk, controller) {
          measured.bytes += chunk.byteLength;
          controller.enqueue(chunk);
        } }));
        return new Response(body, { status: response.status, headers: response.headers });
      }
      return response;
    },
  });
}
const primary = client(fixture.principal), other = client(fixture.other_principal);
const output = previousPath === '-' ? {} : JSON.parse(await readFile(previousPath, 'utf8'));
const outputs = new Map();
function resolve(value) {
  if (Array.isArray(value)) return value.map(resolve);
  if (value && typeof value === 'object') {
    if (Object.keys(value).length === 1 && typeof value.$result === 'string') {
      const [name, ...parts] = value.$result.split('/'); let found = outputs.get(name);
      for (const part of parts) found = found?.[part];
      if (found === undefined) throw new Error('missing retained genuine operation result');
      return found;
    }
    return Object.fromEntries(Object.entries(value).map(([key, value]) => [key, resolve(value)]));
  }
  return value;
}
async function drive(name) {
  const cases = fixture.drivers[name];
  check(Array.isArray(cases) && cases.length > 0 && cases.length <= 1024, 'bounded genuine producer driver');
  for (const row of cases) {
    check(typeof row.id === 'string' && !outputs.has(row.id) && conformance[row.operation] !== undefined, 'unique schema-owned producer request');
    const result = await conformance[row.operation]({ client: client(row.principal), params: resolve(row.params ?? {}),
      ...(row.body === undefined ? {} : { body: resolve(row.body) }),
      ...(row.idempotencyKey === undefined ? {} : { idempotencyKey: row.idempotencyKey }) });
    outputs.set(row.id, result);
  }
}
function subscription(cursor, supplied = primary) {
  const abort = new AbortController();
  const iterator = supplied.streamSubscribe(cursor, { signal: abort.signal })[Symbol.asyncIterator]();
  return { abort, iterator };
}
async function next(stream, limit = 20000) {
  let timer;
  try { return await Promise.race([stream.iterator.next(), new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('production push delivery deadline')), limit); })]); }
  finally { clearTimeout(timer); }
}
async function failure(stream, code) {
  let error;
  try { await next(stream); } catch (caught) { error = caught; }
  finally { stream.abort.abort(); }
  check(error instanceof HumanApiError && error.detail.code === code, 'authenticated typed stream refusal');
}
if (phase === 'live') {
  let cursor = (await primary.streamOpen()).cursor;
  const foreign = subscription(cursor, other);
  await failure(foreign, 'forbidden');
  await failure(subscription(fixture.expired_cursor), 'cursor-expired');
  const stream = subscription(cursor);
  const pending = next(stream);
  await drive('live');
  const seen = new Set(), kinds = new Set();
  let item = await pending;
  for (let count = 0; count < fixture.live_events; count += 1) {
    if (count > 0) item = await next(stream);
    check(!item.done && !seen.has(item.value.cursor), 'live durable event is delivered once');
    seen.add(item.value.cursor); kinds.add(item.value.kind); cursor = item.value.cursor;
  }
  check(kinds.has('journey-progress') && kinds.has('notification'), 'real journey and notification producer paths');
  stream.abort.abort();
  await drive('resume');
  const resumed = subscription(cursor);
  for (let count = 0; count < fixture.resume_events; count += 1) {
    const event = await next(resumed);
    check(!event.done && !seen.has(event.value.cursor), 'disconnect resumes without duplicate cursor');
    seen.add(event.value.cursor); cursor = event.value.cursor;
  }
  resumed.abort.abort();
  const quiet = subscription((await primary.streamOpen()).cursor);
  const quietPending = quiet.iterator.next();
  quiet.abort.abort();
  await quietPending.catch((error) => { check(error.name === 'AbortError', 'iterator cancellation refusal'); });
  check((await primary.version()).schema.major === 1, 'service remains available after quiet cancellation');
  output.cursor = cursor; output.seen = [...seen];
} else if (phase === 'restart') {
  const seen = new Set(output.seen);
  const stream = subscription(output.cursor);
  const pending = next(stream);
  await drive('restart');
  let item = await pending;
  for (let count = 0; count < fixture.restart_events; count += 1) {
    if (count > 0) item = await next(stream);
    check(!item.done && !seen.has(item.value.cursor), 'actual component restart retains cursor journal');
    seen.add(item.value.cursor); output.cursor = item.value.cursor;
  }
  stream.abort.abort();
  const quotaCursor = (await primary.streamOpen()).cursor;
  await drive('quota');
  const quotaIndex = streamSizes.length;
  const quota = subscription(quotaCursor);
  let count = 0;
  while (true) {
    const item = await next(quota);
    if (item.done) break;
    count += 1;
    check(count <= 100, 'original aggregate event quota');
  }
  check(count === 100, 'actual event quota reached without widened page');
  check(streamSizes[quotaIndex].bytes <= 1048576 + 100 * 64, 'original aggregate response payload quota');
  const expiry = subscription((await primary.streamOpen()).cursor);
  const started = performance.now();
  const ended = await next(expiry, 65000);
  check(ended.done && performance.now() - started <= 61000, 'original authorization lifetime ends quiet subscription');
  const revokedClient = client(fixture.revoked_principal);
  const revoke = subscription((await revokedClient.streamOpen()).cursor, revokedClient);
  const revokedPending = next(revoke).then(() => { throw new Error('revoked stream produced success'); }, (error) => error);
  await drive('revoke');
  const refusal = await revokedPending;
  check(refusal instanceof HumanApiError && ['unauthenticated', 'session-expired', 'forbidden'].includes(refusal.detail.code), 'real session revocation interrupts existing subscription');
  revoke.abort.abort();
} else { throw new Error('unknown real-process phase'); }
output.tests = tests;
output.cookies = Object.fromEntries(jars.get(fixture.principal));
await writeFile(resultPath, JSON.stringify(output), { mode: 0o600 });
"""


def main():
    B.require(len(sys.argv) == 1, 'no arguments accepted')
    manifest = os.environ.get('PAXEER_X_HUMAN_STREAM_MANIFEST')
    B.require(manifest, 'genuine private stream fixture required')
    material = B.load(manifest)
    B.require(material['schema'] == 'layerx-human-stream-push.v2', 'fixture version')
    B.require(not B.git('status', '--porcelain', '--untracked-files=no').strip(), 'clean candidate source required')
    revision, source = B.identity()
    directory = B.private(material['evidence_directory'], True)
    B.require(not any(directory.iterdir()), 'fresh private evidence directory required')
    artifacts = {name: B.executable(row, revision, source) for name, row in material['artifacts'].items()}
    roles = ('service', 'components', 'agent', 'core', 'paxeer')
    B.require(all(name in artifacts for name in roles), 'genuine production artifacts required')
    node = Path(material['node'])
    B.require(node.is_absolute() and node.is_file() and os.access(node, os.X_OK)
              and B.digest(node) == material['node_sha256'], 'pinned actual Node runtime')
    ca = B.private(material['ca_pem'])
    context = B.ssl.create_default_context(cafile=str(ca))
    url = B.endpoint(material['url'])
    B.require(material['principal'] != material['other_principal'] and material['revoked_principal'] not in
              (material['principal'], material['other_principal']), 'distinct genuine authenticated sessions')
    for key in ('live_events', 'resume_events', 'restart_events'):
        B.require(type(material[key]) is int and 0 < material[key] <= 100, 'bounded actual event inventory')
    processes = B.Processes(directory, artifacts)
    by_role = {row['artifact']: row for row in material['processes']}
    B.require(all(name in by_role for name in roles), 'real process role inventory')
    service = by_role['service']['environment']
    B.require(service['LAYERX_HUMAN_LISTENER'] == 'tls' and service['LAYERX_HUMAN_BIND'] == '127.0.0.1:' + str(url.port)
              and service['LAYERX_HUMAN_WEB_ORIGIN'] == material['origin'], 'actual isolated HTTPS service')
    B.private(service['LAYERX_HUMAN_TLS_CERT_DER'])
    B.private(service['LAYERX_HUMAN_TLS_KEY_DER'])
    def ready():
        until = time.monotonic() + min(90, B.remaining())
        while time.monotonic() < until:
            B.require(all(child.poll() is None for child, _, _ in processes.children.values()), 'production child exited')
            try:
                status, value = B.request(url, context, 'GET', '/readyz')
                if status == 200 and value.get('result', {}).get('ready') is True:
                    return
            except (OSError, B.http.client.HTTPException):
                pass
            time.sleep(0.1)
        raise RuntimeError('actual production readiness deadline')
    try:
        for row in material['processes']:
            processes.start(row)
        ready()
        runner = directory / 'push-client.mts'
        runner.write_text(CLIENT)
        runner.chmod(0o600)
        previous = '-'
        for phase in ('live', 'restart'):
            if phase == 'restart':
                rows = [processes.stop(by_role[name]['name']) for name in ('service', 'components')]
                for row in reversed(rows):
                    old = directory / (row['name'] + '.log')
                    old.rename(directory / (row['name'] + '-before-restart.log'))
                    processes.start(row)
                ready()
            result_path = directory / (phase + '-result.json')
            with (directory / (phase + '.log')).open('xb') as log:
                os.chmod(log.name, 0o600)
                result = subprocess.run([str(node), '--experimental-strip-types', str(runner), str(ROOT), manifest,
                    str(result_path), phase, previous], cwd=ROOT, env=dict(os.environ,
                    NODE_EXTRA_CA_CERTS=str(ca), NODE_TLS_REJECT_UNAUTHORIZED='1', NODE_OPTIONS=''),
                    stdout=log, stderr=log, timeout=min(600, B.remaining()))
            B.check(result.returncode == 0, 'actual generated iterator production phase')
            observed = B.load(result_path)
            B.require(type(observed['tests']) is int and observed['tests'] > 0, 'real assertions required')
            B.COUNT += observed['tests']
            previous = str(result_path)
        import socket
        import struct
        import hashlib
        def connect(request):
            connection = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            connection.settimeout(min(10, B.remaining()))
            connection.connect(service['LAYERX_HUMAN_COMPONENT_SOCKET'])
            encoded = json.dumps(request, separators=(',', ':')).encode()
            B.require(len(encoded) <= 1048576, 'original request frame bound')
            connection.sendall(struct.pack('>I', len(encoded)) + encoded)
            return connection
        def receive(connection):
            def exact(count):
                result = bytearray()
                while len(result) < count:
                    block = connection.recv(count - len(result))
                    B.require(block, 'real component response required')
                    result.extend(block)
                return bytes(result)
            length = struct.unpack('>I', exact(4))[0]
            B.require(0 < length <= 1048576, 'original response frame bound')
            return json.loads(exact(length))
        def authorize(cookies):
            trace = 'trc_' + os.urandom(16).hex()
            cursor = observed['cursor']
            destination = '/v1/stream/' + cursor
            digest = hashlib.sha256(b'layerx-human/authorized-operation/v1\0')
            for value in ('stream.next', 'GET', destination, 'cursor', cursor, '{}', '', trace):
                value = value.encode()
                digest.update(struct.pack('>Q', len(value)))
                digest.update(value)
            request = {'version': 1, 'kind': 'session.authorize', 'operation': 'stream.next',
                'access_token': cookies['__Host-layerx_access'], 'csrf_token': cookies.get('__Host-layerx_csrf'),
                'intended_destination': destination, 'refresh': False, 'request_digest': digest.hexdigest(),
                'disclosure_digest': hashlib.sha256(b'{}').hexdigest(), 'path_parameters': {'cursor': cursor},
                'body': {}, 'idempotency_key': None, 'trace': trace}
            connection = connect(request)
            try:
                result = receive(connection)
            finally:
                connection.close()
            B.check(result.get('ok') is True and result.get('result', {}).get('capability'), 'genuine affine capability issued by actual owner')
            return {'version': 1, 'kind': 'human-api.execute', 'operation': 'stream.next',
                'component': 'notifications', 'stream_profile': 2, 'principal': result['result'],
                'path_parameters': {'cursor': cursor}, 'body': {}, 'idempotency_key': None, 'trace': trace}
        def refused(request):
            connection = connect(request)
            try:
                failure = receive(connection)
            finally:
                connection.close()
            B.check(failure.get('ok') is False and failure.get('error', {}).get('code') in
                    ('forbidden', 'unauthenticated', 'session-expired'), 'actual affine capability refusal')
        original = authorize(observed['cookies'])
        connection = connect(original)
        try:
            success = receive(connection)
            B.check(success.get('ok') is True and success.get('result', {}).get('events'), 'real original stream consumes capability')
        finally:
            connection.close()
        refused(original)
        foreign = authorize(observed['cookies'])
        actual_other = authorize(material['sessions'][material['other_principal']]['cookies'])
        B.require(actual_other['principal']['tenant_id'] != foreign['principal']['tenant_id'], 'genuine distinct tenant required')
        foreign['principal']['tenant_id'] = actual_other['principal']['tenant_id']
        refused(foreign)
        expired = authorize(observed['cookies'])
        lifetime = expired['principal']['expires_at'] - expired['principal']['issued_at']
        B.require(0 < lifetime <= 60 and lifetime + 1 < B.remaining(), 'original authorization lifetime')
        time.sleep(lifetime + 1)
        refused(expired)
        report = {'revision': revision, 'source_digest': source, 'tests': B.COUNT, 'skipped': 0}
        path = directory / 'report.json'
        path.write_text(json.dumps(report, sort_keys=True) + '\n')
        path.chmod(0o600)
        print('PAXEER_X_GATE tests=' + str(B.COUNT) + ' skipped=0', flush=True)
    finally:
        processes.close()


if __name__ == '__main__':
    def interrupted(_signal, _frame):
        raise RuntimeError('bounded push gate interrupted')
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        main()
    except Exception:
        print('Human push gate refused; inspect private fixture and process logs.', file=sys.stderr)
        sys.exit(1)
