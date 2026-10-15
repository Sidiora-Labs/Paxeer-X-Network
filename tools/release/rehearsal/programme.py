#!/usr/bin/env python3
"""Programme checks of the v6.12.0 rehearsal.

Each check_* function runs the programme scenario of one head acceptance against the four
rehearsal nodes after the first upgraded height: it sends the scenario's calls over EVM JSON-RPC
and CometBFT RPC and raises CheckFailed naming the first result that differs from the scenario.

    programme.py <check> --evm URL x4 --comet URL x4 --halt-height HR
                 [--trader-key FILE] [--maker-key FILE] [--feed-key FILE] [--sidecar-pid PID x4]

The trader and maker keys hold USDL ERC20 and PAX on the rehearsal chain; the feed key is the
registered feed key of the first validator, whose sidecar is the first --sidecar-pid.
"""

import argparse
import base64
import hashlib
import importlib.util
import json
import os
import signal
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]

E18 = 10**18
UINT256 = 2**256

LAUNCHPAD = "0x0000000000000000000000000000000000001017"
PERPS = "0x0000000000000000000000000000000000001015"
PRICEFEED = "0x0000000000000000000000000000000000001020"
POINTERVIEW = "0x000000000000000000000000000000000000100a"
SPOT_PECOR = "0x0000000000000000000000000000000000001021"
SPOT_ORDERS = "0x0000000000000000000000000000000000001022"
SPOT_STOP_ORDERS = "0x0000000000000000000000000000000000001023"
SPOT_QUOTER = "0x0000000000000000000000000000000000001024"
SPOT_VAULT = "0x0000000000000000000000000000000000001025"
SPOT_UNUSED = (
    "0x0000000000000000000000000000000000001026",
    "0x0000000000000000000000000000000000001027",
)
USDL = "0x85fcd13735f4309833a503ee804ea32395851479"
USDL_DENOM = "uusdl"
UPGRADE = "v6.12.0"
ZERO_ADDRESS = "0x" + "00" * 20

# Generation 2 role addresses that serve every market after the upgrade (contracts/launchpad.md section 1).
LAUNCHPAD_ROLES = {
    "SidioraFactory": "0x3993d004166738d1069b5c4e0a923f7b9d5d8830",
    "Router": "0x78fb06f525cd964388f2305a489035263e31c6e4",
    "Quoter": "0x793006f4f321b8863f26dbd75b11baa01da181dc",
    "FeesRouter": "0x05681da6f611c2dffd3e92611f6de2d247647927",
    "FeeAccumulator": "0x7b14808b72f61664cdb79d25e6d1587655571a66",
    "PoolRegistry": "0x1dc357d2b7b850332a91f06a9e66634f46fc4ba7",
    "ProtocolConfig": "0xf3669edc475cb6e1f0d7635240abd8e8fb7e20c6",
    "SidioraNFT": "0xe8dcc844a8006dbf64f5bb851bcad90292a6409b",
    "Treasury": "0xbb82d74e071e03d8e68f1d78fae9efe8b6b7a723",
}
RETIRED_SELECTOR = bytes.fromhex("da9d2e31")
ERC721_INTERFACE = bytes.fromhex("80ac58cd")

# Perps enums of contracts/perps.md section 2.
LIMIT, STOP_LIMIT, TAKE_PROFIT, STOP_LOSS, MARKET, STOP_MARKET = range(6)
GTC, IOC, FOK, POST_ONLY = range(4)
UNTRIGGERED, RESTING, FILLED, CANCELLED, EXPIRED = range(5)

# Spot order status of contracts/spot.md.
SPOT_PENDING, SPOT_ACTIVATED, SPOT_EXECUTED, SPOT_CANCELLED, SPOT_EXPIRED = range(5)
PAX_MARKET = 11
# Native PAX carries 18 decimals; its spot pointer token carries 6.
PAX_POINTER_SCALE = 10**12

# The eighteen opening markets of contracts/perps-markets.md: symbol, name, szDecimals, pxDecimals,
# tier class and open-interest cap in USD (class E markets take the listing-age table).
TIER_CLASSES = "ABCDEF"
PERPS_MARKETS = (
    ("BTC", "Bitcoin", 5, 1, "A", 500_000_000),
    ("ETH", "Ethereum", 4, 2, "A", 500_000_000),
    ("SOL", "Solana", 2, 3, "B", 50_000_000),
    ("AVAX", "Avalanche", 2, 3, "B", 50_000_000),
    ("LINK", "Chainlink", 1, 3, "F", 20_000_000),
    ("TSLA", "Tesla", 3, 2, "D", 25_000_000),
    ("NVDA", "NVIDIA", 3, 2, "D", 25_000_000),
    ("NAS100", "Nasdaq 100", 4, 1, "C", 100_000_000),
    ("XAU", "Gold", 4, 1, "C", 100_000_000),
    ("SPX500", "S&P 500", 4, 1, "C", 100_000_000),
    ("GOOGL", "Alphabet", 3, 2, "D", 25_000_000),
    ("PAX", "Paxeer Network", 0, 5, "E", 0),
    ("SID", "Sidiora Markets", 0, 5, "E", 0),
    ("HYPE", "Hyperliquid", 2, 3, "B", 50_000_000),
    ("XRP", "XRP", 0, 4, "B", 50_000_000),
    ("ASTER", "Aster", 0, 5, "E", 0),
    ("TRUMP", "Trump", 1, 3, "E", 0),
    ("BNB", "Binance Smart Chain", 3, 2, "B", 50_000_000),
)
CLASS_E_LAUNCH_OI_CAP_USD = 2_000_000
CLASS_E_LAUNCH_DAYS = 30
MAX_LEVERAGE = 1000
FUNDING_CAP_PPM_PER_HOUR = 40_000
MAINTENANCE_NUMERATOR_BPS = 5_000
TIER_SHEETS = {
    "A": (
        (0, 1000),
        (1_000_000, 500),
        (5_000_000, 200),
        (20_000_000, 100),
        (50_000_000, 40),
        (150_000_000, 20),
    ),
    "B": (
        (0, 1000),
        (500_000, 200),
        (2_000_000, 50),
        (10_000_000, 20),
        (30_000_000, 10),
    ),
    "C": ((0, 1000), (1_000_000, 200), (5_000_000, 50), (20_000_000, 20)),
    "D": ((0, 1000), (250_000, 100), (1_000_000, 20), (5_000_000, 10)),
    "E": ((0, 1000), (10_000, 100), (50_000, 20), (250_000, 10), (1_000_000, 5)),
    "F": ((0, 1000), (250_000, 50), (1_000_000, 20), (5_000_000, 10)),
}
# Perps economics of contracts/perps-parameters.md.
TAKER_FEE_PPM = 450
MAKER_FEE_PPM = 150
LIQUIDATION_FEE_PPM = 0
INSURANCE_SHARE_PPM = 0
USD_PER_USDL_UNIT = 10**12
FUNDING_INTERVAL_SECONDS = 3600

# Price feed submissions (modules/pricefeed/types).
PRICEFEED_CODESPACE = "pricefeed"
CODE_RATE_LIMITED = 11
CODE_TOO_MANY_OBSERVATIONS = 12
CODE_SUBMISSION_TOO_LARGE = 13
MAX_OBSERVATIONS_PER_MESSAGE = 64
SUBMIT_PRICES_URL = "/paxprotocol.paxchain.pricefeed.MsgSubmitPrices"
SECP256K1_PUBKEY_URL = "/cosmos.crypto.secp256k1.PubKey"
BASE_ACCOUNT_URL = "/cosmos.auth.v1beta1.BaseAccount"
SIGN_MODE_DIRECT = 1
SUBMISSION_GAS = 200_000
BECH32_PREFIX = "pax"
SIDECAR_BLOCKS = 10


class CheckFailed(Exception):
    """A programme scenario returned a result other than the one its acceptance names."""


def require(condition, message):
    if not condition:
        raise CheckFailed(message)


# --- keccak256 --------------------------------------------------------------------------------


def _rol64(value, shift):
    shift %= 64
    return ((value << shift) | (value >> (64 - shift))) & 0xFFFFFFFFFFFFFFFF


def _keccak_f1600(lanes):
    r = 1
    for _ in range(24):
        c = [
            lanes[x][0] ^ lanes[x][1] ^ lanes[x][2] ^ lanes[x][3] ^ lanes[x][4]
            for x in range(5)
        ]
        d = [c[(x + 4) % 5] ^ _rol64(c[(x + 1) % 5], 1) for x in range(5)]
        lanes = [[lanes[x][y] ^ d[x] for y in range(5)] for x in range(5)]
        x, y = 1, 0
        current = lanes[x][y]
        for t in range(24):
            x, y = y, (2 * x + 3 * y) % 5
            current, lanes[x][y] = lanes[x][y], _rol64(current, (t + 1) * (t + 2) // 2)
        for y in range(5):
            row = [lanes[x][y] for x in range(5)]
            for x in range(5):
                lanes[x][y] = row[x] ^ ((~row[(x + 1) % 5]) & row[(x + 2) % 5])
        for j in range(7):
            r = ((r << 1) ^ ((r >> 7) * 0x71)) % 256
            if r & 2:
                lanes[0][0] ^= 1 << ((1 << j) - 1)
    return lanes


def keccak256(data):
    """Keccak-256 with the original 0x01 padding, as the EVM uses it."""
    rate = 136
    padded = bytearray(data) + b"\x01"
    padded += b"\x00" * (-len(padded) % rate)
    padded[-1] |= 0x80
    lanes = [[0] * 5 for _ in range(5)]
    for offset in range(0, len(padded), rate):
        block = padded[offset : offset + rate]
        for i in range(rate // 8):
            lanes[i % 5][i // 5] ^= int.from_bytes(block[8 * i : 8 * i + 8], "little")
        lanes = _keccak_f1600(lanes)
    return b"".join(lanes[i % 5][i // 5].to_bytes(8, "little") for i in range(4))


def selector(signature):
    return keccak256(signature.encode())[:4]


def event_topic(signature):
    return keccak256(signature.encode())


# --- ABI encoding -----------------------------------------------------------------------------


def split_types(text):
    """Splits a comma-separated type list at depth zero."""
    parts, depth, start = [], 0, 0
    for i, char in enumerate(text):
        if char == "(":
            depth += 1
        elif char == ")":
            depth -= 1
        elif char == "," and depth == 0:
            parts.append(text[start:i])
            start = i + 1
    if text:
        parts.append(text[start:])
    return parts


def parse_type(text):
    if text.endswith("]"):
        open_at = text.rindex("[")
        length = text[open_at + 1 : -1]
        return ("array", parse_type(text[:open_at]), int(length) if length else None)
    if text.startswith("("):
        return ("tuple", [parse_type(part) for part in split_types(text[1:-1])])
    return ("base", text)


def is_dynamic(kind):
    if kind[0] == "base":
        return kind[1] in ("bytes", "string")
    if kind[0] == "array":
        return kind[2] is None or is_dynamic(kind[1])
    return any(is_dynamic(child) for child in kind[1])


def head_size(kind):
    if is_dynamic(kind):
        return 32
    if kind[0] == "array":
        return kind[2] * head_size(kind[1])
    if kind[0] == "tuple":
        return sum(head_size(child) for child in kind[1])
    return 32


def address_bytes(value):
    if isinstance(value, (bytes, bytearray)):
        raw = bytes(value)
    else:
        text = value[2:] if value.startswith(("0x", "0X")) else value
        raw = bytes.fromhex(text)
    if len(raw) != 20:
        raise ValueError(f"{value!r} is not a 20-byte address")
    return raw


def word(value):
    return (value % UINT256).to_bytes(32, "big")


def encode_base(name, value):
    if name.startswith("uint"):
        bits = int(name[4:] or 256)
        if not 0 <= value < 2**bits:
            raise ValueError(f"{value} outside {name}")
        return word(value)
    if name.startswith("int"):
        bits = int(name[3:] or 256)
        if not -(2 ** (bits - 1)) <= value < 2 ** (bits - 1):
            raise ValueError(f"{value} outside {name}")
        return word(value)
    if name == "address":
        return b"\x00" * 12 + address_bytes(value)
    if name == "bool":
        return word(1 if value else 0)
    if name in ("bytes", "string"):
        data = value.encode() if name == "string" else bytes(value)
        return word(len(data)) + data + b"\x00" * (-len(data) % 32)
    if name.startswith("bytes"):
        size = int(name[5:])
        data = bytes(value)
        if len(data) != size:
            raise ValueError(f"{name} takes {size} bytes, got {len(data)}")
        return data + b"\x00" * (32 - size)
    raise ValueError(f"unsupported ABI type {name}")


def encode_kind(kind, value):
    if kind[0] == "base":
        return encode_base(kind[1], value)
    values = list(value)
    if kind[0] == "array":
        if kind[2] is None:
            return word(len(values)) + encode_sequence([kind[1]] * len(values), values)
        if len(values) != kind[2]:
            raise ValueError(
                f"array of {kind[2]} takes {kind[2]} values, got {len(values)}"
            )
        return encode_sequence([kind[1]] * len(values), values)
    if len(values) != len(kind[1]):
        raise ValueError(
            f"tuple of {len(kind[1])} takes {len(kind[1])} values, got {len(values)}"
        )
    return encode_sequence(kind[1], values)


def encode_sequence(kinds, values):
    heads, tails = [], []
    offset = sum(head_size(kind) for kind in kinds)
    for kind, value in zip(kinds, values):
        encoded = encode_kind(kind, value)
        if is_dynamic(kind):
            heads.append(word(offset))
            tails.append(encoded)
            offset += len(encoded)
        else:
            heads.append(encoded)
    return b"".join(heads) + b"".join(tails)


def encode_values(types, values):
    values = list(values)
    if len(values) != len(types):
        raise ValueError(
            f"{len(types)} ABI types take {len(types)} values, got {len(values)}"
        )
    return encode_sequence([parse_type(t) for t in types], values)


def _read_word(data, at):
    if at + 32 > len(data):
        raise ValueError(f"ABI data of {len(data)} bytes ends before word at {at}")
    return int.from_bytes(data[at : at + 32], "big")


def decode_base(name, data, at):
    value = _read_word(data, at)
    if name.startswith("uint"):
        bits = int(name[4:] or 256)
        if value >= 2**bits:
            raise ValueError(f"{value} outside {name}")
        return value
    if name.startswith("int"):
        bits = int(name[3:] or 256)
        signed = value - UINT256 if value >= 2**255 else value
        if not -(2 ** (bits - 1)) <= signed < 2 ** (bits - 1):
            raise ValueError(f"{signed} outside {name}")
        return signed
    if name == "address":
        if value >> 160:
            raise ValueError("address word carries high bytes")
        return "0x" + bytes(data[at + 12 : at + 32]).hex()
    if name == "bool":
        if value > 1:
            raise ValueError(f"bool word {value}")
        return value == 1
    if name in ("bytes", "string"):
        end = at + 32 + value
        if end > len(data):
            raise ValueError(f"{name} of {value} bytes runs past the data")
        raw = bytes(data[at + 32 : end])
        return raw.decode() if name == "string" else raw
    if name.startswith("bytes"):
        return bytes(data[at : at + int(name[5:])])
    raise ValueError(f"unsupported ABI type {name}")


def decode_kind(kind, data, at):
    """Decodes kind whose encoding starts at data[at] (the head for static, the tail for dynamic)."""
    if kind[0] == "base":
        return decode_base(kind[1], data, at)
    if kind[0] == "array":
        if kind[2] is None:
            return decode_sequence([kind[1]] * _read_word(data, at), data[at + 32 :])
        return decode_sequence([kind[1]] * kind[2], data[at:])
    return tuple(decode_sequence(kind[1], data[at:]))


def decode_sequence(kinds, data):
    values, at = [], 0
    for kind in kinds:
        if is_dynamic(kind):
            values.append(decode_kind(kind, data, _read_word(data, at)))
        else:
            values.append(decode_kind(kind, data, at))
        at += head_size(kind)
    return values


def decode_values(types, data):
    return tuple(decode_sequence([parse_type(t) for t in types], bytes(data)))


def zero_value(kind):
    if kind[0] == "array":
        return [] if kind[2] is None else [zero_value(kind[1])] * kind[2]
    if kind[0] == "tuple":
        return tuple(zero_value(child) for child in kind[1])
    name = kind[1]
    if name == "address":
        return ZERO_ADDRESS
    if name == "bool":
        return False
    if name == "string":
        return ""
    if name == "bytes":
        return b""
    if name.startswith("bytes"):
        return b"\x00" * int(name[5:])
    return 0


def canonical_type(param):
    kind = param["type"]
    if kind.startswith("tuple"):
        return (
            "("
            + ",".join(canonical_type(c) for c in param["components"])
            + ")"
            + kind[5:]
        )
    return kind


class Function:
    def __init__(self, name, inputs, outputs, mutability="nonpayable"):
        self.name = name
        self.inputs = list(inputs)
        self.outputs = list(outputs)
        self.mutability = mutability
        self.signature = f"{name}({','.join(self.inputs)})"
        self.selector = selector(self.signature)

    def encode(self, *values):
        return self.selector + encode_values(self.inputs, values)

    def decode(self, data):
        return decode_values(self.outputs, data)

    def zero_args(self, address=ZERO_ADDRESS):
        """Zero arguments, with address arguments set to address."""
        values = []
        for text in self.inputs:
            kind = parse_type(text)
            values.append(address if kind == ("base", "address") else zero_value(kind))
        return values


class Event:
    def __init__(self, name, inputs):
        self.name = name
        self.inputs = list(inputs)  # (type, indexed, name)
        self.signature = f"{name}({','.join(t for t, _, _ in self.inputs)})"
        self.topic = event_topic(self.signature)

    def decode(self, log):
        topics = [bytes.fromhex(t[2:]) for t in log["topics"]]
        if not topics or topics[0] != self.topic:
            raise ValueError(f"log is not {self.signature}")
        indexed = [entry for entry in self.inputs if entry[1]]
        if len(topics) != 1 + len(indexed):
            raise ValueError(
                f"{self.signature} log carries {len(topics) - 1} indexed topics"
            )
        plain = decode_values(
            [t for t, idx, _ in self.inputs if not idx], bytes.fromhex(log["data"][2:])
        )
        out, topic_at, plain_at = {}, 1, 0
        for kind, idx, name in self.inputs:
            if idx:
                dynamic = is_dynamic(parse_type(kind))
                out[name] = (
                    topics[topic_at]
                    if dynamic
                    else decode_base(kind, topics[topic_at], 0)
                )
                topic_at += 1
            else:
                out[name] = plain[plain_at]
                plain_at += 1
        return out


class Abi:
    """The functions, events and custom errors of one ABI JSON document."""

    def __init__(self, entries):
        self.functions, self.events, self.errors = {}, {}, {}
        for entry in entries:
            kind = entry.get("type")
            if kind == "function":
                fn = Function(
                    entry["name"],
                    [canonical_type(i) for i in entry["inputs"]],
                    [canonical_type(o) for o in entry.get("outputs", [])],
                    entry.get("stateMutability", "nonpayable"),
                )
                self.functions.setdefault(entry["name"], []).append(fn)
            elif kind == "event":
                inputs = [
                    (canonical_type(i), bool(i.get("indexed")), i["name"])
                    for i in entry["inputs"]
                ]
                self.events[entry["name"]] = Event(entry["name"], inputs)
            elif kind == "error":
                signature = f"{entry['name']}({','.join(canonical_type(i) for i in entry['inputs'])})"
                self.errors[selector(signature)] = signature

    @classmethod
    def load(cls, relative):
        with open(ROOT / relative, encoding="utf-8") as handle:
            document = json.load(handle)
        return cls(document["abi"] if isinstance(document, dict) else document)

    def fn(self, name, arity=None):
        candidates = [
            f
            for f in self.functions.get(name, [])
            if arity is None or len(f.inputs) == arity
        ]
        if len(candidates) != 1:
            raise KeyError(f"{name}/{arity} names {len(candidates)} functions")
        return candidates[0]

    def every_function(self):
        return [fn for overloads in self.functions.values() for fn in overloads]

    def topics(self):
        return {ev.topic for ev in self.events.values()}


def error_selector(name):
    """Selector of a parameterless custom error."""
    return selector(f"{name}()")


ABI_PATHS = {
    "perps": "precompiles/perps/abi/abi.json",
    "pricefeed": "precompiles/pricefeed/abi/abi.json",
    "pointerview": "precompiles/pointerview/abi.json",
    "pecor": "precompiles/spot/abi/pecor.json",
    "orders": "precompiles/spot/abi/orders.json",
    "stoporders": "precompiles/spot/abi/stoporders.json",
    "quoter": "precompiles/spot/abi/quoter.json",
    "vault": "precompiles/spot/abi/vault.json",
    "share": "precompiles/spot/abi/share.json",
}
LAUNCHPAD_ABI_ROLES = (
    "FeeAccumulator",
    "FeesRouter",
    "PoolRegistry",
    "ProtocolConfig",
    "Quoter",
    "Router",
    "SidioraERC20",
    "SidioraFactory",
    "SidioraNFT",
    "SidioraPool",
    "Treasury",
)
SPOT_ADDRESSES = {
    "pecor": SPOT_PECOR,
    "orders": SPOT_ORDERS,
    "stoporders": SPOT_STOP_ORDERS,
    "quoter": SPOT_QUOTER,
    "vault": SPOT_VAULT,
}
_ABIS = {}


def abi(name):
    if name not in _ABIS:
        _ABIS[name] = Abi.load(
            ABI_PATHS.get(name, f"precompiles/launchpad/abi/{name}.json")
        )
    return _ABIS[name]


def _erc20_entries():
    def fn(name, inputs, output):
        return {
            "type": "function",
            "name": name,
            "inputs": [{"type": t} for t in inputs],
            "outputs": [{"type": output}],
        }

    def ev(name, first, second):
        return {
            "type": "event",
            "name": name,
            "inputs": [
                {"type": "address", "name": first, "indexed": True},
                {"type": "address", "name": second, "indexed": True},
                {"type": "uint256", "name": "value", "indexed": False},
            ],
        }

    return [
        fn("balanceOf", ["address"], "uint256"),
        fn("totalSupply", [], "uint256"),
        fn("allowance", ["address", "address"], "uint256"),
        fn("approve", ["address", "uint256"], "bool"),
        fn("transfer", ["address", "uint256"], "bool"),
        ev("Transfer", "from", "to"),
        ev("Approval", "owner", "spender"),
    ]


ERC20 = Abi(_erc20_entries())


# --- golden selector files --------------------------------------------------------------------


def launchpad_golden():
    """(role, signature, id) lines of precompiles/launchpad/testdata/abi_golden.txt."""
    with open(
        ROOT / "precompiles/launchpad/testdata/abi_golden.txt", encoding="utf-8"
    ) as handle:
        return [tuple(line.split()) for line in handle if line.strip()]


def launchpad_golden_topics():
    return {ident for _, _, ident in launchpad_golden() if len(ident) == 66}


def perps_golden():
    """(kind, signature, id) lines of precompiles/perps/testdata/abi_golden.txt."""
    with open(
        ROOT / "precompiles/perps/testdata/abi_golden.txt", encoding="utf-8"
    ) as handle:
        return [tuple(line.split()) for line in handle if line.strip()]


def spot_golden(name):
    with open(
        ROOT / f"precompiles/spot/testdata/{name}.golden.json", encoding="utf-8"
    ) as handle:
        return json.load(handle)


def module_address(name):
    """EVM address of a module account without an associated EVM address: its 20 raw bytes."""
    return "0x" + hashlib.sha256(name.encode()).digest()[:20].hex()


# --- secp256k1 signing (platform/hosted/paxeer/evm.py) -----------------------------------------


def _load_evm():
    spec = importlib.util.spec_from_file_location(
        "paxeer_evm", ROOT / "platform/hosted/paxeer/evm.py"
    )
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


evm = _load_evm()


# --- JSON-RPC ---------------------------------------------------------------------------------


class RpcError(Exception):
    def __init__(self, url, method, error):
        super().__init__(f"{method} at {url}: {json.dumps(error)}")
        self.error = error


class Revert(Exception):
    def __init__(self, data, message=""):
        super().__init__(f"reverted with 0x{data.hex()} {message}".strip())
        self.data = data


def rpc(url, method, params, timeout=30):
    body = json.dumps(
        {"jsonrpc": "2.0", "id": 1, "method": method, "params": params}
    ).encode()
    request = urllib.request.Request(
        url, data=body, headers={"content-type": "application/json"}
    )
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            reply = json.load(response)
    except (urllib.error.URLError, OSError, ValueError) as err:
        raise CheckFailed(f"{method} at {url}: {err}") from err
    if reply.get("error") is not None:
        raise RpcError(url, method, reply["error"])
    return reply.get("result")


def revert_data(error):
    """The revert data of an eth_call or eth_estimateGas error, or None for any other error."""
    data = error.get("data")
    if isinstance(data, dict):
        data = data.get("data")
    if isinstance(data, str) and data.startswith("0x"):
        return bytes.fromhex(data[2:])
    if "revert" in str(error.get("message", "")):
        return b""
    return None


def block_tag(block):
    return block if isinstance(block, str) else hex(block)


class Node:
    def __init__(self, index, evm_url, comet_url):
        self.index = index
        self.evm_url = evm_url
        self.comet_url = comet_url

    def eth(self, method, *params):
        return rpc(self.evm_url, method, list(params))

    def comet(self, method, **params):
        return rpc(self.comet_url, method, params)

    def call(self, to, data, block="latest", sender=None):
        request = {"to": to, "data": "0x" + bytes(data).hex()}
        if sender:
            request["from"] = sender
        try:
            return bytes.fromhex(self.eth("eth_call", request, block_tag(block))[2:])
        except RpcError as err:
            data = revert_data(err.error)
            if data is None:
                raise CheckFailed(str(err)) from err
            raise Revert(data, str(err.error.get("message", ""))) from err

    def height(self):
        return int(self.comet("status")["sync_info"]["latest_block_height"])

    def evm_height(self):
        return int(self.eth("eth_blockNumber"), 16)

    def block_time(self, block="latest"):
        return int(
            self.eth("eth_getBlockByNumber", block_tag(block), False)["timestamp"], 16
        )

    def balance(self, address, block="latest"):
        return int(self.eth("eth_getBalance", address, block_tag(block)), 16)

    def code(self, address, block="latest"):
        return self.eth("eth_getCode", address, block_tag(block))

    def abci_query(self, path, data, height=0):
        params = {"path": path, "data": data.hex(), "prove": False}
        if height:
            params["height"] = str(height)
        response = self.comet("abci_query", **params)["response"]
        require(
            int(response.get("code", 0)) == 0,
            f"abci_query {path} on node {self.index}: {response.get('log')}",
        )
        return base64.b64decode(response.get("value") or "")


class Net:
    """The four rehearsal nodes and the scenario inputs of the command line."""

    def __init__(self, args):
        require(
            len(args.evm) == 4 and len(args.comet) == 4,
            "the programme checks take four --evm and four --comet URLs",
        )
        self.nodes = [
            Node(i, e, c) for i, (e, c) in enumerate(zip(args.evm, args.comet))
        ]
        self.node = self.nodes[0]
        self.halt = args.halt_height
        self.h1 = args.halt_height + 1
        self.args = args
        self._wallets = {}

    def wallet(self, role):
        path = getattr(self.args, f"{role}_key")
        require(path, f"--{role}-key is required by this check")
        if role not in self._wallets:
            self._wallets[role] = Wallet(self, evm.read_key(path))
        return self._wallets[role]

    def settled(self):
        """The highest EVM height all four nodes have reached."""
        return min(n.evm_height() for n in self.nodes)

    def call_raw(self, to, data, block=None, sender=None):
        """('ok' | 'revert', bytes) of one call, required equal on the four nodes."""
        block = self.settled() if block is None else block
        results = []
        for n in self.nodes:
            try:
                results.append(("ok", n.call(to, data, block, sender)))
            except Revert as err:
                results.append(("revert", err.data))
        require(
            all(r == results[0] for r in results),
            f"call {to} 0x{bytes(data[:4]).hex()} at {block} differs between the nodes: "
            + ", ".join(f"{k}:0x{v.hex()[:80]}" for k, v in results),
        )
        return results[0]

    def call(self, to, fn, *values, block=None, sender=None):
        kind, data = self.call_raw(to, fn.encode(*values), block, sender)
        if kind == "revert":
            raise Revert(data, f"{fn.signature} at {to}")
        return fn.decode(data)

    def one(self, to, fn, *values, block=None, sender=None):
        result = self.call(to, fn, *values, block=block, sender=sender)
        require(len(result) == 1, f"{fn.signature} returned {len(result)} values")
        return result[0]

    def expect_revert(self, to, fn, values, error_name, sender=None, block=None):
        kind, data = self.call_raw(to, fn.encode(*values), block, sender)
        expected = error_selector(error_name)
        require(
            kind == "revert", f"{fn.signature} at {to} succeeded, expected {error_name}"
        )
        require(
            data == expected,
            f"{fn.signature} at {to} reverted 0x{data.hex()}, expected {error_name} 0x{expected.hex()}",
        )

    def wait_height(self, height, timeout=180):
        deadline = time.monotonic() + timeout
        while self.settled() < height:
            require(
                time.monotonic() < deadline, f"the nodes did not reach height {height}"
            )
            time.sleep(0.5)

    def wait_comet(self, height, timeout=180):
        deadline = time.monotonic() + timeout
        while min(n.height() for n in self.nodes) < height:
            require(
                time.monotonic() < deadline, f"the nodes did not reach block {height}"
            )
            time.sleep(0.5)

    def logs(self, address, start, end, topics=None, chunk=2000):
        out = []
        for low in range(start, end + 1, chunk):
            query = {
                "address": address,
                "fromBlock": hex(low),
                "toBlock": hex(min(end, low + chunk - 1)),
            }
            if topics:
                query["topics"] = topics
            out.extend(self.node.eth("eth_getLogs", query))
        return out

    def app_hashes_agree(self, height):
        self.wait_comet(height)
        hashes = {
            n.comet("block", height=str(height))["block"]["header"]["app_hash"]
            for n in self.nodes
        }
        require(
            len(hashes) == 1,
            f"the four nodes disagree on the application hash at {height}: {sorted(hashes)}",
        )


class Wallet:
    def __init__(self, net, secret):
        self.net = net
        self.secret = secret
        self.address = "0x" + evm.address_of(secret).hex()

    def send(self, to, data, value=0):
        """Signs, sends and waits for one legacy transaction; requires its receipt status 1."""
        node = self.net.node
        request = {
            "from": self.address,
            "to": to,
            "data": "0x" + bytes(data).hex(),
            "value": hex(value),
        }
        try:
            estimate = int(node.eth("eth_estimateGas", request), 16)
        except RpcError as err:
            raise CheckFailed(
                f"{self.address} to {to} 0x{bytes(data[:4]).hex()} does not estimate: {err}"
            ) from err
        chain_id = int(node.eth("eth_chainId"), 16)
        nonce = int(node.eth("eth_getTransactionCount", self.address, "pending"), 16)
        gas_price = int(node.eth("eth_gasPrice"), 16)
        gas = estimate * 12 // 10 + 21_000
        raw = evm.sign_transaction(
            self.secret,
            chain_id,
            nonce,
            gas_price,
            gas,
            address_bytes(to),
            value,
            bytes(data),
        )
        tx_hash = node.eth("eth_sendRawTransaction", "0x" + raw.hex())
        deadline = time.monotonic() + 120
        while True:
            receipt = node.eth("eth_getTransactionReceipt", tx_hash)
            if receipt is not None:
                break
            require(
                time.monotonic() < deadline, f"transaction {tx_hash} has no receipt"
            )
            time.sleep(0.5)
        receipt["blockNumber"] = int(receipt["blockNumber"], 16)
        require(
            int(receipt["status"], 16) == 1,
            f"transaction {tx_hash} to {to} 0x{bytes(data[:4]).hex()} failed",
        )
        return receipt

    @staticmethod
    def gas_cost(receipt):
        return int(receipt["gasUsed"], 16) * int(receipt["effectiveGasPrice"], 16)


def same_address(a, b):
    return a.lower() == b.lower()


def _same(a, b):
    if isinstance(a, str) and isinstance(b, str):
        return a.lower() == b.lower()
    return a == b


def events(receipt_or_logs, address, event):
    logs = (
        receipt_or_logs["logs"]
        if isinstance(receipt_or_logs, dict)
        else receipt_or_logs
    )
    topic = "0x" + event.topic.hex()
    return [
        event.decode(log)
        for log in logs
        if same_address(log["address"], address) and log["topics"][:1] == [topic]
    ]


def one_event(receipt, address, event, **match):
    found = [
        e
        for e in events(receipt, address, event)
        if all(_same(e[k], v) for k, v in match.items())
    ]
    require(
        len(found) == 1,
        f"{event.signature} at {address} matching {match} appears {len(found)} times",
    )
    return found[0]


def erc20_balance(net, token, holder, block=None):
    return net.one(token, ERC20.fn("balanceOf"), holder, block=block)


def approve(wallet, token, spender, amount):
    receipt = wallet.send(token, ERC20.fn("approve").encode(spender, amount))
    one_event(
        receipt,
        token,
        ERC20.events["Approval"],
        owner=wallet.address,
        spender=spender,
        value=amount,
    )
    return receipt


def ceil_div(a, b):
    return -(-a // b)


def tick_down(value, step):
    return value - value % step


def tick_up(value, step):
    return ceil_div(value, step) * step


# --- protobuf, bech32 and the price submission transaction ------------------------------------


def pb_varint(value):
    out = bytearray()
    while True:
        byte = value & 0x7F
        value >>= 7
        if value:
            out.append(byte | 0x80)
        else:
            out.append(byte)
            return bytes(out)


def pb_uint(field, value):
    return pb_varint(field << 3) + pb_varint(value) if value else b""


def pb_bytes(field, value):
    value = value.encode() if isinstance(value, str) else bytes(value)
    return pb_varint(field << 3 | 2) + pb_varint(len(value)) + value if value else b""


def pb_message(field, value):
    """A sub-message field, written even when empty."""
    return pb_varint(field << 3 | 2) + pb_varint(len(value)) + value


def pb_fields(data):
    """[(field, wire type, value)] of one protobuf message; value is an int or bytes."""
    out, at = [], 0

    def varint():
        nonlocal at
        shift, value = 0, 0
        while True:
            require(at < len(data), "protobuf varint runs past the message")
            byte = data[at]
            at += 1
            value |= (byte & 0x7F) << shift
            shift += 7
            if not byte & 0x80:
                return value

    while at < len(data):
        key = varint()
        field, wire = key >> 3, key & 7
        if wire == 0:
            out.append((field, 0, varint()))
        elif wire == 2:
            length = varint()
            require(at + length <= len(data), "protobuf field runs past the message")
            out.append((field, 2, bytes(data[at : at + length])))
            at += length
        elif wire in (1, 5):
            size = 8 if wire == 1 else 4
            out.append((field, wire, bytes(data[at : at + size])))
            at += size
        else:
            raise CheckFailed(f"protobuf wire type {wire}")
    return out


def pb_get(fields, number, default=None):
    found = [v for f, _, v in fields if f == number]
    return found[0] if found else default


def pb_all(fields, number):
    return [v for f, _, v in fields if f == number]


_BECH32 = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"


def _bech32_polymod(values):
    gen = (0x3B6A57B2, 0x26508E6D, 0x1EA119FA, 0x3D4233DD, 0x2A1462B3)
    chk = 1
    for value in values:
        top = chk >> 25
        chk = (chk & 0x1FFFFFF) << 5 ^ value
        for i in range(5):
            chk ^= gen[i] if (top >> i) & 1 else 0
    return chk


def bech32_encode(prefix, data):
    acc, bits, five = 0, 0, []
    for byte in data:
        acc = (acc << 8) | byte
        bits += 8
        while bits >= 5:
            bits -= 5
            five.append((acc >> bits) & 31)
    if bits:
        five.append((acc << (5 - bits)) & 31)
    expanded = [ord(c) >> 5 for c in prefix] + [0] + [ord(c) & 31 for c in prefix]
    polymod = _bech32_polymod(expanded + five + [0] * 6) ^ 1
    checksum = [(polymod >> 5 * (5 - i)) & 31 for i in range(6)]
    return prefix + "1" + "".join(_BECH32[d] for d in five + checksum)


def feed_account(public_key):
    """The account of a compressed secp256k1 key: ripemd160(sha256(key))."""
    return hashlib.new("ripemd160", hashlib.sha256(public_key).digest()).digest()


def observation_bytes(
    market_id, sequence, index_price, mark_price, source_timestamp_ms
):
    return (
        pb_uint(1, market_id)
        + pb_uint(2, sequence)
        + pb_bytes(3, str(index_price))
        + pb_bytes(4, str(mark_price))
        + pb_uint(5, source_timestamp_ms)
    )


def submission_body(feeder, observations, memo=""):
    msg = pb_bytes(1, feeder) + b"".join(
        pb_message(2, observation_bytes(*o)) for o in observations
    )
    return pb_message(
        1, pb_bytes(1, SUBMIT_PRICES_URL) + pb_message(2, msg)
    ) + pb_bytes(2, memo)


def submission_auth_info(public_key, sequence, gas_limit):
    key = pb_message(
        1, pb_bytes(1, SECP256K1_PUBKEY_URL) + pb_message(2, pb_bytes(1, public_key))
    )
    mode = pb_message(2, pb_message(1, pb_uint(1, SIGN_MODE_DIRECT)))
    return pb_message(1, key + mode + pb_uint(3, sequence)) + pb_message(
        2, pb_uint(2, gas_limit)
    )


def sign_doc(body, auth_info, chain_id, account_number):
    return (
        pb_message(1, body)
        + pb_message(2, auth_info)
        + pb_bytes(3, chain_id)
        + pb_uint(4, account_number)
    )


def tx_raw(body, auth_info, signature):
    return pb_message(1, body) + pb_message(2, auth_info) + pb_message(3, signature)


def submission_parts(
    secret, chain_id, account_number, sequence, gas_limit, observations, memo=""
):
    """(body, auth info, sign doc) of a MsgSubmitPrices in SIGN_MODE_DIRECT."""
    public_key = evm.compressed_public_key(secret)
    feeder = bech32_encode(BECH32_PREFIX, feed_account(public_key))
    body = submission_body(feeder, observations, memo)
    auth = submission_auth_info(public_key, sequence, gas_limit)
    return body, auth, sign_doc(body, auth, chain_id, account_number)


def sign_submission(
    secret, chain_id, account_number, sequence, gas_limit, observations, memo=""
):
    """(sign doc, tx bytes) of a signed MsgSubmitPrices."""
    body, auth, doc = submission_parts(
        secret, chain_id, account_number, sequence, gas_limit, observations, memo
    )
    _, r, s = evm.sign_digest(secret, hashlib.sha256(doc).digest())
    return doc, tx_raw(body, auth, r.to_bytes(32, "big") + s.to_bytes(32, "big"))


def decode_tx(raw):
    """The messages, signer public keys, fee coins and memo of a TxRaw."""
    fields = pb_fields(raw)
    body = pb_fields(pb_get(fields, 1, b""))
    auth = pb_fields(pb_get(fields, 2, b""))
    messages = []
    for any_bytes in pb_all(body, 1):
        any_fields = pb_fields(any_bytes)
        messages.append(
            (pb_get(any_fields, 1, b"").decode(), pb_get(any_fields, 2, b""))
        )
    keys = []
    for signer in pb_all(auth, 1):
        key_any = pb_fields(pb_get(pb_fields(signer), 1, b""))
        keys.append(pb_get(pb_fields(pb_get(key_any, 2, b"")), 1, b""))
    fee = pb_fields(pb_get(auth, 2, b""))
    coins = [
        (pb_get(pb_fields(c), 1, b"").decode(), pb_get(pb_fields(c), 2, b"").decode())
        for c in pb_all(fee, 1)
    ]
    return {
        "messages": messages,
        "keys": keys,
        "fee": coins,
        "memo": pb_get(body, 2, b"").decode(),
    }


def decode_submission(value):
    """(feeder, observations) of a MsgSubmitPrices."""
    fields = pb_fields(value)
    observations = []
    for raw in pb_all(fields, 2):
        o = pb_fields(raw)
        observations.append(
            {
                "market_id": pb_get(o, 1, 0),
                "sequence": pb_get(o, 2, 0),
                "index_price": pb_get(o, 3, b"").decode(),
                "mark_price": pb_get(o, 4, b"").decode(),
                "source_timestamp_ms": pb_get(o, 5, 0),
            }
        )
    return pb_get(fields, 1, b"").decode(), observations


def query_account(node, address):
    """(account number, sequence) of a BaseAccount."""
    value = node.abci_query("/cosmos.auth.v1beta1.Query/Account", pb_bytes(1, address))
    any_fields = pb_fields(pb_get(pb_fields(value), 1, b""))
    require(
        pb_get(any_fields, 1, b"").decode() == BASE_ACCOUNT_URL,
        f"{address} is not a base account",
    )
    account = pb_fields(pb_get(any_fields, 2, b""))
    return pb_get(account, 3, 0), pb_get(account, 4, 0)


def supply_of(node, denom):
    value = node.abci_query("/cosmos.bank.v1beta1.Query/SupplyOf", pb_bytes(1, denom))
    coin = pb_fields(pb_get(pb_fields(value), 1, b""))
    return int(pb_get(coin, 2, b"0").decode() or "0")


def applied_plan_height(node, name):
    value = node.abci_query(
        "/cosmos.upgrade.v1beta1.Query/AppliedPlan", pb_bytes(1, name)
    )
    return pb_get(pb_fields(value), 1, 0)


def broadcast_sync(node, raw):
    result = node.comet("broadcast_tx_sync", tx=base64.b64encode(raw).decode())
    return (
        int(result.get("code", 0)),
        result.get("codespace", ""),
        result.get("log", ""),
    )


def block_txs(node, height):
    block = node.comet("block", height=str(height))["block"]
    results = node.comet("block_results", height=str(height))
    tx_results = results.get("txs_results") or results.get("tx_results") or []
    raws = [base64.b64decode(t) for t in block["data"].get("txs") or []]
    require(
        len(raws) == len(tx_results),
        f"block {height} carries {len(raws)} txs and {len(tx_results)} results",
    )
    return list(zip(raws, tx_results))


# --- shared readers ---------------------------------------------------------------------------


def pricefeed_ids(net):
    """{perps market id: pricefeed market id} through the oracle symbol of every opening market."""
    out = {}
    for market_id in range(len(PERPS_MARKETS)):
        config = net.one(PERPS, abi("perps").fn("getMarketConfig"), market_id)
        out[market_id] = net.one(
            PRICEFEED, abi("pricefeed").fn("marketIdOf"), config[3]
        )
    return out


def pricefeed_price(net, market_id, block=None):
    return net.one(PRICEFEED, abi("pricefeed").fn("getPrice"), market_id, block=block)


def perps_mark(net, market_id):
    return net.one(PERPS, abi("perps").fn("getMarkPrice"), market_id)


def spot_tokens(net):
    tokens = net.one(SPOT_VAULT, abi("vault").fn("getRegisteredTokens"))
    require(
        len(tokens) == 3,
        f"the spot vault registers {len(tokens)} tokens, expected USDL, PAX and SID",
    )
    require(
        same_address(tokens[0], USDL),
        f"the first spot token is {tokens[0]}, not the USDL ERC20",
    )
    return {"USDL": tokens[0], "PAX": tokens[1], "SID": tokens[2]}


def latest_time(net):
    return net.node.block_time(net.settled())


def perps_order(
    market_id, is_long, order_type, tif, size_usd, leverage=10 * E18, **extra
):
    """The OrderParams tuple of placeOrder."""
    return (
        market_id,
        is_long,
        order_type,
        tif,
        size_usd,
        leverage,
        extra.get("limit", 0),
        extra.get("trigger", 0),
        extra.get("acceptable", 0),
        extra.get("collateral", 0),
        extra.get("reduce_only", False),
        extra.get("tp", 0),
        extra.get("sl", 0),
        extra.get("deadline", 0),
        extra.get("cloid", b"\x00" * 16),
    )


def place_order(wallet, params):
    """(receipt, order id) of one placeOrder."""
    perps = abi("perps")
    receipt = wallet.send(PERPS, perps.fn("placeOrder").encode(params))
    placed = one_event(receipt, PERPS, perps.events["OrderPlaced"], user=wallet.address)
    require(
        placed["marketId"] == params[0] and placed["orderType"] == params[2],
        f"OrderPlaced {placed} for {params}",
    )
    return receipt, placed["orderId"]


def order_status(net, order_id):
    return net.one(PERPS, abi("perps").fn("getOrderDetails"), order_id)[6]


def cancel_order(wallet, order_id):
    perps = abi("perps")
    receipt = wallet.send(PERPS, perps.fn("cancelOrder").encode(order_id))
    one_event(
        receipt,
        PERPS,
        perps.events["OrderCancelled"],
        orderId=order_id,
        user=wallet.address,
    )
    require(
        order_status(wallet.net, order_id) == CANCELLED,
        f"order {order_id} is not CANCELLED after cancelOrder",
    )
    return receipt


def cancel_resting(wallet, order_ids):
    for order_id in order_ids:
        if order_status(wallet.net, order_id) == RESTING:
            cancel_order(wallet, order_id)


def perps_collateral(net, user, block=None):
    return net.one(PERPS, abi("perps").fn("getAccount"), user, block=block)[0]


def ensure_perps_collateral(wallet, amount):
    """Deposits USDL until the account holds amount units of collateral."""
    held = perps_collateral(wallet.net, wallet.address)
    if held < amount:
        approve(wallet, USDL, PERPS, amount - held)
        wallet.send(PERPS, abi("perps").fn("deposit").encode(USDL, amount - held))


def position_of(net, user, market_id):
    return net.one(PERPS, abi("perps").fn("getUserMarketPosition"), user, market_id)


def book_side(net, maker, market_id, is_long, distance_ppm, size_usd):
    """Rests a maker LIMIT GTC order distance_ppm from mark; returns (order id, price)."""
    tick = net.one(PERPS, abi("perps").fn("getMarketConfig"), market_id)[6]
    mark = perps_mark(net, market_id)
    if is_long:
        price = tick_down(mark * (10**6 - distance_ppm) // 10**6, tick)
    else:
        price = tick_up(mark * (10**6 + distance_ppm) // 10**6, tick)
    _, order_id = place_order(
        maker, perps_order(market_id, is_long, LIMIT, GTC, size_usd, limit=price)
    )
    require(
        order_status(net, order_id) == RESTING, f"maker order {order_id} is not RESTING"
    )
    return order_id, price


def layout():
    with open(
        ROOT / "modules/launchpad/keeper/testdata/v6120/layout.json", encoding="utf-8"
    ) as handle:
        return json.load(handle)


def registry_pools(net, registry, block):
    fn = abi("PoolRegistry")
    count = net.one(registry, fn.fn("getPoolCount"), block=block)
    pools = []
    for offset in range(0, count, 100):
        pools.extend(net.one(registry, fn.fn("getAllPools"), offset, 100, block=block))
    require(
        len(pools) == count,
        f"registry {registry} lists {len(pools)} of {count} pools at {block}",
    )
    return pools


def first_gen2_pool(net):
    pools = registry_pools(net, LAUNCHPAD_ROLES["PoolRegistry"], net.halt)
    require(pools, "the generation 2 registry holds no pool for the scenario")
    return pools[0]


def buy_pool_token(net, wallet, pool, usdl_in):
    """Buys the pool token of pool through the Router surface with USDL; returns the receipt."""
    approve(wallet, USDL, LAUNCHPAD, usdl_in)
    deadline = latest_time(net) + 600
    return wallet.send(
        LAUNCHPAD_ROLES["Router"],
        abi("Router").fn("buy").encode(pool, usdl_in, 1, deadline),
    )


# --- req 1: launchpad -------------------------------------------------------------------------


def check_launchpad_interface(net):
    """1.1: every function of every generation 2 role answers at HR+1 as the contract answered at HR."""
    pool = first_gen2_pool(net)
    token = net.one(pool, abi("SidioraPool").fn("tokenAddress"), block=net.halt)
    targets = dict(LAUNCHPAD_ROLES, SidioraPool=pool, SidioraERC20=token)
    golden = {(role, sig): ident for role, sig, ident in launchpad_golden()}
    golden_topics = launchpad_golden_topics()
    differences, compared = [], 0
    for role in LAUNCHPAD_ABI_ROLES:
        for topic in abi(role).topics():
            require(
                "0x" + topic.hex() in golden_topics,
                f"{role} event topic 0x{topic.hex()} is not in the golden file",
            )
        for fn in abi(role).every_function():
            require(
                golden.get((role, fn.signature)) == "0x" + fn.selector.hex(),
                f"{role} {fn.signature} is not in the golden file with selector 0x{fn.selector.hex()}",
            )
            data = fn.encode(*fn.zero_args(pool))
            before = net.call_raw(targets[role], data, net.halt)
            after = net.call_raw(targets[role], data, net.h1)
            compared += 1
            if before != after:
                differences.append(
                    f"{role} {fn.signature}: {before[0]} 0x{before[1].hex()[:72]} -> {after[0]} 0x{after[1].hex()[:72]}"
                )
    require(
        not differences,
        f"{len(differences)} of {compared} calls differ: "
        + "; ".join(differences[:10]),
    )
    print(f"launchpad interface: {compared} calls equal at {net.halt} and {net.h1}")


def check_usdl_erc20_custody(net):
    """1.2: USDL is the ERC20, never minted, no denom or pointer; swaps move it by transferFrom and transfer."""
    module = module_address("launchpad")
    for node in net.nodes:
        require(
            supply_of(node, USDL_DENOM) == 0,
            f"node {node.index} carries a {USDL_DENOM} bank supply",
        )
    pointer = net.call(
        POINTERVIEW, abi("pointerview").fn("getNativePointer"), USDL_DENOM
    )
    require(pointer[2] is False, f"{USDL_DENOM} has a native pointer at {pointer[0]}")
    supply = ERC20.fn("totalSupply")
    require(
        net.one(USDL, supply, block=net.halt) == net.one(USDL, supply, block=net.h1),
        "USDL supply changed at the upgrade",
    )

    transfer = ERC20.events["Transfer"]
    upgrade_moves = events(net.logs(USDL, net.h1, net.h1), USDL, transfer)
    gen1 = layout()["generations"][0]
    for pool in registry_pools(net, gen1["poolRegistry"], net.halt):
        held = erc20_balance(net, USDL, pool, block=net.halt)
        require(
            erc20_balance(net, USDL, pool, block=net.h1) == 0,
            f"generation 1 pool {pool} still holds USDL at {net.h1}",
        )
        moved = [
            e["value"]
            for e in upgrade_moves
            if same_address(e["from"], pool) and same_address(e["to"], module)
        ]
        require(
            moved == ([held] if held else []),
            f"pool {pool} moved {moved} to the module, held {held}",
        )

    trader = net.wallet("trader")
    pool = first_gen2_pool(net)
    amount = 1_000_000
    before_trader = erc20_balance(net, USDL, trader.address)
    before_module = erc20_balance(net, USDL, module)
    receipt = buy_pool_token(net, trader, pool, amount)
    block = receipt["blockNumber"]
    net.wait_height(block)
    moves = events(receipt, USDL, transfer)
    require(moves, "the buy moved no USDL")
    require(
        all(not same_address(m["from"], ZERO_ADDRESS) for m in moves),
        "the buy minted USDL",
    )
    out_of_trader = [m for m in moves if same_address(m["from"], trader.address)]
    require(
        len(out_of_trader) == 1
        and same_address(out_of_trader[0]["to"], module)
        and out_of_trader[0]["value"] == amount,
        f"the buy moved {out_of_trader} out of the trader, expected {amount} to the module",
    )
    module_in = sum(m["value"] for m in moves if same_address(m["to"], module))
    module_out = sum(m["value"] for m in moves if same_address(m["from"], module))
    require(
        erc20_balance(net, USDL, trader.address, block) == before_trader - amount,
        "trader USDL delta differs from the buy",
    )
    require(
        erc20_balance(net, USDL, module, block)
        == before_module + module_in - module_out,
        "launchpad module USDL delta differs from its Transfer logs",
    )
    golden_topics = launchpad_golden_topics()
    for log in receipt["logs"]:
        if (
            same_address(log["address"], LAUNCHPAD)
            or log["address"].lower() in LAUNCHPAD_ROLES.values()
        ):
            require(
                log["topics"][0] in golden_topics,
                f"{log['address']} emitted topic {log['topics'][0]} outside the golden file",
            )
    print(
        f"USDL custody: buy of {amount} moved by transferFrom into {module}, no mint, no denom, no pointer"
    )


def check_pool_parity(net):
    """1.3: every pool of both generations answers at HR+1 with its HR state, reserves capped by held balances."""
    pool_abi, nft = abi("SidioraPool"), abi("SidioraNFT")
    served_nft = LAUNCHPAD_ROLES["SidioraNFT"]
    total = 0
    for generation in layout()["generations"]:
        pools = registry_pools(net, generation["poolRegistry"], net.halt)
        require(
            len(pools) == generation["poolCount"],
            f"generation {generation['generation']} lists {len(pools)} pools",
        )
        total += len(pools)
        collection = net.one(
            generation["sidioraFactory"],
            abi("SidioraFactory").fn("nftContract"),
            block=net.halt,
        )
        for pool in pools:
            book = net.call(pool, pool_abi.fn("getReserves"), block=net.halt)
            after = net.call(pool, pool_abi.fn("getReserves"), block=net.h1)
            token = net.one(pool, pool_abi.fn("tokenAddress"), block=net.halt)
            held_usdl = erc20_balance(net, USDL, pool, block=net.halt)
            held_token = erc20_balance(net, token, pool, block=net.halt)
            expected = (book[0], min(book[1], held_usdl), min(book[2], held_token))
            require(
                after == expected,
                f"pool {pool} reserves {after} at {net.h1}, expected {expected}",
            )
            for name in (
                "accumulatedUsdlFees",
                "accumulatedTokenFees",
                "tokenAddress",
                "currentFeeBps",
            ):
                fn = pool_abi.fn(name)
                require(
                    net.call(pool, fn, block=net.halt)
                    == net.call(pool, fn, block=net.h1),
                    f"pool {pool} {name} differs",
                )
            supply = ERC20.fn("totalSupply")
            require(
                net.call(token, supply, block=net.halt)
                == net.call(token, supply, block=net.h1),
                f"token {token} supply differs",
            )
            right = net.one(collection, nft.fn("poolTokenId"), pool, block=net.halt)
            owner = net.one(collection, nft.fn("ownerOf"), right, block=net.halt)
            served = net.one(served_nft, nft.fn("poolTokenId"), pool, block=net.h1)
            served_owner = net.one(served_nft, nft.fn("ownerOf"), served, block=net.h1)
            require(
                same_address(owner, served_owner),
                f"fee right of {pool}: {owner} at {net.halt}, {served_owner} at {net.h1}",
            )
        if generation["generation"] == 1:
            kind, data = net.call_raw(
                generation["poolRegistry"],
                abi("PoolRegistry").fn("getPoolCount").encode(),
                net.h1,
            )
            replacement = b"\x00" * 12 + address_bytes(LAUNCHPAD_ROLES["PoolRegistry"])
            require(
                kind == "revert" and data == RETIRED_SELECTOR + replacement,
                f"generation 1 registry is not retired at {net.h1}",
            )
    served = net.one(LAUNCHPAD, abi("PoolRegistry").fn("getPoolCount"), block=net.h1)
    require(
        served == total, f"0x1017 serves {served} pools, the registries held {total}"
    )
    with open(
        ROOT / "modules/launchpad/keeper/testdata/v6120/holders.json", encoding="utf-8"
    ) as handle:
        holders = json.load(handle)
    for entry in holders:
        for holder in entry["holders"]:
            before = erc20_balance(net, entry["token"], holder, block=net.halt)
            after = erc20_balance(net, entry["token"], holder, block=net.h1)
            require(
                before == after,
                f"{holder} balance of {entry['token']} is {after}, was {before}",
            )
    print(
        f"pool parity: {total} pools, reserves, fees, supplies, holders and fee rights equal"
    )


def check_fee_right_and_pool_token(net):
    """1.4: fee rights answer as ERC721 and pool tokens as ERC20 with their transfer and approval logs."""
    nft = abi("SidioraNFT")
    collection = LAUNCHPAD_ROLES["SidioraNFT"]
    pool = first_gen2_pool(net)
    right = net.one(collection, nft.fn("poolTokenId"), pool)
    owner = net.one(collection, nft.fn("ownerOf"), right)
    count = net.one(collection, nft.fn("balanceOf"), owner)
    owned = [
        net.one(collection, nft.fn("tokenOfOwnerByIndex"), owner, i)
        for i in range(count)
    ]
    require(
        right in owned, f"fee right {right} is not enumerated for its owner {owner}"
    )
    require(
        same_address(net.one(collection, nft.fn("getPoolAddress"), right), pool),
        f"fee right {right} names another pool",
    )
    require(
        net.one(collection, nft.fn("supportsInterface"), ERC721_INTERFACE) is True,
        "fee rights do not report ERC721",
    )

    trader, maker = net.wallet("trader"), net.wallet("maker")
    token = net.one(pool, abi("SidioraPool").fn("tokenAddress"))
    receipt = buy_pool_token(net, trader, pool, 1_000_000)
    bought = one_event(receipt, token, ERC20.events["Transfer"], to=trader.address)[
        "value"
    ]
    require(bought > 0, "the buy delivered no pool token")
    allowance = bought // 2
    approve(trader, token, maker.address, allowance)
    require(
        net.one(token, ERC20.fn("allowance"), trader.address, maker.address)
        == allowance,
        "pool token allowance differs",
    )
    sender_before = erc20_balance(net, token, trader.address)
    receiver_before = erc20_balance(net, token, maker.address)
    sent = bought // 3
    receipt = trader.send(token, ERC20.fn("transfer").encode(maker.address, sent))
    one_event(
        receipt,
        token,
        ERC20.events["Transfer"],
        **{"from": trader.address, "to": maker.address, "value": sent},
    )
    block = receipt["blockNumber"]
    net.wait_height(block)
    require(
        erc20_balance(net, token, trader.address, block) == sender_before - sent,
        "pool token sender balance differs",
    )
    require(
        erc20_balance(net, token, maker.address, block) == receiver_before + sent,
        "pool token receiver balance differs",
    )
    listed = net.logs(
        token, block, block, ["0x" + ERC20.events["Transfer"].topic.hex()]
    )
    require(
        any(log["transactionHash"] == receipt["transactionHash"] for log in listed),
        "eth_getLogs misses the pool token Transfer",
    )
    print(
        f"fee right {right} of {pool} owned by {owner}; pool token {token} approve and transfer logged"
    )


# --- req 2: perps -----------------------------------------------------------------------------


def check_perps_order_types(net):
    """2.1: every order type and time in force executes in the precompile with deterministic matching."""
    perps = abi("perps")
    place = perps.fn("placeOrder")
    trader, maker = net.wallet("trader"), net.wallet("maker")
    market = 0
    ensure_perps_collateral(trader, 100_000_000)
    ensure_perps_collateral(maker, 200_000_000)
    tick = net.one(PERPS, perps.fn("getMarketConfig"), market)[6]
    ask_id, ask = book_side(net, maker, market, False, 2_000, 200 * E18)

    fok = perps_order(market, True, LIMIT, FOK, 500 * E18, limit=ask)
    net.expect_revert(PERPS, place, [fok], "PerpsFokNotFilled", trader.address)
    crossing = perps_order(market, True, LIMIT, POST_ONLY, 20 * E18, limit=ask)
    net.expect_revert(
        PERPS, place, [crossing], "PerpsPostOnlyWouldCross", trader.address
    )

    receipt, taker_id = place_order(
        trader, perps_order(market, True, MARKET, IOC, 20 * E18)
    )
    fill = one_event(receipt, PERPS, perps.events["Fill"], takerOrderId=taker_id)
    require(
        fill["makerOrderId"] == ask_id and fill["price"] == ask,
        f"market IOC filled {fill}, expected order {ask_id} at {ask}",
    )
    one_event(receipt, PERPS, perps.events["PositionOpened"], user=trader.address)
    receipt, taker_id = place_order(
        trader, perps_order(market, True, LIMIT, IOC, 20 * E18, limit=ask)
    )
    one_event(
        receipt, PERPS, perps.events["Fill"], takerOrderId=taker_id, makerOrderId=ask_id
    )
    receipt, fok_id = place_order(
        trader, perps_order(market, True, LIMIT, FOK, 20 * E18, limit=ask)
    )
    one_event(
        receipt, PERPS, perps.events["Fill"], takerOrderId=fok_id, makerOrderId=ask_id
    )
    position = position_of(net, trader.address, market)
    require(position != 0, "the trader holds no position after three fills")

    mark = perps_mark(net, market)
    for tif, distance in ((POST_ONLY, 99), (GTC, 98)):
        _, bid_id = place_order(
            trader,
            perps_order(
                market,
                True,
                LIMIT,
                tif,
                20 * E18,
                limit=tick_down(mark * distance // 100, tick),
            ),
        )
        require(
            order_status(net, bid_id) == RESTING,
            f"the time-in-force {tif} bid is not RESTING",
        )
        cancel_order(trader, bid_id)

    above, below = tick_up(mark * 105 // 100, tick), tick_down(mark * 95 // 100, tick)
    triggered = (
        perps_order(market, True, STOP_MARKET, IOC, 20 * E18, trigger=above),
        perps_order(
            market,
            True,
            STOP_LIMIT,
            GTC,
            20 * E18,
            trigger=above,
            limit=tick_up(mark * 106 // 100, tick),
        ),
        perps_order(
            market, False, TAKE_PROFIT, IOC, 10 * E18, trigger=above, reduce_only=True
        ),
        perps_order(
            market, False, STOP_LOSS, IOC, 10 * E18, trigger=below, reduce_only=True
        ),
    )
    for params in triggered:
        _, order_id = place_order(trader, params)
        details = net.one(PERPS, perps.fn("getOrderDetails"), order_id)
        require(
            details[6] == UNTRIGGERED and details[4] == params[2],
            f"order {order_id} of type {params[2]} is {details}",
        )
        cancel_order(trader, order_id)

    reducing_buy = perps_order(market, True, MARKET, IOC, 20 * E18, reduce_only=True)
    net.expect_revert(
        PERPS, place, [reducing_buy], "PerpsReduceOnlyViolation", trader.address
    )

    bid_id, _ = book_side(net, maker, market, True, 2_000, 200 * E18)
    receipt = trader.send(PERPS, perps.fn("partialClose").encode(position, 10 * E18))
    closed = one_event(
        receipt, PERPS, perps.events["PositionClosed"], positionId=position
    )
    require(closed["isFullClose"] is False, "partialClose closed the whole position")

    tp, sl = tick_up(mark * 120 // 100, tick), tick_down(mark * 80 // 100, tick)
    receipt, _ = place_order(
        trader, perps_order(market, True, MARKET, IOC, 20 * E18, tp=tp, sl=sl)
    )
    one_event(
        receipt,
        PERPS,
        perps.events["PositionTpSlSet"],
        positionId=position,
        takeProfitPrice=tp,
        stopLossPrice=sl,
    )
    details = net.one(PERPS, perps.fn("getPositionDetails"), position)
    require(
        (details[11], details[12]) == (tp, sl),
        f"position {position} carries TP/SL {details[11:13]}, expected {(tp, sl)}",
    )

    receipt = trader.send(PERPS, perps.fn("closePosition").encode(position))
    closed = one_event(
        receipt, PERPS, perps.events["PositionClosed"], positionId=position
    )
    require(
        closed["isFullClose"] is True, "closePosition left part of the position open"
    )
    require(
        net.call(PERPS, perps.fn("getPosition"), position)[9] is False,
        f"position {position} is active after closePosition",
    )
    cancel_resting(maker, (ask_id, bid_id))
    net.app_hashes_agree(receipt["blockNumber"] + 1)
    print(
        f"perps order types: every order type and time in force on market {market}, application hashes equal"
    )


def expected_tiers(tier_class):
    """(lowerBoundUsd, maxLeverage, maintenanceBps, deductionUsd) rows of one tier sheet."""
    rows, previous = [], None
    for lower, leverage in TIER_SHEETS[tier_class]:
        bps = MAINTENANCE_NUMERATOR_BPS // leverage
        lower_usd = lower * E18
        deduction = (
            0
            if previous is None
            else previous[3] + lower_usd * (bps - previous[2]) // 10_000
        )
        previous = (lower_usd, leverage, bps, deduction)
        rows.append(previous)
    return tuple(rows)


def check_perps_margin(net):
    """2.2: cross margin, tiers, deposit, withdrawal and top-up in USDL; 1000x on all eighteen markets."""
    perps = abi("perps")
    place = perps.fn("placeOrder")
    trader, maker = net.wallet("trader"), net.wallet("maker")
    module = module_address("perps")
    amount = 50_000_000
    if net.one(USDL, ERC20.fn("allowance"), trader.address, PERPS):
        approve(trader, USDL, PERPS, 0)
    net.expect_revert(
        PERPS,
        perps.fn("deposit"),
        [USDL, amount],
        "PerpsInsufficientCollateral",
        trader.address,
    )
    collateral_before = perps_collateral(net, trader.address)
    usdl_before, module_before = erc20_balance(
        net, USDL, trader.address
    ), erc20_balance(net, USDL, module)
    approve(trader, USDL, PERPS, amount)
    receipt = trader.send(PERPS, perps.fn("deposit").encode(USDL, amount))
    one_event(
        receipt,
        USDL,
        ERC20.events["Transfer"],
        **{"from": trader.address, "to": module, "value": amount},
    )
    one_event(
        receipt,
        PERPS,
        perps.events["CollateralDeposited"],
        account=trader.address,
        amount=amount,
        newBalance=collateral_before + amount,
    )
    block = receipt["blockNumber"]
    net.wait_height(block)
    require(
        erc20_balance(net, USDL, trader.address, block) == usdl_before - amount,
        "deposit USDL delta of the trader differs",
    )
    require(
        erc20_balance(net, USDL, module, block) == module_before + amount,
        "deposit USDL delta of the perps module differs",
    )
    sid = spot_tokens(net)["SID"]
    net.expect_revert(
        PERPS,
        perps.fn("deposit"),
        [sid, amount],
        "PerpsUnsupportedCollateral",
        trader.address,
    )

    for market_id, market in enumerate(PERPS_MARKETS):
        rows = tuple(net.one(PERPS, perps.fn("getMarginTiers"), market_id))
        require(
            rows == expected_tiers(market[4]),
            f"market {market_id} tiers {rows}, expected {expected_tiers(market[4])}",
        )
        tick = net.one(PERPS, perps.fn("getMarketConfig"), market_id)[6]
        bid = tick_down(perps_mark(net, market_id) * 9 // 10, tick)
        params = perps_order(
            market_id,
            True,
            LIMIT,
            POST_ONLY,
            50 * E18,
            leverage=MAX_LEVERAGE * E18,
            limit=bid,
        )
        kind, data = net.call_raw(PERPS, place.encode(params), sender=trader.address)
        require(
            kind == "ok",
            f"1000x post-only bid on market {market_id} reverted 0x{data.hex()}",
        )
        over = perps_order(
            market_id,
            True,
            LIMIT,
            POST_ONLY,
            50 * E18,
            leverage=(MAX_LEVERAGE + 1) * E18,
            limit=bid,
        )
        net.expect_revert(PERPS, place, [over], "PerpsLeverageTooHigh", trader.address)

    ensure_perps_collateral(maker, 100_000_000)
    ask_id, _ = book_side(net, maker, 0, False, 2_000, 100 * E18)
    place_order(
        trader, perps_order(0, True, MARKET, IOC, 50 * E18, leverage=MAX_LEVERAGE * E18)
    )
    position = position_of(net, trader.address, 0)
    require(
        net.one(PERPS, perps.fn("getPositionDetails"), position)[7]
        == MAX_LEVERAGE * E18,
        "the position is not at 1000x",
    )
    top_up = 1_000_000
    approve(trader, USDL, PERPS, top_up)
    receipt = trader.send(PERPS, perps.fn("topUpPosition").encode(position, top_up))
    one_event(
        receipt,
        USDL,
        ERC20.events["Transfer"],
        **{"from": trader.address, "to": module, "value": top_up},
    )
    one_event(receipt, PERPS, perps.events["PositionModified"], positionId=position)
    net.expect_revert(
        PERPS,
        perps.fn("transferMargin"),
        [position, position, 1],
        "PerpsCrossMarginOnly",
        trader.address,
    )
    net.expect_revert(
        PERPS, perps.fn("setMarginMode"), [1], "PerpsCrossMarginOnly", trader.address
    )
    bid_id, _ = book_side(net, maker, 0, True, 2_000, 100 * E18)
    trader.send(PERPS, perps.fn("closePosition").encode(position))
    cancel_resting(maker, (ask_id, bid_id))

    net.expect_revert(
        PERPS, perps.fn("withdraw"), [USDL, 0], "PerpsZeroAmount", trader.address
    )
    held = perps_collateral(net, trader.address)
    withdraw = 10_000_000
    receipt = trader.send(PERPS, perps.fn("withdraw").encode(USDL, withdraw))
    one_event(
        receipt,
        USDL,
        ERC20.events["Transfer"],
        **{"from": module, "to": trader.address, "value": withdraw},
    )
    one_event(
        receipt,
        PERPS,
        perps.events["CollateralWithdrawn"],
        account=trader.address,
        amount=withdraw,
        newBalance=held - withdraw,
    )
    print(
        "perps margin: deposit, withdrawal, top-up, cross-only refusals, tier tables and 1000x on 18 markets"
    )


def check_perps_economics(net):
    """2.3: maker and taker fees of the parameter sheet, no borrowing fee, hourly funding, fee and insurance accounts."""
    perps = abi("perps")
    fees = net.call(PERPS, perps.fn("getFees"))
    require(
        fees
        == (TAKER_FEE_PPM, MAKER_FEE_PPM, LIQUIDATION_FEE_PPM, INSURANCE_SHARE_PPM),
        f"getFees is {fees}",
    )
    names = [name.lower() for name in list(perps.functions) + list(perps.events)]
    require(
        not any("borrow" in name for name in names),
        "the perps interface carries a borrowing fee surface",
    )
    trader, maker = net.wallet("trader"), net.wallet("maker")
    for wallet in (trader, maker):
        tier = net.call(PERPS, perps.fn("getFeeTier"), wallet.address)
        if tier[0] == 0:
            require(
                tier[1:3] == (TAKER_FEE_PPM, MAKER_FEE_PPM),
                f"fee tier 0 of {wallet.address} is {tier}",
            )
    fee_recipient, insurance = net.call(PERPS, perps.fn("getProtocolAccounts"))
    require(
        not same_address(fee_recipient, insurance),
        "the fee recipient is the insurance fund",
    )
    ensure_perps_collateral(trader, 50_000_000)
    ensure_perps_collateral(maker, 100_000_000)
    ask_id, _ = book_side(net, maker, 1, False, 2_000, 100 * E18)
    taker_ppm = net.call(PERPS, perps.fn("getFeeTier"), trader.address)[1]
    maker_ppm = net.call(PERPS, perps.fn("getFeeTier"), maker.address)[2]
    recipient_before = net.one(PERPS, perps.fn("getBalance", 2), fee_recipient, USDL)
    insurance_before = net.one(PERPS, perps.fn("getInsuranceBalance"), USDL)
    receipt, taker_id = place_order(trader, perps_order(1, True, MARKET, IOC, 30 * E18))
    fill = one_event(receipt, PERPS, perps.events["Fill"], takerOrderId=taker_id)
    taker_fee, maker_fee = ceil_div(fill["sizeUsd"] * taker_ppm, 10**6), ceil_div(
        fill["sizeUsd"] * maker_ppm, 10**6
    )
    require(
        fill["takerFeeUsd"] == taker_fee,
        f"taker fee {fill['takerFeeUsd']} of {fill['sizeUsd']}, expected {taker_fee}",
    )
    require(
        fill["makerFeeUsd"] == maker_fee,
        f"maker fee {fill['makerFeeUsd']} of {fill['sizeUsd']}, expected {maker_fee}",
    )
    block = receipt["blockNumber"]
    net.wait_height(block)
    expected = (
        recipient_before
        + taker_fee // USD_PER_USDL_UNIT
        + maker_fee // USD_PER_USDL_UNIT
    )
    received = net.one(
        PERPS, perps.fn("getBalance", 2), fee_recipient, USDL, block=block
    )
    require(
        received == expected, f"the fee recipient holds {received}, expected {expected}"
    )
    require(
        net.one(PERPS, perps.fn("getInsuranceBalance"), USDL, block=block)
        == insurance_before,
        "the insurance fund moved on a fill",
    )

    collateral = perps_collateral(net, trader.address, block=block)
    later = block + 5
    net.wait_height(later)
    applied = [
        e
        for e in events(
            net.logs(PERPS, block + 1, later),
            PERPS,
            perps.events["PositionFundingApplied"],
        )
        if same_address(e["user"], trader.address)
    ]
    if not applied:
        unchanged = perps_collateral(net, trader.address, block=later) == collateral
        require(
            unchanged,
            "collateral moved without a funding payment: a borrowing fee accrues",
        )
    now = net.node.block_time(later)
    cap = FUNDING_CAP_PPM_PER_HOUR * E18 // 10**6
    for market_id in range(len(PERPS_MARKETS)):
        _, _, last_update, rate = net.call(
            PERPS, perps.fn("getFundingState"), market_id, block=later
        )
        require(
            now - last_update <= FUNDING_INTERVAL_SECONDS,
            f"market {market_id} funding last settled {now - last_update} s ago",
        )
        require(
            abs(rate) * FUNDING_INTERVAL_SECONDS <= cap,
            f"market {market_id} funding rate {rate} per second exceeds the cap",
        )
    position = position_of(net, trader.address, 1)
    bid_id, _ = book_side(net, maker, 1, True, 2_000, 100 * E18)
    trader.send(PERPS, perps.fn("closePosition").encode(position))
    cancel_resting(maker, (ask_id, bid_id))
    print(
        "perps economics: fees, fee recipient, insurance fund, funding cadence and cap, no borrowing fee"
    )


def expected_market_config(market_id, listed_at, now):
    """getMarketConfig of an opening market without its oracle symbol and listing time."""
    symbol, name, sz, px, tier_class, oi_cap = PERPS_MARKETS[market_id]
    impact = 20_000 * E18 if symbol in ("BTC", "ETH") else 6_000 * E18
    if tier_class == "E":
        require(
            now - listed_at < CLASS_E_LAUNCH_DAYS * 86_400,
            f"{symbol} listed more than 30 days ago",
        )
        oi_cap = CLASS_E_LAUNCH_OI_CAP_USD
    return (
        (market_id, symbol, name),
        (
            sz,
            px,
            10 ** (18 - px),
            10 ** (18 - sz),
            MAX_LEVERAGE,
            TIER_CLASSES.index(tier_class),
            impact,
            oi_cap * E18,
        ),
        (FUNDING_CAP_PPM_PER_HOUR, True),
    )


def check_perps_markets(net):
    """2.4: the eighteen opening markets with their parameters, trading only on a fresh oracle price."""
    perps = abi("perps")
    require(
        net.one(PERPS, perps.fn("totalMarkets")) == len(PERPS_MARKETS),
        "totalMarkets differs from 18",
    )
    ids = pricefeed_ids(net)
    now = latest_time(net)
    active = set(net.one(PERPS, perps.fn("getActiveMarketIds")))
    stale = []
    for market_id, (symbol, name, _, _, _, _) in enumerate(PERPS_MARKETS):
        config = net.one(PERPS, perps.fn("getMarketConfig"), market_id)
        expected = expected_market_config(market_id, config[14], now)
        got = (config[0:3], config[4:12], config[12:14])
        require(
            got == expected and config[3] and config[14] > 0,
            f"market {market_id} config {config}, expected {expected}",
        )
        market = net.call(PERPS, perps.fn("getMarket"), market_id)
        require(
            market[:3] == (name, symbol, MAX_LEVERAGE) and market[5] is True,
            f"getMarket({market_id}) is {market}",
        )
        fresh = pricefeed_price(net, ids[market_id])[5]
        require(
            net.one(PERPS, perps.fn("isMarketActive"), market_id) is fresh,
            f"market {market_id} activity differs from its freshness",
        )
        require(
            (market_id in active) is fresh,
            f"getActiveMarketIds disagrees with the freshness of market {market_id}",
        )
        if not fresh:
            stale.append(market_id)
    net.expect_revert(
        PERPS, perps.fn("getMarket"), [len(PERPS_MARKETS)], "PerpsUnknownMarket"
    )
    trader = net.wallet("trader")
    for market_id in stale:
        order = perps_order(market_id, True, MARKET, IOC, 20 * E18)
        net.expect_revert(
            PERPS, perps.fn("placeOrder"), [order], "PerpsPriceStale", trader.address
        )
    print(
        f"perps markets: 18 markets as listed, {len(PERPS_MARKETS) - len(stale)} fresh, stale markets refuse orders"
    )


def check_perps_log_rebuild(net):
    """2.5: positions, orders and fills rebuilt from the logs alone match the views."""
    perps = abi("perps")
    end = net.settled()
    logs = net.logs(PERPS, net.h1, end)
    by_topic = {"0x" + ev.topic.hex(): ev for ev in perps.events.values()}
    orders, positions, users = {}, {}, set()
    for log in logs:
        require(
            log["topics"][0] in by_topic,
            f"0x1015 emitted topic {log['topics'][0]} outside contracts/perps.md",
        )
        ev = by_topic[log["topics"][0]]
        e = ev.decode(log)
        if ev.name == "OrderPlaced":
            orders[e["orderId"]] = {
                "user": e["user"].lower(),
                "market": e["marketId"],
                "status": None,
            }
            users.add(e["user"].lower())
        elif ev.name in ("OrderCancelled", "OrderExpired"):
            require(
                e["orderId"] in orders,
                f"{ev.name} of order {e['orderId']} never placed in the logs",
            )
            orders[e["orderId"]]["status"] = (
                CANCELLED if ev.name == "OrderCancelled" else EXPIRED
            )
        elif ev.name == "Fill":
            for side, key in (("maker", "makerOrderId"), ("taker", "takerOrderId")):
                if e[key] in orders:
                    require(
                        orders[e[key]]["user"] == e[side].lower(),
                        f"Fill {side} {e[side]} does not hold order {e[key]}",
                    )
        elif ev.name == "PositionOpened":
            positions[e["positionId"]] = {
                "user": e["user"].lower(),
                "market": e["marketId"],
                "active": True,
            }
            users.add(e["user"].lower())
        elif ev.name in (
            "PositionModified",
            "PositionClosed",
            "Liquidation",
            "PositionFundingApplied",
            "PositionTpSlSet",
        ):
            require(
                e["positionId"] in positions,
                f"{ev.name} of position {e['positionId']} never opened in the logs",
            )
            if ev.name == "Liquidation" or (
                ev.name == "PositionClosed" and e["isFullClose"]
            ):
                positions[e["positionId"]]["active"] = False
    for position_id, rebuilt in positions.items():
        view = net.call(PERPS, perps.fn("getPosition"), position_id, block=end)
        require(
            view[0].lower() == rebuilt["user"] and view[1] == rebuilt["market"],
            f"position {position_id} holder differs",
        )
        require(
            view[9] is rebuilt["active"],
            f"position {position_id} active {view[9]}, logs say {rebuilt['active']}",
        )
    for order_id, rebuilt in orders.items():
        details = net.one(PERPS, perps.fn("getOrderDetails"), order_id, block=end)
        require(
            details[1].lower() == rebuilt["user"] and details[2] == rebuilt["market"],
            f"order {order_id} holder differs",
        )
        if rebuilt["status"] is not None:
            require(
                details[6] == rebuilt["status"],
                f"order {order_id} status {details[6]}, logs say {rebuilt['status']}",
            )
    for user in users:
        for order_id in net.one(PERPS, perps.fn("getUserOrderIds"), user, block=end):
            require(
                order_id in orders and orders[order_id]["status"] is None,
                f"open order {order_id} of {user} is closed in the logs",
            )
        held = set(net.one(PERPS, perps.fn("getUserPositionIds"), user, block=end))
        rebuilt = {p for p, r in positions.items() if r["user"] == user and r["active"]}
        require(
            held == rebuilt,
            f"positions of {user}: views {sorted(held)}, logs {sorted(rebuilt)}",
        )
    print(
        f"perps log rebuild: {len(logs)} logs from {net.h1} to {end}, {len(orders)} orders, {len(positions)} positions"
    )


# --- req 3: spot ------------------------------------------------------------------------------


def check_spot_interface(net):
    """3.1: the spot surface the application calls answers with its selectors at 0x1021 to 0x1025."""
    pax = spot_tokens(net)["PAX"]
    trader, maker = net.wallet("trader"), net.wallet("maker")
    for address in SPOT_UNUSED:
        require(net.node.code(address) == "0x", f"{address} carries code")
    vault, quoter, pecor, orders, stops = (
        abi(n) for n in ("vault", "quoter", "pecor", "orders", "stoporders")
    )
    deposit = 20_000_000
    approve(maker, USDL, SPOT_VAULT, deposit)
    receipt = maker.send(SPOT_VAULT, vault.fn("deposit").encode(USDL, deposit))
    one_event(
        receipt,
        SPOT_VAULT,
        vault.events["Deposit"],
        token=USDL,
        depositor=maker.address,
        amount=deposit,
    )
    share = net.one(SPOT_VAULT, vault.fn("shareTokens"), USDL)
    require(
        net.one(share, abi("share").fn("balanceOf"), maker.address) > 0,
        "the vault minted no USDL share to the depositor",
    )
    native = 20 * E18
    receipt = maker.send(SPOT_VAULT, vault.fn("depositNative").encode(), value=native)
    one_event(
        receipt,
        SPOT_VAULT,
        vault.events["NativeDeposit"],
        depositor=maker.address,
        amount=native,
    )
    info = net.call(SPOT_QUOTER, quoter.fn("getLiquidityInfo"), pax)
    require(info[0] > 0 and info[2] is False, f"PAX liquidity info {info}")

    spend = 2_000_000
    quote = net.one(SPOT_QUOTER, quoter.fn("quoteMarketBuy"), USDL, pax, spend)
    require(
        quote[0] == spend
        and quote[8] is True
        and quote[12] is False
        and quote[13] is False,
        f"quoteMarketBuy {quote}",
    )
    approve(trader, USDL, SPOT_PECOR, spend)
    usdl_before, native_before = erc20_balance(
        net, USDL, trader.address
    ), net.node.balance(trader.address)
    deadline = latest_time(net) + 600
    receipt = trader.send(
        SPOT_PECOR,
        pecor.fn("marketBuy").encode(
            USDL, pax, spend, quote[1] * 995 // 1000, deadline
        ),
    )
    executed = one_event(
        receipt,
        SPOT_PECOR,
        pecor.events["MarketOrderExecuted"],
        user=trader.address,
        isBuy=True,
    )
    require(
        executed["stablecoinAmount"] == spend,
        f"marketBuy spent {executed['stablecoinAmount']}, sent {spend}",
    )
    block = receipt["blockNumber"]
    net.wait_height(block)
    require(
        erc20_balance(net, USDL, trader.address, block) == usdl_before - spend,
        "marketBuy USDL delta differs",
    )
    expected_native = (
        native_before
        + executed["tokenAmount"] * PAX_POINTER_SCALE
        - trader.gas_cost(receipt)
    )
    require(
        net.node.balance(trader.address, block) == expected_native,
        "marketBuy PAX native delta differs from the executed amount",
    )

    index = pricefeed_price(net, PAX_MARKET)[0]
    order_spend, expires = 2_000_000, latest_time(net) + 3_600
    approve(trader, USDL, SPOT_ORDERS, order_spend)
    receipt = trader.send(
        SPOT_ORDERS,
        orders.fn("placeLimitBuy").encode(USDL, pax, order_spend, index // 2, expires),
    )
    order_id = one_event(
        receipt, SPOT_ORDERS, orders.events["LimitOrderCreated"], user=trader.address
    )["orderId"]
    require(
        order_id
        in net.one(SPOT_ORDERS, orders.fn("getUserLimitOrders"), trader.address),
        "the limit order is not listed",
    )
    record = net.one(SPOT_ORDERS, orders.fn("getLimitOrder"), order_id)
    require(
        record[4] == order_spend
        and record[5] == index // 2
        and record[7] == SPOT_PENDING,
        f"limit order {record}",
    )
    receipt = trader.send(
        SPOT_ORDERS,
        orders.fn("modifyLimitOrder").encode(order_id, index // 3, expires + 60),
    )
    one_event(
        receipt,
        SPOT_ORDERS,
        orders.events["LimitOrderModified"],
        orderId=order_id,
        targetPrice=index // 3,
        expiresAt=expires + 60,
    )
    usdl_before = erc20_balance(net, USDL, trader.address)
    receipt = trader.send(SPOT_ORDERS, orders.fn("cancelLimitOrder").encode(order_id))
    one_event(
        receipt, SPOT_ORDERS, orders.events["LimitOrderCancelled"], orderId=order_id
    )
    block = receipt["blockNumber"]
    net.wait_height(block)
    require(
        erc20_balance(net, USDL, trader.address, block) == usdl_before + order_spend,
        "cancel did not refund the limit order",
    )
    require(
        net.one(SPOT_ORDERS, orders.fn("getLimitOrder"), order_id)[7] == SPOT_CANCELLED,
        "the limit order is not CANCELLED",
    )

    amount = executed["tokenAmount"] // 4
    place = stops.fn("placeStopLimitSell").encode(
        pax, USDL, amount, index // 2, index * 49 // 100, expires
    )
    receipt = trader.send(SPOT_STOP_ORDERS, place)
    stop_id = one_event(
        receipt,
        SPOT_STOP_ORDERS,
        stops.events["StopLimitOrderCreated"],
        user=trader.address,
    )["orderId"]
    require(
        net.one(SPOT_STOP_ORDERS, stops.fn("getStopLimitOrder"), stop_id)[8]
        == SPOT_PENDING,
        "the stop-limit order is not PENDING",
    )
    receipt = trader.send(
        SPOT_STOP_ORDERS, stops.fn("cancelStopLimitOrder").encode(stop_id)
    )
    one_event(
        receipt, SPOT_STOP_ORDERS, stops.events["StopLimitCancelled"], orderId=stop_id
    )
    receipt = trader.send(
        SPOT_STOP_ORDERS,
        stops.fn("placeStopLoss").encode(pax, USDL, amount, index // 2, expires),
    )
    loss_id = one_event(
        receipt,
        SPOT_STOP_ORDERS,
        stops.events["StopLossOrderCreated"],
        user=trader.address,
    )["orderId"]
    require(
        net.one(SPOT_STOP_ORDERS, stops.fn("getStopLossOrder"), loss_id)[6]
        == SPOT_PENDING,
        "the stop-loss order is not PENDING",
    )
    receipt = trader.send(
        SPOT_STOP_ORDERS, stops.fn("cancelStopLossOrder").encode(loss_id)
    )
    one_event(
        receipt,
        SPOT_STOP_ORDERS,
        stops.events["StopLossOrderCancelled"],
        orderId=loss_id,
    )
    print(
        "spot interface: vault deposits, quotes, market buy, limit and stop orders at 0x1021 to 0x1025"
    )


def swap_fee_bps(net, volume, block):
    """Fee of the swap rail: the base fee plus the tier fees the volume reaches."""
    pecor = abi("pecor")
    bps = net.one(SPOT_PECOR, pecor.fn("swapFeeBps"), block=block)
    if volume >= net.one(SPOT_PECOR, pecor.fn("TIER1_THRESHOLD"), block=block):
        bps += net.one(SPOT_PECOR, pecor.fn("tier1FeeBps"), block=block)
    if volume >= net.one(SPOT_PECOR, pecor.fn("TIER2_THRESHOLD"), block=block):
        bps += net.one(SPOT_PECOR, pecor.fn("tier2FeeBps"), block=block)
    return bps


def check_spot_settlement(net):
    """3.2: fills settle atomically with the fee sheet; USDL moves as the ERC20, PAX as native transfers."""
    pax = spot_tokens(net)["PAX"]
    pecor, orders = abi("pecor"), abi("orders")
    trader = net.wallet("trader")
    module = module_address("spot")
    amount = 1_000_000
    usdl_before = erc20_balance(net, USDL, trader.address)
    native_before = net.node.balance(trader.address)
    module_usdl_before = erc20_balance(net, USDL, module)
    fees_before = net.one(SPOT_PECOR, pecor.fn("accumulatedFees"), USDL)
    deadline = latest_time(net) + 600
    receipt = trader.send(
        SPOT_PECOR, pecor.fn("marketSell").encode(pax, USDL, amount, 1, deadline)
    )
    block = receipt["blockNumber"]
    net.wait_height(block)
    price = pricefeed_price(net, PAX_MARKET, block=block)[0]
    volume = amount * price // 10**6
    gross = amount * price // E18
    bps = swap_fee_bps(net, volume, block)
    fee = gross * bps // 10_000
    tiered = one_event(
        receipt,
        SPOT_PECOR,
        pecor.events["TieredFeeApplied"],
        user=trader.address,
        tokenOut=USDL,
    )
    got = (tiered["volumeUSD"], tiered["feeBps"], tiered["feeAmount"])
    require(
        got == (volume, bps, fee),
        f"TieredFeeApplied {tiered}, expected {(volume, bps, fee)}",
    )
    impact = events(receipt, SPOT_PECOR, pecor.events["PriceImpactApplied"])
    require(len(impact) <= 1, "marketSell emitted two PriceImpactApplied logs")
    deduction = impact[0]["deductedAmount"] if impact else 0
    if impact:
        require(
            deduction == (gross - fee) * impact[0]["impactBps"] // 10_000,
            f"impact deduction {impact[0]}",
        )
    net_out = gross - fee - deduction
    executed = one_event(
        receipt,
        SPOT_PECOR,
        pecor.events["MarketOrderExecuted"],
        user=trader.address,
        isBuy=False,
    )
    got = (executed["tokenAmount"], executed["stablecoinAmount"])
    require(got == (amount, net_out), f"MarketOrderExecuted {executed}, net {net_out}")
    one_event(
        receipt,
        USDL,
        ERC20.events["Transfer"],
        **{"from": module, "to": trader.address, "value": net_out},
    )
    require(
        erc20_balance(net, USDL, trader.address, block) == usdl_before + net_out,
        "marketSell USDL delta differs",
    )
    require(
        erc20_balance(net, USDL, module, block) == module_usdl_before - net_out,
        "spot module USDL delta differs",
    )
    expected_native = (
        native_before - amount * PAX_POINTER_SCALE - trader.gas_cost(receipt)
    )
    require(
        net.node.balance(trader.address, block) == expected_native,
        "marketSell PAX native delta differs",
    )
    accrued = net.one(SPOT_PECOR, pecor.fn("accumulatedFees"), USDL, block=block)
    require(
        accrued == fees_before + fee + deduction,
        f"swap fee accrual {accrued}, expected {fees_before + fee + deduction}",
    )

    order_fee_bps = net.one(SPOT_ORDERS, orders.fn("orderFeeBps"))
    target = price * 9 // 10
    order_fees_before = net.one(SPOT_ORDERS, orders.fn("accumulatedFees"), USDL)
    usdl_before = erc20_balance(net, USDL, trader.address)
    receipt = trader.send(
        SPOT_ORDERS,
        orders.fn("placeLimitSell").encode(
            pax, USDL, amount, target, latest_time(net) + 3_600
        ),
    )
    order_id = one_event(
        receipt, SPOT_ORDERS, orders.events["LimitOrderCreated"], user=trader.address
    )["orderId"]
    deadline = time.monotonic() + 120
    while (
        net.one(SPOT_ORDERS, orders.fn("getLimitOrder"), order_id)[7] != SPOT_EXECUTED
    ):
        require(
            time.monotonic() < deadline,
            f"limit sell {order_id} at {target} did not execute in EndBlock",
        )
        time.sleep(1)
    executed_event = orders.events["LimitOrderExecuted"]
    found = [
        log
        for log in net.logs(
            SPOT_ORDERS,
            receipt["blockNumber"],
            net.settled(),
            ["0x" + executed_event.topic.hex()],
        )
        if executed_event.decode(log)["orderId"] == order_id
    ]
    require(
        len(found) == 1,
        f"limit sell {order_id} carries {len(found)} LimitOrderExecuted logs",
    )
    executed_at = int(found[0]["blockNumber"], 16)
    exec_price = executed_event.decode(found[0])["executionPrice"]
    require(
        exec_price >= target,
        f"limit sell executed at {exec_price}, below its target {target}",
    )
    gross = amount * exec_price // E18
    received = erc20_balance(net, USDL, trader.address, executed_at) - usdl_before
    accrued = (
        net.one(SPOT_ORDERS, orders.fn("accumulatedFees"), USDL, block=executed_at)
        - order_fees_before
    )
    require(
        received + accrued == gross,
        f"limit fill paid {received} and accrued {accrued}, gross {gross}",
    )
    require(
        accrued >= gross * order_fee_bps // 10_000,
        f"limit fill accrued {accrued}, below the order fee of {order_fee_bps} bps",
    )
    print(
        f"spot settlement: market sell {amount} at {price} net {net_out}, limit sell {order_id} at {exec_price}"
    )


def check_spot_logs(net):
    """3.3: every spot state change emits a log with the signature of contracts/spot.md."""
    golden = {
        address: {e["id"] for e in spot_golden(name) if e["kind"] == "event"}
        for name, address in SPOT_ADDRESSES.items()
    }
    end = net.settled()
    count = 0
    for address, topics in golden.items():
        for log in net.logs(address, net.h1, end):
            require(
                log["topics"][0] in topics,
                f"{address} emitted topic {log['topics'][0]} outside contracts/spot.md",
            )
            count += 1
    trader = net.wallet("trader")
    pax = spot_tokens(net)["PAX"]
    orders = abi("orders")
    spend = 1_000_000
    approve(trader, USDL, SPOT_ORDERS, spend)
    placed = trader.send(
        SPOT_ORDERS,
        orders.fn("placeLimitBuy").encode(
            USDL, pax, spend, 1, latest_time(net) + 3_600
        ),
    )
    order_id = one_event(
        placed, SPOT_ORDERS, orders.events["LimitOrderCreated"], user=trader.address
    )["orderId"]
    cancelled = trader.send(SPOT_ORDERS, orders.fn("cancelLimitOrder").encode(order_id))
    one_event(
        cancelled, SPOT_ORDERS, orders.events["LimitOrderCancelled"], orderId=order_id
    )
    for receipt in (placed, cancelled):
        for log in receipt["logs"]:
            if log["address"].lower() in golden:
                require(
                    log["topics"][0] in golden[log["address"].lower()],
                    f"{log['topics'][0]} is outside contracts/spot.md",
                )
    print(
        f"spot logs: {count} logs from {net.h1} to {end} carry contracts/spot.md signatures"
    )


# --- req 4: prices ----------------------------------------------------------------------------


def check_sidecar_submissions(net):
    """4.1: the four sidecars submit signed prices for every listed market each block, free of fee."""
    pricefeed = abi("pricefeed")
    markets = net.one(PRICEFEED, pricefeed.fn("getMarketCount"))
    start = net.node.height() + 1
    end = start + SIDECAR_BLOCKS - 1
    net.wait_comet(end + 1)
    seen = {}
    for height in range(start, end + 1):
        in_block = set()
        for raw, result in block_txs(net.node, height):
            tx = decode_tx(raw)
            if not tx["messages"] or tx["messages"][0][0] != SUBMIT_PRICES_URL:
                continue
            require(
                len(tx["messages"]) == 1,
                f"submission at {height} carries {len(tx['messages'])} messages",
            )
            require(
                int(result.get("code", 0)) == 0,
                f"submission at {height} failed: {result.get('log')}",
            )
            require(tx["fee"] == [], f"submission at {height} paid {tx['fee']}")
            feeder, observations = decode_submission(tx["messages"][0][1])
            account = feed_account(tx["keys"][0])
            require(
                feeder == bech32_encode(BECH32_PREFIX, account),
                f"submission at {height} is signed by another key than {feeder}",
            )
            validator = net.one(
                PRICEFEED,
                pricefeed.fn("getValidatorByFeedAccount"),
                "0x" + account.hex(),
                block=height,
            )
            require(
                not same_address(validator, ZERO_ADDRESS),
                f"feeder {feeder} is not registered",
            )
            require(
                validator not in in_block,
                f"validator {validator} submitted twice at {height}",
            )
            in_block.add(validator)
            ids = sorted(o["market_id"] for o in observations)
            require(
                ids == list(range(markets)),
                f"{validator} at {height} submitted markets {ids}, expected 0..{markets - 1}",
            )
            seen[validator] = seen.get(validator, 0) + 1
    require(
        len(seen) == 4,
        f"{len(seen)} validators submitted between {start} and {end}: {seen}",
    )
    print(
        f"sidecar submissions: blocks {start}..{end}, submissions per validator {sorted(seen.values())}"
    )


def sidecar_pids(net):
    pids = net.args.sidecar_pid or []
    require(len(pids) == 4, "the price checks take four --sidecar-pid values")
    for pid in pids:
        os.kill(pid, 0)
    return pids


def wait_freshness(net, ids, fresh, timeout):
    deadline = time.monotonic() + timeout
    while True:
        states = {m: pricefeed_price(net, pf)[5] for m, pf in ids.items()}
        if all(state is fresh for state in states.values()):
            return
        require(
            time.monotonic() < deadline,
            f"prices did not become {'fresh' if fresh else 'stale'}: {states}",
        )
        time.sleep(1)


def check_price_freshness(net):
    """4.2: index and mark per market, fresh only with enough sources; stale prices refuse trading."""
    pricefeed = abi("pricefeed")
    params = net.one(PRICEFEED, pricefeed.fn("getParams"))
    min_sources = params[2]
    ids = pricefeed_ids(net)
    pids = sidecar_pids(net)
    window = (params[0] + params[9]) / 1000 + 30
    wait_freshness(net, ids, True, window)
    for m, pf in ids.items():
        price = pricefeed_price(net, pf)
        require(
            price[0] > 0 and price[1] > 0 and price[4] >= min_sources,
            f"market {m} price {price}",
        )
    trader = net.wallet("trader")
    pax = spot_tokens(net)["PAX"]
    stopped = []
    try:
        for pid in pids:
            os.kill(pid, signal.SIGSTOP)
            stopped.append(pid)
            expect_fresh = len(pids) - len(stopped) >= min_sources
            wait_freshness(net, ids, expect_fresh, window)
            if not expect_fresh:
                order = perps_order(0, True, MARKET, IOC, 20 * E18)
                net.expect_revert(
                    PERPS,
                    abi("perps").fn("placeOrder"),
                    [order],
                    "PerpsPriceStale",
                    trader.address,
                )
                buy = [USDL, pax, 1_000_000, 1, latest_time(net) + 600]
                net.expect_revert(
                    SPOT_PECOR,
                    abi("pecor").fn("marketBuy"),
                    buy,
                    "StalePrice",
                    trader.address,
                )
                break
        require(
            len(pids) - len(stopped) < min_sources,
            "prices never went stale with sidecars stopped",
        )
    finally:
        for pid in stopped:
            os.kill(pid, signal.SIGCONT)
    wait_freshness(net, ids, True, window)
    print(
        f"price freshness: fresh with {min_sources} or more sidecars, stale below, trading refused while stale"
    )


def check_price_submission_bounds(net):
    """4.3: registered feed-key submissions pay no fee and are refused over the size, count and rate bounds."""
    pricefeed = abi("pricefeed")
    require(net.args.feed_key, "--feed-key is required by this check")
    secret = evm.read_key(net.args.feed_key)
    account = feed_account(evm.compressed_public_key(secret))
    feeder = bech32_encode(BECH32_PREFIX, account)
    validator = net.one(
        PRICEFEED, pricefeed.fn("getValidatorByFeedAccount"), "0x" + account.hex()
    )
    require(
        not same_address(validator, ZERO_ADDRESS),
        f"feed key {feeder} is not registered",
    )
    params = net.one(PRICEFEED, pricefeed.fn("getParams"))
    max_observations, max_bytes = params[5], params[6]
    markets = net.one(PRICEFEED, pricefeed.fn("getMarketCount"))
    chain_id = net.node.comet("status")["node_info"]["network"]

    def observations(count):
        out, now_ms = [], int(time.time() * 1000)
        for i in range(count):
            market_id = i % markets
            price = pricefeed_price(net, market_id)
            last = net.one(
                PRICEFEED, pricefeed.fn("getObservation"), market_id, validator
            )
            out.append(
                (market_id, last[0] + 1 + i // markets, price[0], price[1], now_ms)
            )
        return out

    pid = sidecar_pids(net)[0]
    os.kill(pid, signal.SIGSTOP)
    try:
        number, sequence = query_account(net.node, feeder)
        _, raw = sign_submission(
            secret,
            chain_id,
            number,
            sequence,
            SUBMISSION_GAS,
            observations(markets),
            memo="x" * (max_bytes + 1),
        )
        require(
            len(raw) > max_bytes, "the padded submission is not over the size bound"
        )
        code, space, log = broadcast_sync(net.node, raw)
        require(
            (code, space) == (CODE_SUBMISSION_TOO_LARGE, PRICEFEED_CODESPACE),
            f"oversize submission returned {space} {code}: {log}",
        )

        count = min(max_observations, MAX_OBSERVATIONS_PER_MESSAGE) + 1
        _, raw = sign_submission(
            secret, chain_id, number, sequence, SUBMISSION_GAS, observations(count)
        )
        require(
            len(raw) <= max_bytes,
            f"{count} observations take {len(raw)} bytes, over the size bound",
        )
        code, space, log = broadcast_sync(net.node, raw)
        require(
            (code, space) == (CODE_TOO_MANY_OBSERVATIONS, PRICEFEED_CODESPACE),
            f"{count} observations returned {space} {code}: {log}",
        )

        for _ in range(3):
            height = net.node.height()
            number, sequence = query_account(net.node, feeder)
            valid = observations(markets)
            _, first = sign_submission(
                secret, chain_id, number, sequence, SUBMISSION_GAS, valid
            )
            _, second = sign_submission(
                secret, chain_id, number, sequence + 1, SUBMISSION_GAS, valid
            )
            first_result = broadcast_sync(net.node, first)
            second_result = broadcast_sync(net.node, second)
            if net.node.height() != height:
                continue
            require(
                first_result[0] == 0,
                f"the first submission was refused: {first_result}",
            )
            require(
                second_result[:2] == (CODE_RATE_LIMITED, PRICEFEED_CODESPACE),
                f"the second submission returned {second_result}",
            )
            require(decode_tx(first)["fee"] == [], "the submission carries a fee")
            tx_hash = "0x" + hashlib.sha256(first).hexdigest().upper()
            deadline = time.monotonic() + 60
            while True:
                try:
                    found = net.node.comet("tx", hash=tx_hash)
                    break
                except RpcError:
                    require(
                        time.monotonic() < deadline,
                        "the first submission was not included",
                    )
                    time.sleep(1)
            require(
                int(found["tx_result"].get("code", 0)) == 0,
                f"the first submission failed: {found['tx_result'].get('log')}",
            )
            break
        else:
            raise CheckFailed("every pair of submissions straddled a block")
    finally:
        os.kill(pid, signal.SIGCONT)
    print(
        f"price submission bounds: {max_bytes} bytes, {max_observations} observations and one per block enforced"
    )


# --- req 7 ------------------------------------------------------------------------------------


def check_plan_and_version(net):
    """7.2: the plan v6.12.0 is applied at HR+1 and every node reports version v6.12.0."""
    for node in net.nodes:
        height = applied_plan_height(node, UPGRADE)
        require(
            height == net.h1,
            f"node {node.index} applied {UPGRADE} at {height}, expected {net.h1}",
        )
        version = node.comet("abci_info")["response"].get("version")
        require(version == UPGRADE, f"node {node.index} reports version {version}")
    print(f"plan {UPGRADE} applied at {net.h1} on four nodes reporting {UPGRADE}")


CHECKS = {
    fn.__name__: fn
    for fn in (
        check_launchpad_interface,
        check_usdl_erc20_custody,
        check_pool_parity,
        check_fee_right_and_pool_token,
        check_perps_order_types,
        check_perps_margin,
        check_perps_economics,
        check_perps_markets,
        check_perps_log_rebuild,
        check_spot_interface,
        check_spot_settlement,
        check_spot_logs,
        check_sidecar_submissions,
        check_price_freshness,
        check_price_submission_bounds,
        check_plan_and_version,
    )
}


def parse_args(argv):
    parser = argparse.ArgumentParser(prog="programme.py")
    parser.add_argument("check", choices=sorted(CHECKS))
    parser.add_argument("--evm", action="append", default=[])
    parser.add_argument("--comet", action="append", default=[])
    parser.add_argument("--halt-height", type=int, required=True)
    parser.add_argument("--trader-key")
    parser.add_argument("--maker-key")
    parser.add_argument("--feed-key")
    parser.add_argument("--sidecar-pid", action="append", type=int)
    args = parser.parse_args(argv)
    if args.halt_height < 1:
        parser.error("--halt-height must be a positive height")
    return args


def main(argv):
    args = parse_args(argv)
    try:
        CHECKS[args.check](Net(args))
    except (CheckFailed, Revert, RpcError, KeyError, ValueError, OSError) as err:
        print(f"{args.check}: {err}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
