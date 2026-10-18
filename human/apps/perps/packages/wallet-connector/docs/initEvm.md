# initEvm.tsx

## Overview

Initializes the EVM (Web3 Onboard) layer from the shared persisted chain stores, merges EVM chains into options, calls `initConfig`, and registers the Onboard API in the DI container. Renders children only after initialization (or immediately if `skipInit` is true).

## Exports

### `ConnectorInitOptions`

Same as in [types](types.md): optional subset of `InitOptions`.

---

### `WalletConnectorProviderProps` (local)

Used by `InitEvm` only (not the root provider). Props for this component:

| Prop       | Type                   | Required | Description                                         |
| ---------- | ---------------------- | -------- | --------------------------------------------------- |
| `apiKey`   | `string`               | No       | Passed to `initConfig`.                             |
| `options`  | `ConnectorInitOptions` | No       | Merged with fetched chains.                         |
| `skipInit` | `boolean`              | No       | If true, skip init and render children immediately. |
| `children` | `ReactNode`            | Yes      | App tree.                                           |

---

### `InitEvm`

**Props:** `PropsWithChildren<WalletConnectorProviderProps>`

1. Sets `--onboard-modal-z-index: 88` on `document.body`.
2. If `skipInit`, sets initialized and renders children.
3. Otherwise gets `onboardAPI` from DI (`get("onboardAPI")`). If already present, marks initialized and renders children.
4. Waits for the shared mainnet and testnet chain stores to hydrate from IndexedDB. Persisted data is preferred; when no persisted testnet data exists, the store-provided Arbitrum Sepolia and Solana Devnet fallback is available.
5. If either store has no data after hydration, triggers both stores to fetch `/v1/public/chain_info`. A testnet request failure preserves the existing fallback or cache. A mainnet failure without cached data prevents initialization. These background requests have no additional timeout.
6. Filters out Solana chains, maps the remaining EVM rows to `{ id, token, label, rpcUrl, blockExplorerUrl }`, merges them into `options.chains`, calls `initConfig(apiKey, options)`, registers with `register('onboardAPI', onboardAPI)`, then renders children.
7. Web3 Onboard keeps the chain snapshot used at initialization; later store updates do not replace its configured chains.
8. Until initialized, returns `null` (no children).

## Usage example

```tsx
// Used internally by WalletConnectorProvider via evmInitial

<InitEvm apiKey="…" options={{ theme: "light" }} skipInit={false}>
  <Main>{children}</Main>
</InitEvm>
```
