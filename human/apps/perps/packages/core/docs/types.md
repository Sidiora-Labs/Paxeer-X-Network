# types.ts

## types.ts Responsibility

Exports shared types used across the core package. Currently only `Ed25519Keypair` (secretKey and publicKey strings).

## types.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| Ed25519Keypair | interface | Type | secretKey, publicKey strings |

## Ed25519Keypair Fields

| Field | Type | Description |
|-------|------|-------------|
| secretKey | string | Private key representation |
| publicKey | string | Public key representation |

## types.ts Dependencies and Call Relationships

- **Upstream**: None.
- **Downstream**: Re-exported from package entry; may be used by wallet or key-related code for type hints.

## types.ts Example

```typescript
import type { Ed25519Keypair } from "@paxeer-network/core";

const keypair: Ed25519Keypair = { secretKey: "...", publicKey: "..." };
```
