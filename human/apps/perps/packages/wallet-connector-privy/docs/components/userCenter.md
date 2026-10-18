# UserCenter / MwebUserCenter

## Overview

**UserCenter** and **MwebUserCenter** are the account/connect UI used in the header: they show a “Connect wallet” button when not connected, or the formatted address (and optional Privy type icon) when connected. They respect account state (e.g. EnableTradingWithoutConnected) and Abstract chain (AGW address). **MwebUserCenter** is the mobile variant.

## Exports

- **UserCenter(props)** – Desktop account menu content.
- **MwebUserCenter(props)** – Mobile account menu content.

## Props

Both receive extension props; the main ones used:

| Prop | Type | Description |
|------|------|-------------|
| accountState / state | `object` | Account/connection state. |
| disabledConnect | `boolean` | Optional; disables connect button. |

## Behavior

- Not connected or disabled: show “Connect” / “Connect wallet” button that calls `connect()`.
- EnableTradingWithoutConnected: show address only (with LinkDeviceMobile wrapper).
- Connected: show address (and Privy type icon if linkedAccount exists); click opens connect flow (e.g. drawer).

## Usage example

```tsx
// Usually used via installExtension in injectUsercenter.tsx
<UserCenter accountState={state} />
<MwebUserCenter state={state} />
```
