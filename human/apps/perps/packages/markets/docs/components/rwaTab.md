# rwaTab

## Overview

RWA (Real World Asset) tab label components for markets tabs. Exports: `RwaIconTab` (icon + "RWA" text), `RwaTab` (text + "New" badge). Use in tab lists to indicate RWA market section.

## RwaIconTab props

| Prop | Type | Default | Description |
|------|------|---------|-------------|
| `iconSize` | number | 12 | Size of RwaIcon |

## Usage example

```tsx
import { RwaIconTab, RwaTab } from "@paxeer-network/markets";

<RwaIconTab iconSize={14} />
<RwaTab />
```
