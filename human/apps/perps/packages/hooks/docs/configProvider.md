# configProvider.tsx — PaxeerNetworkConfigProvider

## configProvider Responsibility

Provides `PaxeerNetworkConfigProvider`: root config and wallet setup for paxeer-network (config store, key store, wallet adapters, chain filter, optional custom chains, data adapter, notification, amplitude, order metadata). Must wrap the app (or paxeer-network tree) so that `PaxeerNetworkContext` and wallet/config are available. Also wires `DataCenterProvider` and `StatusProvider`.

## PaxeerNetworkConfigProvider Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| keyStore | PaxeerNetworkKeyStore | No | Key store instance. |
| contracts | IContract | No | Contract config. |
| walletAdapters | WalletAdapter[] | No | Wallet adapters (e.g. EVM, Solana). |
| chainFilter | ChainFilter | No | Filter chains (array or function). |
| orderbookDefaultTickSizes | Record<string, string> | No | Default tick sizes per symbol. |
| orderbookDefaultSymbolDepths | Record<PropertyKey, number[]> | No | Default depths per symbol. |
| enableSwapDeposit | boolean | No | Enable swap deposit. |
| customChains | Custom chains | No | Custom chain config. |
| dataAdapter | Data adapter | No | Data adapter. |
| notification | Notification | No | Notification config. |
| amplitudeConfig | Amplitude config | No | Amplitude config. |
| orderMetadata | Order metadata | No | Order metadata. |
| children | ReactNode | Yes | Child tree. |

Exclusive config (one of):

- **Option A**: `brokerId`, `brokerName?`, `networkId`, `configStore` must be omitted.
- **Option B**: `configStore`, and `brokerId` / `brokerName` / `networkId` must be omitted.

## PaxeerNetworkConfigProvider Dependencies

- **Upstream**: `@paxeer-network/core` (ConfigStore, PaxeerNetworkKeyStore, Account, etc.), default EVM/Solana adapters, `ExtendedConfigStore`, `PaxeerNetworkProvider`, `DataCenterProvider`, `StatusProvider`.
- **Downstream**: Any component using `PaxeerNetworkContext`, `useAccount`, `useConfig`, etc.

## PaxeerNetworkConfigProvider Example

```tsx
import { PaxeerNetworkConfigProvider } from "@paxeer-network/hooks";
import { ConfigStore, PaxeerNetworkKeyStore } from "@paxeer-network/core";

<PaxeerNetworkConfigProvider
  brokerId="your_broker"
  networkId={NetworkId.MAINNET}
  keyStore={keyStore}
  walletAdapters={[new DefaultEVMWalletAdapter(), new DefaultSolanaWalletAdapter()]}
  chainFilter={["ethereum", "arbitrum"]}
>
  <App />
</PaxeerNetworkConfigProvider>
```
