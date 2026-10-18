# simpleDI.ts

## simpleDI.ts Responsibility

Exposes a static DI facade that creates and holds a single `Container` on `getGlobalObject()[__PAXEER_NETWORK_CONTAINER__]`. Provides register, registerByName, get, getOr, getAll for app-wide service registration and resolution.

## simpleDI.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| SimpleDI | class | Facade | Static methods: register, registerByName, get, getOr, getAll |
| default | SimpleDI | Export | Default export |

## SimpleDI Responsibility

Single entry point for DI: lazily creates one Container and stores it on the global object so the same container is used across the app (e.g. for Account, wallet adapters).

## SimpleDI Methods

| Method | Description |
|--------|-------------|
| register(...serviceClasses) | Delegates to container.register; instantiates classes and adds by constructor name |
| registerByName(name, serviceClass) | Registers one service by name (instantiates if function) |
| get<T>(name) | Returns service by name; throws or returns undefined if missing |
| getOr<T>(name, instance) | If get(name) is falsy, registerByName(name, instance); returns instance |
| getAll() | Returns copy of all registered services |

## SimpleDI Execution Flow

1. getContainer(): If container not set, create new Container and assign to getGlobalObject()[SimpleDI.KEY]; return container.
2. register/registerByName: getContainer().register(...) or registerByName(...).
3. get/getOr/getAll: getContainer().get/getOr/getAll.

## simpleDI.ts Dependencies and Call Relationships

- **Upstream**: utils (getGlobalObject), container.ts (Container).
- **Downstream**: Account or app bootstrap registers services; BaseWalletAdapter (signMessageByPaxeerNetworkKey) uses SimpleDI.get<Account>("account").

## simpleDI.ts Example

```typescript
import SimpleDI from "@paxeer-network/core";

SimpleDI.register(Account, ConfigStore);
SimpleDI.registerByName("account", accountInstance);
const account = SimpleDI.get<Account>("account");
const config = SimpleDI.getOr("config", defaultConfig);
```
