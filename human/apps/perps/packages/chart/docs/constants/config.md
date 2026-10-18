# config

## Overview

Default margin configuration used by the Chart component when no margin prop is provided.

## Exports

### DeafultMargin

| Property | Type | Value |
|----------|------|-------|
| top | number | 20 |
| right | number | 12 |
| bottom | number | 20 |
| left | number | 30 |

## Usage example

```ts
import { DeafultMargin } from "./constants/config";

<Chart data={data} x="date" y="value" margin={DeafultMargin} />
```
