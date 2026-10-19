# C# quickstart

Add a payment to a .NET application. Four lines, no protocol vocabulary, no key handling.

## Before you start

```text
dotnet add package LayerX.Sdk
```

| Variable | What it is |
|---|---|
| `LAYERX_API_URL` | The base URL of your environment |
| `LAYERX_API_TOKEN` | A bearer token identifying your account |

## The integration

```csharp sample=first-payment-csharp
using var token = new AccessToken(Encoding.UTF8.GetBytes(apiToken));
var layerx = new PlatformClient(new HumanHttpTransport(new Uri(apiUrl), accessToken: token));
var quote = await layerx.HumanMoveQuoteAsync(JsonValue.Object(new Dictionary<string, JsonValue> { ["source"] = JsonValue.String(source), ["destination"] = JsonValue.String(destination), ["money"] = money }));
var journey = await layerx.HumanMoveCommitAsync(JsonValue.Object(new Dictionary<string, JsonValue> { ["quote_id"] = JsonValue.String(Text(quote, "quote_id")) }), new IdempotencyKey(paymentKey));
```

`AccessToken` is `IDisposable` and zeroes its buffer on disposal, which is why it is declared `using`. `HumanMoveCommitAsync` takes an `IdempotencyKey` as a required parameter, so the compiler will not let you spell a money-moving call without one.

`JsonValue` is a discriminated union with no floating-point case. Amounts stay exact.

## Run the whole sample

```text
cd services/platform/docs/samples/first-payment-csharp
dotnet build --configuration Release
LAYERX_API_URL=http://127.0.0.1:9402 LAYERX_API_TOKEN=$(cat ./token) LAYERX_SOURCE=did:layerx:alice LAYERX_DESTINATION=did:layerx:bob \
LAYERX_AMOUNT=1250000 LAYERX_CURRENCY=USD LAYERX_PAYMENT_KEY=order-2f9c1b7e4a10 \
dotnet run --configuration Release
```

The sample polls the journey until it settles, prints a JSON report of the journey identifier, state and receipt references, and returns exit code 2 when the journey did not complete.

## Handling refusals

`PlatformSdkException` carries the machine code and the retry class. Branch on the code; never on the message.

| Code | What to do |
|---|---|
| `idempotency-required` | You omitted the key |
| `idempotency-conflict` | Same key, different body |
| `rate-limit` | Wait for the carried retry timing |
| `unknown-outcome` | Do not retry. Resolve by receipt lookup under your key |
| `budget-refusal` | The payment exceeded a funded budget |

## Enforced by

| Capability | Layer | What that means here |
|---|---|---|
| Atomic settlement | `protocol` | The committed move applies completely or not at all. |
| Replay refusal | `protocol` | A retried submission cannot apply twice. |
| Idempotent money moves | `service` | The commit signature makes the key impossible to omit. |
| Quote then commit | `service` | The quote you committed is the one that executes. |
| Done means verified | `service` | The `done` state is backed by receipt evidence. |
