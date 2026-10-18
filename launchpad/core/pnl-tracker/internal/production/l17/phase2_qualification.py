#!/usr/bin/env python3
import argparse
import asyncio
import base64
import csv
import datetime
import hashlib
import http.client
import io
import ipaddress
import json
import math
import os
import pathlib
import re
import ssl
import subprocess
import sys
import time
import urllib.parse

ROOT = pathlib.Path(__file__).resolve().parents[5]
ALGORITHM = "proportional_floor_v1"
HEX64 = re.compile(r"^[0-9a-f]{64}$")
ADDRESS = re.compile(r"^0x[0-9a-f]{40}$")
UUID = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[45][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")
INTEGER = re.compile(r"^(0|-?[1-9][0-9]*)$")


def require(condition, message):
    if not condition:
        raise ValueError(message)


def strict_json(raw, monetary=True):
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, "duplicate JSON field")
            result[key] = value
        return result
    def invalid(value):
        raise ValueError("noninteger JSON number")
    return json.loads(raw, object_pairs_hook=pairs, parse_float=invalid if monetary else float, parse_constant=invalid)


def raw_integer(value, signed=False):
    require(isinstance(value, str) and INTEGER.fullmatch(value) and len(value.lstrip("-")) <= 156, "noncanonical raw amount")
    number = int(value)
    require(signed or number >= 0, "negative unsigned raw amount")
    return number


def percentile(values, percentage):
    require(bool(values), "measurement has no samples")
    ordered = sorted(values)
    return ordered[math.ceil(len(ordered) * percentage / 100) - 1]


def utc_now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def checked_url(value, realm):
    url = urllib.parse.urlsplit(value)
    require(url.hostname and not url.username and not url.password and not url.fragment, "invalid measurement endpoint")
    if url.scheme != "https":
        loopback = url.hostname == "localhost"
        try:
            loopback = loopback or ipaddress.ip_address(url.hostname).is_loopback
        except ValueError:
            pass
        require(realm == "fixture" and url.scheme == "http" and loopback, "plaintext is restricted to explicitly selected loopback fixture")
    require("\r" not in value and "\n" not in value, "invalid endpoint framing")
    return url


class Evidence:
    def __init__(self, directory):
        self.directory = pathlib.Path(directory).resolve()
        require(not self.directory.is_relative_to(ROOT), "measurement output must be a private external directory")
        self.directory.mkdir(mode=0o700, parents=True, exist_ok=False)
        self.raw = self.directory / "raw"
        self.raw.mkdir(mode=0o700)
        self.observations = open(self.directory / "observations.jsonl", "x", encoding="utf-8")
        os.chmod(self.directory / "observations.jsonl", 0o600)
        self.bytes = 0
        self.artifacts = {}

    def capture(self, payload, suffix="bin"):
        digest = hashlib.sha256(payload).hexdigest()
        name = digest + "." + suffix
        if name not in self.artifacts:
            require(self.bytes + len(payload) <= 256 * 1024 * 1024, "raw evidence resource budget exhausted")
            path = self.raw / name
            with open(path, "xb") as output:
                os.chmod(path, 0o600)
                output.write(payload)
            self.bytes += len(payload)
            self.artifacts[name] = {"sha256": digest, "bytes": len(payload)}
        return {"path": "raw/" + name, "sha256": digest, "bytes": len(payload)}

    def record(self, value):
        self.observations.write(json.dumps(value, separators=(",", ":"), allow_nan=False) + "\n")

    def finish(self, result):
        self.observations.flush()
        os.fsync(self.observations.fileno())
        self.observations.close()
        result["artifacts"] = self.artifacts
        result["observations_sha256"] = hashlib.sha256((self.directory / "observations.jsonl").read_bytes()).hexdigest()
        path = self.directory / "measurement.json"
        with open(path, "x", encoding="utf-8") as output:
            os.chmod(path, 0o600)
            json.dump(result, output, indent=2, allow_nan=False)
            output.write("\n")
            output.flush()
            os.fsync(output.fileno())
        return path


class HTTPConnection:
    def __init__(self, url, headers, realm):
        self.url = checked_url(url, realm)
        self.headers = dict(headers)
        self.reader = None
        self.writer = None

    async def close(self):
        if self.writer is not None:
            self.writer.close()
            try:
                await self.writer.wait_closed()
            except (ConnectionError, ssl.SSLError):
                pass
        self.reader = self.writer = None

    async def request(self, path=None, method="GET", body=b""):
        if self.writer is None:
            secure = self.url.scheme == "https"
            self.reader, self.writer = await asyncio.open_connection(self.url.hostname, self.url.port or (443 if secure else 80), ssl=ssl.create_default_context() if secure else None, server_hostname=self.url.hostname if secure else None, limit=65536)
        target = path or urllib.parse.urlunsplit(("", "", self.url.path or "/", self.url.query, ""))
        require(target.startswith("/") and "\r" not in target and "\n" not in target, "invalid HTTP target")
        host = self.url.netloc
        headers = {"Host": host, "Accept": "application/json", "Accept-Encoding": "identity", "Connection": "keep-alive", **self.headers}
        if body:
            headers["Content-Type"] = "application/json"
            headers["Content-Length"] = str(len(body))
        for key, value in headers.items():
            require(re.fullmatch(r"[A-Za-z0-9-]+", key) and "\r" not in value and "\n" not in value, "invalid HTTP header")
        wire = method + " " + target + " HTTP/1.1\r\n" + "".join(key + ": " + value + "\r\n" for key, value in headers.items()) + "\r\n"
        self.writer.write(wire.encode("ascii") + body)
        await self.writer.drain()
        head = await self.reader.readuntil(b"\r\n\r\n")
        lines = head.decode("iso-8859-1").split("\r\n")
        status = int(lines[0].split(" ")[1])
        response_headers = {}
        for line in lines[1:]:
            if line:
                key, value = line.split(":", 1)
                key = key.lower()
                require(key not in response_headers or key not in {"content-length", "transfer-encoding", "content-encoding", "server-timing"}, "ambiguous response framing or timing header")
                response_headers[key] = value.strip()
        require(response_headers.get("content-encoding", "identity") == "identity", "unexpected content encoding")
        maximum = 2000000
        data = bytearray()
        if response_headers.get("transfer-encoding") == "chunked":
            while True:
                line = await self.reader.readuntil(b"\r\n")
                require(len(line) <= 128, "oversized chunk header")
                count = int(line.split(b";", 1)[0], 16)
                require(count >= 0 and len(data) + count <= maximum, "response exceeds bounded capture")
                if count == 0:
                    trailer_bytes = 0
                    while True:
                        trailer = await self.reader.readuntil(b"\r\n")
                        trailer_bytes += len(trailer)
                        require(trailer_bytes <= 65536, "HTTP trailers exceed bounded framing")
                        if trailer == b"\r\n":
                            break
                    break
                data.extend(await self.reader.readexactly(count))
                require(await self.reader.readexactly(2) == b"\r\n", "invalid chunk framing")
        else:
            require("content-length" in response_headers, "response requires bounded HTTP framing")
            count = int(response_headers["content-length"])
            require(0 <= count <= maximum and "transfer-encoding" not in response_headers, "invalid response length")
            data.extend(await self.reader.readexactly(count))
        if response_headers.get("connection", "").lower() == "close":
            await self.close()
        return status, response_headers, bytes(data)


def headers_from_environment(specification):
    result = {}
    for name, variable in specification.items():
        require(isinstance(variable, str) and variable in os.environ and os.environ[variable], "required credential environment missing")
        result[name] = os.environ[variable]
    return result


def bind_sources(configuration):
    expected = configuration["source_sha256"]
    required = {str(path.relative_to(ROOT)) for path in (ROOT / "core/pnl-tracker").rglob("*.go")}
    required.update({str(path.relative_to(ROOT)) for path in (ROOT / "core/pnl-tracker").rglob("*.py")})
    required.update({"core/pnl-tracker/go.mod", "core/pnl-tracker/go.sum"})
    require(required.issubset(expected), "release source manifest omits portfolio source")
    observed = {}
    for relative, digest in expected.items():
        path = (ROOT / relative).resolve()
        require(path.is_relative_to(ROOT) and path.is_file() and HEX64.fullmatch(digest), "invalid release source identity")
        actual = hashlib.sha256(path.read_bytes()).hexdigest()
        require(actual == digest, "source differs from admitted release manifest: " + relative)
        observed[relative] = actual
    bundle = (ROOT / configuration["interface_bundle"]).resolve()
    require(bundle.is_relative_to(ROOT) and bundle.is_file(), "missing admitted interface bundle")
    require(hashlib.sha256(bundle.read_bytes()).hexdigest() == configuration["interface_sha256"], "interface bundle differs from admission")
    require(HEX64.fullmatch(configuration["release_digest"]), "missing release digest")
    return observed


def portfolio_document(raw, wallet):
    document = strict_json(raw)
    data = document["data"]
    summary = data["summary"]
    items = data["items"] if "items" in data else data["positions"]
    require(summary["wallet"] == wallet and UUID.fullmatch(summary["snapshotId"]), "portfolio identity mismatch")
    require(document["quoteAsset"] == "USDL" and document["decimals"] == 6 and document["algorithm"] == ALGORITHM, "portfolio amount methodology mismatch")
    require(isinstance(items, list) and len(items) <= 100, "unbounded portfolio page")
    forbidden = {"aliases", "groups", "sessionId", "accountId", "privateAlias"}
    def privacy(value):
        if isinstance(value, dict):
            require(not forbidden.intersection(value), "public portfolio leaks private fields")
            for child in value.values():
                privacy(child)
        elif isinstance(value, list):
            for child in value:
                privacy(child)
    privacy(document)
    seen = set()
    for position in items:
        require(position["wallet"] == wallet and position["chainId"] == "125" and ADDRESS.fullmatch(position["tokenAddress"]), "position canonical identity mismatch")
        require(position["positionId"] not in seen, "duplicate position row")
        seen.add(position["positionId"])
        balance = raw_integer(position["balanceRaw"])
        known = raw_integer(position["knownQuantityRaw"])
        unknown = raw_integer(position["unknownQuantityRaw"])
        require(balance == known + unknown, "inventory reconciliation mismatch")
        if unknown:
            require(position["remainingCostUsdlRaw"] is None and position["unrealizedPnlRaw"] is None, "unknown basis invented complete monetary result")
        if position["pricingQuality"] != "priced":
            require(position["unrealizedPnlRaw"] is None, "unavailable price invented PnL")
        if position["basisQuality"] != "complete" or position["realizedBasisQuality"] != "complete" or position["pricingQuality"] != "priced":
            require(position["positionId"] in document["positionErrors"], "partial position omitted error state")
    return document, summary, items


def origin_duration(headers):
    timing = headers.get("server-timing", "")
    match = re.search(r"(?:^|,)\s*portfolio_origin;dur=([0-9]+(?:\.[0-9]+)?)", timing)
    require(match is not None, "actual origin measurement header missing")
    duration = float(match[1])
    require(math.isfinite(duration) and duration >= 0, "invalid actual origin duration")
    return duration


async def one_request(configuration, evidence, url, label, headers=None, path=None):
    connection = HTTPConnection(url, headers or {}, configuration["realm"])
    started = time.perf_counter_ns()
    try:
        status, response_headers, body = await asyncio.wait_for(connection.request(path), timeout=5)
        artifact = evidence.capture(body)
        evidence.record({"kind": label, "status": status, "duration_ns": time.perf_counter_ns() - started, "body": artifact, "observed_at": utc_now()})
        return status, response_headers, body
    finally:
        await connection.close()


def database_measurement(configuration, summary, evidence):
    variable = configuration["database_dsn_env"]
    require(variable in os.environ and os.environ[variable], "actual PostgreSQL credential missing")
    schema = configuration["database_schema"]
    require(re.fullmatch(r"[a-z][a-z0-9_]{0,62}", schema), "invalid owned PostgreSQL schema")
    snapshot = summary["snapshotId"]
    wallet = summary["wallet"]
    environment = dict(os.environ, PGDATABASE=os.environ[variable], PGOPTIONS="-c default_transaction_read_only=on -c statement_timeout=400 -c lock_timeout=400 -c search_path=" + schema)
    queries = {
        "snapshot_rows": "SELECT json_build_object('rows',count(*)) FROM portfolio_snapshot_rows WHERE snapshot_id='" + snapshot + "'",
        "indexed_page": "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT payload FROM portfolio_snapshot_rows WHERE snapshot_id='" + snapshot + "' AND ordinal>-1 ORDER BY ordinal LIMIT 100",
        "indexed_latest": "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT id,revision,expires_at FROM portfolio_snapshots WHERE wallet='" + wallet + "' ORDER BY revision DESC LIMIT 1",
        "notification_state": "SELECT json_build_object('pending',count(*) FILTER(WHERE published_at IS NULL),'total',count(*)) FROM portfolio_outbox WHERE event_id='" + snapshot + "'",
        "history_rows": "SELECT json_build_object('rows',row_count) FROM portfolio_history_snapshots WHERE id='" + snapshot + "'",
    }
    result = {}
    for name, sql in queries.items():
        started = time.perf_counter_ns()
        process = subprocess.run(["psql", "-X", "-q", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-c", sql], env=environment, capture_output=True, timeout=5, check=False)
        artifact = evidence.capture(process.stdout, "json")
        evidence.record({"kind": "postgres_" + name, "exit_code": process.returncode, "duration_ns": time.perf_counter_ns() - started, "stdout": artifact, "stderr_sha256": hashlib.sha256(process.stderr).hexdigest()})
        require(process.returncode == 0, "actual PostgreSQL query failed: " + name)
        result[name] = strict_json(process.stdout, monetary=name not in {"indexed_page", "indexed_latest"})
    require(result["snapshot_rows"]["rows"] == summary["positionCount"] >= 10000, "capacity fixture requires actual committed 10000-position snapshot")
    def bounded_plan(node):
        require(not (node.get("Node Type") == "Seq Scan" and node.get("Relation Name") == "portfolio_snapshot_rows"), "first-page query scanned full position relation")
        for child in node.get("Plans", []):
            bounded_plan(child)
    bounded_plan(result["indexed_page"][0]["Plan"])
    require(result["indexed_page"][0]["Plan"]["Actual Rows"] <= 100, "first-page query returned unbounded work")
    require(result["history_rows"]["rows"] >= 100001, "export fixture lacks actual committed 100001 history rows")
    return result


async def canonical_chain(configuration, evidence, summary, items):
    endpoints = configuration["rpc_urls"]
    require(len(endpoints) >= 2 and len(set(endpoints)) == len(endpoints), "two independent actual RPC endpoints required")
    height = raw_integer(summary["watermark"]["height"])
    block = hex(height)
    observed = []
    for endpoint in endpoints:
        connection = HTTPConnection(endpoint, {}, configuration["realm"])
        async def rpc(method, parameters):
            request = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": parameters}).encode()
            status, _, raw = await asyncio.wait_for(connection.request(method="POST", body=request), 5)
            evidence.record({"kind": "canonical_rpc", "method": method, "endpoint_sha256": hashlib.sha256(endpoint.encode()).hexdigest(), "status": status, "body": evidence.capture(raw, "json")})
            document = strict_json(raw)
            require(status == 200 and document.get("id") == 1 and "error" not in document and document.get("result") is not None, "canonical RPC unavailable")
            return document["result"]
        try:
            require(int(await rpc("eth_chainId", []), 16) == 125, "RPC chain identity mismatch")
            header = await rpc("eth_getBlockByNumber", [block, False])
            require(header["hash"].lower() == summary["watermark"]["blockHash"].lower(), "node disagreement with committed snapshot")
            observed.append(header["hash"])
            for position in items:
                data = "0x70a08231" + summary["wallet"][2:].rjust(64, "0")
                balance = await rpc("eth_call", [{"to": position["tokenAddress"], "data": data}, block])
                require(int(balance, 16) == raw_integer(position["balanceRaw"]), "portfolio balance differs from actual canonical token balance")
        finally:
            await connection.close()
    return {"height": str(height), "block_hashes": observed, "positions_reconciled": len(items)}


async def run_load(configuration, evidence, wallet):
    duration = configuration["duration_seconds"]
    workers = configuration["workers"]
    require(type(duration) is int and 30 <= duration <= 300, "measurement duration must be 30..300 seconds")
    require(type(workers) is int and 1 <= workers <= 256, "measurement concurrency must be 1..256")
    queue = asyncio.Queue(maxsize=workers * 2)
    offered = configuration["offered_rps"]
    require(type(offered) is int and 2000 <= offered <= 10000, "offered workload must be explicitly selected within 2000..10000 reads/s")
    requested = offered * duration
    elapsed = []
    origins = []
    late = []
    failures = []
    dropped = 0
    began = time.perf_counter_ns()
    async def worker():
        connection = HTTPConnection(configuration["portfolio_url"], {}, configuration["realm"])
        try:
            while True:
                work = await queue.get()
                if work is None:
                    queue.task_done()
                    return
                index, planned = work
                started = time.perf_counter_ns()
                row = {"kind": "portfolio_load", "index": index, "schedule_ns": planned - began, "started_ns": started - began}
                try:
                    status, headers, body = await asyncio.wait_for(connection.request(), 1)
                    require(status == 200, "portfolio read unavailable")
                    _, summary, _ = portfolio_document(body, wallet)
                    origin = origin_duration(headers)
                    origins.append(origin)
                    row.update({"status": status, "origin_ms": origin, "snapshot_id": summary["snapshotId"], "revision": summary["watermark"]["projectionRevision"], "body_sha256": hashlib.sha256(body).hexdigest(), "body_sample": evidence.capture(body, "json") if index % offered == 0 else None})
                except Exception as error:
                    row["failure"] = type(error).__name__
                    failures.append(index)
                    await connection.close()
                finally:
                    finished = time.perf_counter_ns()
                    sample = (finished - planned) / 1_000_000
                    elapsed.append(sample)
                    late.append((started - planned) / 1_000_000)
                    row["latency_from_schedule_ms"] = sample
                    row["latency_from_start_ms"] = (finished - started) / 1_000_000
                    row["dispatch_lag_ms"] = late[-1]
                    evidence.record(row)
                    queue.task_done()
        finally:
            await connection.close()
    tasks = [asyncio.create_task(worker()) for _ in range(workers)]
    for index in range(requested):
        planned = began + index * 1_000_000_000 // offered
        delay = (planned - time.perf_counter_ns()) / 1_000_000_000
        if delay > 0:
            await asyncio.sleep(delay)
        try:
            queue.put_nowait((index, planned))
        except asyncio.QueueFull:
            dropped += 1
            evidence.record({"kind": "portfolio_load", "index": index, "failure": "generator_queue_saturated", "schedule_ns": planned - began})
    await queue.join()
    for _ in tasks:
        await queue.put(None)
    await asyncio.gather(*tasks)
    measured_seconds = (time.perf_counter_ns() - began) / 1_000_000_000
    successful = len(elapsed) - len(failures)
    result = {"target_rps": 2000, "offered_rps": offered, "scheduled": requested, "completed": len(elapsed), "successful": successful, "failed": len(failures), "generator_dropped": dropped, "elapsed_seconds": measured_seconds, "successful_rps": successful / measured_seconds, "client_p95_ms": percentile(elapsed, 95), "client_p99_ms": percentile(elapsed, 99), "dispatch_p99_ms": percentile(late, 99), "origin_p95_ms": percentile(origins, 95) if origins else None, "origin_p99_ms": percentile(origins, 99) if origins else None, "shared_10000_dynamic_rps_acceptance": "deferred_to_serial_9.1"}
    evidence.record({"kind": "capacity_summary", **result})
    require(not failures and dropped == 0, "load has failures or omitted requests")
    require(result["successful_rps"] >= 2000, "measured successful portfolio throughput below 2000 reads/s")
    require(result["origin_p95_ms"] is not None and result["origin_p99_ms"] is not None and result["origin_p95_ms"] <= 150 and result["origin_p99_ms"] <= 400, "origin latency exceeds declared bounds")
    return result


def action(configuration, evidence, specification, allowed):
    require(allowed and configuration["realm"] == "fixture", "fixture action requires explicit runtime authorization")
    argv = specification["argv"]
    require(isinstance(argv, list) and argv and all(isinstance(value, str) and "\0" not in value for value in argv), "invalid actual fixture action")
    executable = pathlib.Path(argv[0]).resolve()
    require(executable.is_file() and hashlib.sha256(executable.read_bytes()).hexdigest() == specification["executable_sha256"], "fixture executable differs from admission")
    inputs = specification["artifact_sha256"]
    require(isinstance(inputs, dict) and inputs, "fixture executable inputs must be bound to admission")
    for value, expected in inputs.items():
        artifact = pathlib.Path(value)
        if not artifact.is_absolute():
            artifact = ROOT / artifact
        require(artifact.is_file() and HEX64.fullmatch(expected) and hashlib.sha256(artifact.read_bytes()).hexdigest() == expected, "fixture input artifact differs from admission")
    for value in argv[1:]:
        candidate = pathlib.Path(value.split("=", 1)[-1])
        if not candidate.is_absolute():
            candidate = ROOT / candidate
        if candidate.is_file():
            require(value.split("=", 1)[-1] in inputs, "fixture argv file is not bound to admission")
    started = time.perf_counter_ns()
    process = subprocess.run(argv, cwd=ROOT, capture_output=True, timeout=120, check=False)
    evidence.record({"kind": "fixture_action", "executable_sha256": specification["executable_sha256"], "exit_code": process.returncode, "duration_ns": time.perf_counter_ns() - started, "stdout": evidence.capture(process.stdout), "stderr": evidence.capture(process.stderr)})
    require(process.returncode == 0, "actual fixture action failed")


async def pagination_measurement(configuration, evidence, wallet, allowed):
    first_status, _, first_raw = await one_request(configuration, evidence, configuration["portfolio_url"], "pagination_before_trade")
    require(first_status == 200, "first portfolio page unavailable")
    first, summary, items = portfolio_document(first_raw, wallet)
    cursor = first.get("nextCursor") or first["data"].get("next")
    require(isinstance(cursor, str) and cursor, "two-page fixture needs a real next cursor")
    action(configuration, evidence, configuration["new_trade_action"], allowed)
    url = urllib.parse.urlsplit(configuration["portfolio_url"])
    query = urllib.parse.parse_qs(url.query, strict_parsing=True)
    query["cursor"] = [cursor]
    path = url.path + "?" + urllib.parse.urlencode(query, doseq=True)
    status, _, second_raw = await one_request(configuration, evidence, configuration["portfolio_url"], "pagination_after_trade", path=path)
    require(status == 200, "second portfolio page unavailable")
    _, second_summary, second_items = portfolio_document(second_raw, wallet)
    require(second_summary["snapshotId"] == summary["snapshotId"] and second_summary["watermark"]["projectionRevision"] == summary["watermark"]["projectionRevision"], "new trade mixed cursor revisions")
    identities = [row["positionId"] for row in items + second_items]
    require(len(identities) == len(set(identities)), "pagination duplicated a row")
    schema = configuration["database_schema"]
    require(re.fullmatch(r"[a-z][a-z0-9_]{0,62}", schema), "invalid pagination database schema")
    variable = configuration["database_dsn_env"]
    require(variable in os.environ, "actual pagination database credential missing")
    environment = dict(os.environ, PGDATABASE=os.environ[variable], PGOPTIONS="-c default_transaction_read_only=on -c statement_timeout=400 -c search_path=" + schema)
    sql = "SELECT json_agg(payload->>'positionId' ORDER BY ordinal) FROM (SELECT ordinal,payload FROM portfolio_snapshot_rows WHERE snapshot_id='" + summary["snapshotId"] + "' ORDER BY ordinal LIMIT " + str(len(identities)) + ") selected"
    process = await asyncio.to_thread(subprocess.run, ["psql", "-X", "-q", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-c", sql], env=environment, capture_output=True, timeout=5, check=False)
    evidence.record({"kind": "pagination_actual_row_order", "exit_code": process.returncode, "stdout": evidence.capture(process.stdout, "json"), "stderr_sha256": hashlib.sha256(process.stderr).hexdigest()})
    require(process.returncode == 0 and strict_json(process.stdout) == identities, "pagination omitted or reordered actual snapshot rows")
    latest_status, _, latest_raw = await one_request(configuration, evidence, configuration["portfolio_url"], "pagination_new_revision")
    require(latest_status == 200, "post-trade latest unavailable")
    _, latest, _ = portfolio_document(latest_raw, wallet)
    require(int(latest["watermark"]["projectionRevision"]) > int(summary["watermark"]["projectionRevision"]), "fixture action did not commit a new portfolio revision")
    return {"original_snapshot": summary["snapshotId"], "new_snapshot": latest["snapshotId"], "distinct_rows": len(identities)}


def download_export(configuration, evidence):
    specification = configuration["export"]
    url = checked_url(specification["url"], configuration["realm"])
    headers = headers_from_environment(specification["headers_env"])
    connection = http.client.HTTPSConnection(url.hostname, url.port or 443, timeout=5, context=ssl.create_default_context()) if url.scheme == "https" else http.client.HTTPConnection(url.hostname, url.port or 80, timeout=5)
    target = urllib.parse.urlunsplit(("", "", url.path or "/", url.query, ""))
    digest = hashlib.sha256()
    rows = 0
    bytes_read = 0
    started = time.perf_counter_ns()
    output_path = evidence.directory / "export-download.private"
    try:
        connection.request("GET", target, headers=headers)
        response = connection.getresponse()
        require(response.status == 200 and response.getheader("Cache-Control") == "private, no-store", "completed owner export unavailable or cacheable")
        with open(output_path, "xb") as output:
            os.chmod(output_path, 0o600)
            pending = b""
            while True:
                require(time.perf_counter_ns() - started <= 240_000_000_000, "export download exceeded bounded lifetime")
                chunk = response.read(65536)
                if not chunk:
                    break
                bytes_read += len(chunk)
                require(bytes_read <= 256 * 1024 * 1024, "export download exceeds resource budget")
                digest.update(chunk)
                output.write(chunk)
                pending += chunk
                while b"\n" in pending:
                    line, pending = pending.split(b"\n", 1)
                    if specification["format"] == "jsonl":
                        record = strict_json(line)
                        raw_integer(record["amountRaw"])
                        require(record["position"]["chainId"] == "125" and record["asset"]["decimals"] is not None, "export row lacks canonical identity/decimals")
                        rows += 1
                require(len(pending) <= 1 << 20, "unbounded export record")
            require(not pending, "unfinished export line")
        if specification["format"] == "csv":
            with open(output_path, newline="", encoding="utf-8") as csv_file:
                for row in csv.DictReader(csv_file):
                    raw_integer(row["amount_raw"])
                    require(row["chain_id"] == "125" and row["basis_quality"] in {"complete", "partial", "unknown"}, "export CSV identity or coverage mismatch")
                    rows += 1
        require(rows == specification["expected_rows"] >= 100001 and digest.hexdigest() == specification["expected_sha256"], "export differs from actual completed manifest")
        observation = {"kind": "export_download", "rows": rows, "sha256": digest.hexdigest(), "bytes": bytes_read, "duration_ns": time.perf_counter_ns() - started}
        evidence.record(observation)
        return observation
    finally:
        connection.close()


def redis_ping(configuration, evidence):
    specification = configuration["cache"]
    variable = specification["redis_url_env"]
    require(variable in os.environ, "actual Redis endpoint missing")
    endpoint = urllib.parse.urlsplit(os.environ[variable])
    require(endpoint.hostname and endpoint.scheme in {"redis", "rediss"}, "invalid actual Redis endpoint")
    loopback = endpoint.hostname == "localhost"
    try:
        loopback = loopback or ipaddress.ip_address(endpoint.hostname).is_loopback
    except ValueError:
        pass
    require(endpoint.scheme == "rediss" or (configuration["realm"] == "fixture" and loopback), "Redis transport authority missing")
    environment = dict(os.environ)
    if endpoint.password:
        environment["REDISCLI_AUTH"] = urllib.parse.unquote(endpoint.password)
    command = ["redis-cli", "-h", endpoint.hostname, "-p", str(endpoint.port or 6379), "--raw"]
    if endpoint.username:
        command += ["--user", urllib.parse.unquote(endpoint.username)]
    if endpoint.scheme == "rediss":
        certificate = pathlib.Path(specification["ca_path"])
        require(certificate.is_file() and hashlib.sha256(certificate.read_bytes()).hexdigest() == specification["ca_sha256"], "admitted Redis CA authority missing")
        command += ["--tls", "--cacert", str(certificate)]
    command += ["PING"]
    process = subprocess.run(command, env=environment, capture_output=True, timeout=3, check=False)
    evidence.record({"kind": "actual_redis_ping", "exit_code": process.returncode, "stdout": evidence.capture(process.stdout), "stderr_sha256": hashlib.sha256(process.stderr).hexdigest()})
    return process.returncode == 0 and process.stdout.strip() == b"PONG"


async def cache_measurement(configuration, evidence, allowed):
    specification = configuration["cache"]
    require(await asyncio.to_thread(redis_ping, configuration, evidence), "actual cache unavailable before outage fixture")
    status, _, before_raw = await one_request(configuration, evidence, specification["portfolio_url"], "cache_before_loss")
    require(status == 200, "portfolio unavailable before actual cache loss")
    _, before, before_items = portfolio_document(before_raw, configuration["wallet"])
    await asyncio.to_thread(action, configuration, evidence, specification["loss_action"], allowed)
    try:
        require(not await asyncio.to_thread(redis_ping, configuration, evidence), "fixture action did not remove actual Redis dependency")
        status, headers, after_raw = await one_request(configuration, evidence, specification["portfolio_url"], "cache_origin_after_loss")
        require(status == 200, "actual Redis outage lost durable portfolio availability")
        _, after, after_items = portfolio_document(after_raw, configuration["wallet"])
        require(before == after and before_items == after_items, "actual Redis outage lost or changed business data")
        require(origin_duration(headers) <= 400, "Redis bypass exceeded bounded origin budget")
        return {"snapshot_id": after["snapshotId"], "revision": after["watermark"]["projectionRevision"], "origin_ms": origin_duration(headers), "business_state_unchanged": True}
    finally:
        await asyncio.to_thread(action, configuration, evidence, specification["restore_action"], allowed)
        require(await asyncio.to_thread(redis_ping, configuration, evidence), "actual cache restore failed")


async def export_measurement(configuration, evidence, allowed):
    specification = configuration["export"]
    headers = headers_from_environment(specification["headers_env"])
    status, _, raw = await one_request(configuration, evidence, specification["status_url"], "export_completed_manifest", headers)
    require(status == 200, "actual completed export status unavailable")
    completed = strict_json(raw)
    require(completed["state"] == "complete" and completed["manifest"] is not None, "partial export advertised downloadable")
    manifest = completed["manifest"]
    expires = datetime.datetime.fromisoformat(completed["expiresAt"].replace("Z", "+00:00"))
    lifetime = (expires - datetime.datetime.now(datetime.timezone.utc)).total_seconds()
    require(0 < lifetime <= 900 and manifest["format"] in {"jsonl", "csv"}, "export download lifetime or format mismatch")
    require(manifest["rows"] >= 100001 and HEX64.fullmatch(manifest["sha256"]), "actual durable export manifest lacks required rows/digest")
    require(UUID.fullmatch(completed["id"]), "export identity is guessable or malformed")
    actual_configuration = dict(configuration)
    actual_configuration["export"] = {**specification, "expected_rows": manifest["rows"], "expected_sha256": manifest["sha256"], "format": manifest["format"]}
    download = await asyncio.to_thread(download_export, actual_configuration, evidence)
    status, _, _ = await one_request(configuration, evidence, specification["url"], "export_cross_wallet_download", headers_from_environment(specification["other_owner_headers_env"]))
    require(status in {403, 404}, "cross-wallet export download succeeded")
    unfinished = specification["unfinished"]
    status, _, pending_raw = await one_request(configuration, evidence, unfinished["status_url"], "export_unfinished_manifest", headers)
    require(status == 200, "unfinished actual export unavailable")
    pending = strict_json(pending_raw)
    require(pending["state"] in {"queued", "running"} and pending.get("manifest") is None and UUID.fullmatch(pending["id"]), "unfinished export marked complete")
    status, _, _ = await one_request(configuration, evidence, unfinished["download_url"], "export_unfinished_download_denied", headers)
    require(status in {403, 404}, "unfinished export downloadable")
    directory = pathlib.Path(unfinished["object_directory"]).resolve()
    require(directory.is_dir() and directory.stat().st_mode & 0o077 == 0, "actual owned export object directory unavailable")
    objects = list(directory.glob(pending["id"] + "-*.partial"))
    require(bool(objects), "actual interrupted export object missing; no artificial partial substituted")
    evidence.record({"kind": "export_interrupted_objects", "job_id": pending["id"], "objects": [{"name": path.name, "bytes": path.stat().st_size} for path in objects]})
    await asyncio.to_thread(action, configuration, evidence, unfinished["cancel_and_cleanup_action"], allowed)
    status, _, cancelled_raw = await one_request(configuration, evidence, unfinished["status_url"], "export_cancelled_manifest", headers)
    require(status == 200 and strict_json(cancelled_raw)["state"] == "cancelled", "actual cancellation not durable")
    require(not list(directory.glob(pending["id"] + "-*")), "unfinished export objects survived cancellation")
    return {"manifest": manifest, "download": download, "cross_wallet_status": "denied", "cancelled_job_id": pending["id"], "unfinished_objects_removed": len(objects)}


async def authorization_measurement(configuration, evidence):
    specification = configuration["authorization"]
    owner_headers = headers_from_environment(specification["owner_headers_env"])
    status, _, owner_raw = await one_request(configuration, evidence, specification["private_portfolio_url"], "authorized_private_portfolio", owner_headers)
    require(status == 200, "actual owner private portfolio unavailable")
    owner = strict_json(owner_raw)
    data = owner["data"]
    summary = data["summary"]
    require(summary["wallet"] == configuration["wallet"] and UUID.fullmatch(summary["snapshotId"]), "owner session wallet differs from requested private scope")
    status, _, anonymous_raw = await one_request(configuration, evidence, specification["private_portfolio_url"], "unauthenticated_personalized_cache_denial")
    require(status in {401, 403} and summary["snapshotId"].encode() not in anonymous_raw, "unauthenticated request reached personalized cached data")
    status, _, other_raw = await one_request(configuration, evidence, specification["other_wallet_owner_url"], "cross_wallet_personalized_denial", owner_headers)
    require(status in {403, 404} and summary["snapshotId"].encode() not in other_raw, "owner proof crossed wallet-private cache boundary")
    return {"wallet": summary["wallet"], "snapshot_id": summary["snapshotId"], "unauthenticated_denied": True, "cross_wallet_denied": True}


async def display_measurement(configuration, evidence):
    specification = configuration["display"]
    status, _, raw = await one_request(configuration, evidence, specification["quoted_url"], "usd_display_available")
    require(status == 200, "actual display-enabled portfolio unavailable")
    document = strict_json(raw)
    display = document["display"]
    quote = display["quote"]
    require(document["quoteAsset"] == "USDL" and raw_integer(display["usdlRaw"]) == 100000000, "display fixture is not actual 100 USDL")
    numerator = raw_integer(quote["numerator"])
    denominator = raw_integer(quote["denominator"])
    require(denominator > 0 and numerator * 100 == denominator * 98 and not quote["stale"], "actual independent USDL/USD quote is not 0.98")
    require(quote["provider"] == specification["provider_id"] and quote["observedAt"], "display provenance mismatch")
    observed = datetime.datetime.fromisoformat(quote["observedAt"].replace("Z", "+00:00"))
    age = (datetime.datetime.now(datetime.timezone.utc) - observed).total_seconds()
    require(0 <= age <= specification["maximum_age_seconds"] <= 3600, "display provenance stale or future")
    require(raw_integer(display["usdMicro"]) == 98000000, "display conversion fabricated parity")
    status, _, absent_raw = await one_request(configuration, evidence, specification["absent_quote_url"], "usd_display_unavailable")
    require(status == 200, "independent quote outage blocked actual USDL read")
    absent = strict_json(absent_raw)
    require(absent["quoteAsset"] == "USDL" and (absent.get("display") is None or absent["display"].get("usdMicro") is None), "absent quote fabricated USD amount")
    return {"usdl_raw": "100000000", "usd_micro": "98000000", "provider": quote["provider"], "observed_at": quote["observedAt"], "absence_preserves_usdl": True}


async def deposit_measurement(configuration, evidence, allowed):
    specification = configuration["deposit"]
    status, _, before_raw = await one_request(configuration, evidence, specification["portfolio_url"], "deposit_before")
    require(status == 200, "deposit baseline unavailable")
    _, before, _ = portfolio_document(before_raw, specification["wallet"])
    action(configuration, evidence, specification["deposit_action"], allowed)
    status, _, after_raw = await one_request(configuration, evidence, specification["portfolio_url"], "deposit_after")
    require(status == 200, "post-deposit portfolio unavailable")
    _, after, _ = portfolio_document(after_raw, specification["wallet"])
    require(raw_integer(after["completeValueUsdlRaw"]) - raw_integer(before["completeValueUsdlRaw"]) == 100000000, "actual deposit did not increase equity by 100 USDL")
    require(raw_integer(after["knownRealizedSubtotalUsdlRaw"], True) == raw_integer(before["knownRealizedSubtotalUsdlRaw"], True), "deposit treated as trading profit")
    require(raw_integer(after["watermark"]["height"]) > raw_integer(before["watermark"]["height"]), "deposit fixture did not produce a new canonical block")
    require(UUID.fullmatch(after["snapshotId"]), "deposit snapshot identity invalid")
    schema = configuration["database_schema"]
    require(re.fullmatch(r"[a-z][a-z0-9_]{0,62}", schema), "invalid deposit database schema")
    variable = configuration["database_dsn_env"]
    require(variable in os.environ, "actual deposit database unavailable")
    environment = dict(os.environ, PGDATABASE=os.environ[variable], PGOPTIONS="-c default_transaction_read_only=on -c statement_timeout=400 -c search_path=" + schema)
    sql = "SELECT json_build_object('trades',count(*) FILTER(WHERE payload->>'kind'='trade'),'deposits_usdl_raw',COALESCE(sum((payload->>'amountRaw')::numeric) FILTER(WHERE payload->>'kind'='deposit' AND lower(payload->'asset'->>'address')='0x85fcd13735f4309833a503ee804ea32395851479'),0)::text) FROM portfolio_history_rows WHERE snapshot_id='" + after["snapshotId"] + "' AND (payload->'position'->>'height')::numeric>" + before["watermark"]["height"]
    process = await asyncio.to_thread(subprocess.run, ["psql", "-X", "-q", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-c", sql], env=environment, capture_output=True, timeout=5, check=False)
    evidence.record({"kind": "deposit_actual_cashflows", "exit_code": process.returncode, "stdout": evidence.capture(process.stdout, "json"), "stderr_sha256": hashlib.sha256(process.stderr).hexdigest()})
    require(process.returncode == 0, "actual cash-flow query unavailable")
    flows = strict_json(process.stdout)
    require(flows["trades"] == 0 and raw_integer(flows["deposits_usdl_raw"]) == 100000000, "deposit scenario includes trades or incomplete actual cash flows")
    status, _, history_raw = await one_request(configuration, evidence, specification["history_url"], "deposit_return_method")
    require(status == 200, "actual return history unavailable")
    historical = strict_json(history_raw)["historicalReturn"]
    if historical.get("return") is not None:
        require(raw_integer(historical["return"]["denominator"]) > 0, "zero return denominator")
    else:
        require(historical["method"] == "unavailable", "unavailable return inputs advertised approved percentage")
    return {"opening_equity_raw": before["completeValueUsdlRaw"], "closing_equity_raw": after["completeValueUsdlRaw"], "realized_before_raw": before["knownRealizedSubtotalUsdlRaw"], "realized_after_raw": after["knownRealizedSubtotalUsdlRaw"], "historical_return": historical}


async def card_measurement(configuration, evidence, allowed):
    specification = configuration["card"]
    owner_headers = headers_from_environment(specification["owner_headers_env"])
    require(allowed and configuration["realm"] == "fixture", "card adversarial creation requires explicit fixture authorization")
    require(UUID.fullmatch(specification["source_snapshot_id"]), "actual card source snapshot missing")
    connection = HTTPConnection(specification["create_url"], owner_headers, configuration["realm"])
    try:
        body = json.dumps({"snapshotId": specification["source_snapshot_id"], "profitRaw": "25000000"}).encode()
        invalid_status, _, invalid_raw = await asyncio.wait_for(connection.request(method="POST", body=body), 5)
        evidence.record({"kind": "sharecard_client_amount_rejected", "status": invalid_status, "body": evidence.capture(invalid_raw, "json")})
        require(invalid_status == 400, "actual public share creation trusted client-calculated verified profit")
    finally:
        await connection.close()
    status, _, raw = await one_request(configuration, evidence, specification["public_snapshot_url"], "sharecard_snapshot")
    require(status == 200, "actual public immutable share snapshot unavailable")
    share = strict_json(raw)
    require(share["decimals"] == 6 and share["quoteIdentity"]["currencyCode"] == "USDL" and share["methodology"] == ALGORITHM, "sharecard monetary provenance mismatch")
    require(raw_integer(share["values"]["knownRealizedSubtotalUsdlRaw"], True) == 25000000 and share["basisQuality"] in {"partial", "unknown"}, "actual share fixture lacks realized 25 USDL with incomplete basis")
    labels = " ".join(share["labels"])
    require("USDL" in labels and "unavailable" in labels.lower() and "USD total" not in labels, "share labels claim complete USD profit")
    require(share.get("wallet") is None, "anonymous share disclosed wallet alias")
    status, _, image = await one_request(configuration, evidence, specification["public_image_url"], "sharecard_image")
    require(status == 200 and image[:8] == b"\x89PNG\r\n\x1a\n" and image[12:16] == b"IHDR" and int.from_bytes(image[16:20], "big") == 1200 and int.from_bytes(image[20:24], "big") == 630, "actual sharecard image invalid")
    status, _, _ = await one_request(configuration, evidence, specification["draft_public_url"], "sharecard_draft_privacy")
    require(status == 404, "guessed opaque draft disclosed")
    action(configuration, evidence, specification["revoke_action"], allowed)
    deadline = time.monotonic() + 300
    attempts = 0
    while True:
        status, _, owner_raw = await one_request(configuration, evidence, specification["owner_url"], "sharecard_revoke_owner", owner_headers)
        require(status == 200, "owner deletion acknowledgement unavailable")
        owner = strict_json(owner_raw)
        require(owner["state"] == "revoked", "owner revocation not durable")
        status, _, _ = await one_request(configuration, evidence, specification["cdn_image_url"], "sharecard_cdn_tombstone")
        attempts += 1
        if status == 410 and not owner["purgePending"]:
            break
        require(time.monotonic() < deadline, "privacy purge exceeded 300-second bound")
        await asyncio.sleep(0.5)
    return {"id": share["id"], "labels": share["labels"], "basis_quality": share["basisQuality"], "image_sha256": hashlib.sha256(image).hexdigest(), "purge_observations": attempts, "owner_state": owner["state"], "cdn_status": status}


def delivery_state(configuration, evidence, event_id):
    specification = configuration["notification"]
    schema = specification["delivery_schema"]
    require(re.fullmatch(r"[a-z][a-z0-9_]{0,62}", schema) and re.fullmatch(r"[a-zA-Z0-9_-]{1,128}", event_id), "invalid durable delivery identity")
    variable = configuration["database_dsn_env"]
    require(variable in os.environ, "actual delivery database credential missing")
    environment = dict(os.environ, PGDATABASE=os.environ[variable], PGOPTIONS="-c default_transaction_read_only=on -c statement_timeout=400 -c lock_timeout=400 -c TimeZone=UTC")
    sql = "SELECT json_build_object('event_id',event_id,'state',state,'broker_stream',broker_stream,'broker_sequence',broker_sequence,'created_at',created_at,'published_at',published_at,'envelope_base64',encode(envelope,'base64')) FROM " + schema + ".delivery_outbox WHERE event_id='" + event_id + "'"
    process = subprocess.run(["psql", "-X", "-q", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-c", sql], env=environment, capture_output=True, timeout=5, check=False)
    evidence.record({"kind": "actual_delivery_state", "exit_code": process.returncode, "stdout": evidence.capture(process.stdout, "json"), "stderr_sha256": hashlib.sha256(process.stderr).hexdigest()})
    require(process.returncode == 0 and process.stdout.strip(), "actual durable outbox row unavailable")
    return strict_json(process.stdout)


async def notification_measurement(configuration, evidence, allowed):
    from nats.aio.client import Client as NATS
    specification = configuration["notification"]
    await asyncio.to_thread(action, configuration, evidence, specification["commit_before_publish_action"], allowed)
    pending = await asyncio.to_thread(delivery_state, configuration, evidence, specification["event_id"])
    require(pending["state"] == "pending" and pending["broker_sequence"] is None and pending["published_at"] is None, "fixture did not interrupt after durable commit before publish")
    pending_wire = base64.b64decode(pending["envelope_base64"])
    pending_envelope = strict_json(pending_wire)
    notice = pending_envelope["payload"]
    require(notice["wallet"] == configuration["wallet"] and notice["algorithm"] == ALGORITHM, "pending notice wallet or algorithm mismatch")
    browser = await browser_measurement(configuration, evidence, specification["browser_delivery"], specification["restart_and_duplicate_action"], allowed, notice["revision"])
    published = await asyncio.to_thread(delivery_state, configuration, evidence, specification["event_id"])
    require(published["state"] == "published" and published["broker_stream"] == specification["stream"] and published["broker_sequence"] > 0, "actual persistence acknowledgement missing")
    require(base64.b64decode(published["envelope_base64"]) == pending_wire, "restart changed committed notification bytes")
    variable = specification["nats_url_env"]
    require(variable in os.environ and os.environ[variable], "actual JetStream endpoint missing")
    url = urllib.parse.urlsplit(os.environ[variable])
    loopback = url.hostname == "localhost"
    try:
        loopback = loopback or ipaddress.ip_address(url.hostname).is_loopback
    except ValueError:
        pass
    require(url.scheme == "tls" or (configuration["realm"] == "fixture" and url.scheme == "nats" and loopback), "JetStream transport authority missing")
    client = NATS()
    await client.connect(servers=[os.environ[variable]], connect_timeout=5, max_reconnect_attempts=0)
    try:
        message = await asyncio.wait_for(client.jetstream().get_msg(published["broker_stream"], published["broker_sequence"]), 5)
        require(message.data == pending_wire, "actual broker persisted payload differs from committed outbox")
        evidence.record({"kind": "jetstream_persisted_notice", "stream": published["broker_stream"], "sequence": message.seq, "subject": message.subject, "time": str(message.time), "body": evidence.capture(message.data, "json")})
        require(browser["matching_notification_frames"] >= 2 and browser["logical_refetches"] == 1, "real duplicate notice did not produce exactly one logical refetch")
        attestation = pathlib.Path(specification["clock_attestation_path"])
        require(attestation.is_file() and hashlib.sha256(attestation.read_bytes()).hexdigest() == specification["clock_attestation_sha256"], "actual clock uncertainty authority missing")
        uncertainty = specification["maximum_clock_skew_ms"]
        require(type(uncertainty) is int and 0 <= uncertainty <= 1000, "invalid approved clock uncertainty")
        committed = datetime.datetime.fromisoformat(pending["created_at"].replace("Z", "+00:00"))
        lag = [(datetime.datetime.fromisoformat(row["observed_at"]) - committed).total_seconds()*1000 + uncertainty for row in browser["received_notifications"]]
        require(all(value >= 0 for value in lag) and percentile(lag, 99) <= 1000, "actual portfolio notice p99 lag exceeds 1000ms")
        return {"event_id": pending_envelope["event_id"], "wallet": notice["wallet"], "revision": notice["revision"], "stream": message.subject, "sequence": message.seq, "notice_lag_p99_ms": percentile(lag, 99), "clock_uncertainty_ms": uncertainty, "browser": browser}
    finally:
        await client.close()


async def browser_measurement(configuration, evidence, specification=None, notification_action=None, allowed=False, expected_revision=None):
    from playwright.async_api import async_playwright
    specification = specification or configuration["accessibility"]
    checked_url(specification["url"], configuration["realm"])
    executable_variable = specification["browser_executable_env"]
    require(executable_variable in os.environ, "actual browser executable missing")
    executable = pathlib.Path(os.environ[executable_variable]).resolve()
    require(executable.is_file() and hashlib.sha256(executable.read_bytes()).hexdigest() == specification["browser_sha256"], "browser executable differs from admitted environment")
    axe = pathlib.Path(specification["axe_path"]).resolve()
    require(axe.is_file() and hashlib.sha256(axe.read_bytes()).hexdigest() == specification["axe_sha256"], "admitted actual axe engine missing")
    state_variable = specification["storage_state_env"]
    require(state_variable in os.environ and pathlib.Path(os.environ[state_variable]).is_file(), "actual owner browser session missing")
    viewports = specification["viewports"]
    require(isinstance(viewports, list) and (len(viewports) == 1 if notification_action else len(viewports) >= 3), "explicit accessibility or notification browser viewport inputs required")
    frames = []
    refetches = []
    violations = []
    async with async_playwright() as playwright:
        browser = await playwright.chromium.launch(executable_path=str(executable), headless=True)
        try:
            for viewport in viewports:
                require(type(viewport["width"]) is int and type(viewport["height"]) is int and 240 <= viewport["width"] <= 4096 and 240 <= viewport["height"] <= 4096, "invalid accessibility viewport")
                context = await browser.new_context(storage_state=os.environ[state_variable], viewport=viewport)
                await context.tracing.start(screenshots=True, snapshots=True, sources=False)
                page = await context.new_page()
                page.on("websocket", lambda socket: socket.on("framereceived", lambda payload: frames.append({"payload": payload, "observed_at": utc_now()})))
                refetch_url = checked_url(specification["portfolio_refetch_url"], configuration["realm"])
                page.on("requestfinished", lambda request: refetches.append(request.url) if urllib.parse.urlsplit(request.url).path == refetch_url.path else None)
                response = await page.goto(specification["url"], wait_until="networkidle", timeout=30000)
                require(response is not None and response.status == 200, "actual portfolio UI unavailable")
                for control in specification["controls"]:
                    locator = page.get_by_role(control["role"], name=control["name"], exact=True)
                    require(await locator.count() == 1 and await locator.is_visible(), "required accessible portfolio control missing")
                    await locator.focus()
                    require(await locator.evaluate("element => element === document.activeElement"), "portfolio control cannot receive keyboard focus")

                if notification_action is not None:
                    frames.clear()
                    refetches.clear()
                    await asyncio.to_thread(action, configuration, evidence, notification_action, allowed)
                    await page.wait_for_timeout(1000)
                await page.add_script_tag(path=str(axe))
                audit = await page.evaluate("async () => await axe.run(document, {runOnly:{type:'tag',values:['wcag2a','wcag2aa','wcag21aa']}})")
                artifact = evidence.capture(json.dumps(audit, separators=(",", ":")).encode(), "json")
                violations.extend(audit["violations"])
                geometry = await page.evaluate("() => ({width:innerWidth,scroll:document.documentElement.scrollWidth})")
                aria = await page.locator("body").aria_snapshot()
                png = await page.screenshot(full_page=True)
                evidence.record({"kind": "portfolio_accessibility", "viewport": viewport, "axe": artifact, "aria": evidence.capture(aria.encode(), "txt"), "screenshot": evidence.capture(png, "png"), "violations": len(audit["violations"]), "geometry": geometry})
                trace_path = evidence.directory / ("browser-" + str(viewport["width"]) + "x" + str(viewport["height"]) + ".zip")
                await context.tracing.stop(path=str(trace_path))
                evidence.capture(trace_path.read_bytes(), "zip")
                await context.close()
                require(geometry["scroll"] <= geometry["width"], "portfolio UI overflows viewport")
        finally:
            await browser.close()
    require(not violations, "actual portfolio accessibility violations")
    matching = []
    for frame in frames:
        payload = frame["payload"]
        raw = payload.encode() if isinstance(payload, str) else payload
        evidence.record({"kind": "portfolio_websocket_frame", "observed_at": frame["observed_at"], "body": evidence.capture(raw, "json")})
        document = strict_json(raw)
        topic = document.get("topic")
        if topic and topic.startswith("private.portfolio."):
            require(topic == "private.portfolio." + configuration["wallet"], "private notice leaked another wallet")
            if expected_revision is None or document.get("revision") == expected_revision:
                matching.append({"revision": document.get("revision"), "observed_at": frame["observed_at"]})
    return {"viewports": viewports, "violations": len(violations), "matching_notification_frames": len(matching), "logical_refetches": len(refetches), "received_notifications": matching}


async def phase4(configuration, evidence, allowed):
    require(configuration["realm"] in {"fixture", "staging", "production"}, "explicit deployment realm required")
    source = bind_sources(configuration)
    headers = headers_from_environment(configuration["release_headers_env"])
    status, _, raw = await one_request(configuration, evidence, configuration["release_identity_url"], "release_identity", headers)
    identity = strict_json(raw)
    require(status == 200 and identity["releaseDigest"] == configuration["release_digest"] and identity["interfaceDigest"] == configuration["interface_sha256"], "actual runtime differs from admitted release/interface")
    wallet = configuration["wallet"]
    require(ADDRESS.fullmatch(wallet), "canonical measurement wallet required")
    status, _, raw = await one_request(configuration, evidence, configuration["portfolio_url"], "portfolio_baseline")
    require(status == 200, "actual public portfolio unavailable")
    _, summary, items = portfolio_document(raw, wallet)
    result = {"source_sha256": source, "release_digest": configuration["release_digest"], "interface_sha256": configuration["interface_sha256"], "algorithm": ALGORITHM, "snapshot_id": summary["snapshotId"], "baseline_revision": summary["watermark"]["projectionRevision"]}
    checks = [
        ("canonical_chain", lambda: canonical_chain(configuration, evidence, summary, items)),
        ("authorization", lambda: authorization_measurement(configuration, evidence)),
        ("database", lambda: asyncio.to_thread(database_measurement, configuration, summary, evidence)),
        ("pagination", lambda: pagination_measurement(configuration, evidence, wallet, allowed)),
        ("cache", lambda: cache_measurement(configuration, evidence, allowed)),
        ("export", lambda: export_measurement(configuration, evidence, allowed)),
        ("display", lambda: display_measurement(configuration, evidence)),
        ("deposit", lambda: deposit_measurement(configuration, evidence, allowed)),
        ("card", lambda: card_measurement(configuration, evidence, allowed)),
        ("notification", lambda: notification_measurement(configuration, evidence, allowed)),
        ("accessibility", lambda: browser_measurement(configuration, evidence)),
        ("capacity", lambda: run_load(configuration, evidence, wallet)),
    ]
    failures = []
    for name, check in checks:
        try:
            result[name] = await check()
        except Exception as error:
            failure = {"check": name, "error": type(error).__name__ + (": " + str(error) if type(error) in {ValueError, KeyError} else ": provider_operation_failed")}
            failures.append(failure)
            evidence.record({"kind": "failed_measurement", **failure})
    require(bind_sources(configuration) == source, "source changed during release measurement")
    final_status, _, final_raw = await one_request(configuration, evidence, configuration["release_identity_url"], "release_identity_after_measurements", headers)
    final_identity = strict_json(final_raw)
    require(final_status == 200 and final_identity["releaseDigest"] == configuration["release_digest"] and final_identity["interfaceDigest"] == configuration["interface_sha256"], "runtime release changed during measurement")
    result["failed_checks"] = failures
    result["lane_measurements_passed"] = not failures
    result["shared_release_acceptance"] = "deferred_to_serial_9.1"
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--phase4", action="store_true")
    parser.add_argument("--config", type=pathlib.Path)
    parser.add_argument("--output", type=pathlib.Path)
    parser.add_argument("--allow-fixture-actions", action="store_true")
    args = parser.parse_args()
    if not args.phase4:
        require(args.config is None and args.output is None and not args.allow_fixture_actions, "phase4 options require --phase4")
        if not os.environ.get("L17_TEST_DSN"):
            sys.exit("L17_TEST_DSN is required; phase2 qualification uses actual PostgreSQL")
        environment = dict(os.environ, GOWORK="off", GOMAXPROCS="2", GOFLAGS="-mod=readonly")
        os.execvpe("go", ["go", "-C", str(ROOT / "core/pnl-tracker"), "test", "-p", "1", "-count=1", "-json", "./internal/production/l17", "./internal/ogrender"], environment)
    require(args.config is not None and args.output is not None, "phase4 requires actual provider configuration and external output")
    evidence = Evidence(args.output)
    result = {"task": "4.17", "started_at": utc_now(), "qualification_claimed": False, "shared_release_acceptance": "deferred_to_serial_9.1"}
    exit_code = 1
    try:
        configuration_raw = args.config.read_bytes()
        require(len(configuration_raw) <= 1 << 20, "configuration exceeds resource budget")
        result["configuration_sha256"] = hashlib.sha256(configuration_raw).hexdigest()
        configuration = strict_json(configuration_raw)
        result.update(asyncio.run(asyncio.wait_for(phase4(configuration, evidence, args.allow_fixture_actions), timeout=900)))
        exit_code = 0 if result["lane_measurements_passed"] else 1
    except Exception as error:
        result["failure"] = type(error).__name__ + (": " + str(error) if type(error) in {ValueError, KeyError} else ": provider_operation_failed")
        result["lane_measurements_passed"] = False
    result["ended_at"] = utc_now()
    result["exit_code"] = exit_code
    path = evidence.finish(result)
    print(json.dumps({"exit_code": exit_code, "measurement_path": str(path)}))
    return exit_code


if __name__ == "__main__":
    sys.exit(main())
