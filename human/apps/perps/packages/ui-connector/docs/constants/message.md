# message.ts

## Overview

Defines the shape of alert messages and default English labels and descriptions for the wallet connector flow (connect wallet, wrong network, enable trading, sign-in).

## Exports

### `alertMessages` (type)

| Property | Type | Description |
|----------|------|-------------|
| `connectWallet?` | `string` | Connect wallet message |
| `switchChain?` | `string` | Wrong network / switch chain message |
| `enableTrading?` | `string` | Enable trading message |
| `signin?` | `string` | Sign-in message |

### `LABELS`

Default button labels:

| Key | Value |
|-----|--------|
| `connectWallet` | `"Connect wallet"` |
| `switchChain` | `"Wrong network"` |
| `enableTrading` | `"Enable trading"` |
| `signin` | `"Sign in"` |

### `DESCRIPTIONS`

Default descriptions (typed as `alertMessages`):

| Key | Value |
|-----|--------|
| `connectWallet` | `"Please connect wallet before starting to trade"` |
| `switchChain` | `"Please switch to a supported network to continue"` |
| `enableTrading` | `"Please enable trading before starting to trade"` |
| `signin` | `"Please sign in before starting to trade"` |

## Usage example

```ts
import { LABELS, DESCRIPTIONS, type alertMessages } from "./constants/message";

const custom: alertMessages = {
  connectWallet: "Connect your wallet to continue",
};
```
