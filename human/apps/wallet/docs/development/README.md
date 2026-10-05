# Development

Run every command from `human/apps/wallet`.

## Setup

- Node 20 (`.nvmrc`) and pnpm.
- The app links the wallet SDK from `../../wallet/sdk` (`"@paxeer/wallet": "link:../../wallet/sdk"`); build it first from `human/wallet` with `pnpm install` and `pnpm -r --filter ./sdk build`.

```sh
pnpm install --frozen-lockfile
pnpm dev                       # http://localhost:3099
```

Set the build-time variables listed in the [app README](../../README.md#configuration) in the environment, or in a local `.env.local`, which git ignores.

## Checks

```sh
pnpm lint                      # eslint .
pnpm run type-check            # tsc --noEmit
pnpm test                      # vitest run
pnpm run test:wallet-app       # Playwright, playwright.wallet-app.config.ts
pnpm run check:swap-abis       # swap ABI index drift
scripts/scan-secrets.sh        # committed secrets and local artefacts
```

## Build

```sh
pnpm build                     # scripts/build-pwa.mjs, then next build --webpack
pnpm start                     # next start
```

Build output (`.next`, `public/sw.js`, `tsconfig.tsbuildinfo`) is ignored by git.
