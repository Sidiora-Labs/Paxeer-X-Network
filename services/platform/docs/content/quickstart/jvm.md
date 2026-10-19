# Java and Kotlin quickstart

Add a payment to a JVM service. Six lines, no protocol vocabulary, no key handling.

## Before you start

Add the SDK to your build:

```text
<dependency>
  <groupId>com.sidiora.layerx</groupId>
  <artifactId>layerx-sdk</artifactId>
  <version>0.1.0</version>
</dependency>
```

| Variable | What it is |
|---|---|
| `LAYERX_API_URL` | The base URL of your environment |
| `LAYERX_API_TOKEN` | A bearer token identifying your account |

## The integration

```java sample=first-payment-jvm
var credential = new HttpProductionTransport.BearerCredential(new SecretBytes(apiToken.getBytes(StandardCharsets.UTF_8)));
var layerx = new ProductionClient(HttpProductionTransport.create(URI.create(apiUrl), URI.create(apiUrl), credential));
var quote = layerx.human("move.quote", Map.of("source", source, "destination", destination, "money", money),
    JsonNode.class, ProductionClient.Options.none()).toCompletableFuture().join();
var journey = layerx.human("move.commit", Map.of("quote_id", quote.path("quote_id").asText()), JsonNode.class,
    ProductionClient.Options.idempotent(new IdempotencyKey(paymentKey))).toCompletableFuture().join();
```

`SecretBytes` holds the token, redacts itself in `toString` and zeroes its array when closed. `HttpProductionTransport.create` takes the agent-plane and human-plane base URIs; here they are the same endpoint.

Every call returns a `CompletionStage`. The sample joins it because it is a command-line program; in a service, compose the stages instead.

`ProductionClient.Options.idempotent(...)` is required on `move.commit`. `Options.none()` on that operation fails locally with `idempotency-required`.

Kotlin callers get the same API through the `LayerXKotlin` extensions: `human(operation, request, KClass, options)` with `Options` defaulted, plus `protocolAmount`, `idempotencyKey` and `IdempotencyKey.asOptions`.

## Run the whole sample

```text
cd services/platform/docs/samples/first-payment-jvm
mvn -q package
LAYERX_API_URL=http://127.0.0.1:9402 LAYERX_API_TOKEN=$(cat ./token) LAYERX_SOURCE=did:layerx:alice LAYERX_DESTINATION=did:layerx:bob \
LAYERX_AMOUNT=1250000 LAYERX_CURRENCY=USD LAYERX_PAYMENT_KEY=order-2f9c1b7e4a10 mvn -q exec:java
```

## Reading responses safely

Use `path` rather than `get` when reading a field out of a response node. `path` returns a missing node; `get` returns `null` and turns a protocol-level absence into a `NullPointerException` three frames away from the cause.

## Handling refusals

`PlatformSdkException` carries the machine code and the retry class.

| Code | What to do |
|---|---|
| `idempotency-required` | Pass `Options.idempotent(...)` |
| `idempotency-conflict` | Same key, different body |
| `rate-limit` | Wait for the carried retry timing |
| `unknown-outcome` | Do not retry. Resolve by receipt lookup under your key |
| `budget-refusal` | The payment exceeded a funded budget |

## Enforced by

| Capability | Layer | What that means here |
|---|---|---|
| Atomic settlement | `protocol` | The committed move applies completely or not at all. |
| Replay refusal | `protocol` | A retried submission cannot apply twice. |
| Idempotent money moves | `service` | Repeating the commit with the same key returns the original journey. |
| Quote then commit | `service` | The quote you committed is the one that executes. |
| Done means verified | `service` | The `done` state is backed by receipt evidence. |
