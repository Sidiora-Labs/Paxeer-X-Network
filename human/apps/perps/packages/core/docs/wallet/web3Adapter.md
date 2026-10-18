# web3Adapter.ts

## web3Adapter.ts Responsibility

Provides `Web3WalletAdapter` implementing legacy `IWalletAdapter` using the Web3 library: constructor accepts WalletAdapterOptions, sets chainId from options.chain.id (parsed as hex), implements getBalance and deposit (sendTransaction); sendTransaction, call, on, off throw "Method not implemented." Addresses getter throws. Used as a placeholder or for Web3-based integration.

## web3Adapter.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| Web3WalletAdapter | class | Impl | IWalletAdapter with Web3 |

## Web3WalletAdapter Responsibility

Minimal Web3-backed adapter: getBalance via web3.eth.getBalance, deposit via web3.eth.sendTransaction({ from, to, value }). Other IWalletAdapter methods are not implemented.

## Web3WalletAdapter Members

| Member | Implementation |
|--------|----------------|
| constructor(options) | new Web3(options.provider); chainId = parseInt(options.chain.id, 16) |
| get chainId() | return _chainId |
| get addresses() | throw not implemented |
| getBalance(address) | web3.eth.getBalance(address) |
| deposit(from, to, amount) | web3.eth.sendTransaction({ from, to, value: amount }) |
| sendTransaction, call, on, off | throw "Method not implemented." |
| signTypedData | web3.eth.signTypedData(address, data) |
| send | no-op (empty) |

## web3Adapter.ts Dependencies and Call Relationships

- **Upstream**: ethers (TransactionRequest, TransactionResponse), ./adapter (IWalletAdapter, WalletAdapterOptions), Web3.
- **Downstream**: Optional use by apps that rely on Web3 instead of ethers.

## web3Adapter.ts Example

```typescript
import { Web3WalletAdapter } from "@paxeer-network/core/wallet/web3Adapter";

const adapter = new Web3WalletAdapter({ provider, address: "0x...", chain: { id: 0x66eee } });
const balance = await adapter.getBalance("0x...");
```
