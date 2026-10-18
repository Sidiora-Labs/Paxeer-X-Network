# chain

## Overview

Chain ID helpers: parse hex or number, convert between hex and int, and detect testnet and Solana chains using `@paxeer-network/types` chain ID constants.

## Exports

### hex2int

Parses a hex chain ID string to number.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| chainId | string | Yes | Hex string (e.g. `"0x1"`) |

**Returns:** `number`

---

### int2hex

Converts numeric chain ID to hex string with `0x` prefix.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| chainId | number | Yes | Numeric chain ID |

**Returns:** `string` — e.g. `"0x1"`.

---

### praseChainId (parseChainId)

Normalizes chain ID to number (string → hex2int, number → passthrough).

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| chainId | string \| number | Yes | Chain ID in either form |

**Returns:** `number`

---

### praseChainIdToNumber / parseChainIdToNumber

Parses to number only when the value is a hex string (starts with `0x`, rest valid hex). Otherwise returns the value as number.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| chainId | string \| number | Yes | Chain ID |

**Returns:** `number`

---

### isTestnet

Returns whether the chain ID is a known testnet (Arbitrum, Solana, Story, Monad, Abstract, BSC testnets).

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| chainId | number | Yes | Chain ID |

**Returns:** `boolean`

---

### isSolana

Returns whether the chain ID is Solana testnet or mainnet.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| chainId | number | Yes | Chain ID |

**Returns:** `boolean`

## Usage example

```typescript
import {
  hex2int,
  int2hex,
  praseChainId,
  parseChainIdToNumber,
  isTestnet,
  isSolana,
} from "@paxeer-network/utils";

hex2int("0x66eeb");           // number
int2hex(421614);              // "0x66eeb"
praseChainId("0x1");          // 1
parseChainIdToNumber(421614); // 421614
isTestnet(421614);            // true/false
isSolana(chainId);            // true/false
```
