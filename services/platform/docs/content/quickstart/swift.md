# Swift quickstart

Add a payment to a Swift application. Five lines, no protocol vocabulary, no key handling.

## Before you start

Add the package to your `Package.swift`:

```text
.package(path: "sdk/platform/swift")
```

| Variable | What it is |
|---|---|
| `LAYERX_API_URL` | The base URL of your environment |
| `LAYERX_API_TOKEN` | A bearer token identifying your account |

## The integration

```swift sample=first-payment-swift
let token = try AccessToken(Data(apiToken.utf8))
let layerx = PlatformClient(transport: try HumanHTTPTransport(baseURL: serviceURL, accessToken: token))
let quote = try await layerx.humanMoveQuote(.object(["source": .string(source), "destination": .string(destination), "money": money]))
guard let quoteID = quote.objectValue?["quote_id"]?.stringValue else { fail("move quote omitted quote_id") }
var journey = try await layerx.humanMoveCommit(.object(["quote_id": .string(quoteID)]), idempotencyKey: paymentKey)
```

`AccessToken` is a locked container: it refuses empty material, redacts itself as `[REDACTED]`, and zeroes its storage on `destroy()` and on deinit. Every call is `async throws`, and `humanMoveCommit` takes the idempotency key as a required argument, so there is no way to spell a money-moving call without one.

`JSONValue` is a closed enum with `objectValue`, `stringValue` and `integerValue` accessors. It has no floating-point case, so an amount cannot be silently turned into a double.

## Run the whole sample

```text
cd services/platform/docs/samples/first-payment-swift
swift build
LAYERX_API_URL=http://127.0.0.1:9402 LAYERX_API_TOKEN=$(cat ./token) LAYERX_SOURCE=did:layerx:alice LAYERX_DESTINATION=did:layerx:bob \
LAYERX_AMOUNT=1250000 LAYERX_CURRENCY=USD LAYERX_PAYMENT_KEY=order-2f9c1b7e4a10 swift run FirstPayment
```

The sample polls `humanJourneyGet(pathParameters:)` until the journey settles, emits a JSON report, and destroys the token on the way out.

## If you are building an app, not a server

Do not put a bearer token in a client binary. The iOS binding exists precisely for that case: it holds a publishable configuration only and exchanges it for short-lived session tokens through a broker you run. See the [iOS quickstart](framework-ios.html).

## Handling refusals

`PlatformSDKError` carries `code`, `retry`, `requestID`, `protocolResultCode` and `retryAfterMilliseconds`. Catch it specifically.

| Code | What to do |
|---|---|
| `idempotencyRequired` | You omitted the key |
| `idempotencyConflict` | Same key, different body |
| `rateLimit` | Wait for `retryAfterMilliseconds` |
| `unknownOutcome` | Do not retry. Resolve by receipt lookup under your key |
| `budgetRefusal` | The payment exceeded a funded budget |

## Enforced by

| Capability | Layer | What that means here |
|---|---|---|
| Atomic settlement | `protocol` | The committed move applies completely or not at all. |
| Replay refusal | `protocol` | A retried submission cannot apply twice. |
| Idempotent money moves | `service` | The commit signature makes the key impossible to omit. |
| Quote then commit | `service` | The quote you committed is the one that executes. |
| Done means verified | `service` | The `done` state is backed by receipt evidence. |
