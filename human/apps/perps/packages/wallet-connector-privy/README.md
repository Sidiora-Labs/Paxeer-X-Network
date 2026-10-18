# wallet connector privy

The new wallet connector consists of three parts:

- Privy: Privy provides social login, featuring an injected wallet, EVM wallets, and Solana wallets.
- Wagmi: Wagmi offers connectivity for EVM wallets, such as MetaMask and WalletConnect.
- Solana: Solana provides connectivity for Solana wallets, such as Phantom and Ledger.

other configure:

- `network` require, mainnet or testnet, For example, Solana and Abstract will use this network to determine what network it is.
- `customChains` optional, Brokers can define which chains to display
- `termsOfUse` optional

```javascript
interface WalletConnectorPrivyProps{
  network: Network;
  customChains?: Chains;
  termsOfUse?: string;
  privyConfig?: InitPrivy;
  wagmiConfig?: InitWagmi;
  solanaConfig?: InitSolana;
}
```

if termsOfUse is not configured, will not show terms part.

If privyConfig is not configured, the Privy connector will be disabled.

If wagmiConfig is not configured, the Wagmi connector will be disabled.

If solanaConfig is not configured, the Solana connector will be disabled.

If abstractConfig is not configured, the Abstract Global Wallet connector will be disabled.

At least one of privyConfig, wagmiConfig, or solanaConfig must be provided.

**Available Login Methods for Privy:**

- `email` - Email/password login
- `google` - Google OAuth login
- `twitter` - Twitter OAuth login
- `telegram` - Telegram login (optional)

If customChains only includes a Solana chain, then Privy will only display the Solana injected wallet, and the Wagmi connector will be disabled.

If customChains only includes EVM chains, then Privy will only display the EVM injected wallet, and the Solana connector will be disabled.

eg:

```javascript
<WalletConnectorPrivyProvider
      termsOfUse="https://learn.woo.org/legal/terms-of-use"
      network={Network.testnet}
      // customChains={customChains}
      privyConfig={{
        appid: "you privy appid",
        config: {
          appearance: {
            theme: "dark",
            accentColor: "#181C23",
            logo: "/paxeer-network-logo.svg",
          },
          loginMethods: ["email", "google", "twitter", "telegram"],
        },
      }}
      wagmiConfig={{
        connectors: [
          wagmiConnectors.injected(),
          wagmiConnectors.walletConnect({
            projectId: "you project id",
            showQrModal: true,
            storageOptions: {},
            metadata: {
              name: "paxeer-network Network",
              description: "paxeer-network Network",
              url: "https://paxeer.network",
              icons: ["https://oss.paxeer.network/static/sdk/chains.png"],
            },
          }),
        ],
      }}
      solanaConfig={{
        mainnetRpc: "",
        devnetRpc: "https://api.devnet.solana.com",
        wallets: wallets,
        onError: (error: WalletError, adapter?: Adapter) => {
          console.log("-- error", error, adapter);
        },
      }}
      abstractConfig={{}}
    >
      <PaxeerNetworkAppProvider
        // ...orderAppConfig
      >
        {props.children}
      </PaxeerNetworkAppProvider>
    </WalletConnectorPrivyProvider>
```
