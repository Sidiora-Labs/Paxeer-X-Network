# walletConnectorContext.tsx — WalletConnectorContext and useWalletConnector

## walletConnectorContext Responsibility

Provides `WalletConnectorContext` and `useWalletConnector` for wallet connection state: connected chain, wallet state, connect/disconnect. Used by header or wallet UI to show connection status and trigger connect/disconnect.

## walletConnectorContext Exports

| Name | Type | Role |
|------|------|------|
| WalletConnectorContext | React.Context | Context for wallet connector state. |
| useWalletConnector | hook | Returns WalletConnectorContextState (connected chain, wallet state, etc.). |
| ConnectedChain | type | Connected chain info. |
| WalletConnectorContextState | type | Full context state. |
| WalletState | type | Wallet state type. |

## useWalletConnector Return (typical)

- Connected chain info, wallet state, connect/disconnect methods (exact shape from context).
- Used by components that need to display or control wallet connection.

## walletConnectorContext Example

```tsx
import { useWalletConnector } from "@paxeer-network/hooks";

function WalletButton() {
  const { state, connect, disconnect } = useWalletConnector();
  if (state.connected) return <button onClick={disconnect}>Disconnect</button>;
  return <button onClick={connect}>Connect</button>;
}
```
