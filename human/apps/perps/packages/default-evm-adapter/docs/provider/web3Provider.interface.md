# web3Provider.interface.ts

## web3Provider.interface.ts Responsibility

Defines the Web3 provider contract for the default EVM adapter: `Eip1193Provider` (injected wallet-style provider) and `Web3Provider` (full interface for signing, RPC calls, sending transactions, and polling receipts). Implementations are supplied by consumers; this package only declares the interface.

## web3Provider.interface.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| Eip1193Provider | type | Injected provider | Object with `request({ method, params })` |
| Web3Provider | interface | EVM provider contract | All methods used by DefaultEVMWalletAdapter |

## Eip1193Provider Responsibility

Represents an EIP-1193 compatible provider (e.g. `window.ethereum`). The adapter can assign it to `Web3Provider.provider` so the Web3Provider implementation can forward requests to the wallet.

## Eip1193Provider Shape

| Property | Type | Description |
|----------|------|-------------|
| request | (args: { method: string; params?: any[] }) => Promise\<any\> | Standard EIP-1193 request |

## Web3Provider Responsibility

Interface that the default EVM adapter uses for all chain and signing operations: setter for the underlying EIP-1193 provider, unit parsing/formatting, EIP-712 signing, read call, send transaction, cross-chain call, receipt polling, balance and gas estimation.

## Web3Provider Input and Output

- **Input**: Setter `provider` receives Eip1193Provider; methods receive addresses, method names, params, payloads, options (abi), and optional polling/gas params.
- **Output**: Promises resolving to signing result (string), read result (any), tx result (any), bigint (balance, gas), or receipt (pollTransactionReceiptWithBackoff).

## Web3Provider Properties and Methods

| Member | Type | Description |
|--------|------|-------------|
| set provider(provider) | Eip1193Provider | Set underlying EIP-1193 provider |
| parseUnits(amount, decimals) | (amount: string, decimals: number) => string | Parse human amount to units |
| formatUnits(amount, decimals) | (amount: string, decimals: number) => string | Format units to human amount |
| signTypedData(address, toSignatureMessage) | Promise\<string\> | EIP-712 sign; returns signature |
| send(method, params) | Promise\<any\> | Generic RPC send |
| call(address, method, params, options?) | Promise\<any\> | Contract read (options.abi) |
| sendTransaction(contractAddress, method, payload, options) | Promise\<any\> | Send contract transaction; payload: from, to?, data, value? |
| callOnChain(chain, address, method, params, options) | Promise\<any\> | Contract read on given chain |
| pollTransactionReceiptWithBackoff(txHash, baseInterval?, maxInterval?, maxRetries?) | Promise\<any\> | Poll until receipt or retries exhausted |
| getBalance(userAddress) | Promise\<bigint\> | Native balance |
| getBalances(addresses) | Promise\<any\> | Balances for multiple addresses |
| estimateGasFee(contractAddress, method, payload, options) | Promise\<bigint\> | Estimate gas for payload |

## Web3Provider Dependencies and Callers

- **Upstream**: `@paxeer-network/types` (API.NetworkInfos for callOnChain).
- **Downstream**: `DefaultEVMWalletAdapter` (uses every method); `types.ts` (getWalletAdapterFunc return type and EVMAdapterOptions do not reference Web3Provider directly but adapter expects it).

## Web3Provider Execution Flow (Typical)

1. Consumer creates an implementation of Web3Provider (e.g. wrapping window.ethereum).
2. Adapter is constructed with that Web3Provider.
3. On active(config), adapter may set web3Provider.provider = config.provider.
4. Adapter calls signTypedData for message flows; call/sendTransaction/callOnChain for contract interaction; getBalance/getBalances/estimateGasFee for balance and gas; pollTransactionReceiptWithBackoff after sendTransaction when needed.

## Web3Provider Errors and Boundaries

Not inferable from code: error codes and retry behavior are implementation-defined. Interface does not specify thrown errors; implementers should document their own error and timeout behavior.

## Web3Provider Extension and Modification Points

- New methods would require interface change here and implementation in all Web3Provider implementations; adapter would need to call them if required.
- Payload and options shapes (e.g. abi, value) are part of the contract; changing them is a breaking change for the adapter and all implementations.

## Web3Provider Example

```typescript
import type { Web3Provider, Eip1193Provider } from "@paxeer-network/default-evm-adapter";

const eip1193: Eip1193Provider = window.ethereum;

class MyWeb3Provider implements Web3Provider {
  set provider(p: Eip1193Provider) {
    this._provider = p;
  }
  parseUnits(amount: string, decimals: number) {
    return /* ... */;
  }
  formatUnits(amount: string, decimals: number) {
    return /* ... */;
  }
  async signTypedData(address: string, toSignatureMessage: any) {
    return this._provider.request({
      method: "eth_signTypedData_v4",
      params: [address, toSignatureMessage],
    });
  }
  // ... implement call, sendTransaction, getBalance, etc.
}
```
