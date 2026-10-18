# wallet

## Overview

Types for the current chain and WebSocket wallet status. Uses `API.Chain` from the API types for chain info.

## Exports

### CurrentChain

| Property | Type | Description |
|----------|------|-------------|
| id | number | Chain ID |
| info | API.Chain | Chain details (dexs, network_infos, token_infos, etc.) |

### WS_WalletStatusEnum

| Value | Description |
|-------|-------------|
| NO | Not applicable |
| FAILED | Failed |
| PENDING | Pending |
| PROCESSING | Processing |
| COMPLETED | Completed |

## Usage example

```typescript
import type { CurrentChain } from "@paxeer-network/types";
const current: CurrentChain = { id: 42161, info: chainFromApi };
```
