# plugin — Directory Index

## Directory Responsibility

UI extension/plugin system: registry, installExtension, useExtensionBuilder, useBuilder, plugin context, and built-in plugins (e.g. deposit). Allows registering UI slots and builders by scope and position.

## Files

| File                                                  | Language   | Summary                               | Link                                                 |
| ----------------------------------------------------- | ---------- | ------------------------------------- | ---------------------------------------------------- |
| index.ts                                              | TypeScript | Re-exports plugin API                 | [index.md](index.md)                                 |
| install.tsx                                           | TSX        | installExtension and registration     | [install.md](install.md)                             |
| notFound.tsx                                          | TSX        | NotFound fallback for missing plugin  | [notFound.md](notFound.md)                           |
| pluginContext.tsx                                     | TSX        | Plugin context                        | [pluginContext.md](pluginContext.md)                 |
| registry.ts                                           | TypeScript | Plugin registry                       | [registry.md](registry.md)                           |
| slot.tsx                                              | TSX        | Plugin slot component                 | [slot.md](slot.md)                                   |
| types.ts                                              | TypeScript | Plugin types (positions, scope, etc.) | [types.md](types.md)                                 |
| useBuilder.ts                                         | TypeScript | useBuilder hook                       | [useBuilder.md](useBuilder.md)                       |
| useExtensionBuilder.ts                                | TypeScript | useExtensionBuilder hook              | [useExtensionBuilder.md](useExtensionBuilder.md)     |
| [plugins/deposit/index.tsx](plugins/deposit/index.md) | TSX        | Deposit plugin entry                  | [plugins/deposit/index.md](plugins/deposit/index.md) |
| [plugins/deposit/comp.tsx](plugins/deposit/comp.md)   | TSX        | Deposit plugin component              | [plugins/deposit/comp.md](plugins/deposit/comp.md)   |

## Key Entities

| Entity              | File                   | Role                                                       |
| ------------------- | ---------------------- | ---------------------------------------------------------- |
| installExtension    | install.tsx            | Register an extension with name, scope, positions, builder |
| registry            | registry.ts            | Store and resolve extensions                               |
| useExtensionBuilder | useExtensionBuilder.ts | Hook to resolve and build extension UI                     |
| Plugin slot         | slot.tsx               | Renders extension for a position                           |
