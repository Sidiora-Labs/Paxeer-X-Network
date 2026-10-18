# scaffoldContext

> Location: `packages/ui-scaffold/src/components/scaffold/scaffoldContext.ts`

## Overview

React context for scaffold state: router adapter, expand state, chain support check, navbar/footer/announcement heights, and announcement state from `@paxeer-network/ui-notification`.

## Exports

### `ScaffoldState`

| Field | Type | Description |
|-------|------|-------------|
| routerAdapter | `RouterAdapter \| undefined` | Router adapter |
| expanded | `boolean \| undefined` | Sidebar expanded state |
| setExpand | `(expand: boolean) => void` | Set expand state |
| checkChainSupport | `(chainId: number \| string) => boolean` | Check if chain is supported |
| topNavbarHeight | `number` | Top navbar height |
| footerHeight | `number` | Footer height |
| announcementHeight | `number` | Announcement bar height |
| announcementState | `ReturnType<typeof useAnnouncement>` | Announcement hook state |

### `ScaffoldContext`

React context holding `ScaffoldState`.

### `useScaffoldContext()`

Hook that returns the current `ScaffoldState`.

## Usage example

```tsx
import { useScaffoldContext } from "@paxeer-network/ui-scaffold";

const { checkChainSupport, routerAdapter } = useScaffoldContext();
const ok = checkChainSupport(421614);
```
