from __future__ import annotations

import json
from asyncio import Lock
from dataclasses import dataclass
from re import fullmatch
from time import time
from typing import Awaitable, Callable, Literal, Mapping, Protocol

from .protocol import (
    JsonValue,
    MiddlewareError,
    MiddlewareErrorCode,
    decode_base64,
    sha256_digest,
)
from .signatures import verify_ed25519

WEBHOOK_ID_HEADER = "layerx-webhook-id"
WEBHOOK_TIMESTAMP_HEADER = "layerx-webhook-timestamp"
WEBHOOK_KEY_HEADER = "layerx-webhook-key-id"
WEBHOOK_SIGNATURE_HEADER = "layerx-webhook-signature"

DEFAULT_MAXIMUM_AGE_MS = 5 * 60 * 1000
DEFAULT_LEASE_MS = 60 * 1000

WebhookClaimResult = Literal["claimed", "processing", "completed", "conflict"]
WebhookConsumeResult = Literal["processed", "duplicate", "processing"]


@dataclass(frozen=True)
class WebhookRequestHeaders:
    id: str
    timestamp: str
    key_id: str
    signature: str


@dataclass(frozen=True)
class WebhookDeliveryClaim:
    delivery_id: str
    payload_digest: str
    lease_until_ms: int


class WebhookDeliveryStore(Protocol):
    async def claim(self, value: WebhookDeliveryClaim) -> WebhookClaimResult: ...
    async def complete(self, delivery_id: str, payload_digest: str) -> None: ...
    async def release(self, delivery_id: str, payload_digest: str) -> None: ...


@dataclass
class _DeliveryEntry:
    payload_digest: str
    lease_until_ms: int
    completed: bool


class SingleProcessWebhookDeliveryStore:
    __slots__ = ("_entries", "_lock", "_now")

    def __init__(self, now: Callable[[], int] | None = None) -> None:
        self._entries: dict[str, _DeliveryEntry] = {}
        self._lock = Lock()
        self._now = now if now is not None else _now_ms

    async def claim(self, value: WebhookDeliveryClaim) -> WebhookClaimResult:
        async with self._lock:
            existing = self._entries.get(value.delivery_id)
            if existing is None:
                self._entries[value.delivery_id] = _DeliveryEntry(
                    payload_digest=value.payload_digest,
                    lease_until_ms=value.lease_until_ms,
                    completed=False,
                )
                return "claimed"
            if existing.payload_digest != value.payload_digest:
                return "conflict"
            if existing.completed:
                return "completed"
            if existing.lease_until_ms > self._now():
                return "processing"
            existing.lease_until_ms = value.lease_until_ms
            return "claimed"

    async def complete(self, delivery_id: str, payload_digest: str) -> None:
        async with self._lock:
            existing = self._entries.get(delivery_id)
            if existing is None or existing.payload_digest != payload_digest:
                raise MiddlewareError(MiddlewareErrorCode.WEBHOOK_REPLAY)
            existing.completed = True
            existing.lease_until_ms = 0

    async def release(self, delivery_id: str, payload_digest: str) -> None:
        async with self._lock:
            existing = self._entries.get(delivery_id)
            if existing is not None and existing.payload_digest == payload_digest and not existing.completed:
                del self._entries[delivery_id]


class VerifiedWebhookConsumer:
    __slots__ = ("_keys", "_deliveries", "_maximum_age_ms", "_lease_ms", "_now")

    def __init__(
        self,
        public_keys: Mapping[str, bytes],
        deliveries: WebhookDeliveryStore,
        maximum_age_ms: int = DEFAULT_MAXIMUM_AGE_MS,
        lease_ms: int = DEFAULT_LEASE_MS,
        now: Callable[[], int] | None = None,
    ) -> None:
        if len(public_keys) == 0 or maximum_age_ms <= 0 or lease_ms <= 0:
            raise MiddlewareError(MiddlewareErrorCode.INVALID_WEBHOOK)
        self._keys = dict(public_keys)
        self._deliveries = deliveries
        self._maximum_age_ms = maximum_age_ms
        self._lease_ms = lease_ms
        self._now = now if now is not None else _now_ms

    async def consume(
        self,
        raw_body: bytes,
        headers: WebhookRequestHeaders,
        handle: Callable[[Mapping[str, JsonValue], str], Awaitable[None]],
    ) -> WebhookConsumeResult:
        now = self._now()
        timestamp_ms = parse_canonical_integer(headers.timestamp) * 1000
        if (
            not _bounded(headers.id, 255)
            or not _identifier(headers.key_id, 64)
            or timestamp_ms > now + 30_000
            or now - timestamp_ms > self._maximum_age_ms
        ):
            raise MiddlewareError(MiddlewareErrorCode.INVALID_WEBHOOK)
        public_key = self._keys.get(headers.key_id)
        if public_key is None or len(public_key) != 32:
            raise MiddlewareError(MiddlewareErrorCode.INVALID_WEBHOOK)
        signature = parse_webhook_signature(headers.signature)
        prefix = f"{headers.id}.{headers.timestamp}.".encode("utf-8")
        if not verify_ed25519(public_key, signature, prefix + raw_body):
            raise MiddlewareError(MiddlewareErrorCode.INVALID_WEBHOOK)
        payload_digest = sha256_digest(raw_body).hex()
        claim = await self._deliveries.claim(
            WebhookDeliveryClaim(
                delivery_id=headers.id,
                payload_digest=payload_digest,
                lease_until_ms=now + self._lease_ms,
            )
        )
        if claim == "conflict":
            raise MiddlewareError(MiddlewareErrorCode.WEBHOOK_REPLAY)
        if claim == "completed":
            return "duplicate"
        if claim == "processing":
            return "processing"
        try:
            event = _decode_event(raw_body)
            await handle(event, headers.id)
            await self._deliveries.complete(headers.id, payload_digest)
        except BaseException:
            await self._deliveries.release(headers.id, payload_digest)
            raise
        return "processed"


def parse_webhook_signature(value: str) -> bytes:
    encoded = value[3:] if value.startswith("v1=") else ""
    signature = decode_base64(encoded, MiddlewareErrorCode.INVALID_WEBHOOK)
    if len(signature) != 64:
        raise MiddlewareError(MiddlewareErrorCode.INVALID_WEBHOOK)
    return signature


def parse_canonical_integer(value: str) -> int:
    if fullmatch(r"0|[1-9][0-9]*", value) is None:
        raise MiddlewareError(MiddlewareErrorCode.INVALID_WEBHOOK)
    return int(value)


def _decode_event(raw_body: bytes) -> Mapping[str, JsonValue]:
    try:
        event = json.loads(raw_body.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise MiddlewareError(MiddlewareErrorCode.INVALID_WEBHOOK) from error
    if not isinstance(event, dict):
        raise MiddlewareError(MiddlewareErrorCode.INVALID_WEBHOOK)
    return event


def _now_ms() -> int:
    return int(time() * 1000)


def _bounded(value: str, limit: int) -> bool:
    return 0 < len(value) <= limit and "\0" not in value


def _identifier(value: str, limit: int) -> bool:
    return _bounded(value, limit) and fullmatch(r"[A-Za-z0-9._-]+", value) is not None
