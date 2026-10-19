#!/usr/bin/env python3
"""Real-process readiness qualification; requires the real node and build boundary."""
import argparse
import concurrent.futures
import json
import os
from pathlib import Path
import shutil
import ssl
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import urllib.parse
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', required=True)
    parser.add_argument('--rootfs', required=True)
    parser.add_argument('--url', required=True)
    parser.add_argument('--health-url', required=True)
    parser.add_argument('--ca', required=True)
    parser.add_argument('--cert', required=True)
    parser.add_argument('--key', required=True)
    parser.add_argument('--token-file', required=True)
    parser.add_argument('--build-path', required=True)
    parser.add_argument('--build-body', required=True)
    parser.add_argument('--log', required=True)
    args = parser.parse_args()
    origin = urllib.parse.urlsplit(args.url)
    health_origin = urllib.parse.urlsplit(args.health_url)
    assert health_origin.scheme == 'http' and health_origin.port != origin.port, 'separate plain readiness port'
    for host, port in ((origin.hostname, origin.port or 443), (health_origin.hostname, health_origin.port or 80)):
        try:
            with socket.create_connection((host, port), timeout=1):
                raise RuntimeError('test URL already has a listener')
        except ConnectionRefusedError:
            pass
    context = ssl.create_default_context(cafile=args.ca)
    context.load_cert_chain(args.cert, args.key)
    token = Path(args.token_file).read_text().strip()
    body = Path(args.build_body).read_bytes()

    def verdict(path='/healthz', method='GET'):
        req = urllib.request.Request(args.health_url + path, method=method)
        try:
            with urllib.request.urlopen(req, timeout=10) as response:
                return response.status, response.read()
        except urllib.error.HTTPError as error:
            return error.code, error.read()

    def authenticated_verdict():
        req = urllib.request.Request(args.url + '/healthz')
        try:
            with urllib.request.urlopen(req, context=context, timeout=10) as response:
                return response.status, response.read()
        except urllib.error.HTTPError as error:
            return error.code, error.read()

    def wait_ready(process, limit_seconds):
        limit = time.monotonic() + limit_seconds
        while True:
            if process.poll() is not None:
                raise RuntimeError('registry exited during startup')
            try:
                if request('/healthz')[0] == 200 and verdict()[0] == 200:
                    return
            except (OSError, urllib.error.URLError):
                pass
            if time.monotonic() >= limit:
                raise RuntimeError('registry startup deadline exceeded')
            time.sleep(0.1)

    def request(path, data=None):
        headers = {'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'}
        if data is not None:
            headers['Idempotency-Key'] = str(uuid.uuid4())
        req = urllib.request.Request(args.url + path, data=data,
            headers=headers)
        started = time.monotonic()
        try:
            with urllib.request.urlopen(req, context=context, timeout=1800 if data is not None else 8) as response:
                return response.status, time.monotonic() - started
        except urllib.error.HTTPError as error:
            return error.code, time.monotonic() - started

    with tempfile.TemporaryDirectory(prefix='registry-readiness-') as temporary:
        os.chmod(temporary, 0o755)
        root = Path(temporary) / 'rootfs'
        shutil.copytree(args.rootfs, root)
        if os.geteuid() == 0:
            for path in [root, *root.rglob('*')]:
                os.chown(path, 4030, 4030)
        env = os.environ.copy()
        env['LAYERX_REGISTRY_BUILDER_ENVIRONMENT_ROOT'] = str(root)
        env['LAYERX_REGISTRY_HEALTH_LISTEN'] = '%s:%d' % (health_origin.hostname, health_origin.port)
        with open(args.log, 'wb') as log:
            process = subprocess.Popen([args.binary], env=env, stdout=log, stderr=log)
            try:
                wait_ready(process, 180)
                ready = verdict()
                assert ready == authenticated_verdict(), 'readiness port answers the exact mTLS /healthz verdict'
                assert json.loads(ready[1]) == {'status': 'ready', 'service': 'program-registry'}, ready
                for path, method in (('/v1/programs/registry', 'GET'), ('/metrics', 'GET'),
                                     ('/__registry/sources', 'POST'), ('/healthz', 'POST')):
                    status, body = verdict(path, method)
                    assert status in (404, 405), (path, status)
                    assert token.encode() not in body
                try:
                    urllib.request.urlopen(urllib.request.Request(args.url + '/healthz'),
                                           context=ssl.create_default_context(cafile=args.ca), timeout=8)
                    raise AssertionError('mTLS listener admitted a client without a certificate')
                except (ssl.SSLError, urllib.error.URLError, ConnectionError):
                    pass
                for untrusted in (b'{"record_hex":"4c6179657258"}', b'{"proof_hex":"00"}'):
                    status, _ = request('/__registry/deployments', untrusted)
                    assert status == 503, status
                with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
                    builds = [pool.submit(request, args.build_path, body) for _ in range(2)]
                    latencies = []
                    for _ in range(20):
                        assert any(not build.done() for build in builds), 'build concurrency not established'
                        status, elapsed = request('/healthz')
                        assert status == 200, status
                        assert elapsed < 1, elapsed
                        latencies.append(elapsed)
                    assert any(build.result()[0] == 200 for build in builds), 'no real build succeeded'
                changed = next(path for path in root.rglob('*') if path.is_file() and path.stat().st_size)
                with changed.open('r+b') as output:
                    first = output.read(1)
                    output.seek(0)
                    output.write(bytes([first[0] ^ 1]))
                    output.flush()
                    os.fsync(output.fileno())
                limit = time.monotonic() + 3
                while request('/healthz')[0] != 503:
                    assert time.monotonic() < limit, 'mutated rootfs remained healthy'
                    time.sleep(0.05)
                withdrawn = verdict()
                assert withdrawn[0] == 503, withdrawn
                assert json.loads(withdrawn[1])['error']['code'] == 'builder_unavailable', withdrawn
                assert request(args.build_path, body)[0] == 503
                with changed.open('r+b') as output:
                    output.write(first)
                    output.flush()
                    os.fsync(output.fileno())
                limit = time.monotonic() + 3
                while verdict()[0] != 200 or request('/healthz')[0] != 200:
                    assert time.monotonic() < limit, 'restored rootfs did not regain readiness'
                    time.sleep(0.05)
                process.terminate()
                process.wait(timeout=10)
                try:
                    verdict()
                    raise AssertionError('readiness answered while the registry was down')
                except (ConnectionError, urllib.error.URLError):
                    pass
                process = subprocess.Popen([args.binary], env=env, stdout=log, stderr=log)
                wait_ready(process, 180)
                assert verdict() == authenticated_verdict(), 'restart resumes the exact verified verdict'
                print(json.dumps({'health_samples': len(latencies), 'max_seconds': max(latencies),
                    'mutated_health': 503, 'mutated_build': 503, 'readiness_port_mutated': 503,
                    'restored_health': 200, 'restart_health': 200}))
            finally:
                if process.poll() is None:
                    process.terminate()
                process.wait(timeout=10)


if __name__ == '__main__':
    main()
