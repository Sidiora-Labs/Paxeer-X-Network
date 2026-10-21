import dataclasses
from fractions import Fraction
import http.server
import io
import pathlib
import random
import socket
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock
from oracle import *
from adapters import *
from evidence_policy import validate_phase1
from fixtures import verify_inventory, evaluate_fixture
import json

H=lambda n:"0x"+format(n,"064x")
A=lambda n:"0x"+format(n,"040x")
def trade(h=1,tx=0,log=0,side="buy",q=10,c=100,t=1704067200):
    return Event(h,H(h+1),H(h),t,tx,log,H(100+h*10+tx),"core-v1",A(1),A(2),side,q,c,0,inventory_owner=A(2))
def block(e): return Block(e.height,e.block_hash,e.parent_hash,e.timestamp,(e,))

class FixtureTests(unittest.TestCase):
    def test_persisted_inputs_match_independent_exact_outputs(self):
        result = verify_inventory(pathlib.Path(__file__).with_name("canonical-fixtures.json"))
        self.assertEqual(len(result["fixtures"]), 3)
        self.assertFalse(result["production_qualified"])
        self.assertEqual(len({f["expected_checksum"] for f in result["fixtures"]}), 3)

    def test_fixture_expected_basis_detects_lifetime_average(self):
        source = pathlib.Path(__file__).with_name("canonical-fixtures.json")
        doc = json.loads(source.read_text())
        doc["fixtures"][0]["expected"]["positions"][0]["cost"] = 150000000
        with tempfile.TemporaryDirectory() as td:
            changed = pathlib.Path(td) / "fixtures.json"
            changed.write_text(json.dumps(doc))
            with self.assertRaises(IntegrityError):
                verify_inventory(changed)

    def test_fixture_missing_block_prevents_expected_state(self):
        doc = json.loads(pathlib.Path(__file__).with_name("canonical-fixtures.json").read_text())
        fixture = doc["fixtures"][2]
        fixture["blocks"] = [b for b in fixture["blocks"] if b["height"] != 10]
        with self.assertRaises(IntegrityError):
            evaluate_fixture(fixture)

class PolicyTests(unittest.TestCase):
    def test_exact_bound_manifest_and_no_evidence_escalation(self):
        root=pathlib.Path(__file__).parent
        binding=json.loads((root/"phase-1-evidence-policy.json").read_text())["binding"]
        self.assertEqual(validate_phase1(root,binding)["scenarios"],13)
        with tempfile.TemporaryDirectory() as td:
            out=pathlib.Path(td)
            for name in ("phase-1-scenario-manifest.json","phase-1-evidence-policy.json"):
                (out/name).write_bytes((root/name).read_bytes())
            policy=json.loads((out/"phase-1-evidence-policy.json").read_text());policy["production_qualified"]=True
            (out/"phase-1-evidence-policy.json").write_text(json.dumps(policy))
            with self.assertRaises(ValueError): validate_phase1(out,binding)
        wrong={**binding,"candidate":"f"*64}
        with self.assertRaises(ValueError): validate_phase1(root,wrong)

class OracleTests(unittest.TestCase):
    def test_lifetime_basis_regression_and_recipient(self):
        l=Ledger()
        for e in (trade(q=10,c=100000000),trade(h=2,side="sell",q=10,c=100000000),trade(h=3,q=10,c=200000000)): l.apply(e)
        self.assertEqual(l.positions[(A(2),A(1))],Position(10,200000000,0,0))
        self.assertEqual(l.unrealized(A(2),A(1),Fraction(20000000)),0)
        self.assertNotIn((A(3),A(1)),l.positions)
    def test_sell_owner_differs_from_usdl_recipient(self):
        l=Ledger(); l.apply(trade())
        l.apply(dataclasses.replace(trade(h=2,side="sell"),recipient=A(3)))
        self.assertEqual(l.positions[(A(2),A(1))].quantity,0)
        self.assertNotIn((A(3),A(1)),l.positions)
        with self.assertRaises(IntegrityError): dataclasses.replace(trade(side="sell"),inventory_owner=None)
        with self.assertRaises(IntegrityError): dataclasses.replace(trade(),inventory_owner=A(3))
    def test_partial_floor_residual_and_fee_normalization(self):
        l=Ledger(); l.apply(trade(q=3,c=100)); l.apply(trade(h=2,side="sell",q=1,c=40))
        self.assertEqual(l.positions[(A(2),A(1))],Position(2,67,7,0))
        l.apply(trade(h=3,side="sell",q=2,c=65))
        self.assertEqual(l.positions[(A(2),A(1))],Position(0,0,5,0))
    def test_unknown_transfer_cannot_be_zero_basis_profit(self):
        l=Ledger(); l.transfer_in(A(2),A(1),10)
        self.assertIsNone(l.unrealized(A(2),A(1),Fraction(1)))
        with self.assertRaises(IntegrityError): l.apply(trade(side="sell"))
        self.assertEqual(l.positions[(A(2),A(1))].unknown,10)
    def test_duplicate_ten_times_and_conflict_atomic(self):
        l=Ledger(); e=trade()
        for _ in range(10): l.apply(e)
        before=checksum(l.positions)
        with self.assertRaises(IntegrityError): l.apply(dataclasses.replace(e,usdl_raw=101))
        self.assertEqual(checksum(l.positions),before); self.assertEqual(len(l.seen),1)
    def test_twenty_same_second_blocks_shuffled(self):
        bs=[block(trade(h=i,t=1704067200)) for i in range(1,21)]
        random.Random(125).shuffle(bs)
        events=inventory(bs,1,H(1))
        self.assertEqual([e.height for e in events],list(range(1,21)))
        self.assertEqual(checksum(candles(events)),checksum(candles(list(reversed(events)))))
    def test_hash_disagreement_is_not_reorg(self):
        b=block(trade())
        with self.assertRaises(IntegrityError): inventory([b,dataclasses.replace(b,block_hash=H(999),events=())],1,H(1))
    def test_gap_empty_and_unknown_abi(self):
        e=trade(); empty=Block(2,H(3),H(2),e.timestamp,())
        self.assertEqual(len(inventory([block(e),empty],1,H(1))),1)
        with self.assertRaises(IntegrityError): inventory([block(e),block(trade(h=3))],1,H(1))
        with self.assertRaises(IntegrityError): dataclasses.replace(e,abi_known=False)
        with self.assertRaises(IntegrityError): dataclasses.replace(empty,complete=False)
    def test_position_collision_and_payload_equivocation(self):
        e=trade()
        for other in (dataclasses.replace(e,tx_hash=H(999)),dataclasses.replace(e,usdl_raw=999)):
            with self.assertRaises(IntegrityError): inventory([Block(1,H(2),H(1),e.timestamp,(e,other))],1,H(1))
    def test_complete_inventory_cannot_disagree_or_reuse_global_logs(self):
        e=trade()
        with self.assertRaises(IntegrityError): inventory([block(e),Block(1,H(2),H(1),e.timestamp,())],1,H(1))
        other=trade(tx=1,log=0)
        with self.assertRaises(IntegrityError): inventory([Block(1,H(2),H(1),e.timestamp,(e,other))],1,H(1))
        other=trade(tx=1,log=1)
        with self.assertRaises(IntegrityError): inventory([Block(1,H(2),H(1),e.timestamp,(dataclasses.replace(e,log_index=2),other))],1,H(1))
    def test_total_bounds_and_quantity_overflow_atomic(self):
        with self.assertRaises(IntegrityError): inventory([block(trade()),block(trade(h=2))],1,H(1),max_events=1)
        with self.assertRaises(IntegrityError): candles(iter([trade(),trade(h=2)]),max_events=1)
        l=Ledger(); l.apply(trade(q=UINT256)); before=checksum(l.positions)
        with self.assertRaises(IntegrityError): l.apply(trade(h=2,q=1))
        with self.assertRaises(IntegrityError): l.transfer_in(A(2),A(1),1)
        self.assertEqual(checksum(l.positions),before)
        self.assertEqual(len(l.seen),1)
    def test_transaction_hash_bijection(self):
        a=trade(); b=dataclasses.replace(trade(tx=1,log=1),tx_hash=a.tx_hash)
        with self.assertRaises(IntegrityError): inventory([Block(1,H(2),H(1),a.timestamp,(a,b))],1,H(1))
    def test_candle_exact_all_intervals_order_and_volume(self):
        a=trade(q=3,c=1000000); b=trade(tx=1,log=1,q=7,c=9000000)
        bs=candles([b,a,a]); self.assertEqual(len(bs),7)
        for v in bs.values():
            self.assertEqual(v["open"],Fraction(1,3)); self.assertEqual(v["close"],Fraction(9,7))
            self.assertEqual(v["trades"],2); self.assertEqual(v["usdl_raw"],10000000)
    def test_utc_monday_and_half_open(self):
        a=trade(t=1704067259); b=trade(h=2,t=1704067260)
        c=candles([a,b]); self.assertEqual(len([k for k in c if k[1]=="1m"]),2)
        self.assertIn((A(1),"1w",1704067200),c)
    def test_decimal_255_exact_and_no_float(self):
        e=dataclasses.replace(trade(q=2**200,c=2**180),token_decimals=255)
        v=next(iter(candles([e]).values()))["open"]
        self.assertEqual(v,Fraction(2**180*10**255,2**200*10**6))
        for bad in (True,1.2,-1,2**256):
            with self.assertRaises(IntegrityError): dataclasses.replace(e,token_raw=bad)
    def test_decimal_conflict_rejected(self):
        with self.assertRaises(IntegrityError): candles([trade(),dataclasses.replace(trade(h=2),token_decimals=6)])

class AdapterTests(unittest.TestCase):
    def test_real_loopback_http_bounds_redirect_and_status(self):
        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(302 if self.path=="/redirect" else 200); self.end_headers()
                self.wfile.write(b"x"*200 if self.path=="/large" else b'{"actual":true}')
            def log_message(self,*a): pass
        server=http.server.ThreadingHTTPServer(("127.0.0.1",0),Handler)
        thread=threading.Thread(target=server.serve_forever,daemon=True); thread.start()
        try:
            port=server.server_port; adapter=LocalHTTP("http://127.0.0.1:"+str(port),{port},max_bytes=100)
            self.assertEqual(adapter.request("GET","/health"),(200,b'{"actual":true}'))
            for path in ("/large","/redirect","//external"):
                with self.assertRaises(BoundaryError): adapter.request("GET",path)
        finally: server.shutdown();server.server_close();thread.join()
    def test_real_http_total_deadline_trickle(self):
        class Slow(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(200); self.end_headers()
                try:
                    for _ in range(20): self.wfile.write(b"x"); self.wfile.flush(); time.sleep(.1)
                except (BrokenPipeError,ConnectionResetError): pass
            def log_message(self,*a): pass
        server=http.server.ThreadingHTTPServer(("127.0.0.1",0),Slow);server.daemon_threads=True
        thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
        start=time.monotonic()
        try:
            adapter=LocalHTTP("http://127.0.0.1:"+str(server.server_port),{server.server_port},timeout=.25)
            # An aborted body may return truncated bytes with HTTPConnection; it
            # must still finish at the total deadline and never qualify JSON.
            try: status,body=adapter.request("GET","/slow")
            except (OSError,http.client.HTTPException): pass
            self.assertLess(time.monotonic()-start,.8)
        finally: server.shutdown();server.server_close();thread.join()
    def test_http_closed_world_no_credentials_dns_or_live_origin(self):
        for origin in ("http://localhost:24900","https://127.0.0.1:24900","http://127.0.0.1:8645","http://user:pass@127.0.0.1:24900","http://8.8.8.8:24900"):
            with self.assertRaises(BoundaryError): LocalHTTP(origin,{24900})
    def test_real_owned_process_group_cleanup(self):
        with tempfile.TemporaryDirectory() as td, open(pathlib.Path(td)/"child.log","wb") as log:
            with OwnedProcess([sys.executable,"-c","import time; print('ready',flush=True); time.sleep(60)"],td,log) as process:
                deadline=time.monotonic()+3
                while time.monotonic()<deadline and not (pathlib.Path(td)/"child.log").read_bytes(): time.sleep(.01)
                self.assertTrue(process.alive()); self.assertIn(b"ready",(pathlib.Path(td)/"child.log").read_bytes())
            self.assertIsNotNone(process.child.returncode)
            with mock.patch("adapters.os.killpg",side_effect=AssertionError("repeated cleanup must not signal")):
                process.stop()
    def test_bounded_real_subprocess_timeout_and_output(self):
        self.assertEqual(bounded_command([sys.executable,"-c","print('ok')"],2),b"ok\n")
        with self.assertRaises(BoundaryError): bounded_command([sys.executable,"-c","print('x'*20000)"],2,1000)
        with self.assertRaises(TimeoutError): bounded_command([sys.executable,"-c","import time; time.sleep(10)"],.1)
    def test_owned_process_exited_leader_descendant_cleanup(self):
        with tempfile.TemporaryDirectory() as td, open(pathlib.Path(td)/"log","wb") as log:
            marker=pathlib.Path(td)/"escaped"
            script="import subprocess,sys; subprocess.Popen([sys.executable,'-c',"+repr("import signal,time,pathlib; signal.signal(signal.SIGTERM,signal.SIG_IGN); time.sleep(.8); pathlib.Path("+repr(str(marker))+").write_text('leaked'); time.sleep(20)")+"])"
            proc=OwnedProcess([sys.executable,"-c",script],td,log)
            proc.child.wait(timeout=3); proc.stop(); time.sleep(.9)
            self.assertFalse(marker.exists())
    def test_fault_allowlist_negative_identity_role_mount_action(self):
        p=FaultPermit("a"*64,"kd-"+"b"*24,"12345678-1234-1234-1234-123456789abc","redis",("pause","unpause"))
        obj={"Id":p.container_id,"Config":{"Labels":{"kindle.assurance.namespace":p.namespace,"kindle.assurance.attempt":p.attempt,"kindle.assurance.role":p.role,"kindle.assurance.isolated":"true"}},"HostConfig":{},"Mounts":[]}
        DockerFaults.validate_inspect(p,obj)
        for change in ({"Id":"c"*64},{"Mounts":[{"Source":"/"}]},{"HostConfig":{"Privileged":True}},{"Config":{"Labels":{}}}):
            with self.assertRaises(BoundaryError): DockerFaults.validate_inspect(p,{**obj,**change})
        with self.assertRaises(BoundaryError): DockerFaults([p],"kd-"+"c"*24,p.attempt)
        d=DockerFaults([p],p.namespace,p.attempt)
        for cid,action in ((p.container_id,"kill"),("peer","pause")):
            with self.assertRaises(BoundaryError): d.apply(cid,action)

if __name__=="__main__": unittest.main(verbosity=2)
