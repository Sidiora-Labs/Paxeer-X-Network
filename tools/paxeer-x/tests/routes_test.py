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


def phoenix_read_exact(stream, count, deadline):
    require(0 <= count <= 65536, 'Phoenix read bound')
    chunks = bytearray()
    while len(chunks) < count:
        remaining = deadline - time.monotonic()
        require(remaining > 0, 'Phoenix exchange deadline')
        stream.settimeout(min(8, remaining))
        piece = stream.recv(count - len(chunks))
        require(piece, 'Phoenix WebSocket closed before result')
        chunks.extend(piece)
    return bytes(chunks)


def phoenix_send(stream, opcode, data, deadline):
    require(len(data) <= 65535 and (opcode < 8 or len(data) <= 125), 'Phoenix frame bound')
    remaining = deadline - time.monotonic()
    require(remaining > 0, 'Phoenix exchange deadline')
    stream.settimeout(min(8, remaining))
    mask = os.urandom(4)
    frame = bytes([0x80 | opcode, 0x80 | len(data)]) if len(data) < 126 else bytes([0x80 | opcode, 0xfe]) + struct.pack('!H', len(data))
    stream.sendall(frame + mask + bytes(byte ^ mask[index % 4] for index, byte in enumerate(data)))


def phoenix_receive(stream, deadline):
    data = bytearray()
    fragmented = False
    for _ in range(64):
        first, second = phoenix_read_exact(stream, 2, deadline)
        opcode = first & 15
        final = bool(first & 128)
        require(first & 112 == 0 and second & 128 == 0 and opcode in {0, 1, 8, 9, 10},
                'invalid Phoenix WebSocket frame')
        short = second & 127
        length = short if short < 126 else struct.unpack('!H' if short == 126 else '!Q',
                    phoenix_read_exact(stream, 2 if short == 126 else 8, deadline))[0]
        require(length <= 65536 and (short != 126 or length >= 126)
                and (short != 127 or length >= 65536), 'Phoenix response frame bound')
        require(opcode < 8 or (final and length <= 125), 'invalid Phoenix control frame')
        body = phoenix_read_exact(stream, length, deadline)
        if opcode == 8:
            require(False, 'Phoenix closed before expected reply')
        if opcode == 9:
            phoenix_send(stream, 10, body, deadline)
            continue
        if opcode == 10:
            continue
        require((opcode == 1 and not fragmented) or (opcode == 0 and fragmented),
                'invalid Phoenix fragmentation')
        require(len(data) + len(body) <= 65536, 'Phoenix response message bound')
        data.extend(body)
        fragmented = not final
        if final:
            return json.loads(data.decode('utf-8'))
    raise Invalid('Phoenix fragment/control frame count bound')


def phoenix_websocket_request(target, case):
    import base64
    parsed = urllib.parse.urlsplit(target['url'])
    require(parsed.scheme == 'https' and parsed.hostname and not parsed.username
            and not parsed.password and not parsed.query and not parsed.fragment,
            'Phoenix target requires canonical TLS origin')
    require(case['method'] == 'GET' and request_path(case) == '/explorer/backend/socket/v2/websocket',
            'Phoenix v2 requires the Explorer socket route')
    query = urllib.parse.parse_qs(urllib.parse.urlsplit(case['path']).query, keep_blank_values=True)
    if case['status'] == 101:
        require(query.get('vsn') == ['2.0.0'], 'Phoenix v2 serializer version is required')
    key = base64.b64encode(os.urandom(16)).decode()
    expected = base64.b64encode(hashlib.sha1((key + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').encode()).digest()).decode()
    headers = {'host': parsed.netloc, 'upgrade': 'websocket', 'connection': 'Upgrade',
               'sec-websocket-key': key, 'sec-websocket-version': '13'}
    supplied = dict(case.get('headers', {}))
    require(not any(name.lower() in SECRET_HEADERS for name in supplied),
            'Phoenix credentials require protected file references')
    for name, path in case.get('header_files', {}).items():
        require(name not in supplied, 'duplicate Phoenix header source')
        supplied[name] = secret(path)
    for name, value in supplied.items():
        require(isinstance(name, str) and isinstance(value, str)
                and re.fullmatch(r"[!#$%&'*+.^_`|~0-9A-Za-z-]+", name)
                and all(32 <= ord(char) < 127 for char in value)
                and name.lower() not in headers and not name.lower().startswith('sec-websocket-'),
                'invalid or reserved Phoenix request header')
        headers[name.lower()] = value
    head = 'GET ' + parsed.path.rstrip('/') + case['path'] + ' HTTP/1.1\r\n'
    head += ''.join(name + ': ' + value + '\r\n' for name, value in headers.items()) + '\r\n'
    require(len(head) <= 8192, 'Phoenix request header bound')
    deadline = time.monotonic() + 30
    with socket.create_connection((parsed.hostname, parsed.port or 443), timeout=8) as tcp:
        with context(target).wrap_socket(tcp, server_hostname=parsed.hostname) as stream:
            stream.settimeout(8)
            stream.sendall(head.encode('ascii'))
            reply = bytearray()
            while not reply.endswith(b'\r\n\r\n'):
                require(len(reply) < 8192, 'Phoenix upgrade header bound')
                reply.extend(phoenix_read_exact(stream, 1, deadline))
            lines = reply.decode('ascii').split('\r\n')
            status = lines[0].split(' ', 2)
            require(len(status) >= 2 and status[0] == 'HTTP/1.1' and int(status[1]) == case['status'],
                    'unexpected Phoenix upgrade status')
            fields = {}
            for line in lines[1:]:
                if not line:
                    continue
                require(':' in line, 'malformed Phoenix upgrade header')
                name, value = line.split(':', 1)
                require(re.fullmatch(r"[!#$%&'*+.^_`|~0-9A-Za-z-]+", name)
                        and all(char == '\t' or 32 <= ord(char) < 127 for char in value),
                        'invalid Phoenix upgrade header')
                name, value = name.lower(), value.strip()
                if name == 'set-cookie':
                    continue
                require(name not in fields, 'duplicate Phoenix upgrade header')
                fields[name] = value
            for name, value in case.get('response_headers', {}).items():
                require(fields.get(name.lower()) == value, 'Phoenix response header assertion failed')
            if case['status'] != 101:
                require('transfer-encoding' not in fields, 'unexpected Phoenix refusal transfer encoding')
                if case.get('assertions') or 'response_sha256' in case:
                    length = fields.get('content-length', '')
                    require(re.fullmatch(r'0|[1-9][0-9]*', length) is not None
                            and int(length) <= 65536, 'Phoenix refusal body bound')
                    body = phoenix_read_exact(stream, int(length), deadline)
                    if 'response_sha256' in case:
                        require(hashlib.sha256(body).hexdigest() == case['response_sha256'],
                                'Phoenix refusal byte digest mismatch')
                    if case.get('assertions'):
                        document = json.loads(body)
                        for path, value in case['assertions'].items():
                            require(pointer(document, path) == value, 'Phoenix refusal assertion failed')
                return
            require(fields.get('sec-websocket-accept') == expected
                    and fields.get('upgrade', '').lower() == 'websocket'
                    and 'upgrade' in [value.strip().lower() for value in fields.get('connection', '').split(',')]
                    and not any(name in fields for name in ['sec-websocket-protocol', 'sec-websocket-extensions',
                                                            'content-length', 'transfer-encoding']),
                    'Phoenix upgrade negotiation mismatch')
            messages = case.get('messages')
            require(isinstance(messages, list) and 1 <= len(messages) <= 32,
                    'Phoenix message corpus empty or oversized')
            references, joined, events = set(), {}, set()
            for message in messages:
                sent = message['send']
                require(isinstance(sent, list) and len(sent) == 5, 'Phoenix send requires five elements')
                join_ref, ref, topic, event, payload = sent
                require(isinstance(ref, str) and 0 < len(ref) <= 128 and ref not in references
                        and isinstance(topic, str) and 0 < len(topic) <= 256
                        and isinstance(payload, dict) and event in {'phx_join', 'heartbeat'},
                        'invalid Phoenix join or heartbeat')
                require((event == 'heartbeat' and join_ref is None and topic == 'phoenix')
                        or (event == 'phx_join' and join_ref == ref and topic != 'phoenix'),
                        'Phoenix request reference or topic mismatch')
                references.add(ref)
                events.add(event)
                phoenix_send(stream, 1, canonical(sent), deadline)
                document = None
                for _ in range(32):
                    received = phoenix_receive(stream, deadline)
                    require(isinstance(received, list) and len(received) == 5, 'Phoenix reply requires five elements')
                    if received[1] is None:
                        require(isinstance(received[2], str) and received[2] in joined
                                and (received[0] is None or received[0] == joined[received[2]])
                                and isinstance(received[3], str) and received[3] not in {'phx_error', 'phx_close', 'phx_reply'}
                                and isinstance(received[4], dict), 'unbound Phoenix channel event')
                        continue
                    document = received
                    break
                require(document is not None and document[:3] == [join_ref, ref, topic]
                        and document[3] == 'phx_reply' and isinstance(document[4], dict),
                        'Phoenix reply reference, topic, or event mismatch')
                if case['kind'] == 'positive':
                    require(document[4].get('status') == 'ok' and isinstance(document[4].get('response'), dict),
                            'positive Phoenix operation has no successful reply')
                require(isinstance(message.get('assertions'), dict) and message['assertions'],
                        'empty Phoenix behavioral assertions')
                for path, value in message['assertions'].items():
                    require(pointer(document, path) == value, 'Phoenix response assertion failed')
                if event == 'phx_join' and document[4].get('status') == 'ok':
                    joined[topic] = join_ref
            if case['kind'] == 'positive':
                require(events == {'phx_join', 'heartbeat'}, 'positive Phoenix corpus requires join and heartbeat')
            phoenix_send(stream, 8, struct.pack('!H', 1000), deadline)


def websocket_dispatch(target, case):
    protocol = case.get('websocket_protocol', 'json-rpc')
    require(protocol in {'json-rpc', 'phoenix-v2'}, 'unknown WebSocket protocol')
    if protocol == 'phoenix-v2':
        return phoenix_websocket_request(target, case)
    return websocket_request(target, case)


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
        if route['path'].startswith('/'):
            return path == route['path'] and (path != '/explorer/backend/socket/v2/websocket'
                    or case.get('websocket_protocol') == 'phoenix-v2')
        return path == '/rpc/ws' and any(isinstance(message.get('send'), dict) and message['send'].get('method') == route['path'] for message in case.get('messages',[]))
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
                route = route_index[route_id]
                if route.get('response_integrity') == 'sha256':
                    require('response_sha256' in case,
                            'archive byte route requires an exact response digest')
                    headers = {name.lower(): value for name, value in case.get('response_headers', {}).items()}
                    require(all(name.lower() in headers for name in route['required_response_headers']),
                            'archive integrity header assertions absent')
                    require(headers['x-content-sha256'] == case['response_sha256']
                            and headers['etag'] == '"' + case['response_sha256'] + '"',
                            'archive integrity assertions disagree')
                    if 'x-layerx-batch' in headers:
                        require(headers['x-layerx-batch'] == request_path(case).rsplit('/', 1)[-1],
                                'archive batch assertion does not match requested batch')
                covered_routes.add(route_id)
                if route_index[route_id]['path'] != 'px_getRouteCatalogue':
                    functional_services.add(case['service'])
        if isinstance(case.get('body'), list) and len(case['body']) >= 2:
            require(any('/error/' in p for p in case['assertions']) and any('/result' in p for p in case['assertions']), 'mixed batch requires success and failure assertions')
            scenarios.add('mixed-batch')
        if case.get('transport') == 'websocket':
            require(case.get('websocket_protocol', 'json-rpc') in {'json-rpc', 'phoenix-v2'}, 'unknown WebSocket protocol')
            if case['kind'] == 'positive':
                scenarios.add('phoenix-websocket' if case.get('websocket_protocol') == 'phoenix-v2' else
                              ('evm-websocket' if request_path(case) == '/rpc/evm/ws' else 'native-websocket'))
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
        if case.get('transport') == 'websocket': websocket_dispatch(plan['targets'][case['target']],case)
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
