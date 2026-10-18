# container.ts

## container.ts Responsibility

Implements a simple DI container: register service classes (instantiated and added by constructor name), registerByName(name, classOrInstance), get(name), getAll(). Supports optional inject properties (injectIntoProperties) for property injection; current code does not expose addInjectProperty.

## container.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| Container | class | Container | register, registerByName, get, getAll |
| default | Container | Export | Default export |

## Container Responsibility

Stores services in a map; register() instantiates classes and adds them via add() which uses constructor.name; registerByName adds by explicit name. get/getAll perform lookup (get also registers under name.toLowerCase() for case-insensitive lookup).

## Container Methods

| Method | Description |
|--------|-------------|
| register(...serviceClasses) | Push classes to providers; for each, instantiate and addByName(instance.constructor.name, instance) |
| registerByName(name, serviceClass) | Instantiate if function, then addByName(name, service) |
| get<T>(name) | Return services[name] |
| getAll() | Return shallow copy of services |

## Container Internal Behavior

- add(service): addByName(service.constructor.name, service).
- addByName(name, service): services[name] = service; services[name.toLowerCase()] = service; injectIntoProperties(service, name).
- injectIntoProperties: for each registered inject property for that name, set target[propertyKey] = service.

## container.ts Dependencies and Call Relationships

- **Upstream**: None.
- **Downstream**: simpleDI.ts uses Container as the single global instance.

## container.ts Example

```typescript
import Container from "@paxeer-network/core/di/container";

const container = new Container();
container.register(MyService);
container.registerByName("account", accountInstance);
const svc = container.get<MyService>("MyService");
const all = container.getAll();
```
