#!/usr/bin/env python3
from __future__ import annotations

import argparse
import asyncio
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import re
import signal
import ssl
import sys
import time
import uuid
from urllib.parse import urlencode, urlsplit

ROOT = Path(__file__).resolve().parents[4]
HEX = re.compile(r"^[0-9a-f]{64}$")
ADDRESS = re.compile(r"^0x[0-9a-f]{40}$")
RESOLUTIONS = {"1": "1m", "5": "5m", "15": "15m", "60": "1h", "240": "4h", "D": "1d", "1D": "1d", "W": "1w", "1W": "1w"}


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False).encode()


def sha(path):
    result = hashlib.sha256()
    with Path(path).open("rb") as source:
        for block in iter(lambda: source.read(1048576), b""):
            result.update(block)
    return result.hexdigest()


def unique(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise ValueError("duplicate input key")
        value[key] = item
    return value


def document(record, maximum=256*1024*1024):
    if not isinstance(record, dict) or set(record) != {"path", "sha256"} or not isinstance(record["sha256"], str) or not HEX.fullmatch(record["sha256"]):
        raise ValueError("exact bound artifact required")
    path = Path(record["path"])
    if not path.is_absolute():
        path = ROOT / path
    if path.is_symlink() or not path.is_file() or not 0 < path.stat().st_size <= maximum or sha(path) != record["sha256"]:
        raise ValueError("artifact absent, oversized or digest differs")
    return json.loads(path.read_bytes(), object_pairs_hook=unique)


def load_module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def percentile(values, fraction):
    if not values:
        raise ValueError("missing measured latency samples")
    ordered = sorted(values)
    return ordered[math.ceil(len(ordered)*fraction)-1]


def origin(value):
    parsed = urlsplit(value)
    if parsed.scheme not in ("http", "https") or not parsed.hostname or parsed.username or parsed.password or parsed.path not in ("", "/") or parsed.query or parsed.fragment:
        raise ValueError("explicit origin required")
    if parsed.scheme == "http" and parsed.hostname not in ("127.0.0.1", "::1", "localhost"):
        raise ValueError("TLS required outside owned loopback")
    return value.rstrip("/")


def validate_window(window, pool):
    if set(window) != {"resolution", "from", "to", "countback", "content_sha256"} or window["resolution"] not in RESOLUTIONS:
        raise ValueError("exact genuine history window required")
    if any(type(window[key]) is not int for key in ("from", "to", "countback")) or not 0 <= window["from"] < window["to"] <= 253402300799 or window["to"]-window["from"] > 31622400 or window["countback"] not in (100,500,5000) or not HEX.fullmatch(window["content_sha256"]):
        raise ValueError("history window escapes bounded request mix")
    query = {"chainId": "125", "symbol": pool, "resolution": window["resolution"], "from": str(window["from"]), "to": str(window["to"]), "countback": str(window["countback"]), "mode": "price"}
    return "/v2/udf/history?" + urlencode(query)


def validate_config(config):
    required = {"schema_version", "candidate_source_sha256", "interface_sha256", "topology", "dataset", "license_manifest", "deployment", "common_workload", "controls", "postgres_dsn_env", "redis_url_env", "duration_seconds", "max_inflight", "max_db_connections", "minimum_durable_writes_per_second", "maximum_write_lag_seconds"}
    if set(config) != required or config["schema_version"] != 1 or any(not isinstance(config[key],str) or not HEX.fullmatch(config[key]) for key in ("candidate_source_sha256","interface_sha256")):
        raise ValueError("exact chart capacity configuration required")
    for key in ("duration_seconds","max_inflight","max_db_connections","minimum_durable_writes_per_second","maximum_write_lag_seconds"):
        if type(config[key]) is not int or config[key] < 1:
            raise ValueError("positive explicit capacity budgets required")
    if not 60 <= config["duration_seconds"] <= 900 or not 1 <= config["max_inflight"] <= 4096 or not re.fullmatch(r"[A-Z][A-Z0-9_]{0,100}", config["postgres_dsn_env"]) or not re.fullmatch(r"[A-Z][A-Z0-9_]{0,100}", config["redis_url_env"]):
        raise ValueError("bounded lane duration, concurrency and secret reference required")
    qualification = load_module("chart_source_snapshot", ROOT / "tools/launchpad/production/qualify.py")
    if qualification.snapshot(ROOT)["sha256"] != config["candidate_source_sha256"]:
        raise ValueError("candidate source binding differs")
    topology = document(config["topology"])
    if topology.get("candidate_source_sha256") != config["candidate_source_sha256"] or topology.get("interface_sha256") != config["interface_sha256"] or topology.get("environment") != "qualification" or not topology.get("fixture_id"):
        raise ValueError("owned qualification topology authority required")
    topology["chart_origin"] = origin(topology["chart_origin"])
    topology["frontend_origin"] = origin(topology["frontend_origin"])
    if not isinstance(topology.get("write_pools"),list) or not topology["write_pools"] or any(not ADDRESS.fullmatch(p) for p in topology["write_pools"]):
        raise ValueError("actual common-feeder write pool authority required")
    if not re.fullmatch(r"[a-z][a-z0-9_]{0,62}", topology["chart_schema"]):
        raise ValueError("exact chart schema required")
    dataset = document(config["dataset"])
    if dataset.get("source_kind") != "canonical_launch_history" or dataset.get("chain_id") != "125" or dataset.get("interface_sha256") != config["interface_sha256"] or not dataset.get("algorithm_version"):
        raise ValueError("actual canonical launch history authority required")
    document(dataset["canonical_source_manifest"])
    pools = dataset.get("pools")
    if not isinstance(pools,list) or len(pools) < 10 or len({p["address"] for p in pools}) != len(pools):
        raise ValueError("at least ten genuine active pools required")
    for pool in pools:
        if not ADDRESS.fullmatch(pool["address"]) or not pool.get("windows"):
            raise ValueError("actual active pool windows required")
        sizes = set()
        for window in pool["windows"]:
            validate_window(window,pool["address"])
            sizes.add(window["countback"])
        if len({validate_window(w,pool["address"]) for w in pool["windows"]}) != len(pool["windows"]):
            raise ValueError("duplicate history windows cannot claim cache-miss corpus")
        if sizes != {100,500,5000}:
            raise ValueError("nonempty real 100/500/5000-bar histories required for every pool")
    common = document(config["common_workload"])
    if common.get("candidate_source_sha256") != config["candidate_source_sha256"] or common.get("dataset_sha256") != config["dataset"]["sha256"] or common.get("topology_sha256") != config["topology"]["sha256"]:
        raise ValueError("shared dynamic workload bindings differ")
    for phase,total,charts in (("steady",10000,2000),("surge",20000,4000)):
        mix = common[phase+"_mix"]
        if not isinstance(mix,dict) or any(type(n) is not int or n < 0 for n in mix.values()) or sum(mix.values()) != total or mix.get("chart") != charts:
            raise ValueError("chart traffic must be included in exact shared 10000/20000 RPS budgets")
    controls = document(config["controls"])
    if controls.get("approved_environment") != "qualification" or controls.get("fixture_id") != topology["fixture_id"] or controls.get("topology_sha256") != config["topology"]["sha256"]:
        raise ValueError("reviewed owned fault/common-load controls required")
    for name in ("common_driver","redis_disable","redis_restore"):
        item = controls[name]
        argv = item.get("argv")
        if not isinstance(argv,list) or not argv or any(not isinstance(x,str) or not x or "\x00" in x for x in argv):
            raise ValueError("actual bounded provider command required")
        authority = document(item["authority"])
        if authority.get("argv") != argv or authority.get("fixture_id") != topology["fixture_id"] or authority.get("topology_sha256") != config["topology"]["sha256"] or authority.get("environment") != "qualification":
            raise ValueError("provider command does not match reviewed owned authority")
    license_module = load_module("chart_license_capacity", Path(__file__).with_name("phase2.py"))
    license_manifest = document(config["license_manifest"],16*1024*1024)
    prior = os.environ.get("L11_BROWSER_SOURCE_REVISION")
    os.environ["L11_BROWSER_SOURCE_REVISION"] = config["candidate_source_sha256"]
    try:
        license_module.validate_manifest(license_manifest)
        license_module.exercise_missing_assets(license_manifest)
    finally:
        if prior is None: os.environ.pop("L11_BROWSER_SOURCE_REVISION",None)
        else: os.environ["L11_BROWSER_SOURCE_REVISION"] = prior
    deployment = document(config["deployment"])
    if deployment.get("candidate_source_sha256") != config["candidate_source_sha256"] or deployment.get("license_manifest_sha256") != config["license_manifest"]["sha256"] or deployment.get("fixture_id") != topology["fixture_id"]:
        raise ValueError("source/deployment/license binding differs")
    if not deployment.get("assets"):
        raise ValueError("packaged asset manifest required")
    licensed_assets = {(str((ROOT / a["path"]).resolve()) if not Path(a["path"]).is_absolute() else str(Path(a["path"]).resolve()), a["sha256"]) for a in license_manifest["assets"]}
    deployed_assets = set()
    for asset in deployment["assets"]:
        source_path = Path(asset["source_path"])
        if not source_path.is_absolute(): source_path = ROOT / source_path
        source_path.resolve().relative_to(ROOT.resolve())
        if not source_path.is_file() or sha(source_path) != asset["sha256"] or not HEX.fullmatch(asset["sha256"]) or not asset["url_path"].startswith("/") or asset["url_path"].startswith("//") or any(x in asset["url_path"] for x in ("..","?","#","\\","\r","\n")):
            raise ValueError("deployable asset source identity differs")
        deployed_assets.add((str(source_path.resolve()),asset["sha256"]))
    if deployed_assets != licensed_assets:
        raise ValueError("licensed and deployed asset inventories differ")
    return topology,dataset,common,controls,deployment


def bound_file(record):
    if not isinstance(record,dict) or set(record) != {"path","sha256"} or not isinstance(record["sha256"],str) or not HEX.fullmatch(record["sha256"]):
        raise ValueError("bound raw measurement artifact required")
    path = Path(record["path"])
    if not path.is_absolute(): path = ROOT/path
    if path.is_symlink() or not path.is_file() or path.stat().st_size == 0 or sha(path) != record["sha256"]:
        raise ValueError("raw capture digest differs")
    return path


def validate_common_capture(companion,common,name,rate,duration,config,run_id,utc_start):
    if companion.get("run_id") != run_id or companion.get("candidate_source_sha256") != config["candidate_source_sha256"] or companion.get("dataset_sha256") != config["dataset"]["sha256"] or companion.get("chart_reserved_rps") != rate or companion.get("total_scheduled_rps") != rate*5 or type(companion.get("started_utc_ns")) is not int or companion["started_utc_ns"] > utc_start or companion.get("ended_utc_ns",0) < utc_start+duration*1000000000 or companion.get("failed_requests") != 0:
        raise ValueError("actual companion workload budget/window evidence differs")
    mix = common["surge_mix" if name == "surge" else "steady_mix"]
    expected = {key:value*duration for key,value in mix.items() if key != "chart"}
    total = sum(expected.values())
    observed = {key:0 for key in expected}
    seen = bytearray(total)
    for record in companion["raw_requests"]:
        with bound_file(record).open() as stream:
            for line in stream:
                if len(line) > 65536: raise ValueError("oversized common request capture")
                row = json.loads(line,object_pairs_hook=unique)
                identifier = row["id"]
                if type(identifier) is not int or not 0 <= identifier < total or seen[identifier] or row["run_id"] != run_id or row["category"] not in observed or type(row["status"]) is not int or not 200 <= row["status"] < 300 or not utc_start <= row["scheduled_utc_ns"] < utc_start+duration*1000000000 or row["completed_utc_ns"] < row["scheduled_utc_ns"]:
                    raise ValueError("failed, duplicated or out-of-window common request")
                seen[identifier] = 1
                observed[row["category"]] += 1
    if observed != expected or sum(seen) != total:
        raise ValueError("actual common reads/writes do not meet shared allocation")
    return observed


def validate_shared_soak(record,config):
    soak = document(record,16*1024*1024)
    for key in ("candidate_source_sha256","interface_sha256"):
        if soak.get(key) != config[key]: raise ValueError("24-hour source/interface binding differs")
    for key in ("dataset","topology","common_workload"):
        if soak.get(key+"_sha256") != config[key]["sha256"]: raise ValueError("24-hour shared artifact binding differs")
    start,end = soak["started_utc_ns"],soak["ended_utc_ns"]
    if type(start) is not int or type(end) is not int or end-start < 86400*1000000000:
        raise ValueError("full24-hour mixed soak required")
    dataset = document(config["dataset"])
    expected_contents = {validate_window(w,p["address"]):w["content_sha256"] for p in dataset["pools"] for w in p["windows"]}
    hot_pool = dataset["pools"][0]["address"]
    next_second = start
    count = 0
    with bound_file(soak["windows"]).open() as stream:
        for line in stream:
            if len(line)>1048576: raise ValueError("oversized shared soak window")
            window = json.loads(line,object_pairs_hook=unique)
            if window["started_utc_ns"] != next_second or window["ended_utc_ns"]-window["started_utc_ns"] != 1000000000:
                raise ValueError("shared soak has missing/overlapping second")
            next_second = window["ended_utc_ns"]
            if window["common_rps"] not in (10000,20000) or window["chart_scheduled"] != window["common_rps"]//5 or window["chart_success"] != window["chart_scheduled"] or window["chart_misses"]*5 < window["chart_scheduled"] or window["hot_pool_requests"]*10 != window["chart_scheduled"] or window["origin_p95_ms"]>150 or window["origin_p99_ms"]>400 or window["durable_writes_per_second"] < config["minimum_durable_writes_per_second"] or window["db_connections"] > config["max_db_connections"]:
                raise ValueError("strict mixed-soak budget failed")
            latencies = []
            origin_ids = set()
            with bound_file(window["raw_origin_latencies"]).open() as origins:
                for raw in origins:
                    if len(raw)>65536: raise ValueError("oversized origin sample")
                    item = json.loads(raw,object_pairs_hook=unique)
                    if item["request_id"] in origin_ids or type(item["duration_ms"]) not in (int,float) or not math.isfinite(item["duration_ms"]) or item["duration_ms"] < 0:
                        raise ValueError("invalid real origin latency sample")
                    origin_ids.add(item["request_id"]); latencies.append(item["duration_ms"])
            if percentile(latencies,.95) != window["origin_p95_ms"] or percentile(latencies,.99) != window["origin_p99_ms"]:
                raise ValueError("shared soak summary differs from raw origin timings")
            observed,misses,hot = 0,0,0
            identifiers = set()
            with bound_file(window["raw_requests"]).open() as requests:
                for raw in requests:
                    if len(raw)>65536: raise ValueError("oversized chart request sample")
                    item = json.loads(raw,object_pairs_hook=unique)
                    if item["id"] in identifiers or item.get("error") or item["status"] != 200 or item.get("request_path") not in expected_contents or item.get("content_sha256") != expected_contents[item["request_path"]] or item.get("cache") not in ("hit","miss","coalesced"):
                        raise ValueError("shared soak raw chart request failed correctness")
                    identifiers.add(item["id"])
                    observed += 1; misses += int(item["cache"] != "hit"); hot += int(item["pool"] == hot_pool)
            if observed != window["chart_success"] or misses != window["chart_misses"] or hot != window["hot_pool_requests"] or not origin_ids.issubset(identifiers):
                raise ValueError("shared soak denominator differs from raw requests")
            count += 1
    if count < 86400 or next_second != end:
        raise ValueError("24-hour shared output incomplete")
    return {"manifest":record,"seconds":count,"acceptance_owner":"9.1","final_capacity_qualified":False}


class Capture:
    def __init__(self, output, config):
        self.path = Path(output).resolve()
        self.path.relative_to((ROOT / "launchpad/evidence/production-system/L11/phase4").resolve())
        self.path.mkdir(parents=True,exist_ok=False)
        self.config = config
        self.opened = []

    def rows(self,name):
        stream = (self.path/name).open("x")
        self.opened.append(stream)
        return stream

    @staticmethod
    def append(stream,value):
        stream.write(canonical(value).decode()+"\n")

    def json(self,name,value):
        with (self.path/name).open("x") as stream:
            stream.write(json.dumps(value,indent=2,allow_nan=False)+"\n")

    def close(self):
        for stream in self.opened:
            stream.flush(); os.fsync(stream.fileno()); stream.close()
        self.json("artifact-sha256.json",{p.name:sha(p) for p in self.path.iterdir() if p.is_file()})


async def read_response(response,maximum):
    chunks = []
    size = 0
    while True:
        chunk = await response.content.read(min(65536,maximum+1-size))
        if not chunk: break
        size += len(chunk)
        if size > maximum: raise ValueError("actual response byte bound exceeded")
        chunks.append(chunk)
    return b"".join(chunks)


async def provider_command(item,environment,output,name):
    started = time.monotonic_ns()
    with (output/name).open("xb") as log:
        process = await asyncio.create_subprocess_exec(*item["argv"],cwd=ROOT,env={**os.environ,**environment},stdout=log,stderr=asyncio.subprocess.STDOUT,start_new_session=True)
        try:
            code = await asyncio.wait_for(process.wait(),30)
        except BaseException:
            os.killpg(process.pid,signal.SIGTERM)
            try: await asyncio.wait_for(process.wait(),5)
            except asyncio.TimeoutError:
                os.killpg(process.pid,signal.SIGKILL); await process.wait()
            raise
    if code != 0:
        raise ValueError("actual provider command failed: "+name)
    return {"exit_code":code,"log":name,"sha256":sha(output/name),"started_monotonic_ns":started,"completed_monotonic_ns":time.monotonic_ns()}


def check_chart(body,window,algorithm):
    if not isinstance(body,dict) or body.get("s") != "ok" or body.get("mode") != "price" or body.get("algorithmVersion") != algorithm:
        raise ValueError("empty, erroneous or incompatible history cannot qualify capacity")
    arrays = {key:body[key] for key in ("t","o","h","l","c","v")}
    if any(not isinstance(a,list) or len(a) != window["countback"] for a in arrays.values()):
        raise ValueError("wrong nonempty history length")
    for i,timestamp in enumerate(arrays["t"]):
        if type(timestamp) is not int or not window["from"] <= timestamp < window["to"] or (i and arrays["t"][i-1] >= timestamp):
            raise ValueError("history boundaries/order differ")
    for key in ("o","h","l","c","v"):
        if any(type(n) not in (int,float) or not math.isfinite(n) or n < 0 for n in arrays[key]):
            raise ValueError("malformed vendor monetary value")
    if hashlib.sha256(canonical(arrays)).hexdigest() != window["content_sha256"]:
        raise ValueError("actual chart output differs from genuine dataset")
    if body.get("watermark",{}).get("chainId") != "125" or body.get("watermark",{}).get("integrity") != "healthy":
        raise ValueError("chart source watermark unavailable")


def assert_plan(node,pool):
    if node.get("Node Type") in ("Seq Scan","Parallel Seq Scan"):
        raise ValueError("history plan scans unbounded or unrelated pool rows")
    condition = node.get("Index Cond","")
    indexed = node.get("Node Type") in ("Index Scan","Index Only Scan","Bitmap Index Scan") and all(field in condition for field in ("chain_id","pool","interval","start")) and pool in condition and "125" in condition
    for child in node.get("Plans",[]):
        indexed = assert_plan(child,pool) or indexed
    return indexed


def rows_examined(node):
    own = ((node.get("Actual Rows",0)+node.get("Rows Removed by Filter",0)+node.get("Rows Removed by Index Recheck",0))*node.get("Actual Loops",1)) if "Scan" in node.get("Node Type","") else 0
    return own+sum(rows_examined(child) for child in node.get("Plans",[]))


async def capture_plans(db,topology,dataset,capture):
    schema = '"'+topology["chart_schema"]+'"'
    plans = []
    for pool in dataset["pools"]:
        for size in (100,500,5000):
            window = next(w for w in pool["windows"] if w["countback"] == size)
            for name,sql,params in (
                ("revision",f"SELECT start::text,snapshot->>'revision' FROM {schema}.chart_exact_v1 WHERE chain_id='125' AND pool=$1 AND interval=$2 AND start>=$3 AND start<$4 ORDER BY start LIMIT 5001",[pool["address"],RESOLUTIONS[window["resolution"]],window["from"],window["to"]]),
                ("canonical",f"SELECT snapshot FROM {schema}.chart_exact_v1 WHERE chain_id='125' AND pool=$1 AND interval=$2 AND start>=$3 AND start<$4 ORDER BY start LIMIT $5",[pool["address"],RESOLUTIONS[window["resolution"]],window["from"],window["to"],size+1])):
                raw = await db.fetchval("EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+sql,*params,timeout=10)
                value = json.loads(raw) if isinstance(raw,str) else raw
                if not assert_plan(value[0]["Plan"],pool["address"]):
                    raise ValueError("missing full chain/pool/interval/start indexed path")
                plans.append({"pool":pool["address"],"bars":size,"query":name,"sql":sql,"parameters":params,"rows_examined":rows_examined(value[0]["Plan"]),"plan":value})
    capture.json("explain-plans.json",plans)


async def measure_phase(session,db,redis_client,config,topology,dataset,common,controls,capture,name,rate,run_id):
    duration = config["duration_seconds"]
    output = capture.rows(name+"-requests.ndjson")
    observations = capture.rows(name+"-postgres.ndjson")
    pools = dataset["pools"]
    start = time.monotonic_ns()+1000000000
    utc_start = time.time_ns()+1000000000
    environment = {"L11_RUN_ID":run_id,"L11_PHASE":name,"L11_CHART_RPS":str(rate),"L11_COMMON_RPS":str(rate*5),"L11_DURATION_SECONDS":str(duration),"L11_START_UTC_NS":str(utc_start),"L11_CANDIDATE_SHA256":config["candidate_source_sha256"],"L11_DATASET_SHA256":config["dataset"]["sha256"],"L11_COMMON_OUTPUT":str(capture.path/(name+"-common.json"))}
    driver_log = (capture.path/(name+"-common-driver.log")).open("xb")
    driver = await asyncio.create_subprocess_exec(*controls["common_driver"]["argv"],cwd=ROOT,env={**os.environ,**environment},stdout=driver_log,stderr=asyncio.subprocess.STDOUT,start_new_session=True)
    pending = set()
    summary = {"scheduled":rate*duration,"success":0,"shed":0,"failed":0,"missed_arrivals":0,"cache_hit":0,"cache_miss":0,"cache_coalesced":0,"hot_pool":0,"cold_scheduled":0,"mix":{"100":0,"500":0,"5000":0}}
    origins,latencies,delays = [],[],[]
    fault_done = False
    fault_task = None
    outage_started = None
    db_records = []
    schema = '"'+topology["chart_schema"]+'"'
    baseline_outbox = await db.fetchval(f"SELECT COALESCE(max(id),0) FROM {schema}.chart_outbox",timeout=5)
    sampler_stop = asyncio.Event()
    async def sample_db():
        while not sampler_stop.is_set():
            row = dict(await db.fetchrow("SELECT numbackends,xact_commit,tup_returned,tup_fetched,tup_inserted,tup_updated,blks_read,blks_hit FROM pg_stat_database WHERE datname=current_database()",timeout=2))
            schema = '"'+topology["chart_schema"]+'"'
            progress = dict(await db.fetchrow(f"SELECT (SELECT count(*) FROM {schema}.chart_outbox WHERE id>$1) AS committed_outbox, (SELECT count(*) FROM {schema}.chart_outbox WHERE published_at IS NULL) AS unpublished_outbox, EXTRACT(EPOCH FROM clock_timestamp())-min(watermark_timestamp) AS write_lag_seconds FROM {schema}.chart_series_clock WHERE chain_id='125' AND pool=ANY($2::text[])",baseline_outbox,topology["write_pools"],timeout=2))
            record = {"utc_ns":time.time_ns(),"monotonic_ns":time.monotonic_ns(),**row,**progress}
            for key in ("write_lag_seconds",):
                if record[key] is None: raise ValueError("actual durable aggregation clock unavailable")
                record[key] = float(record[key])
            db_records.append(record)
            Capture.append(observations,record)
            if record["write_lag_seconds"] > config["maximum_write_lag_seconds"]: raise ValueError("durable chart write lag budget exceeded")
            if row["numbackends"] > config["max_db_connections"]:
                raise ValueError("common database connection budget exceeded")
            try: await asyncio.wait_for(sampler_stop.wait(),1)
            except asyncio.TimeoutError: pass
    sampler = asyncio.create_task(sample_db())
    async def disable_redis():
        nonlocal outage_started
        result = await provider_command(controls["redis_disable"],environment,capture.path,"redis-disable.log")
        try:
            await redis_client.ping()
        except Exception as error:
            if type(error).__name__ not in ("ConnectionError","TimeoutError","OSError"):
                raise
        else:
            raise ValueError("Redis remained available after approved fault command")
        outage_started = time.monotonic_ns()
        result["redis_unavailable_observed_monotonic_ns"] = outage_started
        capture.json("redis-disable.json",result)
    async def request(index,scheduled,pool,window,cold):
        attempted = time.monotonic_ns()
        item = {"id":run_id+":"+name+":"+str(index),"scheduled_ns":scheduled,"attempted_ns":attempted,"pool":pool["address"],"countback":window["countback"],"request_path":validate_window(window,pool["address"]),"cold_scheduled":cold,"dependency_phase":"redis_unavailable" if outage_started is not None else "healthy"}
        try:
            async with session.get(topology["chart_origin"]+validate_window(window,pool["address"]),headers={"X-Request-ID":item["id"]},allow_redirects=False) as response:
                raw = await read_response(response,16*1024*1024)
                item["status"] = response.status
                item["bytes"] = len(raw)
                item["response_sha256"] = hashlib.sha256(raw).hexdigest()
                if len(raw)>16*1024*1024: raise ValueError("oversized chart response")
                if response.status in (429,503):
                    if not response.headers.get("Retry-After"): raise ValueError("saturation omitted Retry-After")
                    summary["shed"] += 1
                elif response.status == 200:
                    body = json.loads(raw,object_pairs_hook=unique)
                    check_chart(body,window,dataset["algorithm_version"])
                    item["content_sha256"] = window["content_sha256"]
                    state = response.headers.get("X-Chart-Cache")
                    if state not in ("hit","miss","coalesced"): raise ValueError("actual cache disposition telemetry unavailable")
                    summary["cache_"+state] += 1
                    item["cache"] = state
                    if state == "miss":
                        matched = re.fullmatch(r"chart-origin;dur=([0-9]+(?:\.[0-9]+)?)",response.headers.get("Server-Timing",""))
                        if matched is None: raise ValueError("measured origin timing unavailable")
                        latency = float(matched.group(1))
                        if not math.isfinite(latency) or latency < 0: raise ValueError("invalid origin timing")
                        origins.append(latency); item["origin_ms"] = latency
                    summary["success"] += 1
                    latencies.append((time.monotonic_ns()-scheduled)/1e6)
                else:
                    raise ValueError("unexpected history HTTP status")
        except Exception as error:
            summary["failed"] += 1
            item["error"] = type(error).__name__
        finally:
            item["completed_ns"] = time.monotonic_ns()
            delays.append((attempted-scheduled)/1e6)
            Capture.append(output,item)
    try:
        for index in range(summary["scheduled"]):
            scheduled = start+index*1000000000//rate
            await asyncio.sleep(max(0,(scheduled-time.monotonic_ns())/1e9))
            if sampler.done(): await sampler
            if fault_task is not None and fault_task.done(): await fault_task
            if driver.returncode is not None: raise ValueError("common workload driver stopped before chart phase")
            if name == "redis-outage" and not fault_done and index >= summary["scheduled"]//2:
                fault_done = True
                fault_task = asyncio.create_task(disable_redis())
            slot = index%100
            size = 500 if slot < 50 else 100 if slot < 90 else 5000
            hot = index%10 == 0
            pool = pools[0] if hot else pools[1+(index//10)%(len(pools)-1)]
            windows = [w for w in pool["windows"] if w["countback"] == size]
            cold = index%5 == 4
            window = windows[(index//5)%len(windows)] if cold else windows[0]
            summary["mix"][str(size)] += 1
            summary["hot_pool"] += int(hot)
            summary["cold_scheduled"] += int(cold)
            if len(pending) >= config["max_inflight"]:
                summary["missed_arrivals"] += 1
                Capture.append(output,{"id":run_id+":"+name+":"+str(index),"scheduled_ns":scheduled,"status":"generator_capacity_missed","pool":pool["address"],"countback":size})
                continue
            task = asyncio.create_task(request(index,scheduled,pool,window,cold))
            pending.add(task); task.add_done_callback(pending.discard)
        if pending: await asyncio.gather(*tuple(pending))
        if fault_task is not None: await fault_task
        sampler_stop.set(); await sampler
        if len(db_records) < duration-3: raise ValueError("database observations do not cover measurement window")
        written = db_records[-1]["committed_outbox"]-db_records[0]["committed_outbox"]
        measured_seconds = (db_records[-1]["monotonic_ns"]-db_records[0]["monotonic_ns"])/1e9
        if measured_seconds <= 0 or written/measured_seconds < config["minimum_durable_writes_per_second"]: raise ValueError("actual committed chart aggregation throughput below budget")
        if name == "redis-outage":
            during = [record for record in db_records if outage_started is not None and record["monotonic_ns"] >= outage_started]
            if len(during) < duration//3: raise ValueError("Redis outage not observed long enough during load")
            outage_seconds = (during[-1]["monotonic_ns"]-during[0]["monotonic_ns"])/1e9
            if outage_seconds <= 0 or (during[-1]["committed_outbox"]-during[0]["committed_outbox"])/outage_seconds < config["minimum_durable_writes_per_second"]: raise ValueError("Redis loss starved durable chart aggregation")
        summary["durable_committed_outbox"] = written
        summary["durable_writes_per_second"] = written/measured_seconds
        summary["max_db_connections"] = max(r["numbackends"] for r in db_records)
        summary["max_write_lag_seconds"] = max(r["write_lag_seconds"] for r in db_records)
        driver_code = await asyncio.wait_for(driver.wait(),15)
        if driver_code != 0: raise ValueError("common dynamic workload driver failed")
        companion = json.loads((capture.path/(name+"-common.json")).read_bytes(),object_pairs_hook=unique)
        summary["common_observed_categories"] = validate_common_capture(companion,common,name,rate,duration,config,run_id,utc_start)
        summary.update(origin_p95_ms=percentile(origins,.95),origin_p99_ms=percentile(origins,.99),client_p95_ms=percentile(latencies,.95),client_p99_ms=percentile(latencies,.99),scheduler_p99_ms=percentile(delays,.99),origin_samples=len(origins),cache_collapse_efficiency=(summary["cache_coalesced"]/(summary["cache_miss"]+summary["cache_coalesced"]) if summary["cache_miss"]+summary["cache_coalesced"] else 0))
        capture.json(name+"-summary.json",summary)
        if summary["failed"] or summary["missed_arrivals"] or (name != "redis-outage" and summary["shed"]): raise ValueError("failed, missed or healthy-phase shed arrivals remain in denominator")
        if (summary["cache_miss"]+summary["cache_coalesced"])*5 < summary["success"] or summary["hot_pool"]*10 != summary["scheduled"]:
            raise ValueError("observed cache miss/hot pool fractions do not meet launch mix")
        if name != "redis-outage" and (summary["origin_p95_ms"]>150 or summary["origin_p99_ms"]>400):
            raise ValueError("actual origin p95/p99 exceeds strict150/400ms")
        return summary
    finally:
        if fault_task is not None and not fault_task.done():
            fault_task.cancel(); await asyncio.gather(fault_task,return_exceptions=True)
        sampler_stop.set()
        for task in tuple(pending): task.cancel()
        if pending: await asyncio.gather(*tuple(pending),return_exceptions=True)
        if not sampler.done(): sampler.cancel()
        await asyncio.gather(sampler,return_exceptions=True)
        if driver.returncode is None:
            os.killpg(driver.pid,signal.SIGTERM)
            try: await asyncio.wait_for(driver.wait(),5)
            except asyncio.TimeoutError:
                os.killpg(driver.pid,signal.SIGKILL); await driver.wait()
        driver_log.close()
        if name == "redis-outage" and fault_done:
            restore = await provider_command(controls["redis_restore"],environment,capture.path,"redis-restore.log")
            await redis_client.ping()
            restore["redis_available_observed_monotonic_ns"] = time.monotonic_ns()
            capture.json("redis-restore.json",restore)


async def run(config,output,shared_soak=None):
    import aiohttp
    import asyncpg
    import redis.asyncio as redis_async
    topology,dataset,common,controls,deployment = validate_config(config)
    dsn = os.environ.get(config["postgres_dsn_env"])
    redis_url = os.environ.get(config["redis_url_env"])
    if not dsn or not redis_url: raise ValueError("actual PostgreSQL credential authority unavailable")
    capture = Capture(output,config)
    run_id = str(uuid.uuid4())
    failure = None
    db = None
    redis_client = redis_async.from_url(redis_url,socket_timeout=2,socket_connect_timeout=2,retry_on_timeout=False)
    try:
        await redis_client.ping()
        db = await asyncpg.connect(dsn,timeout=5,command_timeout=10,server_settings={"default_transaction_read_only":"on","application_name":"l11-phase4-measurement"})
        actual_db = dict(await db.fetchrow("SELECT current_database() AS database,inet_server_addr()::text AS address,inet_server_port() AS port",timeout=2))
        if actual_db != topology["postgres_identity"]: raise ValueError("actual PostgreSQL target differs from reviewed topology")
        redis_identity = urlsplit(redis_url)
        if {"host":redis_identity.hostname,"port":redis_identity.port,"database":redis_identity.path} != topology["redis_identity"]: raise ValueError("Redis target differs from reviewed topology")
        await capture_plans(db,topology,dataset,capture)
        connector = aiohttp.TCPConnector(limit=config["max_inflight"],ssl=ssl.create_default_context())
        async with aiohttp.ClientSession(connector=connector,timeout=aiohttp.ClientTimeout(total=4)) as session:
            assets = []
            for asset in deployment["assets"]:
                async with session.get(topology["frontend_origin"]+asset["url_path"],allow_redirects=False) as response:
                    raw = await read_response(response,64*1024*1024)
                    if response.status != 200 or not raw or len(raw)>64*1024*1024 or hashlib.sha256(raw).hexdigest() != asset["sha256"]:
                        raise ValueError("source and actual deployed chart asset hashes differ")
                    assets.append({"url_path":asset["url_path"],"sha256":asset["sha256"],"actual_bytes":len(raw)})
            capture.json("deployed-assets.json",assets)
            results = {}
            for name,rate in (("steady",2000),("surge",4000),("redis-outage",2000)):
                results[name] = await measure_phase(session,db,redis_client,config,topology,dataset,common,controls,capture,name,rate,run_id)
            capture.json("measurement-manifest.json",{"schema_version":1,"run_id":run_id,"candidate_source_sha256":config["candidate_source_sha256"],"interface_sha256":config["interface_sha256"],"dataset_sha256":config["dataset"]["sha256"],"topology_sha256":config["topology"]["sha256"],"deployment_sha256":config["deployment"]["sha256"],"license_manifest_sha256":config["license_manifest"]["sha256"],"common_workload_sha256":config["common_workload"]["sha256"],"harness_sha256":sha(__file__),"results":results,"final_capacity_qualified":False,"shared_24hour_soak_task":"9.1","shared_24hour_soak_evidence":validate_shared_soak(shared_soak,config) if shared_soak is not None else None})
    except BaseException as error:
        failure = error
        capture.json("failure.json",{"run_id":run_id,"error_class":type(error).__name__,"observed_at_utc_ns":time.time_ns(),"final_capacity_qualified":False})
    finally:
        if db is not None: await db.close(timeout=5)
        await redis_client.aclose()
        capture.close()
    if failure is not None: raise failure


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--config",required=True)
    parser.add_argument("--config-sha256",required=True)
    parser.add_argument("--output",required=True)
    parser.add_argument("--shared-soak")
    parser.add_argument("--shared-soak-sha256")
    args = parser.parse_args()
    config = document({"path":args.config,"sha256":args.config_sha256},4*1024*1024)
    if bool(args.shared_soak) != bool(args.shared_soak_sha256): raise ValueError("both shared-soak path and digest required")
    soak = {"path":args.shared_soak,"sha256":args.shared_soak_sha256} if args.shared_soak else None
    asyncio.run(run(config,args.output,soak))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        print(type(error).__name__+": "+str(error),file=sys.stderr)
        sys.exit(1)
