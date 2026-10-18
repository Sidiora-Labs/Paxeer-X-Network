# index (package entry)

> Location: `packages/core/src/index.ts`

## Overview

Main entry of `@paxeer-network/core`. Re-exports keyStore, signer, keyPair, helper, contract, constants, SimpleDI, Account, configStore, defaultConfigStore, wallet types and adapters, types, and utils.

## Exports

- **KeyStore**: `MockKeyStore`, `LocalStorageStore`, `BaseKeyStore`, `PaxeerNetworkKeyStore`
- **Signer**: `Signer`, `MessageFactor`, `SignedMessagePayload`, `BaseSigner`
- **EventEmitter**: default from `eventemitter3`
- **KeyPair**: `PaxeerNetworkKeyPair`, `BasePaxeerNetworkKeyPair`
- **Helper**: `getMockSigner`, `getDefaultSigner`, `generateAddPaxeerNetworkKeyMessage`, `generateRegisterAccountMessage`, `generateSettleMessage`, `generateDexRequestMessage`
- **Contract**: `IContract`, `BaseContract` as `BaseContractManager`
- **Constants**: `EVENT_NAMES`
- **DI**: `SimpleDI`
- **Account**: `Account`, `AccountState`
- **ConfigStore**: `ConfigStore`, `ConfigKey`, etc. from configStore
- **DefaultConfigStore**: `DefaultConfigStore`, `API_URLS`, `URLS`
- **Types**: `SubAccount`, `ChainType`, `WalletAdapter`, `Message`, `RegisterAccountInputs`, `WithdrawInputs`, `InternalTransferInputs`, `SettleInputs`, `AddPaxeerNetworkKeyInputs`, `DexRequestInputs`, `Ed25519Keypair`, `SignatureDomain`
- **Wallet**: `BaseWalletAdapter`, wallet module exports
- **Utils**: `utils` namespace and `SignatureDomain` type

## Usage

```ts
import {
  Account,
  DefaultConfigStore,
  LocalStorageStore,
  BaseSigner,
  EVENT_NAMES,
} from "@paxeer-network/core";
```
