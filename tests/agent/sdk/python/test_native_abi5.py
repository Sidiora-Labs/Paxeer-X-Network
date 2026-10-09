from __future__ import annotations

import importlib.util
import json
import sys
import unittest
from dataclasses import replace
from hashlib import sha256
from pathlib import Path

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat

ROOT = Path(__file__).resolve().parents[4]
sys.path.insert(0, str(ROOT / "agent/sdk/python"))

from layerx_sdk.generated.receipt import (  # noqa: E402
    PROGRAM_ABI_V5,
    ReceiptFailureCode,
    supports_program_guest_abi,
)
from layerx_sdk.native_program_call import (  # noqa: E402
    decode_native_program_call,
    encode_native_program_call,
    native_guest_abi_for_protocol,
)
from layerx_sdk.programs import ProgramTrustContext, verify_program_receipt  # noqa: E402
from layerx_sdk.verifier import (  # noqa: E402
    AuthorizedReceiptBatch,
    ReceiptVerificationError,
    decode_program_receipt_outcome,
    verify_receipt_outcome,
)

SIGNATURES_SPEC = importlib.util.spec_from_file_location(
    "native_abi5_signatures", ROOT / "platform/integrations/fastapi/layerx_fastapi/signatures.py")
if SIGNATURES_SPEC is None or SIGNATURES_SPEC.loader is None:
    raise RuntimeError("real signature verifier is unavailable")
SIGNATURES = importlib.util.module_from_spec(SIGNATURES_SPEC)
SIGNATURES_SPEC.loader.exec_module(SIGNATURES)
VERIFIER = SIGNATURES.LayerXSignatureVerifier()

FIXTURES = ROOT / "platform/sdk/conformance/fixtures"
CALL = json.loads((FIXTURES / "native-program-call-v4.json").read_text())
EXECUTED = json.loads((FIXTURES / "receipt-programs-executed-v4.json").read_text())
BATCH = EXECUTED["authorized_batch"]
AUTHORITY = AuthorizedReceiptBatch(
    bytes.fromhex(BATCH["batch_id_hex"]), bytes.fromhex(BATCH["asset_hex"]),
    bytes.fromhex(BATCH["previous_state_root_hex"]), bytes.fromhex(BATCH["resulting_state_root_hex"]),
    bytes.fromhex(BATCH["sequencer_public_key_hex"]))
ORIGINAL = bytes.fromhex(EXECUTED["canonical_receipt_hex"])
SEQUENCER = Ed25519PrivateKey.from_private_bytes(b"\x45" + bytes(31))
MODULE_HEADER = b"\0\0\0\x20" + AUTHORITY.batch_id + b"\0\x09"
MODULE_VERSION_AT = ORIGINAL.index(MODULE_HEADER) + len(MODULE_HEADER)
OUTCOME_AT = ORIGINAL.rindex(b"PRG4")


def receipt_with(module_version: int, abi: int) -> bytes:
    receipt = bytearray(ORIGINAL)
    receipt[MODULE_VERSION_AT:MODULE_VERSION_AT + 4] = module_version.to_bytes(4, "big")
    receipt[OUTCOME_AT + 11:OUTCOME_AT + 13] = abi.to_bytes(2, "big")
    digest = sha256(b"LXP/v1/receipt\0" + bytes(receipt[:-69]) + b"\0").digest()
    receipt[-64:] = SEQUENCER.sign(digest)
    return bytes(receipt)


class NativeAbi5Test(unittest.TestCase):
    def assert_refused(self, receipt: bytes, check: ReceiptFailureCode) -> None:
        with self.assertRaises(ReceiptVerificationError) as raised:
            verify_receipt_outcome(receipt, AUTHORITY, VERIFIER, protocol_version=3)
        self.assertEqual(raised.exception.check, check)

    def test_generated_policy_admits_exactly_the_fifth_version(self) -> None:
        self.assertEqual(PROGRAM_ABI_V5, 5)
        self.assertEqual([supports_program_guest_abi(abi) for abi in (0, 1, 2, 3, 4, 5, 6, 65_535)],
                         [False, True, True, True, True, True, False, False])
        self.assertEqual([native_guest_abi_for_protocol(5, protocol) for protocol in (1, 2, 3, 4)],
                         [False, False, True, False])
        self.assertEqual([native_guest_abi_for_protocol(abi, 2) for abi in (1, 2, 3, 4, 5)],
                         [True, True, False, False, False])

    def test_native_call_encodes_and_decodes_guest_abi_5(self) -> None:
        payload_v4 = bytes.fromhex(CALL["payload_hex"])
        self.assertEqual(payload_v4[32:34], b"\0\x02")
        payload_v5 = payload_v4[:32] + b"\0\x05" + payload_v4[34:]
        decoded = decode_native_program_call(payload_v5)
        self.assertEqual(decoded.guest_abi, 5)
        self.assertEqual(encode_native_program_call(decoded), payload_v5)
        from_v4 = decode_native_program_call(payload_v4)
        self.assertEqual(replace(from_v4, guest_abi=5), decoded)
        self.assertEqual(encode_native_program_call(replace(from_v4, guest_abi=5)), payload_v5)
        self.assertEqual(decoded.resources, tuple(int(value) for value in CALL["resources"]))
        for abi in (0, 6, 65_535):
            with self.assertRaisesRegex(ValueError, "^invalid native program call$"):
                decode_native_program_call(payload_v4[:32] + abi.to_bytes(2, "big") + payload_v4[34:])
            with self.assertRaisesRegex(ValueError, "^invalid native program call$"):
                encode_native_program_call(replace(from_v4, guest_abi=abi))

    def test_receipt_outcome_carries_guest_abi_5_under_module_version_5(self) -> None:
        public = SEQUENCER.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
        self.assertEqual(public, AUTHORITY.sequencer_public_key)
        self.assertEqual(ORIGINAL.count(MODULE_HEADER), 1)
        self.assertEqual(int.from_bytes(ORIGINAL[MODULE_VERSION_AT:MODULE_VERSION_AT + 4], "big"), 4)
        self.assertEqual((ORIGINAL[OUTCOME_AT + 4], ORIGINAL[OUTCOME_AT + 9:OUTCOME_AT + 13]), (1, b"\0\x01\0\x02"))
        self.assertEqual(receipt_with(4, 2), ORIGINAL)
        receipt = receipt_with(5, 5)
        outcome = decode_program_receipt_outcome(receipt[OUTCOME_AT:-69], 3)
        self.assertEqual((outcome.abi_version, outcome.runtime_version, outcome.encoding_version, outcome.terminal_kind,
                          outcome.result_code), (5, 1, 4, 1, 0))
        self.assertEqual(decode_program_receipt_outcome(ORIGINAL[OUTCOME_AT:-69], 3), replace(outcome, abi_version=2))

    def test_receipt_verification_pairs_guest_abi_5_with_module_version_5(self) -> None:
        legacy = verify_receipt_outcome(receipt_with(5, 2), AUTHORITY, VERIFIER, protocol_version=3)
        self.assertEqual((legacy.receipt.module_version, legacy.receipt.program_outcome.abi_version), (5, 2))
        self.assertEqual(legacy.receipt_digest, sha256(b"LXP/v1/receipt\0" + receipt_with(5, 2)[:-69] + b"\0").digest())
        self.assert_refused(receipt_with(4, 5), ReceiptFailureCode.PROTOCOL_VERSION)
        self.assert_refused(receipt_with(5, 5), ReceiptFailureCode.PROTOCOL_VERSION)
        self.assert_refused(receipt_with(6, 5), ReceiptFailureCode.MODULE_VERSION)
        tampered = bytearray(receipt_with(5, 2))
        tampered[OUTCOME_AT + 12] = 1
        self.assert_refused(bytes(tampered), ReceiptFailureCode.SEQUENCER_SIGNATURE)

    def test_program_execution_evidence_requires_module_version_5_for_guest_abi_5(self) -> None:
        trust = ProgramTrustContext(AUTHORITY.sequencer_public_key, protocol_version=3, network_id=EXECUTED["network_id"])

        def execution(module_version: int) -> dict[str, object]:
            receipt = receipt_with(module_version, 5)
            return {
                "state": "executed", "activity_id": sha256(b"LXP/v1/activity-id\0" + bytes.fromhex(EXECUTED["signed_activity_hex"])).hexdigest(),
                "program_id": EXECUTED["program_id_hex"], "guest_abi_version": 5, "module_version": module_version,
                "batch_id": BATCH["batch_id_hex"], "global_sequence": "1", "result_code": 0,
                "state_root": BATCH["resulting_state_root_hex"], "receipt": receipt.hex(),
                "receipt_digest": sha256(b"LXP/v1/receipt\0" + receipt[:-69] + b"\0").hexdigest(),
                "terminal_payload": EXECUTED["terminal_payload_hex"], "call_graph": EXECUTED["call_graph_hex"],
                "authority": {"batch_id": BATCH["batch_id_hex"], "asset": BATCH["asset_hex"],
                              "previous_state_root": BATCH["previous_state_root_hex"],
                              "resulting_state_root": BATCH["resulting_state_root_hex"],
                              "sequencer_public_key": BATCH["sequencer_public_key_hex"]},
            }

        with self.assertRaisesRegex(ValueError, "^invalid program execution evidence$"):
            verify_program_receipt(execution(4), AUTHORITY, VERIFIER, trust)
        with self.assertRaisesRegex(ValueError, "^v5 receipt requires actual retained signed request$"):
            verify_program_receipt(execution(5), AUTHORITY, VERIFIER, trust)


if __name__ == "__main__":
    unittest.main()
