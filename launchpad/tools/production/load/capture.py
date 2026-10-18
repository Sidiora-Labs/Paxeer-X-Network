from __future__ import annotations

import asyncio
import dataclasses
from decimal import Decimal
import hashlib
import json
import math
from pathlib import Path
import re
import time
from urllib.parse import urlencode

from phase2_http import HTTP
from phase2_types import Bindings, HEX, ROOT, bound_document, canonical, digest, unique
from workload import MIX, VERSION, ledger

CAPTURE_VERSION = "kindle-shared-capture-v1"
SCHEMA_PATH = Path(__file__).with_name("final-shared-capture.schema.json")


def sha_file(path):
    path = Path(path)
    if path.is_symlink() or not path.is_file():
        raise ValueError("ordinary immutable artifact required")
    hasher = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1048576), b""):
            hasher.update(block)
    return hasher.hexdigest()


def validate_record(name, value):
    from jsonschema import Draft202012Validator
    definitions = json.loads(SCHEMA_PATH.read_bytes(), object_pairs_hook=unique)
    if name not in definitions["$defs"]:
        raise ValueError("unknown shared capture record")
    Draft202012Validator({**definitions, "$ref": "#/$defs/" + name}).validate(value)
    return value


def _ordinary_path(root, relative):
    if not isinstance(relative, str) or not relative or Path(relative).is_absolute() or any(part in ("..", ".") for part in Path(relative).parts):
        raise ValueError("artifact must be an exact relative path")
    path = root / relative
    for parent in (path, *path.parents):
        if parent == root.parent:
            break
        if parent.is_symlink():
            raise ValueError("artifact symlink denied")
    if not path.resolve().is_relative_to(root.resolve()) or not path.is_file():
        raise ValueError("artifact outside immutable capture root")
    return path


def read_manifest(path, expected_sha256, *, bindings):
    if not isinstance(bindings, Bindings):
        raise ValueError("current exact candidate bindings required")
    manifest = validate_record("CaptureManifest", bound_document(path, expected_sha256))
    if manifest["bindings"] != dataclasses.asdict(bindings) or manifest["workload_version"] != VERSION or manifest["root_capacity_ledger_sha256"] != digest(canonical(ledger())) or manifest["root_mix_sha256"] != digest(canonical(MIX)):
        raise ValueError("capture candidate/workload differs")
    if int(manifest["ended_utc_ns"]) <= int(manifest["started_utc_ns"]):
        raise ValueError("actual capture interval required")
    root = Path(manifest["artifact_root"])
    if not root.is_absolute() or root.is_symlink() or not root.is_dir():
        raise ValueError("actual immutable artifact root required")
    names = set()
    paths = set()
    for artifact in manifest["artifacts"]:
        if artifact["name"] in names or artifact["path"] in paths:
            raise ValueError("artifact identity duplicated")
        names.add(artifact["name"])
        paths.add(artifact["path"])
        if sha_file(_ordinary_path(root, artifact["path"])) != artifact["sha256"]:
            raise ValueError("artifact bytes differ from capture manifest")
    return manifest


def iter_artifact(manifest, kind, *, record=None, maximum_row_bytes=1048576):
    root = Path(manifest["artifact_root"])
    artifacts = [value for value in manifest["artifacts"] if value["kind"] == kind]
    if not artifacts:
        raise ValueError("required native capture kind absent: " + kind)
    for artifact in artifacts:
        path = _ordinary_path(root, artifact["path"])
        if sha_file(path) != artifact["sha256"]:
            raise ValueError("capture changed during consumption")
        if path.suffix == ".json":
            if path.stat().st_size > 33554432:
                raise ValueError("bounded native JSON artifact required")
            row = json.loads(path.read_bytes(), object_pairs_hook=unique)
            if record:
                validate_record(record, row)
            yield row
            continue
        with path.open("rb") as source:
            while True:
                line = source.readline(maximum_row_bytes + 1)
                if not line:
                    break
                if len(line) > maximum_row_bytes or not line.endswith(b"\n"):
                    raise ValueError("bounded complete capture row required")
                row = json.loads(line, object_pairs_hook=unique)
                if record:
                    validate_record(record, row)
                yield row


def gateway_summary(manifest):
    captures = set()
    last = {}
    total = dict.fromkeys(("delivered_frames", "chart_delivered_frames", "payload_bytes", "chart_payload_bytes", "websocket_bytes", "tls_bytes", "network_bytes", "source_accepted_events", "source_rejected_events", "forced_closes", "resnapshot_requests", "reconnects"), 0)
    per_second = {}
    heap = {}
    for row in iter_artifact(manifest, "gateway_egress", record="GatewayInterval"):
        if row["run_id"] != manifest["run_id"] or row["replica_id"] not in manifest["gateway_replica_ids"]:
            raise ValueError("foreign gateway delivery boundary")
        start, end = int(row["interval_start_utc_ns"]), int(row["interval_end_utc_ns"])
        identity = (row["replica_id"], row["capture_id"])
        if not 1 <= end - start <= 1100000000 or start < int(manifest["started_utc_ns"]) - 5000000 or end > int(manifest["ended_utc_ns"]) + 5000000:
            raise ValueError("gateway interval outside calibrated capture")
        if identity in last and start != last[identity]:
            raise ValueError("gateway capture gap/overlap")
        last[identity] = end
        captures.add(identity)
        if row["accepted_connections"] != row["authenticated_connections"] + row["public_connections"] or row["subscriptions_max_per_socket"] > 20 or row["subscriptions_total"] != row["hot_subscriptions"] + row["cold_subscriptions"]:
            raise ValueError("socket/subscription inventory differs")
        if row["chart_delivered_frames"] > row["delivered_frames"] or row["chart_payload_bytes"] > row["payload_bytes"] or not row["payload_bytes"] <= row["websocket_bytes"] <= row["tls_bytes"] <= row["network_bytes"]:
            raise ValueError("gateway payload/wire accounting differs")
        if row["slow_reader_queue_bytes_max"] > 262144 or row["queue_capacity"] <= 0 or row["queue_depth"] > row["queue_capacity"] or row["memory_limit_bytes"] <= 0 or row["memory_bytes"] >= row["memory_limit_bytes"] or row["generator_cpu_fraction"] > .7 or row["generator_nic_fraction"] > .7 or row["oom_kills"]:
            raise ValueError("queue/OOM/resource bound failed")
        for name in total:
            total[name] += row[name]
        second = (start - int(manifest["started_utc_ns"])) // 1000000000
        entry = per_second.setdefault(second, {"replicas": set(), "connections": 0, "authenticated": 0, "public": 0, "frames": 0, "chart_frames": 0, "payload_bytes": 0, "wire_bytes": 0, "subscriptions": 0, "slow_readers": 0})
        if row["replica_id"] in entry["replicas"]:
            raise ValueError("duplicate replica gateway egress interval")
        entry["replicas"].add(row["replica_id"])
        for key, source in (("connections", "accepted_connections"), ("authenticated", "authenticated_connections"), ("public", "public_connections"), ("frames", "delivered_frames"), ("chart_frames", "chart_delivered_frames"), ("payload_bytes", "payload_bytes"), ("wire_bytes", "network_bytes"), ("subscriptions", "subscriptions_total"), ("slow_readers", "slow_readers")):
            entry[key] += row[source]
        heap.setdefault(row["replica_id"], []).append((end, row["heap_gc_cycle"], row["heap_after_gc_bytes"]))
    if not captures:
        raise ValueError("actual gateway egress observations absent")
    for entry in per_second.values():
        if entry["replicas"] != set(manifest["gateway_replica_ids"]):
            raise ValueError("gateway fleet observation incomplete")
    return {"totals": total, "seconds": {str(key): {**value, "replicas": sorted(value["replicas"])} for key, value in per_second.items()}, "after_gc_heap": heap, "boundary": "edge_socket_write", "capacity_qualified": False}


class GatewaySampler:
    COUNTERS = {"delivered_frames", "chart_delivered_frames", "payload_bytes", "chart_payload_bytes", "websocket_bytes", "tls_bytes", "network_bytes", "forced_closes", "resnapshot_requests", "reconnects", "source_accepted_events", "source_rejected_events"}
    FRACTIONS = {"generator_cpu_fraction", "generator_nic_fraction"}
    def __init__(self, config, application_origin, bindings, run_id):
        self.http = HTTP(config["origin"], application_origin, owned_local=config.get("owned_local", False))
        self.authority_sha = config["authority_sha256"]
        self.authority = bound_document(config["authority_path"], self.authority_sha)
        if self.authority["bindings"] != dataclasses.asdict(bindings) or self.authority["boundary"] != "edge_socket_write":
            raise ValueError("actual gateway telemetry authority differs")
        schema = json.loads(SCHEMA_PATH.read_bytes())
        fields = set(schema["$defs"]["GatewayInterval"]["properties"])
        metadata = {"boundary", "replica_id", "capture_id", "run_id", "capture_version", "metrics_authority_sha256", "interval_start_utc_ns", "interval_end_utc_ns", "native_sample_before_sha256", "native_sample_after_sha256"}
        if set(self.authority["metrics"]) != fields - metadata:
            raise ValueError("complete actual gateway metric mapping required")
        if not 1 <= len(self.authority["replicas"]) <= 64 or len(set(self.authority["replicas"])) != len(self.authority["replicas"]) or any(not re.fullmatch(r"[A-Za-z0-9_.:-]{1,128}", value) for value in self.authority["replicas"]):
            raise ValueError("unique actual gateway replica identities required")
        for name in self.authority["metrics"].values():
            if not re.fullmatch(r"[a-zA-Z_:][a-zA-Z0-9_:]{0,127}", name):
                raise ValueError("native metric names required; arbitrary queries denied")
        self.run_id = run_id
        self.permits = asyncio.Semaphore(64)
        self.capture_ids = {replica: digest(canonical({"run_id": run_id, "replica_id": replica, "authority": self.authority_sha})) for replica in self.authority["replicas"]}

    async def snapshot(self, replica):
        async def value(field, name):
            query = name + '{replica_id="' + replica + '"}'
            async with self.permits:
                result = await self.http.call("GET", "/api/v1/query?" + urlencode({"query": query}))
            data = result.get("data", {})
            rows = data.get("result", [])
            if result.get("status") != "success" or data.get("resultType") != "vector" or len(rows) != 1 or rows[0]["metric"].get("replica_id") != replica:
                raise ValueError("actual per-replica native series missing/ambiguous")
            stamp, encoded = rows[0]["value"]
            if abs(time.time() - float(stamp)) > 2:
                raise ValueError("gateway telemetry is stale")
            numeric = Decimal(encoded)
            if not numeric.is_finite() or numeric < 0 or (field not in self.FRACTIONS and numeric != numeric.to_integral_value()):
                raise ValueError("exact nonnegative native counter/gauge required")
            observed = float(numeric) if field in self.FRACTIONS else int(numeric)
            return field, observed, {"field": field, "timestamp": stamp, "metric": rows[0]["metric"], "value": encoded}
        observed = await asyncio.gather(*(value(field, name) for field, name in self.authority["metrics"].items()))
        return {"observed_utc_ns": time.time_ns(), "values": {field: value for field, value, _ in observed}, "raw": [row for _, _, row in observed]}

    async def capture(self, duration_seconds, output, raw_output):
        replicas = self.authority["replicas"]
        if type(duration_seconds) is not int or not 60 <= duration_seconds <= 86400:
            raise ValueError("bounded genuine wallclock capture required")
        previous = dict(zip(replicas, await asyncio.gather(*(self.snapshot(replica) for replica in replicas))))
        start = time.monotonic_ns()
        for tick in range(1, duration_seconds + 1):
            await asyncio.sleep(max(0, (start + tick * 1000000000 - time.monotonic_ns()) / 1e9))
            current = dict(zip(replicas, await asyncio.gather(*(self.snapshot(replica) for replica in replicas))))
            for replica in replicas:
                left, right = previous[replica], current[replica]
                if not 1 <= right["observed_utc_ns"] - left["observed_utc_ns"] <= 1100000000:
                    raise ValueError("per-second gateway capture schedule exceeded")
                fields = dict(right["values"])
                for field in self.COUNTERS:
                    fields[field] -= left["values"][field]
                    if fields[field] < 0:
                        raise ValueError("native gateway counter reset; new capture epoch required")
                for field in ("source_cursor", "durable_watermark"):
                    fields[field] = str(fields[field])
                row = {**fields, "boundary": "edge_socket_write", "replica_id": replica, "capture_id": self.capture_ids[replica], "run_id": self.run_id, "capture_version": CAPTURE_VERSION, "metrics_authority_sha256": self.authority_sha, "interval_start_utc_ns": str(left["observed_utc_ns"]), "interval_end_utc_ns": str(right["observed_utc_ns"]), "native_sample_before_sha256": digest(canonical(left)), "native_sample_after_sha256": digest(canonical(right))}
                validate_record("GatewayInterval", row)
                raw_output.write(canonical({"replica_id": replica, "before": left, "after": right}).decode() + "\n")
                output.write(canonical(row).decode() + "\n")
            previous = current
        return {"replicas": replicas, "intervals_per_replica": duration_seconds, "duration_ns": time.monotonic_ns() - start, "boundary": "edge_socket_write", "capacity_qualified": False}


def shared_manifest(*, artifacts, bindings, run_id, scope, started_utc_ns, ended_utc_ns, config):
    kinds = config["artifact_kinds"]
    checksums = artifacts.checksums()
    if set(kinds) != set(checksums):
        raise ValueError("exact capture artifact kinds required")
    value = {"schema_version": 1, "capture_version": CAPTURE_VERSION, "run_id": run_id, "scope": scope, "final_gate": "9.1", "bindings": dataclasses.asdict(bindings), "workload_version": VERSION, "root_capacity_ledger_sha256": digest(canonical(ledger())), "root_mix_sha256": digest(canonical(MIX)), "scenario_manifest_sha256": config["scenario_manifest_sha256"], "started_utc_ns": str(started_utc_ns), "ended_utc_ns": str(ended_utc_ns), "artifact_root": str(artifacts.directory), "artifacts": [{"name": name, "path": name, "sha256": checksums[name], "kind": kinds[name]} for name in sorted(checksums)], "generator_hosts": config["generator_hosts"], "gateway_replica_ids": config["gateway_replica_ids"], "required_lane_ids": config["required_lane_ids"], "native_runtime_authority_sha256": config["native_runtime_authority_sha256"], "clock_calibration_sha256": config["clock_calibration_sha256"], "capacity_qualified": False, "monthly_availability_qualified": False}
    return validate_record("CaptureManifest", value)

def final_acceptance(config):
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
    from phase2_types import source_digest
    from workload import final_scenarios, provisioned_budget
    bindings = Bindings(**config["bindings"])
    if bindings.generator != source_digest():
        raise ValueError("current source/tool version required")
    manifest_sha = config["capture_manifest_sha256"]
    manifest = read_manifest(config["capture_manifest_path"], manifest_sha, bindings=bindings)
    if manifest["scope"] != "final_shared" or int(manifest["ended_utc_ns"]) - int(manifest["started_utc_ns"]) < 86400000000000:
        raise ValueError("actual final task9.1 full24hour capture required")
    authority = bound_document(config["runtime_authority_path"], manifest["native_runtime_authority_sha256"])
    if authority["bindings"] != dataclasses.asdict(bindings) or authority["final_task"] != "9.1" or authority["coordinator_source_sha256"] != bindings.candidate:
        raise ValueError("actual final candidate coordinator authority absent")
    lane_ids = {"L" + str(index).zfill(2) for index in range(1, 51)} - {"L48"}
    if set(authority["lane_runtime_bindings"]) != lane_ids or set(manifest["required_lane_ids"]) != lane_ids:
        raise ValueError("49 actual lane/runtime registrations absent")
    for lane, registration in authority["lane_runtime_bindings"].items():
        if registration["candidate"] != bindings.candidate or registration["interface"] != bindings.interface or registration["topology"] != bindings.topology or not HEX.fullmatch(registration["tool_version_sha256"]):
            raise ValueError("actual lane runtime source/tool binding differs: " + lane)
    public = bytes.fromhex(authority["scenario_attestation_ed25519_public_key"])
    if len(public) != 32 or digest(public) != authority["scenario_attestation_public_key_sha256"]:
        raise ValueError("admitted root native gate verification key required")
    key = Ed25519PublicKey.from_public_bytes(public)
    measured = gateway_summary(manifest)
    baseline = measured["seconds"]
    if not baseline or set(map(int, baseline)) != set(range(86400)):
        raise ValueError("complete measured per-second peak soak required")
    for row in baseline.values():
        if row["connections"] != 100000 or row["authenticated"] != 50000 or row["public"] != 50000 or row["subscriptions"] != 500000 or row["frames"] < 250000 or row["payload_bytes"] < 100000000:
            raise ValueError("root peak traffic missing from an individual second")
    for replica, observations in measured["after_gc_heap"].items():
        hour4 = [row for row in observations if int(manifest["started_utc_ns"]) + 14400000000000 <= row[0] <= int(manifest["started_utc_ns"]) + 14405000000000]
        hour24 = [row for row in observations if int(manifest["ended_utc_ns"]) - 5000000000 <= row[0] <= int(manifest["ended_utc_ns"])]
        if not hour4 or not hour24 or hour4[-1][2] <= 0 or hour24[-1][1] <= hour4[-1][1] or hour24[-1][2] > hour4[-1][2] * 1.05:
            raise ValueError("actual postGC4h-to24h retained heap bound failed")
    budget_document = bound_document(config["budget_path"], config["budget_sha256"])
    if budget_document["bindings"] != dataclasses.asdict(bindings):
        raise ValueError("final provision/cost authority candidate differs")
    budget = provisioned_budget(budget_document)
    if not budget["final_run_permitted"]:
        raise ValueError("final provision/cost authority insufficient")
    if any(row["wire_bytes"] * 8 > budget["provisioned"]["steady_network_bits_per_second"] for row in baseline.values()):
        raise ValueError("actual measured wire egress exceeds provisioned network capacity")
    scenarios = final_scenarios()
    if set(config["scenario_proofs"]) != set(scenarios):
        raise ValueError("all root final scenarios need admitted actual native validation")
    artifact_hashes = {artifact["sha256"] for artifact in manifest["artifacts"]}
    records = {}
    for name, reference in config["scenario_proofs"].items():
        signed = bound_document(reference["path"], reference["sha256"])
        if set(signed) != {"payload", "signature"}:
            raise ValueError("exact native scenario signed proof required")
        proof = signed["payload"]
        key.verify(bytes.fromhex(signed["signature"]), canonical(proof))
        if proof["task"] != "9.1" or proof["scenario"] != name or proof["bindings"] != dataclasses.asdict(bindings) or proof["capture_manifest_sha256"] != manifest_sha or proof["run_id"] != manifest["run_id"] or proof["status"] != "passed" or proof["native_exit_code"] != 0 or proof["tool_sha256"] != authority["native_validator_tools"][name] or proof["raw_artifact_sha256"] not in artifact_hashes or proof["scenario_definition_sha256"] != digest(canonical(scenarios[name])):
            raise ValueError("native scenario proof not bound to genuine current execution")
        if not 0 < int(proof["executed_started_utc_ns"]) <= int(proof["executed_ended_utc_ns"]) <= time.time_ns() or not proof["native_command"] or not HEX.fullmatch(reference["sha256"]):
            raise ValueError("native scenario actual command/timestamps absent")
        records[name] = {"status": "passed", "raw_artifact_sha256": proof["raw_artifact_sha256"]}
    return {"schema_version": 1, "task": "9.1", "capture_manifest_sha256": manifest_sha, "bindings": dataclasses.asdict(bindings), "run_id": manifest["run_id"], "scope": "final_shared", "tool_sha256": source_digest(), "workload_version": VERSION, "root_capacity_ledger_sha256": digest(canonical(ledger())), "root_mix_sha256": digest(canonical(MIX)), "scenarios": records, "status": "passed", "capacity_qualified": False, "release_qualified": False}
