#!/usr/bin/env python3
import argparse
import hashlib
import http.server
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
BUILD = Path(os.environ.get("LAYERX_BUILD_DIR", ROOT / "build")).resolve()
BACKEND = ROOT / "explorer/backend"
TOOLCHAIN_IMAGE = "task23-1-toolchain:local"
POSTGRES_IMAGE = "postgres:15-alpine"
PG_CONTAINER = "task22-1-postgres"
API_CONTAINER = "task22-1-explorer"
CASES = ("kernel-receipts", "history-readiness", "standalone-receipts", "unified-pagination", "receipt-refresh")
PUBLIC_ROUTES = ("/v1/sync/network", "/v1/history/batches", "/v1/sync/batches/")


class MissingPrerequisite(Exception):
    pass


def require(condition, message):
    if not condition:
        raise AssertionError(message)


KERNEL_ROOT = Path(__file__).resolve().parents[3]
KERNEL_BUILD = Path(os.environ.get("LAYERX_BUILD_DIR", KERNEL_ROOT / "build")).resolve()


def kernel_relay_fixture(relay_module):
    import urllib.parse
    fixture_path = Path(os.environ["LAYERX_KERNEL_RECEIPT_RELAY_FIXTURE"]).resolve()
    fixture = json.loads(fixture_path.read_text())
    require(fixture.get("network_id") == 77, "kernel receipt fixture must use dedicated network 77")
    for name in ("genesis_sha256", "sequencer_id", "sequencer_public_key", "sequencer_first_batch", "sequencer_last_batch"):
        require(name in fixture, "real kernel fixture lacks " + name)
    require(fixture["sequencer_first_batch"] == 1, "kernel fixture must expose public history from batch 1")
    for name in ("upstreams", "submission_upstreams"):
        values = fixture.get(name)
        require(isinstance(values, list) and len(values) >= 2, "kernel fixture needs two genuine " + name)
        for origin in values:
            parsed = urllib.parse.urlparse(origin)
            require(parsed.scheme in ("http", "https") and parsed.hostname in ("localhost", "127.0.0.1", "::1")
                    and parsed.username is None and parsed.password is None, "kernel fixture must use isolated loopback sources")

    class Relay(relay_module.Scenario):
        def setup(self):
            self.work.chmod(0o700)
            self.bin = KERNEL_BUILD / "bin"
            self.runtime = KERNEL_ROOT / "platform/relay_archive"
            names = ("network_id", "genesis_sha256", "sequencer_id", "sequencer_public_key",
                     "sequencer_first_batch", "sequencer_last_batch", "ca_file")
            self.pins = {name: fixture[name] for name in names if name in fixture}
            if self.pins.get("ca_file"):
                self.pins["ca_file"] = str((fixture_path.parent / self.pins["ca_file"]).resolve())
            self.pins.update(allow_loopback_dev=True, poll_interval_seconds=0.1,
                             codec=str(self.bin / "layerx-archive-codec"))
            self.origin1, self.origin2 = fixture["upstreams"][:2]
            self.headers = {"Content-Type": "application/octet-stream"}
            credential = os.environ.get("LAYERX_KERNEL_RECEIPT_SUBMISSION_AUTHORIZATION")
            if credential:
                require("\r" not in credential and "\n" not in credential and len(credential) <= 8192,
                        "invalid isolated fixture submission authorization")
                self.headers["Authorization"] = credential
            self.submissions = Path(os.environ["LAYERX_KERNEL_RECEIPT_SUBMISSIONS_DIR"]).resolve()

        def relay_config(self, name, upstreams):
            config = super().relay_config(name, upstreams)
            config["submission_upstreams"] = fixture["submission_upstreams"]
            return config

    return Relay()


def prerequisites():
    missing = []
    for tool in ("docker", "openssl"):
        if shutil.which(tool) is None:
            missing.append(tool)
    for name in ("bin/layerxd", "bin/layerx-archive-codec"):
        if not (KERNEL_BUILD / name).is_file():
            missing.append(f"{KERNEL_BUILD / name} (make relay-archive-build)")
    if importlib.util.find_spec("cryptography") is None:
        missing.append("python3 cryptography")
    for name in ("LAYERX_KERNEL_RECEIPT_RELAY_FIXTURE", "LAYERX_KERNEL_RECEIPT_SUBMISSIONS_DIR"):
        if not os.environ.get(name) or not Path(os.environ[name]).exists():
            missing.append(name + " (dedicated genuine kernel/relay fixture)")
    for name in ("deps", "_build"):
        if not (KERNEL_ROOT / "explorer/backend" / name).is_dir():
            missing.append("prebuilt explorer/backend/" + name)
    if shutil.which("docker"):
        for image in (TOOLCHAIN_IMAGE, POSTGRES_IMAGE):
            if subprocess.run(["docker", "image", "inspect", image], stdout=subprocess.DEVNULL,
                              stderr=subprocess.DEVNULL).returncode:
                missing.append("docker image " + image)
        for tool in ("mix", "elixir", "erl"):
            probe = subprocess.run(["docker", "run", "--rm", TOOLCHAIN_IMAGE, "sh", "-c", "command -v " + tool],
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            if probe.returncode:
                missing.append(f"{tool} in {TOOLCHAIN_IMAGE}")
        probe = subprocess.run(["docker", "run", "--rm", POSTGRES_IMAGE, "sh", "-c", "command -v psql"],
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if probe.returncode:
            missing.append(f"psql in {POSTGRES_IMAGE}")
    if missing:
        raise MissingPrerequisite(", ".join(missing))


def load_relay_scenario():
    saved = sys.argv
    sys.argv = [str(KERNEL_ROOT / "tests/relay-archive/e2e.py"), str(KERNEL_BUILD)]
    try:
        spec = importlib.util.spec_from_file_location("relay_archive_e2e", KERNEL_ROOT / "tests/relay-archive/e2e.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
    finally:
        sys.argv = saved
    return module


class TamperProxy:
    """Forwards public GET routes to a real relay and, on demand, alters its real answers."""

    def __init__(self, upstream, port):
        self.upstream = upstream
        self.mode = None
        self.target_batch = None
        self.paths = []
        proxy = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def refuse(self):
                proxy.paths.append((self.command, self.path))
                self.send_response(405)
                self.end_headers()

            do_POST = do_PUT = do_DELETE = do_PATCH = refuse

            def do_GET(self):
                proxy.paths.append(("GET", self.path))
                try:
                    response = urllib.request.urlopen(proxy.upstream + self.path, timeout=10)
                except urllib.error.HTTPError as error:
                    response = error
                with response:
                    body = response.read()
                    status = response.status
                    headers = [(k, v) for k, v in response.getheaders()
                               if k.lower() not in ("content-length", "transfer-encoding", "connection")]
                body = proxy.alter(self.path, status, body)
                self.send_response(status)
                for key, value in headers:
                    self.send_header(key, value)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", port), Handler)
        self.url = f"http://127.0.0.1:{port}"
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def alter(self, path, status, body):
        if status != 200 or self.mode is None:
            return body
        route = path.split("?", 1)[0]
        if self.mode == "sequencer_key" and route == "/v1/sync/network":
            document = json.loads(body)
            document["sequencer_public_key"] = "00" * 32
            return json.dumps(document).encode()
        if self.mode == "authority_range" and route == "/v1/sync/network":
            document = json.loads(body)
            document["first_batch"] = "0"
            return json.dumps(document).encode()
        if self.mode in ("receipt_rehashed", "activity_binding") and route == f"/v1/history/batches/{self.target_batch}":
            document = json.loads(body)
            require(document["activities"], "tamper case requires a real receipt")
            activity = document["activities"][0]
            if self.mode == "receipt_rehashed":
                damaged = bytearray.fromhex(activity["receipt_hex"])
                damaged[-1] ^= 1
                activity["receipt_hex"] = damaged.hex()
                activity["receipt_sha256"] = hashlib.sha256(damaged).hexdigest()
            else:
                activity["result_code"] = activity["result_code"] ^ 1
            return json.dumps(document).encode()
        if self.mode == "signature_rehashed" and route in (
                f"/v1/history/batches/{self.target_batch}", f"/v1/sync/batches/{self.target_batch}"):
            with urllib.request.urlopen(self.upstream + f"/v1/history/batches/{self.target_batch}", timeout=10) as response:
                document = json.load(response)
            with urllib.request.urlopen(self.upstream + f"/v1/sync/batches/{self.target_batch}", timeout=10) as response:
                raw = bytearray(response.read())
            signature = bytes.fromhex(document["signature_hex"])
            offset = raw.find(signature)
            require(offset >= 0, "real batch signature is absent from canonical bytes")
            raw[offset] ^= 1
            document["signature_hex"] = bytes(raw[offset:offset + len(signature)]).hex()
            document["raw_sha256"] = hashlib.sha256(raw).hexdigest()
            return bytes(raw) if route.startswith("/v1/sync/") else json.dumps(document).encode()
        if self.mode == "receipt_digest" and route.startswith("/v1/history/batches"):
            document = json.loads(body)
            for batch in document.get("items", [document]):
                if str(batch.get("batch_number")) == str(self.target_batch):
                    for activity in batch.get("activities", []):
                        activity["receipt_sha256"] = hashlib.sha256(
                            b"tampered" + bytes.fromhex(activity["receipt_hex"])).hexdigest()
            return json.dumps(document).encode()
        if self.mode == "raw_bytes" and route == f"/v1/sync/batches/{self.target_batch}":
            damaged = bytearray(body)
            damaged[-1] ^= 1
            return bytes(damaged)
        return body

    def close(self):
        self.server.shutdown()
        self.server.server_close()


class KernelReceipts:
    def __init__(self, relay_module):
        self.relay = kernel_relay_fixture(relay_module)
        self.require = relay_module.require
        self.unused_port = relay_module.unused_port
        self.execute = relay_module.execute
        self.tests = 0
        self.proxy = None
        self.pg_port = self.unused_port()
        self.api_port = self.unused_port()
        self.api_url = f"http://127.0.0.1:{self.api_port}"
        self.log_dir = Path(os.environ.get("TASK_LOG_DIR") or self.relay.work)
        self.api_starts = 0

    def passed(self, message):
        self.tests += 1
        print("PASS " + message, flush=True)

    def docker(self, *args, check=True, timeout=1200):
        result = subprocess.run(["docker", *args], stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                timeout=timeout)
        if check and result.returncode:
            raise AssertionError(f"docker {args[0]} failed ({result.returncode}): "
                                 + result.stdout.decode(errors="replace")[-6000:])
        return result.stdout.decode(errors="replace")

    def sql(self, statement, check=True):
        result = subprocess.run(["docker", "exec", PG_CONTAINER, "psql", "-U", "postgres", "-p", str(self.pg_port),
                                 "-d", "blockscout", "-v", "ON_ERROR_STOP=1", "-At", "-c", statement],
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=60)
        if check and result.returncode:
            raise AssertionError(f"psql failed: {statement}: {result.stderr.decode(errors='replace')}")
        return result

    def scalar(self, statement):
        return self.sql(statement).stdout.decode().strip()

    def mix_env(self, relay_url, network_id, sequencer_key):
        return {
            "MIX_ENV": "dev",
            "CHAIN_TYPE": "paxeer_x",
            "DATABASE_URL": f"postgresql://postgres:postgres@127.0.0.1:{self.pg_port}/blockscout",
            "ETHEREUM_JSONRPC_VARIANT": "geth",
            "ETHEREUM_JSONRPC_HTTP_URL": f"http://127.0.0.1:{self.unused_port()}",
            "DISABLE_INDEXER": "true",
            "API_V2_ENABLED": "true",
            "PORT": str(self.api_port),
            "INDEXER_PAXEER_X_KERNEL_RECEIPTS_RELAY_URL": relay_url,
            "INDEXER_PAXEER_X_KERNEL_RECEIPTS_NETWORK_ID": str(network_id),
            "INDEXER_PAXEER_X_KERNEL_RECEIPTS_SEQUENCER_PUBLIC_KEY": sequencer_key,
            "INDEXER_PAXEER_X_KERNEL_RECEIPTS_SEQUENCER_ID": self.relay.pins["sequencer_id"],
            "INDEXER_PAXEER_X_KERNEL_RECEIPTS_FIRST_BATCH": str(self.relay.pins["sequencer_first_batch"]),
            "INDEXER_PAXEER_X_KERNEL_RECEIPTS_LAST_BATCH": str(self.relay.pins["sequencer_last_batch"]),
            "INDEXER_PAXEER_X_KERNEL_RECEIPTS_CODEC": "/kernel-build/bin/layerx-archive-codec",
            "INDEXER_PAXEER_X_KERNEL_RECEIPTS_INTERVAL_MS": "300",
        }

    def env_args(self, env):
        args = []
        for key, value in env.items():
            args += ["-e", f"{key}={value}"]
        return args

    def start_database(self):
        self.docker("rm", "-f", PG_CONTAINER, check=False)
        self.docker("run", "-d", "--name", PG_CONTAINER, "--network", "host",
                    "-e", "POSTGRES_PASSWORD=postgres", "-e", "POSTGRES_DB=blockscout",
                    POSTGRES_IMAGE, "postgres", "-p", str(self.pg_port), "-c", "listen_addresses=127.0.0.1")
        self.relay.until(lambda: self.sql("select 1", check=False).returncode == 0, "postgresql ready", 60)

    def migrate(self, env):
        script = "mix ecto.migrate --no-compile"
        output = self.docker("run", "--rm", "--network", "host", "-v", f"{KERNEL_ROOT}:/app", "-v", f"{KERNEL_BUILD}:/kernel-build:ro", "-w", "/app/explorer/backend",
                             *self.env_args(env), TOOLCHAIN_IMAGE, "sh", "-c", script)
        (self.log_dir / "task22-1-migrate.log").write_text(output)
        for table in ("lx_receipts", "lx_kernel_receipt_cursors"):
            self.require(self.scalar(f"select to_regclass('public.{table}') is not null") == "t",
                         table + " missing after migration")
        columns = set(self.scalar("select string_agg(column_name, ',') from information_schema.columns "
                                  "where table_name='lx_receipts'").split(","))
        for column in ("id", "origin", "kernel_batch_number", "kernel_batch_id", "kernel_sequence",
                       "kernel_activity_id", "kernel_result_code", "kernel_receipt_sha256",
                       "kernel_canonical_sha256", "kernel_batch_raw_sha256", "kernel_state_root",
                       "kernel_proof", "kernel_verification"):
            self.require(column in columns, "lx_receipts lacks provenance column " + column)
        self.passed("receipt provenance migration applied through the real Ecto migrator")

    def start_api(self, env):
        self.stop_api()
        self.api_starts += 1
        self.docker("run", "-d", "--name", API_CONTAINER, "--network", "host", "-v", f"{KERNEL_ROOT}:/app", "-v", f"{KERNEL_BUILD}:/kernel-build:ro",
                    "-w", "/app/explorer/backend", *self.env_args(env), TOOLCHAIN_IMAGE,
                    "sh", "-c", "mix phx.server --no-compile")
        self.relay.until(lambda: self.api_ready(), "explorer API start", 900)

    def api_ready(self):
        state = self.docker("inspect", "-f", "{{.State.Running}}", API_CONTAINER, check=False).strip()
        if state != "true":
            raise AssertionError("explorer container exited: " + self.docker("logs", "--tail", "80", API_CONTAINER,
                                                                             check=False))
        return self.api("/api/v2/paxeer-x/receipts", status=200) is not None

    def stop_api(self):
        if self.docker("ps", "-aq", "-f", f"name=^{API_CONTAINER}$", check=False).strip():
            log = self.docker("logs", API_CONTAINER, check=False)
            (self.log_dir / f"task22-1-explorer-{self.api_starts}.log").write_text(log)
            self.docker("stop", "-t", "20", API_CONTAINER, check=False)
            self.docker("rm", "-f", API_CONTAINER, check=False)

    def api(self, path, status=200):
        try:
            response = urllib.request.urlopen(self.api_url + path, timeout=10)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            body = response.read()
            self.require(response.status == status, f"{path}: expected {status}, got {response.status}: {body[:400]!r}")
        return json.loads(body) if body else {}

    def kernel_rows(self, batch=None):
        where = "origin='kernel'" + ("" if batch is None else f" and kernel_batch_number={int(batch)}")
        return int(self.scalar(f"select count(*) from lx_receipts where {where}"))

    def cursor(self):
        row = self.scalar("select coalesce(last_batch::text,'') || '|' || coalesce(refusal_code,'') || '|' || "
                          f"coalesce(refused_batch::text,'') from lx_kernel_receipt_cursors where source='{self.proxy.url}'")
        if not row:
            return None
        last, code, refused = row.split("|")
        return int(last), code or None, int(refused) if refused else None

    def submit(self, index):
        path = self.relay.submissions / f"{index}.activity"
        if not path.is_file():
            raise MissingPrerequisite(f"genuine signed activity input {index}.activity")
        activity = path.read_bytes()
        ack = self.relay.request(self.relay_url, "/v1/activities", data=activity, headers=self.relay.headers)
        self.require(ack["state"] == "acknowledged", "relay forwarding did not acknowledge")
        return ack["activity_id"]

    def head(self):
        return int(self.relay.request(self.relay_url, "/v1/sync/head")["head_batch"])

    def run(self):
        relay = self.relay
        relay.setup()
        settings = relay.relay_config("relay-data", [relay.origin1, relay.origin2])
        relay.launch_relay(settings)
        self.relay_url = settings["public_url"]
        self.submit(1)
        relay.until(lambda: self.head() >= 2, "relay batch 2")
        self.proxy = TamperProxy(self.relay_url, self.unused_port())
        network = relay.request(self.relay_url, "/v1/sync/network")
        network_id, sequencer_key = int(network["network_id"]), network["sequencer_public_key"]
        self.require(network_id == 77 and sequencer_key == relay.pins["sequencer_public_key"],
                     "relay network document disagrees with the pinned kernel")
        env = self.mix_env(self.proxy.url, network_id, sequencer_key)
        self.start_database()
        self.migrate(env)
        self.start_api(env)

        relay.until(lambda: (self.cursor() or (0,))[0] >= 2, "worker imports relay batches 1..2", 120)
        relay_batches = {n: relay.request(self.relay_url, f"/v1/history/batches/{n}") for n in (1, 2)}
        expected = {a["activity_id"]: (n, a) for n, b in relay_batches.items() for a in b["activities"]}
        self.require(expected, "relay produced no activities")
        listing = self.api("/api/v2/paxeer-x/receipts")
        items = {item["id"].lower().removeprefix("0x"): item for item in listing["items"]}
        for activity_id, (number, activity) in expected.items():
            item = items.get(activity_id.lower().removeprefix("0x"))
            self.require(item is not None, f"list route omitted kernel receipt {activity_id}")
            self.require(item["origin"] == "kernel" and item["verification"] == "sequencer_verified",
                         "kernel receipt lacks kernel origin or verified label")
            provenance = item["provenance"]["kernel"]
            self.require(str(provenance["batch_number"]) == str(number), "batch provenance wrong")
            self.require(str(provenance["sequence"]) == str(activity["sequence"]), "sequence provenance wrong")
            self.require(provenance["activity_id"].lower().removeprefix("0x") ==
                         activity_id.lower().removeprefix("0x"), "activity provenance wrong")
            self.require(provenance["receipt_sha256"].lower().removeprefix("0x") ==
                         hashlib.sha256(bytes.fromhex(activity["receipt_hex"])).hexdigest(),
                         "receipt digest provenance differs from the relay receipt bytes")
            self.require(provenance["batch_id"].lower().removeprefix("0x") ==
                         relay_batches[number]["batch_id"].lower().removeprefix("0x"), "batch id provenance wrong")
            self.require(provenance["proof"]["header_hex"] == relay_batches[number]["header_hex"] and
                         provenance["proof"]["signature_hex"] == relay_batches[number]["signature_hex"],
                         "proof provenance differs from the relay batch header/signature")
            raw = relay.request(self.relay_url, f"/v1/sync/batches/{number}", raw=True)
            self.require(provenance["batch_raw_sha256"].lower().removeprefix("0x") == hashlib.sha256(raw).hexdigest(),
                         "raw batch digest provenance differs from the canonical batch bytes")
            detail = self.api("/api/v2/paxeer-x/receipts/" + item["id"])
            self.require(detail["provenance"]["kernel"] == provenance and detail["origin"] == "kernel",
                         "detail route disagrees with list route")
            self.require(detail["transaction_hash"] is None, "kernel receipt detail carries an EVM transaction")
        self.passed("real relay receipts reach PostgreSQL and list/detail routes with batch, sequence, activity "
                    "and proof provenance")

        self.require(self.scalar("select count(*) from lx_receipts where origin='kernel' and (transaction_hash is not null "
                                 "or log_index is not null or block_hash is not null or block_number is not null or block_consensus is not null)") == "0",
                     "kernel rows carry fabricated EVM fields")
        for item in listing["items"]:
            if item["origin"] == "kernel":
                self.require("transaction_hash" not in item["provenance"]["kernel"] and "block_hash" not in item["provenance"]["kernel"],
                             "kernel provenance exposes EVM fields")
                self.require(item["block_number"] is None, "kernel receipt carries an EVM block number")
        refused = self.sql("insert into lx_receipts (receipt_id, status, origin, inserted_at, updated_at) values "
                           "(decode(repeat('ab',32),'hex'), 'unverified', 'evm', now(), now())", check=False)
        self.require(refused.returncode != 0, "database admitted an EVM receipt without EVM provenance")
        refused = self.sql("insert into lx_receipts (receipt_id, status, origin, kernel_batch_number, kernel_sequence, "
                           "kernel_activity_id, kernel_receipt_sha256, kernel_batch_raw_sha256, kernel_proof, "
                           "inserted_at, updated_at) values (decode(repeat('cd',32),'hex'), 'batch_included', 'kernel', "
                           "999, 1, decode(repeat('cd',32),'hex'), decode(repeat('cd',32),'hex'), "
                           "decode(repeat('cd',32),'hex'), '{}'::jsonb, now(), now())", check=False)
        self.require(refused.returncode != 0, "database admitted a kernel receipt without a verified label")
        proof_columns = ("receipt_id", "status", "origin", "kernel_batch_number", "kernel_sequence",
                         "kernel_activity_id", "kernel_batch_id", "kernel_result_code", "kernel_receipt_sha256",
                         "kernel_canonical_sha256", "kernel_batch_raw_sha256", "kernel_state_root", "kernel_proof",
                         "kernel_verification", "inserted_at", "updated_at")
        selected = ["999" if field == "kernel_batch_number" else
                    "NULL" if field == "kernel_verification" else field for field in proof_columns]
        refused = self.sql("insert into lx_receipts (" + ",".join(proof_columns) + ") select " +
                           ",".join(selected) + " from lx_receipts where origin='kernel' limit 1", check=False)
        self.require(refused.returncode != 0, "database admitted real provenance with a null verified label")
        self.passed("kernel rows hold no EVM fields; each provenance class is enforced by the schema")

        for mode, code in (("receipt_digest", "receipt_digest_mismatch"), ("raw_bytes", "raw_digest_mismatch"),
                           ("sequencer_key", "sequencer_key_mismatch"),
                           ("signature_rehashed", "native_verification_failed"),
                           ("receipt_rehashed", "authenticated_projection_mismatch"),
                           ("activity_binding", "authenticated_projection_mismatch"),
                           ("authority_range", "batch_unauthorized")):
            target = self.head() + 1
            self.proxy.mode, self.proxy.target_batch = mode, target
            self.submit(target)
            relay.until(lambda: self.head() >= target, f"relay batch {target}")
            relay.until(lambda: (self.cursor() or (0, None, None))[1] == code, f"worker refusal {code}", 60)
            last, _, refused_batch = self.cursor()
            self.require(last == target - 1, f"cursor advanced past refused batch under {mode}")
            self.require(code in ("sequencer_key_mismatch", "batch_unauthorized") or refused_batch == target,
                         f"refused batch not recorded under {mode}")
            self.require(self.kernel_rows(target) == 0, f"refused batch {target} acquired rows under {mode}")
            self.api(f"/api/v2/paxeer-x/receipts?id=0x{'ff' * 32}")
            self.proxy.mode = None
            relay.until(lambda: (self.cursor() or (0,))[0] >= target, f"batch {target} import after tamper ends", 60)
            self.require(self.cursor()[1] is None, "refusal not cleared after an honest batch")
            self.require(self.kernel_rows(target) >= 1, f"honest batch {target} not imported")
        self.passed("tampered receipts, signatures, authority ranges and activity bindings are refused without a verified label")

        disallowed = [(method, path) for method, path in self.proxy.paths
                      if method != "GET" or not path.split("?", 1)[0].startswith(PUBLIC_ROUTES)]
        self.require(not disallowed, f"worker reached non-public relay routes: {disallowed[:5]}")
        self.passed("worker reads only the declared public relay GET routes")

        before = self.kernel_rows()
        last_before = self.cursor()[0]
        self.stop_api()
        target = self.head() + 1
        self.submit(target)
        relay.until(lambda: self.head() >= target, f"relay batch {target} while explorer stopped")
        self.start_api(env)
        relay.until(lambda: (self.cursor() or (0,))[0] >= target, "resume after restart", 120)
        self.require(self.kernel_rows() > before, "restart did not resume ingestion")
        self.require(self.scalar("select count(*) from (select kernel_batch_number, kernel_sequence from lx_receipts "
                                 "where origin='kernel' group by 1,2 having count(*) > 1) d") == "0",
                     "restart duplicated receipts")
        self.require(self.cursor()[0] > last_before, "cursor did not persist across restart")
        self.passed("durable ingestion resumes after restart from the persisted cursor")

        total = self.kernel_rows()
        self.stop_api()
        self.sql(f"update lx_kernel_receipt_cursors set last_batch=0, next_cursor=null where source='{self.proxy.url}'")
        self.start_api(env)
        relay.until(lambda: (self.cursor() or (0,))[0] >= target, "replay from batch 1", 120)
        self.require(self.kernel_rows() == total, "replay changed the receipt projection")
        self.passed("replay from the relay boundary deduplicates")

        snapshot = self.scalar("select string_agg(encode(kernel_activity_id,'hex') || ':' || kernel_batch_number || ':' || "
                               "kernel_sequence || ':' || encode(kernel_receipt_sha256,'hex'), ',' order by "
                               "kernel_batch_number, kernel_sequence) from lx_receipts where origin='kernel'")
        self.stop_api()
        self.sql("delete from lx_receipts where origin='kernel'")
        self.sql(f"delete from lx_kernel_receipt_cursors where source='{self.proxy.url}'")
        self.start_api(env)
        relay.until(lambda: (self.cursor() or (0,))[0] >= target, "rebuild from relay boundary", 180)
        rebuilt = self.scalar("select string_agg(encode(kernel_activity_id,'hex') || ':' || kernel_batch_number || ':' || "
                              "kernel_sequence || ':' || encode(kernel_receipt_sha256,'hex'), ',' order by "
                              "kernel_batch_number, kernel_sequence) from lx_receipts where origin='kernel'")
        self.require(rebuilt == snapshot, "rebuilt projection differs from the original")
        self.passed("projection rebuilds identically from the public relay boundary")

        self.api("/api/v2/paxeer-x/receipts/0x" + "ee" * 32, status=404)
        self.api_status("/api/v2/paxeer-x/receipts/not-a-receipt")
        for item in self.api("/api/v2/paxeer-x/receipts")["items"]:
            for private in ("email", "profile", "user", "canonical_hex", "receipt_hex", "data_dir", "token"):
                self.require(private not in item and private not in item.get("provenance", {}) and
                             private not in item.get("provenance", {}).get("kernel", {}) and
                             private not in item.get("provenance", {}).get("evm", {}),
                             f"receipt route exposes {private}")
        self.passed("unknown receipts answer 404 and routes expose no private node or profile data")

    def api_status(self, path):
        try:
            with urllib.request.urlopen(self.api_url + path, timeout=10) as response:
                status = response.status
        except urllib.error.HTTPError as error:
            status = error.code
        self.require(status in (400, 404, 422), f"{path}: malformed id answered {status}")
        return status

    def close(self):
        try:
            self.stop_api()
        finally:
            if self.proxy:
                self.proxy.close()
            log = self.docker("logs", PG_CONTAINER, check=False)
            (self.log_dir / "task22-1-postgres.log").write_text(log)
            self.docker("rm", "-f", PG_CONTAINER, check=False)
            self.relay.close()


def kernel_receipts():
    prerequisites()
    case = KernelReceipts(load_relay_scenario())
    try:
        case.run()
    finally:
        case.close()
    return case.tests


# --- case: standalone-receipts (req.195) -------------------------------------
# Self-contained section for tools/explorer/tests/paxeer-x-data-boundary.py.
# Requires only the stdlib. Register with:
#     CASES["standalone-receipts"] = case_standalone_receipts
# The function returns (tests, failures); the dispatcher prints the gate line.

import json
import os
import signal
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

SR_BINARY_NAME = "layerx-explorer-index"
SR_READINESS_PATH = "/v1/readiness"
SR_STARTUP_SECONDS = 120
SR_CONVERGE_SECONDS = 600
SR_SHUTDOWN_SECONDS = 15
SR_UNAVAILABLE_OBSERVE_SECONDS = 30
SR_POLL_SECONDS = 2

# Every variable the binary's config() reads; the case never synthesises them.
SR_BINARY_ENV = (
    "LAYERX_EXPLORER_PROGRAM_LISTEN",
    "LAYERX_EXPLORER_PROGRAM_BEARER_TOKEN",
    "LAYERX_EXPLORER_NODE_BEARER_TOKEN",
    "LAYERX_EXPLORER_AUTHORITY_BEARER_TOKEN",
    "LAYERX_EXPLORER_PROGRAM_MAX_STALENESS_MS",
    "LAYERX_EXPLORER_NODE_ENDPOINT",
    "LAYERX_EXPLORER_AUTHORITY_ENDPOINT",
    "LAYERX_EXPLORER_AUTHORITY_CA_DER",
    "LAYERX_EXPLORER_AUTHORITY_REPLICA_ID",
    "LAYERX_EXPLORER_SEQUENCER_TRUST_HISTORY",
    "LAYERX_EXPLORER_DEPLOYMENT_JOURNAL",
    "LAYERX_EXPLORER_VERIFIED_SOURCE_STORE",
    "LAYERX_EXPLORER_PROGRAM_PROBE_ID",
    "LAYERX_EXPLORER_GENESIS_TRUST",
    "LAYERX_EXPLORER_FINALITY_POLICY",
    "LAYERX_EXPLORER_READ_KEY_FILE",
    "LAYERX_EXPLORER_READ_ENDPOINT",
    "LAYERX_EXPLORER_READ_CA_DER",
    "LAYERX_EXPLORER_READ_SEQUENCER_PUBLIC_KEY_FILE",
    "LAYERX_EXPLORER_READ_NETWORK_ID",
    "LAYERX_EXPLORER_READ_FEE_LIMIT",
    "LAYERX_EXPLORER_NAMING_PROGRAM",
    "LAYERX_NETWORK_GATEWAY_ENDPOINT",
    "LAYERX_EXPLORER_LNI_SOCKET",
    "LAYERX_EXPLORER_NETWORK_ID",
    "LAYERX_EXPLORER_PROTOCOL_VERSION",
    "LAYERX_EXPLORER_INGEST_INTERVAL_MS",
)
SR_CURSOR_ENV = "LAYERX_EXPLORER_INGEST_CURSOR"
SR_CURSOR_MAGIC = "layerx-explorer-cursor/v1"
# Real native topology inputs exported by the kernel fixture: a command that
# submits one real signed transfer in the given asset and exits 0 once it is
# sealed, and at least two asset ids (hex) it can send in.
SR_SEND_ENV = "PAXEER_X_LAYERX_SEND_CMD"
SR_ASSETS_ENV = "PAXEER_X_LAYERX_ASSETS"
# Case inputs: a real account with receipt-backed LayerX activity on the
# followed network, and optionally the node's private data directory whose
# files the explorer must never open.
SR_ACCOUNT_ENV = "PAXEER_X_STANDALONE_RECEIPTS_ACCOUNT"
SR_NODE_PRIVATE_ENV = "PAXEER_X_NODE_PRIVATE_DIR"
SR_SYSTEM_PREFIXES = ("/usr/", "/lib", "/etc/", "/dev/", "/proc/", "/sys/", "/run/", "/nix/")


class SrCheck:
    def __init__(self):
        self.tests = 0
        self.failures = []

    def check(self, name, condition, detail=""):
        self.tests += 1
        if condition:
            print(f"ok standalone-receipts {name}", flush=True)
        else:
            self.failures.append(name)
            print(f"FAIL standalone-receipts {name}: {detail}", flush=True)
        return condition


def sr_binary():
    explicit = os.environ.get("LAYERX_EXPLORER_INDEX_BIN")
    if explicit:
        return explicit
    target = os.environ.get("CARGO_TARGET_DIR")
    if not target:
        return None
    for profile in ("release", "debug"):
        candidate = os.path.join(target, profile, SR_BINARY_NAME)
        if os.path.isfile(candidate):
            return candidate
    return None


def sr_prerequisites():
    missing = [name for name in SR_BINARY_ENV + (SR_ACCOUNT_ENV, SR_SEND_ENV, SR_ASSETS_ENV)
               if not os.environ.get(name)]
    if os.environ.get(SR_ASSETS_ENV) and len(sr_assets()) < 2:
        missing.append(f"{SR_ASSETS_ENV} with at least two assets")
    socket_path = os.environ.get("LAYERX_EXPLORER_LNI_SOCKET")
    if socket_path and not sr_lni_listening(socket_path):
        missing.append(f"listening LNI socket {socket_path}")
    binary = sr_binary()
    if binary is None or not os.access(binary, os.X_OK):
        missing.append(f"executable {SR_BINARY_NAME} (LAYERX_EXPLORER_INDEX_BIN or CARGO_TARGET_DIR)")
    return binary, missing


def sr_get(base, path, bearer, timeout=10):
    request = urllib.request.Request(base.rstrip("/") + path, method="GET")
    request.add_header("Authorization", f"Bearer {bearer}")
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            return response.status, response.read()
    except urllib.error.HTTPError as error:
        return error.code, error.read()


def sr_json(status_body):
    status, body = status_body
    try:
        return status, json.loads(body)
    except ValueError:
        return status, None


def sr_assets():
    return [item.strip() for item in os.environ.get(SR_ASSETS_ENV, "").split(",") if item.strip()]


def sr_lni_listening(path):
    try:
        with socket.socket(socket.AF_UNIX) as connection:
            connection.settimeout(5)
            connection.connect(path)
        return True
    except OSError:
        return False


def sr_send(asset, log_path):
    with open(log_path, "ab") as log:
        return subprocess.run([os.environ[SR_SEND_ENV], asset], stdout=log, stderr=subprocess.STDOUT,
                              timeout=120).returncode


def sr_explorer_base():
    return "http://" + os.environ["LAYERX_EXPLORER_PROGRAM_LISTEN"]


def sr_explorer(path):
    return sr_json(sr_get(sr_explorer_base(), path,
                          os.environ["LAYERX_EXPLORER_PROGRAM_BEARER_TOKEN"]))


def sr_closed_loopback_endpoint():
    """A real loopback endpoint with no listener: the authority is down."""
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as probe:
        probe.bind(("127.0.0.1", 0))
        port = probe.getsockname()[1]
    return f"https://127.0.0.1:{port}"


def sr_start(binary, state_dir, log_path, authority_endpoint=None):
    env = dict(os.environ)
    env[SR_CURSOR_ENV] = os.path.join(state_dir, "ingest.cursor")
    env["LAYERX_EXPLORER_VERIFIED_SOURCE_STORE"] = os.path.join(state_dir, "verified-sources")
    if authority_endpoint is not None:
        env["LAYERX_EXPLORER_AUTHORITY_ENDPOINT"] = authority_endpoint
    for name in (SR_SEND_ENV, SR_ASSETS_ENV, SR_ACCOUNT_ENV):
        env.pop(name, None)
    log = open(log_path, "ab")
    process = subprocess.Popen([binary], env=env, stdout=log, stderr=log, start_new_session=True)
    log.close()
    deadline = time.monotonic() + SR_STARTUP_SECONDS
    while time.monotonic() < deadline:
        if process.poll() is not None:
            return process, False
        try:
            status, _ = sr_explorer("/healthz")
            if status in (200, 503):
                return process, True
        except OSError:
            pass
        time.sleep(SR_POLL_SECONDS)
    return process, False


def sr_stop(process):
    """SIGTERM keeps its default disposition: the process ends by that signal."""
    if process.poll() is not None:
        return process.returncode, 0.0
    started = time.monotonic()
    process.send_signal(signal.SIGTERM)
    try:
        code = process.wait(timeout=SR_SHUTDOWN_SECONDS)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGKILL)
        process.wait()
        return None, time.monotonic() - started
    return code, time.monotonic() - started


def sr_stopped_by_term(code, took):
    return code == -signal.SIGTERM and took <= SR_SHUTDOWN_SECONDS


def sr_readiness():
    status, doc = sr_explorer(SR_READINESS_PATH)
    return status, doc if isinstance(doc, dict) else None


def sr_readiness_consistent(doc):
    """complete <=> the source head is nonzero and every batch 1..=head is verified."""
    try:
        head = int(doc["source_sealed_batch"])
        through = int(doc["indexed_through"])
        ranges = [(int(a), int(b)) for a, b in doc["incomplete_ranges"]]
        complete = doc["complete"]
        source_available = doc["source_available"]
        int(doc["source_chain_sequence"])
    except (KeyError, TypeError, ValueError):
        return False
    if not isinstance(complete, bool) or not isinstance(source_available, bool) or through > head:
        return False
    if any(a > b or a < 1 or b > head for a, b in ranges):
        return False
    if through < head and (not ranges or ranges[0][0] != through + 1):
        return False
    return complete == (source_available and head > 0 and not ranges and through == head)


def sr_activity(account):
    status, doc = sr_explorer(f"/v1/accounts/{urllib.request.quote(account, safe='')}/unified?limit=100")
    if not isinstance(doc, dict):
        return status, None, None
    items = (doc.get("layerx_activity") or {}).get("items")
    return status, doc, items if isinstance(items, list) else None


def sr_row_key(item):
    return json.dumps({key: item.get(key) for key in sorted(item) if key != "verification"},
                      sort_keys=True)


def sr_open_files(pid):
    paths = []
    fd_dir = f"/proc/{pid}/fd"
    try:
        entries = os.listdir(fd_dir)
    except OSError:
        return paths
    for entry in entries:
        try:
            target = os.readlink(os.path.join(fd_dir, entry))
        except OSError:
            continue
        if target.startswith("/"):
            paths.append(target)
    return paths


def sr_private_reads(pid, allowed_roots):
    private = os.environ.get(SR_NODE_PRIVATE_ENV)
    offenders = []
    for path in sr_open_files(pid):
        if private and path.startswith(os.path.realpath(private)):
            offenders.append(path)
        elif not path.startswith(SR_SYSTEM_PREFIXES) and not any(
                path.startswith(root) for root in allowed_roots):
            offenders.append(path)
    return offenders


def sr_watch(process, allowed_roots, seconds, until):
    """Samples readiness until `until(doc)` or timeout; returns (last, consistent, offenders)."""
    consistent = True
    offenders = []
    last = None
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline and process.poll() is None:
        status, doc = sr_readiness()
        offenders += sr_private_reads(process.pid, allowed_roots)
        if status != 200 or doc is None or not sr_readiness_consistent(doc):
            consistent = False
        elif doc is not None:
            last = doc
            if until(doc):
                break
        time.sleep(SR_POLL_SECONDS)
    return last, consistent, sorted(set(offenders))


def sr_ingest_log(log_path, offset=0):
    verified, incomplete = set(), {}
    with open(log_path, "rb") as log:
        log.seek(offset)
        for raw in log:
            line = raw.decode("utf-8", "replace").strip()
            if not line.startswith("explorer-ingest batch="):
                continue
            fields = dict(part.split("=", 1) for part in line.split()[1:] if "=" in part)
            batch = int(fields.get("batch", "0"))
            outcome = fields.get("outcome", "")
            if outcome == "verified":
                verified.add(batch)
            elif outcome.startswith("incomplete:"):
                incomplete[batch] = outcome
    return verified, incomplete


def sr_cursor(state_dir):
    try:
        with open(os.path.join(state_dir, "ingest.cursor"), encoding="ascii") as cursor:
            parts = cursor.read().split()
    except OSError:
        return None
    if len(parts) != 3 or parts[0] != SR_CURSOR_MAGIC:
        return None
    return int(parts[1]), int(parts[2])


def case_standalone_receipts(args=None):
    check = SrCheck()
    binary, missing = sr_prerequisites()
    if not check.check("prerequisites", not missing, "missing " + ", ".join(missing)):
        return check.tests, check.failures
    account = os.environ[SR_ACCOUNT_ENV]
    assets = sr_assets()
    probe = os.environ["LAYERX_EXPLORER_PROGRAM_PROBE_ID"]
    evidence = os.environ.get("PAXEER_X_EVIDENCE_DIR") or tempfile.gettempdir()
    work = tempfile.mkdtemp(prefix="standalone-receipts-", dir=evidence)
    os.chmod(work, 0o700)
    log_path = os.path.join(work, "explorer.log")
    send_log = os.path.join(work, "send.log")
    state = os.path.join(work, "state")
    os.mkdir(state, 0o700)
    allowed = [os.path.realpath(p) for p in (
        work,
        os.environ["LAYERX_EXPLORER_DEPLOYMENT_JOURNAL"],
        os.environ["LAYERX_EXPLORER_SEQUENCER_TRUST_HISTORY"],
        os.environ["LAYERX_EXPLORER_GENESIS_TRUST"],
        os.environ["LAYERX_EXPLORER_FINALITY_POLICY"],
        os.environ["LAYERX_EXPLORER_AUTHORITY_CA_DER"],
        os.environ["LAYERX_EXPLORER_READ_CA_DER"],
        os.environ["LAYERX_EXPLORER_READ_KEY_FILE"],
        os.environ["LAYERX_EXPLORER_READ_SEQUENCER_PUBLIC_KEY_FILE"],
        binary,
    )]
    complete = lambda doc: doc.get("complete") is True
    process = None
    try:
        # 1) Fresh projection that cannot verify anything yet (authority not
        #    reachable): never complete, account refuses. Deterministic: no
        #    batch can become verified while this phase runs.
        process, up = sr_start(binary, state, log_path, sr_closed_loopback_endpoint())
        if not check.check("start.fresh", up, f"see {log_path}"):
            return check.tests, check.failures
        status, first = sr_readiness()
        check.check("1.fresh_readiness_not_complete",
                    status == 200 and first is not None and sr_readiness_consistent(first)
                    and first["complete"] is False, f"{status} {json.dumps(first)}")
        status, doc, _ = sr_activity(account)
        check.check("1.account_incomplete_from_head",
                    status == 503 and isinstance(doc, dict) and doc.get("error") == "incomplete_from_head"
                    and "source_sealed_batch" in doc and "indexed_through" in doc,
                    f"{status} {json.dumps(doc)}")

        code, took = sr_stop(process)
        check.check("1.terminates_on_sigterm", sr_stopped_by_term(code, took), f"{code} after {took:.1f}s")
        cursor_1 = sr_cursor(state)
        check.check("1.no_cursor_past_unverified", cursor_1 is None or cursor_1[0] == 0, f"{cursor_1}")

        # 2) Real authority; real signed sends in two assets; convergence to head.
        process, up = sr_start(binary, state, log_path)
        if not check.check("start.with_authority", up, f"see {log_path}"):
            return check.tests, check.failures
        sent = [asset for asset in assets[:2] if sr_send(asset, send_log) == 0]
        check.check("2.real_sends_sealed", len(sent) == 2, f"sent {sent}; see {send_log}")
        ready, consistent, offenders = sr_watch(process, allowed, SR_CONVERGE_SECONDS, complete)
        check.check("2.ingestion_alive", process.poll() is None, f"exited {process.returncode}")
        check.check("2.readiness_consistent_throughout", consistent, json.dumps(ready))
        check.check("2.readiness_complete_at_head", ready is not None and complete(ready),
                    json.dumps(ready))
        verified, _ = sr_ingest_log(log_path)
        head_a = int(ready["source_sealed_batch"]) if ready else 0
        check.check("2.every_batch_verified_through_authority",
                    head_a > 0 and set(range(1, head_a + 1)) <= verified,
                    f"verified {sorted(verified)} head {head_a}")
        status, _, rows_a = sr_activity(account)
        rows_a = rows_a or []
        check.check("2.account_rows_present_with_verification",
                    status == 200 and rows_a and all(row.get("verification") for row in rows_a),
                    f"{status} {len(rows_a)} rows")
        keys_a = [sr_row_key(row) for row in rows_a]
        check.check("2.no_duplicate_rows", len(keys_a) == len(set(keys_a)), "duplicate rows")
        status, _ = sr_explorer(f"/v1/programs/{probe}")
        check.check("2.program_refresh_coexists", status == 200, str(status))
        check.check("2.no_node_internal_or_private_reads", not offenders, ", ".join(offenders))
        cursor_a = sr_cursor(state)
        check.check("2.durable_cursor_written", cursor_a is not None and cursor_a[0] == head_a,
                    f"{cursor_a} head {head_a}")

        # 4) SIGTERM, restart on the same cursor: identical rows, no duplicates.
        code, took = sr_stop(process)
        check.check("4.terminates_on_sigterm", sr_stopped_by_term(code, took), f"{code} after {took:.1f}s")
        process, up = sr_start(binary, state, log_path)
        if check.check("start.restart", up, f"see {log_path}"):
            ready_b, consistent, offenders = sr_watch(
                process, allowed, SR_CONVERGE_SECONDS,
                lambda doc: complete(doc) and int(doc["indexed_through"]) >= head_a)
            check.check("4.restart_readiness_consistent", consistent, json.dumps(ready_b))
            check.check("4.restart_converges",
                        ready_b is not None and complete(ready_b) and int(ready_b["indexed_through"]) >= head_a,
                        json.dumps(ready_b))
            status, _, rows_b = sr_activity(account)
            keys_b = [sr_row_key(row) for row in rows_b or []]
            check.check("4.identical_rows_after_restart", status == 200 and keys_b == keys_a,
                        f"{len(keys_a)} before, {len(keys_b)} after")
            check.check("4.no_duplicate_rows_after_restart", len(keys_b) == len(set(keys_b)),
                        "duplicate rows")
            check.check("4.no_node_internal_or_private_reads", not offenders, ", ".join(offenders))

        # 3) Authority unavailable: a new sealed batch stays incomplete, zero new rows.
        code, took = sr_stop(process)
        check.check("3.terminates_on_sigterm", sr_stopped_by_term(code, took), f"{code} after {took:.1f}s")
        offset = os.path.getsize(log_path)
        process, up = sr_start(binary, state, log_path, sr_closed_loopback_endpoint())
        if check.check("start.authority_unavailable", up, f"see {log_path}"):
            sent = sr_send(assets[0], send_log)
            check.check("3.real_send_sealed", sent == 0, f"exit {sent}; see {send_log}")
            last, consistent, offenders = sr_watch(
                process, allowed, SR_UNAVAILABLE_OBSERVE_SECONDS, lambda doc: False)
            check.check("3.bounded_retry_keeps_serving", process.poll() is None,
                        f"exited {process.returncode}")
            check.check("3.readiness_consistent", consistent, json.dumps(last))
            check.check("3.new_batch_in_incomplete_ranges",
                        last is not None and int(last["source_sealed_batch"]) > head_a
                        and last["complete"] is False
                        and any(a <= head_a + 1 <= b for a, b in last["incomplete_ranges"]),
                        json.dumps(last))
            status, doc, rows_c = sr_activity(account)
            if status == 200:
                keys_c = [sr_row_key(row) for row in rows_c or []]
                new_rows = sorted(set(keys_c) - set(keys_a))
                check.check("3.zero_new_rows_without_authority", not new_rows, f"{len(new_rows)} new rows")
            else:
                check.check("3.zero_new_rows_without_authority",
                            status == 503 and isinstance(doc, dict)
                            and doc.get("error") == "incomplete_from_head", f"{status} {json.dumps(doc)}")
            verified_c, incomplete = sr_ingest_log(log_path, offset)
            first_gap = last["incomplete_ranges"][0][0] if last and last["incomplete_ranges"] else None
            check.check("3.unverified_batch_logged_incomplete",
                        first_gap in incomplete and not verified_c,
                        f"gap {first_gap} incomplete {sorted(incomplete)} verified {sorted(verified_c)}")
            status, _ = sr_explorer(f"/v1/programs/{probe}")
            check.check("3.program_refresh_independent_of_authority", status == 200, str(status))
            check.check("3.no_node_internal_or_private_reads", not offenders, ", ".join(offenders))
            code, took = sr_stop(process)
            check.check("3.terminates_on_sigterm_during_retry", sr_stopped_by_term(code, took),
                        f"{code} after {took:.1f}s")
    finally:
        if process is not None and process.poll() is None:
            sr_stop(process)
    print(f"standalone-receipts evidence {work}", flush=True)
    return check.tests, check.failures



def history_readiness():
    import re
    import sqlite3
    import socket
    import urllib.parse

    repo = Path(__file__).resolve().parents[3]
    build = Path(os.environ.get("LAYERX_BUILD_DIR", repo / "build")).resolve()
    binaries = {}
    for key, name in (("LAYERX_INDEXER_BIN", "layerx-indexer"),
                      ("LAYERX_GATEWAY_BIN", "layerx-gateway")):
        path = Path(os.environ.get(key, repo / "platform/target/debug" / name)).resolve()
        if not path.is_file() or not os.access(path, os.X_OK):
            raise MissingPrerequisite(f"{key}: prebuilt {name} required")
        binaries[name] = path
    for name in ("bin/layerxd", "bin/layerx-archive-codec"):
        if not (build / name).is_file():
            raise MissingPrerequisite(f"prebuilt {name} required")
    for tool in ("openssl", "redis-server"):
        if shutil.which(tool) is None:
            raise MissingPrerequisite(tool)
    if importlib.util.find_spec("cryptography") is None:
        raise MissingPrerequisite("Python cryptography required")
    fixture_path = os.environ.get("LAYERX_HISTORY_RELAY_FIXTURE_CONFIG")
    if not fixture_path or not Path(fixture_path).is_file():
        raise MissingPrerequisite("LAYERX_HISTORY_RELAY_FIXTURE_CONFIG: dedicated real relay trust pins and idle loopback upstreams required")
    fixture = json.loads(Path(fixture_path).read_text())
    for field in ("network_id", "genesis_sha256", "sequencer_id", "sequencer_public_key", "sequencer_first_batch"):
        require(field in fixture, "real relay fixture is missing " + field)
    upstreams = fixture.get("upstreams")
    require(isinstance(upstreams, list) and upstreams, "real relay fixture has no upstreams")
    for origin in upstreams:
        parsed = urllib.parse.urlparse(origin)
        require(parsed.scheme in ("http", "https") and parsed.hostname in ("localhost", "127.0.0.1", "::1")
                and parsed.username is None and parsed.password is None,
                "history-readiness requires isolated loopback relay upstreams")
    saved = sys.argv
    sys.argv = [str(repo / "tests/relay-archive/e2e.py"), str(build)]
    try:
        spec = importlib.util.spec_from_file_location("history_relay_e2e", repo / "tests/relay-archive/e2e.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
    finally:
        sys.argv = saved
    relay = module.Scenario()
    checks = 0
    reservations = []
    try:
        relay.work.chmod(0o700)
        relay.bin = build / "bin"
        relay.runtime = repo / "platform/relay_archive"
        relay_port = module.unused_port()
        allowed = ("network_id", "genesis_sha256", "sequencer_id", "sequencer_public_key",
                   "sequencer_first_batch", "sequencer_last_batch", "upstreams", "ca_file")
        settings = {key: value for key, value in fixture.items() if key in allowed}
        if settings.get("ca_file"):
            settings["ca_file"] = str((Path(fixture_path).resolve().parent / settings["ca_file"]).resolve())
        settings.update(data_dir=str(relay.work / "history-relay-data"),
                        listen=f"127.0.0.1:{relay_port}", public_url=f"http://127.0.0.1:{relay_port}",
                        codec=str(relay.bin / "layerx-archive-codec"), allow_loopback_dev=True,
                        poll_interval_seconds=0.1)
        config_path = relay.work / "history-relay.json"
        module.write_json(config_path, settings)

        def start_relay(name):
            process = relay.start(name, [relay.bin / "layerxd", "--relay-archive", config_path],
                                  env=dict(os.environ, LAYERX_RELAY_ARCHIVE_RUNTIME=str(relay.runtime / "runtime.py")))
            relay.until(lambda: relay.request(settings["public_url"], "/v1/sync/head").get("head_batch"),
                        "real relay source head", 90)
            return process

        source = start_relay("history-relay")
        module.execute(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
                        "-keyout", relay.work / "tls.key", "-out", relay.work / "tls.crt",
                        "-days", "1", "-subj", "/CN=localhost",
                        "-addext", "subjectAltName=IP:127.0.0.1,DNS:localhost"])
        (relay.work / "tls.key").chmod(0o600)
        for _ in range(3):
            sock = socket.socket()
            sock.bind(("127.0.0.1", 0))
            reservations.append(sock)
        indexer_port, gateway_port, redis_port = [s.getsockname()[1] for s in reservations]
        indexer_url = f"http://127.0.0.1:{indexer_port}"
        gateway_url = f"http://127.0.0.1:{gateway_port}"
        env = {key: value for key, value in os.environ.items()
               if not key.startswith(("LAYERX_INDEXER_", "LAYERX_GATEWAY_", "LAYERX_EVENTS_"))}
        database = relay.work / "history.sqlite"
        indexer_env = dict(env, LAYERX_INDEXER_DB=str(database),
                           LAYERX_INDEXER_LISTEN=f"127.0.0.1:{indexer_port}",
                           LAYERX_INDEXER_RELAY_URL=settings["public_url"],
                           LAYERX_INDEXER_POLL_MS="100", LAYERX_INDEXER_TIMEOUT_MS="400",
                           LAYERX_INDEXER_STALL_SECS="5", LAYERX_INDEXER_MAX_UNITS_PER_STEP="1")
        reservations[0].close()
        indexer = relay.start("history-indexer", [binaries["layerx-indexer"]], env=indexer_env)
        ca = relay.work / "gateway-ca.der"
        module.execute(["openssl", "x509", "-in", relay.work / "tls.crt", "-outform", "DER", "-out", ca])
        username = relay.work / "redis-username"
        password = relay.work / "redis-password"
        username.write_text("default")
        password.write_text(os.urandom(24).hex())
        username.chmod(0o600)
        password.chmod(0o600)
        redis_config = relay.work / "redis.conf"
        redis_config.write_text(f"bind 127.0.0.1\nport 0\ntls-port {redis_port}\n"
                               f"tls-cert-file {relay.work / 'tls.crt'}\n"
                               f"tls-key-file {relay.work / 'tls.key'}\n"
                               f"tls-ca-cert-file {relay.work / 'tls.crt'}\n"
                               "tls-auth-clients no\nsave \"\"\nappendonly no\n"
                               f"requirepass {password.read_text()}\n")
        redis_config.chmod(0o600)
        reservations[2].close()
        redis = relay.start("history-redis", ["redis-server", redis_config])
        protocol_source = (repo / "agent/crates/layerx-wire/src/limits.rs").read_text()
        protocol = re.search(r"STATE_COMMITMENT_PROTOCOL_VERSION: u16 = (\d+);", protocol_source)
        require(protocol is not None, "current wire protocol is missing")
        gateway_env = dict(env, LAYERX_GATEWAY_LISTEN=f"127.0.0.1:{gateway_port}",
                           LAYERX_GATEWAY_LISTENER="plain", LAYERX_GATEWAY_OUTBOUND_CA_DER=str(ca),
                           LAYERX_GATEWAY_REDIS_URL=f"rediss://localhost:{redis_port}",
                           LAYERX_GATEWAY_REDIS_USERNAME_FILE=str(username),
                           LAYERX_GATEWAY_REDIS_PASSWORD_FILE=str(password),
                           LAYERX_GATEWAY_NETWORK_ID="history-readiness",
                           LAYERX_GATEWAY_PROTOCOL_NETWORK_ID=str(fixture["network_id"]),
                           LAYERX_GATEWAY_LXP_WIRE_VERSION=protocol.group(1),
                           LAYERX_GATEWAY_INDEXER_URL=indexer_url)
        reservations[1].close()
        gateway = relay.start("history-gateway", [binaries["layerx-gateway"]], env=gateway_env)

        def get(base, path):
            for child in (indexer, gateway, redis):
                require(child.poll() is None, f"required child exited: {child.returncode}; {relay.work}")
            try:
                response = urllib.request.urlopen(base + path, timeout=5)
            except urllib.error.HTTPError as error:
                response = error
            with response:
                return response.status, json.loads(response.read())

        def ready(state):
            status, doc = get(indexer_url, "/readyz")
            require(doc.get("version") == 1 and doc.get("database") == "ok", "readiness lacks database liveness")
            sources = doc.get("sources")
            require(isinstance(sources, list) and len(sources) == 1 and sources[0]["source"] == "layerx",
                    "readiness omitted required LayerX source")
            if doc["status"] != state:
                return None
            require(status == (200 if state == "ready" else 503), "readiness HTTP status contradicts document")
            return sources[0]

        def gateway_state(state):
            status, doc = get(gateway_url, "/v1/status")
            return status == 200 and doc.get("services", {}).get("indexer") == state

        first = relay.until(lambda: ready("ready"), "fresh source ready")
        head = module.Scenario.request(relay, settings["public_url"], "/v1/sync/head")["head_batch"]
        require(first["reconciled"] and first["source_head"] == int(head)
                and first["indexed_cursor"] == int(head), "source head and persisted cursor differ")
        relay.until(lambda: gateway_state("available"), "gateway preserves available readiness")
        checks += 1
        idle = relay.until(lambda: (s if (s := ready("ready")) and
                                    s["last_success_at"] > first["last_success_at"] else None), "idle head observed")
        require(idle["indexed_cursor"] == first["indexed_cursor"] and idle["source_head"] == first["source_head"],
                "idle proof unexpectedly advanced the source")
        checks += 1
        with sqlite3.connect(database) as connection:
            before = connection.execute("SELECT chain, position, hash FROM cursors ORDER BY chain").fetchall()
            rows_before = connection.execute("SELECT count(*) FROM transfers").fetchone()[0]
            require(rows_before > 0, "real relay fixture requires at least one transfer receipt")
            events_before = connection.execute("SELECT count(*) FROM events").fetchone()[0]
            require(events_before > 0, "real relay produced no indexed receipt rows")
        relay.stop(source)
        failed = relay.until(lambda: (s if (s := ready("degraded")) and s["consecutive_failures"] >= 3 else None),
                             "repeated source failure")
        require(failed["last_error"] == "source_unavailable", "missing typed source failure")
        relay.until(lambda: gateway_state("degraded"), "gateway preserves degraded readiness")
        require(get(indexer_url, "/healthz")[0] == 200, "database liveness fell with disconnected source")
        checks += 1
        stalled = relay.until(lambda: ready("unavailable"), "stalled source unavailable")
        require(stalled["state"] == "stalled" and stalled["freshness_secs"] > 5,
                "stalled source lacks stale freshness")
        relay.until(lambda: gateway_state("unavailable"), "gateway preserves stalled readiness")
        checks += 1
        with sqlite3.connect(database) as connection:
            account = connection.execute("SELECT account FROM transfers ORDER BY id LIMIT 1").fetchone()
        require(account is not None, "real relay fixture has no history account")
        account = account[0]
        status, page = get(indexer_url, "/v1/history/" + account)
        require(status == 200 and page["items"] and page["freshness"]["status"] == "unavailable",
                "stale rows lack source freshness")
        checks += 1
        relay.stop(indexer)
        indexer = relay.start("history-indexer-restarted", [binaries["layerx-indexer"]], env=indexer_env)
        starting = relay.until(lambda: ready("unavailable"), "restart refuses persisted-only readiness")
        require(starting["state"] == "starting" and not starting["reconciled"]
                and starting["indexed_cursor"] == first["indexed_cursor"]
                and starting["last_success_at"] == stalled["last_success_at"],
                "restart reused persisted observation as fresh authority")
        require(get(indexer_url, "/healthz")[0] == 200, "restarted database is not live")
        with sqlite3.connect(database) as connection:
            require(connection.execute("SELECT chain, position, hash FROM cursors ORDER BY chain").fetchall() == before,
                    "restart changed persisted cursor")
            require(connection.execute("SELECT count(*) FROM transfers").fetchone()[0] == rows_before,
                    "disconnected restart changed persisted rows")
            require(connection.execute("SELECT count(*) FROM events").fetchone()[0] == events_before,
                    "disconnected restart changed indexed receipt rows")
        checks += 1
        source = start_relay("history-relay-restarted")
        recovered = relay.until(lambda: ready("ready"), "fresh source reconciles restart")
        require(recovered["reconciled"] and recovered["consecutive_failures"] == 0
                and recovered["source_head"] == first["source_head"]
                and recovered["last_success_at"] > starting["last_success_at"], "reconnect was not freshly observed")
        relay.until(lambda: gateway_state("available"), "gateway recovers after fresh observation")
        checks += 1
        relay.stop(indexer)
        empty_env = {key: value for key, value in indexer_env.items() if key != "LAYERX_INDEXER_RELAY_URL"}
        indexer = relay.start("history-indexer-unconfigured", [binaries["layerx-indexer"]], env=empty_env)
        relay.until(lambda: indexer.poll() is not None, "missing ingestion source refused at startup")
        require(indexer.returncode != 0, "missing ingestion source was accepted")
        doc = relay.request(gateway_url, "/v1/status")
        require(doc["services"]["indexer"] == "unavailable", "gateway accepted missing indexer source")
        checks += 1
        return checks
    finally:
        for reservation in reservations:
            reservation.close()
        relay.close()


def unified_pagination_required(name):
    value = os.environ.get(name)
    if not value:
        raise RuntimeError(f"missing real gate prerequisite: {name}")
    return value


def unified_pagination_local_url(value):
    import urllib.parse

    parsed = urllib.parse.urlsplit(value)
    if parsed.scheme not in ("http", "https", "postgres", "postgresql") or parsed.hostname not in ("127.0.0.1", "localhost", "::1"):
        raise RuntimeError("gate endpoints must be disposable loopback services")
    return parsed


def unified_pagination():
    import secrets
    import urllib.parse

    pagination_root = Path(__file__).resolve().parents[3]
    backend = unified_pagination_required("PAXEER_X_GATE_BACKEND_URL").rstrip("/")
    frontend = unified_pagination_required("PAXEER_X_GATE_FRONTEND_URL").rstrip("/")
    unified_pagination_local_url(backend)
    unified_pagination_local_url(frontend)
    db = unified_pagination_local_url(unified_pagination_required("PAXEER_X_GATE_DATABASE_URL"))
    database = db.path.lstrip("/")
    if not database.startswith("paxeer_x_gate_23_1"):
        raise RuntimeError("refusing any database outside the task's disposable namespace")
    for executable in ("psql", "node", "mix"):
        if not shutil.which(executable):
            raise RuntimeError(f"missing pinned gate dependency: {executable}")
    secret = unified_pagination_required("PAXEER_X_GATE_SECRET_KEY_BASE")
    pg_env = {**os.environ, "PGHOST": db.hostname, "PGPORT": str(db.port or 5432),
              "PGDATABASE": database, "PGUSER": urllib.parse.unquote(db.username or "postgres"),
              "PGPASSWORD": urllib.parse.unquote(db.password or "")}

    def sql(statement):
        result = subprocess.run(["psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1"], input=statement,
                                text=True, capture_output=True, env=pg_env, timeout=30)
        if result.returncode:
            raise RuntimeError("real fixture SQL failed: " + result.stderr[-1500:])
        return result.stdout.strip()

    def api(address, params=None, expected_status=200):
        url = f"{backend}/api/v2/addresses/{address}/unified"
        if params:
            url += "?" + urllib.parse.urlencode(params)
        try:
            with urllib.request.urlopen(url, timeout=30) as response:
                status, payload = response.status, response.read()
        except urllib.error.HTTPError as error:
            status, payload = error.code, error.read()
        assert status == expected_status, f"API status {status}; expected {expected_status}"
        return json.loads(payload)

    account, other, contract = ["0x" + secrets.token_hex(20) for _ in range(3)]
    block_hashes = [secrets.token_hex(32) for _ in range(122)]
    tx_hashes = [secrets.token_hex(32) for _ in range(122)]
    base = int(sql("SELECT COALESCE(MAX(number),0)+1000 FROM blocks;"))
    bytea = lambda value: "decode('" + value.removeprefix("0x") + "','hex')"
    for address in (account, other, contract):
        sql(f"INSERT INTO addresses(hash,inserted_at,updated_at) VALUES ({bytea(address)},now(),now());")
    sql(f"INSERT INTO tokens(contract_address_hash,type,symbol,decimals,inserted_at,updated_at) VALUES ({bytea(contract)},'ERC-20','PAGE',18,now(),now());")

    def insert_transaction(n, consensus=True):
        block_hash, tx_hash = block_hashes[n-1], tx_hashes[n-1]
        sql(f"""BEGIN;
        INSERT INTO blocks(hash,number,consensus,parent_hash,miner_hash,nonce,size,gas_limit,gas_used,timestamp,inserted_at,updated_at)
        VALUES ({bytea(block_hash)},{base+n},{str(consensus).lower()},{bytea(secrets.token_hex(32))},{bytea(other)},decode('0000000000000000','hex'),1,30000000,21000,now(),now(),now());
        INSERT INTO transactions(hash,block_hash,block_number,block_consensus,block_timestamp,"index",from_address_hash,to_address_hash,gas,gas_price,gas_used,cumulative_gas_used,input,nonce,r,s,v,value,status,inserted_at,updated_at)
        VALUES ({bytea(tx_hash)},{bytea(block_hash)},{base+n},{str(consensus).lower()},now(),0,{bytea(account)},{bytea(other)},21000,1,21000,21000,decode('','hex'),{n},1,1,27,1,1,now(),now());
        COMMIT;""")

    for n in range(1,121):
        insert_transaction(n)
    tie = 71
    sql(f"""INSERT INTO token_transfers(transaction_hash,block_hash,block_number,block_consensus,log_index,from_address_hash,to_address_hash,token_contract_address_hash,token_type,amount,inserted_at,updated_at)
    VALUES ({bytea(tx_hashes[tie-1])},{bytea(block_hashes[tie-1])},{base+tie},true,0,{bytea(account)},{bytea(other)},{bytea(contract)},'ERC-20',1,now(),now());
    INSERT INTO lx_custody_events(transaction_hash,block_hash,block_number,block_consensus,log_index,kind,direction,address_hash,amount,inserted_at,updated_at)
    VALUES ({bytea(tx_hashes[tie-1])},{bytea(block_hashes[tie-1])},{base+tie},true,0,'custody_deposit','deposit',{bytea(account)},1,now(),now());""")
    expected = [(base+n,0,"transaction","0x"+tx_hashes[n-1]) for n in range(1,121)]
    expected += [(base+tie,0,kind,"0x"+tx_hashes[tie-1]) for kind in ("token_transfer","custody_deposit")]
    expected.sort(reverse=True)
    key = lambda row: (row["block_number"],row["ordinal"],row["kind"],row["hash"].lower())
    first = api(account)
    assert len(first["activity"]) == 50 and first["next_page_params"]
    assert [key(row) for row in first["activity"]] == expected[:50]
    assert first["page_number"] == 1 and first["activity_total"] is None
    assert api(other)["activity_total"] is None
    no_history = api("0x" + secrets.token_hex(20))
    assert no_history["activity_total"] == 0 and no_history["next_page_params"] is None
    insert_transaction(121)
    insert_transaction(122, consensus=False)
    pages, rows, current = [], [], first
    for page_number in range(1,10):
        assert current["page_number"] == page_number
        page_rows = [key(row) for row in current["activity"]]
        pages.append(page_rows)
        rows.extend(page_rows)
        if current["next_page_params"] is None:
            break
        current = api(account,current["next_page_params"])
    assert rows == expected, "history skipped, duplicated, reordered or admitted a concurrent row"
    assert len(rows) == len(set(rows)) == 122
    assert [key(row) for row in api(account,{"cursor":first["page_cursor"]})["activity"]] == expected[:50]
    latest = api(account)
    assert latest["activity"][0]["hash"].lower() == "0x"+tx_hashes[120]
    assert all(row["hash"].lower() != "0x"+tx_hashes[121] for row in latest["activity"])
    for address, params in [(account,{"cursor":"malformed"}), (account,{"cursor":""}),
                            (account,{"cursor[]":"malformed"}),
                            (account,{**first["next_page_params"],"index":"0"}),
                            (other,first["next_page_params"]),
                            (account,{"block_number":str(base+tie),"index":"0"})]:
        assert api(address,params,422)["message"] == "Invalid activity cursor"

    signer = r'''
    Application.ensure_all_started(:crypto)
    key = System.fetch_env!("PAXEER_X_GATE_SECRET_KEY_BASE")
    {:ok, state} = Phoenix.Token.verify(key, "paxeer-x-unified-account-v1", System.fetch_env!("PAXEER_X_GATE_CURSOR"), max_age: 3600)
    token = Phoenix.Token.sign(key, "paxeer-x-unified-account-v1", state, signed_at: System.system_time(:second) - 3601)
    IO.puts("EXPIRED_CURSOR=" <> token)
    '''
    signed = subprocess.run(["mix","run","--no-compile","--no-deps-check","--no-start","-e",signer],
                            cwd=pagination_root/"explorer/backend", env={**os.environ,"PAXEER_X_GATE_SECRET_KEY_BASE":secret,
                            "PAXEER_X_GATE_CURSOR":first["page_cursor"]},capture_output=True,text=True,timeout=30)
    if signed.returncode:
        raise RuntimeError("real Phoenix cursor expiry setup failed")
    expired = next((line.split("=",1)[1] for line in signed.stdout.splitlines() if line.startswith("EXPIRED_CURSOR=")),None)
    assert expired and api(account,{"cursor":expired},422)["reason"] == "expired"

    browser = r'''
    const { chromium } = require('@playwright/test');
    const assert = require('node:assert/strict');
    const fs = require('node:fs');
    const input = JSON.parse(fs.readFileSync(0, 'utf8'));
    (async () => {
      const browser = await chromium.launch({ headless: true });
      try {
        const page = await browser.newPage();
        const seen = [];
        page.on('request', request => {
          if (request.url().includes('/unified?')) seen.push(request.url());
        });
        const url = input.frontend + '/paxeer-x/account/' + input.account + '?tab=activity&cursor=' + encodeURIComponent(input.cursor);
        await page.goto(url);
        const hashes = () => page.locator('[data-label="paxeer-x-activity"] [data-activity]').evaluateAll(rows => rows.map(row => row.getAttribute('data-activity').toLowerCase()));
        const waitPage = async (number, expected) => {
          await page.locator('[data-control="page"]').filter({hasText: `Page ${number}`}).waitFor();
          await page.waitForFunction(expected => {
            const rows = [...document.querySelectorAll('[data-label="paxeer-x-activity"] [data-activity]')].map(row => row.getAttribute('data-activity').toLowerCase());
            return JSON.stringify(rows) === JSON.stringify(expected);
          }, expected);
          assert.deepEqual(await hashes(), expected);
        };
        const expected = input.pages.map(rows => rows.map(row => row[3]));
        await waitPage(1, expected[0]);
        await page.getByText('50 activity entries on this page', {exact:true}).waitFor();
        await page.getByRole('button', {name:'Next page', exact:true}).click();
        await waitPage(2, expected[1]);
        const pageTwo = page.url();
        assert.notEqual(new URL(pageTwo).searchParams.get('cursor'), input.cursor);
        await page.reload();
        await waitPage(2, expected[1]);
        assert.equal(page.url(), pageTwo);
        await page.goBack();
        await waitPage(1, expected[0]);
        await page.goForward();
        await waitPage(2, expected[1]);
        await page.getByRole('button', {name:'Next page', exact:true}).click();
        await waitPage(3, expected[2]);
        assert.equal(await page.getByRole('button', {name:'Next page', exact:true}).isDisabled(), true);
        assert(seen.some(url => new URL(url).searchParams.get('cursor') === new URL(pageTwo).searchParams.get('cursor')));
        await page.goto(input.frontend + '/paxeer-x/account/' + input.account + '?tab=activity&cursor=malformed');
        await page.getByRole('alert').filter({hasText:'Unable to load this history page'}).waitFor();
        assert.equal(await page.locator('[data-label="paxeer-x-activity"]').count(), 0);
        await page.goto(input.frontend + '/paxeer-x/account/' + input.account + '?tab=activity&cursor=');
        await page.getByRole('alert').filter({hasText:'Unable to load this history page'}).waitFor();
        assert.equal(new URL(page.url()).searchParams.get('cursor'), '');
        assert.equal(await page.locator('[data-label="paxeer-x-activity"]').count(), 0);
        await page.getByRole('button',{name:'Start a new history view',exact:true}).click();
        await page.locator('[data-label="paxeer-x-activity"]').waitFor();
        console.log(JSON.stringify({browser:'passed',pages:expected.length,reload:true,back_forward:true,refusal:true}));
      } finally { await browser.close(); }
    })().catch(error => { console.error(error.message); process.exit(1); });
    '''
    result = subprocess.run(["node","-e",browser],cwd=pagination_root/"explorer/frontend",text=True,
                            input=json.dumps({"frontend":frontend,"account":account,"cursor":first["page_cursor"],"pages":pages}),timeout=180)
    assert result.returncode == 0, "real browser pagination gate failed"
    sql(f"UPDATE blocks SET consensus=false WHERE hash={bytea(block_hashes[119])};")
    assert api(account,{"cursor":first["page_cursor"]},422)["message"] == "Invalid activity cursor"
    checks = ["real_database", "real_api", "full_key_ties", "concurrent_insert", "consensus_filter", "reorg_refusal",
              "malformed_cursor", "expired_cursor", "wrong_account", "real_browser", "reload", "back_forward"]
    print(json.dumps({"case":"unified-pagination","status":"passed","rows":122,"pages":len(pages),
                      "checks":checks,"skips":0}))
    return len(checks)



def main():
    parser = argparse.ArgumentParser(description="Paxeer X explorer data-boundary gates")
    parser.add_argument("--case", required=True, choices=CASES)
    args = parser.parse_args()
    if args.case == "unified-pagination":
        try:
            tests = unified_pagination()
        except (AssertionError, RuntimeError, OSError, subprocess.SubprocessError, urllib.error.URLError) as error:
            print(json.dumps({"case":"unified-pagination","status":"failed","reason":str(error),"skips":0}), file=sys.stderr)
            return 1
        print(f"PAXEER_X_GATE tests={tests} skipped=0", flush=True)
        return 0
    if args.case == "history-readiness":
        try:
            tests = history_readiness()
        except MissingPrerequisite as error:
            print("missing prerequisite: " + str(error), file=sys.stderr)
            return 3
        except AssertionError as error:
            print("FAIL " + str(error), file=sys.stderr)
            return 1
        print(f"PAXEER_X_GATE tests={tests} skipped=0", flush=True)
        return 0
    if args.case == "standalone-receipts":
        tests, failures = case_standalone_receipts()
        print(f"PAXEER_X_GATE tests={tests} skipped=0", flush=True)
        return 1 if failures else 0
    if args.case != "kernel-receipts":
        print(f"case {args.case}: unimplemented", file=sys.stderr)
        return 2
    try:
        tests = kernel_receipts()
    except MissingPrerequisite as error:
        print("missing prerequisite: " + str(error), file=sys.stderr)
        return 3
    except AssertionError as error:
        print("FAIL " + str(error), file=sys.stderr)
        return 1
    print(f"PAXEER_X_GATE tests={tests} skipped=0", flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
