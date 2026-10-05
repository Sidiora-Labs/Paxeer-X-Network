# Wallet app architecture

The wallet app is a Next.js App Router application built as a standalone server and served under the base path `/wallet` (`next.config.mjs`). This page maps the source tree; the code under `src/` is authoritative.

## Entry point

```text
src/app/layout.tsx
  ThemeProvider
    QueryProvider
      ErrorBoundary
        page content
    Toaster
src/app/page.tsx
  SplashScreen
    LocaleProvider
      PWAProvider
        WalletProvider
          ShellWidget
        InstallBanner, NotificationPrompt, FloatingAppIcon, UpdateBanner, OfflineIndicator
```

`src/widgets/shell/ShellWidget.tsx` is the product shell on `/`. `src/providers/WalletProvider.tsx` re-exports the provider from `src/wallet/WalletProvider.tsx`.

## Pages

| Path (under `/wallet`) | Source | Purpose |
| --- | --- | --- |
| `/` | `src/app/page.tsx` | Shell with the routed widgets below |
| `/account`, `/account/history`, `/account/deposit`, `/account/plan` | `src/app/account`, `src/account` | Unified account view, history, deposit and value movement for the signed-in wallet |
| `/surfaces/exchange`, `/surfaces/bridge`, `/surfaces/launchpad`, `/surfaces/fees`, `/surfaces/web-data` | `src/app/surfaces`, `src/surfaces` | Network surfaces listed in `src/surfaces/routes.ts` |
| `/auth/callback` | `src/app/auth/callback` | Sign-in return |
| `/offline` | `src/app/offline` | Offline fallback |
| `/privacy` | `src/app/privacy` | Privacy information |

## Shell routes

`src/domains/shell/index.ts` defines the route model: `SHELL_ROUTE_NAMES`, the `ShellRoute` union, `ROUTE_POLICIES`, `parseRouteUrl`, `parseRouteQuery`, `serializeRoute` and `routeGuard`. `src/widgets/shell/useAppRoute.ts` owns browser history for the shell.

| Route | Widget | Recovery route | Draft lifetime |
| --- | --- | --- | --- |
| `portfolio` | `widgets/portfolio` | `portfolio` | none |
| `send` | `widgets/send` | `portfolio` | route |
| `receive` | `widgets/receive` | `portfolio` | none |
| `transactions` | `widgets/transactions` | `portfolio` | none |
| `swap` | `widgets/swap` | `portfolio` | route |
| `discover` | `widgets/discover` | `portfolio` | none |
| `pns` | `widgets/pns` | `discover` | none |
| `settings` | `widgets/settings` | `portfolio` | none |
| `contacts` | `widgets/contacts` | `settings` | none |
| `token-detail` | `widgets/tokenDetail` | `portfolio` | none |
| `tx-detail` | `widgets/txDetail` | `transactions` | session observation |

Every route admits both custody modes, `embedded` and `injected`, and requires an unlocked wallet with an account.

## Wallet and identity

`src/wallet` connects the app to the wallet SDK (`@paxeer/wallet`, linked from `human/wallet/sdk`):

- `session.ts` builds the embedded wallet and discovers injected EIP-6963 wallets.
- `identity.ts` runs Supabase sign-in through `IdentitySession`; providers are `google`, `discord`, `github`, `apple` and `twitter`.
- `signer.ts` adapts a wallet to an ethers `AbstractSigner`.
- `config.ts` resolves the wallet configuration from the environment.

## Domains and platform

| Path | Contents |
| --- | --- |
| `src/domains` | Typed boundary parsers and contracts: `approval`, `custody`, `platform`, `portfolio`, `product`, `server-edge`, `shared`, `shell`, `transaction` |
| `src/platform/storage` | Browser storage registry and repositories; see [storage-forensics.md](storage-forensics.md) |
| `src/platform/status` | Background failure reporting |
| `src/pwa` | Manifest, service worker, caching policy and registration; `scripts/build-pwa.mjs` writes the worker and manifest into `public/` before `next build` |
| `src/lib/security` | Content security policy (`csp.ts`) and media policy |
| `src/lib/swap` | Swap SDK and ABI modules; `generate:swap-abis` writes the ABI index |

## Server edge

`src/app/api` holds every server route ([route list](../api/README.md)). `src/server` holds the boundary helpers: `http.ts` (public errors, bounded upstream reads, client identity, push admin check), `json-proxy.ts`, `wallet-read-proxy.ts`, `atomic-json-store.ts`, `push-store.ts` and `push-service.ts`. Server-only credentials (S3, VAPID private key, push admin key, chat credential, trusted proxy secret) are read only in server code.

## Configured origins

The browser reaches network services only through the bases named in `src/pwa/config.ts` (`PWA_NETWORK_ENV`); `connectOrigins` in `src/lib/security/csp.ts` turns the configured ones into the `connect-src` list, and `assertConnectSrcCoversConfigured` refuses a policy that is missing one or carries an origin no configuration name yields. Image sources add `NEXT_PUBLIC_MEDIA_STORAGE_ORIGIN`.
