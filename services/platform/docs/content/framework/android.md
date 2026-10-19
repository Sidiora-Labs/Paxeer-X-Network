# Android quickstart

Pay from an Android app. Eight lines, and the app holds no long-lived credential at any point.

## The credential model

Identical to [iOS](framework-ios.html), and for the same reason: anything in an APK is public.

- The app carries a **publishable configuration** only - service URL, session broker URL, event public keys, timeouts.
- The binding exchanges it for **short-lived session tokens** from a broker you run, re-fetching when a capability refusal says the token is stale.
- Your bearer token stays on your server.

## Before you start

```text
<dependency>
  <groupId>com.sidiora.layerx</groupId>
  <artifactId>layerx-android</artifactId>
  <version>0.1.0</version>
</dependency>
```

`PublishableConfiguration` accepts declared keys directly, from a JSON file shipped as a resource, or from the environment:

| Declared key | Environment variable |
|---|---|
| `layerx.service_url` | `LAYERX_SERVICE_URL` |
| `layerx.session_broker_url` | `LAYERX_SESSION_BROKER_URL` |
| `layerx.event_public_key.<key-id>` | `LAYERX_EVENT_PUBLIC_KEY_<KEY_ID>` |
| `layerx.event_max_age_seconds` | `LAYERX_EVENT_MAX_AGE_SECONDS` |
| `layerx.request_timeout_seconds` | `LAYERX_REQUEST_TIMEOUT_SECONDS` |

## The integration

```java sample=mobile-payment-android
static LayerXAndroid openLayerX() {
    return LayerXAndroid.create(PublishableConfiguration.ofEnvironment(System.getenv()));
}

static JsonNode pay(LayerXAndroid layerx, ObjectNode move, String paymentKey) {
    var quote = layerx.client().quote(move).toCompletableFuture().join();
    var commit = layerx.mapper().createObjectNode().put("quote_id", quote.path("quote_id").asText());
    return layerx.client().commit(commit, new IdempotencyKey(paymentKey)).toCompletableFuture().join();
}
```

`LayerXAndroid` is `AutoCloseable`; closing it shuts the transport and the session provider down together, so a session token does not outlive the object that fetched it. On devices whose platform provider lacks Ed25519 the binding installs Bouncy Castle at position one, so event verification behaves identically across API levels.

`layerx.client()` covers the same human-plane surface as iOS: `version`, `profile`, `activity`, `activityEntry`, `journeys`, `journey`, `quote`, `commit` and a resumable stream. Every call returns a `CompletionStage`, so it composes with whatever concurrency your app already uses.

## Verified push and webhook events

`layerx.consume(rawBody, headerFields, handler)` verifies the envelope against your configured public keys, refuses stale deliveries and claims each delivery under a lease so a redelivery runs your handler once.

## Never ship a secret

`EmbeddedSecretScan` walks a built artifact for embedded credentials. Run it in your release pipeline, with `configuration.exemptScannerValues()` supplying the strings that are legitimately public.

## Run the sample

```text
cd services/platform/docs/samples/mobile-payment-android
mvn -q package
LAYERX_SERVICE_URL=http://127.0.0.1:9402 LAYERX_SESSION_BROKER_URL=http://127.0.0.1:9410 \
LAYERX_EVENT_PUBLIC_KEY_PRIMARY=$(cat ./event-key.hex) \
LAYERX_SOURCE=did:layerx:alice LAYERX_DESTINATION=did:layerx:bob LAYERX_AMOUNT=1250000 LAYERX_CURRENCY=USD \
LAYERX_PAYMENT_KEY=order-2f9c1b7e4a10 mvn -q exec:java
```

The sample runs on a plain JVM so it needs no device or emulator, and the API it uses is the one an `Activity` uses.

## Enforced by

| Capability | Layer | What that means here |
|---|---|---|
| Atomic settlement | `protocol` | The committed move applies completely or not at all. |
| Replay refusal | `protocol` | A retried submission from a flaky mobile network cannot apply twice. |
| Refusal to publish a secret | `service` | The configuration accepts publishable values only, and the bundled scanner checks the built artifact. |
| Verified, replay-protected webhooks | `service` | Event envelopes are signature-checked, age-checked and lease-claimed on device. |
| Idempotent money moves | `service` | `commit` requires the key, so a retry after a dropped connection returns the original journey. |
| Agent tenancy isolation | `agent-layer` | Session tokens are scoped by the broker you run. |
