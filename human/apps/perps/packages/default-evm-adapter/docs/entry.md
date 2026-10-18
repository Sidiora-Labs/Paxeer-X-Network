# index.ts

## index.ts Responsibility

Public entry point for the default-evm-adapter package. Re-exports the package version, the EVM wallet adapter class, and the Web3Provider type. Consumers should import from this module (or the package name) rather than from internal paths.

## index.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| version | default export | Version string | Package version, e.g. `"2.10.2"` |
| DefaultEVMWalletAdapter | class | EVM wallet adapter | Adapter implementation for EVM chains |
| Web3Provider | type | Interface | Type for the Web3 provider used by the adapter |

## index.ts Input and Output

- **Input**: None (barrel file).
- **Output**: The three exports above for use by apps and other packages.

## index.ts Dependencies and Callers

- **Upstream**: `./version`, `./walletAdapter`, `./provider/web3Provider.interface`.
- **Downstream**: Any app or package that imports `@paxeer-network/default-evm-adapter`.

## index.ts Example

```typescript
import {
  version,
  DefaultEVMWalletAdapter,
  type Web3Provider,
} from "@paxeer-network/default-evm-adapter";

console.log(version); // "2.10.2"
// Use DefaultEVMWalletAdapter with a Web3Provider implementation.
```
