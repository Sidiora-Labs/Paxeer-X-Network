# default-evm-adapter Package Documentation Index

## Directory Overview

`packages/default-evm-adapter/src` is the default EVM wallet adapter for paxeer-network. It implements `BaseWalletAdapter` for EVM chains: account registration, paxeer-network key management, withdraw, internal transfer, settle, and DEX request message generation with EIP-712 signing via a pluggable Web3 provider.

## Module Responsibility Summary

| Responsibility | Description |
|----------------|-------------|
| EVM wallet adapter | `DefaultEVMWalletAdapter` extends `BaseWalletAdapter`, uses `Web3Provider` for signing and RPC |
| EIP-712 message building | Helper functions build typed data for Registration, AddPaxeerNetworkKey, Withdraw, InternalTransfer, SettlePnl, DexRequest |
| Version and entry | Package version exposed as default export and on `window.__PAXEER_NETWORK_VERSION__`; entry re-exports adapter and types |
| Provider abstraction | `Web3Provider` interface and `Eip1193Provider` type define the contract for EVM RPC and signing |

## Key Entities

| Entity | Type | Responsibility | Entry |
|--------|------|----------------|-------|
| DefaultEVMWalletAdapter | class | EVM wallet adapter: active/update/deactivate, message generation, call/sendTransaction, getDomain | `walletAdapter.ts` |
| Web3Provider | interface | Contract for EVM provider: signTypedData, call, sendTransaction, getBalance, pollTransactionReceiptWithBackoff, etc. | `provider/web3Provider.interface.ts` |
| EVMAdapterOptions | interface | Adapter config: provider, address, chain, contractManager | `types.ts` |
| Eip1193Provider | type | EIP-1193 `request` method for wallet injection | `provider/web3Provider.interface.ts` |
| helper message functions | functions | registerAccountMessage, addPaxeerNetworkKeyMessage, withdrawMessage, internalTransferMessage, settleMessage, dexRequestMessage | `helper.ts` |

## Subdirectories and Top-Level Files

### Subdirectories

| Directory | Description | Index |
|-----------|-------------|--------|
| [provider](provider/index.md) | Web3 provider interface and EIP-1193 type | [provider/index.md](provider/index.md) |

### Top-Level Code Files

| File | Language | Responsibility | Entry symbols | Link |
|------|----------|-----------------|---------------|------|
| index.ts | TypeScript | Package entry: re-exports version, DefaultEVMWalletAdapter, Web3Provider | version, DefaultEVMWalletAdapter, Web3Provider | [entry.md](entry.md) |
| version.ts | TypeScript | Package version string and global `__PAXEER_NETWORK_VERSION__` | default export | [version.md](version.md) |
| walletAdapter.ts | TypeScript | DefaultEVMWalletAdapter class | DefaultEVMWalletAdapter | [walletAdapter.md](walletAdapter.md) |
| helper.ts | TypeScript | EIP-712 message builders for registration, addKey, withdraw, internalTransfer, settle, dexRequest | withdrawMessage, internalTransferMessage, addPaxeerNetworkKeyMessage, registerAccountMessage, settleMessage, dexRequestMessage | [helper.md](helper.md) |
| types.ts | TypeScript | EVM adapter options and getWalletAdapterFunc type | EVMAdapterOptions, getWalletAdapterFunc | [types.md](types.md) |

## Search Keywords

EVM, wallet adapter, Web3Provider, EIP-712, signTypedData, register account, add paxeer-network key, withdraw, internal transfer, settle, dex request, contract manager, chain namespace, default evm adapter
