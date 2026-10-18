# useEvm.tsx

## Overview

Hook that wraps Web3 Onboard’s `useConnectWallet` and `useSetChain` to expose a simple EVM wallet API: connect, disconnect, connecting state, current wallet, connected chain, and chain switching.

## Exports

### `useEvm()`

**Returns:** Object with the following fields.

| Field | Type | Description |
|-------|------|-------------|
| `connect` | From `useConnectWallet` | Start connect flow (optional options e.g. `autoSelect`). |
| `connected` | `boolean` | True if `wallet?.accounts?.[0]?.address` exists. |
| `disconnect` | `() => Promise<void>` | Disconnects current wallet via Onboard (uses `wallet.label`). |
| `connecting` | `boolean` | From `useConnectWallet`. |
| `wallet` | From `useConnectWallet` | Current wallet or null. |
| `connectedChain` | `{ ...evmConnectChain, id: number } \| null` | Current chain with numeric `id`. |
| `changeChain` | `(chain: { chainId: string }) => Promise<any>` | Calls Onboard `setChain`. |

## Usage example

```tsx
import { useEvm } from "./useEvm";

function EvmConnectButton() {
  const { connect, disconnect, connected, connecting, wallet, connectedChain, changeChain } = useEvm();

  if (connected) {
    return (
      <>
        <span>{wallet?.accounts?.[0]?.address}</span>
        <button onClick={() => changeChain({ chainId: "0x38" })}>Switch to BNB</button>
        <button onClick={disconnect}>Disconnect</button>
      </>
    );
  }
  return <button onClick={() => connect()} disabled={connecting}>Connect EVM</button>;
}
```
