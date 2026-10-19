from __future__ import annotations

import shutil
import sys
import tempfile
import unittest
from pathlib import Path

from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.asymmetric import ec, utils
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey, Ed25519PublicKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat

ROOT = Path(__file__).resolve().parents[4]
sys.path.insert(0, str(ROOT / "services/agent/sdk/python"))

from layerx_sdk.account_derivation import (  # noqa: E402
    _SELECTOR_BIND_LAYERX,
    ADDR_PRECOMPILE_ADDRESS,
    AccountDerivationError,
    auto_bind_layerx,
    bind_nonce_call,
    bound_did_call,
    derive_from_seed,
    keccak256,
    layerx_bind_message,
    layerx_did_call,
    normalize_wallet_signature,
    read_layerx_bind_state,
    _recover,
)
from layerx_sdk.ai_market import (  # noqa: E402
    FUND,
    FUNDING_POLICY_VERSION,
    AiMarketError,
    Availability,
    Freshness,
    FreshnessLabel,
    MarketSnapshot,
    PROGRAMS_PRECOMPILE,
    NativeTerms,
    OperationJournal,
    OperationRequest,
    ProjectionState,
    SdkState,
    SnapshotBinding,
    _rlp,
    _rlp_decode,
    _signature_public_key,
)

CHAIN_ID = 125
GAS_PRICE = 1_000_000_000
BIND_GAS = 100_000
RESOURCES = (1_000_000, 16_777_216, 1_048_576, 1_048_576, 64, 1_048_576, 4096)
BINDING = SnapshotBinding(
    b"\x01" * 32, b"\x02" * 32, b"\x03" * 32, 1000, 500, b"\x04" * 32, b"\x05" * 32, 7, b"\x06" * 32, 3, 2,
    b"\x07" * 32, b"\x08" * 32, b"\x09" * 32, None, 4, 1_700_000_000_000)


def _did(seed_byte: int) -> str:
    key = Ed25519PrivateKey.from_private_bytes(bytes(range(seed_byte, seed_byte + 32)))
    return key.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw).hex()


OWNER_DID = _did(0x60)
FOREIGN_DID = _did(0x40)


def _address(public: bytes) -> str:
    point = ec.EllipticCurvePublicKey.from_encoded_point(ec.SECP256K1(), public)
    return "0x" + keccak256(point.public_bytes(Encoding.X962, PublicFormat.UncompressedPoint)[1:])[12:].hex()


def _decode_raw(raw: str) -> tuple[tuple, str]:
    """Decodes a signed EIP-1559 transaction and recovers its sender address."""
    assert raw[:2] == "0x"
    encoded = bytes.fromhex(raw[2:])
    assert encoded[:1] == b"\x02"
    fields = _rlp_decode(encoded[1:])
    assert len(fields) == 12
    parity, r, s = (int.from_bytes(field, "big") for field in fields[9:])
    public = _recover(keccak256(b"\x02" + _rlp(fields[:9])), r, s, parity)
    return fields, "0x" + keccak256(public)[12:].hex()


class AddrChain:
    """An EIP-1193 request endpoint holding ``addr`` precompile bindings; every method is logged."""

    def __init__(self) -> None:
        self.methods: list[str] = []
        self.bound: dict[str, str] = {}
        self.nonces: dict[str, int] = {}
        self.evm_nonces: dict[str, int] = {}
        self.raw_transactions: list[str] = []

    def bind(self, address: str, did: str) -> None:
        self.bound[address.lower()] = did
        self.nonces[address.lower()] = 1

    def request(self, method: str, params: list[object]) -> object:
        return self(method, params)

    def __call__(self, method: str, params: list[object]) -> object:
        self.methods.append(method)
        if method == "eth_chainId":
            assert params == []
            return hex(CHAIN_ID)
        if method == "eth_getTransactionCount":
            assert len(params) == 2 and params[1] == "pending"
            return hex(self.evm_nonces.get(str(params[0]).lower(), 0))
        if method == "eth_gasPrice":
            assert params == []
            return hex(GAS_PRICE)
        if method == "eth_estimateGas":
            assert len(params) == 2 and params[1] == "pending"
            transaction = params[0]
            assert isinstance(transaction, dict) and transaction["to"] == ADDR_PRECOMPILE_ADDRESS
            assert transaction["value"] == "0x0" and str(transaction["data"])[2:10] == _SELECTOR_BIND_LAYERX
            assert str(transaction["from"]).lower() not in self.bound
            return hex(BIND_GAS)
        if method == "eth_sendRawTransaction":
            assert len(params) == 1
            raw = str(params[0])
            fields, sender = _decode_raw(raw)
            assert int.from_bytes(fields[0], "big") == CHAIN_ID
            assert int.from_bytes(fields[1], "big") == self.evm_nonces.get(sender, 0)
            data = fields[7]
            if fields[5] == bytes.fromhex(ADDR_PRECOMPILE_ADDRESS[2:]):
                assert sender not in self.bound and fields[6] == b""
                assert data[:4].hex() == _SELECTOR_BIND_LAYERX
                nonce = self.nonces.get(sender, 0)
                Ed25519PublicKey.from_public_bytes(data[4:36]).verify(
                    data[-64:], layerx_bind_message(CHAIN_ID, sender, nonce))
                self.bound[sender] = data[4:36].hex()
                self.nonces[sender] = nonce + 1
            else:
                assert fields[5] == PROGRAMS_PRECOMPILE and sender in self.bound
            self.evm_nonces[sender] = self.evm_nonces.get(sender, 0) + 1
            self.raw_transactions.append(raw)
            return "0x" + keccak256(bytes.fromhex(raw[2:])).hex()
        if method == "eth_call":
            assert len(params) == 2 and params[1] == "latest"
            call = params[0]
            assert isinstance(call, dict) and call["to"] == ADDR_PRECOMPILE_ADDRESS
            address = "0x" + str(call["data"])[-40:]
            did = self.bound.get(address)
            if call["data"] == bound_did_call(address):
                return "0x" + "00" * 64 + (did or "00" * 32) + "00" * 32
            if call["data"] == bind_nonce_call(address):
                return "0x" + self.nonces.get(address, 0).to_bytes(32, "big").hex()
            if call["data"] == layerx_did_call(address) and did is not None:
                text = f"did:layerx:{did}".encode()
                return ("0x" + did + (64).to_bytes(32, "big").hex() + len(text).to_bytes(32, "big").hex()
                        + text.ljust(96, b"\0").hex())
            raise AssertionError(f"unexpected addr read {call['data']}")
        raise AssertionError(f"unexpected method {method}")


class CountingSigner:
    def __init__(self, secret: int) -> None:
        self._key = ec.derive_private_key(secret, ec.SECP256K1())
        self.calls = 0

    def public_key(self) -> bytes:
        return self._key.public_key().public_bytes(Encoding.X962, PublicFormat.CompressedPoint)

    def sign(self, digest: bytes) -> bytes:
        self.calls += 1
        r, s = utils.decode_dss_signature(self._key.sign(digest, ec.ECDSA(utils.Prehashed(hashes.SHA256()))))
        for parity in (27, 28):
            signature = normalize_wallet_signature(r.to_bytes(32, "big") + s.to_bytes(32, "big") + bytes([parity]))
            if _signature_public_key(signature, digest) == self.public_key():
                return signature
        raise AssertionError("signature recovery failed")


def approval(owner: bytes, account_sequence: int = 4):
    snapshot = MarketSnapshot(BINDING.snapshot_id(), ProjectionState.FINALIZED_PUBLISHABLE, BINDING,
                              (Availability.AVAILABLE,) * 10, b"\x0a" * 32, Freshness(FreshnessLabel.CURRENT))
    payload = (5).to_bytes(16, "big") + b"\xb0" * 32 + FUNDING_POLICY_VERSION.to_bytes(8, "big") + b"\x01"
    prepared = snapshot.prepare(
        500, OperationRequest(FUND, payload, b"\xc0" * 32, 1, 900, b"\xd0" * 32, True, b"\xa1" * 32),
        NativeTerms(CHAIN_ID, b"did:layerx:market", owner, account_sequence, b"\xd0" * 32, 0, 900, 1000, b"", b"", 4096,
                    RESOURCES, 1_000_000, 67_000_000, 1))
    review = prepared.review()
    return prepared, review.approve(review.terms, owner)


class ApprovalBindingPreflightTests(unittest.TestCase):
    def setUp(self) -> None:
        self.signer = CountingSigner(int.from_bytes(bytes(range(1, 33)), "big"))
        self.address = _address(self.signer.public_key())
        self.chain = AddrChain()

    def assertRefusedBeforeSigning(self, code: str) -> None:
        _, approved = approval(self.signer.public_key())
        with self.assertRaises(AccountDerivationError) as raised:
            approved.sign(self.signer, None, 500, self.chain, OWNER_DID)
        self.assertEqual(raised.exception.code, code)
        self.assertEqual(self.signer.calls, 0)
        self.assertNotIn("eth_getTransactionCount", self.chain.methods)
        self.assertNotIn("eth_sendRawTransaction", self.chain.methods)

    def test_unbound_approving_key_is_refused_before_signing(self) -> None:
        self.assertRefusedBeforeSigning("address_unbound")
        self.assertEqual(self.chain.methods, ["eth_call", "eth_call"])

    def test_approving_key_bound_to_a_different_did_is_refused_before_signing(self) -> None:
        self.chain.bind(self.address, FOREIGN_DID)
        self.assertRefusedBeforeSigning("bound_to_different_did")
        self.assertEqual(self.chain.methods, ["eth_call", "eth_call", "eth_call"])

    def test_stale_authority_is_refused_before_the_binding_read(self) -> None:
        _, approved = approval(self.signer.public_key())
        with self.assertRaises(AiMarketError) as raised:
            approved.sign(self.signer, None, 509, self.chain, OWNER_DID)
        self.assertEqual((raised.exception.code, raised.exception.detail), ("StaleAuthority", 9))
        self.assertEqual((self.chain.methods, self.signer.calls), ([], 0))

    def test_bound_approving_key_signs_once(self) -> None:
        self.chain.bind(self.address, OWNER_DID)
        prepared, approved = approval(self.signer.public_key())
        record = approved.sign(self.signer, None, 500, self.chain, OWNER_DID.upper())
        self.assertEqual(self.signer.calls, 1)
        self.assertEqual((record.state, record.attempt, record.intent), (SdkState.SIGNED, 0, prepared.intent))
        self.assertEqual(self.chain.methods, ["eth_call", "eth_call", "eth_call"])

    def test_unbound_account_binds_through_the_bind_transport_then_signs(self) -> None:
        derived = derive_from_seed(bytes.fromhex("000102030405060708090a0b0c0d0e0f"))
        assert derived.evm_private_key is not None
        signer = CountingSigner(int.from_bytes(derived.evm_private_key, "big"))
        address = derived.evm_address.lower()
        self.assertEqual(_address(signer.public_key()), address)
        result = auto_bind_layerx(self.chain, derived, CHAIN_ID)
        self.assertEqual(result["action"], "bind")
        self.assertEqual(self.chain.methods, [
            "eth_call", "eth_call", "eth_chainId", "eth_getTransactionCount", "eth_gasPrice", "eth_estimateGas",
            "eth_sendRawTransaction"])
        self.assertEqual(len(self.chain.raw_transactions), 1)
        raw = self.chain.raw_transactions[0]
        self.assertEqual(result["transaction_hash"], "0x" + keccak256(bytes.fromhex(raw[2:])).hex())
        fields, sender = _decode_raw(raw)
        self.assertEqual(sender, address)
        self.assertEqual(fields[:9], (
            CHAIN_ID.to_bytes(1, "big"), b"", GAS_PRICE.to_bytes(4, "big"), (2 * GAS_PRICE).to_bytes(4, "big"),
            BIND_GAS.to_bytes(3, "big"), bytes.fromhex(ADDR_PRECOMPILE_ADDRESS[2:]), b"",
            bytes.fromhex(result["call"].data[2:]), ()))
        data = fields[7]
        self.assertEqual((data[:4].hex(), data[4:36].hex()), (_SELECTOR_BIND_LAYERX, derived.layerx_public_key))
        Ed25519PublicKey.from_public_bytes(data[4:36]).verify(
            data[-64:], layerx_bind_message(CHAIN_ID, derived.evm_address, 0))
        state = read_layerx_bind_state(self.chain, derived.evm_address)
        self.assertEqual((state.bound_did_public_key, state.nonce), (derived.layerx_public_key, 1))

        prepared, approved = approval(signer.public_key(), self.chain.evm_nonces[address])
        record = approved.sign(signer, None, 500, self.chain, derived.layerx_public_key)
        self.assertEqual(signer.calls, 1)
        self.assertEqual((record.state, record.intent), (SdkState.SIGNED, prepared.intent))
        directory = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, directory)
        journal = OperationJournal(directory / "journal", None)
        journal.record_signed(record)
        self.assertEqual(journal.submit(record, self.chain), SdkState.PENDING)
        self.assertEqual(record.attempt, 1)
        self.assertEqual(self.chain.raw_transactions[1:], ["0x" + record.signed_bytes.hex()])
        self.assertEqual(self.chain.methods[7:], ["eth_call"] * 6 + ["eth_sendRawTransaction"])
        self.assertEqual(self.chain.evm_nonces, {address: 2})

if __name__ == "__main__":
    unittest.main()
