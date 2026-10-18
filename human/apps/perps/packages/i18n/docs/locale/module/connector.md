# connector.ts

## connector.ts responsibility

Provides wallet and onboarding copy: testnet/mainnet, connect/disconnect wallet, create account, enable trading, switch network, wrong network tooltip, expired session, remember me, referral code placeholder and validation, error messages (something went wrong, user rejected), wallet connected/network switched, trade/setUp tooltips (connect, create account, enable trading), Ledger sign message failed, and Privy login, email, Google, Twitter, Telegram, wallet add/create, terms of use, supported chains, no wallet, PWA add-to-home instructions.

## connector.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| connector | object | Key-value map | Keys under "connector.*" |
| Connector | type | typeof connector | Type export |

## connector.ts key groups (sample)

| Theme | Examples |
|-------|----------|
| Connect | connector.connectWallet, connector.createAccount.description |
| Trading | connector.enableTrading, connector.trade.connectWallet.tooltip |
| Network | connector.wrongNetwork.tooltip, connector.switchChain.failed |
| Privy | connector.privy.loginIn, connector.privy.addEvmWallet |

## connector.ts Example

```typescript
t("connector.connectWallet");
t("connector.wrongNetwork.tooltip");
t("connector.privy.termsOfUse");
```
