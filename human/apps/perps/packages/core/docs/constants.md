# constants.ts

## constants.ts Responsibility

Exports contract addresses (USDC, vault, verify) for multiple networks (Arbitrum testnet, mainnet, Solana dev/qa/staging/mainnet, Story testnet, Monad testnet, Abstract, BSC testnet/mainnet) and the `EVENT_NAMES` object used by the Account event emitter.

## constants.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| nativeUSDCAddress | string | Contract | EVM USDC address (native) |
| stagingUSDCAddressOnArbitrumTestnet | string | Contract | Staging Arbitrum testnet USDC |
| stagingVaultAddressOnArbitrumTestnet | string | Contract | Staging Arbitrum testnet vault |
| stagingVerifyAddressOnArbitrumTestnet | string | Contract | Staging Arbitrum testnet verify |
| mainnetUSDCAddress | string | Contract | Mainnet USDC |
| mainnetVaultAddress | string | Contract | Mainnet vault |
| mainnetVerifyAddress | string | Contract | Mainnet verify |
| solanaMainnetVaultAddress, solanaStagingVualtAddress, solanaDevVaultAddress, solanaQaVaultAddress | string | Contract | Solana vaults |
| solanaUSDCAddress, solanaMainnetUSDCAddress | string | Contract | Solana USDC |
| EVENT_NAMES | object | Events | statusChanged, validateStart, validateEnd, switchAccount, subAccountCreated, subAccountUpdated |
| stagingStoryTestnetVaultAddress, stagingMonadTestnetVaultAddress, MonadTestnetUSDCAddress, qaMonadTestnetVaultAddress | string | Contract | Story/Monad testnet |
| qaArbitrumTestnetVaultAddress | string | Contract | QA Arbitrum testnet vault |
| AbstractMainnetUSDCAddress, AbstractTestnetUSDCAddress, AbstractDevVaultAddress, AbstractQaVaultAddress, stagingAbstractTestnetVaultAddress, abstractMainnetVaultAddress | string | Contract | Abstract |
| bscTestnetDevVaultAddress, bscTestnetQaVaultAddress, bscTestnetStagingVaultAddress, bscTestnetUSDCAddress | string | Contract | BSC testnet |
| bscMainnetVaultAddress, bscMainnetUSDCAddress | string | Contract | BSC mainnet |

## EVENT_NAMES Fields

| Key | Value | Usage |
|-----|-------|--------|
| statusChanged | "change:status" | Account state updated |
| validateStart | "validate:start" | Validation started |
| validateEnd | "validate:end" | Validation finished |
| switchAccount | "switch:account" | User switched address |
| subAccountCreated | "account:sub:created" | Sub-account created |
| subAccountUpdated | "account:sub:updated" | Sub-account updated |

## constants.ts Dependencies and Call Relationships

- **Upstream**: None (no imports from other core src files).
- **Downstream**: contract.ts (and possibly wallet/abis) use these addresses; Account and others use EVENT_NAMES.

## constants.ts Extension and Modification Points

- **New network**: Add new address constants and wire them in contract.ts (getContractInfoByEnv / getContractInfoByChainId).
- **New events**: Add keys to EVENT_NAMES and emit in Account where appropriate.

## constants.ts Example

```typescript
import { EVENT_NAMES, mainnetUSDCAddress, mainnetVaultAddress } from "@paxeer-network/core";

account.on(EVENT_NAMES.statusChanged, (state) => { /* ... */ });
account.on(EVENT_NAMES.validateStart, () => { /* ... */ });

const usdc = mainnetUSDCAddress;
const vault = mainnetVaultAddress;
```
