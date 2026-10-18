# additionalInfoRepository.ts

## additionalInfoRepository.ts Responsibility

Thin wrapper around a `Repository` that exposes the same address-scoped API (save, getAll, get, clear, remove) for wallet “additional info” (e.g. active sub-account id, AGW address). Used by Account to persist and read per-address metadata.

## additionalInfoRepository.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| AdditionalInfoRepository | class | Wrapper | Delegates to Repository for save/getAll/get/clear/remove |

## AdditionalInfoRepository Responsibility

Provides a named abstraction for wallet additional info storage so Account and other code can save/get/clear without depending on Repository directly.

## AdditionalInfoRepository Methods

| Method | Description |
|--------|-------------|
| save(address, data) | repository.save(address, data) |
| getAll(address) | repository.getAll(address) |
| get(address, key) | repository.get(address, key) |
| clear(address) | repository.clear(address) |
| remove(address, key) | repository.remove(address, key) |

## additionalInfoRepository.ts Dependencies and Call Relationships

- **Upstream**: repository.ts (Repository interface).
- **Downstream**: account.ts constructs it with LocalStorageRepository(Account.additionalInfoRepositoryName); uses save (setAddress, switchAccount), get (ACTIVE_SUB_ACCOUNT_ID_KEY), getAll (getAdditionalInfo), clear (disconnect).

## additionalInfoRepository.ts Example

```typescript
import { AdditionalInfoRepository, LocalStorageRepository } from "@paxeer-network/core";

const repo = new LocalStorageRepository("paxeer-network_walletAdditionalInfo");
const additionalInfo = new AdditionalInfoRepository(repo);
additionalInfo.save("0x...", { ACTIVE_SUB_ACCOUNT_ID_KEY: "0" });
const active = additionalInfo.get("0x...", "ACTIVE_SUB_ACCOUNT_ID_KEY");
```
