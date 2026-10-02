# Generated from the LayerX Agent API schema. Do not hand-edit.

from collections.abc import Mapping
from dataclasses import dataclass
from enum import IntEnum
from types import MappingProxyType
from typing import NotRequired, TypedDict, Generic, Literal, Protocol, TypeAlias, TypeVar, cast

_PACKAGE_METADATA: Mapping[str, str | int] = MappingProxyType({
    "name": "layerx-sdk",
    "version": "0.1.0",
    "contract_major": 1,
})

def layerx_sdk_py_package() -> Mapping[str, str | int]:
    return _PACKAGE_METADATA

{{SCALARS}}

class VerificationLevel(IntEnum):
{{LEVELS}}

ErrorClass = Literal[{{ERRORS}}]
Operation = Literal[{{OPERATIONS}}]

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


def _native_record(value: object, fields: tuple[str, ...], optional: tuple[str, ...] = ()) -> Mapping[str, object]:
    if not isinstance(value, dict) or any(not isinstance(key, str) for key in value):
        raise ValueError("native_v1.object")
    if not set(fields) <= set(value) or not set(value) <= set(fields + optional):
        raise ValueError("native_v1.fields")
    return value


def _native_text(value: object, maximum: int) -> str:
    if not isinstance(value, str) or not value or len(value.encode("utf-8")) > maximum or "\0" in value:
        raise ValueError("native_v1.text")
    return value


def _native_decimal(value: object, maximum: int = (1 << 64) - 1) -> str:
    if not isinstance(value, str) or not value or len(value) > 39 or not value.isascii() or not value.isdecimal() or (value != "0" and value.startswith("0")) or int(value) > maximum:
        raise ValueError("native_v1.integer")
    return value


def _native_hex(value: object, count: int, exact: bool = True) -> str:
    if not isinstance(value, str) or not value or len(value) % 2 or any(char not in "0123456789abcdef" for char in value) or (len(value) != count * 2 if exact else len(value) > count * 2):
        raise ValueError("native_v1.hex")
    return value


def _native_version(value: object) -> Literal["1"]:
    if value != "1":
        raise ValueError("native_v1.version")
    return "1"


def _native_optional_id(value: object) -> str | None:
    return None if value is None else _native_hex(value, 32)


def _native_activity(value: object) -> NativeActivityV1:
    item = _native_record(value, ("version", "module", "ordinal"))
    module, ordinal = _native_decimal(item["module"], 11), _native_decimal(item["ordinal"], 65535)
    if module == "0" or ordinal == "0":
        raise ValueError("native_v1.activity")
    return {"version": _native_version(item["version"]), "module": module, "ordinal": ordinal}


def encode_native_prepare_request(value: NativePrepareRequestV1) -> NativePrepareRequestV1:
    item = _native_record(value, ("variant", "activity", "actor", "authority", "account_sequence", "not_before", "not_after", "idempotency_key", "fee_limit", "payload", "payload_hash", "capability_id", "purpose"), ("local_grant",))
    if item["variant"] != "native_v1":
        raise ValueError("native_v1.variant")
    signed = _native_record(item["purpose"], ("purpose", "owner_public_key", "signature"))
    raw = _native_record(signed["purpose"], ("version", "tenant", "agent_did", "session_id", "generation", "expires_at_ms", "capability_id", "preparation_id", "canonical_digest", "commitment"))
    purpose: NativePurposeV1 = {"version": _native_version(raw["version"]), "tenant": _native_text(raw["tenant"], 255),
        "agent_did": _native_text(raw["agent_did"], 255), "session_id": _native_hex(raw["session_id"], 32),
        "generation": _native_decimal(raw["generation"]), "expires_at_ms": _native_decimal(raw["expires_at_ms"]),
        "capability_id": _native_hex(raw["capability_id"], 32), "preparation_id": _native_hex(raw["preparation_id"], 32),
        "canonical_digest": _native_hex(raw["canonical_digest"], 32), "commitment": _native_hex(raw["commitment"], 32)}
    activity, actor, capability_id = _native_activity(item["activity"]), _native_text(item["actor"], 255), _native_hex(item["capability_id"], 32)
    not_before, not_after = _native_decimal(item["not_before"]), _native_decimal(item["not_after"])
    if activity["module"] != "9" or actor != purpose["agent_did"] or capability_id != purpose["capability_id"] or purpose["generation"] == "0" or purpose["expires_at_ms"] == "0" or int(not_before) > int(not_after):
        raise ValueError("native_v1.binding")
    local_grant: NativeLocalGrantV1 | None = None
    if item.get("local_grant") is not None:
        grant = _native_record(item["local_grant"], ("version", "capability", "session_scope", "expires_at_ms", "owner_public_key", "signature"))
        local_grant = {"version": _native_version(grant["version"]), "capability": _native_hex(grant["capability"], 1048576, False),
            "session_scope": _native_hex(grant["session_scope"], 1048576, False), "expires_at_ms": _native_decimal(grant["expires_at_ms"]),
            "owner_public_key": _native_hex(grant["owner_public_key"], 32), "signature": _native_hex(grant["signature"], 64)}
        if local_grant["expires_at_ms"] == "0":
            raise ValueError("native_v1.expiry")
    return {"variant": "native_v1", "activity": activity, "actor": actor, "authority": _native_text(item["authority"], 524288),
        "account_sequence": _native_decimal(item["account_sequence"]), "not_before": not_before, "not_after": not_after,
        "idempotency_key": _native_hex(item["idempotency_key"], 32), "fee_limit": _native_decimal(item["fee_limit"], (1 << 128) - 1),
        "payload": _native_hex(item["payload"], 524288, False), "payload_hash": _native_hex(item["payload_hash"], 32),
        "capability_id": capability_id, "purpose": {"purpose": purpose, "owner_public_key": _native_hex(signed["owner_public_key"], 32), "signature": _native_hex(signed["signature"], 64)}, "local_grant": local_grant}


def encode_native_approval_decision(value: NativeApprovalDecisionV1) -> NativeApprovalDecisionV1:
    item = _native_record(value, ("variant", "approval_id", "held_digest", "current_sequence"))
    if item["variant"] != "native_v1":
        raise ValueError("native_v1.variant")
    return {"variant": "native_v1", "approval_id": _native_hex(item["approval_id"], 32), "held_digest": _native_hex(item["held_digest"], 32), "current_sequence": _native_decimal(item["current_sequence"])}


def encode_native_approval_get(approval_id: str) -> dict[str, str]:
    return {"variant": "native_v1", "approval_id": _native_hex(approval_id, 32)}


def decode_native_prepare_result(value: object) -> NativePrepareResultV1:
    item = _native_record(value, ("version", "preparation_id", "canonical_bytes", "signing_preimage", "activity", "approval_required", "approval_id"))
    approval_required, approval_id = item["approval_required"], _native_optional_id(item["approval_id"])
    if not isinstance(approval_required, bool) or approval_required != (approval_id is not None):
        raise ValueError("native_v1.approval")
    return {"version": _native_version(item["version"]), "preparation_id": _native_hex(item["preparation_id"], 32),
        "canonical_bytes": _native_hex(item["canonical_bytes"], 1048576, False), "signing_preimage": _native_hex(item["signing_preimage"], 32),
        "activity": _native_activity(item["activity"]), "approval_required": approval_required, "approval_id": approval_id}


def decode_native_approval_result(value: object) -> NativeApprovalResultV1:
    item = _native_record(value, ("version", "approval_id", "held_digest", "activity", "state", "submission_ref"))
    state = item["state"]
    if state == "Awaiting":
        checked: Literal["Awaiting", "Granted", "Rejected", "Expired", "Defective", "NotRequired"] = "Awaiting"
    elif state == "Granted": checked = "Granted"
    elif state == "Rejected": checked = "Rejected"
    elif state == "Expired": checked = "Expired"
    elif state == "Defective": checked = "Defective"
    elif state == "NotRequired": checked = "NotRequired"
    else: raise ValueError("native_v1.state")
    return {"version": _native_version(item["version"]), "approval_id": _native_hex(item["approval_id"], 32), "held_digest": _native_hex(item["held_digest"], 32),
        "activity": _native_activity(item["activity"]), "state": checked, "submission_ref": _native_optional_id(item["submission_ref"])}


def decode_native_approval_list_result(value: object) -> NativeApprovalListResultV1:
    item = _native_record(value, ("version", "approvals"))
    approvals = item["approvals"]
    if not isinstance(approvals, list) or len(approvals) > 100:
        raise ValueError("native_v1.list")
    return {"version": _native_version(item["version"]), "approvals": [decode_native_approval_result(row) for row in approvals]}


_T = TypeVar("_T")
_R = TypeVar("_R")

@dataclass(frozen=True)
class VerifiedRead(Generic[_R]):
    value: _R
    achieved_verification_level: VerificationLevel
    chain_head: int
    latest_batch: str
    latest_checkpoint: str
    value_sequence: int

def require_verified(requested: VerificationLevel, read: VerifiedRead[_R]) -> VerifiedRead[_R]:
    if read.achieved_verification_level == VerificationLevel.UNVERIFIED:
        raise ValueError("unverified_read")
    if read.achieved_verification_level < requested:
        raise ValueError(
            f"verification_below_requested:{requested.value}:{read.achieved_verification_level.value}"
        )
    return read

@dataclass(frozen=True)
class SubmissionUnknown:
    kind: Literal["Unknown"] = "Unknown"

@dataclass(frozen=True)
class SubmissionExecuted:
    receipt_ref: str
    kind: Literal["Executed"] = "Executed"

@dataclass(frozen=True)
class SubmissionFailed:
    protocol_result_code: int
    kind: Literal["Failed"] = "Failed"

@dataclass(frozen=True)
class SubmissionPending:
    stage: str
    kind: Literal["Pending"] = "Pending"

SubmissionState: TypeAlias = (
    SubmissionUnknown | SubmissionExecuted | SubmissionFailed | SubmissionPending
)

@dataclass(frozen=True)
class IdempotentMutation(Generic[_T]):
    request_id: int
    key: bytes
    body_digest: bytes
    operation: _T

@dataclass(frozen=True)
class ApiError(Exception):
    error_class: ErrorClass
    protocol_result_code: int | None
    retriable: bool
    request_id: int
    reason: str

    def __str__(self) -> str:
        return f"{self.error_class}:{self.reason}"

class Transport(Protocol):
    def call(self, operation: Operation, request: object) -> object: ...

class Client:
    def __init__(self, transport: Transport) -> None:
        self._transport = transport

    def call(self, operation: Operation, request: object) -> object:
        return self._transport.call(operation, request)

    def approval_list(self, request: ApprovalListRequest) -> ApprovalPage:
        return cast(ApprovalPage, self.call("approval.list", request))

    def approval_get(self, request: ApprovalGetRequest) -> ApprovalRecord:
        return cast(ApprovalRecord, self.call("approval.get", request))

    def approval_approve(self, request: ApprovalApproveRequest) -> ApprovalDecision:
        return cast(ApprovalDecision, self.call("approval.approve", request))

    def approval_reject(self, request: ApprovalRejectRequest) -> ApprovalDecision:
        return cast(ApprovalDecision, self.call("approval.reject", request))
