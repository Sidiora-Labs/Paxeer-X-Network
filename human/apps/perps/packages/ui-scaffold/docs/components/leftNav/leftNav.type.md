# leftNav.type

> Location: `packages/ui-scaffold/src/components/leftNav/leftNav.type.ts`

## Overview

Types for the left navigation: props and menu item shape (with optional icon, trailing, customRender, onlyInMainAccount, isSecondary).

## Exports

### `LeftNavProps`

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| leading | `ReactNode` | No | Custom leading content |
| menus | `LeftNavItem[]` | No | Menu items |
| twitterUrl | `string` | No | Twitter link |
| telegramUrl | `string` | No | Telegram link |
| discordUrl | `string` | No | Discord link |
| duneUrl | `string` | No | Dune link |
| feedbackUrl | `string` | No | Feedback link (deprecated; does not control leftNav footer UI) |
| customFooter | `ReactNode` | No | Slot for TradingView T&C custom content in leftNav drawer bottom (mweb). If not provided, default `Charts powered by TradingView` link will be rendered. |
| customLeftNav | `ReactNode` | No | Custom left nav content |

### `LeftNavItem`

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| name | `string` | Yes | Label |
| href | `string` | Yes | Link URL |
| target | `string` | No | Link target |
| icon | `ReactNode` | No | Icon |
| trailing | `ReactNode` | No | Trailing node |
| customRender | `(option) => ReactNode` | No | Custom row render |
| onlyInMainAccount | `boolean` | No | Show only in main account (default false) |
| isSecondary | `boolean` | No | Secondary style, grouped below primary |
