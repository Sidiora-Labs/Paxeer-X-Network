# web3-provider-ethers

Ethers-based Web3 provider implementation for paxeer-network Network. Wraps `ethers` v6 `BrowserProvider` and implements the shared `Web3Provider` interface used by the default EVM adapter.

## Directory overview

All source files live under `src/` with no subdirectories.

## Files

| File | Language | Description |
|------|----------|-------------|
| [ethersProvider.md](./ethersProvider.md) | TypeScript | `EthersProvider` class implementing `Web3Provider`: sign typed data, contract calls, send transaction, balance, etc. |
| [parseError.md](./parseError.md) | TypeScript | `parseError()` utility to decode and normalize ethers/contract errors. |
| [version.md](./version.md) | TypeScript | Package version constant and `window.__PAXEER_NETWORK_VERSION__` registration. |

**Entry point**: `src/index.ts` re-exports `EthersProvider` only.
