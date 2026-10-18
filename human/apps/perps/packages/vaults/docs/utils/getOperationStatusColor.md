# getOperationStatusColor

## Overview

Maps an operation status string to a UI color key: "success", "danger", or "primary".

## Exports

### getOperationStatusColor(status: string): "success" | "danger" | "primary"

- `"completed"` → `"success"`
- `"rejected"` or `"failed"` → `"danger"`
- Otherwise → `"primary"`

## Usage example

```typescript
import { getOperationStatusColor } from "../utils/getOperationStatusColor";
const color = getOperationStatusColor("completed"); // "success"
```
