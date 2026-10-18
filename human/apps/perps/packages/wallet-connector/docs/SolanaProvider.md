# SolanaProvider.tsx

## Overview

React provider that sets up the Solana wallet-adapter stack: `SolanaContext` (network + endpoint), `WalletProvider`, and `WalletModalProvider`. Supports Phantom, Solana Mobile adapter, and custom adapters; handles wallet errors and mobile “not ready” behavior.

## Exports

### `useSolanaContext()`

Hook that returns `{ network, endpoint }` from `SolanaContext`. Throws if used outside `SolanaProvider`.

---

### `SolanaProvider`

**Props:** `SolanaInitialProps` (see [types](types.md))

- **network:** `WalletAdapterNetwork` (default Devnet).
- **mainnetRpc / devnetRpc:** RPC URLs; `endpoint` is set from these based on `network`.
- **wallets:** Optional custom `Adapter[]`; default is Phantom + `SolanaMobileWalletAdapter` (with default address selector and auth cache).
- **onError:** Optional `(error: WalletError, adapter?: Adapter) => void`; default handler opens wallet URL in new tab (desktop) or emits `wallet:connect-error` (mobile).
- **children:** React tree.

## UI behavior

- Injects `@solana/wallet-adapter-react-ui/styles.css`.
- `WalletModalProvider` uses class `oui-pointer-events-auto`.
- On `WalletNotReadyError`, desktop opens `adapter?.url` in a new tab; mobile emits `wallet:connect-error` with message to use in-app browser.
- Mobile adapter uses `getGlobalObject().location` to build `uri` for `appIdentity`.

## Usage example

```tsx
import { SolanaProvider, useSolanaContext } from "./SolanaProvider";

<SolanaProvider
  network={WalletAdapterNetwork.Devnet}
  devnetRpc="https://api.devnet.solana.com"
  onError={(err, adapter) => console.error(err)}
>
  <Child />
</SolanaProvider>

function Child() {
  const { network, endpoint } = useSolanaContext();
}
```
