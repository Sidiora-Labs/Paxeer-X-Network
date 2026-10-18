# contract/index

## Overview

Vault contract addresses per environment (prod, staging, qa, dev). Used for on-chain vault interactions.

## Exports

### VAULTS_CONTRACT_ADDRESSES

`Record<Env, Record<string, string>>` with keys per env: `vaultProtocol`, `vaultCrossChainManager`, `vaultPvLedger`, `vaultId`, `spAddress`.

## Usage example

```typescript
import { VAULTS_CONTRACT_ADDRESSES } from "./contract";
const { vaultProtocol, spAddress } = VAULTS_CONTRACT_ADDRESSES.prod;
```
