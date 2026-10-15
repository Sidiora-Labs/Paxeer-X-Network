#!/usr/bin/env python3
import argparse
import contextlib
import hashlib
import io
import ipaddress
import json
from pathlib import Path
import sys
import tempfile
import unittest
import urllib.parse

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / "tools/release"))
import fleet_check as fleet

LOCAL_ENV = ROOT / ".env.fleet-check-local"
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

    def test_services_phase(self):
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


class LocalChainTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if not LOCAL_ENV.is_file():
            raise AssertionError(
                "LOCAL_CHAIN_INPUT_MISSING: supply --local-env with a filled rollout env for a real upgraded local single-node paxd chain and real data services; no live qualification was performed"
            )
        cls.env = fleet.read_env(LOCAL_ENV)
        nodes, _, _, _, _ = fleet.load_config(cls.env, "post")
        if len(nodes) != 1:
            raise AssertionError("LOCAL_CHAIN_REQUIRES_ONE_NODE")
        for node in nodes:
            for url in (node.consensus_rpc, node.evm_rpc):
                if not ipaddress.ip_address(
                    urllib.parse.urlsplit(url).hostname
                ).is_loopback:
                    raise AssertionError("LOCAL_CHAIN_REQUIRES_LOOPBACK")
        cls.nodes = nodes

    def test_post_on_local_paxd(self):
        with tempfile.TemporaryDirectory() as directory:
            receipt = Path(directory) / "post.json"
            self.assertEqual(
                fleet.main(
                    [
                        "--env-file",
                        str(LOCAL_ENV),
                        "--phase",
                        "post",
                        "--receipt",
                        str(receipt),
                    ]
                ),
                0,
            )
            value = json.loads(receipt.read_text())
            self.assertEqual(value["result"], "pass")
            self.assertEqual((value["nodes_checked"], value["nodes_passed"]), (1, 1))
            self.assertEqual(value["precompiles_served"], 8)
            self.assertEqual(value["markets"], 18)
            self.assertEqual(value["oracle_fresh"], 18)
            self.assertEqual(value["feed_keys"], 1)

    def test_services_phase_on_local_services(self):
        urls = fleet.required(self.env, "DATA_SERVICE_URLS").split()
        urls.append(fleet.required(self.env, "APP_URL"))
        for url in urls:
            self.assertTrue(
                ipaddress.ip_address(urllib.parse.urlsplit(url).hostname).is_loopback
            )
        outcome = fleet.Outcome()
        fleet.check_services(self.env, outcome)
        self.assertEqual(outcome.errors, [])
        self.assertEqual(outcome.finish()["result"], "pass")
        self.assertEqual(outcome.receipt["services_ready"], len(urls) - 1)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument("--local-env", type=Path, default=LOCAL_ENV)
    arguments, remaining = parser.parse_known_args()
    LOCAL_ENV = arguments.local_env
    unittest.main(argv=[sys.argv[0], *remaining], verbosity=2)
