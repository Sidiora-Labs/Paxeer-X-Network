"""EIP-1559 (type 2) transactions for the Paxeer X Network.

Canonical RLP encode and decode, the signing digest, signature attachment with
signer recovery, the activity id of a signed transaction and ABI calldata for
the precompile methods the transactions call.
"""

from __future__ import annotations

import re
from collections.abc import Sequence
from dataclasses import dataclass

from .account_derivation import (
    AccountDerivationError,
    _address_bytes,
    _evm_address,
    _recover,
    keccak256,
    normalize_wallet_signature,
)

TRANSACTION_TYPE = 0x02
MAX_TRANSACTION_BYTES = 2_163_684

_WORD = 32
_NAME = re.compile(r"[A-Za-z_$][A-Za-z0-9_$]*")
_ARRAY_LENGTH = re.compile(r"[1-9][0-9]*")
_SIZED = re.compile(r"(uint|bytes)([1-9][0-9]*)")


class EvmTransactionError(ValueError):
    """A refused transaction, signature or call; ``code`` is stable."""

    def __init__(self, code: str, detail: str | None = None) -> None:
        super().__init__(code if detail is None else f"{code}: {detail}")
        self.code = code


def _rlp(value: bytes | tuple) -> bytes:
    body = b"".join(_rlp(item) for item in value) if isinstance(value, tuple) else value
    if not isinstance(value, tuple) and len(body) == 1 and body[0] < 128:
        return body
    base = 192 if isinstance(value, tuple) else 128
    if len(body) < 56:
        return bytes([base + len(body)]) + body
    length = len(body).to_bytes((len(body).bit_length() + 7) // 8, "big")
    return bytes([base + 55 + len(length)]) + length + body


def _rlp_decode(data: bytes) -> tuple:
    def read(offset: int, limit: int, depth: int) -> tuple[bytes | tuple, int]:
        if offset >= limit or depth > 2:
            raise EvmTransactionError("invalid_rlp")
        prefix = data[offset]
        offset += 1
        if prefix < 128:
            return bytes([prefix]), offset
        is_list = prefix >= 192
        base = 192 if is_list else 128
        width = prefix - base
        if width > 55:
            width -= 55
            if offset + width > limit or data[offset] == 0:
                raise EvmTransactionError("invalid_rlp")
            length = int.from_bytes(data[offset : offset + width], "big")
            offset += width
            if length < 56:
                raise EvmTransactionError("invalid_rlp")
        else:
            length = width
        end = offset + length
        if end > limit:
            raise EvmTransactionError("invalid_rlp")
        if not is_list:
            body = data[offset:end]
            if length == 1 and body[0] < 128:
                raise EvmTransactionError("invalid_rlp")
            return body, end
        items = []
        while offset < end:
            item, offset = read(offset, end, depth + 1)
            items.append(item)
            if len(items) > 12:
                raise EvmTransactionError("invalid_rlp")
        return tuple(items), end

    if type(data) is not bytes or not 0 < len(data) <= MAX_TRANSACTION_BYTES:
        raise EvmTransactionError("invalid_rlp")
    result, consumed = read(0, len(data), 0)
    if not isinstance(result, tuple) or consumed != len(data) or _rlp(result) != data:
        raise EvmTransactionError("invalid_rlp")
    return result


def _rlp_integer(value: int) -> bytes:
    return value.to_bytes((value.bit_length() + 7) // 8, "big")


def _transaction_fields(raw: bytes, signed: bool) -> tuple:
    if type(raw) is not bytes or raw[:1] != bytes([TRANSACTION_TYPE]):
        raise EvmTransactionError("invalid_transaction")
    fields = _rlp_decode(raw[1:])
    if len(fields) != (12 if signed else 9):
        raise EvmTransactionError("invalid_transaction")
    for index, bits in ((0, 64), (1, 64), (2, 256), (3, 256), (4, 64), (6, 256)):
        value = fields[index]
        if type(value) is not bytes or len(value) > bits // 8 or value[:1] == b"\x00":
            raise EvmTransactionError("invalid_transaction")
    if (
        type(fields[5]) is not bytes
        or len(fields[5]) != 20
        or type(fields[7]) is not bytes
        or fields[8] != ()
    ):
        raise EvmTransactionError("invalid_transaction")
    return fields


def _unsigned(value: object, bits: int) -> int:
    if type(value) is not int or not 0 <= value < 1 << bits:
        raise EvmTransactionError("invalid_transaction")
    return value


@dataclass(frozen=True)
class Type2Transaction:
    """An unsigned EIP-1559 call with an empty access list."""

    chain_id: int
    nonce: int
    max_priority_fee_per_gas: int
    max_fee_per_gas: int
    gas_limit: int
    to: bytes
    value: int
    data: bytes

    def __post_init__(self) -> None:
        _unsigned(self.chain_id, 64)
        _unsigned(self.nonce, 64)
        _unsigned(self.max_priority_fee_per_gas, 256)
        _unsigned(self.max_fee_per_gas, 256)
        _unsigned(self.gas_limit, 64)
        _unsigned(self.value, 256)
        if (
            self.chain_id == 0
            or self.gas_limit == 0
            or self.max_priority_fee_per_gas > self.max_fee_per_gas
            or type(self.to) is not bytes
            or len(self.to) != 20
            or type(self.data) is not bytes
        ):
            raise EvmTransactionError("invalid_transaction")

    def _fields(self) -> tuple:
        return (
            _rlp_integer(self.chain_id),
            _rlp_integer(self.nonce),
            _rlp_integer(self.max_priority_fee_per_gas),
            _rlp_integer(self.max_fee_per_gas),
            _rlp_integer(self.gas_limit),
            self.to,
            _rlp_integer(self.value),
            self.data,
            (),
        )

    def encode(self) -> bytes:
        """The unsigned bytes 0x02 || rlp(fields)."""
        raw = bytes([TRANSACTION_TYPE]) + _rlp(self._fields())
        if len(raw) > MAX_TRANSACTION_BYTES:
            raise EvmTransactionError("invalid_transaction")
        return raw

    @classmethod
    def decode(cls, raw: bytes) -> Type2Transaction:
        """Decodes canonical unsigned bytes; any other encoding is refused."""
        return cls._from_fields(_transaction_fields(raw, False))

    @classmethod
    def _from_fields(cls, fields: tuple) -> Type2Transaction:
        return cls(
            *(int.from_bytes(value, "big") for value in fields[:5]),
            fields[5],
            int.from_bytes(fields[6], "big"),
            fields[7],
        )

    def signing_digest(self) -> bytes:
        """keccak256(0x02 || rlp(unsigned fields))."""
        return keccak256(self.encode())

    def recover_signer(self, signature: bytes) -> str:
        """The checksummed address a 65-byte r || s || v signature recovers to.

        v must be 27 or 28 and s must be low; anything else is refused.
        """
        try:
            if (
                type(signature) is not bytes
                or len(signature) != 65
                or signature[64] not in (27, 28)
                or normalize_wallet_signature(signature) != signature
            ):
                raise EvmTransactionError("invalid_signature")
            public = _recover(
                self.signing_digest(),
                int.from_bytes(signature[:32], "big"),
                int.from_bytes(signature[32:64], "big"),
                signature[64] - 27,
            )
        except AccountDerivationError:
            raise EvmTransactionError("invalid_signature") from None
        return _evm_address(public)

    def attach_signature(self, signature: bytes, expected_signer: str) -> bytes:
        """The signed raw bytes, once ``signature`` recovers to ``expected_signer``."""
        try:
            expected = _address_bytes(expected_signer)
        except AccountDerivationError:
            raise EvmTransactionError("invalid_address") from None
        if bytes.fromhex(self.recover_signer(signature)[2:]) != expected:
            raise EvmTransactionError("signer_mismatch")
        raw = bytes([TRANSACTION_TYPE]) + _rlp(
            self._fields()
            + (
                _rlp_integer(signature[64] - 27),
                _rlp_integer(int.from_bytes(signature[:32], "big")),
                _rlp_integer(int.from_bytes(signature[32:64], "big")),
            )
        )
        if len(raw) > MAX_TRANSACTION_BYTES:
            raise EvmTransactionError("invalid_transaction")
        return raw


def decode_signed(raw: bytes) -> tuple[Type2Transaction, bytes]:
    """Decodes canonical signed bytes into the transaction and its r || s || v signature."""
    fields = _transaction_fields(raw, True)
    transaction = Type2Transaction._from_fields(fields[:9])
    for value in fields[9:]:
        if type(value) is not bytes or len(value) > 32 or value[:1] == b"\x00":
            raise EvmTransactionError("invalid_signature")
    if fields[9] not in (b"", b"\x01"):
        raise EvmTransactionError("invalid_signature")
    signature = (
        fields[10].rjust(32, b"\x00")
        + fields[11].rjust(32, b"\x00")
        + bytes([27 + len(fields[9])])
    )
    try:
        if normalize_wallet_signature(signature) != signature:
            raise EvmTransactionError("invalid_signature")
    except AccountDerivationError:
        raise EvmTransactionError("invalid_signature") from None
    return transaction, signature


def activity_id(raw: bytes) -> bytes:
    """keccak256 of the signed raw transaction, which is its activity id."""
    decode_signed(raw)
    return keccak256(raw)


_AbiType = str | tuple


def _split(body: str) -> list[str]:
    parts, depth, start = [], 0, 0
    for index, character in enumerate(body):
        if character in "([":
            depth += 1
        elif character in ")]":
            depth -= 1
            if depth < 0:
                raise EvmTransactionError("invalid_abi_type", body)
        elif character == "," and depth == 0:
            parts.append(body[start:index])
            start = index + 1
    parts.append(body[start:])
    if depth != 0:
        raise EvmTransactionError("invalid_abi_type", body)
    return parts


def _parse(kind: str) -> _AbiType:
    if kind.endswith("]"):
        start = kind.rfind("[")
        length = kind[start + 1 : -1]
        if start <= 0 or (length and _ARRAY_LENGTH.fullmatch(length) is None):
            raise EvmTransactionError("invalid_abi_type", kind)
        return ("array", _parse(kind[:start]), int(length) if length else None)
    if kind.startswith("(") and kind.endswith(")"):
        return ("tuple", tuple(_parse(part) for part in _split(kind[1:-1])))
    if kind in ("address", "bool", "bytes", "string"):
        return kind
    sized = _SIZED.fullmatch(kind)
    if sized is not None:
        size = int(sized.group(2))
        if (sized.group(1) == "uint" and size % 8 == 0 and size <= 256) or (
            sized.group(1) == "bytes" and size <= 32
        ):
            return kind
    raise EvmTransactionError("invalid_abi_type", kind)


def _dynamic(kind: _AbiType) -> bool:
    if isinstance(kind, str):
        return kind in ("bytes", "string")
    if kind[0] == "array":
        return kind[2] is None or _dynamic(kind[1])
    return any(_dynamic(item) for item in kind[1])


def _items(value: object, length: int | None) -> Sequence[object]:
    if not isinstance(value, (list, tuple)) or (
        length is not None and len(value) != length
    ):
        raise EvmTransactionError("invalid_abi_value")
    return value


def _sequence(kinds: Sequence[_AbiType], values: Sequence[object]) -> bytes:
    encoded = [_encode(kind, value) for kind, value in zip(kinds, values)]
    offset = sum(
        _WORD if _dynamic(kind) else len(item) for kind, item in zip(kinds, encoded)
    )
    heads, tails = [], []
    for kind, item in zip(kinds, encoded):
        if _dynamic(kind):
            heads.append(offset.to_bytes(_WORD, "big"))
            tails.append(item)
            offset += len(item)
        else:
            heads.append(item)
    return b"".join(heads) + b"".join(tails)


def _encode(kind: _AbiType, value: object) -> bytes:
    if isinstance(kind, tuple):
        if kind[0] == "tuple":
            return _sequence(kind[1], _items(value, len(kind[1])))
        items = _items(value, kind[2])
        body = _sequence([kind[1]] * len(items), items)
        return body if kind[2] is not None else len(items).to_bytes(_WORD, "big") + body
    if kind in ("bytes", "string"):
        if kind == "string" and type(value) is str:
            try:
                value = value.encode("utf-8")
            except UnicodeEncodeError:
                raise EvmTransactionError("invalid_abi_value") from None
        elif kind == "string" or type(value) is not bytes:
            raise EvmTransactionError("invalid_abi_value")
        return len(value).to_bytes(_WORD, "big") + value + bytes(-len(value) % _WORD)
    if kind == "bool":
        if type(value) is not bool:
            raise EvmTransactionError("invalid_abi_value")
        return int(value).to_bytes(_WORD, "big")
    if kind == "address":
        if type(value) is not bytes or len(value) != 20:
            raise EvmTransactionError("invalid_abi_value")
        return value.rjust(_WORD, b"\x00")
    if kind.startswith("uint"):
        if type(value) is not int or not 0 <= value < 1 << int(kind[4:]):
            raise EvmTransactionError("invalid_abi_value")
        return value.to_bytes(_WORD, "big")
    if type(value) is not bytes or len(value) != int(kind[5:]):
        raise EvmTransactionError("invalid_abi_value")
    return value.ljust(_WORD, b"\x00")


def encode_call(selector: str, types: Sequence[str], values: Sequence[object]) -> bytes:
    """ABI calldata for ``selector(types...)``: the 4-byte selector, then ``values``.

    Types are canonical Solidity types (uintN, bytesN, bytes, string, bool,
    address, T[k], T[] and tuples); integers are ints, addresses 20 bytes,
    bytesN exactly N bytes, strings str, arrays and tuples lists or tuples.
    """
    if type(selector) is not str or _NAME.fullmatch(selector) is None:
        raise EvmTransactionError("invalid_abi_type", "selector")
    if any(type(kind) is not str for kind in types):
        raise EvmTransactionError("invalid_abi_type")
    kinds = [_parse(kind) for kind in types]
    signature = f"{selector}({','.join(types)})"
    return keccak256(signature.encode())[:4] + _sequence(
        kinds, _items(values, len(kinds))
    )
