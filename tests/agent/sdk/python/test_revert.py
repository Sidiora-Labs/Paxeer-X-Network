from __future__ import annotations

import sys
import unittest
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[4]
sys.path.insert(0, str(_REPO_ROOT / "agent" / "sdk" / "python"))

from layerx_sdk import PlatformSdkError  # noqa: E402
from layerx_sdk.production import SdkErrorCode  # noqa: E402
from layerx_sdk.revert import (  # noqa: E402
    LXP_RETRYABLE,
    REVERT_SELECTOR,
    RevertReasonError,
    decode_revert_reason,
    raise_for_receipt_status,
)

_ADDRESS_TABLE = _REPO_ROOT / "spec" / "layerx-merge" / "contracts" / "address-table.md"


def _registry() -> list[tuple[str, str, bool]]:
    text = _ADDRESS_TABLE.read_text(encoding="utf-8")
    section = text[text.index("## (b) Error code registry"):text.index("## (c) Activation")]
    rows = []
    for line in section.splitlines():
        if line.startswith("| LXP_"):
            cells = [cell.strip() for cell in line.strip().strip("|").split("|")]
            if cells[-1] not in {"yes", "no"}:
                raise AssertionError(f"retryable column of {cells[0]} is {cells[-1]!r}")
            rows.append((cells[0], cells[1], cells[-1] == "yes"))
    return rows


def _encode(text: str) -> bytes:
    data = text.encode("utf-8")
    padded = data + bytes(-len(data) % 32)
    return REVERT_SELECTOR + (32).to_bytes(32, "big") + len(data).to_bytes(32, "big") + padded


class RevertRegistryTest(unittest.TestCase):
    def test_retryable_table_is_the_registry_column(self) -> None:
        registry = _registry()
        self.assertEqual(len(registry), 274)
        self.assertEqual(len({code for code, _, _ in registry}), len(registry))
        self.assertEqual(dict(LXP_RETRYABLE), {code: retryable for code, _, retryable in registry})
        self.assertEqual(sum(LXP_RETRYABLE.values()), 37)

    def test_every_registry_code_decodes(self) -> None:
        for code, message, retryable in _registry():
            with self.subTest(code=code):
                decoded = decode_revert_reason(_encode(f"{code}: {message}"))
                self.assertIsInstance(decoded, RevertReasonError)
                self.assertEqual((decoded.code, decoded.message, decoded.detail, decoded.retryable), (code, message, None, retryable))
                self.assertEqual(str(decoded), f"{code}: {message}")
                detailed = decode_revert_reason(_encode(f"{code}: {message}; asset 7"))
                self.assertEqual((detailed.code, detailed.message, detailed.detail, detailed.retryable), (code, message, "asset 7", retryable))
                self.assertEqual(str(detailed), f"{code}: {message}; asset 7")

    def test_retriable_and_terminal_classes(self) -> None:
        self.assertTrue(decode_revert_reason(_encode("LXP_IDENTITY_FROZEN: identity frozen")).retryable)
        self.assertTrue(decode_revert_reason(_encode("LXP_AUTH_NOT_MATURE: auth not mature")).retryable)
        self.assertFalse(decode_revert_reason(_encode("LXP_TRUNCATED: truncated")).retryable)
        self.assertFalse(decode_revert_reason(_encode("LXP_IDEMPOTENT_REPLAY: idempotent replay")).retryable)

    def test_unknown_is_terminal(self) -> None:
        self.assertIs(LXP_RETRYABLE["LXP_UNKNOWN"], False)
        decoded = decode_revert_reason(_encode("LXP_UNKNOWN: unknown; LXP_NOT_REGISTERED; height 9"))
        self.assertEqual((decoded.code, decoded.message, decoded.detail, decoded.retryable), ("LXP_UNKNOWN", "unknown", "LXP_NOT_REGISTERED; height 9", False))
        unlisted = decode_revert_reason(_encode("LXP_NOT_REGISTERED: something"))
        self.assertEqual((unlisted.code, unlisted.message, unlisted.detail, unlisted.retryable), ("LXP_NOT_REGISTERED", "something", None, False))

    def test_literal_wire_bytes_decode(self) -> None:
        wire = bytes.fromhex(
            "08c379a0"
            "0000000000000000000000000000000000000000000000000000000000000020"
            "000000000000000000000000000000000000000000000000000000000000001c"
            "4c58505f5a45524f5f414d4f554e543a207a65726f20616d6f756e7400000000"
        )
        decoded = decode_revert_reason(wire)
        self.assertEqual((decoded.code, decoded.message, decoded.detail, decoded.retryable), ("LXP_ZERO_AMOUNT", "zero amount", None, False))


class RevertRefusalTest(unittest.TestCase):
    def assertRefused(self, return_data: object) -> None:
        with self.assertRaises(PlatformSdkError) as raised:
            decode_revert_reason(return_data)  # type: ignore[arg-type]
        self.assertIs(raised.exception.code, SdkErrorCode.DECODE_FAILURE)
        self.assertEqual(raised.exception.retry, "never")

    def test_payload_without_selector_is_refused(self) -> None:
        encoded = _encode("LXP_TRUNCATED: truncated")
        self.assertRefused(encoded[4:])
        self.assertRefused(bytes.fromhex("4e487b71") + encoded[4:])
        self.assertRefused(b"")
        self.assertRefused(REVERT_SELECTOR)

    def test_malformed_abi_is_refused(self) -> None:
        encoded = _encode("LXP_TRUNCATED: truncated")
        self.assertRefused(encoded.hex())
        self.assertRefused(encoded[:-1])
        self.assertRefused(encoded + bytes(32))
        self.assertRefused(REVERT_SELECTOR + (64).to_bytes(32, "big") + encoded[36:])
        self.assertRefused(REVERT_SELECTOR + encoded[4:36] + (33).to_bytes(32, "big") + encoded[68:])
        self.assertRefused(encoded[:-1] + b"\x01")
        self.assertRefused(REVERT_SELECTOR + (32).to_bytes(32, "big") + (2).to_bytes(32, "big") + b"\xff\xfe" + bytes(30))

    def test_text_outside_the_wire_form_is_refused(self) -> None:
        for text in ("execution reverted", "LXP_TRUNCATED", "LXP_TRUNCATED:", "LXP_TRUNCATED: ", "LXP_: truncated",
                     "lxp_truncated: truncated", "XLP_TRUNCATED: truncated", "LXP_TRUNCATED:truncated", ""):
            with self.subTest(text=text):
                self.assertRefused(_encode(text))


class ReceiptStatusTest(unittest.TestCase):
    def test_status_one_passes(self) -> None:
        self.assertIsNone(raise_for_receipt_status({"rung": "receipted", "status": "0x1", "returnData": "0x"}))

    def test_status_zero_raises_the_registry_code(self) -> None:
        receipt = {"rung": "receipted", "status": "0x0", "returnData": "0x" + _encode("LXP_ACCOUNT_FROZEN: account frozen; sender").hex()}
        with self.assertRaises(RevertReasonError) as raised:
            raise_for_receipt_status(receipt)
        error = raised.exception
        self.assertEqual((error.code, error.message, error.detail, error.retryable), ("LXP_ACCOUNT_FROZEN", "account frozen", "sender", True))
        receipt["returnData"] = "0x" + _encode("LXP_CONSERVATION: conservation").hex().upper()
        with self.assertRaises(RevertReasonError) as raised:
            raise_for_receipt_status(receipt)
        self.assertEqual((raised.exception.code, raised.exception.retryable), ("LXP_CONSERVATION", False))

    def test_malformed_receipt_is_refused(self) -> None:
        valid = "0x" + _encode("LXP_TRUNCATED: truncated").hex()
        for receipt in ({"status": "0x0", "returnData": "0x"}, {"status": "0x0", "returnData": valid[2:]},
                        {"status": "0x0", "returnData": valid[:-1]}, {"status": "0x0", "returnData": valid[:-2] + " 0"},
                        {"status": "0x0"}, {"status": "0x2", "returnData": valid}, {"status": 0, "returnData": valid},
                        {"status": None, "returnData": valid}, {"returnData": valid}):
            with self.subTest(receipt=receipt):
                with self.assertRaises(PlatformSdkError) as raised:
                    raise_for_receipt_status(receipt)
                self.assertIs(raised.exception.code, SdkErrorCode.DECODE_FAILURE)


if __name__ == "__main__":
    unittest.main()
