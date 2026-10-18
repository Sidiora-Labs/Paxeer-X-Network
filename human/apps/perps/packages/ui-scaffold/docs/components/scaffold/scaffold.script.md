# scaffold.script

> Location: `packages/ui-scaffold/src/components/scaffold/scaffold.script.ts`

## Overview

Hook `useScaffoldScript` computes layout state for the scaffold: refs and heights for top navbar, footer, bottom nav, announcement, expand/collapse state from localStorage, mobile flag, sidebar widths, and passes through options (footerProps, routerAdapter, mainNavProps, bottomNavProps).

## Exports

### `useScaffoldScript(options)`

#### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| options | `ScaffoldProps` | Yes | Same as `Scaffold` props |

#### Returns

Object with: `topNavbarRef`, `footerRef`, `topNavbarHeight`, `footerHeight`, `announcementRef`, `announcementHeight`, `restrictedInfo`, `expand`, `setExpand`, `isMobile`, `sideBarExpandWidth`, `sideBarCollaspedWidth`, `hasLeftSidebar`, `footerProps`, `routerAdapter`, `mainNavProps`, `bottomNavProps`, `bottomNavRef`, `bottomNavHeight`.

## Usage example

```ts
import { useScaffoldScript } from "@paxeer-network/ui-scaffold";

const state = useScaffoldScript(scaffoldProps);
// state.isMobile, state.topNavbarHeight, state.expand, ...
```
