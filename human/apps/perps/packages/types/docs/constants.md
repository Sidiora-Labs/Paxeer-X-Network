# constants

## Overview

Central constants and enums for account state, system state, exchange status, network IDs, chain IDs (testnet/mainnet), media breakpoints, token addresses, and chain/token info presets for testnets.

## Exports

### Enums

| Enum | Description |
|------|-------------|
| `AccountStatusEnum` | EnableTradingWithoutConnected, NotConnected, Connected, NotSignedIn, SignedIn, DisabledTrading, EnableTrading |
| `SystemStateEnum` | Loading, Error, Ready |
| `ExchangeStatusEnum` | Normal, Maintain |
| `NetworkStatusEnum` | unknown, unsupported, supported |

### Types

| Type | Description |
|------|-------------|
| `NetworkId` | `"testnet" \| "mainnet"` |

### Chain ID constants

- `ARBITRUM_TESTNET_CHAINID`, `ARBITRUM_TESTNET_CHAINID_HEX`
- `ARBITRUM_MAINNET_CHAINID`, `ARBITRUM_MAINNET_CHAINID_HEX`
- `SOLANA_TESTNET_CHAINID`, `SOLANA_MAINNET_CHAINID`
- `STORY_TESTNET_CHAINID`, `MONAD_TESTNET_CHAINID`, `ABSTRACT_TESTNET_CHAINID`, `ABSTRACT_MAINNET_CHAINID`
- `BSC_TESTNET_CHAINID`, `MANTLE_TESTNET_CHAINID`, `ETHEREUM_MAINNET_CHAINID`
- `ABSTRACT_CHAIN_ID_MAP` (Set)

### Other constants

| Name | Type | Description |
|------|------|-------------|
| `MEDIA_TABLET` | string | CSS media query `(max-width: 768px)` |
| `DEPOSIT_FEE_RATE` | number | 1.05 |
| `MaxUint256` | bigint | Maximum uint256 value |
| `nativeTokenAddress` | string | `"0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE"` |
| `nativeETHAddress` | string | `"0x0000...0000"` (deprecated) |
| `EMPTY_LIST` | ReadonlyArray | `[]` |
| `EMPTY_OBJECT` | Record | `{}` |
| `EMPTY_OPERATION` | function | No-op `() => {}` |

### Functions

| Function | Description |
|----------|-------------|
| `isNativeTokenChecker(address: string)` | Returns true if address is native token or legacy zero address |

### Chain/Token presets

- `ArbitrumSepoliaChainInfo`, `AbstractTestnetChainInfo`, `AbstractTestnetTokenInfo`
- `SolanaDevnetChainInfo`, `SolanaDevnetTokenInfo`, `ArbitrumSepoliaTokenInfo`
- `TesnetTokenFallback(testnetTokens)` – returns fallback token list for testnet

## Usage example

```typescript
import {
  AccountStatusEnum,
  ARBITRUM_MAINNET_CHAINID,
  isNativeTokenChecker,
  MaxUint256,
} from "@paxeer-network/types";
```
