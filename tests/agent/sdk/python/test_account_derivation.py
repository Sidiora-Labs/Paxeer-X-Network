from __future__ import annotations

import json
import os
import re
import shutil
import socket
import subprocess
import tempfile
import time
import unittest
from dataclasses import replace
from pathlib import Path
from urllib.error import URLError
from urllib.request import Request, urlopen

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey

from layerx_sdk.account_derivation import (
    ADDR_PRECOMPILE_ADDRESS,
    LAYERX_COIN_TYPE,
    AccountDerivationError,
    BindFees,
    LayerXBindState,
    _bind_quantity,
    _bind_rpc,
    bind_nonce_call,
    bind_transaction_request,
    bound_did_call,
    decode_bind_nonce,
    decode_bound_did,
    derive_from_mnemonic,
    derive_from_seed,
    derive_from_wallet_signature,
    keccak256,
    key_derivation_hash,
    key_derivation_typed_data,
    layerx_bind_message,
    normalize_wallet_signature,
    plan_layerx_bind,
    send_bind_transaction,
    sign_bind_transaction,
    slip10_ed25519,
)
from layerx_sdk.exchange import PrecompileCall, precompile_transaction_request
from layerx_sdk.production import PlatformSdkError, SdkErrorCode
from layerx_sdk.revert import (
    REVERT_SELECTOR,
    RevertReasonError,
    raise_for_receipt_status,
)
from layerx_sdk.x402_rpc import (
    PaymentRpc,
    PaymentRpcError,
    _decode_rpc_response,
    _submission_error,
)

FIXTURE = json.loads(
    (
        Path(__file__).resolve().parents[4]
        / "sdk/platform/conformance/fixtures/account-derivation-v1.json"
    ).read_text()
)
ABANDON = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"


class AccountDerivationTest(unittest.TestCase):
    def refuses(self, code: str, run) -> None:
        with self.assertRaises(AccountDerivationError) as raised:
            run()
        self.assertEqual(raised.exception.code, code)

    def test_keccak_matches_published_digests(self) -> None:
        self.assertEqual(
            keccak256(b"").hex(),
            "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470",
        )
        self.assertEqual(
            keccak256(b"a" * 200).hex(),
            keccak256(b"a" * 136 + b"a" * 64).hex(),
        )

    def test_fixed_seed_bind_proof_on_paxeer_chain(self) -> None:
        derived = derive_from_seed(
            bytes.fromhex(FIXTURE["mnemonic_vectors"][0]["seed"])
        )
        plan = plan_layerx_bind(derived, 125, LayerXBindState(None, 0))
        self.assertIsNotNone(plan.call)
        call = plan.call
        message = (
            b"LX:PAXEER-BIND:v1"
            + bytes.fromhex("00" * 31 + "7d")
            + bytes.fromhex("9858effd232b4033e47d90003d41ec34ecaeda94")
            + bytes(8)
        )
        self.assertEqual(layerx_bind_message(125, derived.evm_address, 0), message)
        self.assertEqual(
            derived.layerx_public_key,
            "0e0dd0e63aae0420ae0e2aa896013a983facb1ce537be3d368896d81412d7bb9",
        )
        Ed25519PublicKey.from_public_bytes(bytes.fromhex(call.did_public_key)).verify(
            bytes.fromhex(call.signature), message
        )
        self.assertEqual(call.nonce, 0)
        self.assertEqual(
            call.data,
            "0xdd9aa628"
            + derived.layerx_public_key
            + (64).to_bytes(32, "big").hex() * 2
            + call.signature,
        )
        self.assertEqual(
            bind_transaction_request(derived, call),
            precompile_transaction_request(
                derived.evm_address, PrecompileCall(ADDR_PRECOMPILE_ADDRESS, call.data)
            ),
        )

    def test_bind_quantity_refuses_noncanonical_and_out_of_range_answers(self) -> None:
        for bits in (53, 64, 128):
            with self.subTest(bits=bits):
                self.assertEqual(_bind_quantity("0x0", bits), 0)
                self.assertEqual(
                    _bind_quantity(hex((1 << bits) - 1), bits), (1 << bits) - 1
                )
                for answer in (
                    None,
                    True,
                    1,
                    "0x",
                    "0X1",
                    "0x01",
                    "0x-1",
                    "0xgg",
                    "0x1 ",
                    hex(1 << bits),
                ):
                    self.refuses(
                        "malformed_precompile_answer",
                        lambda: _bind_quantity(answer, bits),
                    )

    def test_bind_signer_refuses_invalid_fees_keys_and_calls(self) -> None:
        derived = derive_from_seed(
            bytes.fromhex(FIXTURE["mnemonic_vectors"][0]["seed"])
        )
        call = plan_layerx_bind(derived, 125, LayerXBindState(None, 0)).call
        self.assertIsNotNone(call)
        fees = BindFees(0, 1, 2, 200000)
        for bad in (
            None,
            replace(fees, evm_nonce=-1),
            replace(fees, evm_nonce=True),
            replace(fees, evm_nonce=1 << 64),
            replace(fees, max_priority_fee_per_gas=-1),
            replace(fees, max_priority_fee_per_gas=True),
            replace(fees, max_priority_fee_per_gas=1 << 128),
            replace(fees, max_priority_fee_per_gas=3),
            replace(fees, max_fee_per_gas=-1),
            replace(fees, max_fee_per_gas=True),
            replace(fees, max_fee_per_gas=1 << 128),
            replace(fees, gas_limit=-1),
            replace(fees, gas_limit=True),
            replace(fees, gas_limit=0),
            replace(fees, gas_limit=1 << 64),
        ):
            with self.subTest(fees=bad):
                self.refuses(
                    "invalid_bind_fees",
                    lambda: sign_bind_transaction(derived, 125, call, bad),
                )
        for key in (bytes(32), bytes(31), bytes.fromhex("ff" * 32)):
            self.refuses(
                "invalid_evm_key",
                lambda: sign_bind_transaction(
                    replace(derived, evm_private_key=key), 125, call, fees
                ),
            )
        self.refuses(
            "evm_key_unavailable",
            lambda: sign_bind_transaction(
                replace(derived, evm_private_key=None), 125, call, fees
            ),
        )
        self.refuses(
            "wallet_signer_mismatch",
            lambda: sign_bind_transaction(
                replace(derived, evm_private_key=(1).to_bytes(32, "big")),
                125,
                call,
                fees,
            ),
        )
        for bad in (
            replace(call, to="0x" + "11" * 20),
            replace(call, data=call.data[:-2] + "00"),
            replace(call, did_public_key="00" * 32),
            replace(call, signature="00" * 64),
        ):
            with self.subTest(call=bad):
                self.refuses(
                    "malformed_precompile_answer",
                    lambda: sign_bind_transaction(derived, 125, bad, fees),
                )

    def test_bind_reverts_preserve_registry_code_and_retry_class(self) -> None:
        refusals = (
            ("LXP_ADDR_LAYERX_SIGNATURE", "layerx binding: signature refused", False),
            (
                "LXP_ADDR_LAYERX_ADDRESS_BOUND",
                "layerx binding: evm address is already bound to a did",
                False,
            ),
            (
                "LXP_ADDR_LAYERX_DID_BOUND",
                "layerx binding: did is already bound to an evm address",
                False,
            ),
            (
                "LXP_ADDR_LAYERX_NOT_BOUND",
                "layerx binding: evm address is not bound to a did",
                True,
            ),
            (
                "LXP_ADDR_LAYERX_NONCE_EXHAUSTED",
                "layerx binding: nonce exhausted",
                False,
            ),
            (
                "LXP_ADDR_LAYERX_NON_CANONICAL_KEY",
                "layerx binding: did public key is not canonical",
                False,
            ),
            (
                "LXP_ADDR_LAYERX_SIGNATURE_LENGTH",
                "layerx binding: signature must be 64 bytes",
                False,
            ),
            (
                "LXP_ADDR_LAYERX_CHAIN_ID",
                "layerx binding: chain id does not fit 32 bytes",
                False,
            ),
        )
        for code, message, retryable in refusals:
            with self.subTest(code=code):
                text = f"{code}: {message}; bind".encode()
                encoded = (
                    "0x"
                    + (
                        REVERT_SELECTOR
                        + (32).to_bytes(32, "big")
                        + len(text).to_bytes(32, "big")
                        + text
                        + bytes(-len(text) % 32)
                    ).hex()
                )
                for data in (encoded, {"data": encoded}):
                    error = PaymentRpcError(3, data)
                    with self.assertRaises(RevertReasonError) as raised:
                        _submission_error(error)
                    self.assertEqual(
                        (
                            raised.exception.code,
                            raised.exception.message,
                            raised.exception.detail,
                            raised.exception.retryable,
                        ),
                        (code, message, "bind", retryable),
                    )
                    self.assertIs(raised.exception.__cause__, error)
                with self.assertRaises(RevertReasonError) as raised:
                    raise_for_receipt_status({"status": "0x0", "returnData": encoded})
                self.assertEqual(
                    (raised.exception.code, raised.exception.retryable),
                    (code, retryable),
                )
        for data in ("0x", "0x08c379a0", "0xgg"):
            with self.assertRaises(PlatformSdkError) as raised:
                _submission_error(PaymentRpcError(3, data))
            self.assertEqual(
                (raised.exception.code, raised.exception.retry),
                (SdkErrorCode.DECODE_FAILURE, "never"),
            )

    def test_slip10_official_ed25519_vector(self) -> None:
        seed = bytes.fromhex("000102030405060708090a0b0c0d0e0f")
        self.assertEqual(
            slip10_ed25519(seed, ()).hex(),
            "2b4be7f19ee27bbf30c667b642d5f4aa69fd169872f8fc3059c08ebae2eb19e7",
        )
        self.assertEqual(
            slip10_ed25519(seed, (0, 1)).hex(),
            "b1d0bad404bf35da785a64ca1ac54b2617211d2777696fbffaf208f746ae84f2",
        )
        self.assertEqual(
            slip10_ed25519(seed, (0, 1, 2, 2, 1000000000)).hex(),
            "8f94d394a8e8fd6b1bc2f3f49f5c47e385281d5c17e65324b0f62483e37e8793",
        )

    def test_mnemonic_vectors_and_bind_calls(self) -> None:
        self.assertEqual(FIXTURE["layerx_coin_type"], LAYERX_COIN_TYPE)
        seen = 0
        for vector in FIXTURE["mnemonic_vectors"]:
            for expected in vector["accounts"]:
                derived = derive_from_mnemonic(
                    "  " + vector["mnemonic"].replace(" ", "  ") + "\n",
                    vector["passphrase"],
                    expected["index"],
                )
                self.assertEqual(derived.evm_address, expected["evm_address"])
                self.assertEqual(
                    derived.layerx_public_key, expected["layerx_public_key"]
                )
                self.assertEqual(derived.did, expected["did"])
                self.assertNotIn(derived.layerx_seed.hex(), repr(derived))
                bind = expected["bind"]
                plan = plan_layerx_bind(
                    derived, bind["chain_id"], LayerXBindState(None, bind["nonce"])
                )
                self.assertEqual(plan.action, "bind")
                assert plan.call is not None
                self.assertEqual(plan.call.data, "0x" + bind["calldata"])
                self.assertEqual(plan.call.to, bind["to"])
                self.assertEqual(
                    bind_transaction_request(derived, plan.call),
                    {
                        "from": derived.evm_address,
                        "to": bind["to"],
                        "data": "0x" + bind["calldata"],
                        "value": "0x0",
                    },
                )
                already = plan_layerx_bind(
                    derived,
                    bind["chain_id"],
                    LayerXBindState(derived.layerx_public_key.upper(), 1),
                )
                self.assertEqual(already.action, "already_bound")
                self.assertIsNone(already.call)
                self.refuses(
                    "bound_to_different_did",
                    lambda: plan_layerx_bind(
                        derived, bind["chain_id"], LayerXBindState("aa" * 32, 1)
                    ),
                )
                seen += 1
        self.assertEqual(seen, 4)
        self.assertEqual(
            derive_from_mnemonic(ABANDON).evm_address,
            "0x9858EfFD232B4033E47d90003D41EC34EcaEda94",
        )

    def test_bad_phrases_and_indices_are_refused(self) -> None:
        self.refuses(
            "invalid_mnemonic", lambda: derive_from_mnemonic("abandon abandon abandon")
        )
        self.refuses(
            "invalid_mnemonic", lambda: derive_from_mnemonic(" ".join(["abandon"] * 12))
        )
        self.refuses(
            "index_out_of_range", lambda: derive_from_mnemonic(ABANDON, "", 0x80000000)
        )

    def test_wallet_signature_vectors(self) -> None:
        wallet = FIXTURE["wallet_signature"]
        chain_id, address = FIXTURE["chain_id"], wallet["evm_address"]
        for vector in wallet["vectors"]:
            index = vector["index"]
            self.assertEqual(
                key_derivation_typed_data(chain_id, address, index),
                vector["typed_data"],
            )
            self.assertEqual(
                key_derivation_hash(chain_id, address, index).hex(),
                vector["eip712_hash"],
            )
            derived = derive_from_wallet_signature(
                chain_id, address, "0x" + vector["signature"], index
            )
            self.assertEqual(derived.did, vector["did"])
            self.assertEqual(derived.evm_address, address)
            self.assertIsNone(derived.evm_private_key)
            for equivalent in vector["equivalent_signatures"]:
                self.assertNotEqual(equivalent, vector["signature"])
                self.assertEqual(
                    normalize_wallet_signature(equivalent).hex(), vector["signature"]
                )
                self.assertEqual(
                    derive_from_wallet_signature(
                        chain_id, address, bytes.fromhex(equivalent), index
                    ).did,
                    vector["did"],
                )
            self.refuses(
                "wallet_signer_mismatch",
                lambda: derive_from_wallet_signature(
                    chain_id + 1, address, vector["signature"], index
                ),
            )
            self.refuses(
                "invalid_wallet_signature",
                lambda: derive_from_wallet_signature(
                    chain_id, address, vector["signature"][:128] + "1d", index
                ),
            )
            self.refuses(
                "invalid_origin",
                lambda: key_derivation_typed_data(
                    chain_id, address, index, 'https://Evil.example/"'
                ),
            )

    def test_precompile_reads(self) -> None:
        address = "0x" + "11" * 20
        self.assertEqual(bind_nonce_call(address), "0xcedd9ba2" + "00" * 12 + "11" * 20)
        self.assertTrue(bound_did_call(address).startswith("0x357feed6"))
        self.assertEqual(decode_bind_nonce("0x" + "00" * 31 + "07"), 7)
        self.assertIsNone(decode_bound_did("0x" + "00" * 128))
        self.assertEqual(
            decode_bound_did("0x" + "00" * 64 + "ab" * 32 + "00" * 32), "ab" * 32
        )
        self.refuses("malformed_precompile_answer", lambda: decode_bind_nonce("0x01"))
        self.refuses(
            "malformed_precompile_answer", lambda: decode_bound_did("0x" + "00" * 64)
        )


class BindTransportTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        binary = os.environ.get("ANVIL_BIN") or shutil.which("anvil")
        if binary is None:
            candidate = Path.home() / ".foundry" / "bin" / "anvil"
            if candidate.is_file():
                binary = str(candidate)
        if binary is None:
            raise RuntimeError("bind transport tests require anvil or ANVIL_BIN")
        output = tempfile.TemporaryFile()
        cls.addClassCleanup(output.close)
        process = subprocess.Popen(
            [
                binary,
                "--port",
                "0",
                "--chain-id",
                "125",
                "--mnemonic",
                ABANDON,
                "--no-mining",
            ],
            stdout=output,
            stderr=subprocess.STDOUT,
        )
        cls.process = process
        cls.addClassCleanup(cls.stop_node)
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            output.seek(0)
            listening = re.search(rb"Listening on [^\r\n]+:(\d+)\r?\n", output.read())
            if listening is not None:
                cls.endpoint = f"http://localhost:{int(listening[1])}/"
                cls.request_id = 0
                if cls.request("eth_chainId", []) != "0x7d":
                    raise RuntimeError("bind transport test chain id mismatch")
                return
            if process.poll() is not None:
                raise RuntimeError("bind transport node exited before readiness")
            time.sleep(0.05)
        raise RuntimeError("bind transport node did not become ready")

    @classmethod
    def request(cls, method: str, params: list[object]) -> object:
        cls.request_id += 1
        request_id = cls.request_id
        request = Request(
            cls.endpoint,
            data=json.dumps(
                {"jsonrpc": "2.0", "id": request_id, "method": method, "params": params}
            ).encode(),
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urlopen(request, timeout=10) as response:
            body = json.loads(response.read())
        return _decode_rpc_response(body, request_id)

    @classmethod
    def stop_node(cls) -> None:
        if cls.process.poll() is None:
            cls.process.terminate()
            try:
                cls.process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                cls.process.kill()
                cls.process.wait(timeout=10)

    def setUp(self) -> None:
        self.derived = derive_from_seed(
            bytes.fromhex(FIXTURE["mnemonic_vectors"][0]["seed"])
        )
        self.call = plan_layerx_bind(self.derived, 125, LayerXBindState(None, 0)).call
        self.assertIsNotNone(self.call)

    def assert_submitted(self, tx_hash: str, nonce: int) -> dict:
        transaction = self.request("eth_getTransactionByHash", [tx_hash])
        self.assertIsInstance(transaction, dict)
        self.assertEqual(transaction["hash"].lower(), tx_hash.lower())
        self.assertEqual(transaction["type"], "0x2")
        self.assertEqual(transaction["chainId"], "0x7d")
        self.assertEqual(transaction["from"].lower(), self.derived.evm_address.lower())
        self.assertEqual(transaction["to"].lower(), ADDR_PRECOMPILE_ADDRESS)
        self.assertEqual(transaction["input"], self.call.data)
        self.assertEqual(transaction["value"], "0x0")
        self.assertEqual(int(transaction["nonce"], 16), nonce)
        self.assertIsNone(transaction["blockHash"])
        self.assertIsNone(self.request("eth_getTransactionReceipt", [tx_hash]))
        raw = self.request("eth_getRawTransactionByHash", [tx_hash])
        self.assertTrue(raw.startswith("0x02"))
        self.assertEqual(
            "0x" + keccak256(bytes.fromhex(raw[2:])).hex(), tx_hash.lower()
        )
        self.assertIn(int(transaction["v"], 16), (0, 1))
        self.assertGreater(int(transaction["r"], 16), 0)
        self.assertGreater(int(transaction["s"], 16), 0)
        self.assertLessEqual(
            int(transaction["s"], 16),
            0x7FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF5D576E7357A4501DDFE92F46681B20A0,
        )
        return transaction

    def test_fixed_seed_bind_transaction_submits_raw_with_explicit_fees(self) -> None:
        nonce = int(
            self.request(
                "eth_getTransactionCount", [self.derived.evm_address, "pending"]
            ),
            16,
        )
        fees = BindFees(nonce, 1000000000, 2000000000, 200000)
        tx_hash = send_bind_transaction(
            self.request, self.derived, 125, self.call, fees
        )
        transaction = self.assert_submitted(tx_hash, nonce)
        self.assertEqual(int(transaction["gas"], 16), fees.gas_limit)
        self.assertEqual(
            int(transaction["maxPriorityFeePerGas"], 16), fees.max_priority_fee_per_gas
        )
        self.assertEqual(int(transaction["maxFeePerGas"], 16), fees.max_fee_per_gas)

    def test_bind_transaction_reads_pending_fees_and_nonce_then_submits_raw(
        self,
    ) -> None:
        nonce = int(
            self.request(
                "eth_getTransactionCount", [self.derived.evm_address, "pending"]
            ),
            16,
        )
        price = int(self.request("eth_gasPrice", []), 16)
        gas = int(
            self.request(
                "eth_estimateGas",
                [bind_transaction_request(self.derived, self.call), "pending"],
            ),
            16,
        )
        tx_hash = send_bind_transaction(self.request, self.derived, 125, self.call)
        transaction = self.assert_submitted(tx_hash, nonce)
        self.assertEqual(int(transaction["gas"], 16), gas)
        self.assertEqual(int(transaction["maxPriorityFeePerGas"], 16), price)
        self.assertEqual(int(transaction["maxFeePerGas"], 16), price * 2)

    def test_bind_refuses_wrong_chain_and_wallet_only_keys(self) -> None:
        for chain_id in (True, -1, 126, 713714):
            with self.subTest(chain_id=chain_id):
                with self.assertRaises(AccountDerivationError) as raised:
                    send_bind_transaction(
                        self.request, self.derived, chain_id, self.call
                    )
                self.assertEqual(raised.exception.code, "invalid_chain_id")
        with self.assertRaises(AccountDerivationError) as raised:
            send_bind_transaction(
                self.request,
                replace(self.derived, evm_private_key=None),
                125,
                self.call,
            )
        self.assertEqual(raised.exception.code, "evm_key_unavailable")

    def test_bind_rpc_preserves_nonrevert_refusals(self) -> None:
        with self.assertRaises(PaymentRpcError) as raised:
            _bind_rpc(self.request, "eth_methodNotPresent", [])
        self.assertEqual(raised.exception.code, -32601)
        self.assertIsNone(raised.exception.data)

    def test_bind_rpc_transport_loss_keeps_unknown_submission_outcome(self) -> None:
        with socket.socket() as reserved:
            reserved.bind(("localhost", 0))
            rpc = PaymentRpc(f"http://localhost:{reserved.getsockname()[1]}/rpc")
            for method, code, retry in (
                (
                    "eth_sendRawTransaction",
                    SdkErrorCode.UNKNOWN_OUTCOME,
                    "unknown-outcome",
                ),
                ("eth_chainId", SdkErrorCode.TRANSPORT_FAILURE, "safe"),
            ):
                with self.subTest(method=method):
                    with self.assertRaises(PlatformSdkError) as raised:
                        _bind_rpc(rpc.call, method, [])
                    self.assertEqual(
                        (raised.exception.code, raised.exception.retry), (code, retry)
                    )
                    self.assertIsInstance(raised.exception.__cause__, URLError)


if __name__ == "__main__":
    unittest.main()
