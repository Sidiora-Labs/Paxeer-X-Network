# icons

## Overview

SVG icon components used in notification and announcement headers. All accept `BaseIconProps` from `@paxeer-network/ui` (e.g. `size`, `color`) and use the shared `Icon` wrapper with `viewBox="0 0 18 18"`.

## Exports

### `BattleIcon`

Gradient icon used for campaign-type announcements.

### `CampaignIcon`

Solid icon variant for campaign (same path as Battle, no gradient).

### `ArrowRightShortIcon`

Right-arrow with gradient; used for “Join now” actions.

### `FundIcon`

Fund/listing icon (opacity 0.8).

### `AnnouncementIcon`

Bell/announcement icon; used for general and delisting types.

### `SecurityIcon`

Shield/security icon; used for maintenance type.

## Props (all icons)

All components extend `BaseIconProps` from `@paxeer-network/ui` (e.g. `size`, `color`, `className`). Default `size` is 18.

## Usage example

```tsx
import {
  BattleIcon,
  CampaignIcon,
  ArrowRightShortIcon,
  FundIcon,
  AnnouncementIcon,
  SecurityIcon,
} from "./icons";

<BattleIcon color="white" size={18} />
<FundIcon color="success" />
```
