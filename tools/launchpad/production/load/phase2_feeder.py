from __future__ import annotations

import asyncio
import json
import os
from pathlib import Path
import re
import signal
import time

from phase2_types import canonical, digest


async def owned_command(argv, *, environment, timeout, maximum_bytes=67108864):
    child = await asyncio.create_subprocess_exec(*argv, env=environment, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE, start_new_session=True)
    async def read(stream):
        result = bytearray()
        while True:
            block = await stream.read(65536)
            if not block:
                return bytes(result)
            result.extend(block)
            if len(result) > maximum_bytes:
                raise ValueError("owned command output budget exceeded")
    try:
        stdout, stderr = await asyncio.wait_for(asyncio.gather(read(child.stdout), read(child.stderr)), timeout)
        if await child.wait() != 0:
            raise RuntimeError("actual provider command failed; private provider log required")
        return stdout
    finally:
        if child.returncode is None:
            for sig in (signal.SIGTERM, signal.SIGKILL):
                try:
                    os.killpg(child.pid, sig)
                except ProcessLookupError:
                    break
                if sig == signal.SIGTERM:
                    try:
                        await asyncio.wait_for(child.wait(), 1)
                        break
                    except TimeoutError:
                        pass
            await child.wait()


class PostgresObserver:
    def __init__(self, executable, sha256, environment, *, stream_id, pools):
        path = Path(executable)
        if not path.is_absolute() or path.is_symlink() or digest(path.read_bytes()) != sha256:
            raise ValueError("source-bound real psql executable required")
        if not re.fullmatch(r"[A-Za-z0-9_.:-]{1,128}", stream_id) or not pools or any(not re.fullmatch(r"0x[0-9a-f]{40}", pool) for pool in pools):
            raise ValueError("exact canonical stream/pools required")
        if not environment or not set(environment) <= {"PGHOST", "PGPORT", "PGUSER", "PGDATABASE", "PGPASSWORD", "PGSSLMODE", "PGSSLROOTCERT", "PGSERVICE", "PGSERVICEFILE", "PGPASSFILE", "PATH"}:
            raise ValueError("explicit private PostgreSQL connection environment required")
        self.executable, self.environment = str(path), dict(environment)
        self.stream_id, self.pools = stream_id, pools

    async def query(self, sql):
        raw = await owned_command([self.executable, "--no-psqlrc", "--no-align", "--tuples-only", "--set=ON_ERROR_STOP=1", "--command", "BEGIN READ ONLY; SET LOCAL statement_timeout='3s'; " + sql + "; COMMIT;"], environment=self.environment, timeout=5)
        lines = [line for line in raw.decode().splitlines() if line.startswith("{")]
        return [json.loads(line) for line in lines]

    async def readiness(self):
        rows = await self.query("SELECT json_build_object('postgres',current_setting('server_version_num'),'track_commit_timestamp',current_setting('track_commit_timestamp'),'recovery',pg_is_in_recovery())")
        if len(rows) != 1 or rows[0]["track_commit_timestamp"] != "on" or rows[0]["recovery"]:
            raise ValueError("durable ingest latency needs primary PostgreSQL track_commit_timestamp=on")
        return rows[0]

    async def committed(self, *, after=None, limit=5000):
        if type(limit) is not int or not 1 <= limit <= 5000:
            raise ValueError("bounded canonical observation page required")
        predicate = ""
        if after is not None:
            if not re.fullmatch(r"[A-Za-z0-9_.:-]{1,256}", after):
                raise ValueError("unsafe observation cursor")
            predicate = " AND f.source_fill_id > '" + after + "'"
        pools = ",".join("'" + pool + "'" for pool in self.pools)
        sql = """SELECT json_build_object('event_id',f.source_fill_id,'source_fact_digest',f.source_fact_digest,'transaction_hash',r.transaction_hash,'height',r.height::text,'transaction_index',r.transaction_index::text,'log_index',r.log_index::text,'block_hash',r.block_hash,'durable_commit_utc_ns',(extract(epoch FROM pg_xact_commit_timestamp(c.xmin))*1000000000)::numeric(30,0)::text,'timestamp_authority','postgres_transaction_commit') FROM indexer.indexer_accounting_fills f JOIN indexer.indexer_raw_logs r USING(chain_id,transaction_hash,log_index) JOIN indexer.indexer_ingest_commits c ON c.chain_id=r.chain_id AND r.height>c.previous_height AND r.height<=c.last_complete_height WHERE c.stream_id='""" + self.stream_id + "' AND f.pool_address IN (" + pools + ")" + predicate + " ORDER BY f.source_fill_id LIMIT " + str(limit)
        rows = await self.query(sql)
        if any(row["durable_commit_utc_ns"] is None for row in rows):
            raise ValueError("commit timestamp authority expired or absent")
        return rows

    async def stream_fills(self, topics, initial_sequence):
        if any(not re.fullmatch(r"market\.trades\.0x[0-9a-f]{40}", t) for t in topics):
            raise ValueError("exact trade topic reconciliation required")
        statements = []
        for topic in topics:
            sequence = initial_sequence[topic]
            if type(sequence) is not int or not 0 <= sequence < 2**63:
                raise ValueError("bounded decimal source sequence required")
            statements.append("SELECT json_build_object('event_id',o.event_id,'topic',p.topic,'sequence',o.sequence::text,'frame',convert_from(o.frame,'UTF8')::jsonb) FROM api_stream.stream_delivery_outbox o JOIN api_stream.stream_projection p USING(partition_id) WHERE p.topic='" + topic + "' AND o.sequence>" + str(sequence))
        return await self.query(" UNION ALL ".join(statements))


async def feed_real_transactions(http, transactions, output, *, admitted_mutation=False):
    if admitted_mutation is not True or len(transactions) < 600:
        raise ValueError("explicit owned-chain transaction mutation admission required")
    chain = await http.call("POST", "/", {"jsonrpc": "2.0", "id": 1, "method": "eth_chainId", "params": []})
    if chain.get("result") != "0x7d":
        raise ValueError("actual chain125 required")
    start = time.monotonic_ns()
    for index, transaction in enumerate(transactions):
        scheduled = start + index * 600000000000 // len(transactions)
        await asyncio.sleep(max(0, (scheduled - time.monotonic_ns()) / 1e9))
        if not isinstance(transaction, str) or not re.fullmatch(r"0x[0-9a-fA-F]+", transaction) or len(transaction) > 262144:
            raise ValueError("real pre-signed bounded transaction required")
        response = await http.call("POST", "/", {"jsonrpc": "2.0", "id": index + 2, "method": "eth_sendRawTransaction", "params": [transaction]})
        tx_hash = response.get("result")
        if "error" in response or not isinstance(tx_hash, str) or not re.fullmatch(r"0x[0-9a-fA-F]{64}", tx_hash):
            raise RuntimeError("real signed transaction rejected")
        deadline = time.monotonic() + 20
        while True:
            receipt = (await http.call("POST", "/", {"jsonrpc": "2.0", "id": index + 2, "method": "eth_getTransactionReceipt", "params": [tx_hash]})).get("result")
            if receipt is not None:
                if receipt.get("status") != "0x1" or receipt.get("transactionHash", "").lower() != tx_hash.lower() or not receipt.get("blockHash"):
                    raise RuntimeError("actual mined transaction failed")
                output.write(canonical({"transaction_hash": tx_hash.lower(), "block_hash": receipt["blockHash"], "height": str(int(receipt["blockNumber"], 16)), "logs": receipt["logs"], "observed_utc_ns": time.time_ns()}).decode() + "\n")
                break
            if time.monotonic() >= deadline:
                raise TimeoutError("actual chain receipt deadline")
            await asyncio.sleep(.05)
