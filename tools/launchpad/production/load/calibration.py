"""Fail-closed measured generator gate; reports cannot self-declare capacity."""
import hashlib,json,math,time
from scheduler import Sample,summarize
from pathlib import Path
VERSION="l48-calibration-v1"
def source_digest(directory):
    files=sorted(Path(directory).glob("*.py"))
    return hashlib.sha256(json.dumps({p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in files},sort_keys=True).encode()).hexdigest()
def evaluate(reports, *, candidate, topology, generator, now_ns=None):
    now_ns=time.time_ns() if now_ns is None else now_ns
    failures=[]
    if len(reports)<4: failures.append("four independent generator hosts required")
    identities=set()
    for r in reports:
        try:
            if not isinstance(r["host_id"],str) or not r["host_id"]: raise ValueError("host")
            identities.add(r["host_id"])
            raw=r["raw_samples"]
            if not isinstance(raw,list) or not 1<=len(raw)<=1000000: raise ValueError("samples")
            encoded=json.dumps(raw,sort_keys=True,separators=(",",":")).encode()
            if hashlib.sha256(encoded).hexdigest()!=r["raw_samples_sha256"]: failures.append("raw sample hash mismatch")
            parsed=[Sample(**row) for row in raw]
            if [s.id for s in parsed]!=list(range(len(parsed))): failures.append("sample identity gap")
            for sample in parsed:
                if any(type(v) is not int or v<0 for v in (sample.id,sample.scheduled_ns,sample.completed_ns)): raise ValueError("integer sample timestamps required")
                if any(v is not None and (type(v) is not int or v<=0) for v in (sample.attempted_ns,sample.receiver_monotonic_ns,sample.receiver_utc_ns)): raise ValueError("invalid optional timestamps")
                if (sample.status=="missed") != (sample.attempted_ns is None): raise ValueError("attempt/status mismatch")
                if sample.attempted_ns is not None and sample.completed_ns<sample.attempted_ns: raise ValueError("completion before attempt")
                if sample.status not in ("received","timeout","missed","error") or type(sample.scheduled_ns) is not int or sample.completed_ns is None or sample.completed_ns<sample.scheduled_ns: raise ValueError("sample state")
                if sample.attempted_ns is not None and sample.attempted_ns<sample.scheduled_ns: raise ValueError("early attempt")
                if sample.status=="received" and (sample.attempted_ns is None or sample.receiver_monotonic_ns is None or sample.receiver_utc_ns is None): raise ValueError("missing receiver")
            if type(r["duration_ns"]) is not int or type(r["started_utc_ns"]) is not int or type(r["ended_utc_ns"]) is not int or r["ended_utc_ns"]-r["started_utc_ns"] < r["duration_ns"]: raise ValueError("UTC interval")
            if type(r["rate"]) not in (int,float) or not math.isfinite(r["rate"]) or r["rate"]<=0: raise ValueError("rate")
            if len(parsed)!=math.ceil(r["duration_ns"]*r["rate"]/1e9): raise ValueError("arrival duration mismatch")
            origin=parsed[0].scheduled_ns
            for i,sample in enumerate(parsed):
                if sample.scheduled_ns != origin+int(i*1e9/r["rate"]): raise ValueError("arrival schedule mismatch")
                if sample.receiver_utc_ns is not None and not r["started_utc_ns"]-5000000<=sample.receiver_utc_ns<=r["ended_utc_ns"]+5000000: raise ValueError("receiver UTC coverage")
            if r["nic_counters"]["duration_ns"]!=r["duration_ns"]: raise ValueError("resource interval mismatch")
            measured=summarize(parsed)
            for field in ("scheduled","attempted","received","delivery_error_fraction","scheduler_p99_ns"):
                if measured[field]!=r[field]: failures.append("raw summary mismatch: "+field)
            measured_cpu=cpu_fraction(tuple(r["cpu_before"]),tuple(r["cpu_after"]))
            measured_nic=nic_fraction(**r["nic_counters"])
            if abs(measured_cpu-r["cpu_fraction"])>1e-12 or abs(measured_nic-r["nic_fraction"])>1e-12: failures.append("resource telemetry mismatch")
            if r["candidate"]!=candidate or r["topology"]!=topology or r["generator"]!=generator: failures.append("binding mismatch")
            if not r["outside_application_cluster"]: failures.append("generator inside application cluster")
            if r["ended_utc_ns"]>now_ns or now_ns-r["ended_utc_ns"]>300*10**9: failures.append("future or expired report")
            if r["duration_ns"]<60*10**9: failures.append("short calibration")
            for field,limit in (("cpu_fraction",.7),("nic_fraction",.7),("clock_offset_ms",5),("delivery_error_fraction",.02),("scheduler_p99_ns",10000000)):
                v=r[field]
                if type(v) not in (int,float) or not math.isfinite(v) or v<0 or v>limit: failures.append(field)
            if r["scheduled"]<=0 or r["attempted"]>r["scheduled"] or r["received"]>r["attempted"] or r["received"]<0: failures.append("invalid counters")
            if abs(r["delivery_error_fraction"]-(1-r["received"]/r["scheduled"]))>1e-12: failures.append("counter mismatch")
            if not r["raw_samples_sha256"] or len(r["raw_samples_sha256"])!=64: failures.append("missing samples")
        except (KeyError,TypeError,ValueError,ZeroDivisionError,IndexError): failures.append("missing or malformed measurement")
    if len(identities)!=len(reports): failures.append("duplicate generator host")
    return {"generator_calibrated":not failures,"capacity_qualified":False,"failures":failures}

def linux_cpu_snapshot():
    values=list(map(int,Path("/proc/stat").read_text().splitlines()[0].split()[1:]));return sum(values[:8]),sum(values[3:5])
def cpu_fraction(before,after):
    if len(before)!=2 or len(after)!=2 or any(type(v) is not int or v<0 for v in (*before,*after)): raise ValueError("integer CPU counters required")
    total=after[0]-before[0]; idle=after[1]-before[1]
    if total<=0 or idle<0 or idle>total: raise ValueError("invalid CPU sample interval")
    return (total-idle)/total

def nic_fraction(*,tx_before,rx_before,tx_after,rx_after,duration_ns,speed_bps):
    if any(type(v) is not int for v in (tx_before,rx_before,tx_after,rx_after,duration_ns,speed_bps)): raise ValueError("integer NIC telemetry required")
    if min(tx_before,rx_before,tx_after,rx_after)<0 or duration_ns<=0 or speed_bps<=0 or tx_after<tx_before or rx_after<rx_before: raise ValueError("missing/reset NIC telemetry")
    # Full duplex admission conservatively uses busiest direction, never subtracts traffic.
    return max(tx_after-tx_before,rx_after-rx_before)*8*1e9/duration_ns/speed_bps
