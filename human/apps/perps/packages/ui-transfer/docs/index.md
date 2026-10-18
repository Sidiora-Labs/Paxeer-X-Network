# ui-transfer

> Location: `packages/ui-transfer/src`

## Overview

The **ui-transfer** package provides UI components and utilities for deposit, withdraw, transfer, swap, and convert flows in the paxeer-network ecosystem. It exports forms, hooks, chain/block utilities, and shared types used across transfer-related features.

## Directory structure

| Directory | Description |
| --------- | ----------- |
| [contract](./contract/index.md) | Chain/block time, confirmations, and LayerZero endpoint ID utilities |
| [constants](./constants/index.md) | Yield-bearing asset configuration |
| [types](./types/index.md) | Asset module declarations (images) |
| [components](./components/index.md) | Deposit, withdraw, transfer, swap, convert UI and hooks |

## Top-level files

| File | Language | Description |
| ---- | -------- | ----------- |
| [index.ts](./index.ts.md) | TypeScript | Main package exports (forms, components, utils, types) |
| [types.ts](./types.ts.md) | TypeScript | Shared types: `DST`, `DepositAction`, `WithdrawTo`, `InputStatus` |
| [utils.ts](./utils.ts.md) | TypeScript | Token/transfer helpers, fee decimals, error message mapping |
| [version.ts](./version.md) | TypeScript | Package version and `window.__PAXEER_NETWORK_VERSION__` |
| [icons.tsx](./icons.md) | React/TSX | SVG icons (Exchange, ArrowDown, Refresh, TransferVertical, etc.) |
| [types/assets.d.ts](./types/assets.md) | TypeScript | Module declarations for image assets (png, jpg, svg, etc.) |

## Usage

```ts
import {
  DepositForm,
  WithdrawForm,
  TransferForm,
  ChainSelect,
  QuantityInput,
  Fee,
  YieldBearingReminder,
} from "@paxeer-network/ui-transfer";
import { getTokenByTokenList, checkIsAccountId, getTransferErrorMessage } from "@paxeer-network/ui-transfer";
import type { DST, DepositAction, WithdrawTo, InputStatus } from "@paxeer-network/ui-transfer";
```
