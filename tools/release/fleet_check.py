#!/usr/bin/env python3
import argparse
import http.client
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys
import tempfile
import urllib.error
import urllib.parse
import urllib.request

VERSION = "v6.12.0"
PHASES = (
    "preflight",
    "frozen",
    "validators",
    "oracle",
    "markets",
    "precompiles",
    "services",
    "full",
    "post",
)
RECEIPT_KEYS = frozenset(
    "version plan nodes_checked nodes_passed precompiles_served pools markets "
    "oracle_fresh feed_keys homes_archived hosts_staged units_active services_ready "
    "readiness rollback_executed rollback_rehearsed new_bin_sha256 inputs_sha256 "
    "inputs_approved tag release_sha256 result".split()
)
COUNT_KEYS = frozenset(
    "nodes_checked nodes_passed precompiles_served pools markets oracle_fresh "
    "feed_keys homes_archived hosts_staged units_active services_ready".split()
)
FLAG_KEYS = frozenset(
    "readiness rollback_executed rollback_rehearsed inputs_approved".split()
)
MASK = (1 << 64) - 1
ROTATIONS = (
    0,
    1,
    62,
    28,
    27,
    36,
    44,
    6,
    55,
    20,
    3,
    10,
    43,
    25,
    39,
    41,
    45,
    15,
    21,
    8,
    18,
    2,
    61,
    56,
    14,
)
ROUND_CONSTANTS = (
    0x0000000000000001,
    0x0000000000008082,
    0x800000000000808A,
    0x8000000080008000,
    0x000000000000808B,
    0x0000000080000001,
    0x8000000080008081,
    0x8000000000008009,
    0x000000000000008A,
    0x0000000000000088,
    0x0000000080008009,
    0x000000008000000A,
    0x000000008000808B,
    0x800000000000008B,
    0x8000000000008089,
    0x8000000000008003,
    0x8000000000008002,
    0x8000000000000080,
    0x000000000000800A,
    0x800000008000000A,
    0x8000000080008081,
    0x8000000000008080,
    0x0000000080000001,
    0x8000000080008008,
)
BECH32 = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"


class CheckError(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise CheckError(message)


def keccak256(data):
    data = bytearray(data)
    padding = 136 - len(data) % 136
    data.extend(bytes(padding))
    data[-padding] = 0x01
    data[-1] |= 0x80
    state = [0] * 25
    for offset in range(0, len(data), 136):
        for i in range(17):
            state[i] ^= int.from_bytes(
                data[offset + 8 * i : offset + 8 * i + 8], "little"
            )
        for rc in ROUND_CONSTANTS:
            c = [
                state[x] ^ state[x + 5] ^ state[x + 10] ^ state[x + 15] ^ state[x + 20]
                for x in range(5)
            ]
            d = [
                c[(x - 1) % 5] ^ ((c[(x + 1) % 5] << 1 | c[(x + 1) % 5] >> 63) & MASK)
                for x in range(5)
            ]
            b = [0] * 25
            for y in range(5):
                for x in range(5):
                    i = x + 5 * y
                    v = state[i] ^ d[x]
                    r = ROTATIONS[i]
                    b[y + 5 * ((2 * x + 3 * y) % 5)] = (
                        (v << r) | (v >> ((64 - r) % 64))
                    ) & MASK
            for y in range(5):
                for x in range(5):
                    state[x + 5 * y] = b[x + 5 * y] ^ (
                        (~b[(x + 1) % 5 + 5 * y]) & b[(x + 2) % 5 + 5 * y]
                    )
            state[0] ^= rc
    return b"".join(v.to_bytes(8, "little") for v in state)[:32]


def word(value):
    require(type(value) is int and 0 <= value < 1 << 256, "ABI_UINT_RANGE")
    return value.to_bytes(32, "big")


def call_data(signature, argument=None):
    data = keccak256(signature.encode("ascii"))[:4]
    if argument is not None:
        if signature.endswith("(string)"):
            raw = argument.encode("utf-8")
            data += word(32) + word(len(raw)) + raw + bytes(-len(raw) % 32)
        elif signature.endswith("(address)"):
            require(
                isinstance(argument, str)
                and re.fullmatch(r"0x[0-9a-fA-F]{40}", argument),
                "ABI_ADDRESS",
            )
            data += bytes(12) + bytes.fromhex(argument[2:])
        elif signature.endswith("(uint256)"):
            data += word(argument)
        else:
            raise CheckError("ABI_SIGNATURE")
    return "0x" + data.hex()


def decode_hex(value):
    require(
        isinstance(value, str) and re.fullmatch(r"0x(?:[0-9a-fA-F]{2})*", value),
        "ABI_HEX",
    )
    return bytes.fromhex(value[2:])


def decode_words(value, count):
    data = decode_hex(value)
    require(len(data) == count * 32, "ABI_LENGTH")
    return [int.from_bytes(data[i : i + 32], "big") for i in range(0, len(data), 32)]


def decode_uint(value):
    return decode_words(value, 1)[0]


def decode_pointer(value):
    address, version, exists = decode_words(value, 3)
    require(
        address < 1 << 160 and version < 1 << 16 and exists in (0, 1), "ABI_POINTER"
    )
    require(exists == 1 and address != 0, "POINTER_MISSING")
    return "0x" + address.to_bytes(20, "big").hex()


def decode_price(value):
    values = decode_words(value, 6)
    require(
        values[2] < 1 << 64 and values[4] < 1 << 32 and values[5] in (0, 1), "ABI_PRICE"
    )
    height = values[3]
    require(height < 1 << 63 or height >= (1 << 256) - (1 << 63), "ABI_PRICE_HEIGHT")
    return values[5] == 1


def decode_feed_key(value):
    data = decode_hex(value)
    require(len(data) >= 96 and len(data) % 32 == 0, "ABI_FEED_KEY")
    offset = int.from_bytes(data[:32], "big")
    account = int.from_bytes(data[32:64], "big")
    require(offset == 64 and account < 1 << 160, "ABI_FEED_KEY")
    length = int.from_bytes(data[64:96], "big")
    require(
        length in (0, 33) and len(data) == 96 + ((length + 31) // 32) * 32,
        "FEED_KEY_LENGTH",
    )
    require(not any(data[96 + length :]), "ABI_FEED_KEY_PADDING")
    key = data[96 : 96 + length]
    require(len(key) == 33 and key[0] in (2, 3) and account != 0, "FEED_KEY_MISSING")
    return key


def polymod(values):
    chk = 1
    for value in values:
        top = chk >> 25
        chk = (chk & 0x1FFFFFF) << 5 ^ value
        for i, generator in enumerate(
            (0x3B6A57B2, 0x26508E6D, 0x1EA119FA, 0x3D4233DD, 0x2A1462B3)
        ):
            if (top >> i) & 1:
                chk ^= generator
    return chk


def operator_address(value):
    require(isinstance(value, str) and value == value.lower(), "VALIDATOR_ADDRESS")
    prefix, sep, encoded = value.rpartition("1")
    require(
        sep
        and prefix == "paxvaloper"
        and len(encoded) == 38
        and all(c in BECH32 for c in encoded),
        "VALIDATOR_ADDRESS",
    )
    digits = [BECH32.index(c) for c in encoded]
    expanded = [ord(c) >> 5 for c in prefix] + [0] + [ord(c) & 31 for c in prefix]
    require(polymod(expanded + digits) == 1, "VALIDATOR_CHECKSUM")
    number = 0
    for digit in digits[:-6]:
        number = (number << 5) | digit
    return "0x" + number.to_bytes(20, "big").hex()


def positive_int(value, label, allow_zero=False):
    require(
        type(value) is int or isinstance(value, str) and re.fullmatch(r"[0-9]+", value),
        label,
    )
    value = int(value)
    require(value >= (0 if allow_zero else 1), label)
    return value


def check_version(version, expected=VERSION):
    require(version == expected, "VERSION_MISMATCH")


def check_applied_height_and_sync(applied, status, upgrade_height):
    require(
        positive_int(applied["header"]["height"], "APPLIED_HEIGHT") == upgrade_height,
        "APPLIED_HEIGHT_MISMATCH",
    )
    require(status["sync_info"]["catching_up"] is False, "NODE_CATCHING_UP")
    require(
        positive_int(status["sync_info"]["latest_block_height"], "NODE_HEIGHT")
        > upgrade_height,
        "NODE_HEIGHT_NOT_PAST_UPGRADE",
    )


def check_pool_count(actual, expected):
    require(actual == expected, "POOL_COUNT_MISMATCH")


def check_market_counts(perps, pricefeed):
    require(perps == 18 and pricefeed == 18, "MARKET_COUNT_NOT_EIGHTEEN")


def block_hash(value, height):
    require(
        positive_int(value["block"]["header"]["height"], "BLOCK_HEIGHT") == height,
        "BLOCK_HEIGHT_MISMATCH",
    )
    result = value["block_id"]["hash"]
    require(
        isinstance(result, str) and re.fullmatch(r"[0-9a-fA-F]{64}", result),
        "BLOCK_HASH_INVALID",
    )
    return result.upper()


def check_block_hash(expected, actual):
    require(actual == expected, "BLOCK_HASH_MISMATCH")


def validate_receipt(receipt):
    require(isinstance(receipt, dict) and set(receipt) <= RECEIPT_KEYS, "RECEIPT_KEYS")
    for key, value in receipt.items():
        if key in COUNT_KEYS:
            require(type(value) is int and value >= 0, "RECEIPT_COUNT")
        elif key in FLAG_KEYS:
            require(type(value) is bool, "RECEIPT_FLAG")
        elif key.endswith("sha256"):
            require(
                isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value),
                "RECEIPT_DIGEST",
            )
        else:
            allowed = {
                "version": {VERSION},
                "plan": {VERSION},
                "tag": {"paxeer-network/" + VERSION},
                "result": {"pass", "fail"},
            }
            require(isinstance(value, str) and value in allowed[key], "RECEIPT_VALUE")


def write_receipt(path, receipt):
    validate_receipt(receipt)
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    name = None
    try:
        with tempfile.NamedTemporaryFile(
            mode="w", dir=path.parent, prefix=".receipt-", delete=False
        ) as stream:
            name = stream.name
            json.dump(receipt, stream, sort_keys=True, indent=2)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(name, path)
    finally:
        if name is not None and os.path.exists(name):
            os.unlink(name)


def read_env(path):
    result = {}
    for index, line in enumerate(Path(path).read_text().splitlines(), 1):
        tokens = shlex.split(line, comments=True)
        if not tokens:
            continue
        if tokens[0] == "export":
            tokens = tokens[1:]
        require(len(tokens) == 1 and "=" in tokens[0], f"ENV_SYNTAX line {index}")
        key, value = tokens[0].split("=", 1)
        require(
            re.fullmatch(r"[A-Z_][A-Z0-9_]*", key) and key not in result,
            f"ENV_KEY line {index}",
        )
        result[key] = value
    return result


def required(env, key):
    value = env.get(key)
    require(isinstance(value, str) and bool(value.strip()), f"{key} is empty")
    return value.strip()


def validate_url(value):
    require(isinstance(value, str), "URL_INVALID")
    parsed = urllib.parse.urlsplit(value)
    require(
        parsed.scheme in ("http", "https")
        and parsed.hostname
        and not parsed.username
        and not parsed.password
        and not parsed.fragment
        and not parsed.query,
        "URL_INVALID",
    )
    require(parsed.port is None or 0 < parsed.port < 65536, "URL_INVALID")
    return value.rstrip("/")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def request(url, payload=None):
    data = None if payload is None else json.dumps(payload).encode()
    req = urllib.request.Request(
        url, data=data, headers={"Content-Type": "application/json"}
    )
    try:
        with urllib.request.build_opener(NoRedirect).open(req, timeout=15) as response:
            body = response.read(2 * 1024 * 1024 + 1)
            require(len(body) <= 2 * 1024 * 1024, "HTTP_BODY_LIMIT")
            return response.status, body
    except urllib.error.HTTPError as error:
        error.close()
        return error.code, b""
    except (
        urllib.error.URLError,
        http.client.HTTPException,
        TimeoutError,
        OSError,
    ) as error:
        raise CheckError("HTTP_UNAVAILABLE") from error


def rpc(url, method, params):
    status, body = request(
        url, {"jsonrpc": "2.0", "id": 1, "method": method, "params": params}
    )
    require(status == 200, f"RPC_HTTP_{status}")
    return decode_rpc_response(body)


def decode_rpc_response(body):
    response = json.loads(body)
    require(
        isinstance(response, dict)
        and response.get("id") == 1
        and response.get("jsonrpc") == "2.0",
        "RPC_ENVELOPE",
    )
    require("error" not in response and "result" in response, "RPC_ERROR")
    require(response["result"] is not None, "RPC_EMPTY_RESULT")
    return response["result"]


class Node:
    def __init__(self, row, binary):
        require(
            isinstance(row, dict)
            and row.get("role") in ("validator", "full", "archive", "edge"),
            "INVENTORY_ROLE",
        )
        self.role = row["role"]
        self.consensus_rpc = validate_url(row["consensus_rpc"])
        self.evm_rpc = validate_url(row["evm_rpc"])
        self.binary = binary
        self.height = "latest"

    def consensus(self, method, params=None):
        return rpc(self.consensus_rpc, method, {} if params is None else params)

    def view(self, address, signature, argument=None):
        return rpc(
            self.evm_rpc,
            "eth_call",
            [{"to": address, "data": call_data(signature, argument)}, self.height],
        )

    def query(self, *args):
        try:
            result = subprocess.run(
                [
                    str(self.binary),
                    "q",
                    *args,
                    "--node",
                    self.consensus_rpc,
                    "-o",
                    "json",
                ],
                capture_output=True,
                timeout=30,
                check=False,
            )
        except (OSError, subprocess.TimeoutExpired) as error:
            raise CheckError("PAXD_QUERY_UNAVAILABLE") from error
        require(result.returncode == 0, f"PAXD_QUERY_EXIT_{result.returncode}")
        return json.loads(result.stdout)

    def validators(self):
        validators = []
        seen = set()
        page_key = ""
        for _ in range(100):
            args = [
                "staking",
                "validators",
                "--limit",
                "100",
                "--height",
                str(int(self.height, 16)),
            ]
            if page_key:
                args += ["--page-key", page_key]
            result = self.query(*args)
            for validator in result["validators"]:
                if validator["status"] == "BOND_STATUS_BONDED":
                    address = operator_address(validator["operator_address"])
                    require(address not in validators, "VALIDATOR_DUPLICATE")
                    validators.append(address)
            page_key = result["pagination"]["next_key"]
            if not page_key:
                require(bool(validators), "VALIDATOR_SET_EMPTY")
                return validators
            require(
                isinstance(page_key, str) and page_key not in seen,
                "VALIDATOR_PAGINATION",
            )
            seen.add(page_key)
        raise CheckError("VALIDATOR_PAGE_LIMIT")


class Outcome:
    def __init__(self):
        self.receipt = {"version": VERSION, "plan": VERSION, "result": "fail"}
        self.errors = []

    def attempt(self, label, action):
        try:
            return action()
        except (CheckError, KeyError, TypeError, ValueError, OSError) as error:
            detail = (
                str(error)
                if isinstance(error, CheckError)
                else "INVALID_RESPONSE_OR_INPUT"
            )
            self.errors.append(f"{label}: {detail}")
            return None

    def count(self, key, value):
        self.receipt[key] = min(self.receipt.get(key, value), value)

    def finish(self):
        self.receipt["result"] = "fail" if self.errors else "pass"
        return self.receipt


def check_service_status(status):
    require(status == 200, f"SERVICE_HTTP_{status}")


def check_build_id(status, body, expected):
    check_service_status(status)
    require(json.loads(body)["buildId"] == expected, "APP_BUILD_ID_MISMATCH")


def service_readiness(statuses, outcome):
    ready = 0
    for index, status in enumerate(statuses):
        before = len(outcome.errors)
        outcome.attempt(f"service {index}", lambda: check_service_status(status))
        ready += len(outcome.errors) == before
    outcome.receipt["services_ready"] = ready


def check_services(env, outcome):
    urls = shlex.split(required(env, "DATA_SERVICE_URLS"))
    require(bool(urls) and len(set(urls)) == len(urls), "SERVICE_URLS_INVALID")
    statuses = []
    for index, url in enumerate(urls):

        def probe():
            status, _ = request(validate_url(url) + "/healthz/ready")
            return status

        statuses.append(outcome.attempt(f"service {index}", probe))
    service_readiness(statuses, outcome)

    def app_probe():
        status, body = request(validate_url(required(env, "APP_URL")) + "/api/build-id")
        check_build_id(status, body, required(env, "APP_BUILD_ID"))

    outcome.attempt("application", app_probe)


def check_served_views(observations, views, outcome, label):
    served = 0
    for row in views:
        before = len(outcome.errors)

        def probe():
            raw = observations[row["address"]]
            require(
                len(decode_hex(raw)) == row["return_bytes"], "PRECOMPILE_RETURN_LENGTH"
            )

        outcome.attempt(f"{label} {row['address']}", probe)
        served += len(outcome.errors) == before
    outcome.count("precompiles_served", served)


def check_no_code(value):
    require(value == "0x", "UNEXPECTED_CODE")


def check_precompiles(node, views, outcome, label):
    observations = {}
    for row in views["served"]:
        observations[row["address"]] = outcome.attempt(
            f"{label} {row['address']}",
            lambda: node.view(row["address"], row["signature"]),
        )
    check_served_views(observations, views["served"], outcome, label)
    for row in views["no_code"]:

        def no_code():
            check_no_code(
                rpc(node.evm_rpc, "eth_getCode", [row["address"], node.height])
            )

        outcome.attempt(f"{label} {row['address']}", no_code)
    for row in views["pointers"]:

        def pointer():
            address = decode_pointer(
                node.view(row["address"], row["signature"], row["denom"])
            )
            require(
                decode_uint(node.view(address, "decimals()")) == 6, "POINTER_DECIMALS"
            )

        outcome.attempt(f"{label} pointer {row['token']}", pointer)
    token = views["usdl"]

    def decimals():
        require(
            decode_uint(node.view(token["address"], token["signature"])) == 6,
            "USDL_DECIMALS",
        )

    outcome.attempt(f"{label} USDL", decimals)


def check_markets(node, views, pools, outcome, label):
    def pool_probe():
        count = decode_uint(node.view(views["served"][1]["address"], "getPoolCount()"))
        outcome.count("pools", count)
        check_pool_count(count, pools)

    outcome.attempt(f"{label} pools", pool_probe)
    counts = []
    for address, signature in (
        (views["served"][0]["address"], "totalMarkets()"),
        (views["served"][2]["address"], "getMarketCount()"),
    ):
        value = outcome.attempt(
            f"{label} {signature}", lambda: decode_uint(node.view(address, signature))
        )
        counts.append(value)
        outcome.count("markets", 0 if value is None else value)
    outcome.attempt(f"{label} markets", lambda: check_market_counts(*counts))


def check_oracle(node, views, outcome, label):
    address = views["served"][2]["address"]
    count = decode_uint(node.view(address, "getMarketCount()"))
    outcome.attempt(
        f"{label} oracle markets",
        lambda: require(count == 18, "MARKET_COUNT_NOT_EIGHTEEN"),
    )
    require(count <= 1024, "MARKET_COUNT_LIMIT")
    prices = []
    for market in range(count):
        prices.append(
            outcome.attempt(
                f"{label} market {market}",
                lambda: node.view(address, "getPrice(uint256)", market),
            )
        )
    check_prices(prices, outcome, label)
    validators = node.validators()
    keys = []
    for index, validator in enumerate(validators):
        keys.append(
            outcome.attempt(
                f"{label} validator {index}",
                lambda: node.view(address, "getFeedKey(address)", validator),
            )
        )
    check_feed_keys(keys, outcome, label)


def check_prices(prices, outcome, label):
    fresh = 0
    for index, price in enumerate(prices):
        before = len(outcome.errors)
        outcome.attempt(
            f"{label} market {index}",
            lambda: require(decode_price(price), "PRICE_STALE"),
        )
        fresh += len(outcome.errors) == before
    outcome.count("oracle_fresh", fresh)


def check_feed_keys(values, outcome, label):
    keys = 0
    for index, value in enumerate(values):
        before = len(outcome.errors)
        outcome.attempt(f"{label} validator {index}", lambda: decode_feed_key(value))
        keys += len(outcome.errors) == before
    outcome.count("feed_keys", keys)


def load_config(env, phase):
    if phase == "services":
        for url in shlex.split(required(env, "DATA_SERVICE_URLS")):
            validate_url(url)
        validate_url(required(env, "APP_URL"))
        required(env, "APP_BUILD_ID")
        return None
    halt = positive_int(required(env, "HALT_HEIGHT"), "HALT_HEIGHT_INVALID")
    chain = required(env, "CHAIN_ID")
    binary = Path(required(env, "BIN_DIR")) / "paxd"
    inventory = json.loads(Path(required(env, "INVENTORY_FILE")).read_text())
    require(
        isinstance(inventory, dict)
        and isinstance(inventory.get("nodes"), list)
        and bool(inventory["nodes"]),
        "INVENTORY_EMPTY",
    )
    nodes = [Node(row, binary) for row in inventory["nodes"]]
    require(
        len({(node.consensus_rpc, node.evm_rpc) for node in nodes}) == len(nodes),
        "INVENTORY_DUPLICATE",
    )
    previous = inventory.get("previous_version")
    if phase in ("preflight", "frozen"):
        require(
            isinstance(previous, str)
            and re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", previous)
            and previous != VERSION,
            "PREVIOUS_VERSION_REQUIRED",
        )
    pools = None
    if phase in ("markets", "full", "post"):
        rehearsal = json.loads(
            (Path(required(env, "RECEIPT_DIR")) / "rehearsal.json").read_text()
        )
        validate_receipt(rehearsal)
        require(
            rehearsal.get("result") == "pass"
            and rehearsal.get("rollback_rehearsed") is True,
            "REHEARSAL_NOT_PASSED",
        )
        require(
            rehearsal.get("new_bin_sha256") == required(env, "RELEASE_SHA256"),
            "REHEARSAL_BINARY_MISMATCH",
        )
        pools = positive_int(
            rehearsal["pools"], "REHEARSAL_POOLS_INVALID", allow_zero=True
        )
    return nodes, halt, chain, previous, pools


def run_phase(env, phase, outcome):
    config = load_config(env, phase)
    if phase == "services":
        check_services(env, outcome)
        return outcome.finish()
    nodes, halt, chain, previous, pools = config
    views = json.loads(Path(__file__).with_name("fleet-views.json").read_text())
    if phase == "validators":
        nodes = [node for node in nodes if node.role == "validator"]
    require(bool(nodes), "INVENTORY_NO_SELECTED_NODES")
    outcome.receipt.update(nodes_checked=len(nodes), nodes_passed=0)
    expected_hash = None
    for index, node in enumerate(nodes):
        label = f"node {index}"
        before = len(outcome.errors)
        status = outcome.attempt(label + " status", lambda: node.consensus("status"))
        info = outcome.attempt(
            label + " abci_info", lambda: node.consensus("abci_info")
        )
        if info is not None:
            outcome.attempt(
                label + " version",
                lambda: check_version(
                    info["response"]["version"],
                    previous if phase in ("preflight", "frozen") else VERSION,
                ),
            )
        if status is not None:

            def status_probe():
                require(status["node_info"]["network"] == chain, "CHAIN_ID_MISMATCH")
                height = positive_int(
                    status["sync_info"]["latest_block_height"], "NODE_HEIGHT"
                )
                node.height = hex(height)
                require(status["sync_info"]["catching_up"] is False, "NODE_CATCHING_UP")
                if phase == "preflight":
                    require(height < halt, "PREFLIGHT_HEIGHT")
                elif phase == "frozen":
                    require(height == halt, "FROZEN_HEIGHT")
                else:
                    require(height > halt + 1, "NODE_HEIGHT_NOT_PAST_UPGRADE")

            outcome.attempt(label + " status", status_probe)
        if phase not in ("preflight", "frozen"):

            def applied():
                check_applied_height_and_sync(
                    node.query("upgrade", "applied", VERSION), status, halt + 1
                )

            outcome.attempt(label + " applied", applied)
            actual = outcome.attempt(
                label + " block",
                lambda: block_hash(
                    node.consensus("block", {"height": str(halt + 1)}), halt + 1
                ),
            )
            if actual is not None:
                if expected_hash is None:
                    expected_hash = actual
                outcome.attempt(
                    label + " hash", lambda: check_block_hash(expected_hash, actual)
                )
            if phase in ("precompiles", "full", "post"):
                outcome.attempt(
                    label + " precompiles",
                    lambda: check_precompiles(node, views, outcome, label),
                )
            if phase in ("markets", "full", "post"):
                outcome.attempt(
                    label + " markets",
                    lambda: check_markets(node, views, pools, outcome, label),
                )
            if phase in ("oracle", "full", "post"):
                outcome.attempt(
                    label + " oracle", lambda: check_oracle(node, views, outcome, label)
                )
        outcome.receipt["nodes_passed"] += len(outcome.errors) == before
    return outcome.finish()


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Verify Paxeer X Network rollout phases without changing chain state.",
        epilog="INVENTORY_FILE is JSON with nodes [{role, consensus_rpc, evm_rpc}] and previous_version for preflight/frozen. Roles: validator, full, archive, edge. Relative paths resolve from the working directory. List env values must be quoted. --check validates local inputs without contacting services or nodes.",
    )
    parser.add_argument("--env-file", required=True, type=Path)
    parser.add_argument("--phase", choices=PHASES, default="post")
    parser.add_argument("--check", action="store_true")
    parser.add_argument("--receipt", type=Path)
    args = parser.parse_args(argv)
    if args.check and args.receipt:
        parser.error("--check cannot write a qualification receipt")
    outcome = Outcome()

    def execute():
        env = read_env(args.env_file)
        if args.check:
            load_config(env, args.phase)
        else:
            run_phase(env, args.phase, outcome)

    outcome.attempt("fleet_check", execute)
    receipt = outcome.finish()
    if args.receipt:
        outcome.attempt("receipt", lambda: write_receipt(args.receipt, receipt))
    for error in outcome.errors:
        print(error, file=sys.stderr)
    print(
        "fleet_check: "
        + ("fail" if outcome.errors else "inputs valid" if args.check else "pass")
    )
    return 1 if outcome.errors else 0


if __name__ == "__main__":
    sys.exit(main())
