# packages/core/src Documentation Index

## Directory Overview

`packages/core/src` is the source root of the paxeer-network frontend core SDK. It provides account, wallet, signing, configuration, assets, and contract capabilities. It does not contain UI or page-level business logic.

## Module Responsibility Summary

| Module | Responsibility | Boundary |
|--------|----------------|----------|
| Account (account) | Login state, sub-accounts, paxeer-network Key, register/validate | Does not implement on-chain contract call details |
| Wallet (wallet) | Multi-chain wallet adapters, EIP-712 signing, contract call/sendTransaction | Does not handle UI connect flow |
| Signer / Keys (signer, keyStore, keyPair) | paxeer-network message signing, key storage, Ed25519 key pair | Does not perform wallet-native sign |
| Config (configStore) | API/WS URLs, brokerId, networkId, env | Read/write config only; no business parsing |
| Assets (assets) | Balance, approve, deposit, withdraw, internal transfer | Depends on Account and Contract |
| Contract (contract) | Vault, USDC, verify addresses and ABI by env/networkId | Does not execute on-chain calls |

## Key Entities Table

| Entity | Type | Responsibility | Entry |
|--------|------|----------------|-------|
| Account | class | Account state, login, sub-accounts, paxeer-network Key, settle | `account.ts` |
| AccountState | interface | Account state fields (status, accountId, subAccounts, etc.) | `account.ts` |
| WalletAdapter | interface | Wallet adapter contract (EVM/Solana, etc.) | `wallet/walletAdapter.ts` |
| WalletAdapterManager | class | Switch current WalletAdapter by chainNamespace | `walletAdapterManager.ts` |
| ConfigStore | interface | Config read/write (get/set/getOr) | `configStore/configStore.ts` |
| PaxeerNetworkKeyStore | interface | paxeer-network Key get/set, accountId, address | `keyStore.ts` |
| Signer | interface | Sign MessageFactor; produce paxeer-network-key/timestamp/signature | `signer.ts` |
| IContract | interface | Contract info by env (getContractInfoByEnv) | `contract.ts` |
| Assets | class | Balance, approve, deposit, withdraw, internalTransfer | `assets.ts` |
| SimpleDI | class | Global DI container (register/get) | `di/simpleDI.ts` |

## Subdirectories

| Directory | Description | Index |
|-----------|-------------|-------|
| [di](./di/index.md) | Dependency injection (SimpleDI, Container) | [di/index.md](./di/index.md) |
| [configStore](./configStore/index.md) | Config storage (ConfigStore, DefaultConfigStore, API_URLS) | [configStore/index.md](./configStore/index.md) |
| [wallet](./wallet/index.md) | Wallet adapters (WalletAdapter, BaseWalletAdapter, EtherAdapter, etc.) | [wallet/index.md](./wallet/index.md) |

## Top-Level Files List

| File | Language | Responsibility | Entry symbols | Link |
|------|----------|----------------|---------------|------|
| index.ts | TS | Package entry; re-exports | see file | [entry.md](./entry.md) |
| account.ts | TS | Account, sub-accounts, paxeer-network Key, settle | Account, AccountState | [account.md](./account.md) |
| app.ts | TS | App-level state (prepare list) | App | [app.md](./app.md) |
| assets.ts | TS | Assets: balance, approve, deposit, withdraw, internalTransfer | Assets | [assets.md](./assets.md) |
| constants.ts | TS | Contract address constants, EVENT_NAMES | constants | [constants.md](./constants.md) |
| contract.ts | TS | Contract info and chainId resolution | IContract, BaseContract, getContractInfoByChainId | [contract.md](./contract.md) |
| helper.ts | TS | Signing message builders (register, addPaxeerNetworkKey, settle, dexRequest) | getMockSigner, getDefaultSigner, generate*Message | [helper.md](./helper.md) |
| keyStore.ts | TS | Key storage abstraction and LocalStorage/Mock impl | PaxeerNetworkKeyStore, BaseKeyStore, LocalStorageStore, MockKeyStore | [keyStore.md](./keyStore.md) |
| keyPair.ts | TS | Ed25519 key pair (sign, getPublicKey) | PaxeerNetworkKeyPair, BasePaxeerNetworkKeyPair | [keyPair.md](./keyPair.md) |
| signer.ts | TS | Message-factor signing (paxeer-network API headers) | Signer, BaseSigner, MessageFactor, SignedMessagePayload | [signer.md](./signer.md) |
| types.ts | TS | Shared types (e.g. Ed25519Keypair) | Ed25519Keypair | [types.md](./types.md) |
| utils.ts | TS | Hash, base64url, parseAccountId, getTimestamp, etc. | parseAccountId, parseBrokerHash, SignatureDomain, getTimestamp | [utils.md](./utils.md) |
| version.ts | TS | Package version | default export | [version.md](./version.md) |
| subAccount.ts | TS | Sub-account type definition | SubAccount | [subAccount.md](./subAccount.md) |
| repository.ts | TS | Address-keyed key-value store abstraction and LocalStorage impl | Repository, LocalStorageRepository | [repository.md](./repository.md) |
| additionalInfoRepository.ts | TS | Wallet additional info storage wrapper | AdditionalInfoRepository | [additionalInfoRepository.md](./additionalInfoRepository.md) |
| walletAdapterManager.ts | TS | Multi-wallet adapter management and switching | WalletAdapterManager | [walletAdapterManager.md](./walletAdapterManager.md) |
| walletConnector.ts | TS | Wallet connector interface and placeholder impl | WalletConnector, BaseConnector, Blocknative | [walletConnector.md](./walletConnector.md) |
| _wallet.ts | TS | Legacy WalletClient abstraction (deprecated) | WalletClient, BaseWalletClient, SimpleWallet | [_wallet.md](./_wallet.md) |

## Search Keywords

paxeer-network, core, account, wallet, signer, keyStore, keyPair, configStore, assets, contract, deposit, withdraw, paxeer-network key, sub account, EIP-712, SignatureDomain, brokerId, networkId, apiBaseUrl, EVM, Solana, ChainNamespace.
