# InitSolanaProvider

## Overview

**InitSolanaProvider** wraps children with **WalletProvider** from `@solana/wallet-adapter-react` using adapters and optional RPC from **InitSolana**. It updates **solanaInfo** in wallet-connector context (rpcUrl, network) based on `network` (mainnet/devnet). If Solana is disabled via context, it renders only `children`.

## Props (InitSolana)

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| mainnetRpc | `string` | No | Mainnet RPC URL. |
| devnetRpc | `string` | No | Devnet RPC URL. |
| wallets | `Adapter[]` | Yes | Wallet adapters (defaults to Phantom if not provided). |
| onError | `(error, adapter?) => void` | Yes | Error handler. |
| children | `ReactNode` | Yes | App content. |

## Usage example

```tsx
<InitSolanaProvider
  wallets={[new PhantomWalletAdapter()]}
  onError={(err) => console.error(err)}
  mainnetRpc="https://..."
  devnetRpc="https://..."
>
  {children}
</InitSolanaProvider>
```
