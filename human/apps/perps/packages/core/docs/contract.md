# contract.ts

## contract.ts Responsibility

Defines `PaxeerNetworkContracts` type, `IContract` interface, `BaseContract` implementation that returns contract info (USDC/vault/verify addresses and ABIs) by env/networkId, and `getContractInfoByChainId` to resolve vault/token address by chainId.

## contract.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| PaxeerNetworkContracts | type | Data shape | usdcAbi, erc20Abi, vaultAbi, verifyContractAddress, usdcAddress, vaultAddress, solana*, story*, monad*, abstract*, bsc* |
| IContract | interface | Contract provider | getContractInfoByEnv(): PaxeerNetworkContracts |
| BaseContract | class | Default impl | Reads networkId/env from ConfigStore, returns matching addresses and ABIs |
| getContractInfoByChainId | function | Resolver | (chainId, contractInfo) → { vaultAddress, tokenAddress } for Story, Monad, Abstract, BSC |

## IContract Responsibility

Provides paxeer-network contract metadata (addresses and ABIs) for the current environment. Used by Account (options.contracts), Assets, and wallet adapters.

## IContract Input and Output

- **Input**: None (getContractInfoByEnv() takes no args).
- **Output**: PaxeerNetworkContracts (usdcAddress, vaultAddress, verifyContractAddress, usdcAbi, vaultAbi, erc20Abi, and optional chain-specific addresses).

## PaxeerNetworkContracts Fields

| Field | Type | Description |
|-------|------|-------------|
| usdcAbi | any | USDC/ERC-20 ABI |
| erc20Abi | any | ERC-20 ABI |
| vaultAbi | any | Vault ABI |
| verifyContractAddress | string | Verify contract for EIP-712 |
| usdcAddress | string | USDC token address |
| vaultAddress | string | Vault address (deprecated in favor of chain-specific) |
| solanaUSDCAddress, solanaVaultAddress | string | Solana (deprecated) |
| storyTestnetVaultAddress, monadTestnetVaultAddress, monadTestnetUSDCAddress | string | Optional testnet |
| abstractVaultAddress, abstractUSDCAddress | string | Optional Abstract |
| bscVaultAddress, bscUSDCAddress | string | Optional BSC |

## BaseContract Responsibility

Implements IContract using ConfigStore networkId and env. mainnet returns mainnet addresses; otherwise returns testnet/staging addresses (env qa/dev alter verify and some vaults).

## getContractInfoByChainId Execution Flow

1. Start with contractInfo.vaultAddress and contractInfo.usdcAddress.
2. If chainId === STORY_TESTNET_CHAINID → use storyTestnetVaultAddress.
3. If chainId === MONAD_TESTNET_CHAINID → use monadTestnetVaultAddress and monadTestnetUSDCAddress.
4. If chainId in ABSTRACT_CHAIN_ID_MAP → use abstractVaultAddress and abstractUSDCAddress.
5. If chainId === BSC_TESTNET_CHAINID → use bscVaultAddress and bscUSDCAddress.
6. Return { vaultAddress, tokenAddress }.

## contract.ts Dependencies and Call Relationships

- **Upstream**: ConfigStore (get networkId, env); constants.ts for addresses; wallet/abis for JSON ABIs.
- **Downstream**: Account, Assets, wallet adapters use getContractInfoByEnv() or getContractInfoByChainId.

## contract.ts Extension and Modification Points

- **New env/network**: Extend BaseContract.getContractInfoByEnv() and add constants.
- **New chain**: Add branch in getContractInfoByChainId and optional PaxeerNetworkContracts fields; ensure constants and BaseContract are updated.

## contract.ts Example

```typescript
import { BaseContract, getContractInfoByChainId, IContract } from "@paxeer-network/core";

const contractManager = new BaseContract(configStore);
const info = contractManager.getContractInfoByEnv();
const { vaultAddress, tokenAddress } = getContractInfoByChainId(421614, info);
```
