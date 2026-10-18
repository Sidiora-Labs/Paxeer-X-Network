"""Bounded open-arrival scheduler. All scheduled arrivals survive in denominator.

Monotonic timestamps are local to their process/host; never subtract clocks across
hosts. Receiver also supplies UTC; cross-host latency requires clock calibration.
"""
import asyncio, dataclasses, math, time, uuid
@dataclasses.dataclass
class Sample:
    id: int
    scheduled_ns: int
    attempted_ns: int | None = None
    completed_ns: int | None = None
    receiver_monotonic_ns: int | None = None
    receiver_utc_ns: int | None = None
    status: str = "missed"
    error: str | None = None
    def latency_ns(self):
        return None if self.status=="missed" or self.completed_ns is None else self.completed_ns-self.scheduled_ns
async def run(transport, *, rate, duration, timeout=1.0, max_inflight=128):
    if not all(math.isfinite(x) and x>0 for x in (rate,duration,timeout)):
        raise ValueError("finite positive settings required")
    count=math.ceil(rate*duration)
    if count>1000000 or type(max_inflight) is not int or not 1<=max_inflight<=4096:
        raise ValueError("bounded sample/inflight limits exceeded")
    start=time.monotonic_ns()+10000000
    samples=[Sample(i,start+int(i*1e9/rate)) for i in range(count)]
    pending=set()
    async def send(s):
        s.attempted_ns=time.monotonic_ns()
        try:
            reply=await asyncio.wait_for(transport(s),timeout)
            if type(reply["id"]) is not int or reply["id"]!=s.id or type(reply["receiver_monotonic_ns"]) is not int or reply["receiver_monotonic_ns"]<=0 or type(reply["receiver_utc_ns"]) is not int or reply["receiver_utc_ns"]<=0:
                raise ValueError("receiver identity/timestamps invalid")
            s.receiver_monotonic_ns=reply["receiver_monotonic_ns"]
            s.receiver_utc_ns=reply["receiver_utc_ns"]
            s.status="received"
        except asyncio.TimeoutError: s.status="timeout"
        except Exception as e: s.status="error"; s.error=type(e).__name__
        finally: s.completed_ns=time.monotonic_ns()
    try:
        for s in samples:
            await asyncio.sleep(max(0,(s.scheduled_ns-time.monotonic_ns())/1e9))
            if len(pending)>=max_inflight:
                s.completed_ns=time.monotonic_ns();s.status="missed";continue
            task=asyncio.create_task(send(s));pending.add(task);task.add_done_callback(pending.discard)
        if pending: await asyncio.gather(*tuple(pending))
    finally:
        for task in pending: task.cancel()
        if pending: await asyncio.gather(*tuple(pending),return_exceptions=True)
    return samples

def percentile(values,p):
    if not values: return None
    values=sorted(values);return values[max(0,math.ceil(len(values)*p)-1)]
def summarize(samples):
    lat=[s.latency_ns() for s in samples if s.latency_ns() is not None]
    delays=[s.attempted_ns-s.scheduled_ns for s in samples if s.attempted_ns is not None]
    statuses={k:sum(s.status==k for s in samples) for k in ("received","timeout","missed","error")}
    return dict(scheduled=len(samples), attempted=sum(s.attempted_ns is not None for s in samples),
        **statuses, latency_ns={str(p):percentile(lat,p) for p in (.5,.95,.99,.999,1)},
        scheduler_p99_ns=percentile(delays,.99),
        delivery_error_fraction=1-statuses["received"]/len(samples) if samples else 1,
        latency_denominator=len(samples), latency_observed=len(lat), latency_censored=len(samples)-len(lat))
