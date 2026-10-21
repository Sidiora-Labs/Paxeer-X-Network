from __future__ import annotations

import argparse
import asyncio
import dataclasses
import json
import os
from pathlib import Path
import re
import signal
import time
import uuid

from calibration import evaluate, cpu_fraction, linux_cpu_snapshot, nic_fraction
from phase2_accounts import WalletSigner, login, metadata_headers
from phase2_feeder import PostgresObserver, feed_real_transactions, owned_command
from phase2_http import HTTP, HTTPFailure
from phase2_latency import latency_measurements
from phase2_receivers import connect_receiver
from phase2_types import Artifacts, Bindings, ROOT, bound_document, canonical, digest, source_digest, validate_v2
from scheduler import Sample, summarize
from workload import MIX, VERSION, ledger


BROWSER_DRIVER = r'''
const fs=require('node:fs');
let input='';process.stdin.setEncoding('utf8');process.stdin.on('data',x=>input+=x);
process.stdin.on('end',async()=>{let browser;
 try {
  const c=JSON.parse(input);const {chromium}=require(c.playwright_module);
  browser=await chromium.launch({headless:true});
  const page=await browser.newPage();
  await page.exposeFunction('__loadDelivery',async row=>{process.stdout.write(JSON.stringify(row)+'\n');});
  await page.goto(c.applicationOrigin,{waitUntil:'domcontentloaded',timeout:30000});
  const code=fs.readFileSync(c.compiled_module,'utf8').replace(/^export /gm,'');
  await page.addScriptTag({content:code+'\nwindow.__loadObserve=observeBrowserDelivery;'});
  await page.evaluate(async cfg=>{const controller=new AbortController();await window.__loadObserve({...cfg,signal:controller.signal},window.__loadDelivery);},c);
 }catch {process.stderr.write('Actual browser receiver failed\n');process.exitCode=1;}
 finally{if(browser)await browser.close();}
});
'''


def route_category(index):
    position = index % 10000
    for category, count in MIX.items():
        if position < count:
            return category
        position -= count
    raise AssertionError("frozen load mix incomplete")


def subscription_topics(pools, index):
    if len(set(pools)) < 5:
        raise ValueError("five actually admitted pools required for mean5 subscriptions")
    return sorted("market.trades." + pools[(index + offset) % len(pools)] for offset in range(5))


OPERATIONS = dict(chart="IF_CANDLES", portfolio="IF_PORTFOLIO", search="IF_SEARCH", quotes="IF_QUOTE", writes="IF_WATCHLIST_PUT", market="IF_POOL_DETAIL", rankings="IF_RANKINGS", identity_profile="IF_PROFILE_GET", metadata_media="IF_METADATA_GET", notifications_social="IF_NOTIFICATIONS", capabilities="IF_DEPLOYMENT")


def resolve_routes(routes):
    if set(routes) != set(MIX):
        raise ValueError("exact shared traffic categories required")
    document = json.loads((ROOT / "launchpad/spec/production-system/integration/contracts/v2/openapi.json").read_text())
    resolved = {}
    for category, identifier in OPERATIONS.items():
        matches = [(path, method, operation) for path, path_item in document["paths"].items() for method, operation in path_item.items() if isinstance(operation, dict) and operation.get("operationId") == identifier]
        if len(matches) != 1:
            raise ValueError("exact current operation identity required")
        template, method, operation = matches[0]
        route = dict(routes[category])
        if route.get("method") != method.upper() or route.get("template") != template or "expected_statuses" in route:
            raise ValueError("route differs from current frozen operation")
        response = operation["responses"]["200"]["content"]["application/json"]["schema"]["$ref"].rsplit("/", 1)[1]
        body = operation.get("requestBody", {}).get("content", {}).get("application/json", {}).get("schema", {}).get("$ref")
        route.update(response_schema=response, request_schema=body.rsplit("/", 1)[1] if body else None, authenticated=bool(operation.get("security")))
        resolved[category] = route
    return resolved


def aggregate_hosts(reports, bindings):
    if len(reports) != 4 or len({r["host_id"] for r in reports}) != 4:
        raise ValueError("all four independent current host executions required")
    for report in reports:
        if report["bindings"] != dataclasses.asdict(bindings) or report["workload_version"] != VERSION or report["duration_ns"] < 600000000000 or report["failure_count"] != 0 or report["accepted_connections"] != 25000 or report["authenticated_connections"] != 12500 or report["public_connections"] != 12500 or report["accepted_subscriptions"] != 125000 or report["max_subscriptions"] > 20 or report["http"]["scheduled"] != 1500000 or report["http"]["received"] != 1500000 or not report["replay_qualified"] or not report["latency"]["target_passed"]:
            raise ValueError("actual host baseline/replay/latency incomplete")
    overlap = min(r["ended_utc_ns"] for r in reports) - max(r["started_utc_ns"] for r in reports)
    if overlap < 600000000000:
        raise ValueError("four generator baselines did not overlap for ten minutes")
    return {"phase2_scenario_qualified": True, "capacity_qualified": False, "final_capacity_task": "9.1", "accepted_connections": 100000, "authenticated_connections": 50000, "public_connections": 50000, "mean_subscriptions": 5, "max_subscriptions": max(r["max_subscriptions"] for r in reports), "host_artifact_digests": [r["artifact_manifest_sha256"] for r in reports], "bindings": dataclasses.asdict(bindings)}


async def dependency_probe(config, observer):
    postgres = await observer.readiness()
    redis = config["redis"]
    reader, writer = await asyncio.wait_for(asyncio.open_connection(redis["host"], redis["port"]), 3)
    try:
        if redis.get("password"):
            password = redis["password"].encode()
            writer.write(b"*2\r\n$4\r\nAUTH\r\n$" + str(len(password)).encode() + b"\r\n" + password + b"\r\n")
            await writer.drain()
            if await asyncio.wait_for(reader.readline(), 3) != b"+OK\r\n":
                raise ValueError("real Redis authentication failed")
        writer.write(b"*1\r\n$4\r\nPING\r\n")
        await writer.drain()
        if await asyncio.wait_for(reader.readline(), 3) != b"+PONG\r\n":
            raise ValueError("real Redis dependency unavailable")
    finally:
        writer.close()
        await writer.wait_closed()
    nats_http = HTTP(config["nats_monitor_origin"], config["application_origin"], owned_local=True)
    nats = await nats_http.call("GET", "/jsz?accounts=true&streams=true&config=true")
    streams = [stream for account in nats.get("account_details", []) for stream in account.get("stream_detail", [])]
    actual = [s for s in streams if s.get("name") == config["nats_stream"] and s.get("config", {}).get("storage") == "file"]
    if not actual:
        raise ValueError("actual file-backed JetStream stream required")
    return {"postgres": postgres, "redis": "actual_authenticated_ping", "nats_stream": config["nats_stream"], "nats_storage": "file"}


async def mixed_http(config, http, accounts, signer, output, start_ns):
    max_inflight = config.get("http_max_inflight", 128)
    if type(max_inflight) is not int or not 1 <= max_inflight <= 4096:
        raise ValueError("existing bounded scheduler admission required")
    routes = resolve_routes(config["routes"])
    counts = {key: 0 for key in ("scheduled", "attempted", "received", "timeout", "missed", "error")}
    categories = {key: 0 for key in MIX}
    latency_bins, scheduler_bins = {}, {}
    pending = set()
    async def execute(sample, category):
        route = routes[category]
        account = accounts[sample.id % len(accounts)]
        values = {"wallet": account.wallet, "poolAddress": config["pools"][sample.id % len(config["pools"])], "tokenAddress": config["tokens"][sample.id % len(config["tokens"])], "category": config["ranking_category"]}
        path = route["template"]
        for name, value in values.items():
            path = path.replace("{" + name + "}", value)
        if "{" in path or "}" in path:
            raise ValueError("exact route substitutions incomplete")
        if route.get("query"):
            from urllib.parse import urlencode
            path += "?" + urlencode(route["query"])
        body = route.get("body")
        sample.attempted_ns = time.monotonic_ns()
        scheduler_bucket = (sample.attempted_ns - sample.scheduled_ns + 999999) // 1000000
        scheduler_bins[scheduler_bucket] = scheduler_bins.get(scheduler_bucket, 0) + 1
        counts["attempted"] += 1
        try:
            headers = {"X-Request-ID": str(uuid.uuid4())}
            if route.get("signature") == "metadata_v2":
                headers.update(await metadata_headers(signer, account, route["method"], path, body, config["metadata_audience"]))
            if route.get("request_schema"):
                validate_v2(route["request_schema"], body)
            value = await asyncio.wait_for(http.call(route["method"], path, body, bearer=account.access_token if route["authenticated"] else None, headers=headers, expected=(200,), schema=route["response_schema"]), 5)
            if category == "quotes":
                validate_v2("QuoteResponse", value)
                if value["quality"] != "complete":
                    raise ValueError("real healthy quote unavailable")
            sample.status = "received"
        except TimeoutError:
            sample.status = "timeout"
        except HTTPFailure as error:
            sample.status, sample.error = "error", error.code
        except Exception as error:
            sample.status, sample.error = "error", type(error).__name__
        finally:
            sample.completed_ns = time.monotonic_ns()
            latency_bucket = (sample.completed_ns - sample.scheduled_ns + 999999) // 1000000
            latency_bins[latency_bucket] = latency_bins.get(latency_bucket, 0) + 1
            counts[sample.status] += 1
            Artifacts.append(output, {**dataclasses.asdict(sample), "category": category})
    for index in range(1500000):
        scheduled = start_ns + index * 400000
        await asyncio.sleep(max(0, (scheduled - time.monotonic_ns()) / 1e9))
        category = route_category(index * 4 + config["host_index"])
        categories[category] += 1
        counts["scheduled"] += 1
        sample = Sample(index, scheduled)
        if len(pending) >= max_inflight:
            sample.completed_ns = time.monotonic_ns()
            counts["missed"] += 1
            Artifacts.append(output, {**dataclasses.asdict(sample), "category": category})
        else:
            task = asyncio.create_task(execute(sample, category))
            pending.add(task)
            task.add_done_callback(pending.discard)
    if pending:
        await asyncio.gather(*tuple(pending))
    def quantile(histogram, fraction):
        import math
        target = math.ceil(sum(histogram.values()) * fraction)
        count = 0
        for bucket, entries in sorted(histogram.items()):
            count += entries
            if count >= target:
                return bucket * 1000000
        return None
    return {**counts, "category_counts": categories, "latency_denominator": counts["scheduled"], "latency_censored": counts["missed"], "latency_ns_upper_bound": {str(p): quantile(latency_bins, p) for p in (.5, .95, .99, .999, 1)}, "scheduler_p99_ns_upper_bound": quantile(scheduler_bins, .99)}


async def browser_measure(config, account, output):
    browser = config["browser"]
    module = Path(browser["compiled_module"])
    if not module.is_absolute() or module.is_symlink() or digest(module.read_bytes()) != browser["compiled_sha256"]:
        raise ValueError("actual source-bound precompiled browser receiver required")
    node = Path(config["node"])
    if digest(node.read_bytes()) != config["node_sha256"]:
        raise ValueError("browser Node binding differs")
    child = await asyncio.create_subprocess_exec(str(node), "-e", BROWSER_DRIVER, stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.DEVNULL, start_new_session=True)
    command = {"applicationOrigin": config["application_origin"], "streamOrigin": config["stream_origin"], "topics": subscription_topics(config["pools"], 0), "receiverId": config["host_id"] + "-browser", "bearer": account.access_token, "durationMs": 600000, "playwright_module": browser["playwright_module"], "compiled_module": str(module)}
    try:
        child.stdin.write(canonical(command))
        await child.stdin.drain()
        child.stdin.close()
        count = 0
        end = time.monotonic() + 660
        while True:
            row = await asyncio.wait_for(child.stdout.readline(), max(.001, end - time.monotonic()))
            if not row:
                break
            count += len(row)
            if len(row) > 8192 or count > 67108864:
                raise ValueError("browser raw sample budget exceeded")
            value = json.loads(row)
            Artifacts.append(output, value)
        if await child.wait() != 0:
            raise RuntimeError("actual browser delivery failed")
    finally:
        if child.returncode is None:
            os.killpg(child.pid, signal.SIGTERM)
            try:
                await asyncio.wait_for(child.wait(), 2)
            except TimeoutError:
                os.killpg(child.pid, signal.SIGKILL)
                await child.wait()


async def run(config, artifacts):
    bindings = Bindings(**config["bindings"])
    if bindings.generator != source_digest() or config["host_index"] not in range(4) or config["host_id"] != config["generator_hosts"][config["host_index"]] or len(set(config["generator_hosts"])) != 4:
        raise ValueError("source-bound current external generator host required")
    reports = [bound_document(r["path"], r["sha256"], maximum_bytes=67108864) for r in config["calibration_reports"]]
    calibration = evaluate(reports, candidate=bindings.candidate, topology=bindings.topology, generator=bindings.generator)
    if not calibration["generator_calibrated"]:
        raise ValueError("four-host measured calibration failed")
    if {r["host_id"] for r in reports} != set(config["generator_hosts"]):
        raise ValueError("calibration host inventory differs")
    http = HTTP(config["api_origin"], config["application_origin"], ca_file=config.get("ca_file"), owned_local=config.get("owned_local", False))
    observer = PostgresObserver(**config["postgres"], pools=config["pools"])
    artifacts.json("candidate-bindings.json", dataclasses.asdict(bindings))
    artifacts.json("workload-ledger.json", ledger())
    artifacts.json("dependencies.json", await dependency_probe(config, observer))
    artifacts.json("generator-calibration.json", calibration)
    http_output = artifacts.rows("http-samples.ndjson")
    receiver_output = artifacts.rows("receiver-events.ndjson")
    browser_output = artifacts.rows("browser-delivery.ndjson")
    ingest_output = artifacts.rows("event-ingest.ndjson")
    chain_output = artifacts.rows("chain-receipts.ndjson")
    signer = WalletSigner(config["node"], config["node_sha256"], config["ethers_module"])
    sockets, readers, accounts, states = [], [], [], []
    failures = []
    stop = asyncio.Event()
    async def read(index, socket, state):
        try:
            while not stop.is_set():
                value = await socket.receive()
                accepted = state.receive(value)
                if accepted and index < 128:
                    Artifacts.append(receiver_output, {"receiver_id": index, "event": value, "received_utc_ns": time.time_ns()})
        except asyncio.CancelledError:
            raise
        except Exception as error:
            failures.append({"receiver_id": index, "error": type(error).__name__})
    try:
        for _ in range(12500):
            accounts.append(await login(http, signer))
        artifacts.json("accounts-public.json", [a.public() for a in accounts])
        semaphore = asyncio.Semaphore(64)
        async def establish(index):
            async with semaphore:
                topics = subscription_topics(config["pools"], index)
                socket, state = await connect_receiver(http, config["stream_origin"], topics, accounts[index] if index < 12500 else None, ca_file=config.get("ca_file"), owned_local=config.get("owned_local", False), retain_fills=index < 128)
                sockets.append((index, socket))
                states.append((index, topics, state, dict(state.sequence)))
                readers.append((index, asyncio.create_task(read(index, socket, state))))
        for offset in range(0, 25000, 64):
            await asyncio.gather(*(establish(i) for i in range(offset, min(offset + 64, 25000))))
        if failures:
            raise ValueError("socket cohort failed during establishment")
        artifacts.json("subscription-inventory.json", {"accepted_connections": len(states), "authenticated": 12500, "public": 12500, "actual_topics": [{"receiver_id": i, "topics": topics, "initial_sequence": initial} for i, topics, state, initial in sorted(states)]})
        start_utc = config["baseline_start_utc_ns"]
        if type(start_utc) is not int or not 1 <= start_utc - time.time_ns() <= 300000000000:
            raise ValueError("fresh coordinated common baseline start required")
        await asyncio.sleep((start_utc - time.time_ns()) / 1e9)
        started_utc, started_mono = time.time_ns(), time.monotonic_ns()
        cpu_before = linux_cpu_snapshot()
        nic_name = config["nic_name"]
        if not re.fullmatch(r"[A-Za-z0-9_.:-]{1,64}", nic_name):
            raise ValueError("explicit actual external generator NIC required")
        nic_root = Path("/sys/class/net") / nic_name
        speed_bps = int((nic_root / "speed").read_text()) * 1000000
        tx_before = int((nic_root / "statistics/tx_bytes").read_text())
        rx_before = int((nic_root / "statistics/rx_bytes").read_text())
        browser_task = asyncio.create_task(browser_measure(config, accounts[0], browser_output))
        rpc = HTTP(config["rpc_origin"], config["application_origin"], owned_local=True)
        feed_task = asyncio.create_task(feed_real_transactions(rpc, config["signed_transactions"], chain_output, admitted_mutation=config["owned_chain_mutation_admitted"]))
        mixed_task = asyncio.create_task(mixed_http(config, http, accounts, signer, http_output, started_mono))
        await asyncio.sleep(120)
        disconnected_utc = time.time_ns()
        reader_by_index, socket_by_index = dict(readers), dict(sockets)
        replay = []
        for index, topics, state, initial in sorted(states)[:128]:
            reader_by_index[index].cancel()
            await asyncio.gather(reader_by_index[index], return_exceptions=True)
            await socket_by_index[index].close()
            replay.append((index, topics, state, initial))
        await asyncio.sleep(10)
        for index, topics, state, initial in replay:
            socket, reused = await connect_receiver(http, config["stream_origin"], topics, accounts[index], ca_file=config.get("ca_file"), owned_local=config.get("owned_local", False), state=state, retain_fills=True)
            sockets.append((index, socket))
            readers.append((index, asyncio.create_task(read(index, socket, reused))))
        reconnected_utc = time.time_ns()
        http_summary = await mixed_task
        await feed_task
        await browser_task
        await asyncio.sleep(max(0, 600 - (time.monotonic_ns() - started_mono) / 1e9))
        ended_utc = time.time_ns()
        baseline_duration = time.monotonic_ns() - started_mono
        cpu_after = linux_cpu_snapshot()
        tx_after = int((nic_root / "statistics/tx_bytes").read_text())
        rx_after = int((nic_root / "statistics/rx_bytes").read_text())
        measured_cpu = cpu_fraction(cpu_before, cpu_after)
        measured_nic = nic_fraction(tx_before=tx_before, rx_before=rx_before, tx_after=tx_after, rx_after=rx_after, duration_ns=baseline_duration, speed_bps=speed_bps)
        if measured_cpu > .7 or measured_nic > .7 or http_summary["scheduler_p99_ns_upper_bound"] is None or http_summary["scheduler_p99_ns_upper_bound"] > 10000000:
            raise ValueError("actual baseline generator saturated")
        for index, topics, state, initial in replay:
            rows = await observer.stream_fills(topics, initial)
            expected = {row["event_id"] for row in rows}
            if not expected or set(state.fills) != expected or state.checkpoint_count < 1:
                raise ValueError("distinct durable fills not fully replayed")
        artifacts.json("replay-reconciliation.json", {"receivers": len(replay), "fill_counts": {str(index): len(state.fills) for index, _, state, _ in replay}, "qualified": not failures})
        after, ingest_rows = None, []
        while True:
            rows = await observer.committed(after=after)
            for row in rows:
                Artifacts.append(ingest_output, row)
            ingest_rows.extend(rows)
            if len(ingest_rows) > 1000000:
                raise ValueError("durable correlation inventory budget exceeded")
            if len(rows) < 5000:
                break
            after = rows[-1]["event_id"]
        browser_output.flush()
        browser_rows = [json.loads(line) for line in (artifacts.directory / "browser-delivery.ndjson").read_text().splitlines()]
        latency = latency_measurements(ingest_rows, browser_rows, **config["browser_clock"])
        replay_window_fills = {row["event_id"] for row in ingest_rows if disconnected_utc <= int(row["durable_commit_utc_ns"]) <= reconnected_utc}
        if not replay_window_fills or not replay_window_fills.intersection(set().union(*(set(state.fills) for _, _, state, _ in replay))):
            raise ValueError("actual fills committed while receivers disconnected must replay")
        return {"host_id": config["host_id"], "bindings": dataclasses.asdict(bindings), "workload_version": VERSION, "started_utc_ns": started_utc, "ended_utc_ns": ended_utc, "duration_ns": baseline_duration, "accepted_connections": 25000, "authenticated_connections": 12500, "public_connections": 12500, "accepted_subscriptions": 125000, "max_subscriptions": 5, "http": http_summary, "failure_count": len(failures), "receiver_failures": failures[:100], "replay_qualified": not failures, "latency": latency, "generator_resources": {"cpu_before": cpu_before, "cpu_after": cpu_after, "cpu_fraction": measured_cpu, "nic_fraction": measured_nic, "nic_name": nic_name, "speed_bps": speed_bps, "tx_before": tx_before, "tx_after": tx_after, "rx_before": rx_before, "rx_after": rx_after}, "phase2_scenario_qualified": False, "capacity_qualified": False, "qualification_boundary": "single actual generator; four-host coordinator aggregation required"}
    finally:
        stop.set()
        for _, task in readers:
            task.cancel()
        await asyncio.gather(*(task for _, task in readers), return_exceptions=True)
        await asyncio.gather(*(socket.close() for _, socket in sockets), return_exceptions=True)
        signer.close()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", required=True)
    parser.add_argument("--config-sha256", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    artifacts = Artifacts(args.output)
    result = {"task": "2.48", "qualified": False, "capacity_qualified": False}
    exit_code = 1
    try:
        config = bound_document(args.config, args.config_sha256)
        result.update(asyncio.run(asyncio.wait_for(run(config, artifacts), 1800)))
        result["qualified"] = result["failure_count"] == 0 and result["latency"]["target_passed"] and result["http"]["received"] == result["http"]["scheduled"]
        exit_code = 0 if result["qualified"] else 1
    except Exception as error:
        result["error"] = type(error).__name__
    finally:
        artifacts.close()
        artifacts.json("artifact-sha256.json", artifacts.checksums())
        result["artifact_manifest_sha256"] = digest((artifacts.directory / "artifact-sha256.json").read_bytes())
        artifacts.json("result.json", result)
    print(json.dumps({"task": "2.48", "qualified": result["qualified"], "capacity_qualified": False, "result": str(artifacts.directory / "result.json")}))
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
