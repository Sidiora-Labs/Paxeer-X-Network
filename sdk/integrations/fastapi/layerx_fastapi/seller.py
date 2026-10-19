from __future__ import annotations

from dataclasses import dataclass, field
from re import fullmatch
from typing import Awaitable, Callable, Generic, Literal, Mapping, Protocol, TypeVar

from layerx_sdk import AuthorizedReceiptBatch, LocalSignatureVerifier, ReceiptVerification

from .protocol import (
    PAYMENT_REQUIRED_HEADER,
    PAYMENT_RESPONSE_HEADER,
    JsonValue,
    LayerXReceiptEvidence,
    MiddlewareError,
    MiddlewareErrorCode,
    PaymentPayload,
    PaymentRequired,
    PaymentRequirements,
    SettlementResponse,
    canonical_json,
    constant_time_equal,
    constant_time_equal_text,
    encode_base64,
    encode_payment_required_header,
    encode_settlement_header,
    decode_base64,
    decode_payment_payload_header,
    match_requirements,
    merkle_leaf_digest,
    parse_layerx_evidence,
    parse_payment_required,
    payment_idempotency_key,
    sha256_digest,
    verify_payment_receipt,
)

T = TypeVar("T")


@dataclass(frozen=True)
class SellerSettlementRequest:
    principal: str
    payload: PaymentPayload
    requirements: PaymentRequirements
    idempotency_key: str
    request_digest: str


@dataclass(frozen=True)
class SettlementPending:
    kind: Literal["pending"] = "pending"


@dataclass(frozen=True)
class SettlementRefused:
    reason: str
    kind: Literal["refused"] = "refused"


@dataclass(frozen=True)
class SettlementSettled:
    canonical_receipt: bytes
    authorized_batch: AuthorizedReceiptBatch
    kind: Literal["settled"] = "settled"


SellerSettlementOutcome = SettlementPending | SettlementRefused | SettlementSettled


class SellerPaymentAuthority(Protocol):
    async def settle(self, request: SellerSettlementRequest) -> SellerSettlementOutcome: ...


class AuthorizedBatchResolver(Protocol):
    async def resolve(self, canonical_receipt: bytes) -> AuthorizedReceiptBatch: ...


@dataclass(frozen=True)
class ProposedFulfillment:
    idempotency_key: str
    request_digest: str
    canonical_receipt: bytes
    authorized_batch: AuthorizedReceiptBatch


@dataclass(frozen=True)
class StoredFulfillment(Generic[T]):
    idempotency_key: str
    request_digest: str
    canonical_receipt: bytes
    authorized_batch: AuthorizedReceiptBatch
    resource: T


class FulfillmentRepository(Protocol[T]):
    async def fulfill(
        self,
        proposed: ProposedFulfillment,
        release: Callable[[], Awaitable[T]],
    ) -> StoredFulfillment[T]: ...


@dataclass(frozen=True)
class PaymentRequiredDecision:
    body: PaymentRequired
    headers: Mapping[str, str]
    status: Literal[402] = 402
    kind: Literal["payment-required"] = "payment-required"


@dataclass(frozen=True)
class PendingDecision:
    status: Literal[202] = 202
    kind: Literal["pending"] = "pending"
    headers: Mapping[str, str] = field(default_factory=dict)


@dataclass(frozen=True)
class RefusedDecision:
    settlement: SettlementResponse
    headers: Mapping[str, str]
    status: Literal[402] = 402
    kind: Literal["refused"] = "refused"


@dataclass(frozen=True)
class ReleasedDecision(Generic[T]):
    settlement: SettlementResponse
    headers: Mapping[str, str]
    verification: ReceiptVerification
    resource: T
    status: Literal[200] = 200
    kind: Literal["released"] = "released"


SellerDecision = PaymentRequiredDecision | PendingDecision | RefusedDecision | ReleasedDecision[T]


class ReceiptPayloadAuthority:
    __slots__ = ("_authorized_batches",)

    def __init__(self, authorized_batches: AuthorizedBatchResolver) -> None:
        self._authorized_batches = authorized_batches

    async def settle(self, request: SellerSettlementRequest) -> SellerSettlementOutcome:
        evidence = parse_layerx_evidence(request.payload.payload)
        canonical_receipt = decode_base64(evidence.receipt, MiddlewareErrorCode.INVALID_PAYMENT_PAYLOAD)
        digest = merkle_leaf_digest(canonical_receipt)
        if not constant_time_equal_text(evidence.receipt_digest, digest.hex()):
            raise MiddlewareError(MiddlewareErrorCode.VERIFICATION_FAILURE)
        authorized_batch = await self._authorized_batches.resolve(canonical_receipt)
        return SettlementSettled(canonical_receipt=canonical_receipt, authorized_batch=authorized_batch)


class StaticAuthorizedBatches:
    __slots__ = ("_batch",)

    def __init__(self, batch: AuthorizedReceiptBatch) -> None:
        self._batch = batch

    async def resolve(self, canonical_receipt: bytes) -> AuthorizedReceiptBatch:
        if len(canonical_receipt) == 0:
            raise MiddlewareError(MiddlewareErrorCode.VERIFICATION_FAILURE)
        return self._batch


class SellerMiddleware(Generic[T]):
    __slots__ = ("_required", "_authority", "_fulfillments", "_signatures")

    def __init__(
        self,
        payment_required: PaymentRequired,
        authority: SellerPaymentAuthority,
        fulfillments: FulfillmentRepository[T],
        signatures: LocalSignatureVerifier,
    ) -> None:
        self._required = parse_payment_required(payment_required.to_wire())
        self._authority = authority
        self._fulfillments = fulfillments
        self._signatures = signatures

    @property
    def required(self) -> PaymentRequired:
        return self._required

    def payment_required(self) -> PaymentRequiredDecision:
        return PaymentRequiredDecision(
            body=self._required,
            headers={PAYMENT_REQUIRED_HEADER: encode_payment_required_header(self._required)},
        )

    async def handle(
        self,
        principal: str,
        payment_header: str | None,
        release: Callable[[], Awaitable[T]],
    ) -> SellerDecision[T]:
        if payment_header is None:
            return self.payment_required()
        if not 0 < len(principal) <= 512 or "\0" in principal:
            raise MiddlewareError(MiddlewareErrorCode.INVALID_PAYMENT_PAYLOAD)
        payload = decode_payment_payload_header(payment_header)
        requirements = match_requirements(self._required, payload)
        canonical = canonical_json(payload.to_wire())
        request_digest_bytes = sha256_digest(canonical.encode("utf-8"))
        request_digest = request_digest_bytes.hex()
        idempotency_key = payment_idempotency_key(principal, request_digest_bytes)
        outcome = await self._authority.settle(
            SellerSettlementRequest(
                principal=principal,
                payload=payload,
                requirements=requirements,
                idempotency_key=idempotency_key,
                request_digest=request_digest,
            )
        )
        if isinstance(outcome, SettlementPending):
            return PendingDecision()
        if isinstance(outcome, SettlementRefused):
            return refusal_decision(requirements, outcome.reason)
        proposed = ProposedFulfillment(
            idempotency_key=idempotency_key,
            request_digest=request_digest,
            canonical_receipt=outcome.canonical_receipt,
            authorized_batch=outcome.authorized_batch,
        )
        verification = verify_payment_receipt(
            proposed.canonical_receipt,
            proposed.authorized_batch,
            requirements,
            self._signatures,
        )
        stored = await self._fulfillments.fulfill(proposed, release)
        if stored.idempotency_key != idempotency_key or stored.request_digest != request_digest:
            raise MiddlewareError(MiddlewareErrorCode.FULFILLMENT_CONFLICT)
        stored_verification = verify_payment_receipt(
            stored.canonical_receipt,
            stored.authorized_batch,
            requirements,
            self._signatures,
        )
        if not constant_time_equal(verification.receipt_digest, stored_verification.receipt_digest):
            raise MiddlewareError(MiddlewareErrorCode.FULFILLMENT_CONFLICT)
        receipt_digest = merkle_leaf_digest(stored.canonical_receipt).hex()
        evidence = LayerXReceiptEvidence(
            receipt=encode_base64(stored.canonical_receipt),
            receipt_digest=receipt_digest,
        )
        settlement = SettlementResponse(
            success=True,
            payer=stored_verification.receipt.from_account.hex(),
            transaction=f"lxp:{receipt_digest}",
            network=requirements.network,
            amount=requirements.amount,
            extensions={"layerx": layerx_evidence_wire(evidence)},
            has_extensions=True,
        )
        return ReleasedDecision(
            settlement=settlement,
            headers={PAYMENT_RESPONSE_HEADER: encode_settlement_header(settlement)},
            verification=stored_verification,
            resource=stored.resource,
        )


def layerx_evidence_wire(evidence: LayerXReceiptEvidence) -> JsonValue:
    return {
        "receipt": evidence.receipt,
        "receiptDigest": evidence.receipt_digest,
        "verificationLevel": evidence.verification_level,
    }


def refusal_decision(requirements: PaymentRequirements, reason: str) -> RefusedDecision:
    safe_reason = reason if fullmatch(r"[a-z][a-z0-9_]{0,63}", reason) is not None else "payment_refused"
    settlement = SettlementResponse(
        success=False,
        error_reason=safe_reason,
        transaction="",
        network=requirements.network,
    )
    return RefusedDecision(
        settlement=settlement,
        headers={PAYMENT_RESPONSE_HEADER: encode_settlement_header(settlement)},
    )
