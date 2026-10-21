from __future__ import annotations

import argparse
from datetime import datetime
import shutil
import asyncio
from decimal import Decimal
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import sys
import time

ROOT = Path(__file__).resolve().parents[4]
HASH = re.compile(r"[0-9a-f]{64}")
REQUIRED_SOURCES = {"services/launchpad/trading-charts/cmd/charts-measure/main.go", "services/launchpad/trading-charts/internal/engine/series.go", "services/launchpad/trading-charts/internal/store/retention.go", "tools/launchpad/production/charts-engine/phase4.py"}


def digest_file(path: Path) -> str:
    if not path.is_absolute() or path.is_symlink() or not path.is_file():
        raise ValueError("absolute regular source or executable required")
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def document(path: str, expected: str, maximum=1024 * 1024):
    file = Path(path)
    if not HASH.fullmatch(expected) or digest_file(file) != expected or file.stat().st_size > maximum:
        raise ValueError("bounded digest-bound actual input required")
    def pairs(items):
        result = {}
        for key, value in items:
            if key in result:
                raise ValueError("duplicate configuration field")
            result[key] = value
        return result
    return json.loads(file.read_text(), object_pairs_hook=pairs)


def validate_binding(plan):
    if set(plan) != {"measureConfig", "measureConfigSha256", "binary", "binarySha256", "sourceFiles", "environment", "releaseCommands", "maximumCommandLogBytes"}:
        raise ValueError("exact current executable/provider plan required")
    files = plan["sourceFiles"]
    required = set(REQUIRED_SOURCES)
    for package in ("services/launchpad/trading-charts", "sdk/launchpad/shared/accounting", "sdk/launchpad/shared/delivery", "sdk/launchpad/shared/storage", "sdk/launchpad/shared/interfaces/v2"):
        for source in (ROOT / package).rglob("*.go"):
            required.add(str(source.relative_to(ROOT)))
    for module in ("services/launchpad/trading-charts", "sdk/launchpad/shared"):
        for filename in ("go.mod", "go.sum"):
            required.add(f"{module}/{filename}")
    if not required <= files.keys():
        raise ValueError("all actual measurement and provider source/module paths must be bound")
    for name, expected in files.items():
        path = Path(name)
        if path.is_absolute() or ".." in path.parts or not HASH.fullmatch(expected) or digest_file(ROOT / path) != expected:
            raise ValueError("current candidate source digest differs")
    source_digest = hashlib.sha256(json.dumps(files, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    config = document(plan["measureConfig"], plan["measureConfigSha256"])
    if config["candidateDigest"] != source_digest:
        raise ValueError("candidate digest must name exact executable source manifest")
    if digest_file(Path(plan["binary"])) != plan["binarySha256"]:
        raise ValueError("actual built measurement executable differs")
    if not plan["environment"] or not all(isinstance(key, str) and isinstance(value, str) for key, value in plan["environment"].items()):
        raise ValueError("explicit actual provider environment required")
    if type(plan["maximumCommandLogBytes"]) is not int or not 1048576 <= plan["maximumCommandLogBytes"] <= 1073741824:
        raise ValueError("explicit bounded command log budget required")
    commands = plan["releaseCommands"]
    if not isinstance(commands, list):
        raise ValueError("actual release controller command inventory required")
    for command in commands:
        if set(command) != {"kind", "offsetSeconds", "argv", "executableSha256", "environment", "timeoutSeconds", "boundFiles"}:
            raise ValueError("exact approved controller command required")
        if command["kind"] not in {"workload", "replica_loss", "checkpoint_restore_replay"} or type(command["offsetSeconds"]) is not int or not 0 <= command["offsetSeconds"] < 86400:
            raise ValueError("actual command kind and bounded launch offset required")
        if not command["argv"] or not all(isinstance(arg, str) and arg for arg in command["argv"]) or digest_file(Path(command["argv"][0])) != command["executableSha256"]:
            raise ValueError("actual controller executable binding required")
        if type(command["timeoutSeconds"], plan["maximumCommandLogBytes"]) is not int or not 1 <= command["timeoutSeconds"] <= 86400:
            raise ValueError("bounded controller runtime required")
        for path, expected in command["boundFiles"].items():
            if digest_file(Path(path)) != expected:
                raise ValueError("actual workload/checkpoint command input differs")
        if not command["boundFiles"]:
            raise ValueError("actual controller inputs/checkpoint manifest must be bound")
    return config, source_digest


async def run_command(argv, environment, output: Path, timeout: int, maximum_bytes: int):
    with output.open("xb") as capture:
        child = await asyncio.create_subprocess_exec(*argv, env=environment, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.STDOUT, start_new_session=True)
        started = time.time_ns()
        async def drain():
            total = 0
            while True:
                block = await child.stdout.read(65536)
                if not block:
                    return
                total += len(block)
                if total > maximum_bytes:
                    raise ValueError("actual controller log byte budget exceeded")
                capture.write(block)
        try:
            _, code = await asyncio.wait_for(asyncio.gather(drain(), child.wait()), timeout)
            capture.flush()
            result = {"argvSha256": hashlib.sha256(json.dumps(argv, separators=(",", ":")).encode()).hexdigest(), "startedUtcNs": str(started), "endedUtcNs": str(time.time_ns()), "exitCode": code, "logSha256": digest_file(output.resolve())}
            if code != 0:
                raise RuntimeError("actual provider command failed; inspect private command log")
            return result
        finally:
            if child.returncode is None:
                os.killpg(child.pid, signal.SIGTERM)
                try:
                    await asyncio.wait_for(child.wait(), 5)
                except TimeoutError:
                    os.killpg(child.pid, signal.SIGKILL)
                    await child.wait()


def summarize(samples_path: Path, config, release: bool):
    totals = {key: {"accepted": 0, "hot": 0, "nanoseconds": 0, "publications": 0} for key in ("steady", "burst")}
    boundaries, resets, count = [], [], 0
    previous = None
    maximum = {key: 0 for key in ("waitingBackends", "activeBackends", "brokerPending", "brokerAckPending", "chartOutboxPending", "rollupPending", "observerQueryNanos", "unresolvedDeliveries", "gapSeries")}
    burst_ns = config["burstOffsetSeconds"] * 1_000_000_000
    burst_end_ns = burst_ns + 300_000_000_000
    first = last = None
    with samples_path.open() as samples:
        for line in samples:
            if len(line) > 1024 * 1024:
                raise ValueError("raw sample byte budget exceeded")
            row = json.loads(line)
            count += 1
            if count > 100000:
                raise ValueError("sample inventory budget exceeded")
            if first is None:
                first = row
            last = row
            for key in maximum:
                value = row[key]
                if type(value) is not int or value < 0:
                    raise ValueError("actual nonnegative raw counter required")
                maximum[key] = max(maximum[key], value)
            if previous is not None:
                duration = row["elapsedNanos"] - previous["elapsedNanos"]
                accepted = row["acceptedFills"] - previous["acceptedFills"]
                hot = row["hotPoolFills"] - previous["hotPoolFills"]
                publications = row["candlePublications"] - previous["candlePublications"]
                if duration <= 0:
                    raise ValueError("observed monotonic clock regressed")
                if min(accepted, hot, publications) < 0:
                    resets.append({"from": previous["elapsedNanos"], "to": row["elapsedNanos"], "acceptedDelta": accepted, "hotDelta": hot, "publicationDelta": publications})
                elif row["elapsedNanos"] > 86_400_000_000_000 or previous["stage"] != row["stage"] or previous["elapsedNanos"] < burst_ns < row["elapsedNanos"] or previous["elapsedNanos"] < burst_end_ns < row["elapsedNanos"]:
                    boundaries.append({"from": previous["elapsedNanos"], "to": row["elapsedNanos"], "accepted": accepted})
                else:
                    values = totals[row["stage"]]
                    values["accepted"] += accepted
                    values["hot"] += hot
                    values["publications"] += publications
                    values["nanoseconds"] += duration
            previous = row
    if count < 2 or last is None:
        raise ValueError("actual elapsed provider samples required")
    for name, values in totals.items():
        target = 2000 if name == "steady" else 10000
        values["targetCanonicalEventsPerSecond"] = target
        values["achievedCanonicalEventsPerSecond"] = str(Decimal(values["accepted"]) * Decimal(1_000_000_000) / Decimal(values["nanoseconds"])) if values["nanoseconds"] else None
        values["achievedHotPoolPercent"] = str(Decimal(values["hot"]) * 100 / Decimal(values["accepted"])) if values["accepted"] else None
        values["observedPublicationAmplification"] = str(Decimal(values["publications"]) / Decimal(values["accepted"])) if values["accepted"] else None
        values["hotPoolTargetPercent"] = 10
        values["hotPoolTargetObserved"] = values["accepted"] > 0 and values["hot"] * 100 >= 10 * values["accepted"]
        values["targetAchieved"] = values["nanoseconds"] > 0 and values["accepted"] * 1_000_000_000 >= target * values["nanoseconds"]
    full_duration = last["elapsedNanos"] >= 86_400_000_000_000
    return {"samples": count, "stages": totals, "observedMaximums": maximum, "boundaryIntervalsWithoutInventedStageAllocation": boundaries, "counterResets": resets, "first": first, "last": last, "actual24HourDurationObserved": full_duration, "chainThroughputInferred": False, "capacityQualified": False, "releaseTargetsObserved": release and full_duration and all(stage["targetAchieved"] and stage["hotPoolTargetObserved"] for stage in totals.values()), "globalAcceptance": "serial9.1"}


def utc_ns(value):
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if parsed.utcoffset() is None or parsed.utcoffset().total_seconds() != 0:
        raise ValueError("actual UTC commit time required")
    epoch = datetime.fromisoformat("1970-01-01T00:00:00+00:00")
    elapsed = parsed - epoch
    return ((elapsed.days * 86400 + elapsed.seconds) * 1000000 + elapsed.microseconds) * 1000


def summarize_latencies(path, config, first, last):
    start, stop = utc_ns(first["observedAt"]), utc_ns(last["observedAt"])
    burst_start = utc_ns(config["startAt"]) + config["burstOffsetSeconds"] * 1000000000
    burst_end = burst_start + 300000000000
    stages = {name: {"samples": 0, "histogramUpperMicros": {}, "maximumNanos": 0} for name in ("steady", "burst", "outside_capture")}
    with path.open() as rows:
        for line in rows:
            if len(line) > 8192:
                raise ValueError("raw commit latency row budget exceeded")
            row = json.loads(line)
            committed = utc_ns(row["chartCommittedAt"])
            latency = row["sourceToChartNanos"]
            if type(latency) is not int or latency < 0:
                raise ValueError("actual nonnegative PostgreSQL commit latency required")
            name = "outside_capture" if not start <= committed <= stop else "burst" if burst_start <= committed < burst_end else "steady"
            state = stages[name]
            state["samples"] += 1
            state["maximumNanos"] = max(state["maximumNanos"], latency)
            bucket = (latency + 999) // 1000
            bins = state["histogramUpperMicros"]
            bins[bucket] = bins.get(bucket, 0) + 1
            if len(bins) > 1000000:
                raise ValueError("measured latency histogram memory budget exceeded")
    for state in stages.values():
        bins = state.pop("histogramUpperMicros")
        state["quantileUpperNanos"] = {}
        for numerator, denominator in ((50, 100), (95, 100), (99, 100), (999, 1000)):
            target = (state["samples"] * numerator + denominator - 1) // denominator
            cumulative = 0
            quantile = None
            if target:
                for bucket, count in sorted(bins.items()):
                    cumulative += count
                    if cumulative >= target:
                        quantile = bucket * 1000
                        break
            state["quantileUpperNanos"][f"{numerator}/{denominator}"] = quantile
    return {"authority": "actual_postgresql_source_and_chart_commit_timestamps", "histogramScaleNanos": 1000, "stages": stages}


async def run(plan, config, output: Path, release: bool):
    logs = output / "commands"
    logs.mkdir(mode=0o700)
    coordinated_utc = datetime.fromisoformat(config["startAt"].replace("Z", "+00:00")).timestamp()
    started = time.monotonic() + (coordinated_utc - time.time()) if release else time.monotonic()
    if release and not 0 < coordinated_utc - time.time() <= 300:
        raise ValueError("fresh coordinated actual release start required")
    if shutil.disk_usage(output).free < config["rawArtifactBudgetBytes"] + plan["maximumCommandLogBytes"]:
        raise ValueError("declared raw artifact disk budget unavailable")
    controller_results = []
    async def controller(index, command):
        await asyncio.sleep(max(0, started + command["offsetSeconds"] - time.monotonic()))
        result = await run_command(command["argv"], command["environment"], logs / f"{index}-{command['kind']}.log", command["timeoutSeconds"])
        result["kind"] = command["kind"]
        controller_results.append(result)
    if release and {command["kind"] for command in plan["releaseCommands"]} != {"workload", "replica_loss", "checkpoint_restore_replay"}:
        raise ValueError("actual launch, replica-loss, database checkpoint/72-hour replay commands required for serial release")
    commands = [asyncio.create_task(controller(index, command)) for index, command in enumerate(plan["releaseCommands"])] if release else []
    measure_output = output / "measurement"
    argv = [plan["binary"], "--config", plan["measureConfig"], "--config-sha256", plan["measureConfigSha256"], "--output", str(measure_output)]
    if release:
        argv.append("--release")
    measure_task = asyncio.create_task(run_command(argv, plan["environment"], output / "measure.log", 90000 if release else 1800, plan["maximumCommandLogBytes"]))
    tasks = commands + [measure_task]
    try:
        await asyncio.gather(*tasks)
    finally:
        for task in tasks:
            if not task.done():
                task.cancel()
        await asyncio.gather(*tasks, return_exceptions=True)
    result = json.loads((measure_output / "result.json").read_text())
    report = summarize(measure_output / "samples.ndjson", config, release)
    report["actualControllerResults"] = controller_results
    report["restoreAnd72HourReplayCertified"] = False
    report["restoreAndReplayCertificationBoundary"] = "serial9.1 compares actual source-bound checkpoint and replay controller evidence with exact source conservation"
    report["sourceToChartCommitLatency"] = summarize_latencies(measure_output / "latencies.ndjson", config, report["first"], report["last"])
    report["exactEndState"] = result["parity"]
    report["retention"] = result["retention"]
    report["checkpointRestoreCounterResetObserved"] = bool(report["counterResets"])
    report["seriesRecoveryThroughActualSourceHeight"] = result["recoveryThroughHeight"]
    report["seriesRecoveryNanos"] = result["recoveryNanos"]
    report["retentionMissingObjectBlocked"] = result["retentionMissingObjectBlocked"]
    if release and (not report["releaseTargetsObserved"] or not report["retentionMissingObjectBlocked"] or not report["checkpointRestoreCounterResetObserved"]):
        raise ValueError("actual release targets or missing-object retention scenario not achieved")
    return report


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", required=True)
    parser.add_argument("--config-sha256", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--release", action="store_true")
    args = parser.parse_args()
    output = Path(args.output)
    if not output.is_absolute():
        raise ValueError("absolute private artifact directory required")
    output.mkdir(mode=0o700)
    result = {"task": "4.10", "harnessMeasurementsCaptured": False, "capacityQualified": False, "globalAcceptance": "serial9.1"}
    code = 1
    try:
        plan = document(args.config, args.config_sha256)
        config, source_digest = validate_binding(plan)
        result.update(asyncio.run(run(plan, config, output, args.release)))
        result.update(candidateDigest=source_digest, harnessMeasurementsCaptured=True)
        code = 0
    except Exception as error:
        result["failureClass"] = type(error).__name__
        result["failure"] = str(error)
    finally:
        checksums = {str(path.relative_to(output)): digest_file(path.resolve()) for path in sorted(output.rglob("*")) if path.is_file()}
        (output / "artifact-sha256.json").write_text(json.dumps(checksums, sort_keys=True, indent=2) + "\n")
        result["artifactManifestSha256"] = digest_file((output / "artifact-sha256.json").resolve())
        (output / "result.json").write_text(json.dumps(result, sort_keys=True, indent=2) + "\n")
    print(json.dumps({"task": "4.10", "exitCode": code, "capacityQualified": False, "result": str(output / "result.json")}))
    return code


if __name__ == "__main__":
    sys.exit(main())
