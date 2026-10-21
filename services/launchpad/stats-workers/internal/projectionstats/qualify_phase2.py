from __future__ import annotations

import argparse
import asyncio
import concurrent.futures
import hashlib
import json
import math
import os
from pathlib import Path
import re
import signal
import sqlite3
import ssl
import subprocess
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[5]
HEX = re.compile(r"[0-9a-f]{64}\Z")
ADDRESS = re.compile(r"0x[0-9a-f]{40}\Z")
TOKEN = re.compile(r"[A-Za-z0-9_.:-]{1,128}\Z")
IDENT = re.compile(r"[a-z][a-z0-9_]{0,62}\Z")
UINT = re.compile(r"(?:0|[1-9][0-9]*)\Z")


def require(condition, message):
    if not condition:
        raise ValueError(message)


def unique(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate JSON field")
        result[key] = value
    return result


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False).encode()


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def load(raw):
    return json.loads(raw, object_pairs_hook=unique, parse_constant=lambda _: (_ for _ in ()).throw(ValueError("nonfinite JSON")))


def file_bound(path, digest, maximum=4*1024*1024):
    path = Path(path)
    require(path.is_absolute() and not path.is_symlink() and path.is_file() and HEX.fullmatch(digest) and path.stat().st_size <= maximum, "bounded digest-bound file required")
    raw = path.read_bytes()
    require(sha(raw) == digest, "file source digest mismatch")
    return raw


def number(value, maximum_digits=78):
    require(isinstance(value, str) and len(value) <= maximum_digits and UINT.fullmatch(value), "canonical decimal integer required")
    parsed = int(value)
    if maximum_digits == 78:
        require(parsed < 2**256, "uint256 overflow")
    return parsed


def percentile(values, total, quantile):
    require(total > 0 and len(values) <= total, "complete scheduled denominator required")
    index = math.ceil(total*quantile)-1
    ordered = sorted(values)
    return ordered[index] if index < len(ordered) else None


class Artifacts:
    def __init__(self, directory):
        self.path = Path(directory).resolve()
        require(self.path.is_relative_to((ROOT/"launchpad/evidence/production-system/L13/phase4").resolve()), "L13 phase4 evidence directory required")
        self.path.mkdir(parents=True, exist_ok=False)
        self.lock = threading.Lock()
        self.opened = {}

    def row(self, name, value):
        require(re.fullmatch(r"[a-z0-9-]+\.ndjson",name), "bounded artifact name")
        with self.lock:
            if name not in self.opened:
                self.opened[name] = (self.path/name).open("x",encoding="utf-8")
            self.opened[name].write(canonical(value).decode()+"\n")

    def document(self, name, value):
        with (self.path/name).open("x",encoding="utf-8") as output:
            output.write(canonical(value).decode()+"\n")
            output.flush()
            os.fsync(output.fileno())

    def close(self):
        for output in self.opened.values():
            output.flush()
            os.fsync(output.fileno())
            output.close()
        self.opened.clear()

    def hashes(self):
        result = {}
        for path in sorted(self.path.iterdir()):
            if path.is_file() and path.name != "artifact-sha256.json":
                hasher = hashlib.sha256()
                with path.open("rb") as source:
                    for block in iter(lambda: source.read(1048576), b""):
                        hasher.update(block)
                result[path.name] = hasher.hexdigest()
        return result


class HTTP:
    def __init__(self, tls):
        self.context = ssl.create_default_context(cafile=tls["ca_file"])
        self.context.minimum_version = ssl.TLSVersion.TLSv1_3
        if tls.get("cert_file") or tls.get("key_file"):
            require(tls.get("cert_file") and tls.get("key_file"), "complete mTLS identity required")
            self.context.load_cert_chain(tls["cert_file"],tls["key_file"])
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, *_):
                return None
        self.opener = urllib.request.build_opener(NoRedirect(),urllib.request.HTTPSHandler(context=self.context))

    def get(self, url, maximum=4*1024*1024):
        parsed = urllib.parse.urlsplit(url)
        require(parsed.scheme == "https" and parsed.hostname and not parsed.username and not parsed.password and not parsed.fragment, "actual exact HTTPS endpoint required")
        request = urllib.request.Request(url, headers={"Cache-Control":"no-cache", "Accept":"application/json"})
        try:
            response = self.opener.open(request,timeout=2)
        except urllib.error.HTTPError as err:
            response = err
        with response:
            body = response.read(maximum+1)
            require(len(body)<=maximum, "HTTP response byte bound")
            return response.status, dict(response.headers), body


class Postgres:
    def __init__(self, config):
        self.argv = [config["executable"],"--no-psqlrc","--quiet","--no-align","--tuples-only","--set=ON_ERROR_STOP=1"]
        file_bound(config["executable"],config["sha256"],128*1024*1024)
        allowed = {"PGHOST","PGPORT","PGUSER","PGDATABASE","PGPASSWORD","PGSSLMODE","PGSSLROOTCERT","PGSERVICE","PGSERVICEFILE","PGPASSFILE","PGCONNECT_TIMEOUT","PATH"}
        require(config["environment"] and set(config["environment"])<=allowed, "explicit PostgreSQL environment names required")
        self.env = {key:os.environ[value] for key,value in config["environment"].items()}
        require(self.env.get("PGSSLMODE") in ("verify-full", "verify-ca") and self.env.get("PGCONNECT_TIMEOUT") in ("1","2","3","4","5"), "authenticated PostgreSQL TLS and bounded connection timeout required")

    def rows(self,sql):
        sql = "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;SET LOCAL statement_timeout='30s';"+sql+";COMMIT;"
        with subprocess.Popen(self.argv+["--command",sql],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,start_new_session=True) as process:
            try:
                for raw in iter(process.stdout.readline,b""):
                    require(len(raw)<=2*1024*1024, "SQL row byte bound")
                    yield load(raw)
                require(process.wait(timeout=35)==0, "actual PostgreSQL observation failed")
            finally:
                if process.poll() is None:
                    os.killpg(process.pid,signal.SIGKILL)
                    process.wait()

    def one(self,sql):
        rows = list(self.rows(sql))
        require(len(rows)==1,"one actual SQL observation required")
        return rows[0]


class Harness:
    def __init__(self, config, artifacts):
        self.c,self.a = config,artifacts
        require(config["version"]=="l13-phase4-v1" and config["chain_id"]==125 and config["target_dau"]==500000, "frozen lane envelope required")
        for key in ("candidate_sha256","interface_sha256","topology_sha256"):
            require(HEX.fullmatch(config[key]),"exact candidate/interface/topology bindings required")
        require(sha(file_bound(config["interface_file"],config["interface_sha256"]))==config["interface_sha256"],"prior serial interface required")
        topology = load(file_bound(config["topology_file"],config["topology_sha256"]))
        require(topology["environment"]=="qualification" and topology["chain_id"]==125 and topology["source_write_admitted"] is True, "explicit isolated source mutation admission required")
        manifest = load(file_bound(config["source_manifest"],config["candidate_sha256"]))
        require(manifest["files"] and isinstance(manifest["files"],dict), "candidate source manifest required")
        for name,digest in manifest["files"].items():
            path=(ROOT/name).resolve()
            require(path.is_relative_to(ROOT) and not Path(ROOT/name).is_symlink(),"candidate source escapes checkout")
            file_bound(str(path),digest,128*1024*1024)
        own = str(Path(__file__).resolve().relative_to(ROOT))
        require(own in manifest["files"],"measurement source not candidate-bound")
        for key in ("source_schema","stats_schema"):
            require(IDENT.fullmatch(config[key]),"actual schema identity required")
        require(TOKEN.fullmatch(config["source_stream"]) and TOKEN.fullmatch(config["projection_version"]),"actual stream and projection identities required")
        require(config["pools"] and len(config["pools"])<=4096 and len(set(config["pools"]))==len(config["pools"]) and all(ADDRESS.fullmatch(p) for p in config["pools"]) and config["hot_pool"] in config["pools"],"actual admitted pool inventory required")
        require(config["source_steady_eps"]==2000 and config["source_burst_eps"]==10000 and config["burst_seconds"]==300 and config["steady_seconds"]>=60 and config["daily_event_target"]==10000000,"frozen source demand required")
        require(type(config["origin_rps"]) is int and 1<=config["origin_rps"]<=10000 and type(config["global_read_share_bp"]) is int and 1<=config["global_read_share_bp"]<=10000 and config["origin_rps"]*10000==10000*config["global_read_share_bp"],"explicit global10000RPS share required")
        require(type(config["max_inflight"]) is int and 1<=config["max_inflight"]<=4096 and type(config["queue_bound"]) is int and config["queue_bound"]>0 and 1<=config["recovery_seconds"]<=600,"bounded executor, queue and recovery required")
        origin=urllib.parse.urlsplit(config["origin"])
        require(origin.scheme=="https" and origin.hostname and origin.path in ("","/") and not origin.username and not origin.query and not origin.fragment,"direct origin identity required")
        require(topology["stats_origin"]==config["origin"] and topology["pools"]==config["pools"],"origin/pool topology binding mismatch")
        self.http=HTTP(config["tls"])
        self.pg=Postgres(config["postgres"])
        self.source=config["source_schema"]
        self.stats=config["stats_schema"]
        self.version=config["projection_version"]
        self.stream=config["source_stream"]
        self.pools=",".join("'"+p+"'" for p in config["pools"])
        self.initial_height=None
        self.worker_pool=concurrent.futures.ThreadPoolExecutor(max_workers=config["max_inflight"])
        self.bound_command(config["generator"])
        self.bound_command(config["broker_observer"])
        require(config["hosts"],"actual resource exporters required")
        for host in config["hosts"]:
            require(host["id"] in topology["hosts"] and host["cpu_count"]>0 and host["nic_bytes_per_second"]>0 and host["role"] in ("generator","stats","postgres","broker"),"source-bound host capacity required")
            require(topology["resources"][host["id"]]=={key:host[key] for key in ("cpu_count","nic_bytes_per_second","rss_limit_bytes","heap_limit_bytes","role")},"host limits differ from admitted topology")
            require(set(host["selectors"])=={"cpu_seconds","rss_bytes","heap_bytes","gc_pause_seconds","nic_receive_bytes","nic_send_bytes"},"complete native resource selectors required")
            for selector in host["selectors"].values():
                require(selector["source_path"] in manifest["files"] and selector["source_sha256"]==manifest["files"][selector["source_path"]],"native exporter selector source binding required")
        require(any(host["role"]=="generator" for host in config["hosts"]),"generator native resource evidence required")
        self.a.document("candidate-bindings.json",{key:config[key] for key in ("candidate_sha256","interface_sha256","topology_sha256","target_dau","global_read_share_bp","origin_rps","daily_event_target")})

    def bound_command(self, command):
        require(command["argv"] and all(isinstance(a,str) and a and len(a)<4096 for a in command["argv"]),"bounded exact provider command required")
        file_bound(command["argv"][0],command["executable_sha256"],128*1024*1024)
        require(command["files"],"provider source bindings required")
        for name,digest in command["files"].items():
            file_bound(name,digest,128*1024*1024)
        require(all(TOKEN.fullmatch(k) and TOKEN.fullmatch(v) for k,v in command["environment"].items()),"private provider environment names required")

    def command(self,command,stage=None,rate=None,duration=None):
        env={k:os.environ[v] for k,v in command["environment"].items()}
        if stage is not None:
            env.update(L13_PHASE4_STAGE=stage,L13_PHASE4_SOURCE_EPS=str(rate),L13_PHASE4_DURATION_SECONDS=str(duration),L13_PHASE4_HOT_POOL=self.c["hot_pool"],L13_PHASE4_HOT_POOL_BP="8000")
        return subprocess.Popen(command["argv"],env=env,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,start_new_session=True)

    def state(self):
        sql=f"""SELECT json_build_object('database_time_ns',(extract(epoch FROM clock_timestamp())*1000000000)::numeric(30,0)::text,'source_count',(SELECT count(*)::text FROM {self.source}.indexer_accounting_fills_v1 WHERE pool_address IN ({self.pools}) AND chain_id=125),'projected_count',(SELECT count(*)::text FROM {self.stats}.accounting_market_commits WHERE projection_version='{self.version}' AND pool IN ({self.pools})),'raw_height',w.raw_complete_height::text,'typed_height',w.typed_complete_height::text,'lease_alive',l.expires_at>clock_timestamp(),'pending_outbox',(SELECT count(*) FROM {self.stats}.outbox WHERE NOT published AND projection_version='{self.version}'),'locks_waiting',(SELECT count(*) FROM pg_locks WHERE NOT granted),'blocked_backends',(SELECT count(*) FROM pg_stat_activity WHERE wait_event_type='Lock'),'database', (SELECT row_to_json(d) FROM pg_stat_database d WHERE datname=current_database()),'commit_timestamps',current_setting('track_commit_timestamp')) FROM {self.source}.indexer_ingest_watermarks_v1 w JOIN {self.source}.indexer_ingest_leases_v1 l USING(chain_id,stream_id) WHERE w.chain_id=125 AND w.stream_id='{self.stream}'"""
        row=self.pg.one(sql)
        require(row["lease_alive"] is True and row["raw_height"]==row["typed_height"] and row["commit_timestamps"]=="on","actual complete canonical lease and transaction commit timestamps required")
        require(row["pending_outbox"]<=self.c["queue_bound"],"durable outbox exceeds admitted queue bound")
        self.a.row("sql-telemetry.ndjson",row)
        return row

    def broker(self):
        process=self.command(self.c["broker_observer"])
        try:
            raw,_=process.communicate(timeout=5)
            require(process.returncode==0 and len(raw)<=4*1024*1024,"real broker observer failed")
            value=load(raw)
            require(value["config"]["num_replicas"]==3 and value["config"]["storage"]=="file" and len(value["cluster"]["replicas"])==2,"actual R3 file stream required")
            require(all(r["current"] is True and r.get("offline",False) is False for r in value["cluster"]["replicas"]),"JetStream replicas not current")
            self.a.row("broker-telemetry.ndjson",{"observed_utc_ns":time.time_ns(),"native":value})
            return value
        finally:
            if process.poll() is None:
                os.killpg(process.pid,signal.SIGKILL);process.wait()

    def resources(self):
        result=[]
        for host in self.c["hosts"]:
            status,_,raw=self.http.get(host["url"],8*1024*1024)
            require(status==200,"actual native exporter unavailable")
            native={}
            for line in raw.decode().splitlines():
                if not line or line.startswith("#"):continue
                parts=line.rsplit(" ",1)
                require(len(parts)==2,"native exporter wire malformed")
                if parts[0] in native:raise ValueError("ambiguous native exporter series")
                native[parts[0]]=parts[1]
            row={"host":host["id"],"observed_monotonic_ns":time.monotonic_ns(),"observed_utc_ns":time.time_ns(),"exporter_sha256":sha(raw),"native_text":raw.decode(),"values":{}}
            for key,selector in host["selectors"].items():
                require(selector["series"] in native,"missing native metric selector")
                value=float(native[selector["series"]])
                require(math.isfinite(value) and value>=0,"invalid native resource value")
                row["values"][key]=value
            self.a.row("resource-telemetry.ndjson",row)
            result.append(row)
        return result

    async def arrivals(self,stage,seconds):
        rate=self.c["origin_rps"]
        total=rate*seconds
        start=time.monotonic_ns()+10000000
        active=set()
        completed=[]
        delays=[]
        status_counts={"ok":0,"error":0,"timeout":0,"missed":0}
        loop=asyncio.get_running_loop()
        async def request(index,scheduled,pool):
            attempted=time.monotonic_ns()
            row={"stage":stage,"id":index,"pool":pool,"scheduled_monotonic_ns":scheduled,"attempted_monotonic_ns":attempted,"status":"error"}
            delays.append(attempted-scheduled)
            try:
                path="/v2/stats/platform?windowSeconds=86400" if pool=="" else "/v2/stats/"+pool+"?windowSeconds=86400"
                status,headers,body=await loop.run_in_executor(self.worker_pool,self.http.get,self.c["origin"].rstrip("/")+path)
                row["http_status"]=status
                row["body_sha256"]=sha(body)
                require(status==200 and headers.get("Age","0")=="0","origin request failed or returned cache age")
                response=load(body)
                require(response["stale"] is False and response["watermark"]["chainId"]=="125" and response["watermark"]["sourceStream"]==self.stream and response["watermark"]["projectionVersion"]==self.version,"unknown/stale/coherence disclosure under normal load")
                row["watermark"]=response["watermark"]
                row["status"]="ok"
            except TimeoutError:
                row["status"]="timeout"
            except Exception as err:
                row["error_class"]=type(err).__name__
            finally:
                row["completed_monotonic_ns"]=time.monotonic_ns()
                completed.append(row["completed_monotonic_ns"]-scheduled)
                status_counts[row["status"]]+=1
                self.a.row("http-samples.ndjson",row)
        try:
            for index in range(total):
                scheduled=start+index*1000000000//rate
                await asyncio.sleep(max(0,(scheduled-time.monotonic_ns())/1e9))
                if len(active)>=self.c["max_inflight"]:
                    status_counts["missed"]+=1
                    self.a.row("http-samples.ndjson",{"stage":stage,"id":index,"scheduled_monotonic_ns":scheduled,"status":"missed","error_class":"inflight_bound"})
                    continue
                pool=self.c["hot_pool"] if index%10<8 else ("" if index%10==8 else self.c["pools"][index%len(self.c["pools"])])
                task=asyncio.create_task(request(index,scheduled,pool));active.add(task);task.add_done_callback(active.discard)
            if active:await asyncio.gather(*tuple(active))
        finally:
            for task in active:task.cancel()
            if active:await asyncio.gather(*tuple(active),return_exceptions=True)
        result={"stage":stage,"scheduled":total,"counts":status_counts,"latency_denominator":total,"latency_observed":len(completed),"origin_p95_ns":percentile(completed,total,.95),"origin_p99_ns":percentile(completed,total,.99),"scheduler_p99_ns":percentile(delays,total,.99)}
        require(sum(status_counts.values())==total,"scheduled arrivals disappeared")
        self.a.row("latency-summary.ndjson",result)
        require(result["origin_p95_ns"] is not None and result["origin_p95_ns"]<=150000000 and result["origin_p99_ns"] is not None and result["origin_p99_ns"]<=400000000,"origin p95/p99 target failed or censored")
        require(status_counts["ok"]==total and result["scheduler_p99_ns"] is not None and result["scheduler_p99_ns"]<=10000000,"errors/missed work or saturated generator")
        return result

    def reconcile(self):
        status,headers,body=self.http.get(self.c["origin"].rstrip("/")+"/v2/stats/platform?windowSeconds=86400")
        require(status==200 and headers.get("Age","0")=="0","actual origin snapshot required")
        response=load(body)
        watermark=response["watermark"]
        height=number(watermark["height"])
        require(watermark["chainId"]=="125" and watermark["sourceStream"]==self.stream and watermark["projectionVersion"]==self.version and response["stale"] is False,"canonical snapshot identity mismatch")
        require(type(response["windowStart"]) is int and type(response["windowEnd"]) is int and response["windowEnd"]-response["windowStart"]==86400 and response["computedAt"]==response["windowEnd"],"canonical window geometry")
        block=self.pg.one(f"SELECT json_build_object('hash',block_hash,'complete',fetch_complete AND required_decode_complete) FROM {self.source}.indexer_block_completeness_v1 WHERE chain_id=125 AND stream_id='{self.stream}' AND height={height}")
        require(block["hash"]==watermark["blockHash"] and block["complete"] is True,"watermark block completeness mismatch")
        sqlite_path=self.a.path/"reference.sqlite"
        database=sqlite3.connect(sqlite_path)
        database.execute("CREATE TABLE actors(address TEXT PRIMARY KEY)")
        database.execute("CREATE TABLE identities(event_id TEXT PRIMARY KEY, position TEXT UNIQUE)")
        fees={}
        volume=0
        fills=0
        optical_sides=set()
        after=""
        try:
            while True:
                where=f"f.chain_id=125 AND (f.payload->'position'->>'height')::numeric<={height} AND (f.payload->>'blockTimestampSeconds')::bigint>{response['windowStart']} AND (f.payload->>'blockTimestampSeconds')::bigint<={response['windowEnd']} AND f.source_fill_id>'{after}'"
                sql=f"""SELECT json_build_object('source_id',f.source_fill_id,'source_digest',f.source_fact_digest,'fill',f.payload,'projection',row_to_json(t),'committed_fill',c.fill,'committed_digest',c.source_fact_digest) FROM {self.source}.indexer_accounting_fills_v1 f LEFT JOIN {self.stats}.accounting_market_commits c ON c.source_fill_id=f.source_fill_id AND c.projection_version='{self.version}' LEFT JOIN {self.stats}.trade_facts t ON t.projection_version=c.projection_version AND t.event_id='125:'||(f.payload->'position'->>'transactionHash')||':'||(f.payload->'position'->>'logIndex') WHERE {where} ORDER BY f.source_fill_id LIMIT 5000"""
                page=0
                for row in self.pg.rows(sql):
                    page+=1
                    f=row["fill"]
                    require(f["version"]=="accounting-fill-v1" and row["source_digest"]==row["committed_digest"] and f==row["committed_fill"] and row["projection"] is not None,"missing or mismatched canonical persisted effect")
                    require(TOKEN.fullmatch(row["source_id"]) and f["sourceFillId"]==row["source_id"],"source identity mismatch")
                    p=f["position"]
                    require(p["chainId"]=="125" and all(re.fullmatch(r"0x[0-9a-f]{64}",p[k]) for k in ("transactionHash","blockHash")),"canonical provenance required")
                    pos=[number(p[k]) for k in ("height","transactionIndex","logIndex")]
                    event="125:"+p["transactionHash"]+":"+p["logIndex"]
                    database.execute("INSERT INTO identities VALUES(?,?)",(event,canonical(pos).decode()))
                    require(ADDRESS.fullmatch(f["attribution"]["transactionSender"]),"actual sender actor required")
                    database.execute("INSERT OR IGNORE INTO actors VALUES(?)",(f["attribution"]["transactionSender"],))
                    paid=number(f["paidInputRaw"])
                    fee=number(f["feeRaw"])
                    usdl=number(f["usdlAmountRaw"])
                    token=number(f["tokenAmountRaw"])
                    require(f["side"] in ("buy","sell"),"canonical side required")
                    asset=f["settlement"] if f["side"]=="buy" else f["token"]
                    require(ADDRESS.fullmatch(asset["address"]) and type(asset["decimals"]) is int and 0<=asset["decimals"]<=255,"actual fee asset required")
                    disposition="distributed_usdl" if f["side"]=="buy" else "retained_token_reserve"
                    require(f["feeDisposition"]==disposition and paid==(usdl if f["side"]=="buy" else token),"paid-input or fee asset semantics mismatch")
                    reserve=paid-fee if f["side"]=="buy" else paid
                    require(reserve>=0,"negative canonical reserve")
                    if f["pricedInputRaw"] is not None:
                        priced=number(f["pricedInputRaw"])
                        require(f["inputDerivationQuality"]=="exact" and number(f["ammInputAfterFeeRaw"])==priced-fee,"unproven priced input or net arithmetic")
                        execution=f["execution"]
                        require(type(execution["feeBps"]) is int and 0<=execution["feeBps"]<=10000 and HEX.fullmatch(execution["replayEvidenceDigest"]),"rate replay authority required for fee reference")
                        require(fee==priced*execution["feeBps"]//10000,"floor fee mismatch")
                        if priced<paid:optical_sides.add(f["side"])
                    else:
                        require(f["ammInputAfterFeeRaw"] is None and f["inputDerivationQuality"] in ("receipt_incomplete","legacy_unavailable","abi_unqualified"),"unknown optical adjustment was invented")
                    trade=row["projection"]
                    require(number(str(trade["volume"]))==usdl and number(str(trade["fee"]))==fee and trade["fee_asset"]==asset["address"] and trade["fee_decimals"]==asset["decimals"] and trade["disposition"]==disposition and trade["pool"]==f["poolAddress"],"persisted trade differs from independent arithmetic")
                    require([number(str(trade[k])) for k in ("height","transaction_index","log_index")]==pos and trade["block_timestamp"]==int(f["blockTimestampSeconds"]),"event-time position mismatch")
                    key=(asset["address"],asset["decimals"],disposition)
                    fees[key]=fees.get(key,0)+fee
                    volume+=usdl
                    fills+=1
                    after=row["source_id"]
                    self.a.row("canonical-reference.ndjson",{"actual":row,"expected":{"event_id":event,"volume_usdl_raw":str(usdl),"input_reserve_increment_raw":str(reserve),"fee_raw":str(fee),"fee_asset":asset,"fee_disposition":disposition}})
                database.commit()
                if page<5000:break
            actors=database.execute("SELECT count(*) FROM actors").fetchone()[0]
            actual_fees={}
            for group in response["tradingFees"]:
                key=(group["assetAddress"],group["assetDecimals"],group["disposition"])
                require(key not in actual_fees and group["chainId"]=="125" and group["feeType"]=="trading_fee" and group["quoteValue"] is None and group["quoteUnavailableReason"],"cross-asset scalar or fabricated valuation")
                actual_fees[key]=number(group["raw"],156)
            require(number(response["venueVolumeUsdlRaw"],156)==volume and number(response["poolFillCount"],156)==fills and number(response["uniqueActors"],156)==actors and actual_fees==fees,"API/source/persistence integer reconciliation mismatch")
            require(response["traderPopulation"]=="sender_actor" and response["userActionCount"] is None and response["userActionUnavailableReason"],"unproven routed user actions invented")
            require(optical_sides=={"buy","sell"},"real negative-optical buy/sell evidence missing")
            self.a.document("integer-reconciliation.json",{"watermark":watermark,"window_start":response["windowStart"],"window_end":response["windowEnd"],"expected_volume_usdl_raw":str(volume),"expected_pool_fills":str(fills),"expected_unique_actors":str(actors),"expected_fee_groups":[{"asset":k[0],"decimals":k[1],"disposition":k[2],"raw":str(v)} for k,v in sorted(fees.items())],"actual_origin_response":response,"actual_origin_sha256":sha(body),"matched":True})
        finally:
            database.close()

    def lag(self):
        sql=f"""SELECT json_build_object('unprojected',count(*),'oldest_commit_age_ms',max(extract(epoch FROM(clock_timestamp()-pg_xact_commit_timestamp(c.xmin)))*1000),'missing_commit_timestamps',count(*) FILTER(WHERE pg_xact_commit_timestamp(c.xmin) IS NULL)) FROM {self.source}.indexer_accounting_fills_v1 f JOIN {self.source}.indexer_raw_logs_v1 r USING(chain_id,transaction_hash,log_index) JOIN {self.source}.indexer_ingest_commits c ON c.chain_id=r.chain_id AND c.stream_id='{self.stream}' AND c.previous_height<r.height AND c.last_complete_height>=r.height WHERE f.pool_address IN ({self.pools}) AND NOT EXISTS(SELECT 1 FROM {self.stats}.accounting_market_commits p WHERE p.projection_version='{self.version}' AND p.source_fill_id=f.source_fill_id AND p.source_fact_digest=f.source_fact_digest)"""
        row=self.pg.one(sql)
        require(row["missing_commit_timestamps"]==0,"actual durable-commit latency authority unavailable")
        require(self.initial_height is not None,"projection observation start watermark required")
        completed_sql=f"""SELECT json_build_object('observed_effects',count(*),'missing_commit_timestamps',count(*) FILTER(WHERE pg_xact_commit_timestamp(p.xmin) IS NULL OR pg_xact_commit_timestamp(c.xmin) IS NULL),'negative_latencies',count(*) FILTER(WHERE pg_xact_commit_timestamp(p.xmin)<pg_xact_commit_timestamp(c.xmin)),'projection_p95_ms',percentile_cont(0.95) WITHIN GROUP(ORDER BY extract(epoch FROM(pg_xact_commit_timestamp(p.xmin)-pg_xact_commit_timestamp(c.xmin)))*1000),'projection_p99_ms',percentile_cont(0.99) WITHIN GROUP(ORDER BY extract(epoch FROM(pg_xact_commit_timestamp(p.xmin)-pg_xact_commit_timestamp(c.xmin)))*1000)) FROM {self.source}.indexer_accounting_fills_v1 f JOIN {self.source}.indexer_raw_logs_v1 r USING(chain_id,transaction_hash,log_index) JOIN {self.source}.indexer_ingest_commits c ON c.chain_id=r.chain_id AND c.stream_id='{self.stream}' AND c.previous_height<r.height AND c.last_complete_height>=r.height JOIN {self.stats}.accounting_market_commits p ON p.source_fill_id=f.source_fill_id AND p.source_fact_digest=f.source_fact_digest AND p.projection_version='{self.version}' WHERE f.pool_address IN ({self.pools}) AND r.height>{self.initial_height}"""
        effects=self.pg.one(completed_sql)
        require(effects["missing_commit_timestamps"]==0 and effects["negative_latencies"]==0,"projection commit latency authority missing or inconsistent")
        self.a.row("projection-lag.ndjson",{"observed_utc_ns":time.time_ns(),"unprojected":row,"committed_effects":effects,"latency_authority":"same_postgresql_transaction_commit_timestamps"})
        return row

    async def stage(self,name,rate,seconds):
        before=await asyncio.to_thread(self.state)
        resource_before=await asyncio.to_thread(self.resources)
        source=self.command(self.c["generator"],name,rate,seconds)
        drain=asyncio.create_task(asyncio.to_thread(self.drain_generator,source))
        start=time.monotonic_ns()
        arrivals=asyncio.create_task(self.arrivals(name,seconds))
        try:
            while time.monotonic_ns()-start<seconds*1000000000:
                await asyncio.gather(asyncio.to_thread(self.state),asyncio.to_thread(self.lag),asyncio.to_thread(self.broker))
                current=await asyncio.to_thread(self.resources)
                for previous,latest,host in zip(resource_before,current,self.c["hosts"]):
                    elapsed=(latest["observed_monotonic_ns"]-previous["observed_monotonic_ns"])/1e9
                    require(elapsed>0,"native resource clock regression")
                    values=latest["values"]
                    old=previous["values"]
                    for counter in ("cpu_seconds","gc_pause_seconds","nic_receive_bytes","nic_send_bytes"):
                        require(values[counter]>=old[counter],"native resource counter reset")
                    cpu=(values["cpu_seconds"]-old["cpu_seconds"])/elapsed/host["cpu_count"]
                    nic=(values["nic_receive_bytes"]+values["nic_send_bytes"]-old["nic_receive_bytes"]-old["nic_send_bytes"])/elapsed/host["nic_bytes_per_second"]
                    self.a.row("resource-utilization.ndjson",{"stage":name,"host":host["id"],"cpu_fraction":cpu,"nic_fraction":nic,"heap_bytes":values["heap_bytes"],"rss_bytes":values["rss_bytes"],"gc_pause_seconds_delta":values["gc_pause_seconds"]-old["gc_pause_seconds"]})
                    if host["role"]=="generator":require(cpu<=.7 and nic<=.7,"generator CPU/NIC saturation invalidates offered load")
                    require(values["rss_bytes"]<=host["rss_limit_bytes"] and values["heap_bytes"]<=host["heap_limit_bytes"],"native memory budget exceeded")
                resource_before=current
                await asyncio.sleep(1)
            await arrivals
            await asyncio.wait_for(drain,10)
            require(source.returncode==0,"actual source generator failed")
            deadline=time.monotonic()+self.c["recovery_seconds"]
            while True:
                after=await asyncio.to_thread(self.state)
                pending=await asyncio.to_thread(self.lag)
                if pending["unprojected"]==0 and after["pending_outbox"]==0:break
                require(time.monotonic()<deadline,"broker/projection backlog failed to recover")
                await asyncio.to_thread(self.broker)
                await asyncio.sleep(1)
            generated=int(after["source_count"])-int(before["source_count"])
            projected=int(after["projected_count"])-int(before["projected_count"])
            require(generated>=rate*seconds and projected==generated,"actual source demand missing or durable effects lost/duplicated")
            skew=self.pg.one(f"SELECT json_build_object('total',count(*),'hot',count(*) FILTER(WHERE pool_address='{self.c['hot_pool']}')) FROM {self.source}.indexer_accounting_fills_v1 WHERE chain_id=125 AND pool_address IN ({self.pools}) AND (payload->'position'->>'height')::numeric>{int(before['typed_height'])} AND (payload->'position'->>'height')::numeric<={int(after['typed_height'])}")
            require(skew["total"]==generated and skew["hot"]*10000>=skew["total"]*8000,"actual canonical popular-pool skew below80percent")
            self.a.row("stage-recovery.ndjson",{"stage":name,"target_eps":rate,"duration_seconds":seconds,"generated":generated,"projected":projected,"actual_skew":skew,"before":before,"after":after,"recovery_remaining":pending})
        finally:
            if not arrivals.done():arrivals.cancel()
            await asyncio.gather(arrivals,return_exceptions=True)
            if source.poll() is None:
                os.killpg(source.pid,signal.SIGTERM)
                try:await asyncio.wait_for(drain,2)
                except TimeoutError:os.killpg(source.pid,signal.SIGKILL);await drain

    def drain_generator(self,process):
        hasher=hashlib.sha256()
        count=0
        for block in iter(lambda:process.stdout.read(65536),b""):
            count+=len(block)
            require(count<=64*1024*1024,"source provider log budget exceeded")
            hasher.update(block)
        process.wait()
        self.a.row("source-provider-execution.ndjson",{"argv_sha256":sha(canonical(self.c["generator"]["argv"])),"stdout_sha256":hasher.hexdigest(),"stdout_bytes":count,"exit_code":process.returncode})

    async def run(self):
        baseline=await asyncio.to_thread(self.state)
        require(baseline["source_count"]==baseline["projected_count"] and baseline["pending_outbox"]==0,"clean coherent baseline with no unaccounted backlog required")
        self.initial_height=int(baseline["typed_height"])
        await asyncio.to_thread(self.broker)
        await self.stage("steady",2000,self.c["steady_seconds"])
        await self.stage("five-minute-burst",10000,300)
        await asyncio.to_thread(self.reconcile)
        self.a.document("result.json",{"task":"4.13","lane_harness_passed":True,"capacity_qualified":False,"final_shared_capacity_task":"9.1","candidate_sha256":self.c["candidate_sha256"],"daily_event_target":10000000,"daily_event_acceptance":"deferred_to_serial9.1","scope":"actual lane measurements and independent canonical arithmetic"})


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument("--config",required=True)
    parser.add_argument("--config-sha256",required=True)
    parser.add_argument("--output",required=True)
    mode=parser.add_mutually_exclusive_group()
    mode.add_argument("--validate-current-source",action="store_true")
    mode.add_argument("--reconcile-only",action="store_true")
    args=parser.parse_args()
    artifacts=None
    harness=None
    try:
        config=load(file_bound(str(Path(args.config).resolve()),args.config_sha256))
        artifacts=Artifacts(args.output)
        harness=Harness(config,artifacts)
        if args.validate_current_source or args.reconcile_only:
            harness.state()
            harness.broker()
            harness.resources()
            if args.reconcile_only:harness.reconcile()
            artifacts.document("result.json",{"task":"4.13","measurement_inputs_available":True,"lane_arithmetic_matched":args.reconcile_only,"capacity_qualified":False,"measurements_executed":False,"final_shared_capacity_task":"9.1"})
        else:
            asyncio.run(harness.run())
        return 0
    except Exception as err:
        if artifacts is not None and not (artifacts.path/"result.json").exists():
            artifacts.document("result.json",{"task":"4.13","lane_harness_passed":False,"capacity_qualified":False,"error_class":type(err).__name__,"error":str(err) if isinstance(err,ValueError) else "actual dependency or provider failure; credentials omitted"})
        return 1
    finally:
        if harness is not None:harness.worker_pool.shutdown(wait=True,cancel_futures=True)
        if artifacts is not None:
            artifacts.close()
            artifacts.document("artifact-sha256.json",artifacts.hashes())


if __name__=="__main__":
    raise SystemExit(main())
