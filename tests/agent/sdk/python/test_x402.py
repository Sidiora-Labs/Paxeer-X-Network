from __future__ import annotations

import importlib.util
import json
import unittest
from dataclasses import replace
from hashlib import sha256
from pathlib import Path

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

from layerx_sdk import (
    AuthorizedReceiptBatch,
    PlatformSdkError,
    ReceiptFailureCode,
    ReceiptVerificationError,
    verify_payment_receipt,
)
from layerx_sdk.account_derivation import (
    BindFees,
    LayerXBindState,
    derive_from_mnemonic,
    keccak256,
    plan_layerx_bind,
    sign_bind_transaction,
)
from layerx_sdk.production import SdkErrorCode
from layerx_sdk.revert import LXP_RETRYABLE, REVERT_SELECTOR, RevertReasonError
from layerx_sdk.x402 import PAYMENT_RECEIPT_PROTOCOL_VERSIONS, receipt_protocol_version
from layerx_sdk.x402_rpc import (
    PaymentRpcError,
    _committed_result,
    _decode_rpc_response,
    _return_data,
    _send_result,
    _sequencer_result,
    _submission_error,
    _transaction_bytes,
    verify_rpc_payment,
)

_REPO_ROOT = Path(__file__).resolve().parents[4]
_SIGNATURES_PATH = (
    _REPO_ROOT
    / "platform"
    / "integrations"
    / "fastapi"
    / "layerx_fastapi"
    / "signatures.py"
)
_FIXTURES = _REPO_ROOT / "platform" / "sdk" / "conformance" / "fixtures"


def _signature_verifier_class() -> type:
    spec = importlib.util.spec_from_file_location(
        "layerx_fastapi_signatures", _SIGNATURES_PATH
    )
    if spec is None or spec.loader is None:
        raise AssertionError(f"cannot load {_SIGNATURES_PATH}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.LayerXSignatureVerifier


LayerXSignatureVerifier = _signature_verifier_class()


def _fixture(version: int) -> dict:
    return json.loads(
        (_FIXTURES / f"receipt-positive-v{version}.json").read_text(encoding="utf-8")
    )


def _authorized(fixture: dict) -> AuthorizedReceiptBatch:
    batch = fixture["authorized_batch"]
    return AuthorizedReceiptBatch(
        batch_id=bytes.fromhex(batch["batch_id_hex"]),
        asset=bytes.fromhex(batch["asset_hex"]),
        previous_state_root=bytes.fromhex(batch["previous_state_root_hex"]),
        resulting_state_root=bytes.fromhex(batch["resulting_state_root_hex"]),
        sequencer_public_key=bytes.fromhex(batch["sequencer_public_key_hex"]),
    )


def _verify(
    fixture: dict,
    canonical: bytes | None = None,
    *,
    payer: str | None = None,
    amount: str | None = None,
):
    expected = fixture["expected"]
    return verify_payment_receipt(
        (
            bytes.fromhex(fixture["canonical_receipt_hex"])
            if canonical is None
            else canonical
        ),
        _authorized(fixture),
        LayerXSignatureVerifier(),
        amount=expected["amount"] if amount is None else amount,
        asset=fixture["authorized_batch"]["asset_hex"],
        pay_to=expected["to_hex"],
        payer=expected["from_hex"] if payer is None else payer,
    )


def _revert_data(code: str) -> bytes:
    text = f"{code}: payment refused; ledger".encode()
    return (
        REVERT_SELECTOR
        + (32).to_bytes(32, "big")
        + len(text).to_bytes(32, "big")
        + text
        + bytes(-len(text) % 32)
    )


def _signed_revert(
    fixture: dict, return_data: bytes
) -> tuple[bytes, AuthorizedReceiptBatch]:
    canonical = bytearray.fromhex(fixture["canonical_receipt_hex"])
    canonical[126:158] = sha256(return_data).digest()
    canonical[158:162] = (1).to_bytes(4, "big", signed=True)
    key = Ed25519PrivateKey.generate()
    digest = sha256(b"LXP/v1/receipt\0" + canonical[:-69] + b"\0").digest()
    canonical[-64:] = key.sign(digest)
    authorized = replace(
        _authorized(fixture), sequencer_public_key=key.public_key().public_bytes_raw()
    )
    return bytes(canonical), authorized


class VerifyPaymentReceiptTest(unittest.TestCase):
    def test_supported_versions(self) -> None:
        self.assertEqual(PAYMENT_RECEIPT_PROTOCOL_VERSIONS, (2, 3))

    def test_version_three_receipt_verifies(self) -> None:
        fixture = _fixture(3)
        verified = _verify(fixture)
        self.assertEqual(verified.receipt.protocol_version, 3)
        self.assertEqual(
            verified.receipt_digest.hex(), fixture["expected"]["receipt_digest_hex"]
        )
        self.assertEqual(
            receipt_protocol_version(bytes.fromhex(fixture["canonical_receipt_hex"])), 3
        )

    def test_version_two_receipt_verifies(self) -> None:
        fixture = _fixture(2)
        verified = _verify(fixture)
        self.assertEqual(verified.receipt.protocol_version, 2)
        self.assertEqual(
            verified.receipt_digest.hex(), fixture["expected"]["receipt_digest_hex"]
        )
        self.assertEqual(
            receipt_protocol_version(bytes.fromhex(fixture["canonical_receipt_hex"])), 2
        )

    def test_unsupported_version_is_refused_by_name(self) -> None:
        fixture = _fixture(1)
        canonical = bytes.fromhex(fixture["canonical_receipt_hex"])
        self.assertEqual((canonical[0:2], canonical[4:6]), (b"\x00\x01", b"\x00\x01"))
        with self.assertRaises(ReceiptVerificationError) as raised:
            _verify(fixture)
        self.assertIs(raised.exception.check, ReceiptFailureCode.PROTOCOL_VERSION)
        with self.assertRaises(ReceiptVerificationError) as raised:
            receipt_protocol_version(canonical)
        self.assertIs(raised.exception.check, ReceiptFailureCode.PROTOCOL_VERSION)
        recorded = bytes.fromhex(_fixture(3)["canonical_receipt_hex"])
        for changed in (
            b"\x00\x04" + recorded[2:4] + b"\x00\x04" + recorded[6:],
            b"\x00\x03" + recorded[2:4] + b"\x00\x02" + recorded[6:],
        ):
            with self.assertRaises(ReceiptVerificationError) as raised:
                _verify(_fixture(3), changed)
            self.assertIs(raised.exception.check, ReceiptFailureCode.PROTOCOL_VERSION)
        with self.assertRaises(ReceiptVerificationError) as raised:
            receipt_protocol_version(recorded[:5])
        self.assertIs(raised.exception.check, ReceiptFailureCode.DECODE)

    def test_carried_version_does_not_bypass_the_signature(self) -> None:
        fixture = _fixture(2)
        canonical = bytes.fromhex(fixture["canonical_receipt_hex"])
        downgraded = (
            b"\x00\x02"
            + canonical[2:4]
            + b"\x00\x02"
            + bytes.fromhex(_fixture(3)["canonical_receipt_hex"])[6:]
        )
        with self.assertRaises(ReceiptVerificationError):
            _verify(_fixture(3), downgraded)
        corrupted = canonical[:-1] + bytes([canonical[-1] ^ 1])
        with self.assertRaises(ReceiptVerificationError):
            _verify(fixture, corrupted)

    def test_payment_terms_still_bind(self) -> None:
        for version in (2, 3):
            fixture = _fixture(version)
            with self.assertRaises(PlatformSdkError):
                _verify(fixture, payer="ab" * 32)
            with self.assertRaises(PlatformSdkError):
                _verify(fixture, amount=str(int(fixture["expected"]["amount"]) + 1))


class PaymentRpcProtocolTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        fixture = json.loads((_FIXTURES / "account-derivation-v1.json").read_text())
        vector = fixture["mnemonic_vectors"][0]
        account = vector["accounts"][0]
        bind = account["bind"]
        derived = derive_from_mnemonic(
            vector["mnemonic"], vector["passphrase"], account["index"]
        )
        plan = plan_layerx_bind(
            derived, bind["chain_id"], LayerXBindState(None, bind["nonce"])
        )
        if plan.call is None:
            raise AssertionError("bind transaction unavailable")
        cls.raw = _transaction_bytes(
            sign_bind_transaction(
                derived, bind["chain_id"], plan.call, BindFees(0, 1, 2, 100000)
            )
        )
        cls.transaction_hash = keccak256(cls.raw)
        cls.hash_hex = "0x" + cls.transaction_hash.hex()

    def test_send_result_binds_the_signed_evm_transaction(self) -> None:
        self.assertEqual(self.raw[0], 2)
        self.assertEqual(_send_result(self.hash_hex, self.raw), self.hash_hex)
        self.assertEqual(
            _send_result("0x" + self.transaction_hash.hex().upper(), self.raw),
            self.hash_hex,
        )
        with self.assertRaisesRegex(ValueError, "^transaction-hash-mismatch$"):
            _send_result("0x" + bytes(32).hex(), self.raw)

    def test_send_result_refuses_legacy_and_malformed_hashes(self) -> None:
        for result in (
            None,
            {},
            self.hash_hex[2:],
            "0x1",
            "0x" + "gg" * 32,
            self.hash_hex + "00",
        ):
            with self.subTest(result=result):
                with self.assertRaisesRegex(ValueError, "^invalid-rpc-hex$"):
                    _send_result(result, self.raw)

    def test_rpc_envelope_allows_transaction_hash_and_unknown_receipt(self) -> None:
        for result in (self.hash_hex, None, {"rung": "submitted"}):
            with self.subTest(result=result):
                self.assertEqual(
                    _decode_rpc_response(
                        {"jsonrpc": "2.0", "id": 1, "result": result}, 1
                    ),
                    result,
                )

    def test_rpc_envelope_refuses_ambiguous_or_mismatched_responses(self) -> None:
        for body in (
            [],
            {"jsonrpc": "1.0", "id": 1, "result": None},
            {"jsonrpc": "2.0", "id": True, "result": None},
            {"jsonrpc": "2.0", "id": 2, "result": None},
            {"jsonrpc": "2.0", "id": 1},
            {"jsonrpc": "2.0", "id": 1, "result": None, "error": {}},
        ):
            with self.subTest(body=body):
                with self.assertRaisesRegex(ValueError, "^invalid-rpc-envelope$"):
                    _decode_rpc_response(body, 1)
        for result in (0, True, [], 1.0):
            with self.subTest(result=result):
                with self.assertRaisesRegex(ValueError, "^invalid-rpc-result$"):
                    _decode_rpc_response(
                        {"jsonrpc": "2.0", "id": 1, "result": result}, 1
                    )

    def test_rpc_error_keeps_its_code_and_data(self) -> None:
        data = {"nonce": "0x1"}
        with self.assertRaises(PaymentRpcError) as raised:
            _decode_rpc_response(
                {
                    "jsonrpc": "2.0",
                    "id": 1,
                    "error": {"code": -32000, "message": "refused", "data": data},
                },
                1,
            )
        self.assertEqual(raised.exception.code, -32000)
        self.assertEqual(raised.exception.data, data)
        for error in (
            None,
            {"code": True, "message": "refused"},
            {"code": -32000, "message": None},
        ):
            with self.subTest(error=error):
                with self.assertRaisesRegex(ValueError, "^invalid-rpc-error$"):
                    _decode_rpc_response({"jsonrpc": "2.0", "id": 1, "error": error}, 1)

    def test_unknown_and_submitted_receipts_stay_unexecuted(self) -> None:
        self.assertIsNone(_sequencer_result(None, self.transaction_hash))
        submitted = {"txHash": self.hash_hex, "nonce": "0x0", "rung": "submitted"}
        self.assertEqual(_sequencer_result(submitted, self.transaction_hash), submitted)
        for changed in (
            {**submitted, "txHash": "0x" + bytes(32).hex()},
            {**submitted, "rung": "completed"},
            {**submitted, "status": "0x1"},
            {**submitted, "nonce": "0x00"},
            {**submitted, "nonce": "0x" + "f" * 17},
        ):
            with self.subTest(changed=changed):
                with self.assertRaisesRegex(ValueError, "^invalid-sequencer-receipt$"):
                    _sequencer_result(changed, self.transaction_hash)

    def test_committed_rpc_requires_a_block_hash_and_matching_transaction(self) -> None:
        self.assertIsNone(_committed_result(None, self.transaction_hash))
        receipt = {
            "transactionHash": self.hash_hex,
            "blockHash": "0x" + sha256(self.raw).hexdigest(),
            "status": "0x1",
        }
        self.assertEqual(_committed_result(receipt, self.transaction_hash), receipt)
        for changed in (
            {**receipt, "transactionHash": "0x" + bytes(32).hex()},
            {**receipt, "status": 1},
            {**receipt, "status": "0x2"},
        ):
            with self.subTest(changed=changed):
                with self.assertRaisesRegex(
                    ValueError, "^invalid-transaction-receipt$"
                ):
                    _committed_result(changed, self.transaction_hash)
        with self.assertRaisesRegex(ValueError, "^invalid-rpc-hex$"):
            _committed_result({**receipt, "blockHash": None}, self.transaction_hash)

    def test_submission_reverts_keep_registry_codes_and_retry_classes(self) -> None:
        for code, retryable in LXP_RETRYABLE.items():
            for data in (
                "0x" + _revert_data(code).hex(),
                {"data": "0x" + _revert_data(code).hex()},
            ):
                with self.subTest(code=code, nested=isinstance(data, dict)):
                    error = PaymentRpcError(3, data)
                    with self.assertRaises(RevertReasonError) as raised:
                        _submission_error(error)
                    self.assertEqual(
                        (
                            raised.exception.code,
                            raised.exception.retryable,
                            raised.exception.detail,
                        ),
                        (code, retryable, "ledger"),
                    )
                    self.assertIs(raised.exception.__cause__, error)

    def test_non_revert_rpc_errors_keep_the_existing_mapping(self) -> None:
        for data in (None, {"state": "pending"}, "unavailable"):
            with self.subTest(data=data):
                self.assertIsNone(_submission_error(PaymentRpcError(-32000, data)))

    def test_malformed_submission_revert_is_refused(self) -> None:
        for data in ("0x", "0x00", "0x0", "0xgg"):
            with self.subTest(data=data):
                with self.assertRaises(PlatformSdkError) as raised:
                    _submission_error(PaymentRpcError(3, data))
                self.assertIs(raised.exception.code, SdkErrorCode.DECODE_FAILURE)
                self.assertEqual(raised.exception.retry, "never")

    def test_return_data_accepts_empty_evm_results(self) -> None:
        self.assertEqual(_return_data("0x"), b"")
        self.assertEqual(_return_data("0xAa"), b"\xaa")
        for data in (None, "", "aa", "0x1", "0xgg"):
            with self.subTest(data=data):
                with self.assertRaisesRegex(ValueError, "^invalid-rpc-hex$"):
                    _return_data(data)


class PaymentRevertBindingTest(unittest.TestCase):
    def verify(self, fixture, canonical, authorized, return_data):
        expected = fixture["expected"]
        return verify_payment_receipt(
            canonical,
            authorized,
            LayerXSignatureVerifier(),
            amount=expected["amount"],
            asset=fixture["authorized_batch"]["asset_hex"],
            pay_to=expected["to_hex"],
            payer=expected["from_hex"],
            return_data=return_data,
        )

    def test_signed_revert_reports_retriable_and_terminal_codes(self) -> None:
        for version in (2, 3):
            for code in ("LXP_ACCOUNT_FROZEN", "LXP_IDEMPOTENT_REPLAY", "LXP_UNKNOWN"):
                with self.subTest(version=version, code=code):
                    fixture = _fixture(version)
                    data = _revert_data(code)
                    canonical, authorized = _signed_revert(fixture, data)
                    with self.assertRaises(RevertReasonError) as raised:
                        self.verify(fixture, canonical, authorized, data)
                    self.assertEqual(
                        (raised.exception.code, raised.exception.retryable),
                        (code, LXP_RETRYABLE[code]),
                    )

    def test_unbound_return_data_is_refused_before_decoding_the_revert(self) -> None:
        fixture = _fixture(2)
        data = _revert_data("LXP_ACCOUNT_FROZEN")
        canonical, authorized = _signed_revert(fixture, data)
        with self.assertRaises(PlatformSdkError) as raised:
            self.verify(
                fixture, canonical, authorized, _revert_data("LXP_IDEMPOTENT_REPLAY")
            )
        self.assertIs(raised.exception.code, SdkErrorCode.VERIFICATION_FAILURE)
        self.assertEqual(raised.exception.retry, "never")

    def test_revert_signature_is_required_before_return_data_is_decoded(self) -> None:
        fixture = _fixture(2)
        data = _revert_data("LXP_ACCOUNT_FROZEN")
        canonical, authorized = _signed_revert(fixture, data)
        corrupt = canonical[:-1] + bytes([canonical[-1] ^ 1])
        with self.assertRaises(ReceiptVerificationError) as raised:
            self.verify(fixture, corrupt, authorized, data)
        self.assertIs(raised.exception.check, ReceiptFailureCode.SEQUENCER_SIGNATURE)

    def test_revert_without_return_data_keeps_the_result_code_refusal(self) -> None:
        fixture = _fixture(2)
        canonical, authorized = _signed_revert(
            fixture, _revert_data("LXP_ACCOUNT_FROZEN")
        )
        with self.assertRaises(ReceiptVerificationError) as raised:
            self.verify(fixture, canonical, authorized, None)
        self.assertIs(raised.exception.check, ReceiptFailureCode.RESULT_CODE)

    def test_sequencer_transport_propagates_the_revert_registry_class(self) -> None:
        fixture = _fixture(2)
        transaction_hash = bytes.fromhex(fixture["expected"]["activity_id_hex"])
        data = _revert_data("LXP_ACCOUNT_FROZEN")
        canonical, authorized = _signed_revert(fixture, data)
        result = {
            "rung": "receipted",
            "txHash": "0x" + transaction_hash.hex(),
            "receipt": "0x" + canonical.hex(),
            "keyId": "0x" + authorized.sequencer_public_key.hex(),
            "status": "0x0",
            "returnData": "0x" + data.hex(),
        }
        with self.assertRaises(RevertReasonError) as raised:
            _sequencer_result(result, transaction_hash)
        self.assertEqual(
            (raised.exception.code, raised.exception.retryable),
            ("LXP_ACCOUNT_FROZEN", True),
        )

    def test_rpc_payment_propagates_the_signed_revert_code(self) -> None:
        fixture = _fixture(2)
        data = _revert_data("LXP_IDEMPOTENT_REPLAY")
        canonical, authorized = _signed_revert(fixture, data)
        expected = fixture["expected"]
        with self.assertRaises(RevertReasonError) as raised:
            verify_rpc_payment(
                {
                    "activity_id": expected["activity_id_hex"],
                    "receipt": canonical.hex(),
                    "returnData": "0x" + data.hex(),
                },
                expected["activity_id_hex"],
                expected["from_hex"],
                authorized,
                LayerXSignatureVerifier(),
                amount=expected["amount"],
                asset=fixture["authorized_batch"]["asset_hex"],
                pay_to=expected["to_hex"],
            )
        self.assertEqual(
            (raised.exception.code, raised.exception.retryable),
            ("LXP_IDEMPOTENT_REPLAY", False),
        )


if __name__ == "__main__":
    unittest.main()
