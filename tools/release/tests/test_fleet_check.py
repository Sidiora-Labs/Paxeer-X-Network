#!/usr/bin/env python3
import contextlib
import hashlib
import io
import http.server
import json
import os
from pathlib import Path
import random
import re
import socket
import subprocess
import sys
import tempfile
import threading
import time
import tomllib
import unittest
import urllib.request

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / "tools/release"))
import fleet_check as fleet

LOCAL_VERSION = "v6.11.0"
CHAIN_ID = "fleet_9000-1"
CGO_LDFLAGS = "-L/root/Layerx-protocol/build -L/root/Layerx-protocol/programs/target/debug -L/root/Layerx-protocol/programs/target/release"
VIEWS = json.loads((ROOT / "tools/release/fleet-views.json").read_text())


def abi(*values):
    return "0x" + "".join(f"{value:064x}" for value in values)


def feed_key(key):
    raw = bytes.fromhex(abi(64, 1 if key else 0, len(key))[2:])
    return "0x" + (raw + key + bytes(-len(key) % 32)).hex()


class FleetUnitTests(unittest.TestCase):
    def test_keccak_vectors(self):
        self.assertEqual(
            fleet.keccak256(b"").hex(),
            "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470",
        )
        self.assertEqual(
            fleet.keccak256(b"transfer(address,uint256)")[:4].hex(), "a9059cbb"
        )
        self.assertEqual(
            fleet.keccak256(b"abc").hex(),
            "4e03657aea45a94fc7d47ba826c8d667c0d1e6e33a64a036ec44f58fa12d6c45",
        )
        self.assertEqual(
            fleet.keccak256(b"a" * 200).hex(),
            "96ea54061def936c4be90b518992fdc6f12f535068a256229aca54267b4d084d",
        )

    def test_manifest_matches_real_abis(self):
        paths = (
            "precompiles/perps/abi/abi.json",
            "precompiles/launchpad/abi.json",
            "precompiles/pricefeed/abi/abi.json",
            "precompiles/spot/abi/pecor.json",
            "precompiles/spot/abi/orders.json",
            "precompiles/spot/abi/stoporders.json",
            "precompiles/spot/abi/quoter.json",
            "precompiles/spot/abi/vault.json",
        )
        self.assertEqual(
            [int(row["address"], 16) for row in VIEWS["served"]],
            [0x1015, 0x1017, *range(0x1020, 0x1026)],
        )
        for row, path in zip(VIEWS["served"], paths):
            with self.subTest(address=row["address"]):
                methods = json.loads((ROOT / path).read_text())
                found = [
                    method
                    for method in methods
                    if method.get("type") == "function"
                    and method.get("name")
                    + "("
                    + ",".join(value["type"] for value in method["inputs"])
                    + ")"
                    == row["signature"]
                ]
                self.assertEqual(len(found), 1)
                self.assertEqual(found[0]["stateMutability"], "view")
                self.assertEqual(len(found[0]["outputs"]), 1)
                self.assertIn(found[0]["outputs"][0]["type"], ("uint256", "address"))
                self.assertEqual(row["return_bytes"], 32)
        self.assertEqual(
            [int(row["address"], 16) for row in VIEWS["no_code"]], [0x1026, 0x1027]
        )

    def test_pointer_denom_matches_module_address(self):
        self.assertEqual(VIEWS["pointers"][0]["denom"], "uhpx")
        denom = VIEWS["pointers"][1]["denom"]
        prefix, creator, suffix = denom.split("/")
        self.assertEqual((prefix, suffix), ("factory", "usid"))
        digits = [fleet.BECH32.index(c) for c in creator.split("1", 1)[1]]
        number = 0
        for digit in digits[:-6]:
            number = number * 32 + digit
        self.assertEqual(
            number.to_bytes(20, "big"), hashlib.sha256(b"layerxbridge").digest()[:20]
        )
        expanded = [ord(c) >> 5 for c in "pax"] + [0] + [ord(c) & 31 for c in "pax"]
        self.assertEqual(fleet.polymod(expanded + digits), 1)

    def test_version_mismatch_fails(self):
        outcome = fleet.Outcome()
        outcome.receipt.update(nodes_checked=4, nodes_passed=0)
        for index, version in enumerate([fleet.VERSION] * 3 + ["v6.10.0"]):
            before = len(outcome.errors)
            outcome.attempt(f"node {index}", lambda: fleet.check_version(version))
            outcome.receipt["nodes_passed"] += before == len(outcome.errors)
        self.assertEqual(outcome.errors, ["node 3: VERSION_MISMATCH"])
        self.assertEqual(outcome.finish()["nodes_passed"], 3)
        self.assertEqual(outcome.receipt["result"], "fail")

    def test_applied_height_and_sync(self):
        applied = {"header": {"height": "1001"}}
        status = {"sync_info": {"latest_block_height": "1010", "catching_up": False}}
        fleet.check_applied_height_and_sync(applied, status, 1001)
        for value in (True, "false", 0, None):
            with self.subTest(value=value):
                status["sync_info"]["catching_up"] = value
                outcome = fleet.Outcome()
                outcome.attempt(
                    "node 2",
                    lambda: fleet.check_applied_height_and_sync(applied, status, 1001),
                )
                self.assertEqual(outcome.errors, ["node 2: NODE_CATCHING_UP"])
        status["sync_info"]["catching_up"] = False
        status["sync_info"]["latest_block_height"] = "1001"
        with self.assertRaisesRegex(fleet.CheckError, "^NODE_HEIGHT_NOT_PAST_UPGRADE$"):
            fleet.check_applied_height_and_sync(applied, status, 1001)
        with self.assertRaisesRegex(fleet.CheckError, "^APPLIED_HEIGHT_MISMATCH$"):
            fleet.check_applied_height_and_sync(applied, status, 1002)

    def test_precompile_served_by_view(self):
        observations = {row["address"]: abi(0) for row in VIEWS["served"]}
        missing = VIEWS["served"][-1]["address"]
        observations[missing] = "0x"
        outcome = fleet.Outcome()
        fleet.check_served_views(observations, VIEWS["served"], outcome, "node 0")
        self.assertEqual(outcome.receipt["precompiles_served"], 7)
        self.assertEqual(
            outcome.errors, [f"node 0 {missing}: PRECOMPILE_RETURN_LENGTH"]
        )
        fleet.check_no_code("0x")
        with self.assertRaisesRegex(fleet.CheckError, "^UNEXPECTED_CODE$"):
            fleet.check_no_code("0x00")
        with self.assertRaisesRegex(fleet.CheckError, "^POINTER_MISSING$"):
            fleet.decode_pointer(abi(0, 0, 0))
        self.assertEqual(fleet.decode_pointer(abi(1, 1, 1)), "0x" + "0" * 39 + "1")

    def test_pool_count_matches_rehearsal(self):
        count = fleet.decode_uint(abi(449))
        self.assertEqual(count, 449)
        fleet.check_pool_count(count, 449)
        with self.assertRaisesRegex(fleet.CheckError, "^POOL_COUNT_MISMATCH$"):
            fleet.check_pool_count(fleet.decode_uint(abi(448)), 449)

    def test_market_count_is_eighteen(self):
        fleet.check_market_counts(18, 18)
        outcome = fleet.Outcome()
        outcome.count("markets", 17)
        outcome.count("markets", 18)
        outcome.attempt("node 0 markets", lambda: fleet.check_market_counts(17, 18))
        self.assertEqual(outcome.finish()["markets"], 17)
        self.assertEqual(outcome.errors, ["node 0 markets: MARKET_COUNT_NOT_EIGHTEEN"])
        with self.assertRaisesRegex(fleet.CheckError, "^MARKET_COUNT_NOT_EIGHTEEN$"):
            fleet.check_market_counts(18, 17)

    def test_stale_price_fails(self):
        prices = [abi(10**18, 10**18, 1000, 10, 3, 1) for _ in range(18)]
        prices[7] = abi(10**18, 10**18, 1000, 10, 3, 0)
        outcome = fleet.Outcome()
        fleet.check_prices(prices, outcome, "node 0")
        self.assertEqual(outcome.receipt["oracle_fresh"], 17)
        self.assertEqual(outcome.errors, ["node 0 market 7: PRICE_STALE"])
        key = bytes.fromhex(
            "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"
        )
        fleet.check_feed_keys([feed_key(key)] * 3 + [feed_key(b"")], outcome, "node 0")
        self.assertEqual(outcome.receipt["feed_keys"], 3)
        self.assertEqual(outcome.errors[-1], "node 0 validator 3: FEED_KEY_MISSING")
        self.assertEqual(outcome.finish()["result"], "fail")

    def test_block_hash_agreement(self):
        common = "ab" * 32
        changed = "cd" * 32
        outcome = fleet.Outcome()
        for index, value in enumerate([common] * 3 + [changed]):
            parsed = fleet.block_hash(
                {"block_id": {"hash": value}, "block": {"header": {"height": "1001"}}},
                1001,
            )
            outcome.attempt(
                f"node {index}", lambda: fleet.check_block_hash(common.upper(), parsed)
            )
        self.assertEqual(outcome.errors, ["node 3: BLOCK_HASH_MISMATCH"])
        with self.assertRaisesRegex(fleet.CheckError, "^BLOCK_HASH_INVALID$"):
            fleet.block_hash(
                {"block_id": {"hash": ""}, "block": {"header": {"height": "1001"}}},
                1001,
            )

    def test_receipt_carries_counts_only(self):
        receipt = fleet.Outcome().receipt
        receipt.update(
            result="pass",
            nodes_checked=4,
            nodes_passed=4,
            pools=449,
            markets=18,
            oracle_fresh=18,
            feed_keys=4,
        )
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "receipt.json"
            fleet.write_receipt(path, receipt)
            decoded = json.loads(path.read_text())
            self.assertEqual(decoded, receipt)
            self.assertLessEqual(set(decoded), fleet.RECEIPT_KEYS)
            self.assertNotRegex(path.read_text(), r":\d+|[a-z]+\.[a-z]{2,}")
            original = path.read_bytes()
            for key, value in (
                ("height", 1001),
                ("host", "private"),
                ("pools", "private"),
                ("markets", True),
                ("version", "private"),
                ("inputs_approved", "true"),
                ("new_bin_sha256", "invalid"),
            ):
                with self.subTest(key=key):
                    with self.assertRaises(fleet.CheckError):
                        fleet.write_receipt(path, {**receipt, key: value})
                    self.assertEqual(path.read_bytes(), original)

    def test_service_readiness_units(self):
        outcome = fleet.Outcome()
        fleet.service_readiness([200, 200, 503, 200], outcome)
        self.assertEqual(outcome.finish()["services_ready"], 3)
        self.assertEqual(outcome.receipt["result"], "fail")
        self.assertEqual(outcome.errors, ["service 2: SERVICE_HTTP_503"])
        fleet.check_build_id(200, b'{"buildId":"release-build"}', "release-build")
        with self.assertRaisesRegex(fleet.CheckError, "^APP_BUILD_ID_MISMATCH$"):
            fleet.check_build_id(200, b'{"buildId":"previous-build"}', "release-build")
        with self.assertRaisesRegex(fleet.CheckError, "^SERVICE_HTTP_302$"):
            fleet.check_build_id(302, b"", "release-build")

    def test_abi_rejects_malformed_data(self):
        for value in ("0x", "0x0", "0xzz", "00", abi(1, 2), None):
            with self.subTest(value=value):
                with self.assertRaises(fleet.CheckError):
                    fleet.decode_uint(value)
        with self.assertRaisesRegex(fleet.CheckError, "^ABI_PRICE$"):
            fleet.decode_price(abi(1, 1, 1, 1, 1, 2))
        with self.assertRaisesRegex(fleet.CheckError, "^ABI_PRICE_HEIGHT$"):
            fleet.decode_price(abi(1, 1, 1, 1 << 63, 1, 1))
        with self.assertRaisesRegex(fleet.CheckError, "^ABI_POINTER$"):
            fleet.decode_pointer(abi(1 << 160, 1, 1))
        with self.assertRaisesRegex(fleet.CheckError, "^ABI_FEED_KEY$"):
            fleet.decode_feed_key(abi(32, 0, 0))
        with self.assertRaisesRegex(fleet.CheckError, "^FEED_KEY_LENGTH$"):
            fleet.decode_feed_key(abi(64, 1, 1 << 255))
        with self.assertRaisesRegex(fleet.CheckError, "^ABI_UINT_RANGE$"):
            fleet.word(True)

    def test_rpc_rejects_null_result(self):
        with self.assertRaisesRegex(fleet.CheckError, "^RPC_EMPTY_RESULT$"):
            fleet.decode_rpc_response(b'{"jsonrpc":"2.0","id":1,"result":null}')
        with self.assertRaisesRegex(fleet.CheckError, "^RPC_ERROR$"):
            fleet.decode_rpc_response(b'{"jsonrpc":"2.0","id":1,"error":{}}')
        self.assertEqual(
            fleet.decode_rpc_response(b'{"jsonrpc":"2.0","id":1,"result":"0x"}'),
            "0x",
        )

    def test_calldata_uses_abi_offsets(self):
        data = bytes.fromhex(fleet.call_data("getNativePointer(string)", "uhpx")[2:])
        self.assertEqual(len(data), 100)
        self.assertEqual(data[4:], bytes.fromhex(abi(32, 4)[2:]) + b"uhpx" + bytes(28))
        self.assertEqual(
            fleet.call_data("getFeedKey(address)", "0x" + "01" * 20),
            "0x2d6f477c" + "00" * 12 + "01" * 20,
        )
        self.assertEqual(fleet.call_data("decimals()"), "0x313ce567")
        with self.assertRaisesRegex(fleet.CheckError, "^ABI_ADDRESS$"):
            fleet.call_data("getFeedKey(address)", "0x01")

    def test_env_file_is_not_executed(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "inputs.env"
            path.write_text(
                'HALT_HEIGHT=1000\nAPP_BUILD_ID="$(false)"\nDATA_SERVICE_URLS="one two" # trailing\n'
            )
            self.assertEqual(
                fleet.read_env(path),
                {
                    "HALT_HEIGHT": "1000",
                    "APP_BUILD_ID": "$(false)",
                    "DATA_SERVICE_URLS": "one two",
                },
            )
            path.write_text("HALT_HEIGHT=1\nHALT_HEIGHT=2\n")
            with self.assertRaisesRegex(fleet.CheckError, "ENV_KEY line 2"):
                fleet.read_env(path)
            path.write_text("HALT_HEIGHT=\n")
            with self.assertRaisesRegex(fleet.CheckError, "HALT_HEIGHT is empty"):
                fleet.required(fleet.read_env(path), "HALT_HEIGHT")

    def test_invalid_inputs_write_failure_receipt(self):
        with tempfile.TemporaryDirectory() as directory:
            env = Path(directory) / "input.env"
            receipt = Path(directory) / "receipt.json"
            env.write_text("HALT_HEIGHT=\n")
            stderr = io.StringIO()
            with contextlib.redirect_stderr(stderr), contextlib.redirect_stdout(
                io.StringIO()
            ):
                code = fleet.main(
                    [
                        "--env-file",
                        str(env),
                        "--phase",
                        "post",
                        "--receipt",
                        str(receipt),
                    ]
                )
            self.assertEqual(code, 1)
            self.assertEqual(json.loads(receipt.read_text())["result"], "fail")
            self.assertEqual(stderr.getvalue(), "fleet_check: HALT_HEIGHT is empty\n")

    def test_check_does_not_accept_receipt(self):
        with contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit) as exit_code:
                fleet.main(["--env-file", "unused", "--check", "--receipt", "unused"])
        self.assertEqual(exit_code.exception.code, 2)

    def test_count_aggregation_retains_worst_node(self):
        outcome = fleet.Outcome()
        for count in (8, 7, 8):
            outcome.count("precompiles_served", count)
        self.assertEqual(outcome.receipt["precompiles_served"], 7)


LOOPBACK = "127.0.0.1"
NO_PROXY = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def free_port(taken):
    for _ in range(1000):
        port = random.randint(30001, 60999)
        if port in taken:
            continue
        with socket.socket() as probe:
            try:
                probe.bind((LOOPBACK, port))
            except OSError:
                continue
        taken.add(port)
        return port
    raise AssertionError("LOCAL_PORTS_EXHAUSTED")


def edit_toml(path, changes):
    section = ""
    seen = set()
    lines = []
    for line in path.read_text().splitlines():
        heading = re.fullmatch(r"\s*\[([^]]+)\]\s*", line)
        if heading:
            section = heading[1]
        key = re.match(r"\s*([\w-]+)\s*=", line)
        pair = (section, key[1]) if key else None
        if pair in changes:
            line = f"{key[1]} = {changes[pair]}"
            seen.add(pair)
        lines.append(line)
    if seen != set(changes):
        raise AssertionError(f"TOML_KEYS_MISSING {sorted(set(changes) - seen)}")
    text = "\n".join(lines) + "\n"
    tomllib.loads(text)
    path.write_text(text)


def chain_ready(consensus_rpc):
    try:
        with NO_PROXY.open(consensus_rpc + "/status", timeout=2) as response:
            sync = json.load(response)["sync_info"]
        return sync["catching_up"] is False and int(sync["latest_block_height"]) > 0
    except (OSError, ValueError, KeyError, TypeError):
        return False


def readiness_service(upstream, build_id=None):
    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            if self.path == "/healthz/ready":
                code = 200 if chain_ready(upstream) else 503
                body = json.dumps({"status": "ok" if code == 200 else "degraded"})
            elif self.path == "/api/build-id" and build_id is not None:
                code, body = 200, json.dumps({"buildId": build_id})
            else:
                code, body = 404, "{}"
            data = body.encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def log_message(self, *args):
            pass

    server = http.server.ThreadingHTTPServer((LOOPBACK, 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return server


class LocalChainTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.work = tempfile.TemporaryDirectory(prefix="fleet-check-")
        work = Path(cls.work.name)
        cls.bin_dir = work / "bin"
        cls.bin_dir.mkdir()
        paxd = cls.bin_dir / "paxd"
        given = os.environ.get("PAXD_BIN")
        if given:
            paxd.symlink_to(Path(given).resolve())
        else:
            subprocess.run(
                [
                    "/usr/local/go/bin/go",
                    "build",
                    "-trimpath",
                    "-buildvcs=false",
                    "-tags",
                    "netgo ledger",
                    "-ldflags",
                    "-checklinkname=0 -X github.com/Sidiora-Labs/Paxeer-X-Network/sdk/version.Version="
                    + LOCAL_VERSION,
                    "-o",
                    str(paxd),
                    "./daemon/paxd",
                ],
                cwd=ROOT,
                env={**os.environ, "CGO_LDFLAGS": CGO_LDFLAGS},
                check=True,
            )
        cls.version = subprocess.run(
            [str(paxd), "version"], capture_output=True, text=True, check=True
        ).stdout.strip()
        home = work / "home"

        def paxd_run(*args):
            return subprocess.run(
                [str(paxd), *args, "--home", str(home)],
                capture_output=True,
                text=True,
                check=True,
            ).stdout

        paxd_run("init", "fleet", "--chain-id", CHAIN_ID, "--mode", "validator")
        paxd_run("keys", "add", "operator", "--keyring-backend", "test")
        address = paxd_run(
            "keys", "show", "operator", "-a", "--keyring-backend", "test"
        ).strip()
        genesis_path = home / "config/genesis.json"
        denom = json.loads(genesis_path.read_text())["app_state"]["staking"]["params"][
            "bond_denom"
        ]
        paxd_run("add-genesis-account", address, "100000000000000000000" + denom)
        paxd_run(
            "gentx",
            "operator",
            "1000000000000000000" + denom,
            "--chain-id",
            CHAIN_ID,
            "--keyring-backend",
            "test",
        )
        genesis = json.loads(genesis_path.read_text())
        public_key = json.loads((home / "config/priv_validator_key.json").read_text())[
            "pub_key"
        ]
        genesis["validators"] = [{"power": "1000000000000", "pub_key": public_key}]
        genesis["app_state"]["staking"]["params"][
            "max_voting_power_ratio"
        ] = "1.000000000000000000"
        genesis_path.write_text(json.dumps(genesis))
        paxd_run("collect-gentxs")
        paxd_run("validate-genesis")
        taken = set()
        rpc, peer, http_port, ws, grpc = (free_port(taken) for _ in range(5))
        edit_toml(
            home / "config/config.toml",
            {
                ("rpc", "laddr"): json.dumps(f"tcp://{LOOPBACK}:{rpc}"),
                ("rpc", "pprof-laddr"): '""',
                ("p2p", "laddr"): json.dumps(f"tcp://{LOOPBACK}:{peer}"),
                ("p2p", "persistent-peers"): '""',
                ("p2p", "bootstrap-peers"): '""',
                ("consensus", "create-empty-blocks"): "true",
                ("consensus", "create-empty-blocks-interval"): '"10ms"',
                ("consensus", "unsafe-commit-timeout-override"): '"10ms"',
            },
        )
        edit_toml(
            home / "config/app.toml",
            {
                ("", "minimum-gas-prices"): json.dumps("0" + denom),
                ("api", "enable"): "false",
                ("grpc", "enable"): "true",
                ("grpc", "address"): json.dumps(f"{LOOPBACK}:{grpc}"),
                ("grpc-web", "enable"): "false",
                ("evm", "http_enabled"): "true",
                ("evm", "http_address"): json.dumps(LOOPBACK),
                ("evm", "http_port"): str(http_port),
                ("evm", "ws_enabled"): "true",
                ("evm", "ws_port"): str(ws),
            },
        )
        cls.consensus_rpc = f"http://{LOOPBACK}:{rpc}"
        cls.evm_rpc = f"http://{LOOPBACK}:{http_port}"
        cls.dead_rpc = f"http://{LOOPBACK}:{free_port(taken)}"
        cls.servers = []
        cls.log = open(work / "paxd.log", "wb")
        cls.process = subprocess.Popen(
            [str(paxd), "start", "--home", str(home)],
            stdout=cls.log,
            stderr=subprocess.STDOUT,
        )
        deadline = time.monotonic() + 180
        while not chain_ready(cls.consensus_rpc):
            if cls.process.poll() is not None or time.monotonic() > deadline:
                tail = (work / "paxd.log").read_text(errors="replace")[-4000:]
                cls.tearDownClass()
                raise AssertionError("LOCAL_CHAIN_NOT_READY\n" + tail)
            time.sleep(0.2)

    @classmethod
    def tearDownClass(cls):
        for server in cls.servers:
            server.shutdown()
            server.server_close()
        cls.process.terminate()
        try:
            cls.process.wait(60)
        except subprocess.TimeoutExpired:
            cls.process.kill()
            cls.process.wait()
        cls.log.close()
        cls.work.cleanup()

    def write_env(self, values):
        path = Path(self.work.name) / "inputs.env"
        path.write_text("".join(f'{key}="{value}"\n' for key, value in values.items()))
        return path

    def chain_env(self):
        inventory = Path(self.work.name) / "inventory.json"
        inventory.write_text(
            json.dumps(
                {
                    "previous_version": LOCAL_VERSION,
                    "nodes": [
                        {
                            "role": "validator",
                            "consensus_rpc": self.consensus_rpc,
                            "evm_rpc": self.evm_rpc,
                        }
                    ],
                }
            )
        )
        return {
            "HALT_HEIGHT": "1000000",
            "CHAIN_ID": CHAIN_ID,
            "BIN_DIR": str(self.bin_dir),
            "INVENTORY_FILE": str(inventory),
        }

    def run_main(self, env, phase):
        receipt = Path(self.work.name) / f"{phase}.json"
        stderr = io.StringIO()
        with contextlib.redirect_stderr(stderr), contextlib.redirect_stdout(
            io.StringIO()
        ):
            code = fleet.main(
                [
                    "--env-file",
                    str(self.write_env(env)),
                    "--phase",
                    phase,
                    "--receipt",
                    str(receipt),
                ]
            )
        text = receipt.read_text()
        value = json.loads(text)
        self.assertLessEqual(set(value), fleet.RECEIPT_KEYS)
        self.assertNotRegex(text, r":\d+|\d+\.\d+\.\d+\.\d+")
        return code, value, stderr.getvalue().splitlines()

    def test_preflight_on_local_paxd(self):
        self.assertEqual(self.version, LOCAL_VERSION)
        code, value, errors = self.run_main(self.chain_env(), "preflight")
        self.assertEqual(errors, [])
        self.assertEqual(code, 0)
        self.assertEqual(
            value,
            {
                "version": fleet.VERSION,
                "plan": fleet.VERSION,
                "nodes_checked": 1,
                "nodes_passed": 1,
                "result": "pass",
            },
        )

    def test_version_mismatch_on_local_paxd(self):
        code, value, errors = self.run_main(self.chain_env(), "validators")
        self.assertEqual(code, 1)
        self.assertIn("node 0 version: VERSION_MISMATCH", errors)
        self.assertTrue(any(line.startswith("node 0 applied: ") for line in errors))
        self.assertEqual((value["nodes_checked"], value["nodes_passed"]), (1, 0))
        self.assertEqual(value["result"], "fail")

    def test_services_phase(self):
        live = [readiness_service(self.consensus_rpc) for _ in range(3)]
        dead = readiness_service(self.dead_rpc)
        app = readiness_service(self.consensus_rpc, self.version)
        self.servers.extend([*live, dead, app])

        def url(server):
            return f"http://{LOOPBACK}:{server.server_address[1]}"

        env = {
            "DATA_SERVICE_URLS": " ".join(url(server) for server in [*live, dead]),
            "APP_URL": url(app),
            "APP_BUILD_ID": self.version,
        }
        code, value, errors = self.run_main(env, "services")
        self.assertEqual(code, 1)
        self.assertEqual(errors, ["service 3: SERVICE_HTTP_503"])
        self.assertEqual(value["services_ready"], 3)
        self.assertEqual(value["result"], "fail")
        env["DATA_SERVICE_URLS"] = " ".join(url(server) for server in live)
        code, value, errors = self.run_main(env, "services")
        self.assertEqual((code, errors), (0, []))
        self.assertEqual((value["services_ready"], value["result"]), (3, "pass"))
        env["APP_BUILD_ID"] = fleet.VERSION
        code, value, errors = self.run_main(env, "services")
        self.assertEqual((code, errors), (1, ["application: APP_BUILD_ID_MISMATCH"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
