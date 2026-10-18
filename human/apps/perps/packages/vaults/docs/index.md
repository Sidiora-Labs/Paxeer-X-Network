# Vaults Package — Documentation Index

This directory contains structured documentation for the `packages/vaults/src` codebase. The vaults package provides strategy vault UI, API client, store, and types for paxeer-network vaults (deposit/withdraw, LP info, performance).

## Directory overview

| Directory | Description |
|-----------|-------------|
| [api](./api/index.md) | API client, request layer, and environment URLs |
| [components](./components/index.md) | React components (all-vaults, vault-card, vault-operation, header, introduction, provider) |
| [contract](./contract/index.md) | Contract addresses per environment |
| [hooks](./hooks/index.md) | Hooks (e.g. `useSVApiUrl`) |
| [pages](./pages/index.md) | Page entry (VaultsPage) |
| [store](./store/index.md) | Zustand store and selectors for vault data |
| [types](./types/index.md) | Vault types and enums |
| [utils](./utils/index.md) | Utilities (parseMarkdownLinks, operationPayload, getOperationStatusColor) |

## Root-level files

| File | Language | Description |
|------|----------|-------------|
| [version](./version.md) | TypeScript | Package version (e.g. 2.9.1) and `window.__PAXEER_NETWORK_VERSION__` |

Entry point `index.ts` re-exports: `./pages`, `./types`, `./api/api`, `./store`, `./components`.
