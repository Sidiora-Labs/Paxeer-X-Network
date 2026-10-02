# Generated from the LayerX Agent API schema. Do not hand-edit.

from collections.abc import Mapping
from enum import IntEnum
from typing import NotRequired, TypedDict, Generic, Literal, Protocol, TypeAlias, TypeVar

def layerx_sdk_py_package() -> Mapping[str, str | int]: ...

{{SCALARS}}

class VerificationLevel(IntEnum):
{{LEVELS}}

ErrorClass: TypeAlias = Literal[{{ERRORS}}]
Operation: TypeAlias = Literal[{{OPERATIONS}}]

{{APPROVAL}}

class NativeActivityV1(TypedDict):
    version: Literal["1"]
    module: str
    ordinal: str

class NativePurposeV1(TypedDict):
    version: Literal["1"]
    tenant: str
    agent_did: str
    session_id: str
    generation: str
    expires_at_ms: str
    capability_id: str
    preparation_id: str
    canonical_digest: str
    commitment: str

class SignedNativePurposeV1(TypedDict):
    purpose: NativePurposeV1
    owner_public_key: str
    signature: str

class NativeLocalGrantV1(TypedDict):
    version: Literal["1"]
    capability: str
    session_scope: str
    expires_at_ms: str
    owner_public_key: str
    signature: str

class NativePrepareRequestV1(TypedDict):
    variant: Literal["native_v1"]
    activity: NativeActivityV1
    actor: str
    authority: str
    account_sequence: str
    not_before: str
    not_after: str
    idempotency_key: str
    fee_limit: str
    payload: str
    payload_hash: str
    capability_id: str
    purpose: SignedNativePurposeV1
    local_grant: NotRequired[NativeLocalGrantV1 | None]

class NativePrepareResultV1(TypedDict):
    version: Literal["1"]
    preparation_id: str
    canonical_bytes: str
    signing_preimage: str
    activity: NativeActivityV1
    approval_required: bool
    approval_id: str | None

class NativeApprovalDecisionV1(TypedDict):
    variant: Literal["native_v1"]
    approval_id: str
    held_digest: str
    current_sequence: str

class NativeApprovalResultV1(TypedDict):
    version: Literal["1"]
    approval_id: str
    held_digest: str
    activity: NativeActivityV1
    state: Literal["Awaiting", "Granted", "Rejected", "Expired", "Defective", "NotRequired"]
    submission_ref: str | None

class NativeApprovalListResultV1(TypedDict):
    version: Literal["1"]
    approvals: list[NativeApprovalResultV1]


def encode_native_prepare_request(value: NativePrepareRequestV1) -> NativePrepareRequestV1: ...
def encode_native_approval_decision(value: NativeApprovalDecisionV1) -> NativeApprovalDecisionV1: ...
def encode_native_approval_get(approval_id: str) -> dict[str, str]: ...
def decode_native_prepare_result(value: object) -> NativePrepareResultV1: ...
def decode_native_approval_result(value: object) -> NativeApprovalResultV1: ...
def decode_native_approval_list_result(value: object) -> NativeApprovalListResultV1: ...


_T = TypeVar("_T")
_R = TypeVar("_R")

class VerifiedRead(Generic[_R]):
    value: _R
    achieved_verification_level: VerificationLevel
    chain_head: int
    latest_batch: str
    latest_checkpoint: str
    value_sequence: int
    def __init__(
        self,
        value: _R,
        achieved_verification_level: VerificationLevel,
        chain_head: int,
        latest_batch: str,
        latest_checkpoint: str,
        value_sequence: int,
    ) -> None: ...

def require_verified(requested: VerificationLevel, read: VerifiedRead[_R]) -> VerifiedRead[_R]: ...

class SubmissionUnknown:
    kind: Literal["Unknown"]
    def __init__(self, kind: Literal["Unknown"] = "Unknown") -> None: ...

class SubmissionExecuted:
    receipt_ref: str
    kind: Literal["Executed"]
    def __init__(self, receipt_ref: str, kind: Literal["Executed"] = "Executed") -> None: ...

class SubmissionFailed:
    protocol_result_code: int
    kind: Literal["Failed"]
    def __init__(self, protocol_result_code: int, kind: Literal["Failed"] = "Failed") -> None: ...

class SubmissionPending:
    stage: str
    kind: Literal["Pending"]
    def __init__(self, stage: str, kind: Literal["Pending"] = "Pending") -> None: ...

SubmissionState: TypeAlias = SubmissionUnknown | SubmissionExecuted | SubmissionFailed | SubmissionPending

class IdempotentMutation(Generic[_T]):
    request_id: int
    key: bytes
    body_digest: bytes
    operation: _T
    def __init__(self, request_id: int, key: bytes, body_digest: bytes, operation: _T) -> None: ...

class ApiError(Exception):
    error_class: ErrorClass
    protocol_result_code: int | None
    retriable: bool
    request_id: int
    reason: str
    def __init__(
        self,
        error_class: ErrorClass,
        protocol_result_code: int | None,
        retriable: bool,
        request_id: int,
        reason: str,
    ) -> None: ...

class Transport(Protocol):
    def call(self, operation: Operation, request: object) -> object: ...

class Client:
    def __init__(self, transport: Transport) -> None: ...
    def call(self, operation: Operation, request: object) -> object: ...
    def approval_list(self, request: ApprovalListRequest) -> ApprovalPage: ...
    def approval_get(self, request: ApprovalGetRequest) -> ApprovalRecord: ...
    def approval_approve(self, request: ApprovalApproveRequest) -> ApprovalDecision: ...
    def approval_reject(self, request: ApprovalRejectRequest) -> ApprovalDecision: ...
