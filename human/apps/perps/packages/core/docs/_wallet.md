# _wallet.ts

## _wallet.ts Responsibility

Defines a legacy wallet client abstraction: `WalletClient` interface (address, getBalance, deposit, connect) and `BaseWalletClient` / `SimpleWallet` with stub or unimplemented methods. Deprecated in favor of the wallet adapter pattern in `wallet/`.

## _wallet.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| WalletClient | interface | Contract | address, getBalance, deposit, connect |
| BaseWalletClient | abstract class | Base | address getter; abstract getBalance, deposit, connect |
| SimpleWallet | class | Impl | Extends BaseWalletClient; all methods throw "Method not implemented." |

## WalletClient Responsibility

Legacy interface for a wallet that has an address and can return balance, perform deposit, and connect. Not used by current Account/Assets flow which uses WalletAdapter.

## WalletClient Members

| Member | Type | Description |
|--------|------|-------------|
| address | getter string | Wallet address |
| getBalance() | () => Promise<any> | Balance |
| deposit() | () => Promise<any> | Deposit action |
| connect() | () => Promise<any> | Connect action |

## _wallet.ts Dependencies and Call Relationships

- **Upstream**: None.
- **Downstream**: Not used in current core/src; deprecated. New code should use wallet/walletAdapter and WalletAdapterManager.

## _wallet.ts Example

```typescript
// Deprecated; use WalletAdapter from wallet/ instead.
import { WalletClient, SimpleWallet } from "@paxeer-network/core";
const client: WalletClient = new SimpleWallet("0x...");
```
