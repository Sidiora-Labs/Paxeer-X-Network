# index.ts (Package Entry)

## index.ts Responsibility

This file is the package entry point for `@paxeer-network/core`. It re-exports types, classes, and functions from key modules so consumers import from a single entry.

## index.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| MockKeyStore, LocalStorageStore, BaseKeyStore, PaxeerNetworkKeyStore | class / interface | Key store | From `./keyStore` |
| Signer, MessageFactor, SignedMessagePayload, BaseSigner | type / class | Signer | From `./signer` |
| EventEmitter | default | Event bus | From `eventemitter3` |
| PaxeerNetworkKeyPair, BasePaxeerNetworkKeyPair | type / class | Key pair | From `./keyPair` |
| getMockSigner, getDefaultSigner, generateAddPaxeerNetworkKeyMessage, generateRegisterAccountMessage, generateSettleMessage, generateDexRequestMessage | function | Helper | From `./helper` |
| IContract, BaseContractManager | type / class | Contract | From `./contract` |
| EVENT_NAMES | constant | Events | From `./constants` |
| SimpleDI | default | DI | From `./di/simpleDI` |
| Account, AccountState | class / type | Account | From `./account` |
| ConfigStore exports | * | Config | From `./configStore/configStore` |
| SubAccount | type | Sub-account | From `./subAccount` |
| DefaultConfigStore, API_URLS, URLS | class / const / type | Config | From `./configStore/defaultConfigStore` |
| ChainType, WalletAdapter, Message, RegisterAccountInputs, WithdrawInputs, InternalTransferInputs, SettleInputs, AddPaxeerNetworkKeyInputs, DexRequestInputs | type | Wallet | From `./wallet/walletAdapter` |
| BaseWalletAdapter | class | Wallet | From `./wallet/baseWalletAdapter` |
| Ed25519Keypair | type | Types | From `./types` |
| SignatureDomain | type | Utils | From `./utils` |
| wallet/* | * | Wallet | From `./wallet` |
| utils | namespace | Utils | From `./utils` |

## index.ts Dependency and Usage

- **Upstream**: Build tool (bundler) resolves this file as package main.
- **Downstream**: Apps and other packages import from `@paxeer-network/core` and get these exports.

## index.ts Example

```typescript
import {
  Account,
  AccountState,
  DefaultConfigStore,
  BaseWalletAdapter,
  LocalStorageStore,
  EVENT_NAMES,
} from "@paxeer-network/core";

const config = new DefaultConfigStore({ networkId: "testnet" });
const keyStore = new LocalStorageStore("testnet");
// Use Account with wallet adapters...
```
