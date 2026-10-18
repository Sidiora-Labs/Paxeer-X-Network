# type.ts

## Overview

Defines shared types and enums for the chain selector: a single chain item shape and the mainnet/testnet classification.

## Exports

### TChainItem

Type describing one selectable chain.

| Property   | Type    | Required | Description                          |
|-----------|---------|----------|--------------------------------------|
| `name`    | `string`| Yes      | Display name of the chain.           |
| `id`      | `number`| Yes      | Chain ID (e.g. for `ChainIcon`).     |
| `lowestFee` | `boolean` | No    | Whether the chain has lowest fee (e.g. bridgeless). |
| `isTestnet` | `boolean` | Yes    | `true` for testnet, `false` for mainnet. |

### ChainType (enum)

Used for tab selection and filtering.

| Member    | Value      | Description   |
|-----------|------------|---------------|
| `Mainnet` | `"Mainnet"`| Mainnet tab.  |
| `Testnet` | `"Testnet"`| Testnet tab.  |

## Usage example

```typescript
import { TChainItem, ChainType } from "@paxeer-network/ui-chain-selector";

const chain: TChainItem = {
  name: "Arbitrum One",
  id: 42161,
  lowestFee: true,
  isTestnet: false,
};

if (chain.isTestnet) {
  // show in testnet tab
}
// ChainType.Mainnet | ChainType.Testnet for tab value
```
