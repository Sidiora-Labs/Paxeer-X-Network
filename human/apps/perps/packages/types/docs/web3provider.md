# web3provider

## Overview

Interface for a Web3-style provider used to sign typed data (EIP-712), call contract methods, send transactions, and query balance. Integrates with `API.NetworkInfos` for chain-specific calls.

## Exports

### Web3Provider (interface)

| Method | Description |
|--------|-------------|
| `signTypedData(address, toSignatureMessage)` | Returns a promise of the signature string for EIP-712 payload. |
| `call(address, method, params, options?)` | Read-only contract call; options may include `abi`. |
| `sendTransaction(address, method, params, options?)` | Send a transaction; options may include `abi`. |
| `callOnChain(chain, address, method, params, options)` | Same as call but on a specific chain (`API.NetworkInfos`); `options.abi` required. |
| `getBalance()` | Returns promise of balance as string. |

Parameters:

- **address**: User or contract address.
- **method**: Contract method name.
- **params**: Array of method parameters.
- **options**: Optional `{ abi }` for call/sendTransaction; required for callOnChain with `abi`.
- **chain**: `API.NetworkInfos` for the target chain.

## Usage example

```typescript
import type { Web3Provider } from "@paxeer-network/types";
const provider: Web3Provider = {
  signTypedData: async (addr, msg) => await signer.signTypedData(...),
  call: async (addr, method, params, opts) => contract[method](...params),
  sendTransaction: async (...) => ...,
  callOnChain: async (chain, addr, method, params, opts) => ...,
  getBalance: async () => await signer.getBalance(),
};
```
