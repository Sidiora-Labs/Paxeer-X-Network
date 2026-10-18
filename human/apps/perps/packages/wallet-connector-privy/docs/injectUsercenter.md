# injectUsercenter

## Overview

Side-effect module that registers **UserCenter** and **MwebUserCenter** as UI extensions for the account menu and mobile account menu. Uses `installExtension` from `@paxeer-network/ui` with scope `["*"]` and positions `AccountMenu` and `MobileAccountMenu`.

## Exports

None (side effects only).

## Behavior

- **account-menu-privy** – Renders `UserCenter` in the desktop account menu.
- **mobile-account-menu-privy** – Renders `MwebUserCenter` in the mobile account menu.

## Usage

Import once at app startup (e.g. from `main.tsx`):

```tsx
import "./injectUsercenter";
```
