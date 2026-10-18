# walletConnector.ts

## walletConnector.ts Responsibility

Defines the `WalletConnector` interface (connect()) and two minimal implementations: `BaseConnector` (connect resolves immediately) and `Blocknative` (connect resolves immediately). Used as a placeholder or extension point for wallet connection flow.

## walletConnector.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| WalletConnector | interface | Contract | connect(): Promise<void> |
| BaseConnector | class | Impl | connect() => Promise.resolve() |
| Blocknative | class | Impl | connect() => Promise.resolve() |

## WalletConnector Responsibility

Abstracts “connect” as a single async step. Implementations can trigger wallet UI or SDK connect; current impls are no-op.

## WalletConnector Methods

| Method | Description |
|--------|-------------|
| connect() | Returns Promise<void>; when to show UI or perform connect is implementation-defined |

## walletConnector.ts Dependencies and Call Relationships

- **Upstream**: None.
- **Downstream**: Not referenced in core/src; likely used by app or wallet UI layer.

## walletConnector.ts Example

```typescript
import { WalletConnector, BaseConnector, Blocknative } from "@paxeer-network/core";

const connector: WalletConnector = new BaseConnector();
await connector.connect();
```
