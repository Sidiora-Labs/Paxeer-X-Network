# etherAdapter.ts

## etherAdapter.ts Responsibility

Implements the legacy `IWalletAdapter` using ethers `BrowserProvider`: call, callOnChain, sendTransaction, getBalance, signTypedData, pollTransactionReceiptWithBackoff, parseUnits/formatUnits, chainId/addresses. Uses ethers-decode-error for error parsing. **@deprecated** in favor of the new WalletAdapter-based EVM adapter.

## etherAdapter.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| EtherAdapterOptions | interface | Config | provider, wallet.name?, chain.id |
| EtherAdapter | class | Impl | IWalletAdapter with ethers |

## EtherAdapter Responsibility

Provides EVM wallet operations for paxeer-network when using the legacy adapter path: contract reads (call/callOnChain), sendTransaction (encodeFunctionData + sendTransaction), getBalance, signTypedData (eth_signTypedData_v4), and polling for transaction receipt.

## EtherAdapter Constructor and Key Methods

| Method | Description |
|--------|-------------|
| constructor(options: WalletAdapterOptions) | Creates BrowserProvider(options.provider, "any"); stores chainId and address |
| call(address, method, params, options) | Gets signer, Contract(abi), contract[method](...params); on error parses with errorDecoder |
| callOnChain(chain, address, method, params, options) | Uses JsonRpcProvider(chain.public_rpc_url), no signer |
| sendTransaction(contractAddress, method, payload, options) | Encodes function data, sends tx via signer.sendTransaction(tx) |
| getBalance(userAddress) | provider.getBalance(userAddress) |
| signTypedData(address, data) | provider.send("eth_signTypedData_v4", [address, data]) |
| pollTransactionReceiptWithBackoff(txHash, baseInterval, maxInterval, maxRetries) | Exponential backoff until receipt or max retries |

## etherAdapter.ts Dependencies and Call Relationships

- **Upstream**: ethers (BrowserProvider, Contract, JsonRpcProvider), ethers-decode-error (ErrorDecoder), ./adapter (IWalletAdapter, WalletAdapterOptions), @paxeer-network/types (API).
- **Downstream**: Apps that still use legacy IWalletAdapter for EVM.

## etherAdapter.ts Errors and Boundaries

- call/sendTransaction/callOnChain: decode raw error via errorDecoder and rethrow DecodedError.
- pollTransactionReceiptWithBackoff: throws "Transaction did not complete after maximum retries." if no receipt.

## etherAdapter.ts Example

```typescript
import { EtherAdapter } from "@paxeer-network/core/wallet/etherAdapter";

const adapter = new EtherAdapter({ provider, address: "0x...", chain: { id: 421614 } });
const balance = await adapter.getBalance("0x...");
const sig = await adapter.signTypedData(address, typedData);
```
