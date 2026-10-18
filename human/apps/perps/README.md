# Paxeer Network Perps

pnpm workspace for the Paxeer Network perps web app (`apps/web`) and its packages (`packages/*`).

## Commands

Run from this directory with pnpm 10 (`packageManager` in `package.json`).

| Command | Purpose |
| --- | --- |
| `pnpm install --frozen-lockfile` | Install the workspace from `pnpm-lock.yaml`. |
| `pnpm dev` | Build the data gateway package and start the web app on port 3000. |
| `pnpm build` | Production build of the web app. |
| `pnpm start` | Serve the production build on port 3000. |
| `pnpm typecheck` | Type-check the web app and the packages it imports. |
| `pnpm test` | Run the Node test suite in `tests/*.test.ts`. |
| `pnpm test:browser` | Run the Playwright browser tests. |
| `pnpm check:ui` | Verify the tracked presentation sources against `docs/ui-source-baseline.json`; exits nonzero and lists every file that drifted. |
| `pnpm check:ui:write` | Regenerate `docs/ui-source-baseline.json` from the tracked presentation sources at the current HEAD after an intended presentation change. |

## UI source baseline

`docs/ui-source-baseline.json` records, for every tracked `packages/*/src/**/*.css`, `packages/*/src/**/*.tsx` and `apps/web/**/*.css` file, the SHA-256 of its runtime presentation (see `scripts/ui-presentation.mjs`) together with the commit it was generated from. After an intended presentation change, commit the change, run `pnpm check:ui:write` and commit the regenerated baseline.
