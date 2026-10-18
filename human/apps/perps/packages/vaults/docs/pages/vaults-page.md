# VaultsPage (pages/index.tsx)

## Overview

Main vaults page: wraps content in `VaultsProvider`, then renders lazy-loaded header, introduction, and all-vaults widgets. Layout is responsive (mobile vs desktop padding/gap) and uses `@paxeer-network/ui` (cn, useScreen).

## Exports

### VaultsPageProps

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| className | string | No | Extra class for the outer container |
| config | VaultsPageConfig | No | headerImage, overallInfoBrokerIds |

### VaultsPage

- **Type**: `FC<VaultsPageProps>`
- **Behavior**: Renders a full-width min-height container with `VaultsProvider`, then a content div (max-width 1200px) with `React.Suspense` around `VaultsHeaderWidget`, `VaultsIntroductionWidget`, `AllVaultsWidget`.

## Usage example

```tsx
import { VaultsPage } from "@paxeer-network/vaults";
<VaultsPage config={{ overallInfoBrokerIds: "paxeer-network,woofi_pro" }} />
```
