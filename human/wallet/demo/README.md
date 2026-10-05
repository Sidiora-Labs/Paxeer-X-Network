# `@paxeer/demo`

Next.js reference app for the wallet SDK (`@paxeer/wallet`, the workspace package in `../sdk`). It signs a user in with Supabase, provisions the embedded wallet and sends transactions through the wallet gateway.

## Run

From `human/wallet`:

```sh
pnpm install
pnpm -r --filter ./sdk build
pnpm --filter @paxeer/demo dev      # http://localhost:3000
```

Set these in the environment or in an ignored `demo/.env.local`:

| Variable | Purpose |
| --- | --- |
| `NEXT_PUBLIC_PAXEER_WALLET_API` | Wallet gateway origin: HTTPS, or loopback HTTP, with no path or credentials |
| `NEXT_PUBLIC_SUPABASE_URL`, `NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY` | Supabase project used for sign-in |
| `NEXT_PUBLIC_AUTH_REDIRECT_URL` | OAuth and magic-link return URL; defaults to `<origin>/auth/callback` in the browser |
| `NEXT_PUBLIC_SITE_URL` | Optional metadata base URL |

The Supabase project must allow the `/auth/callback` URL of the running demo as a redirect target.

Other scripts: `pnpm --filter @paxeer/demo build`, `start` (port 3000), `lint` and `typecheck`.

## Source

| File | Role |
| --- | --- |
| `src/lib/paxeer.ts` | `paxeerWallet()` SDK singleton from the environment, and `getAuthRedirectUrl()` |
| `src/lib/format.ts` | `truncateAddress`, `weiToNative`, `nativeToWei`, `isAddress` |
| `src/components/ConnectButton.tsx` | Connect button that opens the wallet modal |
| `src/components/WalletModal.tsx` | Sign-in (Google, X, GitHub, email magic link), wallet view and send flow, on Radix Dialog |
| `src/components/ProviderButton.tsx` | One sign-in provider button |
| `src/components/SendTxForm.tsx` | Send form shown inside the modal |
| `src/components/FundedAccountPanel.tsx` | `unifiedOrigin()` and `SupportedWalletPanel`, which drives an EIP-1193 provider (an injected EIP-6963 wallet or `PaxeerProvider`) against the gateway, including custody authorization and plan intents |
| `src/app/auth/callback/page.tsx` | Sign-in return page that restores the session and goes back to `/` |
| `public/icons/` | Provider icons |
