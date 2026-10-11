from collections.abc import Sequence

TRANSACTION_TYPE: int
MAX_TRANSACTION_BYTES: int

class EvmTransactionError(ValueError):
    code: str
    def __init__(self, code: str, detail: str | None = None) -> None: ...

class Type2Transaction:
    chain_id: int
    nonce: int
    max_priority_fee_per_gas: int
    max_fee_per_gas: int
    gas_limit: int
    to: bytes
    value: int
    data: bytes
    def __init__(
        self,
        chain_id: int,
        nonce: int,
        max_priority_fee_per_gas: int,
        max_fee_per_gas: int,
        gas_limit: int,
        to: bytes,
        value: int,
        data: bytes,
    ) -> None: ...
    def encode(self) -> bytes: ...
    @classmethod
    def decode(cls, raw: bytes) -> Type2Transaction: ...
    def signing_digest(self) -> bytes: ...
    def recover_signer(self, signature: bytes) -> str: ...
    def attach_signature(self, signature: bytes, expected_signer: str) -> bytes: ...

def decode_signed(raw: bytes) -> tuple[Type2Transaction, bytes]: ...
def activity_id(raw: bytes) -> bytes: ...
def encode_call(
    selector: str, types: Sequence[str], values: Sequence[object]
) -> bytes: ...
