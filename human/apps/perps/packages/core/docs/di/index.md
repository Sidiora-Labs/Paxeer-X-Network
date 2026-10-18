# di Directory Index

## Directory Description

`di` provides a simple dependency-injection container: register and resolve services by name on a global singleton, used by wallet adapters and others to obtain Account or other shared services.

## Directory Responsibility and Boundary

- **Responsible for**: Singleton container creation, register/get by name, binding to globalThis.
- **Not responsible for**: Constructor injection, type-safe generic resolution, lifecycle management.

## File List

| File | Language | Summary | Entry symbols | Link |
|------|----------|--------|---------------|------|
| simpleDI.ts | TS | Static DI facade; holds global Container | SimpleDI | [simpleDI.md](./simpleDI.md) |
| container.ts | TS | Container impl: register, registerByName, get, getAll | Container | [container.md](./container.md) |

## Key Entities

| Entity | File | Responsibility | Dependency |
|--------|------|----------------|------------|
| SimpleDI | simpleDI.ts | Global singleton access, register/get | Container, getGlobalObject |
| Container | container.ts | Service registration and lookup by name | None |

## Subdirectories

None.
