# adapter.ts

## adapter.ts Responsibility

Defines the legacy wallet adapter interface `IWalletAdapter` (chainId, addresses, parseUnits, formatUnits, send, sendTransaction, getTransactionRecipect, signTypedData, pollTransactionReceiptWithBackoff, getBalance, call, callOnChain, on, off) and types `WalletAdapterOptions`, `getWalletAdapterFunc`. Used by EtherAdapter and Web3WalletAdapter; new code should prefer the WalletAdapter interface in walletAdapter.ts.

## adapter.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| IWalletAdapter | interface | Contract | Legacy adapter API |
| WalletAdapterOptions | type | Config | { provider, address, chain: { id } } |
| getWalletAdapterFunc | type | Factory | (options: WalletAdapterOptions) => IWalletAdapter |

## IWalletAdapter Responsibility

Legacy interface for a wallet that has chainId, address(es), can send transactions, sign typed data, call contracts, and poll receipts. EtherAdapter implements it with ethers BrowserProvider.

## IWalletAdapter Key Members

| Member | Type | Description |
|--------|------|-------------|
| chainId | number (get) | Chain ID |
| addresses | string (get) | Wallet address |
| set chainId | number | Set chain ID |
| parseUnits, formatUnits | (amount, decimals) => string | Unit conversion |
| send(method, params) | Promise<any> | Provider send |
| sendTransaction(contractAddress, method, payload, options) | Promise<TransactionResponse> | Contract tx |
| getTransactionRecipect(txHash) | Promise<any> | Receipt |
| signTypedData(address, data) | Promise<string> | EIP-712 sign |
| pollTransactionReceiptWithBackoff(txHash, ...) | Promise<any> | Poll receipt |
| getBalance(userAddress) | Promise<any> | Balance |
| call(address, method, params, options) | Promise<any> | Contract read |
| callOnChain(chain, address, method, params, options) | Promise<any> | Read on other chain |
| on, off | events | Provider events |

## adapter.ts Dependencies and Call Relationships

- **Upstream**: ethers (TransactionResponse), @paxeer-network/types (API).
- **Downstream**: etherAdapter.ts, web3Adapter.ts implement; wallet/index.ts re-exports as WalletAdapter type.

## adapter.ts Example

```typescript
import type { IWalletAdapter, WalletAdapterOptions } from "@paxeer-network/core";

const options: WalletAdapterOptions = { provider, address: "0x...", chain: { id: 421614 } };
const adapter: IWalletAdapter = new EtherAdapter(options);
await adapter.signTypedData(address, typedData);
```
