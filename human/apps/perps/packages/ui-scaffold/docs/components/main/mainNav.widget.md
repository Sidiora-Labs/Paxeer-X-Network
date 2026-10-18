# mainNav.widget

> Location: `packages/ui-scaffold/src/components/main/mainNav.widget.tsx`

## Overview

Top bar component: logo, language switcher, sub-account, link device, chain menu, wallet connect, notifications, main nav menus; on mobile also left nav and QR scanner. Uses `useMainNavScript` and supports custom render via `customRender`.

## Props (`MainNavWidgetProps`)

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| leading | `ReactNode` | No | Leading content |
| trailing | `ReactNode` | No | Trailing content |
| logo | `{ src, alt }` | No | Logo image |
| mainMenus | `MainNavItem[]` | No | Main nav items |
| campaigns | `MainNavItem` | No | @deprecated use mainMenus |
| campaignPosition | `CampaignPositionEnum` | No | @deprecated use mainMenus |
| initialMenu | `string \| string[]` | No | Initial menu path |
| onItemClick | `(options) => void` | No | Menu item click |
| leftNav | `LeftNavProps` | No | Left nav (mobile) |
| customLeftNav | `ReactNode` | No | Custom left nav |
| className | `string` | No | Root class |
| classNames | `object` | No | Slots: root, mainNav, logo, account, chains, campaignButton |
| customRender | `(components) => ReactNode` | No | Custom render for title, languageSwitcher, subAccount, linkDevice, chainMenu, walletConnect, notify, mainNav, accountSummary, leftNav, scanQRCode |

## Usage example

```tsx
import { MainNavWidget } from "@paxeer-network/ui-scaffold";

<MainNavWidget mainMenus={menus} leftNav={{ menus: leftMenus }} />
```
