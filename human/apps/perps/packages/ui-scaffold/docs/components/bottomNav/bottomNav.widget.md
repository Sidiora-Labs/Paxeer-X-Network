# bottomNav.widget

> Location: `packages/ui-scaffold/src/components/bottomNav/bottomNav.widget.tsx`

## Overview

Renders bottom nav on mobile only (uses `useScreen().isMobile`). Exposes `BottomNavWidget` and types `BottomNavItem`, `BottomNavProps`.

## Exports

### `BottomNavItem`

| Property | Type | Description |
|----------|------|-------------|
| name | `string` | Label |
| href | `string` | Link |
| activeIcon | `ReactNode` | Icon when active |
| inactiveIcon | `ReactNode` | Icon when inactive |

### `BottomNavProps`

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| mainMenus | `BottomNavItem[]` | No | Menu items |
| current | `string` | No | Current route/path |
| onRouteChange | `RouterAdapter["onRouteChange"]` | No | Route change callback |

## Usage example

```tsx
import { BottomNavWidget } from "@paxeer-network/ui-scaffold";

<BottomNavWidget mainMenus={items} current={pathname} />
```
