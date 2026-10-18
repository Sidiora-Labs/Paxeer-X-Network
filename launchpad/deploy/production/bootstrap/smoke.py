#!/usr/bin/env python3
"""Real local fixture probes. Creates one local EVM tx and temporary test records."""
import hashlib
import hmac
import http.client
import json
from pathlib import Path
import socket
import subprocess
import time
import urllib.request
import uuid
from datetime import datetime, timezone

HERE = Path(__file__).resolve().parent
TOKEN = "fixture_" + uuid.uuid4().hex


def get_json(port, path):
    with urllib.request.urlopen(f"http://127.0.0.1:{port}{path}", timeout=5) as response:
        return json.load(response)


def rpc(method, params):
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode()
    request = urllib.request.Request("http://127.0.0.1:28545", body, {"Content-Type": "application/json"})
    with urllib.request.urlopen(request, timeout=5) as response:
        result = json.load(response)
    if "error" in result:
        raise RuntimeError(f"{method}: {result['error']}")
    return result["result"]


def redis_roundtrip():
    with socket.create_connection(("127.0.0.1", 26379), timeout=5) as conn:
        stream = conn.makefile("rb")
        def command(*parts):
            wire = f"*{len(parts)}\r\n".encode()
            for part in parts:
                value = part.encode()
                wire += f"${len(value)}\r\n".encode() + value + b"\r\n"
            conn.sendall(wire)
            header = stream.readline()
            if header.startswith(b"-"):
                raise RuntimeError(header.decode())
            if header.startswith(b"$"):
                return stream.read(int(header[1:-2]) + 2)[:-2]
            return header.strip()
        assert command("AUTH", "kindle_fixture_password") == b"+OK"
        assert command("SET", TOKEN, "roundtrip", "EX", "60") == b"+OK"
        assert command("GET", TOKEN) == b"roundtrip"
        assert command("DEL", TOKEN) == b":1"


def nats_roundtrip():
    with socket.create_connection(("127.0.0.1", 24222), timeout=5) as conn:
        stream = conn.makefile("rb")
        assert stream.readline().startswith(b"INFO ")
        conn.sendall(b'CONNECT {"user":"kindle_fixture","pass":"kindle_fixture_password","verbose":false}\r\nPING\r\n')
        while True:
            line = stream.readline()
            if line == b"PONG\r\n":
                break
            if line.startswith(b"-ERR") or not line:
                raise RuntimeError(f"NATS connection rejected: {line!r}")
        sid = 0
        def request(subject, payload):
            nonlocal sid
            sid += 1
            inbox = f"_INBOX.{TOKEN}.{sid}"
            data = json.dumps(payload).encode()
            conn.sendall(f"SUB {inbox} {sid}\r\nUNSUB {sid} 1\r\nPUB {subject} {inbox} {len(data)}\r\n".encode() + data + b"\r\n")
            while True:
                line = stream.readline()
                if line == b"PING\r\n":
                    conn.sendall(b"PONG\r\n")
                    continue
                if not line or line.startswith(b"-ERR"):
                    raise RuntimeError(f"NATS request failed: {line!r}")
                if line.startswith(b"MSG "):
                    body = stream.read(int(line.split()[-1]) + 2)[:-2]
                    result = json.loads(body)
                    if "error" in result:
                        raise RuntimeError(result["error"])
                    return result
        name = TOKEN.upper()
        request(f"$JS.API.STREAM.CREATE.{name}", {"name": name, "subjects": [TOKEN], "storage": "file", "num_replicas": 1, "max_age": 60000000000})
        try:
            ack = request(TOKEN, {"probe": TOKEN})
            assert ack["seq"] == 1
            state = request(f"$JS.API.STREAM.INFO.{name}", {})
            assert state["state"]["messages"] == 1
            stored = request(f"$JS.API.STREAM.MSG.GET.{name}", {"seq": 1})
            import base64
            assert json.loads(base64.b64decode(stored["message"]["data"]))["probe"] == TOKEN
        finally:
            request(f"$JS.API.STREAM.DELETE.{name}", {})


def s3_request(method, key, payload=b""):
    # AWS Signature V4 against the local MinIO fixture, never an ambient endpoint.
    timestamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    day = timestamp[:8]
    path = "/kindle-fixture/" + key
    digest = hashlib.sha256(payload).hexdigest()
    headers = {"host": "127.0.0.1:29000", "x-amz-content-sha256": digest, "x-amz-date": timestamp}
    names = ";".join(sorted(headers))
    canonical_headers = "".join(f"{name}:{headers[name]}\n" for name in sorted(headers))
    canonical = "\n".join([method, path, "", canonical_headers, names, digest])
    scope = f"{day}/us-east-1/s3/aws4_request"
    string_to_sign = "\n".join(["AWS4-HMAC-SHA256", timestamp, scope, hashlib.sha256(canonical.encode()).hexdigest()])
    signing = ("AWS4" + "kindle_fixture_password").encode()
    for value in [day, "us-east-1", "s3", "aws4_request"]:
        signing = hmac.new(signing, value.encode(), hashlib.sha256).digest()
    signature = hmac.new(signing, string_to_sign.encode(), hashlib.sha256).hexdigest()
    headers["Authorization"] = f"AWS4-HMAC-SHA256 Credential=kindle_fixture/{scope}, SignedHeaders={names}, Signature={signature}"
    conn = http.client.HTTPConnection("127.0.0.1", 29000, timeout=5)
    try:
        conn.request(method, path, payload, headers)
        response = conn.getresponse()
        body = response.read()
        if response.status not in [200, 204]:
            raise RuntimeError(f"S3 {method}: HTTP {response.status} {body[:500]!r}")
        return body
    finally:
        conn.close()


def main():
    # Run static safety checks before connecting to any fixture.
    subprocess.run(["python3", str(HERE / "validate.py")], check=True)
    result = subprocess.run(["docker", "compose", "-f", str(HERE / "compose.yml"), "exec", "-T", "postgres", "psql", "-v", "ON_ERROR_STOP=1", "-U", "kindle_fixture", "-d", "kindle_fixture", "-Atc", "BEGIN; CREATE TEMP TABLE fixture_probe(value text); INSERT INTO fixture_probe VALUES ('roundtrip'); SELECT value FROM fixture_probe; ROLLBACK;"], capture_output=True, text=True, check=True)
    assert "roundtrip" in result.stdout.splitlines()
    redis_roundtrip()
    assert get_json(28222, "/healthz?js-enabled-only=true")["status"] == "ok"
    nats_roundtrip()
    payload = TOKEN.encode()
    s3_request("PUT", TOKEN, payload)
    try:
        assert s3_request("GET", TOKEN) == payload
    finally:
        s3_request("DELETE", TOKEN)
    assert rpc("eth_chainId", []) == "0x7d", "Wrong local fixture chain ID"
    accounts = rpc("eth_accounts", [])
    assert accounts[0].lower() == "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266", "Unexpected fixture account; refusing transaction"
    transaction = rpc("eth_sendTransaction", [{"from": accounts[0], "to": accounts[1], "value": "0x1", "gas": "0x5208"}])
    receipt = None
    for _ in range(50):
        receipt = rpc("eth_getTransactionReceipt", [transaction])
        if receipt:
            break
        time.sleep(0.1)
    assert receipt and receipt["status"] == "0x1", "Fixture transfer did not succeed"
    print(json.dumps({"status": "pass", "checks": ["postgres_transaction", "redis_roundtrip", "jetstream_file_publish_read", "s3_signed_roundtrip", "chain125_real_transfer_receipt"], "fixture_transaction": transaction, "chain_id": 125}))


if __name__ == "__main__":
    main()
