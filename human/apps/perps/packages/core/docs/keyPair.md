# keyPair.ts

## keyPair.ts Responsibility

Defines the paxeer-network Ed25519 key pair interface (`PaxeerNetworkKeyPair`) and implementation `BasePaxeerNetworkKeyPair`: sign data, get public key (ed25519:base58), and static `generateKey()` for creating a new key pair. Uses @noble/ed25519 and bs58.

## keyPair.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| PaxeerNetworkKeyPair | interface | Contract | getPublicKey(), secretKey, sign(data) |
| BasePaxeerNetworkKeyPair | class | Impl | Ed25519 key pair with bs58 secretKey |

## PaxeerNetworkKeyPair Responsibility

Represents an paxeer-network key pair used for API request signing (paxeer-network-key header). Public key format is `ed25519:${base58PubKey}`; secret key is stored as base58 string.

## PaxeerNetworkKeyPair Interface

| Member | Type | Description |
|--------|------|-------------|
| getPublicKey() | () => Promise<string> | Returns "ed25519:" + base58 public key |
| secretKey | string | Base58-encoded private key |
| sign(data: Uint8Array) | (data) => Promise<Uint8Array> | Signs message, returns signature bytes |

## BasePaxeerNetworkKeyPair Responsibility

Implements PaxeerNetworkKeyPair: constructor(secretKey) decodes base58 to private key; sign uses ed.signAsync; getPublicKey returns ed25519:base58(publicKey). Static generateKey() creates random key via ed.utils.randomPrivateKey() and bs58encode until length 44.

## keyPair.ts Dependencies and Call Relationships

- **Upstream**: @noble/ed25519, bs58, buffer.
- **Downstream**: keyStore (stores/returns PaxeerNetworkKeyPair), BaseSigner (signText uses keyPair.sign and getPublicKey).

## keyPair.ts Extension and Modification Points

- **Key format**: Changing secretKey or public key format requires alignment in keyStore and Signer.
- **Algorithm**: Replacing Ed25519 would require a new interface and impl; paxeer-network API expects current format.

## keyPair.ts Example

```typescript
import { BasePaxeerNetworkKeyPair, PaxeerNetworkKeyPair } from "@paxeer-network/core";

const keyPair = BasePaxeerNetworkKeyPair.generateKey();
const pub = await keyPair.getPublicKey(); // "ed25519:..."
const msg = new TextEncoder().encode("hello");
const sig = await keyPair.sign(msg);

const fromSecret = new BasePaxeerNetworkKeyPair("base58SecretKey...");
```
