# provider

## Overview

Defines the root **WalletConnectorPrivyProvider** and **useWalletConnectorPrivy** context. The provider composes Privy, Wagmi, Solana, and Abstract wallet providers, fetches or uses custom chains, and exposes network, drawer state, chain type config, and Solana RPC info via context.

## Exports

### useWalletConnectorPrivy()

Hook that returns the wallet connector context value.

### WalletConnectorPrivyProvider

Root provider component.

#### Props (WalletConnectorPrivyProps)

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| privyConfig | `InitPrivy` | No | Privy app ID and config. |
| wagmiConfig | `InitWagmi` | No | Wagmi connectors/storage. |
| solanaConfig | `InitSolana` | No | Solana adapters and RPC. |
| abstractConfig | `InitAbstract` | No | Abstract (AGW) config. |
| network | `Network` | Yes | mainnet/testnet. |
| customChains | `Chains` | No | Custom mainnet/testnet chain definitions. |
| termsOfUse | `string` | No | URL for terms of use link. |
| headerProps | `{ mobile: ReactNode }` | No | Header content for mobile. |
| enableSwapDeposit | `boolean` | No | Enable swap/deposit chain fetching. |
| children | `ReactNode` | Yes | App content. |

#### Context value (WalletConnectorPrivyContextType)

| Field | Type | Description |
|-------|------|-------------|
| initChains | `Chain[]` | All configured chains. |
| mainnetChains | `Chain[]` | Mainnet chains. |
| testnetChains | `Chain[]` | Testnet chains. |
| getChainsByNetwork | `(network) => Chain[]` | Get chains by mainnet/testnet. |
| openConnectDrawer | `boolean` | Connect drawer open state. |
| setOpenConnectDrawer | `(open: boolean) => void` | Set drawer open. |
| targetWalletType | `WalletType \| undefined` | Wallet type to preselect. |
| setTargetWalletType | `(v) => void` | Set target wallet type. |
| network | `Network` | Current network. |
| setNetwork | `(n: Network) => void` | Set network. |
| solanaInfo | `{ rpcUrl, network } \| null` | Solana RPC and network. |
| setSolanaInfo | `(info) => void` | Set Solana info. |
| termsOfUse | `string` | Terms URL. |
| walletChainType | `WalletChainType` | Legacy chain type. |
| walletChainTypeConfig | `WalletChainTypeConfig` | hasEvm, hasSol, hasAbstract. |
| connectorWalletType | `ConnectorWalletType` | Which connectors are enabled. |
| privyConfig | `{ loginMethods? }` | Privy login methods. |

## Usage example

```tsx
import { WalletConnectorPrivyProvider, useWalletConnectorPrivy, Network } from "@paxeer-network/wallet-connector-privy";

function App() {
  return (
    <WalletConnectorPrivyProvider
      network={Network.mainnet}
      privyConfig={{ appid: "your-app-id" }}
      wagmiConfig={{ connectors: [] }}
    >
      <Content />
    </WalletConnectorPrivyProvider>
  );
}

function Content() {
  const { openConnectDrawer, setOpenConnectDrawer, initChains } = useWalletConnectorPrivy();
  return (
    <button onClick={() => setOpenConnectDrawer(true)}>Connect</button>
  );
}
```
