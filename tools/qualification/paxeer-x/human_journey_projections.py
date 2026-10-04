#!/usr/bin/env python3
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import ssl
import subprocess
import sys
import time

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location('human_boundary', ROOT / 'tools/qualification/paxeer-x/human-api-boundary.py')
B = importlib.util.module_from_spec(spec)
spec.loader.exec_module(B)
TEST = 'genuine_journey_family_owners_survive_reopen'


def main():
    B.require(not sys.argv[1:], 'unsupported gate arguments')
    fixture_path = os.environ.get('PAXEER_X_HUMAN_JOURNEY_PROJECTIONS_FIXTURE')
    B.require(fixture_path, 'protected genuine journey production fixture required')
    fixture = B.load(fixture_path)
    B.require(fixture['schema'] == 'layerx-human-journey-projections.v1'
              and fixture['disposable_real_authority'] is True, 'closed disposable authority profile required')
    revision, source = B.identity()
    B.require(fixture['source_revision'] == revision and fixture['source_digest'] == source,
              'fixture must bind final candidate source')
    B.require(not B.git('status', '--porcelain', '--untracked-files=no').strip(), 'clean candidate required')
    directory = B.private(fixture['evidence_directory'], True)
    B.require(not any(directory.iterdir()), 'fresh private evidence directory required')
    artifacts = {name: B.executable(row, revision, source) for name, row in fixture['artifacts'].items()}
    roles = ('service', 'components', 'agent', 'core', 'paxeer', 'corpus')
    B.require(all(role in artifacts for role in roles), 'actual candidate process and compiled corpus artifacts required')
    B.require(5 <= len(fixture['processes']) <= 32, 'bounded actual process inventory')
    by_role = {row['artifact']: row for row in fixture['processes']}
    B.require(all(role in by_role for role in roles[:-1]), 'actual production process roles required')
    url = B.endpoint(fixture['url'])
    service = by_role['service']['environment']
    B.require(service['LAYERX_HUMAN_LISTENER'] == 'tls'
              and service['LAYERX_HUMAN_BIND'] == '127.0.0.1:' + str(url.port)
              and service['LAYERX_HUMAN_WEB_ORIGIN'] == fixture['origin'], 'isolated genuine HTTPS boundary required')
    B.private(service['LAYERX_HUMAN_TLS_CERT_DER'])
    B.private(service['LAYERX_HUMAN_TLS_KEY_DER'])
    context = ssl.create_default_context(cafile=str(B.private(fixture['ca_pem'])))
    sessions = fixture['sessions']
    principal, foreign = fixture['principal'], fixture['other_principal']
    B.require(principal != foreign and principal in sessions and foreign in sessions, 'distinct genuine authenticated principals required')
    cases = fixture['cases']
    B.require(7 <= len(cases) <= 128 and {row['family'] for row in cases} == {'native', 'deposit', 'withdrawal', 'exit'},
              'all genuine journey families and state cases required')
    B.require({'pending', 'unknown', 'terminal'} <= {row['category'] for row in cases}, 'genuine pending, unknown and terminal cases required')
    B.require({'waiting-custody', 'unknown-withdrawal-broadcast', 'terminal-failed', 'receipt-finality-done'} <= {row['stage_case'] for row in cases}, 'actual waiting, unknown broadcast, failed and verified done stage cases required')
    processes = B.Processes(directory, artifacts)
    observations = []

    def headers(name):
        cookies = sessions[name]['cookies']
        B.require(isinstance(cookies, dict) and cookies.get('__Host-layerx_csrf'), 'real cookie and CSRF authority required')
        return {'Origin': fixture['origin'], 'Cookie': '; '.join(key + '=' + value for key, value in cookies.items()),
                'X-LayerX-CSRF': cookies['__Host-layerx_csrf']}

    def call(name, method, path, body=None, idempotency=None):
        B.require(path.startswith('/v1/') and '?' not in path and '#' not in path, 'canonical production route required')
        supplied = headers(name)
        if idempotency is not None:
            supplied['Idempotency-Key'] = idempotency
        return B.request(url, context, method, path, supplied, body)

    def ready():
        until = time.monotonic() + min(90, B.remaining())
        while True:
            B.require(all(child.poll() is None for child, _, _ in processes.children.values()), 'actual processes remain alive')
            try:
                status, response = B.request(url, context, 'GET', '/readyz')
                if status == 200 and response.get('result', {}).get('ready') is True:
                    return
            except (OSError, B.http.client.HTTPException):
                pass
            B.require(time.monotonic() < until, 'production readiness deadline')
            time.sleep(0.1)

    def inventory(name):
        found, seen, pages = {}, set(), []
        cursor = 'cur_start'
        for _ in range(64):
            status, envelope = call(name, 'GET', '/v1/journeys/page/' + cursor)
            B.check(status == 200, 'common authenticated journey page')
            page = envelope['result']
            B.check(isinstance(page['journeys'], list) and len(page['journeys']) <= 50, 'fixed merged page bound')
            for journey in page['journeys']:
                identity = journey['journey_id']
                B.check(identity not in found, 'no duplicate merged journey identity')
                found[identity] = journey
            next_cursor = page['next_cursor']
            pages.append(page)
            B.check(isinstance(next_cursor, str) and re.fullmatch(r'cur_(?:end|jrn1_[a-f0-9]{64}_[1-9][0-9]*)', next_cursor), 'closed merged cursor')
            if next_cursor == 'cur_end':
                return found, pages
            B.check(next_cursor not in seen, 'pagination progresses')
            seen.add(next_cursor)
            if name == principal:
                denied, refusal = call(foreign, 'GET', '/v1/journeys/page/' + next_cursor)
                B.check(denied == 400 and refusal['error']['code'] == 'invalid-request' and refusal['error']['field'] == 'cursor', 'cursor remains principal bound')
            cursor = next_cursor
        raise RuntimeError('bounded pagination did not terminate')

    def evidence(journey):
        levels = {'unverified', 'receipt-verified', 'checkpoint-finalised', 'settlement-anchored', 'paxeer-finalised'}
        refs = journey['evidence']
        B.check(isinstance(refs, list), 'honest journey evidence inventory')
        for reference in refs:
            B.check(reference['verification'] in levels, 'achieved evidence level preserved')
            identity = reference['evidence_id']
            B.check(re.fullmatch(r'evd_[a-f0-9]{64}', identity), 'actual evidence identity')
            status, exported = call(principal, 'GET', '/v1/evidence/' + identity)
            B.check(status == 200, 'genuine evidence export reachable')
            import base64
            raw = base64.b64decode(exported['result']['bytes_base64'], validate=True)
            binding = fixture['evidence_bindings'].get(identity)
            B.check(raw and binding and hashlib.sha256(raw).hexdigest() == binding['material_sha256'] and exported['result']['evidence_id'] == identity and exported['result']['class'] == reference['class'], 'independent genuine material digest and exact owner class bind evidence')
            B.check(exported['result']['verification'] == reference['verification'], 'export cannot promote evidence')
            denied, _ = call(foreign, 'GET', '/v1/evidence/' + identity)
            B.check(denied in (403, 404), 'evidence ownership retained')

    try:
        for row in fixture['processes']:
            processes.start(row)
        ready()
        for row in cases:
            status, submitted = call(principal, 'POST', row['submission']['path'], row['submission']['body'], row['submission']['idempotency_key'])
            B.check(status == 200, 'genuine original family submission')
            returned = submitted['result']
            for field in row['returned_id_path']:
                returned = returned[field]
            B.check(isinstance(returned, str) and returned == row['journey_id'], 'returned outer identity matches actual authority fixture')
            status, projected = call(principal, 'GET', '/v1/journeys/' + returned)
            B.check(status == 200 and projected['result']['journey_id'] == returned, 'returned outer ID resolves through common get')
            value = projected['result']
            B.check(value['state'] == row['state'], 'pending unknown terminal state comes from actual owner')
            B.check(value['started_at'] == row['started_at'] and value['updated_at'] == row['updated_at'], 'genuine owner timestamps preserved')
            evidence(value)
            if row['category'] == 'terminal' and value['state'] == 'done':
                B.check(any(ref['verification'] != 'unverified' for ref in value['evidence']), 'completed journey requires verified evidence')
            denied, _ = call(foreign, 'GET', '/v1/journeys/' + returned)
            B.check(denied in (403, 404), 'cross-principal outer ID refused')
            observations.append({'family': row['family'], 'journey_id': returned, 'projection': value, 'inner_ids': row['inner_ids'], 'started_at_seconds': row['started_at_seconds'], 'updated_at_seconds': row['updated_at_seconds']})
        before, pages = inventory(principal)
        B.check(len(before) > 50 and len(pages) >= 2, 'real multi-page producer inventory required')
        status, first = call(principal, 'GET', '/v1/journeys')
        B.check(status == 200 and first['result'] == pages[0], 'list and page share exact projection and cursor')
        for row in observations:
            B.check(before.get(row['journey_id']) == row['projection'], 'outer family appears once in common list')
            for child in row['inner_ids']:
                B.check(child not in before, 'parent-owned inner economic journey suppressed')
                denied, _ = call(principal, 'GET', '/v1/journeys/' + child)
                B.check(denied == 404, 'inner IDs do not create a second public journey')
        restart_roles = ('service', 'components', 'agent')
        restart = [processes.stop(by_role[role]['name']) for role in restart_roles]
        for row in reversed(restart):
            row = dict(row, name=row['name'] + '-reopened')
            processes.start(row)
        ready()
        after, reopened_pages = inventory(principal)
        B.check(after == before and reopened_pages == pages, 'restart preserves merged projections, evidence and pagination')
        for row in observations:
            status, projected = call(principal, 'GET', '/v1/journeys/' + row['journey_id'])
            B.check(status == 200 and projected['result'] == row['projection'], 'restart retains original parent identity and evidence')
        empty, _ = call(principal, 'GET', '/v1/journeys/page/cur_end')
        B.check(empty == 200, 'terminal cursor remains readable')
        malformed, _ = call(principal, 'GET', '/v1/journeys/page/cur_invalid')
        B.check(malformed in (400, 403, 404), 'malformed cursor refused')
    finally:
        processes.close()
    observed_path = directory / 'observed-journeys.json'
    observed_path.write_text(json.dumps({'fixture': str(B.private(fixture_path)), 'journeys': observations}, separators=(',', ':')))
    observed_path.chmod(0o600)
    with (directory / 'durable-owner-corpus.log').open('xb') as log:
        os.chmod(log.name, 0o600)
        result = subprocess.run([str(artifacts['corpus']), '--exact', TEST], cwd=ROOT,
                                env=dict(os.environ, PAXEER_X_HUMAN_JOURNEY_OBSERVATIONS=str(observed_path)),
                                stdout=log, stderr=log, timeout=min(120, B.remaining()))
    raw = (directory / 'durable-owner-corpus.log').read_bytes()
    B.check(result.returncode == 0 and b'running 1 test' in raw and b'1 passed; 0 failed' in raw,
            'actual durable-owner corpus executed without skips')
    record = {'revision': revision, 'command': 'python3 tools/qualification/paxeer-x/human_journey_projections.py',
              'exit_code': 0, 'cases': B.COUNT, 'families': sorted({row['family'] for row in observations}),
              'evidence_directory': str(directory), 'durable_log': str(directory / 'durable-owner-corpus.log')}
    (directory / 'result.json').write_text(json.dumps(record, separators=(',', ':')))
    (directory / 'result.json').chmod(0o600)
    print('human-journey-projections: PASS cases=' + str(B.COUNT) + ' revision=' + revision)


if __name__ == '__main__':
    try:
        main()
    except Exception:
        print('human-journey-projections: FAIL: genuine candidate, authority or production case unavailable', file=sys.stderr)
        sys.exit(1)
