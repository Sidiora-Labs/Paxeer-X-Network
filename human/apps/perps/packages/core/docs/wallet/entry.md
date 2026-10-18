# wallet/index.ts (Wallet Entry)

## wallet/index.ts Responsibility

Re-exports wallet adapter types from `./adapter`: `IWalletAdapter` as `WalletAdapter`, `getWalletAdapterFunc`, `WalletAdapterOptions`. Package consumers importing from `@paxeer-network/core` wallet get these from the main index re-export of `./wallet`.

## wallet/index.ts Exports

| Name | Type | Description |
|------|------|-------------|
| WalletAdapter | type | Alias of IWalletAdapter from adapter.ts |
| getWalletAdapterFunc | type | (options: WalletAdapterOptions) => IWalletAdapter |
| WalletAdapterOptions | type | { provider, address, chain: { id } } |

## wallet/index.ts Dependencies and Call Relationships

- **Upstream**: adapter.ts.
- **Downstream**: Root index.ts re-exports from ./wallet; apps use WalletAdapter and options when constructing adapters.
