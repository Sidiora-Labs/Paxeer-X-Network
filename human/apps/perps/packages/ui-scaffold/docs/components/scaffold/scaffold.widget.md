# Scaffold (scaffold.widget)

> Location: `packages/ui-scaffold/src/components/scaffold/scaffold.widget.tsx`

## Overview

Root layout component. Uses `useScaffoldScript` to decide desktop vs mobile, then renders either `DesktopScaffold` or `MobileScaffold` inside `ScaffoldProvider`. Supports custom left sidebar, top bar, bottom nav, footer, and class names.

## Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| leftSidebar | `ReactNode` | No | — | Custom left sidebar (desktop only) |
| leftSideProps | `SideBarProps` | No | — | Props for default left sidebar (desktop) |
| leftNavProps | `LeftNavProps` | No | — | Custom left nav (mobile only) |
| topBar | `ReactNode` | No | — | Custom top bar |
| mainNavProps | `MainNavWidgetProps` | No | — | Props for main nav |
| bottomNav | `ReactNode` | No | — | Custom bottom nav (mobile only) |
| bottomNavProps | `BottomNavProps` | No | — | Props for bottom nav (mobile only) |
| footer | `ReactNode` | No | — | Custom footer (desktop only) |
| footerProps | `FooterProps` | No | — | Props for footer (desktop only) |
| routerAdapter | `RouterAdapter` | No | — | Router adapter from `@paxeer-network/types` |
| classNames | `object` | No | — | Class names for root, container, content, body, leftSidebar, topNavbar, footer, bottomNav |
| children | `ReactNode` | — | — | Page content |

## Usage example

```tsx
import { Scaffold } from "@paxeer-network/ui-scaffold";

<Scaffold
  mainNavProps={{ mainMenus: [...] }}
  footerProps={{ wsStatus: "connected" }}
  routerAdapter={router}
>
  {children}
</Scaffold>
```
