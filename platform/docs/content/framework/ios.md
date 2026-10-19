# iOS quickstart

Pay from an iOS app. Five lines, and the app holds no long-lived credential at any point.

## The credential model

A phone is not a server. Anything you ship in the binary is public, so the iOS binding is built so that there is nothing worth extracting:

- The app carries a **publishable configuration** only: a service URL, a session broker URL, one or more event public keys, and two timeouts. All of it is safe in a bundle.
- At call time the binding exchanges that configuration for a **short-lived session token** from a broker you run. The token lives in memory, is zeroed after use, and is re-fetched when the service says a capability was refused.
- Your bearer token stays on your server, where it belongs.

`PublishableConfiguration` refuses anything that is not publishable, so you cannot accidentally hand it a secret.

## Before you start

```text
.package(path: "sdk/integrations/ios")
```

| Variable | What it is |
|---|---|
| `LAYERX_SERVICE_URL` | Your LayerX endpoint |
| `LAYERX_SESSION_BROKER_URL` | The broker that mints session tokens for your users |
| `LAYERX_EVENT_PUBLIC_KEY_<key-id>` | An event signing public key you trust |
| `LAYERX_EVENT_MAX_AGE_SECONDS` | Optional. How stale an event may be |
| `LAYERX_REQUEST_TIMEOUT_SECONDS` | Optional |

In a real app these come from your build configuration rather than the process environment; `PublishableConfiguration(declaredKeys:)` and `PublishableConfiguration(contentsOfJSONFile:)` exist for exactly that.

## The integration

```swift sample=mobile-payment-ios
let settings = try PublishableConfiguration(environment: ProcessInfo.processInfo.environment)
let layerx = try LayerXMobile(configuration: settings)
let quote = try await layerx.client.quote(.object(["source": .string(source), "destination": .string(destination), "money": money]))
guard let quoteID = quote.objectValue?["quote_id"]?.stringValue else { fail("move quote omitted quote_id") }
var journey = try await layerx.client.commit(.object(["quote_id": .string(quoteID)]), idempotencyKey: paymentKey)
```

`layerx.client` covers the human plane an app actually needs: `version`, `profile`, `activity`, `activityEntry(id:)`, `journeys`, `journey(id:)`, `quote`, `commit(_:idempotencyKey:)` and a resumable event stream. Path values are validated before they reach a URL, so an identifier containing a slash or a query character is refused rather than used to build a request.

## Verified push and webhook events

`layerx.consume(rawBody:headerFields:handle:)` verifies an event envelope against your configured public keys, refuses it if it is stale, and claims the delivery under a lease so a redelivery runs your handler once. The claim store is pluggable; the default is in-process.

## Never ship a secret

The package includes `layerx-ios-secret-scan`, which walks a built product and reports embedded secrets. Run it in your release pipeline. The configuration's `exemptScannerValues` tells the scanner which strings are legitimately public, so the scan is precise rather than noisy.

## Run the sample

```text
cd platform/docs/samples/mobile-payment-ios
swift build
LAYERX_SERVICE_URL=http://127.0.0.1:9402 LAYERX_SESSION_BROKER_URL=http://127.0.0.1:9410 \
LAYERX_EVENT_PUBLIC_KEY_primary=$(cat ./event-key.hex) \
LAYERX_SOURCE=did:layerx:alice LAYERX_DESTINATION=did:layerx:bob LAYERX_AMOUNT=1250000 LAYERX_CURRENCY=USD \
LAYERX_PAYMENT_KEY=order-2f9c1b7e4a10 swift run MobilePayment
```

The sample is a command-line executable so it runs anywhere Swift does; the API it uses is exactly the one an app target uses.

## Enforced by

| Capability | Layer | What that means here |
|---|---|---|
| Atomic settlement | `protocol` | The committed move applies completely or not at all. |
| Replay refusal | `protocol` | A retried submission from a flaky mobile network cannot apply twice. |
| Refusal to publish a secret | `service` | The configuration accepts publishable values only, and the bundled scanner checks the built product. |
| Verified, replay-protected webhooks | `service` | Event envelopes are signature-checked, age-checked and lease-claimed on device. |
| Idempotent money moves | `service` | `commit` requires the key, so a retry after a dropped connection returns the original journey. |
| Agent tenancy isolation | `agent-layer` | Session tokens are scoped by the broker you run. |
