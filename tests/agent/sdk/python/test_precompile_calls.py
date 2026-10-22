from __future__ import annotations

import json
import unittest
from pathlib import Path

from layerx_sdk.bridge import BRIDGE_EVENTS, LAYERX_BRIDGE_PRECOMPILE, bridge_in_call, bridge_out_call, decode_bridge_event
from layerx_sdk.exchange import (
    EXCHANGE_EVENTS,
    LAYERX_EXCHANGE_PRECOMPILE,
    PrecompileAbiError,
    abi_selector,
    decode_exchange_event,
    exchange_cancel_order_call,
    exchange_deposit_margin_call,
    exchange_deposit_margin_token_call,
    exchange_place_order_call,
    exchange_request_settlement_call,
    exchange_withdraw_margin_call,
    precompile_transaction_request,
    send_exchange_deposit_margin,
)
from layerx_sdk.launchpad import (
    LAUNCHPAD_EVENTS,
    LAUNCHPAD_PRECOMPILE,
    LAUNCHPAD_WRITES,
    decode_launchpad_event,
    launchpad_buy_call,
    launchpad_call,
    launchpad_claim_fees_call,
    launchpad_create_market_call,
    launchpad_nft_call,
    launchpad_sell_call,
    launchpad_set_fee_strategy_call,
    send_launchpad_nft_write,
)
from layerx_sdk.perps import (
    PERPS_PRECOMPILE,
    PERPS_WRITES,
    perps_call,
    perps_cancel_order_call,
    perps_close_position_call,
    perps_deposit_call,
    perps_open_position_call,
    perps_withdraw_call,
)

ROOT = Path(__file__).resolve().parents[4]
TOPICS = {
    "MarginDeposited": "456ba29aa60d5cac1a6dc1c0f3df30b1f18963fd90dafe8c5f4a6de80440118a",
    "MarginWithdrawalRequested": "9cf8600ce0e07d0d2b0b82ed99fc940eb6121143201d8d682c25fc74972c88f0",
    "OrderCancelRequested": "39148489da3c16ee8c589a95e2f0c869816ee4f816b4ba1b85b32bc6d94c0241",
    "OrderPlaced": "88b93538701d726739ade066c2a9f09e5088f608d9b2ebba0cf305f5ee0752ce",
    "SettlementRequested": "70d5b9c37994017669de2a991c65a88ebeb34999f37c6dc6d0c44462d57eb655",
    "BridgeIn": "4352fb2e09bdaa35c4d407ce85dfa93eaec318876eddd6f5a490cce830c3f274",
    "BridgeOut": "3e990eb54009dcdca53d8fa87307210f07097f37dcf6185dee71a42f8e7d524e",
    "ConfigUpdated": "2903b3642d2105b7fbd127326de888ab50e8d975bf819398337fb58407c1b10e",
    "FeeDistributed": "f7ab826cedf9d005102eddf95e65f43597aef36a55ff09ee4f452204c83f4afc",
    "FeeFlow": "626e3fd5d9323ca4b7ffa3ca51c21c02766b638d15eb8896a36a4e0e97ae6de4",
    "FeeStrategyChanged": "337fd9f9304e9da4c04886a5b8bf56e74032b459758212a860bc61d3544353dd",
    "MarketCreated": "b0a9c7605389f1127c2299d4a3ed85f0b2fc19894ab5411d76b453bb29f9f47b",
    "NftMint": "d2917d37031c0dfe5016665c7a83360cbf2ff32063096b2caf61ba66c06832e5",
    "NftTransfer": "021f03e8a1315704c74c03e8c022c12bc80c4a2bd4fec8bd36b217322c340ad3",
    "PoolRegistered": "da5568642b67660133b1de538c867c58f3037a5722b0e231a959ee8ca11b2ed7",
    "PoolStateUpdated": "8155a4b38a877d212b271811786986f8f7d78074c82fce4526c58defcc6d7ebd",
    "RouterTrade": "17c1899a3fd431f2b2200b90e738d270bb856a4222040c5650bfa208acb3b650",
    "Swap": "a234ab85728295548823d8a26077ecc77aa5815f9ed8f4c5e6fd390ac851d4cc",
    "TokenDeployed": "ad187874b793634c55d977f35d4828526eb48170a9531c71d88f8fd10cf9c237",
}
SOURCES = (("layerxexchange", EXCHANGE_EVENTS), ("layerxbridge", BRIDGE_EVENTS), ("launchpad", LAUNCHPAD_EVENTS))


def canonical(item: dict) -> str:
    if item["type"].startswith("tuple"):
        return "(" + ",".join(canonical(part) for part in item["components"]) + ")" + item["type"][5:]
    return item["type"]


def signature_of(entry: dict) -> str:
    return f"{entry['name']}({','.join(canonical(item) for item in entry['inputs'])})"


def abi(directory: str) -> list[dict]:
    return json.loads((ROOT / "precompiles" / ("perps/abi" if directory == "perps" else directory) / "abi.json").read_text())


def word(value: int) -> str:
    return f"{value:064x}"


def ident(byte: str) -> str:
    return "0x" + byte * 32


def address(byte: str) -> str:
    return "0x" + byte * 20


def address_word(byte: str) -> str:
    return "0" * 24 + byte * 20


def text(value: str) -> str:
    body = value.encode().hex()
    return word(len(body) // 2) + body.ljust(-(-len(body) // 64) * 64, "0")


FROM = address("12")
SWAP = address_word("aa") + word(100) + word(90) + word(1700000000)
WRITES = [
    ("layerxexchange", "placeOrder(bytes32,uint8,uint256,uint256,uint8)",
     exchange_place_order_call(ident("11"), 2, 18446744073709551618, 5000, 0), LAYERX_EXCHANGE_PRECOMPILE,
     "11" * 32 + word(2) + word(18446744073709551618) + word(5000) + word(0)),
    ("layerxexchange", "cancelOrder(bytes32)", exchange_cancel_order_call(ident("31")), LAYERX_EXCHANGE_PRECOMPILE, "31" * 32),
    ("layerxexchange", "requestSettlement(bytes32)", exchange_request_settlement_call(ident("41")), LAYERX_EXCHANGE_PRECOMPILE, "41" * 32),
    ("layerxexchange", "depositMargin(bytes32)", exchange_deposit_margin_call(ident("51"), 7), LAYERX_EXCHANGE_PRECOMPILE, "51" * 32),
    ("layerxexchange", "depositMarginToken(address,uint256,bytes32)",
     exchange_deposit_margin_token_call(address("ab"), 9, ident("51")), LAYERX_EXCHANGE_PRECOMPILE,
     address_word("ab") + word(9) + "51" * 32),
    ("layerxexchange", "withdrawMargin(bytes32,bytes32,uint256)",
     exchange_withdraw_margin_call(ident("51"), ident("61"), 10), LAYERX_EXCHANGE_PRECOMPILE, "51" * 32 + "61" * 32 + word(10)),
    ("perps", "deposit(address,uint256)", perps_deposit_call(address("ab"), 250000000), PERPS_PRECOMPILE,
     address_word("ab") + word(250000000)),
    ("perps", "withdraw(address,uint256)", perps_withdraw_call(address("ab"), 30000000), PERPS_PRECOMPILE,
     address_word("ab") + word(30000000)),
    ("perps", "openPosition(uint256,address,uint256,uint256,bool)",
     perps_open_position_call(3, address("ab"), 50000000, 5, False), PERPS_PRECOMPILE,
     word(3) + address_word("ab") + word(50000000) + word(5) + word(0)),
    ("perps", "cancelOrder(uint256)", perps_cancel_order_call(9), PERPS_PRECOMPILE, word(9)),
    ("perps", "closePosition(uint256)", perps_close_position_call(7), PERPS_PRECOMPILE, word(7)),
    ("layerxbridge", "bridgeIn(uint64,address,bytes32,uint64,bytes32,address,uint256,bytes[])",
     bridge_in_call(1, address("aa"), ident("bb"), 3, ident("cc"), address("dd"), 5, ["0x0102", "0x" + "ee" * 33]),
     LAYERX_BRIDGE_PRECOMPILE,
     word(1) + address_word("aa") + "bb" * 32 + word(3) + "cc" * 32 + address_word("dd") + word(5) + word(256)
     + word(2) + word(64) + word(128) + word(2) + "0102".ljust(64, "0") + word(33) + ("ee" * 33).ljust(128, "0")),
    ("layerxbridge", "bridgeOut(uint64,address,uint256,address)", bridge_out_call(8, address("dd"), 5, address("ee")),
     LAYERX_BRIDGE_PRECOMPILE, word(8) + address_word("dd") + word(5) + address_word("ee")),
    ("launchpad", "buy(address,uint256,uint256,uint256)",
     launchpad_buy_call(address("aa"), 100, 90, 1700000000), LAUNCHPAD_PRECOMPILE, SWAP),
    ("launchpad", "sell(address,uint256,uint256,uint256)",
     launchpad_sell_call(address("aa"), 100, 90, 1700000000), LAUNCHPAD_PRECOMPILE, SWAP),
    ("launchpad", "createMarket(string,string,uint8,address)", launchpad_create_market_call("Paxeer Dog", "PDOG", 2),
     LAUNCHPAD_PRECOMPILE, word(128) + word(192) + word(2) + word(0) + text("Paxeer Dog") + text("PDOG")),
    ("launchpad", "createMarket(string,string,uint8,address)", launchpad_create_market_call("Paxeer Dog", "PDOG", 2, address("cc")),
     LAUNCHPAD_PRECOMPILE, word(128) + word(192) + word(2) + address_word("cc") + text("Paxeer Dog") + text("PDOG")),
    ("launchpad", "setFeeStrategy(uint256,uint8)", launchpad_set_fee_strategy_call(7, 1), LAUNCHPAD_PRECOMPILE, word(7) + word(1)),
    ("launchpad", "claimFees(uint256)", launchpad_claim_fees_call(7), LAUNCHPAD_PRECOMPILE, word(7)),
    ("launchpad", "multicall(bytes[])", launchpad_call("multicall(bytes[])", [["0x0102"]]), LAUNCHPAD_PRECOMPILE,
     word(32) + word(1) + word(32) + word(2) + "0102".ljust(64, "0")),
    ("launchpad", "buyWithPermit(address,uint256,uint256,uint256,(uint256,uint256,uint8,bytes32,bytes32))",
     launchpad_call("buyWithPermit(address,uint256,uint256,uint256,(uint256,uint256,uint8,bytes32,bytes32))",
                    [address("aa"), 100, 90, 1700000000, [5, 1700000000, 27, ident("31"), ident("41")]]),
     LAUNCHPAD_PRECOMPILE, SWAP + word(5) + word(1700000000) + word(27) + "31" * 32 + "41" * 32),
    ("launchpad", "claimLegacyAirdrop(address,uint256,address,uint256,bytes32[])",
     launchpad_call("claimLegacyAirdrop(address,uint256,address,uint256,bytes32[])",
                    [address("aa"), 1, address("bb"), 2, [ident("31"), ident("41")]]),
     LAUNCHPAD_PRECOMPILE, address_word("aa") + word(1) + address_word("bb") + word(2) + word(160) + word(2) + "31" * 32 + "41" * 32),
] + [
    ("launchpad", f"{write}(uint256)", launchpad_nft_call(write, 7), LAUNCHPAD_PRECOMPILE, word(7))
    for write in ("claimAirdrop", "executeAirdrop", "executeBurn", "executeLpRewards")
]


class PrecompileCallsTest(unittest.TestCase):
    def test_every_abi_event_is_declared_with_its_topic(self) -> None:
        declared = 0
        for directory, specs in SOURCES:
            events = [entry for entry in abi(directory) if entry["type"] == "event"]
            self.assertEqual(len(specs), len(events), directory)
            for entry in events:
                spec = next(spec for spec in specs if spec.name == entry["name"])
                self.assertEqual(
                    [(item.name, item.type, item.indexed) for item in spec.inputs],
                    [(item["name"], item["type"], bool(item.get("indexed"))) for item in entry["inputs"]],
                )
                self.assertEqual(spec.topic0, "0x" + TOPICS[spec.name])
                declared += 1
        self.assertEqual(declared, 19)

    def test_every_write_encodes_its_abi_calldata(self) -> None:
        self.assertEqual(abi_selector("transfer(address,uint256)"), "0xa9059cbb")
        for directory, signature, call, to, body in WRITES:
            self.assertEqual(call.to, to, signature)
            self.assertEqual(call.data, abi_selector(signature) + body, signature)
            self.assertEqual(call.value, 7 if signature.startswith("depositMargin(") else 0, signature)
            self.assertTrue(
                any(item["type"] == "function" and signature_of(item) == signature for item in abi(directory)), signature
            )
        launchpad_writes = {
            signature_of(entry): entry.get("stateMutability") == "payable"
            for entry in abi("launchpad")
            if entry["type"] == "function" and entry.get("stateMutability") not in ("view", "pure")
        }
        self.assertEqual(LAUNCHPAD_WRITES, launchpad_writes)
        perps_writes = {
            signature_of(entry): entry.get("stateMutability") == "payable"
            for entry in abi("perps")
            if entry["type"] == "function" and entry.get("stateMutability") not in ("view", "pure")
        }
        self.assertEqual(PERPS_WRITES, perps_writes)
        for retired in ("placeOrder(bytes32,uint8,uint256,uint256,uint8)", "requestSettlement(bytes32)",
                        "depositMargin(bytes32)", "placeOrder(bytes32,bytes32,uint8,uint256,uint256,uint8)"):
            self.assertNotIn(retired, perps_writes)
            with self.assertRaises(PrecompileAbiError):
                perps_call(retired, [])
        for directory, _ in SOURCES:
            for entry in abi(directory):
                if entry["type"] == "function" and entry.get("stateMutability") not in ("view", "pure"):
                    self.assertTrue(
                        any(source == directory and sig.startswith(entry["name"] + "(") for source, sig, *_ in WRITES)
                        or (directory == "launchpad" and signature_of(entry) in LAUNCHPAD_WRITES),
                        entry["name"],
                    )
        for signature, values, value in (
            ("claimFees(uint256)", [1], 1),
            ("claimFees(uint256)", [], 0),
            ("setFeeStrategy(uint256,uint8)", [1, 256], 0),
            ("pause(address)", [address("aa")], 0),
            ("buy(address,uint256,uint256,uint256)", ["0x12", 1, 1, 1], 0),
        ):
            with self.assertRaises(PrecompileAbiError):
                launchpad_call(signature, values, value)
        with self.assertRaises(PrecompileAbiError):
            exchange_deposit_margin_call(ident("51"), 0)
        with self.assertRaises(PrecompileAbiError):
            exchange_place_order_call(ident("11"), 256, 1, 1, 0)

    def test_send_helpers_ask_the_wallet_for_eth_send_transaction(self) -> None:
        sent: list[tuple[str, list[object]]] = []

        def request(method: str, params: list[object]) -> object:
            sent.append((method, params))
            return ident("fe")

        self.assertEqual(send_exchange_deposit_margin(request, FROM, ident("51"), 255), ident("fe"))
        self.assertEqual(send_launchpad_nft_write(request, FROM, "executeBurn", 9), ident("fe"))
        self.assertEqual(sent, [
            ("eth_sendTransaction", [{"from": FROM, "to": LAYERX_EXCHANGE_PRECOMPILE,
                                      "data": abi_selector("depositMargin(bytes32)") + "51" * 32, "value": "0xff"}]),
            ("eth_sendTransaction", [{"from": FROM, "to": LAUNCHPAD_PRECOMPILE,
                                      "data": abi_selector("executeBurn(uint256)") + word(9), "value": "0x0"}]),
        ])
        self.assertEqual(precompile_transaction_request(FROM, exchange_cancel_order_call(ident("31")))["value"], "0x0")
        with self.assertRaises(PrecompileAbiError) as caught:
            send_exchange_deposit_margin(lambda method, params: None, FROM, ident("51"), 1)
        self.assertEqual(caught.exception.code, "malformed_wallet_answer")

    def test_logs_decode_to_their_typed_fields(self) -> None:
        topics = ["0x" + TOPICS["OrderPlaced"], ident("a1"), ident("11"), "0x" + address_word("12")]
        data = "0x" + word(2) + word(18446744073709551618) + word(5000) + word(0) + word(4)
        order = decode_exchange_event(LAYERX_EXCHANGE_PRECOMPILE, topics, data)
        self.assertEqual(order.event, "OrderPlaced")
        self.assertEqual(order.fields, {
            "intentId": ident("a1"), "marketId": ident("11"), "owner": FROM, "side": 2,
            "price": 18446744073709551618, "quantity": 5000, "timeInForce": 0, "nonce": 4,
        })
        for code, args in (
            ("topic_count", (LAYERX_EXCHANGE_PRECOMPILE, topics[:3], data)),
            ("data_length", (LAYERX_EXCHANGE_PRECOMPILE, topics, data[:-2])),
            ("non_canonical_word", (LAYERX_EXCHANGE_PRECOMPILE, topics, "0x" + word(256) + data[66:])),
            ("unknown_event", (LAUNCHPAD_PRECOMPILE, topics, data)),
        ):
            with self.assertRaises(PrecompileAbiError) as caught:
                decode_exchange_event(*args)
            self.assertEqual(caught.exception.code, code)
        bridge_in = decode_bridge_event(
            LAYERX_BRIDGE_PRECOMPILE,
            ["0x" + TOPICS["BridgeIn"], "0x" + word(1), ident("bb"), "0x" + address_word("12")],
            "0x" + word(3) + address_word("dd") + word(5) + word(128) + text("factory/paxeer/usdc"),
        )
        self.assertEqual(bridge_in.fields, {
            "chain": 1, "txHash": ident("bb"), "recipient": FROM, "logIndex": 3,
            "asset": address("dd"), "amount": 5, "denom": "factory/paxeer/usdc",
        })
        bridge_out = decode_bridge_event(
            LAYERX_BRIDGE_PRECOMPILE,
            ["0x" + TOPICS["BridgeOut"], "0x" + word(8), "0x" + address_word("dd"), "0x" + word(9)],
            "0x" + word(5) + address_word("ee"),
        )
        self.assertEqual(bridge_out.fields, {"chain": 8, "asset": address("dd"), "amount": 5, "recipient": address("ee"), "nonce": 9})
        created = decode_launchpad_event(
            LAUNCHPAD_PRECOMPILE,
            ["0x" + TOPICS["MarketCreated"], ident("a1"), "0x" + address_word("aa"), "0x" + address_word("12")],
            "0x" + address_word("bb") + address_word("cc") + word(1700000000) + word(42),
        )
        self.assertEqual(created.fields, {
            "poolId": ident("a1"), "token": address("aa"), "creator": FROM, "pool": address("bb"), "optical": address("cc"),
            "timestamp": 1700000000, "blockNumber": 42,
        })
        swap_topics = ["0x" + TOPICS["Swap"], ident("a1"), "0x" + address_word("12")]
        swap_data = "0x" + word(1) + word(100) + word(95) + word(1) + word(7) + word(1700000000) + word(42)
        swap = decode_launchpad_event(LAUNCHPAD_PRECOMPILE, swap_topics, swap_data)
        self.assertEqual(swap.fields, {
            "poolId": ident("a1"), "sender": FROM, "isBuy": True, "amountIn": 100, "amountOut": 95, "fee": 1, "price": 7,
            "timestamp": 1700000000, "blockNumber": 42,
        })
        with self.assertRaises(PrecompileAbiError):
            decode_launchpad_event(LAUNCHPAD_PRECOMPILE, swap_topics, "0x" + word(2) + swap_data[66:])


if __name__ == "__main__":
    unittest.main()
