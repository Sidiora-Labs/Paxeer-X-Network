# Python quickstart

Add a payment to a Python application. Seven lines, no protocol vocabulary, no key handling.

## Before you start

```text
python3 -m pip install layerx-sdk
```

| Variable | What it is |
|---|---|
| `LAYERX_API_URL` | The base URL of your environment |
| `LAYERX_API_TOKEN` | A bearer token identifying your account |

## The integration

```python sample=first-payment-python
from layerx_sdk import IdempotencyKey, ProductionClient
from layerx_transport import HumanApiTransport

def open_layerx(api_url: str, api_token: str) -> ProductionClient:
    return ProductionClient(HumanApiTransport(api_url, api_token))

def pay(layerx, source, destination, money, payment_key):
    quote = layerx.human("move.quote", {"source": source, "destination": destination, "money": money})
    return layerx.human("move.commit", {"quote_id": quote["quote_id"]}, idempotency_key=IdempotencyKey(payment_key))
```

`ProductionClient` refuses `move.commit` without an idempotency key before anything reaches the network, so a missing key is a local error rather than a duplicate payment.

> `layerx-sdk` currently ships the client, the operation catalogue and the typed error taxonomy, but not an HTTP transport for the human plane. The sample includes a complete one, `layerx_transport.py`, in a region marked `plumbing`: it validates the endpoint scheme, refuses non-loopback `http://`, holds the token in `SecretBytes`, maps the service's typed error codes onto `SdkErrorCode`, and honours the declared retriability. Copy it as-is until the package ships its own.

## The transport the sample supplies

```python sample=first-payment-python file=layerx_transport.py region=plumbing
from __future__ import annotations

import json
import urllib.error
import urllib.parse
import urllib.request
from collections.abc import Mapping

from layerx_sdk import IdempotencyKey, PlatformSdkError, SdkErrorCode, SecretBytes

MAXIMUM_RESPONSE_BYTES = 8 * 1024 * 1024
LOOPBACK_HOSTS = frozenset({"localhost", "127.0.0.1", "::1"})

ROUTES: Mapping[str, tuple[str, str, bool]] = {
    "journey.get": ("GET", "/v1/journeys/{journey_id}", True),
    "move.commit": ("POST", "/v1/moves", False),
    "move.quote": ("POST", "/v1/moves/quote", False),
    "version": ("GET", "/v1/version", True),
}

SERVICE_CODES: Mapping[str, SdkErrorCode] = {
    "conflict": SdkErrorCode.IDEMPOTENCY_CONFLICT,
    "forbidden": SdkErrorCode.CAPABILITY_REFUSAL,
    "rate-limited": SdkErrorCode.RATE_LIMIT,
    "refused-by-budget": SdkErrorCode.BUDGET_REFUSAL,
    "refused-by-capability": SdkErrorCode.CAPABILITY_REFUSAL,
    "refused-by-limit": SdkErrorCode.BUDGET_REFUSAL,
    "refused-by-policy": SdkErrorCode.POLICY_REFUSAL,
    "refused-by-protocol": SdkErrorCode.CORE_REJECTION,
    "session-expired": SdkErrorCode.CAPABILITY_REFUSAL,
    "step-up-required": SdkErrorCode.CAPABILITY_REFUSAL,
    "unauthenticated": SdkErrorCode.CAPABILITY_REFUSAL,
    "unavailable": SdkErrorCode.TRANSPORT_FAILURE,
    "upstream-degraded": SdkErrorCode.TRANSPORT_FAILURE,
}

RETRY_CLASSES: Mapping[str, str] = {
    "final": "never",
    "retriable": "safe",
    "retriable-after": "after",
    "structural": "never",
}


class HumanApiTransport:
    def __init__(self, base_url: str, bearer_token: str, timeout_seconds: float = 30.0) -> None:
        parsed = urllib.parse.urlsplit(base_url)
        if parsed.scheme not in ("http", "https") or not parsed.hostname or parsed.username:
            raise PlatformSdkError(SdkErrorCode.INVALID_ARGUMENT, "never")
        if parsed.scheme == "http" and parsed.hostname not in LOOPBACK_HOSTS:
            raise PlatformSdkError(SdkErrorCode.INVALID_ARGUMENT, "never")
        if not bearer_token or "\r" in bearer_token or "\n" in bearer_token:
            raise PlatformSdkError(SdkErrorCode.INVALID_ARGUMENT, "never")
        if timeout_seconds <= 0:
            raise PlatformSdkError(SdkErrorCode.INVALID_ARGUMENT, "never")
        self._base_url = base_url.rstrip("/")
        self._token = SecretBytes(bearer_token.encode("utf-8"))
        self._timeout_seconds = timeout_seconds

    def call(
        self,
        plane: str,
        operation: str,
        request: object,
        idempotency_key: IdempotencyKey | None,
    ) -> object:
        if plane != "human":
            raise PlatformSdkError(SdkErrorCode.UNAVAILABLE_CAPABILITY, "never")
        route = ROUTES.get(operation)
        if route is None:
            raise PlatformSdkError(SdkErrorCode.UNAVAILABLE_CAPABILITY, "never")
        method, template, bodyless = route
        parameters = request if isinstance(request, Mapping) else {}
        path = self._resolve(template, parameters)
        body = None if bodyless else json.dumps(request, separators=(",", ":")).encode("utf-8")
        http = urllib.request.Request(self._base_url + path, data=body, method=method)
        http.add_header("Accept", "application/json")
        http.add_header("User-Agent", "layerx-docs-python/0.1.0")
        if body is not None:
            http.add_header("Content-Type", "application/json")
        if idempotency_key is not None:
            http.add_header("Idempotency-Key", str(idempotency_key))
        self._token.use(lambda secret: http.add_header("Authorization", "Bearer " + bytes(secret).decode("utf-8")))
        status, encoded = self._send(http)
        return self._decode(status, encoded)

    def destroy(self) -> None:
        self._token.destroy()

    def _resolve(self, template: str, parameters: Mapping[str, object]) -> str:
        path = template
        while "{" in path:
            start = path.index("{")
            end = path.index("}", start)
            name = path[start + 1 : end]
            value = parameters.get(name)
            if not isinstance(value, str) or not value:
                raise PlatformSdkError(SdkErrorCode.INVALID_ARGUMENT, "never")
            path = path[:start] + urllib.parse.quote(value, safe="") + path[end + 1 :]
        return path

    def _send(self, http: urllib.request.Request) -> tuple[int, bytes]:
        try:
            with urllib.request.urlopen(http, timeout=self._timeout_seconds) as response:
                return response.status, response.read(MAXIMUM_RESPONSE_BYTES + 1)
        except urllib.error.HTTPError as failure:
            with failure:
                return failure.code, failure.read(MAXIMUM_RESPONSE_BYTES + 1)
        except urllib.error.URLError:
            raise PlatformSdkError(SdkErrorCode.TRANSPORT_FAILURE, "safe") from None
        except TimeoutError:
            raise PlatformSdkError(SdkErrorCode.DEADLINE, "safe") from None

    def _decode(self, status: int, encoded: bytes) -> object:
        if len(encoded) > MAXIMUM_RESPONSE_BYTES:
            raise PlatformSdkError(SdkErrorCode.DECODE_FAILURE, "never")
        try:
            envelope = json.loads(encoded.decode("utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError):
            raise PlatformSdkError(SdkErrorCode.DECODE_FAILURE, "never") from None
        if not isinstance(envelope, dict) or not isinstance(envelope.get("ok"), bool):
            raise PlatformSdkError(SdkErrorCode.DECODE_FAILURE, "never")
        trace = envelope.get("trace")
        if not isinstance(trace, str) or not trace:
            raise PlatformSdkError(SdkErrorCode.DECODE_FAILURE, "never")
        if envelope["ok"]:
            if not 200 <= status < 300 or "error" in envelope or "result" not in envelope:
                raise PlatformSdkError(SdkErrorCode.DECODE_FAILURE, "never")
            return envelope["result"]
        if 200 <= status < 300:
            raise PlatformSdkError(SdkErrorCode.DECODE_FAILURE, "never")
        raise self._refusal(envelope.get("error"), trace)

    def _refusal(self, error: object, trace: str) -> PlatformSdkError:
        if not isinstance(error, dict):
            return PlatformSdkError(SdkErrorCode.DECODE_FAILURE, "never")
        classification = error.get("retry")
        retry = RETRY_CLASSES.get(classification) if isinstance(classification, str) else None
        if retry is None:
            return PlatformSdkError(SdkErrorCode.DECODE_FAILURE, "never", request_id=trace)
        after = error.get("retry_after_ms")
        code = error.get("code")
        return PlatformSdkError(
            SERVICE_CODES.get(code, SdkErrorCode.CORE_REJECTION) if isinstance(code, str) else SdkErrorCode.CORE_REJECTION,
            retry,
            request_id=trace,
            retry_after_ms=after if isinstance(after, int) and not isinstance(after, bool) else None,
        )
```

## Run the whole sample

```text
cd services/platform/docs/samples/first-payment-python
python3 -m pip install -r requirements.txt
LAYERX_API_URL=http://127.0.0.1:9402 LAYERX_API_TOKEN=$(cat ./token) LAYERX_SOURCE=did:layerx:alice LAYERX_DESTINATION=did:layerx:bob \
LAYERX_AMOUNT=1250000 LAYERX_CURRENCY=USD LAYERX_PAYMENT_KEY=order-2f9c1b7e4a10 python3 main.py
```

It polls `journey.get` until the journey settles, prints a JSON report of the journey identifier, state and receipt references, and exits non-zero unless the journey reached `done` or `done-finalised`.

## Amounts

`ProtocolAmount` is an `int` subclass that refuses anything that is not an exact non-negative integer, including a bool and a leading-zero string. Amounts cross the wire as decimal strings, never as JSON numbers.

## Handling refusals

`PlatformSdkError` carries `code` and a retry class. Branch on the code.

| Code | What to do |
|---|---|
| `idempotency-required` | You omitted the key. Fix the call |
| `idempotency-conflict` | Same key, different body. Reuse the original result |
| `rate-limit` | Wait for the carried retry timing |
| `unknown-outcome` | Do not retry. Resolve by receipt lookup under your key |
| `budget-refusal` | The payment exceeded a funded budget |

## Enforced by

| Capability | Layer | What that means here |
|---|---|---|
| Atomic settlement | `protocol` | The committed move applies completely or not at all. |
| Conserved supply | `protocol` | No transport bug can create or destroy value. |
| Idempotent money moves | `service` | Repeating the commit with the same key returns the original journey. |
| Quote then commit | `service` | The quote you committed is the one that executes. |
| Done means verified | `service` | The `done` state is backed by receipt evidence. |
