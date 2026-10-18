# layout.widget.tsx

## Overview

`AffiliateLayoutWidget` is the public layout component: it uses `useLayoutBuilder()` and renders `AffiliateLayout` with that state and children. Re-exported from `layout/index.ts` as `AffiliateLayoutWidget`.

## Props

| Name | Type | Required | Description |
|------|------|----------|-------------|
| children | ReactNode | Yes | Content to render inside the layout |

## Usage Example

```tsx
import { AffiliateLayoutWidget } from "@paxeer-network/affiliate";
<AffiliateLayoutWidget>
  <DashboardPage />
</AffiliateLayoutWidget>
```
