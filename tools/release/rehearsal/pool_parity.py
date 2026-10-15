#!/usr/bin/env python3
"""Launchpad pool parity of the v6.12.0 rehearsal.

record reads, at the halt height, getPoolCount() and every getAllPools page of the two
deployment-generation registries and getReserves() of every pool, and writes them as JSON.
check reads, at the first upgraded height, getPoolCount() of the launchpad precompile and
getReserves() of every recorded pool, and exits 1 naming the first difference.
"""

import argparse
import json
import sys
import urllib.error
import urllib.request

LAUNCHPAD = "0x0000000000000000000000000000000000001017"
GENERATION_REGISTRIES = (
    "0x7684382c89f79104574D8EF9b31eFf2eD2C2BA0b",
    "0x1dC357d2B7B850332a91F06A9E66634F46fC4Ba7",
)
GET_POOL_COUNT = "0x8eec5d70"
GET_ALL_POOLS = "0xa7753f09"
GET_RESERVES = "0x0902f1ac"
RESERVE_FIELDS = ("virtualUsdl", "realUsdl", "tokenReserve")
PAGE = 100
WORD = 32


class ParityError(Exception):
    """A read or a comparison of the pool parity check failed."""


def _address(text):
    value = text.lower()
    if len(value) != 42 or not value.startswith("0x"):
        raise ParityError(f"{text} is not a 20-byte hex address")
    int(value[2:], 16)
    return value


def eth_call(rpc, to, data, block):
    body = json.dumps(
        {"jsonrpc": "2.0", "id": 1, "method": "eth_call", "params": [{"to": to, "data": data}, hex(block)]}
    ).encode()
    request = urllib.request.Request(rpc, data=body, headers={"content-type": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            reply = json.load(response)
    except (urllib.error.URLError, OSError, ValueError) as err:
        raise ParityError(f"eth_call {to} at {block}: {err}") from err
    if reply.get("error") is not None:
        raise ParityError(f"eth_call {to} {data[:10]} at {block} reverted: {json.dumps(reply['error'])}")
    result = reply.get("result")
    if not isinstance(result, str) or not result.startswith("0x"):
        raise ParityError(f"eth_call {to} at {block} returned {result!r}")
    return bytes.fromhex(result[2:])


def decode_words(raw, count, what):
    if len(raw) != count * WORD:
        raise ParityError(f"{what} returned {len(raw)} bytes, expected {count * WORD}")
    return [int.from_bytes(raw[i * WORD : (i + 1) * WORD], "big") for i in range(count)]


def decode_address_array(raw, what):
    if len(raw) < 2 * WORD:
        raise ParityError(f"{what} returned {len(raw)} bytes, too short for address[]")
    offset = int.from_bytes(raw[:WORD], "big")
    if offset != WORD:
        raise ParityError(f"{what} returned array offset {offset}, expected {WORD}")
    length = int.from_bytes(raw[WORD : 2 * WORD], "big")
    if len(raw) != (2 + length) * WORD:
        raise ParityError(f"{what} returned {len(raw)} bytes for {length} addresses")
    pools = []
    for i in range(length):
        word = raw[(2 + i) * WORD : (3 + i) * WORD]
        if any(word[:12]):
            raise ParityError(f"{what} entry {i} is not an address")
        pools.append("0x" + word[12:].hex())
    return pools


def read_pool_count(rpc, address, block):
    return decode_words(eth_call(rpc, address, GET_POOL_COUNT, block), 1, f"getPoolCount() of {address}")[0]


def read_pools(rpc, registry, count, block):
    pools = []
    for offset in range(0, count, PAGE):
        data = GET_ALL_POOLS + offset.to_bytes(WORD, "big").hex() + PAGE.to_bytes(WORD, "big").hex()
        page = decode_address_array(eth_call(rpc, registry, data, block), f"getAllPools({offset},{PAGE}) of {registry}")
        if len(page) != min(PAGE, count - offset):
            raise ParityError(f"getAllPools({offset},{PAGE}) of {registry} returned {len(page)} pools")
        pools.extend(page)
    return pools


def read_reserves(rpc, pool, block):
    return tuple(
        decode_words(eth_call(rpc, pool, GET_RESERVES, block), len(RESERVE_FIELDS), f"getReserves() of {pool}")
    )


def record(rpc, block, registries):
    entries = []
    reserves = {}
    for registry in registries:
        count = read_pool_count(rpc, registry, block)
        pools = read_pools(rpc, registry, count, block)
        entries.append({"address": registry, "count": count, "pools": pools})
        for pool in pools:
            if pool in reserves:
                raise ParityError(f"pool {pool} is listed twice")
            reserves[pool] = [str(value) for value in read_reserves(rpc, pool, block)]
    return {"block": block, "registries": entries, "reserves": reserves}


def check_pool_count(counts, served):
    """Requires the launchpad count to equal the sum of the registry counts read at the halt height."""
    expected = sum(counts)
    if served != expected:
        raise ParityError(f"pools {served} of {expected}: getPoolCount() of {LAUNCHPAD} differs from the registries")
    return f"pools {served} of {expected}"


def check_reserves(before, after):
    """Requires every pool's (virtualUsdl, realUsdl, tokenReserve) to be equal word for word."""
    for pool, words in before.items():
        if pool not in after:
            raise ParityError(f"pool {pool} has no reserves after the upgrade")
        for field, old, new in zip(RESERVE_FIELDS, words, after[pool]):
            if int(old) != int(new):
                raise ParityError(f"pool {pool} {field}: {old} at the halt height, {new} after the upgrade")
    return f"reserves {len(before)} of {len(before)}"


def load_record(path):
    with open(path, encoding="utf-8") as handle:
        data = json.load(handle)
    counts = [entry["count"] for entry in data["registries"]]
    before = {pool: tuple(int(value) for value in words) for pool, words in data["reserves"].items()}
    if sum(counts) != len(before):
        raise ParityError(f"{path} holds {len(before)} pools for registry counts {counts}")
    return counts, before


def main(argv):
    parser = argparse.ArgumentParser(prog="pool_parity.py")
    sub = parser.add_subparsers(dest="command", required=True)
    rec = sub.add_parser("record")
    rec.add_argument("--rpc", required=True)
    rec.add_argument("--block", type=int, required=True)
    rec.add_argument("--registry", action="append", type=_address)
    rec.add_argument("--out", required=True)
    chk = sub.add_parser("check")
    chk.add_argument("--rpc", required=True)
    chk.add_argument("--block", type=int, required=True)
    chk.add_argument("--record", required=True)
    args = parser.parse_args(argv)
    try:
        if args.block < 1:
            raise ParityError("--block must be a positive height")
        if args.command == "record":
            registries = args.registry or [_address(r) for r in GENERATION_REGISTRIES]
            data = record(args.rpc, args.block, registries)
            with open(args.out, "w", encoding="utf-8") as handle:
                json.dump(data, handle, indent=1, sort_keys=True)
            print(f"pools at {args.block}: {len(data['reserves'])}")
            return 0
        counts, before = load_record(args.record)
        print(check_pool_count(counts, read_pool_count(args.rpc, LAUNCHPAD, args.block)))
        after = {pool: read_reserves(args.rpc, pool, args.block) for pool in before}
        print(check_reserves(before, after))
        return 0
    except (ParityError, OSError, KeyError, ValueError) as err:
        print(f"pool_parity: {err}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
