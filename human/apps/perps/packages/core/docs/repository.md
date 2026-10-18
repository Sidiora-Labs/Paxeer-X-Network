# repository.ts

## repository.ts Responsibility

Defines a key-value storage abstraction keyed by address: `Repository` interface (save, getAll, get, clear, update, remove) and `LocalStorageRepository` implementation that persists data under a single localStorage key (name), with per-address nested objects.

## repository.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| Repository | interface | Contract | save, getAll, get, clear, update, remove (all address-scoped) |
| LocalStorageRepository | class | Impl | Uses localStorage[name] as JSON: { [address]: { ...keys } } |

## Repository Responsibility

Generic address-scoped storage for arbitrary key-value data. Used by AdditionalInfoRepository to store wallet additional info (e.g. active sub-account id).

## Repository Methods

| Method | Description |
|--------|-------------|
| save(address, data) | Merge data into stored object for address |
| getAll(address) | Return full object for address (or null) |
| get(address, key) | Return single key value for address |
| clear(address) | Remove all data for address |
| update(address, key, data) | Set key for address |
| remove(address, key) | Delete key for address |

## LocalStorageRepository Behavior

- Storage key: constructor `name`. Value: JSON object `{ [address]: Record<string, any> }`.
- save: merges data into the address entry. clear: deletes address entry. update/remove: mutate address entry. run() guards all writes to run only when window and localStorage exist.

## repository.ts Dependencies and Call Relationships

- **Upstream**: None.
- **Downstream**: additionalInfoRepository.ts uses Repository; Account uses AdditionalInfoRepository with LocalStorageRepository(Account.additionalInfoRepositoryName).

## repository.ts Example

```typescript
import { LocalStorageRepository, Repository } from "@paxeer-network/core";

const repo = new LocalStorageRepository("my_store");
repo.save("0x123", { activeSubAccountId: "0" });
const all = repo.getAll("0x123");
const one = repo.get("0x123", "activeSubAccountId");
repo.remove("0x123", "activeSubAccountId");
repo.clear("0x123");
```
