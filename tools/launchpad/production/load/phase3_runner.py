from __future__ import annotations

import argparse
import asyncio
import dataclasses
import json
import math
import os
from pathlib import Path
import re
import time
from urllib.parse import urlencode, quote

from calibration import evaluate, cpu_fraction, linux_cpu_snapshot, nic_fraction
from phase2_accounts import WalletSigner, login, metadata_headers
from phase2_feeder import PostgresObserver, feed_real_transactions, owned_command
from phase2_http import HTTP, HTTPFailure
from phase2_receivers import connect_receiver, WebSocket, HandshakeDenied, ReceiverState, ReconciliationError
from phase2_runner import dependency_probe, resolve_routes, route_category
from phase2_types import Artifacts, Bindings, bound_document, canonical, digest, source_digest, validate_v2
from scheduler import Sample
from workload import MIX, VERSION, ledger

PHASE_VERSION = "l48-skew-dependency-loss-v1"
FAULTS = ((120, "postgres"), (240, "redis"), (360, "nats"))
METRICS = {"queue_depth", "queue_capacity", "queue_age_seconds", "lock_wait_seconds", "cache_fill_duplicates_total", "delivery_latency_seconds", "heap_bytes", "gc_cycles_total", "memory_bytes", "memory_limit_bytes", "oom_kills_total", "cpu_fraction", "file_descriptors", "goroutines"}


def pool_for(index, pools, *, subscriptions=False):
    if len(pools) < 6 or len(set(pools)) != len(pools) or any(not re.fullmatch(r"0x[0-9a-f]{40}", p) for p in pools):
        raise ValueError("six distinct admitted canonical pools required")
    if type(index) is not int or index < 0:
        raise ValueError("nonnegative exact arrival identity required")
    if index % (5 if subscriptions else 10) < (4 if subscriptions else 6):
        return pools[0]
    return pools[1 + (index // (5 if subscriptions else 10)) % (len(pools) - 1)]


def skew_topics(pools, index):
    pool = pool_for(index, pools, subscriptions=True)
    return sorted(["market.trades." + pool, "market.price." + pool, "market.stats." + pool, "market.candles." + pool + ".1m", "market.candles." + pool + ".5m"])


class SkewReceiverState(ReceiverState):
    def receive(self, value):
        if "op" in value:
            validate_v2("StreamControl", value)
            self.controls += 1
            if value["op"] in ("error", "resnapshot_required", "integrity_incident", "subscription_closed"):
                raise ReconciliationError("explicit server control: " + value["op"])
            if value["op"] == "checkpoint":
                if not value["cursor"]:
                    raise ReconciliationError("checkpoint lacks opaque cursor")
                self.cursor = value["cursor"]
                self.checkpoint_count += 1
            return False
        validate_v2("StreamEvent", value)
        topic = value["topic"]
        if topic not in self.sequence:
            raise ReconciliationError("event outside accepted scope")
        sequence, revision = int(value["sequence"]), int(value["revision"])
        fingerprint = digest(canonical(value))
        if sequence <= self.sequence[topic] and topic.startswith("market.trades.") and self.fills is not None and self.fills.get(value["eventId"]) == fingerprint:
            return False
        if sequence == self.sequence[topic] and fingerprint == self.last_digest.get(topic):
            return False
        if sequence <= self.sequence[topic] or revision < self.revision[topic]:
            raise ReconciliationError("sequence or revision regression")
        if value["coalesced"]:
            if not topic.startswith(("market.price.", "market.stats.", "market.candles.", "market.rankings.")) or value["sourceSequenceFirst"] is None or value["sourceSequenceLast"] is None or int(value["sourceSequenceFirst"]) != self.sequence[topic] + 1 or int(value["sourceSequenceLast"]) != sequence:
                raise ReconciliationError("invalid coalesced source coverage")
        elif sequence != self.sequence[topic] + 1 or value["sourceSequenceFirst"] is not None or value["sourceSequenceLast"] is not None:
            raise ReconciliationError("durable source gap")
        if topic.startswith("market.trades.") and self.fills is not None:
            identity = value["payload"]["eventId"]
            if identity != value["eventId"]:
                raise ReconciliationError("fill identity differs from canonical event")
            previous = self.fills.get(identity)
            if previous is not None and previous != fingerprint:
                raise ReconciliationError("immutable fill changed")
            if len(self.fills) >= 1000000 and previous is None:
                raise ReconciliationError("bounded replay inventory exhausted")
            self.fills[identity] = fingerprint
        self.sequence[topic], self.revision[topic] = sequence, revision
        self.last_digest[topic] = fingerprint
        self.frames += 1
        return True


class Histogram:
    def __init__(self):
        self.counts = dict.fromkeys(("scheduled", "attempted", "received", "timeout", "missed", "error"), 0)
        self.bins, self.delay_bins = {}, {}

    def add(self, sample):
        if sample.status not in ("received", "timeout", "missed", "error") or sample.completed_ns is None or sample.completed_ns < sample.scheduled_ns:
            raise ValueError("terminal arrival required")
        self.counts["scheduled"] += 1
        self.counts[sample.status] += 1
        if sample.attempted_ns is not None:
            self.counts["attempted"] += 1
            delay = max(0, (sample.attempted_ns - sample.scheduled_ns + 999999) // 1000000)
            self.delay_bins[delay] = self.delay_bins.get(delay, 0) + 1
        if sample.status != "missed":
            elapsed = (sample.completed_ns - sample.scheduled_ns + 999999) // 1000000
            self.bins[elapsed] = self.bins.get(elapsed, 0) + 1

    @staticmethod
    def quantile(bins, fraction, denominator=None):
        target = math.ceil((sum(bins.values()) if denominator is None else denominator) * fraction)
        count = 0
        for bucket, entries in sorted(bins.items()):
            count += entries
            if count >= target:
                return bucket * 1000000
        return None

    def result(self):
        denominator = self.counts["scheduled"]
        return {**self.counts, "latency_denominator": denominator, "latency_censored": self.counts["missed"], "latency_ns_upper_bound": {str(p): self.quantile(self.bins, p, denominator) for p in (.5, .95, .99, .999, 1)}, "scheduler_p99_ns_upper_bound": self.quantile(self.delay_bins, .99), "eligible_errors": denominator - self.counts["received"], "availability": self.counts["received"] / denominator if denominator else None, "monthly_availability_qualified": False}


class IsolatedFaultControl:
    def __init__(self, config):
        executable = Path(config["executable"])
        if not executable.is_absolute() or executable.is_symlink() or digest(executable.read_bytes()) != config["sha256"]:
            raise ValueError("bound actual Docker executable required")
        if config.get("mutation_admitted") is not True or set(config["containers"]) != {"postgres", "redis", "nats"}:
            raise ValueError("explicit isolated dependency mutation admission required")
        if any(not re.fullmatch(r"[0-9a-f]{64}", value) for value in config["containers"].values()):
            raise ValueError("immutable actual container IDs required")
        if config["project"] != "kindle-bootstrap-local":
            raise ValueError("frozen isolated fixture project required")
        self.executable = str(executable)
        self.environment = {"PATH": "/usr/bin:/bin", "DOCKER_HOST": "unix:///var/run/docker.sock"}
        self.containers, self.project = dict(config["containers"]), config["project"]
        self.paused = set()

    async def command(self, *args):
        return await owned_command([self.executable, *args], environment=self.environment, timeout=5, maximum_bytes=1048576)

    async def inspect(self, dependency, *, paused=False):
        raw = await self.command("inspect", "--type", "container", self.containers[dependency])
        rows = json.loads(raw)
        if len(rows) != 1:
            raise ValueError("one exact isolated container required")
        row = rows[0]
        labels = row["Config"].get("Labels") or {}
        state = row["State"]
        if row["Id"] != self.containers[dependency] or labels.get("app.kindlelaunch.scope") != "isolated-bootstrap-fixture" or labels.get("com.docker.compose.project") != self.project or labels.get("com.docker.compose.service") != dependency or state["Running"] is not True or state["Paused"] is not paused or state.get("OOMKilled"):
            raise ValueError("isolated live dependency identity/state mismatch")
        return {"dependency": dependency, "container_id": row["Id"], "running": state["Running"], "paused": state["Paused"], "oom_killed": state.get("OOMKilled", False), "observed_utc_ns": time.time_ns()}

    async def pause(self, dependency):
        await self.inspect(dependency)
        self.paused.add(dependency)
        await self.command("pause", self.containers[dependency])
        return await self.inspect(dependency, paused=True)

    async def resume(self, dependency):
        await self.command("unpause", self.containers[dependency])
        result = await self.inspect(dependency)
        self.paused.discard(dependency)
        return result

    async def restore(self):
        errors = []
        for dependency in tuple(self.paused):
            try:
                await self.resume(dependency)
            except Exception as error:
                errors.append(type(error).__name__)
        if errors:
            raise RuntimeError("isolated dependency restoration failed")


class Telemetry:
    def __init__(self, config, application_origin, pools):
        self.http = HTTP(config["origin"], application_origin, owned_local=config.get("owned_local", False))
        contract = bound_document(config["contract_path"], config["contract_sha256"])
        if set(contract["metrics"]) != METRICS:
            raise ValueError("complete source-bound telemetry mapping required")
        self.metrics, self.pools = contract["metrics"], pools
        self.instance = contract["instance"]
        if not re.fullmatch(r"[A-Za-z0-9_.:-]{1,128}", self.instance):
            raise ValueError("exact application instance telemetry binding required")
        for key, metric in self.metrics.items():
            if not re.fullmatch(r"[a-zA-Z_:][a-zA-Z0-9_:]{0,127}", metric["name"]) or type(metric["per_pool"]) is not bool:
                raise ValueError("native exact metric selectors required")
            if key not in {"queue_depth", "queue_capacity", "queue_age_seconds", "lock_wait_seconds", "cache_fill_duplicates_total", "delivery_latency_seconds"} and metric["per_pool"]:
                raise ValueError("process metrics require exact instance scope")
            if key in {"queue_depth", "queue_capacity", "queue_age_seconds", "lock_wait_seconds", "cache_fill_duplicates_total", "delivery_latency_seconds"} and not metric["per_pool"]:
                raise ValueError("global averages cannot replace per-pool metrics")

    async def value(self, key, pool=None):
        metric = self.metrics[key]
        labels = 'instance="' + self.instance + '"'
        if metric["per_pool"]:
            if pool not in self.pools:
                raise ValueError("admitted pool label required")
            labels += ',pool="' + pool + '"'
        expression = metric["name"] + "{" + labels + "}"
        response = await self.http.call("GET", "/api/v1/query?" + urlencode({"query": expression}))
        data = response.get("data", {})
        rows = data.get("result", [])
        if response.get("status") != "success" or data.get("resultType") != "vector" or len(rows) != 1:
            raise ValueError("missing/ambiguous native telemetry")
        timestamp, text = rows[0]["value"]
        value = float(text)
        if not math.isfinite(value) or value < 0 or abs(time.time() - float(timestamp)) > 5 or rows[0]["metric"].get("instance") != self.instance or (metric["per_pool"] and rows[0]["metric"].get("pool") != pool):
            raise ValueError("stale/invalid telemetry identity")
        return {"value": value, "source_timestamp": timestamp, "metric": rows[0]["metric"]}

    async def sample(self):
        result = {"observed_utc_ns": time.time_ns(), "instance": self.instance, "pools": {}, "process": {}}
        for key, metric in self.metrics.items():
            if metric["per_pool"]:
                for pool in self.pools:
                    result["pools"].setdefault(pool, {})[key] = await self.value(key, pool)
            else:
                result["process"][key] = await self.value(key)
        process = result["process"]
        if process["memory_limit_bytes"]["value"] <= 0 or process["memory_bytes"]["value"] >= process["memory_limit_bytes"]["value"] or process["cpu_fraction"]["value"] > .7:
            raise ValueError("application resource headroom exhausted")
        for metrics in result["pools"].values():
            if metrics["queue_capacity"]["value"] <= 0 or metrics["queue_depth"]["value"] > metrics["queue_capacity"]["value"]:
                raise ValueError("unbounded or overflowing pool queue")
        return result


async def stress_http(config, http, accounts, signer, output, start_ns):
    routes = resolve_routes(config["routes"])
    maximum = config.get("http_max_inflight", 128)
    if type(maximum) is not int or not 1 <= maximum <= 4096:
        raise ValueError("bounded inflight limit required")
    histograms = {key: Histogram() for key in ("all", "hot", "cold", "reads", "mutations", "quotes")}
    categories = dict.fromkeys(MIX, 0)
    pending, pool_reads = set(), 0
    def persist(sample, category, pool, group):
        histograms["all"].add(sample)
        histograms[group].add(sample)
        if pool is not None and group == "reads":
            histograms["hot" if pool == config["pools"][0] else "cold"].add(sample)
        Artifacts.append(output, {**dataclasses.asdict(sample), "category": category, "pool": pool, "slo_group": group})
    async def execute(sample, category, pool, group):
        route = routes[category]
        account = accounts[sample.id % len(accounts)]
        path = route["template"]
        values = {"wallet": account.wallet, "poolAddress": pool, "tokenAddress": config["tokens"][sample.id % len(config["tokens"])], "category": config["ranking_category"]}
        sample.attempted_ns = time.monotonic_ns()
        try:
            for name, value in values.items():
                if "{" + name + "}" in path:
                    if value is None:
                        raise ValueError("missing route authority")
                    path = path.replace("{" + name + "}", value)
            if "{" in path or "}" in path:
                raise ValueError("unresolved current route")
            if route.get("query"):
                path += "?" + urlencode(route["query"])
            body = route.get("body")
            headers = {}
            if route.get("signature") == "metadata_v2":
                headers.update(await metadata_headers(signer, account, route["method"], path, body, config["metadata_audience"]))
            if route.get("request_schema"):
                validate_v2(route["request_schema"], body)
            await http.call(route["method"], path, body, bearer=account.access_token if route["authenticated"] else None, headers=headers, schema=route["response_schema"])
            sample.status = "received"
        except TimeoutError:
            sample.status = "timeout"
        except HTTPFailure as error:
            sample.status, sample.error = "error", error.code
        except Exception as error:
            sample.status, sample.error = "error", type(error).__name__
        finally:
            sample.completed_ns = time.monotonic_ns()
            persist(sample, category, pool, group)
    try:
        for index in range(1500000):
            sample = Sample(index, start_ns + index * 400000)
            await asyncio.sleep(max(0, (sample.scheduled_ns - time.monotonic_ns()) / 1e9))
            category = route_category(index * 4 + config["host_index"])
            route = routes[category]
            group = "mutations" if route["method"] != "GET" and category != "quotes" else ("quotes" if category == "quotes" else "reads")
            pool = None
            if group == "reads" and "{poolAddress}" in route["template"]:
                pool = pool_for(pool_reads, config["pools"])
                pool_reads += 1
            elif "{poolAddress}" in route["template"]:
                pool = config["pools"][index % len(config["pools"]) ]
            categories[category] += 1
            if len(pending) >= maximum:
                sample.completed_ns = time.monotonic_ns()
                persist(sample, category, pool, group)
            else:
                task = asyncio.create_task(execute(sample, category, pool, group))
                pending.add(task)
                task.add_done_callback(pending.discard)
        if pending:
            await asyncio.gather(*tuple(pending))
    finally:
        for task in pending:
            task.cancel()
        if pending:
            await asyncio.gather(*tuple(pending), return_exceptions=True)
    return {"categories": categories, "slo_targets": {"reads": .9995, "mutations": .999, "monthly_qualification": False, "fault_errors_excluded": False}, "groups": {key: histogram.result() for key, histogram in histograms.items()}, "pool_scoped_read_arrivals": pool_reads, "skew_denominator": "GET operations with frozen poolAddress path parameter", "unscoped_reads_reported_separately": True}


async def fault_schedule(controller, start_ns, output):
    observations = []
    try:
        for offset, dependency in FAULTS:
            scheduled_ns = start_ns + offset * 1000000000
            await asyncio.sleep(max(0, (scheduled_ns - time.monotonic_ns()) / 1e9))
            before = await controller.inspect(dependency)
            paused = await controller.pause(dependency)
            began = time.monotonic_ns()
            await asyncio.sleep(2)
            ended = time.monotonic_ns()
            restored = await controller.resume(dependency)
            row = {"dependency": dependency, "scheduled_ns": scheduled_ns, "paused_duration_ns": ended - began, "before": before, "paused": paused, "restored": restored}
            observations.append(row)
            Artifacts.append(output, row)
        return observations
    finally:
        await asyncio.shield(controller.restore())


async def chart_reconcile(observer, pools, output):
    selected = ",".join("'" + pool + "'" for pool in pools)
    sql = """SELECT json_build_object('pool',f.pool_address,'fill_count',count(*),'missing_inbox',count(*) FILTER(WHERE i.tx_hash IS NULL),'payload_mismatch',count(*) FILTER(WHERE i.fill<>f.payload),'missing_candle_source',count(*) FILTER(WHERE NOT EXISTS(SELECT 1 FROM candles.chart_stream_sources_v2 c WHERE c.pool=f.pool_address AND c.source_fill_id=f.source_fill_id))) FROM indexer.indexer_accounting_fills f JOIN indexer.indexer_raw_logs r USING(chain_id,transaction_hash,log_index) LEFT JOIN candles.chart_inbox_v2 i ON i.chain_id=f.chain_id::text AND i.pool=f.pool_address AND i.tx_hash=f.transaction_hash AND i.log_index=f.log_index AND i.emitter=r.emitter AND i.code_epoch=f.payload->'provenance'->>'emitterCodeEpoch' WHERE f.pool_address IN (""" + selected + ") GROUP BY f.pool_address"
    rows = await observer.query(sql)
    for row in rows:
        Artifacts.append(output, row)
    if {r["pool"] for r in rows} != set(pools) or any(r["fill_count"] <= 0 or r["missing_inbox"] or r["payload_mismatch"] or r["missing_candle_source"] for r in rows):
        raise ValueError("canonical fills/candle source reconciliation incomplete")
    counts = await observer.query("SELECT json_build_object('pool',pool,'trades',sum((snapshot->>'trades')::numeric)::text) FROM candles.chart_exact_v2 WHERE chain_id='125' AND interval='1m' AND pool IN (" + selected + ") GROUP BY pool")
    observed = {row["pool"]: int(row["trades"]) for row in counts}
    if any(observed.get(row["pool"]) != row["fill_count"] for row in rows):
        raise ValueError("duplicate swap effect or missing candle trade count")
    for row in counts:
        Artifacts.append(output, row)
    return {"pools": rows, "candle_trade_counts": counts, "canonical_identity_reconciled": True, "final_capacity_qualified": False}


async def authorization_probe(config, http, owner, foreign):
    topics = ["market.trades." + config["pools"][0]]
    private_path = "/v2/snapshots?topics=" + quote("private.portfolio." + owner.wallet, safe="")
    observations = []
    for principal, bearer in (("anonymous", None), ("foreign_account", foreign.access_token)):
        try:
            await http.call("GET", private_path, bearer=bearer)
        except HTTPFailure as error:
            if error.status not in (401, 403):
                raise ValueError("authorization denial must be explicit") from error
            observations.append({"principal": principal, "status": error.status, "observed_utc_ns": time.time_ns()})
        else:
            raise ValueError("private portfolio snapshot escaped account scope")
    body = {"schemaMajor": 2, "topics": topics, "filterDigest": digest(canonical(topics)), "origin": http.application_origin, "endpoint": "/v2/stream"}
    response = await http.call("POST", "/v2/stream/tickets", body, bearer=owner.access_token, schema="StreamTicketIssueResponse")
    ticket = response["data"]["ticket"]
    first = WebSocket(config["stream_origin"], http.application_origin, ca_file=config.get("ca_file"), owned_local=config.get("owned_local", False))
    second = WebSocket(config["stream_origin"], http.application_origin, ca_file=config.get("ca_file"), owned_local=config.get("owned_local", False))
    try:
        await first.connect(ticket)
        try:
            await second.connect(ticket)
        except HandshakeDenied as error:
            observations.append({"principal": "ticket_reuse", "denied": True, "status": error.status, "observed_utc_ns": time.time_ns()})
        else:
            raise ValueError("single-use ticket accepted twice")
    finally:
        await asyncio.gather(first.close(), second.close())
    return {"observations": observations, "authorization_passed": True}


async def run(config, artifacts):
    bindings = Bindings(**config["bindings"])
    pool_for(0, config["pools"])
    if bindings.generator != source_digest() or type(config["host_index"]) is not int or config["host_index"] not in range(4) or len(set(config["generator_hosts"])) != 4 or config["generator_hosts"][config["host_index"]] != config["host_id"]:
        raise ValueError("four external current source-bound generator identities required")
    reports = [bound_document(r["path"], r["sha256"], maximum_bytes=67108864) for r in config["calibration_reports"]]
    calibration = evaluate(reports, candidate=bindings.candidate, topology=bindings.topology, generator=bindings.generator)
    if not calibration["generator_calibrated"] or {r["host_id"] for r in reports} != set(config["generator_hosts"]):
        raise ValueError("current four-host calibration required")
    http = HTTP(config["api_origin"], config["application_origin"], ca_file=config.get("ca_file"), owned_local=config.get("owned_local", False))
    observer = PostgresObserver(**config["postgres"], pools=config["pools"])
    telemetry = Telemetry(config["telemetry"], config["application_origin"], config["pools"])
    controller = IsolatedFaultControl(config["fault_control"]) if config["host_index"] == 0 else None
    if controller:
        for _, dependency in FAULTS:
            await controller.inspect(dependency)
    artifacts.json("candidate-bindings.json", dataclasses.asdict(bindings))
    artifacts.json("workload-ledger.json", ledger())
    artifacts.json("generator-calibration.json", calibration)
    artifacts.json("dependencies.json", await dependency_probe(config, observer))
    outputs = {key: artifacts.rows(key + ".ndjson") for key in ("http-samples", "receiver-events", "fault-control", "resource-pool-telemetry", "chain-receipts", "chart-reconciliation")}
    signer = WalletSigner(config["node"], config["node_sha256"], config["ethers_module"])
    sockets, readers, states, jobs, accounts = {}, {}, {}, [], []
    stop = asyncio.Event()
    failures = []
    async def read(index):
        try:
            while not stop.is_set():
                value = await sockets[index].receive()
                if states[index]["state"].receive(value) and index % 200 == 0:
                    Artifacts.append(outputs["receiver-events"], {"receiver_id": index, "event": value, "received_utc_ns": time.time_ns()})
        except asyncio.CancelledError:
            raise
        except Exception as error:
            if len(failures) < 25000:
                failures.append({"receiver_id": index, "error": type(error).__name__, "observed_utc_ns": time.time_ns()})
    async def monitor(start_ns):
        before = None
        count = 0
        for tick in range(0, 601, 5):
            await asyncio.sleep(max(0, (start_ns + tick * 1000000000 - time.monotonic_ns()) / 1e9))
            row = await telemetry.sample()
            Artifacts.append(outputs["resource-pool-telemetry"], row)
            current = row["process"]
            if before is not None and (current["oom_kills_total"]["value"] != before["oom_kills_total"]["value"] or current["gc_cycles_total"]["value"] < before["gc_cycles_total"]["value"]):
                raise ValueError("OOM or GC counter reset observed")
            before = current
            count += 1
        return {"sample_count": count, "per_pool": True, "memory_and_gc_observed": True, "resource_headroom_passed": True}
    try:
        for _ in range(12500):
            accounts.append(await login(http, signer))
        artifacts.json("accounts-public.json", [account.public() for account in accounts])
        authorization = await authorization_probe(config, http, accounts[0], accounts[1])
        artifacts.json("authorization-denials.json", authorization)
        async def establish(index):
            topics = skew_topics(config["pools"], index)
            account = accounts[index % 12500] if index % 2 == 0 else None
            snapshot = (await http.call("GET", "/v2/snapshots?topics=" + quote(",".join(topics), safe=""), bearer=None if account is None else account.access_token, schema="TopicSnapshotReadResponse"))["data"]["snapshot"]
            state = SkewReceiverState(topics, snapshot, retain_fills=index % 200 == 0)
            socket, state = await connect_receiver(http, config["stream_origin"], topics, account, ca_file=config.get("ca_file"), owned_local=config.get("owned_local", False), state=state, retain_fills=index % 200 == 0)
            sockets[index] = socket
            states[index] = {"state": state, "topics": topics, "initial": dict(state.sequence)}
            readers[index] = asyncio.create_task(read(index))
        for offset in range(0, 25000, 64):
            await asyncio.gather(*(establish(index) for index in range(offset, min(offset + 64, 25000))))
        if failures:
            raise ValueError("actual skew cohort admission failed")
        artifacts.json("subscription-inventory.json", {"host_id": config["host_id"], "connections": 25000, "authenticated": 12500, "public": 12500, "hot_pool_subscribers": 20000, "hot_subscriptions": 100000, "cold_subscriptions": 25000, "subscriptions": [{"receiver_id": index, "topics": state["topics"], "initial_sequence": state["initial"]} for index, state in states.items()]})
        start_utc = config["baseline_start_utc_ns"]
        if type(start_utc) is not int or not 1 <= start_utc - time.time_ns() <= 300000000000:
            raise ValueError("fresh common stress start required")
        await asyncio.sleep((start_utc - time.time_ns()) / 1e9)
        started_utc, start_ns = time.time_ns(), time.monotonic_ns()
        cpu_before = linux_cpu_snapshot()
        nic = config["nic_name"]
        if not re.fullmatch(r"[A-Za-z0-9_.:-]{1,64}", nic) or nic == "lo":
            raise ValueError("actual external NIC required")
        nic_root = Path("/sys/class/net") / nic
        speed = int((nic_root / "speed").read_text()) * 1000000
        tx_before = int((nic_root / "statistics/tx_bytes").read_text())
        rx_before = int((nic_root / "statistics/rx_bytes").read_text())
        jobs.append(asyncio.create_task(stress_http(config, http, accounts, signer, outputs["http-samples"], start_ns)))
        jobs.append(asyncio.create_task(monitor(start_ns)))
        rpc = HTTP(config["rpc_origin"], config["application_origin"], owned_local=True)
        jobs.append(asyncio.create_task(feed_real_transactions(rpc, config["signed_transactions"], outputs["chain-receipts"], admitted_mutation=config["owned_chain_mutation_admitted"])))
        if controller:
            jobs.append(asyncio.create_task(fault_schedule(controller, start_ns, outputs["fault-control"])))
        gathered = await asyncio.gather(*jobs)
        http_result, resources = gathered[:2]
        duration = time.monotonic_ns() - start_ns
        cpu_after = linux_cpu_snapshot()
        tx_after = int((nic_root / "statistics/tx_bytes").read_text())
        rx_after = int((nic_root / "statistics/rx_bytes").read_text())
        generator_cpu = cpu_fraction(cpu_before, cpu_after)
        generator_nic = nic_fraction(tx_before=tx_before, rx_before=rx_before, tx_after=tx_after, rx_after=rx_after, duration_ns=duration, speed_bps=speed)
        if generator_cpu > .7 or generator_nic > .7 or http_result["groups"]["all"]["scheduler_p99_ns_upper_bound"] is None or http_result["groups"]["all"]["scheduler_p99_ns_upper_bound"] > 10000000:
            raise ValueError("generator saturated during faults")
        recovery = []
        for index in sorted({r["receiver_id"] for r in failures}):
            record = states[index]
            await sockets[index].close()
            sockets[index], state = await connect_receiver(http, config["stream_origin"], record["topics"], accounts[index % 12500] if index % 2 == 0 else None, ca_file=config.get("ca_file"), owned_local=config.get("owned_local", False), state=record["state"], retain_fills=index % 200 == 0)
            readers[index] = asyncio.create_task(read(index))
            recovery.append(index)
        await asyncio.sleep(5)
        for index, record in states.items():
            state = record["state"]
            if state.fills is not None:
                topic = next(t for t in record["topics"] if t.startswith("market.trades."))
                rows = await observer.stream_fills([topic], record["initial"])
                expected = {row["event_id"] for row in rows}
                if not expected or set(state.fills) != expected or state.checkpoint_count < 1:
                    raise ValueError("hot/cold canonical fill replay incomplete")
        reconciliation = await chart_reconcile(observer, config["pools"], outputs["chart-reconciliation"])
        artifacts.json("recovery-reconciliation.json", {"reconnected_receiver_ids": recovery, "forced_close_count": len(failures), "canonical_fills_reconciled": True, "charts": reconciliation})
        cold = http_result["groups"]["cold"]
        cold_passed = cold["latency_ns_upper_bound"]["0.99"] is not None and cold["latency_ns_upper_bound"]["0.99"] <= 400000000
        return {"task": "3.48", "host_id": config["host_id"], "host_index": config["host_index"], "bindings": dataclasses.asdict(bindings), "phase_version": PHASE_VERSION, "workload_version": VERSION, "started_utc_ns": started_utc, "ended_utc_ns": time.time_ns(), "duration_ns": duration, "accepted_connections": 25000, "hot_pool_subscribers": 20000, "hot_subscriptions": 100000, "cold_subscriptions": 25000, "authenticated_connections": 12500, "public_connections": 12500, "http": http_result, "cold_pool_p99_passed": cold_passed, "resources": resources, "faults": gathered[3] if controller else [], "fault_controller_host": config["generator_hosts"][0], "forced_close_count": len(failures), "recovered_receiver_count": len(recovery), "canonical_reconciliation_passed": True, "authorization_passed": authorization["authorization_passed"], "generator_resources": {"cpu_fraction": generator_cpu, "nic_fraction": generator_nic, "cpu_before": cpu_before, "cpu_after": cpu_after, "tx_before": tx_before, "tx_after": tx_after, "rx_before": rx_before, "rx_after": rx_after, "duration_ns": duration, "speed_bps": speed}, "monthly_availability_qualified": False, "phase3_scenario_qualified": False, "capacity_qualified": False, "final_capacity_task": "9.1"}
    finally:
        stop.set()
        for task in jobs + list(readers.values()):
            task.cancel()
        await asyncio.gather(*(jobs + list(readers.values())), return_exceptions=True)
        try:
            if controller:
                await asyncio.shield(controller.restore())
        finally:
            await asyncio.gather(*(socket.close() for socket in sockets.values()), return_exceptions=True)
            signer.close()


def aggregate_hosts(reports, bindings):
    if len(reports) != 4 or {report["host_index"] for report in reports} != set(range(4)) or len({report["host_id"] for report in reports}) != 4:
        raise ValueError("four distinct actual host reports required")
    for report in reports:
        if report["bindings"] != dataclasses.asdict(bindings) or report["phase_version"] != PHASE_VERSION or report["workload_version"] != VERSION or report["duration_ns"] < 600000000000 or report["accepted_connections"] != 25000 or report["hot_pool_subscribers"] != 20000 or report["hot_subscriptions"] != 100000 or report["cold_subscriptions"] != 25000 or report["authenticated_connections"] != 12500 or report["public_connections"] != 12500 or not report["cold_pool_p99_passed"] or not report["canonical_reconciliation_passed"] or not report["authorization_passed"] or not report["resources"]["resource_headroom_passed"] or report["resources"]["sample_count"] != 121 or report["forced_close_count"] != report["recovered_receiver_count"] or report["http"]["groups"]["all"]["scheduled"] != 1500000:
            raise ValueError("incomplete skew/fault host result")
        if report["capacity_qualified"] or report["monthly_availability_qualified"] or report["generator_resources"]["cpu_fraction"] > .7 or report["generator_resources"]["nic_fraction"] > .7:
            raise ValueError("invalid capacity or saturated generator result")
    if min(r["ended_utc_ns"] for r in reports) - max(r["started_utc_ns"] for r in reports) < 600000000000:
        raise ValueError("ten minute common stress overlap required")
    controller = next(r for r in reports if r["host_index"] == 0)
    if [f["dependency"] for f in controller["faults"]] != [dependency for _, dependency in FAULTS] or any(f["paused_duration_ns"] < 2000000000 or f["paused"]["paused"] is not True or f["restored"]["paused"] is not False for f in controller["faults"]) or any(r["faults"] for r in reports if r["host_index"] != 0):
        raise ValueError("one isolated real two-second dependency fault controller required")
    return {"phase3_scenario_qualified": True, "capacity_qualified": False, "monthly_availability_qualified": False, "accepted_connections": 100000, "hot_pool_subscribers": 80000, "hot_subscription_fraction": .8, "cold_subscription_fraction": .2, "bindings": dataclasses.asdict(bindings), "monthly_slo_targets": {"reads": .9995, "mutations": .999}, "all_dependency_fault_timeouts_retained": True, "host_artifact_digests": [r["artifact_manifest_sha256"] for r in reports], "final_capacity_task": "9.1"}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", required=True)
    parser.add_argument("--config-sha256", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    artifacts = Artifacts(args.output, phase=3)
    result = {"task": "3.48", "qualified": False, "capacity_qualified": False}
    exit_code = 1
    try:
        config = bound_document(args.config, args.config_sha256)
        result.update(asyncio.run(asyncio.wait_for(run(config, artifacts), 1800)))
        result["qualified"] = result["cold_pool_p99_passed"] and result["canonical_reconciliation_passed"]
        exit_code = 0 if result["qualified"] else 1
    except Exception as error:
        result["error"] = type(error).__name__
    finally:
        artifacts.close()
        artifacts.json("artifact-sha256.json", artifacts.checksums())
        result["artifact_manifest_sha256"] = digest((artifacts.directory / "artifact-sha256.json").read_bytes())
        artifacts.json("result.json", result)
    print(json.dumps({"task": "3.48", "qualified": result["qualified"], "capacity_qualified": False, "result": str(artifacts.directory / "result.json")}))
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
