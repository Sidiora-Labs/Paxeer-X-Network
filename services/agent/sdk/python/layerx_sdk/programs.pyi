from collections.abc import Callable, Mapping
from typing import Literal, TypeAlias

from .native_program_call import NativeProgramCall
from .program_wire import OccupancyPayer
from .production import IdempotencyKey, ProductionClient
from .verifier import (
    AuthorizedReceiptBatch,
    LocalSignatureVerifier,
    ReceiptVerification,
)

ProgramCapability: TypeAlias = Literal["storage_read", "storage_write", "transfer", "emit_event", "compose"]
ProgramLifecycle: TypeAlias = Literal["active", "deprecated", "tombstoned"]
ProgramSourceStatus: TypeAlias = Literal["unpublished", "verified", "mismatch"]

class ProgramCall:
    program_id: str
    calldata: bytes
    fuel: int
    fee_limit: int
    capabilities: tuple[ProgramCapability, ...]
    signed_activity: bytes
    def __init__(self, program_id: str, calldata: bytes, fuel: int, fee_limit: int, capabilities: tuple[ProgramCapability, ...], signed_activity: bytes) -> None: ...

class ProgramSource:
    status: ProgramSourceStatus
    source_digest: str | None
    environment_digest: str | None
    pipeline: str | None
    expected_code_hash: str | None
    reproduced_artifact_digest: str | None

class ProgramDiscovery:
    program_id: str
    lifecycle: ProgramLifecycle
    version: int
    code_hash: str
    abi_version: int
    receipt_digest: str
    state_root: str
    observed_sequence: int
    observed_at: int
    valid_through: int
    verification: Literal["server-side-receipt-verification-only"]
    deployment_receipt_digest: str | None
    sequencer_signature_verified: bool
    value_accounts: Mapping[str, object] | None

class ProgramInterface:
    program_id: str
    version: int
    code_hash: str
    abi_version: int
    interface: bytes
    interface_digest: str
    receipt_digest: str
    state_root: str
    observed_sequence: int
    observed_at: int
    valid_through: int
    source: ProgramSource
    verification: Literal["server-side-receipt-verification-only"]

class ProgramTrustContext:
    sequencer_public_key: bytes
    clock_milliseconds: Callable[[], int]
    protocol_version: int
    network_id: int | None
    maximum_simulation_age_milliseconds: int
    def __init__(self, sequencer_public_key: bytes, clock_milliseconds: Callable[[], int] = ..., maximum_simulation_age_milliseconds: int = ..., protocol_version: int = ..., network_id: int | None = ...) -> None: ...
    def now_milliseconds(self) -> int: ...

class VerifiedProgramReceipt:
    verification: ReceiptVerification
    terminal_payload: bytes
    call_graph: bytes
    transfer_verification: Literal["reconstructed", "recorded_terminal_root_not_locally_reconstructable"]
    occupancy_payment_accounts: tuple[bytes, ...]

def verify_program_receipt(execution: Mapping[str, object], authority: AuthorizedReceiptBatch, signatures: LocalSignatureVerifier, trust: ProgramTrustContext, *, expected_signed_activity: bytes | None = ..., occupancy_payers: tuple[OccupancyPayer, ...] = ...) -> VerifiedProgramReceipt: ...

class ProgramOperations:
    def __init__(self, client: ProductionClient, signatures: LocalSignatureVerifier, trust: ProgramTrustContext) -> None: ...
    def discover(self, program_id: str) -> ProgramDiscovery: ...
    def interface(self, program_id: str) -> ProgramInterface: ...
    def simulate(self, call: ProgramCall | NativeProgramRequest) -> Mapping[str, object]: ...
    def submit(self, call: ProgramCall | NativeProgramRequest, idempotency_key: IdempotencyKey) -> Mapping[str, object]: ...
    def receipt(self, idempotency_key: str, expected_activity_id: str) -> Mapping[str, object]: ...
    def activity(self, activity_id: str) -> Mapping[str, object]: ...

def platform_sdk_programs() -> str: ...

class NativeProgramRequest:
    native_call: NativeProgramCall
    fee_limit: int
    signed_activity: bytes
    program_id: str
    calldata: bytes
    fuel: int
    capabilities: tuple[ProgramCapability, ...]
    def __init__(self, native_call: NativeProgramCall, fee_limit: int, signed_activity: bytes) -> None: ...
