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
from urllib.parse import urlencode

from calibration import evaluate, cpu_fraction, linux_cpu_snapshot, nic_fraction
from capture import CAPTURE_VERSION, GatewaySampler, final_acceptance, gateway_summary, read_manifest, sha_file, shared_manifest, validate_record
from phase2_accounts import WalletSigner, login, metadata_headers
from phase2_feeder import PostgresObserver
from phase2_http import HTTP, HTTPFailure
from phase2_receivers import connect_receiver
from phase2_runner import resolve_routes, route_category
from phase2_types import Artifacts, Bindings, ROOT, bound_document, canonical, digest, source_digest, validate_v2
from phase3_runner import Histogram, SkewReceiverState, authorization_probe
from scheduler import Sample
from workload import MIX, VERSION, final_scenarios, ledger, provisioned_budget

PHASE_VERSION = "l48-integrated-harness-v1"


def topics_for(pools, index):
    if len(set(pools)) < 6 or any(not re.fullmatch(r"0x[0-9a-f]{40}", pool) for pool in pools):
        raise ValueError("six genuine admitted pools required")
    pool = pools[0] if index % 5 < 3 else pools[1 + (index // 5) % (len(pools) - 1)]
    return sorted(["market.trades." + pool, "market.price." + pool, "market.stats." + pool, "market.candles." + pool + ".1m", "market.candles." + pool + ".5m"])


def validate_runtime(config, bindings, *, final=False):
    authority = bound_document(config["native_runtime_authority_path"], config["native_runtime_authority_sha256"])
    if authority["bindings"] != dataclasses.asdict(bindings) or authority["workload_version"] != VERSION:
        raise ValueError("real runtime authority candidate differs")
    if len(set(authority["generator_hosts"])) != 4 or set(authority["generator_hosts"]) != set(config["generator_hosts"]):
        raise ValueError("four exact independently admitted external hosts required")
    for generator in authority["generators"]:
        if generator["host_id"] not in config["generator_hosts"] or generator["application_cluster_member"] is not False or abs(generator["clock_offset_ns"]) > 5000000 or generator["clock_uncertainty_ns"] > 5000000 or not generator["host_attestation_sha256"]:
            raise ValueError("independently collected generator topology/clock authority missing")
    if {generator["host_id"] for generator in authority["generators"]} != set(config["generator_hosts"]):
        raise ValueError("external host attestation coverage missing")
    required = {"L" + str(index).zfill(2) for index in range(1, 51)} - {"L48"}
    if final and set(authority["lane_runtime_bindings"]) != required:
        raise ValueError("all49 genuine other lane runtime authorities required")
    for lane, value in authority["lane_runtime_bindings"].items():
        if lane not in required or value["candidate"] != bindings.candidate or value["interface"] != bindings.interface or value["topology"] != bindings.topology or not re.fullmatch(r"[0-9a-f]{64}", value["tool_version_sha256"]):
            raise ValueError("lane runtime registration differs from frozen final candidate")
    if final:
        spec = (ROOT / "spec/production-system/spec.kvx").read_text()
        for identifier, status in (("4.99", "done"), ("9.1", "in_progress")):
            match = re.search(r"(?ms)^\[task\." + re.escape(identifier) + r"\]\n.*?(?=^\[|\Z)", spec)
            if not match or not re.search(r'^status = "' + status + r'"$', match.group(0), re.M):
                raise ValueError("final shared execution requires actual serial task9.1 admission after4.99")
        if authority["final_task"] != "9.1" or authority["coordinator_source_sha256"] != bindings.candidate:
            raise ValueError("final coordinator source freeze absent")
    return authority


async def http_arrivals(config, http, accounts, signer, output, start_ns, duration_seconds, rate):
    if type(rate) is not int or rate != 2500 or type(duration_seconds) is not int or not 60 <= duration_seconds <= 86400:
        raise ValueError("root peak workload shard and genuine duration required")
    maximum = config["http_max_inflight"]
    if type(maximum) is not int or not 1 <= maximum <= 4096:
        raise ValueError("bounded exact generator admission required")
    routes = resolve_routes(config["routes"])
    groups = {key: Histogram() for key in ("all", "reads", "quotes", "mutations")}
    categories = dict.fromkeys(MIX, 0)
    pending = set()
    def persist(sample, category, group):
        groups["all"].add(sample)
        groups[group].add(sample)
        output.write(canonical({**dataclasses.asdict(sample), "category": category, "slo_group": group, "host_id": config["host_id"]}).decode() + "\n")
    async def execute(sample, category, group):
        route = routes[category]
        account = accounts[sample.id % len(accounts)]
        sample.attempted_ns = time.monotonic_ns()
        try:
            values = {"wallet": account.wallet, "poolAddress": config["pools"][sample.id % len(config["pools"])], "tokenAddress": config["tokens"][sample.id % len(config["tokens"])], "category": config["ranking_category"]}
            path = route["template"]
            for key, value in values.items():
                path = path.replace("{" + key + "}", value)
            if "{" in path or "}" in path:
                raise ValueError("actual route substitutions absent")
            if route.get("query"):
                path += "?" + urlencode(route["query"])
            body, headers = route.get("body"), {}
            if route.get("request_schema"):
                validate_v2(route["request_schema"], body)
            if route.get("signature") == "metadata_v2":
                headers = await metadata_headers(signer, account, route["method"], path, body, config["metadata_audience"])
            value = await http.call(route["method"], path, body, bearer=account.access_token if route["authenticated"] else None, headers=headers, schema=route["response_schema"])
            if group == "quotes" and value.get("quality") != "complete":
                raise ValueError("healthy complete quote authority required")
            sample.status = "received"
        except TimeoutError:
            sample.status = "timeout"
        except HTTPFailure as error:
            sample.status, sample.error = "error", error.code
        except Exception as error:
            sample.status, sample.error = "error", type(error).__name__
        finally:
            sample.completed_ns = time.monotonic_ns()
            persist(sample, category, group)
    try:
        for index in range(rate * duration_seconds):
            sample = Sample(index, start_ns + index * 1000000000 // rate)
            await asyncio.sleep(max(0, (sample.scheduled_ns - time.monotonic_ns()) / 1e9))
            category = route_category(index * 4 + config["host_index"])
            group = "quotes" if category == "quotes" else ("mutations" if routes[category]["method"] != "GET" else "reads")
            categories[category] += 1
            if len(pending) >= maximum:
                sample.completed_ns = time.monotonic_ns()
                persist(sample, category, group)
            else:
                task = asyncio.create_task(execute(sample, category, group))
                pending.add(task)
                task.add_done_callback(pending.discard)
        if pending:
            await asyncio.gather(*tuple(pending))
    finally:
        for task in pending:
            task.cancel()
        if pending:
            await asyncio.gather(*tuple(pending), return_exceptions=True)
    return {"groups": {key: value.result() for key, value in groups.items()}, "category_counts": categories, "root_mix_sha256": digest(canonical(MIX)), "duration_seconds": duration_seconds, "capacity_qualified": False, "monthly_availability_qualified": False}


async def event_adapter(config, output, duration_seconds, start_utc_ns):
    adapter = config["event_adapter"]
    executable = Path(adapter["executable"])
    source = Path(__file__).parent / "eventdriver/main.go"
    if not executable.is_absolute() or executable.is_symlink() or sha_file(executable) != adapter["executable_sha256"] or sha_file(source) != adapter["source_sha256"] or adapter["mutation_admitted"] is not True:
        raise ValueError("actual source/build-bound admitted event adapter required")
    allowed = {"PGHOST", "PGPORT", "PGUSER", "PGDATABASE", "PGPASSWORD", "PGSSLMODE", "PGSSLROOTCERT", "PGSERVICE", "PGSERVICEFILE", "PGPASSFILE", "NATS_URL", "NATS_USER", "NATS_PASSWORD", "NATS_CA", "NATS_CERT", "NATS_KEY"}
    if not set(adapter["environment"]) <= allowed or not adapter["environment"]:
        raise ValueError("explicit private actual provider environment required")
    argv = [str(executable), "--stream", adapter["stream"], "--source-stream", adapter["source_stream"], "--delivery-schema", adapter["delivery_schema"], "--rate", "500", "--duration", str(duration_seconds), "--host-index", str(config["host_index"]), "--start-utc-ns", str(start_utc_ns)]
    child = await asyncio.create_subprocess_exec(*argv, env=adapter["environment"], stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.DEVNULL, start_new_session=True)
    counts = dict.fromkeys(("scheduled", "received", "timeout", "missed", "error"), 0)
    end = time.monotonic() + max(0, (start_utc_ns - time.time_ns()) / 1e9) + duration_seconds + 30
    try:
        while True:
            raw = await asyncio.wait_for(child.stdout.readline(), max(.001, end - time.monotonic()))
            if not raw:
                break
            if len(raw) > 8192:
                raise ValueError("event adapter sample bound exceeded")
            row = json.loads(raw)
            if row["status"] not in ("received", "timeout", "missed", "error") or row["scheduled_utc_ns"] < start_utc_ns or not row["source_authority"] == "canonical_postgres_delivery_envelope" or row["boundary"] != "file_jetstream_ack":
                raise ValueError("actual canonical event adapter output required")
            counts["scheduled"] += 1
            counts[row["status"]] += 1
            if row["status"] == "received" and (not re.fullmatch(r"[0-9a-f]{64}", row["event_id"]) or row["stream"] != adapter["stream"] or row["stream_sequence"] <= 0):
                raise ValueError("real immutable event acknowledgment differs")
            output.write(canonical(row).decode() + "\n")
        if await child.wait() != 0 or counts["scheduled"] != 500 * duration_seconds:
            raise ValueError("actual canonical replay source incomplete or adapter failed")
        return {**counts, "latency_denominator": counts["scheduled"], "component_scope": "real canonical publication replay through existing consumers", "new_raw_ingestion_qualified": False, "capacity_qualified": False}
    finally:
        if child.returncode is None:
            os.killpg(child.pid, signal.SIGTERM)
            try:
                await asyncio.wait_for(child.wait(), 2)
            except TimeoutError:
                os.killpg(child.pid, signal.SIGKILL)
                await child.wait()


async def run_isolated(config, artifacts):
    bindings = Bindings(**config["bindings"])
    if bindings.generator != source_digest() or type(config["host_index"]) is not int or config["host_index"] not in range(4) or config["generator_hosts"][config["host_index"]] != config["host_id"]:
        raise ValueError("actual current calibrated generator shard required")
    authority = validate_runtime(config, bindings)
    calibrations = [bound_document(row["path"], row["sha256"], maximum_bytes=67108864) for row in config["calibration_reports"]]
    calibration = evaluate(calibrations, candidate=bindings.candidate, topology=bindings.topology, generator=bindings.generator)
    if not calibration["generator_calibrated"] or {row["host_id"] for row in calibrations} != set(config["generator_hosts"]):
        raise ValueError("four actual external generators need current60second calibration")
    duration = config["duration_seconds"]
    if type(duration) is not int or duration != 60:
        raise ValueError("isolated component proof requires actual60second wallclock")
    sockets_count = config["socket_count"]
    if type(sockets_count) is not int or not 4 <= sockets_count <= 25000 or sockets_count % 2:
        raise ValueError("bounded even real socket cohort required")
    http = HTTP(config["api_origin"], config["application_origin"], ca_file=config.get("ca_file"), owned_local=config.get("owned_local", False))
    observer = PostgresObserver(**config["postgres"], pools=config["pools"])
    await observer.readiness()
    signer = WalletSigner(config["node"], config["node_sha256"], config["ethers_module"])
    sockets, readers, states, jobs, accounts = {}, {}, {}, [], []
    failures = []
    stop = asyncio.Event()
    outputs = {key: artifacts.rows(key + ".ndjson") for key in ("http-arrivals", "event-arrivals", "receiver-ledger", "source-commits", "gateway-egress", "native-gateway-samples")}
    async def receive(index):
        try:
            while not stop.is_set():
                event = await sockets[index].receive()
                accepted = states[index].receive(event)
                if accepted:
                    outputs["receiver-ledger"].write(canonical({"receiver_id": config["host_id"] + ":" + str(index), "event_id": event["eventId"], "topic": event["topic"], "sequence": event["sequence"], "revision": event["revision"], "received_utc_ns": str(time.time_ns()), "event": event}).decode() + "\n")
        except asyncio.CancelledError:
            raise
        except Exception as error:
            failures.append({"receiver_id": index, "error": type(error).__name__})
    try:
        for _ in range(sockets_count // 2):
            accounts.append(await login(http, signer))
        authorization = await authorization_probe(config, http, accounts[0], accounts[-1])
        artifacts.json("authorization.json", authorization)
        artifacts.json("root-mix.json", MIX)
        artifacts.json("workload-ledger.json", ledger())
        artifacts.json("generator-calibration.json", calibration)
        inventory = []
        for offset in range(0, sockets_count, 64):
            async def connect(index):
                topics = topics_for(config["pools"], index)
                account = accounts[index // 2] if index % 2 == 0 else None
                from urllib.parse import quote
                snapshot = (await http.call("GET", "/v2/snapshots?topics=" + quote(",".join(topics), safe=""), bearer=None if account is None else account.access_token, schema="TopicSnapshotReadResponse"))["data"]["snapshot"]
                state = SkewReceiverState(topics, snapshot, retain_fills=True)
                sockets[index], states[index] = await connect_receiver(http, config["stream_origin"], topics, account, ca_file=config.get("ca_file"), owned_local=config.get("owned_local", False), state=state, retain_fills=True)
                inventory.append({"receiver_id": index, "authenticated": account is not None, "topics": topics, "initial_sequence": dict(state.sequence)})
                readers[index] = asyncio.create_task(receive(index))
            await asyncio.gather(*(connect(index) for index in range(offset, min(offset + 64, sockets_count))))
        artifacts.json("socket-inventory.json", {"accepted_connections": len(sockets), "authenticated": sockets_count // 2, "public": sockets_count // 2, "inventory": inventory})
        start_utc = config["start_utc_ns"]
        if type(start_utc) is not int or not 1 <= start_utc - time.time_ns() <= 300000000000:
            raise ValueError("fresh coordinated future start required")
        jobs.append(asyncio.create_task(event_adapter(config, outputs["event-arrivals"], duration, start_utc)))
        await asyncio.sleep((start_utc - time.time_ns()) / 1e9)
        started_utc, start_ns = time.time_ns(), time.monotonic_ns()
        cpu_before = linux_cpu_snapshot()
        nic = config["nic_name"]
        if not re.fullmatch(r"[A-Za-z0-9_.:-]{1,64}", nic) or nic == "lo":
            raise ValueError("actual external generator NIC required")
        nic_root = Path("/sys/class/net") / nic
        speed = int((nic_root / "speed").read_text()) * 1000000
        tx_before = int((nic_root / "statistics/tx_bytes").read_text())
        rx_before = int((nic_root / "statistics/rx_bytes").read_text())
        jobs.insert(0, asyncio.create_task(http_arrivals(config, http, accounts, signer, outputs["http-arrivals"], start_ns, duration, 2500)))
        if config["host_index"] == 0:
            sampler = GatewaySampler(config["gateway_capture"], config["application_origin"], bindings, config["run_id"])
            jobs.append(asyncio.create_task(sampler.capture(duration, outputs["gateway-egress"], outputs["native-gateway-samples"])))
        result = await asyncio.gather(*jobs)
        elapsed = time.monotonic_ns() - start_ns
        measured_cpu = cpu_fraction(cpu_before, linux_cpu_snapshot())
        measured_nic = nic_fraction(tx_before=tx_before, rx_before=rx_before, tx_after=int((nic_root / "statistics/tx_bytes").read_text()), rx_after=int((nic_root / "statistics/rx_bytes").read_text()), duration_ns=elapsed, speed_bps=speed)
        schedule_p99 = result[0]["groups"]["all"]["scheduler_p99_ns_upper_bound"]
        if measured_cpu > .7 or measured_nic > .7 or schedule_p99 is None or schedule_p99 > 10000000:
            raise ValueError("generator headroom or scheduling qualification failed")
        if failures or any(state.frames <= 0 or state.checkpoint_count <= 0 for state in states.values()):
            raise ValueError("real socket/fanout proof lacks delivery/checkpoint or failed")
        after = None
        for _ in range(200):
            commits = await observer.committed(after=after)
            for row in commits:
                validate_record("SourceCommit", row)
                outputs["source-commits"].write(canonical(row).decode() + "\n")
            if len(commits) < 5000:
                break
            after = commits[-1]["event_id"]
        else:
            raise ValueError("bounded isolated source observation budget exceeded")
        return {"task": "4.48", "phase_version": PHASE_VERSION, "bindings": dataclasses.asdict(bindings), "host_id": config["host_id"], "host_index": config["host_index"], "run_id": config["run_id"], "started_utc_ns": started_utc, "ended_utc_ns": time.time_ns(), "duration_ns": elapsed, "http": result[0], "events": result[1], "gateway_capture": result[2] if len(result) > 2 else None, "accepted_connections": len(sockets), "socket_frames": sum(state.frames for state in states.values()), "socket_checkpoints": sum(state.checkpoint_count for state in states.values()), "generator_resources": {"cpu_fraction": measured_cpu, "nic_fraction": measured_nic, "scheduler_p99_ns": schedule_p99}, "isolated_component_qualified": True, "final_shared_acceptance": "pending task9.1", "capacity_qualified": False, "monthly_availability_qualified": False}
    finally:
        stop.set()
        for task in jobs + list(readers.values()):
            task.cancel()
        await asyncio.gather(*(jobs + list(readers.values())), return_exceptions=True)
        await asyncio.gather(*(socket.close() for socket in sockets.values()), return_exceptions=True)
        signer.close()


def readiness(config, reports):
    bindings = Bindings(**config["bindings"])
    authority = validate_runtime(config, bindings)
    if len(reports) != 4 or {report["host_index"] for report in reports} != set(range(4)) or {report["host_id"] for report in reports} != set(config["generator_hosts"]):
        raise ValueError("all four actual isolated component proofs required")
    for report in reports:
        if report["bindings"] != dataclasses.asdict(bindings) or report["phase_version"] != PHASE_VERSION or report["duration_ns"] < 60000000000 or report["http"]["groups"]["all"]["scheduled"] != 150000 or report["http"]["groups"]["all"]["received"] != 150000 or report["events"]["scheduled"] != 30000 or report["events"]["received"] != 30000 or report["socket_frames"] <= 0 or report["generator_resources"]["scheduler_p99_ns"] is None or report["generator_resources"]["scheduler_p99_ns"] > 10000000 or report["generator_resources"]["cpu_fraction"] > .7 or report["generator_resources"]["nic_fraction"] > .7:
            raise ValueError("isolated adapters incomplete or saturated")
    if min(report["ended_utc_ns"] for report in reports) - max(report["started_utc_ns"] for report in reports) < 60000000000:
        raise ValueError("actual four-host60second shared overlap required")
    categories = {name: sum(report["http"]["category_counts"][name] for report in reports) for name in MIX}
    if categories != {name: count * 60 for name, count in MIX.items()}:
        raise ValueError("all four actual workload shards must preserve frozen rootmix")
    budget_document = bound_document(config["budget_path"], config["budget_sha256"])
    if budget_document["bindings"] != dataclasses.asdict(bindings):
        raise ValueError("provision/cost authority source candidate differs")
    budget = provisioned_budget(budget_document)
    if not budget["final_run_permitted"]:
        raise ValueError("actual topology storage/network/cost budget insufficient")
    return {"task": "4.48", "bindings": dataclasses.asdict(bindings), "phase_version": PHASE_VERSION, "harness_ready": True, "isolated_component_qualified": True, "final_gate": {"task": "9.1", "status": "pending", "qualified": False}, "scenarios": final_scenarios(), "root_capacity_ledger_sha256": digest(canonical(ledger())), "root_mix_sha256": digest(canonical(MIX)), "runtime_authority_sha256": config["native_runtime_authority_sha256"], "budget": budget, "capacity_qualified": False, "monthly_availability_qualified": False}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", required=True)
    parser.add_argument("--config-sha256", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--readiness", action="store_true")
    parser.add_argument("--final-readout", action="store_true")
    args = parser.parse_args()
    config = bound_document(args.config, args.config_sha256)
    artifacts = Artifacts(args.output, phase=4)
    result = {"task": "4.48", "qualified": False, "capacity_qualified": False}
    exit_code = 1
    try:
        if args.final_readout:
            acceptance = final_acceptance(config)
            artifacts.json("shared-acceptance.json", acceptance)
            result.update(acceptance)
            result["qualified"] = False
            exit_code = 0
        elif args.readiness:
            reports = [bound_document(report["path"], report["sha256"], maximum_bytes=67108864) for report in config["isolated_host_reports"]]
            result.update(readiness(config, reports))
        else:
            result.update(asyncio.run(asyncio.wait_for(run_isolated(config, artifacts), 1800)))
        if not args.final_readout:
            result["qualified"] = bool(result.get("harness_ready", False) or result.get("isolated_component_qualified", False))
            exit_code = 0 if result["qualified"] else 1
    except Exception as error:
        result["error"] = type(error).__name__
    finally:
        artifacts.close()
        if result.get("isolated_component_qualified") and not args.readiness:
            kinds = {"authorization.json": "authorization", "root-mix.json": "workload_definition", "workload-ledger.json": "workload_definition", "generator-calibration.json": "calibration", "socket-inventory.json": "socket_inventory", "http-arrivals.ndjson": "http_arrivals", "event-arrivals.ndjson": "source_arrivals", "receiver-ledger.ndjson": "receiver_ledger", "source-commits.ndjson": "source_commits", "gateway-egress.ndjson": "gateway_egress", "native-gateway-samples.ndjson": "native_provider"}
            capture_config = {**config, "artifact_kinds": kinds}
            manifest = shared_manifest(artifacts=artifacts, bindings=Bindings(**config["bindings"]), run_id=config["run_id"], scope="isolated_component", started_utc_ns=result["started_utc_ns"], ended_utc_ns=result["ended_utc_ns"], config=capture_config)
            artifacts.json("capture-manifest.json", manifest)
            result["capture_manifest_sha256"] = sha_file(artifacts.directory / "capture-manifest.json")
        artifacts.json("artifact-sha256.json", artifacts.checksums())
        result["artifact_manifest_sha256"] = digest((artifacts.directory / "artifact-sha256.json").read_bytes())
        artifacts.json("result.json", result)
    print(json.dumps({"task": "4.48", "qualified": result["qualified"], "capacity_qualified": False, "result": str(artifacts.directory / "result.json")}))
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
