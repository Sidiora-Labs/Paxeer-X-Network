# vaults.ts

## vaults.ts responsibility

Provides vault copy: available on, header title and description, introduction (TVL, depositors), all vaults, list (pool name, my deposits, all-time pnl, operate), card (paxeer-network title/description, TVL, 30D APY, all-time return/tooltip, my deposits/earnings, account balance, more/less, launching soon, view more, status active/closing/closed), deposit (est. shares, shares, lockup duration, latest deposit), withdraw (est. price per share, est. receiving amount, latest withdraw, dialog title/withdrawal amount/estimated receiving/note, initiate withdrawal, process steps, up to 6 hours), operation errors (switch account, min deposit, min withdrawal).

## vaults.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| vaults | object | Key-value map | Keys under "vaults.*" |
| Vaults | type | typeof vaults | Type export |

## vaults.ts Example

```typescript
t("vaults.header.title");
t("vaults.card.apy");
t("vaults.withdraw.dialog.title");
```
