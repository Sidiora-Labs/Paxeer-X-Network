# types.ts

> Location: `packages/ui-transfer/src/types.ts`

## Overview

Shared types for the ui-transfer package: token/asset representation, deposit actions, withdraw destination, and input validation status.

## Exports

### DST

Token/asset type used across deposit, swap, and transfer flows.

| Property | Type | Required | Description |
| -------- | ---- | -------- | ----------- |
| symbol | string | Yes | Token symbol |
| address | string | No | Contract address |
| decimals | number | No | Token decimals |
| chainId | number | No | Chain ID |
| network | string | No | Network name |

### DepositAction (enum)

| Value | Description |
| ----- | ----------- |
| Deposit | Deposit only |
| Approve | Approve only |
| ApproveAndDeposit | Approve and deposit in one flow |

### WithdrawTo (enum)

| Value | Description |
| ----- | ----------- |
| Wallet | Withdraw to connected web3 wallet |
| Account | Withdraw to another account ID |

### InputStatus (type)

`"error" | "warning" | "success" | "default"` — validation/display state for inputs.

## Usage example

```ts
import type { DST, DepositAction, WithdrawTo, InputStatus } from "@paxeer-network/ui-transfer";

const token: DST = { symbol: "USDC", decimals: 6 };
const action = DepositAction.ApproveAndDeposit;
const to = WithdrawTo.Wallet;
const status: InputStatus = "success";
```
