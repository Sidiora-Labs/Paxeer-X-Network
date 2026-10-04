from __future__ import annotations

import hashlib
import json
import secrets
import ssl
from collections.abc import Mapping
from dataclasses import dataclass
from http.client import HTTPException
from typing import Generic, Literal, TypedDict, TypeVar, cast
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlparse, urlunparse
from urllib.request import HTTPRedirectHandler, HTTPSHandler, Request, build_opener

from .production import (
    _AGENT_IDEMPOTENT,
    AGENT_OPERATIONS,
    IdempotencyKey,
    PlatformPlane,
    PlatformSdkError,
    ProductionTransport,
    SdkErrorCode,
    SecretBytes,
)
from .generated.client import (
    NativePrepareRequestV1, NativeApprovalDecisionV1, NativePrepareResultV1, NativeApprovalResultV1, NativeApprovalListResultV1,
    encode_native_prepare_request, encode_native_approval_decision, encode_native_approval_get,
    decode_native_prepare_result, decode_native_approval_result, decode_native_approval_list_result,
)
from .program_wire import bind_signed_program_lifecycle
from .native_effect import NativeEffectPrepareRequestV1, encode_native_effect_prepare_request

_MAX_RESPONSE_BYTES = 8 * 1024 * 1024
_MAX_REQUEST_BYTES = 4 * 1024 * 1024
_HEX = frozenset("0123456789abcdef")
_ERROR_CLASS: Mapping[str, SdkErrorCode] = {
    "TransportFailure": SdkErrorCode.TRANSPORT_FAILURE,
    "Deadline": SdkErrorCode.DEADLINE,
    "ProtocolIncompatibility": SdkErrorCode.PROTOCOL_INCOMPATIBILITY,
    "UnavailableCapability": SdkErrorCode.UNAVAILABLE_CAPABILITY,
    "CoreRejection": SdkErrorCode.CORE_REJECTION,
    "VerificationFailure": SdkErrorCode.VERIFICATION_FAILURE,
    "PolicyRefusal": SdkErrorCode.POLICY_REFUSAL,
    "CapabilityRefusal": SdkErrorCode.CAPABILITY_REFUSAL,
    "BudgetRefusal": SdkErrorCode.BUDGET_REFUSAL,
    "RateLimit": SdkErrorCode.RATE_LIMIT,
    "IdempotencyConflict": SdkErrorCode.IDEMPOTENCY_CONFLICT,
    "InternalFault": SdkErrorCode.INTERNAL_FAULT,
}


@dataclass(frozen=True)
class _Route:
    method: str
    path: str
    path_field: str | None = None


_ROUTES: Mapping[str, _Route] = {
    "program.deploy": _Route("POST", "/v1/programs/deploy"),
    "program.upgrade": _Route("POST", "/v1/programs/upgrade"),
    "program.wind-down": _Route("POST", "/v1/programs/wind-down"),
    "program.discover": _Route("GET", "/v1/programs/registry/{program_id}", "program_id"),
    "program.interface": _Route("GET", "/v1/programs/registry/{program_id}/interface", "program_id"),
    "program.simulate": _Route("POST", "/v1/programs/simulate"),
    "program.call": _Route("POST", "/v1/programs/call"),
    "program.receipt": _Route("GET", "/v1/programs/receipts/by-idempotency/{idempotency_key}", "idempotency_key"),
    "program.activity": _Route("GET", "/v1/programs/activities/{activity_id}", "activity_id"),
}


class LayerXKeyCredential:
    __slots__ = ("_key_id", "_secret")

    def __init__(self, key_id: str, secret: SecretBytes) -> None:
        if not 0 < len(key_id) <= 64 or not key_id.isascii() or any(
            not (character.isalnum() or character in "-_") for character in key_id
        ):
            raise _invalid_argument()
        self._key_id = key_id
        self._secret = secret

    def use(self) -> str:
        def authorization(value: memoryview) -> str:
            try:
                secret = bytes(value).decode("ascii")
            except UnicodeDecodeError:
                raise _invalid_argument() from None
            suffix = secret.removeprefix("lxp_live_")
            if len(suffix) != 64 or any(character not in _HEX for character in suffix):
                raise _invalid_argument()
            return f"LayerX-Key {self._key_id}:{secret}"

        return self._secret.use(authorization)

    def __repr__(self) -> str:
        return "LayerXKeyCredential([REDACTED])"

    def __str__(self) -> str:
        return "[REDACTED]"


class AgentHttpTransport(ProductionTransport):
    __slots__ = ("_credential", "_endpoint", "_maximum_response_bytes", "_opener", "_timeout")

    def __init__(
        self,
        endpoint: str,
        *,
        credential: LayerXKeyCredential | None = None,
        timeout: float = 30.0,
        maximum_response_bytes: int = _MAX_RESPONSE_BYTES,
    ) -> None:
        self._endpoint = _validated_endpoint(endpoint)
        if not isinstance(timeout, (int, float)) or isinstance(timeout, bool) or timeout <= 0:
            raise _invalid_argument()
        if not isinstance(maximum_response_bytes, int) or isinstance(maximum_response_bytes, bool) or maximum_response_bytes <= 0 or maximum_response_bytes > _MAX_RESPONSE_BYTES:
            raise _invalid_argument()
        self._credential = credential
        self._timeout = float(timeout)
        self._maximum_response_bytes = maximum_response_bytes
        self._opener = build_opener(_NoRedirect())

    def call(
        self,
        plane: PlatformPlane,
        operation: object,
        request: object,
        idempotency_key: IdempotencyKey | None,
    ) -> object:
        if plane != "agent" or not isinstance(operation, str) or operation not in _ROUTES:
            raise _unavailable_capability()
        if not isinstance(request, Mapping) or any(not isinstance(key, str) for key in request):
            raise _invalid_argument()
        route = _ROUTES[operation]
        path = route.path
        if route.path_field is not None:
            value = request.get(route.path_field)
            if not isinstance(value, str) or not _hex32(value):
                raise _invalid_argument()
            path = path.replace("{" + route.path_field + "}", quote(value, safe=""))
        if operation in {"program.call", "program.deploy", "program.upgrade", "program.wind-down"}:
            if idempotency_key is None or not _hex32(str(idempotency_key)):
                raise _invalid_argument()
        elif idempotency_key is not None:
            raise _invalid_argument()
        if operation in {"program.discover", "program.interface", "program.receipt", "program.activity"} and request.get("requested_verification_level") != "sequencer-signed":
            raise _invalid_argument()
        _require_exact_request(operation, request)
        try:
            if route.method == "POST":
                body = _encode_program_mutation_body(request.get("signed_activity"))
            else:
                body = json.dumps(request, ensure_ascii=True, allow_nan=False, separators=(",", ":")).encode("utf-8")
        except (TypeError, ValueError, OverflowError):
            raise _invalid_argument() from None
        if len(body) > _MAX_REQUEST_BYTES:
            raise _invalid_argument()
        if operation in {"program.deploy", "program.upgrade", "program.wind-down"}:
            ordinal = {"program.deploy": 1, "program.upgrade": 2, "program.wind-down": 7}[operation]
            try:
                bind_signed_program_lifecycle(body, None, ordinal, str(idempotency_key))
            except (TypeError, ValueError, OverflowError):
                raise _invalid_argument() from None
        headers = {
            "Accept": "application/json",
            "Content-Type": "application/octet-stream" if route.method == "POST" else "application/json",
            "Content-Length": str(len(body)),
            "User-Agent": "layerx-python/0.1.0",
        }
        if idempotency_key is not None:
            headers["Idempotency-Key"] = str(idempotency_key)
        if self._credential is not None:
            headers["Authorization"] = self._credential.use()
        outbound = Request(_route_endpoint(self._endpoint, path), data=body, headers=headers, method=route.method)
        try:
            with self._opener.open(outbound, timeout=self._timeout) as response:
                encoded = _bounded_read(response, self._maximum_response_bytes)
                if response.headers.get("Content-Type") != "application/json":
                    raise _decode_failure()
                return _decode_envelope(response.status, encoded, operation)
        except HTTPError as error:
            try:
                encoded = _bounded_read(error, self._maximum_response_bytes)
                if error.headers.get("Content-Type") != "application/json":
                    raise _decode_failure()
                return _decode_envelope(error.code, encoded, operation)
            finally:
                error.close()
        except PlatformSdkError:
            raise
        except (TimeoutError, URLError, OSError, HTTPException):
            raise _transport_failure(operation) from None


_ENVELOPE_VERSION = 1
_ENVELOPE_PATH = "/v1/agent/rpc"
_DAEMON_PATH = "/rpc"
_ENVELOPE_MAX_BODY_BYTES = 1_048_576
_BOOTSTRAP_OPERATIONS = frozenset({"agent.register", "session.open"})
_MAX_U64 = (1 << 64) - 1
_LEVELS: Mapping[str, int] = {
    "Unverified": 0,
    "SequencerSigned": 1,
    "BatchIncluded": 2,
    "StateProven": 3,
    "CheckpointFinalised": 4,
    "SettlementAnchored": 5,
}


class AgentSessionCredential:
    __slots__ = ("_generation", "_session_id", "_tenant", "_token_id")

    def __init__(self, tenant: str, session_id: str, token_id: str, generation: int) -> None:
        if not isinstance(tenant, str) or "\0" in tenant:
            raise _invalid_argument()
        try:
            encoded_tenant = tenant.encode("utf-8")
        except UnicodeEncodeError:
            raise _invalid_argument() from None
        if not 0 < len(encoded_tenant) <= 255:
            raise _invalid_argument()
        if not isinstance(session_id, str) or not _hex32(session_id) or not isinstance(token_id, str) or not _hex32(token_id):
            raise _invalid_argument()
        if type(generation) is not int or not 0 <= generation <= _MAX_U64:
            raise _invalid_argument()
        self._tenant = tenant
        self._session_id = session_id
        self._token_id = token_id
        self._generation = generation

    def coordinates(self) -> dict[str, str]:
        return {
            "tenant": self._tenant,
            "session_id": self._session_id,
            "token_id": self._token_id,
            "generation": str(self._generation),
        }

    def __repr__(self) -> str:
        return "AgentSessionCredential([REDACTED])"

    def __str__(self) -> str:
        return "[REDACTED]"


@dataclass(frozen=True)
class AgentEnvelopeSuccess:
    request_id: str
    value: object
    verification_status: Mapping[str, object]


_NativeValue = TypeVar("_NativeValue")

@dataclass(frozen=True)
class NativeEnvelopeSuccess(Generic[_NativeValue]):
    request_id: str
    value: _NativeValue
    verification_status: Mapping[str, object]


TenantRecoveryReasonV1 = Literal[
    "recovery_pending", "store_unavailable", "store_refused", "budget_state_unverified",
    "receipt_evidence_missing", "durable_recovery_failed", "spend_unreconciled",
    "transport_unavailable", "verified_read_unavailable",
]


class TenantReadinessV1(TypedDict):
    transport_ready: bool
    verified_reads_ready: bool
    writes_admitted: bool
    recovery_reason: TenantRecoveryReasonV1 | None


_TENANT_RECOVERY_REASONS = frozenset({
    "recovery_pending", "store_unavailable", "store_refused", "budget_state_unverified",
    "receipt_evidence_missing", "durable_recovery_failed", "spend_unreconciled",
    "transport_unavailable", "verified_read_unavailable",
})


def decode_tenant_readiness(value: object) -> TenantReadinessV1:
    if not isinstance(value, Mapping) or set(value) != {
        "transport_ready", "verified_reads_ready", "writes_admitted", "recovery_reason",
    }:
        raise _decode_failure()
    transport, reads, writes = value["transport_ready"], value["verified_reads_ready"], value["writes_admitted"]
    reason = value["recovery_reason"]
    if (type(transport) is not bool or type(reads) is not bool or type(writes) is not bool
        or reason is not None and (not isinstance(reason, str) or reason not in _TENANT_RECOVERY_REASONS)
        or writes != (reason is None) or writes and (not transport or not reads) or reads and not transport):
        raise _decode_failure()
    return cast(TenantReadinessV1, dict(value))


def check_tenant_readiness_response(response: AgentEnvelopeSuccess) -> TenantReadinessV1:
    if response.verification_status != {"state": "achieved", "level": "Unverified"}:
        raise _decode_failure(response.request_id)
    try:
        return decode_tenant_readiness(response.value)
    except PlatformSdkError:
        raise _decode_failure(response.request_id) from None


ProofBundleVerificationLevel = Literal["Unverified", "SequencerSigned", "BatchIncluded", "StateProven", "CheckpointFinalised", "SettlementAnchored"]


class ProofBundleRequest(TypedDict):
    target: str
    requested_verification_level: ProofBundleVerificationLevel


class ProofBundleValue(TypedDict):
    target: str
    proofs: list[str]


class ProofBundleRelativeTo(TypedDict):
    batch: str


class ProofBundleFreshness(TypedDict):
    chain_head: str
    latest_sealed_batch: str
    latest_finalised_checkpoint: str
    value_sequence: str
    relative_to: ProofBundleRelativeTo


class ProofBundleRead(TypedDict):
    value: ProofBundleValue
    achieved_verification_level: Literal["BatchIncluded", "StateProven"]
    freshness: ProofBundleFreshness


@dataclass(frozen=True)
class ProofBundleRecord:
    variant: Literal[1, 2, 3, 4]
    canonical_value: bytes
    native_proof: bytes
    activity_receipt: bytes | None
    activity_receipt_sp1: bytes | None


def encode_proof_bundle_request(value: object) -> ProofBundleRequest:
    if not isinstance(value, Mapping) or set(value) != {"target", "requested_verification_level"}:
        raise _invalid_argument()
    target, level = value["target"], value["requested_verification_level"]
    if (not isinstance(target, str) or len(target) not in {70, 134} or any(character not in _HEX for character in target)
        or not isinstance(level, str) or level not in _LEVELS):
        raise _invalid_argument()
    raw = bytes.fromhex(target)
    kind = raw[2]
    if (raw[:2] != b"\x00\x01" or kind not in {1, 2, 3} or len(raw) != (67 if kind == 2 else 35)
        or not any(raw[3:35]) or kind == 2 and not any(raw[35:])):
        raise _invalid_argument()
    return {"target": target, "requested_verification_level": cast(ProofBundleVerificationLevel, level)}


def decode_proof_bundle_record(value: object) -> ProofBundleRecord:
    if (not isinstance(value, str) or not 0 < len(value) <= 524_288 or len(value) % 2
        or any(character not in _HEX for character in value)):
        raise _decode_failure()
    raw = bytes.fromhex(value)
    if len(raw) < 6 or raw[:5] != b"LXPB1" or raw[5] not in {1, 2, 3, 4}:
        raise _decode_failure()
    variant = cast(Literal[1, 2, 3, 4], raw[5])
    offset = 6

    def field() -> bytes:
        nonlocal offset
        if offset + 4 > len(raw):
            raise _decode_failure()
        length = int.from_bytes(raw[offset:offset + 4], "big")
        offset += 4
        if length == 0 or length > len(raw) - offset:
            raise _decode_failure()
        result = raw[offset:offset + length]
        offset += length
        return result

    canonical_value, native_proof = field(), field()
    receipt, receipt_proof = (field(), field()) if variant == 4 else (None, None)
    if offset != len(raw):
        raise _decode_failure()
    return ProofBundleRecord(variant, canonical_value, native_proof, receipt, receipt_proof)


def check_proof_bundle_response(request: ProofBundleRequest, response: AgentEnvelopeSuccess) -> ProofBundleRecord:
    expected = encode_proof_bundle_request(request)
    try:
        read = response.value
        if not isinstance(read, Mapping) or set(read) != {"value", "achieved_verification_level", "freshness"}:
            raise ValueError("proof_bundle.read")
        value = read["value"]
        if not isinstance(value, Mapping) or set(value) != {"target", "proofs"} or value["target"] != expected["target"]:
            raise ValueError("proof_bundle.target")
        proofs = value["proofs"]
        if not isinstance(proofs, list) or len(proofs) != 1:
            raise ValueError("proof_bundle.proofs")
        proof = decode_proof_bundle_record(proofs[0])
        kind = bytes.fromhex(expected["target"])[2]
        if kind == 1 and proof.variant != 1 or kind == 3 and proof.variant != 3 or kind == 2 and proof.variant not in {2, 4}:
            raise ValueError("proof_bundle.variant")
        achieved = "StateProven" if kind == 2 else "BatchIncluded"
        if (read["achieved_verification_level"] != achieved or _LEVELS[achieved] < _LEVELS[expected["requested_verification_level"]]
            or response.verification_status != {"state": "achieved", "level": achieved}):
            raise ValueError("proof_bundle.level")
        freshness = read["freshness"]
        if not isinstance(freshness, Mapping) or set(freshness) != {"chain_head", "latest_sealed_batch", "latest_finalised_checkpoint", "value_sequence", "relative_to"}:
            raise ValueError("proof_bundle.freshness")
        relative = freshness["relative_to"]
        if not isinstance(relative, Mapping) or set(relative) != {"batch"}:
            raise ValueError("proof_bundle.relative_to")

        def decimal(value: object) -> int:
            if (not isinstance(value, str) or not 0 < len(value) <= 20 or not value.isascii() or not value.isdigit()
                or len(value) > 1 and value[0] == "0" or int(value) > _MAX_U64):
                raise ValueError("proof_bundle.sequence")
            return int(value)

        head, latest = decimal(freshness["chain_head"]), decimal(freshness["latest_sealed_batch"])
        sequence, batch = decimal(freshness["value_sequence"]), decimal(relative["batch"])
        checkpoint = freshness["latest_finalised_checkpoint"]
        if (head == 0 or latest == 0 or batch == 0 or sequence == 0 or sequence > head or batch > latest
            or not isinstance(checkpoint, str) or not _hex32(checkpoint)
            or kind == 2 and (sequence != head or batch != latest)):
            raise ValueError("proof_bundle.freshness_binding")
        return proof
    except (ValueError, TypeError, KeyError, PlatformSdkError):
        raise _decode_failure(response.request_id) from None


class AgentEnvelopeTransport(ProductionTransport):
    __slots__ = ("_gateway_key", "_endpoint", "_maximum_response_bytes", "_opener", "_path", "_session", "_timeout")

    def __init__(
        self,
        endpoint: str,
        *,
        gateway_key: LayerXKeyCredential,
        session: AgentSessionCredential | None,
        ca_file: str | None = None,
        timeout: float = 30.0,
        maximum_response_bytes: int = _MAX_RESPONSE_BYTES,
    ) -> None:
        self._endpoint = _validated_endpoint(endpoint)
        if not isinstance(gateway_key, LayerXKeyCredential) or (session is not None and not isinstance(session, AgentSessionCredential)):
            raise _invalid_argument()
        if not isinstance(timeout, (int, float)) or isinstance(timeout, bool) or timeout <= 0:
            raise _invalid_argument()
        if not isinstance(maximum_response_bytes, int) or isinstance(maximum_response_bytes, bool) or maximum_response_bytes <= 0 or maximum_response_bytes > _MAX_RESPONSE_BYTES:
            raise _invalid_argument()
        handlers: list[object] = [_NoRedirect()]
        if ca_file is not None:
            if not isinstance(ca_file, str) or not ca_file or urlparse(self._endpoint).scheme != "https":
                raise _invalid_argument()
            try:
                context = ssl.create_default_context(cafile=ca_file)
            except (OSError, ssl.SSLError):
                raise _invalid_argument() from None
            handlers.append(HTTPSHandler(context=context))
        self._gateway_key: LayerXKeyCredential | None = gateway_key
        self._path = _ENVELOPE_PATH
        self._session = session
        self._timeout = float(timeout)
        self._maximum_response_bytes = maximum_response_bytes
        self._opener = build_opener(*handlers)

    def call(
        self,
        plane: PlatformPlane,
        operation: object,
        request: object,
        idempotency_key: IdempotencyKey | None,
    ) -> AgentEnvelopeSuccess:
        if plane != "agent" or not isinstance(operation, str) or operation not in AGENT_OPERATIONS:
            raise _unavailable_capability()
        if not isinstance(request, Mapping) or any(not isinstance(key, str) for key in request):
            raise _invalid_argument()
        if operation == "tenant.readiness" and len(request) != 0:
            raise _invalid_argument()
        proof_request = encode_proof_bundle_request(request) if operation == "read.proof_bundle" else None
        if proof_request is not None:
            request = proof_request
        mutating = operation in _AGENT_IDEMPOTENT
        if operation in _BOOTSTRAP_OPERATIONS:
            credential: dict[str, str] | None = None
        elif self._session is None:
            raise _invalid_argument()
        else:
            credential = self._session.coordinates()
        if mutating:
            if idempotency_key is None or not _nonzero_hex32(str(idempotency_key)):
                raise _invalid_argument()
        elif idempotency_key is not None:
            raise _invalid_argument()
        request_id = str(secrets.randbits(64))
        envelope = {
            "version": _ENVELOPE_VERSION,
            "request_id": request_id,
            "operation": operation,
            "request": dict(request),
            "credential": credential,
            "idempotency_key": None if idempotency_key is None else str(idempotency_key),
        }
        try:
            body = json.dumps(envelope, ensure_ascii=True, allow_nan=False, separators=(",", ":")).encode("utf-8")
        except (TypeError, ValueError, OverflowError, RecursionError):
            raise _invalid_argument() from None
        if len(body) > _ENVELOPE_MAX_BODY_BYTES:
            raise _invalid_argument()
        headers = {
            "Accept": "application/json",
            "Content-Type": "application/json",
            "Content-Length": str(len(body)),
            "User-Agent": "layerx-python/0.1.0",
        }
        if self._gateway_key is not None:
            headers["Authorization"] = self._gateway_key.use()
        maximum = min(self._maximum_response_bytes, _ENVELOPE_MAX_BODY_BYTES) if proof_request is not None else self._maximum_response_bytes

        def checked(response: AgentEnvelopeSuccess) -> AgentEnvelopeSuccess:
            if proof_request is not None:
                check_proof_bundle_response(proof_request, response)
            if operation == "tenant.readiness":
                return AgentEnvelopeSuccess(response.request_id, check_tenant_readiness_response(response), response.verification_status)
            return response

        outbound = Request(_route_endpoint(self._endpoint, self._path), data=body, headers=headers, method="POST")
        try:
            with self._opener.open(outbound, timeout=self._timeout) as response:
                encoded = _envelope_read(response, maximum, mutating)
                return checked(_envelope_reply(response.status, response.headers.get("Content-Type"), encoded, request_id, mutating))
        except HTTPError as error:
            try:
                encoded = _envelope_read(error, maximum, mutating)
                return checked(_envelope_reply(error.code, error.headers.get("Content-Type"), encoded, request_id, mutating))
            finally:
                error.close()
        except PlatformSdkError:
            raise
        except (TimeoutError, URLError, OSError, HTTPException):
            if mutating:
                raise PlatformSdkError(SdkErrorCode.UNKNOWN_OUTCOME, "unknown-outcome") from None
            raise PlatformSdkError(SdkErrorCode.TRANSPORT_FAILURE, "safe") from None


    def tenant_readiness(self) -> NativeEnvelopeSuccess[TenantReadinessV1]:
        response = self.call("agent", "tenant.readiness", {}, None)
        return NativeEnvelopeSuccess(response.request_id, cast(TenantReadinessV1, response.value), response.verification_status)

    def read_proof_bundle(self, request: ProofBundleRequest) -> NativeEnvelopeSuccess[ProofBundleRead]:
        response = self.call("agent", "read.proof_bundle", request, None)
        return NativeEnvelopeSuccess(response.request_id, cast(ProofBundleRead, response.value), response.verification_status)

    def prepare_native(self, request: NativePrepareRequestV1, idempotency_key: IdempotencyKey) -> NativeEnvelopeSuccess[NativePrepareResultV1]:
        try:
            body = encode_native_prepare_request(request)
        except (ValueError, TypeError, OverflowError):
            raise _invalid_argument() from None
        purpose = body["purpose"]["purpose"]
        if self._session is None:
            raise _invalid_argument()
        coordinates = self._session.coordinates()
        if (purpose["tenant"] != coordinates["tenant"] or purpose["session_id"] != coordinates["session_id"]
            or purpose["generation"] != coordinates["generation"]):
            raise _invalid_argument()
        response = self.call("agent", "prepare", body, idempotency_key)
        try:
            value = decode_native_prepare_result(response.value)
            canonical = bytes.fromhex(value["canonical_bytes"])
            digest = hashlib.sha256(canonical).hexdigest()
            preimage = hashlib.sha256(b"LXP/v1/signature-preimage\0" + canonical).hexdigest()
            if (value["preparation_id"] != purpose["preparation_id"] or digest != purpose["canonical_digest"]
                or digest != value["preparation_id"] or preimage != value["signing_preimage"]
                or value["activity"] != body["activity"]
                or value["approval_id"] is not None and value["approval_id"] != value["preparation_id"]):
                raise ValueError("native_v1.binding")
            return NativeEnvelopeSuccess(response.request_id, value, response.verification_status)
        except (ValueError, TypeError, OverflowError):
            raise PlatformSdkError(SdkErrorCode.UNKNOWN_OUTCOME, "unknown-outcome", request_id=response.request_id) from None

    def prepare_native_effect(self, request: NativeEffectPrepareRequestV1, idempotency_key: IdempotencyKey) -> NativeEnvelopeSuccess[NativePrepareResultV1]:
        try:
            body = encode_native_effect_prepare_request(request)
        except (ValueError, TypeError, OverflowError):
            raise _invalid_argument() from None
        purpose = body["purpose"]["purpose"]
        if self._session is None:
            raise _invalid_argument()
        coordinates = self._session.coordinates()
        if (purpose["tenant"] != coordinates["tenant"] or purpose["session_id"] != coordinates["session_id"]
            or purpose["generation"] != coordinates["generation"]
            or body["idempotency_key"] != str(idempotency_key)):
            raise _invalid_argument()
        response = self.call("agent", "prepare", body, idempotency_key)
        try:
            value = decode_native_prepare_result(response.value)
            canonical = bytes.fromhex(value["canonical_bytes"])
            digest = hashlib.sha256(canonical).hexdigest()
            preimage = hashlib.sha256(b"LXP/v1/signature-preimage\0" + canonical).hexdigest()
            if (value["preparation_id"] != purpose["preparation_id"] or digest != purpose["canonical_digest"]
                or digest != value["preparation_id"] or preimage != value["signing_preimage"]
                or value["activity"] != body["activity"]
                or value["approval_id"] is not None and value["approval_id"] != value["preparation_id"]):
                raise ValueError("native_effect_v1.binding")
            return NativeEnvelopeSuccess(response.request_id, value, response.verification_status)
        except (ValueError, TypeError, OverflowError):
            raise PlatformSdkError(SdkErrorCode.UNKNOWN_OUTCOME, "unknown-outcome", request_id=response.request_id) from None

    def approval_list_native(self) -> NativeEnvelopeSuccess[NativeApprovalListResultV1]:
        response = self.call("agent", "approval.list", {"variant": "native_v1"}, None)
        try:
            value = decode_native_approval_list_result(response.value)
            return NativeEnvelopeSuccess(response.request_id, value, response.verification_status)
        except (ValueError, TypeError, OverflowError):
            raise _decode_failure(response.request_id) from None

    def approval_get_native(self, approval_id: str) -> NativeEnvelopeSuccess[NativeApprovalResultV1]:
        try:
            request = encode_native_approval_get(approval_id)
        except (ValueError, TypeError):
            raise _invalid_argument() from None
        response = self.call("agent", "approval.get", request, None)
        try:
            value = decode_native_approval_result(response.value)
            if value["approval_id"] != approval_id:
                raise ValueError("native_v1.binding")
            return NativeEnvelopeSuccess(response.request_id, value, response.verification_status)
        except (ValueError, TypeError, OverflowError):
            raise _decode_failure(response.request_id) from None

    def approval_decide_native(self, request: NativeApprovalDecisionV1, grant: bool, idempotency_key: IdempotencyKey) -> NativeEnvelopeSuccess[NativeApprovalResultV1]:
        try:
            body = encode_native_approval_decision(request)
            if type(grant) is not bool:
                raise ValueError("native_v1.decision")
        except (ValueError, TypeError, OverflowError):
            raise _invalid_argument() from None
        response = self.call("agent", "approval.approve" if grant else "approval.reject", body, idempotency_key)
        try:
            value = decode_native_approval_result(response.value)
            if value["approval_id"] != body["approval_id"] or value["held_digest"] != body["held_digest"]:
                raise ValueError("native_v1.binding")
            return NativeEnvelopeSuccess(response.request_id, value, response.verification_status)
        except (ValueError, TypeError, OverflowError):
            raise PlatformSdkError(SdkErrorCode.UNKNOWN_OUTCOME, "unknown-outcome", request_id=response.request_id) from None


class AgentDaemonEnvelopeTransport(AgentEnvelopeTransport):
    __slots__ = ()

    def __init__(
        self,
        endpoint: str,
        *,
        ssl_context: ssl.SSLContext,
        session: AgentSessionCredential | None,
        timeout: float = 30.0,
        maximum_response_bytes: int = _MAX_RESPONSE_BYTES,
    ) -> None:
        self._endpoint = _validated_endpoint(endpoint)
        if urlparse(self._endpoint).scheme != "https" or urlparse(self._endpoint).path:
            raise _invalid_argument()
        if not isinstance(ssl_context, ssl.SSLContext) or ssl_context.verify_mode != ssl.CERT_REQUIRED or not ssl_context.check_hostname:
            raise _invalid_argument()
        if session is not None and not isinstance(session, AgentSessionCredential):
            raise _invalid_argument()
        if not isinstance(timeout, (int, float)) or isinstance(timeout, bool) or timeout <= 0:
            raise _invalid_argument()
        if not isinstance(maximum_response_bytes, int) or isinstance(maximum_response_bytes, bool) or maximum_response_bytes <= 0 or maximum_response_bytes > _MAX_RESPONSE_BYTES:
            raise _invalid_argument()
        self._gateway_key = None
        self._path = _DAEMON_PATH
        self._session = session
        self._timeout = float(timeout)
        self._maximum_response_bytes = maximum_response_bytes
        self._opener = build_opener(_NoRedirect(), HTTPSHandler(context=ssl_context))


def _envelope_reply(status: int, content_type: str | None, encoded: bytes, request_id: str, mutating: bool) -> AgentEnvelopeSuccess:
    try:
        if content_type != "application/json":
            raise _decode_failure()
        return _decode_agent_envelope_response(status, encoded, request_id)
    except PlatformSdkError as error:
        if error.code is not SdkErrorCode.DECODE_FAILURE:
            raise
        if mutating:
            raise PlatformSdkError(SdkErrorCode.UNKNOWN_OUTCOME, "unknown-outcome", request_id=error.request_id) from None
        if content_type != "application/json" or status in {502, 503} or not _well_formed_json(encoded):
            raise PlatformSdkError(SdkErrorCode.TRANSPORT_FAILURE, "safe") from None
        raise


def _envelope_read(response: object, maximum: int, mutating: bool) -> bytes:
    if not mutating:
        try:
            return _bounded_read(response, maximum)
        except PlatformSdkError:
            raise PlatformSdkError(SdkErrorCode.TRANSPORT_FAILURE, "safe") from None
    reader = getattr(response, "read", None)
    if not callable(reader):
        raise _decode_failure()
    try:
        encoded = cast(bytes, reader(maximum + 1))
    except (TimeoutError, OSError, HTTPException):
        raise PlatformSdkError(SdkErrorCode.UNKNOWN_OUTCOME, "unknown-outcome") from None
    if len(encoded) > maximum:
        raise PlatformSdkError(SdkErrorCode.UNKNOWN_OUTCOME, "unknown-outcome")
    return encoded


def _decode_agent_envelope_response(status: int, encoded: bytes, sent_request_id: str) -> AgentEnvelopeSuccess:
    try:
        envelope = json.loads(encoded.decode("utf-8"), parse_constant=_reject_constant, object_pairs_hook=_unique_object)
    except (UnicodeDecodeError, json.JSONDecodeError, ValueError, RecursionError):
        raise _decode_failure() from None
    if not isinstance(envelope, dict):
        raise _decode_failure()
    if "class" in envelope:
        _exact(envelope, ("class", "protocol_result_code", "retriability", "reason", "request_id"))
        if envelope["request_id"] not in {sent_request_id, "0"}:
            raise _decode_failure()
        protocol = envelope["protocol_result_code"]
        if protocol is not None and (type(protocol) is not int or not -(1 << 31) <= protocol < 1 << 31):
            raise _decode_failure(sent_request_id)
        raise _service_error(status, envelope)
    _exact(envelope, ("request_id", "value", "verification_status"))
    request_id = envelope["request_id"]
    if status != 200 or request_id != sent_request_id:
        raise _decode_failure(request_id if _valid_request_id(request_id) else None)
    verification = envelope["verification_status"]
    if not _valid_verification_status(verification):
        raise PlatformSdkError(SdkErrorCode.VERIFICATION_FAILURE, "never", request_id=request_id)
    return AgentEnvelopeSuccess(request_id, envelope["value"], verification)


def _well_formed_json(encoded: bytes) -> bool:
    try:
        json.loads(encoded.decode("utf-8"), parse_constant=_reject_constant, object_pairs_hook=_unique_object)
    except (UnicodeDecodeError, json.JSONDecodeError, ValueError, RecursionError):
        return False
    return True


def _unique_object(pairs: list[tuple[str, object]]) -> dict[str, object]:
    result = dict(pairs)
    if len(result) != len(pairs):
        raise ValueError("duplicate key")
    return result


def _reject_constant(value: str) -> object:
    raise ValueError(value)


def _valid_verification_status(value: object) -> bool:
    if not isinstance(value, dict):
        return False
    if value.get("state") == "achieved":
        return set(value) == {"state", "level"} and isinstance(value.get("level"), str) and value["level"] in _LEVELS
    if value.get("state") != "unverified" or set(value) != {"state", "requested", "achieved", "reason"}:
        return False
    requested = value.get("requested")
    achieved = value.get("achieved")
    reason = value.get("reason")
    return (
        isinstance(requested, str) and requested in _LEVELS
        and isinstance(achieved, str) and achieved in _LEVELS
        and _LEVELS[achieved] < _LEVELS[requested]
        and isinstance(reason, str) and bool(reason)
        and all(character in "abcdefghijklmnopqrstuvwxyz0123456789_." for character in reason)
    )


def _nonzero_hex32(value: str) -> bool:
    return _hex32(value) and value != "0" * 64


def _encode_program_mutation_body(signed: object) -> bytes:
    if not isinstance(signed, str) or not 0 < len(signed) <= 2_097_152 or len(signed) % 2 or any(character not in _HEX for character in signed):
        raise _invalid_argument()
    return bytes.fromhex(signed)


def _validated_endpoint(value: str) -> str:
    try:
        parsed = urlparse(value)
        port = parsed.port
    except ValueError:
        raise _invalid_argument() from None
    if parsed.scheme not in {"http", "https"} or not parsed.hostname or parsed.username is not None or parsed.password is not None or parsed.query or parsed.fragment:
        raise _invalid_argument()
    if port is not None and not 0 < port <= 65535:
        raise _invalid_argument()
    if parsed.scheme == "http" and not _loopback(parsed.hostname):
        raise _invalid_argument()
    return urlunparse((parsed.scheme, parsed.netloc, parsed.path.rstrip("/"), "", "", ""))


def _loopback(hostname: str) -> bool:
    lowered = hostname.lower()
    if lowered in {"localhost", "::1"}:
        return True
    octets = lowered.split(".")
    return len(octets) == 4 and octets[0] == "127" and all(octet.isdigit() and 0 <= int(octet) <= 255 for octet in octets)


def _route_endpoint(base: str, path: str) -> str:
    parsed = urlparse(base)
    return urlunparse((parsed.scheme, parsed.netloc, parsed.path.rstrip("/") + path, "", "", ""))


def _require_exact_request(operation: str, request: Mapping[str, object]) -> None:
    fields: Mapping[str, frozenset[str]] = {
        "program.deploy": frozenset(("signed_activity",)),
        "program.upgrade": frozenset(("signed_activity",)),
        "program.wind-down": frozenset(("signed_activity",)),
        "program.discover": frozenset(("program_id", "requested_verification_level")),
        "program.interface": frozenset(("program_id", "requested_verification_level")),
        "program.simulate": frozenset(("program_id", "calldata", "budget", "capabilities", "signed_activity")),
        "program.call": frozenset(("program_id", "calldata", "budget", "capabilities", "signed_activity")),
        "program.receipt": frozenset(("idempotency_key", "expected_activity_id", "requested_verification_level")),
        "program.activity": frozenset(("activity_id", "requested_verification_level")),
    }
    expected = fields[operation]
    if operation in {"program.call", "program.simulate"} and request.get("payload_encoding") == "native-v1":
        expected = frozenset(("payload_encoding", "program_id", "calldata", "budget", "signed_activity", "native_call"))
    if frozenset(request) != expected:
        raise _invalid_argument()


def _bounded_read(response: object, maximum: int) -> bytes:
    reader = getattr(response, "read", None)
    if not callable(reader):
        raise _decode_failure()
    try:
        encoded = cast(bytes, reader(maximum + 1))
    except (OSError, HTTPException):
        raise _decode_failure() from None
    if len(encoded) > maximum:
        raise _decode_failure()
    return encoded


def _decode_envelope(status: int, encoded: bytes, operation: str) -> object:
    try:
        envelope = json.loads(encoded.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError):
        raise _decode_failure() from None
    if not isinstance(envelope, dict) or any(not isinstance(key, str) for key in envelope):
        raise _decode_failure()
    if "class" in envelope:
        _exact(envelope, ("class", "protocol_result_code", "retriability", "reason", "request_id"))
        raise _service_error(status, envelope)
    if operation in {"program.deploy", "program.upgrade", "program.wind-down"} or operation == "program.receipt" and ("result" in envelope or "error" in envelope):
        if "error" in envelope:
            _exact(envelope, ("error",))
            raise _decode_program_boundary_error(status, envelope["error"])
        if status == 202 and envelope.get("state") == "unknown":
            return envelope
        _exact(envelope, ("result",))
        if not 200 <= status < 300:
            raise _decode_failure()
        return envelope["result"]
    _exact(envelope, ("request_id", "value", "verification_status"))
    request_id = envelope.get("request_id")
    if not 200 <= status < 300 or not _valid_request_id(request_id) or "value" not in envelope:
        raise _decode_failure(request_id if isinstance(request_id, str) else None)
    if not _accepted_program_verification(operation, envelope.get("value"), envelope.get("verification_status")):
        raise PlatformSdkError(SdkErrorCode.VERIFICATION_FAILURE, "never", request_id=request_id)
    return envelope["value"]


class ProgramBoundaryError(PlatformSdkError):
    def __init__(self, status: int, boundary_code: str, retry_after_ms: int | None = None) -> None:
        super().__init__(SdkErrorCode.IDEMPOTENCY_CONFLICT if status == 409 else SdkErrorCode.RATE_LIMIT if status == 429 else SdkErrorCode.CORE_REJECTION,
                         "never" if retry_after_ms is None else "safe", retry_after_ms=retry_after_ms)
        self.status = status
        self.boundary_code = boundary_code


def _decode_program_boundary_error(status: int, value: object) -> ProgramBoundaryError:
    if type(status) is not int or not 400 <= status < 600 or not isinstance(value, dict):
        raise _decode_failure()
    code = value.get("code")
    if not isinstance(code, str) or not 0 < len(code) <= 128 or any(character not in "abcdefghijklmnopqrstuvwxyz0123456789_" for character in code):
        raise _decode_failure()
    if value.get("retry") == "never":
        _exact(value, ("code", "retry"))
        return ProgramBoundaryError(status, code)
    _exact(value, ("code", "retry", "retry_after_seconds"))
    seconds = value.get("retry_after_seconds")
    if value.get("retry") != "after" or type(seconds) is not int or not 0 < seconds < 1 << 64:
        raise _decode_failure()
    return ProgramBoundaryError(status, code, seconds * 1000)


def _accepted_program_verification(operation: str, result: object, value: object) -> bool:
    if not isinstance(value, dict) or any(not isinstance(key, str) for key in value):
        return False
    result_state = result.get("state") if isinstance(result, dict) else None
    if operation in {"program.discover", "program.interface"}:
        return _exact_unverified(value, "server_side_receipt_verification_only")
    if operation in {"program.call", "program.receipt", "program.activity"} and result_state in {"unknown", "pending"}:
        return _exact_unverified(value, "receipt_pending")
    return set(value) == {"state", "level"} and value.get("state") == "Achieved" and value.get("level") == "SequencerSigned"


def _exact_unverified(value: Mapping[str, object], reason: str) -> bool:
    return set(value) == {"state", "requested", "achieved", "reason"} and value.get("state") == "Unverified" and value.get("requested") == "SequencerSigned" and value.get("achieved") == "Unverified" and value.get("reason") == reason


def _service_error(status: int, error: Mapping[str, object]) -> PlatformSdkError:
    request_id = error.get("request_id")
    exact_class = error.get("class")
    retriability = error.get("retriability")
    reason = error.get("reason")
    protocol = error.get("protocol_result_code")
    code = _ERROR_CLASS.get(exact_class) if isinstance(exact_class, str) else None
    if 200 <= status < 300 or not _valid_request_id(request_id) or code is None or retriability not in {"Terminal", "Retriable"} or not isinstance(reason, str) or not reason or any(character not in "abcdefghijklmnopqrstuvwxyz0123456789_." for character in reason) or (protocol is not None and (not isinstance(protocol, int) or isinstance(protocol, bool))):
        raise _decode_failure(request_id if isinstance(request_id, str) else None)
    return PlatformSdkError(
        code,
        "safe" if retriability == "Retriable" else "never",
        request_id=request_id,
        protocol_result_code=cast(int | None, protocol),
    )


def _hex32(value: str) -> bool:
    return len(value) == 64 and all(character in _HEX for character in value)


def _exact(value: Mapping[str, object], required: tuple[str, ...]) -> None:
    if set(value) != set(required):
        raise _decode_failure()


def _valid_request_id(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 128 and value.isascii() and all(0x21 <= ord(character) <= 0x7E for character in value)


class _NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, request: Request, file_pointer: object, code: int, message: str, headers: object, new_url: str) -> None:
        del request, file_pointer, code, message, headers, new_url


def _transport_failure(operation: str) -> PlatformSdkError:
    if operation in {"program.call", "program.deploy", "program.upgrade", "program.wind-down"}:
        return PlatformSdkError(SdkErrorCode.UNKNOWN_OUTCOME, "unknown-outcome")
    return PlatformSdkError(SdkErrorCode.TRANSPORT_FAILURE, "safe")


def _invalid_argument() -> PlatformSdkError:
    return PlatformSdkError(SdkErrorCode.INVALID_ARGUMENT, "never")


def _unavailable_capability() -> PlatformSdkError:
    return PlatformSdkError(SdkErrorCode.UNAVAILABLE_CAPABILITY, "never")


def _decode_failure(request_id: str | None = None) -> PlatformSdkError:
    return PlatformSdkError(SdkErrorCode.DECODE_FAILURE, "never", request_id=request_id)
