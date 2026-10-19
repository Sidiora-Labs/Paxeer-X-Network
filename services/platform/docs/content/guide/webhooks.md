# Webhooks

A webhook is an unauthenticated HTTP request from the internet that claims to be from us. Treat it that way. The `VerifiedWebhookConsumer` in `@sidiora/layerx-seller-middleware` - and the equivalents wired into every framework integration - turns that claim into something you can act on, or refuses it.

## The four checks, in order

1. **Shape.** The delivery id must be bounded text, the key id an identifier, the timestamp a canonical integer. Anything else is `invalid-webhook` before a byte of the body is parsed.
2. **Freshness.** More than 30 seconds in the future, or older than the maximum age (5 minutes by default), is refused. This is what bounds the window in which a captured delivery is worth anything.
3. **Signature.** Ed25519 over `"{delivery-id}.{timestamp}." || rawBody`, against the public key named by the `keyId` header. Multiple keys can be declared at once, which is what makes rotation possible without downtime. The signature is verified over the **raw bytes**, so if your framework hands you a parsed body you have already lost - keep the raw body.
4. **Replay.** The delivery store is asked to claim `(deliveryId, payloadDigest)` under a lease.

Only after all four does your handler run, and it runs with the parsed event and the delivery id.

## What the claim returns

| Claim | Result | Why |
|---|---|---|
| new | handler runs | First time this delivery has been seen |
| `completed` | `duplicate` | Already handled - a `200`, not a second effect |
| `processing` | `processing` | Another worker holds the lease |
| `conflict` | `webhook-replay` | Same delivery id, different payload digest |

`conflict` is the important one: it means someone replayed a delivery id with different bytes. That is an attack, not a retry, and it raises.

If your handler throws, the lease is released, so a genuine retry from us can be handled cleanly. If it succeeds, the delivery is completed and can never run again.

## The store is yours, and it must be durable

The bundled `SingleProcessWebhookDeliveryStore` is a correct implementation of the interface and holds its state in memory. It is right for a single process and wrong for two, and it forgets on restart. In production, back `claim`/`complete`/`release` with the same transactional store your business data lives in, so that "the delivery was handled" and "the effect happened" commit or fail together.

## Verifying rather than trusting the payload

A signed webhook proves the message came from the holder of the signing key. It does not prove a payment happened. Where the event asserts money - the merchant settlement event, for example - the handler resolves the receipt reference, verifies the receipt bytes against a batch authorisation, and compares the recomputed receipt digest to the one in the event in constant time. Signature checking authenticates the messenger; receipt verification establishes the fact.

## Declaring keys

Public keys arrive through `LAYERX_WEBHOOK_PUBLIC_KEYS_JSON`, a declared key like any other. It is public material, so it is safe in a bundle. The webhook path itself is declared, not derived, so it appears in exactly one place across your app and the middleware.

## Enforced by

| Capability | Layer | What that means here |
|---|---|---|
| Offline receipt verification | `protocol` | Money-bearing events are settled against receipts, not headers. |
| Verified, replay-protected webhooks | `service` | Shape, freshness, Ed25519 signature and replay claim, in that order. |
| Exactly-once fulfilment | `service` | As durable as the delivery store you supply. |
| Refusal to publish a secret | `service` | Only public verification keys are declared; the signing key never leaves us. |
