# walletConnectorContent.tsx

## Overview

`WalletConnectContent` is the main content of the wallet connector modal/sheet: shows steps (create account, enable trading), optional referral code input, remember-me switch, action buttons (sign-in / enable trading), and disconnect. Supports Ledger (Solana) and error handling.

## Exports

### `WalletConnectContentProps`

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `initAccountState` | `AccountStatusEnum` | No | Initial account status (default `NotConnected`) |
| `signIn` | `() => Promise<any>` | Yes | Create account (sign-in) |
| `enableTrading` | `(remember: boolean) => Promise<any>` | Yes | Enable trading (create paxeer-network key) |
| `enableTradingComplted` | `() => Promise<void>` | No | Called after enable-trading success |
| `onCompleted` | `() => void` | No | Called when flow completes (alternative to `close`) |
| `close` | `() => void` | No | Close modal/sheet when flow completes |
| `refCode` | `string` | Yes | Referral code value |
| `setRefCode` | `React.Dispatch<React.SetStateAction<string>>` | Yes | Setter for referral code |
| `helpText` | `string` | No | Validation error for referral code |
| `showRefCodeInput` | `boolean` | Yes | Whether to show referral code input |

### `WalletConnectContent`

React component that renders the steps, referral field (when `showRefCodeInput` and 2 steps), remember-me, primary action (sign-in or enable trading), and disconnect.

## Usage example

```tsx
import { WalletConnectContent, useWalletConnectorBuilder } from "@paxeer-network/ui-connector";

const Widget = (props) => {
  const state = useWalletConnectorBuilder();
  return <WalletConnectContent {...state} {...props} />;
};
```
