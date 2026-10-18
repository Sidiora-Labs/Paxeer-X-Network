# types.ts

## types.ts Responsibility

Defines configuration and factory types for the default EVM adapter: `EVMAdapterOptions` (options passed when activating the adapter) and `getWalletAdapterFunc` (function type that returns a Web3Provider from those options).

## types.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| EVMAdapterOptions | interface | Adapter config | provider, address, chain, contractManager |
| getWalletAdapterFunc | type | Factory | `(options: EVMAdapterOptions) => Web3Provider` |

## EVMAdapterOptions Responsibility

Describes the inputs required to activate or update the EVM wallet adapter: an EIP-1193 provider, the current address and chain, and a contract manager implementing `IContract`.

## EVMAdapterOptions Input and Output

- **Input**: Used as the argument to `active(config)` and `update(config)` on `DefaultEVMWalletAdapter`.
- **Output**: Not applicable (configuration type).

## EVMAdapterOptions Fields

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| provider | Eip1193Provider | Yes | EIP-1193 provider (e.g. from wallet injection) |
| address | string | Yes | Current wallet address |
| chain | { id: number } | Yes | Current chain id |
| contractManager | IContract | Yes | Contract manager for verification contract address and chain calls |

## EVMAdapterOptions Dependencies

- **Eip1193Provider**: From `./provider/web3Provider.interface.ts` (object with `request({ method, params })`).
- **Web3Provider**: From same file; `getWalletAdapterFunc` returns it.
- **IContract**: From `@paxeer-network/core`.

## getWalletAdapterFunc Description

Type alias for a function that takes `EVMAdapterOptions` and returns a `Web3Provider`. Used when the adapter is created from options (e.g. a factory that wraps the injected provider into a Web3Provider implementation).

## types.ts Example

```typescript
import type { EVMAdapterOptions, getWalletAdapterFunc } from "@paxeer-network/default-evm-adapter";
import type { IContract } from "@paxeer-network/core";

const options: EVMAdapterOptions = {
  provider: window.ethereum,
  address: "0x...",
  chain: { id: 421614 },
  contractManager: myContractManager as IContract,
};

const getAdapter: getWalletAdapterFunc = (opts) => myWeb3ProviderImplementation(opts);
const web3Provider = getAdapter(options);
```
