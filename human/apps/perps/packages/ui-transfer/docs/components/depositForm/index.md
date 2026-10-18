# depositForm

> Location: `packages/ui-transfer/src/components/depositForm`

## Overview

Deposit form: UI, script (state/logic), and widget. Exposes hooks for token, chain, action type, native balance, input status, deposit action, yield APY, etc.

## Files

| File | Language | Description |
| ---- | -------- | ----------- |
| [index.ts](./index.ts.md) | TypeScript | Re-exports DepositForm, useDepositFormScript, DepositFormWidget and types |
| [depositForm.ui.tsx](./depositForm.ui.md) | React/TSX | Deposit form presentational component |
| [depositForm.script.tsx](./depositForm.script.md) | TypeScript | useDepositFormScript and options |
| [depositForm.widget.tsx](./depositForm.widget.md) | React/TSX | DepositFormWidget (script + UI) |
| [hooks/index.ts](./hooks/index.md) | TypeScript | Hooks barrel |
| [hooks/useToken.ts](./hooks/useToken.md) | TypeScript | Token selection/state |
| [hooks/useChainSelect.ts](./hooks/useChainSelect.md) | TypeScript | Chain select state |
| [hooks/useActionType.ts](./hooks/useActionType.md) | TypeScript | Deposit action type |
| [hooks/useNativeBalance.ts](./hooks/useNativeBalance.md) | TypeScript | Native token balance |
| [hooks/useInputStatus.ts](./hooks/useInputStatus.md) | TypeScript | Input validation status |
| [hooks/useDepositAction.tsx](./hooks/useDepositAction.md) | React/TSX | Deposit action handler |
| [hooks/useYieldAPY.ts](./hooks/useYieldAPY.md) | TypeScript | Yield APY data |
