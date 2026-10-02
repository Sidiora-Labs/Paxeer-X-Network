#!/usr/bin/env python3
import hashlib
import http.client
import json
import os
import re
from pathlib import Path
import socket
import ssl
import struct
import subprocess
import sys
import time
import urllib.parse

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'tools/paxeer-x'))
from candidate import load_private, validate, catalogue, Invalid

MAX_BODY = 8 * 1024 * 1024
COUNT = 0
KINDS = {'positive', 'malformed', 'unauthorized', 'wrong-network', 'missing-dependency'}
SECRET_HEADERS = {'authorization', 'cookie', 'x-agent-signature', 'x-api-key',
                  'x-csrf-token', 'x-layerx-csrf', 'x-payment', 'payment-signature'}


def require(value, message):
    if not value:
        raise Invalid(message)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':')).encode()


def pointer(value, path):
    require(path.startswith('/'), 'response assertion has no JSON pointer')
    for part in path[1:].split('/'):
        part = part.replace('~1', '/').replace('~0', '~')
        value = value[int(part)] if isinstance(value, list) else value[part]
    return value


def secret(path):
    path = Path(path)
    info = path.lstat()
    require(path.is_file() and not path.is_symlink() and info.st_mode & 0o077 == 0
            and info.st_uid == os.geteuid() and info.st_nlink == 1 and info.st_size <= 65536,
            'credential reference is not protected')
    return path.read_text().strip()


def context(target):
    ctx = ssl.create_default_context(cafile=target.get('ca_file'))
    ctx.minimum_version = ssl.TLSVersion.TLSv1_2
    if 'certificate_file' in target:
        secret(target['private_key_file'])
        ctx.load_cert_chain(target['certificate_file'], target['private_key_file'])
    return ctx


def request_path(case):
    path = case['path']
    require(isinstance(path, str), 'request path must be a string')
    parsed = urllib.parse.urlsplit(path)
    require(path.startswith('/') and not path.startswith('//')
            and not parsed.scheme and not parsed.netloc and not parsed.fragment
            and len(path) <= 2048 and all(32 < ord(c) < 127 for c in path),
            'request requires a bounded origin-relative path')
    return parsed.path


def request_body(case):
    require(not ('body' in case and 'body_file' in case), 'ambiguous request body')
    if 'body_file' in case:
        path = Path(case['body_file'])
        info = path.lstat()
        require(path.is_file() and not path.is_symlink() and info.st_mode & 0o077 == 0
                and info.st_uid == os.geteuid() and info.st_nlink == 1
                and info.st_size <= MAX_BODY, 'request body reference is not protected')
        return path.read_bytes()
    body = case.get('body')
    encoded = None if body is None else canonical(body)
    require(encoded is None or len(encoded) <= MAX_BODY, 'request body exceeds byte bound')
    return encoded


def http_request(target, case):
    parsed = urllib.parse.urlsplit(target['url'])
    require(parsed.scheme == 'https' and parsed.hostname and not parsed.username
            and not parsed.password and not parsed.query and not parsed.fragment,
            'target requires canonical HTTPS')
    headers = dict(case.get('headers', {}))
    require(not any(k.lower() in SECRET_HEADERS for k in headers),
            'credentials require protected file references')
    for name, path in case.get('header_files', {}).items():
        headers[name] = secret(path)
    encoded = request_body(case)
    if encoded is not None:
        headers.setdefault('Content-Type', 'application/octet-stream' if 'body_file' in case else 'application/json')
    conn = http.client.HTTPSConnection(parsed.hostname, parsed.port or 443,
                                       timeout=8, context=context(target))
    try:
        conn.request(case['method'], parsed.path.rstrip('/') + case['path'], encoded, headers)
        reply = conn.getresponse()
        data = reply.read(MAX_BODY + 1)
        require(len(data) <= MAX_BODY, 'response exceeds byte bound')
        require(reply.status == case['status'], 'unexpected routed status')
        for name,value in case.get('response_headers',{}).items():
            require(reply.getheader(name) == value, 'response header assertion failed')
        if 'response_sha256' in case:
            require(hashlib.sha256(data).hexdigest() == case['response_sha256'],
                    'routed response byte digest mismatch')
        document = json.loads(data) if data and ('response_sha256' not in case or case['assertions']) else None
        for key, value in case['assertions'].items():
            require(pointer(document, key) == value, 'routed response assertion failed')
        return document
    finally:
        conn.close()


def read_exact(stream, count):
    chunks = bytearray()
    while len(chunks) < count:
        piece = stream.recv(count - len(chunks))
        require(piece, 'WebSocket closed before result')
        chunks.extend(piece)
    return bytes(chunks)


def websocket_request(target, case):
    import base64
    parsed = urllib.parse.urlsplit(target['url'])
    require(parsed.scheme == 'https' and parsed.hostname, 'WebSocket target requires TLS')
    key = base64.b64encode(os.urandom(16)).decode()
    expected = base64.b64encode(hashlib.sha1((key + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').encode()).digest()).decode()
    headers = {'Host': parsed.netloc, 'Upgrade': 'websocket', 'Connection': 'Upgrade',
               'Sec-WebSocket-Key': key, 'Sec-WebSocket-Version': '13'}
    for name, path in case.get('header_files', {}).items():
        headers[name] = secret(path)
    with socket.create_connection((parsed.hostname, parsed.port or 443), timeout=8) as tcp:
        with context(target).wrap_socket(tcp, server_hostname=parsed.hostname) as stream:
            stream.settimeout(8)
            head = 'GET ' + case['path'] + ' HTTP/1.1\r\n'
            require(all('\r' not in k+v and '\n' not in k+v for k,v in headers.items()), 'invalid WebSocket headers')
            head += ''.join(k + ': ' + v + '\r\n' for k,v in headers.items()) + '\r\n'
            stream.sendall(head.encode())
            reply = bytearray()
            while not reply.endswith(b'\r\n\r\n'):
                require(len(reply) < 8192, 'WebSocket header bound')
                reply.extend(read_exact(stream, 1))
            lines = reply.decode().split('\r\n')
            require(int(lines[0].split()[1]) == case['status'], 'unexpected WebSocket upgrade status')
            if case['status'] != 101:
                return
            fields = {line.split(':',1)[0].lower():line.split(':',1)[1].strip() for line in lines[1:] if ':' in line}
            require(fields.get('sec-websocket-accept') == expected, 'WebSocket accept mismatch')
            require(case.get('messages'), 'empty WebSocket operation corpus')
            for message in case['messages']:
                data = canonical(message['send'])
                mask = os.urandom(4)
                require(len(data) <= 65535, 'WebSocket message bound')
                frame = bytes([0x81, 0x80 | len(data)]) if len(data) < 126 else bytes([0x81, 0xfe]) + struct.pack('!H', len(data))
                stream.sendall(frame + mask + bytes(b ^ mask[i%4] for i,b in enumerate(data)))
                first, second = read_exact(stream,2)
                require(first == 0x81 and second & 128 == 0, 'unexpected WebSocket frame')
                length = second if second < 126 else struct.unpack('!H' if second == 126 else '!Q', read_exact(stream,2 if second == 126 else 8))[0]
                require(length <= 65536, 'WebSocket response bound')
                document = json.loads(read_exact(stream,length))
                if case['kind'] == 'positive':
                    require(isinstance(document, dict) and document.get('jsonrpc') == '2.0'
                            and document.get('id') == message['send'].get('id')
                            and 'id' in document and 'result' in document and 'error' not in document,
                            'positive WebSocket operation has no successful result')
                require(message['assertions'], 'empty WebSocket assertions')
                for key,value in message['assertions'].items():
                    require(pointer(document,key) == value, 'WebSocket response assertion failed')


def route_matches(route, case):
    path = request_path(case)
    if route['service'] != case['service']:
        return False
    if route['transport'] == 'mcp' and not route['path'].startswith('/'):
        body = case.get('body',{})
        return case['method'] == 'POST' and path == route.get('endpoint_path') and isinstance(body,dict) and body.get('method') == 'tools/call' and body.get('params',{}).get('name') == route['path']
    if route['transport'] == 'json-rpc':
        body = case.get('body')
        calls = body if isinstance(body, list) else [body]
        return case['method'] == 'POST' and path in route.get('endpoint_paths', ['/rpc']) and any(isinstance(call, dict) and
            (call.get('method','').startswith(route['path'][:-1]) if route['path'].endswith('*')
             else call.get('method') == route['path']) for call in calls)
    if route['transport'] == 'websocket':
        if case.get('transport') != 'websocket': return False
        if route['path'].startswith('/'): return case['path'] == route['path']
        return path == '/rpc/ws' and any(message.get('send',{}).get('method') == route['path'] for message in case.get('messages',[]))
    pieces = []
    for part in route['path'].split('/'):
        pieces.append('[^/?#]+' if part.startswith(':') or (part.startswith('{') and part.endswith('}')) else re.escape(part))
    return case['method'] == route['method'] and re.fullmatch('/'.join(pieces),path) is not None


def require_rpc_results(case, document, route_index):
    if case['kind'] != 'positive':
        return
    body = case.get('body')
    calls = body if isinstance(body, list) else [body]
    replies = document if isinstance(document, list) else [document]
    for route_id in case.get('route_ids', []):
        route = route_index[route_id]
        if route['transport'] != 'json-rpc':
            continue
        matching = [call for call in calls if isinstance(call, dict) and
                    (call.get('method', '').startswith(route['path'][:-1])
                     if route['path'].endswith('*') else call.get('method') == route['path'])]
        require(matching and any('id' in call and any(
            isinstance(reply, dict) and reply.get('jsonrpc') == '2.0'
            and 'id' in reply and reply['id'] == call['id']
            and 'result' in reply and 'error' not in reply for reply in replies)
            for call in matching), 'positive route has no successful JSON-RPC result')


def run():
    global COUNT
    manifest_path = os.environ.get('PAXEER_X_CANDIDATE_MANIFEST')
    plan_path = os.environ.get('PAXEER_X_ROUTE_CASES')
    require(manifest_path and plan_path, 'real candidate manifest and PAXEER_X_ROUTE_CASES are required')
    manifest = load_private(manifest_path)
    validate(manifest, catalogue(ROOT / 'spec/paxeer-x/spec.kvx'), ROOT, False,
             os.environ.get('PAXEER_X_MAINLINE', 'refs/heads/main'))
    require(not manifest['source']['dirty'], 'route evidence requires clean candidate source')
    for service in manifest['services']:
        require(all(value != 'unknown' for value in service['bindings'].values()),
                'required runtime identity or authority is unknown')
    plan = load_private(plan_path)
    require(plan['schema'] == 'paxeer-x.route-contract.v1', 'unsupported route case schema')
    require(plan['source_revision'] == manifest['source']['revision'] and plan['source_tree'] == manifest['source']['tree'],
            'route corpus belongs to another source')
    require(plan['candidate_sha256'] == hashlib.sha256(canonical(manifest)).hexdigest(),
            'route corpus candidate identity mismatch')
    product = json.loads((ROOT / 'tools/paxeer-x/route-catalogue.json').read_text())
    require(plan['catalogue_sha256'] == hashlib.sha256(canonical(product)).hexdigest(), 'route catalogue identity mismatch')
    require(isinstance(plan['targets'], dict) and plan['targets'], 'route targets absent')
    for target in plan['targets'].values():
        require(target['url'].rstrip('/') == product['canonical_origin'],
                'route evidence must use the canonical unified origin')
    bound = {s['id']: {k:s['bindings'][k] for k in ['image_digest','config_digest','source_revision']} for s in manifest['services']}
    require(plan['service_bindings'] == bound, 'routed image or configuration binding mismatch')
    cases = plan['cases']
    require(isinstance(cases,list) and cases and len(cases) <= 4096, 'route corpus empty or oversized')
    ids = set()
    coverage = {}
    covered_routes = set()
    functional_services = set()
    route_index = {r["id"]:r for r in product["routes"]}
    scenarios = set()
    for case in cases:
        request_path(case)
        require(case['id'] not in ids, 'duplicate routed case')
        ids.add(case['id'])
        require(case['kind'] in KINDS and case['service'] in bound, 'unknown route case selector')
        require(case.get('assertions') or case.get('response_headers') or case.get('response_sha256') or (case.get('transport') == 'websocket' and case.get('messages')),
                'case has no behavioral assertions')
        require(isinstance(case.get('assertions'), dict), 'case assertions must be an object')
        if 'response_sha256' in case:
            require(re.fullmatch('[0-9a-f]{64}', case['response_sha256']) is not None,
                    'invalid response byte digest')
        coverage.setdefault(case['service'],set()).add(case['kind'])
        for route_id in case.get('route_ids',[]):
            require(route_id in route_index and route_matches(route_index[route_id],case),
                    'route coverage claim does not match the executed request')
            if case['kind'] == 'positive':
                covered_routes.add(route_id)
                if route_index[route_id]['path'] != 'px_getRouteCatalogue':
                    functional_services.add(case['service'])
        if isinstance(case.get('body'), list) and len(case['body']) >= 2:
            require(any('/error/' in p for p in case['assertions']) and any('/result' in p for p in case['assertions']), 'mixed batch requires success and failure assertions')
            scenarios.add('mixed-batch')
        if case.get('transport') == 'websocket' and case['kind'] == 'positive':
            scenarios.add('evm-websocket' if case['path'] == '/rpc/evm/ws' else 'native-websocket')
        if case['method'] == 'OPTIONS' and 'Origin' in case.get('headers',{}):
            require(case.get('response_headers',{}).get('Access-Control-Allow-Origin') == case['headers']['Origin'], 'CORS case must assert exact allowed origin')
            scenarios.add('cors')
        if 'certificate_file' in plan['targets'][case['target']]: scenarios.add('tls-client-identity')
        if case['kind'] == 'unauthorized' and case['path'].startswith('/internal/'): scenarios.add('private-publication-refused')
        body = case.get('body')
        if isinstance(body,dict) and body.get('method') in product['node_signing_methods']:
            require(any(value == -32601 for value in case['assertions'].values()), 'node signing must be refused')
            scenarios.add('node-signing-refused')
        if case['kind'] == 'positive' and isinstance(body,dict) and body.get('method') == 'px_resolveAccount':
            for identity in body.get('params',[]):
                if isinstance(identity,str):
                    if identity.startswith('did:'): scenarios.add('identity-did')
                    elif re.fullmatch('0x[0-9a-fA-F]{40}',identity): scenarios.add('identity-evm')
                    elif re.fullmatch('[0-9a-f]{64}',identity): scenarios.add('identity-account')
        if case['kind'] == 'positive': require(case['status'] in {101,200,201,202,204}, 'positive case requires success')
        elif case['kind'] == 'unauthorized': require(case['status'] in {401,403}, 'private route refusal requires authorization status')
        elif case['kind'] in {'wrong-network','missing-dependency'}: require(case['status'] == 503, 'dependency refusal must not succeed')
        elif case['kind'] == 'malformed': require(case['status'] in {200,400,405,413,415,422}, 'invalid malformed case status')
        require(case['target'] in plan['targets'], 'case target unavailable')
    for service in product['services']:
        needed = {'unauthorized'} if service['exposure']=='private' else KINDS
        require(needed <= coverage.get(service['id'],set()), 'service route negative/positive coverage incomplete')
        if service['exposure'] == 'product':
            require(service['id'] in functional_services,
                    'product service has no positive feature route coverage')
    require({r['id'] for r in product['routes']} <= covered_routes, 'published route corpus incomplete')
    require({'mixed-batch','identity-did','identity-evm','identity-account','node-signing-refused','private-publication-refused',
             'tls-client-identity','native-websocket','evm-websocket','cors'} <= scenarios,
            'required transport or identity scenarios absent')
    started = time.monotonic()
    for case in cases:
        require(time.monotonic() - started < 870, 'routed contract time bound reached')
        COUNT += 1
        if case.get('transport') == 'websocket': websocket_request(plan['targets'][case['target']],case)
        else:
            document = http_request(plan['targets'][case['target']],case)
            require_rpc_results(case, document, route_index)
        print('ok route-case ' + str(COUNT), flush=True)


if __name__ == '__main__':
    try:
        run()
    except (Invalid, OSError, ValueError, KeyError, TypeError, http.client.HTTPException) as error:
        print('routes: refusal: ' + (str(error) if isinstance(error,Invalid) else type(error).__name__), file=sys.stderr)
        print(f'PAXEER_X_GATE tests={COUNT} skipped=0')
        sys.exit(1)
    print(f'PAXEER_X_GATE tests={COUNT} skipped=0')
