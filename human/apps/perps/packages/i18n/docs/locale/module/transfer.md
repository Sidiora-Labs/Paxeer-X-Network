# transfer.ts

## transfer.ts responsibility

Provides transfer and deposit/withdraw copy: network, lowest fee, Web3 wallet labels, broker account, quantity invalid, insufficient balance/allowance, reject transaction, deposit (approve, requested/completed/failed, fee unavailable, not enough gas, exceed cap, close to max limit, est. gas fee, destination gas fee), withdraw (unsupported chain/network/token, cross-chain confirm/process/warning, vault warning, min amount, LTV error, requested/completed/failed, other account, account ID tips and invalid, available tooltip, external wallet add), internal transfer (from/to, success, unsettled tooltip, settle PnL description, errors), swap deposit (cross-swap/swap notice, bridging, deposit, view status, failed, slippage, swap/bridge fee, minimum received, confirm swap, average swap time, dialog title, FAQs, not enough liquidity), convert completed/failed, deposit cap, convert rate, collateral contribution, LTV description/tooltip/current/isolated mode, convert, global max qty and gas fee errors, deposit status (pending/completed one/multiple), yield reminder (earn APY, rewards, distribution, disclaimer).

## transfer.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| transfer | object | Key-value map | Keys under "transfer.*" |
| Transfer | type | typeof transfer | Type export |

## transfer.ts key groups (sample)

| Theme | Examples |
|-------|----------|
| Deposit | transfer.deposit.approve, transfer.deposit.estGasFee.tooltip |
| Withdraw | transfer.withdraw.crossChain.confirmWithdraw, transfer.withdraw.available.tooltip |
| Internal | transfer.internalTransfer.success, transfer.internalTransfer.error.transferToSelf |
| Swap | transfer.swapDeposit.bridging, transfer.swapDeposit.slippage.slippageTolerance.description |

## transfer.ts Example

```typescript
t("transfer.deposit.approve");
t("transfer.LTV.description");
t("transfer.internalTransfer.settlePnl.description");
```
