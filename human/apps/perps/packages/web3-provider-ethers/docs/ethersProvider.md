# ethersProvider

## Overview

`EthersProvider` is a TypeScript class that implements the `Web3Provider` interface from `@paxeer-network/default-evm-adapter`. It wraps `ethers` v6 `BrowserProvider` (and an underlying EIP-1193 provider) to provide signing, contract calls, transaction sending, and balance queries used by paxeer-network's EVM integration.

## Exports

### `EthersProvider` (class)

Implements `Web3Provider` with the following members.

#### Properties (get/set)

| Name | Type | Description |
|------|------|-------------|
| `provider` | `Eip1193Provider` (setter) | Sets the underlying EIP-1193 provider and creates an internal `BrowserProvider` with network `"any"`. |
| `browserProvider` | `BrowserProvider` (getter) | Returns the ethers `BrowserProvider`; throws if not initialized. |

#### Methods

| Method | Returns | Description |
|--------|--------|-------------|
| `parseUnits(amount, decimals?)` | `string` | Uses `ethers.parseUnits` to convert a decimal string to wei/smallest unit. |
| `formatUnits(amount, decimals?)` | `string` | Uses `ethers.formatUnits` to format a wei/smallest-unit string to a decimal string. |
| `signTypedData(address, toSignatureMessage)` | `Promise<string>` | Sends `eth_signTypedData_v4` via the browser provider for the given address and message. |
| `call(address, method, params, options)` | `Promise<any>` | Calls a contract method (read or write). Uses wallet-specific `writeContract` for `approve` when `agwWallet` is present; otherwise uses ethers `Contract` with the signer. Errors are parsed via `parseError`. |
| `send(method, params)` | `Promise<any>` | Forwards to `browserProvider.send(method, params)`. |
| `sendTransaction(contractAddress, method, payload, options)` | `Promise<any>` | Encodes function data and sends a transaction (with special handling for `depositTo` and agw wallet). Uses `parseError` on failure. |
| `pollTransactionReceiptWithBackoff(txHash, baseInterval?, maxInterval?, maxRetries?)` | `Promise<TransactionReceipt \| null>` | Polls for transaction receipt with exponential backoff (defaults: base 1000 ms, max 6000 ms, 30 retries). |
| `callOnChain(chain, address, method, params, options)` | `Promise<any>` | Creates a `JsonRpcProvider` from `chain.public_rpc_url` and calls the contract method without signer (read-only on that chain). |
| `getBalance(userAddress)` | `Promise<bigint>` | Returns the account balance from the browser provider. |

#### Method parameters (summary)

- **call**: `address` (contract), `method` (name), `params` (array), `options: { abi }`.
- **sendTransaction**: `contractAddress`, `method`, `payload: { from, to?, data, value? }`, `options: { abi }`.
- **pollTransactionReceiptWithBackoff**: `txHash`, optional `baseInterval`, `maxInterval`, `maxRetries`.
- **callOnChain**: `chain: API.NetworkInfos`, `address`, `method`, `params`, `options: { abi }`.

## Usage example

```typescript
import { EthersProvider } from "@paxeer-network/web3-provider-ethers";

// Assume window.ethereum or another EIP-1193 provider
const provider = new EthersProvider();
provider.provider = window.ethereum;

const balance = await provider.getBalance(userAddress);
const hash = await provider.sendTransaction(
  contractAddress,
  "approve",
  { from: userAddress, data: [spender, amount] },
  { abi: erc20Abi }
);
const receipt = await provider.pollTransactionReceiptWithBackoff(hash.hash);
```
