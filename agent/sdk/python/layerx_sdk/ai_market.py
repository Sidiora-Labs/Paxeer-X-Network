from __future__ import annotations

import json
import os
import struct
from collections.abc import Mapping
from dataclasses import dataclass
from enum import IntEnum
from hashlib import sha256
from http.client import HTTPException
from pathlib import Path
from typing import Protocol
from urllib.error import HTTPError, URLError
from urllib.request import Request, build_opener

from .agent_http import (
    LayerXKeyCredential,
    _NoRedirect,
    _bounded_read,
    _reject_constant,
    _route_endpoint,
    _unique_object,
    _validated_endpoint,
)
from .native_program_call import NativeProgramCall, decode_native_program_call, encode_native_program_call
from .production import IdempotencyKey, PlatformSdkError, SdkErrorCode
from .program_wire import _signed_call_envelope
from .programs import NativeProgramRequest, ProgramOperations
from .verifier import AuthorizedReceiptBatch, CheckpointVerification, LocalSignatureVerifier, verify_receipt_outcome

FINALIZED_RANK = 4
SETTLEMENT_RANK = 5
DEFAULT_PAGE_ROWS = 16
MAX_PAGE_ROWS = 32
CURSOR_MAX_BYTES = 1024
AUTHORITY_FRESHNESS_HEIGHTS = 8
ENVELOPE_MAX_BYTES = 16384
PAYLOAD_MAX_BYTES = 15965
NATIVE_CALL_PROTOCOL_VERSION = 3
PROGRAMS_MODULE = 9
PROGRAM_CALL_ORDINAL = 3
GUEST_ABI = 5
ENTRYPOINT = "layerx_call"
FUNDING_POLICY_VERSION = 1
ELIGIBLE_SERVING = 1

BAD_VERSION = 0x0001
NON_CANONICAL = 0x0002
WRONG_DOMAIN = 0x0003
WRONG_PROGRAM = 0x0004
WRONG_MARKET = 0x0005
UNAUTHORIZED = 0x0006
WRONG_CONFIG = 0x000D
WRONG_ROSTER = 0x000E
NOT_FOUND = 0x0015
CAPACITY = 0x0016
UNKNOWN_OPERATION = 0x001F
F06_FUNDING_POLICY_MISMATCH = 0x0601
F06_REFUND_RECIPIENT_MISMATCH = 0x0602
F06_INVALID_AMOUNT = 0x0604
F06_UNKNOWN_WORKER_ENTITLEMENT = 0x060B
F06_WRONG_CLAIM_RECIPIENT = 0x060C
F06_NOTHING_TO_CLAIM = 0x060E
F06_CONTRIBUTION_CONSENT_REQUIRED = 0x0610

QUERY_ERRORS = ("BindingMismatch", "FinalityUnavailable", "SnapshotConflict", "IntegrityFailure",
                "CursorExpired", "CursorMismatch", "ResponseTooLarge")
CLIENT_ERRORS = ("StaleAuthority", "NotNativeProgramCall", "NotMutation", "ReviewMismatch", "UnauthorizedKey",
                 "InvalidTransition", "ReceiptMismatch", "FinalityMismatch", "HistoryUnavailable", "CorruptRecord")
MISMATCH_FIELDS = ("action", "chain", "program", "market", "actor", "epoch", "config", "roster", "policy", "snapshot",
                   "amount", "asset", "payee", "worker", "capabilities", "access_declaration", "response_capacity",
                   "resources", "fee_limit", "validity", "expiry", "idempotency_key", "authority", "commitment")

_VIEW_DOMAIN = b"PAXAI/view/v1\0"
_REQUEST_DOMAIN = b"PAXAI/request/v1\0"
_ACTIVITY_DOMAIN = b"LXP/v1/activity-id\0"
_PAYLOAD_DOMAIN = b"LXP/v1/payload-hash\0"
_RECORD_MAGIC = b"PAXAIOP1"
_RECORD_EXTENSION = ".op"
_VIEW_MAX_RESPONSE_BYTES = 1_048_576
_VIEW_PATH = "/v1/ai/markets/"
_HEX = frozenset("0123456789abcdef")
_FEATURES = ("F01", "F02", "F03", "F04", "F05", "F06", "F07", "F08", "F09", "F10")
_TERMINAL_REFUSALS = frozenset({SdkErrorCode.CORE_REJECTION, SdkErrorCode.POLICY_REFUSAL,
                                SdkErrorCode.CAPABILITY_REFUSAL, SdkErrorCode.BUDGET_REFUSAL})


class AiMarketError(Exception):
    def __init__(self, code: str, detail: object = None) -> None:
        super().__init__(code if detail is None else f"{code}: {detail!r}")
        self.code = code
        self.detail = detail


def _application(code: int) -> AiMarketError:
    return AiMarketError("Application", code)


def _integrity() -> AiMarketError:
    return AiMarketError("IntegrityFailure")


class _WireEnum(IntEnum):
    @property
    def wire(self) -> str:
        return self.name.lower().replace("_", "-")

    @classmethod
    def from_wire(cls, value: object) -> _WireEnum:
        for member in cls:
            if member.wire == value:
                return member
        raise _integrity()


class ProjectionState(_WireEnum):
    OBSERVED_UNVERIFIED = 1
    EVIDENCE_VERIFIED_UNFINALIZED = 2
    FINALIZED_PUBLISHABLE = 3
    ARCHIVED = 4
    QUARANTINED = 5


class FreshnessLabel(_WireEnum):
    CURRENT = 1
    STALE = 2
    UNKNOWN = 3


class EpochStatus(_WireEnum):
    RETAINED = 1
    NEVER_OPENED = 2
    RETAINED_TERMINAL = 3
    ARCHIVE_REQUIRED = 4
    ARCHIVE_UNAVAILABLE = 5
    UNSUPPORTED_VERSION = 6


class Availability(_WireEnum):
    AVAILABLE = 1
    NOT_ENABLED = 2
    NOT_YET_PRODUCED = 3
    CONTENT_UNAVAILABLE = 4
    UNSUPPORTED_VERSION = 5


class ParticipantKind(_WireEnum):
    WORKER = 1
    EVALUATOR = 2


class ScoreStatus(_WireEnum):
    PRESENT = 1
    NO_ADMISSIBLE_SCORE = 2
    INSUFFICIENT_COVERAGE = 3
    NOT_PRODUCED = 4
    UNAVAILABLE = 5
    UNSUPPORTED = 6


class KindFilter(_WireEnum):
    ALL = 0
    WORKER = 1
    EVALUATOR = 2


class SdkState(IntEnum):
    PREPARED = 1
    REVIEWED = 2
    SIGNED = 3
    SUBMITTING = 4
    PENDING = 5
    EXECUTED = 6
    FINALIZED = 7
    FAILED = 8
    UNKNOWN = 9


class DomainStatus(IntEnum):
    PENDING = 1
    COMPLETED = 2


SDK_TRANSITIONS: Mapping[SdkState, frozenset[SdkState]] = {
    SdkState.REVIEWED: frozenset({SdkState.PREPARED}),
    SdkState.SIGNED: frozenset({SdkState.REVIEWED}),
    SdkState.SUBMITTING: frozenset({SdkState.SIGNED, SdkState.UNKNOWN}),
    SdkState.PENDING: frozenset({SdkState.SUBMITTING}),
    SdkState.EXECUTED: frozenset({SdkState.PENDING, SdkState.UNKNOWN}),
    SdkState.FINALIZED: frozenset({SdkState.EXECUTED}),
    SdkState.FAILED: frozenset({SdkState.SIGNED, SdkState.SUBMITTING, SdkState.PENDING, SdkState.UNKNOWN}),
    SdkState.UNKNOWN: frozenset({SdkState.SUBMITTING}),
}

SOURCELESS_EPOCH_STATUSES = frozenset({EpochStatus.NEVER_OPENED, EpochStatus.UNSUPPORTED_VERSION})


@dataclass(frozen=True)
class Operation:
    name: str
    selector: int
    feature: int
    mutation: bool
    object_local: bool
    delegate: bool
    payload_min: int
    payload_max: int


def _operations(rows: tuple[tuple[str, int, int, bool, bool, bool, int, int], ...]) -> Mapping[int, Operation]:
    return {row[1]: Operation(*row) for row in rows}


_M, _R = True, False
OPERATIONS: Mapping[int, Operation] = _operations((
    ("CREATE", 0x0101, 1, _M, False, False, 0, 15965),
    ("STAGE_POLICY", 0x0102, 1, _M, False, False, 0, 15965),
    ("CANCEL_POLICY", 0x0103, 1, _M, False, False, 16, 16),
    ("SCHEDULE_ACTIVATION", 0x0104, 1, _M, False, False, 16, 16),
    ("ADVANCE_ACTIVATION", 0x0105, 1, _M, True, False, 16, 16),
    ("SUSPEND", 0x0106, 1, _M, False, False, 40, 48),
    ("UPDATE_METADATA", 0x0107, 1, _M, False, False, 40, 48),
    ("APPOINT_OPERATOR", 0x0108, 1, _M, False, False, 49, 49),
    ("REVOKE_OPERATOR", 0x0109, 1, _M, False, False, 16, 16),
    ("REQUEST_CLOSE", 0x010A, 1, _M, False, False, 40, 40),
    ("ADVANCE_CLOSE", 0x010B, 1, _M, True, False, 11, 11),
    ("ADMIT_TASK", 0x010C, 1, _M, True, False, 248, 248),
    ("ACCEPT_TASK", 0x010D, 1, _M, False, True, 72, 72),
    ("CANCEL_TASK", 0x010E, 1, _M, True, False, 32, 32),
    ("COMMIT_TASK_RESULT", 0x010F, 1, _M, False, True, 72, 72),
    ("SEAL_TASK_SET", 0x0110, 1, _M, True, False, 48, 48),
    ("OPEN_EPOCH", 0x0111, 1, _M, True, False, 0, 15965),
    ("EnrollWorker", 0x0201, 2, _M, False, False, 136, 136),
    ("PublishMetadata", 0x0202, 2, _M, False, True, 0, 15965),
    ("SetDraining", 0x0203, 2, _M, False, False, 0, 15965),
    ("UndoDrain", 0x0204, 2, _M, False, False, 0, 15965),
    ("RotateDelegate", 0x0205, 2, _M, False, False, 0, 15965),
    ("RevokeDelegate", 0x0206, 2, _M, False, False, 0, 15965),
    ("RetireWorker", 0x0207, 2, _M, False, False, 0, 15965),
    ("AcceptEnrollment", 0x0209, 2, _M, False, False, 144, 144),
    ("ExpireEnrollment", 0x020A, 2, _M, False, False, 72, 72),
    ("ScheduleEvaluator", 0x0301, 3, _M, False, False, 160, 160),
    ("RotateEvaluatorKey", 0x0302, 3, _M, False, False, 88, 88),
    ("RevokeEvaluator", 0x0303, 3, _M, False, False, 74, 74),
    ("ChallengeAssessment", 0x0304, 3, _M, True, False, 97, 97),
    ("CommitScore", 0x0401, 4, _M, False, True, 224, 224),
    ("RevealScore", 0x0402, 4, _M, False, True, 364, 1480),
    ("BeginAggregation", 0x0501, 5, _M, True, False, 0, 0),
    ("ProcessAggregation", 0x0502, 5, _M, True, False, 34, 34),
    ("FinalizeAggregation", 0x0503, 5, _M, True, False, 32, 32),
    ("FUND", 0x0601, 6, _M, False, False, 57, 57),
    ("CLAIM", 0x0602, 6, _M, True, False, 80, 80),
    ("EXPIRE_EPOCH_CLAIMS", 0x0603, 6, _M, True, False, 0, 0),
    ("REFUND_FREE", 0x0604, 6, _M, True, False, 64, 64),
    ("PRUNE_EPOCH", 0x0605, 6, _M, True, False, 0, 0),
    ("ResetHistory", 0x0701, 7, _M, False, False, 73, 73),
    ("SuspendHistory", 0x0702, 7, _M, False, False, 73, 73),
    ("ResumeHistory", 0x0703, 7, _M, False, False, 73, 73),
    ("ApproveAdmission", 0x0801, 8, _M, False, False, 0, 15965),
    ("RevokeAdmissionApproval", 0x0802, 8, _M, False, False, 0, 15965),
    ("AdmitWorker", 0x0803, 8, _M, False, False, 0, 15965),
    ("AdmitEvaluator", 0x0804, 8, _M, False, False, 0, 15965),
    ("Heartbeat", 0x0805, 8, _M, False, True, 0, 15965),
    ("RequestExit", 0x0806, 8, _M, False, False, 0, 15965),
    ("CancelExit", 0x0807, 8, _M, False, False, 0, 15965),
    ("PruneInactive", 0x0808, 8, _M, True, False, 0, 15965),
    ("AdministrativeRemove", 0x0809, 8, _M, False, False, 0, 15965),
    ("SealEvidence", 0x0901, 9, _M, False, True, 131, 131),
    ("READ_HEADER", 0x0A01, 10, _R, True, False, 0, 0),
    ("READ_STATE_CHUNK", 0x0A02, 10, _R, True, False, 46, 46),
))
FUND = 0x0601
CLAIM = 0x0602
REFUND_FREE = 0x0604


def _registry_before_epoch(selector: int) -> bool:
    return (0x0101 <= selector <= 0x010B or selector == 0x0111 or 0x0201 <= selector <= 0x0207
            or 0x0209 <= selector <= 0x020A or 0x0301 <= selector <= 0x0303 or selector in (0x0601, 0x0604)
            or 0x0801 <= selector <= 0x0804 or 0x0A01 <= selector <= 0x0A02)


def _unsigned(value: object, bits: int, name: str) -> int:
    if type(value) is not int or not 0 <= value < 1 << bits:
        raise ValueError(f"invalid {name}")
    return value


def _bytes32(value: object, name: str) -> bytes:
    if type(value) is not bytes or len(value) != 32 or value == bytes(32):
        raise ValueError(f"invalid {name}")
    return value


class _Reader:
    def __init__(self, data: bytes, failure: AiMarketError) -> None:
        self._data = data
        self._offset = 0
        self._failure = failure

    def take(self, length: int) -> bytes:
        end = self._offset + length
        if end > len(self._data):
            raise self._failure
        value = self._data[self._offset:end]
        self._offset = end
        return value

    def u8(self) -> int:
        return self.take(1)[0]

    def u16(self) -> int:
        return int.from_bytes(self.take(2), "big")

    def u32(self) -> int:
        return int.from_bytes(self.take(4), "big")

    def u64(self) -> int:
        return int.from_bytes(self.take(8), "big")

    def u128(self) -> int:
        return int.from_bytes(self.take(16), "big")

    def identifier(self) -> bytes:
        value = self.take(32)
        if value == bytes(32):
            raise self._failure
        return value

    @property
    def offset(self) -> int:
        return self._offset

    def finish(self) -> None:
        if self._offset != len(self._data):
            raise self._failure


@dataclass(frozen=True)
class PaxaiEnvelope:
    selector: int
    chain: bytes
    program: bytes
    market: bytes
    actor: bytes
    epoch: int
    config: int
    roster: bytes | None
    sequence: int
    expiry: int
    request: bytes
    payload: bytes
    delegate: tuple[bytes, bytes] | None = None

    def unsigned_bytes(self) -> bytes:
        for value in (self.epoch, self.config, self.sequence, self.expiry):
            _unsigned(value, 64, "envelope integer")
        if any(type(value) is not bytes or len(value) != 32 or value == bytes(32)
               for value in (self.chain, self.program, self.market, self.actor, self.request)):
            raise _application(NON_CANONICAL)
        if self.roster is not None and (type(self.roster) is not bytes or len(self.roster) != 32 or self.roster == bytes(32)):
            raise _application(NON_CANONICAL)
        if type(self.payload) is not bytes:
            raise ValueError("invalid envelope payload")
        if len(self.payload) > PAYLOAD_MAX_BYTES:
            raise _application(CAPACITY)
        return (b"PAXAI1" + struct.pack(">HH", 1, self.selector) + self.chain + self.program + self.market + self.actor
                + struct.pack(">QQ", self.epoch, self.config) + (self.roster or bytes(32))
                + struct.pack(">QQ", self.sequence, self.expiry) + self.request
                + struct.pack(">I", len(self.payload)) + self.payload)

    def encode(self) -> bytes:
        self.validate()
        if self.delegate is None:
            return self.unsigned_bytes() + b"\x00"
        key, signature = self.delegate
        if type(key) is not bytes or len(key) != 32 or type(signature) is not bytes or len(signature) != 64:
            raise _application(NON_CANONICAL)
        return self.unsigned_bytes() + b"\x01" + key + signature

    def request_digest(self) -> bytes:
        return sha256(_REQUEST_DOMAIN + self.unsigned_bytes()).digest()

    def validate(self) -> None:
        operation = OPERATIONS.get(self.selector)
        if operation is None:
            raise _application(UNKNOWN_OPERATION)
        self.unsigned_bytes()
        length = len(self.payload)
        if self.selector in (0x0106, 0x0107) and length not in (40, 48):
            raise _application(NON_CANONICAL)
        if not operation.payload_min <= length <= operation.payload_max or self.expiry == 0:
            raise _application(NON_CANONICAL)
        if operation.object_local != (self.sequence == 0):
            raise _application(NON_CANONICAL)
        if self.delegate is not None and not operation.delegate:
            raise _application(UNAUTHORIZED)
        if not operation.mutation:
            if self.epoch != 0 or self.config != 0 or self.roster is not None or self.delegate is not None:
                raise _application(NON_CANONICAL)
        elif self.config == 0:
            raise _application(NON_CANONICAL)
        if self.roster is None and (self.epoch != 0 or not _registry_before_epoch(self.selector)):
            raise _application(WRONG_ROSTER)

    def check_domain(self, chain: bytes, program: bytes, market: bytes) -> None:
        if self.chain != chain:
            raise _application(WRONG_DOMAIN)
        if self.program != program:
            raise _application(WRONG_PROGRAM)
        if self.market != market:
            raise _application(WRONG_MARKET)


def decode_envelope(data: bytes) -> PaxaiEnvelope:
    if type(data) is not bytes:
        raise ValueError("invalid envelope bytes")
    if len(data) > ENVELOPE_MAX_BYTES:
        raise _application(CAPACITY)
    reader = _Reader(data, _application(NON_CANONICAL))
    if reader.take(6) != b"PAXAI1":
        raise _application(NON_CANONICAL)
    if reader.u16() != 1:
        raise _application(BAD_VERSION)
    selector = reader.u16()
    if selector not in OPERATIONS:
        raise _application(UNKNOWN_OPERATION)
    chain, program, market, actor = (reader.identifier() for _ in range(4))
    epoch, config = reader.u64(), reader.u64()
    roster = reader.take(32)
    sequence, expiry = reader.u64(), reader.u64()
    request = reader.identifier()
    length = reader.u32()
    if length > PAYLOAD_MAX_BYTES:
        raise _application(CAPACITY)
    payload = reader.take(length)
    authentication = reader.u8()
    if authentication == 0:
        delegate = None
    elif authentication == 1:
        delegate = (reader.take(32), reader.take(64))
    else:
        raise _application(NON_CANONICAL)
    reader.finish()
    envelope = PaxaiEnvelope(selector, chain, program, market, actor, epoch, config,
                             None if roster == bytes(32) else roster, sequence, expiry, request, payload, delegate)
    envelope.validate()
    return envelope


@dataclass(frozen=True)
class SnapshotBinding:
    chain: bytes
    program: bytes
    market: bytes
    observed_sequence: int
    execution_height: int
    batch_id: bytes
    native_state_root: bytes
    revision: int
    state_digest: bytes
    epoch: int | None
    config: int
    policy: bytes
    roster: bytes | None
    checkpoint: bytes
    settlement: bytes | None
    rank: int
    publication_time_ms: int

    def prefix(self) -> bytes:
        return (struct.pack(">H", 1) + self.chain + self.program + self.market
                + struct.pack(">QQ", self.observed_sequence, self.execution_height) + self.batch_id
                + self.native_state_root + struct.pack(">Q", self.revision) + self.state_digest
                + (b"\x00" if self.epoch is None else b"\x01" + struct.pack(">Q", self.epoch))
                + struct.pack(">Q", self.config) + self.policy
                + (b"\x00" if self.roster is None else b"\x01" + self.roster))

    def snapshot_id(self) -> bytes:
        return sha256(_VIEW_DOMAIN + self.prefix()).digest()

    def encode(self) -> bytes:
        if self.revision == 0 or self.rank > FINALIZED_RANK:
            raise _application(NON_CANONICAL)
        return (self.prefix() + self.checkpoint
                + (b"\x00" if self.settlement is None else b"\x01" + self.settlement)
                + struct.pack(">BQ", self.rank, self.publication_time_ms) + bytes(8))

    def require_checkpoint(self, verification: CheckpointVerification) -> None:
        if verification.level not in ("checkpoint-finalised", "settlement-anchored"):
            raise AiMarketError("FinalityUnavailable")
        header = verification.header
        if (verification.checkpoint_id != self.checkpoint or header.resulting_state_root != self.native_state_root
                or not header.first_sequence <= self.observed_sequence <= header.last_sequence):
            raise AiMarketError("BindingMismatch")


@dataclass(frozen=True)
class Freshness:
    label: FreshnessLabel
    lag: int | None = None

    @staticmethod
    def of(execution_height: int, authority_height: int | None) -> Freshness:
        if authority_height is None:
            return Freshness(FreshnessLabel.UNKNOWN)
        lag = max(authority_height - execution_height, 0)
        if lag > AUTHORITY_FRESHNESS_HEIGHTS:
            return Freshness(FreshnessLabel.STALE, lag)
        return Freshness(FreshnessLabel.CURRENT)


def _require_current(execution_height: int, authority_height: int) -> None:
    _unsigned(authority_height, 64, "authority height")
    freshness = Freshness.of(execution_height, authority_height)
    if freshness.label is FreshnessLabel.STALE:
        raise AiMarketError("StaleAuthority", freshness.lag)


@dataclass(frozen=True)
class Score:
    status: ScoreStatus
    epoch: int | None
    ppm: int | None


@dataclass(frozen=True)
class Reward:
    status: Availability
    asset: bytes | None
    earned: int | None
    claimed: int | None


@dataclass(frozen=True)
class ParticipantRow:
    kind: ParticipantKind
    id: bytes
    owner: bytes
    generation: int
    identity_state: int
    frozen_member: bool
    frozen_generation: int | None
    eligibility: int
    metadata: bytes | None
    metadata_revision: int
    score: Score
    reward: Reward
    history_status: Availability
    history: bytes | None

    @property
    def key(self) -> tuple[int, bytes]:
        return (int(self.kind), self.id)

    @property
    def active(self) -> bool:
        return bool(self.eligibility & ELIGIBLE_SERVING)


@dataclass(frozen=True)
class OperationRequest:
    operation: int
    payload: bytes
    actor: bytes
    sequence: int
    expiry: int
    request: bytes
    roster_bound: bool
    asset: bytes | None = None
    required_rank: int = FINALIZED_RANK


@dataclass(frozen=True)
class NativeTerms:
    network_id: int
    actor_did: bytes
    owner_public_key: bytes
    account_sequence: int
    idempotency_key: bytes
    not_before: int
    not_after: int
    fee_limit: int
    capabilities: bytes
    access_declaration: bytes
    response_capacity: int
    resources: tuple[int, int, int, int, int, int, int]


@dataclass(frozen=True)
class Effect:
    selector: int
    amount: int | None = None
    expected_refunded: int | None = None
    asset: bytes | None = None
    payee: bytes | None = None
    worker: bytes | None = None


@dataclass(frozen=True)
class ApprovalTerms:
    action: int
    chain: bytes
    program: bytes
    market: bytes
    actor: bytes
    epoch: int
    config: int
    roster: bytes | None
    policy: bytes
    snapshot: bytes
    effect: Effect
    capabilities: bytes
    access_declaration: bytes
    response_capacity: int
    resources: tuple[int, ...]
    fee_limit: int
    not_before: int
    not_after: int
    expiry: int
    idempotency_key: bytes
    authority: bytes
    commitment: bytes


def first_difference(actual: ApprovalTerms, approved: ApprovalTerms) -> str | None:
    effect, other = actual.effect, approved.effect
    checks = (
        ("action", actual.action == approved.action and effect.selector == other.selector),
        ("chain", actual.chain == approved.chain),
        ("program", actual.program == approved.program),
        ("market", actual.market == approved.market),
        ("actor", actual.actor == approved.actor),
        ("epoch", actual.epoch == approved.epoch),
        ("config", actual.config == approved.config),
        ("roster", actual.roster == approved.roster),
        ("policy", actual.policy == approved.policy),
        ("snapshot", actual.snapshot == approved.snapshot),
        ("amount", effect.amount == other.amount and effect.expected_refunded == other.expected_refunded),
        ("asset", effect.asset == other.asset),
        ("payee", effect.payee == other.payee),
        ("worker", effect.worker == other.worker),
        ("capabilities", actual.capabilities == approved.capabilities),
        ("access_declaration", actual.access_declaration == approved.access_declaration),
        ("response_capacity", actual.response_capacity == approved.response_capacity),
        ("resources", tuple(actual.resources) == tuple(approved.resources)),
        ("fee_limit", actual.fee_limit == approved.fee_limit),
        ("validity", actual.not_before == approved.not_before and actual.not_after == approved.not_after),
        ("expiry", actual.expiry == approved.expiry),
        ("idempotency_key", actual.idempotency_key == approved.idempotency_key),
        ("authority", actual.authority == approved.authority),
        ("commitment", actual.commitment == approved.commitment),
    )
    return next((name for name, equal in checks if not equal), None)


def _effect_of(envelope: PaxaiEnvelope, asset: bytes | None) -> Effect:
    if envelope.selector not in (FUND, CLAIM, REFUND_FREE):
        return Effect(envelope.selector)
    reader = _Reader(envelope.payload, _application(NON_CANONICAL))
    if envelope.selector == FUND:
        amount, recipient, version, consent = reader.u128(), reader.identifier(), reader.u64(), reader.u8()
        reader.finish()
        if consent > 1:
            raise _application(NON_CANONICAL)
        if version != FUNDING_POLICY_VERSION:
            raise _application(F06_FUNDING_POLICY_MISMATCH)
        if not consent:
            raise _application(F06_CONTRIBUTION_CONSENT_REQUIRED)
        if amount == 0:
            raise _application(F06_INVALID_AMOUNT)
        return Effect(FUND, amount=amount, asset=asset, payee=recipient)
    if envelope.selector == CLAIM:
        worker, recipient, amount = reader.identifier(), reader.identifier(), reader.u128()
        reader.finish()
        if amount == 0:
            raise _application(F06_NOTHING_TO_CLAIM)
        return Effect(CLAIM, amount=amount, asset=asset, payee=recipient, worker=worker)
    expected, amount, recipient = reader.u128(), reader.u128(), reader.identifier()
    reader.finish()
    if amount == 0:
        raise _application(F06_INVALID_AMOUNT)
    return Effect(REFUND_FREE, amount=amount, expected_refunded=expected, asset=asset, payee=recipient)


def _unsigned_activity(terms: NativeTerms, payload: bytes) -> bytes:
    _unsigned(terms.account_sequence, 64, "account sequence")
    _unsigned(terms.not_before, 64, "validity")
    _unsigned(terms.not_after, 64, "validity")
    _unsigned(terms.fee_limit, 128, "fee limit")
    if (type(terms.network_id) is not int or not 0 < terms.network_id < 1 << 32
            or type(terms.actor_did) is not bytes or not 0 < len(terms.actor_did) <= 255
            or terms.not_after < terms.not_before
            or type(terms.idempotency_key) is not bytes or len(terms.idempotency_key) != 32):
        raise ValueError("invalid native terms")
    _bytes32(terms.owner_public_key, "owner public key")
    return (struct.pack(">HHB", NATIVE_CALL_PROTOCOL_VERSION, 0x1001, 11)
            + b"\x01" + struct.pack(">H", NATIVE_CALL_PROTOCOL_VERSION)
            + b"\x02" + struct.pack(">I", terms.network_id)
            + b"\x03" + struct.pack(">HH", PROGRAMS_MODULE, PROGRAM_CALL_ORDINAL)
            + b"\x04" + struct.pack(">I", len(terms.actor_did)) + terms.actor_did
            + b"\x05" + struct.pack(">I", 32) + terms.owner_public_key
            + b"\x06" + struct.pack(">Q", terms.account_sequence)
            + b"\x07" + struct.pack(">QQ", terms.not_before, terms.not_after)
            + b"\x08" + struct.pack(">I", 32) + terms.idempotency_key
            + b"\x09" + terms.fee_limit.to_bytes(16, "big")
            + b"\x0a" + struct.pack(">I", 32) + sha256(_PAYLOAD_DOMAIN + payload).digest()
            + b"\x0b" + struct.pack(">I", len(payload)) + payload)


def _attach_signature(unsigned: bytes, signature: bytes) -> bytes:
    return unsigned[:4] + b"\x0c" + unsigned[5:] + b"\x0c" + struct.pack(">I", len(signature)) + signature


@dataclass(frozen=True)
class MarketSnapshot:
    snapshot_id: bytes
    projection: ProjectionState
    binding: SnapshotBinding
    components: tuple[Availability, ...]
    source_activity: bytes
    freshness: Freshness

    def prepare(self, authority_height: int, request: OperationRequest, terms: NativeTerms) -> PreparedOperation:
        binding = self.binding
        _require_current(binding.execution_height, authority_height)
        if type(request.required_rank) is not int or not FINALIZED_RANK <= request.required_rank <= SETTLEMENT_RANK:
            raise _application(NON_CANONICAL)
        if request.required_rank > binding.rank:
            raise AiMarketError("FinalityUnavailable")
        operation = OPERATIONS.get(request.operation)
        if operation is None:
            raise _application(UNKNOWN_OPERATION)
        if not operation.mutation:
            raise AiMarketError("NotMutation")
        if request.operation in (FUND, CLAIM, REFUND_FREE):
            _bytes32(request.asset, "asset")
        if request.roster_bound:
            if binding.epoch is None or binding.roster is None:
                raise _application(WRONG_ROSTER)
            epoch, roster = binding.epoch, binding.roster
        else:
            epoch, roster = 0, None
        envelope = PaxaiEnvelope(request.operation, binding.chain, binding.program, binding.market, request.actor,
                                 epoch, binding.config, roster, request.sequence, request.expiry, request.request,
                                 request.payload)
        calldata = envelope.encode()
        intent = envelope.request_digest()
        effect = _effect_of(envelope, request.asset)
        try:
            call = encode_native_program_call(NativeProgramCall(
                binding.program, GUEST_ABI, ENTRYPOINT, calldata, terms.capabilities, terms.access_declaration,
                terms.response_capacity, terms.resources))
        except ValueError as error:
            raise AiMarketError("NativeCall", str(error)) from None
        return PreparedOperation(_unsigned_activity(terms, call), self.binding, self.snapshot_id, intent, effect)


@dataclass(frozen=True)
class PreparedOperation:
    canonical: bytes
    binding: SnapshotBinding
    snapshot_id: bytes
    intent: bytes
    effect: Effect

    @property
    def expected_revision(self) -> int:
        return self.binding.revision

    def review(self) -> Review:
        try:
            activity = _signed_call_envelope(_attach_signature(self.canonical, bytes(64)))
        except ValueError:
            raise _integrity() from None
        try:
            call = decode_native_program_call(activity.payload)
        except ValueError:
            raise AiMarketError("NotNativeProgramCall") from None
        if (activity.protocol_version != NATIVE_CALL_PROTOCOL_VERSION or call.guest_abi != GUEST_ABI
                or call.entrypoint != ENTRYPOINT or call.program_id != self.binding.program):
            raise AiMarketError("NotNativeProgramCall")
        envelope = decode_envelope(call.calldata)
        envelope.check_domain(self.binding.chain, self.binding.program, self.binding.market)
        if envelope.request_digest() != self.intent or _effect_of(envelope, self.effect.asset) != self.effect:
            raise _integrity()
        terms = ApprovalTerms(
            envelope.selector, envelope.chain, envelope.program, envelope.market, envelope.actor, envelope.epoch,
            envelope.config, envelope.roster, self.binding.policy, self.snapshot_id, self.effect, call.capabilities,
            call.access_declaration, call.response_capacity, tuple(call.resources), activity.fee_limit,
            activity.not_before, activity.not_after, envelope.expiry, activity.idempotency, activity.public_key,
            activity.signature_digest)
        return Review(self, terms)


class Ed25519Signer(Protocol):
    def public_key(self) -> bytes: ...
    def sign(self, digest: bytes) -> bytes: ...


@dataclass(frozen=True)
class Review:
    prepared: PreparedOperation
    terms: ApprovalTerms

    def approve(self, approved: ApprovalTerms, key: bytes) -> Approval:
        field = first_difference(self.terms, approved)
        if field is not None:
            raise AiMarketError("ReviewMismatch", field)
        if key != self.terms.authority:
            raise AiMarketError("UnauthorizedKey")
        return Approval(self, key)


@dataclass(frozen=True)
class Approval:
    review: Review
    key: bytes

    def sign(self, signer: Ed25519Signer, signatures: LocalSignatureVerifier, authority_height: int) -> OperationRecord:
        prepared = self.review.prepared
        _require_current(prepared.binding.execution_height, authority_height)
        if signer.public_key() != self.key:
            raise AiMarketError("UnauthorizedKey")
        digest = self.review.terms.commitment
        signature = signer.sign(digest)
        if type(signature) is not bytes or len(signature) != 64 or not signatures.verify_ed25519(self.key, signature, digest):
            raise AiMarketError("Signature")
        return OperationRecord.signed(_attach_signature(prepared.canonical, signature), self.key, prepared.intent, signatures)


@dataclass(frozen=True)
class _SignedIdentity:
    activity_id: bytes
    idempotency_key: bytes
    protocol_version: int
    network_id: int
    not_after: int


def _signed_identity(signed_bytes: bytes, key: bytes, signatures: LocalSignatureVerifier) -> _SignedIdentity:
    try:
        activity = _signed_call_envelope(signed_bytes)
    except ValueError:
        raise AiMarketError("CorruptRecord") from None
    if (activity.public_key != key or len(activity.signature) != 64
            or not signatures.verify_ed25519(key, activity.signature, activity.signature_digest)):
        raise AiMarketError("CorruptRecord")
    return _SignedIdentity(sha256(_ACTIVITY_DOMAIN + signed_bytes).digest(), activity.idempotency,
                           activity.protocol_version, activity.network_id, activity.not_after)


def _put_optional(value: bytes | None) -> bytes:
    return b"\x00" if value is None else b"\x01" + value


@dataclass
class OperationRecord:
    state: SdkState
    domain: DomainStatus
    protocol_version: int
    network_id: int
    activity_id: bytes
    idempotency_key: bytes
    intent: bytes
    signer_public_key: bytes
    not_after: int
    attempt: int
    result_code: int | None
    global_sequence: int | None
    checkpoint: bytes | None
    signed_bytes: bytes

    @classmethod
    def signed(cls, signed_bytes: bytes, key: bytes, intent: bytes, signatures: LocalSignatureVerifier) -> OperationRecord:
        if type(intent) is not bytes or len(intent) != 32:
            raise ValueError("invalid intent")
        identity = _signed_identity(signed_bytes, key, signatures)
        return cls(SdkState.SIGNED, DomainStatus.PENDING, identity.protocol_version, identity.network_id,
                   identity.activity_id, identity.idempotency_key, intent, key, identity.not_after, 0, None, None, None,
                   signed_bytes)

    def encode(self) -> bytes:
        return (_RECORD_MAGIC + struct.pack(">BBHI", self.state, self.domain, self.protocol_version, self.network_id)
                + self.activity_id + self.idempotency_key + self.intent + self.signer_public_key
                + struct.pack(">QI", self.not_after, self.attempt)
                + _put_optional(None if self.result_code is None else struct.pack(">i", self.result_code))
                + _put_optional(None if self.global_sequence is None else struct.pack(">Q", self.global_sequence))
                + _put_optional(self.checkpoint)
                + struct.pack(">I", len(self.signed_bytes)) + self.signed_bytes)

    @classmethod
    def decode(cls, data: bytes, signatures: LocalSignatureVerifier) -> OperationRecord:
        corrupt = AiMarketError("CorruptRecord")
        reader = _Reader(data, corrupt)

        def optional(length: int) -> bytes | None:
            flag = reader.u8()
            if flag > 1:
                raise corrupt
            return reader.take(length) if flag else None

        if reader.take(8) != _RECORD_MAGIC:
            raise corrupt
        try:
            state, domain = SdkState(reader.u8()), DomainStatus(reader.u8())
        except ValueError:
            raise corrupt from None
        protocol_version, network_id = reader.u16(), reader.u32()
        activity_id, idempotency_key, intent, key = (reader.take(32) for _ in range(4))
        not_after, attempt = reader.u64(), reader.u32()
        result = optional(4)
        sequence = optional(8)
        checkpoint = optional(32)
        signed_bytes = reader.take(reader.u32())
        reader.finish()
        record = cls(state, domain, protocol_version, network_id, activity_id, idempotency_key, intent, key, not_after,
                     attempt, None if result is None else struct.unpack(">i", result)[0],
                     None if sequence is None else int.from_bytes(sequence, "big"), checkpoint, signed_bytes)
        identity = _signed_identity(signed_bytes, key, signatures)
        if (identity != _SignedIdentity(activity_id, idempotency_key, protocol_version, network_id, not_after)
                or not record.consistent()):
            raise corrupt
        return record

    def consistent(self) -> bool:
        pristine = self.result_code is None and self.global_sequence is None and self.checkpoint is None
        executed = self.result_code == 0 and self.global_sequence is not None
        if self.state in (SdkState.PREPARED, SdkState.REVIEWED):
            return False
        if self.state is SdkState.SIGNED:
            return pristine and self.attempt == 0
        if self.state in (SdkState.SUBMITTING, SdkState.PENDING, SdkState.UNKNOWN):
            return pristine and self.attempt > 0
        if self.state is SdkState.EXECUTED:
            return executed and self.checkpoint is None and self.attempt > 0
        if self.state is SdkState.FINALIZED:
            return executed and self.checkpoint is not None and self.attempt > 0
        return self.checkpoint is None and (self.global_sequence is None if self.result_code is None else self.result_code != 0)


def _require(record: OperationRecord, *allowed: SdkState) -> None:
    if record.state not in allowed:
        raise AiMarketError("InvalidTransition", record.state)


def _move(record: OperationRecord, target: SdkState) -> None:
    if record.state not in SDK_TRANSITIONS[target]:
        raise AiMarketError("InvalidTransition", record.state)
    record.state = target


class OperationJournal:
    def __init__(self, directory: Path, signatures: LocalSignatureVerifier) -> None:
        directory.mkdir(parents=True, exist_ok=True)
        self._directory = directory
        self._signatures = signatures

    def record_path(self, activity_id: bytes) -> Path:
        return self._directory / (activity_id.hex() + _RECORD_EXTENSION)

    def persist(self, record: OperationRecord) -> None:
        data = record.encode()
        path = self.record_path(record.activity_id)
        partial = path.with_suffix(".partial")
        descriptor = os.open(partial, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        try:
            os.write(descriptor, data)
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
        os.replace(partial, path)
        directory = os.open(self._directory, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)

    def load(self, activity_id: bytes) -> OperationRecord:
        record = OperationRecord.decode(self.record_path(activity_id).read_bytes(), self._signatures)
        if record.activity_id != activity_id:
            raise AiMarketError("CorruptRecord")
        if record.state is SdkState.SUBMITTING:
            _move(record, SdkState.UNKNOWN)
            self.persist(record)
        return record

    def record_signed(self, record: OperationRecord) -> None:
        _require(record, SdkState.SIGNED)
        self.persist(record)

    def submit(self, record: OperationRecord, operations: ProgramOperations) -> SdkState:
        _require(record, SdkState.SIGNED)
        return self._deliver(record, operations)

    def resend_exact(self, record: OperationRecord, operations: ProgramOperations) -> SdkState:
        _require(record, SdkState.UNKNOWN)
        return self._deliver(record, operations)

    def _deliver(self, record: OperationRecord, operations: ProgramOperations) -> SdkState:
        prior_state, prior_attempt = record.state, record.attempt
        if record.attempt >= 0xFFFF_FFFF:
            raise AiMarketError("CorruptRecord")
        _move(record, SdkState.SUBMITTING)
        record.attempt += 1
        self.persist(record)
        try:
            activity = _signed_call_envelope(record.signed_bytes)
            request = NativeProgramRequest(decode_native_program_call(activity.payload), activity.fee_limit,
                                           record.signed_bytes)
            outcome = operations.submit(request, IdempotencyKey(record.idempotency_key.hex()))
        except PlatformSdkError as error:
            if record.attempt == 1 and error.code in _TERMINAL_REFUSALS:
                code = error.protocol_result_code
                record.result_code = code if type(code) is int and code != 0 and -(1 << 31) <= code < 1 << 31 else None
                _move(record, SdkState.FAILED)
            else:
                _move(record, SdkState.UNKNOWN)
        except (ValueError, TypeError):
            record.state, record.attempt = prior_state, prior_attempt
            self.persist(record)
            raise
        else:
            self._acknowledged(record, outcome)
        self.persist(record)
        return record.state

    def _acknowledged(self, record: OperationRecord, outcome: Mapping[str, object]) -> None:
        if outcome.get("activity_id") != record.activity_id.hex():
            _move(record, SdkState.UNKNOWN)
            return
        state = outcome.get("state")
        code, sequence = outcome.get("result_code"), outcome.get("global_sequence")
        if (state not in ("executed", "refused") or type(code) is not int or not -(1 << 31) <= code < 1 << 31
                or (code == 0) != (state == "executed") or not isinstance(sequence, str) or not sequence.isdigit()):
            _move(record, SdkState.UNKNOWN)
            return
        _move(record, SdkState.PENDING)
        self.persist(record)
        record.result_code, record.global_sequence = code, int(sequence)
        _move(record, SdkState.EXECUTED if code == 0 else SdkState.FAILED)

    def resolve(self, record: OperationRecord, receipt: bytes | None, authorized: AuthorizedReceiptBatch) -> SdkState:
        _require(record, SdkState.PENDING, SdkState.UNKNOWN)
        if receipt is None:
            return record.state
        verified = verify_receipt_outcome(receipt, authorized, self._signatures,
                                          protocol_version=record.protocol_version).receipt
        if verified.activity_id != record.activity_id or verified.module_id != PROGRAMS_MODULE:
            raise AiMarketError("ReceiptMismatch")
        record.result_code, record.global_sequence = verified.result_code, verified.global_sequence
        _move(record, SdkState.EXECUTED if verified.result_code == 0 else SdkState.FAILED)
        self.persist(record)
        return record.state

    def finalize(self, record: OperationRecord, verification: CheckpointVerification) -> SdkState:
        _require(record, SdkState.EXECUTED)
        header = verification.header
        if (header.network_id != record.network_id or record.global_sequence is None
                or not header.first_sequence <= record.global_sequence <= header.last_sequence):
            raise AiMarketError("FinalityMismatch")
        record.checkpoint = verification.checkpoint_id
        _move(record, SdkState.FINALIZED)
        self.persist(record)
        return record.state

    def expire(self, record: OperationRecord, now: int) -> SdkState:
        _require(record, SdkState.SIGNED)
        if _unsigned(now, 64, "time") > record.not_after:
            _move(record, SdkState.FAILED)
            self.persist(record)
        return record.state


def _strict_json(encoded: bytes) -> object:
    def reject_float(value: str) -> object:
        raise ValueError(value)

    try:
        return json.loads(encoded.decode("utf-8"), parse_constant=_reject_constant, parse_float=reject_float,
                          object_pairs_hook=_unique_object)
    except (UnicodeDecodeError, ValueError, RecursionError):
        raise _integrity() from None


def _object(value: object, keys: tuple[str, ...]) -> dict[str, object]:
    if not isinstance(value, dict) or set(value) != set(keys):
        raise _integrity()
    return value


def _hex_id(value: object) -> bytes:
    if (not isinstance(value, str) or len(value) != 66 or not value.startswith("0x")
            or any(character not in _HEX for character in value[2:]) or value == "0x" + "0" * 64):
        raise _integrity()
    return bytes.fromhex(value[2:])


def _decimal(value: object, bits: int = 64) -> int:
    if (not isinstance(value, str) or not value or not value.isascii() or not value.isdigit()
            or (len(value) > 1 and value[0] == "0") or int(value) >= 1 << bits):
        raise _integrity()
    return int(value)


def _small(value: object, maximum: int) -> int:
    if type(value) is not int or not 0 <= value <= maximum:
        raise _integrity()
    return value


def _nullable(value: object, decode: object) -> object:
    return None if value is None else decode(value)  # type: ignore[operator]


_BINDING_KEYS = ("chain", "program", "market", "observed_sequence", "execution_height", "batch_id", "native_state_root",
                 "revision", "state_digest", "epoch", "config", "policy", "roster", "checkpoint", "settlement", "rank",
                 "publication_time_ms")


def _binding(value: object, market: bytes) -> SnapshotBinding:
    fields = _object(value, _BINDING_KEYS)
    binding = SnapshotBinding(
        _hex_id(fields["chain"]), _hex_id(fields["program"]), _hex_id(fields["market"]),
        _decimal(fields["observed_sequence"]), _decimal(fields["execution_height"]), _hex_id(fields["batch_id"]),
        _hex_id(fields["native_state_root"]), _decimal(fields["revision"]), _hex_id(fields["state_digest"]),
        _nullable(fields["epoch"], _decimal), _decimal(fields["config"]), _hex_id(fields["policy"]),  # type: ignore[arg-type]
        _nullable(fields["roster"], _hex_id), _hex_id(fields["checkpoint"]),  # type: ignore[arg-type]
        _nullable(fields["settlement"], _hex_id), _small(fields["rank"], 255),  # type: ignore[arg-type]
        _decimal(fields["publication_time_ms"]))
    if binding.config == 0 or binding.revision == 0 or binding.rank > FINALIZED_RANK:
        raise _integrity()
    if binding.market != market:
        raise AiMarketError("BindingMismatch")
    if binding.rank != FINALIZED_RANK:
        raise AiMarketError("FinalityUnavailable")
    return binding


def _bound_view(fields: Mapping[str, object], market: bytes, pinned: bytes | None) -> tuple[bytes, SnapshotBinding]:
    snapshot_id = _hex_id(fields["snapshot_id"])
    binding = _binding(fields["binding"], market)
    if binding.snapshot_id() != snapshot_id:
        raise AiMarketError("BindingMismatch")
    if pinned is not None and snapshot_id != pinned:
        raise AiMarketError("SnapshotConflict")
    return snapshot_id, binding


def _components(value: object) -> tuple[Availability, ...]:
    fields = _object(value, _FEATURES)
    return tuple(Availability.from_wire(fields[name]) for name in _FEATURES)  # type: ignore[misc]


def _freshness(value: object) -> Freshness:
    if not isinstance(value, dict):
        raise _integrity()
    label = FreshnessLabel.from_wire(value.get("label"))
    if label is not FreshnessLabel.STALE:
        _object(value, ("label",))
        return Freshness(label)  # type: ignore[arg-type]
    lag = _decimal(_object(value, ("label", "lag"))["lag"])
    if lag <= AUTHORITY_FRESHNESS_HEIGHTS:
        raise _integrity()
    return Freshness(FreshnessLabel.STALE, lag)


def _score(value: object) -> Score:
    fields = _object(value, ("status", "epoch", "ppm"))
    status = ScoreStatus.from_wire(fields["status"])
    epoch = _nullable(fields["epoch"], _decimal)
    ppm = None if fields["ppm"] is None else _small(fields["ppm"], 1_000_000)
    if (status is ScoreStatus.PRESENT) != (ppm is not None) or (ppm is not None and epoch is None):
        raise _integrity()
    return Score(status, epoch, ppm)  # type: ignore[arg-type]


def _reward(value: object) -> Reward:
    fields = _object(value, ("status", "asset", "earned", "claimed"))
    status = Availability.from_wire(fields["status"])
    if status is not Availability.AVAILABLE:
        if fields["asset"] is not None or fields["earned"] is not None or fields["claimed"] is not None:
            raise _integrity()
        return Reward(status, None, None, None)  # type: ignore[arg-type]
    earned, claimed = _decimal(fields["earned"], 128), _decimal(fields["claimed"], 128)
    if claimed > earned:
        raise _integrity()
    return Reward(Availability.AVAILABLE, _hex_id(fields["asset"]), earned, claimed)


_ROW_KEYS = ("kind", "id", "owner", "generation", "identity_state", "frozen_member", "frozen_generation", "eligibility",
             "metadata", "metadata_revision", "score", "reward", "history")


def _row(value: object) -> ParticipantRow:
    fields = _object(value, _ROW_KEYS)
    history = _object(fields["history"], ("status", "digest"))
    history_status = Availability.from_wire(history["status"])
    digest = _nullable(history["digest"], _hex_id)
    if (history_status is Availability.AVAILABLE) != (digest is not None) or type(fields["frozen_member"]) is not bool:
        raise _integrity()
    return ParticipantRow(
        ParticipantKind.from_wire(fields["kind"]), _hex_id(fields["id"]), _hex_id(fields["owner"]),  # type: ignore[arg-type]
        _decimal(fields["generation"]), _small(fields["identity_state"], 255), fields["frozen_member"],
        _nullable(fields["frozen_generation"], _decimal), _small(fields["eligibility"], 255),  # type: ignore[arg-type]
        _nullable(fields["metadata"], _hex_id), _decimal(fields["metadata_revision"]),  # type: ignore[arg-type]
        _score(fields["score"]), _reward(fields["reward"]), history_status, digest)  # type: ignore[arg-type]


@dataclass(frozen=True)
class ParticipantPage:
    market: bytes
    snapshot_id: bytes
    binding: SnapshotBinding
    components: tuple[Availability, ...]
    rows: tuple[ParticipantRow, ...]
    cursor: str | None
    kind: KindFilter
    active_only: bool
    limit: int


@dataclass(frozen=True)
class EpochEntry:
    epoch: int
    status: EpochStatus
    snapshot_id: bytes | None


@dataclass(frozen=True)
class EpochPage:
    component: Availability
    entries: tuple[EpochEntry, ...]


@dataclass(frozen=True)
class _CursorScope:
    market: bytes
    snapshot_id: bytes
    binding: SnapshotBinding
    kind: KindFilter
    active_only: bool
    last: tuple[int, bytes]


_TYPED_VIEW_ERRORS: Mapping[str, str] = {
    "binding-mismatch": "BindingMismatch",
    "finality-unavailable": "FinalityUnavailable",
    "snapshot-conflict": "SnapshotConflict",
    "integrity-failure": "IntegrityFailure",
    "cursor-expired": "CursorExpired",
    "cursor-mismatch": "CursorMismatch",
    "response-too-large": "ResponseTooLarge",
}
_REFUSED_VIEW_ERRORS = frozenset({
    "invalid-encoding", "unsupported-version", "wrong-domain", "projection-unavailable", "capacity-exceeded",
    "access-refused", "rate-limited", "endpoint-disabled", "quarantined", "rollback-refused", "projection-store",
    "api_key_required", "persistence_unavailable", "method_not_allowed",
})


def _view_error(value: object) -> AiMarketError:
    if not isinstance(value, dict):
        return _integrity()
    code = value.get("code")
    if code == "authority-stale" and set(value) == {"code", "lag"}:
        try:
            return AiMarketError("StaleAuthority", _decimal(value["lag"]))
        except AiMarketError as error:
            return error
    if code == "snapshot-pruned" and set(value) == {"code", "oldest"}:
        oldest = value["oldest"]
        if oldest is None:
            return AiMarketError("ViewRefused", (code, None))
        try:
            fields = _object(oldest, ("snapshot_id", "observed_sequence"))
            return AiMarketError("ViewRefused", (code, (_hex_id(fields["snapshot_id"]), _decimal(fields["observed_sequence"]))))
        except AiMarketError as error:
            return error
    if set(value) != {"code"}:
        return _integrity()
    if code in _TYPED_VIEW_ERRORS:
        return AiMarketError(_TYPED_VIEW_ERRORS[code])  # type: ignore[index]
    if code in _REFUSED_VIEW_ERRORS:
        return AiMarketError("ViewRefused", (code, None))
    return _integrity()


def _limit(value: object) -> int:
    if type(value) is not int or not 1 <= value <= MAX_PAGE_ROWS:
        raise ValueError("invalid page limit")
    return value


class AiMarketViews:
    def __init__(self, endpoint: str, *, credential: LayerXKeyCredential | None = None, timeout: float = 30.0) -> None:
        if not isinstance(timeout, (int, float)) or isinstance(timeout, bool) or timeout <= 0:
            raise ValueError("invalid timeout")
        self._endpoint = _validated_endpoint(endpoint)
        self._credential = credential
        self._timeout = float(timeout)
        self._opener = build_opener(_NoRedirect())
        self._cursors: dict[str, _CursorScope] = {}

    def snapshot(self, market: bytes, *, snapshot: bytes | None = None) -> MarketSnapshot:
        _bytes32(market, "market")
        if snapshot is not None:
            _bytes32(snapshot, "snapshot")
        query = [] if snapshot is None else [("snapshot", snapshot.hex())]
        fields = _object(self._get(market, "snapshot", query),
                         ("snapshot_id", "projection", "binding", "components", "source_activity", "freshness"))
        snapshot_id, binding = _bound_view(fields, market, snapshot)
        projection = ProjectionState.from_wire(fields["projection"])
        if projection not in (ProjectionState.FINALIZED_PUBLISHABLE, ProjectionState.ARCHIVED):
            raise AiMarketError("FinalityUnavailable")
        return MarketSnapshot(snapshot_id, projection, binding, _components(fields["components"]),  # type: ignore[arg-type]
                              _hex_id(fields["source_activity"]), _freshness(fields["freshness"]))

    def participants(self, market: bytes, *, snapshot: bytes | None = None, kind: KindFilter = KindFilter.ALL,
                     active_only: bool = False, limit: int = DEFAULT_PAGE_ROWS, cursor: str | None = None) -> ParticipantPage:
        _bytes32(market, "market")
        if snapshot is not None:
            _bytes32(snapshot, "snapshot")
        if not isinstance(kind, KindFilter) or type(active_only) is not bool:
            raise ValueError("invalid participant filter")
        _limit(limit)
        scope = None
        if cursor is not None:
            if not isinstance(cursor, str):
                raise ValueError("invalid cursor")
            if len(cursor) > CURSOR_MAX_BYTES:
                raise AiMarketError("ResponseTooLarge")
            if not cursor or len(cursor) % 2 or any(character not in _HEX for character in cursor):
                raise ValueError("invalid cursor")
            scope = self._cursors.get(cursor)
            if scope is not None:
                if (scope.market != market or scope.kind is not kind or scope.active_only != active_only
                        or snapshot is not None and snapshot != scope.snapshot_id):
                    raise AiMarketError("CursorMismatch")
                snapshot = scope.snapshot_id
        query = [] if snapshot is None else [("snapshot", snapshot.hex())]
        query += [("kind", kind.wire), ("active_only", "true" if active_only else "false"), ("limit", str(limit))]
        if cursor is not None:
            query.append(("cursor", cursor))
        fields = _object(self._get(market, "participants", query),
                         ("snapshot_id", "binding", "components", "rows", "cursor"))
        snapshot_id, binding = _bound_view(fields, market, snapshot)
        if scope is not None and binding != scope.binding:
            raise AiMarketError("SnapshotConflict")
        if not isinstance(fields["rows"], list) or len(fields["rows"]) > limit:
            raise _integrity()
        rows = tuple(_row(value) for value in fields["rows"])
        previous = None if scope is None else scope.last
        for row in rows:
            if ((kind is not KindFilter.ALL and int(row.kind) != int(kind)) or (active_only and not row.active)
                    or previous is not None and row.key <= previous):
                raise _integrity()
            previous = row.key
        issued = fields["cursor"]
        if issued is not None and (not isinstance(issued, str) or not 0 < len(issued) <= CURSOR_MAX_BYTES
                                   or len(issued) % 2 or any(character not in _HEX for character in issued)
                                   or len(rows) != limit):
            raise _integrity()
        if cursor is not None:
            self._cursors.pop(cursor, None)
        if issued is not None and previous is not None:
            self._cursors[issued] = _CursorScope(market, snapshot_id, binding, kind, active_only, previous)
        return ParticipantPage(market, snapshot_id, binding, _components(fields["components"]), rows,
                               issued, kind, active_only, limit)  # type: ignore[arg-type]

    def next_page(self, page: ParticipantPage) -> ParticipantPage | None:
        if page.cursor is None:
            return None
        return self.participants(page.market, kind=page.kind, active_only=page.active_only, limit=page.limit,
                                 cursor=page.cursor)

    def epochs(self, market: bytes, *, start: int = 0, limit: int = DEFAULT_PAGE_ROWS) -> EpochPage:
        _bytes32(market, "market")
        _unsigned(start, 64, "epoch")
        _limit(limit)
        fields = _object(self._get(market, "epochs", [("from", str(start)), ("limit", str(limit))]),
                         ("component", "entries"))
        component = Availability.from_wire(fields["component"])
        values = fields["entries"]
        if (not isinstance(values, list) or len(values) > limit
                or component is not Availability.AVAILABLE and values):
            raise _integrity()
        entries = []
        for offset, value in enumerate(values):
            entry = _object(value, ("epoch", "status", "snapshot_id"))
            status = EpochStatus.from_wire(entry["status"])
            snapshot_id = _nullable(entry["snapshot_id"], _hex_id)
            if _decimal(entry["epoch"]) != start + offset or (status in SOURCELESS_EPOCH_STATUSES) != (snapshot_id is None):
                raise _integrity()
            entries.append(EpochEntry(start + offset, status, snapshot_id))  # type: ignore[arg-type]
        return EpochPage(component, tuple(entries))  # type: ignore[arg-type]

    def _get(self, market: bytes, view: str, query: list[tuple[str, str]]) -> object:
        url = _route_endpoint(self._endpoint, f"{_VIEW_PATH}{market.hex()}/{view}")
        if query:
            url += "?" + "&".join(f"{key}={value}" for key, value in query)
        headers = {"Accept": "application/json", "User-Agent": "layerx-python/0.1.0"}
        if self._credential is not None:
            headers["Authorization"] = self._credential.use()
        request = Request(url, headers=headers, method="GET")
        try:
            with self._opener.open(request, timeout=self._timeout) as response:
                return _view_result(response.status, response.headers.get("Content-Type"),
                                    _bounded_read(response, _VIEW_MAX_RESPONSE_BYTES))
        except HTTPError as error:
            try:
                return _view_result(error.code, error.headers.get("Content-Type"),
                                    _bounded_read(error, _VIEW_MAX_RESPONSE_BYTES))
            finally:
                error.close()
        except (TimeoutError, URLError, OSError, HTTPException):
            raise PlatformSdkError(SdkErrorCode.TRANSPORT_FAILURE, "safe") from None


def _view_result(status: int, content_type: str | None, encoded: bytes) -> object:
    if content_type != "application/json":
        raise _integrity()
    value = _strict_json(encoded)
    if isinstance(value, dict) and value.get("ok") is True and 200 <= status < 300:
        return _object(value, ("ok", "result"))["result"]
    if isinstance(value, dict) and value.get("ok") is False and not 200 <= status < 300:
        raise _view_error(_object(value, ("ok", "error"))["error"])
    raise _integrity()
