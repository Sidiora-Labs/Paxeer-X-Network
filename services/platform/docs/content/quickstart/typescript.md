# TypeScript quickstart

Add a payment to a Node.js application. Eight lines, no protocol vocabulary, no key handling.

## Before you start

```text
npm install @sidiora/layerx-sdk @sidiora/layerx-buyer-middleware
```

Set two values in your environment. Both are given to you; neither is signing material.

| Variable | What it is |
|---|---|
| `LAYERX_API_URL` | The base URL of your environment |
| `LAYERX_API_TOKEN` | A bearer token identifying your account |

## The integration

```js sample=first-payment-typescript
import { LayerXPaymentHttpTransport } from "@sidiora/layerx-buyer-middleware";
import { ProductionClient, SecretBytes, idempotencyKey } from "@sidiora/layerx-sdk";
export const openLayerX = (apiUrl, apiToken) => new ProductionClient(
  new LayerXPaymentHttpTransport({ baseUrl: apiUrl, bearerToken: new SecretBytes(new TextEncoder().encode(apiToken)) }));
export const pay = async (layerx, source, destination, money, paymentKey) => {
  const quote = await layerx.human("move.quote", { source, destination, money });
  return layerx.human("move.commit", { quote_id: quote.quote_id }, { idempotencyKey: idempotencyKey(paymentKey) });
};
```

That is all of it. `openLayerX` builds a client over an HTTPS transport that holds your token in a `SecretBytes` container - redacted in logs, zeroed when destroyed. `pay` quotes the move, then commits the quote under an idempotency key you choose.

Loopback URLs are allowed for the emulator. Any other `http://` URL is refused before a request is made, so a misconfigured environment cannot leak a bearer token in clear text.

## Run the whole sample

The sample directory adds the parts that are yours rather than LayerX's: reading the environment, polling until the journey settles, and printing a report.

```text
cd services/platform/docs/samples/first-payment-typescript
npm install
LAYERX_API_URL=http://127.0.0.1:9402 LAYERX_API_TOKEN=$(cat ./token) LAYERX_SOURCE=did:layerx:alice LAYERX_DESTINATION=did:layerx:bob \
LAYERX_AMOUNT=1250000 LAYERX_CURRENCY=USD LAYERX_PAYMENT_KEY=order-2f9c1b7e4a10 node index.mjs
```

It prints the journey identifier, the final state, the receipt evidence references, and - if the payment was refused - who refused it and whether any money left the account. It exits non-zero unless the journey reached `done` or `done-finalised`.

## Choosing an idempotency key

Derive it from the thing being paid for, not from the attempt. `order-2f9c1b7e4a10` is right; a UUID generated at the call site is the classic mistake, because the retry generates a new one and pays twice. Calling `move.commit` without a key fails inside the SDK with `idempotency-required` rather than reaching the network.

## Handling refusals

Every failure arrives as a `PlatformSdkError` carrying a machine code and a retry class. Switch on `error.code`; do not parse messages.

| Code | What to do |
|---|---|
| `idempotency-required` | You omitted the key. Fix the call |
| `idempotency-conflict` | Same key, different body. Reuse the original result |
| `rate-limit` | Wait for `error.retryAfterMs` |
| `unknown-outcome` | Do not retry. Resolve by looking up the receipt under your key |
| `budget-refusal` | The payment exceeded a funded budget. This is a protocol refusal |

## Enforced by

| Capability | Layer | What that means here |
|---|---|---|
| Atomic settlement | `protocol` | The committed move applies completely or not at all. |
| Replay refusal | `protocol` | A retried submission cannot apply twice, whatever your key handling does. |
| Idempotent money moves | `service` | Repeating the commit with the same key returns the original journey. |
| Quote then commit | `service` | The fee ceiling and arrival expectation you saw are the ones you committed to. |
| Done means verified | `service` | The `done` state your poll observes is backed by receipt evidence. |
