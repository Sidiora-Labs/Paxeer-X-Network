from __future__ import annotations

from dataclasses import dataclass
from re import fullmatch
from typing import Awaitable, Callable, Mapping, Protocol

from fastapi import FastAPI, Request, Response
from fastapi.responses import JSONResponse

from layerx_sdk import LocalSignatureVerifier

from .config import (
    DeclaredConfig,
    IntegrationError,
    IntegrationErrorCode,
    read_declared_config,
)
from .protocol import (
    PAYMENT_REQUIRED_HEADER,
    PAYMENT_RESPONSE_HEADER,
    PAYMENT_SIGNATURE_HEADER,
    JsonValue,
    MiddlewareError,
    MiddlewareErrorCode,
    PaymentRequirements,
    constant_time_equal,
    constant_time_equal_text,
    merkle_leaf_digest,
    parse_hex32,
)
from .seller import (
    AuthorizedBatchResolver,
    FulfillmentRepository,
    PaymentRequiredDecision,
    PendingDecision,
    ReceiptPayloadAuthority,
    RefusedDecision,
    ReleasedDecision,
    SellerMiddleware,
    SellerPaymentAuthority,
    StaticAuthorizedBatches,
)
from .signatures import LayerXSignatureVerifier
from .webhooks import (
    WEBHOOK_ID_HEADER,
    WEBHOOK_KEY_HEADER,
    WEBHOOK_SIGNATURE_HEADER,
    WEBHOOK_TIMESTAMP_HEADER,
    VerifiedWebhookConsumer,
    WebhookDeliveryStore,
    WebhookRequestHeaders,
)

MAXIMUM_WEBHOOK_BYTES = 1_048_576


@dataclass(frozen=True)
class LayerXResource:
    content_type: str
    body: bytes


class LayerXResourceHandler(Protocol):
    async def release(self, request: Request) -> LayerXResource: ...


class LayerXWebhookEventHandler(Protocol):
    async def handle(self, event: Mapping[str, JsonValue], delivery_id: str) -> None: ...


@dataclass(frozen=True)
class LayerXMountOptions:
    resources: LayerXResourceHandler
    fulfillments: FulfillmentRepository[LayerXResource]
    deliveries: WebhookDeliveryStore
    events: LayerXWebhookEventHandler
    environment: Mapping[str, str] | None = None
    authorized_batches: AuthorizedBatchResolver | None = None
    authority: SellerPaymentAuthority | None = None
    signatures: LocalSignatureVerifier | None = None
    now: Callable[[], int] | None = None


@dataclass(frozen=True)
class LayerXSellerRuntime:
    seller: SellerMiddleware[LayerXResource]
    requirements: PaymentRequirements
    principal: str
    resources: LayerXResourceHandler


@dataclass(frozen=True)
class LayerXMount:
    config: DeclaredConfig
    seller: SellerMiddleware[LayerXResource]
    webhooks: VerifiedWebhookConsumer
    runtime: LayerXSellerRuntime
    payment_gate: Callable[[Request], Awaitable[Response]]
    webhook_endpoint: Callable[[Request], Awaitable[Response]]


def platform_int_fastapi() -> str:
    return "receipt-gated-x402-fastapi"


def create_layerx_mount(options: LayerXMountOptions) -> LayerXMount:
    config = read_declared_config(options.environment)
    authorized_batches = (
        options.authorized_batches
        if options.authorized_batches is not None
        else StaticAuthorizedBatches(config.authorized_batch)
    )
    signatures = options.signatures if options.signatures is not None else LayerXSignatureVerifier()
    seller: SellerMiddleware[LayerXResource] = SellerMiddleware(
        payment_required=config.payment_required,
        authority=(
            options.authority
            if options.authority is not None
            else ReceiptPayloadAuthority(authorized_batches)
        ),
        fulfillments=options.fulfillments,
        signatures=signatures,
    )
    webhooks = VerifiedWebhookConsumer(
        public_keys=config.webhook.public_keys,
        deliveries=options.deliveries,
        maximum_age_ms=config.webhook.maximum_age_ms,
        lease_ms=config.webhook.lease_ms,
        now=options.now,
    )
    runtime = LayerXSellerRuntime(
        seller=seller,
        requirements=config.requirements,
        principal=config.principal,
        resources=options.resources,
    )
    return LayerXMount(
        config=config,
        seller=seller,
        webhooks=webhooks,
        runtime=runtime,
        payment_gate=layerx_payment_gate(runtime),
        webhook_endpoint=layerx_webhook_endpoint(webhooks, options.events),
    )


def mount_layerx(app: FastAPI, options: LayerXMountOptions) -> LayerXMount:
    mount = create_layerx_mount(options)
    app.add_api_route(
        mount.config.protected_path,
        mount.payment_gate,
        methods=["GET", "POST"],
        response_model=None,
        include_in_schema=False,
        name="layerx-protected-resource",
    )
    app.add_api_route(
        mount.config.webhook.path,
        mount.webhook_endpoint,
        methods=["POST"],
        response_model=None,
        include_in_schema=False,
        name="layerx-webhook",
    )
    return mount


def layerx_payment_gate(runtime: LayerXSellerRuntime) -> Callable[[Request], Awaitable[Response]]:
    async def endpoint(request: Request) -> Response:
        return await release_guarded(runtime, request)

    return endpoint


def layerx_webhook_endpoint(
    consumer: VerifiedWebhookConsumer,
    events: LayerXWebhookEventHandler,
) -> Callable[[Request], Awaitable[Response]]:
    async def endpoint(request: Request) -> Response:
        return await consume_guarded(consumer, events, request)

    return endpoint


def assert_receipt_backed(
    decision: ReleasedDecision[LayerXResource],
    requirements: PaymentRequirements,
) -> None:
    evidence_digest = layerx_evidence_digest(decision.settlement.extensions)
    receipt_digest = merkle_leaf_digest(decision.verification.canonical_bytes).hex()
    if (
        decision.verification.level != "sequencer-signed"
        or decision.verification.receipt.result_code != 0
        or not decision.settlement.success
        or decision.settlement.network != requirements.network
        or decision.settlement.amount != requirements.amount
        or decision.settlement.transaction != f"lxp:{receipt_digest}"
        or not constant_time_equal_text(evidence_digest, receipt_digest)
        or decision.verification.receipt.amount != int(requirements.amount)
        or not constant_time_equal(decision.verification.receipt.asset, parse_hex32(requirements.asset))
        or not constant_time_equal(decision.verification.receipt.to_account, parse_hex32(requirements.pay_to))
    ):
        raise IntegrationError(IntegrationErrorCode.RECEIPT_NOT_BACKED)


async def release_guarded(runtime: LayerXSellerRuntime, request: Request) -> Response:
    async def release() -> LayerXResource:
        return await runtime.resources.release(request)

    try:
        decision = await runtime.seller.handle(
            runtime.principal,
            single_header(request, PAYMENT_SIGNATURE_HEADER.lower()),
            release,
        )
    except MiddlewareError as error:
        return JSONResponse({"error": error.code.value}, status_code=payment_error_status(error.code))
    if isinstance(decision, PaymentRequiredDecision):
        return JSONResponse(
            decision.body.to_wire(),
            status_code=decision.status,
            headers={PAYMENT_REQUIRED_HEADER: decision.headers[PAYMENT_REQUIRED_HEADER]},
        )
    if isinstance(decision, PendingDecision):
        return Response(status_code=decision.status, headers={"retry-after": "1"})
    if isinstance(decision, RefusedDecision):
        return Response(
            status_code=decision.status,
            headers={PAYMENT_RESPONSE_HEADER: decision.headers[PAYMENT_RESPONSE_HEADER]},
        )
    assert_receipt_backed(decision, runtime.requirements)
    return Response(
        content=decision.resource.body,
        status_code=decision.status,
        media_type=decision.resource.content_type,
        headers={
            PAYMENT_RESPONSE_HEADER: decision.headers[PAYMENT_RESPONSE_HEADER],
            "layerx-receipt-digest": decision.verification.receipt_digest.hex(),
            "layerx-transaction": decision.settlement.transaction,
        },
    )


async def consume_guarded(
    consumer: VerifiedWebhookConsumer,
    events: LayerXWebhookEventHandler,
    request: Request,
) -> Response:
    headers = webhook_headers(request)
    raw_body = await read_raw_body(request)

    async def handle(event: Mapping[str, JsonValue], delivery_id: str) -> None:
        await events.handle(event, delivery_id)

    try:
        outcome = await consumer.consume(raw_body, headers, handle)
    except MiddlewareError as error:
        if error.code == MiddlewareErrorCode.INVALID_WEBHOOK:
            return JSONResponse({"error": error.code.value}, status_code=401)
        if error.code == MiddlewareErrorCode.WEBHOOK_REPLAY:
            return JSONResponse({"error": error.code.value}, status_code=409)
        raise
    if outcome == "processed":
        return Response(status_code=204)
    if outcome == "duplicate":
        return JSONResponse({"outcome": outcome}, status_code=200)
    return JSONResponse({"outcome": outcome}, status_code=409, headers={"retry-after": "1"})


def layerx_evidence_digest(extensions: Mapping[str, JsonValue]) -> str:
    layerx = extensions.get("layerx")
    if not isinstance(layerx, Mapping):
        raise IntegrationError(IntegrationErrorCode.RECEIPT_NOT_BACKED)
    digest = layerx.get("receiptDigest")
    if not isinstance(digest, str) or fullmatch(r"[0-9a-f]{64}", digest) is None:
        raise IntegrationError(IntegrationErrorCode.RECEIPT_NOT_BACKED)
    return digest


def payment_error_status(code: MiddlewareErrorCode) -> int:
    if code == MiddlewareErrorCode.PAYMENT_PENDING:
        return 202
    if code == MiddlewareErrorCode.FULFILLMENT_CONFLICT:
        return 409
    return 402


def webhook_headers(request: Request) -> WebhookRequestHeaders:
    delivery_id = single_header(request, WEBHOOK_ID_HEADER)
    timestamp = single_header(request, WEBHOOK_TIMESTAMP_HEADER)
    key_id = single_header(request, WEBHOOK_KEY_HEADER)
    signature = single_header(request, WEBHOOK_SIGNATURE_HEADER)
    if delivery_id is None or timestamp is None or key_id is None or signature is None:
        raise MiddlewareError(MiddlewareErrorCode.INVALID_WEBHOOK)
    return WebhookRequestHeaders(
        id=delivery_id,
        timestamp=timestamp,
        key_id=key_id,
        signature=signature,
    )


async def read_raw_body(request: Request) -> bytes:
    chunks: list[bytes] = []
    total = 0
    async for chunk in request.stream():
        total += len(chunk)
        if total > MAXIMUM_WEBHOOK_BYTES:
            raise MiddlewareError(MiddlewareErrorCode.INVALID_WEBHOOK)
        chunks.append(chunk)
    return b"".join(chunks)


def single_header(request: Request, name: str) -> str | None:
    values = request.headers.getlist(name)
    if len(values) == 0:
        return None
    if len(values) > 1:
        raise IntegrationError(IntegrationErrorCode.DUPLICATE_HEADER)
    return values[0]
