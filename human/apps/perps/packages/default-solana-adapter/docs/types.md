# types.ts

## types.ts responsibility

Defines TypeScript interfaces for configuring the Solana wallet adapter: adapter options and the Solana wallet provider contract (connection, sign, send) used by the paxeer-network Solana adapter.

## types.ts exports

| Name | Type | Role | Description |
|------|------|------|--------------|
| SolanaAdapterOption | interface | Config | Options passed when activating/updating the adapter |
| SolanaWalletProvider | interface | Provider | Solana wallet capabilities required by the adapter |

## SolanaAdapterOption responsibility

Holds the provider instance, wallet address, and chain info used to configure the default Solana wallet adapter.

## SolanaAdapterOption properties

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| provider | SolanaWalletProvider | Yes | Solana wallet provider (connection, sign, send) |
| address | string | Yes | Wallet public key / address (e.g. base58) |
| chain | { id: number } | Yes | Chain identifier (chain id) |

## SolanaWalletProvider responsibility

Describes the Solana wallet interface the adapter expects: optional connection/RPC, public key, network, and required sign/send methods.

## SolanaWalletProvider properties

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| connection | Connection | No | Pre-built Solana Connection |
| rpcUrl | string | No | RPC URL to create Connection if connection not set |
| publicKey | PublicKey | No | Wallet public key |
| network | WalletAdapterNetwork | Yes | Solana network (e.g. Mainnet, Devnet) |
| signMessage | (message: Uint8Array) => Promise<Uint8Array> | Yes | Sign raw bytes (e.g. for off-chain messages) |
| signTransaction | SignerWalletAdapterProps["signTransaction"] | Yes | Sign a Solana transaction |
| sendTransaction | WalletAdapterProps["sendTransaction"] | Yes | Send a signed transaction |

## types.ts dependency and usage

- **Upstream**: `@solana/wallet-adapter-base`, `@solana/web3.js`.
- **Downstream**: `walletAdapter.ts` uses these types for config and provider.

## types.ts Example

```typescript
import type { SolanaAdapterOption, SolanaWalletProvider } from "@paxeer-network/default-solana-adapter";
import { WalletAdapterNetwork } from "@solana/wallet-adapter-base";

const option: SolanaAdapterOption = {
  provider: {
    network: WalletAdapterNetwork.Devnet,
    signMessage: async (msg) => wallet.signMessage(msg),
    signTransaction: async (tx) => wallet.signTransaction(tx),
    sendTransaction: async (tx, conn) => conn.sendTransaction(tx),
  },
  address: "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM",
  chain: { id: 101 },
};
```
