# authGuardEmpty.tsx

## Overview

`AuthGuardEmpty` wraps `AuthGuard` with a default empty state as fallback. Accepts optional hint messages and status; uses trade-related tooltip copy for descriptions.

## Exports

### `AuthGuardEmpty`

#### Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `hint` | `{ connectWallet?, signIn?, enableTrading?, wrongNetwork? }` | No | i18n trade tooltips | Override descriptions for each state |
| `status` | `AccountStatusEnum` | No | — | Required account status (passed to `AuthGuard`) |
| `children` | `ReactNode` | No | `<EmptyDataState />` | Content when auth is satisfied |

## Usage example

```tsx
import { AuthGuardEmpty } from "@paxeer-network/ui-connector";

<AuthGuardEmpty status={AccountStatusEnum.EnableTrading}>
  <YourTableOrContent />
</AuthGuardEmpty>
```
