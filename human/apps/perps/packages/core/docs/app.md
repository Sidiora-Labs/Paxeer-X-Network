# app.ts

## app.ts Responsibility

Provides the `App` class for app-level state: a simple “prepare” list (e.g. symbolInfo, clientInfo) and an `updateState` method to remove items from that list. Used to track readiness of app-level data.

## app.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| App | class | Core entity | Prepare list and updateState |

## App Responsibility

Holds a private `prepare` array and exposes `updateState(name)` to remove a given name from it. No other behavior is implemented in code.

## App Input and Output

- **Input**: None at construction. `updateState(name: string)` takes a name to remove.
- **Output**: Side effect only (mutates internal prepare list). No return value or events.

## App Fields / Methods

| Name | Type | Description |
|------|------|-------------|
| prepare | private string[] | Initial list: `["symbolInfo", "clientInfo"]` |
| updateState(name) | method | Removes `name` from `prepare` |

## App Dependencies and Call Relationships

- **Upstream**: Not inferable from code; likely app bootstrap or a provider.
- **Downstream**: None in this file.
- **Related**: Account, Assets (referenced in comments only).

## App Extension and Modification Points

- **Prepare list**: Change initial `prepare` array in constructor or add a setter.
- **Readiness**: Add a getter (e.g. `isReady`) or event when `prepare` becomes empty if needed.

## App Example

```typescript
import { App } from "@paxeer-network/core";

const app = new App();
app.updateState("symbolInfo");
// prepare now does not contain "symbolInfo"
```
