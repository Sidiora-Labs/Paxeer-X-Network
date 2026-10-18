# ScaffoldProvider

> Location: `packages/ui-scaffold/src/components/scaffold/scaffoldProvider.tsx`

## Overview

Provider component that builds `ScaffoldState` (including `checkChainSupport` from `useChains()` and `useAnnouncement()`), memoizes it, and supplies it via `ScaffoldContext.Provider`. Used internally by `Scaffold`; can be used standalone if you need the same context without the full layout.

## Props

Same as `ScaffoldState` except **omitting**:

- `checkChainSupport` (derived from `useChains` and network)
- `announcementState` (from `useAnnouncement()`)

So: `routerAdapter`, `expanded`, `setExpand`, `topNavbarHeight`, `footerHeight`, `announcementHeight`, plus `children`.

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| routerAdapter | `RouterAdapter` | No | Router adapter |
| expanded | `boolean` | No | Sidebar expanded |
| setExpand | `(expand: boolean) => void` | Yes | Set expand |
| topNavbarHeight | `number` | Yes | Top navbar height |
| footerHeight | `number` | Yes | Footer height |
| announcementHeight | `number` | Yes | Announcement height |
| children | `ReactNode` | — | Tree under context |

## Usage example

```tsx
import { ScaffoldProvider } from "@paxeer-network/ui-scaffold";

<ScaffoldProvider
  setExpand={setExpand}
  topNavbarHeight={56}
  footerHeight={28}
  announcementHeight={0}
>
  {children}
</ScaffoldProvider>
```
