from __future__ import annotations

import json
import re
import urllib.parse
import urllib.request
from collections.abc import Mapping
from urllib.error import URLError

from .account_derivation import keccak256
from .production import PlatformSdkError, SdkErrorCode
from .revert import decode_revert_reason, raise_for_receipt_status
from .x402 import verify_payment_receipt


def rpc_hex(value: object, size: int | None = None) -> bytes:
    if (
        not isinstance(value, str)
        or not 0 < len(value) <= 2097152
        or re.fullmatch(r"(?:[0-9a-f]{2})+", value) is None
        or size is not None
        and len(value) != size * 2
    ):
        raise ValueError("invalid-rpc-hex")
    return bytes.fromhex(value)


class PaymentRpcError(Exception):
    def __init__(self, code: int, data: object):
        super().__init__(f"JSON-RPC error {code}")
        self.code = code
        self.data = data


def _transaction_bytes(value: object, size: int | None = None) -> bytes:
    if not isinstance(value, str) or not value.startswith("0x"):
        raise ValueError("invalid-rpc-hex")
    return rpc_hex(value[2:].lower(), size)


def _return_data(value: object) -> bytes:
    return b"" if value == "0x" else _transaction_bytes(value)


def _decode_rpc_response(body: object, request_id: int) -> dict | str | None:
    if (
        not isinstance(body, dict)
        or body.get("jsonrpc") != "2.0"
        or type(body.get("id")) is not int
        or body["id"] != request_id
        or ("error" in body) == ("result" in body)
    ):
        raise ValueError("invalid-rpc-envelope")
    if "error" in body:
        error = body["error"]
        if (
            not isinstance(error, dict)
            or type(error.get("code")) is not int
            or not isinstance(error.get("message"), str)
        ):
            raise ValueError("invalid-rpc-error")
        raise PaymentRpcError(error["code"], error.get("data"))
    result = body["result"]
    if result is None or isinstance(result, (dict, str)):
        return result
    raise ValueError("invalid-rpc-result")


def _send_result(result: object, raw_transaction: bytes) -> str:
    transaction_hash = _transaction_bytes(result, 32)
    if transaction_hash != keccak256(raw_transaction):
        raise ValueError("transaction-hash-mismatch")
    return "0x" + transaction_hash.hex()


def _sequencer_result(result: object, transaction_hash: bytes) -> dict | None:
    if result is None:
        return None
    if (
        not isinstance(result, dict)
        or _transaction_bytes(result.get("txHash"), 32) != transaction_hash
        or result.get("rung") not in ("submitted", "receipted", "committed")
    ):
        raise ValueError("invalid-sequencer-receipt")
    if result["rung"] == "submitted":
        if set(result) != {"txHash", "nonce", "rung"}:
            raise ValueError("invalid-sequencer-receipt")
        nonce = result["nonce"]
        if (
            not isinstance(nonce, str)
            or re.fullmatch(r"0x(?:0|[1-9a-f][0-9a-f]*)", nonce) is None
            or len(nonce) > 18
        ):
            raise ValueError("invalid-sequencer-receipt")
        return result
    if result.get("status") not in ("0x0", "0x1"):
        raise ValueError("invalid-sequencer-receipt")
    _transaction_bytes(result.get("receipt"))
    _transaction_bytes(result.get("keyId"), 32)
    if result["rung"] == "committed":
        _transaction_bytes(result.get("blockHash"), 32)
    elif "blockHash" in result:
        raise ValueError("invalid-sequencer-receipt")
    raise_for_receipt_status(result)
    return result


def _committed_result(result: object, transaction_hash: bytes) -> dict | None:
    if result is None:
        return None
    if (
        not isinstance(result, dict)
        or _transaction_bytes(result.get("transactionHash"), 32) != transaction_hash
        or result.get("status") not in ("0x0", "0x1")
    ):
        raise ValueError("invalid-transaction-receipt")
    _transaction_bytes(result.get("blockHash"), 32)
    return result


def _submission_error(error: PaymentRpcError) -> None:
    data = error.data
    if isinstance(data, Mapping):
        data = data.get("data")
    if isinstance(data, str) and (error.code == 3 or data[:10].lower() == "0x08c379a0"):
        try:
            return_data = _return_data(data)
        except ValueError as malformed:
            raise PlatformSdkError(SdkErrorCode.DECODE_FAILURE, "never") from malformed
        raise decode_revert_reason(return_data) from error


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise ValueError("rpc-redirect")


class PaymentRpc:
    def __init__(self, endpoint: str, headers: Mapping[str, str] | None = None):
        url = urllib.parse.urlsplit(endpoint)
        if (
            url.username
            or url.password
            or url.fragment
            or url.query
            or url.path != "/rpc"
            or not url.hostname
            or url.scheme != "https"
            and not (
                url.scheme == "http"
                and url.hostname in ("localhost", "127.0.0.1", "::1")
            )
        ):
            raise ValueError("invalid-rpc-endpoint")
        self.endpoint = endpoint
        self.headers = dict(headers or {})
        self._id = 0
        self._opener = urllib.request.build_opener(_NoRedirect())

    def call(self, method: str, params: list) -> dict | str | None:
        self._id += 1
        request_id = self._id
        request = urllib.request.Request(
            self.endpoint,
            method="POST",
            headers={**self.headers, "Content-Type": "application/json"},
            data=json.dumps(
                {"jsonrpc": "2.0", "id": request_id, "method": method, "params": params}
            ).encode(),
        )
        with self._opener.open(request, timeout=30) as response:
            raw = response.read(8388609)
        if len(raw) > 8388608:
            raise ValueError("rpc-body-too-large")
        return _decode_rpc_response(json.loads(raw), request_id)

    def send(self, raw_transaction: str) -> str:
        canonical = _transaction_bytes(raw_transaction)
        if len(canonical) > 524288:
            raise ValueError("invalid-rpc-submit")
        try:
            result = self.call("eth_sendRawTransaction", [raw_transaction])
        except PaymentRpcError as error:
            _submission_error(error)
            raise
        except (URLError, TimeoutError, OSError) as error:
            raise PlatformSdkError(
                SdkErrorCode.UNKNOWN_OUTCOME, "unknown-outcome"
            ) from error
        return _send_result(result, canonical)

    def receipt(self, transaction_hash: str) -> dict | None:
        expected = _transaction_bytes(transaction_hash, 32)
        return _sequencer_result(
            self.call("pax_sequencerReceipt", [transaction_hash]), expected
        )

    def status(self, transaction_hash: str) -> dict | None:
        expected = _transaction_bytes(transaction_hash, 32)
        result = _committed_result(
            self.call("eth_getTransactionReceipt", [transaction_hash]), expected
        )
        if result is not None and result["status"] == "0x0":
            sequencer = self.receipt(transaction_hash)
            if sequencer is None:
                raise ValueError("revert-receipt-unavailable")
            raise ValueError("revert-receipt-mismatch")
        return result


def verify_rpc_payment(
    result,
    expected_activity,
    expected_payer,
    authorized,
    signatures,
    *,
    amount,
    asset,
    pay_to,
    commitment="executed",
    evidence=None,
):
    rpc_hex(expected_activity, 32)
    rpc_hex(expected_payer, 32)
    if result.get("activity_id") != expected_activity:
        raise ValueError("activity-mismatch")
    if result.get("state") == "pending":
        return None
    if result.get("state") not in (None, "completed"):
        raise ValueError("payment-refused")
    if "commitment" in result and result["commitment"] != commitment:
        raise ValueError("commitment-mismatch")
    try:
        verified = verify_payment_receipt(
            rpc_hex(result.get("receipt")),
            authorized,
            signatures,
            amount=amount,
            asset=asset,
            pay_to=pay_to,
            payer=expected_payer,
            commitment=commitment,
            evidence=evidence,
            return_data=(
                _return_data(result["returnData"]) if "returnData" in result else None
            ),
        )
    except PlatformSdkError as error:
        raise ValueError("payment-binding-mismatch") from error
    if (
        verified.receipt.activity_id.hex() != expected_activity
        or verified.receipt.from_account.hex() != expected_payer
    ):
        raise ValueError("payment-binding-mismatch")
    return verified


def rpc_batch_evidence(result, activity_id, receipt, network_id, authorization):
    from .verifier import MerkleProof
    from .x402 import PaymentCommitmentEvidence

    bundle = result["batch_evidence"]
    signed = bundle["signed_header"]
    proof = bundle["proof"]
    if (
        bundle["kind"] != "receipt"
        or bundle["activity_id"] != activity_id
        or rpc_hex(bundle["canonical_value"]) != receipt
        or rpc_hex(signed["public_key"], 32) != authorization.public_key
        or rpc_hex(signed["sequencer_id"], 32) != authorization.sequencer_id
        or type(proof["leaf_index"]) is not int
        or type(proof["leaf_count"]) is not int
        or not isinstance(proof["siblings"], list)
        or len(proof["siblings"]) > 64
    ):
        raise ValueError("batch-binding-mismatch")
    return PaymentCommitmentEvidence(
        network_id,
        rpc_hex(signed["canonical_header"]),
        rpc_hex(signed["signature"], 64),
        authorization,
        MerkleProof(
            proof["leaf_index"],
            proof["leaf_count"],
            tuple(rpc_hex(v, 32) for v in proof["siblings"]),
        ),
    )
