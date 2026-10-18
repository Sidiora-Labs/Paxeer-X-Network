# operationPayload

## Overview

Maps operation type and role to a numeric payload type used for vault operations (e.g. deposit/withdraw as LP/SP).

## Exports

### getToAccountPayloadType(type: OperationType, role: RoleType): number

| type | role | Return |
|------|------|--------|
| DEPOSIT | LP | 0 |
| WITHDRAWAL | LP | 1 |
| DEPOSIT | SP | 2 |
| WITHDRAWAL | SP | 3 |
| (other) | — | 1 |

## Usage example

```typescript
import { getToAccountPayloadType } from "../utils/operationPayload";
import { OperationType, RoleType } from "../types/vault";
const payloadType = getToAccountPayloadType(OperationType.DEPOSIT, RoleType.LP); // 0
```
