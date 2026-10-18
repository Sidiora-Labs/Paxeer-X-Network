# useSOL.tsx

## Overview

Hook that wraps Solana wallet-adapter (`useWallet`, `useWalletModal`, `useConnection`) and `useSolanaContext` to expose connect/disconnect, wallet state, and current chain in a shape compatible with the unified connector (e.g. `WalletState` with `provider`, `accounts`, `chains`). Handles modal open/close, Ledger address persistence, and mobile “not ready” behavior.

## Exports

### `useSOL()`

**Returns:** Object with the following fields.

| Field | Type | Description |
|-------|------|-------------|
| `connected` | `boolean` | True when Solana wallet and publicKey are set. |
| `connect` | `() => Promise<any>` | Opens modal if no wallet selected; then connects and returns `[tempWallet]` in unified shape. |
| `disconnect` | `() => Promise<any>` | Disconnects adapter and returns `[]`. |
| `connecting` | `boolean` | From `useWallet()`. |
| `wallet` | `WalletState \| null` | Unified wallet object (label, icon, provider with rpcUrl/network/signMessage/signTransaction/sendTransaction/publicKey, accounts, chains). |
| `connectedChain` | `{ id: number; namespace: ChainNamespace.solana } \| null` | Current Solana chain (MAINNET or DEVNET id). |

## Behavior

- **connect():** Initializes internal promises for “wallet select” and “connect”. If no wallet selected, opens Solana modal; on select/connect success builds a `WalletState`-like object with `SolanaChains.get(network)` and `ChainNamespace.solana`. Ledger wallet address is stored via `setLedgerAddress`.
- **disconnect:** Calls adapter disconnect and clears local wallet state.
- **connectedChain:** Derived from `network` (Mainnet → `SolanaChainIdEnum.MAINNET`, else DEVNET).
- On mobile, if wallet is `Loadable` and not manual connect, disconnects (avoids bad auto-connect state).
- Emits `wallet:connect-error` on mobile for `WalletNotReadyError` (“Please open the wallet app and use the in-app browser.”).

## Usage example

```tsx
import { useSOL } from "./useSOL";

function SolanaConnectButton() {
  const { connect, disconnect, connected, wallet, connectedChain } = useSOL();

  if (connected && wallet?.accounts?.[0]) {
    return (
      <>
        <span>{wallet.accounts[0].address}</span>
        <button onClick={disconnect}>Disconnect</button>
      </>
    );
  }
  return <button onClick={connect}>Connect Solana</button>;
}
```
