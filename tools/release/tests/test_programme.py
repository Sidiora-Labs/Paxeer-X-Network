#!/usr/bin/env python3
"""Calldata and result decoding of every programme check against the golden selector files."""

import hashlib
import importlib.util
import json
import re
import sys
import traceback
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location(
    "programme", ROOT / "tools/release/rehearsal/programme.py"
)
programme = importlib.util.module_from_spec(spec)
spec.loader.exec_module(programme)
P = programme


def hexid(raw):
    return "0x" + raw.hex()


def abi_entries(abi):
    """{(kind, signature): id} of one Abi."""
    out = {
        ("function", fn.signature): hexid(fn.selector) for fn in abi.every_function()
    }
    out.update({("event", ev.signature): hexid(ev.topic) for ev in abi.events.values()})
    out.update({("error", sig): hexid(sel) for sel, sig in abi.errors.items()})
    return out


def test_keccak256_vectors():
    assert (
        P.keccak256(b"").hex()
        == "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
    )
    assert (
        P.keccak256(b"abc").hex()
        == "4e03657aea45a94fc7d47ba826c8d667c0d1e6e33a64a036ec44f58fa12d6c45"
    )
    assert P.keccak256(b"a" * 136).hex() == P.evm.keccak(b"a" * 136).hex()
    assert P.keccak256(b"a" * 300).hex() == P.evm.keccak(b"a" * 300).hex()
    assert P.selector("transfer(address,uint256)").hex() == "a9059cbb"
    assert (
        P.event_topic("Transfer(address,address,uint256)").hex().startswith("ddf252ad")
    )
    assert (
        P.event_topic("Approval(address,address,uint256)").hex().startswith("8c5be1e5")
    )


def test_launchpad_abis_match_golden():
    golden = {}
    for role, signature, ident in P.launchpad_golden():
        golden.setdefault(role, {})[signature] = ident
    for role in P.LAUNCHPAD_ABI_ROLES:
        entries = abi_entries(P.abi(role))
        assert {sig: ident for (_, sig), ident in entries.items()} == golden[role], role
    assert golden["Chain"]["Retired(address)"] == hexid(P.RETIRED_SELECTOR)
    assert set(golden) == set(P.LAUNCHPAD_ABI_ROLES) | {"Chain", "EventEmitter"}


def test_perps_abi_matches_golden():
    kinds = {"method": "function", "event": "event", "error": "error"}
    golden = {(kinds[kind], sig): ident for kind, sig, ident in P.perps_golden()}
    assert abi_entries(P.abi("perps")) == golden
    assert len(golden) == 141


def test_spot_abis_match_golden():
    for name in ("pecor", "orders", "stoporders", "quoter", "vault", "share"):
        golden = {(e["kind"], e["signature"]): e["id"] for e in P.spot_golden(name)}
        assert abi_entries(P.abi(name)) == golden, name


def test_pricefeed_abi_matches_golden_table():
    source = (ROOT / "precompiles/pricefeed/abi_test.go").read_text()
    constants = dict(re.findall(r'(\w+)\s*=\s*"(\([^"]*\))"', source))
    rows = re.findall(
        r'\{"(\w+)", "([^"]+)", "([0-9a-f]{8})", "(\w+)", ([^}]+)\}', source
    )
    assert len(rows) == 12
    pricefeed = P.abi("pricefeed")
    assert len(pricefeed.every_function()) == len(rows)
    for name, signature, ident, mutability, outputs in rows:
        text = "".join(
            constants.get(part.strip(), part.strip().strip('"'))
            for part in outputs.split("+")
        )
        fn = pricefeed.fn(name)
        assert (fn.signature, fn.selector.hex(), fn.mutability) == (
            signature,
            ident,
            mutability,
        ), name
        assert "(" + ",".join(fn.outputs) + ")" == text, (name, fn.outputs, text)


def test_every_refusal_the_checks_expect_is_golden():
    perps_errors = {
        sig: ident for kind, sig, ident in P.perps_golden() if kind == "error"
    }
    source = (ROOT / "tools/release/rehearsal/programme.py").read_text()
    used = set(re.findall(r'"(Perps\w+)"', source))
    assert len(used) == 10, used
    for name in used:
        assert perps_errors[name + "()"] == hexid(P.error_selector(name)), name
    assert perps_errors["PerpsFokNotFilled()"] == "0xc3dbe522"
    assert perps_errors["PerpsPriceStale()"] == "0x737dedba"
    for name in ("pecor", "orders", "stoporders", "quoter"):
        stale = [
            e
            for e in P.spot_golden(name)
            if e["kind"] == "error" and e["name"] == "StalePrice"
        ]
        assert (
            stale
            and stale[0]["id"] == hexid(P.error_selector("StalePrice")) == "0x19abf40e"
        ), name


def test_every_function_the_checks_call_is_golden():
    source = (ROOT / "tools/release/rehearsal/programme.py").read_text()
    calls = re.findall(r'(\w+)\.fn\("(\w+)"', source)
    names = {
        "perps": "perps",
        "pricefeed": "pricefeed",
        "vault": "vault",
        "quoter": "quoter",
        "pecor": "pecor",
        "orders": "orders",
        "stops": "stoporders",
        "nft": "SidioraNFT",
        "pool_abi": "SidioraPool",
        "fn": "PoolRegistry",
    }
    seen = set()
    for owner, name in calls:
        if owner in names:
            assert P.abi(names[owner]).functions[name], (owner, name)
            seen.add((owner, name))
    assert len(seen) >= 60, len(seen)
    for name in ("balanceOf", "totalSupply", "allowance", "approve", "transfer"):
        sid = {e["signature"]: e["id"] for e in P.spot_golden("share")}
        fn = P.ERC20.fn(name)
        assert sid[fn.signature] == hexid(fn.selector), name
    share = {e["signature"]: e["id"] for e in P.spot_golden("share")}
    for event in P.ERC20.events.values():
        assert share[event.signature] == hexid(event.topic)


def test_erc20_calldata():
    data = P.ERC20.fn("transfer").encode("0x" + "11" * 20, 1_000_000)
    assert data.hex() == "a9059cbb" + "00" * 12 + "11" * 20 + format(1_000_000, "064x")
    assert P.ERC20.fn("balanceOf").decode(P.word(7)) == (7,)


def test_place_order_calldata_and_order_details():
    perps = P.abi("perps")
    params = P.perps_order(
        3,
        True,
        P.STOP_LIMIT,
        P.GTC,
        20 * P.E18,
        trigger=5,
        limit=6,
        reduce_only=True,
        cloid=b"\x01" * 16,
    )
    data = perps.fn("placeOrder").encode(params)
    assert (
        data[:4].hex()
        == [i for k, s, i in P.perps_golden() if s.startswith("placeOrder(")][0][2:]
    )
    assert len(data) == 4 + 15 * 32
    assert P.decode_values(perps.fn("placeOrder").inputs, data[4:]) == (params,)
    assert params[7] == 5 and params[6] == 6 and params[10] is True
    details = (
        9,
        "0x" + "22" * 20,
        3,
        True,
        P.STOP_LIMIT,
        P.GTC,
        P.UNTRIGGERED,
        True,
        6,
        5,
        0,
        20 * P.E18,
        1,
        0,
        10 * P.E18,
        0,
        b"\x01" * 16,
        4,
    )
    encoded = P.encode_values(perps.fn("getOrderDetails").outputs, [details])
    assert perps.fn("getOrderDetails").decode(encoded) == (details,)


def test_market_config_and_tiers_decoding():
    perps = P.abi("perps")
    now = 1_000_000
    for market_id, market in enumerate(P.PERPS_MARKETS):
        head, middle, tail = P.expected_market_config(market_id, now - 86_400, now)
        config = head + (market[0],) + middle + tail + (now - 86_400,)
        encoded = P.encode_values(perps.fn("getMarketConfig").outputs, [config])
        decoded = perps.fn("getMarketConfig").decode(encoded)[0]
        assert (decoded[0:3], decoded[4:12], decoded[12:14]) == (head, middle, tail)
        tiers = P.expected_tiers(market[4])
        encoded = P.encode_values(perps.fn("getMarginTiers").outputs, [list(tiers)])
        assert tuple(perps.fn("getMarginTiers").decode(encoded)[0]) == tiers
    btc = P.expected_market_config(0, 0, 0)
    assert btc[1] == (
        5,
        1,
        10**17,
        10**13,
        1000,
        0,
        20_000 * P.E18,
        500_000_000 * P.E18,
    )
    pax = P.expected_market_config(11, now - 86_400, now)
    assert pax[1][5] == 4 and pax[1][7] == 2_000_000 * P.E18
    try:
        P.expected_market_config(11, 0, 31 * 86_400)
        raise AssertionError("a class E market past its launch window passed")
    except P.CheckFailed:
        pass
    a = P.expected_tiers("A")
    assert a[0] == (0, 1000, 5, 0)
    assert a[1] == (1_000_000 * P.E18, 500, 10, 500 * P.E18)
    assert a[2] == (
        5_000_000 * P.E18,
        200,
        25,
        500 * P.E18 + 5_000_000 * 15 * P.E18 // 10_000,
    )
    assert [row[1] for row in P.expected_tiers("E")] == [1000, 100, 20, 10, 5]


def test_pricefeed_result_decoding():
    pricefeed = P.abi("pricefeed")
    price = (65_000 * P.E18, 65_010 * P.E18, 1_700_000_000_000, 12, 4, True)
    assert pricefeed.fn("getPrice").decode(
        P.encode_values(pricefeed.fn("getPrice").outputs, [price])
    ) == (price,)
    params = (5000, 1000, 3, 500, 1000, 64, 16384, 2000, 100, 10000)
    assert pricefeed.fn("getParams").decode(
        P.encode_values(pricefeed.fn("getParams").outputs, [params])
    ) == (params,)
    many = [price, (1, 2, 3, -4, 5, False)]
    assert pricefeed.fn("getPrices").decode(
        P.encode_values(pricefeed.fn("getPrices").outputs, [many])
    ) == (many,)
    market = (11, "PAX", "PAX-USD", 1, 10**30, True)
    assert pricefeed.fn("getMarket").decode(
        P.encode_values(pricefeed.fn("getMarket").outputs, [market])
    ) == (market,)
    assert (
        pricefeed.fn("marketIdOf").encode("BTC").hex()
        == "19fa583c" + P.word(32).hex() + P.word(3).hex() + b"BTC".hex() + "00" * 29
    )
    key = (b"\x02" + b"\x07" * 32, "0x" + "33" * 20)
    assert (
        pricefeed.fn("getFeedKey").decode(
            P.encode_values(pricefeed.fn("getFeedKey").outputs, key)
        )
        == key
    )


def test_spot_quote_and_order_decoding():
    quote = tuple(range(1, 24))
    quote = (
        quote[:8]
        + (True,)
        + quote[9:12]
        + (False, False)
        + quote[14:16]
        + (True,)
        + quote[17:]
    )
    fn = P.abi("quoter").fn("quoteMarketBuy")
    assert len(fn.outputs) == 1 and fn.outputs[0].count(",") == 22
    assert fn.decode(P.encode_values(fn.outputs, [quote])) == (quote,)
    order = (
        1,
        "0x" + "44" * 20,
        P.USDL,
        "0x" + "55" * 20,
        2_000_000,
        7 * P.E18,
        0,
        P.SPOT_PENDING,
        10,
        20,
    )
    fn = P.abi("orders").fn("getLimitOrder")
    assert fn.decode(P.encode_values(fn.outputs, [order])) == (order,)
    tokens = [P.USDL, "0x" + "66" * 20, "0x" + "77" * 20]
    fn = P.abi("vault").fn("getRegisteredTokens")
    assert fn.decode(P.encode_values(fn.outputs, [tokens])) == (tokens,)
    fn = P.abi("pecor").fn("marketSell")
    data = fn.encode("0x" + "66" * 20, P.USDL, 1_000_000, 1, 99)
    assert (
        data[:4].hex()
        == [e["id"] for e in P.spot_golden("pecor") if e["name"] == "marketSell"][0][2:]
    )


def test_launchpad_calldata_and_retired_revert():
    pools = ["0x" + "88" * 20, "0x" + "99" * 20]
    fn = P.abi("PoolRegistry").fn("getAllPools")
    assert fn.encode(0, 100).hex() == "a7753f09" + P.word(0).hex() + P.word(100).hex()
    assert fn.decode(P.encode_values(fn.outputs, [pools])) == (pools,)
    fn = P.abi("SidioraPool").fn("getReserves")
    assert fn.encode().hex() == "0902f1ac" and fn.decode(
        P.encode_values(fn.outputs, (1, 2, 3))
    ) == (1, 2, 3)
    fn = P.abi("Router").fn("buy")
    assert fn.encode(pools[0], 1, 2, 3)[:4].hex() == "1622dbe4"
    assert (
        P.abi("SidioraNFT").fn("supportsInterface").encode(P.ERC721_INTERFACE).hex()
        == "01ffc9a7" + "80ac58cd" + "00" * 28
    )
    retired = (
        P.RETIRED_SELECTOR
        + b"\x00" * 12
        + P.address_bytes(P.LAUNCHPAD_ROLES["PoolRegistry"])
    )
    assert P.decode_values(["address"], retired[4:]) == (
        P.LAUNCHPAD_ROLES["PoolRegistry"],
    )
    assert (
        P.revert_data(
            {"code": 3, "message": "execution reverted", "data": "0x" + retired.hex()}
        )
        == retired
    )
    assert P.revert_data({"code": -32000, "message": "header not found"}) is None
    assert P.revert_data({"code": 3, "message": "execution reverted"}) == b""


def test_zero_argument_calldata_of_every_function():
    count = 0
    for name in list(P.ABI_PATHS) + list(P.LAUNCHPAD_ABI_ROLES):
        for fn in P.abi(name).every_function():
            data = fn.encode(*fn.zero_args("0x" + "ab" * 20))
            assert data[:4] == fn.selector
            assert P.decode_values(fn.inputs, data[4:]) == tuple(
                "0x" + "ab" * 20 if a == "0x" + "ab" * 20 else a
                for a in fn.zero_args("0x" + "ab" * 20)
            ), fn.signature
            count += 1
    abi_roles = set(P.LAUNCHPAD_ABI_ROLES)
    launchpad_functions = sum(
        1
        for role, sig, ident in P.launchpad_golden()
        if role in abi_roles and ident != "0x" and len(ident) == 10
    ) - sum(len(P.abi(role).errors) for role in abi_roles)
    golden = (
        launchpad_functions
        + sum(1 for kind, _, _ in P.perps_golden() if kind == "method")
        + sum(
            1
            for n in ("pecor", "orders", "stoporders", "quoter", "vault", "share")
            for e in P.spot_golden(n)
            if e["kind"] == "function"
        )
        + 12
        + 4
    )
    assert count == golden, (count, golden)


def test_event_decoding():
    perps = P.abi("perps")
    fill = perps.events["Fill"]
    maker, taker = "0x" + "12" * 20, "0x" + "34" * 20
    data = P.encode_values(
        ["address", "address", "bool", "uint256", "uint256", "int256", "uint256"],
        [maker, taker, True, 65_000 * P.E18, 20 * P.E18, -3, 9 * 10**15],
    )
    log = {
        "address": P.PERPS,
        "topics": ["0x" + fill.topic.hex()]
        + ["0x" + P.word(v).hex() for v in (0, 5, 6)],
        "data": "0x" + data.hex(),
    }
    got = P.one_event({"logs": [log]}, P.PERPS, fill, takerOrderId=6)
    assert got == {
        "marketId": 0,
        "makerOrderId": 5,
        "takerOrderId": 6,
        "maker": maker,
        "taker": taker,
        "takerIsLong": True,
        "price": 65_000 * P.E18,
        "sizeUsd": 20 * P.E18,
        "makerFeeUsd": -3,
        "takerFeeUsd": 9 * 10**15,
    }
    assert ("event", fill.signature, "0x" + fill.topic.hex()) in set(P.perps_golden())
    transfer = P.ERC20.events["Transfer"]
    log = {
        "address": P.USDL.upper().replace("0X", "0x"),
        "topics": [
            "0x" + transfer.topic.hex(),
            "0x" + "00" * 12 + "12" * 20,
            "0x" + "00" * 12 + "34" * 20,
        ],
        "data": "0x" + P.word(5).hex(),
    }
    assert P.events({"logs": [log]}, P.USDL, transfer) == [
        {"from": maker, "to": taker, "value": 5}
    ]
    try:
        P.one_event({"logs": [log, log]}, P.USDL, transfer)
        raise AssertionError("two matching logs passed one_event")
    except P.CheckFailed:
        pass


def test_cosmos_submission_vector():
    vector = json.loads(
        (
            ROOT / "services/platform/hosted/feeder/tests/fixtures/cosmos_tx_vector.json"
        ).read_text()
    )
    secret = int(vector["secret_key_hex"], 16)
    public_key = P.evm.compressed_public_key(secret)
    assert public_key.hex() == vector["public_key_hex"]
    assert P.feed_account(public_key).hex() == vector["feed_account_hex"]
    assert (
        P.bech32_encode(P.BECH32_PREFIX, P.feed_account(public_key)) == vector["feeder"]
    )
    observations = [
        (
            o["market_id"],
            o["sequence"],
            o["index_price"],
            o["mark_price"],
            o["source_timestamp_ms"],
        )
        for o in vector["observations"]
    ]
    body, auth, doc = P.submission_parts(
        secret,
        vector["chain_id"],
        vector["account_number"],
        vector["sequence"],
        vector["gas_limit"],
        observations,
    )
    assert doc.hex() == vector["sign_doc_hex"]
    signature = bytes.fromhex(vector["signature_hex"])
    assert P.tx_raw(body, auth, signature).hex() == vector["tx_hex"]
    decoded = P.decode_tx(bytes.fromhex(vector["tx_hex"]))
    assert (
        decoded["keys"] == [public_key]
        and decoded["fee"] == []
        and decoded["memo"] == ""
    )
    assert [url for url, _ in decoded["messages"]] == [P.SUBMIT_PRICES_URL]
    feeder, parsed = P.decode_submission(decoded["messages"][0][1])
    assert feeder == vector["feeder"] and parsed == vector["observations"]

    fresh_doc, raw = P.sign_submission(
        secret,
        vector["chain_id"],
        vector["account_number"],
        vector["sequence"],
        vector["gas_limit"],
        observations,
        memo="xy",
    )
    fields = P.pb_fields(raw)
    sig = P.pb_get(fields, 3)
    r, s = int.from_bytes(sig[:32], "big"), int.from_bytes(sig[32:], "big")
    digest = hashlib.sha256(fresh_doc).digest()
    assert any(
        P.evm.recover(digest, r, s, parity) == P.evm.public_point(secret)
        for parity in (0, 1)
    )
    assert P.decode_tx(raw)["memo"] == "xy"


def test_protobuf_queries():
    assert P.pb_bytes(1, P.UPGRADE) == b"\x0a\x07v6.12.0"
    assert P.pb_fields(P.pb_uint(1, 300) + P.pb_bytes(2, b"ab")) == [
        (1, 0, 300),
        (2, 2, b"ab"),
    ]
    account = P.pb_message(
        1,
        P.pb_bytes(1, P.BASE_ACCOUNT_URL)
        + P.pb_message(2, P.pb_bytes(1, "pax1x") + P.pb_uint(3, 9) + P.pb_uint(4, 3)),
    )
    any_fields = P.pb_fields(P.pb_get(P.pb_fields(account), 1))
    inner = P.pb_fields(P.pb_get(any_fields, 2))
    assert (P.pb_get(inner, 3), P.pb_get(inner, 4)) == (9, 3)


def test_module_addresses():
    for name in ("launchpad", "perps", "spot"):
        address = P.module_address(name)
        assert len(P.address_bytes(address)) == 20
        assert address[2:] == hashlib.sha256(name.encode()).hexdigest()[:40]
    assert len({P.module_address(n) for n in ("launchpad", "perps", "spot")}) == 3


def test_checks_and_driver_functions():
    expected = [
        "check_launchpad_interface",
        "check_usdl_erc20_custody",
        "check_pool_parity",
        "check_fee_right_and_pool_token",
        "check_perps_order_types",
        "check_perps_margin",
        "check_perps_economics",
        "check_perps_markets",
        "check_perps_log_rebuild",
        "check_spot_interface",
        "check_spot_settlement",
        "check_spot_logs",
        "check_sidecar_submissions",
        "check_price_freshness",
        "check_price_submission_bounds",
        "check_plan_and_version",
    ]
    assert list(P.CHECKS) == expected
    driver = (ROOT / "tools/release/rehearse-v6120.sh").read_text()
    for name in expected:
        assert f"{name}() {{ run_programme_check {name}; }}" in driver, name
    args = P.parse_args(
        ["check_pool_parity", "--halt-height", "100"]
        + [
            x
            for i in range(4)
            for x in ("--evm", f"http://e{i}", "--comet", f"http://c{i}")
        ]
        + [x for pid in (11, 12, 13, 14) for x in ("--sidecar-pid", str(pid))]
    )
    net = P.Net(args)
    assert (net.halt, net.h1, len(net.nodes), args.sidecar_pid) == (
        100,
        101,
        4,
        [11, 12, 13, 14],
    )
    assert [n.comet_url for n in net.nodes] == [f"http://c{i}" for i in range(4)]
    try:
        P.Net(
            P.parse_args(
                ["check_pool_parity", "--halt-height", "100", "--evm", "http://e0"]
            )
        )
        raise AssertionError("one node passed the four-node requirement")
    except P.CheckFailed:
        pass


def main():
    tests = [
        (name, fn)
        for name, fn in sorted(globals().items())
        if name.startswith("test_") and callable(fn)
    ]
    failed = 0
    for name, fn in tests:
        try:
            fn()
            print(f"PASS {name}")
        except Exception:
            failed += 1
            print(f"FAIL {name}")
            traceback.print_exc()
    print(f"{len(tests) - failed} passed, {failed} failed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
