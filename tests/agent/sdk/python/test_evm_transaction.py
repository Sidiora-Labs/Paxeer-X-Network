from __future__ import annotations

import sys
import unittest
from collections.abc import Callable
from pathlib import Path

from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.asymmetric import ec, utils
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat

ROOT = Path(__file__).resolve().parents[4]
sys.path.insert(0, str(ROOT / "services/agent/sdk/python"))
sys.path.insert(0, str(Path(__file__).resolve().parent))

from layerx_sdk.account_derivation import _N, _recover, keccak256  # noqa: E402
from layerx_sdk.ai_market import (  # noqa: E402
    _MAX_TRANSACTION_BYTES,
    ENTRYPOINT,
    GUEST_ABI,
    PROGRAMS_PRECOMPILE,
    NativeTerms,
    _attach_signature,
    _encode_programs_call,
    _unsigned_activity,
)
from layerx_sdk.evm_transaction import (  # noqa: E402
    MAX_TRANSACTION_BYTES,
    EvmTransactionError,
    Type2Transaction,
    _rlp,
    activity_id,
    decode_signed,
    encode_call,
)
from layerx_sdk.native_program_call import NativeProgramCall, encode_native_program_call  # noqa: E402
from test_ai_market import B1, RESOURCES, claim_envelope, fixture_call, fund_envelope  # noqa: E402

KEY = ec.derive_private_key(0x4C0883A69102937D6231471B5DBB6204FE5129617082792AE468D01A3F362318, ec.SECP256K1())
NUMBERS = KEY.public_key().public_numbers()
PUBLIC_XY = NUMBERS.x.to_bytes(32, "big") + NUMBERS.y.to_bytes(32, "big")
COMPRESSED = KEY.public_key().public_bytes(Encoding.X962, PublicFormat.CompressedPoint)
ADDRESS = "0x2c7536E3605D9C16a7a3D7b1898e529396a65c23"
OTHER = "0x" + "11" * 20
FEE_LIMIT = 1_000_000
GAS_LIMIT = 2_000_000
MAX_FEE = 3_000_000_000
PRIORITY_FEE = 1_000_000_000
CALL_TYPES = ["bytes32", "uint16", "string", "bytes", "bytes", "bytes", "uint32", "uint64[7]", "uint128"]
DRAW_TYPES = ["bytes32", "bytes32", "uint128", "bytes32", "uint64", "bytes32", "bytes32"]
PAY_EXACT_TYPES = ["bytes32", "bytes32", "bytes32", "uint128", "uint64", "bytes32"]


def sign(digest: bytes) -> bytes:
    r, s = utils.decode_dss_signature(KEY.sign(digest, ec.ECDSA(utils.Prehashed(hashes.SHA256()))))
    s = min(s, _N - s)
    for parity in (0, 1):
        if _recover(digest, r, s, parity) == PUBLIC_XY:
            return r.to_bytes(32, "big") + s.to_bytes(32, "big") + bytes([27 + parity])
    raise AssertionError("signature does not recover")


def signed_raw(transaction: Type2Transaction) -> bytes:
    return transaction.attach_signature(sign(transaction.signing_digest()), ADDRESS)


def word(value: int) -> bytes:
    return value.to_bytes(32, "big")


def plain_transaction(**changes: object) -> Type2Transaction:
    fields: dict[str, object] = {
        "chain_id": 125,
        "nonce": 9,
        "max_priority_fee_per_gas": PRIORITY_FEE,
        "max_fee_per_gas": MAX_FEE,
        "gas_limit": 50_000,
        "to": bytes.fromhex("101a".rjust(40, "0")),
        "value": 0,
        "data": b"\x2e\x89\x49\x0f",
    }
    fields.update(changes)
    return Type2Transaction(**fields)  # type: ignore[arg-type]


class EvmTransactionCase(unittest.TestCase):
    def assertRefused(self, code: str, call: Callable[..., object], *args: object) -> None:
        with self.assertRaises(EvmTransactionError) as caught:
            call(*args)
        self.assertEqual(caught.exception.code, code)


class AiMarketVectorTests(EvmTransactionCase):
    def test_codec_round_trips_the_ai_market_transaction_vectors_byte_for_byte(self) -> None:
        self.assertEqual(MAX_TRANSACTION_BYTES, _MAX_TRANSACTION_BYTES)
        fixture = fixture_call()
        for name, envelope in (("claim", claim_envelope()), ("fund", fund_envelope())):
            with self.subTest(vector=name):
                call = NativeProgramCall(
                    B1.program,
                    GUEST_ABI,
                    ENTRYPOINT,
                    envelope.encode(),
                    fixture.capabilities,
                    fixture.access_declaration,
                    16,
                    RESOURCES,
                )
                terms = NativeTerms(
                    125,
                    b"did:lxp:ai-market-owner",
                    COMPRESSED,
                    1,
                    envelope.request,
                    0,
                    envelope.expiry,
                    FEE_LIMIT,
                    fixture.capabilities,
                    fixture.access_declaration,
                    16,
                    RESOURCES,
                    GAS_LIMIT,
                    MAX_FEE,
                    PRIORITY_FEE,
                )
                unsigned = _unsigned_activity(terms, encode_native_program_call(call))
                transaction = Type2Transaction.decode(unsigned)
                self.assertEqual(
                    transaction,
                    Type2Transaction(
                        125,
                        1,
                        PRIORITY_FEE,
                        MAX_FEE,
                        GAS_LIMIT,
                        PROGRAMS_PRECOMPILE,
                        0,
                        _encode_programs_call(call, FEE_LIMIT),
                    ),
                )
                self.assertEqual(transaction.encode(), unsigned)
                self.assertEqual(transaction.signing_digest(), keccak256(unsigned))
                signature = sign(transaction.signing_digest())
                self.assertEqual(transaction.recover_signer(signature), ADDRESS)
                raw = transaction.attach_signature(signature, ADDRESS)
                self.assertEqual(raw, _attach_signature(unsigned, signature))
                self.assertEqual(transaction.attach_signature(signature, ADDRESS.lower()), raw)
                self.assertEqual(decode_signed(raw), (transaction, signature))
                self.assertEqual(activity_id(raw), keccak256(raw))

    def test_programs_call_calldata_matches_the_ai_market_encoder(self) -> None:
        fixture = fixture_call()
        envelope = claim_envelope()
        call = NativeProgramCall(
            B1.program,
            GUEST_ABI,
            ENTRYPOINT,
            envelope.encode(),
            fixture.capabilities,
            fixture.access_declaration,
            16,
            RESOURCES,
        )
        data = encode_call(
            "call",
            CALL_TYPES,
            [
                call.program_id,
                call.guest_abi,
                call.entrypoint,
                call.calldata,
                call.capabilities,
                call.access_declaration,
                call.response_capacity,
                list(call.resources),
                FEE_LIMIT,
            ],
        )
        self.assertEqual(data[:4].hex(), "54bd2b48")
        self.assertEqual(data, _encode_programs_call(call, FEE_LIMIT))


class SignatureTests(EvmTransactionCase):
    def setUp(self) -> None:
        self.transaction = plain_transaction()
        self.signature = sign(self.transaction.signing_digest())
        self.raw = self.transaction.attach_signature(self.signature, ADDRESS)

    def test_a_signature_with_high_s_or_v_0_is_refused(self) -> None:
        r, s, v = self.signature[:32], int.from_bytes(self.signature[32:64], "big"), self.signature[64]
        high_s = r + word(_N - s) + bytes([55 - v])
        self.assertEqual(
            _recover(self.transaction.signing_digest(), int.from_bytes(r, "big"), _N - s, 28 - v), PUBLIC_XY
        )
        for name, signature in (
            ("high s", high_s),
            ("v 0", self.signature[:64] + b"\x00"),
            ("v 1", self.signature[:64] + b"\x01"),
            ("short", self.signature[:64]),
            ("zero r", bytes(32) + self.signature[32:]),
            ("text", self.signature.hex()),
        ):
            with self.subTest(signature=name):
                self.assertRefused("invalid_signature", self.transaction.attach_signature, signature, ADDRESS)
                self.assertRefused("invalid_signature", self.transaction.recover_signer, signature)

    def test_a_signer_other_than_the_expected_address_is_refused(self) -> None:
        self.assertRefused("signer_mismatch", self.transaction.attach_signature, self.signature, OTHER)
        other = plain_transaction(nonce=10)
        self.assertRefused("signer_mismatch", other.attach_signature, self.signature, ADDRESS)
        self.assertRefused("invalid_address", self.transaction.attach_signature, self.signature, "0x1234")

    def test_signed_bytes_with_a_non_canonical_signature_are_refused(self) -> None:
        fields = self.transaction._fields()
        r, s = self.signature[:32], int.from_bytes(self.signature[32:64], "big")
        parity = self.signature[64] - 27
        self.assertEqual(
            self.raw, b"\x02" + _rlp(fields + (b"\x01" if parity else b"", r.lstrip(b"\x00"), word(s).lstrip(b"\x00")))
        )
        for name, tail in (
            ("high s", (b"" if parity else b"\x01", r.lstrip(b"\x00"), word(_N - s).lstrip(b"\x00"))),
            ("parity 2", (b"\x02", r.lstrip(b"\x00"), word(s).lstrip(b"\x00"))),
            ("parity 27", (b"\x1b", r.lstrip(b"\x00"), word(s).lstrip(b"\x00"))),
            ("padded r", (b"\x01" if parity else b"", b"\x00" + r.lstrip(b"\x00"), word(s).lstrip(b"\x00"))),
            ("zero s", (b"\x01" if parity else b"", r.lstrip(b"\x00"), b"")),
            ("wide s", (b"\x01" if parity else b"", r.lstrip(b"\x00"), b"\x01" + word(s))),
        ):
            with self.subTest(tail=name):
                self.assertRefused("invalid_signature", decode_signed, b"\x02" + _rlp(fields + tail))
                self.assertRefused("invalid_signature", activity_id, b"\x02" + _rlp(fields + tail))

    def test_activity_id_is_keccak256_of_the_signed_raw_bytes(self) -> None:
        self.assertEqual(activity_id(self.raw), keccak256(self.raw))
        self.assertEqual(len(activity_id(self.raw)), 32)
        self.assertRefused("invalid_transaction", activity_id, self.transaction.encode())
        self.assertRefused("invalid_rlp", activity_id, self.raw + b"\x00")


class CodecTests(EvmTransactionCase):
    def test_unsigned_encoding_is_the_canonical_type_2_rlp(self) -> None:
        transaction = plain_transaction(value=0x0100)
        expected = b"\x02" + _rlp(
            (
                b"\x7d",
                b"\x09",
                word(PRIORITY_FEE).lstrip(b"\x00"),
                word(MAX_FEE).lstrip(b"\x00"),
                b"\xc3\x50",
                transaction.to,
                b"\x01\x00",
                b"\x2e\x89\x49\x0f",
                (),
            )
        )
        self.assertEqual(transaction.encode(), expected)
        self.assertEqual(expected[:3].hex(), "02ed7d")
        self.assertEqual(Type2Transaction.decode(expected), transaction)
        zero = plain_transaction(nonce=0, max_priority_fee_per_gas=0, data=b"")
        self.assertEqual(Type2Transaction.decode(zero.encode()), zero)
        self.assertEqual(zero.encode()[3], 0x80)

    def test_decode_refuses_non_canonical_and_foreign_encodings(self) -> None:
        fields = plain_transaction()._fields()
        body = _rlp(fields)
        for name, code, raw in (
            ("empty", "invalid_transaction", b""),
            ("type 1", "invalid_transaction", b"\x01" + body),
            ("trailing byte", "invalid_rlp", b"\x02" + body + b"\x00"),
            ("truncated", "invalid_rlp", b"\x02" + body[:-1]),
            ("long form short list", "invalid_rlp", b"\x02\xf8" + bytes([len(body) - 1]) + body[1:]),
            ("eight fields", "invalid_transaction", b"\x02" + _rlp(fields[:8])),
            ("leading zero nonce", "invalid_transaction", b"\x02" + _rlp(fields[:1] + (b"\x00\x09",) + fields[2:])),
            ("wide chain id", "invalid_transaction", b"\x02" + _rlp((b"\x01" * 9,) + fields[1:])),
            ("short to", "invalid_transaction", b"\x02" + _rlp(fields[:5] + (b"\x10" * 19,) + fields[6:])),
            ("list data", "invalid_transaction", b"\x02" + _rlp(fields[:7] + ((),) + fields[8:])),
            ("access list", "invalid_transaction", b"\x02" + _rlp(fields[:8] + ((b"\x01" * 20,),))),
            ("zero chain", "invalid_transaction", b"\x02" + _rlp((b"",) + fields[1:])),
            ("zero gas", "invalid_transaction", b"\x02" + _rlp(fields[:4] + (b"",) + fields[5:])),
            ("tip above cap", "invalid_transaction", b"\x02" + _rlp(fields[:2] + (b"\x01" + fields[3],) + fields[3:])),
            ("signed", "invalid_transaction", signed_raw(plain_transaction())),
        ):
            with self.subTest(raw=name):
                self.assertRefused(code, Type2Transaction.decode, raw)
        self.assertRefused("invalid_transaction", decode_signed, plain_transaction().encode())
        self.assertRefused("invalid_rlp", Type2Transaction.decode, b"\x02" + b"\x00" * (MAX_TRANSACTION_BYTES + 1))

    def test_construction_refuses_out_of_range_fields(self) -> None:
        for name, changes in (
            ("zero chain", {"chain_id": 0}),
            ("wide chain", {"chain_id": 1 << 64}),
            ("bool nonce", {"nonce": True}),
            ("negative value", {"value": -1}),
            ("zero gas", {"gas_limit": 0}),
            ("tip above cap", {"max_priority_fee_per_gas": MAX_FEE + 1}),
            ("text to", {"to": "0x" + "10" * 20}),
            ("short to", {"to": b"\x10" * 19}),
            ("text data", {"data": "2e89490f"}),
            ("oversized data", {"data": bytes(MAX_TRANSACTION_BYTES)}),
        ):
            with self.subTest(field=name):
                self.assertRefused("invalid_transaction", lambda: plain_transaction(**changes).encode())


class EncodeCallTests(EvmTransactionCase):
    def test_draw_selector_is_produced_for_the_ledger_draw_signature(self) -> None:
        data = encode_call(
            "draw", DRAW_TYPES, [b"\x01" * 32, b"\x02" * 32, 1_000, b"\x03" * 32, 7, b"\x04" * 32, b"\x05" * 32]
        )
        self.assertEqual(data[:4].hex(), "2e89490f")
        self.assertEqual(
            data,
            bytes.fromhex("2e89490f")
            + b"\x01" * 32
            + b"\x02" * 32
            + word(1_000)
            + b"\x03" * 32
            + word(7)
            + b"\x04" * 32
            + b"\x05" * 32,
        )

    def test_pay_exact_selector_is_the_keccak_prefix_of_its_contract_signature(self) -> None:
        data = encode_call(
            "payExact",
            PAY_EXACT_TYPES,
            [b"\x01" * 32, b"\x02" * 32, b"\x03" * 32, (1 << 128) - 1, (1 << 64) - 1, b"\x04" * 32],
        )
        prefix = keccak256(b"payExact(bytes32,bytes32,bytes32,uint128,uint64,bytes32)")[:4]
        self.assertEqual(data[:4], prefix)
        self.assertEqual(prefix.hex(), "78ea11db")
        self.assertEqual(
            data[4:],
            b"\x01" * 32 + b"\x02" * 32 + b"\x03" * 32 + word((1 << 128) - 1) + word((1 << 64) - 1) + b"\x04" * 32,
        )

    def test_dynamic_bytes_and_tuple_arrays_follow_the_abi_layout(self) -> None:
        payer_grant = b"\x09" * 33
        pull = encode_call(
            "budgetPull", ["bytes32", "bytes32", "uint128", "bytes"], [b"\xaa" * 32, b"\xbb" * 32, 5, payer_grant]
        )
        self.assertEqual(
            pull,
            bytes.fromhex("bc2548e7")
            + b"\xaa" * 32
            + b"\xbb" * 32
            + word(5)
            + word(128)
            + word(33)
            + payer_grant
            + bytes(31),
        )
        legs = [(b"\x01" * 32, b"\x02" * 32, b"\x03" * 32, 6), (b"\x04" * 32, b"\x05" * 32, b"\x06" * 32, 7)]
        transfer = encode_call("transfer", ["bytes32", "(bytes32,bytes32,bytes32,uint128)[]"], [b"\xcc" * 32, legs])
        self.assertEqual(
            transfer,
            bytes.fromhex("59849fe7")
            + b"\xcc" * 32
            + word(64)
            + word(2)
            + b"\x01" * 32
            + b"\x02" * 32
            + b"\x03" * 32
            + word(6)
            + b"\x04" * 32
            + b"\x05" * 32
            + b"\x06" * 32
            + word(7),
        )
        dispute = encode_call(
            "serviceDisputeOpen", ["bytes32", "bytes32", "bytes32[]"], [b"\x01" * 32, b"\x02" * 32, [b"\x03" * 32]]
        )
        self.assertEqual(
            dispute,
            keccak256(b"serviceDisputeOpen(bytes32,bytes32,bytes32[])")[:4]
            + b"\x01" * 32
            + b"\x02" * 32
            + word(96)
            + word(1)
            + b"\x03" * 32,
        )
        nested = encode_call(
            "probe", ["(string,bool)[]", "address", "bytes4"], [[("ab", True)], b"\x22" * 20, b"\x01\x02\x03\x04"]
        )
        self.assertEqual(
            nested,
            keccak256(b"probe((string,bool)[],address,bytes4)")[:4]
            + word(96)
            + bytes(12)
            + b"\x22" * 20
            + b"\x01\x02\x03\x04"
            + bytes(28)
            + word(1)
            + word(32)
            + word(64)
            + word(1)
            + word(2)
            + b"ab"
            + bytes(30),
        )

    def test_unknown_types_and_mismatched_values_are_refused(self) -> None:
        values = [b"\x01" * 32, b"\x02" * 32, 1, b"\x03" * 32, 7, b"\x04" * 32, b"\x05" * 32]
        for name, kind in (
            ("uint", "uint"),
            ("uint7", "uint7"),
            ("uint264", "uint264"),
            ("bytes33", "bytes33"),
            ("bytes0", "bytes0"),
            ("int256", "int256"),
            ("zero array", "uint64[0]"),
            ("padded array", "uint64[07]"),
            ("open tuple", "(bytes32"),
            ("empty tuple", "()"),
            ("bare array", "[]"),
        ):
            with self.subTest(type=name):
                self.assertRefused("invalid_abi_type", encode_call, "draw", [kind], [values[0]])
        self.assertRefused("invalid_abi_type", encode_call, "1draw", DRAW_TYPES, values)
        self.assertRefused("invalid_abi_type", encode_call, "draw(", DRAW_TYPES, values)
        for name, index, value in (
            ("short bytes32", 0, b"\x01" * 31),
            ("text bytes32", 0, "0x" + "01" * 32),
            ("wide uint128", 2, 1 << 128),
            ("negative uint128", 2, -1),
            ("bool uint64", 4, True),
        ):
            with self.subTest(value=name):
                self.assertRefused(
                    "invalid_abi_value", encode_call, "draw", DRAW_TYPES, values[:index] + [value] + values[index + 1 :]
                )
        self.assertRefused("invalid_abi_value", encode_call, "draw", DRAW_TYPES, values[:6])
        self.assertRefused("invalid_abi_value", encode_call, "draw", DRAW_TYPES, b"".join(values[:2]))
        self.assertRefused("invalid_abi_value", encode_call, "quoteCallFee", ["uint64[7]"], [list(RESOURCES[:6])])
        self.assertRefused("invalid_abi_value", encode_call, "retireStorage", ["bytes"], ["00"])
        self.assertRefused("invalid_abi_value", encode_call, "probe", ["string"], [b"ab"])
        self.assertRefused("invalid_abi_value", encode_call, "probe", ["string"], ["\ud800"])
        self.assertRefused("invalid_abi_value", encode_call, "probe", ["bool"], [1])
        self.assertRefused("invalid_abi_value", encode_call, "probe", ["address"], ["0x" + "22" * 20])
        self.assertEqual(
            encode_call("quoteCallFee", ["uint64[7]"], [list(RESOURCES)]),
            bytes.fromhex("d67f9ffa") + b"".join(word(value) for value in RESOURCES),
        )


if __name__ == "__main__":
    unittest.main()
