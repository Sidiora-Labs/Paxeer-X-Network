# signer.ts

## signer.ts Responsibility

Defines the paxeer-network API request signer: `MessageFactor` (url, method, optional data), `SignedMessagePayload` (paxeer-network-key, paxeer-network-timestamp, paxeer-network-signature, optional paxeer-network-account-id), `Signer` interface, and `BaseSigner` implementation that builds a message string from timestamp + method + url + JSON(data) and signs it with PaxeerNetworkKeyPair from keyStore, returning base64url signature and public key.

## signer.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| MessageFactor | type | Input | { url, method: "GET"\|"POST"\|"PUT"\|"DELETE", data? } |
| SignedMessagePayload | type | Output | { "paxeer-network-key", "paxeer-network-timestamp", "paxeer-network-signature", "paxeer-network-account-id"? } |
| Signer | interface | Contract | sign(messageFactor, timestamp?), signText(text) |
| BaseSigner | class | Impl | Uses PaxeerNetworkKeyStore to get PaxeerNetworkKeyPair and sign |

## Signer Responsibility

Produces paxeer-network API headers (paxeer-network-key, paxeer-network-timestamp, paxeer-network-signature) for authenticated requests. Signing is over the canonical string: timestamp + method + url + (optional) JSON.stringify(data).

## MessageFactor Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| url | string | Yes | Request path/URL |
| method | "GET" \| "POST" \| "PUT" \| "DELETE" | Yes | HTTP method |
| data | any | No | Request body (object); included in message if present |

## SignedMessagePayload Fields

| Field | Type | Description |
|-------|------|-------------|
| paxeer-network-key | string | Public key (ed25519:...) |
| paxeer-network-timestamp | string | Timestamp used in message |
| paxeer-network-signature | string | Base64url signature |
| paxeer-network-account-id | string | Optional; set by Account for some calls |

## BaseSigner Execution Flow

1. sign(messageFactor, timestamp): Build msgStr = timestamp + method + url + (data ? JSON.stringify(data)); call signText(msgStr); return { "paxeer-network-key": publicKey, "paxeer-network-timestamp": timestamp, "paxeer-network-signature": signature }.
2. signText(text): Get paxeerNetworkKeyPair from keyStore.getPaxeerNetworkKey(); Buffer.from(text) → sign with keyPair.sign; encode signature as base64url; return { signature, publicKey: await getPublicKey() }.

## signer.ts Dependencies and Call Relationships

- **Upstream**: keyStore (PaxeerNetworkKeyStore), utils (base64url, getTimestamp), buffer.
- **Downstream**: Account (signData, signGetMessageFactor), Assets (withdraw, internalTransfer use account.signer).

## signer.ts Errors and Boundaries

| Scenario | Condition | Behavior | Handling |
|----------|-----------|----------|----------|
| No paxeer-network key | signText when getPaxeerNetworkKey() null | throw "paxeerNetworkKeyPair is not defined" | Ensure keyStore has key for current address |

## signer.ts Example

```typescript
import { BaseSigner, MessageFactor, SignedMessagePayload } from "@paxeer-network/core";
import { LocalStorageStore } from "@paxeer-network/core";

const keyStore = new LocalStorageStore("testnet");
// ... setAddress, setKey so getPaxeerNetworkKey() returns key
const signer = new BaseSigner(keyStore);

const payload: MessageFactor = { method: "GET", url: "/v1/client/sub_account" };
const headers: SignedMessagePayload = await signer.sign(payload);
// Use headers["paxeer-network-key"], headers["paxeer-network-timestamp"], headers["paxeer-network-signature"] in request
```
