from __future__ import annotations

import argparse
import asyncio
import dataclasses
import hashlib
import json
import math
import os
from pathlib import Path
import re
import sys
import time
from urllib.parse import quote

ROOT = Path(__file__).resolve().parents[4]
LOAD = ROOT / "tools/production/load"
INTEGRATION = ROOT / "tools/production/integration"
sys.path.insert(0, str(LOAD))
sys.path.insert(0, str(INTEGRATION))

from adapters import RecoveryFaults, RecoveryPermit
from calibration import evaluate, cpu_fraction, linux_cpu_snapshot, nic_fraction
from phase2_accounts import WalletSigner, login
from phase2_feeder import PostgresObserver
from phase2_http import HTTP
from phase2_latency import latency_measurements
from phase2_receivers import connect_receiver
from phase2_runner import BROWSER_DRIVER, subscription_topics
from phase2_types import Bindings, bound_document, canonical, digest, source_digest, unique
import signal
from phase3_runner import SkewReceiverState
from workload import VERSION

PROFILES = {
    "preflight": {"sockets": 256, "seconds": 600, "frames_per_second": None},
    "peak": {"sockets": 100000, "seconds": 86400, "frames_per_second": 250000},
    "surge": {"sockets": 200000, "seconds": 600, "frames_per_second": 1000000},
}
HEX = re.compile(r"[0-9a-f]{64}")


def require(condition, message):
    if not condition:
        raise ValueError(message)


def file_hash(path):
    h = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for block in iter(lambda: stream.read(1048576), b""):
            h.update(block)
    return h.hexdigest()


def generator_hash():
    return digest(canonical({"l23": file_hash(__file__), "shared_load": source_digest(), "fault_adapter": file_hash(INTEGRATION / "adapters.py")}))


def schema_validator(config, name):
    from jsonschema import Draft202012Validator, FormatChecker
    schema = bound_document(config["schema"]["path"], config["schema"]["sha256"], maximum_bytes=1048576)
    require(name in schema["$defs"], "missing exact shared capture definition")
    return Draft202012Validator({**schema, "$ref": "#/$defs/" + name}, format_checker=FormatChecker())


class Capture:
    def __init__(self, directory, maximum_bytes):
        require(type(maximum_bytes) is int and 1048576 <= maximum_bytes <= 2**60, "explicit finite raw capture byte budget required")
        path = Path(directory)
        require(path.is_absolute() and not any(p.is_symlink() for p in (path, *path.parents)), "direct lane-owned artifact path required")
        allowed = ROOT / "evidence/production-system/L23/phase4"
        self.directory = path.resolve()
        require(self.directory.is_relative_to(allowed) and self.directory != allowed, "unique L23 phase4 run directory required")
        self.directory.mkdir(parents=True, exist_ok=False, mode=0o700)
        self.maximum_bytes, self.bytes = maximum_bytes, 0
        self.files, self.kinds = {}, {}

    def append(self, name, value, kind):
        require(re.fullmatch(r"[a-z0-9-]+\.ndjson", name), "bounded raw artifact name required")
        raw = canonical(value) + b"\n"
        require(len(raw) <= 524288 and self.bytes + len(raw) <= self.maximum_bytes, "raw capture byte budget exhausted")
        if name not in self.files:
            require(len(self.kinds) < 480, "capture artifact inventory budget exhausted")
            fd = os.open(self.directory / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            self.files[name] = os.fdopen(fd, "wb")
            self.kinds[name] = kind
        require(self.kinds[name] == kind, "artifact producer kind changed")
        self.files[name].write(raw)
        self.bytes += len(raw)

    def flush(self):
        for output in self.files.values():
            output.flush()

    def close(self):
        for output in self.files.values():
            if not output.closed:
                output.flush()
                os.fsync(output.fileno())
                output.close()

    def json(self, name, value, kind):
        require(re.fullmatch(r"[a-z0-9-]+\.json", name) and name not in self.kinds, "unique JSON artifact required")
        raw = canonical(value) + b"\n"
        require(self.bytes + len(raw) <= self.maximum_bytes, "raw capture byte budget exhausted")
        fd = os.open(self.directory / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "wb") as output:
            output.write(raw)
            output.flush()
            os.fsync(output.fileno())
        self.bytes += len(raw)
        self.kinds[name] = kind

    def inventory(self):
        self.close()
        return [{"name": name, "path": name, "sha256": file_hash(self.directory / name), "kind": kind} for name, kind in sorted(self.kinds.items())]

    def rows(self, name):
        self.flush()
        with (self.directory / name).open("rb") as source:
            for line in source:
                require(len(line) <= 524288, "raw artifact row bound")
                yield json.loads(line, object_pairs_hook=unique)


class BrowserOutput:
    def __init__(self, capture):
        self.capture = capture

    def write(self, line):
        require(line.endswith("\n") and len(line) <= 8192, "bounded actual browser callback required")
        value = json.loads(line, object_pairs_hook=unique)
        if value.get("kind") == "browser_scope":
            self.capture.append("browser-scopes.ndjson", value, "socket_inventory")
        elif value.get("kind") == "browser_done":
            self.capture.append("browser-scopes.ndjson", value, "socket_inventory")
        else:
            self.capture.append("browser-delivery.ndjson", value, "browser_delivery")



async def browser_capture(config, account, capture):
    browser = config["browser"]
    module, node = Path(browser["compiled_module"]), Path(config["node"])
    require(module.is_absolute() and not module.is_symlink() and file_hash(module) == browser["compiled_sha256"], "actual source-bound compiled browser receiver required")
    require(node.is_absolute() and not node.is_symlink() and file_hash(node) == config["node_sha256"], "actual source-bound Node required")
    needle = "await page.exposeFunction('__loadDelivery',async row=>{process.stdout.write(JSON.stringify(row)+'\\n');});"
    require(BROWSER_DRIVER.count(needle) == 1, "browser instrumentation source boundary changed")
    driver = BROWSER_DRIVER.replace(needle, needle + """
  await page.exposeFunction('__loadScope',async row=>{process.stdout.write(JSON.stringify(row)+'\\n');});
  await page.addInitScript(()=>{
    const nativeFetch=window.fetch;
    window.fetch=async (...args)=>{
      const response=await nativeFetch(...args);
      const address=new URL(typeof args[0]==='string'?args[0]:args[0].url||args[0].href,location.origin);
      if(address.pathname==='/v2/snapshots'&&response.ok&&window.__l23ReceiverId&&!window.__l23ScopeCaptured&&address.searchParams.get('topics')===window.__l23Topics){
        window.__l23ScopeCaptured=true;
        const value=await response.clone().json();
        await window.__loadScope({kind:'browser_scope',receiver_id:window.__l23ReceiverId,observed_utc_ns:BigInt(Math.round((performance.timeOrigin+performance.now())*1000000)).toString(),snapshot:value.data.snapshot});
      }
      return response;
    };
  });
""")
    original = "await page.evaluate(async cfg=>{const controller=new AbortController();await window.__loadObserve({...cfg,signal:controller.signal},window.__loadDelivery);},c);"
    require(driver.count(original) == 1, "browser completion source boundary changed")
    driver = driver.replace(original, "const summary=await page.evaluate(async cfg=>{window.__l23ReceiverId=cfg.receiverId;window.__l23Topics=[...cfg.topics].sort().join(',');window.__l23ScopeCaptured=false;const controller=new AbortController();return await window.__loadObserve({...cfg,signal:controller.signal},window.__loadDelivery);},c);process.stdout.write(JSON.stringify({kind:'browser_done',receiver_id:c.receiverId,observed_utc_ns:BigInt(Date.now())*1000000n+'' ,frames:summary.frames,checkpoints:summary.checkpoints})+'\\n');")
    child = await asyncio.create_subprocess_exec(str(node), "-e", driver, stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.DEVNULL, start_new_session=True)
    command = {"applicationOrigin": config["application_origin"], "streamOrigin": config["stream_origin"], "topics": subscription_topics(config["pools"], 0), "receiverId": config["host_id"]+"-browser", "bearer": account.access_token, "durationMs": 601000, "playwright_module": browser["playwright_module"], "compiled_module": str(module)}
    output = BrowserOutput(capture)
    try:
        child.stdin.write(canonical(command))
        await child.stdin.drain()
        child.stdin.close()
        deadline = time.monotonic()+660
        while True:
            row = await asyncio.wait_for(child.stdout.readline(), max(.001, deadline-time.monotonic()))
            if not row:
                break
            output.write(row.decode())
        require(await child.wait() == 0, "actual browser receiver failed")
    finally:
        if child.returncode is None:
            os.killpg(child.pid, signal.SIGTERM)
            try:
                await asyncio.wait_for(child.wait(), 2)
            except TimeoutError:
                os.killpg(child.pid, signal.SIGKILL)
                await child.wait()


def topics_for(pools, global_index):
    require(len(pools) >= 6 and len(set(pools)) == len(pools) and all(re.fullmatch(r"0x[0-9a-f]{40}", pool) for pool in pools), "six distinct admitted pool identities required")
    pool = pools[0] if global_index % 2 == 0 else pools[1 + (global_index // 2) % (len(pools) - 1)]
    return sorted(["market.trades." + pool, "market.price." + pool, "market.stats." + pool, "market.candles." + pool + ".1m", "market.candles." + pool + ".5m"])


def inputs(config):
    bindings = Bindings(**config["bindings"])
    profile = PROFILES[config["profile"]]
    candidate = bound_document(config["candidate_manifest"]["path"], config["candidate_manifest"]["sha256"], maximum_bytes=67108864)
    require(config["candidate_manifest"]["sha256"] == bindings.candidate and isinstance(candidate["files"], dict) and candidate["files"], "exact current candidate source manifest required")
    for name, expected in candidate["files"].items():
        path = ROOT / name
        require(not Path(name).is_absolute() and path.resolve().is_relative_to(ROOT) and not path.is_symlink() and file_hash(path) == expected, "actual candidate file differs from source manifest")
    interface = Path(config["interface_digest"]["path"])
    require(not interface.is_symlink() and file_hash(interface) == bindings.interface == config["interface_digest"]["sha256"], "prior serial gate interface digest required")
    topology = bound_document(config["topology"]["path"], config["topology"]["sha256"])
    require(config["topology"]["sha256"] == bindings.topology and topology["generator_hosts"] == config["generator_hosts"], "actual topology differs from current binding")
    require(bindings.generator == generator_hash(), "current actual harness/provider source digest required")
    authority = bound_document(config["authority"]["path"], config["authority"]["sha256"])
    require(authority["bindings"] == {k: v for k, v in dataclasses.asdict(bindings).items() if k != "generator"}, "native runtime authority differs from candidate/interface/topology")
    require(int(authority["expires_utc_ns"]) > time.time_ns() and authority["network_mutation_admitted"] is True, "fresh explicit owned workload admission required")
    hosts = config["generator_hosts"]
    require(len(hosts) == 4 and len(set(hosts)) == 4 and type(config["host_index"]) is int and 0 <= config["host_index"] < 4 and config["host_id"] == hosts[config["host_index"]], "four distinct current generator host identities required")
    reports = [bound_document(r["path"], r["sha256"], maximum_bytes=67108864) for r in config["calibration_reports"]]
    calibrated = evaluate(reports, candidate=bindings.candidate, topology=bindings.topology, generator=bindings.generator)
    require(calibrated["generator_calibrated"] and {r["host_id"] for r in reports} == set(hosts), "four-host measured calibration required")
    clock = bound_document(config["clock"]["path"], config["clock"]["sha256"])
    require(clock["bindings"] == dataclasses.asdict(bindings) and clock["host_id"] == config["host_id"] and int(clock["expires_utc_ns"]) > time.time_ns(), "fresh native clock authority required")
    require(type(clock["clock_offset_ns"]) is int and type(clock["clock_uncertainty_ns"]) is int and 0 <= clock["clock_uncertainty_ns"] <= 5000000, "measured clock uncertainty above5ms")
    require({g["replica_id"] for g in authority["gateways"]} == set(topology["gateway_replica_ids"]), "native replica inventory differs from topology")
    require(authority["api_origin"] == config["api_origin"] and authority["stream_origin"] == config["stream_origin"] and authority["application_origin"] == config["application_origin"], "endpoint outside exact current native authority")
    require(type(config["start_utc_ns"]) is int and config["start_utc_ns"] > time.time_ns(), "future coordinated load start required")
    require(type(config["burst_start_seconds"]) is int and type(config["burst_duration_seconds"]) is int and 0 <= config["burst_start_seconds"] < profile["seconds"] and 1 <= config["burst_duration_seconds"] <= profile["seconds"] - config["burst_start_seconds"], "bounded explicit burst interval required")
    require(authority["transaction_workload_sha256"][config["host_id"]] == digest(canonical(config["signed_transactions"])), "actual owned signed transaction workload differs from authority")
    require(authority["chain_mutation_admitted"] is True and authority["rpc_origin"] == config["rpc_origin"], "exact owned real transaction feeder authority required")
    for key in ("root_capacity_ledger", "root_mix", "scenario_manifest"):
        bound_document(config[key]["path"], config[key]["sha256"])
    require(config["browser"]["compiled_sha256"] == authority["browser_compiled_sha256"] and file_hash(LOAD / "phase2_browser.ts") == authority["browser_source_sha256"], "compiled browser source/runtime authority mismatch")
    require(authority["fault_schedule"] == config["coordinated_fault_schedule"], "common fault plan differs from admitted topology")
    if config["profile"] != "preflight":
        require(any(f["scenario"] == "az_loss" for f in config["coordinated_fault_schedule"]), "full realtime measurement requires actual AZ-loss plan")
    return bindings, profile, authority, clock, calibrated


async def native_samples(config, authority, capture, started, seconds, stop):
    validator = schema_validator(config, "GatewayInterval")
    gateways = authority["gateways"]
    require(gateways and len({g["replica_id"] for g in gateways}) == len(gateways), "actual exact gateway replica inventory required")
    before = {}
    observed = []
    for tick in range(seconds + 3):
        await asyncio.sleep(max(0, started + tick - time.monotonic()))
        for gateway in gateways:
            http = HTTP(gateway["capture_origin"], config["application_origin"], ca_file=config["ca_file"])
            row = await http.call("GET", gateway["capture_path"])
            validator.validate(row)
            require(row["run_id"] == config["run_id"] and row["replica_id"] == gateway["replica_id"] and row["metrics_authority_sha256"] == gateway["metrics_authority_sha256"], "native edge capture identity/authority mismatch")
            start, end = int(row["interval_start_utc_ns"]), int(row["interval_end_utc_ns"])
            require(start < end and 0 <= time.time_ns() - end <= 3000000000 and end - start <= 1100000000, "actual fresh one-second edge capture required")
            previous = before.get(gateway["replica_id"])
            if previous is not None:
                require(start == int(previous["interval_end_utc_ns"]) and row["capture_id"] != previous["capture_id"], "native gateway interval gap or duplicate")
                require(row["oom_kills"] == previous["oom_kills"] and row["heap_gc_cycle"] >= previous["heap_gc_cycle"], "native OOM or process counter reset")
            require(row["authenticated_connections"] + row["public_connections"] == row["accepted_connections"] and row["subscriptions_max_per_socket"] <= 20 and row["slow_reader_queue_bytes_max"] <= 256*1024, "native scope or send queue cap exceeded")
            require(row["queue_capacity"] > 0 and row["queue_depth"] <= row["queue_capacity"] and 0 < row["memory_bytes"] < row["memory_limit_bytes"], "native resource admission exhausted")
            require(row["payload_bytes"] <= row["websocket_bytes"] <= row["tls_bytes"] <= row["network_bytes"] and row["chart_delivered_frames"] <= row["delivered_frames"], "native transport byte boundaries inconsistent")
            require(row["generator_cpu_fraction"] <= .7 and row["generator_nic_fraction"] <= .7, "native generator calibration headroom exhausted")
            capture.append("gateway-egress.ndjson", row, "gateway_egress")
            before[gateway["replica_id"]] = row
            observed.append(row)
        if stop.is_set():
            break
    return observed


async def actual_feed(config, capture, started, seconds):
    http = HTTP(config["rpc_origin"], config["application_origin"], ca_file=config["ca_file"])
    chain = await http.call("POST", "/", {"jsonrpc": "2.0", "id": 1, "method": "eth_chainId", "params": []})
    require(chain.get("result") == "0x7d", "real chain125 required")
    transactions = config["signed_transactions"]
    require(transactions and len(transactions) <= 1000000 and len(set(transactions)) == len(transactions), "nonempty unique actual pre-signed transaction workload required")
    for index, tx in enumerate(transactions):
        require(isinstance(tx, str) and re.fullmatch(r"0x[0-9a-fA-F]+", tx) and len(tx) <= 262144, "actual bounded signed transaction required")
        burst_start = config["burst_start_seconds"]
        burst_duration = config["burst_duration_seconds"]
        weighted = index*(seconds+3*burst_duration)/len(transactions)
        if weighted < burst_start:
            offset = weighted
        elif weighted < burst_start+4*burst_duration:
            offset = burst_start+(weighted-burst_start)/4
        else:
            offset = weighted-3*burst_duration
        scheduled = started+offset
        await asyncio.sleep(max(0, scheduled - time.monotonic()))
        submitted = time.time_ns()
        capture.append("source-arrivals.ndjson", {"arrival_id": config["host_id"]+":"+str(index), "signed_transaction_sha256": digest(tx.encode()), "scheduled_monotonic_ns": str(int(scheduled*1000000000)), "attempted_utc_ns": str(submitted), "scheduler_delay_ns": max(0, int((time.monotonic()-scheduled)*1000000000))}, "source_arrivals")
        value = await http.call("POST", "/", {"jsonrpc": "2.0", "id": index+2, "method": "eth_sendRawTransaction", "params": [tx]})
        transaction = value.get("result")
        require("error" not in value and isinstance(transaction, str) and re.fullmatch(r"0x[0-9a-fA-F]{64}", transaction), "actual transaction broadcast failed")
        deadline = time.monotonic()+20
        while True:
            receipt = (await http.call("POST", "/", {"jsonrpc": "2.0", "id": index+2, "method": "eth_getTransactionReceipt", "params": [transaction]})).get("result")
            if receipt is not None:
                require(receipt.get("status") == "0x1" and receipt.get("transactionHash", "").lower() == transaction.lower(), "real canonical receipt failed")
                capture.append("chain-receipts.ndjson", {"transaction_hash": transaction.lower(), "submitted_utc_ns": str(submitted), "observed_utc_ns": str(time.time_ns()), "receipt": receipt}, "native_provider")
                break
            require(time.monotonic() < deadline, "actual transaction receipt deadline")
            await asyncio.sleep(.05)


async def apply_faults(config, authority, capture, started, seconds):
    if config["host_index"] != 0:
        require(not config["fault_schedule"], "only one admitted failure-domain controller permitted")
        return []
    if not config["fault_schedule"]:
        require(config["profile"] == "preflight", "full realtime profile requires actual failure-domain schedule")
        return []
    document = config["fault_control"]
    executable = Path(document["docker"])
    require(executable.is_absolute() and not executable.is_symlink() and file_hash(executable) == document["docker_sha256"], "source-bound actual Docker binary required")
    require(document["mutation_admitted"] is True and document["namespace"] == authority["namespace"] and document["attempt"] == authority["attempt"], "exact owned fault mutation admission required")
    permits = [RecoveryPermit(**{**p, "actions": tuple(p["actions"]), "volumes": tuple(p["volumes"])}) for p in document["permits"]]
    require(document["permits"] == authority["fault_permits"], "fault allowlist differs from admitted actual resources")
    controller = RecoveryFaults(permits, document["namespace"], document["attempt"], str(executable))
    restore = []
    rows = []
    try:
        for fault in config["fault_schedule"]:
            require(fault["scenario"] in ("subscriber_stop", "broker_node_loss", "az_loss") and type(fault["offset_seconds"]) is int and type(fault["duration_seconds"]) is int and 0 <= fault["offset_seconds"] and fault["offset_seconds"]+fault["duration_seconds"] < seconds, "exact bounded realtime fault schedule required")
            selected = [controller.permits[target] for target in fault["targets"]]
            require(selected and len({p.failure_domain for p in selected}) == 1, "one admitted exact failure domain required")
            if fault["scenario"] == "az_loss":
                require({p.role for p in selected} >= {"app", "postgres", "nats"}, "AZ loss requires actual app/database/broker victims")
            if fault["scenario"] == "subscriber_stop":
                require(fault["duration_seconds"] == 60 and {p.role for p in selected} == {"app"}, "actual60second gateway subscriber interruption required")
            if fault["scenario"] == "broker_node_loss":
                require(len(selected) == 1 and selected[0].role == "nats", "one exact owned broker node required")
            await asyncio.sleep(max(0, started+fault["offset_seconds"]-time.monotonic()))
            began = time.time_ns()
            controls = []
            for permit in selected:
                action = "pause" if fault["scenario"] == "subscriber_stop" else "stop"
                inverse = "unpause" if action == "pause" else "start"
                restore.append((permit.container_id, inverse))
                controls.append(await asyncio.to_thread(controller.apply, permit.container_id, action))
            await asyncio.sleep(fault["duration_seconds"])
            ended = time.time_ns()
            for permit in selected:
                inverse = "unpause" if fault["scenario"] == "subscriber_stop" else "start"
                controls.append(await asyncio.to_thread(controller.apply, permit.container_id, inverse))
                restore.remove((permit.container_id, inverse))
            row = {"scenario": fault["scenario"], "fault_id": fault["fault_id"], "observed_start_utc_ns": str(began), "observed_end_utc_ns": str(ended), "controls": controls, "transport_restored_utc_ns": str(time.time_ns()), "reference_reconstruction_qualified": False}
            capture.append("native-fault-controls.ndjson", row, "native_provider")
            rows.append(row)
        return rows
    finally:
        failures = []
        for target, action in reversed(restore):
            try:
                row = await asyncio.shield(asyncio.to_thread(controller.apply, target, action))
                capture.append("native-fault-controls.ndjson", {"cleanup": row}, "native_provider")
            except Exception as error:
                failures.append(type(error).__name__)
        require(not failures, "real owned failure targets could not be restored")


async def observe_source(observer, config, capture, stop):
    validator = schema_validator(config, "SourceCommit")
    observed = {}
    while not stop.is_set():
        cursor = None
        while True:
            try:
                rows = await observer.committed(after=cursor)
            except Exception as error:
                capture.append("source-observer-errors.ndjson", {"observed_utc_ns": str(time.time_ns()), "error": type(error).__name__}, "native_provider")
                await asyncio.sleep(1)
                break
            for row in rows:
                validator.validate(row)
                identity = row["event_id"]
                h = digest(canonical(row))
                require(identity not in observed or observed[identity][0] == h, "canonical commit identity changed")
                if identity not in observed:
                    require(len(observed) < config["max_source_events"], "bounded durable source inventory exhausted")
                    observed[identity] = (h, row)
                    capture.append("source-commits.ndjson", row, "source_commits")
            if len(rows) < 5000:
                break
            cursor = rows[-1]["event_id"]
        try:
            await asyncio.wait_for(stop.wait(), 1)
        except TimeoutError:
            pass
    return observed


async def run(config, capture):
    bindings, profile, authority, clock, calibration = inputs(config)
    require(type(config["max_source_events"]) is int and 1 <= config["max_source_events"] <= 1000000, "explicit bounded source inventory required")
    capture.json("generator-calibration.json", calibration, "calibration")
    http = HTTP(config["api_origin"], config["application_origin"], ca_file=config["ca_file"])
    observer = PostgresObserver(**config["postgres"], pools=config["pools"])
    capture.json("postgres-readiness.json", await observer.readiness(), "native_provider")
    signer = WalletSigner(config["node"], config["node_sha256"], config["ethers_module"])
    accounts, sockets, states, readers, jobs = {}, {}, {}, {}, []
    stop = asyncio.Event()
    per_host = profile["sockets"] // 4
    faults_active = False
    started = None
    failures = []
    reconnects = 0
    sampled = set(range(min(128, per_host)))
    async def establish(index, preserved=None):
        global_index = config["host_index"]*per_host+index
        topics = topics_for(config["pools"], global_index)
        account = accounts.get(index)
        if preserved is None:
            raw = (await http.call("GET", "/v2/snapshots?topics="+quote(",".join(topics), safe=""), bearer=None if account is None else account.access_token, schema="TopicSnapshotReadResponse"))["data"]["snapshot"]
            preserved = SkewReceiverState(topics, raw, retain_fills=index in sampled)
            states[index] = {"state": preserved, "initial": dict(preserved.sequence), "topics": topics}
        socket, state = await connect_receiver(http, config["stream_origin"], topics, account, ca_file=config["ca_file"], state=preserved, retain_fills=index in sampled)
        sockets[index] = socket
        return socket, state
    async def receive(index):
        nonlocal reconnects
        while not stop.is_set():
            try:
                try:
                    event = await sockets[index].receive()
                except TimeoutError:
                    await sockets[index].send_frame(1, canonical({"op": "ping"}))
                    event = await sockets[index].receive()
                accepted = states[index]["state"].receive(event)
                capture.append("receiver-events.ndjson", {"receiver_id": config["host_id"]+":"+str(index), "received_utc_ns": str(time.time_ns()), "accepted": accepted, "event": event}, "receiver_ledger")
            except asyncio.CancelledError:
                raise
            except Exception as error:
                capture.append("receiver-errors.ndjson", {"receiver_id": index, "observed_utc_ns": str(time.time_ns()), "error": type(error).__name__}, "receiver_ledger")
                await sockets[index].close()
                elapsed = time.monotonic()-started if started is not None else -1
                expected_fault = faults_active and any(f["offset_seconds"] <= elapsed <= f["offset_seconds"]+f["duration_seconds"]+900 for f in config["coordinated_fault_schedule"])
                if not expected_fault:
                    failures.append({"receiver_id": index, "error": type(error).__name__})
                    return
                for attempt in range(30):
                    try:
                        await asyncio.sleep(min(30, 1+attempt))
                        await establish(index, states[index]["state"])
                        reconnects += 1
                        break
                    except Exception as retry_error:
                        capture.append("receiver-errors.ndjson", {"receiver_id": index, "attempt": attempt+1, "observed_utc_ns": str(time.time_ns()), "error": type(retry_error).__name__}, "receiver_ledger")
                else:
                    failures.append({"receiver_id": index, "error": "recovery_exhausted"})
                    return
    try:
        for index in range(per_host):
            if index % 2 == 0:
                accounts[index] = await login(http, signer)
        capture.json("accounts-public.json", [account.public() for account in accounts.values()], "socket_inventory")
        for offset in range(0, per_host, 64):
            indices = range(offset, min(offset+64, per_host))
            await asyncio.gather(*(establish(index) for index in indices))
            for index in indices:
                readers[index] = asyncio.create_task(receive(index))
        require(not failures, "real socket cohort failed during admission")
        capture.json("socket-inventory.json", {"host_id": config["host_id"], "connections": len(sockets), "authenticated": len(accounts), "public": len(sockets)-len(accounts), "topics": [{"receiver_id": index, "topics": row["topics"], "initial_sequence": row["initial"]} for index, row in states.items()]}, "socket_inventory")
        require(config["start_utc_ns"] > time.time_ns(), "cohort missed coordinated start; no shortened run permitted")
        await asyncio.sleep((config["start_utc_ns"]-time.time_ns())/1e9)
        started_utc, started = time.time_ns(), time.monotonic()
        cpu_before = linux_cpu_snapshot()
        process_before = process_resources()
        nic_name = config["nic_name"]
        require(re.fullmatch(r"[A-Za-z0-9_.:-]{1,64}", nic_name) and nic_name != "lo", "actual external generator NIC required")
        nic = Path("/sys/class/net")/nic_name
        speed = int((nic/"speed").read_text())*1000000
        tx_before = int((nic/"statistics/tx_bytes").read_text())
        rx_before = int((nic/"statistics/rx_bytes").read_text())
        faults_active = bool(config["coordinated_fault_schedule"])
        jobs.append(asyncio.create_task(native_samples(config, authority, capture, started, profile["seconds"], stop)))
        jobs.append(asyncio.create_task(actual_feed(config, capture, started, profile["seconds"])))
        jobs.append(asyncio.create_task(apply_faults(config, authority, capture, started, profile["seconds"])))
        source_task = asyncio.create_task(observe_source(observer, config, capture, stop))
        jobs.append(source_task)
        async def browsers():
            for offset in range(0, profile["seconds"], 600):
                await asyncio.sleep(max(0, started+offset-time.monotonic()))
                fresh = await login(http, signer)
                capture.append("browser-accounts.ndjson", {"receiver_id": config["host_id"]+"-"+str(offset)+"-browser", **fresh.public()}, "socket_inventory")
                await browser_capture({**config, "host_id": config["host_id"]+"-"+str(offset)}, fresh, capture)
        browser_task = asyncio.create_task(browsers())
        jobs.append(browser_task)
        await asyncio.sleep(profile["seconds"])
        await jobs[1]
        fault_results = await jobs[2]
        await browser_task
        native = await jobs[0]
        stop.set()
        source = await source_task
        ended_utc = time.time_ns()
        duration_ns = int((time.monotonic()-started)*1e9)
        cpu_after = linux_cpu_snapshot()
        tx_after, rx_after = int((nic/"statistics/tx_bytes").read_text()), int((nic/"statistics/rx_bytes").read_text())
        resource = {"cpu_before": cpu_before, "cpu_after": cpu_after, "cpu_fraction": cpu_fraction(cpu_before, cpu_after), "nic_fraction": nic_fraction(tx_before=tx_before, rx_before=rx_before, tx_after=tx_after, rx_after=rx_after, duration_ns=duration_ns, speed_bps=speed), "tx_before": tx_before, "tx_after": tx_after, "rx_before": rx_before, "rx_after": rx_after, "duration_ns": duration_ns, "speed_bps": speed}
        resource["process_before"] = process_before
        resource["process_after"] = process_resources()
        capture.json("generator-resources.json", resource, "resource_pool")
        require(resource["cpu_fraction"] <= .7 and resource["nic_fraction"] <= .7, "generator saturated during actual socket load")
        require(not failures, "healthy/recovered socket cohort incomplete")
        reference = []
        for index in sorted(sampled):
            row = states[index]
            topic = next(t for t in row["topics"] if t.startswith("market.trades."))
            expected = await observer.stream_fills([topic], row["initial"])
            state = row["state"]
            expected_digests = {entry["event_id"]: digest(canonical(entry["frame"])) for entry in expected}
            require(expected_digests and state.fills == expected_digests and state.checkpoint_count > 0, "acknowledged durable fill reference differs after recovery")
            reference.append({"receiver_id": index, "topic": topic, "expected_fill_count": len(expected), "reference_sha256": digest(canonical(expected)), "observed_identity_sha256": digest(canonical(sorted(state.fills)))})
            for entry in expected:
                capture.append("replay-reference.ndjson", {"receiver_id": index, **entry}, "receiver_ledger")
        capture.json("replay-comparison.json", reference, "receiver_ledger")
        for fault in fault_results:
            committed_during = {identity for identity, (_, row) in source.items() if int(fault["observed_start_utc_ns"]) <= int(row["durable_commit_utc_ns"]) <= int(fault["observed_end_utc_ns"])}
            if fault["scenario"] == "subscriber_stop":
                require(len(committed_during) == 10000, "actual subscriber interruption requires10000committed retained identities")
                observed_retained = set().union(*(set(states[index]["state"].fills) for index in sampled))
                scoped_expected = {entry["event_id"] for entry in capture.rows("replay-reference.ndjson") if entry["event_id"] in committed_during}
                require(scoped_expected and scoped_expected <= observed_retained, "committed identities during interruption not replayed")
            capture.append("fault-reference.ndjson", {"fault_id": fault["fault_id"], "committed_identity_count": len(committed_during), "committed_identity_sha256": digest(canonical(sorted(committed_during))), "reference_receivers": len(reference)}, "receiver_ledger")
        if fault_results:
            capture.flush()
            native_control = file_hash(capture.directory / "native-fault-controls.ndjson")
            for fault in fault_results:
                plan = next(f for f in config["fault_schedule"] if f["fault_id"] == fault["fault_id"])
                require(plan["affected_replica_ids"] and set(plan["affected_replica_ids"]) <= {g["replica_id"] for g in authority["gateways"]}, "actual affected gateway binding required")
                value = {"run_id": config["run_id"], "fault_id": fault["fault_id"], "action": "pause" if fault["scenario"] == "subscriber_stop" else "replica_loss", "affected_replica_ids": plan["affected_replica_ids"], "observed_start_utc_ns": fault["observed_start_utc_ns"], "observed_end_utc_ns": fault["observed_end_utc_ns"], "recovery_utc_ns": str(time.time_ns()), "native_control_sha256": native_control, "outcome": "recovered"}
                schema_validator(config, "FaultInterval").validate(value)
                capture.append("fault-controls.ndjson", value, "fault_control")
        browser_rows = list(capture.rows("browser-delivery.ndjson"))
        source_manifest = file_hash(capture.directory / "source-commits.ndjson")
        scopes = list(capture.rows("browser-scopes.ndjson"))
        expected_browser = set()
        starts = [row for row in scopes if row["kind"] == "browser_scope"]
        ends = [row for row in scopes if row["kind"] == "browser_done"]
        require(starts and len(starts) == len(ends), "every actual browser snapshot requires a completed capture interval")
        for scope, completion in zip(starts, ends):
            snapshot = scope["snapshot"]
            topics = [entry["topic"] for entry in snapshot["topics"]]
            initial = {entry["topic"]: int(entry["sequence"]) for entry in snapshot["topics"]}
            entries = await observer.stream_fills(topics, initial)
            require(scope["receiver_id"] == completion["receiver_id"] and int(scope["observed_utc_ns"]) < int(completion["observed_utc_ns"]) and completion["frames"] > 0, "actual browser duration/frame denominator absent")
            for entry in entries:
                identity = entry["event_id"]
                require(identity in source, "browser expected event lacks canonical commit evidence")
                committed = int(source[identity][1]["durable_commit_utc_ns"])
                if committed > int(completion["observed_utc_ns"]) - clock["clock_uncertainty_ns"]:
                    continue
                key = (completion["receiver_id"], identity)
                require(key not in expected_browser, "expected browser receiver/event identity repeated")
                expected_browser.add(key)
                capture.append("expected-browser-delivery.ndjson", {"run_id": config["run_id"], "receiver_id": completion["receiver_id"], "event_id": identity, "source_event_id": identity, "topic": entry["topic"], "scheduled_utc_ns": str(committed), "deadline_utc_ns": str(committed+1000000000), "source_manifest_sha256": source_manifest}, "expected_browser_delivery")
        observed_browser = {(row["receiver_id"], row["event_id"]) for row in browser_rows}
        require(expected_browser and observed_browser == expected_browser, "missing or unexplained browser deliveries remain in expected denominator")
        latency = latency_measurements([row for _, row in source.values()], browser_rows, clock_offset_ns=clock["clock_offset_ns"], clock_uncertainty_ns=clock["clock_uncertainty_ns"])
        latency["expected_delivery_denominator"] = len(expected_browser)
        latency["missing_deliveries"] = len(expected_browser-observed_browser)
        require(latency["target_passed"], "actual ingest-to-browser p99 exceeded1000ms")
        ledger = evaluate_native(config, profile, authority, native, started_utc, ended_utc)
        capture.json("native-capacity-math.json", ledger, "resource_pool")
        return {"task": "4.23", "host_id": config["host_id"], "host_index": config["host_index"], "machine_identity_sha256": file_hash("/etc/machine-id"), "profile": config["profile"], "bindings": dataclasses.asdict(bindings), "started_utc_ns": str(started_utc), "ended_utc_ns": str(ended_utc), "actual_duration_ns": duration_ns, "native_connections": len(sockets), "authenticated": len(accounts), "public": len(sockets)-len(accounts), "browser_probe_connections_additional": 1, "latency": latency, "reconnects": reconnects, "reference_receivers": len(reference), "generator_resources": resource, "native_math": ledger, "measurement_passed": True, "capacity_qualified": False, "monthly_availability_qualified": False, "final_gate": "9.1"}
    finally:
        stop.set()
        for task in jobs+list(readers.values()):
            if not task.done():
                task.cancel()
        await asyncio.gather(*(jobs+list(readers.values())), return_exceptions=True)
        await asyncio.gather(*(socket.close() for socket in sockets.values()), return_exceptions=True)
        signer.close()



def process_resources():
    values = {}
    for line in Path("/proc/self/status").read_text().splitlines():
        key, _, value = line.partition(":")
        if key in ("VmRSS", "VmHWM"):
            fields = value.split()
            require(len(fields) == 2 and fields[1] == "kB", "actual Linux resident-memory units required")
            values[key] = int(fields[0])*1024
    require(set(values) == {"VmRSS", "VmHWM"}, "actual process resident-memory counters absent")
    return {"rss_bytes": values["VmRSS"], "peak_rss_bytes": values["VmHWM"], "file_descriptors": len(list(Path("/proc/self/fd").iterdir())), "observed_utc_ns": str(time.time_ns())}


def aggregate(config, inputs):
    require(len(inputs) == 4, "four actual source-bound L23 host manifests required")
    expected = dataclasses.asdict(Bindings(**config["bindings"]))
    reports, seen = [], set()
    for item in inputs:
        manifest = bound_document(item["manifest_path"], item["manifest_sha256"])
        schema_validator(config, "CaptureManifest").validate(manifest)
        require(manifest["bindings"] == expected and manifest["run_id"] == config["run_id"] and manifest["scope"] == "isolated_component", "L23 host capture binding mismatch")
        root = Path(manifest["artifact_root"])
        require(root.is_absolute() and not root.is_symlink(), "actual host artifact root required")
        for artifact in manifest["artifacts"]:
            path = root / artifact["path"]
            require(not Path(artifact["path"]).is_absolute() and path.resolve().is_relative_to(root.resolve()) and not path.is_symlink() and file_hash(path) == artifact["sha256"], "native capture artifact digest differs")
        report = bound_document(item["result_path"], item["result_sha256"], maximum_bytes=67108864)
        require(report["task"] == "4.23" and report["bindings"] == expected and report["profile"] == config["profile"] and report["measurement_passed"] is True and report["capacity_qualified"] is False, "actual L23 host measurement incomplete")
        require(report["host_id"] not in seen and report["host_id"] == config["generator_hosts"][report["host_index"]], "duplicate or unbound generator host")
        seen.add(report["host_id"])
        require(report["native_connections"] == PROFILES[config["profile"]]["sockets"]//4 and report["authenticated"] == report["public"] == report["native_connections"]//2 and report["latency"]["missing_deliveries"] == 0 and report["latency"]["target_passed"], "actual mixed socket/expected browser denominator incomplete")
        reports.append(report)
    require(len({r["machine_identity_sha256"] for r in reports}) == 4, "four actual independent machine identities required")
    overlap = min(int(r["ended_utc_ns"]) for r in reports)-max(int(r["started_utc_ns"]) for r in reports)
    require(overlap >= PROFILES[config["profile"]]["seconds"]*1000000000, "actual four-host overlap shorter than profile")
    return {"task": "4.23", "profile": config["profile"], "bindings": expected, "measured_cohort_connections": sum(r["native_connections"] for r in reports), "host_result_sha256": [item["result_sha256"] for item in inputs], "common_overlap_ns": overlap, "measurement_passed": True, "native_gateway_captures_not_summed_across_duplicate_observers": True, "capacity_qualified": False, "monthly_availability_qualified": False, "final_gate": "9.1"}

def evaluate_native(config, profile, authority, rows, started, ended):
    ids = {gateway["replica_id"] for gateway in authority["gateways"]}
    by_replica = {identifier: [] for identifier in ids}
    for row in rows:
        if int(row["interval_start_utc_ns"]) >= started and int(row["interval_end_utc_ns"]) <= ended:
            by_replica[row["replica_id"]].append(row)
    require(all(by_replica.values()), "every admitted native replica must report edge writes")
    for records in by_replica.values():
        require(sum(int(row["interval_end_utc_ns"])-int(row["interval_start_utc_ns"]) for row in records) >= profile["seconds"]*1000000000, "native capture coverage shorter than the declared profile")
    intervals = {}
    for records in by_replica.values():
        for row in records:
            key = (int(row["interval_start_utc_ns"]), int(row["interval_end_utc_ns"]))
            intervals.setdefault(key, []).append(row)
    require(intervals and all({r["replica_id"] for r in rows} == ids for rows in intervals.values()), "native gateway measurements lack simultaneous replica coverage")
    steady, burst = [], []
    for (start, end), records in sorted(intervals.items()):
        frames = sum(r["delivered_frames"] for r in records)
        payload = sum(r["payload_bytes"] for r in records)
        connections = sum(r["accepted_connections"] for r in records)
        authenticated = sum(r["authenticated_connections"] for r in records)
        public = sum(r["public_connections"] for r in records)
        require(connections >= profile["sockets"] and authenticated >= profile["sockets"]//2 and public >= profile["sockets"]//2, "native mixed socket occupancy below admitted profile")
        subscriptions = sum(r["subscriptions_total"] for r in records)
        require(subscriptions == connections*5, "native mean subscription profile differs from5")
        hot = sum(r["hot_subscriptions"] for r in records)
        cold = sum(r["cold_subscriptions"] for r in records)
        require(hot+cold == subscriptions and hot*2 == subscriptions, "native50percent hot-pool skew differs")
        require(frames > 0, "actual market frames required")
        if config["profile"] != "preflight":
            require(payload == 400*frames, "actual mean application payload differs from400bytes")
        rate = frames*1000000000/(end-start)
        offset = (start-started)/1e9
        target = burst if config["burst_start_seconds"] <= offset < config["burst_start_seconds"]+config["burst_duration_seconds"] else steady
        target.append({"start_utc_ns": str(start), "end_utc_ns": str(end), "frames": frames, "payload_bytes": payload, "frames_per_second": rate})
    if profile["frames_per_second"] is not None:
        require(steady and all(r["frames_per_second"] >= 250000 for r in steady), "native steady250000frames/second not measured")
        require(burst and all(r["frames_per_second"] >= 1000000 for r in burst), "native1000000frames/second burst not measured")
    replicas = []
    for gateway in authority["gateways"]:
        values = by_replica[gateway["replica_id"]]
        payload = sum(row["payload_bytes"] for row in values)
        websocket = sum(row["websocket_bytes"] for row in values)
        tls = sum(row["tls_bytes"] for row in values)
        network = sum(row["network_bytes"] for row in values)
        duration = sum(int(row["interval_end_utc_ns"])-int(row["interval_start_utc_ns"]) for row in values)
        require(type(gateway["network_capacity_bps"]) is int and gateway["network_capacity_bps"] > 0 and gateway["node_type"] and gateway["tls_termination"], "actual node type/TLS placement/NIC capacity authority required")
        require(network*8*1000000000*10 <= gateway["network_capacity_bps"]*duration*7, "native transport network headroom below30percent")
        replicas.append({"replica_id": gateway["replica_id"], "node_type": gateway["node_type"], "tls_termination": gateway["tls_termination"], "payload_bytes": payload, "measured_websocket_bytes": websocket, "measured_tls_bytes": tls, "measured_network_bytes": network, "websocket_overhead_bytes": websocket-payload, "tls_overhead_bytes": tls-websocket, "network_overhead_bytes": network-tls, "max_memory_bytes": max(row["memory_bytes"] for row in values), "max_heap_after_gc_bytes": max(row["heap_after_gc_bytes"] for row in values), "memory_per_socket_observed_ratio": max(row["memory_bytes"]/row["accepted_connections"] for row in values if row["accepted_connections"]), "includes_process_baseline": True})
    return {"replicas": replicas, "steady_intervals": steady, "burst_intervals": burst, "mean_application_payload_bytes": sum(row["payload_bytes"] for row in rows)/sum(row["delivered_frames"] for row in rows), "minimum_expected_steady_payload_bytes_per_second": 100000000, "minimum_expected_burst_payload_bytes_per_second": 400000000, "capacity_qualified": False, "final_gate": "9.1"}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--config")
    parser.add_argument("--config-sha256")
    parser.add_argument("--output")
    parser.add_argument("--print-source-digest", action="store_true")
    parser.add_argument("--aggregate-inputs")
    parser.add_argument("--aggregate-inputs-sha256")
    args = parser.parse_args()
    if args.print_source_digest:
        print(generator_hash())
        return 0
    require(args.config and args.config_sha256 and args.output, "explicit config, digest and output required")
    config = bound_document(args.config, args.config_sha256)
    if args.aggregate_inputs:
        require(args.aggregate_inputs_sha256, "source-bound actual aggregate input manifest required")
        rows = bound_document(args.aggregate_inputs, args.aggregate_inputs_sha256)
        result = aggregate(config, rows)
        target = Path(args.output)
        require(target.is_absolute() and not any(p.is_symlink() for p in (target, *target.parents)) and target.resolve().is_relative_to(ROOT / "evidence/production-system/L23/phase4"), "lane-owned aggregate output required")
        fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "wb") as output:
            output.write(canonical(result)+b"\n")
        print(json.dumps({"task": "4.23", "measurement_passed": True, "capacity_qualified": False, "result_path": str(target)}))
        return 0
    capture = Capture(args.output, config["max_artifact_bytes"])
    result = {"task": "4.23", "measurement_passed": False, "capacity_qualified": False, "monthly_availability_qualified": False, "final_gate": "9.1"}
    started = time.time_ns()
    status = 1
    capture.json("input-bindings.json", {"config_sha256": args.config_sha256, "bindings": config["bindings"], "profile": config["profile"], "run_id": config["run_id"]}, "native_provider")
    try:
        result.update(asyncio.run(asyncio.wait_for(run(config, capture), PROFILES[config["profile"]]["seconds"]+1800)))
        status = 0 if result["measurement_passed"] else 1
    except Exception as error:
        result["failure_class"] = type(error).__name__
        result["failure"] = str(error)[:256] if isinstance(error, ValueError) else "Actual provider or measurement failed; retained private raw captures identify the boundary"
    finally:
        capture.close()
        manifest = {"schema_version": 1, "capture_version": "kindle-shared-capture-v1", "run_id": config["run_id"], "scope": "isolated_component", "final_gate": "9.1", "bindings": config["bindings"], "workload_version": VERSION, "root_capacity_ledger_sha256": config["root_capacity_ledger"]["sha256"], "root_mix_sha256": config["root_mix"]["sha256"], "scenario_manifest_sha256": config["scenario_manifest"]["sha256"], "started_utc_ns": str(started), "ended_utc_ns": str(time.time_ns()), "artifact_root": str(capture.directory), "artifacts": capture.inventory(), "generator_hosts": config["generator_hosts"], "gateway_replica_ids": [row["replica_id"] for row in bound_document(config["authority"]["path"], config["authority"]["sha256"])["gateways"]], "required_lane_ids": ["L23"], "native_runtime_authority_sha256": config["authority"]["sha256"], "clock_calibration_sha256": config["clock"]["sha256"], "capacity_qualified": False, "monthly_availability_qualified": False}
        try:
            schema_validator(config, "CaptureManifest").validate(manifest)
        except Exception as error:
            result["measurement_passed"] = False
            result["manifest_failure_class"] = type(error).__name__
            status = 1
        capture.json("capture-manifest.json", manifest, "native_provider")
        capture.json("result.json", result, "native_provider")
    print(json.dumps({"task": "4.23", "measurement_passed": result["measurement_passed"], "capacity_qualified": False, "result_path": str(capture.directory/"result.json")}))
    return status


if __name__ == "__main__":
    raise SystemExit(main())
