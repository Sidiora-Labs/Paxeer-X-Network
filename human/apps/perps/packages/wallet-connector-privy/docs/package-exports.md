# index (package exports)

## Overview

Main entry point. Re-exports the wallet connector provider, hooks, UserCenter components, types, and re-exports of `viem`, `wagmi`, and `wagmi/connectors` for consumer use.

## Exports

### Provider & hook

- **WalletConnectorPrivyProvider** – Root provider component for Privy + Wagmi + Solana + Abstract.
- **useWalletConnectorPrivy** – Hook to access wallet connector context (chains, drawer state, network, etc.).

### Components

- **UserCenter** – Account/connect button and address display for desktop.
- **MwebUserCenter** – Mobile variant of UserCenter.

### Types

- **export \* from "./types"** – All types, enums, and chain maps from `types.ts`.

### Re-exports

- **viem** – Full `viem` namespace.
- **wagmiConnectors** – `wagmi/connectors`.
- **wagmi** – Full `wagmi` namespace.

## Usage example

```tsx
import {
  WalletConnectorPrivyProvider,
  useWalletConnectorPrivy,
  UserCenter,
  Network,
  InitPrivy,
  wagmi,
  wagmiConnectors,
} from "@paxeer-network/wallet-connector-privy";

function App() {
  return (
    <WalletConnectorPrivyProvider
      network={Network.mainnet}
      privyConfig={{ appId: "your-privy-app-id" }}
      wagmiConfig={{ connectors: [wagmiConnectors.metaMask()] }}
    >
      <YourApp />
    </WalletConnectorPrivyProvider>
  );
}
```
