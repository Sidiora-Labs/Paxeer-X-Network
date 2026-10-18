# useWalletConnectorBuilder.ts

## Overview

Hook that builds wallet connector state and handlers: account status, sign-in, enable-trading, referral code validation, and optional bind. Used by `WalletConnectorWidget` to drive `WalletConnectContent`.

## Exports

### `useWalletConnectorBuilder`

Returns a readonly object with:

| Property | Type | Description |
|----------|------|-------------|
| `enableTrading` | `(remember: boolean) => Promise<any>` | Creates paxeer-network key (enable trading); validates referral code when `showRefCodeInput` is true |
| `initAccountState` | `AccountStatus` | Current account status from `useAccount().state.status` |
| `signIn` | `() => Promise<any>` | Creates account (sign-in); validates referral code when `showRefCodeInput` is true |
| `enableTradingComplted` | `() => void` | Callback on enable-trading success: toast, optional bind referral code, clears `referral_code` from localStorage |
| `refCode` | `string` | Referral code input (from localStorage or user) |
| `setRefCode` | `React.Dispatch<React.SetStateAction<string>>` | Setter for referral code |
| `helpText` | `string` | Validation error message for referral code |
| `showRefCodeInput` | `boolean` | Whether to show referral code input (no existing code and not loading) |

## Dependencies

- `@paxeer-network/hooks`: `useAccount`, `useGetReferralCode`, `useLazyQuery`, `useMutation`
- `@paxeer-network/i18n`: `useTranslation`
- `@paxeer-network/ui`: `toast`

## Usage example

```tsx
import { useWalletConnectorBuilder } from "@paxeer-network/ui-connector";
import { WalletConnectContent } from "./walletConnectorContent";

const Widget = () => {
  const state = useWalletConnectorBuilder();
  return <WalletConnectContent {...state} />;
};
```
