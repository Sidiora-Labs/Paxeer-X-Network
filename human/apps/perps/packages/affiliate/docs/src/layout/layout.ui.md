# layout.ui.tsx

## Overview

`AffiliateLayout` wraps content in a scaffold with a left sidebar. Uses `@paxeer-network/ui` Box and `@paxeer-network/ui-scaffold` Scaffold/SideBar. Props extend `SideBarProps`.

## Props

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| (SideBarProps) | | | | Spread to SideBar (e.g. items, current) |
| children | ReactNode | Yes | | Main content |

## Usage Example

```tsx
import { AffiliateLayout } from "./layout.ui";
<AffiliateLayout items={items} current="/affiliate">
  <PageContent />
</AffiliateLayout>
```
