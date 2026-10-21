import asyncio,base64,copy,hashlib,json,pathlib,struct,time,unittest
from scheduler import run,summarize,Sample
from workload import ledger,validate_mix,MIX
from calibration import evaluate,cpu_fraction,nic_fraction,linux_cpu_snapshot,source_digest
from protocol import subscribe,validate
from network import null_request,websocket_subscribe
class WorkloadTests(unittest.TestCase):
    def test_exact_arithmetic_and_peak_subset(self):
        x=ledger();self.assertEqual(x["requests_per_day"],500000*2*50);self.assertAlmostEqual(float(__import__("fractions").Fraction(x["average_concurrency"])),10416.6666666667);self.assertEqual(sum(MIX.values()),10000);self.assertEqual(x["nats_r3_headroom_bytes"],2000*72*3600*1200*3*2);self.assertFalse(x["capacity_qualified"])
    def test_ledger_returned_mix_cannot_mutate_frozen_targets(self):
        first=ledger();first["peak_mix"]["quotes"]-=100;first["peak_mix"]["market"]+=100
        self.assertEqual(ledger()["peak_mix"]["quotes"],2000);self.assertEqual(ledger()["peak_mix"]["market"],1000)
    def test_doublecount_and_bad_categories_rejected(self):
        with self.assertRaises(ValueError):validate_mix(MIX,14000)
        with self.assertRaises(ValueError):validate_mix(dict(MIX,extra=1))
    def test_missed_arrivals_are_censored_not_near_zero_latency(self):
        samples=[Sample(i,i,completed_ns=i+1,status="missed") for i in range(99)]
        samples.append(Sample(99,99,attempted_ns=100,completed_ns=2000000099,status="timeout"))
        x=summarize(samples);self.assertEqual(x["latency_censored"],99);self.assertEqual(x["latency_observed"],1);self.assertEqual(x["latency_ns"]["0.99"],2000000000);self.assertEqual(x["latency_denominator"],100)
    def test_protocol_bounds(self):
        self.assertEqual(subscribe(["market.pool.0x"+"1"*40])["op"],"subscribe")
        for topics in ([],["x"]*2,[str(i) for i in range(21)]):
            with self.assertRaises(Exception):subscribe(topics)
        with self.assertRaises(Exception):validate("IF_STREAM_EVENT",{"sequence":1})
    def test_actual_frozen_positive_market_event(self):
        timestamp="2026-10-10T00:00:00.000Z"
        asset=dict(kind="erc20",chainId="125",address="0x"+"1"*40,currencyCode=None,decimals=6)
        price=dict(numerator="1",denominator="1",baseAsset=asset,quoteAsset=asset,source=dict(kind="external_display",amountBasis="provider_display",sourceEventId=None,position=None,providerId="synthetic-load",observedAt=timestamp,sourceReference="synthetic-not-chain"))
        event=dict(eventId="synthetic-1",topic="market.price.0x"+"1"*40,sequence="1",revision="1",occurredAt=timestamp,watermark=None,payload=price,coalesced=False,sourceSequenceFirst=None,sourceSequenceLast=None)
        validate("IF_STREAM_EVENT",event)
        event["payload"]["numerator"]=1
        with self.assertRaises(Exception):validate("IF_STREAM_EVENT",event)
    def test_cpu_and_nic_saturation_measurement(self):
        self.assertEqual(cpu_fraction((100,20),(200,40)),.8)
        self.assertEqual(nic_fraction(tx_before=0,rx_before=0,tx_after=100000000,rx_after=1,duration_ns=10**9,speed_bps=10**9),.8)
        self.assertGreater(linux_cpu_snapshot()[0],0)
        for bad in (float("nan"),float("inf"),True):
            with self.assertRaises(ValueError):cpu_fraction((bad,0),(100,1))
            with self.assertRaises(ValueError):nic_fraction(tx_before=bad,rx_before=0,tx_after=100,rx_after=0,duration_ns=1,speed_bps=1)
        with self.assertRaises(ValueError):nic_fraction(tx_before=10,rx_before=0,tx_after=0,rx_after=0,duration_ns=1,speed_bps=1)
    def test_gate_fails_closed_and_cannot_certify_capacity(self):
        now=time.time_ns();r=dict(candidate="a",topology="b",generator="c",outside_application_cluster=True,ended_utc_ns=now,duration_ns=60*10**9,cpu_fraction=.1,nic_fraction=.1,clock_offset_ms=1,delivery_error_fraction=0,scheduler_p99_ns=1000,scheduled=100,attempted=100,received=100,raw_samples_sha256="1"*64)
        raw=[dict(id=i,scheduled_ns=i*600000000,attempted_ns=i*600000000+1000,completed_ns=i*600000000+2000,receiver_monotonic_ns=i+1,receiver_utc_ns=now,status="received",error=None) for i in range(100)]
        r.update(rate=100/60,started_utc_ns=now-60*10**9,raw_samples=raw,raw_samples_sha256=hashlib.sha256(json.dumps(raw,sort_keys=True,separators=(",",":")).encode()).hexdigest(),cpu_before=[100,20],cpu_after=[200,110],nic_counters=dict(tx_before=0,rx_before=0,tx_after=750000000,rx_after=0,duration_ns=60*10**9,speed_bps=10**9))
        rs=[dict(r,host_id=str(i)) for i in range(4)]
        self.assertTrue(evaluate(rs,candidate="a",topology="b",generator="c",now_ns=now)["generator_calibrated"])
        for field,value in (("cpu_fraction",.71),("nic_fraction",.71),("clock_offset_ms",6),("duration_ns",1),("ended_utc_ns",now-301*10**9),("generator","stale"),("scheduled",200),("scheduler_p99_ns",float("nan")),("raw_samples_sha256","0"*64), ("raw_samples",[])):
            bad=copy.deepcopy(rs);bad[0][field]=value;self.assertFalse(evaluate(bad,candidate="a",topology="b",generator="c",now_ns=now)["generator_calibrated"],field)
        for field,value in (("receiver_monotonic_ns","bad"),("completed_ns",float("nan")),("attempted_ns",True),("receiver_utc_ns",-1)):
            malformed=copy.deepcopy(rs);malformed[0]["raw_samples"][0][field]=value
            malformed[0]["raw_samples_sha256"]=hashlib.sha256(json.dumps(malformed[0]["raw_samples"],sort_keys=True,separators=(",",":")).encode()).hexdigest()
            self.assertFalse(evaluate(malformed,candidate="a",topology="b",generator="c",now_ns=now)["generator_calibrated"],field)
        bad=copy.deepcopy(rs)
        for report in bad:
            report["raw_samples"]=report["raw_samples"][:1];report["scheduled"]=report["attempted"]=report["received"]=1
            report["raw_samples_sha256"]=hashlib.sha256(json.dumps(report["raw_samples"],sort_keys=True,separators=(",",":")).encode()).hexdigest()
        self.assertFalse(evaluate(bad,candidate="a",topology="b",generator="c",now_ns=now)["generator_calibrated"])
        self.assertFalse(evaluate(rs[:1],candidate="a",topology="b",generator="c",now_ns=now)["generator_calibrated"])
        self.assertFalse(evaluate(rs,candidate="a",topology="b",generator="c",now_ns=now)["capacity_qualified"])
class NetworkTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self): self.servers=[]
    async def asyncTearDown(self):
        for s in self.servers:s.close();await s.wait_closed()
    async def receiver(self,delay=0):
        async def handle(r,w):
            try:
                data=json.loads(await r.readline());mono=time.monotonic_ns();utc=time.time_ns()
                await asyncio.sleep(delay)
                w.write(json.dumps(dict(id=data["id"],receiver_monotonic_ns=mono,receiver_utc_ns=utc)).encode()+b"\n");await w.drain()
            finally:w.close();await w.wait_closed()
        s=await asyncio.start_server(handle,"127.0.0.1",24800,limit=4096);self.servers.append(s);return 24800
    async def test_corrupt_receiver_identity_and_timestamps_retained_as_errors(self):
        for field,value in (("id",True),("id",0.0),("receiver_monotonic_ns",0),("receiver_utc_ns",-1)):
            async def transport(sample):
                reply=dict(id=sample.id,receiver_monotonic_ns=1,receiver_utc_ns=1);reply[field]=value;return reply
            samples=await run(transport,rate=10,duration=.1);x=summarize(samples)
            self.assertEqual(x["scheduled"],1);self.assertEqual(x["received"],0);self.assertEqual(x["error"],1);self.assertEqual(x["latency_denominator"],1)
    async def test_actual_nullreceiver_delivery_timestamps(self):
        port=await self.receiver();samples=await run(lambda s:null_request(port,s),rate=40,duration=.5)
        x=summarize(samples);self.assertEqual(x["scheduled"],20);self.assertEqual(x["received"],20);self.assertLessEqual(x["delivery_error_fraction"],.02)
        for s in samples:self.assertLessEqual(s.scheduled_ns,s.attempted_ns);self.assertGreater(s.receiver_utc_ns,0)
        self.persist("nullreceiver",samples,x)
    async def test_two_second_backend_pause_keeps_arrival_denominator(self):
        port=await self.receiver(2);start=time.monotonic();samples=await run(lambda s:null_request(port,s),rate=20,duration=.5,timeout=2.5,max_inflight=32)
        x=summarize(samples);self.assertEqual(x["scheduled"],10);self.assertEqual(x["received"],10);self.assertGreaterEqual(x["latency_ns"]["0.99"],2*10**9);self.assertLess(samples[-1].attempted_ns-samples[0].attempted_ns,700000000);self.assertGreaterEqual(time.monotonic()-start,2)
        self.persist("pause",samples,x)
    async def test_inflight_misses_and_timeouts_not_erased(self):
        port=await self.receiver(.3);samples=await run(lambda s:null_request(port,s),rate=100,duration=.1,timeout=.04,max_inflight=1);x=summarize(samples)
        self.assertEqual(x["scheduled"],10);self.assertGreater(x["missed"],0);self.assertEqual(x["latency_censored"],x["missed"]);self.assertGreater(x["timeout"],0);self.assertEqual(sum(x[k] for k in ("received","timeout","missed","error")),10)
        self.persist("overload",samples,x)
    async def test_real_websocket_masked_frozen_subscribe(self):
        async def handler(r,w):
            try:
                h=await r.readuntil(b"\r\n\r\n");key=next(v.split(b":",1)[1].strip() for v in h.split(b"\r\n") if v.lower().startswith(b"sec-websocket-key:"));accept=base64.b64encode(hashlib.sha1(key+b"258EAFA5-E914-47DA-95CA-C5AB0DC85B11").digest());w.write(b"HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: "+accept+b"\r\n\r\n");await w.drain()
                head=await r.readexactly(2);self.assertTrue(head[1]&128);n=head[1]&127
                if n==126:n=struct.unpack("!H",await r.readexactly(2))[0]
                mask=await r.readexactly(4);data=await r.readexactly(n);body=bytes(v^mask[i%4] for i,v in enumerate(data));validate("IF_SUBSCRIBE",json.loads(body));w.write(b"\x81"+(bytes([n]) if n<126 else b"\x7e"+struct.pack("!H",n))+body);await w.drain()
            finally:w.close();await w.wait_closed()
        s=await asyncio.start_server(handler,"127.0.0.1",24801);self.servers.append(s)
        result=await asyncio.wait_for(websocket_subscribe(24801,["market.pool.0x"+"1"*40]),2);self.assertEqual(result["op"],"subscribe")
        topic=next("x"*n for n in range(1,100) if len(json.dumps(subscribe(["x"*n])).encode())==127)
        self.assertEqual((await asyncio.wait_for(websocket_subscribe(24801,[topic]),2))["topics"],[topic])
    async def test_websocket_accept_case_is_cryptographically_exact(self):
        async def handler(r,w):
            try:
                h=await r.readuntil(b"\r\n\r\n");key=next(v.split(b":",1)[1].strip() for v in h.split(b"\r\n") if v.lower().startswith(b"sec-websocket-key:"));accept=base64.b64encode(hashlib.sha1(key+b"258EAFA5-E914-47DA-95CA-C5AB0DC85B11").digest()).swapcase();w.write(b"HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: "+accept+b"\r\n\r\n");await w.drain()
            finally:w.close();await w.wait_closed()
        server=await asyncio.start_server(handler,"127.0.0.1",24801);self.servers.append(server)
        with self.assertRaises(ValueError):await asyncio.wait_for(websocket_subscribe(24801,["x"]),2)
    def persist(self,name,samples,summary):
        import dataclasses
        out=pathlib.Path(__file__).resolve().parents[4]/"launchpad/evidence/production-system/L48/phase1";out.mkdir(parents=True,exist_ok=True)
        (out/(name+".json")).write_text(json.dumps(dict(label="single-loopback-small-calibration-not-capacity",generator=source_digest(pathlib.Path(__file__).parent),samples=[dataclasses.asdict(s) for s in samples],summary=summary),indent=2))
if __name__=="__main__":unittest.main()
